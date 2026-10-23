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
	registerEditorContext(r)
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
