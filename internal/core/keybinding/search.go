package keybinding

import (
	"regexp"
	"slices"
	"strings"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/fuzzy"
)

// The search of the keybindings editor (SPEC §11.3): free text over the command, its id, the when-clause and
// the source; the keys, written as words ("ctrl k") or as an exact sequence in quotes ("ctrl+k ctrl+c"); and the
// filters @source:, @command: (with +when:) and @keybinding:.
//
// Ported from VS Code: src/vs/workbench/services/preferences/browser/keybindingsEditorModel.ts (fetch,
// KeybindingItemMatches). Not ported: @ext: and the filter @source:extension (no extensions), and the labels
// of a command in the language of the UI that differ from the English ones.

var (
	commandFilter    = regexp.MustCompile(`(?i)@command:\s*([^+]+)`)
	whenFilter       = regexp.MustCompile(`(?i)\+when:\s*(.+)`)
	sourceFilter     = regexp.MustCompile(`(?i)@source:\s*(user|default|system)`)
	sourceUserFilter = regexp.MustCompile(`(?i)@source:\s*user`)
	keybindingFilter = regexp.MustCompile(`(?i)@keybinding:\s*((".+")|(\S+))`)
)

// ChordMatch says which parts of a chord a search matched: the modifiers and the key.
type ChordMatch struct {
	Ctrl, Shift, Alt, Meta, Key bool
}

func (c ChordMatch) any() bool { return c.Ctrl || c.Shift || c.Alt || c.Meta || c.Key }

// KeyMatches are the matches of a search in the keybinding of a row: its first chord and, for a sequence, the
// second.
type KeyMatches struct {
	First, Chord ChordMatch
}

// Result is a row that a search found, with where the search matched in it (for the highlights).
type Result struct {
	Row Row
	// Label, ID, When and Source are the places the search matched in the text of those columns, as rune
	// offsets.
	Label, ID, When, Source []fuzzy.Match
	// Keybinding says which chords and keys of the keybinding matched; nil when the search did not match it.
	Keybinding *KeyMatches
}

// Fetch lists the rows that a search finds, in the order of the table. An empty search finds every row.
func (m *Model) Fetch(search string) []Result {
	rows := m.rows
	// @command:ID, with +when:CLAUSE
	if mm := commandFilter.FindStringSubmatch(search); mm != nil && strings.TrimSpace(mm[1]) != "" {
		command := strings.TrimSpace(mm[1])
		var found []Row
		for _, r := range rows {
			if r.Command == command {
				found = append(found, r)
			}
		}
		if len(found) > 0 {
			if wm := whenFilter.FindStringSubmatch(search); wm != nil && strings.TrimSpace(wm[1]) != "" {
				found = filterByWhen(found, command, strings.TrimSpace(wm[1]))
			}
		}
		return plain(found)
	}
	switch {
	case sourceFilter.MatchString(search):
		rows = filterBySource(rows, search)
		search = sourceFilter.ReplaceAllString(search, "")
	default:
		if mm := keybindingFilter.FindStringSubmatch(search); mm != nil && (mm[2] != "" || mm[3] != "") {
			if mm[2] != "" {
				search = mm[2]
			} else {
				search = `"` + mm[3] + `"`
			}
		}
	}
	search = strings.TrimSpace(search)
	if search == "" {
		return plain(rows)
	}
	return filterByText(rows, search)
}

func plain(rows []Row) []Result {
	out := make([]Result, len(rows))
	for i, r := range rows {
		out[i] = Result{Row: r}
	}
	return out
}

func filterBySource(rows []Row, search string) []Row {
	want := SourceDefault
	if sourceUserFilter.MatchString(search) {
		want = SourceUser
	}
	var out []Row
	for _, r := range rows {
		if r.Source == want {
			out = append(out, r)
		}
	}
	return out
}

// filterByWhen keeps the rows with the when-clause; when there is none, a row for the command with that
// clause that is not in the table (the editor offers to add a keybinding under it).
func filterByWhen(rows []Row, command, when string) []Row {
	var same []Row
	for _, r := range rows {
		if r.When == when {
			same = append(same, r)
		}
	}
	if len(same) > 0 {
		return same
	}
	row := Row{Command: command, Label: rows[0].Label, When: when, Source: SourceUser}
	if expr, err := contextkey.Parse(when); err == nil {
		row.When = expr.Serialize()
	}
	row.ID = rowID(row)
	return []Row{row}
}

// filter is a function that matches a word against a text.
type filter = func(word, target string) ([]fuzzy.Match, bool)

var (
	wordFilter  = fuzzy.Or(fuzzy.MatchesBaseContiguousSubString, func(w, t string) ([]fuzzy.Match, bool) { return fuzzy.MatchesWords(w, t, false) })
	idFilter    = fuzzy.Or(func(w, t string) ([]fuzzy.Match, bool) { return fuzzy.MatchesWords(w, t, false) }, fuzzy.MatchesCamelCase)
	phraseFound = func(w, t string) ([]fuzzy.Match, bool) { return fuzzy.MatchesWords(w, t, true) }
)

func filterByText(rows []Row, search string) []Result {
	quoteAtFirst := strings.HasPrefix(search, `"`)
	quoteAtLast := strings.HasSuffix(search, `"`)
	complete := quoteAtFirst && quoteAtLast
	if quoteAtFirst {
		search = search[1:]
	}
	if quoteAtLast && search != "" {
		search = search[:len(search)-1]
	}
	search = strings.TrimSpace(search)
	words := strings.Split(search, " ")
	var keyWords []string
	for _, w := range words {
		for _, part := range strings.Split(w, "+") {
			if part != "" {
				keyWords = append(keyWords, part)
			}
		}
	}
	var out []Result
	for _, r := range rows {
		res := Result{Row: r}
		if !complete {
			res.ID = matchText(search, r.Command, idFilter, words)
			if r.Label != "" {
				res.Label = matchText(search, r.Label, phraseFound, words)
			}
			if r.When != "" {
				res.When = matchText("", r.When, idFilter, words)
			}
			res.Source = matchText(search, r.Source.String(), phraseFound, words)
		}
		if r.Bound() {
			res.Keybinding = matchKeybinding(r.Keybinding, search, keyWords, complete)
		}
		if res.ID != nil || res.Label != nil || res.When != nil || res.Source != nil || res.Keybinding != nil {
			out = append(out, res)
		}
	}
	return out
}

// matchText matches the search as a whole, else each word of it, against a text; nil when it does not match.
func matchText(search, target string, perWord filter, words []string) []fuzzy.Match {
	var matches []fuzzy.Match
	found := false
	if search != "" {
		matches, found = wordFilter(search, target)
	}
	if !found {
		matches, found = matchWords(words, target, perWord)
	}
	if !found {
		return nil
	}
	return filterAndSort(matches)
}

func matchWords(words []string, target string, f filter) ([]fuzzy.Match, bool) {
	var all []fuzzy.Match
	for _, w := range words {
		m, ok := f(w, target)
		if !ok {
			return nil, false
		}
		all = append(all, m...)
	}
	return all, true
}

// filterAndSort drops repeated matches and the ones inside another, and sorts the rest by start.
func filterAndSort(matches []fuzzy.Match) []fuzzy.Match {
	var out []fuzzy.Match
	for _, m := range matches {
		if slices.Contains(out, m) {
			continue
		}
		inside := false
		for _, o := range matches {
			if o != m && o.Start <= m.Start && o.End >= m.End {
				inside = true
				break
			}
		}
		if !inside {
			out = append(out, m)
		}
	}
	slices.SortStableFunc(out, func(a, b fuzzy.Match) int { return a.Start - b.Start })
	if out == nil {
		out = []fuzzy.Match{}
	}
	return out
}

// chordInfo is a chord as the matching of keys sees it.
type chordInfo struct {
	ctrl, shift, alt, meta bool
	key                    string
}

func chordInfoOf(kb Keybinding, i int) *chordInfo {
	if i >= len(kb) {
		return nil
	}
	c := kb[i]
	return &chordInfo{ctrl: c.Ctrl, shift: c.Shift, alt: c.Alt, meta: c.Meta, key: c.Key.String()}
}

// matchKeybinding matches the words of a search against the keys of a keybinding, as VS Code's
// KeybindingItemMatches.matchesKeybinding does: the whole search against the three labels first, then each word
// against a modifier or the key of the first chord, or of the second.
func matchKeybinding(kb Keybinding, search string, words []string, complete bool) *KeyMatches {
	first, second := chordInfoOf(kb, 0), chordInfoOf(kb, 1)
	for _, label := range []string{kb.UserSettingsLabel(), kb.AriaLabel(), kb.Label()} {
		if label != "" && strings.EqualFold(search, label) {
			return &KeyMatches{First: completeChord(first), Chord: completeChord(second)}
		}
	}
	var firstMatch, chordMatch ChordMatch
	var matchedWords, firstWords, chordWords []int
	matchFirst := true
	for index, word := range words {
		firstMatched, chordMatched := false, false
		matchFirst = matchFirst && !firstMatch.Key
		matchChord := !chordMatch.Key
		if matchFirst {
			firstMatched = matchPart(first, &firstMatch, word, complete)
			if firstMatch.Key {
				for _, ci := range chordWords {
					if !slices.Contains(firstWords, ci) {
						if at := slices.Index(matchedWords, ci); at >= 0 {
							matchedWords = slices.Delete(matchedWords, at, at+1)
						}
					}
				}
				chordMatch = ChordMatch{}
				chordWords = nil
				matchChord = false
			}
		}
		if matchChord {
			chordMatched = matchPart(second, &chordMatch, word, complete)
		}
		if firstMatched {
			firstWords = append(firstWords, index)
		}
		if chordMatched {
			chordWords = append(chordWords, index)
		}
		if firstMatched || chordMatched {
			matchedWords = append(matchedWords, index)
		}
		matchFirst = matchFirst && isModifierWord(word)
	}
	if len(matchedWords) != len(words) {
		return nil
	}
	if complete {
		if !isCompleteChord(first, firstMatch) {
			return nil
		}
		if chordMatch.any() && !isCompleteChord(second, chordMatch) {
			return nil
		}
	}
	if firstMatch.any() || chordMatch.any() {
		return &KeyMatches{First: firstMatch, Chord: chordMatch}
	}
	return nil
}

func completeChord(c *chordInfo) ChordMatch {
	if c == nil {
		return ChordMatch{}
	}
	return ChordMatch{Key: true, Ctrl: c.ctrl, Shift: c.shift, Alt: c.alt, Meta: c.meta}
}

func isCompleteChord(c *chordInfo, m ChordMatch) bool {
	if c == nil {
		return true
	}
	if !m.Key {
		return false
	}
	return (!c.meta || m.Meta) && (!c.alt || m.Alt) && (!c.ctrl || m.Ctrl) && (!c.shift || m.Shift)
}

// The words that name a modifier: the labels of the UI, of a screen reader and of keybindings.json.
var (
	ctrlWords  = []string{"Ctrl", "Control", "ctrl"}
	shiftWords = []string{"Shift"}
	altWords   = []string{"Alt", "option"}
	metaWords  = []string{"Super", "meta"}
)

func wordIn(word string, list []string) bool {
	return slices.ContainsFunc(list, func(s string) bool { return strings.EqualFold(s, word) })
}

func isModifierWord(word string) bool {
	return wordIn(word, ctrlWords) || wordIn(word, shiftWords) || wordIn(word, altWords) || wordIn(word, metaWords)
}

func matchPart(c *chordInfo, m *ChordMatch, word string, complete bool) bool {
	if c == nil {
		return false
	}
	matched := false
	if c.meta && wordIn(word, metaWords) {
		matched, m.Meta = true, true
	}
	if c.ctrl && wordIn(word, ctrlWords) {
		matched, m.Ctrl = true, true
	}
	if c.shift && wordIn(word, shiftWords) {
		matched, m.Shift = true, true
	}
	if c.alt && wordIn(word, altWords) {
		matched, m.Alt = true, true
	}
	if matchesKey(c, word, complete) {
		matched, m.Key = true, true
	}
	return matched
}

func matchesKey(c *chordInfo, word string, complete bool) bool {
	label := c.key
	if complete || len([]rune(label)) == 1 || len([]rune(word)) == 1 {
		return strings.EqualFold(label, word)
	}
	_, ok := fuzzy.MatchesContiguousSubString(word, label)
	return ok
}
