package editor

import (
	"context"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
)

// The rows of the suggest widget and its commands (SPEC §9.12 "Completion").
//
// Ported from VS Code: src/vs/editor/contrib/suggest/browser/suggestWidget.ts and suggestController.ts (the commands)

// suggestRows is the number of rows the widget shows at once, VS Code's 12 (its default height).
const suggestRows = 12

// suggestCommandIDs are the commands of the widget, which do not end the run of it.
var suggestCommandIDs = []string{
	triggerSuggestCommand, "acceptSelectedSuggestion", "acceptAlternativeSelectedSuggestion", "hideSuggestWidget",
	"selectNextSuggestion", "selectPrevSuggestion", "selectNextPageSuggestion", "selectPrevPageSuggestion",
	"selectFirstSuggestion", "selectLastSuggestion", "focusSuggestion", "focusAndAcceptSuggestion",
	"toggleSuggestionDetails", "toggleSuggestionFocus", "dev.triggerSuggest", "dev.toggleSuggestDetails",
}

func isSuggestCommand(id string) bool {
	for _, c := range suggestCommandIDs {
		if c == id {
			return true
		}
	}
	return false
}

// SuggestDetails is what the details pane shows for the focused item.
type SuggestDetails struct {
	// Detail is the text above the documentation, Documentation the documentation (nil for none).
	Detail        string
	Documentation *langfeatures.Markup
	// Loading says that the details are being asked for.
	Loading bool
}

// SuggestView is what the suggest widget draws.
type SuggestView struct {
	State SuggestState
	// Items are the rows and Focus the focused one (-1 for none); Top is the first row in view.
	Items      []*langfeatures.SuggestItem
	Focus, Top int
	// Anchor is the place of the text the widget hangs from: the start of the word that is being completed.
	Anchor buffer.Position
	// Details is the pane next to the list, or nil when it is closed or the focused item has nothing to show.
	Details *SuggestDetails
	// Options are the settings of the widget.
	Options SuggestOptions
}

// SuggestView is what the suggest widget shows now, or nil when it is hidden.
func (vm *ViewModel) SuggestView() *SuggestView {
	s := vm.sg.session
	if s == nil || s.state == SuggestHidden {
		return nil
	}
	v := &SuggestView{State: s.state, Items: s.items, Focus: s.focus, Top: s.top, Anchor: buffer.Position{Line: s.req.line, Col: s.req.wordStart}, Options: vm.opts.Suggest}
	if s.state == SuggestOpen || s.state == SuggestFrozen {
		if it := s.focused(); it != nil && vm.sg.host.Shared.DetailsVisible && canExpand(it) {
			v.Details = &SuggestDetails{Detail: it.Detail, Documentation: it.Documentation, Loading: s.resolving == it && !it.Resolved}
		}
	}
	return v
}

// canExpand tells whether an item has details to show beyond its row: a documentation, or a detail that is not its label.
func canExpand(it *langfeatures.SuggestItem) bool {
	return (it.Documentation != nil && strings.TrimSpace(it.Documentation.Value) != "") || (it.Detail != "" && it.Detail != it.Label)
}

// CanExpandSuggestion is canExpand for the widget.
func CanExpandSuggestion(it *langfeatures.SuggestItem) bool { return canExpand(it) }

func (s *suggestSession) focused() *langfeatures.SuggestItem {
	if s.focus < 0 || s.focus >= len(s.items) || (s.state != SuggestOpen && s.state != SuggestFrozen) {
		return nil
	}
	return s.items[s.focus]
}

// SuggestVisible tells whether the widget shows (the key suggestWidgetVisible).
func (vm *ViewModel) SuggestVisible() bool {
	s := vm.sg.session
	return s != nil && s.state != SuggestHidden
}

// SuggestKeys are the context keys of the widget.
type SuggestKeys struct {
	Visible, HasFocused, DetailsVisible, Multiple, MakesTextEdit, HasInsertAndReplace, CanResolve bool
	InsertMode                                                                                    string
	AcceptOnEnter                                                                                 bool
}

// SuggestKeys are the context keys of the suggest widget now.
func (vm *ViewModel) SuggestKeys() SuggestKeys {
	k := SuggestKeys{InsertMode: vm.opts.Suggest.InsertMode, MakesTextEdit: true}
	k.AcceptOnEnter = vm.opts.Suggest.AcceptSuggestionOnEnter == "on" || vm.opts.Suggest.AcceptSuggestionOnEnter == "smart"
	s := vm.sg.session
	if s == nil || s.state == SuggestHidden {
		return k
	}
	k.Visible = true
	k.Multiple = len(s.items) > 1
	k.DetailsVisible = vm.sg.host.Shared.DetailsVisible
	if it := s.focused(); it != nil {
		k.HasFocused = true
		k.MakesTextEdit = vm.makesTextEdit(it)
		k.HasInsertAndReplace = it.InsertEnd != it.ReplaceEnd
		k.CanResolve = vm.sg.host.Features != nil && vm.sg.host.Features.CanResolveCompletion(it.CompletionItem) || it.Documentation != nil || it.Detail != it.Label
	}
	return k
}

// makesTextEdit tells whether accepting the item changes the text: with editor.acceptSuggestionOnEnter "smart" an item that is
// already typed is not accepted by Enter (the Enter key makes a new line instead).
func (vm *ViewModel) makesTextEdit(it *langfeatures.SuggestItem) bool {
	s := vm.sg.session
	if vm.opts.Suggest.AcceptSuggestionOnEnter != "smart" || s == nil || !s.trigger.auto || it.AdditionalEdits != nil || it.Snippet {
		return true
	}
	pos := vm.cursors.Primary().Position()
	text := it.EffectiveInsertText()
	if pos.Col-it.EditStart != len(text) {
		return true
	}
	line, _ := vm.doc.Snapshot().LineContent(pos.Line)
	if it.EditStart < 0 || pos.Col > len(line) {
		return true
	}
	return line[it.EditStart:pos.Col] != text
}

// suggestFocus focuses a row: it scrolls into view, notes the commit characters, and asks for the details of the item.
func (vm *ViewModel) suggestFocus(i int) {
	s := vm.sg.session
	if s == nil || i < 0 || i >= len(s.items) {
		return
	}
	s.focus = i
	rows := vm.sg.rows
	if rows <= 0 {
		rows = suggestRows
	}
	switch {
	case i < s.top:
		s.top = i
	case i >= s.top+rows:
		s.top = i - rows + 1
	}
	it := s.items[i]
	s.commit = nil
	if len(it.CommitCharacters) > 0 {
		s.commit = it
	}
	vm.startResolve(s, it)
	vm.suggestChanged()
}

// cancelResolveOf ends the request for the details of the item that was being resolved.
func (vm *ViewModel) cancelResolveOf(s *suggestSession) {
	if s.cancelResolve != nil {
		s.cancelResolve()
		s.cancelResolve = nil
	}
	s.resolving = nil
}

// startResolve asks the provider of the item for its details, if it can give them and they were not asked for yet. Only the
// focused row is resolved: moving to another row ends the request.
func (vm *ViewModel) startResolve(s *suggestSession, it *langfeatures.SuggestItem) {
	vm.cancelResolveOf(s)
	h := vm.sg.host
	if it.Resolved || h.Features == nil || !h.Features.CanResolveCompletion(it.CompletionItem) {
		return
	}
	base := h.Ctx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	s.cancelResolve, s.resolving = cancel, it
	doc := vm.langDocument()
	item := it.CompletionItem
	go func() {
		merged, err := h.Features.ResolveCompletion(ctx, doc, item)
		vm.post(func() {
			if ctx.Err() != nil || vm.sg.session != s || s.resolving != it {
				return
			}
			s.resolving, s.cancelResolve = nil, nil
			cancel()
			it.Resolved = true
			if err != nil {
				vm.reportSuggest(err)
				return
			}
			it.CompletionItem = merged
			vm.suggestChanged()
		})
	}()
}

// selectRow moves the focus by delta rows, wrapping around the ends.
func (vm *ViewModel) selectRow(delta int) {
	s := vm.sg.session
	if s == nil || (s.state != SuggestOpen && s.state != SuggestFrozen) || len(s.items) == 0 {
		return
	}
	n := len(s.items)
	i := s.focus
	switch {
	case i < 0 && delta > 0:
		i = 0
	case i < 0:
		i = n - 1
	default:
		i = ((i+delta)%n + n) % n
	}
	vm.suggestFocus(i)
}

// selectPage moves the focus by a page of rows without wrapping.
func (vm *ViewModel) selectPage(dir int) {
	s := vm.sg.session
	if s == nil || (s.state != SuggestOpen && s.state != SuggestFrozen) || len(s.items) == 0 {
		return
	}
	rows := vm.sg.rows
	if rows <= 0 {
		rows = suggestRows
	}
	i := s.focus
	if i < 0 {
		i = 0
	} else {
		i = min(max(i+dir*(rows-1), 0), len(s.items)-1)
	}
	vm.suggestFocus(i)
}

// SetSuggestRows tells the view model how many rows the widget shows at once, which the page keys and the scrolling use.
func (vm *ViewModel) SetSuggestRows(n int) { vm.sg.rows = n }

// SuggestScroll scrolls the list by rows without moving the focus.
func (vm *ViewModel) SuggestScroll(rows int) {
	s := vm.sg.session
	if s == nil {
		return
	}
	n := vm.sg.rows
	if n <= 0 {
		n = suggestRows
	}
	top := min(max(s.top+rows, 0), max(len(s.items)-n, 0))
	if top != s.top {
		s.top = top
		vm.suggestChanged()
	}
}

// SuggestAccept accepts the row i (a click on it).
func (vm *ViewModel) SuggestAccept(i int) {
	s := vm.sg.session
	if s == nil || i < 0 || i >= len(s.items) {
		return
	}
	vm.suggestInsert(s.items[i], insertOptions{})
	vm.finishSuggestCommand()
}

// SuggestToggleDetails opens or closes the details pane (the "read more" icon of the focused row).
func (vm *ViewModel) SuggestToggleDetails() {
	vm.toggleSuggestDetails()
}

func (vm *ViewModel) toggleSuggestDetails() {
	s := vm.sg.session
	if s == nil {
		return
	}
	sh := vm.sg.host.Shared
	switch {
	case sh.DetailsVisible:
		sh.DetailsVisible = false
	case s.focused() != nil && canExpand(s.focused()):
		sh.DetailsVisible = true
	}
	vm.suggestChanged()
}

// finishSuggestCommand tells the view about the cursors after an acceptance that did not come from a command.
func (vm *ViewModel) finishSuggestCommand() {
	vm.SelectionsChanged()
	vm.RevealPrimary()
}

// suggestActions are the commands of the suggest widget.
func suggestActions(a map[string]action) {
	a[triggerSuggestCommand] = func(vm *ViewModel, args any) error {
		vm.keepSessions = true
		auto := false
		if m, ok := args.(map[string]any); ok {
			auto, _ = m["auto"].(bool)
		}
		vm.suggestTrigger(triggerOptions{auto: auto})
		return nil
	}
	accept := func(alternative bool) action {
		return func(vm *ViewModel, _ any) error {
			vm.keepSessions = true
			if s := vm.sg.session; s != nil {
				if it := s.focused(); it != nil {
					vm.suggestInsert(it, insertOptions{alternative: alternative})
				}
			}
			return nil
		}
	}
	a["acceptSelectedSuggestion"] = accept(false)
	a["acceptAlternativeSelectedSuggestion"] = accept(true)
	a["hideSuggestWidget"] = func(vm *ViewModel, _ any) error {
		vm.keepSessions, vm.skipReveal = true, true
		vm.suggestCancel()
		return nil
	}
	nav := func(f func(vm *ViewModel)) action {
		return func(vm *ViewModel, _ any) error {
			vm.keepSessions, vm.skipReveal = true, true
			f(vm)
			return nil
		}
	}
	a["selectNextSuggestion"] = nav(func(vm *ViewModel) { vm.selectRow(1) })
	a["selectPrevSuggestion"] = nav(func(vm *ViewModel) { vm.selectRow(-1) })
	a["selectNextPageSuggestion"] = nav(func(vm *ViewModel) { vm.selectPage(1) })
	a["selectPrevPageSuggestion"] = nav(func(vm *ViewModel) { vm.selectPage(-1) })
	a["selectFirstSuggestion"] = nav(func(vm *ViewModel) {
		if s := vm.sg.session; s != nil && len(s.items) > 0 {
			vm.suggestFocus(0)
		}
	})
	a["selectLastSuggestion"] = nav(func(vm *ViewModel) {
		if s := vm.sg.session; s != nil && len(s.items) > 0 {
			vm.suggestFocus(len(s.items) - 1)
		}
	})
	a["focusSuggestion"] = nav(func(vm *ViewModel) {
		if s := vm.sg.session; s != nil && s.focus < 0 && len(s.items) > 0 {
			vm.suggestFocus(0)
		}
	})
	a["focusAndAcceptSuggestion"] = func(vm *ViewModel, _ any) error {
		vm.keepSessions = true
		if s := vm.sg.session; s != nil && len(s.items) > 0 {
			if s.focus < 0 {
				vm.suggestFocus(0)
			}
			if it := s.focused(); it != nil {
				vm.suggestInsert(it, insertOptions{})
			}
		}
		return nil
	}
	a["toggleSuggestionDetails"] = nav(func(vm *ViewModel) { vm.toggleSuggestDetails() })
	a["toggleSuggestionFocus"] = a["toggleSuggestionDetails"]
}
