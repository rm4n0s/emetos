package emmet

import (
	"unicode/utf8"

	"github.com/rm4n0s/errors"
)

// Ported from Emmet: packages/abbreviation/src/tokenizer/index.ts,
// packages/abbreviation/src/tokenizer/tokens.ts and packages/abbreviation/src/tokenizer/utils.ts

// TokenType is the kind of a token of an abbreviation.
type TokenType int

// The kinds of tokens.
const (
	// TokenLiteral is a name or a value: Value is its text.
	TokenLiteral TokenType = iota + 1
	// TokenWhiteSpace is white space: Value is its text.
	TokenWhiteSpace
	// TokenQuote is a ' or a ": Single tells which.
	TokenQuote
	// TokenBracket is one of ( ) [ ] { }: Open and Context tell which.
	TokenBracket
	// TokenOperator is one of > + ^ . # / =: Operator tells which.
	TokenOperator
	// TokenRepeater is * with an optional count: Count, Implicit.
	TokenRepeater
	// TokenRepeaterNumber is $ (one or more) with optional modifiers: Size, Reverse, Base, Parent.
	TokenRepeaterNumber
	// TokenRepeaterPlaceholder is $#.
	TokenRepeaterPlaceholder
	// TokenField is ${1:name} or a ${variable}: Index, HasIndex, Name.
	TokenField
)

// OperatorType is the kind of an operator token.
type OperatorType string

// The operators.
const (
	OpChild   OperatorType = "child"
	OpSibling OperatorType = "sibling"
	OpClimb   OperatorType = "climb"
	OpClass   OperatorType = "class"
	OpID      OperatorType = "id"
	OpClose   OperatorType = "close"
	OpEqual   OperatorType = "equal"
)

// BracketType is the kind of a bracket token.
type BracketType string

// The brackets: parentheses group, square brackets hold attributes, curly ones an expression (text).
const (
	BracketGroup      BracketType = "group"
	BracketAttribute  BracketType = "attribute"
	BracketExpression BracketType = "expression"
)

// Token is a piece of an abbreviation. Which fields are used depends on Type; the others are zero.
type Token struct {
	// Type is the kind of token.
	Type TokenType
	// Start and End are the byte offsets of the token in the abbreviation.
	Start, End int

	// Value is the text of a literal or of white space.
	Value string
	// Operator is the kind of an operator.
	Operator OperatorType
	// Open tells if a bracket opens (and does not close).
	Open bool
	// Context is the kind of bracket.
	Context BracketType
	// Single is a quote that is ' (and not ").
	Single bool
	// Count and Implicit describe a repeater: how many times, or (Implicit) as many times as
	// there are lines of text. Position is the place of the item in a repeater that was
	// unrolled (zero in a token).
	Count int
	// Implicit is a repeater with no count.
	Implicit bool
	// Size, Reverse, Base and Parent describe a repeater number ($, $$, $@-3, $@^).
	Size int
	// Reverse counts down.
	Reverse bool
	// Base is the number to start from.
	Base int
	// Parent is how many repeaters up the numbering follows (the number of ^).
	Parent int
	// Index, HasIndex and Name describe a field; a field with no index is a variable.
	Index int
	// HasIndex is set for a field; it is not for a variable.
	HasIndex bool
	// Name is the placeholder of a field, or the name of a variable.
	Name string
}

// tokenCtx is what the tokenizer knows about the place it is in.
type tokenCtx struct {
	group, attribute, expression, quote int
}

func (c *tokenCtx) brackets(t BracketType) *int {
	switch t {
	case BracketGroup:
		return &c.group
	case BracketAttribute:
		return &c.attribute
	}
	return &c.expression
}

// Tokenize cuts an abbreviation into tokens.
func Tokenize(source string) ([]Token, error) {
	s := newScanner(source)
	var result []Token
	var ctx tokenCtx
	for !s.eof() {
		ch := s.peek()
		tok, ok, err := getToken(s, &ctx)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, errors.New("UnexpectedCharacter", "Unexpected character"+" at "+itoa(s.pos+1), "pos", s.pos, "abbreviation", s.str)
		}
		result = append(result, tok)
		switch tok.Type {
		case TokenQuote:
			if ch == ctx.quote {
				ctx.quote = 0
			} else {
				ctx.quote = ch
			}
		case TokenBracket:
			n := ctx.brackets(tok.Context)
			if tok.Open {
				*n++
			} else {
				*n--
			}
		}
	}
	return result, nil
}

// getToken reads the next token. ok is false when there is none at the position.
func getToken(s *scanner, ctx *tokenCtx) (Token, bool, error) {
	if tok, ok, err := field(s, ctx); err != nil || ok {
		return tok, ok, err
	}
	for _, read := range []func() (Token, bool){
		func() (Token, bool) { return repeaterPlaceholder(s) },
		func() (Token, bool) { return repeaterNumber(s) },
		func() (Token, bool) { return repeater(s) },
		func() (Token, bool) { return whiteSpace(s) },
		func() (Token, bool) { return literal(s, ctx) },
		func() (Token, bool) { return operator(s) },
		func() (Token, bool) { return quote(s) },
		func() (Token, bool) { return bracket(s) },
	} {
		if tok, ok := read(); ok {
			return tok, true, nil
		}
	}
	return Token{}, false, nil
}

// escaped moves over a backslash and the character after it; it tells if there was a backslash.
func escaped(s *scanner) (string, bool) {
	if !s.eat('\\') {
		return "", false
	}
	start := s.pos
	if !s.eof() {
		_, size := s.runeAt(s.pos)
		s.pos += size
	}
	return s.str[start:s.pos], true
}

func literal(s *scanner, ctx *tokenCtx) (Token, bool) {
	start := s.pos
	expressionStart := ctx.expression
	var value []byte

	for !s.eof() {
		// An escaped sequence is consumed in every context
		if esc, ok := escaped(s); ok {
			value = append(value, esc...)
			continue
		}

		ch := s.peek()
		size := 1
		if ch >= utf8.RuneSelf {
			var r rune
			r, size = s.runeAt(s.pos)
			ch = int(r)
		}

		if ch == '/' && ctx.quote == 0 && ctx.expression == 0 && ctx.attribute == 0 {
			// A slash between numbers is a part of a class name
			if isNumber(s.at(s.pos-1)) && isNumber(s.at(s.pos+1)) {
				value = append(value, '/')
				s.pos++
				continue
			}
		}

		if ch == ctx.quote || ch == '$' || isAllowedOperator(ch, ctx) {
			// The matching quote; `$` has a meaning everywhere; and, depending on the context,
			// some characters are operators
			break
		}

		if expressionStart != 0 {
			// Nested expressions, e.g. span{{foo}}
			if ch == '{' {
				ctx.expression++
			} else if ch == '}' {
				if ctx.expression > expressionStart {
					ctx.expression--
				} else {
					break
				}
			}
		} else if ctx.quote == 0 {
			if ctx.attribute == 0 && !isElementName(ch) {
				break
			}
			if (s.spaceLen(s.pos) > 0 && ctx.expression == 0) || isAllowedRepeater(ch, ctx) || isQuoteChar(ch) || bracketType(ch) != "" {
				break
			}
		}

		value = append(value, s.str[s.pos:s.pos+size]...)
		s.pos += size
	}

	if start != s.pos {
		return Token{Type: TokenLiteral, Value: string(value), Start: start, End: s.pos}, true
	}
	return Token{}, false
}

func whiteSpace(s *scanner) (Token, bool) {
	start := s.pos
	for {
		n := s.spaceLen(s.pos)
		if n == 0 {
			break
		}
		s.pos += n
	}
	if s.pos != start {
		return Token{Type: TokenWhiteSpace, Start: start, End: s.pos, Value: s.str[start:s.pos]}, true
	}
	return Token{}, false
}

func quote(s *scanner) (Token, bool) {
	ch := s.peek()
	if isQuoteChar(ch) {
		start := s.pos
		s.pos++
		return Token{Type: TokenQuote, Single: ch == '\'', Start: start, End: s.pos}, true
	}
	return Token{}, false
}

func bracket(s *scanner) (Token, bool) {
	ch := s.peek()
	if ctx := bracketType(ch); ctx != "" {
		start := s.pos
		s.pos++
		return Token{Type: TokenBracket, Open: isOpenBracket(ch), Context: ctx, Start: start, End: s.pos}, true
	}
	return Token{}, false
}

func operator(s *scanner) (Token, bool) {
	if op := operatorType(s.peek()); op != "" {
		start := s.pos
		s.pos++
		return Token{Type: TokenOperator, Operator: op, Start: start, End: s.pos}, true
	}
	return Token{}, false
}

func repeater(s *scanner) (Token, bool) {
	start := s.pos
	if !s.eat('*') {
		return Token{}, false
	}
	digits := s.pos
	count := 1
	implicit := false
	if s.eatWhile(isNumber) {
		count = parseCount(s.str[digits:s.pos])
	} else {
		implicit = true
	}
	return Token{Type: TokenRepeater, Count: count, Implicit: implicit, Start: start, End: s.pos}, true
}

func repeaterPlaceholder(s *scanner) (Token, bool) {
	start := s.pos
	if s.eat('$') && s.eat('#') {
		return Token{Type: TokenRepeaterPlaceholder, Start: start, End: s.pos}, true
	}
	s.pos = start
	return Token{}, false
}

func repeaterNumber(s *scanner) (Token, bool) {
	start := s.pos
	if !s.eatWhile(func(c int) bool { return c == '$' }) {
		return Token{}, false
	}
	size := s.pos - start
	reverse := false
	base := 1
	parent := 0

	if s.eat('@') {
		// The numbering modifiers
		for s.eat('^') {
			parent++
		}
		reverse = s.eat('-')
		digits := s.pos
		if s.eatWhile(isNumber) {
			base = parseCount(s.str[digits:s.pos])
		}
	}
	return Token{Type: TokenRepeaterNumber, Size: size, Reverse: reverse, Base: base, Parent: parent, Start: start, End: s.pos}, true
}

func field(s *scanner, ctx *tokenCtx) (Token, bool, error) {
	start := s.pos
	// Fields are allowed inside expressions and attributes
	if (ctx.expression != 0 || ctx.attribute != 0) && s.eat('$') && s.eat('{') {
		index := 0
		hasIndex := false
		name := ""
		digits := s.pos

		if s.eatWhile(isNumber) {
			// A field
			index = parseCount(s.str[digits:s.pos])
			hasIndex = true
			if s.eat(':') {
				var err error
				name, err = consumeFieldPlaceholder(s)
				if err != nil {
					return Token{}, false, err
				}
			}
		} else if isAlpha(s.peek()) {
			// A variable
			var err error
			name, err = consumeFieldPlaceholder(s)
			if err != nil {
				return Token{}, false, err
			}
		}

		if s.eat('}') {
			return Token{Type: TokenField, Index: index, HasIndex: hasIndex, Name: name, Start: start, End: s.pos}, true, nil
		}
		return Token{}, false, errors.New("FieldUnclosed", "Expecting }"+" at "+itoa(s.pos+1), "pos", s.pos, "abbreviation", s.str)
	}

	// There is no field here: back to the start
	s.pos = start
	return Token{}, false, nil
}

// consumeFieldPlaceholder reads the placeholder of a field: what is after the `:`, up to the closing brace.
func consumeFieldPlaceholder(s *scanner) (string, error) {
	var stack []int
	start := s.pos
	for !s.eof() {
		if s.eat('{') {
			stack = append(stack, s.pos)
		} else if s.eat('}') {
			if len(stack) == 0 {
				s.pos--
				break
			}
			stack = stack[:len(stack)-1]
		} else {
			s.pos++
		}
	}
	if len(stack) > 0 {
		s.pos = stack[len(stack)-1]
		return "", errors.New("PlaceholderUnclosed", "Expecting }"+" at "+itoa(s.pos+1), "pos", s.pos, "abbreviation", s.str)
	}
	return s.str[start:s.pos], nil
}

// isAllowedOperator tells if the character is an operator in the context.
func isAllowedOperator(ch int, ctx *tokenCtx) bool {
	op := operatorType(ch)
	if op == "" || ctx.quote != 0 || ctx.expression != 0 {
		// No operators in quoted values or expressions
		return false
	}
	// In attributes, only `equal` is one
	return ctx.attribute == 0 || op == OpEqual
}

// isAllowedRepeater tells if the character can start a repeater in the context.
func isAllowedRepeater(ch int, ctx *tokenCtx) bool {
	return ch == '*' && ctx.attribute == 0 && ctx.expression == 0
}

func bracketType(ch int) BracketType {
	switch ch {
	case '(', ')':
		return BracketGroup
	case '[', ']':
		return BracketAttribute
	case '{', '}':
		return BracketExpression
	}
	return ""
}

func operatorType(ch int) OperatorType {
	switch ch {
	case '>':
		return OpChild
	case '+':
		return OpSibling
	case '^':
		return OpClimb
	case '.':
		return OpClass
	case '#':
		return OpID
	case '/':
		return OpClose
	case '=':
		return OpEqual
	}
	return ""
}

func isOpenBracket(ch int) bool { return ch == '{' || ch == '[' || ch == '(' }

// isElementName tells if the character can be in the name of an element.
func isElementName(ch int) bool {
	return isAlphaNumericWord(ch) || isUmlaut(ch) || ch == '-' || ch == ':' || ch == '!'
}
