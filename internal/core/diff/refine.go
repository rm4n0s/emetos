package diff

import "github.com/rm4n0s/errors"

// Ported from VS Code: src/vs/editor/common/diff/defaultLinesDiffComputer/defaultLinesDiffComputer.ts (refineDiff)

// charDPThreshold is the size (both slices together, in characters) under
// which a block is refined with dynamic programming; Myers' algorithm is used
// from there on.
const charDPThreshold = 500

// buildRefinedChanges turns the aligned line blocks into line range mappings
// with their character changes: each block is refined (refineDiff), and the
// lines between the blocks that are equal after trimming but not equal are
// refined as blocks of their own, unless ignoreTrimWhitespace is set. The
// result tells whether the time limit was reached while refining.
func buildRefinedChanges(b *budget, original, modified []string, alignments []sequenceDiff, opts Options) ([]LineRangeMapping, bool, error) {
	considerWhitespaceChanges := !opts.IgnoreTrimWhitespace
	var (
		mappings   []RangeMapping
		hitTimeout bool
		last1      int
		last2      int
	)

	refine := func(d sequenceDiff) error {
		m, hit, err := refineDiff(b, original, modified, d, considerWhitespaceChanges, opts.ExtendToSubwords)
		if err != nil {
			return err
		}
		hitTimeout = hitTimeout || hit
		mappings = append(mappings, m...)
		return nil
	}

	scanForWhitespaceChanges := func(equalLines int) error {
		if !considerWhitespaceChanges {
			return nil
		}
		for i := range equalLines {
			o1, o2 := last1+i, last2+i
			if original[o1] != modified[o2] {
				// This is because of white space changes: diff these lines.
				if err := refine(sequenceDiff{offsetRange{o1, o1 + 1}, offsetRange{o2, o2 + 1}}); err != nil {
					return err
				}
			}
		}
		return nil
	}

	for _, d := range alignments {
		if d.seq1.start-last1 != d.seq2.start-last2 {
			return nil, false, errors.New("DiffAlignmentBroken", "the unchanged lines before a change differ in number",
				"original", d.seq1.start-last1, "modified", d.seq2.start-last2)
		}
		if err := scanForWhitespaceChanges(d.seq1.start - last1); err != nil {
			return nil, false, err
		}

		last1, last2 = d.seq1.end, d.seq2.end

		if err := refine(d); err != nil {
			return nil, false, err
		}
	}
	if err := scanForWhitespaceChanges(len(original) - last1); err != nil {
		return nil, false, err
	}

	return lineRangeMappingFromRangeMappings(mappings, original, modified), hitTimeout, nil
}

// refineDiff finds the character changes inside a block of changed lines
// (VS Code's refineDiff): the characters of both blocks are aligned with
// dynamic programming or Myers' algorithm, and the changes are improved by
// shifting them to word and line boundaries, extending them to whole words
// and joining the ones that have little between them. The result tells
// whether the time limit was reached (the changes are then those of the whole
// block).
func refineDiff(b *budget, original, modified []string, diff sequenceDiff, considerWhitespaceChanges, extendToSubwords bool) ([]RangeMapping, bool, error) {
	if err := b.ctx.Err(); err != nil {
		return nil, false, errors.NewErr("DiffCancelled", err)
	}

	origBlock := LineRange{diff.seq1.start + 1, diff.seq1.end + 1}
	modBlock := LineRange{diff.seq2.start + 1, diff.seq2.end + 1}
	rangeMapping, err := toRangeMapping2(original, modified, origBlock, modBlock)
	if err != nil {
		return nil, false, err
	}

	slice1 := newCharSequence(original, rangeMapping.Original, considerWhitespaceChanges)
	slice2 := newCharSequence(modified, rangeMapping.Modified, considerWhitespaceChanges)

	var (
		diffs      []sequenceDiff
		hitTimeout bool
	)
	if slice1.length()+slice2.length() < charDPThreshold {
		diffs, hitTimeout, err = dynamicProgramming(b, slice1, slice2, nil)
	} else {
		diffs, hitTimeout, err = myers(b, slice1, slice2)
	}
	if err != nil {
		return nil, false, err
	}

	diffs = optimizeSequenceDiffs(slice1, slice2, diffs)
	diffs = extendDiffsToEntireWordIfAppropriate(slice1, slice2, diffs,
		func(s *charSequence, offset int) (offsetRange, bool) { return s.findWordContaining(offset) }, false)
	if extendToSubwords {
		diffs = extendDiffsToEntireWordIfAppropriate(slice1, slice2, diffs,
			func(s *charSequence, offset int) (offsetRange, bool) { return s.findSubWordContaining(offset) }, true)
	}
	diffs = removeShortMatches(diffs)
	diffs = removeVeryShortMatchingTextBetweenLongDiffs(slice1, slice2, diffs)

	result := make([]RangeMapping, len(diffs))
	for i, d := range diffs {
		result[i] = RangeMapping{Original: slice1.translateRange(d.seq1), Modified: slice2.translateRange(d.seq2)}
	}
	return result, hitTimeout, nil
}
