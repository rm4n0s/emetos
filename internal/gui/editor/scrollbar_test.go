package editor

import (
	"image/color"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// wideDoc has a line of 300 characters among short ones.
func wideDoc(lines int) string {
	var b strings.Builder
	for i := range lines {
		if i == 3 {
			b.WriteString(strings.Repeat("0123456789", 30))
		} else {
			b.WriteString("short line")
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// revealScrollbars moves the pointer over the editor and lets the scrollbars appear.
func revealScrollbars(ts *gluttertest.Tester, at ui.Offset) {
	ts.Hover(at)
	ts.Pump(time.Millisecond)
	ts.Pump(200 * time.Millisecond)
}

func TestEditorHasNoScrollbarWithoutTheNeed(t *testing.T) {
	vm := NewViewModel(newTestDoc("one\ntwo\nthree"), goOptions(), nil)
	opts := vm.Options()
	opts.ScrollBeyondLastLine = false
	vm.SetOptions(opts)
	ts := pumpEditor(t, vm, ui.Size{Width: 800, Height: 400})
	revealScrollbars(ts, ui.Offset{Dx: 400, Dy: 200})
	// The pixel is the editor's, whatever it is: nothing at the right edge differs from its neighbor.
	if pixel(ts, 793, 20) != pixel(ts, 700, 20) {
		t.Errorf("something is drawn at the right edge: %v", pixel(ts, 793, 20))
	}
	if vm.ContentWidth() != 800-vm.GutterWidths(ts.Dispatcher().TextEngine()).Total() {
		t.Errorf("short lines give a content as wide as the text area: %v", vm.ContentWidth())
	}
	noErrors(t, ts)
}

func TestVerticalScrollbarDragsTheEditor(t *testing.T) {
	vm := NewViewModel(linesDoc(5000), goOptions(), nil)
	ts := pumpEditor(t, vm, ui.Size{Width: 800, Height: 400})
	revealScrollbars(ts, ui.Offset{Dx: 400, Dy: 200})
	_, viewport := vm.ViewportSize()
	// ScrollBeyondLastLine: the last line can reach the top.
	wantContent := 5001*19.0 - 19 + viewport
	if vm.ContentHeight() != wantContent {
		t.Fatalf("content height %v, want %v", vm.ContentHeight(), wantContent)
	}
	state := kit.NewScrollbarState(0, 14, 0, viewport, vm.ContentHeight(), 0)
	if state.SliderSize() != 20 {
		t.Fatalf("a long document has the smallest slider: %v", state.SliderSize())
	}
	slider := pixel(ts, 793, 10)
	if slider == pixel(ts, 700, 10) {
		t.Fatal("the vertical scrollbar is revealed by the pointer")
	}

	g := ts.StartGesture(ui.Offset{Dx: 793, Dy: 10})
	ts.Pump(time.Millisecond)
	g.MoveBy(ui.Offset{Dy: 100})
	ts.Pump(time.Millisecond)
	if want := state.DesiredScrollPositionFromDelta(100); vm.ScrollTop() != want || want < 10000 {
		t.Errorf("dragged by 100 pixels: top %v, want %v", vm.ScrollTop(), want)
	}
	g.Up()
	ts.Pump(time.Millisecond)
	// The text follows: the first visible line is far down.
	top := int(vm.ScrollTop() / 19)
	layer := ts.RenderObject(gluttertest.ByType[*textLayer]()).(*RenderEditorText)
	if got := layer.PositionAt(ui.Offset{Dx: 1, Dy: 1}); got.Line != top {
		t.Errorf("the first row shows line %d, want %d", got.Line, top)
	}
	noErrors(t, ts)
}

func TestHorizontalScrollbarDragsTheEditor(t *testing.T) {
	vm := NewViewModel(newTestDoc(wideDoc(30)), goOptions(), nil)
	ts := pumpEditor(t, vm, ui.Size{Width: 640, Height: 400})
	gutter := vm.GutterWidths(ts.Dispatcher().TextEngine()).Total()
	width, _ := vm.ViewportSize()
	charWidth := newGeometry(vm, ts.Dispatcher().TextEngine()).charWidth
	if want := 300*charWidth + 4*charWidth; !near(vm.ContentWidth(), want) {
		t.Fatalf("content width %v, want the longest line and four characters: %v", vm.ContentWidth(), want)
	}
	revealScrollbars(ts, ui.Offset{Dx: 400, Dy: 200})
	// The track is the text area less the vertical scrollbar's 14 pixels; it starts after the gutter.
	state := kit.NewScrollbarState(0, 12, 14, width, vm.ContentWidth(), 0)
	if pixel(ts, int(gutter)+5, 394) == pixel(ts, int(gutter)+5, 300) {
		t.Fatal("the horizontal scrollbar is at the bottom, after the gutter")
	}
	if pixel(ts, int(gutter)-5, 394) != pixel(ts, int(gutter)-5, 300) {
		t.Error("nothing over the gutter")
	}
	strip := func() (out []color.NRGBA) { // the long line's row, across the text area
		shot := ts.Screenshot()
		for x := int(gutter); x < 620; x++ {
			out = append(out, color.NRGBAModel.Convert(shot.At(x, 3*19+9)).(color.NRGBA))
		}
		return out
	}
	before := strip()

	g := ts.StartGesture(ui.Offset{Dx: gutter + 5, Dy: 394})
	ts.Pump(time.Millisecond)
	g.MoveBy(ui.Offset{Dx: 30})
	ts.Pump(time.Millisecond)
	want := state.DesiredScrollPositionFromDelta(30)
	if want <= 0 || vm.ScrollLeft() != want {
		t.Errorf("dragged by 30 pixels: left %v, want %v", vm.ScrollLeft(), want)
	}
	g.Up()
	ts.Pump(time.Millisecond)
	if slices.Equal(strip(), before) {
		t.Error("the text did not move with the horizontal scroll")
	}
	// The hit test follows the scroll.
	layer := ts.RenderObject(gluttertest.ByType[*textLayer]()).(*RenderEditorText)
	if got := layer.PositionAt(ui.Offset{Dx: 1, Dy: 3*19 + 5}); got.Line != 3 || got.Col < 10 {
		t.Errorf("the left edge of the long line is far into it: %v", got)
	}
	// Dragging far to the right stops at the end of the content.
	g = ts.StartGesture(ui.Offset{Dx: gutter + 5, Dy: 394})
	g.MoveBy(ui.Offset{Dx: 2000})
	ts.Pump(time.Millisecond)
	g.Up()
	if vm.ScrollLeft() != vm.MaxScrollLeft() {
		t.Errorf("left %v, want the maximum %v", vm.ScrollLeft(), vm.MaxScrollLeft())
	}
	noErrors(t, ts)
}

func TestScrollLeftIsClamped(t *testing.T) {
	vm := NewViewModel(newTestDoc(wideDoc(5)), goOptions(), nil)
	pumpEditor(t, vm, ui.Size{Width: 640, Height: 400})
	vm.SetScrollLeft(1e6)
	if vm.ScrollLeft() != vm.MaxScrollLeft() || vm.ScrollLeft() <= 0 {
		t.Errorf("left %v, max %v", vm.ScrollLeft(), vm.MaxScrollLeft())
	}
	vm.SetScrollLeft(-5)
	if vm.ScrollLeft() != 0 {
		t.Errorf("left %v", vm.ScrollLeft())
	}
	// A view model that was never laid out accepts a remembered position.
	fresh := NewViewModel(newTestDoc("x"), goOptions(), nil)
	if err := fresh.RestoreViewState(ViewState{ScrollLeft: 120}); err != nil {
		t.Fatal(err)
	}
	if fresh.ScrollLeft() != 120 {
		t.Errorf("restored left %v", fresh.ScrollLeft())
	}
}

func TestContentWidthFollowsTheDocument(t *testing.T) {
	doc := newTestDoc(wideDoc(10))
	vm := NewViewModel(doc, goOptions(), nil)
	ts := pumpEditor(t, vm, ui.Size{Width: 640, Height: 400})
	wide := vm.ContentWidth()
	// Removing the long line shortens the content once the editor measures the rows again.
	_, err := doc.Edit(replaceLine(3, "short"), documentEditOptions())
	if err != nil {
		t.Fatal(err)
	}
	vm.DocumentChanged()
	ts.Pump(time.Millisecond)
	if vm.ContentWidth() >= wide {
		t.Errorf("content width %v, was %v", vm.ContentWidth(), wide)
	}
	if vm.ScrollLeft() > vm.MaxScrollLeft() {
		t.Errorf("left %v beyond the maximum %v", vm.ScrollLeft(), vm.MaxScrollLeft())
	}
	noErrors(t, ts)
}

func TestScrollbarsAppearWhenTheEditorScrolls(t *testing.T) {
	vm := NewViewModel(linesDoc(2000), goOptions(), nil)
	ts := pumpEditor(t, vm, ui.Size{Width: 800, Height: 400})
	background := pixel(ts, 793, 10)
	ts.Scroll(gluttertest.ByType[*Editor](), wheel(400))
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	ts.Pump(200 * time.Millisecond)
	// The pointer of the wheel is over the editor, so the scrollbar stays.
	if pixel(ts, 793, 10) == background && pixel(ts, 793, 11) == background {
		// the slider moved down; find it
		if !differsInColumn(ts, 793, 0, 400, background) {
			t.Error("scrolling reveals the scrollbar")
		}
	}
	ts.Hover(ui.Offset{Dx: -20, Dy: -20})
	ts.Pump(scrollbarWait)
	ts.Pump(time.Millisecond)
	ts.Pump(2 * time.Second)
	if differsInColumn(ts, 793, 8, 400, background) { // below the shadow of scrolled content
		t.Error("the scrollbar is still there")
	}
	noErrors(t, ts)
}

// scrollbarWait is the delay before the scrollbars fade (VS Code's 500 ms).
const scrollbarWait = 500 * time.Millisecond

func TestGoldenScrollbars(t *testing.T) {
	vm := NewViewModel(newTestDoc(wideDoc(60)), goOptions(), nil)
	ts := pumpEditor(t, vm, ui.Size{Width: 640, Height: 440})
	vm.SetScrollLeft(40)
	ts.Scroll(gluttertest.ByType[*Editor](), wheel(190))
	ts.Pump(100 * time.Millisecond)
	revealScrollbars(ts, ui.Offset{Dx: 300, Dy: 200})
	noErrors(t, ts)
	testutil.MatchesGolden(ts, "scrollbars")
}

// differsInColumn tells whether a pixel of a column of the screenshot, from y0 up to y1, is not
// the color.
func differsInColumn(ts *gluttertest.Tester, x, y0, y1 int, c color.NRGBA) bool {
	shot := ts.Screenshot()
	for y := y0; y < y1; y++ {
		if color.NRGBAModel.Convert(shot.At(x, y)).(color.NRGBA) != c {
			return true
		}
	}
	return false
}

func TestContentWidthOfWrappedTextHasNothingToScrollSideways(t *testing.T) {
	tests := []struct {
		name string
		mode string
		wide bool // the content is wider than the text area
	}{
		{"off scrolls", "off", true},
		{"on wraps", "on", false},
		{"bounded wraps", "bounded", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			opts := goOptions()
			opts.WordWrap, opts.WordWrapColumn = tc.mode, 80
			vm := NewViewModel(newTestDoc(wideDoc(30)), opts, nil)
			ts := pumpEditor(t, vm, ui.Size{Width: 640, Height: 400})
			ts.Pump(time.Second)
			width, _ := vm.ViewportSize()
			if got := vm.ContentWidth() > width+0.5; got != tc.wide {
				t.Errorf("content width %v in a text area of %v: wider is %v, want %v", vm.ContentWidth(), width, got, tc.wide)
			}
			noErrors(t, ts)
		})
	}
}
