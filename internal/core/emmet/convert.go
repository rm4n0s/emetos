package emmet

import (
	"regexp"
	"strconv"
	"strings"
)

// Ported from Emmet: packages/abbreviation/src/convert.ts, packages/abbreviation/src/stringify.ts
// and packages/abbreviation/src/index.ts

var (
	urlRegex   = regexp.MustCompile(`^((https?:|ftp:|file:)?//|(www|ftp)\.)[^ ]*$`)
	emailRegex = regexp.MustCompile(`^[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,5}$`)
	schemeRe   = regexp.MustCompile(`\w+:`)
)

// maxNumberWidth is the largest width a repeater number ($$$...) is padded to.
const maxNumberWidth = 64

// maxNesting is the deepest nesting of elements and groups the parser accepts, so that a long
// run of `(` or `>` cannot exhaust the stack.
const maxNesting = 1000

// ParseAbbreviation tokenizes, parses and converts a markup abbreviation into a tree of nodes
// with the repeaters unrolled and the numbering, text and variables filled in.
func ParseAbbreviation(abbr string, opts ParserOptions) (*Abbreviation, error) {
	tokens, err := Tokenize(abbr)
	if err != nil {
		return nil, err
	}
	tree, err := ParseTokens(tokens, opts)
	if err != nil {
		return nil, err
	}
	return Convert(tree, opts), nil
}

// convertState is what the conversion remembers while it goes through the statements.
type convertState struct {
	opts        ParserOptions
	inserted    bool
	textUsed    bool
	cleanText   []string
	repeatGuard int
	// repeaters are the repeaters of the statements around the one being converted.
	repeaters []*Repeater
}

// Convert makes the node tree of a parsed abbreviation: it unrolls the groups and the repeaters,
// numbers the elements, inserts the text and the variables and moves the children of text
// nodes. It never fails.
func Convert(abbr *Statement, opts ParserOptions) *Abbreviation {
	st := &convertState{opts: opts, repeatGuard: effectiveMaxRepeat(opts.MaxRepeat)}
	if t := opts.Text; t != nil {
		if t.list {
			st.cleanText = []string{}
			for _, s := range t.lines {
				if strings.TrimSpace(s) != "" {
					st.cleanText = append(st.cleanText, s)
				}
			}
		}
	}

	result := &Abbreviation{Children: st.convertGroup(abbr, nil)}

	if opts.Text != nil && !st.textUsed && len(result.Children) > 0 {
		// Text was given but there is no implicitly repeated element: it goes into the deepest child
		deepest := deepestNode(result.Children[len(result.Children)-1])
		text := opts.Text.value
		if opts.Text.list {
			text = strings.Join(opts.Text.lines, "\n")
		}
		insertText(deepest, text)
		if deepest.Name == "a" && opts.Href {
			// The link takes the URL or the e-mail address that is inserted
			insertHref(deepest, text)
		}
	}
	return result
}

func effectiveMaxRepeat(n int) int {
	switch {
	case n <= 0:
		return DefaultMaxRepeat
	case n > HardMaxRepeat:
		return HardMaxRepeat
	}
	return n
}

// getText gives the text for an item of an implicit repeater, or the whole text.
func (st *convertState) getText(pos *int) string {
	st.textUsed = true
	t := st.opts.Text
	if t == nil {
		return ""
	}
	if !t.list {
		return t.value
	}
	if pos != nil {
		if *pos >= 0 && *pos < len(st.cleanText) {
			return st.cleanText[*pos]
		}
		if *pos >= 0 && *pos < len(t.lines) {
			return t.lines[*pos]
		}
		return ""
	}
	return strings.Join(t.lines, "\n")
}

func (st *convertState) getVariable(name string) string {
	if v, ok := st.opts.Variables[name]; ok {
		return v
	}
	return name
}

// convertStatement converts an element or a group; a repeated one makes a copy for each repeat.
func (st *convertState) convertStatement(node *Statement) []*Node {
	if node.Repeat == nil {
		return st.convertNode(node, nil)
	}

	var result []*Node
	repeat := &Repeater{Implicit: node.Repeat.Implicit}
	if repeat.Implicit && st.opts.Text != nil && st.opts.Text.list {
		repeat.Count = len(st.cleanText)
	} else {
		repeat.Count = node.Repeat.Count
		if repeat.Count == 0 {
			repeat.Count = 1
		}
	}

	st.repeaters = append(st.repeaters, repeat)
	for i := 0; i < repeat.Count; i++ {
		repeat.Value = i
		items := st.convertNode(node, repeat)

		if repeat.Implicit && !st.inserted {
			// An implicit repeater with no repeater placeholder inside: the text goes into the deepest node
			if len(items) > 0 {
				pos := repeat.Value
				insertText(deepestNode(items[len(items)-1]), st.getText(&pos))
			}
		}

		result = append(result, items...)

		// At least one item is output even if the limit is reached
		st.repeatGuard--
		if st.repeatGuard <= 0 {
			break
		}
	}
	st.repeaters = st.repeaters[:len(st.repeaters)-1]

	if repeat.Implicit {
		st.inserted = true
	}
	return result
}

func (st *convertState) convertNode(node *Statement, repeat *Repeater) []*Node {
	if node.IsGroup {
		return st.convertGroup(node, repeat)
	}
	return st.convertElement(node, repeat)
}

func (st *convertState) convertElement(node *Statement, repeat *Repeater) []*Node {
	elem := &Node{SelfClosing: node.SelfClose, Children: []*Node{}}
	if repeat != nil {
		r := *repeat
		elem.Repeat = &r
	}
	if node.Name != nil {
		elem.Name = st.stringifyName(node.Name)
	}
	if node.Value != nil {
		elem.Value = st.stringifyValue(node.Value)
	}
	result := []*Node{elem}

	children := []*Node{}
	for _, child := range node.Elements {
		children = append(children, st.convertStatement(child)...)
	}

	if node.Attributes != nil {
		elem.Attributes = make([]*Attribute, 0, len(node.Attributes))
		for _, attr := range node.Attributes {
			elem.Attributes = append(elem.Attributes, st.convertAttribute(attr))
		}
	}

	// A text node without fields has no children: they are its siblings
	if elem.Name == "" && elem.Attributes == nil && elem.Value != nil && !hasField(elem.Value) {
		result = append(result, children...)
	} else {
		elem.Children = children
	}
	return result
}

func (st *convertState) convertGroup(node *Statement, repeat *Repeater) []*Node {
	var result []*Node
	for _, child := range node.Elements {
		result = append(result, st.convertStatement(child)...)
	}
	if repeat != nil {
		// The items that were not repeated by themselves are repeated by the group
		for _, item := range result {
			if item.Repeat == nil {
				r := *repeat
				item.Repeat = &r
			}
		}
	}
	return result
}

func (st *convertState) convertAttribute(node TokenAttribute) *Attribute {
	implied := false
	isBoolean := false
	valueType := AttrRaw
	if node.Expression {
		valueType = AttrExpression
	}
	var value []Value
	name := ""
	if node.Name != nil {
		name = st.stringifyName(node.Name)
	}

	if name != "" && name[0] == '!' {
		implied = true
	}
	if name != "" && name[len(name)-1] == '.' {
		isBoolean = true
	}

	if node.HasValue {
		tokens := append([]Token{}, node.Value...)

		if len(tokens) > 0 && tokens[0].Type == TokenQuote {
			// A quoted value: the quotes are not a part of the value but are remembered
			quote := tokens[0]
			tokens = tokens[1:]
			if len(tokens) > 0 && tokens[len(tokens)-1].Type == TokenQuote {
				tokens = tokens[:len(tokens)-1]
			}
			if quote.Single {
				valueType = AttrSingleQuote
			} else {
				valueType = AttrDoubleQuote
			}
		} else if len(tokens) > 0 && isBracket(&tokens[0], BracketExpression, true) {
			// An expression: the braces are not a part of the value but are remembered
			valueType = AttrExpression
			tokens = tokens[1:]
			if len(tokens) > 0 && isBracket(&tokens[len(tokens)-1], BracketExpression, false) {
				tokens = tokens[:len(tokens)-1]
			}
		}
		value = st.stringifyValue(tokens)
	}

	if implied || isBoolean {
		from, to := 0, len(name)
		if implied {
			from = 1
		}
		if isBoolean {
			to--
		}
		name = name[from:to]
	}

	return &Attribute{
		Name:      name,
		Value:     value,
		Boolean:   isBoolean,
		Implied:   implied,
		ValueType: valueType,
		Multiple:  node.Multiple,
	}
}

// stringifyName writes the tokens of a name.
func (st *convertState) stringifyName(tokens []Token) string {
	var sb strings.Builder
	for i := range tokens {
		sb.WriteString(st.stringifyToken(&tokens[i]))
	}
	return sb.String()
}

// stringifyValue writes the tokens of a value; a field with an index stays a field.
func (st *convertState) stringifyValue(tokens []Token) []Value {
	result := []Value{}
	var sb strings.Builder
	for i := range tokens {
		tok := &tokens[i]
		if tok.Type == TokenField && tok.HasIndex {
			// The fields stay in the output: an editor has its own syntax for them, and it
			// has to know where they are
			if sb.Len() > 0 {
				result = append(result, strValue(sb.String()))
				sb.Reset()
			}
			result = append(result, Value{Field: &FieldValue{Index: tok.Index, Name: tok.Name}})
		} else {
			sb.WriteString(st.stringifyToken(tok))
		}
	}
	if sb.Len() > 0 {
		result = append(result, strValue(sb.String()))
	}
	return result
}

// stringifyToken writes a token of a value as text.
func (st *convertState) stringifyToken(tok *Token) string {
	switch tok.Type {
	case TokenLiteral, TokenWhiteSpace:
		return tok.Value
	case TokenQuote:
		if tok.Single {
			return "'"
		}
		return `"`
	case TokenBracket:
		switch tok.Context {
		case BracketAttribute:
			if tok.Open {
				return "["
			}
			return "]"
		case BracketExpression:
			if tok.Open {
				return "{"
			}
			return "}"
		}
		if tok.Open {
			return "("
		}
		return ")"
	case TokenOperator:
		return operatorChar(tok.Operator)
	case TokenField:
		if tok.HasIndex {
			// A field, as TextMate writes it
			if tok.Name != "" {
				return "${" + strconv.Itoa(tok.Index) + ":" + tok.Name + "}"
			}
			return "${" + strconv.Itoa(tok.Index) + "}"
		} else if tok.Name != "" {
			// A variable
			return st.getVariable(tok.Name)
		}
		return ""
	case TokenRepeaterPlaceholder:
		// The closest implicit repeater
		var pos *int
		for i := len(st.repeaters) - 1; i >= 0; i-- {
			if st.repeaters[i].Implicit {
				v := st.repeaters[i].Value
				pos = &v
				break
			}
		}
		st.inserted = true
		return st.getText(pos)
	case TokenRepeaterNumber:
		return st.repeaterNumber(tok)
	}
	return ""
}

func (st *convertState) repeaterNumber(tok *Token) string {
	value := 1
	lastIx := len(st.repeaters) - 1
	if lastIx >= 0 {
		rep := st.repeaters[lastIx]
		if tok.Reverse {
			value = tok.Base + rep.Count - rep.Value - 1
		} else {
			value = tok.Base + rep.Value
		}
		if tok.Parent > 0 {
			parentIx := max(0, lastIx-tok.Parent)
			if parentIx != lastIx {
				value += rep.Count * st.repeaters[parentIx].Value
			}
		}
	}
	result := strconv.Itoa(value)
	for width := min(tok.Size, maxNumberWidth); len(result) < width; {
		result = "0" + result
	}
	return result
}

func operatorChar(op OperatorType) string {
	switch op {
	case OpChild:
		return ">"
	case OpClass:
		return "."
	case OpClimb:
		return "^"
	case OpID:
		return "#"
	case OpEqual:
		return "="
	case OpClose:
		return "/"
	case OpSibling:
		return "+"
	}
	return ""
}

func hasField(values []Value) bool {
	for _, v := range values {
		if v.IsField() {
			return true
		}
	}
	return false
}

func deepestNode(n *Node) *Node {
	for len(n.Children) > 0 {
		n = n.Children[len(n.Children)-1]
	}
	return n
}

// insertText appends text to the value of a node.
func insertText(n *Node, text string) {
	if n.Value == nil {
		n.Value = []Value{strValue(text)}
		return
	}
	if len(n.Value) > 0 && !n.Value[len(n.Value)-1].IsField() {
		n.Value[len(n.Value)-1].Text += text
	} else {
		n.Value = append(n.Value, strValue(text))
	}
}

// insertHref sets the href of a link from the text that is inserted into it.
func insertHref(n *Node, text string) {
	href := ""
	if urlRegex.MatchString(text) {
		href = text
		if !schemeRe.MatchString(href) && !strings.HasPrefix(href, "//") {
			href = "http://" + href
		}
	} else if emailRegex.MatchString(text) {
		href = "mailto:" + text
	}

	var hrefAttr *Attribute
	for _, a := range n.Attributes {
		if a.Name == "href" {
			hrefAttr = a
			break
		}
	}
	switch {
	case hrefAttr == nil:
		n.Attributes = append(n.Attributes, &Attribute{Name: "href", Value: []Value{strValue(href)}, ValueType: AttrDoubleQuote})
	case hrefAttr.Value == nil:
		hrefAttr.Value = []Value{strValue(href)}
	}
}
