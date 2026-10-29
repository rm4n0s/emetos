package resultgrid

// Cell is a place in the grid: a row as it is shown (after the sort) and a column.
type Cell struct{ Row, Col int }

// SelKind is what a Selection selects.
type SelKind int

// The kinds of selection.
const (
	// SelNone selects nothing.
	SelNone SelKind = iota
	// SelCells selects the rectangle of cells between Anchor and Focus.
	SelCells
	// SelRows selects the rows from Anchor.Row to Focus.Row, whole.
	SelRows
	// SelColumns selects the columns from Anchor.Col to Focus.Col, whole.
	SelColumns
	// SelAll selects everything.
	SelAll
)

// Selection is what the user selected: the cells between Anchor (where the selection began) and Focus (the cell the keyboard
// moves), as a rectangle, or whole rows, or whole columns, or everything.
type Selection struct {
	Kind          SelKind
	Anchor, Focus Cell
}

// Bounds are the rows and columns the selection covers, in a grid of rows by cols: first and last row, first and last column,
// all inclusive. ok is false for an empty selection or an empty grid.
func (s Selection) Bounds(rows, cols int) (r0, c0, r1, c1 int, ok bool) {
	if s.Kind == SelNone || rows <= 0 || cols <= 0 {
		return 0, 0, 0, 0, false
	}
	clamp := func(v, n int) int { return min(max(v, 0), n-1) }
	switch s.Kind {
	case SelAll:
		return 0, 0, rows - 1, cols - 1, true
	case SelRows:
		return clamp(min(s.Anchor.Row, s.Focus.Row), rows), 0, clamp(max(s.Anchor.Row, s.Focus.Row), rows), cols - 1, true
	case SelColumns:
		return 0, clamp(min(s.Anchor.Col, s.Focus.Col), cols), rows - 1, clamp(max(s.Anchor.Col, s.Focus.Col), cols), true
	}
	return clamp(min(s.Anchor.Row, s.Focus.Row), rows), clamp(min(s.Anchor.Col, s.Focus.Col), cols),
		clamp(max(s.Anchor.Row, s.Focus.Row), rows), clamp(max(s.Anchor.Col, s.Focus.Col), cols), true
}

// Mods are the keys held with a click or a key.
type Mods struct{ Shift, Ctrl bool }

// Selection returns what is selected.
func (m *Model) Selection() Selection { return m.sel }

// Focus is the cell the keyboard moves, and false when nothing is selected.
func (m *Model) Focus() (Cell, bool) { return m.sel.Focus, m.sel.Kind != SelNone }

// Press selects the cell that was clicked: only it, or, with Shift, the rectangle from where the selection began.
func (m *Model) Press(r, c int, mods Mods) {
	if !m.cellOK(r, c) {
		return
	}
	switch {
	case mods.Shift && (m.sel.Kind == SelCells || m.sel.Kind == SelRows || m.sel.Kind == SelColumns):
		anchor := m.sel.Anchor
		if m.sel.Kind != SelCells {
			anchor = m.sel.Focus
		}
		m.sel = Selection{Kind: SelCells, Anchor: anchor, Focus: Cell{r, c}}
	default:
		m.sel = Selection{Kind: SelCells, Anchor: Cell{r, c}, Focus: Cell{r, c}}
	}
	m.touch()
}

// PressRow selects a whole row (the number at its left); with Shift, the rows from where the selection began.
func (m *Model) PressRow(r int, mods Mods) {
	if r < 0 || r >= m.Len() {
		return
	}
	if mods.Shift && m.sel.Kind == SelRows {
		m.sel.Focus = Cell{Row: r, Col: m.sel.Focus.Col}
	} else {
		m.sel = Selection{Kind: SelRows, Anchor: Cell{Row: r}, Focus: Cell{Row: r}}
	}
	m.touch()
}

// PressColumn selects a whole column; with Shift, the columns from where the selection began.
func (m *Model) PressColumn(c int, mods Mods) {
	if c < 0 || c >= len(m.cols) {
		return
	}
	if mods.Shift && m.sel.Kind == SelColumns {
		m.sel.Focus = Cell{Row: m.sel.Focus.Row, Col: c}
	} else {
		m.sel = Selection{Kind: SelColumns, Anchor: Cell{Col: c}, Focus: Cell{Col: c}}
	}
	m.touch()
}

// SelectAll selects everything.
func (m *Model) SelectAll() {
	if m.Len() == 0 || len(m.cols) == 0 {
		return
	}
	m.sel = Selection{Kind: SelAll, Anchor: Cell{}, Focus: Cell{Row: m.Len() - 1, Col: len(m.cols) - 1}}
	m.touch()
}

// ClearSelection selects nothing.
func (m *Model) ClearSelection() {
	if m.sel.Kind != SelNone {
		m.sel = Selection{}
		m.touch()
	}
}

// cellOK tells whether the cell is in the grid.
func (m *Model) cellOK(r, c int) bool { return r >= 0 && r < m.Len() && c >= 0 && c < len(m.cols) }

// MoveTo moves the focus to a cell: the selection becomes that cell, or, with extend, the rectangle from where it began. The cell is
// kept inside the grid.
func (m *Model) MoveTo(r, c int, extend bool) {
	if m.Len() == 0 || len(m.cols) == 0 {
		return
	}
	cell := Cell{min(max(r, 0), m.Len()-1), min(max(c, 0), len(m.cols)-1)}
	if extend && m.sel.Kind != SelNone {
		anchor := m.sel.Anchor
		if m.sel.Kind != SelCells {
			anchor = m.sel.Focus
		}
		m.sel = Selection{Kind: SelCells, Anchor: anchor, Focus: cell}
	} else {
		m.sel = Selection{Kind: SelCells, Anchor: cell, Focus: cell}
	}
	m.touch()
}

// Move moves the focus by rows and columns; with nothing selected it starts at the first cell.
func (m *Model) Move(dr, dc int, extend bool) {
	if m.sel.Kind == SelNone {
		m.MoveTo(0, 0, false)
		return
	}
	f := m.sel.Focus
	m.MoveTo(f.Row+dr, f.Col+dc, extend)
}

// SelectedRows is how many rows the selection covers.
func (m *Model) SelectedRows() int {
	r0, _, r1, _, ok := m.sel.Bounds(m.Len(), len(m.cols))
	if !ok {
		return 0
	}
	return r1 - r0 + 1
}

// SelectedCells is how many cells the selection covers.
func (m *Model) SelectedCells() int {
	r0, c0, r1, c1, ok := m.sel.Bounds(m.Len(), len(m.cols))
	if !ok {
		return 0
	}
	return (r1 - r0 + 1) * (c1 - c0 + 1)
}
