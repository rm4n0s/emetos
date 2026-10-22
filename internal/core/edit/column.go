package edit

import "github.com/rm4n0s/emetos/internal/core/buffer"

// ColumnSelectData is where a column selection started and where it ends. Columns
// are visual columns (a tab goes to the next tab stop), so the rectangle they make
// is the one the eyes see, also over lines shorter than it.
type ColumnSelectData struct {
	// FromLine and FromVisual are where the selection started: a line (from 0) and a
	// visual column (from 0).
	FromLine, FromVisual int
	// ToLine and ToVisual are the other corner.
	ToLine, ToVisual int
}

// ColumnSelection is the cursors of a column selection, one per line that reaches
// the rectangle, and the data to go on with it.
type ColumnSelection struct {
	// Cursors have the selected part of each line, from the starting line on; the first
	// is the primary cursor.
	Cursors []Cursor
	// Data is the rectangle, to give back to extend the selection.
	Data ColumnSelectData
	// Reversed is true when the selection goes from a line up to a lower one.
	Reversed bool
}

// ColumnSelectDataOf is the rectangle of a cursor that has no column selection yet:
// from the anchor to the active end.
func (mv Mover) ColumnSelectDataOf(c Cursor) ColumnSelectData {
	tab := mv.Options.tabSize()
	anchor := mv.Model.ValidatePosition(c.Selection.Anchor)
	active := mv.Model.ValidatePosition(c.Position())
	return ColumnSelectData{
		FromLine:   anchor.Line,
		FromVisual: VisibleColumn(mv.Model.LineContent(anchor.Line), anchor.Col, tab),
		ToLine:     active.Line,
		ToVisual:   VisibleColumn(mv.Model.LineContent(active.Line), active.Col, tab),
	}
}

// ColumnSelect selects the rectangle between two corners given as a line and a
// visual column: for each line between them, the part of the line inside the
// columns. Lines that end before the rectangle starts are left out; when all of them
// do, the cursors are at the end of each line. This is what dragging the mouse with
// Shift+Alt does, with the corner under the mouse as the second one.
//
// Ported from VS Code: src/vs/editor/common/cursor/cursorColumnSelection.ts
func (mv Mover) ColumnSelect(fromLine, fromVisual, toLine, toVisual int) ColumnSelection {
	tab := mv.Options.tabSize()
	count := max(fromLine-toLine, toLine-fromLine) + 1
	reversed := fromLine > toLine
	rtl := fromVisual > toVisual
	ltr := fromVisual < toVisual
	lineAt := func(i int) int {
		if reversed {
			return fromLine - i
		}
		return fromLine + i
	}

	var cursors []Cursor
	for i := 0; i < count; i++ {
		line := lineAt(i)
		text := mv.Model.LineContent(line)
		startCol := ColumnFromVisible(text, fromVisual, tab)
		endCol := ColumnFromVisible(text, toVisual, tab)
		visibleStart := VisibleColumn(text, startCol, tab)
		visibleEnd := VisibleColumn(text, endCol, tab)
		if ltr && (visibleStart > toVisual || visibleEnd < fromVisual) {
			continue
		}
		if rtl && (visibleEnd > fromVisual || visibleStart < toVisual) {
			continue
		}
		cursors = append(cursors, Cursor{
			Selection:        buffer.Selection{Anchor: posAt(line, startCol), Active: posAt(line, endCol)},
			DesiredVisualCol: -1,
		})
	}
	if len(cursors) == 0 {
		for i := 0; i < count; i++ {
			line := lineAt(i)
			cursors = append(cursors, NewCursor(lineEnd(mv.Model, line)))
		}
	}
	return ColumnSelection{
		Cursors:  cursors,
		Data:     ColumnSelectData{FromLine: fromLine, FromVisual: fromVisual, ToLine: toLine, ToVisual: toVisual},
		Reversed: reversed,
	}
}

// ColumnSelectLeft makes the rectangle one visual column narrower on its moving side
// (or wider to the left), stopping at the first column.
func (mv Mover) ColumnSelectLeft(d ColumnSelectData) ColumnSelection {
	if d.ToVisual > 0 {
		d.ToVisual--
	}
	return mv.ColumnSelect(d.FromLine, d.FromVisual, d.ToLine, d.ToVisual)
}

// ColumnSelectRight makes the rectangle one column wider to the right, up to the end
// of the longest line it covers.
func (mv Mover) ColumnSelectRight(d ColumnSelectData) ColumnSelection {
	tab := mv.Options.tabSize()
	widest := 0
	for line := min(d.FromLine, d.ToLine); line <= max(d.FromLine, d.ToLine); line++ {
		text := mv.Model.LineContent(line)
		widest = max(widest, VisibleColumn(text, len(text), tab))
	}
	if d.ToVisual < widest {
		d.ToVisual++
	}
	return mv.ColumnSelect(d.FromLine, d.FromVisual, d.ToLine, d.ToVisual)
}

// ColumnSelectUp moves the second corner up one line, or one page.
func (mv Mover) ColumnSelectUp(d ColumnSelectData, paged bool) ColumnSelection {
	d.ToLine = max(0, d.ToLine-mv.columnRows(paged))
	return mv.ColumnSelect(d.FromLine, d.FromVisual, d.ToLine, d.ToVisual)
}

// ColumnSelectDown moves the second corner down one line, or one page.
func (mv Mover) ColumnSelectDown(d ColumnSelectData, paged bool) ColumnSelection {
	d.ToLine = min(lastLine(mv.Model), d.ToLine+mv.columnRows(paged))
	return mv.ColumnSelect(d.FromLine, d.FromVisual, d.ToLine, d.ToVisual)
}

func (mv Mover) columnRows(paged bool) int {
	if paged {
		return mv.Layout.PageRows()
	}
	return 1
}
