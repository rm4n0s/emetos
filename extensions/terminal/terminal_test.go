package terminal

import (
	"context"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
	"github.com/rm4n0s/emetos/extensions/internal/pty"
)

// extRig is the extension activated over the fake API, with the test shell as its default profile.
type extRig struct {
	t   *testing.T
	api *fakeapi.API
	ext *terminals
	dir string
	// cancel ends the context of the activation.
	cancel context.CancelFunc
}

// newExtRig activates the extension in a folder; profile is the program of the default profile ("" is the test shell) and
// args its arguments. The shells see the isolated environment of the tests.
func newExtRig(t *testing.T, profile string, args ...string) *extRig {
	t.Helper()
	return newExtRigAt(t, t.TempDir(), nil, profile, args...)
}

// newExtRigAt is newExtRig in a folder that may have been used before; before is called on the fake API before the extension
// is activated (to give it the state of an earlier session, say).
func newExtRigAt(t *testing.T, dir string, before func(*fakeapi.API), profile string, args ...string) *extRig {
	t.Helper()
	if profile == "" {
		profile = testShell(t)
	}
	e := New().(*terminals)
	api := fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: dir, ID: dir})
	api.SetSetting(settingProfiles, map[string]any{"test": map[string]any{"path": profile, "args": args}})
	api.SetSetting(settingDefaultProfile, "test")
	if before != nil {
		before(api)
	}
	e.env = environ{vars: isolatedEnv(t), lookPath: exec.LookPath, home: dir}
	ctx, cancel := context.WithCancel(context.Background())
	if err := e.Activate(ctx, api); err != nil {
		t.Fatal(err)
	}
	r := &extRig{t: t, api: api, ext: e, dir: dir, cancel: cancel}
	t.Cleanup(func() {
		cancel()
		for _, en := range e.all() {
			en.term.Kill()
			select {
			case <-en.term.Exited():
			case <-time.After(5 * time.Second):
				t.Errorf("terminal %d did not end", en.term.ID())
			}
			en.term.Wait()
		}
	})
	return r
}

// run runs a command and pumps what it posted.
func (r *extRig) run(id string, args any) {
	r.t.Helper()
	if err := r.api.Run(id, args); err != nil {
		r.t.Fatalf("%s: %v", id, err)
	}
	r.api.Pump()
}

// counts are the number of terminals and of groups.
func (r *extRig) counts() (terminals, groups int) { return r.ext.count(), len(r.ext.groups) }

func TestTheManifestDeclaresWhatItNeeds(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if len(m.Contributes.PanelViews) != 1 || m.Contributes.PanelViews[0].ID != ViewID {
		t.Errorf("panel views = %+v", m.Contributes.PanelViews)
	}
	// every key binds a command the manifest declares, and the keys of VS Code's Linux defaults are there
	declared := map[string]bool{}
	for _, c := range m.Contributes.Commands {
		declared[c.ID] = true
	}
	keys := map[string]string{}
	for _, kb := range m.Contributes.Keybindings {
		if !declared[kb.Command] {
			t.Errorf("the key %s binds %s, which the manifest does not declare", kb.Key, kb.Command)
		}
		// a key two commands share (Ctrl+Shift+Up, step 17d.5) is checked for the first
		if _, ok := keys[kb.Key]; !ok {
			keys[kb.Key] = kb.Command
		}
	}
	for key, command := range map[string]string{
		"ctrl+`": CmdToggle, "ctrl+shift+`": CmdNew, "ctrl+shift+5": CmdSplit, "ctrl+shift+c": CmdCopySelection,
		"ctrl+shift+v": CmdPaste, "ctrl+shift+up": CmdScrollUp, "shift+pageup": CmdScrollUpPage, "shift+home": CmdScrollToTop,
	} {
		if keys[key] != command {
			t.Errorf("the key %s runs %q, want %s", key, keys[key], command)
		}
	}
	// the commands of step 17d.3 are there, with the keys of VS Code
	for _, c := range []string{"runSelectedText", "runActiveFile", "sendSequence", "rename", "renameWithArgs", "selectDefaultShell", "focusFind",
		"hideFind", "findNext", "findPrevious", "toggleFindRegex", "toggleFindWholeWord", "toggleFindCaseSensitive", "focusAtIndex1", "focusAtIndex9"} {
		if !declared["workbench.action.terminal."+c] {
			t.Errorf("the command %s is not declared", c)
		}
	}
	// Search Workspace needs the privilege to run the command of the Search extension, and a selection to act on
	if !m.HasPrivilege(extension.PrivilegeCommandsExecute) || len(m.Privileges) != 1 {
		t.Errorf("privileges = %v", m.Privileges)
	}
	for _, kb := range m.Contributes.Keybindings {
		if kb.Command == CmdSearchWorkspace && (kb.Key != "ctrl+shift+f" || kb.When != keyFocus+" && "+keyTextSelected) {
			t.Errorf("the key of searchWorkspace is %+v", kb)
		}
	}
	for key, command := range map[string]string{"ctrl+f": CmdFocusFind, "f3": CmdFindNext, "shift+f3": CmdFindPrevious, "alt+r": CmdToggleFindRegex} {
		if keys[key] != command {
			t.Errorf("the key %s runs %q, want %s", key, keys[key], command)
		}
	}
	// the context keys of the manifest are the ones of the keys and the when-clauses
	for _, kb := range m.Contributes.Keybindings {
		for _, word := range strings.FieldsFunc(kb.When, func(r rune) bool { return r == ' ' || r == '&' || r == '|' || r == '!' }) {
			if strings.HasPrefix(word, "terminal") && !slices.Contains(m.Contributes.ContextKeys, word) {
				t.Errorf("the when-clause of %s reads %s, which the manifest does not declare", kb.Command, word)
			}
		}
	}
}

func TestEveryCommandOfTheManifestHasAHandler(t *testing.T) {
	r := newExtRig(t, "")
	for _, c := range r.ext.Manifest().Contributes.Commands {
		if !r.api.HasHandler(c.ID) {
			t.Errorf("the command %s has no handler", c.ID)
		}
	}
}

func TestCommandsMakeFocusSplitAndKillTerminals(t *testing.T) {
	r := newExtRig(t, "")
	r.run(CmdNew, nil)
	if n, g := r.counts(); n != 1 || g != 1 {
		t.Fatalf("after New: %d terminals in %d groups", n, g)
	}
	if r.api.ContextKey(keyIsOpen) != true || r.api.ContextKey(keyCount) != float64(1) || r.api.ContextKey(keyHasBeenCreated) != true {
		t.Errorf("context keys after New: open %v, count %v, created %v", r.api.ContextKey(keyIsOpen), r.api.ContextKey(keyCount), r.api.ContextKey(keyHasBeenCreated))
	}
	r.run(CmdNew, nil)
	r.run(CmdSplit, nil)
	if n, g := r.counts(); n != 3 || g != 2 {
		t.Fatalf("after New, New, Split: %d terminals in %d groups", n, g)
	}
	if got := len(r.ext.activeGroup().entries); got != 2 || r.ext.cur != 1 {
		t.Errorf("the split group has %d terminals and is the group %d", got, r.ext.cur)
	}
	if r.api.ContextKey(keySplitTerminal) != true || r.api.ContextKey(keyGroupCount) != float64(2) {
		t.Errorf("context keys after Split: split %v, groups %v", r.api.ContextKey(keySplitTerminal), r.api.ContextKey(keyGroupCount))
	}
	// the focus commands move around
	r.run(CmdFocusPrevPane, nil)
	if r.ext.activeGroup().active != 0 {
		t.Errorf("FocusPreviousPane left the active terminal at %d", r.ext.activeGroup().active)
	}
	r.run(CmdFocusNextPane, nil)
	if r.ext.activeGroup().active != 1 {
		t.Errorf("FocusNextPane left the active terminal at %d", r.ext.activeGroup().active)
	}
	r.run(CmdFocusPrevious, nil)
	if r.ext.cur != 0 {
		t.Errorf("FocusPrevious left the group at %d", r.ext.cur)
	}
	r.run(CmdFocusNext, nil)
	r.run(CmdFocusNext, nil) // around
	if r.ext.cur != 0 {
		t.Errorf("FocusNext twice left the group at %d, want 0 (around)", r.ext.cur)
	}
	// Kill takes the active one away; a group of one terminal goes with it
	r.run(CmdKill, nil)
	if n, g := r.counts(); n != 2 || g != 1 {
		t.Errorf("after Kill: %d terminals in %d groups", n, g)
	}
	view := r.api.View(ViewID)
	hides := view.Hides
	r.run(CmdKill, nil)
	r.run(CmdKill, nil)
	if n, g := r.counts(); n != 0 || g != 0 {
		t.Errorf("after killing every terminal: %d terminals in %d groups", n, g)
	}
	if view.Hides <= hides || r.api.ContextKey(keyIsOpen) != false {
		t.Errorf("the last Kill did not hide the panel (hides %d, was %d) or left terminalIsOpen %v", view.Hides, hides, r.api.ContextKey(keyIsOpen))
	}
	if !r.ext.suppressOpen {
		t.Error("the view would make a new terminal while the panel hides")
	}
	// the state is saved while the view is still on screen: the view still makes none
	if _, err := view.Content.SaveState(); err != nil {
		t.Fatal(err)
	}
	if !r.ext.suppressOpen {
		t.Error("saving the state of a view that is on screen made it forget that the panel hides")
	}
	// the view goes off screen and comes back later: it may make a terminal again
	r.ext.viewGone()
	if r.ext.suppressOpen {
		t.Error("the panel was hidden and the view still refuses to make a terminal")
	}
}

func TestNewWithArgumentsAndProfiles(t *testing.T) {
	r := newExtRig(t, "")
	r.run(CmdNew, map[string]any{"name": "builder", "cwd": r.dir})
	e := r.ext.activeEntry()
	if e.term.Name() != "builder" || e.term.Cwd() != r.dir {
		t.Errorf("name %q, cwd %q", e.term.Name(), e.term.Cwd())
	}
	if got := r.ext.title(e); got != "builder" {
		t.Errorf("the tab is %q", got)
	}
	// the dropdown of profiles lists the profiles that exist and starts the one that is chosen
	r.run(CmdNewWithProfile, nil)
	pick := r.api.LastPick()
	if pick == nil {
		t.Fatal("no quick pick of profiles")
	}
	if labels := pick.Labels(); len(labels) != 1 || !strings.Contains(labels[0], "test") {
		t.Errorf("the profiles are %q", labels)
	}
	pick.Choose(0)
	if n, _ := r.counts(); n != 2 {
		t.Errorf("%d terminals after choosing a profile", n)
	}
}

func TestATerminalThatEndsGoesAndAFailureIsReported(t *testing.T) {
	tests := []struct {
		name, script string
		note         string // "" for no notification
	}{
		{"a clean exit", "exit 0", ""},
		{"a failure status", "exit 3", "terminated with exit code: 3"},
		{"killed by a hangup", "kill -HUP $$", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newExtRig(t, "/bin/sh", "-c", tt.script)
			r.run(CmdNew, nil)
			r.api.WaitFor("the terminal to go", func() bool { return r.ext.count() == 0 })
			notes := r.api.Notes()
			switch {
			case tt.note == "" && len(notes) != 0:
				t.Errorf("notifications for a clean end: %+v", notes)
			case tt.note != "" && (len(notes) != 1 || !strings.Contains(notes[0].Message, tt.note)):
				t.Errorf("notifications = %+v, want one with %q", notes, tt.note)
			}
			if r.api.View(ViewID).Hides == 0 {
				t.Error("the last terminal ended and the panel stayed")
			}
		})
	}
}

func TestAStartThatFailsIsShownAndNotRepeated(t *testing.T) {
	r := newExtRig(t, "")
	r.ext.start = func(context.Context, pty.Options) (process, error) {
		return nil, errors.New("PtyTestStartFailed", "the program cannot start")
	}
	r.run(CmdNew, nil)
	if n, _ := r.counts(); n != 0 {
		t.Errorf("%d terminals after a start that failed", n)
	}
	notes := r.api.Notes()
	if len(notes) != 1 || notes[0].Severity != "error" || !strings.Contains(notes[0].Message, "failed to launch") {
		t.Errorf("notifications = %+v", notes)
	}
	if r.ext.failure == "" {
		t.Error("the view has no message about the failure, and would try again in every frame")
	}
}

func TestWhichKeysGoToTheShell(t *testing.T) {
	tests := []struct {
		name    string
		user    []string
		sendAll bool
		focused bool
		command string
		want    bool
	}{
		{"the focus is elsewhere", nil, false, false, "workbench.action.files.save", true},
		{"a command of the default list", nil, true, true, "workbench.action.quickOpen", true},
		{"the toggle of the terminal", nil, true, true, CmdToggle, true},
		{"a command that is not in the list", nil, true, true, "workbench.action.files.save", false},
		{"a command the user added", []string{"workbench.action.files.save"}, true, true, "workbench.action.files.save", true},
		{"a default the user took away", []string{"-workbench.action.quickOpen"}, true, true, "workbench.action.quickOpen", false},
		{"sendKeybindingsToShell", nil, true, true, "workbench.action.quickOpen", true}, // set below
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newExtRig(t, "")
			r.run(CmdNew, nil)
			c := *r.ext.cfg
			c.skip = newSkipSet(tt.user)
			if i == len(tests)-1 {
				c.sendKeys, tt.want = true, false
			}
			r.ext.cfg = &c
			if tt.focused {
				r.ext.paneFocusChanged(r.ext.activeEntry().pv, true)
			} else {
				r.ext.paneFocus = nil
			}
			if got := r.ext.allowCommand(tt.command); got != tt.want {
				t.Errorf("allowCommand(%q) = %v, want %v", tt.command, got, tt.want)
			}
		})
	}
}

func TestTabTitles(t *testing.T) {
	tests := []struct {
		format string
		m      meta
		cwd    string
		want   string
	}{
		{"${process}", meta{process: "vim"}, "/home/u/p", "vim"},
		{"${process}", meta{}, "/home/u/p", "bash"},
		{"${cwdFolder}", meta{}, "/home/u/project", "project"},
		{"${process} - ${sequence}", meta{process: "bash", title: "~/p"}, "/x", "bash - ~/p"},
		{"${cwd}", meta{}, "/home/u/p", "/home/u/p"},
		{"", meta{process: "zsh"}, "/", "zsh"},
	}
	for _, tt := range tests {
		t.Run(tt.format, func(t *testing.T) {
			if got := tabTitle(tt.format, tt.m, "bash", tt.cwd); got != tt.want {
				t.Errorf("tabTitle(%q) = %q, want %q", tt.format, got, tt.want)
			}
		})
	}
}

func TestTheTabsListShowsWhenTheSettingsSay(t *testing.T) {
	tests := []struct {
		name              string
		enabled           bool
		hide              string
		terminals, groups int
		want              bool
	}{
		{"one terminal, the default", true, "singleTerminal", 1, 1, false},
		{"two terminals, the default", true, "singleTerminal", 2, 1, true},
		{"one terminal, never hide", true, "never", 1, 1, true},
		{"a split in one group, singleGroup", true, "singleGroup", 2, 1, false},
		{"two groups, singleGroup", true, "singleGroup", 2, 2, true},
		{"disabled", false, "never", 3, 3, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ext := &terminals{cfg: &config{tabsEnabled: tt.enabled, tabsHide: tt.hide}}
			for g := 0; g < tt.groups; g++ {
				ext.groups = append(ext.groups, &group{})
			}
			for n := 0; n < tt.terminals; n++ {
				g := ext.groups[min(n, tt.groups-1)]
				g.entries = append(g.entries, &entry{})
			}
			if got := ext.showTabs(); got != tt.want {
				t.Errorf("showTabs = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSettingsChangeTheConfiguration(t *testing.T) {
	r := newExtRig(t, "")
	r.run(CmdNew, nil)
	r.api.SetSetting(settingScrollback, 50)
	r.api.SetSetting(settingFontSize, 18)
	r.api.SetSetting(settingTabsHide, "never")
	if c := r.ext.cfg; c.scrollback != 50 || c.fontSize != 18 || c.tabsHide != "never" {
		t.Errorf("config = scrollback %d, font size %v, hide %q", c.scrollback, c.fontSize, c.tabsHide)
	}
	em := r.ext.activeEntry().term.Emulator()
	if _, err := em.Write([]byte(strings.Repeat("a line\r\n", 200))); err != nil {
		t.Fatal(err)
	}
	if got := em.Scrollback(); got != 50 {
		t.Errorf("the emulator keeps %d lines of scrollback, want 50", got)
	}
}

func TestEndingTheScopeEndsTheShells(t *testing.T) {
	dir := t.TempDir()
	e := New().(*terminals)
	api := fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "p", Folder: dir, ID: dir})
	api.SetSetting(settingProfiles, map[string]any{"test": map[string]any{"path": testShell(t)}})
	api.SetSetting(settingDefaultProfile, "test")
	e.env = environ{vars: isolatedEnv(t), lookPath: exec.LookPath, home: dir}
	ctx, cancel := context.WithCancel(context.Background())
	if err := e.Activate(ctx, api); err != nil {
		t.Fatal(err)
	}
	if err := api.Run(CmdNew, nil); err != nil {
		t.Fatal(err)
	}
	term := e.activeEntry().term
	cancel()
	select {
	case <-term.Exited():
	case <-time.After(10 * time.Second):
		t.Fatal("the shell outlived the context of the extension")
	}
	term.Wait()
}
