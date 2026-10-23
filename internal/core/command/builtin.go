package command

import (
	"strconv"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

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
	registerBracketCommands(r)
	registerIndentationCommands(r)
	registerCommentCommands(r)
	registerFoldingCommands(r)
	registerFindCommands(r)
	registerWordWrapCommands(r)
	registerClipboardCommands(r)
	registerDisplayCommands(r)
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

// registerIndentationCommands registers the commands that change the indentation of a document.
//
// Ported from VS Code: src/vs/editor/contrib/indentation/browser/indentation.ts
func registerIndentationCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: "editor.action.indentationToSpaces", Title: "Convert Indentation to Spaces"},
		Meta{ID: "editor.action.indentationToTabs", Title: "Convert Indentation to Tabs"},
		Meta{ID: "editor.action.indentUsingSpaces", Title: "Indent Using Spaces"},
		Meta{ID: "editor.action.indentUsingTabs", Title: "Indent Using Tabs"},
		Meta{ID: "editor.action.changeTabDisplaySize", Title: "Change Tab Display Size"},
		Meta{ID: "editor.action.detectIndentation", Title: "Detect Indentation from Content"},
		Meta{ID: "editor.action.reindentlines", Title: "Reindent Lines", Precondition: writable()},
		Meta{ID: "editor.action.reindentselectedlines", Title: "Reindent Selected Lines", Precondition: writable()},
	)
}

// registerCommentCommands registers the commands that comment and uncomment code.
//
// Ported from VS Code: src/vs/editor/contrib/comment/browser/comment.ts
func registerCommentCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: "editor.action.commentLine", Title: "Toggle Line Comment", Precondition: writable()},
		Meta{ID: "editor.action.addCommentLine", Title: "Add Line Comment", Precondition: writable()},
		Meta{ID: "editor.action.removeCommentLine", Title: "Remove Line Comment", Precondition: writable()},
		Meta{ID: "editor.action.blockComment", Title: "Toggle Block Comment", Precondition: writable()},
	)
}

// registerBracketCommands registers the commands of the brackets.
//
// Ported from VS Code: src/vs/editor/contrib/bracketMatching/browser/bracketMatching.ts
func registerBracketCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: "editor.action.jumpToBracket", Title: "Go to Bracket"},
		Meta{ID: "editor.action.selectToBracket", Title: "Select to Bracket"},
		Meta{ID: "editor.action.removeBrackets", Title: "Remove Brackets"},
	)
}

// registerClipboardCommands registers the commands that copy, cut and paste.
//
// Ported from VS Code: src/vs/editor/contrib/clipboard/browser/clipboard.ts
func registerClipboardCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: "editor.action.clipboardCutAction", Title: "Cut"},
		Meta{ID: "editor.action.clipboardCopyAction", Title: "Copy"},
		Meta{ID: "editor.action.clipboardPasteAction", Title: "Paste"},
	)
}

// registerDisplayCommands registers the commands that zoom the font of the editor.
//
// Ported from VS Code: src/vs/editor/contrib/fontZoom/browser/fontZoom.ts
func registerDisplayCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: "editor.action.fontZoomIn", Title: "Increase Editor Font Size"},
		Meta{ID: "editor.action.fontZoomOut", Title: "Decrease Editor Font Size"},
		Meta{ID: "editor.action.fontZoomReset", Title: "Reset Editor Font Size"},
	)
}

// registerFoldingCommands registers the commands that fold and unfold code; they work when the
// editor folds (the context key foldingEnabled).
//
// Ported from VS Code: src/vs/editor/contrib/folding/browser/folding.ts
func registerFoldingCommands(r *Registry) {
	folding := contextkey.Has("foldingEnabled")
	metas := []Meta{
		{ID: "editor.fold", Title: "Fold"},
		{ID: "editor.unfold", Title: "Unfold"},
		{ID: "editor.toggleFold", Title: "Toggle Fold"},
		{ID: "editor.foldRecursively", Title: "Fold Recursively"},
		{ID: "editor.unfoldRecursively", Title: "Unfold Recursively"},
		{ID: "editor.toggleFoldRecursively", Title: "Toggle Fold Recursively"},
		{ID: "editor.foldAll", Title: "Fold All"},
		{ID: "editor.unfoldAll", Title: "Unfold All"},
		{ID: "editor.foldAllBlockComments", Title: "Fold All Block Comments"},
		{ID: "editor.foldAllMarkerRegions", Title: "Fold All Regions"},
		{ID: "editor.unfoldAllMarkerRegions", Title: "Unfold All Regions"},
		{ID: "editor.foldAllExcept", Title: "Fold All Regions Except Selected"},
		{ID: "editor.unfoldAllExcept", Title: "Unfold All Regions Except Selected"},
		{ID: "editor.gotoParentFold", Title: "Go to Parent Fold"},
		{ID: "editor.gotoPreviousFold", Title: "Go to Previous Folding Range"},
		{ID: "editor.gotoNextFold", Title: "Go to Next Folding Range"},
		{ID: "editor.createFoldingRangeFromSelection", Title: "Create Manual Folding Ranges from Selection"},
		{ID: "editor.removeManualFoldingRanges", Title: "Remove Manual Folding Ranges"},
	}
	for level := 1; level <= 7; level++ {
		metas = append(metas, Meta{ID: "editor.foldLevel" + strconv.Itoa(level), Title: "Fold Level " + strconv.Itoa(level)})
	}
	for i := range metas {
		metas[i].Precondition = folding
	}
	r.MustRegister(metas...)
}

// registerFindCommands registers the commands of the find widget (SPEC §9.8). The ones that work
// on the open widget need the context key findWidgetVisible.
//
// Ported from VS Code: src/vs/editor/contrib/find/browser/findController.ts
func registerFindCommands(r *Registry) {
	visible := contextkey.Has("findWidgetVisible")
	r.MustRegister(
		Meta{ID: "actions.find", Title: "Find", Category: "Edit"},
		Meta{ID: "actions.findWithSelection", Title: "Find with Selection", Category: "Edit"},
		Meta{ID: "editor.actions.findWithArgs", Title: "Find with Arguments", Category: "Edit",
			Description: `Open the find widget. Arguments: {"searchString", "replaceString", "isRegex", "matchWholeWord", "isCaseSensitive", "preserveCase", "findInSelection"}, all optional.`},
		Meta{ID: "editor.action.nextMatchFindAction", Title: "Find Next", Category: "Edit"},
		Meta{ID: "editor.action.previousMatchFindAction", Title: "Find Previous", Category: "Edit"},
		Meta{ID: "editor.action.nextSelectionMatchFindAction", Title: "Find Next Selection", Category: "Edit"},
		Meta{ID: "editor.action.previousSelectionMatchFindAction", Title: "Find Previous Selection", Category: "Edit"},
		Meta{ID: "editor.action.startFindReplaceAction", Title: "Replace", Category: "Edit"},
		Meta{ID: "closeFindWidget", Title: "Close Find Widget", Precondition: visible},
		Meta{ID: "toggleFindCaseSensitive", Title: "Toggle Find Case Sensitive"},
		Meta{ID: "toggleFindWholeWord", Title: "Toggle Find Whole Word"},
		Meta{ID: "toggleFindRegex", Title: "Toggle Find Regex"},
		Meta{ID: "toggleFindInSelection", Title: "Toggle Find in Selection"},
		Meta{ID: "togglePreserveCase", Title: "Toggle Preserve Case"},
		Meta{ID: "editor.action.replaceOne", Title: "Replace One", Precondition: visible},
		Meta{ID: "editor.action.replaceAll", Title: "Replace All", Precondition: visible},
		Meta{ID: "editor.action.selectAllMatches", Title: "Select All Occurrences of Find Match", Precondition: visible},
	)
}

// registerWordWrapCommands registers the command that toggles word wrap.
//
// Ported from VS Code: src/vs/workbench/contrib/codeEditor/browser/toggleWordWrap.ts
func registerWordWrapCommands(r *Registry) {
	r.MustRegister(Meta{ID: "editor.action.toggleWordWrap", Title: "Toggle Word Wrap", Category: "View"})
}
