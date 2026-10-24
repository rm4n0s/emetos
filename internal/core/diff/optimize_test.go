package diff

import "testing"

// seqOf makes a line sequence of the lines, hashing them by their trimmed text.
func seqOf(lines ...string) *lineSequence {
	hashes := map[string]int{}
	s := &lineSequence{lines: lines}
	for _, l := range lines {
		h, ok := hashes[jsTrim(l)]
		if !ok {
			h = len(hashes)
			hashes[jsTrim(l)] = h
		}
		s.trimmedHash = append(s.trimmedHash, h)
	}
	return s
}

func sd(s1, e1, s2, e2 int) sequenceDiff {
	return sequenceDiff{offsetRange{s1, e1}, offsetRange{s2, e2}}
}

func TestOptimizeSequenceDiffs(t *testing.T) {
	tests := []struct {
		name  string
		seq1  *lineSequence
		seq2  *lineSequence
		diffs []sequenceDiff
		want  string
	}{
		{
			"no diffs",
			seqOf("a"), seqOf("a"), nil, "",
		},
		{
			// orig a p b, mod a p p p b: two insertions of p around a p join
			// into one insertion of p p.
			"insertions around an equal line are joined",
			seqOf("a", "p", "b"), seqOf("a", "p", "p", "p", "b"),
			[]sequenceDiff{sd(1, 1, 1, 2), sd(2, 2, 3, 4)},
			"[1,1)->[1,3)",
		},
		{
			"insertions that cannot meet stay apart",
			seqOf("a", "p", "b"), seqOf("a", "q", "p", "r", "b"),
			[]sequenceDiff{sd(1, 1, 1, 2), sd(2, 2, 3, 4)},
			"[1,1)->[1,2) [2,2)->[3,4)",
		},
		{
			// The inserted function could start one line earlier, at the "}" of
			// the previous one; the insertion goes to the better boundary.
			"insertion is shifted to the line boundary",
			seqOf("f() {", "\ta", "}", "g() {", "\tb", "}"),
			seqOf("f() {", "\ta", "}", "h() {", "\tc", "}", "g() {", "\tb", "}"),
			[]sequenceDiff{sd(2, 2, 2, 5)},
			"[3,3)->[3,6)",
		},
		{
			"deletion is shifted to the line boundary",
			seqOf("f() {", "\ta", "}", "h() {", "\tc", "}", "g() {", "\tb", "}"),
			seqOf("f() {", "\ta", "}", "g() {", "\tb", "}"),
			[]sequenceDiff{sd(2, 5, 2, 2)},
			"[3,6)->[3,3)",
		},
		{
			"insertion that is already at the best place stays",
			seqOf("f() {", "\ta", "}", "g() {", "\tb", "}"),
			seqOf("f() {", "\ta", "}", "h() {", "\tc", "}", "g() {", "\tb", "}"),
			[]sequenceDiff{sd(3, 3, 3, 6)},
			"[3,3)->[3,6)",
		},
		{
			// The insertion slides right over the equal lines up to the next
			// change and joins it.
			"insertion slides over equal lines and joins the next change",
			seqOf("x", "a", "a", "y"), seqOf("x", "a", "a", "a", "z"),
			[]sequenceDiff{sd(1, 1, 1, 2), sd(3, 4, 4, 5)},
			"[3,4)->[3,5)",
		},
		{
			"changes that replace lines are not shifted",
			seqOf("a", "b", "b", "c"), seqOf("a", "x", "b", "c"),
			[]sequenceDiff{sd(1, 2, 1, 2)},
			"[1,2)->[1,2)",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := optimizeSequenceDiffs(tc.seq1, tc.seq2, append([]sequenceDiff(nil), tc.diffs...))
			if f := formatDiffs(got); f != tc.want {
				t.Errorf("diffs = %q, want %q", f, tc.want)
			}
		})
	}
}

func TestShiftOnlyOverStronglyEqualLines(t *testing.T) {
	// "b" and " b" have the same trimmed text but are not the same line, so the
	// insertion of the line "b" is not shifted over " b".
	seq1 := seqOf("a", " b", "c")
	seq2 := seqOf("a", " b", "b", "c")
	got := optimizeSequenceDiffs(seq1, seq2, []sequenceDiff{sd(2, 2, 2, 3)})
	if f := formatDiffs(got); f != "[2,2)->[2,3)" {
		t.Errorf("diffs = %q, want the insertion where it was", f)
	}
}

func TestRemoveVeryShortMatchingLinesBetweenDiffs(t *testing.T) {
	lines := seqOf("a", "b", "c", "}", "e", "f", "g", "h", "very long line", "j", "k", "l", "m")
	tests := []struct {
		name  string
		diffs []sequenceDiff
		want  string
	}{
		{"nothing", nil, ""},
		{"one diff", []sequenceDiff{sd(0, 3, 0, 3)}, "[0,3)->[0,3)"},
		{
			// 3 + 3 lines changed (more than 5), and one short line between.
			"short line between big changes", []sequenceDiff{sd(0, 3, 0, 3), sd(4, 5, 4, 5)},
			"[0,5)->[0,5)",
		},
		{
			"both changes small", []sequenceDiff{sd(0, 1, 0, 1), sd(2, 3, 2, 3)},
			"[0,1)->[0,1) [2,3)->[2,3)",
		},
		{
			"five characters between", []sequenceDiff{sd(0, 3, 0, 3), sd(8, 9, 8, 9)},
			"[0,3)->[0,3) [8,9)->[8,9)",
		},
		{
			"long unchanged text", []sequenceDiff{sd(0, 3, 0, 3), sd(10, 11, 10, 11)},
			"[0,3)->[0,3) [10,11)->[10,11)",
		},
		{
			"joining repeats", []sequenceDiff{sd(0, 3, 0, 3), sd(4, 5, 4, 5), sd(6, 7, 6, 7)},
			"[0,7)->[0,7)",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := removeVeryShortMatchingLinesBetweenDiffs(lines, tc.diffs)
			if f := formatDiffs(got); f != tc.want {
				t.Errorf("diffs = %q, want %q", f, tc.want)
			}
		})
	}
}

func TestRemoveVeryShortMatchingLinesWhiteSpaceBetween(t *testing.T) {
	// Blank lines and indentation between two changes are not counted.
	lines := seqOf("a", "b", "c", "  ", "}", "\t", "f", "g")
	got := removeVeryShortMatchingLinesBetweenDiffs(lines, []sequenceDiff{sd(0, 3, 0, 3), sd(6, 7, 6, 7)})
	if f := formatDiffs(got); f != "[0,7)->[0,7)" {
		t.Errorf("diffs = %q, want the changes joined", f)
	}
}

func TestLineSequenceBoundaryScore(t *testing.T) {
	s := seqOf("a", "\tb", "  c", "")
	tests := []struct {
		offset, want int
	}{
		{0, 1000}, // before "a": nothing before, no indentation
		{1, 999},  // "a" then "\tb"
		{2, 997},  // "\tb" then "  c"
		{3, 998},  // "  c" then ""
		{4, 1000}, // after the last line
	}
	for _, tc := range tests {
		if got := s.boundaryScore(tc.offset); got != tc.want {
			t.Errorf("boundaryScore(%d) = %d, want %d", tc.offset, got, tc.want)
		}
	}
}

func TestLineSequenceElements(t *testing.T) {
	s := seqOf("a", " a ", "b")
	if s.length() != 3 || s.element(0) != s.element(1) || s.element(0) == s.element(2) {
		t.Errorf("elements %v: trimmed equal lines must have the same number", s.trimmedHash)
	}
	if s.stronglyEqual(0, 1) || !s.stronglyEqual(0, 0) {
		t.Error("stronglyEqual must compare the lines themselves")
	}
}

func TestJSStrings(t *testing.T) {
	trim := []struct{ in, want string }{
		{"  a b \t", "a b"},
		{" a ", "a"},
		{"\ufeffa ", "a"},
		{" a　", "a"},
		{"\u0085a", "\u0085a"}, // U+0085 is not white space for JavaScript
		{"", ""},
		{"   ", ""},
	}
	for _, tc := range trim {
		if got := jsTrim(tc.in); got != tc.want {
			t.Errorf("jsTrim(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	lens := []struct {
		in   string
		want int
	}{{"", 0}, {"abc", 3}, {"é", 1}, {"€", 1}, {"😀", 2}, {"a😀b", 4}}
	for _, tc := range lens {
		if got := utf16Len(tc.in); got != tc.want {
			t.Errorf("utf16Len(%q) = %d, want %d", tc.in, got, tc.want)
		}
	}
	if got := nonSpaceLen([]string{" a b ", "\t", "😀 c"}, 100); got != 5 {
		t.Errorf("nonSpaceLen = %d, want 5", got)
	}
	if got := nonSpaceLen([]string{"abcdefgh", "ijkl"}, 4); got != 8 {
		t.Errorf("nonSpaceLen stops counting at the limit: %d, want 8", got)
	}
}
