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

// DefaultPageRows is the page size of a ModelLineLayout that has none.
const DefaultPageRows = 20

// ModelLineLayout lays out every model line as one row.
type ModelLineLayout struct {
	Model   Model
	TabSize int
	// Page is the number of rows of a page; zero means DefaultPageRows.
	Page int
}

// PageRows is the page size.
func (l ModelLineLayout) PageRows() int {
	if l.Page > 0 {
		return l.Page
	}
	return DefaultPageRows
}

// MoveVertically ports VS Code's MoveOperations.vertical. The desired visual
// column is the one of the position, or the one kept from the last move when a
// short line took the cursor to its end.
func (l ModelLineLayout) MoveVertically(pos buffer.Position, rows, desired int) (buffer.Position, int) {
	m := l.Model
	line, col := pos.Line, pos.Col
	cur := desired
	if cur < 0 {
		cur = VisibleColumn(m.LineContent(line), col, l.TabSize)
	}
	last := lastLine(m)
	target := line + rows
	wasOnFirst := line == 0 && col == 0
	wasOnLast := line == last && col == len(m.LineContent(line))
	wasAtEdge := wasOnLast
	if target < line {
		wasAtEdge = wasOnFirst
	}
	switch {
	case target < 0:
		line, col = 0, 0
	case target > last:
		line = last
		col = len(m.LineContent(line))
	default:
		line = target
		col = ColumnFromVisible(m.LineContent(line), cur, l.TabSize)
	}
	if wasAtEdge {
		return buffer.Position{Line: line, Col: col}, -1
	}
	return buffer.Position{Line: line, Col: col}, cur
}

// RowStart is the start of the line.
func (l ModelLineLayout) RowStart(pos buffer.Position) buffer.Position {
	return buffer.Position{Line: pos.Line}
}

// RowEnd is the end of the line.
func (l ModelLineLayout) RowEnd(pos buffer.Position) buffer.Position {
	return lineEnd(l.Model, pos.Line)
}
