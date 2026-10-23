package editor

import (
	"math"
	"sort"

	"github.com/rm4n0s/emetos/internal/core/wrap"
)

// wrappedProjection shows each line as the rows that word wrap splits it into (SPEC §9.1, §9.2),
// all of the same height. The lines that a fold hides have no row. A row shows the bytes of its
// line between two breaks.
type wrappedProjection struct {
	lines     int
	rowHeight float64
	// base holds the hidden lines; nil when none is hidden.
	base *foldedProjection
	// breaks are the breaks of each line; nil is a line of one row.
	breaks []*wrap.Breaks
	// rowStart[i] is the first row of line i; it has one more entry than there are lines, the
	// number of rows. A hidden line has the entry of the next line.
	rowStart []int
}

// newWrappedProjection makes the projection of lines whose breaks are known. breaks may be
// shorter than lines, the lines it does not reach are one row.
func newWrappedProjection(lines int, rowHeight float64, base *foldedProjection, breaks []*wrap.Breaks) *wrappedProjection {
	lines = max(lines, 1)
	p := &wrappedProjection{lines: lines, rowHeight: rowHeight, base: base, breaks: breaks, rowStart: make([]int, lines+1)}
	si := 0
	rows := 0
	for i := range lines {
		p.rowStart[i] = rows
		if base != nil {
			for si < len(base.spans) && base.spans[si].Last < i {
				si++
			}
			if si < len(base.spans) && base.spans[si].First <= i {
				continue
			}
		}
		rows += p.breaksOf(i).RowCount()
	}
	p.rowStart[lines] = rows
	return p
}

func (p *wrappedProjection) breaksOf(line int) *wrap.Breaks {
	if line < len(p.breaks) {
		return p.breaks[line]
	}
	return nil
}

// RowCount implements Projection.
func (p *wrappedProjection) RowCount() int { return max(p.rowStart[p.lines], 1) }

// LineRows implements Projection.
func (p *wrappedProjection) LineRows(line int) (first, count int) {
	if line < 0 || line >= p.lines {
		return 0, 0
	}
	count = p.rowStart[line+1] - p.rowStart[line]
	if count == 0 {
		return max(p.rowStart[line]-1, 0), 0 // hidden: the last row of the line that holds the fold
	}
	return p.rowStart[line], count
}

// lineOf is the line that has a row, clamped to the rows there are.
func (p *wrappedProjection) lineOf(row int) int {
	row = min(max(row, 0), p.RowCount()-1)
	return sort.Search(p.lines, func(i int) bool { return p.rowStart[i+1] > row })
}

// RowSpan implements Projection.
func (p *wrappedProjection) RowSpan(row int) (line, start, end int) {
	row = min(max(row, 0), p.RowCount()-1)
	line = min(p.lineOf(row), p.lines-1)
	b := p.breaksOf(line)
	k := row - p.rowStart[line]
	if b == nil || k >= b.RowCount()-1 {
		return line, b.RowStart(k), -1
	}
	return line, b.RowStart(k), b.RowEnd(k)
}

// RowTop implements Projection.
func (p *wrappedProjection) RowTop(row int) float64 { return float64(row) * p.rowHeight }

// RowHeight implements Projection.
func (p *wrappedProjection) RowHeight(int) float64 { return p.rowHeight }

// RowAt implements Projection.
func (p *wrappedProjection) RowAt(y float64) int {
	if p.rowHeight <= 0 || math.IsNaN(y) {
		return 0
	}
	return min(max(int(math.Floor(y/p.rowHeight)), 0), p.RowCount()-1)
}

// Height implements Projection.
func (p *wrappedProjection) Height() float64 { return float64(p.RowCount()) * p.rowHeight }

// RowIndentColumns is the number of columns a row is indented by: the rows after the first of a
// line have the indentation that wrapping gives them.
func (p *wrappedProjection) RowIndentColumns(row int) int {
	line, start, _ := p.RowSpan(row)
	if start == 0 {
		return 0
	}
	return p.breaksOf(line).IndentLength
}

// RowStarts implements edit.RowMap.
func (p *wrappedProjection) RowStarts(line int) []int {
	b := p.breaksOf(line)
	if b == nil {
		return nil
	}
	starts := make([]int, b.RowCount())
	for i := 1; i < len(starts); i++ {
		starts[i] = b.Offsets[i-1]
	}
	return starts
}

// Indent implements edit.RowMap.
func (p *wrappedProjection) Indent(line int) int {
	if b := p.breaksOf(line); b != nil {
		return b.IndentLength
	}
	return 0
}

// rowIndenter is implemented by a projection whose rows can be indented.
type rowIndenter interface{ RowIndentColumns(row int) int }
