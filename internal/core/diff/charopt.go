package diff

// Ported from VS Code: src/vs/editor/common/diff/defaultLinesDiffComputer/heuristicSequenceOptimizations.ts
// (the heuristics of the character stage) and algorithms/diffAlgorithm.ts (SequenceDiff.invert, intersect)

import "math"

// intersect returns the overlap of two ranges; ok is false when they do not
// even touch. The overlap of ranges that only touch is empty.
func (r offsetRange) intersect(o offsetRange) (offsetRange, bool) {
	start := max(r.start, o.start)
	end := min(r.end, o.end)
	if start <= end {
		return offsetRange{start, end}, true
	}
	return offsetRange{}, false
}

// intersects reports whether the ranges share an offset (never true when one
// is empty).
func (r offsetRange) intersects(o offsetRange) bool {
	return max(r.start, o.start) < min(r.end, o.end)
}

func (d sequenceDiff) intersect(o sequenceDiff) (sequenceDiff, bool) {
	i1, ok1 := d.seq1.intersect(o.seq1)
	i2, ok2 := d.seq2.intersect(o.seq2)
	if !ok1 || !ok2 {
		return sequenceDiff{}, false
	}
	return sequenceDiff{i1, i2}, true
}

func (d sequenceDiff) deltaStart(n int) sequenceDiff {
	if n == 0 {
		return d
	}
	return sequenceDiff{offsetRange{d.seq1.start + n, d.seq1.end}, offsetRange{d.seq2.start + n, d.seq2.end}}
}

func (d sequenceDiff) deltaEnd(n int) sequenceDiff {
	if n == 0 {
		return d
	}
	return sequenceDiff{offsetRange{d.seq1.start, d.seq1.end + n}, offsetRange{d.seq2.start, d.seq2.end + n}}
}

// offsetPair is a pair of offsets, one in each sequence.
type offsetPair struct {
	offset1, offset2 int
}

func (p offsetPair) delta(n int) offsetPair { return offsetPair{p.offset1 + n, p.offset2 + n} }

func (d sequenceDiff) starts() offsetPair { return offsetPair{d.seq1.start, d.seq2.start} }

func (d sequenceDiff) ends() offsetPair { return offsetPair{d.seq1.end, d.seq2.end} }

// diffFromPairs makes the change between two pairs of offsets.
func diffFromPairs(start, end offsetPair) sequenceDiff {
	return sequenceDiff{offsetRange{start.offset1, end.offset1}, offsetRange{start.offset2, end.offset2}}
}

// maxPair is larger than any offset.
var maxPair = offsetPair{math.MaxInt, math.MaxInt}

// invertDiffs returns the parts of the sequences between the changes, which
// are equal: from the start to the first change, between the changes and from
// the last change to the end of the first sequence of length n1.
func invertDiffs(diffs []sequenceDiff, n1 int) []sequenceDiff {
	result := make([]sequenceDiff, 0, len(diffs)+1)
	for i := 0; i <= len(diffs); i++ {
		start := offsetPair{}
		if i > 0 {
			start = diffs[i-1].ends()
		}
		var end offsetPair
		if i < len(diffs) {
			end = diffs[i].starts()
		} else {
			shift := 0
			if i > 0 {
				shift = diffs[i-1].seq2.end - diffs[i-1].seq1.end
			}
			end = offsetPair{n1, shift + n1}
		}
		result = append(result, diffFromPairs(start, end))
	}
	return result
}

// removeShortMatches joins changes that have at most two elements between
// them.
func removeShortMatches(diffs []sequenceDiff) []sequenceDiff {
	var result []sequenceDiff
	for _, s := range diffs {
		if len(result) == 0 {
			result = append(result, s)
			continue
		}
		last := result[len(result)-1]
		if s.seq1.start-last.seq1.end <= 2 || s.seq2.start-last.seq2.end <= 2 {
			result[len(result)-1] = last.join(s)
		} else {
			result = append(result, s)
		}
	}
	return result
}

// extendDiffsToEntireWordIfAppropriate extends the changes to the whole word
// (the one findParent returns for an offset) around them when the word is
// mostly changed: the equal parts of the word are less than two thirds of it
// (any part with force). It is VS Code's function of that name.
func extendDiffsToEntireWordIfAppropriate(seq1, seq2 *charSequence, diffs []sequenceDiff,
	findParent func(s *charSequence, offset int) (offsetRange, bool), force bool) []sequenceDiff {
	equalMappings := invertDiffs(diffs, seq1.length())

	var additional []sequenceDiff
	lastPoint := offsetPair{}

	scanWord := func(pair offsetPair, equalMapping sequenceDiff) {
		if pair.offset1 < lastPoint.offset1 || pair.offset2 < lastPoint.offset2 {
			return
		}

		w1, ok1 := findParent(seq1, pair.offset1)
		w2, ok2 := findParent(seq2, pair.offset2)
		if !ok1 || !ok2 {
			return
		}
		w := sequenceDiff{w1, w2}
		equalPart, _ := w.intersect(equalMapping)

		equalChars1 := equalPart.seq1.length()
		equalChars2 := equalPart.seq2.length()

		// The words do not touch previous equals mappings, as we would have
		// processed them already. But they might touch the next ones.
		for len(equalMappings) > 0 {
			next := equalMappings[0]
			if !next.seq1.intersects(w.seq1) && !next.seq2.intersects(w.seq2) {
				break
			}

			v1, _ := findParent(seq1, next.seq1.start)
			v2, _ := findParent(seq2, next.seq2.start)
			// Because there is an intersection, the words are not empty.
			v := sequenceDiff{v1, v2}
			equalPart, _ := v.intersect(next)

			equalChars1 += equalPart.seq1.length()
			equalChars2 += equalPart.seq2.length()

			w = w.join(v)

			if w.seq1.end >= next.seq1.end {
				// The word extends beyond the next equal mapping.
				equalMappings = equalMappings[1:]
			} else {
				break
			}
		}

		wordLen := w.seq1.length() + w.seq2.length()
		if (force && equalChars1+equalChars2 < wordLen) || float64(equalChars1+equalChars2) < float64(wordLen)*2/3 {
			additional = append(additional, w)
		}

		lastPoint = w.ends()
	}

	for len(equalMappings) > 0 {
		next := equalMappings[0]
		equalMappings = equalMappings[1:]
		if next.seq1.isEmpty() {
			continue
		}
		scanWord(next.starts(), next)
		// The equal parts are not empty, so -1 gives a character that is
		// equal in both parts.
		scanWord(next.ends().delta(-1), next)
	}

	return mergeSequenceDiffs(diffs, additional)
}

// mergeSequenceDiffs merges two sorted lists of changes, joining the ones that
// overlap or touch.
func mergeSequenceDiffs(diffs1, diffs2 []sequenceDiff) []sequenceDiff {
	var result []sequenceDiff
	for len(diffs1) > 0 || len(diffs2) > 0 {
		var next sequenceDiff
		if len(diffs1) > 0 && (len(diffs2) == 0 || diffs1[0].seq1.start < diffs2[0].seq1.start) {
			next, diffs1 = diffs1[0], diffs1[1:]
		} else {
			next, diffs2 = diffs2[0], diffs2[1:]
		}

		if n := len(result); n > 0 && result[n-1].seq1.end >= next.seq1.start {
			result[n-1] = result[n-1].join(next)
		} else {
			result = append(result, next)
		}
	}
	return result
}

// removeVeryShortMatchingTextBetweenLongDiffs joins two changes that have a
// little text between them when the changes are large, and extends a large
// change over a line prefix or suffix of at most three characters.
func removeVeryShortMatchingTextBetweenLongDiffs(seq1, seq2 *charSequence, diffs []sequenceDiff) []sequenceDiff {
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

			if shouldJoinDiffs(seq1, seq2, last, cur) {
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

	var newDiffs []sequenceDiff

	// Remove short suffixes and prefixes.
	for i, cur := range diffs {
		newDiff := cur

		shouldMarkAsChanged := func(elems []int32) bool {
			return len(elems) > 0 && trimmedLen(elems) <= 3 && cur.seq1.length()+cur.seq2.length() > 100
		}

		fullRange1 := seq1.extendToFullLines(cur.seq1)
		prefix := seq1.elements[fullRange1.start:cur.seq1.start]
		if shouldMarkAsChanged(prefix) {
			newDiff = newDiff.deltaStart(-len(prefix))
		}
		suffix := seq1.elements[cur.seq1.end:fullRange1.end]
		if shouldMarkAsChanged(suffix) {
			newDiff = newDiff.deltaEnd(len(suffix))
		}

		before := offsetPair{}
		if i > 0 {
			before = diffs[i-1].ends()
		}
		after := maxPair
		if i+1 < len(diffs) {
			after = diffs[i+1].starts()
		}
		result, _ := newDiff.intersect(diffFromPairs(before, after))
		if n := len(newDiffs); n > 0 && result.starts() == newDiffs[n-1].ends() {
			newDiffs[n-1] = newDiffs[n-1].join(result)
		} else {
			newDiffs = append(newDiffs, result)
		}
	}

	return newDiffs
}

// shouldJoinDiffs reports whether two changes that have a short unchanged text
// between them are joined (VS Code's shouldJoinDiffs of
// removeVeryShortMatchingTextBetweenLongDiffs).
func shouldJoinDiffs(seq1, seq2 *charSequence, before, after sequenceDiff) bool {
	unchangedRange := offsetRange{before.seq1.end, after.seq1.start}

	unchangedLineCount := seq1.countLinesIn(unchangedRange)
	if unchangedLineCount > 5 || unchangedRange.length() > 500 {
		return false
	}

	unchanged := seq1.elements[unchangedRange.start:unchangedRange.end]
	if trimmedLen(unchanged) > 20 || hasLineBreakInside(unchanged) {
		return false
	}

	beforeLineCount1 := seq1.countLinesIn(before.seq1)
	beforeSeq1Length := before.seq1.length()
	beforeLineCount2 := seq2.countLinesIn(before.seq2)
	beforeSeq2Length := before.seq2.length()

	afterLineCount1 := seq1.countLinesIn(after.seq1)
	afterSeq1Length := after.seq1.length()
	afterLineCount2 := seq2.countLinesIn(after.seq2)
	afterSeq2Length := after.seq2.length()

	const maxLen = 2*40 + 50
	capLen := func(v int) float64 { return float64(min(v, maxLen)) }

	// math.Pow(maxLen, 1.5) ** 1.5
	limit := math.Pow(math.Pow(maxLen, 1.5), 1.5) * 1.3
	sum := math.Pow(math.Pow(capLen(beforeLineCount1*40+beforeSeq1Length), 1.5)+math.Pow(capLen(beforeLineCount2*40+beforeSeq2Length), 1.5), 1.5) +
		math.Pow(math.Pow(capLen(afterLineCount1*40+afterSeq1Length), 1.5)+math.Pow(capLen(afterLineCount2*40+afterSeq2Length), 1.5), 1.5)
	return sum > limit
}

// trimmedLen returns the number of elements without the white space (as
// JavaScript's trim) at both ends.
func trimmedLen(elems []int32) int {
	start, end := 0, len(elems)
	for start < end && isSpaceElement(elems[start]) {
		start++
	}
	for end > start && isSpaceElement(elems[end-1]) {
		end--
	}
	return end - start
}

// hasLineBreakInside reports whether there is a line break between the first
// and the last element that is not white space.
func hasLineBreakInside(elems []int32) bool {
	start, end := 0, len(elems)
	for start < end && isSpaceElement(elems[start]) {
		start++
	}
	for end > start && isSpaceElement(elems[end-1]) {
		end--
	}
	for _, e := range elems[start:end] {
		if e == '\n' || e == '\r' {
			return true
		}
	}
	return false
}

func isSpaceElement(e int32) bool { return e < invalidByteBase && isJSSpace(rune(e)) }
