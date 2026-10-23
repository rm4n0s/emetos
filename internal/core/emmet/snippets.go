package emmet

import "slices"

// Ported from Emmet: src/markup/snippets.ts

// resolveSnippets finds the snippet each node stands for and replaces the node by it.
//
// The snippets are abbreviations too: a name, attributes and so on, maybe with several
// elements. So a snippet is parsed and resolved in turn. A snippet that is in the stack of the ones
// being resolved is a circular reference, as in "img": "img[src alt]/", which is a valid
// snippet: the element is left as it is.
func resolveSnippets(abbr *Abbreviation, cfg *Config) {
	var stack []string
	reversed := cfg.Options.ReverseAttributes

	var resolve func(child *Node) *Abbreviation
	resolve = func(child *Node) *Abbreviation {
		snippet := cfg.Snippets[child.Name]
		if child.Name == "" || snippet == "" || slices.Contains(stack, snippet) {
			return nil
		}

		snippetAbbr, err := ParseAbbreviation(snippet, ParserOptions{Variables: cfg.Variables, MaxRepeat: cfg.MaxRepeat})
		if err != nil {
			// A snippet of the user may be invalid: it is left out silently
			cfg.warn("Unable to parse \""+snippet+"\" snippet", err)
			return nil
		}

		stack = append(stack, snippet)
		snippetAbbr.Children = walkResolve(snippetAbbr.Children, resolve)
		stack = stack[:len(stack)-1]

		// The attributes of the node go into every node of the top level of the snippet
		for _, top := range snippetAbbr.Children {
			if child.Attributes != nil {
				from := top.Attributes
				to := child.Attributes
				if reversed {
					top.Attributes = append(append([]*Attribute{}, to...), from...)
				} else {
					top.Attributes = append(append([]*Attribute{}, from...), to...)
				}
			}
			mergeNodes(child, top)
		}
		return snippetAbbr
	}

	abbr.Children = walkResolve(abbr.Children, resolve)
}

func walkResolve(children []*Node, resolve func(*Node) *Abbreviation) []*Node {
	out := []*Node{}
	for _, child := range children {
		if resolved := resolve(child); resolved != nil {
			out = append(out, resolved.Children...)
			if deepest, ok := findDeepest(resolved.Children); ok {
				deepest.Children = append(deepest.Children, walkResolve(child.Children, resolve)...)
			}
		} else {
			out = append(out, child)
			child.Children = walkResolve(child.Children, resolve)
		}
	}
	return out
}

// mergeNodes adds the data of the first node to the second.
func mergeNodes(from, to *Node) {
	if from.SelfClosing {
		to.SelfClosing = true
	}
	if from.Value != nil {
		to.Value = from.Value
	}
	if from.Repeat != nil {
		r := *from.Repeat
		to.Repeat = &r
	}
}
