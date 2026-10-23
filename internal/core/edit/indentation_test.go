package edit

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/indent"
	"github.com/rm4n0s/emetos/internal/core/indent/indenttest"
)

func TestNewLineLevels(t *testing.T) {
	tests := []struct {
		name  string
		auto  indent.AutoIndent
		lines []string
		line  int
		col   int
		want  []string
		caret [2]int
	}{
		{"none starts at the first column", indent.AutoIndentNone, []string{"    foo("}, 1, 9, []string{"    foo(", ""}, [2]int{2, 1}},
		{"keep copies the indentation", indent.AutoIndentKeep, []string{"    foo("}, 1, 9, []string{"    foo(", "    "}, [2]int{2, 5}},
		{"keep copies only what is before the cursor", indent.AutoIndentKeep, []string{"    foo("}, 1, 3, []string{"  ", "    foo("}, [2]int{2, 3}},
		{"brackets indents after an opening bracket", indent.AutoIndentBrackets, []string{"    foo("}, 1, 9, []string{"    foo(", "        "}, [2]int{2, 9}},
		{"brackets puts the closing bracket on its own line", indent.AutoIndentBrackets, []string{"foo()"}, 1, 5, []string{"foo(", "    ", ")"}, [2]int{2, 5}},
		{"brackets does not apply the on-enter rules", indent.AutoIndentBrackets, []string{"/**"}, 1, 4, []string{"/**", ""}, [2]int{2, 1}},
		{"advanced applies the on-enter rules", indent.AutoIndentAdvanced, []string{"/**"}, 1, 4, []string{"/**", " * "}, [2]int{2, 4}},
		{"advanced does not apply the indentation rules", indent.AutoIndentAdvanced, []string{"const f = (n) =>"}, 1, 17, []string{"const f = (n) =>", ""}, [2]int{2, 1}},
		{"full applies the indentation rules", indent.AutoIndentFull, []string{"const f = (n) =>"}, 1, 17, []string{"const f = (n) =>", "    "}, [2]int{2, 5}},
		{"a selection is replaced by the line break", indent.AutoIndentFull, []string{"function f() { a }"}, 1, 15, []string{"function f() {", "    a }"}, [2]int{2, 5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := indentEd(t, indenttest.TypeScript, tt.auto, nil, tt.lines...)
			if tt.name == "a selection is replaced by the line break" {
				e.at(sel4{1, 15, 1, 16})
			} else {
				e.caret(tt.line, tt.col)
			}
			e.enter()
			e.assertLines(tt.want...)
			e.assertCaret(tt.caret[0], tt.caret[1])
		})
	}
}

func TestNewLineWithTabs(t *testing.T) {
	e := indentEd(t, indenttest.TypeScript, indent.AutoIndentFull, nil, "function f() {", "\ta();", "}")
	e.opts.InsertSpaces = false
	e.caret(2, 6).enter()
	e.assertLines("function f() {", "\ta();", "\t", "}")
	e.caret(1, 15).enter()
	e.assertLines("function f() {", "\t", "\ta();", "\t", "}")
}

func TestNewLineWithoutLanguageRules(t *testing.T) {
	// A document with no indenter keeps the indentation, whatever the level.
	e := newEd(t, DefaultOptions(), "  foo(")
	e.caret(1, 7).enter()
	e.assertLines("  foo(", "  ")
	e.opts.AutoIndent = 0
	e.caret(2, 3).enter()
	e.assertLines("  foo(", "  ", "")
}

func TestNewLineNotCheapToTokenizeKeepsIndentation(t *testing.T) {
	e := indentEd(t, indenttest.TypeScript, indent.AutoIndentFull, nil, "function f() {")
	e.opts.Tokens = indenttest.Source{Code: indenttest.TypeScript, Content: func(l int) string { return e.line(l + 1) }, Cheap: func(int) bool { return false }}
	e.caret(1, 15).enter()
	e.assertLines("function f() {", "")
}

func TestLineBreakInsertKeepsPosition(t *testing.T) {
	e := indentEd(t, indenttest.TypeScript, indent.AutoIndentFull, nil, "{}")
	e.caret(1, 2)
	e.run(Commands.LineBreakInsert)
	e.assertLines("{", "    ", "}")
	e.assertCaret(1, 2)

	e = indentEd(t, indenttest.TypeScript, indent.AutoIndentFull, nil, "  a")
	e.caret(1, 4)
	e.run(Commands.LineBreakInsert)
	e.assertLines("  a", "  ")
	e.assertCaret(1, 4)
}

func TestInsertLineUsesRules(t *testing.T) {
	e := indentEd(t, indenttest.TypeScript, indent.AutoIndentFull, nil, "function f() {", "}")
	e.caret(1, 3)
	e.run(Commands.InsertLineAfter)
	e.assertLines("function f() {", "    ", "}")
	e.assertCaret(2, 5)

	e = indentEd(t, indenttest.TypeScript, indent.AutoIndentFull, nil, "function f() {", "    a();", "}")
	e.caret(3, 1)
	e.run(Commands.InsertLineBefore)
	e.assertLines("function f() {", "    a();", "    ", "}")
}

func TestEnterInsideIndentation(t *testing.T) {
	// The cursor in the indentation of a line stays at the same place in the new indentation.
	e := indentEd(t, indenttest.TypeScript, indent.AutoIndentFull, nil, "function f() {", "        a();", "}")
	e.caret(2, 3).enter()
	e.assertLine(2, "  ")
	e.assertLine(3, "    a();")
	e.assertCaret(3, 3)
}

func TestTabOnBlankLineUsesRules(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		want  string
	}{
		{"after an opening brace", []string{"function f() {", ""}, "    "},
		{"after a statement", []string{"function f() {", "    a();", ""}, "    "},
		{"after an if without braces", []string{"if (a)", ""}, "    "},
		{"in the first lines", []string{""}, "    "},
		{"in a comment whose lines the rules ignore", []string{" * a", " * b", ""}, " * "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := indentEd(t, indenttest.TypeScript, indent.AutoIndentFull, nil, tt.lines...)
			last := len(tt.lines)
			e.caret(last, 1).tab()
			e.assertLine(last, tt.want)
		})
	}
	t.Run("a line that has the indentation goes to the next level", func(t *testing.T) {
		e := indentEd(t, indenttest.TypeScript, indent.AutoIndentFull, nil, "function f() {", "    ")
		e.caret(2, 5).tab()
		e.assertLine(2, "        ")
	})
}

func TestReindentLines(t *testing.T) {
	src := []string{"function f() {", "if (a) {", "b();", "}", "}"}
	e := indentEd(t, indenttest.TypeScript, indent.AutoIndentFull, nil, src...)
	e.caret(3, 5)
	e.run(func(c Commands, s []buffer.Selection) Result { return c.Reindent(s, 0, 4) })
	e.assertLines("function f() {", "    if (a) {", "        b();", "    }", "}")
	e.assertCaret(3, 13)

	// A language without rules leaves the text alone.
	e = indentEd(t, indenttest.Go+100, indent.AutoIndentFull, nil, src...)
	e.run(func(c Commands, s []buffer.Selection) Result { return c.Reindent(s, 0, 4) })
	e.assertLines(src...)
}

func TestReindentSelectedLines(t *testing.T) {
	e := indentEd(t, indenttest.TypeScript, indent.AutoIndentFull, nil, "function f() {", "  a();", "      b();", "        c();", "}")
	e.at(sel4{3, 1, 5, 1})
	e.run(Commands.ReindentSelected)
	e.assertLines("function f() {", "  a();", "  b();", "  c();", "}")

	// The line above the selection is the base of the indentation.
	e = indentEd(t, indenttest.TypeScript, indent.AutoIndentFull, nil, "function f() {", "    a();", "      b();", "        c();", "}")
	e.at(sel4{3, 1, 5, 1})
	e.run(Commands.ReindentSelected)
	e.assertLines("function f() {", "    a();", "    b();", "    c();", "}")
}

func TestPasteIndentRespectsOptions(t *testing.T) {
	paste := "x();\ny();"
	src := []string{"function f() {", "    if (a) {", "", "    }", "}"}
	e := indentEd(t, indenttest.TypeScript, indent.AutoIndentFull, nil, src...)
	e.caret(3, 1)
	e.pasteIndent(paste, [4]int{3, 1, 4, 5})
	e.assertLines("function f() {", "    if (a) {", "        x();", "        y();", "    }", "}")
	e.assertCaret(4, 13)

	// The setting is off.
	e = indentEd(t, indenttest.TypeScript, indent.AutoIndentFull, nil, src...)
	e.caret(3, 1)
	e.pasteIndent(paste, [4]int{3, 1, 4, 5})
	e.opts.AutoIndentOnPaste = false
	e.assertLine(3, "        x();")

	// Below the level full there is no indentation on paste.
	e = indentEd(t, indenttest.TypeScript, indent.AutoIndentAdvanced, nil, src...)
	e.caret(3, 1)
	e.pasteIndent(paste, [4]int{3, 1, 4, 5})
	e.assertLines("function f() {", "    if (a) {", "x();", "y();", "    }", "}")
}
