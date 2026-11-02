package command

import (
	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// The Debug Console (SPEC §10.31, step 25.6): the ids of its container and view, its commands and the items of its menus. The handlers
// are bound by the shell of each screen while its panel is on screen (internal/gui/app/debugconsole.go).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/repl.ts, debug.contribution.ts (the registration of the repl panel),
// debugEditorActions.ts (SelectionToReplAction), debugCommands.ts (FOCUS_REPL_ID) and debugViewlet.ts (debug.toggleReplIgnoreFocus)

// The ids of the container and the view of the Debug Console.
const (
	// ViewContainerRepl is the Debug Console tab of the panel (VS Code's DEBUG_PANEL_ID) and ViewRepl its view (REPL_VIEW_ID).
	ViewContainerRepl = "workbench.panel.repl"
	ViewRepl          = "workbench.panel.repl.view"
)

// DebugConsoleContext is the context menu of the Debug Console.
const DebugConsoleContext = "DebugConsoleContext"

// The ids of the commands of the Debug Console.
const (
	// DebugToggleRepl shows or hides the Debug Console (Ctrl+Shift+Y) and DebugFocusRepl shows it and gives it the keyboard.
	DebugToggleRepl = "workbench.debug.action.toggleRepl"
	DebugFocusRepl  = "workbench.debug.action.focusRepl"
	// DebugClearRepl is Clear Console.
	DebugClearRepl = "workbench.debug.panel.action.clearReplAction"
	// DebugReplAcceptInput evaluates what is in the input, DebugReplFocusFilter moves to the filter box.
	DebugReplAcceptInput = "repl.action.acceptInput"
	DebugReplFocusFilter = "repl.action.filter"
	// DebugReplCopyAll and DebugReplCopyAllItem copy the lines of the console that show; DebugReplCopy copies the line under the
	// pointer, DebugReplPaste pastes into the input, DebugReplCollapseAll closes the groups and objects.
	DebugReplCopyAll     = "repl.action.copyAll"
	DebugReplCopyAllItem = "workbench.debug.action.copyAll"
	DebugReplCopy        = "debug.replCopy"
	DebugReplPaste       = "debug.replPaste"
	DebugReplCollapseAll = "debug.collapseRepl"
	DebugSelectRepl      = "workbench.action.debug.selectRepl"
	DebugSelectionToRepl = "editor.debug.action.selectionToRepl"
)

// registerDebugConsoleCommands registers the commands of the Debug Console.
func registerDebugConsoleCommands(r *Registry) {
	inRepl := contextkey.Has("inDebugRepl")
	r.MustRegister(
		Meta{ID: DebugToggleRepl, Title: "Toggle Debug Console", Category: "View"},
		Meta{ID: DebugFocusRepl, Title: "Focus on Debug Console View", Category: debugCategory},
		Meta{ID: DebugClearRepl, Title: "Clear Console", Icon: "clear-all", Description: "Clears all program output from your debug REPL"},
		Meta{ID: DebugReplAcceptInput, Title: "Debug Console: Accept Input", Precondition: inRepl},
		Meta{ID: DebugReplFocusFilter, Title: "Debug Console: Focus Filter", Precondition: inRepl},
		Meta{ID: DebugReplCopyAll, Title: "Debug: Console Copy All", Precondition: inRepl},
		Meta{ID: DebugReplCopyAllItem, Title: "Copy All"},
		Meta{ID: DebugReplCopy, Title: "Copy"},
		Meta{ID: DebugReplPaste, Title: "Paste", Precondition: contextkey.NotEquals("debugState", "inactive")},
		Meta{ID: DebugReplCollapseAll, Title: "Collapse All"},
		Meta{ID: DebugSelectRepl, Title: "Select Debug Console"},
		Meta{ID: DebugSelectionToRepl, Title: "Evaluate in Debug Console", Category: debugCategory},
	)
}

// registerDebugConsoleMenus adds the Debug Console to the View menu, the command palette, the title of Run and Debug and the context
// menus of the console and of the editor.
func registerDebugConsoleMenus(r *MenuRegistry) {
	inDebug := contextkey.Has("inDebugMode")
	r.MustAdd(
		// the command palette: what the user can run from anywhere; the rest works on the console under the pointer or in its input
		MenuItem{Menu: CommandPalette, Command: DebugFocusRepl, When: contextkey.Has("debuggersAvailable")},
		MenuItem{Menu: CommandPalette, Command: DebugClearRepl, When: contextkey.Has("debuggersAvailable")},
		MenuItem{Menu: CommandPalette, Command: DebugSelectionToRepl, When: inDebug},
		MenuItem{Menu: CommandPalette, Command: DebugReplAcceptInput, When: contextkey.False()},
		MenuItem{Menu: CommandPalette, Command: DebugReplFocusFilter, When: contextkey.False()},
		MenuItem{Menu: CommandPalette, Command: DebugReplCopyAll, When: contextkey.False()},
		MenuItem{Menu: CommandPalette, Command: DebugReplCopyAllItem, When: contextkey.False()},
		MenuItem{Menu: CommandPalette, Command: DebugReplCopy, When: contextkey.False()},
		MenuItem{Menu: CommandPalette, Command: DebugReplPaste, When: contextkey.False()},
		MenuItem{Menu: CommandPalette, Command: DebugReplCollapseAll, When: contextkey.False()},
		MenuItem{Menu: CommandPalette, Command: DebugSelectRepl, When: contextkey.False()},

		MenuItem{Menu: MenubarViewMenu, Command: DebugToggleRepl, Title: "Debug Console", MnemonicTitle: "De&&bug Console", Group: "5_panel", Order: 2,
			When: contextkey.Has("debuggersAvailable")},
		MenuItem{Menu: EditorContext, Command: DebugSelectionToRepl, Group: "debug", Order: 0, When: contextkey.And(inDebug, contextkey.Has("editorTextFocus"))},

		// the context menu of the console
		MenuItem{Menu: DebugConsoleContext, Command: DebugReplCopy, Group: "2_cutcopypaste", Order: 10},
		MenuItem{Menu: DebugConsoleContext, Command: DebugReplCopyAllItem, Group: "2_cutcopypaste", Order: 20},
		MenuItem{Menu: DebugConsoleContext, Command: DebugReplPaste, Group: "2_cutcopypaste", Order: 30},
		MenuItem{Menu: DebugConsoleContext, Command: DebugReplCollapseAll, Group: "z_commands", Order: 10},
		MenuItem{Menu: DebugConsoleContext, Command: DebugClearRepl, Group: "z_commands", Order: 20},
	)
}
