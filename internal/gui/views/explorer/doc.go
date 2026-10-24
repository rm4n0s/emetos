// Package explorer holds the views of the Explorer (SPEC §10.5, §10.8): the tree of the folder of the window,
// the Open Editors list, and the welcome content of a window that has no folder.
//
// The views are plain models (View, OpenEditors) over the folder model of core/workspace and the editor
// groups of core/editorgroup, with widgets that show them (NewBody, NewActions, NewOpenEditorsBody,
// NewWelcome). The window makes the models, binds the commands that act on them and decides what a click
// opens.
package explorer
