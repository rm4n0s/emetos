package editor

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// foldRig is an editor on screen over foldingSample, with its regions known and the cursor on
// the last line, out of every region.
type foldRig struct {
	*mouseRig
}

func newFoldRig(t *testing.T, tweak func(*Options)) foldRig {
	t.Helper()
	opts := goOptions()
	if tweak != nil {
		tweak(&opts)
	}
	vm := NewViewModel(newTestDoc(foldingSample), opts, nil)
	vm.StartFolding(nil, 0)
	computeFolds(t, vm)
	cursorAt(vm, 9, 0)
	ts := testutil.New(t, ui.Size{Width: 640, Height: 300})
	pumpEditorWith(t, ts, vm, EditorOpts{Autofocus: true})
	ts.Pump(time.Millisecond)
	g := newGeometry(vm, ts.Dispatcher().TextEngine())
	return foldRig{&mouseRig{t: t, ts: ts, vm: vm, gutter: vm.GutterWidths(ts.Dispatcher().TextEngine()).Total(), cw: g.charWidth, lineH: opts.LineHeight}}
}

// control is a point on the folding control of a row.
func (r foldRig) control(row int) ui.Offset {
	w := r.vm.GutterWidths(r.ts.Dispatcher().TextEngine())
	return ui.Offset{Dx: w.GlyphMargin + w.LineNumbers + 8, Dy: float64(row)*r.lineH + r.lineH/2}
}

// afterText is a point of a row past the end of the text of a line, on the placeholder.
func (r foldRig) afterText(row, line int) ui.Offset {
	text, _ := r.vm.Document().Snapshot().LineContent(line)
	return ui.Offset{Dx: r.gutter + float64(len(text))*r.cw + 0.2*r.vm.Options().FontSize + 2, Dy: float64(row)*r.lineH + r.lineH/2}
}

func TestClickingTheControlFoldsAndUnfolds(t *testing.T) {
	r := newFoldRig(t, nil)
	before := r.selections()
	r.click(r.control(2)) // the inner region
	if !r.vm.FoldedLine(2) {
		t.Fatalf("the click on the control did not fold")
	}
	r.want(before...)
	r.click(r.control(2))
	if r.vm.FoldedLine(2) {
		t.Fatalf("the second click did not unfold")
	}
	r.want(before...)
	noErrors(t, r.ts)
}

func TestClickingNextToTheControlDoesNotFold(t *testing.T) {
	r := newFoldRig(t, nil)
	w := r.vm.GutterWidths(r.ts.Dispatcher().TextEngine())
	r.click(ui.Offset{Dx: w.GlyphMargin + w.LineNumbers + 1, Dy: r.lineH / 2}) // the space before the control
	if r.vm.FoldedLine(0) {
		t.Errorf("the first pixels of the decorations are not the control")
	}
	r.click(ui.Offset{Dx: w.GlyphMargin + w.LineNumbers/2, Dy: r.lineH / 2}) // a line number
	if r.vm.FoldedLine(0) {
		t.Errorf("a line number does not fold")
	}
	r.click(r.control(1)) // a line that starts no region goes to the start of the line
	if got := r.vm.Cursors().Primary().Position(); got != (buffer.Position{Line: 1}) {
		t.Errorf("the cursor is at %v", got)
	}
}

func TestShiftClickOnTheControlFoldsTheChildren(t *testing.T) {
	r := newFoldRig(t, nil)
	release := r.holdShift()
	r.click(r.control(0))
	release()
	if !r.vm.FoldedLine(2) || r.vm.FoldedLine(0) {
		t.Errorf("Shift folds what is inside first: 2 %v, 0 %v", r.vm.FoldedLine(2), r.vm.FoldedLine(0))
	}
}

func TestClickingThePlaceholderUnfolds(t *testing.T) {
	r := newFoldRig(t, nil)
	runFold(t, r.vm, "editor.foldLevel1", nil)
	r.ts.Pump(time.Millisecond)
	if !r.vm.FoldedLine(0) {
		t.Fatal("not folded")
	}
	// the text of a folded line is clicked as ever: the cursor goes there, the fold stays
	r.click(r.at(0, 3))
	if !r.vm.FoldedLine(0) {
		t.Errorf("a click on the text unfolded")
	}
	// the space after the placeholder does not unfold (editor.unfoldOnClickAfterEndOfLine is off)
	r.click(ui.Offset{Dx: r.gutter + 300, Dy: r.lineH / 2})
	if !r.vm.FoldedLine(0) {
		t.Errorf("a click far after the text unfolded")
	}
	r.click(r.afterText(0, 0))
	if r.vm.FoldedLine(0) {
		t.Errorf("a click on the placeholder did not unfold")
	}
	noErrors(t, r.ts)
}

func TestClickAfterTheEndOfALineUnfoldsWithTheSetting(t *testing.T) {
	r := newFoldRig(t, func(o *Options) { o.UnfoldOnClickAfterEndOfLine = true })
	runFold(t, r.vm, "editor.foldLevel1", nil)
	r.ts.Pump(time.Millisecond)
	r.click(ui.Offset{Dx: r.gutter + 300, Dy: r.lineH / 2})
	if r.vm.FoldedLine(0) {
		t.Errorf("the click after the end of the line did not unfold")
	}
}

func TestHitTestingFollowsTheFolds(t *testing.T) {
	r := newFoldRig(t, nil)
	runFold(t, r.vm, "editor.foldLevel1", nil)
	r.ts.Pump(time.Millisecond)
	// shown: lines 0 5 6 8 9, as the rows 0 to 4
	for row, line := range []int{0, 5, 6, 8, 9} {
		r.click(r.at(row, 0))
		if got := r.vm.Cursors().Primary().Position(); got != (buffer.Position{Line: line}) {
			t.Errorf("row %d: the cursor is at %v, want line %d", row, got, line)
		}
	}
	// the line numbers of the rows
	r.click(r.numberAt(1))
	if got := r.vm.Cursors().Primary().Selection; got.Start().Line != 5 {
		t.Errorf("the number of row 1 selects %v", got)
	}
}

func TestFoldingControlsFollowTheSetting(t *testing.T) {
	for _, tc := range []struct {
		name    string
		setting string
		hover   bool
		golden  string
	}{
		{"always", "always", false, "folding_always"},
		{"mouseover without the mouse", "mouseover", false, "folding_mouseover"},
		{"mouseover with the mouse", "mouseover", true, "folding_hover"},
		{"never", "never", false, "folding_never"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newFoldRig(t, func(o *Options) { o.ShowFoldingControls = tc.setting })
			// func b is folded, func a is open with its inner region folded
			runFold(t, r.vm, "editor.fold", map[string]any{"selectionLines": []any{6.0}})
			runFold(t, r.vm, "editor.fold", map[string]any{"selectionLines": []any{2.0}})
			r.vm.setGutterHover(tc.hover)
			r.ts.Pump(time.Millisecond)
			noErrors(t, r.ts)
			r.ts.MatchesGolden(tc.golden)
		})
	}
}

func TestGoldenFoldedLight(t *testing.T) {
	vm := NewViewModel(newTestDoc(foldingSample), goOptions(), nil)
	vm.StartFolding(nil, 0)
	computeFolds(t, vm)
	cursorAt(vm, 9, 0)
	runFold(t, vm, "editor.foldLevel1", nil)
	ts := testutil.New(t, ui.Size{Width: 640, Height: 300})
	pumpEditorColors(t, ts, vm, EditorOpts{Autofocus: true}, bundledColors(t, "Light Modern"))
	noErrors(t, ts)
	ts.MatchesGolden("folding_light")
}

func TestFoldRowsAreGoneFromTheTextLayer(t *testing.T) {
	r := newFoldRig(t, nil)
	runFold(t, r.vm, "editor.foldLevel1", nil)
	r.ts.Pump(time.Millisecond)
	layer := r.ts.RenderObject(gluttertest.ByType[*textLayer]()).(*RenderEditorText)
	if got := layer.PositionAt(ui.Offset{Dx: 1, Dy: 19 + 5}); got != (buffer.Position{Line: 5}) {
		t.Errorf("row 1 is line 5: %v", got)
	}
	// a position in a hidden line is shown on the row of the line that holds the fold
	if rect := layer.RectOf(buffer.Position{Line: 3, Col: 0}); rect.Top != 0 {
		t.Errorf("rect of a hidden position: %v", rect)
	}
}
