package edit

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/indent"
	"github.com/rm4n0s/emetos/internal/core/indent/indenttest"
	"github.com/rm4n0s/emetos/internal/core/textmate"
)

// indentEd is an editor of the lines in a language of indenttest, with the tokens of the table,
// at the level of editor.autoIndent.
func indentEd(t *testing.T, code int, auto indent.AutoIndent, toks map[int][]indenttest.Tok, lines ...string) *ed {
	t.Helper()
	e := newEd(t, DefaultOptions(), lines...)
	e.opts.AutoIndent = auto
	e.opts.Indenter = indent.New(indenttest.All())
	e.opts.Tokens = indenttest.Source{Code: code, Lines: toks, Content: func(l int) string { return e.line(l + 1) }}
	return e
}

// insert types text as a program does (VS Code's type without interceptors): as it is, with no
// auto-indentation and no new line rules.
func (e *ed) insert(text string) *ed {
	e.t.Helper()
	return e.run(func(c Commands, s []buffer.Selection) Result { return c.CompositionType(s, text, 0, 0, 0) })
}

// enter presses Enter.
func (e *ed) enter() *ed { return e.typeText("\n") }

func (e *ed) assertLines(want ...string) {
	e.t.Helper()
	e.assertText(strings.Join(want, "\n"))
}

// step is a step of a ported test: select, insert text as it is, type text key by key, or check the
// text.
type step struct {
	sel    *[4]int
	insert string
	key    string
	want   []string
}

type indentCase struct {
	name  string
	lines []string
	steps []step
}

func runIndentCases(t *testing.T, code int, auto indent.AutoIndent, cases []indentCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := indentEd(t, code, auto, nil, tc.lines...)
			for _, s := range tc.steps {
				switch {
				case s.sel != nil:
					e.at(sel4(*s.sel))
				case s.insert != "":
					e.insert(s.insert)
				case s.key != "":
					e.typeText(s.key)
				default:
					e.assertLines(s.want...)
				}
			}
		})
	}
}

// Ported from VS Code: reference/vscode/src/vs/editor/contrib/indentation/test/browser/indentation.test.ts
// ('Auto Indent On Type - Visual Basic').
func TestVSCodeAutoIndentVisualBasic(t *testing.T) {
	runIndentCases(t, indenttest.VB, indent.AutoIndentFull, []indentCase{
		{name: "issue #118932: no indentation in visual basic files", lines: []string{"If True Then", "    Some code", "    End I"}, steps: []step{
			{sel: &[4]int{3, 10, 3, 10}},
			{key: "f"},
			{want: []string{"If True Then", "    Some code", "End If"}},
		}},
		{name: "issue #118932: indent after Module declaration", lines: []string{""}, steps: []step{
			{insert: "Module Test"},
			{key: "\n"},
			{want: []string{"Module Test", "    "}},
		}},
		{name: "issue #118932: indent after Sub declaration", lines: []string{"Module Test", "    "}, steps: []step{
			{sel: &[4]int{2, 5, 2, 5}},
			{insert: "Sub Main()"},
			{key: "\n"},
			{want: []string{"Module Test", "    Sub Main()", "        "}},
		}},
		{name: "issue #118932: dedent on End Sub", lines: []string{"Module Test", "    Sub Main()", "        Console.WriteLine(\"Hello\")", "        End Su"}, steps: []step{
			{sel: &[4]int{4, 15, 4, 15}},
			{key: "b"},
			{want: []string{"Module Test", "    Sub Main()", "        Console.WriteLine(\"Hello\")", "    End Sub"}},
		}},
		{name: "issue #118932: dedent on End Module", lines: []string{"Module Test", "    Private x As Integer", "    End Modul"}, steps: []step{
			{sel: &[4]int{3, 14, 3, 14}},
			{key: "e"},
			{want: []string{"Module Test", "    Private x As Integer", "End Module"}},
		}},
		{name: "issue #118932: indent after Function declaration", lines: []string{"Module Test", "    "}, steps: []step{
			{sel: &[4]int{2, 5, 2, 5}},
			{insert: "Function Add(a As Integer, b As Integer) As Integer"},
			{key: "\n"},
			{want: []string{"Module Test", "    Function Add(a As Integer, b As Integer) As Integer", "        "}},
		}},
		{name: "issue #118932: dedent on End Function", lines: []string{"Module Test", "    Function Add(a, b)", "        Return a + b", "        End Functio"}, steps: []step{
			{sel: &[4]int{4, 20, 4, 20}},
			{key: "n"},
			{want: []string{"Module Test", "    Function Add(a, b)", "        Return a + b", "    End Function"}},
		}},
		{name: "issue #118932: indent after If Then", lines: []string{"Sub Test()", "    "}, steps: []step{
			{sel: &[4]int{2, 5, 2, 5}},
			{insert: "If x > 0 Then"},
			{key: "\n"},
			{want: []string{"Sub Test()", "    If x > 0 Then", "        "}},
		}},
		{name: "issue #118932: indent after ElseIf Then", lines: []string{"Sub Test()", "    If x > 0 Then", "        DoSomething()", "    ElseIf x < 0 Then"}, steps: []step{
			{sel: &[4]int{4, 22, 4, 22}},
			{key: "\n"},
			{want: []string{"Sub Test()", "    If x > 0 Then", "        DoSomething()", "    ElseIf x < 0 Then", "        "}},
		}},
		{name: "issue #118932: dedent and indent on Else", lines: []string{"Sub Test()", "    If x > 0 Then", "        DoSomething()", "        Els"}, steps: []step{
			{sel: &[4]int{4, 12, 4, 12}},
			{key: "e"},
			{want: []string{"Sub Test()", "    If x > 0 Then", "        DoSomething()", "    Else"}},
		}},
		{name: "issue #118932: indent after While", lines: []string{"Sub Test()", "    "}, steps: []step{
			{sel: &[4]int{2, 5, 2, 5}},
			{insert: "While x > 0"},
			{key: "\n"},
			{want: []string{"Sub Test()", "    While x > 0", "        "}},
		}},
		{name: "issue #118932: dedent on End While", lines: []string{"Sub Test()", "    While x > 0", "        x = x - 1", "        End Whil"}, steps: []step{
			{sel: &[4]int{4, 17, 4, 17}},
			{key: "e"},
			{want: []string{"Sub Test()", "    While x > 0", "        x = x - 1", "    End While"}},
		}},
		{name: "issue #118932: indent after For", lines: []string{"Sub Test()", "    "}, steps: []step{
			{sel: &[4]int{2, 5, 2, 5}},
			{insert: "For i = 1 To 10"},
			{key: "\n"},
			{want: []string{"Sub Test()", "    For i = 1 To 10", "        "}},
		}},
		{name: "issue #118932: dedent on Next", lines: []string{"Sub Test()", "    For i = 1 To 10", "        DoSomething(i)", "        Nex"}, steps: []step{
			{sel: &[4]int{4, 12, 4, 12}},
			{key: "t"},
			{want: []string{"Sub Test()", "    For i = 1 To 10", "        DoSomething(i)", "    Next"}},
		}},
		{name: "issue #118932: indent after Do", lines: []string{"Sub Test()", "    "}, steps: []step{
			{sel: &[4]int{2, 5, 2, 5}},
			{insert: "Do"},
			{key: "\n"},
			{want: []string{"Sub Test()", "    Do", "        "}},
		}},
		{name: "issue #118932: dedent on Loop", lines: []string{"Sub Test()", "    Do", "        x = x + 1", "        Loo"}, steps: []step{
			{sel: &[4]int{4, 12, 4, 12}},
			{key: "p"},
			{want: []string{"Sub Test()", "    Do", "        x = x + 1", "    Loop"}},
		}},
		{name: "issue #118932: indent after Select Case", lines: []string{"Sub Test()", "    "}, steps: []step{
			{sel: &[4]int{2, 5, 2, 5}},
			{insert: "Select Case x"},
			{key: "\n"},
			{want: []string{"Sub Test()", "    Select Case x", "        "}},
		}},
		{name: "issue #118932: dedent on End Select", lines: []string{"Sub Test()", "    Select Case x", "        End Selec"}, steps: []step{
			{sel: &[4]int{3, 18, 3, 18}},
			{key: "t"},
			{want: []string{"Sub Test()", "    Select Case x", "    End Select"}},
		}},
		{name: "issue #118932: indent after Try", lines: []string{"Sub Test()", "    "}, steps: []step{
			{sel: &[4]int{2, 5, 2, 5}},
			{insert: "Try"},
			{key: "\n"},
			{want: []string{"Sub Test()", "    Try", "        "}},
		}},
		{name: "issue #118932: dedent and indent on Catch", lines: []string{"Sub Test()", "    Try", "        DoSomething()", "        Catc"}, steps: []step{
			{sel: &[4]int{4, 13, 4, 13}},
			{key: "h"},
			{want: []string{"Sub Test()", "    Try", "        DoSomething()", "    Catch"}},
		}},
		{name: "issue #118932: dedent and indent on Finally", lines: []string{"Sub Test()", "    Try", "        DoSomething()", "    Catch", "        HandleError()", "        Finall"}, steps: []step{
			{sel: &[4]int{6, 15, 6, 15}},
			{key: "y"},
			{want: []string{"Sub Test()", "    Try", "        DoSomething()", "    Catch", "        HandleError()", "    Finally"}},
		}},
		{name: "issue #118932: dedent on End Try", lines: []string{"Sub Test()", "    Try", "        DoSomething()", "    Catch", "        HandleError()", "    Finally", "        Cleanup()", "        End Tr"}, steps: []step{
			{sel: &[4]int{8, 15, 8, 15}},
			{key: "y"},
			{want: []string{"Sub Test()", "    Try", "        DoSomething()", "    Catch", "        HandleError()", "    Finally", "        Cleanup()", "    End Try"}},
		}},
		{name: "issue #118932: indent after Class", lines: []string{""}, steps: []step{
			{insert: "Class MyClass"},
			{key: "\n"},
			{want: []string{"Class MyClass", "    "}},
		}},
		{name: "issue #118932: dedent on End Class", lines: []string{"Class MyClass", "    Private x As Integer", "    End Clas"}, steps: []step{
			{sel: &[4]int{3, 14, 3, 14}},
			{key: "s"},
			{want: []string{"Class MyClass", "    Private x As Integer", "End Class"}},
		}},
		{name: "issue #118932: full program indentation flow", lines: []string{""}, steps: []step{
			{insert: "Module Test"},
			{key: "\n"},
			{want: []string{"Module Test", "    "}},
			{insert: "Sub Main()"},
			{key: "\n"},
			{want: []string{"Module Test", "    Sub Main()", "        "}},
			{insert: "Console.WriteLine(\"Hello, World!\")"},
			{key: "\n"},
			{want: []string{"Module Test", "    Sub Main()", "        Console.WriteLine(\"Hello, World!\")", "        "}},
			{insert: "End Su"},
			{key: "b"},
			{want: []string{"Module Test", "    Sub Main()", "        Console.WriteLine(\"Hello, World!\")", "    End Sub"}},
			{key: "\n"},
			{want: []string{"Module Test", "    Sub Main()", "        Console.WriteLine(\"Hello, World!\")", "    End Sub", "    "}},
		}},
	})
}

// pasteIndent pastes text as it is and then fits it to the indentation around it (the range is
// VS Code's, as the tests give it to AutoIndentOnPaste.trigger).
func (e *ed) pasteIndent(text string, r [4]int) *ed {
	e.t.Helper()
	e.opts.AutoIndentOnPaste = true
	e.run(func(c Commands, s []buffer.Selection) Result { return c.Paste(s, text, PasteOptions{}) })
	rng := buffer.Range{Start: e.pos(r[0], r[1]), End: e.pos(r[2], r[3])}
	return e.run(func(c Commands, s []buffer.Selection) Result { return c.AutoIndentPaste(s[0], rng) })
}

// Ported from VS Code: indentation.test.ts ('Auto Indent On Paste - TypeScript/JavaScript').
func TestVSCodeAutoIndentOnPaste(t *testing.T) {
	other, comment, str := textmate.TokenOther, textmate.TokenComment, textmate.TokenString
	t.Run("issue #119225: Do not add extra leading space when pasting JSDoc", func(t *testing.T) {
		paste := "/**\n * JSDoc\n */\nfunction a() {}"
		toks := map[int][]indenttest.Tok{
			0: {{Start: 0, Type: comment}, {Start: 3, Type: comment}},
			1: {{Start: 0, Type: comment}, {Start: 2, Type: comment}, {Start: 8, Type: comment}},
			2: {{Start: 0, Type: comment}, {Start: 1, Type: comment}, {Start: 3, Type: other}},
			3: {{Start: 0, Type: other}, {Start: 8, Type: other}, {Start: 9, Type: other}, {Start: 10, Type: other}, {Start: 11, Type: other}, {Start: 12, Type: other}, {Start: 13, Type: other}, {Start: 14, Type: other}, {Start: 15, Type: other}},
		}
		e := indentEd(t, indenttest.TypeScript, indent.AutoIndentFull, toks, "")
		e.pasteIndent(paste, [4]int{1, 1, 4, 16})
		e.assertText(paste)
	})
	t.Run("issue #167299: Blank line removes indent", func(t *testing.T) {
		paste := strings.Join([]string{
			"",
			"export type IncludeReference =",
			"\t| BaseReference",
			"\t| SelfReference",
			"\t| RelativeReference;",
			"",
			"export const enum IncludeReferenceKind {",
			"\tBase,",
			"\tSelf,",
			"\tRelativeReference,",
			"}",
		}, "\n")
		e := indentEd(t, indenttest.TypeScript, indent.AutoIndentFull, nil, "")
		e.pasteIndent(paste, [4]int{1, 1, 11, 2})
		e.assertText(paste)
	})
	t.Run("issue #29803: do not indent when pasting text with only one line", func(t *testing.T) {
		e := indentEd(t, indenttest.TypeScript, indent.AutoIndentFull, nil, "const linkHandler = new Class(a, b, c,", "    d)")
		e.caret(2, 6)
		e.pasteIndent(", null", [4]int{2, 6, 2, 11})
		e.assertLines("const linkHandler = new Class(a, b, c,", "    d, null)")
	})
	t.Run("issue #29753: incorrect indentation after comment", func(t *testing.T) {
		e := indentEd(t, indenttest.TypeScript, indent.AutoIndentFull, nil,
			"class A {", "    /**", "     * used only for debug purposes.", "     */", "    private _codeInfo: KeyMapping[];", "}")
		e.at(sel4{5, 24, 5, 34})
		e.pasteIndent("IMacLinuxKeyMapping", [4]int{5, 24, 5, 43})
		e.assertLines("class A {", "    /**", "     * used only for debug purposes.", "     */", "    private _codeInfo: IMacLinuxKeyMapping[];", "}")
	})
	t.Run("issue #29753: incorrect indentation of header comment", func(t *testing.T) {
		paste := "/*----------------\n *  Copyright (c) \n *  Licensed under ...\n *-----------------*/"
		e := indentEd(t, indenttest.TypeScript, indent.AutoIndentFull, nil, "")
		e.pasteIndent(paste, [4]int{1, 1, 4, 22})
		e.assertText(paste)
	})
	t.Run("issue #209859: do not change the indentation when pasted inside of a string", func(t *testing.T) {
		initial := []string{`const foo = "some text`, `         which is strangely`, `    indented"`}
		toks := map[int][]indenttest.Tok{
			0: {{Start: 0, Type: other}, {Start: 12, Type: str}},
			1: {{Start: 0, Type: str}},
			2: {{Start: 0, Type: str}},
		}
		// VS Code's test leaves its lines not yet tokenized, and so the paste is left alone;
		// Emetos does the same when a line is not cheap to tokenize.
		e := indentEd(t, indenttest.TypeScript, indent.AutoIndentFull, toks, initial...)
		e.opts.Tokens = indenttest.Source{Code: indenttest.TypeScript, Lines: toks, Content: func(l int) string { return e.line(l + 1) }, Cheap: func(l int) bool { return l == 0 }}
		e.at(sel4{2, 10, 2, 15})
		e.pasteIndent("which", [4]int{2, 1, 2, 28})
		e.assertLines(initial...)

		// With editor.autoIndentOnPasteWithinString off, the paste in a string is left alone.
		e = indentEd(t, indenttest.TypeScript, indent.AutoIndentFull, toks, initial...)
		e.opts.AutoIndentOnPasteWithinString = false
		e.at(sel4{2, 10, 2, 15})
		e.pasteIndent("which", [4]int{2, 1, 2, 28})
		e.assertLines(initial...)

		// With it on, the line is fitted to the indentation of the line above.
		e = indentEd(t, indenttest.TypeScript, indent.AutoIndentFull, toks, initial...)
		e.at(sel4{2, 10, 2, 15})
		e.pasteIndent("which", [4]int{2, 1, 2, 28})
		e.assertLine(2, "which is strangely")
	})
}

// Ported from VS Code: indentation.test.ts ('Auto Indent On Type - TypeScript/JavaScript').
func TestVSCodeAutoIndentOnTypeTypeScript(t *testing.T) {
	comment := textmate.TokenComment
	ts := func(auto indent.AutoIndent, toks map[int][]indenttest.Tok, lines ...string) *ed {
		return indentEd(t, indenttest.TypeScript, auto, toks, lines...)
	}
	t.Run("issue #208215: indent after arrow function", func(t *testing.T) {
		e := ts(indent.AutoIndentFull, nil, "")
		e.insert("const add1 = (n) =>").enter()
		e.assertLines("const add1 = (n) =>", "    ")
	})
	t.Run("issue #208215: indent after arrow function 2", func(t *testing.T) {
		e := ts(indent.AutoIndentFull, nil, "const array = [1, 2, 3, 4, 5];", "array.map(", "    v =>")
		e.caret(3, 9).enter()
		e.assertLines("const array = [1, 2, 3, 4, 5];", "array.map(", "    v =>", "        ")
	})
	t.Run("issue #116843: indent after arrow function", func(t *testing.T) {
		e := ts(indent.AutoIndentFull, nil, "")
		e.insert("const add1 = (n) =>\n    n + 1;").enter()
		e.assertLines("const add1 = (n) =>", "    n + 1;", "")
	})
	t.Run("issue #29755: do not add indentation on enter if indentation is already valid", func(t *testing.T) {
		e := ts(indent.AutoIndentFull, nil, "function f() {", "    const one = 1;", "    const two = 2;", "}")
		e.caret(3, 1).enter()
		e.assertLines("function f() {", "    const one = 1;", "", "    const two = 2;", "}")
	})
	t.Run("issue #36090", func(t *testing.T) {
		e := ts(indent.AutoIndentAdvanced, nil,
			"class ItemCtrl {",
			"    getPropertiesByItemId(id) {",
			"        return this.fetchItem(id)",
			"            .then(item => {",
			"                return this.getPropertiesOfItem(item);",
			"            });",
			"    }",
			"}")
		e.caret(7, 6).enter()
		e.assertLines(
			"class ItemCtrl {",
			"    getPropertiesByItemId(id) {",
			"        return this.fetchItem(id)",
			"            .then(item => {",
			"                return this.getPropertiesOfItem(item);",
			"            });",
			"    }",
			"    ",
			"}")
		e.assertCaret(8, 5)
	})
	t.Run("issue #115304: indent block comment onEnter", func(t *testing.T) {
		e := ts(indent.AutoIndentAdvanced, nil, "/** */", "function f() {}")
		e.caret(1, 4).enter()
		e.assertLines("/**", " * ", " */", "function f() {}")
		e.assertCaret(2, 4)
	})
	t.Run("issue #43244: indent when lambda arrow function is detected, outdent when end is reached", func(t *testing.T) {
		e := ts(indent.AutoIndentFull, nil, "const array = [1, 2, 3, 4, 5];", "array.map(_)")
		e.caret(2, 12).enter()
		e.assertLines("const array = [1, 2, 3, 4, 5];", "array.map(_", "    ", ")")
	})
	t.Run("issue #43244: incorrect indentation after if/for/while without braces", func(t *testing.T) {
		e := ts(indent.AutoIndentFull, nil, "function f() {", "    if (condition)", "}")
		e.caret(2, 19).enter()
		e.assertLines("function f() {", "    if (condition)", "        ", "}")
		e.insert("return;").enter()
		e.assertLines("function f() {", "    if (condition)", "        return;", "    ", "}")
	})
	t.Run("issue #208232: incorrect indentation inside of comments", func(t *testing.T) {
		toks := map[int][]indenttest.Tok{0: {{Type: comment}}, 1: {{Type: comment}}, 2: {{Type: comment}}}
		e := ts(indent.AutoIndentFull, toks, "/**", "indentation done for {", "*/")
		e.caret(2, 23).enter()
		e.assertLines("/**", "indentation done for {", "", "*/")
	})
	t.Run("issue #209802: allman style braces in JavaScript", func(t *testing.T) {
		e := ts(indent.AutoIndentFull, nil, "if (/*condition*/)")
		e.caret(1, 19).enter()
		e.assertLines("if (/*condition*/)", "    ")
		// The rest of VS Code's test types `{`, which needs auto-closing pairs (step 4.17).
	})
	t.Run("closing brace typed on its own line goes back to the indentation of the block", func(t *testing.T) {
		e := ts(indent.AutoIndentFull, nil, "function f() {", "    a();", "    ")
		e.caret(3, 5).typeText("}")
		e.assertLines("function f() {", "    a();", "}")
		e.assertCaret(3, 2)
	})
	t.Run("a closing brace on a line that was indented on purpose is left alone", func(t *testing.T) {
		e := ts(indent.AutoIndentFull, nil, "function f() {", "    a();", "  }")
		e.caret(3, 4).typeText("}")
		e.assertLines("function f() {", "    a();", "  }}")
	})
	t.Run("typing below the level full does not re-indent", func(t *testing.T) {
		e := ts(indent.AutoIndentAdvanced, nil, "function f() {", "    a();", "    ")
		e.caret(3, 5).typeText("}")
		e.assertLines("function f() {", "    a();", "    }")
	})
}

// Ported from VS Code: indentation.test.ts ('Auto Indent On Type - Ruby', '- PHP').
func TestVSCodeAutoIndentOnTypeRubyAndPHP(t *testing.T) {
	t.Run("Ruby: issue #198350: in or when incorrectly match non keywords", func(t *testing.T) {
		e := indentEd(t, indenttest.Ruby, indent.AutoIndentFull, nil, "")
		e.insert("def foo\n        i").typeText("n")
		e.assertText("def foo\n        in")
		e.typeText(" ")
		e.assertText("def foo\nin ")

		e = indentEd(t, indenttest.Ruby, indent.AutoIndentFull, nil, "")
		e.insert("  # in").typeText(" ")
		e.assertText("  # in ")
	})
	t.Run("PHP: issue #199050: should not indent after { detected in a string", func(t *testing.T) {
		toks := map[int][]indenttest.Tok{0: {{Start: 0, Type: textmate.TokenOther}, {Start: 13, Type: textmate.TokenString}, {Start: 16, Type: textmate.TokenOther}}}
		e := indentEd(t, indenttest.PHP, indent.AutoIndentFull, toks, `preg_replace('{');`)
		e.caret(1, 54).enter()
		e.assertLines(`preg_replace('{');`, "")
	})
}
