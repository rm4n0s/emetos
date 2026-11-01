// Package graph lays out the history of a source control as lanes: for each commit the lanes that come into its row and the lanes
// that leave it, with the color of every lane (SPEC §10.30 "The Source Control Graph", step 24.20). The layout is done page by page:
// Layout takes the next page and the state the previous page left, so the rows of the pages above are never computed again. The
// package has no user interface; internal/gui/views/scmgraph draws the rows.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmHistory.ts (toISCMHistoryItemViewModelArray and the functions it
// calls) and src/vs/workbench/contrib/scm/common/history.ts
package graph
