package editor

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// stickySample is Go with three levels of scopes around a long body: the lines 3, 4 and 5 (from 0)
// start the scopes that stick in the body, which is the lines 6 to 45.
func stickySample() string {
	var b strings.Builder
	b.WriteString("package sample\n\nfunc Outer(a int) int {\n\tfor i := 0; i < a; i++ {\n\t\tif i%2 == 0 {\n\t\t\tswitch {\n")
	for i := range 40 {
		fmt.Fprintf(&b, "\t\t\t\t// step %d\n", i)
	}
	b.WriteString("\t\t\t}\n\t\t}\n\t}\n\treturn a\n}\n\nfunc Other() {}\n")
	return b.String()
}

type stickyRig struct {
	t  *testing.T
	ts *gluttertest.Tester
	vm *ViewModel
}

func newStickyRig(t *testing.T, size ui.Size, tweak func(*Options)) *stickyRig {
	t.Helper()
	vm, tok := highlighted(t, stickySample())
	opts := vm.Options()
	opts.StickyScroll = StickyScrollOptions{Enabled: true, MaxLineCount: 5, ScrollWithEditor: true}
	opts.Folding, opts.ShowFoldingControls, opts.RenderLineHighlight = true, "mouseover", "none"
	opts.LineHeight = 19
	opts.OccurrencesHighlight, opts.SelectionHighlight = "off", false
	if tweak != nil {
		tweak(&opts)
	}
	vm.SetOptions(opts)
	vm.StartFolding(nil, 0)
	computeFolds(t, vm)
	t.Cleanup(vm.DisposeHighlights)
	ts := testutil.New(t, size)
	pumpHighlighted(t, ts, vm, kit.DarkModern(), darkModernTokens(t))
	tok.Settle()
	r := &stickyRig{t: t, ts: ts, vm: vm}
	r.settle()
	return r
}

func (r *stickyRig) settle() {
	r.t.Helper()
	for range 3 {
		r.ts.Pump(time.Millisecond)
	}
	r.ts.FlushPosted()
	for range 3 {
		r.ts.Pump(time.Millisecond)
	}
}

// scrollRows scrolls the editor to a number of rows from the top.
func (r *stickyRig) scrollRows(rows float64) {
	r.t.Helper()
	r.vm.scroller(rows * 19)
	r.settle()
}

func (r *stickyRig) mouse() gluttertest.GestureOpts {
	return gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse}
}

func (r *stickyRig) layer() *RenderEditorSticky {
	r.t.Helper()
	return r.ts.RenderObject(gluttertest.ByType[*stickyLayer]()).(*RenderEditorSticky)
}

func TestStickyLinesOfRealCode(t *testing.T) {
	r := newStickyRig(t, ui.Size{Width: 640, Height: 300}, nil)
	if got := r.vm.stickyLinesAt(0); len(got.Lines) != 0 {
		t.Errorf("sticky lines at the top: %v", got.Lines)
	}
	r.scrollRows(12)
	got := r.vm.stickyLinesAt(r.vm.ScrollTop())
	want := []int{2, 3, 4, 5}
	if len(got.Lines) != len(want) {
		t.Fatalf("lines %v, want %v", got.Lines, want)
	}
	for i := range want {
		if got.Lines[i] != want[i] {
			t.Fatalf("lines %v, want %v", got.Lines, want)
		}
	}
	// the widget is as high as its lines, and its box has the room of the shadow too
	if h := r.layer().Size().Height; h != 4*19+stickyShadowHeight {
		t.Errorf("widget height %v, want %v", h, 4*19+stickyShadowHeight)
	}
	noErrors(t, r.ts)
}

func TestStickyWidgetIsGoneAtTheTopAndWhenOff(t *testing.T) {
	r := newStickyRig(t, ui.Size{Width: 640, Height: 300}, nil)
	if h := r.layer().Size().Height; h != 0 {
		t.Errorf("a widget of %v pixels at the top", h)
	}
	off := newStickyRig(t, ui.Size{Width: 640, Height: 300}, func(o *Options) { o.StickyScroll.Enabled = false })
	if off.ts.Exists(gluttertest.ByType[*stickyLayer]()) {
		t.Error("sticky lines while the setting is off")
	}
}

func TestClickingAStickyLineGoesToIt(t *testing.T) {
	r := newStickyRig(t, ui.Size{Width: 640, Height: 300}, nil)
	r.scrollRows(12)
	// the second sticky line, which is the line 3 (from 0): the click is at its text, 4 characters in
	gutter := r.vm.GutterWidths(r.ts.Dispatcher().TextEngine()).Total()
	g := newGeometry(r.vm, r.ts.Dispatcher().TextEngine())
	r.ts.TapAt(ui.Offset{Dx: gutter + g.x(0, 0) + 4*g.charWidth + 100, Dy: 19 + 9}, r.mouse())
	r.settle()
	pos := r.vm.Cursors().Primary().Position()
	if pos.Line != 3 {
		t.Fatalf("the cursor is on the line %d, want 3", pos.Line)
	}
	if pos.Col == 0 {
		t.Errorf("a click in the text of the line put the cursor at its start")
	}
	// the line is on the screen and no sticky line is over it
	proj := r.vm.Projection()
	y := proj.RowTop(r.vm.RowOfPosition(pos)) - r.vm.ScrollTop()
	stuck := r.vm.stickyLinesAt(r.vm.ScrollTop())
	if y < float64(len(stuck.Lines))*19 {
		t.Errorf("the line is %v pixels from the top, under %d sticky lines", y, len(stuck.Lines))
	}
	for _, l := range stuck.Lines {
		if l == 3 {
			t.Errorf("the line that was clicked still sticks: %v", stuck.Lines)
		}
	}
	noErrors(t, r.ts)
}

func TestClickingAStickyLineNumberGoesToTheStartOfTheLine(t *testing.T) {
	r := newStickyRig(t, ui.Size{Width: 640, Height: 300}, nil)
	r.scrollRows(12)
	gutter := r.vm.GutterWidths(r.ts.Dispatcher().TextEngine())
	r.ts.TapAt(ui.Offset{Dx: gutter.GlyphMargin + gutter.LineNumbers/2, Dy: 19*0 + 9}, r.mouse())
	r.settle()
	if got := r.vm.Cursors().Primary().Position(); got != (buffer.Position{Line: 2, Col: 0}) {
		t.Errorf("a click on the number of the first sticky line: %v", got)
	}
	noErrors(t, r.ts)
}

func TestShiftClickingAStickyLineShowsTheEndOfItsScope(t *testing.T) {
	r := newStickyRig(t, ui.Size{Width: 640, Height: 300}, nil)
	r.scrollRows(12)
	r.vm.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: 15})})
	hold := r.ts.SendKeyDownEvent
	hold(services.LogicalKeyboardKeyShiftLeft, gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyShiftLeft})
	gutter := r.vm.GutterWidths(r.ts.Dispatcher().TextEngine()).Total()
	r.ts.TapAt(ui.Offset{Dx: gutter + 50, Dy: 9}, r.mouse())
	r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyShiftLeft, gluttertest.KeyOpts{Physical: services.PhysicalKeyboardKeyShiftLeft})
	r.settle()
	// the first sticky line is the function, whose scope ends at the line after its last: the line 48
	end := r.vm.Projection().RowTop(r.vm.RowOfPosition(buffer.Position{Line: 49}))
	if end < r.vm.ScrollTop() || end > r.vm.ScrollTop()+300 {
		t.Errorf("the end of the scope (at %v) is not on the screen (scroll top %v)", end, r.vm.ScrollTop())
	}
	if got := r.vm.Cursors().Primary().Position(); got.Line != 15 {
		t.Errorf("a shift click moved the cursor to %v", got)
	}
	noErrors(t, r.ts)
}

func TestStickyFoldingIcon(t *testing.T) {
	r := newStickyRig(t, ui.Size{Width: 640, Height: 300}, func(o *Options) { o.ShowFoldingControls = "always" })
	r.scrollRows(12)
	w := r.vm.GutterWidths(r.ts.Dispatcher().TextEngine())
	// the icon of the second sticky line, the `for` at line 3
	r.ts.TapAt(ui.Offset{Dx: w.GlyphMargin + w.LineNumbers + foldingControlsWidth/2, Dy: 19 + 9}, r.mouse())
	r.settle()
	if got := r.vm.FoldingStateOf(3); got != FoldingCollapsed {
		t.Errorf("the scope of the line 3 is %v after a click on its icon, want folded", got)
	}
	noErrors(t, r.ts)
}

func TestStickyHoverHighlightsTheLine(t *testing.T) {
	r := newStickyRig(t, ui.Size{Width: 640, Height: 300}, nil)
	r.scrollRows(12)
	gutter := r.vm.GutterWidths(r.ts.Dispatcher().TextEngine()).Total()
	r.ts.Hover(ui.Offset{Dx: gutter + 40, Dy: 19*2 + 5})
	r.settle()
	if !r.vm.sticky.hover || r.vm.sticky.line != 2 {
		t.Errorf("hover %v on the line %d, want the third", r.vm.sticky.hover, r.vm.sticky.line)
	}
	r.ts.Hover(ui.Offset{Dx: gutter + 40, Dy: 250})
	r.settle()
	if r.vm.sticky.hover {
		t.Error("the pointer left the widget")
	}
}

func TestRevealPositionKeepsClearOfTheStickyLines(t *testing.T) {
	cases := []struct {
		name         string
		sticky       bool
		viewport     float64
		top, bottom  int
		cursorLine   int
		scroll       float64
		wantScrolled bool
		wantTop      float64
	}{
		{"off: a row on the screen does not scroll", false, 380, 0, 0, 10, 0, false, 0},
		{"on: a row near the top scrolls to the top, clamped", true, 380, 5, 4, 3, 19 * 20, true, 0},
		{"on: a row above the screen is brought down from the edge", true, 380, 5, 4, 30, 19 * 40, true, 30*19 - 5*19},
		{"on: a row in the middle does not scroll", true, 380, 5, 4, 10, 0, false, 0},
		{"on: a row near the bottom is brought up", true, 380, 5, 4, 19, 0, true, 20*19 + 4*19 - 380},
		{"a small editor halves the padding", true, 76, 2, 1, 1, 19 * 5, true, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := stickyVM(t, func(o *Options) { o.StickyScroll.Enabled = tc.sticky })
			vm.SetTextMetrics(700, tc.viewport, vm.Projection().Height()-19, 0, 8.4)
			vm.SetScrollTop(tc.scroll)
			if top, bottom := vm.revealPadding(); tc.sticky && (top != tc.top || bottom != tc.bottom) || !tc.sticky && (top != 0 || bottom != 0) {
				t.Errorf("padding %d %d, want %d %d", top, bottom, tc.top, tc.bottom)
			}
			var scrolled []float64
			vm.SetScroller(func(top float64) { scrolled = append(scrolled, top) })
			vm.RevealPosition(buffer.Position{Line: tc.cursorLine})
			if (len(scrolled) > 0) != tc.wantScrolled {
				t.Fatalf("scrolled %v, want scrolling %v", scrolled, tc.wantScrolled)
			}
			if tc.wantScrolled && scrolled[0] != tc.wantTop {
				t.Errorf("scrolled to %v, want %v", scrolled[0], tc.wantTop)
			}
		})
	}
}

func TestGoldenStickyScroll(t *testing.T) {
	r := newStickyRig(t, ui.Size{Width: 640, Height: 300}, func(o *Options) { o.ShowFoldingControls = "always" })
	r.scrollRows(14)
	r.vm.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: 24})})
	r.vm.SelectionsChanged()
	r.settle()
	noErrors(t, r.ts)
	testutil.MatchesGolden(r.ts, "sticky_scroll")
}

func TestGoldenStickyScrollPushedUp(t *testing.T) {
	r := newStickyRig(t, ui.Size{Width: 640, Height: 300}, nil)
	// 9 rows from the end of the innermost scope (the line 46): its line slides up under the others
	r.vm.scroller(19*46 - 10)
	r.settle()
	noErrors(t, r.ts)
	testutil.MatchesGolden(r.ts, "sticky_scroll_pushed")
}
