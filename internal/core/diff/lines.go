package diff

import (
	"context"
	"math"
	"slices"
	"time"

	"github.com/rm4n0s/errors"
)

// Ported from VS Code: src/vs/editor/common/diff/defaultLinesDiffComputer/defaultLinesDiffComputer.ts
// and src/vs/editor/common/diff/rangeMapping.ts (lineRangeMappingFromRangeMappings)

// dpThreshold is the size (both texts together, in lines) under which the
// lines are aligned with dynamic programming; Myers' algorithm is used from
// there on.
const dpThreshold = 1700

// useDynamicProgramming reports whether texts of n1 and n2 lines are aligned
// with dynamic programming rather than with Myers' algorithm.
func useDynamicProgramming(n1, n2 int) bool { return n1+n2 < dpThreshold }

// ComputeLines computes the lines diff of two texts given as lines (the lines
// of a text without their line breaks; a text that ends with a line break has
// an empty last line). It is VS Code's default lines diff computer up to the
// refinement to character changes (see the package documentation), so each
// change has an empty Inner.
//
// The work stops with a DiffCancelled error when ctx is done. When
// opts.MaxComputationTime is reached the result is the one change that covers
// both texts, with HitTimeout set.
func ComputeLines(ctx context.Context, original, modified []string, opts Options) (LinesDiff, error) {
	return computeDiff(ctx, original, modified, opts, false)
}

// Compute computes the diff of two texts given as lines (see ComputeLines)
// with the character changes of every changed block in Inner: VS Code's
// default lines diff computer, DefaultLinesDiffComputer.computeDiff, without
// the detection of moved code (SPEC section 2). A text always has at least
// one line, so no lines at all count as one empty line.
//
// opts.MaxComputationTime limits the whole work, the line stage and the
// character stage together, with one clock. When it is reached in the line
// stage the line diff is the one change that covers both texts; when it is
// reached in the character stage the blocks that are not refined yet have
// one character change that covers the whole block (as VS Code's algorithms
// return the trivial diff when their time is over). HitTimeout is set in both
// cases. The work stops with a DiffCancelled error when ctx is done.
func Compute(ctx context.Context, original, modified []string, opts Options) (LinesDiff, error) {
	if len(original) == 0 {
		original = []string{""}
	}
	if len(modified) == 0 {
		modified = []string{""}
	}
	return computeDiff(ctx, original, modified, opts, true)
}

// computeDiff is DefaultLinesDiffComputer.computeDiff; refine says whether the
// blocks are refined to character changes.
func computeDiff(ctx context.Context, original, modified []string, opts Options, refine bool) (LinesDiff, error) {
	if err := ctx.Err(); err != nil {
		return LinesDiff{}, errCancelled(err)
	}
	if opts.MaxComputationTime < 0 {
		return LinesDiff{}, errors.New("DiffTimeoutInvalid", "the time limit of a diff must not be negative",
			"maxComputationTime", opts.MaxComputationTime.String())
	}

	if len(original) <= 1 && slices.Equal(original, modified) {
		return LinesDiff{}, nil
	}

	if (len(original) == 1 && original[0] == "") || (len(modified) == 1 && modified[0] == "") {
		return LinesDiff{Changes: []LineRangeMapping{{
			Original: LineRange{1, len(original) + 1},
			Modified: LineRange{1, len(modified) + 1},
			Inner: []RangeMapping{{
				Original: Range{Position{1, 1}, endOfText(original)},
				Modified: Range{Position{1, 1}, endOfText(modified)},
			}},
		}}}, nil
	}

	var tm timeout = infiniteTimeout{}
	if opts.MaxComputationTime > 0 {
		now := opts.Now
		if now == nil {
			now = time.Now
		}
		tm = newDateTimeout(opts.MaxComputationTime, now)
	}
	b := &budget{ctx: ctx, timeout: tm}

	perfectHashes := map[string]int{}
	hash := func(text string) int {
		h, ok := perfectHashes[text]
		if !ok {
			h = len(perfectHashes)
			perfectHashes[text] = h
		}
		return h
	}
	hashes := func(lines []string) []int {
		out := make([]int, len(lines))
		for i, l := range lines {
			out[i] = hash(jsTrim(l))
		}
		return out
	}
	seq1 := &lineSequence{trimmedHash: hashes(original), lines: original}
	seq2 := &lineSequence{trimmedHash: hashes(modified), lines: modified}

	var (
		alignments []sequenceDiff
		hitTimeout bool
		err        error
	)
	if useDynamicProgramming(seq1.length(), seq2.length()) {
		// Use the improved algorithm for small texts.
		alignments, hitTimeout, err = dynamicProgramming(b, seq1, seq2, func(o1, o2 int) float64 {
			switch {
			case original[o1] != modified[o2]:
				return 0.99
			case modified[o2] == "":
				return 0.1
			default:
				return 1 + math.Log(1+float64(utf16Len(modified[o2])))
			}
		})
	} else {
		alignments, hitTimeout, err = myers(b, seq1, seq2)
	}
	if err != nil {
		return LinesDiff{}, err
	}

	alignments = optimizeSequenceDiffs(seq1, seq2, alignments)
	alignments = removeVeryShortMatchingLinesBetweenDiffs(seq1, alignments)

	if !refine {
		changes, err := buildChanges(original, modified, alignments, opts.IgnoreTrimWhitespace)
		if err != nil {
			return LinesDiff{}, err
		}
		return LinesDiff{Changes: changes, HitTimeout: hitTimeout}, nil
	}

	changes, refineTimeout, err := buildRefinedChanges(b, original, modified, alignments, opts)
	if err != nil {
		return LinesDiff{}, err
	}
	return LinesDiff{Changes: changes, HitTimeout: hitTimeout || refineTimeout}, nil
}

// endOfText returns the position after the last character of the lines (the
// start of the text when there are none).
func endOfText(lines []string) Position {
	if len(lines) == 0 {
		return Position{1, 1}
	}
	return Position{len(lines), len(lines[len(lines)-1]) + 1}
}

// buildChanges turns the aligned line blocks into line range mappings. Lines
// between the blocks that are equal after trimming but not equal are one-line
// changes of their own (VS Code refines them to character changes), unless
// ignoreTrimWhitespace is set. Changes that touch are joined.
func buildChanges(original, modified []string, alignments []sequenceDiff, ignoreTrimWhitespace bool) ([]LineRangeMapping, error) {
	var parts []linePair
	last1, last2 := 0, 0

	scanForWhitespaceChanges := func(equalLines int) {
		if ignoreTrimWhitespace {
			return
		}
		for i := range equalLines {
			o1, o2 := last1+i, last2+i
			if original[o1] != modified[o2] {
				// A white space change: these lines differ.
				parts = append(parts, linePair{LineRange{o1 + 1, o1 + 2}, LineRange{o2 + 1, o2 + 2}})
			}
		}
	}

	for _, d := range alignments {
		if d.seq1.start-last1 != d.seq2.start-last2 {
			return nil, errors.New("DiffAlignmentBroken", "the unchanged lines before a change differ in number",
				"original", d.seq1.start-last1, "modified", d.seq2.start-last2)
		}
		scanForWhitespaceChanges(d.seq1.start - last1)

		last1, last2 = d.seq1.end, d.seq2.end

		// ComputeLines leaves the block as it is; Compute refines it (buildRefinedChanges).
		parts = append(parts, linePair{LineRange{d.seq1.start + 1, d.seq1.end + 1}, LineRange{d.seq2.start + 1, d.seq2.end + 1}})
	}
	scanForWhitespaceChanges(len(original) - last1)

	var changes []LineRangeMapping
	for _, p := range parts {
		if n := len(changes); n > 0 &&
			(changes[n-1].Original.intersectsOrTouches(p.original) || changes[n-1].Modified.intersectsOrTouches(p.modified)) {
			changes[n-1].Original = changes[n-1].Original.join(p.original)
			changes[n-1].Modified = changes[n-1].Modified.join(p.modified)
			continue
		}
		changes = append(changes, LineRangeMapping{Original: p.original, Modified: p.modified})
	}
	return changes, nil
}

type linePair struct {
	original, modified LineRange
}
