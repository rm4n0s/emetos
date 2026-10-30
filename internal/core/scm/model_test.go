package scm

import (
	"slices"
	"sync"
	"testing"
)

// repo adds a source control with a root under /work and one group holding n resources.
func repo(m *Model, name string, n int) *Source {
	s := m.Add(SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/" + name, Owner: "e"})
	g := s.AddGroup("workingTree", "Changes", false, "")
	rs := make([]Resource, n)
	for i := range rs {
		rs[i] = Resource{URI: "file:///work/" + name + "/f" + string(rune('a'+i)) + ".go"}
	}
	g.SetResources(rs)
	return s
}

func names(ss []*Source) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Name()
	}
	return out
}

func TestCountsAndBadges(t *testing.T) {
	tests := []struct {
		name       string
		cfg        func(*Config)
		counts     []int // the count a provider gives; -1 for none
		resources  []int
		hidden     []int // indexes of the repositories that are not shown
		active     int
		wantBadge  int
		wantHeader []int // BadgeCount of each
		wantShown  []bool
		providerBd ProviderCountBadge
	}{
		{"all: the resources of every repository shown", func(c *Config) {}, []int{-1, -1}, []int{3, 2}, nil, 0, 5, []int{3, 2}, []bool{false, false}, ProviderCountHidden},
		{"all: the count of a provider replaces its resources", func(c *Config) {}, []int{10, -1}, []int{3, 2}, nil, 0, 12, []int{10, 2}, []bool{false, false}, ProviderCountHidden},
		{"all: a count of 0 is a count", func(c *Config) {}, []int{0, -1}, []int{3, 2}, nil, 0, 2, []int{0, 2}, []bool{false, false}, ProviderCountHidden},
		{"all: a repository that is not shown does not count", func(c *Config) {}, []int{-1, -1}, []int{3, 2}, []int{1}, 0, 3, []int{3, 2}, []bool{false, false}, ProviderCountHidden},
		{"focused: only the active repository", func(c *Config) { c.CountBadge = CountFocused }, []int{-1, -1}, []int{3, 2}, nil, 1, 2, []int{3, 2}, nil, ProviderCountHidden},
		{"off: no badge", func(c *Config) { c.CountBadge = CountOff }, []int{-1, -1}, []int{3, 2}, nil, 0, 0, []int{3, 2}, nil, ProviderCountHidden},
		{"auto: the repository badge is shown when it is not 0", func(c *Config) { c.ProviderCountBadge = ProviderCountAuto }, []int{-1, 0}, []int{3, 2}, nil, 0, 3, []int{3, 0}, []bool{true, false}, ProviderCountAuto},
		{"visible: the repository badge is always shown", func(c *Config) { c.ProviderCountBadge = ProviderCountVisible }, []int{-1, 0}, []int{3, 2}, nil, 0, 3, []int{3, 0}, []bool{true, true}, ProviderCountVisible},
		{"hidden: never", func(c *Config) { c.ProviderCountBadge = ProviderCountHidden }, []int{-1, 0}, []int{3, 2}, nil, 0, 3, []int{3, 0}, []bool{false, false}, ProviderCountHidden},
		{"an unknown value is the default", func(c *Config) { c.CountBadge = "weird" }, []int{-1, -1}, []int{1, 1}, nil, 0, 2, []int{1, 1}, []bool{false, false}, ProviderCountHidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.cfg(&cfg)
			m := NewModel(cfg)
			var srcs []*Source
			for i, n := range tt.resources {
				srcs = append(srcs, repo(m, string(rune('a'+i)), n))
			}
			for i, c := range tt.counts {
				srcs[i].SetCount(c)
			}
			for _, i := range tt.hidden {
				m.ToggleVisibility(srcs[i], new(bool))
			}
			m.Pin(srcs[tt.active])
			if got := m.ActivityBadge(); got != tt.wantBadge {
				t.Errorf("ActivityBadge = %d, want %d", got, tt.wantBadge)
			}
			for i, want := range tt.wantHeader {
				if got := srcs[i].BadgeCount(); got != want {
					t.Errorf("BadgeCount of %d = %d, want %d", i, got, want)
				}
			}
			for i, want := range tt.wantShown {
				if got := m.ProviderCountVisible(srcs[i]); got != want {
					t.Errorf("ProviderCountVisible of %d = %v, want %v", i, got, want)
				}
			}
		})
	}
	if got := PendingChangesLabel(3); got != "3 pending changes" {
		t.Errorf("label = %q", got)
	}
}

func TestResourceCountFollowsTheGroups(t *testing.T) {
	m := NewModel(DefaultConfig())
	s := repo(m, "a", 2)
	idx := s.AddGroup("index", "Staged Changes", true, "")
	if s.ResourceCount() != 2 || s.BadgeCount() != 2 {
		t.Fatalf("count = %d", s.ResourceCount())
	}
	idx.SetResources(resources("/work/a/x.go", "/work/a/y.go", "/work/a/z.go"))
	if s.ResourceCount() != 5 || m.ActivityBadge() != 5 {
		t.Errorf("count = %d, badge = %d", s.ResourceCount(), m.ActivityBadge())
	}
	idx.Remove()
	idx.Remove() // twice does nothing
	if s.ResourceCount() != 2 || len(s.Groups()) != 1 {
		t.Errorf("count after Remove = %d, groups %d", s.ResourceCount(), len(s.Groups()))
	}
	if idx.Visible() {
		t.Error("a removed group is visible")
	}
	idx.SetResources(resources("/work/a/q.go")) // a removed group takes nothing
	if idx.Len() != 3 {
		t.Errorf("a removed group changed: %d", idx.Len())
	}
}

func TestGroupVisibility(t *testing.T) {
	_, s, _ := newGroup(t, nil)
	merge := s.AddGroup("merge", "Merge Changes", true, "")
	if merge.Visible() {
		t.Error("a group hidden when empty is visible while empty")
	}
	merge.SetResources(resources("/repo/a.go"))
	if !merge.Visible() {
		t.Error("a group with resources is hidden")
	}
	merge.SetResources(nil)
	merge.SetHideWhenEmpty(false)
	if !merge.Visible() {
		t.Error("a group not hidden when empty is hidden")
	}
	merge.SetLabel("Conflicts")
	merge.SetContextValue("merge")
	if merge.Label() != "Conflicts" || merge.ContextValue() != "merge" {
		t.Errorf("label %q context %q", merge.Label(), merge.ContextValue())
	}
	if s.Group("merge") != merge || s.Group("nope") != nil {
		t.Error("Group by id")
	}
}

func TestSelectionRules(t *testing.T) {
	type step struct {
		do      func(m *Model, s []*Source)
		visible []string
		focused string
	}
	tests := []struct {
		name  string
		mode  SelectionMode
		sort  string
		steps []step
	}{
		{"multiple: each new repository is shown, in the order they were found", "multiple", "discovery time", []step{
			{func(m *Model, s []*Source) {}, []string{"a", "b", "c"}, "a"},
			{func(m *Model, s []*Source) { m.ToggleVisibility(s[1], nil) }, []string{"a", "c"}, "a"},
			{func(m *Model, s []*Source) { m.ToggleVisibility(s[1], nil) }, []string{"a", "c", "b"}, "a"}, // by the order they were selected
			// the focus moves to the first of the repositories that are shown, in the order they were found
			{func(m *Model, s []*Source) { m.ToggleVisibility(s[0], new(bool)) }, []string{"c", "b"}, "b"},
			{func(m *Model, s []*Source) { m.Focus(s[0]) }, []string{"c", "b"}, "b"}, // a hidden repository is not focused
			{func(m *Model, s []*Source) { m.Focus(s[2]) }, []string{"c", "b"}, "c"},
			{func(m *Model, s []*Source) { m.SetVisible([]*Source{s[1], s[0]}) }, []string{"b", "a"}, "a"},
			{func(m *Model, s []*Source) { m.SetVisible(nil) }, []string{}, ""},
		}},
		{"multiple, sorted by name: the order of the repositories", "multiple", "name", []step{
			{func(m *Model, s []*Source) {}, []string{"a", "b", "c"}, "a"},
			{func(m *Model, s []*Source) { m.ToggleVisibility(s[0], nil); m.ToggleVisibility(s[0], nil) }, []string{"a", "b", "c"}, "b"},
		}},
		{"single: only the first is shown, showing one shows it alone", "single", "discovery time", []step{
			{func(m *Model, s []*Source) {}, []string{"a"}, "a"},
			{func(m *Model, s []*Source) { m.ToggleVisibility(s[2], nil) }, []string{"c"}, "c"},
			{func(m *Model, s []*Source) { m.ToggleVisibility(s[2], new(bool)) }, []string{}, ""},
			{func(m *Model, s []*Source) { m.ToggleVisibility(s[1], new(bool)) }, []string{}, ""}, // hiding what is hidden does nothing
		}},
		{"a change of the mode: single keeps the first, multiple shows all", "multiple", "discovery time", []step{
			{func(m *Model, s []*Source) { m.SetSelectionMode(SelectSingle) }, []string{"a"}, "a"},
			{func(m *Model, s []*Source) { m.SetSelectionMode(SelectMultiple) }, []string{"a", "b", "c"}, "a"},
		}},
		{"removing the shown repositories shows the first of the others", "multiple", "discovery time", []step{
			{func(m *Model, s []*Source) { m.Remove(s[0]) }, []string{"b", "c"}, "b"},
			{func(m *Model, s []*Source) { m.SetVisible([]*Source{s[2]}); m.Remove(s[2]) }, []string{"b"}, "b"},
			{func(m *Model, s []*Source) { m.Remove(s[1]); m.Remove(s[1]) }, []string{}, ""},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.SelectionMode, cfg.RepositoriesSortOrder = tt.mode, tt.sort
			m := NewModel(cfg)
			s := []*Source{repo(m, "a", 1), repo(m, "b", 1), repo(m, "c", 1)}
			if tt.mode == SelectSingle {
				// a single selection mode shows the first repository only
				if got := names(m.Visible()); !slices.Equal(got, []string{"a"}) {
					t.Fatalf("visible at the start = %v", got)
				}
			}
			for i, st := range tt.steps {
				st.do(m, s)
				if got := names(m.Visible()); !slices.Equal(got, st.visible) {
					t.Errorf("step %d: visible = %v, want %v", i, got, st.visible)
				}
				f := ""
				if x := m.Focused(); x != nil {
					f = x.Name()
				}
				if f != st.focused {
					t.Errorf("step %d: focused = %q, want %q", i, f, st.focused)
				}
			}
		})
	}
}

func TestRepositoriesAreSortedByTheSortKey(t *testing.T) {
	m := NewModel(DefaultConfig())
	for _, p := range []string{"zeta", "Alpha", "mid/alpha", "beta"} {
		m.Add(SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/" + p})
	}
	tests := []struct {
		key  RepositorySortKey
		want []string // the roots
	}{
		{SortDiscoveryTime, []string{"/work/zeta", "/work/Alpha", "/work/mid/alpha", "/work/beta"}},
		// by name, and the paths of equal names (Alpha, alpha) by comparePaths
		{SortRepositoryName, []string{"/work/Alpha", "/work/mid/alpha", "/work/beta", "/work/zeta"}},
		// comparePaths: a name of /work before the names below a folder of /work
		{SortRepositoryPath, []string{"/work/Alpha", "/work/beta", "/work/zeta", "/work/mid/alpha"}},
	}
	for _, tt := range tests {
		m.SetRepositorySortKey(tt.key)
		var roots []string
		for _, s := range m.Sources() {
			roots = append(roots, s.RootPath())
		}
		if !slices.Equal(roots, tt.want) {
			t.Errorf("sorted by %s: %v, want %v", tt.key, roots, tt.want)
		}
	}
	// a repository found later takes its place in the order
	m.Add(SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/Gamma"})
	if got := m.Sources()[2].RootPath(); got != "/work/Gamma" {
		t.Errorf("the third repository by path = %q", got)
	}
}

func TestActiveRepository(t *testing.T) {
	type step struct {
		do     func(m *Model, s []*Source)
		active string
		pinned bool
	}
	tests := []struct {
		name  string
		steps []step
	}{
		{"the focused repository, then the repository of the active editor, then the focus again", []step{
			{func(m *Model, s []*Source) {}, "a", false},
			{func(m *Model, s []*Source) { m.SetActiveResource("file:///work/b/fa.go") }, "b", false},
			{func(m *Model, s []*Source) { m.SetActiveResource("file:///elsewhere/x.go") }, "b", false}, // in no repository: the last stays
			{func(m *Model, s []*Source) { m.Focus(s[2]) }, "c", false},
			{func(m *Model, s []*Source) { m.SetActiveResource("file:///work/a/sub/deep.go") }, "a", false},
			{func(m *Model, s []*Source) { m.SetActiveResource("") }, "a", false},
		}},
		{"a pinned repository wins, and Auto gives it up", []step{
			{func(m *Model, s []*Source) { m.Pin(s[1]) }, "b", true},
			{func(m *Model, s []*Source) { m.SetActiveResource("file:///work/c/fa.go"); m.Focus(s[2]) }, "b", true},
			{func(m *Model, s []*Source) { m.Pin(nil) }, "c", false},
		}},
		{"a pinned repository that is removed is unpinned", []step{
			{func(m *Model, s []*Source) { m.Pin(s[1]); m.Remove(s[1]) }, "a", false},
		}},
		{"the repository with the deepest root holds a file", []step{
			{func(m *Model, s []*Source) {
				s[0].m.Add(SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/a/nested"})
				m.SetActiveResource("file:///work/a/nested/x.go")
			}, "nested", false},
			{func(m *Model, s []*Source) { m.SetActiveResource("file:///work/a/nested") }, "a", false}, // the root of nested is in a, not in nested
			{func(m *Model, s []*Source) { m.SetActiveResource("file:///work/a/other.go") }, "a", false},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel(DefaultConfig())
			s := []*Source{repo(m, "a", 1), repo(m, "b", 1), repo(m, "c", 1)}
			for i, st := range tt.steps {
				st.do(m, s)
				a, pinned := m.Active()
				got := ""
				if a != nil {
					got = a.Name()
				}
				if got != st.active || pinned != st.pinned {
					t.Errorf("step %d: active = %q pinned %v, want %q %v", i, got, pinned, st.active, st.pinned)
				}
			}
		})
	}
}

// TestActiveRepositoryNameOverride is the case of VS Code's activity.test.ts: the name of the active repository is the name that
// the provider set, and goes back to the name of the repository when the provider clears it.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/test/browser/activity.test.ts
func TestActiveRepositoryNameOverride(t *testing.T) {
	m := NewModel(DefaultConfig())
	s := m.Add(SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/worktree"})
	m.Pin(s)
	var names []any
	read := func() { names = append(names, m.Context()["scmActiveRepositoryName"]) }
	read()
	s.SetActiveRepositoryName("repository")
	read()
	s.SetActiveRepositoryName("")
	read()
	want := []any{"worktree", "repository", "worktree"}
	if !slices.Equal(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
	if s.ActiveRepositoryName() != "worktree" {
		t.Errorf("ActiveRepositoryName = %q", s.ActiveRepositoryName())
	}
}

func TestViewModeAndSortKey(t *testing.T) {
	tests := []struct {
		name         string
		cfg          func(*Config)
		do           func(m *Model)
		wantMode     ViewMode
		wantSortKey  SortKey
		wantCtxMode  string
		wantCtxStore string
	}{
		{"the defaults: a list sorted by path", func(c *Config) {}, func(m *Model) {}, ViewList, SortByPath, "list", "path"},
		{"the configured defaults", func(c *Config) { c.DefaultViewMode, c.DefaultViewSortKey = ViewTree, SortByName }, func(m *Model) {}, ViewTree, SortByPath, "tree", "path"},
		{"a list sorted by name by default", func(c *Config) { c.DefaultViewSortKey = SortByName }, func(m *Model) {}, ViewList, SortByName, "list", "name"},
		{"the user chooses a sort key for the list", func(c *Config) {}, func(m *Model) { m.SetSortKey(SortByStatus) }, ViewList, SortByStatus, "list", "status"},
		{"the tree is always sorted by path, and the list keeps the choice", func(c *Config) {}, func(m *Model) {
			m.SetSortKey(SortByStatus)
			m.SetViewMode(ViewTree)
		}, ViewTree, SortByPath, "tree", "path"},
		{"back to the list: the chosen key", func(c *Config) {}, func(m *Model) {
			m.SetSortKey(SortByName)
			m.SetViewMode(ViewTree)
			m.SetViewMode(ViewList)
		}, ViewList, SortByName, "list", "name"},
		{"the user's choice of the mode is not undone by a new default", func(c *Config) {}, func(m *Model) {
			m.SetViewMode(ViewList)
			cfg := m.Config()
			cfg.DefaultViewMode = ViewTree
			m.SetConfig(cfg)
		}, ViewList, SortByPath, "list", "path"},
		{"a new default applies when the user chose nothing", func(c *Config) {}, func(m *Model) {
			cfg := m.Config()
			cfg.DefaultViewMode = ViewTree
			m.SetConfig(cfg)
		}, ViewTree, SortByPath, "tree", "path"},
		{"an unknown sort key is path", func(c *Config) {}, func(m *Model) { m.SetSortKey("weird") }, ViewList, SortByPath, "list", "path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.cfg(&cfg)
			m := NewModel(cfg)
			tt.do(m)
			if m.ViewMode() != tt.wantMode || m.SortKey() != tt.wantSortKey {
				t.Errorf("mode %q sort %q, want %q %q", m.ViewMode(), m.SortKey(), tt.wantMode, tt.wantSortKey)
			}
			c := m.Context()
			if c["scmViewMode"] != tt.wantCtxMode || c["scmViewSortKey"] != tt.wantCtxStore {
				t.Errorf("context scmViewMode %v scmViewSortKey %v", c["scmViewMode"], c["scmViewSortKey"])
			}
		})
	}
}

func TestEventsAndRemoval(t *testing.T) {
	m := NewModel(DefaultConfig())
	var kinds []EventKind
	cancel := m.Subscribe(func(e Event) { kinds = append(kinds, e.Kind) })
	s := m.Add(SourceSpec{ProviderID: "git", Label: "Git"})
	g := s.AddGroup("g", "G", false, "")
	g.SetResources(resources("/a"))
	s.SetCount(3)
	s.SetCount(3) // no change, no event
	s.Input().SetValue("msg")
	s.Input().SetValue("msg")
	s.Input().Focus()
	m.Remove(s)
	s.SetCount(7) // a removed source control is not changed
	s.Input().SetValue("later")
	g.SetResources(resources("/b"))
	cancel()
	m.Add(SourceSpec{ProviderID: "git", Label: "Git"}) // not seen
	want := []EventKind{SourceAdded, GroupsChanged, ResourcesChanged, SourceChanged, InputChanged, InputChanged, SourceRemoved}
	if !slices.Equal(kinds, want) {
		t.Errorf("events = %v, want %v", kinds, want)
	}
	if !s.Removed() || s.Count() != 3 || s.Input().Value() != "msg" || g.Len() != 1 || g.Resources()[0].Path != "/a" {
		t.Errorf("removed source changed: removed %v count %d input %q group %v", s.Removed(), s.Count(), s.Input().Value(), g.Resources())
	}
	if m.Count() != 1 || m.Source(s.Key()) != nil {
		t.Errorf("count %d, lookup %v", m.Count(), m.Source(s.Key()))
	}
}

func TestSourceProperties(t *testing.T) {
	m := NewModel(DefaultConfig())
	s := m.Add(SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/My Repo/", ContextValue: "repository", Owner: "emetos.gitscm"})
	other := m.Add(SourceSpec{ProviderID: "svn", Label: "Subversion"})
	if s.Key() == other.Key() || s.ProviderID() != "git" || s.Owner() != "emetos.gitscm" || s.ContextValue() != "repository" || s.RootPath() != "/work/My Repo/" {
		t.Errorf("source = %+v", s)
	}
	if s.Name() != "My Repo" || other.Name() != "Subversion" {
		t.Errorf("names = %q, %q", s.Name(), other.Name())
	}
	s.SetStatusBarCommands([]StatusCommand{{Text: "$(git-branch) main", Command: "git.checkout"}})
	if got := s.StatusBarCommands(); len(got) != 1 || got[0].Command != "git.checkout" {
		t.Errorf("status = %v", got)
	}
	s.SetAcceptInputCommand(Command{ID: "git.commit", Title: "Commit"})
	if c, ok := s.AcceptInputCommand(); !ok || c.ID != "git.commit" {
		t.Errorf("accept = %v %v", c, ok)
	}
	s.SetAcceptInputCommand(Command{})
	if _, ok := s.AcceptInputCommand(); ok {
		t.Error("an empty command is a command")
	}
	b := &ActionButton{Command: Command{ID: "git.commit", Title: "Commit"}, Secondary: [][]Command{{{ID: "git.commitAll"}}}, Enabled: true, Description: "Commit"}
	s.SetActionButton(b)
	b.Secondary[0][0].ID = "changed" // the model has its own copy
	got := s.ActionButton()
	if got == nil || got.Secondary[0][0].ID != "git.commitAll" || !got.Enabled {
		t.Errorf("button = %+v", got)
	}
	got.Secondary[0][0].ID = "changed too"
	if s.ActionButton().Secondary[0][0].ID != "git.commitAll" {
		t.Error("the button handed out shares its slices")
	}
	s.SetActionButton(nil)
	if s.ActionButton() != nil {
		t.Error("button not removed")
	}
	s.SetBusy(true)
	if !s.Busy() {
		t.Error("not busy")
	}
	s.SetCount(-5)
	if s.Count() != -1 {
		t.Errorf("a negative count = %d", s.Count())
	}
}

func TestInput(t *testing.T) {
	m := NewModel(DefaultConfig())
	s := m.Add(SourceSpec{ProviderID: "git", Label: "Git"})
	in := s.Input()
	if !in.Enabled() || !in.Visible() || in.Value() != "" || in.Placeholder() != "" || in.HasValidation() {
		t.Fatalf("initial input = %+v", in)
	}
	var seen []string
	cancel := in.OnChange(func(v string) { seen = append(seen, v) })
	in.SetValue("a")
	in.SetValue("a")
	in.SetValue("ab")
	cancel()
	in.SetValue("abc")
	if !slices.Equal(seen, []string{"a", "ab"}) || in.Value() != "abc" {
		t.Errorf("seen %v value %q", seen, in.Value())
	}
	in.SetPlaceholder("Message")
	in.SetEnabled(false)
	in.SetVisible(false)
	if in.Placeholder() != "Message" || in.Enabled() || in.Visible() {
		t.Errorf("input = %v %v %v", in.Placeholder(), in.Enabled(), in.Visible())
	}
	if in.Validate("x") != nil {
		t.Error("a message without a check")
	}
	in.SetValidation(func(v string) *Validation {
		if len(v) > 3 {
			return &Validation{Message: "too long", Level: ValidationWarning}
		}
		return nil
	})
	if v := in.Validate("abcd"); v == nil || v.Level != ValidationWarning || in.Validate("ab") != nil || !in.HasValidation() {
		t.Errorf("validation = %v", v)
	}
	in.SetValidation(nil)
	if in.HasValidation() {
		t.Error("validation not removed")
	}
	n := in.FocusRequests()
	in.Focus()
	if in.FocusRequests() != n+1 {
		t.Error("Focus not counted")
	}
}

func TestConfigFromSettings(t *testing.T) {
	tests := []struct {
		name   string
		values map[string]any
		want   Config
	}{
		{"nothing: the defaults", nil, DefaultConfig()},
		{"every setting", map[string]any{
			"scm.countBadge": "focused", "scm.providerCountBadge": "auto", "scm.defaultViewMode": "tree", "scm.defaultViewSortKey": "status",
			"scm.compactFolders": false, "scm.repositories.selectionMode": "single", "scm.repositories.sortOrder": "path",
		}, Config{CountBadge: CountFocused, ProviderCountBadge: ProviderCountAuto, DefaultViewMode: ViewTree, DefaultViewSortKey: SortByStatus,
			CompactFolders: false, SelectionMode: SelectSingle, RepositoriesSortOrder: "path"}},
		{"values of the wrong type or unknown", map[string]any{
			"scm.countBadge": 3, "scm.providerCountBadge": "sometimes", "scm.defaultViewMode": true, "scm.compactFolders": "no", "scm.defaultViewSortKey": "size",
		}, DefaultConfig()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ConfigFromSettings(func(key string) any { return tt.values[key] })
			if got != tt.want {
				t.Errorf("config = %+v\nwant %+v", got, tt.want)
			}
		})
	}
	for _, in := range []struct {
		s    string
		want RepositorySortKey
	}{{"name", SortRepositoryName}, {"path", SortRepositoryPath}, {"discovery time", SortDiscoveryTime}, {"", SortDiscoveryTime}} {
		if got := ParseRepositorySortOrder(in.s); got != in.want {
			t.Errorf("ParseRepositorySortOrder(%q) = %q", in.s, got)
		}
	}
	// the sort order is read when the model is made and not changed by a later configuration
	cfg := DefaultConfig()
	cfg.RepositoriesSortOrder = "name"
	m := NewModel(cfg)
	cfg.RepositoriesSortOrder = "path"
	m.SetConfig(cfg)
	if m.RepositorySortKey() != SortRepositoryName {
		t.Errorf("the sort key = %q", m.RepositorySortKey())
	}
}

// TestConcurrentUse changes and reads a model from several goroutines at once, as the extensions and the UI thread do; it is
// meant for the race detector.
func TestConcurrentUse(t *testing.T) {
	m := NewModel(DefaultConfig())
	var events atomicInt
	m.Subscribe(func(Event) { events.add(1) })
	done := make(chan struct{})
	const workers = 4
	for w := 0; w < workers; w++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for i := 0; i < 50; i++ {
				s := m.Add(SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/r" + string(rune('a'+w))})
				g := s.AddGroup("g", "G", false, "")
				g.SetResources(resources("/work/x/b.go", "/work/x/a.go", "/work/y/c.go"))
				_ = g.Sorted(SortByPath)
				_ = g.Tree().Root().Resources()
				s.SetCount(i)
				s.Input().SetValue("m" + string(rune('a'+i%20)))
				_ = m.ActivityBadge()
				_ = m.Context()
				m.SetActiveResource("file:///work/ra/x.go")
				m.Focus(s)
				m.Remove(s)
			}
		}()
	}
	for w := 0; w < workers; w++ {
		<-done
	}
	if m.Count() != 0 || events.get() == 0 {
		t.Errorf("sources left %d, events %d", m.Count(), events.get())
	}
}

type atomicInt struct {
	mu sync.Mutex
	n  int
}

func (a *atomicInt) add(d int) { a.mu.Lock(); a.n += d; a.mu.Unlock() }
func (a *atomicInt) get() int  { a.mu.Lock(); defer a.mu.Unlock(); return a.n }
