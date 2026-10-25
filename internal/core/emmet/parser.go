package emmet

import "github.com/rm4n0s/errors"

// Ported from Emmet: packages/abbreviation/src/parser/index.ts and
// packages/abbreviation/src/parser/TokenScanner.ts

// Statement is an element or a group of the parsed tokens of an abbreviation.
type Statement struct {
	// IsGroup is set for a group: only Elements and Repeat are used.
	IsGroup bool
	// Name is the tokens of the name of an element; nil is no name.
	Name []Token
	// Attributes of the element; nil is none, a non-nil empty list is '[]'.
	Attributes []TokenAttribute
	// Value is the tokens of the text of the element; nil is no text, a non-nil empty list is '{}'.
	Value []Token
	// Repeat is the repeater token of the element or of the group, nil when there is none.
	Repeat *Token
	// SelfClose is set for an element that ends with a slash.
	SelfClose bool
	// Elements are the children of an element, or the statements of a group.
	Elements []*Statement
}

// TokenAttribute is an attribute of an element as tokens.
type TokenAttribute struct {
	// Name is the tokens of the name; nil for a value written alone.
	Name []Token
	// Value is the tokens of the value, if HasValue.
	Value []Token
	// Expression is set for the value of a shorthand written as an expression (in JSX mode).
	Expression bool
	// Multiple is set for a shorthand written more than once in a row, like '..'.
	Multiple bool
	// HasValue tells that Value is set (it may be empty).
	HasValue bool
}

// tokenScanner reads the tokens.
type tokenScanner struct {
	tokens []Token
	start  int
	pos    int
}

func (s *tokenScanner) readable() bool { return s.pos < len(s.tokens) }

// peek gives the next token, or nil.
func (s *tokenScanner) peek() *Token {
	if s.pos < len(s.tokens) {
		return &s.tokens[s.pos]
	}
	return nil
}

func (s *tokenScanner) next() *Token {
	t := s.peek()
	s.pos++
	return t
}

// slice gives the tokens from..to; never nil.
func (s *tokenScanner) slice(from, to int) []Token {
	r := make([]Token, to-from)
	copy(r, s.tokens[from:to])
	return r
}

func (s *tokenScanner) sliceCurrent() []Token { return s.slice(s.start, s.pos) }

func (s *tokenScanner) consume(test func(*Token) bool) bool {
	if t := s.peek(); t != nil && test(t) {
		s.pos++
		return true
	}
	return false
}

// where gives the place of an error at a token (the one at the position by default): the text " at N" to
// add to the message, and the offset, or "" and -1 when there is no token.
func (s *tokenScanner) where(tok *Token) (at string, pos int) {
	if tok == nil {
		tok = s.peek()
	}
	if tok == nil {
		return "", -1
	}
	return " at " + itoa(tok.Start), tok.Start
}

// ParseTokens reads the tokens of an abbreviation as a tree of elements and groups. The result is
// a group with no repeater.
func ParseTokens(tokens []Token, opts ParserOptions) (*Statement, error) {
	s := &tokenScanner{tokens: tokens}
	result, err := statements(s, opts, 0)
	if err != nil {
		return nil, err
	}
	if s.readable() {
		at, pos := s.where(nil)
		return nil, errors.New("UnexpectedToken", "Unexpected character"+at, "pos", pos)
	}
	return result, nil
}

func statements(s *tokenScanner, opts ParserOptions, depth int) (*Statement, error) {
	if depth > maxNesting {
		at, pos := s.where(nil)
		return nil, errors.New("NestingTooDeep", "Abbreviation is nested too deep"+at, "pos", pos)
	}
	result := &Statement{IsGroup: true, Elements: []*Statement{}}
	ctx := result
	var stack []*Statement

	for s.readable() {
		node, err := element(s, opts)
		if err != nil {
			return nil, err
		}
		if node == nil {
			node, err = group(s, opts, depth+len(stack))
			if err != nil {
				return nil, err
			}
		}
		if node == nil {
			break
		}
		ctx.Elements = append(ctx.Elements, node)
		switch {
		case s.consume(isChildOperator):
			if depth+len(stack) >= maxNesting {
				at, pos := s.where(nil)
				return nil, errors.New("NestingTooDeep", "Abbreviation is nested too deep"+at, "pos", pos)
			}
			stack = append(stack, ctx)
			ctx = node
		case s.consume(isSiblingOperator):
			continue
		case s.consume(isClimbOperator):
			for {
				if len(stack) > 0 {
					ctx = stack[len(stack)-1]
					stack = stack[:len(stack)-1]
				}
				if !s.consume(isClimbOperator) {
					break
				}
			}
		}
	}
	return result, nil
}

// group reads a group from the scanner.
func group(s *tokenScanner, opts ParserOptions, depth int) (*Statement, error) {
	if !s.consume(isGroupStart) {
		return nil, nil
	}
	result, err := statements(s, opts, depth+1)
	if err != nil {
		return nil, err
	}
	if tok := s.next(); isBracket(tok, BracketGroup, false) {
		result.Repeat = repeaterAt(s)
	}
	return result, nil
}

// element reads one element from the scanner; nil when there is none.
func element(s *tokenScanner, opts ParserOptions) (*Statement, error) {
	elem := &Statement{Elements: []*Statement{}}

	if elementName(s, opts) {
		elem.Name = s.sliceCurrent()
	}

	for s.readable() {
		s.start = s.pos
		if elem.Repeat == nil && !isEmptyElement(elem) && s.consume(isRepeater) {
			elem.Repeat = &s.tokens[s.pos-1]
			continue
		}
		if elem.Value == nil && text(s) {
			elem.Value = getText(s)
			continue
		}
		if attr, ok := shortAttribute(s, OpID, opts); ok {
			elem.Attributes = append(attrsOrEmpty(elem.Attributes), attr)
			continue
		}
		if attr, ok := shortAttribute(s, OpClass, opts); ok {
			elem.Attributes = append(attrsOrEmpty(elem.Attributes), attr)
			continue
		}
		set, ok, err := attributeSet(s)
		if err != nil {
			return nil, err
		}
		if ok {
			elem.Attributes = append(attrsOrEmpty(elem.Attributes), set...)
			continue
		}
		if !isEmptyElement(elem) && s.consume(isCloseOperator) {
			elem.SelfClose = true
			if elem.Repeat == nil && s.consume(isRepeater) {
				elem.Repeat = &s.tokens[s.pos-1]
			}
		}
		break
	}

	if isEmptyElement(elem) {
		return nil, nil
	}
	return elem, nil
}

func attrsOrEmpty(a []TokenAttribute) []TokenAttribute {
	if a == nil {
		return []TokenAttribute{}
	}
	return a
}

// attributeSet reads `[a b=c]`. The set may be empty.
func attributeSet(s *tokenScanner) ([]TokenAttribute, bool, error) {
	if !s.consume(isAttributeSetStart) {
		return nil, false, nil
	}
	attributes := []TokenAttribute{}
	for s.readable() {
		attr, ok, err := attribute(s)
		if err != nil {
			return nil, false, err
		}
		if ok {
			attributes = append(attributes, attr)
		} else if s.consume(isAttributeSetEnd) {
			break
		} else if !s.consume(isWhiteSpaceToken) {
			at, pos := s.where(nil)
			return nil, false, errors.New("UnexpectedAttributeToken", `Unexpected "`+tokenTypeName(s.peek().Type)+`" token`+at, "pos", pos)
		}
	}
	return attributes, true, nil
}

func tokenTypeName(t TokenType) string {
	switch t {
	case TokenLiteral:
		return "Literal"
	case TokenWhiteSpace:
		return "WhiteSpace"
	case TokenQuote:
		return "Quote"
	case TokenBracket:
		return "Bracket"
	case TokenOperator:
		return "Operator"
	case TokenRepeater:
		return "Repeater"
	case TokenRepeaterNumber:
		return "RepeaterNumber"
	case TokenRepeaterPlaceholder:
		return "RepeaterPlaceholder"
	case TokenField:
		return "Field"
	}
	return "Unknown"
}

// shortAttribute reads the shorthand of an id or a class.
func shortAttribute(s *tokenScanner, typ OperatorType, opts ParserOptions) (TokenAttribute, bool) {
	if !isOperator(s.peek(), typ) {
		return TokenAttribute{}, false
	}
	s.pos++

	// More operators of the same kind
	count := 1
	for isOperator(s.peek(), typ) {
		s.pos++
		count++
	}

	name := "class"
	if typ == OpID {
		name = "id"
	}
	attr := TokenAttribute{Name: []Token{{Type: TokenLiteral, Value: name}}}
	if count > 1 {
		attr.Multiple = true
	}

	// In JSX an expression follows the shorthand, like in `div.{theme.style}`
	if opts.JSX && text(s) {
		attr.Value = getText(s)
		attr.HasValue = true
		attr.Expression = true
	} else if literalTokens(s, false) {
		attr.Value = s.sliceCurrent()
		attr.HasValue = true
	}
	return attr, true
}

// attribute reads one attribute of a set.
func attribute(s *tokenScanner) (TokenAttribute, bool, error) {
	ok, err := quoted(s)
	if err != nil {
		return TokenAttribute{}, false, err
	}
	if ok {
		// A quoted value is the value of the default attribute
		return TokenAttribute{Value: s.sliceCurrent(), HasValue: true}, true, nil
	}

	if literalTokens(s, true) {
		attr := TokenAttribute{Name: s.sliceCurrent()}
		if s.consume(isEquals) {
			ok, err := quoted(s)
			if err != nil {
				return TokenAttribute{}, false, err
			}
			if ok || literalTokens(s, true) {
				attr.Value = s.sliceCurrent()
				attr.HasValue = true
			}
		}
		return attr, true, nil
	}
	return TokenAttribute{}, false, nil
}

func repeaterAt(s *tokenScanner) *Token {
	if isRepeater(s.peek()) {
		t := &s.tokens[s.pos]
		s.pos++
		return t
	}
	return nil
}

// quoted reads a quoted value; the quotes are a part of the tokens.
func quoted(s *tokenScanner) (bool, error) {
	start := s.pos
	q := s.peek()
	if isQuote(q) {
		quote := *q
		s.pos++
		for s.readable() {
			if isQuoteSingle(s.next(), quote.Single) {
				s.start = start
				return true, nil
			}
		}
		at, pos := s.where(&quote)
		return false, errors.New("UnclosedQuote", "Unclosed quote"+at, "pos", pos)
	}
	return false, nil
}

// literalTokens reads an unquoted value.
func literalTokens(s *tokenScanner, allowBrackets bool) bool {
	start := s.pos
	brackets := map[BracketType]int{}

loop:
	for s.readable() {
		tok := s.peek()
		if brackets[BracketExpression] != 0 {
			// Inside an expression everything is a part of it
			if isBracket(tok, BracketExpression, nil) {
				if tok.Open {
					brackets[tok.Context]++
				} else {
					brackets[tok.Context]--
				}
			}
		} else if isQuote(tok) || isOperator(tok, "") || isWhiteSpaceToken(tok) || isRepeater(tok) {
			break loop
		} else if isBracket(tok, "", nil) {
			if !allowBrackets {
				break loop
			}
			if tok.Open {
				brackets[tok.Context]++
			} else if brackets[tok.Context] == 0 {
				// An unmatched closing bracket belongs to the parent
				break loop
			} else {
				brackets[tok.Context]--
			}
		}
		s.pos++
	}

	if start != s.pos {
		s.start = start
		return true
	}
	return false
}

// elementName reads the name of an element.
func elementName(s *tokenScanner, opts ParserOptions) bool {
	start := s.pos

	if opts.JSX && s.consume(isCapitalizedLiteral) {
		// The names of React components, like `Foo.Bar.Baz`
		for s.readable() {
			pos := s.pos
			if !s.consume(isClassNameOperator) || !s.consume(isCapitalizedLiteral) {
				s.pos = pos
				break
			}
		}
	}

	for s.readable() && s.consume(isElementNameToken) {
	}

	if s.pos != start {
		s.start = start
		return true
	}
	return false
}

// text reads `{text}`.
func text(s *tokenScanner) bool {
	start := s.pos
	if s.consume(isTextStart) {
		brackets := 0
		for s.readable() {
			tok := s.next()
			if isBracket(tok, BracketExpression, nil) {
				if tok.Open {
					brackets++
				} else if brackets == 0 {
					break
				} else {
					brackets--
				}
			}
		}
		s.start = start
		return true
	}
	return false
}

// getText gives the tokens of the text just read, without the braces.
func getText(s *tokenScanner) []Token {
	from, to := s.start, s.pos
	if isBracket(tokenAt(s, from), BracketExpression, true) {
		from++
	}
	if isBracket(tokenAt(s, to-1), BracketExpression, false) {
		to--
	}
	return s.slice(from, to)
}

func tokenAt(s *tokenScanner, i int) *Token {
	if i >= 0 && i < len(s.tokens) {
		return &s.tokens[i]
	}
	return nil
}

// isBracket tells if the token is a bracket of the context (any when empty) and the openness (any when nil).
func isBracket(tok *Token, context BracketType, open any) bool {
	if tok == nil || tok.Type != TokenBracket {
		return false
	}
	if context != "" && tok.Context != context {
		return false
	}
	if o, ok := open.(bool); ok && tok.Open != o {
		return false
	}
	return true
}

func isOperator(tok *Token, typ OperatorType) bool {
	return tok != nil && tok.Type == TokenOperator && (typ == "" || tok.Operator == typ)
}

func isQuote(tok *Token) bool { return tok != nil && tok.Type == TokenQuote }

func isQuoteSingle(tok *Token, single bool) bool { return isQuote(tok) && tok.Single == single }

func isWhiteSpaceToken(tok *Token) bool { return tok != nil && tok.Type == TokenWhiteSpace }

func isEquals(tok *Token) bool { return isOperator(tok, OpEqual) }

func isRepeater(tok *Token) bool { return tok != nil && tok.Type == TokenRepeater }

func isCapitalizedLiteral(tok *Token) bool {
	return tok.Type == TokenLiteral && len(tok.Value) > 0 && tok.Value[0] >= 'A' && tok.Value[0] <= 'Z'
}

func isElementNameToken(tok *Token) bool {
	return tok.Type == TokenLiteral || tok.Type == TokenRepeaterNumber || tok.Type == TokenRepeaterPlaceholder
}

func isClassNameOperator(tok *Token) bool { return isOperator(tok, OpClass) }
func isAttributeSetStart(tok *Token) bool { return isBracket(tok, BracketAttribute, true) }
func isAttributeSetEnd(tok *Token) bool   { return isBracket(tok, BracketAttribute, false) }
func isTextStart(tok *Token) bool         { return isBracket(tok, BracketExpression, true) }
func isGroupStart(tok *Token) bool        { return isBracket(tok, BracketGroup, true) }
func isChildOperator(tok *Token) bool     { return isOperator(tok, OpChild) }
func isSiblingOperator(tok *Token) bool   { return isOperator(tok, OpSibling) }
func isClimbOperator(tok *Token) bool     { return isOperator(tok, OpClimb) }
func isCloseOperator(tok *Token) bool     { return isOperator(tok, OpClose) }
func isEmptyElement(elem *Statement) bool {
	return elem.Name == nil && elem.Value == nil && elem.Attributes == nil
}
