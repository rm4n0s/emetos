package editor

import (
	"context"
	"strings"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/edit"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
)

// Ported from VS Code: src/vs/editor/contrib/wordHighlighter/browser/wordHighlighter.ts and
// src/vs/editor/contrib/multicursor/browser/multicursor.ts (SelectionHighlighter)

// The slots of the decorations of the highlights (1 is auto-closing, 2 folding, 3 find, 6 snippets;
// see ViewModel.owner).
const (
	wordHighlightOwner      = 4
	selectionHighlightOwner = 5
)

// The classes of the decorations of the highlights: the occurrences of a symbol have the class of their kind, the way VS Code
// names them (read is "wordHighlight", write "wordHighlightStrong", and a textual occurrence "wordHighlightText").
const (
	classWordHighlight       = "wordHighlight"
	classWordHighlightStrong = "wordHighlightStrong"
	classWordHighlightText   = "wordHighlightText"
	classSelectionHighlight  = "selectionHighlight"
)

// selectionHighlightDelay is how long the selection highlight waits after an edit.
const selectionHighlightDelay = 300 * time.Millisecond

// highlightLink holds the highlights of the occurrences of the word at the cursor and of the text
// that equals the selection: the decorations that mark them and the timers that make them.
type highlightLink struct {
	wordIDs, selectionIDs []int
	// peekIDs are the marks of the references in the editor of a peek view.
	peekIDs []int
	// askGen numbers the requests of the highlights of the providers, and cancelAsk ends the one in flight.
	askGen    int
	cancelAsk context.CancelFunc
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
	vm.cancelHighlightRequest()
	vm.setHighlights(wordHighlightOwner, &h.wordIDs, nil)
	vm.setHighlights(selectionHighlightOwner, &h.selectionIDs, nil)
	vm.setHighlights(peekMatchOwner, &h.peekIDs, nil)
}

// highlightsEnabled tells whether highlights are computed at all: not in large file mode.
func (vm *ViewModel) highlightsEnabled() bool { return !vm.doc.LargeFile() }

// setHighlights replaces the decorations of an owner by marks of the ranges.
func (vm *ViewModel) setHighlights(owner int, ids *[]int, ranges []buffer.Range) {
	class := classWordHighlightText
	switch owner {
	case selectionHighlightOwner:
		class = classSelectionHighlight
	case peekMatchOwner:
		class = classPeekMatch
	}
	items := make([]highlightItem, len(ranges))
	for i, r := range ranges {
		items[i] = highlightItem{rng: r, class: class}
	}
	vm.setHighlightItems(owner, ids, items)
}

// highlightItem is a range to mark and the class of its decoration.
type highlightItem struct {
	rng   buffer.Range
	class string
}

// setHighlightItems replaces the decorations of an owner by marks of the items.
func (vm *ViewModel) setHighlightItems(owner int, ids *[]int, items []highlightItem) {
	if len(*ids) == 0 && len(items) == 0 {
		return
	}
	add := make([]document.NewDecoration, len(items))
	for i, it := range items {
		add[i] = document.NewDecoration{Range: it.rng, Options: document.DecorationOptions{
			Stickiness: document.NeverGrowsWhenTypingAtEdges, ClassName: it.class,
		}}
	}
	*ids = vm.doc.DeltaDecorations(vm.owner(owner), *ids, add)
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
	vm.cancelHighlightRequest()
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
	for _, d := range vm.doc.DecorationsOf(vm.owner(wordHighlightOwner)) {
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
		if f := vm.hoverFeatures(); f != nil && f.Has(vm.langDocument(), langfeatures.FeatureDocumentHighlights) {
			// a provider decides; the word is marked when it has nothing to say
			vm.askHighlights(primary.Active, func() { vm.textualHighlights(query, word) })
			return
		}
		vm.textualHighlights(query, word)
	}
	delay := time.Duration(max(vm.opts.OccurrencesHighlightDelay, 0)) * time.Millisecond
	if delay == 0 || vm.afterTimer == nil {
		run()
		return
	}
	h.cancelWord = vm.afterTimer(delay, run)
}

// textualHighlights marks the occurrences of the word at the cursor by searching the text.
func (vm *ViewModel) textualHighlights(query edit.OccurrenceQuery, word buffer.Range) {
	ranges := vm.commands().Occurrences(query)
	// the word at the cursor is one of them, whatever the search says
	vm.setHighlights(wordHighlightOwner, &vm.hl.wordIDs, withRange(ranges, word))
}

// cancelHighlightRequest ends the request for the highlights of the providers that is in flight; its answer is dropped.
func (vm *ViewModel) cancelHighlightRequest() {
	h := &vm.hl
	h.askGen++
	if h.cancelAsk != nil {
		h.cancelAsk()
		h.cancelAsk = nil
	}
}

// askHighlights asks the document highlight providers for the occurrences of the symbol at a position, off the UI thread, and
// marks them by kind (read, write, text). When they answer nothing, or fail, fallback runs. An answer for a document that
// changed or for a request that was replaced is dropped.
//
// Ported from VS Code: wordHighlighter.ts (WordHighlighter._run, computeOccurencesAtPosition) and
// src/vs/editor/contrib/wordHighlighter/browser/highlightDecorations.ts
func (vm *ViewModel) askHighlights(pos buffer.Position, fallback func()) {
	h := &vm.hl
	vm.cancelHighlightRequest()
	f := vm.hoverFeatures()
	doc := vm.langDocument()
	ctx := context.Background()
	if host := vm.sg.host; host != nil && host.Ctx != nil {
		ctx = host.Ctx
	}
	ctx, h.cancelAsk = context.WithCancel(ctx)
	gen := h.askGen
	go func() {
		hls, errs := f.DocumentHighlights(ctx, doc, pos)
		vm.post(func() {
			if vm.hl.askGen != gen || vm.doc.Version() != doc.Version {
				return
			}
			vm.hl.cancelAsk = nil
			if host := vm.sg.host; host != nil && host.Log != nil {
				for _, e := range errs {
					if !errors.Is(e.Err, context.Canceled) {
						host.Log("highlight provider "+e.Owner+" failed", e.Err)
					}
				}
			}
			if len(hls) == 0 {
				fallback()
				return
			}
			snap := vm.doc.Snapshot()
			items := make([]highlightItem, len(hls))
			for i, hl := range hls {
				class := classWordHighlightText
				switch hl.Kind {
				case langfeatures.HighlightRead:
					class = classWordHighlight
				case langfeatures.HighlightWrite:
					class = classWordHighlightStrong
				}
				items[i] = highlightItem{rng: snap.ValidateRange(hl.Range), class: class}
			}
			vm.setHighlightItems(wordHighlightOwner, &vm.hl.wordIDs, items)
		})
	}()
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

// occurrencesIn are the decorations of an owner slot in the lines of the rows first to last.
func (vm *ViewModel) occurrencesIn(owner, firstRow, lastRow int) []document.Decoration {
	var ids []int
	switch owner {
	case wordHighlightOwner:
		ids = vm.hl.wordIDs
	case peekMatchOwner:
		ids = vm.hl.peekIDs
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
	return vm.doc.DecorationsIn(span, vm.owner(owner))
}
