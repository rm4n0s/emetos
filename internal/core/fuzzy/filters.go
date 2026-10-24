package fuzzy

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// This file ports the filters of VS Code that the quick input uses to narrow lists of commands,
// views and picks. Positions are offsets in runes, like the rest of the package.
//
// Not ported: the alternate characters of Korean in matchesWords (natural language search for
// Korean jamo), the icon-aware variants (labels in Emetos carry no $(icon) markers) and
// matchesFuzzy2.

// Filter matches a word against a target and returns where in the target the word matched, or
// ok false when it does not. It is VS Code's IFilter.
type Filter func(word, target string) (matches []Match, ok bool)

// Or combines filters: the first one that matches decides the result.
//
// Ported from VS Code: src/vs/base/common/filters.ts (or)
func Or(filters ...Filter) Filter {
	return func(word, target string) ([]Match, bool) {
		for _, f := range filters {
			if m, ok := f(word, target); ok {
				return m, true
			}
		}
		return nil, false
	}
}

// MatchesContiguousSubString matches when the word is a substring of the target, ignoring case.
//
// Ported from VS Code: src/vs/base/common/filters.ts (matchesContiguousSubString)
func MatchesContiguousSubString(word, target string) ([]Match, bool) {
	w, t := []rune(word), []rune(target)
	if len(w) > len(t) {
		return nil, false
	}
	i := indexRunes(appendLower(nil, t), appendLower(nil, w))
	if i < 0 {
		return nil, false
	}
	return []Match{{Start: i, End: i + len(w)}}, true
}

// MatchesBaseContiguousSubString matches when the base form of the word (lower case, without
// accents) is a substring of the base form of the target.
//
// Ported from VS Code: src/vs/base/common/filters.ts (matchesBaseContiguousSubString)
func MatchesBaseContiguousSubString(word, target string) ([]Match, bool) {
	w, t := []rune(word), []rune(target)
	if len(w) > len(t) {
		return nil, false
	}
	bw, bt := []rune(TryNormalizeToBase(word)), []rune(TryNormalizeToBase(target))
	i := indexRunes(bt, bw)
	if i < 0 {
		return nil, false
	}
	return []Match{{Start: i, End: i + len(bw)}}, true
}

// MatchesSubString matches when the characters of the word are in the target in order, ignoring
// case; the matches are the runs of consecutive characters.
//
// Ported from VS Code: src/vs/base/common/filters.ts (matchesSubString)
func MatchesSubString(word, target string) ([]Match, bool) {
	w, t := []rune(word), []rune(target)
	if len(w) > len(t) {
		return nil, false
	}
	w, t = appendLower(nil, w), appendLower(nil, t)
	var out []Match
	j := 0
	for i := 0; i < len(w); i++ {
		for j < len(t) && t[j] != w[i] {
			j++
		}
		if j == len(t) {
			return nil, false
		}
		out = joinMatch(out, Match{Start: j, End: j + 1})
		j++
	}
	if out == nil {
		out = []Match{}
	}
	return out, true
}

// joinMatch appends m to a list of matches, extending the last one when they touch.
func joinMatch(list []Match, m Match) []Match {
	if n := len(list); n > 0 && list[n-1].End == m.Start {
		list[n-1].End = m.End
		return list
	}
	return append(list, m)
}

func isLowerASCII(c rune) bool  { return 'a' <= c && c <= 'z' }
func isNumberASCII(c rune) bool { return '0' <= c && c <= '9' }
func isWhitespaceASCII(c rune) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}
func isAlphanumericASCII(c rune) bool { return isLowerASCII(c) || isUpper(c) || isNumberASCII(c) }

// isWordSeparator tells whether a character separates words of written text.
func isWordSeparator(c rune) bool {
	return isWhitespaceASCII(c) || strings.ContainsRune("()[]{}<>`'\"-/;:,.?!", c)
}

func charactersMatch(a, b rune) bool {
	return a == b || (isWordSeparator(a) && isWordSeparator(b))
}

func nextWord(word []rune, start int) int {
	for i := start; i < len(word); i++ {
		if isWordSeparator(word[i]) || (i > 0 && isWordSeparator(word[i-1])) {
			return i
		}
	}
	return len(word)
}

// MatchesWords matches the word against the beginnings of the words of the target: with
// contiguous "pul" matches "Git: Pull"; without it "gp" and "g p" match it too.
//
// Ported from VS Code: src/vs/base/common/filters.ts (matchesWords)
func MatchesWords(word, target string, contiguous bool) ([]Match, bool) {
	if target == "" {
		return nil, false
	}
	w, t := []rune(TryNormalizeToBase(word)), []rune(TryNormalizeToBase(target))
	memo := map[int]*[]Match{}
	for ti := 0; ti < len(t); {
		if r, ok := matchWords(w, t, 0, ti, contiguous, memo); ok {
			if r == nil {
				r = []Match{}
			}
			return r, true
		}
		ti = nextWord(t, ti+1)
	}
	return nil, false
}

func cloneMatches(m []Match) []Match {
	if m == nil {
		return nil
	}
	return append([]Match{}, m...)
}

func matchWords(word, target []rune, wi, ti int, contiguous bool, memo map[int]*[]Match) ([]Match, bool) {
	if wi == len(word) {
		return nil, true
	}
	if ti == len(target) {
		return nil, false
	}
	key := wi*(len(target)+1) + ti
	if c, ok := memo[key]; ok {
		if c == nil {
			return nil, false
		}
		return cloneMatches(*c), true
	}
	r, ok := matchWordsCompute(word, target, wi, ti, contiguous, memo)
	if ok {
		c := cloneMatches(r)
		memo[key] = &c
	} else {
		memo[key] = nil
	}
	return r, ok
}

func matchWordsCompute(word, target []rune, wi, ti int, contiguous bool, memo map[int]*[]Match) ([]Match, bool) {
	if !charactersMatch(word[wi], target[ti]) {
		return nil, false
	}
	next := ti + 1
	result, ok := matchWords(word, target, wi+1, next, contiguous, memo)
	if !contiguous {
		for !ok {
			next = nextWord(target, next)
			if next >= len(target) {
				break
			}
			result, ok = matchWords(word, target, wi+1, next, contiguous, memo)
			next++
		}
	}
	if !ok {
		return nil, false
	}
	// characters that are not equal are separators: they are not part of the matches
	if word[wi] != target[ti] {
		return result, true
	}
	head := Match{Start: ti, End: ti + 1}
	if len(result) == 0 {
		return []Match{head}, true
	}
	if head.End == result[0].Start {
		result[0].Start = head.Start
		return result, true
	}
	return append([]Match{head}, result...), true
}

func nextAnchor(word []rune, start int) int {
	for i := start; i < len(word); i++ {
		c := word[i]
		if isUpper(c) || isNumberASCII(c) || (i > 0 && !isAlphanumericASCII(word[i-1])) {
			return i
		}
	}
	return len(word)
}

func matchCamelCase(word []rune, camel []rune, i, j int) ([]Match, bool) {
	switch {
	case i == len(word):
		return nil, true
	case j == len(camel):
		return nil, false
	case word[i] != lowerRune(camel[j]):
		return nil, false
	}
	result, ok := matchCamelCase(word, camel, i+1, j+1)
	for next := j + 1; !ok; {
		next = nextAnchor(camel, next)
		if next >= len(camel) {
			break
		}
		result, ok = matchCamelCase(word, camel, i+1, next)
		next++
	}
	if !ok {
		return nil, false
	}
	head := Match{Start: j, End: j + 1}
	if len(result) == 0 {
		return []Match{head}, true
	}
	if head.End == result[0].Start {
		result[0].Start = head.Start
		return result, true
	}
	return append([]Match{head}, result...), true
}

func isCamelCasePattern(word []rune) bool {
	upper, lower, ws := 0, 0, 0
	for _, c := range word {
		if isUpper(c) {
			upper++
		}
		if isLowerASCII(c) {
			lower++
		}
		if isWhitespaceASCII(c) {
			ws++
		}
	}
	if (upper == 0 || lower == 0) && ws == 0 {
		return len(word) <= 30
	}
	return upper <= 5
}

// MatchesCamelCase matches the word against the humps of a camel case target ("fB" matches
// "fooBar").
//
// Ported from VS Code: src/vs/base/common/filters.ts (matchesCamelCase)
func MatchesCamelCase(word, camelCaseWord string) ([]Match, bool) {
	camelCaseWord = strings.TrimFunc(camelCaseWord, func(r rune) bool { return jsSpace(r) })
	if camelCaseWord == "" {
		return nil, false
	}
	w := []rune(word)
	if !isCamelCasePattern(w) {
		return nil, false
	}
	c := []rune(camelCaseWord)
	if len(c) > 60 {
		c = c[:60]
	}
	var upper, lower, alpha, numeric int
	for _, ch := range c {
		if isUpper(ch) {
			upper++
		}
		if isLowerASCII(ch) {
			lower++
		}
		if isAlphanumericASCII(ch) {
			alpha++
		}
		if isNumberASCII(ch) {
			numeric++
		}
	}
	n := float64(len(c))
	up, lo, al, nu := float64(upper)/n, float64(lower)/n, float64(alpha)/n, float64(numeric)/n
	if !(lo > 0.2 && up < 0.8 && al > 0.6 && nu < 0.2) {
		if !(lo == 0 && up > 0.6) {
			return nil, false
		}
		c = appendLower(nil, c)
	}
	w = appendLower(nil, w)
	for i := 0; i < len(c); i = nextAnchor(c, i+1) {
		if r, ok := matchCamelCase(w, c, 0, i); ok {
			if r == nil {
				r = []Match{}
			}
			return r, true
		}
	}
	return nil, false
}

// wildcardMatch finds the pattern, in which * stands for any text and the other characters are
// themselves, in the target ignoring case (the RegExp that matchesFuzzy builds).
func wildcardMatch(pattern, target []rune) (Match, bool) {
	parts := splitRunes(appendLower(nil, pattern), '*')
	t := appendLower(nil, target)
	for start := 0; start <= len(t); start++ {
		pos := start
		ok := true
		for i, part := range parts {
			if i == 0 {
				if !hasRunesAt(t, part, pos) {
					ok = false
					break
				}
				pos += len(part)
				continue
			}
			// ".*" is greedy, but only the start and the end of the whole match are reported, and the
			// match of a greedy ".*" ends at the end of the text
			idx := indexRunes(t[pos:], part)
			if idx < 0 {
				ok = false
				break
			}
			pos += idx + len(part)
		}
		if !ok {
			continue
		}
		if len(parts) > 1 {
			pos = len(t) // a trailing greedy ".*" takes the rest; the last part may still be later in the text
			last := parts[len(parts)-1]
			if len(last) > 0 {
				pos = lastIndexRunes(t, last) + len(last)
			}
		}
		return Match{Start: start, End: pos}, true
	}
	return Match{}, false
}

func splitRunes(s []rune, sep rune) [][]rune {
	var out [][]rune
	start := 0
	for i, r := range s {
		if r == sep {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}

func hasRunesAt(s, sub []rune, at int) bool {
	if at+len(sub) > len(s) {
		return false
	}
	for i, r := range sub {
		if s[at+i] != r {
			return false
		}
	}
	return true
}

func indexRunes(s, sub []rune) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if hasRunesAt(s, sub, i) {
			return i
		}
	}
	return -1
}

func lastIndexRunes(s, sub []rune) int {
	for i := len(s) - len(sub); i >= 0; i-- {
		if hasRunesAt(s, sub, i) {
			return i
		}
	}
	return -1
}

// MatchesFuzzy matches a word against a target the way VS Code's pickers do: a pattern with *
// matches as a wildcard, then a prefix, then camel case humps, then a substring (one stretch,
// or with enableSeparateSubstringMatching the characters in order).
//
// Ported from VS Code: src/vs/base/common/filters.ts (matchesFuzzy)
func MatchesFuzzy(word, target string, enableSeparateSubstringMatching bool) ([]Match, bool) {
	if m, ok := wildcardMatch([]rune(word), []rune(target)); ok {
		return []Match{m}, true
	}
	if enableSeparateSubstringMatching {
		return fuzzySeparateFilter(word, target)
	}
	return fuzzyContiguousFilter(word, target)
}

var (
	fuzzyContiguousFilter = Or(MatchesPrefix, MatchesCamelCase, MatchesContiguousSubString)
	fuzzySeparateFilter   = Or(MatchesPrefix, MatchesCamelCase, MatchesSubString)
)

// MatchesCommandWords is the filter of the command palette: the word as a contiguous substring
// of the base forms of the target, else as the beginnings of its words.
//
// Ported from VS Code: src/vs/platform/quickinput/browser/commandsQuickAccess.ts (WORD_FILTER)
func MatchesCommandWords(word, target string) ([]Match, bool) {
	return Or(MatchesBaseContiguousSubString, func(w, t string) ([]Match, bool) { return MatchesWords(w, t, false) })(word, target)
}

// FuzzyContains tells whether the characters of the query are in the target in order (not
// necessarily next to each other), ignoring case.
//
// Ported from VS Code: src/vs/base/common/strings.ts (fuzzyContains)
func FuzzyContains(target, query string) bool {
	if target == "" || query == "" {
		return false
	}
	t, q := appendLower(nil, []rune(target)), appendLower(nil, []rune(query))
	if len(t) < len(q) {
		return false
	}
	pos := 0
	for _, c := range q {
		found := false
		for pos < len(t) {
			pos++
			if t[pos-1] == c {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// TryNormalizeToBase turns a string into its base form: decomposed, without accents and in lower
// case. A string whose length would change keeps its accents, so that offsets stay valid.
//
// Ported from VS Code: src/vs/base/common/normalization.ts (tryNormalizeToBase)
func TryNormalizeToBase(s string) string {
	ascii := true
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			ascii = false
			break
		}
	}
	if ascii {
		return strings.Map(func(r rune) rune { return lowerRune(r) }, s)
	}
	decomposed := norm.NFD.String(s)
	var b strings.Builder
	for _, r := range decomposed {
		if 0x300 <= r && r <= 0x36f {
			continue
		}
		b.WriteRune(r)
	}
	out := b.String()
	if len([]rune(out)) != len([]rune(s)) {
		out = s
	}
	return strings.Map(unicode.ToLower, out)
}

// CompareAnything compares two strings that a query found, as the pickers sort their results:
// the strings that start with lookFor (lower case) first, the shorter first, then the ones that
// end with it, then by file name rules and the collation of the root locale.
//
// Ported from VS Code: src/vs/base/common/comparers.ts (compareAnything)
func CompareAnything(one, other, lookFor string) int {
	return new(Scorer).compareAnything(one, other, lookFor)
}

// Collate compares two strings by the collation of the root locale, as String.localeCompare
// does; it is negative when a goes first.
func Collate(a, b string) int {
	plain, _ := new(Scorer).collators()
	return plain.CompareString(a, b)
}
