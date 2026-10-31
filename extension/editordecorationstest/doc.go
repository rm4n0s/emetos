// Package editordecorationstest is the conformance suite of the Editor Decorations API (extension.EditorDecorations, SPEC §10.30, step
// 24.22): one set of tests that both implementations of the API pass, the host (internal/gui/exthost, over the store of
// internal/core/annotations) and the fake of the tests of the extensions (extensions/internal/fakeapi).
//
// An implementation passes a Harness to Run: the API of an activated extension that has the commands "test.run" (declared by its
// manifest) and what the suite needs that the API does not give (what the editors would draw for a document, a click on a link of a
// hover, the end of the activation).
package editordecorationstest
