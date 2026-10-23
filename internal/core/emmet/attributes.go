package emmet

// Ported from Emmet: src/markup/attributes.ts

// mergeAttributes merges the attributes of a node: the ones with the same name become one, and
// the class names are joined.
func mergeAttributes(node *Node, cfg *Config) {
	if node.Attributes == nil {
		return
	}
	attributes := make([]*Attribute, 0, len(node.Attributes))
	lookup := map[string]*Attribute{}

	for _, attr := range node.Attributes {
		if attr.Name == "" {
			attributes = append(attributes, attr)
			continue
		}
		if prev, ok := lookup[attr.Name]; ok {
			if attr.Name == "class" {
				prev.Value = mergeValue(prev.Value, attr.Value, " ")
			} else {
				mergeDeclarations(prev, attr, cfg)
			}
		} else {
			// A new attribute, so that it can be changed later
			c := *attr
			lookup[attr.Name] = &c
			attributes = append(attributes, &c)
		}
	}
	node.Attributes = attributes
}

// mergeValue joins two lists of values into one; strings that touch become one string.
func mergeValue(prev, next []Value, glue string) []Value {
	if prev != nil && next != nil {
		result := append([]Value{}, prev...)
		if len(result) > 0 && glue != "" {
			result = appendValue(result, strValue(glue))
		}
		for _, t := range next {
			result = appendValue(result, t)
		}
		return result
	}
	r := prev
	if r == nil {
		r = next
	}
	if r == nil {
		return nil
	}
	return append([]Value{}, r...)
}

// mergeDeclarations adds the data of the attribute src to dest.
func mergeDeclarations(dest, src *Attribute, cfg *Config) {
	dest.Name = src.Name
	if !cfg.Options.ReverseAttributes {
		dest.Value = src.Value
	}
	// The properties that are kept
	if !dest.Implied {
		dest.Implied = src.Implied
	}
	if !dest.Boolean {
		dest.Boolean = src.Boolean
	}
	if dest.ValueType != AttrExpression {
		dest.ValueType = src.ValueType
	}
}

func appendValue(tokens []Value, v Value) []Value {
	if n := len(tokens); n > 0 && !tokens[n-1].IsField() && !v.IsField() {
		tokens[n-1].Text += v.Text
		return tokens
	}
	return append(tokens, v)
}
