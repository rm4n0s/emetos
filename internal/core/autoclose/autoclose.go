// Package autoclose holds what the editor needs to know of the auto-closing pairs, the
// surrounding pairs and the "auto close before" characters of a language, and the values of the
// settings that control them (SPEC §8.6.4). It has no text and no cursors: the editing
// functions of internal/core/edit use it.
package autoclose

import (
	"strings"
	"sync"

	"github.com/rm4n0s/emetos/internal/core/language"
	"github.com/rm4n0s/emetos/internal/core/regex"
	"github.com/rm4n0s/emetos/internal/core/textmate"
)

// Strategy is the value of editor.autoClosingBrackets, editor.autoClosingQuotes and
// editor.autoClosingComments: whether a closing character is added, and before which characters.
type Strategy int

// The values of a Strategy; the zero value is "languageDefined".
const (
	// LanguageDefined closes before the characters the language lists (autoCloseBefore).
	LanguageDefined Strategy = iota
	// BeforeWhitespace closes only before white space or the end of the line.
	BeforeWhitespace
	// Always closes whatever follows the cursor.
	Always
	// Never does not close.
	Never
)

// ParseStrategy reads a Strategy; false when s is not one of its values.
func ParseStrategy(s string) (Strategy, bool) {
	switch s {
	case "languageDefined":
		return LanguageDefined, true
	case "beforeWhitespace":
		return BeforeWhitespace, true
	case "always":
		return Always, true
	case "never":
		return Never, true
	}
	return LanguageDefined, false
}

// String is the value of the setting.
func (s Strategy) String() string {
	switch s {
	case BeforeWhitespace:
		return "beforeWhitespace"
	case Always:
		return "always"
	case Never:
		return "never"
	}
	return "languageDefined"
}

// Behavior is the value of editor.autoClosingDelete and editor.autoClosingOvertype: whether
// Backspace removes a closing character with its opening one, and whether typing a closing
// character types over the one after the cursor.
type Behavior int

// The values of a Behavior; the zero value is "auto".
const (
	// Auto acts on the closing characters the editor inserted.
	Auto Behavior = iota
	// AlwaysAct acts on every closing character.
	AlwaysAct
	// NeverAct does not act.
	NeverAct
)

// ParseBehavior reads a Behavior ("auto", "always" or "never"); false when s is none of them.
func ParseBehavior(s string) (Behavior, bool) {
	switch s {
	case "auto":
		return Auto, true
	case "always":
		return AlwaysAct, true
	case "never":
		return NeverAct, true
	}
	return Auto, false
}

// String is the value of the setting.
func (b Behavior) String() string {
	switch b {
	case AlwaysAct:
		return "always"
	case NeverAct:
		return "never"
	}
	return "auto"
}

// Surround is the value of editor.autoSurround: what typing a quote or a bracket over a
// selection does.
type Surround int

// The values of a Surround; the zero value is "languageDefined".
const (
	// SurroundLanguageDefined surrounds with brackets and quotes.
	SurroundLanguageDefined Surround = iota
	// SurroundQuotes surrounds with quotes only.
	SurroundQuotes
	// SurroundBrackets surrounds with brackets only.
	SurroundBrackets
	// SurroundNever replaces the selection.
	SurroundNever
)

// ParseSurround reads a Surround; false when s is not one of its values.
func ParseSurround(s string) (Surround, bool) {
	switch s {
	case "languageDefined":
		return SurroundLanguageDefined, true
	case "quotes":
		return SurroundQuotes, true
	case "brackets":
		return SurroundBrackets, true
	case "never":
		return SurroundNever, true
	}
	return SurroundLanguageDefined, false
}

// String is the value of the setting.
func (s Surround) String() string {
	switch s {
	case SurroundQuotes:
		return "quotes"
	case SurroundBrackets:
		return "brackets"
	case SurroundNever:
		return "never"
	}
	return "languageDefined"
}

// The characters a pair is closed before when the language lists none.
const (
	defaultBeforeQuotes   = ";:.,=}])> \n\t"
	defaultBeforeBrackets = "'\"`;:.,=}])> \n\t"
	whitespace            = " \n\t"
)

// IsQuote reports whether ch is a quote character: ', " or `.
func IsQuote(ch string) bool { return ch == "'" || ch == "\"" || ch == "`" }

// Pair is a pair that is closed when its opening is typed, with the conditions of its language
// configuration (VS Code's StandardAutoClosingPairConditional).
type Pair struct {
	// Open and Close are the texts of the pair.
	Open, Close string

	beforeText                   *regex.Regexp
	inString, inComment, inRegEx bool
	neutral                      string
}

func newPair(p language.AutoClosingPair) *Pair {
	pair := &Pair{Open: p.Open, Close: p.Close, beforeText: p.BeforeText, inString: true, inComment: true, inRegEx: true}
	for _, notIn := range p.NotIn {
		switch notIn {
		case "string":
			pair.inString = false
		case "comment":
			pair.inComment = false
		case "regex":
			pair.inRegEx = false
		}
	}
	pair.neutral = pair.findNeutral()
	return pair
}

// BeforeTextMatches reports whether the text before the cursor, with the typed character, is one
// the pair is closed after; it is always true for a pair without a beforeText.
func (p *Pair) BeforeTextMatches(text string) bool {
	if p.beforeText == nil {
		return true
	}
	_, ok, err := p.beforeText.Find([]rune(text), 0)
	return ok && err == nil
}

// OK reports whether the pair is closed in a token of the type.
func (p *Pair) OK(t textmate.TokenType) bool {
	switch t {
	case textmate.TokenComment:
		return p.inComment
	case textmate.TokenString:
		return p.inString
	case textmate.TokenRegEx:
		return p.inRegEx
	}
	return true
}

// Conditional reports whether the pair is closed only in some kinds of token.
func (p *Pair) Conditional() bool { return !p.inString || !p.inComment || !p.inRegEx }

// Neutral is a character that is in neither text of the pair, "" when the pair is closed in
// every kind of token: typing it where the quote would go shows what kind of token the quote
// would be in.
func (p *Pair) Neutral() string {
	if !p.Conditional() {
		return ""
	}
	return p.neutral
}

func (p *Pair) findNeutral() string {
	for _, r := range [][2]rune{{'0', '9'}, {'a', 'z'}, {'A', 'Z'}} {
		for c := r[0]; c <= r[1]; c++ {
			s := string(c)
			if !strings.Contains(p.Open, s) && !strings.Contains(p.Close, s) {
				return s
			}
		}
	}
	return ""
}

// Language is the auto-closing and surrounding configuration of one language. It is read-only
// singleClose made, so it can be shared.
type Language struct {
	pairs                                                         []*Pair
	openByStart, openByEnd, closeByStart, closeByEnd, singleClose map[string][]*Pair
	surrounding                                                   map[string]string
	beforeQuotes, beforeBrackets                                  string
	blockCommentStart                                             string
}

// NewLanguage makes the Language of a language configuration, which may be nil. The pairs are
// the autoClosingPairs of the configuration, else its brackets, else none; the surrounding pairs
// are its surroundingPairs, else the auto-closing pairs.
//
// Ported from VS Code: src/vs/editor/common/languages/supports/characterPair.ts (CharacterPairSupport)
func NewLanguage(c *language.Configuration) *Language {
	l := &Language{
		openByStart: map[string][]*Pair{}, openByEnd: map[string][]*Pair{},
		closeByStart: map[string][]*Pair{}, closeByEnd: map[string][]*Pair{}, singleClose: map[string][]*Pair{},
		surrounding:  map[string]string{},
		beforeQuotes: defaultBeforeQuotes, beforeBrackets: defaultBeforeBrackets,
	}
	if c == nil {
		return l
	}
	switch {
	case c.AutoClosingPairs != nil:
		for _, p := range c.AutoClosingPairs {
			l.pairs = append(l.pairs, newPair(p))
		}
	case c.Brackets != nil:
		for _, b := range c.Brackets {
			l.pairs = append(l.pairs, newPair(language.AutoClosingPair{Pair: b}))
		}
	}
	for _, p := range l.pairs {
		appendEntry(l.openByStart, firstChar(p.Open), p)
		appendEntry(l.openByEnd, lastChar(p.Open), p)
		appendEntry(l.closeByStart, firstChar(p.Close), p)
		appendEntry(l.closeByEnd, lastChar(p.Close), p)
		if len(p.Close) == 1 && len(p.Open) == 1 {
			appendEntry(l.singleClose, p.Close, p)
		}
	}
	if c.AutoCloseBefore != nil {
		l.beforeQuotes, l.beforeBrackets = *c.AutoCloseBefore, *c.AutoCloseBefore
	}
	if c.SurroundingPairs != nil {
		for _, p := range c.SurroundingPairs {
			l.surrounding[p.Open] = p.Close
		}
	} else {
		for _, p := range l.pairs {
			l.surrounding[p.Open] = p.Close
		}
	}
	if c.Comments != nil && c.Comments.BlockComment != nil {
		l.blockCommentStart = c.Comments.BlockComment.Open
	}
	return l
}

func appendEntry(m map[string][]*Pair, key string, p *Pair) { m[key] = append(m[key], p) }

// firstChar and lastChar are the first and the last character (code point) of s.
func firstChar(s string) string {
	for i := range s {
		if i > 0 {
			return s[:i]
		}
	}
	return s
}

func lastChar(s string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i]&0xC0 != 0x80 {
			return s[i:]
		}
	}
	return s
}

// Empty reports whether the language closes nothing and surrounds nothing.
func (l *Language) Empty() bool { return l == nil || (len(l.pairs) == 0 && len(l.surrounding) == 0) }

// OpenByEnd are the pairs whose opening ends with the character ch.
func (l *Language) OpenByEnd(ch string) []*Pair {
	if l == nil {
		return nil
	}
	return l.openByEnd[ch]
}

// OpenByStart are the pairs whose opening starts with the character ch.
func (l *Language) OpenByStart(ch string) []*Pair {
	if l == nil {
		return nil
	}
	return l.openByStart[ch]
}

// CloseByStart are the pairs whose closing starts with the character ch.
func (l *Language) CloseByStart(ch string) []*Pair {
	if l == nil {
		return nil
	}
	return l.closeByStart[ch]
}

// CloseByEnd are the pairs whose closing ends with the character ch.
func (l *Language) CloseByEnd(ch string) []*Pair {
	if l == nil {
		return nil
	}
	return l.closeByEnd[ch]
}

// IsCloseSingleChar reports whether ch is the closing of a pair of two single characters.
func (l *Language) IsCloseSingleChar(ch string) bool { return l != nil && len(l.singleClose[ch]) > 0 }

// SurroundingClose is the closing of the surrounding pair that opens with ch.
func (l *Language) SurroundingClose(ch string) (string, bool) {
	if l == nil {
		return "", false
	}
	c, ok := l.surrounding[ch]
	return c, ok
}

// BlockCommentStart is the start of a block comment, "" for none.
func (l *Language) BlockCommentStart() string {
	if l == nil {
		return ""
	}
	return l.blockCommentStart
}

// ShouldCloseBefore returns the test of whether a pair is closed before the character ch, for a
// strategy (VS Code's CursorConfiguration.shouldAutoCloseBefore).
func (l *Language) ShouldCloseBefore(s Strategy, forQuotes bool) func(ch string) bool {
	switch s {
	case BeforeWhitespace:
		return func(ch string) bool { return strings.Contains(whitespace, ch) }
	case Always:
		return func(string) bool { return true }
	case Never:
		return func(string) bool { return false }
	}
	set := defaultBeforeBrackets
	switch {
	case l != nil && forQuotes:
		set = l.beforeQuotes
	case l != nil:
		set = l.beforeBrackets
	case forQuotes:
		set = defaultBeforeQuotes
	}
	return func(ch string) bool { return strings.Contains(set, ch) }
}

// Languages gives the Language for the code of a language. It must never return nil.
type Languages interface {
	Language(code int) *Language
}

// RegistryLanguages gives the Languages of a language registry. It makes each singleClose.
type RegistryLanguages struct {
	reg   *language.Registry
	mu    sync.Mutex
	cache map[int]*Language
}

// NewRegistryLanguages makes the Languages of a registry. When the configurations of the registry
// change, make a new one.
func NewRegistryLanguages(reg *language.Registry) *RegistryLanguages {
	return &RegistryLanguages{reg: reg, cache: map[int]*Language{}}
}

// Language implements Languages.
func (r *RegistryLanguages) Language(code int) *Language {
	r.mu.Lock()
	defer r.mu.Unlock()
	if l, ok := r.cache[code]; ok {
		return l
	}
	l := NewLanguage(r.reg.Configuration(r.reg.IDOfCode(code)))
	r.cache[code] = l
	return l
}
