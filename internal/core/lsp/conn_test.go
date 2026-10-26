package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

const wait = 10 * time.Second

// pipePair joins two ends: what one writes the other reads.
type pipeRWC struct {
	r *io.PipeReader
	w *io.PipeWriter
}

func (p pipeRWC) Read(b []byte) (int, error)  { return p.r.Read(b) }
func (p pipeRWC) Write(b []byte) (int, error) { return p.w.Write(b) }
func (p pipeRWC) Close() error {
	if err := p.w.Close(); err != nil {
		return err
	}
	return p.r.Close()
}

func pipePair() (a, b pipeRWC) {
	ar, bw := io.Pipe()
	br, aw := io.Pipe()
	return pipeRWC{ar, aw}, pipeRWC{br, bw}
}

// peer plays the server by hand.
type peer struct {
	t   *testing.T
	rwc pipeRWC
	br  *bufio.Reader
}

// logRecorder keeps the records a logger got.
type logRecorder struct {
	mu   sync.Mutex
	errs []error
	msgs []string
}

func (l *logRecorder) Enabled(context.Context, slog.Level) bool { return true }
func (l *logRecorder) Handle(_ context.Context, r slog.Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.msgs = append(l.msgs, r.Message)
	r.Attrs(func(a slog.Attr) bool {
		if err, ok := a.Value.Any().(error); ok {
			l.errs = append(l.errs, err)
		}
		return true
	})
	return nil
}
func (l *logRecorder) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *logRecorder) WithGroup(string) slog.Handler      { return l }

func (l *logRecorder) errors() []error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]error(nil), l.errs...)
}

// connWithPeer starts a connection to a hand-driven peer.
func connWithPeer(t *testing.T, opts ConnOptions) (*Conn, *peer) {
	t.Helper()
	cEnd, sEnd := pipePair()
	c := NewConn(cEnd, opts)
	t.Cleanup(func() {
		if err := c.Close(); err != nil {
			t.Errorf("Close: %v", err)
		}
		if err := sEnd.Close(); err != nil {
			t.Errorf("closing the peer: %v", err)
		}
		c.Wait()
	})
	return c, &peer{t: t, rwc: sEnd, br: bufio.NewReader(sEnd)}
}

// read waits for the next message of the client.
func (p *peer) read() message {
	p.t.Helper()
	type res struct {
		m   message
		err error
	}
	ch := make(chan res, 1)
	go func() {
		body, err := ReadFrame(p.br, 0)
		if err != nil {
			ch <- res{err: err}
			return
		}
		var m message
		ch <- res{m: m, err: json.Unmarshal(body, &m)}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			p.t.Fatalf("peer read: %v", r.err)
		}
		return r.m
	case <-time.After(wait):
		p.t.Fatal("the client sent nothing")
		return message{}
	}
}

// send writes a message to the client.
func (p *peer) send(v any) {
	p.t.Helper()
	body, err := json.Marshal(v)
	if err != nil {
		p.t.Fatal(err)
	}
	if err := WriteFrame(p.rwc, body); err != nil {
		p.t.Fatalf("peer write: %v", err)
	}
}

func (p *peer) respond(m message, result any) {
	p.t.Helper()
	p.send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(m.ID), "result": result})
}

func (p *peer) fail(m message, code int, msg string) {
	p.t.Helper()
	p.send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(m.ID), "error": map[string]any{"code": code, "message": msg, "data": map[string]int{"x": 1}}})
}

func (p *peer) request(id any, method string, params any) {
	p.t.Helper()
	p.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
}

func (p *peer) notify(method string, params any) {
	p.t.Helper()
	p.send(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

// callAsync runs a call on a goroutine and returns the channel of its error.
func callAsync(ctx context.Context, c *Conn, method string, params, result any) <-chan error {
	ch := make(chan error, 1)
	go func() { ch <- c.Call(ctx, method, params, result) }()
	return ch
}

func get(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(wait):
		t.Fatal("the call did not return")
		return nil
	}
}

// wantRoute fails the test unless err is an *errors.Error whose route contains route.
func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	_ = wantRouteError(t, err, route)
}

// wantRouteError is wantRoute and gives the error back, for its metadata.
func wantRouteError(t *testing.T, err error, route string) *errors.Error {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("route = %s, want it to contain %s", appErr.Route(), route)
	}
	return appErr
}

func TestCallMatchesResponse(t *testing.T) {
	c, p := connWithPeer(t, ConnOptions{})
	var got struct{ Name string }
	ch := callAsync(context.Background(), c, "test/echo", map[string]string{"a": "b"}, &got)
	m := p.read()
	if m.Method != "test/echo" || string(m.Params) != `{"a":"b"}` || string(m.ID) != "1" {
		t.Errorf("request = %+v (params %s)", m, m.Params)
	}
	p.respond(m, map[string]string{"Name": "x"})
	if err := get(t, ch); err != nil {
		t.Fatal(err)
	}
	if got.Name != "x" {
		t.Errorf("result = %+v", got)
	}
}

func TestCallsAnsweredOutOfOrder(t *testing.T) {
	c, p := connWithPeer(t, ConnOptions{})
	var a, b int
	chA := callAsync(context.Background(), c, "test/a", nil, &a)
	first := p.read()
	chB := callAsync(context.Background(), c, "test/b", nil, &b)
	second := p.read()
	if first.ID == nil || string(first.ID) == string(second.ID) {
		t.Fatalf("ids = %s, %s", first.ID, second.ID)
	}
	p.respond(second, 2)
	p.respond(first, 1)
	if err := get(t, chA); err != nil {
		t.Fatal(err)
	}
	if err := get(t, chB); err != nil {
		t.Fatal(err)
	}
	if a != 1 || b != 2 {
		t.Errorf("a = %d, b = %d, want 1 and 2", a, b)
	}
}

func TestCallNullAndNilResult(t *testing.T) {
	c, p := connWithPeer(t, ConnOptions{})
	got := 7
	ch := callAsync(context.Background(), c, "test/null", nil, &got)
	p.respond(p.read(), nil)
	if err := get(t, ch); err != nil {
		t.Fatal(err)
	}
	if got != 7 {
		t.Errorf("a null result changed the value to %d", got)
	}
	ch = callAsync(context.Background(), c, "test/ignored", nil, nil)
	p.respond(p.read(), 5)
	if err := get(t, ch); err != nil {
		t.Fatal(err)
	}
}

func TestCallServerError(t *testing.T) {
	c, p := connWithPeer(t, ConnOptions{})
	ch := callAsync(context.Background(), c, "test/fail", nil, nil)
	p.fail(p.read(), CodeContentModified, "content modified")
	appErr := wantRouteError(t, get(t, ch), "Conn.Call.ServerError")
	if appErr.Metadata["code"] != CodeContentModified || appErr.Metadata["method"] != "test/fail" || appErr.Metadata["data"] != `{"x":1}` {
		t.Errorf("metadata = %v", appErr.Metadata)
	}
	if appErr.Message != "content modified" {
		t.Errorf("message = %q", appErr.Message)
	}
	// An error response without data.
	ch = callAsync(context.Background(), c, "test/fail2", nil, nil)
	m := p.read()
	p.send(map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(m.ID), "error": map[string]any{"code": CodeRequestFailed, "message": "no"}})
	appErr = wantRouteError(t, get(t, ch), "Conn.Call.ServerError")
	if _, ok := appErr.Metadata["data"]; ok {
		t.Errorf("metadata has data: %v", appErr.Metadata)
	}
}

func TestCallResultInvalid(t *testing.T) {
	c, p := connWithPeer(t, ConnOptions{})
	var n int
	ch := callAsync(context.Background(), c, "test/bad", nil, &n)
	p.respond(p.read(), "text")
	wantRoute(t, get(t, ch), "Conn.Call.ResultInvalid")

	// A type of ours that reads several shapes brings its own tag.
	var h Hover
	ch = callAsync(context.Background(), c, "test/hover", nil, &h)
	p.respond(p.read(), map[string]any{"contents": 5})
	err := get(t, ch)
	appErr, ok := errors.FromError(err)
	if !ok || appErr.Tag != "HoverInvalid" {
		t.Errorf("err = %v, want the tag HoverInvalid", err)
	}
}

func TestCallParamsInvalid(t *testing.T) {
	c, _ := connWithPeer(t, ConnOptions{})
	tests := []struct {
		name   string
		params any
		route  string
	}{
		{"a value that cannot be encoded", make(chan int), "Conn.Call.ParamsInvalid"},
		{"raw JSON that is not JSON", json.RawMessage("{"), "Conn.Call.ParamsInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantRoute(t, c.Call(context.Background(), "test/x", tt.params, nil), tt.route)
		})
	}
	wantRoute(t, c.Notify("test/x", make(chan int)), "Conn.Notify.ParamsInvalid")
	// Nothing was sent: the next call is the first message.
}

func TestCancellationSendsCancelRequest(t *testing.T) {
	c, p := connWithPeer(t, ConnOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	ch := callAsync(ctx, c, "test/slow", nil, nil)
	m := p.read()
	cancel()
	n := p.read()
	if n.Method != "$/cancelRequest" || string(n.Params) != `{"id":`+string(m.ID)+`}` || n.ID != nil {
		t.Errorf("after cancel the client sent %+v params %s, want $/cancelRequest for %s", n, n.Params, m.ID)
	}
	err := get(t, ch)
	wantRoute(t, err, "Conn.Call.RequestCancelled")
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the error does not wrap context.Canceled: %v", err)
	}

	// The late response is dropped; the connection works on.
	p.respond(m, "late")
	ch = callAsync(context.Background(), c, "test/next", nil, nil)
	next := p.read()
	if next.Method != "test/next" {
		t.Fatalf("got %+v", next)
	}
	p.respond(next, nil)
	if err := get(t, ch); err != nil {
		t.Fatal(err)
	}
}

func TestCancelledBeforeSending(t *testing.T) {
	c, p := connWithPeer(t, ConnOptions{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := c.Call(ctx, "test/never", nil, nil)
	wantRoute(t, err, "Conn.Call.RequestCancelled")
	// Nothing was sent: the first message of the client is the next call.
	ch := callAsync(context.Background(), c, "test/first", nil, nil)
	if m := p.read(); m.Method != "test/first" {
		t.Fatalf("the client sent %q first", m.Method)
	} else {
		p.respond(m, nil)
	}
	if err := get(t, ch); err != nil {
		t.Fatal(err)
	}
}

func TestDeadlineEndsTheCall(t *testing.T) {
	c, p := connWithPeer(t, ConnOptions{})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	ch := callAsync(ctx, c, "test/slow", nil, nil)
	p.read()
	err := get(t, ch)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want the deadline", err)
	}
	if m := p.read(); m.Method != "$/cancelRequest" {
		t.Errorf("the client sent %q", m.Method)
	}
}

func TestServerRequestsAreAnswered(t *testing.T) {
	boom := errors.New("Boom", "a plain failure")
	c, p := connWithPeer(t, ConnOptions{HandleRequest: func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		switch method {
		case "ok":
			return map[string]string{"echo": string(params)}, nil
		case "null":
			return nil, nil
		case "notfound":
			return nil, errors.New("MethodNotFound", "no such method")
		case "params":
			return nil, errors.New("InvalidParams", "bad params")
		case "coded":
			return nil, errors.New("Anything", "refused", "code", CodeRequestFailed)
		case "plain":
			return nil, boom
		case "cancelled":
			return nil, context.Canceled
		case "unencodable":
			return make(chan int), nil
		}
		return nil, errors.New("MethodNotFound", "no such method")
	}})
	_ = c
	tests := []struct {
		id      any
		method  string
		result  string
		code    int
		message string
	}{
		{1, "ok", `{"echo":"[1,2]"}`, 0, ""},
		{"abc", "null", `null`, 0, ""},
		{2, "notfound", "", CodeMethodNotFound, "no such method"},
		{3, "params", "", CodeInvalidParams, "bad params"},
		{4, "coded", "", CodeRequestFailed, "refused"},
		{5, "plain", "", CodeInternalError, "a plain failure"},
		{6, "cancelled", "", CodeRequestCancelled, "context canceled"},
		{7, "unencodable", "", CodeInternalError, ""},
	}
	for _, tt := range tests {
		t.Run(tt.method, func(t *testing.T) {
			p.request(tt.id, tt.method, []int{1, 2})
			m := p.read()
			wantID, err := json.Marshal(tt.id)
			if err != nil {
				t.Fatal(err)
			}
			if string(m.ID) != string(wantID) || m.Method != "" {
				t.Fatalf("response = %+v, want the id %s", m, wantID)
			}
			if tt.code == 0 {
				if m.Error != nil || string(m.Result) != tt.result {
					t.Errorf("result = %s error = %+v, want %s", m.Result, m.Error, tt.result)
				}
				return
			}
			if m.Error == nil || m.Error.Code != tt.code || (tt.message != "" && m.Error.Message != tt.message) {
				t.Errorf("error = %+v, want code %d %q", m.Error, tt.code, tt.message)
			}
		})
	}
}

func TestServerRequestWithoutHandler(t *testing.T) {
	_, p := connWithPeer(t, ConnOptions{})
	p.request(1, "workspace/anything", nil)
	m := p.read()
	if m.Error == nil || m.Error.Code != CodeMethodNotFound {
		t.Errorf("response = %+v", m)
	}
}

func TestServerRequestsRunOffTheReader(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	c, p := connWithPeer(t, ConnOptions{HandleRequest: func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return "done", nil
	}})
	p.request(1, "test/block", nil)
	<-started
	// While the handler waits, the reader still delivers the response to a call.
	ch := callAsync(context.Background(), c, "test/other", nil, nil)
	m := p.read()
	if m.Method != "test/other" {
		t.Fatalf("got %+v", m)
	}
	p.respond(m, nil)
	if err := get(t, ch); err != nil {
		t.Fatal(err)
	}
	close(release)
	if r := p.read(); string(r.ID) != "1" || string(r.Result) != `"done"` {
		t.Errorf("response = %+v", r)
	}
}

func TestServerCancelsItsRequest(t *testing.T) {
	cancelled := make(chan struct{})
	started := make(chan struct{})
	_, p := connWithPeer(t, ConnOptions{HandleRequest: func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		close(started)
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	}})
	p.request("r1", "test/long", nil)
	<-started
	p.notify("$/cancelRequest", map[string]string{"id": "r1"})
	select {
	case <-cancelled:
	case <-time.After(wait):
		t.Fatal("the handler's context was not cancelled")
	}
	if m := p.read(); m.Error == nil || m.Error.Code != CodeRequestCancelled {
		t.Errorf("response = %+v", m)
	}
}

func TestNotificationsArriveInOrderOffTheReader(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	gate := make(chan struct{})
	c, p := connWithPeer(t, ConnOptions{HandleNotification: func(method string, params json.RawMessage) {
		if method == "n/0" {
			<-gate // a slow handler does not stop the reader
		}
		mu.Lock()
		seen = append(seen, method)
		mu.Unlock()
	}})
	for i := 0; i < 5; i++ {
		p.notify("n/"+strconv.Itoa(i), nil)
	}
	ch := callAsync(context.Background(), c, "test/while-blocked", nil, nil)
	m := p.read()
	p.respond(m, nil)
	if err := get(t, ch); err != nil {
		t.Fatal(err)
	}
	close(gate)
	deadline := time.Now().Add(wait)
	for {
		mu.Lock()
		n := len(seen)
		mu.Unlock()
		if n == 5 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	for i, m := range seen {
		if m != "n/"+strconv.Itoa(i) {
			t.Fatalf("notifications = %v, want them in order", seen)
		}
	}
	if len(seen) != 5 {
		t.Fatalf("notifications = %v", seen)
	}
}

func TestServerDiesMidRequest(t *testing.T) {
	c, p := connWithPeer(t, ConnOptions{})
	ch := callAsync(context.Background(), c, "test/die", nil, nil)
	p.read()
	if err := p.rwc.Close(); err != nil {
		t.Fatal(err)
	}
	err := get(t, ch)
	appErr := wantRouteError(t, err, "Conn.Call.ServerClosed")
	if appErr.Metadata["cause"] != io.EOF.Error() {
		t.Errorf("cause = %v, want EOF", appErr.Metadata["cause"])
	}
	select {
	case <-c.Done():
	case <-time.After(wait):
		t.Fatal("Done is not closed")
	}
	if !errors.Is(c.Err(), io.EOF) {
		t.Errorf("Err = %v, want EOF", c.Err())
	}
	// Later calls and notifications fail at once.
	wantRoute(t, c.Call(context.Background(), "test/after", nil, nil), "Conn.Call.ServerClosed")
	wantRoute(t, c.Notify("test/after", nil), "Conn.Notify.NotifyClosed")
}

func TestBrokenFramingEndsTheConnection(t *testing.T) {
	log := &logRecorder{}
	c, p := connWithPeer(t, ConnOptions{Logger: slog.New(log)})
	ch := callAsync(context.Background(), c, "test/x", nil, nil)
	p.read()
	if _, err := p.rwc.Write([]byte("Content-Length: nope\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	err := get(t, ch)
	wantRoute(t, err, "Conn.Call.ServerClosed")
	wantRoute(t, c.Err(), "ReadFrame.FrameLengthInvalid")
	found := false
	for _, e := range log.errors() {
		if a, ok := errors.FromError(e); ok && a.Tag == "FrameLengthInvalid" {
			found = true
		}
	}
	if !found {
		t.Errorf("the broken connection was not logged")
	}
}

func TestMessageOverTheLimitEndsTheConnection(t *testing.T) {
	c, p := connWithPeer(t, ConnOptions{MaxMessageSize: 100})
	body := make([]byte, 101)
	for i := range body {
		body[i] = ' '
	}
	if err := WriteFrame(p.rwc, body); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.Done():
	case <-time.After(wait):
		t.Fatal("the connection did not end")
	}
	wantRoute(t, c.Err(), "ReadFrame.FrameTooLarge")
}

func TestInvalidMessagesAreLoggedAndSkipped(t *testing.T) {
	log := &logRecorder{}
	c, p := connWithPeer(t, ConnOptions{Logger: slog.New(log)})
	if err := WriteFrame(p.rwc, []byte("this is not json")); err != nil {
		t.Fatal(err)
	}
	if err := WriteFrame(p.rwc, []byte(`{"jsonrpc":"2.0"}`)); err != nil {
		t.Fatal(err)
	}
	p.send(map[string]any{"jsonrpc": "2.0", "id": 99, "result": 1}) // a response nobody waits for
	// The connection still works.
	ch := callAsync(context.Background(), c, "test/ok", nil, nil)
	p.respond(p.read(), nil)
	if err := get(t, ch); err != nil {
		t.Fatal(err)
	}
	var tags []string
	for _, e := range log.errors() {
		if a, ok := errors.FromError(e); ok {
			wantRoute(t, a, "Conn.dispatch.MessageInvalid")
			tags = append(tags, a.Tag)
		}
	}
	if len(tags) != 2 {
		t.Errorf("logged tags = %v, want two MessageInvalid", tags)
	}
}

func TestInvalidCancelRequestFromServer(t *testing.T) {
	log := &logRecorder{}
	c, p := connWithPeer(t, ConnOptions{Logger: slog.New(log)})
	p.notify("$/cancelRequest", "not an object")
	ch := callAsync(context.Background(), c, "test/ok", nil, nil)
	p.respond(p.read(), nil)
	if err := get(t, ch); err != nil {
		t.Fatal(err)
	}
	errs := log.errors()
	if len(errs) != 1 {
		t.Fatalf("logged errors = %v", errs)
	}
	wantRoute(t, errs[0], "Conn.cancelInflight.MessageInvalid")
}

// brokenRWC fails every write and close.
type brokenRWC struct{ closed chan struct{} }

func (b brokenRWC) Read([]byte) (int, error) {
	<-b.closed
	return 0, io.EOF
}
func (b brokenRWC) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func (b brokenRWC) Close() error {
	close(b.closed)
	return io.ErrShortBuffer
}

func TestWriteFailureEndsTheConnection(t *testing.T) {
	c := NewConn(brokenRWC{closed: make(chan struct{})}, ConnOptions{})
	err := c.Call(context.Background(), "test/x", nil, nil)
	wantRoute(t, err, "Conn.Call.ServerClosed")
	wantRoute(t, c.Err(), "WriteFrame.FrameWriteFailed")
	c.Wait()
}

func TestCloseReportsTheTransportError(t *testing.T) {
	c := NewConn(brokenRWC{closed: make(chan struct{})}, ConnOptions{})
	err := c.Close()
	wantRoute(t, err, "Conn.Close.TransportCloseFailed")
	if !errors.Is(err, io.ErrShortBuffer) {
		t.Errorf("the transport's error is lost: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Errorf("the second Close = %v, want nil", err)
	}
	c.Wait()
}

func TestFlush(t *testing.T) {
	c, p := connWithPeer(t, ConnOptions{})
	if err := c.Notify("test/queued", nil); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- c.Flush(context.Background()) }()
	if m := p.read(); m.Method != "test/queued" {
		t.Fatalf("got %+v", m)
	}
	if err := get(t, done); err != nil {
		t.Fatal(err)
	}
}

func TestFlushAborted(t *testing.T) {
	t.Run("the context ends while the stream does not read", func(t *testing.T) {
		c, _ := connWithPeer(t, ConnOptions{})
		if err := c.Notify("test/stuck", nil); err != nil { // nobody reads, so the write blocks
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		appErr := wantRouteError(t, c.Flush(ctx), "Conn.Flush.FlushAborted")
		if appErr.Metadata["reason"] != context.DeadlineExceeded.Error() {
			t.Errorf("reason = %v", appErr.Metadata["reason"])
		}
	})
	t.Run("the connection ends", func(t *testing.T) {
		c, _ := connWithPeer(t, ConnOptions{})
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		wantRoute(t, c.Flush(context.Background()), "Conn.Flush.FlushAborted")
	})
}

func TestTrace(t *testing.T) {
	var mu sync.Mutex
	var got []TraceMessage
	c, p := connWithPeer(t, ConnOptions{Trace: func(m TraceMessage) {
		mu.Lock()
		got = append(got, m)
		mu.Unlock()
	}, HandleRequest: func(context.Context, string, json.RawMessage) (any, error) { return "r", nil }})
	ch := callAsync(context.Background(), c, "test/req", nil, nil)
	m := p.read()
	p.respond(m, 1)
	if err := get(t, ch); err != nil {
		t.Fatal(err)
	}
	if err := c.Notify("test/note", nil); err != nil {
		t.Fatal(err)
	}
	p.read()
	p.notify("test/fromserver", nil)
	p.request(9, "test/srvreq", nil)
	p.read()
	// the notification may still be in flight; wait for all five
	deadline := time.Now().Add(wait)
	for {
		mu.Lock()
		n := len(got)
		mu.Unlock()
		if n >= 5 || time.Now().After(deadline) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	type key struct {
		dir    Direction
		kind   MessageKind
		method string
	}
	want := map[key]bool{
		{ToServer, KindRequest, "test/req"}:               false,
		{FromServer, KindResponse, "test/req"}:            false,
		{ToServer, KindNotification, "test/note"}:         false,
		{FromServer, KindNotification, "test/fromserver"}: false,
		{FromServer, KindRequest, "test/srvreq"}:          false,
	}
	for _, m := range got {
		k := key{m.Direction, m.Kind, m.Method}
		if _, ok := want[k]; ok {
			want[k] = true
			if len(m.Body) == 0 || m.Time.IsZero() {
				t.Errorf("trace %+v has no body or time", k)
			}
			if m.Kind == KindResponse && (m.ID != "1" || m.Elapsed <= 0) {
				t.Errorf("response trace = %+v", m)
			}
		}
	}
	for k, seen := range want {
		if !seen {
			t.Errorf("no trace for %+v; got %d messages", k, len(got))
		}
	}
}

func TestResponseError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want int
	}{
		{"plain", errors.New("X", "x"), CodeInternalError},
		{"method not found", errors.New("MethodNotFound", "x"), CodeMethodNotFound},
		{"invalid params", errors.New("InvalidParams", "x"), CodeInvalidParams},
		{"code", errors.New("X", "x", "code", CodeServerCancelled), CodeServerCancelled},
		{"cancelled", context.Canceled, CodeRequestCancelled},
		{"not ours", io.ErrUnexpectedEOF, CodeInternalError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := responseError(tt.err); got.Code != tt.want || got.Message != tt.err.Error() {
				t.Errorf("responseError = %+v, want code %d", got, tt.want)
			}
		})
	}
}
