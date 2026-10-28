package references

import (
	"slices"
	"strconv"
	"strings"

	"github.com/rm4n0s/emetos/extension"
)

// The searches that were made, newest first in the list the user sees (VS Code's TreeInputHistory).
//
// Ported from VS Code: extensions/references-view/src/tree.ts (HistoryItem, TreeInputHistory), utils.ts (WordAnchor)

// The two kinds of search, and the titles VS Code gives them.
const (
	kindReferences       = "references"
	kindImplementations  = "implementations"
	titleReferences      = "References"
	titleImplementations = "Implementations"
)

// input is a search: what is asked of the language servers and where.
type input struct {
	// title is "References" or "Implementations": the title of the view while the search is shown.
	title string
	// kind is kindReferences or kindImplementations.
	kind string
	// uri and pos are the document and the position of the symbol.
	uri string
	pos extension.Position
}

// with is the same search at another position.
func (in *input) with(pos extension.Position) *input {
	c := *in
	c.pos = pos
	return &c
}

// anchor remembers the word a search was made on, so that a search made again after the text changed looks for the word near
// where it was (VS Code's WordAnchor).
type anchor struct {
	uri  string
	pos  extension.Position
	word string
}

// newAnchor makes the anchor of a position of a text.
func newAnchor(uri string, lines []string, pos extension.Position) anchor {
	return anchor{uri: uri, pos: pos, word: wordOfLine(lines, pos)}
}

// wordOfLine is the word at a position, else the run of characters that are not white space; empty when there is none.
func wordOfLine(lines []string, pos extension.Position) string {
	line := lineOf(lines, pos.Line)
	if s, e, ok := wordOrToken(line, pos.Col); ok {
		return line[s:e]
	}
	return ""
}

// guessedPosition is where the word is now, in the text as it is now: the same place when it is still there, else the first
// line, going out from the old one up to 100 lines either way, that has it.
func (a anchor) guessedPosition(lines []string) extension.Position {
	if a.word == "" || wordOfLine(lines, a.pos) == a.word {
		return a.pos
	}
	i := 0
	for {
		checked := false
		if n := a.pos.Line + i; n < len(lines) {
			checked = true
			if col := strings.Index(lines[n], a.word); col >= 0 {
				return extension.Position{Line: n, Col: col}
			}
		}
		i++
		if n := a.pos.Line - i; n >= 0 && n < len(lines) {
			checked = true
			if col := strings.Index(lines[n], a.word); col >= 0 {
				return extension.Position{Line: n, Col: col}
			}
		}
		if i >= 100 || !checked {
			break
		}
	}
	return a.pos
}

// historyItem is a search that found something.
type historyItem struct {
	// key tells two searches of the same word in the same place and kind apart from the others: a new one replaces the old.
	key string
	// word is the word that was searched, "???" when the position had none.
	word string
	// description is the file, relative to the folder, and the kind: "main.go • references".
	description string
	anchor      anchor
	in          *input
}

// history is the searches, the newest last.
type history struct {
	items []*historyItem
}

// add puts a search at the end; one with the same key goes first.
func (h *history) add(in *input, lines []string, relPath string) *historyItem {
	start := in.pos.Col
	word := "???"
	line := lineOf(lines, in.pos.Line)
	if s, e, ok := wordOrToken(line, in.pos.Col); ok {
		start, word = s, line[s:e]
	}
	it := &historyItem{
		key:         in.uri + "|" + in.title + "|" + strconv.Itoa(in.pos.Line) + ":" + strconv.Itoa(start),
		word:        word,
		description: relPath + " • " + strings.ToLower(in.title),
		anchor:      newAnchor(in.uri, lines, in.pos),
		in:          in,
	}
	h.remove(it.key)
	h.items = append(h.items, it)
	return it
}

// remove drops the search with the key.
func (h *history) remove(key string) {
	h.items = slices.DeleteFunc(h.items, func(it *historyItem) bool { return it.key == key })
}

// clear forgets every search.
func (h *history) clear() { h.items = nil }

// size is how many searches there are.
func (h *history) size() int { return len(h.items) }

// newestFirst lists the searches as the view shows them.
func (h *history) newestFirst() []*historyItem {
	out := slices.Clone(h.items)
	slices.Reverse(out)
	return out
}

// last is the newest search, nil when there is none.
func (h *history) last() *historyItem {
	if len(h.items) == 0 {
		return nil
	}
	return h.items[len(h.items)-1]
}
