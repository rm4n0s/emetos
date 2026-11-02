// Package testing is the Testing view container (SPEC §10.32, step 26.2): the Test Explorer, a tree (or list) of the tests the
// controllers of the extensions describe, with the state, the duration and the buttons of each test, the filter box with its terms, the
// sort order and the view mode, and the summary of the last run. The tree is a model over the test service of internal/core/testing (the
// controllers, the items and the results are its data; a view keeps only what is expanded, selected, hidden and typed in the filter) and a
// widget over kit's tree. Everything here runs on the UI thread: the events of the service come on any goroutine and are handed over with
// Opts.Post.
//
// The window (internal/gui/app) binds the view to its menus, commands and context keys; the view of a client of a hosted workspace builds
// the same view over the host's service (D46).
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/testingExplorerView.ts, testingExplorerFilter.ts, icons.ts and theme.ts,
// browser/explorerProjections/{listProjection,treeProjection,display}.ts, common/testExplorerFilterState.ts, testingProgressMessages.ts
// and testingStates.ts
package testing
