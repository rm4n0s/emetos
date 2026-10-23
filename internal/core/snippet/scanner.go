package snippet

// Ported from VS Code: src/vs/editor/contrib/snippet/browser/snippetParser.ts

// TokenType is the kind of a token of a snippet.
type TokenType int

// The token types of a snippet. A text that is none of the special characters, a number or a
// name is a Format token.
const (
	Dollar TokenType = iota
	Colon
	Comma
	CurlyOpen
	CurlyClose
	Backslash
	Forwardslash
	Pipe
	Int
	VariableName
	Format
	Plus
	Dash
	QuestionMark
	EOF
)

// Token is a piece of a snippet: its kind and where it is, in bytes.
type Token struct {
	Type TokenType
	Pos  int
	Len  int
}

// Scanner cuts a snippet into tokens.
type Scanner struct {
	value string
	// Pos is the byte offset the next token starts at.
	Pos int
}

// staticTokens maps the characters that are tokens of their own.
func staticToken(ch byte) (TokenType, bool) {
	switch ch {
	case '$':
		return Dollar, true
	case ':':
		return Colon, true
	case ',':
		return Comma, true
	case '{':
		return CurlyOpen, true
	case '}':
		return CurlyClose, true
	case '\\':
		return Backslash, true
	case '/':
		return Forwardslash, true
	case '|':
		return Pipe, true
	case '+':
		return Plus, true
	case '-':
		return Dash, true
	case '?':
		return QuestionMark, true
	}
	return 0, false
}

func isDigit(ch byte) bool { return ch >= '0' && ch <= '9' }

func isVariableCharacter(ch byte) bool {
	return ch == '_' || (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z')
}

// Text sets the text to scan and starts at its beginning.
func (s *Scanner) Text(value string) {
	s.value = value
	s.Pos = 0
}

// TokenText is the text of a token.
func (s *Scanner) TokenText(t Token) string { return s.value[t.Pos : t.Pos+t.Len] }

// Next scans the next token; at the end of the text it gives EOF.
func (s *Scanner) Next() Token {
	if s.Pos >= len(s.value) {
		return Token{Type: EOF, Pos: s.Pos}
	}
	pos := s.Pos
	ch := s.value[pos]

	if t, ok := staticToken(ch); ok {
		s.Pos++
		return Token{Type: t, Pos: pos, Len: 1}
	}

	n := 1
	switch {
	case isDigit(ch):
		for pos+n < len(s.value) && isDigit(s.value[pos+n]) {
			n++
		}
		s.Pos += n
		return Token{Type: Int, Pos: pos, Len: n}
	case isVariableCharacter(ch):
		for pos+n < len(s.value) && (isVariableCharacter(s.value[pos+n]) || isDigit(s.value[pos+n])) {
			n++
		}
		s.Pos += n
		return Token{Type: VariableName, Pos: pos, Len: n}
	}

	// format: everything up to the next token of another kind
	for pos+n < len(s.value) {
		c := s.value[pos+n]
		if _, static := staticToken(c); static || isDigit(c) || isVariableCharacter(c) {
			break
		}
		n++
	}
	s.Pos += n
	return Token{Type: Format, Pos: pos, Len: n}
}
