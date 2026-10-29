package terminal

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The terminals in the editor area (step 17d.7): moving between the panel and a tab, where a new terminal goes, a tab and its
// terminal ending together, and what the tab shows.

// newTerminalReady makes a terminal in the panel with the command New and waits for its prompt.
func (r *extRig) newTerminalReady() *entry {
	r.t.Helper()
	r.run(CmdNew, nil)
	e := r.ext.activeEntry()
	waitText(r.t, e.term, "$")
	return e
}

// say types a line in a terminal and waits for it to echo the text.
func say(t *testing.T, e *entry, line, want string) {
	t.Helper()
	e.term.Send([]byte(line + "\r"))
	waitText(t, e.term, want)
}

// screenOf is everything the terminal shows, as the serialization the saved sessions are made of writes it: two terminals with
// the same cells give the same bytes.
func screenOf(e *entry) []byte { return e.term.Emulator().Serialize(-1) }

// openTabs are the editor tabs of the terminals that are not closed.
func openTabs(r *extRig) []*fakeapi.Tab {
	var out []*fakeapi.Tab
	for _, tab := range r.api.EditorTabs() {
		if !tab.Closed {
			out = append(out, tab)
		}
	}
	return out
}

func TestMovingATerminalKeepsItsProcessAndItsCells(t *testing.T) {
	tests := []struct {
		name       string
		moves      []string
		wantEditor bool
	}{
		{"to the editor area", []string{CmdMoveToEditor}, true},
		{"to the editor area and back", []string{CmdMoveToEditor, CmdMoveToPanel}, false},
		{"to the editor area, back and again", []string{CmdMoveToEditor, CmdMoveToPanel, CmdMoveToEditor}, true},
		{"back with no terminal in an editor does nothing", []string{CmdMoveToPanel}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newExtRig(t, "")
			e := r.newTerminalReady()
			say(t, e, "echo before", "before\n$")
			term, pid, cells := e.term, e.term.Pid(), screenOf(e)
			for _, cmd := range tt.moves {
				r.run(cmd, nil)
				if e.term != term || e.term.Pid() != pid {
					t.Fatalf("after %s the terminal is %p pid %d, was %p pid %d", cmd, e.term, e.term.Pid(), term, pid)
				}
				if got := screenOf(e); !bytes.Equal(got, cells) {
					t.Fatalf("after %s the cells changed:\n%q\nwas\n%q", cmd, got, cells)
				}
				if _, exited := term.ExitCode(); exited {
					t.Fatalf("after %s the process ended", cmd)
				}
			}
			tabs := openTabs(r)
			switch {
			case tt.wantEditor && (len(tabs) != 1 || !e.inEditor() || len(r.ext.groups) != 0 || len(r.ext.editors) != 1):
				t.Fatalf("want the terminal in a tab alone: %d tabs, in editor %v, %d groups, %d in editors", len(tabs), e.inEditor(), len(r.ext.groups), len(r.ext.editors))
			case !tt.wantEditor && (len(tabs) != 0 || e.inEditor() || len(r.ext.groups) != 1 || len(r.ext.editors) != 0):
				t.Fatalf("want the terminal in the panel alone: %d tabs, in editor %v, %d groups, %d in editors", len(tabs), e.inEditor(), len(r.ext.groups), len(r.ext.editors))
			}
			// it still works where it is
			say(t, e, "echo after", "after\n$")
			// the tab, when there is one, is the terminal's
			if tt.wantEditor {
				if got := tabs[0].Title(); got != r.ext.title(e) {
					t.Errorf("the tab is called %q, the terminal %q", got, r.ext.title(e))
				}
				if r.api.ContextKey(keyCount) != float64(1) || r.api.ContextKey(keyIsOpen) != true {
					t.Errorf("context keys: count %v, open %v", r.api.ContextKey(keyCount), r.api.ContextKey(keyIsOpen))
				}
			}
		})
	}
}

func TestMovingATerminalOfASplitLeavesTheOthers(t *testing.T) {
	r := newExtRig(t, "")
	first := r.newTerminalReady()
	r.run(CmdSplit, nil)
	second := r.ext.activeEntry()
	waitText(t, second.term, "$")
	r.run(CmdMoveToEditor, nil) // the one with the keyboard: the split
	if second.inEditor() == false || first.inEditor() {
		t.Fatalf("the split is in an editor: %v, the first: %v", second.inEditor(), first.inEditor())
	}
	if n, g := r.counts(); n != 2 || g != 1 || len(r.ext.groups[0].entries) != 1 {
		t.Errorf("%d terminals in %d groups after the move", n, g)
	}
	if r.ext.activeEntry() != first {
		t.Error("the terminal left in the panel is not the active one")
	}
	if r.api.View(ViewID).Hides != 0 {
		t.Error("the panel hid with a terminal still in it")
	}
}

func TestMovingTheLastTerminalOfThePanelHidesItAndMakesNoNewOne(t *testing.T) {
	r := newExtRig(t, "")
	e := r.newTerminalReady()
	r.run(CmdMoveToEditor, nil)
	if r.api.View(ViewID).Hides == 0 || !r.ext.suppressOpen {
		t.Errorf("the panel did not hide (hides %d, suppress %v)", r.api.View(ViewID).Hides, r.ext.suppressOpen)
	}
	// the view comes back later with the terminal still in its tab: it makes none
	if _, err := r.api.View(ViewID).Content.SaveState(); err != nil {
		t.Fatal(err)
	}
	if r.ext.count() != 1 || !e.inEditor() {
		t.Errorf("%d terminals, in editor %v", r.ext.count(), e.inEditor())
	}
}

func TestAnEditorTabThatCannotBeOpenedLeavesTheTerminalInThePanel(t *testing.T) {
	r := newExtRig(t, "")
	e := r.newTerminalReady()
	// the window has no editor area to show the tab: Create ran, the open failed
	r.api.FailEditorOpen(true)
	r.run(CmdMoveToEditor, nil)
	if e.inEditor() || r.ext.panelCount() != 1 || len(r.ext.editors) != 0 {
		t.Fatalf("the terminal is in editor %v; %d in the panel, %d in editor tabs", e.inEditor(), r.ext.panelCount(), len(r.ext.editors))
	}
	if _, exited := e.term.ExitCode(); exited {
		t.Error("a failed move ended the terminal")
	}
	if notes := r.api.Notes(); len(notes) != 1 || !strings.Contains(notes[0].Message, "cannot move") {
		t.Errorf("notifications = %+v", notes)
	}
	say(t, e, "echo still", "still\n$")
}

func TestWhereANewTerminalGoes(t *testing.T) {
	tests := []struct {
		name       string
		location   string
		cmd        string
		args       any
		wantEditor bool
	}{
		{"New, the setting is view", "view", CmdNew, nil, false},
		{"New, the setting is editor", "editor", CmdNew, nil, true},
		{"Create in editor area, the setting is view", "view", CmdCreateEditor, nil, true},
		{"Create in editor area with a name", "view", CmdCreateEditor, map[string]any{"name": "tabbed"}, true},
		{"New with a profile, the setting is editor", "editor", CmdNew, "test", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newExtRig(t, "")
			r.api.SetSetting(settingDefaultLocation, tt.location)
			r.run(tt.cmd, tt.args)
			if r.ext.count() != 1 {
				t.Fatalf("%d terminals", r.ext.count())
			}
			e := r.ext.all()[0]
			if e.inEditor() != tt.wantEditor || (len(openTabs(r)) == 1) != tt.wantEditor {
				t.Errorf("in editor %v with %d open tabs, want editor %v", e.inEditor(), len(openTabs(r)), tt.wantEditor)
			}
			if m, ok := tt.args.(map[string]any); ok && e.term.Name() != m["name"] {
				t.Errorf("the name is %q", e.term.Name())
			}
			waitText(t, e.term, "$")
		})
	}
}

func TestATerminalMadeForAClientGoesToThePanelWhateverTheSetting(t *testing.T) {
	r := newExtRig(t, "")
	r.api.SetSetting(settingDefaultLocation, "editor")
	var reply createReply
	if err := r.ask(extension.RelayID, TopicCreate, createRequest{}, &reply); err != nil {
		t.Fatal(err)
	}
	e := r.ext.entryByID(reply.ID)
	if e == nil || e.inEditor() || len(openTabs(r)) != 0 {
		t.Fatalf("the terminal of the request: %+v, %d tabs", e, len(openTabs(r)))
	}
}

func TestASplitOfATerminalInAnEditorTabIsAnotherEditorTab(t *testing.T) {
	r := newExtRig(t, "")
	r.run(CmdCreateEditor, nil)
	first := r.ext.all()[0]
	waitText(t, first.term, "$")
	r.run(CmdSplit, nil)
	if n := len(r.ext.editors); n != 2 || r.ext.panelCount() != 0 {
		t.Fatalf("%d terminals in tabs and %d in the panel after a split of a terminal in a tab", n, r.ext.panelCount())
	}
}

func TestAnEditorTabAndItsTerminalEndTogether(t *testing.T) {
	tests := []struct {
		name string
		end  func(r *extRig, e *entry)
	}{
		{"the user closes the tab", func(_ *extRig, e *entry) { e.tab.Close() }},
		{"Kill the Active Terminal in Editor Area", func(r *extRig, _ *entry) { r.run(CmdKillEditor, nil) }},
		{"Kill", func(r *extRig, _ *entry) { r.run(CmdKill, nil) }},
		{"the program ends", func(_ *extRig, e *entry) { e.term.Send([]byte("exit\r")) }},
		{"Kill All Terminals", func(r *extRig, _ *entry) { r.run(CmdKillAll, nil) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newExtRig(t, "/bin/sh")
			r.run(CmdCreateEditor, nil)
			e := r.ext.all()[0]
			tab := e.tab.(*fakeapi.Tab)
			tt.end(r, e)
			r.api.WaitFor("the terminal to go", func() bool { return r.ext.count() == 0 })
			select {
			case <-e.term.Exited():
			case <-time.After(5 * time.Second):
				t.Fatal("the program did not end")
			}
			if !tab.Closed || len(openTabs(r)) != 0 {
				t.Errorf("the tab is still open")
			}
			if r.ext.count() != 0 || len(r.ext.editors) != 0 {
				t.Errorf("%d terminals are left", r.ext.count())
			}
		})
	}
}

func TestClosingTheTabOfAKilledTerminalWritesNoNotification(t *testing.T) {
	r := newExtRig(t, "/bin/sh")
	r.run(CmdCreateEditor, nil)
	e := r.ext.all()[0]
	e.tab.Close()
	r.api.WaitFor("the terminal to go", func() bool { return r.ext.count() == 0 })
	if notes := r.api.Notes(); len(notes) != 0 {
		t.Errorf("notifications for closing a tab: %+v", notes)
	}
}

func TestTheTabShowsTheTitleIconAndColorOfItsTerminal(t *testing.T) {
	r := newExtRig(t, "")
	r.run(CmdCreateEditor, nil)
	e := r.ext.all()[0]
	tab := e.tab.(*fakeapi.Tab)
	waitText(t, e.term, "$")
	if tab.Title() == "" || tab.Icon() != "terminal" || tab.IconColor() != "" {
		t.Errorf("a new tab: title %q, icon %q, color %q", tab.Title(), tab.Icon(), tab.IconColor())
	}
	// Rename
	r.run(CmdRename, nil)
	r.api.LastInput().Accept("builder")
	r.api.Pump()
	if tab.Title() != "builder" {
		t.Errorf("after Rename the tab is %q", tab.Title())
	}
	// Change Icon: the Codicons in a quick pick
	r.run(CmdChangeIcon, nil)
	pick := r.api.LastPick()
	choose := -1
	for i, l := range pick.Labels() {
		if l == "$(rocket) rocket" {
			choose = i
		}
	}
	if choose < 0 {
		t.Fatalf("the icons: %d labels, none is the rocket", len(pick.Labels()))
	}
	pick.Choose(choose)
	r.api.Pump()
	if tab.Icon() != "rocket" {
		t.Errorf("after Change Icon the tab icon is %q", tab.Icon())
	}
	// Change Color
	r.run(CmdChangeColor, nil)
	pick = r.api.LastPick()
	labels := pick.Labels()
	if len(labels) != 9 || labels[0] != "$(circle-slash) Default" || labels[2] != "$(circle-filled) Red" {
		t.Fatalf("the colors: %v", labels)
	}
	pick.Choose(2)
	r.api.Pump()
	if tab.IconColor() != "terminal.ansiRed" {
		t.Errorf("after Change Color the tab color is %q", tab.IconColor())
	}
	// Default takes it away again
	r.run(CmdChangeColor, nil)
	r.api.LastPick().Choose(0)
	r.api.Pump()
	if tab.IconColor() != "" {
		t.Errorf("after Default the tab color is %q", tab.IconColor())
	}
	// the hover names the process, the terminal and the folder
	for _, want := range []string{"Process ID (PID): ", "Command line: ", "Terminal: ", "Folder: ", "Shell integration"} {
		if !strings.Contains(tab.Hint(), want) {
			t.Errorf("the hover %q lacks %q", tab.Hint(), want)
		}
	}
}

func TestTheIconAndColorOfATabOfThePanelFollowTheChoice(t *testing.T) {
	r := newExtRig(t, "")
	e := r.newTerminalReady()
	r.ext.setColor(e, "terminal.ansiBlue")
	r.ext.setIcon(e, "bug")
	if m := e.term.Meta(); m.color != "terminal.ansiBlue" || m.icon != "bug" {
		t.Errorf("meta = %+v", m)
	}
	// a color that is not one of the ansi colors is refused
	r.ext.setColor(e, "editor.background")
	if e.term.Meta().color != "terminal.ansiBlue" {
		t.Errorf("a foreign color was taken: %q", e.term.Meta().color)
	}
	// an empty icon goes back to the profile's
	r.ext.setIcon(e, "")
	if e.term.Meta().icon == "bug" {
		t.Error("the icon was not reset")
	}
}

func TestJoinUnsplitKillAllAndNewWithCwd(t *testing.T) {
	r := newExtRig(t, "")
	a := r.newTerminalReady()
	b := r.newTerminalReady()
	c := r.newTerminalReady()
	if n, g := r.counts(); n != 3 || g != 3 {
		t.Fatalf("%d terminals in %d groups", n, g)
	}
	// Join Terminals...: the active one (c) and the one picked (a) share a tab
	r.run(CmdJoin, nil)
	pick := r.api.LastPick()
	if len(pick.Labels()) != 2 {
		t.Fatalf("the pick lists %v", pick.Labels())
	}
	pick.Choose(0)
	r.api.Pump()
	if n, g := r.counts(); n != 3 || g != 2 || len(groupOf(r.ext, c).entries) != 2 || groupOf(r.ext, a) != groupOf(r.ext, c) {
		t.Fatalf("after Join: %d terminals in %d groups", n, g)
	}
	if r.ext.activeEntry() != a {
		t.Error("the joined terminal is not the active one")
	}
	// Unsplit gives the active terminal a tab after its group
	r.run(CmdUnsplit, nil)
	if n, g := r.counts(); n != 3 || g != 3 || groupOf(r.ext, a) == groupOf(r.ext, c) {
		t.Fatalf("after Unsplit: %d terminals in %d groups", n, g)
	}
	if r.ext.groups[len(r.ext.groups)-2].entries[0] != c || r.ext.groups[len(r.ext.groups)-1].entries[0] != a {
		t.Errorf("the order of the tabs after Unsplit is wrong")
	}
	// a terminal alone in its tab is not unsplit
	r.run(CmdUnsplit, nil)
	if _, g := r.counts(); g != 3 {
		t.Errorf("Unsplit of a lone terminal changed the groups: %d", g)
	}
	// Join with one tab only
	_ = b
	// New with a folder: the argument, then the box
	sub := r.dir + "/sub"
	if err := mkdir(sub); err != nil {
		t.Fatal(err)
	}
	r.run(CmdNewWithCwd, map[string]any{"cwd": sub})
	if r.ext.count() != 4 || r.ext.activeEntry().term.InitialDir() != sub {
		t.Fatalf("%d terminals, the last starts in %q", r.ext.count(), r.ext.activeEntry().term.InitialDir())
	}
	r.run(CmdNewWithCwd, nil)
	in := r.api.LastInput()
	if in == nil || !strings.Contains(in.Options().Prompt, "Working directory") {
		t.Fatalf("the box = %+v", in)
	}
	in.Accept("sub")
	r.api.Pump()
	if r.ext.count() != 5 || r.ext.activeEntry().term.InitialDir() != sub {
		t.Errorf("%d terminals, the last starts in %q", r.ext.count(), r.ext.activeEntry().term.InitialDir())
	}
	r.run(CmdNewWithCwd, map[string]any{"cwd": r.dir + "/missing"})
	if r.ext.count() != 5 {
		t.Errorf("a folder that is not there made a terminal")
	}
	if notes := r.api.Notes(); len(notes) == 0 || !strings.Contains(notes[len(notes)-1].Message, "does not exist") {
		t.Errorf("notifications = %+v", notes)
	}
	// Kill All Terminals, the panel and the editor
	r.run(CmdCreateEditor, nil)
	if r.ext.count() != 6 || len(r.ext.editors) != 1 {
		t.Fatalf("%d terminals, %d in editors", r.ext.count(), len(r.ext.editors))
	}
	r.run(CmdKillAll, nil)
	if r.ext.count() != 0 {
		t.Errorf("%d terminals after Kill All", r.ext.count())
	}
}

func TestMovingATabInTheList(t *testing.T) {
	// a, b, c are three tabs; d and e are a split in a fourth
	setup := func(t *testing.T) (*extRig, []*entry) {
		r := newExtRig(t, "")
		var es []*entry
		for range 3 {
			es = append(es, r.newTerminalReady())
		}
		r.run(CmdNew, nil)
		d := r.ext.activeEntry()
		r.run(CmdSplit, nil)
		es = append(es, d, r.ext.activeEntry())
		return r, es
	}
	order := func(r *extRig) string {
		var s []string
		for _, g := range r.ext.groups {
			var ids []string
			for _, e := range g.entries {
				ids = append(ids, string(rune('a'-1+e.term.ID())))
			}
			s = append(s, strings.Join(ids, ""))
		}
		return strings.Join(s, " ")
	}
	tests := []struct {
		name         string
		move, target int // indexes into a, b, c, d, e; -1 target is the end
		want         string
	}{
		{"a tab goes down, after the target", 0, 2, "b c a de"},
		{"a tab goes up, before the target", 2, 0, "c a b de"},
		{"a tab goes to the end", 0, -1, "b c de a"},
		{"a tab of a split goes inside it", 4, 3, "a b c ed"},
		{"a split goes with its tab", 3, 0, "de a b c"},
		{"onto itself nothing moves", 1, 1, "a b c de"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, es := setup(t)
			if got := order(r); got != "a b c de" {
				t.Fatalf("setup: %s", got)
			}
			var target *entry
			if tt.target >= 0 {
				target = es[tt.target]
			}
			r.ext.moveTab(es[tt.move], target)
			if got := order(r); got != tt.want {
				t.Errorf("order = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTheAdoptionOfATerminalThatIsNotThereFailsWithItsOwnRoute(t *testing.T) {
	r := newExtRig(t, "")
	tests := []struct {
		name  string
		state string
		route string
	}{
		{"a state that is not JSON", `{"adopt":`, "createTab.TerminalTabStateInvalid"},
		{"a terminal that is not there", `{"adopt":99}`, "createTab.TerminalAdoptUnknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := r.ext.createTab(&fakeapi.Tab{}, json.RawMessage(tt.state))
			if err == nil {
				t.Fatal("no error")
			}
			routeHas(t, err, tt.route)
		})
	}
}

func TestATabWithoutASessionHasNothingToShowAndCloses(t *testing.T) {
	tests := []struct {
		name  string
		state string
		setup func(r *extRig)
	}{
		{"no state at all", ``, nil},
		{"a session while the sessions are not kept", `{"session":{"profile":"test","cwd":"/","text":"old"}}`, func(r *extRig) { r.api.SetSetting(settingPersistent, false) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newExtRig(t, "")
			if tt.setup != nil {
				tt.setup(r)
			}
			tab := r.api.RestoreTab(EditorKindID, "old", json.RawMessage(tt.state))
			r.api.Pump()
			if !tab.Closed || r.ext.count() != 0 {
				t.Errorf("closed %v, %d terminals", tab.Closed, r.ext.count())
			}
		})
	}
}

func TestEverythingTheStepAddsIsDeclared(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if len(m.Contributes.EditorKinds) != 1 || m.Contributes.EditorKinds[0].ID != EditorKindID {
		t.Errorf("editor kinds = %+v", m.Contributes.EditorKinds)
	}
	declared := map[string]bool{}
	for _, c := range m.Contributes.Commands {
		declared[c.ID] = true
	}
	for _, id := range []string{CmdCreateEditor, CmdMoveToEditor, CmdMoveToPanel, CmdKillEditor, CmdChangeIcon, CmdChangeColor, CmdJoin,
		CmdUnsplit, CmdKillAll, CmdNewWithCwd, CmdSizeToContentWid} {
		if !declared[id] {
			t.Errorf("%s is not declared", id)
		}
	}
	keys := map[string]string{}
	for _, kb := range m.Contributes.Keybindings {
		keys[kb.Key+" "+kb.When] = kb.Command
	}
	if keys["alt+z "+keyFocus] != CmdSizeToContentWid || keys["ctrl+w "+keyFocus+" && "+keyEditorFocus] != CmdKillEditor {
		t.Errorf("the keys of Size to Content Width and Kill Editor: %v", keys)
	}
	settings := map[string]any{}
	for _, s := range m.Contributes.Settings {
		settings[s.Key] = s.Default
	}
	for key, want := range map[string]any{
		settingDefaultLocation: "view", settingPersistent: true, settingReviveProcess: "onExit", settingPersistScrollback: 100.0,
	} {
		if settings[key] != want {
			t.Errorf("%s defaults to %v, want %v", key, settings[key], want)
		}
	}
}

// mkdir makes a folder for a test.
func mkdir(path string) error { return os.MkdirAll(path, 0o755) }
