package editor

import (
	"strings"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/edit"
)

// Ported from VS Code: src/vs/editor/contrib/wordHighlighter/browser/wordHighlighter.ts and
// src/vs/editor/contrib/multicursor/browser/multicursor.ts (SelectionHighlighter)

// The owners of the decorations of the highlights (1 is auto-closing, 2 folding, 3 find).
const (
	wordHighlightOwner      = 4
	selectionHighlightOwner = 5
)

// The classes of the decorations of the highlights.
const (
	classWordHighlight      = "wordHighlight"
	classSelectionHighlight = "selectionHighlight"
)

// selectionHighlightDelay is how long the selection highlight waits after an edit.
const selectionHighlightDelay = 300 * time.Millisecond

// highlightLink holds the highlights of the occurrences of the word at the cursor and of the text
// that equals the selection: the decorations that mark them and the timers that make them.
type highlightLink struct {
	wordIDs, selectionIDs []int
	// seenVersion is the version of the document at the last move of the cursors; a move over a
	// newer version is the result of typing.
	seenVersion     int
	cancelWord      func()
	cancelSelection func()
	// unsubscribe stops following the document; nil before the first move of the cursors.
	unsubscribe func()
}

// DisposeHighlights stops the highlights and takes their decorations away.
func (vm *ViewModel) DisposeHighlights() {
	h := &vm.hl
	if h.unsubscribe != nil {
		h.unsubscribe()
		h.unsubscribe = nil
	}
	if h.cancelWord != nil {
		h.cancelWord()
		h.cancelWord = nil
	}
	if h.cancelSelection != nil {
		h.cancelSelection()
		h.cancelSelection = nil
	}
	vm.setHighlights(wordHighlightOwner, &h.wordIDs, nil)
	vm.setHighlights(selectionHighlightOwner, &h.selectionIDs, nil)
}

// highlightsEnabled tells whether highlights are computed at all: not in large file mode.
func (vm *ViewModel) highlightsEnabled() bool { return !vm.doc.LargeFile() }

// setHighlights replaces the decorations of an owner by marks of the ranges.
func (vm *ViewModel) setHighlights(owner int, ids *[]int, ranges []buffer.Range) {
	if len(*ids) == 0 && len(ranges) == 0 {
		return
	}
	class := classWordHighlight
	if owner == selectionHighlightOwner {
		class = classSelectionHighlight
	}
	add := make([]document.NewDecoration, len(ranges))
	for i, r := range ranges {
		add[i] = document.NewDecoration{Range: r, Options: document.DecorationOptions{
			Stickiness: document.NeverGrowsWhenTypingAtEdges, ClassName: class,
		}}
	}
	*ids = vm.doc.DeltaDecorations(owner, *ids, add)
	vm.sel.NotifyListeners()
}

// highlightsCursorsChanged follows the cursors: the word highlight follows the word at the cursor
// and goes away when the cursor moved by typing; the selection highlight follows the selection.
func (vm *ViewModel) highlightsCursorsChanged() {
	h := &vm.hl
	if h.unsubscribe == nil {
		h.unsubscribe = vm.doc.Subscribe(func(document.ChangeEvent) { vm.highlightsDocumentChanged() })
	}
	typed := vm.doc.Version() != h.seenVersion
	h.seenVersion = vm.doc.Version()
	vm.updateWordHighlight(typed)
	vm.updateSelectionHighlight()
}

// highlightsOptionsChanged makes the highlights again for the settings that changed.
func (vm *ViewModel) highlightsOptionsChanged() {
	if vm.hl.unsubscribe == nil {
		return // the cursors have not moved: nothing to follow yet
	}
	vm.updateWordHighlight(false)
	vm.updateSelectionHighlight()
}

// highlightsDocumentChanged follows an edit: the word highlight stops and the selection highlight
// is made again soon.
func (vm *ViewModel) highlightsDocumentChanged() {
	h := &vm.hl
	if len(h.wordIDs) > 0 || h.cancelWord != nil {
		vm.stopWordHighlight()
	}
	if vm.opts.SelectionHighlight && vm.highlightsEnabled() {
		vm.scheduleSelectionHighlight(selectionHighlightDelay)
	}
}

func (vm *ViewModel) stopWordHighlight() {
	h := &vm.hl
	if h.cancelWord != nil {
		h.cancelWord()
		h.cancelWord = nil
	}
	vm.setHighlights(wordHighlightOwner, &h.wordIDs, nil)
}

// updateWordHighlight looks for the word at the cursor. When the cursor is in a word that is
// marked already nothing changes; otherwise the old marks go and the new ones come after
// editor.occurrencesHighlightDelay.
//
// Ported from VS Code: wordHighlighter.ts (WordHighlighter._run, _onPositionChanged)
func (vm *ViewModel) updateWordHighlight(typed bool) {
	h := &vm.hl
	if vm.opts.OccurrencesHighlight == "" || vm.opts.OccurrencesHighlight == "off" || typed || !vm.highlightsEnabled() {
		vm.stopWordHighlight()
		return
	}
	primary := vm.cursors.Primary().Selection
	query, word, ok := vm.commands().WordQuery(primary)
	if !ok {
		vm.stopWordHighlight()
		return
	}
	for _, d := range vm.doc.DecorationsOf(wordHighlightOwner) {
		if d.Range.ContainsPosition(primary.Active) && d.Range.ContainsPosition(primary.Anchor) {
			return
		}
	}
	vm.stopWordHighlight()
	version := vm.doc.Version()
	run := func() {
		h.cancelWord = nil
		if vm.doc.Version() != version {
			return
		}
		ranges := vm.commands().Occurrences(query)
		// the word at the cursor is one of them, whatever the search says
		vm.setHighlights(wordHighlightOwner, &h.wordIDs, withRange(ranges, word))
	}
	delay := time.Duration(max(vm.opts.OccurrencesHighlightDelay, 0)) * time.Millisecond
	if delay == 0 || vm.afterTimer == nil {
		run()
		return
	}
	h.cancelWord = vm.afterTimer(delay, run)
}

// withRange is the ranges with r among them.
func withRange(ranges []buffer.Range, r buffer.Range) []buffer.Range {
	for _, x := range ranges {
		if x == r {
			return ranges
		}
	}
	return append(ranges, r)
}

// scheduleSelectionHighlight makes the selection highlight after a delay, or now with no timer.
func (vm *ViewModel) scheduleSelectionHighlight(delay time.Duration) {
	h := &vm.hl
	if h.cancelSelection != nil {
		h.cancelSelection()
		h.cancelSelection = nil
	}
	if vm.afterTimer == nil || delay <= 0 {
		vm.updateSelectionHighlight()
		return
	}
	h.cancelSelection = vm.afterTimer(delay, func() {
		h.cancelSelection = nil
		vm.updateSelectionHighlight()
	})
}

// updateSelectionHighlight marks the text that equals the selection, apart from the selection.
// The find widget's own matches are not marked twice: nothing is when the find widget looks for
// the same text with the same switches.
//
// Ported from VS Code: multicursor.ts (SelectionHighlighter._createState, _setState)
func (vm *ViewModel) updateSelectionHighlight() {
	h := &vm.hl
	if h.cancelSelection != nil {
		h.cancelSelection()
		h.cancelSelection = nil
	}
	if !vm.opts.SelectionHighlight || !vm.highlightsEnabled() {
		vm.setHighlights(selectionHighlightOwner, &h.selectionIDs, nil)
		return
	}
	sels := vm.cursors.Selections()
	c := vm.commands()
	query, ok := c.SelectionQuery(sels, vm.find, edit.SelectionHighlightOptions{
		MaxLength: vm.opts.SelectionHighlightMaxLength, Multiline: vm.opts.SelectionHighlightMultiline,
	})
	if ok && vm.findSearchesFor(query) {
		ok = false
	}
	if !ok {
		vm.setHighlights(selectionHighlightOwner, &h.selectionIDs, nil)
		return
	}
	vm.setHighlights(selectionHighlightOwner, &h.selectionIDs, c.OccurrencesApartFrom(query, sels))
}

// findSearchesFor tells whether the find widget is open and looks for the text of the query with
// the query's switches, without a regular expression.
func (vm *ViewModel) findSearchesFor(q edit.OccurrenceQuery) bool {
	st := vm.findw.state
	if st == nil || !st.IsRevealed() || st.IsRegex() {
		return false
	}
	return st.SearchString() == q.Text && st.MatchCase() == q.MatchCase && st.WholeWord() == q.WholeWord ||
		!q.MatchCase && !st.MatchCase() && strings.EqualFold(st.SearchString(), q.Text) && st.WholeWord() == q.WholeWord
}

// occurrencesIn are the decorations of an owner in the lines of the rows first to last.
func (vm *ViewModel) occurrencesIn(owner, firstRow, lastRow int) []document.Decoration {
	var ids []int
	switch owner {
	case wordHighlightOwner:
		ids = vm.hl.wordIDs
	default:
		ids = vm.hl.selectionIDs
	}
	if len(ids) == 0 {
		return nil
	}
	proj := vm.Projection()
	firstLine, _, _ := proj.RowSpan(firstRow)
	lastLine, _, _ := proj.RowSpan(lastRow)
	span := buffer.Range{Start: buffer.Position{Line: firstLine}, End: buffer.Position{Line: lastLine + 1}}
	return vm.doc.DecorationsIn(span, owner)
}
