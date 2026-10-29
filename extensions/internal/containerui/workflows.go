package containerui

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/rm4n0s/emetos/extension"
)

// The workflows around files (step 18.3): Build Image, Compose, Push, Run Interactive, Open Shell and the scaffolding. This file has
// what they share: the folder, the choice of a file and the choice of several names.

// folder is the folder of the workspace, "" when there is none.
func (e *ext) folder() string { return e.api.Scope().Folder }

// needFolder tells the user that a command needs a folder; it returns the folder, and ok false when there is none.
func (e *ext) needFolder(what string) (string, bool) {
	f := e.folder()
	if f == "" {
		e.api.Notifications().Warn("To " + what + " you must first open a folder.")
		return "", false
	}
	return f, true
}

// pickFiles finds the files a command works on and calls fn with them:
//   - the rows of the Explorer the command was run on (the arguments of the Explorer's context menu), those that match;
//   - else the document in front, when it matches (the editor's context menu, or the palette with a Dockerfile open);
//   - else the files of the workspace that match, the only one, or the one the user picks.
//
// what names the files in the messages ("Dockerfile"), match tells whether a path is one, lang is the language id of the files.
func (e *ext) pickFiles(args any, what string, match func(string) bool, lang string, fn func(paths []string)) {
	var paths []string
	for _, uri := range extension.ResourceURIs(args) {
		if p, ok := pathOfURI(uri); ok && match(p) {
			paths = append(paths, p)
		}
	}
	if len(paths) > 0 {
		fn(paths)
		return
	}
	if doc, ok := e.api.Documents().Active(); ok {
		if p, ok := pathOfURI(doc.URI); ok && (doc.LanguageID == lang || match(p)) {
			fn([]string{p})
			return
		}
	}
	folder, ok := e.needFolder("use a " + what)
	if !ok {
		return
	}
	list, err := e.api.Workspace().Files()
	if err != nil {
		e.api.Notifications().Warn("Cannot list the files of the workspace: " + reasonOf(err))
		return
	}
	var rels []string
	for _, f := range list.Files {
		if match(f) {
			rels = append(rels, f)
		}
	}
	slices.Sort(rels)
	switch len(rels) {
	case 0:
		e.api.Notifications().Info("There is no " + what + " in the workspace.")
		return
	case 1:
		fn([]string{filepath.Join(folder, filepath.FromSlash(rels[0]))})
		return
	}
	items := make([]extension.QuickPickItem, len(rels))
	for i, r := range rels {
		items[i] = extension.QuickPickItem{Label: r}
	}
	err = e.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Select a " + what, Items: items}, func(i int) {
		if i >= 0 && i < len(rels) {
			fn([]string{filepath.Join(folder, filepath.FromSlash(rels[i]))})
		}
	})
	if err != nil {
		e.api.Log().Warn("the quick pick cannot be shown", "err", err)
	}
}

// pickSome lets the user choose some of the names (the quick pick chooses one item, so each choice toggles a name and the list is
// shown again until the first item, which says how many are chosen, is picked). preselected are the names chosen at first. done is
// called with the chosen names in the order of all; it is not called when the list is closed with Escape or nothing is chosen.
func (e *ext) pickSome(placeholder string, all, preselected []string, done func(chosen []string)) {
	on := map[string]bool{}
	for _, n := range preselected {
		on[n] = true
	}
	var show func()
	show = func() {
		chosen := func() []string {
			var out []string
			for _, n := range all {
				if on[n] {
					out = append(out, n)
				}
			}
			return out
		}
		n := len(chosen())
		items := []extension.QuickPickItem{{Label: "$(check) Done: " + strconv.Itoa(n) + " of " + strconv.Itoa(len(all)) + " chosen",
			Description: "Pick a name to choose it or to leave it out"}}
		for _, name := range all {
			mark := "$(circle-large-outline) "
			if on[name] {
				mark = "$(pass-filled) "
			}
			items = append(items, extension.QuickPickItem{Label: mark + name})
		}
		err := e.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: placeholder, Items: items}, func(i int) {
			switch {
			case i < 0:
			case i == 0:
				if got := chosen(); len(got) > 0 {
					done(got)
				} else {
					e.api.Notifications().Info("Nothing is chosen.")
				}
			default:
				on[all[i-1]] = !on[all[i-1]]
				show()
			}
		})
		if err != nil {
			e.api.Log().Warn("the quick pick cannot be shown", "err", err)
		}
	}
	show()
}

// readBoolSetting reads a setting of the extension; def when it cannot be read.
func (e *ext) readBoolSetting(key string, def bool) bool {
	v, err := extension.SettingValue[bool](e.api.Settings(), key)
	if err != nil {
		e.api.Log().Warn("a setting cannot be read", "key", key, "err", err)
		return def
	}
	return v
}

// readStringSetting reads a setting of the extension.
func (e *ext) readStringSetting(key string) string {
	v, err := extension.SettingValue[string](e.api.Settings(), key)
	if err != nil {
		e.api.Log().Warn("a setting cannot be read", "key", key, "err", err)
		return ""
	}
	return strings.TrimSpace(v)
}

// SettingComposeBuild, SettingComposeDetached and SettingImageBuildContextPath are the keys of the settings of the workflows
// (Container Tools' containers.composeBuild, containers.composeDetached and containers.imageBuildContextPath, with the prefix of the
// Config in place of containers).
func (c Config) settingComposeBuild() string     { return c.Prefix + ".composeBuild" }
func (c Config) settingComposeDetached() string  { return c.Prefix + ".composeDetached" }
func (c Config) settingBuildContextPath() string { return c.Prefix + ".imageBuildContextPath" }

// BuildFinishedTopic is the topic of the event published when a build ended: {image, ok}.
func (c Config) BuildFinishedTopic() string { return c.ID + ".buildFinished" }

// DockerfileServerPathSetting and ComposeServerPathSetting are the keys of the settings that hold the path of the language servers.
func (c Config) DockerfileServerPathSetting() string {
	return c.Prefix + ".dockerfileLanguageServer.path"
}

// ComposeServerPathSetting is the key of the setting that holds the path of the Compose language server.
func (c Config) ComposeServerPathSetting() string { return c.Prefix + ".composeLanguageServer.path" }
