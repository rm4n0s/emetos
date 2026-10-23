package kit

// Ported from VS Code: src/vs/base/browser/ui/tree/abstractTree.ts (FindFilter, FindController: the find
// mode, the match type, which rows a pattern matches, the rows the focus may land on). The fuzzy match is
// a plain case-insensitive subsequence in place of VS Code's `fuzzyScore`, and the contiguous match a
// case-insensitive substring (`contiguousFuzzyScore`).

import "unicode"

// FindMode says what a pattern typed into a list or tree does to the rows.
type FindMode int

const (
	// FindModeHighlight marks the rows that match and the matching text in their labels, and keeps all rows
	// shown; the focus moves between the matches only (`TreeFindMode.Highlight`, the default).
	FindModeHighlight FindMode = iota
	// FindModeFilter shows only the rows that match; a tree also shows the nodes above a match, expanded
	// (`TreeFindMode.Filter`).
	FindModeFilter
)

// FindMatchType says how a pattern is matched against a label.
type FindMatchType int

const (
	// FindMatchFuzzy matches when the letters of the pattern are in the label in order, not necessarily next to
	// each other, ignoring case (`TreeFindMatchType.Fuzzy`, the default).
	FindMatchFuzzy FindMatchType = iota
	// FindMatchContiguous matches when the pattern is part of the label, ignoring case
	// (`TreeFindMatchType.Contiguous`).
	FindMatchContiguous
)

// TextRange is a part of a text, in bytes: Start is the first byte and End the one after the last.
type TextRange struct{ Start, End int }

// MatchLabel matches a pattern against a label, ignoring case, and gives the parts of the label that match,
// in order and without touching each other. An empty pattern matches every label and gives no part. The
// fuzzy match takes the pattern as a part of the label when it is one, else the first letters of the label,
// from the left, that make the pattern in order.
func MatchLabel(pattern, label string, t FindMatchType) ([]TextRange, bool) {
	if pattern == "" {
		return nil, true
	}
	p := foldRunes(pattern)
	// the folded runes of the label, and the byte offset of each and of the end
	var l []rune
	var offs []int
	for i, r := range label {
		l = append(l, fold(r))
		offs = append(offs, i)
	}
	offs = append(offs, len(label))
	// a contiguous match first: it is the better one even for the fuzzy type
	for at := 0; at+len(p) <= len(l); at++ {
		if equalRunes(l[at:at+len(p)], p) {
			return []TextRange{{offs[at], offs[at+len(p)]}}, true
		}
	}
	if t == FindMatchContiguous {
		return nil, false
	}
	var out []TextRange
	j := 0
	for i := 0; i < len(l) && j < len(p); i++ {
		if l[i] != p[j] {
			continue
		}
		j++
		if n := len(out); n > 0 && out[n-1].End == offs[i] {
			out[n-1].End = offs[i+1]
		} else {
			out = append(out, TextRange{offs[i], offs[i+1]})
		}
	}
	if j < len(p) {
		return nil, false
	}
	return out, true
}

func fold(r rune) rune { return unicode.ToLower(r) }

func foldRunes(s string) []rune {
	out := []rune(s)
	for i, r := range out {
		out[i] = fold(r)
	}
	return out
}

func equalRunes(a, b []rune) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// FindState is what a list or tree knows about the find that is open in it.
type FindState struct {
	// Open says that the find box is shown.
	Open bool
	// Typing says that the keys go to the pattern: the box has the keyboard. Enter, a click on a row and
	// Escape end it; typing a letter starts it again.
	Typing bool
	// Pattern is what was typed; empty shows everything.
	Pattern string
	// Mode is what the pattern does to the rows.
	Mode FindMode
	// MatchType is how the pattern is matched.
	MatchType FindMatchType
	// MatchCount is how many rows match the pattern, and Total how many rows were matched against it.
	MatchCount, Total int
}

// NoResults says that there is a pattern and no row matches it.
func (f FindState) NoResults() bool {
	return f.Open && f.Pattern != "" && f.MatchCount == 0 && f.Total > 0
}

// findCore is the find of a model: the pattern, the mode, the match type and the rows that match.
type findCore struct {
	open, typing bool
	pattern      string
	mode         FindMode
	matchType    FindMatchType
	matches      map[string][]TextRange
	total        int
}

// active says that a pattern is in use.
func (f *findCore) active() bool { return f.open && f.pattern != "" }

// filtering says that only the rows that match are shown.
func (f *findCore) filtering() bool { return f.active() && f.mode == FindModeFilter }

// restrictsFocus says that the keyboard moves the focus between the matches only: a pattern is in use and
// more than one row matches (`FindController.shouldAllowFocus`).
func (f *findCore) restrictsFocus() bool {
	return f.active() && (f.total <= 0 || len(f.matches) > 1)
}

func (f *findCore) isMatch(key string) bool { _, ok := f.matches[key]; return ok }

// reset forgets the matches.
func (f *findCore) reset() {
	clear(f.matches)
	f.total = 0
}

// consider matches a label against the pattern and records the result.
func (f *findCore) consider(key, label string) bool {
	f.total++
	ranges, ok := MatchLabel(f.pattern, label, f.matchType)
	if ok {
		if f.matches == nil {
			f.matches = map[string][]TextRange{}
		}
		f.matches[key] = ranges
	}
	return ok
}

func (f *findCore) state() FindState {
	return FindState{Open: f.open, Typing: f.typing, Pattern: f.pattern, Mode: f.mode, MatchType: f.matchType, MatchCount: len(f.matches), Total: f.total}
}
