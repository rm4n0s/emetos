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
