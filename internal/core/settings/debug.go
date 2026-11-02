package settings

// categoryDebug is the group of the settings of Run and Debug.
const categoryDebug = "Debug"

// registerDebug registers the debug.* settings that the views, the toolbar, the status bar and the start of a session read (step 25.4,
// SPEC §10.31), with VS Code's keys, descriptions, scopes and defaults, and the `launch` setting. The settings of the editor
// (debug.inlineValues, debug.showBreakpointsInOverviewRuler, debug.showInlineBreakpointCandidates, debug.gutterMiddleClickAction) came
// with step 25.5 and those of the Debug Console (debug.console.*, debug.internalConsoleOptions) with 25.6. The names, values and defaults were checked against the pinned reference; `debug.toolBarLocation` has VS Code's fourth
// value `commandCenter`, which Emetos has no command center for, so it is not offered (decision 1186).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debug.contribution.ts (the "debug" configuration)
func registerDebug(r *Registry) {
	r.MustRegister(
		Setting{
			Key: "debug.showVariableTypes", Types: []Type{Boolean}, Default: false,
			Description: "Show variable type in variable pane during debug session", Scope: ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.openExplorerOnEnd", Types: []Type{Boolean}, Default: false,
			Description: "Automatically open the explorer view at the end of a debug session.", Scope: ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.closeReadonlyTabsOnEnd", Types: []Type{Boolean}, Default: false,
			Description: "At the end of a debug session, all the read-only tabs associated with that session will be closed",
			Scope:       ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.toolBarLocation", Types: []Type{String}, Default: "floating", Enum: []any{"floating", "docked", "hidden"},
			EnumDescriptions: []string{"Show debug toolbar in all views.", "Show debug toolbar only in debug views.", "Do not show debug toolbar."},
			Description:      "Controls the location of the debug toolbar. Either `floating` in all views, `docked` in the debug view, or `hidden`.",
			Scope:            ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.showInStatusBar", Types: []Type{String}, Default: "onFirstSessionStart", Enum: []any{"never", "always", "onFirstSessionStart"},
			EnumDescriptions: []string{"Never show debug item in status bar", "Always show debug item in status bar",
				"Show debug item in status bar only after debug was started for the first time"},
			Description: "Controls when the debug status bar item should be visible.", Scope: ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.terminal.clearBeforeReusing", Types: []Type{Boolean}, Default: false,
			Description: "Before starting a new debug session in an integrated or external terminal, clear the terminal.",
			Scope:       ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.openDebug", Types: []Type{String}, Default: "openOnDebugBreak",
			Enum:        []any{"neverOpen", "openOnSessionStart", "openOnFirstSessionStart", "openOnDebugBreak"},
			Description: "Controls when the debug view should open.", Scope: ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.showSubSessionsInToolBar", Types: []Type{Boolean}, Default: false,
			Description: "Controls whether the debug sub-sessions are shown in the debug tool bar. When this setting is false the stop command on a sub-session will also stop the parent session.",
			Scope:       ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "launch", Types: []Type{Object}, Default: map[string]any{"configurations": []any{}, "compounds": []any{}},
			Description: "Global debug launch configuration. Should be used as an alternative to 'launch.json' that is shared across workspaces.",
			Scope:       ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.focusWindowOnBreak", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether the workbench window should be focused when the debugger breaks.", Scope: ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.focusEditorOnBreak", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether the editor should be focused when the debugger breaks.", Scope: ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.onTaskErrors", Types: []Type{String}, Default: "prompt", Enum: []any{"debugAnyway", "showErrors", "prompt", "abort"},
			EnumDescriptions: []string{"Ignore task errors and start debugging.", "Show the Problems view and do not start debugging.", "Prompt user.", "Cancel debugging."},
			Description:      "Controls what to do when errors are encountered after running a preLaunchTask.", Scope: ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.allowBreakpointsEverywhere", Types: []Type{Boolean}, Default: false,
			Description: "Allow setting breakpoints in any file.", Scope: ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.breakpointsView.presentation", Types: []Type{String}, Default: "list", Enum: []any{"tree", "list"},
			Description: "Controls whether breakpoints are displayed in a tree view grouped by file, or as a flat list.",
			Scope:       ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.saveBeforeStart", Types: []Type{String}, Default: "allEditorsInActiveGroup",
			Enum: []any{"allEditorsInActiveGroup", "nonUntitledEditorsInActiveGroup", "none"},
			EnumDescriptions: []string{"Save all editors in the active group before starting a debug session.",
				"Save all editors in the active group except untitled ones before starting a debug session.", "Don't save any editors before starting a debug session."},
			Description: "Controls what editors to save before starting a debug session.", Scope: ScopeLanguageOverridable, Category: categoryDebug,
		},
		Setting{
			Key: "debug.confirmOnExit", Types: []Type{String}, Default: "never", Enum: []any{"never", "always"},
			EnumDescriptions: []string{"Never confirm.", "Always confirm if there are debug sessions."},
			Description:      "Controls whether to confirm when the window closes if there are active debug sessions.", Scope: ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.disassemblyView.showSourceCode", Types: []Type{Boolean}, Default: true,
			Description: "Show Source Code in Disassembly View.", Scope: ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.autoExpandLazyVariables", Types: []Type{String}, Default: "auto", Enum: []any{"auto", "on", "off"},
			EnumDescriptions: []string{"When in screen reader optimized mode, automatically expand lazy variables.", "Always automatically expand lazy variables.",
				"Never automatically expand lazy variables."},
			Description: "Controls whether variables that are lazily resolved, such as getters, are automatically resolved and expanded by the debugger.",
			Scope:       ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.enableStatusBarColor", Types: []Type{Boolean}, Default: true,
			Description: "Color of the status bar when the debugger is active.", Scope: ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.hideLauncherWhileDebugging", Types: []Type{Boolean}, Default: false,
			Description: "Hide 'Start Debugging' control in title bar of 'Run and Debug' view while debugging is active. Only relevant when `#debug.toolBarLocation#` is not `docked`.",
			Scope:       ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.hideSlowPreLaunchWarning", Types: []Type{Boolean}, Default: false,
			Description: "Hide the warning shown when a `preLaunchTask` has been running for a while.", Scope: ScopeWindow, Category: categoryDebug,
		},
		// the settings of the editor (step 25.5)
		Setting{
			Key: "debug.inlineValues", Types: []Type{String}, Default: "auto", Enum: []any{"on", "off", "auto"},
			EnumDescriptions: []string{"Always show variable values inline in editor while debugging.", "Never show variable values inline in editor while debugging.",
				"Show variable values inline in editor while debugging when the language supports inline value locations."},
			Description: "Show variable values inline in editor while debugging.", Scope: ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.showBreakpointsInOverviewRuler", Types: []Type{Boolean}, Default: false,
			Description: "Controls whether breakpoints should be shown in the overview ruler.", Scope: ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.showInlineBreakpointCandidates", Types: []Type{Boolean}, Default: true,
			Description: "Controls whether inline breakpoints candidate decorations should be shown in the editor while debugging.",
			Scope:       ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.gutterMiddleClickAction", Types: []Type{String}, Default: "logpoint",
			Enum:             []any{"logpoint", "conditionalBreakpoint", "triggeredBreakpoint", "none"},
			EnumDescriptions: []string{"Add Logpoint.", "Add Conditional Breakpoint.", "Add Triggered Breakpoint.", "Don't perform any action."},
			Description:      "Controls the action to perform when clicking the editor gutter with the middle mouse button.", Scope: ScopeWindow, Category: categoryDebug,
		},
		// the settings of the Debug Console (step 25.6)
		Setting{
			Key: "debug.internalConsoleOptions", Types: []Type{String}, Default: "openOnFirstSessionStart",
			Enum:        []any{"neverOpen", "openOnSessionStart", "openOnFirstSessionStart"},
			Description: "Controls when the internal Debug Console should open.", Scope: ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.console.closeOnEnd", Types: []Type{Boolean}, Default: false,
			Description: "Controls if the Debug Console should be automatically closed when the debug session ends.", Scope: ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.console.fontSize", Types: []Type{Number}, Default: 14,
			Description: "Controls the font size in pixels in the Debug Console.", Scope: ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.console.fontFamily", Types: []Type{String}, Default: "default",
			Description: "Controls the font family in the Debug Console.", Scope: ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.console.lineHeight", Types: []Type{Number}, Default: 0,
			Description: "Controls the line height in pixels in the Debug Console. Use 0 to compute the line height from the font size.",
			Scope:       ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.console.wordWrap", Types: []Type{Boolean}, Default: true,
			Description: "Controls if the lines should wrap in the Debug Console.", Scope: ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.console.historySuggestions", Types: []Type{Boolean}, Default: true,
			Description: "Controls if the Debug Console should suggest previously typed input.", Scope: ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.console.collapseIdenticalLines", Types: []Type{Boolean}, Default: true,
			Description: "Controls if the Debug Console should collapse identical lines and show a number of occurrences with a badge.",
			Scope:       ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.console.acceptSuggestionOnEnter", Types: []Type{String}, Default: "off", Enum: []any{"off", "on"},
			Description: "Controls whether suggestions should be accepted on Enter in the Debug Console. Enter is also used to evaluate whatever is typed in the Debug Console.",
			Scope:       ScopeWindow, Category: categoryDebug,
		},
		Setting{
			Key: "debug.console.maximumLines", Types: []Type{Number}, Default: 10000,
			Description: "Controls the maximum number of lines in the Debug Console.", Scope: ScopeWindow, Category: categoryDebug,
		},
	)
}
