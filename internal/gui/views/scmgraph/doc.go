// Package scmgraph is the Graph view of Source Control: the history of the repository as rows with lanes (SPEC §10.30 "The Source
// Control Graph", step 24.20). The Pane is the model of the view (which repository, which references, the pages loaded, the rows), the
// Body draws them. The lanes are laid out by internal/core/scm/graph.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmHistoryViewPane.ts and scmHistory.ts
package scmgraph
