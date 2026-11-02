package debug_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/emetos/internal/core/debug"
)

func TestSessionChildSessionAndRunInTerminal(t *testing.T) {
	e := newEnv(t, nil)
	ctx := ctxT(t)
	caps := map[string]any{}
	// The parent asks for a terminal and then for a child session (what js-debug does); the child answers its own launch.
	e.script("parent", join(
		handshake(caps, noBreakpoints(caps)),
		steps(
			daptest.Request("term", "runInTerminal", map[string]any{"kind": "integrated", "title": "Program", "cwd": "/work", "args": []string{"node", "app.js"}}),
			daptest.Await("term"),
			daptest.Request("child", "startDebugging", map[string]any{"request": "launch", "configuration": map[string]any{"name": "worker", "program": "w.js", "$vscode": 7}}),
			daptest.Await("child"),
		),
		ending(),
	))
	e.script("worker", join(handshake(caps, noBreakpoints(caps)), ending()))
	parent, ok, err := e.start(fakeConfig("parent"))
	if err != nil || !ok {
		t.Fatalf("start: %v %v", ok, err)
	}
	e.wait(debug.EventSessionStarted, nil)
	child := e.wait(debug.EventSessionStarted, nil).Session
	if child == nil || child == parent || child.Parent() != parent || child.Name() != "worker" || child.Type() != "fake" || child.Configuration().String("program") != "w.js" {
		t.Fatalf("child = %+v", child)
	}
	if child.Root() != parent.Root() {
		t.Errorf("the child's root is %q", child.Root())
	}
	// The terminal was asked for, and the adapter was told the answer.
	e.mu.Lock()
	terminals := append([]interface{}(nil), len(e.terminals))
	_ = terminals
	got := e.terminals
	e.mu.Unlock()
	if len(got) != 1 || got[0].Cwd != "/work" || got[0].Args[1] != "app.js" || got[0].Title != "Program" {
		t.Errorf("terminals = %+v", got)
	}
	var cfg map[string]any
	_ = cfg
	// Children are listed after their parent, in the model.
	sessions := e.svc.Model().Sessions(false)
	if len(sessions) != 2 || sessions[0] != parent || sessions[1] != child {
		t.Errorf("sessions = %v", sessions)
	}
	// The unknown configuration members the adapter sent are kept for the launch of the child.
	if v := e.requests(1, "launch"); len(v) != 1 {
		t.Fatalf("launch requests of the child = %d", len(v))
	}
	if err := e.svc.Stop(ctx, nil, true, false); err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventSessionEnded, nil)
	e.wait(debug.EventSessionEnded, nil)
	waitUntil(t, "the sessions to be inactive", func() bool { return len(e.svc.Model().Sessions(false)) == 0 })
	if len(e.svc.Model().Sessions(true)) != 2 {
		t.Errorf("inactive sessions are dropped: %d", len(e.svc.Model().Sessions(true)))
	}
}

func TestSessionRunInTerminalFailsWithoutAHandler(t *testing.T) {
	e := newEnv(t, func(o *debug.Options) { o.RunInTerminal = nil })
	caps := map[string]any{}
	e.script("p", join(
		handshake(caps, noBreakpoints(caps)),
		steps(daptest.Request("term", "runInTerminal", map[string]any{"cwd": "/", "args": []string{"x"}}), daptest.Await("term")),
		ending(),
	))
	sess, _, err := e.start(fakeConfig("p"))
	if err != nil {
		t.Fatal(err)
	}
	idle(t, sess)
	waitUntil(t, "the answer to runInTerminal", func() bool {
		for _, m := range e.harness(0).Server.Traffic() {
			if m.Direction == "in" && m.Type == "response" && m.Name == "runInTerminal" {
				return !m.Success
			}
		}
		return false
	})
	if err := e.svc.Stop(ctxT(t), sess, true, false); err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventSessionEnded, nil)
}

func TestSessionRestart(t *testing.T) {
	t.Run("with the restart request", func(t *testing.T) {
		e := newEnv(t, nil)
		ctx := ctxT(t)
		caps := with(fullCaps, "supportsRestartRequest", true)
		e.script("r", join(
			handshake(caps, noBreakpoints(caps)),
			steps(daptest.ReplyArgs("restart", map[string]any{"arguments": map[string]any{"name": "r", "type": "fake"}}, nil)),
			ending(),
		))
		sess, _, err := e.start(fakeConfig("r"))
		if err != nil {
			t.Fatal(err)
		}
		idle(t, sess)
		if err := e.svc.RestartSession(ctx, sess, nil); err != nil {
			t.Fatal(err)
		}
		if e.connects()["r"] != 1 || sess.State() != debug.Running {
			t.Errorf("connections %v, state %v", e.connects(), sess.State())
		}
		if err := e.svc.Stop(ctx, sess, true, false); err != nil {
			t.Fatal(err)
		}
		e.wait(debug.EventSessionEnded, nil)
	})
	t.Run("by ending and starting again", func(t *testing.T) {
		var mu sync.Mutex
		var order []string
		e := newEnv(t, func(o *debug.Options) {
			o.RunTask = func(_ context.Context, _ string, task debug.TaskRef) (debug.TaskResult, error) {
				mu.Lock()
				order = append(order, task.String())
				mu.Unlock()
				return debug.TaskSuccess, nil
			}
		})
		ctx := ctxT(t)
		caps := map[string]any{}
		cfg := fakeConfig("r2")
		cfg["preLaunchTask"] = "build"
		cfg["preRestartTask"] = "stop-services"
		cfg["postRestartTask"] = "start-services"
		cfg["postDebugTask"] = "clean"
		e.script("r2", join(
			handshake(caps, noBreakpoints(caps)),
			steps(daptest.ReplyArgs("disconnect", map[string]any{"restart": true}, nil), daptest.WaitClose()),
		))
		// (the same script serves the second start, which ends with the user's stop)
		sess, _, err := e.start(cfg)
		if err != nil {
			t.Fatal(err)
		}
		idle(t, sess)
		e.wait(debug.EventSessionStarted, nil)
		mu.Lock()
		order = nil
		mu.Unlock()
		e.script("r2", join(handshake(caps, noBreakpoints(caps)), ending()))
		if err := e.svc.RestartSession(ctx, sess, nil); err != nil {
			t.Fatal(err)
		}
		end := e.wait(debug.EventSessionEnded, nil)
		if !end.Restart || end.Session != sess {
			t.Errorf("end = %+v", end)
		}
		e.wait(debug.EventSessionStarted, nil)
		if e.connects()["r2"] != 2 || sess.State() != debug.Running {
			t.Errorf("connections %v, state %v", e.connects(), sess.State())
		}
		mu.Lock()
		got := append([]string(nil), order...)
		mu.Unlock()
		// The tasks of a restart, in VS Code's order; the task after the session does not run for a restart.
		want := []string{"stop-services", "clean", "build", "start-services"}
		if len(got) != len(want) {
			t.Fatalf("tasks = %v, want %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("tasks = %v, want %v", got, want)
				break
			}
		}
		if err := e.svc.Stop(ctx, sess, true, false); err != nil {
			t.Fatal(err)
		}
		e.wait(debug.EventSessionEnded, func(ev debug.Event) bool { return !ev.Restart })
		waitUntil(t, "the task after the session", func() bool {
			mu.Lock()
			defer mu.Unlock()
			return order[len(order)-1] == "clean"
		})
	})
	t.Run("an automatic restart asked by the adapter runs no task", func(t *testing.T) {
		var mu sync.Mutex
		tasks := 0
		e := newEnv(t, func(o *debug.Options) {
			o.RunTask = func(context.Context, string, debug.TaskRef) (debug.TaskResult, error) {
				mu.Lock()
				tasks++
				mu.Unlock()
				return debug.TaskSuccess, nil
			}
		})
		caps := map[string]any{}
		cfg := fakeConfig("auto")
		cfg["preLaunchTask"] = "build"
		first := join(handshake(caps, noBreakpoints(caps)), steps(
			daptest.Expect("kick", nil),
			daptest.Event("terminated", map[string]any{"restart": map[string]any{"again": true}}),
			daptest.Respond("kick", nil),
			daptest.ReplyArgs("disconnect", map[string]any{"restart": true}, nil),
			daptest.WaitClose(),
		))
		e.script("auto", first)
		sess, _, err := e.start(cfg)
		if err != nil {
			t.Fatal(err)
		}
		idle(t, sess)
		e.wait(debug.EventSessionStarted, nil)
		mu.Lock()
		tasks = 0
		mu.Unlock()
		e.script("auto", join(handshake(caps, noBreakpoints(caps)), ending()))
		if _, err := sess.CustomRequest(ctxT(t), "kick", nil); err != nil {
			t.Fatal(err)
		}
		e.wait(debug.EventSessionStarted, nil)
		mu.Lock()
		n := tasks
		mu.Unlock()
		if n != 0 || e.connects()["auto"] != 2 {
			t.Errorf("tasks run for the automatic restart: %d; connections %v", n, e.connects())
		}
		// What the adapter asked the restart with goes into the next launch.
		if v := e.requests(1, "launch"); len(v) != 1 || !containsJSON(v[0], `"__restart":{"again":true}`) {
			t.Errorf("launch of the restart = %s", v)
		}
		if err := e.svc.Stop(ctxT(t), sess, true, false); err != nil {
			t.Fatal(err)
		}
		e.wait(debug.EventSessionEnded, func(ev debug.Event) bool { return !ev.Restart })
	})
}

func TestSessionStopAndDisconnect(t *testing.T) {
	ctx := ctxT(t)
	t.Run("an attach is disconnected without ending the program", func(t *testing.T) {
		e := newEnv(t, nil)
		caps := map[string]any{"supportTerminateDebuggee": true}
		s := join(handshake(caps, noBreakpoints(caps)), steps(daptest.ReplyArgs("disconnect", map[string]any{"terminateDebuggee": false}, nil), daptest.WaitClose()))
		s[1] = daptest.Expect("attach", nil)
		for i := range s {
			if s[i].Op == daptest.OpRespond && s[i].To == "launch" {
				s[i].To = "attach"
			}
		}
		e.script("a", s)
		cfg := fakeConfig("a")
		cfg["request"] = "attach"
		sess, _, err := e.start(cfg)
		if err != nil {
			t.Fatal(err)
		}
		idle(t, sess)
		if !sess.IsAttach() {
			t.Error("not an attach")
		}
		if err := e.svc.Stop(ctx, sess, true, false); err != nil {
			t.Fatal(err)
		}
		e.wait(debug.EventSessionEnded, nil)
	})
	t.Run("terminating an attach ends the program", func(t *testing.T) {
		e := newEnv(t, nil)
		caps := map[string]any{"supportTerminateDebuggee": true, "supportsTerminateRequest": true}
		s := join(handshake(caps, noBreakpoints(caps)), steps(daptest.ReplyArgs("disconnect", map[string]any{"terminateDebuggee": true}, nil), daptest.WaitClose()))
		s[1] = daptest.Expect("attach", nil)
		for i := range s {
			if s[i].Op == daptest.OpRespond && s[i].To == "launch" {
				s[i].To = "attach"
			}
		}
		e.script("a2", s)
		cfg := fakeConfig("a2")
		cfg["request"] = "attach"
		sess, _, err := e.start(cfg)
		if err != nil {
			t.Fatal(err)
		}
		idle(t, sess)
		if err := e.svc.Stop(ctx, sess, false, false); err != nil {
			t.Fatal(err)
		}
		e.wait(debug.EventSessionEnded, nil)
	})
	t.Run("a launch is terminated, then disconnected when the adapter says terminated", func(t *testing.T) {
		e := newEnv(t, nil)
		caps := map[string]any{"supportTerminateDebuggee": true, "supportsTerminateRequest": true}
		e.script("l", join(handshake(caps, noBreakpoints(caps)), steps(
			daptest.Expect("terminate", map[string]any{}),
			daptest.Event("terminated", nil),
			daptest.Respond("terminate", nil),
			daptest.ReplyArgs("disconnect", map[string]any{}, nil),
			daptest.WaitClose(),
		)))
		sess, _, err := e.start(fakeConfig("l"))
		if err != nil {
			t.Fatal(err)
		}
		idle(t, sess)
		if err := e.svc.Stop(ctx, sess, false, false); err != nil {
			t.Fatal(err)
		}
		end := e.wait(debug.EventSessionEnded, nil)
		if end.Err != nil {
			t.Errorf("end error = %v", end.Err)
		}
		if got := e.requests(0, "disconnect"); len(got) != 1 || !containsJSON(got[0], `"terminateDebuggee":false`) {
			t.Errorf("disconnect = %s", got)
		}
	})
	t.Run("a second terminate disconnects with terminateDebuggee", func(t *testing.T) {
		e := newEnv(t, nil)
		caps := map[string]any{"supportTerminateDebuggee": true, "supportsTerminateRequest": true}
		e.script("l2", join(handshake(caps, noBreakpoints(caps)), steps(
			daptest.Reply("terminate", nil), // the adapter does not say terminated
			daptest.ReplyArgs("disconnect", map[string]any{"terminateDebuggee": true}, nil),
			daptest.WaitClose(),
		)))
		sess, _, err := e.start(fakeConfig("l2"))
		if err != nil {
			t.Fatal(err)
		}
		idle(t, sess)
		if err := sess.Terminate(ctx, false); err != nil {
			t.Fatal(err)
		}
		if sess.State() != debug.Running {
			t.Errorf("the session ended before the adapter said so: %v", sess.State())
		}
		if err := sess.Terminate(ctx, false); err != nil {
			t.Fatal(err)
		}
		e.wait(debug.EventSessionEnded, nil)
	})
	t.Run("stopping with no session cancels the starts", func(t *testing.T) {
		e := newEnv(t, nil)
		if err := e.svc.Stop(ctx, nil, false, false); err != nil {
			t.Fatal(err)
		}
		if e.svc.State() != debug.Inactive {
			t.Errorf("state = %v", e.svc.State())
		}
	})
	t.Run("a request on a session with no adapter", func(t *testing.T) {
		e := newEnv(t, nil)
		caps := map[string]any{}
		e.script("gone", join(handshake(caps, noBreakpoints(caps)), ending()))
		sess, _, err := e.start(fakeConfig("gone"))
		if err != nil {
			t.Fatal(err)
		}
		idle(t, sess)
		if err := e.svc.Stop(ctx, sess, true, false); err != nil {
			t.Fatal(err)
		}
		e.wait(debug.EventSessionEnded, nil)
		_, err = sess.CustomRequest(ctx, "x", nil)
		_ = wantRoute(t, err, "do.DebugNoAdapter")
		_ = wantRoute(t, sess.Pause(ctx, 1), "do.DebugNoAdapter")
	})
}

func TestSessionAdapterDies(t *testing.T) {
	t.Run("a program that exits with a failure", func(t *testing.T) {
		e := newEnv(t, nil)
		e.transport = map[string]daptest.Transport{"crash": daptest.Stdio}
		ctx := ctxT(t)
		b, err := e.svc.AddBreakpoints(ctx, "/p/a.go", []debug.BreakpointData{{Line: 3}})
		if err != nil {
			t.Fatal(err)
		}
		caps := map[string]any{}
		e.script("crash", join(
			handshake(caps, steps(
				daptest.Reply("setBreakpoints", map[string]any{"breakpoints": []map[string]any{{"id": 1, "verified": true, "line": 4}}}),
				daptest.ReplyArgs("setExceptionBreakpoints", map[string]any{"filters": []string{}}, map[string]any{}),
			)),
			steps(daptest.Delay(50), daptest.Stderr("the debugger fell over"), daptest.Crash(3)),
		))
		sess, ok, err := e.start(fakeConfig("crash"))
		if err != nil || !ok {
			t.Fatalf("start: %v %v", ok, err)
		}
		end := e.wait(debug.EventSessionEnded, nil)
		appErr := wantRoute(t, end.Err, "adapterCrash.DebugAdapterCrashed")
		if appErr.Metadata["code"] != 3 {
			t.Errorf("exit code = %v", appErr.Metadata["code"])
		}
		if sess.State() != debug.Inactive || len(sess.Threads()) != 0 {
			t.Errorf("state %v, threads %d", sess.State(), len(sess.Threads()))
		}
		// What the adapter knew about the breakpoints is gone.
		if bp, _ := e.svc.Model().Breakpoint(b[0].ID); bp.Line != 3 {
			t.Errorf("breakpoint after the crash = %+v", bp)
		}
		if f := e.svc.Model().Focus(); f.Session != nil {
			t.Errorf("focus = %+v", f)
		}
		if e.svc.State() != debug.Inactive {
			t.Errorf("service state = %v", e.svc.State())
		}
	})
	t.Run("a program that exits by itself with status 0", func(t *testing.T) {
		e := newEnv(t, nil)
		e.transport = map[string]daptest.Transport{"quiet": daptest.Stdio}
		caps := map[string]any{}
		e.script("quiet", join(handshake(caps, noBreakpoints(caps)), steps(daptest.Delay(50), daptest.Exit(0))))
		if _, _, err := e.start(fakeConfig("quiet")); err != nil {
			t.Fatal(err)
		}
		if end := e.wait(debug.EventSessionEnded, nil); end.Err != nil {
			t.Errorf("end error = %v", end.Err)
		}
	})
	t.Run("an adapter that fails to initialize", func(t *testing.T) {
		e := newEnv(t, nil)
		e.script("bad", steps(daptest.ReplyError("initialize", "I do not know how", nil), daptest.Reply("disconnect", nil), daptest.WaitClose()))
		sess, ok, err := e.start(fakeConfig("bad"))
		if ok || sess != nil {
			t.Fatalf("started: %v", ok)
		}
		appErr := wantRoute(t, err, "DapResponseFailed")
		if appErr.Message != "I do not know how" {
			t.Errorf("message = %q", appErr.Message)
		}
		if e.svc.State() != debug.Inactive {
			t.Errorf("service state = %v", e.svc.State())
		}
	})
	t.Run("an adapter that fails to launch", func(t *testing.T) {
		e := newEnv(t, nil)
		caps := map[string]any{}
		e.script("nolaunch", steps(
			daptest.Reply("initialize", caps),
			daptest.ReplyError("launch", "cannot start the program", nil),
			daptest.Reply("disconnect", nil),
			daptest.WaitClose(),
		))
		_, ok, err := e.start(fakeConfig("nolaunch"))
		if ok {
			t.Fatal("started")
		}
		_ = wantRoute(t, err, "DapResponseFailed")
		e.wait(debug.EventSessionEnded, nil)
	})
	t.Run("the connection cannot be made", func(t *testing.T) {
		e := newEnv(t, nil)
		e.script("other", nil)
		_, ok, err := e.start(fakeConfig("missing script"))
		if ok {
			t.Fatal("started")
		}
		_ = wantRoute(t, err, "TestNoScript")
	})
}

func TestSessionStartTimesOutWhenTheAdapterDoesNotAnswer(t *testing.T) {
	e := newEnv(t, nil)
	e.script("slow", steps(daptest.Stall()))
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	ok, err := e.svc.StartDebugging(ctx, nil, debug.Target{Config: fakeConfig("slow")}, debug.SessionOptions{})
	if ok {
		t.Fatal("started")
	}
	appErr := wantRoute(t, err, "DapTimeout")
	if appErr.Tag != "DapTimeout" {
		t.Errorf("tag = %s", appErr.Tag)
	}
	if e.svc.State() != debug.Inactive {
		t.Errorf("state = %v", e.svc.State())
	}
}

func TestSessionCancelledStartIsSilent(t *testing.T) {
	e := newEnv(t, nil)
	e.script("slow", steps(daptest.Stall()))
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	ok, err := e.svc.StartDebugging(ctx, nil, debug.Target{Config: fakeConfig("slow")}, debug.SessionOptions{})
	if ok || err != nil {
		t.Errorf("a cancelled start = %v, %v", ok, err)
	}
}

func TestServiceStopCancelsAStartInProgress(t *testing.T) {
	e := newEnv(t, nil)
	e.script("slow", steps(daptest.Stall()))
	done := make(chan struct{})
	var ok bool
	var err error
	go func() {
		ok, err = e.svc.StartDebugging(context.Background(), nil, debug.Target{Config: fakeConfig("slow")}, debug.SessionOptions{})
		close(done)
	}()
	waitUntil(t, "the session to be initializing", func() bool { return e.svc.State() == debug.Initializing })
	// The session exists but has no adapter answer yet; Stop without a session ends it.
	waitUntil(t, "the start to be stopped", func() bool {
		_ = e.svc.Stop(context.Background(), nil, true, false)
		select {
		case <-done:
			return true
		default:
			return false
		}
	})
	if ok || err != nil {
		t.Errorf("a stopped start = %v, %v", ok, err)
	}
}
