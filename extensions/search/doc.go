// Package search is the extension that finds and replaces a string in the files of the workspace: VS Code's Search view
// (SPEC §10.27, D29). The engine is Emetos's own (internal/core/search) and reaches the extension through
// api.Workspace().FindTextInFiles; this package is the view, the commands, the keys, the context keys, the menus and the
// settings, with VS Code's ids and defaults.
//
// The goroutine rules: a search runs on a goroutine of the extension, which calls FindTextInFiles; the engine calls back from
// its own goroutines, one file at a time, and the callback only puts the file in a list under a lock and asks the UI thread
// (api.Post) to take the list, at most once every 30 ms, so the UI thread does a bounded amount of work per frame however fast
// the results come. Everything else, the model, the tree and the widgets, belongs to the UI thread.
//
// Ported from VS Code (in structure, ids, titles, messages and keys): src/vs/workbench/contrib/search/browser/
// (search.contribution.ts, searchView.ts, searchWidget.ts, searchActions*.ts, searchIcons.ts, patternInputWidget.ts) and
// src/vs/workbench/contrib/search/common/constants.ts (MIT).
package search
