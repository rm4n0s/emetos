// Package diff computes line and character diffs and holds the three-way merge model (SPEC §9.11).
//
// # Line diff (step 9.1)
//
// ComputeLines is VS Code's default ("advanced") lines diff computer
// (DefaultLinesDiffComputer.computeDiff) up to, and excluding, the refinement
// of the changed line blocks to character changes: the lines are compared by
// their trimmed text, aligned with dynamic programming (fewer than 1700 lines
// in both texts together) or Myers' O(ND) algorithm, and improved by VS
// Code's heuristics (optimizeSequenceDiffs: joining changes that a shift makes
// meet and shifting insertions and deletions to the boundary with the least
// indentation, and removeVeryShortMatchingLinesBetweenDiffs). Lines that are equal after
// trimming but not equal are reported as one-line changes unless
// Options.IgnoreTrimWhitespace is set. A time limit (Options.MaxComputationTime,
// read through an injectable clock) turns the result into one change that
// covers both texts, with LinesDiff.HitTimeout set; a cancelled context stops
// the work with a DiffCancelled error. Moved-code detection is not built
// (SPEC §2).
//
// # Character diff (step 9.2)
//
// Compute runs the line stage and then refines every changed block of lines
// to character changes, as VS Code's refineDiff does: the characters of the
// block (LinesSliceCharSequence, with the white space at the start and end of
// the lines left out under Options.IgnoreTrimWhitespace) are aligned with
// dynamic programming (fewer than 500 characters in both blocks together) or
// Myers' algorithm; the changes are improved by optimizeSequenceDiffs with
// the boundary scores of characters, extended to whole words
// (extendDiffsToEntireWordIfAppropriate; to sub-words with
// Options.ExtendToSubwords), joined when they have at most two characters
// between them (removeShortMatches) and when large changes have a short text
// between them (removeVeryShortMatchingTextBetweenLongDiffs); the lines
// between the blocks that differ only in white space are refined as blocks of
// their own. The character changes are then grouped into line blocks
// (lineRangeMappingFromRangeMappings), which are the LineRangeMapping values
// with their Inner.
//
// Columns are byte columns of the line (SPEC D6), where VS Code counts UTF-16
// code units. The elements of the character sequences are code points (a
// byte that is not valid UTF-8 is an element of its own), so a change never
// splits a character; the limits that VS Code counts in code units (500
// characters for dynamic programming, 20 for the text between changes, 100
// for a large change) count code points here.
//
// The time limit covers both stages with one clock; see Compute. ComputeLines
// is the line stage alone, and leaves Inner empty.
//
// Apply rebuilds the modified lines from the original lines and the changes,
// and Validate checks that a list of changes is a consistent diff of two texts
// of the given sizes.
//
// Ported from VS Code: src/vs/editor/common/diff/defaultLinesDiffComputer/defaultLinesDiffComputer.ts
// Ported from VS Code: src/vs/editor/common/diff/defaultLinesDiffComputer/algorithms/diffAlgorithm.ts
// Ported from VS Code: src/vs/editor/common/diff/defaultLinesDiffComputer/algorithms/dynamicProgrammingDiffing.ts
// Ported from VS Code: src/vs/editor/common/diff/defaultLinesDiffComputer/algorithms/myersDiffAlgorithm.ts
// Ported from VS Code: src/vs/editor/common/diff/defaultLinesDiffComputer/heuristicSequenceOptimizations.ts
// Ported from VS Code: src/vs/editor/common/diff/defaultLinesDiffComputer/linesSliceCharSequence.ts
// Ported from VS Code: src/vs/editor/common/diff/defaultLinesDiffComputer/lineSequence.ts
// Ported from VS Code: src/vs/editor/common/diff/rangeMapping.ts
// Ported from VS Code: src/vs/editor/common/diff/linesDiffComputer.ts
package diff
