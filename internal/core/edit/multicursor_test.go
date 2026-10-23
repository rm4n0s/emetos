package edit

import (
	"regexp"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// setSelections sets the cursors as the editor does: merging the ones that meet.
func (e *ed) setSelections(sels []buffer.Selection) {
	cs := NewCursors(e.opts)
	cs.SetSelections(sels)
	e.sels = cs.Selections()
}

func (e *ed) mover() Mover { return NewMover(NewModel(e.doc.Snapshot()), e.opts, nil) }

func (e *ed) cursors() []Cursor {
	out := make([]Cursor, len(e.sels))
	for i, s := range e.sels {
		out[i] = CursorOf(s)
	}
	return out
}

// addCursors presses "add cursor" n times with the cursors kept between presses, as the editor
// does: they remember the column they aim for.
func (e *ed) addCursors(below bool, n int) *ed {
	cur := NewCursors(e.opts)
	cur.SetSelections(e.sels)
	for range n {
		if below {
			cur.Set(e.mover().AddCursorBelow(cur.All()))
		} else {
			cur.Set(e.mover().AddCursorAbove(cur.All()))
		}
	}
	e.sels = cur.Selections()
	return e
}

func (e *ed) addCursorAbove() *ed { return e.addCursors(false, 1) }
func (e *ed) addCursorBelow() *ed { return e.addCursors(true, 1) }

func (e *ed) commands() Commands {
	o := e.opts
	o.AutoClosed = e.closed.Characters()
	return NewCommands(NewModel(e.doc.Snapshot()), o)
}

// Ported from VS Code: reference/vscode/src/vs/editor/contrib/multicursor/test/browser/multicursor.test.ts
// ('issue #1336'; the word wrap and paste cases need the view and the clipboard).
func TestVSCodeAddCursor(t *testing.T) {
	t.Run("issue #1336: insert cursor below on the last line adds nothing", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "abc").addCursorBelow()
		e.assertSels(sel4{1, 1, 1, 1})
	})
	t.Run("insert cursor above on the first line adds nothing", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "abc", "def").caret(1, 2).addCursorAbove()
		e.assertSels(sel4{1, 2, 1, 2})
	})
	t.Run("below keeps the column and the shape of the selection", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "hello world", "hi", "another one").at(sel4{1, 2, 1, 5}).addCursorBelow()
		e.assertSels(sel4{1, 2, 1, 5}, sel4{2, 2, 2, 3})
		e = newEd(t, DefaultOptions(), "hello world", "hi", "another one").at(sel4{1, 2, 1, 5}).addCursors(true, 2)
		e.assertSels(sel4{1, 2, 1, 5}, sel4{2, 2, 2, 3}, sel4{3, 2, 3, 5})
	})
	t.Run("above and below with several cursors keep the primary first", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "a", "b", "c", "d").at(sel4{3, 1, 3, 1}, sel4{1, 1, 1, 1})
		e.addCursorBelow()
		e.assertSels(sel4{3, 1, 3, 1}, sel4{4, 1, 4, 1}, sel4{1, 1, 1, 1}, sel4{2, 1, 2, 1})
		e = newEd(t, DefaultOptions(), "a", "b", "c").caret(3, 1).addCursors(false, 2)
		e.assertSels(sel4{3, 1, 3, 1}, sel4{2, 1, 2, 1}, sel4{1, 1, 1, 1})
	})
	t.Run("the desired column is kept over short lines", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "a long line", "x", "another long one").caret(1, 8).addCursors(true, 2)
		e.assertSels(sel4{1, 8, 1, 8}, sel4{2, 2, 2, 2}, sel4{3, 8, 3, 8})
	})
}

func TestCursorsAtLineEnds(t *testing.T) {
	e := newEd(t, DefaultOptions(), "abc", "de", "", "fghi", "jk")
	got := e.commands().CursorsAtLineEnds([]buffer.Selection{e.sel(sel4{1, 2, 4, 3})})
	e.setSelections(got)
	e.assertSels(sel4{1, 4, 1, 4}, sel4{2, 3, 2, 3}, sel4{3, 1, 3, 1}, sel4{4, 3, 4, 3})
	// A selection that ends at the start of a line has no cursor there.
	got = e.commands().CursorsAtLineEnds([]buffer.Selection{e.sel(sel4{1, 2, 3, 1})})
	e.setSelections(got)
	e.assertSels(sel4{1, 4, 1, 4}, sel4{2, 3, 2, 3})
	if got := e.commands().CursorsAtLineEnds([]buffer.Selection{e.sel(sel4{1, 2, 1, 2})}); got != nil {
		t.Errorf("an empty selection gave %+v", got)
	}
	e.setSelections(e.commands().CursorsToBottom([]buffer.Selection{e.sel(sel4{3, 1, 3, 1})}))
	e.assertSels(sel4{3, 1, 3, 1}, sel4{4, 1, 4, 1}, sel4{5, 1, 5, 1})
	e.setSelections(e.commands().CursorsToTop([]buffer.Selection{e.sel(sel4{2, 2, 2, 3})}))
	e.assertSels(sel4{2, 2, 2, 3}, sel4{1, 2, 1, 3})
}

// occ is an editor with an occurrence session, as the controller keeps it.
type occ struct {
	*ed
	sess *OccurrenceSession
	find SearchOptions
}

func newOcc(t *testing.T, find SearchOptions, lines ...string) *occ {
	return &occ{ed: newEd(t, DefaultOptions(), lines...), find: find}
}

func (o *occ) do(f func(Commands, *OccurrenceSession, []buffer.Selection, SearchOptions) (*OccurrenceSession, []buffer.Selection, bool)) *occ {
	sess, sels, ok := f(o.commands(), o.sess, o.sels, o.find)
	o.sess = sess
	if ok {
		o.setSelections(sels)
	}
	return o
}

func (o *occ) at(sels ...sel4) *occ { o.ed.at(sels...); return o }

func (o *occ) addNext() *occ     { return o.do(Commands.AddNextOccurrence) }
func (o *occ) addPrevious() *occ { return o.do(Commands.AddPreviousOccurrence) }
func (o *occ) moveNext() *occ    { return o.do(Commands.MoveToNextOccurrence) }
func (o *occ) movePrev() *occ    { return o.do(Commands.MoveToPreviousOccurrence) }
func (o *occ) selectAll() *occ {
	sess, sels := o.commands().SelectAllOccurrences(o.sess, o.sels, o.find, nil)
	o.sess = sess
	if len(sels) > 0 {
		o.setSelections(sels)
	}
	return o
}

// Ported from VS Code: multicursor.test.ts ('issue #8817', 'AddSelectionToNextFindMatchAction can
// work with multiline', 'issue #6661', the 'starting with ...' cases and 'issue #20651').
func TestVSCodeOccurrences(t *testing.T) {
	insensitive := SearchOptions{}
	t.Run("issue #8817: the cursor stays when the others are cancelled", func(t *testing.T) {
		o := newOcc(t, insensitive, "var x = (3 * 5)", "var y = (3 * 5)", "var z = (3 * 5)")
		o.at(sel4{2, 9, 2, 16}).selectAll()
		o.assertSels(sel4{2, 9, 2, 16}, sel4{1, 9, 1, 16}, sel4{3, 9, 3, 16})
		cs := NewCursors(o.opts)
		cs.SetSelections(o.sels)
		cs.CancelSecondary()
		if got := cs.Primary().Selection; got != o.sel(sel4{2, 9, 2, 16}) {
			t.Errorf("primary = %+v", got)
		}
	})
	t.Run("can work with multiline", func(t *testing.T) {
		o := newOcc(t, insensitive, "", "qwe", "rty", "", "qwe", "", "rty", "qwe", "rty")
		o.at(sel4{2, 1, 3, 4}).addNext()
		o.assertSels(sel4{2, 1, 3, 4}, sel4{8, 1, 9, 4})
	})
	t.Run("issue #6661: touching ranges", func(t *testing.T) {
		o := newOcc(t, insensitive, "abcabc", "abc", "abcabc")
		o.at(sel4{1, 1, 1, 4}).addNext()
		o.assertSels(sel4{1, 1, 1, 4}, sel4{1, 4, 1, 7})
		o.addNext().addNext().addNext()
		o.assertSels(sel4{1, 1, 1, 4}, sel4{1, 4, 1, 7}, sel4{2, 1, 2, 4}, sel4{3, 1, 3, 4}, sel4{3, 4, 3, 7})
		o.typeText("z")
		o.assertSels(sel4{1, 2, 1, 2}, sel4{1, 3, 1, 3}, sel4{2, 2, 2, 2}, sel4{3, 2, 3, 2}, sel4{3, 3, 3, 3})
		o.assertText("zz\nz\nzz")
	})
	text := []string{"abc pizza", "abc house", "abc bar"}
	t.Run("a single collapsed selection selects the word first", func(t *testing.T) {
		o := newOcc(t, insensitive, text...).at(sel4{1, 2, 1, 2})
		o.addNext().assertSels(sel4{1, 1, 1, 4})
		o.addNext().assertSels(sel4{1, 1, 1, 4}, sel4{2, 1, 2, 4})
		o.addNext().assertSels(sel4{1, 1, 1, 4}, sel4{2, 1, 2, 4}, sel4{3, 1, 3, 4})
	})
	t.Run("two selections, one collapsed", func(t *testing.T) {
		o := newOcc(t, insensitive, text...).at(sel4{1, 1, 1, 4}, sel4{2, 2, 2, 2})
		o.addNext().assertSels(sel4{1, 1, 1, 4}, sel4{2, 1, 2, 4})
		o = newOcc(t, insensitive, text...).at(sel4{1, 2, 1, 2}, sel4{2, 1, 2, 4})
		o.addNext().assertSels(sel4{1, 1, 1, 4}, sel4{2, 1, 2, 4})
	})
	t.Run("all selections collapsed", func(t *testing.T) {
		o := newOcc(t, insensitive, text...).at(sel4{1, 2, 1, 2}, sel4{2, 2, 2, 2}, sel4{3, 2, 3, 2})
		o.addNext().assertSels(sel4{1, 1, 1, 4}, sel4{2, 1, 2, 4}, sel4{3, 1, 3, 4})
	})
	t.Run("selections of different words are left alone", func(t *testing.T) {
		o := newOcc(t, insensitive, text...).at(sel4{1, 1, 1, 4}, sel4{2, 5, 2, 10})
		o.addNext().assertSels(sel4{1, 1, 1, 4}, sel4{2, 5, 2, 10})
		if o.sess != nil {
			t.Error("a session started")
		}
	})
	t.Run("issue #20651: case insensitive", func(t *testing.T) {
		o := newOcc(t, insensitive, "test", "testte", "Test", "testte", "test").at(sel4{1, 1, 1, 5})
		o.addNext().assertSels(sel4{1, 1, 1, 5}, sel4{2, 1, 2, 5})
		o.addNext().addNext().addNext()
		o.assertSels(sel4{1, 1, 1, 5}, sel4{2, 1, 2, 5}, sel4{3, 1, 3, 5}, sel4{4, 1, 4, 5}, sel4{5, 1, 5, 5})
	})
	t.Run("whole word skips longer words", func(t *testing.T) {
		o := newOcc(t, SearchOptions{WholeWord: true}, "foo foobar foo").at(sel4{1, 1, 1, 4})
		o.addNext().assertSels(sel4{1, 1, 1, 4}, sel4{1, 12, 1, 15})
	})
	t.Run("nothing more to add keeps the selections", func(t *testing.T) {
		o := newOcc(t, insensitive, "abc", "def").at(sel4{1, 1, 1, 4})
		o.addNext().assertSels(sel4{1, 1, 1, 4})
	})
}

// Ported from VS Code: multicursor.test.ts ('Find state: selected text matching uses the Find
// options', 'AddSelectionToNextFindMatchAction can work with...').
func TestVSCodeOccurrenceFindOptions(t *testing.T) {
	text := []string{"foo", "FOObar", "foobar", "FOO", "foo"}
	lines := func(o *occ) []int {
		var out []int
		for _, s := range o.sels {
			out = append(out, min(s.Anchor.Line, s.Active.Line)+1)
		}
		return out
	}
	type kind int
	const (
		insens kind = iota
		sens
		empty
	)
	run := func(t *testing.T, k kind, line int, steps int, f func(*occ) *occ) *occ {
		find := SearchOptions{MatchCase: k == sens}
		o := newOcc(t, find, text...)
		if k == empty {
			o.at(sel4{line, 2, line, 2})
		} else {
			o.at(sel4{line, 1, line, 4})
		}
		for range steps {
			f(o)
		}
		return o
	}
	steps := func(k kind) int {
		if k == empty {
			return 2
		}
		return 1
	}
	check := func(t *testing.T, name string, line int, f func(*occ) *occ, want map[kind][]int) {
		for k, w := range want {
			t.Run(name, func(t *testing.T) {
				got := lines(run(t, k, line, steps(k), f))
				if len(got) != len(w) {
					t.Fatalf("kind %d: lines %v, want %v", k, got, w)
				}
				for i := range w {
					if got[i] != w[i] {
						t.Fatalf("kind %d: lines %v, want %v", k, got, w)
					}
				}
			})
		}
	}
	check(t, "add next", 1, (*occ).addNext, map[kind][]int{insens: {1, 2}, sens: {1, 3}, empty: {1, 5}})
	check(t, "add previous", 5, (*occ).addPrevious, map[kind][]int{insens: {5, 4}, sens: {5, 3}, empty: {5, 1}})
	check(t, "move next", 1, (*occ).moveNext, map[kind][]int{insens: {2}, sens: {3}, empty: {5}})
	check(t, "move previous", 5, (*occ).movePrev, map[kind][]int{insens: {4}, sens: {3}, empty: {1}})
	check(t, "select all", 1, (*occ).selectAll, map[kind][]int{insens: {1, 2, 3, 4, 5}, sens: {1, 3, 5}, empty: {1, 5}})

	t.Run("select all puts the occurrence at the cursor first", func(t *testing.T) {
		o := newOcc(t, SearchOptions{}, "a", "b", "a", "a").at(sel4{3, 1, 3, 2}).selectAll()
		o.assertSels(sel4{3, 1, 3, 2}, sel4{1, 1, 1, 2}, sel4{4, 1, 4, 2})
	})
	t.Run("select all within a scope", func(t *testing.T) {
		o := newOcc(t, SearchOptions{}, "a", "a", "a", "a").at(sel4{2, 1, 2, 2})
		sess, sels := o.commands().SelectAllOccurrences(nil, o.sels, o.find, []buffer.Range{{Start: o.pos(2, 1), End: o.pos(3, 2)}})
		_ = sess
		o.setSelections(sels)
		o.assertSels(sel4{2, 1, 2, 2}, sel4{3, 1, 3, 2})
	})
	t.Run("skipping an occurrence", func(t *testing.T) {
		o := newOcc(t, SearchOptions{}, "a", "a", "a").at(sel4{1, 1, 1, 2})
		o.addNext().moveNext()
		o.assertSels(sel4{1, 1, 1, 2}, sel4{3, 1, 3, 2})
		o.movePrev()
		o.assertSels(sel4{1, 1, 1, 2}, sel4{2, 1, 2, 2})
	})
}

// Ported from VS Code: multicursor.test.ts (the CRLF-free parts of the line end cases).
func TestMultiCursorFromSelection(t *testing.T) {
	e := newEd(t, DefaultOptions(), "a", "b", "c")
	e.setSelections(e.commands().CursorsToBottom([]buffer.Selection{e.sel(sel4{1, 1, 1, 1})}))
	e.assertSels(sel4{1, 1, 1, 1}, sel4{2, 1, 2, 1}, sel4{3, 1, 3, 1})
}

// colText is the text of VS Code's 'column select 1' test.
var colText = []string{
	"\tprivate compute(a:number): boolean {",
	"\t\tif (a + 3 === 0 || a + 5 === 0) {",
	"\t\t\treturn false;",
	"\t\t}",
	"\t}",
}

func (e *ed) columnSelect(c ColumnSelection) *ed {
	cur := NewCursors(e.opts)
	cur.Set(c.Cursors)
	e.sels = cur.Selections()
	return e
}

// Ported from VS Code: src/vs/editor/test/browser/controller/cursor.test.ts ('column select 1',
// 'issue #4905', 'issue #118062').
func TestVSCodeColumnSelect(t *testing.T) {
	t.Run("column select 1", func(t *testing.T) {
		e := newEd(t, tabsOptions(), colText...).caret(1, 7)
		mv := e.mover()
		d := mv.ColumnSelectDataOf(e.cursors()[0])
		e.columnSelect(mv.ColumnSelect(d.FromLine, d.FromVisual, 3, 14))
		e.assertSels(sel4{1, 7, 1, 12}, sel4{2, 4, 2, 9}, sel4{3, 3, 3, 6}, sel4{4, 4, 4, 4})
	})
	t.Run("issue #4905", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "abcdef", "abcdef", "abcdef", "abcdef").caret(1, 4)
		mv := e.mover()
		d := mv.ColumnSelectDataOf(e.cursors()[0])
		e.columnSelect(mv.ColumnSelect(d.FromLine, d.FromVisual, 3, 0))
		e.assertSels(sel4{1, 4, 1, 1}, sel4{2, 4, 2, 1}, sel4{3, 4, 3, 1}, sel4{4, 4, 4, 1})
	})
	t.Run("issue #118062: left from the second column", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "abc").caret(1, 2)
		mv := e.mover()
		e.columnSelect(mv.ColumnSelectLeft(mv.ColumnSelectDataOf(e.cursors()[0])))
		e.assertSels(sel4{1, 2, 1, 1})
	})
	t.Run("keyboard: right and down stop at the widest line", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "abc", "abcdef", "ab").caret(1, 2)
		mv := e.mover()
		cs := mv.ColumnSelectRight(mv.ColumnSelectDataOf(e.cursors()[0]))
		e.columnSelect(cs)
		e.assertSels(sel4{1, 2, 1, 3})
		cs = mv.ColumnSelectDown(cs.Data, false)
		e.columnSelect(cs)
		e.assertSels(sel4{1, 2, 1, 3}, sel4{2, 2, 2, 3})
		cs = mv.ColumnSelectDown(cs.Data, false)
		e.columnSelect(cs)
		e.assertSels(sel4{1, 2, 1, 3}, sel4{2, 2, 2, 3}, sel4{3, 2, 3, 3})
		for range 10 {
			cs = mv.ColumnSelectRight(cs.Data)
		}
		e.columnSelect(cs)
		e.assertSels(sel4{1, 2, 1, 4}, sel4{2, 2, 2, 7}, sel4{3, 2, 3, 3})
		cs = mv.ColumnSelectUp(cs.Data, false)
		cs = mv.ColumnSelectUp(cs.Data, false)
		cs = mv.ColumnSelectUp(cs.Data, false)
		e.columnSelect(cs)
		e.assertSels(sel4{1, 2, 1, 4})
	})
}

// wordPattern is the word definition of the language VS Code's smart select tests use: it
// leaves '-' and '|' inside words.
var wordPattern = regexp.MustCompile(`(-?\d*\.\d\w*)|([^` + "`" + `~!@#$%^&*()=+\[{\]}\\;:'",.<>/?\s]+)`)

func smartOptions() Options {
	o := DefaultOptions()
	o.WordAtPosition = func(line string, col int) (int, int, bool) {
		for _, m := range wordPattern.FindAllStringIndex(line, -1) {
			if m[0] <= col && col <= m[1] {
				return m[0], m[1], true
			}
		}
		return 0, 0, false
	}
	return o
}

// providedRanges are the ranges around the position of the pipe in text, as the word
// provider gives them; the pipe is removed from the text.

func assertRanges(t *testing.T, subwords bool, text string, want ...sel4) {
	t.Helper()
	i := strings.Index(text, "|")
	e := newEd(t, smartOptions(), strings.Replace(text, "|", "", 1))
	got := NewWordSelectionRanges(e.opts, subwords).SelectionRanges(NewModel(e.doc.Snapshot()), []buffer.Position{{Col: i}})[0]
	if len(got) != len(want) {
		t.Fatalf("%s: %d ranges %+v, want %d", text, len(got), got, len(want))
	}
	for k, w := range want {
		if got[k] != e.sel(w).Range() {
			t.Errorf("%s: range %d = %+v, want %+v", text, k+1, got[k], e.sel(w).Range())
		}
	}
}

// assertGetRanges checks the ranges around a position from the smallest to the largest; the
// expectation is written from the largest, as VS Code's test does.
func assertGetRanges(t *testing.T, e *ed, line, col int, whitespace bool, providers []SelectionRangeProvider, want ...sel4) {
	t.Helper()
	ropts := DefaultSelectionRangesOptions()
	ropts.SelectLeadingAndTrailingWhitespace = whitespace
	got := ProvideSelectionRanges(NewModel(e.doc.Snapshot()), e.opts, []buffer.Position{e.pos(line, col)}, ropts, providers...)[0]
	if len(got) != len(want) {
		t.Fatalf("%d ranges %+v, want %d", len(got), got, len(want))
	}
	for k, w := range want {
		g := got[len(got)-1-k]
		if g != e.sel(w).Range() {
			t.Errorf("range %d = %+v, want %+v", k+1, g, e.sel(w).Range())
		}
	}
}

// Ported from VS Code: src/vs/editor/contrib/smartSelect/test/browser/smartSelect.test.ts (the
// cases that need no bracket or string provider).
func TestVSCodeSmartSelect(t *testing.T) {
	t.Run("config: selectLeadingAndTrailingWhitespace", func(t *testing.T) {
		e := newEd(t, smartOptions(), "aaa", "\tbbb", "")
		assertGetRanges(t, e, 2, 3, true, nil, sel4{1, 1, 3, 1}, sel4{2, 1, 2, 5}, sel4{2, 2, 2, 5})
		assertGetRanges(t, e, 2, 3, false, nil, sel4{1, 1, 3, 1}, sel4{2, 2, 2, 5})
	})
	t.Run("in-word ranges", func(t *testing.T) {
		assertRanges(t, true, "f|ooBar", sel4{1, 1, 1, 4}, sel4{1, 1, 1, 7}, sel4{1, 1, 1, 7})
		assertRanges(t, true, "f|oo_Ba", sel4{1, 1, 1, 4}, sel4{1, 1, 1, 7}, sel4{1, 1, 1, 7})
		assertRanges(t, true, "f|oo-Ba", sel4{1, 1, 1, 4}, sel4{1, 1, 1, 7}, sel4{1, 1, 1, 7})
	})
	t.Run("in-word ranges with selectSubwords=false", func(t *testing.T) {
		assertRanges(t, false, "f|ooBar", sel4{1, 1, 1, 7}, sel4{1, 1, 1, 7})
		assertRanges(t, false, "f|oo_Ba", sel4{1, 1, 1, 7}, sel4{1, 1, 1, 7})
		assertRanges(t, false, "f|oo-Ba", sel4{1, 1, 1, 7}, sel4{1, 1, 1, 7})
	})
	t.Run("issue #67493: the current hump first in camelCase", func(t *testing.T) {
		assertRanges(t, true, "Abs|tractSmartSelect", sel4{1, 1, 1, 9}, sel4{1, 1, 1, 20}, sel4{1, 1, 1, 20})
		for _, text := range []string{"AbstractSma|rtSelect", "Abstrac-Sma|rt-elect", "Abstrac_Sma|rt_elect", "Abstrac_Sma|rt-elect", "Abstrac_Sma|rtSelect"} {
			assertRanges(t, true, text, sel4{1, 9, 1, 14}, sel4{1, 1, 1, 20}, sel4{1, 1, 1, 20})
		}
	})
	t.Run("issue #90589: words with underscores", func(t *testing.T) {
		for _, text := range []string{"Hel|lo_World"} {
			assertRanges(t, true, text, sel4{1, 1, 1, 6}, sel4{1, 1, 1, 12}, sel4{1, 1, 1, 12})
		}
		for _, text := range []string{"Hello_Wo|rld", "Hello_|World", "Hello-|World"} {
			assertRanges(t, true, text, sel4{1, 7, 1, 12}, sel4{1, 1, 1, 12}, sel4{1, 1, 1, 12})
		}
		for _, text := range []string{"Hello|_World", "Hello|-World"} {
			assertRanges(t, true, text, sel4{1, 1, 1, 6}, sel4{1, 1, 1, 12}, sel4{1, 1, 1, 12})
		}
		assertRanges(t, true, "Hello|World", sel4{1, 6, 1, 11}, sel4{1, 1, 1, 11}, sel4{1, 1, 1, 11})
	})
	t.Run("issue #73850: line ranges only when the next range contains them", func(t *testing.T) {
		e := newEd(t, smartOptions(), "type T = {", "\tx: number", "}")
		p := fixedRanges{e.sel(sel4{1, 10, 1, 11}).Range(), e.sel(sel4{1, 10, 3, 2}).Range(), e.sel(sel4{1, 1, 3, 2}).Range()}
		assertGetRanges(t, e, 1, 10, true, []SelectionRangeProvider{p}, sel4{1, 1, 3, 2}, sel4{1, 10, 3, 2}, sel4{1, 10, 1, 11})
	})
	t.Run("expanding and shrinking", func(t *testing.T) {
		e := newEd(t, smartOptions(), "foo bar").caret(1, 2)
		x := e.commands().NewExpansion(e.sels, DefaultSelectionRangesOptions())
		for _, w := range []sel4{{1, 1, 1, 4}, {1, 1, 1, 8}} {
			if got := x.Expand(); got[0] != e.sel(w) {
				t.Errorf("expand = %+v, want %+v", got[0], e.sel(w))
			}
		}
		if got := x.Shrink(); got[0] != e.sel(sel4{1, 1, 1, 4}) {
			t.Errorf("shrink = %+v", got[0])
		}
		x.Shrink()
		if got := x.Shrink(); got[0] != e.sel(sel4{1, 2, 1, 2}) {
			t.Errorf("shrink back = %+v", got[0])
		}
	})
}

// fixedRanges is a provider that gives the same ranges at every position.
type fixedRanges []buffer.Range

func (f fixedRanges) SelectionRanges(_ Model, positions []buffer.Position) [][]buffer.Range {
	out := make([][]buffer.Range, len(positions))
	for i := range out {
		out[i] = f
	}
	return out
}

func TestLiteralSearch(t *testing.T) {
	e := newEd(t, DefaultOptions(), "Foo foo_bar (foo) \u00e9foo foo", "xfoo", "foo")
	m := NewModel(e.doc.Snapshot())
	r := func(line, from, to int) buffer.Range {
		return buffer.Range{Start: buffer.Position{Line: line, Col: from}, End: buffer.Position{Line: line, Col: to}}
	}
	// Columns are bytes: the \u00e9 takes two.
	words := newLiteralSearch(m, "foo", false, e.opts.words())
	want := []buffer.Range{r(0, 0, 3), r(0, 13, 16), r(0, 24, 27), r(2, 0, 3)}
	got := words.All(nil)
	if len(got) != len(want) {
		t.Fatalf("whole word matches = %+v, want %+v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("match %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if got, ok := words.Previous(buffer.Position{Line: 2}); !ok || got != want[2] {
		t.Errorf("previous = %+v %v", got, ok)
	}
	if got, ok := words.Previous(buffer.Position{}); !ok || got != want[3] {
		t.Errorf("previous wraps to %+v %v", got, ok)
	}
	if got, ok := words.Next(buffer.Position{Line: 2, Col: 3}); !ok || got != want[0] {
		t.Errorf("next wraps to %+v %v", got, ok)
	}
	if got := words.All([]buffer.Range{{End: buffer.Position{Col: 20}}}); len(got) != 2 {
		t.Errorf("scoped matches = %+v", got)
	}
	plain := newLiteralSearch(m, "FOO", true, e.opts.words())
	if got := plain.All(nil); len(got) != 0 {
		t.Errorf("case sensitive matches = %+v", got)
	}
}
