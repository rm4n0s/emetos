package edit

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// repeatDelete runs a delete command until the text is empty and returns the
// position to record after each one, as the ported tests do: the cursor for
// deleting left, and for deleting right the number of bytes deleted so far (the
// cursor stays put, so its position in the original text is that of the text
// still there).
func repeatDelete(t *testing.T, opts Options, text string, left bool, run func(Commands, []buffer.Selection) Result) []buffer.Position {
	t.Helper()
	e := newEd(t, opts, text)
	if left {
		e.caret(1, len(text)+1)
	} else {
		e.caret(1, 1)
	}
	var stops []buffer.Position
	for e.doc.Text() != "" {
		e.run(run)
		if left {
			stops = append(stops, e.sels[0].Active)
		} else {
			stops = append(stops, buffer.Position{Col: len(text) - len(e.doc.Text())})
		}
		if len(stops) > 1000 {
			t.Fatalf("endless loop in %q", e.doc.Text())
		}
	}
	return stops
}

func checkDeleteStops(t *testing.T, opts Options, expected string, left bool, run func(Commands, []buffer.Selection) Result) {
	t.Helper()
	text := deserializePipes(expected)
	stops := repeatDelete(t, opts, text, left, run)
	if got := serializePipes(t, text, stops); got != expected {
		t.Errorf("stops\n got: %q\nwant: %q", got, expected)
	}
}

var cursorLines = []string{"    \tMy First Line\t ", "\tMy Second Line", "    Third Line🐶", "", "1"}

// Ported from VS Code: reference/vscode/src/vs/editor/contrib/wordOperations/test/browser/wordOperations.test.ts
// ('deleteWordLeft ...', 'deleteWordRight ...', the pipe-position cases and the
// issue cases that need no auto-closing).
func TestVSCodeDeleteWord(t *testing.T) {
	run := func(f func(Commands, []buffer.Selection) Result) func(Commands, []buffer.Selection) Result { return f }
	t.Run("deleteWordLeft for non-empty selection", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), cursorLines...).at(sel4{3, 7, 3, 9})
		e.run(Commands.DeleteWordLeft)
		e.assertLine(3, "    Thd Line🐶")
		e.assertCaret(3, 7)
	})
	t.Run("deleteWordLeft for cursor at beginning of document", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), cursorLines...).caret(1, 1)
		e.run(Commands.DeleteWordLeft)
		e.assertLine(1, "    \tMy First Line\t ")
		e.assertCaret(1, 1)
	})
	t.Run("deleteWordLeft for cursor at end of whitespace", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), cursorLines...).caret(3, 11)
		e.run(Commands.DeleteWordLeft)
		e.assertLine(3, "    Line🐶")
		e.assertCaret(3, 5)
	})
	t.Run("deleteWordLeft for cursor just behind a word", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), cursorLines...).caret(2, 11)
		e.run(Commands.DeleteWordLeft)
		e.assertLine(2, "\tMy  Line")
		e.assertCaret(2, 5)
	})
	t.Run("deleteWordLeft for cursor inside of a word", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), cursorLines...).caret(1, 12)
		e.run(Commands.DeleteWordLeft)
		e.assertLine(1, "    \tMy st Line\t ")
		e.assertCaret(1, 9)
	})
	t.Run("deleteWordRight for non-empty selection", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), cursorLines...).at(sel4{3, 7, 3, 9})
		e.run(Commands.DeleteWordRight)
		e.assertLine(3, "    Thd Line🐶")
		e.assertCaret(3, 7)
	})
	t.Run("deleteWordRight for cursor at end of document", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), cursorLines...).caret(5, 3)
		e.run(Commands.DeleteWordRight)
		e.assertLine(5, "1")
		e.assertCaret(5, 2)
	})
	t.Run("deleteWordRight for cursor at beginning of whitespace", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), cursorLines...).caret(3, 1)
		e.run(Commands.DeleteWordRight)
		e.assertLine(3, "Third Line🐶")
		e.assertCaret(3, 1)
	})
	t.Run("deleteWordRight for cursor just before a word", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), cursorLines...).caret(2, 5)
		e.run(Commands.DeleteWordRight)
		e.assertLine(2, "\tMy  Line")
		e.assertCaret(2, 5)
	})
	t.Run("deleteWordRight for cursor inside of a word", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), cursorLines...).caret(1, 11)
		e.run(Commands.DeleteWordRight)
		e.assertLine(1, "    \tMy Fi Line\t ")
		e.assertCaret(1, 11)
	})

	t.Run("deleteWordLeft - issue #832", func(t *testing.T) {
		checkDeleteStops(t, DefaultOptions(), "|   |/* |Just |some |text |a|+= |3 |+|5 |*/|  ", true, run(Commands.DeleteWordLeft))
	})
	t.Run("deleteWordStartLeft", func(t *testing.T) {
		checkDeleteStops(t, DefaultOptions(), "|   |/* |Just |some |text |a|+= |3 |+|5 |*/  ", true, run(Commands.DeleteWordStartLeft))
	})
	t.Run("deleteWordEndLeft", func(t *testing.T) {
		checkDeleteStops(t, DefaultOptions(), "|   /*| Just| some| text| a|+=| 3| +|5| */|  ", true, run(Commands.DeleteWordEndLeft))
	})
	t.Run("deleteWordLeft - issue #24947", func(t *testing.T) {
		for name, f := range map[string]func(Commands, []buffer.Selection) Result{
			"deleteWordLeft": Commands.DeleteWordLeft, "deleteWordStartLeft": Commands.DeleteWordStartLeft, "deleteWordEndLeft": Commands.DeleteWordEndLeft,
		} {
			e := newEd(t, DefaultOptions(), "{", "}").caret(2, 1)
			e.run(f)
			if e.line(1) != "{}" {
				t.Errorf("%s: line 1 = %q", name, e.line(1))
			}
		}
	})
	t.Run("deleteWordRight - issue #832", func(t *testing.T) {
		checkDeleteStops(t, DefaultOptions(), "   |/*| |Just| |some| |text| |a|+=| |3| |+|5|-|3| |*/|  |", false, run(Commands.DeleteWordRight))
	})
	t.Run("deleteWordStartRight", func(t *testing.T) {
		checkDeleteStops(t, DefaultOptions(), "   |/* |Just |some |text |a|+= |3 |+|5|-|3 |*/  |", false, run(Commands.DeleteWordStartRight))
	})
	t.Run("deleteWordEndRight", func(t *testing.T) {
		checkDeleteStops(t, DefaultOptions(), "   /*| Just| some| text| a|+=| 3| +|5|-|3| */|  |", false, run(Commands.DeleteWordEndRight))
	})
	t.Run("deleteWordRight - issue #3882", func(t *testing.T) {
		for name, f := range map[string]func(Commands, []buffer.Selection) Result{
			"deleteWordRight": Commands.DeleteWordRight, "deleteWordStartRight": Commands.DeleteWordStartRight, "deleteWordEndRight": Commands.DeleteWordEndRight,
		} {
			e := newEd(t, DefaultOptions(), "public void Add( int x,", "                 int y )").caret(1, 24)
			e.run(f)
			if e.line(1) != "public void Add( int x,int y )" {
				t.Errorf("%s: line 1 = %q", name, e.line(1))
			}
		}
	})
	t.Run("deleteWordRight - issue #3882 (1): Ctrl+Delete at the end of a line", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "A line with text.", "   And another one").caret(1, 18)
		e.run(Commands.DeleteWordRight)
		e.assertLine(1, "A line with text.And another one")
	})
	t.Run("deleteWordLeft - issue #3882 (2): Ctrl+Backspace at the start of a line", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "A line with text.", "   And another one").caret(2, 1)
		e.run(Commands.DeleteWordLeft)
		e.assertLine(1, "A line with text.   And another one")
	})
}

// Ported from VS Code: wordOperations.test.ts ('deleteInsideWord - ...').
func TestVSCodeDeleteInsideWord(t *testing.T) {
	inside := func(only bool) func(Commands, []buffer.Selection) Result {
		return func(c Commands, s []buffer.Selection) Result { return c.DeleteInsideWord(s, only) }
	}
	t.Run("empty line", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "Line1", "", "Line2").caret(2, 1)
		e.run(inside(false))
		e.assertText("Line1\nLine2")
	})
	t.Run("in whitespace 1", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "Just  some text.").caret(1, 6)
		e.run(inside(false))
		e.assertText("Justsome text.")
	})
	t.Run("in whitespace 2", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "Just     some text.").caret(1, 6)
		e.run(inside(false))
		e.assertText("Justsome text.")
	})
	t.Run("in whitespace 3", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), `Just     "some text.`).caret(1, 6)
		for _, want := range []string{`Just"some text.`, `"some text.`, "some text.", "text.", ".", "", ""} {
			e.run(inside(false))
			e.assertText(want)
		}
	})
	t.Run("in non-words", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "x=3+4+5+6").caret(1, 7)
		for _, want := range []string{"x=3+45+6", "x=3++6", "x=36", "x=", "x", "", ""} {
			e.run(inside(false))
			e.assertText(want)
		}
	})
	t.Run("in words 1", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "This is interesting").caret(1, 7)
		for _, want := range []string{"This interesting", "This", "", ""} {
			e.run(inside(false))
			e.assertText(want)
		}
	})
	t.Run("in words 2", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "This  is  interesting").caret(1, 7)
		for _, want := range []string{"This  interesting", "This", "", ""} {
			e.run(inside(false))
			e.assertText(want)
		}
	})
	t.Run("onlyWord: does not delete whitespace before last word", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "hello world").caret(1, 9)
		e.run(inside(true))
		e.assertText("hello ")
	})
	t.Run("onlyWord: deletes just the word (leaves double spaces)", func(t *testing.T) {
		e := newEd(t, DefaultOptions(), "This is interesting").caret(1, 7)
		e.run(inside(true))
		e.assertText("This  interesting")
	})
}

const kebabSeparators = "!\"#&'()*+,./:;<=>?@[\\]^`{|}·"

func kebabOptions() Options {
	o := DefaultOptions()
	o.WordSeparators = NewWordClassifier(kebabSeparators)
	return o
}

const partBasic = "|start| |line|\n|this|Is|A|Camel|Case|Var|  |this_|is_|a_|snake_|case_|var| |THIS_|IS_|CAPS_|SNAKE| |this_|IS|Mixed|Use|\n|end| |line"

// Ported from VS Code: reference/vscode/src/vs/editor/contrib/wordPartOperations/test/browser/wordPartOperations.test.ts
// (cursorWordPartLeft and cursorWordPartRight).
func TestVSCodeWordPartMovement(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		opts     Options
		start    [2]int // 0, 0 is the end of the text
		stop     [2]int
		left     bool
	}{
		{"cursorWordPartLeft - basic", partBasic, DefaultOptions(), [2]int{}, [2]int{1, 1}, true},
		{"cursorWordPartLeft - issue #53899: whitespace", "|myvar| |=| |'|demonstration|     |of| |selection| |with| |space|'", DefaultOptions(), [2]int{}, [2]int{1, 1}, true},
		{"cursorWordPartLeft - issue #53899: underscores", "|myvar| |=| |'|demonstration_____|of| |selection| |with| |space|'", DefaultOptions(), [2]int{}, [2]int{1, 1}, true},
		{"cursorWordPartRight - basic", "start| |line|\n|this|Is|A|Camel|Case|Var|  |this|_is|_a|_snake|_case|_var| |THIS|_IS|_CAPS|_SNAKE| |this|_IS|Mixed|Use|\n|end| |line|", DefaultOptions(), [2]int{1, 1}, [2]int{3, 9}, false},
		{"cursorWordPartRight - issue #53899: whitespace", "myvar| |=| |'|demonstration|     |of| |selection| |with| |space|'|", DefaultOptions(), [2]int{1, 1}, [2]int{1, 52}, false},
		{"cursorWordPartRight - issue #53899: underscores", "myvar| |=| |'|demonstration|_____of| |selection| |with| |space|'|", DefaultOptions(), [2]int{1, 1}, [2]int{1, 52}, false},
		{"cursorWordPartRight - issue #53899: second case", ";| |--| |1|\n|;|        |--| |2|\n|;|    |#|3|\n|;|   |#|4|", DefaultOptions(), [2]int{1, 1}, [2]int{4, 7}, false},
		{"issue #93239 - cursorWordPartRight", "foo|_bar|", DefaultOptions(), [2]int{1, 1}, [2]int{1, 8}, false},
		{"issue #93239 - cursorWordPartLeft", "|foo_|bar", DefaultOptions(), [2]int{1, 8}, [2]int{1, 1}, true},
		{"issue #158667: cursorWordPartLeft stops at - even when it is not a separator", "|this-|is-|a-|kebab-|case-|var| |THIS-|IS-|CAPS-|KEBAB| |this-|IS|Mixed|Use", kebabOptions(), [2]int{}, [2]int{1, 1}, true},
		{"issue #158667: cursorWordPartRight stops at - even when it is not a separator", "this|-is|-a|-kebab|-case|-var| |THIS|-IS|-CAPS|-KEBAB| |this|-IS|Mixed|Use|", kebabOptions(), [2]int{1, 1}, [2]int{1, 60}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			text := deserializePipes(tt.expected)
			h := newHarness(t, text)
			h.mv.Options = tt.opts
			if tt.start == ([2]int{}) {
				h.cur = CursorOf(buffer.NewCursor(lineEnd(h.mv.Model, lastLine(h.mv.Model))))
			} else {
				h.cur = CursorOf(buffer.NewCursor(h.vsPos(tt.start[0], tt.start[1])))
			}
			action := func() {
				if tt.left {
					h.cur = h.mv.WordPartLeft(h.cur, false, false)
				} else {
					h.cur = h.mv.WordPartRight(h.cur, false)
				}
			}
			stops := repeatUntil(t, h, action, h.vsPos(tt.stop[0], tt.stop[1]))
			if got := serializePipes(t, text, stops); got != tt.expected {
				t.Errorf("stops\n got: %q\nwant: %q", got, tt.expected)
			}
		})
	}
}

// Ported from VS Code: wordPartOperations.test.ts (deleteWordPartLeft and
// deleteWordPartRight).
func TestVSCodeDeleteWordPart(t *testing.T) {
	line1 := "|   |/*| |Just| |some| |text| |a|+=| |3| |+|5|-|3| |*/|  |this|Is|A|Camel|Case|Var|  |this_|is_|a_|snake_|case_|var| |THIS_|IS_|CAPS_|SNAKE| |this_|IS|Mixed|Use"
	right := "   |/*| |Just| |some| |text| |a|+=| |3| |+|5|-|3| |*/|  |this|Is|A|Camel|Case|Var|  |this|_is|_a|_snake|_case|_var| |THIS|_IS|_CAPS|_SNAKE| |this|_IS|Mixed|Use|"
	t.Run("deleteWordPartLeft - basic", func(t *testing.T) {
		checkDeleteStops(t, DefaultOptions(), line1, true, Commands.DeleteWordPartLeft)
	})
	t.Run("deleteWordPartRight - basic", func(t *testing.T) {
		checkDeleteStops(t, DefaultOptions(), right, false, Commands.DeleteWordPartRight)
	})
	t.Run("issue #158667: deleteWordPartLeft stops at -", func(t *testing.T) {
		checkDeleteStops(t, kebabOptions(), "|this-|is-|a-|kebab-|case-|var| |THIS-|IS-|CAPS-|KEBAB| |this-|IS|Mixed|Use", true, Commands.DeleteWordPartLeft)
	})
	t.Run("issue #158667: deleteWordPartRight stops at -", func(t *testing.T) {
		checkDeleteStops(t, kebabOptions(), "this|-is|-a|-kebab|-case|-var| |THIS|-IS|-CAPS|-KEBAB| |this|-IS|Mixed|Use|", false, Commands.DeleteWordPartRight)
	})
}
