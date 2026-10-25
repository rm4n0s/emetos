package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
)

// The recently opened folders and files (SPEC §10.10): the rows of File > Open Recent, the Open Recent picker
// (Ctrl+R) and Clear Recently Opened.
//
// Ported from VS Code: src/vs/workbench/browser/actions/windowActions.ts (BaseOpenRecentAction, OpenRecentAction),
// src/vs/workbench/browser/parts/titlebar/menubarControl.ts (getOpenRecentActions) and
// src/vs/workbench/browser/parts/editor/editorActions.ts (ClearRecentFilesAction)

const (
	// maxMenuRecent is how many folders, and how many files, File > Open Recent lists.
	maxMenuRecent = 10
	// maxMenuRecentLabel is the longest label of a row of the menu; a longer one is cut in the middle.
	maxMenuRecentLabel = 120
)

// Texts of the recently opened list.
const (
	// RecentClearMessage and RecentClearDetail are the question of Clear Recently Opened.
	RecentClearMessage = "Do you want to clear all recently opened files and workspaces?"
	RecentClearDetail  = "This action is irreversible!"
	// RecentPlaceholder is the hint of the Open Recent picker.
	RecentPlaceholder = "Select to open"
	// RecentFoldersLabel and RecentFilesLabel are the headings of the picker.
	RecentFoldersLabel = "folders"
	RecentFilesLabel   = "files"
)

// recentLabel is the label of a recent folder or file: its path, with the home folder written "~".
func recentLabel(path string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if path == home {
			return "~"
		}
		if rest, ok := strings.CutPrefix(path, strings.TrimSuffix(home, "/")+"/"); ok {
			return "~/" + rest
		}
	}
	return path
}

// truncateMiddle cuts a text longer than max characters in the middle, putting "…" in the place of the cut.
//
// Ported from VS Code: src/vs/base/common/strings.ts (truncateMiddle)
func truncateMiddle(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	keep := max - 1
	head := (keep + 1) / 2
	tail := keep - head
	return string(runes[:head]) + "…" + string(runes[len(runes)-tail:])
}

// recentMenuItems are the rows File > Open Recent has for the recently opened folders and files: the first
// maxMenuRecent folders and files that still exist, the most recent first, each running openRecentFolder or
// openRecentFile with its path. The registry asks again each time the menu opens.
func (p *Process) recentMenuItems(contextkey.Context) []command.MenuItem {
	g, err := p.State.LoadGlobal(context.Background())
	if err != nil {
		p.Logger.Debug("the recently opened list cannot be used", "err", err)
	}
	var items []command.MenuItem
	add := func(list []string, id, group string) {
		n := 0
		for _, path := range list {
			if n == maxMenuRecent {
				break
			}
			if !storage.DiskExists(path) {
				continue
			}
			items = append(items, command.MenuItem{
				Menu: command.MenubarRecentMenu, Command: id, Group: group, Order: float64(n), Args: path,
				Title: truncateMiddle(recentLabel(path), maxMenuRecentLabel),
			})
			n++
		}
	}
	add(g.Recent.Folders, "openRecentFolder", "2_folders")
	add(g.Recent.Files, "openRecentFile", "3_files")
	return items
}

// bindRecentCommands binds the commands of the recently opened list while the shell is on screen.
func (s *shellState) bindRecentCommands(binding *widgets.WidgetsBinding) {
	win := PeekWorkspace(s.Context())
	pathOf := func(id string, args any) (string, error) {
		path, _ := args.(string)
		if !filepath.IsAbs(path) {
			return "", errors.New("OpenRecentPathInvalid", "the argument is the absolute path of a recently opened folder or file", "command", id, "argument", args)
		}
		return path, nil
	}
	handlers := map[string]command.Handler{
		"workbench.action.openRecent":       func(command.Context, any) error { s.showRecent(binding); return nil },
		"workbench.action.clearRecentFiles": func(command.Context, any) error { s.clearRecent(); return nil },
		"openRecentFolder": func(_ command.Context, args any) error {
			path, err := pathOf("openRecentFolder", args)
			if err != nil {
				return err
			}
			s.openFolder(path)
			return nil
		},
		"openRecentFile": func(_ command.Context, args any) error {
			path, err := pathOf("openRecentFile", args)
			if err != nil {
				return err
			}
			s.openPathAt(binding, path, 0, 0, openOptions{})
			return nil
		},
	}
	for id, h := range handlers {
		remove, err := win.Commands.SetHandler(id, h)
		if err != nil {
			// the commands are registered by the program itself: a missing one is a broken build
			panic(err)
		}
		s.explorer.removeCommands = append(s.explorer.removeCommands, remove)
	}
}

// clearRecent asks whether to clear the recently opened folders and files, and clears them, with the files the
// quick open lists first and the history of editors.
func (s *shellState) clearRecent() {
	s.confirm(confirmSpec{Type: kit.DialogWarning, Message: RecentClearMessage, Detail: RecentClearDetail, Primary: "Clear"},
		func(confirmed, _ bool) {
			if !confirmed || !s.Mounted() {
				return
			}
			PeekWorkspace(s.Context()).ClearRecent()
			if s.nav.history != nil {
				s.nav.history.Clear()
			}
		})
}

// showRecent shows the Open Recent picker: the recent folders under "folders" and the recent files under
// "files", with the folder of each as its description. The folders and files that are gone are checked for in the
// background and left out.
func (s *shellState) showRecent(binding *widgets.WidgetsBinding) {
	win := PeekWorkspace(s.Context())
	s.Go(func(life context.Context) {
		g, err := win.Process.State.LoadGlobal(life)
		if err != nil {
			win.Process.Logger.Debug("the recently opened list cannot be used", "err", err)
		}
		recent := g.Recent.Existing(storage.DiskExists)
		binding.Post(func() {
			if s.Mounted() {
				s.pickRecent(binding, recent)
			}
		})
	})
}

// pickRecent shows the picker of the recently opened folders and files.
func (s *shellState) pickRecent(binding *widgets.WidgetsBinding, recent storage.RecentlyOpened) {
	win := PeekWorkspace(s.Context())
	remove := quickinput.Button{Icon: fonts.CodiconClose, Tooltip: "Remove from Recently Opened"}
	trigger := func(path string) func(int, quickinput.KeyMods) quickinput.TriggerAction {
		return func(int, quickinput.KeyMods) quickinput.TriggerAction {
			win.RemoveRecent(path)
			return quickinput.TriggerRemove
		}
	}
	split := func(path string) (name, parent string) {
		name = filepath.Base(path)
		if dir := filepath.Dir(path); dir != path {
			parent = recentLabel(dir)
		}
		return name, parent
	}
	var items []quickinput.Item
	active := ""
	items = append(items, quickinput.Item{Separator: true, Label: RecentFoldersLabel})
	for _, path := range recent.Folders {
		name, parent := split(path)
		items = append(items, quickinput.Item{
			ID: path, Label: name, Description: parent, Icon: fonts.CodiconFolder, Buttons: []quickinput.Button{remove},
			// the Ctrl key opens the folder in a new window (VS Code's ctrlCmd)
			Accept:  func(mods quickinput.KeyMods) { s.openFolderIn(path, mods.Ctrl) },
			Trigger: trigger(path),
		})
		// the first row is the folder that is open: the second one is the one to open
		if active == "" && (path != win.Folder || len(recent.Folders) == 1) {
			active = path
		}
	}
	items = append(items, quickinput.Item{Separator: true, Label: RecentFilesLabel})
	for _, path := range recent.Files {
		name, parent := split(path)
		items = append(items, quickinput.Item{
			ID: path, Label: name, Description: parent, Buttons: []quickinput.Button{remove},
			FileIcon: func(typ theme.Type) (theme.FileIcon, bool) {
				th := win.FileIcons()
				if th == nil {
					return theme.FileIcon{}, false
				}
				return th.IconFor(theme.FileIconQuery{Kind: theme.FileIconFile, Name: name, LanguageID: win.languageOf(path), Type: typ})
			},
			Accept:  func(quickinput.KeyMods) { s.openPathAt(binding, path, 0, 0, openOptions{}) },
			Trigger: trigger(path),
		})
		if active == "" {
			active = path
		}
	}
	win.QuickInput.Pick(quickinput.PickOptions{
		Placeholder: RecentPlaceholder, Items: items, MatchOnDescription: true, ActiveID: active,
	})
}
