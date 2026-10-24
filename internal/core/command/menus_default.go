package command

import (
	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// DefaultMenus makes a registry of menus over a registry of commands, with the items of the menus of
// the window (SPEC §10.4, §11.2). Each step that adds a command VS Code puts in a menu adds its row
// here, in the group and the order VS Code uses; a row for a command that does not exist yet is not
// added (the menus skip an item whose command is not registered, but the tests check the table).
//
// The menu bar shows a menu only when it has an item, so File, Terminal and Help are not in the bar
// until the commands of their rows exist.
func DefaultMenus(commands *Registry) *MenuRegistry {
	r := NewMenuRegistry(commands)
	registerMainMenu(r)
	registerFileMenu(r)
	registerEditMenu(r)
	registerSelectionMenu(r)
	registerViewMenu(r)
	registerGoMenu(r)
	registerEditorGroupMenus(r)
	registerEditorContext(r)
	registerEditorTitleContext(r)
	return r
}

// when reads a when-clause of the rows below; a clause that does not parse is a mistake in this file.
func when(clause string) contextkey.Expr {
	e, err := contextkey.Parse(clause)
	if err != nil {
		panic(err)
	}
	return e
}

// registerMainMenu adds the menus of the menu bar, in its order.
//
// Ported from VS Code: src/vs/workbench/browser/parts/titlebar/menubar.contribution.ts
func registerMainMenu(r *MenuRegistry) {
	for _, m := range []struct {
		submenu, title, mnemonic string
		order                    float64
	}{
		{MenubarFileMenu, "File", "&&File", 1},
		{MenubarEditMenu, "Edit", "&&Edit", 2},
		{MenubarSelectionMenu, "Selection", "&&Selection", 3},
		{MenubarViewMenu, "View", "&&View", 4},
		{MenubarGoMenu, "Go", "&&Go", 5},
		{MenubarTerminalMenu, "Terminal", "&&Terminal", 7},
		{MenubarHelpMenu, "Help", "&&Help", 8},
	} {
		r.MustAdd(MenuItem{Menu: MenubarMainMenu, Submenu: m.submenu, Title: m.title, MnemonicTitle: m.mnemonic, Order: m.order})
	}
}

// registerFileMenu adds the File menu. VS Code's other rows belong to the steps that add their
// commands (open and save in 7.1), and its "Close Editor" row is enabled while an editor is open.
// VS Code has no "Close All Editors" row in the File menu; its command is in the palette only.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/fileActions.contribution.ts
func registerFileMenu(r *MenuRegistry) {
	r.MustAdd(
		MenuItem{Menu: MenubarFileMenu, Command: "workbench.action.closeActiveEditor", MnemonicTitle: "&&Close Editor", Group: "6_close", Order: 2,
			Precondition: contextkey.Has("activeEditor")},
	)
}

// registerEditMenu adds the Edit menu.
//
// Ported from VS Code: src/vs/editor/browser/editorExtensions.ts (Undo, Redo),
// src/vs/editor/contrib/clipboard/browser/clipboard.ts, src/vs/editor/contrib/find/browser/findController.ts,
// src/vs/editor/contrib/comment/browser/comment.ts and
// src/vs/workbench/contrib/emmet/browser/actions/expandAbbreviation.ts
func registerEditMenu(r *MenuRegistry) {
	r.MustAdd(
		MenuItem{Menu: MenubarEditMenu, Command: "undo", MnemonicTitle: "&&Undo", Group: "1_do", Order: 1},
		MenuItem{Menu: MenubarEditMenu, Command: "redo", MnemonicTitle: "&&Redo", Group: "1_do", Order: 2},

		MenuItem{Menu: MenubarEditMenu, Command: "editor.action.clipboardCutAction", MnemonicTitle: "Cu&&t", Group: "2_ccp", Order: 1},
		MenuItem{Menu: MenubarEditMenu, Command: "editor.action.clipboardCopyAction", MnemonicTitle: "&&Copy", Group: "2_ccp", Order: 2},
		MenuItem{Menu: MenubarEditMenu, Command: "editor.action.clipboardPasteAction", MnemonicTitle: "&&Paste", Group: "2_ccp", Order: 4},

		MenuItem{Menu: MenubarEditMenu, Command: "actions.find", MnemonicTitle: "&&Find", Group: "3_find", Order: 1},
		MenuItem{Menu: MenubarEditMenu, Command: "editor.action.startFindReplaceAction", MnemonicTitle: "&&Replace", Group: "3_find", Order: 2},

		MenuItem{Menu: MenubarEditMenu, Command: "editor.action.commentLine", MnemonicTitle: "&&Toggle Line Comment", Group: "5_insert", Order: 1},
		MenuItem{Menu: MenubarEditMenu, Command: "editor.action.blockComment", MnemonicTitle: "Toggle &&Block Comment", Group: "5_insert", Order: 2},
		MenuItem{Menu: MenubarEditMenu, Command: "editor.emmet.action.expandAbbreviation", MnemonicTitle: "Emmet: E&&xpand Abbreviation", Group: "5_insert", Order: 3},
	)
}

// registerSelectionMenu adds the Selection menu.
//
// Ported from VS Code: src/vs/editor/browser/editorExtensions.ts (Select All),
// src/vs/editor/contrib/smartSelect/browser/smartSelect.ts,
// src/vs/editor/contrib/linesOperations/browser/linesOperations.ts and
// src/vs/editor/contrib/multicursor/browser/multicursor.ts
func registerSelectionMenu(r *MenuRegistry) {
	r.MustAdd(
		MenuItem{Menu: MenubarSelectionMenu, Command: "editor.action.selectAll", MnemonicTitle: "&&Select All", Group: "1_basic", Order: 1},
		MenuItem{Menu: MenubarSelectionMenu, Command: "editor.action.smartSelect.expand", MnemonicTitle: "&&Expand Selection", Group: "1_basic", Order: 2},
		MenuItem{Menu: MenubarSelectionMenu, Command: "editor.action.smartSelect.shrink", MnemonicTitle: "&&Shrink Selection", Group: "1_basic", Order: 3},

		MenuItem{Menu: MenubarSelectionMenu, Command: "editor.action.copyLinesUpAction", MnemonicTitle: "&&Copy Line Up", Group: "2_line", Order: 1},
		MenuItem{Menu: MenubarSelectionMenu, Command: "editor.action.copyLinesDownAction", MnemonicTitle: "Co&&py Line Down", Group: "2_line", Order: 2},
		MenuItem{Menu: MenubarSelectionMenu, Command: "editor.action.moveLinesUpAction", MnemonicTitle: "Mo&&ve Line Up", Group: "2_line", Order: 3},
		MenuItem{Menu: MenubarSelectionMenu, Command: "editor.action.moveLinesDownAction", MnemonicTitle: "Move &&Line Down", Group: "2_line", Order: 4},

		MenuItem{Menu: MenubarSelectionMenu, Command: "editor.action.insertCursorAbove", MnemonicTitle: "&&Add Cursor Above", Group: "3_multi", Order: 2},
		MenuItem{Menu: MenubarSelectionMenu, Command: "editor.action.insertCursorBelow", MnemonicTitle: "A&&dd Cursor Below", Group: "3_multi", Order: 3},
		MenuItem{Menu: MenubarSelectionMenu, Command: "editor.action.insertCursorAtEndOfEachLineSelected", MnemonicTitle: "Add C&&ursors to Line Ends", Group: "3_multi", Order: 4},
		MenuItem{Menu: MenubarSelectionMenu, Command: "editor.action.addSelectionToNextFindMatch", Title: "Add Next Occurrence", MnemonicTitle: "Add &&Next Occurrence", Group: "3_multi", Order: 5},
		MenuItem{Menu: MenubarSelectionMenu, Command: "editor.action.addSelectionToPreviousFindMatch", Title: "Add Previous Occurrence", MnemonicTitle: "Add P&&revious Occurrence", Group: "3_multi", Order: 6},
		MenuItem{Menu: MenubarSelectionMenu, Command: "editor.action.selectHighlights", Title: "Select All Occurrences", MnemonicTitle: "Select All &&Occurrences", Group: "3_multi", Order: 7},
	)
}

// registerViewMenu adds the View menu and its Appearance submenu.
//
// Ported from VS Code: src/vs/workbench/browser/actions/layoutActions.ts,
// src/vs/workbench/browser/parts/panel/panelActions.ts and
// src/vs/workbench/contrib/codeEditor/browser/toggleWordWrap.ts
func registerViewMenu(r *MenuRegistry) {
	r.MustAdd(
		MenuItem{Menu: MenubarViewMenu, Submenu: MenubarAppearanceMenu, Title: "Appearance", MnemonicTitle: "&&Appearance", Group: "2_appearance", Order: 1},
		MenuItem{Menu: MenubarViewMenu, Command: "editor.action.toggleWordWrap", Title: "Word Wrap", MnemonicTitle: "&&Word Wrap", Group: "6_editor", Order: 1,
			Precondition: contextkey.Has("canToggleWordWrap"), Toggled: contextkey.Has("editorWordWrap")},

		// Menu Bar is toggled while the bar is a custom menu, which it always is in Emetos, unless the
		// setting hides it or shows it on the Alt key
		MenuItem{Menu: MenubarAppearanceMenu, Command: "workbench.action.toggleMenuBar", Title: "Menu Bar", MnemonicTitle: "Menu &&Bar", Group: "2_workbench_layout", Order: 0,
			Toggled: when("config.window.menuBarVisibility == 'classic' || config.window.menuBarVisibility == 'visible'")},
		MenuItem{Menu: MenubarAppearanceMenu, Command: "workbench.action.toggleSidebarVisibility", Group: "2_workbench_layout", Order: 1,
			ToggledTitle: "Primary Side Bar", ToggledMnemonicTitle: "&&Primary Side Bar", Toggled: contextkey.Has("sideBarVisible")},
		MenuItem{Menu: MenubarAppearanceMenu, Command: "workbench.action.toggleStatusbarVisibility", Title: "Status Bar", MnemonicTitle: "S&&tatus Bar", Group: "2_workbench_layout", Order: 3,
			Toggled: when("config.workbench.statusBar.visible == true")},
		MenuItem{Menu: MenubarAppearanceMenu, Command: "workbench.action.togglePanel", Group: "2_workbench_layout", Order: 5,
			ToggledTitle: "Hide Panel", ToggledMnemonicTitle: "&&Panel", Toggled: contextkey.Has("panelVisible")},
		MenuItem{Menu: MenubarAppearanceMenu, Submenu: PanelPositionMenu, Title: "Panel Position", Group: "3_workbench_layout_move", Order: 4},

		MenuItem{Menu: PanelPositionMenu, Command: "workbench.action.positionPanelTop", Title: "Top", Order: 5, Toggled: contextkey.Equals("panelPosition", "top")},
		MenuItem{Menu: PanelPositionMenu, Command: "workbench.action.positionPanelLeft", Title: "Left", Order: 6, Toggled: contextkey.Equals("panelPosition", "left")},
		MenuItem{Menu: PanelPositionMenu, Command: "workbench.action.positionPanelRight", Title: "Right", Order: 7, Toggled: contextkey.Equals("panelPosition", "right")},
		MenuItem{Menu: PanelPositionMenu, Command: "workbench.action.positionPanelBottom", Title: "Bottom", Order: 8, Toggled: contextkey.Equals("panelPosition", "bottom")},
	)
}

// registerGoMenu adds the Go menu.
//
// Ported from VS Code: src/vs/editor/contrib/bracketMatching/browser/bracketMatching.ts
func registerGoMenu(r *MenuRegistry) {
	r.MustAdd(
		MenuItem{Menu: MenubarGoMenu, Command: "editor.action.jumpToBracket", MnemonicTitle: "Go to &&Bracket", Group: "5_infile_nav", Order: 2},
	)
}

// registerEditorContext adds the context menu of the editor.
//
// Select All is not in the context menu of VS Code's editor (its simple editors have it, in the group
// 9_select); Emetos has it below Paste.
//
// Ported from VS Code: src/vs/editor/contrib/clipboard/browser/clipboard.ts and
// src/vs/editor/browser/editorExtensions.ts (Select All)
func registerEditorContext(r *MenuRegistry) {
	writable := contextkey.Not("editorReadonly")
	r.MustAdd(
		MenuItem{Menu: EditorContext, Command: "editor.action.clipboardCutAction", Group: "9_cutcopypaste", Order: 1, When: writable},
		MenuItem{Menu: EditorContext, Command: "editor.action.clipboardCopyAction", Group: "9_cutcopypaste", Order: 2},
		MenuItem{Menu: EditorContext, Command: "editor.action.clipboardPasteAction", Group: "9_cutcopypaste", Order: 4, When: writable},
		MenuItem{Menu: EditorContext, Command: "editor.action.selectAll", Group: "9_select", Order: 1},
	)
}

// registerEditorGroupMenus adds the Editor Layout submenu of the View menu and the Switch Group
// submenu of the Go menu. Not here: Split in Group and Join in Group (editors that split inside a
// group), Move/Copy Editor into New Window, and the Switch Editor submenu (step 6.17).
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/editor.contribution.ts
func registerEditorGroupMenus(r *MenuRegistry) {
	multiple := contextkey.Has("multipleEditorGroups")
	r.MustAdd(
		MenuItem{Menu: MenubarViewMenu, Submenu: MenubarLayoutMenu, Title: "Editor Layout", MnemonicTitle: "Editor &&Layout", Group: "2_appearance", Order: 2},

		MenuItem{Menu: MenubarLayoutMenu, Command: "workbench.action.splitEditorUp", Title: "Split Up", MnemonicTitle: "Split &&Up", Group: "1_split", Order: 1},
		MenuItem{Menu: MenubarLayoutMenu, Command: "workbench.action.splitEditorDown", Title: "Split Down", MnemonicTitle: "Split &&Down", Group: "1_split", Order: 2},
		MenuItem{Menu: MenubarLayoutMenu, Command: "workbench.action.splitEditorLeft", Title: "Split Left", MnemonicTitle: "Split &&Left", Group: "1_split", Order: 3},
		MenuItem{Menu: MenubarLayoutMenu, Command: "workbench.action.splitEditorRight", Title: "Split Right", MnemonicTitle: "Split &&Right", Group: "1_split", Order: 4},

		MenuItem{Menu: MenubarLayoutMenu, Command: "workbench.action.editorLayoutSingle", Title: "Single", MnemonicTitle: "&&Single", Group: "4_layouts", Order: 1},
		MenuItem{Menu: MenubarLayoutMenu, Command: "workbench.action.editorLayoutTwoColumns", Title: "Two Columns", MnemonicTitle: "&&Two Columns", Group: "4_layouts", Order: 3},
		MenuItem{Menu: MenubarLayoutMenu, Command: "workbench.action.editorLayoutThreeColumns", Title: "Three Columns", MnemonicTitle: "T&&hree Columns", Group: "4_layouts", Order: 4},
		MenuItem{Menu: MenubarLayoutMenu, Command: "workbench.action.editorLayoutTwoRows", Title: "Two Rows", MnemonicTitle: "T&&wo Rows", Group: "4_layouts", Order: 5},
		MenuItem{Menu: MenubarLayoutMenu, Command: "workbench.action.editorLayoutThreeRows", Title: "Three Rows", MnemonicTitle: "Three &&Rows", Group: "4_layouts", Order: 6},
		MenuItem{Menu: MenubarLayoutMenu, Command: "workbench.action.editorLayoutTwoByTwoGrid", Title: "Grid (2x2)", MnemonicTitle: "&&Grid (2x2)", Group: "4_layouts", Order: 7},
		MenuItem{Menu: MenubarLayoutMenu, Command: "workbench.action.editorLayoutTwoRowsRight", Title: "Two Rows Right", MnemonicTitle: "Two R&&ows Right", Group: "4_layouts", Order: 8},
		MenuItem{Menu: MenubarLayoutMenu, Command: "workbench.action.editorLayoutTwoColumnsBottom", Title: "Two Columns Bottom", MnemonicTitle: "Two &&Columns Bottom", Group: "4_layouts", Order: 9},

		MenuItem{Menu: MenubarGoMenu, Submenu: MenubarSwitchGroupMenu, Title: "Switch Group", MnemonicTitle: "Switch &&Group", Group: "2_editor_nav", Order: 2},

		MenuItem{Menu: MenubarSwitchGroupMenu, Command: "workbench.action.focusFirstEditorGroup", Title: "Group 1", MnemonicTitle: "Group &&1", Group: "1_focus_index", Order: 1},
		MenuItem{Menu: MenubarSwitchGroupMenu, Command: "workbench.action.focusSecondEditorGroup", Title: "Group 2", MnemonicTitle: "Group &&2", Group: "1_focus_index", Order: 2},
		MenuItem{Menu: MenubarSwitchGroupMenu, Command: "workbench.action.focusThirdEditorGroup", Title: "Group 3", MnemonicTitle: "Group &&3", Group: "1_focus_index", Order: 3, Precondition: multiple},
		MenuItem{Menu: MenubarSwitchGroupMenu, Command: "workbench.action.focusFourthEditorGroup", Title: "Group 4", MnemonicTitle: "Group &&4", Group: "1_focus_index", Order: 4, Precondition: multiple},
		MenuItem{Menu: MenubarSwitchGroupMenu, Command: "workbench.action.focusFifthEditorGroup", Title: "Group 5", MnemonicTitle: "Group &&5", Group: "1_focus_index", Order: 5, Precondition: multiple},
		MenuItem{Menu: MenubarSwitchGroupMenu, Command: "workbench.action.focusNextGroup", Title: "Next Group", MnemonicTitle: "&&Next Group", Group: "2_next_prev", Order: 1, Precondition: multiple},
		MenuItem{Menu: MenubarSwitchGroupMenu, Command: "workbench.action.focusPreviousGroup", Title: "Previous Group", MnemonicTitle: "&&Previous Group", Group: "2_next_prev", Order: 2, Precondition: multiple},
		MenuItem{Menu: MenubarSwitchGroupMenu, Command: "workbench.action.focusLeftGroup", Title: "Group Left", MnemonicTitle: "Group &&Left", Group: "3_directional", Order: 1, Precondition: multiple},
		MenuItem{Menu: MenubarSwitchGroupMenu, Command: "workbench.action.focusRightGroup", Title: "Group Right", MnemonicTitle: "Group &&Right", Group: "3_directional", Order: 2, Precondition: multiple},
		MenuItem{Menu: MenubarSwitchGroupMenu, Command: "workbench.action.focusAboveGroup", Title: "Group Above", MnemonicTitle: "Group &&Above", Group: "3_directional", Order: 3, Precondition: multiple},
		MenuItem{Menu: MenubarSwitchGroupMenu, Command: "workbench.action.focusBelowGroup", Title: "Group Below", MnemonicTitle: "Group &&Below", Group: "3_directional", Order: 4, Precondition: multiple},
	)
}

// registerEditorTitleContext adds the context menu of a tab and its Split & Move submenu. The
// when-clauses and preconditions read the keys of the editor that was clicked (`activeEditorIsPinned`
// and the rest of the keys of the active editor, set to those of the tab for the menu).
//
// Not here yet: Save and Save As (step 7.1), Reveal in Explorer View and Reveal in File Manager (steps
// 6.14 and 7.x), Reopen Editor With, Share, the moves into a new window and Split in Group.
//
// TODO(step 6.14): Reveal in Explorer View (group 2_files, order 1, when isFileSystemResource) once the
// explorer has a command that reveals a file.
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/editor.contribution.ts and
// src/vs/workbench/contrib/files/browser/fileActions.contribution.ts (appendEditorTitleContextMenuItem)
func registerEditorTitleContext(r *MenuRegistry) {
	r.MustAdd(
		MenuItem{Menu: EditorTitleContext, Command: "workbench.action.closeActiveEditor", Title: "Close", Group: "1_close", Order: 10},
		MenuItem{Menu: EditorTitleContext, Command: "workbench.action.closeOtherEditors", Title: "Close Others", Group: "1_close", Order: 20,
			Precondition: when("groupEditorsCount != 1")},
		MenuItem{Menu: EditorTitleContext, Command: "workbench.action.closeEditorsToTheRight", Title: "Close to the Right", Group: "1_close", Order: 30,
			Precondition: when("!activeEditorIsLastInGroup"), When: contextkey.Has("editorTabsVisible")},
		MenuItem{Menu: EditorTitleContext, Command: "workbench.action.closeUnmodifiedEditors", Title: "Close Saved", Group: "1_close", Order: 40},
		MenuItem{Menu: EditorTitleContext, Command: "workbench.action.closeEditorsInGroup", Title: "Close All", Group: "1_close", Order: 50},

		MenuItem{Menu: EditorTitleContext, Command: "copyFilePath", Title: "Copy Path", Group: "1_cutcopypaste", When: contextkey.Has("isFileSystemResource")},
		MenuItem{Menu: EditorTitleContext, Command: "copyRelativeFilePath", Title: "Copy Relative Path", Group: "1_cutcopypaste", When: contextkey.Has("isFileSystemResource")},

		MenuItem{Menu: EditorTitleContext, Command: "workbench.action.keepEditor", Title: "Keep Open", Group: "3_preview", Order: 10,
			Precondition: when("!activeEditorIsNotPreview"), When: contextkey.Has("config.workbench.editor.enablePreview")},
		MenuItem{Menu: EditorTitleContext, Command: "workbench.action.pinEditor", Title: "Pin", Group: "3_preview", Order: 20, When: when("!activeEditorIsPinned")},
		MenuItem{Menu: EditorTitleContext, Command: "workbench.action.unpinEditor", Title: "Unpin", Group: "3_preview", Order: 20, When: contextkey.Has("activeEditorIsPinned")},

		MenuItem{Menu: EditorTitleContext, Command: "workbench.action.splitEditor", Title: "Split Right", Group: "5_split", Order: 10, When: when("!splitEditorsVertically")},
		MenuItem{Menu: EditorTitleContext, Command: "workbench.action.splitEditor", Title: "Split Down", Group: "5_split", Order: 10, When: contextkey.Has("splitEditorsVertically")},
		MenuItem{Menu: EditorTitleContext, Submenu: EditorSplitMoveSubmenu, Title: "Split & Move", Group: "5_split", Order: 15},

		MenuItem{Menu: EditorSplitMoveSubmenu, Command: "workbench.action.splitEditorUp", Title: "Split Up", Group: "1_split", Order: 10},
		MenuItem{Menu: EditorSplitMoveSubmenu, Command: "workbench.action.splitEditorDown", Title: "Split Down", Group: "1_split", Order: 20},
		MenuItem{Menu: EditorSplitMoveSubmenu, Command: "workbench.action.splitEditorLeft", Title: "Split Left", Group: "1_split", Order: 30},
		MenuItem{Menu: EditorSplitMoveSubmenu, Command: "workbench.action.splitEditorRight", Title: "Split Right", Group: "1_split", Order: 40},
		MenuItem{Menu: EditorSplitMoveSubmenu, Command: "workbench.action.moveEditorToAboveGroup", Title: "Move Above", Group: "2_move", Order: 10},
		MenuItem{Menu: EditorSplitMoveSubmenu, Command: "workbench.action.moveEditorToBelowGroup", Title: "Move Below", Group: "2_move", Order: 20},
		MenuItem{Menu: EditorSplitMoveSubmenu, Command: "workbench.action.moveEditorToLeftGroup", Title: "Move Left", Group: "2_move", Order: 30},
		MenuItem{Menu: EditorSplitMoveSubmenu, Command: "workbench.action.moveEditorToRightGroup", Title: "Move Right", Group: "2_move", Order: 40},
	)
}
