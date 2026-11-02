// Package debug is the Run and Debug views (SPEC §10.31, step 25.4): the Variables, Watch, Call Stack, Breakpoints and Loaded Scripts
// views of the container, the welcome of the container, the debug toolbar and the pieces of the status bar. The views are models over
// the debug service of internal/core/debug (the sessions, threads, frames, variables and breakpoints are its data; a view only
// keeps what is expanded and selected) and widgets over kit's tree. Everything here runs on the UI thread: the events of the service
// come on any goroutine and are handed over with Opts.Post, and the requests the views make (the scopes of a frame, the children of
// a variable, the stack frames) run on goroutines of their own and hand their answer over the same way.
//
// The window (internal/gui/app) binds the views to its menus, commands and context keys; the view of a client of a hosted workspace
// builds the same views over the host's service (D46).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/variablesView.ts, watchExpressionsView.ts, callStackView.ts,
// breakpointsView.ts, loadedScriptsView.ts, welcomeView.ts, debugToolBar.ts, baseDebugView.ts and debugExpressionRenderer.ts
package debug
