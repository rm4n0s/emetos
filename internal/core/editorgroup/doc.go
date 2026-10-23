// Package editorgroup holds editor inputs, editor groups and their grid, as data (SPEC §10.3).
//
// An EditorInput is what a tab stands for: a text file, later a diff, a merge or a settings
// page. A Model is the editors of one group: their order, the preview editor, the pinned and
// sticky ones, the active one and the most recently used order.
//
// Ported from VS Code: src/vs/workbench/common/editor/editorGroupModel.ts (the model),
// src/vs/workbench/common/editor/editorInput.ts (the input) and src/vs/base/common/labels.ts
// (shorten, for the descriptions of tabs that share a name).
package editorgroup
