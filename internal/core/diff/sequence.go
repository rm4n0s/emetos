package diff

import (
	"strings"
	"unicode/utf8"
)

// Ported from VS Code: src/vs/editor/common/diff/defaultLinesDiffComputer/algorithms/diffAlgorithm.ts
// and src/vs/editor/common/diff/defaultLinesDiffComputer/lineSequence.ts

// offsetRange is a range of offsets in a sequence (0-based, end exclusive).
type offsetRange struct {
	start, end int
}

func (r offsetRange) isEmpty() bool { return r.start == r.end }
func (r offsetRange) length() int   { return r.end - r.start }

func (r offsetRange) delta(d int) offsetRange { return offsetRange{r.start + d, r.end + d} }

func (r offsetRange) join(o offsetRange) offsetRange {
	return offsetRange{min(r.start, o.start), max(r.end, o.end)}
}

// sequenceDiff says that the range seq1 of the first sequence was replaced by
// the range seq2 of the second one.
type sequenceDiff struct {
	seq1, seq2 offsetRange
}

func (d sequenceDiff) swap() sequenceDiff { return sequenceDiff{d.seq2, d.seq1} }

func (d sequenceDiff) join(o sequenceDiff) sequenceDiff {
	return sequenceDiff{d.seq1.join(o.seq1), d.seq2.join(o.seq2)}
}

func (d sequenceDiff) delta(offset int) sequenceDiff {
	if offset == 0 {
		return d
	}
	return sequenceDiff{d.seq1.delta(offset), d.seq2.delta(offset)}
}

// sequence is what the diff algorithms and the heuristics work on.
type sequence interface {
	// length returns the number of elements.
	length() int
	// element returns a number for the element at the offset; equal numbers
	// mean equal elements.
	element(offset int) int
	// boundaryScore says how well the sequence can be split before the given
	// offset (0 to length): the higher, the better. It is never negative.
	boundaryScore(offset int) int
	// stronglyEqual compares the elements by more than element does. For
	// lines, element is the hash of the trimmed line and this compares the
	// lines themselves.
	stronglyEqual(offset1, offset2 int) bool
}

// trivialDiff is the one change that replaces the whole of seq1 by the whole
// of seq2.
func trivialDiff(seq1, seq2 sequence) []sequenceDiff {
	return []sequenceDiff{{offsetRange{0, seq1.length()}, offsetRange{0, seq2.length()}}}
}

// lineSequence is the sequence of the lines of a text. The elements are the
// numbers of the trimmed lines.
type lineSequence struct {
	trimmedHash []int
	lines       []string
}

func (s *lineSequence) length() int { return len(s.trimmedHash) }

func (s *lineSequence) element(offset int) int { return s.trimmedHash[offset] }

func (s *lineSequence) boundaryScore(offset int) int {
	before, after := 0, 0
	if offset > 0 {
		before = indentation(s.lines[offset-1])
	}
	if offset < len(s.lines) {
		after = indentation(s.lines[offset])
	}
	return 1000 - (before + after)
}

func (s *lineSequence) stronglyEqual(offset1, offset2 int) bool {
	return s.lines[offset1] == s.lines[offset2]
}

// indentation returns the number of spaces and tabs at the start of the line.
func indentation(line string) int {
	i := 0
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return i
}

// isJSSpace reports whether r is white space for JavaScript's String.trim and
// the \s of its regular expressions (not the same set as unicode.IsSpace).
func isJSSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\v', '\f', '\r', ' ', 0xA0, 0x1680, 0x2028, 0x2029, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// jsTrim removes the white space at both ends of s as JavaScript's trim does.
func jsTrim(s string) string { return strings.TrimFunc(s, isJSSpace) }

// utf16Len returns the number of UTF-16 code units of s, which is what
// JavaScript's string length counts.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// nonSpaceLen returns the UTF-16 length of the lines without their white
// space, and stops counting once it is above limit.
func nonSpaceLen(lines []string, limit int) int {
	n := 0
	for _, l := range lines {
		for i := 0; i < len(l); {
			r, size := utf8.DecodeRuneInString(l[i:])
			i += size
			if isJSSpace(r) {
				continue
			}
			if r >= 0x10000 {
				n += 2
			} else {
				n++
			}
		}
		if n > limit {
			return n
		}
	}
	return n
}
