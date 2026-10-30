package app

import (
	"context"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/scm"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
)

func runCommand(t *testing.T, w *Workspace, id string) {
	t.Helper()
	if err := w.Commands.Run(command.Context{Ctx: context.Background(), Source: command.SourcePalette}, id, nil); err != nil {
		t.Fatalf("%s: %v", id, err)
	}
}

func TestTheCommandsOfTheSourceControlModel(t *testing.T) {
	tests := []struct {
		name  string
		setup func(w *Workspace)
		id    string
		check func(t *testing.T, w *Workspace)
	}{
		{"view as tree", nil, "workbench.scm.action.setTreeViewMode", func(t *testing.T, w *Workspace) {
			if w.SCM.ViewMode() != scm.ViewTree || w.Keys.Get("scmViewMode") != "tree" {
				t.Errorf("mode %q key %v", w.SCM.ViewMode(), w.Keys.Get("scmViewMode"))
			}
		}},
		{"view as list", func(w *Workspace) { w.SCM.SetViewMode(scm.ViewTree) }, "workbench.scm.action.setListViewMode", func(t *testing.T, w *Workspace) {
			if w.SCM.ViewMode() != scm.ViewList || w.Keys.Get("scmViewMode") != "list" {
				t.Errorf("mode %q key %v", w.SCM.ViewMode(), w.Keys.Get("scmViewMode"))
			}
		}},
		{"sort changes by name", nil, "workbench.scm.action.setSortKey.name", func(t *testing.T, w *Workspace) {
			if w.SCM.SortKey() != scm.SortByName || w.Keys.Get("scmViewSortKey") != "name" {
				t.Errorf("key %q ctx %v", w.SCM.SortKey(), w.Keys.Get("scmViewSortKey"))
			}
		}},
		{"sort changes by status", nil, "workbench.scm.action.setSortKey.status", func(t *testing.T, w *Workspace) {
			if w.SCM.SortKey() != scm.SortByStatus {
				t.Errorf("key %q", w.SCM.SortKey())
			}
		}},
		{"sort changes by path", func(w *Workspace) { w.SCM.SetSortKey(scm.SortByName) }, "workbench.scm.action.setSortKey.path", func(t *testing.T, w *Workspace) {
			if w.SCM.SortKey() != scm.SortByPath {
				t.Errorf("key %q", w.SCM.SortKey())
			}
		}},
		{"sort the repositories by name", nil, "workbench.scm.action.repositories.setSortKey.name", func(t *testing.T, w *Workspace) {
			if w.SCM.RepositorySortKey() != scm.SortRepositoryName || w.Keys.Get("scmRepositorySortKey") != "name" {
				t.Errorf("key %q ctx %v", w.SCM.RepositorySortKey(), w.Keys.Get("scmRepositorySortKey"))
			}
		}},
		{"sort the repositories by path", nil, "workbench.scm.action.repositories.setSortKey.path", func(t *testing.T, w *Workspace) {
			if w.SCM.RepositorySortKey() != scm.SortRepositoryPath {
				t.Errorf("key %q", w.SCM.RepositorySortKey())
			}
		}},
		{"sort the repositories by discovery time", func(w *Workspace) { w.SCM.SetRepositorySortKey(scm.SortRepositoryName) }, "workbench.scm.action.repositories.setSortKey.discoveryTime", func(t *testing.T, w *Workspace) {
			if w.SCM.RepositorySortKey() != scm.SortDiscoveryTime {
				t.Errorf("key %q", w.SCM.RepositorySortKey())
			}
		}},
		{"select a single repository writes the setting", func(w *Workspace) {
			for _, name := range []string{"a", "b"} {
				w.SCM.Add(scm.SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/" + name})
			}
		}, "workbench.scm.action.repositories.setSelectionMode.single", func(t *testing.T, w *Workspace) {
			if got := settings.Get[string](w.Settings, "scm.repositories.selectionMode", ""); got != "single" {
				t.Errorf("setting = %q", got)
			}
			if w.SCM.SelectionMode() != scm.SelectSingle || len(w.SCM.Visible()) != 1 || w.Keys.Get("scmRepositoryVisibleCount") != 1.0 {
				t.Errorf("mode %q visible %d ctx %v", w.SCM.SelectionMode(), len(w.SCM.Visible()), w.Keys.Get("scmRepositoryVisibleCount"))
			}
		}},
		{"select multiple repositories", func(w *Workspace) {
			for _, name := range []string{"a", "b"} {
				w.SCM.Add(scm.SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/" + name})
			}
			if err := w.Settings.Update(context.Background(), "scm.repositories.selectionMode", "single", settings.TargetUser); err != nil {
				t.Fatal(err)
			}
		}, "workbench.scm.action.repositories.setSelectionMode.multiple", func(t *testing.T, w *Workspace) {
			if w.SCM.SelectionMode() != scm.SelectMultiple || len(w.SCM.Visible()) != 2 {
				t.Errorf("mode %q visible %d", w.SCM.SelectionMode(), len(w.SCM.Visible()))
			}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := settingsWindow(t, nil)
			t.Cleanup(w.release)
			if tt.setup != nil {
				tt.setup(w)
			}
			runCommand(t, w, tt.id)
			tt.check(t, w)
		})
	}
}

func TestTheSourceControlSettingsReachTheModel(t *testing.T) {
	w := settingsWindow(t, map[string]any{"scm.defaultViewMode": "tree", "scm.countBadge": "focused", "scm.compactFolders": false})
	t.Cleanup(w.release)
	c := w.SCM.Config()
	if c.DefaultViewMode != scm.ViewTree || c.CountBadge != scm.CountFocused || c.CompactFolders || w.SCM.ViewMode() != scm.ViewTree {
		t.Fatalf("config at the start = %+v, mode %q", c, w.SCM.ViewMode())
	}
	if err := w.Settings.Update(context.Background(), "scm.providerCountBadge", "visible", settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	if err := w.Settings.Update(context.Background(), "scm.compactFolders", true, settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	if c := w.SCM.Config(); c.ProviderCountBadge != scm.ProviderCountVisible || !c.CompactFolders || c.CountBadge != scm.CountFocused {
		t.Errorf("config after the change = %+v", c)
	}
}

func TestTheContextKeysFollowTheSourceControlModel(t *testing.T) {
	w := settingsWindow(t, nil)
	t.Cleanup(w.release)
	if w.Keys.Get("scm.providerCount") != 0.0 {
		t.Fatalf("scm.providerCount = %v at the start", w.Keys.Get("scm.providerCount"))
	}
	s := w.SCM.Add(scm.SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/repo"})
	if w.Keys.Get("scm.providerCount") != 1.0 || w.Keys.Get("scmActiveRepositoryName") != "repo" {
		t.Errorf("after Add: providerCount %v, active name %v", w.Keys.Get("scm.providerCount"), w.Keys.Get("scmActiveRepositoryName"))
	}
	s.SetActiveRepositoryName("worktree")
	if w.Keys.Get("scmActiveRepositoryName") != "worktree" {
		t.Errorf("active name = %v", w.Keys.Get("scmActiveRepositoryName"))
	}
	w.SCM.Remove(s)
	if w.Keys.Get("scm.providerCount") != 0.0 || w.Keys.Get("scmActiveRepositoryName") != "" {
		t.Errorf("after Remove: providerCount %v, active name %v", w.Keys.Get("scm.providerCount"), w.Keys.Get("scmActiveRepositoryName"))
	}
}

func TestTheActiveRepositoryIsChosenInTheQuickInput(t *testing.T) {
	w := settingsWindow(t, nil)
	t.Cleanup(w.release)
	a := w.SCM.Add(scm.SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/a"})
	b := w.SCM.Add(scm.SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/b"})
	runCommand(t, w, "scm.setActiveProvider")
	if !w.QuickInput.Visible() {
		t.Fatal("the list is not shown")
	}
	var labels []string
	for _, it := range w.QuickInput.Picker().Items() {
		if !it.Separator {
			labels = append(labels, it.Label+"|"+it.Description)
		}
	}
	if want := []string{"Auto|The active repository is updated based on active editor", "a|/work/a", "b|/work/b"}; !equalStrings(labels, want) {
		t.Fatalf("items = %q", labels)
	}
	w.QuickInput.Picker().Items()[len(w.QuickInput.Picker().Items())-1].Accept(quickinput.KeyMods{}) // b
	if got, pinned := w.SCM.Active(); got != b || !pinned {
		t.Errorf("active = %v pinned %v, want b pinned", got, pinned)
	}
	runCommand(t, w, "scm.setActiveProvider")
	for _, it := range w.QuickInput.Picker().Items() {
		if it.ID == "auto" {
			it.Accept(quickinput.KeyMods{})
		}
	}
	if got, pinned := w.SCM.Active(); got != a || pinned {
		t.Errorf("after Auto: active = %v pinned %v, want a unpinned", got, pinned)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
