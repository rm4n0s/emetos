package app

import (
	"context"
	"slices"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/gui/editor"
)

// A location in a document that an extension provides (the class files of jdtls, SPEC §10.34 "Java") opens as the file does, and a
// location in a document no provider makes is said not to open.

func TestALocationInAProvidedDocumentOpensItsTab(t *testing.T) {
	r := newContentRig(t, "a.go")
	uri := tst("contents/java.base/List.class")
	r.set(uri, "public interface List {\n}\n")
	loc := langfeatures.Location{URI: document.VirtualURI("/tst/contents/java.base/List.class"), Range: rng(1, 0, 1, 1)}
	r.shell().navOpen(loc, editor.OpenLocation{})
	waitFor(t, r.ts, "the provided document", func() bool { return slices.Contains(r.tabNames(), "List.class") })
	doc, ok := r.win.Documents.Get(loc.URI)
	if !ok || !doc.ReadOnly() || doc.Text() != "public interface List {\n}\n" {
		t.Fatalf("document open = %v", ok)
	}
	if got := r.line(); got != 1 { // counted from 0: the range of the location starts on the second line
		t.Errorf("the cursor is on line %d, want 1", got)
	}
	r.noErrors()
}

func TestALocationInADocumentNoProviderMakesIsSaidNotToOpen(t *testing.T) {
	r := newContentRig(t, "a.go")
	r.shell().navOpen(langfeatures.Location{URI: document.VirtualURI("/nobody/x.class")}, editor.OpenLocation{})
	if len(r.notifications()) != 1 {
		t.Errorf("notifications = %+v", r.notifications())
	}
	if len(r.tabs()) != 0 {
		t.Errorf("tabs = %v", r.tabNames())
	}
}

func TestAProvidedDocumentCanBeShownInThePeekView(t *testing.T) {
	r := newContentRig(t, "a.go")
	r.set(tst("contents/x/List.class"), "interface List {}\n")
	uri := document.VirtualURI("/tst/contents/x/List.class")
	var vm *editor.ViewModel
	var release func()
	var gotErr error
	r.shell().openEmbedded(context.Background(), uri, func(v *editor.ViewModel, rel func(), err error) { vm, release, gotErr = v, rel, err })
	waitFor(t, r.ts, "the editor", func() bool { return vm != nil || gotErr != nil })
	if gotErr != nil {
		t.Fatal(gotErr)
	}
	if !vm.IsEmbedded() || vm.Document().Text() != "interface List {}\n" {
		t.Errorf("embedded %v, text %q", vm.IsEmbedded(), vm.Document().Text())
	}
	release()
	if _, open := r.win.Documents.Get(uri); open {
		t.Error("the document stays open after the peek view let go of it")
	}
	// a document no provider makes is still refused
	var got error
	r.shell().openEmbedded(context.Background(), document.VirtualURI("/nobody/x"), func(_ *editor.ViewModel, _ func(), err error) { got = err })
	appErr, ok := errors.FromError(got)
	if !ok || !appErr.HasRoute("openEmbedded.PeekFileUnsupported") {
		t.Errorf("error = %v", got)
	}
	_ = extension.ContentScheme
}
