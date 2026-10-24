package editor

import (
	"math"
	"sort"

	"github.com/rm4n0s/emetos/internal/core/folding"
)

// Projection maps the lines of the document to the rows of the screen (SPEC §9.1). Layout,
// scrolling, hit testing and vertical movement go through it, so that folding, word wrap and
// view zones can change what a row is without the callers noticing. Rows are numbered from
// 0 and stack from the top of the document; a row has a height of its own.
type Projection interface {
	// RowCount is the number of rows, at least 1.
	RowCount() int
	// LineRows is the first row of a model line and the number of rows the line has; a
	// folded line has none (count 0).
	LineRows(line int) (first, count int)
	// RowSpan tells what a row shows: the model line and the byte columns of the line from
	// start up to end. An end of -1 means the end of the line.
	RowSpan(row int) (line, start, end int)
	// RowTop is the y of the top of a row from the top of the document.
	RowTop(row int) float64
	// RowHeight is the height of a row.
	RowHeight(row int) float64
	// RowAt is the row at a y from the top of the document, clamped to the first and the
	// last row.
	RowAt(y float64) int
	// Height is the height of all the rows.
	Height() float64

	// RowZone tells whether a row belongs to a view zone (SPEC §9.1): the zone and the index of
	// the row inside it. A row of a zone shows no text; RowSpan gives it the line it follows and
	// an empty range.
	RowZone(row int) (id ZoneID, index int, ok bool)
	// LineZoneRows is the first row and the number of rows of the zones that follow a line, which
	// come after the rows LineRows tells; none for a hidden line.
	LineZoneRows(line int) (first, count int)
	// ZoneRows is the first row and the number of rows of a zone; a zone that is not shown has
	// none.
	ZoneRows(id ZoneID) (first, count int)
	// ZonesIn lists the zones that have a row among the rows first to last, in order.
	ZonesIn(first, last int) []ZoneID
	// ZoneRowsBefore is the number of rows of zones among the rows before a row.
	ZoneRowsBefore(row int) int
}

// identityProjection shows each line as one row of the same height.
type identityProjection struct {
	noZones
	lines     int
	rowHeight float64
}

func newIdentityProjection(lines int, rowHeight float64) *identityProjection {
	return &identityProjection{lines: max(lines, 1), rowHeight: rowHeight}
}

func (p *identityProjection) RowCount() int { return p.lines }

func (p *identityProjection) LineRows(line int) (first, count int) {
	if line < 0 || line >= p.lines {
		return 0, 0
	}
	return line, 1
}

func (p *identityProjection) RowSpan(row int) (line, start, end int) {
	return min(max(row, 0), p.lines-1), 0, -1
}

func (p *identityProjection) RowTop(row int) float64 { return float64(row) * p.rowHeight }

func (p *identityProjection) RowHeight(int) float64 { return p.rowHeight }

func (p *identityProjection) RowAt(y float64) int {
	if p.rowHeight <= 0 || math.IsNaN(y) {
		return 0
	}
	return min(max(int(math.Floor(y/p.rowHeight)), 0), p.lines-1)
}

func (p *identityProjection) Height() float64 { return float64(p.lines) * p.rowHeight }

// foldedProjection shows each line that is not hidden by a fold as one row of the same height
// (SPEC §8.7, §9.1). A hidden line has no row; the row of the line that holds the fold stands for
// it.
type foldedProjection struct {
	noZones
	lines     int
	rowHeight float64
	// spans are the hidden lines, sorted and apart, and before[i] the number of hidden lines in
	// spans[:i]; before has one more entry than spans.
	spans  []folding.LineSpan
	before []int
}

// newFoldedProjection makes the projection of a document of lines lines with some hidden. A span
// is cut to the lines the document has, and the first line is never hidden.
func newFoldedProjection(lines int, rowHeight float64, hidden []folding.LineSpan) *foldedProjection {
	lines = max(lines, 1)
	p := &foldedProjection{lines: lines, rowHeight: rowHeight, before: []int{0}}
	for _, s := range hidden {
		s.First, s.Last = max(s.First, 1), min(s.Last, lines-1)
		if s.Last < s.First {
			continue
		}
		p.spans = append(p.spans, s)
		p.before = append(p.before, p.before[len(p.before)-1]+s.Last-s.First+1)
	}
	return p
}

// spanOf is the index of the span that holds a line, or -1.
func (p *foldedProjection) spanOf(line int) int {
	i := sort.Search(len(p.spans), func(i int) bool { return p.spans[i].Last >= line })
	if i < len(p.spans) && p.spans[i].First <= line {
		return i
	}
	return -1
}

// hiddenSpan is the span of hidden lines that holds a line.
func (p *foldedProjection) hiddenSpan(line int) (first, last int, ok bool) {
	if i := p.spanOf(line); i >= 0 {
		return p.spans[i].First, p.spans[i].Last, true
	}
	return 0, 0, false
}

// hiddenBefore is the number of hidden lines before a line that is not hidden.
func (p *foldedProjection) hiddenBefore(line int) int {
	return p.before[sort.Search(len(p.spans), func(i int) bool { return p.spans[i].First > line })]
}

func (p *foldedProjection) RowCount() int { return p.lines - p.before[len(p.spans)] }

func (p *foldedProjection) LineRows(line int) (first, count int) {
	if line < 0 || line >= p.lines {
		return 0, 0
	}
	if i := p.spanOf(line); i >= 0 {
		owner := p.spans[i].First - 1
		return owner - p.before[i], 0
	}
	return line - p.hiddenBefore(line), 1
}

func (p *foldedProjection) RowSpan(row int) (line, start, end int) {
	row = min(max(row, 0), p.RowCount()-1)
	// the spans that come before the row: those whose first hidden line would be at or before it
	n := sort.Search(len(p.spans), func(i int) bool { return p.spans[i].First-p.before[i] > row })
	return row + p.before[n], 0, -1
}

func (p *foldedProjection) RowTop(row int) float64 { return float64(row) * p.rowHeight }

func (p *foldedProjection) RowHeight(int) float64 { return p.rowHeight }

func (p *foldedProjection) RowAt(y float64) int {
	if p.rowHeight <= 0 || math.IsNaN(y) {
		return 0
	}
	return min(max(int(math.Floor(y/p.rowHeight)), 0), p.RowCount()-1)
}

func (p *foldedProjection) Height() float64 { return float64(p.RowCount()) * p.rowHeight }
