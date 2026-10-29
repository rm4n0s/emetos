package playground

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
)

// tokKind is the kind of a token.
type tokKind uint8

const (
	tkEOF    tokKind = iota
	tkIdent          // a name: text
	tkString         // a string literal: str is its value
	tkNumber         // a number literal: text is how it was written
	tkRegex          // a regular expression literal: str is the pattern and text the flags
	tkPunct          // one character that is not part of the other kinds: text
	tkError          // something that cannot be read: err says what
)

// token is a piece of the source.
type token struct {
	kind       tokKind
	start, end int
	text, str  string
	// nl says that a line break came between the token before and this one.
	nl  bool
	err *errors.Error
}

// lexer reads tokens one at a time, on request, from a position that the parser can move (a regular expression and the name
// after "use" are read in the context where they can be told apart).
type lexer struct {
	src string
	pos int
	idx *lineIndex
}

// meta is the metadata that says where an error is.
func (lx *lexer) meta(start, end int) []any {
	p, q := lx.idx.pos(start), lx.idx.pos(end)
	return []any{"offset", start, "end", end, "line", p.Line, "column", p.Column, "endLine", q.Line, "endColumn", q.Column}
}

// isLineBreak tells whether the rune ends a line for JavaScript.
func isLineBreak(r rune) bool { return r == '\n' || r == '\r' || r == 0x2028 || r == 0x2029 }

// isIdentStart and isIdentPart tell what an identifier is made of.
func isIdentStart(r rune) bool {
	return r == '_' || r == '$' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= 0x80 && unicode.IsLetter(r))
}

func isIdentPart(r rune) bool { return isIdentStart(r) || (r >= '0' && r <= '9') }

// skipSpace skips white space and comments and says whether a line break was among them. A comment that never ends is returned as an
// error token.
func (lx *lexer) skipSpace() (nl bool, bad *token) {
	for lx.pos < len(lx.src) {
		r, size := utf8.DecodeRuneInString(lx.src[lx.pos:])
		switch {
		case isLineBreak(r):
			nl = true
			lx.pos += size
		case r == ' ' || r == '\t' || r == '\v' || r == '\f' || r == 0xA0 || r == 0xFEFF || unicode.Is(unicode.Zs, r):
			lx.pos += size
		case r == '/' && strings.HasPrefix(lx.src[lx.pos:], "//"):
			for lx.pos < len(lx.src) {
				c, n := utf8.DecodeRuneInString(lx.src[lx.pos:])
				if isLineBreak(c) {
					break
				}
				lx.pos += n
			}
		case r == '/' && strings.HasPrefix(lx.src[lx.pos:], "/*"):
			start := lx.pos
			end := strings.Index(lx.src[lx.pos+2:], "*/")
			if end < 0 {
				lx.pos = len(lx.src)
				return nl, &token{kind: tkError, start: start, end: lx.pos, nl: nl,
					err: errors.New("MongoParseUnterminatedComment", "a comment that starts with /* has no */", lx.meta(start, lx.pos)...)}
			}
			body := lx.src[lx.pos : lx.pos+2+end+2]
			if strings.ContainsAny(body, "\n\r\u2028\u2029") {
				nl = true
			}
			lx.pos += len(body)
		default:
			return nl, nil
		}
	}
	return nl, nil
}

// next reads the next token. At the end of the source it returns tkEOF for ever.
func (lx *lexer) next() token {
	nl, bad := lx.skipSpace()
	if bad != nil {
		return *bad
	}
	if lx.pos >= len(lx.src) {
		return token{kind: tkEOF, start: len(lx.src), end: len(lx.src), nl: nl}
	}
	start := lx.pos
	r, size := utf8.DecodeRuneInString(lx.src[start:])
	switch {
	case r == utf8.RuneError && size <= 1:
		lx.pos += size
		return token{kind: tkError, start: start, end: lx.pos, nl: nl,
			err: errors.New("MongoParseUnexpectedCharacter", "the text is not valid UTF-8 here", lx.meta(start, lx.pos)...)}
	case isIdentStart(r):
		lx.pos += size
		for lx.pos < len(lx.src) {
			c, n := utf8.DecodeRuneInString(lx.src[lx.pos:])
			if !isIdentPart(c) {
				break
			}
			lx.pos += n
		}
		return token{kind: tkIdent, start: start, end: lx.pos, text: lx.src[start:lx.pos], nl: nl}
	case r >= '0' && r <= '9', r == '.' && start+1 < len(lx.src) && lx.src[start+1] >= '0' && lx.src[start+1] <= '9':
		return lx.number(start, nl)
	case r == '"' || r == '\'':
		return lx.str(start, r, nl)
	case r < 0x80 && r > ' ':
		lx.pos += size
		return token{kind: tkPunct, start: start, end: lx.pos, text: string(r), nl: nl}
	}
	lx.pos += size
	return token{kind: tkError, start: start, end: lx.pos, nl: nl,
		err: errors.New("MongoParseUnexpectedCharacter", "this character is not part of a playground", append(lx.meta(start, lx.pos), "character", string(r))...)}
}

// number reads a number literal: decimal with a fraction and an exponent, or hexadecimal.
func (lx *lexer) number(start int, nl bool) token {
	i := start
	src := lx.src
	switch {
	case strings.HasPrefix(src[i:], "0x") || strings.HasPrefix(src[i:], "0X"):
		i += 2
		for i < len(src) && isHexDigit(src[i]) {
			i++
		}
	default:
		for i < len(src) && src[i] >= '0' && src[i] <= '9' {
			i++
		}
		if i < len(src) && src[i] == '.' {
			i++
			for i < len(src) && src[i] >= '0' && src[i] <= '9' {
				i++
			}
		}
		if i < len(src) && (src[i] == 'e' || src[i] == 'E') {
			j := i + 1
			if j < len(src) && (src[j] == '+' || src[j] == '-') {
				j++
			}
			if j < len(src) && src[j] >= '0' && src[j] <= '9' {
				for j < len(src) && src[j] >= '0' && src[j] <= '9' {
					j++
				}
				i = j
			}
		}
	}
	// a name glued to a number (5n, 10px, 0x) is not a number
	if i < len(src) {
		if c, _ := utf8.DecodeRuneInString(src[i:]); isIdentPart(c) || (i == start+2 && (src[start+1] == 'x' || src[start+1] == 'X')) {
			for i < len(src) {
				c, n := utf8.DecodeRuneInString(src[i:])
				if !isIdentPart(c) && c != '.' {
					break
				}
				i += n
			}
			lx.pos = i
			return token{kind: tkError, start: start, end: i, nl: nl,
				err: errors.New("MongoParseInvalidNumber", "this is not a number (BigInt and units are not supported; use NumberLong(\"...\") for a 64-bit integer)", lx.meta(start, i)...)}
		}
	} else if i == start+2 && (src[start+1] == 'x' || src[start+1] == 'X') {
		lx.pos = i
		return token{kind: tkError, start: start, end: i, nl: nl,
			err: errors.New("MongoParseInvalidNumber", "a hexadecimal number needs digits after 0x", lx.meta(start, i)...)}
	}
	lx.pos = i
	return token{kind: tkNumber, start: start, end: i, text: src[start:i], nl: nl}
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// str reads a string literal that starts with the quote at start.
func (lx *lexer) str(start int, quote rune, nl bool) token {
	var b strings.Builder
	i := start + 1
	src := lx.src
	for i < len(src) {
		c, size := utf8.DecodeRuneInString(src[i:])
		switch c {
		case quote:
			lx.pos = i + size
			return token{kind: tkString, start: start, end: lx.pos, str: b.String(), nl: nl}
		case '\n', '\r':
			lx.pos = i
			return token{kind: tkError, start: start, end: i, nl: nl,
				err: errors.New("MongoParseUnterminatedString", "a string ends at the end of its line without a closing quote", lx.meta(start, i)...)}
		case '\\':
			i += size
			if i >= len(src) {
				continue
			}
			e, esize := utf8.DecodeRuneInString(src[i:])
			switch e {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case 'r':
				b.WriteByte('\r')
			case 'b':
				b.WriteByte('\b')
			case 'f':
				b.WriteByte('\f')
			case 'v':
				b.WriteByte('\v')
			case '0':
				if i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9' {
					lx.pos = lineEnd(src, i)
					return token{kind: tkError, start: start, end: lx.pos, nl: nl,
						err: errors.New("MongoParseInvalidEscape", "octal escapes are not supported", lx.meta(i-1, i+2)...)}
				}
				b.WriteByte(0)
			case 'x', 'u':
				r, n, ok := lx.hexEscape(src[i:])
				if !ok {
					lx.pos = lineEnd(src, i)
					return token{kind: tkError, start: start, end: lx.pos, nl: nl,
						err: errors.New("MongoParseInvalidEscape", "\\x needs two hexadecimal digits, \\u four or {digits}", lx.meta(i-1, min(i+1+n, len(src)))...)}
				}
				b.WriteRune(r)
				i += n
				continue
			case '\r':
				if i+1 < len(src) && src[i+1] == '\n' {
					i++
				}
			case '\n', 0x2028, 0x2029:
				// a backslash before a line break continues the string on the next line
			default:
				b.WriteRune(e)
			}
			i += esize
		default:
			b.WriteRune(c)
			i += size
		}
	}
	lx.pos = len(src)
	return token{kind: tkError, start: start, end: len(src), nl: nl,
		err: errors.New("MongoParseUnterminatedString", "a string ends with the end of the text without a closing quote", lx.meta(start, len(src))...)}
}

// lineEnd is the offset of the end of the line that has the offset.
func lineEnd(src string, from int) int {
	for i := from; i < len(src); i++ {
		if src[i] == '\n' || src[i] == '\r' {
			return i
		}
	}
	return len(src)
}

// hexEscape reads what follows the backslash of \x41, A and \u{1F600}: s starts at the x or the u. It returns the rune, how
// many bytes it took (the x or u included) and whether it was well formed.
func (lx *lexer) hexEscape(s string) (rune, int, bool) {
	digits := func(from, n int) (int64, bool) {
		if len(s) < from+n {
			return 0, false
		}
		for _, c := range []byte(s[from : from+n]) {
			if !isHexDigit(c) {
				return 0, false
			}
		}
		v, err := strconv.ParseInt(s[from:from+n], 16, 32)
		return v, err == nil
	}
	switch {
	case s[0] == 'x':
		v, ok := digits(1, 2)
		return rune(v), 3, ok
	case strings.HasPrefix(s, "u{"):
		end := strings.IndexByte(s, '}')
		if end < 3 || end > 8 {
			return 0, 1, false
		}
		v, err := strconv.ParseInt(s[2:end], 16, 32)
		if err != nil || v > unicode.MaxRune || (v >= 0xD800 && v <= 0xDFFF) {
			return 0, end + 1, false
		}
		return rune(v), end + 1, true
	default:
		v, ok := digits(1, 4)
		if !ok {
			return 0, 1, false
		}
		if v >= 0xD800 && v < 0xDC00 { // a high surrogate: a low one must follow as \uXXXX
			if strings.HasPrefix(s[5:], "\\u") {
				if lo, ok := digits(7, 4); ok && lo >= 0xDC00 && lo < 0xE000 {
					return 0x10000 + (rune(v)-0xD800)<<10 + (rune(lo) - 0xDC00), 11, true
				}
			}
			return utf8.RuneError, 5, true
		}
		if v >= 0xDC00 && v < 0xE000 {
			return utf8.RuneError, 5, true
		}
		return rune(v), 5, true
	}
}

// regex reads a regular expression literal that starts with the slash at start: /pattern/flags.
func (lx *lexer) regex(start int, nl bool) token {
	src := lx.src
	i := start + 1
	inClass := false
	for i < len(src) {
		c := src[i]
		switch {
		case c == '\n' || c == '\r':
			lx.pos = i
			return token{kind: tkError, start: start, end: i, nl: nl,
				err: errors.New("MongoParseUnterminatedRegex", "a regular expression ends at the end of its line without a closing slash", lx.meta(start, i)...)}
		case c == '\\':
			i += 2
			continue
		case c == '[':
			inClass = true
		case c == ']':
			inClass = false
		case c == '/' && !inClass:
			pattern := unescapeSlashes(src[start+1 : i])
			i++
			fstart := i
			for i < len(src) && isIdentPart(rune(src[i])) {
				i++
			}
			flags := src[fstart:i]
			lx.pos = i
			if pattern == "" {
				return token{kind: tkError, start: start, end: i, nl: nl,
					err: errors.New("MongoParseUnterminatedRegex", "a regular expression is not empty", lx.meta(start, i)...)}
			}
			for _, f := range flags {
				if !strings.ContainsRune("imsxu", f) || strings.Count(flags, string(f)) > 1 {
					return token{kind: tkError, start: start, end: i, nl: nl,
						err: errors.New("MongoParseInvalidRegexFlags", "the flags of a regular expression are i, m, s, x and u, each once", append(lx.meta(fstart, i), "flags", flags)...)}
				}
			}
			return token{kind: tkRegex, start: start, end: i, str: pattern, text: flags, nl: nl}
		}
		i++
	}
	lx.pos = len(src)
	return token{kind: tkError, start: start, end: len(src), nl: nl,
		err: errors.New("MongoParseUnterminatedRegex", "a regular expression has no closing slash", lx.meta(start, len(src))...)}
}

// unescapeSlashes turns \/ of the source of a regular expression into /, which means the same and reads back the same.
func unescapeSlashes(pattern string) string {
	if !strings.Contains(pattern, "\\/") {
		return pattern
	}
	var b strings.Builder
	for i := 0; i < len(pattern); i++ {
		if pattern[i] == '\\' && i+1 < len(pattern) {
			if pattern[i+1] != '/' {
				b.WriteByte('\\')
			}
			b.WriteByte(pattern[i+1])
			i++
			continue
		}
		b.WriteByte(pattern[i])
	}
	return b.String()
}
