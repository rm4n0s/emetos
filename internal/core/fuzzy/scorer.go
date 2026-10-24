package fuzzy

import (
	"slices"

	"golang.org/x/text/collate"
	"golang.org/x/text/language"
)

// maxMatrixCells bounds the matrix of ScoreFuzzy (query length times target length). VS Code
// has no bound, so a very long query on a very long label allocates without limit there; here
// a pair that is too big scores nothing.
const maxMatrixCells = 1 << 21

// maxKeptCells is the biggest scratch matrix a Scorer keeps for the next call.
const maxKeptCells = 1 << 16

// Scorer scores strings. It owns the scratch memory of the dynamic programs, so a caller that
// scores many strings (a quick open over 100,000 paths) allocates almost nothing after the
// first call. The zero value is ready to use. A Scorer is not safe for concurrent use: give
// every goroutine its own. A nil *Scorer is not valid; use the package level functions or
// new(Scorer).
type Scorer struct {
	tables *wordTables

	// matrix of ScoreFuzzy
	scores, matches []int32
	positions       []int

	// runes of the arguments of the string functions
	tgt, tgtLow, qry, qryLow []rune
	pat, patLow, word        []rune
	wordLow                  []rune

	item itemRunes

	collator, baseCollator *collate.Collator
}

// NewScorer returns a Scorer.
func NewScorer() *Scorer { return new(Scorer) }

// NoScore is the result of a scoring that did not match.
var NoScore = Score{}

// Score is the result of ScoreFuzzy.
type Score struct {
	// Score is the score of the match: 0 when the target does not match, else positive.
	Score int
	// Positions are the offsets (in runes) in the target of the matching runes, ascending.
	Positions []int
}

// ScoreFuzzy scores query against target: the query matches when its runes occur in the target
// in order (when allowNonContiguousMatches is false the query has to occur as a whole).
// queryLower is the query in lower case, as PrepareQuery gives it. Runes that start words,
// follow a separator, continue a run or have the case of the query score more.
//
// Ported from VS Code: src/vs/base/common/fuzzyScorer.ts (scoreFuzzy)
func (s *Scorer) ScoreFuzzy(target, query, queryLower string, allowNonContiguousMatches bool) Score {
	if target == "" || query == "" {
		return NoScore
	}
	t := appendRunes(s.tgt[:0], target)
	tl := appendLower(s.tgtLow[:0], t)
	q := appendRunes(s.qry[:0], query)
	ql := appendRunes(s.qryLow[:0], queryLower)
	s.tgt, s.tgtLow, s.qry, s.qryLow = t, tl, q, ql
	if len(q) != len(ql) {
		return NoScore
	}
	score, pos := s.scoreRunes(t, tl, q, ql, allowNonContiguousMatches)
	return Score{Score: score, Positions: pos}
}

// ScoreFuzzy is Scorer.ScoreFuzzy with a new Scorer.
func ScoreFuzzy(target, query, queryLower string, allowNonContiguousMatches bool) Score {
	return new(Scorer).ScoreFuzzy(target, query, queryLower, allowNonContiguousMatches)
}

// scoreRunes is scoreFuzzy and doScoreFuzzy on runes. The positions are nil when the score is 0.
//
// Before the matrix is filled it checks that the query is a subsequence of the target at all
// (VS Code fills the matrix first and ends with a score of 0): the result is the same and the
// targets that do not match, nearly all of them in a file search, cost no matrix.
//
// Ported from VS Code: src/vs/base/common/fuzzyScorer.ts (scoreFuzzy, doScoreFuzzy)
func (s *Scorer) scoreRunes(target, targetLower, query, queryLower []rune, allowNonContiguousMatches bool) (int, []int) {
	targetLength := len(target)
	queryLength := len(query)

	if targetLength == 0 || queryLength == 0 {
		return 0, nil
	}
	if targetLength < queryLength {
		return 0, nil // impossible for query to be contained in target
	}
	if targetLength > maxMatrixCells/queryLength {
		return 0, nil
	}
	if !isSubsequence(targetLower, queryLower) {
		return 0, nil
	}

	cells := targetLength * queryLength
	if cap(s.scores) < cells {
		s.scores = make([]int32, cells)
		s.matches = make([]int32, cells)
	}
	scores := s.scores[:cells]
	matches := s.matches[:cells]

	//
	// Build Scorer Matrix:
	//
	// The matrix is composed of query q and target t. For each index we score
	// q[i] with t[i] and compare that with the previous score. If the score is
	// equal or larger, we keep the match. In addition to the score, we also keep
	// the length of the consecutive matches to use as boost for the score.
	//
	//      t   a   r   g   e   t
	//  q
	//  u
	//  e
	//  r
	//  y
	//
	for queryIndex := 0; queryIndex < queryLength; queryIndex++ {
		queryIndexOffset := queryIndex * targetLength
		queryIndexPreviousOffset := queryIndexOffset - targetLength

		queryIndexGtNull := queryIndex > 0

		queryCharAtIndex := query[queryIndex]
		queryLowerCharAtIndex := queryLower[queryIndex]

		for targetIndex := 0; targetIndex < targetLength; targetIndex++ {
			targetIndexGtNull := targetIndex > 0

			currentIndex := queryIndexOffset + targetIndex
			leftIndex := currentIndex - 1
			diagIndex := queryIndexPreviousOffset + targetIndex - 1

			var leftScore, diagScore, matchesSequenceLength int32
			if targetIndexGtNull {
				leftScore = scores[leftIndex]
			}
			if queryIndexGtNull && targetIndexGtNull {
				diagScore = scores[diagIndex]
				matchesSequenceLength = matches[diagIndex]
			}

			// If we are not matching on the first query character any more, we only produce a
			// score if we had a score previously for the last query index (by looking at the diagScore).
			// This makes sure that the query always matches in sequence on the target. For example
			// given a target of "ede" and a query of "de", we would otherwise produce a wrong high score
			// for query[1] ("e") matching on target[0] ("e") because of the "beginning of word" boost.
			var score int32
			if diagScore == 0 && queryIndexGtNull {
				score = 0
			} else {
				score = computeCharScore(queryCharAtIndex, queryLowerCharAtIndex, target, targetLower, targetIndex, matchesSequenceLength)
			}

			// We have a score and its equal or larger than the left score
			// Match: sequence continues growing from previous diag value
			// Score: increases by diag score value
			// With a contiguous match (a quoted query) the first query rune has to start a
			// place where the whole query occurs in the target; the later ones follow from it,
			// otherwise there would have been no score.
			isValidScore := score != 0 && diagScore+score >= leftScore
			if isValidScore && (allowNonContiguousMatches || queryIndexGtNull || hasPrefixAt(targetLower, queryLower, targetIndex)) {
				matches[currentIndex] = matchesSequenceLength + 1
				scores[currentIndex] = diagScore + score
			} else {
				// We either have no score or the score is lower than the left score
				// Match: reset to 0
				// Score: pick up from left hand side
				matches[currentIndex] = 0
				scores[currentIndex] = leftScore
			}
		}
	}

	// Restore Positions (starting from bottom right of matrix)
	positions := s.positions[:0]
	queryIndex := queryLength - 1
	targetIndex := targetLength - 1
	for queryIndex >= 0 && targetIndex >= 0 {
		currentIndex := queryIndex*targetLength + targetIndex
		if matches[currentIndex] == 0 {
			targetIndex-- // go left
		} else {
			positions = append(positions, targetIndex)

			// go up and left
			queryIndex--
			targetIndex--
		}
	}
	s.positions = positions

	final := int(scores[queryLength*targetLength-1])
	if cells > maxKeptCells {
		s.scores, s.matches = nil, nil
	}
	if final == 0 {
		return 0, nil
	}
	out := slices.Clone(positions)
	slices.Reverse(out)
	return final, out
}

// isSubsequence tells whether the runes of query occur in order in target, with the equality
// of the scorer.
func isSubsequence(target, query []rune) bool {
	qi := 0
	for _, t := range target {
		if considerAsEqual(query[qi], t) {
			qi++
			if qi == len(query) {
				return true
			}
		}
	}
	return false
}

// hasPrefixAt tells whether s has prefix at offset at.
func hasPrefixAt(s, prefix []rune, at int) bool {
	if at+len(prefix) > len(s) {
		return false
	}
	for i, r := range prefix {
		if s[at+i] != r {
			return false
		}
	}
	return true
}

// Ported from VS Code: src/vs/base/common/fuzzyScorer.ts (computeCharScore)
func computeCharScore(queryCharAtIndex, queryLowerCharAtIndex rune, target, targetLower []rune, targetIndex int, matchesSequenceLength int32) int32 {
	if !considerAsEqual(queryLowerCharAtIndex, targetLower[targetIndex]) {
		return 0 // no match of characters
	}

	// Character match bonus
	var score int32 = 1

	// Consecutive match bonus: sequences up to 3 get the full bonus (6)
	// and the remainder gets half the bonus (3). This helps reduce the
	// overall boost for long sequence matches.
	if matchesSequenceLength > 0 {
		score += min(matchesSequenceLength, 3)*6 + max(0, matchesSequenceLength-3)*3
	}

	// Same case bonus
	if queryCharAtIndex == target[targetIndex] {
		score++
	}

	if targetIndex == 0 {
		// Start of word bonus
		score += 8
	} else if separatorBonus := scoreSeparatorAtPos(target[targetIndex-1]); separatorBonus != 0 {
		// After separator bonus
		score += separatorBonus
	} else if isUpper(target[targetIndex]) && matchesSequenceLength == 0 {
		// Inside word upper case bonus (camel case). We only give this bonus if we're not in a contiguous sequence.
		// For example:
		// NPE => NullPointerException = boost
		// HTTP => HTTP = not boost
		score += 2
	}

	return score
}

// Ported from VS Code: src/vs/base/common/fuzzyScorer.ts (considerAsEqual)
func considerAsEqual(a, b rune) bool {
	if a == b {
		return true
	}

	// Special case path separators: ignore platform differences
	if a == '/' || a == '\\' {
		return b == '/' || b == '\\'
	}

	return false
}

// Ported from VS Code: src/vs/base/common/fuzzyScorer.ts (scoreSeparatorAtPos)
func scoreSeparatorAtPos(charCode rune) int32 {
	switch charCode {
	case '/', '\\':
		return 5 // prefer path separators...
	case '_', '-', '.', ' ', '\'', '"', ':':
		return 4 // ...over other separators
	default:
		return 0
	}
}

// collators returns the collators of the comparisons, made on first use. A collate.Collator
// keeps a buffer, so they belong to the Scorer.
func (s *Scorer) collators() (plain, base *collate.Collator) {
	if s.collator == nil {
		s.collator = collate.New(language.Und)
		s.baseCollator = collate.New(language.Und, collate.Numeric, collate.Loose)
	}
	return s.collator, s.baseCollator
}
