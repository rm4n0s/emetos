package editor

import (
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
)

func cur(line, col int) buffer.Selection { return buffer.NewCursor(positionOf(line, col)) }

// run makes a view model over a text with selections and runs one command on it.
func run(t *testing.T, text string, sels []buffer.Selection, id string, args any) *ViewModel {
	t.Helper()
	vm := NewViewModel(newTestDoc(text), testOptions(), nil)
	vm.Cursors().SetSelections(sels)
	if err := vm.RunCommand(id, args); err != nil {
		t.Fatalf("%s: %v", id, err)
	}
	return vm
}

func TestCommandsMoveCursors(t *testing.T) {
	const doc = "hello world\nfoo bar\nbaz"
	cases := []struct {
		id   string
		from buffer.Selection
		want buffer.Selection
	}{
		{"cursorLeft", cur(0, 5), cur(0, 4)},
		{"cursorLeft", cur(1, 0), cur(0, 11)},
		{"cursorRight", cur(0, 5), cur(0, 6)},
		{"cursorRight", cur(0, 11), cur(1, 0)},
		{"cursorUp", cur(1, 5), cur(0, 5)},
		{"cursorUp", cur(0, 5), cur(0, 0)},
		{"cursorDown", cur(0, 5), cur(1, 5)},
		{"cursorDown", cur(2, 1), cur(2, 3)},
		{"cursorPageUp", cur(2, 1), cur(0, 0)},
		{"cursorPageDown", cur(0, 5), cur(2, 3)},
		{"cursorHome", cur(0, 5), cur(0, 0)},
		{"cursorLineStart", cur(1, 5), cur(1, 0)},
		{"cursorEnd", cur(0, 5), cur(0, 11)},
		{"cursorLineEnd", cur(1, 2), cur(1, 7)},
		{"cursorTop", cur(2, 1), cur(0, 0)},
		{"cursorBottom", cur(0, 1), cur(2, 3)},
		{"cursorWordStartLeft", cur(0, 8), cur(0, 6)},
		{"cursorWordEndLeft", cur(0, 8), cur(0, 5)},
		{"cursorWordStartRight", cur(0, 2), cur(0, 6)},
		{"cursorWordEndRight", cur(0, 2), cur(0, 5)},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			vm := run(t, doc, []buffer.Selection{tc.from}, tc.id, nil)
			if got := vm.Cursors().Selections(); len(got) != 1 || got[0] != tc.want {
				t.Errorf("from %v: %v, want %v", tc.from, got, tc.want)
			}
		})
	}
}

func TestSmartHomeGoesToTheIndentationThenToTheStart(t *testing.T) {
	vm := run(t, "    ab", []buffer.Selection{cur(0, 6)}, "cursorHome", nil)
	if got := vm.Cursors().Primary().Position(); got != positionOf(0, 4) {
		t.Fatalf("first the first character: %v", got)
	}
	if err := vm.RunCommand("cursorHome", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.Cursors().Primary().Position(); got != positionOf(0, 0) {
		t.Errorf("then the start of the line: %v", got)
	}
}

func TestSelectingCommandsExtendTheSelection(t *testing.T) {
	const doc = "hello world foo.bar\n  camelCase snake_case\nlast line"
	moves := []string{
		"cursorLeft", "cursorRight", "cursorUp", "cursorDown", "cursorPageUp", "cursorPageDown",
		"cursorHome", "cursorLineStart", "cursorEnd", "cursorLineEnd", "cursorTop", "cursorBottom",
		"cursorWordStartLeft", "cursorWordEndLeft", "cursorWordLeft", "cursorWordAccessibilityLeft",
		"cursorWordStartRight", "cursorWordEndRight", "cursorWordRight", "cursorWordAccessibilityRight",
		"cursorWordPartLeft", "cursorWordPartRight",
	}
	starts := []buffer.Selection{cur(0, 0), cur(0, 8), cur(1, 5), cur(1, 14), cur(2, 4), cur(2, 9)}
	for _, id := range moves {
		t.Run(id, func(t *testing.T) {
			for _, start := range starts {
				plain := run(t, doc, []buffer.Selection{start}, id, nil).Cursors().Primary()
				selecting := run(t, doc, []buffer.Selection{start}, id+"Select", nil).Cursors().Primary()
				want := buffer.Selection{Anchor: start.Active, Active: plain.Position()}
				if selecting.Selection != want {
					t.Errorf("%sSelect from %v: %v, want %v", id, start.Active, selecting.Selection, want)
				}
			}
		})
	}
}

func TestWordPartCommands(t *testing.T) {
	cases := []struct {
		id   string
		from buffer.Selection
		want buffer.Selection
	}{
		{"cursorWordPartLeft", cur(0, 9), cur(0, 5)},
		{"cursorWordPartRight", cur(0, 0), cur(0, 5)},
		{"cursorWordPartLeftSelect", cur(0, 9), sel(0, 9, 0, 5)},
	}
	for _, tc := range cases {
		vm := run(t, "camelCase", []buffer.Selection{tc.from}, tc.id, nil)
		if got := vm.Cursors().Primary().Selection; got != tc.want {
			t.Errorf("%s: %v, want %v", tc.id, got, tc.want)
		}
	}
}

func sel(l1, c1, l2, c2 int) buffer.Selection {
	return buffer.Selection{Anchor: positionOf(l1, c1), Active: positionOf(l2, c2)}
}

func TestColumnSelectionGoesOnUntilSomethingElseMovesTheCursors(t *testing.T) {
	vm := run(t, "abcd\nefgh\nijkl", []buffer.Selection{cur(0, 1)}, "cursorColumnSelectDown", nil)
	if got := vm.Cursors().Selections(); len(got) != 2 || got[0] != cur(0, 1) || got[1] != cur(1, 1) {
		t.Fatalf("one cursor per line: %v", got)
	}
	for _, id := range []string{"cursorColumnSelectRight", "cursorColumnSelectDown"} {
		if err := vm.RunCommand(id, nil); err != nil {
			t.Fatal(err)
		}
	}
	want := []buffer.Selection{sel(0, 1, 0, 2), sel(1, 1, 1, 2), sel(2, 1, 2, 2)}
	got := vm.Cursors().Selections()
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("the rectangle grew: %v, want %v", got, want)
	}
	if err := vm.RunCommand("cursorColumnSelectUp", nil); err != nil {
		t.Fatal(err)
	}
	if n := vm.Cursors().Len(); n != 2 {
		t.Errorf("up shrinks the rectangle to two lines: %d", n)
	}
	// Another command ends the column selection: the next one starts from the primary cursor.
	if err := vm.RunCommand("removeSecondaryCursors", nil); err != nil {
		t.Fatal(err)
	}
	if err := vm.RunCommand("cursorColumnSelectLeft", nil); err != nil {
		t.Fatal(err)
	}
	if vm.columns == nil || vm.Cursors().Len() != 1 {
		t.Errorf("a new column selection: %d cursors", vm.Cursors().Len())
	}
	// The primary selection was (0,1)-(0,2): going left from its active end shrinks it to
	// nothing.
	if got := vm.Cursors().Primary().Selection; got != cur(0, 1) {
		t.Errorf("selection %v", got)
	}
}

func TestSelectionCommands(t *testing.T) {
	cases := []struct {
		name string
		id   string
		text string
		from []buffer.Selection
		want []buffer.Selection
	}{
		{"cancelSelection", "cancelSelection", "abcdef", []buffer.Selection{sel(0, 1, 0, 4)}, []buffer.Selection{cur(0, 4)}},
		{"cancelSelection removes the other cursors", "cancelSelection", "abcdef", []buffer.Selection{sel(0, 1, 0, 4), cur(0, 5)}, []buffer.Selection{cur(0, 4)}},
		{"removeSecondaryCursors", "removeSecondaryCursors", "abcdef", []buffer.Selection{cur(0, 1), cur(0, 3), cur(0, 5)}, []buffer.Selection{cur(0, 1)}},
		{"expandLineSelection", "expandLineSelection", "a\nbcd\ne", []buffer.Selection{cur(1, 2)}, []buffer.Selection{sel(1, 0, 2, 0)}},
		{"selectAll", "editor.action.selectAll", "ab\ncde", []buffer.Selection{cur(0, 1)}, []buffer.Selection{sel(0, 0, 1, 3)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := run(t, tc.text, tc.from, tc.id, nil)
			if got := vm.Cursors().Selections(); !buffer.SelectionsEqual(got, tc.want) {
				t.Errorf("%v, want %v", got, tc.want)
			}
		})
	}
}

func TestCursorUndoAndRedo(t *testing.T) {
	vm := run(t, "abcdef", []buffer.Selection{cur(0, 0)}, "cursorRight", nil)
	for range 2 {
		if err := vm.RunCommand("cursorRight", nil); err != nil {
			t.Fatal(err)
		}
	}
	at := func() int { return vm.Cursors().Primary().Position().Col }
	if at() != 3 {
		t.Fatalf("at %d", at())
	}
	steps := []struct {
		id   string
		want int
	}{{"cursorUndo", 2}, {"cursorUndo", 1}, {"cursorRedo", 2}, {"cursorRedo", 3}, {"cursorRedo", 3}}
	for _, s := range steps {
		if err := vm.RunCommand(s.id, nil); err != nil {
			t.Fatal(err)
		}
		if at() != s.want {
			t.Errorf("after %s at %d, want %d", s.id, at(), s.want)
		}
	}
	// Editing the text forgets the history: the old positions refer to the old text.
	if err := vm.RunCommand("type", "x"); err != nil {
		t.Fatal(err)
	}
	before := at()
	if err := vm.RunCommand("cursorUndo", nil); err != nil {
		t.Fatal(err)
	}
	if at() != before {
		t.Errorf("cursor undo after an edit moved to %d", at())
	}
}

func TestTypingAndDeleting(t *testing.T) {
	cases := []struct {
		name     string
		id       string
		args     any
		text     string
		from     []buffer.Selection
		want     string
		wantSels []buffer.Selection
	}{
		{"type", "type", map[string]any{"text": "xy"}, "abcd", []buffer.Selection{cur(0, 2)}, "abxycd", []buffer.Selection{cur(0, 4)}},
		{"type a string", "type", "q", "ab", []buffer.Selection{cur(0, 1)}, "aqb", []buffer.Selection{cur(0, 2)}},
		{"type replaces the selection", "type", "X", "abcd", []buffer.Selection{sel(0, 1, 0, 3)}, "aXd", []buffer.Selection{cur(0, 2)}},
		{"type at every cursor", "type", "-", "ab\ncd", []buffer.Selection{cur(0, 1), cur(1, 1)}, "a-b\nc-d", []buffer.Selection{cur(0, 2), cur(1, 2)}},
		{"type nothing", "type", map[string]any{}, "ab", []buffer.Selection{cur(0, 1)}, "ab", []buffer.Selection{cur(0, 1)}},
		{"type an empty text", "type", "", "ab", []buffer.Selection{cur(0, 1)}, "ab", []buffer.Selection{cur(0, 1)}},
		{"deleteLeft", "deleteLeft", nil, "abc", []buffer.Selection{cur(0, 2)}, "ac", []buffer.Selection{cur(0, 1)}},
		{"deleteRight", "deleteRight", nil, "abc", []buffer.Selection{cur(0, 1)}, "ac", []buffer.Selection{cur(0, 1)}},
		{"deleteLeft joins lines", "deleteLeft", nil, "ab\ncd", []buffer.Selection{cur(1, 0)}, "abcd", []buffer.Selection{cur(0, 2)}},
		{"deleteWordLeft", "deleteWordLeft", nil, "hello world", []buffer.Selection{cur(0, 11)}, "hello ", []buffer.Selection{cur(0, 6)}},
		{"deleteWordRight", "deleteWordRight", nil, "hello world", []buffer.Selection{cur(0, 0)}, " world", []buffer.Selection{cur(0, 0)}},
		{"deleteWordStartLeft", "deleteWordStartLeft", nil, "hello world", []buffer.Selection{cur(0, 8)}, "hello rld", []buffer.Selection{cur(0, 6)}},
		{"deleteWordEndLeft", "deleteWordEndLeft", nil, "hello world", []buffer.Selection{cur(0, 8)}, "hellorld", []buffer.Selection{cur(0, 5)}},
		{"deleteWordStartRight", "deleteWordStartRight", nil, "hello world", []buffer.Selection{cur(0, 2)}, "heworld", []buffer.Selection{cur(0, 2)}},
		{"deleteWordEndRight", "deleteWordEndRight", nil, "hello world", []buffer.Selection{cur(0, 2)}, "he world", []buffer.Selection{cur(0, 2)}},
		{"deleteWordPartLeft", "deleteWordPartLeft", nil, "camelCase", []buffer.Selection{cur(0, 9)}, "camel", []buffer.Selection{cur(0, 5)}},
		{"deleteWordPartRight", "deleteWordPartRight", nil, "camelCase", []buffer.Selection{cur(0, 0)}, "Case", []buffer.Selection{cur(0, 0)}},
		{"tab", "tab", nil, "ab", []buffer.Selection{cur(0, 1)}, "a   b", []buffer.Selection{cur(0, 4)}},
		{"outdent", "outdent", nil, "    ab", []buffer.Selection{cur(0, 6)}, "ab", []buffer.Selection{cur(0, 2)}},
		{"lineBreakInsert keeps the cursor before the break", "lineBreakInsert", nil, "  abcd", []buffer.Selection{cur(0, 4)}, "  ab\n  cd", []buffer.Selection{cur(0, 4)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := run(t, tc.text, tc.from, tc.id, tc.args)
			if got := vm.Document().Text(); got != tc.want {
				t.Errorf("text %q, want %q", got, tc.want)
			}
			if got := vm.Cursors().Selections(); !buffer.SelectionsEqual(got, tc.wantSels) {
				t.Errorf("selections %v, want %v", got, tc.wantSels)
			}
		})
	}
}

func TestDeleteInsideWord(t *testing.T) {
	vm := run(t, "hello world", []buffer.Selection{cur(0, 2)}, "deleteInsideWord", nil)
	if got := vm.Document().Text(); strings.Contains(got, "hello") || !strings.Contains(got, "world") {
		t.Errorf("text %q", got)
	}
}

func TestUndoAndRedoPutTheCursorsBack(t *testing.T) {
	vm := run(t, "abc", []buffer.Selection{cur(0, 1)}, "type", "xy")
	if vm.Document().Text() != "axybc" {
		t.Fatalf("text %q", vm.Document().Text())
	}
	if err := vm.RunCommand("undo", nil); err != nil {
		t.Fatal(err)
	}
	if vm.Document().Text() != "abc" || vm.Cursors().Primary().Selection != cur(0, 1) {
		t.Errorf("after undo: %q %v", vm.Document().Text(), vm.Cursors().Primary().Selection)
	}
	if err := vm.RunCommand("redo", nil); err != nil {
		t.Fatal(err)
	}
	if vm.Document().Text() != "axybc" || vm.Cursors().Primary().Selection != cur(0, 3) {
		t.Errorf("after redo: %q %v", vm.Document().Text(), vm.Cursors().Primary().Selection)
	}
	// Nothing left to redo, nothing to undo past the start.
	for _, id := range []string{"redo", "undo", "undo", "undo"} {
		if err := vm.RunCommand(id, nil); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	if vm.Document().Text() != "abc" {
		t.Errorf("text %q", vm.Document().Text())
	}
}

func TestTypingJoinsIntoOneUndoStep(t *testing.T) {
	vm := run(t, "", []buffer.Selection{cur(0, 0)}, "type", "a")
	for _, c := range []string{"b", "c"} {
		if err := vm.RunCommand("type", c); err != nil {
			t.Fatal(err)
		}
	}
	if err := vm.RunCommand("undo", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.Document().Text(); got != "" {
		t.Errorf("one undo takes back the whole word: %q", got)
	}
}

func TestLineCommands(t *testing.T) {
	cases := []struct {
		name     string
		id       string
		text     string
		from     []buffer.Selection
		want     string
		wantSels []buffer.Selection
	}{
		{"copyLinesUp", "editor.action.copyLinesUpAction", "a\nb", []buffer.Selection{cur(1, 0)}, "a\nb\nb", []buffer.Selection{cur(1, 0)}},
		{"copyLinesDown", "editor.action.copyLinesDownAction", "a\nb", []buffer.Selection{cur(0, 0)}, "a\na\nb", []buffer.Selection{cur(1, 0)}},
		{"moveLinesUp", "editor.action.moveLinesUpAction", "a\nb\nc", []buffer.Selection{cur(1, 0)}, "b\na\nc", []buffer.Selection{cur(0, 0)}},
		{"moveLinesDown", "editor.action.moveLinesDownAction", "a\nb\nc", []buffer.Selection{cur(0, 0)}, "b\na\nc", []buffer.Selection{cur(1, 0)}},
		{"sortLinesAscending", "editor.action.sortLinesAscending", "c\nb\na", []buffer.Selection{sel(0, 0, 2, 1)}, "a\nb\nc", []buffer.Selection{sel(0, 0, 2, 1)}},
		{"sortLinesDescending", "editor.action.sortLinesDescending", "a\nb\nc", []buffer.Selection{sel(0, 0, 2, 1)}, "c\nb\na", []buffer.Selection{sel(0, 0, 2, 1)}},
		{"trimTrailingWhitespace", "editor.action.trimTrailingWhitespace", "a  \nb\t\nc", []buffer.Selection{cur(2, 1)}, "a\nb\nc", []buffer.Selection{cur(2, 1)}},
		{"deleteLines", "editor.action.deleteLines", "a\nb\nc", []buffer.Selection{cur(1, 0)}, "a\nc", []buffer.Selection{cur(1, 0)}},
		{"indentLines", "editor.action.indentLines", "a", []buffer.Selection{cur(0, 0)}, "    a", []buffer.Selection{cur(0, 4)}},
		{"outdentLines", "editor.action.outdentLines", "    a", []buffer.Selection{cur(0, 4)}, "a", []buffer.Selection{cur(0, 0)}},
		{"insertLineBefore", "editor.action.insertLineBefore", "a", []buffer.Selection{cur(0, 1)}, "\na", []buffer.Selection{cur(0, 0)}},
		{"insertLineAfter", "editor.action.insertLineAfter", "a", []buffer.Selection{cur(0, 1)}, "a\n", []buffer.Selection{cur(1, 0)}},
		{"joinLines", "editor.action.joinLines", "a\nb", []buffer.Selection{cur(0, 0)}, "a b", []buffer.Selection{cur(0, 1)}},
		{"transpose", "editor.action.transpose", "ab", []buffer.Selection{cur(0, 1)}, "ba", []buffer.Selection{cur(0, 2)}},
		{"upper case", "editor.action.transformToUppercase", "abc", []buffer.Selection{sel(0, 0, 0, 3)}, "ABC", []buffer.Selection{sel(0, 0, 0, 3)}},
		{"lower case", "editor.action.transformToLowercase", "ABC", []buffer.Selection{sel(0, 0, 0, 3)}, "abc", []buffer.Selection{sel(0, 0, 0, 3)}},
		{"title case", "editor.action.transformToTitlecase", "hello world", []buffer.Selection{sel(0, 0, 0, 11)}, "Hello World", []buffer.Selection{sel(0, 0, 0, 11)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := run(t, tc.text, tc.from, tc.id, nil)
			if got := vm.Document().Text(); got != tc.want {
				t.Errorf("text %q, want %q", got, tc.want)
			}
			if got := vm.Cursors().Selections(); !buffer.SelectionsEqual(got, tc.wantSels) {
				t.Errorf("selections %v, want %v", got, tc.wantSels)
			}
		})
	}
}

func TestMultiCursorCommands(t *testing.T) {
	cases := []struct {
		name string
		id   string
		text string
		from []buffer.Selection
		want []buffer.Selection
	}{
		{"cursor above", "editor.action.insertCursorAbove", "abc\ndef", []buffer.Selection{cur(1, 2)}, []buffer.Selection{cur(0, 2), cur(1, 2)}},
		{"cursor below", "editor.action.insertCursorBelow", "abc\ndef", []buffer.Selection{cur(0, 2)}, []buffer.Selection{cur(0, 2), cur(1, 2)}},
		{"cursors at the ends of the selected lines", "editor.action.insertCursorAtEndOfEachLineSelected", "ab\ncd\nef", []buffer.Selection{sel(0, 0, 2, 1)}, []buffer.Selection{cur(0, 2), cur(1, 2), cur(2, 1)}},
		{"select highlights", "editor.action.selectHighlights", "foo bar foo\nfoo", []buffer.Selection{sel(0, 0, 0, 3)}, []buffer.Selection{sel(0, 0, 0, 3), sel(0, 8, 0, 11), sel(1, 0, 1, 3)}},
		{"move to the next occurrence", "editor.action.moveSelectionToNextFindMatch", "foo bar foo", []buffer.Selection{sel(0, 0, 0, 3)}, []buffer.Selection{sel(0, 8, 0, 11)}},
		{"move to the previous occurrence", "editor.action.moveSelectionToPreviousFindMatch", "foo bar foo", []buffer.Selection{sel(0, 8, 0, 11)}, []buffer.Selection{sel(0, 0, 0, 3)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			vm := run(t, tc.text, tc.from, tc.id, nil)
			got := vm.Cursors().Selections()
			if len(got) != len(tc.want) {
				t.Fatalf("%v, want %v", got, tc.want)
			}
			for _, w := range tc.want {
				found := false
				for _, g := range got {
					found = found || g == w
				}
				if !found {
					t.Errorf("%v is missing from %v", w, got)
				}
			}
		})
	}
}

func TestAddSelectionToNextFindMatchGoesOnAsASession(t *testing.T) {
	vm := run(t, "foo bar foo bar foo", []buffer.Selection{cur(0, 1)}, "editor.action.addSelectionToNextFindMatch", nil)
	if got := vm.Cursors().Selections(); len(got) != 1 || got[0] != sel(0, 0, 0, 3) {
		t.Fatalf("the first press selects the word: %v", got)
	}
	for _, want := range []int{2, 3, 3} {
		if err := vm.RunCommand("editor.action.addSelectionToNextFindMatch", nil); err != nil {
			t.Fatal(err)
		}
		if got := vm.Cursors().Len(); got != want {
			t.Errorf("%d selections, want %d", got, want)
		}
	}
	// Another command moves the cursors: the session is over and the next press starts one.
	if err := vm.RunCommand("removeSecondaryCursors", nil); err != nil {
		t.Fatal(err)
	}
	if vm.occurrences != nil {
		t.Fatal("a command that is not one of the session ends it")
	}
	if err := vm.RunCommand("editor.action.addSelectionToNextFindMatch", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.Cursors().Len(); got != 2 {
		t.Errorf("after the reset a selected word is searched for: %d selections", got)
	}
	if err := vm.RunCommand("editor.action.addSelectionToPreviousFindMatch", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.Cursors().Len(); got < 2 {
		t.Errorf("previous: %d selections", got)
	}
}

func TestSmartSelectExpandsAndShrinks(t *testing.T) {
	vm := run(t, "foo.bar baz", []buffer.Selection{cur(0, 5)}, "editor.action.smartSelect.expand", nil)
	first := vm.Cursors().Primary().Selection
	if first.IsEmpty() || first.Start().Col > 5 || first.End().Col < 5 {
		t.Fatalf("the first expansion selects around the cursor: %v", first)
	}
	if err := vm.RunCommand("editor.action.smartSelect.expand", nil); err != nil {
		t.Fatal(err)
	}
	second := vm.Cursors().Primary().Selection
	if second.Start().Col > first.Start().Col || second.End().Col < first.End().Col || second == first {
		t.Errorf("the second is bigger: %v then %v", first, second)
	}
	if err := vm.RunCommand("editor.action.smartSelect.shrink", nil); err != nil {
		t.Fatal(err)
	}
	if got := vm.Cursors().Primary().Selection; got != first {
		t.Errorf("shrink goes back: %v, want %v", got, first)
	}
	// Moving the cursor ends the expansion; shrink then has nothing to go back to.
	if err := vm.RunCommand("cursorRight", nil); err != nil {
		t.Fatal(err)
	}
	moved := vm.Cursors().Selections()
	if err := vm.RunCommand("editor.action.smartSelect.shrink", nil); err != nil {
		t.Fatal(err)
	}
	if !buffer.SelectionsEqual(vm.Cursors().Selections(), moved) {
		t.Errorf("shrink without an expansion moved the cursors: %v", vm.Cursors().Selections())
	}
}

func TestReadOnlyDocumentRefusesEditsButMoves(t *testing.T) {
	vm := NewViewModel(newTestDoc("abc"), testOptions(), nil)
	vm.Document().SetReadOnly(true)
	vm.Cursors().SetSelections([]buffer.Selection{cur(0, 1)})
	err := vm.RunCommand("type", "x")
	appErr, ok := errors.FromError(err)
	if !ok || !strings.HasSuffix(appErr.Route(), "DocumentReadOnly") {
		t.Fatalf("type: %v", err)
	}
	if vm.Document().Text() != "abc" {
		t.Errorf("text %q", vm.Document().Text())
	}
	if err := vm.RunCommand("cursorRight", nil); err != nil {
		t.Fatal(err)
	}
	if vm.Cursors().Primary().Position() != positionOf(0, 2) {
		t.Errorf("cursor %v", vm.Cursors().Primary().Position())
	}
}

func TestUnknownCommand(t *testing.T) {
	vm := NewViewModel(newTestDoc("abc"), testOptions(), nil)
	err := vm.RunCommand("noSuchCommand", nil)
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("ViewModel.RunCommand.EditorCommandUnknown") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestCommandsRevealTheCursor(t *testing.T) {
	var lines strings.Builder
	for range 100 {
		lines.WriteString("line\n")
	}
	vm := NewViewModel(newTestDoc(lines.String()), testOptions(), nil)
	vm.SetTextMetrics(400, 190, 1000, 0, 8) // ten rows of 19 in view
	var scrolled []float64
	vm.SetScroller(func(top float64) { scrolled = append(scrolled, top); vm.SetScrollTop(top) })
	vm.Cursors().SetSelections([]buffer.Selection{cur(0, 0)})

	if err := vm.RunCommand("cursorDown", nil); err != nil {
		t.Fatal(err)
	}
	if len(scrolled) != 0 {
		t.Errorf("a cursor in view does not scroll: %v", scrolled)
	}
	vm.Cursors().SetSelections([]buffer.Selection{cur(20, 0)})
	if err := vm.RunCommand("cursorRight", nil); err != nil {
		t.Fatal(err)
	}
	// Row 20 ends at 21*19 = 399; the view is 190 high.
	if len(scrolled) != 1 || scrolled[0] != 399-190 {
		t.Errorf("scrolled %v, want the row at the bottom edge", scrolled)
	}
	if err := vm.RunCommand("cursorTop", nil); err != nil {
		t.Fatal(err)
	}
	if got := scrolled[len(scrolled)-1]; got != 0 {
		t.Errorf("back to the top: %v", got)
	}
}

func TestEditingNotifiesTheLayersMovingDoesNot(t *testing.T) {
	vm := NewViewModel(newTestDoc("abc\ndef"), testOptions(), nil)
	vm.Cursors().SetSelections([]buffer.Selection{cur(0, 1)})
	changed, selected := 0, 0
	vm.AddListener(func() { changed++ })
	vm.AddSelectionListener(func() { selected++ })
	if err := vm.RunCommand("cursorRight", nil); err != nil {
		t.Fatal(err)
	}
	if changed != 0 || selected != 1 {
		t.Errorf("moving: %d text changes, %d selection changes", changed, selected)
	}
	if err := vm.RunCommand("type", "x"); err != nil {
		t.Fatal(err)
	}
	if changed == 0 || selected < 2 {
		t.Errorf("typing: %d text changes, %d selection changes", changed, selected)
	}
}

func TestBindCommandsStackAndUnbind(t *testing.T) {
	reg := command.DefaultRegistry()
	first := NewViewModel(newTestDoc("one"), testOptions(), nil)
	second := NewViewModel(newTestDoc("two"), testOptions(), nil)
	first.Cursors().SetSelections([]buffer.Selection{cur(0, 0)})
	second.Cursors().SetSelections([]buffer.Selection{cur(0, 0)})
	runType := func() {
		if err := reg.Run(command.Context{}, "type", "!"); err != nil {
			t.Fatal(err)
		}
	}
	if err := reg.Run(command.Context{}, "type", "!"); err == nil {
		t.Error("no editor is bound yet")
	}
	unbindFirst := BindCommands(reg, first)
	runType()
	unbindSecond := BindCommands(reg, second)
	runType()
	if first.Document().Text() != "!one" || second.Document().Text() != "!two" {
		t.Errorf("the editor bound last runs the commands: %q %q", first.Document().Text(), second.Document().Text())
	}
	unbindSecond()
	runType()
	if first.Document().Text() != "!!one" || second.Document().Text() != "!two" {
		t.Errorf("the first one again: %q %q", first.Document().Text(), second.Document().Text())
	}
	unbindFirst()
	unbindFirst() // again: nothing happens
	if reg.HasHandler("type") {
		t.Error("handlers left behind")
	}
	// A registry that lacks a command is bound without it.
	empty := command.NewRegistry()
	BindCommands(empty, first)()
}
