//go:build emetos_display

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/internal/testenv/display"
)

// git typed in the terminal as a user meets it (step 24.23, SPEC §10.30 "Passwords and editors" Terminals, §15.1): the real editor, the
// real Terminal extension with a bash, a real git push against git http-backend on 127.0.0.1 behind Basic authentication. git runs the
// askpass script that the Git extension put in the environment of the terminal; the window asks for the password in its box; the test
// types it with the virtual keyboard and the push reaches the remote. With git.terminalAuthentication off the terminal itself asks.

// terminalGitConfig is the configuration of the run: the terminal of terminalConfig and the settings of Git that the test turns.
func terminalGitConfig(gitSettings string) map[string]string {
	return map[string]string{"settings.json": `{
		"terminal.integrated.defaultProfile.linux": "bash",
		"terminal.integrated.profiles.linux": {"bash": {"path": "bash", "args": ["--norc", "--noprofile"]}},
		"terminal.integrated.env.linux": {"PS1": "$ ", "HISTFILE": "/dev/null"}` + gitSettings + `}`}
}

// terminalGitWorld is a remote that wants a password and a clone of it with a commit to push, in the project folder of the editor.
type terminalGitWorld struct {
	e       *gittest.Env
	srv     *gittest.HTTPServer
	project string
	head    string
	host    string
}

func (w *terminalGitWorld) options(gitSettings string) display.Options {
	return display.Options{
		Args:   []string{"."},
		Config: terminalGitConfig(gitSettings),
		Prepare: func(dir string) {
			w.project = dir
			withPassword := strings.Replace(w.host, "http://", "http://ada:"+typedPassword+"@", 1) + "/origin.git"
			w.e.Git(w.e.Work, "clone", "-q", withPassword, dir)
			w.e.Git(dir, "config", "core.hooksPath", w.e.HooksDir)
			w.e.Git(dir, "remote", "set-url", "origin", strings.Replace(w.host, "http://", "http://ada@", 1)+"/origin.git")
			repo := &gittest.Repo{Env: w.e, Dir: dir}
			repo.Write("b.txt", "two\n")
			w.head = repo.Commit("add b")
		},
		Env: []string{"EMETOS_DEV_GIT_TRUST=" + os.TempDir(), "GIT_CONFIG_NOSYSTEM=1"},
	}
}

func newTerminalGitWorld(t *testing.T) *terminalGitWorld {
	t.Helper()
	e := gittest.NewEnv(t)
	src := e.Init("src")
	src.Write("a.txt", "one\n")
	src.Commit("one")
	src.AddRemote("origin")
	src.Git("push", "-q", "origin", "main")
	srv := e.HTTPBackend(e.Work, "ada", typedPassword)
	return &terminalGitWorld{e: e, srv: srv, host: srv.URL}
}

func (w *terminalGitWorld) remoteHead() string {
	return w.e.Git(filepath.Join(w.e.Work, "origin.git"), "rev-parse", "main")
}

func TestRealInputGitPushInTheTerminalAsksTheWindow(t *testing.T) {
	w := newTerminalGitWorld(t)
	a := display.Start(t, w.options(""))
	a.Settle()
	a.Press("ctrl+grave")
	waitTerminals(t, a, 1)
	waitPrompt(t, a, 0)

	prompt := "Password for '" + withUserHost(w.host) + "'"
	a.Type("git push origin main")
	a.Press("Return")
	a.WaitText(prompt)
	a.WaitText("Git: " + w.host)
	in, ok := a.Focused()
	if !ok || in.Text != "" {
		t.Fatalf("the box has no keyboard: %+v (%v)", in, ok)
	}
	// git waits: nothing is pushed, and the terminal did not ask by itself
	if w.remoteHead() == w.head {
		t.Fatal("the push went through before the password was typed")
	}
	if got := termText(a, 0); strings.Contains(got, "Password for") {
		t.Errorf("the terminal asked for the password itself:\n%s", got)
	}
	shot(t, a, "git-terminal-password")
	a.Type(typedPassword)
	a.Press("Enter")
	a.Eventually("the push to reach the remote", func() (bool, string) { return w.remoteHead() == w.head, w.remoteHead() })
	a.WaitNoText(prompt)
	waitTermText(t, a, 0, "main -> main")
	if got := termText(a, 0); strings.Contains(got, typedPassword) {
		t.Errorf("the password is on the screen of the terminal:\n%s", got)
	}
	for _, tx := range a.Texts() {
		if strings.Contains(tx.Text, typedPassword) {
			t.Errorf("the password is drawn on screen as text: %+v", tx)
		}
	}
	if strings.Contains(a.Log(), typedPassword) {
		t.Errorf("the log of the editor has the password:\n%s", a.Log())
	}
	a.Quit()
}

func TestRealInputGitInTheTerminalAsksByItselfWhenTheSettingIsOff(t *testing.T) {
	w := newTerminalGitWorld(t)
	a := display.Start(t, w.options(`, "git.terminalAuthentication": false`))
	a.Settle()
	a.Press("ctrl+grave")
	waitTerminals(t, a, 1)
	waitPrompt(t, a, 0)
	a.Type("git push origin main")
	a.Press("Return")
	// git asks in the terminal, as in any terminal, and the window shows no box
	waitTermText(t, a, 0, "Password for 'http://ada@")
	if _, open := a.Text("Password for '" + withUserHost(w.host) + "'"); open {
		t.Error("the window asked as well")
	}
	if w.remoteHead() == w.head {
		t.Fatal("the push went through without a password")
	}
	a.Press("ctrl+c")
	a.Quit()
}
