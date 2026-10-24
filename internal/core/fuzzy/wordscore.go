package fuzzy

// maxLen is the longest pattern and word the word scorer looks at; longer ones are cut. It
// bounds the size of the tables.
// Ported from VS Code: src/vs/base/common/filters.ts (_maxLen)
const maxLen = 128

const tableStride = maxLen + 1

// arrow says where a cell of the table was reached from.
type arrow int8

const (
	arrowDiag     arrow = 1
	arrowLeft     arrow = 2
	arrowLeftLeft arrow = 3
)

// wordTables are the tables of the word scorer. VS Code keeps them in module level arrays;
// here every Scorer owns one set, allocated on first use.
type wordTables struct {
	minWordMatchPos [tableStride]int // min word position for a certain pattern position
	maxWordMatchPos [tableStride]int // max word position for a certain pattern position
	diag            [tableStride * tableStride]int32
	table           [tableStride * tableStride]int32
	arrows          [tableStride * tableStride]arrow
}

// WordScore is the result of FuzzyScore (VS Code's FuzzyScore of filters.ts).
type WordScore struct {
	// Score is the score of the match; higher is better.
	Score int
	// WordStart is the offset at which matching started.
	WordStart int
	// Positions are the offsets (in runes, from the start of the word) of the matching
	// runes, in ascending order, one for every rune of the pattern that was used.
	Positions []int
}

// FuzzyScoreOptions are the options of FuzzyScore.
type FuzzyScoreOptions struct {
	// FirstMatchCanBeWeak allows a match whose first rune is not at the start of a word.
	FirstMatchCanBeWeak bool
	// BoostFullMatch gives a word that is matched by all its runes a higher score.
	BoostFullMatch bool
}

// DefaultFuzzyScoreOptions are the options VS Code uses when none are given.
var DefaultFuzzyScoreOptions = FuzzyScoreOptions{BoostFullMatch: true}

// FuzzyScore matches pattern (from patternStart) against word (from wordStart) as a
// subsequence and scores the match; false when there is no match. The strings in lower case
// are passed in so that a caller scoring many words does not lower the pattern again. Only the
// first 128 runes of the pattern and of the word are used.
//
// Ported from VS Code: src/vs/base/common/filters.ts (fuzzyScore)
func (s *Scorer) FuzzyScore(pattern, patternLow string, patternStart int, word, wordLow string, wordStart int, opts FuzzyScoreOptions) (WordScore, bool) {
	p := appendRunes(s.pat[:0], pattern)
	pl := appendRunes(s.patLow[:0], patternLow)
	w := appendRunes(s.word[:0], word)
	wl := appendRunes(s.wordLow[:0], wordLow)
	s.pat, s.patLow, s.word, s.wordLow = p, pl, w, wl
	if len(p) != len(pl) || len(w) != len(wl) {
		return WordScore{}, false
	}
	return s.fuzzyScoreRunes(p, pl, patternStart, w, wl, wordStart, opts)
}

// FuzzyScore is Scorer.FuzzyScore with a new Scorer.
func FuzzyScore(pattern, patternLow string, patternStart int, word, wordLow string, wordStart int, opts FuzzyScoreOptions) (WordScore, bool) {
	return new(Scorer).FuzzyScore(pattern, patternLow, patternStart, word, wordLow, wordStart, opts)
}

func (s *Scorer) fuzzyScoreRunes(pattern, patternLow []rune, patternStart int, word, wordLow []rune, wordStart int, opts FuzzyScoreOptions) (WordScore, bool) {
	patternLen := min(len(pattern), maxLen)
	wordLen := min(len(word), maxLen)

	if patternStart < 0 || wordStart < 0 || patternStart >= patternLen || wordStart >= wordLen || (patternLen-patternStart) > (wordLen-wordStart) {
		return WordScore{}, false
	}

	t := s.tables
	if t == nil {
		t = new(wordTables)
		s.tables = t
	}

	// Run a simple check if the characters of pattern occur (in order) at all in word. If that
	// isn't the case we stop because no match will be possible
	if !isPatternInWord(t, patternLow, patternStart, patternLen, wordLow, wordStart, wordLen, true) {
		return WordScore{}, false
	}

	// Find the max matching word position for each pattern position
	// NOTE: the min matching word position was filled in above, in the isPatternInWord call
	fillInMaxWordMatchPos(t, patternLen, wordLen, patternStart, wordStart, patternLow, wordLow)

	row := 1
	column := 1
	hasStrongFirstMatch := false

	// There will be a match, fill in tables
	for patternPos := patternStart; patternPos < patternLen; row, patternPos = row+1, patternPos+1 {

		// Reduce search space to possible matching word positions and to possible access from next row
		minWordMatchPos := t.minWordMatchPos[patternPos]
		maxWordMatchPos := t.maxWordMatchPos[patternPos]
		nextMaxWordMatchPos := wordLen
		if patternPos+1 < patternLen {
			nextMaxWordMatchPos = t.maxWordMatchPos[patternPos+1]
		}

		rowOff := row * tableStride
		prevOff := rowOff - tableStride

		column = minWordMatchPos - wordStart + 1
		for wordPos := minWordMatchPos; wordPos < nextMaxWordMatchPos; column, wordPos = column+1, wordPos+1 {

			score := 0
			scored := false
			canComeDiag := false

			if wordPos <= maxWordMatchPos {
				score, scored = doScoreWord(
					pattern, patternLow, patternPos, patternStart,
					word, wordLow, wordPos, wordLen, wordStart,
					t.diag[prevOff+column-1] == 0,
					&hasStrongFirstMatch,
				)
			}

			diagScore := 0
			if scored {
				canComeDiag = true
				diagScore = score + int(t.table[prevOff+column-1])
			}

			canComeLeft := wordPos > minWordMatchPos
			leftScore := 0
			if canComeLeft {
				leftScore = int(t.table[rowOff+column-1])
				if t.diag[rowOff+column-1] > 0 {
					leftScore -= 5 // penalty for a gap start
				}
			}

			canComeLeftLeft := wordPos > minWordMatchPos+1 && t.diag[rowOff+column-1] > 0
			leftLeftScore := 0
			if canComeLeftLeft {
				leftLeftScore = int(t.table[rowOff+column-2])
				if t.diag[rowOff+column-2] > 0 {
					leftLeftScore -= 5 // penalty for a gap start
				}
			}

			cur := rowOff + column
			switch {
			case canComeLeftLeft && (!canComeLeft || leftLeftScore >= leftScore) && (!canComeDiag || leftLeftScore >= diagScore):
				// always prefer choosing left left to jump over a diagonal because that means a match is earlier in the word
				t.table[cur] = int32(leftLeftScore)
				t.arrows[cur] = arrowLeftLeft
				t.diag[cur] = 0
			case canComeLeft && (!canComeDiag || leftScore >= diagScore):
				// always prefer choosing left since that means a match is earlier in the word
				t.table[cur] = int32(leftScore)
				t.arrows[cur] = arrowLeft
				t.diag[cur] = 0
			case canComeDiag:
				t.table[cur] = int32(diagScore)
				t.arrows[cur] = arrowDiag
				t.diag[cur] = t.diag[prevOff+column-1] + 1
			default:
				// VS Code throws "not possible" here; the case cannot happen.
				return WordScore{}, false
			}
		}
	}

	if !hasStrongFirstMatch && !opts.FirstMatchCanBeWeak {
		return WordScore{}, false
	}

	row--
	column--

	rows := row
	result := WordScore{
		Score:     int(t.table[row*tableStride+column]),
		WordStart: wordStart,
		Positions: make([]int, rows),
	}

	backwardsDiagLength := 0
	maxMatchColumn := 0

	for row >= 1 {
		// Find the column where we go diagonally up
		diagColumn := column
		for {
			a := t.arrows[row*tableStride+diagColumn]
			if a != arrowLeftLeft && a != arrowLeft {
				// found the diagonal
				break
			}
			if a == arrowLeftLeft {
				diagColumn -= 2
			} else {
				diagColumn--
			}
			if diagColumn < 1 {
				break
			}
		}

		// Overturn the "forwards" decision if keeping the "backwards" diagonal would give a better match
		if backwardsDiagLength > 1 && // only if we would have a contiguous match of 3 characters
			patternLow[patternStart+row-1] == wordLow[wordStart+column-1] && // only if we can do a contiguous match diagonally
			!isUpperCaseAtPos(diagColumn+wordStart-1, word, wordLow) && // only if the forwards chose diagonal is not an uppercase
			backwardsDiagLength+1 > int(t.diag[row*tableStride+diagColumn]) { // only if our contiguous match would be longer than the "forwards" contiguous match
			diagColumn = column
		}

		if diagColumn == column {
			// this is a contiguous match
			backwardsDiagLength++
		} else {
			backwardsDiagLength = 1
		}

		if maxMatchColumn == 0 {
			// remember the last matched column
			maxMatchColumn = diagColumn
		}

		row--
		column = diagColumn - 1
		result.Positions[row] = column + wordStart
	}

	if wordLen-wordStart == patternLen && opts.BoostFullMatch {
		// the word matches the pattern with all characters!
		// giving the score a total match boost (to come up ahead other words)
		result.Score += 2
	}

	// Add 1 penalty for each skipped character in the word
	skippedCharsCount := maxMatchColumn - patternLen
	result.Score -= skippedCharsCount

	return result, true
}

// isUpperCaseAtPos tells whether the rune of word at pos has a lower case form.
func isUpperCaseAtPos(pos int, word, wordLow []rune) bool {
	return word[pos] != wordLow[pos]
}

// isPatternInWord checks that the runes of the pattern occur in order in the word, and
// optionally remembers the first position of each in t.minWordMatchPos.
// Ported from VS Code: src/vs/base/common/filters.ts (isPatternInWord)
func isPatternInWord(t *wordTables, patternLow []rune, patternPos, patternLen int, wordLow []rune, wordPos, wordLen int, fillMinWordPosArr bool) bool {
	for patternPos < patternLen && wordPos < wordLen {
		if patternLow[patternPos] == wordLow[wordPos] {
			if fillMinWordPosArr {
				// Remember the min word position for each pattern position
				t.minWordMatchPos[patternPos] = wordPos
			}
			patternPos++
		}
		wordPos++
	}
	return patternPos == patternLen // pattern must be exhausted
}

// Ported from VS Code: src/vs/base/common/filters.ts (_fillInMaxWordMatchPos)
func fillInMaxWordMatchPos(t *wordTables, patternLen, wordLen, patternStart, wordStart int, patternLow, wordLow []rune) {
	patternPos := patternLen - 1
	wordPos := wordLen - 1
	for patternPos >= patternStart && wordPos >= wordStart {
		if patternLow[patternPos] == wordLow[wordPos] {
			t.maxWordMatchPos[patternPos] = wordPos
			patternPos--
		}
		wordPos--
	}
}

// doScoreWord scores the match of one rune of the pattern with one of the word; false when
// they differ.
// Ported from VS Code: src/vs/base/common/filters.ts (_doScore)
func doScoreWord(
	pattern, patternLow []rune, patternPos, patternStart int,
	word, wordLow []rune, wordPos, wordLen, wordStart int,
	newMatchStart bool,
	outFirstMatchStrong *bool,
) (int, bool) {
	if patternLow[patternPos] != wordLow[wordPos] {
		return 0, false
	}

	score := 1
	isGapLocation := false
	switch {
	case wordPos == (patternPos - patternStart):
		// common prefix: `foobar <-> foobaz`
		//                            ^^^^^
		if pattern[patternPos] == word[wordPos] {
			score = 7
		} else {
			score = 5
		}

	case isUpperCaseAtPos(wordPos, word, wordLow) && (wordPos == 0 || !isUpperCaseAtPos(wordPos-1, word, wordLow)):
		// hitting upper-case: `foo <-> forOthers`
		//                              ^^ ^
		if pattern[patternPos] == word[wordPos] {
			score = 7
		} else {
			score = 5
		}
		isGapLocation = true

	case isSeparatorAtPos(wordLow, wordPos) && (wordPos == 0 || !isSeparatorAtPos(wordLow, wordPos-1)):
		// hitting a separator: `. <-> foo.bar`
		//                                ^
		score = 5

	case isSeparatorAtPos(wordLow, wordPos-1) || isWhitespaceAtPos(wordLow, wordPos-1):
		// post separator: `foo <-> bar_foo`
		//                              ^^^
		score = 5
		isGapLocation = true
	}

	if score > 1 && patternPos == patternStart {
		*outFirstMatchStrong = true
	}

	if !isGapLocation {
		isGapLocation = isUpperCaseAtPos(wordPos, word, wordLow) || isSeparatorAtPos(wordLow, wordPos-1) || isWhitespaceAtPos(wordLow, wordPos-1)
	}

	if patternPos == patternStart { // first character in pattern
		if wordPos > wordStart {
			// the first pattern character would match a word character that is not at the word start
			// so introduce a penalty to account for the gap preceding this match
			if isGapLocation {
				score -= 3
			} else {
				score -= 5
			}
		}
	} else {
		if newMatchStart {
			// this would be the beginning of a new match (i.e. there would be a gap before this location)
			if isGapLocation {
				score += 2
			}
		} else if !isGapLocation {
			// this is part of a contiguous match, so give it a slight bonus, but do so only if it would not be a preferred gap location
			score++
		}
	}

	if wordPos+1 == wordLen {
		// we always penalize gaps, but this gives unfair advantages to a match that would match the last character in the word
		// so pretend there is a gap after the last character in the word to normalize things
		if isGapLocation {
			score -= 3
		} else {
			score -= 5
		}
	}

	return score, true
}
