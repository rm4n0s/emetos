package exthost

import (
	"context"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/scm/quickdiff"
)

// quickDiffTab starts an extension with a source control rooted at /work/one and gives its API, the source control and the tab.
func quickDiffTab(t *testing.T, tweak func(r *rig)) (*tab, extension.API, extension.SourceControl) {
	t.Helper()
	_, tb, api := grabAPI(t, "emetos.quickdiff", tweak)
	sc, err := api.SourceControl().Create(extension.SourceControlSpec{ID: "git", Label: "Git", RootURI: "file:///work/one"})
	if err != nil {
		t.Fatal(err)
	}
	return tb, api, sc
}

func indexProvider(label string, kind extension.QuickDiffKind) extension.QuickDiffProvider {
	return extension.QuickDiffProvider{Label: label, Kind: kind, Original: func(_ context.Context, uri string) (string, error) {
		return "index:" + uri, nil
	}}
}

func TestQuickDiffProviderIsAskedForFilesBelowTheRootOfItsSourceControl(t *testing.T) {
	tb, api, sc := quickDiffTab(t, nil)
	tb.quickTexts["index:file:///work/one/a.go"] = "package a\n"
	reg, err := sc.AddQuickDiffProvider(indexProvider("Git Local Changes (Working Tree)", extension.QuickDiffPrimary))
	if err != nil {
		t.Fatal(err)
	}
	list, err := tb.quickDiff.Originals(context.Background(), "file:///work/one/a.go")
	if err != nil || len(list) != 1 || list[0].Label != "Git Local Changes (Working Tree)" || list[0].Text != "package a\n" {
		t.Fatalf("originals = %+v, %v", list, err)
	}
	if list, _ := tb.quickDiff.Originals(context.Background(), "file:///work/other/a.go"); len(list) != 0 {
		t.Errorf("a file outside the root has originals: %+v", list)
	}
	// the API of the documents answers with the primary original
	text, ok, err := api.Documents().QuickDiffOriginal(context.Background(), "file:///work/one/a.go")
	if err != nil || !ok || text != "package a\n" {
		t.Errorf("QuickDiffOriginal = %q, %v, %v", text, ok, err)
	}
	if _, ok, _ := api.Documents().QuickDiffOriginal(context.Background(), "file:///work/one/none.go"); ok {
		t.Error("QuickDiffOriginal found an original that has no text")
	}
	// disposing takes the provider away, and a second time does nothing
	reg.Dispose()
	reg.Dispose()
	if tb.quickDiff.Count() != 0 {
		t.Errorf("%d providers left after Dispose", tb.quickDiff.Count())
	}
	// and the kind is free again
	if _, err := sc.AddQuickDiffProvider(indexProvider("Again", extension.QuickDiffPrimary)); err != nil {
		t.Errorf("the kind was not freed: %v", err)
	}
}

func TestQuickDiffRefreshTellsTheModelsOfTheRoot(t *testing.T) {
	tb, _, sc := quickDiffTab(t, nil)
	if _, err := sc.AddQuickDiffProvider(indexProvider("P", extension.QuickDiffPrimary)); err != nil {
		t.Fatal(err)
	}
	var roots []string
	tb.quickDiff.Subscribe(func(ev quickdiff.Event) { roots = append(roots, ev.Root) })
	sc.RefreshQuickDiff()
	if len(roots) != 1 || roots[0] != "file:///work/one" {
		t.Errorf("refresh told %v, want the root of the source control", roots)
	}
}

func TestQuickDiffProvidersGoWithTheirSourceControlAndTheActivation(t *testing.T) {
	t.Run("the source control", func(t *testing.T) {
		tb, _, sc := quickDiffTab(t, nil)
		if _, err := sc.AddQuickDiffProvider(indexProvider("P", extension.QuickDiffPrimary)); err != nil {
			t.Fatal(err)
		}
		if _, err := sc.AddQuickDiffProvider(indexProvider("S", extension.QuickDiffSecondary)); err != nil {
			t.Fatal(err)
		}
		if tb.quickDiff.Count() != 2 {
			t.Fatalf("%d providers, want 2", tb.quickDiff.Count())
		}
		sc.Dispose()
		if tb.quickDiff.Count() != 0 {
			t.Errorf("%d providers left", tb.quickDiff.Count())
		}
		mustRoute(t, errOf(sc.AddQuickDiffProvider(indexProvider("P", extension.QuickDiffPrimary))), "scmSource.AddQuickDiffProvider.ExtensionSCMDisposed")
	})
	t.Run("the activation", func(t *testing.T) {
		tb, api, sc := quickDiffTab(t, nil)
		if _, err := sc.AddQuickDiffProvider(indexProvider("P", extension.QuickDiffPrimary)); err != nil {
			t.Fatal(err)
		}
		tb.disabled = []string{"emetos.quickdiff"}
		tb.scope.Refresh()
		if tb.quickDiff.Count() != 0 {
			t.Errorf("%d providers left after the activation ended", tb.quickDiff.Count())
		}
		mustRoute(t, errOf(sc.AddQuickDiffProvider(indexProvider("P", extension.QuickDiffPrimary))), "scmSource.AddQuickDiffProvider.ExtensionDeactivated")
		_, _, err := api.Documents().QuickDiffOriginal(context.Background(), "file:///work/one/a.go")
		mustRoute(t, err, "documentsAPI.QuickDiffOriginal.ExtensionDeactivated")
		sc.RefreshQuickDiff() // does nothing
	})
}

func errOf(_ extension.QuickDiffRegistration, err error) error { return err }

func TestQuickDiffProvidersFailWithTheirOwnTags(t *testing.T) {
	good := indexProvider("P", extension.QuickDiffPrimary)
	t.Run("invalid providers", func(t *testing.T) {
		_, _, sc := quickDiffTab(t, nil)
		for name, p := range map[string]extension.QuickDiffProvider{
			"no label":      {Kind: extension.QuickDiffPrimary, Original: good.Original},
			"a blank label": {Label: "  ", Kind: extension.QuickDiffPrimary, Original: good.Original},
			"no function":   {Label: "P", Kind: extension.QuickDiffPrimary},
			"an odd kind":   {Label: "P", Kind: 7, Original: good.Original},
		} {
			t.Run(name, func(t *testing.T) {
				mustRoute(t, errOf(sc.AddQuickDiffProvider(p)), "scmSource.AddQuickDiffProvider.ExtensionQuickDiffInvalid")
			})
		}
	})
	t.Run("a source control with no root", func(t *testing.T) {
		_, _, api := grabAPI(t, "emetos.quickdiff", nil)
		sc, err := api.SourceControl().Create(extension.SourceControlSpec{ID: "git", Label: "Git"})
		if err != nil {
			t.Fatal(err)
		}
		mustRoute(t, errOf(sc.AddQuickDiffProvider(good)), "scmSource.AddQuickDiffProvider.ExtensionQuickDiffInvalid")
	})
	t.Run("a second provider of a kind", func(t *testing.T) {
		_, _, sc := quickDiffTab(t, nil)
		if _, err := sc.AddQuickDiffProvider(good); err != nil {
			t.Fatal(err)
		}
		mustRoute(t, errOf(sc.AddQuickDiffProvider(good)), "scmSource.AddQuickDiffProvider.ExtensionQuickDiffDuplicate")
		if _, err := sc.AddQuickDiffProvider(indexProvider("S", extension.QuickDiffSecondary)); err != nil {
			t.Errorf("a provider of the other kind is refused: %v", err)
		}
	})
	t.Run("a scope with no editors", func(t *testing.T) {
		_, api, sc := quickDiffTab(t, func(r *rig) { r.noQuickDiff = map[string]bool{"one": true} })
		mustRoute(t, errOf(sc.AddQuickDiffProvider(good)), "scmSource.AddQuickDiffProvider.ExtensionQuickDiffUnavailable")
		_, _, err := api.Documents().QuickDiffOriginal(context.Background(), "file:///work/one/a.go")
		mustRoute(t, err, "documentsAPI.QuickDiffOriginal.ExtensionQuickDiffUnavailable")
		sc.RefreshQuickDiff() // does nothing
	})
}

func TestAPanicInAQuickDiffProviderTakesTheExtensionOff(t *testing.T) {
	tb, _, sc := quickDiffTab(t, nil)
	if _, err := sc.AddQuickDiffProvider(extension.QuickDiffProvider{Label: "P", Kind: extension.QuickDiffPrimary,
		Original: func(context.Context, string) (string, error) { panic("boom") }}); err != nil {
		t.Fatal(err)
	}
	// the service logs the failure of a provider and goes on without it; the extension is off
	list, err := tb.quickDiff.Originals(context.Background(), "file:///work/one/a.go")
	if err != nil || len(list) != 0 {
		t.Errorf("originals = %+v, %v; want none and no error", list, err)
	}
	tb.pump()
	if tb.scope.State("emetos.quickdiff") != StateFailed {
		t.Errorf("state = %v, want failed", tb.scope.State("emetos.quickdiff"))
	}
	if tb.quickDiff.Count() != 0 {
		t.Errorf("%d providers left after the extension failed", tb.quickDiff.Count())
	}
}

func TestQuickDiffProviderErrorTagOfAPanic(t *testing.T) {
	_, _, sc := quickDiffTab(t, nil)
	h := sc.(*scmSource)
	call := h.containOriginal(extension.QuickDiffProvider{Label: "P", Original: func(context.Context, string) (string, error) { panic("boom") }})
	_, err := call(context.Background(), "file:///work/one/a.go")
	appErr, ok := errors.FromError(err)
	if !ok || appErr.Tag != "ExtensionQuickDiffPanicked" || !appErr.HasRoute("containOriginal.func1.1.ExtensionQuickDiffPanicked") {
		t.Errorf("error = %v, route %s", err, appErr.Route())
	}
}

func TestLineChangesAreTheChangesOfTheMarks(t *testing.T) {
	_, api, _ := quickDiffTab(t, nil)
	tests := []struct {
		name               string
		original, modified string
		want               []extension.LineChange
	}{
		{"equal texts", "a\nb\n", "a\nb\n", nil},
		{"a modification", "a\nb\nc\n", "a\nB\nc\n", []extension.LineChange{{OriginalStartLineNumber: 2, OriginalEndLineNumber: 2, ModifiedStartLineNumber: 2, ModifiedEndLineNumber: 2}}},
		{"an insertion", "a\nc\n", "a\nb\nc\n", []extension.LineChange{{OriginalStartLineNumber: 1, OriginalEndLineNumber: 0, ModifiedStartLineNumber: 2, ModifiedEndLineNumber: 2}}},
		{"a deletion", "a\nb\nc\n", "a\nc\n", []extension.LineChange{{OriginalStartLineNumber: 2, OriginalEndLineNumber: 2, ModifiedStartLineNumber: 1, ModifiedEndLineNumber: 0}}},
		{"a deletion at the start", "a\nb\n", "b\n", []extension.LineChange{{OriginalStartLineNumber: 1, OriginalEndLineNumber: 1, ModifiedStartLineNumber: 0, ModifiedEndLineNumber: 0}}},
		{"CRLF in the original", "a\r\nb\r\n", "a\nB\n", []extension.LineChange{{OriginalStartLineNumber: 2, OriginalEndLineNumber: 2, ModifiedStartLineNumber: 2, ModifiedEndLineNumber: 2}}},
		{"white space at the end of a line counts", "a\nb\n", "a\nb  \n", []extension.LineChange{{OriginalStartLineNumber: 2, OriginalEndLineNumber: 2, ModifiedStartLineNumber: 2, ModifiedEndLineNumber: 2}}},
		{"an empty original", "", "a\n", []extension.LineChange{{OriginalStartLineNumber: 1, OriginalEndLineNumber: 1, ModifiedStartLineNumber: 1, ModifiedEndLineNumber: 2}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := api.Documents().LineChanges(context.Background(), tt.original, tt.modified)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("changes = %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("change %d = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestLineChangesFailWithTheirOwnTags(t *testing.T) {
	t.Run("a scope with no editors", func(t *testing.T) {
		_, api, _ := quickDiffTab(t, func(r *rig) { r.noQuickDiff = map[string]bool{"one": true} })
		_, err := api.Documents().LineChanges(context.Background(), "a\n", "b\n")
		mustRoute(t, err, "documentsAPI.LineChanges.ExtensionQuickDiffUnavailable")
	})
	t.Run("after the activation", func(t *testing.T) {
		tb, api, _ := quickDiffTab(t, nil)
		tb.disabled = []string{"emetos.quickdiff"}
		tb.scope.Refresh()
		_, err := api.Documents().LineChanges(context.Background(), "a\n", "b\n")
		mustRoute(t, err, "documentsAPI.LineChanges.ExtensionDeactivated")
	})
	t.Run("a context that is done", func(t *testing.T) {
		_, api, _ := quickDiffTab(t, nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := api.Documents().LineChanges(ctx, "a\nb\nc\n", "a\nx\nc\n"); err == nil {
			t.Error("a diff that was cancelled gave an answer")
		}
	})
}
