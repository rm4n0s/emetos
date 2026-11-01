package dap_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/errors"
)

// forEachTransport runs the test over every transport.
func forEachTransport(t *testing.T, f func(t *testing.T, tr daptest.Transport)) {
	t.Helper()
	for _, tt := range transports {
		t.Run(tt.name, func(t *testing.T) { f(t, tt.tr) })
	}
}

func noViolations(t *testing.T, h *daptest.Harness) {
	t.Helper()
	if v := h.Violations(); len(v) > 0 {
		t.Errorf("the fake adapter noticed violations: %q", v)
	}
}

// nextEvent takes the next event of the client or fails the test.
func nextEvent(t *testing.T, c *dap.Client) dap.Event {
	t.Helper()
	select {
	case ev, ok := <-c.Events():
		if !ok {
			t.Fatal("the event stream ended")
		}
		return ev
	case <-time.After(wait):
		t.Fatal("no event")
		return dap.Event{}
	}
}

func initArgs() dap.InitializeRequestArguments {
	return dap.InitializeRequestArguments{ClientID: "emetos", AdapterID: "fake", LinesStartAt1: true, ColumnsStartAt1: true, PathFormat: "path", SupportsRunInTerminalRequest: true}
}

func TestSessionSequence(t *testing.T) {
	forEachTransport(t, func(t *testing.T, tr daptest.Transport) {
		h := daptest.Start(t, daptest.Options{Transport: tr, Script: daptest.Script{
			daptest.ReplyArgs("initialize", map[string]any{"adapterID": "fake", "linesStartAt1": true, "pathFormat": "path"}, dap.Capabilities{SupportsConfigurationDoneRequest: true, SupportsSetVariable: true}),
			daptest.Event(dap.EventInitialized, nil),
			daptest.ReplyArgs("launch", map[string]any{"program": "/p"}, nil),
			daptest.ReplyArgs("setBreakpoints", map[string]any{"source": map[string]any{"path": "/a.go"}, "breakpoints": []any{map[string]any{"line": 3}}},
				dap.BreakpointsResponseBody{Breakpoints: []dap.Breakpoint{{ID: 1, Verified: true, Line: 4}}}),
			daptest.Reply("configurationDone", nil),
			daptest.Event(dap.EventStopped, dap.StoppedEventBody{Reason: dap.StopBreakpoint, ThreadID: 1, AllThreadsStopped: true}),
			daptest.Reply("threads", dap.ThreadsResponseBody{Threads: []dap.Thread{{ID: 1, Name: "main"}}}),
			daptest.Reply("disconnect", nil),
			daptest.Event(dap.EventTerminated, nil),
		}})
		c, ctx := h.Client, ctxT(t)

		caps, err := dap.Call[dap.Capabilities](ctx, c, dap.CmdInitialize, initArgs())
		if err != nil {
			t.Fatal(err)
		}
		if !caps.SupportsConfigurationDoneRequest || !c.Capabilities().SupportsSetVariable {
			t.Errorf("capabilities = %+v / %+v", caps, c.Capabilities())
		}
		if ev := nextEvent(t, c); ev.Event != dap.EventInitialized {
			t.Errorf("first event = %s", ev.Event)
		}
		if _, err := c.Request(ctx, dap.CmdLaunch, map[string]any{"program": "/p", "noDebug": false}); err != nil {
			t.Fatal(err)
		}
		bps, err := dap.Call[dap.BreakpointsResponseBody](ctx, c, dap.CmdSetBreakpoints, dap.SetBreakpointsArguments{Source: dap.Source{Path: "/a.go"}, Breakpoints: []dap.SourceBreakpoint{{Line: 3}}})
		if err != nil || len(bps.Breakpoints) != 1 || bps.Breakpoints[0].Line != 4 || !bps.Breakpoints[0].Verified {
			t.Fatalf("setBreakpoints = %+v, %v", bps, err)
		}
		if _, err := c.Request(ctx, dap.CmdConfigurationDone, nil); err != nil {
			t.Fatal(err)
		}
		ev := nextEvent(t, c)
		body, err := dap.ParseEvent(ev)
		if err != nil {
			t.Fatal(err)
		}
		if st, ok := body.(*dap.StoppedEventBody); !ok || st.Reason != "breakpoint" || st.ThreadID != 1 || !st.AllThreadsStopped {
			t.Errorf("stopped = %#v", body)
		}
		threads, err := dap.Call[dap.ThreadsResponseBody](ctx, c, dap.CmdThreads, nil)
		if err != nil || len(threads.Threads) != 1 || threads.Threads[0].Name != "main" {
			t.Fatalf("threads = %+v, %v", threads, err)
		}
		if _, err := c.Request(ctx, dap.CmdDisconnect, dap.DisconnectArguments{}); err != nil {
			t.Fatal(err)
		}
		if ev := nextEvent(t, c); ev.Event != dap.EventTerminated {
			t.Errorf("last event = %s", ev.Event)
		}
		noViolations(t, h)
		// every message of the client and the adapter was numbered 1, 2, 3, ... by its sender
		last := map[string]int{}
		for _, m := range h.Traffic() {
			if m.Seq != last[m.Direction]+1 {
				t.Errorf("%s message %s %s has seq %d after %d", m.Direction, m.Type, m.Name, m.Seq, last[m.Direction])
			}
			last[m.Direction] = m.Seq
		}
	})
}

func TestResponsesInAnyOrderWithEventsBetween(t *testing.T) {
	forEachTransport(t, func(t *testing.T, tr daptest.Transport) {
		h := daptest.Start(t, daptest.Options{Transport: tr, Script: daptest.Script{
			daptest.Collect("threads", "scopes", "evaluate"),
			daptest.Respond("evaluate", dap.EvaluateResponseBody{Result: "3"}),
			daptest.Event(dap.EventOutput, dap.OutputEventBody{Output: "one"}),
			daptest.Respond("threads", dap.ThreadsResponseBody{Threads: []dap.Thread{{ID: 7, Name: "t"}}}),
			daptest.Event(dap.EventOutput, dap.OutputEventBody{Output: "two"}),
			daptest.Respond("scopes", dap.ScopesResponseBody{Scopes: []dap.Scope{{Name: "Locals", VariablesReference: 9}}}),
		}})
		c, ctx := h.Client, ctxT(t)
		var wg sync.WaitGroup
		results := make([]string, 3)
		call := func(i int, command string, args any) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				resp, err := c.Request(ctx, command, args)
				if err != nil {
					results[i] = "error: " + err.Error()
					return
				}
				results[i] = resp.Command + " " + string(resp.Body)
			}()
		}
		call(0, dap.CmdThreads, nil)
		call(1, dap.CmdScopes, dap.ScopesArguments{FrameID: 1})
		call(2, dap.CmdEvaluate, dap.EvaluateArguments{Expression: "1+2"})
		wg.Wait()
		want := []string{`threads {"threads":[{"id":7,"name":"t"}]}`, `scopes {"scopes":[{"name":"Locals","variablesReference":9,"expensive":false}]}`, `evaluate {"result":"3","variablesReference":0}`}
		for i := range want {
			if results[i] != want[i] {
				t.Errorf("result %d = %s, want %s", i, results[i], want[i])
			}
		}
		if got := nextEvent(t, c); !strings.Contains(string(got.Body), "one") {
			t.Errorf("first event = %s", got.Body)
		}
		if got := nextEvent(t, c); !strings.Contains(string(got.Body), "two") {
			t.Errorf("second event = %s", got.Body)
		}
		if c.Pending() != 0 {
			t.Errorf("%d requests still pending", c.Pending())
		}
		noViolations(t, h)
	})
}

func TestReverseRequests(t *testing.T) {
	forEachTransport(t, func(t *testing.T, tr daptest.Transport) {
		type call struct {
			command string
			args    string
		}
		var mu sync.Mutex
		var calls []call
		handler := func(_ context.Context, command string, args json.RawMessage) (any, error) {
			mu.Lock()
			calls = append(calls, call{command, string(args)})
			mu.Unlock()
			switch command {
			case dap.ReverseRunInTerminal:
				return dap.RunInTerminalResponseBody{ProcessID: 4242}, nil
			case dap.ReverseStartDebugging:
				return nil, errors.New("ChildRefused", "no room for a child session")
			}
			return nil, errors.New("Unexpected", "unexpected")
		}
		h := daptest.Start(t, daptest.Options{Transport: tr, Client: dap.ClientOptions{HandleRequest: handler}, Script: daptest.Script{
			daptest.Request("rt", dap.ReverseRunInTerminal, dap.RunInTerminalRequestArguments{Cwd: "/w", Args: []string{"prog", "-x"}, Title: "T"}),
			daptest.Await("rt"),
			daptest.Request("sd", dap.ReverseStartDebugging, map[string]any{"request": "launch", "configuration": map[string]any{"name": "child"}}),
			daptest.Await("sd"),
			daptest.Request("other", "somethingElse", nil),
			daptest.Await("other"),
			daptest.Reply("threads", dap.ThreadsResponseBody{}),
		}})
		// a request after the reverse ones proves that the adapter has them all in its traffic
		if _, err := h.Client.Request(ctxT(t), dap.CmdThreads, nil); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		defer mu.Unlock()
		if len(calls) != 3 || calls[0].command != "runInTerminal" || !strings.Contains(calls[0].args, `"prog","-x"`) || calls[1].command != "startDebugging" {
			t.Errorf("handler calls = %+v", calls)
		}
		var answers []daptest.Message
		for _, m := range h.Traffic() {
			if m.Direction == "in" && m.Type == dap.TypeResponse {
				answers = append(answers, m)
			}
		}
		if len(answers) != 3 {
			t.Fatalf("%d responses from the client, want 3: %+v", len(answers), answers)
		}
		if !answers[0].Success || answers[0].Name != "runInTerminal" || string(answers[0].Body) != `{"processId":4242}` {
			t.Errorf("runInTerminal answer = %+v", answers[0])
		}
		if answers[1].Success || answers[1].Name != "startDebugging" {
			t.Errorf("startDebugging answer = %+v", answers[1])
		}
		noViolations(t, h)
	})
}

func TestReverseRequestWithNoHandlerIsAnsweredWithAnError(t *testing.T) {
	h := daptest.Start(t, daptest.Options{Script: daptest.Script{
		daptest.Request("rt", dap.ReverseRunInTerminal, map[string]any{"cwd": "/", "args": []string{"x"}}),
		daptest.Await("rt"),
		daptest.Reply("threads", nil),
	}})
	if _, err := h.Client.Request(ctxT(t), dap.CmdThreads, nil); err != nil {
		t.Fatal(err)
	}
	for _, m := range h.Traffic() {
		if m.Direction == "in" && m.Type == dap.TypeResponse {
			if m.Success || m.Name != "runInTerminal" {
				t.Errorf("answer = %+v", m)
			}
			return
		}
	}
	t.Error("the client did not answer the request of the adapter")
}

func TestCancelledContextSendsCancelWhenSupported(t *testing.T) {
	forEachTransport(t, func(t *testing.T, tr daptest.Transport) {
		h := daptest.Start(t, daptest.Options{Transport: tr, Script: daptest.Script{
			daptest.Reply("initialize", dap.Capabilities{SupportsCancelRequest: true}),
			daptest.Expect("evaluate", nil),
			daptest.ReplyArgs("cancel", map[string]any{"requestId": 2}, nil),
			daptest.Respond("evaluate", dap.EvaluateResponseBody{Result: "too late"}),
			daptest.Reply("threads", dap.ThreadsResponseBody{Threads: []dap.Thread{{ID: 1, Name: "ok"}}}),
		}})
		c := h.Client
		if _, err := c.Request(ctxT(t), dap.CmdInitialize, initArgs()); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(ctxT(t))
		errc := make(chan error, 1)
		go func() {
			_, err := c.Request(ctx, dap.CmdEvaluate, dap.EvaluateArguments{Expression: "slow"})
			errc <- err
		}()
		for c.Pending() == 0 { // the request has been sent
			time.Sleep(time.Millisecond)
		}
		cancel()
		err := <-errc
		appErr := wantRouteError(t, err, "Client.Request.DapRequestCancelled")
		if !errors.Is(err, context.Canceled) {
			t.Errorf("the context's error is lost: %v", err)
		}
		if appErr.Metadata["command"] != "evaluate" || appErr.Metadata["seq"] != 2 {
			t.Errorf("metadata = %v", appErr.Metadata)
		}
		// the late response is dropped, and the next request gets its own answer
		threads, err := dap.Call[dap.ThreadsResponseBody](ctxT(t), c, dap.CmdThreads, nil)
		if err != nil || len(threads.Threads) != 1 || threads.Threads[0].Name != "ok" {
			t.Fatalf("threads = %+v, %v", threads, err)
		}
		if c.Pending() != 0 {
			t.Errorf("%d requests still pending", c.Pending())
		}
		noViolations(t, h)
	})
}

func TestCancelledContextSendsNoCancelWhenNotSupported(t *testing.T) {
	h := daptest.Start(t, daptest.Options{Script: daptest.Script{
		daptest.Reply("initialize", dap.Capabilities{}),
		daptest.Expect("evaluate", nil),
		daptest.Reply("threads", nil),
	}})
	c := h.Client
	if _, err := c.Request(ctxT(t), dap.CmdInitialize, initArgs()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(ctxT(t))
	errc := make(chan error, 1)
	go func() {
		_, err := c.Request(ctx, dap.CmdEvaluate, nil)
		errc <- err
	}()
	for c.Pending() == 0 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	wantRoute(t, <-errc, "Client.Request.DapRequestCancelled")
	if _, err := c.Request(ctxT(t), dap.CmdThreads, nil); err != nil {
		t.Fatal(err)
	}
	if got := h.Requests("cancel"); len(got) != 0 {
		t.Errorf("the client sent cancel to an adapter that does not support it: %s", got)
	}
}

func TestRequestWithACancelledContextIsNotSent(t *testing.T) {
	h := daptest.Start(t, daptest.Options{Script: daptest.Script{daptest.Reply("threads", nil)}})
	ctx, cancel := context.WithCancel(ctxT(t))
	cancel()
	_, err := h.Client.Request(ctx, dap.CmdScopes, nil)
	wantRoute(t, err, "Client.Request.DapRequestCancelled")
	dctx, dcancel := context.WithDeadline(ctxT(t), time.Now().Add(-time.Second))
	defer dcancel()
	_, err = h.Client.Request(dctx, dap.CmdScopes, nil)
	wantRoute(t, err, "Client.Request.DapTimeout")
	if _, err := h.Client.Request(ctxT(t), dap.CmdThreads, nil); err != nil {
		t.Fatal(err)
	}
	if got := h.Requests("scopes"); len(got) != 0 {
		t.Errorf("a request that was already cancelled reached the adapter: %s", got)
	}
}

func TestSlowAnswerTimesOut(t *testing.T) {
	forEachTransport(t, func(t *testing.T, tr daptest.Transport) {
		h := daptest.Start(t, daptest.Options{Transport: tr, Client: dap.ClientOptions{RequestTimeout: 100 * time.Millisecond}, Script: daptest.Script{
			daptest.Reply("evaluate", dap.EvaluateResponseBody{Result: "late"}).After(300),
			daptest.Reply("threads", dap.ThreadsResponseBody{Threads: []dap.Thread{{ID: 1}}}),
			daptest.Expect("scopes", nil),
		}})
		c := h.Client
		start := time.Now()
		_, err := c.Request(ctxT(t), dap.CmdEvaluate, nil)
		appErr := wantRouteError(t, err, "Client.Request.DapTimeout")
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("the deadline error is lost: %v", err)
		}
		if appErr.Metadata["command"] != "evaluate" {
			t.Errorf("metadata = %v", appErr.Metadata)
		}
		if d := time.Since(start); d > 350*time.Millisecond {
			t.Errorf("the timeout took %v", d)
		}
		// the late answer is dropped (the adapter is busy until it is sent); the next one is matched to its own request
		time.Sleep(300 * time.Millisecond)
		if _, err := c.Request(ctxT(t), dap.CmdThreads, nil); err != nil {
			t.Fatalf("threads after a timeout: %v", err)
		}
		// an adapter that never answers: the deadline of the context
		dctx, cancel := context.WithTimeout(ctxT(t), 80*time.Millisecond)
		defer cancel()
		_, err = c.Request(dctx, dap.CmdScopes, nil)
		wantRoute(t, err, "Client.Request.DapTimeout")
	})
}

func TestAdapterExitingInTheMiddleOfARequest(t *testing.T) {
	forEachTransport(t, func(t *testing.T, tr daptest.Transport) {
		h := daptest.Start(t, daptest.Options{Transport: tr, Script: daptest.Script{
			daptest.Event(dap.EventOutput, dap.OutputEventBody{Output: "before the crash"}),
			daptest.Expect("threads", nil),
			daptest.Crash(3),
		}})
		c := h.Client
		_, err := c.Request(ctxT(t), dap.CmdThreads, nil)
		appErr := wantRouteError(t, err, "Client.Request.DapAdapterExited")
		if appErr.Metadata["command"] != "threads" {
			t.Errorf("metadata = %v", appErr.Metadata)
		}
		if tr == daptest.Stdio || tr == daptest.Announce {
			if appErr.Metadata["exitCode"] != 3 {
				t.Errorf("exitCode = %v, want 3 (%v)", appErr.Metadata["exitCode"], appErr.Metadata)
			}
		}
		if h.PID() != 0 {
			if code := h.WaitExit(); code != 3 {
				t.Errorf("exit status = %d, want 3", code)
			}
		}
		select {
		case <-c.Done():
		case <-time.After(wait):
			t.Fatal("the client did not notice the end")
		}
		// the event that came before the crash is still delivered, then the stream ends
		if ev := nextEvent(t, c); ev.Event != dap.EventOutput {
			t.Errorf("event = %s", ev.Event)
		}
		if _, ok := <-c.Events(); ok {
			t.Error("the event stream is still open")
		}
		if c.Err() == nil {
			t.Error("Err is nil after the adapter died")
		}
		// a request after the end fails at once with the same tag
		_, err = c.Request(ctxT(t), dap.CmdThreads, nil)
		wantRoute(t, err, "Client.Request.DapAdapterExited")
	})
}

func TestGarbageEndsTheConnection(t *testing.T) {
	tests := []struct {
		name    string
		garbage daptest.Step
		reason  string
	}{
		{"text instead of a header", daptest.Raw("hello, I am not a debug adapter\r\n\r\n"), "header without colon"},
		{"a length that is not a number", daptest.Raw("Content-Length: lots\r\n\r\n"), "length invalid"},
		{"headers without a length", daptest.Raw("X-Other: 1\r\n\r\n{}"), "length missing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			forEachTransport(t, func(t *testing.T, tr daptest.Transport) {
				h := daptest.Start(t, daptest.Options{Transport: tr, Script: daptest.Script{
					daptest.Reply("initialize", dap.Capabilities{}),
					daptest.Expect("threads", nil),
					tt.garbage,
				}})
				c := h.Client
				if _, err := c.Request(ctxT(t), dap.CmdInitialize, initArgs()); err != nil {
					t.Fatal(err)
				}
				_, err := c.Request(ctxT(t), dap.CmdThreads, nil)
				appErr := wantRouteError(t, err, "ReadFrame.DapProtocolInvalid")
				if appErr.Metadata["reason"] != tt.reason {
					t.Errorf("reason = %v, want %q", appErr.Metadata["reason"], tt.reason)
				}
				if e := c.Err(); e == nil || !errors.Is(e, appErr) {
					t.Errorf("Err = %v, want the same error", e)
				}
			})
		})
	}
}

// captureHandler keeps the records a logger gets.
type captureHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	h.records = append(h.records, r)
	h.mu.Unlock()
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

// warnings returns the errors the warnings carried.
func (h *captureHandler) warnings() []*errors.Error {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []*errors.Error
	for _, r := range h.records {
		if r.Level != slog.LevelWarn {
			continue
		}
		r.Attrs(func(a slog.Attr) bool {
			if e, ok := a.Value.Any().(*errors.Error); ok {
				out = append(out, e)
			}
			return true
		})
	}
	return out
}

func TestInvalidMessagesAreLoggedAndSkipped(t *testing.T) {
	forEachTransport(t, func(t *testing.T, tr daptest.Transport) {
		logs := &captureHandler{}
		h := daptest.Start(t, daptest.Options{Transport: tr, Client: dap.ClientOptions{Logger: slog.New(logs)}, Script: daptest.Script{
			daptest.Expect("threads", nil),
			daptest.SendText("{this is not json"),
			daptest.Send(map[string]any{"seq": 5, "type": "banana"}),
			daptest.Send(map[string]any{"seq": 6, "type": "event"}),
			daptest.Respond("threads", dap.ThreadsResponseBody{Threads: []dap.Thread{{ID: 1, Name: "still works"}}}),
		}})
		threads, err := dap.Call[dap.ThreadsResponseBody](ctxT(t), h.Client, dap.CmdThreads, nil)
		if err != nil || threads.Threads[0].Name != "still works" {
			t.Fatalf("threads = %+v, %v", threads, err)
		}
		warned := logs.warnings()
		if len(warned) != 3 {
			t.Fatalf("%d warnings, want 3", len(warned))
		}
		for i, e := range warned {
			if e.Tag != "DapProtocolInvalid" || !e.HasRoute("decodeMessage.DapProtocolInvalid") {
				t.Errorf("warning %d: %s %s", i, e.Tag, e.Route())
			}
		}
		if h.Client.Err() != nil {
			t.Errorf("the connection ended: %v", h.Client.Err())
		}
	})
}

func TestMessageOverTheLimitEndsTheConnection(t *testing.T) {
	forEachTransport(t, func(t *testing.T, tr daptest.Transport) {
		h := daptest.Start(t, daptest.Options{Transport: tr, Client: dap.ClientOptions{MaxMessageSize: 1000}, Script: daptest.Script{
			daptest.Expect("threads", nil),
			daptest.Burst(1, 5000),
		}})
		_, err := h.Client.Request(ctxT(t), dap.CmdThreads, nil)
		appErr := wantRouteError(t, err, "ReadFrame.DapMessageTooLarge")
		if appErr.Metadata["max"] != 1000 {
			t.Errorf("metadata = %v", appErr.Metadata)
		}
	})
}

func TestFailedResponses(t *testing.T) {
	h := daptest.Start(t, daptest.Options{Script: daptest.Script{
		daptest.ReplyError("evaluate", "short form", dap.ErrorBody{Error: &dap.Message{ID: 2, Format: "cannot evaluate {expr}: {why}", Variables: map[string]string{"expr": "x.y", "why": "not found"}, ShowUser: true, URL: "https://example.com/help"}}),
		daptest.ReplyError("scopes", "no frame", nil),
		daptest.ReplyError("threads", "", nil),
		daptest.ReplyError("pause", dap.MessageNotStopped, map[string]any{"error": "not a message"}),
	}})
	c, ctx := h.Client, ctxT(t)

	resp, err := c.Request(ctx, dap.CmdEvaluate, nil)
	appErr := wantRouteError(t, err, "Client.Request.DapResponseFailed")
	if appErr.Message != "cannot evaluate x.y: not found" {
		t.Errorf("message = %q", appErr.Message)
	}
	if appErr.Metadata["reason"] != "short form" || appErr.Metadata["showUser"] != true || appErr.Metadata["url"] != "https://example.com/help" || appErr.Metadata["errorId"] != 2 || appErr.Metadata["command"] != "evaluate" {
		t.Errorf("metadata = %v", appErr.Metadata)
	}
	if resp.Success || resp.Message != "short form" || !strings.Contains(string(resp.Body), "cannot evaluate") {
		t.Errorf("the failed response is returned with the error: %+v", resp)
	}

	_, err = c.Request(ctx, dap.CmdScopes, nil)
	if appErr = wantRouteError(t, err, "Client.Request.DapResponseFailed"); appErr.Message != "no frame" {
		t.Errorf("message = %q", appErr.Message)
	}
	_, err = c.Request(ctx, dap.CmdThreads, nil)
	if appErr = wantRouteError(t, err, "Client.Request.DapResponseFailed"); !strings.Contains(appErr.Message, "threads") {
		t.Errorf("a response with no message still says which command failed: %q", appErr.Message)
	}
	_, err = c.Request(ctx, dap.CmdPause, nil)
	if appErr = wantRouteError(t, err, "Client.Request.DapResponseFailed"); appErr.Message != "notStopped" || appErr.Metadata["reason"] != dap.MessageNotStopped {
		t.Errorf("a body that is no error body is ignored: %q %v", appErr.Message, appErr.Metadata)
	}
	noViolations(t, h)
}

func TestCloseFailsThePendingRequest(t *testing.T) {
	forEachTransport(t, func(t *testing.T, tr daptest.Transport) {
		h := daptest.Start(t, daptest.Options{Transport: tr, Script: daptest.Script{daptest.Expect("threads", nil), daptest.WaitClose()}})
		c := h.Client
		errc := make(chan error, 1)
		go func() {
			_, err := c.Request(ctxT(t), dap.CmdThreads, nil)
			errc <- err
		}()
		for c.Pending() == 0 {
			time.Sleep(time.Millisecond)
		}
		if err := c.Close(); err != nil {
			t.Fatal(err)
		}
		wantRoute(t, <-errc, "Client.Request.DapClientClosed")
		if err := c.Close(); err != nil {
			t.Errorf("closing twice: %v", err)
		}
		_, err := c.Request(ctxT(t), dap.CmdThreads, nil)
		wantRoute(t, err, "Client.Request.DapClientClosed")
		if c.Err() != nil {
			t.Errorf("Err after a Close = %v, want nil", c.Err())
		}
		c.Wait()
		if _, ok := <-c.Events(); ok {
			t.Error("the event stream is still open")
		}
		if h.PID() != 0 {
			h.WaitExit() // closing the client ends the program
		}
	})
}

func TestCapabilitiesEventMergesIntoCapabilities(t *testing.T) {
	h := daptest.Start(t, daptest.Options{Script: daptest.Script{
		daptest.Reply("initialize", dap.Capabilities{SupportsConfigurationDoneRequest: true, SupportsSetVariable: true, SupportsRestartRequest: true}),
		daptest.Event(dap.EventCapabilities, dap.CapabilitiesEventBody{Capabilities: dap.Capabilities{SupportsStepBack: true}}),
		daptest.Event(dap.EventCapabilities, map[string]any{"capabilities": map[string]any{"supportsSetVariable": false}}),
		daptest.Event(dap.EventInitialized, nil),
	}})
	c := h.Client
	if got := c.Capabilities(); got.SupportsSetVariable {
		t.Errorf("capabilities before initialize: %+v", got)
	}
	if _, err := c.Request(ctxT(t), dap.CmdInitialize, initArgs()); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{dap.EventCapabilities, dap.EventCapabilities, dap.EventInitialized} {
		if ev := nextEvent(t, c); ev.Event != name {
			t.Fatalf("event = %s, want %s", ev.Event, name)
		}
	}
	got := c.Capabilities()
	if !got.SupportsConfigurationDoneRequest || !got.SupportsRestartRequest || !got.SupportsStepBack || got.SupportsSetVariable {
		t.Errorf("merged capabilities = %+v", got)
	}
}

func TestTraceSeesEveryMessageInOrder(t *testing.T) {
	var mu sync.Mutex
	var traced []dap.TraceMessage
	h := daptest.Start(t, daptest.Options{Client: dap.ClientOptions{Trace: func(m dap.TraceMessage) {
		mu.Lock()
		traced = append(traced, m)
		mu.Unlock()
	}}, Script: daptest.Script{
		daptest.Reply("threads", dap.ThreadsResponseBody{}),
		daptest.Event(dap.EventThread, dap.ThreadEventBody{Reason: "started", ThreadID: 2}),
		daptest.Request("rt", "ping", nil),
		daptest.Await("rt"),
		daptest.Reply("scopes", nil),
	}})
	c := h.Client
	if _, err := c.Request(ctxT(t), dap.CmdThreads, nil); err != nil {
		t.Fatal(err)
	}
	nextEvent(t, c)
	for { // the answer to ping goes out on its own goroutine; scopes waits for it so the order is fixed
		mu.Lock()
		n := len(traced)
		done := n > 0 && traced[n-1].Name == "ping" && traced[n-1].Direction == dap.ToAdapter
		mu.Unlock()
		if done {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := c.Request(ctxT(t), dap.CmdScopes, nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	var got []string
	for _, m := range traced {
		d := "->"
		if m.Direction == dap.FromAdapter {
			d = "<-"
		}
		got = append(got, d+m.Type+":"+m.Name)
		if len(m.Body) == 0 || m.Time.IsZero() {
			t.Errorf("trace %v has no body or time", m)
		}
		if m.Type == dap.TypeResponse && m.Direction == dap.FromAdapter && m.Name == "threads" && m.Elapsed <= 0 {
			t.Errorf("the response to threads has no elapsed time")
		}
	}
	// the response to ping (ours) is written after the adapter's request was read
	want := []string{"->request:threads", "<-response:threads", "<-event:thread", "<-request:ping", "->response:ping", "->request:scopes", "<-response:scopes"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("trace =\n%v\nwant\n%v", got, want)
	}
}

func TestArgumentsThatCannotBeSent(t *testing.T) {
	h := daptest.Start(t, daptest.Options{Script: daptest.Script{daptest.Reply("threads", nil)}})
	c, ctx := h.Client, ctxT(t)
	_, err := c.Request(ctx, dap.CmdEvaluate, make(chan int))
	wantRoute(t, err, "Client.Request.DapArgumentsInvalid")
	_, err = c.Request(ctx, dap.CmdEvaluate, json.RawMessage(`{"expression": `))
	wantRoute(t, err, "Client.Request.DapArgumentsInvalid")
	if _, err := c.Request(ctx, dap.CmdThreads, json.RawMessage(nil)); err != nil {
		t.Errorf("no arguments as an empty raw message: %v", err)
	}
	if got := h.Requests("evaluate"); len(got) != 0 {
		t.Errorf("a broken request reached the adapter: %s", got)
	}
	if v := h.Violations(); len(v) != 0 {
		t.Errorf("a broken request used a sequence number: %q", v)
	}
}

func TestCallBodyThatDoesNotFit(t *testing.T) {
	h := daptest.Start(t, daptest.Options{Script: daptest.Script{
		daptest.Reply("threads", map[string]any{"threads": "not a list"}),
		daptest.Reply("scopes", nil),
	}})
	_, err := dap.Call[dap.ThreadsResponseBody](ctxT(t), h.Client, dap.CmdThreads, nil)
	wantRoute(t, err, "decodeBody.DapBodyInvalid")
	// an empty body decodes to the zero value
	scopes, err := dap.Call[dap.ScopesResponseBody](ctxT(t), h.Client, dap.CmdScopes, nil)
	if err != nil || scopes.Scopes != nil {
		t.Errorf("empty body = %+v, %v", scopes, err)
	}
}

// brokenStream is a stream whose writes fail and whose close can fail.
type brokenStream struct {
	closed   chan struct{}
	once     sync.Once
	writeErr error
	closeErr error
}

func (b *brokenStream) Read([]byte) (int, error) {
	<-b.closed
	return 0, io.EOF
}
func (b *brokenStream) Write([]byte) (int, error) { return 0, b.writeErr }
func (b *brokenStream) Close() error {
	b.once.Do(func() { close(b.closed) })
	return b.closeErr
}

func TestWriteFailureEndsTheConnection(t *testing.T) {
	boom := errors.New("Boom", "the pipe broke")
	c := dap.NewClient(&brokenStream{closed: make(chan struct{}), writeErr: boom}, dap.ClientOptions{})
	_, err := c.Request(ctxT(t), dap.CmdThreads, nil)
	appErr := wantRouteError(t, err, "Client.Request.DapAdapterExited")
	if !strings.Contains(appErr.Metadata["cause"].(string), "the pipe broke") {
		t.Errorf("cause = %v", appErr.Metadata["cause"])
	}
	if e := c.Err(); e == nil || !wantRouteBool(e, "WriteFrame.DapFrameWriteFailed") {
		t.Errorf("Err = %v", e)
	}
	c.Wait()
}

func wantRouteBool(err error, route string) bool {
	appErr, ok := errors.FromError(err)
	return ok && appErr.HasRoute(route)
}

func TestCloseFailureIsReported(t *testing.T) {
	boom := errors.New("Boom", "close failed")
	c := dap.NewClient(&brokenStream{closed: make(chan struct{}), closeErr: boom}, dap.ClientOptions{})
	err := c.Close()
	wantRoute(t, err, "Client.Close.DapTransportCloseFailed")
	if !errors.Is(err, boom) {
		t.Errorf("the cause is lost: %v", err)
	}
	c.Wait()
}
