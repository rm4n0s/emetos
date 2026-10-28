package terminal

import (
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// bashExt is the extension over the fake API with the real bash as its default profile, shell integration on, in an isolated
// home folder.
func bashExt(t *testing.T) *extRig {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	r := newExtRig(t, bash)
	r.api.Pump()
	return r
}

// waitFor pumps the extension until f is true.
func (r *extRig) waitFor(what string, f func() bool) {
	r.t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !f() {
		if time.Now().After(deadline) {
			r.t.Fatalf("timed out waiting for %s; the terminal shows:\n%s", what, allText(r.ext.activeEntry().term))
		}
		time.Sleep(5 * time.Millisecond)
		r.api.Pump()
	}
}

// open makes a terminal and waits for its first prompt.
func (r *extRig) openBash() *entry {
	r.t.Helper()
	r.run(CmdNew, nil)
	e := r.ext.activeEntry()
	r.waitFor("the first prompt", func() bool { return e.term.si.Current() != nil })
	return e
}

// type runs a line in the terminal and waits until n commands have ended.
func (r *extRig) typeLine(e *entry, line string, n int) {
	r.t.Helper()
	e.term.Send([]byte(line + "\r"))
	r.waitFor("the command", func() bool { return len(e.term.si.Commands()) >= n && e.term.si.Current() != nil })
}

func TestTheScriptsAreInjectedIntoBash(t *testing.T) {
	r := bashExt(t)
	e := r.openBash()
	if args := e.term.Profile().args; len(args) != 2 || args[0] != "--init-file" || !strings.HasSuffix(args[1], "shellIntegration-bash.sh") {
		t.Errorf("args = %q", args)
	}
	r.typeLine(e, "echo hi", 1)
	if got := e.term.si.Commands(); len(got) != 1 || got[0].text != "echo hi" || !got[0].trusted {
		t.Errorf("commands = %+v", got)
	}
	// the key that tells the sequences are there
	if v := r.api.ContextKey(keyShellIntegration); v != true {
		t.Errorf("%s = %v, want true", keyShellIntegration, v)
	}
	// the scripts are in the cache folder of the (isolated) home
	cache := r.ext.cacheDir()
	if _, err := os.Stat(filepath.Join(cache, "shell-integration", scriptsHash(), "shellIntegration-bash.sh")); err != nil {
		t.Errorf("the script is not in %s: %v", cache, err)
	}
}

func TestTheSettingTurnsInjectionOff(t *testing.T) {
	r := bashExt(t)
	r.api.SetSetting(settingShellInteg, false)
	r.ext.settingsChanged()
	r.run(CmdNew, nil)
	e := r.ext.activeEntry()
	if args := e.term.Profile().args; len(args) != 0 {
		t.Errorf("args = %q, want none", args)
	}
	time.Sleep(200 * time.Millisecond)
	if e.term.si.Active() {
		t.Errorf("shell integration is active with the setting off")
	}
}

func TestRunRecentCommand(t *testing.T) {
	r := bashExt(t)
	e := r.openBash()
	r.typeLine(e, "echo one", 1)
	r.typeLine(e, "echo two", 2)
	r.typeLine(e, "echo one", 3)
	r.run(CmdRunRecentCommand, nil)
	pick := r.api.LastPick()
	if pick == nil {
		t.Fatal("no quick pick")
	}
	labels := pick.Labels()
	// newest first, each once: "echo one" is the newest, then "echo two"
	if len(labels) < 2 || labels[0] != "echo one" || labels[1] != "echo two" {
		t.Fatalf("labels = %q", labels)
	}
	pick.Choose(1) // echo two again
	r.waitFor("the command run again", func() bool { return len(e.term.si.Commands()) >= 4 })
	if got := e.term.si.Commands(); got[3].text != "echo two" {
		t.Errorf("the command run is %q", got[3].text)
	}
}

func TestRunRecentCommandUsesThePreviousSessionAndTheHistoryFile(t *testing.T) {
	r := bashExt(t)
	writeFile(t, filepath.Join(r.ext.env.home, ".bash_history"), "from the file\nsecond in file\n")
	e := r.openBash()
	r.typeLine(e, "echo mine", 1)
	// the command is in the history of the extension (written on a goroutine)
	r.waitFor("the history file", func() bool {
		cmds, _, _ := r.ext.hist.snapshot("bash")
		return slices.Contains(cmds, "echo mine")
	})
	r.run(CmdRunRecentCommand, nil)
	labels := r.api.LastPick().Labels()
	want := []string{"echo mine", "second in file", "from the file"}
	if len(labels) < 3 || !slices.Equal(labels[:3], want) {
		t.Fatalf("labels = %q, want %q first", labels, want)
	}
	descs := []string{}
	for _, it := range r.api.LastPick().Options().Items {
		descs = append(descs, it.Description)
	}
	if descs[1] != "~/.bash_history" {
		t.Errorf("descriptions = %q", descs)
	}
	// a second window tab of the same user sees the commands of the first as the previous session
	e2 := New().(*terminals)
	api2 := fakeapi.New(t, e2.Manifest(), r.api.Scope())
	e2.env = r.ext.env
	if err := e2.Activate(t.Context(), api2); err != nil {
		t.Fatal(err)
	}
	cmds, _, err := e2.hist.snapshot("bash")
	if err != nil || !slices.Contains(cmds, "echo mine") {
		t.Errorf("the second window reads %q, %v", cmds, err)
	}
}

func TestGoToRecentDirectory(t *testing.T) {
	r := bashExt(t)
	e := r.openBash()
	r.typeLine(e, "cd /tmp", 1)
	r.typeLine(e, "cd /var", 2)
	r.run(CmdGoToRecentDirectory, nil)
	pick := r.api.LastPick()
	if pick == nil {
		t.Fatal("no quick pick")
	}
	labels := pick.Labels()
	if len(labels) < 3 || labels[0] != "/var" || labels[1] != "/tmp" {
		t.Fatalf("labels = %q, want /var, /tmp, then the first folder", labels)
	}
	pick.Choose(1)
	r.waitFor("the cd", func() bool { return len(e.term.si.Commands()) >= 3 })
	if got := e.term.si.Commands()[2].text; got != "cd '/tmp'" {
		t.Errorf("the command is %q, want cd '/tmp'", got)
	}
	r.waitFor("the folder", func() bool { return e.term.si.Cwd() == "/tmp" })
}

func TestASplitStartsWhereTheShellIs(t *testing.T) {
	r := bashExt(t)
	e := r.openBash()
	r.typeLine(e, "cd /tmp", 1)
	r.run(CmdSplit, nil)
	if r.ext.count() != 2 {
		t.Fatalf("%d terminals", r.ext.count())
	}
	if got := r.ext.activeEntry().term.InitialDir(); got != "/tmp" {
		t.Errorf("the split started in %q, want /tmp (inherited)", got)
	}
	for mode, want := range map[string]string{"initial": e.term.InitialDir(), "workspaceRoot": r.dir} {
		r.api.SetSetting(settingSplitCwd, mode)
		r.ext.settingsChanged()
		r.run(CmdFocus, nil)
		r.ext.focusEntry(e, false)
		r.run(CmdSplit, nil)
		if got := r.ext.activeEntry().term.InitialDir(); got != want {
			t.Errorf("splitCwd %s: the split started in %q, want %q", mode, got, want)
		}
	}
}

func TestASplitOfAShellWithNoIntegrationFollowsItsProcess(t *testing.T) {
	r := newExtRig(t, "/bin/sh")
	r.run(CmdNew, nil)
	e := r.ext.activeEntry()
	e.term.Send([]byte("cd /tmp\r"))
	r.waitFor("the folder of the process", func() bool { return e.term.SpeculativeCwd() == "/tmp" })
	r.run(CmdSplit, nil)
	if got := r.ext.activeEntry().term.InitialDir(); got != "/tmp" {
		t.Errorf("the split started in %q, want /tmp (read from the process)", got)
	}
}

func TestQuickFixOfTheLastCommand(t *testing.T) {
	r := bashExt(t)
	// a program that says what git says when an option needs two dashes
	writeFile(t, filepath.Join(r.dir, "git"), "#!/bin/sh\necho 'error: did you mean `--all` (with two dashes)?' >&2\nexit 1\n")
	if err := os.Chmod(filepath.Join(r.dir, "git"), 0o755); err != nil {
		t.Fatal(err)
	}
	e := r.openBash()
	r.typeLine(e, "./git add . -all", 1)
	r.waitFor("the fix", func() bool { return e.fixes != nil })
	if len(e.fixes.list) != 1 || e.fixes.list[0].command != "./git add . --all" {
		t.Fatalf("fixes = %+v", e.fixes)
	}
	cmds := e.term.si.Commands()
	if e.fixes.start != cmds[0].start {
		t.Errorf("the fixes belong to another command")
	}
	// the menu of that command starts with the fix
	items := e.pv.commandMenu(cmds[0])
	if items[0].Label != "Run: ./git add . --all" {
		t.Errorf("first item = %q", items[0].Label)
	}
	items[0].OnRun()
	r.waitFor("the fix run", func() bool { return len(e.term.si.Commands()) >= 2 })
	if got := e.term.si.Commands()[1].text; got != "./git add . --all" {
		t.Errorf("the fix ran %q", got)
	}
	// the new command (which fails the same way) has fixes of its own, and a command that works takes them away
	r.waitFor("the new fixes", func() bool { return e.fixes != nil && e.fixes.start == e.term.si.Commands()[1].start })
	r.typeLine(e, "true", 3)
	r.waitFor("no fixes", func() bool { return e.fixes == nil })
}

func TestCopyLastCommandAndItsOutput(t *testing.T) {
	r := bashExt(t)
	e := r.openBash()
	r.typeLine(e, "printf 'a\\nb\\n'", 1)
	for _, tc := range []struct {
		command, output bool
		want            string
	}{{true, false, "printf 'a\\nb\\n'"}, {false, true, "a\nb"}, {true, true, "printf 'a\\nb\\n'\na\nb"}} {
		if _, got := r.ext.lastText(tc.command, tc.output); got != tc.want {
			t.Errorf("lastText(%v, %v) = %q, want %q", tc.command, tc.output, got, tc.want)
		}
	}
}

func TestToggleStickyScrollWritesTheSetting(t *testing.T) {
	r := newExtRig(t, "")
	r.run(CmdToggleStickyScroll, nil)
	ups := r.api.SettingUpdates()
	if len(ups) != 1 || ups[0].Key != settingStickyEnabled || ups[0].Value != false {
		t.Errorf("updates = %+v", ups)
	}
}

func TestTheNewCommandsAreDeclaredWithVSCodesKeys(t *testing.T) {
	m := New().Manifest()
	keys := map[string][]string{}
	for _, kb := range m.Contributes.Keybindings {
		keys[kb.Key] = append(keys[kb.Key], kb.Command)
	}
	for key, command := range map[string]string{
		"ctrl+up": CmdScrollToPrevCommand, "ctrl+down": CmdScrollToNextCommand, "ctrl+shift+up": CmdSelectToPrevCommand,
		"ctrl+shift+down": CmdSelectToNextCommand, "ctrl+alt+r": CmdRunRecentCommand, "ctrl+g": CmdGoToRecentDirectory, "ctrl+.": CmdShowQuickFixes,
	} {
		if !slices.Contains(keys[key], command) {
			t.Errorf("the key %s does not run %s: %v", key, command, keys[key])
		}
	}
	titles := map[string]string{}
	for _, c := range m.Contributes.Commands {
		titles[c.ID] = c.Title
	}
	for id, title := range map[string]string{
		CmdRunRecentCommand: "Run Recent Command...", CmdGoToRecentDirectory: "Go to Recent Directory...",
		CmdScrollToPrevCommand: "Scroll to Previous Command", CmdSelectToNextCommand: "Select to Next Command",
		CmdToggleStickyScroll: "Toggle Sticky Scroll", CmdShowQuickFixes: "Show Terminal Quick Fixes",
	} {
		if titles[id] != title {
			t.Errorf("the title of %s is %q, want %q", id, titles[id], title)
		}
	}
	for _, key := range []string{settingShellInteg, settingDecorations, settingQuickFix, settingHistory, settingStickyEnabled, settingStickyMaxLines, settingStickyIgnored, settingSplitCwd} {
		found := false
		for _, s := range m.Contributes.Settings {
			found = found || s.Key == key
		}
		if !found {
			t.Errorf("the setting %s is not declared", key)
		}
	}
}
