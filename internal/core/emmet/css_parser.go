package emmet

import "github.com/rm4n0s/errors"

// Ported from Emmet: packages/css-abbreviation/src/parser/index.ts,
// packages/css-abbreviation/src/parser/TokenScanner.ts and packages/css-abbreviation/src/index.ts

// CSSValue is one value of a property: the tokens between two commas (`10px solid`).
type CSSValue struct {
	// Value are the tokens of the value: literals, numbers, colors, strings, fields, custom
	// properties and function calls.
	Value []*CSSToken
}

// CSSProperty is a property of a stylesheet abbreviation: `p10!` is the name p, the value 10 and
// important. The name is empty for a value written alone (`10`, a snippet or a value).
type CSSProperty struct {
	// Name of the property; empty for none.
	Name string
	// Value are the values of the property, separated by commas in the output.
	Value []*CSSValue
	// Important is a property with `!`.
	Important bool

	snippet *cssSnippet // the snippet the property matched
}

// ParseCSS reads a stylesheet abbreviation as a list of properties. With value the abbreviation is
// the value of a property: there is no name.
func ParseCSS(abbr string, value bool) ([]*CSSProperty, error) {
	tokens, err := TokenizeCSS(abbr, value)
	if err != nil {
		return nil, err
	}
	return ParseCSSTokens(tokens, value)
}

// ParseCSSTokens reads the tokens of a stylesheet abbreviation as properties.
func ParseCSSTokens(tokens []*CSSToken, value bool) ([]*CSSProperty, error) {
	s := &cssTokenScanner{tokens: tokens}
	var result []*CSSProperty
	for s.readable() {
		prop, err := s.consumeProperty(value)
		if err != nil {
			return nil, err
		}
		if prop != nil {
			result = append(result, prop)
		} else if !s.consume(cssIsSibling) {
			at, pos := s.where(nil)
			return nil, errors.New("CSSUnexpectedToken", "Unexpected token"+at, "pos", pos)
		}
	}
	return result, nil
}

// cssTokenScanner reads the tokens.
type cssTokenScanner struct {
	tokens []*CSSToken
	pos    int
}

func (s *cssTokenScanner) readable() bool { return s.pos < len(s.tokens) }

func (s *cssTokenScanner) peek() *CSSToken {
	if s.pos < len(s.tokens) {
		return s.tokens[s.pos]
	}
	return nil
}

func (s *cssTokenScanner) consume(test func(*CSSToken) bool) bool {
	if t := s.peek(); t != nil && test(t) {
		s.pos++
		return true
	}
	return false
}

// where gives the place of an error at a token (the one at the position by default): the text " at N" to
// add to the message, and the offset, or "" and -1 when there is no token.
func (s *cssTokenScanner) where(tok *CSSToken) (at string, pos int) {
	if tok == nil {
		tok = s.peek()
	}
	if tok == nil {
		return "", -1
	}
	return " at " + itoa(tok.Start), tok.Start
}

// consumeProperty reads one property, or nothing (nil).
func (s *cssTokenScanner) consumeProperty(valueMode bool) (*CSSProperty, error) {
	name := ""
	important := false
	var value []*CSSValue
	tok := s.peek()

	if !valueMode && tok != nil && tok.Type == CSSLiteral && !s.isFunctionStart() {
		s.pos++
		name = tok.Value
		// Any delimiter of values after the name of the property
		s.consume(cssIsValueDelimiter)
	}

	// Skip the white space right after the name
	if valueMode {
		s.consume(cssIsWhiteSpace)
	}

	for s.readable() {
		if s.consume(cssIsImportant) {
			important = true
			continue
		}
		fragment, err := s.consumeValue(valueMode, 0)
		if err != nil {
			return nil, err
		}
		if fragment != nil {
			value = append(value, fragment)
		} else if !s.consume(cssIsArgumentDelimiter) {
			break
		}
	}

	if name != "" || len(value) > 0 || important {
		return &CSSProperty{Name: name, Value: value, Important: important}, nil
	}
	return nil, nil
}

// consumeValue reads one value: all the tokens up to a comma.
func (s *cssTokenScanner) consumeValue(inArgument bool, depth int) (*CSSValue, error) {
	var result []*CSSToken
	for s.readable() {
		tok := s.peek()
		switch {
		case cssIsValue(tok):
			s.pos++
			if tok.Type == CSSLiteral {
				args, ok, err := s.consumeArguments(depth)
				if err != nil {
					return nil, err
				}
				if ok {
					result = append(result, &CSSToken{Type: CSSFunction, Name: tok.Value, Arguments: args, noPos: true})
					continue
				}
			}
			result = append(result, tok)
		case cssIsValueDelimiter(tok) || (inArgument && tok.Type == CSSWhiteSpace):
			s.pos++
		default:
			return finishValue(result), nil
		}
	}
	return finishValue(result), nil
}

func finishValue(result []*CSSToken) *CSSValue {
	if len(result) == 0 {
		return nil
	}
	return &CSSValue{Value: result}
}

// consumeArguments reads the arguments of a function, if the next token opens them.
func (s *cssTokenScanner) consumeArguments(depth int) ([]*CSSValue, bool, error) {
	if !s.consume(cssIsOpenBracket) {
		return nil, false, nil
	}
	if depth >= maxCSSNesting {
		at, pos := s.where(s.tokens[s.pos-1])
		return nil, false, errors.New("CSSNestingTooDeep", "Abbreviation is nested too deep"+at, "pos", pos)
	}
	args := []*CSSValue{}
	for s.readable() && !s.consume(cssIsCloseBracket) {
		value, err := s.consumeValue(true, depth+1)
		if err != nil {
			return nil, false, err
		}
		if value != nil {
			args = append(args, value)
		} else if !s.consume(cssIsWhiteSpace) && !s.consume(cssIsArgumentDelimiter) {
			at, pos := s.where(nil)
			return nil, false, errors.New("CSSUnexpectedToken", "Unexpected token"+at, "pos", pos)
		}
	}
	return args, true, nil
}

func (s *cssTokenScanner) isFunctionStart() bool {
	if s.pos+1 >= len(s.tokens) {
		return false
	}
	t1, t2 := s.tokens[s.pos], s.tokens[s.pos+1]
	return t1.Type == CSSLiteral && t2.Type == CSSBracket
}

func cssIsOpenBracket(t *CSSToken) bool  { return t.Type == CSSBracket && t.Open }
func cssIsCloseBracket(t *CSSToken) bool { return t.Type == CSSBracket && !t.Open }
func cssIsWhiteSpace(t *CSSToken) bool   { return t.Type == CSSWhiteSpace }
func cssIsOperator(t *CSSToken, op CSSOperatorType) bool {
	return t.Type == CSSOperator && t.Operator == op
}
func cssIsSibling(t *CSSToken) bool           { return cssIsOperator(t, CSSOpSibling) }
func cssIsArgumentDelimiter(t *CSSToken) bool { return cssIsOperator(t, CSSOpArgumentDelimiter) }
func cssIsImportant(t *CSSToken) bool         { return cssIsOperator(t, CSSOpImportant) }

func cssIsValue(t *CSSToken) bool {
	switch t.Type {
	case CSSString, CSSColor, CSSNumber, CSSLiteral, CSSField, CSSCustomProperty:
		return true
	}
	return false
}

func cssIsValueDelimiter(t *CSSToken) bool {
	return cssIsOperator(t, CSSOpPropertyDelimiter) || cssIsOperator(t, CSSOpValueDelimiter)
}
