package theme

// registerDebugColors registers the colors of the Run and Debug views, the debug toolbar and the status bar while debugging (step 25.4,
// SPEC §10.31). The colors of the editor (the stack frame lines, the breakpoint icons) come with step 25.5 and those of the Debug
// Console with 25.6. The icon colors debugIcon.start, pause, stop and restart came with step 18.2.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugColors.ts and statusbarColorProvider.ts
func registerDebugColors(r *ColorRegistry) {
	r.MustRegister(
		ColorContribution{ID: "statusBar.debuggingBackground", Defaults: dflt(Hex("#CC6633"), Hex("#CC6633"), Hex("#BA592C"), Hex("#B5200D")),
			Description: "Status bar background color when a program is being debugged. The status bar is shown in the bottom of the window"},
		ColorContribution{ID: "statusBar.debuggingForeground", Defaults: dflt(Ref("statusBar.foreground"), Ref("statusBar.foreground"), Ref("statusBar.foreground"), Hex("#FFFFFF")),
			Description: "Status bar foreground color when a program is being debugged. The status bar is shown in the bottom of the window"},
		ColorContribution{ID: "statusBar.debuggingBorder", Defaults: AllThemes(Ref("statusBar.border")),
			Description: "Status bar border color separating to the sidebar and editor when a program is being debugged. The status bar is shown in the bottom of the window"},
		ColorContribution{ID: "debugToolBar.background", Defaults: dflt(Hex("#333333"), Hex("#F3F3F3"), Hex("#000000"), Hex("#FFFFFF")),
			Description: "Debug toolbar background color."},
		ColorContribution{ID: "debugToolBar.border", Defaults: dflt(nil, nil, nil, nil), Description: "Debug toolbar border color."},
		ColorContribution{ID: "debugIcon.breakpointForeground", Defaults: AllThemes(Hex("#E51400")), Description: "Icon color for breakpoints."},
		ColorContribution{ID: "debugIcon.breakpointDisabledForeground", Defaults: AllThemes(Hex("#848484")), Description: "Icon color for disabled breakpoints."},
		ColorContribution{ID: "debugIcon.breakpointUnverifiedForeground", Defaults: AllThemes(Hex("#848484")), Description: "Icon color for unverified breakpoints."},
		ColorContribution{ID: "list.deemphasizedForeground", Defaults: dflt(Hex("#8C8C8C"), Hex("#8E8E90"), Hex("#A7A8A9"), Hex("#666666")),
			Description: "List/Tree foreground color for items that are deemphasized."},
		ColorContribution{ID: "debugIcon.disconnectForeground", Defaults: dflt(Hex("#F48771"), Hex("#A1260D"), Hex("#F48771"), Hex("#A1260D")),
			Description: "Debug toolbar icon for disconnect."},
		ColorContribution{ID: "debugIcon.stepOverForeground", Defaults: dflt(Hex("#75BEFF"), Hex("#007ACC"), Hex("#75BEFF"), Hex("#007ACC")),
			Description: "Debug toolbar icon for step over."},
		ColorContribution{ID: "debugIcon.stepIntoForeground", Defaults: dflt(Hex("#75BEFF"), Hex("#007ACC"), Hex("#75BEFF"), Hex("#007ACC")),
			Description: "Debug toolbar icon for step into."},
		ColorContribution{ID: "debugIcon.stepOutForeground", Defaults: dflt(Hex("#75BEFF"), Hex("#007ACC"), Hex("#75BEFF"), Hex("#007ACC")),
			Description: "Debug toolbar icon for step over."},
		ColorContribution{ID: "debugIcon.continueForeground", Defaults: dflt(Hex("#75BEFF"), Hex("#007ACC"), Hex("#75BEFF"), Hex("#007ACC")),
			Description: "Debug toolbar icon for continue."},
		ColorContribution{ID: "debugIcon.stepBackForeground", Defaults: dflt(Hex("#75BEFF"), Hex("#007ACC"), Hex("#75BEFF"), Hex("#007ACC")),
			Description: "Debug toolbar icon for step back."},
		ColorContribution{ID: "debugTokenExpression.name", Defaults: dflt(Hex("#c586c0"), Hex("#9b46b0"), Ref("foreground"), Ref("foreground")),
			Description: "Foreground color for the token names shown in the debug views (ie. the Variables or Watch view)."},
		ColorContribution{ID: "debugTokenExpression.type", Defaults: dflt(Hex("#4A90E2"), Hex("#4A90E2"), Ref("foreground"), Ref("foreground")),
			Description: "Foreground color for the token types shown in the debug views (ie. the Variables or Watch view)."},
		ColorContribution{ID: "debugTokenExpression.value", Defaults: dflt(Hex("#cccccc99"), Hex("#6c6c6ccc"), Ref("foreground"), Ref("foreground")),
			Description: "Foreground color for the token values shown in the debug views (ie. the Variables or Watch view)."},
		ColorContribution{ID: "debugTokenExpression.string", Defaults: dflt(Hex("#ce9178"), Hex("#a31515"), Hex("#f48771"), Hex("#a31515")),
			Description: "Foreground color for strings in the debug views (ie. the Variables or Watch view)."},
		ColorContribution{ID: "debugTokenExpression.boolean", Defaults: dflt(Hex("#4e94ce"), Hex("#0000ff"), Hex("#75bdfe"), Hex("#0000ff")),
			Description: "Foreground color for booleans in the debug views (ie. the Variables or Watch view)."},
		ColorContribution{ID: "debugTokenExpression.number", Defaults: dflt(Hex("#b5cea8"), Hex("#098658"), Hex("#89d185"), Hex("#098658")),
			Description: "Foreground color for numbers in the debug views (ie. the Variables or Watch view)."},
		ColorContribution{ID: "debugTokenExpression.error", Defaults: dflt(Hex("#f48771"), Hex("#e51400"), Hex("#f48771"), Hex("#e51400")),
			Description: "Foreground color for expression errors in the debug views (ie. the Variables or Watch view) and for error logs shown in the debug console."},
		ColorContribution{ID: "debugView.exceptionLabelForeground", Defaults: dflt(Ref("foreground"), Hex("#FFF"), Ref("foreground"), Ref("foreground")),
			Description: "Foreground color for a label shown in the CALL STACK view when the debugger breaks on an exception."},
		ColorContribution{ID: "debugView.exceptionLabelBackground", Defaults: dflt(Hex("#6C2022"), Hex("#A31515"), Hex("#6C2022"), Hex("#A31515")),
			Description: "Background color for a label shown in the CALL STACK view when the debugger breaks on an exception."},
		ColorContribution{ID: "debugView.stateLabelForeground", Defaults: AllThemes(Ref("foreground")),
			Description: "Foreground color for a label in the CALL STACK view showing the current session's or thread's state."},
		ColorContribution{ID: "debugView.stateLabelBackground", Defaults: AllThemes(Hex("#88888844")),
			Description: "Background color for a label in the CALL STACK view showing the current session's or thread's state."},
		ColorContribution{ID: "debugView.valueChangedHighlight", Defaults: AllThemes(Hex("#569CD6")),
			Description: "Color used to highlight value changes in the debug views (ie. in the Variables view)."},
	)
	registerDebugEditorColors(r)
}

// registerDebugEditorColors registers the colors of the editor while debugging (step 25.5): the lines of the stack frames, the icons of
// the current frames in the glyph margin, the inline values and the exception widget.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/callStackEditorContribution.ts, breakpointEditorContribution.ts,
// debugEditorContribution.ts and exceptionWidget.ts (the registerColor calls)
func registerDebugEditorColors(r *ColorRegistry) {
	r.MustRegister(
		ColorContribution{ID: "editor.stackFrameHighlightBackground", Defaults: dflt(Hex("#ffff0033"), Hex("#ffff6673"), Hex("#ffff0033"), Hex("#ffff6673")),
			Description: "Background color for the highlight of line at the top stack frame position."},
		ColorContribution{ID: "editor.focusedStackFrameHighlightBackground", Defaults: dflt(Hex("#7abd7a4d"), Hex("#cee7ce73"), Hex("#7abd7a4d"), Hex("#cee7ce73")),
			Description: "Background color for the highlight of line at focused stack frame position."},
		ColorContribution{ID: "debugIcon.breakpointCurrentStackframeForeground", Defaults: dflt(Hex("#FFCC00"), Hex("#BE8700"), Hex("#FFCC00"), Hex("#BE8700")),
			Description: "Icon color for the current breakpoint stack frame."},
		ColorContribution{ID: "debugIcon.breakpointStackframeForeground", Defaults: AllThemes(Hex("#89D185")),
			Description: "Icon color for all breakpoint stack frames."},
		ColorContribution{ID: "editor.inlineValuesForeground", Defaults: dflt(Hex("#ffffff80"), Hex("#00000080"), Hex("#ffffff80"), Hex("#00000080")),
			Description: "Color for the debug inline value text."},
		ColorContribution{ID: "editor.inlineValuesBackground", Defaults: AllThemes(Hex("#ffc80033")),
			Description: "Color for the debug inline value background."},
		ColorContribution{ID: "debugExceptionWidget.border", Defaults: AllThemes(Hex("#a31515")), Description: "Exception widget border color."},
		ColorContribution{ID: "debugExceptionWidget.background", Defaults: dflt(Hex("#420b0d"), Hex("#f1dfde"), Hex("#420b0d"), Hex("#f1dfde")),
			Description: "Exception widget background color."},
	)
}
