package quickdiff

import (
	"cmp"
	"context"
	"time"

	"github.com/rm4n0s/emetos/internal/core/diff"
)

// ChangeType is the kind of a change (VS Code's ChangeType).
type ChangeType int

const (
	// Modify is a change that replaces lines with lines.
	Modify ChangeType = iota
	// Add is a change that only adds lines.
	Add
	// Delete is a change that only removes lines.
	Delete
)

// String implements fmt.Stringer.
func (t ChangeType) String() string {
	switch t {
	case Add:
		return "added"
	case Delete:
		return "deleted"
	}
	return "modified"
}

// Change is a changed block of lines (VS Code's IChange): lines are counted from 1; an End of 0 says that the side has no lines in the
// block (an added block has OriginalEnd 0 and OriginalStart the line before it, a deleted block has ModifiedEnd 0 and ModifiedStart the
// line the lines were deleted after, 0 for a deletion at the start of the text).
type Change struct {
	OriginalStart, OriginalEnd int
	ModifiedStart, ModifiedEnd int
}

// Type is the kind of the change (VS Code's getChangeType).
func (c Change) Type() ChangeType {
	switch {
	case c.OriginalEnd == 0:
		return Add
	case c.ModifiedEnd == 0:
		return Delete
	}
	return Modify
}

// ModifiedEndLine is the last line of the block in the modified text for navigation and for the marks: a deleted block counts as the
// line it follows, the first line at the start of the text (VS Code's getModifiedEndLineNumber).
func (c Change) ModifiedEndLine() int {
	if c.ModifiedEnd == 0 {
		if c.ModifiedStart == 0 {
			return 1
		}
		return c.ModifiedStart
	}
	return c.ModifiedEnd
}

// Intersects tells whether the line of the modified text is in the block (VS Code's lineIntersectsChange): a deletion at the start of the
// text is at line 1.
func (c Change) Intersects(line int) bool {
	if line == 1 && c.ModifiedStart == 0 && c.ModifiedEnd == 0 {
		return true
	}
	end := c.ModifiedEnd
	if end == 0 {
		end = c.ModifiedStart
	}
	return line >= c.ModifiedStart && line <= end
}

// Compare orders changes by their place in the modified text, then in the original (VS Code's compareChanges).
func Compare(a, b Change) int {
	return cmp.Or(
		cmp.Compare(a.ModifiedStart, b.ModifiedStart),
		cmp.Compare(a.ModifiedEnd, b.ModifiedEnd),
		cmp.Compare(a.OriginalStart, b.OriginalStart),
		cmp.Compare(a.OriginalEnd, b.OriginalEnd),
	)
}

// Height is the number of lines a change shows (VS Code's getChangeHeight): the lines of the side that has them, both for a modification.
func (c Change) Height() int {
	modified := c.ModifiedEnd - c.ModifiedStart + 1
	original := c.OriginalEnd - c.OriginalStart + 1
	switch {
	case c.OriginalEnd == 0:
		return modified
	case c.ModifiedEnd == 0:
		return original
	}
	return modified + original
}

// FromMappings turns the blocks of a lines diff into changes (VS Code's toLineChanges): an empty block of a side counts as 0 lines and
// starts at the line before.
//
// Ported from VS Code: src/vs/editor/browser/widget/diffEditor/diffEditorWidget.ts (toLineChanges)
func FromMappings(maps []diff.LineRangeMapping) []Change {
	out := make([]Change, len(maps))
	for i, m := range maps {
		var c Change
		if m.Original.IsEmpty() {
			c.OriginalStart, c.OriginalEnd = m.Original.StartLine-1, 0
		} else {
			c.OriginalStart, c.OriginalEnd = m.Original.StartLine, m.Original.EndLine-1
		}
		if m.Modified.IsEmpty() {
			c.ModifiedStart, c.ModifiedEnd = m.Modified.StartLine-1, 0
		} else {
			c.ModifiedStart, c.ModifiedEnd = m.Modified.StartLine, m.Modified.EndLine-1
		}
		out[i] = c
	}
	return out
}

// DiffOptions are the options of Changes.
type DiffOptions struct {
	// IgnoreTrimWhitespace counts lines that differ only in the white space at their ends as equal.
	IgnoreTrimWhitespace bool
	// TimeLimit is the time the diff may take (VS Code's 1000 ms for the quick diff); zero means no limit. When it is over, the result
	// is the one change that covers both texts.
	TimeLimit time.Duration
	// Now is the clock of the time limit; nil is time.Now.
	Now func() time.Time
}

// Changes compares the lines of the original with the lines of the modified text (a text that ends with a line break has an empty last
// line). It fails with the tags of core/diff (DiffCancelled when ctx ends).
func Changes(ctx context.Context, original, modified []string, opts DiffOptions) ([]Change, error) {
	res, err := diff.ComputeLines(ctx, original, modified, diff.Options{
		IgnoreTrimWhitespace: opts.IgnoreTrimWhitespace, MaxComputationTime: opts.TimeLimit, Now: opts.Now,
	})
	if err != nil {
		return nil, err
	}
	return FromMappings(res.Changes), nil
}

// FindNext returns the index of the first change that ends at or after the line when inclusive, or starts after it when not, and 0 when
// there is none (the list wraps round). Lines are counted from 1 and the list is sorted (VS Code's findNextClosestChange).
func FindNext(changes []Change, line int, inclusive bool) int {
	for i, c := range changes {
		if inclusive {
			if c.ModifiedEndLine() >= line {
				return i
			}
		} else if c.ModifiedStart > line {
			return i
		}
	}
	return 0
}

// FindPrevious returns the index of the last change that starts at or before the line when inclusive, or ends before it when not, and
// the last one when there is none (the list wraps round); -1 for an empty list (VS Code's findPreviousClosestChange).
func FindPrevious(changes []Change, line int, inclusive bool) int {
	for i := len(changes) - 1; i >= 0; i-- {
		c := changes[i]
		if inclusive {
			if c.ModifiedStart <= line {
				return i
			}
		} else if c.ModifiedEndLine() < line {
			return i
		}
	}
	return len(changes) - 1
}
