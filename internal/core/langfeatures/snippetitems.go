package langfeatures

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/internal/core/fuzzy"
	"github.com/rm4n0s/emetos/internal/core/snippet"
)

// SnippetOwner is the owner of the items that are the user's snippets (SPEC §9.9, §9.12 "Completion").
const SnippetOwner = "emetos.snippets"

// snippetAnchor is a place in the line where the text of a snippet's prefix may start.
type snippetAnchor struct {
	start     int
	prefixLow string
}

// lowerSameLength lowers the case of s without changing its length in bytes, so that the columns of s are those of the result.
func lowerSameLength(s string) string {
	low := strings.ToLower(s)
	if len(low) == len(s) {
		return low
	}
	var sb strings.Builder
	for _, r := range s {
		l := unicode.ToLower(r)
		if utf8.RuneLen(l) != utf8.RuneLen(r) {
			l = r
		}
		sb.WriteRune(l)
	}
	return sb.String()
}

// SnippetItems are the snippets that the text before the cursor asks for, as completion items: a snippet is offered when its
// prefix contains the characters typed since one of the places a prefix may start (a word start, or any character that is not
// part of a word), in order. A request for a trigger character only keeps the snippets whose prefix starts with it. When the
// character before the cursor is white space, or the line is empty, the snippets that did not match are offered too, so that a
// snippet can be found from the empty prefix. The edit of a snippet item starts where the matching text starts.
//
// Ported from VS Code: src/vs/workbench/contrib/snippets/browser/snippetCompletionProvider.ts (provideCompletionItems,
// _computeSnippetPositions). The step that adds the name of a snippet to equal labels is left out: in VS Code it compares two
// objects with === and never does anything
func SnippetItems(snippets []*snippet.Snippet, line string, pos Position, wordAt WordFunc, trigger CompletionTrigger) []CompletionItem {
	col := min(max(pos.Col, 0), len(line))
	lineLow := lowerSameLength(line)
	wordStart, wordEnd := col, col
	hasWord := false
	if s, e, ok := wordAt(line, col); ok {
		wordStart, wordEnd, hasWord = s, e, true
	}
	withWord := lineLow[:min(wordEnd, len(lineLow))]
	// the anchors: word starts and every character that is not part of a word
	var anchors []snippetAnchor
	for off := 0; off < wordStart; {
		anchors = append(anchors, snippetAnchor{off, withWord[min(off, len(withWord)):]})
		if _, e, ok := wordAt(line, off); ok {
			// the character right after a word is an anchor, always, and the next one to look at is the one after it
			anchors = append(anchors, snippetAnchor{e, withWord[min(e, len(withWord)):]})
			off = e
		}
		if off >= len(line) {
			break
		}
		_, w := utf8.DecodeRuneInString(line[off:])
		off += w
	}
	if hasWord || len(anchors) == 0 {
		anchors = append(anchors, snippetAnchor{wordStart, withWord[min(wordStart, len(withWord)):]})
	}

	pending := make(map[*snippet.Snippet]bool, len(snippets))
	for _, s := range snippets {
		pending[s] = true
	}
	triggerLow := strings.ToLower(trigger.Character)
	var out []CompletionItem
	for _, s := range snippets {
		prefixLow := strings.ToLower(s.Prefix)
		if trigger.Kind == TriggerCharacter && !strings.HasPrefix(prefixLow, triggerLow) {
			continue // when there is a trigger character the prefix must start with it
		}
		var cand *snippetAnchor
		for i := range anchors {
			a := &anchors[i]
			if startsWithSpace(a.prefixLow) && !startsWithSpace(prefixLow) {
				continue // a blank anchor is for the snippets whose prefix starts with a blank
			}
			if fuzzy.IsPatternInWord(a.prefixLow, prefixLow) {
				cand = a
				break
			}
		}
		if cand == nil {
			continue
		}
		rest := len(prefixLow) - (col - cand.start)
		end := col
		if rest > 0 && col+rest <= len(lineLow) && col-cand.start >= 0 && col-cand.start <= len(prefixLow) &&
			lineLow[col:col+rest] == prefixLow[col-cand.start:] {
			end = col + rest // the rest of the prefix is already after the cursor
		}
		out = append(out, snippetItem(s, pos.Line, cand.start, col, end))
		delete(pending, s)
	}
	// the other snippets, when the prefix before the cursor ends in white space or the line is empty
	if trigger.Kind != TriggerCharacter && (line == "" || AfterSpace(lineLow, col)) {
		for _, s := range snippets {
			if !pending[s] {
				continue
			}
			prefixLow := strings.ToLower(s.Prefix)
			end := col
			if strings.HasPrefix(lineLow[col:], prefixLow) && prefixLow != "" {
				end = col + len(prefixLow)
			}
			out = append(out, snippetItem(s, pos.Line, col, col, end))
		}
	}
	slices.SortStableFunc(out, func(a, b CompletionItem) int { return strings.Compare(a.Label, b.Label) })
	return out
}

func startsWithSpace(s string) bool {
	r, _ := utf8.DecodeRuneInString(s)
	return s != "" && unicode.IsSpace(r)
}

// snippetItem is the completion item of a snippet whose text starts at the column start of the line and is replaced up to end.
func snippetItem(s *snippet.Snippet, line, start, col, end int) CompletionItem {
	sortPrefix := "z"
	if s.Source == "User Snippet" {
		sortPrefix = "a"
	}
	detail := s.Description
	if detail == "" {
		detail = s.Name
	}
	return CompletionItem{
		Label: s.Prefix, LabelDescription: s.Name, Detail: detail + " (" + s.Source + ")",
		Documentation: &Markup{Value: "```\n" + snippet.AsInsertText(s.CodeSnippet()) + "\n```", Markdown: true},
		Kind:          SnippetKind, SortText: sortPrefix + "-" + s.Prefix,
		InsertText: s.CodeSnippet(), Snippet: true, Owner: SnippetOwner,
		Insert:  &Range{Start: Position{Line: line, Col: start}, End: Position{Line: line, Col: col}},
		Replace: &Range{Start: Position{Line: line, Col: start}, End: Position{Line: line, Col: end}},
	}
}
