package dbconn

import "strconv"

// TreeRows lists the rows of the tree of the view as "depth:label" (the depth is the number of rows above it). It is for the tests of
// the extensions that fill the tree with their engine's nodes, which cannot reach the view's model; call it on the UI thread.
func (e *Extension) TreeRows() []string {
	if e.view == nil {
		return nil
	}
	var out []string
	for _, n := range e.view.tree.Rows() {
		label := n.Label
		if n.Placeholder {
			label = "(loading)"
		}
		out = append(out, strconv.Itoa(n.Depth)+":"+label)
	}
	return out
}

// ExpandTree opens the row at the end of a path of labels (the name of a connection, then the label of a node below it, and so on)
// and every row on the way. It returns false when a row of the path is not there (yet): the nodes below a row are read when it is
// first opened, so the caller waits for them and calls again. It is for the same tests as TreeRows; call it on the UI thread.
func (e *Extension) ExpandTree(path ...string) bool {
	if e.view == nil {
		return false
	}
	parent := ""
	for depth, label := range path {
		found := ""
		for _, n := range e.view.tree.Rows() {
			if n.Depth == depth && n.Label == label && !n.Placeholder && n.Parent == parent {
				found = n.Key
				break
			}
		}
		if found == "" {
			return false
		}
		e.view.tree.Expand(found)
		parent = found
	}
	return true
}
