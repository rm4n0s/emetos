package mergeeditor

// Ported from VS Code: src/vs/workbench/contrib/mergeEditor/browser/view/editorGutter.ts and
// src/vs/workbench/contrib/mergeEditor/browser/view/editors/inputCodeEditorView.ts (MergeConflictGutterItemView.layout).
// This file imports no Glutter.

// CheckboxSize is the width and height of the checkbox of a conflict in the gutter of an input.
const CheckboxSize = 18.0

// MultiLineHeight is the height from which a conflict is drawn as a bracket along its lines, not as a checkbox
// alone (VS Code's `height > 30`).
const MultiLineHeight = 30.0

// CheckboxTop is where the checkbox of a conflict is drawn, as a distance from the top of the gutter: in the middle
// of the conflict's lines, kept inside the part of the gutter that shows and inside the conflict when it is tall
// enough, so that the checkbox stays in view while a tall conflict scrolls (VS Code's layout of an item). top and
// height are the extent of the conflict's lines in the gutter's coordinates, viewTop and viewHeight those of the
// visible part.
func CheckboxTop(top, height, viewTop, viewHeight float64) float64 {
	size := CheckboxSize
	y := top + height/2 - size/2
	viewLow, viewHigh := size, viewTop+viewHeight-size-size
	parentLow, parentHigh := top+size, top+height-size-size
	if parentLow < parentHigh {
		y = min(max(y, viewLow), viewHigh)
		y = min(max(y, parentLow), parentHigh)
	}
	return y
}

// ChecksOf describes what a checkbox shows for an input: the input is not part of the result, is the only one, or
// is the second of two (VS Code's InputState).
type ChecksOf int

// The states a checkbox shows.
const (
	// CheckEmpty: the result does not have the lines of the input.
	CheckEmpty ChecksOf = iota
	// CheckFirst: the result has the lines of the input (a check mark).
	CheckFirst
	// CheckSecond: the result has the lines of the input after those of the other (two check marks).
	CheckSecond
	// CheckUnrecognized: the result of the conflict was edited by hand and is none of the states (a dot).
	CheckUnrecognized
)
