package emmet

// Ported from Emmet: src/markup/format/walk.ts, src/markup/format/utils.ts and src/markup/utils.ts

// caret is the field that marks where the cursor goes.
var caret = []Value{{Field: &FieldValue{Index: 0}}}

// walkState is the state of a formatter while it goes through the nodes.
type walkState struct {
	current   *Node
	parent    *Node
	ancestors []*Node
	cfg       *Config
	out       *outputStream
	// field is the index of the next field of the output.
	field int
	// comment is the state of the commenting of HTML.
	comment commentState
	// indent holds the options of the indent based formats.
	indent indentOptions
}

func newWalkState(cfg *Config) *walkState {
	return &walkState{cfg: cfg, field: 1, out: newOutputStream(&cfg.Options)}
}

// visitor writes a node; next writes a child of it.
type visitor func(node *Node, index int, items []*Node, st *walkState, next walkNext)

type walkNext func(node *Node, index int, items []*Node)

// walk calls the visitor for each node of the top level.
func walk(abbr *Abbreviation, v visitor, st *walkState) {
	var callback func(ctx *Node, index int, items []*Node)
	var next walkNext
	callback = func(ctx *Node, index int, items []*Node) {
		parent, current := st.parent, st.current
		st.parent = current
		st.current = ctx
		v(ctx, index, items, st, next)
		st.current = current
		st.parent = parent
	}
	next = func(node *Node, index int, items []*Node) {
		st.ancestors = append(st.ancestors, st.current)
		callback(node, index, items)
		st.ancestors = st.ancestors[:len(st.ancestors)-1]
	}
	for i, n := range abbr.Children {
		callback(n, i, abbr.Children)
	}
}

// isSnippet tells if the node is a snippet: it has no name and no attributes.
func isSnippet(n *Node) bool { return n != nil && n.Name == "" && n.Attributes == nil }

// isInlineElement tells if the node is an inline-level element; a missing node is not.
func isInlineElement(n *Node, cfg *Config) bool { return n != nil && isInline(n, cfg) }

// pushTokens writes values and fields.
func pushTokens(tokens []Value, st *walkState) {
	largest := -1
	for _, t := range tokens {
		if !t.IsField() {
			st.out.pushString(t.Text)
		} else {
			st.out.pushField(st.field+t.Field.Index, t.Field.Name)
			if t.Field.Index > largest {
				largest = t.Field.Index
			}
		}
	}
	if largest != -1 {
		st.field += largest + 1
	}
}

// shouldOutputAttribute tells if the attribute is written: an implied one only when it has a value
// (a quoted empty value is one).
func shouldOutputAttribute(a *Attribute) bool {
	return !a.Implied || a.ValueType != AttrRaw || len(a.Value) > 0
}

// walkAll calls fn for each node of the tree (not the root) with the ancestors of the node.
func walkTree(abbr *Abbreviation, fn func(node *Node, ancestors []*Node)) {
	var ancestors []*Node
	var cb func(n *Node)
	cb = func(n *Node) {
		fn(n, ancestors)
		ancestors = append(ancestors, n)
		for _, c := range append([]*Node(nil), n.Children...) {
			cb(c)
		}
		ancestors = ancestors[:len(ancestors)-1]
	}
	for _, c := range append([]*Node(nil), abbr.Children...) {
		cb(c)
	}
}

// find gives the first descendant of the node for which the callback is true.
func find(children []*Node, callback func(*Node) bool) *Node {
	for _, child := range children {
		if callback(child) {
			return child
		}
		if r := find(child.Children, callback); r != nil {
			return r
		}
	}
	return nil
}

// findDeepest gives the last descendant that is always the last child; the root's node.
func findDeepest(children []*Node) (node *Node, ok bool) {
	for len(children) > 0 {
		node = children[len(children)-1]
		ok = true
		children = node.Children
	}
	return node, ok
}
