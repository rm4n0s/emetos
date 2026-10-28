package emulator

import (
	"strings"
	"testing"
)

// TestParserStates: the transitions of the DEC parser (vt100.net/emu/dec_ansi_parser), one row per behavior.
func TestParserStates(t *testing.T) {
	runScreenCases(t, []screenCase{
		{name: "CSI with controls inside executes them", in: "\x1b[1\n;3Hx", want: "  x", x: 3, y: 0},
		{name: "ESC restarts a sequence", in: "\x1b[1;\x1b[2;3Hx", want: "\n  x", x: 3, y: 1},
		{name: "CAN cancels a sequence", in: "\x1b[1;2\x18Hx", want: "Hx", x: 2, y: 0},
		{name: "SUB cancels a sequence", in: "\x1b[1;2\x1aHx", want: "Hx", x: 2, y: 0},
		{name: "CSI ignore: a marker in the middle", in: "\x1b[1?2Hx", want: "x", x: 1, y: 0},
		{name: "CSI ignore: a parameter after an intermediate", in: "\x1b[ 1Hx", want: "x", x: 1, y: 0},
		{name: "CSI ignore ends at a final byte", in: "\x1b[1?2Hx\x1b[2;2Hy", want: "x\n y", x: 2, y: 1},
		{name: "unknown final bytes are ignored", in: "\x1b[1;2yx\x1b[?1;2yy", want: "xy", x: 2, y: 0},
		{name: "unknown ESC sequences are ignored", in: "\x1b%Gx\x1b ax\x1b!x", want: "xx", x: 2, y: 0},
		{name: "DEL is ignored in the ground state", in: "a\x7fb", want: "ab", x: 2, y: 0},
		{name: "DEL is ignored inside a sequence", in: "\x1b[1\x7f;2Hx", want: " x", x: 2, y: 0},
		{name: "a sequence with 40 parameters", in: "\x1b[" + strings.Repeat("1;", 40) + "5Hx", want: "x", x: 1, y: 0},
		{name: "a huge parameter is held", in: "\x1b[99999999999999999999;1Hx", want: "\n\n\n\nx", x: 1, y: 4},
		{name: "colon separates parameters of every sequence", in: "\x1b[1:2Hx", want: " x", x: 2, y: 0},
		{name: "SOS PM and APC strings are ignored", in: "a\x1bXtext\x1b\\b\x1b^text\x1b\\c\x1b_text\x1b\\d", want: "abcd", x: 4, y: 0},
		{name: "DCS strings are ignored", in: "a\x1bP1;2|data\x1b\\b", want: "ab", x: 2, y: 0},
		{name: "a DCS with a bad parameter is ignored", in: "a\x1bP1:2|data\x1b\\b", want: "ab", x: 2, y: 0},
		{name: "OSC ends with BEL", in: "a\x1b]0;title\x07b", want: "ab", x: 2, y: 0},
		{name: "OSC ends with ST", in: "a\x1b]0;title\x1b\\b", want: "ab", x: 2, y: 0},
		{name: "OSC ends with C1 ST", in: "a\x1b]0;title\u009cb", want: "ab", x: 2, y: 0},
		{name: "OSC started by C1", in: "a\u009d0;title\x07b", want: "ab", x: 2, y: 0},
		{name: "CSI started by C1", in: "\u009b2;3Hx", want: "\n  x", x: 3, y: 1},
		{name: "C1 IND", in: "a\u0084b", want: "a\n b", x: 2, y: 1},
		{name: "C1 NEL", in: "a\u0085b", want: "a\nb", x: 1, y: 1},
		{name: "C1 RI", in: "a\n\u008db", want: "ab", x: 2, y: 0},
		{name: "C1 HTS", in: "\x1b[1;4H\u0088\x1b[1;1H\tX", want: "   X", x: 4, y: 0},
		{name: "controls in the middle of text", in: "ab\r\ncd\ref", want: "ab\nef", x: 2, y: 1},
		{name: "invalid UTF-8 prints the replacement character", in: "a\xffb", want: "a�b", x: 3, y: 0},
		{name: "a lone continuation byte", in: "a\x80b", want: "a�b", x: 3, y: 0},
		{name: "a lead byte cut by ASCII", in: "a\xe3\x81b", want: "a�b", x: 3, y: 0},
		{name: "an overlong sequence", in: "a\xc0\x80b", want: "a��b", x: 4, y: 0},
		{name: "a surrogate", in: "a\xed\xa0\x80b", want: "a���b", x: 5, y: 0},
		{name: "a character above U+10FFFF", in: "a\xf4\x90\x80\x80b", want: "a����b", x: 6, y: 0},
		{name: "a non-ASCII character inside a CSI ends it", in: "\x1b[1é2Hx", want: "x", x: 1, y: 0},
		{name: "a non-ASCII character after ESC", in: "\x1bébx", want: "bx", x: 2, y: 0},
		{name: "the C1 range is controls, not text", in: "a\u0090b\u009c", want: "a", x: 1, y: 0},
	})
}

// TestParserSplit: a sequence cut at every byte gives what the whole did (the reads of a pseudo-terminal end anywhere).
func TestParserSplit(t *testing.T) {
	inputs := []string{
		"hello \x1b[1;31mred\x1b[0m world\r\n\x1b[2;4Hx",
		"日本語のテキスト\u0301 😀 and e\u0301\r\n",
		"\x1b]0;a title\x07\x1b]8;;http://example.com\x1b\\link\x1b]8;;\x1b\\",
		"\x1b[38;2;1;2;3m\x1b[4:3munder\x1b[0m\x1bP$qm\x1b\\",
		"\x1b[?1049hALT\x1b[?1049l\u009b3;3H\u0084x",
		"bad\xff\xe3\x81\xc0 bytes \xf0\x9f",
	}
	for i, in := range inputs {
		whole := newRig(6, 20)
		whole.feed(in)
		wantScreen, wantReply := whole.screenString(), whole.take()
		wx, wy := whole.cursorPos()
		for cut := 1; cut < len(in); cut++ {
			r := newRig(6, 20)
			r.feed(in[:cut])
			r.feed(in[cut:])
			x, y := r.cursorPos()
			if got := r.screenString(); got != wantScreen || x != wx || y != wy || r.take() != wantReply {
				t.Fatalf("input %d cut at %d: screen %q cursor (%d,%d), want %q (%d,%d)", i, cut, got, x, y, wantScreen, wx, wy)
			}
		}
		// And one byte at a time.
		r := newRig(6, 20)
		for k := 0; k < len(in); k++ {
			r.feed(in[k : k+1])
		}
		if got := r.screenString(); got != wantScreen {
			t.Errorf("input %d byte by byte: %q, want %q", i, got, wantScreen)
		}
	}
}

// TestParserLimits: the bounds of SPEC §10.25 "Memory".
func TestParserLimits(t *testing.T) {
	t.Run("an OSC string over the cap is ignored", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("\x1b]0;" + strings.Repeat("x", MaxOSCLength+10) + "\x07")
		assertEq(t, "titles", len(r.titles), 0)
		if cap(r.osc) > 2*MaxOSCLength {
			t.Errorf("the OSC buffer grew to %d", cap(r.osc))
		}
		r.feed("\x1b]0;ok\x07")
		assertEq(t, "title after", r.Title(), "ok")
	})
	t.Run("an OSC string at the cap is kept", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("\x1b]0;" + strings.Repeat("y", MaxOSCLength-3) + "\x07")
		assertEq(t, "titles", len(r.titles), 1)
	})
	t.Run("a title is cut", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("\x1b]2;" + strings.Repeat("é", maxTitle) + "\x07")
		if len(r.Title()) > maxTitle {
			t.Errorf("title is %d bytes", len(r.Title()))
		}
	})
	t.Run("parameters are capped", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("\x1b[" + strings.Repeat("1;", 100000) + "m")
		if r.nparams > MaxParams {
			t.Errorf("nparams = %d", r.nparams)
		}
	})
	t.Run("a DCS string is capped", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("\x1bP$q" + strings.Repeat("m", 100000) + "\x1b\\")
		if len(r.dcsBuf) > maxDCS {
			t.Errorf("dcs buffer = %d", len(r.dcsBuf))
		}
		assertEq(t, "reply", r.take(), "\x1bP0$r\x1b\\")
	})
	t.Run("the size is clamped", func(t *testing.T) {
		e := New(100000, 100000, Options{Scrollback: 1 << 40})
		rows, cols := e.Size()
		assertEq(t, "rows", rows, MaxRows)
		assertEq(t, "cols", cols, MaxCols)
		e.Resize(0, -5)
		rows, cols = e.Size()
		assertEq(t, "rows", rows, 1)
		assertEq(t, "cols", cols, 1)
	})
}

// TestOSC: the operating system commands (ctlseqs "Operating System Commands").
func TestOSC(t *testing.T) {
	t.Run("titles", func(t *testing.T) {
		r := newRig(3, 10)
		var icons []string
		r.cb.IconName = func(s string) { icons = append(icons, s) }
		r.feed("\x1b]2;two\x07\x1b]0;zero\x07\x1b]1;one\x07")
		assertEq(t, "titles", strings.Join(r.titles, ","), "two,zero")
		assertEq(t, "icons", strings.Join(icons, ","), "zero,one")
		assertEq(t, "Title()", r.Title(), "zero")
	})
	t.Run("a title loses its control characters", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("\x1b]2;a\x01b\x7fc\x07")
		assertEq(t, "title", r.Title(), "abc")
	})
	t.Run("a title may hold UTF-8", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("\x1b]2;日本語 😀\x07")
		assertEq(t, "title", r.Title(), "日本語 😀")
	})
	t.Run("the working folder", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("\x1b]7;file://host/home/me/my%20dir\x07")
		assertEq(t, "cwd", strings.Join(r.cwds, ","), "host/home/me/my dir")
		assertEq(t, "WorkingFolder", r.WorkingFolder(), "/home/me/my dir")
		r.feed("\x1b]7;http://host/x\x07\x1b]7;%zz\x07")
		assertEq(t, "ignored", len(r.cwds), 1)
	})
	t.Run("unknown codes are passed on with the cursor position", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("ab\r\ncd\x1b]633;A\x07\x1b]133;D;0\x1b\\\x1b]1337;SetMark\x07\x1b]9;note\x07")
		if len(r.oscs) != 4 {
			t.Fatalf("events = %+v", r.oscs)
		}
		assertEq(t, "code", r.oscs[0].Code, 633)
		assertEq(t, "data", r.oscs[0].Data, "A")
		assertEq(t, "line", r.oscs[0].Line, int64(1))
		assertEq(t, "col", r.oscs[0].Col, 2)
		assertEq(t, "data of 133", r.oscs[1].Data, "D;0")
		assertEq(t, "1337", r.oscs[2].Data, "SetMark")
		assertEq(t, "9", r.oscs[3].Code, 9)
	})
	t.Run("a code with no data", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("\x1b]777\x07")
		assertEq(t, "events", len(r.oscs), 1)
		assertEq(t, "data", r.oscs[0].Data, "")
	})
	t.Run("a bad code is ignored", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("\x1b]x;y\x07\x1b]-1;y\x07\x1b];y\x07")
		assertEq(t, "events", len(r.oscs), 0)
	})
	t.Run("OSC 52 goes to the clipboard callback", func(t *testing.T) {
		var sel, data string
		e := New(3, 10, Options{Callbacks: Callbacks{Clipboard: func(s, d string) { sel, data = s, d }}})
		if _, err := e.Write([]byte("\x1b]52;c;aGVsbG8=\x07")); err != nil {
			t.Fatal(err)
		}
		assertEq(t, "selection", sel, "c")
		assertEq(t, "data", data, "aGVsbG8=")
	})
	t.Run("OSC 52 does nothing by itself", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("\x1b]52;c;?\x07")
		assertEq(t, "reply", r.take(), "")
	})
	t.Run("palette changes call back", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("\x1b]4;1;#ff0000\x07\x1b]10;#00ff00\x07")
		assertEq(t, "calls", r.palette, 2)
		var f Frame
		r.Snapshot(&f)
		assertEq(t, "palette 1", f.Palette[1], uint32(0xff0000))
		assertEq(t, "fg", f.Fg, uint32(0x00ff00))
	})
	t.Run("the bell", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("a\ab\a")
		assertEq(t, "bells", r.bells, 2)
	})
	t.Run("BEL inside an OSC is not a bell", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("\x1b]0;x\x07")
		assertEq(t, "bells", r.bells, 0)
	})
}

// TestHyperlinks: OSC 8.
func TestHyperlinks(t *testing.T) {
	r := newRig(3, 20)
	r.feed("a\x1b]8;;http://one\x1b\\bc\x1b]8;id=x;http://two\x1b\\d\x1b]8;;\x1b\\e")
	var f Frame
	r.ReadLines(0, 1, &f)
	var got []string
	for x := 0; x < 5; x++ {
		l := f.LinkOf(f.StyleOf(f.Cell(0, x)))
		got = append(got, l.ID+"|"+l.URL)
	}
	assertEq(t, "links", strings.Join(got, " "), "| |http://one |http://one x|http://two |")
	t.Run("the link survives SGR 0", func(t *testing.T) {
		r := newRig(3, 20)
		r.feed("\x1b]8;;http://one\x1b\\\x1b[1m\x1b[0mx")
		_, st := r.cell(0, 0)
		if st.Link == 0 {
			t.Error("link lost")
		}
	})
	t.Run("a link over the cap is ignored", func(t *testing.T) {
		r := newRig(3, 20)
		r.feed("\x1b]8;;http://" + strings.Repeat("a", maxURL) + "\x1b\\x")
		_, st := r.cell(0, 0)
		assertEq(t, "link", st.Link, uint16(0))
	})
	t.Run("a link with no semicolon is ignored", func(t *testing.T) {
		r := newRig(3, 20)
		r.feed("\x1b]8\x1b\\\x1b]8;x\x1b\\y")
		_, st := r.cell(0, 0)
		assertEq(t, "link", st.Link, uint16(0))
	})
	t.Run("many different links are compacted", func(t *testing.T) {
		r := newRig(3, 20)
		for i := 0; i < MaxLinks+500; i++ {
			r.feed("\x1b]8;;http://h/" + itoa(i) + "\x1b\\" + "\x1b[1;1Hx")
		}
		if len(r.st.links) > MaxLinks {
			t.Errorf("links = %d", len(r.st.links))
		}
		_, st := r.cell(0, 0)
		var f Frame
		r.ReadLines(0, 1, &f)
		assertEq(t, "last link", f.LinkOf(st).URL, "http://h/"+itoa(MaxLinks+499))
	})
}
