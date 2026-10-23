package kit

import (
	"image/color"
	"math"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// fakeModel is a ScrollModel with fixed sizes and a position the element moves.
type fakeModel struct {
	foundation.ChangeNotifier
	m ScrollMetrics
}

func newFakeModel(m ScrollMetrics) *fakeModel {
	return &fakeModel{ChangeNotifier: *foundation.NewChangeNotifier(), m: m}
}

func (f *fakeModel) Metrics() ScrollMetrics { return f.m }

func (f *fakeModel) ScrollTo(left, top float64) {
	f.m.Left = math.Min(math.Max(left, 0), math.Max(f.m.ContentWidth-f.m.ViewportWidth, 0))
	f.m.Top = math.Min(math.Max(top, 0), math.Max(f.m.ContentHeight-f.m.ViewportHeight, 0))
	f.NotifyListeners()
}

var (
	elementBackground = color.NRGBA{R: 0x1F, G: 0x1F, B: 0x1F, A: 0xFF}
	viewSize          = ui.Size{Width: 400, Height: 300}
)

// pumpElement shows an element over a plain child in a window of viewSize.
func pumpElement(t *testing.T, model ScrollModel, opts ScrollableElementOpts) *gluttertest.Tester {
	t.Helper()
	ts := gluttertest.New(t, gluttertest.Opts{Size: viewSize})
	colors := DarkModern()
	ts.PumpWidget(widgets.NewDirectionality(ui.TextDirectionLTR, NewTheme(colors,
		NewScrollableElement(widgets.NewColoredBox(colors.EditorBackground, widgets.NewSizedBox(nil)), editorScrollbars(model, opts)))))
	ts.Pump(time.Millisecond)
	return ts
}

// editorScrollbars gives the options a model and the sizes of the scrollbars of an editor (14 by
// 12), which the tests below are written for, unless they have sizes.
func editorScrollbars(model ScrollModel, opts ScrollableElementOpts) ScrollableElementOpts {
	opts.Model = model
	if !opts.VerticalSize.IsSome() {
		opts.VerticalSize = foundation.Some(DefaultVerticalScrollbarSize)
	}
	if !opts.HorizontalSize.IsSome() {
		opts.HorizontalSize = foundation.Some(DefaultHorizontalScrollbarSize)
	}
	return opts
}

func pixelAt(ts *gluttertest.Tester, x, y int) color.NRGBA {
	return color.NRGBAModel.Convert(ts.Screenshot().At(x, y)).(color.NRGBA)
}

func tall() ScrollMetrics {
	return ScrollMetrics{ViewportWidth: 400, ViewportHeight: 300, ContentWidth: 400, ContentHeight: 1200}
}

func wide() ScrollMetrics {
	return ScrollMetrics{ViewportWidth: 400, ViewportHeight: 300, ContentWidth: 1600, ContentHeight: 300}
}

// The vertical slider of tall() is 75 high and moves 225 pixels over 900 of scroll; it is in the
// 14 pixels at the right edge. Both are read as the pixel at (393, y).
func sliderPixel(ts *gluttertest.Tester, y int) color.NRGBA { return pixelAt(ts, 393, y) }

func TestScrollbarAppearsWithThePointerAndFades(t *testing.T) {
	ts := pumpElement(t, newFakeModel(tall()), ScrollableElementOpts{})
	ts.Pump(100 * time.Millisecond)
	if got := sliderPixel(ts, 20); got != elementBackground {
		t.Fatalf("a scrollbar nobody asked for is drawn: %v", got)
	}
	ts.Hover(ui.Offset{Dx: 200, Dy: 150})
	ts.Pump(time.Millisecond) // the animation starts at the first frame
	ts.Pump(scrollbarRevealDuration + 20*time.Millisecond)
	shown := sliderPixel(ts, 20)
	if shown == elementBackground {
		t.Fatal("the pointer over the element reveals the scrollbar")
	}
	if got := sliderPixel(ts, 150); got != elementBackground {
		t.Errorf("below the slider there is only the track, which has no color: %v", got)
	}
	// The pointer stays: the scrollbar stays.
	ts.Pump(3 * time.Second)
	if got := sliderPixel(ts, 20); got != shown {
		t.Errorf("the scrollbar went away under the pointer: %v", got)
	}
	// The pointer leaves: half a second, then the fade.
	ts.Hover(ui.Offset{Dx: -20, Dy: -20})
	ts.Pump(scrollbarHideDelay - 50*time.Millisecond)
	if got := sliderPixel(ts, 20); got != shown {
		t.Errorf("the scrollbar waits half a second: %v", got)
	}
	ts.Pump(50*time.Millisecond + time.Millisecond) // the timer fires; the fade starts at the next frame
	ts.Pump(scrollbarFadeDuration / 2)
	if mid := sliderPixel(ts, 20); mid == shown || mid == elementBackground {
		t.Errorf("halfway through the fade the slider is halfway: %v", mid)
	}
	ts.Pump(scrollbarFadeDuration)
	if got := sliderPixel(ts, 20); got != elementBackground {
		t.Errorf("the scrollbar faded away: %v", got)
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestScrollbarAppearsWhenTheContentScrolls(t *testing.T) {
	model := newFakeModel(tall())
	ts := pumpElement(t, model, ScrollableElementOpts{})
	model.ScrollTo(0, 450)
	ts.Pump(time.Millisecond) // the scrollbar learns of it after the frame
	ts.Pump(time.Millisecond)
	ts.Pump(scrollbarRevealDuration + 20*time.Millisecond)
	// 450 of 900 puts the slider (75 long) at 112.5, rounded to 113, 113 to 188.
	if got := sliderPixel(ts, 150); got == elementBackground {
		t.Error("a scroll reveals the scrollbar, with its slider in the middle")
	}
	if got := sliderPixel(ts, 20); got != elementBackground {
		t.Errorf("and not where the slider was: %v", got)
	}
	ts.Pump(scrollbarHideDelay + time.Millisecond)
	ts.Pump(time.Millisecond)
	ts.Pump(scrollbarFadeDuration + 50*time.Millisecond)
	if got := sliderPixel(ts, 150); got != elementBackground {
		t.Errorf("it goes away half a second after: %v", got)
	}
}

func TestScrollbarNeededOnlyWhenContentIsLonger(t *testing.T) {
	fits := tall()
	fits.ContentHeight = 300
	ts := pumpElement(t, newFakeModel(fits), ScrollableElementOpts{Vertical: ScrollbarVisible, Horizontal: ScrollbarVisible})
	ts.Hover(ui.Offset{Dx: 200, Dy: 150})
	ts.Pump(time.Millisecond)
	ts.Pump(200 * time.Millisecond)
	if got := sliderPixel(ts, 20); got != elementBackground {
		t.Errorf("no scrollbar without a need: %v", got)
	}
}

func TestScrollbarVisibility(t *testing.T) {
	cases := []struct {
		name      string
		vertical  ScrollbarVisibility
		hover     bool
		wantShown bool
	}{
		{"auto without the pointer", ScrollbarAuto, false, false},
		{"auto with the pointer", ScrollbarAuto, true, true},
		{"visible without the pointer", ScrollbarVisible, false, true},
		{"hidden with the pointer", ScrollbarHidden, true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := pumpElement(t, newFakeModel(tall()), ScrollableElementOpts{Vertical: tc.vertical})
			if tc.hover {
				ts.Hover(ui.Offset{Dx: 200, Dy: 150})
			}
			ts.Pump(time.Millisecond)
			ts.Pump(300 * time.Millisecond)
			if shown := sliderPixel(ts, 20) != elementBackground; shown != tc.wantShown {
				t.Errorf("shown %v, want %v", shown, tc.wantShown)
			}
		})
	}
}

func TestVerticalSliderDrag(t *testing.T) {
	model := newFakeModel(tall())
	ts := pumpElement(t, model, ScrollableElementOpts{Vertical: ScrollbarVisible})
	resting := sliderPixel(ts, 20)

	g := ts.StartGesture(ui.Offset{Dx: 393, Dy: 30})
	ts.Pump(time.Millisecond)
	if model.m.Top != 0 {
		t.Fatalf("a press on the slider does not scroll: %v", model.m.Top)
	}
	if active := sliderPixel(ts, 20); active == resting || active == elementBackground {
		t.Errorf("a slider that is dragged has the active color: %v (resting %v)", active, resting)
	}
	// 50 pixels of the slider are 200 of the content (225 of track for 900 of scroll).
	g.MoveBy(ui.Offset{Dx: 0, Dy: 50})
	ts.Pump(time.Millisecond)
	if model.m.Top != 200 {
		t.Errorf("dragged by 50: top %v, want 200", model.m.Top)
	}
	// The pointer may leave the scrollbar: the drag goes on.
	g.MoveBy(ui.Offset{Dx: -300, Dy: 50})
	ts.Pump(time.Millisecond)
	if model.m.Top != 400 {
		t.Errorf("dragged by 100 and away: top %v, want 400", model.m.Top)
	}
	g.MoveBy(ui.Offset{Dx: 0, Dy: 1000})
	ts.Pump(time.Millisecond)
	if model.m.Top != 900 {
		t.Errorf("dragged past the end: top %v, want 900", model.m.Top)
	}
	g.Up()
	ts.Pump(time.Millisecond)
	if model.m.Top != 900 {
		t.Errorf("letting go keeps the position: %v", model.m.Top)
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestSliderHoverColor(t *testing.T) {
	ts := pumpElement(t, newFakeModel(tall()), ScrollableElementOpts{Vertical: ScrollbarVisible})
	resting := sliderPixel(ts, 20)
	ts.Hover(ui.Offset{Dx: 393, Dy: 30})
	ts.Pump(time.Millisecond)
	if hover := sliderPixel(ts, 20); hover == resting {
		t.Errorf("the slider changes color under the pointer: %v", hover)
	}
	ts.Hover(ui.Offset{Dx: 200, Dy: 30})
	ts.Pump(time.Millisecond)
	if got := sliderPixel(ts, 20); got != resting {
		t.Errorf("and back when the pointer leaves it: %v, want %v", got, resting)
	}
}

func TestClickOnTheTrack(t *testing.T) {
	cases := []struct {
		name   string
		byPage bool
		at     float64
		want   float64
	}{
		// The slider's center goes to the click: (200 - 37.5) / 0.25.
		{"jump to the click", false, 200, 650},
		{"jump near the end is clamped", false, 299, 900},
		{"jump near the start is clamped", false, 80, 170},
		{"a click below the slider pages down", true, 200, 300},
		{"a click on the slider does not page", true, 30, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			model := newFakeModel(tall())
			ts := pumpElement(t, model, ScrollableElementOpts{Vertical: ScrollbarVisible, ScrollByPage: tc.byPage})
			g := ts.StartGesture(ui.Offset{Dx: 393, Dy: tc.at})
			ts.Pump(time.Millisecond)
			g.Up()
			ts.Pump(time.Millisecond)
			if model.m.Top != tc.want {
				t.Errorf("top %v, want %v", model.m.Top, tc.want)
			}
		})
	}
}

func TestClickOnTheTrackAbovePagesUp(t *testing.T) {
	model := newFakeModel(tall())
	model.m.Top = 600
	ts := pumpElement(t, model, ScrollableElementOpts{Vertical: ScrollbarVisible, ScrollByPage: true})
	g := ts.StartGesture(ui.Offset{Dx: 393, Dy: 10}) // the slider is at 150..225
	ts.Pump(time.Millisecond)
	g.Up()
	if model.m.Top != 300 {
		t.Errorf("a page up from 600: %v", model.m.Top)
	}
}

func TestDragContinuesFromAClickOnTheTrack(t *testing.T) {
	model := newFakeModel(tall())
	ts := pumpElement(t, model, ScrollableElementOpts{Vertical: ScrollbarVisible})
	g := ts.StartGesture(ui.Offset{Dx: 393, Dy: 200}) // jumps to 650
	ts.Pump(time.Millisecond)
	g.MoveBy(ui.Offset{Dx: 0, Dy: -40}) // the slider was at 163 (rounded) and goes up to 123
	ts.Pump(time.Millisecond)
	if model.m.Top != 492 {
		t.Errorf("top %v, want 492", model.m.Top)
	}
	g.Up()
}

func TestHorizontalScrollbar(t *testing.T) {
	model := newFakeModel(wide())
	ts := pumpElement(t, model, ScrollableElementOpts{Horizontal: ScrollbarVisible, Vertical: ScrollbarVisible})
	// 386 pixels of track (the vertical scrollbar's 14 are not available), a slider of 96.
	resting := pixelAt(ts, 20, 294)
	if resting == elementBackground {
		t.Fatal("the horizontal scrollbar is drawn at the bottom")
	}
	if got := pixelAt(ts, 300, 294); got != elementBackground {
		t.Errorf("past the slider there is only the track: %v", got)
	}
	g := ts.StartGesture(ui.Offset{Dx: 20, Dy: 294})
	ts.Pump(time.Millisecond)
	g.MoveBy(ui.Offset{Dx: 24, Dy: 0})
	ts.Pump(time.Millisecond)
	// 24 pixels of the slider are 24 / (290 / 1200) = 99.3 of the content.
	if model.m.Left != 99 || model.m.Top != 0 {
		t.Errorf("dragged by 24: left %v top %v, want 99 and 0", model.m.Left, model.m.Top)
	}
	g.Up()
	ts.Pump(time.Millisecond)
	// A click on the track moves the horizontal position only.
	model.m.Top = 0
	c := ts.StartGesture(ui.Offset{Dx: 300, Dy: 294})
	ts.Pump(time.Millisecond)
	c.Up()
	if want := math.Round((300 - 48.0) / (290.0 / 1200)); model.m.Left != want {
		t.Errorf("click: left %v, want %v", model.m.Left, want)
	}
}

func TestHorizontalScrollbarStartsAfterTheInset(t *testing.T) {
	model := newFakeModel(wide())
	model.m.ViewportWidth = 340
	ts := pumpElement(t, model, ScrollableElementOpts{Horizontal: ScrollbarVisible, InsetLeft: 60})
	if got := pixelAt(ts, 30, 294); got != elementBackground {
		t.Errorf("nothing over the inset: %v", got)
	}
	if got := pixelAt(ts, 70, 294); got == elementBackground {
		t.Error("the slider starts at the inset")
	}
}

func TestScrollShadows(t *testing.T) {
	model := newFakeModel(tall())
	model.m.ContentWidth = 1200
	ts := pumpElement(t, model, ScrollableElementOpts{UseShadows: true, InsetLeft: 20})
	if got := pixelAt(ts, 100, 1); got != elementBackground {
		t.Errorf("no shadow at the top of content that is not scrolled: %v", got)
	}
	model.ScrollTo(0, 100)
	ts.Pump(50 * time.Millisecond)
	ts.Pump(time.Millisecond)
	if got := pixelAt(ts, 100, 1); got.R >= elementBackground.R {
		t.Errorf("a shadow at the top of scrolled content: %v", got)
	}
	if got := pixelAt(ts, 100, 20); got != elementBackground {
		t.Errorf("the shadow is thin: %v", got)
	}
	model.ScrollTo(50, 0)
	ts.Pump(50 * time.Millisecond)
	ts.Pump(time.Millisecond)
	if got := pixelAt(ts, 21, 100); got.R >= elementBackground.R {
		t.Errorf("a shadow at the left of content scrolled sideways: %v", got)
	}
	if got := pixelAt(ts, 5, 100); got != elementBackground {
		t.Errorf("none over the inset: %v", got)
	}
}

func TestScrollableElementFollowsANewModel(t *testing.T) {
	first, second := newFakeModel(tall()), newFakeModel(tall())
	second.m.Top = 600
	ts := gluttertest.New(t, gluttertest.Opts{Size: viewSize})
	colors := DarkModern()
	pump := func(m ScrollModel) {
		ts.PumpWidget(widgets.NewDirectionality(ui.TextDirectionLTR, NewTheme(colors,
			NewScrollableElement(widgets.NewColoredBox(colors.EditorBackground, widgets.NewSizedBox(nil)), editorScrollbars(m, ScrollableElementOpts{Vertical: ScrollbarVisible})))))
		ts.Pump(time.Millisecond)
	}
	pump(first)
	if got := sliderPixel(ts, 20); got == elementBackground {
		t.Fatal("the slider of the first model is at the top")
	}
	pump(second)
	if got := sliderPixel(ts, 20); got != elementBackground {
		t.Errorf("the slider of the second model is not at the top: %v", got)
	}
	if got := sliderPixel(ts, 160); got == elementBackground {
		t.Error("it is at 150 to 225")
	}
	first.ScrollTo(0, 100) // nobody listens to the first any more
	ts.PumpWidget(widgets.NewSizedBox(nil))
	second.ScrollTo(0, 0) // nor to the second once the element is gone
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

// tallKey and wideKey find the child that an element scrolls.
var childKey = foundation.NewKey("scrolled")

// scrolledChild is a child of a size, with a key to find where it is.
func scrolledChild(width, height float64) widgets.Widget {
	opts := widgets.SizedBoxOpts{Key: childKey}
	if width > 0 {
		opts.Width = foundation.Some(width)
	}
	if height > 0 {
		opts.Height = foundation.Some(height)
	}
	return widgets.NewColoredBox(DarkModern().EditorBackground, widgets.NewSizedBox(nil, opts))
}

// pumpPlain shows an element that scrolls a child of its own.
func pumpPlain(t *testing.T, child widgets.Widget, opts ScrollableElementOpts) *gluttertest.Tester {
	t.Helper()
	ts := gluttertest.New(t, gluttertest.Opts{Size: viewSize})
	ts.PumpWidget(widgets.NewDirectionality(ui.TextDirectionLTR, NewTheme(DarkModern(), NewScrollableElement(child, opts))))
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond) // the scroll view's sizes are known after its first layout
	ts.Pump(time.Millisecond)
	return ts
}

// childAt is where the scrolled child is.
func childAt(ts *gluttertest.Tester) ui.Offset { return ts.TopLeft(gluttertest.ByKey(childKey)) }

// settle runs the frames of a smooth scroll and the one after it.
func settle(ts *gluttertest.Tester) {
	ts.Pump(time.Millisecond)
	ts.Pump(scrollSmoothDuration + 50*time.Millisecond)
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
}

func TestWheelDelta(t *testing.T) {
	notch := func(dx, dy float64) ui.Offset { return ui.Offset{Dx: dx * wheelNotch, Dy: dy * wheelNotch} }
	cases := []struct {
		name   string
		delta  ui.Offset
		shift  bool
		alt    bool
		opts   ScrollableElementOpts
		dx, dy float64
	}{
		{"one notch down is 50", notch(0, 1), false, false, ScrollableElementOpts{}, 0, 50},
		{"up", notch(0, -2), false, false, ScrollableElementOpts{}, 0, -100},
		{"sideways", notch(1, 0), false, false, ScrollableElementOpts{}, 50, 0},
		{"the larger axis wins", notch(0.5, 1), false, false, ScrollableElementOpts{}, 0, 50},
		{"both axes without the predominant one", notch(0.5, 1), false, false, ScrollableElementOpts{NoScrollPredominantAxis: true}, 25, 50},
		{"shift makes it sideways", notch(0, 1), true, false, ScrollableElementOpts{}, 50, 0},
		{"shift does not turn a wheel that is sideways", notch(1, 0), true, false, ScrollableElementOpts{}, 50, 0},
		{"alt is five times faster", notch(0, 1), false, true, ScrollableElementOpts{}, 0, 250},
		{"the sensitivity", notch(0, 1), false, false, ScrollableElementOpts{MouseWheelScrollSensitivity: foundation.Some(2.0)}, 0, 100},
		{"the fast sensitivity", notch(0, 1), false, true, ScrollableElementOpts{FastScrollSensitivity: foundation.Some(2.0)}, 0, 100},
		{"a small movement goes a whole pixel", ui.Offset{Dy: 0.5}, false, false, ScrollableElementOpts{}, 0, 1},
		{"and the other way", ui.Offset{Dy: -0.5}, false, false, ScrollableElementOpts{}, 0, -1},
		{"flip the axes", notch(0, 1), false, false, ScrollableElementOpts{FlipAxes: true}, 50, 0},
		{"scroll y to x", notch(0, 1), false, false, ScrollableElementOpts{ScrollYToX: true}, 50, 0},
		{"scroll y to x with opposite values does nothing", notch(1, -1), false, false, ScrollableElementOpts{ScrollYToX: true}, 0, 0},
		{"nothing", ui.Offset{}, false, false, ScrollableElementOpts{}, 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dx, dy := WheelDelta(tc.delta, tc.shift, tc.alt, tc.opts)
			if dx != tc.dx || dy != tc.dy {
				t.Errorf("got %v, %v, want %v, %v", dx, dy, tc.dx, tc.dy)
			}
		})
	}
}

func TestElementScrollsAChildWithTheWheel(t *testing.T) {
	cases := []struct {
		name  string
		child widgets.Widget
		opts  ScrollableElementOpts
		at    ui.Offset
		delta ui.Offset
		want  ui.Offset
	}{
		{"a notch down", scrolledChild(0, 1200), ScrollableElementOpts{}, ui.Offset{Dx: 200, Dy: 150}, ui.Offset{Dy: wheelNotch}, ui.Offset{Dy: -50}},
		{"three notches", scrolledChild(0, 1200), ScrollableElementOpts{}, ui.Offset{Dx: 200, Dy: 150}, ui.Offset{Dy: 3 * wheelNotch}, ui.Offset{Dy: -150}},
		{"up from the top does nothing", scrolledChild(0, 1200), ScrollableElementOpts{}, ui.Offset{Dx: 200, Dy: 150}, ui.Offset{Dy: -wheelNotch}, ui.Offset{}},
		{"stops at the end", scrolledChild(0, 1200), ScrollableElementOpts{}, ui.Offset{Dx: 200, Dy: 150}, ui.Offset{Dy: 40 * wheelNotch}, ui.Offset{Dy: -900}},
		{"sensitivity", scrolledChild(0, 1200), ScrollableElementOpts{MouseWheelScrollSensitivity: foundation.Some(0.5)}, ui.Offset{Dx: 200, Dy: 150}, ui.Offset{Dy: wheelNotch}, ui.Offset{Dy: -25}},
		{"a child that fits does not move", scrolledChild(0, 200), ScrollableElementOpts{}, ui.Offset{Dx: 200, Dy: 150}, ui.Offset{Dy: wheelNotch}, ui.Offset{}},
		{"sideways when the element scrolls sideways", scrolledChild(1600, 0), ScrollableElementOpts{Axes: ScrollAxisHorizontal}, ui.Offset{Dx: 200, Dy: 150}, ui.Offset{Dx: wheelNotch}, ui.Offset{Dx: -50}},
		{"a vertical wheel does not scroll sideways", scrolledChild(1600, 0), ScrollableElementOpts{Axes: ScrollAxisHorizontal}, ui.Offset{Dx: 200, Dy: 150}, ui.Offset{Dy: wheelNotch}, ui.Offset{}},
		{"both axes", scrolledChild(1600, 1200), ScrollableElementOpts{Axes: ScrollAxisBoth}, ui.Offset{Dx: 200, Dy: 150}, ui.Offset{Dy: wheelNotch}, ui.Offset{Dy: -50}},
		{"not handled when turned off", scrolledChild(0, 1200), ScrollableElementOpts{HandleMouseWheel: foundation.Some(false)}, ui.Offset{Dx: 200, Dy: 150}, ui.Offset{Dy: wheelNotch}, ui.Offset{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := pumpPlain(t, tc.child, tc.opts)
			ts.ScrollAt(tc.at, tc.delta)
			settle(ts)
			if got := childAt(ts); got != tc.want {
				t.Errorf("child at %v, want %v", got, tc.want)
			}
			if errs := ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

func TestElementWheelWithModifiers(t *testing.T) {
	ts := pumpPlain(t, scrolledChild(1600, 1200), ScrollableElementOpts{Axes: ScrollAxisBoth})
	at := ui.Offset{Dx: 200, Dy: 150}
	ts.SendKeyDownEvent(services.LogicalKeyboardKeyShiftLeft, gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyShiftLeft})
	ts.ScrollAt(at, ui.Offset{Dy: wheelNotch})
	settle(ts)
	ts.SendKeyUpEvent(services.LogicalKeyboardKeyShiftLeft, gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyShiftLeft})
	if got := childAt(ts); got != (ui.Offset{Dx: -50}) {
		t.Errorf("shift and a vertical wheel scroll sideways: %v", got)
	}
	ts.SendKeyDownEvent(services.LogicalKeyboardKeyAltLeft, gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyAltLeft})
	ts.ScrollAt(at, ui.Offset{Dy: wheelNotch})
	settle(ts)
	ts.SendKeyUpEvent(services.LogicalKeyboardKeyAltLeft, gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyAltLeft})
	if got := childAt(ts); got != (ui.Offset{Dx: -50, Dy: -250}) {
		t.Errorf("alt scrolls five times as far: %v", got)
	}
}

func TestElementWheelIsSmooth(t *testing.T) {
	ts := pumpPlain(t, scrolledChild(0, 1200), ScrollableElementOpts{})
	ts.ScrollAt(ui.Offset{Dx: 200, Dy: 150}, ui.Offset{Dy: wheelNotch})
	ts.Pump(time.Millisecond) // the first frame of the animation
	ts.Pump(scrollSmoothDuration / 2)
	ts.Pump(time.Millisecond)
	mid := childAt(ts).Dy
	if mid >= 0 || mid <= -50 {
		t.Errorf("half way through the scroll the child is half way: %v", mid)
	}
	ts.Pump(scrollSmoothDuration)
	ts.Pump(time.Millisecond)
	if got := childAt(ts).Dy; got != -50 {
		t.Errorf("the scroll ends at 50: %v", got)
	}
	// A second notch during the animation goes on from where the first ends.
	ts.ScrollAt(ui.Offset{Dx: 200, Dy: 150}, ui.Offset{Dy: wheelNotch})
	ts.Pump(time.Millisecond)
	ts.ScrollAt(ui.Offset{Dx: 200, Dy: 150}, ui.Offset{Dy: wheelNotch})
	settle(ts)
	if got := childAt(ts).Dy; got != -150 {
		t.Errorf("two more notches: %v", got)
	}
}

func TestElementWheelWithoutSmoothScrolling(t *testing.T) {
	ts := pumpPlain(t, scrolledChild(0, 1200), ScrollableElementOpts{NoSmoothScroll: true})
	ts.ScrollAt(ui.Offset{Dx: 200, Dy: 150}, ui.Offset{Dy: wheelNotch})
	ts.Pump(time.Millisecond)
	if got := childAt(ts).Dy; got != -50 {
		t.Errorf("at once: %v", got)
	}
}

func TestElementWheelRevealsTheScrollbar(t *testing.T) {
	ts := pumpPlain(t, scrolledChild(0, 1200), ScrollableElementOpts{})
	if got := sliderPixel(ts, 20); got != elementBackground {
		t.Fatalf("hidden at first: %v", got)
	}
	ts.ScrollAt(ui.Offset{Dx: 200, Dy: 150}, ui.Offset{Dy: wheelNotch})
	settle(ts)
	if got := pixelAt(ts, 395, 40); got == elementBackground {
		t.Error("a scroll shows the scrollbar, with the slider at 50/4")
	}
	ts.Hover(ui.Offset{Dx: -20, Dy: -20}) // the pointer leaves
	ts.Pump(scrollbarHideDelay + 10*time.Millisecond)
	ts.Pump(time.Millisecond)
	ts.Pump(scrollbarFadeDuration + 50*time.Millisecond)
	if got := pixelAt(ts, 395, 40); got != elementBackground {
		t.Errorf("and hides it again: %v", got)
	}
}

func TestElementConsumesTheWheel(t *testing.T) {
	// An inner element of 100 high with a child of 150, in an outer element of a tall child.
	inner := func(always bool) widgets.Widget {
		return widgets.NewSizedBox(NewScrollableElement(widgets.NewColoredBox(DarkModern().EditorBackground, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(150.0)})),
			ScrollableElementOpts{AlwaysConsumeMouseWheel: always, NoSmoothScroll: true}), widgets.SizedBoxOpts{Height: foundation.Some(100.0)})
	}
	outerChild := func(always bool) widgets.Widget {
		return widgets.NewColumn([]widgets.Widget{
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Key: childKey, Height: foundation.Some(200.0)}),
			inner(always),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(1000.0)}),
		})
	}
	cases := []struct {
		name   string
		always bool
		want   float64 // where the outer child is after four notches over the inner element
	}{
		// The inner element scrolls its 50, then the next notches go to the outer one.
		{"the wheel goes on to the outer element at the end", false, -150},
		{"a wheel the inner element consumes stays there", true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := pumpPlain(t, outerChild(tc.always), ScrollableElementOpts{NoSmoothScroll: true})
			// scroll the outer element so that the inner one is in view, with the pointer over it
			over := ui.Offset{Dx: 200, Dy: 250}
			for range 4 {
				ts.ScrollAt(over, ui.Offset{Dy: wheelNotch})
				ts.Pump(time.Millisecond)
			}
			if got := childAt(ts).Dy; got != tc.want {
				t.Errorf("the outer child is at %v, want %v", got, tc.want)
			}
		})
	}
}

func TestElementDragsAndClicksOnTheScrollbarsOfAChild(t *testing.T) {
	ts := pumpPlain(t, scrolledChild(0, 1200), ScrollableElementOpts{Vertical: ScrollbarVisible})
	// The slider is 75 long in a track of 300, in the 10 px at the right.
	g := ts.StartGesture(ui.Offset{Dx: 395, Dy: 30})
	ts.Pump(time.Millisecond)
	g.MoveBy(ui.Offset{Dy: 50})
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	if got := childAt(ts).Dy; got != -200 {
		t.Errorf("a slider dragged by 50 moves the child by 200: %v", got)
	}
	g.Up()
	ts.Pump(time.Millisecond)
	// A click on the track puts the center of the slider there: (200 - 37.5) / 0.25.
	ts.TapAt(ui.Offset{Dx: 395, Dy: 200})
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	if got := childAt(ts).Dy; got != -650 {
		t.Errorf("a click on the track: %v", got)
	}
}

func TestElementScrollsBothWaysWithItsScrollbars(t *testing.T) {
	ts := pumpPlain(t, scrolledChild(1600, 1200), ScrollableElementOpts{Axes: ScrollAxisBoth, Vertical: ScrollbarVisible, Horizontal: ScrollbarVisible})
	// The horizontal track is 390 long (the vertical scrollbar's 10 are not available) and its
	// slider 97: it moves 293 over 1200 of scroll.
	g := ts.StartGesture(ui.Offset{Dx: 20, Dy: 295})
	ts.Pump(time.Millisecond)
	g.MoveBy(ui.Offset{Dx: 30})
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	want := math.Round(30 / (293.0 / 1200))
	if got := childAt(ts); got.Dx != -want || got.Dy != 0 {
		t.Errorf("child at %v, want %v", got, ui.Offset{Dx: -want})
	}
	g.Up()
}

func TestElementWithControllers(t *testing.T) {
	ctrl := widgets.NewScrollController()
	items := 40
	build := func() widgets.Widget {
		rows := make([]widgets.Widget, items)
		for i := range rows {
			rows[i] = widgets.NewSizedBox(widgets.NewColoredBox(DarkModern().EditorBackground, widgets.NewSizedBox(nil)), widgets.SizedBoxOpts{Height: foundation.Some(22.0)})
		}
		list := widgets.NewListView(rows, widgets.ListViewOpts{Controller: ctrl})
		return widgets.NewDirectionality(ui.TextDirectionLTR, NewTheme(DarkModern(), NewScrollableElement(list, ScrollableElementOpts{Controller: ctrl})))
	}
	ts := gluttertest.New(t, gluttertest.Opts{Size: viewSize})
	ts.PumpWidget(build())
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	// 40 rows of 22 are 880 in 300: the slider is 102 long (300 * 300 / 880) over a track of 300.
	if got := pixelAt(ts, 395, 20); got != elementBackground {
		t.Fatalf("the scrollbar waits for the pointer: %v", got)
	}
	ts.Hover(ui.Offset{Dx: 200, Dy: 100})
	ts.Pump(time.Millisecond)
	ts.Pump(scrollbarRevealDuration + 20*time.Millisecond)
	if got := pixelAt(ts, 395, 20); got == elementBackground {
		t.Fatal("the scrollbar of the list shows under the pointer")
	}
	// The list's own wheel scrolls it, and the slider follows.
	ts.ScrollAt(ui.Offset{Dx: 200, Dy: 100}, ui.Offset{Dy: 100})
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	if ctrl.Offset() == 0 {
		t.Fatal("the list scrolls with the wheel")
	}
	pos := ctrl.Offset()
	ts.Pump(time.Millisecond)
	if got := pixelAt(ts, 395, 20); got != elementBackground {
		t.Errorf("the slider moved down with the list: %v", got)
	}
	// Dragging the slider scrolls the list.
	ctrl.JumpTo(0)
	ts.Pump(time.Millisecond)
	g := ts.StartGesture(ui.Offset{Dx: 395, Dy: 30})
	ts.Pump(time.Millisecond)
	g.MoveBy(ui.Offset{Dy: 30})
	ts.Pump(time.Millisecond)
	want := math.Round(30 / ((300 - 102.0) / (880 - 300)))
	if got := ctrl.Offset(); math.Abs(got-want) > 1 {
		t.Errorf("a slider dragged by 30 scrolls the list to %v, want about %v", got, want)
	}
	g.Up()
	_ = pos

	// A list that grows gets a scrollbar when it needs one.
	items = 5
	ts.PumpWidget(build())
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	ts.Hover(ui.Offset{Dx: 200, Dy: 101})
	ts.Pump(time.Millisecond)
	ts.Pump(200 * time.Millisecond)
	if got := pixelAt(ts, 395, 20); got != elementBackground {
		t.Errorf("a list that fits has no scrollbar: %v", got)
	}
	items = 100
	ts.PumpWidget(build())
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	ts.Pump(200 * time.Millisecond)
	if got := pixelAt(ts, 395, 20); got == elementBackground {
		t.Error("a list that grew needs its scrollbar")
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestScrollControllerModel(t *testing.T) {
	m := NewScrollControllerModel(nil, nil)
	if got := m.Metrics(); got != (ScrollMetrics{}) {
		t.Errorf("without controllers: %v", got)
	}
	m.ScrollTo(10, 10) // nothing to move
	notified := 0
	ctrl := widgets.NewScrollController()
	model := NewScrollControllerModel(ctrl, nil)
	model.AddListener(func() { notified++ })
	ts := gluttertest.New(t, gluttertest.Opts{Size: viewSize})
	ts.PumpWidget(widgets.NewDirectionality(ui.TextDirectionLTR, widgets.NewSingleChildScrollView(
		widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(900.0)}), widgets.SingleChildScrollViewOpts{Controller: ctrl})))
	ts.Pump(time.Millisecond)
	if got := model.Metrics(); got.ViewportHeight != 300 || got.ContentHeight != 900 || got.Top != 0 {
		t.Errorf("metrics %+v", got)
	}
	model.ScrollTo(0, 5000)
	ts.Pump(time.Millisecond)
	if got := model.Metrics(); got.Top != 600 {
		t.Errorf("the model keeps the position within the content: %+v", got)
	}
	if notified == 0 {
		t.Error("the model tells its listeners about the controller")
	}
	model.ScrollTo(0, -10)
	if got := model.Metrics().Top; got != 0 {
		t.Errorf("not before the start: %v", got)
	}
	model.Dispose()
	before := notified
	ctrl.JumpTo(100)
	if notified != before {
		t.Error("a disposed model does not listen")
	}
}

func TestElementDefaultSizes(t *testing.T) {
	// Without sizes the scrollbars are 10 thick, as VS Code's; an editor asks for its own.
	ts := pumpPlain(t, scrolledChild(0, 1200), ScrollableElementOpts{Vertical: ScrollbarVisible})
	if got := pixelAt(ts, 390, 20); got == elementBackground {
		t.Error("the slider starts 10 px from the edge")
	}
	if got := pixelAt(ts, 389, 20); got != elementBackground {
		t.Errorf("and not before: %v", got)
	}
}

func TestElementTrackBackground(t *testing.T) {
	colors := DarkModern()
	colors.ScrollbarBackground = ui.NewColor(0xFF336699)
	ts := gluttertest.New(t, gluttertest.Opts{Size: viewSize})
	ts.PumpWidget(widgets.NewDirectionality(ui.TextDirectionLTR, NewTheme(colors,
		NewScrollableElement(scrolledChild(0, 1200), ScrollableElementOpts{Vertical: ScrollbarVisible}))))
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	if got := pixelAt(ts, 395, 200); got != (color.NRGBA{R: 0x33, G: 0x66, B: 0x99, A: 0xFF}) {
		t.Errorf("the track is painted in scrollbar.background: %v", got)
	}
}

func TestElementShadowsOfAChild(t *testing.T) {
	ts := pumpPlain(t, scrolledChild(0, 1200), ScrollableElementOpts{UseShadows: true, NoSmoothScroll: true})
	if got := pixelAt(ts, 100, 1); got != elementBackground {
		t.Errorf("no shadow at the top of content that is not scrolled: %v", got)
	}
	ts.ScrollAt(ui.Offset{Dx: 200, Dy: 150}, ui.Offset{Dy: wheelNotch})
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	if got := pixelAt(ts, 100, 1); got.R >= elementBackground.R {
		t.Errorf("a shadow at the top of scrolled content: %v", got)
	}
}

func TestElementPanZoomScrolls(t *testing.T) {
	ts := pumpPlain(t, scrolledChild(0, 1200), ScrollableElementOpts{})
	g := ts.StartTrackpadGesture(ui.Offset{Dx: 200, Dy: 150})
	g.PanBy(ui.Offset{Dy: -40})
	ts.Pump(time.Millisecond)
	g.End()
	ts.Pump(time.Millisecond)
	if got := childAt(ts).Dy; got != -40 {
		t.Errorf("fingers moving up scroll the content down by 40: %v", got)
	}
}
