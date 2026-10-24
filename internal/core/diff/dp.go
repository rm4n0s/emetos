package diff

import "math"

// Ported from VS Code: src/vs/editor/common/diff/defaultLinesDiffComputer/algorithms/dynamicProgrammingDiffing.ts

// dynamicProgramming is VS Code's DynamicProgrammingDiffing: an O(MN) diff
// algorithm that supports a score function for equal elements. equalityScore
// may be nil, which scores every equal pair 1. It returns the changes and
// whether the time limit was reached (the changes are then the trivial diff).
func dynamicProgramming(b *budget, seq1, seq2 sequence, equalityScore func(offset1, offset2 int) float64) ([]sequenceDiff, bool, error) {
	n1, n2 := seq1.length(), seq2.length()
	if n1 == 0 || n2 == 0 {
		return trivialDiff(seq1, seq2), false, nil
	}

	// lcs[s1*n2+s2]: score of the best common subsequence of the first s1+1
	// elements of seq1 and the first s2+1 elements of seq2.
	lcs := make([]float64, n1*n2)
	// directions: 1 means seq1 went one step back, 2 means seq2 did, 3 means
	// the elements are aligned.
	directions := make([]int8, n1*n2)
	// lengths: the number of consecutive aligned elements that end here.
	lengths := make([]int32, n1*n2)

	for s1 := range n1 {
		for s2 := range n2 {
			expired, err := b.expired()
			if err != nil {
				return nil, false, err
			}
			if expired {
				return trivialDiff(seq1, seq2), true, nil
			}

			idx := s1*n2 + s2
			horizontalLen := 0.0
			if s1 != 0 {
				horizontalLen = lcs[(s1-1)*n2+s2]
			}
			verticalLen := 0.0
			if s2 != 0 {
				verticalLen = lcs[idx-1]
			}

			var extendedSeqScore float64
			if seq1.element(s1) == seq2.element(s2) {
				if s1 == 0 || s2 == 0 {
					extendedSeqScore = 0
				} else {
					extendedSeqScore = lcs[(s1-1)*n2+s2-1]
				}
				if s1 > 0 && s2 > 0 && directions[(s1-1)*n2+s2-1] == 3 {
					// Prefer consecutive diagonals.
					extendedSeqScore += float64(lengths[(s1-1)*n2+s2-1])
				}
				if equalityScore != nil {
					extendedSeqScore += equalityScore(s1, s2)
				} else {
					extendedSeqScore++
				}
			} else {
				extendedSeqScore = -1
			}

			newValue := math.Max(math.Max(horizontalLen, verticalLen), extendedSeqScore)

			switch newValue {
			case extendedSeqScore:
				// Prefer diagonals.
				var prevLen int32
				if s1 > 0 && s2 > 0 {
					prevLen = lengths[(s1-1)*n2+s2-1]
				}
				lengths[idx] = prevLen + 1
				directions[idx] = 3
			case horizontalLen:
				lengths[idx] = 0
				directions[idx] = 1
			case verticalLen:
				lengths[idx] = 0
				directions[idx] = 2
			}
			lcs[idx] = newValue
		}
	}

	// Backtracking.
	var result []sequenceDiff
	lastPos1, lastPos2 := n1, n2
	report := func(s1, s2 int) {
		if s1+1 != lastPos1 || s2+1 != lastPos2 {
			result = append(result, sequenceDiff{offsetRange{s1 + 1, lastPos1}, offsetRange{s2 + 1, lastPos2}})
		}
		lastPos1, lastPos2 = s1, s2
	}

	s1, s2 := n1-1, n2-1
	for s1 >= 0 && s2 >= 0 {
		switch directions[s1*n2+s2] {
		case 3:
			report(s1, s2)
			s1--
			s2--
		case 1:
			s1--
		default:
			s2--
		}
	}
	report(-1, -1)
	reverse(result)
	return result, false, nil
}

// reverse reverses s in place.
func reverse[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}
