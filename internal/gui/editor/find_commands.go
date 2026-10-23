package editor

import (
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/find"
)

// Ported from VS Code: src/vs/editor/contrib/find/browser/findController.ts (the actions)

// findArguments are the arguments of editor.actions.findWithArgs; every field is optional.
type findArguments struct {
	searchString, replaceString                                         *string
	isRegex, matchWholeWord, isCaseSensitive, preserveCase, inSelection *bool
}

// parseFindArguments reads {"searchString", "replaceString", "isRegex", "matchWholeWord",
// "isCaseSensitive", "preserveCase", "findInSelection"}; anything else is no arguments.
func parseFindArguments(args any) (a findArguments, given bool) {
	m, ok := args.(map[string]any)
	if !ok {
		return a, false
	}
	str := func(key string) *string {
		if v, ok := m[key].(string); ok {
			return &v
		}
		return nil
	}
	flag := func(key string) *bool {
		if v, ok := m[key].(bool); ok {
			return &v
		}
		return nil
	}
	a.searchString, a.replaceString = str("searchString"), str("replaceString")
	a.isRegex, a.matchWholeWord, a.isCaseSensitive = flag("isRegex"), flag("matchWholeWord"), flag("isCaseSensitive")
	a.preserveCase, a.inSelection = flag("preserveCase"), flag("findInSelection")
	return a, true
}

// findActions adds the commands of the find widget to the table of the editor's commands.
func findActions(a map[string]action) {
	a["actions.find"] = func(vm *ViewModel, _ any) error {
		vm.skipReveal = true
		vm.startFind(findStart{
			seed:                      vm.seedSetting(),
			seedFromNonEmptySelection: vm.opts.FindSeedSearchStringFromSelection == "selection",
			focus:                     findFocusInput,
			loop:                      vm.opts.FindLoop,
		}, find.NewState{})
		return nil
	}
	a["editor.actions.findWithArgs"] = func(vm *ViewModel, args any) error {
		vm.skipReveal = true
		ns := find.NewState{}
		parsed, given := parseFindArguments(args)
		if given {
			optString := func(s *string) find.Opt[string] {
				if s == nil {
					return find.Opt[string]{}
				}
				return find.Some(*s)
			}
			optBool := func(b *bool) find.Opt[bool] {
				if b == nil {
					return find.Opt[bool]{}
				}
				return find.Some(*b)
			}
			ns.SearchString, ns.ReplaceString = optString(parsed.searchString), optString(parsed.replaceString)
			ns.IsReplaceRevealed = find.Some(parsed.replaceString != nil)
			ns.IsRegex, ns.WholeWord, ns.MatchCase = optBool(parsed.isRegex), optBool(parsed.matchWholeWord), optBool(parsed.isCaseSensitive)
			ns.PreserveCase = optBool(parsed.preserveCase)
		}
		seed := "none"
		if vm.FindState().SearchString() == "" && vm.opts.FindSeedSearchStringFromSelection != "never" {
			seed = "single"
		}
		vm.startFind(findStart{
			seed:                      seed,
			seedFromNonEmptySelection: vm.opts.FindSeedSearchStringFromSelection == "selection",
			focus:                     findFocusInput,
			updateSearchScope:         given && parsed.inSelection != nil && *parsed.inSelection,
			loop:                      vm.opts.FindLoop,
		}, ns)
		return nil
	}
	a["actions.findWithSelection"] = func(vm *ViewModel, _ any) error {
		vm.skipReveal = true
		vm.startFind(findStart{seed: "multiple", loop: vm.opts.FindLoop}, find.NewState{})
		return nil
	}
	a["editor.action.startFindReplaceAction"] = func(vm *ViewModel, _ any) error {
		if vm.doc.ReadOnly() {
			return nil
		}
		vm.skipReveal = true
		sel := vm.cursors.Primary().Selection
		findInputFocused, _ := vm.findInputFocus()
		// The search string is seeded from a selection of one line that is not empty, unless the
		// find input has the focus.
		seed := !sel.IsEmpty() && sel.Start().Line == sel.End().Line &&
			vm.opts.FindSeedSearchStringFromSelection != "never" && !findInputFocused
		focus := findFocusInput
		if findInputFocused || seed {
			focus = findFocusReplaceInput
		}
		o := findStart{
			forceRevealReplace:        true,
			seedFromNonEmptySelection: vm.opts.FindSeedSearchStringFromSelection == "selection",
			focus:                     focus,
			loop:                      vm.opts.FindLoop,
			seed:                      "none",
		}
		if seed {
			o.seed = "single"
		}
		vm.startFind(o, find.NewState{})
		return nil
	}

	a["editor.action.nextMatchFindAction"] = func(vm *ViewModel, _ any) error { vm.matchFind(true); return nil }
	a["editor.action.previousMatchFindAction"] = func(vm *ViewModel, _ any) error { vm.matchFind(false); return nil }
	a["editor.action.nextSelectionMatchFindAction"] = func(vm *ViewModel, _ any) error { vm.selectionMatchFind(true); return nil }
	a["editor.action.previousSelectionMatchFindAction"] = func(vm *ViewModel, _ any) error { vm.selectionMatchFind(false); return nil }

	a["closeFindWidget"] = func(vm *ViewModel, _ any) error {
		vm.skipReveal = true
		vm.closeFind()
		return nil
	}
	toggle := func(id string, change func(s *find.State) find.NewState) {
		a[id] = func(vm *ViewModel, _ any) error {
			vm.skipReveal = true
			s := vm.FindState()
			s.Change(change(s), false, true)
			return nil
		}
	}
	toggle("toggleFindCaseSensitive", func(s *find.State) find.NewState { return find.NewState{MatchCase: find.Some(!s.MatchCase())} })
	toggle("toggleFindWholeWord", func(s *find.State) find.NewState { return find.NewState{WholeWord: find.Some(!s.WholeWord())} })
	toggle("toggleFindRegex", func(s *find.State) find.NewState { return find.NewState{IsRegex: find.Some(!s.IsRegex())} })
	toggle("togglePreserveCase", func(s *find.State) find.NewState { return find.NewState{PreserveCase: find.Some(!s.PreserveCase())} })
	a["toggleFindInSelection"] = func(vm *ViewModel, _ any) error {
		vm.skipReveal = true
		vm.toggleSearchScope()
		return nil
	}

	a["editor.action.replaceOne"] = func(vm *ViewModel, _ any) error {
		if vm.findw.model == nil {
			return nil
		}
		return vm.findw.model.Replace()
	}
	a["editor.action.replaceAll"] = func(vm *ViewModel, _ any) error {
		if vm.findw.model == nil {
			return nil
		}
		return vm.findw.model.ReplaceAll()
	}
	a["editor.action.selectAllMatches"] = func(vm *ViewModel, _ any) error {
		if vm.findw.model == nil {
			return nil
		}
		vm.findw.model.SelectAllMatches()
		if f := vm.findw.ui.FocusEditor; f != nil {
			f()
		}
		return nil
	}
}

// seedSetting is the seeding that editor.find.seedSearchStringFromSelection asks for.
func (vm *ViewModel) seedSetting() string {
	if vm.opts.FindSeedSearchStringFromSelection != "never" {
		return "single"
	}
	return "none"
}

// matchFind moves to the next or previous match, opening the widget first when there is none.
func (vm *ViewModel) matchFind(next bool) {
	wasVisible := vm.FindRevealed()
	run := func() bool {
		if vm.findw.model == nil {
			return false
		}
		before := vm.cursors.Primary().Selection
		if next {
			vm.findw.model.MoveToNextMatch()
		} else {
			vm.findw.model.MoveToPrevMatch()
		}
		if vm.cursors.Primary().Selection.Equal(before) {
			return false
		}
		if findInput, _ := vm.findInputFocus(); vm.opts.FindCloseOnResult && wasVisible && findInput {
			vm.closeFind()
		}
		return true
	}
	if !run() {
		vm.startForNavigation()
		run()
	}
}

// selectionMatchFind searches for the selection (or the word at the cursor) as it is, and moves
// to its next or previous match.
func (vm *ViewModel) selectionMatchFind(next bool) {
	state := vm.FindState()
	if text, ok := vm.selectionSearchString("single", false); ok {
		if state.IsRegex() {
			text = find.EscapeRegExp(text)
		}
		state.Change(find.NewState{SearchString: find.Some(text)}, false, true)
	}
	run := func() bool {
		if vm.findw.model == nil {
			return false
		}
		if next {
			vm.findw.model.MoveToNextMatch()
		} else {
			vm.findw.model.MoveToPrevMatch()
		}
		return true
	}
	if !run() {
		vm.startFind(findStart{seed: "none", loop: vm.opts.FindLoop}, find.NewState{})
		run()
	}
}

// toggleSearchScope limits the search to the selections, or lifts the limit.
//
// Ported from VS Code: src/vs/editor/contrib/find/browser/findController.ts (toggleSearchScope)
func (vm *ViewModel) toggleSearchScope() {
	state := vm.FindState()
	if len(state.SearchScope()) > 0 {
		state.Change(find.NewState{SearchScope: find.Some[[]buffer.Range](nil)}, true, true)
		return
	}
	snap := vm.doc.Snapshot()
	var scope []buffer.Range
	for _, s := range vm.cursors.Selections() {
		r := s.Range()
		if r.End.Col == 0 && r.End.Line > r.Start.Line {
			line, _ := snap.LineContent(r.End.Line - 1)
			r.End = buffer.Position{Line: r.End.Line - 1, Col: len(line)}
		}
		if !r.IsEmpty() {
			scope = append(scope, r)
		}
	}
	if len(scope) > 0 {
		state.Change(find.NewState{SearchScope: find.Some(scope)}, true, true)
	}
}
