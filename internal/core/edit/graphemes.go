package edit

import (
	"sync"
	"unicode/utf8"

	"github.com/go-text/typesetting/segmenter"
)

// graphemeWindow is how far, in bytes, either side of a column the grapheme
// functions look in a long line. A line up to four times it is segmented whole.
const graphemeWindow = 1 << 10

var segmenters = sync.Pool{New: func() any { return new(segmenter.Segmenter) }}

// forEachGrapheme calls f with the byte range and the first rune of each
// grapheme cluster of s, in order, until f returns false. Invalid UTF-8 bytes
// count as one U+FFFD each.
func forEachGrapheme(s string, f func(start, end int, first rune) bool) {
	seg := segmenters.Get().(*segmenter.Segmenter)
	defer segmenters.Put(seg)
	seg.InitWithString(s)
	iter := seg.GraphemeIterator()
	pos := 0
	for iter.Next() {
		g := iter.Grapheme()
		start := pos
		first, _ := utf8.DecodeRuneInString(s[pos:])
		for range g.Text {
			_, w := utf8.DecodeRuneInString(s[pos:])
			pos += w
		}
		if !f(start, pos, first) {
			return
		}
	}
}

// GraphemeBoundaries returns the byte offsets where the grapheme clusters of s
// start, then len(s). An empty string gives [0].
func GraphemeBoundaries(s string) []int {
	bounds := []int{0}
	forEachGrapheme(s, func(_, end int, _ rune) bool {
		bounds = append(bounds, end)
		return true
	})
	return bounds
}

// window returns the part of line to segment to answer about col, and where it
// starts in line.
func window(line string, col int) (string, int) {
	if len(line) <= 4*graphemeWindow {
		return line, 0
	}
	lo := max(col-graphemeWindow, 0)
	for lo > 0 && !utf8.RuneStart(line[lo]) {
		lo--
	}
	hi := min(col+graphemeWindow, len(line))
	for hi < len(line) && !utf8.RuneStart(line[hi]) {
		hi++
	}
	return line[lo:hi], lo
}

// NextGrapheme returns the byte column of the first grapheme boundary after col,
// or len(line) when there is none. A column before the start counts as the start.
// In a line longer than 4 KiB only the 1 KiB around col is looked at, so a
// cluster of more than that is cut.
func NextGrapheme(line string, col int) int {
	col = min(max(col, 0), len(line))
	text, base := window(line, col)
	next := len(line)
	forEachGrapheme(text, func(_, end int, _ rune) bool {
		if base+end > col {
			next = base + end
			return false
		}
		return true
	})
	return next
}

// PrevGrapheme returns the byte column of the last grapheme boundary before col,
// or 0 when there is none. A column after the end counts as the end. Long lines
// are treated as NextGrapheme does.
func PrevGrapheme(line string, col int) int {
	col = min(max(col, 0), len(line))
	text, base := window(line, col)
	prev := 0
	forEachGrapheme(text, func(start, _ int, _ rune) bool {
		if base+start >= col {
			return false
		}
		prev = base + start
		return true
	})
	return prev
}
