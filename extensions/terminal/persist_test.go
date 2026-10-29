package terminal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The persistent sessions (step 17d.7): what is saved when Emetos ends and what comes back at the next start.

// savedSession is what the window keeps of an extension: the state of its view and of each of its editor tabs.
type savedSession struct {
	view json.RawMessage
	tabs map[string]json.RawMessage
}

// save asks the extension for the state the way the window does when it ends, then ends the activation.
func (r *extRig) save() savedSession {
	r.t.Helper()
	s := savedSession{view: r.api.View(ViewID).State(), tabs: map[string]json.RawMessage{}}
	for i, tab := range r.api.EditorTabs() {
		if tab.Closed {
			continue
		}
		if st := tab.State(); st != nil {
			s.tabs[string(rune('a'+i))] = st
		}
	}
	r.cancel()
	for _, e := range r.ext.all() {
		e.term.Kill()
		<-e.term.Exited()
		e.term.Wait()
	}
	return s
}

// restart activates the extension in the same folder with a saved session.
func (r *extRig) restart(s savedSession) *extRig {
	r.t.Helper()
	n := newExtRigAt(r.t, r.dir, func(api *fakeapi.API) {
		if s.view != nil {
			api.SetViewState(ViewID, s.view)
		}
	}, "")
	for id, st := range s.tabs {
		n.api.RestoreTab(EditorKindID, id, st)
	}
	n.api.Pump()
	return n
}

func TestTheSessionComesBackWithItsContentAndANewShellInTheSameFolder(t *testing.T) {
	r := newExtRig(t, "")
	sub := filepath.Join(r.dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// a tab of one terminal, a tab of a split of two (the second in a folder) and a terminal in an editor tab
	a := r.newTerminalReady()
	say(t, a, "echo first-terminal", "first-terminal\n$")
	a.term.SetName("alpha")
	r.run(CmdNew, map[string]any{"cwd": sub})
	b := r.ext.activeEntry()
	waitText(t, b.term, "$")
	say(t, b, "echo second-terminal", "second-terminal\n$")
	r.ext.setColor(b, "terminal.ansiRed")
	r.ext.setIcon(b, "bug")
	r.run(CmdSplit, nil)
	c := r.ext.activeEntry()
	waitText(t, c.term, "$")
	say(t, c, "echo third-terminal", "third-terminal\n$")
	r.run(CmdCreateEditor, nil)
	d := r.ext.editors[0]
	waitText(t, d.term, "$")
	say(t, d, "echo in-the-tab", "in-the-tab\n$")
	r.ext.setColor(d, "terminal.ansiGreen")
	oldPids := map[string]int{"a": a.term.Pid(), "b": b.term.Pid(), "c": c.term.Pid(), "d": d.term.Pid()}
	cwdOfB := b.term.SpeculativeCwd()
	if cwdOfB != sub {
		t.Fatalf("the folder of the second terminal is %q, want %q", cwdOfB, sub)
	}

	s := r.save()
	r2 := r.restart(s)

	// the structure: two tabs in the panel, the second a split of two, and one editor tab; the panel is not asked to make any
	if n, g := r2.counts(); n != 4 || g != 2 || r2.ext.panelCount() != 3 || len(r2.ext.editors) != 1 {
		t.Fatalf("%d terminals in %d groups, %d in the panel, %d in editor tabs", n, g, r2.ext.panelCount(), len(r2.ext.editors))
	}
	if len(r2.ext.groups[0].entries) != 1 || len(r2.ext.groups[1].entries) != 2 || r2.ext.cur != 1 {
		t.Errorf("the tabs hold %d and %d terminals, the current is %d", len(r2.ext.groups[0].entries), len(r2.ext.groups[1].entries), r2.ext.cur)
	}
	a2, b2, c2, d2 := r2.ext.groups[0].entries[0], r2.ext.groups[1].entries[0], r2.ext.groups[1].entries[1], r2.ext.editors[0]
	for name, e := range map[string]*entry{"a": a2, "b": b2, "c": c2, "d": d2} {
		if e.term.Pid() == oldPids[name] {
			t.Errorf("%s: the old process came back instead of a new shell", name)
		}
	}
	// the content, the line that says so, and a shell that works
	for name, want := range map[string]struct {
		e    *entry
		text string
	}{"a": {a2, "first-terminal"}, "b": {b2, "second-terminal"}, "c": {c2, "third-terminal"}, "d": {d2, "in-the-tab"}} {
		waitText(t, want.e.term, want.text)
		waitText(t, want.e.term, "History restored")
		waitText(t, want.e.term, "History restored\nready\n$")
		say(t, want.e, "echo new-"+name, "new-"+name+"\n$")
	}
	// the folder, the name, the icon and the colors
	if got := b2.term.SpeculativeCwd(); got != sub {
		t.Errorf("the second terminal is in %q, want %q", got, sub)
	}
	if got := a2.term.Name(); got != "alpha" {
		t.Errorf("the name is %q", got)
	}
	if m := b2.term.Meta(); m.color != "terminal.ansiRed" || m.icon != "bug" {
		t.Errorf("the second terminal has color %q icon %q", m.color, m.icon)
	}
	if tab := d2.tab.(*fakeapi.Tab); tab.IconColor() != "terminal.ansiGreen" {
		t.Errorf("the tab of the terminal in the editor has color %q", tab.IconColor())
	}
	// the new shell found the old lines above the new prompt, not the other way round
	text := allText(a2.term)
	if i, j := strings.Index(text, "first-terminal"), strings.LastIndex(text, "ready"); i < 0 || j < i {
		t.Errorf("the order of the restored lines is wrong:\n%s", text)
	}
}

func TestTheSessionIsKeptOnlyWhereTheSettingsSay(t *testing.T) {
	tests := []struct {
		name      string
		atSave    map[string]any
		atStart   map[string]any
		wantSaved bool
		wantBack  bool
	}{
		{"the defaults", nil, nil, true, true},
		{"onExitAndWindowClose revives as well", map[string]any{settingReviveProcess: "onExitAndWindowClose"}, nil, true, true},
		{"the sessions are off", map[string]any{settingPersistent: false}, nil, false, false},
		{"never revive", map[string]any{settingReviveProcess: "never"}, nil, false, false},
		{"turned off after the save", nil, map[string]any{settingPersistent: false}, true, false},
		{"turned off to never after the save", nil, map[string]any{settingReviveProcess: "never"}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newExtRig(t, "")
			for k, v := range tt.atSave {
				r.api.SetSetting(k, v)
			}
			e := r.newTerminalReady()
			say(t, e, "echo kept", "kept\n$")
			r.run(CmdCreateEditor, nil)
			waitText(t, r.ext.editors[0].term, "$")
			s := r.save()
			if (s.view != nil) != tt.wantSaved || (len(s.tabs) == 1) != tt.wantSaved {
				t.Fatalf("saved view %s and %d tabs, want saved %v", s.view, len(s.tabs), tt.wantSaved)
			}
			n := newExtRigAt(t, r.dir, func(api *fakeapi.API) {
				for k, v := range tt.atStart {
					api.SetSetting(k, v)
				}
				if s.view != nil {
					api.SetViewState(ViewID, s.view)
				}
			}, "")
			for id, st := range s.tabs {
				n.api.RestoreTab(EditorKindID, id, st)
			}
			n.api.Pump()
			if got := n.ext.count() > 0; got != tt.wantBack {
				t.Errorf("%d terminals came back, want them back: %v", n.ext.count(), tt.wantBack)
			}
			if tt.wantBack {
				waitText(t, n.ext.groups[0].entries[0].term, "kept")
			}
		})
	}
}

func TestTheSavedStateIsBoundedAndReadable(t *testing.T) {
	r := newExtRig(t, "")
	e := r.newTerminalReady()
	// more lines than the setting keeps: only the last ones are saved
	e.term.Send([]byte("flood 300\r"))
	waitText(t, e.term, "line 300")
	r.api.SetSetting(settingPersistScrollback, 10)
	var st sessionState
	if err := json.Unmarshal(r.api.View(ViewID).State(), &st); err != nil {
		t.Fatal(err)
	}
	text := st.Groups[0].Terminals[0].Text
	rows, _ := e.term.Emulator().Size()
	if st.Version != sessionVersion || strings.Count(text, "\r\n") >= 10+rows {
		t.Errorf("version %d and %d lines saved", st.Version, strings.Count(text, "\r\n"))
	}
	if strings.Contains(text, "line 100") || !strings.Contains(text, "line 300") {
		t.Errorf("the saved lines are not the last ones: %q", text[:min(len(text), 80)])
	}
	// a state of another version, and a state that is not JSON, are left alone and logged
	for _, raw := range []string{`{"version":99,"groups":[{"terminals":[{"profile":"test","cwd":"/"}]}]}`, `{"version":`} {
		n := newExtRigAt(t, r.dir, func(api *fakeapi.API) { api.SetViewState(ViewID, json.RawMessage(raw)) }, "")
		if n.ext.count() != 0 {
			t.Errorf("a state %q made %d terminals", raw, n.ext.count())
		}
	}
}

func TestATerminalThatCannotStartIsLeftOutOfTheRestoredSession(t *testing.T) {
	r := newExtRig(t, "")
	r.newTerminalReady()
	r.run(CmdNew, nil)
	waitText(t, r.ext.activeEntry().term, "$")
	s := r.save()
	// the second terminal's folder is gone, the program too: it starts in the default folder / the default profile
	var st sessionState
	if err := json.Unmarshal(s.view, &st); err != nil {
		t.Fatal(err)
	}
	st.Groups[1].Terminals[0].Cwd = filepath.Join(r.dir, "gone")
	st.Groups[1].Terminals[0].Profile = "nothing"
	st.Groups[1].Terminals[0].Path = "/does/not/exist"
	raw, _ := json.Marshal(st)
	n := newExtRigAt(t, r.dir, func(api *fakeapi.API) { api.SetViewState(ViewID, raw) }, "")
	if n.ext.count() != 2 {
		t.Fatalf("%d terminals came back", n.ext.count())
	}
	if got := n.ext.groups[1].entries[0].term.InitialDir(); got != r.dir {
		t.Errorf("a folder that is gone: the terminal starts in %q, want %q", got, r.dir)
	}
	if got := n.ext.groups[1].entries[0].term.Profile().name; got != "test" {
		t.Errorf("a profile that is gone: the terminal has %q", got)
	}
}

func TestTheViewRestoresItsStateOnce(t *testing.T) {
	r := newExtRig(t, "")
	r.newTerminalReady()
	s := r.save()
	n := newExtRigAt(t, r.dir, func(api *fakeapi.API) { api.SetViewState(ViewID, s.view) }, "")
	if n.ext.count() != 1 {
		t.Fatalf("%d terminals", n.ext.count())
	}
	// the view asks again whenever its box is made again: the terminals that are there are the truth
	if err := n.api.View(ViewID).Content.RestoreState(s.view); err != nil {
		t.Fatal(err)
	}
	if n.ext.count() != 1 || n.ext.saved != nil {
		t.Errorf("%d terminals after the second restore, saved %q", n.ext.count(), n.ext.saved)
	}
}

func TestARestoredPanelAsksForItsTabOnlyWhenItWasShown(t *testing.T) {
	tests := []struct {
		name       string
		shown      bool
		wantReveal bool
	}{
		{"the panel was showing the terminals", true, true},
		{"the panel was hidden or on another tab", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newExtRig(t, "")
			e := r.newTerminalReady()
			say(t, e, "echo kept", "kept\n$")
			s := r.save()
			var st sessionState
			if err := json.Unmarshal(s.view, &st); err != nil {
				t.Fatal(err)
			}
			if st.Shown {
				t.Fatal("a panel that no widget built is saved as shown")
			}
			st.Shown = tt.shown
			raw, err := json.Marshal(st)
			if err != nil {
				t.Fatal(err)
			}
			n := newExtRigAt(t, r.dir, func(api *fakeapi.API) { api.SetViewState(ViewID, raw) }, "")
			n.api.Pump()
			if got := len(n.api.View(ViewID).Reveals) > 0; got != tt.wantReveal {
				t.Errorf("the view revealed %v, want %v", n.api.View(ViewID).Reveals, tt.wantReveal)
			}
		})
	}
}

func TestASessionThatComesAfterTheActivationIsRestoredWhenItComes(t *testing.T) {
	tests := []struct {
		name     string
		existing bool
		want     int
	}{
		{"the panel has no terminal yet", false, 2},
		{"the panel made a terminal first", true, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newExtRig(t, "")
			r.newTerminalReady()
			r.run(CmdNew, nil)
			waitText(t, r.ext.activeEntry().term, "$")
			s := r.save()
			n := newExtRigAt(t, r.dir, nil, "")
			if tt.existing {
				n.newTerminalReady()
			}
			if err := n.api.View(ViewID).Content.RestoreState(s.view); err != nil {
				t.Fatal(err)
			}
			n.api.Pump()
			if got := n.ext.count(); got != tt.want {
				t.Errorf("%d terminals, want %d", got, tt.want)
			}
		})
	}
}
