// Package workbench holds the workbench: layout parts, editor group views, the title bar row (the menu and the workspace tabs), the activity bar and the status bar (SPEC §10).
//
// So far it has the layout (SPEC §10.1): Layout is the model of which parts are shown, how large the side bar and the panel are
// and where they are, and Workbench is the widget that lays the parts out with kit.SplitView, binds the commands that show,
// hide, move and focus them, and sets their context keys. TitleInput makes the title of the window from window.title.
//
// Ported from VS Code: src/vs/workbench/browser/layout.ts, src/vs/workbench/browser/parts/
// and src/vs/workbench/browser/parts/titlebar/windowTitle.ts
package workbench
