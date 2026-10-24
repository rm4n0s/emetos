package diff

import (
	"fmt"
	"math"
	"strings"
	"testing"
)

// Ported from VS Code: src/vs/editor/test/node/diffing/defaultLinesDiffComputer.test.ts

// sliceLines are the lines of VS Code's LinesSliceCharSequence test.
var sliceLines = []string{
	"line1: foo",
	"line2: fizzbuzz",
	"line3: barr",
	"line4: hello world",
	"line5: bazz",
}

func TestCharSequenceTranslateOffset(t *testing.T) {
	seq := newCharSequence(sliceLines, Range{Position{2, 1}, Position{5, 1}}, true)
	var want []string
	for _, line := range []struct{ n, cols int }{{2, 16}, {3, 12}, {4, 19}} {
		for c := 1; c <= line.cols; c++ {
			want = append(want, fmt.Sprintf("(%d,%d)", line.n, c))
		}
	}
	if seq.length() != len(want) {
		t.Fatalf("length = %d, want %d", seq.length(), len(want))
	}
	for offset := range seq.length() {
		p := seq.translateOffset(offset, false)
		if got := fmt.Sprintf("(%d,%d)", p.Line, p.Column); got != want[offset] {
			t.Errorf("translateOffset(%d) = %s, want %s", offset, got, want[offset])
		}
	}
}

func TestCharSequenceExtendToFullLines(t *testing.T) {
	seq := newCharSequence(sliceLines, Range{Position{2, 1}, Position{5, 1}}, true)
	tests := []struct {
		name string
		in   offsetRange
		want string
	}{
		{"inside one line", offsetRange{20, 25}, "line3: barr\n"},
		{"over two lines", offsetRange{20, 45}, "line3: barr\nline4: hello world\n"},
		{"empty range at a line start", offsetRange{16, 16}, ""},
		{"the first line", offsetRange{0, 3}, "line2: fizzbuzz\n"},
		{"the end of the sequence", offsetRange{40, 47}, "line4: hello world\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := seq.text(seq.extendToFullLines(tc.in)); got != tc.want {
				t.Errorf("extendToFullLines(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestCharSequenceText(t *testing.T) {
	tests := []struct {
		name  string
		lines []string
		rng   Range
		ws    bool
		want  string
	}{
		{"whole line", []string{"ab cd"}, Range{Position{1, 1}, Position{1, 6}}, true, "ab cd"},
		{"column range", []string{"ab cd"}, Range{Position{1, 2}, Position{1, 5}}, true, "b c"},
		{"end past the line", []string{"ab"}, Range{Position{1, 1}, Position{1, math.MaxInt}}, true, "ab"},
		{"two lines", []string{"ab", "cd"}, Range{Position{1, 1}, Position{2, 3}}, true, "ab\ncd"},
		{"end at the start of a line", []string{"ab", "cd"}, Range{Position{1, 1}, Position{2, 1}}, true, "ab\n"},
		{"start in the middle", []string{"abc", "def"}, Range{Position{1, 3}, Position{2, 2}}, true, "c\nd"},
		{"white space kept", []string{"  ab  "}, Range{Position{1, 1}, Position{1, 7}}, true, "  ab  "},
		{"white space trimmed", []string{"  ab  ", "\tcd "}, Range{Position{1, 1}, Position{2, 5}}, false, "ab\ncd"},
		{"end inside the trimmed line", []string{"  abcd"}, Range{Position{1, 1}, Position{1, 5}}, false, "ab"},
		{"only white space", []string{"   "}, Range{Position{1, 1}, Position{1, 4}}, false, ""},
		{"multibyte", []string{"é€😀x"}, Range{Position{1, 1}, Position{1, 11}}, true, "é€😀x"},
		{"empty slice", []string{"abc"}, Range{Position{1, 2}, Position{1, 2}}, true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			seq := newCharSequence(tc.lines, tc.rng, tc.ws)
			if got := seq.text(offsetRange{0, seq.length()}); got != tc.want {
				t.Errorf("text = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCharSequenceInvalidBytesAreDistinct(t *testing.T) {
	a := newCharSequence([]string{"\xff"}, Range{Position{1, 1}, Position{1, 2}}, true)
	b := newCharSequence([]string{"\xfe"}, Range{Position{1, 1}, Position{1, 2}}, true)
	c := newCharSequence([]string{"�"}, Range{Position{1, 1}, Position{1, 4}}, true)
	if a.element(0) == b.element(0) || a.element(0) == c.element(0) {
		t.Errorf("elements %d, %d, %d: different bytes must differ", a.element(0), b.element(0), c.element(0))
	}
	if got := a.text(offsetRange{0, 1}); got != "\xff" {
		t.Errorf("text = %q, want the invalid byte back", got)
	}
}

func TestCharSequenceTranslate(t *testing.T) {
	tests := []struct {
		name   string
		lines  []string
		rng    Range
		ws     bool
		offset int
		left   bool
		want   Position
	}{
		{"column slice start", []string{"abcdef"}, Range{Position{1, 3}, Position{1, 7}}, true, 0, false, Position{1, 3}},
		{"column slice middle", []string{"abcdef"}, Range{Position{1, 3}, Position{1, 7}}, true, 2, false, Position{1, 5}},
		{"column slice end", []string{"abcdef"}, Range{Position{1, 3}, Position{1, 7}}, true, 4, false, Position{1, 7}},
		{"trimmed start, right", []string{"  ab"}, Range{Position{1, 1}, Position{1, 5}}, false, 0, false, Position{1, 3}},
		{"trimmed start, left", []string{"  ab"}, Range{Position{1, 1}, Position{1, 5}}, false, 0, true, Position{1, 1}},
		{"trimmed start, middle", []string{"  ab"}, Range{Position{1, 1}, Position{1, 5}}, false, 1, true, Position{1, 4}},
		{"second line start, left", []string{"x", "  ab"}, Range{Position{1, 1}, Position{2, 5}}, false, 2, true, Position{2, 1}},
		{"second line start, right", []string{"x", "  ab"}, Range{Position{1, 1}, Position{2, 5}}, false, 2, false, Position{2, 3}},
		{"end of the trimmed line", []string{"ab  ", "x"}, Range{Position{1, 1}, Position{2, 2}}, false, 2, false, Position{1, 3}},
		{"bytes of multibyte characters", []string{"é€😀x"}, Range{Position{1, 1}, Position{1, 11}}, true, 3, false, Position{1, 10}},
		{"after the multibyte characters", []string{"é€😀x"}, Range{Position{1, 1}, Position{1, 11}}, true, 4, false, Position{1, 11}},
		{"after the first character", []string{"é€😀x"}, Range{Position{1, 1}, Position{1, 11}}, true, 1, false, Position{1, 3}},
		{"the end", []string{"ab", "cd"}, Range{Position{1, 1}, Position{2, 3}}, true, 5, false, Position{2, 3}},
		{"empty last line", []string{"ab", ""}, Range{Position{1, 1}, Position{2, 1}}, true, 3, false, Position{2, 1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			seq := newCharSequence(tc.lines, tc.rng, tc.ws)
			if got := seq.translateOffset(tc.offset, tc.left); got != tc.want {
				t.Errorf("translateOffset(%d, left=%v) = %+v, want %+v", tc.offset, tc.left, got, tc.want)
			}
		})
	}
}

func TestCharSequenceTranslateRange(t *testing.T) {
	// A range of the trimmed white space only is empty, not reversed.
	seq := newCharSequence([]string{"  ab", "  cd"}, Range{Position{1, 1}, Position{2, 5}}, false)
	tests := []struct {
		name string
		in   offsetRange
		want Range
	}{
		{"first line", offsetRange{0, 2}, Range{Position{1, 3}, Position{1, 5}}},
		{"over the line break", offsetRange{1, 4}, Range{Position{1, 4}, Position{2, 4}}},
		{"empty at a line start", offsetRange{3, 3}, Range{Position{2, 1}, Position{2, 1}}},
		{"ends at a line start", offsetRange{1, 3}, Range{Position{1, 4}, Position{2, 1}}},
		{"starts at a line start", offsetRange{3, 4}, Range{Position{2, 3}, Position{2, 4}}},
		{"empty before a line start", offsetRange{2, 3}, Range{Position{1, 5}, Position{2, 1}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := seq.translateRange(tc.in); got != tc.want {
				t.Errorf("translateRange(%v) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}

	// The end of a range at the start of a line is left of the trimmed white
	// space, the start right of it, so the range of nothing at that offset
	// would be reversed and is made empty.
	seq = newCharSequence([]string{"a", "  b"}, Range{Position{1, 1}, Position{2, 4}}, false)
	if got := seq.translateRange(offsetRange{2, 2}); got.Start != got.End {
		t.Errorf("empty range = %+v, want an empty range", got)
	}
}

func TestCharSequenceWords(t *testing.T) {
	seq := newCharSequence([]string{"fooBar_baz9 x", "q"}, Range{Position{1, 1}, Position{2, 2}}, true)
	tests := []struct {
		name   string
		offset int
		word   string // "" when there is none
		sub    string
	}{
		{"start of foo", 0, "fooBar", "foo"},
		{"middle of foo", 1, "fooBar", "foo"},
		// VS Code finds no characters for the sub-word at an upper case letter.
		{"the upper case letter", 3, "fooBar", ""},
		{"after the upper case letter", 4, "fooBar", "Bar"},
		{"underscore", 6, "", ""},
		{"baz9", 8, "baz9", "baz9"},
		{"digit", 10, "baz9", "baz9"},
		{"space", 11, "", ""},
		{"x", 12, "x", "x"},
		{"line break", 13, "", ""},
		{"next line", 14, "q", "q"},
		{"negative", -1, "", ""},
		{"past the end", 15, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w, ok := seq.findWordContaining(tc.offset)
			if got := ""; ok {
				got = seq.text(w)
				if got != tc.word {
					t.Errorf("findWordContaining(%d) = %q, want %q", tc.offset, got, tc.word)
				}
			} else if tc.word != "" {
				t.Errorf("findWordContaining(%d) found nothing, want %q", tc.offset, tc.word)
			}
			s, ok := seq.findSubWordContaining(tc.offset)
			if ok {
				if got := seq.text(s); got != tc.sub {
					t.Errorf("findSubWordContaining(%d) = %q, want %q", tc.offset, got, tc.sub)
				}
			} else if tc.sub != "" {
				t.Errorf("findSubWordContaining(%d) found nothing, want %q", tc.offset, tc.sub)
			}
		})
	}
}

func TestCharSequenceCountLinesIn(t *testing.T) {
	seq := newCharSequence([]string{"ab", "cd", "ef"}, Range{Position{1, 1}, Position{3, 3}}, true)
	tests := []struct {
		in   offsetRange
		want int
	}{
		{offsetRange{0, 2}, 0},
		{offsetRange{0, 3}, 1},
		{offsetRange{1, 7}, 2},
		{offsetRange{3, 3}, 0},
		{offsetRange{0, 8}, 2},
	}
	for _, tc := range tests {
		if got := seq.countLinesIn(tc.in); got != tc.want {
			t.Errorf("countLinesIn(%v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestCharSequenceStronglyEqual(t *testing.T) {
	seq := newCharSequence([]string{"abab"}, Range{Position{1, 1}, Position{1, 5}}, true)
	if !seq.stronglyEqual(0, 2) || seq.stronglyEqual(0, 1) {
		t.Error("stronglyEqual compares the characters")
	}
}

func TestCharSequenceBoundaryScore(t *testing.T) {
	// The values are VS Code's: 10 for a change of category plus the score of
	// both categories, 1 more from a lower to an upper case letter, 150 right
	// after a line break and 0 between CR and LF. The text is
	// "abC1 ,x<tab>.<LF>y<CR><LF>z".
	seq := &charSequence{elements: []int32{'a', 'b', 'C', '1', ' ', ',', 'x', '\t', '.', '\n', 'y', '\r', '\n', 'z'}}
	tests := []struct {
		name   string
		offset int
		want   int
	}{
		{"start of the text", 0, 10 + 10 + 0},
		{"between lower case letters", 1, 0},
		{"lower to upper case", 2, 10 + 1},
		{"upper case to digit", 3, 10},
		{"digit to space", 4, 10 + 0 + 3},
		{"space to comma", 5, 10 + 3 + 30},
		{"comma to letter", 6, 10 + 30 + 0},
		{"letter to tab", 7, 10 + 0 + 3},
		{"tab to dot", 8, 10 + 3 + 2},
		{"dot to line break", 9, 10 + 2 + 10},
		{"after a line break", 10, 150},
		{"letter to carriage return", 11, 10 + 0 + 10},
		{"between CR and LF", 12, 0},
		{"after the second line break", 13, 150},
		{"end of the text", 14, 10 + 0 + 10},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := seq.boundaryScore(tc.offset); got != tc.want {
				t.Errorf("boundaryScore(%d) = %d, want %d", tc.offset, got, tc.want)
			}
		})
	}
}

func TestCharCategory(t *testing.T) {
	tests := []struct {
		e    int32
		want int
	}{
		{'a', catWordLower}, {'z', catWordLower}, {'A', catWordUpper}, {'Z', catWordUpper},
		{'0', catWordNumber}, {'9', catWordNumber}, {-1, catEnd}, {'.', catOther}, {'_', catOther},
		{0xe9, catOther}, {0x1F600, catOther}, {',', catSeparator}, {';', catSeparator},
		{' ', catSpace}, {'\t', catSpace}, {'\r', catLineBreakCR}, {'\n', catLineBreakLF},
		{0xA0, catOther}, {invalidByteBase + 0xff, catOther},
	}
	for _, tc := range tests {
		if got := category(tc.e); got != tc.want {
			t.Errorf("category(%U) = %d, want %d", tc.e, got, tc.want)
		}
	}
}

func TestMyersAndDynamicProgrammingOnCharacters(t *testing.T) {
	// VS Code's myers smoke test: hello world against hallo welt.
	for _, ws := range []bool{true, false} {
		s1 := newCharSequence([]string{"hello world"}, Range{Position{1, 1}, Position{1, math.MaxInt}}, ws)
		s2 := newCharSequence([]string{"hallo welt"}, Range{Position{1, 1}, Position{1, math.MaxInt}}, ws)
		for name, run := range map[string]func(*budget) ([]sequenceDiff, bool, error){
			"myers": func(b *budget) ([]sequenceDiff, bool, error) { return myers(b, s1, s2) },
			"dp":    func(b *budget) ([]sequenceDiff, bool, error) { return dynamicProgramming(b, s1, s2, nil) },
		} {
			diffs, hit, err := run(newBudget())
			if err != nil || hit {
				t.Fatalf("%s: err %v, hit %v", name, err, hit)
			}
			// Both give a common subsequence: the unchanged parts are equal.
			var rebuilt strings.Builder
			last1 := 0
			for _, d := range diffs {
				rebuilt.WriteString(s1.text(offsetRange{last1, d.seq1.start}))
				rebuilt.WriteString(s2.text(d.seq2))
				last1 = d.seq1.end
			}
			rebuilt.WriteString(s1.text(offsetRange{last1, s1.length()}))
			if rebuilt.String() != "hallo welt" {
				t.Errorf("%s: %v rebuild %q", name, diffs, rebuilt.String())
			}
		}
	}
}
