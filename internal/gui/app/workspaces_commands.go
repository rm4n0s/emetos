package app

import (
	"os"
	"path/filepath"
	"strconv"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
)

// The commands about workspace tabs (SPEC §10.11, §10.14): New Workspace Tab (workbench.action.newWindow), Switch
// Window and the dev commands that open a folder in a new tab and activate a tab. They are bound when the workspace is
// made, not when the shell is shown: they use the manager of the tabs and need no widget.

// SwitchWindowPlaceholder is the hint of the Switch Window picker.
//
// TODO(step 9b.5): the picker of the workspace tabs (emetos.workspaceTab.switch) takes the place of this one.
const SwitchWindowPlaceholder = "Select a window to switch to"

// SwitchWindowCurrent is the description of the row of the tab the picker was opened in.
const SwitchWindowCurrent = "Current Window"

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
	set("workbench.action.switchWindow", func(command.Context, any) error {
		w.Process.Workspaces.showSwitcher(w)
		return nil
	})
	w.registerModeCommands(set)
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

// showSwitcher shows the Switch Window picker in a workspace: one row for each tab, named by its title, in the
// order of the strip, with "Current Window" on the row of the tab the picker is in; choosing a row makes that tab the
// active one.
//
// Ported from VS Code: src/vs/workbench/electron-sandbox/actions/windowActions.ts (SwitchWindowAction)
func (m *Workspaces) showSwitcher(from *Workspace) {
	var items []quickinput.Item
	current := -1
	for i, t := range m.Tabs() {
		w := t.ws
		if w == nil {
			continue
		}
		id := strconv.Itoa(t.seq)
		label := w.Title()
		if label == "" {
			label = workspaceLabel(w)
		}
		item := quickinput.Item{
			ID: id, Label: label, Description: w.Folder,
			Icon:   fonts.CodiconWindow,
			Accept: func(quickinput.KeyMods) { w.Raise() },
		}
		if w == from {
			item.Description = SwitchWindowCurrent
			current = i
		}
		items = append(items, item)
	}
	// the window after the current one is the one to switch to
	active := ""
	if len(items) > 1 && current >= 0 {
		active = items[(current+1)%len(items)].ID
	}
	from.QuickInput.Pick(quickinput.PickOptions{
		Placeholder: SwitchWindowPlaceholder, Items: items, MatchOnDescription: true, ActiveID: active,
	})
}

// workspaceLabel names a workspace that has no title yet: its folder, else the name of the application.
func workspaceLabel(w *Workspace) string {
	if w.Folder != "" {
		return filepath.Base(w.Folder)
	}
	return Title
}
