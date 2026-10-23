package editor

import "math"

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
}

// identityProjection shows each line as one row of the same height.
type identityProjection struct {
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
