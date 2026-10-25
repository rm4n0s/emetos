// Package exthost runs the compiled-in extensions of Emetos (SPEC §10.15, D18). It is the other side of the public
// package extension: Host holds the extensions of the process, and a Scope is the extension host of one window tab,
// which activates the extensions that the tab needs, gives each an extension.API bound to the tab, and takes
// everything they contributed away again when the tab closes or the extension is turned off.
//
// The package has no widgets yet (the step that adds views and tabs adds them here), but it lives under internal/gui
// because the contributions it hosts are the workbench's: the status bar, the menus, the notifications. The editor
// core never imports it.
//
// Every call into an extension is made through a function of this package that catches a panic and an error
// (Activate, a command handler, a status bar callback, a notification action, a settings listener, the factory itself).
// The tag of the error says which call it was; see Failure for what is done with it. Everything runs on the UI thread
// except where a method says otherwise.
package exthost
