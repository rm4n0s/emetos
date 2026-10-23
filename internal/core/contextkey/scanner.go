package contextkey

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// TokenType is the kind of a token of a when-clause.
type TokenType int

// The kinds of tokens.
const (
	// TokLParen is (.
	TokLParen TokenType = iota
	// TokRParen is ).
	TokRParen
	// TokNeg is !.
	TokNeg
	// TokEq is == or ===.
	TokEq
	// TokNotEq is != or !==.
	TokNotEq
	// TokLt is <.
	TokLt
	// TokLtEq is <=.
	TokLtEq
	// TokGt is >.
	TokGt
	// TokGtEq is >=.
	TokGtEq
	// TokRegexOp is =~.
	TokRegexOp
	// TokRegexStr is /regex/ with its flags.
	TokRegexStr
	// TokTrue is true.
	TokTrue
	// TokFalse is false.
	TokFalse
	// TokIn is in.
	TokIn
	// TokNot is not.
	TokNot
	// TokAnd is &&.
	TokAnd
	// TokOr is ||.
	TokOr
	// TokStr is a word: a key or a value.
	TokStr
	// TokQuotedStr is a 'quoted string', its lexeme without the quotes.
	TokQuotedStr
	// TokError is text that is no token.
	TokError
	// TokEOF is the end of the input.
	TokEOF
)

// Token is a piece of a when-clause. Offsets are in bytes.
type Token struct {
	Type   TokenType
	Offset int
	// Lexeme is the text of a TokRegexStr, TokStr, TokQuotedStr and TokError.
	Lexeme string
	// TripleEq is set for === and !==.
	TripleEq bool
}

// Lexeme returns the text of a token as the parser shows it in messages.
func (t Token) String() string {
	switch t.Type {
	case TokLParen:
		return "("
	case TokRParen:
		return ")"
	case TokNeg:
		return "!"
	case TokEq:
		if t.TripleEq {
			return "==="
		}
		return "=="
	case TokNotEq:
		if t.TripleEq {
			return "!=="
		}
		return "!="
	case TokLt:
		return "<"
	case TokLtEq:
		return "<="
	case TokGt:
		return ">"
	case TokGtEq:
		return ">="
	case TokRegexOp:
		return "=~"
	case TokTrue:
		return "true"
	case TokFalse:
		return "false"
	case TokIn:
		return "in"
	case TokNot:
		return "not"
	case TokAnd:
		return "&&"
	case TokOr:
		return "||"
	case TokEOF:
		return "EOF"
	}
	return t.Lexeme
}

// LexingError is text the scanner could not make a token of.
type LexingError struct {
	Offset int
	Lexeme string
	// AdditionalInfo is a hint, such as "Did you mean == or =~?".
	AdditionalInfo string
}

func didYouMean(meant ...string) string {
	switch len(meant) {
	case 1:
		return "Did you mean " + meant[0] + "?"
	case 2:
		return "Did you mean " + meant[0] + " or " + meant[1] + "?"
	case 3:
		return "Did you mean " + meant[0] + ", " + meant[1] + " or " + meant[2] + "?"
	}
	return ""
}

const (
	hintForgotQuote = "Did you forget to open or close the quote?"
	hintForgotSlash = "Did you forget to escape the '/' (slash) character? Put two backslashes before it to escape, e.g., '\\\\/'."
)

// Scanner splits a when-clause into tokens.
//
// Ported from VS Code: src/vs/platform/contextkey/common/scanner.ts
type Scanner struct {
	input   string
	start   int
	current int
	tokens  []Token
	errors  []LexingError
}

// Errors are the lexing errors of the last Scan.
func (s *Scanner) Errors() []LexingError { return s.errors }

// Reset sets the input to scan and forgets the last scan.
func (s *Scanner) Reset(input string) *Scanner {
	*s = Scanner{input: input}
	return s
}

func (s *Scanner) atEnd() bool { return s.current >= len(s.input) }

func (s *Scanner) advance() rune {
	r, size := utf8.DecodeRuneInString(s.input[s.current:])
	s.current += size
	return r
}

func (s *Scanner) peek() rune {
	if s.atEnd() {
		return 0
	}
	r, _ := utf8.DecodeRuneInString(s.input[s.current:])
	return r
}

func (s *Scanner) match(expected rune) bool {
	if s.atEnd() {
		return false
	}
	r, size := utf8.DecodeRuneInString(s.input[s.current:])
	if r != expected {
		return false
	}
	s.current += size
	return true
}

func (s *Scanner) add(t TokenType) {
	s.tokens = append(s.tokens, Token{Type: t, Offset: s.start})
}

func (s *Scanner) fail(additional string) {
	lexeme := s.input[s.start:s.current]
	s.errors = append(s.errors, LexingError{Offset: s.start, Lexeme: lexeme, AdditionalInfo: additional})
	s.tokens = append(s.tokens, Token{Type: TokError, Offset: s.start, Lexeme: lexeme})
}

// Scan returns the tokens of the input, ending with TokEOF. Text that is no token
// is a TokError and is listed in Errors.
func (s *Scanner) Scan() []Token {
	for !s.atEnd() {
		s.start = s.current
		switch ch := s.advance(); ch {
		case '(':
			s.add(TokLParen)
		case ')':
			s.add(TokRParen)
		case '!':
			if s.match('=') {
				triple := s.match('=')
				s.tokens = append(s.tokens, Token{Type: TokNotEq, Offset: s.start, TripleEq: triple})
			} else {
				s.add(TokNeg)
			}
		case '\'':
			s.quotedString()
		case '/':
			s.regex()
		case '=':
			switch {
			case s.match('='):
				triple := s.match('=')
				s.tokens = append(s.tokens, Token{Type: TokEq, Offset: s.start, TripleEq: triple})
			case s.match('~'):
				s.add(TokRegexOp)
			default:
				s.fail(didYouMean("==", "=~"))
			}
		case '<':
			if s.match('=') {
				s.add(TokLtEq)
			} else {
				s.add(TokLt)
			}
		case '>':
			if s.match('=') {
				s.add(TokGtEq)
			} else {
				s.add(TokGt)
			}
		case '&':
			if s.match('&') {
				s.add(TokAnd)
			} else {
				s.fail(didYouMean("&&"))
			}
		case '|':
			if s.match('|') {
				s.add(TokOr)
			} else {
				s.fail(didYouMean("||"))
			}
		case ' ', '\r', '\t', '\n', 0xA0:
		default:
			s.str()
		}
	}
	s.start = s.current
	s.add(TokEOF)
	return append([]Token(nil), s.tokens...)
}

// isWordRune tells whether a character can be part of a word (a key or a value):
// [a-zA-Z0-9_<>\-./\\:*?+[\]^,#@;"%$\p{L}].
func isWordRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return true
	case strings.ContainsRune(`_<>-./\:*?+[]^,#@;"%$`, r):
		return true
	}
	return r >= utf8.RuneSelf && unicode.IsLetter(r)
}

// str reads a word; a character that cannot start one is skipped without an error.
func (s *Scanner) str() {
	s.current = s.start
	for !s.atEnd() {
		r, size := utf8.DecodeRuneInString(s.input[s.current:])
		if !isWordRune(r) {
			break
		}
		s.current += size
	}
	if s.current == s.start {
		_, size := utf8.DecodeRuneInString(s.input[s.start:])
		s.current = s.start + size
		return
	}
	lexeme := s.input[s.start:s.current]
	switch lexeme {
	case "not":
		s.add(TokNot)
	case "in":
		s.add(TokIn)
	case "false":
		s.add(TokFalse)
	case "true":
		s.add(TokTrue)
	default:
		s.tokens = append(s.tokens, Token{Type: TokStr, Offset: s.start, Lexeme: lexeme})
	}
}

// quotedString reads 'text'; the lexeme is without the quotes.
func (s *Scanner) quotedString() {
	for s.peek() != '\'' && !s.atEnd() {
		s.advance()
	}
	if s.atEnd() {
		s.fail(hintForgotQuote)
		return
	}
	s.advance() // the closing quote
	s.tokens = append(s.tokens, Token{Type: TokQuotedStr, Offset: s.start + 1, Lexeme: s.input[s.start+1 : s.current-1]})
}

// regex reads /.../flags. A slash inside the regex must be escaped.
func (s *Scanner) regex() {
	p := s.current
	inEscape, inClass := false, false
	for {
		if p >= len(s.input) {
			s.current = p
			s.fail(hintForgotSlash)
			return
		}
		ch := s.input[p]
		switch {
		case inEscape:
			inEscape = false
		case ch == '/' && !inClass:
			p++
			goto flags
		case ch == '[':
			inClass = true
		case ch == '\\':
			inEscape = true
		case ch == ']':
			inClass = false
		}
		p++
	}
flags:
	for p < len(s.input) && strings.IndexByte("igsmyu", s.input[p]) >= 0 {
		p++
	}
	s.current = p
	s.tokens = append(s.tokens, Token{Type: TokRegexStr, Offset: s.start, Lexeme: s.input[s.start:s.current]})
}
