package emmet

import (
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Ported from Emmet: src/markup/format/html.ts

var htmlTagRegex = regexp.MustCompile(`^<([\w\-:]+)[\s>]`)

var reservedKeywords = map[string]bool{
	"for": true, "while": true, "of": true, "async": true, "await": true, "const": true, "let": true,
	"var": true, "continue": true, "break": true, "debugger": true, "do": true, "export": true,
	"import": true, "in": true, "instanceof": true, "new": true, "return": true, "switch": true,
	"this": true, "throw": true, "try": true, "catch": true, "typeof": true, "void": true,
	"with": true, "yield": true,
}

var propKeyRegex = regexp.MustCompile(`^[a-zA-Z_$][\w_$]*$`)

// formatHTML writes the abbreviation as HTML (and XML, XSL and JSX).
func formatHTML(abbr *Abbreviation, cfg *Config) string {
	st := newWalkState(cfg)
	st.comment = newCommentState(cfg)
	walk(abbr, htmlElement, st)
	return st.out.value()
}

// htmlElement writes a node.
func htmlElement(node *Node, index int, items []*Node, st *walkState, next walkNext) {
	out, cfg := st.out, st.cfg
	format := htmlShouldFormat(node, index, items, st)

	// The level of the node
	level := htmlIndent(st)
	out.level += level

	if format {
		out.pushNewline(useLevel)
	}

	nextChildren := func() {
		for i, c := range node.Children {
			next(c, i, node.Children)
		}
	}

	if node.Name != "" {
		name := tagName(node.Name, cfg)
		commentNodeBefore(node, st)
		out.pushString("<" + name)

		for _, attr := range node.Attributes {
			if shouldOutputAttribute(attr) {
				pushAttribute(attr, st)
			}
		}

		if node.SelfClosing && len(node.Children) == 0 && node.Value == nil {
			out.pushString(selfClose(cfg) + ">")
		} else {
			out.pushString(">")

			if !pushSnippet(node, st, next) {
				if node.Value != nil {
					innerFormat := slices.ContainsFunc(node.Value, hasNewline) || startsWithBlockTag(node.Value, cfg)
					if innerFormat {
						out.level++
						out.pushNewline(out.level)
					}
					pushTokens(node.Value, st)
					if innerFormat {
						out.level--
						out.pushNewline(out.level)
					}
				}

				nextChildren()

				if node.Value == nil && len(node.Children) == 0 {
					innerFormat := cfg.Options.FormatLeafNode || slices.Contains(cfg.Options.FormatForce, node.Name)
					if innerFormat {
						out.level++
						out.pushNewline(out.level)
					}
					pushTokens(caret, st)
					if innerFormat {
						out.level--
						out.pushNewline(out.level)
					}
				}
			}

			out.pushString("</" + name + ">")
			commentNodeAfter(node, st)
		}
	} else if !pushSnippet(node, st, next) && node.Value != nil {
		// A text node (a snippet)
		pushTokens(node.Value, st)
		nextChildren()
	}

	if format && index == len(items)-1 && st.parent != nil {
		offset := 1
		if isSnippet(st.parent) {
			offset = 0
		}
		out.pushNewline(out.level - offset)
	}

	out.level -= level
}

// pushAttribute writes an attribute.
func pushAttribute(attr *Attribute, st *walkState) {
	out, cfg := st.out, st.cfg
	if attr.Name == "" {
		return
	}
	attributes := cfg.Options.MarkupAttributes
	valuePrefix := cfg.Options.MarkupValuePrefix

	name, value := attr.Name, attr.Value
	lQuote := attrQuote(attr, cfg, true)
	rQuote := attrQuote(attr, cfg, false)

	if attributes != nil {
		if v := getMultiValue(name, attributes, attr.Multiple); v != "" {
			name = v
		}
	}
	name = attrName(name, cfg)

	if cfg.Options.JSXEnabled && attr.Multiple {
		lQuote, rQuote = expressionStart, expressionEnd
	}

	prefix := ""
	if valuePrefix != nil {
		prefix = getMultiValue(attr.Name, valuePrefix, attr.Multiple)
	}

	if prefix != "" && len(value) == 1 && !value[0].IsField() {
		// The prefix makes the value a property of an object
		val := value[0].Text
		if isPropKey(val) {
			value = []Value{strValue(prefix + "." + val)}
		} else {
			value = []Value{strValue(prefix + "['" + val + "']")}
		}
		if cfg.Options.JSXEnabled {
			lQuote, rQuote = expressionStart, expressionEnd
		}
	}

	if isBooleanAttribute(attr, cfg) && value == nil {
		// A boolean attribute with no value is written in the XML style (hidden="hidden")
		// unless the compact notation is on
		if !cfg.Options.CompactBoolean {
			value = []Value{strValue(name)}
		}
	} else if value == nil {
		value = caret
	}

	out.pushString(" " + name)
	if value != nil {
		out.pushString("=" + lQuote)
		pushTokens(value, st)
		out.pushString(rQuote)
	} else if cfg.Options.SelfClosingStyle != SelfClosingHTML {
		out.pushString("=" + lQuote + rQuote)
	}
}

// pushSnippet writes a node that has a value with a field and children: the children go into the
// first field. It tells if it did.
func pushSnippet(node *Node, st *walkState, next walkNext) bool {
	if node.Value != nil && len(node.Children) > 0 {
		fieldIx := slices.IndexFunc(node.Value, func(v Value) bool { return v.IsField() })
		if fieldIx != -1 {
			pushTokens(node.Value[:fieldIx], st)
			line := st.out.line
			pos := fieldIx + 1
			for i, c := range node.Children {
				next(c, i, node.Children)
			}

			// If there was a line change, the white space after the children is trimmed
			if st.out.line != line && pos < len(node.Value) && !node.Value[pos].IsField() {
				st.out.pushString(strings.TrimLeftFunc(node.Value[pos].Text, jsIsSpace))
				pos++
			}

			if pos < len(node.Value) {
				pushTokens(node.Value[pos:], st)
			}
			return true
		}
	}
	return false
}

// jsIsSpace tells the characters that JavaScript's trim removes.
func jsIsSpace(r rune) bool {
	return (unicode.IsSpace(r) && r != 0x85) || r == 0xFEFF
}

// htmlShouldFormat tells if the node goes on a line of its own.
func htmlShouldFormat(node *Node, index int, items []*Node, st *walkState) bool {
	cfg, parent := st.cfg, st.parent

	if !cfg.Options.Format {
		return false
	}
	if index == 0 && parent == nil {
		// The very first node is not formatted
		return false
	}
	// The single child of a snippet is not formatted
	if parent != nil && isSnippet(parent) && len(items) == 1 {
		return false
	}

	if isSnippet(node) {
		// Adjacent text nodes and snippets, a text with line breaks (it looks like a fragment of
		// code to wrap), and a snippet that wraps its children in a field
		if isSnippet(nodeAt(items, index-1)) || isSnippet(nodeAt(items, index+1)) ||
			slices.ContainsFunc(node.Value, hasNewline) ||
			(hasField(node.Value) && len(node.Children) > 0) {
			return true
		}
	}

	if isInline(node, cfg) {
		// An inline node after a block-level one
		if index == 0 {
			// The first node: formatted if a block-level node follows
			for _, it := range items {
				if !isInline(it, cfg) {
					return true
				}
			}
		} else if !isInline(items[index-1], cfg) {
			return true
		}

		if cfg.Options.InlineBreak != 0 {
			// Inline nodes before and after this one
			adjacent := 1
			before, after := index, index
			for before--; isInlineElement(nodeAt(items, before), cfg); before-- {
				adjacent++
			}
			for after++; isInlineElement(nodeAt(items, after), cfg); after++ {
				adjacent++
			}
			if adjacent >= cfg.Options.InlineBreak {
				return true
			}
		}

		// An inline node that has a node that is formatted
		for i, c := range node.Children {
			if htmlShouldFormat(c, i, node.Children, st) {
				return true
			}
		}
		return false
	}
	return true
}

func nodeAt(items []*Node, i int) *Node {
	if i >= 0 && i < len(items) {
		return items[i]
	}
	return nil
}

// htmlIndent gives the levels of indentation the children of the parent add.
func htmlIndent(st *walkState) int {
	parent := st.parent
	if parent == nil || isSnippet(parent) || (parent.Name != "" && slices.Contains(st.cfg.Options.FormatSkip, parent.Name)) {
		return 0
	}
	return 1
}

func hasNewline(v Value) bool {
	return !v.IsField() && strings.ContainsAny(v.Text, "\r\n")
}

// startsWithBlockTag tells if the value starts with the tag of a block-level element.
func startsWithBlockTag(value []Value, cfg *Config) bool {
	if len(value) > 0 && !value[0].IsField() {
		if m := htmlTagRegex.FindStringSubmatch(value[0].Text); m != nil && !slices.Contains(cfg.Options.InlineElements, strings.ToLower(m[1])) {
			return true
		}
	}
	return false
}

func getMultiValue(key string, data map[string]string, multiple bool) string {
	if multiple {
		if v := data[key+"*"]; v != "" {
			return v
		}
	}
	return data[key]
}

func isPropKey(name string) bool {
	return !reservedKeywords[name] && propKeyRegex.MatchString(name)
}
