package editorgroup

import (
	"math"
	"slices"

	"github.com/rm4n0s/errors"
)

// Orientation is the axis along which the children of a split lie.
type Orientation int

const (
	// Horizontal puts the children side by side (VS Code's GroupOrientation.HORIZONTAL, 0): the
	// sashes between them are vertical lines.
	Horizontal Orientation = iota
	// Vertical stacks the children from top to bottom (GroupOrientation.VERTICAL, 1).
	Vertical
)

func (o Orientation) orthogonal() Orientation {
	if o == Horizontal {
		return Vertical
	}
	return Horizontal
}

// Direction is a side of a group (VS Code's GroupDirection).
type Direction int

const (
	// DirUp is the side above a group.
	DirUp Direction = iota
	// DirDown is the side below a group.
	DirDown
	// DirLeft is the side left of a group.
	DirLeft
	// DirRight is the side right of a group.
	DirRight
)

// Orientation is the axis a direction lies on: left and right are horizontal.
func (d Direction) Orientation() Orientation {
	if d == DirUp || d == DirDown {
		return Vertical
	}
	return Horizontal
}

// after tells whether the direction is the end side of its axis (right, down).
func (d Direction) after() bool { return d == DirRight || d == DirDown }

// Location is a place in the order the groups appear in the grid (VS Code's GroupLocation).
type Location int

const (
	// LocationFirst is the first group of the grid.
	LocationFirst Location = iota
	// LocationLast is the last group of the grid.
	LocationLast
	// LocationNext is the group after the source group.
	LocationNext
	// LocationPrevious is the group before the source group.
	LocationPrevious
)

// GroupsOrder is the order Groups lists the groups in (VS Code's GroupsOrder).
type GroupsOrder int

const (
	// CreationTime lists the groups in the order they were made.
	CreationTime GroupsOrder = iota
	// MostRecentlyActiveGroups lists the active group first, then the one that was active before it,
	// and so on; groups that were never active come last, in creation order.
	MostRecentlyActiveGroups
	// GridAppearance lists the groups as they appear in the grid: depth first, left to right and top
	// to bottom. The numbers of "focus group 1..8" and of dev.openFile's group count in this order.
	GridAppearance
)

// SplitSizing is `workbench.editor.splitSizing`: how the groups are sized when a group is added or
// removed.
type SplitSizing string

const (
	// SizingAuto splits the group to equal parts, unless all the groups of the split are of equal
	// size already; then all are made equal.
	SizingAuto SplitSizing = "auto"
	// SizingDistribute makes all the groups of the split equal.
	SizingDistribute SplitSizing = "distribute"
	// SizingSplit splits the group that the new one is added next to to equal parts.
	SizingSplit SplitSizing = "split"
)

// GridOptions are the settings a Grid follows.
type GridOptions struct {
	// Model are the options of the group models.
	Model Options
	// CloseEmptyGroups is `workbench.editor.closeEmptyGroups`: a group whose last editor closes
	// closes too, unless it is the only group.
	CloseEmptyGroups bool
	// SplitSizing is `workbench.editor.splitSizing`; empty is SizingAuto.
	SplitSizing SplitSizing
}

// DefaultGridOptions are VS Code's defaults of the settings the grid follows.
func DefaultGridOptions() GridOptions {
	return GridOptions{Model: DefaultOptions(), CloseEmptyGroups: true, SplitSizing: SizingAuto}
}

// Group is a leaf of the grid: the editors of one group (SPEC §10.3).
type Group struct {
	// Model holds the editors of the group.
	Model *Model
	node  *node
}

// ID is the id of the group; it is never used again by another group of the grid.
func (g *Group) ID() int { return g.Model.ID() }

// Empty tells whether the group has no editor.
func (g *Group) Empty() bool { return g.Model.Count() == 0 }

// node is a node of the tree of splits: a branch holding children that lie along orient, or a leaf
// holding a group. size is the share of the extent of its parent along the parent's axis; the
// sizes of the children of a branch add up to 1.
type node struct {
	parent   *node
	orient   Orientation
	children []*node
	group    *Group
	size     float64
}

func (n *node) leaf() bool { return n.group != nil }

func (n *node) indexInParent() int {
	if n.parent == nil {
		return -1
	}
	return slices.Index(n.parent.children, n)
}

// GridChangeKind says what changed in a Grid.
type GridChangeKind int

const (
	// GroupAdded tells that a group was added; Group is it.
	GroupAdded GridChangeKind = iota
	// GroupRemoved tells that a group was removed; Group is it, no longer in the grid.
	GroupRemoved
	// GroupMoved tells that a group moved in the tree; Group is it.
	GroupMoved
	// ActiveGroupChanged tells that another group is the active group; Group is it.
	ActiveGroupChanged
	// GroupEditorsChanged forwards a change of the model of a group; Group and Change say which.
	GroupEditorsChanged
	// LayoutChanged tells that the tree, the sizes, the orientation or the maximized group changed.
	LayoutChanged
)

// GridEvent tells that a Grid changed.
type GridEvent struct {
	Kind  GridChangeKind
	Group *Group
	// Change is the change of the model, for GroupEditorsChanged.
	Change ChangeEvent
}

type gridListener struct{ fn func(GridEvent) }

// Grid is the editor groups of a window as data (SPEC §10.3): a tree of splits whose leaves are
// groups, the active group and the most-recently-used order of the groups. It is a port of the
// behavior of VS Code's Grid, GridView and EditorPart (the sizes are shares of the extent of the
// split instead of pixels). It is used on the UI thread only.
//
// Not ported: groups in auxiliary windows, locked groups, the centered layout, and sizes in
// pixels (the view that shows the grid keeps the least sizes).
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/editorPart.ts,
// src/vs/base/browser/ui/grid/grid.ts and src/vs/base/browser/ui/grid/gridview.ts
type Grid struct {
	opts      GridOptions
	root      *node
	groups    map[int]*Group
	created   []*Group
	nextID    int
	active    *Group
	mru       []int
	maximized *Group
	listeners []*gridListener
	version   int
	stops     map[*Group]func()
	disposed  bool
}

// NewGrid makes a grid of one empty group, which is the active group.
func NewGrid(opts GridOptions) *Grid {
	g := newBareGrid(opts)
	grp := g.newGroup(0)
	g.root = &node{orient: Horizontal, size: 1}
	attach(g.root, grp, 1)
	g.active = grp
	g.mru = []int{grp.ID()}
	return g
}

func newBareGrid(opts GridOptions) *Grid {
	if opts.SplitSizing == "" {
		opts.SplitSizing = SizingAuto
	}
	return &Grid{opts: opts, groups: map[int]*Group{}, stops: map[*Group]func(){}, nextID: 1}
}

// attach makes a leaf for a group, as the last child of a branch.
func attach(parent *node, grp *Group, size float64) {
	leaf := &node{parent: parent, group: grp, size: size}
	grp.node = leaf
	parent.children = append(parent.children, leaf)
}

// newGroup makes a group with the next id (or the id given, when it is not 0) and follows its
// model.
func (g *Grid) newGroup(id int) *Group {
	if id == 0 {
		id = g.nextID
	}
	g.nextID = max(g.nextID, id+1)
	return g.adopt(&Group{Model: NewModel(id, g.opts.Model)})
}

// adopt registers a group with its model.
func (g *Grid) adopt(grp *Group) *Group {
	g.groups[grp.ID()] = grp
	g.created = append(g.created, grp)
	g.stops[grp] = grp.Model.OnChange(func(ce ChangeEvent) { g.modelChanged(grp, ce) })
	return grp
}

// Options are the settings the grid follows.
func (g *Grid) Options() GridOptions { return g.opts }

// SetOptions changes the settings the grid follows, and those of the models of its groups.
func (g *Grid) SetOptions(opts GridOptions) {
	if opts.SplitSizing == "" {
		opts.SplitSizing = SizingAuto
	}
	g.opts = opts
	for _, grp := range g.groups {
		grp.Model.SetOptions(opts.Model)
	}
}

// OnChange calls fn for every change of the grid and of the models of its groups, and gives the
// function that stops the calls.
func (g *Grid) OnChange(fn func(GridEvent)) (unsubscribe func()) {
	l := &gridListener{fn: fn}
	g.listeners = append(g.listeners, l)
	return func() {
		for i, x := range g.listeners {
			if x == l {
				g.listeners = append(g.listeners[:i:i], g.listeners[i+1:]...)
				return
			}
		}
	}
}

func (g *Grid) fire(e GridEvent) {
	for _, l := range append([]*gridListener(nil), g.listeners...) {
		l.fn(e)
	}
}

// modelChanged forwards the change of a model, and closes the group when its last editor closed
// and workbench.editor.closeEmptyGroups asks for it.
func (g *Grid) modelChanged(grp *Group, ce ChangeEvent) {
	if g.groups[grp.ID()] != grp {
		return
	}
	g.fire(GridEvent{Kind: GroupEditorsChanged, Group: grp, Change: ce})
	if ce.Kind == EditorClose && grp.Empty() && g.opts.CloseEmptyGroups && len(g.groups) > 1 {
		g.removeEmpty(grp)
	}
}

// Version counts the changes of the tree and the sizes that the grid itself made (a group added,
// removed or moved, sizes made even, a layout applied); a view that shows the grid starts over
// from the sizes of the grid when it changes. Sizes that the user dragged (SetSizes) and the
// maximized group do not count.
func (g *Grid) Version() int { return g.version }

func (g *Grid) layoutChanged() {
	g.version++
	g.fire(GridEvent{Kind: LayoutChanged})
}

// Count is the number of groups.
func (g *Grid) Count() int { return len(g.groups) }

// Group is the group with an id, or nil.
func (g *Grid) Group(id int) *Group { return g.groups[id] }

// Active is the active group.
func (g *Grid) Active() *Group { return g.active }

// Groups lists the groups in an order.
func (g *Grid) Groups(order GroupsOrder) []*Group {
	switch order {
	case MostRecentlyActiveGroups:
		var out []*Group
		for _, id := range g.mru {
			if grp := g.groups[id]; grp != nil {
				out = append(out, grp)
			}
		}
		for _, grp := range g.created {
			if g.groups[grp.ID()] == grp && !slices.Contains(out, grp) {
				out = append(out, grp)
			}
		}
		return out
	case GridAppearance:
		var out []*Group
		var walk func(n *node)
		walk = func(n *node) {
			if n.leaf() {
				out = append(out, n.group)
				return
			}
			for _, c := range n.children {
				walk(c)
			}
		}
		walk(g.root)
		return out
	default:
		out := make([]*Group, 0, len(g.created))
		for _, grp := range g.created {
			if g.groups[grp.ID()] == grp {
				out = append(out, grp)
			}
		}
		return out
	}
}

// Index is the place of a group in GridAppearance order, counted from 0; -1 for a group that is not in
// the grid.
func (g *Grid) Index(grp *Group) int { return slices.Index(g.Groups(GridAppearance), grp) }

// Orientation is the orientation of the root split.
func (g *Grid) Orientation() Orientation { return g.root.orient }

// has tells whether a group is in the grid.
func (g *Grid) has(grp *Group) bool { return grp != nil && g.groups[grp.ID()] == grp }

// errUnknownGroup is the error of an operation on a group that is not in the grid.
func errUnknownGroup(grp *Group) error {
	id := 0
	if grp != nil {
		id = grp.ID()
	}
	return errors.New("GroupUnknown", "the group is not in the grid", "group", id)
}

// Activate makes a group the active group. A maximized group that is not the one activated stops
// being maximized.
func (g *Grid) Activate(grp *Group) error {
	if !g.has(grp) {
		return errUnknownGroup(grp)
	}
	if g.maximized != nil && g.maximized != grp {
		g.exitMaximized()
	}
	if g.active != grp {
		g.active = grp
		g.touch(grp)
		g.fire(GridEvent{Kind: ActiveGroupChanged, Group: grp})
	}
	return nil
}

// touch makes a group the most recently active one.
func (g *Grid) touch(grp *Group) {
	g.mru = slices.DeleteFunc(g.mru, func(id int) bool { return id == grp.ID() })
	g.mru = slices.Insert(g.mru, 0, grp.ID())
}

// ---- finding groups ----

// box is the place of a group in the grid, as shares of the width and the height of the whole.
type box struct{ left, top, width, height float64 }

func (g *Grid) boxes() map[*Group]box {
	out := map[*Group]box{}
	var walk func(n *node, b box)
	walk = func(n *node, b box) {
		if n.leaf() {
			out[n.group] = b
			return
		}
		at := 0.0
		for _, c := range n.children {
			cb := b
			if n.orient == Horizontal {
				cb.left, cb.width = b.left+at*b.width, c.size*b.width
			} else {
				cb.top, cb.height = b.top+at*b.height, c.size*b.height
			}
			at += c.size
			walk(c, cb)
		}
	}
	walk(g.root, box{0, 0, 1, 1})
	return out
}

const boxEpsilon = 1e-6

func near(a, b float64) bool { return math.Abs(a-b) < boxEpsilon }

// FindByDirection is the group next to a group on one side: the groups whose edge touches the edge of
// the group on that side, the most recently active first. With wrap a group at the edge of the
// grid finds the groups at the opposite edge (a group alone finds itself). It gives nil when
// there is none.
//
// Ported from VS Code: Grid.getNeighborViews and EditorPart.doFindGroupByDirection
func (g *Grid) FindByDirection(source *Group, dir Direction, wrap bool) *Group {
	if !g.has(source) {
		return nil
	}
	boxes := g.boxes()
	b := boxes[source]
	var offset, lo, hi float64
	switch dir {
	case DirUp:
		offset, lo, hi = b.top, b.left, b.left+b.width
	case DirDown:
		offset, lo, hi = b.top+b.height, b.left, b.left+b.width
	case DirLeft:
		offset, lo, hi = b.left, b.top, b.top+b.height
	case DirRight:
		offset, lo, hi = b.left+b.width, b.top, b.top+b.height
	}
	if wrap {
		switch {
		case dir == DirUp && near(b.top, 0):
			offset = 1
		case dir == DirRight && near(b.left+b.width, 1):
			offset = 0
		case dir == DirDown && near(b.top+b.height, 1):
			offset = 0
		case dir == DirLeft && near(b.left, 0):
			offset = 1
		}
	}
	var found []*Group
	for _, grp := range g.Groups(GridAppearance) {
		o := boxes[grp]
		var edge, olo, ohi float64 // the edge of the other group that faces the boundary
		switch dir {
		case DirUp:
			edge, olo, ohi = o.top+o.height, o.left, o.left+o.width
		case DirDown:
			edge, olo, ohi = o.top, o.left, o.left+o.width
		case DirLeft:
			edge, olo, ohi = o.left+o.width, o.top, o.top+o.height
		case DirRight:
			edge, olo, ohi = o.left, o.top, o.top+o.height
		}
		if near(edge, offset) && !(olo >= hi-boxEpsilon || lo >= ohi-boxEpsilon) {
			found = append(found, grp)
		}
	}
	if len(found) == 0 {
		return nil
	}
	// VS Code sorts by the place in the most-recently-used list; a group that was never active has
	// -1 and so comes first
	rank := func(grp *Group) int { return slices.Index(g.mru, grp.ID()) }
	slices.SortStableFunc(found, func(a, b *Group) int { return rank(a) - rank(b) })
	return found[0]
}

// FindByLocation is the first, last, next or previous group in the order of GridAppearance. With
// wrap, the next of the last group is the first and the previous of the first is the last. It
// gives nil when there is none.
//
// Ported from VS Code: EditorPart.doFindGroupByLocation
func (g *Grid) FindByLocation(source *Group, loc Location, wrap bool) *Group {
	if !g.has(source) {
		return nil
	}
	groups := g.Groups(GridAppearance)
	index := slices.Index(groups, source)
	at := func(i int) *Group {
		if i < 0 || i >= len(groups) {
			return nil
		}
		return groups[i]
	}
	switch loc {
	case LocationFirst:
		return at(0)
	case LocationLast:
		return at(len(groups) - 1)
	case LocationNext:
		if next := at(index + 1); next != nil || !wrap {
			return next
		}
		return at(0)
	default:
		if prev := at(index - 1); prev != nil || !wrap {
			return prev
		}
		return at(len(groups) - 1)
	}
}

// ---- tree surgery ----

// resolveSizing says how to size a split that gets or loses a child: distribute when the setting
// says so, or when it is auto and the children are of equal size already.
func (g *Grid) distributes(parent *node) bool {
	switch g.opts.SplitSizing {
	case SizingDistribute:
		return true
	case SizingSplit:
		return false
	default:
		return distributed(parent)
	}
}

// distributed tells whether the children of a split are of (about) equal size.
func distributed(parent *node) bool {
	lo, hi := math.Inf(1), math.Inf(-1)
	for _, c := range parent.children {
		lo, hi = min(lo, c.size), max(hi, c.size)
	}
	return hi-lo <= 0.005
}

func distribute(parent *node) {
	for _, c := range parent.children {
		c.size = 1 / float64(len(parent.children))
	}
}

// insert puts a group next to the group at, on the side dir (VS Code's Grid.addView with
// GridView.addView).
func (g *Grid) insert(nu, at *Group, dir Direction) {
	if len(g.groups) == 2 { // nu is counted already: at is the only other group
		if g.root.orient != dir.Orientation() {
			g.root.orient = dir.Orientation()
		}
	}
	ref := at.node
	parent := ref.parent
	index := ref.indexInParent()
	leaf := &node{group: nu}
	nu.node = leaf
	if parent.orient == dir.Orientation() {
		equal := g.distributes(parent)
		pos := index
		if dir.after() {
			pos++
		}
		leaf.parent = parent
		parent.children = slices.Insert(parent.children, pos, leaf)
		if equal {
			distribute(parent)
		} else {
			half := ref.size / 2
			ref.size, leaf.size = half, half
		}
		return
	}
	// the other axis: the group and the new one become the children of a new split
	branch := &node{parent: parent, orient: dir.Orientation(), size: ref.size}
	parent.children[index] = branch
	ref.parent, leaf.parent = branch, branch
	ref.size, leaf.size = 0.5, 0.5
	if dir.after() {
		branch.children = []*node{ref, leaf}
	} else {
		branch.children = []*node{leaf, ref}
	}
}

// detach takes a group's leaf out of the tree and gives the room it had to its neighbor (or shares
// it, as the sizing says), and folds away a split that is left with one child (VS Code's
// Grid.removeView with GridView.removeView).
func (g *Grid) detach(grp *Group) {
	leaf := grp.node
	parent := leaf.parent
	index := leaf.indexInParent()
	equal := g.distributes(parent)
	neighbor := parent.children[1]
	if index > 0 {
		neighbor = parent.children[index-1]
	}
	parent.children = slices.Delete(parent.children, index, index+1)
	leaf.parent = nil
	if equal {
		distribute(parent)
	} else {
		neighbor.size += leaf.size
	}
	if len(parent.children) == 1 {
		g.fold(parent)
	}
}

// fold removes a split that has one child left: the child takes its place.
func (g *Grid) fold(parent *node) {
	only := parent.children[0]
	if parent == g.root {
		if only.leaf() {
			only.size = 1
			return
		}
		only.parent, only.size = nil, 1
		g.root = only
		return
	}
	grand := parent.parent
	at := parent.indexInParent()
	if only.leaf() {
		only.size, only.parent = parent.size, grand
		grand.children[at] = only
		return
	}
	for _, c := range only.children {
		c.size *= parent.size
		c.parent = grand
	}
	grand.children = slices.Replace(grand.children, at, at+1, only.children...)
}

// ---- operations ----

// AddGroup adds an empty group next to a group, on the side dir. A group that was the only one
// makes the root split run along that side. The new group is not activated.
//
// Ported from VS Code: EditorPart.addGroup
func (g *Grid) AddGroup(at *Group, dir Direction) (*Group, error) {
	return g.addGroup(at, dir, nil)
}

// CopyGroup adds a group next to a group at, on the side dir, with a copy of the editors of the
// group grp in the same state (workbench.action.duplicateActiveEditorGroupRight and its
// siblings). An editor that cannot be copied is left out. The new group is not activated.
//
// Ported from VS Code: EditorPart.copyGroup
func (g *Grid) CopyGroup(grp, at *Group, dir Direction) (*Group, error) {
	if !g.has(grp) {
		return nil, errUnknownGroup(grp)
	}
	return g.addGroup(at, dir, grp)
}

func (g *Grid) addGroup(at *Group, dir Direction, from *Group) (*Group, error) {
	if !g.has(at) {
		return nil, errUnknownGroup(at)
	}
	g.exitMaximized()
	var nu *Group
	if from == nil {
		nu = g.newGroup(0)
	} else {
		id := g.nextID
		g.nextID++
		nu = g.adopt(&Group{Model: from.Model.Clone(id, func(e EditorInput) EditorInput {
			if c, ok := e.(Copier); ok {
				return c.CopyInput()
			}
			return nil
		})})
	}
	g.insert(nu, at, dir)
	g.fire(GridEvent{Kind: GroupAdded, Group: nu})
	g.layoutChanged()
	return nu, nil
}

// RemoveGroup removes a group. A group with editors moves them to the most recently active other
// group first; the last group cannot be removed (false).
//
// Ported from VS Code: EditorPart.removeGroup
func (g *Grid) RemoveGroup(grp *Group) (bool, error) {
	if !g.has(grp) {
		return false, errUnknownGroup(grp)
	}
	if len(g.groups) == 1 {
		return false, nil
	}
	if grp.Empty() {
		g.removeEmpty(grp)
		return true, nil
	}
	recent := g.Groups(MostRecentlyActiveGroups)
	target := recent[0]
	if g.active == grp {
		target = recent[1]
	}
	_, err := g.MergeGroup(grp, target)
	return true, err
}

// removeEmpty removes an empty group; if it was the active one, the most recently active other
// group becomes active.
func (g *Grid) removeEmpty(grp *Group) {
	g.exitMaximized()
	if g.active == grp {
		recent := g.Groups(MostRecentlyActiveGroups)
		next := recent[1]
		g.active = next
		g.touch(next)
		g.fire(GridEvent{Kind: ActiveGroupChanged, Group: next})
	}
	g.detach(grp)
	delete(g.groups, grp.ID())
	g.mru = slices.DeleteFunc(g.mru, func(id int) bool { return id == grp.ID() })
	if stop := g.stops[grp]; stop != nil {
		stop()
		delete(g.stops, grp)
	}
	g.fire(GridEvent{Kind: GroupRemoved, Group: grp})
	grp.Model.Dispose()
	g.layoutChanged()
}

// MoveGroup moves a group next to another one, on the side dir. Moving a group within its own
// split (next to a sibling along the split's axis) reorders it, keeping the sizes.
//
// Ported from VS Code: Grid.moveView and EditorPart.moveGroup
func (g *Grid) MoveGroup(grp, target *Group, dir Direction) error {
	if !g.has(grp) {
		return errUnknownGroup(grp)
	}
	if !g.has(target) {
		return errUnknownGroup(target)
	}
	if grp == target {
		return errors.New("GroupMoveOntoItself", "a group cannot be moved next to itself", "group", grp.ID())
	}
	g.exitMaximized()
	src, ref := grp.node, target.node
	if src.parent == ref.parent && src.parent.orient == dir.Orientation() {
		from, to := src.indexInParent(), ref.indexInParent()
		if dir.after() {
			to++
		}
		if from < to {
			to--
		}
		if from != to {
			p := src.parent
			p.children = slices.Delete(p.children, from, from+1)
			p.children = slices.Insert(p.children, to, src)
		}
	} else {
		g.detach(grp)
		g.insert(grp, target, dir)
	}
	g.fire(GridEvent{Kind: GroupMoved, Group: grp})
	g.layoutChanged()
	return nil
}

// ---- sizes ----

// Maximized is the maximized group, or nil.
func (g *Grid) Maximized() *Group { return g.maximized }

// Maximize hides every other group and gives all the room to a group; Unmaximize shows them again
// at the sizes they had. It does nothing with a single group.
//
// Ported from VS Code: GridView.maximizeView
func (g *Grid) Maximize(grp *Group) error {
	if !g.has(grp) {
		return errUnknownGroup(grp)
	}
	if len(g.groups) < 2 || g.maximized == grp {
		return nil
	}
	g.maximized = grp
	g.fire(GridEvent{Kind: LayoutChanged})
	return nil
}

// ExitMaximized shows the groups that were hidden by Maximize.
func (g *Grid) ExitMaximized() { g.exitMaximized() }

func (g *Grid) exitMaximized() {
	if g.maximized == nil {
		return
	}
	g.maximized = nil
	g.fire(GridEvent{Kind: LayoutChanged})
}

// ToggleMaximize unmaximizes when a group is maximized, else maximizes a group
// (EditorPart.toggleMaximizeGroup).
func (g *Grid) ToggleMaximize(grp *Group) error {
	if g.maximized != nil {
		g.exitMaximized()
		return nil
	}
	return g.Maximize(grp)
}

// Hidden tells whether a group is hidden by the maximized group.
func (g *Grid) Hidden(grp *Group) bool { return g.maximized != nil && g.maximized != grp }

// DistributeSizes makes all the groups of every split equal (workbench.action.evenEditorWidths).
//
// Ported from VS Code: GridView.distributeViewSizes
func (g *Grid) DistributeSizes() {
	if len(g.groups) < 2 {
		return
	}
	g.exitMaximized()
	var walk func(n *node)
	walk = func(n *node) {
		if n.leaf() {
			return
		}
		distribute(n)
		for _, c := range n.children {
			walk(c)
		}
	}
	walk(g.root)
	g.layoutChanged()
}

// Sizes are the shares of the children of the split at a path (the indexes of the children from the
// root; an empty path is the root), or nil for a path that is not a split.
func (g *Grid) Sizes(path []int) []float64 {
	n := g.at(path)
	if n == nil || n.leaf() {
		return nil
	}
	out := make([]float64, len(n.children))
	for i, c := range n.children {
		out[i] = c.size
	}
	return out
}

// SetSizes sets the shares of the children of the split at a path, as the user dragged a sash. The
// sizes are made to add up to 1 and do not count as a change of Version. A wrong number of sizes,
// or a size that is not positive, is ignored.
func (g *Grid) SetSizes(path []int, sizes []float64) {
	n := g.at(path)
	if n == nil || n.leaf() || len(sizes) != len(n.children) {
		return
	}
	var sum float64
	for _, s := range sizes {
		if s <= 0 || math.IsNaN(s) || math.IsInf(s, 0) {
			return
		}
		sum += s
	}
	for i, c := range n.children {
		c.size = sizes[i] / sum
	}
}

func (g *Grid) at(path []int) *node {
	n := g.root
	for _, i := range path {
		if n.leaf() || i < 0 || i >= len(n.children) {
			return nil
		}
		n = n.children[i]
	}
	return n
}

// Node is a node of the tree of splits as a value, for the view that shows the grid.
type Node struct {
	// Size is the share of the extent of the parent split that the node takes (the root has 1).
	Size float64
	// Group is the group of a leaf; nil for a split.
	Group *Group
	// Orientation is the axis the children of a split lie along.
	Orientation Orientation
	// Children are the nodes of a split.
	Children []Node
}

// Tree describes the tree of splits. The root is always a split.
func (g *Grid) Tree() Node {
	var walk func(n *node) Node
	walk = func(n *node) Node {
		out := Node{Size: n.size, Group: n.group, Orientation: n.orient}
		for _, c := range n.children {
			out.Children = append(out.Children, walk(c))
		}
		return out
	}
	t := walk(g.root)
	t.Size = 1
	return t
}

// Dispose stops following the models. It does not dispose the inputs of the editors.
func (g *Grid) Dispose() {
	if g.disposed {
		return
	}
	g.disposed = true
	for grp, stop := range g.stops {
		stop()
		delete(g.stops, grp)
		grp.Model.Dispose()
	}
	g.listeners = nil
}
