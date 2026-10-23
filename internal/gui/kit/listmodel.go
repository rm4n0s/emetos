package kit

import "strings"

// ListItem is one row of a list: its label, an optional description shown after it and an
// optional Codicon.
type ListItem struct {
	// Label is the text of the row; typing filters by it.
	Label string
	// Description is shown after the label in a dimmer color.
	Description string
	// Icon is the code point of a Codicon shown before the label; 0 shows none.
	Icon int
	// Disabled rows are shown dimmed, are skipped by the keyboard and cannot be chosen.
	Disabled bool
}

// ListModel is the state of a list with a filter: the items it shows, the row that has the focus
// and the rows that are in view. It is what ListOverlay's keyboard moves and is tested without a
// window.
type ListModel struct {
	items  []ListItem
	filter string
	// shown are the indexes of the items that match the filter, in order.
	shown []int
	// focus is the position in shown of the focused row, or -1 when nothing can be focused.
	focus int
	// top is the position in shown of the first row in view.
	top int
	// rows is the number of rows in view.
	rows int
}

// NewListModel makes a model of items that shows at most rows rows at a time (at least 1), with
// the focus on the first row that is not disabled.
func NewListModel(items []ListItem, rows int) *ListModel {
	m := &ListModel{items: items, rows: max(rows, 1)}
	m.SetFilter("")
	return m
}

// Items are all the items, whatever the filter says.
func (m *ListModel) Items() []ListItem { return m.items }

// Filter is the text the items are filtered by.
func (m *ListModel) Filter() string { return m.filter }

// SetFilter shows the items whose label contains the text, ignoring case ("" shows all), and
// moves the focus to the first of them that is not disabled.
func (m *ListModel) SetFilter(text string) {
	m.filter = text
	needle := strings.ToLower(text)
	m.shown = m.shown[:0]
	for i, it := range m.items {
		if needle == "" || strings.Contains(strings.ToLower(it.Label), needle) {
			m.shown = append(m.shown, i)
		}
	}
	m.top = 0
	m.focus = -1
	m.moveTo(0, 1)
}

// Shown are the indexes of the items that the filter lets through.
func (m *ListModel) Shown() []int { return m.shown }

// Focused is the index of the focused item, or -1 when no row can be focused.
func (m *ListModel) Focused() int {
	if m.focus < 0 || m.focus >= len(m.shown) {
		return -1
	}
	return m.shown[m.focus]
}

// FocusedRow is the position of the focused row among the shown ones, or -1.
func (m *ListModel) FocusedRow() int { return m.focus }

// Top is the position of the first row in view.
func (m *ListModel) Top() int { return m.top }

// InView are the indexes of the items of the rows in view, from the top.
func (m *ListModel) InView() []int {
	end := min(m.top+m.rows, len(m.shown))
	return m.shown[m.top:end]
}

// FocusItem focuses an item and scrolls it into view. It does nothing when the item is not shown
// or is disabled.
func (m *ListModel) FocusItem(item int) {
	for row, i := range m.shown {
		if i == item && !m.items[i].Disabled {
			m.focus = row
			m.reveal()
			return
		}
	}
}

// Move moves the focus by rows (negative goes up), skipping the disabled ones. It stops at the
// first or last row; it does not wrap round.
func (m *ListModel) Move(delta int) {
	if delta == 0 || m.focus < 0 {
		return
	}
	step := 1
	if delta < 0 {
		step = -1
	}
	target := min(max(m.focus+delta, 0), len(m.shown)-1)
	// the nearest row that can be focused, looking the way of the move first
	if !m.moveTo(target, step) {
		m.moveTo(target, -step)
	}
}

// Page moves the focus by the rows in view less one (negative goes up).
func (m *ListModel) Page(direction int) {
	if direction == 0 {
		return
	}
	m.Move(direction * max(m.rows-1, 1))
}

// First focuses the first row that is not disabled.
func (m *ListModel) First() { m.moveTo(0, 1) }

// Last focuses the last row that is not disabled.
func (m *ListModel) Last() { m.moveTo(len(m.shown)-1, -1) }

// Scroll moves the rows in view by rows (negative goes up) without changing the focus.
func (m *ListModel) Scroll(rows int) {
	m.top = min(max(m.top+rows, 0), max(len(m.shown)-m.rows, 0))
}

// moveTo focuses the first row that is not disabled from a position, looking in a direction
// (1 or -1), and reports whether there was one.
func (m *ListModel) moveTo(from, step int) bool {
	for row := from; row >= 0 && row < len(m.shown); row += step {
		if !m.items[m.shown[row]].Disabled {
			m.focus = row
			m.reveal()
			return true
		}
	}
	return false
}

// reveal scrolls the focused row into view.
func (m *ListModel) reveal() {
	if m.focus < 0 {
		return
	}
	if m.focus < m.top {
		m.top = m.focus
	}
	if m.focus >= m.top+m.rows {
		m.top = m.focus - m.rows + 1
	}
}
