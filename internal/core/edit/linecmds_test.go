package edit

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

func sortCmd(desc bool) func(Commands, []buffer.Selection) Result {
	return func(c Commands, s []buffer.Selection) Result { return c.SortLines(s, desc) }
}

// Ported from VS Code: reference/vscode/src/vs/editor/contrib/linesOperations/test/browser/linesOperations.test.ts
// ('SortLinesAscendingAction', 'SortLinesDescendingAction').
func TestVSCodeSortLinesActions(t *testing.T) {
	t.Run("ascending: selected lines", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "omicron", "beta", "alpha").at(sel4{1, 1, 3, 5})
		e.run(sortCmd(false))
		e.assertText("alpha\nbeta\nomicron")
		e.assertSels(sel4{1, 1, 3, 7})
	})
	t.Run("ascending: the whole text without a selection", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "omicron", "beta", "alpha")
		e.run(sortCmd(false))
		e.assertText("alpha\nbeta\nomicron")
	})
	t.Run("ascending: several selections", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "omicron", "beta", "alpha", "", "omicron", "beta", "alpha").at(sel4{1, 1, 3, 5}, sel4{5, 1, 7, 5})
		e.run(sortCmd(false))
		e.assertText("alpha\nbeta\nomicron\n\nalpha\nbeta\nomicron")
		e.assertSels(sel4{1, 1, 3, 7}, sel4{5, 1, 7, 7})
	})
	t.Run("ascending: a single line selection sorts the whole text", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "omicron", "beta", "alpha").at(sel4{2, 1, 2, 4})
		e.run(sortCmd(false))
		e.assertText("alpha\nbeta\nomicron")
	})
	t.Run("descending: selected lines", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "alpha", "beta", "omicron").at(sel4{1, 1, 3, 7})
		e.run(sortCmd(true))
		e.assertText("omicron\nbeta\nalpha")
		e.assertSels(sel4{1, 1, 3, 5})
	})
	t.Run("descending: several selections", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "alpha", "beta", "omicron", "", "alpha", "beta", "omicron").at(sel4{1, 1, 3, 7}, sel4{5, 1, 7, 7})
		e.run(sortCmd(true))
		e.assertText("omicron\nbeta\nalpha\n\nomicron\nbeta\nalpha")
		e.assertSels(sel4{1, 1, 3, 5}, sel4{5, 1, 7, 5})
	})
	t.Run("nothing changes when a selection is already in order", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "b", "a", "x", "y").at(sel4{1, 1, 2, 2}, sel4{3, 1, 4, 2})
		e.run(sortCmd(false))
		e.assertText("b\na\nx\ny")
		if e.doc.CanUndo() {
			t.Error("a sort with nothing to do was recorded")
		}
	})
	t.Run("the order of the locale, not of the bytes", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "banana", "Apple", "cherry", "apple", "Éclair", "eclair").at(sel4{1, 1, 6, 7})
		e.run(sortCmd(false))
		e.assertText("apple\nApple\nbanana\ncherry\neclair\nÉclair")
	})
}

// Ported from VS Code: linesOperations.test.ts ('JoinLinesAction').
func TestVSCodeJoinLines(t *testing.T) {
	join := func(c Commands, s []buffer.Selection) Result { return c.JoinLines(s) }
	t.Run("join lines and insert a space if necessary", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "hello", "world", "hello ", "world", "hello\t\t", "\tworld", "hello   ", "\tworld", "", "", "hello world")
		e.caret(1, 2).run(join)
		e.assertLine(1, "hello world")
		e.assertCaret(1, 6)
		e.caret(2, 2).run(join)
		e.assertLine(2, "hello world")
		e.assertCaret(2, 7)
		e.caret(3, 2).run(join)
		e.assertLine(3, "hello world")
		e.assertCaret(3, 7)
		e.at(sel4{4, 2, 5, 3}).run(join)
		e.assertLine(4, "hello world")
		e.assertSels(sel4{4, 2, 4, 8})
		e.at(sel4{5, 1, 7, 3}).run(join)
		e.assertLine(5, "hello world")
		e.assertSels(sel4{5, 1, 5, 3})
	})
	t.Run("#50471 join lines at the end of the document", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "hello", "world").caret(2, 1)
		e.run(join)
		e.assertText("hello\nworld")
		e.assertCaret(2, 6)
	})
	t.Run("multi cursor mode", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "hello", "world", "hello ", "world", "hello\t\t", "\tworld", "hello   ", "\tworld", "", "", "hello world").
			at(sel4{5, 2, 5, 2}, sel4{1, 2, 1, 2}, sel4{3, 2, 4, 2}, sel4{5, 4, 6, 3}, sel4{7, 5, 8, 4}, sel4{10, 1, 10, 1})
		e.run(join)
		e.assertText("hello world\nhello world\nhello world\nhello world\n\nhello world")
		e.assertSels(sel4{3, 4, 3, 8}, sel4{1, 6, 1, 6}, sel4{2, 2, 2, 8}, sel4{4, 5, 4, 9}, sel4{6, 1, 6, 1})
	})
	t.Run("pushes an undo stop", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "hello", "world").caret(1, 6)
		e.typeText(" my dear")
		e.assertLine(1, "hello my dear")
		e.assertCaret(1, 14)
		e.run(join)
		e.assertLine(1, "hello my dear world")
		e.assertCaret(1, 14)
		e.undo()
		e.assertLine(1, "hello my dear")
		e.assertCaret(1, 14)
	})
	t.Run("a primary cursor that is not joined moves up with the lines above it", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "a", "b", "c", "").at(sel4{4, 1, 4, 1}, sel4{1, 1, 1, 1})
		e.run(join)
		e.assertText("a b\nc\n")
		e.assertSels(sel4{3, 1, 3, 1}, sel4{1, 2, 1, 2})
	})
}

// Ported from VS Code: linesOperations.test.ts ('transpose').
func TestVSCodeTranspose(t *testing.T) {
	transpose := func(c Commands, s []buffer.Selection) Result { return c.Transpose(s) }
	t.Run("transpose", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "hello world", "", "", "   ").caret(1, 1)
		e.run(transpose)
		e.assertLine(1, "hello world")
		e.assertCaret(1, 2)
		e.caret(1, 6).run(transpose)
		e.assertLine(1, "hell oworld")
		e.assertCaret(1, 7)
		e.caret(1, 12).run(transpose)
		e.assertLine(1, "hell oworl")
		e.assertCaret(2, 2)
		e.caret(3, 1).run(transpose)
		e.assertLine(3, "")
		e.assertCaret(4, 1)
		e.caret(4, 2).run(transpose)
		e.assertLine(4, "   ")
		e.assertCaret(4, 3)
	})
	t.Run("issue #16633", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "", "", "hello", "world", "", "hello world", "", "hello world").caret(1, 1)
		e.run(transpose)
		e.assertLine(2, "")
		e.assertCaret(2, 1)
		e.caret(3, 6).run(transpose)
		e.assertLine(4, "oworld")
		e.assertCaret(4, 2)
		e.caret(6, 12).run(transpose)
		e.assertLine(7, "d")
		e.assertCaret(7, 2)
		e.caret(8, 12).run(transpose)
		e.assertLine(8, "hello world")
		e.assertCaret(8, 12)
	})
	t.Run("selections are left alone and characters are code points", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "a😍b", "xy").at(sel4{2, 1, 2, 2}, sel4{1, 4, 1, 4})
		e.run(transpose)
		e.assertText("ab😍\nxy")
		e.assertSels(sel4{2, 1, 2, 2}, sel4{1, 5, 1, 5})
	})
}

func caseCmd(kind Case) func(Commands, []buffer.Selection) Result {
	return func(c Commands, s []buffer.Selection) Result { return c.TransformCase(s, kind) }
}

// Ported from VS Code: linesOperations.test.ts ('toggle case': the upper, lower and
// title case steps; the snake, camel, pascal and kebab case commands are not part of
// Emetos).
func TestVSCodeToggleCase(t *testing.T) {
	e := newEd(t, DefaultOptions(), "hello world", "öçşğü")
	e.at(sel4{1, 1, 1, 12}).run(caseCmd(CaseUpper))
	e.assertLine(1, "HELLO WORLD")
	e.assertSels(sel4{1, 1, 1, 12})
	e.at(sel4{1, 1, 1, 12}).run(caseCmd(CaseLower))
	e.assertLine(1, "hello world")
	e.assertSels(sel4{1, 1, 1, 12})
	e.caret(1, 3).run(caseCmd(CaseUpper))
	e.assertLine(1, "HELLO world")
	e.assertCaret(1, 3)
	e.caret(1, 4).run(caseCmd(CaseLower))
	e.assertLine(1, "hello world")
	e.assertCaret(1, 4)
	e.at(sel4{1, 1, 1, 12}).run(caseCmd(CaseTitle))
	e.assertLine(1, "Hello World")
	e.assertSels(sel4{1, 1, 1, 12})
	e.at(sel4{2, 1, 2, 6}).run(caseCmd(CaseUpper))
	e.assertLine(2, "ÖÇŞĞÜ")
	e.assertSels(sel4{2, 1, 2, 6})
	e.at(sel4{2, 1, 2, 6}).run(caseCmd(CaseLower))
	e.assertLine(2, "öçşğü")
	e.at(sel4{2, 1, 2, 6}).run(caseCmd(CaseTitle))
	e.assertLine(2, "Öçşğü")
	e.assertSels(sel4{2, 1, 2, 6})
}

func TestVSCodeTitleCase(t *testing.T) {
	e := newEd(t, DefaultOptions(), "foO baR BaZ", "foO'baR'BaZ", "foO[baR]BaZ", "foO`baR~BaZ", "foO^baR%BaZ", "foO$baR!BaZ", "'physician's assistant'")
	for i, want := range []string{"Foo Bar Baz", "Foo'bar'baz", "Foo[Bar]Baz", "Foo`Bar~Baz", "Foo^Bar%Baz", "Foo$Bar!Baz", "'Physician's Assistant'"} {
		e.at(sel4{i + 1, 1, i + 1, len(e.line(i+1)) + 1}).run(caseCmd(CaseTitle))
		e.assertLine(i+1, want)
	}
}

func TestTransformCase(t *testing.T) {
	t.Run("full case mapping", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "straße ǆ ΑΣ").at(sel4{1, 1, 1, 12})
		e.run(caseCmd(CaseUpper))
		e.assertLine(1, "STRASSE Ǆ ΑΣ")
		e.at(sel4{1, 1, 1, 13}).run(caseCmd(CaseLower))
		e.assertLine(1, "strasse ǆ ας")
	})
	t.Run("a caret not in a word does nothing", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "ab  cd").caret(1, 4)
		e.run(caseCmd(CaseUpper))
		e.assertText("ab  cd")
		if e.doc.CanUndo() {
			t.Error("a no-op was recorded")
		}
	})
	t.Run("several selections and words", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "one two", "three four").at(sel4{1, 2, 1, 2}, sel4{2, 8, 2, 11}, sel4{1, 6, 1, 6})
		e.run(caseCmd(CaseUpper))
		e.assertText("ONE TWO\nthree fOUR")
		e.assertSels(sel4{1, 2, 1, 2}, sel4{2, 8, 2, 11}, sel4{1, 6, 1, 6})
	})
	t.Run("a selection across lines", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "ab", "cd", "ef").at(sel4{1, 2, 3, 2})
		e.run(caseCmd(CaseUpper))
		e.assertText("aB\nCD\nEf")
		e.assertSels(sel4{1, 2, 3, 2})
	})
	t.Run("the text grows", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "aß b").at(sel4{1, 1, 1, 3})
		e.run(caseCmd(CaseUpper))
		e.assertText("ASS b")
		e.assertSels(sel4{1, 1, 1, 4})
	})
}

func lineInsert(before bool) func(Commands, []buffer.Selection) Result {
	return func(c Commands, s []buffer.Selection) Result {
		if before {
			return c.InsertLineBefore(s)
		}
		return c.InsertLineAfter(s)
	}
}

// Ported from VS Code: linesOperations.test.ts ('InsertLineBeforeAction',
// 'InsertLineAfterAction').
func TestVSCodeInsertLine(t *testing.T) {
	text := []string{"First line", "Second line", "Third line"}
	before := []struct {
		line int
		want string
	}{
		{1, "\nFirst line\nSecond line\nThird line"},
		{2, "First line\n\nSecond line\nThird line"},
		{3, "First line\nSecond line\n\nThird line"},
	}
	for _, tt := range before {
		e := newEd(t, DefaultOptions(), text...).caret(tt.line, 3)
		e.run(lineInsert(true))
		e.assertText(tt.want)
		e.assertCaret(tt.line, 1)
	}
	after := []struct {
		line int
		want string
	}{
		{1, "First line\n\nSecond line\nThird line"},
		{2, "First line\nSecond line\n\nThird line"},
		{3, "First line\nSecond line\nThird line\n"},
	}
	for _, tt := range after {
		e := newEd(t, DefaultOptions(), text...).caret(tt.line, 3)
		e.run(lineInsert(false))
		e.assertText(tt.want)
		e.assertCaret(tt.line+1, 1)
	}
}

func TestInsertLineKeepsIndentation(t *testing.T) {
	e := newEd(t, DefaultOptions(), "  if x {", "    y", "  }").caret(2, 3)
	e.run(lineInsert(false))
	e.assertText("  if x {\n    y\n    \n  }")
	e.assertCaret(3, 5)
	e.caret(1, 3).run(lineInsert(true))
	e.assertText("\n  if x {\n    y\n    \n  }")
	e.assertCaret(1, 1)
	e.caret(3, 1).run(lineInsert(true))
	e.assertLine(3, "  ")
	e.assertCaret(3, 3)
}

func TestInsertLineWithSeveralCursors(t *testing.T) {
	e := newEd(t, DefaultOptions(), "a", " b", "c").at(sel4{1, 1, 1, 1}, sel4{2, 2, 2, 2})
	e.run(lineInsert(false))
	e.assertText("a\n\n b\n \nc")
	e.assertSels(sel4{2, 1, 2, 1}, sel4{4, 2, 4, 2})
}

func deleteLines(c Commands, s []buffer.Selection) Result { return c.DeleteLines(s) }

// Ported from VS Code: linesOperations.test.ts ('issue #62112', and the
// 'empty selection ...', 'with selection ...', 'with full line selection ...' and
// 'multicursor 1' cases of the delete lines tests).
func TestVSCodeDeleteLines(t *testing.T) {
	five := []string{"first", "second line", "third line", "fourth line", "fifth"}
	tests := []struct {
		name string
		text []string
		sels []sel4
		want []string
		at   []sel4
	}{
		{"issue #62112: several cursors on a line", []string{"a", "foo boo", "too", "c"}, []sel4{{2, 4, 2, 4}, {2, 8, 2, 8}, {3, 4, 3, 4}}, []string{"a", "c"}, []sel4{{2, 2, 2, 2}}},
		{"empty selection in middle of lines", five, []sel4{{2, 3, 2, 3}}, []string{"first", "third line", "fourth line", "fifth"}, []sel4{{2, 3, 2, 3}}},
		{"empty selection at top of lines", five, []sel4{{1, 5, 1, 5}}, []string{"second line", "third line", "fourth line", "fifth"}, []sel4{{1, 5, 1, 5}}},
		{"empty selection at end of lines", five, []sel4{{5, 2, 5, 2}}, []string{"first", "second line", "third line", "fourth line"}, []sel4{{4, 2, 4, 2}}},
		{"with selection in middle of lines", five, []sel4{{3, 3, 2, 2}}, []string{"first", "fourth line", "fifth"}, []sel4{{2, 2, 2, 2}}},
		{"with selection at top of lines", five, []sel4{{1, 4, 1, 5}}, []string{"second line", "third line", "fourth line", "fifth"}, []sel4{{1, 5, 1, 5}}},
		{"with selection at end of lines", five, []sel4{{5, 1, 5, 2}}, []string{"first", "second line", "third line", "fourth line"}, []sel4{{4, 2, 4, 2}}},
		{"with full line selection in middle of lines", five, []sel4{{4, 1, 2, 1}}, []string{"first", "fourth line", "fifth"}, []sel4{{2, 1, 2, 1}}},
		{"with full line selection at top of lines", five, []sel4{{2, 1, 1, 5}}, []string{"second line", "third line", "fourth line", "fifth"}, []sel4{{1, 5, 1, 5}}},
		{"with full line selection at end of lines", five, []sel4{{4, 1, 5, 2}}, []string{"first", "second line", "third line"}, []sel4{{3, 2, 3, 2}}},
		{"multicursor 1", []string{
			"class P {", "", "    getA() {", "        if (true) {", "            return \"a\";", "        }", "    }", "",
			"    getB() {", "        if (true) {", "            return \"b\";", "        }", "    }", "",
			"    getC() {", "        if (true) {", "            return \"c\";", "        }", "    }", "}",
		}, []sel4{{4, 1, 5, 1}, {10, 1, 11, 1}, {16, 1, 17, 1}}, []string{
			"class P {", "", "    getA() {", "            return \"a\";", "        }", "    }", "",
			"    getB() {", "            return \"b\";", "        }", "    }", "",
			"    getC() {", "            return \"c\";", "        }", "    }", "}",
		}, []sel4{{4, 1, 4, 1}, {9, 1, 9, 1}, {14, 1, 14, 1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEd(t, DefaultOptions(), tt.text...).at(tt.sels...)
			e.run(deleteLines)
			e.assertText(strings.Join(tt.want, "\n"))
			e.assertSels(tt.at...)
		})
	}
}

func TestDeleteLines(t *testing.T) {
	t.Run("an empty text has nothing to delete", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "")
		e.run(deleteLines)
		e.assertText("")
		if e.doc.CanUndo() {
			t.Error("a no-op was recorded")
		}
	})
	t.Run("the only line", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "abc").caret(1, 3)
		e.run(deleteLines)
		e.assertText("")
		e.assertCaret(1, 1)
	})
	t.Run("the column fits the new line", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "a long line", "ab", "x").caret(1, 8)
		e.run(deleteLines)
		e.assertText("ab\nx")
		e.assertCaret(1, 3)
	})
	t.Run("all the lines", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "a", "b", "c").at(sel4{1, 1, 3, 2})
		e.run(deleteLines)
		e.assertText("")
	})
	t.Run("undo restores the lines", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "a", "b", "c").caret(2, 1)
		e.run(deleteLines)
		e.assertText("a\nc")
		e.undo()
		e.assertText("a\nb\nc")
		e.assertCaret(2, 1)
	})
}

func trimEdits(t *testing.T, lines []string, cursors []sel4) []string {
	t.Helper()
	e := newEd(t, DefaultOptions(), lines...)
	var sels []buffer.Selection
	for _, c := range cursors {
		sels = append(sels, e.sel(c))
	}
	res := NewCommands(NewModel(e.doc.Snapshot()), e.opts).TrimTrailingWhitespace(sels, len(sels) > 0)
	var out []string
	for _, ed := range res.Edits {
		if ed.Text != "" {
			t.Errorf("edit %+v inserts text", ed)
		}
		l1, c1, _ := vsLineCol(e, ed.Range.Start)
		l2, c2, _ := vsLineCol(e, ed.Range.End)
		out = append(out, strings.Join([]string{itoa(l1), itoa(c1), itoa(l2), itoa(c2)}, ","))
	}
	return out
}

func vsLineCol(e *ed, p buffer.Position) (int, int, bool) {
	l, c := vsFromPosition(e.doc.Text(), p)
	return l, c, true
}

// Ported from VS Code: reference/vscode/src/vs/editor/test/browser/commands/trimTrailingWhitespaceCommand.test.ts
// ('remove trailing whitespace'; the second test needs the tokens of a language).
func TestVSCodeTrimTrailingWhitespace(t *testing.T) {
	mixed := []string{"some text\t", "some more text", "\t  ", "even more text  ", "and some mixed\t   \t"}
	tests := []struct {
		name    string
		lines   []string
		cursors []sel4
		want    []string
	}{
		{"empty", []string{""}, nil, nil},
		{"no white space", []string{"text"}, nil, nil},
		{"spaces", []string{"text   "}, nil, []string{"1,5,1,8"}},
		{"tab then spaces", []string{"text\t   "}, nil, []string{"1,5,1,9"}},
		{"only white space", []string{"\t   "}, nil, []string{"1,1,1,5"}},
		{"one tab", []string{"text\t"}, nil, []string{"1,5,1,6"}},
		{"several lines", mixed, nil, []string{"1,10,1,11", "3,1,3,4", "4,15,4,17", "5,15,5,20"}},
		{"cursors before the white space", []string{"text   "}, []sel4{{1, 1, 1, 1}, {1, 2, 1, 2}, {1, 3, 1, 3}}, []string{"1,5,1,8"}},
		{"a cursor at the start of the white space", []string{"text   "}, []sel4{{1, 1, 1, 1}, {1, 5, 1, 5}}, []string{"1,5,1,8"}},
		{"a cursor inside the white space", []string{"text   "}, []sel4{{1, 1, 1, 1}, {1, 5, 1, 5}, {1, 6, 1, 6}}, []string{"1,6,1,8"}},
		{"cursors on several lines", mixed, []sel4{{1, 11, 1, 11}, {3, 2, 3, 2}, {5, 1, 5, 1}, {4, 1, 4, 1}, {5, 10, 5, 10}}, []string{"3,2,3,4", "4,15,4,17", "5,15,5,20"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trimEdits(t, tt.lines, tt.cursors)
			if strings.Join(got, ";") != strings.Join(tt.want, ";") {
				t.Errorf("edits = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTrimTrailingWhitespaceMovesCursors(t *testing.T) {
	e := newEd(t, DefaultOptions(), "ab   ", "c ", "d").at(sel4{1, 4, 1, 4}, sel4{2, 3, 2, 3}, sel4{3, 2, 3, 2})
	e.run(func(c Commands, s []buffer.Selection) Result { return c.TrimTrailingWhitespace(s, false) })
	e.assertText("ab\nc\nd")
	e.assertSels(sel4{1, 3, 1, 3}, sel4{2, 2, 2, 2}, sel4{3, 2, 3, 2})
}

func TestLineCommandsWithSeveralCursors(t *testing.T) {
	t.Run("move down", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "a", "b", "c", "d", "e").at(sel4{1, 1, 1, 1}, sel4{3, 1, 3, 1})
		e.run(func(c Commands, s []buffer.Selection) Result { return c.MoveLines(s, true) })
		e.assertText("b\na\nd\nc\ne")
		e.assertSels(sel4{2, 1, 2, 1}, sel4{4, 1, 4, 1})
	})
	t.Run("move up: a block and a line", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "a", "b", "c", "d", "e").at(sel4{2, 1, 3, 1}, sel4{5, 1, 5, 1})
		e.run(func(c Commands, s []buffer.Selection) Result { return c.MoveLines(s, false) })
		e.assertText("b\na\nc\ne\nd")
		e.assertSels(sel4{1, 1, 2, 1}, sel4{4, 1, 4, 1})
	})
	t.Run("an empty line swaps with the next line", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "a", "", "b").caret(2, 1)
		e.run(func(c Commands, s []buffer.Selection) Result { return c.MoveLines(s, true) })
		e.assertText("a\nb\n")
		e.assertCaret(3, 1)
		e.run(func(c Commands, s []buffer.Selection) Result { return c.MoveLines(s, false) })
		e.assertText("a\n\nb")
		e.assertCaret(2, 1)
	})
	t.Run("two empty lines: only the cursor moves", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "a", "", "", "b").caret(2, 1)
		e.run(func(c Commands, s []buffer.Selection) Result { return c.MoveLines(s, true) })
		e.assertText("a\n\n\nb")
		e.assertCaret(3, 1)
	})
	t.Run("copy down with two cursors on different lines", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "a", "b", "c").at(sel4{3, 1, 3, 1}, sel4{1, 1, 1, 1})
		e.run(func(c Commands, s []buffer.Selection) Result { return c.CopyLines(s, true) })
		e.assertText("a\na\nb\nc\nc")
		e.assertSels(sel4{2, 1, 2, 1}, sel4{5, 1, 5, 1})
	})
	t.Run("copy lines on the same line once", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "abc", "def").at(sel4{1, 1, 1, 1}, sel4{1, 3, 1, 3})
		e.run(func(c Commands, s []buffer.Selection) Result { return c.CopyLines(s, false) })
		e.assertText("abc\nabc\ndef")
	})
	t.Run("undo of a move", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "a", "b", "c").caret(1, 1)
		e.run(func(c Commands, s []buffer.Selection) Result { return c.MoveLines(s, true) })
		e.assertText("b\na\nc")
		e.undo()
		e.assertText("a\nb\nc")
		e.assertCaret(1, 1)
	})
}

func TestEditKindOfLineCommands(t *testing.T) {
	e := newEd(t, DefaultOptions(), "a", "b")
	c := NewCommands(NewModel(e.doc.Snapshot()), e.opts)
	for name, res := range map[string]Result{
		"move":   c.MoveLines(e.sels, true),
		"copy":   c.CopyLines(e.sels, true),
		"delete": c.DeleteLines(e.sels),
		"join":   c.JoinLines(e.sels),
		"sort":   c.SortLines(e.sels, false),
		"case":   c.TransformCase(e.sels, CaseUpper),
		"trim":   c.TrimTrailingWhitespace(e.sels, false),
		"insert": c.InsertLineAfter(e.sels),
	} {
		if res.Kind != document.KindOther {
			t.Errorf("%s: kind = %v, want KindOther", name, res.Kind)
		}
	}
}

// Ported from VS Code: linesOperations.test.ts ('Bug 18276', 'issue #80736',
// 'Indenting on empty line should move cursor'), which test Indent Lines.
func TestVSCodeIndentLines(t *testing.T) {
	t.Run("Bug 18276: indentation when the selection is empty", func(t *testing.T) {
		e := newEd(t, tabsOptions(), "function baz() {").caret(1, 2)
		e.indent()
		e.assertLine(1, "\tfunction baz() {")
		e.assertCaret(1, 3)
		e.tab()
		e.assertLine(1, "\tf\tunction baz() {")
	})
	t.Run("issue #80736: the added tab is not selected", func(t *testing.T) {
		e := newEd(t, tabsOptions(), "Some text").caret(1, 1)
		e.indent()
		e.assertLine(1, "\tSome text")
		e.assertCaret(1, 2)
	})
	t.Run("indenting on an empty line moves the cursor", func(t *testing.T) {
		opts := DefaultOptions()
		opts.UseTabStops = false
		e := newEd(t, opts, "").caret(1, 1)
		e.indent()
		e.assertLine(1, "    ")
		e.assertCaret(1, 5)
	})
}
