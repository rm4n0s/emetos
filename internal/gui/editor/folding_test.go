package editor

import (
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/folding"
)

const foldingSample = `func a() {
	x := 1
	if x > 0 {
		y()
	}
}
func b() {
	z()
}
last`

// foldedVM makes a view model over text with its folding regions known.
func foldedVM(t *testing.T, text string) *ViewModel {
	t.Helper()
	vm := NewViewModel(newTestDoc(text), testOptions(), nil)
	vm.StartFolding(nil, 0)
	computeFolds(t, vm)
	// the cursor is out of the regions: Fold Level folds the regions that do not hold it
	cursorAt(vm, vm.Document().Snapshot().LineCount()-1, 0)
	return vm
}

// computeFolds finds the regions of the document now, as the background updater would.
func computeFolds(t *testing.T, vm *ViewModel) {
	t.Helper()
	regions, err := folding.ComputeRanges(folding.SnapshotText(vm.Document().Snapshot()), 4, false, nil, folding.Limit{})
	if err != nil {
		t.Fatal(err)
	}
	if err := vm.UpdateFolding(regions); err != nil {
		t.Fatal(err)
	}
}

func runFold(t *testing.T, vm *ViewModel, id string, args any) {
	t.Helper()
	if err := vm.RunCommand(id, args); err != nil {
		t.Fatalf("%s: %v", id, err)
	}
}

func cursorAt(vm *ViewModel, line, col int) {
	vm.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: line, Col: col})})
}

func TestFoldedProjection(t *testing.T) {
	spans := []folding.LineSpan{{First: 2, Last: 4}, {First: 7, Last: 8}}
	p := newFoldedProjection(10, 20, spans)
	// lines 0 1 [2 3 4] 5 6 [7 8] 9 are the rows 0 1 2 3 4 5
	wantLines := []int{0, 1, 5, 6, 9}
	if got := p.RowCount(); got != len(wantLines) {
		t.Fatalf("RowCount %d, want %d", got, len(wantLines))
	}
	for row, line := range wantLines {
		if got, start, end := p.RowSpan(row); got != line || start != 0 || end != -1 {
			t.Errorf("RowSpan(%d) = %d,%d,%d, want line %d", row, got, start, end, line)
		}
		if first, count := p.LineRows(line); first != row || count != 1 {
			t.Errorf("LineRows(%d) = %d,%d, want %d,1", line, first, count, row)
		}
	}
	// a hidden line has no row of its own: its first is the row of the line that holds the fold
	hidden := map[int]int{2: 1, 3: 1, 4: 1, 7: 3, 8: 3}
	for line, row := range hidden {
		if first, count := p.LineRows(line); first != row || count != 0 {
			t.Errorf("LineRows(%d) = %d,%d, want %d,0", line, first, count, row)
		}
	}
	if first, count := p.LineRows(10); first != 0 || count != 0 {
		t.Errorf("LineRows past the end = %d,%d", first, count)
	}
	if got := p.Height(); got != 100 {
		t.Errorf("Height %v", got)
	}
	for _, tc := range []struct {
		y   float64
		row int
	}{{-5, 0}, {0, 0}, {19.9, 0}, {20, 1}, {85, 4}, {1000, 4}} {
		if got := p.RowAt(tc.y); got != tc.row {
			t.Errorf("RowAt(%v) = %d, want %d", tc.y, got, tc.row)
		}
	}
	if p.RowTop(3) != 60 || p.RowHeight(3) != 20 {
		t.Errorf("RowTop/RowHeight")
	}
	for _, tc := range []struct {
		line        int
		first, last int
		ok          bool
	}{{1, 0, 0, false}, {2, 2, 4, true}, {4, 2, 4, true}, {5, 0, 0, false}, {8, 7, 8, true}} {
		if first, last, ok := p.hiddenSpan(tc.line); ok != tc.ok || first != tc.first || last != tc.last {
			t.Errorf("hiddenSpan(%d) = %d,%d,%v", tc.line, first, last, ok)
		}
	}
}

func TestFoldedProjectionCutsTheSpans(t *testing.T) {
	// a span that starts on line 0, past the end or backwards hides what it can
	p := newFoldedProjection(5, 10, []folding.LineSpan{{First: 0, Last: 1}, {First: 3, Last: 99}, {First: 6, Last: 5}})
	if got := p.RowCount(); got != 2 {
		t.Errorf("RowCount %d, want 2 (lines 0 and 2)", got)
	}
	if line, _, _ := p.RowSpan(1); line != 2 {
		t.Errorf("row 1 shows line %d", line)
	}
}

func TestFoldLevelHidesRows(t *testing.T) {
	vm := foldedVM(t, foldingSample)
	if got := vm.Projection().RowCount(); got != 10 {
		t.Fatalf("rows before folding: %d", got)
	}
	runFold(t, vm, "editor.foldLevel1", nil)
	proj := vm.Projection()
	// func a hides lines 1-4, func b hides line 7
	var lines []int
	for row := range proj.RowCount() {
		line, _, _ := proj.RowSpan(row)
		lines = append(lines, line)
	}
	if want := []int{0, 5, 6, 8, 9}; !slices.Equal(lines, want) {
		t.Fatalf("lines shown %v, want %v", lines, want)
	}
	if !vm.FoldedLine(0) || !vm.FoldedLine(6) || vm.FoldedLine(5) {
		t.Errorf("FoldedLine: %v %v %v", vm.FoldedLine(0), vm.FoldedLine(6), vm.FoldedLine(5))
	}
	if got := proj.Height(); got != 5*19 {
		t.Errorf("Height %v", got)
	}
	runFold(t, vm, "editor.unfoldAll", nil)
	if got := vm.Projection().RowCount(); got != 10 {
		t.Errorf("rows after unfolding: %d", got)
	}
}

func TestFoldingStates(t *testing.T) {
	vm := foldedVM(t, foldingSample)
	runFold(t, vm, "editor.foldLevel2", nil)
	for _, tc := range []struct {
		line int
		want FoldingState
	}{{0, FoldingExpanded}, {1, FoldingNone}, {2, FoldingCollapsed}, {5, FoldingNone}, {6, FoldingExpanded}} {
		if got := vm.FoldingStateOf(tc.line); got != tc.want {
			t.Errorf("FoldingStateOf(%d) = %d, want %d", tc.line, got, tc.want)
		}
	}
	vm.SetOptions(func() Options { o := testOptions(); o.Folding = false; return o }())
	if vm.FoldingStateOf(2) != FoldingNone || vm.Projection().RowCount() != 10 {
		t.Errorf("without editor.folding nothing is folded")
	}
	vm.SetOptions(testOptions())
	if vm.FoldingStateOf(2) != FoldingCollapsed || vm.Projection().RowCount() != 9 {
		t.Errorf("editor.folding on again folds again: %d rows", vm.Projection().RowCount())
	}
}

func TestFoldCommandMovesTheCursorOutOfTheFold(t *testing.T) {
	vm := foldedVM(t, foldingSample)
	cursorAt(vm, 3, 2)
	runFold(t, vm, "editor.fold", nil) // the innermost region that holds the line
	if got := vm.Cursors().Primary().Position(); got != (buffer.Position{Line: 2, Col: len("\tif x > 0 {")}) {
		t.Errorf("the cursor is at %v, want the end of the line that holds the fold", got)
	}
	runFold(t, vm, "editor.fold", nil) // that one is folded: the one around it
	if got := vm.Cursors().Primary().Position(); got != (buffer.Position{Line: 0, Col: len("func a() {")}) {
		t.Errorf("the cursor is at %v after the second fold", got)
	}
	if got := vm.Projection().RowCount(); got != 6 {
		t.Errorf("rows %d, want 6", got)
	}
}

func TestMovementOverFolds(t *testing.T) {
	vm := foldedVM(t, foldingSample)
	runFold(t, vm, "editor.foldLevel1", nil) // shown: 0 5 6 8 9
	cursorAt(vm, 0, 3)
	steps := []struct {
		command string
		want    buffer.Position
	}{
		{"cursorDown", buffer.Position{Line: 5, Col: 1}},
		{"cursorDown", buffer.Position{Line: 6, Col: 3}},
		{"cursorDown", buffer.Position{Line: 8, Col: 1}},
		{"cursorDown", buffer.Position{Line: 9, Col: 3}},
		{"cursorUp", buffer.Position{Line: 8, Col: 1}},
		{"cursorUp", buffer.Position{Line: 6, Col: 3}},
		{"cursorUp", buffer.Position{Line: 5, Col: 1}},
		{"cursorUp", buffer.Position{Line: 0, Col: 3}},
		{"cursorEnd", buffer.Position{Line: 0, Col: len("func a() {")}},
		{"cursorRight", buffer.Position{Line: 5, Col: 0}},
		{"cursorLeft", buffer.Position{Line: 0, Col: len("func a() {")}},
	}
	for i, st := range steps {
		runFold(t, vm, st.command, nil)
		if got := vm.Cursors().Primary().Position(); got != st.want {
			t.Fatalf("step %d (%s): at %v, want %v", i, st.command, got, st.want)
		}
	}
	// the selection that goes down over a fold selects it whole
	runFold(t, vm, "cursorBottom", nil)
	runFold(t, vm, "cursorTop", nil)
	runFold(t, vm, "cursorDownSelect", nil)
	if sel := vm.Cursors().Primary().Selection; sel.Active.Line != 5 || sel.Anchor.Line != 0 {
		t.Errorf("selection %v", sel)
	}
}

func TestPageMovementCountsRows(t *testing.T) {
	vm := foldedVM(t, foldingSample)
	runFold(t, vm, "editor.foldLevel1", nil)
	vm.SetViewportHeight(19 * 3) // a page is 3-1 rows... pageRows keeps at least 1
	cursorAt(vm, 0, 0)
	runFold(t, vm, "cursorPageDown", nil)
	if got := vm.Cursors().Primary().Position().Line; got != 6 {
		t.Errorf("PageDown over folds went to line %d, want 6 (two rows down)", got)
	}
}

func TestRevealUnfoldsWhatHidesTheCursor(t *testing.T) {
	vm := foldedVM(t, foldingSample)
	runFold(t, vm, "editor.foldLevel1", nil)
	cursorAt(vm, 3, 0) // go to a line in a fold
	if got := vm.Projection().RowCount(); got != 9 {
		t.Errorf("rows %d, want 9: func a is open (func b is still folded)", got)
	}
	if vm.FoldedLine(0) || !vm.FoldedLine(6) {
		t.Errorf("func a is open: %v, func b is folded: %v", !vm.FoldedLine(0), vm.FoldedLine(6))
	}
	if got := vm.Cursors().Primary().Position(); got != (buffer.Position{Line: 3}) {
		t.Errorf("the cursor stays where it was sent: %v", got)
	}
}

func TestUndoRevealsAFold(t *testing.T) {
	vm := foldedVM(t, foldingSample)
	cursorAt(vm, 3, 2)
	runFold(t, vm, "type", "z")
	cursorAt(vm, 9, 0)
	runFold(t, vm, "editor.foldLevel1", nil)
	cursorAt(vm, 0, 0)
	if vm.Projection().RowCount() != 5 {
		t.Fatalf("not folded: %d rows", vm.Projection().RowCount())
	}
	runFold(t, vm, "undo", nil)
	if vm.FoldedLine(0) {
		t.Errorf("undo went to a line in the fold: it is open")
	}
	if got := vm.Cursors().Primary().Position().Line; got != 3 {
		t.Errorf("the cursor went to %d", got)
	}
}

func TestFoldsFollowEdits(t *testing.T) {
	vm := foldedVM(t, foldingSample)
	runFold(t, vm, "editor.foldLevel1", nil)
	cursorAt(vm, 0, 0)
	runFold(t, vm, "type", "// top\n") // two lines now: the fold starts one line lower
	if !vm.FoldedLine(1) || vm.FoldedLine(0) {
		t.Errorf("the fold did not follow the edit: %v %v", vm.FoldedLine(0), vm.FoldedLine(1))
	}
	if first, last, ok := vm.HiddenSpan(3); !ok || first != 2 || last != 5 {
		t.Errorf("hidden %d-%d %v, want 2-5", first, last, ok)
	}
}

func TestToggleFoldAt(t *testing.T) {
	vm := foldedVM(t, foldingSample)
	vm.ToggleFoldAt(2, true, false, false) // the control of an inner region
	if !vm.FoldedLine(2) {
		t.Fatalf("the control folds the region")
	}
	vm.ToggleFoldAt(2, true, false, false)
	if vm.FoldedLine(2) {
		t.Fatalf("and unfolds it again")
	}
	vm.ToggleFoldAt(1, true, false, false) // no region starts on line 1
	if vm.Projection().RowCount() != 10 {
		t.Errorf("a line that starts no region does nothing")
	}
	// recursive: the children first, then the parent when they are folded already
	vm.ToggleFoldAt(0, true, false, true)
	if !vm.FoldedLine(2) || vm.FoldedLine(0) {
		t.Errorf("first click folds the children only: %v %v", vm.FoldedLine(2), vm.FoldedLine(0))
	}
	vm.ToggleFoldAt(0, true, false, true)
	if !vm.FoldedLine(0) {
		t.Errorf("second click folds the parent")
	}
	// the placeholder only unfolds
	vm.ToggleFoldAt(0, false, false, false)
	if vm.FoldedLine(0) {
		t.Errorf("the placeholder unfolds")
	}
	vm.ToggleFoldAt(0, false, false, false)
	if vm.FoldedLine(0) {
		t.Errorf("the placeholder of an open region does nothing")
	}
	// Alt: the regions that are not around this one
	runFold(t, vm, "editor.unfoldAll", nil)
	vm.ToggleFoldAt(0, true, true, false)
	if !vm.FoldedLine(6) || vm.FoldedLine(2) || vm.FoldedLine(0) {
		t.Errorf("Alt folds the regions that are neither around nor inside: 6 %v, 2 %v, 0 %v", vm.FoldedLine(6), vm.FoldedLine(2), vm.FoldedLine(0))
	}
}

func TestFoldingViewState(t *testing.T) {
	vm := foldedVM(t, foldingSample)
	runFold(t, vm, "editor.foldLevel1", nil)
	state := vm.ViewState()
	if len(state.Folds) != 2 {
		t.Fatalf("view state folds %v", state.Folds)
	}

	// restored when the regions are known
	again := foldedVM(t, foldingSample)
	if err := again.RestoreViewState(state); err != nil {
		t.Fatal(err)
	}
	if !again.FoldedLine(0) || !again.FoldedLine(6) {
		t.Errorf("the folds were not restored")
	}

	// restored as soon as the regions are known
	late := NewViewModel(newTestDoc(foldingSample), testOptions(), nil)
	late.StartFolding(nil, 0)
	if err := late.RestoreViewState(state); err != nil {
		t.Fatal(err)
	}
	if late.FoldedLine(0) {
		t.Fatalf("folded before there were regions")
	}
	computeFolds(t, late)
	if !late.FoldedLine(0) || !late.FoldedLine(6) {
		t.Errorf("the folds waited for the regions and were not applied")
	}

	// a region whose text changed is not folded again
	other := foldedVM(t, "func a() {\n\tdifferent()\n}\nfunc b() {\n\tz()\n}\nlast")
	if err := other.RestoreViewState(state); err != nil {
		t.Fatal(err)
	}
	if other.FoldedLine(0) {
		t.Errorf("func a changed and is not folded")
	}
}

func TestFoldingCommandsWithoutFolding(t *testing.T) {
	vm := NewViewModel(newTestDoc(foldingSample), testOptions(), nil)
	for _, id := range []string{"editor.fold", "editor.foldAll", "editor.gotoNextFold", "editor.createFoldingRangeFromSelection", "editor.removeManualFoldingRanges"} {
		runFold(t, vm, id, nil)
	}
	if vm.Projection().RowCount() != 10 || vm.FoldedLine(0) || vm.FoldingStateOf(0) != FoldingNone {
		t.Errorf("an editor without folding folds nothing")
	}
}

func TestFoldingCommandArguments(t *testing.T) {
	vm := foldedVM(t, foldingSample)
	// selectionLines are the lines to work on instead of the selections
	runFold(t, vm, "editor.fold", map[string]any{"selectionLines": []any{6.0}})
	if !vm.FoldedLine(6) || vm.FoldedLine(0) {
		t.Errorf("selectionLines: %v %v", vm.FoldedLine(6), vm.FoldedLine(0))
	}
	runFold(t, vm, "editor.unfold", map[string]any{"selectionLines": []any{6.0}})
	if vm.FoldedLine(6) {
		t.Errorf("unfold")
	}
	runFold(t, vm, "editor.fold", map[string]any{"selectionLines": []any{0.0}, "levels": 2.0})
	if regions := vm.FoldingModel().Regions(); !regions.Collapsed(0) || !regions.Collapsed(1) {
		t.Errorf("two levels down folds the region and its children: %v", regions)
	}
}

func TestFoldingJumps(t *testing.T) {
	vm := foldedVM(t, foldingSample)
	cursorAt(vm, 0, 4)
	runFold(t, vm, "editor.gotoNextFold", nil)
	if got := vm.Cursors().Primary().Position(); got != (buffer.Position{Line: 6}) {
		t.Errorf("next fold: %v", got)
	}
	runFold(t, vm, "editor.gotoPreviousFold", nil)
	if got := vm.Cursors().Primary().Position(); got != (buffer.Position{Line: 0}) {
		t.Errorf("previous fold: %v", got)
	}
	cursorAt(vm, 3, 1)
	runFold(t, vm, "editor.gotoParentFold", nil)
	if got := vm.Cursors().Primary().Position(); got != (buffer.Position{Line: 2}) {
		t.Errorf("parent fold: %v", got)
	}
}

func TestManualFoldingRanges(t *testing.T) {
	vm := foldedVM(t, "a\nb\nc\nd\ne")
	if vm.Projection().RowCount() != 5 {
		t.Fatal("no regions in flat text")
	}
	vm.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: 1}, Active: buffer.Position{Line: 3, Col: 1}}})
	runFold(t, vm, "editor.createFoldingRangeFromSelection", nil)
	if !vm.FoldedLine(1) || vm.Projection().RowCount() != 3 {
		t.Fatalf("the selection is folded: %d rows", vm.Projection().RowCount())
	}
	if got := vm.Cursors().Primary().Position(); got != (buffer.Position{Line: 1}) {
		t.Errorf("the cursor is at %v, at the start of the folded line", got)
	}
	runFold(t, vm, "editor.removeManualFoldingRanges", nil)
	if vm.Projection().RowCount() != 5 {
		t.Errorf("the manual range is gone: %d rows", vm.Projection().RowCount())
	}
}

func TestFoldingUpdaterFindsTheRegions(t *testing.T) {
	vm := NewViewModel(newTestDoc(foldingSample), testOptions(), nil)
	done := make(chan struct{}, 4)
	vm.SetPoster(func(f func()) { f(); done <- struct{}{} })
	stop := vm.StartFolding(folding.IndentProvider{}, 1)
	defer stop()
	<-done // the regions of the text as it is
	if r := vm.FoldingModel().Regions(); r.Len() != 3 {
		t.Fatalf("regions %v", r)
	}
	stop()
	if vm.FoldingModel() != nil || vm.foldingEnabled() {
		t.Errorf("stopped")
	}
}

func TestFoldingStopUnfolds(t *testing.T) {
	vm := foldedVM(t, foldingSample)
	runFold(t, vm, "editor.foldAll", nil)
	if vm.Projection().RowCount() >= 10 {
		t.Fatal("not folded")
	}
	vm.fold.model.Dispose()
	vm.stopFolding()
	if vm.Projection().RowCount() != 10 {
		t.Errorf("stopping the folding shows every line")
	}
	if got := len(vm.Document().DecorationsOf(foldingOwner)); got != 0 {
		t.Errorf("%d decorations left", got)
	}
}

func TestFoldingKeepsCursorsOutOfHiddenLinesAfterRegionsChange(t *testing.T) {
	vm := foldedVM(t, foldingSample)
	runFold(t, vm, "editor.foldLevel1", nil)
	// the region of func a is found again with another size while a cursor is in line 1:
	// the fold opens (VS Code's rule), it does not hide the cursor
	if _, err := vm.Document().Edit([]document.Edit{{Range: buffer.Range{Start: buffer.Position{Line: 4, Col: 0}, End: buffer.Position{Line: 4, Col: 0}}, Text: "\tw()\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	vm.DocumentChanged()
	computeFolds(t, vm)
	for _, s := range vm.Cursors().Selections() {
		if _, _, hidden := vm.HiddenSpan(s.Active.Line); hidden {
			t.Errorf("the cursor %v is in a hidden line", s.Active)
		}
	}
}
