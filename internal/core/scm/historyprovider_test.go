package scm

import (
	"context"
	"reflect"
	"testing"
)

// stubHistory is a history provider that answers nothing: the model only holds it.
type stubHistory struct{ open string }

func (stubHistory) Items(context.Context, HistoryQuery) ([]HistoryItem, error) { return nil, nil }
func (stubHistory) Refs(context.Context, []string) ([]HistoryRef, error)       { return nil, nil }
func (stubHistory) CommonAncestor(context.Context, []string) (string, error)   { return "", nil }
func (s stubHistory) OpenCommand() string                                      { return s.open }

func collect(m *Model) *[]Event {
	var events []Event
	m.Subscribe(func(e Event) { events = append(events, e) })
	return &events
}

func TestTheModelHoldsTheHistoryProviderOfASourceControl(t *testing.T) {
	m := NewModel(DefaultConfig())
	a := m.Add(SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/a"})
	b := m.Add(SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/b"})
	events := collect(m)
	if m.HistoryProviderCount() != 0 || a.HistoryProvider() != nil {
		t.Fatal("a source control has a provider before it was given one")
	}
	p := stubHistory{open: "git.viewCommit"}
	if !a.SetHistoryProvider(p) {
		t.Fatal("the provider was refused")
	}
	if m.HistoryProviderCount() != 1 || a.HistoryProvider() == nil || b.HistoryProvider() != nil {
		t.Errorf("count %d", m.HistoryProviderCount())
	}
	if got := m.Context()["scm.historyProviderCount"]; got != 1 {
		t.Errorf("the context key scm.historyProviderCount = %v", got)
	}
	b.SetHistoryProvider(p)
	if m.HistoryProviderCount() != 2 {
		t.Errorf("count %d", m.HistoryProviderCount())
	}
	a.SetHistoryProvider(nil)
	if m.HistoryProviderCount() != 1 || a.HistoryProvider() != nil {
		t.Errorf("count %d after taking one away", m.HistoryProviderCount())
	}
	var kinds []EventKind
	for _, e := range *events {
		kinds = append(kinds, e.Kind)
	}
	if want := []EventKind{HistoryChanged, HistoryChanged, HistoryChanged}; !reflect.DeepEqual(kinds, want) {
		t.Errorf("events %v, want %v", kinds, want)
	}
	// a removed source control takes no provider, and its provider is gone from the count
	m.Remove(b)
	if m.HistoryProviderCount() != 0 || b.SetHistoryProvider(p) {
		t.Errorf("count %d, or a removed source control took a provider", m.HistoryProviderCount())
	}
}

func TestTheCurrentReferencesAreKeptAndToldOnlyWhenTheyChange(t *testing.T) {
	m := NewModel(DefaultConfig())
	a := m.Add(SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/a"})
	// a source control with no provider holds no references
	a.SetHistoryRefs(HistoryRefs{Current: &HistoryRef{ID: "refs/heads/main", Name: "main"}})
	if got := a.HistoryRefs(); got.Current != nil {
		t.Errorf("references %+v without a provider", got)
	}
	a.SetHistoryProvider(stubHistory{})
	events := collect(m)
	cur := &HistoryRef{ID: "refs/heads/main", Name: "main", Revision: "a1"}
	a.SetHistoryRefs(HistoryRefs{Current: cur})
	if got := a.HistoryRefs(); got.Current == nil || got.Current.Name != "main" || got.Current == cur {
		t.Errorf("references %+v (must be a copy)", got)
	}
	a.SetHistoryRefs(HistoryRefs{Current: &HistoryRef{ID: "refs/heads/main", Name: "main", Revision: "a1"}})
	if len(*events) != 1 {
		t.Errorf("%d events: the same references were told again", len(*events))
	}
	if got := m.Context()["scmActiveRepositoryBranchName"]; got != "main" {
		t.Errorf("scmActiveRepositoryBranchName = %v", got)
	}
	a.SetHistoryRefs(HistoryRefs{Current: &HistoryRef{ID: "refs/heads/main", Name: "main", Revision: "b2"}, Remote: &HistoryRef{ID: "refs/remotes/origin/main", Name: "origin/main"}})
	if len(*events) != 2 || a.HistoryRefs().Remote == nil {
		t.Errorf("%d events, references %+v", len(*events), a.HistoryRefs())
	}
}

func TestAChangeOfReferencesIsToldToTheSubscribers(t *testing.T) {
	m := NewModel(DefaultConfig())
	a := m.Add(SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/a"})
	events := collect(m)
	// nothing to tell without a provider
	a.NotifyHistoryRefs(HistoryRefsChange{Silent: true})
	if len(*events) != 0 {
		t.Fatalf("%d events for a source control with no provider", len(*events))
	}
	a.SetHistoryProvider(stubHistory{})
	*events = nil
	a.NotifyHistoryRefs(HistoryRefsChange{Silent: true, Added: []HistoryRef{{ID: "refs/tags/v1"}}})
	if len(*events) != 1 || (*events)[0].Kind != HistoryRefsChanged || (*events)[0].Refs == nil || !(*events)[0].Refs.Silent || (*events)[0].Source != a {
		t.Errorf("events %+v", *events)
	}
}

func TestTheArgumentsOfTheCommandsOfTheGraph(t *testing.T) {
	m := NewModel(DefaultConfig())
	a := m.Add(SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/a"})
	item := HistoryItem{ID: "a1", ParentIDs: []string{"b2"}, Subject: "S", Message: "S\n\nbody", DisplayID: "a1", Author: "Ada", AuthorEmail: "ada@example.com",
		Refs: []HistoryRef{{ID: "refs/heads/main", Name: "main", Revision: "a1", Category: "branches"}}}
	args := HistoryMenuArgs(a, item, "refs/heads/main")
	if len(args) != 3 {
		t.Fatalf("args %v", args)
	}
	hi := args[1].(map[string]any)["historyItem"].(map[string]any)
	if hi["id"] != "a1" || hi["subject"] != "S" || hi["author"] != "Ada" || !reflect.DeepEqual(hi["parentIds"], []any{"b2"}) {
		t.Errorf("item %v", hi)
	}
	refs := hi["references"].([]any)
	if len(refs) != 1 || refs[0].(map[string]any)["name"] != "main" {
		t.Errorf("references %v", refs)
	}
	if args[2].(map[string]any)["historyItemRef"] != "refs/heads/main" {
		t.Errorf("ref %v", args[2])
	}
	if got := HistoryMenuArgs(a, item, ""); len(got) != 2 {
		t.Errorf("args without a reference: %v", got)
	}
	rg := HistoryRangeArgs(a, "b", "h", "Title")
	if got := rg[1].(map[string]any)["historyRange"].(map[string]any); got["base"] != "b" || got["head"] != "h" || got["title"] != "Title" {
		t.Errorf("range %v", got)
	}
}
