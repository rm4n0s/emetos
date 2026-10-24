package diff

// Ported from VS Code: src/vs/editor/common/diff/defaultLinesDiffComputer/heuristicSequenceOptimizations.ts

// optimizeSequenceDiffs improves the changes of a diff: it joins changes by
// shifting them over the elements they have in common, and shifts the changes
// that only insert or delete elements to the best boundary of the sequence.
// The slice is changed in place by the last step.
func optimizeSequenceDiffs(seq1, seq2 sequence, diffs []sequenceDiff) []sequenceDiff {
	result := diffs
	result = joinSequenceDiffsByShifting(seq1, seq2, result)
	// Sometimes, calling this function twice improves the result.
	result = joinSequenceDiffsByShifting(seq1, seq2, result)
	result = shiftSequenceDiffs(seq1, seq2, result)
	return result
}

// joinSequenceDiffsByShifting moves the changes that only insert or delete
// elements to the left and joins them with the change before when they meet,
// then moves them to the right and joins them with the change after.
func joinSequenceDiffsByShifting(seq1, seq2 sequence, diffs []sequenceDiff) []sequenceDiff {
	if len(diffs) == 0 {
		return diffs
	}

	result := []sequenceDiff{diffs[0]}

	// First move them all to the left as much as possible and join them if
	// possible.
	for i := 1; i < len(diffs); i++ {
		prev := result[len(result)-1]
		cur := diffs[i]

		if cur.seq1.isEmpty() || cur.seq2.isEmpty() {
			length := cur.seq1.start - prev.seq1.end
			d := 1
			for ; d <= length; d++ {
				if seq1.element(cur.seq1.start-d) != seq1.element(cur.seq1.end-d) ||
					seq2.element(cur.seq2.start-d) != seq2.element(cur.seq2.end-d) {
					break
				}
			}
			d--

			if d == length {
				// Merge the previous and the current change.
				result[len(result)-1] = sequenceDiff{
					offsetRange{prev.seq1.start, cur.seq1.end - length},
					offsetRange{prev.seq2.start, cur.seq2.end - length},
				}
				continue
			}

			cur = cur.delta(-d)
		}

		result = append(result, cur)
	}

	var result2 []sequenceDiff
	// Then move them all to the right and join them again if possible.
	for i := 0; i < len(result)-1; i++ {
		next := result[i+1]
		cur := result[i]

		if cur.seq1.isEmpty() || cur.seq2.isEmpty() {
			length := next.seq1.start - cur.seq1.end
			d := 0
			for ; d < length; d++ {
				if !seq1.stronglyEqual(cur.seq1.start+d, cur.seq1.end+d) ||
					!seq2.stronglyEqual(cur.seq2.start+d, cur.seq2.end+d) {
					break
				}
			}

			if d == length {
				// Merge the current and the next change, write to result.
				result[i+1] = sequenceDiff{
					offsetRange{cur.seq1.start + length, next.seq1.end},
					offsetRange{cur.seq2.start + length, next.seq2.end},
				}
				continue
			}

			if d > 0 {
				cur = cur.delta(d)
			}
		}

		result2 = append(result2, cur)
	}

	if len(result) > 0 {
		result2 = append(result2, result[len(result)-1])
	}

	return result2
}

// shiftSequenceDiffs moves each change that only inserts or deletes elements
// to the position with the best boundary score. It changes diffs in place.
func shiftSequenceDiffs(seq1, seq2 sequence, diffs []sequenceDiff) []sequenceDiff {
	for i := range diffs {
		diff := diffs[i]

		valid1 := offsetRange{0, seq1.length()}
		valid2 := offsetRange{0, seq2.length()}
		if i > 0 {
			valid1.start = diffs[i-1].seq1.end + 1
			valid2.start = diffs[i-1].seq2.end + 1
		}
		if i+1 < len(diffs) {
			valid1.end = diffs[i+1].seq1.start - 1
			valid2.end = diffs[i+1].seq2.start - 1
		}

		if diff.seq1.isEmpty() {
			diffs[i] = shiftDiffToBetterPosition(diff, seq1, seq2, valid1, valid2)
		} else if diff.seq2.isEmpty() {
			diffs[i] = shiftDiffToBetterPosition(diff.swap(), seq2, seq1, valid2, valid1).swap()
		}
	}
	return diffs
}

// maxShiftLimit bounds how far a change is shifted, to prevent performance
// issues.
const maxShiftLimit = 100

// shiftDiffToBetterPosition shifts a change whose range in seq1 is empty (a
// pure insertion into seq2) to the position with the best boundary score.
func shiftDiffToBetterPosition(diff sequenceDiff, seq1, seq2 sequence, valid1, valid2 offsetRange) sequenceDiff {
	// Don't touch the previous or the next change.
	deltaBefore := 1
	for diff.seq1.start-deltaBefore >= valid1.start &&
		diff.seq2.start-deltaBefore >= valid2.start &&
		seq2.stronglyEqual(diff.seq2.start-deltaBefore, diff.seq2.end-deltaBefore) &&
		deltaBefore < maxShiftLimit {
		deltaBefore++
	}
	deltaBefore--

	deltaAfter := 0
	for diff.seq1.start+deltaAfter < valid1.end &&
		diff.seq2.end+deltaAfter < valid2.end &&
		seq2.stronglyEqual(diff.seq2.start+deltaAfter, diff.seq2.end+deltaAfter) &&
		deltaAfter < maxShiftLimit {
		deltaAfter++
	}

	if deltaBefore == 0 && deltaAfter == 0 {
		return diff
	}

	bestDelta := 0
	bestScore := -1
	for delta := -deltaBefore; delta <= deltaAfter; delta++ {
		start2 := diff.seq2.start + delta
		end2 := diff.seq2.end + delta
		offset1 := diff.seq1.start + delta

		score := seq1.boundaryScore(offset1) + seq2.boundaryScore(start2) + seq2.boundaryScore(end2)
		if score > bestScore {
			bestScore = score
			bestDelta = delta
		}
	}

	return diff.delta(bestDelta)
}

// removeVeryShortMatchingLinesBetweenDiffs joins changes that have only a few
// characters of unchanged lines between them, when one of them is not small.
func removeVeryShortMatchingLinesBetweenDiffs(seq1 *lineSequence, diffs []sequenceDiff) []sequenceDiff {
	if len(diffs) == 0 {
		return diffs
	}

	counter := 0
	for {
		shouldRepeat := false

		result := []sequenceDiff{diffs[0]}

		for i := 1; i < len(diffs); i++ {
			cur := diffs[i]
			last := result[len(result)-1]

			unchanged := seq1.lines[last.seq1.end:cur.seq1.start]
			if nonSpaceLen(unchanged, 4) <= 4 &&
				(last.seq1.length()+last.seq2.length() > 5 || cur.seq1.length()+cur.seq2.length() > 5) {
				shouldRepeat = true
				result[len(result)-1] = last.join(cur)
			} else {
				result = append(result, cur)
			}
		}

		diffs = result

		repeat := counter < 10 && shouldRepeat
		counter++
		if !repeat {
			break
		}
	}

	return diffs
}
