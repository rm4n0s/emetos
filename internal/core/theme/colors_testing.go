package theme

// registerTestingColors registers the colors of the Test Explorer (step 26.2, SPEC §10.32): the icons of the states of a test, and the same
// colors for a result that is outdated ("retired"). The colors of the editor (the run glyphs' color, messages, the peek, coverage) come
// with steps 26.3 and 26.4. list.warningForeground, which testing.iconQueued refers to, is registered here as well.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/theme.ts and src/vs/platform/theme/common/colors/listColors.ts
func registerTestingColors(r *ColorRegistry) {
	r.MustRegister(
		ColorContribution{ID: "list.warningForeground", Defaults: dflt(Hex("#CCA700"), Hex("#855F00"), nil, nil),
			Description: "Foreground color of list items containing warnings."},
		ColorContribution{ID: "testing.iconFailed", Defaults: AllThemes(Ref("list.errorForeground")),
			Description: "Color for the 'failed' icon in the test explorer."},
		ColorContribution{ID: "testing.iconErrored", Defaults: AllThemes(Ref("list.errorForeground")),
			Description: "Color for the 'Errored' icon in the test explorer."},
		ColorContribution{ID: "testing.iconPassed", Defaults: AllThemes(Hex("#73c991")),
			Description: "Color for the 'passed' icon in the test explorer."},
		ColorContribution{ID: "testing.runAction", Defaults: AllThemes(Ref("testing.iconPassed")),
			Description: "Color for 'run' icons in the editor."},
		ColorContribution{ID: "testing.iconQueued", Defaults: AllThemes(Ref("list.warningForeground")),
			Description: "Color for the 'Queued' icon in the test explorer."},
		ColorContribution{ID: "testing.iconUnset", Defaults: AllThemes(Hex("#848484")),
			Description: "Color for the 'Unset' icon in the test explorer."},
		ColorContribution{ID: "testing.iconSkipped", Defaults: AllThemes(Hex("#848484")),
			Description: "Color for the 'Skipped' icon in the test explorer."},
		ColorContribution{ID: "testing.iconErrored.retired", Defaults: AllThemes(Transparent(Ref("testing.iconErrored"), 0.7)),
			Description: "Retired color for the 'Errored' icon in the test explorer."},
		ColorContribution{ID: "testing.iconFailed.retired", Defaults: AllThemes(Transparent(Ref("testing.iconFailed"), 0.7)),
			Description: "Retired color for the 'failed' icon in the test explorer."},
		ColorContribution{ID: "testing.iconPassed.retired", Defaults: AllThemes(Transparent(Ref("testing.iconPassed"), 0.7)),
			Description: "Retired color for the 'passed' icon in the test explorer."},
		ColorContribution{ID: "testing.iconQueued.retired", Defaults: AllThemes(Transparent(Ref("testing.iconQueued"), 0.7)),
			Description: "Retired color for the 'Queued' icon in the test explorer."},
		ColorContribution{ID: "testing.iconUnset.retired", Defaults: AllThemes(Transparent(Ref("testing.iconUnset"), 0.7)),
			Description: "Retired color for the 'Unset' icon in the test explorer."},
		ColorContribution{ID: "testing.iconSkipped.retired", Defaults: AllThemes(Transparent(Ref("testing.iconSkipped"), 0.7)),
			Description: "Retired color for the 'Skipped' icon in the test explorer."},
	)
}

// registerTestingEditorColors registers the colors of the editor and the peek of Testing (step 26.3): the frame and header of the peek, the
// inline messages, and the error badge colors they refer to. The colors of the coverage in the editor come with step 26.4.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/theme.ts and src/vs/platform/theme/common/colors/miscColors.ts
func registerTestingEditorColors(r *ColorRegistry) {
	r.MustRegister(
		ColorContribution{ID: "activityErrorBadge.background", Defaults: dflt(Hex("#F14C4C"), Hex("#E51400"), Hex("#F14C4C"), Hex("#E51400")),
			Description: "Background color of the error activity badge."},
		ColorContribution{ID: "activityErrorBadge.foreground", Defaults: dflt(Hex("#FFFFFF"), Hex("#FFFFFF"), Hex("#FFFFFF"), Hex("#FFFFFF")),
			Description: "Foreground color of the error activity badge."},
		ColorContribution{ID: "testing.peekBorder", Defaults: dflt(Ref("editorError.foreground"), Ref("editorError.foreground"), Ref("contrastBorder"), Ref("contrastBorder")),
			Description: "Color of the peek view borders and arrow."},
		ColorContribution{ID: "testing.messagePeekBorder", Defaults: dflt(Ref("editorInfo.foreground"), Ref("editorInfo.foreground"), Ref("contrastBorder"), Ref("contrastBorder")),
			Description: "Color of the peek view borders and arrow when peeking a logged message."},
		ColorContribution{ID: "testing.peekHeaderBackground", Defaults: dflt(Transparent(Ref("editorError.foreground"), 0.1), Transparent(Ref("editorError.foreground"), 0.1), nil, nil),
			Description: "Color of the peek view borders and arrow."},
		ColorContribution{ID: "testing.messagePeekHeaderBackground", Defaults: dflt(Transparent(Ref("editorInfo.foreground"), 0.1), Transparent(Ref("editorInfo.foreground"), 0.1), nil, nil),
			Description: "Color of the peek view borders and arrow when peeking a logged message."},
		ColorContribution{ID: "testing.message.error.badgeBackground", Defaults: AllThemes(Ref("activityErrorBadge.background")),
			Description: "Background color of test error messages shown inline in the editor."},
		ColorContribution{ID: "testing.message.error.badgeBorder", Defaults: AllThemes(Ref("testing.message.error.badgeBackground")),
			Description: "Border color of test error messages shown inline in the editor."},
		ColorContribution{ID: "testing.message.error.badgeForeground", Defaults: AllThemes(Ref("activityErrorBadge.foreground")),
			Description: "Text color of test error messages shown inline in the editor."},
		ColorContribution{ID: "testing.message.error.lineBackground", Defaults: dflt(nil, nil, nil, nil),
			Description: "Margin color beside error messages shown inline in the editor."},
		ColorContribution{ID: "testing.message.info.decorationForeground", Defaults: AllThemes(Transparent(Ref("editor.foreground"), 0.5)),
			Description: "Text color of test info messages shown inline in the editor."},
		ColorContribution{ID: "testing.message.info.lineBackground", Defaults: dflt(nil, nil, nil, nil),
			Description: "Margin color beside info messages shown inline in the editor."},
	)
}
