package diffeditor

import "github.com/rm4n0s/emetos/internal/core/diff"

// Ported from VS Code: src/vs/editor/browser/widget/diffEditor/diffEditorWidget.ts (goToDiff)

// ChangeToGo picks the change that Go to Next Change or Go to Previous Change moves to, as the index in changes,
// from the line (counted from 1) of the cursor of the modified editor. Next is the first change that starts
// below the cursor's line, or the first change when there is none or the cursor is on the last line of the text;
// previous is the last change that starts above the cursor's line, or the last change when there is none. It
// reports false when there are no changes.
func ChangeToGo(changes []diff.LineRangeMapping, cursorLine, lineCount int, next bool) (int, bool) {
	if len(changes) == 0 {
		return 0, false
	}
	if next {
		if lineCount == cursorLine {
			return 0, true
		}
		for i, c := range changes {
			if c.Modified.StartLine > cursorLine {
				return i, true
			}
		}
		return 0, true
	}
	for i := len(changes) - 1; i >= 0; i-- {
		if changes[i].Modified.StartLine < cursorLine {
			return i, true
		}
	}
	return len(changes) - 1, true
}
