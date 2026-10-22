package edit

import "testing"

func TestWordClassifier(t *testing.T) {
	c := NewWordClassifier(DefaultWordSeparators + "。")
	tests := []struct {
		r    rune
		want CharClass
	}{
		{'a', ClassRegular}, {'Z', ClassRegular}, {'_', ClassRegular}, {'9', ClassRegular},
		{' ', ClassWhitespace}, {'\t', ClassWhitespace},
		{'.', ClassSeparator}, {'-', ClassSeparator}, {'(', ClassSeparator}, {'\\', ClassSeparator}, {'"', ClassSeparator},
		{'é', ClassRegular}, {'日', ClassRegular}, {'😀', ClassRegular}, {'。', ClassSeparator},
		{'\n', ClassRegular}, {0, ClassRegular},
	}
	for _, tt := range tests {
		if got := c.Class(tt.r); got != tt.want {
			t.Errorf("Class(%q) = %d, want %d", tt.r, got, tt.want)
		}
	}
	// With no separators only white space splits words.
	bare := NewWordClassifier("")
	if bare.Class('.') != ClassRegular || bare.Class(' ') != ClassWhitespace {
		t.Error("an empty separator set is not just white space")
	}
}

func TestNextAndPrevWord(t *testing.T) {
	c := NewWordClassifier(DefaultWordSeparators)
	type want struct {
		w  Word
		ok bool
	}
	none := want{}
	w := func(start, end int, kind WordKind, next CharClass) want {
		return want{Word{Start: start, End: end, Kind: kind, NextClass: next}, true}
	}
	tests := []struct {
		name string
		line string
		col  int
		next want
		prev want
	}{
		{"empty", "", 0, none, none},
		{"start of foo bar.baz", "foo bar.baz", 0, w(0, 3, RegularWord, ClassWhitespace), none},
		{"after foo", "foo bar.baz", 3, w(4, 7, RegularWord, ClassSeparator), w(0, 3, RegularWord, ClassWhitespace)},
		{"inside bar", "foo bar.baz", 5, w(4, 7, RegularWord, ClassSeparator), w(4, 7, RegularWord, ClassWhitespace)},
		{"before the dot", "foo bar.baz", 7, w(7, 8, SeparatorWord, ClassRegular), w(4, 7, RegularWord, ClassWhitespace)},
		{"after the dot", "foo bar.baz", 8, w(8, 11, RegularWord, ClassWhitespace), w(7, 8, SeparatorWord, ClassRegular)},
		{"end of line", "foo bar.baz", 11, none, w(8, 11, RegularWord, ClassSeparator)},
		{"between two spaces", "foo  bar", 4, w(5, 8, RegularWord, ClassWhitespace), w(0, 3, RegularWord, ClassWhitespace)},
		{"several separators are one word", "a...b", 1, w(1, 4, SeparatorWord, ClassRegular), w(0, 1, RegularWord, ClassWhitespace)},
		{"only white space", "   ", 1, none, none},
		{"greek", "καλημέρα κόσμε", 0, w(0, 16, RegularWord, ClassWhitespace), none},
		{"inside a cjk word", "日本語 abc", 3, w(0, 9, RegularWord, ClassWhitespace), w(0, 9, RegularWord, ClassWhitespace)},
		{"emoji are regular", "a😀 b", 0, w(0, 5, RegularWord, ClassWhitespace), none},
		{"column past the end", "ab", 99, none, w(0, 2, RegularWord, ClassWhitespace)},
		{"column before the start", "ab", -4, w(0, 2, RegularWord, ClassWhitespace), none},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := c.NextWord(tt.line, tt.col)
			if (want{got, ok}) != tt.next {
				t.Errorf("NextWord(%q, %d) = %+v, %v, want %+v, %v", tt.line, tt.col, got, ok, tt.next.w, tt.next.ok)
			}
			got, ok = c.PrevWord(tt.line, tt.col)
			if (want{got, ok}) != tt.prev {
				t.Errorf("PrevWord(%q, %d) = %+v, %v, want %+v, %v", tt.line, tt.col, got, ok, tt.prev.w, tt.prev.ok)
			}
		})
	}
}

func TestWordAt(t *testing.T) {
	c := NewWordClassifier(DefaultWordSeparators)
	tests := []struct {
		name       string
		line       string
		col        int
		start, end int
		ok         bool
	}{
		{"inside", "foo bar.baz", 5, 4, 7, true},
		{"at the start of a word", "foo bar.baz", 4, 4, 7, true},
		{"at the end of a word", "foo bar.baz", 7, 4, 7, true},
		{"between two words with a space", "foo  bar", 4, 0, 0, false},
		{"on a separator run", "a...b", 2, 0, 0, false},
		{"touching a word and a separator", "a.b", 1, 0, 1, true},
		{"empty line", "", 0, 0, 0, false},
		{"end of a cjk word", "日本語 abc", 9, 0, 9, true},
		{"start of the word after it", "日本語 abc", 10, 10, 13, true},
		{"inside a cjk word", "日本語 abc", 3, 0, 9, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := c.WordAt(tt.line, tt.col)
			if ok != tt.ok || (ok && (got.Start != tt.start || got.End != tt.end)) {
				t.Errorf("WordAt(%q, %d) = %+v, %v, want %d..%d, %v", tt.line, tt.col, got, ok, tt.start, tt.end, tt.ok)
			}
		})
	}
}

func TestCustomSeparators(t *testing.T) {
	if w, ok := NewWordClassifier("").NextWord("foo.bar baz", 0); !ok || w.End != 7 {
		t.Errorf("with no separators, 'foo.bar' is one word: %+v, %v", w, ok)
	}
	c := NewWordClassifier("。")
	if w, ok := c.NextWord("日本。語", 0); !ok || w.Start != 0 || w.End != 6 || w.NextClass != ClassSeparator {
		t.Errorf("NextWord before an ideographic full stop = %+v, %v", w, ok)
	}
	if w, ok := c.NextWord("日本。語", 6); !ok || w.Kind != SeparatorWord || w.End != 9 {
		t.Errorf("NextWord on an ideographic full stop = %+v, %v", w, ok)
	}
}

func TestWordParts(t *testing.T) {
	tests := []struct {
		name string
		line string
		// next[i] is the stop after the i-th one, starting from 0; prev goes back from the end.
		next []int
		prev []int
	}{
		{"camelCase", "camelCase", []int{5, 9}, []int{5, 0}},
		{"snake_case_name", "snake_case_name", []int{5, 10, 15}, []int{11, 6, 0}},
		{"kebab-case", "kebab-case", []int{5, 10}, []int{6, 0}},
		{"one letter words", "thisIsACamelCase", []int{4, 6, 7, 12, 16}, []int{12, 7, 6, 4, 0}},
		{"capitals then a word", "HTMLParser", []int{4, 10}, []int{4, 0}},
		{"digits then a capital", "abc123Def", []int{6, 9}, []int{6, 0}},
		{"two underscores", "a__b", []int{1, 4}, []int{3, 0}},
		{"after a multibyte letter", "é_x", []int{2, 4}, []int{3, 0}},
		{"plain word", "word", []int{4}, []int{0}},
		{"empty", "", []int{0}, []int{0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			col := 0
			for i, want := range tt.next {
				col = NextWordPart(tt.line, col)
				if col != want {
					t.Errorf("stop %d forward = %d, want %d", i, col, want)
				}
			}
			col = len(tt.line)
			for i, want := range tt.prev {
				col = PrevWordPart(tt.line, col)
				if col != want {
					t.Errorf("stop %d backward = %d, want %d", i, col, want)
				}
			}
		})
	}
}

// Whatever the text, the stops are at the start of characters and always make
// progress, so repeated movement ends.
func TestWordPartStopsAreSound(t *testing.T) {
	for _, line := range []string{"fooBar_baz-QUX9Ab", "日本語Abc_é", "😀aB😀", "a\xffB", "__--"} {
		col := 0
		for steps := 0; col < len(line); steps++ {
			next := NextWordPart(line, col)
			if next <= col || next > len(line) {
				t.Fatalf("%q: NextWordPart(%d) = %d", line, col, next)
			}
			if next < len(line) && !isRuneStart(line, next) {
				t.Fatalf("%q: NextWordPart(%d) = %d is inside a character", line, col, next)
			}
			col = next
		}
		for col = len(line); col > 0; {
			prev := PrevWordPart(line, col)
			if prev >= col || prev < 0 {
				t.Fatalf("%q: PrevWordPart(%d) = %d", line, col, prev)
			}
			if !isRuneStart(line, prev) {
				t.Fatalf("%q: PrevWordPart(%d) = %d is inside a character", line, col, prev)
			}
			col = prev
		}
	}
}

func isRuneStart(s string, i int) bool {
	return i >= len(s) || s[i]&0xC0 != 0x80
}
