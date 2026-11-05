package terminal

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// The terminal's window tab (SPEC §10.25 "A terminal as a window tab", step 28.1): one terminal with no workspace of its
// own, as a tab beside the workspaces.

// wtRig is the extension activated in the scope of a window tab, over the fake API, with the test shell as its default
// profile.
type wtRig struct {
	t      *testing.T
	api    *fakeapi.API
	ext    *terminals
	wt     *fakeapi.WindowTab
	home   string
	cancel context.CancelFunc
}

// newWtRig activates the extension in the scope of a window tab and starts the tab with no saved state.
func newWtRig(t *testing.T, profile string, args ...string) *wtRig {
	t.Helper()
	return newWtRigAt(t, nil, nil, profile, args...)
}

// newWtRigAt is newWtRig with a saved state to revive (nil for none) and a chance to set up the fake API first.
func newWtRigAt(t *testing.T, before func(*fakeapi.API), state json.RawMessage, profile string, args ...string) *wtRig {
	t.Helper()
	if profile == "" {
		profile = testShell(t)
	}
	home := t.TempDir()
	e := New().(*terminals)
	id := ID + ":t1"
	api := fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWindowTab, Name: "Terminal", WindowTab: WindowTabKindID, ID: id})
	api.SetSetting(settingProfiles, map[string]any{"test": map[string]any{"path": profile, "args": args}})
	api.SetSetting(settingDefaultProfile, "test")
	if before != nil {
		before(api)
	}
	e.env = environ{vars: isolatedEnv(t), lookPath: exec.LookPath, home: home}
	ctx, cancel := context.WithCancel(context.Background())
	if err := e.Activate(ctx, api); err != nil {
		t.Fatal(err)
	}
	wt := api.StartWindowTab(id, state)
	r := &wtRig{t: t, api: api, ext: e, wt: wt, home: home, cancel: cancel}
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
		e.histWrites.Wait()
	})
	return r
}

// entry is the one terminal of the tab, nil once it ended.
func (r *wtRig) entry() *entry {
	if len(r.ext.groups) == 0 || len(r.ext.groups[0].entries) == 0 {
		return nil
	}
	return r.ext.groups[0].entries[0]
}

// ready waits for the shell's prompt and returns the terminal.
func (r *wtRig) ready() *entry {
	r.t.Helper()
	e := r.entry()
	if e == nil {
		r.t.Fatal("the window tab has no terminal")
	}
	waitText(r.t, e.term, "$")
	return e
}

func TestTheWindowTabKindIsDeclared(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if len(m.Contributes.WindowTabKinds) != 1 {
		t.Fatalf("window tab kinds = %+v", m.Contributes.WindowTabKinds)
	}
	k := m.Contributes.WindowTabKinds[0]
	if k.ID != WindowTabKindID || k.Title != "Terminal" || k.Icon != "terminal" || k.PlusMenuLabel != "" {
		t.Errorf("the kind = %+v", k)
	}
}

// TestOpeningTheWindowTabStartsARunningShell is the "+" menu's Terminal entry (dev.openWindowTab {"kind":
// "emetos.terminal.tab"}): a tab with one terminal and a running shell, where typing and output work.
func TestOpeningTheWindowTabStartsARunningShell(t *testing.T) {
	r := newWtRig(t, "")
	e := r.ready()
	if n := r.ext.count(); n != 1 {
		t.Fatalf("%d terminals, want 1", n)
	}
	say(t, e, "echo hello-tab", "hello-tab\n$")
	if r.api.ContextKey(keyCount) != float64(1) || r.api.ContextKey(keyIsOpen) != true {
		t.Errorf("context keys: count %v, open %v", r.api.ContextKey(keyCount), r.api.ContextKey(keyIsOpen))
	}
}

// TestTheWindowTabsTerminalStartsInTheHomeFolder is SPEC §10.25 "A terminal as a window tab": there is no workspace folder,
// so terminal.integrated.cwd is used only when it is absolute, and otherwise the home folder is used, exactly as a No
// Folder workspace tab's terminal already falls back (profile.go's startDir).
func TestTheWindowTabsTerminalStartsInTheHomeFolder(t *testing.T) {
	r := newWtRig(t, "")
	e := r.ready()
	if got := e.term.SpeculativeCwd(); got != r.home {
		t.Errorf("the terminal started in %q, want the home folder %q", got, r.home)
	}
	if got := e.pv.folderOf(); got != "" {
		t.Errorf("the folder links are confined to = %q, want none", got)
	}
}

// TestTheWindowTabsTerminalUsesAnAbsoluteCwdSetting is the other half of the same fallback: an absolute
// terminal.integrated.cwd is used even with no workspace folder.
func TestTheWindowTabsTerminalUsesAnAbsoluteCwdSetting(t *testing.T) {
	dir := t.TempDir()
	r := newWtRigAt(t, func(api *fakeapi.API) { api.SetSetting(settingCwd, dir) }, nil, "")
	e := r.ready()
	if got := e.term.SpeculativeCwd(); got != dir {
		t.Errorf("the terminal started in %q, want %q", got, dir)
	}
}

// TestTheWindowTabsTerminalHasNoFileLinkEvenUnderAFixtureFolder checks that a real, existing folder passed to the rig as if
// it were a fixture's absolute path gives no file link: the window tab's terminal has no workspace folder to confine links
// to (SPEC §10.25 "A terminal as a window tab": "it has no file links"), whatever files exist on disk. Web links and OSC 8
// hyperlinks are unaffected by this (they go through openLink, not the file link detector that needs a folder).
func TestTheWindowTabsTerminalHasNoFileLinkEvenUnderAFixtureFolder(t *testing.T) {
	r := newWtRig(t, "")
	e := r.ready()
	fixture := filepath.Join(r.home, "project")
	if err := os.MkdirAll(fixture, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	env := e.pv.linkEnvironment()
	if env.stat(filepath.Join(fixture, "main.go")) {
		t.Errorf("a file under %q is treated as a link with no workspace folder", fixture)
	}
}

func TestTheWindowTabsTerminalIsDroppedFromTheRequestsAndEvents(t *testing.T) {
	r := newWtRig(t, "")
	r.ready()
	for _, topic := range []string{TopicList, TopicSnapshot, TopicCreate, TopicWrite, TopicRun, TopicKill} {
		_, err := r.api.AskAs(context.Background(), "test", topic, map[string]any{})
		appErr, ok := errors.FromError(err)
		if !ok || appErr.Tag != "EventNoHandler" {
			t.Errorf("%s: want no handler in the window tab's scope, got %v", topic, err)
		}
	}
	// publishing an event of the terminal (created, output...) is suppressed too (noEvents)
	published := false
	if _, err := r.api.Events().Subscribe("emetos.terminal.*", func(extension.Event) { published = true }); err != nil {
		t.Fatal(err)
	}
	say(t, r.entry(), "echo again", "again\n$")
	if published {
		t.Error("the window tab's terminal published an event of emetos.terminal.*")
	}
}

func TestClosingTheWindowTabEndsItsTerminalWithNoQuestion(t *testing.T) {
	r := newWtRig(t, "")
	e := r.ready()
	term := e.term
	r.wt.Content.OnClose()
	select {
	case <-term.Exited():
	case <-time.After(5 * time.Second):
		t.Fatal("the process did not end when the tab closed")
	}
	term.Wait()
	if !r.ext.alive(e) {
		// already removed: fine, nothing else to check
		return
	}
	t.Error("the terminal is still tracked after the tab closed")
}

// TestKillingTheOnlyTerminalClosesTheWindowTab checks the other direction: ending the one terminal (Kill, or the process
// exiting by itself) closes the tab, since there is nothing left to show in it, the same way the last terminal gone hides
// the panel and an editor tab's terminal ending closes its tab.
func TestKillingTheOnlyTerminalClosesTheWindowTab(t *testing.T) {
	r := newWtRig(t, "")
	r.ready()
	if r.wt.Closes != 0 {
		t.Fatalf("the tab closed before the terminal was killed: %d", r.wt.Closes)
	}
	r.ext.kill(r.entry())
	if r.wt.Closes != 1 {
		t.Errorf("the tab closed %d times, want 1", r.wt.Closes)
	}
}

func TestTheWindowTabsSessionPersistsWithTheSetting(t *testing.T) {
	tests := []struct {
		name       string
		persistent bool
		wantSaved  bool
	}{
		{"on by default", true, true},
		{"off", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newWtRigAt(t, func(api *fakeapi.API) { api.SetSetting(settingPersistent, tt.persistent) }, nil, "")
			e := r.ready()
			say(t, e, "echo saved-text", "saved-text\n$")
			state := r.wt.State()
			if (state != nil) != tt.wantSaved {
				t.Fatalf("saved state = %v, want saved %v", state != nil, tt.wantSaved)
			}
			if !tt.wantSaved {
				return
			}
			r.cancel()
			select {
			case <-e.term.Exited():
			case <-time.After(5 * time.Second):
				t.Fatal("the terminal did not end with the activation")
			}
			e.term.Wait()

			r2 := newWtRigAt(t, nil, state, "")
			e2 := r2.ready()
			if e2.term.Pid() == e.term.Pid() {
				t.Error("the old process came back instead of a new shell")
			}
			waitText(t, e2.term, "saved-text")
			waitText(t, e2.term, "History restored")
			say(t, e2, "echo again", "again\n$")
		})
	}
}

// TestASecondTerminalEntryOpensASecondIndependentWindowTab is "one terminal per window tab": a second activation (as the
// "+" menu's Terminal entry chosen again makes) is a wholly separate instance of the extension, in its own scope, with its
// own terminal; killing one does not touch the other.
func TestASecondTerminalEntryOpensASecondIndependentWindowTab(t *testing.T) {
	r1 := newWtRig(t, "")
	r2 := newWtRig(t, "")
	e1, e2 := r1.ready(), r2.ready()
	if e1.term.Pid() == e2.term.Pid() {
		t.Fatal("the two window tabs share a process")
	}
	say(t, e1, "echo one", "one\n$")
	say(t, e2, "echo two", "two\n$")
	r1.ext.kill(r1.entry())
	if r1.wt.Closes != 1 {
		t.Errorf("the first tab closed %d times, want 1", r1.wt.Closes)
	}
	if r2.wt.Closes != 0 {
		t.Errorf("the second tab closed, it should not have: %d", r2.wt.Closes)
	}
	if _, exited := e2.term.ExitCode(); exited {
		t.Error("the second terminal ended when the first was killed")
	}
}

func TestAWindowTabWithAStateThatIsNotJSONFailsWithItsOwnRoute(t *testing.T) {
	e := New().(*terminals)
	api := fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWindowTab, Name: "Terminal", WindowTab: WindowTabKindID, ID: ID + ":t1"})
	e.env = environ{vars: isolatedEnv(t), lookPath: exec.LookPath, home: t.TempDir()}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := e.Activate(ctx, api); err != nil {
		t.Fatal(err)
	}
	_, err := e.createWindowTab(&fakeapi.WindowTab{}, json.RawMessage(`{"session":`))
	if err == nil {
		t.Fatal("no error")
	}
	routeHas(t, err, "createWindowTab.TerminalWindowTabStateInvalid")
}

func TestEverythingTheWindowTabStepAddsIsDeclared(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if len(m.Contributes.WindowTabKinds) != 1 || m.Contributes.WindowTabKinds[0].ID != WindowTabKindID {
		t.Errorf("window tab kinds = %+v", m.Contributes.WindowTabKinds)
	}
	e := New().(*terminals)
	home := t.TempDir()
	id := ID + ":t1"
	api := fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWindowTab, Name: "Terminal", WindowTab: WindowTabKindID, ID: id})
	api.SetSetting(settingProfiles, map[string]any{"test": map[string]any{"path": testShell(t)}})
	api.SetSetting(settingDefaultProfile, "test")
	e.env = environ{vars: isolatedEnv(t), lookPath: exec.LookPath, home: home}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := e.Activate(ctx, api); err != nil {
		t.Fatal(err)
	}
	for _, id := range windowTabExcluded {
		if api.HasHandler(id) {
			t.Errorf("%s is handled in the window tab's scope, want it excluded (no split, no editor area, no second terminal)", id)
		}
	}
	if !api.HasHandler(CmdKill) || !api.HasHandler(CmdCopySelection) {
		t.Error("the commands of the one terminal are not registered in the window tab's scope")
	}
}
