package emulator

import (
	"strings"
	"testing"
)

// allLines returns the text of every line of the buffer, scrollback first, with rows that autowrap joined marked "⏎".
func allLines(r *rig) []string {
	var out []string
	for i := 0; i < r.LineCount(); i++ {
		text, wrapped := r.LineText(i)
		if wrapped {
			text += "⏎"
		}
		out = append(out, text)
	}
	return out
}

func TestResizeColumns(t *testing.T) {
	cases := []struct {
		name       string
		rows, cols int
		in         string
		newRows    int
		newCols    int
		want       []string // all lines, scrollback first
		x, y       int
	}{
		{"shrinking cuts a long line", 4, 10, "abcdefghij", 4, 5, []string{"abcde⏎", "fghij", "", ""}, 4, 1},
		{"growing joins wrapped rows", 4, 5, "abcdefghij", 4, 10, []string{"abcdefghij", "", "", ""}, 9, 0},
		{"hard line breaks stay", 4, 10, "ab\r\ncd", 4, 5, []string{"ab", "cd", "", ""}, 2, 1},
		{"the cursor keeps its place in the text", 4, 20, "hello world", 4, 6, []string{"hello⏎", "world", "", ""}, 5, 1},
		{"the cursor in the middle of a line", 4, 20, "hello world\x1b[1;3H", 4, 6, []string{"hello⏎", "world", "", ""}, 2, 0},
		{"a cursor after trailing spaces", 4, 20, "ab    \x1b[1;5H", 4, 3, []string{"ab⏎", "", "", ""}, 1, 1},
		{"a full row with the cursor waiting to wrap", 4, 5, "abcde", 4, 10, []string{"abcde", "", "", ""}, 5, 0},
		{"a full row shrunk exactly", 4, 10, "abcdef", 4, 3, []string{"abc⏎", "def", "", ""}, 2, 1},
		{"a wide character moves to the next row", 4, 4, "aあb", 4, 2, []string{"a⏎", "あ⏎", "b", ""}, 1, 2},
		{"a wide character joins again", 4, 2, "aあb", 4, 4, []string{"aあb", "", "", ""}, 3, 0},
		{"clusters are carried over", 4, 6, "e\u0301xe\u0301y", 4, 2, []string{"e\u0301x⏎", "e\u0301y", "", ""}, 1, 1},
		{"a one column terminal", 4, 4, "abc", 4, 1, []string{"a⏎", "b⏎", "c", ""}, 0, 2},
		{"lines that scrolled off are reflowed too", 3, 6, "aaaaaaaaaaaa\r\nbbbbbb\r\ncc\r\ndd", 3, 12,
			[]string{"aaaaaaaaaaaa", "bbbbbb", "cc", "dd"}, 2, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(tc.rows, tc.cols)
			r.feed(tc.in)
			r.Resize(tc.newRows, tc.newCols)
			got := allLines(r)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("lines = %q, want %q", got, tc.want)
			}
			x, y := r.cursorPos()
			if x != tc.x || y != tc.y {
				t.Errorf("cursor = (%d,%d), want (%d,%d)", x, y, tc.x, tc.y)
			}
		})
	}
	t.Run("the wide fill cell is made and removed", func(t *testing.T) {
		r := newRig(4, 4)
		r.feed("aあb")
		r.Resize(4, 2)
		c, _ := r.cell(0, 1)
		assertEq(t, "fill", c.Flags, CellWideFill)
		r.Resize(4, 4)
		c, _ = r.cell(0, 3)
		assertEq(t, "no fill", c.Flags, uint16(0))
	})
	t.Run("styles and links stay with their text", func(t *testing.T) {
		r := newRig(4, 10)
		r.feed("\x1b[31mred\x1b[0m \x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\")
		r.Resize(4, 4)
		_, st := r.cell(0, 0)
		assertEq(t, "fg", st.Fg, IndexedColor(1))
		var f Frame
		r.ReadLines(1, 1, &f)
		assertEq(t, "link", f.LinkOf(f.StyleOf(f.Cell(0, 0))).URL, "http://x")
	})
	t.Run("the scroll region and the tab stops are reset", func(t *testing.T) {
		r := newRig(5, 10)
		r.feed("\x1b[2;4r")
		r.Resize(5, 20)
		s := r.mainScr
		assertEq(t, "top", s.top, 0)
		assertEq(t, "bottom", s.bottom, 4)
		assertEq(t, "tab 16", s.tabs[16], true)
		assertEq(t, "tab 8", s.tabs[8], true)
		assertEq(t, "tab 7", s.tabs[7], false)
	})
	t.Run("a logical line is cut at the cap", func(t *testing.T) {
		r := newRigSB(10, 100, 1000)
		r.feed(strings.Repeat("x", 25000))
		r.Resize(10, 50)
		wrapped := 0
		total := 0
		for _, l := range allLines(r) {
			if strings.HasSuffix(l, "⏎") {
				wrapped++
			}
			total++
		}
		if wrapped == total-1 {
			t.Errorf("the whole line of 25000 cells was joined (%d rows)", total)
		}
		assertEq(t, "cells kept", strings.Count(strings.Join(allLines(r), ""), "x"), 25000)
	})
	t.Run("scrollback limit after a reflow", func(t *testing.T) {
		r := newRigSB(3, 10, 5)
		for i := 0; i < 30; i++ {
			r.feed("0123456789abcdef\r\n")
		}
		r.Resize(3, 4)
		if sb := r.Scrollback(); sb > 5 {
			t.Errorf("scrollback = %d, limit 5", sb)
		}
	})
}

func TestResizeRows(t *testing.T) {
	t.Run("growing pulls lines back from the scrollback", func(t *testing.T) {
		r := newRigSB(3, 10, 10)
		r.feed("1\r\n2\r\n3\r\n4\r\n5")
		assertEq(t, "before", strings.Join(r.screen(), ","), "3,4,5")
		r.Resize(5, 10)
		assertEq(t, "after", strings.Join(r.screen(), ","), "1,2,3,4,5")
		assertEq(t, "scrollback", r.Scrollback(), 0)
		_, y := r.cursorPos()
		assertEq(t, "cursor row", y, 4)
	})
	t.Run("growing with no scrollback adds blank rows below", func(t *testing.T) {
		r := newRigSB(3, 10, 0)
		r.feed("1\r\n2")
		r.Resize(5, 10)
		assertEq(t, "screen", strings.Join(r.screen(), ","), "1,2,,,")
		_, y := r.cursorPos()
		assertEq(t, "cursor row", y, 1)
	})
	t.Run("shrinking drops blank rows below the cursor first", func(t *testing.T) {
		r := newRig(5, 10)
		r.feed("1\r\n2")
		r.Resize(3, 10)
		assertEq(t, "screen", strings.Join(r.screen(), ","), "1,2,")
		assertEq(t, "scrollback", r.Scrollback(), 0)
	})
	t.Run("shrinking pushes the top into the scrollback", func(t *testing.T) {
		r := newRig(5, 10)
		r.feed("1\r\n2\r\n3\r\n4\r\n5")
		r.Resize(3, 10)
		assertEq(t, "screen", strings.Join(r.screen(), ","), "3,4,5")
		assertEq(t, "scrollback", r.Scrollback(), 2)
		_, y := r.cursorPos()
		assertEq(t, "cursor row", y, 2)
	})
	t.Run("shrinking with the cursor at the top cuts the bottom", func(t *testing.T) {
		r := newRig(5, 10)
		r.feed("1\r\n2\r\n3\r\n4\r\n5\x1b[1;1H")
		r.Resize(3, 10)
		assertEq(t, "screen", strings.Join(r.screen(), ","), "1,2,3")
		_, y := r.cursorPos()
		assertEq(t, "cursor row", y, 0)
	})
	t.Run("grow and shrink again", func(t *testing.T) {
		r := newRig(4, 10)
		r.feed("1\r\n2\r\n3\r\n4")
		r.Resize(8, 10)
		r.Resize(4, 10)
		assertEq(t, "screen", strings.Join(r.screen(), ","), "1,2,3,4")
	})
	t.Run("the scrollback limit holds after shrinking", func(t *testing.T) {
		r := newRigSB(10, 10, 2)
		for i := 0; i < 10; i++ {
			r.feed(itoa(i) + "\r\n")
		}
		r.Resize(2, 10)
		if sb := r.Scrollback(); sb > 2 {
			t.Errorf("scrollback = %d", sb)
		}
	})
	t.Run("the saved cursor is clamped", func(t *testing.T) {
		r := newRig(10, 10)
		r.feed("\x1b[9;9H\x1b7")
		r.Resize(3, 4)
		r.feed("\x1b8")
		x, y := r.cursorPos()
		if x > 3 || y > 2 {
			t.Errorf("cursor = (%d,%d)", x, y)
		}
	})
	t.Run("the alternate screen is cut or extended", func(t *testing.T) {
		r := newRig(4, 10)
		r.feed("\x1b[?1049h1234567890\r\nabc\x1b[4;3Hz")
		r.Resize(3, 5)
		assertEq(t, "screen", strings.Join(r.screen(), ","), "12345,abc,")
		r.Resize(5, 12)
		assertEq(t, "screen after growing", strings.Join(r.screen(), ","), "12345,abc,,,")
		r.feed("\x1b[?1049l")
	})
	t.Run("a resize to the same size changes nothing", func(t *testing.T) {
		r := newRig(4, 10)
		var f Frame
		r.Snapshot(&f)
		r.Resize(4, 10)
		assertEq(t, "changed", r.Snapshot(&f), false)
	})
	t.Run("the main buffer is reflowed while the alternate screen is shown", func(t *testing.T) {
		r := newRig(4, 10)
		r.feed("abcdefghij\x1b[?1049hALT")
		r.Resize(4, 5)
		r.feed("\x1b[?1049l")
		assertEq(t, "main", strings.Join(allLines(r), "|"), "abcde⏎|fghij||")
	})
}
