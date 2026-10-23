package brackets

import (
	"sync"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/language"
	"github.com/rm4n0s/emetos/internal/core/tokens"
)

// RegistryLanguages gives the brackets of the languages of a language registry. It makes the
// brackets of a language once, so a call with the same code gives the same *Language.
type RegistryLanguages struct {
	reg   *language.Registry
	mu    sync.Mutex
	cache map[int]*Language
}

// NewRegistryLanguages makes a Languages over a registry. When the language configurations
// change, make a new one and Reset the indexes.
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
	l := FromConfiguration(code, r.reg.Configuration(r.reg.IDOfCode(code)))
	r.cache[code] = l
	return l
}

// docSource reads the text of a document and the tokens of its tokenizer.
type docSource struct {
	doc  *document.Document
	tok  *tokens.Tokenizer
	code int
}

func (s *docSource) LineCount() int { return s.doc.Snapshot().LineCount() }

func (s *docSource) LineContent(line int) string {
	text, err := s.doc.Snapshot().LineContent(line)
	if err != nil {
		return ""
	}
	return text
}

func (s *docSource) LineTokens(line int) []tokens.Token {
	if s.tok == nil {
		return nil
	}
	return s.tok.Line(line)
}

func (s *docSource) Language() int { return s.code }

func (s *docSource) TokensComplete() bool {
	if s.tok == nil || !s.tok.Active() {
		return true
	}
	last := s.LineCount() - 1
	return s.tok.Line(last) != nil
}

// Attach makes the index of a document with the tokens of tok (which may be nil for a document
// with no grammar: every line is then code in the language of the document). The index follows
// the document and the tokenizer until the function it returns is called. A document in large
// file mode has an index that finds nothing.
func Attach(doc *document.Document, tok *tokens.Tokenizer, langs Languages, languageCode int) (*Index, func()) {
	ix := New(&docSource{doc: doc, tok: tok, code: languageCode}, langs, !doc.LargeFile())
	stopDoc := doc.Subscribe(func(ev document.ChangeEvent) {
		if ev.Cause == document.CauseSetText {
			ix.Reset()
			return
		}
		changes := make([]Edited, len(ev.Changes))
		for i, c := range ev.Changes {
			changes[i] = Edited{From: c.OldRange.Start.Line, To: c.OldRange.End.Line, Count: c.NewRange.End.Line - c.NewRange.Start.Line + 1}
		}
		ix.Apply(changes)
	})
	stop := stopDoc
	if tok != nil {
		stopTok := tok.OnChange(ix.Invalidate)
		stop = func() {
			stopDoc()
			stopTok()
		}
	}
	return ix, stop
}
