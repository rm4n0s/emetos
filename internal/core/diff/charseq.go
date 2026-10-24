package diff

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// Ported from VS Code: src/vs/editor/common/diff/defaultLinesDiffComputer/linesSliceCharSequence.ts

// invalidByteBase is added to the value of a byte that is not valid UTF-8 to
// make its element: it is above every code point, so two different invalid
// bytes are never equal elements.
const invalidByteBase int32 = 0x110000

// charSequence is the sequence of the characters of a slice of lines (VS Code's
// LinesSliceCharSequence). The elements are code points (where VS Code has
// UTF-16 code units), and a line break between two lines of the slice is the
// element '\n'. Offsets of the sequence are element offsets; translateOffset
// turns them into positions in the lines, whose columns are byte columns.
type charSequence struct {
	// rng is the slice in the lines.
	rng Range
	// considerWhitespaceChanges is false when the white space at the start and
	// at the end of each line is left out of the sequence.
	considerWhitespaceChanges bool

	elements []int32
	// byteOffsets[i] is the byte offset of element i in its line after the
	// slice start and the trimming; for a line break it is the length of the
	// line's elements in bytes. It has one more entry, for the end of the
	// sequence.
	byteOffsets []int32
	// firstElementOffsetByLineIdx is the offset of the first element of each
	// line of the slice.
	firstElementOffsetByLineIdx []int
	// lineStartOffsets is the number of bytes cut off the start of each line:
	// the start column of the slice for its first line, else 0.
	lineStartOffsets []int
	// trimmedWsLengthsByLineIdx is the number of bytes of white space trimmed
	// off the start of each line.
	trimmedWsLengthsByLineIdx []int
}

// newCharSequence makes the sequence of the characters of the range of lines.
// The range must lie inside the lines.
func newCharSequence(lines []string, rng Range, considerWhitespaceChanges bool) *charSequence {
	s := &charSequence{rng: rng, considerWhitespaceChanges: considerWhitespaceChanges}
	s.firstElementOffsetByLineIdx = append(s.firstElementOffsetByLineIdx, 0)
	consumed := 0
	for lineNumber := rng.Start.Line; lineNumber <= rng.End.Line; lineNumber++ {
		line := lines[lineNumber-1]
		lineStartOffset := 0
		if lineNumber == rng.Start.Line && rng.Start.Column > 1 {
			lineStartOffset = min(rng.Start.Column-1, len(line))
			line = line[lineStartOffset:]
		}
		s.lineStartOffsets = append(s.lineStartOffsets, lineStartOffset)

		trimmedWsLength := 0
		if !considerWhitespaceChanges {
			trimmedStart := strings.TrimLeftFunc(line, isJSSpace)
			trimmedWsLength = len(line) - len(trimmedStart)
			line = strings.TrimRightFunc(trimmedStart, isJSSpace)
		}
		s.trimmedWsLengthsByLineIdx = append(s.trimmedWsLengthsByLineIdx, trimmedWsLength)

		lineLength := len(line)
		if lineNumber == rng.End.Line {
			lineLength = min(rng.End.Column-1-lineStartOffset-trimmedWsLength, len(line))
		}
		lineLength = max(lineLength, 0)

		consumed = 0
		text := line[:lineLength]
		for consumed < len(text) {
			r, size := utf8.DecodeRuneInString(text[consumed:])
			e := int32(r)
			if r == utf8.RuneError && size == 1 {
				e = invalidByteBase + int32(text[consumed])
			}
			s.elements = append(s.elements, e)
			s.byteOffsets = append(s.byteOffsets, int32(consumed))
			consumed += size
		}

		if lineNumber < rng.End.Line {
			s.elements = append(s.elements, '\n')
			s.byteOffsets = append(s.byteOffsets, int32(consumed))
			s.firstElementOffsetByLineIdx = append(s.firstElementOffsetByLineIdx, len(s.elements))
		}
	}
	s.byteOffsets = append(s.byteOffsets, int32(consumed))
	return s
}

func (s *charSequence) length() int { return len(s.elements) }

func (s *charSequence) element(offset int) int { return int(s.elements[offset]) }

func (s *charSequence) stronglyEqual(offset1, offset2 int) bool {
	return s.elements[offset1] == s.elements[offset2]
}

// text returns the characters of the range of offsets as a string (for the
// tests, and for measuring text).
func (s *charSequence) text(r offsetRange) string {
	var b strings.Builder
	for _, e := range s.elements[r.start:r.end] {
		b.WriteString(elementString(e))
	}
	return b.String()
}

// elementString returns the text of an element.
func elementString(e int32) string {
	if e >= invalidByteBase {
		return string([]byte{byte(e - invalidByteBase)})
	}
	return string(rune(e))
}

// Character categories, in the order of VS Code's CharBoundaryCategory.
const (
	catWordLower = iota
	catWordUpper
	catWordNumber
	catEnd
	catOther
	catSeparator
	catSpace
	catLineBreakCR
	catLineBreakLF
)

// categoryScore is the score of each category (VS Code's score table).
var categoryScore = [...]int{
	catWordLower:   0,
	catWordUpper:   0,
	catWordNumber:  0,
	catEnd:         10,
	catOther:       2,
	catSeparator:   30,
	catSpace:       3,
	catLineBreakCR: 10,
	catLineBreakLF: 10,
}

// category returns the category of an element; -1 is the place before the
// start and after the end of the sequence.
func category(e int32) int {
	switch {
	case e == '\n':
		return catLineBreakLF
	case e == '\r':
		return catLineBreakCR
	case e == ' ' || e == '\t':
		return catSpace
	case e >= 'a' && e <= 'z':
		return catWordLower
	case e >= 'A' && e <= 'Z':
		return catWordUpper
	case e >= '0' && e <= '9':
		return catWordNumber
	case e == -1:
		return catEnd
	case e == ',' || e == ';':
		return catSeparator
	default:
		return catOther
	}
}

func isWordChar(e int32) bool {
	return e >= 'a' && e <= 'z' || e >= 'A' && e <= 'Z' || e >= '0' && e <= '9'
}

func isUpperCase(e int32) bool { return e >= 'A' && e <= 'Z' }

// boundaryScore says how well the sequence can be split before the element at
// the offset: the best places are line breaks, then separators, then places
// where the kind of character changes.
func (s *charSequence) boundaryScore(offset int) int {
	var prev, next int32 = -1, -1
	if offset > 0 {
		prev = s.elements[offset-1]
	}
	if offset < len(s.elements) {
		next = s.elements[offset]
	}
	prevCategory, nextCategory := category(prev), category(next)

	if prevCategory == catLineBreakCR && nextCategory == catLineBreakLF {
		// Don't break between \r and \n.
		return 0
	}
	if prevCategory == catLineBreakLF {
		// Prefer the line break before the change.
		return 150
	}

	score := 0
	if prevCategory != nextCategory {
		score += 10
		if prevCategory == catWordLower && nextCategory == catWordUpper {
			score++
		}
	}
	return score + categoryScore[prevCategory] + categoryScore[nextCategory]
}

// translateOffset returns the position of an offset of the sequence in the
// lines. At the start of a line the preference tells whether the position is
// before ('left') or after ('right') the white space trimmed off the line.
func (s *charSequence) translateOffset(offset int, left bool) Position {
	// The last line whose first element is at or before the offset.
	i := sort.Search(len(s.firstElementOffsetByLineIdx), func(k int) bool {
		return s.firstElementOffsetByLineIdx[k] > offset
	}) - 1
	lineOffset := offset - s.firstElementOffsetByLineIdx[i]
	trimmed := s.trimmedWsLengthsByLineIdx[i]
	if lineOffset == 0 && left {
		trimmed = 0
	}
	return Position{
		Line:   s.rng.Start.Line + i,
		Column: 1 + s.lineStartOffsets[i] + int(s.byteOffsets[offset]) + trimmed,
	}
}

// translateRange returns the range of text of a range of offsets. The start
// is placed after the white space trimmed off the start of its line, the end
// before it; a range that would be reversed by this is empty.
func (s *charSequence) translateRange(r offsetRange) Range {
	pos1 := s.translateOffset(r.start, false)
	pos2 := s.translateOffset(r.end, true)
	if pos2.isBefore(pos1) {
		return Range{pos2, pos2}
	}
	return Range{pos1, pos2}
}

// findWordContaining returns the word (letters and digits) that contains the
// element at the offset.
func (s *charSequence) findWordContaining(offset int) (offsetRange, bool) {
	if offset < 0 || offset >= len(s.elements) || !isWordChar(s.elements[offset]) {
		return offsetRange{}, false
	}
	start := offset
	for start > 0 && isWordChar(s.elements[start-1]) {
		start--
	}
	end := offset
	for end < len(s.elements) && isWordChar(s.elements[end]) {
		end++
	}
	return offsetRange{start, end}, true
}

// findSubWordContaining returns the sub-word that contains the element at the
// offset: fooBar has the sub-words foo and bar.
func (s *charSequence) findSubWordContaining(offset int) (offsetRange, bool) {
	if offset < 0 || offset >= len(s.elements) || !isWordChar(s.elements[offset]) {
		return offsetRange{}, false
	}
	start := offset
	for start > 0 && isWordChar(s.elements[start-1]) && !isUpperCase(s.elements[start]) {
		start--
	}
	end := offset
	for end < len(s.elements) && isWordChar(s.elements[end]) && !isUpperCase(s.elements[end]) {
		end++
	}
	return offsetRange{start, end}, true
}

// countLinesIn returns the number of line breaks in the range of offsets.
func (s *charSequence) countLinesIn(r offsetRange) int {
	return s.translateOffset(r.end, false).Line - s.translateOffset(r.start, false).Line
}

// extendToFullLines returns the range of the lines that the range touches:
// from the start of the line of its start to the start of the line after its
// end (or the end of the sequence).
func (s *charSequence) extendToFullLines(r offsetRange) offsetRange {
	first := s.firstElementOffsetByLineIdx
	// The last line start at or before the start of the range.
	i := sort.Search(len(first), func(k int) bool { return first[k] > r.start }) - 1
	start := 0
	if i >= 0 {
		start = first[i]
	}
	// The first line start at or after the end of the range.
	j := sort.Search(len(first), func(k int) bool { return r.end <= first[k] })
	end := len(s.elements)
	if j < len(first) {
		end = first[j]
	}
	return offsetRange{start, end}
}
