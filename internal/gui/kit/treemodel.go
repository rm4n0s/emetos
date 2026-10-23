package kit

// Ported from VS Code: src/vs/base/browser/ui/tree/abstractTree.ts (the collapse state, the twistie rules and
// the active indent nodes), src/vs/base/browser/ui/list/listWidget.ts (focusNext, focusPrevious,
// focusNextPage, focusPreviousPage, reveal), src/vs/workbench/browser/actions/listCommands.ts (the commands
// the keys of a tree run: list.expand, list.collapse, list.select, list.toggleExpand, list.collapseAll,
// list.clear) and src/vs/base/browser/ui/tree/asyncDataTree.ts (children that are read when a node expands).

import (
	"cmp"
	"slices"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
)

// DefaultTreeRowHeight is the height of a row of a tree: VS Code's 22 px (`ITreeListOptions` rows are
// 22 px high in the explorer and in every other workbench tree).
const DefaultTreeRowHeight = 22.0

// treePlaceholderSuffix follows the key of a node in the key of the placeholder row under it. A NUL byte
// cannot be in a path, so the key is not one of a node.
const treePlaceholderSuffix = "\x00loading"

// PlaceholderKey is the key of the placeholder row that a node shows below it while its children are being
// read (TreeNode.Placeholder).
func PlaceholderKey(parent string) string { return parent + treePlaceholderSuffix }

// TreeItem is the data of a node that is given to a TreeModel. A node has a key, which names it for as long
// as it lives: the expansion state, the focus and the selection are kept by key when the items are given
// again, so a path is a good key. Keys are not empty and unique in the whole tree.
type TreeItem struct {
	// Key names the node.
	Key string
	// Data is the caller's value for the node (a file entry, a symbol); the row renderer receives it. It
	// is never compared.
	Data any
	// Children are the nodes below this one.
	Children []TreeItem
	// HasChildren makes the node expandable even though it has no Children: a folder that is empty (the
	// children are read and there are none). A node with Children is always expandable.
	HasChildren bool
	// Lazy says that the node has children that have not been read: it is expandable, and the first
	// time it is expanded the model asks its loader for them (TreeModelOpts.LoadChildren) and shows a
	// placeholder row below the node until SetChildren gives them. A node with Children is not lazy.
	Lazy bool
}

// TreeNode is a row of a TreeModel: a node that is shown, or the placeholder under a node that is being
// loaded.
type TreeNode struct {
	// Key is the key of the item; for a placeholder it is PlaceholderKey(Parent).
	Key string
	// Data is the item's Data (nil for a placeholder).
	Data any
	// Depth is how many nodes are above this one: 0 for the roots.
	Depth int
	// Parent is the key of the node above this one, empty for a root.
	Parent string
	// Expandable says that the node can be expanded: it has children, or has some that are not read.
	Expandable bool
	// Expanded says that the node is expanded (a node that cannot be expanded never is).
	Expanded bool
	// Loading says that the node is expanded and its children are being read.
	Loading bool
	// Placeholder marks the row shown below a node that is loading. It can be neither focused nor
	// selected.
	Placeholder bool
	// Index is the position of the row among the rows.
	Index int
}

// TreeModelOpts are the options of NewTreeModel.
type TreeModelOpts struct {
	// LoadChildren is called when a node that is Lazy is expanded for the first time (and when a
	// restored expansion state expands one that is shown), on the UI thread, after the model has
	// changed. It starts the reading, which may take as long as it wants and must not block the UI
	// thread; the answer goes to SetChildren (or FailLoad) on the UI thread. Without it a lazy node
	// stays loading until someone calls SetChildren.
	LoadChildren func(key string, data any)
	// RowHeight is the height of a row in pixels; the default is DefaultTreeRowHeight. It is what the
	// keyboard's page commands and the scrolling to a row count with.
	RowHeight foundation.Opt[float64]
}

// TreeAction is a key command of a tree, as VS Code runs it (`list.*` commands).
type TreeAction int

const (
	// TreeActionDown focuses the next row (`list.focusDown`).
	TreeActionDown TreeAction = iota
	// TreeActionUp focuses the previous row (`list.focusUp`).
	TreeActionUp
	// TreeActionFirst focuses the first row (`list.focusFirst`, Home).
	TreeActionFirst
	// TreeActionLast focuses the last row (`list.focusLast`, End).
	TreeActionLast
	// TreeActionPageDown focuses the last row in view, or scrolls a page when it is focused already
	// (`list.focusPageDown`).
	TreeActionPageDown
	// TreeActionPageUp focuses the first row in view, or scrolls a page (`list.focusPageUp`).
	TreeActionPageUp
	// TreeActionExpand expands the focused node, or focuses its first child when it is expanded
	// (`list.expand`, Right).
	TreeActionExpand
	// TreeActionCollapse collapses the focused node, or focuses its parent when it is collapsed or a leaf
	// (`list.collapse`, Left).
	TreeActionCollapse
	// TreeActionSelect toggles the focused node unless the model toggles only on a twistie click, and
	// selects it (`list.select`, Enter).
	TreeActionSelect
	// TreeActionToggle toggles the focused node, or selects it when it cannot be expanded
	// (`list.toggleExpand`, Space).
	TreeActionToggle
	// TreeActionCollapseAll collapses every node (`list.collapseAll`, Ctrl+Left).
	TreeActionCollapseAll
	// TreeActionClear clears the selection, or the focus when nothing is selected (`list.clear`,
	// Escape).
	TreeActionClear
)

// TreeResult is what an action or a click did beyond changing the model.
type TreeResult struct {
	// Handled says that the key or click was used. A key that is not handled goes on to the widgets
	// above (Right on a leaf, Escape with nothing to clear).
	Handled bool
	// Select says that the user selected Node (which the model has selected).
	Select bool
	// Open says that the user asked to open Node: Enter, or a double click.
	Open bool
	// Node is the node that Select and Open are about.
	Node TreeNode
	// ScrollTo is the scroll position (the distance of the top of the content from the top of the view,
	// in pixels) that shows the row that was focused, or the page that was asked for, when it is not
	// where the view is now.
	ScrollTo foundation.Opt[float64]
}

// treeEntry is a node of the model, shown or not.
type treeEntry struct {
	key         string
	data        any
	hasChildren bool
	lazy        bool
	parent      *treeEntry
	children    []*treeEntry
	depth       int
}

func (e *treeEntry) expandable() bool { return e.lazy || e.hasChildren || len(e.children) > 0 }

// TreeModel is the state of a tree: its nodes, the rows that are shown (the nodes below the expanded
// ones, in order), which nodes are expanded, the focused row and the selected node, and the keyboard.
// It is what the Tree widget shows and moves, and it is tested without a window. It is used on the UI
// thread only. It tells its listeners when anything changed.
//
// The model keeps the expansion state by key, for keys the tree does not have as well: giving the items
// again (SetItems) after a folder was read again, or restoring the state of an earlier session
// (SetExpandedKeys), finds every node that is back expanded. Nodes whose children are not read are Lazy:
// they show a chevron, and expanding one asks the loader for the children.
type TreeModel struct {
	foundation.ChangeNotifier
	opts TreeModelOpts

	roots   []*treeEntry
	entries map[string]*treeEntry
	// expanded is the expansion state by key.
	expanded map[string]bool
	// requested are the lazy nodes whose children were asked for and have not come.
	requested map[string]bool
	// focusTrail are the keys above the focused node, nearest first, taken before a change that may remove
	// it: where the focus goes if it does.
	focusTrail []string
	rows       []TreeNode
	index      map[string]int

	focus, selected string
	toggleOnSelect  bool

	// top and height are the scroll position and the height of the view, as the widget last told.
	top, height float64

	batch int
	dirty bool
	loads []string
}

// NewTreeModel makes an empty model.
func NewTreeModel(opts ...TreeModelOpts) *TreeModel {
	m := &TreeModel{
		entries: map[string]*treeEntry{}, expanded: map[string]bool{}, requested: map[string]bool{},
		index: map[string]int{}, toggleOnSelect: true,
	}
	if len(opts) > 0 {
		m.opts = opts[0]
	}
	return m
}

// RowHeight is the height of a row in pixels.
func (m *TreeModel) RowHeight() float64 {
	return max(m.opts.RowHeight.OrElse(DefaultTreeRowHeight), 1)
}

// SetToggleOnSelect sets whether Enter, and a click on a row, toggle an expandable node besides selecting
// it (the default). Turned off, only a click on the twistie and Space toggle (VS Code's
// `expandOnlyOnTwistieClick`), and a double click does too.
func (m *TreeModel) SetToggleOnSelect(on bool) { m.toggleOnSelect = on }

// ToggleOnSelect is whether Enter and a click toggle an expandable node.
func (m *TreeModel) ToggleOnSelect() bool { return m.toggleOnSelect }

// begin and end bracket a change: the rows are made again and the listeners called once, when the
// outermost one ends, and the loads that the change asked for are started then.
func (m *TreeModel) begin() { m.batch++ }

func (m *TreeModel) end() {
	m.batch--
	if m.batch > 0 {
		return
	}
	if m.settle() {
		m.NotifyListeners()
	}
}

// settle makes the rows again and starts the loads until nothing is left to do, and reports whether the
// rows changed. What a loader changes at once (SetChildren) is part of the same round.
func (m *TreeModel) settle() (changed bool) {
	m.batch++ // what the loaders change waits for the loop
	defer func() { m.batch-- }()
	for m.dirty || len(m.loads) > 0 {
		if m.dirty {
			m.dirty = false
			m.flatten()
			m.fixup()
			changed = true
		}
		loads := m.loads
		m.loads = nil
		for _, key := range loads {
			e := m.entries[key]
			if e == nil || !e.lazy || !m.requested[key] || m.opts.LoadChildren == nil {
				continue
			}
			m.opts.LoadChildren(key, e.data)
		}
	}
	return changed
}

// changed marks the rows as out of date.
func (m *TreeModel) changed() { m.dirty = true }

// flatten makes the rows from the nodes that are expanded.
func (m *TreeModel) flatten() {
	m.rows = m.rows[:0]
	clear(m.index)
	m.walk(m.roots)
}

func (m *TreeModel) walk(entries []*treeEntry) {
	for _, e := range entries {
		expanded := m.expanded[e.key] && e.expandable()
		row := TreeNode{
			Key: e.key, Data: e.data, Depth: e.depth, Expandable: e.expandable(), Expanded: expanded,
			Loading: expanded && e.lazy, Index: len(m.rows),
		}
		if e.parent != nil {
			row.Parent = e.parent.key
		}
		m.index[e.key] = row.Index
		m.rows = append(m.rows, row)
		if !expanded {
			continue
		}
		if e.lazy {
			p := TreeNode{Key: PlaceholderKey(e.key), Depth: e.depth + 1, Parent: e.key, Placeholder: true, Index: len(m.rows)}
			m.index[p.Key] = p.Index
			m.rows = append(m.rows, p)
			continue
		}
		m.walk(e.children)
	}
}

// fixup repairs the focus and the selection after the rows changed: a selection of a node that is gone
// is dropped, and the focus of a row that is not shown goes to the nearest node above it that is shown.
func (m *TreeModel) fixup() {
	if m.selected != "" && m.entries[m.selected] == nil {
		m.selected = ""
	}
	trail := m.focusTrail
	m.focusTrail = nil
	if m.focus == "" {
		return
	}
	if _, ok := m.index[m.focus]; ok {
		return
	}
	// the nearest node above that is shown: through the entries when the node is still known, else
	// through the trail taken before it went
	if e := m.entries[m.focus]; e != nil {
		trail = trail[:0]
		for p := e.parent; p != nil; p = p.parent {
			trail = append(trail, p.key)
		}
	}
	m.focus = ""
	for _, key := range trail {
		if _, ok := m.index[key]; ok {
			m.focus = key
			return
		}
	}
}

// rememberFocusTrail records the keys above the focused node, for fixup.
func (m *TreeModel) rememberFocusTrail() {
	m.focusTrail = m.focusTrail[:0]
	if e := m.entries[m.focus]; e != nil {
		for p := e.parent; p != nil; p = p.parent {
			m.focusTrail = append(m.focusTrail, p.key)
		}
	}
}

// checkItems validates the keys of items against each other and against the keys of the nodes that stay.
func checkItems(items []TreeItem, seen map[string]bool, taken func(string) bool) error {
	for _, it := range items {
		if it.Key == "" {
			return errors.New("TreeItemKeyEmpty", "a tree item has no key")
		}
		if seen[it.Key] || taken(it.Key) {
			return errors.New("TreeItemKeyDuplicated", "two tree items have the same key", "key", it.Key)
		}
		seen[it.Key] = true
		if err := checkItems(it.Children, seen, taken); err != nil {
			return err
		}
	}
	return nil
}

// build makes the entries of items below a parent and records them in the entry map.
func (m *TreeModel) build(items []TreeItem, parent *treeEntry, depth int) []*treeEntry {
	out := make([]*treeEntry, len(items))
	for i, it := range items {
		e := &treeEntry{key: it.Key, data: it.Data, hasChildren: it.HasChildren, lazy: it.Lazy && len(it.Children) == 0, parent: parent, depth: depth}
		m.entries[e.key] = e
		e.children = m.build(it.Children, e, depth+1)
		out[i] = e
	}
	return out
}

// drop removes the entries of a subtree from the entry map and from the lazy bookkeeping.
func (m *TreeModel) drop(entries []*treeEntry) {
	for _, e := range entries {
		delete(m.entries, e.key)
		delete(m.requested, e.key)
		m.drop(e.children)
	}
}

// SetItems replaces all the nodes. The expansion state is kept by key, and so are the focus and the
// selection when their nodes are still there; the lazy nodes that are expanded and shown are loaded
// again unless they are being loaded. An item without a key, or two with the same key, are refused
// (TreeItemKeyEmpty, TreeItemKeyDuplicated) and nothing changes.
func (m *TreeModel) SetItems(items []TreeItem) error {
	if err := checkItems(items, map[string]bool{}, func(string) bool { return false }); err != nil {
		return err
	}
	m.begin()
	defer m.end()
	m.rememberFocusTrail()
	loading := m.requested
	m.requested = map[string]bool{}
	m.drop(m.roots)
	m.roots = m.build(items, nil, 0)
	for key := range loading {
		if e := m.entries[key]; e != nil && e.lazy {
			m.requested[key] = true // still being read
		}
	}
	m.changed()
	m.requestShown(m.roots)
	return nil
}

// requestShown asks for the children of the lazy nodes that are expanded and shown.
func (m *TreeModel) requestShown(entries []*treeEntry) {
	for _, e := range entries {
		if !m.expanded[e.key] || !e.expandable() {
			continue
		}
		if e.lazy {
			m.requestLoad(e)
			continue
		}
		m.requestShown(e.children)
	}
}

func (m *TreeModel) requestLoad(e *treeEntry) {
	if !e.lazy || m.requested[e.key] {
		return
	}
	m.requested[e.key] = true
	m.loads = append(m.loads, e.key)
}

// SetChildren gives the children of a node: the answer to the loader of a lazy node, or the new content
// of a node that was read before. The node is not lazy any more and is expandable even if there are no
// children. A key the model does not have (the node went away while its children were being read) is
// ignored. The expansion state of the children that are kept is kept. The rules for the keys are those of
// SetItems, and the keys of the old children may be used again.
func (m *TreeModel) SetChildren(key string, children []TreeItem) error {
	e := m.entries[key]
	if e == nil {
		return nil
	}
	leaving := map[string]bool{}
	var collect func(es []*treeEntry)
	collect = func(es []*treeEntry) {
		for _, c := range es {
			leaving[c.key] = true
			collect(c.children)
		}
	}
	collect(e.children)
	if err := checkItems(children, map[string]bool{}, func(k string) bool { return m.entries[k] != nil && !leaving[k] }); err != nil {
		return err
	}
	m.begin()
	defer m.end()
	m.rememberFocusTrail()
	m.drop(e.children)
	e.children = m.build(children, e, e.depth+1)
	e.hasChildren = e.hasChildren || e.lazy || len(children) > 0
	e.lazy = false
	delete(m.requested, key)
	m.changed()
	m.requestShown(e.children)
	return nil
}

// FailLoad ends the reading of the children of a node that failed: the node collapses and stays lazy, so
// that expanding it again tries again.
func (m *TreeModel) FailLoad(key string) {
	e := m.entries[key]
	if e == nil || !e.lazy {
		return
	}
	m.begin()
	defer m.end()
	delete(m.requested, key)
	delete(m.expanded, key)
	m.changed()
}

// Len is the number of rows.
func (m *TreeModel) Len() int { return len(m.rows) }

// Rows are the rows, in order. The slice is the model's: do not change it, and do not keep it across a
// change of the model.
func (m *TreeModel) Rows() []TreeNode { return m.rows }

// Row is the row at an index; the zero node when there is none.
func (m *TreeModel) Row(i int) TreeNode {
	if i < 0 || i >= len(m.rows) {
		return TreeNode{Index: -1}
	}
	return m.rows[i]
}

// IndexOf is the index of the row of a key, or -1 when the node is not shown.
func (m *TreeModel) IndexOf(key string) int {
	if i, ok := m.index[key]; ok {
		return i
	}
	return -1
}

// Node is the row of a key, when it is shown.
func (m *TreeModel) Node(key string) (TreeNode, bool) {
	if i, ok := m.index[key]; ok {
		return m.rows[i], true
	}
	return TreeNode{Index: -1}, false
}

// Has says that the model has a node with the key, shown or not.
func (m *TreeModel) Has(key string) bool { return m.entries[key] != nil }

// Data is the Data of the node with the key, shown or not.
func (m *TreeModel) Data(key string) (any, bool) {
	if e := m.entries[key]; e != nil {
		return e.data, true
	}
	return nil, false
}

// Parent is the key of the node above a node; false for a root or a node the model does not have.
func (m *TreeModel) Parent(key string) (string, bool) {
	if e := m.entries[key]; e != nil && e.parent != nil {
		return e.parent.key, true
	}
	return "", false
}

// Children are the keys of the children of a node that are read, shown or not.
func (m *TreeModel) Children(key string) []string {
	e := m.entries[key]
	if e == nil {
		return nil
	}
	out := make([]string, len(e.children))
	for i, c := range e.children {
		out[i] = c.key
	}
	return out
}

// Ancestors are the keys of the nodes above a node, the outermost first.
func (m *TreeModel) Ancestors(key string) []string {
	e := m.entries[key]
	if e == nil {
		return nil
	}
	var out []string
	for p := e.parent; p != nil; p = p.parent {
		out = append(out, p.key)
	}
	slices.Reverse(out)
	return out
}

// ActiveGuides are the keys of the nodes whose indent guide is the active one: for the focused row and
// the selected node, the node itself when it is expanded and has children, else its parent
// (`_onDidChangeActiveNodes`). The guide that runs through the children of such a node is drawn in
// `tree.indentGuidesStroke`.
func (m *TreeModel) ActiveGuides() map[string]bool {
	out := map[string]bool{}
	for _, key := range []string{m.focus, m.selected} {
		e := m.entries[key]
		if key == "" || e == nil {
			continue
		}
		switch {
		case m.expanded[key] && len(e.children) > 0:
			out[key] = true
		case e.parent != nil:
			out[e.parent.key] = true
		}
	}
	return out
}

// IsExpanded says that the node is expanded (shown or not: a node below a collapsed one can be).
func (m *TreeModel) IsExpanded(key string) bool {
	e := m.entries[key]
	return e != nil && e.expandable() && m.expanded[key]
}

// Expand expands a node, asking for its children when they are not read. It reports whether anything
// changed: a node that cannot be expanded, or is expanded, is left alone.
func (m *TreeModel) Expand(key string) bool {
	e := m.entries[key]
	if e == nil || !e.expandable() || m.expanded[key] {
		return false
	}
	m.begin()
	defer m.end()
	m.expanded[key] = true
	m.requestLoad(e)
	m.changed()
	return true
}

// Collapse collapses a node; it reports whether anything changed. A focus below the node moves to it.
func (m *TreeModel) Collapse(key string) bool {
	e := m.entries[key]
	if e == nil || !e.expandable() || !m.expanded[key] {
		return false
	}
	m.begin()
	defer m.end()
	delete(m.expanded, key)
	m.changed()
	return true
}

// Toggle expands a collapsed node and collapses an expanded one; with recursive, every node below it
// that is read gets the new state too (Alt+click). It reports whether anything changed.
func (m *TreeModel) Toggle(key string, recursive bool) bool {
	e := m.entries[key]
	if e == nil || !e.expandable() {
		return false
	}
	expand := !m.expanded[key]
	m.begin()
	defer m.end()
	m.set(e, expand, recursive)
	return true
}

func (m *TreeModel) set(e *treeEntry, expand, recursive bool) {
	if e.expandable() {
		if expand && !m.expanded[e.key] {
			m.expanded[e.key] = true
			m.requestLoad(e)
		} else if !expand {
			delete(m.expanded, e.key)
		}
	}
	m.changed()
	if recursive {
		for _, c := range e.children {
			m.set(c, expand, true)
		}
	}
}

// ExpandAll expands every node the model has read; the lazy ones are loaded, and their children come
// collapsed.
func (m *TreeModel) ExpandAll() {
	m.begin()
	defer m.end()
	for _, e := range m.roots {
		m.set(e, true, true)
	}
}

// CollapseAll collapses every node. A focus that is hidden moves to the root above it.
func (m *TreeModel) CollapseAll() {
	m.begin()
	defer m.end()
	clear(m.expanded)
	m.changed()
}

// ExpandedKeys are the keys of the nodes that are expanded, sorted: the state to keep between sessions.
func (m *TreeModel) ExpandedKeys() []string {
	out := make([]string, 0, len(m.expanded))
	for k := range m.expanded {
		out = append(out, k)
	}
	slices.SortFunc(out, cmp.Compare[string])
	return out
}

// SetExpandedKeys replaces the expansion state with the keys; the nodes the model has are shown
// expanded (and loaded when lazy), the others are expanded when they turn up.
func (m *TreeModel) SetExpandedKeys(keys []string) {
	m.begin()
	defer m.end()
	clear(m.expanded)
	for _, k := range keys {
		m.expanded[k] = true
	}
	m.changed()
	m.requestShown(m.roots)
}

// Reveal expands the nodes above a node so that it is shown, and gives its index; false when the model
// does not have the node (a node deeper than the folders that are read: read them one after the other,
// calling Reveal again when their children came).
func (m *TreeModel) Reveal(key string) (int, bool) {
	e := m.entries[key]
	if e == nil {
		return -1, false
	}
	m.begin()
	for p := e.parent; p != nil; p = p.parent {
		if !m.expanded[p.key] {
			m.expanded[p.key] = true
			m.requestLoad(p)
			m.changed()
		}
	}
	m.end()
	i, ok := m.index[key]
	if !ok {
		return -1, false
	}
	return i, true
}

// Focused is the key of the focused row, if there is one.
func (m *TreeModel) Focused() (string, bool) { return m.focus, m.focus != "" }

// FocusedIndex is the index of the focused row, or -1.
func (m *TreeModel) FocusedIndex() int {
	if i, ok := m.index[m.focus]; ok && m.focus != "" {
		return i
	}
	return -1
}

// SetFocus focuses a row that is shown; it reports whether it could (a placeholder cannot be).
func (m *TreeModel) SetFocus(key string) bool {
	i, ok := m.index[key]
	if !ok || m.rows[i].Placeholder {
		return false
	}
	if m.focus != key {
		m.focus = key
		m.notify()
	}
	return true
}

// ClearFocus takes the focus off the rows.
func (m *TreeModel) ClearFocus() {
	if m.focus != "" {
		m.focus = ""
		m.notify()
	}
}

// Selected is the key of the selected node, if there is one.
func (m *TreeModel) Selected() (string, bool) { return m.selected, m.selected != "" }

// SetSelected selects a node the model has (shown or not); it reports whether it could. It makes no
// callback: the widget's OnSelect is for the user's choices.
func (m *TreeModel) SetSelected(key string) bool {
	if m.entries[key] == nil {
		return false
	}
	if m.selected != key {
		m.selected = key
		m.notify()
	}
	return true
}

// ClearSelection selects nothing.
func (m *TreeModel) ClearSelection() {
	if m.selected != "" {
		m.selected = ""
		m.notify()
	}
}

// notify tells the listeners of a change that needs no new rows (the focus, the selection).
func (m *TreeModel) notify() {
	if m.batch > 0 {
		m.dirty = true // the end of the batch tells them
		return
	}
	m.NotifyListeners()
}

// EnsureFocus gives the focus to a row when none has it, as a tree that takes the keyboard focus does:
// the selected row when it is shown, else the first row that can be focused.
func (m *TreeModel) EnsureFocus() {
	if m.FocusedIndex() >= 0 {
		return
	}
	if _, ok := m.index[m.selected]; ok && m.selected != "" {
		m.SetFocus(m.selected)
		return
	}
	if i := m.nextFocusable(0); i >= 0 {
		m.SetFocus(m.rows[i].Key)
	}
}

// SetViewport tells the model where the view is: how far the content is scrolled and how high the view
// is, in pixels. The keyboard's page commands and the scrolling to the focused row use it.
func (m *TreeModel) SetViewport(top, height float64) { m.top, m.height = max(top, 0), max(height, 0) }

// RowsInView is how many rows fit in the view completely (at least 1).
func (m *TreeModel) RowsInView() int { return max(int(m.height/m.RowHeight()), 1) }

// nextFocusable is the index of the first row from i on that can be focused, or -1.
func (m *TreeModel) nextFocusable(i int) int {
	for ; i < len(m.rows); i++ {
		if !m.rows[i].Placeholder {
			return i
		}
	}
	return -1
}

// previousFocusable is the index of the last row from i down that can be focused, or -1.
func (m *TreeModel) previousFocusable(i int) int {
	for i = min(i, len(m.rows)-1); i >= 0; i-- {
		if !m.rows[i].Placeholder {
			return i
		}
	}
	return -1
}

// maxTop is the farthest the content scrolls.
func (m *TreeModel) maxTop() float64 {
	return max(float64(len(m.rows))*m.RowHeight()-m.height, 0)
}

// revealOffset is the scroll position that shows a row with the least scrolling (`ListView.reveal`):
// none when the row is in view. It records the new position as the view's.
func (m *TreeModel) revealOffset(index int) foundation.Opt[float64] {
	if index < 0 || m.height <= 0 {
		return foundation.None[float64]()
	}
	h := m.RowHeight()
	top, bottom := float64(index)*h, float64(index+1)*h
	target := m.top
	switch {
	case top < m.top:
		target = top
	case bottom > m.top+m.height:
		target = bottom - m.height
	}
	target = min(max(target, 0), m.maxTop())
	if target == m.top {
		return foundation.None[float64]()
	}
	m.top = target
	return foundation.Some(target)
}

// focusIndex focuses the row at an index and reveals it.
func (m *TreeModel) focusIndex(i int) TreeResult {
	if i < 0 || i >= len(m.rows) {
		return TreeResult{Handled: true}
	}
	m.SetFocus(m.rows[i].Key)
	return TreeResult{Handled: true, ScrollTo: m.revealOffset(i)}
}

// indexAt is the index of the row at a position of the content; the number of rows past the end.
func (m *TreeModel) indexAt(y float64) int {
	if y < 0 {
		return -1
	}
	return min(int(y/m.RowHeight()), len(m.rows))
}

// pageDown is `focusNextPage`: it focuses the last row that is completely in view, and when that is
// focused already scrolls a page (the viewport's height) and does it again.
func (m *TreeModel) pageDown() TreeResult {
	var res TreeResult
	res.Handled = true
	focused := m.FocusedIndex()
	for range len(m.rows) + 2 {
		last := m.indexAt(m.top + m.height)
		if last > 0 {
			last--
		}
		last = min(last, len(m.rows)-1)
		if last < 0 {
			return res
		}
		if focused != last && (focused < 0 || last > focused) {
			i := m.previousFocusable(last)
			if i < 0 || focused == i {
				i = last
			}
			m.SetFocus(m.rows[i].Key)
			return res
		}
		next := min(m.top+m.height, m.maxTop())
		if next == m.top {
			return res
		}
		m.top = next
		res.ScrollTo = foundation.Some(next)
		focused = -1
	}
	return res
}

// pageUp is `focusPreviousPage`.
func (m *TreeModel) pageUp() TreeResult {
	var res TreeResult
	res.Handled = true
	focused := m.FocusedIndex()
	for range len(m.rows) + 2 {
		first := 0
		if m.top != 0 {
			first = min(m.indexAt(m.top-1)+1, len(m.rows))
		}
		first = min(first, len(m.rows)-1)
		if first < 0 {
			return res
		}
		if focused != first && (focused < 0 || focused >= first) {
			i := m.nextFocusable(first)
			if i < 0 || focused == i {
				i = first
			}
			m.SetFocus(m.rows[i].Key)
			return res
		}
		next := max(m.top-m.height, 0)
		if next == m.top {
			return res
		}
		m.top = next
		res.ScrollTo = foundation.Some(next)
		focused = -1
	}
	return res
}

// Perform runs a key command of the tree. The scroll position that results, if it is not where the
// view is, is in the result; the widget moves the view there.
func (m *TreeModel) Perform(a TreeAction) TreeResult {
	if len(m.rows) == 0 && a != TreeActionClear {
		return TreeResult{}
	}
	focused := m.FocusedIndex()
	switch a {
	case TreeActionDown:
		start := 0
		if focused >= 0 {
			start = focused + 1
		}
		return m.focusIndex(m.nextFocusable(start))
	case TreeActionUp:
		if focused < 0 {
			return m.focusIndex(m.nextFocusable(0))
		}
		return m.focusIndex(m.previousFocusable(focused - 1))
	case TreeActionFirst:
		return m.focusIndex(m.nextFocusable(0))
	case TreeActionLast:
		return m.focusIndex(m.previousFocusable(len(m.rows) - 1))
	case TreeActionPageDown:
		return m.pageDown()
	case TreeActionPageUp:
		return m.pageUp()
	case TreeActionExpand:
		return m.expandKey(focused)
	case TreeActionCollapse:
		return m.collapseKey(focused)
	case TreeActionSelect:
		if focused < 0 {
			return TreeResult{}
		}
		n := m.rows[focused]
		if n.Expandable && m.toggleOnSelect {
			m.Toggle(n.Key, false)
		}
		m.SetSelected(n.Key)
		return TreeResult{Handled: true, Select: true, Open: true, Node: m.nodeOf(n.Key)}
	case TreeActionToggle:
		if focused < 0 {
			return TreeResult{}
		}
		n := m.rows[focused]
		if n.Expandable {
			m.Toggle(n.Key, false)
			return TreeResult{Handled: true}
		}
		m.SetSelected(n.Key)
		return TreeResult{Handled: true, Select: true, Node: n}
	case TreeActionCollapseAll:
		m.CollapseAll()
		res := TreeResult{Handled: true}
		if i := m.FocusedIndex(); i >= 0 {
			res.ScrollTo = m.revealOffset(i)
		}
		return res
	case TreeActionClear:
		switch {
		case m.selected != "":
			m.ClearSelection()
		case m.focus != "":
			m.ClearFocus()
		default:
			return TreeResult{}
		}
		return TreeResult{Handled: true}
	}
	return TreeResult{}
}

// nodeOf is the row of a key as it is now (the node the user acted on, after the action changed it), or
// the node of a key that is not shown.
func (m *TreeModel) nodeOf(key string) TreeNode {
	if n, ok := m.Node(key); ok {
		return n
	}
	if e := m.entries[key]; e != nil {
		return TreeNode{Key: key, Data: e.data, Depth: e.depth, Expandable: e.expandable(), Index: -1}
	}
	return TreeNode{Key: key, Index: -1}
}

// expandKey is `list.expand`: the key is used when the focused node can be expanded or has a child.
func (m *TreeModel) expandKey(focused int) TreeResult {
	if focused < 0 {
		return TreeResult{}
	}
	n := m.rows[focused]
	if !n.Expandable {
		return TreeResult{}
	}
	if m.Expand(n.Key) {
		return TreeResult{Handled: true}
	}
	// expanded: the first child, unless it is the placeholder of a node that is loading
	if focused+1 < len(m.rows) && m.rows[focused+1].Parent == n.Key && !m.rows[focused+1].Placeholder {
		return m.focusIndex(focused + 1)
	}
	return TreeResult{Handled: true}
}

// collapseKey is `list.collapse`: the key is used when the focused node is expanded or has a parent.
func (m *TreeModel) collapseKey(focused int) TreeResult {
	if focused < 0 {
		return TreeResult{}
	}
	n := m.rows[focused]
	if m.Collapse(n.Key) {
		return TreeResult{Handled: true}
	}
	if n.Parent == "" {
		return TreeResult{}
	}
	if i, ok := m.index[n.Parent]; ok {
		return m.focusIndex(i)
	}
	return TreeResult{Handled: true}
}

// Click is a press of the primary button on the row of a key, as VS Code's `TreeNodeListMouseController`
// treats it: the row is focused; on the twistie of an expandable node the node is toggled and that is
// all; elsewhere on an expandable node it is toggled too unless ToggleOnSelect is off, and the node is
// selected. recursive (Alt) toggles the whole subtree.
func (m *TreeModel) Click(key string, onTwistie, recursive bool) TreeResult {
	i, ok := m.index[key]
	if !ok || m.rows[i].Placeholder {
		return TreeResult{}
	}
	m.begin()
	n := m.rows[i]
	m.SetFocus(key)
	if n.Expandable && (onTwistie || m.toggleOnSelect) {
		m.Toggle(key, recursive)
	}
	selected := !onTwistie || !n.Expandable
	if selected {
		m.SetSelected(key)
	}
	m.end()
	return TreeResult{Handled: true, Select: selected, Node: m.nodeOf(key)}
}

// DoubleClick is the second press of a double click on a row: the node is opened, and an expandable node
// is toggled when a single click does not toggle it (`expandOnDoubleClick`). A double click with the
// toggling click would toggle twice, which is left out.
func (m *TreeModel) DoubleClick(key string) TreeResult {
	i, ok := m.index[key]
	if !ok || m.rows[i].Placeholder {
		return TreeResult{}
	}
	m.begin()
	n := m.rows[i]
	m.SetFocus(key)
	if n.Expandable && !m.toggleOnSelect {
		m.Toggle(key, false)
	}
	m.end()
	return TreeResult{Handled: true, Open: true, Node: m.nodeOf(key)}
}
