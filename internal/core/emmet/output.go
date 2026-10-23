package emmet

import (
	"math"
	"regexp"
	"slices"
	"strings"
)

// Ported from Emmet: src/output-stream.ts

const (
	expressionStart = "{"
	expressionEnd   = "}"
)

// outputStream is the text being written, with the position of its end.
type outputStream struct {
	options *Options
	sb      strings.Builder
	level   int
	offset  int
	line    int
	column  int
}

func newOutputStream(options *Options) *outputStream {
	return &outputStream{options: options}
}

func (o *outputStream) value() string { return o.sb.String() }

// push writes plain text, with no line break processing (the text hook still sees it).
func (o *outputStream) push(text string) {
	if f := o.options.Text; f != nil {
		text = f(text, o.offset, o.line, o.column)
	}
	o.pushRaw(text)
}

// pushRaw writes text with no processing.
func (o *outputStream) pushRaw(text string) {
	o.sb.WriteString(text)
	o.offset += len(text)
	o.column += len(text)
}

// pushString writes text that may have line breaks: each line goes with the break and the base
// indentation of the options, so that the line and the column are right.
func (o *outputStream) pushString(value string) {
	lines := splitByLines(value)
	for i, line := range lines {
		o.push(line)
		if i != len(lines)-1 {
			o.pushNewline(useLevel)
		}
	}
}

// useLevel, as the indentation of pushNewline, means the level of the stream.
const useLevel = math.MinInt

// pushNewline writes a line break followed by the base indentation, and then by indent levels of
// indentation: none for 0 (or less), or the level of the stream for useLevel.
func (o *outputStream) pushNewline(indent int) {
	o.push(o.options.Newline + o.options.BaseIndent)
	o.line++
	o.column = len(o.options.BaseIndent)
	switch {
	case indent == useLevel:
		o.pushIndent(o.level)
	case indent != 0:
		o.pushIndent(indent)
	}
}

// pushIndent writes size levels of indentation.
func (o *outputStream) pushIndent(size int) {
	o.push(strings.Repeat(o.options.Indent, max(size, 0)))
}

// pushField writes a field of the output.
func (o *outputStream) pushField(index int, placeholder string) {
	text := placeholder
	if f := o.options.Field; f != nil {
		text = f(index, placeholder, o.offset, o.line, o.column)
	}
	// Written raw: the text hook does not see fields
	o.pushRaw(text)
}

// tagName gives a tag name in the case of the options.
func tagName(name string, cfg *Config) string { return strCase(name, cfg.Options.TagCase) }

// attrName gives an attribute name in the case of the options.
func attrName(name string, cfg *Config) string { return strCase(name, cfg.Options.AttributeCase) }

// attrQuote gives the quote that surrounds the value of an attribute.
func attrQuote(attr *Attribute, cfg *Config, isOpen bool) string {
	if attr.ValueType == AttrExpression {
		if isOpen {
			return expressionStart
		}
		return expressionEnd
	}
	if cfg.Options.AttributeQuotes == QuoteSingle {
		return "'"
	}
	return `"`
}

// isBooleanAttribute tells if the attribute is a boolean one.
func isBooleanAttribute(attr *Attribute, cfg *Config) bool {
	return attr.Boolean || slices.Contains(cfg.Options.BooleanAttributes, strings.ToLower(attr.Name))
}

// selfClose gives the end of a tag with no closing tag.
func selfClose(cfg *Config) string {
	switch cfg.Options.SelfClosingStyle {
	case SelfClosingXHTML:
		return " /"
	case SelfClosingXML:
		return "/"
	}
	return ""
}

// isInlineName tells if the name is an inline-level element.
func isInlineName(name string, cfg *Config) bool {
	return slices.Contains(cfg.Options.InlineElements, strings.ToLower(name))
}

// isInline tells if the node is inline: its name is, or it is text with no attributes.
func isInline(n *Node, cfg *Config) bool {
	if n.Name != "" {
		return isInlineName(n.Name, cfg)
	}
	return n.Value != nil && n.Attributes == nil
}

var lineBreakRe = regexp.MustCompile(`\r\n|\r|\n`)

func splitByLines(text string) []string { return lineBreakRe.Split(text, -1) }

func strCase(s string, c StringCase) string {
	switch c {
	case CaseUpper:
		return strings.ToUpper(s)
	case CaseLower:
		return strings.ToLower(s)
	}
	return s
}
