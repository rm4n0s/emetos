package editor

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/find"
)

const findSample = "alpha beta\nbeta gamma beta\n\nBeta delta\nepsilon beta"

// findUIFake records what the view model asks of the find widget.
type findUIFake struct {
	calls                       []string
	findFocused, replaceFocused bool
}

func (f *findUIFake) ui() FindUI {
	return FindUI{
		FocusFind:    func() { f.calls = append(f.calls, "find") },
		FocusReplace: func() { f.calls = append(f.calls, "replace") },
		FocusEditor:  func() { f.calls = append(f.calls, "editor") },
		InputFocus:   func() (bool, bool) { return f.findFocused, f.replaceFocused },
	}
}

func newFindVM(t *testing.T, text string, tweak func(*Options)) (*ViewModel, *findUIFake) {
	t.Helper()
	opts := goOptions()
	if tweak != nil {
		tweak(&opts)
	}
	vm := NewViewModel(newTestDoc(text), opts, nil)
	fake := &findUIFake{}
	vm.SetFindUI(fake.ui())
	t.Cleanup(vm.DisposeFind)
	return vm, fake
}

func selectRange(vm *ViewModel, l1, c1, l2, c2 int) {
	vm.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: l1, Col: c1}, Active: buffer.Position{Line: l2, Col: c2}}})
}

func primary(vm *ViewModel) buffer.Range { return vm.Cursors().Primary().Selection.Range() }

func TestStartingAFindSeedsTheSearchString(t *testing.T) {
	cases := []struct {
		name    string
		tweak   func(*Options)
		prepare func(vm *ViewModel)
		before  string
		want    string
		focus   string
	}{
		{"the word at the cursor", nil, func(vm *ViewModel) { cursorAt(vm, 0, 7) }, "", "beta", "find"},
		{"the selection", nil, func(vm *ViewModel) { selectRange(vm, 0, 2, 0, 8) }, "", "pha be", "find"},
		{"a selection of several lines seeds nothing", nil, func(vm *ViewModel) { selectRange(vm, 0, 2, 1, 2) }, "", "", "find"},
		{"never", func(o *Options) { o.FindSeedSearchStringFromSelection = "never" }, func(vm *ViewModel) { cursorAt(vm, 0, 7) }, "", "", "find"},
		{"selection only: the word at the cursor is not taken", func(o *Options) { o.FindSeedSearchStringFromSelection = "selection" },
			func(vm *ViewModel) { cursorAt(vm, 0, 7) }, "", "", "find"},
		{"selection only: the selection is", func(o *Options) { o.FindSeedSearchStringFromSelection = "selection" },
			func(vm *ViewModel) { selectRange(vm, 0, 6, 0, 10) }, "", "beta", "find"},
		{"an empty line has no word", nil, func(vm *ViewModel) { cursorAt(vm, 2, 0) }, "old", "old", "find"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm, fake := newFindVM(t, findSample, tc.tweak)
			if tc.before != "" {
				vm.FindState().Change(find.NewState{SearchString: find.Some(tc.before)}, false, false)
			}
			tc.prepare(vm)
			if err := vm.RunCommand("actions.find", nil); err != nil {
				t.Fatal(err)
			}
			st := vm.FindState()
			if !st.IsRevealed() || !vm.FindRevealed() {
				t.Error("the widget is not open")
			}
			if st.SearchString() != tc.want {
				t.Errorf("search string %q, want %q", st.SearchString(), tc.want)
			}
			if !slices.Equal(fake.calls, []string{tc.focus}) {
				t.Errorf("focus calls %v, want %v", fake.calls, tc.focus)
			}
			if st.IsReplaceRevealed() {
				t.Error("Find opens the find row only")
			}
		})
	}
}

func TestSeedingEscapesTheStringOfARegularExpressionSearch(t *testing.T) {
	vm, _ := newFindVM(t, "a.b(c)\n", nil)
	vm.FindState().Change(find.NewState{IsRegex: find.Some(true)}, false, false)
	selectRange(vm, 0, 0, 0, 6)
	if err := vm.RunCommand("actions.find", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.FindState().SearchString(); got != `a\.b\(c\)` {
		t.Errorf("search string %q", got)
	}
	if n := vm.FindState().MatchesCount(); n != 1 {
		t.Errorf("%d matches", n)
	}
}

func TestFindWithSelectionKeepsTheFocus(t *testing.T) {
	vm, fake := newFindVM(t, findSample, nil)
	selectRange(vm, 0, 6, 1, 4) // several lines: findWithSelection takes them as they are
	if err := vm.RunCommand("actions.findWithSelection", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.FindState().SearchString(); got != "beta\nbeta" {
		t.Errorf("search string %q", got)
	}
	if len(fake.calls) != 0 {
		t.Errorf("the focus moved: %v", fake.calls)
	}
}

func TestFindWithArguments(t *testing.T) {
	cases := []struct {
		name string
		args any
		want func(t *testing.T, vm *ViewModel)
	}{
		{"all of them", map[string]any{"searchString": "Beta", "replaceString": "B", "isRegex": false, "matchWholeWord": true, "isCaseSensitive": true, "preserveCase": true},
			func(t *testing.T, vm *ViewModel) {
				st := vm.FindState()
				if st.SearchString() != "Beta" || st.ReplaceString() != "B" || !st.IsReplaceRevealed() || !st.WholeWord() || !st.MatchCase() || !st.PreserveCase() {
					t.Errorf("state %q %q replace %v", st.SearchString(), st.ReplaceString(), st.IsReplaceRevealed())
				}
				if st.MatchesCount() != 1 {
					t.Errorf("%d matches, want the one with the capital", st.MatchesCount())
				}
			}},
		{"in selection", map[string]any{"searchString": "beta", "findInSelection": true},
			func(t *testing.T, vm *ViewModel) {
				// a scope of several lines is made of whole lines
				if len(vm.FindState().SearchScope()) != 1 || vm.FindState().MatchesCount() != 3 {
					t.Errorf("scope %v, %d matches", vm.FindState().SearchScope(), vm.FindState().MatchesCount())
				}
			}},
		{"no arguments", nil, func(t *testing.T, vm *ViewModel) {
			if !vm.FindState().IsRevealed() || vm.FindState().SearchString() != "" {
				t.Errorf("state %q", vm.FindState().SearchString())
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm, _ := newFindVM(t, findSample, nil)
			selectRange(vm, 0, 0, 1, 10) // not the word at a cursor: nothing seeds
			if err := vm.RunCommand("editor.actions.findWithArgs", tc.args); err != nil {
				t.Fatal(err)
			}
			tc.want(t, vm)
		})
	}
}

func TestMovingBetweenMatches(t *testing.T) {
	vm, _ := newFindVM(t, findSample, nil)
	cursorAt(vm, 0, 7)
	run := func(id string) {
		t.Helper()
		if err := vm.RunCommand(id, nil); err != nil {
			t.Fatal(err)
		}
	}
	// F3 with the widget closed opens it with the word at the cursor, and goes to the next match
	run("editor.action.nextMatchFindAction")
	if !vm.FindRevealed() || vm.FindState().SearchString() != "beta" {
		t.Fatalf("revealed %v, search %q", vm.FindRevealed(), vm.FindState().SearchString())
	}
	if got := primary(vm); got != rng(1, 0, 1, 4) {
		t.Errorf("the next match from the word at the cursor is %v", got)
	}
	wantNext := []buffer.Range{rng(1, 11, 1, 15), rng(3, 0, 3, 4), rng(4, 8, 4, 12), rng(0, 6, 0, 10)} // and round to the first
	for _, want := range wantNext {
		run("editor.action.nextMatchFindAction")
		if got := primary(vm); got != want {
			t.Errorf("next match %v, want %v", got, want)
		}
	}
	run("editor.action.previousMatchFindAction")
	if got := primary(vm); got != rng(4, 8, 4, 12) {
		t.Errorf("previous match %v", got)
	}
	if st := vm.FindState(); st.MatchesCount() != 5 || st.MatchesPosition() != 5 {
		t.Errorf("position %d of %d", st.MatchesPosition(), st.MatchesCount())
	}
}

func TestMovingBetweenMatchesStopsWithoutLoop(t *testing.T) {
	vm, _ := newFindVM(t, findSample, func(o *Options) { o.FindLoop = false })
	cursorAt(vm, 4, 9)
	if err := vm.RunCommand("actions.find", nil); err != nil {
		t.Fatal(err)
	}
	selectRange(vm, 4, 8, 4, 12) // the last match
	last := primary(vm)
	if err := vm.RunCommand("editor.action.nextMatchFindAction", nil); err != nil {
		t.Fatal(err)
	}
	if got := primary(vm); got != last {
		t.Errorf("a search that does not loop moved from %v to %v", last, got)
	}
}

func TestNextSelectionMatchSearchesForTheSelection(t *testing.T) {
	vm, _ := newFindVM(t, findSample, nil)
	selectRange(vm, 1, 5, 1, 10) // gamma
	vm.FindState().Change(find.NewState{SearchString: find.Some("beta")}, false, false)
	if err := vm.RunCommand("editor.action.nextSelectionMatchFindAction", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.FindState().SearchString(); got != "gamma" {
		t.Errorf("search string %q", got)
	}
	if got := primary(vm); got != rng(1, 5, 1, 10) {
		t.Errorf("the only match of gamma is the one that was selected: %v", got)
	}
	cursorAt(vm, 0, 7)
	if err := vm.RunCommand("editor.action.previousSelectionMatchFindAction", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.FindState().SearchString(); got != "beta" {
		t.Errorf("search string %q", got)
	}
	if got := primary(vm); got != rng(4, 8, 4, 12) {
		t.Errorf("previous match %v", got)
	}
}

func TestClosingTheWidgetDropsTheMatchesAndTheScope(t *testing.T) {
	vm, fake := newFindVM(t, findSample, nil)
	selectRange(vm, 0, 0, 1, 10)
	if err := vm.RunCommand("editor.actions.findWithArgs", map[string]any{"searchString": "beta", "findInSelection": true}); err != nil {
		t.Fatal(err)
	}
	if len(vm.Document().DecorationsOf(vm.owner(findOwner))) == 0 {
		t.Fatal("no decorations while the widget is open")
	}
	fake.calls = nil
	if err := vm.RunCommand("closeFindWidget", nil); err != nil {
		t.Fatal(err)
	}
	if vm.FindRevealed() || len(vm.FindState().SearchScope()) != 0 {
		t.Errorf("revealed %v, scope %v", vm.FindRevealed(), vm.FindState().SearchScope())
	}
	if got := vm.Document().DecorationsOf(vm.owner(findOwner)); len(got) != 0 {
		t.Errorf("%d decorations after closing", len(got))
	}
	if !slices.Equal(fake.calls, []string{"editor"}) {
		t.Errorf("focus calls %v: closing gives the focus to the editor", fake.calls)
	}
}

func TestDisposingTheFindRemovesItsDecorations(t *testing.T) {
	vm, _ := newFindVM(t, findSample, nil)
	cursorAt(vm, 0, 7)
	if err := vm.RunCommand("actions.find", nil); err != nil {
		t.Fatal(err)
	}
	vm.DisposeFind()
	if got := vm.Document().DecorationsOf(vm.owner(findOwner)); len(got) != 0 {
		t.Errorf("%d decorations after disposing", len(got))
	}
	if vm.FindRevealed() {
		t.Error("still open")
	}
}

func TestTogglesChangeTheSearch(t *testing.T) {
	cases := []struct {
		command string
		check   func(st *find.State) bool
		matches int // after the toggle, for "beta" (5 whole words, one with a capital)
	}{
		{"toggleFindCaseSensitive", func(st *find.State) bool { return st.MatchCase() }, 4},
		{"toggleFindWholeWord", func(st *find.State) bool { return st.WholeWord() }, 5},
		{"toggleFindRegex", func(st *find.State) bool { return st.IsRegex() }, 5},
		{"togglePreserveCase", func(st *find.State) bool { return st.PreserveCase() }, 5},
	}
	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			vm, _ := newFindVM(t, findSample, nil)
			cursorAt(vm, 0, 7)
			if err := vm.RunCommand("actions.find", nil); err != nil {
				t.Fatal(err)
			}
			st := vm.FindState()
			if tc.check(st) {
				t.Fatal("on before the toggle")
			}
			if err := vm.RunCommand(tc.command, nil); err != nil {
				t.Fatal(err)
			}
			if !tc.check(st) {
				t.Error("not on after the toggle")
			}
			if st.MatchesCount() != tc.matches {
				t.Errorf("%d matches, want %d", st.MatchesCount(), tc.matches)
			}
			if err := vm.RunCommand(tc.command, nil); err != nil {
				t.Fatal(err)
			}
			if tc.check(st) {
				t.Error("on after the second toggle")
			}
		})
	}
}

func TestFindInSelectionToggle(t *testing.T) {
	vm, _ := newFindVM(t, findSample, nil)
	cursorAt(vm, 0, 7)
	if err := vm.RunCommand("actions.find", nil); err != nil {
		t.Fatal(err)
	}
	st := vm.FindState()
	if err := vm.RunCommand("toggleFindInSelection", nil); err != nil {
		t.Fatal(err)
	}
	if len(st.SearchScope()) != 0 {
		t.Errorf("an empty selection limits nothing: %v", st.SearchScope())
	}
	selectRange(vm, 1, 0, 3, 0) // ends at the start of line 3: the scope stops at the end of line 2
	if err := vm.RunCommand("toggleFindInSelection", nil); err != nil {
		t.Fatal(err)
	}
	if got := st.SearchScope(); len(got) != 1 || got[0] != rng(1, 0, 2, 0) {
		t.Errorf("scope %v", got)
	}
	if st.MatchesCount() != 2 {
		t.Errorf("%d matches in the scope", st.MatchesCount())
	}
	if err := vm.RunCommand("toggleFindInSelection", nil); err != nil {
		t.Fatal(err)
	}
	if len(st.SearchScope()) != 0 || st.MatchesCount() != 5 {
		t.Errorf("scope %v, %d matches", st.SearchScope(), st.MatchesCount())
	}
}

func TestAutoFindInSelection(t *testing.T) {
	cases := []struct {
		setting string
		sel     [4]int
		scoped  bool
	}{
		{"never", [4]int{0, 0, 1, 5}, false},
		{"always", [4]int{0, 0, 0, 5}, true},
		{"multiline", [4]int{0, 0, 1, 5}, true},
		{"multiline", [4]int{0, 0, 0, 5}, false},
	}
	for _, tc := range cases {
		t.Run(tc.setting, func(t *testing.T) {
			vm, _ := newFindVM(t, findSample, func(o *Options) { o.FindAutoInSelection = tc.setting })
			selectRange(vm, tc.sel[0], tc.sel[1], tc.sel[2], tc.sel[3])
			if err := vm.RunCommand("actions.find", nil); err != nil {
				t.Fatal(err)
			}
			if got := len(vm.FindState().SearchScope()) > 0; got != tc.scoped {
				t.Errorf("scoped %v, want %v", got, tc.scoped)
			}
		})
	}
}

func TestReplaceCommands(t *testing.T) {
	setup := func(t *testing.T) *ViewModel {
		vm, _ := newFindVM(t, findSample, nil)
		if err := vm.RunCommand("editor.actions.findWithArgs", map[string]any{"searchString": "beta", "replaceString": "B-$&", "isRegex": true, "isCaseSensitive": true}); err != nil {
			t.Fatal(err)
		}
		return vm
	}
	t.Run("one", func(t *testing.T) {
		vm := setup(t)
		cursorAt(vm, 0, 0)
		for range 2 { // the first selects the match, the second replaces it
			if err := vm.RunCommand("editor.action.replaceOne", nil); err != nil {
				t.Fatal(err)
			}
		}
		if got := vm.Document().Text(); got != "alpha B-beta\nbeta gamma beta\n\nBeta delta\nepsilon beta" {
			t.Errorf("text %q", got)
		}
	})
	t.Run("all", func(t *testing.T) {
		vm := setup(t)
		if err := vm.RunCommand("editor.action.replaceAll", nil); err != nil {
			t.Fatal(err)
		}
		if got := vm.Document().Text(); got != "alpha B-beta\nB-beta gamma B-beta\n\nBeta delta\nepsilon B-beta" {
			t.Errorf("text %q", got)
		}
		// the search is for beta, which the replacement holds: it is found again
		if vm.FindState().MatchesCount() != 4 {
			t.Errorf("%d matches after replacing them all", vm.FindState().MatchesCount())
		}
		if _, err := vm.Document().Undo(); err != nil {
			t.Fatal(err)
		}
		if got := vm.Document().Text(); got != findSample {
			t.Errorf("one undo step undoes all: %q", got)
		}
	})
	t.Run("a closed widget replaces nothing", func(t *testing.T) {
		vm, _ := newFindVM(t, findSample, nil)
		for _, id := range []string{"editor.action.replaceOne", "editor.action.replaceAll", "editor.action.selectAllMatches"} {
			if err := vm.RunCommand(id, nil); err != nil {
				t.Errorf("%s: %v", id, err)
			}
		}
		if vm.Document().Text() != findSample {
			t.Error("the text changed")
		}
	})
	t.Run("select all", func(t *testing.T) {
		vm, fake := newFindVM(t, findSample, nil)
		cursorAt(vm, 0, 7)
		if err := vm.RunCommand("actions.find", nil); err != nil {
			t.Fatal(err)
		}
		fake.calls = nil
		if err := vm.RunCommand("editor.action.selectAllMatches", nil); err != nil {
			t.Fatal(err)
		}
		if got := len(vm.Cursors().Selections()); got != 5 {
			t.Errorf("%d selections", got)
		}
		if !slices.Equal(fake.calls, []string{"editor"}) {
			t.Errorf("focus calls %v: the editor gets the focus", fake.calls)
		}
	})
}

func TestReplacingInAReadOnlyDocumentFails(t *testing.T) {
	vm, _ := newFindVM(t, findSample, nil)
	vm.Document().SetReadOnly(true)
	if err := vm.RunCommand("editor.actions.findWithArgs", map[string]any{"searchString": "beta", "replaceString": "x"}); err != nil {
		t.Fatal(err)
	}
	if err := vm.RunCommand("editor.action.replaceAll", nil); err == nil {
		t.Error("replace all changed a read-only document")
	}
	if vm.Document().Text() != findSample {
		t.Error("the text changed")
	}
	// Replace (Ctrl+H) does not even open on a read-only document
	vm2, _ := newFindVM(t, findSample, nil)
	vm2.Document().SetReadOnly(true)
	if err := vm2.RunCommand("editor.action.startFindReplaceAction", nil); err != nil || vm2.FindRevealed() {
		t.Errorf("err %v, revealed %v", err, vm2.FindRevealed())
	}
}

func TestReplaceStartsOnTheRightInput(t *testing.T) {
	cases := []struct {
		name          string
		prepare       func(vm *ViewModel, fake *findUIFake)
		wantSearch    string
		wantFocus     string
		replaceShowed bool
	}{
		{"an empty selection: the find input", func(vm *ViewModel, _ *findUIFake) { cursorAt(vm, 0, 7) }, "", "find", true},
		{"a selection of a line: the replace input", func(vm *ViewModel, _ *findUIFake) { selectRange(vm, 0, 6, 0, 10) }, "beta", "replace", true},
		{"several lines are not taken", func(vm *ViewModel, _ *findUIFake) { selectRange(vm, 0, 6, 1, 4) }, "", "find", true},
		{"from the find input: the replace input", func(vm *ViewModel, fake *findUIFake) {
			cursorAt(vm, 0, 7)
			fake.findFocused = true
		}, "", "replace", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm, fake := newFindVM(t, findSample, nil)
			tc.prepare(vm, fake)
			if err := vm.RunCommand("editor.action.startFindReplaceAction", nil); err != nil {
				t.Fatal(err)
			}
			if got := vm.FindState().SearchString(); got != tc.wantSearch {
				t.Errorf("search string %q, want %q", got, tc.wantSearch)
			}
			if !slices.Equal(fake.calls, []string{tc.wantFocus}) || vm.FindState().IsReplaceRevealed() != tc.replaceShowed {
				t.Errorf("focus calls %v, replace row %v", fake.calls, vm.FindState().IsReplaceRevealed())
			}
		})
	}
}

func TestCloseOnResult(t *testing.T) {
	for _, closeOn := range []bool{false, true} {
		vm, fake := newFindVM(t, findSample, func(o *Options) { o.FindCloseOnResult = closeOn })
		cursorAt(vm, 0, 7)
		if err := vm.RunCommand("actions.find", nil); err != nil {
			t.Fatal(err)
		}
		fake.findFocused = true // Enter in the find input
		if err := vm.RunCommand("editor.action.nextMatchFindAction", nil); err != nil {
			t.Fatal(err)
		}
		if vm.FindRevealed() == closeOn {
			t.Errorf("closeOnResult %v: revealed %v", closeOn, vm.FindRevealed())
		}
	}
}

func TestTheSwitchesAreSharedByTheEditorsOfAWindow(t *testing.T) {
	memory := &FindMemory{}
	a, _ := newFindVM(t, findSample, nil)
	a.SetFindMemory(memory)
	a.FindState().Change(find.NewState{MatchCase: find.Some(true), IsRegex: find.Some(true)}, false, false)
	if !memory.MatchCase || !memory.Regex || memory.WholeWord {
		t.Errorf("memory %+v", *memory)
	}
	b, _ := newFindVM(t, findSample, nil)
	b.SetFindMemory(memory)
	if st := b.FindState(); !st.MatchCase() || !st.IsRegex() || st.WholeWord() {
		t.Errorf("the second editor starts with the switches of the first: case %v regex %v", st.MatchCase(), st.IsRegex())
	}
}

func TestParseFindArguments(t *testing.T) {
	if _, given := parseFindArguments(nil); given {
		t.Error("nil is no arguments")
	}
	if _, given := parseFindArguments("text"); given {
		t.Error("a string is no arguments")
	}
	a, given := parseFindArguments(map[string]any{"searchString": 3.0, "isRegex": "yes", "findInSelection": true})
	if !given || a.searchString != nil || a.isRegex != nil || a.inSelection == nil || !*a.inSelection {
		t.Errorf("arguments %+v", a)
	}
}

func TestOverviewRulerMarks(t *testing.T) {
	vm, _ := newFindVM(t, linesText(100, "match"), nil)
	vm.SetViewportHeight(400)
	vm.SetTextMetrics(600, 400, 100*19-400+400, 0, 8) // 100 lines of 19 px; the content scrolls through them and a page more
	if marks := vm.rulerMarks(400); len(marks) != 0 {
		t.Fatalf("%d marks while the widget is closed", len(marks))
	}
	if err := vm.RunCommand("editor.actions.findWithArgs", map[string]any{"searchString": "match"}); err != nil {
		t.Fatal(err)
	}
	if err := vm.RunCommand("editor.action.nextMatchFindAction", nil); err != nil { // the current match has the lines that are highlighted
		t.Fatal(err)
	}
	marks := vm.rulerMarks(400)
	centers, full := 0, 0
	for _, m := range marks {
		if m.lane == laneCenter {
			centers++
		} else {
			full++
		}
		if m.y < 0 || m.y+m.height > 400+rulerMinMarkHeight || m.height < rulerMinMarkHeight {
			t.Errorf("mark %+v is outside the ruler", m)
		}
	}
	// one match in every line; they are about 3 px apart on the ruler, and each has its own mark
	if centers != 100 || full != 1 {
		t.Errorf("%d marks of matches and %d of the range highlight", centers, full)
	}
	if marks[0].y > marks[len(marks)-1].y+400 {
		t.Error("marks out of order")
	}
}

// linesText is n lines that each have the word.
func linesText(n int, word string) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "line %d has a %s in it\n", i, word)
	}
	return b.String()
}
