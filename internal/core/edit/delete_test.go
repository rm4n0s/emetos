package edit

import "testing"

// Ported from VS Code: reference/vscode/src/vs/editor/test/browser/controller/cursor.test.ts
// ('issue #1140', 'issue #84897', 'issue #122914', 'issue #99629' twice, 'UseTabStops
// is off', 'Backspace removes whitespaces with tab size', 'All cursors should do
// the same thing when deleting left').
func TestVSCodeDeleteLeft(t *testing.T) {
	t.Run("issue #1140: Backspace stops prematurely", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "function baz() {", "  return 1;", "};").at(sel4{3, 2, 1, 14})
		e.back()
		e.assertCaret(1, 14)
		e.assertText("function baz(;")
	})
	for _, useTabStops := range []bool{true, false} {
		name := "issue #84897: Left delete in Thai"
		if !useTabStops {
			name = "issue #122914: Left delete in Thai (useTabStops: false)"
		}
		t.Run(name, func(t *testing.T) {
			opts := DefaultOptions()
			opts.UseTabStops = useTabStops
			e := newEd(t, opts, "สวัสดี").caret(1, 7)
			for _, want := range []string{"สวัสด", "สวัส", "สวั", "สว", "ส", ""} {
				e.back()
				e.assertText(want)
			}
		})
	}
	t.Run("issue #99629: emoji modifiers", func(t *testing.T) {
		opts := DefaultOptions()
		opts.UseTabStops = false
		e := newEd(t, opts, "👶🏾").caret(1, 5)
		e.back()
		e.assertText("")
	})
	t.Run("issue #99629: emoji ZWJ sequence", func(t *testing.T) {
		opts := DefaultOptions()
		opts.UseTabStops = false
		e := newEd(t, opts, "👨‍👩🏽‍👧‍👦").caret(1, 14)
		for _, want := range []string{"👨‍👩🏽‍👧", "👨‍👩🏽", "👨", ""} {
			e.back()
			e.assertText(want)
		}
	})
	t.Run("UseTabStops is off", func(t *testing.T) {
		opts := DefaultOptions()
		opts.UseTabStops = false
		e := newEd(t, opts, "    x", "        a    ", "    ").caret(2, 9)
		e.back()
		e.assertLine(2, "       a    ")
	})
	t.Run("Backspace removes whitespaces with tab size", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), " \t \t     x", "        a    ", "    ")
		// Not in the indentation: only one space goes.
		e.caret(2, len(e.line(2))+1).back()
		e.assertLine(2, "        a   ")
		// In the indentation: to the previous tab stop.
		e.caret(2, 9).back()
		e.assertLine(2, "    a   ")
		e.back()
		e.assertLine(2, "a   ")
		e.undo()
		e.assertLine(2, "        a   ")
		// At the start of the text nothing happens.
		e.caret(1, 1).back()
		e.assertLine(1, " \t \t     x")
		// Mixed white space stops at the tab stops.
		e.caret(1, 10).back()
		e.assertLine(1, " \t \t    x")
		e.back()
		e.assertLine(1, " \t \tx")
		e.back()
		e.assertLine(1, " \tx")
		e.back()
		e.assertLine(1, "x")
		// On the last line, then the line break.
		e.caret(3, len(e.line(3))+1).back()
		e.assertLine(3, "")
		e.back()
		e.assertText("x\n        a   ")
		// A selection: only the selected text.
		e.at(sel4{2, 3, 2, 4}).back()
		e.assertLine(2, "       a   ")
	})
	t.Run("All cursors should do the same thing when deleting left", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "var a = ()").at(sel4{1, 4, 1, 4}, sel4{1, 10, 1, 10})
		e.back()
		e.assertText("va a = )")
	})
}

func TestDeleteLeft(t *testing.T) {
	t.Run("start of the text does nothing", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "ab").caret(1, 1)
		e.back()
		e.assertText("ab")
		e.assertCaret(1, 1)
		if e.doc.CanUndo() {
			t.Error("a no-op was recorded")
		}
	})
	t.Run("joins with the line above", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "ab", "cd").caret(2, 1)
		e.back()
		e.assertText("abcd")
		e.assertCaret(1, 3)
	})
	t.Run("deletes one code point of a combining sequence", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "ãb").caret(1, 4)
		e.back()
		e.assertText("ã")
		e.back()
		e.assertText("a")
	})
	t.Run("keycap and variation selector", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "x❤️").caret(1, 4)
		e.back()
		e.assertText("x")
	})
	t.Run("tab stops with tabs", func(t *testing.T) {
		e := newEd(t, tabsOptions(), "\t\t  x").caret(1, 5)
		e.back()
		e.assertText("\t\tx")
		e.back()
		e.assertText("\tx")
	})
	t.Run("several cursors on one line", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "abcdef").at(sel4{1, 3, 1, 3}, sel4{1, 5, 1, 5})
		e.back()
		e.assertText("acef")
		e.assertSels(sel4{1, 2, 1, 2}, sel4{1, 3, 1, 3})
	})
	t.Run("cursors at the start and joined lines", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "a", "b", "c").at(sel4{1, 1, 1, 1}, sel4{2, 1, 2, 1}, sel4{3, 1, 3, 1})
		e.back()
		e.assertText("abc")
		e.assertSels(sel4{1, 1, 1, 1}, sel4{1, 2, 1, 2}, sel4{1, 3, 1, 3})
	})
	t.Run("a deleted line break starts an undo group", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "ab", "cd").caret(2, 2)
		e.back().back().back()
		e.assertText("ad")
		e.undo()
		e.assertText("abd")
		e.undo()
		e.assertText("ab\nd")
		e.undo()
		e.assertText("ab\ncd")
	})
	t.Run("backspaces join one undo group", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "abcdef").caret(1, 7)
		e.back().back().back()
		e.assertText("abc")
		e.undo()
		e.assertText("abcdef")
		e.assertCaret(1, 7)
	})
}

func TestDeleteRight(t *testing.T) {
	t.Run("deletes a character", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "abc").caret(1, 2)
		e.del()
		e.assertText("ac")
		e.assertCaret(1, 2)
	})
	t.Run("end of the text does nothing", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "ab").caret(1, 3)
		e.del()
		e.assertText("ab")
		if e.doc.CanUndo() {
			t.Error("a no-op was recorded")
		}
	})
	t.Run("joins with the next line", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "ab", "  cd").caret(1, 3)
		e.del()
		e.assertText("ab  cd")
		e.assertCaret(1, 3)
	})
	t.Run("deletes a whole grapheme cluster", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "ãb", "👨‍👩‍👧x").caret(1, 1)
		e.del()
		e.assertText("b\n👨‍👩‍👧x")
		e.caret(2, 1).del()
		e.assertText("b\nx")
	})
	t.Run("a selection is deleted", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "hello world").at(sel4{1, 3, 1, 8})
		e.del()
		e.assertText("heorld")
		e.assertCaret(1, 3)
	})
	t.Run("several cursors", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "abcd", "efgh").at(sel4{1, 2, 1, 2}, sel4{2, 4, 2, 4}, sel4{1, 3, 1, 3})
		e.del()
		e.assertText("ad\nefg")
		e.assertSels(sel4{1, 2, 1, 2}, sel4{2, 4, 2, 4})
	})
	t.Run("deletes join one undo group, a typing one does not join", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "abcdef").caret(1, 2)
		e.del().del().typeText("X")
		e.assertText("aXdef")
		e.undo()
		e.assertText("adef")
		e.undo()
		e.assertText("abcdef")
	})
}

// Ported from VS Code: cursor.test.ts, the 'Undo stops' suite (typing and
// deleting in every order).
func TestVSCodeDeleteUndoStops(t *testing.T) {
	line := func() *ed { return newEd(t, DefaultOptions(), "A  line", "Another line") }
	t.Run("between typing and deleting left", func(t *testing.T) {
		e := line().caret(1, 3).typeText("first")
		e.assertLine(1, "A first line")
		e.assertCaret(1, 8)
		e.back().back()
		e.assertLine(1, "A fir line")
		e.assertCaret(1, 6)
		e.undo()
		e.assertLine(1, "A first line")
		e.assertCaret(1, 8)
		e.undo()
		e.assertLine(1, "A  line")
		e.assertCaret(1, 3)
	})
	t.Run("between typing and deleting right", func(t *testing.T) {
		e := line().caret(1, 3).typeText("first")
		e.del().del()
		e.assertLine(1, "A firstine")
		e.assertCaret(1, 8)
		e.undo()
		e.assertLine(1, "A first line")
		e.assertCaret(1, 8)
		e.undo()
		e.assertLine(1, "A  line")
		e.assertCaret(1, 3)
	})
	t.Run("between deleting left and typing", func(t *testing.T) {
		e := line().caret(2, 8)
		for i := 0; i < 7; i++ {
			e.back()
		}
		e.assertLine(2, " line")
		e.assertCaret(2, 1)
		e.typeText("Second")
		e.assertLine(2, "Second line")
		e.assertCaret(2, 7)
		e.undo()
		e.assertLine(2, " line")
		e.assertCaret(2, 1)
		e.undo()
		e.assertLine(2, "Another line")
		e.assertCaret(2, 8)
	})
	t.Run("between deleting left and deleting right", func(t *testing.T) {
		e := line().caret(2, 8)
		for i := 0; i < 7; i++ {
			e.back()
		}
		e.assertLine(2, " line")
		e.assertCaret(2, 1)
		for i := 0; i < 5; i++ {
			e.del()
		}
		e.assertLine(2, "")
		e.assertCaret(2, 1)
		e.undo()
		e.assertLine(2, " line")
		e.assertCaret(2, 1)
		e.undo()
		e.assertLine(2, "Another line")
		e.assertCaret(2, 8)
	})
	t.Run("between deleting right and typing", func(t *testing.T) {
		e := line().caret(2, 9)
		for i := 0; i < 4; i++ {
			e.del()
		}
		e.assertLine(2, "Another ")
		e.assertCaret(2, 9)
		e.typeText("text")
		e.assertLine(2, "Another text")
		e.assertCaret(2, 13)
		e.undo()
		e.assertLine(2, "Another ")
		e.assertCaret(2, 9)
		e.undo()
		e.assertLine(2, "Another line")
		e.assertCaret(2, 9)
	})
	t.Run("between deleting right and deleting left", func(t *testing.T) {
		e := line().caret(2, 9)
		for i := 0; i < 4; i++ {
			e.del()
		}
		e.assertLine(2, "Another ")
		e.assertCaret(2, 9)
		for i := 0; i < 6; i++ {
			e.back()
		}
		e.assertLine(2, "An")
		e.assertCaret(2, 3)
		e.undo()
		e.assertLine(2, "Another ")
		e.assertCaret(2, 9)
		e.undo()
		e.assertLine(2, "Another line")
		e.assertCaret(2, 9)
	})
}

func TestOverlappingEditsDropTheLaterCursor(t *testing.T) {
	// Ctrl+Backspace at two cursors whose ranges overlap: the later cursor loses.
	e := newEd(t, DefaultOptions(), "one two three").at(sel4{1, 8, 1, 8}, sel4{1, 6, 1, 6})
	e.run(Commands.DeleteWordLeft)
	e.assertText("one  three")
	e.assertSels(sel4{1, 5, 1, 5})
}

func TestResultWithoutEditsKeepsTheSelections(t *testing.T) {
	e := newEd(t, DefaultOptions(), "ab").at(sel4{1, 1, 1, 1}, sel4{1, 3, 1, 3})
	res := NewCommands(NewModel(e.doc.Snapshot()), e.opts).DeleteWordLeft(e.sels)
	// The first cursor has nothing to delete; the second deletes the word.
	if len(res.Edits) != 1 || len(res.Selections) != 2 {
		t.Fatalf("result = %+v", res)
	}
	e.run(Commands.DeleteWordLeft)
	e.assertText("")
}

func TestPrimaryCursorNeverLoses(t *testing.T) {
	// Both cursors want to delete overlapping words: the primary one (the first)
	// keeps its edit, whichever is first in the text.
	e := newEd(t, DefaultOptions(), "one two three").at(sel4{1, 6, 1, 6}, sel4{1, 8, 1, 8})
	e.run(Commands.DeleteWordLeft)
	e.assertText("one wo three")
	e.assertSels(sel4{1, 5, 1, 5})
}
