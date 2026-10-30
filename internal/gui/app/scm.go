package app

import (
	"context"
	"sync/atomic"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/scm"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
)

// The Source Control model of a workspace (SPEC §10.30, step 24.2): the model that the extensions' source controls are added to
// (exthost's SourceControl()), the configuration it reads from the scm.* settings, the context keys the menus and the
// when-clauses read, and the commands of the model that need no widget. The view that draws it is step 24.3's.

// scmConfig reads the configuration of the model from the settings.
func scmConfig(store *settings.Store) scm.Config {
	return scm.ConfigFromSettings(func(key string) any {
		switch key {
		case "scm.compactFolders":
			return settings.Get[bool](store, key, "")
		}
		return settings.Get[string](store, key, "")
	})
}

// startSCM follows the settings and the model: a change of a scm.* setting is given to the model, and each change of the model
// sets the context keys again (once for a burst of changes).
func (w *Workspace) startSCM() {
	m := w.SCM
	if m == nil {
		return
	}
	w.stops = append(w.stops, w.Settings.OnChange(func(e settings.ChangeEvent) {
		for _, key := range scm.ConfigKeys {
			if e.Affects(key, "") {
				w.onUI(func() { m.SetConfig(scmConfig(w.Settings)) })
				return
			}
		}
	}))
	var pending atomic.Bool
	w.stops = append(w.stops, m.Subscribe(func(scm.Event) {
		if pending.Swap(true) {
			return
		}
		w.onUI(func() {
			pending.Store(false)
			w.syncSCMKeys()
		})
	}))
	w.syncSCMKeys()
}

// syncSCMKeys sets the context keys of the model in the scope of the window.
func (w *Workspace) syncSCMKeys() {
	if w.SCM == nil {
		return
	}
	for key, value := range w.SCM.Context() {
		w.Keys.Set(key, value)
	}
}

// registerSCMCommands binds the handlers of the commands of the model (command.registerSourceControlCommands): the view mode and
// the sort key of the Changes view, the order and the selection mode of the repositories, and the choice of the active
// repository. VS Code's selection mode commands write the setting scm.repositories.selectionMode, and so do these; the rest
// is the state of the model.
func (w *Workspace) registerSCMCommands(set func(string, command.Handler)) {
	m := w.SCM
	if m == nil {
		return
	}
	handler := func(fn func()) command.Handler {
		return func(command.Context, any) error {
			fn()
			return nil
		}
	}
	set("workbench.scm.action.setListViewMode", handler(func() { m.SetViewMode(scm.ViewList) }))
	set("workbench.scm.action.setTreeViewMode", handler(func() { m.SetViewMode(scm.ViewTree) }))
	for _, key := range []scm.SortKey{scm.SortByName, scm.SortByPath, scm.SortByStatus} {
		set("workbench.scm.action.setSortKey."+string(key), handler(func() { m.SetSortKey(key) }))
	}
	for _, key := range []scm.RepositorySortKey{scm.SortDiscoveryTime, scm.SortRepositoryName, scm.SortRepositoryPath} {
		set("workbench.scm.action.repositories.setSortKey."+string(key), handler(func() { m.SetRepositorySortKey(key) }))
	}
	for _, mode := range []scm.SelectionMode{scm.SelectSingle, scm.SelectMultiple} {
		set("workbench.scm.action.repositories.setSelectionMode."+string(mode), func(command.Context, any) error {
			return w.Settings.Update(context.Background(), "scm.repositories.selectionMode", string(mode), settings.TargetUser)
		})
	}
	set("scm.setActiveProvider", func(command.Context, any) error {
		w.pickActiveRepository()
		return nil
	})
}

// pickActiveRepository shows the list of the repositories and pins the one that the user chooses as the active one; Auto goes
// back to the repository of the active editor (VS Code's RepositoryPicker).
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmViewService.ts (RepositoryPicker) and
// src/vs/workbench/contrib/scm/browser/scm.contribution.ts (scm.setActiveProvider)
func (w *Workspace) pickActiveRepository() {
	m := w.SCM
	active, pinned := m.Active()
	items := []quickinput.Item{
		{ID: "auto", Label: "Auto", Description: "The active repository is updated based on active editor", Accept: func(quickinput.KeyMods) { m.Pin(nil) }},
		{Separator: true},
	}
	activeID := "auto"
	for _, s := range m.Sources() {
		s := s
		items = append(items, quickinput.Item{
			ID: s.Key(), Label: s.Name(), Description: s.RootPath(), Accept: func(quickinput.KeyMods) { m.Pin(s) },
		})
		if pinned && s == active {
			activeID = s.Key()
		}
	}
	w.QuickInput.Pick(quickinput.PickOptions{
		Placeholder: "Select the active repository, type to filter all repositories",
		Items:       items, ActiveID: activeID,
	})
}
