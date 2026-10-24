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
// Each LineRangeMapping has an empty Inner for now (except for the one change
// that VS Code returns directly when a text is one empty line, which has its
// one character change): the character changes of the block are step 9.2
// (refineDiff and its heuristics, and applying the time limit to them).
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
// Ported from VS Code: src/vs/editor/common/diff/defaultLinesDiffComputer/lineSequence.ts
// Ported from VS Code: src/vs/editor/common/diff/rangeMapping.ts
// Ported from VS Code: src/vs/editor/common/diff/linesDiffComputer.ts
package diff
