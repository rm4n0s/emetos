package command

import (
	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// The commands, menu items and keys of the editor while debugging (step 25.5). The handlers of the editor's commands are bound by the shell
// of each screen to the editor in front (internal/gui/app/debugeditor.go).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugEditorActions.ts, debugCommands.ts (JUMP_TO_CURSOR_ID,
// TOGGLE_INLINE_BREAKPOINT_ID), breakpointWidget.ts, debug.contribution.ts

// The ids of the commands of the editor.
const (
	// DebugToggleBreakpoint is F9: a breakpoint on the lines of the cursors, or none when there is one.
	DebugToggleBreakpoint = "editor.debug.action.toggleBreakpoint"
	// DebugToggleInlineBreakpoint is Shift+F9: a breakpoint at the column of the cursor.
	DebugToggleInlineBreakpoint = "editor.debug.action.toggleInlineBreakpoint"
	// DebugConditionalBreakpoint, DebugLogPoint, DebugTriggerPoint and DebugEditBreakpointAction open the breakpoint widget at the cursor.
	DebugConditionalBreakpoint = "editor.debug.action.conditionalBreakpoint"
	DebugLogPoint              = "editor.debug.action.addLogPoint"
	DebugTriggerPoint          = "editor.debug.action.triggerByBreakpoint"
	DebugEditBreakpointAction  = "editor.debug.action.editBreakpoint"
	// DebugRunToCursor and DebugJumpToCursor are Run to Cursor and Jump to Cursor.
	DebugRunToCursor  = "editor.debug.action.runToCursor"
	DebugJumpToCursor = "debug.jumpToCursor"
	// DebugSelectionToWatch is Add to Watch for the selection or the expression at the cursor.
	DebugSelectionToWatch = "editor.debug.action.selectionToWatch"
	// DebugShowHover shows the debug hover at the cursor.
	DebugShowHover = "editor.debug.action.showDebugHover"
	// DebugGoToNextBreakpoint and DebugGoToPreviousBreakpoint move to the next and the previous enabled breakpoint.
	DebugGoToNextBreakpoint     = "editor.debug.action.goToNextBreakpoint"
	DebugGoToPreviousBreakpoint = "editor.debug.action.goToPreviousBreakpoint"
	// DebugCloseExceptionWidget closes the exception widget (Escape).
	DebugCloseExceptionWidget = "editor.debug.action.closeExceptionWidget"
	// DebugAcceptBreakpointWidget and DebugCloseBreakpointWidget accept (Enter) and close (Escape) the breakpoint widget.
	DebugAcceptBreakpointWidget = "breakpointWidget.action.acceptInput"
	DebugCloseBreakpointWidget  = "closeBreakpointWidget"
)

// MenubarNewBreakpointMenu is the submenu "New Breakpoint" of the Run menu.
const MenubarNewBreakpointMenu = "MenubarNewBreakpointMenu"

// registerDebugEditorCommands registers the commands of the editor.
func registerDebugEditorCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: DebugToggleBreakpoint, Title: "Toggle Breakpoint", Category: debugCategory, Precondition: contextkey.Has("debuggersAvailable")},
		Meta{ID: DebugToggleInlineBreakpoint, Title: "Inline Breakpoint", Category: debugCategory},
		Meta{ID: DebugConditionalBreakpoint, Title: "Add Conditional Breakpoint...", Category: debugCategory, Precondition: contextkey.Has("debuggersAvailable")},
		Meta{ID: DebugLogPoint, Title: "Add Logpoint...", Category: debugCategory, Precondition: contextkey.Has("debuggersAvailable")},
		Meta{ID: DebugTriggerPoint, Title: "Add Triggered Breakpoint...", Category: debugCategory, Precondition: contextkey.Has("debuggersAvailable")},
		Meta{ID: DebugEditBreakpointAction, Title: "Edit Breakpoint...", Category: debugCategory, Precondition: contextkey.Has("debuggersAvailable")},
		Meta{ID: DebugRunToCursor, Title: "Run to Cursor", Category: debugCategory, Precondition: contextkey.Has("debuggersAvailable")},
		Meta{ID: DebugJumpToCursor, Title: "Jump to Cursor", Category: debugCategory},
		Meta{ID: DebugSelectionToWatch, Title: "Add to Watch", Category: debugCategory},
		Meta{ID: DebugShowHover, Title: "Show Hover", Category: debugCategory, Precondition: contextkey.Has("inDebugMode")},
		Meta{ID: DebugGoToNextBreakpoint, Title: "Go to Next Breakpoint", Category: debugCategory, Precondition: contextkey.Has("debuggersAvailable")},
		Meta{ID: DebugGoToPreviousBreakpoint, Title: "Go to Previous Breakpoint", Category: debugCategory, Precondition: contextkey.Has("debuggersAvailable")},
		Meta{ID: DebugCloseExceptionWidget, Title: "Close Exception Widget", Category: debugCategory, Precondition: contextkey.Has("exceptionWidgetVisible")},
		Meta{ID: DebugAcceptBreakpointWidget, Precondition: contextkey.Has("breakpointWidgetVisible")},
		Meta{ID: DebugCloseBreakpointWidget, Precondition: contextkey.Has("breakpointWidgetVisible")},
	)
}

// registerDebugEditorMenus adds the commands of the editor to the command palette, the Run menu and the context menu of the editor.
func registerDebugEditorMenus(r *MenuRegistry) {
	available := contextkey.Has("debuggersAvailable")
	inDebug := contextkey.Has("inDebugMode")
	for _, c := range []struct {
		id   string
		when contextkey.Expr
	}{
		{DebugToggleBreakpoint, available}, {DebugToggleInlineBreakpoint, available}, {DebugConditionalBreakpoint, available},
		{DebugLogPoint, available}, {DebugTriggerPoint, available}, {DebugEditBreakpointAction, available}, {DebugRunToCursor, available},
		{DebugJumpToCursor, contextkey.Has("jumpToCursorSupported")}, {DebugSelectionToWatch, inDebug}, {DebugShowHover, inDebug},
		{DebugGoToNextBreakpoint, available}, {DebugGoToPreviousBreakpoint, available},
	} {
		r.MustAdd(MenuItem{Menu: CommandPalette, Command: c.id, When: c.when})
	}
	for _, id := range []string{DebugCloseExceptionWidget} {
		r.MustAdd(MenuItem{Menu: CommandPalette, Command: id, When: contextkey.False()})
	}

	// the Run menu: Toggle Breakpoint, the submenu of the other kinds, and the commands on all breakpoints
	r.MustAdd(
		MenuItem{Menu: MenubarDebugMenu, Command: DebugToggleBreakpoint, MnemonicTitle: "Toggle &&Breakpoint", Group: "4_new_breakpoint", Order: 1, When: available},
		MenuItem{Menu: MenubarDebugMenu, Submenu: MenubarNewBreakpointMenu, Title: "New Breakpoint", MnemonicTitle: "&&New Breakpoint", Group: "4_new_breakpoint", Order: 2,
			When: available},
		MenuItem{Menu: MenubarNewBreakpointMenu, Command: DebugConditionalBreakpoint, Title: "Conditional Breakpoint...", MnemonicTitle: "&&Conditional Breakpoint...", Group: "1_breakpoints", Order: 1, When: available},
		MenuItem{Menu: MenubarNewBreakpointMenu, Command: DebugToggleInlineBreakpoint, MnemonicTitle: "Inline Breakp&&oint", Group: "1_breakpoints", Order: 2, When: available},
		MenuItem{Menu: MenubarNewBreakpointMenu, Command: DebugAddFunctionBreakpoit, Title: "Function Breakpoint...", MnemonicTitle: "&&Function Breakpoint...",
			Group: "1_breakpoints", Order: 3, When: available},
		MenuItem{Menu: MenubarNewBreakpointMenu, Command: DebugLogPoint, Title: "Logpoint...", MnemonicTitle: "&&Logpoint...", Group: "1_breakpoints", Order: 4, When: available},
		MenuItem{Menu: MenubarNewBreakpointMenu, Command: DebugTriggerPoint, Title: "Triggered Breakpoint...", MnemonicTitle: "&&Triggered Breakpoint...", Group: "1_breakpoints", Order: 5, When: available},
		MenuItem{Menu: MenubarDebugMenu, Command: DebugEnableAllBreakpoints, MnemonicTitle: "&&Enable All Breakpoints", Group: "5_breakpoints", Order: 1, When: available},
		MenuItem{Menu: MenubarDebugMenu, Command: DebugDisableAllBreakpoint, MnemonicTitle: "Disable &&All Breakpoints", Group: "5_breakpoints", Order: 2, When: available},
		MenuItem{Menu: MenubarDebugMenu, Command: DebugRemoveAllBreakpoints, MnemonicTitle: "Remove &&All Breakpoints", Group: "5_breakpoints", Order: 3, When: available},
	)

	// the context menu of the editor
	focus := contextkey.Has("editorTextFocus")
	r.MustAdd(
		MenuItem{Menu: EditorContext, Command: DebugToggleInlineBreakpoint, Title: "Add Inline Breakpoint", Group: "debug", Order: 1,
			When: contextkey.And(inDebug, focus)},
		MenuItem{Menu: EditorContext, Command: DebugSelectionToWatch, Group: "debug", Order: 1, When: contextkey.And(inDebug, focus)},
		MenuItem{Menu: EditorContext, Command: DebugRunToCursor, Group: "debug", Order: 2, When: contextkey.And(inDebug, focus)},
		MenuItem{Menu: EditorContext, Command: DebugJumpToCursor, Group: "debug", Order: 3, When: contextkey.And(contextkey.Has("jumpToCursorSupported"), focus)},
	)
}
