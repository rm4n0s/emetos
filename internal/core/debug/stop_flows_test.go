package debug_test

import (
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/emetos/internal/core/debug"
)

// frameRange is the stack trace body of frames from..from+n-1 of a stack of total frames (ids 100+).
func frameRange(from, n, total int) map[string]any {
	var fs []map[string]any
	for i := from; i < from+n; i++ {
		fs = append(fs, map[string]any{"id": 100 + i, "name": "f" + itoa(i), "line": 10 * (i + 1), "column": 1, "source": map[string]any{"path": "/p/a.go"}})
	}
	return map[string]any{"stackFrames": fs, "totalFrames": total}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var d []byte
	for ; i > 0; i /= 10 {
		d = append([]byte{byte('0' + i%10)}, d...)
	}
	return string(d)
}

func TestDelayedStackLoadingAndShowMoreFrames(t *testing.T) {
	e := newEnv(t, func(o *debug.Options) { o.StackDelay = 30 * time.Millisecond })
	ctx := ctxT(t)
	caps := with(fullCaps, "supportsDelayedStackTraceLoading", true)
	e.script("delayed", join(
		handshake(caps, noBreakpoints(caps)),
		steps(
			daptest.Event("stopped", map[string]any{"reason": "step", "threadId": 1, "allThreadsStopped": true}),
			daptest.Reply("threads", map[string]any{"threads": []map[string]any{{"id": 1, "name": "main"}}}),
			daptest.ReplyArgs("stackTrace", map[string]any{"threadId": 1, "levels": 1}, frameRange(0, 1, 25)),
			daptest.ReplyArgs("stackTrace", map[string]any{"threadId": 1, "startFrame": 1, "levels": 19}, frameRange(1, 19, 25)),
			// Show more: only the five that are left are asked for.
			daptest.ReplyArgs("stackTrace", map[string]any{"threadId": 1, "startFrame": 20, "levels": 5}, frameRange(20, 5, 25)),
		),
		ending(),
	))
	sess, ok, err := e.start(fakeConfig("delayed"))
	if err != nil || !ok {
		t.Fatalf("start: %v %v", ok, err)
	}
	ev := e.wait(debug.EventStopped, nil)
	th := ev.Thread
	if n := len(th.CallStack()); n != 20 || th.ReachedEndOfCallStack() {
		t.Fatalf("call stack %d frames, reached the end %v", n, th.ReachedEndOfCallStack())
	}
	if d := th.StoppedDetails(); d.TotalFrames != 25 {
		t.Errorf("total frames = %d", d.TotalFrames)
	}
	if err := sess.LoadMoreFrames(ctx, th, 100); err != nil {
		t.Fatal(err)
	}
	if n := len(th.CallStack()); n != 25 || !th.ReachedEndOfCallStack() {
		t.Errorf("after loading more: %d frames, reached the end %v", n, th.ReachedEndOfCallStack())
	}
	for i, f := range th.CallStack() {
		if f.Index != i {
			t.Fatalf("frame %d has index %d", i, f.Index)
		}
	}
	// Nothing is asked once the end is reached.
	if err := sess.LoadMoreFrames(ctx, th, 100); err != nil {
		t.Fatal(err)
	}
	if err := e.svc.Stop(ctx, sess, true, false); err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventSessionEnded, nil)
}

// Decision 1102: a flood of events does not keep the answers to the requests of a stop from being read.
func TestStopAfterAFloodOfEvents(t *testing.T) {
	e := newEnv(t, nil)
	ctx := ctxT(t)
	const flood = 3000
	e.script("flood", join(
		handshake(fullCaps, noBreakpoints(fullCaps)),
		steps(
			daptest.Expect("kick", nil),
			daptest.Burst(flood, 40),
			daptest.Event("stopped", map[string]any{"reason": "breakpoint", "threadId": 1, "allThreadsStopped": true}),
			daptest.Respond("kick", nil),
			daptest.Reply("threads", map[string]any{"threads": []map[string]any{{"id": 1, "name": "main"}}}),
			daptest.ReplyArgs("stackTrace", map[string]any{"threadId": 1, "levels": 20}, frames(2, 2)),
		),
		ending(),
	))
	sess, ok, err := e.start(fakeConfig("flood"))
	if err != nil || !ok {
		t.Fatalf("start: %v %v", ok, err)
	}
	idle(t, sess)
	if _, err := sess.CustomRequest(ctx, "kick", nil); err != nil {
		t.Fatal(err)
	}
	ev := e.wait(debug.EventStopped, nil)
	if n := len(ev.Thread.CallStack()); n != 2 {
		t.Errorf("call stack = %d frames", n)
	}
	waitUntil(t, "every output event to be handled", func() bool { return e.count(debug.EventOutput) >= flood })
	if err := e.svc.Stop(ctx, sess, true, false); err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventSessionEnded, nil)
}

func TestTriggeredBreakpointsAreSentWhenTheirTriggerIsHit(t *testing.T) {
	e := newEnv(t, nil)
	ctx := ctxT(t)
	trig, err := e.svc.AddBreakpoints(ctx, "/p/a.go", []debug.BreakpointData{{Line: 3}})
	if err != nil {
		t.Fatal(err)
	}
	waiting, err := e.svc.AddBreakpoints(ctx, "/p/b.go", []debug.BreakpointData{{Line: 9, TriggeredBy: trig[0].ID}})
	if err != nil {
		t.Fatal(err)
	}
	e.script("trig", join(
		handshake(fullCaps, steps(
			bpsReplyFor("/p/a.go", []map[string]any{{"line": 3}}, 41),
			bpsReplyFor("/p/b.go", []map[string]any{}), // the waiting one is not sent yet
			daptest.ReplyArgs("setFunctionBreakpoints", map[string]any{"breakpoints": []any{}}, map[string]any{"breakpoints": []any{}}),
			daptest.ReplyArgs("setExceptionBreakpoints", map[string]any{"filters": []string{"all"}}, map[string]any{}),
		)),
		steps(
			daptest.Expect("kick", nil),
			daptest.Event("stopped", map[string]any{"reason": "breakpoint", "threadId": 1, "allThreadsStopped": true, "hitBreakpointIds": []int{41}}),
			daptest.Respond("kick", nil),
			// The stop first sends what waited for the breakpoint, then asks for the threads.
			bpsReplyFor("/p/b.go", []map[string]any{{"line": 9}}, 42),
			daptest.Reply("threads", map[string]any{"threads": []map[string]any{{"id": 1, "name": "main"}}}),
			daptest.ReplyArgs("stackTrace", map[string]any{"threadId": 1}, frames(1, 1)),
			// Removing the trigger makes the other an ordinary breakpoint: both files are sent again.
			bpsReplyFor("/p/a.go", []map[string]any{}),
			bpsReplyFor("/p/b.go", []map[string]any{{"line": 9}}, 42),
		),
		ending(),
	))
	sess, ok, err := e.start(fakeConfig("trig"))
	if err != nil || !ok {
		t.Fatalf("start: %v %v", ok, err)
	}
	idle(t, sess)
	if b, _ := e.svc.Model().Breakpoint(waiting[0].ID); b.TriggeredBy != trig[0].ID {
		t.Errorf("waiting breakpoint = %+v", b)
	}
	if _, err := sess.CustomRequest(ctx, "kick", nil); err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventStopped, nil)
	if b, _ := e.svc.Model().Breakpoint(waiting[0].ID); !b.Verified {
		t.Errorf("the waiting breakpoint was not sent: %+v", b)
	}
	if err := e.svc.RemoveBreakpoints(ctx, trig[0].ID); err != nil {
		t.Fatal(err)
	}
	if b, _ := e.svc.Model().Breakpoint(waiting[0].ID); b.TriggeredBy != "" {
		t.Errorf("the waiting breakpoint still waits: %+v", b)
	}
	if err := e.svc.Stop(ctx, sess, true, false); err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventSessionEnded, nil)
}

func bpsReplyFor(path string, bps []map[string]any, ids ...int) daptest.Step {
	var out []map[string]any
	for _, id := range ids {
		out = append(out, map[string]any{"id": id, "verified": true})
	}
	if out == nil {
		out = []map[string]any{}
	}
	return daptest.ReplyArgs("setBreakpoints", map[string]any{"source": map[string]any{"path": path}, "breakpoints": bps}, map[string]any{"breakpoints": out})
}
