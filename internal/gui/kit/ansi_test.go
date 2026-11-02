package kit

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
)

func ansiTheme(i int) AnsiColor       { return AnsiColor{Kind: AnsiColorTheme, Index: i} }
func ansiRGB(r, g, b uint8) AnsiColor { return AnsiColor{Kind: AnsiColorRGB, R: r, G: g, B: b} }

// styleOf is the style of the text after the sequence.
func styleOf(t *testing.T, sequence string) AnsiStyle {
	t.Helper()
	spans := ParseANSI(sequence + "content")
	if len(spans) != 1 || spans[0].Text != "content" {
		t.Fatalf("%q: spans = %+v, want one span with the text", sequence, spans)
	}
	return spans[0].Style
}

// Ported from VS Code: src/vs/workbench/contrib/debug/test/browser/debugANSIHandling.test.ts ('Expected single sequence operation', 'Expected
// single 8-bit color sequence operation', 'Expected single 24-bit color sequence operation', 'Empty sequence output'): the classes of the
// DOM become the members of the style.
func TestParseANSISingleSequences(t *testing.T) {
	cases := []struct {
		name, seq string
		want      AnsiStyle
	}{
		{"nothing", "", AnsiStyle{}},
		{"bold", "\x1b[1m", AnsiStyle{Bold: true}},
		{"dim", "\x1b[2m", AnsiStyle{Dim: true}},
		{"italic", "\x1b[3m", AnsiStyle{Italic: true}},
		{"underline", "\x1b[4m", AnsiStyle{Underline: true}},
		{"blink", "\x1b[5m", AnsiStyle{Blink: true}},
		{"rapid blink", "\x1b[6m", AnsiStyle{RapidBlink: true}},
		{"hidden", "\x1b[8m", AnsiStyle{Hidden: true}},
		{"strike through", "\x1b[9m", AnsiStyle{StrikeThrough: true}},
		{"double underline", "\x1b[21m", AnsiStyle{DoubleUnderline: true}},
		{"overline", "\x1b[53m", AnsiStyle{Overline: true}},
		{"superscript", "\x1b[73m", AnsiStyle{Superscript: true}},
		{"subscript", "\x1b[74m", AnsiStyle{Subscript: true}},
		{"font 1 to 10", "\x1b[11m", AnsiStyle{Font: 1}},
		{"font 10", "\x1b[20m", AnsiStyle{Font: 10}},
		{"font back to normal", "\x1b[13;10m", AnsiStyle{}},
		{"not italic ends font 10", "\x1b[3;20;23m", AnsiStyle{}},
		{"not italic keeps another font", "\x1b[3;12;23m", AnsiStyle{Font: 2}},
		{"normal intensity", "\x1b[1;2;22m", AnsiStyle{}},
		{"not underlined", "\x1b[4;24m", AnsiStyle{}},
		{"not double underlined", "\x1b[21;24m", AnsiStyle{}},
		{"not blinking", "\x1b[5;6;25m", AnsiStyle{}},
		{"not hidden", "\x1b[8;28m", AnsiStyle{}},
		{"not struck", "\x1b[9;29m", AnsiStyle{}},
		{"not overlined", "\x1b[53;55m", AnsiStyle{}},
		{"neither super nor subscript", "\x1b[73;75m", AnsiStyle{}},
		{"red", "\x1b[31m", AnsiStyle{Fg: ansiTheme(1)}},
		{"white", "\x1b[37m", AnsiStyle{Fg: ansiTheme(7)}},
		{"bright red", "\x1b[91m", AnsiStyle{Fg: ansiTheme(9)}},
		{"bright white", "\x1b[97m", AnsiStyle{Fg: ansiTheme(15)}},
		{"green behind", "\x1b[42m", AnsiStyle{Bg: ansiTheme(2)}},
		{"bright cyan behind", "\x1b[106m", AnsiStyle{Bg: ansiTheme(14)}},
		{"default color", "\x1b[33;39m", AnsiStyle{}},
		{"default background", "\x1b[43;49m", AnsiStyle{}},
		{"default underline color", "\x1b[58;5;3m\x1b[59m", AnsiStyle{}},
		{"an advanced color ignores the codes after it", "\x1b[58;5;3;59m", AnsiStyle{UnderlineColor: ansiTheme(3)}},
		{"a color replaces the one before", "\x1b[31;32m", AnsiStyle{Fg: ansiTheme(2)}},
		{"colors do not remove the other kind", "\x1b[40;31;42;33m", AnsiStyle{Fg: ansiTheme(3), Bg: ansiTheme(2)}},
		{"formats and colors together", "\x1b[1;3;4;30;41m", AnsiStyle{Bold: true, Italic: true, Underline: true, Fg: ansiTheme(0), Bg: ansiTheme(1)}},
		{"doubles do not accumulate", "\x1b[1;1;2;2;3;3;4;4;5;5;6;6;8;8;9;9;21;21;53;53;73;73;74;74m",
			AnsiStyle{Bold: true, Dim: true, Italic: true, DoubleUnderline: true, Blink: true, RapidBlink: true, Hidden: true, StrikeThrough: true, Overline: true, Subscript: true}},
		{"more formats", "\x1b[1;2;5;6;21;8;9m", AnsiStyle{Bold: true, Dim: true, Blink: true, RapidBlink: true, DoubleUnderline: true, Hidden: true, StrikeThrough: true}},
		{"duplicates", "\x1b[1;1;4;1;4;4;1;4m", AnsiStyle{Bold: true, Underline: true}},
		{"a semicolon at the end", "\x1b[1;4;m", AnsiStyle{Bold: true, Underline: true}},
		{"reset clears all", "\x1b[1;4;30;41;32;43;34;45;36;47;0m", AnsiStyle{}},
		{"reset in the middle", "\x1b[1;31;0;4m", AnsiStyle{Underline: true}},
		{"several sequences", "\x1b[1m\x1b[3m\x1b[4m\x1b[32m", AnsiStyle{Bold: true, Italic: true, Underline: true, Fg: ansiTheme(2)}},
		{"8-bit, the colors of the theme", "\x1b[38;5;1m", AnsiStyle{Fg: ansiTheme(1)}},
		{"8-bit, the last of the theme", "\x1b[38;5;15m", AnsiStyle{Fg: ansiTheme(15)}},
		{"8-bit, the first bright", "\x1b[38;5;8m", AnsiStyle{Fg: ansiTheme(8)}},
		{"8-bit behind, theme", "\x1b[48;5;12m", AnsiStyle{Bg: ansiTheme(12)}},
		{"8-bit underline, theme", "\x1b[58;5;9m", AnsiStyle{UnderlineColor: ansiTheme(9)}},
		{"8-bit, the cube", "\x1b[38;5;16m", AnsiStyle{Fg: ansiRGB(0, 0, 0)}},
		{"8-bit, the cube, a middle", "\x1b[38;5;100m", AnsiStyle{Fg: ansiRGB(102, 102, 0)}},
		{"8-bit, the end of the cube", "\x1b[48;5;231m", AnsiStyle{Bg: ansiRGB(255, 255, 255)}},
		{"8-bit, a gray", "\x1b[38;5;240m", AnsiStyle{Fg: ansiRGB(89, 89, 89)}},
		{"8-bit underline, a color", "\x1b[58;5;196m", AnsiStyle{UnderlineColor: ansiRGB(255, 0, 0)}},
		{"8-bit, out of the palette", "\x1b[48;5;300m", AnsiStyle{}},
		{"8-bit, codes after the color are ignored", "\x1b[48;5;100;42;77;99;4;24m", AnsiStyle{Bg: ansiRGB(102, 102, 0)}},
		{"8-bit, no number", "\x1b[38;5m", AnsiStyle{}},
		{"24-bit", "\x1b[38;2;10;20;30m", AnsiStyle{Fg: ansiRGB(10, 20, 30)}},
		{"24-bit behind", "\x1b[48;2;255;0;128m", AnsiStyle{Bg: ansiRGB(255, 0, 128)}},
		{"24-bit underline", "\x1b[58;2;1;2;3m", AnsiStyle{UnderlineColor: ansiRGB(1, 2, 3)}},
		{"24-bit, too short", "\x1b[38;2;4;4m", AnsiStyle{}},
		{"24-bit, a component too big", "\x1b[48;2;150;300;5m", AnsiStyle{}},
		{"24-bit, codes after the color are ignored", "\x1b[48;2;100;42;77;99;200;75m", AnsiStyle{Bg: ansiRGB(100, 42, 77)}},
		{"an unknown mode of the advanced color", "\x1b[38;3;1;2;3m", AnsiStyle{}},
		{"inversion swaps the colors", "\x1b[31;42;7m", AnsiStyle{Fg: ansiTheme(2), Bg: ansiTheme(1)}},
		{"inversion with only a foreground", "\x1b[31;7m", AnsiStyle{Bg: ansiTheme(1)}},
		{"inversion off swaps them back", "\x1b[31;42;7;27m", AnsiStyle{Fg: ansiTheme(1), Bg: ansiTheme(2)}},
		{"inversion twice is once", "\x1b[31;42;7;7m", AnsiStyle{Fg: ansiTheme(2), Bg: ansiTheme(1)}},
		{"an empty sequence", "\x1b[m", AnsiStyle{}},
		{"two semicolons", "\x1b[1;;m", AnsiStyle{}},
		{"only semicolons", "\x1b[;m", AnsiStyle{}},
		{"a number that is no format", "\x1b[99m", AnsiStyle{}},
		{"a sequence that is no graphics is hidden", "\x1b[2K", AnsiStyle{}},
		{"a cursor sequence is hidden", "\x1b[10;5H", AnsiStyle{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := styleOf(t, tc.seq); got != tc.want {
				t.Errorf("%q: style = %+v, want %+v", tc.seq, got, tc.want)
			}
		})
	}
}

func TestParseANSIEverySimpleColorAndTheCancel(t *testing.T) {
	for i := 30; i <= 37; i++ {
		if got := styleOf(t, fmt.Sprintf("\x1b[%dm", i)).Fg; got != ansiTheme(i-30) {
			t.Errorf("%d: %+v", i, got)
		}
		if got := styleOf(t, fmt.Sprintf("\x1b[%d;39m", i)).Fg; got.Kind != AnsiColorNone {
			t.Errorf("%d;39: %+v", i, got)
		}
	}
	for i := 40; i <= 47; i++ {
		if got := styleOf(t, fmt.Sprintf("\x1b[%dm", i)).Bg; got != ansiTheme(i-40) {
			t.Errorf("%d: %+v", i, got)
		}
		if got := styleOf(t, fmt.Sprintf("\x1b[%d;49m", i)).Bg; got.Kind != AnsiColorNone {
			t.Errorf("%d;49: %+v", i, got)
		}
	}
	for i := 90; i <= 97; i++ {
		if got := styleOf(t, fmt.Sprintf("\x1b[%dm", i)).Fg; got != ansiTheme(i-90+8) {
			t.Errorf("%d: %+v", i, got)
		}
	}
	for i := 100; i <= 107; i++ {
		if got := styleOf(t, fmt.Sprintf("\x1b[%dm", i)).Bg; got != ansiTheme(i-100+8) {
			t.Errorf("%d: %+v", i, got)
		}
	}
	// 8-bit colors of the theme are the same colors as the simple codes
	for i := 0; i <= 15; i++ {
		if got := styleOf(t, fmt.Sprintf("\x1b[38;5;%dm", i)).Fg; got != ansiTheme(i) {
			t.Errorf("38;5;%d: %+v", i, got)
		}
		if got := styleOf(t, fmt.Sprintf("\x1b[48;5;%dm", i)).Bg; got != ansiTheme(i) {
			t.Errorf("48;5;%d: %+v", i, got)
		}
	}
	for i := 0; i <= 255; i++ {
		if got := styleOf(t, fmt.Sprintf("\x1b[58;5;%dm\x1b[59m", i)).UnderlineColor; got.Kind != AnsiColorNone {
			t.Errorf("58;5;%d;59: %+v", i, got)
		}
		if got := styleOf(t, fmt.Sprintf("\x1b[58;5;%dm", i)).UnderlineColor; got.Kind == AnsiColorNone {
			t.Errorf("58;5;%d: no color", i)
		}
	}
	for r := 0; r <= 255; r += 64 {
		for g := 0; g <= 255; g += 64 {
			for b := 0; b <= 255; b += 64 {
				want := ansiRGB(uint8(r), uint8(g), uint8(b))
				if got := styleOf(t, fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b)).Fg; got != want {
					t.Errorf("38;2;%d;%d;%d: %+v", r, g, b, got)
				}
			}
		}
	}
}

// Ported from VS Code: debugANSIHandling.test.ts ('Expected multiple sequence operation').
func TestParseANSISpans(t *testing.T) {
	cases := []struct {
		name  string
		in    string
		texts []string
		want  []AnsiStyle
	}{
		{"consecutive codes add up", "\x1b[1mbold\x1b[32mgreen\x1b[4munderline\x1b[3mitalic\x1b[0mnothing",
			[]string{"bold", "green", "underline", "italic", "nothing"},
			[]AnsiStyle{{Bold: true}, {Bold: true, Fg: ansiTheme(2)}, {Bold: true, Fg: ansiTheme(2), Underline: true},
				{Bold: true, Fg: ansiTheme(2), Underline: true, Italic: true}, {}}},
		{"off codes", "\x1b[1mbold\x1b[22m\x1b[32mgreen\x1b[4munderline\x1b[24m\x1b[3mitalic\x1b[23mjustgreen\x1b[0mnothing",
			[]string{"bold", "green", "underline", "italic", "justgreen", "nothing"},
			[]AnsiStyle{{Bold: true}, {Fg: ansiTheme(2)}, {Fg: ansiTheme(2), Underline: true}, {Fg: ansiTheme(2), Italic: true}, {Fg: ansiTheme(2)}, {}}},
		{"text before and after", "plain \x1b[31mred\x1b[0m plain", []string{"plain ", "red", " plain"}, []AnsiStyle{{}, {Fg: ansiTheme(1)}, {}}},
		{"a hidden sequence between two texts", "ab\x1b[2Kcd", []string{"ab", "cd"}, []AnsiStyle{{}, {}}},
		{"no text, no span", "\x1b[31m\x1b[0m", nil, nil},
		{"line feeds stay", "a\n\x1b[31mb\n", []string{"a\n", "b\n"}, []AnsiStyle{{}, {Fg: ansiTheme(1)}}},
		{"non-ASCII text", "é\x1b[31mü", []string{"é", "ü"}, []AnsiStyle{{}, {Fg: ansiTheme(1)}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spans := ParseANSI(tc.in)
			var texts []string
			var styles []AnsiStyle
			for _, s := range spans {
				texts = append(texts, s.Text)
				styles = append(styles, s.Style)
			}
			if !reflect.DeepEqual(texts, tc.texts) || !reflect.DeepEqual(styles, tc.want) {
				t.Errorf("spans = %q %+v\nwant %q %+v", texts, styles, tc.texts, tc.want)
			}
		})
	}
}

func TestParseANSIStartOffsets(t *testing.T) {
	spans := ParseANSI("ab\x1b[31mcd\x1b[0mef\x1b[2Kg")
	var starts []int
	for _, s := range spans {
		starts = append(starts, s.Start)
	}
	if !reflect.DeepEqual(starts, []int{0, 2, 4, 6}) {
		t.Errorf("starts = %v, want [0 2 4 6] (the places in the text without its sequences)", starts)
	}
}

// Ported from VS Code: debugANSIHandling.test.ts ('Invalid codes treated as regular text', 'Empty sequence output').
func TestParseANSIInvalidCodesAreText(t *testing.T) {
	plain := []string{"\x1b", "[", "\x1b[", "\x1b[31", "a\x1b[1;2", "\x1b[x", "\x1b[content", "é", "", "random-9f3a"}
	for _, s := range plain {
		t.Run(s, func(t *testing.T) {
			var got strings.Builder
			for _, sp := range ParseANSI(s) {
				got.WriteString(sp.Text)
				if sp.Style != (AnsiStyle{}) {
					t.Errorf("%q: the text has a style %+v", s, sp.Style)
				}
			}
			// a sequence that never ends is kept as it is
			if got.String() != s {
				t.Errorf("%q: text = %q", s, got.String())
			}
		})
	}
	// every character that ends a sequence ends one, and the text before it is hidden with it
	for _, term := range "ABCDHIJKfhmpsu" {
		spans := ParseANSI("\x1b[content" + string(term) + "after")
		if len(spans) != 1 || spans[0].Text != "after" {
			t.Errorf("terminator %q: spans = %+v", term, spans)
		}
	}
}

func TestRemoveANSI(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"", ""},
		{"\x1b[31mred\x1b[0m and \x1b[1;4mbold\x1b[m", "red and bold"},
		{"line\x1b[2K\n", "line\n"},
		{"\x1b[31", "\x1b[31"},
	}
	for _, tc := range cases {
		if got := RemoveANSI(tc.in); got != tc.want {
			t.Errorf("RemoveANSI(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Ported from VS Code: debugANSIHandling.test.ts ('calcANSI8bitColor').
func TestAnsi8BitColor(t *testing.T) {
	for i := -10; i < 16; i++ {
		if _, ok := Ansi8BitColor(i); ok {
			t.Errorf("%d has a color", i)
		}
	}
	for i := 256; i < 300; i++ {
		if _, ok := Ansi8BitColor(i); ok {
			t.Errorf("%d has a color", i)
		}
	}
	for r := 0; r <= 5; r++ {
		for g := 0; g <= 5; g++ {
			for b := 0; b <= 5; b++ {
				c, ok := Ansi8BitColor(16 + r*36 + g*6 + b)
				want := func(v int) uint8 { return uint8(math.Round(float64(v) * 255 / 5)) }
				if !ok || c.R != want(r) || c.G != want(g) || c.B != want(b) {
					t.Errorf("cube %d %d %d = %+v", r, g, b, c)
				}
			}
		}
	}
	for i := 232; i <= 255; i++ {
		c, ok := Ansi8BitColor(i)
		level := uint8(math.Round(float64(i-232) / 23 * 255))
		if !ok || c.R != level || c.G != level || c.B != level {
			t.Errorf("gray %d = %+v", i, c)
		}
	}
}
