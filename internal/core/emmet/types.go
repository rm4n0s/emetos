package emmet

// Ported from Emmet: packages/abbreviation/src/types.ts

// ParserOptions are the options of the abbreviation parser.
type ParserOptions struct {
	// Text is the text to insert into the implicitly repeated elements, or into the deepest
	// element when none is repeated (the text an abbreviation wraps). Nil is no text.
	Text *Text
	// Variables are the values of the ${name} variables of the abbreviation.
	Variables map[string]string
	// MaxRepeat is the total amount of repeated elements the abbreviation may make (the guard
	// against 'a*1000000'). Zero is DefaultMaxRepeat; a value above HardMaxRepeat is
	// HardMaxRepeat.
	MaxRepeat int
	// JSX turns on the JSX mode: capitalized names with dots (Foo.Bar) and expressions after
	// the shorthands of an attribute (div.{theme.style}).
	JSX bool
	// Href fills the href attribute of a link that wraps a URL or an e-mail address.
	Href bool
}

const (
	// DefaultMaxRepeat is the amount of repeated elements an abbreviation may make when the
	// options do not say (VS Code's Emmet extension uses the same limit). Emmet itself has no
	// limit unless one is given.
	DefaultMaxRepeat = 1000
	// HardMaxRepeat is the largest limit of repeated elements, whatever the options say.
	HardMaxRepeat = 10000
)

// Text is the text that an abbreviation wraps: a string, or a list of strings (the lines of
// the selection). The two differ: a list makes an implicit repeater ('li*') repeat once for each
// non-blank line.
type Text struct {
	value string
	lines []string
	list  bool
}

// TextString makes a Text of one string.
func TextString(s string) *Text { return &Text{value: s} }

// TextLines makes a Text of a list of strings.
func TextLines(lines ...string) *Text { return &Text{lines: lines, list: true} }

// Value is a string, or a Field (a tab stop of the output).
type Value struct {
	// Text is the string. It is meaningful when Field is nil.
	Text string
	// Field is the tab stop, or nil when the value is a string.
	Field *FieldValue
}

// FieldValue is a tab stop in a value: ${index:name}.
type FieldValue struct {
	// Index is the number of the field in the abbreviation (0 is the caret).
	Index int
	// Name is its placeholder.
	Name string
}

// IsField tells if the value is a tab stop.
func (v Value) IsField() bool { return v.Field != nil }

func strValue(s string) Value { return Value{Text: s} }

// AttributeType tells how the value of an attribute is quoted.
type AttributeType string

// The types of attribute values.
const (
	// AttrRaw is a value that was not quoted.
	AttrRaw AttributeType = "raw"
	// AttrSingleQuote is a value in single quotes.
	AttrSingleQuote AttributeType = "singleQuote"
	// AttrDoubleQuote is a value in double quotes.
	AttrDoubleQuote AttributeType = "doubleQuote"
	// AttrExpression is a value in curly braces, written as {value} in the output.
	AttrExpression AttributeType = "expression"
)

// Abbreviation is a parsed abbreviation: the nodes of the top level.
type Abbreviation struct {
	// Children are the nodes of the top level.
	Children []*Node
}

// Node is an element of a parsed abbreviation.
type Node struct {
	// Name is the name of the element; empty for a text only node (a snippet).
	Name string
	// Value is the content of the node; nil is no content, a non-nil empty list is an empty one ('a{}').
	Value []Value
	// Repeat is the repeater the node was made by, nil for a node that was not repeated.
	Repeat *Repeater
	// Attributes of the node; nil is none, a non-nil empty list is 'div[]'.
	Attributes []*Attribute
	// Children are the nodes inside the node.
	Children []*Node
	// SelfClosing is set for an element that has no closing tag ('br/').
	SelfClosing bool

	bem *bemData
}

// Attribute is an attribute of a node.
type Attribute struct {
	// Name of the attribute; empty for a value written alone (the default attribute).
	Name string
	// Value of the attribute; nil is none.
	Value []Value
	// ValueType tells how the value is quoted.
	ValueType AttributeType
	// Boolean is an attribute like 'disabled.'.
	Boolean bool
	// Implied is an attribute that is output only when it has a value ('!href').
	Implied bool
	// Multiple tells that the shorthand was written more than once ('..foo' is a class,
	// written twice).
	Multiple bool
}

// Repeater is the state of a repeated element: it is the Value-th of Count items.
type Repeater struct {
	// Count is how many times the element is repeated.
	Count int
	// Value is the position of the element in the sequence.
	Value int
	// Implicit is a repeater with no count ('li*'): it repeats by the lines of the text.
	Implicit bool
}

// bemData is what the BEM transformation remembers of a node.
type bemData struct {
	classNames []string
	block      string
}
