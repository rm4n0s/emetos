package emulator

import "testing"

// TestSGR: ECMA-48 8.3.117 and ctlseqs "CSI Pm m".
func TestSGR(t *testing.T) {
	idx := IndexedColor
	cases := []struct {
		name string
		in   string
		want Style
	}{
		{"none", "", Style{}},
		{"bold", "\x1b[1m", Style{Attrs: AttrBold}},
		{"dim", "\x1b[2m", Style{Attrs: AttrDim}},
		{"italic", "\x1b[3m", Style{Attrs: AttrItalic}},
		{"underline", "\x1b[4m", Style{Underline: UnderlineSingle}},
		{"blink", "\x1b[5m", Style{Attrs: AttrBlink}},
		{"rapid blink is blink", "\x1b[6m", Style{Attrs: AttrBlink}},
		{"inverse", "\x1b[7m", Style{Attrs: AttrInverse}},
		{"invisible", "\x1b[8m", Style{Attrs: AttrInvisible}},
		{"strikethrough", "\x1b[9m", Style{Attrs: AttrStrike}},
		{"double underline", "\x1b[21m", Style{Underline: UnderlineDouble}},
		{"overline", "\x1b[53m", Style{Attrs: AttrOverline}},
		{"several", "\x1b[1;3;4;31m", Style{Attrs: AttrBold | AttrItalic, Underline: UnderlineSingle, Fg: idx(1)}},
		{"reset", "\x1b[1;31m\x1b[0m", Style{}},
		{"empty parameter resets", "\x1b[1;31m\x1b[m", Style{}},
		{"reset in the middle", "\x1b[1;0;3m", Style{Attrs: AttrItalic}},
		{"22 clears bold and dim", "\x1b[1;2m\x1b[22m", Style{}},
		{"23 clears italic", "\x1b[3m\x1b[23m", Style{}},
		{"24 clears underline", "\x1b[4m\x1b[24m", Style{}},
		{"25 clears blink", "\x1b[5m\x1b[25m", Style{}},
		{"27 clears inverse", "\x1b[7m\x1b[27m", Style{}},
		{"28 clears invisible", "\x1b[8m\x1b[28m", Style{}},
		{"29 clears strikethrough", "\x1b[9m\x1b[29m", Style{}},
		{"55 clears overline", "\x1b[53m\x1b[55m", Style{}},
		{"foreground 30-37", "\x1b[34m", Style{Fg: idx(4)}},
		{"background 40-47", "\x1b[43m", Style{Bg: idx(3)}},
		{"bright foreground 90-97", "\x1b[91m", Style{Fg: idx(9)}},
		{"bright background 100-107", "\x1b[107m", Style{Bg: idx(15)}},
		{"39 default foreground", "\x1b[31m\x1b[39m", Style{}},
		{"49 default background", "\x1b[41m\x1b[49m", Style{}},
		{"256 colors foreground", "\x1b[38;5;196m", Style{Fg: idx(196)}},
		{"256 colors background", "\x1b[48;5;21m", Style{Bg: idx(21)}},
		{"true color foreground", "\x1b[38;2;1;2;3m", Style{Fg: RGBColor(1, 2, 3)}},
		{"true color background", "\x1b[48;2;255;128;0m", Style{Bg: RGBColor(255, 128, 0)}},
		{"true color then more", "\x1b[38;2;1;2;3;1m", Style{Fg: RGBColor(1, 2, 3), Attrs: AttrBold}},
		{"colon 256", "\x1b[38:5:196m", Style{Fg: idx(196)}},
		{"colon true color", "\x1b[38:2:10:20:30m", Style{Fg: RGBColor(10, 20, 30)}},
		{"colon true color with colorspace", "\x1b[38:2::10:20:30m", Style{Fg: RGBColor(10, 20, 30)}},
		{"colon background", "\x1b[48:2::1:2:3;1m", Style{Bg: RGBColor(1, 2, 3), Attrs: AttrBold}},
		{"underline double", "\x1b[4:2m", Style{Underline: UnderlineDouble}},
		{"underline curly", "\x1b[4:3m", Style{Underline: UnderlineCurly}},
		{"underline dotted", "\x1b[4:4m", Style{Underline: UnderlineDotted}},
		{"underline dashed", "\x1b[4:5m", Style{Underline: UnderlineDashed}},
		{"underline none", "\x1b[4m\x1b[4:0m", Style{}},
		{"underline then bold", "\x1b[4:3;1m", Style{Underline: UnderlineCurly, Attrs: AttrBold}},
		{"underline color 256", "\x1b[58;5;9m", Style{UnderlineColor: idx(9)}},
		{"underline color true", "\x1b[58:2::1:2:3m", Style{UnderlineColor: RGBColor(1, 2, 3)}},
		{"59 resets the underline color", "\x1b[58;5;9m\x1b[59m", Style{}},
		{"component above 255 is clamped", "\x1b[38;2;300;0;0m", Style{Fg: RGBColor(255, 0, 0)}},
		{"truncated 38;5 is ignored", "\x1b[1;38;5m", Style{Attrs: AttrBold}},
		{"truncated 38;2 is ignored", "\x1b[38;2;1;2m", Style{}},
		{"unknown 38;9 skips only the 9", "\x1b[38;9;1m", Style{Attrs: AttrBold}},
		{"fonts are ignored", "\x1b[11;1m", Style{Attrs: AttrBold}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(3, 10)
			r.feed(tc.in + "x")
			_, got := r.cell(0, 0)
			if got != tc.want {
				t.Errorf("style = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestBackgroundColorErase: erased cells take the background of the pen (ctlseqs: the BCE capability of xterm-256color)
// and nothing else of it.
func TestBackgroundColorErase(t *testing.T) {
	cases := []struct{ name, in string }{
		{"EL", "\x1b[41;1m\x1b[K"},
		{"ED", "\x1b[41;1m\x1b[2J"},
		{"ECH", "\x1b[41;1m\x1b[3X"},
		{"DCH", "\x1b[41;1m\x1b[1;1H\x1b[P"},
		{"IL", "\x1b[41;1m\x1b[L"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(3, 10)
			r.feed("abcdefghij\x1b[H" + tc.in)
			x := 0
			if tc.name == "DCH" {
				x = 9
			}
			c, st := r.cell(0, x)
			assertEq(t, "rune", c.Rune, uint32(0))
			assertEq(t, "style", st, Style{Bg: IndexedColor(1)})
		})
	}
	t.Run("a pen with a link erases without the link", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("\x1b]8;;http://a\x1b\\\x1b[41m\x1b[K")
		_, st := r.cell(0, 0)
		assertEq(t, "style", st, Style{Bg: IndexedColor(1)})
	})
}

// TestStyleTable: styles are shared, and a full table is compacted.
func TestStyleTable(t *testing.T) {
	t.Run("the same style is one entry", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("\x1b[31mab\x1b[0m\x1b[31mcd")
		a, _ := r.cell(0, 0)
		d, _ := r.cell(0, 3)
		assertEq(t, "style index", a.Style, d.Style)
		assertEq(t, "table size", len(r.st.styles), 2)
	})
	t.Run("a full table is compacted", func(t *testing.T) {
		r := newRig(4, 100)
		// 70,000 different styles, written one per cell over and over: only the last cells survive.
		for i := 0; i < 70000; i++ {
			r.feed("\x1b[38;2;" + itoa(i%256) + ";" + itoa((i/256)%256) + ";" + itoa(i/65536) + "m")
			r.feed("\x1b[" + itoa(1+(i/100)%4) + ";" + itoa(1+i%100) + "Hx")
		}
		if n := len(r.st.styles); n > MaxStyles {
			t.Errorf("table has %d entries", n)
		}
		if r.st.gen == 0 {
			t.Errorf("the table was never compacted")
		}
		// Every cell still shows a style that exists.
		var f Frame
		r.ReadLines(0, 4, &f)
		for y := 0; y < 4; y++ {
			for x := 0; x < 100; x++ {
				if int(f.Cell(y, x).Style) >= len(f.Styles) {
					t.Fatalf("cell (%d,%d) has style %d of %d", y, x, f.Cell(y, x).Style, len(f.Styles))
				}
			}
		}
		// The most recent cell shows the style that was in force.
		_, st := r.cell(int((69999/100)%4), 69999%100)
		assertEq(t, "last style", st, Style{Fg: RGBColor(uint8(69999%256), uint8((69999/256)%256), uint8(69999/65536))})
	})
}
