// Package contenttest is the conformance suite of the provided documents of the extension contract (Documents().ProvideContent,
// OpenDiff, OpenDocument, Tabs and CloseTabs, and ProvideMultiDiff and OpenMultiDiff of the multi-file diff editor): the host runs it over a window tab, and the fake of the tests of the extensions runs it
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
	// MultiFiles asks the window for the files of a set of the multi-file diff editor, as it does when it opens the set, from any
	// goroutine; the error has the tag ExtMultiDiffKindUnknown when no provider has the kind. MultiOpened and MultiChanged are the
	// editors the window was asked to open and what the provider said changed, since the start.
	MultiFiles   func(ctx context.Context, kind, key string) ([]extension.MultiDiffResource, error)
	MultiOpened  func() []extension.MultiDiffSpec
	MultiChanged func() [][]string
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
	multi := []struct {
		name string
		run  func(t *testing.T, h *Harness)
	}{
		{"the multi-file diff provider makes the files of a set", testMultiProvide},
		{"the same kind twice is refused, and Dispose frees it", testMultiKindTaken},
		{"a kind that is not a word, and a provider with no Resources, are refused", testMultiInvalidProvider},
		{"OpenMultiDiff hands the window what it was given", testMultiOpen},
		{"OpenMultiDiff refuses what cannot open", testMultiOpenInvalid},
		{"Changed tells the window the keys", testMultiChanged},
		{"the end of the activation takes the multi-file diff provider away", testMultiDeactivate},
		{"a panic in Resources is an error for the caller and takes the extension off", testMultiPanic},
	}
	for _, tc := range multi {
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
	for _, how := range []extension.TabClose{extension.TabCloseAsk, extension.TabCloseSave, extension.TabCloseDiscard} {
		if n := docs.CloseTabsAs(how, extension.DocumentTab{ID: "1:a"}); n != 0 {
			t.Errorf("closed %d as %d", n, how)
		}
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

func multiProvider(kind string) extension.MultiDiffProvider {
	return extension.MultiDiffProvider{Kind: kind, Resources: func(_ context.Context, key string) ([]extension.MultiDiffResource, error) {
		return []extension.MultiDiffResource{{Original: "file:///" + key + "/a", Modified: "file:///" + key + "/b", Path: "src/a.go", Letter: "M"}}, nil
	}}
}

func testMultiProvide(t *testing.T, h *Harness) {
	if _, err := h.API.Documents().ProvideMultiDiff(multiProvider("tst.changes")); err != nil {
		t.Fatal(err)
	}
	files, err := h.MultiFiles(context.Background(), "tst.changes", "k1")
	if err != nil || len(files) != 1 || files[0].Original != "file:///k1/a" || files[0].FileName() != "a.go" || files[0].Folder() != "src" {
		t.Errorf("files %+v %v", files, err)
	}
	_, err = h.MultiFiles(context.Background(), "other", "k1")
	wantTag(t, err, "ExtMultiDiffKindUnknown")
}

func testMultiKindTaken(t *testing.T, h *Harness) {
	docs := h.API.Documents()
	reg, err := docs.ProvideMultiDiff(multiProvider("tst.changes"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = docs.ProvideMultiDiff(multiProvider("tst.changes"))
	wantTag(t, err, "ExtMultiDiffKindTaken")
	reg.Dispose()
	reg.Dispose()
	if _, err := h.MultiFiles(context.Background(), "tst.changes", "k"); err == nil {
		t.Error("a disposed provider still answers")
	}
	if _, err := docs.ProvideMultiDiff(multiProvider("tst.changes")); err != nil {
		t.Errorf("the kind is not free after Dispose: %v", err)
	}
}

func testMultiInvalidProvider(t *testing.T, h *Harness) {
	docs := h.API.Documents()
	for _, kind := range []string{"", "1a", "a b", "a_b", "a/b", ".a"} {
		_, err := docs.ProvideMultiDiff(multiProvider(kind))
		wantTag(t, err, "ExtensionMultiDiffInvalid")
	}
	_, err := docs.ProvideMultiDiff(extension.MultiDiffProvider{Kind: "tst"})
	wantTag(t, err, "ExtensionMultiDiffInvalid")
	if _, err := docs.ProvideMultiDiff(multiProvider("Tst.a-b2")); err != nil {
		t.Errorf("Tst.a-b2: %v", err)
	}
}

func testMultiOpen(t *testing.T, h *Harness) {
	docs := h.API.Documents()
	if _, err := docs.ProvideMultiDiff(multiProvider("tst.changes")); err != nil {
		t.Fatal(err)
	}
	set := extension.MultiDiffSpec{Title: "Git: Changes", Kind: "tst.changes", Key: "k1", Reveal: "file:///k1/b", Preview: true}
	fixed := extension.MultiDiffSpec{Title: "Two files", Resources: []extension.MultiDiffResource{{Original: "file:///a", Modified: "file:///b", Path: "b"}, {Modified: "file:///c", Path: "c"}}}
	for _, spec := range []extension.MultiDiffSpec{set, fixed} {
		if err := docs.OpenMultiDiff(spec); err != nil {
			t.Fatal(err)
		}
	}
	h.Pump()
	got := h.MultiOpened()
	if len(got) != 2 || got[0].Key != "k1" || got[0].Title != "Git: Changes" || len(got[1].Resources) != 2 {
		t.Errorf("opened %+v", got)
	}
}

func testMultiOpenInvalid(t *testing.T, h *Harness) {
	docs := h.API.Documents()
	one := []extension.MultiDiffResource{{Modified: "file:///a", Path: "a"}}
	wantTag(t, docs.OpenMultiDiff(extension.MultiDiffSpec{}), "ExtensionMultiDiffInvalid")
	wantTag(t, docs.OpenMultiDiff(extension.MultiDiffSpec{Kind: "tst"}), "ExtensionMultiDiffInvalid")
	wantTag(t, docs.OpenMultiDiff(extension.MultiDiffSpec{Key: "k", Resources: one}), "ExtensionMultiDiffInvalid")
	wantTag(t, docs.OpenMultiDiff(extension.MultiDiffSpec{Kind: "tst", Key: "k", Resources: one}), "ExtensionMultiDiffInvalid")
	wantTag(t, docs.OpenMultiDiff(extension.MultiDiffSpec{Resources: []extension.MultiDiffResource{{Path: "a"}}}), "ExtensionMultiDiffInvalid")
	wantTag(t, docs.OpenMultiDiff(extension.MultiDiffSpec{Kind: "none", Key: "k"}), "ExtMultiDiffKindUnknown")
	if n := len(h.MultiOpened()); n != 0 {
		t.Errorf("%d editors opened", n)
	}
}

func testMultiChanged(t *testing.T, h *Harness) {
	reg, err := h.API.Documents().ProvideMultiDiff(multiProvider("tst.changes"))
	if err != nil {
		t.Fatal(err)
	}
	reg.Changed("k1", "k2")
	reg.Changed()
	h.Pump()
	got := h.MultiChanged()
	if len(got) < 1 || len(got[0]) != 2 || got[0][0] != "k1" || got[0][1] != "k2" {
		t.Errorf("changed %v", got)
	}
}

func testMultiDeactivate(t *testing.T, h *Harness) {
	docs := h.API.Documents()
	reg, err := docs.ProvideMultiDiff(multiProvider("tst.changes"))
	if err != nil {
		t.Fatal(err)
	}
	h.Deactivate()
	if _, err := h.MultiFiles(context.Background(), "tst.changes", "k"); err == nil {
		t.Error("the provider answers after the end of the activation")
	}
	reg.Changed("k") // nothing happens
	reg.Dispose()
	_, err = docs.ProvideMultiDiff(multiProvider("tst.other"))
	wantTag(t, err, "ExtensionDeactivated")
	wantTag(t, docs.OpenMultiDiff(extension.MultiDiffSpec{Kind: "tst.changes", Key: "k"}), "ExtensionDeactivated")
}

func testMultiPanic(t *testing.T, h *Harness) {
	p := multiProvider("tst.changes")
	p.Resources = func(context.Context, string) ([]extension.MultiDiffResource, error) { panic("on purpose") }
	if _, err := h.API.Documents().ProvideMultiDiff(p); err != nil {
		t.Fatal(err)
	}
	_, err := h.MultiFiles(context.Background(), "tst.changes", "k")
	wantTag(t, err, "ExtensionMultiDiffPanicked")
	h.Pump()
	if !h.Failed() {
		t.Error("the extension is still on after a panic in its provider")
	}
}
