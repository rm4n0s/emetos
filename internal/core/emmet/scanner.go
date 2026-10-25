package emmet

import "unicode/utf8"

// Ported from Emmet: packages/scanner/src/scanner.ts and packages/scanner/src/utils.ts
//
// The scanner works on bytes. Every character with a meaning in an abbreviation is ASCII, so a
// byte of a multi-byte character is never taken for one; the umlauts of element names are decoded
// where they are looked at.

// eofChar is what the scanner gives at the end of the text.
const eofChar = -1

// scanner is a streaming reader of a string.
type scanner struct {
	str string
	pos int
}

func newScanner(s string) *scanner { return &scanner{str: s} }

func (s *scanner) eof() bool { return s.pos >= len(s.str) }

// peek gives the next byte without moving, or eofChar.
func (s *scanner) peek() int { return s.at(s.pos) }

// at gives the byte at a position, or eofChar outside of the text.
func (s *scanner) at(pos int) int {
	if pos < 0 || pos >= len(s.str) {
		return eofChar
	}
	return int(s.str[pos])
}

// eat moves over the next byte if it is the given one.
func (s *scanner) eat(ch byte) bool {
	if s.peek() == int(ch) {
		s.pos++
		return true
	}
	return false
}

// eatWhile moves over the bytes that match and tells if there were any.
func (s *scanner) eatWhile(match func(int) bool) bool {
	start := s.pos
	for !s.eof() && match(s.peek()) {
		s.pos++
	}
	return s.pos != start
}

// runeAt decodes the character at a position.
func (s *scanner) runeAt(pos int) (rune, int) {
	if s.str[pos] < utf8.RuneSelf {
		return rune(s.str[pos]), 1
	}
	return utf8.DecodeRuneInString(s.str[pos:])
}

func isNumber(code int) bool { return code > 47 && code < 58 }

// isAlpha tells if the code is a letter A to Z.
func isAlpha(code int) bool {
	c := code &^ 32
	return c >= 65 && c <= 90
}

func isAlphaWord(code int) bool { return code == '_' || isAlpha(code) }

func isAlphaNumericWord(code int) bool { return isNumber(code) || isAlphaWord(code) }

// isUmlaut tells if the code is one of ä, Ä, ö, Ö, ü and Ü.
func isUmlaut(code int) bool {
	switch code {
	case 196, 214, 220, 228, 246, 252:
		return true
	}
	return false
}

// spaceLen gives the length in bytes of the white space character at a position (a space, a tab,
// a line break or a no-break space, which is two bytes in UTF-8), or 0 when there is none. Emmet
// tests the character code 160 for the no-break space; a byte of that value would split other
// characters here.
func (s *scanner) spaceLen(pos int) int {
	switch s.at(pos) {
	case ' ', '\t', '\n', '\r':
		return 1
	case 0xC2:
		if s.at(pos+1) == 0xA0 {
			return 2
		}
	}
	return 0
}

func isQuoteChar(code int) bool { return code == '\'' || code == '"' }

// itoa writes a non-negative number.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

// parseCount reads a run of digits as a number, and stops growing at a very large one (the
// amounts are limited by the repeat guard long before).
func parseCount(digits string) int {
	const limit = 1 << 30
	n := 0
	for i := 0; i < len(digits); i++ {
		n = n*10 + int(digits[i]-'0')
		if n > limit {
			return limit
		}
	}
	return n
}
