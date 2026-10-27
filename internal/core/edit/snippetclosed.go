package edit

import (
	"strings"

	"github.com/rm4n0s/emetos/internal/core/autoclose"
	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// snippetClosers are the characters that can end a pair of a snippet's text (the class of VS Code's regular expression).
const snippetClosers = ")]}>'\"`"

// SnippetPairs finds, in the text of each edit of an inserted snippet, the pair that its end closes: the last closing character
// of the text (nothing but other characters after it) and the nearest opening character of its pair before it. It gives, for each
// text, the byte index of the opening and of the closing character. It reports false when any text has no such pair, has more than
// one line, or is empty, or when the closing character is that of none or of several pairs of single characters: the snippet
// then has no pair for the editor to type over (VS Code's `_findAutoClosingPairs`, for the edits of the source "snippet").
//
// Ported from VS Code: src/vs/editor/common/cursor/cursor.ts (_findAutoClosingPairs). One difference: for a closing character at
// the very start of the text VS Code's lastIndexOf with a negative index finds the opening there when it is the same character
// (a quote); here there is no pair.
func SnippetPairs(l *autoclose.Language, texts []string) ([][2]int, bool) {
	if len(texts) == 0 {
		return nil, false
	}
	pairs := make([][2]int, 0, len(texts))
	for _, text := range texts {
		if text == "" || strings.Contains(text, "\n") {
			return nil, false
		}
		closeIdx := strings.LastIndexAny(text, snippetClosers)
		if closeIdx < 0 {
			return nil, false
		}
		closing := text[closeIdx : closeIdx+1]
		opening, ok := l.SingleCloseOpen(closing)
		if !ok {
			return nil, false
		}
		openIdx := strings.LastIndex(text[:closeIdx], opening)
		if openIdx < 0 {
			return nil, false
		}
		pairs = append(pairs, [2]int{openIdx, closeIdx})
	}
	return pairs, true
}

// SnippetAutoClosed makes the AutoClosed values of the pairs that SnippetPairs found: starts are the offsets in the document
// where the texts are now, and snap is the document after the insertion. The tracker then lets the user type over the closing
// characters of the snippet as over those the editor typed.
func SnippetAutoClosed(snap buffer.Snapshot, l *autoclose.Language, texts []string, starts []int) []AutoClosed {
	pairs, ok := SnippetPairs(l, texts)
	if !ok || len(starts) != len(texts) {
		return nil
	}
	out := make([]AutoClosed, len(pairs))
	for i, p := range pairs {
		openAt, closeAt := starts[i]+p[0], starts[i]+p[1]
		out[i] = AutoClosed{
			Character: buffer.Range{Start: snap.PositionAt(closeAt), End: snap.PositionAt(closeAt + 1)},
			Enclosing: buffer.Range{Start: snap.PositionAt(openAt), End: snap.PositionAt(closeAt + 1)},
		}
	}
	return out
}
