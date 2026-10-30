// Package scm is the Source Control view (SPEC §10.30, step 24.3): the Changes view with the groups and the resources of the
// repositories that the model of internal/core/scm holds, the Repositories view, the welcome content of an empty view, and the
// pieces the window builds from the same model (the badge of the activity bar icon and the status bar items).
//
// The view only draws and navigates: the model is the extensions' (their source controls, groups and resources), the menus are
// the registry's (the rows of the menus that extensions contribute) and the commands run through the registry of the window. The
// window gives the view what it needs in Options.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmViewPane.ts, scmRepositoryRenderer.ts, scmRepositoriesViewPane.ts,
// scmViewService.ts, util.ts and media/scm.css
package scm
