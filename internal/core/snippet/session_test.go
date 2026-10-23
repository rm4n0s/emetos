package snippet

import (
	"fmt"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/edit"
	"github.com/rm4n0s/errors"
)

// Ported from VS Code: src/vs/editor/contrib/snippet/test/browser/snippetSession.test.ts
// (the positions of the tests are 1-based lines and columns of ASCII text, as VS Code writes them;
// at, sel and rng make the positions of Emetos from them)

const sessionText = "function foo() {\n    console.log(a);\n}"

func rng(l1, c1, l2, c2 int) buffer.Range { return buffer.Range{Start: at(l1, c1), End: at(l2, c2)} }

// rig is an editor made of a document and cursors, with the two cursors of the VS Code tests.
type rig struct {
	t   *testing.T
	doc *document.Document
	cur *edit.Cursors
	ed  Editor
	env Env
}

func newRig(t *testing.T, text string, sels ...buffer.Selection) *rig {
	t.Helper()
	doc := document.New(document.UntitledURI(1), text, document.NewOptions())
	cur := edit.NewCursors(edit.DefaultOptions())
	if len(sels) == 0 {
		sels = []buffer.Selection{sel(1, 1, 1, 1), sel(2, 5, 2, 5)}
	}
	cur.SetSelections(sels)
	return &rig{t: t, doc: doc, cur: cur, ed: Editor{Doc: doc, Cursors: cur}}
}

func (r *rig) value() string { return r.doc.Text() }

func (r *rig) setSelections(sels ...buffer.Selection) { r.cur.SetSelections(sels) }

func (r *rig) assertSelections(want ...buffer.Selection) {
	r.t.Helper()
	got := r.cur.Selections()
	if !buffer.SelectionsEqual(got, want) {
		r.t.Errorf("selections\n got %v\nwant %v", got, want)
	}
}

func (r *rig) editOptions() edit.Options {
	o := edit.DefaultOptions()
	o.TabSize, o.InsertSpaces = r.doc.Indentation()
	return o
}

// apply applies the result of an editing command, as the editor does.
func (r *rig) apply(res edit.Result) {
	r.t.Helper()
	if _, err := r.doc.Edit(res.Edits, document.EditOptions{Kind: res.Kind, Before: r.cur.Selections(), After: func([]document.Edit) []buffer.Selection { return res.Selections }}); err != nil {
		r.t.Fatal(err)
	}
	if len(res.Selections) > 0 {
		r.cur.SetSelections(res.Selections)
	}
}

// typeText types a text a character at a time, as VS Code's type command does.
func (r *rig) typeText(text string) {
	r.t.Helper()
	for _, ch := range text {
		r.apply(edit.NewCommands(edit.NewModel(r.doc.Snapshot()), r.editOptions()).Type(r.cur.Selections(), string(ch)))
	}
}

func (r *rig) cut() {
	r.t.Helper()
	r.apply(edit.NewCommands(edit.NewModel(r.doc.Snapshot()), r.editOptions()).Cut(r.cur.Selections(), true))
}

func (r *rig) session(template string) *Session {
	return NewSession(r.ed, r.env, template, DefaultInsertOptions())
}

// insert inserts a template and fails the test when it cannot.
func (r *rig) insert(template string) *Session {
	r.t.Helper()
	s := r.session(template)
	if err := s.Insert(); err != nil {
		r.t.Fatal(err)
	}
	return s
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestSessionNormalizeWhitespace(t *testing.T) {
	r := newRig(t, sessionText)
	cases := []struct {
		line, col int
		in, want  string
	}{
		{1, 1, "foo", "foo"},
		{1, 1, "foo\rbar", "foo\nbar"},
		{2, 5, "foo\r\tbar", "foo\n        bar"},
		{2, 3, "foo\r\tbar", "foo\n    bar"},
		{2, 5, "foo\r\tbar\nfoo", "foo\n        bar\n    foo"},
		// #46266: choice elements that span several lines
		{2, 5, "a\nb${1|foo,\nbar|}", "a\n    b${1|foo,\nbar|}"},
	}
	for _, tc := range cases {
		s := parse(tc.in)
		AdjustWhitespace(r.doc, at(tc.line, tc.col), true, s, nil)
		if got := s.ToTextmateString(); got != tc.want {
			t.Errorf("%q at %d,%d: %q, want %q", tc.in, tc.line, tc.col, got, tc.want)
		}
	}
}

func TestSessionAdjustSelection(t *testing.T) {
	snap := newRig(t, sessionText).doc.Snapshot()
	cases := []struct {
		sel           buffer.Selection
		before, after int
		want          buffer.Range
	}{
		{sel(1, 2, 1, 2), 1, 0, rng(1, 1, 1, 2)},
		{sel(1, 2, 1, 2), 1111, 0, rng(1, 1, 1, 2)},
		{sel(1, 2, 1, 2), 0, 10, rng(1, 2, 1, 12)},
		{sel(1, 2, 1, 2), 0, 10111, rng(1, 2, 1, 17)},
	}
	for _, tc := range cases {
		if got := AdjustSelection(snap, tc.sel, tc.before, tc.after).Range(); got != tc.want {
			t.Errorf("%v (%d, %d): %v, want %v", tc.sel, tc.before, tc.after, got, tc.want)
		}
	}
	// #31199: a selection from left to right keeps its direction
	snap = newRig(t, "console.log").doc.Snapshot()
	if got := AdjustSelection(snap, sel(1, 12, 1, 9), 3, 0); got != sel(1, 9, 1, 6) {
		t.Errorf("backward: %v", got)
	}
	if got := AdjustSelection(snap, sel(1, 9, 1, 12), 3, 0); got != sel(1, 9, 1, 12) {
		t.Errorf("forward: %v", got)
	}
}

func TestSessionTextEditsAndSelection(t *testing.T) {
	r := newRig(t, sessionText)
	s := r.insert("foo${1:bar}foo$0")
	if got := r.value(); got != "foobarfoofunction foo() {\n    foobarfooconsole.log(a);\n}" {
		t.Errorf("text %q", got)
	}
	r.assertSelections(sel(1, 4, 1, 7), sel(2, 8, 2, 11))
	must(t, s.Next())
	r.assertSelections(sel(1, 10, 1, 10), sel(2, 14, 2, 14))
}

func TestSessionTextEditWithReversedSelection(t *testing.T) {
	r := newRig(t, sessionText)
	s := r.session("${1:bar}$0")
	r.setSelections(sel(2, 5, 2, 5), sel(1, 1, 1, 1))
	must(t, s.Insert())
	if got := r.value(); got != "barfunction foo() {\n    barconsole.log(a);\n}" {
		t.Errorf("text %q", got)
	}
	r.assertSelections(sel(2, 5, 2, 8), sel(1, 1, 1, 4))
}

func TestSessionRepeatedTabstops(t *testing.T) {
	r := newRig(t, sessionText)
	s := r.insert("${1:abc}foo${1:abc}$0")
	r.assertSelections(sel(1, 1, 1, 4), sel(1, 7, 1, 10), sel(2, 5, 2, 8), sel(2, 11, 2, 14))
	must(t, s.Next())
	r.assertSelections(sel(1, 10, 1, 10), sel(2, 14, 2, 14))
}

func TestSessionJustText(t *testing.T) {
	r := newRig(t, sessionText)
	r.insert("foobar")
	if got := r.value(); got != "foobarfunction foo() {\n    foobarconsole.log(a);\n}" {
		t.Errorf("text %q", got)
	}
	r.assertSelections(sel(1, 7, 1, 7), sel(2, 11, 2, 11))
}

func TestSessionNewlines(t *testing.T) {
	t.Run("selections and new text with newlines", func(t *testing.T) {
		r := newRig(t, sessionText)
		s := r.insert("foo\n\t${1:bar}\n$0")
		if got := r.value(); got != "foo\n    bar\nfunction foo() {\n    foo\n        bar\n    console.log(a);\n}" {
			t.Errorf("text %q", got)
		}
		r.assertSelections(sel(2, 5, 2, 8), sel(5, 9, 5, 12))
		must(t, s.Next())
		r.assertSelections(sel(3, 1, 3, 1), sel(6, 5, 6, 5))
	})
	t.Run("no whitespace adjust", func(t *testing.T) {
		r := newRig(t, sessionText, sel(2, 5, 2, 5))
		opts := DefaultInsertOptions()
		opts.AdjustWhitespace = false
		must(t, NewSession(r.ed, r.env, "abc\n    foo\n        bar\n$0", opts).Insert())
		if got := r.value(); got != "function foo() {\n    abc\n    foo\n        bar\nconsole.log(a);\n}" {
			t.Errorf("text %q", got)
		}
	})
}

func TestSessionNextPrev(t *testing.T) {
	r := newRig(t, sessionText)
	s := r.insert("f$1oo${2:bar}foo$0")
	r.assertSelections(sel(1, 2, 1, 2), sel(2, 6, 2, 6)) // $2... the first group is $1
	must(t, s.Next())
	r.assertSelections(sel(1, 4, 1, 7), sel(2, 8, 2, 11))
	must(t, s.Prev())
	r.assertSelections(sel(1, 2, 1, 2), sel(2, 6, 2, 6))
	must(t, s.Next())
	r.assertSelections(sel(1, 4, 1, 7), sel(2, 8, 2, 11))
	must(t, s.Next())
	r.assertSelections(sel(1, 10, 1, 10), sel(2, 14, 2, 14))
}

func TestSessionSelectionsAndTyping(t *testing.T) {
	r := newRig(t, sessionText)
	s := r.insert("f${1:oo}_$2_$0")
	r.typeText("X")
	must(t, s.Next())
	r.typeText("bar")
	// go back to ${2:oo} which is now just 'X'... (VS Code's comment; the tab stops are $1 and $2)
	must(t, s.Prev())
	r.assertSelections(sel(1, 2, 1, 3), sel(2, 6, 2, 7))
	// go forward to $2 which is now 'bar'
	must(t, s.Next())
	r.assertSelections(sel(1, 4, 1, 7), sel(2, 8, 2, 11))
	// go to the final tab stop
	must(t, s.Next())
	if got := r.value(); got != "fX_bar_function foo() {\n    fX_bar_console.log(a);\n}" {
		t.Errorf("text %q", got)
	}
	r.assertSelections(sel(1, 8, 1, 8), sel(2, 12, 2, 12))
}

func TestSessionIntoNonEmptySelection(t *testing.T) {
	t.Run("shorter snippet", func(t *testing.T) {
		r := newRig(t, "foo_bar_foo", sel(1, 1, 1, 4), sel(1, 9, 1, 12))
		r.insert("x$0")
		if got := r.value(); got != "x_bar_x" {
			t.Errorf("text %q", got)
		}
		r.assertSelections(sel(1, 2, 1, 2), sel(1, 8, 1, 8))
	})
	t.Run("longer snippet", func(t *testing.T) {
		r := newRig(t, "foo_bar_foo", sel(1, 1, 1, 4), sel(1, 9, 1, 12))
		r.insert("LONGER$0")
		if got := r.value(); got != "LONGER_bar_LONGER" {
			t.Errorf("text %q", got)
		}
		r.assertSelections(sel(1, 7, 1, 7), sel(1, 18, 1, 18))
	})
}

func TestSessionFinalTabstopDoesNotGrow(t *testing.T) {
	r := newRig(t, "foo_zzz_foo", sel(1, 5, 1, 8))
	s := r.insert("$1bar$0")
	r.assertSelections(sel(1, 5, 1, 5))
	r.typeText("foo-")
	must(t, s.Next())
	if got := r.value(); got != "foo_foo-bar_foo" {
		t.Errorf("text %q", got)
	}
	r.assertSelections(sel(1, 12, 1, 12))
	r.typeText("XXX")
	if got := r.value(); got != "foo_foo-barXXX_foo" {
		t.Errorf("text %q", got)
	}
	must(t, s.Prev())
	r.assertSelections(sel(1, 5, 1, 9))
	must(t, s.Next())
	r.assertSelections(sel(1, 15, 1, 15))
}

func TestSessionDoesNotMergeTouchingTabstops(t *testing.T) {
	t.Run("1/2", func(t *testing.T) {
		r := newRig(t, sessionText)
		s := r.insert("$1$2$3$0")
		r.assertSelections(sel(1, 1, 1, 1), sel(2, 5, 2, 5))
		for range 3 {
			must(t, s.Next())
			r.assertSelections(sel(1, 1, 1, 1), sel(2, 5, 2, 5))
		}
		for range 3 {
			must(t, s.Prev())
		}
		r.assertSelections(sel(1, 1, 1, 1), sel(2, 5, 2, 5))
		r.typeText("111")
		must(t, s.Next())
		r.typeText("222")
		must(t, s.Next())
		r.typeText("333")
		must(t, s.Next())
		if got := r.value(); got != "111222333function foo() {\n    111222333console.log(a);\n}" {
			t.Errorf("text %q", got)
		}
		r.assertSelections(sel(1, 10, 1, 10), sel(2, 14, 2, 14))
		must(t, s.Prev())
		r.assertSelections(sel(1, 7, 1, 10), sel(2, 11, 2, 14))
		must(t, s.Prev())
		r.assertSelections(sel(1, 4, 1, 7), sel(2, 8, 2, 11))
		must(t, s.Prev())
		r.assertSelections(sel(1, 1, 1, 4), sel(2, 5, 2, 8))
	})
	t.Run("2/2", func(t *testing.T) {
		r := newRig(t, sessionText)
		s := r.insert("$1$2$3$0")
		r.assertSelections(sel(1, 1, 1, 1), sel(2, 5, 2, 5))
		r.typeText("111")
		must(t, s.Next())
		r.assertSelections(sel(1, 4, 1, 4), sel(2, 8, 2, 8))
		r.typeText("222")
		must(t, s.Next())
		r.assertSelections(sel(1, 7, 1, 7), sel(2, 11, 2, 11))
		r.typeText("333")
		must(t, s.Next())
		if !s.IsAtLastPlaceholder() {
			t.Error("not at the last placeholder")
		}
	})
}

func TestSessionGracefullyMovesOverFinalTabstop(t *testing.T) {
	r := newRig(t, sessionText)
	s := r.insert("${1}bar$0")
	if s.IsAtLastPlaceholder() {
		t.Error("at the last placeholder at first")
	}
	r.assertSelections(sel(1, 1, 1, 1), sel(2, 5, 2, 5))
	must(t, s.Next())
	if !s.IsAtLastPlaceholder() {
		t.Error("not at the last placeholder")
	}
	r.assertSelections(sel(1, 4, 1, 4), sel(2, 8, 2, 8))
	must(t, s.Next())
	if !s.IsAtLastPlaceholder() {
		t.Error("not at the last placeholder after another next")
	}
	r.assertSelections(sel(1, 4, 1, 4), sel(2, 8, 2, 8))
}

func TestSessionOverwritingNestedPlaceholder(t *testing.T) {
	r := newRig(t, sessionText)
	s := r.insert(`log(${1:"$2"});$0`)
	r.assertSelections(sel(1, 5, 1, 7), sel(2, 9, 2, 11))
	r.typeText("XXX")
	if got := r.value(); got != "log(XXX);function foo() {\n    log(XXX);console.log(a);\n}" {
		t.Errorf("text %q", got)
	}
	must(t, s.Next())
	if s.IsAtLastPlaceholder() {
		t.Error("at the last placeholder")
	}
	must(t, s.Next())
	if !s.IsAtLastPlaceholder() {
		t.Error("not at the last placeholder")
	}
	r.assertSelections(sel(1, 10, 1, 10), sel(2, 14, 2, 14))
}

func TestSessionSelectionsAndSnippetRanges(t *testing.T) {
	r := newRig(t, sessionText)
	s := r.insert("${1:foo}farboo${2:bar}$0")
	if got := r.value(); got != "foofarboobarfunction foo() {\n    foofarboobarconsole.log(a);\n}" {
		t.Errorf("text %q", got)
	}
	r.assertSelections(sel(1, 1, 1, 4), sel(2, 5, 2, 8))
	within := func(want bool, why string) {
		t.Helper()
		if got := s.IsSelectionWithinPlaceholders(); got != want {
			t.Errorf("%s: within = %v, want %v", why, got, want)
		}
	}
	within(true, "at the start")
	r.setSelections(sel(1, 1, 1, 1))
	within(false, "one cursor too few")
	r.setSelections(sel(1, 6, 1, 6), sel(2, 10, 2, 10))
	within(false, "in the snippet, outside the placeholders")
	r.setSelections(sel(1, 6, 1, 6), sel(2, 10, 2, 10), sel(1, 1, 1, 1))
	within(false, "three cursors")
	r.setSelections(sel(1, 6, 1, 6), sel(2, 10, 2, 10), sel(2, 20, 2, 21))
	within(false, "a cursor away")
	// reset the selection to the placeholder
	must(t, s.Next())
	within(true, "after next")
	r.assertSelections(sel(1, 10, 1, 13), sel(2, 14, 2, 17))
	must(t, s.Next())
	within(true, "at the end")
	if !s.IsAtLastPlaceholder() {
		t.Error("not at the last placeholder")
	}
	r.assertSelections(sel(1, 13, 1, 13), sel(2, 17, 2, 17))
}

func TestSessionNestedSessions(t *testing.T) {
	r := newRig(t, "", sel(1, 1, 1, 1))
	first := r.insert("foo${2:bar}foo$0")
	if got := r.value(); got != "foobarfoo" {
		t.Errorf("text %q", got)
	}
	r.assertSelections(sel(1, 4, 1, 7))

	second := r.insert("ba${1:zzzz}$0")
	if got := r.value(); got != "foobazzzzfoo" {
		t.Errorf("text %q", got)
	}
	r.assertSelections(sel(1, 6, 1, 10))

	must(t, second.Next())
	if !second.IsAtLastPlaceholder() {
		t.Error("the second is not at its last placeholder")
	}
	r.assertSelections(sel(1, 10, 1, 10))

	must(t, first.Next())
	if !first.IsAtLastPlaceholder() {
		t.Error("the first is not at its last placeholder")
	}
	r.assertSelections(sel(1, 13, 1, 13))
}

func TestSessionTypingAtTheFinalTabstop(t *testing.T) {
	t.Run("at the final tab stop", func(t *testing.T) {
		r := newRig(t, sessionText)
		s := r.insert("farboo$0")
		if !s.IsAtLastPlaceholder() || s.IsSelectionWithinPlaceholders() {
			t.Error("a snippet with only the final tab stop")
		}
		r.typeText("XXX")
		if s.IsSelectionWithinPlaceholders() {
			t.Error("within placeholders after typing")
		}
	})
	t.Run("at the beginning", func(t *testing.T) {
		r := newRig(t, sessionText, sel(1, 2, 1, 2))
		s := r.insert("farboo$0")
		r.setSelections(sel(1, 2, 1, 2))
		if s.IsSelectionWithinPlaceholders() || !s.IsAtLastPlaceholder() {
			t.Error("after moving back")
		}
		r.typeText("XXX")
		if line, _ := r.doc.Snapshot().LineContent(0); line != "fXXXfarboounction foo() {" {
			t.Errorf("line %q", line)
		}
		if s.IsSelectionWithinPlaceholders() {
			t.Error("within placeholders after typing")
		}
		must(t, s.Next())
		r.assertSelections(sel(1, 11, 1, 11))
	})
}

func TestSessionTypingWithNestedPlaceholder(t *testing.T) {
	r := newRig(t, sessionText, sel(1, 1, 1, 1))
	s := r.insert("This ${1:is ${2:nested}}.$0")
	r.assertSelections(sel(1, 6, 1, 15))
	must(t, s.Next())
	r.assertSelections(sel(1, 9, 1, 15))
	r.cut()
	r.assertSelections(sel(1, 9, 1, 9))
	r.typeText("XXX")
	must(t, s.Prev())
	r.assertSelections(sel(1, 6, 1, 12))
}

func TestSessionVariables(t *testing.T) {
	r := newRig(t, sessionText)
	r.insert("@line=$TM_LINE_NUMBER$0")
	if got := r.value(); got != "@line=1function foo() {\n    @line=2console.log(a);\n}" {
		t.Errorf("text %q", got)
	}
	r.assertSelections(sel(1, 8, 1, 8), sel(2, 12, 2, 12))
}

func TestSessionMerge(t *testing.T) {
	r := newRig(t, sessionText, sel(1, 1, 1, 1))
	s := r.insert("This ${1:is ${2:nested}}.$0")
	must(t, s.Next())
	r.assertSelections(sel(1, 9, 1, 15))

	must(t, s.Merge("really ${1:nested}$0", DefaultInsertOptions()))
	r.assertSelections(sel(1, 16, 1, 22))

	must(t, s.Next())
	r.assertSelections(sel(1, 22, 1, 22))
	if s.IsAtLastPlaceholder() {
		t.Error("at the last placeholder")
	}
	must(t, s.Next())
	if !s.IsAtLastPlaceholder() {
		t.Error("not at the last placeholder")
	}
	r.assertSelections(sel(1, 23, 1, 23))

	must(t, s.Prev())
	r.typeText("AAA")

	// back to `really ${1:nested}`
	must(t, s.Prev())
	r.assertSelections(sel(1, 16, 1, 22))
	// back to `${1:is ...}` which now grew
	must(t, s.Prev())
	r.assertSelections(sel(1, 6, 1, 25))
}

func TestSessionNextWithMissingDecorations(t *testing.T) {
	r := newRig(t, "", sel(1, 1, 1, 1))
	s := r.insert("${1:foo}$0")
	all := r.doc.AllDecorations()
	if len(all) == 0 {
		t.Fatal("no decorations")
	}
	for _, d := range all {
		r.doc.RemoveDecorations(d.ID)
	}
	must(t, s.Next())
}

func TestSessionDeepMergeHasNoPhantomCursors(t *testing.T) { // #279349
	r := newRig(t, "", sel(1, 1, 1, 1))
	s := r.insert(`\sin\left(${1:x}\right) `)
	for i := range 25 {
		must(t, s.Merge(`\sin\left(${1:x}\right) `, DefaultInsertOptions()))
		if n := r.cur.Len(); n != 1 {
			t.Fatalf("%d selections after %d merges", n, i+1)
		}
	}
}

func TestSessionMergeKeepsMirrors(t *testing.T) {
	r := newRig(t, "", sel(1, 1, 1, 1))
	s := r.insert("(${1:x})")
	must(t, s.Merge("${1:y}-${1}", DefaultInsertOptions()))
	if got := r.value(); got != "(y-y)" {
		t.Errorf("text %q", got)
	}
	if n := r.cur.Len(); n != 2 {
		t.Errorf("%d selections", n)
	}
}

func TestSessionMergeWhenOccurrencesCollapse(t *testing.T) {
	// $1$1 puts two empty occurrences at one place; the cursors collapse into one, so there are
	// more placeholders than nested snippets and the nested snippet is only inserted
	r := newRig(t, "", sel(1, 1, 1, 1))
	s := r.insert("$1$1$0")
	must(t, s.Merge("${1:nested}$0", DefaultInsertOptions()))
	if got := r.value(); got != "nested" {
		t.Errorf("text %q", got)
	}
}

func TestSessionTransforms(t *testing.T) {
	t.Run("transform", func(t *testing.T) {
		r := newRig(t, "", sel(1, 1, 1, 1))
		s := r.insert("${1/foo/bar/}$0")
		r.assertSelections(sel(1, 1, 1, 1))
		r.typeText("foo")
		must(t, s.Next())
		if got := r.value(); got != "bar" || !s.IsAtLastPlaceholder() {
			t.Errorf("text %q, at last %v", got, s.IsAtLastPlaceholder())
		}
		r.assertSelections(sel(1, 4, 1, 4))
	})
	t.Run("multi placeholder same index one transform", func(t *testing.T) {
		r := newRig(t, "", sel(1, 1, 1, 1))
		s := r.insert("$1 baz ${1/foo/bar/}$0")
		r.assertSelections(sel(1, 1, 1, 1), sel(1, 6, 1, 6))
		r.typeText("foo")
		must(t, s.Next())
		if got := r.value(); got != "foo baz bar" || !s.IsAtLastPlaceholder() {
			t.Errorf("text %q", got)
		}
		r.assertSelections(sel(1, 12, 1, 12))
	})
	t.Run("example", func(t *testing.T) {
		r := newRig(t, "", sel(1, 1, 1, 1))
		s := r.insert("${1:name} : ${2:type}${3/\\s:=(.*)/${1:+ :=}${1}/};\n$0")
		r.assertSelections(sel(1, 1, 1, 5))
		r.typeText("clk")
		must(t, s.Next())
		r.assertSelections(sel(1, 7, 1, 11))
		r.typeText("std_logic")
		must(t, s.Next())
		r.assertSelections(sel(1, 16, 1, 16))
		must(t, s.Next())
		if got := r.value(); got != "clk : std_logic;\n" || !s.IsAtLastPlaceholder() {
			t.Errorf("text %q", got)
		}
		r.assertSelections(sel(2, 1, 2, 1))
	})
	t.Run("example, the condition holds", func(t *testing.T) {
		r := newRig(t, "", sel(1, 1, 1, 1))
		s := r.insert("${1:name} : ${2:type}${3/\\s:=(.*)/${1:+ :=}${1}/};\n$0")
		r.typeText("clk")
		must(t, s.Next())
		r.typeText("std_logic")
		must(t, s.Next())
		r.assertSelections(sel(1, 16, 1, 16))
		r.typeText(" := '1'")
		must(t, s.Next())
		if got := r.value(); got != "clk : std_logic := '1';\n" || !s.IsAtLastPlaceholder() {
			t.Errorf("text %q", got)
		}
		r.assertSelections(sel(2, 1, 2, 1))
	})
	t.Run("with indent", func(t *testing.T) {
		snippet := strings.Join([]string{
			"private readonly ${1} = new Emitter<$2>();",
			"readonly ${1/^_(.*)/$1/}: Event<$2> = this.$1.event;",
			"$0",
		}, "\n")
		r := newRig(t, "{\n\t\n}")
		r.doc.SetIndentation(4, false)
		r.setSelections(sel(2, 2, 2, 2))
		s := r.insert(snippet)
		r.assertSelections(sel(2, 19, 2, 19), sel(3, 11, 3, 11), sel(3, 28, 3, 28))
		r.typeText("_prop")
		must(t, s.Next())
		r.assertSelections(sel(2, 39, 2, 39), sel(3, 23, 3, 23))
		r.typeText("string")
		must(t, s.Next())
		want := "{\n\tprivate readonly _prop = new Emitter<string>();\n\treadonly prop: Event<string> = this._prop.event;\n\t\n}"
		if got := r.value(); got != want || !s.IsAtLastPlaceholder() {
			t.Errorf("text %q", got)
		}
		r.assertSelections(sel(4, 2, 4, 2))
	})
	t.Run("adjacent to the previous placeholder, #96545", func(t *testing.T) {
		r := newRig(t, "", sel(1, 1, 1, 1))
		s := r.insert("${1:{}${2:fff}${1/{/}/}")
		r.assertSelections(sel(1, 1, 1, 2), sel(1, 5, 1, 6))
		must(t, s.Next())
		if got := r.value(); got != "{fff}" {
			t.Errorf("text %q", got)
		}
		r.assertSelections(sel(1, 2, 1, 5))
		r.typeText("ggg")
		must(t, s.Next())
		if got := r.value(); got != "{ggg}" || !s.IsAtLastPlaceholder() {
			t.Errorf("text %q", got)
		}
		r.assertSelections(sel(1, 6, 1, 6))
	})
	t.Run("tab stop selection, #96545", func(t *testing.T) {
		r := newRig(t, "", sel(1, 1, 1, 1))
		s := r.insert(`${1:{}${2:fff}${1/[\{]/}/}$0`)
		if got := r.value(); got != "{fff{" {
			t.Errorf("text %q", got)
		}
		r.assertSelections(sel(1, 1, 1, 2), sel(1, 5, 1, 6))
		must(t, s.Next())
		r.assertSelections(sel(1, 2, 1, 5))
	})
	t.Run("tabs are not replaced with spaces, #103818", func(t *testing.T) {
		r := newRig(t, "\n{\n  \n}")
		r.doc.SetIndentation(2, true)
		// VS Code's second cursor is at column 6 of a line of 2: the editor puts it at the end
		r.setSelections(sel(1, 1, 1, 1), sel(3, 3, 3, 3))
		s := r.insert(strings.Join([]string{"function animate () {", "\tvar ${1:a} = 12;", "\tconsole.log(${1/(.*)/\n\t\t$1\n\t/})", "}"}, "\n"))
		want := strings.Join([]string{
			"function animate () {", "  var a = 12;", "  console.log(a)", "}", "{",
			"  function animate () {", "    var a = 12;", "    console.log(a)", "  }", "}",
		}, "\n")
		if got := r.value(); got != want {
			t.Errorf("text\n%q\nwant\n%q", got, want)
		}
		r.typeText("bbb")
		must(t, s.Next())
		want = strings.Join([]string{
			"function animate () {", "  var bbb = 12;", "  console.log(", "    bbb", "  )", "}", "{",
			"  function animate () {", "    var bbb = 12;", "    console.log(", "      bbb", "    )", "  }", "}",
		}, "\n")
		if got := r.value(); got != want {
			t.Errorf("text\n%q\nwant\n%q", got, want)
		}
	})
}

func TestSessionPlaceholderIndexAfterRepeatedMerges(t *testing.T) { // #30769
	r := newRig(t, "", sel(1, 1, 1, 1))
	s := r.insert("test ${1:replaceme}")
	r.typeText("1")
	r.typeText("\n")
	if got := r.value(); got != "test 1\n" {
		t.Fatalf("text %q", got)
	}
	for i := 2; i <= 4; i++ {
		must(t, s.Merge("test ${1:replaceme}", DefaultInsertOptions()))
		r.typeText(fmt.Sprint(i))
		r.typeText("\n")
		want := ""
		for j := 1; j <= i; j++ {
			want += fmt.Sprintf("test %d\n", j)
		}
		if got := r.value(); got != want {
			t.Fatalf("after %d: text %q, want %q", i, got, want)
		}
	}
}

func TestSessionVariableTextIsNotNormalized(t *testing.T) { // #31124
	cases := []struct{ in, want string }{
		{"start\n\t\t-one\n\t\t-two\nend", "start\n\t<div>\n\t\t\t-one\n\t\t\t-two\n\t</div>\nend"},
		{"start\n\t\t-one\n\t-two\nend", "start\n\t<div>\n\t\t\t-one\n\t\t-two\n\t</div>\nend"},
	}
	for _, tc := range cases {
		r := newRig(t, tc.in)
		r.doc.SetIndentation(4, false)
		r.setSelections(sel(2, 2, 3, 7))
		r.insert("<div>\n\t$TM_SELECTED_TEXT\n</div>$0")
		if got := r.value(); got != tc.want {
			t.Errorf("text %q, want %q", got, tc.want)
		}
	}
}

func TestSessionOverwriteBefore(t *testing.T) { // #31199
	r := newRig(t, "console.log", sel(1, 9, 1, 12))
	opts := InsertOptions{OverwriteBefore: 3, AdjustWhitespace: true}
	must(t, NewSession(r.ed, r.env, "far", opts).Insert())
	if got := r.value(); got != "console.far" {
		t.Errorf("text %q", got)
	}
}

func TestSessionFromEdits(t *testing.T) {
	opts := DefaultInsertOptions()
	t.Run("empty", func(t *testing.T) {
		r := newRig(t, sessionText)
		p := r.session("").fromEdits(nil, true, opts)
		if len(p.edits) != 0 || len(p.snippets) != 0 {
			t.Errorf("%+v", p)
		}
	})
	t.Run("basic", func(t *testing.T) {
		r := newRig(t, `foo("bar")`)
		p := r.session("").fromEdits([]Edit{{Range: rng(1, 5, 1, 9), Template: "$1"}, {Range: rng(1, 1, 1, 1), Template: `const ${1:new_const} = "bar"`}}, true, opts)
		if len(p.edits) != 2 || p.edits[0].Range != rng(1, 1, 1, 1) || p.edits[0].Text != `const new_const = "bar"` ||
			p.edits[1].Range != rng(1, 5, 1, 9) || p.edits[1].Text != "new_const" {
			t.Fatalf("edits %+v", p.edits)
		}
		if len(p.snippets) != 1 || p.snippets[0].isTrivial() {
			t.Errorf("snippets %v", p.snippets)
		}
	})
	t.Run("with $SELECTION", func(t *testing.T) {
		r := newRig(t, "Some text and a selection", sel(1, 17, 1, 26))
		p := r.session("").fromEdits([]Edit{{Range: rng(1, 17, 1, 26), Template: "wrapped <$SELECTION>"}}, true, opts)
		if len(p.edits) != 1 || p.edits[0].Range != rng(1, 17, 1, 26) || p.edits[0].Text != "wrapped <selection>" {
			t.Fatalf("edits %+v", p.edits)
		}
		if len(p.snippets) != 1 || !p.snippets[0].isTrivial() {
			t.Error("the snippet is not trivial")
		}
	})
	t.Run("$TM_SELECTED_TEXT resolves per edit, not for the primary selection, #206121", func(t *testing.T) {
		r := newRig(t, "aaa\nbbb\nccc", sel(1, 1, 1, 4))
		p := r.session("").fromEdits([]Edit{
			{Range: rng(2, 1, 2, 4), Template: "[$TM_SELECTED_TEXT]"},
			{Range: rng(3, 1, 3, 4), Template: "[$TM_SELECTED_TEXT]"},
		}, true, opts)
		if len(p.edits) != 2 || p.edits[0].Text != "[bbb]" || p.edits[1].Text != "[ccc]" ||
			p.edits[0].Range != rng(2, 1, 2, 4) || p.edits[1].Range != rng(3, 1, 3, 4) {
			t.Errorf("edits %+v", p.edits)
		}
	})
	t.Run("$TM_LINE_NUMBER resolves per edit", func(t *testing.T) {
		r := newRig(t, "a\nb\nc", sel(1, 1, 1, 1))
		p := r.session("").fromEdits([]Edit{{Range: rng(1, 2, 1, 2), Template: "$TM_LINE_NUMBER"}, {Range: rng(3, 2, 3, 2), Template: "$TM_LINE_NUMBER"}}, true, opts)
		if len(p.edits) != 2 || p.edits[0].Text != "1" || p.edits[1].Text != "3" {
			t.Errorf("edits %+v", p.edits)
		}
	})
	t.Run("per-edit resolution does not corrupt earlier edits when value lengths differ", func(t *testing.T) {
		lines := make([]string, 100)
		for i := range lines {
			lines[i] = fmt.Sprintf("line%d", i+1)
		}
		r := newRig(t, strings.Join(lines, "\n"), sel(1, 1, 1, 1))
		p := r.session("").fromEdits([]Edit{{Range: rng(1, 2, 1, 2), Template: "$TM_LINE_NUMBER"}, {Range: rng(100, 2, 100, 2), Template: "$TM_LINE_NUMBER"}}, true, opts)
		if len(p.edits) != 2 || p.edits[0].Text != "1" || p.edits[1].Text != "100" {
			t.Errorf("edits %+v", p.edits)
		}
	})
	t.Run("$CURSOR_NUMBER uses the order the caller gave, not the sorted order", func(t *testing.T) {
		r := newRig(t, "xx\nyy", sel(1, 1, 1, 1))
		p := r.session("").fromEdits([]Edit{{Range: rng(2, 3, 2, 3), Template: "$CURSOR_NUMBER"}, {Range: rng(1, 3, 1, 3), Template: "$CURSOR_NUMBER"}}, true, opts)
		if len(p.edits) != 2 || p.edits[0].Range != rng(1, 3, 1, 3) || p.edits[0].Text != "2" || p.edits[1].Range != rng(2, 3, 2, 3) || p.edits[1].Text != "1" {
			t.Errorf("edits %+v", p.edits)
		}
	})
	t.Run("the placeholder of a later edit fills an earlier one, and its variables resolve, #206121", func(t *testing.T) {
		r := newRig(t, "aaa\nbbb", sel(1, 1, 1, 1))
		p := r.session("").fromEdits([]Edit{{Range: rng(1, 2, 1, 2), Template: "$1"}, {Range: rng(2, 2, 2, 2), Template: "${1:$TM_LINE_NUMBER}"}}, true, opts)
		unresolved := false
		p.snippets[0].snippet.Walk(func(m Marker) bool {
			if v, ok := m.(*Variable); ok && len(v.Children()) == 0 {
				unresolved = true
			}
			return true
		})
		if unresolved {
			t.Error("a variable that was copied into an earlier placeholder is not resolved")
		}
	})
	t.Run("inserting them as one snippet", func(t *testing.T) {
		r := newRig(t, `foo("bar")`)
		s := NewSessionFromEdits(r.ed, r.env, []Edit{{Range: rng(1, 5, 1, 9), Template: "$1"}, {Range: rng(1, 1, 1, 1), Template: `const ${1:new_const} = "bar"`}}, opts)
		must(t, s.Insert())
		if got := r.value(); got != `const new_const = "bar"foo(new_const")` {
			t.Errorf("text %q", got)
		}
		r.assertSelections(sel(1, 7, 1, 16), sel(1, 28, 1, 37))
	})
}

func TestSessionErrors(t *testing.T) {
	t.Run("a read-only document", func(t *testing.T) {
		r := newRig(t, sessionText)
		r.doc.SetReadOnly(true)
		err := r.session("foo").Insert()
		appErr, ok := errors.FromError(err)
		if !ok || !appErr.HasRoute("DocumentReadOnly") {
			t.Errorf("error %v", err)
		}
	})
	t.Run("cursors whose snippets overlap", func(t *testing.T) {
		// both cursors replace the two characters before them, which they share
		r := newRig(t, "aaaa", sel(1, 3, 1, 3), sel(1, 4, 1, 4))
		err := NewSession(r.ed, r.env, "x", InsertOptions{OverwriteBefore: 2, AdjustWhitespace: true}).Insert()
		appErr, ok := errors.FromError(err)
		if !ok || !appErr.HasRoute("EditsOverlap") {
			t.Errorf("error %v", err)
		}
	})
}

// Two editors of one document mark their tab stops with owners of their own.
func TestSessionDecorationOwners(t *testing.T) {
	for _, tc := range []struct {
		name  string
		owner int
		want  int
	}{
		{"the default owner", 0, DecorationOwner},
		{"an owner of the editor", 17, 17},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, sessionText, sel(1, 1, 1, 1))
			r.ed.Owner = tc.owner
			s := r.insert("a${1:b}${2:c}$0")
			defer s.Dispose()
			if got := len(r.doc.DecorationsOf(tc.want)); got != 3 {
				t.Errorf("%d tab stops marked for owner %d, want 3", got, tc.want)
			}
			if tc.owner != 0 && len(r.doc.DecorationsOf(DecorationOwner)) != 0 {
				t.Error("the default owner is used as well")
			}
		})
	}
}
