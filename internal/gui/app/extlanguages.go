package app

import (
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/tokens"
)

// maxTokenizeBytes is the longest text Languages().Tokenize takes: the text of a code block, not of a file.
const maxTokenizeBytes = 256 << 10

// tokenizeLineBytes is the longest line the grammar tokenizes (editor.maxTokenizationLineLength's default).
const tokenizeLineBytes = 20000

// extLanguages is the exthost.LanguageSource of a workspace: the grammars of the process.
type extLanguages struct{ w *Workspace }

// Resolve implements exthost.LanguageSource.
func (l extLanguages) Resolve(name string) (string, bool) {
	return l.w.Process.Languages.ByName(strings.TrimSpace(name))
}

// Tokenize implements exthost.LanguageSource: the text is tokenized on the caller with a tokenizer of its own over a
// document that is dropped afterwards, and the scopes of each token are given by name.
func (l extLanguages) Tokenize(languageID, text string) ([][]extension.Token, error) {
	p := l.w.Process
	g, err := p.Grammar(languageID, tokenizeLineBytes)
	if err != nil {
		return nil, err
	}
	if g == nil {
		return nil, errors.New("ExtensionLanguageUnknown", "the language has no grammar", "language", languageID)
	}
	if len(text) > maxTokenizeBytes {
		return nil, errors.New("ExtensionTokenizeTooLong", "the text is too long to highlight", "language", languageID, "bytes", len(text))
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	doc := document.New(document.URI{Scheme: document.SchemeUntitled, Path: "highlight"}, text, document.Options{LanguageID: languageID})
	tk := tokens.New(doc, g, tokens.Options{})
	defer tk.Close()
	tk.Settle()
	lines := strings.Count(text, "\n") + 1
	out := make([][]extension.Token, lines)
	for i := range lines {
		start := 0
		for _, tok := range tk.Line(i) {
			end := int(tok.End)
			names := tk.Scopes().Names(tok.Scope)
			out[i] = append(out[i], extension.Token{Start: start, End: end, Scopes: append([]string(nil), names...)})
			start = end
		}
	}
	return out, nil
}
