package jsonc

import (
	"strconv"
	"strings"
)

// ParseErrorCode says what is wrong at a place of the text.
type ParseErrorCode int

// The errors the parser reports.
const (
	// InvalidSymbol is text that is no token.
	InvalidSymbol ParseErrorCode = iota + 1
	// InvalidNumberFormat is a number that is not one.
	InvalidNumberFormat
	// PropertyNameExpected is a missing or misplaced property name.
	PropertyNameExpected
	// ValueExpected is a missing value.
	ValueExpected
	// ColonExpected is a property name without a colon after it.
	ColonExpected
	// CommaExpected is a missing comma between values or properties.
	CommaExpected
	// CloseBraceExpected is an object that is not closed.
	CloseBraceExpected
	// CloseBracketExpected is an array that is not closed.
	CloseBracketExpected
	// EndOfFileExpected is text after the value.
	EndOfFileExpected
	// InvalidCommentToken is a comment where comments are not allowed.
	InvalidCommentToken
	// UnexpectedEndOfComment is a block comment that is not closed.
	UnexpectedEndOfComment
	// UnexpectedEndOfString is a string that is not closed.
	UnexpectedEndOfString
	// UnexpectedEndOfNumber is a number that stops after a dot or an exponent sign.
	UnexpectedEndOfNumber
	// InvalidUnicode is a \u escape without four hex digits.
	InvalidUnicode
	// InvalidEscapeCharacter is an escape that does not exist.
	InvalidEscapeCharacter
	// InvalidCharacter is a control character in a string.
	InvalidCharacter
)

var errorMessages = [...]string{
	"", "Invalid symbol", "Invalid number format", "Property name expected", "Value expected",
	"Colon expected", "Comma expected", "Closing brace expected", "Closing bracket expected",
	"End of file expected", "Invalid comment token", "Unexpected end of comment",
	"Unexpected end of string", "Unexpected end of number", "Invalid unicode",
	"Invalid escape character", "Invalid character",
}

// Message is a short description of the error, as VS Code words it (it leaves the
// last seven without one).
func (c ParseErrorCode) Message() string {
	if c < 1 || int(c) >= len(errorMessages) {
		return "ParseErrorCode(" + strconv.Itoa(int(c)) + ")"
	}
	return errorMessages[c]
}

// ParseError is an error at a place of the text, in bytes.
type ParseError struct {
	Code           ParseErrorCode
	Offset, Length int
}

// Message is the short description of the error's code.
func (e ParseError) Message() string { return e.Code.Message() }

// ParseOptions tune the parser; the zero value allows comments and trailing commas,
// as settings.json does.
type ParseOptions struct {
	// DisallowComments reports comments as errors (InvalidCommentToken).
	DisallowComments bool
	// DisallowTrailingComma reports a comma before } or ] as an error.
	DisallowTrailingComma bool
	// AllowEmptyContent accepts a text with nothing but trivia.
	AllowEmptyContent bool
}

// Visitor receives what the parser finds, in order. Every function may be nil.
// Offsets and lengths are those of the token that triggers the call.
type Visitor struct {
	// ObjectBegin is called at {.
	ObjectBegin func(offset, length int)
	// ObjectProperty is called at the name of a property.
	ObjectProperty func(name string, offset, length int)
	// ObjectEnd is called at }; at the end of the text when the object is not closed.
	ObjectEnd func(offset, length int)
	// ArrayBegin is called at [.
	ArrayBegin func(offset, length int)
	// ArrayEnd is called at ]; at the end of the text when the array is not closed.
	ArrayEnd func(offset, length int)
	// LiteralValue is called for a string (string), number (float64), true, false
	// (bool) and null (nil).
	LiteralValue func(value any, offset, length int)
	// Separator is called at a comma or a colon.
	Separator func(char string, offset, length int)
	// Comment is called at a comment, when comments are allowed.
	Comment func(offset, length int)
	// Error is called at every error.
	Error func(code ParseErrorCode, offset, length int)
}

// stopVisit is what StopVisit panics with and Visit recovers.
type stopVisit struct{}

// StopVisit ends the visit in progress; call it from a function of a Visitor.
func StopVisit() { panic(stopVisit{}) }

// visitor is the state of a Visit.
type visitor struct {
	sc   *Scanner
	v    *Visitor
	opts ParseOptions
}

// Visit parses text and calls the functions of v for each object, array and literal it
// reaches. Whatever the text is, it goes on after an error and reports it.
//
// Ported from VS Code: src/vs/base/common/json.ts (visit).
func Visit(text string, v *Visitor, opts ParseOptions) {
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(stopVisit); !ok {
				panic(r)
			}
		}
	}()
	p := &visitor{sc: NewScanner(text, false), v: v, opts: opts}
	p.document()
}

func (p *visitor) pos() (int, int) { return p.sc.Offset(), p.sc.Length() }

func (p *visitor) objectBegin() {
	if p.v.ObjectBegin != nil {
		p.v.ObjectBegin(p.pos())
	}
}

func (p *visitor) objectEnd() {
	if p.v.ObjectEnd != nil {
		p.v.ObjectEnd(p.pos())
	}
}

func (p *visitor) arrayBegin() {
	if p.v.ArrayBegin != nil {
		p.v.ArrayBegin(p.pos())
	}
}

func (p *visitor) arrayEnd() {
	if p.v.ArrayEnd != nil {
		p.v.ArrayEnd(p.pos())
	}
}

func (p *visitor) literal(value any) {
	if p.v.LiteralValue != nil {
		o, l := p.pos()
		p.v.LiteralValue(value, o, l)
	}
}

func (p *visitor) separator(char string) {
	if p.v.Separator != nil {
		o, l := p.pos()
		p.v.Separator(char, o, l)
	}
}

func (p *visitor) property(name string) {
	if p.v.ObjectProperty != nil {
		o, l := p.pos()
		p.v.ObjectProperty(name, o, l)
	}
}

func (p *visitor) comment() {
	if p.v.Comment != nil {
		p.v.Comment(p.pos())
	}
}

// scanNext reads the next token that is not trivia, reporting the errors of the
// tokens it passes.
func (p *visitor) scanNext() SyntaxKind {
	for {
		token := p.sc.Scan()
		switch p.sc.Error() {
		case ScanInvalidUnicode:
			p.fail(InvalidUnicode, nil, nil)
		case ScanInvalidEscapeCharacter:
			p.fail(InvalidEscapeCharacter, nil, nil)
		case ScanUnexpectedEndOfNumber:
			p.fail(UnexpectedEndOfNumber, nil, nil)
		case ScanUnexpectedEndOfComment:
			if !p.opts.DisallowComments {
				p.fail(UnexpectedEndOfComment, nil, nil)
			}
		case ScanUnexpectedEndOfString:
			p.fail(UnexpectedEndOfString, nil, nil)
		case ScanInvalidCharacter:
			p.fail(InvalidCharacter, nil, nil)
		}
		switch token {
		case LineCommentTrivia, BlockCommentTrivia:
			if p.opts.DisallowComments {
				p.fail(InvalidCommentToken, nil, nil)
			} else {
				p.comment()
			}
		case Unknown:
			p.fail(InvalidSymbol, nil, nil)
		case Trivia, LineBreakTrivia:
		default:
			return token
		}
	}
}

// fail reports an error and then skips tokens: up to and including the first of
// skipUntilAfter, or up to the first of skipUntil.
func (p *visitor) fail(code ParseErrorCode, skipUntilAfter, skipUntil []SyntaxKind) {
	if p.v.Error != nil {
		o, l := p.pos()
		p.v.Error(code, o, l)
	}
	if len(skipUntilAfter)+len(skipUntil) == 0 {
		return
	}
	token := p.sc.Token()
	for token != EOF {
		if contains(skipUntilAfter, token) {
			p.scanNext()
			break
		} else if contains(skipUntil, token) {
			break
		}
		token = p.scanNext()
	}
}

func contains(kinds []SyntaxKind, k SyntaxKind) bool {
	for _, x := range kinds {
		if x == k {
			return true
		}
	}
	return false
}

var (
	braceOrComma   = []SyntaxKind{CloseBraceToken, CommaToken}
	bracketOrComma = []SyntaxKind{CloseBracketToken, CommaToken}
)

func (p *visitor) parseString(isValue bool) {
	value := p.sc.Value()
	if isValue {
		p.literal(value)
	} else {
		p.property(value)
	}
	p.scanNext()
}

// number is the value of a number token, and whether the token is a number.
func number(text string) (float64, bool) {
	if strings.HasSuffix(text, ".") {
		return 0, false
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		// Out of range gives an infinity, as JSON.parse does.
		if ne, ok := err.(*strconv.NumError); ok && ne.Err == strconv.ErrRange {
			return f, true
		}
		return 0, false
	}
	return f, true
}

func (p *visitor) parseLiteral() bool {
	switch p.sc.Token() {
	case NumericLiteral:
		value, ok := number(p.sc.Value())
		if !ok {
			p.fail(InvalidNumberFormat, nil, nil)
		}
		p.literal(value)
	case NullKeyword:
		p.literal(nil)
	case TrueKeyword:
		p.literal(true)
	case FalseKeyword:
		p.literal(false)
	default:
		return false
	}
	p.scanNext()
	return true
}

func (p *visitor) parseProperty() bool {
	if p.sc.Token() != StringLiteral {
		p.fail(PropertyNameExpected, nil, braceOrComma)
		return false
	}
	p.parseString(false)
	if p.sc.Token() == ColonToken {
		p.separator(":")
		p.scanNext()
		if !p.parseValue() {
			p.fail(ValueExpected, nil, braceOrComma)
		}
	} else {
		p.fail(ColonExpected, nil, braceOrComma)
	}
	return true
}

func (p *visitor) parseObject() {
	p.objectBegin()
	p.scanNext()
	needsComma := false
	for p.sc.Token() != CloseBraceToken && p.sc.Token() != EOF {
		if p.sc.Token() == CommaToken {
			if !needsComma {
				p.fail(ValueExpected, nil, nil)
			}
			p.separator(",")
			p.scanNext()
			if p.sc.Token() == CloseBraceToken && !p.opts.DisallowTrailingComma {
				break
			}
		} else if needsComma {
			p.fail(CommaExpected, nil, nil)
		}
		if !p.parseProperty() {
			p.fail(ValueExpected, nil, braceOrComma)
		}
		needsComma = true
	}
	p.objectEnd()
	if p.sc.Token() != CloseBraceToken {
		p.fail(CloseBraceExpected, []SyntaxKind{CloseBraceToken}, nil)
	} else {
		p.scanNext()
	}
}

func (p *visitor) parseArray() {
	p.arrayBegin()
	p.scanNext()
	needsComma := false
	for p.sc.Token() != CloseBracketToken && p.sc.Token() != EOF {
		if p.sc.Token() == CommaToken {
			if !needsComma {
				p.fail(ValueExpected, nil, nil)
			}
			p.separator(",")
			p.scanNext()
			if p.sc.Token() == CloseBracketToken && !p.opts.DisallowTrailingComma {
				break
			}
		} else if needsComma {
			p.fail(CommaExpected, nil, nil)
		}
		if !p.parseValue() {
			p.fail(ValueExpected, nil, bracketOrComma)
		}
		needsComma = true
	}
	p.arrayEnd()
	if p.sc.Token() != CloseBracketToken {
		p.fail(CloseBracketExpected, []SyntaxKind{CloseBracketToken}, nil)
	} else {
		p.scanNext()
	}
}

func (p *visitor) parseValue() bool {
	switch p.sc.Token() {
	case OpenBracketToken:
		p.parseArray()
		return true
	case OpenBraceToken:
		p.parseObject()
		return true
	case StringLiteral:
		p.parseString(true)
		return true
	}
	return p.parseLiteral()
}

func (p *visitor) document() {
	p.scanNext()
	if p.sc.Token() == EOF {
		if !p.opts.AllowEmptyContent {
			p.fail(ValueExpected, nil, nil)
		}
		return
	}
	if !p.parseValue() {
		p.fail(ValueExpected, nil, nil)
		return
	}
	if p.sc.Token() != EOF {
		p.fail(EndOfFileExpected, nil, nil)
	}
}
