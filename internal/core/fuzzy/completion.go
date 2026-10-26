package fuzzy

import "strings"

// This file ports the variants of the word scorer that the suggest widget uses (SPEC §9.12 "Completion"): anyScore for the
// highlights of an item that matched through its filter text, the graceful scorers that forgive a typo, and the check that a
// pattern is in a word. Positions are offsets in runes, like the rest of the package.

// AnyScore matches the pattern, from patternStart, against the word with the first match allowed to be weak, dropping runes
// from the start of the pattern (up to 13 of them) until one of the suffixes matches; the result is a WordScore with score 0
// and no positions when none does. It never fails. The suggest widget uses it to highlight an item whose filter text matched
// but whose label does not.
//
// Ported from VS Code: src/vs/base/common/filters.ts (anyScore)
func (s *Scorer) AnyScore(pattern, patternLow string, patternStart int, word, wordLow string, wordStart int) WordScore {
	limit := min(13, len([]rune(pattern)))
	opts := FuzzyScoreOptions{FirstMatchCanBeWeak: true, BoostFullMatch: true}
	for ; patternStart < limit; patternStart++ {
		if r, ok := s.FuzzyScore(pattern, patternLow, patternStart, word, wordLow, wordStart, opts); ok {
			return r
		}
	}
	return WordScore{Score: 0, WordStart: wordStart}
}

// AnyScore is Scorer.AnyScore with a new Scorer.
func AnyScore(pattern, patternLow string, patternStart int, word, wordLow string, wordStart int) WordScore {
	return new(Scorer).AnyScore(pattern, patternLow, patternStart, word, wordLow, wordStart)
}

// FuzzyScoreGraceful is FuzzyScore that, when the pattern does not match, tries the patterns with two neighbouring runes
// swapped (a typo), at a penalty of 3 for the score.
//
// Ported from VS Code: src/vs/base/common/filters.ts (fuzzyScoreGraceful)
func (s *Scorer) FuzzyScoreGraceful(pattern, patternLow string, patternStart int, word, wordLow string, wordStart int, opts FuzzyScoreOptions) (WordScore, bool) {
	return s.fuzzyScoreWithPermutations(pattern, patternLow, patternStart, word, wordLow, wordStart, false, opts)
}

// FuzzyScoreGracefulAggressive is FuzzyScoreGraceful that also tries the permutations when the pattern matches as it is,
// to find a better alignment (`cno` for `console` as c-o-n and not c-o-n-s-o-l-e).
//
// Ported from VS Code: src/vs/base/common/filters.ts (fuzzyScoreGracefulAggressive)
func (s *Scorer) FuzzyScoreGracefulAggressive(pattern, patternLow string, patternStart int, word, wordLow string, wordStart int, opts FuzzyScoreOptions) (WordScore, bool) {
	return s.fuzzyScoreWithPermutations(pattern, patternLow, patternStart, word, wordLow, wordStart, true, opts)
}

func (s *Scorer) fuzzyScoreWithPermutations(pattern, patternLow string, patternStart int, word, wordLow string, wordStart int, aggressive bool, opts FuzzyScoreOptions) (WordScore, bool) {
	top, ok := s.FuzzyScore(pattern, patternLow, patternStart, word, wordLow, wordStart, opts)
	if ok && !aggressive {
		return top, true
	}
	runes := []rune(pattern)
	if len(runes) >= 3 {
		// A few (at most 7) permutations that swap neighbouring runes: `cnoso` becomes `conso`, `cnsoo`, `cnoos`.
		tries := min(7, len(runes)-1)
		for moving := patternStart + 1; moving < tries; moving++ {
			next, has := nextTypoPermutation(runes, moving)
			if !has {
				continue
			}
			p := string(next)
			if cand, found := s.FuzzyScore(p, strings.ToLower(p), patternStart, word, wordLow, wordStart, opts); found {
				cand.Score -= 3 // the penalty of a permutation
				if !ok || cand.Score > top.Score {
					top, ok = cand, true
				}
			}
		}
	}
	return top, ok
}

// nextTypoPermutation swaps the runes at pos and pos+1; false when there is no pair or the two are equal.
func nextTypoPermutation(pattern []rune, pos int) ([]rune, bool) {
	if pos+1 >= len(pattern) || pattern[pos] == pattern[pos+1] {
		return nil, false
	}
	out := append([]rune(nil), pattern...)
	out[pos], out[pos+1] = out[pos+1], out[pos]
	return out, true
}

// IsPatternInWord tells whether the runes of patternLow occur in order in wordLow (both in lower case).
//
// Ported from VS Code: src/vs/base/common/filters.ts (isPatternInWord)
func IsPatternInWord(patternLow, wordLow string) bool {
	p, w := []rune(patternLow), []rune(wordLow)
	pi := 0
	for wi := 0; pi < len(p) && wi < len(w); wi++ {
		if p[pi] == w[wi] {
			pi++
		}
	}
	return pi == len(p)
}
