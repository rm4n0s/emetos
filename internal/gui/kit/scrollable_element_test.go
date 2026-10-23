package kit

import (
	"image/color"
	"math"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
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
		NewScrollableElement(widgets.NewColoredBox(colors.EditorBackground, widgets.NewSizedBox(nil)), model, opts))))
	ts.Pump(time.Millisecond)
	return ts
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
			NewScrollableElement(widgets.NewColoredBox(colors.EditorBackground, widgets.NewSizedBox(nil)), m, ScrollableElementOpts{Vertical: ScrollbarVisible}))))
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
