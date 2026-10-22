package edit

import (
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/vstest"
	"github.com/rm4n0s/errors"
)

// harness is an editor without a view: a text, a Mover and one cursor. Its
// methods take VS Code positions (1-based lines, UTF-16 columns) as the ported
// tests write them, and convert them to byte columns through vstest.
type harness struct {
	t    *testing.T
	text string
	mv   Mover
	cur  Cursor
}

func newHarness(t *testing.T, text string) *harness {
	t.Helper()
	snap := buffer.New(text).Snapshot()
	return &harness{t: t, text: text, mv: NewMover(NewModel(snap), DefaultOptions(), nil), cur: NewCursor(buffer.Position{})}
}

// vsPos converts a VS Code position, moving a column that is past the end of
// its line to the end, as VS Code's validation does.
func (h *harness) vsPos(line, col int) buffer.Position {
	h.t.Helper()
	p, err := vstest.ToPosition(h.text, line, col)
	if err == nil {
		return p
	}
	appErr, _ := errors.FromError(err)
	if appErr.HasRoute("VSColumnOutOfRange") {
		return lineEnd(h.mv.Model, line-1)
	}
	h.t.Fatalf("VS Code position %d:%d: %v", line, col, err)
	return buffer.Position{}
}

func (h *harness) moveTo(line, col int, selecting bool) {
	h.t.Helper()
	h.cur = h.mv.MoveTo(h.cur, h.vsPos(line, col), selecting)
}

func (h *harness) assertCursor(line, col int) {
	h.t.Helper()
	h.assertSelection(line, col, line, col)
}

func (h *harness) assertSelection(anchorLine, anchorCol, line, col int) {
	h.t.Helper()
	want := buffer.Selection{Anchor: h.vsPos(anchorLine, anchorCol), Active: h.vsPos(line, col)}
	if h.cur.Selection != want {
		h.t.Fatalf("selection = %+v, want %+v (VS Code %d:%d -> %d:%d)", h.cur.Selection, want, anchorLine, anchorCol, line, col)
	}
}

// endOfLine is the VS Code column after the last character of a line.
func endOfLine(line string) int { return len(utf16Units(line)) + 1 }

func utf16Units(s string) []uint16 {
	var out []uint16
	for _, r := range s {
		if r >= 0x10000 {
			r -= 0x10000
			out = append(out, uint16(0xD800+(r>>10)), uint16(0xDC00+(r&0x3FF)))
			continue
		}
		out = append(out, uint16(r))
	}
	return out
}

const (
	line1 = "    \tMy First Line\t "
	line2 = "\tMy Second Line"
	line3 = "    Third Line🐶"
	line4 = ""
	line5 = "1"
)

var cursorText = strings.Join([]string{line1, line2, line3, line4, line5}, "\n")

// Ported from VS Code: reference/vscode/src/vs/editor/test/browser/controller/cursor.test.ts
// ('Editor Controller - Cursor': the cases that move the cursor without word
// wrap, editing or the view). The text of the suite has "\r\n" in two places;
// the buffer holds "\n" only.
func TestVSCodeCursorMovement(t *testing.T) {
	tests := []struct {
		name string
		run  func(h *harness)
	}{
		{"cursor initialized", func(h *harness) { h.assertCursor(1, 1) }},
		{"no move", func(h *harness) { h.moveTo(1, 1, false); h.assertCursor(1, 1) }},
		{"move", func(h *harness) { h.moveTo(1, 2, false); h.assertCursor(1, 2) }},
		{"move in selection mode", func(h *harness) { h.moveTo(1, 2, true); h.assertSelection(1, 1, 1, 2) }},
		{"move beyond line end", func(h *harness) { h.moveTo(1, 25, false); h.assertCursor(1, endOfLine(line1)) }},
		{"move empty line", func(h *harness) { h.moveTo(4, 20, false); h.assertCursor(4, 1) }},
		{"move one char line", func(h *harness) { h.moveTo(5, 20, false); h.assertCursor(5, 2) }},
		{"selection down", func(h *harness) { h.moveTo(2, 1, true); h.assertSelection(1, 1, 2, 1) }},
		{"move and then select", func(h *harness) {
			h.moveTo(2, 3, false)
			h.assertCursor(2, 3)
			h.moveTo(2, 15, true)
			h.assertSelection(2, 3, 2, 15)
			h.moveTo(1, 2, true)
			h.assertSelection(2, 3, 1, 2)
		}},
		// --------- move left
		{"move left on top left position", func(h *harness) { h.cur = h.mv.Left(h.cur, false); h.assertCursor(1, 1) }},
		{"move left", func(h *harness) {
			h.moveTo(1, 3, false)
			h.assertCursor(1, 3)
			h.cur = h.mv.Left(h.cur, false)
			h.assertCursor(1, 2)
		}},
		{"move left with surrogate pair", func(h *harness) {
			h.moveTo(3, 17, false)
			h.assertCursor(3, 17)
			h.cur = h.mv.Left(h.cur, false)
			h.assertCursor(3, 15)
		}},
		{"move left goes to previous row", func(h *harness) {
			h.moveTo(2, 1, false)
			h.cur = h.mv.Left(h.cur, false)
			h.assertCursor(1, 21)
		}},
		{"move left selection", func(h *harness) {
			h.moveTo(2, 1, false)
			h.cur = h.mv.Left(h.cur, true)
			h.assertSelection(2, 1, 1, 21)
		}},
		// --------- move right
		{"move right on bottom right position", func(h *harness) {
			h.moveTo(5, 2, false)
			h.cur = h.mv.Right(h.cur, false)
			h.assertCursor(5, 2)
		}},
		{"move right", func(h *harness) {
			h.moveTo(1, 3, false)
			h.cur = h.mv.Right(h.cur, false)
			h.assertCursor(1, 4)
		}},
		{"move right with surrogate pair", func(h *harness) {
			h.moveTo(3, 15, false)
			h.cur = h.mv.Right(h.cur, false)
			h.assertCursor(3, 17)
		}},
		{"move right goes to next row", func(h *harness) {
			h.moveTo(1, 21, false)
			h.cur = h.mv.Right(h.cur, false)
			h.assertCursor(2, 1)
		}},
		{"move right selection", func(h *harness) {
			h.moveTo(1, 21, false)
			h.cur = h.mv.Right(h.cur, true)
			h.assertSelection(1, 21, 2, 1)
		}},
		// --------- move down
		{"move down", func(h *harness) {
			for _, want := range [][2]int{{2, 1}, {3, 1}, {4, 1}, {5, 1}, {5, 2}} {
				h.cur = h.mv.Down(h.cur, false)
				h.assertCursor(want[0], want[1])
			}
		}},
		{"move down with selection", func(h *harness) {
			for _, want := range [][2]int{{2, 1}, {3, 1}, {4, 1}, {5, 1}, {5, 2}} {
				h.cur = h.mv.Down(h.cur, true)
				h.assertSelection(1, 1, want[0], want[1])
			}
		}},
		{"move down with tabs", func(h *harness) {
			h.moveTo(1, 5, false)
			h.assertCursor(1, 5)
			for _, want := range [][2]int{{2, 2}, {3, 5}, {4, 1}, {5, 2}} {
				h.cur = h.mv.Down(h.cur, false)
				h.assertCursor(want[0], want[1])
			}
		}},
		// --------- move up
		{"move up", func(h *harness) {
			h.moveTo(3, 5, false)
			h.cur = h.mv.Up(h.cur, false)
			h.assertCursor(2, 2)
			h.cur = h.mv.Up(h.cur, false)
			h.assertCursor(1, 5)
		}},
		{"move up with selection", func(h *harness) {
			h.moveTo(3, 5, false)
			h.cur = h.mv.Up(h.cur, true)
			h.assertSelection(3, 5, 2, 2)
			h.cur = h.mv.Up(h.cur, true)
			h.assertSelection(3, 5, 1, 5)
		}},
		{"move up and down with tabs", func(h *harness) {
			h.moveTo(1, 5, false)
			for i := 0; i < 4; i++ {
				h.cur = h.mv.Down(h.cur, false)
			}
			h.assertCursor(5, 2)
			for _, want := range [][2]int{{4, 1}, {3, 5}, {2, 2}, {1, 5}} {
				h.cur = h.mv.Up(h.cur, false)
				h.assertCursor(want[0], want[1])
			}
		}},
		{"move up and down with end of lines starting from a long one", func(h *harness) {
			h.cur = h.mv.End(h.cur, false)
			h.assertCursor(1, endOfLine(line1))
			h.cur = h.mv.End(h.cur, false)
			h.assertCursor(1, endOfLine(line1))
			for i, line := range []string{line2, line3, line4, line5} {
				h.cur = h.mv.Down(h.cur, false)
				h.assertCursor(i+2, endOfLine(line))
			}
			for i := 0; i < 4; i++ {
				h.cur = h.mv.Up(h.cur, false)
			}
			h.assertCursor(1, endOfLine(line1))
		}},
		{"issue #44465: cursor position not correct when move", func(h *harness) {
			h.cur = CursorOf(buffer.NewCursor(h.vsPos(1, 5)))
			// going once up on the first line remembers the offset visual columns
			h.cur = h.mv.Up(h.cur, false)
			h.assertCursor(1, 1)
			h.cur = h.mv.Down(h.cur, false)
			h.assertCursor(2, 2)
			h.cur = h.mv.Up(h.cur, false)
			h.assertCursor(1, 5)
			// going twice up on the first line discards the offset visual columns
			h.cur = h.mv.Up(h.cur, false)
			h.assertCursor(1, 1)
			h.cur = h.mv.Up(h.cur, false)
			h.assertCursor(1, 1)
			h.cur = h.mv.Down(h.cur, false)
			h.assertCursor(2, 1)
		}},
		// --------- move to beginning of line
		{"move to beginning of line", func(h *harness) {
			h.cur = h.mv.Home(h.cur, false)
			h.assertCursor(1, 6)
			h.cur = h.mv.Home(h.cur, false)
			h.assertCursor(1, 1)
		}},
		{"move to beginning of line from within line", func(h *harness) {
			h.moveTo(1, 8, false)
			h.cur = h.mv.Home(h.cur, false)
			h.assertCursor(1, 6)
			h.cur = h.mv.Home(h.cur, false)
			h.assertCursor(1, 1)
		}},
		{"move to beginning of line from whitespace at beginning of line", func(h *harness) {
			h.moveTo(1, 2, false)
			h.cur = h.mv.Home(h.cur, false)
			h.assertCursor(1, 6)
			h.cur = h.mv.Home(h.cur, false)
			h.assertCursor(1, 1)
		}},
		{"move to beginning of line from within line selection", func(h *harness) {
			h.moveTo(1, 8, false)
			h.cur = h.mv.Home(h.cur, true)
			h.assertSelection(1, 8, 1, 6)
			h.cur = h.mv.Home(h.cur, true)
			h.assertSelection(1, 8, 1, 1)
		}},
		{"move to beginning of line with selection multiline forward", func(h *harness) {
			h.moveTo(1, 8, false)
			h.moveTo(3, 9, true)
			h.cur = h.mv.Home(h.cur, false)
			h.assertSelection(3, 5, 3, 5)
		}},
		{"move to beginning of line with selection multiline backward", func(h *harness) {
			h.moveTo(3, 9, false)
			h.moveTo(1, 8, true)
			h.cur = h.mv.Home(h.cur, false)
			h.assertSelection(1, 6, 1, 6)
		}},
		{"move to beginning of line with selection single line forward", func(h *harness) {
			h.moveTo(3, 2, false)
			h.moveTo(3, 9, true)
			h.cur = h.mv.Home(h.cur, false)
			h.assertSelection(3, 5, 3, 5)
		}},
		{"move to beginning of line with selection single line backward", func(h *harness) {
			h.moveTo(3, 9, false)
			h.moveTo(3, 2, true)
			h.cur = h.mv.Home(h.cur, false)
			h.assertSelection(3, 5, 3, 5)
		}},
		{`issue #15401: "End" key is behaving weird when text is selected part 1`, func(h *harness) {
			h.moveTo(1, 8, false)
			h.moveTo(3, 9, true)
			h.cur = h.mv.Home(h.cur, false)
			h.assertSelection(3, 5, 3, 5)
		}},
		{"issue #17011: Shift+home/end now go to the end of the selection start's line, not the selection's end", func(h *harness) {
			h.moveTo(1, 8, false)
			h.moveTo(3, 9, true)
			h.cur = h.mv.Home(h.cur, true)
			h.assertSelection(1, 8, 3, 5)
		}},
		// --------- move to end of line
		{"move to end of line", func(h *harness) {
			h.cur = h.mv.End(h.cur, false)
			h.assertCursor(1, endOfLine(line1))
			h.cur = h.mv.End(h.cur, false)
			h.assertCursor(1, endOfLine(line1))
		}},
		{"move to end of line from within line", func(h *harness) {
			h.moveTo(1, 6, false)
			h.cur = h.mv.End(h.cur, false)
			h.assertCursor(1, endOfLine(line1))
			h.cur = h.mv.End(h.cur, false)
			h.assertCursor(1, endOfLine(line1))
		}},
		{"move to end of line from whitespace at end of line", func(h *harness) {
			h.moveTo(1, 20, false)
			h.cur = h.mv.End(h.cur, false)
			h.assertCursor(1, endOfLine(line1))
			h.cur = h.mv.End(h.cur, false)
			h.assertCursor(1, endOfLine(line1))
		}},
		{"move to end of line from within line selection", func(h *harness) {
			h.moveTo(1, 6, false)
			h.cur = h.mv.End(h.cur, true)
			h.assertSelection(1, 6, 1, endOfLine(line1))
			h.cur = h.mv.End(h.cur, true)
			h.assertSelection(1, 6, 1, endOfLine(line1))
		}},
		{"move to end of line with selection multiline forward", func(h *harness) {
			h.moveTo(1, 1, false)
			h.moveTo(3, 9, true)
			h.cur = h.mv.End(h.cur, false)
			h.assertSelection(3, 17, 3, 17)
		}},
		{"move to end of line with selection multiline backward", func(h *harness) {
			h.moveTo(3, 9, false)
			h.moveTo(1, 1, true)
			h.cur = h.mv.End(h.cur, false)
			h.assertSelection(1, 21, 1, 21)
		}},
		{"move to end of line with selection single line forward", func(h *harness) {
			h.moveTo(3, 1, false)
			h.moveTo(3, 9, true)
			h.cur = h.mv.End(h.cur, false)
			h.assertSelection(3, 17, 3, 17)
		}},
		{"move to end of line with selection single line backward", func(h *harness) {
			h.moveTo(3, 9, false)
			h.moveTo(3, 1, true)
			h.cur = h.mv.End(h.cur, false)
			h.assertSelection(3, 17, 3, 17)
		}},
		{`issue #15401: "End" key is behaving weird when text is selected part 2`, func(h *harness) {
			h.moveTo(1, 1, false)
			h.moveTo(3, 9, true)
			h.cur = h.mv.End(h.cur, false)
			h.assertSelection(3, 17, 3, 17)
		}},
		// --------- move to beginning of buffer
		{"move to beginning of buffer", func(h *harness) { h.cur = h.mv.DocStart(h.cur, false); h.assertCursor(1, 1) }},
		{"move to beginning of buffer from within first line", func(h *harness) {
			h.moveTo(1, 3, false)
			h.cur = h.mv.DocStart(h.cur, false)
			h.assertCursor(1, 1)
		}},
		{"move to beginning of buffer from within another line", func(h *harness) {
			h.moveTo(3, 3, false)
			h.cur = h.mv.DocStart(h.cur, false)
			h.assertCursor(1, 1)
		}},
		{"move to beginning of buffer from within first line selection", func(h *harness) {
			h.moveTo(1, 3, false)
			h.cur = h.mv.DocStart(h.cur, true)
			h.assertSelection(1, 3, 1, 1)
		}},
		{"move to beginning of buffer from within another line selection", func(h *harness) {
			h.moveTo(3, 3, false)
			h.cur = h.mv.DocStart(h.cur, true)
			h.assertSelection(3, 3, 1, 1)
		}},
		// --------- move to end of buffer
		{"move to end of buffer", func(h *harness) { h.cur = h.mv.DocEnd(h.cur, false); h.assertCursor(5, endOfLine(line5)) }},
		{"move to end of buffer from within last line", func(h *harness) {
			h.moveTo(5, 1, false)
			h.cur = h.mv.DocEnd(h.cur, false)
			h.assertCursor(5, endOfLine(line5))
		}},
		{"move to end of buffer from within another line", func(h *harness) {
			h.moveTo(3, 3, false)
			h.cur = h.mv.DocEnd(h.cur, false)
			h.assertCursor(5, endOfLine(line5))
		}},
		{"move to end of buffer from within last line selection", func(h *harness) {
			h.moveTo(5, 1, false)
			h.cur = h.mv.DocEnd(h.cur, true)
			h.assertSelection(5, 1, 5, endOfLine(line5))
		}},
		{"move to end of buffer from within another line selection", func(h *harness) {
			h.moveTo(3, 3, false)
			h.cur = h.mv.DocEnd(h.cur, true)
			h.assertSelection(3, 3, 5, endOfLine(line5))
		}},
		// --------- misc
		{"select all", func(h *harness) {
			h.cur = h.mv.SelectAll()
			h.assertSelection(1, 1, 5, endOfLine(line5))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.run(newHarness(t, cursorText)) })
	}
}

// Ported from VS Code: cursor.test.ts ('grapheme breaking'): the cursor moves over
// whole grapheme clusters, and vertical moves keep the visual column.
func TestVSCodeGraphemeBreaking(t *testing.T) {
	// The second line is six times "a" with a combining tilde; the third six
	// characters of a base with a variation selector; the fourth a Tamil syllable.
	text := strings.Join([]string{"abcabc", strings.Repeat("a\u0303", 6), strings.Repeat("\u8fbb\U000E0100", 3), "\u0baa\u0bc1"}, "\n")
	h := newHarness(t, text)
	set := func(line, col int) { h.cur = CursorOf(buffer.NewCursor(h.vsPos(line, col))) }
	right := func(line, col int) { h.cur = h.mv.Right(h.cur, false); h.assertCursor(line, col) }
	left := func(line, col int) { h.cur = h.mv.Left(h.cur, false); h.assertCursor(line, col) }
	down := func(line, col int) { h.cur = h.mv.Down(h.cur, false); h.assertCursor(line, col) }
	up := func(line, col int) { h.cur = h.mv.Up(h.cur, false); h.assertCursor(line, col) }
	set(2, 1)
	right(2, 3)
	left(2, 1)
	set(3, 1)
	right(3, 4)
	left(3, 1)
	set(4, 1)
	right(4, 3)
	left(4, 1)
	set(1, 3)
	down(2, 5)
	down(3, 4)
	up(2, 5)
	up(1, 3)
}
