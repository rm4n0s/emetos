package terminal

import (
	"strings"

	"github.com/rm4n0s/emetos/extensions/terminal/emulator"
)

// textLine is a logical line of the buffer: the text of the rows that an autowrap joined, with the cell of each character, so
// that what a search or a link detector finds in the text can be shown on the cells. The characters are runes; a cluster
// (a base character and its combining marks) is several runes in one cell, and a wide character takes two cells.
type textLine struct {
	// first is the absolute number of the first row of the line, and rows how many rows it has.
	first int64
	rows  int
	cols  int
	text  string
	// at is the cell of each character of text: row*cols + column of the cell it is in.
	at []int32
	// wide says for each character whether it takes two cells.
	wide []bool
}

// buildLine makes the logical line that starts at row y of a frame (the rows until one does not autowrap). trim removes the
// blanks at the end of the last row. A line longer than maxRunes characters is cut there.
func buildLine(f *emulator.Frame, y int, trim bool, maxRunes int) textLine {
	l := textLine{first: f.Top + int64(y), cols: f.Cols}
	var sb strings.Builder
	n := 0
	for ; y < f.Rows; y++ {
		l.rows++
		cells := f.Row(y)
		var clusters []string
		if y < len(f.Clusters) {
			clusters = f.Clusters[y]
		}
		for x := 0; x < f.Cols && n < maxRunes; x++ {
			c := cells[x]
			if c.Flags&(emulator.CellTail|emulator.CellWideFill) != 0 {
				continue
			}
			var s string
			switch {
			case c.Flags&emulator.CellCluster != 0 && int(c.Rune) < len(clusters):
				s = clusters[c.Rune]
			case c.Rune == 0:
				s = " "
			default:
				s = string(rune(c.Rune))
			}
			wide := c.Flags&emulator.CellWide != 0
			for _, r := range s {
				sb.WriteRune(r)
				l.at = append(l.at, int32((l.rows-1)*f.Cols+x))
				l.wide = append(l.wide, wide)
				n++
			}
		}
		if y >= len(f.Wrapped) || !f.Wrapped[y] || n >= maxRunes {
			break
		}
	}
	l.text = sb.String()
	if trim {
		// the blanks at the end of the last row are no text
		runes := []rune(l.text)
		k := len(runes)
		for k > 0 && runes[k-1] == ' ' {
			k--
		}
		l.text, l.at, l.wide = string(runes[:k]), l.at[:k], l.wide[:k]
	}
	return l
}

// segments are the cells that the characters [start, end) of the line cover, one segment for each row. The color is for the
// caller to fill in.
func (l textLine) segments(start, end int) []mark {
	end = min(end, len(l.at))
	if start >= end || start < 0 {
		return nil
	}
	var out []mark
	for i := start; i < end; i++ {
		row, col := int(l.at[i])/l.cols, int(l.at[i])%l.cols
		w := 1
		if l.wide[i] {
			w = 2
		}
		if k := len(out) - 1; k >= 0 && out[k].line == l.first+int64(row) && out[k].x1 >= col {
			out[k].x1 = max(out[k].x1, col+w)
			continue
		}
		out = append(out, mark{line: l.first + int64(row), x0: col, x1: col + w})
	}
	return out
}

// indexAt is the character of the line that is in the cell (row, col) of the line's rows, -1 when the cell has none (a blank
// cell past the end of the text). The right half of a wide character is the character.
func (l textLine) indexAt(row, col int) int {
	target := int32(row*l.cols + col)
	for i, c := range l.at {
		if c == target || (l.wide[i] && c+1 == target) {
			return i
		}
	}
	return -1
}

// maxLineWindow is how many rows around a row lineAround reads at a time to find where a logical line begins.
const maxLineWindow = 64

// lineAround makes the logical line that has the line with the absolute number abs: it reads the rows around it from the
// emulator (the screen and the scrollback), walks up to the row where the line begins and builds the text from there. ok is
// false when the line is no longer in the buffer. f is a frame the caller keeps for reading.
func lineAround(em *emulator.Emulator, f *emulator.Frame, abs int64, maxRunes int) (textLine, bool) {
	first := em.FirstLine()
	count := em.LineCount()
	idx := int(abs - first)
	if idx < 0 || idx >= count {
		return textLine{}, false
	}
	lo := max(idx-maxLineWindow, 0)
	for {
		em.ReadLines(lo, min(idx+maxLineWindow, count-1)-lo+1, f)
		y := idx - lo
		for y > 0 && y-1 < len(f.Wrapped) && f.Wrapped[y-1] {
			y--
		}
		// the line may begin above the rows read
		if y == 0 && lo > 0 && lo-1 >= 0 && (idx-lo) < maxLinkLineRows(f.Cols, maxRunes) {
			lo = max(lo-maxLineWindow, 0)
			continue
		}
		return buildLine(f, y, true, maxRunes), true
	}
}

// maxLinkLineRows is how many rows a line of maxRunes characters can have at most.
func maxLinkLineRows(cols, maxRunes int) int { return maxRunes/max(cols, 1) + 2 }
