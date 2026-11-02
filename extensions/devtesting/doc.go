// Package devtesting is the fake test controller of the debug builds (SPEC §15.2, step 26.2): "dev.testing.fake" reads a script of tests and
// results from the folder and plays it as a test run, so that the Test Explorer can be shown, in a proof or by hand, without a language's
// test tool. A release build never has it (cmd/emetos adds it only where dev commands are enabled).
package devtesting
