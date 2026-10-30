package app

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	scmcore "github.com/rm4n0s/emetos/internal/core/scm"
	scmview "github.com/rm4n0s/emetos/internal/gui/views/scm"
)

// The dev command dev.scm.fake (SPEC §15.2, step 24.3): it adds source controls to the Source Control view from a JSON fixture, with
// groups, resources, status bar items and decorations, and the menus a Git-like provider would add. The goldens and the proof of
// the view use it; there is no provider behind it.
//
//	{"sourceControls": [{"id": "git", "label": "Git", "root": ".", "status": [{"text": "$(git-branch) main", "tooltip": "..."}],
//	  "count": 3, "groups": [{"id": "index", "label": "Staged Changes", "resources": [{"path": "src/a.go", "letter": "M",
//	  "tooltip": "Index Modified", "color": "gitDecoration.stageModifiedResourceForeground", "strikeThrough": false,
//	  "faded": false}]}]}], "hover": "src/a.go", "tree": false}

// fakeResource is a resource of a fixture.
type fakeResource struct {
	Path          string `json:"path"`
	Letter        string `json:"letter"`
	Tooltip       string `json:"tooltip"`
	Color         string `json:"color"`
	Icon          string `json:"icon"`
	StrikeThrough bool   `json:"strikeThrough"`
	Faded         bool   `json:"faded"`
}

// fakeGroup is a group of a fixture.
type fakeGroup struct {
	ID            string         `json:"id"`
	Label         string         `json:"label"`
	HideWhenEmpty bool           `json:"hideWhenEmpty"`
	Resources     []fakeResource `json:"resources"`
}

// fakeStatus is a status bar command of a fixture.
type fakeStatus struct {
	Text    string `json:"text"`
	Tooltip string `json:"tooltip"`
	Command string `json:"command"`
}

// fakeSource is a source control of a fixture.
type fakeSource struct {
	ID     string       `json:"id"`
	Label  string       `json:"label"`
	Root   string       `json:"root"`
	Status []fakeStatus `json:"status"`
	Count  *int         `json:"count"`
	Busy   bool         `json:"busy"`
	Groups []fakeGroup  `json:"groups"`
}

// fakeFixture is the fixture of dev.scm.fake.
type fakeFixture struct {
	SourceControls []fakeSource `json:"sourceControls"`
	// Hover is the file whose row is drawn as hovered, so that a proof shows its inline actions.
	Hover string `json:"hover"`
	// Tree shows the changes as a tree.
	Tree bool `json:"tree"`
}

// parseFakeSCM reads the arguments of dev.scm.fake: {"fixture": "path.json"}, or the path as a string. The path is relative to the
// folder Emetos runs in. The tags are DevSCMFakeInvalid, DevSCMFakeReadFailed and DevSCMFakeFixtureInvalid.
func parseFakeSCM(args any) (fakeFixture, error) {
	var path string
	switch v := args.(type) {
	case string:
		path = v
	case map[string]any:
		path, _ = v["fixture"].(string)
	}
	if path == "" {
		return fakeFixture{}, errors.New("DevSCMFakeInvalid", `dev.scm.fake needs {"fixture": "<path>"}`, "arguments", args)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return fakeFixture{}, errors.NewErr("DevSCMFakeReadFailed", err).SetMetadata("path", path)
	}
	var f fakeFixture
	if err := json.Unmarshal(data, &f); err != nil {
		return fakeFixture{}, errors.NewErr("DevSCMFakeFixtureInvalid", err).SetMetadata("path", path)
	}
	return f, nil
}

// registerDevSCM registers dev.scm.fake; the window binds its handler while it is shown.
func registerDevSCM(r *command.Registry) {
	meta := command.Meta{ID: "dev.scm.fake", Description: `Add source controls from a JSON fixture. Arguments: {"fixture": "<path>"}.`}
	err := RegisterDev(r, meta, func(command.Context, any) error {
		return errors.New("DevNoWindow", "no window is shown")
	})
	if err != nil {
		// the id is fixed, new and starts with dev., so a failure is a mistake here
		panic(err)
	}
}

// fakeCommands are the commands of the fake provider: title, id, icon.
var fakeCommands = []struct{ id, title, icon string }{
	{"dev.scm.openFile", "Open File", "go-to-file"},
	{"dev.scm.openChanges", "Open Changes", ""},
	{"dev.scm.stage", "Stage Changes", "add"},
	{"dev.scm.unstage", "Unstage Changes", "remove"},
	{"dev.scm.discard", "Discard Changes", "discard"},
	{"dev.scm.stageAll", "Stage All Changes", "add"},
	{"dev.scm.unstageAll", "Unstage All Changes", "remove"},
	{"dev.scm.discardAll", "Discard All Changes", "discard"},
	{"dev.scm.commit", "Commit", "check"},
	{"dev.scm.refresh", "Refresh", "refresh"},
	{"dev.scm.pull", "Pull", ""},
	{"dev.scm.push", "Push", ""},
	{"dev.scm.checkout", "Checkout to...", ""},
	{"dev.scm.sync", "Sync Changes", ""},
}

// bindDevSCM gives dev.scm.fake its handler.
func (s *shellState) bindDevSCM() {
	if !DevCommandsEnabled() {
		return
	}
	win := s.win
	remove, err := win.Commands.SetHandler("dev.scm.fake", func(_ command.Context, args any) error {
		f, err := parseFakeSCM(args)
		if err != nil {
			return err
		}
		return s.addFakeSCM(f)
	})
	if err != nil {
		win.Process.Logger.Error("cannot bind dev.scm.fake", "err", err)
		return
	}
	s.scm.removeCommands = append(s.scm.removeCommands, remove)
}

// addFakeSCM adds what a fixture describes, with the commands and menus of the fake provider (once for the window).
func (s *shellState) addFakeSCM(f fakeFixture) error {
	win := s.win
	if win.SCM == nil {
		return errors.New("DevNoWindow", "the window has no Source Control model")
	}
	s.fakeProvider()
	for _, fs := range f.SourceControls {
		root := fs.Root
		if root == "" {
			root = "."
		}
		if !filepath.IsAbs(root) {
			base := win.Folder
			if base == "" {
				base, _ = os.Getwd()
			}
			root = filepath.Join(base, root)
		}
		id, label := fs.ID, fs.Label
		if id == "" {
			id = "git"
		}
		if label == "" {
			label = "Git"
		}
		src := win.SCM.Add(scmcore.SourceSpec{ProviderID: id, Label: label, RootURI: "file://" + filepath.ToSlash(root), ContextValue: "repository", Owner: "dev"})
		var status []scmcore.StatusCommand
		for _, st := range fs.Status {
			status = append(status, scmcore.StatusCommand{Text: st.Text, Tooltip: st.Tooltip, Command: st.Command})
		}
		src.SetStatusBarCommands(status)
		if fs.Count != nil {
			src.SetCount(*fs.Count)
		}
		src.SetBusy(fs.Busy)
		for _, fg := range fs.Groups {
			g := src.AddGroup(fg.ID, fg.Label, fg.HideWhenEmpty, "")
			var rs []scmcore.Resource
			for _, fr := range fg.Resources {
				uri := "file://" + filepath.ToSlash(filepath.Join(root, fr.Path))
				rs = append(rs, scmcore.Resource{URI: uri, Decorations: scmcore.Decorations{Icon: fr.Icon, Tooltip: fr.Tooltip,
					StrikeThrough: fr.StrikeThrough, Faded: fr.Faded}})
				if fr.Letter != "" {
					win.scmDecorations.Set(uri, scmview.Decoration{Badge: fr.Letter, Tooltip: fr.Tooltip, Color: fr.Color})
				}
			}
			g.SetResources(rs)
		}
	}
	if f.Tree {
		win.SCM.SetViewMode(scmcore.ViewTree)
	}
	if f.Hover != "" {
		base := win.Folder
		if len(f.SourceControls) > 0 {
			base = filepath.Join(win.Folder, f.SourceControls[0].Root)
		}
		s.scm.view.SetHover("file://" + filepath.ToSlash(filepath.Join(base, f.Hover)))
	}
	return nil
}

// fakeProvider registers the commands and menu items of the fake provider, once for the window.
func (s *shellState) fakeProvider() {
	win := s.win
	if _, ok := win.Commands.Lookup("dev.scm.stage"); ok {
		return
	}
	for _, c := range fakeCommands {
		if err := win.Commands.Register(command.Meta{ID: c.id, Title: c.title, Icon: c.icon}); err != nil {
			win.Process.Logger.Warn("a command of the fake provider cannot be registered", "command", c.id, "err", err)
			continue
		}
		id := c.id
		remove, err := win.Commands.SetHandler(id, func(_ command.Context, args any) error {
			win.Process.Logger.Info("dev.scm command", "command", id, "args", args)
			return nil
		})
		if err == nil {
			s.scm.removeCommands = append(s.scm.removeCommands, remove)
		}
	}
	when := func(clause string) contextkey.Expr {
		e, err := contextkey.Parse(clause)
		if err != nil {
			panic(err)
		}
		return e
	}
	git := when("scmProvider == git")
	inGroup := func(g string) contextkey.Expr { return when("scmProvider == git && scmResourceGroup == " + g) }
	remove, err := win.Menus.AddRemovable(
		command.MenuItem{Menu: command.SCMResourceContext, Command: "dev.scm.openFile", Group: "inline", Order: 1, When: git},
		command.MenuItem{Menu: command.SCMResourceContext, Command: "dev.scm.discard", Group: "inline", Order: 2, When: inGroup("workingTree")},
		command.MenuItem{Menu: command.SCMResourceContext, Command: "dev.scm.stage", Group: "inline", Order: 3, When: when("scmProvider == git && scmResourceGroup =~ /^(workingTree|merge)$/")},
		command.MenuItem{Menu: command.SCMResourceContext, Command: "dev.scm.unstage", Group: "inline", Order: 3, When: inGroup("index")},
		command.MenuItem{Menu: command.SCMResourceContext, Command: "dev.scm.openChanges", Group: "navigation", Order: 1, When: git},
		command.MenuItem{Menu: command.SCMResourceContext, Command: "dev.scm.openFile", Group: "navigation", Order: 2, When: git},
		command.MenuItem{Menu: command.SCMResourceContext, Command: "dev.scm.stage", Group: "1_modification", Order: 1, When: inGroup("workingTree")},
		command.MenuItem{Menu: command.SCMResourceContext, Command: "dev.scm.discard", Group: "1_modification", Order: 2, When: inGroup("workingTree")},
		command.MenuItem{Menu: command.SCMResourceContext, Command: "dev.scm.unstage", Group: "1_modification", Order: 1, When: inGroup("index")},
		command.MenuItem{Menu: command.SCMResourceGroupContext, Command: "dev.scm.stageAll", Group: "inline", Order: 1, When: when("scmProvider == git && scmResourceGroup =~ /^(workingTree|merge)$/")},
		command.MenuItem{Menu: command.SCMResourceGroupContext, Command: "dev.scm.unstageAll", Group: "inline", Order: 1, When: inGroup("index")},
		command.MenuItem{Menu: command.SCMResourceGroupContext, Command: "dev.scm.discardAll", Group: "inline", Order: 0, When: inGroup("workingTree")},
		command.MenuItem{Menu: command.SCMResourceGroupContext, Command: "dev.scm.stageAll", Group: "1_modification", Order: 1, When: inGroup("workingTree")},
		command.MenuItem{Menu: command.SCMTitle, Command: "dev.scm.commit", Group: "navigation", Order: 1, When: git},
		command.MenuItem{Menu: command.SCMTitle, Command: "dev.scm.refresh", Group: "navigation", Order: 2, When: git},
		command.MenuItem{Menu: command.SCMTitle, Command: "dev.scm.pull", Group: "1_sync", Order: 1, When: git},
		command.MenuItem{Menu: command.SCMTitle, Command: "dev.scm.push", Group: "1_sync", Order: 2, When: git},
		command.MenuItem{Menu: command.SCMSourceControl, Command: "dev.scm.refresh", Group: "1_refresh", When: git},
	)
	if err != nil {
		win.Process.Logger.Warn("the menu items of the fake provider cannot be added", "err", err)
		return
	}
	s.scm.stops = append(s.scm.stops, remove)
}
