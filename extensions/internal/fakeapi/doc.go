// Package fakeapi is an extension.API for the tests of the extensions under extensions/: a fake that does what the host does
// in memory, so a test runs the Activate of an extension and then looks at what it contributed (the views and their
// content, the Output channels, the Problems markers, the commands, the status bar items), triggers what the user does (run
// a command, change a setting, publish an event) and lets the work the extension posted to the UI thread run. It is never
// compiled into Emetos.
package fakeapi
