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
