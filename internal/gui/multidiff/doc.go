// Package multidiff holds the multi-file diff editor (SPEC §10.30 "Diffs of changes", §9.11): one scrolling column of file sections, each
// a header and the diff editor of the file sized to its content. The package is the layout (Model: the heights of the sections, which
// are collapsed, which are near the viewport) and the widget that draws the headers and positions the bodies; what a body is, and the
// work of reading its documents and computing its diff, belongs to the window (gui/app), which gives the widget a Body function and
// tells the model the height each body measured.
//
// Ported from VS Code: src/vs/editor/browser/widget/multiDiffEditor/ (multiDiffEditorWidgetImpl.ts, diffEditorItemTemplate.ts,
// virtualizedItemManager.ts)
package multidiff
