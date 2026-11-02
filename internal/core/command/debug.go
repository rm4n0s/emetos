package command

import (
	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// Run and Debug (SPEC §10.31, step 25.4): the ids of its view container and views, its commands, and the items of its menus. The
// handlers are bound by the window (internal/gui/app/debugview.go and debugcommands.go) while its views are on screen, and by the
// shell of the view of a client of a hosted workspace over the host's debug service (D46).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debug.contribution.ts, debugCommands.ts, debugViewlet.ts,
// debugToolBar.ts and the registrations of the views (variablesView.ts, watchExpressionsView.ts, callStackView.ts, breakpointsView.ts,
// loadedScriptsView.ts, welcomeView.ts)

// The ids of the view container and the views of Run and Debug (common/debug.ts).
const (
	// ViewContainerDebug is the Run and Debug container (VS Code's VIEWLET_ID).
	ViewContainerDebug = "workbench.view.debug"
	// ViewDebugWelcome is the welcome view of the container, shown while there is no configuration (debugUx is simple).
	ViewDebugWelcome = "workbench.debug.welcome"
	// ViewDebugVariables, ViewDebugWatch, ViewDebugCallStack, ViewDebugBreakpoints and ViewDebugLoadedScripts are the five views.
	ViewDebugVariables     = "workbench.debug.variablesView"
	ViewDebugWatch         = "workbench.debug.watchExpressionsView"
	ViewDebugCallStack     = "workbench.debug.callStackView"
	ViewDebugBreakpoints   = "workbench.debug.breakPointsView"
	ViewDebugLoadedScripts = "workbench.debug.loadedScriptsView"
)

// The ids of the debug menus. DebugToolBar is the buttons of the debug toolbar in each of its places, DebugToolBarStop the other
// ways to end a session (the dropdown of Stop), the Debug...Context menus the context menus and inline buttons of the views, and
// DebugViewTitle the title bar of each view (its rows test `view == '<id>'`).
const (
	// MenubarDebugMenu is the Run menu of the menu bar.
	MenubarDebugMenu = "MenubarDebugMenu"
	// DebugToolBar is the debug toolbar.
	DebugToolBar = "DebugToolBar"
	// DebugToolBarStop is the menu behind the Stop button of the toolbar.
	DebugToolBarStop = "DebugToolBarStop"
	// DebugCallStackContext is the context menu and the inline buttons of the Call Stack.
	DebugCallStackContext = "DebugCallStackContext"
	// DebugVariablesContext is the context menu of the Variables view.
	DebugVariablesContext = "DebugVariablesContext"
	// DebugWatchContext is the context menu and the inline buttons of the Watch view.
	DebugWatchContext = "DebugWatchContext"
	// DebugBreakpointsContext is the context menu of the Breakpoints view.
	DebugBreakpointsContext = "DebugBreakpointsContext"
	// DebugLoadedScriptsContext is the context menu of the Loaded Scripts view.
	DebugLoadedScriptsContext = "DebugLoadedScriptsContext"
	// DebugViewTitle is the title bar of the views of Run and Debug.
	DebugViewTitle = "DebugViewTitle"
	// DebugContainerTitle is the title bar of the Run and Debug container: the start button, the gear, and, while the toolbar is
	// docked, the toolbar's buttons.
	DebugContainerTitle = "DebugContainerTitle"
)

// The ids of the commands of Run and Debug.
const (
	DebugStartCommand         = "workbench.action.debug.start"
	DebugRunCommand           = "workbench.action.debug.run"
	DebugStopCommand          = "workbench.action.debug.stop"
	DebugDisconnectCommand    = "workbench.action.debug.disconnect"
	DebugDisconnectSuspend    = "workbench.action.debug.disconnectAndSuspend"
	DebugRestartCommand       = "workbench.action.debug.restart"
	DebugContinueCommand      = "workbench.action.debug.continue"
	DebugPauseCommand         = "workbench.action.debug.pause"
	DebugStepOverCommand      = "workbench.action.debug.stepOver"
	DebugStepIntoCommand      = "workbench.action.debug.stepInto"
	DebugStepOutCommand       = "workbench.action.debug.stepOut"
	DebugStepIntoTarget       = "workbench.action.debug.stepIntoTarget"
	DebugStepBackCommand      = "workbench.action.debug.stepBack"
	DebugReverseContinue      = "workbench.action.debug.reverseContinue"
	DebugTerminateThread      = "workbench.action.debug.terminateThread"
	DebugRestartFrame         = "workbench.action.debug.restartFrame"
	DebugFocusSession         = "workbench.action.debug.focusProcess"
	DebugSelectAndStart       = "workbench.action.debug.selectandstart"
	DebugConfigure            = "workbench.action.debug.configure"
	DebugAddConfiguration     = "debug.addConfiguration"
	DebugCallStackTop         = "workbench.action.debug.callStackTop"
	DebugCallStackBottom      = "workbench.action.debug.callStackBottom"
	DebugCallStackUp          = "workbench.action.debug.callStackUp"
	DebugCallStackDown        = "workbench.action.debug.callStackDown"
	DebugShowLoadedScripts    = "workbench.action.debug.showLoadedScripts"
	DebugSelectSession        = "workbench.action.debug.selectDebugSession"
	DebugCopyStackTrace       = "debug.copyStackTrace"
	DebugCopyValue            = "workbench.debug.viewlet.action.copyValue"
	DebugCopyEvaluatePath     = "debug.copyEvaluatePath"
	DebugAddToWatch           = "debug.addToWatchExpressions"
	DebugSetVariable          = "debug.setVariable"
	DebugSetExpression        = "debug.setWatchExpression"
	DebugRenameWatch          = "debug.renameWatchExpression"
	DebugRemoveWatch          = "debug.removeWatchExpression"
	DebugAddWatch             = "workbench.debug.viewlet.action.addWatchExpression"
	DebugRemoveAllWatch       = "workbench.debug.viewlet.action.removeAllWatchExpressions"
	DebugCollapseAll          = "workbench.debug.viewlet.action.collapseAll"
	DebugRemoveAllBreakpoints = "workbench.debug.viewlet.action.removeAllBreakpoints"
	DebugEnableAllBreakpoints = "workbench.debug.viewlet.action.enableAllBreakpoints"
	DebugDisableAllBreakpoint = "workbench.debug.viewlet.action.disableAllBreakpoints"
	DebugToggleActivated      = "workbench.debug.viewlet.action.toggleBreakpointsActivatedAction"
	DebugAddFunctionBreakpoit = "workbench.debug.viewlet.action.addFunctionBreakpointAction"
	DebugToggleBreakpointItem = "debug.toggleBreakpoint"
	DebugRemoveBreakpoint     = "debug.removeBreakpoint"
	DebugToggleExceptions     = "debug.toggleExceptionBreakpoints"
	DebugToggleReplIgnore     = "debug.toggleReplIgnoreFocus"
	// DebugEditBreakpoint edits the function name or the condition of the breakpoint row it is run for.
	DebugEditBreakpoint = "debug.editBreakpoint"
)

// debugCategory is the category of the commands of the palette.
const debugCategory = "Debug"

// registerDebugCommands registers the commands of Run and Debug that the views, the toolbar, the status bar and the Run menu run.
// The commands of the editor (debugeditor.go, step 25.5) and the Debug Console's (25.6) are elsewhere.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugCommands.ts, debugViewlet.ts, callStackView.ts,
// variablesView.ts, watchExpressionsView.ts, breakpointsView.ts and loadedScriptsView.ts
func registerDebugCommands(r *Registry) {
	r.MustRegister(
		Meta{ID: ViewContainerDebug, Title: "Show Run and Debug", Category: "View"},
		// the preconditions of the commands that have keys are on the rows of the command palette and of the menus, as in VS Code
		Meta{ID: DebugStartCommand, Title: "Start Debugging", Category: debugCategory, Icon: "debug-start"},
		Meta{ID: DebugRunCommand, Title: "Start Without Debugging", Category: debugCategory},
		Meta{ID: DebugStopCommand, Title: "Stop", Category: debugCategory, Icon: "debug-stop"},
		Meta{ID: DebugDisconnectCommand, Title: "Disconnect", Category: debugCategory, Icon: "debug-disconnect"},
		Meta{ID: DebugDisconnectSuspend, Title: "Disconnect and Suspend", Category: debugCategory, Icon: "debug-disconnect"},
		Meta{ID: DebugRestartCommand, Title: "Restart", Category: debugCategory, Icon: "debug-restart"},
		Meta{ID: DebugContinueCommand, Title: "Continue", Category: debugCategory, Icon: "debug-continue"},
		Meta{ID: DebugPauseCommand, Title: "Pause", Category: debugCategory, Icon: "debug-pause"},
		Meta{ID: DebugStepOverCommand, Title: "Step Over", Category: debugCategory, Icon: "debug-step-over"},
		Meta{ID: DebugStepIntoCommand, Title: "Step Into", Category: debugCategory, Icon: "debug-step-into"},
		Meta{ID: DebugStepOutCommand, Title: "Step Out", Category: debugCategory, Icon: "debug-step-out"},
		Meta{ID: DebugStepIntoTarget, Title: "Step Into Target", Category: debugCategory},
		Meta{ID: DebugStepBackCommand, Title: "Step Back", Icon: "debug-step-back"},
		Meta{ID: DebugReverseContinue, Title: "Reverse", Icon: "debug-reverse-continue"},
		Meta{ID: DebugTerminateThread, Title: "Terminate Thread", Category: debugCategory},
		Meta{ID: DebugRestartFrame, Title: "Restart Frame", Icon: "debug-restart-frame", Precondition: contextkey.Has("stackFrameSupportsRestart")},
		Meta{ID: DebugFocusSession, Title: "Focus Session", Icon: "list-tree"},
		Meta{ID: DebugSelectAndStart, Title: "Select and Start Debugging", Category: debugCategory},
		Meta{ID: DebugConfigure, Title: "Open 'launch.json'", Category: debugCategory, Icon: "gear",
			Description: "Opens the file used to configure how your program is debugged", Precondition: contextkey.NotEquals("debugUx", "simple")},
		Meta{ID: DebugAddConfiguration, Title: "Add Configuration...", Category: debugCategory},
		Meta{ID: DebugCallStackTop, Title: "Navigate to Top of Call Stack", Category: debugCategory},
		Meta{ID: DebugCallStackBottom, Title: "Navigate to Bottom of Call Stack", Category: debugCategory},
		Meta{ID: DebugCallStackUp, Title: "Navigate Up Call Stack", Category: debugCategory},
		Meta{ID: DebugCallStackDown, Title: "Navigate Down Call Stack", Category: debugCategory},
		Meta{ID: DebugShowLoadedScripts, Title: "Open Loaded Script...", Category: debugCategory},
		Meta{ID: DebugSelectSession, Title: "Select Debug Session", Category: debugCategory},
		Meta{ID: DebugCopyStackTrace, Title: "Copy Call Stack"},
		Meta{ID: DebugCopyValue, Title: "Copy Value"},
		Meta{ID: DebugCopyEvaluatePath, Title: "Copy as Expression"},
		Meta{ID: DebugAddToWatch, Title: "Add to Watch"},
		Meta{ID: DebugSetVariable, Title: "Set Value"},
		Meta{ID: DebugSetExpression, Title: "Set Value"},
		Meta{ID: DebugRenameWatch, Title: "Edit Expression"},
		Meta{ID: DebugRemoveWatch, Title: "Remove Expression", Icon: "close"},
		Meta{ID: DebugAddWatch, Title: "Add Expression", Icon: "add"},
		Meta{ID: DebugRemoveAllWatch, Title: "Remove All Expressions", Icon: "close-all", Precondition: contextkey.Has("watchExpressionsExist")},
		Meta{ID: DebugCollapseAll, Title: "Collapse All", Icon: "collapse-all"},
		Meta{ID: DebugRemoveAllBreakpoints, Title: "Remove All Breakpoints", Icon: "close-all", Precondition: contextkey.Has("breakpointsExist")},
		Meta{ID: DebugEnableAllBreakpoints, Title: "Enable All Breakpoints", Precondition: contextkey.Has("breakpointsExist")},
		Meta{ID: DebugDisableAllBreakpoint, Title: "Disable All Breakpoints", Precondition: contextkey.Has("breakpointsExist")},
		Meta{ID: DebugToggleActivated, Title: "Toggle Activate Breakpoints", Icon: "activate-breakpoints", Precondition: contextkey.Has("breakpointsExist")},
		Meta{ID: DebugAddFunctionBreakpoit, Title: "Add Function Breakpoint", Icon: "add"},
		Meta{ID: DebugToggleBreakpointItem},
		Meta{ID: DebugRemoveBreakpoint},
		Meta{ID: DebugToggleExceptions},
		Meta{ID: DebugToggleReplIgnore, Title: "Debug Console"},
		Meta{ID: DebugEditBreakpoint, Title: "Edit Breakpoint..."},
		// the commands that focus a view of the container
		Meta{ID: "workbench.debug.action.focusVariablesView", Title: "Focus on Variables View", Category: "View"},
		Meta{ID: "workbench.debug.action.focusWatchView", Title: "Focus on Watch View", Category: "View"},
		Meta{ID: "workbench.debug.action.focusCallStackView", Title: "Focus on Call Stack View", Category: "View"},
		Meta{ID: "workbench.debug.action.focusBreakpointsView", Title: "Focus on Breakpoints View", Category: "View"},
		Meta{ID: "workbench.debug.action.focusLoadedScriptsView", Title: "Focus on Loaded Scripts View", Category: "View"},
	)
	registerDebugEditorCommands(r)
}

// registerDebugMenus adds the Run menu, the toolbar, the context menus and the title bars of Run and Debug.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debug.contribution.ts, debugToolBar.ts, debugViewlet.ts and the
// registerAction2 calls of the views
func registerDebugMenus(r *MenuRegistry) {
	available := contextkey.Has("debuggersAvailable")
	stopped := contextkey.Equals("debugState", "stopped")
	inDebug := contextkey.Has("inDebugMode")
	// the command palette: a debug command is listed while a debugger is available, and the ones that need a session while one runs; the
	// precondition of a row is when the command is enabled
	attach, terminate := contextkey.Has("focusedSessionIsAttach"), contextkey.Has("terminateDebuggeeSupported")
	idle := contextkey.NotEquals("debugState", "initializing")
	for _, c := range []struct {
		id        string
		when, pre contextkey.Expr
	}{
		{DebugRestartCommand, nil, nil},
		{DebugTerminateThread, inDebug, contextkey.Has("terminateThreadsSupported")},
		{DebugStepOverCommand, inDebug, stopped},
		{DebugStepIntoCommand, inDebug, stopped},
		{DebugStepIntoTarget, inDebug, contextkey.And(contextkey.Has("stepIntoTargetsSupported"), inDebug, stopped)},
		{DebugStepOutCommand, inDebug, contextkey.And(stopped, contextkey.Has("threadHasMultipleStackFrames"))},
		{DebugPauseCommand, inDebug, contextkey.And(contextkey.Equals("debugState", "running"), contextkey.Not("focusedSessionIsNoDebug"))},
		{DebugDisconnectCommand, inDebug, contextkey.Or(attach, terminate)},
		{DebugDisconnectSuspend, inDebug, contextkey.Or(attach, contextkey.And(contextkey.Has("suspendDebuggeeSupported"), terminate))},
		{DebugStopCommand, inDebug, contextkey.Or(contextkey.Not("focusedSessionIsAttach"), terminate)},
		{DebugContinueCommand, inDebug, stopped},
		{DebugStartCommand, nil, idle},
		{DebugRunCommand, nil, idle},
		{DebugSelectAndStart, nil, idle},
		{DebugShowLoadedScripts, inDebug, contextkey.Has("loadedScriptsSupported")},
		{DebugSelectSession, nil, nil},
		{DebugCallStackTop, inDebug, stopped}, {DebugCallStackBottom, inDebug, stopped},
		{DebugCallStackUp, inDebug, stopped}, {DebugCallStackDown, inDebug, stopped},
	} {
		w := available
		if c.when != nil {
			w = contextkey.And(available, c.when)
		}
		r.MustAdd(MenuItem{Menu: CommandPalette, Command: c.id, When: w, Precondition: c.pre})
	}
	// the commands that only the menus and the views run
	for _, id := range []string{
		DebugRestartFrame, DebugFocusSession, DebugCopyStackTrace, DebugCopyValue, DebugCopyEvaluatePath, DebugAddToWatch, DebugSetVariable,
		DebugSetExpression, DebugRenameWatch, DebugRemoveWatch, DebugAddWatch, DebugRemoveAllWatch, DebugCollapseAll,
		DebugRemoveAllBreakpoints, DebugEnableAllBreakpoints, DebugDisableAllBreakpoint, DebugToggleActivated, DebugAddFunctionBreakpoit,
		DebugToggleReplIgnore, DebugStepBackCommand, DebugReverseContinue,
		DebugConfigure,
	} {
		r.MustAdd(MenuItem{Menu: CommandPalette, Command: id, When: contextkey.False()})
	}
	r.MustAdd(MenuItem{Menu: CommandPalette, Command: DebugConfigure, When: available})

	// the Run menu
	r.MustAdd(
		MenuItem{Menu: MenubarMainMenu, Submenu: MenubarDebugMenu, Title: "Run", MnemonicTitle: "&&Run", Order: 6},
		MenuItem{Menu: MenubarDebugMenu, Command: DebugStartCommand, MnemonicTitle: "&&Start Debugging", Group: "1_debug", Order: 1, When: available},
		MenuItem{Menu: MenubarDebugMenu, Command: DebugRunCommand, Title: "Run Without Debugging", MnemonicTitle: "Run &&Without Debugging", Group: "1_debug", Order: 2, When: available},
		MenuItem{Menu: MenubarDebugMenu, Command: DebugStopCommand, Title: "Stop Debugging", MnemonicTitle: "&&Stop Debugging", Group: "1_debug", Order: 3,
			Precondition: inDebug, When: available},
		MenuItem{Menu: MenubarDebugMenu, Command: DebugRestartCommand, Title: "Restart Debugging", MnemonicTitle: "&&Restart Debugging", Group: "1_debug", Order: 4,
			Precondition: inDebug, When: available},
		MenuItem{Menu: MenubarDebugMenu, Command: DebugConfigure, Title: "Open Configurations", MnemonicTitle: "Open &&Configurations", Group: "2_configuration", Order: 1, When: available},
		MenuItem{Menu: MenubarDebugMenu, Command: DebugAddConfiguration, MnemonicTitle: "A&&dd Configuration...", Group: "2_configuration", Order: 2, When: available},
		MenuItem{Menu: MenubarDebugMenu, Command: DebugStepOverCommand, MnemonicTitle: "Step &&Over", Group: "3_step", Order: 1,
			Precondition: stopped, When: available},
		MenuItem{Menu: MenubarDebugMenu, Command: DebugStepIntoCommand, MnemonicTitle: "Step &&Into", Group: "3_step", Order: 2,
			Precondition: stopped, When: available},
		MenuItem{Menu: MenubarDebugMenu, Command: DebugStepOutCommand, MnemonicTitle: "Step O&&ut", Group: "3_step", Order: 3,
			Precondition: contextkey.And(stopped, contextkey.Has("threadHasMultipleStackFrames")), When: available},
		MenuItem{Menu: MenubarDebugMenu, Command: DebugContinueCommand, MnemonicTitle: "&&Continue", Group: "3_step", Order: 4,
			Precondition: stopped, When: available},
		MenuItem{Menu: MenubarViewMenu, Command: ViewContainerDebug, Title: "Run", MnemonicTitle: "&&Run", Group: "3_views", Order: 3.5},
	)

	// the debug toolbar, in its three places: the buttons and their order are the same
	toolbar := func(id string, order float64, when contextkey.Expr, precondition contextkey.Expr) {
		r.MustAdd(MenuItem{Menu: DebugToolBar, Command: id, Group: "navigation", Order: order, When: when, Precondition: precondition})
	}
	toolbar(DebugContinueCommand, 10, stopped, nil)
	toolbar(DebugPauseCommand, 10, contextkey.NotEquals("debugState", "stopped"),
		contextkey.And(contextkey.Equals("debugState", "running"), contextkey.Not("focusedSessionIsNoDebug")))
	toolbar(DebugStopCommand, 70, contextkey.Not("focusedSessionIsAttach"), nil)
	toolbar(DebugDisconnectCommand, 70, contextkey.Has("focusedSessionIsAttach"), nil)
	toolbar(DebugStepOverCommand, 20, nil, stopped)
	toolbar(DebugStepIntoCommand, 30, nil, stopped)
	toolbar(DebugStepOutCommand, 40, nil, contextkey.And(stopped, contextkey.Has("threadHasMultipleStackFrames")))
	toolbar(DebugRestartCommand, 60, nil, nil)
	toolbar(DebugStepBackCommand, 50, contextkey.Has("stepBackSupported"), stopped)
	toolbar(DebugReverseContinue, 55, contextkey.Has("stepBackSupported"), stopped)
	toolbar(DebugFocusSession, 100, contextkey.Has("multiSessionDebug"), nil)
	r.MustAdd(
		MenuItem{Menu: DebugToolBarStop, Command: DebugDisconnectCommand, Group: "navigation", Order: 0,
			When: contextkey.And(contextkey.Not("focusedSessionIsAttach"), contextkey.Has("terminateDebuggeeSupported"))},
		MenuItem{Menu: DebugToolBarStop, Command: DebugStopCommand, Group: "navigation", Order: 0,
			When: contextkey.And(contextkey.Has("focusedSessionIsAttach"), contextkey.Has("terminateDebuggeeSupported"))},
		MenuItem{Menu: DebugToolBarStop, Command: DebugDisconnectSuspend, Group: "navigation", Order: 0,
			When: contextkey.Or(
				contextkey.And(contextkey.Not("focusedSessionIsAttach"), contextkey.Has("suspendDebuggeeSupported"), contextkey.Has("terminateDebuggeeSupported")),
				contextkey.And(contextkey.Has("focusedSessionIsAttach"), contextkey.Has("suspendDebuggeeSupported")))},
	)

	// the title of the container: Start Debugging and the gear while no session is docked, and the toolbar's buttons while docked
	notDocked := contextkey.Or(contextkey.Equals("debugState", "inactive"), contextkey.NotEquals("config.debug.toolBarLocation", "docked"))
	r.MustAdd(
		MenuItem{Menu: DebugContainerTitle, Command: DebugStartCommand, Group: "navigation", Order: 10,
			Precondition: contextkey.NotEquals("debugState", "initializing"),
			When: contextkey.And(contextkey.NotEquals("debugUx", "simple"), contextkey.NotEquals("workbenchState", "empty"), notDocked,
				contextkey.Or(contextkey.Not("config.debug.hideLauncherWhileDebugging"), contextkey.Not("inDebugMode")))},
		MenuItem{Menu: DebugContainerTitle, Command: DebugConfigure, Group: "navigation", Order: 20,
			When: contextkey.And(contextkey.NotEquals("debugUx", "simple"), contextkey.NotEquals("workbenchState", "empty"), notDocked)},
		MenuItem{Menu: DebugContainerTitle, Command: DebugConfigure, Order: 20,
			When: contextkey.And(contextkey.NotEquals("debugState", "inactive"), contextkey.Equals("config.debug.toolBarLocation", "docked"))},
		// the Debug Console comes with step 25.6, which turns the item on
		MenuItem{Menu: DebugContainerTitle, Command: DebugToggleReplIgnore, Title: "Debug Console", Group: "3_toggleRepl", Order: 30, When: contextkey.False()},
		MenuItem{Menu: DebugContainerTitle, Command: DebugSelectAndStart, Title: "Start Additional Session", Order: 10,
			When: contextkey.And(contextkey.NotEquals("debugState", "inactive"),
				contextkey.Or(contextkey.Equals("config.debug.toolBarLocation", "docked"), contextkey.Has("config.debug.hideLauncherWhileDebugging")))},
	)

	// the Call Stack
	itemType := func(t string) contextkey.Expr { return contextkey.Equals("callStackItemType", t) }
	r.MustAdd(
		MenuItem{Menu: DebugCallStackContext, Command: DebugRestartCommand, Group: "3_modification", Order: 10, When: itemType("session")},
		MenuItem{Menu: DebugCallStackContext, Command: DebugDisconnectCommand, Group: "3_modification", Order: 20, When: itemType("session")},
		MenuItem{Menu: DebugCallStackContext, Command: DebugDisconnectSuspend, Group: "3_modification", Order: 21,
			When: contextkey.And(itemType("session"), contextkey.Has("suspendDebuggeeSupported"), contextkey.Has("terminateDebuggeeSupported"))},
		MenuItem{Menu: DebugCallStackContext, Command: DebugStopCommand, Group: "3_modification", Order: 30, When: itemType("session")},
		MenuItem{Menu: DebugCallStackContext, Command: DebugPauseCommand, Group: "navigation", Order: 10,
			When: contextkey.And(itemType("thread"), contextkey.Equals("debugState", "running"), contextkey.Not("focusedSessionIsNoDebug"))},
		MenuItem{Menu: DebugCallStackContext, Command: DebugContinueCommand, Group: "navigation", Order: 10,
			When: contextkey.And(itemType("thread"), stopped)},
		MenuItem{Menu: DebugCallStackContext, Command: DebugStepOverCommand, Group: "navigation", Order: 20, When: itemType("thread"), Precondition: stopped},
		MenuItem{Menu: DebugCallStackContext, Command: DebugStepIntoCommand, Group: "navigation", Order: 30, When: itemType("thread"), Precondition: stopped},
		MenuItem{Menu: DebugCallStackContext, Command: DebugStepOutCommand, Group: "navigation", Order: 40, When: itemType("thread"),
			Precondition: contextkey.And(stopped, contextkey.Has("threadHasMultipleStackFrames"))},
		MenuItem{Menu: DebugCallStackContext, Command: DebugTerminateThread, Title: "Terminate Thread", Group: "termination", Order: 10,
			When: itemType("thread"), Precondition: contextkey.Has("terminateThreadsSupported")},
		MenuItem{Menu: DebugCallStackContext, Command: DebugRestartFrame, Title: "Restart Frame", Group: "navigation", Order: 10,
			When: contextkey.And(itemType("stackFrame"), contextkey.Has("restartFrameSupported")), Precondition: contextkey.Has("stackFrameSupportsRestart")},
		MenuItem{Menu: DebugCallStackContext, Command: DebugCopyStackTrace, Title: "Copy Call Stack", Group: "3_modification", Order: 20, When: itemType("stackFrame")},
	)

	// the Variables
	r.MustAdd(
		MenuItem{Menu: DebugVariablesContext, Command: DebugSetVariable, Title: "Set Value", Group: "3_modification", Order: 10,
			When: contextkey.Or(contextkey.Has("debugSetVariableSupported"),
				contextkey.And(contextkey.Has("variableEvaluateNamePresent"), contextkey.Has("debugSetExpressionSupported"))),
			Precondition: contextkey.Not("variableIsReadonly")},
		MenuItem{Menu: DebugVariablesContext, Command: DebugCopyValue, Group: "5_cutcopypaste", Order: 10},
		MenuItem{Menu: DebugVariablesContext, Command: DebugCopyEvaluatePath, Group: "5_cutcopypaste", Order: 20, When: contextkey.Has("variableEvaluateNamePresent")},
		MenuItem{Menu: DebugVariablesContext, Command: DebugAddToWatch, Group: "z_commands", Order: 100, When: contextkey.Has("variableEvaluateNamePresent")},
	)

	// the Watch
	watchType := func(t string) contextkey.Expr { return contextkey.Equals("watchItemType", t) }
	r.MustAdd(
		MenuItem{Menu: DebugWatchContext, Command: DebugAddWatch, Title: "Add Expression", Group: "3_modification", Order: 10},
		MenuItem{Menu: DebugWatchContext, Command: DebugRenameWatch, Title: "Edit Expression", Group: "3_modification", Order: 20, When: watchType("expression")},
		MenuItem{Menu: DebugWatchContext, Command: DebugSetExpression, Title: "Set Value", Group: "3_modification", Order: 30,
			When: contextkey.Or(contextkey.And(watchType("expression"), contextkey.Has("debugSetExpressionSupported")),
				contextkey.And(watchType("variable"), contextkey.Has("debugSetVariableSupported"))),
			Precondition: contextkey.Not("variableIsReadonly")},
		MenuItem{Menu: DebugWatchContext, Command: DebugCopyValue, Title: "Copy Value", Group: "3_modification", Order: 40,
			When: contextkey.Or(watchType("expression"), watchType("variable")), Precondition: inDebug},
		MenuItem{Menu: DebugWatchContext, Command: DebugCopyEvaluatePath, Group: "3_modification", Order: 50,
			When: contextkey.Has("variableEvaluateNamePresent"), Precondition: inDebug},
		MenuItem{Menu: DebugWatchContext, Command: DebugRemoveWatch, Title: "Remove Expression", Group: "inline", Order: 20, When: watchType("expression")},
		MenuItem{Menu: DebugWatchContext, Command: DebugRemoveAllWatch, Group: "z_commands", Order: 20},
	)

	// the title bars of the views
	view := func(id string) contextkey.Expr { return contextkey.Equals("view", id) }
	r.MustAdd(
		MenuItem{Menu: DebugViewTitle, Command: DebugAddWatch, Group: "navigation", Order: 10, When: view(ViewDebugWatch)},
		MenuItem{Menu: DebugViewTitle, Command: DebugCollapseAll, Group: "navigation", Order: 20, When: view(ViewDebugWatch)},
		MenuItem{Menu: DebugViewTitle, Command: DebugRemoveAllWatch, Group: "navigation", Order: 30, When: view(ViewDebugWatch)},
		MenuItem{Menu: DebugViewTitle, Command: DebugCollapseAll, Group: "navigation", Order: 20, When: view(ViewDebugVariables)},
		MenuItem{Menu: DebugViewTitle, Command: DebugCollapseAll, Group: "navigation", Order: 20, When: view(ViewDebugCallStack)},
		MenuItem{Menu: DebugViewTitle, Command: DebugCollapseAll, Group: "navigation", Order: 20, When: view(ViewDebugLoadedScripts)},
		MenuItem{Menu: DebugViewTitle, Command: DebugAddFunctionBreakpoit, Group: "navigation", Order: 10, When: view(ViewDebugBreakpoints)},
		MenuItem{Menu: DebugViewTitle, Command: DebugToggleActivated, Group: "navigation", Order: 20, When: view(ViewDebugBreakpoints)},
		MenuItem{Menu: DebugViewTitle, Command: DebugRemoveAllBreakpoints, Group: "navigation", Order: 30, When: view(ViewDebugBreakpoints)},
		MenuItem{Menu: DebugViewTitle, Command: DebugEnableAllBreakpoints, Group: "z_commands", Order: 10, When: view(ViewDebugBreakpoints)},
		MenuItem{Menu: DebugViewTitle, Command: DebugDisableAllBreakpoint, Group: "z_commands", Order: 20, When: view(ViewDebugBreakpoints)},
	)

	// the context menu of the Breakpoints
	r.MustAdd(
		MenuItem{Menu: DebugBreakpointsContext, Command: DebugAddFunctionBreakpoit, Group: "2_breakpoints", Order: 10},
		MenuItem{Menu: DebugBreakpointsContext, Command: DebugEditBreakpoint, Group: "1_breakpoints", Order: 10,
			When: contextkey.Or(contextkey.Equals("breakpointItemType", "functionBreakpoint"), contextkey.Equals("breakpointItemType", "exceptionBreakpoint"))},
		MenuItem{Menu: DebugBreakpointsContext, Command: DebugRemoveBreakpoint, Title: "Remove Breakpoint", Group: "1_breakpoints", Order: 20,
			When: contextkey.Or(contextkey.Equals("breakpointItemType", "breakpoint"), contextkey.Equals("breakpointItemType", "functionBreakpoint"))},
		MenuItem{Menu: DebugBreakpointsContext, Command: DebugRemoveBreakpoint, Title: "Remove Breakpoint", Group: "inline", Order: 10,
			When: contextkey.Or(contextkey.Equals("breakpointItemType", "breakpoint"), contextkey.Equals("breakpointItemType", "functionBreakpoint"))},
		// the buttons that show on a row of the Call Stack while the pointer is over it
		MenuItem{Menu: DebugCallStackContext, Command: DebugRestartCommand, Title: "Restart", Group: "inline", Order: 10, When: itemType("session")},
		MenuItem{Menu: DebugCallStackContext, Command: DebugStopCommand, Title: "Stop", Group: "inline", Order: 20,
			When: contextkey.And(itemType("session"), contextkey.Not("focusedSessionIsAttach"))},
		MenuItem{Menu: DebugCallStackContext, Command: DebugDisconnectCommand, Title: "Disconnect", Group: "inline", Order: 20,
			When: contextkey.And(itemType("session"), contextkey.Has("focusedSessionIsAttach"))},
		MenuItem{Menu: DebugCallStackContext, Command: DebugContinueCommand, Title: "Continue", Group: "inline", Order: 10,
			When: contextkey.And(itemType("thread"), stopped)},
		MenuItem{Menu: DebugCallStackContext, Command: DebugPauseCommand, Title: "Pause", Group: "inline", Order: 10,
			When: contextkey.And(itemType("thread"), contextkey.Equals("debugState", "running"))},
		MenuItem{Menu: DebugCallStackContext, Command: DebugStepOverCommand, Title: "Step Over", Group: "inline", Order: 20, When: contextkey.And(itemType("thread"), stopped)},
		MenuItem{Menu: DebugCallStackContext, Command: DebugStepIntoCommand, Title: "Step Into", Group: "inline", Order: 30, When: contextkey.And(itemType("thread"), stopped)},
		MenuItem{Menu: DebugCallStackContext, Command: DebugStepOutCommand, Title: "Step Out", Group: "inline", Order: 40,
			When: contextkey.And(itemType("thread"), stopped, contextkey.Has("threadHasMultipleStackFrames"))},
		MenuItem{Menu: DebugCallStackContext, Command: DebugRestartFrame, Title: "Restart Frame", Group: "inline", Order: 10,
			When: contextkey.And(itemType("stackFrame"), contextkey.Has("restartFrameSupported"), contextkey.Has("stackFrameSupportsRestart"))},
		MenuItem{Menu: DebugBreakpointsContext, Command: DebugToggleActivated, Group: "2_breakpoints", Order: 20,
			Toggled: contextkey.Not("breakpointsDeactivated")},
		MenuItem{Menu: DebugBreakpointsContext, Command: DebugEnableAllBreakpoints, Group: "z_commands", Order: 10},
		MenuItem{Menu: DebugBreakpointsContext, Command: DebugDisableAllBreakpoint, Group: "z_commands", Order: 20},
		MenuItem{Menu: DebugBreakpointsContext, Command: DebugRemoveAllBreakpoints, Group: "z_commands", Order: 30},
	)
	registerDebugEditorMenus(r)
}
