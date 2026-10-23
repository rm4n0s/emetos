package edit

import "github.com/rm4n0s/emetos/internal/core/buffer"

// LineLayout is what cursor movement asks of the view: how model lines are laid
// out in rows on the screen. The editor's view model implements it to account
// for word wrap and folding; ModelLineLayout is the one with a row per line.
type LineLayout interface {
	// PageRows is the number of rows a page up or page down moves.
	PageRows() int
	// MoveVertically moves a position by a number of rows, up when negative, and
	// returns the new position and the desired visual column to keep for the next
	// vertical move (-1 when there is none). desiredVisualCol is the one from the
	// previous move, or -1. Moving past the first or last row goes to the start or
	// end of it.
	MoveVertically(pos buffer.Position, rows, desiredVisualCol int) (buffer.Position, int)
	// RowStart is the position where the row that holds pos starts.
	RowStart(pos buffer.Position) buffer.Position
	// RowEnd is the position where the row that holds pos ends.
	RowEnd(pos buffer.Position) buffer.Position
}

// HiddenSpanner is implemented by a LineLayout that has lines hidden by folding: they have no
// row, and a cursor moves over them (SPEC §8.7).
type HiddenSpanner interface {
	// HiddenSpan is the span of hidden lines, first to last, that has a line; ok is false for a
	// line that is shown.
	HiddenSpan(line int) (first, last int, ok bool)
}

// hiddenSpan asks a layout for the hidden span of a line.
func hiddenSpan(l LineLayout, line int) (first, last int, ok bool) {
	if h, is := l.(HiddenSpanner); is {
		return h.HiddenSpan(line)
	}
	return 0, 0, false
}

// RowMap tells how word wrap splits the model lines into rows (SPEC §9.2). A line that is not
// split is one row.
type RowMap interface {
	// RowStarts are the byte columns where the rows of a line start: the first is 0. A line of one
	// row may give nil.
	RowStarts(line int) []int
	// Indent is the number of columns the rows after the first of a line are indented by.
	Indent(line int) int
}

// DefaultPageRows is the page size of a ModelLineLayout that has none.
const DefaultPageRows = 20

// ModelLineLayout lays out every model line as one row, or as the rows of Rows when word wrap
// splits lines. A position where a row starts is on that row; a cursor that moves by rows or goes
// to the end of a row that is not the last stops after its last visible character (before the
// white space that ends it, or before its last character when it has none), since a position
// has no side to say that it is shown at the end of the row before.
type ModelLineLayout struct {
	Model   Model
	TabSize int
	// Page is the number of rows of a page; zero means DefaultPageRows.
	Page int
	// Hidden tells the span of hidden lines that has a line (see HiddenSpanner); nil hides none.
	Hidden func(line int) (first, last int, ok bool)
	// Rows tells how the lines are split into rows; nil makes every line one row.
	Rows RowMap
}

// rowStarts are the byte columns the rows of a line start at.
func (l ModelLineLayout) rowStarts(line int) []int {
	if l.Rows == nil {
		return nil
	}
	return l.Rows.RowStarts(line)
}

// rowCount is the number of rows of a line.
func (l ModelLineLayout) rowCount(line int) int { return max(len(l.rowStarts(line)), 1) }

// rowOf is the index of the row of a line that holds a byte column.
func (l ModelLineLayout) rowOf(line, col int) int {
	row := 0
	for i, s := range l.rowStarts(line) {
		if s <= col {
			row = i
		}
	}
	return row
}

// rowBounds are the byte columns a row starts and ends at.
func (l ModelLineLayout) rowBounds(line, row int) (start, end int) {
	starts := l.rowStarts(line)
	end = len(l.Model.LineContent(line))
	if row < len(starts) {
		start = starts[row]
	}
	if row+1 < len(starts) {
		end = starts[row+1]
	}
	return start, end
}

// rowIndent is the number of columns a row is indented by.
func (l ModelLineLayout) rowIndent(line, row int) int {
	if l.Rows == nil || row == 0 {
		return 0
	}
	return l.Rows.Indent(line)
}

// rowLastColumn is the column the cursor goes to at the end of a row: the end of the line for
// the last row, otherwise the end of the row without the white space after its last word, or
// before its last character when it ends in none.
func (l ModelLineLayout) rowLastColumn(line, row int) int {
	start, end := l.rowBounds(line, row)
	if row+1 >= l.rowCount(line) {
		return end
	}
	text := l.Model.LineContent(line)
	last := end
	for last > start && (text[last-1] == ' ' || text[last-1] == '\t') {
		last--
	}
	if last == end && last > start {
		last = max(PrevGrapheme(text[:end], end), start)
	}
	return last
}

// HiddenSpan implements HiddenSpanner.
func (l ModelLineLayout) HiddenSpan(line int) (first, last int, ok bool) {
	if l.Hidden == nil {
		return 0, 0, false
	}
	return l.Hidden(line)
}

// stepRows moves a row of a line by a number of rows over the lines that are shown: it returns
// the line and the row of the target and whether the rows went past the first or the last row.
func (l ModelLineLayout) stepRows(line, row, rows int) (tl, tr int, past bool) {
	last := lastLine(l.Model)
	for ; rows > 0; rows-- {
		if row+1 < l.rowCount(line) {
			row++
			continue
		}
		next := line + 1
		if _, end, hidden := l.HiddenSpan(next); hidden {
			next = end + 1
		}
		if next > last {
			return line, row, true
		}
		line, row = next, 0
	}
	for ; rows < 0; rows++ {
		if row > 0 {
			row--
			continue
		}
		next := line - 1
		if start, _, hidden := l.HiddenSpan(next); hidden {
			next = start - 1
		}
		if next < 0 {
			return line, row, true
		}
		line = next
		row = l.rowCount(line) - 1
	}
	return line, row, false
}

// PageRows is the page size.
func (l ModelLineLayout) PageRows() int {
	if l.Page > 0 {
		return l.Page
	}
	return DefaultPageRows
}

// MoveVertically ports VS Code's MoveOperations.vertical. The desired visual
// column is the one of the position in its row, or the one kept from the last move when a
// short row took the cursor to its end.
func (l ModelLineLayout) MoveVertically(pos buffer.Position, rows, desired int) (buffer.Position, int) {
	m := l.Model
	line, col := pos.Line, pos.Col
	row := l.rowOf(line, col)
	cur := desired
	if cur < 0 {
		start, _ := l.rowBounds(line, row)
		cur = l.rowIndent(line, row) + VisibleColumn(m.LineContent(line)[start:], col-start, l.TabSize)
	}
	last := lastLine(m)
	tl, tr, past := l.stepRows(line, row, rows)
	wasOnFirst := line == 0 && col == 0
	wasOnLast := line == last && col == len(m.LineContent(line))
	wasAtEdge := wasOnLast
	if rows < 0 {
		wasAtEdge = wasOnFirst
	}
	switch {
	case past && rows < 0:
		line, col = 0, 0
	case past:
		line = last
		if first, _, hidden := l.HiddenSpan(line); hidden {
			line = first - 1 // the text ends in a fold: the last row is the line that holds it
		}
		col = len(m.LineContent(line))
	default:
		line = tl
		start, end := l.rowBounds(line, tr)
		text := m.LineContent(line)[start:end]
		col = start + ColumnFromVisible(text, max(cur-l.rowIndent(line, tr), 0), l.TabSize)
		col = min(col, l.rowLastColumn(line, tr))
	}
	if wasAtEdge {
		return buffer.Position{Line: line, Col: col}, -1
	}
	return buffer.Position{Line: line, Col: col}, cur
}

// RowStart is the start of the row that holds the position.
func (l ModelLineLayout) RowStart(pos buffer.Position) buffer.Position {
	start, _ := l.rowBounds(pos.Line, l.rowOf(pos.Line, pos.Col))
	return buffer.Position{Line: pos.Line, Col: start}
}

// RowEnd is the end of the row that holds the position: the end of the line, or where a cursor
// stops at the end of a row that is not the last (see ModelLineLayout).
func (l ModelLineLayout) RowEnd(pos buffer.Position) buffer.Position {
	return buffer.Position{Line: pos.Line, Col: l.rowLastColumn(pos.Line, l.rowOf(pos.Line, pos.Col))}
}
