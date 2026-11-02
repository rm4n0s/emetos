package debug

import (
	"strings"

	"github.com/rm4n0s/emetos/internal/core/fuzzy"
	"github.com/rm4n0s/emetos/internal/core/glob"
)

type replQuery struct {
	exclude bool
	text    string
}

// ReplFilter is the filter box of the Debug Console: terms separated by commas, each text to look for (fuzzy), `!text` to leave out
// the lines that match, and `\text` for a text that starts with `!`. A line is shown when no `!` term matches it and, when there
// are terms to look for, at least one matches. Only outputs and groups are filtered: what the user typed, its results and
// objects always show.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/replFilter.ts
type ReplFilter struct {
	queries []replQuery
}

// NewReplFilter parses a filter text.
func NewReplFilter(text string) *ReplFilter {
	f := &ReplFilter{}
	f.Set(text)
	return f
}

// Set replaces the filter text.
func (f *ReplFilter) Set(query string) {
	f.queries = nil
	query = strings.TrimSpace(query)
	if query == "" {
		return
	}
	for _, part := range glob.SplitGlobAware(query, ',') {
		part = strings.TrimSpace(part)
		switch {
		case part == "":
		case strings.HasPrefix(part, "\\"):
			f.queries = append(f.queries, replQuery{text: part[1:]})
		case strings.HasPrefix(part, "!"):
			f.queries = append(f.queries, replQuery{exclude: true, text: part[1:]})
		default:
			f.queries = append(f.queries, replQuery{text: part})
		}
	}
}

// Empty tells whether the filter lets everything through.
func (f *ReplFilter) Empty() bool { return len(f.queries) == 0 }

// Visible tells whether the element shows. parentVisible is whether its group does (a line of a group that is shown shows when the
// filter has only terms to leave out).
func (f *ReplFilter) Visible(e ReplElement, parentVisible bool) bool {
	switch e.(type) {
	case *ReplInput, *ReplResult:
		return true
	}
	if f.Empty() {
		return true
	}
	text := e.Text(true)
	includePresent, includeMatched := false, false
	for _, q := range f.queries {
		_, ok := fuzzy.MatchesFuzzy(q.text, text, false)
		switch {
		case q.exclude && ok:
			return false
		case !q.exclude:
			includePresent = true
			includeMatched = includeMatched || ok
		}
	}
	if includePresent {
		return includeMatched
	}
	return parentVisible
}
