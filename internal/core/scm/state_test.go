package scm

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
)

// addRepo adds a source control for a root under /work.
func addRepo(m *Model, name string) *Source {
	return m.Add(SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/" + name, Owner: "e"})
}

func visibleNames(m *Model) []string { return names(m.Visible()) }

// TestStateRoundTrip checks what State saves: the repositories in their order, which are shown and in what order, the order of the
// repositories and the choices of the view, and that it reads back as JSON.
func TestStateRoundTrip(t *testing.T) {
	m := NewModel(DefaultConfig())
	a, b, c := addRepo(m, "a"), addRepo(m, "b"), addRepo(m, "c")
	m.SetVisible([]*Source{c, a})
	m.SetViewMode(ViewTree)
	m.SetViewMode(ViewList)
	m.SetSortKey(SortByStatus)
	_ = b
	st := m.State()
	want := State{
		All:     []string{a.StorageKey(), b.StorageKey(), c.StorageKey()},
		Visible: []int{2, 0}, RepositoriesSortKey: SortDiscoveryTime, ViewMode: ViewList, ViewSortKey: SortByStatus,
	}
	if !reflect.DeepEqual(st, want) {
		t.Fatalf("State = %+v, want %+v", st, want)
	}
	data, err := json.Marshal(st)
	if err != nil {
		t.Fatal(err)
	}
	var back State
	if err := json.Unmarshal(data, &back); err != nil || !reflect.DeepEqual(back, want) {
		t.Errorf("round trip: %+v %v", back, err)
	}
	// the user has chosen nothing: the choices are not part of the state
	empty := NewModel(DefaultConfig()).State()
	if empty.ViewMode != "" || empty.ViewSortKey != "" {
		t.Errorf("an untouched model saves no choice: %+v", empty)
	}
}

// TestRestore checks that the saved state is found again by the provider and the root of each repository, whatever order the extensions
// create them in, and what a repository that the state does not know does.
func TestRestore(t *testing.T) {
	saved := State{All: []string{"git\x00file:///work/a", "git\x00file:///work/b", "git\x00file:///work/c"}, Visible: []int{2, 1}}
	tests := []struct {
		name      string
		mode      SelectionMode
		add       []string
		wantShown []string
	}{
		{"the repositories that were shown are shown again", SelectMultiple, []string{"a", "b", "c"}, []string{"c", "b"}},
		{"in any order of arrival", SelectMultiple, []string{"b", "a", "c"}, []string{"b", "c"}},
		{"a repository that was hidden stays hidden once one that was shown came", SelectMultiple, []string{"c", "a"}, []string{"c"}},
		{"a repository the state does not know shows all", SelectMultiple, []string{"c", "d"}, []string{"c", "d"}},
		{"the single selection mode keeps the first of the shown", SelectSingle, []string{"a", "b", "c"}, []string{"c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.SelectionMode = tt.mode
			m := NewModel(cfg)
			m.Restore(saved)
			for _, n := range tt.add {
				addRepo(m, n)
			}
			got := visibleNames(m)
			slices.Sort(got)
			want := slices.Clone(tt.wantShown)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("shown = %v, want %v", got, want)
			}
		})
	}
}

// TestRestoreEnds checks that a repository that comes after FinishRestore is a new repository again.
func TestRestoreEnds(t *testing.T) {
	m := NewModel(DefaultConfig())
	m.Restore(State{All: []string{"git\x00file:///work/a"}, Visible: []int{0}})
	addRepo(m, "a")
	m.FinishRestore()
	addRepo(m, "z")
	if got := visibleNames(m); !slices.Equal(got, []string{"a", "z"}) {
		t.Errorf("shown = %v: a new repository is shown", got)
	}
}

// TestRestoreChoices checks that the view mode and the sort keys of the state apply at once, and that a model that was closed before
// its extensions started gives the state back.
func TestRestoreChoices(t *testing.T) {
	m := NewModel(DefaultConfig())
	m.Restore(State{ViewMode: ViewTree, ViewSortKey: SortByName, RepositoriesSortKey: SortRepositoryName})
	if m.ViewMode() != ViewTree || m.RepositorySortKey() != SortRepositoryName {
		t.Errorf("mode %v, repositories %v", m.ViewMode(), m.RepositorySortKey())
	}
	m.SetViewMode(ViewList)
	if m.SortKey() != SortByName {
		t.Errorf("the list uses the saved sort key: %v", m.SortKey())
	}
	closed := NewModel(DefaultConfig())
	saved := State{All: []string{"git\x00file:///work/a"}, Visible: []int{0}}
	closed.Restore(saved)
	if got := closed.State(); !reflect.DeepEqual(got.All, saved.All) || !reflect.DeepEqual(got.Visible, saved.Visible) {
		t.Errorf("a model with no source control keeps the state it was given: %+v", got)
	}
	closed.Restore(State{ViewMode: "weird"})
	if closed.ViewMode() != ViewList {
		t.Errorf("an unknown view mode is ignored: %v", closed.ViewMode())
	}
}

// TestTreeNodeOf checks the node of a resource in the tree and the folders that hold it.
func TestTreeNodeOf(t *testing.T) {
	rs := resources("/repo/a/b/c.go", "/repo/a/d.go", "/repo/e.go")
	tree := NewTree(rs, "/repo")
	n := tree.NodeOf(&rs[0])
	if n == nil || n.Name() != "c.go" || n.Resource() == nil {
		t.Fatalf("NodeOf = %+v", n)
	}
	var folders []string
	for _, a := range n.Ancestors() {
		folders = append(folders, a.Name())
	}
	if !slices.Equal(folders, []string{"a", "b"}) {
		t.Errorf("Ancestors = %v", folders)
	}
	if top := tree.NodeOf(&rs[2]); top == nil || len(top.Ancestors()) != 0 {
		t.Errorf("a file at the root has no folder: %+v", top)
	}
	other := resources("/repo/zzz/q.go")
	if tree.NodeOf(&other[0]) != nil {
		t.Error("a resource that is not in the tree has no node")
	}
}
