// Package scmtest is the conformance suite of the Source Control API (extension.SourceControls, SPEC §10.30, step 24.2): one
// set of tests that both implementations of the API pass, the host (internal/gui/exthost, over the model of
// internal/core/scm) and the fake of the tests of the extensions (extensions/internal/fakeapi). A test of an extension on the
// fake is worth what the fake does like the host; the suite is what keeps the two alike.
//
// An implementation passes a Harness to Run: the API of an activated extension, and the few things the suite needs that the API
// does not give (a look at what the view would show, the user's typing, the end of the activation).
package scmtest
