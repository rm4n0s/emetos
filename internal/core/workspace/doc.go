// Package workspace holds the folder model, the file tree, the file watcher and file operations (SPEC §10.5).
//
// The folder of a window is a [Workspace]: a tree of [Item] values read lazily, sorted and filtered
// like the Explorer of VS Code, and kept up to date with the events of a [Watcher], which watches the
// folder with inotify (golang.org/x/sys/unix) and falls back to polling.
//
// Ported from VS Code: src/vs/workbench/contrib/files/common/explorerModel.ts (the tree),
// src/vs/workbench/contrib/files/browser/views/explorerViewer.ts (the sort order, the filter and
// the compact folders), src/vs/base/common/comparers.ts (the name comparers) and
// src/vs/platform/files/common/watcher.ts (the coalescing of events).
package workspace
