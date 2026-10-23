package editor

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// scrollRig is an editor over a long document, scrolled from the outside.
type scrollRig struct {
	*mouseRig
}

func newScrollRig(t *testing.T, lines int, tweak func(*Options)) scrollRig {
	t.Helper()
	opts := goOptions()
	if tweak != nil {
		tweak(&opts)
	}
	var text strings.Builder
	for i := range lines {
		text.WriteString("line ")
		text.WriteString(strings.Repeat("x", 3+i%7))
		text.WriteString(" of a long document ")
		text.WriteString(strings.Repeat("0123456789", 12))
		text.WriteString("\n")
	}
	vm := NewViewModel(newTestDoc(text.String()), opts, nil)
	ts := pumpEditor(t, vm, ui.Size{Width: 800, Height: 400})
	g := newGeometry(vm, ts.Dispatcher().TextEngine())
	return scrollRig{&mouseRig{t: t, ts: ts, vm: vm, gutter: vm.GutterWidths(ts.Dispatcher().TextEngine()).Total(), cw: g.charWidth, lineH: opts.LineHeight}}
}

// top is the vertical scroll position the scrollable has.
func (r scrollRig) top() float64 { return r.vm.ScrollTop() }

func (r scrollRig) scroll(delta ui.Offset) {
	r.t.Helper()
	r.ts.ScrollAt(r.at(3, 3), delta)
	r.ts.Pump(time.Millisecond)
}

func TestWheelScrollsFiftyPixelsAnotch(t *testing.T) {
	r := newScrollRig(t, 200, nil)
	r.scroll(ui.Offset{Dy: wheelNotch})
	if r.top() != 50 {
		t.Errorf("one notch scrolled to %v, want 50 (not also the scrollable's own scroll)", r.top())
	}
	r.scroll(ui.Offset{Dy: 2 * wheelNotch})
	if r.top() != 150 {
		t.Errorf("three notches: %v", r.top())
	}
	r.scroll(ui.Offset{Dy: -wheelNotch})
	if r.top() != 100 {
		t.Errorf("a notch back: %v", r.top())
	}
	noErrors(t, r.ts)
}

func TestWheelHighResolutionDeltas(t *testing.T) {
	cases := []struct {
		name  string
		delta float64
		want  float64
	}{
		{"a tenth of a notch", wheelNotch / 10, 5},
		{"a hundredth is rounded away from zero", wheelNotch / 100, 1},
		{"a half notch", wheelNotch / 2, 25},
		{"backwards below the top does nothing", -wheelNotch, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newScrollRig(t, 200, nil)
			r.scroll(ui.Offset{Dy: tc.delta})
			if r.top() != tc.want {
				t.Errorf("scrolled to %v, want %v", r.top(), tc.want)
			}
		})
	}
}

func TestWheelStopsAtTheEnds(t *testing.T) {
	r := newScrollRig(t, 30, nil)
	r.scroll(ui.Offset{Dy: -wheelNotch})
	if r.top() != 0 {
		t.Errorf("above the top: %v", r.top())
	}
	r.scroll(ui.Offset{Dy: 100 * wheelNotch})
	max := r.top()
	// The last line can reach the top of the editor (scrollBeyondLastLine).
	if want := 31*19 - 19.0; max != want {
		t.Errorf("at the end: %v, want %v", max, want)
	}
	r.scroll(ui.Offset{Dy: wheelNotch})
	if r.top() != max {
		t.Errorf("past the end: %v", r.top())
	}
}

func TestWheelSensitivity(t *testing.T) {
	r := newScrollRig(t, 200, func(o *Options) { o.MouseWheelScrollSensitivity = 2 })
	r.scroll(ui.Offset{Dy: wheelNotch})
	if r.top() != 100 {
		t.Errorf("a notch at sensitivity 2: %v", r.top())
	}
	r = newScrollRig(t, 200, func(o *Options) { o.MouseWheelScrollSensitivity = 0.5 })
	r.scroll(ui.Offset{Dy: wheelNotch})
	if r.top() != 25 {
		t.Errorf("a notch at sensitivity 0.5: %v", r.top())
	}
}

func TestAltWheelScrollsFaster(t *testing.T) {
	r := newScrollRig(t, 400, nil)
	release := r.holdAlt()
	r.scroll(ui.Offset{Dy: wheelNotch})
	release()
	if r.top() != 250 {
		t.Errorf("a notch with Alt: %v, want 250", r.top())
	}
	r = newScrollRig(t, 400, func(o *Options) { o.FastScrollSensitivity = 2 })
	release = r.holdAlt()
	r.scroll(ui.Offset{Dy: wheelNotch})
	release()
	if r.top() != 100 {
		t.Errorf("fastScrollSensitivity 2: %v", r.top())
	}
}

func TestShiftWheelScrollsSideways(t *testing.T) {
	r := newScrollRig(t, 200, nil)
	release := r.holdShift()
	r.scroll(ui.Offset{Dy: wheelNotch})
	release()
	if r.top() != 0 || r.vm.ScrollLeft() != 50 {
		t.Errorf("Shift+wheel: top %v left %v", r.top(), r.vm.ScrollLeft())
	}
	// A wheel that scrolls sideways itself does so without Shift.
	r.scroll(ui.Offset{Dx: wheelNotch})
	if r.vm.ScrollLeft() != 100 {
		t.Errorf("a sideways wheel: %v", r.vm.ScrollLeft())
	}
	r.scroll(ui.Offset{Dx: -10 * wheelNotch})
	if r.vm.ScrollLeft() != 0 {
		t.Errorf("back to the left edge: %v", r.vm.ScrollLeft())
	}
}

func TestWheelOverTheGutterScrolls(t *testing.T) {
	r := newScrollRig(t, 200, nil)
	r.ts.ScrollAt(ui.Offset{Dx: 5, Dy: 100}, ui.Offset{Dy: wheelNotch})
	r.ts.Pump(time.Millisecond)
	if r.top() != 50 {
		t.Errorf("over the gutter: %v", r.top())
	}
}

func TestTrackpadPansTheEditor(t *testing.T) {
	r := newScrollRig(t, 200, nil)
	g := r.ts.StartTrackpadGesture(r.at(3, 3))
	g.PanBy(ui.Offset{Dy: -40}) // the fingers go up: the text goes down
	r.ts.Pump(time.Millisecond)
	if r.top() != 40 {
		t.Errorf("a pan of 40 pixels: %v (the pan is 1:1, and the scrollable does not scroll too)", r.top())
	}
	g.PanBy(ui.Offset{Dx: -25, Dy: 10})
	r.ts.Pump(time.Millisecond)
	if r.top() != 30 || r.vm.ScrollLeft() != 25 {
		t.Errorf("a diagonal pan: top %v left %v", r.top(), r.vm.ScrollLeft())
	}
	g.End()
	noErrors(t, r.ts)
}

func TestTrackpadSensitivity(t *testing.T) {
	r := newScrollRig(t, 200, func(o *Options) { o.MouseWheelScrollSensitivity = 3 })
	g := r.ts.StartTrackpadGesture(r.at(3, 3))
	g.PanBy(ui.Offset{Dy: -10})
	g.End()
	if r.top() != 30 {
		t.Errorf("sensitivity 3: %v", r.top())
	}
}

func TestSmoothScrollingAnimates(t *testing.T) {
	r := newScrollRig(t, 200, func(o *Options) { o.SmoothScrolling = true })
	r.ts.ScrollAt(r.at(3, 3), ui.Offset{Dy: wheelNotch})
	r.ts.Pump()
	if r.top() != 0 {
		t.Errorf("the animation starts at the top: %v", r.top())
	}
	r.ts.Pump(smoothScrollDuration / 2)
	mid := r.top()
	if mid <= 0 || mid >= 50 {
		t.Errorf("halfway through: %v", mid)
	}
	r.ts.Pump(smoothScrollDuration)
	if r.top() != 50 {
		t.Errorf("at the end of the animation: %v", r.top())
	}
	if r.smoothTarget(t) != nil {
		t.Error("the animation is over")
	}
}

func (r scrollRig) smoothTarget(t *testing.T) *float64 {
	t.Helper()
	el, ok := r.ts.Element(gluttertest.ByType[*Editor]()).(*widgets.StatefulElement)
	if !ok {
		t.Fatal("the editor is a stateful widget")
	}
	return el.State().(*editorState).smoothTarget
}

func TestSmoothScrollingAddsUp(t *testing.T) {
	r := newScrollRig(t, 200, func(o *Options) { o.SmoothScrolling = true })
	for range 3 {
		r.ts.ScrollAt(r.at(3, 3), ui.Offset{Dy: wheelNotch})
		r.ts.Pump(10 * time.Millisecond)
	}
	r.ts.Pump(time.Second)
	if r.top() != 150 {
		t.Errorf("three notches during one animation: %v", r.top())
	}
}

func TestSmoothScrollingOffMovesAtOnce(t *testing.T) {
	r := newScrollRig(t, 200, nil)
	r.ts.ScrollAt(r.at(3, 3), ui.Offset{Dy: wheelNotch})
	if r.top() != 50 {
		t.Errorf("without smooth scrolling: %v", r.top())
	}
}

func TestSmoothScrollingDoesNotAnimateAPan(t *testing.T) {
	r := newScrollRig(t, 200, func(o *Options) { o.SmoothScrolling = true })
	g := r.ts.StartTrackpadGesture(r.at(3, 3))
	g.PanBy(ui.Offset{Dy: -40})
	g.End()
	if r.top() != 40 {
		t.Errorf("a pan follows the fingers: %v", r.top())
	}
}

func TestScrollSpeedOfADrag(t *testing.T) {
	cases := []struct {
		distance, viewport, want float64
	}{
		{0.5, 20, 30},         // close: at least 30 lines a second
		{1, 20, 40},           // viewport*(1+1)
		{1.5, 40, 100},        // viewport*(1+1.5)
		{2, 10, 60},           // at least 60
		{3, 20, 100},          // viewport*(2+3)
		{5, 30, 360},          // viewport*(7+5)
		{10, 5, 200},          // at least 200
		{1.51, 20, 20 * 3.51}, // the next band starts above 1.5
	}
	for _, tc := range cases {
		if got := scrollSpeed(tc.distance, tc.viewport); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("scrollSpeed(%v, %v) = %v, want %v", tc.distance, tc.viewport, got, tc.want)
		}
	}
}

func TestDragBelowTheEditorScrollsAndSelects(t *testing.T) {
	r := newScrollRig(t, 200, nil)
	g := r.ts.StartGesture(r.at(2, 3), mouseOpts())
	g.MoveTo(ui.Offset{Dx: r.gutter + 5*r.cw, Dy: 440}) // 40 pixels below the editor
	r.ts.Pump()                                         // the first frame only starts counting
	r.ts.Pump(16 * time.Millisecond)
	first := r.top()
	if first <= 0 {
		t.Fatalf("the editor did not scroll: %v", first)
	}
	for range 10 {
		r.ts.Pump(16 * time.Millisecond)
	}
	if r.top() <= first {
		t.Errorf("it goes on scrolling: %v then %v", first, r.top())
	}
	sel := r.vm.Cursors().Primary().Selection
	bottom := r.vm.Projection().RowAt(r.top() + 400 - 0.1)
	if sel.Anchor != positionOf(2, 3) || sel.Active.Line != bottom {
		t.Errorf("selection %v, want from (2,3) to the bottom row %d", sel, bottom)
	}
	// The pointer stays still but the selection follows the edge as it scrolls.
	at := r.top()
	g.Up()
	r.ts.Pump(200 * time.Millisecond)
	if r.top() != at {
		t.Errorf("releasing the button stops the scrolling: %v then %v", at, r.top())
	}
	noErrors(t, r.ts)
}

func TestDragAboveTheEditorScrollsUp(t *testing.T) {
	r := newScrollRig(t, 200, nil)
	r.scroll(ui.Offset{Dy: 10 * wheelNotch}) // 500 pixels down
	g := r.ts.StartGesture(r.at(10, 3), mouseOpts())
	g.MoveTo(ui.Offset{Dx: r.gutter + 5*r.cw, Dy: -30})
	r.ts.Pump()
	for range 8 {
		r.ts.Pump(16 * time.Millisecond)
	}
	if r.top() >= 500 {
		t.Errorf("the editor did not scroll up: %v", r.top())
	}
	sel := r.vm.Cursors().Primary().Selection
	top := r.vm.Projection().RowAt(r.top())
	if sel.Active.Line != top || sel.Active.Col != 0 && sel.Active.Col != 5 {
		t.Errorf("selection %v, want the top row %d", sel, top)
	}
	g.Up()
}

func TestDragScrollingStopsAtTheTop(t *testing.T) {
	r := newScrollRig(t, 200, nil)
	g := r.ts.StartGesture(r.at(3, 3), mouseOpts())
	g.MoveTo(ui.Offset{Dx: r.gutter + 10*r.cw, Dy: -20})
	r.ts.Pump()
	for range 5 {
		r.ts.Pump(16 * time.Millisecond)
	}
	if r.top() != 0 {
		t.Errorf("scrolled above the top: %v", r.top())
	}
	// It cannot scroll further, so the selection goes to the start of the first line.
	if sel := r.vm.Cursors().Primary().Selection; sel.Active != positionOf(0, 0) {
		t.Errorf("selection %v", sel)
	}
	g.Up()
}

func TestDragBackInsideStopsScrolling(t *testing.T) {
	r := newScrollRig(t, 200, nil)
	g := r.ts.StartGesture(r.at(2, 3), mouseOpts())
	g.MoveTo(ui.Offset{Dx: r.gutter, Dy: 460})
	r.ts.Pump()
	r.ts.Pump(16 * time.Millisecond)
	r.ts.Pump(16 * time.Millisecond)
	g.MoveTo(r.at(5, 3))
	at := r.top()
	r.ts.Pump(100 * time.Millisecond)
	if r.top() != at {
		t.Errorf("still scrolling after the pointer came back: %v then %v", at, r.top())
	}
	if sel := r.vm.Cursors().Primary().Selection; sel.Active.Line != r.vm.Projection().RowAt(r.top()+5*19+9) {
		t.Errorf("selection %v follows the pointer", sel)
	}
	g.Up()
}

func TestDragRightOfTheEditorScrollsSideways(t *testing.T) {
	r := newScrollRig(t, 50, nil)
	g := r.ts.StartGesture(r.at(1, 3), mouseOpts())
	g.MoveTo(ui.Offset{Dx: 830, Dy: r.lineH*1 + 9})
	r.ts.Pump()
	for range 10 {
		r.ts.Pump(16 * time.Millisecond)
	}
	if r.vm.ScrollLeft() <= 0 {
		t.Fatalf("no sideways scroll: %v", r.vm.ScrollLeft())
	}
	sel := r.vm.Cursors().Primary().Selection
	line, _ := r.vm.Document().Snapshot().LineContent(1)
	if sel.Active != positionOf(1, len(line)) {
		t.Errorf("selection %v, want to the end of line 1", sel)
	}
	g.MoveTo(ui.Offset{Dx: -30, Dy: r.lineH*1 + 9})
	for range 60 {
		r.ts.Pump(16 * time.Millisecond)
	}
	if r.vm.ScrollLeft() != 0 {
		t.Errorf("scrolled back: %v", r.vm.ScrollLeft())
	}
	if sel := r.vm.Cursors().Primary().Selection; sel.Active != positionOf(1, 0) {
		t.Errorf("selection %v, want to the start of line 1", sel)
	}
	g.Up()
}
