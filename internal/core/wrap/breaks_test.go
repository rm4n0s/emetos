package wrap

import (
	"strings"
	"testing"
)

// parseAnnotated removes the | that mark the starts of rows.
func parseAnnotated(annotated string) string { return strings.ReplaceAll(annotated, "|", "") }

// annotate puts a | at every row start of a line.
func annotate(text string, b *Breaks) string {
	if b == nil {
		return text
	}
	var out strings.Builder
	row := 0
	for i, r := range text {
		if b.RowOf(i) != row {
			row = b.RowOf(i)
			out.WriteByte('|')
		}
		out.WriteRune(r)
	}
	return out.String()
}

// Ported from VS Code: src/vs/editor/test/common/viewModel/monospaceLineBreaksComputer.test.ts

// The break characters of the factories of the upstream tests.
type breakChars struct{ before, after string }

var (
	brackets      = breakChars{"(", "\t)."}
	cjk           = breakChars{"(", "\t)"}
	spaceTab      = breakChars{"", "\t "}
	tabBefore     = breakChars{"\t", " "}
	vscodeDefault = breakChars{DefaultBreakBefore, DefaultBreakAfter}
)

func TestComputeBreaks(t *testing.T) {
	cases := []struct {
		name    string
		chars   breakChars
		tabSize int
		column  int
		text    string // annotated
		indent  Indent
		keepAll bool
		// wantIndent is IndentLength when it is checked (-1 otherwise).
		wantIndent int
	}{
		{"empty", brackets, 4, 5, "", IndentNone, false, -1},
		{"no wrapping if not necessary", brackets, 4, 5, "aaa", IndentNone, false, -1},
		{"fits", brackets, 4, 5, "aaaaa", IndentNone, false, -1},
		{"no column", brackets, 4, -1, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", IndentNone, false, -1},
		{"hard wrapping if no char found", brackets, 4, 5, "aaaaa|a", IndentNone, false, -1},
		{"wrapping character", brackets, 4, 5, "aaaaa|.", IndentNone, false, -1},
		{"wrapping character 2", brackets, 4, 5, "aaaaa|a.|aaa.|aa", IndentNone, false, -1},
		{"wrapping character 3", brackets, 4, 5, "aaaaa|a..|aaa.|aa", IndentNone, false, -1},
		{"wrapping character 4", brackets, 4, 5, "aaaaa|a...|aaa.|aa", IndentNone, false, -1},
		{"wrapping character 5", brackets, 4, 5, "aaaaa|a....|aaa.|aa", IndentNone, false, -1},
		{"a tab", brackets, 4, 5, "\t", IndentNone, false, -1},
		{"a tab and text", brackets, 4, 5, "\t|aaa", IndentNone, false, -1},
		{"tabs", brackets, 4, 5, "\t|a\t|aa", IndentNone, false, -1},
		{"a tab in the middle fits", brackets, 4, 5, "aa\ta", IndentNone, false, -1},
		{"a tab in the middle breaks", brackets, 4, 5, "aa\t|aa", IndentNone, false, -1},
		{"before characters", brackets, 4, 5, "aaa.|aa", IndentNone, false, -1},
		{"before characters 2", brackets, 4, 5, "aaa(.|aa", IndentNone, false, -1},
		{"after characters", brackets, 4, 5, "aaa))|).aaa", IndentNone, false, -1},
		{"after characters 2", brackets, 4, 5, "aaa))|).|aaaa", IndentNone, false, -1},
		{"after characters 3", brackets, 4, 5, "aaa)|().|aaa", IndentNone, false, -1},
		{"after characters 4", brackets, 4, 5, "aaa|(().|aaa", IndentNone, false, -1},
		{"after characters 5", brackets, 4, 5, "aa.|(().|aaa", IndentNone, false, -1},
		{"after characters 6", brackets, 4, 5, "aa.|(.).|aaa", IndentNone, false, -1},

		{"CJK and kinsoku shori 1", cjk, 4, 5, "aa 安|安", IndentNone, false, -1},
		{"CJK and kinsoku shori 2", cjk, 4, 5, "あ 安|安", IndentNone, false, -1},
		{"CJK and kinsoku shori 3", cjk, 4, 5, "ああ|安安", IndentNone, false, -1},
		{"CJK and kinsoku shori 4", cjk, 4, 5, "aa |安)安|安", IndentNone, false, -1},
		{"CJK and kinsoku shori 5", cjk, 4, 5, "aa あ|安あ)|安", IndentNone, false, -1},
		{"CJK and kinsoku shori 6", cjk, 4, 5, "aa |(安aa|安", IndentNone, false, -1},

		{"WrappingIndent.Same", spaceTab, 4, 38, " *123456789012345678901234567890123456|7890", IndentSame, false, -1},
		{"issue #16332: scroll bar overlaying on top of text", spaceTab, 4, 24, "a/ very/long/line/of/tex|t/that/expands/beyon|d/your/typical/line/|of/code/", IndentIndent, false, -1},
		{"issue #35162: wrappingIndent not consistently working", spaceTab, 4, 24, "                t h i s |i s |a l |o n |g l |i n |e", IndentIndent, false, 20},
		{"issue #75494: surrogate pairs", tabBefore, 4, 49,
			"🐇👬🌖🌞🏇🍼🐇👬🌖🌞🏇🍼🐇👬🌖🌞🏇🍼🐇👬🌖🌞🏇🍼|🐇👬🌖🌞🏇🍼🐇👬🌖🌞🏇🍼🐇👬🌖🌞🏇🍼🐇👬🌖🌞🏇🍼|🐇👬", IndentSame, false, -1},
		{"issue #75494: surrogate pairs overrun 1", vscodeDefault, 4, 4, "🐇👬|&|🌞🌖", IndentSame, false, -1},
		{"issue #75494: surrogate pairs overrun 2", vscodeDefault, 4, 17, "factory, |\"xtxtfunc|(x\"🌞🏇🍼🌞🏇🍼🐇|&👬🌖🌞👬🌖🌞🏇🍼|🐇👬x\"", IndentSame, false, -1},
		{"WrappingIndent.DeepIndent", spaceTab, 4, 26, "        W e A r e T e s t |i n g D e |e p I n d |e n t a t |i o n", IndentDeepIndent, false, 16},
		{"issue #33366: around punctuation", vscodeDefault, 4, 23, "this is a line of |text, text that sits |on a line", IndentSame, false, -1},
		{"issue #152773: a bracket followed by a comma", vscodeDefault, 4, 24, "this is a line of |(text), text that sits |on a line", IndentSame, false, -1},
		{"issue #112382: control characters", vscodeDefault, 4, 6, "\x06\x06\x06|\x06\x06\x06", IndentSame, false, -1},
		{"CJK text with word break normal", vscodeDefault, 4, 5, "你好|1111", IndentSame, false, -1},
		{"CJK text with word break keepAll", vscodeDefault, 4, 8, "你好1111", IndentSame, true, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text := parseAnnotated(tc.text)
			b := Compute(text, Options{
				TabSize: tc.tabSize, Column: tc.column, Indent: tc.indent,
				BreakBefore: tc.chars.before, BreakAfter: tc.chars.after, KeepAll: tc.keepAll,
			})
			if got := annotate(text, b); got != tc.text {
				t.Errorf("breaks\n got %q\nwant %q", got, tc.text)
			}
			if tc.wantIndent >= 0 {
				if b == nil || b.IndentLength != tc.wantIndent {
					t.Errorf("indent length %+v, want %d", b, tc.wantIndent)
				}
			}
		})
	}
}

func TestBreaksAreByteOffsetsOfCompleteCharacters(t *testing.T) {
	text := "añb日本語c🐇d" + strings.Repeat("é", 20)
	b := Compute(text, Options{TabSize: 4, Column: 6, Indent: IndentNone})
	if b == nil {
		t.Fatal("no breaks")
	}
	if b.Offsets[len(b.Offsets)-1] != len(text) {
		t.Errorf("the last offset is %d, the line has %d bytes", b.Offsets[len(b.Offsets)-1], len(text))
	}
	prev := 0
	for _, end := range b.Offsets {
		if end <= prev {
			t.Errorf("offsets do not grow: %v", b.Offsets)
		}
		if end < len(text) && !isRuneStart(text, end) {
			t.Errorf("a break at %d is in the middle of a character", end)
		}
		prev = end
	}
}

func isRuneStart(s string, i int) bool { return i >= len(s) || s[i]&0xC0 != 0x80 }

func TestARowNeverStartsWithAnExtendingCharacter(t *testing.T) {
	// "e" followed by a combining acute accent, over and over
	text := strings.Repeat("é", 12)
	b := Compute(text, Options{TabSize: 4, Column: 5})
	if b == nil {
		t.Fatal("no breaks")
	}
	for _, end := range b.Offsets[:len(b.Offsets)-1] {
		if text[end:end+2] == "́" || strings.HasPrefix(text[end:], "́") {
			t.Errorf("a row starts with the combining mark at %d: %v", end, b.Offsets)
		}
	}
	if len(b.Offsets) != 3 { // 12 columns of 5: 5, 5, 2
		t.Errorf("rows %v", b.Offsets)
	}
}

func TestRowQueries(t *testing.T) {
	b := Compute("aaaaaaaaaa", Options{TabSize: 4, Column: 4})
	if b.RowCount() != 3 {
		t.Fatalf("rows %v", b.Offsets)
	}
	cases := []struct{ offset, row int }{{0, 0}, {3, 0}, {4, 1}, {7, 1}, {8, 2}, {10, 2}}
	for _, tc := range cases {
		if got := b.RowOf(tc.offset); got != tc.row {
			t.Errorf("RowOf(%d) = %d, want %d", tc.offset, got, tc.row)
		}
	}
	if b.RowStart(0) != 0 || b.RowStart(1) != 4 || b.RowStart(2) != 8 || b.RowEnd(1) != 8 || b.RowEnd(2) != 10 {
		t.Errorf("row bounds of %v", b.Offsets)
	}
	var none *Breaks
	if none.RowCount() != 1 || none.RowStart(3) != 0 || none.RowEnd(0) != -1 || none.RowOf(5) != 0 {
		t.Error("a line without breaks is one row")
	}
}

func TestParseIndent(t *testing.T) {
	for in, want := range map[string]Indent{"none": IndentNone, "same": IndentSame, "indent": IndentIndent, "deepIndent": IndentDeepIndent, "other": IndentSame, "": IndentSame} {
		if got := ParseIndent(in); got != want {
			t.Errorf("ParseIndent(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestIndentIsDroppedWhenNothingWouldFit(t *testing.T) {
	// 8 columns of indentation in a row of 8: a continuation row would have no room
	b := Compute("        abcdefghijkl", Options{TabSize: 4, Column: 8, Indent: IndentSame})
	if b == nil || b.IndentLength != 0 {
		t.Errorf("breaks %+v: the indentation is dropped", b)
	}
}
