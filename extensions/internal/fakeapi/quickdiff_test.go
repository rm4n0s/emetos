package fakeapi

import (
	"context"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

// TestTheFakeQuickDiffFailsLikeTheHost checks the routes of the fake's errors, which have the tags of the host's.
func TestTheFakeQuickDiffFailsLikeTheHost(t *testing.T) {
	good := extension.QuickDiffProvider{Label: "P", Kind: extension.QuickDiffPrimary, Original: func(context.Context, string) (string, error) { return "", nil }}
	newSource := func(root string) (*API, *FakeSource) {
		a := New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
		sc, err := a.SourceControl().Create(extension.SourceControlSpec{ID: "git", Label: "Git", RootURI: root})
		if err != nil {
			t.Fatal(err)
		}
		return a, sc.(*FakeSource)
	}
	t.Run("invalid", func(t *testing.T) {
		_, s := newSource("file:///repo")
		_, err := s.AddQuickDiffProvider(extension.QuickDiffProvider{Kind: extension.QuickDiffPrimary})
		route(t, err, "FakeSource.AddQuickDiffProvider.ExtensionQuickDiffInvalid")
		_, err = s.AddQuickDiffProvider(extension.QuickDiffProvider{Label: "P", Kind: 9, Original: good.Original})
		route(t, err, "FakeSource.AddQuickDiffProvider.ExtensionQuickDiffInvalid")
		_, s = newSource("")
		_, err = s.AddQuickDiffProvider(good)
		route(t, err, "FakeSource.AddQuickDiffProvider.ExtensionQuickDiffInvalid")
	})
	t.Run("duplicate, freed by Dispose", func(t *testing.T) {
		_, s := newSource("file:///repo")
		reg, err := s.AddQuickDiffProvider(good)
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.AddQuickDiffProvider(good)
		route(t, err, "FakeSource.AddQuickDiffProvider.ExtensionQuickDiffDuplicate")
		reg.Dispose()
		if _, err := s.AddQuickDiffProvider(good); err != nil {
			t.Errorf("the kind was not freed: %v", err)
		}
		if got := s.QuickDiffProviders(); len(got) != 1 || got[0].Label() != "P" || got[0].Kind() != extension.QuickDiffPrimary || got[0].Root() != "file:///repo" {
			t.Errorf("providers = %+v", got)
		}
	})
	t.Run("unavailable", func(t *testing.T) {
		a, s := newSource("file:///repo")
		a.SetQuickDiffUnavailable(true)
		_, err := s.AddQuickDiffProvider(good)
		route(t, err, "FakeSource.AddQuickDiffProvider.ExtensionQuickDiffUnavailable")
		_, _, err = a.Documents().QuickDiffOriginal(context.Background(), "file:///repo/a.go")
		route(t, err, "documents.QuickDiffOriginal.ExtensionQuickDiffUnavailable")
	})
	t.Run("disposed and deactivated", func(t *testing.T) {
		a, s := newSource("file:///repo")
		reg, err := s.AddQuickDiffProvider(good)
		if err != nil {
			t.Fatal(err)
		}
		s.Dispose()
		if !reg.(*FakeQuickDiff).Disposed() || len(s.QuickDiffProviders()) != 0 {
			t.Error("the provider outlived its source control")
		}
		_, err = s.AddQuickDiffProvider(good)
		route(t, err, "FakeSource.AddQuickDiffProvider.ExtensionSCMDisposed")
		a.Deactivate()
		_, err = s.AddQuickDiffProvider(good)
		route(t, err, "FakeSource.AddQuickDiffProvider.ExtensionDeactivated")
		_, _, err = a.Documents().QuickDiffOriginal(context.Background(), "file:///repo/a.go")
		route(t, err, "documents.QuickDiffOriginal.ExtensionDeactivated")
	})
}

func TestTheFakeQuickDiffAnswersWithTheTextOfTheOriginal(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
	if _, err := a.Documents().ProvideContent(extension.ContentProvider{Scheme: "git", Text: func(_ context.Context, uri string) (string, error) { return "text of " + uri, nil }}); err != nil {
		t.Fatal(err)
	}
	sc, err := a.SourceControl().Create(extension.SourceControlSpec{ID: "git", Label: "Git", RootURI: "file:///repo"})
	if err != nil {
		t.Fatal(err)
	}
	s := sc.(*FakeSource)
	if _, err := s.AddQuickDiffProvider(extension.QuickDiffProvider{Label: "Index", Kind: extension.QuickDiffSecondary, Original: func(context.Context, string) (string, error) { return "unused", nil }}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddQuickDiffProvider(extension.QuickDiffProvider{Label: "Working Tree", Kind: extension.QuickDiffPrimary, Original: func(_ context.Context, uri string) (string, error) {
		if uri == "file:///repo/skip.go" {
			return "", nil
		}
		return extension.ContentURI("git", "id//a.go"), nil
	}}); err != nil {
		t.Fatal(err)
	}
	text, ok, err := a.Documents().QuickDiffOriginal(context.Background(), "file:///repo/a.go")
	if err != nil || !ok || text != "text of emetos-virtual:/git/id//a.go" {
		t.Errorf("QuickDiffOriginal = %q, %v, %v", text, ok, err)
	}
	for _, uri := range []string{"file:///repo/skip.go", "file:///elsewhere/a.go"} {
		if _, ok, err := a.Documents().QuickDiffOriginal(context.Background(), uri); ok || err != nil {
			t.Errorf("QuickDiffOriginal(%s) = ok %v, %v; want none", uri, ok, err)
		}
	}
	s.RefreshQuickDiff()
	s.RefreshQuickDiff()
	if s.QuickDiffRefreshes() != 2 {
		t.Errorf("%d refreshes, want 2", s.QuickDiffRefreshes())
	}
}

func TestAPanicInAFakeQuickDiffProviderTakesTheExtensionOff(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
	sc, err := a.SourceControl().Create(extension.SourceControlSpec{ID: "git", Label: "Git", RootURI: "file:///repo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sc.AddQuickDiffProvider(extension.QuickDiffProvider{Label: "P", Original: func(context.Context, string) (string, error) { panic("boom") }}); err != nil {
		t.Fatal(err)
	}
	// the panic is contained: the extension is off, and there is no original
	_, ok, err := a.Documents().QuickDiffOriginal(context.Background(), "file:///repo/a.go")
	if err != nil || ok || !a.Failed() {
		t.Errorf("err = %v, ok = %v, failed = %v; want no error, no original and a failed extension", err, ok, a.Failed())
	}
}

func TestTheFakeLineChangesFindTheBlocksOfTheHost(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
	tests := []struct {
		name               string
		original, modified string
		want               []extension.LineChange
	}{
		{"equal", "a\nb\n", "a\nb\n", nil},
		{"a modification", "a\nb\nc\n", "a\nB\nc\n", []extension.LineChange{{OriginalStartLineNumber: 2, OriginalEndLineNumber: 2, ModifiedStartLineNumber: 2, ModifiedEndLineNumber: 2}}},
		{"an insertion", "a\nc\n", "a\nb\nc\n", []extension.LineChange{{OriginalStartLineNumber: 1, OriginalEndLineNumber: 0, ModifiedStartLineNumber: 2, ModifiedEndLineNumber: 2}}},
		{"a deletion", "a\nb\nc\n", "a\nc\n", []extension.LineChange{{OriginalStartLineNumber: 2, OriginalEndLineNumber: 2, ModifiedStartLineNumber: 1, ModifiedEndLineNumber: 0}}},
		{"a deletion at the start", "a\nb\n", "b\n", []extension.LineChange{{OriginalStartLineNumber: 1, OriginalEndLineNumber: 1, ModifiedStartLineNumber: 0, ModifiedEndLineNumber: 0}}},
		{"CRLF", "a\r\nb\r\n", "a\nB\n", []extension.LineChange{{OriginalStartLineNumber: 2, OriginalEndLineNumber: 2, ModifiedStartLineNumber: 2, ModifiedEndLineNumber: 2}}},
		{"two blocks", "1\n2\n3\n4\n5\n", "1\nx\n3\n4\ny\n", []extension.LineChange{
			{OriginalStartLineNumber: 2, OriginalEndLineNumber: 2, ModifiedStartLineNumber: 2, ModifiedEndLineNumber: 2},
			{OriginalStartLineNumber: 5, OriginalEndLineNumber: 5, ModifiedStartLineNumber: 5, ModifiedEndLineNumber: 5}}},
	}
	for _, tt := range tests {
		got, err := a.Documents().LineChanges(context.Background(), tt.original, tt.modified)
		if err != nil || len(got) != len(tt.want) {
			t.Errorf("%s: %+v, %v; want %+v", tt.name, got, err, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("%s: change %d = %+v, want %+v", tt.name, i, got[i], tt.want[i])
			}
		}
	}
}

func TestTheFakeLineChangesFailLikeTheHost(t *testing.T) {
	t.Run("a scope with no editors", func(t *testing.T) {
		a := New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
		a.SetQuickDiffUnavailable(true)
		_, err := a.Documents().LineChanges(context.Background(), "a", "b")
		route(t, err, "documents.LineChanges.ExtensionQuickDiffUnavailable")
	})
	t.Run("a context that is done", func(t *testing.T) {
		a := New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := a.Documents().LineChanges(ctx, "a", "b")
		route(t, err, "documents.LineChanges.ExtensionContextDone")
	})
}
