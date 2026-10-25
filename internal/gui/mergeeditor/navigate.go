package mergeeditor

import "github.com/rm4n0s/emetos/internal/core/diff"

// Ported from VS Code: src/vs/workbench/contrib/mergeEditor/browser/view/viewModel.ts (goToNextModifiedBaseRange and
// goToPreviousModifiedBaseRange, with goToConflict). This file imports no Glutter.

// ConflictToGo picks the conflict that "Go to Next Unhandled Conflict" or "Go to Previous Unhandled Conflict" goes to
// from a cursor on line (counted from 1) of an editor: ranges are the line ranges the unhandled conflicts have in that
// editor, in order. The next one is the first that starts after the line, the previous one the last that ends before
// it (the end is excluded: a range that ends on the line is not before it), and at the end of the list it wraps
// around to the first or the last. It gives the index into ranges, false when there are none.
func ConflictToGo(ranges []diff.LineRange, line int, next bool) (int, bool) {
	if len(ranges) == 0 {
		return 0, false
	}
	if next {
		for i, r := range ranges {
			if r.StartLine > line {
				return i, true
			}
		}
		return 0, true
	}
	for i := len(ranges) - 1; i >= 0; i-- {
		if ranges[i].EndLine < line {
			return i, true
		}
	}
	return len(ranges) - 1, true
}

// ActiveConflict finds the region the cursor is in: the first region whose range in the editor holds the line, or an
// empty range that starts at the line (VS Code's activeModifiedBaseRange). ranges are the regions' ranges in the
// editor, in order; it gives the index into ranges, false when no region is there.
func ActiveConflict(ranges []diff.LineRange, line int) (int, bool) {
	for i, r := range ranges {
		if r.IsEmpty() && r.StartLine == line || !r.IsEmpty() && r.StartLine <= line && line < r.EndLine {
			return i, true
		}
	}
	return 0, false
}

// RevealLines are the lines of an editor to show for a conflict: the first line of its range and the last, clamped to
// the lines the text has (an empty range at the end of the text, or past it, shows the last line). The cursor goes to
// the first one.
func RevealLines(r diff.LineRange, lineCount int) (first, last int) {
	first = min(max(r.StartLine, 1), max(lineCount, 1))
	last = min(max(r.EndLine-1, first), max(lineCount, 1))
	return first, last
}
