// Package terminalenvtest is the conformance suite of the Terminal Environment API (extension.TerminalEnvironment, SPEC §10.30, step
// 24.23): one set of tests that both implementations of the API pass, the host (internal/gui/exthost) and the fake of the tests of the
// extensions (extensions/internal/fakeapi).
//
// An implementation passes a Harness to Run: the API of an activated extension that contributes, the API of another extension of the same
// scope that reads, and the end of the activation of the first.
package terminalenvtest
