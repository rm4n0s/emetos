package command

import "github.com/rm4n0s/emetos/internal/core/contextkey"

// DefaultRegistry makes a registry with the metadata of the commands Emetos has. Each step
// that adds a command adds it here (or to a function it calls) with VS Code's id and title; a
// command VS Code does not list in the palette (the cursor commands) has no title. The
// handlers are attached later by the packages that implement the commands.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	registerCursorCommands(r)
	registerEditCommands(r)
	registerLineCommands(r)
	registerMultiCursorCommands(r)
	return r
}

// writable is VS Code's EditorContextKeys.writable, the precondition of every command that
// changes the text.
func writable() contextkey.Expr { return contextkey.Not("editorReadonly") }

// registerCursorCommands registers the commands that move and select.
//
// Ported from VS Code: src/vs/editor/browser/coreCommands.ts,
// src/vs/editor/contrib/wordOperations/browser/wordOperations.ts,
// src/vs/editor/contrib/wordPartOperations/browser/wordPartOperations.ts
func registerCursorCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: "cursorLeft"},
		Meta{ID: "cursorLeftSelect"},
		Meta{ID: "cursorRight"},
		Meta{ID: "cursorRightSelect"},
		Meta{ID: "cursorUp"},
		Meta{ID: "cursorUpSelect"},
		Meta{ID: "cursorPageUp"},
		Meta{ID: "cursorPageUpSelect"},
		Meta{ID: "cursorDown"},
		Meta{ID: "cursorDownSelect"},
		Meta{ID: "cursorPageDown"},
		Meta{ID: "cursorPageDownSelect"},
		Meta{ID: "cursorHome"},
		Meta{ID: "cursorHomeSelect"},
		Meta{ID: "cursorLineStart"},
		Meta{ID: "cursorLineStartSelect"},
		Meta{ID: "cursorEnd"},
		Meta{ID: "cursorEndSelect"},
		Meta{ID: "cursorLineEnd"},
		Meta{ID: "cursorLineEndSelect"},
		Meta{ID: "cursorTop"},
		Meta{ID: "cursorTopSelect"},
		Meta{ID: "cursorBottom"},
		Meta{ID: "cursorBottomSelect"},
		Meta{ID: "cursorColumnSelectLeft"},
		Meta{ID: "cursorColumnSelectRight"},
		Meta{ID: "cursorColumnSelectUp"},
		Meta{ID: "cursorColumnSelectPageUp"},
		Meta{ID: "cursorColumnSelectDown"},
		Meta{ID: "cursorColumnSelectPageDown"},
		Meta{ID: "cursorWordStartLeft"},
		Meta{ID: "cursorWordEndLeft"},
		Meta{ID: "cursorWordLeft"},
		Meta{ID: "cursorWordStartLeftSelect"},
		Meta{ID: "cursorWordEndLeftSelect"},
		Meta{ID: "cursorWordLeftSelect"},
		Meta{ID: "cursorWordAccessibilityLeft"},
		Meta{ID: "cursorWordAccessibilityLeftSelect"},
		Meta{ID: "cursorWordStartRight"},
		Meta{ID: "cursorWordEndRight"},
		Meta{ID: "cursorWordRight"},
		Meta{ID: "cursorWordStartRightSelect"},
		Meta{ID: "cursorWordEndRightSelect"},
		Meta{ID: "cursorWordRightSelect"},
		Meta{ID: "cursorWordAccessibilityRight"},
		Meta{ID: "cursorWordAccessibilityRightSelect"},
		Meta{ID: "cursorWordPartLeft"},
		Meta{ID: "cursorWordPartLeftSelect"},
		Meta{ID: "cursorWordPartRight"},
		Meta{ID: "cursorWordPartRightSelect"},
		Meta{ID: "cancelSelection", Precondition: contextkey.Has("editorHasSelection")},
		Meta{ID: "removeSecondaryCursors", Precondition: contextkey.Has("editorHasMultipleSelections")},
		Meta{ID: "expandLineSelection", Title: "Expand Line Selection"},
		Meta{ID: "cursorUndo", Title: "Cursor Undo"},
		Meta{ID: "cursorRedo", Title: "Cursor Redo"},
		Meta{ID: "editor.action.selectAll", Title: "Select All"},
	)
}

// registerEditCommands registers the commands that type, delete and undo.
//
// Ported from VS Code: src/vs/editor/browser/coreCommands.ts,
// src/vs/editor/browser/editorExtensions.ts,
// src/vs/editor/contrib/wordOperations/browser/wordOperations.ts
func registerEditCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: "type", Description: "Type text at the cursors."},
		Meta{ID: "undo", Title: "Undo"},
		Meta{ID: "redo", Title: "Redo"},
		Meta{ID: "lineBreakInsert", Precondition: writable()},
		Meta{ID: "outdent", Precondition: writable()},
		Meta{ID: "tab", Precondition: writable()},
		Meta{ID: "deleteLeft", Precondition: writable()},
		Meta{ID: "deleteRight", Precondition: writable()},
		Meta{ID: "deleteWordStartLeft", Precondition: writable()},
		Meta{ID: "deleteWordEndLeft", Precondition: writable()},
		Meta{ID: "deleteWordLeft", Precondition: writable()},
		Meta{ID: "deleteWordStartRight", Precondition: writable()},
		Meta{ID: "deleteWordEndRight", Precondition: writable()},
		Meta{ID: "deleteWordRight", Precondition: writable()},
		Meta{ID: "deleteWordPartLeft", Precondition: writable()},
		Meta{ID: "deleteWordPartRight", Precondition: writable()},
		Meta{ID: "deleteInsideWord", Title: "Delete Word", Precondition: writable()},
	)
}

// registerLineCommands registers the commands on whole lines.
//
// Ported from VS Code: src/vs/editor/contrib/linesOperations/browser/linesOperations.ts
func registerLineCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: "editor.action.copyLinesUpAction", Title: "Copy Line Up", Precondition: writable()},
		Meta{ID: "editor.action.copyLinesDownAction", Title: "Copy Line Down", Precondition: writable()},
		Meta{ID: "editor.action.moveLinesUpAction", Title: "Move Line Up", Precondition: writable()},
		Meta{ID: "editor.action.moveLinesDownAction", Title: "Move Line Down", Precondition: writable()},
		Meta{ID: "editor.action.sortLinesAscending", Title: "Sort Lines Ascending", Precondition: writable()},
		Meta{ID: "editor.action.sortLinesDescending", Title: "Sort Lines Descending", Precondition: writable()},
		Meta{ID: "editor.action.trimTrailingWhitespace", Title: "Trim Trailing Whitespace", Precondition: writable()},
		Meta{ID: "editor.action.deleteLines", Title: "Delete Line", Precondition: writable()},
		Meta{ID: "editor.action.indentLines", Title: "Indent Line", Precondition: writable()},
		Meta{ID: "editor.action.outdentLines", Title: "Outdent Line", Precondition: writable()},
		Meta{ID: "editor.action.insertLineBefore", Title: "Insert Line Above", Precondition: writable()},
		Meta{ID: "editor.action.insertLineAfter", Title: "Insert Line Below", Precondition: writable()},
		Meta{ID: "editor.action.joinLines", Title: "Join Lines", Precondition: writable()},
		Meta{ID: "editor.action.transpose", Title: "Transpose Characters around the Cursor", Precondition: writable()},
		Meta{ID: "editor.action.transformToUppercase", Title: "Transform to Uppercase", Precondition: writable()},
		Meta{ID: "editor.action.transformToLowercase", Title: "Transform to Lowercase", Precondition: writable()},
		Meta{ID: "editor.action.transformToTitlecase", Title: "Transform to Title Case", Precondition: writable()},
	)
}

// registerMultiCursorCommands registers the commands that add cursors and expand the
// selection.
//
// Ported from VS Code: src/vs/editor/contrib/multicursor/browser/multicursor.ts and
// src/vs/editor/contrib/smartSelect/browser/smartSelect.ts
func registerMultiCursorCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: "editor.action.insertCursorAbove", Title: "Add Cursor Above"},
		Meta{ID: "editor.action.insertCursorBelow", Title: "Add Cursor Below"},
		Meta{ID: "editor.action.insertCursorAtEndOfEachLineSelected", Title: "Add Cursors to Line Ends"},
		Meta{ID: "editor.action.addSelectionToNextFindMatch", Title: "Add Selection to Next Find Match"},
		Meta{ID: "editor.action.addSelectionToPreviousFindMatch", Title: "Add Selection to Previous Find Match"},
		Meta{ID: "editor.action.moveSelectionToNextFindMatch", Title: "Move Last Selection to Next Find Match"},
		Meta{ID: "editor.action.moveSelectionToPreviousFindMatch", Title: "Move Last Selection to Previous Find Match"},
		Meta{ID: "editor.action.selectHighlights", Title: "Select All Occurrences of Find Match"},
		Meta{ID: "editor.action.smartSelect.expand", Title: "Expand Selection"},
		Meta{ID: "editor.action.smartSelect.shrink", Title: "Shrink Selection"},
	)
}
