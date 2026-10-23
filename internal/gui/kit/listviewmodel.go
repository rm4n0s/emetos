package kit

// Ported from VS Code: src/vs/base/browser/ui/list/listWidget.ts (the list's focus, selection, anchor and
// keyboard), src/vs/base/browser/ui/list/listView.ts (rows of different heights) and the `list.*` commands
// of src/vs/workbench/browser/actions/listCommands.ts.

import (
	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
)

// ListViewItem is the data of a row that is given to a ListViewModel. Rows are named by their keys: the
// focus and the selection are kept by key when the items are given again. Keys are not empty and unique.
type ListViewItem struct {
	// Key names the row.
	Key string
	// Data is the caller's value for the row; the row renderer receives it. It is never compared.
	Data any
	// Label is the text of the row that a find matches against and highlights; it defaults to Data when
	// that is a string.
	Label string
}

// ListViewRow is a row of a ListViewModel: an item that is shown.
type ListViewRow struct {
	// Key is the key of the item.
	Key string
	// Data is the item's Data.
	Data any
	// Label is the item's Label, or its Data when that is a string.
	Label string
	// Index is the position of the row among the rows that are shown.
	Index int
}

// ListViewModelOpts are the options of NewListViewModel.
type ListViewModelOpts struct {
	// RowHeight is the height of a row in pixels; the default is DefaultTreeRowHeight (22). It is the height
	// of every row unless HeightOf is set.
	RowHeight foundation.Opt[float64]
	// HeightOf gives the height of the row at an index among the rows that are shown (rows of different
	// heights, `IListVirtualDelegate.getHeight`). It is called when the rows change, for every row.
	HeightOf func(index int) float64
	// MultiSelect lets Ctrl and Shift select several rows.
	MultiSelect bool
	// FindMode is what a pattern typed into the list does to the rows, and FindMatchType how it is matched;
	// the defaults are VS Code's, FindModeHighlight and FindMatchFuzzy.
	FindMode      FindMode
	FindMatchType FindMatchType
	// SelectionNavigation says what Escape does with several rows selected: it reduces the selection to the
	// focused row (the default) or clears it.
	SelectionNavigation foundation.Opt[bool]
}

// ListAction is a key command of a list, as VS Code runs it (`list.*` commands).
type ListAction int

const (
	// ListActionDown focuses the next row (`list.focusDown`).
	ListActionDown ListAction = iota
	// ListActionUp focuses the previous row (`list.focusUp`).
	ListActionUp
	// ListActionFirst focuses the first row (`list.focusFirst`, Home).
	ListActionFirst
	// ListActionLast focuses the last row (`list.focusLast`, End).
	ListActionLast
	// ListActionPageDown focuses the last row in view, or scrolls a page when it is focused already
	// (`list.focusPageDown`).
	ListActionPageDown
	// ListActionPageUp focuses the first row in view, or scrolls a page (`list.focusPageUp`).
	ListActionPageUp
	// ListActionSelect selects the focused row and opens it (`list.select`, Enter).
	ListActionSelect
	// ListActionToggle selects the focused row without opening it (`list.toggleExpand` on a list, Space).
	ListActionToggle
	// ListActionClear reduces several selected rows to the focused one, or clears the selection, or the focus
	// when nothing is selected (`list.clear`, Escape).
	ListActionClear
	// ListActionExpandSelectionDown moves the focus down and extends the selection
	// (`list.expandSelectionDown`, Shift+Down). It needs MultiSelect.
	ListActionExpandSelectionDown
	// ListActionExpandSelectionUp moves the focus up and extends the selection (Shift+Up).
	ListActionExpandSelectionUp
	// ListActionSelectAll selects every row that is shown (`list.selectAll`, Ctrl+A).
	ListActionSelectAll
	// ListActionToggleSelection adds the focused row to the selection or takes it out
	// (`list.toggleSelection`, Ctrl+Space and Ctrl+Shift+Enter).
	ListActionToggleSelection
)

// ListResult is what an action or a click did beyond changing the model.
type ListResult struct {
	// Handled says that the key or click was used. A key that is not handled goes on to the widgets above.
	Handled bool
	// Select says that the user selected the row Key (which the model has selected).
	Select bool
	// Open says that the user asked to open the row: Enter, or a double click.
	Open bool
	// Key is the key of the row that Select and Open are about; Index its index, -1 when it is not shown.
	Key   string
	Index int
	// SelectionChanged says that the selected rows are not the ones they were.
	SelectionChanged bool
	// ScrollTo is the scroll position that shows the row that was focused, or the page that was asked for,
	// when it is not where the view is now.
	ScrollTo foundation.Opt[float64]
}

// ListViewModel is the state of a List: its items, the rows that are shown (the items that match the find
// when it filters), the focused row, the selected rows and the keyboard. It is what the List widget shows
// and moves, and it is tested without a window. It is used on the UI thread only and tells its listeners
// when anything changed.
type ListViewModel struct {
	foundation.ChangeNotifier
	rowNav
	opts  ListViewModelOpts
	items []ListViewItem
	byKey map[string]int
	rows  []ListViewRow
}

// NewListViewModel makes an empty model.
func NewListViewModel(opts ...ListViewModelOpts) *ListViewModel {
	m := &ListViewModel{byKey: map[string]int{}}
	if len(opts) > 0 {
		m.opts = opts[0]
	}
	m.rowNav = newRowNav(max(m.opts.RowHeight.OrElse(DefaultTreeRowHeight), 1))
	m.has = func(key string) bool { _, ok := m.byKey[key]; return ok }
	m.notify = m.NotifyListeners
	m.rebuild = m.makeRows
	m.sel.SetMulti(m.opts.MultiSelect)
	m.find.mode, m.find.matchType = m.opts.FindMode, m.opts.FindMatchType
	m.selectionNavigation = m.opts.SelectionNavigation.OrElse(true)
	return m
}

// RowHeight is the height of a row when HeightOf is not set.
func (m *ListViewModel) RowHeight() float64 { return m.fixed }

// SetItems replaces all the items. The focus and the selection are kept by key when their items are still
// there. An item without a key, or two with the same key, are refused (ListItemKeyEmpty, ListItemKeyDuplicated)
// and nothing changes.
func (m *ListViewModel) SetItems(items []ListViewItem) error {
	seen := make(map[string]bool, len(items))
	for _, it := range items {
		switch {
		case it.Key == "":
			return errors.New("ListItemKeyEmpty", "a list item has no key")
		case seen[it.Key]:
			return errors.New("ListItemKeyDuplicated", "two list items have the same key", "key", it.Key)
		}
		seen[it.Key] = true
	}
	m.items = append(m.items[:0], items...)
	for i := range m.items {
		if m.items[i].Label == "" {
			if s, ok := m.items[i].Data.(string); ok {
				m.items[i].Label = s
			}
		}
	}
	clear(m.byKey)
	for i, it := range m.items {
		m.byKey[it.Key] = i
	}
	m.makeRows()
	return nil
}

// makeRows finds the rows that are shown, fixes the focus and the selection and tells the listeners.
func (m *ListViewModel) makeRows() {
	m.find.reset()
	m.rows = m.rows[:0]
	for _, it := range m.items {
		if m.find.active() && !m.find.consider(it.Key, it.Label) && m.find.mode == FindModeFilter {
			continue
		}
		m.rows = append(m.rows, ListViewRow{Key: it.Key, Data: it.Data, Label: it.Label, Index: len(m.rows)})
	}
	keys := make([]string, len(m.rows))
	for i, r := range m.rows {
		keys[i] = r.Key
	}
	m.setRows(keys, nil, m.opts.HeightOf)
	m.sel.Prune(m.has)
	if _, ok := m.index[m.focus]; !ok {
		m.focus = ""
	}
	m.NotifyListeners()
}

// Rows are the rows that are shown, in order. The slice is the model's: do not change it, and do not keep it
// across a change of the model.
func (m *ListViewModel) Rows() []ListViewRow { return m.rows }

// Row is the row at an index; the zero row (with index -1) when there is none.
func (m *ListViewModel) Row(i int) ListViewRow {
	if i < 0 || i >= len(m.rows) {
		return ListViewRow{Index: -1}
	}
	return m.rows[i]
}

// IndexOf is the index of the row of a key, or -1 when the item is not shown.
func (m *ListViewModel) IndexOf(key string) int { return m.IndexOfKey(key) }

// Item is the item with a key, shown or not.
func (m *ListViewModel) Item(key string) (ListViewItem, bool) {
	if i, ok := m.byKey[key]; ok {
		return m.items[i], true
	}
	return ListViewItem{}, false
}

// SelectedIndexes are the indexes of the selected rows that are shown, in order.
func (m *ListViewModel) SelectedIndexes() []int {
	var out []int
	for _, k := range m.SelectedKeys() {
		if i := m.IndexOfKey(k); i >= 0 {
			out = append(out, i)
		}
	}
	return out
}

// RowTop is the position of the top of the row at an index in the content, and RowHeightAt its height.
func (m *ListViewModel) RowTop(i int) float64 { return m.rowTop(i) }

// RowHeightAt is the height of the row at an index.
func (m *ListViewModel) RowHeightAt(i int) float64 { return m.rowHeightAt(i) }

// ContentHeight is the height of all the rows.
func (m *ListViewModel) ContentHeight() float64 { return m.contentHeight() }

// RowAt is the index of the row at a position of the content, -1 above the first row and Len() below the
// last.
func (m *ListViewModel) RowAt(y float64) int { return m.indexAt(y) }

// Reveal gives the index of the row of a key and the scroll position that shows it with the least scrolling
// (none when it is in view); false when the item is not shown.
func (m *ListViewModel) Reveal(key string) (int, foundation.Opt[float64], bool) {
	i := m.IndexOfKey(key)
	if i < 0 {
		return -1, foundation.None[float64](), false
	}
	return i, m.revealOffset(i), true
}

// result converts what the shared navigation did into a ListResult.
func (m *ListViewModel) result(r navResult) ListResult {
	res := ListResult{Handled: r.handled, Select: r.selected, Open: r.open, Key: r.key, Index: -1, SelectionChanged: r.selectionChanged, ScrollTo: r.scrollTo}
	if r.key != "" {
		res.Index = m.IndexOfKey(r.key)
	}
	return res
}

// Perform runs a key command of the list. The scroll position that results, if it is not where the view is,
// is in the result; the widget moves the view there.
func (m *ListViewModel) Perform(a ListAction) ListResult {
	if len(m.rows) == 0 && a != ListActionClear {
		return ListResult{}
	}
	switch a {
	case ListActionDown:
		return m.result(m.down())
	case ListActionUp:
		return m.result(m.up())
	case ListActionFirst:
		return m.result(m.first())
	case ListActionLast:
		return m.result(m.last())
	case ListActionPageDown:
		return m.result(m.pageDown())
	case ListActionPageUp:
		return m.result(m.pageUp())
	case ListActionExpandSelectionDown:
		return m.result(m.expandSelection(1))
	case ListActionExpandSelectionUp:
		return m.result(m.expandSelection(-1))
	case ListActionSelectAll:
		return m.result(m.selectAll())
	case ListActionToggleSelection:
		return m.result(m.toggleSelection())
	case ListActionSelect, ListActionToggle:
		key, changed, ok := m.selectFocused()
		if !ok {
			return ListResult{}
		}
		return ListResult{Handled: true, Select: true, Open: a == ListActionSelect, Key: key, Index: m.IndexOfKey(key), SelectionChanged: changed}
	case ListActionClear:
		return m.result(m.clear())
	}
	return ListResult{}
}

// Click is a press of the primary button on the row of a key, as VS Code's `MouseController` treats it: the
// row is focused and selected alone; with MultiSelect, Ctrl toggles it in the selection, Shift selects the
// range from the anchor and both add that range to the selection.
func (m *ListViewModel) Click(key string, mods ClickModifiers) ListResult {
	i := m.IndexOfKey(key)
	if i < 0 {
		return ListResult{}
	}
	if !m.MultiSelect() {
		mods = ClickModifiers{}
	}
	r := m.press(key, mods)
	return ListResult{Handled: true, Select: m.IsSelected(key), Key: key, Index: i, SelectionChanged: r.selectionChanged}
}

// DoubleClick is the second press of a double click on a row: the row is opened.
func (m *ListViewModel) DoubleClick(key string) ListResult {
	i := m.IndexOfKey(key)
	if i < 0 {
		return ListResult{}
	}
	m.SetFocus(key)
	return ListResult{Handled: true, Open: true, Key: key, Index: i}
}
