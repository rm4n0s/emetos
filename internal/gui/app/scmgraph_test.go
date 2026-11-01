package app

import (
	"context"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/internal/core/command"
	scmcore "github.com/rm4n0s/emetos/internal/core/scm"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
	"github.com/rm4n0s/emetos/internal/gui/views/scmgraph"
)

// The tests of the Graph view in the window (step 24.20): it comes with a history provider, shows the history of the active
// repository, its commands, its pickers, its context keys and its saved filters.

// graphHistory is a history provider with four commits that records what it was asked.
type graphHistory struct {
	mu      sync.Mutex
	queries []scmcore.HistoryQuery
}

var graphRefs = []scmcore.HistoryRef{
	{ID: "refs/heads/main", Name: "main", Revision: "c4", Category: "branches", Icon: "git-branch", Description: "c4"},
	{ID: "refs/heads/topic", Name: "topic", Revision: "c2", Category: "branches", Icon: "git-branch", Description: "c2"},
	{ID: "refs/tags/v1", Name: "v1", Revision: "c1", Category: "tags", Icon: "tag", Description: "Tag at c1"},
}

func (h *graphHistory) Items(_ context.Context, q scmcore.HistoryQuery) ([]scmcore.HistoryItem, error) {
	h.mu.Lock()
	h.queries = append(h.queries, q)
	h.mu.Unlock()
	items := []scmcore.HistoryItem{
		{ID: "c4", ParentIDs: []string{"c3"}, Subject: "Fourth", DisplayID: "c4", Author: "Ada Example", Timestamp: time.Now().Add(-time.Hour), Refs: graphRefs[:1]},
		{ID: "c3", ParentIDs: []string{"c1"}, Subject: "Third", DisplayID: "c3", Author: "Ada Example", Timestamp: time.Now().Add(-2 * time.Hour)},
		{ID: "c2", ParentIDs: []string{"c1"}, Subject: "Second", DisplayID: "c2", Author: "Grace Hopper", Timestamp: time.Now().Add(-3 * time.Hour), Refs: graphRefs[1:2]},
		{ID: "c1", Subject: "First", DisplayID: "c1", Author: "Ada Example", Timestamp: time.Now().Add(-4 * time.Hour), Refs: graphRefs[2:]},
	}
	if q.Skip >= len(items) {
		return nil, nil
	}
	return items[q.Skip:min(q.Skip+q.Limit, len(items))], nil
}

func (h *graphHistory) Refs(_ context.Context, ids []string) ([]scmcore.HistoryRef, error) {
	if ids == nil {
		return graphRefs, nil
	}
	var out []scmcore.HistoryRef
	for _, r := range graphRefs {
		if slices.Contains(ids, r.ID) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (h *graphHistory) CommonAncestor(context.Context, []string) (string, error) { return "c1", nil }
func (h *graphHistory) OpenCommand() string                                      { return "test.openCommit" }

func (h *graphHistory) asked() []scmcore.HistoryQuery {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.queries)
}

func (a *saveApp) graphPane() *scmgraph.Pane { return a.shell().graph.pane }

// graphApp is a window with a repository that has the history of graphHistory, the Source Control container open.
func graphApp(t *testing.T) (*saveApp, *scmcore.Source, *graphHistory) {
	t.Helper()
	a := newSaveApp(t, map[string]string{"a.go": "package a\n"})
	src := a.win.SCM.Add(scmcore.SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/repo"})
	h := &graphHistory{}
	src.SetHistoryProvider(h)
	src.SetHistoryRefs(scmcore.HistoryRefs{Current: &graphRefs[0]})
	a.pump()
	return a, src, h
}

func TestTheGraphViewComesWithAHistoryProvider(t *testing.T) {
	a := newSaveApp(t, map[string]string{"a.go": "package a\n"})
	a.run("workbench.view.scm")
	if a.ts.Exists(gluttertest.ByKey(scmgraph.SearchKey)) {
		t.Fatal("the Graph shows with no history provider")
	}
	if got := a.win.Keys.Get("scm.historyProviderCount"); got != 0.0 {
		t.Errorf("scm.historyProviderCount = %v", got)
	}
	src := a.win.SCM.Add(scmcore.SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/repo"})
	h := &graphHistory{}
	src.SetHistoryProvider(h)
	src.SetHistoryRefs(scmcore.HistoryRefs{Current: &graphRefs[0]})
	a.pump()
	if got := a.win.Keys.Get("scm.historyProviderCount"); got != 1.0 {
		t.Errorf("scm.historyProviderCount = %v with a provider", got)
	}
	if got := a.win.Keys.Get("scmActiveRepositoryBranchName"); got != "main" {
		t.Errorf("scmActiveRepositoryBranchName = %v", got)
	}
	a.waitFor("the commits", func() bool { return len(a.graphPane().Rows()) == 4 })
	if !a.ts.Exists(gluttertest.ByKey(scmgraph.SearchKey)) || !a.ts.Exists(gluttertest.ByKey(scmgraph.RowKey("c3"))) {
		t.Error("the Graph does not show its search box and its commits")
	}
	if got := a.win.Keys.Get("scmHistoryItemCount"); got != 4.0 {
		t.Errorf("scmHistoryItemCount = %v", got)
	}
	// the provider goes: the view goes with it
	src.SetHistoryProvider(nil)
	a.waitFor("the Graph to go", func() bool { return !a.ts.Exists(gluttertest.ByKey(scmgraph.SearchKey)) })
	a.noErrors()
}

func TestTheGraphReadsOnlyWhileItIsShown(t *testing.T) {
	a, _, h := graphApp(t)
	a.run("workbench.view.explorer")
	a.pump()
	if n := len(h.asked()); n != 0 {
		t.Fatalf("the Graph read %d pages while the Source Control container was hidden", n)
	}
	a.run("workbench.view.scm")
	a.waitFor("the first page", func() bool { return len(h.asked()) > 0 })
	if q := h.asked()[0]; q.Skip != 0 || q.Limit != 50 || !reflect.DeepEqual(q.RefIDs, []string{"c4"}) {
		t.Errorf("first query %+v", q)
	}
}

func TestTheSettingsOfTheGraphReachTheView(t *testing.T) {
	a, _, h := graphApp(t)
	a.setting("scm.graph.pageSize", 2)
	a.run("workbench.view.scm")
	a.waitFor("the first page", func() bool { return len(a.graphPane().Rows()) > 0 })
	if q := h.asked(); q[0].Limit != 2 {
		t.Errorf("limit = %d, want the setting 2", q[0].Limit)
	}
	a.setting("scm.graph.pageOnScroll", false)
	a.setting("scm.graph.pageSize", 3)
	a.waitFor("the read after the change", func() bool { return len(h.asked()) >= 2 })
	if q := h.asked(); q[len(q)-1].Limit != 3 {
		t.Errorf("limit = %d after the change, want 3", q[len(q)-1].Limit)
	}
}

func TestTheCommandsOfTheGraph(t *testing.T) {
	a, _, h := graphApp(t)
	a.run("workbench.view.explorer")
	a.run("workbench.scm.history.focus")
	if a.win.Views.Active() != command.ViewContainerSCM || a.win.Views.IsCollapsed(command.ViewSCMHistory) {
		t.Fatalf("workbench.scm.history.focus: the container is %q", a.win.Views.Active())
	}
	a.waitFor("the commits", func() bool { return len(a.graphPane().Rows()) == 4 })
	n := len(h.asked())
	a.run("workbench.scm.action.graph.refresh")
	a.waitFor("the refresh", func() bool { return len(h.asked()) > n })
	// the commit that the current branch is at is revealed, by the command
	a.run("workbench.scm.action.graph.revealCurrentHistoryItem")
	a.pump()
	// view changes opens the row of the commit with the command of the provider
	var ran []any
	if err := a.win.Commands.Register(command.Meta{ID: "test.openCommit", Title: "Open commit"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.win.Commands.SetHandler("test.openCommit", func(_ command.Context, args any) error { ran = append(ran, args); return nil }); err != nil {
		t.Fatal(err)
	}
	if err := a.win.Commands.Run(command.Context{Ctx: context.Background(), Source: command.SourceMenu}, "workbench.scm.action.graph.viewChanges",
		scmcore.HistoryMenuArgs(a.win.SCM.Sources()[0], scmcore.HistoryItem{ID: "c3", Subject: "Third"}, "")); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 {
		t.Fatalf("the command of the provider ran %d times", len(ran))
	}
	a.noErrors()
}

func TestThePickerOfTheReferencesChoosesTheFilter(t *testing.T) {
	a, src, h := graphApp(t)
	a.run("workbench.view.scm")
	a.waitFor("the commits", func() bool { return len(a.graphPane().Rows()) == 4 })
	a.run("workbench.scm.action.graph.pickHistoryItemRefs")
	a.waitFor("the picker", func() bool { return a.win.QuickInput.Visible() })
	labels := func() []string {
		var out []string
		for _, it := range a.win.QuickInput.Picker().Items() {
			if !it.Separator {
				out = append(out, it.Label)
			}
		}
		return out
	}
	if got, want := labels(), []string{"All", "$(check) Auto", "main", "topic", "v1"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("items = %q, want %q", got, want)
	}
	pick := func(label string) {
		t.Helper()
		for _, it := range a.win.QuickInput.Picker().Items() {
			if it.Label == label {
				it.Accept(quickinput.KeyMods{})
				return
			}
		}
		t.Fatalf("no item %q in %q", label, labels())
	}
	// two references are marked one by one, then applied
	pick("topic")
	a.waitFor("the picker again", func() bool { return a.win.QuickInput.Visible() && slices.Contains(labels(), "$(check) topic") })
	pick("v1")
	a.waitFor("the picker with two", func() bool { return a.win.QuickInput.Visible() && slices.Contains(labels(), "$(check) v1") })
	pick("$(check) Apply (2 selected)")
	a.waitFor("the filter", func() bool { return a.graphPane().Filter().Kind == scmgraph.FilterRefs })
	if got := a.graphPane().Filter().Refs; !reflect.DeepEqual(got, []string{"refs/heads/topic", "refs/tags/v1"}) {
		t.Errorf("filter refs = %v", got)
	}
	a.waitFor("the page of the filter", func() bool { q := h.asked(); return reflect.DeepEqual(q[len(q)-1].RefIDs, []string{"c2", "c1"}) })
	// the filter is saved with the window, by repository
	raw := a.shell().scmWindowState()
	st, err := decodeSCMState(raw)
	if err != nil || st.Graph[src.StorageKey()].Kind != scmgraph.FilterRefs {
		t.Fatalf("state %s (%v)", raw, err)
	}
	// All
	a.run("workbench.scm.action.graph.pickHistoryItemRefs")
	a.waitFor("the picker", func() bool { return a.win.QuickInput.Visible() })
	pick("All")
	a.waitFor("all", func() bool { return a.graphPane().Filter().Kind == scmgraph.FilterAll })
	a.noErrors()
}

func TestTheSavedFiltersOfTheGraphComeBackWithTheWindow(t *testing.T) {
	first, src, _ := graphApp(t)
	first.graphPane().SetFilter(scmgraph.Filter{Kind: scmgraph.FilterAll})
	raw := first.shell().scmWindowState()
	win := newTestWorkspace(t, "")
	win.restoreSCM(raw)
	second := newSaveAppWith(t, win, []string{"a.go"}, map[string]string{"a.go": "x\n"})
	again := second.win.SCM.Add(scmcore.SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/repo"})
	if again.StorageKey() != src.StorageKey() {
		t.Fatalf("the repository has another key: %q and %q", again.StorageKey(), src.StorageKey())
	}
	again.SetHistoryProvider(&graphHistory{})
	again.SetHistoryRefs(scmcore.HistoryRefs{Current: &graphRefs[0]})
	second.pump()
	second.run("workbench.view.scm")
	second.waitFor("the filter", func() bool { return second.graphPane().Filter().Kind == scmgraph.FilterAll })
	second.noErrors()
}

func TestThePickerOfTheRepositoriesListsTheOnesWithAHistory(t *testing.T) {
	a, _, _ := graphApp(t)
	other := a.win.SCM.Add(scmcore.SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/other"})
	other.SetHistoryProvider(&graphHistory{})
	other.SetHistoryRefs(scmcore.HistoryRefs{Current: &graphRefs[1]})
	// one with no history is not in the list
	a.win.SCM.Add(scmcore.SourceSpec{ProviderID: "svn", Label: "Svn", RootURI: "file:///work/svn"})
	a.run("workbench.view.scm")
	a.waitFor("the commits", func() bool { return len(a.graphPane().Rows()) > 0 })
	a.run("workbench.scm.action.graph.pickRepository")
	a.waitFor("the picker", func() bool { return a.win.QuickInput.Visible() })
	var labels []string
	for _, it := range a.win.QuickInput.Picker().Items() {
		if !it.Separator {
			labels = append(labels, it.Label)
		}
	}
	if want := []string{"Auto", "repo", "other"}; !reflect.DeepEqual(labels, want) {
		t.Fatalf("items = %q, want %q", labels, want)
	}
	for _, it := range a.win.QuickInput.Picker().Items() {
		if it.Label == "other" {
			it.Accept(quickinput.KeyMods{})
		}
	}
	a.waitFor("the other repository", func() bool { return a.graphPane().Selected() == other })
	a.noErrors()
}
