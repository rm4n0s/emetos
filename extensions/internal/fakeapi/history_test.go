package fakeapi

import (
	"context"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/historytest"
)

// TestHistoryConformance runs the suite that the host passes too (internal/gui/exthost/history_test.go).
func TestHistoryConformance(t *testing.T) {
	historytest.Run(t, func(t *testing.T) *historytest.Harness {
		a := New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
		return &historytest.Harness{
			API: a,
			Ask: func(handle string) *historytest.View {
				src := a.SourceControlByHandle(handle)
				if src == nil || src.History() == nil {
					return nil
				}
				h := src.History()
				cur, rem, base := h.CurrentRefs()
				return &historytest.View{
					Items: func(ctx context.Context, o extension.HistoryOptions) ([]extension.HistoryItem, error) {
						items, err := h.Items(ctx, o)
						var out []extension.HistoryItem
						for _, it := range items {
							if it.ID != "" {
								out = append(out, it)
							}
						}
						return out, err
					},
					Refs: h.Refs, CommonAncestor: h.CommonAncestor, OpenCommand: h.OpenCommand(),
					Current: cur, Remote: rem, Base: base, Changes: h.RefsChanges(),
				}
			},
			Pump:       func() { a.Pump() },
			Deactivate: a.Deactivate,
			Failed:     a.Failed,
		}
	})
}

func TestHistoryUnavailable(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
	a.SetHistoryUnavailable(true)
	sc, err := a.SourceControl().Create(extension.SourceControlSpec{ID: "git", Label: "Git", RootURI: "file:///work/repo"})
	if err != nil {
		t.Fatal(err)
	}
	ok := extension.HistoryProvider{
		Items:          func(context.Context, extension.HistoryOptions) ([]extension.HistoryItem, error) { return nil, nil },
		Refs:           func(context.Context, []string) ([]extension.HistoryItemRef, error) { return nil, nil },
		CommonAncestor: func(context.Context, []string) (string, error) { return "", nil },
	}
	if _, err := sc.SetHistoryProvider(ok); err == nil {
		t.Fatal("a scope with no Graph accepted a provider")
	}
}
