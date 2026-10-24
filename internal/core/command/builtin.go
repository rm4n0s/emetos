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
// src/vs/workbench/browser/parts/titlebar/menubarControl.ts
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
	)
}

// registerEditorGroupCommands registers the commands that close, switch and keep the editors of a
// group (SPEC §10.3). The commands of the grid of groups are in registerEditorGridCommands; the
// commands that need the quick input (the editor picker, step 6.17) are not here yet.
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
		Meta{ID: "workbench.action.openNextRecentlyUsedEditorInGroup", Title: "Open Next Recently Used Editor In Group", Category: "View"},
		Meta{ID: "workbench.action.openPreviousRecentlyUsedEditorInGroup", Title: "Open Previous Recently Used Editor In Group", Category: "View"},

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
