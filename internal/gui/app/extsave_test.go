package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/document"
)

// Dirty and SaveAll of the documents an extension sees (step 24.18).

// saveAllIn runs SaveAll on a goroutine, as an extension does, and waits for its answer while the window works.
func saveAllIn(a *saveApp, docs extensionDocuments, ctx context.Context, uris ...string) error {
	a.t.Helper()
	done := make(chan error, 1)
	go func() { done <- docs.SaveAll(ctx, uris) }()
	var err error
	a.waitFor("SaveAll", func() bool {
		select {
		case err = <-done:
			return true
		default:
			return false
		}
	})
	return err
}

func TestDirtyListsTheFilesWithUnsavedChangesAndSaveAllSavesThem(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.go": "package a\n", "b.go": "package b\n", "c.go": "package c\n"}, "a.go", "b.go", "c.go")
	docs := extensionDocuments{w: a.win}
	uri := func(n string) string { return document.FileURI(a.files[n]).String() }
	if got := docs.Dirty(); len(got) != 0 {
		t.Fatalf("dirty %v with nothing typed", got)
	}
	// change a.go and c.go
	a.edit("c.go")
	a.edit("a.go")
	if got := docs.Dirty(); len(got) != 2 || got[0] != uri("a.go") || got[1] != uri("c.go") {
		t.Fatalf("dirty %v, want a.go and c.go in the order they were opened", got)
	}
	// b.go is clean: it is skipped; a.go is saved, c.go is not asked for
	if err := saveAllIn(a, docs, context.Background(), uri("a.go"), uri("b.go"), "file:///not/open.go"); err != nil {
		t.Fatal(err)
	}
	if got := docs.Dirty(); len(got) != 1 || got[0] != uri("c.go") {
		t.Errorf("dirty after the save %v", got)
	}
	if readFile(t, a.files["a.go"]) != "xpackage a\n" {
		t.Errorf("a.go on disk: %q", readFile(t, a.files["a.go"]))
	}
	if readFile(t, a.files["c.go"]) != "package c\n" {
		t.Errorf("c.go was written: %q", readFile(t, a.files["c.go"]))
	}
	if err := saveAllIn(a, docs, context.Background(), uri("c.go")); err != nil || len(docs.Dirty()) != 0 {
		t.Errorf("save of the rest: %v, dirty %v", err, docs.Dirty())
	}
	if err := saveAllIn(a, docs, context.Background()); err != nil {
		t.Errorf("save of nothing: %v", err)
	}
	a.noErrors()
}

func TestSaveAllFailsForADocumentThatIsNotSaved(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.go": "package a\n"}, "a.go")
	docs := extensionDocuments{w: a.win}
	a.edit("a.go")
	a.doc("a.go").SetReadOnly(true)
	err := saveAllIn(a, docs, context.Background(), document.FileURI(a.files["a.go"]).String())
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("extensionDocuments.saveAllOnUI.func1.ExtDocumentNotSaved") {
		t.Errorf("save of a read-only document: %v", err)
	}
	if len(docs.Dirty()) != 1 {
		t.Error("the document is not dirty any more")
	}
	// a URI that does not parse
	err = saveAllIn(a, docs, context.Background(), "not a uri")
	if err == nil {
		t.Error("a URI that does not parse was accepted")
	}
}

func TestSaveAllEndsWithItsContext(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.go": "package a\n"}, "a.go")
	docs := extensionDocuments{w: a.win}
	a.edit("a.go")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// the window is not pumped meanwhile, so it cannot answer: the context ends the wait
	err := docs.SaveAll(ctx, []string{document.FileURI(a.files["a.go"]).String()})
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("extensionDocuments.SaveAll.ExtDocumentSaveCancelled") {
		t.Errorf("error %v", err)
	}
	// what was posted runs when the window works again, and harms nothing
	a.pump()
	a.noErrors()
}

func TestSaveAllOfAWindowThatIsNotOnScreen(t *testing.T) {
	p := extensionProcess(t)
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(folder, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	win := newExtensionWorkspace(t, p, folder)
	docs := extensionDocuments{w: win}
	if got := docs.Dirty(); got != nil {
		t.Errorf("dirty %v", got)
	}
	err := docs.SaveAll(context.Background(), []string{document.FileURI(filepath.Join(folder, "a.go")).String()})
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("extensionDocuments.saveAllOnUI.ExtDocumentSaveUnavailable") {
		t.Errorf("save: %v", err)
	}
}
