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
	registerExplorerContext(r)
	registerQuickInputMenus(r)
	registerSettingsMenus(r)
	registerKeybindingsEditorMenus(r)
	registerThemeMenus(r)
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
// commands (opening files, 7.x), and its "Close Editor" row is enabled while an editor is open. Save and Save
// As are enabled while an editor is open, Save All while a document has unsaved changes (dirtyWorkingCopies), and
// Revert File while the active editor can revert (it is not an untitled one). Close Window and Exit (Quit) close the window and the app (step 7.2). Auto Save is a toggled row (step 7.3).
// VS Code has no "Close All Editors" row in the File menu; its command is in the palette only.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/fileActions.contribution.ts
func registerFileMenu(r *MenuRegistry) {
	r.MustAdd(
		MenuItem{Menu: MenubarFileMenu, Command: "workbench.action.files.newUntitledFile", Title: "New Text File", MnemonicTitle: "&&New Text File", Group: "1_new", Order: 1},
		MenuItem{Menu: MenubarFileMenu, Command: "workbench.action.newWindow", MnemonicTitle: "New &&Window", Group: "1_new", Order: 3},
		MenuItem{Menu: MenubarFileMenu, Command: "workbench.action.files.openFolder", MnemonicTitle: "Open &&Folder...", Group: "2_open", Order: 2},
		MenuItem{Menu: MenubarFileMenu, Submenu: MenubarRecentMenu, Title: "Open Recent", MnemonicTitle: "Open &&Recent", Group: "2_open", Order: 4},
		MenuItem{Menu: MenubarFileMenu, Command: "workbench.action.files.save", MnemonicTitle: "&&Save", Group: "4_save", Order: 1,
			Precondition: contextkey.Has("activeEditor")},
		MenuItem{Menu: MenubarFileMenu, Command: "workbench.action.files.saveAs", MnemonicTitle: "Save &&As...", Group: "4_save", Order: 2,
			Precondition: contextkey.Has("activeEditor")},
		MenuItem{Menu: MenubarFileMenu, Command: "saveAll", Title: "Save All", MnemonicTitle: "Save A&&ll", Group: "4_save", Order: 3,
			Precondition: contextkey.Has("dirtyWorkingCopies")},
		// the check mark follows files.autoSave: any mode but "off" is on
		MenuItem{Menu: MenubarFileMenu, Command: "workbench.action.toggleAutoSave", Title: "Auto Save", MnemonicTitle: "A&&uto Save", Group: "5_autosave", Order: 1,
			Toggled: when("config.files.autoSave != 'off'")},
		MenuItem{Menu: MenubarFileMenu, Command: "workbench.action.files.revert", MnemonicTitle: "Re&&vert File", Group: "6_close", Order: 1,
			Precondition: contextkey.Has("activeEditorCanRevert")},
		MenuItem{Menu: MenubarFileMenu, Command: "workbench.action.closeFolder", Title: "Close Folder", MnemonicTitle: "Close &&Folder", Group: "6_close", Order: 3,
			When: contextkey.Equals("workbenchState", "folder")},
		MenuItem{Menu: MenubarFileMenu, Command: "workbench.action.closeActiveEditor", MnemonicTitle: "&&Close Editor", Group: "6_close", Order: 2,
			Precondition: contextkey.Has("activeEditor")},
		MenuItem{Menu: MenubarFileMenu, Command: "workbench.action.closeWindow", MnemonicTitle: "Clos&&e Window", Group: "6_close", Order: 4},
		MenuItem{Menu: MenubarFileMenu, Command: "workbench.action.quit", Title: "Exit", MnemonicTitle: "E&&xit", Group: "z_Exit", Order: 1},
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
// src/vs/workbench/browser/parts/panel/panelActions.ts,
// src/vs/workbench/browser/actions/windowActions.ts, src/vs/workbench/browser/workbench.zenMode.contribution.ts,
// src/vs/workbench/electron-browser/actions/windowActions.ts and
// src/vs/workbench/contrib/codeEditor/browser/toggleWordWrap.ts
func registerViewMenu(r *MenuRegistry) {
	r.MustAdd(
		MenuItem{Menu: MenubarViewMenu, Submenu: MenubarAppearanceMenu, Title: "Appearance", MnemonicTitle: "&&Appearance", Group: "2_appearance", Order: 1},
		// the views of the panel, as viewsService.ts adds them to the View menu
		MenuItem{Menu: MenubarViewMenu, Command: "workbench.actions.view.problems", Title: "Problems", MnemonicTitle: "&&Problems", Group: "5_panel", Order: 0},
		MenuItem{Menu: MenubarViewMenu, Command: "workbench.action.output.toggleOutput", Title: "Output", MnemonicTitle: "&&Output", Group: "5_panel", Order: 1},
		MenuItem{Menu: MenubarViewMenu, Command: "editor.action.toggleWordWrap", Title: "Word Wrap", MnemonicTitle: "&&Word Wrap", Group: "6_editor", Order: 1,
			Precondition: contextkey.Has("canToggleWordWrap"), Toggled: contextkey.Has("editorWordWrap")},

		// full screen, zen mode and the centered layout, then the zoom (step 8.5)
		MenuItem{Menu: MenubarAppearanceMenu, Command: "workbench.action.toggleFullScreen", Title: "Full Screen", MnemonicTitle: "&&Full Screen", Group: "1_toggle_view", Order: 1,
			Toggled: contextkey.Has("isFullscreen")},
		MenuItem{Menu: MenubarAppearanceMenu, Command: "workbench.action.toggleZenMode", Title: "Zen Mode", MnemonicTitle: "Zen Mode", Group: "1_toggle_view", Order: 2,
			Toggled: contextkey.Has("inZenMode")},
		MenuItem{Menu: MenubarAppearanceMenu, Command: "workbench.action.toggleCenteredLayout", Title: "Centered Layout", MnemonicTitle: "&&Centered Layout", Group: "1_toggle_view", Order: 3,
			Toggled: contextkey.Has("isCenteredLayout")},
		MenuItem{Menu: MenubarAppearanceMenu, Command: "workbench.action.zoomIn", Title: "Zoom In", MnemonicTitle: "&&Zoom In", Group: "5_zoom", Order: 1},
		MenuItem{Menu: MenubarAppearanceMenu, Command: "workbench.action.zoomOut", Title: "Zoom Out", MnemonicTitle: "&&Zoom Out", Group: "5_zoom", Order: 2},
		MenuItem{Menu: MenubarAppearanceMenu, Command: "workbench.action.zoomReset", Title: "Reset Zoom", MnemonicTitle: "&&Reset Zoom", Group: "5_zoom", Order: 3},

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

// registerEditorGroupMenus adds the Editor Layout submenu of the View menu, the Switch Editor and Switch
// Group submenus of the Go menu, the history rows of the Go menu (Back, Forward, Last Edit Location) and
// Reopen Closed Editor, More... and Clear Recently Opened in the Open Recent submenu of the File menu (the
// Open Recent row itself is in the File menu). Not here: Split in Group and Join in
// Group (editors that split inside a group), Move/Copy Editor into New Window, and the first and second
// side of a side by side editor.
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

		MenuItem{Menu: MenubarGoMenu, Submenu: MenubarSwitchEditorMenu, Title: "Switch Editor", MnemonicTitle: "Switch &&Editor", Group: "2_editor_nav", Order: 1},
		MenuItem{Menu: MenubarGoMenu, Submenu: MenubarSwitchGroupMenu, Title: "Switch Group", MnemonicTitle: "Switch &&Group", Group: "2_editor_nav", Order: 2},

		// Back, Forward and Last Edit Location (step 6.19); the first side and the second side of an editor
		// are not here: Emetos has no side by side editors
		MenuItem{Menu: MenubarGoMenu, Command: "workbench.action.navigateBack", Title: "Back", MnemonicTitle: "&&Back", Group: "1_history_nav", Order: 1,
			Precondition: contextkey.Has("canNavigateBack")},
		MenuItem{Menu: MenubarGoMenu, Command: "workbench.action.navigateForward", Title: "Forward", MnemonicTitle: "&&Forward", Group: "1_history_nav", Order: 2,
			Precondition: contextkey.Has("canNavigateForward")},
		MenuItem{Menu: MenubarGoMenu, Command: "workbench.action.navigateToLastEditLocation", Title: "Last Edit Location", MnemonicTitle: "&&Last Edit Location", Group: "1_history_nav", Order: 3,
			Precondition: contextkey.Has("canNavigateToLastEditLocation")},
		MenuItem{Menu: MenubarRecentMenu, Command: "workbench.action.reopenClosedEditor", Title: "Reopen Closed Editor", MnemonicTitle: "&&Reopen Closed Editor", Group: "1_editor", Order: 1,
			Precondition: contextkey.Has("canReopenClosedEditor")},
		// the recent folders and files are put before "More..." by the window (workbench.MenuSource.Before)
		MenuItem{Menu: MenubarRecentMenu, Command: "workbench.action.openRecent", Title: "More...", MnemonicTitle: "&&More...", Group: "y_more", Order: 1},
		MenuItem{Menu: MenubarRecentMenu, Command: "workbench.action.clearRecentFiles", Title: "Clear Recently Opened...", MnemonicTitle: "&&Clear Recently Opened...", Group: "z_clear", Order: 1},

		MenuItem{Menu: MenubarSwitchEditorMenu, Command: "workbench.action.nextEditor", Title: "Next Editor", MnemonicTitle: "&&Next Editor", Group: "2_any", Order: 1},
		MenuItem{Menu: MenubarSwitchEditorMenu, Command: "workbench.action.previousEditor", Title: "Previous Editor", MnemonicTitle: "&&Previous Editor", Group: "2_any", Order: 2},
		MenuItem{Menu: MenubarSwitchEditorMenu, Command: "workbench.action.openNextRecentlyUsedEditor", Title: "Next Used Editor", MnemonicTitle: "&&Next Used Editor", Group: "3_any_used", Order: 1},
		MenuItem{Menu: MenubarSwitchEditorMenu, Command: "workbench.action.openPreviousRecentlyUsedEditor", Title: "Previous Used Editor", MnemonicTitle: "&&Previous Used Editor", Group: "3_any_used", Order: 2},
		MenuItem{Menu: MenubarSwitchEditorMenu, Command: "workbench.action.nextEditorInGroup", Title: "Next Editor in Group", MnemonicTitle: "&&Next Editor in Group", Group: "4_group", Order: 1},
		MenuItem{Menu: MenubarSwitchEditorMenu, Command: "workbench.action.previousEditorInGroup", Title: "Previous Editor in Group", MnemonicTitle: "&&Previous Editor in Group", Group: "4_group", Order: 2},
		MenuItem{Menu: MenubarSwitchEditorMenu, Command: "workbench.action.openNextRecentlyUsedEditorInGroup", Title: "Next Used Editor in Group", MnemonicTitle: "&&Next Used Editor in Group", Group: "5_group_used", Order: 1},
		MenuItem{Menu: MenubarSwitchEditorMenu, Command: "workbench.action.openPreviousRecentlyUsedEditorInGroup", Title: "Previous Used Editor in Group", MnemonicTitle: "&&Previous Used Editor in Group", Group: "5_group_used", Order: 2},

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
// Save and Save As are in the menu while the tab is dirty, as VS Code's. Not here yet: Reveal in File Manager (step 7.x), Reopen Editor With, Share, the moves into a new window and Split in Group.
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

		MenuItem{Menu: EditorTitleContext, Command: "workbench.action.files.save", Title: "Save", Group: "1_close_save", Order: 10,
			When: contextkey.Has("activeEditorIsDirty")},
		MenuItem{Menu: EditorTitleContext, Command: "workbench.action.files.saveAs", Title: "Save As...", Group: "1_close_save", Order: 20,
			When: contextkey.Has("activeEditorIsDirty")},

		MenuItem{Menu: EditorTitleContext, Command: "copyFilePath", Title: "Copy Path", Group: "1_cutcopypaste", When: contextkey.Has("isFileSystemResource")},
		MenuItem{Menu: EditorTitleContext, Command: "copyRelativeFilePath", Title: "Copy Relative Path", Group: "1_cutcopypaste", When: contextkey.Has("isFileSystemResource")},

		MenuItem{Menu: EditorTitleContext, Command: "revealInExplorer", Title: "Reveal in Explorer View", Group: "2_files", Order: 1,
			When: contextkey.Has("isFileSystemResource")},

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

// registerExplorerContext adds the context menu of the Explorer (SPEC §10.5), in VS Code's groups and
// order. The "Delete" row has the "Delete Permanently" row as its alternative, which VS Code shows while
// Shift is held; here the permanent delete shows where the trash cannot be used, and has the key
// Shift+Delete. Not here: the rows for a root folder of a workspace, Open With, the comparisons, Find in
// Folder, the terminal and the upload and download rows of the web.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/fileActions.contribution.ts and
// src/vs/workbench/contrib/files/electron-browser/fileActions.contribution.ts
func registerExplorerContext(r *MenuRegistry) {
	r.MustAdd(
		MenuItem{Menu: ExplorerContext, Command: "explorer.newFile", Title: "New File...", Group: "navigation", Order: 4,
			When: contextkey.Has("explorerResourceIsFolder"), Precondition: contextkey.Not("explorerResourceReadonly")},
		MenuItem{Menu: ExplorerContext, Command: "explorer.newFolder", Title: "New Folder...", Group: "navigation", Order: 6,
			When: contextkey.Has("explorerResourceIsFolder"), Precondition: contextkey.Not("explorerResourceReadonly")},
		MenuItem{Menu: ExplorerContext, Command: "explorer.openToSide", Title: "Open to the Side", Group: "navigation", Order: 10,
			When: contextkey.And(contextkey.Not("explorerResourceIsFolder"), contextkey.Has("resourceScheme"))},
		MenuItem{Menu: ExplorerContext, Command: "revealFileInOS", Title: "Open Containing Folder", Group: "navigation", Order: 20,
			When: contextkey.Equals("resourceScheme", "file")},

		MenuItem{Menu: ExplorerContext, Command: "filesExplorer.cut", Title: "Cut", Group: "5_cutcopypaste", Order: 8,
			When: when("!explorerResourceIsRoot && !explorerResourceReadonly")},
		MenuItem{Menu: ExplorerContext, Command: "filesExplorer.copy", Title: "Copy", Group: "5_cutcopypaste", Order: 10,
			When: contextkey.Not("explorerResourceIsRoot")},
		MenuItem{Menu: ExplorerContext, Command: "filesExplorer.paste", Title: "Paste", Group: "5_cutcopypaste", Order: 20,
			When: contextkey.Has("explorerResourceIsFolder"), Precondition: when("!explorerResourceReadonly && fileCopied")},

		MenuItem{Menu: ExplorerContext, Command: "copyFilePath", Title: "Copy Path", Group: "6_copypath", Order: 10,
			When: contextkey.Has("isFileSystemResource")},
		MenuItem{Menu: ExplorerContext, Command: "copyRelativeFilePath", Title: "Copy Relative Path", Group: "6_copypath", Order: 20,
			When: contextkey.Has("isFileSystemResource")},

		MenuItem{Menu: ExplorerContext, Command: "renameFile", Title: "Rename...", Group: "7_modification", Order: 10,
			When: contextkey.Not("explorerResourceIsRoot"), Precondition: contextkey.Not("explorerResourceReadonly")},
		MenuItem{Menu: ExplorerContext, Command: "moveFileToTrash", Title: "Delete", Group: "7_modification", Order: 20,
			When: when("!explorerResourceIsRoot && explorerResourceMoveableToTrash")},
		MenuItem{Menu: ExplorerContext, Command: "deleteFile", Title: "Delete Permanently", Group: "7_modification", Order: 20,
			When: when("!explorerResourceIsRoot && !explorerResourceMoveableToTrash")},
	)
}

// registerQuickInputMenus adds the rows of the quick input: Command Palette and Open View in the View
// menu, Go to Line/Column in the Go menu, Command Palette in the Manage menu, and the commands the
// palette leaves out (the CommandPalette menu: a row whose When does not hold hides its command).
// Go to File... is in the Go menu (step 6.18).
//
// Ported from VS Code: src/vs/workbench/contrib/quickaccess/browser/quickAccess.contribution.ts,
// src/vs/workbench/contrib/files/browser/fileActions.contribution.ts (miGotoFile) and
// src/vs/workbench/contrib/quickaccess/browser/viewQuickAccess.ts (f1: false)
func registerQuickInputMenus(r *MenuRegistry) {
	r.MustAdd(
		MenuItem{Menu: MenubarViewMenu, Command: "workbench.action.showCommands", Title: "Command Palette...", MnemonicTitle: "&&Command Palette...", Group: "1_open", Order: 1},
		MenuItem{Menu: MenubarViewMenu, Command: "workbench.action.openView", Title: "Open View...", MnemonicTitle: "&&Open View...", Group: "1_open", Order: 2},
		MenuItem{Menu: MenubarGoMenu, Command: "workbench.action.quickOpen", MnemonicTitle: "Go to &&File...", Group: "3_global_nav", Order: 1},
		MenuItem{Menu: MenubarGoMenu, Command: "workbench.action.gotoLine", MnemonicTitle: "Go to &&Line/Column...", Group: "5_infile_nav", Order: 1},
		MenuItem{Menu: GlobalActivity, Command: "workbench.action.showCommands", Title: "Command Palette...", Group: "1_command", Order: 1},

		MenuItem{Menu: CommandPalette, Command: "workbench.action.quickOpenView", When: contextkey.False()},
	)
}

// registerSettingsMenus adds the rows of the settings editor (step 8.1): the Preferences submenu of the File menu
// with Settings (Ctrl+,), the same row in the Manage menu, and the palette rules: "Settings" is left out of the
// palette (VS Code lists openSettings2 instead) and Open Workspace Settings needs a folder. Step 8.2 adds the same
// rule for Open Workspace Settings (JSON); the other JSON commands are in the palette only, as in VS Code.
//
// Ported from VS Code: src/vs/workbench/contrib/preferences/browser/preferences.contribution.ts
func registerSettingsMenus(r *MenuRegistry) {
	r.MustAdd(
		MenuItem{Menu: MenubarFileMenu, Submenu: MenubarPreferencesMenu, Title: "Preferences", MnemonicTitle: "&&Preferences", Group: "5_autosave", Order: 2},
		MenuItem{Menu: MenubarPreferencesMenu, Command: "workbench.action.openSettings", Title: "Settings", MnemonicTitle: "&&Settings", Group: "2_configuration", Order: 2},
		MenuItem{Menu: GlobalActivity, Command: "workbench.action.openSettings", Title: "Settings", Group: "2_configuration", Order: 2},

		MenuItem{Menu: CommandPalette, Command: "workbench.action.openSettings", When: contextkey.False()},
		MenuItem{Menu: CommandPalette, Command: "workbench.action.openWorkspaceSettings", When: when("workbenchState != 'empty'")},
		MenuItem{Menu: CommandPalette, Command: "workbench.action.openWorkspaceSettingsFile", When: when("workbenchState != 'empty'")},
	)
}

// registerKeybindingsEditorMenus adds the rows of the keybindings editor (step 8.3): Keyboard Shortcuts (Ctrl+K
// Ctrl+S) in the Preferences submenu of the File menu and in the Manage menu, and the palette rule that hides
// Clear Search Results. The palette lists Open Keyboard Shortcuts and the two JSON commands.
//
// Ported from VS Code: src/vs/workbench/contrib/preferences/browser/preferences.contribution.ts
func registerKeybindingsEditorMenus(r *MenuRegistry) {
	r.MustAdd(
		MenuItem{Menu: MenubarPreferencesMenu, Command: "workbench.action.openGlobalKeybindings", Title: "Keyboard Shortcuts", Group: "2_configuration", Order: 4},
		MenuItem{Menu: GlobalActivity, Command: "workbench.action.openGlobalKeybindings", Title: "Keyboard Shortcuts", Group: "2_configuration", Order: 4},

		MenuItem{Menu: CommandPalette, Command: "keybindings.editor.clearSearchResults", When: contextkey.False()},
	)
}

// registerThemeMenus adds the Themes submenu (step 8.4) to the Preferences submenu of the File menu and to the Manage
// menu, with Color Theme and File Icon Theme. VS Code's third row, Product Icon Theme, is not built.
//
// Ported from VS Code: src/vs/workbench/contrib/themes/browser/themes.contribution.ts
func registerThemeMenus(r *MenuRegistry) {
	r.MustAdd(
		MenuItem{Menu: GlobalActivity, Submenu: ThemesSubMenu, Title: "Themes", Group: "2_configuration", Order: 7},
		MenuItem{Menu: MenubarPreferencesMenu, Submenu: ThemesSubMenu, Title: "Themes", MnemonicTitle: "&&Themes", Group: "2_configuration", Order: 7},
		MenuItem{Menu: ThemesSubMenu, Command: "workbench.action.selectTheme", Title: "Color Theme", Order: 1},
		MenuItem{Menu: ThemesSubMenu, Command: "workbench.action.selectIconTheme", Title: "File Icon Theme", Order: 2},
	)
}
