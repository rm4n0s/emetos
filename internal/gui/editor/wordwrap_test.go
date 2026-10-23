package editor

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/folding"
	"github.com/rm4n0s/emetos/internal/core/wrap"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// wrapSample has lines of 1, 2, 2 and 1 rows at 20 columns.
const wrapSample = "short\n" +
	"alpha beta gamma delta epsilon\n" + // "alpha beta gamma " "delta epsilon"
	"one two three four five six seven eight\n" + // "one two three four " "five six seven eight"
	"end"

// wrapVM is a view model that wraps at a number of columns of 8 pixels: the text layer would
// measure the editor and the font; the test does.
func wrapVM(t *testing.T, text string, mode string, columns int, tweak func(*Options)) *ViewModel {
	t.Helper()
	opts := goOptions()
	opts.WordWrap, opts.WordWrapColumn, opts.WrappingIndent, opts.WrappingStrategy, opts.StopRenderingLineAfter = mode, columns, "none", "simple", 10000
	if tweak != nil {
		tweak(&opts)
	}
	vm := NewViewModel(newTestDoc(text), opts, nil)
	t.Cleanup(vm.DisposeWrap)
	setWrapViewport(vm, 20)
	return vm
}

// setWrapViewport makes the editor as wide as a row of the columns takes with its scrollbar.
func setWrapViewport(vm *ViewModel, columns int) {
	vm.SetTextMetrics(float64(columns)*8+kit.DefaultVerticalScrollbarSize, 200, 1000, 0, 8)
}

func rowsOf(vm *ViewModel, line int) int {
	_, n := vm.Projection().LineRows(line)
	return n
}

// rowTexts are the texts of the rows of a line.
func rowTexts(vm *ViewModel, line int) []string {
	proj := vm.Projection()
	text, _ := vm.Document().Snapshot().LineContent(line)
	first, count := proj.LineRows(line)
	var out []string
	for row := first; row < first+count; row++ {
		_, start, end := proj.RowSpan(row)
		if end < 0 {
			end = len(text)
		}
		out = append(out, text[start:end])
	}
	return out
}

func TestWrapColumnsFollowTheMode(t *testing.T) {
	cases := []struct {
		name    string
		mode    string
		column  int
		columns int // of the editor
		want    int
	}{
		{"off", "off", 30, 20, 0},
		{"an empty mode is off", "", 30, 20, 0},
		{"on wraps at the width of the editor", "on", 30, 20, 20},
		{"on with a wide column setting", "on", 200, 20, 20},
		{"wordWrapColumn wraps at the setting", "wordWrapColumn", 12, 20, 12},
		{"wordWrapColumn does not follow the editor", "wordWrapColumn", 50, 20, 50},
		{"bounded takes the smaller: the editor", "bounded", 30, 20, 20},
		{"bounded takes the smaller: the setting", "bounded", 15, 20, 15},
		{"a narrow editor has at least a column", "on", 30, 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := wrapVM(t, wrapSample, tc.mode, tc.column, nil)
			setWrapViewport(vm, tc.columns)
			if got := vm.WrapColumn(); got != tc.want {
				t.Errorf("columns %d, want %d", got, tc.want)
			}
			if vm.Wrapping() != (tc.want > 0) {
				t.Errorf("Wrapping = %v", vm.Wrapping())
			}
		})
	}
}

func TestNoWrappingBeforeTheEditorIsMeasuredOrInLargeFileMode(t *testing.T) {
	opts := goOptions()
	opts.WordWrap = "on"
	vm := NewViewModel(newTestDoc(wrapSample), opts, nil)
	if vm.Wrapping() {
		t.Error("wrapping before the text layer measured anything")
	}
	doc := document.FromRead(document.FileURI("/big.txt"), fileio.ReadResult{Text: wrapSample, LineEnding: fileio.LF, LargeFile: true}, document.NewOptions())
	large := NewViewModel(doc, opts, nil)
	setWrapViewport(large, 20)
	if large.Wrapping() {
		t.Error("a document in large file mode wraps")
	}
}

func TestLinesBecomeRows(t *testing.T) {
	vm := wrapVM(t, wrapSample, "on", 80, nil)
	proj := vm.Projection()
	if got := []int{rowsOf(vm, 0), rowsOf(vm, 1), rowsOf(vm, 2), rowsOf(vm, 3)}; !slices.Equal(got, []int{1, 2, 2, 1}) {
		t.Fatalf("rows of the lines: %v", got)
	}
	if proj.RowCount() != 6 {
		t.Errorf("%d rows", proj.RowCount())
	}
	if got := rowTexts(vm, 1); !slices.Equal(got, []string{"alpha beta gamma ", "delta epsilon"}) {
		t.Errorf("rows of line 1: %q", got)
	}
	if got := rowTexts(vm, 2); !slices.Equal(got, []string{"one two three four ", "five six seven eight"}) {
		t.Errorf("rows of line 2: %q", got)
	}
	for row, want := range []struct{ line, start int }{{0, 0}, {1, 0}, {1, 17}, {2, 0}, {2, 19}, {3, 0}} {
		line, start, _ := proj.RowSpan(row)
		if line != want.line || start != want.start {
			t.Errorf("row %d is line %d from %d, want %d from %d", row, line, start, want.line, want.start)
		}
	}
	if proj.Height() != 6*vm.Options().LineHeight || proj.RowAt(proj.RowTop(4)+1) != 4 {
		t.Errorf("height %v", proj.Height())
	}
	// the cursor's row
	if got := vm.RowOfPosition(buffer.Position{Line: 2, Col: 20}); got != 4 {
		t.Errorf("row of (2,20) is %d", got)
	}
	if got := vm.RowOfPosition(buffer.Position{Line: 2, Col: 18}); got != 3 {
		t.Errorf("row of (2,18) is %d", got)
	}
	if got := vm.RowOfPosition(buffer.Position{Line: 2, Col: 19}); got != 4 {
		t.Errorf("a position at the start of a row is on it: %d", got)
	}
	if got := vm.RowOfPosition(buffer.Position{Line: 2, Col: 39}); got != 4 {
		t.Errorf("the end of the line is on its last row: %d", got)
	}
}

func TestWrappingIndentGivesContinuationRowsAnIndent(t *testing.T) {
	text := "    indented text that wraps around the edge\n"
	for _, tc := range []struct {
		mode string
		want int
	}{{"none", 0}, {"same", 4}, {"indent", 8}, {"deepIndent", 12}} {
		vm := wrapVM(t, text, "on", 80, func(o *Options) { o.WrappingIndent = tc.mode })
		proj := vm.Projection().(*wrappedProjection)
		if proj.RowCount() < 2 {
			t.Fatalf("%s: not wrapped", tc.mode)
		}
		if got := proj.RowIndentColumns(0); got != 0 {
			t.Errorf("%s: the first row is indented by %d", tc.mode, got)
		}
		if got := proj.RowIndentColumns(1); got != tc.want {
			t.Errorf("%s: the continuation is indented by %d, want %d", tc.mode, got, tc.want)
		}
	}
}

func TestEditsChangeTheRows(t *testing.T) {
	vm := wrapVM(t, wrapSample, "on", 80, nil)
	doc := vm.Document()
	edit := func(t *testing.T, l1, c1, l2, c2 int, text string) {
		t.Helper()
		if _, err := doc.Edit([]document.Edit{{Range: rng(l1, c1, l2, c2), Text: text}}, document.EditOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	rows := func() []int {
		out := make([]int, doc.Snapshot().LineCount())
		for i := range out {
			out[i] = rowsOf(vm, i)
		}
		return out
	}
	// a short line grows into two rows
	edit(t, 0, 5, 0, 5, " and a lot more text to make it long")
	if got := rows(); !slices.Equal(got, []int{3, 2, 2, 1}) {
		t.Errorf("after typing in line 0: %v", got)
	}
	// a line is deleted
	edit(t, 1, 0, 2, 0, "")
	if got := rows(); !slices.Equal(got, []int{3, 2, 1}) {
		t.Errorf("after deleting line 1: %v", got)
	}
	// lines are inserted
	edit(t, 1, 0, 1, 0, "x\nyyyy yyyy yyyy yyyy yyyy yyyy\n")
	if got := rows(); !slices.Equal(got, []int{3, 1, 2, 2, 1}) {
		t.Errorf("after inserting two lines: %v", got)
	}
	// one edit that joins two lines
	edit(t, 1, 1, 2, 0, "")
	if got := rows(); len(got) != 4 {
		t.Errorf("after joining: %v", got)
	}
	// undo goes back
	for range 3 {
		if _, err := doc.Undo(); err != nil {
			t.Fatal(err)
		}
	}
	if got := rows(); !slices.Equal(got, []int{3, 2, 2, 1}) {
		t.Errorf("after undoing three edits: %v", got)
	}
	// the breaks of every line are those of its text
	for line := range doc.Snapshot().LineCount() {
		text, _ := doc.Snapshot().LineContent(line)
		want := wrap.Compute(text, wrap.Options{TabSize: 4, Column: 80 / 4, Indent: wrap.IndentNone, BreakBefore: wrap.DefaultBreakBefore, BreakAfter: wrap.DefaultBreakAfter})
		if got := rowsOf(vm, line); got != want.RowCount() {
			t.Errorf("line %d has %d rows, its text has %d", line, got, want.RowCount())
		}
	}
}

func TestWrapWithFolds(t *testing.T) {
	text := "func a() {\n    alpha beta gamma delta epsilon\n    one two three four five six seven\n}\nafter that line is long enough to wrap twice"
	vm := wrapVM(t, text, "on", 80, nil)
	vm.StartFolding(nil, 0)
	computeFolds(t, vm)
	before := vm.Projection().RowCount()
	runFold(t, vm, "editor.foldAll", nil)
	proj := vm.Projection()
	if !vm.FoldedLine(0) {
		t.Fatal("line 0 is not folded")
	}
	for _, line := range []int{1, 2} {
		if _, n := proj.LineRows(line); n != 0 {
			t.Errorf("hidden line %d has %d rows", line, n)
		}
	}
	if first, _ := proj.LineRows(2); first != 0 {
		t.Errorf("a hidden line stands for the last row of the line that holds the fold: row %d", first)
	}
	if got := proj.RowCount(); got != 1+1+3 {
		t.Errorf("rows %d: the folded line, the closing brace and the three of the last (was %d)", got, before)
	}
	for row, want := range []int{0, 3, 4, 4, 4} {
		if line, _, _ := proj.RowSpan(row); line != want {
			t.Errorf("row %d is line %d, want %d", row, line, want)
		}
	}
	runFold(t, vm, "editor.unfoldAll", nil)
	if got := vm.Projection().RowCount(); got != before {
		t.Errorf("rows after unfolding: %d, want %d", got, before)
	}
	_ = folding.Region{}
}

func TestToggleWordWrap(t *testing.T) {
	vm := wrapVM(t, wrapSample, "off", 80, nil)
	if vm.Wrapping() {
		t.Fatal("wraps while off")
	}
	if err := vm.RunCommand("editor.action.toggleWordWrap", nil); err != nil {
		t.Fatal(err)
	}
	if !vm.Wrapping() || vm.Projection().RowCount() != 6 {
		t.Errorf("toggled on: wrapping %v, %d rows", vm.Wrapping(), vm.Projection().RowCount())
	}
	if err := vm.RunCommand("editor.action.toggleWordWrap", nil); err != nil {
		t.Fatal(err)
	}
	if vm.Wrapping() || vm.Projection().RowCount() != 4 {
		t.Errorf("toggled off: wrapping %v, %d rows", vm.Wrapping(), vm.Projection().RowCount())
	}
	// the setting says on: the command turns it off for this editor
	on := wrapVM(t, wrapSample, "on", 80, nil)
	if err := on.RunCommand("editor.action.toggleWordWrap", nil); err != nil || on.Wrapping() {
		t.Errorf("toggled the setting off: %v, wrapping %v", err, on.Wrapping())
	}
}

func TestResizingMakesTheRowsAgain(t *testing.T) {
	vm := wrapVM(t, wrapSample, "on", 80, nil)
	if vm.Projection().RowCount() != 6 {
		t.Fatalf("rows %d", vm.Projection().RowCount())
	}
	setWrapViewport(vm, 40)
	if got := vm.Projection().RowCount(); got != 4 {
		t.Errorf("at 40 columns the sample is %d rows, want 4 (the longest line is 39 columns)", got)
	}
	setWrapViewport(vm, 10)
	if got := vm.Projection().RowCount(); got <= 6 {
		t.Errorf("at 10 columns %d rows", got)
	}
}

func TestALongLineIsNotWrapped(t *testing.T) {
	long := strings.Repeat("word ", 40)
	vm := wrapVM(t, long+"\nshort line that wraps once more than that\n", "on", 80, func(o *Options) { o.StopRenderingLineAfter = 100 })
	if rowsOf(vm, 0) != 1 {
		t.Errorf("a line longer than editor.stopRenderingLineAfter has %d rows", rowsOf(vm, 0))
	}
	if rowsOf(vm, 1) < 2 {
		t.Error("a short line wraps")
	}
	never := wrapVM(t, long+"\n", "on", 80, func(o *Options) { o.StopRenderingLineAfter = -1 })
	if rowsOf(never, 0) < 2 {
		t.Error("with no limit a long line wraps")
	}
}

// fakeTimers is in decoration_test.go.

func TestTheBreaksAreMadeInSteps(t *testing.T) {
	var b strings.Builder
	for i := range 20000 {
		b.WriteString("line number ")
		b.WriteString(strings.Repeat("x", i%5))
		b.WriteString(" with enough words to need a second row of text\n")
	}
	clock := &fakeTimers{}
	opts := goOptions()
	opts.WordWrap, opts.WrappingIndent, opts.StopRenderingLineAfter = "on", "none", 10000
	vm := NewViewModel(newTestDoc(b.String()), opts, nil)
	vm.SetTimer(clock.after)
	t.Cleanup(vm.DisposeWrap)
	setWrapViewport(vm, 20)
	proj := vm.Projection()
	if !vm.Wrapping() || vm.WrapPending() == 0 {
		t.Fatalf("wrapping %v, %d lines pending", vm.Wrapping(), vm.WrapPending())
	}
	// the lines around the top are known at once; the others are one row until a step
	if proj.RowCount() >= 20001*3 {
		t.Errorf("rows %d before the steps", proj.RowCount())
	}
	steps := 0
	for vm.WrapPending() > 0 && steps < 50 {
		clock.advance(2 * time.Millisecond)
		steps++
	}
	if vm.WrapPending() != 0 {
		t.Fatalf("%d lines pending after %d steps", vm.WrapPending(), steps)
	}
	if steps < 2 {
		t.Errorf("done in %d step: not in steps", steps)
	}
	if rows := vm.Projection().RowCount(); rows < 20000*3 {
		t.Errorf("%d rows for 20000 lines of three rows", rows)
	}
}

func TestTheTopOfTheEditorStaysWhereItIsWhileLinesAboveAreBroken(t *testing.T) {
	var b strings.Builder
	for range 6000 {
		b.WriteString("an ordinary line that is long enough to wrap in a narrow editor\n")
	}
	clock := &fakeTimers{}
	opts := goOptions()
	opts.WordWrap, opts.WrappingIndent, opts.StopRenderingLineAfter = "on", "none", 10000
	vm := NewViewModel(newTestDoc(b.String()), opts, nil)
	vm.SetTimer(clock.after)
	t.Cleanup(vm.DisposeWrap)
	var scrolled []float64
	vm.SetScroller(func(top float64) { scrolled = append(scrolled, top); vm.SetScrollTop(top) })
	setWrapViewport(vm, 20)
	// the editor shows line 5000 at the top; the lines above are one row each until broken
	vm.SetScrollTop(vm.Projection().RowTop(5000))
	vm.Projection()
	steps := 0
	for vm.WrapPending() > 0 && steps < 100 {
		clock.advance(2 * time.Millisecond)
		steps++
	}
	line, _, _ := vm.Projection().RowSpan(vm.Projection().RowAt(vm.ScrollTop()))
	if line != 5000 {
		t.Errorf("the top of the editor is line %d after the breaks, want 5000", line)
	}
	if len(scrolled) == 0 {
		t.Error("the editor never scrolled to keep its place")
	}
}

func TestBreaksByLayoutForOtherThanColumns(t *testing.T) {
	ts := testutil.New(t, ui.Size{Width: 400, Height: 200})
	factory := ts.Dispatcher().TextEngine()
	text := "éééééééééééééééééééééééééééééé\nplain ascii line that wraps at the edge of the editor\n"
	for _, advanced := range []bool{false, true} {
		vm := wrapVM(t, text, "on", 80, func(o *Options) {
			if advanced {
				o.WrappingStrategy = "advanced"
			}
		})
		vm.SetTextEngine(factory, true)
		vm.SetTextMetrics(20*newGeometry(vm, factory).charWidth+kit.DefaultVerticalScrollbarSize, 200, 1000, 0, newGeometry(vm, factory).charWidth)
		if got := rowTexts(vm, 0); len(got) != 2 || len([]rune(got[0])) != 20 || strings.Join(got, "") != strings.Split(text, "\n")[0] {
			t.Errorf("advanced %v: rows of the line of é: %q", advanced, got)
		}
		if rowsOf(vm, 1) < 3 {
			t.Errorf("advanced %v: the ASCII line has %d rows", advanced, rowsOf(vm, 1))
		}
	}
}

func TestColumnCountable(t *testing.T) {
	for text, want := range map[string]bool{"": true, "plain\ttext": true, "é": false, "日本": false, "bell\a": false, "del\x7f": false} {
		if got := columnCountable(text); got != want {
			t.Errorf("columnCountable(%q) = %v, want %v", text, got, want)
		}
	}
}

func TestCursorMovesByWrappedRows(t *testing.T) {
	vm := wrapVM(t, wrapSample, "on", 80, nil)
	run := func(id string) {
		t.Helper()
		if err := vm.RunCommand(id, nil); err != nil {
			t.Fatal(err)
		}
	}
	cursorAt(vm, 1, 3)
	run("cursorDown")
	if got := vm.Cursors().Primary().Position(); got != (buffer.Position{Line: 1, Col: 20}) {
		t.Errorf("down goes to the next row of the line: %v", got)
	}
	run("cursorEnd")
	if got := vm.Cursors().Primary().Position(); got != (buffer.Position{Line: 1, Col: 30}) {
		t.Errorf("End goes to the end of the last row: %v", got)
	}
	run("cursorHome")
	if got := vm.Cursors().Primary().Position(); got != (buffer.Position{Line: 1, Col: 17}) {
		t.Errorf("Home on a continuation row goes to the start of the row: %v", got)
	}
	run("cursorUp")
	if got := vm.Cursors().Primary().Position(); got.Line != 1 || got.Col > 16 {
		t.Errorf("up goes to the row before: %v", got)
	}
	run("cursorEnd")
	if got := vm.Cursors().Primary().Position(); got != (buffer.Position{Line: 1, Col: 16}) {
		t.Errorf("End on a row that is not the last stops before its trailing space: %v", got)
	}
	// page down moves a page of rows
	cursorAt(vm, 0, 0)
	run("cursorDown")
	run("cursorDown")
	if got := vm.Cursors().Primary().Position(); got.Line != 1 || got.Col < 17 {
		t.Errorf("two rows down from line 0: %v", got)
	}
}
