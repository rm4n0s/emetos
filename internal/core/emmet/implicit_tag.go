package emmet

import "strings"

// Ported from Emmet: src/markup/implicit-tag.ts

var implicitElements = map[string]string{
	"p":        "span",
	"ul":       "li",
	"ol":       "li",
	"table":    "tr",
	"tr":       "td",
	"tbody":    "tr",
	"thead":    "tr",
	"tfoot":    "tr",
	"colgroup": "col",
	"select":   "option",
	"optgroup": "option",
	"audio":    "source",
	"video":    "source",
	"object":   "param",
	"map":      "area",
}

// implicitTag gives a name to a node that has attributes but no name (`.item`).
func implicitTag(node *Node, ancestors []*Node, cfg *Config) {
	if node.Name == "" && node.Attributes != nil {
		resolveImplicitTag(node, ancestors, cfg)
	}
}

// resolveImplicitTag names the node after the element it is in: a `li` in a `ul`, a `span` in an
// inline element, a `div` otherwise.
func resolveImplicitTag(node *Node, ancestors []*Node, cfg *Config) {
	parentName := ""
	if len(ancestors) > 0 {
		parentName = ancestors[len(ancestors)-1].Name
	} else if cfg.Context != nil {
		parentName = cfg.Context.Name
	}
	parentName = strings.ToLower(parentName)
	if name, ok := implicitElements[parentName]; ok {
		node.Name = name
	} else if isInlineName(parentName, cfg) {
		node.Name = "span"
	} else {
		node.Name = "div"
	}
}
