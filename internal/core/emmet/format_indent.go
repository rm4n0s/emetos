package emmet

import (
	"regexp"
	"strings"
)

// Ported from Emmet: src/markup/format/indent-format.ts, src/markup/format/haml.ts,
// src/markup/format/pug.ts and src/markup/format/slim.ts
//
// The formats that are written by indentation: HAML, Pug and Slim.

// indentOptions describe a format that is written by indentation.
type indentOptions struct {
	// beforeName is written before the name of the tag.
	beforeName string
	// afterName is written after the name of the tag.
	afterName string
	// beforeAttribute and afterAttribute surround the secondary attributes.
	beforeAttribute string
	afterAttribute  string
	// glueAttribute is written between the secondary attributes.
	glueAttribute string
	// booleanValue is the value of a boolean attribute.
	booleanValue string
	// beforeTextLine and afterTextLine surround each line of a text that has several.
	beforeTextLine string
	afterTextLine  string
	// selfClose is written after an element that has no closing tag, like `/`.
	selfClose string
}

func formatHAML(abbr *Abbreviation, cfg *Config) string {
	return indentFormat(abbr, cfg, indentOptions{
		beforeName:      "%",
		beforeAttribute: "(",
		afterAttribute:  ")",
		glueAttribute:   " ",
		afterTextLine:   " |",
		booleanValue:    "true",
		selfClose:       "/",
	})
}

func formatPug(abbr *Abbreviation, cfg *Config) string {
	sc := ""
	if cfg.Options.SelfClosingStyle == SelfClosingXML {
		sc = "/"
	}
	return indentFormat(abbr, cfg, indentOptions{
		beforeAttribute: "(",
		afterAttribute:  ")",
		glueAttribute:   ", ",
		beforeTextLine:  "| ",
		selfClose:       sc,
	})
}

func formatSlim(abbr *Abbreviation, cfg *Config) string {
	return indentFormat(abbr, cfg, indentOptions{
		beforeAttribute: " ",
		glueAttribute:   " ",
		beforeTextLine:  "| ",
		selfClose:       "/",
	})
}

func indentFormat(abbr *Abbreviation, cfg *Config, opts indentOptions) string {
	st := newWalkState(cfg)
	st.indent = opts
	walk(abbr, indentElement, st)
	return st.out.value()
}

// indentElement writes a node.
func indentElement(node *Node, index int, items []*Node, st *walkState, next walkNext) {
	out, opts := st.out, &st.indent
	primary, secondary := collectAttributes(node)

	// The level of the node
	level := 0
	if st.parent != nil {
		level = 1
	}
	out.level += level

	// The top level elements are not indented
	if indentShouldFormat(node, index, st) {
		out.pushNewline(useLevel)
	}

	if node.Name != "" && (node.Name != "div" || len(primary) == 0) {
		out.pushString(opts.beforeName + node.Name + opts.afterName)
	}

	pushPrimaryAttributes(primary, st)
	var shown []*Attribute
	for _, a := range secondary {
		if shouldOutputAttribute(a) {
			shown = append(shown, a)
		}
	}
	pushSecondaryAttributes(shown, st)

	if node.SelfClosing && node.Value == nil && len(node.Children) == 0 {
		if opts.selfClose != "" {
			out.pushString(opts.selfClose)
		}
	} else {
		pushValue(node, st)
		for i, c := range node.Children {
			next(c, i, node.Children)
		}
	}

	out.level -= level
}

// collectAttributes splits the attributes of a node into the primary ones (id and class, which have
// a syntax of their own in these formats) and the secondary ones.
func collectAttributes(node *Node) (primary, secondary []*Attribute) {
	for _, a := range node.Attributes {
		if a.Name == "class" || a.Name == "id" {
			primary = append(primary, a)
		} else {
			secondary = append(secondary, a)
		}
	}
	return primary, secondary
}

var spaceRunRegex = regexp.MustCompile(`[\s\p{Zs}\x{feff}\x{2028}\x{2029}]+`)

// pushPrimaryAttributes writes the id and the class.
func pushPrimaryAttributes(attrs []*Attribute, st *walkState) {
	for _, a := range attrs {
		if a.Value == nil {
			continue
		}
		if a.Name == "class" {
			st.out.pushString(".")
			// White space in class names are dots
			tokens := make([]Value, len(a.Value))
			for i, t := range a.Value {
				if t.IsField() {
					tokens[i] = t
				} else {
					tokens[i] = strValue(spaceRunRegex.ReplaceAllString(t.Text, "."))
				}
			}
			pushTokens(tokens, st)
		} else {
			// The id
			st.out.pushString("#")
			pushTokens(a.Value, st)
		}
	}
}

// pushSecondaryAttributes writes the attributes other than the id and the class.
func pushSecondaryAttributes(attrs []*Attribute, st *walkState) {
	if len(attrs) == 0 {
		return
	}
	out, cfg, opts := st.out, st.cfg, &st.indent
	if opts.beforeAttribute != "" {
		out.pushString(opts.beforeAttribute)
	}
	for i, a := range attrs {
		out.pushString(attrName(a.Name, cfg))
		if isBooleanAttribute(a, cfg) && a.Value == nil {
			if !cfg.Options.CompactBoolean && opts.booleanValue != "" {
				out.pushString("=" + opts.booleanValue)
			}
		} else {
			out.pushString("=" + attrQuote(a, cfg, true))
			if a.Value != nil {
				pushTokens(a.Value, st)
			} else {
				pushTokens(caret, st)
			}
			out.pushString(attrQuote(a, cfg, false))
		}
		if i != len(attrs)-1 && opts.glueAttribute != "" {
			out.pushString(opts.glueAttribute)
		}
	}
	if opts.afterAttribute != "" {
		out.pushString(opts.afterAttribute)
	}
}

// pushValue writes the value of a node.
func pushValue(node *Node, st *walkState) {
	// Either the value or the caret, and the caret only for the nodes with no children
	if node.Value == nil && len(node.Children) > 0 {
		return
	}
	value := node.Value
	if value == nil {
		value = caret
	}
	lines := splitValueByLines(value)
	out, opts := st.out, &st.indent

	if len(lines) == 1 {
		if node.Name != "" || node.Attributes != nil {
			out.push(" ")
		}
		pushTokens(value, st)
		return
	}

	// A value of several lines has a line length that is the same for all of them
	lineLengths := make([]int, len(lines))
	maxLength := 0
	for i, line := range lines {
		lineLengths[i] = valueLength(line)
		maxLength = max(maxLength, lineLengths[i])
	}

	out.level++
	for i, line := range lines {
		out.pushNewline(useLevel)
		if opts.beforeTextLine != "" {
			out.push(opts.beforeTextLine)
		}
		pushTokens(line, st)
		if opts.afterTextLine != "" {
			out.push(strings.Repeat(" ", maxLength-lineLengths[i]))
			out.push(opts.afterTextLine)
		}
	}
	out.level--
}

// valueLength is the length of the text of values, a field counting as its name. The length is
// in characters as JavaScript counts (UTF-16 units) would be for text in the BMP.
func valueLength(tokens []Value) int {
	n := 0
	for _, t := range tokens {
		if t.IsField() {
			n += utf16Len(t.Field.Name)
		} else {
			n += utf16Len(t.Text)
		}
	}
	return n
}

func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n++
		if r >= 0x10000 {
			n++
		}
	}
	return n
}

// splitValueByLines splits the tokens of a value into lines.
func splitValueByLines(tokens []Value) [][]Value {
	var result [][]Value
	var line []Value
	for _, t := range tokens {
		if t.IsField() {
			line = append(line, t)
			continue
		}
		parts := crlfRegex.Split(t.Text, -1)
		line = append(line, strValue(parts[0]))
		for _, p := range parts[1:] {
			result = append(result, line)
			line = []Value{strValue(p)}
		}
	}
	if len(line) > 0 {
		result = append(result, line)
	}
	return result
}

var crlfRegex = regexp.MustCompile(`\r\n?|\n`)

func indentShouldFormat(node *Node, index int, st *walkState) bool {
	// The first top level element and the snippets are not formatted
	if st.parent == nil && index == 0 {
		return false
	}
	return !isSnippet(node)
}
