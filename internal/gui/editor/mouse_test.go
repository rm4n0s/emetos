package editor

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// mouseRig is an editor on screen and the means to point at its text.
type mouseRig struct {
	t      *testing.T
	ts     *gluttertest.Tester
	vm     *ViewModel
	gutter float64
	cw     float64
	lineH  float64
}

func newMouseRig(t *testing.T, text string, tweak func(*Options)) *mouseRig {
	t.Helper()
	return newMouseRigWith(t, text, tweak, EditorOpts{Autofocus: true})
}

// newMouseRigWith is newMouseRig with the options of the editor widget.
func newMouseRigWith(t *testing.T, text string, tweak func(*Options), editorOpts EditorOpts) *mouseRig {
	t.Helper()
	opts := goOptions()
	if tweak != nil {
		tweak(&opts)
	}
	vm := NewViewModel(newTestDoc(text), opts, nil)
	ts := testutil.New(t, ui.Size{Width: 800, Height: 400})
	pumpEditorWith(t, ts, vm, editorOpts)
	g := newGeometry(vm, ts.Dispatcher().TextEngine())
	return &mouseRig{t: t, ts: ts, vm: vm, gutter: vm.GutterWidths(ts.Dispatcher().TextEngine()).Total(), cw: g.charWidth, lineH: opts.LineHeight}
}

// at is the point of the window in the middle of a row, on the boundary before a column.
func (r *mouseRig) at(line, col int) ui.Offset {
	return ui.Offset{Dx: r.gutter + float64(col)*r.cw + 1, Dy: float64(line)*r.lineH + r.lineH/2}
}

// numberAt is a point on the line number of a line.
func (r *mouseRig) numberAt(line int) ui.Offset {
	w := r.vm.GutterWidths(r.ts.Dispatcher().TextEngine())
	return ui.Offset{Dx: w.GlyphMargin + w.LineNumbers/2, Dy: float64(line)*r.lineH + r.lineH/2}
}

func mouseOpts() gluttertest.GestureOpts {
	return gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse}
}

// click presses and releases the primary button, and lets enough time pass for the next click
// to be a new one.
func (r *mouseRig) click(p ui.Offset) {
	r.t.Helper()
	r.ts.TapAt(p, mouseOpts())
	r.ts.Pump(time.Second)
}

// clicks presses n times in a row.
func (r *mouseRig) clicks(p ui.Offset, n int) {
	r.t.Helper()
	for range n {
		r.ts.TapAt(p, mouseOpts())
	}
	r.ts.Pump(time.Second)
}

// hold presses a key until the returned function releases it.
func (r *mouseRig) hold(logical services.LogicalKeyboardKey, physical services.PhysicalKeyboardKey) func() {
	r.ts.SendKeyDownEvent(logical, gluttertest.KeyOpts{Physical: physical})
	return func() { r.ts.SendKeyUpEvent(logical, gluttertest.KeyOpts{Physical: physical}) }
}

func (r *mouseRig) holdAlt() func() {
	return r.hold(services.LogicalKeyboardKeyAltLeft, services.PhysicalKeyboardKeyAltLeft)
}
func (r *mouseRig) holdShift() func() {
	return r.hold(services.LogicalKeyboardKeyShiftLeft, services.PhysicalKeyboardKeyShiftLeft)
}
func (r *mouseRig) holdCtrl() func() {
	return r.hold(services.LogicalKeyboardKeyControlLeft, services.PhysicalKeyboardKeyControlLeft)
}

// drag presses at a point, moves to others and releases.
func (r *mouseRig) drag(from ui.Offset, to ...ui.Offset) {
	r.t.Helper()
	g := r.ts.StartGesture(from, mouseOpts())
	for _, p := range to {
		g.MoveTo(p)
	}
	g.Up()
	r.ts.Pump(time.Second)
}

// wheel is the scroll of a mouse wheel that moves the editor by px pixels, down for a positive
// number: a notch is wheelNotch pixels of scroll and moves wheelStep.
func wheel(px float64) ui.Offset {
	if px < 0 {
		return ui.Offset{Dy: (px + 0.5) / wheelStep * wheelNotch}
	}
	return ui.Offset{Dy: (px - 0.5) / wheelStep * wheelNotch}
}

func (r *mouseRig) selections() []buffer.Selection { return r.vm.Cursors().Selections() }

func (r *mouseRig) want(sels ...buffer.Selection) {
	r.t.Helper()
	if got := r.selections(); !buffer.SelectionsEqual(got, sels) {
		r.t.Errorf("selections %v, want %v", got, sels)
	}
}

const mouseText = "hello world\nfoo.bar baz\nthe end\n\nlast line here"

func TestClickPlacesTheCursor(t *testing.T) {
	cases := []struct {
		name string
		at   [2]int
		want buffer.Selection
	}{
		{"in a line", [2]int{0, 4}, sel(0, 4, 0, 4)},
		{"at the start", [2]int{1, 0}, sel(1, 0, 1, 0)},
		{"past the end of a line", [2]int{2, 30}, sel(2, 7, 2, 7)},
		{"in an empty line", [2]int{3, 5}, sel(3, 0, 3, 0)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newMouseRig(t, mouseText, nil)
			r.click(r.at(tc.at[0], tc.at[1]))
			r.want(tc.want)
			noErrors(t, r.ts)
		})
	}
}

func TestClickTakesTheFocusAndEndsSelections(t *testing.T) {
	r := newMouseRig(t, mouseText, nil)
	r.vm.Cursors().SetSelections([]buffer.Selection{sel(0, 0, 0, 5), sel(1, 0, 1, 3)})
	r.click(r.at(2, 2))
	r.want(sel(2, 2, 2, 2))
	if !r.vm.Focused() {
		t.Error("a click focuses the editor")
	}
}

func TestDoubleClickSelectsAWord(t *testing.T) {
	cases := []struct {
		name string
		at   [2]int
		want buffer.Selection
	}{
		{"a word", [2]int{0, 7}, sel(0, 6, 0, 11)},
		{"a word before a separator", [2]int{1, 1}, sel(1, 0, 1, 3)},
		{"a separator", [2]int{1, 3}, sel(1, 0, 1, 3)},
		{"the word after a separator", [2]int{1, 5}, sel(1, 4, 1, 7)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newMouseRig(t, mouseText, nil)
			r.clicks(r.at(tc.at[0], tc.at[1]), 2)
			r.want(tc.want)
		})
	}
}

func TestTripleClickSelectsALine(t *testing.T) {
	r := newMouseRig(t, mouseText, nil)
	r.clicks(r.at(1, 5), 3)
	r.want(sel(1, 0, 2, 0))
	r = newMouseRig(t, mouseText, nil)
	r.clicks(r.at(4, 5), 3)
	r.want(sel(4, 0, 4, 14))
}

func TestFourClicksSelectEverything(t *testing.T) {
	r := newMouseRig(t, mouseText, nil)
	r.clicks(r.at(1, 5), 4)
	r.want(sel(0, 0, 4, 14))
	r.clicks(r.at(1, 5), 6)
	r.want(sel(0, 0, 4, 14))
}

func TestSlowOrFarClicksAreSeparateClicks(t *testing.T) {
	r := newMouseRig(t, mouseText, nil)
	r.ts.TapAt(r.at(0, 7), mouseOpts())
	r.ts.Pump(doubleClickInterval + time.Millisecond)
	r.ts.TapAt(r.at(0, 7), mouseOpts())
	r.want(sel(0, 7, 0, 7))
	r.ts.Pump(time.Second)

	r.ts.TapAt(r.at(0, 2), mouseOpts())
	r.ts.TapAt(r.at(0, 9), mouseOpts())
	r.want(sel(0, 9, 0, 9))
}

func TestDragSelectsCharacters(t *testing.T) {
	r := newMouseRig(t, mouseText, nil)
	r.drag(r.at(0, 2), r.at(0, 5), r.at(1, 4))
	r.want(sel(0, 2, 1, 4))
	r.click(r.at(4, 3)) // a press on the selection would drag its text
	r.drag(r.at(1, 4), r.at(0, 1))
	r.want(sel(1, 4, 0, 1))
	// A drag past the end of the text goes to the end.
	r.drag(r.at(0, 0), ui.Offset{Dx: 700, Dy: 390})
	r.want(sel(0, 0, 4, 14))
}

func TestDragAfterDoubleClickSelectsWords(t *testing.T) {
	r := newMouseRig(t, mouseText, nil)
	r.ts.TapAt(r.at(0, 7), mouseOpts())
	g := r.ts.StartGesture(r.at(0, 7), mouseOpts())
	r.want(sel(0, 6, 0, 11))
	g.MoveTo(r.at(1, 5)) // inside "bar" on the next line
	r.want(sel(0, 6, 1, 7))
	g.MoveTo(r.at(0, 2)) // inside "hello"
	r.want(sel(0, 11, 0, 0))
	g.MoveTo(r.at(0, 8)) // back inside the first word
	r.want(sel(0, 6, 0, 11))
	g.Up()
}

func TestDragAfterTripleClickSelectsLines(t *testing.T) {
	r := newMouseRig(t, mouseText, nil)
	r.ts.TapAt(r.at(1, 2), mouseOpts())
	r.ts.TapAt(r.at(1, 2), mouseOpts())
	g := r.ts.StartGesture(r.at(1, 2), mouseOpts())
	r.want(sel(1, 0, 2, 0))
	g.MoveTo(r.at(2, 3))
	r.want(sel(1, 0, 3, 0))
	g.MoveTo(r.at(0, 3))
	r.want(sel(2, 0, 0, 0))
	g.MoveTo(r.at(1, 9))
	r.want(sel(1, 0, 2, 0))
	g.Up()
}

func TestShiftClickExtendsTheSelection(t *testing.T) {
	r := newMouseRig(t, mouseText, nil)
	r.click(r.at(0, 3))
	release := r.holdShift()
	r.click(r.at(1, 4))
	r.want(sel(0, 3, 1, 4))
	r.click(r.at(0, 8))
	r.want(sel(0, 3, 0, 8))
	release()
	r.click(r.at(2, 1))
	r.want(sel(2, 1, 2, 1))
}

func TestShiftDoubleClickExtendsByWords(t *testing.T) {
	r := newMouseRig(t, mouseText, nil)
	r.click(r.at(0, 1))
	release := r.holdShift()
	r.clicks(r.at(1, 5), 2)
	release()
	r.want(sel(0, 1, 1, 7))
}

func TestAltClickAddsAndRemovesCursors(t *testing.T) {
	r := newMouseRig(t, mouseText, nil)
	r.click(r.at(0, 2))
	release := r.holdAlt()
	r.click(r.at(1, 3))
	r.want(sel(0, 2, 0, 2), sel(1, 3, 1, 3))
	r.click(r.at(2, 1))
	r.want(sel(0, 2, 0, 2), sel(1, 3, 1, 3), sel(2, 1, 2, 1))
	// Clicking where there is a cursor takes it away.
	r.click(r.at(1, 3))
	r.want(sel(0, 2, 0, 2), sel(2, 1, 2, 1))
	release()
	// A click without the modifier is back to one cursor.
	r.click(r.at(4, 0))
	r.want(sel(4, 0, 4, 0))
}

func TestAltClickInsideASelectionRemovesIt(t *testing.T) {
	r := newMouseRig(t, mouseText, nil)
	r.vm.Cursors().SetSelections([]buffer.Selection{sel(0, 0, 0, 5), sel(1, 0, 1, 3)})
	release := r.holdAlt()
	r.click(r.at(0, 2))
	release()
	r.want(sel(1, 0, 1, 3))
}

func TestAltDragSelectsWithTheNewCursor(t *testing.T) {
	r := newMouseRig(t, mouseText, nil)
	r.click(r.at(0, 2))
	release := r.holdAlt()
	r.drag(r.at(1, 1), r.at(1, 5))
	release()
	r.want(sel(0, 2, 0, 2), sel(1, 1, 1, 5))
}

func TestAltDoubleClickAddsAWord(t *testing.T) {
	r := newMouseRig(t, mouseText, nil)
	r.click(r.at(0, 2))
	release := r.holdAlt()
	r.clicks(r.at(1, 5), 2)
	release()
	r.want(sel(0, 2, 0, 2), sel(1, 4, 1, 7))
}

func TestAltTripleClickAddsALine(t *testing.T) {
	r := newMouseRig(t, mouseText, nil)
	r.click(r.at(0, 2))
	release := r.holdAlt()
	r.clicks(r.at(2, 2), 3)
	release()
	r.want(sel(0, 2, 0, 2), sel(2, 0, 3, 0))
}

func TestCtrlIsTheModifierWhenConfigured(t *testing.T) {
	r := newMouseRig(t, mouseText, func(o *Options) { o.MultiCursorModifier = "ctrlCmd" })
	r.click(r.at(0, 2))
	release := r.holdCtrl()
	r.click(r.at(1, 3))
	release()
	r.want(sel(0, 2, 0, 2), sel(1, 3, 1, 3))
	// Alt is then the modifier of the column selection, not of cursors.
	release = r.holdAlt()
	r.drag(r.at(0, 1), r.at(2, 4))
	release()
	if got := r.selections(); len(got) != 3 || got[0] != sel(0, 1, 0, 4) {
		t.Errorf("column selection with alt: %v", got)
	}
}

func TestAltIsNotTheModifierWhenCtrlIs(t *testing.T) {
	r := newMouseRig(t, mouseText, func(o *Options) { o.MultiCursorModifier = "ctrlCmd" })
	r.click(r.at(0, 2))
	release := r.holdAlt()
	r.click(r.at(1, 3))
	release()
	r.want(sel(1, 3, 1, 3)) // Alt+click only moves the cursor (a drag would select columns)
}

func TestShiftAltDragSelectsAColumn(t *testing.T) {
	r := newMouseRig(t, mouseText, nil)
	releaseShift, releaseAlt := r.holdShift(), r.holdAlt()
	r.click(r.at(0, 2)) // anchors at the cursor, which is at the start
	releaseAlt()
	releaseShift()

	r = newMouseRig(t, mouseText, nil)
	r.click(r.at(0, 2))
	releaseShift, releaseAlt = r.holdShift(), r.holdAlt()
	r.drag(r.at(0, 2), r.at(2, 5))
	releaseAlt()
	releaseShift()
	r.want(sel(0, 2, 0, 5), sel(1, 2, 1, 5), sel(2, 2, 2, 5))
	if r.vm.columns == nil {
		t.Error("the column selection is a session")
	}
}

func TestColumnSelectionReachesPastShortLines(t *testing.T) {
	r := newMouseRig(t, mouseText, nil)
	r.click(r.at(0, 1))
	releaseShift, releaseAlt := r.holdShift(), r.holdAlt()
	r.drag(r.at(0, 1), r.at(3, 9))
	releaseAlt()
	releaseShift()
	// A short line is selected up to its end, and one that does not reach the rectangle is left out.
	r.want(sel(0, 1, 0, 9), sel(1, 1, 1, 9), sel(2, 1, 2, 7))
}

func TestColumnSelectionSetting(t *testing.T) {
	r := newMouseRig(t, mouseText, func(o *Options) { o.ColumnSelection = true })
	r.drag(r.at(0, 2), r.at(2, 5))
	r.want(sel(0, 2, 0, 5), sel(1, 2, 1, 5), sel(2, 2, 2, 5))
	// A click still places the cursor.
	r.click(r.at(1, 1))
	r.want(sel(1, 1, 1, 1))
}

func TestAltDragSelectsAColumnWithoutTheSetting(t *testing.T) {
	r := newMouseRig(t, mouseText, func(o *Options) { o.MultiCursorModifier = "ctrlCmd" })
	release := r.holdAlt()
	r.drag(r.at(0, 2), r.at(1, 5))
	release()
	r.want(sel(0, 2, 0, 5), sel(1, 2, 1, 5))
}

func TestLineNumbersSelectLines(t *testing.T) {
	r := newMouseRig(t, mouseText, nil)
	r.click(r.numberAt(1))
	r.want(sel(1, 0, 2, 0))
	r.drag(r.numberAt(1), r.numberAt(3))
	r.want(sel(1, 0, 4, 0))
	r.drag(r.numberAt(2), r.numberAt(0))
	r.want(sel(3, 0, 0, 0))
	// Pressing twice on a number is no double click.
	r.clicks(r.numberAt(4), 2)
	r.want(sel(4, 0, 4, 14))
}

func TestShiftClickOnALineNumberExtendsByLines(t *testing.T) {
	r := newMouseRig(t, mouseText, nil)
	r.click(r.numberAt(0))
	release := r.holdShift()
	r.click(r.numberAt(2))
	release()
	r.want(sel(0, 0, 3, 0))
}

func TestAltClickOnALineNumberAddsALine(t *testing.T) {
	r := newMouseRig(t, mouseText, nil)
	r.click(r.at(0, 2))
	release := r.holdAlt()
	r.click(r.numberAt(2))
	r.want(sel(0, 2, 0, 2), sel(2, 0, 3, 0))
	r.drag(r.numberAt(4), r.numberAt(4))
	release()
	if got := r.selections(); len(got) != 3 || got[2] != sel(4, 0, 4, 14) {
		t.Errorf("selections %v", got)
	}
}

func TestClickInTheGutterGoesToTheStartOfTheLine(t *testing.T) {
	r := newMouseRig(t, mouseText, nil)
	r.click(r.at(1, 5))
	r.click(ui.Offset{Dx: 2, Dy: r.lineH * 2.5})
	r.want(sel(2, 0, 2, 0))
}

func TestOnlyThePrimaryAndMiddleButtonsPlaceTheCursor(t *testing.T) {
	r := newMouseRig(t, mouseText, nil)
	r.click(r.at(0, 3))
	r.ts.TapAt(r.at(2, 2), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.SecondaryButton)})
	r.ts.Pump(time.Second)
	r.want(sel(0, 3, 0, 3))
	r.ts.TapAt(r.at(2, 2), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.TertiaryButton)})
	r.ts.Pump(time.Second)
	r.want(sel(2, 2, 2, 2))
}

func TestTouchTapPlacesTheCursorButDoesNotSelect(t *testing.T) {
	r := newMouseRig(t, mouseText, nil)
	r.ts.TapAt(r.at(1, 4))
	r.want(sel(1, 4, 1, 4))
}

func TestMouseCursorShapes(t *testing.T) {
	cases := []struct {
		style string
		want  services.MouseCursor
	}{
		{"text", services.SystemMouseCursors.Text},
		{"default", services.SystemMouseCursors.Basic},
		{"copy", services.SystemMouseCursors.Copy},
		{"", services.SystemMouseCursors.Text},
	}
	for _, tc := range cases {
		if got := mouseCursor(tc.style); got != tc.want {
			t.Errorf("mouseCursor(%q) = %v, want %v", tc.style, got, tc.want)
		}
	}
}

func TestMouseCursorOverTheText(t *testing.T) {
	r := newMouseRig(t, mouseText, func(o *Options) { o.MouseStyle = "copy" })
	var shown []string
	r.ts.Binding.Renderer().MouseTracker().SetSystemCursorActivator(func(_ int, kind string) { shown = append(shown, kind) })
	r.ts.Hover(r.at(0, 3))
	if len(shown) == 0 || shown[len(shown)-1] != services.SystemMouseCursors.Copy.Kind() {
		t.Errorf("over the text the pointer shows %v", shown)
	}
	shown = nil
	r.ts.Hover(ui.Offset{Dx: 3, Dy: 20})
	if len(shown) == 0 || shown[len(shown)-1] == services.SystemMouseCursors.Copy.Kind() {
		t.Errorf("over the gutter the pointer shows %v", shown)
	}
}

func TestShiftTripleClickExtendsByLines(t *testing.T) {
	r := newMouseRig(t, mouseText, nil)
	r.click(r.at(1, 3))
	release := r.holdShift()
	r.clicks(r.at(3, 0), 3)
	release()
	r.want(sel(1, 3, 4, 0))
}

func TestDraggingWithTheMouseDoesNotScroll(t *testing.T) {
	vm := NewViewModel(linesDoc(100), goOptions(), nil)
	ts := pumpEditor(t, vm, ui.Size{Width: 800, Height: 400})
	rig := &mouseRig{t: t, ts: ts, vm: vm, lineH: 19, cw: 8}
	rig.gutter = vm.GutterWidths(ts.Dispatcher().TextEngine()).Total()
	rig.drag(rig.at(2, 3), rig.at(8, 6), rig.at(14, 2))
	if vm.ScrollTop() != 0 {
		t.Errorf("the view scrolled to %v", vm.ScrollTop())
	}
	if got := vm.Cursors().Primary().Selection; got.Anchor.Line != 2 || got.Active.Line != 14 {
		t.Errorf("selection %v", got)
	}
}

func TestClickOnAScrolledEditor(t *testing.T) {
	vm := NewViewModel(linesDoc(100), goOptions(), nil)
	ts := pumpEditor(t, vm, ui.Size{Width: 800, Height: 400})
	vm.scroller(19 * 30)
	ts.Pump()
	rig := &mouseRig{t: t, ts: ts, vm: vm, lineH: 19, cw: 8}
	rig.gutter = vm.GutterWidths(ts.Dispatcher().TextEngine()).Total()
	rig.click(rig.at(2, 3))
	if got := vm.Cursors().Primary().Position(); got.Line != 32 {
		t.Errorf("a click on the third visible row is line %d, want 32", got.Line)
	}
}

func TestGoldenMouseColumnSelection(t *testing.T) {
	r := newMouseRig(t, "alpha beta gamma\nbe\ndelta epsilon zeta\neta theta iota", nil)
	r.click(r.at(0, 2))
	releaseShift, releaseAlt := r.holdShift(), r.holdAlt()
	r.drag(r.at(0, 2), r.at(3, 9))
	releaseAlt()
	releaseShift()
	r.ts.Pump()
	r.ts.MatchesGolden("mouse_column_selection")
}
