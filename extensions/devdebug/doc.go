// Package devdebug is the extension dev.debug, a debugger for debug builds (SPEC §15.2, step 25.4): the debug type dev.debug.fake, whose
// adapter is the fake adapter of internal/core/dap/daptest run as a program with the script the configuration names. It lets the proofs
// and the tests of the Run and Debug view show a session without a real language server or debugger. It is never in extensions.All():
// cmd/emetos adds it only where app.DevCommandsEnabled says so, so a release build has no such debugger.
package devdebug
