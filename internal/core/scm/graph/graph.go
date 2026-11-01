package graph

import "slices"

// The ids of the two rows that stand for the commits the branch and its remote do not share (VS Code's SCMIncomingHistoryItemId and
// SCMOutgoingHistoryItemId).
const (
	// IncomingID is the row of the incoming changes: the commits of the remote branch that the branch has not.
	IncomingID = "scm-graph-incoming-changes"
	// OutgoingID is the row of the outgoing changes: the commits of the branch that the remote branch has not.
	OutgoingID = "scm-graph-outgoing-changes"
)

// The ids of the theme colors the lanes are drawn in. The colors of the references are the ids of the first three; the lanes that no
// reference colors take Foreground1 to Foreground5 in turn.
const (
	// RefColor is the color of the lane of the current branch (scmGraph.historyItemRefColor).
	RefColor = "scmGraph.historyItemRefColor"
	// RemoteRefColor is the color of the lane of the branch it tracks (scmGraph.historyItemRemoteRefColor).
	RemoteRefColor = "scmGraph.historyItemRemoteRefColor"
	// BaseRefColor is the color of the lane of the branch it was made from (scmGraph.historyItemBaseRefColor).
	BaseRefColor = "scmGraph.historyItemBaseRefColor"
)

// Palette are the colors of the lanes that no reference colors, in the order they are taken (VS Code's colorRegistry).
var Palette = [...]string{"scmGraph.foreground1", "scmGraph.foreground2", "scmGraph.foreground3", "scmGraph.foreground4", "scmGraph.foreground5"}

// Ref is a reference of an item: a branch or a tag that points at it.
type Ref struct {
	// ID identifies the reference ("refs/heads/main").
	ID string
	// Color is the id of the theme color of its badge and of its lane; empty for none. Layout sets it from the color map of the options.
	Color string
}

// Item is a commit to lay out.
type Item struct {
	// ID identifies the commit and ParentIDs are its parents, the first parent first.
	ID        string
	ParentIDs []string
	// Refs are the references that point at the commit. Layout returns them with their colors, the current reference first.
	Refs []Ref
}

// Node is a lane at the top or at the bottom of a row: the commit it is heading for, and its color.
type Node struct {
	// ID is the commit the lane leads to.
	ID string
	// Color is the id of the theme color of the lane.
	Color string
}

// Kind says what a row stands for.
type Kind int

// The kinds of row.
const (
	// KindNode is a commit.
	KindNode Kind = iota
	// KindHead is the commit the current branch points at.
	KindHead
	// KindIncoming is the incoming changes.
	KindIncoming
	// KindOutgoing is the outgoing changes.
	KindOutgoing
)

// Row is the commit and the lanes of a row of the graph (VS Code's ISCMHistoryItemViewModel).
type Row struct {
	// Item is the commit; its references have their colors and are sorted.
	Item Item
	// Kind says what the row stands for.
	Kind Kind
	// Input are the lanes that come into the row from above, Output those that leave it below.
	Input, Output []Node
}

// Index is the lane the circle of the row is on: the one that leads to the commit, or the one after the last when none does (VS Code's
// getHistoryItemIndex).
func (r Row) Index() int {
	for i, n := range r.Input {
		if n.ID == r.Item.ID {
			return i
		}
	}
	return len(r.Input)
}

// RefInfo is a reference that the layout needs to know: the current branch, the one it tracks and the one it was made from.
type RefInfo struct {
	// ID identifies the reference ("refs/heads/main") and Revision is the commit it points at.
	ID, Revision string
}

// Options are what Layout is told about the references.
type Options struct {
	// ColorMap gives the color of the lane of a reference, by its ID. A reference that is in the map with an empty color is colored by
	// the lane of the commit it points at; one that is not in the map has no color.
	ColorMap map[string]string
	// Current, Remote and Base are the current branch, the one it tracks and the one it was made from; nil for none. They order the
	// references of a commit, and Current marks the row of its commit as the head.
	Current, Remote, Base *RefInfo
}

// State is what a page leaves for the next one: the lanes at the bottom of its last row and how many colors of the palette were used.
// The zero value is the state before the first page.
type State struct {
	// Output are the lanes that leave the last row.
	Output []Node
	// next is the place in the palette of the color the next lane without one takes (VS Code's colorIndex + 1).
	next int
}

// Layout lays out a page of commits (newest first, parents after children) after the rows the state describes. It returns the rows of
// the page and the state for the next one. Laid out in pages the rows are the ones the whole list would have, except that the color of
// the lane of a second parent that is only on a later page is taken from the palette (the commit is not known yet).
//
// Ported from VS Code: scmHistory.ts (toISCMHistoryItemViewModelArray)
func Layout(page []Item, prev State, o Options) ([]Row, State) {
	rows := make([]Row, 0, len(page))
	state := prev
	for _, item := range page {
		kind := KindNode
		if o.Current != nil && o.Current.Revision == item.ID {
			kind = KindHead
		}
		input := cloneNodes(state.Output)
		var output []Node
		firstParentAdded := false
		// the first parent takes the place of the lane that leads to the commit
		if len(item.ParentIDs) > 0 {
			for _, node := range input {
				if node.ID == item.ID {
					if !firstParentAdded {
						color := node.Color
						if c := labelColor(item, o.ColorMap); c != "" {
							color = c
						}
						output = append(output, Node{ID: item.ParentIDs[0], Color: color})
						firstParentAdded = true
					}
					continue
				}
				output = append(output, node)
			}
		}
		// the parents that have no lane yet get one
		for i := 0; i < len(item.ParentIDs); i++ {
			if i == 0 && firstParentAdded {
				continue
			}
			var color string
			if i == 0 {
				color = labelColor(item, o.ColorMap)
			} else if j := slices.IndexFunc(page, func(h Item) bool { return h.ID == item.ParentIDs[i] }); j >= 0 {
				color = labelColor(page[j], o.ColorMap)
			}
			if color == "" {
				color = Palette[state.next]
				state.next = (state.next + 1) % len(Palette)
			}
			output = append(output, Node{ID: item.ParentIDs[i], Color: color})
		}
		// the references get the color of their lane
		refs := make([]Ref, len(item.Refs))
		for i, ref := range item.Refs {
			color, known := o.ColorMap[ref.ID]
			if known && color == "" {
				color = circleColor(item.ID, input, output)
			}
			refs[i] = Ref{ID: ref.ID, Color: color}
		}
		slices.SortStableFunc(refs, func(a, b Ref) int { return refOrder(a, o) - refOrder(b, o) })
		item.Refs = refs
		rows = append(rows, Row{Item: item, Kind: kind, Input: input, Output: output})
		state.Output = output
	}
	return rows, state
}

// circleColor is the color of the circle of a commit: that of its lane at the bottom of the row, else at the top, else the color of the
// current branch.
func circleColor(id string, input, output []Node) string {
	i := len(input)
	for j, n := range input {
		if n.ID == id {
			i = j
			break
		}
	}
	switch {
	case i < len(output):
		return output[i].Color
	case i < len(input):
		return input[i].Color
	}
	return RefColor
}

// labelColor is the color of the first reference of the commit that has one in the map, or of the incoming or outgoing changes.
func labelColor(item Item, colors map[string]string) string {
	switch item.ID {
	case IncomingID:
		return RemoteRefColor
	case OutgoingID:
		return RefColor
	}
	for _, ref := range item.Refs {
		if c := colors[ref.ID]; c != "" {
			return c
		}
	}
	return ""
}

func cloneNodes(n []Node) []Node { return slices.Clone(n) }

// refOrder orders the references of a commit: the current one, the one it tracks, the one it was made from, the ones with a color, the
// rest (VS Code's compareHistoryItemRefs).
func refOrder(r Ref, o Options) int {
	switch {
	case o.Current != nil && r.ID == o.Current.ID:
		return 1
	case o.Remote != nil && r.ID == o.Remote.ID:
		return 2
	case o.Base != nil && r.ID == o.Base.ID:
		return 3
	case r.Color != "":
		return 4
	}
	return 99
}

// ChangesOptions say how WithChanges adds the incoming and the outgoing changes.
type ChangesOptions struct {
	// Current and Remote are the current branch and the one it tracks.
	Current, Remote *RefInfo
	// Incoming and Outgoing say which of the two rows are wanted.
	Incoming, Outgoing bool
	// MergeBase is the commit the branch and its remote branch part at; empty when it is not known (then no row is added).
	MergeBase string
}

// WithChanges adds the row of the incoming changes before the merge base and the row of the outgoing changes before the head, among
// rows laid out by Layout, and joins their lanes. The rows it is given are not changed; the rows around the new ones are copied. A row
// is added only when the commits it needs are among the rows (the merge base, the head).
//
// Ported from VS Code: scmHistory.ts (addIncomingOutgoingChangesHistoryItems)
func WithChanges(rows []Row, o ChangesOptions) []Row {
	rows = slices.Clone(rows)
	if o.MergeBase == "" || sameRevision(o.Current, o.Remote) {
		return rows
	}
	if o.Incoming && o.Remote != nil && o.Remote.Revision != o.MergeBase {
		before := -1
		for i := len(rows) - 1; i >= 0; i-- {
			if hasNode(rows[i].Output, o.MergeBase) {
				before = i
				break
			}
		}
		after := slices.IndexFunc(rows, func(r Row) bool { return r.Item.ID == o.MergeBase })
		if before != -1 && after != -1 {
			// the incoming changes may have been merged already: then the row is not shown
			merged := len(rows[before].Item.ParentIDs) == 2 && slices.Contains(rows[before].Item.ParentIDs, o.MergeBase)
			if !merged {
				b := rows[before]
				b.Input = redirect(b.Input, o.MergeBase, IncomingID)
				b.Output = redirect(b.Output, o.MergeBase, IncomingID)
				rows[before] = b
				incoming := Row{
					Item: Item{ID: IncomingID, ParentIDs: []string{o.MergeBase}}, Kind: KindIncoming,
					Input: cloneNodes(rows[before].Output), Output: cloneNodes(rows[after].Input),
				}
				rows = slices.Insert(rows, after, incoming)
			}
		}
	}
	if o.Outgoing && o.Current != nil && o.Current.Revision != "" && o.Current.Revision != o.MergeBase {
		head := slices.IndexFunc(rows, func(r Row) bool { return r.Kind == KindHead && r.Item.ID == o.Current.Revision })
		if head != -1 {
			input := cloneNodes(rows[head].Input)
			output := append(cloneNodes(rows[head].Input), Node{ID: o.Current.Revision, Color: RefColor})
			outgoing := Row{
				Item: Item{ID: OutgoingID, ParentIDs: []string{o.Current.Revision}}, Kind: KindOutgoing, Input: input, Output: output,
			}
			h := rows[head]
			h.Input = append(cloneNodes(h.Input), Node{ID: o.Current.Revision, Color: RefColor})
			rows[head] = h
			rows = slices.Insert(rows, head, outgoing)
		}
	}
	return rows
}

func sameRevision(a, b *RefInfo) bool {
	switch {
	case a == nil && b == nil:
		return true
	case a == nil || b == nil:
		return false
	}
	return a.Revision == b.Revision
}

func hasNode(nodes []Node, id string) bool {
	return slices.ContainsFunc(nodes, func(n Node) bool { return n.ID == id })
}

// redirect makes the lanes that lead to a commit and have the color of the remote branch lead to another one.
func redirect(nodes []Node, from, to string) []Node {
	out := cloneNodes(nodes)
	for i, n := range out {
		if n.ID == from && n.Color == RemoteRefColor {
			out[i].ID = to
		}
	}
	return out
}
