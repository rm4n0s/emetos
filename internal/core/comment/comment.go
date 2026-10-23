// Package comment holds the comment tokens of the languages (SPEC §8.6.5): what the commands that
// comment and uncomment lines need to know of a language. It has no text and no cursors; the
// editing functions of internal/core/edit use it.
package comment

import (
	"sync"

	"github.com/rm4n0s/emetos/internal/core/language"
)

// Language is how the comments of one language are written. It is read-only once made, so it
// can be shared.
type Language struct {
	// Line starts a comment that ends with the line; empty for none.
	Line string
	// NoIndent puts a line comment at the first column and not at the indentation of the line.
	NoIndent bool
	// BlockStart and BlockEnd begin and end a block comment; both are empty for none.
	BlockStart, BlockEnd string
}

// NewLanguage makes the Language of a language configuration, which may be nil.
//
// Ported from VS Code: src/vs/editor/common/languages/languageConfigurationRegistry.ts (_handleComments)
func NewLanguage(c *language.Configuration) *Language {
	l := &Language{}
	if c == nil || c.Comments == nil {
		return l
	}
	l.Line, l.NoIndent = c.Comments.LineComment, c.Comments.LineCommentNoIndent
	if b := c.Comments.BlockComment; b != nil && b.Open != "" && b.Close != "" {
		l.BlockStart, l.BlockEnd = b.Open, b.Close
	}
	return l
}

// HasBlock reports whether the language has block comments.
func (l *Language) HasBlock() bool { return l != nil && l.BlockStart != "" && l.BlockEnd != "" }

// Languages gives the Language for the code of a language of the document or of one embedded in
// it. It must never return nil.
type Languages interface {
	Language(code int) *Language
}

// RegistryLanguages gives the Languages of a language registry. It makes each once.
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
