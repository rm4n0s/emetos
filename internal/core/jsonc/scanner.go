package jsonc

import (
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// ScanError is what went wrong in the token the scanner just read.
type ScanError int

// The errors a token can have.
const (
	// ScanNone means the token is well formed.
	ScanNone ScanError = iota
	// ScanUnexpectedEndOfComment is a block comment that is never closed.
	ScanUnexpectedEndOfComment
	// ScanUnexpectedEndOfString is a string that ends at a line break or the end of the text.
	ScanUnexpectedEndOfString
	// ScanUnexpectedEndOfNumber is a number with a dot or an exponent and no digits after it.
	ScanUnexpectedEndOfNumber
	// ScanInvalidUnicode is a \u escape without four hex digits.
	ScanInvalidUnicode
	// ScanInvalidEscapeCharacter is a backslash followed by a character that is no escape.
	ScanInvalidEscapeCharacter
	// ScanInvalidCharacter is a control character inside a string.
	ScanInvalidCharacter
)

// SyntaxKind is the kind of a token.
type SyntaxKind int

// The kinds of tokens.
const (
	// OpenBraceToken is {.
	OpenBraceToken SyntaxKind = iota + 1
	// CloseBraceToken is }.
	CloseBraceToken
	// OpenBracketToken is [.
	OpenBracketToken
	// CloseBracketToken is ].
	CloseBracketToken
	// CommaToken is ,.
	CommaToken
	// ColonToken is :.
	ColonToken
	// NullKeyword is null.
	NullKeyword
	// TrueKeyword is true.
	TrueKeyword
	// FalseKeyword is false.
	FalseKeyword
	// StringLiteral is a string with its quotes.
	StringLiteral
	// NumericLiteral is a number.
	NumericLiteral
	// LineCommentTrivia is a // comment up to, not including, the line break.
	LineCommentTrivia
	// BlockCommentTrivia is a /* */ comment.
	BlockCommentTrivia
	// LineBreakTrivia is a line break: \n, \r\n, \r, U+2028 or U+2029.
	LineBreakTrivia
	// Trivia is white space other than line breaks.
	Trivia
	// Unknown is text that is no token: a word that is no keyword, or a lone slash.
	Unknown
	// EOF is the end of the text.
	EOF
)

var kindNames = [...]string{
	"", "OpenBraceToken", "CloseBraceToken", "OpenBracketToken", "CloseBracketToken", "CommaToken",
	"ColonToken", "NullKeyword", "TrueKeyword", "FalseKeyword", "StringLiteral", "NumericLiteral",
	"LineCommentTrivia", "BlockCommentTrivia", "LineBreakTrivia", "Trivia", "Unknown", "EOF",
}

// String is the name of the kind.
func (k SyntaxKind) String() string {
	if k < 1 || int(k) >= len(kindNames) {
		return "SyntaxKind(" + strconv.Itoa(int(k)) + ")"
	}
	return kindNames[k]
}

// Scanner reads the tokens of a text. Offsets and lengths are in bytes.
//
// Ported from VS Code: src/vs/base/common/json.ts (createScanner).
type Scanner struct {
	text         string
	ignoreTrivia bool
	pos          int
	value        string
	tokenOffset  int
	token        SyntaxKind
	err          ScanError
}

// NewScanner makes a scanner at the start of text. With ignoreTrivia, Scan skips
// white space, line breaks and comments.
func NewScanner(text string, ignoreTrivia bool) *Scanner {
	return &Scanner{text: text, ignoreTrivia: ignoreTrivia, token: Unknown}
}

// SetPosition moves the scanner to offset; Scan gives the first token from there.
func (s *Scanner) SetPosition(offset int) {
	s.pos = max(0, min(offset, len(s.text)))
	s.value = ""
	s.tokenOffset = 0
	s.token = Unknown
	s.err = ScanNone
}

// Position is the offset after the last token read.
func (s *Scanner) Position() int { return s.pos }

// Token is the kind of the last token read.
func (s *Scanner) Token() SyntaxKind { return s.token }

// Value is the value of the last token: the decoded content of a string (without
// its quotes), the text of a number, comment, word or white space.
func (s *Scanner) Value() string { return s.value }

// Offset is where the last token starts.
func (s *Scanner) Offset() int { return s.tokenOffset }

// Length is the length of the last token.
func (s *Scanner) Length() int { return s.pos - s.tokenOffset }

// Error is what is wrong with the last token, if anything.
func (s *Scanner) Error() ScanError { return s.err }

// Scan reads the next token and returns its kind.
func (s *Scanner) Scan() SyntaxKind {
	if !s.ignoreTrivia {
		return s.next()
	}
	for {
		if k := s.next(); k < LineCommentTrivia || k > Trivia {
			return k
		}
	}
}

// isWhitespace tells white space other than line breaks, as JSON in VS Code takes it.
func isWhitespace(r rune) bool {
	switch r {
	case ' ', '\t', '\v', '\f', 0xA0, 0x1680, 0x202F, 0x205F, 0x3000, 0xFEFF:
		return true
	}
	return r >= 0x2000 && r <= 0x200B
}

func isLineBreak(r rune) bool {
	return r == '\n' || r == '\r' || r == 0x2028 || r == 0x2029
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// runeAt decodes the character at pos; at the end of the text it is -1.
func (s *Scanner) runeAt(pos int) (rune, int) {
	if pos >= len(s.text) {
		return -1, 0
	}
	if b := s.text[pos]; b < utf8.RuneSelf {
		return rune(b), 1
	}
	return utf8.DecodeRuneInString(s.text[pos:])
}

// hexDigit is the value of a hex digit, or -1.
func hexDigit(b byte) int {
	switch {
	case b >= '0' && b <= '9':
		return int(b - '0')
	case b >= 'a' && b <= 'f':
		return int(b-'a') + 10
	case b >= 'A' && b <= 'F':
		return int(b-'A') + 10
	}
	return -1
}

// hexAt reads count hex digits at pos without moving; it is -1 when there are fewer.
func (s *Scanner) hexAt(pos, count int) (value, digits int) {
	for digits < count && pos+digits < len(s.text) {
		d := hexDigit(s.text[pos+digits])
		if d < 0 {
			break
		}
		value = value*16 + d
		digits++
	}
	if digits < count {
		value = -1
	}
	return value, digits
}

func (s *Scanner) scanNumber() string {
	text := s.text
	start := s.pos
	if text[s.pos] == '0' {
		s.pos++
	} else {
		s.pos++
		for s.pos < len(text) && isDigit(text[s.pos]) {
			s.pos++
		}
	}
	if s.pos < len(text) && text[s.pos] == '.' {
		s.pos++
		if s.pos < len(text) && isDigit(text[s.pos]) {
			for s.pos < len(text) && isDigit(text[s.pos]) {
				s.pos++
			}
		} else {
			s.err = ScanUnexpectedEndOfNumber
			return text[start:s.pos]
		}
	}
	end := s.pos
	if s.pos < len(text) && (text[s.pos] == 'e' || text[s.pos] == 'E') {
		s.pos++
		if s.pos < len(text) && (text[s.pos] == '+' || text[s.pos] == '-') {
			s.pos++
		}
		if s.pos < len(text) && isDigit(text[s.pos]) {
			for s.pos < len(text) && isDigit(text[s.pos]) {
				s.pos++
			}
			end = s.pos
		} else {
			s.err = ScanUnexpectedEndOfNumber
		}
	}
	return text[start:end]
}

// scanString reads a string after its opening quote and returns its decoded content.
// An escape \uXXXX of a high surrogate followed by one of a low surrogate is one
// character; a lone surrogate becomes U+FFFD, as no Go string can hold it.
func (s *Scanner) scanString() string {
	text := s.text
	var result strings.Builder
	start := s.pos
	for {
		if s.pos >= len(text) {
			result.WriteString(text[start:s.pos])
			s.err = ScanUnexpectedEndOfString
			break
		}
		ch := text[s.pos]
		if ch == '"' {
			result.WriteString(text[start:s.pos])
			s.pos++
			break
		}
		if ch == '\\' {
			result.WriteString(text[start:s.pos])
			s.pos++
			if s.pos >= len(text) {
				s.err = ScanUnexpectedEndOfString
				break
			}
			esc := text[s.pos]
			s.pos++
			switch esc {
			case '"':
				result.WriteByte('"')
			case '\\':
				result.WriteByte('\\')
			case '/':
				result.WriteByte('/')
			case 'b':
				result.WriteByte('\b')
			case 'f':
				result.WriteByte('\f')
			case 'n':
				result.WriteByte('\n')
			case 'r':
				result.WriteByte('\r')
			case 't':
				result.WriteByte('\t')
			case 'u':
				v, digits := s.hexAt(s.pos, 4)
				s.pos += digits
				if v < 0 {
					s.err = ScanInvalidUnicode
					break
				}
				r := rune(v)
				if utf16.IsSurrogate(r) {
					r = utf8.RuneError
					if v < 0xDC00 && s.pos+1 < len(text) && text[s.pos] == '\\' && text[s.pos+1] == 'u' {
						if low, n := s.hexAt(s.pos+2, 4); low >= 0xDC00 && low <= 0xDFFF && n == 4 {
							r = utf16.DecodeRune(rune(v), rune(low))
							s.pos += 6
						}
					}
				}
				result.WriteRune(r)
			default:
				s.err = ScanInvalidEscapeCharacter
				if esc >= utf8.RuneSelf {
					// The character after the backslash is dropped whole.
					_, size := utf8.DecodeRuneInString(text[s.pos-1:])
					s.pos += size - 1
				}
			}
			start = s.pos
			continue
		}
		if ch <= 0x1F {
			if ch == '\n' || ch == '\r' {
				result.WriteString(text[start:s.pos])
				s.err = ScanUnexpectedEndOfString
				break
			}
			// Mark it as an error and go on with the string.
			s.err = ScanInvalidCharacter
		}
		s.pos++
	}
	return result.String()
}

// isUnknownContent tells whether a character can be part of a word that is no token.
func isUnknownContent(r rune) bool {
	if isWhitespace(r) || isLineBreak(r) {
		return false
	}
	switch r {
	case '}', ']', '{', '[', '"', ':', ',', '/':
		return false
	}
	return true
}

func (s *Scanner) next() SyntaxKind {
	text := s.text
	s.value = ""
	s.err = ScanNone
	s.tokenOffset = s.pos
	if s.pos >= len(text) {
		s.tokenOffset = len(text)
		s.token = EOF
		return s.token
	}
	r, size := s.runeAt(s.pos)
	if isWhitespace(r) {
		for isWhitespace(r) {
			s.pos += size
			r, size = s.runeAt(s.pos)
		}
		s.value = text[s.tokenOffset:s.pos]
		s.token = Trivia
		return s.token
	}
	if isLineBreak(r) {
		s.pos += size
		if r == '\r' && s.pos < len(text) && text[s.pos] == '\n' {
			s.pos++
		}
		s.value = text[s.tokenOffset:s.pos]
		s.token = LineBreakTrivia
		return s.token
	}
	switch text[s.pos] {
	case '{':
		s.pos++
		s.token = OpenBraceToken
	case '}':
		s.pos++
		s.token = CloseBraceToken
	case '[':
		s.pos++
		s.token = OpenBracketToken
	case ']':
		s.pos++
		s.token = CloseBracketToken
	case ':':
		s.pos++
		s.token = ColonToken
	case ',':
		s.pos++
		s.token = CommaToken
	case '"':
		s.pos++
		s.value = s.scanString()
		s.token = StringLiteral
	case '/':
		s.token = s.scanSlash()
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		s.token = s.scanNumberToken()
	default:
		s.token = s.scanWord()
	}
	return s.token
}

// scanSlash reads a comment, or a slash that starts none.
func (s *Scanner) scanSlash() SyntaxKind {
	text := s.text
	start := s.pos
	switch {
	case s.pos+1 < len(text) && text[s.pos+1] == '/':
		s.pos += 2
		for s.pos < len(text) {
			r, size := s.runeAt(s.pos)
			if isLineBreak(r) {
				break
			}
			s.pos += size
		}
		s.value = text[start:s.pos]
		return LineCommentTrivia
	case s.pos+1 < len(text) && text[s.pos+1] == '*':
		s.pos += 2
		closed := false
		for s.pos < len(text)-1 {
			if text[s.pos] == '*' && text[s.pos+1] == '/' {
				s.pos += 2
				closed = true
				break
			}
			s.pos++
		}
		if !closed {
			s.pos = len(text)
			s.err = ScanUnexpectedEndOfComment
		}
		s.value = text[start:s.pos]
		return BlockCommentTrivia
	}
	s.pos++
	s.value = "/"
	return Unknown
}

// scanNumberToken reads a number, or a minus that starts none.
func (s *Scanner) scanNumberToken() SyntaxKind {
	sign := ""
	if s.text[s.pos] == '-' {
		sign = "-"
		s.pos++
		if s.pos >= len(s.text) || !isDigit(s.text[s.pos]) {
			s.value = sign
			return Unknown
		}
	}
	s.value = sign + s.scanNumber()
	return NumericLiteral
}

// scanWord reads a keyword or a word that is no token.
func (s *Scanner) scanWord() SyntaxKind {
	start := s.pos
	for s.pos < len(s.text) {
		r, size := s.runeAt(s.pos)
		if !isUnknownContent(r) {
			break
		}
		s.pos += size
	}
	if s.pos == start {
		_, size := s.runeAt(s.pos)
		s.pos += size
	}
	s.value = s.text[start:s.pos]
	switch s.value {
	case "true":
		return TrueKeyword
	case "false":
		return FalseKeyword
	case "null":
		return NullKeyword
	}
	return Unknown
}
