package emulator

import (
	"strings"
	"testing"
)

// screenCase is a table row: the bytes fed to a fresh terminal and what it shows afterwards.
type screenCase struct {
	name       string
	rows, cols int
	in         string
	want       string // the screen rows joined by "\n", trailing empty rows dropped
	x, y       int    // the cursor; -1 for "do not check"
}

func runScreenCases(t *testing.T, cases []screenCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rows, cols := tc.rows, tc.cols
			if rows == 0 {
				rows, cols = 5, 10
			}
			r := newRig(rows, cols)
			r.feed(tc.in)
			if got := r.screenString(); got != tc.want {
				t.Errorf("screen:\n%q\nwant:\n%q", got, tc.want)
			}
			if tc.x >= 0 {
				x, y := r.cursorPos()
				if x != tc.x || y != tc.y {
					t.Errorf("cursor = (%d,%d), want (%d,%d)", x, y, tc.x, tc.y)
				}
			}
		})
	}
}

const fiveRows = "1\r\n2\r\n3\r\n4\r\n5"

// TestCursorMovement: ECMA-48 8.3.18 to 8.3.22 (CUB, CUD, CUF, CUP, CUU), 8.3.9 CHA, 8.3.12 CNL, 8.3.13 CPL, 8.3.57 HPA,
// 8.3.59 HPR, 8.3.63 HVP, 8.3.158 VPA, 8.3.160 VPR, VT510 DECSTBM and DECOM.
func TestCursorMovement(t *testing.T) {
	runScreenCases(t, []screenCase{
		{name: "CUP default is home", in: "abc\x1b[H", want: "abc", x: 0, y: 0},
		{name: "CUP row and column", in: "\x1b[3;4H", want: "", x: 3, y: 2},
		{name: "CUP clamps to the screen", in: "\x1b[99;99H", want: "", x: 9, y: 4},
		{name: "CUP zero is one", in: "\x1b[0;0H", want: "", x: 0, y: 0},
		{name: "CUP row only", in: "\x1b[3H", want: "", x: 0, y: 2},
		{name: "HVP", in: "\x1b[2;3f", want: "", x: 2, y: 1},
		{name: "CUU stops at the top", in: "\x1b[3;1H\x1b[9A", want: "", x: 0, y: 0},
		{name: "CUU stops at the top margin inside the region", in: "\x1b[2;4r\x1b[3;1H\x1b[9A", want: "", x: 0, y: 1},
		{name: "CUU above the region goes to the screen top", in: "\x1b[3;5r\x1b[2;1H\x1b[9A", want: "", x: 0, y: 0},
		{name: "CUD stops at the bottom margin", in: "\x1b[1;3r\x1b[1;1H\x1b[9B", want: "", x: 0, y: 2},
		{name: "CUD below the region goes to the screen bottom", in: "\x1b[1;3r\x1b[4;1H\x1b[9B", want: "", x: 0, y: 4},
		{name: "CUF clamps", in: "\x1b[99C", want: "", x: 9, y: 0},
		{name: "CUB clamps", in: "\x1b[5C\x1b[99D", want: "", x: 0, y: 0},
		{name: "CUF zero is one", in: "\x1b[0C", want: "", x: 1, y: 0},
		{name: "CNL", in: "\x1b[1;5H\x1b[2E", want: "", x: 0, y: 2},
		{name: "CPL", in: "\x1b[4;5H\x1b[2F", want: "", x: 0, y: 1},
		{name: "CHA", in: "\x1b[3;1H\x1b[7G", want: "", x: 6, y: 2},
		{name: "VPA", in: "\x1b[1;4H\x1b[4d", want: "", x: 3, y: 3},
		{name: "HPA", in: "\x1b[5`", want: "", x: 4, y: 0},
		{name: "HPR", in: "\x1b[2a", want: "", x: 2, y: 0},
		{name: "VPR", in: "\x1b[3e", want: "", x: 0, y: 3},
		{name: "origin mode homes to the top margin", in: "\x1b[2;4r\x1b[?6h", want: "", x: 0, y: 1},
		{name: "origin mode CUP is relative", in: "\x1b[2;4r\x1b[?6h\x1b[2;3H", want: "", x: 2, y: 2},
		{name: "origin mode CUP stays in the region", in: "\x1b[2;4r\x1b[?6h\x1b[99;1H", want: "", x: 0, y: 3},
		{name: "DECSTBM refuses a region of one row", in: "\x1b[3;3r\x1b[2;2H", want: "", x: 1, y: 1},
		{name: "DECSTBM homes the cursor", in: "\x1b[3;3H\x1b[2;4r", want: "", x: 0, y: 0},
		{name: "backspace", in: "ab\b", want: "ab", x: 1, y: 0},
		{name: "backspace at column 0", in: "\b\b", want: "", x: 0, y: 0},
		{name: "reverse wrap goes up", in: "\x1b[?45h0123456789a\b\b\b", want: "0123456789\na", x: 8, y: 0},
		{name: "no reverse wrap by default", in: "0123456789a\b\b\b", want: "0123456789\na", x: 0, y: 1},
	})
}

// TestPrinting: autowrap (VT510 DECAWM), insert (IRM), new line (LNM) mode and the character sets.
func TestPrinting(t *testing.T) {
	runScreenCases(t, []screenCase{
		{name: "autowrap", in: "0123456789A", want: "0123456789\nA", x: 1, y: 1},
		{name: "the cursor waits at the last column", in: "0123456789", want: "0123456789", x: 9, y: 0},
		{name: "autowrap off overwrites the last column", in: "\x1b[?7l0123456789ABC", want: "012345678C", x: 9, y: 0},
		{name: "CR clears the pending wrap", in: "0123456789\rX", want: "X123456789", x: 1, y: 0},
		{name: "BS clears the pending wrap", in: "0123456789\bX", want: "01234567X9", x: 9, y: 0},
		{name: "insert mode", in: "abcdef\x1b[1;3H\x1b[4hXY", want: "abXYcdef", x: 4, y: 0},
		{name: "insert mode pushes text off the edge", in: "abcdefghij\x1b[1;1H\x1b[4hX", want: "Xabcdefghi", x: 1, y: 0},
		{name: "replace mode again", in: "abcdef\x1b[1;3H\x1b[4h\x1b[4lXY", want: "abXYef", x: 4, y: 0},
		{name: "new line mode", in: "a\x1b[20h\nb", want: "a\nb", x: 1, y: 1},
		{name: "LF keeps the column", in: "a\nb", want: "a\n b", x: 2, y: 1},
		{name: "DEC special graphics", in: "\x1b(0lqk\x1b(B", want: "┌─┐", x: 3, y: 0},
		{name: "DEC special graphics arrows and block", in: "\x1b(0+,-.0\x1b(B", want: "→←↑↓▮", x: 5, y: 0},
		{name: "DEC special graphics leaves other bytes", in: "\x1b(0aA1\x1b(B", want: "▒A1", x: 3, y: 0},
		{name: "SO and SI", in: "\x1b)0\x0eq\x0fq", want: "─q", x: 2, y: 0},
		{name: "UK set", in: "\x1b(A#\x1b(B#", want: "£#", x: 2, y: 0},
		{name: "REP", in: "a\x1b[3b", want: "aaaa", x: 4, y: 0},
		{name: "REP with nothing printed", in: "\x1b[3b", want: "", x: 0, y: 0},
		{name: "DECALN", in: "x\x1b#8", want: strings.Join([]string{"EEEEEEEEEE", "EEEEEEEEEE", "EEEEEEEEEE", "EEEEEEEEEE", "EEEEEEEEEE"}, "\n"), x: 0, y: 0},
		{name: "controls are not printed", in: "a\x00\x01\x02b\x7fc", want: "abc", x: 3, y: 0},
	})
}

// TestTabs: ECMA-48 8.3.60 HT, 8.3.62 HTS, 8.3.154 TBC, 8.3.10 CHT, 8.3.7 CBT.
func TestTabs(t *testing.T) {
	runScreenCases(t, []screenCase{
		{name: "HT goes to column 8", in: "a\tb", want: "a       b", x: 9, y: 0},
		{name: "HT at the last column stays", in: "\x1b[1;10H\t", want: "", x: 9, y: 0},
		{name: "HTS sets a stop", in: "\x1b[1;4H\x1bH\x1b[1;1H\tX", want: "   X", x: 4, y: 0},
		{name: "TBC 0 clears the stop at the cursor", in: "\x1b[1;9H\x1b[g\x1b[1;1H\tX", want: "         X", x: 9, y: 0},
		{name: "TBC 3 clears all", in: "\x1b[3g\tX", want: "         X", x: 9, y: 0},
		{name: "CHT", in: "\x1b[2IX", want: "                X", rows: 3, cols: 20, x: 17, y: 0},
		{name: "CBT", in: "\x1b[1;20H\x1b[Z", want: "", rows: 3, cols: 20, x: 16, y: 0},
		{name: "CBT stops at column 0", in: "\x1b[1;3H\x1b[9Z", want: "", x: 0, y: 0},
	})
}

// TestErasing: ECMA-48 8.3.39 ED, 8.3.41 EL, 8.3.38 ECH, 8.3.64 ICH, 8.3.26 DCH.
func TestErasing(t *testing.T) {
	fill := "\x1b[H" + strings.Repeat("aaaaaaaaaa", 3) + "\x1b[2;5H"
	runScreenCases(t, []screenCase{
		{name: "ED 0", in: fill + "\x1b[J", want: "aaaaaaaaaa\naaaa", x: 4, y: 1},
		{name: "ED 1", in: fill + "\x1b[1J", want: "\n     aaaaa\naaaaaaaaaa", x: 4, y: 1},
		{name: "ED 2", in: fill + "\x1b[2J", want: "", x: 4, y: 1},
		{name: "ED default is 0", in: fill + "\x1b[0J", want: "aaaaaaaaaa\naaaa", x: 4, y: 1},
		{name: "EL 0", in: fill + "\x1b[K", want: "aaaaaaaaaa\naaaa\naaaaaaaaaa", x: 4, y: 1},
		{name: "EL 1", in: fill + "\x1b[1K", want: "aaaaaaaaaa\n     aaaaa\naaaaaaaaaa", x: 4, y: 1},
		{name: "EL 2", in: fill + "\x1b[2K", want: "aaaaaaaaaa\n\naaaaaaaaaa", x: 4, y: 1},
		{name: "ECH", in: "abcdef\x1b[1;2H\x1b[3X", want: "a   ef", x: 1, y: 0},
		{name: "ECH past the edge", in: "abcdef\x1b[1;5H\x1b[99X", want: "abcd", x: 4, y: 0},
		{name: "ICH", in: "abcdef\x1b[1;2H\x1b[2@", want: "a  bcdef", x: 1, y: 0},
		{name: "ICH loses text off the edge", in: "abcdefghij\x1b[1;9H\x1b[5@", want: "abcdefgh", x: 8, y: 0},
		{name: "DCH", in: "abcdef\x1b[1;2H\x1b[2P", want: "adef", x: 1, y: 0},
		{name: "DCH past the edge", in: "abcdef\x1b[1;2H\x1b[99P", want: "a", x: 1, y: 0},
		{name: "DECSED is ED", in: fill + "\x1b[?2J", want: "", x: 4, y: 1},
		{name: "DECSEL is EL", in: fill + "\x1b[?2K", want: "aaaaaaaaaa\n\naaaaaaaaaa", x: 4, y: 1},
	})
}

// TestScrolling: ECMA-48 8.3.67 IL, 8.3.32 DL, 8.3.147 SU, 8.3.113 SD, 8.3.104 RI, 8.3.86 NEL, VT510 IND and DECSTBM.
func TestScrolling(t *testing.T) {
	runScreenCases(t, []screenCase{
		{name: "IL", in: fiveRows + "\x1b[3;1H\x1b[L", want: "1\n2\n\n3\n4", x: 0, y: 2},
		{name: "IL moves the cursor to the first column", in: fiveRows + "\x1b[3;1H\x1b[2G\x1b[L", want: "1\n2\n\n3\n4", x: 0, y: 2},
		{name: "IL count", in: fiveRows + "\x1b[2;1H\x1b[2L", want: "1\n\n\n2\n3", x: 0, y: 1},
		{name: "IL count over the region", in: fiveRows + "\x1b[2;1H\x1b[99L", want: "1", x: 0, y: 1},
		{name: "DL", in: fiveRows + "\x1b[2;1H\x1b[M", want: "1\n3\n4\n5", x: 0, y: 1},
		{name: "DL count", in: fiveRows + "\x1b[2;1H\x1b[2M", want: "1\n4\n5", x: 0, y: 1},
		{name: "IL in a region", in: fiveRows + "\x1b[2;4r\x1b[3;1H\x1b[L", want: "1\n2\n\n3\n5", x: 0, y: 2},
		{name: "DL in a region", in: fiveRows + "\x1b[2;4r\x1b[2;1H\x1b[M", want: "1\n3\n4\n\n5", x: 0, y: 1},
		{name: "IL outside the region does nothing", in: fiveRows + "\x1b[2;4r\x1b[5;1H\x1b[L", want: fiveRows[:0] + "1\n2\n3\n4\n5", x: 0, y: 4},
		{name: "SU", in: fiveRows + "\x1b[S", want: "2\n3\n4\n5", x: -1},
		{name: "SU count", in: fiveRows + "\x1b[2S", want: "3\n4\n5", x: -1},
		{name: "SD", in: fiveRows + "\x1b[T", want: "\n1\n2\n3\n4", x: -1},
		{name: "SD with the caret form", in: fiveRows + "\x1b[2^", want: "\n\n1\n2\n3", x: -1},
		{name: "SU in a region", in: fiveRows + "\x1b[2;4r\x1b[S", want: "1\n3\n4\n\n5", x: -1},
		{name: "RI at the top scrolls down", in: fiveRows + "\x1b[H\x1bM", want: "\n1\n2\n3\n4", x: 0, y: 0},
		{name: "RI moves up", in: fiveRows + "\x1bM", want: "1\n2\n3\n4\n5", x: 1, y: 3},
		{name: "IND at the bottom scrolls up", in: fiveRows + "\x1b[5;1H\x1bD", want: "2\n3\n4\n5", x: 0, y: 4},
		{name: "IND moves down", in: "a\x1bDb", want: "a\n b", x: 2, y: 1},
		{name: "NEL", in: "ab\x1bEcd", want: "ab\ncd", x: 2, y: 1},
		{name: "LF at the bottom margin scrolls the region", in: fiveRows + "\x1b[2;4r\x1b[4;1H\n", want: "1\n3\n4\n\n5", x: 0, y: 3},
		{name: "LF below the region moves down", in: fiveRows + "\x1b[2;4r\x1b[5;1H\n", want: "1\n2\n3\n4\n5", x: 0, y: 4},
		{name: "RI at the top margin scrolls the region down", in: fiveRows + "\x1b[2;4r\x1b[2;1H\x1bM", want: "1\n\n2\n3\n5", x: 0, y: 1},
	})
}

// TestSavedCursor: VT510 DECSC and DECRC, ctlseqs CSI s and CSI u.
func TestSavedCursor(t *testing.T) {
	runScreenCases(t, []screenCase{
		{name: "DECSC and DECRC", in: "\x1b[2;3H\x1b7\x1b[1;1H\x1b8", want: "", x: 2, y: 1},
		{name: "SCOSC and SCORC", in: "\x1b[2;3H\x1b[s\x1b[1;1H\x1b[u", want: "", x: 2, y: 1},
		{name: "DECRC with nothing saved goes home", in: "\x1b[2;3H\x1b8", want: "", x: 0, y: 0},
		{name: "DECRC restores the pen", in: "\x1b[31m\x1b7\x1b[0m\x1b8A", want: "A", x: 1, y: 0},
		{name: "DECRC restores the pending wrap", in: "0123456789\x1b7\x1b[1;1H\x1b8X", want: "0123456789\nX", x: 1, y: 1},
		{name: "DECRC restores origin mode", in: "\x1b[2;4r\x1b[?6h\x1b7\x1b[?6l\x1b8\x1b[1;1H", want: "", x: 0, y: 1},
	})
}

// TestFullReset: ECMA-48 8.3.105 RIS and VT510 DECSTR.
func TestFullReset(t *testing.T) {
	r := newRig(5, 10)
	r.feed("abc\x1b[31m\x1b[?7l\x1b[2;4r\x1b[4h\x1b=")
	r.feed("\x1bc")
	assertEq(t, "screen after RIS", r.screenString(), "")
	m := r.Modes()
	assertEq(t, "autowrap", m.Autowrap, true)
	assertEq(t, "insert", m.Insert, false)
	assertEq(t, "keypad", m.AppKeypad, false)
	r.feed("x")
	_, st := r.cell(0, 0)
	assertEq(t, "pen after RIS", st, Style{})

	r.feed("\x1b[31m\x1b[?7l\x1b[2;4r\x1b[4h\x1b[1;5H")
	r.feed("\x1b[!p")
	m = r.Modes()
	assertEq(t, "autowrap after DECSTR", m.Autowrap, true)
	assertEq(t, "insert after DECSTR", m.Insert, false)
	x, y := r.cursorPos()
	assertEq(t, "x after DECSTR", x, 4)
	assertEq(t, "y after DECSTR", y, 0)
}

// TestAlternateScreen: ctlseqs DECSET 47, 1047, 1048, 1049.
func TestAlternateScreen(t *testing.T) {
	t.Run("1049 saves the cursor and clears the alternate screen", func(t *testing.T) {
		r := newRig(5, 10)
		r.feed("main\x1b[?1049hALT")
		assertEq(t, "alt screen", r.screenString(), "    ALT")
		assertEq(t, "mode", r.Modes().AltScreen, true)
		r.feed("\x1b[?1049l")
		assertEq(t, "main screen", r.screenString(), "main")
		x, y := r.cursorPos()
		assertEq(t, "x", x, 4)
		assertEq(t, "y", y, 0)
		r.feed("\x1b[?1049h")
		assertEq(t, "cleared again", r.screenString(), "")
	})
	t.Run("47 keeps the alternate screen", func(t *testing.T) {
		r := newRig(5, 10)
		r.feed("main\x1b[?47hX\x1b[?47l\x1b[?47h")
		assertEq(t, "alt screen", r.screenString(), "    X")
	})
	t.Run("1047 clears the alternate screen when leaving", func(t *testing.T) {
		r := newRig(5, 10)
		r.feed("main\x1b[?1047hX\x1b[?1047l\x1b[?1047h")
		assertEq(t, "alt screen", r.screenString(), "")
	})
	t.Run("the alternate screen has no scrollback", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("\x1b[?1049h1\r\n2\r\n3\r\n4\r\n5")
		assertEq(t, "screen", r.screenString(), "3\n4\n5")
		r.feed("\x1b[?1049l")
		assertEq(t, "main scrollback", r.Scrollback(), 0)
	})
	t.Run("1048 saves and restores the cursor", func(t *testing.T) {
		r := newRig(5, 10)
		r.feed("\x1b[2;3H\x1b[?1048h\x1b[1;1H\x1b[?1048l")
		x, y := r.cursorPos()
		assertEq(t, "x", x, 2)
		assertEq(t, "y", y, 1)
	})
}

// TestScrollback: the limit, ED 3, and the lines that region scrolls do not keep.
func TestScrollback(t *testing.T) {
	t.Run("limit", func(t *testing.T) {
		r := newRigSB(5, 10, 3)
		for i := 1; i <= 20; i++ {
			r.feed("L" + itoa(i) + "\r\n")
		}
		assertEq(t, "scrollback", r.Scrollback(), 3)
		text, _ := r.LineText(0)
		assertEq(t, "oldest", text, "L14")
		assertEq(t, "screen", r.screenString(), "L17\nL18\nL19\nL20")
	})
	t.Run("no scrollback", func(t *testing.T) {
		r := newRigSB(3, 10, 0)
		r.feed("1\r\n2\r\n3\r\n4")
		assertEq(t, "scrollback", r.Scrollback(), 0)
		assertEq(t, "screen", r.screenString(), "2\n3\n4")
	})
	t.Run("ED 3", func(t *testing.T) {
		r := newRigSB(3, 10, 10)
		r.feed("1\r\n2\r\n3\r\n4\r\n5")
		assertEq(t, "before", r.Scrollback(), 2)
		r.feed("\x1b[3J")
		assertEq(t, "after", r.Scrollback(), 0)
		assertEq(t, "screen kept", r.screenString(), "3\n4\n5")
	})
	t.Run("a region scroll keeps nothing", func(t *testing.T) {
		r := newRigSB(5, 10, 10)
		r.feed(fiveRows + "\x1b[2;4r\x1b[4;1H\n\n\n")
		assertEq(t, "scrollback", r.Scrollback(), 0)
	})
	t.Run("the whole screen region keeps lines", func(t *testing.T) {
		r := newRigSB(5, 10, 10)
		r.feed(fiveRows + "\x1b[1;5r\x1b[5;1H\n")
		assertEq(t, "scrollback", r.Scrollback(), 1)
	})
	t.Run("absolute numbers survive trimming", func(t *testing.T) {
		r := newRigSB(2, 10, 2)
		r.feed("a\r\nb\r\nc\r\nd\r\ne\x1b]777;x\x07")
		if len(r.oscs) != 1 {
			t.Fatalf("osc events = %v", r.oscs)
		}
		assertEq(t, "line", r.oscs[0].Line, int64(4))
	})
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for ; n > 0; n /= 10 {
		b = append([]byte{byte('0' + n%10)}, b...)
	}
	return string(b)
}

// TestWideAndCombining: wide characters (East Asian Width), combining marks and clusters in the buffer.
func TestWideAndCombining(t *testing.T) {
	t.Run("a wide character takes two cells", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("aあb")
		c, _ := r.cell(0, 1)
		assertEq(t, "head flags", c.Flags, CellWide)
		c, _ = r.cell(0, 2)
		assertEq(t, "tail flags", c.Flags, CellTail)
		c, _ = r.cell(0, 3)
		assertEq(t, "next", c.Rune, uint32('b'))
		assertEq(t, "text", r.screenString(), "aあb")
		x, _ := r.cursorPos()
		assertEq(t, "x", x, 4)
	})
	t.Run("emoji are wide", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("😀x")
		c, _ := r.cell(0, 0)
		assertEq(t, "flags", c.Flags, CellWide)
		x, _ := r.cursorPos()
		assertEq(t, "x", x, 3)
	})
	t.Run("a wide character that does not fit wraps", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("012345678あ")
		assertEq(t, "screen", r.screenString(), "012345678\nあ")
		c, _ := r.cell(0, 9)
		assertEq(t, "fill", c.Flags, CellWideFill)
		var f Frame
		r.ReadLines(0, 2, &f)
		assertEq(t, "wrapped", f.Wrapped[0], true)
	})
	t.Run("a wide character at the edge without autowrap is dropped", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("\x1b[?7l012345678あ")
		assertEq(t, "screen", r.screenString(), "012345678")
	})
	t.Run("a wide character on a one column terminal is dropped", func(t *testing.T) {
		r := newRig(3, 1)
		r.feed("あa")
		assertEq(t, "screen", r.screenString(), "a")
	})
	t.Run("overwriting the left half blanks the right half", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("あ\x1b[1;1Hx")
		assertEq(t, "screen", r.screenString(), "x")
		c, _ := r.cell(0, 1)
		assertEq(t, "cell 1", c.Flags, uint16(0))
	})
	t.Run("overwriting the right half blanks the left half", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("あ\x1b[1;2Hx")
		assertEq(t, "screen", r.screenString(), " x")
		c, _ := r.cell(0, 0)
		assertEq(t, "cell 0", c.Flags, uint16(0))
	})
	t.Run("overwriting with a run", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("あいう\x1b[1;2Hxyz")
		assertEq(t, "screen", r.screenString(), " xyzう")
	})
	t.Run("erasing half of a wide character blanks both", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("あい\x1b[1;2H\x1b[K")
		assertEq(t, "screen", r.screenString(), "")
		r.feed("あい\x1b[1;2H\x1b[1K")
		assertEq(t, "screen", r.screenString(), "   い")
	})
	t.Run("deleting characters repairs wide characters", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("aあb\x1b[1;1H\x1b[P")
		assertEq(t, "after DCH 1", r.screenString(), "あb")
		r.feed("\x1b[1;1H\x1b[P")
		assertEq(t, "after DCH 2", r.screenString(), " b")
	})
	t.Run("inserting characters repairs wide characters", func(t *testing.T) {
		r := newRig(3, 4)
		r.feed("abあ\x1b[1;1H\x1b[@")
		assertEq(t, "screen", r.screenString(), " ab")
	})
	t.Run("a combining mark joins the cell before it", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("e\u0301x")
		var f Frame
		r.ReadLines(0, 1, &f)
		assertEq(t, "cluster flag", f.Cell(0, 0).Flags, CellCluster)
		assertEq(t, "text", f.Text(0, 0), "e\u0301")
		assertEq(t, "next", f.Text(0, 1), "x")
		x, _ := r.cursorPos()
		assertEq(t, "x", x, 2)
	})
	t.Run("several marks", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("a\u0301̂̃")
		var f Frame
		r.ReadLines(0, 1, &f)
		assertEq(t, "text", f.Text(0, 0), "a\u0301̂̃")
	})
	t.Run("a mark at the start of the line is dropped", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("\u0301a")
		assertEq(t, "screen", r.screenString(), "a")
	})
	t.Run("a mark after a wide character", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("あ゙x")
		var f Frame
		r.ReadLines(0, 1, &f)
		assertEq(t, "text", f.Text(0, 0), "あ゙")
		assertEq(t, "x", f.Text(0, 2), "x")
	})
	t.Run("a mark after the last column", func(t *testing.T) {
		r := newRig(3, 5)
		r.feed("abcde\u0301")
		var f Frame
		r.ReadLines(0, 1, &f)
		assertEq(t, "text", f.Text(0, 4), "e\u0301")
	})
	t.Run("a zero width joiner follows the wcwidth model", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("👨\u200d👩")
		var f Frame
		r.ReadLines(0, 1, &f)
		assertEq(t, "first", f.Text(0, 0), "👨\u200d")
		assertEq(t, "second", f.Text(0, 2), "👩")
	})
	t.Run("clusters are capped", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("a" + strings.Repeat("\u0301", 200) + "b")
		var f Frame
		r.ReadLines(0, 1, &f)
		if n := len(f.Text(0, 0)); n > maxClusterBytes {
			t.Errorf("cluster is %d bytes, cap %d", n, maxClusterBytes)
		}
		assertEq(t, "next", f.Text(0, 1), "b")
	})
	t.Run("rewriting a cluster many times keeps the row small", func(t *testing.T) {
		r := newRig(3, 10)
		for i := 0; i < 5000; i++ {
			r.feed("\x1b[1;1He\u0301")
		}
		l := r.mainScr.row(0)
		if l.ext != nil && len(l.ext.clusters) > 2*10+16 {
			t.Errorf("row keeps %d cluster strings", len(l.ext.clusters))
		}
	})
}
