package fakeapi

import (
	"context"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/contenttest"
)

// TestContentConformance runs the suite that the host passes too (internal/gui/exthost/content_test.go).
func TestContentConformance(t *testing.T) {
	contenttest.Run(t, func(t *testing.T) *contenttest.Harness {
		a := New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
		return &contenttest.Harness{
			API: a, Text: a.ContentText, Label: a.ContentLabel, Language: a.ContentLanguage,
			Diffs: a.OpenedDiffs, Docs: a.OpenedDocuments,
			Changed: func() [][]string {
				if f := a.ContentProviderFor("tst"); f != nil {
					return f.ContentChanges()
				}
				return nil
			},
			Deactivate: a.Deactivate, Failed: a.Failed, Pump: func() { a.Pump() },
			MultiFiles: a.MultiDiffFiles, MultiOpened: a.OpenedMultiDiffs,
			MultiChanged: func() [][]string {
				if f := a.MultiDiffProviderFor("tst.changes"); f != nil {
					return f.MultiDiffChanges()
				}
				return nil
			},
		}
	})
}

func TestTheFakeTabsAndRevealFollowTheHost(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
	a.SetTabs(extension.DocumentTab{ID: "1:a", Kind: "text"}, extension.DocumentTab{ID: "1:b", Kind: "diff"})
	if got := a.Documents().Tabs(); len(got) != 2 {
		t.Fatalf("tabs %v", got)
	}
	if n := a.Documents().CloseTabs(extension.DocumentTab{ID: "1:b"}, extension.DocumentTab{ID: "1:gone"}); n != 1 {
		t.Errorf("closed %d", n)
	}
	if got := a.ClosedTabs(); len(got) != 1 || got[0] != "1:b" || len(a.Documents().Tabs()) != 1 {
		t.Errorf("closed %v", got)
	}
	if err := a.Workspace().RevealFile("file:///work/a.go", true); err != nil {
		t.Fatal(err)
	}
	route(t, a.Workspace().RevealFile("emetos-virtual:/x", false), "workspace.RevealFile.ExtensionFilePathInvalid")
	if got := a.RevealedFiles(); len(got) != 1 || got[0].URI != "file:///work/a.go" || !got[0].InOS {
		t.Errorf("revealed %v", got)
	}
	a.FailOpen(context.Canceled)
	if err := a.Documents().OpenDocument(extension.OpenSpec{URI: "file:///a"}); err == nil {
		t.Error("OpenDocument did not fail")
	}
	a.FailOpen(nil)
}

func TestAPanicInAFakeProviderIsAnErrorWithItsRoute(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
	_, err := a.Documents().ProvideContent(extension.ContentProvider{Scheme: "tst", Text: func(context.Context, string) (string, error) { panic("text") }})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.ContentText(context.Background(), extension.ContentURI("tst", "x"))
	route(t, err, "API.safeText.func1.ExtensionContentPanicked")
	if !a.Failed() {
		t.Error("the fake is on after a panic")
	}
}

func TestTheFakeMultiDiffRefusalsHaveTheirRoutes(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
	docs := a.Documents()
	files := func(context.Context, string) ([]extension.MultiDiffResource, error) { return nil, nil }
	if _, err := docs.ProvideMultiDiff(extension.MultiDiffProvider{Kind: "tst.changes", Resources: files}); err != nil {
		t.Fatal(err)
	}
	_, err := docs.ProvideMultiDiff(extension.MultiDiffProvider{Kind: "tst.changes", Resources: files})
	route(t, err, "documents.ProvideMultiDiff.ExtMultiDiffKindTaken")
	_, err = docs.ProvideMultiDiff(extension.MultiDiffProvider{Kind: "9 no", Resources: files})
	route(t, err, "documents.ProvideMultiDiff.ExtensionMultiDiffInvalid")
	_, err = docs.ProvideMultiDiff(extension.MultiDiffProvider{Kind: "tst.other"})
	route(t, err, "documents.ProvideMultiDiff.ExtensionMultiDiffInvalid")
	route(t, docs.OpenMultiDiff(extension.MultiDiffSpec{}), "documents.OpenMultiDiff.ExtensionMultiDiffInvalid")
	route(t, docs.OpenMultiDiff(extension.MultiDiffSpec{Resources: []extension.MultiDiffResource{{Path: "a"}}}), "documents.OpenMultiDiff.ExtensionMultiDiffInvalid")
}

func TestAPanicInAFakeMultiDiffProviderIsAnErrorWithItsRoute(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
	_, err := a.Documents().ProvideMultiDiff(extension.MultiDiffProvider{Kind: "tst.changes", Resources: func(context.Context, string) ([]extension.MultiDiffResource, error) {
		panic("files")
	}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.MultiDiffFiles(context.Background(), "tst.changes", "k")
	route(t, err, "API.MultiDiffFiles.func1.ExtensionMultiDiffPanicked")
	if !a.Failed() {
		t.Error("the fake is on after a panic")
	}
}
