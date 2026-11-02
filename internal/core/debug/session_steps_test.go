package debug_test

import (
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/emetos/internal/core/debug"
)

func TestSessionStepWaitsForTheEventsBeforeItsResponse(t *testing.T) {
	// The response of next is handed over as soon as it is read, even while the stopped event before it waits for the event loop.
	// The step must not take that for "no stop followed": the loop is held up here by a listener that blocks on a custom event.
	e := newEnv(t, nil)
	ctx := ctxT(t)
	release := make(chan struct{})
	held := make(chan struct{}, 1)
	e.svc.Subscribe(func(ev debug.Event) {
		if ev.Kind == debug.EventCustom && ev.DAP.Event == "hold" {
			held <- struct{}{}
			<-release
		}
	})
	e.script("step", join(
		handshake(fullCaps, noBreakpoints(fullCaps)),
		stoppedAt("breakpoint", 2),
		steps(
			daptest.Expect("next", map[string]any{"threadId": 1, "granularity": "line"}),
			daptest.Event("hold", nil),
			daptest.Event("stopped", map[string]any{"reason": "step", "threadId": 1, "allThreadsStopped": true}),
			daptest.Respond("next", nil),
			daptest.Reply("threads", map[string]any{"threads": []map[string]any{{"id": 1, "name": "main"}, {"id": 2, "name": "worker"}}}),
			daptest.ReplyArgs("stackTrace", map[string]any{"threadId": 1, "levels": 20}, frames(2, 2)),
			// A step that is followed by no stop: the thread runs.
			daptest.Expect("stepIn", map[string]any{"threadId": 1}),
			daptest.Respond("stepIn", nil),
			daptest.Expect("stepOut", map[string]any{"threadId": 2}),
			daptest.Respond("stepOut", nil),
		),
		ending(),
	))
	sess, ok, err := e.start(fakeConfig("step"))
	if err != nil || !ok {
		t.Fatalf("start: %v %v", ok, err)
	}
	th := e.wait(debug.EventStopped, nil).Thread

	done := make(chan error, 1)
	go func() { done <- th.Next(ctx, "line") }()
	select {
	case <-held:
	case <-time.After(wait):
		t.Fatal("the loop was not held up")
	}
	select {
	case err := <-done:
		t.Fatalf("Next returned (%v) while the stopped event before its response was not handled", err)
	case <-time.After(150 * time.Millisecond):
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventStopped, nil)
	if !th.Stopped() || th.StoppedDetails().Reason != "step" || th.LastSteppingGranularity() != "line" || sess.State() != debug.Stopped {
		t.Errorf("after the step: stopped %v details %+v granularity %q", th.Stopped(), th.StoppedDetails(), th.LastSteppingGranularity())
	}

	// stepIn answered with no stop after it: only the thread that stepped continues.
	other := sess.Thread(2)
	if !other.Stopped() {
		t.Fatal("thread 2 should be stopped")
	}
	if err := th.StepIn(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if th.Stopped() || !other.Stopped() {
		t.Errorf("after stepIn: thread 1 stopped %v, thread 2 stopped %v", th.Stopped(), other.Stopped())
	}
	// The focused thread decides (VS Code): it runs, so the session does, though thread 2 is stopped.
	if sess.State() != debug.Running {
		t.Errorf("the state with the focused thread running and another stopped is %v", sess.State())
	}
	if err := other.StepOut(ctx, ""); err != nil {
		t.Fatal(err)
	}
	if other.Stopped() {
		t.Error("thread 2 is still stopped")
	}
	if err := e.svc.Stop(ctx, sess, true, false); err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventSessionEnded, nil)
}
