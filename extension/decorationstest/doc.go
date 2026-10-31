// Package decorationstest is the conformance suite of the File Decorations API (extension.Decorations, SPEC §10.30, step 24.6): one set of
// tests that both implementations of the API pass, the host (internal/gui/exthost, over the service of internal/core/decorations)
// and the fake of the tests of the extensions (extensions/internal/fakeapi).
//
// An implementation passes a Harness to Run: the API of an activated extension, and the few things the suite needs that the API does
// not give (what a view would get for a file, whether it was told of a change, the end of the activation).
package decorationstest
