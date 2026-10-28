package emulator

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// TestRepeat: REP (ECMA-48 8.3.103) through every path.
func TestRepeat(t *testing.T) {
	runScreenCases(t, []screenCase{
		{name: "ASCII", in: "ab\x1b[3b", want: "abbbb", x: 5, y: 0},
		{name: "wraps like text", in: "a\x1b[12b", want: "aaaaaaaaaa\naaa", x: 3, y: 1},
		{name: "wide character", in: "あ\x1b[2b", want: "あああ", x: 6, y: 0},
		{name: "wide character at the edge", in: "0123456あ\x1b[1b", want: "0123456あ\nあ", x: 2, y: 1},
		{name: "insert mode", in: "xyz\x1b[1;1H\x1b[4h1\x1b[2b", want: "111xyz", x: 3, y: 0},
		{name: "after a character set translation", in: "\x1b(0q\x1b[2b", want: "───", x: 3, y: 0},
		{name: "a mark is not repeated", in: "a\u0301\x1b[2b", want: "a\u0301aa", x: 3, y: 0},
		{name: "the count is held to the screen", in: "a\x1b[99999999b", want: strings.Repeat("aaaaaaaaaa\n", 4) + "a", x: 1, y: 4},
	})
}

// TestAmplification: sequences that are short and ask for a lot of work stay cheap.
func TestAmplification(t *testing.T) {
	if raceEnabled {
		t.Skip("timing is not checked under the race detector")
	}
	for _, tc := range []struct{ name, unit string }{
		{"REP", "a\x1b[99999999b"},
		{"REP of a wide character", "あ\x1b[99999999b"},
		{"ICH", "\x1b[99999999@"},
		{"IL", "\x1b[99999999L"},
		{"SU", "\x1b[99999999S"},
		{"ED", "\x1b[2J"},
		{"DECALN", "\x1b#8"},
		{"RIS", "\x1bc"},
		{"screens", "\x1b[?1049h\x1b[?1049l"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := New(50, 150, Options{Scrollback: 40})
			in := []byte(strings.Repeat(tc.unit, (1<<16)/len(tc.unit)))
			start := time.Now()
			if _, err := e.Write(in); err != nil {
				t.Fatal(err)
			}
			if d := time.Since(start); d > 2*time.Second {
				t.Errorf("64 KiB of %q took %v", tc.unit, d)
			}
		})
	}
}

// TestStyleTableSaturation: when more styles are alive than the table holds, new ones are rounded to the palette, never
// refused with a panic, and the table does not grow past its cap or rebuild at every style.
func TestStyleTableSaturation(t *testing.T) {
	r := newRigSB(5, 200, 2000)
	rebuilds := func() uint32 { return r.st.gen }
	var gen uint32
	for i := 0; i < MaxStyles+20000; i++ {
		r.feed("\x1b[38;2;" + itoa(i%256) + ";" + itoa((i/256)%256) + ";" + itoa(i/65536) + "mx")
		if i%190 == 189 {
			r.feed("\r\n")
		}
		if i == MaxStyles+100 {
			gen = rebuilds()
		}
	}
	if len(r.st.styles) > MaxStyles {
		t.Errorf("table has %d entries", len(r.st.styles))
	}
	if n := rebuilds() - gen; n > 2 {
		t.Errorf("the table was rebuilt %d times for 20,000 styles", n)
	}
	// New text still prints, in its own color or a rounded one.
	r.feed("\x1b[38;2;250;10;10mZ")
	if !strings.HasSuffix(r.screenString(), "Z") {
		t.Errorf("the screen does not end with Z: %q", r.screenString())
	}
	if msg := checkInvariants(r.Emulator); msg != "" {
		t.Error(msg)
	}
}

func TestQuantize(t *testing.T) {
	cases := []struct {
		in   Color
		want Color
	}{
		{0, 0},
		{IndexedColor(9), IndexedColor(9)},
		{RGBColor(0, 0, 0), IndexedColor(16)},
		{RGBColor(255, 255, 255), IndexedColor(231)},
		{RGBColor(255, 0, 0), IndexedColor(196)},
		{RGBColor(0, 255, 0), IndexedColor(46)},
		{RGBColor(0, 0, 255), IndexedColor(21)},
		{RGBColor(128, 128, 128), IndexedColor(16 + 36*3 + 6*3 + 3)},
	}
	for _, tc := range cases {
		assertEq(t, "quantize", quantizeColor(tc.in), tc.want)
	}
}

// TestInvalidInputKeepsInvariants: the odd corners.
func TestInvalidInputKeepsInvariants(t *testing.T) {
	inputs := map[string]string{
		"all bytes": func() string {
			var b []byte
			for i := 0; i < 256; i++ {
				b = append(b, byte(i))
			}
			return string(b)
		}(),
		"escape storm":     strings.Repeat("\x1b[\x1b]\x1bP\x1b_\x1b\\", 200),
		"unterminated OSC": "\x1b]0;" + strings.Repeat("a", 100000),
		"unterminated DCS": "\x1bP" + strings.Repeat("1;", 100000),
		"every C1": func() string {
			var b []byte
			for r := rune(0x80); r < 0xa0; r++ {
				b = utf8.AppendRune(b, r)
			}
			return string(b)
		}(),
		"huge parameters":  "\x1b[99999999999;99999999999;99999999999r\x1b[99999999999;99999999999H\x1b[99999999999A",
		"wide on 1 column": "あいう日本語😀",
	}
	for name, in := range inputs {
		for _, size := range [][2]int{{1, 1}, {1, 2}, {3, 5}, {24, 80}} {
			e := New(size[0], size[1], Options{Scrollback: 5})
			if _, err := e.Write([]byte(in)); err != nil {
				t.Fatal(err)
			}
			if msg := checkInvariants(e); msg != "" {
				t.Errorf("%s at %v: %s", name, size, msg)
			}
		}
	}
}
