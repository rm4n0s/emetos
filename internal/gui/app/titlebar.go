package app

import (
	"context"
	"net/url"
	"slices"

	"github.com/rm4n0s/glutter/desktop"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The workspace tabs of the title bar row (SPEC §10.14): what the shell gives the workbench to draw (the tabs, their
// labels, which has unsaved changes, the menus of a tab and of the "+" button) and the commands of those menus. The tabs
// are the ones of the process, so every workspace shows the same strip; the shell of the active one draws it.

// hasUnsavedChanges tells whether a document of the workspace has unsaved changes (the dot of its tab).
func (w *Workspace) hasUnsavedChanges() bool {
	for _, d := range w.Documents.All() {
		if d != nil && d.Dirty() {
			return true
		}
	}
	return false
}

// tabTooltip is the text of the hover of a workspace tab: the path of the folder, and the profile on a second line when
// it is not the default one.
func tabTooltip(w *Workspace) string {
	text := "No folder is open"
	if w.Folder != "" {
		text = w.Folder
	}
	if w.Profile() != storage.DefaultProfile {
		text += "\nProfile: " + w.ProfileName()
	}
	return text
}

// workspaceTabs are the tabs of the strip as the workbench shows them, with their labels told apart.
func (m *Workspaces) workspaceTabs() []workbench.WorkspaceTab {
	tabs := m.Tabs()
	folders := make([]string, len(tabs))
	for i, t := range tabs {
		if t.ws != nil {
			folders[i] = t.ws.Folder
		}
	}
	labels := workbench.LabelWorkspaceTabs(folders)
	out := make([]workbench.WorkspaceTab, len(tabs))
	for i, t := range tabs {
		out[i] = workbench.WorkspaceTab{ID: t.id, Name: labels[i].Name, Description: labels[i].Description}
		if t.ws != nil {
			out[i].Tooltip, out[i].Dirty, out[i].Folder = tabTooltip(t.ws), t.ws.hasUnsavedChanges(), t.ws.Folder != ""
		}
	}
	return out
}

// tabContextKeys are the context keys the menu of the tab at an index of the strip is evaluated in.
func (m *Workspaces) tabContextKeys(index int) map[string]any {
	tabs := m.Tabs()
	extra := map[string]any{"workspaceTabCount": float64(len(tabs)), "workspaceTabIsLast": index == len(tabs)-1}
	if index >= 0 && index < len(tabs) {
		extra["activeWorkspaceTabHasFolder"] = tabs[index].ws != nil && tabs[index].ws.Folder != ""
	}
	return extra
}

// tabArgs is the argument that makes a command of the menu of a tab act on that tab.
func tabArgs(t *WindowTab) map[string]any { return map[string]any{"tabId": t.id} }

// titleBarOptions are the workspace tabs of the title bar for the workbench of this shell.
func (s *shellState) titleBarOptions(win *Workspace) *workbench.TitleBarOptions {
	m := win.Process.Workspaces
	opts := &workbench.TitleBarOptions{Tabs: m.workspaceTabs(), Active: m.IndexOf(m.ActiveTab())}
	opts.OnActivate = func(i int) { m.ActivateIndex(i) }
	opts.OnClose = func(i int) {
		if tabs := m.Tabs(); i >= 0 && i < len(tabs) {
			m.Close(tabs[i], nil)
		}
	}
	opts.OnMove = func(i, to int) {
		if tabs := m.Tabs(); i >= 0 && i < len(tabs) {
			m.Move(tabs[i], to)
		}
	}
	opts.ContextItems = func(i int) []kit.MenuItem {
		tabs := m.Tabs()
		if i < 0 || i >= len(tabs) {
			return nil
		}
		ctx := menuContext{base: win.Keys, settings: win.Settings, extra: m.tabContextKeys(i)}
		items := s.menus.Items(command.WorkspaceTabContext, ctx)
		withArgs(items, tabArgs(tabs[i]))
		return items
	}
	opts.NewItems = func() []kit.MenuItem {
		return s.menus.Items(command.WorkspaceTabNew, menuContext{base: win.Keys, settings: win.Settings})
	}
	opts.OnRunItem = s.menus.Run
	return opts
}

// tabOfArgs is the tab a command of the menu of a tab acts on: the one the argument {"tabId"} names, else the tab of
// the workspace that runs the command (the active one).
func (m *Workspaces) tabOfArgs(w *Workspace, args any) *WindowTab {
	if v, ok := args.(map[string]any); ok {
		if id, ok := v["tabId"].(string); ok {
			for _, t := range m.Tabs() {
				if t.id == id {
					return t
				}
			}
			return nil
		}
	}
	return m.tabOf(w)
}

// closeTabs closes tabs one after the other, each after the questions about its unsaved changes; the first one the user
// cancels stops the rest.
func (m *Workspaces) closeTabs(tabs []*WindowTab) {
	if len(tabs) == 0 {
		return
	}
	m.Close(tabs[0], func(closed bool) {
		if closed {
			m.closeTabs(tabs[1:])
		}
	})
}

// bindTitleBar binds the commands of the workspace tabs (SPEC §10.14, §11.2) and follows the tabs, so that the strip of
// this shell shows what changed while this workspace is the active one. It returns what Dispose calls.
func (s *shellState) bindTitleBar(binding *widgets.WidgetsBinding) func() {
	win := PeekWorkspace(s.Context())
	m := win.Process.Workspaces
	handlers := map[string]command.Handler{
		"emetos.workspaceTab.close": func(_ command.Context, args any) error {
			if t := m.tabOfArgs(win, args); t != nil {
				m.Close(t, nil)
			}
			return nil
		},
		"emetos.workspaceTab.closeOthers": func(_ command.Context, args any) error {
			keep := m.tabOfArgs(win, args)
			var others []*WindowTab
			for _, t := range m.Tabs() {
				if t != keep {
					others = append(others, t)
				}
			}
			m.closeTabs(others)
			return nil
		},
		"emetos.workspaceTab.closeToTheRight": func(_ command.Context, args any) error {
			from := m.tabOfArgs(win, args)
			tabs := m.Tabs()
			if i := slices.Index(tabs, from); i >= 0 {
				m.closeTabs(tabs[i+1:])
			}
			return nil
		},
		"emetos.workspaceTab.copyPath": func(_ command.Context, args any) error {
			if t := m.tabOfArgs(win, args); t != nil && t.ws != nil && t.ws.Folder != "" {
				s.copyText(t.ws.Folder)
			}
			return nil
		},
		"emetos.workspaceTab.revealInFileManager": func(_ command.Context, args any) error {
			if t := m.tabOfArgs(win, args); t != nil && t.ws != nil && t.ws.Folder != "" {
				s.revealFolder(t.ws.Folder)
			}
			return nil
		},
		"emetos.workspaceTab.openFolder": func(command.Context, any) error {
			s.pickFolderInNewTab()
			return nil
		},
	}
	var removers []func()
	for id, h := range handlers {
		remove, err := win.Commands.SetHandler(id, h)
		if err != nil {
			// the commands are registered by the program itself: a missing one is a broken build
			panic(err)
		}
		removers = append(removers, remove)
	}
	// the strip is the one of the process: a change of the tabs (opened, closed, moved, activated, another folder or
	// profile, unsaved changes) shows in the shell of the tab that is the active one
	stop := m.OnChange(func() {
		if s.Mounted() && win.activeTab() {
			s.SetState(func() {})
		}
	})
	return func() {
		stop()
		for _, remove := range removers {
			remove()
		}
	}
}

// revealFolder shows a folder in the file manager of the desktop (an OpenURI portal, on a goroutine).
func (s *shellState) revealFolder(dir string) {
	win := PeekWorkspace(s.Context())
	binding := widgets.WidgetsBindingOf(s.Context())
	bc := s.Context()
	open := win.OpenURI
	if open == nil {
		open = desktop.OpenURI
	}
	s.Go(func(life context.Context) {
		if err := open(life, bc, (&url.URL{Scheme: "file", Path: dir}).String()); err != nil {
			binding.Post(func() {
				win.Process.Logger.Warn("cannot show the folder in the file manager", "path", dir, "err", err)
				win.Notify.Error("Emetos", "Cannot show the folder in the file manager: "+describe(err))
			})
		}
	})
}

// pickFolderInNewTab is the "Workspace..." entry of the "+" menu: the folder dialog of the desktop, and the folder chosen
// opens as a new workspace tab, or its tab becomes the active one when it is open already.
func (s *shellState) pickFolderInNewTab() {
	win := PeekWorkspace(s.Context())
	binding := widgets.WidgetsBindingOf(s.Context())
	bc := s.Context()
	s.Go(func(life context.Context) {
		path, err := chooseFolder(life, bc, win.Folder)
		binding.Post(func() {
			if !s.Mounted() {
				return
			}
			if err != nil {
				win.Process.Logger.Warn("the folder dialog failed", "err", err)
				win.Notify.Error("Emetos", "Cannot show the dialog to open a folder: "+describe(err))
				return
			}
			if path != "" {
				win.Process.Workspaces.OpenFolder(win, path, true, false, nil)
			}
		})
	})
}
