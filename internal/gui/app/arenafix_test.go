package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// slowFrameClicks plays what Emetos sees when a frame is slow and the mouse events wait for it: the Linux platform gives
// every mouse event the pointer id 0, and Glutter dispatches the events that waited in one go, before the next turn of the
// event loop. A press on a tap target, a move far past the slop (the tap rejects itself), the release and a second press,
// and then the turn of the loop that the first press posted.
func slowFrameClicks(ts *gluttertest.Tester, at ui.Offset) (panicked any) {
	g := ts.Binding.Gestures()
	event := func(newEvent func(gestures.PointerEventOpts) gestures.PointerEvent, pos, delta ui.Offset) {
		g.HandlePointerEvent(newEvent(gestures.PointerEventOpts{
			TimeStamp: ts.Elapsed(), Pointer: 0, Kind: ui.PointerDeviceKindMouse, Position: pos, Delta: delta,
		}))
	}
	g.HandlePointerEvent(gestures.NewPointerAddedEvent(gestures.PointerEventOpts{
		TimeStamp: ts.Elapsed(), Pointer: 0, Kind: ui.PointerDeviceKindMouse, Position: at,
	}))
	away := at.Add(ui.Offset{Dx: 150})
	event(func(o gestures.PointerEventOpts) gestures.PointerEvent { return gestures.NewPointerDownEvent(o) }, at, ui.Offset{})
	event(func(o gestures.PointerEventOpts) gestures.PointerEvent { return gestures.NewPointerMoveEvent(o) }, away, away.Sub(at))
	event(func(o gestures.PointerEventOpts) gestures.PointerEvent { return gestures.NewPointerUpEvent(o) }, away, ui.Offset{})
	event(func(o gestures.PointerEventOpts) gestures.PointerEvent { return gestures.NewPointerDownEvent(o) }, at, away.Sub(at))
	defer func() { panicked = recover() }()
	ts.FlushPosted() // the turn of the loop
	return nil
}

// TestGestureArenaSurvivesPointerIDReuseInOneTurn reproduces the crash of the desktop test of 2026-10-27 (Glutter 1.9.1:
// `index out of range [0] with length 0` in gestures/arena.go:309). Without fixGestureArena the process panics in the turn
// of the loop; with it nothing happens, and the target still takes the clicks that follow.
func TestGestureArenaSurvivesPointerIDReuseInOneTurn(t *testing.T) {
	for _, tc := range []struct {
		name      string
		fix       bool
		wantPanic bool
	}{
		{name: "Glutter alone panics", fix: false, wantPanic: true},
		{name: "with the workaround", fix: true, wantPanic: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ts := testutil.New(t, ui.Size{Width: 200, Height: 100})
			taps := 0
			ts.PumpWidget(widgets.NewGestureDetector(widgets.NewSizedBoxExpand(nil), widgets.GestureDetectorOpts{
				Behavior: rendering.HitTestBehaviorOpaque, OnTap: func() { taps++ },
			}))
			if tc.fix {
				fixGestureArena(ts.Binding)
			}
			at := ui.Offset{Dx: 50, Dy: 50}
			got := slowFrameClicks(ts, at)
			if (got != nil) != tc.wantPanic {
				t.Fatalf("panic = %v, want a panic: %v", got, tc.wantPanic)
			}
			if tc.wantPanic {
				if msg := fmt.Sprint(got); msg != "runtime error: index out of range [0] with length 0" {
					t.Errorf("panic = %q, want the index error of arena.go:309", msg)
				}
				return
			}
			// the second press is still down: release it, and then click again
			g := ts.Binding.Gestures()
			up := gestures.NewPointerUpEvent(gestures.PointerEventOpts{TimeStamp: ts.Elapsed(), Pointer: 0, Kind: ui.PointerDeviceKindMouse, Position: at})
			g.HandlePointerEvent(up)
			ts.FlushPosted()
			if taps != 1 {
				t.Errorf("the second click gave %d taps, want 1 (the arena of the second press was resolved by its own member)", taps)
			}
			ts.Pump(time.Second)
			click := func(newEvent func(gestures.PointerEventOpts) gestures.PointerEvent) {
				g.HandlePointerEvent(newEvent(gestures.PointerEventOpts{TimeStamp: ts.Elapsed(), Pointer: 0, Kind: ui.PointerDeviceKindMouse, Position: at}))
			}
			click(func(o gestures.PointerEventOpts) gestures.PointerEvent { return gestures.NewPointerDownEvent(o) })
			click(func(o gestures.PointerEventOpts) gestures.PointerEvent { return gestures.NewPointerUpEvent(o) })
			ts.FlushPosted()
			if taps != 2 {
				t.Errorf("a click after the sequence gave %d taps in all, want 2", taps)
			}
			if errs := ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("the framework reported errors: %v", errs)
			}
		})
	}
}

// arenaMember is a member of a gesture arena that counts what it is told.
type arenaMember struct{ accepted, rejected int }

func (m *arenaMember) AcceptGesture(int) { m.accepted++ }
func (m *arenaMember) RejectGesture(int) { m.rejected++ }

// reusePointerBeforeTheTurn is the sequence of slowFrameClicks on the arena itself, with no target: the only member of the
// arena of pointer 0 gives up after the arena was closed, the next event of the pointer opens a new arena, and the turn of
// the loop that the first close posted comes after. It says whether the turn panicked, and whether the last member (of the
// second arena) was then accepted when that arena closed.
func reusePointerBeforeTheTurn(g *gestures.GestureBinding, flush func()) (panicked any, accepted int) {
	first, second := &arenaMember{}, &arenaMember{}
	entry := g.GestureArena.Add(0, first)
	g.GestureArena.Close(0)
	entry.Resolve(gestures.GestureRejected)
	g.DispatchEvent(gestures.NewPointerDownEvent(gestures.PointerEventOpts{Kind: ui.PointerDeviceKindMouse}), gestures.NewHitTestResult())
	g.GestureArena.Add(0, second)
	func() {
		defer func() { panicked = recover() }()
		flush()
	}()
	if panicked != nil {
		return panicked, 0
	}
	g.GestureArena.Close(0)
	flush()
	return nil, second.accepted
}

// TestTheArenaIsFixedInTheApplicationAndInTheView pins that the two roots of Emetos's bindings (the application's, under
// the tray, and each headless view's) install fixGestureArena: with Glutter's own arena the sequence panics.
func TestTheArenaIsFixedInTheApplicationAndInTheView(t *testing.T) {
	t.Run("a binding without the workaround panics", func(t *testing.T) {
		ts := testutil.New(t, ui.Size{Width: 100, Height: 100})
		ts.PumpWidget(widgets.NewSizedBoxExpand(nil))
		got, _ := reusePointerBeforeTheTurn(ts.Binding.Gestures(), ts.FlushPosted)
		if got == nil {
			t.Fatal("Glutter's arena did not panic: the workaround may no longer be needed (docs/GLUTTER-ISSUES.md)")
		}
	})
	t.Run("the application", func(t *testing.T) {
		env := newMultiEnv(t, nil)
		p := newTestProcess(ProcessOptions{Paths: env.paths, StateAfter: env.clock.AfterFunc})
		m := p.Workspaces
		m.ctx = t.Context()
		shown, err := m.start(t.Context(), Options{})
		if err != nil {
			t.Fatal(err)
		}
		shown.steps = nil
		ts := testutil.New(t, ui.Size{Width: 1000, Height: 700})
		ts.FakeDesktop() // the tray of the test is not the machine's
		ts.PumpWidget(m.rootWidget())
		got, accepted := reusePointerBeforeTheTurn(ts.Binding.Gestures(), ts.FlushPosted)
		if got != nil || accepted != 1 {
			t.Errorf("panic = %v, accepted = %d, want no panic and 1", got, accepted)
		}
	})
	t.Run("a headless view", func(t *testing.T) {
		r := newViewRig(t, "alpha\n", extension.Size{Width: 400, Height: 300})
		got, accepted := reusePointerBeforeTheTurn(r.view.ov.Binding().Gestures(), r.ts.FlushPosted)
		if got != nil || accepted != 1 {
			t.Errorf("panic = %v, accepted = %d, want no panic and 1", got, accepted)
		}
	})
}
