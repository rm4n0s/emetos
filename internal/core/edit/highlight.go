package edit

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// Ported from VS Code: src/vs/editor/contrib/multicursor/browser/multicursor.ts (SelectionHighlighter)
// and src/vs/editor/contrib/wordHighlighter/browser/textualHighlightProvider.ts

// OccurrenceQuery is a piece of text that a highlight looks for in the whole document.
type OccurrenceQuery struct {
	// Text is the text to find.
	Text string
	// MatchCase makes the search case sensitive.
	MatchCase bool
	// WholeWord only accepts matches that are whole words.
	WholeWord bool
}

// SelectionHighlightOptions are the settings of the selection highlight.
type SelectionHighlightOptions struct {
	// MaxLength is the longest selection, in characters, that is searched for; 0 is no limit
	// (editor.selectionHighlightMaxLength).
	MaxLength int
	// Multiline lets a selection that spans lines be searched for (editor.selectionHighlightMultiline).
	Multiline bool
}

// SelectionQuery is what the selection highlight looks for: the text of the selection, with the
// switches of the find widget. It says no (false) when the selections have no text, when a
// selection spans lines and Multiline is off, when several selections hold different texts
// (compared without case unless the find options match case), when the text is only spaces and
// tabs, and when it is longer than MaxLength. A cursor with no selection highlights nothing: the
// word at it is the business of WordOccurrences.
//
// Ported from VS Code: multicursor.ts (SelectionHighlighter._createState)
func (c Commands) SelectionQuery(sels []buffer.Selection, find SearchOptions, o SelectionHighlightOptions) (OccurrenceQuery, bool) {
	if len(sels) == 0 {
		return OccurrenceQuery{}, false
	}
	primary := c.validSelection(sels[0])
	if !o.Multiline && primary.Start().Line != primary.End().Line {
		return OccurrenceQuery{}, false
	}
	if primary.IsEmpty() {
		return OccurrenceQuery{}, false
	}
	if len(sels) > 1 {
		first := c.textIn(primary.Range())
		for _, s := range sels[1:] {
			s = c.validSelection(s)
			if s.IsEmpty() {
				return OccurrenceQuery{}, false
			}
			text := c.textIn(s.Range())
			if text != first && (find.MatchCase || !strings.EqualFold(text, first)) {
				return OccurrenceQuery{}, false
			}
		}
	}
	text := c.textIn(primary.Range())
	if strings.Trim(text, " \t") == "" {
		return OccurrenceQuery{}, false
	}
	if o.MaxLength > 0 && utf8.RuneCountInString(text) > o.MaxLength {
		return OccurrenceQuery{}, false
	}
	return OccurrenceQuery{Text: text, MatchCase: find.MatchCase, WholeWord: find.WholeWord}, true
}

// WordQuery is what the word highlight looks for: the word the cursor is in or touches, as a
// case sensitive search for whole words (the textual document highlight of VS Code). It says no
// when the selection spans lines, when there is no word at its start, or when the selection
// reaches out of that word.
//
// Ported from VS Code: wordHighlighter.ts (WordHighlighter._run), textualHighlightProvider.ts
func (c Commands) WordQuery(sel buffer.Selection) (q OccurrenceQuery, word buffer.Range, ok bool) {
	sel = c.validSelection(sel)
	if sel.Start().Line != sel.End().Line {
		return OccurrenceQuery{}, buffer.Range{}, false
	}
	line := sel.Start().Line
	text := c.Model.LineContent(line)
	w, found := c.Options.words().WordAt(text, sel.Start().Col)
	if !found || w.Start > sel.Start().Col || w.End < sel.End().Col {
		return OccurrenceQuery{}, buffer.Range{}, false
	}
	word = buffer.Range{Start: posAt(line, w.Start), End: posAt(line, w.End)}
	return OccurrenceQuery{Text: text[w.Start:w.End], MatchCase: true, WholeWord: true}, word, true
}

// Occurrences are the matches of a query in the document, in order and without overlapping.
func (c Commands) Occurrences(q OccurrenceQuery) []buffer.Range {
	if q.Text == "" {
		return nil
	}
	var words *WordClassifier
	if q.WholeWord {
		words = c.Options.words()
	}
	return newLiteralSearch(c.Model, q.Text, q.MatchCase, words).All(nil)
}

// OccurrencesApartFrom are the matches of a query that are not the selections and do not overlap a
// selection that has text: the highlight of the text that equals the selection leaves the
// selection itself alone.
//
// Ported from VS Code: multicursor.ts (SelectionHighlighter._setState)
func (c Commands) OccurrencesApartFrom(q OccurrenceQuery, sels []buffer.Selection) []buffer.Range {
	all := c.Occurrences(q)
	if len(all) == 0 {
		return nil
	}
	ranges := make([]buffer.Range, 0, len(sels))
	for _, s := range sels {
		ranges = append(ranges, c.validSelection(s).Range())
	}
	slices.SortFunc(ranges, func(a, b buffer.Range) int { return a.Start.Compare(b.Start) })
	out := all[:0:0]
	for _, m := range all {
		keep := true
		for _, r := range ranges {
			if !r.IsEmpty() && r.Start.IsBefore(m.End) && m.Start.IsBefore(r.End) {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, m)
		}
	}
	return out
}
