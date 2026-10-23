package kit

// Ported from VS Code: src/vs/base/browser/ui/list/listWidget.ts (focusNext, focusPrevious, focusNextPage,
// focusPreviousPage, reveal, the anchor that the keys set), src/vs/base/browser/ui/list/listView.ts (the
// positions of the rows) and src/vs/workbench/browser/actions/listCommands.ts (list.expandSelectionDown,
// list.expandSelectionUp, list.selectAll, list.toggleSelection, list.clear).

import (
	"sort"

	"github.com/rm4n0s/glutter/foundation"
)

// navResult is what a key command or a click did, in the terms both lists and trees share.
type navResult struct {
	// handled says that the key or click was used.
	handled bool
	// selectionChanged says that the selected keys are not the ones they were.
	selectionChanged bool
	// selected says that the user selected the row of key; open that he opened it.
	selected, open bool
	key            string
	// scrollTo is the scroll position that shows the row that was focused, or the page asked for.
	scrollTo foundation.Opt[float64]
}

// rowNav is what a TreeModel and a ListViewModel have in common: rows named by keys with a height each, the
// view they are scrolled in, the focused row, the selection and the find. It holds the rules of the keyboard
// (Up, Down, Home, End, PageUp, PageDown, Shift+Up, Shift+Down, Ctrl+A, Ctrl+Space, Escape) and of the
// mouse; the owner makes the rows and says when they are made again.
type rowNav struct {
	keys    []string
	index   map[string]int
	blocked []bool
	// fixed is the height of every row, unless tops is set: then row i is at tops[i] and as high as the
	// distance to tops[i+1].
	fixed float64
	tops  []float64
	// top and height are the scroll position and the height of the view, as the widget last told.
	top, height float64

	focus string
	sel   ListSelectionModel
	find  findCore
	// selectionNavigation makes Escape reduce several selected rows to the focused one, in place of
	// clearing the selection (VS Code's `list.selectionNavigation`).
	selectionNavigation bool

	// has says that the owner knows a key, shown or not; notify tells the listeners of a change that needs
	// no new rows; rebuild makes the rows again (and notifies).
	has     func(key string) bool
	notify  func()
	rebuild func()
}

func newRowNav(fixed float64) rowNav {
	return rowNav{index: map[string]int{}, fixed: fixed, selectionNavigation: true}
}

// setRows gives the rows. A nil heightOf makes every row as high as the fixed height; blocked marks the
// rows that can be neither focused nor selected.
func (n *rowNav) setRows(keys []string, blocked []bool, heightOf func(i int) float64) {
	n.keys = append(n.keys[:0], keys...)
	n.blocked = append(n.blocked[:0], blocked...)
	clear(n.index)
	for i, k := range n.keys {
		n.index[k] = i
	}
	n.tops = n.tops[:0]
	if heightOf == nil {
		n.tops = nil
		return
	}
	y := 0.0
	for i := range n.keys {
		n.tops = append(n.tops, y)
		y += max(heightOf(i), 1)
	}
	n.tops = append(n.tops, y)
}

// Len is the number of rows.
func (n *rowNav) Len() int { return len(n.keys) }

// KeyAt is the key of the row at an index (RowSource).
func (n *rowNav) KeyAt(i int) string { return n.keys[i] }

// IndexOfKey is the index of the row of a key, or -1 (RowSource).
func (n *rowNav) IndexOfKey(key string) int {
	if i, ok := n.index[key]; ok {
		return i
	}
	return -1
}

// Selectable says that the row at an index can be selected (RowSource).
func (n *rowNav) Selectable(i int) bool { return i >= 0 && i < len(n.keys) && !n.isBlocked(i) }

func (n *rowNav) isBlocked(i int) bool { return i < len(n.blocked) && n.blocked[i] }

// rowTop is the position of the top of row i in the content.
func (n *rowNav) rowTop(i int) float64 {
	if n.tops != nil {
		return n.tops[min(max(i, 0), len(n.tops)-1)]
	}
	return float64(i) * n.fixed
}

// rowHeightAt is the height of row i.
func (n *rowNav) rowHeightAt(i int) float64 {
	if n.tops != nil && i >= 0 && i < len(n.tops)-1 {
		return n.tops[i+1] - n.tops[i]
	}
	return n.fixed
}

// contentHeight is the height of all the rows.
func (n *rowNav) contentHeight() float64 {
	if n.tops != nil {
		return n.tops[len(n.tops)-1]
	}
	return float64(len(n.keys)) * n.fixed
}

// indexAt is the index of the row at a position of the content; the number of rows past the end, -1 above
// the first.
func (n *rowNav) indexAt(y float64) int {
	if y < 0 {
		return -1
	}
	if n.tops == nil {
		return min(int(y/n.fixed), len(n.keys))
	}
	if y >= n.tops[len(n.tops)-1] {
		return len(n.keys)
	}
	// the last row whose top is not below y
	return sort.Search(len(n.keys), func(i int) bool { return n.tops[i+1] > y })
}

// SetViewport tells the model where the view is: how far the content is scrolled and how high the view
// is, in pixels. The keyboard's page commands and the scrolling to the focused row use it.
func (n *rowNav) SetViewport(top, height float64) { n.top, n.height = max(top, 0), max(height, 0) }

// RowsInView is how many rows fit in the view completely, counted from the first row in view (at least 1).
func (n *rowNav) RowsInView() int {
	if n.tops == nil {
		return max(int(n.height/n.fixed), 1)
	}
	first := max(n.indexAt(n.top), 0)
	count := 0
	for i := first; i < len(n.keys) && n.rowTop(i+1) <= n.top+n.height; i++ {
		count++
	}
	return max(count, 1)
}

// maxTop is the farthest the content scrolls.
func (n *rowNav) maxTop() float64 { return max(n.contentHeight()-n.height, 0) }

// revealOffset is the scroll position that shows a row with the least scrolling (`ListView.reveal`): none
// when the row is in view. It records the new position as the view's.
func (n *rowNav) revealOffset(index int) foundation.Opt[float64] {
	if index < 0 || index >= len(n.keys) || n.height <= 0 {
		return foundation.None[float64]()
	}
	top, bottom := n.rowTop(index), n.rowTop(index+1)
	target := n.top
	switch {
	case top < n.top:
		target = top
	case bottom > n.top+n.height:
		target = bottom - n.height
	}
	target = min(max(target, 0), n.maxTop())
	if target == n.top {
		return foundation.None[float64]()
	}
	n.top = target
	return foundation.Some(target)
}

// focusable says that the keyboard may move the focus to the row at an index: it is not blocked, and while a
// find restricts the focus the row matches.
func (n *rowNav) focusable(i int) bool {
	if i < 0 || i >= len(n.keys) || n.isBlocked(i) {
		return false
	}
	return !n.find.restrictsFocus() || n.find.isMatch(n.keys[i])
}

// nextFocusable is the index of the first row from i on that the keyboard can focus, or -1.
func (n *rowNav) nextFocusable(i int) int {
	for ; i < len(n.keys); i++ {
		if n.focusable(i) {
			return i
		}
	}
	return -1
}

// previousFocusable is the index of the last row from i down that the keyboard can focus, or -1.
func (n *rowNav) previousFocusable(i int) int {
	for i = min(i, len(n.keys)-1); i >= 0; i-- {
		if n.focusable(i) {
			return i
		}
	}
	return -1
}

func (n *rowNav) tell() {
	if n.notify != nil {
		n.notify()
	}
}

// Focused is the key of the focused row, if there is one.
func (n *rowNav) Focused() (string, bool) { return n.focus, n.focus != "" }

// FocusedIndex is the index of the focused row, or -1.
func (n *rowNav) FocusedIndex() int {
	if i, ok := n.index[n.focus]; ok && n.focus != "" {
		return i
	}
	return -1
}

// SetFocus focuses a row that is shown; it reports whether it could (a placeholder cannot be).
func (n *rowNav) SetFocus(key string) bool {
	i, ok := n.index[key]
	if !ok || n.isBlocked(i) {
		return false
	}
	if n.focus != key {
		n.focus = key
		n.tell()
	}
	return true
}

// ClearFocus takes the focus off the rows.
func (n *rowNav) ClearFocus() {
	if n.focus != "" {
		n.focus = ""
		n.tell()
	}
}

// EnsureFocus gives the focus to a row when none has it, as a list that takes the keyboard focus does:
// the selected row when it is shown, else the first row that can be focused.
func (n *rowNav) EnsureFocus() {
	if n.FocusedIndex() >= 0 {
		return
	}
	if p := n.sel.Primary(); p != "" {
		if _, ok := n.index[p]; ok {
			n.SetFocus(p)
			return
		}
	}
	if i := n.nextFocusable(0); i >= 0 {
		n.SetFocus(n.keys[i])
	}
}

// MultiSelect says that several rows can be selected.
func (n *rowNav) MultiSelect() bool { return n.sel.Multi() }

// SetMultiSelect turns the selection of several rows on or off; off keeps the row that was selected last.
func (n *rowNav) SetMultiSelect(on bool) {
	if n.sel.Multi() != on {
		n.sel.SetMulti(on)
		n.tell()
	}
}

// SetSelectionNavigation sets what Escape does with several rows selected: reduce the selection to the
// focused row (the default, `list.selectionNavigation`), or clear it.
func (n *rowNav) SetSelectionNavigation(on bool) { n.selectionNavigation = on }

// Selected is the key of the node selected last, if one is selected; with several selected rows it is the
// one that was selected last.
func (n *rowNav) Selected() (string, bool) { p := n.sel.Primary(); return p, p != "" }

// SelectedKeys are the keys of the selected rows in the order of the rows; the keys of rows that are not
// shown (a node under a collapsed one) come first.
func (n *rowNav) SelectedKeys() []string { return n.sel.Sorted(n) }

// SelectionCount is the number of selected rows.
func (n *rowNav) SelectionCount() int { return n.sel.Len() }

// IsSelected says that the row of the key is selected.
func (n *rowNav) IsSelected(key string) bool { return n.sel.Has(key) }

// SetSelected selects a row the model has (shown or not) alone; it reports whether it could. It makes no
// callback: the widget's OnSelect is for the user's choices.
func (n *rowNav) SetSelected(key string) bool {
	if !n.known(key) {
		return false
	}
	if n.sel.Select(key) {
		n.tell()
	}
	return true
}

// SetSelection selects the rows of the keys the model has (all of them with MultiSelect, else the last). It
// makes no callback.
func (n *rowNav) SetSelection(keys []string) {
	var known []string
	for _, k := range keys {
		if n.known(k) {
			known = append(known, k)
		}
	}
	if n.sel.Set(known...) {
		n.tell()
	}
}

// ClearSelection selects nothing.
func (n *rowNav) ClearSelection() {
	if n.sel.Clear() {
		n.tell()
	}
}

// Anchor is the key that a range selection starts from.
func (n *rowNav) Anchor() (string, bool) { return n.sel.Anchor() }

func (n *rowNav) known(key string) bool {
	if n.has != nil {
		return n.has(key)
	}
	_, ok := n.index[key]
	return ok
}

// focusIndex focuses the row at an index, reveals it and makes it the anchor (the arrow keys do).
func (n *rowNav) focusIndex(i int) navResult {
	if i < 0 || i >= len(n.keys) {
		return navResult{handled: true}
	}
	n.SetFocus(n.keys[i])
	n.sel.SetAnchor(n.keys[i])
	return navResult{handled: true, scrollTo: n.revealOffset(i)}
}

func (n *rowNav) down() navResult {
	start := 0
	if f := n.FocusedIndex(); f >= 0 {
		start = f + 1
	}
	return n.focusIndex(n.nextFocusable(start))
}

func (n *rowNav) up() navResult {
	f := n.FocusedIndex()
	if f < 0 {
		return n.focusIndex(n.nextFocusable(0))
	}
	return n.focusIndex(n.previousFocusable(f - 1))
}

func (n *rowNav) first() navResult { return n.focusIndex(n.nextFocusable(0)) }

func (n *rowNav) last() navResult { return n.focusIndex(n.previousFocusable(len(n.keys) - 1)) }

// pageDown is `focusNextPage`: it focuses the last row that is completely in view, and when that is
// focused already scrolls a page (the viewport's height) and does it again.
func (n *rowNav) pageDown() navResult {
	res := navResult{handled: true}
	focused := n.FocusedIndex()
	for range len(n.keys) + 2 {
		last := n.indexAt(n.top + n.height)
		if last > 0 {
			last--
		}
		last = min(last, len(n.keys)-1)
		if last < 0 {
			return res
		}
		if focused != last && (focused < 0 || last > focused) {
			i := n.previousFocusable(last)
			if i < 0 || focused == i {
				i = last
			}
			n.SetFocus(n.keys[i])
			n.sel.SetAnchor(n.keys[i])
			return res
		}
		next := min(n.top+n.height, n.maxTop())
		if next == n.top {
			return res
		}
		n.top = next
		res.scrollTo = foundation.Some(next)
		focused = -1
	}
	return res
}

// pageUp is `focusPreviousPage`.
func (n *rowNav) pageUp() navResult {
	res := navResult{handled: true}
	focused := n.FocusedIndex()
	for range len(n.keys) + 2 {
		first := 0
		if n.top != 0 {
			first = min(n.indexAt(n.top-1)+1, len(n.keys))
		}
		first = min(first, len(n.keys)-1)
		if first < 0 {
			return res
		}
		if focused != first && (focused < 0 || focused >= first) {
			i := n.nextFocusable(first)
			if i < 0 || focused == i {
				i = first
			}
			n.SetFocus(n.keys[i])
			n.sel.SetAnchor(n.keys[i])
			return res
		}
		next := max(n.top-n.height, 0)
		if next == n.top {
			return res
		}
		n.top = next
		res.scrollTo = foundation.Some(next)
		focused = -1
	}
	return res
}

// expandSelection is Shift+Down (dir 1) and Shift+Up (dir -1): the focus moves one row and the selection
// follows (`list.expandSelectionDown`, `expandMultiSelection`). Without multi selection the key is not used.
func (n *rowNav) expandSelection(dir int) navResult {
	if !n.sel.Multi() {
		return navResult{}
	}
	previous := n.focus
	anchor, _ := n.sel.Anchor()
	var res navResult
	if dir > 0 {
		res = n.down()
	} else {
		res = n.up()
	}
	n.sel.SetAnchor(anchor) // only the focus moved
	res.selectionChanged = n.sel.ExpandTo(previous, n.focus)
	if res.selectionChanged {
		n.tell()
	}
	return res
}

// selectAll is Ctrl+A.
func (n *rowNav) selectAll() navResult {
	if !n.sel.Multi() || len(n.keys) == 0 {
		return navResult{}
	}
	changed := n.sel.SelectAll(n)
	if changed {
		n.tell()
	}
	return navResult{handled: true, selectionChanged: changed}
}

// toggleSelection is Ctrl+Space and Ctrl+Shift+Enter (`list.toggleSelection`): the focused row joins the
// selection or leaves it.
func (n *rowNav) toggleSelection() navResult {
	if !n.sel.Multi() || n.FocusedIndex() < 0 {
		return navResult{}
	}
	if n.sel.Has(n.focus) {
		n.sel.Remove(n.focus)
	} else {
		n.sel.Add(n.focus)
	}
	n.tell()
	return navResult{handled: true, selectionChanged: true, key: n.focus}
}

// selectFocused is Enter on a row: the focused row is selected alone and becomes the anchor
// (`selectElement`).
func (n *rowNav) selectFocused() (key string, changed, ok bool) {
	f := n.FocusedIndex()
	if f < 0 {
		return "", false, false
	}
	key = n.keys[f]
	changed = n.sel.Select(key)
	n.tell()
	return key, changed, true
}

// clear is Escape (`list.clear`): with several rows selected the selection is reduced to the focused row (or
// cleared, see SetSelectionNavigation); with one the selection is cleared; with none the focus is. Nothing
// is used when there is neither.
func (n *rowNav) clear() navResult {
	switch {
	case n.sel.Len() > 1:
		changed := false
		if n.selectionNavigation && n.FocusedIndex() >= 0 {
			changed = n.sel.Set(n.focus)
		} else {
			changed = n.sel.Clear()
		}
		n.sel.SetAnchor("")
		n.tell()
		return navResult{handled: true, selectionChanged: changed}
	case n.sel.Len() == 1:
		n.sel.Clear()
		n.sel.SetAnchor("")
		n.tell()
		return navResult{handled: true, selectionChanged: true}
	case n.focus != "":
		n.ClearFocus()
		n.sel.SetAnchor("")
		return navResult{handled: true}
	}
	return navResult{}
}

// press is a press of the primary button on a row, with the modifiers that were held: the row is focused,
// and the selection follows the rules of ListSelectionModel.Press.
func (n *rowNav) press(key string, mods ClickModifiers) navResult {
	i, ok := n.index[key]
	if !ok || n.isBlocked(i) {
		return navResult{}
	}
	changed := n.sel.Press(n, key, n.focus, mods)
	n.SetFocus(key)
	n.tell()
	return navResult{handled: true, selectionChanged: changed, key: key}
}

// Find is the state of the find: whether it is open, the pattern, the mode, the match type and the number
// of matches.
func (n *rowNav) Find() FindState { return n.find.state() }

// FindMatch gives the parts of the label of a row that the pattern matches, and whether the row matches.
// There are none while no pattern is in use.
func (n *rowNav) FindMatch(key string) ([]TextRange, bool) {
	if !n.find.active() {
		return nil, false
	}
	r, ok := n.find.matches[key]
	return r, ok
}

// OpenFind opens the find box (it keeps the pattern of the last time unless there is one) and gives it the
// keys. It is what typing a letter does.
func (n *rowNav) OpenFind() {
	if n.find.open && n.find.typing {
		return
	}
	n.find.open, n.find.typing = true, true
	n.rebuildOrTell()
}

// CloseFind closes the find box and forgets the pattern: all rows are shown again, the focus stays where it
// is. It reports whether a find was open.
func (n *rowNav) CloseFind() bool {
	if !n.find.open {
		return false
	}
	n.find.open, n.find.typing, n.find.pattern = false, false, ""
	n.find.reset()
	n.rebuildOrTell()
	return true
}

// SetFindTyping sets whether the keys go to the pattern (the box has the keyboard).
func (n *rowNav) SetFindTyping(on bool) {
	if n.find.open && n.find.typing != on {
		n.find.typing = on
		n.tell()
	}
}

func (n *rowNav) rebuildOrTell() {
	if n.rebuild != nil {
		n.rebuild()
		return
	}
	n.tell()
}

// SetFindPattern sets the pattern, opening the find box if it is closed. The rows that match are found, and
// the focus goes to the first match from the focused row on (wrapping), as VS Code's `applyPattern` does.
// The result is the scroll position that shows the focus, if the view has to move.
func (n *rowNav) SetFindPattern(pattern string) foundation.Opt[float64] {
	n.find.open = true
	if n.find.pattern == pattern {
		return foundation.None[float64]()
	}
	n.find.pattern = pattern
	n.rebuildOrTell()
	return n.focusFirstMatch()
}

// focusFirstMatch moves the focus to the first row that matches, from the focused row on and wrapping.
func (n *rowNav) focusFirstMatch() foundation.Opt[float64] {
	if !n.find.active() || len(n.find.matches) == 0 {
		return foundation.None[float64]()
	}
	start := max(n.FocusedIndex(), 0)
	for k := range len(n.keys) {
		i := (start + k) % len(n.keys)
		if n.focusable(i) && n.find.isMatch(n.keys[i]) {
			n.SetFocus(n.keys[i])
			return n.revealOffset(i)
		}
	}
	return foundation.None[float64]()
}

// SetFindMode sets what the pattern does to the rows (the filter toggle of the find box).
func (n *rowNav) SetFindMode(mode FindMode) {
	if n.find.mode == mode {
		return
	}
	n.find.mode = mode
	n.rebuildOrTell()
}

// SetFindMatchType sets how the pattern is matched (the fuzzy toggle of the find box).
func (n *rowNav) SetFindMatchType(t FindMatchType) {
	if n.find.matchType == t {
		return
	}
	n.find.matchType = t
	n.rebuildOrTell()
}
