package main

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// The lexer of the TypeScript subset the completion specs use (SPEC §10.25 "Terminal IntelliSense"). It does not know the
// language: it cuts the text into words, strings, numbers and punctuation, skips comments and keeps a regular expression
// literal in one piece, so that the brackets inside it do not confuse the matching of brackets in the code that is skipped.

type tokKind int

const (
	tokEOF tokKind = iota
	tokIdent
	tokString   // a string with no interpolation
	tokTemplate // a template literal with ${...} in it
	tokNumber
	tokRegex
	tokPunct
)

type token struct {
	kind tokKind
	// text is the identifier, the punctuation or the decoded string.
	text string
	num  float64
}

// lex cuts src into tokens.
func lex(src string) []token {
	var out []token
	i := 0
	// regexAllowed says whether a '/' here starts a regular expression: after an operator or an opening bracket, not after a
	// value.
	regexAllowed := func() bool {
		if len(out) == 0 {
			return true
		}
		p := out[len(out)-1]
		switch p.kind {
		case tokIdent:
			switch p.text {
			case "return", "typeof", "in", "of", "case", "delete", "void", "yield", "await":
				return true
			}
			return false
		case tokString, tokTemplate, tokNumber, tokRegex:
			return false
		}
		switch p.text {
		case ")", "]", "}":
			return false
		}
		return true
	}
	for i < len(src) {
		c := src[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case strings.HasPrefix(src[i:], "//"):
			for i < len(src) && src[i] != '\n' {
				i++
			}
		case strings.HasPrefix(src[i:], "/*"):
			end := strings.Index(src[i+2:], "*/")
			if end < 0 {
				return out
			}
			i += end + 4
		case c == '"' || c == '\'':
			s, n := readString(src[i:], c)
			out = append(out, token{kind: tokString, text: s})
			i += n
		case c == '`':
			s, n, interp := readTemplate(src[i:])
			k := tokString
			if interp {
				k = tokTemplate
			}
			out = append(out, token{kind: k, text: s})
			i += n
		case c >= '0' && c <= '9' || c == '.' && i+1 < len(src) && src[i+1] >= '0' && src[i+1] <= '9':
			j := i
			for j < len(src) && (src[j] >= '0' && src[j] <= '9' || src[j] == '.' || src[j] == 'e' || src[j] == 'x' || src[j] >= 'a' && src[j] <= 'f' || src[j] == '_') {
				j++
			}
			f, _ := strconv.ParseFloat(strings.ReplaceAll(src[i:j], "_", ""), 64)
			out = append(out, token{kind: tokNumber, num: f, text: src[i:j]})
			i = j
		case c == '/' && regexAllowed():
			n := readRegex(src[i:])
			out = append(out, token{kind: tokRegex, text: src[i : i+n]})
			i += n
		default:
			r, w := utf8.DecodeRuneInString(src[i:])
			if unicode.IsLetter(r) || r == '_' || r == '$' {
				j := i + w
				for j < len(src) {
					r2, w2 := utf8.DecodeRuneInString(src[j:])
					if !unicode.IsLetter(r2) && !unicode.IsDigit(r2) && r2 != '_' && r2 != '$' {
						break
					}
					j += w2
				}
				out = append(out, token{kind: tokIdent, text: src[i:j]})
				i = j
				continue
			}
			p := punct(src[i:])
			out = append(out, token{kind: tokPunct, text: p})
			i += len(p)
		}
	}
	return out
}

// punct is the operator or bracket at the start of s; the ones that matter are =>, ... and ?. , the rest are one character.
func punct(s string) string {
	for _, p := range []string{"...", "=>", "?.", "??", "===", "!==", "&&", "||", "==", "!=", "<=", ">="} {
		if strings.HasPrefix(s, p) {
			return p
		}
	}
	_, w := utf8.DecodeRuneInString(s)
	return s[:w]
}

// readString reads the string that starts at s[0] (the quote q) and returns its value and the length of the literal.
func readString(s string, q byte) (string, int) {
	var b strings.Builder
	i := 1
	for i < len(s) {
		c := s[i]
		if c == q {
			return b.String(), i + 1
		}
		if c == '\\' && i+1 < len(s) {
			i = readEscape(s, i, &b)
			continue
		}
		if c == '\n' {
			break // an unterminated string: stop at the end of the line
		}
		b.WriteByte(c)
		i++
	}
	return b.String(), i
}

// readEscape decodes the escape at s[i] (a backslash) into b and returns the index after it.
func readEscape(s string, i int, b *strings.Builder) int {
	i++
	switch c := s[i]; c {
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
		b.WriteByte(0)
	case '\n':
		// a line continuation
	case 'u':
		if i+1 < len(s) && s[i+1] == '{' {
			if end := strings.IndexByte(s[i:], '}'); end > 0 {
				if n, err := strconv.ParseUint(s[i+2:i+end], 16, 32); err == nil {
					b.WriteRune(rune(n))
				}
				return i + end + 1
			}
		}
		if i+4 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+5], 16, 32); err == nil {
				b.WriteRune(rune(n))
				return i + 5
			}
		}
		b.WriteByte('u')
	case 'x':
		if i+2 < len(s) {
			if n, err := strconv.ParseUint(s[i+1:i+3], 16, 32); err == nil {
				b.WriteRune(rune(n))
				return i + 3
			}
		}
		b.WriteByte('x')
	default:
		b.WriteByte(c)
	}
	return i + 1
}

// readTemplate reads a template literal; interp says that it has ${...} in it (its text is then not the value).
func readTemplate(s string) (text string, n int, interp bool) {
	var b strings.Builder
	i := 1
	for i < len(s) {
		c := s[i]
		switch {
		case c == '`':
			return b.String(), i + 1, interp
		case c == '\\' && i+1 < len(s):
			i = readEscape(s, i, &b)
		case c == '$' && i+1 < len(s) && s[i+1] == '{':
			interp = true
			depth := 0
			for i < len(s) {
				if s[i] == '{' {
					depth++
				} else if s[i] == '}' {
					depth--
					if depth == 0 {
						i++
						break
					}
				}
				i++
			}
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String(), i, interp
}

// readRegex returns the length of the regular expression literal at s[0] (a slash), with its flags.
func readRegex(s string) int {
	i := 1
	inClass := false
	for i < len(s) {
		switch s[i] {
		case '\\':
			i++
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '/':
			if !inClass {
				i++
				for i < len(s) && (s[i] >= 'a' && s[i] <= 'z') {
					i++
				}
				return i
			}
		case '\n':
			return 1 // not a regular expression after all: a division
		}
		i++
	}
	return 1
}
