package keybinding

// registerDebugKeys registers the keys of Run and Debug (Linux): the start, the steps, the stop and restart, the keys of the Variables,
// Watch and Breakpoints views, and Ctrl+Shift+D for the container. A rule's when is written out in full: the key's own condition and the
// precondition of its command, as VS Code writes them. The keys of the editor (F9, Shift+F9, the hover, the widgets) are registerDebugEditorKeys (step 25.5) and the
// Debug Console's (Ctrl+Shift+Y) with 25.6.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugCommands.ts, debug.contribution.ts and debugViewlet.ts
func registerDebugKeys(r *Registry) {
	registerDebugEditorKeys(r)
	const (
		stopped = "debugState == 'stopped'"
		watch   = "focusedView == 'workbench.debug.watchExpressionsView'"
		vars    = "focusedView == 'workbench.debug.variablesView'"
	)
	r.MustRegister(
		Rule{Command: "workbench.view.debug", Weight: WeightWorkbenchContrib, Primary: "ctrl+shift+d"},
		Rule{Command: "workbench.debug.action.toggleRepl", Weight: WeightWorkbenchContrib, Primary: "ctrl+shift+y"},
		Rule{Command: "workbench.action.debug.start", Weight: WeightWorkbenchContrib, When: mustWhen("debuggersAvailable", "debugState == 'inactive'"), Primary: "f5"},
		Rule{Command: "workbench.action.debug.run", Weight: WeightWorkbenchContrib, When: mustWhen("debuggersAvailable", "debugState != 'initializing'"), Primary: "ctrl+f5"},
		// Continue has a stronger weight than Start Debugging, which has F5 as well
		Rule{Command: "workbench.action.debug.continue", Weight: WeightWorkbenchContrib + 10, When: mustWhen(stopped), Primary: "f5"},
		Rule{Command: "workbench.action.debug.stepOver", Weight: WeightWorkbenchContrib, When: mustWhen(stopped), Primary: "f10"},
		// Step Into has a stronger weight than full screen, and a more flexible when, so that F11 pressed often does not toggle it
		Rule{Command: "workbench.action.debug.stepInto", Weight: WeightWorkbenchContrib + 10, When: mustWhen("debugState != 'inactive'"), Primary: "f11"},
		Rule{Command: "workbench.action.debug.stepOut", Weight: WeightWorkbenchContrib, When: mustWhen(stopped, "threadHasMultipleStackFrames"), Primary: "shift+f11"},
		// Pause takes priority over focusing the next part while debugging
		Rule{Command: "workbench.action.debug.pause", Weight: WeightWorkbenchContrib + 2, When: mustWhen("debugState == 'running'"), Primary: "f6"},
		Rule{Command: "workbench.action.debug.stepIntoTarget", Weight: WeightWorkbenchContrib,
			When: mustWhen("stepIntoTargetsSupported", "inDebugMode", stopped), Primary: "ctrl+f11"},
		Rule{Command: "workbench.action.debug.restart", Weight: WeightWorkbenchContrib, When: mustWhen("inDebugMode"), Primary: "ctrl+shift+f5"},
		Rule{Command: "workbench.action.debug.disconnect", Weight: WeightWorkbenchContrib, When: mustWhen("focusedSessionIsAttach", "inDebugMode"), Primary: "shift+f5"},
		Rule{Command: "workbench.action.debug.stop", Weight: WeightWorkbenchContrib, When: mustWhen("!focusedSessionIsAttach", "inDebugMode"), Primary: "shift+f5"},

		// the views: Copy Value on Ctrl+C, F2 to edit, Delete to remove, Space to toggle a breakpoint
		Rule{Command: "workbench.debug.viewlet.action.copyValue", Weight: WeightWorkbenchContrib,
			When: mustWhen("!expressionSelected", "("+watch+" || "+vars+")"), Primary: "ctrl+c"},
		Rule{Command: "debug.renameWatchExpression", Weight: WeightWorkbenchContrib + 5, When: mustWhen("watchExpressionsFocused"), Primary: "f2"},
		Rule{Command: "debug.setVariable", Weight: WeightWorkbenchContrib + 5, When: mustWhen("variablesFocused"), Primary: "f2"},
		Rule{Command: "debug.removeWatchExpression", Weight: WeightWorkbenchContrib, When: mustWhen("watchExpressionsFocused", "!expressionSelected"), Primary: "delete"},
		Rule{Command: "debug.toggleBreakpoint", Weight: WeightWorkbenchContrib + 5, When: mustWhen("breakpointsFocused", "!breakpointInputFocused"), Primary: "space"},
		Rule{Command: "debug.removeBreakpoint", Weight: WeightWorkbenchContrib, When: mustWhen("breakpointsFocused", "!breakpointInputFocused"), Primary: "delete"},
	)
}

// registerDebugEditorKeys registers the keys of the editor while debugging (step 25.5): F9 and Shift+F9, the debug hover, Escape for the
// exception widget, and Enter and Escape in the breakpoint widget.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugEditorActions.ts, debugCommands.ts and breakpointWidget.ts
func registerDebugEditorKeys(r *Registry) {
	r.MustRegister(
		Rule{Command: "editor.debug.action.toggleBreakpoint", Weight: WeightEditorContrib, When: mustWhen("editorTextFocus", "debuggersAvailable"), Primary: "f9"},
		Rule{Command: "editor.debug.action.toggleInlineBreakpoint", Weight: WeightWorkbenchContrib, When: mustWhen("editorTextFocus"), Primary: "shift+f9"},
		Rule{Command: "editor.debug.action.showDebugHover", Weight: WeightEditorContrib, When: mustWhen("editorTextFocus", "inDebugMode"), Primary: "ctrl+k ctrl+i"},
		Rule{Command: "editor.debug.action.closeExceptionWidget", Weight: WeightEditorContrib, When: mustWhen("exceptionWidgetVisible"), Primary: "escape"},
		Rule{Command: "breakpointWidget.action.acceptInput", Weight: WeightEditorContrib, When: mustWhen("breakpointWidgetVisible", "inBreakpointWidget"), Primary: "enter"},
		Rule{Command: "closeBreakpointWidget", Weight: WeightEditorContrib, When: mustWhen("breakpointWidgetVisible", "textInputFocus"),
			Primary: "escape", Secondary: []string{"shift+escape"}},
	)
}
