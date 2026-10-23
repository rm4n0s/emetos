package editor

import (
	"time"

	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/edit"
	"github.com/rm4n0s/emetos/internal/core/find"
)

// Ported from VS Code: src/vs/editor/contrib/find/browser/findController.ts (CommonFindController)

// findOwner is the owner of the document decorations of the find model (1 is the auto-closed
// pairs' and 2 the folding regions').
const findOwner = 3

// searchStringMaxLength is the longest selection that seeds the search string.
const searchStringMaxLength = 524288

// FindUI is what the view model asks of the find widget that the editor shows. Every function
// is optional.
type FindUI struct {
	// FocusFind and FocusReplace give the focus to an input of the widget, FocusEditor to the
	// editor.
	FocusFind, FocusReplace, FocusEditor func()
	// InputFocus says which input of the widget has the focus.
	InputFocus func() (findInput, replaceInput bool)
}

// FindMemory holds what the editors of a window share of their searches: the switches stay as
// the user set them when another editor opens (VS Code keeps them in the workspace storage).
type FindMemory struct {
	MatchCase, WholeWord, Regex, PreserveCase bool
}

// findLink is what a view model knows of its find widget.
type findLink struct {
	state       *find.State
	model       *find.Model
	unsubscribe func()
	ui          FindUI
	memory      *FindMemory
	// changed tells the widget and the layers that the state, the matches or the current match
	// changed.
	changed *foundation.ChangeNotifier
}

// SetFindUI gives the view model the means to move the focus into and out of the find widget.
func (vm *ViewModel) SetFindUI(ui FindUI) { vm.findw.ui = ui }

// SetFindMemory gives the view model the switches the editors of the window share; nil makes
// them its own.
func (vm *ViewModel) SetFindMemory(m *FindMemory) {
	vm.findw.memory = m
	if vm.findw.state != nil && m != nil {
		vm.loadFindMemory()
	}
}

// FindState is the state of the find widget of the editor (what is searched for and found).
func (vm *ViewModel) FindState() *find.State {
	f := &vm.findw
	if f.state == nil {
		f.state = find.NewFindState()
		f.changed = foundation.NewChangeNotifier()
		vm.loadFindMemory()
		f.unsubscribe = f.state.OnChange(vm.findStateChanged)
	}
	return f.state
}

// FindRevealed says whether the find widget is open.
func (vm *ViewModel) FindRevealed() bool { return vm.findw.state != nil && vm.findw.state.IsRevealed() }

// AddFindListener calls fn whenever the state of the find widget or its matches change.
func (vm *ViewModel) AddFindListener(fn func()) foundation.ListenerHandle {
	vm.FindState()
	return vm.findw.changed.AddListener(fn)
}

// RemoveFindListener removes a listener of AddFindListener.
func (vm *ViewModel) RemoveFindListener(h foundation.ListenerHandle) {
	if vm.findw.changed != nil {
		vm.findw.changed.RemoveListener(h)
	}
}

func (vm *ViewModel) loadFindMemory() {
	m := vm.findw.memory
	if m == nil {
		return
	}
	vm.findw.state.Change(find.NewState{
		MatchCase: find.Some(m.MatchCase), WholeWord: find.Some(m.WholeWord),
		IsRegex: find.Some(m.Regex), PreserveCase: find.Some(m.PreserveCase),
	}, false, false)
}

// findStateChanged follows the state: the switches are remembered, closing the widget drops the
// model, and the layers show the matches.
func (vm *ViewModel) findStateChanged(e find.ChangeEvent) {
	f := &vm.findw
	if m := f.memory; m != nil {
		s := f.state
		if e.MatchCase {
			m.MatchCase = s.ActualMatchCase()
		}
		if e.WholeWord {
			m.WholeWord = s.ActualWholeWord()
		}
		if e.IsRegex {
			m.Regex = s.ActualIsRegex()
		}
		if e.PreserveCase {
			m.PreserveCase = s.ActualPreserveCase()
		}
	}
	if e.IsRevealed && !f.state.IsRevealed() {
		vm.disposeFindModel()
	}
	f.changed.NotifyListeners()
	vm.sel.NotifyListeners()
}

func (vm *ViewModel) disposeFindModel() {
	if vm.findw.model != nil {
		vm.findw.model.Dispose()
		vm.findw.model = nil
	}
}

// DisposeFind drops the find model, its decorations and the state; the view model starts again
// with a closed widget when it is asked for the state.
func (vm *ViewModel) DisposeFind() {
	f := &vm.findw
	vm.disposeFindModel()
	if f.state == nil {
		return
	}
	f.unsubscribe()
	f.state.Dispose()
	f.changed.NotifyListeners()
	f.state, f.changed, f.unsubscribe = nil, nil, nil
}

// findHost is the editor as the find model sees it.
type findHost struct{ vm *ViewModel }

func (h findHost) Selection() buffer.Selection { return h.vm.cursors.Primary().Selection }

func (h findHost) SetSelection(s buffer.Selection) { h.vm.setSelections([]buffer.Selection{s}) }

func (h findHost) SetSelections(sels []buffer.Selection) { h.vm.setSelections(sels) }

func (h findHost) Reveal(r buffer.Range) { h.vm.RevealRangeInCenterIfOutsideViewport(r) }

func (h findHost) WordSeparators() string {
	if s := h.vm.opts.WordSeparators; s != "" {
		return s
	}
	return edit.DefaultWordSeparators
}

func (h findHost) CursorMoveOnType() bool { return h.vm.opts.FindCursorMoveOnType }

func (h findHost) After(d time.Duration, fn func()) func() {
	if h.vm.afterTimer == nil {
		return func() {}
	}
	return h.vm.afterTimer(d, fn)
}

// RevealRangeInCenterIfOutsideViewport scrolls the editor, if the first row of a range is not in
// view, so that the row is in the middle.
//
// Ported from VS Code: src/vs/editor/browser/widget/codeEditor/codeEditorWidget.ts (revealRangeInCenterIfOutsideViewport)
func (vm *ViewModel) RevealRangeInCenterIfOutsideViewport(r buffer.Range) {
	if vm.scroller == nil || vm.viewport <= 0 {
		return
	}
	proj := vm.Projection()
	first, last := vm.RowOfPosition(r.Start), vm.RowOfPosition(r.End)
	top, bottom := proj.RowTop(first), proj.RowTop(last)+proj.RowHeight(last)
	if top >= vm.scrollTop && bottom <= vm.scrollTop+vm.viewport {
		return
	}
	vm.scroller(max(top-(vm.viewport-proj.RowHeight(first))/2, 0))
}

// findFocus says what the find widget does with the focus when it starts.
type findFocus int

const (
	findNoFocusChange findFocus = iota
	findFocusInput
	findFocusReplaceInput
)

// findStart are the options of starting a find (VS Code's IFindStartOptions).
type findStart struct {
	forceRevealReplace bool
	// seed is "none", "single" or "multiple".
	seed                      string
	seedFromNonEmptySelection bool
	focus                     findFocus
	updateSearchScope         bool
	loop                      bool
}

// startFind opens the find widget with a state change and makes the model that marks the matches.
func (vm *ViewModel) startFind(o findStart, ns find.NewState) {
	state := vm.FindState()
	vm.disposeFindModel()

	selection := vm.cursors.Primary().Selection
	switch vm.opts.FindAutoInSelection {
	case "always":
		o.updateSearchScope = true
	case "multiline":
		o.updateSearchScope = o.updateSearchScope || selection.Start().Line != selection.End().Line
	}

	ns.IsRevealed = find.Some(true)
	// A search string that is asked for is not replaced by the selection (VS Code does).
	switch {
	case ns.SearchString.Set:
	case o.seed == "single":
		if text, ok := vm.selectionSearchString("single", o.seedFromNonEmptySelection); ok {
			if state.IsRegex() {
				text = find.EscapeRegExp(text)
			}
			ns.SearchString = find.Some(text)
		}
	case o.seed == "multiple" && !o.updateSearchScope:
		if text, ok := vm.selectionSearchString("multiple", false); ok {
			ns.SearchString = find.Some(text)
		}
	}
	switch {
	case o.forceRevealReplace || (ns.IsReplaceRevealed.Set && ns.IsReplaceRevealed.Value):
		ns.IsReplaceRevealed = find.Some(true)
	case !state.IsRevealed():
		ns.IsReplaceRevealed = find.Some(false)
	}
	if o.updateSearchScope {
		sels := vm.cursors.Selections()
		for _, s := range sels {
			if !s.IsEmpty() {
				ranges := make([]buffer.Range, len(sels))
				for i, s := range sels {
					ranges[i] = s.Range()
				}
				ns.SearchScope = find.Some(ranges)
				break
			}
		}
	}
	ns.Loop = find.Some(o.loop)
	state.Change(ns, false, true)

	if vm.findw.model == nil {
		vm.findw.model = find.NewModel(findHost{vm}, vm.doc, state, findOwner)
	}
	ui := vm.findw.ui
	switch {
	case o.focus == findFocusReplaceInput && ui.FocusReplace != nil:
		ui.FocusReplace()
	case o.focus == findFocusInput && ui.FocusFind != nil:
		ui.FocusFind()
	}
}

// selectionSearchString is the text that a find is seeded with: the selection, or the word at a
// cursor without one.
//
// Ported from VS Code: src/vs/editor/contrib/find/browser/findController.ts (getSelectionSearchString)
func (vm *ViewModel) selectionSearchString(seed string, fromNonEmptySelection bool) (string, bool) {
	sel := vm.cursors.Primary().Selection
	if seed == "single" && sel.Start().Line != sel.End().Line {
		return "", false
	}
	if sel.IsEmpty() {
		if fromNonEmptySelection {
			return "", false
		}
		line, _ := vm.doc.Snapshot().LineContent(sel.Active.Line)
		sep := vm.opts.WordSeparators
		if sep == "" {
			sep = edit.DefaultWordSeparators
		}
		w, ok := edit.NewWordClassifier(sep).WordAt(line, sel.Active.Col)
		if !ok || w.End <= w.Start {
			return "", false
		}
		return line[w.Start:w.End], true
	}
	text := vm.doc.Snapshot().Text(sel.Range())
	if len(text) >= searchStringMaxLength {
		return "", false
	}
	return text, text != ""
}

// closeFind closes the find widget and gives the focus back to the editor.
func (vm *ViewModel) closeFind() {
	if vm.findw.state == nil {
		return
	}
	vm.findw.state.Change(find.NewState{IsRevealed: find.Some(false), SearchScope: find.Some[[]buffer.Range](nil)}, false, true)
	if f := vm.findw.ui.FocusEditor; f != nil {
		f()
	}
}

// startForNavigation is the start that a find command makes when there is no model to move
// with: it seeds the search string from the selection when the widget has none.
func (vm *ViewModel) startForNavigation() {
	vm.startFind(findStart{
		seed:                      vm.seedWhenEmpty(),
		seedFromNonEmptySelection: vm.opts.FindSeedSearchStringFromSelection == "selection",
		loop:                      vm.opts.FindLoop,
	}, find.NewState{})
}

func (vm *ViewModel) seedWhenEmpty() string {
	if vm.FindState().SearchString() == "" && vm.opts.FindSeedSearchStringFromSelection != "never" {
		return "single"
	}
	return "none"
}

// findInputFocus tells which input of the widget has the focus.
func (vm *ViewModel) findInputFocus() (findInput, replaceInput bool) {
	if f := vm.findw.ui.InputFocus; f != nil {
		return f()
	}
	return false, false
}
