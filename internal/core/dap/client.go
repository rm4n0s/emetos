package dap

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
)

// DefaultEventBuffer is how many events the client holds between the goroutine that reads the adapter and the consumer of
// Events when ClientOptions.EventBuffer is not set.
const DefaultEventBuffer = 256

// exitWait is how long a request that failed because the adapter's stream ended waits for the adapter program to be reaped, to
// put its exit status into the error.
const exitWait = 200 * time.Millisecond

// Direction says which way a traced message went.
type Direction int

const (
	// ToAdapter is a message the client sent.
	ToAdapter Direction = iota
	// FromAdapter is a message the client received.
	FromAdapter
)

// TraceMessage is one message that crossed the connection, for the debug adapter trackers of the host (SPEC §10.31) and
// the log of an adapter.
type TraceMessage struct {
	Direction Direction
	// Type is "request", "response" or "event".
	Type string
	// Name is the command of a request or response, or the name of an event.
	Name string
	// Seq is the sequence number of the message.
	Seq int
	// Body is the whole JSON message. It is a copy the callback may keep.
	Body []byte
	// Time is when the message was written or read.
	Time time.Time
	// Elapsed is, for the response to a request of the client, the time since it was sent.
	Elapsed time.Duration
}

// RequestHandler answers a request of the adapter (runInTerminal, startDebugging). The result is the body of the
// response; an error makes an unsuccessful response whose message is the error's. It runs on a goroutine of its own, with a
// context that ends when the connection ends.
type RequestHandler func(ctx context.Context, command string, args json.RawMessage) (any, error)

// ClientOptions are the settings of a Client; the zero value is usable.
type ClientOptions struct {
	// MaxMessageSize is the largest body ReadFrame accepts; zero means DefaultMaxMessageSize.
	MaxMessageSize int
	// EventBuffer is the capacity of the channel of Events; zero means DefaultEventBuffer. When it is full the client stops
	// reading the adapter (and so stops answering its requests) until the consumer takes an event: the adapter is held up
	// by the pipe, and memory does not grow.
	EventBuffer int
	// RequestTimeout, when positive, limits every request that has no sooner deadline; a request that exceeds it fails with
	// the tag DapTimeout.
	RequestTimeout time.Duration
	// Logger receives the protocol problems that do not end the connection; nil discards them.
	Logger *slog.Logger
	// Trace, when set, is called for every message that is written (just before) or read, on the goroutine that does it and in
	// the order of the wire. It must not block.
	Trace func(TraceMessage)
	// HandleRequest answers the requests of the adapter; nil answers each with an unsuccessful response ("unknown request").
	HandleRequest RequestHandler
}

// Client is a connection to a debug adapter. It is safe for concurrent use: one goroutine reads, one writes the queue of
// outgoing messages in order, and each request of the adapter is answered on a goroutine of its own. Nothing a caller does
// blocks on the stream except waiting for the response of its own request.
//
// The events reach one consumer, in the order of the wire, through Events. A response is handed to its caller as soon as it is
// read, so it may be seen before events that arrived earlier and are still in the channel; a consumer that needs the events
// first waits for them itself (the session driver of step 25.2 does, with the events it knows to expect). A consumer that
// issues a request from the loop that takes the events can deadlock when the adapter sends more events than EventBuffer before
// it answers that request, so such a request is better started on a goroutine of its own.
type Client struct {
	rwc  io.ReadWriteCloser
	opts ClientOptions
	log  *slog.Logger

	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	seq     int
	pending map[int]*pendingCall
	closed  bool
	cause   error
	done    chan struct{}

	capMu sync.Mutex
	caps  Capabilities

	out    *queue[outgoing]
	events chan Event
	wg     sync.WaitGroup
}

type pendingCall struct {
	command string
	started time.Time
	reply   chan Response
}

// outgoing is a message waiting for the writer.
type outgoing struct {
	body []byte
	meta TraceMessage
}

// exitStatus is implemented by the transports that are a program (Process, ServerProcess): the client asks for the exit
// status to put into the error of a request the program's end interrupted.
type exitStatus interface {
	Done() <-chan struct{}
	ExitCode() int
}

// NewClient starts a client over rwc: the goroutines that read and write begin at once. The client owns rwc and closes it
// when it ends. Nothing is sent: the initialize request is the caller's first.
func NewClient(rwc io.ReadWriteCloser, opts ClientOptions) *Client {
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if opts.EventBuffer <= 0 {
		opts.EventBuffer = DefaultEventBuffer
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &Client{
		rwc: rwc, opts: opts, log: log, ctx: ctx, cancel: cancel,
		pending: map[int]*pendingCall{},
		done:    make(chan struct{}),
		out:     newQueue[outgoing](),
		events:  make(chan Event, opts.EventBuffer),
	}
	c.wg.Add(2)
	go c.readLoop()
	go c.writeLoop()
	return c
}

// Events is the stream of the adapter's events, in the order they were read. It is closed when the connection has ended and
// the events that had arrived were taken. The consumer must keep taking events (see ClientOptions.EventBuffer).
func (c *Client) Events() <-chan Event { return c.events }

// Done is closed when the connection has ended: it was closed, or the adapter went away, or the stream broke.
func (c *Client) Done() <-chan struct{} { return c.done }

// Err is why the connection ended: nil while it runs and after a Close; io.EOF when the adapter closed the stream; otherwise
// the framing or read or write error that ended it.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cause
}

// Wait returns when the goroutines of the client have ended, which they do soon after Done is closed. It does not wait for the
// handlers of requests of the adapter.
func (c *Client) Wait() { c.wg.Wait() }

// Capabilities returns the capabilities of the adapter: those of the initialize response, changed by the capabilities events
// read since. They are empty before the initialize response.
func (c *Client) Capabilities() Capabilities {
	c.capMu.Lock()
	defer c.capMu.Unlock()
	return c.caps
}

// Close ends the client: the pending requests fail, the stream is closed and the queued messages that were not written are
// dropped. Closing twice is harmless. For a program (Process) the close also ends it: SIGTERM, then SIGKILL after three seconds.
// Send disconnect first to let the adapter end the debuggee properly. It fails with the tag DapTransportCloseFailed.
func (c *Client) Close() error {
	if !c.terminate(nil) {
		return nil
	}
	if err := c.rwc.Close(); err != nil {
		return errors.NewErr("DapTransportCloseFailed", err)
	}
	return nil
}

// abort ends the client because of cause, which is logged when it is a failure.
func (c *Client) abort(cause error) {
	if !c.terminate(cause) {
		return
	}
	if cause != nil && !errors.Is(cause, io.EOF) {
		c.log.Warn("the connection to the debug adapter broke", "err", cause)
	}
	if err := c.rwc.Close(); err != nil {
		c.log.Debug("closing the transport failed", "err", err)
	}
}

// terminate marks the client as ended and wakes everything that waits; it reports whether this call was the one that ended it.
func (c *Client) terminate(cause error) bool {
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
	close(c.done)
	return true
}

// Request sends a request and waits for its response. args is the "arguments" member: a json.RawMessage is sent as it is, nil
// sends none, anything else is encoded as JSON. The ctx is the limit of the wait (ClientOptions.RequestTimeout is another):
// when it ends first, the request is forgotten, the adapter is told with a cancel request when it supports one, and a response
// that comes later is dropped.
//
// A response with success false is returned together with the error, so that the caller can read the adapter's body.
//
// It fails with the tags DapArgumentsInvalid, DapRequestCancelled (the error wraps the context's error), DapTimeout (the context's
// deadline or the request timeout), DapClientClosed (Close was called), DapAdapterExited (the adapter ended or its stream broke
// before it answered; the metadata "cause" says why and "exitCode" holds the exit status of a program), DapResponseFailed (the
// adapter answered with an error; the metadata hold its "reason", "showUser", "url" and "errorId") and, when the stream broke
// by a violation of the protocol, the tag of that (DapProtocolInvalid, DapMessageTooLarge).
func (c *Client) Request(ctx context.Context, command string, args any) (Response, error) {
	if c.opts.RequestTimeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.RequestTimeout)
		defer cancel()
	}
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return Response{}, errors.NewErr("DapTimeout", err).SetMetadata("command", command)
		}
		return Response{}, errors.NewErr("DapRequestCancelled", err).SetMetadata("command", command)
	}
	raw, err := marshalArgs(args)
	if err != nil {
		return Response{}, errors.NewErr("DapArgumentsInvalid", err).SetMetadata("command", command)
	}
	call := &pendingCall{command: command, started: time.Now(), reply: make(chan Response, 1)}
	seq, sent, err := c.send(command, raw, call)
	if err != nil {
		return Response{}, errors.NewErr("DapArgumentsInvalid", err).SetMetadata("command", command)
	}
	var r Response
	if sent {
		select {
		case r = <-call.reply:
		case <-ctx.Done():
			c.forget(seq)
			c.cancelOnAdapter(seq)
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return Response{}, errors.NewErr("DapTimeout", ctx.Err()).SetMetadata("command", command, "seq", seq)
			}
			return Response{}, errors.NewErr("DapRequestCancelled", ctx.Err()).SetMetadata("command", command, "seq", seq)
		case <-c.done:
			select {
			case r = <-call.reply: // the response came just before the end
			default:
				c.forget(seq)
				sent = false
			}
		}
	}
	if !sent {
		cause := c.Err()
		if appErr, ok := errors.FromError(cause); ok && (appErr.Tag == "DapProtocolInvalid" || appErr.Tag == "DapMessageTooLarge") {
			return Response{}, cause // the stream broke by a violation of the protocol: that is the reason
		}
		if cause == nil {
			return Response{}, errors.New("DapClientClosed", "the client of the debug adapter was closed", "command", command)
		}
		meta := []any{"command", command, "cause", cause.Error()}
		if code, ok := c.exitCode(); ok {
			meta = append(meta, "exitCode", code)
		}
		return Response{}, errors.New("DapAdapterExited", "the debug adapter ended before it answered "+command, meta...)
	}
	if !r.Success {
		text := r.Message
		var body ErrorBody
		if len(r.Body) > 0 {
			if jerr := json.Unmarshal(r.Body, &body); jerr != nil {
				c.log.Debug("the body of a failed response is not an error body", "command", command, "err", jerr)
			}
		}
		id, showUser, url := 0, false, ""
		if body.Error != nil {
			id, showUser, url = body.Error.ID, body.Error.ShowUser, body.Error.URL
			if body.Error.Format != "" {
				text = FormatMessage(*body.Error)
			}
		}
		if text == "" {
			text = "the debug adapter answered " + command + " with an error"
		}
		return r, errors.New("DapResponseFailed", text, "command", command, "seq", seq, "reason", r.Message, "errorId", id, "showUser", showUser, "url", url)
	}
	return r, nil
}

// Call is Request with the body of the response decoded into R (nothing is decoded when the body is empty or null). A body that
// does not fit R fails with the tag DapBodyInvalid; the other failures are Request's.
func Call[R any](ctx context.Context, c *Client, command string, args any) (R, error) {
	var out R
	resp, err := c.Request(ctx, command, args)
	if err != nil {
		return out, err
	}
	if len(resp.Body) > 0 && string(resp.Body) != "null" {
		if err := decodeBody(command, resp.Body, &out); err != nil {
			return out, err
		}
	}
	return out, nil
}

// marshalArgs encodes the arguments of a request; nil and an empty json.RawMessage mean none. The encoder checks and compacts
// a json.RawMessage.
func marshalArgs(args any) (json.RawMessage, error) {
	if raw, ok := args.(json.RawMessage); (ok && len(raw) == 0) || args == nil {
		return nil, nil
	}
	return json.Marshal(args)
}

// outRequest is a request as it is written.
type outRequest struct {
	Seq       int             `json:"seq"`
	Type      string          `json:"type"`
	Command   string          `json:"command"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// send gives the request the next sequence number and queues it. When call is not nil it is registered to receive the response
// under that number. It reports sent false when the client has ended; the queue order is the number order.
func (c *Client) send(command string, raw json.RawMessage, call *pendingCall) (seq int, sent bool, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, false, nil
	}
	body, err := json.Marshal(outRequest{Seq: c.seq + 1, Type: TypeRequest, Command: command, Arguments: raw})
	if err != nil {
		return 0, false, err
	}
	c.seq++
	if call != nil {
		c.pending[c.seq] = call
	}
	c.out.push(outgoing{body: body, meta: TraceMessage{Type: TypeRequest, Name: command, Seq: c.seq}})
	return c.seq, true, nil
}

// cancelOnAdapter asks the adapter to stop working on a request that nobody waits for, when it supports that.
func (c *Client) cancelOnAdapter(requestSeq int) {
	if !c.Capabilities().SupportsCancelRequest {
		return
	}
	raw, err := json.Marshal(CancelArguments{RequestID: requestSeq})
	if err != nil {
		return // two integers always encode
	}
	if _, _, err := c.send(CmdCancel, raw, nil); err != nil {
		c.log.Debug("cannot send cancel", "err", err)
	}
}

func (c *Client) forget(seq int) {
	c.mu.Lock()
	delete(c.pending, seq)
	c.mu.Unlock()
}

// Pending is how many requests wait for their response.
func (c *Client) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pending)
}

// exitCode is the exit status of the adapter program when the transport is one and it ended just now.
func (c *Client) exitCode() (int, bool) {
	p, ok := c.rwc.(exitStatus)
	if !ok {
		return 0, false
	}
	select {
	case <-p.Done():
		return p.ExitCode(), true
	case <-time.After(exitWait):
		return 0, false
	}
}

// writeLoop writes the queued messages in order.
func (c *Client) writeLoop() {
	defer c.wg.Done()
	for {
		m, ok := c.out.pop()
		if !ok {
			return
		}
		// Traced before the write: once the adapter has the message it may answer at once, and the answer must not be traced
		// before the request.
		c.trace(ToAdapter, m.meta, m.body)
		if err := WriteFrame(c.rwc, m.body); err != nil {
			c.abort(err)
			return
		}
	}
}

// readLoop reads the messages of the adapter until the stream ends, and closes the event stream when it does.
func (c *Client) readLoop() {
	defer c.wg.Done()
	defer close(c.events)
	br := bufio.NewReaderSize(c.rwc, 64<<10)
	for {
		body, err := ReadFrame(br, c.opts.MaxMessageSize)
		if err != nil {
			c.abort(err)
			return
		}
		if len(body) == 0 {
			continue // VS Code ignores an empty message
		}
		if !c.dispatch(body) {
			return
		}
	}
}

// envelope is a message as it is read; its type says which members matter.
type envelope struct {
	Seq        int             `json:"seq"`
	Type       string          `json:"type"`
	Command    string          `json:"command"`
	Arguments  json.RawMessage `json:"arguments"`
	RequestSeq int             `json:"request_seq"`
	Success    bool            `json:"success"`
	Message    string          `json:"message"`
	Body       json.RawMessage `json:"body"`
	Event      string          `json:"event"`
}

// decodeMessage reads the JSON of a message and checks that it has what its type needs. It fails with the tag DapProtocolInvalid.
func decodeMessage(body []byte) (envelope, error) {
	var env envelope
	if err := json.Unmarshal(body, &env); err != nil {
		return envelope{}, errors.NewErr("DapProtocolInvalid", err).SetMetadata("reason", "not JSON", "length", len(body))
	}
	switch {
	case env.Type == TypeEvent && env.Event == "":
		return envelope{}, errors.New("DapProtocolInvalid", "an event has no name", "reason", "event without name", "seq", env.Seq)
	case env.Type == TypeRequest && env.Command == "":
		return envelope{}, errors.New("DapProtocolInvalid", "a request has no command", "reason", "request without command", "seq", env.Seq)
	case env.Type == TypeResponse && env.RequestSeq == 0:
		return envelope{}, errors.New("DapProtocolInvalid", "a response has no request_seq", "reason", "response without request_seq", "seq", env.Seq)
	case env.Type != TypeEvent && env.Type != TypeRequest && env.Type != TypeResponse:
		return envelope{}, errors.New("DapProtocolInvalid", "a message has an unknown type", "reason", "unknown type", "type", clip(env.Type), "seq", env.Seq)
	}
	return env, nil
}

// dispatch routes one message that was read. It reports false when the client ended while an event was waiting for room.
func (c *Client) dispatch(body []byte) bool {
	env, err := decodeMessage(body)
	if err != nil {
		c.log.Warn("the debug adapter sent a message that cannot be used", "err", err)
		return true
	}
	switch env.Type {
	case TypeEvent:
		c.trace(FromAdapter, TraceMessage{Type: TypeEvent, Name: env.Event, Seq: env.Seq}, body)
		if env.Event == EventCapabilities {
			c.mergeCapabilities(env.Body, true)
		}
		select {
		case c.events <- Event{Seq: env.Seq, Event: env.Event, Body: env.Body}:
		case <-c.ctx.Done():
			return false
		}
	case TypeResponse:
		c.answer(env, body)
	case TypeRequest:
		c.trace(FromAdapter, TraceMessage{Type: TypeRequest, Name: env.Command, Seq: env.Seq}, body)
		c.serve(env)
	}
	return true
}

// answer hands a response to the request that waits for it.
func (c *Client) answer(env envelope, body []byte) {
	c.mu.Lock()
	call := c.pending[env.RequestSeq]
	delete(c.pending, env.RequestSeq)
	c.mu.Unlock()
	if call == nil {
		// The request was cancelled or never existed; a response to a cancelled request is normal.
		c.log.Debug("response without a pending request", "request_seq", env.RequestSeq, "command", env.Command)
		c.trace(FromAdapter, TraceMessage{Type: TypeResponse, Name: env.Command, Seq: env.Seq}, body)
		return
	}
	c.trace(FromAdapter, TraceMessage{Type: TypeResponse, Name: call.command, Seq: env.Seq, Elapsed: time.Since(call.started)}, body)
	if env.Success && call.command == CmdInitialize {
		c.mergeCapabilities(env.Body, false)
	}
	call.reply <- Response{Seq: env.Seq, RequestSeq: env.RequestSeq, Success: env.Success, Command: call.command, Message: env.Message, Body: env.Body}
}

// mergeCapabilities adds the capabilities of a body to those the client holds: only the members the adapter sent change.
// wrapped says the body is the capabilities event's, with the capabilities one level down.
func (c *Client) mergeCapabilities(body json.RawMessage, wrapped bool) {
	if len(body) == 0 || string(body) == "null" {
		return
	}
	c.capMu.Lock()
	defer c.capMu.Unlock()
	merged := c.caps
	var err error
	if wrapped {
		ev := CapabilitiesEventBody{Capabilities: merged}
		if err = json.Unmarshal(body, &ev); err == nil {
			merged = ev.Capabilities
		}
	} else {
		err = json.Unmarshal(body, &merged)
	}
	if err != nil {
		c.log.Warn("the capabilities of the debug adapter cannot be read", "err", errors.NewErr("DapBodyInvalid", err))
		return
	}
	c.caps = merged
}

// serve answers a request of the adapter on a goroutine of its own.
func (c *Client) serve(env envelope) {
	ctx, cancel := context.WithCancel(c.ctx)
	go func() {
		defer cancel()
		if c.opts.HandleRequest == nil {
			c.respond(env, nil, "unknown request '"+env.Command+"'") // as VS Code words it
			return
		}
		result, err := c.opts.HandleRequest(ctx, env.Command, env.Arguments)
		if err != nil {
			msg := err.Error()
			if msg == "" {
				msg = "the request failed"
			}
			c.respond(env, nil, msg)
			return
		}
		c.respond(env, result, "")
	}()
}

// outResponse is a response as it is written.
type outResponse struct {
	Seq        int             `json:"seq"`
	Type       string          `json:"type"`
	RequestSeq int             `json:"request_seq"`
	Success    bool            `json:"success"`
	Command    string          `json:"command"`
	Message    string          `json:"message,omitempty"`
	Body       json.RawMessage `json:"body,omitempty"`
}

// respond queues the response to a request of the adapter.
func (c *Client) respond(env envelope, result any, failure string) {
	resp := outResponse{Type: TypeResponse, RequestSeq: env.Seq, Command: env.Command, Success: failure == "", Message: failure}
	if failure == "" && result != nil {
		raw, err := json.Marshal(result)
		if err != nil {
			c.log.Error("cannot encode the response to a request of the adapter", "command", env.Command, "err", errors.NewErr("DapBodyInvalid", err))
			resp.Success, resp.Message = false, "the client cannot encode the result: "+err.Error()
		} else {
			resp.Body = raw
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	resp.Seq = c.seq + 1
	body, err := json.Marshal(resp)
	if err != nil {
		c.log.Error("cannot encode a response", "command", env.Command, "err", err)
		return // strings, numbers and a valid raw message always encode
	}
	c.seq++
	c.out.push(outgoing{body: body, meta: TraceMessage{Type: TypeResponse, Name: env.Command, Seq: resp.Seq}})
}

// trace calls the trace callback of the options.
func (c *Client) trace(dir Direction, meta TraceMessage, body []byte) {
	if c.opts.Trace == nil {
		return
	}
	meta.Direction = dir
	meta.Time = time.Now()
	meta.Body = append([]byte(nil), body...)
	c.opts.Trace(meta)
}

// Transport returns the stream the client was made over, so that the caller can reach the program behind it (a *Process or a
// *ServerProcess: Done, ExitCode, PID, Stop). Closing it is the client's job.
func (c *Client) Transport() io.ReadWriteCloser { return c.rwc }

// decodeBody decodes the body of the response to command into out. It fails with the tag DapBodyInvalid.
func decodeBody(command string, body json.RawMessage, out any) error {
	if err := json.Unmarshal(body, out); err != nil {
		return errors.NewErr("DapBodyInvalid", err).SetMetadata("command", command)
	}
	return nil
}
