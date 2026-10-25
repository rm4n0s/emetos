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
	registerSnippetCommands(r)
	registerEmmetCommands(r)
	registerWordWrapCommands(r)
	registerClipboardCommands(r)
	registerDisplayCommands(r)
	registerLayoutCommands(r)
	registerEditorGroupCommands(r)
	registerEditorGridCommands(r)
	registerNotificationCommands(r)
	registerPanelViewCommands(r)
	registerExplorerCommands(r)
	registerSaveCommands(r)
	registerWindowCommands(r)
	registerRecentCommands(r)
	registerSettingsCommands(r)
	registerKeybindingsEditorCommands(r)
	registerThemeCommands(r)
	registerProfileCommands(r)
	registerQuickInputCommands(r)
	registerStatusBarCommands(r)
	registerDiffEditorCommands(r)
	registerMergeEditorCommands(r)
	return r
}

// registerLayoutCommands registers the commands that show, hide, move and focus the parts of the
// workbench (SPEC §10.1). Maximizing the panel needs the panel at a side or a panel that is
// centered (the alignment is always centered in Emetos).
//
// Ported from VS Code: src/vs/workbench/browser/actions/layoutActions.ts,
// src/vs/workbench/browser/parts/panel/panelActions.ts,
// src/vs/workbench/browser/parts/sidebar/sidebarActions.ts,
// src/vs/workbench/browser/parts/statusbar/statusbarActions.ts,
// src/vs/workbench/browser/parts/editor/editorActions.ts,
// src/vs/workbench/browser/parts/activitybar/activitybarPart.ts and
// src/vs/workbench/browser/parts/titlebar/menubarControl.ts,
// src/vs/workbench/browser/actions/windowActions.ts (toggleFullScreen),
// src/vs/workbench/electron-browser/actions/windowActions.ts (zoomIn, zoomOut, zoomReset) and
// src/vs/workbench/browser/workbench.zenMode.contribution.ts
func registerLayoutCommands(r *Registry) {
	maximizable := contextkey.Or(
		contextkey.Equals("panelAlignment", "center"),
		contextkey.And(contextkey.NotEquals("panelPosition", "bottom"), contextkey.NotEquals("panelPosition", "top")))
	r.MustRegister(
		Meta{ID: "workbench.action.toggleSidebarVisibility", Title: "Toggle Primary Side Bar Visibility", Category: "View",
			Description: "Open/Show and Close/Hide Sidebar"},
		Meta{ID: "workbench.action.toggleActivityBarVisibility", Title: "Toggle Activity Bar Visibility"},
		Meta{ID: "workbench.action.toggleStatusbarVisibility", Title: "Toggle Status Bar Visibility", Category: "View"},
		Meta{ID: "workbench.action.toggleMenuBar", Title: "Toggle Menu Bar", Category: "View"},
		Meta{ID: "workbench.action.togglePanel", Title: "Toggle Panel Visibility", Category: "View",
			Description: "Open/Show and Close/Hide Panel"},
		Meta{ID: "workbench.action.toggleMaximizedPanel", Title: "Toggle Maximized Panel", Category: "View", Precondition: maximizable},
		Meta{ID: "workbench.action.positionPanelLeft", Title: "Move Panel Left", Category: "View"},
		Meta{ID: "workbench.action.positionPanelRight", Title: "Move Panel Right", Category: "View"},
		Meta{ID: "workbench.action.positionPanelBottom", Title: "Move Panel To Bottom", Category: "View"},
		Meta{ID: "workbench.action.positionPanelTop", Title: "Move Panel To Top", Category: "View"},
		Meta{ID: "workbench.action.focusSideBar", Title: "Focus into Primary Side Bar", Category: "View"},
		Meta{ID: "workbench.action.focusActiveEditorGroup", Title: "Focus Active Editor Group", Category: "View"},
		Meta{ID: "workbench.action.focusPanel", Title: "Focus into Panel", Category: "View"},
		Meta{ID: "workbench.action.focusStatusBar", Title: "Focus Status Bar", Category: "View"},
		Meta{ID: "workbench.action.focusActivityBar", Title: "Focus Activity Bar", Category: "View"},
		Meta{ID: "workbench.actions.menubar.focus", Title: "Focus Application Menu"},

		// step 8.5: full screen, zen mode, the centered layout and the zoom of the window (SPEC §10.9);
		// Escape Escape leaves zen mode and has no title, so the palette does not list it
		Meta{ID: "workbench.action.toggleFullScreen", Title: "Toggle Full Screen", Category: "View"},
		Meta{ID: "workbench.action.toggleZenMode", Title: "Toggle Zen Mode", Category: "View"},
		Meta{ID: "workbench.action.exitZenMode"},
		Meta{ID: "workbench.action.toggleCenteredLayout", Title: "Toggle Centered Layout", Category: "View"},
		Meta{ID: "workbench.action.zoomIn", Title: "Zoom In", Category: "View"},
		Meta{ID: "workbench.action.zoomOut", Title: "Zoom Out", Category: "View"},
		Meta{ID: "workbench.action.zoomReset", Title: "Reset Zoom", Category: "View"},
	)
}

// registerEditorGroupCommands registers the commands that close, switch and keep the editors of a
// group (SPEC §10.3), switch between them with the quick input and walk the history of locations (step
// 6.19). The commands of the grid of groups are in registerEditorGridCommands.
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/editorCommands.ts,
// src/vs/workbench/browser/parts/editor/editorActions.ts and
// src/vs/workbench/browser/parts/editor/editor.contribution.ts
func registerEditorGroupCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: "workbench.action.closeActiveEditor", Title: "Close Editor", Category: "View"},
		Meta{ID: "workbench.action.closeEditorsInGroup", Title: "Close All Editors in Group", Category: "View"},
		Meta{ID: "workbench.action.closeUnmodifiedEditors", Title: "Close Saved Editors in Group", Category: "View"},
		Meta{ID: "workbench.action.closeOtherEditors", Title: "Close Other Editors in Group", Category: "View"},
		Meta{ID: "workbench.action.closeEditorsToTheRight", Title: "Close Editors to the Right in Group", Category: "View"},
		Meta{ID: "workbench.action.closeAllEditors", Title: "Close All Editors", Category: "View"},
		Meta{ID: "workbench.action.keepEditor", Title: "Keep Editor", Category: "View"},
		Meta{ID: "workbench.action.pinEditor", Title: "Pin Editor", Category: "View"},
		Meta{ID: "workbench.action.unpinEditor", Title: "Unpin Editor", Category: "View"},
		Meta{ID: "workbench.action.nextEditor", Title: "Open Next Editor", Category: "View"},
		Meta{ID: "workbench.action.previousEditor", Title: "Open Previous Editor", Category: "View"},
		Meta{ID: "workbench.action.nextEditorInGroup", Title: "Open Next Editor in Group", Category: "View"},
		Meta{ID: "workbench.action.previousEditorInGroup", Title: "Open Previous Editor in Group", Category: "View"},
		Meta{ID: "workbench.action.openNextRecentlyUsedEditor", Title: "Open Next Recently Used Editor", Category: "View"},
		Meta{ID: "workbench.action.openPreviousRecentlyUsedEditor", Title: "Open Previous Recently Used Editor", Category: "View"},
		Meta{ID: "workbench.action.openNextRecentlyUsedEditorInGroup", Title: "Open Next Recently Used Editor In Group", Category: "View"},
		Meta{ID: "workbench.action.openPreviousRecentlyUsedEditorInGroup", Title: "Open Previous Recently Used Editor In Group", Category: "View"},

		// the editor picker: the modes of the quick input that list the open editors, and the keys that
		// open it and move in it while the key that opened it is held
		Meta{ID: "workbench.action.showAllEditors", Title: "Show All Editors By Appearance", Category: "File"},
		Meta{ID: "workbench.action.showAllEditorsByMostRecentlyUsed", Title: "Show All Editors By Most Recently Used", Category: "View"},
		Meta{ID: "workbench.action.showEditorsInActiveGroup", Title: "Show Editors in Active Group By Most Recently Used", Category: "View"},
		Meta{ID: "workbench.action.quickOpenPreviousRecentlyUsedEditor", Title: "Quick Open Previous Recently Used Editor", Category: "View"},
		Meta{ID: "workbench.action.quickOpenLeastRecentlyUsedEditor", Title: "Quick Open Least Recently Used Editor", Category: "View"},
		Meta{ID: "workbench.action.quickOpenPreviousRecentlyUsedEditorInGroup", Title: "Quick Open Previous Recently Used Editor in Group", Category: "View",
			Precondition: contextkey.Not("activeEditorGroupEmpty")},
		Meta{ID: "workbench.action.quickOpenLeastRecentlyUsedEditorInGroup", Title: "Quick Open Least Recently Used Editor in Group", Category: "View",
			Precondition: contextkey.Not("activeEditorGroupEmpty")},
		Meta{ID: "workbench.action.openPreviousEditorFromHistory", Title: "Quick Open Previous Editor from History"},
		Meta{ID: "workbench.action.quickOpenNavigateNextInEditorPicker"},
		Meta{ID: "workbench.action.quickOpenNavigatePreviousInEditorPicker"},

		// the history of locations
		Meta{ID: "workbench.action.navigateBack", Title: "Go Back", Precondition: contextkey.Has("canNavigateBack")},
		Meta{ID: "workbench.action.navigateForward", Title: "Go Forward", Precondition: contextkey.Has("canNavigateForward")},
		Meta{ID: "workbench.action.navigateLast", Title: "Go Previous"},
		Meta{ID: "workbench.action.navigateBackInEditLocations", Title: "Go Back in Edit Locations"},
		Meta{ID: "workbench.action.navigateForwardInEditLocations", Title: "Go Forward in Edit Locations"},
		Meta{ID: "workbench.action.navigatePreviousInEditLocations", Title: "Go Previous in Edit Locations"},
		Meta{ID: "workbench.action.navigateToLastEditLocation", Title: "Go to Last Edit Location"},
		Meta{ID: "workbench.action.navigateBackInNavigationLocations", Title: "Go Back in Navigation Locations"},
		Meta{ID: "workbench.action.navigateForwardInNavigationLocations", Title: "Go Forward in Navigation Locations"},
		Meta{ID: "workbench.action.navigatePreviousInNavigationLocations", Title: "Go Previous in Navigation Locations"},
		Meta{ID: "workbench.action.navigateToLastNavigationLocation", Title: "Go to Last Navigation Location"},
		Meta{ID: "workbench.action.reopenClosedEditor", Title: "Reopen Closed Editor", Category: "View"},
		Meta{ID: "workbench.action.clearEditorHistory", Title: "Clear Editor History"},

		// the paths of the file of an editor: the menu of a tab, the palette and the keys
		Meta{ID: "copyFilePath", Title: "Copy Path of Active File", Category: "File"},
		Meta{ID: "copyRelativeFilePath", Title: "Copy Relative Path of Active File", Category: "File"},
		Meta{ID: "workbench.action.files.copyPathOfActiveFile"},
	)
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

// registerSnippetCommands registers the commands of snippets: moving between the tab stops of a
// snippet that is being filled in, leaving it, and inserting snippets.
//
// Ported from VS Code: src/vs/editor/contrib/snippet/browser/snippetController2.ts,
// src/vs/workbench/contrib/snippets/browser/tabCompletion.ts and
// src/vs/workbench/contrib/snippets/browser/commands/insertSnippet.ts
func registerSnippetCommands(r *Registry) {
	inSnippet := contextkey.Has("inSnippetMode")
	r.MustRegister(
		Meta{ID: "jumpToNextSnippetPlaceholder", Precondition: contextkey.And(inSnippet, contextkey.Has("hasNextTabstop"))},
		Meta{ID: "jumpToPrevSnippetPlaceholder", Precondition: contextkey.And(inSnippet, contextkey.Has("hasPrevTabstop"))},
		Meta{ID: "leaveSnippet", Precondition: inSnippet},
		Meta{ID: "acceptSnippet", Precondition: inSnippet},
		Meta{ID: "insertSnippet", Precondition: contextkey.Has("hasSnippetCompletions")},
		Meta{ID: "editor.action.insertSnippet", Title: "Insert Snippet", Precondition: writable(),
			Description: "Insert a snippet. The argument is an object with snippet (the text), or name and langId (a snippet of a language); without them the user picks one."},
	)
}

// registerEmmetCommands registers the command that expands an Emmet abbreviation.
//
// Ported from VS Code: extensions/emmet/package.json (editor.emmet.action.expandAbbreviation)
func registerEmmetCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: "editor.emmet.action.expandAbbreviation", Title: "Emmet: Expand Abbreviation", Precondition: writable(),
			Description: "Expand the Emmet abbreviation before each cursor, or the selected text."},
	)
}

// registerEditorGridCommands registers the commands of the grid of editor groups (SPEC §10.3): split,
// focus, move and copy editors between groups, move, join, close, maximize and size groups, and the
// layouts. A command that VS Code registers without a title (focus group 2 to 8, the commands that
// take arguments) has none here, and the palette skips it.
//
// Not here: the commands that expand a group (workbench.action.minimizeOtherEditors,
// minimizeOtherEditorsHideSidebar, toggleEditorWidths), which need the least size of a group in
// pixels, and workbench.action.maximizeEditorHideSidebar.
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/editorCommands.ts,
// src/vs/workbench/browser/parts/editor/editorActions.ts and
// src/vs/workbench/browser/parts/editor/editor.contribution.ts
func registerEditorGridCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: "workbench.action.splitEditor", Title: "Split Editor", Category: "View"},
		Meta{ID: "workbench.action.splitEditorOrthogonal", Title: "Split Editor Orthogonal", Category: "View"},
		Meta{ID: "workbench.action.splitEditorLeft", Title: "Split Editor Left", Category: "View"},
		Meta{ID: "workbench.action.splitEditorRight", Title: "Split Editor Right", Category: "View"},
		Meta{ID: "workbench.action.splitEditorUp", Title: "Split Editor Up", Category: "View"},
		Meta{ID: "workbench.action.splitEditorDown", Title: "Split Editor Down", Category: "View"},

		Meta{ID: "workbench.action.focusFirstEditorGroup", Title: "Focus First Editor Group", Category: "View"},
		Meta{ID: "workbench.action.focusSecondEditorGroup"},
		Meta{ID: "workbench.action.focusThirdEditorGroup"},
		Meta{ID: "workbench.action.focusFourthEditorGroup"},
		Meta{ID: "workbench.action.focusFifthEditorGroup"},
		Meta{ID: "workbench.action.focusSixthEditorGroup"},
		Meta{ID: "workbench.action.focusSeventhEditorGroup"},
		Meta{ID: "workbench.action.focusEighthEditorGroup"},
		Meta{ID: "workbench.action.focusLastEditorGroup", Title: "Focus Last Editor Group", Category: "View"},
		Meta{ID: "workbench.action.focusNextGroup", Title: "Focus Next Editor Group", Category: "View"},
		Meta{ID: "workbench.action.focusPreviousGroup", Title: "Focus Previous Editor Group", Category: "View"},
		Meta{ID: "workbench.action.focusLeftGroup", Title: "Focus Left Editor Group", Category: "View"},
		Meta{ID: "workbench.action.focusRightGroup", Title: "Focus Right Editor Group", Category: "View"},
		Meta{ID: "workbench.action.focusAboveGroup", Title: "Focus Editor Group Above", Category: "View"},
		Meta{ID: "workbench.action.focusBelowGroup", Title: "Focus Editor Group Below", Category: "View"},
		Meta{ID: "workbench.action.focusLeftGroupWithoutWrap"},
		Meta{ID: "workbench.action.focusRightGroupWithoutWrap"},
		Meta{ID: "workbench.action.focusAboveGroupWithoutWrap"},
		Meta{ID: "workbench.action.focusBelowGroupWithoutWrap"},
		Meta{ID: "workbench.action.navigateEditorGroups", Title: "Navigate Between Editor Groups", Category: "View"},

		Meta{ID: "workbench.action.moveEditorToPreviousGroup", Title: "Move Editor into Previous Group", Category: "View"},
		Meta{ID: "workbench.action.moveEditorToNextGroup", Title: "Move Editor into Next Group", Category: "View"},
		Meta{ID: "workbench.action.moveEditorToAboveGroup", Title: "Move Editor into Group Above", Category: "View"},
		Meta{ID: "workbench.action.moveEditorToBelowGroup", Title: "Move Editor into Group Below", Category: "View"},
		Meta{ID: "workbench.action.moveEditorToLeftGroup", Title: "Move Editor into Left Group", Category: "View"},
		Meta{ID: "workbench.action.moveEditorToRightGroup", Title: "Move Editor into Right Group", Category: "View"},
		Meta{ID: "workbench.action.moveEditorToFirstGroup", Title: "Move Editor into First Group", Category: "View"},
		Meta{ID: "workbench.action.moveEditorToLastGroup", Title: "Move Editor into Last Group", Category: "View"},
		Meta{ID: "workbench.action.splitEditorToPreviousGroup", Title: "Split Editor into Previous Group", Category: "View"},
		Meta{ID: "workbench.action.splitEditorToNextGroup", Title: "Split Editor into Next Group", Category: "View"},
		Meta{ID: "workbench.action.splitEditorToAboveGroup", Title: "Split Editor into Group Above", Category: "View"},
		Meta{ID: "workbench.action.splitEditorToBelowGroup", Title: "Split Editor into Group Below", Category: "View"},
		Meta{ID: "workbench.action.splitEditorToLeftGroup", Title: "Split Editor into Left Group", Category: "View"},
		Meta{ID: "workbench.action.splitEditorToRightGroup", Title: "Split Editor into Right Group", Category: "View"},
		Meta{ID: "workbench.action.splitEditorToFirstGroup", Title: "Split Editor into First Group", Category: "View"},
		Meta{ID: "workbench.action.splitEditorToLastGroup", Title: "Split Editor into Last Group", Category: "View"},
		Meta{ID: "moveActiveEditor", Description: "Move the active editor by tabs or groups. The argument is {\"to\": \"left\"|\"right\"|\"up\"|\"down\"|\"first\"|\"last\"|\"previous\"|\"next\"|\"center\"|\"position\", \"by\": \"tab\"|\"group\", \"value\": number}."},
		Meta{ID: "copyActiveEditor", Description: "Copy the active editor by groups. The argument is {\"to\": ..., \"value\": number}."},

		Meta{ID: "workbench.action.newGroupLeft", Title: "New Editor Group to the Left", Category: "View"},
		Meta{ID: "workbench.action.newGroupRight", Title: "New Editor Group to the Right", Category: "View"},
		Meta{ID: "workbench.action.newGroupAbove", Title: "New Editor Group Above", Category: "View"},
		Meta{ID: "workbench.action.newGroupBelow", Title: "New Editor Group Below", Category: "View"},

		Meta{ID: "workbench.action.moveActiveEditorGroupLeft", Title: "Move Editor Group Left", Category: "View"},
		Meta{ID: "workbench.action.moveActiveEditorGroupRight", Title: "Move Editor Group Right", Category: "View"},
		Meta{ID: "workbench.action.moveActiveEditorGroupUp", Title: "Move Editor Group Up", Category: "View"},
		Meta{ID: "workbench.action.moveActiveEditorGroupDown", Title: "Move Editor Group Down", Category: "View"},
		Meta{ID: "workbench.action.duplicateActiveEditorGroupLeft", Title: "Duplicate Editor Group Left", Category: "View"},
		Meta{ID: "workbench.action.duplicateActiveEditorGroupRight", Title: "Duplicate Editor Group Right", Category: "View"},
		Meta{ID: "workbench.action.duplicateActiveEditorGroupUp", Title: "Duplicate Editor Group Up", Category: "View"},
		Meta{ID: "workbench.action.duplicateActiveEditorGroupDown", Title: "Duplicate Editor Group Down", Category: "View"},
		Meta{ID: "workbench.action.joinTwoGroups", Title: "Join Editor Group with Next Group", Category: "View"},
		Meta{ID: "workbench.action.joinAllGroups", Title: "Join All Editor Groups", Category: "View"},

		Meta{ID: "workbench.action.closeGroup"},
		Meta{ID: "workbench.action.closeEditorsAndGroup"},
		Meta{ID: "workbench.action.closeAllGroups", Title: "Close All Editor Groups", Category: "View"},
		Meta{ID: "workbench.action.closeEditorsInOtherGroups", Title: "Close Editors in Other Groups", Category: "View"},
		Meta{ID: "workbench.action.closeEditorInAllGroups", Title: "Close Editor in All Groups", Category: "View"},

		Meta{ID: "workbench.action.evenEditorWidths", Title: "Reset Editor Group Sizes", Category: "View"},
		Meta{ID: "workbench.action.toggleMaximizeEditorGroup", Title: "Toggle Maximize Editor Group", Category: "View",
			Precondition: contextkey.Or(contextkey.Has("editorPartMultipleEditorGroups"), contextkey.Has("editorPartMaximizedEditorGroup"))},

		Meta{ID: "workbench.action.editorLayoutSingle", Title: "Single Column Editor Layout", Category: "View"},
		Meta{ID: "workbench.action.editorLayoutTwoColumns", Title: "Two Columns Editor Layout", Category: "View"},
		Meta{ID: "workbench.action.editorLayoutThreeColumns", Title: "Three Columns Editor Layout", Category: "View"},
		Meta{ID: "workbench.action.editorLayoutTwoRows", Title: "Two Rows Editor Layout", Category: "View"},
		Meta{ID: "workbench.action.editorLayoutThreeRows", Title: "Three Rows Editor Layout", Category: "View"},
		Meta{ID: "workbench.action.editorLayoutTwoByTwoGrid", Title: "Grid Editor Layout (2x2)", Category: "View"},
		Meta{ID: "workbench.action.editorLayoutTwoColumnsBottom", Title: "Two Columns Bottom Editor Layout", Category: "View"},
		Meta{ID: "workbench.action.editorLayoutTwoRowsRight", Title: "Two Rows Right Editor Layout", Category: "View"},
		Meta{ID: "layoutEditorGroups", Description: "Set the layout of the editor groups. The argument is {\"orientation\": 0|1, \"groups\": [{\"size\": number, \"groups\": [...]}]}: a tree of groups in which the orientation of a node is the opposite of its parent's."},
	)
}

// registerNotificationCommands registers the commands of the toasts and of the notification center
// (SPEC §10.7). The commands that act on the notification that has the focus (notification.clear,
// notification.expand, notification.collapse, notification.toggle), the ones that move between toasts
// (notifications.focusNextToast and its kin), notifications.toggleDoNotDisturbModeBySource (it needs the
// quick input) and notification.acceptPrimaryAction (it is for the screen reader mode) are not built yet.
//
// Ported from VS Code: src/vs/workbench/browser/parts/notifications/notificationsCommands.ts
func registerNotificationCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: "notifications.showList", Title: "Show Notifications", Category: "Notifications"},
		Meta{ID: "notifications.hideList", Title: "Hide Notifications", Category: "Notifications"},
		Meta{ID: "notifications.toggleList"},
		Meta{ID: "notifications.hideToasts"},
		Meta{ID: "notifications.focusToasts", Title: "Focus Notification Toast", Category: "Notifications"},
		Meta{ID: "notifications.clearAll", Title: "Clear All Notifications", Category: "Notifications"},
		Meta{ID: "notifications.toggleDoNotDisturbMode", Title: "Toggle Do Not Disturb Mode", Category: "Notifications"},
	)
}

// registerPanelViewCommands registers the commands of the Problems and Output panels (SPEC §10.13).
// Not registered yet: the commands that need the quick input (workbench.action.showOutputChannels,
// workbench.output.action.switchBetweenOutputs, the compound, import, export and save log actions, the
// log level actions), the table view of the Problems (the viewAsTable and viewAsTree actions),
// the copy and quick fix actions of a problem, and the toggle of the excluded files (Emetos has no
// files.exclude setting before step 6.13).
//
// Ported from VS Code: src/vs/workbench/contrib/markers/browser/markers.contribution.ts,
// src/vs/workbench/contrib/markers/browser/markersViewActions.ts,
// src/vs/workbench/contrib/output/browser/output.contribution.ts and
// src/vs/workbench/services/views/browser/viewsService.ts (the commands of a view)
func registerPanelViewCommands(r *Registry) {
	r.MustRegister(
		// open, toggle and focus the views
		Meta{ID: "workbench.actions.view.problems", Title: "Toggle Problems", Category: "View"},
		Meta{ID: "workbench.action.output.toggleOutput", Title: "Toggle Output", Category: "View"},
		Meta{ID: "workbench.panel.markers.view.focus", Title: "Focus on Problems View", Category: "View"},
		Meta{ID: "workbench.panel.output.focus", Title: "Focus on Output View", Category: "View"},
		Meta{ID: "workbench.action.problems.focus", Title: "Focus Problems (Errors, Warnings, Infos)", Category: "View"},
		Meta{ID: "workbench.actions.view.toggleProblems", Title: "Toggle Problems (Errors, Warnings, Infos)"},
		Meta{ID: "workbench.action.showErrorsWarnings"},

		// the Problems view
		Meta{ID: "problems.action.focusFilter", Title: "Focus problems filter"},
		Meta{ID: "problems.action.focusProblemsFromFilter", Title: "Focus problems view"},
		Meta{ID: "problems.action.clearFilterText", Title: "Clear filters text", Category: "Problems"},
		Meta{ID: "workbench.actions.treeView.workbench.panel.markers.view.collapseAll", Title: "Collapse All"},
		Meta{ID: "workbench.actions.workbench.panel.markers.view.toggleErrors", Title: "Show Errors", Category: "Problems",
			Description: "Show or hide errors in the problems view."},
		Meta{ID: "workbench.actions.workbench.panel.markers.view.toggleWarnings", Title: "Show Warnings", Category: "Problems",
			Description: "Show or hide warnings in the problems view."},
		Meta{ID: "workbench.actions.workbench.panel.markers.view.toggleInfos", Title: "Show Infos", Category: "Problems",
			Description: "Show or hide infos in the problems view."},
		Meta{ID: "workbench.actions.workbench.panel.markers.view.toggleActiveFile", Title: "Show Active File Only", Category: "Problems",
			Description: "Show or hide problems (errors, warnings, info) only from the active file in the problems view."},

		// the Output view
		Meta{ID: "workbench.output.action.clearOutput", Title: "Clear Output", Category: "View"},
		Meta{ID: "workbench.output.action.toggleAutoScroll", Title: "Toggle Auto Scrolling"},
		Meta{ID: "workbench.action.openActiveLogOutputFile", Title: "Open Output in Editor"},
	)
}

// registerExplorerCommands registers the commands of the Explorer (SPEC §10.8): showing and focusing it,
// revealing a file in it, refreshing and collapsing its tree, and opening and closing the folder of
// the window. The commands of its file operations (new file, rename, delete) come with step 6.15.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/explorerViewlet.ts,
// src/vs/workbench/contrib/files/browser/fileActions.ts (FocusFilesExplorer, ShowActiveFileInExplorer),
// src/vs/workbench/contrib/files/browser/fileCommands.ts (revealInExplorer),
// src/vs/workbench/contrib/files/browser/views/explorerView.ts (refreshFilesExplorer,
// collapseExplorerFolders) and src/vs/workbench/browser/actions/workspaceActions.ts
func registerExplorerCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: "workbench.view.explorer", Title: "Show Explorer", Category: "View"},
		Meta{ID: "workbench.explorer.fileView.focus", Title: "Focus on Folders View", Category: "View"},
		Meta{ID: "workbench.files.action.focusOpenEditorsView", Title: "Focus on Open Editors View", Category: "View"},
		Meta{ID: "workbench.files.action.focusFilesExplorer", Title: "Focus on Files Explorer", Category: "File",
			Description: "Moves focus to the file explorer view container."},
		Meta{ID: "workbench.files.action.showActiveFileInExplorer", Title: "Reveal Active File in Explorer View", Category: "File",
			Description: "Reveals and selects the active file within the explorer view."},
		Meta{ID: "revealInExplorer", Title: "Reveal in Explorer View"},
		Meta{ID: "workbench.files.action.refreshFilesExplorer", Title: "Refresh Explorer",
			Description: "Forces a refresh of the Explorer."},
		Meta{ID: "workbench.files.action.collapseExplorerFolders", Title: "Collapse Folders in Explorer",
			Description: "Folds all folders in the Explorer."},
		Meta{ID: "workbench.action.files.openFolder", Title: "Open Folder...", Category: "File"},
		Meta{ID: "workbench.action.closeFolder", Title: "Close Workspace", Category: "Workspaces",
			Precondition: contextkey.NotEquals("workbenchState", "empty")},
	)
	registerExplorerFileCommands(r)
}

// registerSaveCommands registers the commands that save, revert and make files (SPEC §3.1, §6.3): Save, Save As,
// Save All, Save without Formatting (which skips the save participants, as there is no formatter), Revert File,
// New Untitled Text File and Toggle Auto Save (files.autoSave, step 7.3). "saveAll" is VS Code's id of the command the File menu and the keys run;
// workbench.action.files.saveAll is the one of the palette, which runs it. Not here: Save All in Group
// (workbench.files.action.saveAllInGroup) and Save All Files (workbench.action.files.saveFiles).
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/fileConstants.ts,
// src/vs/workbench/contrib/files/browser/fileCommands.ts and
// src/vs/workbench/contrib/files/browser/fileActions.contribution.ts and
// src/vs/workbench/contrib/files/browser/fileActions.ts (ToggleAutoSaveAction)
func registerSaveCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: "workbench.action.files.save", Title: "Save", Category: "File"},
		Meta{ID: "workbench.action.files.saveWithoutFormatting", Title: "Save without Formatting", Category: "File"},
		Meta{ID: "workbench.action.files.saveAs", Title: "Save As...", Category: "File"},
		Meta{ID: "saveAll"},
		Meta{ID: "workbench.action.files.saveAll", Title: "Save All"},
		Meta{ID: "workbench.action.files.revert", Title: "Revert File", Category: "File"},
		Meta{ID: "workbench.action.files.newUntitledFile", Title: "New Untitled Text File", Category: "File",
			Description: "New Untitled Text File"},
		Meta{ID: "workbench.action.toggleAutoSave", Title: "Toggle Auto Save", Category: "File",
			Description: "Toggle the ability to save files automatically after typing"},
	)
}

// registerExplorerFileCommands registers the commands of the file operations of the Explorer (SPEC §10.5):
// creating, renaming, deleting, cutting, copying and pasting, opening to the side, revealing in the file
// manager, and the buttons in the header of the folder view that create. The handlers are the window's
// (gui/app/fileops.go).
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/fileActions.ts,
// src/vs/workbench/contrib/files/browser/fileActions.contribution.ts,
// src/vs/workbench/contrib/files/browser/fileCommands.ts,
// src/vs/workbench/contrib/files/browser/views/explorerView.ts and
// src/vs/workbench/contrib/files/electron-browser/fileActions.contribution.ts
func registerExplorerFileCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: "explorer.newFile", Title: "New File...", Description: "Creates a file in the folder of the Explorer."},
		Meta{ID: "explorer.newFolder", Title: "New Folder...", Description: "Creates a folder in the folder of the Explorer."},
		Meta{ID: "workbench.files.action.createFileFromExplorer", Title: "New File..."},
		Meta{ID: "workbench.files.action.createFolderFromExplorer", Title: "New Folder..."},
		Meta{ID: "renameFile", Title: "Rename..."},
		Meta{ID: "moveFileToTrash", Title: "Delete"},
		Meta{ID: "deleteFile", Title: "Delete Permanently"},
		Meta{ID: "filesExplorer.cut", Title: "Cut"},
		Meta{ID: "filesExplorer.copy", Title: "Copy"},
		Meta{ID: "filesExplorer.paste", Title: "Paste"},
		Meta{ID: "filesExplorer.cancelCut", Title: "Cancel Cut"},
		Meta{ID: "explorer.openToSide", Title: "Open to the Side"},
		Meta{ID: "revealFileInOS", Title: "Open Containing Folder"},
	)
}

// registerQuickInputCommands registers the commands that open the quick input in a mode (SPEC §10.6).
// Quick Open View is a quick pick of its own in VS Code and is hidden from the palette (see
// registerCommandPaletteMenu); Close Quick Open has no title.
//
// Ported from VS Code: src/vs/workbench/contrib/quickaccess/browser/commandsQuickAccess.ts,
// src/vs/workbench/contrib/quickaccess/browser/viewQuickAccess.ts,
// src/vs/workbench/browser/actions/quickAccessActions.ts and
// src/vs/workbench/contrib/codeEditor/browser/quickaccess/gotoLineQuickAccess.ts
func registerQuickInputCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: "workbench.action.showCommands", Title: "Show All Commands"},
		Meta{ID: "workbench.action.quickOpen", Title: "Go to File...", Description: "Quick access"},
		Meta{ID: "workbench.action.gotoLine", Title: "Go to Line/Column..."},
		Meta{ID: "workbench.action.gotoOffset", Title: "Go to Offset..."},
		Meta{ID: "workbench.action.openView", Title: "Open View", Category: "View"},
		Meta{ID: "workbench.action.quickOpenView", Title: "Quick Open View", Category: "View"},
		Meta{ID: "workbench.action.clearCommandHistory", Title: "Clear Command History"},
		Meta{ID: "workbench.action.closeQuickOpen"},
	)
}

// registerStatusBarCommands registers the commands of the pickers of the status bar (SPEC §3.2): the
// language mode, the end of line sequence, the encoding and the indentation. VS Code lists the first three
// in the palette; the indentation one is registered with the id of the window in its name and has no title.
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/editorStatus.ts
func registerStatusBarCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: "workbench.action.editor.changeLanguageMode", Title: "Change Language Mode",
			Description: "Change the language mode of the active text editor."},
		Meta{ID: "workbench.action.editor.changeEOL", Title: "Change End of Line Sequence"},
		Meta{ID: "workbench.action.editor.changeEncoding", Title: "Change File Encoding"},
		Meta{ID: "changeEditorIndentation"},
	)
}

// registerWindowCommands registers the commands that close the window and quit (SPEC §10.11): Close Window is
// in the palette; Quit (VS Code's "Exit", workbench.action.quit) is a command with a key and a menu row but no
// entry in the palette, as in VS Code.
//
// Ported from VS Code: src/vs/workbench/electron-browser/actions/windowActions.ts (CloseWindowAction,
// SwitchWindowAction), src/vs/workbench/browser/actions/windowActions.ts (NewWindowAction) and
// src/vs/workbench/electron-browser/desktop.contribution.ts (workbench.action.quit)
func registerWindowCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: "workbench.action.closeWindow", Title: "Close Window"},
		Meta{ID: "workbench.action.quit"},
		// Step 7.8: New Window (Ctrl+Shift+N, File menu) and Switch Window..., which picks one of the windows
		// of the process (no key on Linux).
		Meta{ID: "workbench.action.newWindow", Title: "New Window"},
		Meta{ID: "workbench.action.switchWindow", Title: "Switch Window..."},
	)
}

// registerRecentCommands registers the commands of the recently opened folders and files (SPEC §10.10, step
// 7.5): Open Recent (Ctrl+R), which picks one from a list, and Clear Recently Opened. "openRecentFolder" and
// "openRecentFile" are what the rows of File > Open Recent run, with the path as the argument; they have no
// title, so the palette does not list them (VS Code's menu rows use these ids without registering commands).
// Not here: Quick Open Recent (workbench.action.quickOpenRecent, which is the picker as a quick navigation) and
// the workspace rows of the list.
//
// Ported from VS Code: src/vs/workbench/browser/actions/windowActions.ts (OpenRecentAction) and
// src/vs/workbench/browser/parts/editor/editorActions.ts (ClearRecentFilesAction)
func registerRecentCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: "workbench.action.openRecent", Title: "Open Recent...", Category: "File"},
		Meta{ID: "workbench.action.clearRecentFiles", Title: "Clear Recently Opened...", Category: "File"},
		Meta{ID: "openRecentFolder"},
		Meta{ID: "openRecentFile"},
	)
}

// registerSettingsCommands registers the commands that open the settings editor (SPEC §11.1, step 8.1):
// workbench.action.openSettings (Ctrl+,; the title "Settings" is that of the rows of the Preferences and Manage
// menus, and the palette leaves it out as VS Code does: it lists "Preferences: Open Settings (UI)", which is
// workbench.action.openSettings2) and Open Workspace Settings, which needs a folder. Step 8.2 adds the commands
// that open the JSON files: Open User Settings (JSON), Open Workspace Settings (JSON), Open Default Settings (JSON)
// and Open User Settings (the User tab of the editor); the palette has all of them.
//
// Ported from VS Code: src/vs/workbench/contrib/preferences/browser/preferences.contribution.ts
func registerSettingsCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: "workbench.action.openSettings", Title: "Settings"},
		Meta{ID: "workbench.action.openSettings2", Title: "Open Settings (UI)", Category: "Preferences"},
		Meta{ID: "workbench.action.openWorkspaceSettings", Title: "Open Workspace Settings", Category: "Preferences"},
		Meta{ID: "workbench.action.openSettingsJson", Title: "Open User Settings (JSON)", Category: "Preferences"},
		Meta{ID: "workbench.action.openGlobalSettings", Title: "Open User Settings", Category: "Preferences"},
		Meta{ID: "workbench.action.openRawDefaultSettings", Title: "Open Default Settings (JSON)", Category: "Preferences"},
		Meta{ID: "workbench.action.openWorkspaceSettingsFile", Title: "Open Workspace Settings (JSON)", Category: "Preferences"},
	)
}

// registerKeybindingsEditorCommands registers the commands of the keybindings editor (step 8.3): the ones that open
// it and the files of the keybindings, and the ones it runs on its own rows and search box, which have no title
// (VS Code lists them by id in its own table) except Clear Search Results. The palette does not list the commands
// of the rows. Not registered: Sort by Precedence, Show System/Extension/User Keybindings, Clear Keyboard Shortcuts
// Search History, Define Keybinding of a JSON editor, and the Accept and Reject of the when-clause box (the box
// takes Enter and Escape itself).
//
// Ported from VS Code: src/vs/workbench/contrib/preferences/browser/preferences.contribution.ts
func registerKeybindingsEditorCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: "workbench.action.openGlobalKeybindings", Title: "Open Keyboard Shortcuts", Category: "Preferences"},
		Meta{ID: "workbench.action.openGlobalKeybindingsFile", Title: "Open Keyboard Shortcuts (JSON)", Category: "Preferences"},
		Meta{ID: "workbench.action.openDefaultKeybindingsFile", Title: "Open Default Keyboard Shortcuts (JSON)", Category: "Preferences"},

		Meta{ID: "keybindings.editor.searchKeybindings"},
		Meta{ID: "keybindings.editor.clearSearchResults", Title: "Clear Search Results"},
		Meta{ID: "keybindings.editor.recordSearchKeys"},
		Meta{ID: "keybindings.editor.focusKeybindings"},
		Meta{ID: "keybindings.editor.defineKeybinding"},
		Meta{ID: "keybindings.editor.addKeybinding"},
		Meta{ID: "keybindings.editor.defineWhenExpression"},
		Meta{ID: "keybindings.editor.removeKeybinding"},
		Meta{ID: "keybindings.editor.resetKeybinding"},
		Meta{ID: "keybindings.editor.showConflicts"},
		Meta{ID: "keybindings.editor.copyKeybindingEntry"},
		Meta{ID: "keybindings.editor.copyCommandKeybindingEntry"},
		Meta{ID: "keybindings.editor.copyCommandTitle"},
	)
}

// registerThemeCommands registers the commands of the theme pickers (step 8.4). Not registered: Product Icon Theme,
// Try New Default Themes, Install Additional Color Themes and Generate Color Theme From Current Settings.
//
// Ported from VS Code: src/vs/workbench/contrib/themes/browser/themes.contribution.ts
func registerThemeCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: "workbench.action.selectTheme", Title: "Color Theme", Category: "Preferences"},
		Meta{ID: "workbench.action.selectIconTheme", Title: "File Icon Theme", Category: "Preferences"},
	)
}

// registerProfileCommands registers the commands of the profiles (step 8.6b, SPEC §10.12), all in the category
// Profiles and in the palette, as VS Code's. VS Code's Create Profile opens the profiles editor (title "New Profile...");
// here it is a pair of quick inputs (title "Create Profile..."). Rename Profile and Import Profile have no command of
// their own in VS Code (they are buttons of its editor). Not registered: New Window with Profile, Save Current Profile
// As, Open Profiles (UI), Show Profile Contents, Cleanup Profiles and Reset Workspaces.
//
// Ported from VS Code: src/vs/workbench/contrib/userDataProfile/browser/userDataProfile.ts
func registerProfileCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: "workbench.profiles.actions.createProfile", Title: "Create Profile...", Category: "Profiles"},
		Meta{ID: "workbench.profiles.actions.switchProfile", Title: "Switch Profile...", Category: "Profiles"},
		Meta{ID: "workbench.profiles.actions.renameProfile", Title: "Rename Profile...", Category: "Profiles"},
		Meta{ID: "workbench.profiles.actions.deleteProfile", Title: "Delete Profile...", Category: "Profiles"},
		Meta{ID: "workbench.profiles.actions.exportProfile", Title: "Export Profile...", Category: "Profiles"},
		Meta{ID: "workbench.profiles.actions.importProfile", Title: "Import Profile...", Category: "Profiles"},
	)
}

// registerDiffEditorCommands registers the commands of the diff editor (step 9.5): Go to Next Change and Go to
// Previous Change (Alt+F5 and Shift+Alt+F5, the compare editor commands of the workbench), the three that focus a
// side of the diff (no title, as in VS Code: they are for keybindings) and Switch Side (the editor command, which
// maps the cursor to the other side); step 9.6 adds Toggle Inline View, Toggle Use Inline View When Space Is Limited and
// the three view modes, and the commands that open a diff: Compare with Saved, Compare Active File With..., Compare
// Active File with Clipboard and the Explorer's Select for Compare, Compare with Selected and Compare Selected (the ones
// without a title are for menus and keys). Not built: Open Active Diff Side, Swap Left and Right Editor Side, Toggle
// Collapse Unchanged Regions, Toggle Show Moved Code Blocks, Exit Compare Move, Compare New Untitled Text Files, the
// revert commands and the accessible diff viewer (SPEC section 9.11).
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/diffEditorCommands.ts,
// src/vs/editor/browser/widget/diffEditor/commands.ts and src/vs/workbench/contrib/files/browser/fileActions.contribution.ts
func registerDiffEditorCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: "workbench.action.compareEditor.nextChange", Title: "Go to Next Change"},
		Meta{ID: "workbench.action.compareEditor.previousChange", Title: "Go to Previous Change"},
		Meta{ID: "workbench.action.compareEditor.focusPrimarySide"},
		Meta{ID: "workbench.action.compareEditor.focusSecondarySide"},
		Meta{ID: "workbench.action.compareEditor.focusOtherSide"},
		Meta{ID: "diffEditor.switchSide", Title: "Switch Side", Category: "Diff Editor", Precondition: contextkey.Has("isInDiffEditor")},
		// the inline view (step 9.6)
		Meta{ID: "toggle.diff.renderSideBySide", Title: "Toggle Inline View", Category: "Compare"},
		Meta{ID: "diffEditor.toggleUseInlineViewWhenSpaceIsLimited", Title: "Toggle Use Inline View When Space Is Limited", Precondition: contextkey.Has("isInDiffEditor")},
		Meta{ID: "diffEditor.setViewMode.inline"},
		Meta{ID: "diffEditor.setViewMode.sideBySide"},
		Meta{ID: "diffEditor.setViewMode.automatic"},
		// the ways to open a diff (step 9.6)
		Meta{ID: "workbench.files.action.compareWithSaved", Title: "Compare Active File with Saved", Category: "File",
			Description: "Opens a new diff editor to compare the active file with the version on disk."},
		Meta{ID: "workbench.files.action.compareFileWith", Title: "Compare Active File With...", Category: "File", Precondition: contextkey.Has("activeEditor"),
			Description: "Opens a picker to select a file to diff with the active editor."},
		Meta{ID: "workbench.files.action.compareWithClipboard", Title: "Compare Active File with Clipboard", Category: "File",
			Description: "Opens a new diff editor to compare the active file with the contents of the clipboard."},
		Meta{ID: "selectForCompare"},
		Meta{ID: "compareFiles"},
		Meta{ID: "compareSelected"},
	)
}

// registerMergeEditorCommands registers the commands of the merge editor (steps 9.7 and 9.8): Go to Next Unhandled Conflict and Go
// to Previous Unhandled Conflict (in the palette while the active editor is a merge editor; VS Code gives them no key),
// "Resolve in Merge Editor" (git.openMergeEditor of VS Code's Git extension, which Emetos offers for a file with conflict
// markers) and _open.mergeEditor, the internal command that opens a merge of four files (no title: the palette skips
// it). Step 9.8 adds the commands that resolve and complete a merge: Toggle Current Conflict from Left and from Right,
// Accept All Incoming Changes from Left, Accept All Current Changes from Right, Accept All Combination, Reset Result and
// Complete Merge (Ctrl+Enter, see keybinding.registerMergeEditorKeys). Not built: the layout commands (mixed and column
// layout, show base), Show Non-Conflicting Changes, Open File, Open Base File, Compare Input With Base, Reset Choice for
// 'Close with Conflicts' and Toggle Between Merge Editor Inputs (SPEC section 9.11).
//
// Ported from VS Code: src/vs/workbench/contrib/mergeEditor/browser/commands/commands.ts and
// extensions/git/package.json (command.git.openMergeEditor)
func registerMergeEditorCommands(r *Registry) {
	isMergeEditor := contextkey.Has("isMergeEditor")
	r.MustRegister(
		Meta{ID: "merge.goToNextUnhandledConflict", Title: "Go to Next Unhandled Conflict", Category: "Merge Editor", Precondition: isMergeEditor},
		Meta{ID: "merge.goToPreviousUnhandledConflict", Title: "Go to Previous Unhandled Conflict", Category: "Merge Editor", Precondition: isMergeEditor},
		Meta{ID: "merge.toggleActiveConflictInput1", Title: "Toggle Current Conflict from Left", Category: "Merge Editor", Precondition: isMergeEditor},
		Meta{ID: "merge.toggleActiveConflictInput2", Title: "Toggle Current Conflict from Right", Category: "Merge Editor", Precondition: isMergeEditor},
		Meta{ID: "merge.acceptAllInput1", Title: "Accept All Incoming Changes from Left", Category: "Merge Editor", Precondition: isMergeEditor},
		Meta{ID: "merge.acceptAllInput2", Title: "Accept All Current Changes from Right", Category: "Merge Editor", Precondition: isMergeEditor},
		Meta{ID: "mergeEditor.resetResultToBaseAndAutoMerge", Title: "Reset Result", Category: "Merge Editor", Precondition: isMergeEditor},
		Meta{ID: "mergeEditor.acceptAllCombination", Title: "Accept All Combination", Category: "Merge Editor", Precondition: isMergeEditor},
		Meta{ID: "mergeEditor.acceptMerge", Title: "Complete Merge", Category: "Merge Editor", Precondition: isMergeEditor},
		Meta{ID: "git.openMergeEditor", Title: "Resolve in Merge Editor", Category: "Git",
			Description: "Opens the merge editor for the file with conflict markers of the active editor."},
		Meta{ID: "_open.mergeEditor"},
	)
}
