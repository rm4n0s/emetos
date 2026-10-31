// Package contenttest is the conformance suite of the provided documents of the extension contract (Documents().ProvideContent,
// OpenDiff, OpenDocument, Tabs and CloseTabs): the host runs it over a window tab, and the fake of the tests of the extensions runs it
// too, so that an extension that passes its tests against the fake meets the same rules in the window.
package contenttest

import (
	"context"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// Harness is what an implementation gives the suite.
type Harness struct {
	// API is the API of the extension under test, in an activation that is on.
	API extension.API
	// Text asks the window for the text of a provided document, as it does when it opens one, from any goroutine; the error has the
	// tag ExtContentUnknown when no provider has the scheme.
	Text func(ctx context.Context, uri string) (string, error)
	// Label and Language ask the window for the name and the language of a provided document ("" for none).
	Label, Language func(uri string) string
	// Diffs, Docs and Changed are what the window was asked to open and what the provider said changed, since the start.
	Diffs   func() []extension.DiffSpec
	Docs    func() []extension.OpenSpec
	Changed func() [][]string
	// Deactivate ends the activation, as closing the window tab does.
	Deactivate func()
	// Failed tells whether the implementation took the extension off because of a panic in the provider.
	Failed func() bool
	// Pump lets the work posted to the UI thread run.
	Pump func()
}

// Run runs the suite: newHarness makes a fresh harness for each test.
func Run(t *testing.T, newHarness func(t *testing.T) *Harness) {
	t.Helper()
	tests := []struct {
		name string
		run  func(t *testing.T, h *Harness)
	}{
		{"the provider makes the text, the label and the language", testProvide},
		{"the same scheme twice is refused, and Dispose frees it", testSchemeTaken},
		{"a scheme that is not a word, and one of the core, are refused", testSchemeInvalid},
		{"a provider with no Text is refused", testNoText},
		{"OpenDiff and OpenDocument hand the window what they were given", testOpen},
		{"OpenDiff and OpenDocument refuse what cannot open", testOpenInvalid},
		{"Changed tells the window the URIs", testChanged},
		{"the end of the activation takes the provider away", testDeactivate},
		{"a panic in Text is an error for the caller and takes the extension off", testPanic},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { tc.run(t, newHarness(t)) })
	}
}

func wantTag(t *testing.T, err error, tag string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	if appErr.Tag != tag {
		t.Fatalf("tag %s (%s), want %s", appErr.Tag, appErr.Route(), tag)
	}
}

func provider(scheme string) extension.ContentProvider {
	return extension.ContentProvider{
		Scheme:   scheme,
		Label:    func(uri string) string { return "label of " + uri },
		Language: func(uri string) string { return "lang" },
		Text:     func(_ context.Context, uri string) (string, error) { return "text of " + uri, nil },
	}
}

func testProvide(t *testing.T, h *Harness) {
	if _, err := h.API.Documents().ProvideContent(provider("tst")); err != nil {
		t.Fatal(err)
	}
	uri := extension.ContentURI("tst", "a/HEAD/x.go")
	text, err := h.Text(context.Background(), uri)
	if err != nil || text != "text of "+uri {
		t.Errorf("text %q %v", text, err)
	}
	if got := h.Label(uri); got != "label of "+uri {
		t.Errorf("label %q", got)
	}
	if got := h.Language(uri); got != "lang" {
		t.Errorf("language %q", got)
	}
	_, err = h.Text(context.Background(), extension.ContentURI("other", "x"))
	wantTag(t, err, "ExtContentUnknown")
}

func testSchemeTaken(t *testing.T, h *Harness) {
	reg, err := h.API.Documents().ProvideContent(provider("tst"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = h.API.Documents().ProvideContent(provider("tst"))
	wantTag(t, err, "ExtContentSchemeTaken")
	reg.Dispose()
	reg.Dispose()
	if _, err := h.Text(context.Background(), extension.ContentURI("tst", "x")); err == nil {
		t.Error("a disposed provider still answers")
	}
	if _, err := h.API.Documents().ProvideContent(provider("tst")); err != nil {
		t.Errorf("the scheme is not free after Dispose: %v", err)
	}
}

func testSchemeInvalid(t *testing.T, h *Harness) {
	for _, scheme := range []string{"", "Git", "1git", "a b", "a/b", "file", "untitled", "emetos-virtual", "a_b"} {
		_, err := h.API.Documents().ProvideContent(provider(scheme))
		wantTag(t, err, "ExtensionContentSchemeInvalid")
	}
	if _, err := h.API.Documents().ProvideContent(provider("a-b2")); err != nil {
		t.Errorf("a-b2: %v", err)
	}
}

func testNoText(t *testing.T, h *Harness) {
	_, err := h.API.Documents().ProvideContent(extension.ContentProvider{Scheme: "tst"})
	wantTag(t, err, "ExtensionContentProviderInvalid")
}

func testOpen(t *testing.T, h *Harness) {
	docs := h.API.Documents()
	if _, err := docs.ProvideContent(provider("tst")); err != nil {
		t.Fatal(err)
	}
	left, right := extension.ContentURI("tst", "a/HEAD/x.go"), "file:///work/x.go"
	sel := &extension.Selection{Anchor: extension.Position{Line: 1}, Active: extension.Position{Line: 2, Col: 3}}
	diff := extension.DiffSpec{Left: left, Right: right, Title: "x.go (Working Tree)", Preview: true, Selection: sel, PreserveFocus: true}
	if err := docs.OpenDiff(diff); err != nil {
		t.Fatal(err)
	}
	open := extension.OpenSpec{URI: left, Title: "x.go (HEAD)", Preview: true, Selection: sel}
	if err := docs.OpenDocument(open); err != nil {
		t.Fatal(err)
	}
	h.Pump()
	if got := h.Diffs(); len(got) != 1 || got[0] != diff {
		t.Errorf("diffs %+v", got)
	}
	if got := h.Docs(); len(got) != 1 || got[0] != open {
		t.Errorf("documents %+v", got)
	}
}

func testOpenInvalid(t *testing.T, h *Harness) {
	docs := h.API.Documents()
	file := "file:///work/x.go"
	wantTag(t, docs.OpenDiff(extension.DiffSpec{Left: file}), "ExtensionDiffInvalid")
	wantTag(t, docs.OpenDiff(extension.DiffSpec{Right: file}), "ExtensionDiffInvalid")
	wantTag(t, docs.OpenDocument(extension.OpenSpec{}), "ExtensionDocumentInvalid")
	wantTag(t, docs.OpenDiff(extension.DiffSpec{Left: extension.ContentURI("none", "x"), Right: file}), "ExtContentUnknown")
	wantTag(t, docs.OpenDocument(extension.OpenSpec{URI: extension.ContentURI("none", "x")}), "ExtContentUnknown")
	if n := len(h.Diffs()) + len(h.Docs()); n != 0 {
		t.Errorf("%d things opened", n)
	}
}

func testChanged(t *testing.T, h *Harness) {
	reg, err := h.API.Documents().ProvideContent(provider("tst"))
	if err != nil {
		t.Fatal(err)
	}
	a, b := extension.ContentURI("tst", "a//x.go"), extension.ContentURI("tst", "a/~/x.go")
	reg.Changed(a, b)
	reg.Changed()
	h.Pump()
	got := h.Changed()
	if len(got) < 1 || len(got[0]) != 2 || got[0][0] != a || got[0][1] != b {
		t.Errorf("changed %v", got)
	}
}

func testDeactivate(t *testing.T, h *Harness) {
	docs := h.API.Documents()
	reg, err := docs.ProvideContent(provider("tst"))
	if err != nil {
		t.Fatal(err)
	}
	h.Deactivate()
	if _, err := h.Text(context.Background(), extension.ContentURI("tst", "x")); err == nil {
		t.Error("the provider answers after the end of the activation")
	}
	reg.Changed(extension.ContentURI("tst", "x")) // nothing happens
	reg.Dispose()
	_, err = docs.ProvideContent(provider("tst2"))
	wantTag(t, err, "ExtensionDeactivated")
	wantTag(t, docs.OpenDiff(extension.DiffSpec{Left: "file:///a", Right: "file:///b"}), "ExtensionDeactivated")
	wantTag(t, docs.OpenDocument(extension.OpenSpec{URI: "file:///a"}), "ExtensionDeactivated")
	if got := docs.Tabs(); len(got) != 0 {
		t.Errorf("tabs %v", got)
	}
	if n := docs.CloseTabs(extension.DocumentTab{ID: "1:a"}); n != 0 {
		t.Errorf("closed %d", n)
	}
}

func testPanic(t *testing.T, h *Harness) {
	p := provider("tst")
	p.Text = func(context.Context, string) (string, error) { panic("on purpose") }
	if _, err := h.API.Documents().ProvideContent(p); err != nil {
		t.Fatal(err)
	}
	_, err := h.Text(context.Background(), extension.ContentURI("tst", "x"))
	wantTag(t, err, "ExtensionContentPanicked")
	h.Pump()
	if !h.Failed() {
		t.Error("the extension is still on after a panic in its provider")
	}
}
