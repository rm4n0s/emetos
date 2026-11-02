package debug_test

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/emetos/internal/core/debug"
)

// kickWith is the steps of an adapter that sends events as a result of the custom request "kick".
func kickWith(events ...daptest.Step) []daptest.Step {
	out := []daptest.Step{daptest.Expect("kick", nil)}
	out = append(out, events...)
	return append(out, daptest.Respond("kick", nil))
}

func kick(t *testing.T, sess *debug.Session) {
	t.Helper()
	if _, err := sess.CustomRequest(ctxT(t), "kick", nil); err != nil {
		t.Fatal(err)
	}
	idle(t, sess)
}

func TestSessionEvents(t *testing.T) {
	e := newEnv(t, nil)
	ctx := ctxT(t)
	e.script("ev", join(
		handshake(fullCaps, noBreakpoints(fullCaps)),
		// A thread starts and one exits.
		kickWith(
			daptest.Event("thread", map[string]any{"reason": "started", "threadId": 2}),
		),
		steps(daptest.Reply("threads", map[string]any{"threads": []map[string]any{{"id": 1, "name": "main"}, {"id": 2, "name": "w2"}}})),
		kickWith(daptest.Event("thread", map[string]any{"reason": "exited", "threadId": 2})),
		// Output, with and without something to expand, the process exiting, an event of the adapter's own, the capabilities.
		kickWith(
			daptest.Event("output", map[string]any{"category": "stdout", "output": "hello\n"}),
			daptest.Event("output", map[string]any{"category": "console", "output": "object", "variablesReference": 500}),
			daptest.Event("exited", map[string]any{"exitCode": 3}),
			daptest.Event("loadedSource", map[string]any{"reason": "new", "source": map[string]any{"path": "/p/c.go"}}),
			daptest.Event("progressStart", map[string]any{"progressId": "p1", "title": "Compiling"}),
			daptest.Event("capabilities", map[string]any{"capabilities": map[string]any{"supportsStepBack": true}}),
			daptest.Event("frobnicated", map[string]any{"n": 1}),
			daptest.Event("module", map[string]any{"reason": "new", "module": map[string]any{"id": 1, "name": "m"}}),
		),
		steps(daptest.ReplyArgs("variables", map[string]any{"variablesReference": 500}, map[string]any{"variables": []map[string]any{{"name": "k", "value": "v"}}})),
		ending(),
	))
	sess, ok, err := e.start(fakeConfig("ev"))
	if err != nil || !ok {
		t.Fatalf("start: %v %v", ok, err)
	}
	idle(t, sess)

	kick(t, sess)
	waitUntil(t, "the second thread", func() bool { return len(sess.Threads()) == 2 })
	if got := len(sess.Threads()); got != 2 {
		t.Fatalf("threads = %d, want 2", got)
	}
	if th := sess.Thread(2); th == nil || th.Name() != "w2" {
		t.Errorf("threads = %v", sess.Threads())
	}
	kick(t, sess)
	waitUntil(t, "the thread to exit", func() bool { return sess.Thread(2) == nil })
	if len(sess.Threads()) != 1 {
		t.Errorf("threads after an exit = %v", sess.Threads())
	}

	kick(t, sess)
	var outputs []debug.Event
	waitUntil(t, "the output events", func() bool { return e.count(debug.EventOutput) == 2 })
	e.mu.Lock()
	for _, ev := range e.log {
		if ev.Kind == debug.EventOutput {
			outputs = append(outputs, ev)
		}
	}
	e.mu.Unlock()
	if outputs[0].Output.Output != "hello\n" || outputs[0].Node != nil {
		t.Errorf("first output = %+v", outputs[0])
	}
	if outputs[1].Node == nil || outputs[1].Output.VariablesReference != 500 {
		t.Fatalf("second output = %+v", outputs[1])
	}
	kids, err := outputs[1].Node.Children(ctx)
	if err != nil || len(kids) != 1 || kids[0].Name != "k" || kids[0].Value() != "v" {
		t.Errorf("children of the output = %v %v", kids, err)
	}
	if exited := e.wait(debug.EventExited, nil); exited.ExitCode != 3 {
		t.Errorf("exit code = %d", exited.ExitCode)
	}
	e.wait(debug.EventLoadedSource, nil)
	if p := e.wait(debug.EventProgress, nil); p.DAP == nil || p.DAP.Event != "progressStart" {
		t.Errorf("progress = %+v", p)
	}
	e.wait(debug.EventCapabilities, nil)
	if c := e.wait(debug.EventCustom, nil); c.DAP == nil || c.DAP.Event != "frobnicated" {
		t.Errorf("custom = %+v", c)
	}
	if e.count(debug.EventCustom) != 1 {
		t.Errorf("modules are shown as custom events: %d", e.count(debug.EventCustom))
	}
	if !sess.Capabilities().SupportsStepBack {
		t.Errorf("capabilities = %+v", sess.Capabilities())
	}
	if err := e.svc.Stop(ctx, sess, true, false); err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventSessionEnded, nil)
}

func TestBreakpointEventsOfTheAdapter(t *testing.T) {
	e := newEnv(t, nil)
	ctx := ctxT(t)
	known, err := e.svc.AddBreakpoints(ctx, "/p/a.go", []debug.BreakpointData{{Line: 3}, {Line: 20}})
	if err != nil {
		t.Fatal(err)
	}
	fn, err := e.svc.AddFunctionBreakpoint(ctx, debug.FunctionBreakpointOptions{Name: "main.run"})
	if err != nil {
		t.Fatal(err)
	}
	e.script("bpev", join(
		handshake(fullCaps, steps(
			daptest.Reply("setBreakpoints", map[string]any{"breakpoints": []map[string]any{{"id": 61, "verified": false}, {"id": 62, "verified": false}}}),
			daptest.Reply("setFunctionBreakpoints", map[string]any{"breakpoints": []map[string]any{{"id": 63, "verified": false}}}),
			daptest.Reply("setExceptionBreakpoints", map[string]any{}),
		)),
		kickWith(
			// 61 is verified and moved; the adapter has a new one of its own; 62 goes away; the function breakpoint is verified.
			daptest.Event("breakpoint", map[string]any{"reason": "changed", "breakpoint": map[string]any{"id": 61, "verified": true, "line": 4}}),
			daptest.Event("breakpoint", map[string]any{"reason": "new", "breakpoint": map[string]any{"id": 70, "verified": true, "line": 12, "source": map[string]any{"path": "/p/n.go"}}}),
			daptest.Event("breakpoint", map[string]any{"reason": "removed", "breakpoint": map[string]any{"id": 62}}),
			daptest.Event("breakpoint", map[string]any{"reason": "changed", "breakpoint": map[string]any{"id": 63, "verified": true}}),
		),
		ending(),
	))
	sess, ok, err := e.start(fakeConfig("bpev"))
	if err != nil || !ok {
		t.Fatalf("start: %v %v", ok, err)
	}
	idle(t, sess)
	kick(t, sess)
	m := e.svc.Model()
	waitUntil(t, "the events to be handled", func() bool {
		fbs := m.FunctionBreakpoints()
		b, _ := m.Breakpoint(known[0].ID)
		_, stays := m.Breakpoint(known[1].ID)
		return len(fbs) == 1 && fbs[0].Verified && b.Verified && !stays && len(m.Breakpoints(debug.BreakpointFilter{Path: "/p/n.go"})) == 1
	})
	if b, _ := m.Breakpoint(known[0].ID); !b.Verified || b.Line != 4 || b.RequestedLine != 3 {
		t.Errorf("changed breakpoint = %+v", b)
	}
	if _, exists := m.Breakpoint(known[1].ID); exists {
		t.Error("the removed breakpoint is still there")
	}
	added := m.Breakpoints(debug.BreakpointFilter{Path: "/p/n.go"})
	if len(added) != 1 || added[0].Line != 12 || !added[0].Verified {
		t.Errorf("breakpoint of the adapter = %+v", added)
	}
	if fbs := m.FunctionBreakpoints(); len(fbs) != 1 || fbs[0].ID != fn.ID || !fbs[0].Verified {
		t.Errorf("function breakpoints = %+v", fbs)
	}
	if err := e.svc.Stop(ctx, sess, true, false); err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventSessionEnded, nil)
}

func TestInvalidatedEventLoadsTheStopAgain(t *testing.T) {
	e := newEnv(t, nil)
	ctx := ctxT(t)
	e.script("inv", join(
		handshake(fullCaps, noBreakpoints(fullCaps)),
		stoppedAt("breakpoint", 2),
		kickWith(daptest.Event("invalidated", map[string]any{"areas": []string{"stacks"}})),
		steps(
			daptest.Reply("threads", map[string]any{"threads": []map[string]any{{"id": 1, "name": "main"}, {"id": 2, "name": "worker"}}}),
			daptest.ReplyArgs("stackTrace", map[string]any{"threadId": 1}, frames(3, 3)),
		),
		// Areas that are not about the stack change nothing.
		kickWith(daptest.Event("invalidated", map[string]any{"areas": []string{"variables"}})),
		ending(),
	))
	sess, _, err := e.start(fakeConfig("inv"))
	if err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventStopped, nil)
	if n := len(sess.Thread(1).CallStack()); n != 2 {
		t.Fatalf("call stack = %d", n)
	}
	kick(t, sess)
	e.wait(debug.EventInvalidated, nil)
	ev := e.wait(debug.EventStopped, nil)
	if n := len(ev.Thread.CallStack()); n != 3 {
		t.Errorf("call stack after invalidated = %d, want 3", n)
	}
	kick(t, sess)
	e.wait(debug.EventInvalidated, nil)
	if n := len(sess.Thread(1).CallStack()); n != 3 {
		t.Errorf("call stack after a variables-only invalidation = %d", n)
	}
	if err := e.svc.Stop(ctx, sess, true, false); err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventSessionEnded, nil)
}

func TestTerminatedEvent(t *testing.T) {
	t.Run("ends the session", func(t *testing.T) {
		e := newEnv(t, nil)
		e.script("term", join(
			handshake(fullCaps, noBreakpoints(fullCaps)),
			kickWith(daptest.Event("terminated", nil)),
			ending(),
		))
		sess, _, err := e.start(fakeConfig("term"))
		if err != nil {
			t.Fatal(err)
		}
		idle(t, sess)
		if _, err := sess.CustomRequest(ctxT(t), "kick", nil); err != nil {
			t.Fatal(err)
		}
		if end := e.wait(debug.EventSessionEnded, nil); end.Session != sess || end.Restart {
			t.Errorf("end = %+v", end)
		}
		if sess.State() != debug.Inactive {
			t.Errorf("state = %v", sess.State())
		}
	})
	t.Run("with restart data restarts the session", func(t *testing.T) {
		e := newEnv(t, nil)
		caps := with(fullCaps, "supportsRestartRequest", true)
		e.script("term", join(
			handshake(caps, noBreakpoints(caps)),
			kickWith(daptest.Event("terminated", map[string]any{"restart": map[string]any{"again": 1}})),
			steps(daptest.ReplyArgs("restart", map[string]any{"arguments": map[string]any{"__restart": map[string]any{"again": 1}}}, nil)),
			ending(),
		))
		sess, _, err := e.start(fakeConfig("term"))
		if err != nil {
			t.Fatal(err)
		}
		idle(t, sess)
		if _, err := sess.CustomRequest(ctxT(t), "kick", nil); err != nil {
			t.Fatal(err)
		}
		waitUntil(t, "the restart request", func() bool { return len(e.requests(0, "restart")) == 1 })
		if sess.State() == debug.Inactive {
			t.Error("the session ended")
		}
		if err := e.svc.Stop(ctxT(t), sess, true, false); err != nil {
			t.Fatal(err)
		}
		e.wait(debug.EventSessionEnded, nil)
	})
}

func TestStateOfTheServiceWhileStarting(t *testing.T) {
	e := newEnv(t, nil)
	ctx := ctxT(t)
	if !e.svc.FirstSessionStart() || e.svc.HasDebugged() || e.svc.State() != debug.Inactive {
		t.Errorf("before: first %v, debugged %v, state %v", e.svc.FirstSessionStart(), e.svc.HasDebugged(), e.svc.State())
	}
	e.script("st", join(handshake(fullCaps, noBreakpoints(fullCaps)), stoppedAt("pause", 1), ending()))
	sess, _, err := e.start(fakeConfig("st"))
	if err != nil {
		t.Fatal(err)
	}
	if e.svc.FirstSessionStart() || !e.svc.HasDebugged() {
		t.Errorf("after: first %v, debugged %v", e.svc.FirstSessionStart(), e.svc.HasDebugged())
	}
	e.wait(debug.EventStopped, nil)
	if e.svc.State() != debug.Stopped {
		t.Errorf("state = %v", e.svc.State())
	}
	if err := e.svc.Stop(ctx, sess, true, false); err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventSessionEnded, nil)
	if e.svc.State() != debug.Inactive || e.svc.FirstSessionStart() {
		t.Errorf("at the end: state %v, first %v", e.svc.State(), e.svc.FirstSessionStart())
	}
}
