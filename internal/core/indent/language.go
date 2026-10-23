package indent

import (
	"sync"

	"github.com/rm4n0s/emetos/internal/core/brackets"
	"github.com/rm4n0s/emetos/internal/core/language"
)

// Language is what the indentation needs to know of a language: its indentation rules, its
// on-enter rules and its brackets. It is read-only once made, so it can be shared.
type Language struct {
	// Rules are the indentation rules, nil when the language has none.
	Rules *Rules
	// OnEnter decides what Enter does, nil when the language has no brackets, indentation rules
	// or on-enter rules.
	OnEnter *OnEnterSupport
	// Brackets are the brackets that are taken out of the strings, comments and regular
	// expressions of a line before the rules look at it.
	Brackets *brackets.Language

	// autoClosingEnds holds the last character of the opening of each auto-closing pair.
	autoClosingEnds map[string]bool
}

// NewLanguage makes the Language of a language configuration (which may be nil) and its brackets.
func NewLanguage(c *language.Configuration, br *brackets.Language) *Language {
	l := &Language{Brackets: br, autoClosingEnds: map[string]bool{}}
	if c == nil {
		return l
	}
	l.Rules = NewRules(c.IndentationRules)
	l.OnEnter = NewOnEnterSupport(c)
	for _, p := range c.AutoClosingPairs {
		if p.Open != "" {
			l.autoClosingEnds[lastChar(p.Open)] = true
		}
	}
	return l
}

// lastChar is the last character of s.
func lastChar(s string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i]&0xC0 != 0x80 {
			return s[i:]
		}
	}
	return s
}

// ClosesOnTyping reports whether typing ch completes the opening of an auto-closing pair of the
// language (VS Code's autoClosingPairsOpenByEnd).
func (l *Language) ClosesOnTyping(ch string) bool { return l != nil && l.autoClosingEnds[ch] }

// Languages gives the Language for the code of a language of the document or of one embedded in
// it. It must never return nil.
type Languages interface {
	Language(code int) *Language
}

// Indenter applies the indentation rules of languages to texts (SPEC §8.6.2).
type Indenter struct {
	langs Languages
}

// New makes an Indenter over languages.
func New(langs Languages) *Indenter { return &Indenter{langs: langs} }

// RegistryLanguages gives the Languages of a language registry. It makes each once.
type RegistryLanguages struct {
	reg   *language.Registry
	br    brackets.Languages
	mu    sync.Mutex
	cache map[int]*Language
}

// NewRegistryLanguages makes the Languages of a registry, with the brackets br gives. When the
// configurations of the registry change, make a new one.
func NewRegistryLanguages(reg *language.Registry, br brackets.Languages) *RegistryLanguages {
	return &RegistryLanguages{reg: reg, br: br, cache: map[int]*Language{}}
}

// Language implements Languages.
func (r *RegistryLanguages) Language(code int) *Language {
	r.mu.Lock()
	defer r.mu.Unlock()
	if l, ok := r.cache[code]; ok {
		return l
	}
	l := NewLanguage(r.reg.Configuration(r.reg.IDOfCode(code)), r.br.Language(code))
	r.cache[code] = l
	return l
}
