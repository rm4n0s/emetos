package emmet

import (
	"strconv"

	"github.com/rm4n0s/errors"
)

// Ported from Emmet: packages/css-abbreviation/src/tokenizer/index.ts,
// packages/css-abbreviation/src/tokenizer/tokens.ts and packages/css-abbreviation/src/tokenizer/utils.ts
//
// The tokenizer works on bytes, like the markup one: every character with a meaning in a stylesheet
// abbreviation is ASCII.

// CSSTokenType is the kind of a token of a stylesheet abbreviation.
type CSSTokenType int

// The kinds of tokens. CSSFunction is not made by the tokenizer: the parser makes it from a
// literal and the arguments after it.
const (
	// CSSLiteral is a name or a keyword: Value is its text.
	CSSLiteral CSSTokenType = iota + 1
	// CSSWhiteSpace is white space.
	CSSWhiteSpace
	// CSSBracket is ( or ): Open tells which.
	CSSBracket
	// CSSOperator is one of + ! , - and : (Operator tells which).
	CSSOperator
	// CSSNumber is a number with a unit: Number, RawValue and Unit.
	CSSNumber
	// CSSColor is a color: R, G, B, A (and Raw, the text after the #).
	CSSColor
	// CSSString is a quoted string: Value is its content, Single tells the quote.
	CSSString
	// CSSCustomProperty is a name that starts with two dashes (--foo-bar): Value is its text.
	CSSCustomProperty
	// CSSField is ${1:name} or ${name}: Index, HasIndex and Name.
	CSSField
	// CSSFunction is a call, name(arguments): Name and Arguments.
	CSSFunction
)

// CSSOperatorType is the kind of an operator token.
type CSSOperatorType string

// The operators of a stylesheet abbreviation.
const (
	// CSSOpSibling separates properties.
	CSSOpSibling CSSOperatorType = "+"
	// CSSOpImportant is `!important`.
	CSSOpImportant CSSOperatorType = "!"
	// CSSOpArgumentDelimiter separates the arguments of a function and the values of a property.
	CSSOpArgumentDelimiter CSSOperatorType = ","
	// CSSOpValueDelimiter separates the values of a property.
	CSSOpValueDelimiter CSSOperatorType = "-"
	// CSSOpPropertyDelimiter separates the name of a property from its value.
	CSSOpPropertyDelimiter CSSOperatorType = ":"
)

// CSSToken is a piece of a stylesheet abbreviation, and the value the parser makes of it. Which
// fields are used depends on Type; the others are zero.
type CSSToken struct {
	// Type is the kind of token.
	Type CSSTokenType
	// Start and End are the byte offsets of the token in the abbreviation (zero for a token
	// that the expansion makes, like a field of a snippet).
	Start, End int

	// Value is the text of a literal or a custom property, or the content of a string.
	Value string
	// Operator is the kind of an operator.
	Operator CSSOperatorType
	// Open tells if a bracket is (.
	Open bool
	// Number is the value of a number.
	Number float64
	// RawValue is the text of a number.
	RawValue string
	// Unit is the unit after a number: `px`, `%`, or empty.
	Unit string
	// R, G and B are the red, green and blue of a color, 0 to 255.
	R, G, B int
	// A is the opacity of a color, 0 to 1.
	A float64
	// Raw is the text of a color after the #.
	Raw string
	// Single is a string in single quotes.
	Single bool
	// Index is the number of a field, if HasIndex.
	Index int
	// HasIndex tells that the field has a number (`${1}`) and not a name (`${name}`).
	HasIndex bool
	// Name is the placeholder of a field, the name of a variable, or the name of a function.
	Name string
	// Arguments are the arguments of a function.
	Arguments []*CSSValue

	// noPos is set for a token that Emmet makes and that has no place in the abbreviation (Start
	// and End mean nothing): the output does not tell if two tokens are close by them.
	noPos bool
}

// maxCSSNesting limits how deep the brackets of a function call go in a stylesheet abbreviation.
const maxCSSNesting = maxNesting

// TokenizeCSS splits a stylesheet abbreviation into tokens. With isValue the abbreviation is the
// value of a property (a snippet, or the text typed after `prop:`), and names may have digits.
func TokenizeCSS(abbr string, isValue bool) ([]*CSSToken, error) {
	s := newScanner(abbr)
	var tokens []*CSSToken
	brackets := 0

	for !s.eof() {
		tok, err := cssGetToken(s, brackets == 0 && !isValue)
		if err != nil {
			return nil, err
		}
		if tok == nil {
			return nil, errors.New("CSSUnexpectedCharacter", "Unexpected character"+" at "+itoa(s.pos+1), "pos", s.pos, "abbreviation", s.str)
		}

		if tok.Type == CSSBracket {
			if brackets == 0 && tok.Open {
				tokens = cssMergeTokens(s, tokens)
			}
			if tok.Open {
				brackets++
			} else {
				brackets--
			}
			if brackets < 0 {
				return nil, errors.New("CSSUnexpectedBracket", "Unexpected bracket"+" at "+itoa(tok.Start+1), "pos", tok.Start, "abbreviation", s.str)
			}
		}

		tokens = append(tokens, tok)

		// Forcibly consume the next operator after a number with no unit or a color: a dash must
		// be used as a delimiter of values
		if cssShouldConsumeDashAfter(tok) {
			if op := cssOperator(s); op != nil {
				tokens = append(tokens, op)
			}
		}
	}
	return tokens, nil
}

// cssGetToken reads the next token, or nil if there is none that fits.
func cssGetToken(s *scanner, short bool) (*CSSToken, error) {
	if tok, err := cssField(s); tok != nil || err != nil {
		return tok, err
	}
	if tok := cssCustomProperty(s); tok != nil {
		return tok, nil
	}
	if tok := cssNumberValue(s); tok != nil {
		return tok, nil
	}
	if tok := cssColorValue(s); tok != nil {
		return tok, nil
	}
	if tok := cssStringValue(s); tok != nil {
		return tok, nil
	}
	if tok := cssBracket(s); tok != nil {
		return tok, nil
	}
	if tok := cssOperator(s); tok != nil {
		return tok, nil
	}
	if tok := cssWhiteSpace(s); tok != nil {
		return tok, nil
	}
	return cssLiteral(s, short), nil
}

func cssField(s *scanner) (*CSSToken, error) {
	start := s.pos
	if s.eat('$') && s.eat('{') {
		tok := &CSSToken{Type: CSSField, Start: start}
		digits := s.pos
		if s.eatWhile(isNumber) {
			// A field
			tok.Index = parseCount(s.str[digits:s.pos])
			tok.HasIndex = true
			if s.eat(':') {
				name, err := cssPlaceholder(s)
				if err != nil {
					return nil, err
				}
				tok.Name = name
			}
		} else if isAlpha(s.peek()) {
			// A variable
			name, err := cssPlaceholder(s)
			if err != nil {
				return nil, err
			}
			tok.Name = name
		}

		if s.eat('}') {
			tok.End = s.pos
			return tok, nil
		}
		return nil, errors.New("CSSFieldUnclosed", "Expecting }"+" at "+itoa(s.pos+1), "pos", s.pos, "abbreviation", s.str)
	}

	// There is no field here: back to the start
	s.pos = start
	return nil, nil
}

// cssPlaceholder reads the placeholder of a field: what is after the `:`, up to the closing brace.
func cssPlaceholder(s *scanner) (string, error) {
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
		return "", errors.New("CSSPlaceholderUnclosed", "Expecting }"+" at "+itoa(s.pos+1), "pos", s.pos, "abbreviation", s.str)
	}
	return s.str[start:s.pos], nil
}

// cssLiteral reads a literal. The short notation (names of letters only, for the keywords at the
// start of the abbreviation) has no digits and no dashes; the full one has them.
func cssLiteral(s *scanner, short bool) *CSSToken {
	start := s.pos

	if cssIsIdentPrefix(s.peek()) {
		s.pos++
		// A variable of SCSS or LESS. A bit of a hack in Emmet: if the abbreviation starts with the
		// prefix, only letters are read, to allow variables inside
		if start != 0 {
			s.eatWhile(cssIsKeyword)
		} else {
			s.eatWhile(cssIsLiteral)
		}
	} else if isAlphaWord(s.peek()) {
		s.pos++
		if short {
			s.eatWhile(cssIsLiteral)
		} else {
			s.eatWhile(cssIsKeyword)
		}
	} else {
		// Dots only at the start of a literal
		s.eat('.')
		s.eatWhile(cssIsLiteral)
	}

	if start != s.pos {
		return &CSSToken{Type: CSSLiteral, Value: s.str[start:s.pos], Start: start, End: s.pos}
	}
	return nil
}

// cssNumberValue reads a number with a unit (a % or a word).
func cssNumberValue(s *scanner) *CSSToken {
	start := s.pos
	if !cssConsumeNumber(s) {
		return nil
	}
	raw := s.str[start:s.pos]
	unitStart := s.pos
	if !s.eat('%') {
		s.eatWhile(isAlphaWord)
	}
	return &CSSToken{
		Type: CSSNumber, Number: parseNumber(raw), RawValue: raw, Unit: s.str[unitStart:s.pos],
		Start: start, End: s.pos,
	}
}

// parseNumber reads the text of a number as JavaScript's Number() does: a number too large is infinite.
func parseNumber(raw string) float64 {
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0
	}
	return v
}

// cssStringValue reads a string in quotes. A string that is not closed goes to the end.
func cssStringValue(s *scanner) *CSSToken {
	ch := s.peek()
	start := s.pos
	if !isQuoteChar(ch) {
		return nil
	}
	s.pos++
	finished := false
	for !s.eof() {
		if s.peek() == ch {
			s.pos++
			finished = true
			break
		}
		s.pos++
	}
	end := s.pos
	if finished {
		end--
	}
	return &CSSToken{Type: CSSString, Value: s.str[start+1 : end], Single: ch == '\'', Start: start, End: s.pos}
}

// cssColorValue reads a color:
//
//	#abc   -> #aabbcc
//	#0     -> #000000
//	#fff.5 -> rgba(255, 255, 255, 0.5)
//	#t     -> transparent
func cssColorValue(s *scanner) *CSSToken {
	start := s.pos
	if !s.eat('#') {
		return nil
	}
	valueStart := s.pos
	color, alpha := "", ""
	switch {
	case s.eatWhile(cssIsHex):
		color = s.str[valueStart:s.pos]
		alpha = cssColorAlpha(s)
	case s.eat('t'):
		color = "0"
		alpha = cssColorAlpha(s)
		if alpha == "" {
			alpha = "0"
		}
	default:
		alpha = cssColorAlpha(s)
	}

	if color == "" && alpha == "" && !s.eof() {
		// A # and no value: it is a literal
		return &CSSToken{Type: CSSLiteral, Value: s.str[start:s.pos], Start: start, End: s.pos}
	}
	r, g, b, a := cssParseColor(color, alpha)
	return &CSSToken{Type: CSSColor, R: r, G: g, B: b, A: a, Raw: s.str[start+1 : s.pos], Start: start, End: s.pos}
}

// cssColorAlpha reads the opacity of a color: `.1`.
func cssColorAlpha(s *scanner) string {
	start := s.pos
	if s.eat('.') {
		if s.eatWhile(isNumber) {
			return s.str[start:s.pos]
		}
		return "1"
	}
	return ""
}

func cssWhiteSpace(s *scanner) *CSSToken {
	start := s.pos
	for {
		n := s.spaceLen(s.pos)
		if n == 0 {
			break
		}
		s.pos += n
	}
	if s.pos == start {
		return nil
	}
	return &CSSToken{Type: CSSWhiteSpace, Start: start, End: s.pos}
}

// cssCustomProperty reads a custom property: --foo-bar.
func cssCustomProperty(s *scanner) *CSSToken {
	start := s.pos
	if s.eat('-') {
		if s.eat('-') {
			s.eatWhile(cssIsKeyword)
			return &CSSToken{Type: CSSCustomProperty, Value: s.str[start:s.pos], Start: start, End: s.pos}
		}
	}
	s.pos = start
	return nil
}

func cssBracket(s *scanner) *CSSToken {
	ch := s.peek()
	if ch != '(' && ch != ')' {
		return nil
	}
	tok := &CSSToken{Type: CSSBracket, Open: ch == '(', Start: s.pos}
	s.pos++
	tok.End = s.pos
	return tok
}

func cssOperator(s *scanner) *CSSToken {
	var op CSSOperatorType
	switch s.peek() {
	case '+':
		op = CSSOpSibling
	case '!':
		op = CSSOpImportant
	case ',':
		op = CSSOpArgumentDelimiter
	case ':':
		op = CSSOpPropertyDelimiter
	case '-':
		op = CSSOpValueDelimiter
	default:
		return nil
	}
	tok := &CSSToken{Type: CSSOperator, Operator: op, Start: s.pos}
	s.pos++
	tok.End = s.pos
	return tok
}

// cssConsumeNumber reads a number: an optional dash, digits, a dot and digits (`1.` is a fraction).
func cssConsumeNumber(s *scanner) bool {
	start := s.pos
	s.eat('-')
	afterNegative := s.pos

	hasDecimal := s.eatWhile(isNumber)

	prev := s.pos
	if s.eat('.') {
		hasFloat := s.eatWhile(isNumber)
		if !hasDecimal && !hasFloat {
			// A lone dot
			s.pos = prev
		}
	}

	// Only a dash was read: it is not a number
	if s.pos == afterNegative {
		s.pos = start
	}
	return s.pos != start
}

func cssIsIdentPrefix(code int) bool { return code == '@' || code == '$' }

// cssIsHex tells if the code is a digit of a hexadecimal number.
func cssIsHex(code int) bool { return isNumber(code) || (code|32 >= 'a' && code|32 <= 'f') }

func cssIsKeyword(code int) bool { return isAlphaNumericWord(code) || code == '-' }

func cssIsLiteral(code int) bool { return isAlphaWord(code) || code == '%' || code == '/' }

// cssParseColor reads the text of a color (the digits and the opacity) as its parts.
func cssParseColor(value, alpha string) (r, g, b int, a float64) {
	a = 1
	if alpha != "" {
		a = parseNumber(alpha)
	}
	rs, gs, bs := "0", "0", "0"
	switch len(value) {
	case 0:
	case 1:
		rs = value + value
		gs, bs = rs, rs
	case 2:
		rs, gs, bs = value, value, value
	case 3:
		rs = value[0:1] + value[0:1]
		gs = value[1:2] + value[1:2]
		bs = value[2:3] + value[2:3]
	default:
		value += value
		rs, gs, bs = value[0:2], value[2:4], value[4:6]
	}
	return hexByte(rs), hexByte(gs), hexByte(bs), a
}

// hexByte reads one or two hexadecimal digits.
func hexByte(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
			n = n*16 + int(c-'0')
		default:
			n = n*16 + int(c|32-'a') + 10
		}
	}
	return n
}

// cssShouldConsumeDashAfter tells if the operator after the token is read at once: the user must
// separate numbers with no unit and colors with a dash.
func cssShouldConsumeDashAfter(tok *CSSToken) bool {
	return tok.Type == CSSColor || (tok.Type == CSSNumber && tok.Unit == "")
}

// cssMergeTokens merges the last tokens that are literals and numbers into one literal. A name like
// `scale3d(` is read as a literal and a number (`scale` and `3d`), which is a valid abbreviation but not what is wanted.
func cssMergeTokens(s *scanner, tokens []*CSSToken) []*CSSToken {
	start, end := 0, 0
	for len(tokens) > 0 {
		tok := tokens[len(tokens)-1]
		if tok.Type != CSSLiteral && tok.Type != CSSNumber {
			break
		}
		start = tok.Start
		if end == 0 {
			end = tok.End
		}
		tokens = tokens[:len(tokens)-1]
	}
	if start != end {
		tokens = append(tokens, &CSSToken{Type: CSSLiteral, Value: s.str[start:end], Start: start, End: end})
	}
	return tokens
}
