package diff

import (
	"context"
	"time"
)

// Ported from VS Code: src/vs/editor/common/diff/rangeMapping.ts and linesDiffComputer.ts

// LineRange is a range of lines: StartLine is the first line (1-based) and
// EndLine is the line after the last one, so a range is empty when both are
// equal. It is VS Code's LineRange.
type LineRange struct {
	StartLine int
	EndLine   int
}

// Len returns the number of lines in the range.
func (r LineRange) Len() int { return r.EndLine - r.StartLine }

// IsEmpty reports whether the range has no lines.
func (r LineRange) IsEmpty() bool { return r.StartLine == r.EndLine }

// intersectsOrTouches reports whether the ranges share a line or are adjacent.
func (r LineRange) intersectsOrTouches(o LineRange) bool {
	return max(r.StartLine, o.StartLine) <= min(r.EndLine, o.EndLine)
}

// join returns the smallest range that contains both ranges.
func (r LineRange) join(o LineRange) LineRange {
	return LineRange{StartLine: min(r.StartLine, o.StartLine), EndLine: max(r.EndLine, o.EndLine)}
}

// Position is a position in a text: Line and Column are 1-based, and Column
// counts bytes of the line, so the end of a line is len(line)+1.
type Position struct {
	Line   int
	Column int
}

// Range is a range of text between two positions; End is exclusive.
type Range struct {
	Start Position
	End   Position
}

// RangeMapping maps a range of the original text to the range of the modified
// text that replaces it (a character change).
type RangeMapping struct {
	Original Range
	Modified Range
}

// LineRangeMapping maps a block of lines of the original text to the block of
// lines of the modified text that replaces it. It is VS Code's
// DetailedLineRangeMapping.
type LineRangeMapping struct {
	// Original is the block in the original text.
	Original LineRange
	// Modified is the block in the modified text.
	Modified LineRange
	// Inner holds the character changes inside the block. ComputeLines leaves
	// it empty; the character diff of step 9.2 fills it.
	Inner []RangeMapping
}

// LinesDiff is the result of a lines diff. Changes are sorted, do not touch
// each other, and have the same number of unchanged lines between them in both
// texts (see Validate).
type LinesDiff struct {
	// Changes lists the changed blocks.
	Changes []LineRangeMapping
	// HitTimeout reports that the time limit was reached: the changes are then
	// an approximation (one change that covers both texts).
	HitTimeout bool
}

// Options are the options of a diff.
type Options struct {
	// IgnoreTrimWhitespace makes lines that differ only in the white space at
	// their start and end count as equal (VS Code's
	// diffEditor.ignoreTrimWhitespace).
	IgnoreTrimWhitespace bool
	// MaxComputationTime is the time limit of the diff; zero means no limit
	// (VS Code's diffEditor.maxComputationTime); it must not be negative.
	MaxComputationTime time.Duration
	// Now is the clock the time limit is measured with; nil means time.Now.
	Now func() time.Time
}

// ctxCheckInterval is the number of steps of an algorithm between two looks at
// the context.
const ctxCheckInterval = 1024

// budget bounds the work of a diff: the time limit, and the context.
type budget struct {
	ctx     context.Context
	timeout timeout
	steps   int
}

// expired reports whether the time limit is reached. It returns an error once
// the context is done; the context is only looked at every ctxCheckInterval
// calls.
func (b *budget) expired() (bool, error) {
	b.steps++
	if b.steps%ctxCheckInterval == 0 {
		if err := b.ctx.Err(); err != nil {
			return false, errCancelled(err)
		}
	}
	return !b.timeout.isValid(), nil
}
