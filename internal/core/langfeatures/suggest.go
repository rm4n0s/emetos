package langfeatures

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/internal/core/fuzzy"
)

// The model of the suggest widget (SPEC §9.12 "Completion"): the items of a request with their edit ranges, their filtering by
// the text typed since and their sorting. No UI.

// WordFunc finds the word of regular characters that touches the byte column col of a line: its start and end columns.
type WordFunc func(line string, col int) (start, end int, ok bool)

// SnippetOrder says where snippets stand among the items (editor.snippetSuggestions).
type SnippetOrder int

// The snippet orders.
const (
	// SnippetsInline sorts snippets with the other items.
	SnippetsInline SnippetOrder = iota
	// SnippetsTop puts the snippets before the other items.
	SnippetsTop
	// SnippetsBottom puts them after.
	SnippetsBottom
)

// SnippetKind is the completion kind of a snippet (CompletionItemKind.Snippet).
const SnippetKind CompletionKind = 15

// vsCodeKindOrder is the order of the kinds in VS Code's own CompletionItemKind, which the sort uses to tell items that are
// equal in everything else (the LSP numbers them differently).
var vsCodeKindOrder = [...]CompletionKind{
	2, 3, 4, 5, 6, 7, 22, 8, 9, 10, 23, 24, 11, 12, 21, 13, 20, 14, 1, 16, 17, 18, 19, 25, 15,
}

func kindRank(k CompletionKind) int {
	if i := slices.Index(vsCodeKindOrder[:], k); i >= 0 {
		return i
	}
	return len(vsCodeKindOrder) + int(k)
}

// EffectiveInsertText is the text an item inserts: its InsertText, else its label.
func (it CompletionItem) EffectiveInsertText() string {
	if it.InsertText != "" {
		return it.InsertText
	}
	return it.Label
}

// SuggestItem is an item of the suggest widget: a completion item with the place it was asked at, the columns it edits and
// the result of the last filtering.
//
// Ported from VS Code: src/vs/editor/contrib/suggest/browser/suggest.ts (CompletionItem)
type SuggestItem struct {
	CompletionItem
	// Position is where the request was made.
	Position Position
	// EditStart is the column where the edit of the item starts; InsertEnd and ReplaceEnd are the columns it ends at when the
	// item inserts and when it replaces the word. They are bytes in the line of Position.
	EditStart, InsertEnd, ReplaceEnd int
	// Invalid is set for an item the widget ignores: an empty label, or an edit over several lines or on another line.
	Invalid bool

	// Score is the result of the last filtering: the score, and the positions of the matching runes of the label (or of the
	// filter text when the label did not match). A score of -100 and no positions means there was nothing to match.
	Score fuzzy.WordScore
	// Word is the text the item was scored against; Idx its place in the order of the request; Distance the locality bonus.
	Word     string
	Idx      int
	Distance int

	// Resolved is set once the details of the item were asked for.
	Resolved bool

	labelLow, sortTextLow, filterTextLow string
}

// DefaultScore is the score of an item with nothing to match: out of the range the scorer gives.
const DefaultScore = -100

// SuggestRequest is what the items of one request are made against.
type SuggestRequest struct {
	Doc Document
	Pos Position
	// WordAt finds words, as editor.wordSeparators says.
	WordAt WordFunc
}

// ItemFilter says which items a request keeps (the kind filter and the deprecated ones).
type ItemFilter struct {
	// HiddenKinds are the kinds that are left out (editor.suggest.show<Kind> off, editor.snippetSuggestions none).
	HiddenKinds map[CompletionKind]bool
	// HideDeprecated leaves out the items tagged deprecated (editor.suggest.showDeprecated off).
	HideDeprecated bool
}

// NewSuggestItems makes the items of the answer of a request at pos: an item without a range gets the word at the position
// (it replaces the word, and inserts up to the cursor), an item without a sort text sorts by its label, and an item that is
// invalid is marked so. Items of a hidden kind are left out.
//
// Ported from VS Code: src/vs/editor/contrib/suggest/browser/suggest.ts (provideSuggestionItems, CompletionItem)
func NewSuggestItems(req SuggestRequest, items []CompletionItem, f ItemFilter) []*SuggestItem {
	line, _ := req.Doc.Text.LineContent(req.Pos.Line)
	wordStart, wordEnd := req.Pos.Col, req.Pos.Col
	if req.WordAt != nil {
		if s, e, ok := req.WordAt(line, req.Pos.Col); ok {
			wordStart, wordEnd = s, e
		}
	}
	out := make([]*SuggestItem, 0, len(items))
	for _, c := range items {
		if f.HiddenKinds[c.Kind] || (f.HideDeprecated && c.Deprecated) {
			continue
		}
		si := &SuggestItem{CompletionItem: c, Position: req.Pos}
		if c.SortText == "" {
			si.SortText = c.Label
		}
		si.labelLow = strings.ToLower(c.Label)
		si.sortTextLow = strings.ToLower(si.SortText)
		si.filterTextLow = strings.ToLower(c.FilterText)
		si.Invalid = c.Label == ""
		si.Score = fuzzy.WordScore{Score: DefaultScore}
		insert, replace := c.Insert, c.Replace
		switch {
		case insert == nil && replace == nil:
			si.EditStart, si.InsertEnd, si.ReplaceEnd = wordStart, req.Pos.Col, wordEnd
		case replace == nil: // a plain edit
			si.EditStart, si.InsertEnd, si.ReplaceEnd = insert.Start.Col, insert.End.Col, insert.End.Col
			si.Invalid = si.Invalid || insert.Start.Line != insert.End.Line || insert.Start.Line != req.Pos.Line
		default:
			if insert == nil {
				insert = replace
			}
			si.EditStart, si.InsertEnd, si.ReplaceEnd = insert.Start.Col, insert.End.Col, replace.End.Col
			si.Invalid = si.Invalid || insert.Start.Line != insert.End.Line || replace.Start.Line != replace.End.Line ||
				insert.Start.Line != req.Pos.Line || replace.Start.Line != req.Pos.Line || insert.Start.Col != replace.Start.Col
		}
		out = append(out, si)
	}
	return out
}

// CompareSuggestItems is the order the items of a request come in: by sort text, then label, then kind.
//
// Ported from VS Code: src/vs/editor/contrib/suggest/browser/suggest.ts (defaultComparator)
func CompareSuggestItems(a, b *SuggestItem) int {
	if a.sortTextLow != "" && b.sortTextLow != "" {
		if c := strings.Compare(a.sortTextLow, b.sortTextLow); c != 0 {
			return c
		}
	}
	if c := strings.Compare(a.Label, b.Label); c != 0 {
		return c
	}
	return kindRank(a.Kind) - kindRank(b.Kind)
}

// SortSuggestItems sorts the items of a request (a stable sort, as JavaScript's), with the snippets where the order says.
//
// Ported from VS Code: src/vs/editor/contrib/suggest/browser/suggest.ts (getSuggestionComparator)
func SortSuggestItems(items []*SuggestItem, order SnippetOrder) {
	slices.SortStableFunc(items, func(a, b *SuggestItem) int { return compareWithSnippets(a, b, order, CompareSuggestItems) })
}

func compareWithSnippets(a, b *SuggestItem, order SnippetOrder, rest func(a, b *SuggestItem) int) int {
	if order != SnippetsInline && a.Kind != b.Kind {
		up := 1
		if order == SnippetsTop {
			up = -1
		}
		if a.Kind == SnippetKind {
			return up
		}
		if b.Kind == SnippetKind {
			return -up
		}
	}
	return rest(a, b)
}

// LineContext is the text before the cursor and how far the cursor has moved since the request.
type LineContext struct {
	// LeadingLine is the text of the line before the cursor.
	LeadingLine string
	// Delta is the number of bytes the cursor moved to the right since the request.
	Delta int
}

// SuggestModelOptions are the options of a SuggestModel.
type SuggestModelOptions struct {
	// FilterGraceful accounts for small typos (editor.suggest.filterGraceful).
	FilterGraceful bool
	// MatchOnWordStartOnly asks the first match to be at a word start (editor.suggest.matchOnWordStartOnly).
	MatchOnWordStartOnly bool
	// Snippets is where the snippets stand (editor.snippetSuggestions).
	Snippets SnippetOrder
}

type refilterKind int

const (
	refilterNothing refilterKind = iota
	refilterAll
	refilterIncr
)

// SuggestModel is the sorted, filtered view of the items of a request: what is typed after the request narrows it.
//
// Ported from VS Code: src/vs/editor/contrib/suggest/browser/completionModel.ts
type SuggestModel struct {
	items   []*SuggestItem
	column  int
	ctx     LineContext
	opts    SuggestModelOptions
	refilt  refilterKind
	scorer  fuzzy.Scorer
	cache   []*SuggestItem
	byOwner map[string][]*SuggestItem
}

// NewSuggestModel makes the model of items asked for at the byte column column of a line, with the line context now.
func NewSuggestModel(items []*SuggestItem, column int, ctx LineContext, opts SuggestModelOptions) *SuggestModel {
	return &SuggestModel{items: items, column: column, ctx: ctx, opts: opts, refilt: refilterAll}
}

// LineContext is the context the items were filtered for.
func (m *SuggestModel) LineContext() LineContext { return m.ctx }

// SetLineContext filters the items for a new context: only when it differs from the last. When the cursor only moved to the
// right the items that did not match before are not looked at again.
func (m *SuggestModel) SetLineContext(ctx LineContext) {
	if m.ctx.LeadingLine == ctx.LeadingLine && m.ctx.Delta == ctx.Delta {
		return
	}
	if m.ctx.Delta < ctx.Delta && m.cache != nil {
		m.refilt = refilterIncr
	} else {
		m.refilt = refilterAll
	}
	m.ctx = ctx
}

// Items are the items that match, best first.
func (m *SuggestModel) Items() []*SuggestItem {
	m.ensure()
	return m.cache
}

// ItemsByOwner are the valid items that were looked at, by the owner of the provider that made them.
func (m *SuggestModel) ItemsByOwner() map[string][]*SuggestItem {
	m.ensure()
	return m.byOwner
}

// IncompleteOwners are the providers whose list of items was incomplete: asked again when the user types more.
func (m *SuggestModel) IncompleteOwners() map[string]bool {
	m.ensure()
	out := map[string]bool{}
	for owner, items := range m.byOwner {
		if len(items) > 0 && items[0].IncompleteList {
			out[owner] = true
		}
	}
	return out
}

func (m *SuggestModel) ensure() {
	if m.refilt != refilterNothing {
		m.build()
	}
}

// build scores the items against the text typed since the request and sorts those that match.
func (m *SuggestModel) build() {
	m.byOwner = map[string][]*SuggestItem{}
	source := m.items
	if m.refilt == refilterIncr {
		source = m.cache
	}
	target := make([]*SuggestItem, 0, len(source))
	leading, delta := m.ctx.LeadingLine, m.ctx.Delta
	var word, wordLow string
	fuzzyOpts := fuzzy.FuzzyScoreOptions{BoostFullMatch: true, FirstMatchCanBeWeak: !m.opts.MatchOnWordStartOnly}
	score := m.scorer.FuzzyScore
	if m.opts.FilterGraceful && len(source) <= 2000 {
		score = m.scorer.FuzzyScoreGracefulAggressive
	}
	for i, item := range source {
		if item.Invalid {
			continue
		}
		m.byOwner[item.Owner] = append(m.byOwner[item.Owner], item)
		// 'word' is the text before the cursor that the item is scored against: the stretch it edits, and what was typed
		// since. Each item may have another, but in practice they have the same, which is why it is kept
		overwriteBefore := item.Position.Col - item.EditStart
		wordLen := min(max(overwriteBefore+delta-(item.Position.Col-m.column), 0), len(leading))
		if len(word) != wordLen {
			word = leading[len(leading)-wordLen:]
			wordLow = strings.ToLower(word)
		}
		item.Word = word
		if wordLen == 0 {
			// nothing to score against: a constant rank, out of the range of the scorer, and the order of the request decides
			item.Score = fuzzy.WordScore{Score: DefaultScore}
		} else {
			// skip the blanks until the replaced range starts
			wordPos := 0
			for wordPos < overwriteBefore && wordPos < len(word) && (word[wordPos] == ' ' || word[wordPos] == '\t') {
				wordPos++
			}
			switch {
			case wordPos >= wordLen:
				item.Score = fuzzy.WordScore{Score: DefaultScore}
			case item.FilterText != "":
				// the filter text must match the word; the label gives the highlights when it matches too
				match, ok := score(word, wordLow, wordPos, item.FilterText, item.filterTextLow, 0, fuzzyOpts)
				if !ok {
					continue
				}
				if strings.EqualFold(item.FilterText, item.Label) {
					item.Score = match
				} else {
					item.Score = m.scorer.AnyScore(word, wordLow, wordPos, item.Label, item.labelLow, 0)
					item.Score.Score = match.Score // the rank of the filter text
				}
			default:
				match, ok := score(word, wordLow, wordPos, item.Label, item.labelLow, 0, fuzzyOpts)
				if !ok {
					continue
				}
				item.Score = match
			}
		}
		item.Idx = i
		target = append(target, item)
	}
	slices.SortStableFunc(target, func(a, b *SuggestItem) int {
		return compareWithSnippets(a, b, m.opts.Snippets, compareScores)
	})
	m.cache = target
	m.refilt = refilterNothing
}

// compareScores orders by score (higher first), distance, then the place in the request.
func compareScores(a, b *SuggestItem) int {
	switch {
	case a.Score.Score > b.Score.Score:
		return -1
	case a.Score.Score < b.Score.Score:
		return 1
	case a.Distance < b.Distance:
		return -1
	case a.Distance > b.Distance:
		return 1
	case a.Idx < b.Idx:
		return -1
	case a.Idx > b.Idx:
		return 1
	}
	return 0
}

// LeadingWhitespace is the spaces and tabs a line starts with.
func LeadingWhitespace(s string) string {
	n := 0
	for n < len(s) && (s[n] == ' ' || s[n] == '\t') {
		n++
	}
	return s[:n]
}

// LastRuneStart is the byte offset of the last rune of s (0 for an empty string).
func LastRuneStart(s string) int {
	if s == "" {
		return 0
	}
	_, w := utf8.DecodeLastRuneInString(s)
	return len(s) - w
}
