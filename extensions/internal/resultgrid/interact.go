package resultgrid

import (
	"time"

	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/services"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
)

// doubleClick is how close two presses on the edge of a title are to fit the column.
const doubleClick = 450 * time.Millisecond

// isPrimary and isSecondary tell which button a press was.
func isPrimary(e *gestures.PointerDownEvent) bool { return e.Buttons()&gestures.SecondaryButton == 0 }

func isSecondary(e *gestures.PointerDownEvent) bool { return e.Buttons()&gestures.SecondaryButton != 0 }

// modsNow reads the keys held.
func (g *Grid) modsNow() Mods {
	if g.state == nil || !g.state.Mounted() {
		return Mods{}
	}
	kb := widgets.WidgetsBindingOf(g.state.Context()).Keyboard()
	return Mods{Shift: kb.IsShiftPressed(), Ctrl: kb.IsControlPressed()}
}

// colAt is the column under a position in a row (x counted from the left of the row), and -1 over the numbers.
func (g *Grid) colAt(x float64) int {
	if x < gutterWidth {
		return -1
	}
	x -= gutterWidth
	for c := range g.model.Columns() {
		w := g.model.Width(c)
		if x < w {
			return c
		}
		x -= w
	}
	return len(g.model.Columns()) - 1
}

// pressRow handles a press in the row r: a click selects the cell, or the row when it is on its number; the secondary button opens
// the menu.
func (g *Grid) pressRow(r int, e *gestures.PointerDownEvent) {
	c := g.colAt(e.LocalPosition().Dx)
	g.focus.RequestFocus()
	m := g.model
	switch {
	case isSecondary(e):
		if c < 0 {
			c = 0
		}
		r0, c0, r1, c1, ok := m.sel.Bounds(m.Len(), len(m.Columns()))
		if !ok || r < r0 || r > r1 || c < c0 || c > c1 {
			m.Press(r, c, Mods{})
		}
		g.showMenu(e.Position(), r, c)
	case c < 0:
		m.PressRow(r, g.modsNow())
	default:
		m.Press(r, c, g.modsNow())
	}
	g.refresh()
}

// startResize begins the drag of the right edge of the title of column c; a second press soon after fits the column to its content.
func (g *Grid) startResize(c int, e *gestures.PointerDownEvent) {
	if !isPrimary(e) {
		return
	}
	now := e.TimeStamp()
	if g.lastEdge.col == c && g.lastEdge.valid && !g.lastEdge.dragged && now-g.lastEdge.at < doubleClick {
		g.model.FitWidth(c)
		g.lastEdge.valid = false
		g.resize.active = false
		g.refresh()
		return
	}
	g.lastEdge.col, g.lastEdge.at, g.lastEdge.valid, g.lastEdge.dragged = c, now, true, false
	g.resize.active, g.resize.col = true, c
	g.resize.startX, g.resize.startWidth = e.Position().Dx, g.model.Width(c)
}

// moveResize follows the pointer while the edge of a title is dragged.
func (g *Grid) moveResize(e *gestures.PointerMoveEvent) {
	if !g.resize.active {
		return
	}
	if dx := e.Position().Dx - g.resize.startX; dx != 0 {
		g.lastEdge.dragged = true
	}
	g.model.SetWidth(g.resize.col, g.resize.startWidth+e.Position().Dx-g.resize.startX)
	g.refresh()
}

// ---- the menu ----

// showMenu opens the context menu at a position: for a cell (r >= 0), or for the title of column c (r == -1).
func (g *Grid) showMenu(at gui.Offset, r, c int) {
	m := g.model
	var items []ui.MenuItem
	copyItem := func(label string, text func() string) {
		items = append(items, ui.MenuItem{Label: label, OnRun: func() { g.copyText(text()) }})
	}
	if r >= 0 {
		if m.SelectedCells() > 1 {
			copyItem("Copy Selection", func() string { return m.CopySelection(false) })
			copyItem("Copy Selection with Headers", func() string { return m.CopySelection(true) })
		} else {
			copyItem("Copy Cell", func() string { return m.CopyCell(r, c) })
		}
		copyItem("Copy Row", func() string { return m.CopyRow(r, false) })
		copyItem("Copy Row with Headers", func() string { return m.CopyRow(r, true) })
		copyItem("Copy Column", func() string { return m.CopyColumn(c, false) })
		items = append(items, ui.SeparatorItem(), ui.MenuItem{Label: "Select All", OnRun: func() { m.SelectAll(); g.refresh() }})
	} else {
		copyItem("Copy Column", func() string { return m.CopyColumn(c, false) })
		copyItem("Copy Column with Header", func() string { return m.CopyColumn(c, true) })
		items = append(items, ui.SeparatorItem(),
			ui.MenuItem{Label: "Sort Ascending", OnRun: func() { m.SortBy(c, SortAsc); g.refresh() }},
			ui.MenuItem{Label: "Sort Descending", OnRun: func() { m.SortBy(c, SortDesc); g.refresh() }},
			ui.MenuItem{Label: "Clear Sort", OnRun: func() { m.SortBy(c, SortNone); g.refresh() }},
			ui.SeparatorItem(),
			ui.MenuItem{Label: "Select Column", OnRun: func() { m.PressColumn(c, Mods{}); g.refresh() }},
			ui.MenuItem{Label: "Fit Column Width", OnRun: func() { m.FitWidth(c); g.refresh() }})
	}
	if g.opts.MenuItems != nil {
		if extra := g.opts.MenuItems(); len(extra) > 0 {
			items = append(items, ui.SeparatorItem())
			items = append(items, extra...)
		}
	}
	g.menu.Show(at, items)
}

// ---- the keyboard ----

// handleKey is the keyboard of the grid: the arrows move the cell (Shift extends the selection), Home and End go to the ends of the
// row (with Ctrl, of the result), Page Up and Page Down by a page, Ctrl+A selects all, Ctrl+C copies the selection as TSV
// (Ctrl+Shift+C with the headers), Ctrl+Space and Shift+Space select the column and the row, Escape selects nothing.
func (s *gridState) handleKey(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
	switch e.(type) {
	case *services.KeyDownEvent, *services.KeyRepeatEvent:
	default:
		return widgets.KeyEventResultIgnored
	}
	g := s.Widget().g
	m := g.model
	kb := widgets.WidgetsBindingOf(s.Context()).Keyboard()
	ctrl, shift, alt := kb.IsControlPressed(), kb.IsShiftPressed(), kb.IsAltPressed()
	if alt || kb.IsMetaPressed() {
		return widgets.KeyEventResultIgnored
	}
	page := max(int(g.pageRows()), 1)
	f, has := m.Focus()
	key := e.LogicalKey()
	handled := true
	switch {
	case key == services.LogicalKeyboardKeyArrowDown:
		m.Move(1, 0, shift)
	case key == services.LogicalKeyboardKeyArrowUp:
		m.Move(-1, 0, shift)
	case key == services.LogicalKeyboardKeyArrowRight:
		m.Move(0, 1, shift)
	case key == services.LogicalKeyboardKeyArrowLeft:
		m.Move(0, -1, shift)
	case key == services.LogicalKeyboardKeyPageDown:
		m.Move(page, 0, shift)
	case key == services.LogicalKeyboardKeyPageUp:
		m.Move(-page, 0, shift)
	case key == services.LogicalKeyboardKeyHome && ctrl:
		m.MoveTo(0, 0, shift)
	case key == services.LogicalKeyboardKeyEnd && ctrl:
		m.MoveTo(m.Len()-1, len(m.Columns())-1, shift)
	case key == services.LogicalKeyboardKeyHome:
		m.MoveTo(f.Row, 0, shift)
	case key == services.LogicalKeyboardKeyEnd:
		m.MoveTo(f.Row, len(m.Columns())-1, shift)
	case key == services.LogicalKeyboardKeyKeyA && ctrl:
		m.SelectAll()
	case key == services.LogicalKeyboardKeyKeyC && ctrl:
		g.copyText(m.CopySelection(shift))
		return widgets.KeyEventResultHandled
	case key == services.LogicalKeyboardKeySpace && ctrl && has:
		m.PressColumn(f.Col, Mods{})
	case key == services.LogicalKeyboardKeySpace && shift && has:
		m.PressRow(f.Row, Mods{})
	case key == services.LogicalKeyboardKeyEscape && m.sel.Kind != SelNone:
		m.ClearSelection()
	default:
		handled = false
	}
	if !handled {
		return widgets.KeyEventResultIgnored
	}
	if nf, ok := m.Focus(); ok {
		g.reveal(nf)
	}
	g.refresh()
	return widgets.KeyEventResultHandled
}

// pageRows is how many rows fit in the view.
func (g *Grid) pageRows() float64 {
	if !g.vCtl.HasClients() {
		return 10
	}
	return g.vCtl.Position().ViewportDimension() / g.opts.RowHeight
}

// reveal scrolls so that a cell is in view.
func (g *Grid) reveal(c Cell) {
	if g.vCtl.HasClients() {
		p := g.vCtl.Position()
		y0 := float64(c.Row) * g.opts.RowHeight
		top, h := p.Pixels(), p.ViewportDimension()
		switch {
		case y0 < top:
			g.vCtl.JumpTo(max(y0, 0))
		case y0+g.opts.RowHeight > top+h:
			g.vCtl.JumpTo(min(y0+g.opts.RowHeight-h, p.MaxScrollExtent()))
		}
	}
	if g.hCtl.HasClients() && c.Col >= 0 && c.Col < len(g.model.Columns()) {
		p := g.hCtl.Position()
		x0 := gutterWidth
		for i := range c.Col {
			x0 += g.model.Width(i)
		}
		x1 := x0 + g.model.Width(c.Col)
		left, w := p.Pixels(), p.ViewportDimension()
		switch {
		case x0 < left:
			g.hCtl.JumpTo(max(x0-gutterWidth, 0))
		case x1 > left+w:
			g.hCtl.JumpTo(min(x1-w, p.MaxScrollExtent()))
		}
	}
}
