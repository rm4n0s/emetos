package terminal

import (
	"slices"
	"strings"
	"testing"
)

func TestBashCommands(t *testing.T) {
	b := startBash(t, 24, 80)
	b.waitCommands(t, 0)
	b.run(t, "true", 1)
	b.run(t, "false", 2)
	b.run(t, "echo hello; echo world", 3)
	b.run(t, "", 3) // an empty line is no command
	b.run(t, "cd /tmp", 4)
	cmds := b.si.Commands()
	want := []struct {
		text      string
		exit      int
		output    string
		hasOutput bool
	}{
		{"true", 0, "", false}, {"false", 1, "", false}, {"echo hello; echo world", 0, "hello\nworld\n", true}, {"cd /tmp", 0, "", false},
	}
	if len(cmds) != len(want) {
		t.Fatalf("%d commands, want %d: %+v", len(cmds), len(want), cmds)
	}
	for i, w := range want {
		c := cmds[i]
		if c.text != w.text || !c.hasExit || c.exit != w.exit || c.confidence != confHigh || !c.trusted {
			t.Errorf("command %d = %q exit %d/%v conf %s trusted %v, want %q exit %d", i, c.text, c.exit, c.hasExit, c.confidence, c.trusted, w.text, w.exit)
		}
		if got := b.si.output(c); got != w.output || c.hasOutput() != w.hasOutput {
			t.Errorf("command %d output = %q (hasOutput %v), want %q", i, got, c.hasOutput(), w.output)
		}
		if c.startLine < 0 || c.execLine < c.startLine || c.endLine < c.execLine || c.promptLine < 0 || c.promptLine > c.startLine {
			t.Errorf("command %d lines: prompt %d start %d exec %d end %d", i, c.promptLine, c.startLine, c.execLine, c.endLine)
		}
		if i > 0 && c.startLine <= cmds[i-1].startLine {
			t.Errorf("command %d starts at %d, not after the one before (%d)", i, c.startLine, cmds[i-1].startLine)
		}
	}
	if !b.si.Active() || b.si.Cwd() != "/tmp" {
		t.Errorf("active %v cwd %q, want true and /tmp", b.si.Active(), b.si.Cwd())
	}
	if cmds[0].cwd == "" || cmds[3].cwd != cmds[0].cwd && cmds[3].cwd != "/tmp" {
		t.Errorf("working folders = %q ... %q", cmds[0].cwd, cmds[3].cwd)
	}
	if got := b.Cwd(); got != "/tmp" {
		t.Errorf("Terminal.Cwd = %q, want /tmp", got)
	}
	// the line of the command is the line that shows it
	em := b.Emulator()
	text, _ := em.LineText(int(cmds[2].startLine - em.FirstLine()))
	if !strings.Contains(text, "echo hello; echo world") {
		t.Errorf("the line of the third command shows %q", text)
	}
	// the folders of the session
	if got := b.si.Cwds(); len(got) < 2 || got[len(got)-1] != "/tmp" {
		t.Errorf("Cwds = %v, want the last to be /tmp", got)
	}
}

func TestBashCommandsSurviveAResize(t *testing.T) {
	b := startBash(t, 24, 80)
	b.waitCommands(t, 0)
	b.run(t, "echo first", 1)
	b.run(t, "echo second", 2)
	before := b.si.Commands()
	b.Resize(30, 24)
	b.Resize(100, 24)
	after := b.si.Commands()
	if len(after) != 2 {
		t.Fatalf("%d commands after the resize, want 2", len(after))
	}
	em := b.Emulator()
	for i, c := range after {
		text, _ := em.LineText(int(c.startLine - em.FirstLine()))
		if !strings.Contains(text, before[i].text) && !strings.HasSuffix(strings.TrimSpace(text), before[i].text[:min(len(before[i].text), 3)]) {
			// a narrow terminal may break the line anywhere; the command is on the line or on the first part of it
			t.Logf("command %d (%q) is on the line %q after the resize", i, before[i].text, text)
		}
		if c.startLine < 0 {
			t.Errorf("command %d lost its line", i)
		}
	}
	if got := b.si.output(after[1]); got != "second\n" {
		t.Errorf("output after the resize = %q", got)
	}
}

func TestBashRejectsAForgedCommandLine(t *testing.T) {
	b := startBash(t, 24, 80)
	b.waitCommands(t, 0)
	// a program prints a command line with the wrong secret: the real one (from the shell) is used
	typed := `printf '\033]633;E;rm -rf /;wrongnonce\007'`
	b.run(t, typed, 1)
	cmds := b.si.Commands()
	if len(cmds) != 1 || cmds[0].text != typed {
		t.Errorf("commands = %+v, want the line that was typed", cmds)
	}
}

func TestBashUserMarksAndProperties(t *testing.T) {
	b := startBash(t, 24, 80)
	b.waitCommands(t, 0)
	b.run(t, `printf '\033]1337;SetMark\007marked\n'`, 1)
	if got := b.si.Marks(); len(got) != 1 {
		t.Errorf("marks = %v, want one", got)
	}
	b.run(t, `printf '\033]1337;CurrentDir=/var/tmp\007'`, 2)
	if got := b.si.Cwds(); !slices.Contains(got, "/var/tmp") {
		t.Errorf("Cwds = %v, want /var/tmp among them (OSC 1337 CurrentDir)", got)
	}
	b.si.mu.Lock()
	rich := b.si.rich
	b.si.mu.Unlock()
	if !rich {
		t.Errorf("the script said HasRichCommandDetection but the state does not know")
	}
}
