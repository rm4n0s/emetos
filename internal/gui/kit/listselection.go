package kit

// Ported from VS Code: src/vs/base/browser/ui/list/listWidget.ts (MouseController.changeSelection and
// DefaultMultipleSelectionController: the selection that a click with Ctrl or Shift makes, the anchor),
// src/vs/workbench/browser/actions/listCommands.ts (expandMultiSelection, list.selectAll, list.toggleSelection).
// Not ported: the groups of an identity provider (getGroupId), and the scope of Ctrl+A in a tree (the whole
// list is selected, as in a list).

import "slices"

// RowSource is what a ListSelectionModel needs to know about the rows it selects among: how many there are,
// which key a row has and where a key is. The rows that are shown are the ones that count; a key that is not
// among them is "hidden" (a node of a tree under a collapsed one).
type RowSource interface {
	// Len is the number of rows.
	Len() int
	// KeyAt is the key of the row at an index.
	KeyAt(i int) string
	// IndexOfKey is the index of the row of a key, or -1 when the key is not shown.
	IndexOfKey(key string) int
	// Selectable says that the row at an index can be selected (a placeholder cannot).
	Selectable(i int) bool
}

// ClickModifiers are the keys held while a row is clicked: Ctrl toggles the row, Shift selects the range
// from the anchor, both add that range to the selection.
type ClickModifiers struct {
	// Ctrl is whether Ctrl is held (Meta on a Mac; Emetos targets Linux only).
	Ctrl bool
	// Shift is whether Shift is held.
	Shift bool
}

// ListSelectionModel is the selection of a list or a tree, by key, with the anchor that a range selection
// starts from. Without multi selection it holds at most one key. It is the part of VS Code's `List` that
// `MouseController` and the `list.*` commands use, and is tested without rows of a widget: a RowSource
// gives the order of the rows.
//
// The selection keeps the order in which keys were selected; Primary is the key selected last, which a
// single-selection API reports as "the" selection. The zero value is an empty single selection.
type ListSelectionModel struct {
	multi   bool
	keys    []string
	set     map[string]bool
	primary string
	anchor  string
}

// NewListSelectionModel makes an empty selection; multi lets it hold several keys.
func NewListSelectionModel(multi bool) *ListSelectionModel { return &ListSelectionModel{multi: multi} }

// Multi says that the selection can hold several keys.
func (m *ListSelectionModel) Multi() bool { return m.multi }

// SetMulti turns multi selection on or off. Turning it off keeps only the primary key.
func (m *ListSelectionModel) SetMulti(multi bool) {
	m.multi = multi
	if !multi && len(m.keys) > 1 {
		m.replace([]string{m.Primary()}, m.Primary())
	}
}

// Len is the number of selected keys.
func (m *ListSelectionModel) Len() int { return len(m.keys) }

// Has says that the key is selected.
func (m *ListSelectionModel) Has(key string) bool { return m.set[key] }

// Keys are the selected keys in the order they were selected. The slice is a copy.
func (m *ListSelectionModel) Keys() []string { return slices.Clone(m.keys) }

// Sorted are the selected keys in the order of the rows; the keys that are not shown come first, in the
// order they were selected.
func (m *ListSelectionModel) Sorted(rows RowSource) []string {
	var hidden []string
	type shown struct {
		index int
		key   string
	}
	var in []shown
	for _, k := range m.keys {
		if i := rows.IndexOfKey(k); i >= 0 {
			in = append(in, shown{i, k})
		} else {
			hidden = append(hidden, k)
		}
	}
	slices.SortFunc(in, func(a, b shown) int { return a.index - b.index })
	for _, s := range in {
		hidden = append(hidden, s.key)
	}
	return hidden
}

// Primary is the key that was selected last; false when nothing is selected.
func (m *ListSelectionModel) Primary() string {
	if len(m.keys) == 0 {
		return ""
	}
	if m.set[m.primary] {
		return m.primary
	}
	return m.keys[len(m.keys)-1]
}

// Anchor is the key that a range selection starts from; false when there is none.
func (m *ListSelectionModel) Anchor() (string, bool) { return m.anchor, m.anchor != "" }

// SetAnchor sets the key that a range selection starts from ("" clears it).
func (m *ListSelectionModel) SetAnchor(key string) { m.anchor = key }

// replace sets the selected keys; it reports whether the set of keys changed.
func (m *ListSelectionModel) replace(keys []string, primary string) bool {
	changed := len(keys) != len(m.keys)
	set := make(map[string]bool, len(keys))
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if set[k] {
			continue
		}
		set[k] = true
		out = append(out, k)
	}
	if len(out) != len(m.keys) {
		changed = true
	}
	for k := range set {
		if !m.set[k] {
			changed = true
		}
	}
	m.keys, m.set = out, set
	m.primary = primary
	if !set[primary] {
		m.primary = ""
	}
	return changed
}

// Set selects the keys, in the order given; without multi selection only the last is kept. It reports
// whether the selection changed.
func (m *ListSelectionModel) Set(keys ...string) bool {
	if !m.multi && len(keys) > 1 {
		keys = keys[len(keys)-1:]
	}
	primary := ""
	if len(keys) > 0 {
		primary = keys[len(keys)-1]
	}
	return m.replace(keys, primary)
}

// Select selects one key and makes it the anchor.
func (m *ListSelectionModel) Select(key string) bool {
	m.anchor = key
	return m.Set(key)
}

// Clear selects nothing (the anchor stays). It reports whether the selection changed.
func (m *ListSelectionModel) Clear() bool { return m.replace(nil, "") }

// Remove takes a key out of the selection. It reports whether it was selected.
func (m *ListSelectionModel) Remove(key string) bool {
	if !m.set[key] {
		return false
	}
	keys := slices.DeleteFunc(slices.Clone(m.keys), func(k string) bool { return k == key })
	primary := m.primary
	if primary == key {
		primary = ""
		if len(keys) > 0 {
			primary = keys[len(keys)-1]
		}
	}
	return m.replace(keys, primary)
}

// Add adds a key to the selection (a single selection is replaced) and makes it the primary one.
func (m *ListSelectionModel) Add(key string) bool {
	if m.set[key] {
		if m.primary == key {
			return false
		}
		m.primary = key
		return false
	}
	if !m.multi {
		return m.replace([]string{key}, key)
	}
	return m.replace(append(slices.Clone(m.keys), key), key)
}

// Toggle selects a key that is not selected and deselects one that is, and makes it the anchor.
func (m *ListSelectionModel) Toggle(key string) bool {
	m.anchor = key
	if m.set[key] {
		return m.Remove(key)
	}
	return m.Add(key)
}

// Prune takes the keys out of the selection that the function does not know any more (the nodes that left
// a tree). A lost anchor is cleared. It reports whether the selection changed.
func (m *ListSelectionModel) Prune(has func(key string) bool) bool {
	if m.anchor != "" && !has(m.anchor) {
		m.anchor = ""
	}
	var keys []string
	for _, k := range m.keys {
		if has(k) {
			keys = append(keys, k)
		}
	}
	if len(keys) == len(m.keys) {
		return false
	}
	return m.replace(keys, m.primary)
}

// indexes are the indexes of the selected keys that are shown, sorted, and the keys that are not shown.
func (m *ListSelectionModel) indexes(rows RowSource) (idx []int, hidden []string) {
	for _, k := range m.keys {
		if i := rows.IndexOfKey(k); i >= 0 {
			idx = append(idx, i)
		} else {
			hidden = append(hidden, k)
		}
	}
	slices.Sort(idx)
	return idx, hidden
}

// compose selects the hidden keys and the rows at the indexes (sorted), with the primary key given.
func (m *ListSelectionModel) compose(rows RowSource, hidden []string, idx []int, primary string) bool {
	keys := slices.Clone(hidden)
	for _, i := range idx {
		keys = append(keys, rows.KeyAt(i))
	}
	return m.replace(keys, primary)
}

// SelectRange selects the rows from one key to another, both included. With add the range joins the
// selection; without it the rows of the selection that touch the anchor (a run of selected rows with the
// anchor in it) are replaced by the range, as VS Code's shift click does, so a block of selected rows
// elsewhere stays. The primary key becomes `to`. It reports whether the selection changed.
func (m *ListSelectionModel) SelectRange(rows RowSource, anchor, to string, add bool) bool {
	a, b := rows.IndexOfKey(anchor), rows.IndexOfKey(to)
	if a < 0 || b < 0 {
		return false
	}
	lo, hi := min(a, b), max(a, b)
	var span []int
	for i := lo; i <= hi; i++ {
		if rows.Selectable(i) {
			span = append(span, i)
		}
	}
	sel, hidden := m.indexes(rows)
	var next []int
	if add {
		next = unionSorted(sel, span)
	} else {
		in := make(map[int]bool, len(sel)+1)
		for _, i := range sel {
			in[i] = true
		}
		in[a] = true
		first, last := a, a
		for in[first-1] {
			first--
		}
		for in[last+1] {
			last++
		}
		var keep []int
		for _, i := range sel {
			if i < first || i > last {
				keep = append(keep, i)
			}
		}
		next = unionSorted(keep, span)
	}
	return m.compose(rows, hidden, next, to)
}

func unionSorted(a, b []int) []int {
	out := slices.Concat(a, b)
	slices.Sort(out)
	return slices.Compact(out)
}

// SelectAll selects every row that can be selected and clears the anchor (`list.selectAll`).
func (m *ListSelectionModel) SelectAll(rows RowSource) bool {
	if !m.multi {
		return false
	}
	var all []int
	for i := range rows.Len() {
		if rows.Selectable(i) {
			all = append(all, i)
		}
	}
	m.anchor = ""
	_, hidden := m.indexes(rows)
	return m.compose(rows, hidden, all, m.primary)
}

// Press is a click on the row of a key, as VS Code's `MouseController.onViewPointer` makes it: without
// modifiers the row is selected alone and becomes the anchor; with Ctrl it is toggled; with Shift the range
// from the anchor (the focused row when there is no anchor) is selected; with both the range joins the
// selection. focus is the key of the focused row before the click. Modifiers are ignored without multi
// selection. It reports whether the selection changed; a row that cannot be selected changes nothing.
func (m *ListSelectionModel) Press(rows RowSource, key, focus string, mods ClickModifiers) bool {
	i := rows.IndexOfKey(key)
	if i < 0 || !rows.Selectable(i) {
		return false
	}
	if !m.multi {
		mods = ClickModifiers{}
	}
	switch {
	case mods.Shift:
		if rows.IndexOfKey(m.anchor) < 0 {
			m.anchor = key
			if rows.IndexOfKey(focus) >= 0 {
				m.anchor = focus
			}
		}
		return m.SelectRange(rows, m.anchor, key, mods.Ctrl)
	case mods.Ctrl:
		return m.Toggle(key)
	}
	return m.Select(key)
}

// ExpandTo is the selection that Shift+Up and Shift+Down make once the focus moved from one row to another
// (`expandMultiSelection`): when the new row is selected the row the focus left is deselected (the selection
// shrinks), else the new row is added. Nothing happens when the focus did not move. It reports whether the
// selection changed.
func (m *ListSelectionModel) ExpandTo(previous, focus string) bool {
	if !m.multi || focus == "" || previous == focus {
		return false
	}
	if m.set[focus] {
		return m.Remove(previous)
	}
	return m.Add(focus)
}
