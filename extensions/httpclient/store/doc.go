// Package store keeps the tree of the collections and environments of a workspace (SPEC §10.33): it reads the files of
// <folder>/.emetos/http with package format, follows the changes made on disk (an editor, git pull) through the workspace watcher,
// and makes the changes the side bar asks for (create, rename, move, duplicate, delete to the trash) as edits of the files that
// keep the comments and the keys a user added.
//
// The tree (Snapshot) never changes after it is handed out: a change makes a new one and calls the listeners of OnChange. A file
// that cannot be read, or is of a newer version, is a node with Err or ReadOnly set, so the side bar shows it and the user can
// open its text. All the operations are serialized and may be called from any goroutine; they do I/O, so not from the UI thread.
package store
