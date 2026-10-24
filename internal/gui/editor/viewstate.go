package editor

import (
	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// Ported from VS Code: src/vs/editor/browser/widget/codeEditor/codeEditorWidget.ts (saveViewState,
// restoreViewState, revealRangeInCenter) and
// src/vs/editor/contrib/quickAccess/browser/editorNavigationQuickAccess.ts (the line decoration)

// RestoreView is RestoreViewState for the editor that is on screen: it also scrolls the editor to the
// saved position now (RestoreViewState leaves that to the next layout). Go to line puts the editor back
// with it when the user gives up.
func (vm *ViewModel) RestoreView(s ViewState) error {
	err := vm.RestoreViewState(s)
	if vm.scroller != nil {
		vm.scroller(max(s.ScrollTop, 0))
	}
	vm.SelectionsChanged()
	return err
}

// RevealPositionInCenter scrolls so that the row of a position is in the middle of the editor.
func (vm *ViewModel) RevealPositionInCenter(p buffer.Position) {
	if vm.scroller == nil || vm.viewport <= 0 {
		return
	}
	proj := vm.Projection()
	row := vm.RowOfPosition(p)
	vm.scroller(max(proj.RowTop(row)+proj.RowHeight(row)/2-vm.viewport/2, 0))
}

// GoToPosition puts a single cursor at a position, scrolls it into the middle of the editor and
// makes the range highlight go away.
func (vm *ViewModel) GoToPosition(p buffer.Position) {
	p = vm.doc.Snapshot().ValidatePosition(p)
	vm.cursors.SetSelections([]buffer.Selection{buffer.NewCursor(p)})
	vm.SelectionsChanged()
	vm.RevealPositionInCenter(p)
	vm.ClearRangeHighlight()
	vm.NotifyListeners()
}

// SetRangeHighlight shades the whole line of a position (zero-based line), as go to line does while
// the user types a line number.
func (vm *ViewModel) SetRangeHighlight(line int) {
	line = min(max(line, 0), vm.doc.Snapshot().LineCount()-1)
	if vm.rangeLine == line+1 {
		return
	}
	vm.rangeLine = line + 1
	vm.sel.NotifyListeners()
}

// ClearRangeHighlight takes the shading of SetRangeHighlight away.
func (vm *ViewModel) ClearRangeHighlight() {
	if vm.rangeLine == 0 {
		return
	}
	vm.rangeLine = 0
	vm.sel.NotifyListeners()
}

// RangeHighlight is the line SetRangeHighlight shaded, and whether there is one.
func (vm *ViewModel) RangeHighlight() (line int, ok bool) { return vm.rangeLine - 1, vm.rangeLine > 0 }

// paintRangeHighlight shades the line of the range highlight across the layer.
func (v overlayView) paintRangeHighlight() {
	line, ok := v.geo.vm.RangeHighlight()
	if !ok {
		return
	}
	if _, _, hidden := v.geo.vm.HiddenSpan(line); hidden {
		return
	}
	at := buffer.Position{Line: line}
	v.paintWholeLines(buffer.Range{Start: at, End: at}, v.colors.RangeHighlight, v.colors.RangeHighlightBorder)
}
