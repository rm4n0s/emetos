package editorgroup

import (
	"slices"

	"github.com/rm4n0s/errors"
)

// LayoutNode is a node of a Layout: a group, or a split of nodes along the orthogonal axis of
// its parent (VS Code's GroupLayoutArgument).
type LayoutNode struct {
	// Size is the weight of the node among its siblings; nodes without one share what the sized
	// nodes leave.
	Size float64 `json:"size,omitempty"`
	// Groups are the nodes of a split; a node without them is a group. A split of one node is that
	// node.
	Groups []LayoutNode `json:"groups,omitempty"`
}

// Layout describes the shape of the grid without its editors (VS Code's EditorGroupLayout, what
// `vscode.setEditorLayout` and `vscode.getEditorLayout` carry). The orientation is the axis of
// Groups.
type Layout struct {
	Orientation Orientation  `json:"orientation"`
	Groups      []LayoutNode `json:"groups"`
}

// Layout gives the shape and the sizes of the grid (EditorPart.getLayout).
func (g *Grid) Layout() Layout {
	var conv func(n *node) LayoutNode
	conv = func(n *node) LayoutNode {
		out := LayoutNode{Size: n.size}
		for _, c := range n.children {
			out.Groups = append(out.Groups, conv(c))
		}
		return out
	}
	root := conv(g.root)
	return Layout{Orientation: g.root.orient, Groups: root.Groups}
}

// sanitize gives the nodes without a size a share, as VS Code's sanitizeGridNodeDescriptor does,
// and turns a split of one node into that node.
func sanitize(n *LayoutNode, root bool) {
	if !root && len(n.Groups) <= 1 {
		n.Groups = nil
	}
	if len(n.Groups) == 0 {
		return
	}
	var defined float64
	definedCount := 0
	for i := range n.Groups {
		sanitize(&n.Groups[i], false)
		if n.Groups[i].Size > 0 {
			defined += n.Groups[i].Size
			definedCount++
		}
	}
	total := 1.0
	if definedCount > 0 {
		total = defined
	}
	if undefined := len(n.Groups) - definedCount; undefined > 0 {
		each := total / float64(undefined)
		for i := range n.Groups {
			if n.Groups[i].Size <= 0 {
				n.Groups[i].Size = each
			}
		}
	}
}

func countLayoutGroups(nodes []LayoutNode) int {
	n := 0
	for _, c := range nodes {
		if len(c.Groups) > 1 {
			n += countLayoutGroups(c.Groups)
		} else {
			n++
		}
	}
	return n
}

// ApplyLayout rebuilds the grid in the shape of a layout, keeping the groups: they take the
// places of the layout in the order of the grid. If the layout has fewer places, the editors of
// the extra groups move to the last place; if it has more, empty groups are added.
//
// Ported from VS Code: EditorPart.applyLayout
func (g *Grid) ApplyLayout(l Layout) error {
	if len(l.Groups) == 0 {
		return errors.New("LayoutInvalid", "a layout needs at least one group")
	}
	if l.Orientation != Horizontal && l.Orientation != Vertical {
		return errors.New("LayoutInvalid", "the orientation of a layout is 0 (horizontal) or 1 (vertical)", "orientation", int(l.Orientation))
	}
	places := countLayoutGroups(l.Groups)
	current := g.Groups(GridAppearance)
	if places < len(current) {
		last := current[places-1]
		for _, grp := range current[places:] {
			if g.has(grp) {
				if _, err := g.MergeGroup(grp, last); err != nil {
					return err
				}
			}
		}
		current = g.Groups(GridAppearance)
	}
	g.exitMaximized()
	for len(current) < places {
		grp := g.newGroup(0)
		current = append(current, grp)
		g.fire(GridEvent{Kind: GroupAdded, Group: grp})
	}
	root := LayoutNode{Groups: slices.Clone(l.Groups)}
	for i := range root.Groups {
		root.Groups[i] = cloneLayout(root.Groups[i])
	}
	sanitize(&root, true)
	next := 0
	var build func(ln LayoutNode, parent *node, orient Orientation) *node
	build = func(ln LayoutNode, parent *node, orient Orientation) *node {
		if len(ln.Groups) == 0 {
			grp := current[next]
			next++
			leaf := &node{parent: parent, group: grp, size: ln.Size}
			grp.node = leaf
			return leaf
		}
		n := &node{parent: parent, orient: orient, size: ln.Size}
		for _, c := range ln.Groups {
			n.children = append(n.children, build(c, n, orient.orthogonal()))
		}
		return n
	}
	g.root = build(root, nil, l.Orientation)
	g.root.size = 1
	if len(g.root.children) == 1 && !g.root.children[0].leaf() {
		g.fold(g.root)
	}
	normalizeSizes(g.root)
	g.layoutChanged()
	return nil
}

func cloneLayout(n LayoutNode) LayoutNode {
	out := LayoutNode{Size: n.Size}
	for _, c := range n.Groups {
		out.Groups = append(out.Groups, cloneLayout(c))
	}
	return out
}

// normalizeSizes makes the sizes of the children of every split add up to 1.
func normalizeSizes(n *node) {
	if n.leaf() {
		return
	}
	var sum float64
	for _, c := range n.children {
		sum += c.size
	}
	for _, c := range n.children {
		if sum > 0 {
			c.size /= sum
		} else {
			c.size = 1 / float64(len(n.children))
		}
		normalizeSizes(c)
	}
}

// SerializedNode is a node of a SerializedGrid: a group with its editors, or a split.
type SerializedNode struct {
	// Size is the share of the node among its siblings.
	Size float64 `json:"size"`
	// Groups are the nodes of a split.
	Groups []SerializedNode `json:"groups,omitempty"`
	// Model is the group, for a leaf.
	Model *SerializedModel `json:"model,omitempty"`
}

// SerializedGrid is a Grid in the saved state of a window (step 7.5): VS Code's ISerializedGrid
// with the state of the editor part.
type SerializedGrid struct {
	// Orientation is the axis of Root.
	Orientation Orientation `json:"orientation"`
	// Root are the children of the root split.
	Root []SerializedNode `json:"root"`
	// Active is the id of the active group, MRU the ids of the groups from the most recently
	// active.
	Active int   `json:"active"`
	MRU    []int `json:"mru"`
	// Maximized is the id of the maximized group, or 0.
	Maximized int `json:"maximized,omitempty"`
	// NextID is the id the next group gets.
	NextID int `json:"nextId"`
}

// Serialize gives the state of the grid to save: the tree, the sizes and the editors of each
// group that can be serialized, the active group and the order of the groups.
func (g *Grid) Serialize() SerializedGrid {
	var conv func(n *node) SerializedNode
	conv = func(n *node) SerializedNode {
		out := SerializedNode{Size: n.size}
		if n.leaf() {
			m := n.group.Model.Serialize()
			out.Model = &m
			return out
		}
		for _, c := range n.children {
			out.Groups = append(out.Groups, conv(c))
		}
		return out
	}
	root := conv(g.root)
	out := SerializedGrid{Orientation: g.root.orient, Root: root.Groups, Active: g.active.ID(), NextID: g.nextID}
	for _, id := range g.mru {
		if g.groups[id] != nil {
			out.MRU = append(out.MRU, id)
		}
	}
	if g.maximized != nil {
		out.Maximized = g.maximized.ID()
	}
	return out
}

// RestoreGrid makes a grid from the state Serialize gave. create makes the input of a saved
// editor, or reports false when it cannot (see RestoreModel). A state that is not a tree of
// groups fails with the tag GridStateInvalid.
func RestoreGrid(data SerializedGrid, opts GridOptions, create func(typeID, value string) (EditorInput, bool)) (*Grid, error) {
	if len(data.Root) == 0 {
		return nil, errors.New("GridStateInvalid", "the saved grid has no group")
	}
	if data.Orientation != Horizontal && data.Orientation != Vertical {
		return nil, errors.New("GridStateInvalid", "the orientation of the saved grid is not known", "orientation", int(data.Orientation))
	}
	g := newBareGrid(opts)
	var build func(sn SerializedNode, parent *node, orient Orientation) (*node, error)
	build = func(sn SerializedNode, parent *node, orient Orientation) (*node, error) {
		switch {
		case sn.Model != nil && len(sn.Groups) == 0:
			id := sn.Model.ID
			if id < 1 || g.groups[id] != nil {
				return nil, errors.New("GridStateInvalid", "a saved group has an id that is not valid or is used twice", "group", id)
			}
			grp := g.adopt(&Group{Model: RestoreModel(*sn.Model, g.opts.Model, create)})
			g.nextID = max(g.nextID, id+1)
			leaf := &node{parent: parent, group: grp, size: sn.Size}
			grp.node = leaf
			return leaf, nil
		case sn.Model == nil && len(sn.Groups) >= 2:
			n := &node{parent: parent, orient: orient, size: sn.Size}
			for _, c := range sn.Groups {
				child, err := build(c, n, orient.orthogonal())
				if err != nil {
					return nil, err
				}
				n.children = append(n.children, child)
			}
			return n, nil
		}
		return nil, errors.New("GridStateInvalid", "a saved node is neither a group nor a split of two or more nodes")
	}
	root := &node{orient: data.Orientation, size: 1}
	for _, c := range data.Root {
		child, err := build(c, root, data.Orientation.orthogonal())
		if err != nil {
			g.Dispose()
			return nil, err
		}
		root.children = append(root.children, child)
	}
	if len(root.children) == 1 && !root.children[0].leaf() {
		g.Dispose()
		return nil, errors.New("GridStateInvalid", "the root of the saved grid has one split and no group")
	}
	g.root = root
	normalizeSizes(root)
	g.nextID = max(g.nextID, data.NextID)
	for _, id := range data.MRU {
		if g.groups[id] != nil && !slices.Contains(g.mru, id) {
			g.mru = append(g.mru, id)
		}
	}
	g.active = g.groups[data.Active]
	if g.active == nil {
		g.active = g.Groups(GridAppearance)[0]
	}
	g.touch(g.active)
	if m := g.groups[data.Maximized]; m != nil && len(g.groups) > 1 {
		g.maximized = m
	}
	return g, nil
}
