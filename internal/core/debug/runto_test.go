package debug_test

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/emetos/internal/core/debug"
)

// stopAgain is the second stop of a script: the thread stops at a breakpoint, and the answers for the stop follow.
func stopAgain(hit int) []daptest.Step {
	return []daptest.Step{
		daptest.Event("stopped", map[string]any{"reason": "breakpoint", "threadId": 1, "allThreadsStopped": true, "hitBreakpointIds": []int{hit}}),
		daptest.Reply("threads", map[string]any{"threads": []map[string]any{{"id": 1, "name": "main"}, {"id": 2, "name": "worker"}}}),
		daptest.ReplyArgs("stackTrace", map[string]any{"threadId": 1, "levels": 20}, frames(1, 1)),
	}
}

func TestRunToLetsAStoppedThreadRunToALineAndTakesTheBreakpointAway(t *testing.T) {
	e := newEnv(t, nil)
	ctx := ctxT(t)
	e.script("runto", join(
		handshake(fullCaps, noBreakpoints(fullCaps)),
		steps(stoppedAt("step", 1)...),
		steps(bpsReplyFor("/p/a.go", []map[string]any{{"line": 30}}, 61), daptest.Reply("continue", map[string]any{"allThreadsContinued": true})),
		steps(stopAgain(61)...),
		// the breakpoint of the run goes when the thread has stopped
		steps(bpsReplyFor("/p/a.go", []map[string]any{})),
		ending(),
	))
	sess, ok, err := e.start(fakeConfig("runto"))
	if err != nil || !ok {
		t.Fatalf("start: %v %v", ok, err)
	}
	e.wait(debug.EventStopped, nil)
	if err := e.svc.RunTo(ctx, "/p/a.go", 30, 0); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the second stop", func() bool { return e.count(debug.EventStopped) >= 2 })
	waitUntil(t, "the breakpoint of the run to go", func() bool { return len(e.svc.Model().Breakpoints(debug.BreakpointFilter{})) == 0 })
	if err := e.svc.Stop(ctx, sess, true, false); err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventSessionEnded, nil)
}

func TestRunToToAPlaceThatHasABreakpointAddsNothingAndKeepsIt(t *testing.T) {
	e := newEnv(t, nil)
	ctx := ctxT(t)
	own, err := e.svc.AddBreakpoints(ctx, "/p/a.go", []debug.BreakpointData{{Line: 30}})
	if err != nil {
		t.Fatal(err)
	}
	e.script("runto2", join(
		handshake(fullCaps, steps(bpsReplyFor("/p/a.go", []map[string]any{{"line": 30}}, 71),
			daptest.ReplyArgs("setFunctionBreakpoints", map[string]any{"breakpoints": []any{}}, map[string]any{"breakpoints": []any{}}),
			daptest.ReplyArgs("setExceptionBreakpoints", map[string]any{"filters": []string{"all"}}, map[string]any{}))),
		steps(stoppedAt("step", 1)...),
		steps(daptest.Reply("continue", map[string]any{"allThreadsContinued": true})),
		steps(stopAgain(71)...),
		ending(),
	))
	sess, ok, err := e.start(fakeConfig("runto2"))
	if err != nil || !ok {
		t.Fatalf("start: %v %v", ok, err)
	}
	e.wait(debug.EventStopped, nil)
	if err := e.svc.RunTo(ctx, "/p/a.go", 30, 0); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the second stop", func() bool { return e.count(debug.EventStopped) >= 2 })
	if got := e.svc.Model().Breakpoints(debug.BreakpointFilter{}); len(got) != 1 || got[0].ID != own[0].ID {
		t.Errorf("breakpoints = %+v, want only the one of the user", got)
	}
	if err := e.svc.Stop(ctx, sess, true, false); err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventSessionEnded, nil)
}

func TestRunToDoesNothingWhileTheProgramRuns(t *testing.T) {
	e := newEnv(t, nil)
	ctx := ctxT(t)
	e.script("runto3", join(handshake(fullCaps, noBreakpoints(fullCaps)), ending()))
	sess, ok, err := e.start(fakeConfig("runto3"))
	if err != nil || !ok {
		t.Fatalf("start: %v %v", ok, err)
	}
	idle(t, sess)
	if st := e.svc.State(); st != debug.Running {
		t.Skipf("the state is %v, not running", st)
	}
	if err := e.svc.RunTo(ctx, "/p/a.go", 30, 0); err != nil {
		t.Fatal(err)
	}
	if got := e.svc.Model().Breakpoints(debug.BreakpointFilter{}); len(got) != 0 {
		t.Errorf("a run program got breakpoints: %+v", got)
	}
	if err := e.svc.Stop(ctx, sess, true, false); err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventSessionEnded, nil)
}
