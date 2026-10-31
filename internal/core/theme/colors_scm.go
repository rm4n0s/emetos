package theme

// registerSCMColors registers the colors of the status letters of the Source Control view and of the file decorations of Git
// (step 24.3; the Git extension of VS Code contributes them in its package.json, here the host registers them because the view
// reads them by id). The blame color came with step 24.22; the incoming colors come with the step that uses them.
//
// Ported from VS Code: extensions/git/package.json (contributes.colors) and package.nls.json
func registerSCMColors(r *ColorRegistry) {
	r.MustRegister(
		ColorContribution{ID: "gitDecoration.addedResourceForeground", Defaults: dflt(Hex("#81b88b"), Hex("#587c0c"), Hex("#a1e3ad"), Hex("#374e06")),
			Description: "Color for added resources."},
		ColorContribution{ID: "gitDecoration.modifiedResourceForeground", Defaults: dflt(Hex("#E2C08D"), Hex("#895503"), Hex("#E2C08D"), Hex("#895503")),
			Description: "Color for modified resources."},
		ColorContribution{ID: "gitDecoration.deletedResourceForeground", Defaults: dflt(Hex("#c74e39"), Hex("#ad0707"), Hex("#c74e39"), Hex("#ad0707")),
			Description: "Color for deleted resources."},
		ColorContribution{ID: "gitDecoration.renamedResourceForeground", Defaults: dflt(Hex("#73C991"), Hex("#007100"), Hex("#73C991"), Hex("#007100")),
			Description: "Color for renamed or copied resources."},
		ColorContribution{ID: "gitDecoration.untrackedResourceForeground", Defaults: dflt(Hex("#73C991"), Hex("#007100"), Hex("#73C991"), Hex("#007100")),
			Description: "Color for untracked resources."},
		ColorContribution{ID: "gitDecoration.ignoredResourceForeground", Defaults: dflt(Hex("#8C8C8C"), Hex("#8E8E90"), Hex("#A7A8A9"), Hex("#8e8e90")),
			Description: "Color for ignored resources."},
		ColorContribution{ID: "gitDecoration.stageModifiedResourceForeground", Defaults: dflt(Hex("#E2C08D"), Hex("#895503"), Hex("#E2C08D"), Hex("#895503")),
			Description: "Color for modified resources which have been staged."},
		ColorContribution{ID: "gitDecoration.stageDeletedResourceForeground", Defaults: dflt(Hex("#c74e39"), Hex("#ad0707"), Hex("#c74e39"), Hex("#ad0707")),
			Description: "Color for deleted resources which have been staged."},
		ColorContribution{ID: "gitDecoration.conflictingResourceForeground", Defaults: dflt(Hex("#e4676b"), Hex("#ad0707"), Hex("#c74e39"), Hex("#ad0707")),
			Description: "Color for resources with conflicts."},
		ColorContribution{ID: "gitDecoration.submoduleResourceForeground", Defaults: dflt(Hex("#8db9e2"), Hex("#1258a7"), Hex("#8db9e2"), Hex("#1258a7")),
			Description: "Color for submodule resources."},
		ColorContribution{ID: "git.blame.editorDecorationForeground", Defaults: AllThemes(Ref("editorInlayHint.foreground")),
			Description: "Color for the blame editor decoration."},
	)
}

// registerQuickDiffColors registers the colors of the marks of the quick diff in the gutter, the overview ruler and the minimap (step 24.11).
//
// Ported from VS Code: src/vs/workbench/contrib/scm/common/quickDiff.ts
func registerQuickDiffColors(r *ColorRegistry) {
	r.MustRegister(
		ColorContribution{ID: "editorGutter.modifiedBackground", Defaults: dflt(Hex("#1B81A8"), Hex("#2090D3"), Hex("#1B81A8"), Hex("#2090D3")),
			Description: "Editor gutter background color for lines that are modified."},
		ColorContribution{ID: "editorGutter.modifiedSecondaryBackground",
			Defaults:    dflt(Darken(Ref("editorGutter.modifiedBackground"), 0.5), Lighten(Ref("editorGutter.modifiedBackground"), 0.7), Hex("#1B81A8"), Hex("#2090D3")),
			Description: "Editor gutter secondary background color for lines that are modified."},
		ColorContribution{ID: "editorGutter.addedBackground", Defaults: dflt(Hex("#487E02"), Hex("#48985D"), Hex("#487E02"), Hex("#48985D")),
			Description: "Editor gutter background color for lines that are added."},
		ColorContribution{ID: "editorGutter.addedSecondaryBackground",
			Defaults:    dflt(Darken(Ref("editorGutter.addedBackground"), 0.5), Lighten(Ref("editorGutter.addedBackground"), 0.7), Hex("#487E02"), Hex("#48985D")),
			Description: "Editor gutter secondary background color for lines that are added."},
		ColorContribution{ID: "editorGutter.deletedBackground", Defaults: AllThemes(Ref("editorError.foreground")),
			Description: "Editor gutter background color for lines that are deleted."},
		ColorContribution{ID: "editorGutter.deletedSecondaryBackground",
			Defaults:    dflt(Darken(Ref("editorGutter.deletedBackground"), 0.4), Lighten(Ref("editorGutter.deletedBackground"), 0.3), Hex("#F48771"), Hex("#B5200D")),
			Description: "Editor gutter secondary background color for lines that are deleted."},
		ColorContribution{ID: "minimapGutter.modifiedBackground", Defaults: AllThemes(Ref("editorGutter.modifiedBackground")),
			Description: "Minimap gutter background color for lines that are modified."},
		ColorContribution{ID: "minimapGutter.addedBackground", Defaults: AllThemes(Ref("editorGutter.addedBackground")),
			Description: "Minimap gutter background color for lines that are added."},
		ColorContribution{ID: "minimapGutter.deletedBackground", Defaults: AllThemes(Ref("editorGutter.deletedBackground")),
			Description: "Minimap gutter background color for lines that are deleted."},
		ColorContribution{ID: "editorOverviewRuler.modifiedForeground", Defaults: AllThemes(Transparent(Ref("editorGutter.modifiedBackground"), 0.6)),
			Description: "Overview ruler marker color for modified content."},
		ColorContribution{ID: "editorOverviewRuler.addedForeground", Defaults: AllThemes(Transparent(Ref("editorGutter.addedBackground"), 0.6)),
			Description: "Overview ruler marker color for added content."},
		ColorContribution{ID: "editorOverviewRuler.deletedForeground", Defaults: AllThemes(Transparent(Ref("editorGutter.deletedBackground"), 0.6)),
			Description: "Overview ruler marker color for deleted content."},
	)
}
