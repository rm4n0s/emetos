package emmet

// Ported from Emmet: src/markup/addon/label.ts and src/markup/addon/xsl.ts

// label is the preprocessor of `<label>`: if it holds an `<input>` (or a `<textarea>`), the empty
// `for` of the label and the empty `id` of the input are removed.
func label(node *Node) {
	if node.Name != "label" {
		return
	}
	input := find(node.Children, func(n *Node) bool { return n.Name == "input" || n.Name == "textarea" })
	if input == nil {
		return
	}
	if node.Attributes != nil {
		node.Attributes = filterAttributes(node.Attributes, "for")
	}
	if input.Attributes != nil {
		input.Attributes = filterAttributes(input.Attributes, "id")
	}
}

// filterAttributes removes the attributes of the name that are empty.
func filterAttributes(attrs []*Attribute, name string) []*Attribute {
	out := make([]*Attribute, 0, len(attrs))
	for _, a := range attrs {
		if a.Name != name || !isEmptyAttribute(a) {
			out = append(out, a)
		}
	}
	return out
}

// isEmptyAttribute tells an attribute with no value, or with only a field with no name.
func isEmptyAttribute(a *Attribute) bool {
	if a.Value == nil {
		return true
	}
	return len(a.Value) == 1 && a.Value[0].IsField() && a.Value[0].Field.Name == ""
}

// xsl removes the `select` attribute from the nodes that have content: <xsl:variable> and
// <xsl:with-param>.
func xsl(node *Node) {
	if (node.Name == "xsl:variable" || node.Name == "xsl:with-param") && node.Attributes != nil && (len(node.Children) > 0 || node.Value != nil) {
		out := make([]*Attribute, 0, len(node.Attributes))
		for _, a := range node.Attributes {
			if a.Name != "select" {
				out = append(out, a)
			}
		}
		node.Attributes = out
	}
}
