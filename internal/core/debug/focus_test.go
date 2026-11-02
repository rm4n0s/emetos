package debug_test

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/debug"
)

func TestFocusAcrossSessions(t *testing.T) {
	e := newEnv(t, nil)
	ctx := ctxT(t)
	for _, name := range []string{"A", "B"} {
		e.script(name, join(handshake(fullCaps, noBreakpoints(fullCaps)), stoppedAt("breakpoint", 2), ending()))
	}
	a, _, err := e.start(fakeConfig("A"))
	if err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventStopped, nil)
	m := e.svc.Model()
	if f := m.Focus(); f.Session != a || f.Thread != a.Thread(1) || f.Frame != a.Thread(1).TopStackFrame() {
		t.Fatalf("focus after the first stop = %+v", f)
	}

	// A second session that stops does not take the focus away.
	b, _, err := e.start(fakeConfig("B"))
	if err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventStopped, func(ev debug.Event) bool { return ev.Session == b })
	if f := m.Focus(); f.Session != a {
		t.Errorf("focus after the second stop = %+v, want session A", f)
	}
	if got := e.svc.State(); got != debug.Stopped {
		t.Errorf("state = %v", got)
	}

	// The user focuses the other session's second frame.
	frame := b.Thread(1).CallStack()[1]
	e.svc.FocusStackFrame(frame, nil, nil)
	if f := m.Focus(); f.Session != b || f.Thread != b.Thread(1) || f.Frame != frame {
		t.Errorf("focus on a frame = %+v", f)
	}
	// Only a session: its stopped thread and top frame.
	e.svc.FocusStackFrame(nil, nil, a)
	if f := m.Focus(); f.Session != a || f.Thread != a.Thread(1) || f.Frame != a.Thread(1).TopStackFrame() {
		t.Errorf("focus on a session = %+v", f)
	}

	// The focused session ends: the focus goes to the one that is left, with its stopped thread.
	if err := e.svc.Stop(ctx, a, true, false); err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventSessionEnded, nil)
	if f := m.Focus(); f.Session != b || f.Thread != b.Thread(1) {
		t.Errorf("focus after A ended = %+v", f)
	}
	if err := e.svc.Stop(ctx, b, true, false); err != nil {
		t.Fatal(err)
	}
	e.wait(debug.EventSessionEnded, nil)
	if f := m.Focus(); f.Session != nil || f.Thread != nil || f.Frame != nil {
		t.Errorf("focus after both ended = %+v", f)
	}
}
