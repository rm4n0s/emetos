package editor

import (
	"slices"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/scm/quickdiff"
)

// Quick diff marks (SPEC §10.30 "Quick diff", step 24.11): the places where the text differs from the original that a source control says
// it is compared with, shown as a stripe in the gutter, a mark on the overview ruler and a mark in the gutter of the minimap, and the
// commands that move from one to the next. The marks are decorations of the document, so they follow its edits until the model of the
// quick diff gives the editor new ones (the editor knows nothing of how they were found).
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/quickDiffDecorator.ts (the decorations, their classes and the places they
// are shown in), media/dirtydiffDecorator.css (the stripe, the pattern and the triangle of the deleted lines), and quickDiffWidget.ts
// (GotoNextChangeAction and GotoPreviousChangeAction)

// quickDiffOwner is the slot of the decorations of the quick diff marks (see ViewModel.owner).
const quickDiffOwner = 14

// QuickDiffOptions are the settings scm.diffDecorations*.
type QuickDiffOptions struct {
	// Decorations says where the marks are shown: "all", "gutter", "overview", "minimap" or "none" (scm.diffDecorations).
	Decorations string
	// GutterWidth is the width of the stripe in pixels, 1 to 5 (scm.diffDecorationsGutterWidth; anything else is 3).
	GutterWidth int
	// Visibility is "always", or "hover" to show the gutter marks only while the pointer is over the gutter
	// (scm.diffDecorationsGutterVisibility).
	Visibility string
	// PatternAdded and PatternModified draw the stripe of added and of modified lines as a hatch instead of a fill
	// (scm.diffDecorationsGutterPattern).
	PatternAdded, PatternModified bool
	// GutterAction is "diff" to open the peek of a change with a click on its stripe, or "none" (scm.diffDecorationsGutterAction).
	GutterAction string
}

func (o QuickDiffOptions) gutter() bool { return o.Decorations == "all" || o.Decorations == "gutter" }
func (o QuickDiffOptions) overview() bool {
	return o.Decorations == "all" || o.Decorations == "overview"
}
func (o QuickDiffOptions) minimap() bool { return o.Decorations == "all" || o.Decorations == "minimap" }

// width is the stripe width: VS Code takes 3 for a value that is not 1 to 5.
func (o QuickDiffOptions) width() int {
	if o.GutterWidth <= 0 || o.GutterWidth > 5 {
		return 3
	}
	return o.GutterWidth
}

// QuickDiffMark is a change the editor marks.
type QuickDiffMark struct {
	// Type is added, modified or deleted lines.
	Type quickdiff.ChangeType
	// Start and End are the lines of the change in the text, counted from 1 (Change.ModifiedStart and ModifiedEnd): a deleted change has
	// End 0 and Start the line the lines were deleted after (0 for the start of the text).
	Start, End int
	// Secondary draws the mark in the secondary colors (the change of a secondary provider).
	Secondary bool
}

// quickDiffState is the quick diff marks of a view model.
type quickDiffState struct {
	ids     []int
	entries map[int]QuickDiffMark
	// generation counts the changes of the marks; cachedGen and cachedVersion say for which the placed marks were worked out.
	generation, cachedGen, cachedVersion int
	cached                               []placedMark
	ruler                                rulerCache
	// peeker is the peek of a change the window made for the editor, nil for none (SetQuickDiffPeeker).
	peeker QuickDiffPeeker
}

// placedMark is a mark on the lines it is on now (counted from 0).
type placedMark struct {
	QuickDiffMark
	first, last int
}

// SetQuickDiffMarks replaces the quick diff marks of the editor. A mark whose lines the document does not have is not kept.
func (vm *ViewModel) SetQuickDiffMarks(marks []QuickDiffMark) {
	st := &vm.qd
	snap := vm.doc.Snapshot()
	count := snap.LineCount()
	add := make([]document.NewDecoration, 0, len(marks))
	kept := make([]QuickDiffMark, 0, len(marks))
	for _, m := range marks {
		first, last := max(m.Start-1, 0), m.End-1
		if m.Type == quickdiff.Delete {
			last = first
		}
		if first >= count || last < first || last >= count {
			continue
		}
		text, err := snap.LineContent(last)
		if err != nil {
			continue
		}
		add = append(add, document.NewDecoration{
			Range:   buffer.Range{Start: buffer.Position{Line: first}, End: buffer.Position{Line: last, Col: len(text)}},
			Options: document.DecorationOptions{Stickiness: document.NeverGrowsWhenTypingAtEdges, ClassName: "quickDiff"},
		})
		kept = append(kept, m)
	}
	if len(add) == 0 && len(st.ids) == 0 {
		return
	}
	st.ids = vm.doc.DeltaDecorations(vm.owner(quickDiffOwner), st.ids, add)
	st.entries = make(map[int]QuickDiffMark, len(kept))
	for i, id := range st.ids {
		st.entries[id] = kept[i]
	}
	st.generation++
	vm.NotifyListeners()
}

// DisposeQuickDiff takes the quick diff marks off the document.
func (vm *ViewModel) DisposeQuickDiff() {
	vm.doc.RemoveDecorationsOf(vm.owner(quickDiffOwner))
	vm.qd = quickDiffState{generation: vm.qd.generation + 1}
}

// QuickDiffCount is the number of marks the editor holds (the context key quickDiffDecorationCount).
func (vm *ViewModel) QuickDiffCount() int { return len(vm.qd.ids) }

// placedMarks are the marks on the lines they are on now, in the order of the lines; they are worked out again when the text or the
// marks changed.
func (vm *ViewModel) placedMarks() []placedMark {
	st := &vm.qd
	if len(st.ids) == 0 {
		return nil
	}
	version := vm.doc.Version()
	if st.cached != nil && st.cachedGen == st.generation && st.cachedVersion == version {
		return st.cached
	}
	out := make([]placedMark, 0, len(st.ids))
	for _, d := range vm.doc.DecorationsOf(vm.owner(quickDiffOwner)) {
		if m, ok := st.entries[d.ID]; ok {
			out = append(out, placedMark{QuickDiffMark: m, first: d.Range.Start.Line, last: d.Range.End.Line})
		}
	}
	slices.SortStableFunc(out, func(a, b placedMark) int {
		if a.first != b.first {
			return a.first - b.first
		}
		return a.last - b.last
	})
	st.cached, st.cachedGen, st.cachedVersion = out, st.generation, version
	return out
}

// QuickDiffMarks are the marks on the lines they are on now, in the order of the lines.
func (vm *ViewModel) QuickDiffMarks() []QuickDiffMark {
	placed := vm.placedMarks()
	out := make([]QuickDiffMark, len(placed))
	for i, p := range placed {
		m := p.QuickDiffMark
		m.Start = p.first + 1
		m.End = p.last + 1
		if m.Type == quickdiff.Delete {
			m.End = 0
		}
		out[i] = m
	}
	return out
}

// QuickDiffGoTo puts the cursor on the first line of the next (or previous) change after (before) the cursor's line, wrapping round at the
// ends, and scrolls it into the middle of the editor. It does nothing and returns false when the editor has no marks.
func (vm *ViewModel) QuickDiffGoTo(next bool) bool {
	marks := vm.QuickDiffMarks()
	if len(marks) == 0 {
		return false
	}
	changes := make([]quickdiff.Change, len(marks))
	for i, m := range marks {
		changes[i] = quickdiff.Change{ModifiedStart: m.Start, ModifiedEnd: m.End}
	}
	line := vm.cursors.Primary().Position().Line + 1
	var at int
	if next {
		at = quickdiff.FindNext(changes, line, false)
	} else {
		at = quickdiff.FindPrevious(changes, line, false)
	}
	vm.GoToPosition(buffer.Position{Line: max(changes[at].ModifiedStart-1, 0)})
	return true
}

// QuickDiffPeeker is the peek of a change under its lines (SPEC §10.30 "The peek", step 24.12): the window makes it for an editor of a file
// (internal/gui/quickdiffpeek) and the editor runs its commands and its clicks on the stripe, so the editor knows nothing of how it looks.
// Its methods are called on the UI thread.
type QuickDiffPeeker interface {
	// Step opens the peek at the next (or previous) change after (before) the cursor, or moves an open peek to the next (previous) one.
	Step(next bool)
	// Click is a click on the stripe of the change at the line, counted from 1: it opens the peek on the change, or closes it when it
	// shows that change already.
	Click(line int)
	// Close closes the peek.
	Close()
	// ToggleFocus moves the keyboard into the peek, or back to the editor.
	ToggleFocus()
	// Visible says that the peek is open (the context key dirtyDiffVisible).
	Visible() bool
}

// SetQuickDiffPeeker gives the editor the peek of its changes; nil takes it away.
func (vm *ViewModel) SetQuickDiffPeeker(p QuickDiffPeeker) { vm.qd.peeker = p }

// QuickDiffPeekVisible tells whether the peek of a change is open (the context key dirtyDiffVisible).
func (vm *ViewModel) QuickDiffPeekVisible() bool {
	return vm.qd.peeker != nil && vm.qd.peeker.Visible()
}

// QuickDiffPeekChanged tells the editor that the peek opened or closed, so that the context keys follow.
func (vm *ViewModel) QuickDiffPeekChanged() { vm.NotifyListeners() }

// quickDiffStripeAt finds the change whose stripe is at the point of the gutter, in the coordinates of the gutter: the line of the change
// (from 0) and true when the editor opens a peek from a click, the point is on a stripe, and a mark covers the line of the pointer. The
// stripe is the width of the setting and VS Code lets a click 3 px to either side of it count.
func (vm *ViewModel) quickDiffStripeAt(x float64, line int, widths GutterWidths) (int, bool) {
	opts := vm.Options()
	qd := opts.QuickDiff
	if vm.qd.peeker == nil || qd.GutterAction != "diff" || !qd.gutter() || len(vm.qd.ids) == 0 {
		return 0, false
	}
	left := widths.GlyphMargin + widths.LineNumbers
	if opts.Folding {
		left += foldingControlsWidth
	}
	start := left + quickDiffStripeLeft
	if x < start-quickDiffStripeSlack || x > start+float64(qd.width())+quickDiffStripeSlack {
		return 0, false
	}
	for _, p := range vm.shownMarks() {
		if line >= p.first && line <= p.last {
			return line, true
		}
	}
	return 0, false
}

// quickDiffStripeSlack is how far from the stripe a click still counts as one on it (VS Code's 3 px).
const quickDiffStripeSlack = 3.0

// quickDiffActions adds the commands of the quick diff to the table of the editor's commands: Go to Next Change and Go to Previous Change
// (VS Code's workbench.action.editor.nextChange and previousChange, Alt+F5 and Shift+Alt+F5), and the commands of the peek of a change
// (editor.action.dirtydiff.next and previous, closeQuickDiff and toggleQuickDiffWidgetFocus), which the peek of the window runs.
func quickDiffActions(a map[string]action) {
	peek := func(run func(p QuickDiffPeeker)) action {
		return func(vm *ViewModel, _ any) error {
			if vm.qd.peeker != nil {
				run(vm.qd.peeker)
			}
			return nil
		}
	}
	a["editor.action.dirtydiff.next"] = peek(func(p QuickDiffPeeker) { p.Step(true) })
	a["editor.action.dirtydiff.previous"] = peek(func(p QuickDiffPeeker) { p.Step(false) })
	a["closeQuickDiff"] = peek(func(p QuickDiffPeeker) { p.Close() })
	a["toggleQuickDiffWidgetFocus"] = peek(func(p QuickDiffPeeker) { p.ToggleFocus() })
	a["workbench.action.editor.nextChange"] = func(vm *ViewModel, _ any) error {
		vm.QuickDiffGoTo(true)
		return nil
	}
	a["workbench.action.editor.previousChange"] = func(vm *ViewModel, _ any) error {
		vm.QuickDiffGoTo(false)
		return nil
	}
}

// PlaceCursor puts the only cursor at a position without scrolling, for a peek that scrolls itself.
func (vm *ViewModel) PlaceCursor(p buffer.Position) {
	p = vm.doc.Snapshot().ValidatePosition(p)
	vm.cursors.SetSelections([]buffer.Selection{buffer.NewCursor(p)})
	vm.SelectionsChanged()
	vm.NotifyListeners()
}

// ContextKeysForTest gives the context keys the window and the diff editor gave the editor, as they would be set in its scope.
func (vm *ViewModel) ContextKeysForTest() map[string]any {
	out := map[string]any{}
	for _, keys := range vm.contextKeys {
		for k, v := range keys() {
			out[k] = v
		}
	}
	return out
}
