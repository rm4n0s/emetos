package regex

import (
	"strconv"
	"strings"
	"unicode"
)

// setItem is one piece of a character class in regexp2's syntax: a range, an escape such as
// \d, or a Unicode category such as \p{Lu}.
type setItem struct {
	// text is valid inside [...].
	text string
	// inv is the piece that matches the characters text does not, or "" when there is none.
	inv string
}

// charSet is a set of characters as Oniguruma writes it: a class with nested classes, an
// intersection (&&) and negation, or an escape such as \w and \p{Alpha}. It is written in
// regexp2's syntax by expr.
type charSet struct {
	// neg makes the set the complement of what is described below.
	neg bool
	// items are the ranges and escapes of the class.
	items []setItem
	// subs are nested sets that are joined to the items by union.
	subs []*charSet
	// and, when it is not empty, makes the set the intersection of its sets (items and subs are
	// then unused).
	and []*charSet
}

// anyChar matches one character, a line break included.
const anyChar = `[\s\S]`

// simple reports whether the set is a list of items (and nothing nested).
func (s *charSet) simple() bool { return len(s.subs) == 0 && len(s.and) == 0 }

// addRange adds the characters from lo to hi.
func (s *charSet) addRange(lo, hi rune) {
	if lo == hi {
		s.items = append(s.items, setItem{text: classLiteral(lo)})
		return
	}
	s.items = append(s.items, setItem{text: classLiteral(lo) + "-" + classLiteral(hi)})
}

// addItem adds an escape and the escape of its complement.
func (s *charSet) addItem(text, inv string) {
	s.items = append(s.items, setItem{text: text, inv: inv})
}

// addSet joins the set t to the union s.
func (s *charSet) addSet(t *charSet) {
	switch {
	case t.simple() && !t.neg:
		s.items = append(s.items, t.items...)
	case t.simple() && len(t.items) == 1 && t.items[0].inv != "":
		// The complement of one escape is one escape: [^\d] is \D.
		s.items = append(s.items, setItem{text: t.items[0].inv, inv: t.items[0].text})
	default:
		s.subs = append(s.subs, t)
	}
}

// negated returns the complement of the set.
func (s *charSet) negated() *charSet {
	c := *s
	c.neg = !s.neg
	return &c
}

// expr writes the set as a pattern that matches one character of it.
func (s *charSet) expr() string {
	if s.neg && s.simple() {
		if len(s.items) == 0 {
			return anyChar
		}
		return "[^" + joinItems(s.items) + "]"
	}
	var pieces []string
	if len(s.and) > 0 {
		var sb strings.Builder
		sb.WriteString("(?:")
		for i, t := range s.and {
			if i < len(s.and)-1 {
				sb.WriteString("(?=" + t.expr() + ")")
			} else {
				sb.WriteString(t.expr())
			}
		}
		sb.WriteString(")")
		pieces = append(pieces, sb.String())
	} else {
		if len(s.items) > 0 {
			pieces = append(pieces, "["+joinItems(s.items)+"]")
		}
		for _, t := range s.subs {
			pieces = append(pieces, t.expr())
		}
	}
	var body string
	switch len(pieces) {
	case 0:
		body = "(?!)"
	case 1:
		body = pieces[0]
	default:
		body = "(?:" + strings.Join(pieces, "|") + ")"
	}
	if !s.neg {
		return body
	}
	return "(?:(?!" + body + ")" + anyChar + ")"
}

// joinItems concatenates the text of items.
func joinItems(items []setItem) string {
	var sb strings.Builder
	for _, it := range items {
		sb.WriteString(it.text)
	}
	return sb.String()
}

// classLiteral writes r as a character of a class, escaped where the class would read it as
// syntax.
func classLiteral(r rune) string {
	switch r {
	case '\\', ']', '[', '^', '-':
		return `\` + string(r)
	}
	return literalText(r)
}

// literalText writes r so that regexp2 reads it back as r: control characters as \x escapes
// and everything else as it is.
func literalText(r rune) string {
	if r < 0x20 || r == 0x7f {
		return `\x` + hex2(int(r))
	}
	return string(r)
}

// patternLiteral writes r as a character outside a class.
func patternLiteral(r rune) string {
	switch r {
	case '\\', '.', '*', '+', '?', '(', ')', '[', ']', '{', '}', '|', '^', '$', '#', ' ':
		return `\` + string(r)
	}
	return literalText(r)
}

// hex2 writes n as two hex digits.
func hex2(n int) string {
	s := strconv.FormatInt(int64(n), 16)
	if len(s) < 2 {
		s = "0" + s
	}
	return s
}

// setOfTable makes the set of the characters of a Go range table.
func setOfTable(t *unicode.RangeTable) *charSet {
	s := &charSet{}
	var lo, hi rune = -1, -1
	flush := func() {
		if lo >= 0 {
			s.addRange(lo, hi)
		}
	}
	add := func(a, b rune) {
		// Surrogates cannot be written in a Go string.
		for _, r := range [][2]rune{{a, min(b, 0xD7FF)}, {max(a, 0xE000), b}} {
			if r[0] > r[1] {
				continue
			}
			if lo >= 0 && r[0] == hi+1 {
				hi = r[1]
				continue
			}
			flush()
			lo, hi = r[0], r[1]
		}
	}
	for _, r := range t.R16 {
		if r.Stride == 1 {
			add(rune(r.Lo), rune(r.Hi))
			continue
		}
		for c := rune(r.Lo); c <= rune(r.Hi); c += rune(r.Stride) {
			add(c, c)
		}
	}
	for _, r := range t.R32 {
		if r.Stride == 1 {
			add(rune(r.Lo), rune(r.Hi))
			continue
		}
		for c := rune(r.Lo); c <= rune(r.Hi); c += rune(r.Stride) {
			add(c, c)
		}
	}
	flush()
	return s
}
