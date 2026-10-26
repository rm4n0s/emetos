package langfeatures

import (
	"context"
	"strconv"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// WordOwner is the owner of the items that are words of the open documents.
const WordOwner = "emetos.words"

// The values of editor.wordBasedSuggestions.
const (
	WordsOff               = "off"
	WordsOffWithInline     = "offWithInlineSuggestions"
	WordsCurrentDocument   = "currentDocument"
	WordsMatchingDocuments = "matchingDocuments"
	WordsAllDocuments      = "allDocuments"
)

// maxWordSource is the size in bytes above which the text of a document gives no words (VS Code's canSyncModel limit is a
// 50 MB model; a suggestion must be quick, so the limit here is smaller).
const maxWordSource = 5 << 20

// WordProvider answers completion requests with the words of the open documents (editor.wordBasedSuggestions): a document
// that no other provider serves still gets suggestions. Register it for every document, which gives it the lowest score, so
// that it is only asked when the providers of the language had nothing to say (VS Code's rule).
//
// Ported from VS Code: src/vs/editor/browser/services/editorWorkerService.ts (WordBasedCompletionItemProvider) and
// src/vs/editor/common/services/editorSimpleWorker.ts (textualSuggest)
type WordProvider struct {
	// Mode gives the value of editor.wordBasedSuggestions for the document.
	Mode func(doc Document) string
	// Documents lists snapshots of the open documents, the one of the request included or not. It is called on the goroutine
	// of the request and gives up when ctx ends.
	Documents func(ctx context.Context) []Document
	// WordAt finds the words of a language, as editor.wordSeparators says.
	WordAt WordFunc
}

// ProvideCompletion implements CompletionProvider.
func (p *WordProvider) ProvideCompletion(ctx context.Context, doc Document, pos Position, _ CompletionTrigger) (CompletionList, error) {
	mode := WordsOffWithInline
	if p.Mode != nil {
		mode = p.Mode(doc)
	}
	if mode == WordsOff {
		return CompletionList{}, nil
	}
	sources := []Document{doc}
	if mode != WordsCurrentDocument && p.Documents != nil {
		for _, d := range p.Documents(ctx) {
			if d.URI == doc.URI {
				continue
			}
			if mode == WordsAllDocuments || d.LanguageID == doc.LanguageID {
				sources = append(sources, d)
			}
		}
	}
	line, _ := doc.Text.LineContent(pos.Line)
	current := ""
	if s, e, ok := p.WordAt(line, pos.Col); ok {
		current = line[s:e]
	}
	seen := map[string]bool{current: true}
	var out CompletionList
	for _, src := range sources {
		if src.Text.Len() > maxWordSource {
			continue
		}
		if err := ctx.Err(); err != nil {
			return CompletionList{}, err
		}
		collectWords(src.Text, p.WordAt, seen, func(w string) {
			out.Items = append(out.Items, CompletionItem{Label: w, Kind: CompletionKind(1), InsertText: w})
		})
	}
	return out, nil
}

// collectWords calls add with each word of the text that is not in seen yet (a number is not a word), and adds it to seen.
func collectWords(text buffer.Snapshot, wordAt WordFunc, seen map[string]bool, add func(string)) {
	for ln := 0; ln < text.LineCount(); ln++ {
		line, err := text.LineContent(ln)
		if err != nil {
			return
		}
		for col := 0; col < len(line); {
			s, e, ok := wordAt(line, col)
			if !ok || e <= s || e <= col {
				_, w := utf8.DecodeRuneInString(line[col:])
				col += w
				continue
			}
			if w := line[s:e]; !seen[w] && !isNumber(w) {
				seen[w] = true
				add(w)
			}
			col = e
		}
	}
}

func isNumber(w string) bool {
	_, err := strconv.ParseFloat(w, 64)
	return err == nil
}
