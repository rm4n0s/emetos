package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
)

// The error codes of JSON-RPC and of the protocol (LSP 3.17 "Response Message").
const (
	CodeParseError           = -32700
	CodeInvalidRequest       = -32600
	CodeMethodNotFound       = -32601
	CodeInvalidParams        = -32602
	CodeInternalError        = -32603
	CodeServerNotInitialized = -32002
	CodeUnknownErrorCode     = -32001
	CodeRequestFailed        = -32803
	CodeServerCancelled      = -32802
	CodeContentModified      = -32801
	CodeRequestCancelled     = -32800
)

// ResponseError is the error member of a response.
type ResponseError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Direction says which way a traced message went.
type Direction int

const (
	// ToServer is a message the client sent.
	ToServer Direction = iota
	// FromServer is a message the client received.
	FromServer
)

// MessageKind is what a traced message is.
type MessageKind int

const (
	// KindRequest is a request: the server's or the client's, by Direction.
	KindRequest MessageKind = iota
	// KindResponse is a response to a request.
	KindResponse
	// KindNotification is a notification.
	KindNotification
)

// TraceMessage is one message that crossed the connection, for the trace of a server (step 12.3's
// "<id>.trace.server" setting: "messages" shows the method and the id, "verbose" shows Body too).
type TraceMessage struct {
	Direction Direction
	Kind      MessageKind
	// Method is the method; for a response it is the method of its request when that is known.
	Method string
	// ID is the id of a request or a response, as JSON; empty for a notification.
	ID string
	// Body is the whole JSON message. It is a copy the callback may keep.
	Body []byte
	// Time is when the message was written or read.
	Time time.Time
	// Elapsed is, for the response to a request of the client, the time since it was sent.
	Elapsed time.Duration
}

// ConnOptions are the settings of a connection; the zero value is usable.
type ConnOptions struct {
	// MaxMessageSize is the largest body ReadFrame accepts; zero means DefaultMaxMessageSize.
	MaxMessageSize int
	// Logger receives the protocol problems that do not end the connection; nil discards them.
	Logger *slog.Logger
	// Trace, when set, is called for every message that is written (just before) or read, on the goroutine that
	// does it and in the order of the wire. It must not block.
	Trace func(TraceMessage)
	// HandleRequest answers a request of the server. It runs on its own goroutine, with a context that is
	// cancelled when the server cancels the request or the connection ends. The result is the "result"
	// of the response; an error becomes the "error" (see responseError): the tags MethodNotFound and
	// InvalidParams give their codes, a "code" in the metadata gives its own, anything else is an internal
	// error. Nil answers every request with MethodNotFound.
	HandleRequest func(ctx context.Context, method string, params json.RawMessage) (any, error)
	// HandleNotification receives the notifications of the server, one at a time and in the order they
	// arrived, on a goroutine of its own, so it may wait for a response of the server. Nil drops them.
	HandleNotification func(method string, params json.RawMessage)
}

// Conn is a JSON-RPC 2.0 connection over a stream (LSP "Base Protocol"). It is safe for concurrent use:
// one goroutine reads, one writes the queue of outgoing messages in order, one delivers the notifications,
// and each request of the server is answered on a goroutine of its own. Nothing a caller does blocks on
// the stream except waiting for the response of its own request.
type Conn struct {
	rwc  io.ReadWriteCloser
	opts ConnOptions
	log  *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc

	mu       sync.Mutex
	nextID   int64
	pending  map[int64]*pendingCall
	inflight map[string]context.CancelFunc
	closed   bool
	cause    error
	done     chan struct{}

	out *queue[outgoing]
	in  *queue[notification]
	wg  sync.WaitGroup
}

type pendingCall struct {
	method  string
	started time.Time
	reply   chan reply
}

type reply struct {
	result json.RawMessage
	err    *ResponseError
}

type notification struct {
	method string
	params json.RawMessage
}

// outgoing is a message waiting for the writer, or (with a nil body) a marker the writer closes when it
// gets there.
type outgoing struct {
	body    []byte
	meta    TraceMessage
	flushed chan struct{}
}

// message is a JSON-RPC message as it is read; which member is set tells what it is.
type message struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *ResponseError  `json:"error"`
}

// NewConn starts a connection over rwc: the goroutines that read and write begin at once. The connection
// owns rwc and closes it when it ends.
func NewConn(rwc io.ReadWriteCloser, opts ConnOptions) *Conn {
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &Conn{
		rwc: rwc, opts: opts, log: log, ctx: ctx, cancel: cancel,
		pending:  map[int64]*pendingCall{},
		inflight: map[string]context.CancelFunc{},
		done:     make(chan struct{}),
		out:      newQueue[outgoing](),
		in:       newQueue[notification](),
	}
	c.wg.Add(3)
	go c.readLoop()
	go c.writeLoop()
	go c.notifyLoop()
	return c
}

// Done is closed when the connection has ended: it was closed, or the server went away, or the stream
// broke.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err is why the connection ended: nil while it runs and after a Close; io.EOF when the server closed the
// stream; otherwise the framing or write error that ended it.
func (c *Conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cause
}

// Wait returns when the goroutines of the connection have ended, which they do soon after Done is closed.
// It does not wait for the handlers of requests of the server.
func (c *Conn) Wait() { c.wg.Wait() }

// Close ends the connection: the pending calls fail, the stream is closed and the queued messages that
// were not written are dropped (use Flush first to keep them). Closing twice is harmless. It fails with
// the tag TransportCloseFailed.
func (c *Conn) Close() error {
	if !c.terminate(nil) {
		return nil
	}
	if err := c.rwc.Close(); err != nil {
		return errors.NewErr("TransportCloseFailed", err)
	}
	return nil
}

// abort ends the connection because of cause, which is logged when it is a failure.
func (c *Conn) abort(cause error) {
	if !c.terminate(cause) {
		return
	}
	if cause != nil && !errors.Is(cause, io.EOF) {
		c.log.Warn("the connection to the server broke", "err", cause)
	}
	if err := c.rwc.Close(); err != nil {
		c.log.Debug("closing the transport failed", "err", err)
	}
}

// terminate marks the connection as ended and wakes everything that waits; it reports whether this call
// was the one that ended it.
func (c *Conn) terminate(cause error) bool {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return false
	}
	c.closed = true
	c.cause = cause
	c.mu.Unlock()
	c.cancel()
	c.out.close()
	c.in.close()
	close(c.done)
	return true
}

// Call sends a request and waits for its response, which is decoded into result unless result is nil or
// the response is null. A request has no timeout of its own (the protocol has none): ctx is the limit.
// When ctx ends first, the server is told with "$/cancelRequest" and the call fails at once; a response
// that comes later is dropped.
//
// It fails with the tags ParamsInvalid, RequestCancelled (the error wraps the context's error), ServerClosed
// (the connection ended; the metadata "cause" says why), ServerError (the server answered with an error;
// the metadata hold its "code", "method" and, when it sent one, "data") and ResultInvalid.
func (c *Conn) Call(ctx context.Context, method string, params, result any) error {
	if err := ctx.Err(); err != nil {
		return errors.NewErr("RequestCancelled", err).SetMetadata("method", method)
	}
	raw, err := marshalParams(params)
	if err != nil {
		return errors.NewErr("ParamsInvalid", err).SetMetadata("method", method)
	}
	call := &pendingCall{method: method, started: time.Now(), reply: make(chan reply, 1)}
	c.mu.Lock()
	if c.closed {
		cause := c.cause
		c.mu.Unlock()
		return errors.New("ServerClosed", "the connection to the server has ended", "method", method, "cause", causeText(cause))
	}
	c.nextID++
	id := c.nextID
	c.pending[id] = call
	c.mu.Unlock()

	body, err := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      int64           `json:"id"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params,omitempty"`
	}{"2.0", id, method, raw})
	if err != nil {
		c.forget(id)
		return errors.NewErr("ParamsInvalid", err).SetMetadata("method", method)
	}
	c.out.push(outgoing{body: body, meta: TraceMessage{Kind: KindRequest, Method: method, ID: strconv.FormatInt(id, 10)}})

	var r reply
	select {
	case r = <-call.reply:
	case <-ctx.Done():
		c.forget(id)
		if cancel, err := json.Marshal(CancelParams{ID: id}); err == nil {
			c.queueNotification("$/cancelRequest", cancel)
		}
		return errors.NewErr("RequestCancelled", ctx.Err()).SetMetadata("method", method, "id", id)
	case <-c.done:
		select {
		case r = <-call.reply: // the response came just before the end
		default:
			c.forget(id)
			return errors.New("ServerClosed", "the connection to the server ended before the response", "method", method, "cause", causeText(c.Err()))
		}
	}
	if r.err != nil {
		if len(r.err.Data) > 0 {
			return errors.New("ServerError", r.err.Message, "method", method, "code", r.err.Code, "data", string(r.err.Data))
		}
		return errors.New("ServerError", r.err.Message, "method", method, "code", r.err.Code)
	}
	if result != nil && len(r.result) > 0 && string(r.result) != "null" {
		if err := json.Unmarshal(r.result, result); err != nil {
			if _, ok := errors.FromError(err); ok {
				return err // a type of ours that cannot read the shape (HoverInvalid, ...) has its tag already
			}
			return errors.NewErr("ResultInvalid", err).SetMetadata("method", method)
		}
	}
	return nil
}

// Notify queues a notification. It does not wait for the stream, so it is safe to call from the UI
// thread; the messages leave in the order they were queued, with the requests. It fails with the tags
// ParamsInvalid and NotifyClosed.
func (c *Conn) Notify(method string, params any) error {
	raw, err := marshalParams(params)
	if err != nil {
		return errors.NewErr("ParamsInvalid", err).SetMetadata("method", method)
	}
	c.mu.Lock()
	closed, cause := c.closed, c.cause
	c.mu.Unlock()
	if closed {
		return errors.New("NotifyClosed", "the connection to the server has ended", "method", method, "cause", causeText(cause))
	}
	c.queueNotification(method, raw)
	return nil
}

// Flush returns when everything queued before the call has been written, or fails with the tag
// FlushAborted when ctx ends or the connection ends first.
func (c *Conn) Flush(ctx context.Context) error {
	marker := make(chan struct{})
	if c.out.push(outgoing{flushed: marker}) {
		select {
		case <-marker:
			return nil
		case <-ctx.Done():
			return errors.New("FlushAborted", "the context ended before the messages were written", "reason", ctx.Err().Error())
		case <-c.done:
			select {
			case <-marker:
				return nil
			default:
			}
		}
	}
	return errors.New("FlushAborted", "the connection ended before the messages were written", "reason", causeText(c.Err()))
}

func (c *Conn) forget(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// queueNotification queues a notification whose params are already JSON.
func (c *Conn) queueNotification(method string, params json.RawMessage) {
	body, err := json.Marshal(struct {
		JSONRPC string          `json:"jsonrpc"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params,omitempty"`
	}{"2.0", method, params})
	if err != nil {
		// params came out of json.Marshal or is empty, so the message is always valid.
		c.log.Error("cannot encode a notification", "method", method, "err", err)
		return
	}
	c.out.push(outgoing{body: body, meta: TraceMessage{Kind: KindNotification, Method: method}})
}

// marshalParams encodes the params of a message; nil means none.
func marshalParams(params any) (json.RawMessage, error) {
	if params == nil {
		return nil, nil
	}
	if raw, ok := params.(json.RawMessage); ok {
		return raw, nil
	}
	return json.Marshal(params)
}

func causeText(cause error) string {
	if cause == nil {
		return "the connection was closed"
	}
	return cause.Error()
}

// writeLoop writes the queued messages in order.
func (c *Conn) writeLoop() {
	defer c.wg.Done()
	for {
		m, ok := c.out.pop(false)
		if !ok {
			return
		}
		if m.body == nil {
			close(m.flushed)
			continue
		}
		// Traced before the write: once the server has the message it may answer at once, and the answer
		// must not be traced before the request.
		c.trace(ToServer, m.meta, m.body)
		if err := WriteFrame(c.rwc, m.body); err != nil {
			c.abort(err)
			return
		}
	}
}

// notifyLoop delivers the notifications of the server in order. After the end of the connection it still
// delivers those that had arrived, so the last log message of a server that crashed is not lost.
func (c *Conn) notifyLoop() {
	defer c.wg.Done()
	for {
		n, ok := c.in.pop(true)
		if !ok {
			return
		}
		if c.opts.HandleNotification != nil {
			c.opts.HandleNotification(n.method, n.params)
		}
	}
}

// readLoop reads the messages of the server until the stream ends.
func (c *Conn) readLoop() {
	defer c.wg.Done()
	br := bufio.NewReaderSize(c.rwc, 64<<10)
	for {
		body, err := ReadFrame(br, c.opts.MaxMessageSize)
		if err != nil {
			c.abort(err)
			return
		}
		c.dispatch(body)
	}
}

// dispatch routes one message that was read.
func (c *Conn) dispatch(body []byte) {
	var m message
	if err := json.Unmarshal(body, &m); err != nil {
		c.log.Warn("the server sent a message that is not JSON-RPC", "err", errors.NewErr("MessageInvalid", err).SetMetadata("length", len(body)))
		return
	}
	switch {
	case m.Method != "" && len(m.ID) > 0:
		c.trace(FromServer, TraceMessage{Kind: KindRequest, Method: m.Method, ID: string(m.ID)}, body)
		c.serve(m)
	case m.Method != "":
		c.trace(FromServer, TraceMessage{Kind: KindNotification, Method: m.Method}, body)
		if m.Method == "$/cancelRequest" {
			c.cancelInflight(m.Params)
			return
		}
		c.in.push(notification{method: m.Method, params: m.Params})
	case len(m.ID) > 0:
		c.answer(m, body)
	default:
		c.log.Warn("the server sent a message with neither a method nor an id", "err",
			errors.New("MessageInvalid", "a message has neither a method nor an id", "length", len(body)))
	}
}

// answer hands a response to the call that waits for it.
func (c *Conn) answer(m message, body []byte) {
	id, err := strconv.ParseInt(string(m.ID), 10, 64)
	c.mu.Lock()
	call := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if err != nil || call == nil {
		// The call was cancelled or never existed; a response to a cancelled request is normal.
		c.log.Debug("response without a pending request", "id", string(m.ID))
		c.trace(FromServer, TraceMessage{Kind: KindResponse, ID: string(m.ID)}, body)
		return
	}
	c.trace(FromServer, TraceMessage{Kind: KindResponse, Method: call.method, ID: string(m.ID), Elapsed: time.Since(call.started)}, body)
	call.reply <- reply{result: m.Result, err: m.Error}
}

// cancelInflight cancels the request of the server that "$/cancelRequest" names.
func (c *Conn) cancelInflight(params json.RawMessage) {
	var p struct {
		ID json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		c.log.Warn("the server sent a $/cancelRequest that cannot be read", "err", errors.NewErr("MessageInvalid", err))
		return
	}
	c.mu.Lock()
	cancel := c.inflight[string(p.ID)]
	c.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// serve answers a request of the server on a goroutine of its own.
func (c *Conn) serve(m message) {
	key := string(m.ID)
	ctx, cancel := context.WithCancel(c.ctx)
	c.mu.Lock()
	c.inflight[key] = cancel
	c.mu.Unlock()
	go func() {
		defer func() {
			cancel()
			c.mu.Lock()
			delete(c.inflight, key)
			c.mu.Unlock()
		}()
		var result any
		var err error
		if c.opts.HandleRequest == nil {
			err = errors.New("MethodNotFound", "the client does not handle this request", "method", m.Method)
		} else {
			result, err = c.opts.HandleRequest(ctx, m.Method, m.Params)
		}
		c.respond(m, result, err)
	}()
}

// respond queues the response to a request of the server.
func (c *Conn) respond(m message, result any, err error) {
	var body []byte
	var merr error
	if err != nil {
		body, merr = json.Marshal(struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Error   ResponseError   `json:"error"`
		}{"2.0", m.ID, responseError(err)})
	} else {
		var raw json.RawMessage = []byte("null")
		if result != nil {
			raw, merr = json.Marshal(result)
		}
		if merr == nil {
			body, merr = json.Marshal(struct {
				JSONRPC string          `json:"jsonrpc"`
				ID      json.RawMessage `json:"id"`
				Result  json.RawMessage `json:"result"`
			}{"2.0", m.ID, raw})
		}
	}
	if merr != nil {
		c.log.Error("cannot encode the response to a request of the server", "method", m.Method, "err", merr)
		body, merr = json.Marshal(struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      json.RawMessage `json:"id"`
			Error   ResponseError   `json:"error"`
		}{"2.0", m.ID, ResponseError{Code: CodeInternalError, Message: "the client cannot encode the result: " + merr.Error()}})
		if merr != nil {
			return // a string and a raw id always encode
		}
	}
	c.out.push(outgoing{body: body, meta: TraceMessage{Kind: KindResponse, Method: m.Method, ID: string(m.ID)}})
}

// responseError turns the error of a handler into the error member of a response: the tag MethodNotFound
// and InvalidParams, an int "code" in the metadata, or a cancelled context choose the code, and the
// message is the error's.
func responseError(err error) ResponseError {
	re := ResponseError{Code: CodeInternalError, Message: err.Error()}
	if errors.Is(err, context.Canceled) {
		re.Code = CodeRequestCancelled
	}
	if appErr, ok := errors.FromError(err); ok {
		switch appErr.Tag {
		case "MethodNotFound":
			re.Code = CodeMethodNotFound
		case "InvalidParams":
			re.Code = CodeInvalidParams
		}
		if code, ok := appErr.Metadata["code"].(int); ok {
			re.Code = code
		}
	}
	return re
}

// trace calls the trace callback of the options.
func (c *Conn) trace(dir Direction, meta TraceMessage, body []byte) {
	if c.opts.Trace == nil {
		return
	}
	meta.Direction = dir
	meta.Time = time.Now()
	meta.Body = append([]byte(nil), body...)
	c.opts.Trace(meta)
}

// CancelParams are the params of "$/cancelRequest".
type CancelParams struct {
	ID int64 `json:"id"`
}
