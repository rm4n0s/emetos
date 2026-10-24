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

// The commands about windows (SPEC §10.11): New Window, Switch Window and the dev command that opens a folder in a
// new window. They are bound when the window is made, not when the shell is shown: they use the manager of the
// windows and need no widget.

// SwitchWindowPlaceholder is the hint of the Switch Window picker.
const SwitchWindowPlaceholder = "Select a window to switch to"

// SwitchWindowCurrent is the description of the row of the window the picker was opened in.
const SwitchWindowCurrent = "Current Window"

// registerWindowCommands binds the commands about windows in the registry of the window.
func (w *Window) registerWindowCommands() {
	set := func(id string, h command.Handler) {
		if _, err := w.Commands.SetHandler(id, h); err != nil {
			// the commands are registered by the program itself: a missing one is a broken build
			panic(err)
		}
	}
	set("workbench.action.newWindow", func(command.Context, any) error {
		_, err := w.Process.Windows.openFrom(WindowSpec{}, w)
		if err != nil {
			w.Notify.Error("Emetos", "Cannot open a new window: "+describe(err))
		}
		return nil
	})
	set("workbench.action.switchWindow", func(command.Context, any) error {
		w.Process.Windows.showSwitcher(w)
		return nil
	})
	w.registerModeCommands(set)
	if DevCommandsEnabled() {
		set("dev.openFolderInNewWindow", func(_ command.Context, args any) error {
			path, err := parseOpenFolderInNewWindow(args, os.Getwd)
			if err != nil {
				return err
			}
			_, err = w.Process.Windows.openFrom(WindowSpec{Folder: path}, w)
			return err
		})
		set("dev.notify", devNotifyHandler(w.Notify, w.Process.Logger))
	}
}

// parseOpenFolderInNewWindow reads the arguments of dev.openFolderInNewWindow: {"path": "..."}, or just the path
// as a string; a relative path is relative to the working directory of the process (getwd). The tag is
// DevOpenFolderInvalid.
func parseOpenFolderInNewWindow(args any, getwd func() (string, error)) (string, error) {
	var path string
	switch v := args.(type) {
	case string:
		path = v
	case map[string]any:
		path, _ = v["path"].(string)
	}
	if path == "" {
		return "", errors.New("DevOpenFolderInvalid", `dev.openFolderInNewWindow needs a folder or {"path": ...}`, "arguments", args)
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

// showSwitcher shows the Switch Window picker in a window: one row for each window, named by its title, in the
// order the windows were made, with "Current Window" on the row of the window the picker is in; choosing a row
// brings that window to the front.
//
// Ported from VS Code: src/vs/workbench/electron-sandbox/actions/windowActions.ts (SwitchWindowAction)
func (m *Windows) showSwitcher(from *Window) {
	var items []quickinput.Item
	current := -1
	for i, e := range m.entries() {
		w := e.win
		id := strconv.Itoa(e.seq)
		label := w.Title()
		if label == "" {
			label = windowLabel(w)
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

// windowLabel names a window that has no title yet: its folder, else "Untitled (Workspace)".
func windowLabel(w *Window) string {
	if w.Folder != "" {
		return filepath.Base(w.Folder)
	}
	return Title
}
