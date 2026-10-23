package brackets

import (
	"slices"
	"sort"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/language"
)

// Ported from VS Code: reference/vscode/src/vs/editor/common/languages/supports/languageBracketsConfiguration.ts and
// reference/vscode/src/vs/editor/common/model/bracketPairsTextModelPart/bracketPairsTree/brackets.ts (MIT)

// Pair is a pair of bracket texts, as a language configuration lists them.
type Pair = language.Pair

// Bracket is one bracket text of a language, an opening or a closing one. The texts are in lower
// case: brackets are found without regard to case, as VS Code does. A text that two pairs use,
// as the closing bracket of both, is one Bracket (`endcase` closes `case` and `casez`), and a
// text that is the opening of one pair and the closing of another is a closing bracket.
type Bracket struct {
	// Text is the text of the bracket, in lower case.
	Text string
	// Opening is true for an opening bracket.
	Opening bool

	lang      *Language
	closes    []*Bracket // the opening brackets that a closing bracket closes, in order of definition
	colorized []bool     // for each of them, whether its pair is colorized
	// wordStart and wordEnd tell that the text starts or ends with a word character (or a space),
	// so that the bracket is found only at the edge of a word: `end` is not in `friend`.
	wordStart, wordEnd bool
}

// Language returns the language the bracket belongs to.
func (b *Bracket) Language() *Language { return b.lang }

// Closes reports whether b is a closing bracket of the pair that opening begins.
func (b *Bracket) Closes(opening *Bracket) bool {
	if b.Opening || opening == nil {
		return false
	}
	// Most closing brackets close one opening bracket.
	if len(b.closes) == 1 {
		return b.closes[0] == opening
	}
	return slices.Contains(b.closes, opening)
}

// ClosesColorized reports whether b closes opening in a pair whose levels are colored.
func (b *Bracket) ClosesColorized(opening *Bracket) bool {
	if b.Opening {
		return false
	}
	for i, o := range b.closes {
		if o == opening {
			return b.colorized[i]
		}
	}
	return false
}

// Openings returns the opening brackets that b, a closing bracket, closes.
func (b *Bracket) Openings() []*Bracket { return b.closes }

// Language holds the brackets of one language (SPEC §8.5). It is built from a language
// configuration and is read-only afterwards, so it can be shared.
type Language struct {
	code  int
	byTxt map[string]*Bracket
	// first lists, for each byte, the brackets that start with it (in either case) in the order of
	// VS Code's regular expression: the texts sorted, then reversed, so that a longer text comes
	// before the texts that are a prefix of it.
	first [256][]*Bracket
	all   []*Bracket
}

// NewLanguage makes the brackets of the language with the given code from the pairs of
// "brackets" and "colorizedBracketPairs" of its configuration. The colorized pairs are brackets
// too. When colorizedDefined is false the pairs of "brackets" are colorized, except `<` `>`,
// which many languages use as a comparison.
func NewLanguage(code int, brackets, colorized []Pair, colorizedDefined bool) *Language {
	l := &Language{code: code, byTxt: map[string]*Bracket{}}
	openings := map[string]*Bracket{}
	closings := map[string]*Bracket{}
	opening := func(text string) *Bracket {
		b, ok := openings[text]
		if !ok {
			b = &Bracket{Text: text, Opening: true, lang: l}
			openings[text] = b
		}
		return b
	}
	closing := func(text string) *Bracket {
		b, ok := closings[text]
		if !ok {
			b = &Bracket{Text: text, lang: l}
			closings[text] = b
		}
		return b
	}
	valid := func(pairs []Pair) []Pair {
		var out []Pair
		for _, p := range pairs {
			if p.Open != "" && p.Close != "" {
				out = append(out, Pair{Open: strings.ToLower(p.Open), Close: strings.ToLower(p.Close)})
			}
		}
		return out
	}
	add := func(p Pair, color bool) {
		o, c := opening(p.Open), closing(p.Close)
		i := slices.Index(c.closes, o)
		if i < 0 {
			c.closes = append(c.closes, o)
			c.colorized = append(c.colorized, false)
			i = len(c.closes) - 1
		}
		if color {
			c.colorized[i] = true
		}
	}
	plain := valid(brackets)
	for _, p := range plain {
		add(p, false)
	}
	var colored []Pair
	if colorizedDefined {
		colored = valid(colorized)
	} else {
		for _, p := range plain {
			if p.Open != "<" || p.Close != ">" {
				colored = append(colored, p)
			}
		}
	}
	for _, p := range colored {
		add(p, true)
	}
	for text, b := range openings {
		l.byTxt[text] = b
	}
	// A text that is a closing bracket wins over the same text as an opening bracket.
	for text, b := range closings {
		l.byTxt[text] = b
	}
	texts := make([]string, 0, len(l.byTxt))
	for text := range l.byTxt {
		texts = append(texts, text)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(texts)))
	for _, text := range texts {
		b := l.byTxt[text]
		b.wordStart = isWordOrSpace(text[0])
		b.wordEnd = isWordOrSpace(text[len(text)-1])
		l.all = append(l.all, b)
		first := text[0]
		l.first[first] = append(l.first[first], b)
		if other := swapCase(first); other != first {
			l.first[other] = append(l.first[other], b)
		}
	}
	return l
}

// FromConfiguration makes the brackets of the language with the given code from its language
// configuration, which may be nil.
func FromConfiguration(code int, c *language.Configuration) *Language {
	if c == nil {
		return NewLanguage(code, nil, nil, false)
	}
	return NewLanguage(code, c.Brackets, c.ColorizedBracketPairs, c.ColorizedBracketPairs != nil)
}

// Code returns the code of the language.
func (l *Language) Code() int { return l.code }

// Empty reports whether the language has no brackets.
func (l *Language) Empty() bool { return l == nil || len(l.byTxt) == 0 }

// Lookup returns the bracket with the text (in any case), or nil.
func (l *Language) Lookup(text string) *Bracket {
	if l == nil {
		return nil
	}
	return l.byTxt[strings.ToLower(text)]
}

// Brackets returns all the brackets of the language, in the order in which they are searched
// for.
func (l *Language) Brackets() []*Bracket {
	if l == nil {
		return nil
	}
	return l.all
}

// Strip returns text without the brackets of the language, found as in a token: the edges of the
// text count as the edges of a word (VS Code's replace of the global bracket regular expression
// by nothing).
func (l *Language) Strip(text string) string {
	if l.Empty() {
		return text
	}
	var sb strings.Builder
	last, from := 0, 0
	for from < len(text) {
		at, b := l.findFrom(text, from)
		if b == nil {
			break
		}
		sb.WriteString(text[last:at])
		last = at + len(b.Text)
		from = last
	}
	if last == 0 {
		return text
	}
	sb.WriteString(text[last:])
	return sb.String()
}

// find returns the first bracket in text and its offset, or nil. The edges of the text count as
// the edges of a word, as in VS Code, which searches the text of one token.
func (l *Language) find(text string) (int, *Bracket) { return l.findFrom(text, 0) }

// findFrom is find, starting at the byte offset from; the text before it still decides where a
// word begins.
func (l *Language) findFrom(text string, from int) (int, *Bracket) {
	if l == nil || len(l.all) == 0 {
		return 0, nil
	}
	for i := from; i < len(text); i++ {
		for _, b := range l.first[text[i]] {
			if !hasPrefixFold(text[i:], b.Text) {
				continue
			}
			if b.wordStart && isWordAt(text, i-1) == isWordAt(text, i) {
				continue
			}
			end := i + len(b.Text)
			if b.wordEnd && isWordAt(text, end-1) == isWordAt(text, end) {
				continue
			}
			return i, b
		}
	}
	return 0, nil
}

// isWordAt reports whether the byte at i is a word character; outside the text it is not.
func isWordAt(text string, i int) bool {
	return i >= 0 && i < len(text) && isWord(text[i])
}

func isWord(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func isWordOrSpace(c byte) bool { return c == ' ' || isWord(c) }

func swapCase(c byte) byte {
	switch {
	case c >= 'a' && c <= 'z':
		return c - 'a' + 'A'
	case c >= 'A' && c <= 'Z':
		return c - 'A' + 'a'
	}
	return c
}

// hasPrefixFold reports whether s starts with prefix, a lower-case text, ignoring the case of
// ASCII letters.
func hasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c != prefix[i] {
			return false
		}
	}
	return true
}
