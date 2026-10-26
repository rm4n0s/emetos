package app

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// The commands about workspace tabs (SPEC §10.11, §10.14): New Workspace Tab (workbench.action.newWindow), the ones that
// switch and move tabs (emetos.workspaceTab.next, previous, open1 ... open9, switch, moveLeft, moveRight; Switch Window
// is the picker too) and the dev commands that open a folder in a new tab and activate a tab. They are bound when the
// workspace is made, not when the shell is shown: they use the manager of the tabs and need no widget.

const (
	// SwitchTabPlaceholder is the hint of the picker of the workspace tabs.
	SwitchTabPlaceholder = "Select a workspace tab to switch to"
	// SwitchTabCurrent is what the row of the active tab says before its path.
	SwitchTabCurrent = "current"
	// SwitchTabNoFolder is the path of a tab that has no folder.
	SwitchTabNoFolder = "No folder"
	// switchTabPathWidth is the longest path the picker shows in full: a longer one loses its first folders.
	switchTabPathWidth = 48
)

// registerWindowCommands binds the commands about tabs in the registry of the workspace.
func (w *Workspace) registerWindowCommands() {
	set := func(id string, h command.Handler) {
		if _, err := w.Commands.SetHandler(id, h); err != nil {
			// the commands are registered by the program itself: a missing one is a broken build
			panic(err)
		}
	}
	set("workbench.action.newWindow", func(command.Context, any) error {
		_, err := w.Process.Workspaces.openFrom(WorkspaceSpec{}, w)
		if err != nil {
			w.Notify.Error("Emetos", "Cannot open a new workspace tab: "+describe(err))
		}
		return nil
	})
	m := w.Process.Workspaces
	pick := func(command.Context, any) error {
		m.showSwitcher(w)
		return nil
	}
	set("workbench.action.switchWindow", pick)
	set("emetos.workspaceTab.switch", pick)
	set("emetos.workspaceTab.next", func(command.Context, any) error {
		m.stepTab(1)
		return nil
	})
	set("emetos.workspaceTab.previous", func(command.Context, any) error {
		m.stepTab(-1)
		return nil
	})
	for i := 1; i <= 9; i++ {
		set("emetos.workspaceTab.open"+strconv.Itoa(i), func(command.Context, any) error {
			m.openTabAt(i)
			return nil
		})
	}
	set("emetos.workspaceTab.moveLeft", func(_ command.Context, args any) error {
		m.moveTab(m.tabOfArgs(w, args), -1)
		return nil
	})
	set("emetos.workspaceTab.moveRight", func(_ command.Context, args any) error {
		m.moveTab(m.tabOfArgs(w, args), 1)
		return nil
	})
	w.registerModeCommands(set)
	w.registerExtensionTabCommands(set)
	if DevCommandsEnabled() {
		set("dev.openFolderInNewTab", func(_ command.Context, args any) error {
			path, err := parseOpenFolderInNewTab(args, os.Getwd)
			if err != nil {
				return err
			}
			_, err = w.Process.Workspaces.openFrom(WorkspaceSpec{Folder: path}, w)
			return err
		})
		set("dev.activateWindowTab", func(_ command.Context, args any) error {
			index, err := parseActivateWindowTab(args)
			if err != nil {
				return err
			}
			if !w.Process.Workspaces.ActivateIndex(index) {
				return errors.New("DevActivateWindowTabMissing", "no tab is at this place of the strip",
					"index", index, "tabs", w.Process.Workspaces.Count())
			}
			return nil
		})
		set("dev.notify", devNotifyHandler(w.Notify, w.Process.Logger))
		set("dev.switchProfile", func(c command.Context, args any) error {
			name, err := parseSwitchProfile(args)
			if err != nil {
				return err
			}
			ctx := c.Ctx
			if ctx == nil {
				ctx = w.ctx
			}
			return w.SwitchProfile(ctx, name)
		})
	}
}

// parseSwitchProfile reads the arguments of dev.switchProfile: {"name": "..."}, or just the name as a string; the
// default profile is "Default". The tag is DevSwitchProfileInvalid.
func parseSwitchProfile(args any) (string, error) {
	var name string
	switch v := args.(type) {
	case string:
		name = v
	case map[string]any:
		name, _ = v["name"].(string)
	}
	if name == "" {
		return "", errors.New("DevSwitchProfileInvalid", `dev.switchProfile needs a profile name or {"name": ...}`, "arguments", args)
	}
	return name, nil
}

// parseOpenFolderInNewTab reads the arguments of dev.openFolderInNewTab: {"path": "..."}, or just the path
// as a string; a relative path is relative to the working directory of the process (getwd). The tag is
// DevOpenFolderInvalid.
func parseOpenFolderInNewTab(args any, getwd func() (string, error)) (string, error) {
	var path string
	switch v := args.(type) {
	case string:
		path = v
	case map[string]any:
		path, _ = v["path"].(string)
	}
	if path == "" {
		return "", errors.New("DevOpenFolderInvalid", `dev.openFolderInNewTab needs a folder or {"path": ...}`, "arguments", args)
	}
	if !filepath.IsAbs(path) {
		cwd, err := getwd()
		if err != nil {
			return "", errors.NewErr("DevOpenFolderInvalid", err).SetMetadata("path", path)
		}
		path = filepath.Join(cwd, path)
	}
	return filepath.Clean(path), nil
}

// parseActivateWindowTab reads the arguments of dev.activateWindowTab: {"index": n}, or just the number; the place in
// the strip is counted from 0. The tag is DevActivateWindowTabInvalid.
func parseActivateWindowTab(args any) (int, error) {
	var raw any
	switch v := args.(type) {
	case float64, int:
		raw = v
	case map[string]any:
		raw = v["index"]
	}
	switch n := raw.(type) {
	case float64:
		if n == float64(int(n)) && n >= 0 {
			return int(n), nil
		}
	case int:
		if n >= 0 {
			return n, nil
		}
	}
	return 0, errors.New("DevActivateWindowTabInvalid", `dev.activateWindowTab needs a place of the strip, counted from 0: {"index": ...}`, "arguments", args)
}

// stepTab makes the tab delta places from the active one the active tab, wrapping around at the ends of the strip
// (Next and Previous Workspace Tab; they go through the strip in its order, not in the order the tabs were used in).
func (m *Workspaces) stepTab(delta int) {
	tabs := m.Tabs()
	at := m.IndexOf(m.ActiveTab())
	if len(tabs) < 2 || at < 0 {
		return
	}
	n := len(tabs)
	m.Activate(tabs[((at+delta)%n+n)%n])
}

// openTabAt makes the tab at a place of the strip, counted from 1, the active tab; 9 is the last tab, whatever the number
// of tabs (VS Code's lastEditorInGroup on Alt+9). A place with no tab does nothing.
func (m *Workspaces) openTabAt(place int) {
	tabs := m.Tabs()
	if place >= 9 {
		place = len(tabs)
	}
	if place >= 1 && place <= len(tabs) {
		m.Activate(tabs[place-1])
	}
}

// moveTab moves a tab delta places along the strip; the ends of the strip stop it (they do not wrap), and the active
// tab stays the active one.
func (m *Workspaces) moveTab(t *WindowTab, delta int) {
	if at := m.IndexOf(t); at >= 0 {
		m.Move(t, at+delta)
	}
}

// syncKeys sets the context keys of the tabs in every workspace: workspaceTabCount, activeWorkspaceTabHasFolder, and
// workspaceTabIsFirst and workspaceTabIsLast (the active tab's place in the strip), which the Workspace Tabs submenu of
// the View menu and the keys use. The keys of a workspace matter while its tab is the active one, and all of them
// describe the active tab, so a tab that is activated has them right at once.
func (m *Workspaces) syncKeys() {
	tabs := m.Tabs()
	at := m.IndexOf(m.ActiveTab())
	hasFolder := at >= 0 && tabs[at].ws != nil && tabs[at].ws.Folder != ""
	for _, t := range tabs {
		w := t.ws
		if w == nil || w.Keys == nil {
			continue
		}
		w.Keys.Set("workspaceTabCount", float64(len(tabs)))
		w.Keys.Set("activeWorkspaceTabHasFolder", hasFolder)
		w.Keys.Set("workspaceTabIsFirst", at <= 0)
		w.Keys.Set("workspaceTabIsLast", at == len(tabs)-1)
	}
}

// showSwitcher shows the picker of the workspace tabs in a workspace (Switch Workspace Tab...). One row for each tab:
// the Codicon of the tab (root-folder, or window without a folder), its name (a dot after it when a document has unsaved
// changes), and in dimmer text "current" for the active tab, its folder (the home folder written "~", a long path
// without its first folders) and "Profile: <name>" when the profile is not the default. The active tab is the first row;
// the others follow in the order of the strip, with the first of them the active row, so that Enter switches to it at
// once. Typing filters the names and the descriptions with the fuzzy scorer of the quick input (§10.6); choosing a row
// makes that tab the active one and shows the window.
//
// Ported from VS Code: src/vs/workbench/electron-sandbox/actions/windowActions.ts (SwitchWindowAction)
func (m *Workspaces) showSwitcher(from *Workspace) {
	tabs := m.Tabs()
	labels := make([]string, len(tabs))
	folders := make([]string, len(tabs))
	for i, t := range tabs {
		if t.ws != nil {
			folders[i] = t.ws.Folder
		}
	}
	for i, l := range workbench.LabelWorkspaceTabs(folders) {
		labels[i] = l.Name
		if w := tabs[i].ws; w != nil && w.ext != nil {
			labels[i] = w.ext.displayTitle()
		}
	}
	home, _ := os.UserHomeDir()
	active := m.ActiveTab()
	row := func(i int) quickinput.Item {
		t := tabs[i]
		w := t.ws
		item := quickinput.Item{ID: t.id, Label: labels[i], Icon: fonts.CodiconWindow, Accept: func(quickinput.KeyMods) { m.raiseTab(t) }}
		if w == nil {
			return item
		}
		var parts []string
		if t == active {
			parts = append(parts, SwitchTabCurrent)
		}
		if w.ext != nil {
			// the tab of an extension: its icon, and the name of the extension in place of a folder
			if code := w.ext.iconCode(); code != 0 {
				item.Icon = code
			}
			item.Description = strings.Join(append(parts, w.ext.kind.ExtensionName), " · ")
			return item
		}
		if w.Folder != "" {
			item.Icon = fonts.CodiconRootFolder
			parts = append(parts, shortenPath(w.Folder, home, switchTabPathWidth))
		} else {
			parts = append(parts, SwitchTabNoFolder)
		}
		if w.Profile() != storage.DefaultProfile {
			parts = append(parts, "Profile: "+w.ProfileName())
		}
		item.Description = strings.Join(parts, " · ")
		if w.hasUnsavedChanges() {
			item.Label += " ●"
		}
		return item
	}
	var items []quickinput.Item
	for i, t := range tabs {
		if t == active {
			items = append(items, row(i))
		}
	}
	for i, t := range tabs {
		if t != active {
			items = append(items, row(i))
		}
	}
	activeID := ""
	if len(items) > 1 {
		activeID = items[1].ID
	}
	from.QuickInput.Pick(quickinput.PickOptions{
		Placeholder: SwitchTabPlaceholder, Items: items, MatchOnDescription: true, ActiveID: activeID,
	})
}

// raiseTab makes a tab the active one and shows the window.
func (m *Workspaces) raiseTab(t *WindowTab) {
	if t.ws != nil {
		t.ws.Raise()
		return
	}
	m.Activate(t)
}

// shortenPath writes a folder for a row of a picker: the home folder as "~", and a path longer than max characters
// without its first folders ("…/work/api"), at least the last folder staying.
func shortenPath(path, home string, limit int) string {
	if home != "" {
		if path == home {
			return "~"
		}
		if rest, ok := strings.CutPrefix(path, strings.TrimSuffix(home, "/")+"/"); ok {
			path = "~/" + rest
		}
	}
	if utf8.RuneCountInString(path) <= limit {
		return path
	}
	parts := strings.Split(path, "/")
	short := parts[len(parts)-1]
	for i := len(parts) - 2; i >= 0 && parts[i] != ""; i-- {
		next := parts[i] + "/" + short
		if utf8.RuneCountInString("…/"+next) > limit {
			break
		}
		short = next
	}
	return "…/" + short
}
