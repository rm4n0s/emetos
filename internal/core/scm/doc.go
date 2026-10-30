// Package scm is the model of the Source Control view (SPEC §10.30, step 24.2): the source controls (repositories) that the
// extensions create, their groups of changed resources, the input and the action button, the counts of the badges, which
// repositories are shown and which one is active, the order and the tree of the resources of a group, and the values of the
// context keys and the arguments of the commands that the menus of the view run. It knows nothing of the UI and of the
// extensions; the host binds the extension API to it (internal/gui/exthost/scm.go) and the view draws it (step 24.3).
//
// The model is safe for use from several goroutines: an extension changes it from any of them. A change is announced to the
// subscribers (Model.Subscribe) from the goroutine that made it, after the lock is released; the view hands the work over to the
// UI thread itself.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/common/scm.ts, scmService.ts, browser/scmViewService.ts,
// browser/activity.ts, browser/menus.ts, browser/util.ts, browser/scmViewPane.ts (the sort and the compact folders of the
// tree) and src/vs/workbench/api/common/extHostSCM.ts.
package scm
