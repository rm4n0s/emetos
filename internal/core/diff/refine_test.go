package diff

import (
	"context"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

// chars makes the sequence of a one line text.
func chars(s string) *charSequence {
	return newCharSequence([]string{s}, Range{Position{1, 1}, Position{1, len(s) + 1}}, true)
}

func TestInvertDiffs(t *testing.T) {
	tests := []struct {
		name  string
		diffs []sequenceDiff
		n1    int
		want  []sequenceDiff
	}{
		{"no changes", nil, 5, []sequenceDiff{{offsetRange{0, 5}, offsetRange{0, 5}}}},
		{"one change", []sequenceDiff{{offsetRange{2, 3}, offsetRange{2, 4}}}, 6, []sequenceDiff{
			{offsetRange{0, 2}, offsetRange{0, 2}},
			{offsetRange{3, 6}, offsetRange{4, 7}},
		}},
		{"change at the start", []sequenceDiff{{offsetRange{0, 2}, offsetRange{0, 1}}}, 4, []sequenceDiff{
			{offsetRange{0, 0}, offsetRange{0, 0}},
			{offsetRange{2, 4}, offsetRange{1, 3}},
		}},
		{"two changes", []sequenceDiff{{offsetRange{1, 2}, offsetRange{1, 2}}, {offsetRange{4, 5}, offsetRange{4, 6}}}, 7, []sequenceDiff{
			{offsetRange{0, 1}, offsetRange{0, 1}},
			{offsetRange{2, 4}, offsetRange{2, 4}},
			{offsetRange{5, 7}, offsetRange{6, 8}},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := invertDiffs(tc.diffs, tc.n1)
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Errorf("invertDiffs = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRemoveShortMatches(t *testing.T) {
	tests := []struct {
		name  string
		diffs []sequenceDiff
		want  []sequenceDiff
	}{
		{"none", nil, nil},
		{"one", []sequenceDiff{sd(1, 2, 1, 2)}, []sequenceDiff{sd(1, 2, 1, 2)}},
		{"two apart", []sequenceDiff{sd(0, 1, 0, 1), sd(4, 5, 4, 5)}, []sequenceDiff{sd(0, 1, 0, 1), sd(4, 5, 4, 5)}},
		{"two equal elements between", []sequenceDiff{sd(0, 1, 0, 1), sd(3, 4, 3, 4)}, []sequenceDiff{sd(0, 4, 0, 4)}},
		{"short in the first sequence only", []sequenceDiff{sd(0, 1, 0, 1), sd(3, 4, 9, 10)}, []sequenceDiff{sd(0, 4, 0, 10)}},
		{"touching", []sequenceDiff{sd(0, 1, 0, 1), sd(1, 2, 1, 3)}, []sequenceDiff{sd(0, 2, 0, 3)}},
		{"chain", []sequenceDiff{sd(0, 1, 0, 1), sd(3, 4, 3, 4), sd(6, 7, 6, 7)}, []sequenceDiff{sd(0, 7, 0, 7)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := removeShortMatches(tc.diffs)
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Errorf("removeShortMatches = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMergeSequenceDiffs(t *testing.T) {
	tests := []struct {
		name string
		a, b []sequenceDiff
		want []sequenceDiff
	}{
		{"empty", nil, nil, nil},
		{"only a", []sequenceDiff{sd(1, 2, 1, 2)}, nil, []sequenceDiff{sd(1, 2, 1, 2)}},
		{"interleaved", []sequenceDiff{sd(0, 1, 0, 1), sd(6, 7, 6, 7)}, []sequenceDiff{sd(3, 4, 3, 4)}, []sequenceDiff{sd(0, 1, 0, 1), sd(3, 4, 3, 4), sd(6, 7, 6, 7)}},
		{"overlapping", []sequenceDiff{sd(2, 4, 2, 4)}, []sequenceDiff{sd(3, 6, 3, 6)}, []sequenceDiff{sd(2, 6, 2, 6)}},
		{"touching", []sequenceDiff{sd(2, 4, 2, 4)}, []sequenceDiff{sd(4, 6, 4, 6)}, []sequenceDiff{sd(2, 6, 2, 6)}},
		{"b first on a tie", []sequenceDiff{sd(2, 3, 2, 3)}, []sequenceDiff{sd(2, 5, 2, 5)}, []sequenceDiff{sd(2, 5, 2, 5)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeSequenceDiffs(tc.a, tc.b)
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Errorf("mergeSequenceDiffs = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExtendDiffsToEntireWord(t *testing.T) {
	word := func(s *charSequence, i int) (offsetRange, bool) { return s.findWordContaining(i) }
	sub := func(s *charSequence, i int) (offsetRange, bool) { return s.findSubWordContaining(i) }
	tests := []struct {
		name     string
		a, b     string
		diffs    []sequenceDiff
		findWord func(*charSequence, int) (offsetRange, bool)
		force    bool
		want     []sequenceDiff
	}{
		{"mostly equal word is kept", "abcdef", "abXdef", []sequenceDiff{sd(2, 3, 2, 3)}, word, false, []sequenceDiff{sd(2, 3, 2, 3)}},
		{"two thirds equal is kept", "abcdef", "abXYef", []sequenceDiff{sd(2, 4, 2, 4)}, word, false, []sequenceDiff{sd(2, 4, 2, 4)}},
		{"mostly changed word is extended", "abcdef", "aXYZef", []sequenceDiff{sd(1, 4, 1, 4)}, word, false, []sequenceDiff{sd(0, 6, 0, 6)}},
		{"words around punctuation", "foo.abcdef", "foo.aXYZef", []sequenceDiff{sd(5, 8, 5, 8)}, word, false, []sequenceDiff{sd(4, 10, 4, 10)}},
		{"no word at the change", "a.b", "a,b", []sequenceDiff{sd(1, 2, 1, 2)}, word, false, []sequenceDiff{sd(1, 2, 1, 2)}},
		{"subwords are forced", "fooBar", "fooBaz", []sequenceDiff{sd(5, 6, 5, 6)}, sub, true, []sequenceDiff{sd(3, 6, 3, 6)}},
		{"words are not forced", "fooBar", "fooBaz", []sequenceDiff{sd(5, 6, 5, 6)}, word, false, []sequenceDiff{sd(5, 6, 5, 6)}},
		{"no changes", "abc", "abc", nil, word, false, nil},
		{"extension joins neighbours", "abcdef ghijkl", "aXYZef gXYZkl", []sequenceDiff{sd(1, 4, 1, 4), sd(8, 11, 8, 11)}, word, false, []sequenceDiff{sd(0, 6, 0, 6), sd(7, 13, 7, 13)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := extendDiffsToEntireWordIfAppropriate(chars(tc.a), chars(tc.b), tc.diffs, tc.findWord, tc.force)
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Errorf("extendDiffsToEntireWordIfAppropriate = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRemoveVeryShortMatchingTextBetweenLongDiffs(t *testing.T) {
	a := strings.Repeat("a", 100)
	b := strings.Repeat("b", 100)
	tests := []struct {
		name  string
		s1    string
		s2    string
		diffs []sequenceDiff
		want  []sequenceDiff
	}{
		{"no changes", "abc", "abc", nil, nil},
		{"long changes with one equal character are joined", a + "x" + a, b + "x" + b,
			[]sequenceDiff{sd(0, 100, 0, 100), sd(101, 201, 101, 201)}, []sequenceDiff{sd(0, 201, 0, 201)}},
		{"small changes are not joined", "ab" + "x" + "cd", "AB" + "x" + "CD",
			[]sequenceDiff{sd(0, 2, 0, 2), sd(3, 5, 3, 5)}, []sequenceDiff{sd(0, 2, 0, 2), sd(3, 5, 3, 5)}},
		{"more than 20 equal characters are not joined", a + strings.Repeat("x", 21) + a, b + strings.Repeat("x", 21) + b,
			[]sequenceDiff{sd(0, 100, 0, 100), sd(121, 221, 121, 221)}, []sequenceDiff{sd(0, 100, 0, 100), sd(121, 221, 121, 221)}},
		{"20 equal characters are joined", a + strings.Repeat("x", 20) + a, b + strings.Repeat("x", 20) + b,
			[]sequenceDiff{sd(0, 100, 0, 100), sd(120, 220, 120, 220)}, []sequenceDiff{sd(0, 220, 0, 220)}},
		{"a short prefix is taken into a long change", "ab" + a, "ab" + b,
			[]sequenceDiff{sd(2, 102, 2, 102)}, []sequenceDiff{sd(0, 102, 0, 102)}},
		{"a short suffix is taken into a long change", a + "cd", b + "cd",
			[]sequenceDiff{sd(0, 100, 0, 100)}, []sequenceDiff{sd(0, 102, 0, 102)}},
		{"a long prefix is left", "abcd" + a, "abcd" + b,
			[]sequenceDiff{sd(4, 104, 4, 104)}, []sequenceDiff{sd(4, 104, 4, 104)}},
		{"a short prefix is left for a small change", "ab" + "c", "ab" + "d",
			[]sequenceDiff{sd(2, 3, 2, 3)}, []sequenceDiff{sd(2, 3, 2, 3)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := removeVeryShortMatchingTextBetweenLongDiffs(chars(tc.s1), chars(tc.s2), tc.diffs)
			if fmt.Sprint(got) != fmt.Sprint(tc.want) {
				t.Errorf("removeVeryShortMatchingTextBetweenLongDiffs = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestRemoveVeryShortMatchingTextAcrossLines(t *testing.T) {
	// Between two large changes, text that is only white space (any number of
	// line breaks up to 5) is not an obstacle, but text on two lines, or
	// more than 5 line breaks, keeps the changes apart.
	long := strings.Repeat("a", 100)
	for _, tc := range []struct {
		name   string
		middle []string
		want   int
	}{
		{"one line break", nil, 1},
		{"empty lines", []string{"", "", ""}, 1},
		{"one line of text", []string{"x"}, 1},
		{"two lines of text", []string{"x", "y"}, 2},
		{"text and an empty line", []string{"x", ""}, 1},
		{"more than five line breaks", []string{"", "", "", "", "", ""}, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ls := append(append([]string{long}, tc.middle...), long)
			s := newCharSequence(ls, Range{Position{1, 1}, Position{len(ls), 101}}, true)
			first := sd(0, 100, 0, 100)
			second := sd(s.length()-100, s.length(), s.length()-100, s.length())
			got := removeVeryShortMatchingTextBetweenLongDiffs(s, s, []sequenceDiff{first, second})
			if len(got) != tc.want {
				t.Errorf("%d changes %v, want %d", len(got), got, tc.want)
			}
		})
	}
}

func TestToRangeMapping2(t *testing.T) {
	tests := []struct {
		name      string
		orig, mod []string
		oRange    LineRange
		mRange    LineRange
		want      string
	}{
		{"whole lines inside the texts", fields("a b c"), fields("a x c"), LineRange{2, 3}, LineRange{2, 3}, "2:1-3:1 2:1-3:1"},
		{"block touches the end of the texts", fields("a b c"), fields("a x y"), LineRange{2, 4}, LineRange{2, 4}, "2:1-3:2 2:1-3:2"},
		{"block is the whole texts", fields("a b"), fields("xx yy z"), LineRange{1, 3}, LineRange{1, 4}, "1:1-2:2 1:1-3:2"},
		{"empty original block", fields("a b c"), fields("a x b c"), LineRange{2, 2}, LineRange{2, 3}, "2:1-2:1 2:1-3:1"},
		{"empty block at the end", fields("a b"), fields("a b x"), LineRange{3, 3}, LineRange{3, 4}, "2:2-2:2 2:2-3:2"},
		{"empty modified block at the end", fields("a b x"), fields("a b"), LineRange{3, 4}, LineRange{3, 3}, "2:2-3:2 2:2-2:2"},
		{"multibyte lines", []string{"é", "日本"}, []string{"é", "日"}, LineRange{2, 3}, LineRange{2, 3}, "2:1-2:7 2:1-2:4"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, err := toRangeMapping2(tc.orig, tc.mod, tc.oRange, tc.mRange)
			if err != nil {
				t.Fatalf("toRangeMapping2: %v", err)
			}
			r := func(r Range) string {
				return fmt.Sprintf("%d:%d-%d:%d", r.Start.Line, r.Start.Column, r.End.Line, r.End.Column)
			}
			if got := r(m.Original) + " " + r(m.Modified); got != tc.want {
				t.Errorf("toRangeMapping2 = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestToRangeMapping2Invalid(t *testing.T) {
	// An empty block at the start of a text whose partner is not a valid diff.
	_, err := toRangeMapping2([]string{"a"}, []string{"a", "b"}, LineRange{1, 1}, LineRange{1, 3})
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute("toRangeMapping2.DiffRangeInvalid") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}

	_, _, err = refineDiff(newBudget(), []string{"a"}, []string{"a", "b"}, sd(0, 0, 0, 2), true, false)
	appErr, ok = errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute("refineDiff->toRangeMapping2.DiffRangeInvalid") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

// Ported from VS Code: src/vs/editor/test/node/diffing/defaultLinesDiffComputer.test.ts (lineRangeMapping)
func TestLineRangeMappingOf(t *testing.T) {
	tests := []struct {
		name      string
		m         RangeMapping
		orig, mod []string
		want      string
	}{
		{"Simple", RangeMapping{Range{Position{2, 1}, Position{3, 1}}, Range{Position{2, 1}, Position{2, 1}}},
			[]string{`const abc = "helloworld".split("");`, "", ""}, []string{`const asciiLower = "helloworld".split("");`, ""}, "[2,3)->[2,2)"},
		{"Empty Lines", RangeMapping{Range{Position{2, 1}, Position{2, 1}}, Range{Position{2, 1}, Position{4, 1}}},
			[]string{"", ""}, []string{"", "", "", ""}, "[2,2)->[2,4)"},
		{"inside a line", RangeMapping{Range{Position{1, 2}, Position{1, 3}}, Range{Position{1, 2}, Position{1, 4}}},
			[]string{"abc"}, []string{"abcd"}, "[1,2)->[1,2)"},
		{"line break added at the end of a line", RangeMapping{Range{Position{1, 4}, Position{1, 4}}, Range{Position{1, 4}, Position{2, 1}}},
			[]string{"abc"}, []string{"abc", ""}, "[2,2)->[2,3)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := lineRangeMappingOf(tc.m, tc.orig, tc.mod)
			if f := format([]LineRangeMapping{got}); f != tc.want {
				t.Errorf("lineRangeMappingOf = %s, want %s", f, tc.want)
			}
			if len(got.Inner) != 1 || got.Inner[0] != tc.m {
				t.Errorf("Inner = %+v, want the mapping", got.Inner)
			}
		})
	}
}

func TestLineRangeMappingFromRangeMappings(t *testing.T) {
	rm := func(l1, c1, l2, c2 int) RangeMapping {
		return RangeMapping{Range{Position{l1, c1}, Position{l2, c2}}, Range{Position{l1, c1}, Position{l2, c2}}}
	}
	orig := fields("aaa bbb ccc ddd eee fff")
	tests := []struct {
		name string
		in   []RangeMapping
		want string
	}{
		{"none", nil, ""},
		{"one", []RangeMapping{rm(2, 1, 2, 3)}, "[2,3)->[2,3)"},
		{"same line", []RangeMapping{rm(2, 1, 2, 2), rm(2, 3, 2, 4)}, "[2,3)->[2,3)"},
		{"adjacent lines are joined", []RangeMapping{rm(2, 1, 2, 2), rm(3, 1, 3, 2)}, "[2,4)->[2,4)"},
		{"lines that touch are joined", []RangeMapping{rm(2, 1, 2, 2), rm(4, 1, 4, 2)}, "[2,3)->[2,3) [4,5)->[4,5)"},
		{"a line between keeps them apart", []RangeMapping{rm(1, 1, 1, 2), rm(4, 1, 4, 2)}, "[1,2)->[1,2) [4,5)->[4,5)"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := lineRangeMappingFromRangeMappings(tc.in, orig, orig)
			if f := format(got); f != tc.want {
				t.Errorf("= %s, want %s", f, tc.want)
			}
			n := 0
			for _, c := range got {
				n += len(c.Inner)
			}
			if n != len(tc.in) {
				t.Errorf("%d character changes, want %d", n, len(tc.in))
			}
		})
	}
}

func TestCompute(t *testing.T) {
	tests := []struct {
		name     string
		original []string
		modified []string
		opts     Options
		want     string
	}{
		{"both empty", nil, nil, Options{}, ""},
		{"both one empty line", fields("_"), fields("_"), Options{}, ""},
		{"identical", fields("a b c"), fields("a b c"), Options{}, ""},
		{"no lines count as one empty line", nil, fields("a b"), Options{}, "[1,2)->[1,3){1:1-1:1=>1:1-2:2}"},
		{"text to no lines", fields("a b"), nil, Options{}, "[1,3)->[1,2){1:1-2:2=>1:1-1:1}"},
		{"one letter", []string{"hello world"}, []string{"hallo welt"}, Options{}, "[1,2)->[1,2){1:2-1:3=>1:2-1:3 1:7-1:12=>1:7-1:11}"},
		{"one line of three", fields("a b c"), fields("a x c"), Options{}, "[2,3)->[2,3){2:1-2:2=>2:1-2:2}"},
		{"inserted line", fields("a b"), fields("a x b"), Options{}, "[2,2)->[2,3){2:1-2:1=>2:1-3:1}"},
		{"deleted line", fields("a x b"), fields("a b"), Options{}, "[2,3)->[2,2){2:1-3:1=>2:1-2:1}"},
		{"line added at the end", fields("a b"), fields("a b _"), Options{}, "[3,3)->[3,4){2:2-2:2=>2:2-3:1}"},
		{"extra space", []string{"a", "foo bar", "c"}, []string{"a", "foo  bar", "c"}, Options{}, "[2,3)->[2,3){2:4-2:4=>2:4-2:5}"},
		{"extra space, ignoring trim white space", []string{"a", "foo bar", "c"}, []string{"a", "foo  bar", "c"}, Options{IgnoreTrimWhitespace: true}, "[2,3)->[2,3){2:4-2:4=>2:4-2:5}"},
		{"indentation changed", []string{"a", "  foo bar", "c"}, []string{"a", "foo bar  ", "c"}, Options{}, "[2,3)->[2,3){2:1-2:3=>2:1-2:1 2:10-2:10=>2:8-2:10}"},
		{"indentation changed, ignoring trim white space", []string{"a", "  foo bar", "c"}, []string{"a", "foo bar  ", "c"}, Options{IgnoreTrimWhitespace: true}, ""},
		{"content and indentation changed, ignoring trim white space", []string{"a", "  foo bar", "c"}, []string{"a", "foo baz  ", "c"}, Options{IgnoreTrimWhitespace: true}, "[2,3)->[2,3){2:9-2:10=>2:7-2:8}"},
		{"two separate lines of white space", []string{"a", " b", "c", "d ", "e"}, []string{"a", "b", "c", "d", "e"}, Options{}, "[2,3)->[2,3){2:1-2:2=>2:1-2:1} [4,5)->[4,5){4:2-4:3=>4:2-4:2}"},
		{"extra character after 2-byte ones", []string{"héllo wörld"}, []string{"héllo wörld!"}, Options{}, "[1,2)->[1,2){1:14-1:14=>1:14-1:15}"},
		{"character inside 2-byte ones", []string{"héllo wörld"}, []string{"héllo wärld"}, Options{}, "[1,2)->[1,2){1:9-1:11=>1:9-1:11}"},
		{"emoji replaced", []string{"x 😀 y"}, []string{"x 😁 y"}, Options{}, "[1,2)->[1,2){1:3-1:7=>1:3-1:7}"},
		{"text after an emoji", []string{"😀 abc"}, []string{"😀 abd"}, Options{}, "[1,2)->[1,2){1:8-1:9=>1:8-1:9}"},
		{"part of a word", []string{"fooBar"}, []string{"fooBaz"}, Options{}, "[1,2)->[1,2){1:6-1:7=>1:6-1:7}"},
		{"part of a word, extended to the sub-word", []string{"fooBar"}, []string{"fooBaz"}, Options{ExtendToSubwords: true}, "[1,2)->[1,2){1:4-1:7=>1:4-1:7}"},
		{"all lines replaced", fields("a b c"), fields("x y z"), Options{}, "[1,4)->[1,4){1:1-3:2=>1:1-3:2}"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Compute(context.Background(), tc.original, tc.modified, tc.opts)
			if err != nil {
				t.Fatalf("Compute: %v", err)
			}
			if got.HitTimeout {
				t.Error("HitTimeout is set")
			}
			if f := formatDetailed(got.Changes); f != tc.want {
				t.Errorf("changes = %s\nwant      %s", f, tc.want)
			}
			original, modified := tc.original, tc.modified
			if len(original) == 0 {
				original = []string{""}
			}
			if len(modified) == 0 {
				modified = []string{""}
			}
			if err := Validate(got.Changes, len(original), len(modified)); err != nil {
				t.Errorf("Validate: %v", err)
			}
			checkInner(t, got.Changes, original, modified)
		})
	}
}

// TestComputeSingleLineBlocks checks that Compute covers the same lines as
// ComputeLines on texts where every block is a single changed line.
func TestComputeSingleLineBlocks(t *testing.T) {
	original := fields("a b c d e f g")
	modified := fields("a x c d y f g")
	lines, err := ComputeLines(context.Background(), original, modified, Options{})
	if err != nil {
		t.Fatal(err)
	}
	full, err := Compute(context.Background(), original, modified, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if format(lines.Changes) != format(full.Changes) {
		t.Errorf("Compute %s, ComputeLines %s", format(full.Changes), format(lines.Changes))
	}
	for _, c := range lines.Changes {
		if c.Inner != nil {
			t.Errorf("ComputeLines gave character changes: %+v", c.Inner)
		}
	}
}

func TestComputeSubwordsOffByDefault(t *testing.T) {
	// VS Code's diff editor does not extend to sub-words (the fixture "subword"
	// is computed without it).
	got, err := Compute(context.Background(), []string{"fooBar"}, []string{"fooBaz"}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if f := formatDetailed(got.Changes); f != "[1,2)->[1,2){1:6-1:7=>1:6-1:7}" {
		t.Errorf("changes = %s", f)
	}
}

func TestComputeTimeLimit(t *testing.T) {
	// Two changed lines far apart: the first is a small block, the second a
	// block of 33 characters on each side, which takes 1089 steps of the
	// character stage. The clock moves a millisecond at every look.
	long := strings.Repeat("abcdefghij ", 3)
	reversedLong := strings.Repeat("jihgfedcba ", 3)
	original := []string{"a", "q", "same1", "same2", "same3", "same4", long, "z"}
	modified := []string{"a", "r", "same1", "same2", "same3", "same4", reversedLong, "z"}
	whole := func(lines []string) string {
		return fmt.Sprintf("1:1-%d:%d", len(lines), len(lines[len(lines)-1])+1)
	}

	tests := []struct {
		name     string
		limit    time.Duration
		step     time.Duration
		wantHit  bool
		contains []string // parts the result must have
		excludes []string
	}{
		{"no limit", 0, time.Hour, false, []string{"[2,3)->[2,3){2:1-2:2=>2:1-2:2}"}, []string{"7:1-8:1=>7:1-8:1"}},
		{"a limit nobody reaches", time.Hour, time.Millisecond, false, []string{"[2,3)->[2,3){2:1-2:2=>2:1-2:2}"}, []string{"7:1-8:1=>7:1-8:1"}},
		{"over in the line stage", time.Millisecond, time.Second, true, []string{"[1,9)->[1,9){" + whole(original) + "=>" + whole(modified) + "}"}, nil},
		// 64 steps of the line stage, then the small block, then the long one.
		{"over in the character stage", 200 * time.Millisecond, time.Millisecond, true,
			[]string{"[2,3)->[2,3){2:1-2:2=>2:1-2:2} [7,8)->[7,8){7:1-8:1=>7:1-8:1}"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Compute(context.Background(), original, modified,
				Options{MaxComputationTime: tc.limit, Now: steppingClock(tc.step)})
			if err != nil {
				t.Fatalf("Compute: %v", err)
			}
			if got.HitTimeout != tc.wantHit {
				t.Errorf("HitTimeout = %v, want %v", got.HitTimeout, tc.wantHit)
			}
			f := formatDetailed(got.Changes)
			for _, s := range tc.contains {
				if !strings.Contains(f, s) {
					t.Errorf("changes = %s\nwant it to contain %s", f, s)
				}
			}
			for _, s := range tc.excludes {
				if strings.Contains(f, s) {
					t.Errorf("changes = %s\nwant it not to contain %s", f, s)
				}
			}
			if err := Validate(got.Changes, len(original), len(modified)); err != nil {
				t.Errorf("Validate: %v", err)
			}
			checkInner(t, got.Changes, original, modified)
			if applied := applyInner(original, modified, got.Changes); applied != strings.Join(modified, "\n") {
				t.Errorf("the character changes do not rebuild the modified text:\n%s", applied)
			}
		})
	}
}

func TestComputeTimeLimitMyers(t *testing.T) {
	// Texts that Myers' algorithm aligns, over in the line stage: the one
	// change covers both texts, with the one character change over both.
	mk := func(n int, with string) []string {
		out := make([]string, n)
		for i := range out {
			out[i] = with + string(rune('a'+i%7)) + strings.Repeat("x", i%5)
		}
		return out
	}
	original, modified := mk(1200, "p"), mk(1000, "q")
	got, err := Compute(context.Background(), original, modified, Options{MaxComputationTime: time.Millisecond, Now: steppingClock(time.Second)})
	if err != nil {
		t.Fatal(err)
	}
	if !got.HitTimeout || len(got.Changes) != 1 || len(got.Changes[0].Inner) != 1 {
		t.Fatalf("got %d changes, hit %v", len(got.Changes), got.HitTimeout)
	}
	in := got.Changes[0].Inner[0]
	if in.Original.Start != (Position{1, 1}) || in.Original.End.Line != 1200 || in.Modified.End.Line != 1000 {
		t.Errorf("inner = %+v, want one change over both texts", in)
	}
	if err := Validate(got.Changes, len(original), len(modified)); err != nil {
		t.Errorf("Validate: %v", err)
	}
}

func TestComputeOptionsInvalid(t *testing.T) {
	_, err := Compute(context.Background(), fields("a"), fields("b"), Options{MaxComputationTime: -time.Second})
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("Compute->computeDiff.DiffTimeoutInvalid") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestComputeCancelled(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	// Two single lines of 700 different characters each: the character stage
	// takes more than 1024 steps of Myers' algorithm.
	rnd := rand.New(rand.NewPCG(3, 3))
	letters := func(n int) string {
		b := make([]byte, n)
		for i := range b {
			b[i] = byte('a' + rnd.IntN(26))
		}
		return string(b)
	}
	longA, longB := []string{letters(700)}, []string{strings.ToUpper(letters(700))}

	tests := []struct {
		name     string
		ctx      context.Context
		original []string
		modified []string
		routes   []string
	}{
		{"before the start", cancelled, fields("a b"), fields("a c"), []string{"Compute->computeDiff", "DiffCancelled"}},
		// The context is looked at once at the start and once at the start of
		// every refined block.
		{"at the start of the character stage", &cancelAfter{Context: context.Background(), n: 1}, fields("a b c"), fields("a x c"),
			[]string{"computeDiff->buildRefinedChanges", "refineDiff", "DiffCancelled"}},
		{"in the second block", &cancelAfter{Context: context.Background(), n: 2}, fields("a b c d e"), fields("a x c y e"),
			[]string{"buildRefinedChanges", "refineDiff", "DiffCancelled"}},
		{"in the white space scan", &cancelAfter{Context: context.Background(), n: 1}, []string{"a", " b", "c"}, []string{"a", "b", "d"},
			[]string{"buildRefinedChanges", "refineDiff", "DiffCancelled"}},
		{"in Myers' algorithm on characters", &cancelAfter{Context: context.Background(), n: 2}, longA, longB,
			[]string{"refineDiff->myers", "DiffCancelled"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Compute(tc.ctx, tc.original, tc.modified, Options{})
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			for _, r := range tc.routes {
				if !appErr.HasRoute(r) {
					t.Errorf("route %q is missing: %s", r, appErr.Route())
				}
			}
			if !errors.Is(err, context.Canceled) {
				t.Errorf("the error does not wrap context.Canceled: %v", err)
			}
		})
	}
}

func TestBuildRefinedChangesAlignmentBroken(t *testing.T) {
	b := newBudget()
	_, _, err := buildRefinedChanges(b, fields("a b c"), fields("a b c"), []sequenceDiff{sd(2, 3, 1, 2)}, Options{})
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	if !appErr.HasRoute("buildRefinedChanges.DiffAlignmentBroken") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
}

func TestRefineDiffUsesBothAlgorithms(t *testing.T) {
	// Slices of 249 + 250 characters are aligned by dynamic programming, of
	// 250 + 250 by Myers' algorithm; both rebuild the text.
	for _, sizes := range [][2]int{{249, 250}, {250, 250}, {400, 300}} {
		a := strings.Repeat("abc def ", sizes[0]/8+1)[:sizes[0]]
		b := strings.Repeat("abd dxf ", sizes[1]/8+1)[:sizes[1]]
		got, err := Compute(context.Background(), []string{a}, []string{b}, Options{})
		if err != nil {
			t.Fatalf("sizes %v: %v", sizes, err)
		}
		if err := Validate(got.Changes, 1, 1); err != nil {
			t.Errorf("sizes %v: Validate: %v", sizes, err)
		}
		checkInner(t, got.Changes, []string{a}, []string{b})
		if applied := applyInner([]string{a}, []string{b}, got.Changes); applied != b {
			t.Errorf("sizes %v: the character changes do not rebuild the text", sizes)
		}
	}
}

// randomText makes lines of tokens: words, camel case words, numbers,
// punctuation and (when multibyte is set) non-ASCII words, separated by
// spaces or tabs, some indented.
func randomText(r *rand.Rand, lines int, multibyte bool) []string {
	tokens := []string{"foo", "bar", "Baz", "fooBar", "x1", "42", "(", ")", ",", ";", ".", "{", "}", "=", "if", "return"}
	if multibyte {
		tokens = append(tokens, "é", "日本語", "😀", "naïve", "€")
	}
	out := make([]string, lines)
	for i := range out {
		var b strings.Builder
		b.WriteString(strings.Repeat(" ", r.IntN(3)*2))
		for j, n := 0, r.IntN(9); j < n; j++ {
			if j > 0 && r.IntN(4) > 0 {
				b.WriteString([]string{" ", " ", "  ", "\t"}[r.IntN(4)])
			}
			b.WriteString(tokens[r.IntN(len(tokens))])
		}
		if r.IntN(6) == 0 {
			b.WriteString(" ")
		}
		out[i] = b.String()
	}
	return out
}

// editText changes a few tokens, lines or white space of the lines.
func editText(r *rand.Rand, lines []string, multibyte bool) []string {
	out := append([]string(nil), lines...)
	fresh := randomText(r, 4, multibyte)
	for range 1 + r.IntN(6) {
		if len(out) == 0 {
			out = append(out, fresh[0])
			continue
		}
		i := r.IntN(len(out))
		switch r.IntN(6) {
		case 0:
			out = append(out[:i], out[i+1:]...)
		case 1:
			out = append(out[:i], append([]string{fresh[r.IntN(len(fresh))]}, out[i:]...)...)
		case 2:
			out[i] = fresh[r.IntN(len(fresh))]
		case 3:
			out[i] = " " + out[i]
		case 4:
			out[i] = strings.TrimLeft(out[i], " \t") + " "
		default:
			if f := strings.Fields(out[i]); len(f) > 1 {
				f[r.IntN(len(f))] = "changed"
				out[i] = strings.Join(f, " ")
			}
		}
	}
	return out
}

func TestComputeRandom(t *testing.T) {
	r := rand.New(rand.NewPCG(11, 7))
	for i := range 700 {
		multibyte := i%3 == 0
		original := randomText(r, r.IntN(41), multibyte)
		var modified []string
		if i%4 == 0 {
			modified = randomText(r, r.IntN(41), multibyte)
		} else {
			modified = editText(r, original, multibyte)
		}
		for _, ignore := range []bool{false, true} {
			got, err := Compute(context.Background(), original, modified, Options{IgnoreTrimWhitespace: ignore, ExtendToSubwords: i%5 == 0})
			if err != nil {
				t.Fatalf("case %d: Compute: %v\noriginal %q\nmodified %q", i, err, original, modified)
			}
			o, m := original, modified
			if len(o) == 0 {
				o = []string{""}
			}
			if len(m) == 0 {
				m = []string{""}
			}
			checkRefined(t, i, o, m, got, ignore)
		}
	}
}

// checkRefined checks a diff with character changes: it is valid, its
// character changes are inside the texts and the blocks, and applying them
// turns the original into the modified text (up to white space when that is
// ignored).
func checkRefined(t *testing.T, id int, original, modified []string, got LinesDiff, ignoreWS bool) {
	t.Helper()
	fail := func(format string, args ...any) {
		t.Helper()
		t.Fatalf("case %d (ignoreWS %v): %s\noriginal %q\nmodified %q\nchanges %s", id, ignoreWS, fmt.Sprintf(format, args...), original, modified, formatDetailed(got.Changes))
	}
	if got.HitTimeout {
		fail("HitTimeout is set")
	}
	if err := Validate(got.Changes, len(original), len(modified)); err != nil {
		fail("Validate: %v", err)
	}
	for i, c := range got.Changes {
		if len(c.Inner) == 0 {
			fail("change %d has no character changes", i)
		}
	}
	checkInner(t, got.Changes, original, modified)
	if t.Failed() {
		fail("character changes are not valid")
	}
	if ignoreWS {
		// The white space at the start and end of the lines of a block is
		// not part of its character changes, so only the rest is rebuilt.
		strip := func(s string) string {
			return strings.Map(func(r rune) rune {
				if isJSSpace(r) {
					return -1
				}
				return r
			}, s)
		}
		if a, m := strip(applyInner(original, modified, got.Changes)), strip(strings.Join(modified, "\n")); a != m {
			fail("the character changes give %q, want %q (ignoring white space)", a, m)
		}
		return
	}
	rebuilt := strings.Split(applyInner(original, modified, got.Changes), "\n")
	if len(rebuilt) != len(modified) {
		fail("the character changes give %d lines, want %d", len(rebuilt), len(modified))
	}
	for i := range rebuilt {
		if rebuilt[i] != modified[i] {
			fail("line %d is %q, want %q", i+1, rebuilt[i], modified[i])
		}
	}
	if (len(got.Changes) == 0) != equalLines(original, modified) {
		fail("changes exist: %v, texts equal: %v", len(got.Changes) > 0, equalLines(original, modified))
	}
}

func TestComputeBigFileWithScatteredEdits(t *testing.T) {
	r := rand.New(rand.NewPCG(21, 4))
	original := make([]string, 3000)
	for i := range original {
		original[i] = fmt.Sprintf("\tfunc%d(value%d int, name string) string { return name + fmt.Sprint(value%d, %d) }", i, i%17, i%5, i*7)
	}
	modified := append([]string(nil), original...)
	for range 60 {
		i := r.IntN(len(modified))
		switch r.IntN(3) {
		case 0:
			modified[i] = strings.Replace(modified[i], "name", "label", 1)
		case 1:
			modified[i] += " // changed"
		default:
			modified[i] = strings.Replace(modified[i], "int", "int64", 1)
		}
	}
	start := time.Now()
	got, err := Compute(context.Background(), original, modified, Options{MaxComputationTime: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("took %v", took)
	}
	if len(got.Changes) < 30 || len(got.Changes) > 60 {
		t.Errorf("%d changes for 60 edits", len(got.Changes))
	}
	checkRefined(t, 0, original, modified, got, false)
	for _, c := range got.Changes {
		if c.Original.Len() > 3 || c.Modified.Len() > 3 {
			t.Errorf("block %+v is larger than an edit", c)
		}
	}
}

func TestComputeRealTimeLimitInTheCharacterStage(t *testing.T) {
	// Every line differs: one block of 600 lines, about 12000 characters on each
	// side, which Myers' algorithm takes far longer than the limit to align. The
	// real clock stops it, and the block is one character change.
	original := make([]string, 600)
	modified := make([]string, 600)
	for i := range original {
		original[i] = fmt.Sprintf("alpha %d beta", i)
		modified[i] = fmt.Sprintf("gamma %d delta", i*3)
	}
	start := time.Now()
	got, err := Compute(context.Background(), original, modified, Options{MaxComputationTime: 100 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("took %v with a limit of 100 ms", took)
	}
	if !got.HitTimeout {
		t.Fatal("HitTimeout is not set")
	}
	got.HitTimeout = false // checkRefined expects a complete result
	checkRefined(t, 0, original, modified, got, false)
	if len(got.Changes) != 1 || len(got.Changes[0].Inner) != 1 {
		t.Errorf("changes = %s, want one block with one character change", formatDetailed(got.Changes))
	}
}

func TestValidateInner(t *testing.T) {
	r := func(l1, c1, l2, c2 int) Range { return Range{Position{l1, c1}, Position{l2, c2}} }
	withInner := func(o1, o2, m1, m2 int, inner ...RangeMapping) []LineRangeMapping {
		c := change(o1, o2, m1, m2)
		c.Inner = inner
		return []LineRangeMapping{c}
	}
	rm := func(o, m Range) RangeMapping { return RangeMapping{o, m} }
	tests := []struct {
		name    string
		changes []LineRangeMapping
		nOrig   int
		nMod    int
		invalid string // "" when valid
	}{
		{"inside the block", withInner(2, 3, 2, 3, rm(r(2, 1, 2, 2), r(2, 1, 2, 3))), 3, 3, ""},
		{"empty ranges", withInner(2, 3, 2, 3, rm(r(2, 2, 2, 2), r(2, 2, 2, 2))), 3, 3, ""},
		{"a line break at the start of the block", withInner(2, 2, 2, 3, rm(r(1, 4, 1, 4), r(1, 4, 2, 1))), 3, 4, ""},
		{"ends at the start of the line after the block", withInner(2, 3, 2, 3, rm(r(2, 1, 3, 1), r(2, 1, 3, 1))), 3, 3, ""},
		{"two in order", withInner(2, 4, 2, 4, rm(r(2, 1, 2, 2), r(2, 1, 2, 2)), rm(r(3, 1, 3, 2), r(3, 1, 3, 2))), 4, 4, ""},
		{"touching", withInner(2, 3, 2, 3, rm(r(2, 1, 2, 2), r(2, 1, 2, 2)), rm(r(2, 2, 2, 3), r(2, 2, 2, 3))), 3, 3, ""},
		{"no lines count as one", withInner(1, 1, 1, 2, rm(r(1, 1, 1, 1), r(1, 1, 1, 3))), 0, 1, ""},
		{"reversed in the original", withInner(2, 3, 2, 3, rm(r(2, 3, 2, 1), r(2, 1, 2, 2))), 3, 3, "outside its change or reversed"},
		{"reversed in the modified text", withInner(2, 3, 2, 3, rm(r(2, 1, 2, 2), r(3, 1, 2, 1))), 3, 3, "outside its change or reversed"},
		{"starts above the block", withInner(3, 4, 3, 4, rm(r(1, 1, 3, 2), r(3, 1, 3, 2))), 4, 4, "outside its change or reversed"},
		{"ends below the block", withInner(2, 3, 2, 3, rm(r(2, 1, 4, 1), r(2, 1, 2, 2))), 4, 4, "outside its change or reversed"},
		{"line past the text", withInner(2, 3, 2, 3, rm(r(2, 1, 2, 2), r(2, 1, 4, 1))), 3, 3, "outside its change or reversed"},
		{"column below one", withInner(2, 3, 2, 3, rm(r(2, 0, 2, 2), r(2, 1, 2, 2))), 3, 3, "outside its change or reversed"},
		{"overlap", withInner(2, 3, 2, 3, rm(r(2, 1, 2, 4), r(2, 1, 2, 2)), rm(r(2, 3, 2, 5), r(2, 3, 2, 5))), 3, 3, "overlap"},
		{"overlap in the modified text", withInner(2, 3, 2, 3, rm(r(2, 1, 2, 2), r(2, 1, 2, 4)), rm(r(2, 2, 2, 3), r(2, 3, 2, 5))), 3, 3, "overlap"},
		{"not sorted", withInner(2, 4, 2, 4, rm(r(3, 1, 3, 2), r(3, 1, 3, 2)), rm(r(2, 1, 2, 2), r(2, 1, 2, 2))), 4, 4, "overlap"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.changes, tc.nOrig, tc.nMod)
			if tc.invalid == "" {
				if err != nil {
					t.Fatalf("Validate = %v, want nil", err)
				}
				return
			}
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute("Validate->validateInner->errInvalid.DiffInvalid") {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
			if reason, _ := appErr.Metadata["reason"].(string); !strings.Contains(reason, tc.invalid) {
				t.Errorf("reason = %q, want it to mention %q", reason, tc.invalid)
			}
		})
	}
}

func TestOffsetRangeIntersect(t *testing.T) {
	tests := []struct {
		a, b       offsetRange
		want       offsetRange
		ok         bool
		intersects bool
	}{
		{offsetRange{0, 5}, offsetRange{3, 8}, offsetRange{3, 5}, true, true},
		{offsetRange{0, 3}, offsetRange{3, 8}, offsetRange{3, 3}, true, false},
		{offsetRange{0, 2}, offsetRange{3, 8}, offsetRange{}, false, false},
		{offsetRange{2, 2}, offsetRange{0, 5}, offsetRange{2, 2}, true, false},
	}
	for _, tc := range tests {
		got, ok := tc.a.intersect(tc.b)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("%v.intersect(%v) = %v, %v; want %v, %v", tc.a, tc.b, got, ok, tc.want, tc.ok)
		}
		if tc.a.intersects(tc.b) != tc.intersects {
			t.Errorf("%v.intersects(%v) = %v, want %v", tc.a, tc.b, !tc.intersects, tc.intersects)
		}
	}
}
