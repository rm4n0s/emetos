package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

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

// fakeCommand is a command of the action button of a fixture.
type fakeCommand struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// fakeButton is the action button of a fixture: the title of the primary command (the id is dev.scm.commit unless it is set), the
// commands of the dropdown by groups, and whether it is enabled.
type fakeButton struct {
	Command     string          `json:"command"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Disabled    bool            `json:"disabled"`
	Secondary   [][]fakeCommand `json:"secondary"`
}

// fakeValidation is the rule of the validation of a fixture: a message of a level when the first line of the text is longer than
// SubjectLength characters (Git's git.inputValidationSubjectLength, which the Git extension will check, step 24.10).
type fakeValidation struct {
	SubjectLength int    `json:"subjectLength"`
	Level         string `json:"level"`
}

// fakeSource is a source control of a fixture.
type fakeSource struct {
	ID          string          `json:"id"`
	Label       string          `json:"label"`
	Root        string          `json:"root"`
	Status      []fakeStatus    `json:"status"`
	Count       *int            `json:"count"`
	Busy        bool            `json:"busy"`
	Groups      []fakeGroup     `json:"groups"`
	Placeholder string          `json:"placeholder"`
	Input       string          `json:"input"`
	HideInput   bool            `json:"hideInput"`
	Button      *fakeButton     `json:"actionButton"`
	Validation  *fakeValidation `json:"validation"`
}

// fakeFixture is the fixture of dev.scm.fake.
type fakeFixture struct {
	SourceControls []fakeSource `json:"sourceControls"`
	// Hover is the file whose row is drawn as hovered, so that a proof shows its inline actions.
	Hover string `json:"hover"`
	// Tree shows the changes as a tree.
	Tree bool `json:"tree"`
	// Input is the text of the box of the first source control, and Dropdown opens the dropdown of its action button; both come
	// from the arguments of the command (they are not in the file).
	Input    string `json:"-"`
	Dropdown bool   `json:"-"`
}

// parseFakeSCM reads the arguments of dev.scm.fake: {"fixture": "path.json", "input": "text", "dropdown": true}, or the path as a
// string. The path is relative to the folder Emetos runs in; input is the text of the box of the first source control and dropdown
// opens the dropdown of its action button. The tags are DevSCMFakeInvalid, DevSCMFakeReadFailed and DevSCMFakeFixtureInvalid.
func parseFakeSCM(args any) (fakeFixture, error) {
	var path, input string
	var dropdown bool
	switch v := args.(type) {
	case string:
		path = v
	case map[string]any:
		path, _ = v["fixture"].(string)
		input, _ = v["input"].(string)
		dropdown, _ = v["dropdown"].(bool)
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
	f.Input, f.Dropdown = input, dropdown
	return f, nil
}

// registerDevSCM registers dev.scm.fake and dev.scm.input; the window binds their handlers while it is shown.
func registerDevSCM(r *command.Registry) {
	for _, meta := range []command.Meta{
		{ID: "dev.scm.fake", Description: `Add source controls from a JSON fixture. Arguments: {"fixture": "<path>", "input": "<text of the box>", "dropdown": true}.`},
		{ID: "dev.scm.input", Description: `Set the text of the commit box of a source control, {"text": "...", "repository": <index>, "focus": true}; with no text, report the box and the commands the fake provider ran. Read-only then.`},
	} {
		err := RegisterDev(r, meta, func(command.Context, any) error {
			return errors.New("DevNoWindow", "no window is shown")
		})
		if err != nil {
			// the ids are fixed, new and start with dev., so a failure is a mistake here
			panic(err)
		}
	}
}

// devSCMRun is a command of the fake provider that ran: its id, its argument and the text of the box of its repository.
type devSCMRun struct {
	Command string `json:"command"`
	Args    any    `json:"args,omitempty"`
	Input   string `json:"input"`
}

// devSCMInput is what dev.scm.input answers when it is not given a text.
type devSCMInput struct {
	// Value is the text of the box, Focused tells whether the box has the keyboard, and Runs are the commands of the fake provider that
	// ran, in order.
	Value   string      `json:"value"`
	Focused bool        `json:"focused"`
	Runs    []devSCMRun `json:"runs"`
}

// devSCMInputArgs reads the arguments of dev.scm.input. The tags are DevSCMInputInvalid.
func devSCMInputArgs(args any) (text string, hasText bool, repo int, focus bool, err error) {
	m, ok := args.(map[string]any)
	if args != nil && !ok {
		return "", false, 0, false, errors.New("DevSCMInputInvalid", `dev.scm.input takes {"text": "..."}`, "arguments", args)
	}
	text, hasText = m["text"].(string)
	focus, _ = m["focus"].(bool)
	if n, ok := m["repository"].(float64); ok {
		repo = int(n)
	}
	return text, hasText, repo, focus, nil
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
	{"dev.scm.commitAmend", "Commit (Amend)", ""},
	{"dev.scm.commitSigned", "Commit (Signed Off)", ""},
	{"dev.scm.commitEmpty", "Commit (Empty)", ""},
	{"dev.scm.undoCommit", "Undo Last Commit", ""},
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
	remove, err = win.Commands.SetHandler("dev.scm.input", func(ctx command.Context, args any) error { return s.devSCMInput(ctx, args) })
	if err != nil {
		win.Process.Logger.Error("cannot bind dev.scm.input", "err", err)
		return
	}
	s.scm.removeCommands = append(s.scm.removeCommands, remove)
}

// devSCMInput sets the text of the box of a source control, or answers with it and with what the fake provider ran.
func (s *shellState) devSCMInput(ctx command.Context, args any) error {
	win := s.win
	text, hasText, repo, focus, err := devSCMInputArgs(args)
	if err != nil {
		return err
	}
	sources := win.SCM.Sources()
	if repo < 0 || repo >= len(sources) {
		return errors.New("DevSCMInputNoRepository", "the source control does not exist", "index", repo, "count", len(sources))
	}
	src := sources[repo]
	if hasText {
		src.Input().SetValue(text)
	}
	if focus {
		src.Input().Focus()
	}
	if !hasText && !focus {
		setDevAnswer(ctx.Ctx, devSCMInput{Value: src.Input().Value(), Focused: s.scm.view.InputFocused(src), Runs: slices.Clone(s.scm.fakeRuns)})
	}
	return nil
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
		s.fakeInput(src, fs)
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
	if srcs := win.SCM.Sources(); len(srcs) > 0 {
		first := srcs[0]
		if f.Input != "" {
			first.Input().SetValue(f.Input)
		}
		if f.Dropdown {
			s.openFakeDropdown(first, 120, 6)
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

// fakeInput gives a fake source control what a Git-like provider gives the box: the placeholder with the key of the accept command,
// the accept command, the action button, and the validation rule of the fixture.
func (s *shellState) fakeInput(src *scmcore.Source, fs fakeSource) {
	in := src.Input()
	if fs.HideInput {
		in.SetVisible(false)
	}
	placeholder := fs.Placeholder
	if placeholder == "" {
		placeholder = `Message ({0} to commit on "main")`
	}
	in.SetPlaceholder(placeholder)
	if fs.Input != "" {
		in.SetValue(fs.Input)
	}
	arg := scmcore.SourceArg(src)
	src.SetAcceptInputCommand(scmcore.Command{ID: "dev.scm.commit", Title: "Commit", Args: arg})
	if b := fs.Button; b != nil {
		id := b.Command
		if id == "" {
			id = "dev.scm.commit"
		}
		button := scmcore.ActionButton{Command: scmcore.Command{ID: id, Title: b.Title, Args: arg}, Enabled: !b.Disabled, Description: b.Description}
		for _, g := range b.Secondary {
			var group []scmcore.Command
			for _, c := range g {
				group = append(group, scmcore.Command{ID: c.ID, Title: c.Title, Args: arg})
			}
			button.Secondary = append(button.Secondary, group)
		}
		src.SetActionButton(&button)
	}
	if v := fs.Validation; v != nil && v.SubjectLength > 0 {
		level := scmcore.ValidationWarning
		switch v.Level {
		case "error":
			level = scmcore.ValidationError
		case "info":
			level = scmcore.ValidationInfo
		}
		in.SetValidation(func(text string) *scmcore.Validation {
			subject, _, _ := strings.Cut(text, "\n")
			if n := len([]rune(subject)); n > v.SubjectLength {
				return &scmcore.Validation{Message: fmt.Sprintf("The subject has %d characters; %d or fewer are recommended.", n, v.SubjectLength), Level: level}
			}
			return nil
		})
	}
}

// openFakeDropdown opens the dropdown of the action button of a source control as soon as its row is on screen, trying for some
// frames (the command that shows the view may come after the one that makes the source control). It lets settle frames pass first,
// so that the menu is placed under the button after the box has taken the height of its text.
func (s *shellState) openFakeDropdown(src *scmcore.Source, tries, settle int) {
	if tries <= 0 || s.binding == nil {
		return
	}
	s.afterFrame(s.binding, func() {
		if settle > 0 {
			s.openFakeDropdown(src, tries-1, settle-1)
			return
		}
		if s.scm.view == nil || !s.scm.view.OpenActionDropdown(src) {
			s.openFakeDropdown(src, tries-1, 0)
		}
	})
}

// fakeRan records a command of the fake provider that ran with the text of the box of its repository (the first one when the argument
// names none), and, as a provider does after a commit, empties the box.
func (s *shellState) fakeRan(id string, args any) {
	win := s.win
	var src *scmcore.Source
	for _, a := range scmArgsOf(args) {
		if src = win.SCM.Source(a.sourceControl); src != nil {
			break
		}
	}
	if src == nil {
		if srcs := win.SCM.Sources(); len(srcs) > 0 {
			src = srcs[0]
		}
	}
	run := devSCMRun{Command: id, Args: args}
	if src != nil {
		run.Input = src.Input().Value()
		if id == "dev.scm.commit" {
			src.Input().SetValue("")
		}
	}
	s.scm.fakeRuns = append(s.scm.fakeRuns, run)
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
			s.fakeRan(id, args)
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
