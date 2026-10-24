// Package settingsui holds the settings editor (SPEC §11.1): the widget that lists the registered settings by
// group, edits them in the user's or the workspace's settings.json through core/settings, and marks the ones the
// file sets.
//
// Ported from VS Code: src/vs/workbench/contrib/preferences/browser/settingsEditor2.ts and settingsTree.ts (the
// layout, the rows, the commit rules and the colors); the model is core/settings (UIModel).
package settingsui
