package search

import (
	"bytes"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/find"
	"github.com/rm4n0s/emetos/internal/core/regex"
)

// regexTimeout is the longest a regular expression may run on a line or a file before the search gives up on that file.
const regexTimeout = 2 * time.Second

// Query says what to look for (VS Code's IPatternInfo).
type Query struct {
	// Pattern is the text, or the regular expression when IsRegExp is set. It must not be empty.
	Pattern string
	// IsRegExp says that Pattern is a regular expression in JavaScript's syntax.
	IsRegExp bool
	// IsCaseSensitive makes the search match case.
	IsCaseSensitive bool
	// IsWordMatch finds whole words only.
	IsWordMatch bool
	// IsSmartCase (search.smartCase) makes the search case sensitive when Pattern has an uppercase letter (for a regular
	// expression: an unescaped one), as VS Code's query builder does.
	IsSmartCase bool
	// IsMultiline lets the search match across lines; it is on anyway for a Pattern with a line break and for a regular
	// expression that can match one.
	IsMultiline bool
}

// CaseSensitive tells whether the search matches case, once IsSmartCase is applied.
func (q Query) CaseSensitive() bool {
	if q.IsSmartCase && containsUppercase(q.Pattern, q.IsRegExp) {
		return true
	}
	return q.IsCaseSensitive
}

// Multiline tells whether the search runs over the whole file instead of line by line.
func (q Query) Multiline() bool {
	return q.IsMultiline || strings.Contains(q.Pattern, "\n") || (q.IsRegExp && find.IsMultilineRegexSource(q.Pattern))
}

// containsUppercase is VS Code's containsUppercaseCharacter: a regular expression is read without its escapes (\S is not a
// capital letter of the user).
func containsUppercase(s string, ignoreEscaped bool) bool {
	if ignoreEscaped {
		var b strings.Builder
		for i := 0; i < len(s); i++ {
			if s[i] == '\\' && i+1 < len(s) {
				i++
				continue
			}
			b.WriteByte(s[i])
		}
		s = b.String()
	}
	return strings.ToLower(s) != s
}

// Validate compiles the query and returns the error of a regular expression that does not compile (the error of
// internal/core/regex, with its tag: RegexUnbalancedParen, RegexUnterminatedClass, ...) or of an empty pattern
// (SearchPatternEmpty).
func (q Query) Validate() error {
	_, err := compile(q)
	return err
}

// matcher finds the matches of a Query in the text of a file.
type matcher struct {
	q         Query
	literal   []byte
	fold      bool
	word      bool
	wordFirst bool // the literal starts with a word character: it needs a boundary before it
	wordLast  bool
	re        *regex.Regexp
	multiline bool
}

// compile prepares the query.
func compile(q Query) (*matcher, error) {
	if q.Pattern == "" {
		return nil, errors.New("SearchPatternEmpty", "there is nothing to search for")
	}
	m := &matcher{q: q, word: q.IsWordMatch, multiline: q.Multiline()}
	cs := q.CaseSensitive()
	if !q.IsRegExp && !m.multiline && (cs || isASCII(q.Pattern)) {
		m.literal = []byte(q.Pattern)
		if !cs {
			m.fold = true
			for i, c := range m.literal {
				m.literal[i] = lowerASCII(c)
			}
		}
		first, _ := utf8.DecodeRuneInString(q.Pattern)
		last, _ := utf8.DecodeLastRuneInString(q.Pattern)
		m.wordFirst, m.wordLast = isWordRune(first), isWordRune(last)
		return m, nil
	}
	pattern := regexSource(q)
	re, err := regex.Compile(pattern, regex.Options{
		Syntax: regex.ECMAScript, IgnoreCase: !cs, Multiline: true, Unicode: true, Timeout: regexTimeout,
	})
	if err != nil {
		return nil, err // an error of internal/core/regex, with its own tag (RegexUnbalancedParen, ...)
	}
	m.re = re
	return m, nil
}

// regexSource is the regular expression of a query that is not a plain literal: the pattern, escaped when the query is
// not a regular expression, with a word boundary at the ends for a whole word search (VS Code's createRegExp).
func regexSource(q Query) string {
	pattern := q.Pattern
	if !q.IsRegExp {
		pattern = find.EscapeRegExp(pattern)
	}
	if q.IsWordMatch {
		if c := pattern[0]; isASCIIWord(c) {
			pattern = `\b` + pattern
		}
		if c := pattern[len(pattern)-1]; isASCIIWord(c) {
			pattern += `\b`
		}
	}
	return pattern
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

func lowerASCII(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}

func upperASCII(c byte) byte {
	if 'a' <= c && c <= 'z' {
		return c - 'a' + 'A'
	}
	return c
}

func isASCIIWord(c byte) bool {
	return c == '_' || '0' <= c && c <= '9' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

func isWordRune(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// scratch is the memory a worker reuses between files.
type scratch struct {
	runes []rune
}

// scan calls emit with the byte range of each match in data, in order and without overlaps, until emit returns false. data is
// valid UTF-8 with "\n" at the end of lines (a "\r" before it is allowed, except in a multiline search, where it was removed).
// An error is the timeout of a regular expression.
func (m *matcher) scan(data []byte, sc *scratch, emit func(start, end int) bool) error {
	switch {
	case m.literal != nil:
		m.scanLiteral(data, emit)
		return nil
	case m.multiline:
		return m.scanWhole(data, sc, emit)
	}
	return m.scanLines(data, sc, emit)
}

func (m *matcher) scanLiteral(data []byte, emit func(start, end int) bool) {
	lit := m.literal
	pos := 0
	var fs foldSearch
	if m.fold {
		fs = newFoldSearch(lit)
	}
	for pos+len(lit) <= len(data) {
		var i int
		if m.fold {
			i = fs.index(data, pos)
		} else if i = bytes.Index(data[pos:], lit); i >= 0 {
			i += pos
		}
		if i < 0 {
			return
		}
		end := i + len(lit)
		if m.word && !m.wordBoundaries(data, i, end) {
			pos = i + 1
			continue
		}
		if !emit(i, end) {
			return
		}
		pos = end
	}
}

// wordBoundaries tells whether the text data[start:end] stands alone as a word where it starts or ends with a word character.
func (m *matcher) wordBoundaries(data []byte, start, end int) bool {
	if m.wordFirst && start > 0 {
		if r, _ := utf8.DecodeLastRune(data[:start]); isWordRune(r) {
			return false
		}
	}
	if m.wordLast && end < len(data) {
		if r, _ := utf8.DecodeRune(data[end:]); isWordRune(r) {
			return false
		}
	}
	return true
}

// foldSearch finds a text in lower case ASCII in data without regard to case: it looks for the first byte in both cases, with
// the vectorized IndexByte, and compares the rest.
type foldSearch struct {
	lit    []byte
	lo, up byte
}

func newFoldSearch(lit []byte) foldSearch {
	return foldSearch{lit: lit, lo: lit[0], up: upperASCII(lit[0])}
}

// indexByteFrom is the offset in data of the first c at or after from, or len(data).
func indexByteFrom(data []byte, c byte, from int) int {
	if i := bytes.IndexByte(data[from:], c); i >= 0 {
		return from + i
	}
	return len(data)
}

// index returns the offset of the first match at or after from, or -1.
func (f foldSearch) index(data []byte, from int) int {
	n := len(f.lit)
	last := len(data) - n
	nl := indexByteFrom(data, f.lo, from)
	nu := nl
	if f.up != f.lo {
		nu = indexByteFrom(data, f.up, from)
	}
	for {
		p := min(nl, nu)
		if p > last {
			return -1
		}
		if equalFoldLower(data[p+1:p+n], f.lit[1:]) {
			return p
		}
		if nl == p {
			nl = indexByteFrom(data, f.lo, p+1)
		}
		if nu == p {
			nu = indexByteFrom(data, f.up, p+1)
		}
	}
}

// equalFoldLower compares data with lit, which is in lower case ASCII.
func equalFoldLower(data, lit []byte) bool {
	for i, c := range lit {
		if lowerASCII(data[i]) != c {
			return false
		}
	}
	return true
}

// scanLines runs the regular expression on each line.
func (m *matcher) scanLines(data []byte, sc *scratch, emit func(start, end int) bool) error {
	for ls := 0; ls < len(data); {
		le := bytes.IndexByte(data[ls:], '\n')
		next := 0
		if le < 0 {
			le, next = len(data), len(data)
		} else {
			le += ls
			next = le + 1
		}
		line := data[ls:le]
		if n := len(line); n > 0 && line[n-1] == '\r' {
			line = line[:n-1]
		}
		if len(line) > 0 {
			runes, ascii := sc.fill(line)
			from := 0
			for from <= len(runes) {
				mt, ok, err := m.re.Find(runes, from)
				if err != nil {
					return err
				}
				if !ok {
					break
				}
				s, e := mt.Groups[0].Start, mt.Groups[0].End
				if e <= s {
					from = e + 1
					continue
				}
				bs, be := s, e
				if !ascii {
					bs, be = runeBytes(runes, 0, 0, s), runeBytes(runes, 0, 0, e)
				}
				if !emit(ls+bs, ls+be) {
					return nil
				}
				from = e
			}
		}
		ls = next
	}
	return nil
}

// scanWhole runs the regular expression on the whole text.
func (m *matcher) scanWhole(data []byte, sc *scratch, emit func(start, end int) bool) error {
	runes, ascii := sc.fill(data)
	cr, cb := 0, 0 // a rune offset and the byte offset it is at, which only move forward
	toByte := func(r int) int {
		if ascii {
			return r
		}
		cb = runeBytes(runes, cr, cb, r)
		cr = r
		return cb
	}
	from := 0
	for from <= len(runes) {
		mt, ok, err := m.re.Find(runes, from)
		if err != nil {
			return err
		}
		if !ok {
			break
		}
		s, e := mt.Groups[0].Start, mt.Groups[0].End
		if e <= s {
			from = e + 1
			continue
		}
		bs := toByte(s)
		be := toByte(e)
		if !emit(bs, be) {
			return nil
		}
		from = e
	}
	return nil
}

// runeBytes is the byte offset of the rune at index to, given that the rune at index from is at byte offset fromByte.
func runeBytes(runes []rune, from, fromByte, to int) int {
	n := fromByte
	for _, r := range runes[from:to] {
		n += utf8.RuneLen(r)
	}
	return n
}

// fill converts text to runes in the scratch memory and tells whether it is all ASCII (a rune is then its byte's index).
func (sc *scratch) fill(text []byte) (runes []rune, ascii bool) {
	sc.runes = sc.runes[:0]
	ascii = true
	for i := 0; i < len(text); {
		c := text[i]
		if c < utf8.RuneSelf {
			sc.runes = append(sc.runes, rune(c))
			i++
			continue
		}
		ascii = false
		r, w := utf8.DecodeRune(text[i:])
		sc.runes = append(sc.runes, r)
		i += w
	}
	return sc.runes, ascii
}
