package contextkey

import (
	"strings"

	"github.com/rm4n0s/errors"
)

// ParserConfig tunes the parser.
type ParserConfig struct {
	// RegexErrorRecovery lets the parser rebuild a regex that has unescaped slashes
	// (/src//), as VS Code's own keybindings and menus need. Parse and Validate turn it off,
	// to guide users to correct regexes.
	RegexErrorRecovery bool
}

// ParsingError is something wrong in the order of the tokens.
type ParsingError struct {
	Message string
	Offset  int
	Lexeme  string
	// AdditionalInfo is a hint.
	AdditionalInfo string
}

const (
	errEmptyString      = "Empty context key expression"
	hintEmptyString     = "Did you forget to write an expression? You can also put 'false' or 'true' to always evaluate to false or true, respectively."
	errNoInAfterNot     = "'in' after 'not'."
	errClosingParen     = "closing parenthesis ')'"
	errUnexpectedToken  = "Unexpected token"
	hintUnexpectedToken = "Did you forget to put && or || before the token?"
	errUnexpectedEOF    = "Unexpected end of expression"
	hintUnexpectedEOF   = "Did you forget to put a context key?"
)

// parseFailure is what the parser panics with to stop; the errors are in the parser.
type parseFailure struct{}

// Parser reads when-clauses. After Parse, LexingErrors and ParsingErrors say what was
// wrong when it returned nil. A Parser is not safe for concurrent use.
//
// Grammar:
//
//	expression ::= or
//	or         ::= and { '||' and }*
//	and        ::= term { '&&' term }*
//	term       ::= '!' (KEY | true | false | parenthesized) | primary
//	primary    ::= 'true' | 'false' | parenthesized | KEY '=~' REGEX
//	             | KEY [ ('==' | '!=' | '<' | '<=' | '>' | '>=' | 'not' 'in' | 'in') value ]
//	parenthesized ::= '(' expression ')'
//	value      ::= 'true' | 'false' | 'in' | VALUE | SINGLE_QUOTED_STR | (nothing)
//
// Ported from VS Code: src/vs/platform/contextkey/common/contextkey.ts (Parser)
type Parser struct {
	cfg     ParserConfig
	scanner Scanner
	tokens  []Token
	current int
	errs    []ParsingError
}

// NewParser makes a parser.
func NewParser(cfg ParserConfig) *Parser { return &Parser{cfg: cfg} }

// LexingErrors are the lexing errors of the last Parse.
func (p *Parser) LexingErrors() []LexingError { return p.scanner.Errors() }

// ParsingErrors are the parsing errors of the last Parse.
func (p *Parser) ParsingErrors() []ParsingError { return p.errs }

// Parse reads a when-clause. It returns nil when there are errors.
func (p *Parser) Parse(input string) (expr Expr) {
	p.errs = nil
	if input == "" {
		p.errs = append(p.errs, ParsingError{Message: errEmptyString, Offset: 0, AdditionalInfo: hintEmptyString})
		return nil
	}
	p.tokens = p.scanner.Reset(input).Scan()
	p.current = 0
	defer func() {
		if r := recover(); r != nil {
			if _, ok := r.(parseFailure); !ok {
				panic(r)
			}
			expr = nil
		}
	}()
	expr = p.or()
	if !p.atEnd() {
		peek := p.peek()
		info := ""
		if peek.Type == TokStr {
			info = hintUnexpectedToken
		}
		p.errs = append(p.errs, ParsingError{Message: errUnexpectedToken, Offset: peek.Offset, Lexeme: peek.String(), AdditionalInfo: info})
		panic(parseFailure{})
	}
	return expr
}

func (p *Parser) or() Expr {
	exprs := []Expr{p.and()}
	for p.matchOne(TokOr) {
		exprs = append(exprs, p.and())
	}
	if len(exprs) == 1 {
		return exprs[0]
	}
	return Or(exprs...)
}

func (p *Parser) and() Expr {
	exprs := []Expr{p.term()}
	for p.matchOne(TokAnd) {
		exprs = append(exprs, p.term())
	}
	if len(exprs) == 1 {
		return exprs[0]
	}
	return And(exprs...)
}

func (p *Parser) term() Expr {
	if p.matchOne(TokNeg) {
		peek := p.peek()
		switch peek.Type {
		case TokTrue:
			p.advance()
			return falseExpr{}
		case TokFalse:
			p.advance()
			return trueExpr{}
		case TokLParen:
			p.advance()
			expr := p.or()
			p.consume(TokRParen, errClosingParen)
			if expr == nil {
				return nil
			}
			return expr.Negate()
		case TokStr:
			p.advance()
			return Not(peek.Lexeme)
		}
		panic(p.expectedButGot("KEY | true | false | '(' expression ')'", peek, ""))
	}
	return p.primary()
}

func (p *Parser) primary() Expr {
	peek := p.peek()
	switch peek.Type {
	case TokTrue:
		p.advance()
		return trueExpr{}
	case TokFalse:
		p.advance()
		return falseExpr{}
	case TokLParen:
		p.advance()
		expr := p.or()
		p.consume(TokRParen, errClosingParen)
		return expr
	case TokStr:
		return p.keyExpr(peek.Lexeme)
	case TokEOF:
		p.errs = append(p.errs, ParsingError{Message: errUnexpectedEOF, Offset: peek.Offset, AdditionalInfo: hintUnexpectedEOF})
		panic(parseFailure{})
	}
	panic(p.expectedButGot("true | false | KEY \n\t| KEY '=~' REGEX \n\t| KEY ('==' | '!=' | '<' | '<=' | '>' | '>=' | 'in' | 'not' 'in') value", p.peek(), ""))
}

// keyExpr reads what follows a key.
func (p *Parser) keyExpr(key string) Expr {
	p.advance()

	if p.matchOne(TokRegexOp) {
		return p.regexExpr(key)
	}
	if p.matchOne(TokNot) {
		p.consume(TokIn, errNoInAfterNot)
		return NotIn(key, p.value())
	}
	switch p.peek().Type {
	case TokEq:
		p.advance()
		right := p.value()
		if p.previous().Type == TokQuotedStr {
			// "foo == 'true'" stays a comparison with a string; "foo == true" is "foo".
			return Equals(key, right)
		}
		switch right {
		case "true":
			return Has(key)
		case "false":
			return Not(key)
		}
		return Equals(key, right)
	case TokNotEq:
		p.advance()
		right := p.value()
		if p.previous().Type == TokQuotedStr {
			return NotEquals(key, right)
		}
		switch right {
		case "true":
			return Not(key)
		case "false":
			return Has(key)
		}
		return NotEquals(key, right)
	case TokLt:
		p.advance()
		return Smaller(key, p.value())
	case TokLtEq:
		p.advance()
		return SmallerEquals(key, p.value())
	case TokGt:
		p.advance()
		return Greater(key, p.value())
	case TokGtEq:
		p.advance()
		return GreaterEquals(key, p.value())
	case TokIn:
		p.advance()
		return In(key, p.value())
	}
	return Has(key)
}

// splitRegex cuts /pattern/flags into the pattern and the flags, without g and y.
func splitRegex(lexeme string) (pattern, flags string, ok bool) {
	closing := strings.LastIndex(lexeme, "/")
	if closing < 1 {
		return "", "", false
	}
	if closing != len(lexeme)-1 {
		flags = strings.NewReplacer("g", "", "y", "").Replace(lexeme[closing+1:])
	}
	return lexeme[1:closing], flags, true
}

func (p *Parser) regexExpr(key string) Expr {
	// Some keybindings write slashes in a regex without escaping them: the regex is
	// rebuilt from the tokens.
	expr := p.peek()

	if !p.cfg.RegexErrorRecovery {
		p.advance()
		if expr.Type != TokRegexStr {
			panic(p.expectedButGot("REGEX", expr, ""))
		}
		return p.makeRegex(key, expr.Lexeme, expr)
	}

	switch expr.Type {
	case TokRegexStr, TokError: // an error token too, for something like /(/file)/
		reconstruction := []string{expr.Lexeme}
		p.advance()
		following := p.peek()
		balance := 0
		for _, r := range expr.Lexeme {
			switch r {
			case '(':
				balance++
			case ')':
				balance--
			}
		}
		for !p.atEnd() && following.Type != TokAnd && following.Type != TokOr {
			switch following.Type {
			case TokLParen:
				balance++
			case TokRParen:
				balance--
			case TokRegexStr, TokQuotedStr:
				for i := 0; i < len(following.Lexeme); i++ {
					if following.Lexeme[i] == '(' {
						balance++
					} else if i < len(expr.Lexeme) && expr.Lexeme[i] == ')' {
						// VS Code looks at the first token here, not at the following one.
						balance--
					}
				}
			}
			if balance < 0 {
				break
			}
			reconstruction = append(reconstruction, following.String())
			p.advance()
			following = p.peek()
		}
		return p.makeRegex(key, strings.Join(reconstruction, ""), expr)

	case TokQuotedStr:
		serialized := expr.Lexeme
		p.advance()
		// The old way of writing regexes: '/pattern/i' in quotes.
		if strings.TrimSpace(serialized) != "" {
			start, end := strings.Index(serialized, "/"), strings.LastIndex(serialized, "/")
			if start != end && start >= 0 {
				flags := ""
				if end+1 < len(serialized) && serialized[end+1] == 'i' {
					flags = "i"
				}
				if e, err := Regex(key, serialized[start+1:end], flags); err == nil {
					return e
				}
			}
		}
		panic(p.expectedButGot("REGEX", expr, ""))
	}
	panic(p.expectedButGot("REGEX", p.peek(), ""))
}

func (p *Parser) makeRegex(key, lexeme string, at Token) Expr {
	pattern, flags, ok := splitRegex(lexeme)
	if !ok {
		panic(p.expectedButGot("REGEX", at, ""))
	}
	e, err := Regex(key, pattern, flags)
	if err != nil {
		panic(p.expectedButGot("REGEX", at, ""))
	}
	return e
}

func (p *Parser) value() string {
	token := p.peek()
	switch token.Type {
	case TokStr, TokQuotedStr:
		p.advance()
		return token.Lexeme
	case TokTrue:
		p.advance()
		return "true"
	case TokFalse:
		p.advance()
		return "false"
	case TokIn: // "languageId == in" exists in extensions
		p.advance()
		return "in"
	}
	// "foo == " is used by extensions: no token is eaten.
	return ""
}

func (p *Parser) previous() Token { return p.tokens[p.current-1] }

func (p *Parser) matchOne(t TokenType) bool {
	if p.peek().Type == t {
		p.advance()
		return true
	}
	return false
}

func (p *Parser) advance() Token {
	if !p.atEnd() {
		p.current++
	}
	return p.previous()
}

func (p *Parser) consume(t TokenType, message string) Token {
	if p.peek().Type == t {
		return p.advance()
	}
	panic(p.expectedButGot(message, p.peek(), ""))
}

func (p *Parser) expectedButGot(expected string, got Token, info string) parseFailure {
	lexeme := got.String()
	p.errs = append(p.errs, ParsingError{
		Message:        "Expected: " + expected + "\nReceived: '" + lexeme + "'.",
		Offset:         got.Offset,
		Lexeme:         lexeme,
		AdditionalInfo: info,
	})
	return parseFailure{}
}

func (p *Parser) peek() Token { return p.tokens[p.current] }

func (p *Parser) atEnd() bool { return p.peek().Type == TokEOF }

func errInvalidRegex(source, flags string) error {
	return errors.New("WhenRegexInvalid", "the regular expression is not valid", "source", source, "flags", flags)
}

// Parse reads a when-clause with the strict parser (a regex with unescaped slashes is an
// error). It fails with the tag WhenClauseInvalid, whose metadata has the message and the
// offset of the first problem.
//
// Ported from VS Code: src/vs/platform/contextkey/common/contextkey.ts (ContextKeyExpr.deserialize)
func Parse(input string) (Expr, error) {
	p := NewParser(ParserConfig{})
	if expr := p.Parse(input); expr != nil {
		return expr, nil
	}
	d := diagnostics(p)
	return nil, errors.New("WhenClauseInvalid", d[0].Message, "input", input, "offset", d[0].Offset, "length", d[0].Length)
}

// Diagnostic is a problem in a when-clause.
type Diagnostic struct {
	Message string
	Offset  int
	Length  int
}

func diagnostics(p *Parser) []Diagnostic {
	var out []Diagnostic
	if lex := p.LexingErrors(); len(lex) > 0 {
		for _, e := range lex {
			msg := "Unexpected token."
			if e.AdditionalInfo != "" {
				msg = "Unexpected token. Hint: " + e.AdditionalInfo
			}
			out = append(out, Diagnostic{Message: msg, Offset: e.Offset, Length: len(e.Lexeme)})
		}
		return out
	}
	for _, e := range p.ParsingErrors() {
		msg := e.Message
		if e.AdditionalInfo != "" {
			msg += ". " + e.AdditionalInfo
		}
		out = append(out, Diagnostic{Message: msg, Offset: e.Offset, Length: len(e.Lexeme)})
	}
	return out
}

// Validate lists the problems of a when-clause, for the editor of keybindings: none when it is
// valid. It uses the strict parser.
//
// Ported from VS Code: src/vs/platform/contextkey/common/contextkey.ts (validateWhenClauses)
func Validate(input string) []Diagnostic {
	p := NewParser(ParserConfig{})
	p.Parse(input)
	return diagnostics(p)
}
