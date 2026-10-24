package fuzzy

import (
	"slices"
)

// Match is a range of a string that matched: Start is the offset of the first rune and End the
// offset after the last one (offsets in runes). It is VS Code's IMatch.
type Match struct {
	Start, End int
}

// CreateMatches turns ascending positions into matches, joining positions that follow each
// other. Ported from VS Code: src/vs/base/common/fuzzyScorer.ts (createMatches).
func CreateMatches(positions []int) []Match {
	var ret []Match
	for _, pos := range positions {
		if n := len(ret); n > 0 && ret[n-1].End == pos {
			ret[n-1].End++
		} else {
			ret = append(ret, Match{Start: pos, End: pos + 1})
		}
	}
	return ret
}

// Matches returns the matches of a word score (VS Code's createMatches of filters.ts).
// Ported from VS Code: src/vs/base/common/filters.ts (createMatches)
func (w WordScore) Matches() []Match {
	return CreateMatches(w.Positions)
}

// MatchesPrefix reports whether wordToMatchAgainst starts with word, ignoring case, and gives
// the matching range. Ported from VS Code: src/vs/base/common/filters.ts (matchesPrefix)
func MatchesPrefix(word, wordToMatchAgainst string) ([]Match, bool) {
	w := appendRunes(nil, word)
	t := appendRunes(nil, wordToMatchAgainst)
	if !hasPrefixFold(t, w) {
		return nil, false
	}
	if len(w) > 0 {
		return []Match{{Start: 0, End: len(w)}}, true
	}
	return []Match{}, true
}

// hasPrefixFold reports whether str starts with candidate, ignoring case, and is not empty.
// Ported from VS Code: src/vs/base/common/filters.ts (_matchesPrefix with ignoreCase)
func hasPrefixFold(str, candidate []rune) bool {
	if len(str) == 0 || len(str) < len(candidate) {
		return false
	}
	for i, c := range candidate {
		s := str[i]
		if s != c && lowerRune(s) != lowerRune(c) {
			return false
		}
	}
	return true
}

// isUpper tells whether code is an ASCII upper case letter.
// Ported from VS Code: src/vs/base/common/filters.ts (isUpper)
func isUpper(code rune) bool {
	return 'A' <= code && code <= 'Z'
}

// isSeparatorAtPos tells whether the rune at index is a separator of words. Out of range
// indexes are not separators.
// Ported from VS Code: src/vs/base/common/filters.ts (isSeparatorAtPos)
func isSeparatorAtPos(value []rune, index int) bool {
	if index < 0 || index >= len(value) {
		return false
	}
	switch value[index] {
	case '_', '-', '.', ' ', '/', '\\', '\'', '"', ':', '$', '<', '>', '(', ')', '[', ']', '{', '}':
		return true
	}
	return isEmojiImprecise(value[index])
}

// isWhitespaceAtPos tells whether the rune at index is a space or a tab.
// Ported from VS Code: src/vs/base/common/filters.ts (isWhitespaceAtPos)
func isWhitespaceAtPos(value []rune, index int) bool {
	if index < 0 || index >= len(value) {
		return false
	}
	return value[index] == ' ' || value[index] == '\t'
}

// isEmojiImprecise is a rough test for emoji code points.
// Ported from VS Code: src/vs/base/common/strings.ts (isEmojiImprecise)
func isEmojiImprecise(x rune) bool {
	return (x >= 0x1F1E6 && x <= 0x1F1FF) || x == 8986 || x == 8987 || x == 9200 ||
		x == 9203 || (x >= 9728 && x <= 10175) || x == 11088 || x == 11093 ||
		(x >= 127744 && x <= 128591) || (x >= 128640 && x <= 128764) ||
		(x >= 128992 && x <= 129008) || (x >= 129280 && x <= 129535) ||
		(x >= 129648 && x <= 129782)
}

// normalizeMatches sorts matches by start and merges the ones that overlap or touch. It sorts
// the slice it is given and returns a new, never nil, slice.
// Ported from VS Code: src/vs/base/common/fuzzyScorer.ts (normalizeMatches)
func normalizeMatches(matches []Match) []Match {
	slices.SortStableFunc(matches, func(a, b Match) int { return a.Start - b.Start })

	normalized := make([]Match, 0, len(matches))
	for _, m := range matches {
		n := len(normalized)
		if n == 0 || !matchOverlaps(normalized[n-1], m) {
			normalized = append(normalized, m)
			continue
		}
		cur := &normalized[n-1]
		cur.Start = min(cur.Start, m.Start)
		cur.End = max(cur.End, m.End)
	}
	return normalized
}

// matchOverlaps tells whether two matches overlap or touch.
// Ported from VS Code: src/vs/base/common/fuzzyScorer.ts (matchOverlaps)
func matchOverlaps(a, b Match) bool {
	if a.End < b.Start {
		return false
	}
	if b.End < a.Start {
		return false
	}
	return true
}
