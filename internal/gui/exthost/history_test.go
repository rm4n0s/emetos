package exthost

import (
	"context"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/historytest"
	"github.com/rm4n0s/emetos/internal/core/scm"
)

// historyHarness makes the harness of the conformance suite over a tab of the host.
func historyHarness(t *testing.T) *historytest.Harness {
	t.Helper()
	_, tb, api := grabAPI(t, "emetos.historytest", nil)
	var changes = map[string][]extension.HistoryRefsChange{}
	tb.scm.Subscribe(func(e scm.Event) {
		if e.Kind == scm.HistoryRefsChanged && e.Refs != nil {
			changes[e.Source.Key()] = append(changes[e.Source.Key()], extension.HistoryRefsChange{
				Added: extRefs(e.Refs.Added), Removed: extRefs(e.Refs.Removed), Modified: extRefs(e.Refs.Modified), Silent: e.Refs.Silent,
			})
		}
	})
	return &historytest.Harness{
		API: api,
		Ask: func(handle string) *historytest.View {
			src := tb.scm.Source(handle)
			if src == nil || src.HistoryProvider() == nil {
				return nil
			}
			p := src.HistoryProvider()
			refs := src.HistoryRefs()
			return &historytest.View{
				Items: func(ctx context.Context, o extension.HistoryOptions) ([]extension.HistoryItem, error) {
					items, err := p.Items(ctx, scm.HistoryQuery{Skip: o.Skip, Limit: o.Limit, RefIDs: o.RefIDs, Filter: o.FilterText})
					var out []extension.HistoryItem
					for _, it := range items {
						e := extension.HistoryItem{ID: it.ID, ParentIDs: it.ParentIDs, Subject: it.Subject, Message: it.Message, DisplayID: it.DisplayID,
							Author: it.Author, AuthorEmail: it.AuthorEmail, Timestamp: it.Timestamp, References: extRefs(it.Refs)}
						if it.Stats != nil {
							e.Statistics = &extension.HistoryStatistics{Files: it.Stats.Files, Insertions: it.Stats.Insertions, Deletions: it.Stats.Deletions}
						}
						out = append(out, e)
					}
					return out, err
				},
				Refs: func(ctx context.Context, ids []string) ([]extension.HistoryItemRef, error) {
					list, err := p.Refs(ctx, ids)
					return extRefs(list), err
				},
				CommonAncestor: p.CommonAncestor,
				OpenCommand:    p.OpenCommand(),
				Current:        extRef(refs.Current), Remote: extRef(refs.Remote), Base: extRef(refs.Base),
				Changes: changes[handle],
			}
		},
		Pump:       tb.pump,
		Deactivate: func() { tb.disabled = []string{"emetos.historytest"}; tb.scope.Refresh() },
		Failed:     func() bool { return tb.scope.State("emetos.historytest") == StateFailed },
	}
}

func extRef(r *scm.HistoryRef) *extension.HistoryItemRef {
	if r == nil {
		return nil
	}
	return &extension.HistoryItemRef{ID: r.ID, Name: r.Name, Revision: r.Revision, Category: r.Category, Description: r.Description, Icon: r.Icon}
}

func extRefs(list []scm.HistoryRef) []extension.HistoryItemRef {
	var out []extension.HistoryItemRef
	for i := range list {
		out = append(out, *extRef(&list[i]))
	}
	return out
}

// TestHistoryConformance runs the suite that the fake of the tests of the extensions passes too.
func TestHistoryConformance(t *testing.T) { historytest.Run(t, historyHarness) }

func TestHistoryRoutesOfTheFailures(t *testing.T) {
	ok := extension.HistoryProvider{
		Items:          func(context.Context, extension.HistoryOptions) ([]extension.HistoryItem, error) { return nil, nil },
		Refs:           func(context.Context, []string) ([]extension.HistoryItemRef, error) { return nil, nil },
		CommonAncestor: func(context.Context, []string) (string, error) { return "", nil },
	}
	create := func(t *testing.T, api extension.API) extension.SourceControl {
		t.Helper()
		sc, err := api.SourceControl().Create(extension.SourceControlSpec{ID: "git", Label: "Git", RootURI: "file:///work/repo"})
		if err != nil {
			t.Fatal(err)
		}
		return sc
	}
	t.Run("invalid", func(t *testing.T) {
		_, _, api := grabAPI(t, "emetos.historytest", nil)
		_, err := create(t, api).SetHistoryProvider(extension.HistoryProvider{})
		mustRoute(t, err, "scmSource.SetHistoryProvider.ExtensionHistoryInvalid")
	})
	t.Run("no Graph view", func(t *testing.T) {
		_, _, api := grabAPI(t, "emetos.historytest", func(r *rig) { r.noHistory["one"] = true })
		_, err := create(t, api).SetHistoryProvider(ok)
		mustRoute(t, err, "scmSource.SetHistoryProvider.ExtensionHistoryUnavailable")
	})
	t.Run("duplicate", func(t *testing.T) {
		_, _, api := grabAPI(t, "emetos.historytest", nil)
		sc := create(t, api)
		if _, err := sc.SetHistoryProvider(ok); err != nil {
			t.Fatal(err)
		}
		_, err := sc.SetHistoryProvider(ok)
		mustRoute(t, err, "scmSource.SetHistoryProvider.ExtensionHistoryDuplicate")
	})
	t.Run("disposed", func(t *testing.T) {
		_, _, api := grabAPI(t, "emetos.historytest", nil)
		sc := create(t, api)
		sc.Dispose()
		_, err := sc.SetHistoryProvider(ok)
		mustRoute(t, err, "scmSource.SetHistoryProvider.ExtensionSCMDisposed")
	})
	t.Run("a panic", func(t *testing.T) {
		_, tb, api := grabAPI(t, "emetos.historytest", nil)
		sc := create(t, api)
		p := ok
		p.Refs = func(context.Context, []string) ([]extension.HistoryItemRef, error) { panic("boom") }
		if _, err := sc.SetHistoryProvider(p); err != nil {
			t.Fatal(err)
		}
		_, err := tb.scm.Source(sc.Handle()).HistoryProvider().Refs(context.Background(), nil)
		mustRoute(t, err, "historyProvider.contain.func1.ExtensionHistoryPanicked")
	})
	t.Run("a foreign source control", func(t *testing.T) {
		_, tb, api := grabAPI(t, "emetos.historytest", nil)
		sc := create(t, api)
		src := tb.scm.Source(sc.Handle())
		foreign := &scmSource{inst: tb.scope.slots[0].inst, src: src, key: "x", model: tb.scm}
		src2 := tb.scm.Add(scm.SourceSpec{ProviderID: "other", Label: "Other", Owner: "someone.else"})
		foreign.src = src2
		_, err := foreign.SetHistoryProvider(ok)
		mustRoute(t, err, "scmSource.SetHistoryProvider->Scope.scmOwned.ExtensionSCMForeign")
	})
}
