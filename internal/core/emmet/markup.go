package emmet

// Ported from Emmet: src/markup/index.ts and src/index.ts

// Expand expands a markup abbreviation and writes it in the syntax and the style of the configuration.
func Expand(abbr string, cfg Config) (string, error) {
	tree, err := Parse(abbr, cfg)
	if err != nil {
		return "", err
	}
	return Stringify(tree, cfg), nil
}

// Parse parses a markup abbreviation into the tree that is ready to be written: the snippets are
// resolved and the transformations of the syntax and of the options are done.
func Parse(abbr string, cfg Config) (*Abbreviation, error) {
	tree, err := ParseAbbreviation(abbr, ParserOptions{
		Text:      cfg.Text,
		Variables: cfg.Variables,
		MaxRepeat: cfg.MaxRepeat,
		JSX:       cfg.Options.JSXEnabled,
		Href:      cfg.Options.MarkupHref,
	})
	if err != nil {
		return nil, err
	}
	// The text was used by the parser; the snippets are parsed with no text
	cfg.Text = nil
	return Resolve(tree, cfg), nil
}

// Resolve does the second part of Parse for an abbreviation that is parsed: the abbreviations
// (snippets) that the names stand for replace the nodes that have them, and the nodes are changed
// for output (implicit tags, merged attributes, Lorem ipsum, XSL, labels, BEM). It changes the tree.
func Resolve(abbr *Abbreviation, cfg Config) *Abbreviation {
	// Two passes: each node is mapped to its snippet, which is an abbreviation too (it may be
	// several nodes), and then each resolved node is transformed
	resolveSnippets(abbr, &cfg)
	walkTree(abbr, func(node *Node, ancestors []*Node) {
		transform(node, ancestors, &cfg)
	})
	return abbr
}

// Stringify writes a parsed abbreviation as the syntax of the configuration says.
func Stringify(abbr *Abbreviation, cfg Config) string {
	switch cfg.Syntax {
	case "haml":
		return formatHAML(abbr, &cfg)
	case "slim":
		return formatSlim(abbr, &cfg)
	case "pug":
		return formatPug(abbr, &cfg)
	}
	return formatHTML(abbr, &cfg)
}

// transform changes a node for output.
func transform(node *Node, ancestors []*Node, cfg *Config) {
	implicitTag(node, ancestors, cfg)
	mergeAttributes(node, cfg)
	lorem(node, ancestors, cfg)

	if cfg.Syntax == "xsl" {
		xsl(node)
	}
	label(node)
	if cfg.Options.BEMEnabled {
		bem(node, ancestors, cfg)
	}
}
