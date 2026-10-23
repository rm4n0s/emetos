package edit

import (
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/vstest"
	"github.com/rm4n0s/errors"
)

// sel4 is a selection written as VS Code's tests write it: the line and column
// (1-based, UTF-16) of the anchor and then of the active end.
type sel4 [4]int

// ed is an editor without a view: a document, the settings and the selections of
// its cursors. Commands are run on the text as it is and their result is applied
// to the document, as the editor does; positions are VS Code's.
type ed struct {
	t    *testing.T
	doc  *document.Document
	opts Options
	sels []buffer.Selection
	// closed tracks the closing characters that typing added, as the editor does.
	closed *AutoClosedTracker
}

func newEd(t *testing.T, opts Options, lines ...string) *ed {
	t.Helper()
	d := document.New(document.UntitledURI(1), strings.Join(lines, "\n"), document.NewOptions())
	return &ed{t: t, doc: d, opts: opts, sels: []buffer.Selection{buffer.NewCursor(buffer.Position{})}, closed: NewAutoClosedTracker(d, 1)}
}

// pos converts a VS Code position into the current text; a column past the end of
// its line goes to the end.
func (e *ed) pos(line, col int) buffer.Position {
	e.t.Helper()
	p, err := vstest.ToPosition(e.doc.Text(), line, col)
	if err == nil {
		return p
	}
	if appErr, _ := errors.FromError(err); appErr.HasRoute("VSColumnOutOfRange") {
		return buffer.Position{Line: line - 1, Col: len(strings.Split(e.doc.Text(), "\n")[line-1])}
	}
	e.t.Fatalf("VS Code position %d:%d: %v", line, col, err)
	return buffer.Position{}
}

func (e *ed) sel(s sel4) buffer.Selection {
	return buffer.Selection{Anchor: e.pos(s[0], s[1]), Active: e.pos(s[2], s[3])}
}

// at puts the cursors at VS Code selections.
func (e *ed) at(sels ...sel4) *ed {
	e.t.Helper()
	e.sels = e.sels[:0]
	for _, s := range sels {
		e.sels = append(e.sels, e.sel(s))
	}
	e.closed.Validate(e.sels)
	return e
}

// caret puts one cursor at a VS Code position.
func (e *ed) caret(line, col int) *ed { return e.at(sel4{line, col, line, col}) }

// editOptionsOf is how the editor records a result in the undo history.
func editOptionsOf(res Result) document.EditOptions {
	return document.EditOptions{
		Kind:  res.Kind,
		After: func([]document.Edit) []buffer.Selection { return res.Selections },
	}
}

// run runs a command and applies its result to the document.
func (e *ed) run(f func(Commands, []buffer.Selection) Result) *ed {
	e.t.Helper()
	return e.apply(f(e.commands(), e.sels))
}

// apply applies a result to the document, tracks what it auto-closed and runs its follow-up.
func (e *ed) apply(res Result) *ed {
	e.t.Helper()
	if len(res.Edits) > 0 {
		_, err := e.doc.Edit(res.Edits, document.EditOptions{
			Kind: res.Kind, Before: e.sels,
			After: func([]document.Edit) []buffer.Selection { return res.Selections },
		})
		if err != nil {
			e.t.Fatalf("applying %+v: %v", res.Edits, err)
		}
	}
	cs := NewCursors(e.opts)
	cs.SetSelections(res.Selections)
	e.sels = cs.Selections()
	e.closed.Validate(e.sels)
	e.closed.Add(res.AutoClosed)
	if res.FollowUp != nil {
		if next, ok := res.FollowUp(e.commands(), e.sels); ok {
			e.apply(next)
		}
	}
	return e
}

// typeText types text one character at a time, as the keyboard does.
func (e *ed) typeText(text string) *ed {
	e.t.Helper()
	for _, r := range text {
		e.run(func(c Commands, s []buffer.Selection) Result { return c.Type(s, string(r)) })
	}
	return e
}

// vsFromPosition is the VS Code line and column of a position of text.
func vsFromPosition(text string, p buffer.Position) (int, int) {
	l, c, _ := vstest.FromPosition(text, p)
	return l, c
}

func itoa(n int) string { return strconv.Itoa(n) }

func (e *ed) tab() *ed     { return e.run(Commands.Tab) }
func (e *ed) indent() *ed  { return e.run(Commands.Indent) }
func (e *ed) outdent() *ed { return e.run(Commands.Outdent) }
func (e *ed) back() *ed    { return e.run(Commands.DeleteLeft) }
func (e *ed) del() *ed     { return e.run(Commands.DeleteRight) }

func (e *ed) undo() *ed {
	e.t.Helper()
	res, err := e.doc.Undo()
	if err != nil {
		e.t.Fatal(err)
	}
	if len(res.Selections) > 0 {
		e.sels = res.Selections
	}
	return e
}

func (e *ed) redo() *ed {
	e.t.Helper()
	res, err := e.doc.Redo()
	if err != nil {
		e.t.Fatal(err)
	}
	if len(res.Selections) > 0 {
		e.sels = res.Selections
	}
	return e
}

// line is the content of a line, counted from 1 as VS Code's tests do.
func (e *ed) line(n int) string { return strings.Split(e.doc.Text(), "\n")[n-1] }

func (e *ed) assertText(want string) {
	e.t.Helper()
	if got := e.doc.Text(); got != want {
		e.t.Errorf("text = %q, want %q", got, want)
	}
}

func (e *ed) assertLine(n int, want string) {
	e.t.Helper()
	if got := e.line(n); got != want {
		e.t.Errorf("line %d = %q, want %q", n, got, want)
	}
}

func (e *ed) assertSels(want ...sel4) {
	e.t.Helper()
	var got []sel4
	for _, s := range e.sels {
		a, ac, _ := vstest.FromPosition(e.doc.Text(), s.Anchor)
		l, c, _ := vstest.FromPosition(e.doc.Text(), s.Active)
		got = append(got, sel4{a, ac, l, c})
	}
	if len(got) != len(want) {
		e.t.Fatalf("selections = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			e.t.Fatalf("selections = %v, want %v", got, want)
		}
	}
}

func (e *ed) assertCaret(line, col int) {
	e.t.Helper()
	e.assertSels(sel4{line, col, line, col})
}

func tabsOptions() Options {
	o := DefaultOptions()
	o.InsertSpaces = false
	return o
}

// Ported from VS Code: reference/vscode/src/vs/editor/test/browser/controller/cursor.test.ts
// ('issue #12950', 'issue #3463', 'issue #4312', 'Bug #16657', 'Cursor honors
// insertSpaces configuration on tab', 'issue microsoft/monaco-editor#443').
func TestVSCodeTab(t *testing.T) {
	t.Run("issue #12950: emoji typed", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "some lines", "and more lines", "just some text").caret(3, 1)
		e.typeText("😍")
		e.assertText("some lines\nand more lines\n😍just some text")
		e.assertCaret(3, 3)
	})
	t.Run("issue #3463: pressing tab adds spaces, but not as many as for a tab", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "function a() {", "\tvar a = {", "\t\tx: 3", "\t};", "}").caret(3, 2)
		e.tab()
		e.assertLine(3, "\t    \tx: 3")
	})
	t.Run("issue #4312: a tab over a sequence of spaces", func(t *testing.T) {
		e := newEd(t, tabsOptions(), "var foo = 123;       // this is a comment", "var bar = 4;       // another comment").
			at(sel4{1, 15, 1, 22})
		e.tab()
		e.assertLine(1, "var foo = 123;\t// this is a comment")
	})
	t.Run("Bug #16657: Tab on empty line of zero indentation", func(t *testing.T) {
		e := newEd(t, tabsOptions(), "function baz() {", "\tfunction hello() { // something here", "\t", "", "\t}", "}", "").caret(7, 1)
		e.tab()
		e.assertLine(7, "\t")
		e.assertCaret(7, 2)
	})
	t.Run("issue monaco-editor#443: indenting a whole line keeps the selection start", func(t *testing.T) {
		e := newEd(t, tabsOptions(), "Hello world!", "another line").at(sel4{1, 1, 1, 13})
		e.tab()
		e.assertSels(sel4{1, 1, 1, 14})
	})
	t.Run("Cursor honors insertSpaces configuration on tab", func(t *testing.T) {
		opts := DefaultOptions()
		opts.TabSize = 13
		for _, tt := range []struct {
			col  int
			want string
		}{
			{1, "             My Second Line123"},
			{2, "M            y Second Line123"},
			{3, "My            Second Line123"},
			{4, "My           Second Line123"},
			{5, "My S         econd Line123"},
			{13, "My Second Li ne123"},
			{14, "My Second Lin             e123"},
		} {
			e := newEd(t, opts, "    \tMy First Line\t ", "My Second Line123", "    Third Line", "", "1").caret(2, tt.col)
			e.tab()
			e.assertLine(2, tt.want)
			e.undo()
			e.assertLine(2, "My Second Line123")
		}
	})
}

// Ported from VS Code: cursor.test.ts ('bug #16815', 'Bug #18293', 'issue #95591');
// the first needs no language here since the line is not misaligned after the
// first line.
func TestVSCodeOutdent(t *testing.T) {
	t.Run("bug #16815: Shift+Tab goes back to the tab stop", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "     function baz() {").caret(1, 6)
		e.outdent()
		e.assertLine(1, "    function baz() {")
		e.assertCaret(1, 5)
	})
	t.Run("Bug #18293: outdent a white space line", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "      ").caret(1, 7)
		e.outdent()
		e.assertLine(1, "    ")
		e.assertCaret(1, 5)
	})
	t.Run("issue #95591: unindenting does not move the cursor to the line start (no tab stops)", func(t *testing.T) {
		opts := DefaultOptions()
		opts.UseTabStops = false
		e := newEd(t, opts, "        ").caret(1, 9)
		e.outdent()
		e.assertLine(1, "    ")
		e.assertCaret(1, 5)
	})
}

// Ported from VS Code: cursor.test.ts ('Cursor honors insertSpaces configuration
// on new line', 'issue #148256' twice).
func TestVSCodeNewLine(t *testing.T) {
	t.Run("insertSpaces on a new line", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "    \tMy First Line\t ", "\tMy Second Line", "    Third Line", "", "1").caret(1, 21)
		e.typeText("\n")
		e.assertLine(1, "    \tMy First Line\t ")
		e.assertLine(2, "        ")
	})
	t.Run("issue #148256: bad indent with insertSpaces: true", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "  \t").caret(1, 4)
		e.typeText("\n")
		e.assertText("  \t\n    ")
	})
	t.Run("issue #148256: bad indent with insertSpaces: false", func(t *testing.T) {
		e := newEd(t, tabsOptions(), "  \t").caret(1, 4)
		e.typeText("\n")
		e.assertText("  \t\n\t")
	})
}

func TestNewLineKeepsIndentationBeforeTheCursor(t *testing.T) {
	tests := []struct {
		name string
		opts Options
		text string
		sel  sel4
		want string
		at   [2]int
	}{
		{"in the middle of the text", DefaultOptions(), "    abc", sel4{1, 7, 1, 7}, "    ab\n    c", [2]int{2, 5}},
		{"inside the indentation", DefaultOptions(), "    abc", sel4{1, 3, 1, 3}, "  \n    abc", [2]int{2, 3}},
		{"selection is replaced", DefaultOptions(), "  abc def", sel4{1, 6, 1, 8}, "  abc\n  ef", [2]int{2, 3}},
		{"tabs", tabsOptions(), "\t\tx", sel4{1, 4, 1, 4}, "\t\tx\n\t\t", [2]int{2, 3}},
		{"spaces become tabs", tabsOptions(), "        x", sel4{1, 10, 1, 10}, "        x\n\t\t", [2]int{2, 3}},
		{"no indentation", DefaultOptions(), "abc", sel4{1, 2, 1, 2}, "a\nbc", [2]int{2, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newEd(t, tt.opts, tt.text).at(tt.sel)
			e.typeText("\n")
			e.assertText(tt.want)
			e.assertCaret(tt.at[0], tt.at[1])
		})
	}
}

func TestTypeNormalizesLineBreaks(t *testing.T) {
	e := newEd(t, DefaultOptions(), "ab").caret(1, 2)
	e.run(func(c Commands, s []buffer.Selection) Result { return c.Type(s, "x\r\ny\rz") })
	e.assertText("ax\ny\nzb")
	e.assertCaret(3, 2)
}

// Ported from VS Code: cursor.test.ts ('issue #37967: problem replacing
// consecutive characters'; the quote typed here has no auto-closing, which
// needs a language).
func TestVSCodeTypeAtConsecutiveCursors(t *testing.T) {
	opts := DefaultOptions()
	opts.MultiCursorMergeOverlapping = false
	e := newEd(t, opts, `const a = "foo";`, `const b = ""`).
		at(sel4{1, 12, 1, 12}, sel4{1, 16, 1, 16}, sel4{2, 12, 2, 12}, sel4{2, 13, 2, 13})
	e.back()
	e.assertSels(sel4{1, 11, 1, 11}, sel4{1, 14, 1, 14}, sel4{2, 11, 2, 11}, sel4{2, 11, 2, 11})
	e.typeText("'")
	e.assertLine(1, `const a = 'foo';`)
	e.assertLine(2, `const b = ''`)
}

// Ported from VS Code: cursor.test.ts ('there is a single undo stop for
// consecutive whitespaces', 'there is no undo stop after a single whitespace',
// 'inserts undo stop when typing space', 'issue #93585: Undo multi cursor edit
// corrupts document').
func TestVSCodeTypingUndoStops(t *testing.T) {
	t.Run("a single undo stop for consecutive whitespaces", func(t *testing.T) {
		e := newEd(t, tabsOptions(), "").typeText("ab  cd")
		e.assertText("ab  cd")
		e.undo().assertText("ab  ")
		e.undo().assertText("ab")
		e.undo().assertText("")
	})
	t.Run("no undo stop after a single whitespace", func(t *testing.T) {
		e := newEd(t, tabsOptions(), "").typeText("ab cd")
		e.assertText("ab cd")
		e.undo().assertText("ab")
		e.undo().assertText("")
	})
	t.Run("an undo stop when typing space", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "A  line", "Another line").caret(1, 3).typeText("first and interesting")
		e.assertLine(1, "A first and interesting line")
		e.assertCaret(1, 24)
		e.undo()
		e.assertLine(1, "A first and line")
		e.assertCaret(1, 12)
		e.undo()
		e.assertLine(1, "A first line")
		e.assertCaret(1, 8)
		e.undo()
		e.assertLine(1, "A  line")
		e.assertCaret(1, 3)
	})
	t.Run("issue #93585: undo a multi cursor edit", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "hello world", "hello world").at(sel4{2, 7, 2, 12}, sel4{1, 7, 1, 12})
		e.typeText("no")
		e.assertText("hello no\nhello no")
		e.undo()
		e.assertText("hello world\nhello world")
	})
}

// Ported from VS Code: cursor.test.ts, the 'Overtype Mode' suite ('simple type',
// 'multi-line selection type' and the first half of 'composition type'; paste is
// a later step).
func TestVSCodeOvertype(t *testing.T) {
	opts := tabsOptions()
	opts.Overtype = true
	t.Run("simple type", func(t *testing.T) {
		e := newEd(t, opts, "123456789", "123456789").caret(1, 3)
		e.run(func(c Commands, s []buffer.Selection) Result { return c.Type(s, "a") })
		e.assertText("12a456789\n123456789")
		e.caret(1, 9)
		e.run(func(c Commands, s []buffer.Selection) Result { return c.Type(s, "bbb") })
		e.assertText("12a45678bbb\n123456789")
	})
	t.Run("multi-line selection type", func(t *testing.T) {
		e := newEd(t, opts, "123456789", "123456789").at(sel4{1, 5, 2, 3})
		e.run(func(c Commands, s []buffer.Selection) Result { return c.Type(s, "cc") })
		e.assertText("1234cc456789")
	})
	t.Run("composition type does not overtype", func(t *testing.T) {
		e := newEd(t, opts, "123456789", "123456789").caret(1, 5)
		e.run(func(c Commands, s []buffer.Selection) Result { return c.CompositionType(s, "セ", 0, 0, 0) })
		e.assertText("1234セ56789\n123456789")
	})
	t.Run("overtype stops at the end of the line", func(t *testing.T) {
		e := newEd(t, opts, "ab", "cd").caret(1, 2)
		e.run(func(c Commands, s []buffer.Selection) Result { return c.Type(s, "xyz") })
		e.assertText("axyz\ncd")
	})
	t.Run("an emoji replaces two UTF-16 units", func(t *testing.T) {
		e := newEd(t, opts, "abcdef").caret(1, 2)
		e.run(func(c Commands, s []buffer.Selection) Result { return c.Type(s, "😍") })
		e.assertText("a😍def")
	})
}

func TestCompositionType(t *testing.T) {
	comp := func(text string, prev, next, delta int) func(Commands, []buffer.Selection) Result {
		return func(c Commands, s []buffer.Selection) Result { return c.CompositionType(s, text, prev, next, delta) }
	}
	t.Run("replaces the characters around the cursor", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "hello wörld").caret(1, 9)
		e.run(comp("O", 1, 0, 0))
		e.assertText("hello wOrld")
		e.assertCaret(1, 9)
	})
	t.Run("position delta moves the cursor", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "abcdef").caret(1, 4)
		e.run(comp("XY", 0, 0, -1))
		e.assertText("abcXYdef")
		e.assertCaret(1, 5)
		e.run(comp("Z", 0, 0, 2))
		e.assertText("abcXZYdef")
		e.assertCaret(1, 8)
	})
	t.Run("counts are clamped to the line", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "ab", "cd").caret(1, 2)
		e.run(comp("Z", 10, 10, 0))
		e.assertText("Z\ncd")
	})
	t.Run("a selection is left alone", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "abc").at(sel4{1, 1, 1, 3})
		e.run(comp("Z", 0, 0, 0))
		e.assertText("abc")
		e.assertSels(sel4{1, 1, 1, 3})
	})
	t.Run("several cursors", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "aXb", "cXd").at(sel4{1, 3, 1, 3}, sel4{2, 3, 2, 3})
		e.run(comp("Y", 1, 0, 0))
		e.assertText("aYb\ncYd")
		e.assertSels(sel4{1, 3, 1, 3}, sel4{2, 3, 2, 3})
	})
	t.Run("typing kind joins the typing group", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "").typeText("ab")
		e.run(comp("c", 0, 0, 0))
		e.undo()
		e.assertText("")
	})
}

func TestTypeWithSeveralCursors(t *testing.T) {
	t.Run("same line, shifted columns", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "a b c").at(sel4{1, 2, 1, 2}, sel4{1, 4, 1, 4}, sel4{1, 6, 1, 6})
		e.typeText("-")
		e.assertText("a- b- c-")
		e.assertSels(sel4{1, 3, 1, 3}, sel4{1, 6, 1, 6}, sel4{1, 9, 1, 9})
	})
	t.Run("new lines on several lines", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "  a", "  b").at(sel4{1, 4, 1, 4}, sel4{2, 4, 2, 4})
		e.typeText("\n")
		e.assertText("  a\n  \n  b\n  ")
		e.assertSels(sel4{2, 3, 2, 3}, sel4{4, 3, 4, 3})
	})
	t.Run("cursor order is kept", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "ab", "cd").at(sel4{2, 2, 2, 2}, sel4{1, 2, 1, 2})
		e.typeText("-")
		e.assertText("a-b\nc-d")
		e.assertSels(sel4{2, 3, 2, 3}, sel4{1, 3, 1, 3})
	})
	t.Run("a selection replaced and a caret after it", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "hello world").at(sel4{1, 1, 1, 6}, sel4{1, 12, 1, 12})
		e.typeText("!")
		e.assertText("! world!")
		e.assertSels(sel4{1, 2, 1, 2}, sel4{1, 9, 1, 9})
	})
}

func TestIndentAndOutdent(t *testing.T) {
	t.Run("indent several lines keeps the selection on the same text", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "one", "two", "three").at(sel4{1, 2, 3, 3})
		e.indent()
		e.assertText("    one\n    two\n    three")
		e.assertSels(sel4{1, 6, 3, 7})
		e.outdent()
		e.assertText("one\ntwo\nthree")
		e.assertSels(sel4{1, 2, 3, 3})
	})
	t.Run("a selection that ends at the start of a line leaves that line out", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "one", "two", "three").at(sel4{1, 1, 3, 1})
		e.indent()
		e.assertText("    one\n    two\nthree")
	})
	t.Run("empty lines are not indented in a multi-line selection", func(t *testing.T) {
		e := newEd(t, tabsOptions(), "one", "", "three").at(sel4{1, 1, 3, 2})
		e.indent()
		e.assertText("\tone\n\n\tthree")
	})
	t.Run("outdent takes at most one level", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "        x", "      y", "z").at(sel4{1, 1, 3, 2})
		e.outdent()
		e.assertText("    x\n    y\nz")
	})
	t.Run("outdent with tabs and no tab stops", func(t *testing.T) {
		opts := tabsOptions()
		opts.UseTabStops = false
		e := newEd(t, opts, "\t\tx", "  \ty", "   z").at(sel4{1, 1, 3, 2})
		e.outdent()
		e.assertText("\tx\ny\nz")
	})
	t.Run("indent with no tab stops", func(t *testing.T) {
		opts := DefaultOptions()
		opts.UseTabStops = false
		e := newEd(t, opts, "  x", "y").at(sel4{1, 1, 2, 2})
		e.indent()
		e.assertText("      x\n    y")
	})
	t.Run("indent of an empty line puts the cursor after the indentation", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "a", "").caret(2, 1)
		e.indent()
		e.assertText("a\n    ")
		e.assertCaret(2, 5)
	})
	t.Run("tab on a blank line goes to a whole indentation", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "a", "  ").caret(2, 3)
		e.tab()
		e.assertText("a\n    ")
		e.assertCaret(2, 5)
		e.tab()
		e.assertText("a\n        ")
	})
	t.Run("tab with a selection inside a line is replaced", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "abcdef").at(sel4{1, 2, 1, 4})
		e.tab()
		e.assertText("a   def")
		e.assertCaret(1, 5)
	})
	t.Run("two cursors on lines of a multi-cursor tab", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "ab", "cd").at(sel4{1, 2, 1, 2}, sel4{2, 2, 2, 2})
		e.tab()
		e.assertText("a   b\nc   d")
		e.assertSels(sel4{1, 5, 1, 5}, sel4{2, 5, 2, 5})
	})
}

func TestTypingUndoAndRedoRestoreCursors(t *testing.T) {
	e := newEd(t, DefaultOptions(), "hello").caret(1, 6)
	e.typeText("!!")
	e.assertText("hello!!")
	e.undo()
	e.assertText("hello")
	e.assertCaret(1, 6)
	e.redo()
	e.assertText("hello!!")
	e.assertCaret(1, 8)
}
