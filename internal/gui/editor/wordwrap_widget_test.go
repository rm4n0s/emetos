package editor

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// wrapWidgetText has lines that wrap in a 400 pixel editor, one of them indented.
const wrapWidgetText = "// Package greeter says hello in a few languages, and this comment is long enough to wrap.\n" +
	"package greeter\n" +
	"\n" +
	"func Say(language, name string) string {\n" +
	"\treturn fmt.Sprintf(\"%s, %s! You are welcome to this very long greeting that goes on\", name, language)\n" +
	"}\n"

// newMouseRigSized is newMouseRig in a window of the given size.
func newMouseRigSized(t *testing.T, size ui.Size, text string, tweak func(*Options)) *mouseRig {
	t.Helper()
	opts := goOptions()
	if tweak != nil {
		tweak(&opts)
	}
	vm := NewViewModel(newTestDoc(text), opts, nil)
	ts := testutil.New(t, size)
	pumpEditorWith(t, ts, vm, EditorOpts{Autofocus: true})
	g := newGeometry(vm, ts.Dispatcher().TextEngine())
	return &mouseRig{t: t, ts: ts, vm: vm, gutter: vm.GutterWidths(ts.Dispatcher().TextEngine()).Total(), cw: g.charWidth, lineH: opts.LineHeight}
}

// newWrapRig is an editor that wraps its lines, on screen.
func newWrapRig(t *testing.T, size ui.Size, text string, tweak func(*Options)) *mouseRig {
	t.Helper()
	r := newMouseRigSized(t, size, text, func(o *Options) {
		o.WordWrap, o.WrappingIndent, o.WrappingStrategy, o.StopRenderingLineAfter = "on", "same", "simple", 10000
		o.RenderLineHighlight = "all"
		if tweak != nil {
			tweak(o)
		}
	})
	t.Cleanup(r.vm.DisposeWrap)
	r.settle()
	return r
}

func (r *mouseRig) settle() {
	r.t.Helper()
	for range 4 {
		r.ts.Pump(time.Millisecond)
	}
	r.ts.FlushPosted()
	for range 3 {
		r.ts.Pump(time.Millisecond)
	}
}

func TestTheEditorWrapsItsLinesOnScreen(t *testing.T) {
	r := newWrapRig(t, ui.Size{Width: 440, Height: 260}, wrapWidgetText, nil)
	if !r.vm.Wrapping() {
		t.Fatal("not wrapping")
	}
	if rowsOf(r.vm, 0) < 2 || rowsOf(r.vm, 4) < 2 || rowsOf(r.vm, 1) != 1 {
		t.Errorf("rows of lines 0, 1, 4: %d %d %d", rowsOf(r.vm, 0), rowsOf(r.vm, 1), rowsOf(r.vm, 4))
	}
	noErrors(t, r.ts)
}

func TestClickingOnWrappedRows(t *testing.T) {
	r := newWrapRig(t, ui.Size{Width: 440, Height: 260}, wrapWidgetText, nil)
	proj := r.vm.Projection()
	second := proj.RowCount() // find the row that continues line 0
	for row := range proj.RowCount() {
		if line, start, _ := proj.RowSpan(row); line == 0 && start > 0 {
			second = row
			break
		}
	}
	_, start, _ := proj.RowSpan(second)
	// a click at the start of the text of the row, then in the middle of its text
	g := newGeometry(r.vm, r.ts.Dispatcher().TextEngine())
	y := float64(second)*r.lineH + r.lineH/2
	r.click(ui.Offset{Dx: r.gutter + g.indentPx(second) + 0.1, Dy: y})
	if got := r.vm.Cursors().Primary().Position(); got != (buffer.Position{Line: 0, Col: start}) {
		t.Errorf("a click at the start of the continuation row: %v, want column %d", got, start)
	}
	r.click(ui.Offset{Dx: r.gutter + g.indentPx(second) + 5*r.cw + 0.1, Dy: y})
	if got := r.vm.Cursors().Primary().Position(); got != (buffer.Position{Line: 0, Col: start + 5}) {
		t.Errorf("a click five characters into it: %v, want column %d", got, start+5)
	}
	// a click after the end of the last row of the line is the end of the line
	last := proj.RowCount()
	for row := range proj.RowCount() {
		if line, _, _ := proj.RowSpan(row); line == 4 {
			last = row
		}
	}
	r.click(ui.Offset{Dx: r.gutter + 300, Dy: float64(last)*r.lineH + r.lineH/2})
	text, _ := r.vm.Document().Snapshot().LineContent(4)
	if got := r.vm.Cursors().Primary().Position(); got != (buffer.Position{Line: 4, Col: len(text)}) {
		t.Errorf("a click after the line: %v", got)
	}
	noErrors(t, r.ts)
}

func TestTheCursorShowsOnTheRowOfItsPosition(t *testing.T) {
	r := newWrapRig(t, ui.Size{Width: 440, Height: 260}, wrapWidgetText, nil)
	cursorAt(r.vm, 0, 60)
	r.settle()
	row := r.vm.RowOfPosition(buffer.Position{Line: 0, Col: 60})
	if row == 0 {
		t.Fatalf("column 60 of line 0 is on the first row")
	}
	g := newGeometry(r.vm, r.ts.Dispatcher().TextEngine())
	rect := g.rectOf(buffer.Position{Line: 0, Col: 60})
	if rect.Top != float64(row)*r.lineH {
		t.Errorf("the rectangle is at %v, row %d is at %v", rect.Top, row, float64(row)*r.lineH)
	}
	_, start, _ := r.vm.Projection().RowSpan(row)
	want := g.indentPx(row) + float64(60-start)*r.cw
	if d := rect.Left - want; d > 0.5 || d < -0.5 {
		t.Errorf("the rectangle starts at x %v, want %v", rect.Left, want)
	}
}

func TestResizingTheEditorWrapsAgain(t *testing.T) {
	r := newWrapRig(t, ui.Size{Width: 700, Height: 260}, wrapWidgetText, nil)
	wide := r.vm.Projection().RowCount()
	r.ts.SetViewSize(ui.Size{Width: 360, Height: 260})
	r.settle()
	narrow := r.vm.Projection().RowCount()
	if narrow <= wide {
		t.Errorf("%d rows when narrow, %d when wide", narrow, wide)
	}
	r.ts.SetViewSize(ui.Size{Width: 700, Height: 260})
	r.settle()
	if got := r.vm.Projection().RowCount(); got != wide {
		t.Errorf("%d rows after going back, want %d", got, wide)
	}
	noErrors(t, r.ts)
}

func TestTheCurrentLineIsHighlightedOnAllItsRows(t *testing.T) {
	r := newWrapRig(t, ui.Size{Width: 440, Height: 260}, wrapWidgetText, nil)
	cursorAt(r.vm, 0, 5)
	rows := cursorRows(r.vm)
	if len(rows) != rowsOf(r.vm, 0) || rows[0] != 0 {
		t.Errorf("rows of the highlight %v, the line has %d", rows, rowsOf(r.vm, 0))
	}
	plain := newMouseRig(t, wrapWidgetText, nil)
	cursorAt(plain.vm, 0, 5)
	if got := cursorRows(plain.vm); len(got) != 1 {
		t.Errorf("without wrapping: %v", got)
	}
}

func TestGoldenWordWrap(t *testing.T) {
	r := newWrapRig(t, ui.Size{Width: 440, Height: 260}, wrapWidgetText, nil)
	// a selection that runs over a wrapped line and the cursor on a continuation row
	r.vm.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: 0, Col: 40}, Active: buffer.Position{Line: 4, Col: 30}}})
	r.vm.SelectionsChanged()
	r.settle()
	noErrors(t, r.ts)
	r.ts.MatchesGolden("wordwrap")
}

func TestGoldenWordWrapIndentAndCursor(t *testing.T) {
	r := newWrapRig(t, ui.Size{Width: 440, Height: 260}, wrapWidgetText, nil)
	cursorAt(r.vm, 4, 70)
	r.vm.SelectionsChanged()
	r.settle()
	noErrors(t, r.ts)
	r.ts.MatchesGolden("wordwrap_cursor")
}

func TestGoldenWordWrapWithFoldsAndFind(t *testing.T) {
	r := newWrapRig(t, ui.Size{Width: 440, Height: 260}, wrapWidgetText, func(o *Options) { o.ShowFoldingControls = "always" })
	r.vm.StartFolding(nil, 0)
	computeFolds(t, r.vm)
	runFold(t, r.vm, "editor.foldAll", nil)
	cursorAt(r.vm, 0, 0)
	if err := r.vm.RunCommand("editor.actions.findWithArgs", map[string]any{"searchString": "o"}); err != nil {
		t.Fatal(err)
	}
	r.settle()
	noErrors(t, r.ts)
	r.ts.MatchesGolden("wordwrap_fold_find")
}

func TestWordWrapIsOffByDefaultAndTogglesLive(t *testing.T) {
	r := newMouseRigSized(t, ui.Size{Width: 440, Height: 260}, wrapWidgetText, func(o *Options) {
		o.WordWrap, o.WrappingIndent, o.WrappingStrategy, o.StopRenderingLineAfter = "off", "same", "simple", 10000
	})
	if r.vm.Wrapping() {
		t.Fatal("wraps by default in the test options")
	}
	before := r.vm.Projection().RowCount()
	if err := r.vm.RunCommand("editor.action.toggleWordWrap", nil); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if got := r.vm.Projection().RowCount(); got <= before {
		t.Errorf("rows %d after toggling on, %d before", got, before)
	}
	text := strings.Count(wrapWidgetText, "\n")
	if r.vm.Projection().RowCount() < text {
		t.Errorf("fewer rows than lines")
	}
	noErrors(t, r.ts)
}
