package extensions

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm"
	"github.com/rm4n0s/emetos/extensions/gitscm/gittest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
	"github.com/rm4n0s/emetos/extensions/terminal"
)

// git typed in a terminal (step 24.23, SPEC §10.30 "Passwords and editors" Terminals): the real Terminal extension with a real shell in a
// pseudo-terminal, the real Git extension and the real helper (this test binary answers the flags, testmain_test.go), the real git against
// git http-backend on 127.0.0.1. The two extensions are two fakes of the API that share the registry of terminal variables, as the two
// extensions of a window tab share it in the host. The process is given the isolated environment of the world for the test, so the shell
// sees nothing of the user's. (They live in the package extensions because the tests of one extension may not import another: internal/archtest.)

const typedPW = "Tr0ub4dor&3-typed-in-the-box"

// termGit is the Git extension and the Terminal extension over two fakes that share the variables.
type termGit struct {
	t    *testing.T
	env  *gittest.Env
	git  *fakeapi.API
	term *fakeapi.API
	srv  *gittest.HTTPServer
	// work is the clone the shell runs in, bare the remote it has.
	work *gittest.Repo
	bare string
}

// isolateProcess gives the process the environment of the world of gittest for the length of the test (the Terminal extension starts its
// shells with the environment of the process), and takes out what would answer for the window: the askpass program of the world, the
// session's ssh agent and display, and the Git helper of an Emetos whose terminal the test runs in.
func isolateProcess(t *testing.T, e *gittest.Env) {
	t.Helper()
	for _, kv := range e.Vars() {
		// not TMPDIR (the runtime folder of the server is made in the one of the process: a socket path is limited to about 100 bytes) and
		// not XDG_RUNTIME_DIR (the server is given a folder of its own)
		if k, v, ok := strings.Cut(kv, "="); ok && k != "TMPDIR" && k != "XDG_RUNTIME_DIR" {
			t.Setenv(k, v)
		}
	}
	for _, k := range []string{"GIT_ASKPASS", "SSH_ASKPASS", "SSH_ASKPASS_REQUIRE", "SSH_AUTH_SOCK", "DISPLAY", "WAYLAND_DISPLAY", "GIT_TERMINAL_PROMPT",
		"GIT_EDITOR", "GIT_SEQUENCE_EDITOR", "ENV", "BASH_ENV",
		// a test run in a terminal of an Emetos has that window's helper in its environment
		"EMETOS_GIT_SOCKET", "EMETOS_GIT_TOKEN", "EMETOS_TERMINAL"} {
		t.Setenv(k, "")
		if err := os.Unsetenv(k); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PS1", "$ ")
}

// newTermGit makes a remote that wants a password, a clone of it as the folder of the window tab, and the two extensions in it.
func newTermGit(t *testing.T, gitSettings map[string]any) *termGit {
	t.Helper()
	e := gittest.NewEnv(t)
	src := e.Init("src")
	src.Write("a.txt", "one\n")
	src.Commit("one")
	src.AddRemote("origin")
	src.Git("push", "-q", "origin", "main")
	srv := e.HTTPBackend(e.Work, "ada", typedPW)
	dir := filepath.Join(e.Work, "clone")
	e.Git(e.Work, "clone", "-q", strings.Replace(srv.URL, "http://", "http://ada:"+typedPW+"@", 1)+"/origin.git", dir)
	e.Git(dir, "config", "core.hooksPath", e.HooksDir)
	e.Git(dir, "remote", "set-url", "origin", strings.Replace(srv.URL, "http://", "http://ada@", 1)+"/origin.git")
	e.Trust(dir)
	isolateProcess(t, e)

	state := filepath.Join(e.StateHome, "emetos")
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "state.json"), []byte(`{"autofetchInformed":true}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runtime, err := os.MkdirTemp("", "gr") // short: a Unix socket path is limited to about 100 bytes
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(runtime) })
	gx := gitscm.NewWith(gitscm.Options{StateDir: state, Home: e.Home, Env: e.Vars(), HelperBinary: os.Args[0], RuntimeDir: runtime})
	gapi := fakeapi.New(t, gx.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "clone", Folder: dir, ID: dir})
	gapi.SetSetting("git.path", e.GitPath)
	for k, v := range gitSettings {
		gapi.SetSetting(k, v)
	}
	tx := terminal.New()
	tapi := fakeapi.New(t, tx.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "clone", Folder: dir, ID: dir})
	tapi.SetSetting("terminal.integrated.profiles.linux", map[string]any{"sh": map[string]any{"path": "/bin/sh"}})
	tapi.SetSetting("terminal.integrated.defaultProfile.linux", "sh")
	shared := fakeapi.NewTerminalSources()
	gapi.ShareTerminalEnvironment(shared)
	tapi.ShareTerminalEnvironment(shared)
	memory := fakeapi.NewOriginMemory() // who typed in which terminal
	gapi.ShareOrigins(memory)
	tapi.ShareOrigins(memory)
	ctx, cancel := context.WithCancel(context.Background())
	for _, ext := range []struct {
		x extension.Extension
		a *fakeapi.API
	}{{gx, gapi}, {tx, tapi}} {
		if err := ext.x.Activate(ctx, ext.a); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		_ = tapi.Run("workbench.action.terminal.killAll", nil)
		tapi.Pump()
		cancel()
		time.Sleep(50 * time.Millisecond)
	})
	return &termGit{t: t, env: e, git: gapi, term: tapi, srv: srv, work: &gittest.Repo{Env: e, Dir: dir}, bare: filepath.Join(e.Work, "origin.git")}
}

// pump runs the UI thread of both extensions.
func (g *termGit) pump() {
	g.term.Pump()
	g.git.Pump()
}

// waitFor pumps until cond holds.
func (g *termGit) waitFor(what string, cond func() bool) {
	g.t.Helper()
	g.git.WaitFor(what, func() bool { g.pump(); return cond() })
}

// open opens a terminal.
func (g *termGit) open() {
	g.t.Helper()
	if err := g.term.Run("workbench.action.terminal.new", nil); err != nil {
		g.t.Fatal(err)
	}
	g.term.Pump()
}

// typeLine types a line into the active terminal.
func (g *termGit) typeLine(line string) {
	g.t.Helper()
	g.send(line + "\r")
}

func (g *termGit) send(text string) {
	g.t.Helper()
	if err := g.term.Run("workbench.action.terminal.sendSequence", map[string]any{"text": text}); err != nil {
		g.t.Fatal(err)
	}
	g.term.Pump()
}

// nextInput waits for the n-th input box of the Git extension.
func (g *termGit) nextInput(n int) *fakeapi.Input {
	g.t.Helper()
	var in *fakeapi.Input
	g.waitFor("an input box", func() bool {
		if list := g.git.Inputs(); len(list) > n {
			in = list[n]
			return true
		}
		return false
	})
	return in
}

// shellValue asks the shell for a variable: the shell writes it to a file, which the test reads.
func (g *termGit) shellValue(name string) string {
	g.t.Helper()
	out := filepath.Join(g.t.TempDir(), "value")
	line := "printenv " + name + " > " + out + "; echo END >> " + out
	g.typeLine(line)
	retry := time.Now()
	var data []byte
	g.waitFor("the shell to answer", func() bool {
		var err error
		data, err = os.ReadFile(out)
		if err == nil && strings.Contains(string(data), "END\n") {
			return true
		}
		if time.Since(retry) > 500*time.Millisecond { // a shell that was still starting may have dropped the line: the line writes the file anew
			g.typeLine(line)
			retry = time.Now()
		}
		return false
	})
	return strings.TrimSuffix(strings.TrimSuffix(string(data), "END\n"), "\n")
}

func TestGitPushTypedInATerminalAsksTheWindowAndTheAnswerReachesGit(t *testing.T) {
	g := newTermGit(t, nil)
	g.open()
	g.work.Write("b.txt", "two\n")
	head := g.work.Commit("add b")
	g.typeLine("git push -q origin main")
	in := g.nextInput(0)
	o := in.Options()
	if !strings.HasPrefix(o.Prompt, "Password for '") || !strings.Contains(o.Prompt, "ada@") || o.Message != "Git: "+g.srv.URL || !o.Password || !o.IgnoreFocusOut {
		t.Errorf("the box is %+v", o)
	}
	// git is waiting for the answer: the remote has not got the commit
	if got := g.env.Git(g.bare, "rev-parse", "main"); got == head {
		t.Fatal("the push went through before the password was typed")
	}
	in.Accept(typedPW)
	g.waitFor("the push to arrive", func() bool { return g.env.Git(g.bare, "rev-parse", "main") == head })
	if n := len(g.git.Inputs()); n != 1 {
		t.Errorf("%d boxes were shown, want 1", n)
	}
	for _, req := range g.srv.Requests() {
		if strings.Contains(req, typedPW) {
			t.Errorf("the server logged the password: %s", req)
		}
	}
	for _, n := range g.git.Notes() {
		if strings.Contains(n.Message, typedPW) {
			t.Errorf("the password is in a notification: %s", n.Message)
		}
	}
}

func TestEscapeInTheBoxDoesNotPushAndGitAsksInTheTerminalItself(t *testing.T) {
	g := newTermGit(t, nil)
	g.open()
	g.work.Write("c.txt", "three\n")
	head := g.work.Commit("add c")
	g.typeLine("git push -q origin main")
	g.nextInput(0).Cancel()
	// the askpass program failed, so git asks the terminal itself, as it does in any terminal (the person at the keyboard can type
	// the password there): it is waiting there, and nothing was pushed. Ctrl+C ends it and the shell is back.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		g.pump()
		time.Sleep(10 * time.Millisecond)
	}
	if got := g.env.Git(g.bare, "rev-parse", "main"); got == head {
		t.Error("the commit was pushed after the box was cancelled")
	}
	if n := len(g.git.Inputs()); n != 1 {
		t.Errorf("%d boxes were shown, want 1", n)
	}
	g.send("\x03")
	marker := filepath.Join(t.TempDir(), "alive")
	// git puts the terminal back with a flush of what was typed meanwhile: the line is typed again until the shell answers
	retry := time.Now()
	g.waitFor("the shell to come back", func() bool {
		if _, err := os.Stat(marker); err == nil {
			return true
		}
		if time.Since(retry) > 300*time.Millisecond {
			g.typeLine("echo alive > " + marker)
			retry = time.Now()
		}
		return false
	})
}

func TestATerminalGetsTheVariablesOfTheSettingsAndNoMoreThanThat(t *testing.T) {
	tests := []struct {
		name            string
		settings        map[string]any
		askpass, editor bool
	}{
		{name: "the defaults", askpass: true},
		{name: "terminalAuthentication off", settings: map[string]any{"git.terminalAuthentication": false}},
		{name: "terminalGitEditor on", settings: map[string]any{"git.terminalGitEditor": true}, askpass: true, editor: true},
		{name: "the editor alone", settings: map[string]any{"git.terminalGitEditor": true, "git.terminalAuthentication": false}, editor: true},
		{name: "useIntegratedAskPass off", settings: map[string]any{"git.useIntegratedAskPass": false, "git.terminalGitEditor": true}, editor: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g := newTermGit(t, tc.settings)
			g.open()
			if got := g.shellValue("GIT_ASKPASS"); (got != "") != tc.askpass {
				t.Errorf("GIT_ASKPASS = %q, want set: %v", got, tc.askpass)
			}
			if got := g.shellValue("GIT_EDITOR"); (got != "") != tc.editor {
				t.Errorf("GIT_EDITOR = %q, want set: %v", got, tc.editor)
			}
			for _, name := range []string{"SSH_ASKPASS", "SSH_ASKPASS_REQUIRE", "GIT_SEQUENCE_EDITOR", "GIT_TERMINAL_PROMPT"} {
				if got := g.shellValue(name); got != "" {
					t.Errorf("%s = %q in the terminal", name, got)
				}
			}
			socket := g.shellValue("EMETOS_GIT_SOCKET")
			if (socket != "") != (tc.askpass || tc.editor) {
				t.Errorf("EMETOS_GIT_SOCKET = %q", socket)
			}
			if got := g.shellValue("EMETOS_GIT_TOKEN"); (got != "") != (socket != "") {
				t.Errorf("the token is %q and the socket %q", got, socket)
			}
		})
	}
}

func TestATerminalThatRanBeforeTheSettingWasOnKeepsItsEnvironment(t *testing.T) {
	g := newTermGit(t, map[string]any{"git.terminalAuthentication": false})
	g.open()
	if got := g.shellValue("GIT_ASKPASS"); got != "" {
		t.Fatalf("GIT_ASKPASS = %q before", got)
	}
	g.git.SetSetting("git.terminalAuthentication", true)
	g.pump()
	if got := g.shellValue("GIT_ASKPASS"); got != "" {
		t.Errorf("a running terminal got GIT_ASKPASS = %q", got)
	}
	g.open()
	if got := g.shellValue("GIT_ASKPASS"); got == "" {
		t.Error("a new terminal did not get GIT_ASKPASS")
	}
	g.git.Deactivate()
	g.open()
	if got := g.shellValue("GIT_ASKPASS"); got != "" {
		t.Errorf("a terminal started after the extension was turned off got GIT_ASKPASS = %q", got)
	}
}

// A client types git push in the host's terminal from its own view (step 24.25, SPEC §10.25 "Clients of a shared workspace", D51): the
// password box is shown in the view of the client who typed, not in the host's window, and the answer reaches the git in the host's shell.
func TestAGitPushTypedByAClientAsksInTheClientsView(t *testing.T) {
	const view extension.Origin = 3
	g := newTermGit(t, nil)
	g.open()
	g.work.Write("d.txt", "four\n")
	head := g.work.Commit("add d")
	// the client's keys reach the terminal through a pane of its view; the command of the test types as that pane would
	typeAs := func(o extension.Origin, line string) {
		g.t.Helper()
		g.term.RunAs(o, func() { g.send(line + "\r") })
	}
	typeAs(view, "git push -q origin main")
	in := g.nextInput(0)
	if o := in.Options(); o.Origin != view || !o.Password {
		t.Fatalf("the box is %+v, want a password box in the view %d", o, view)
	}
	if got := g.env.Git(g.bare, "rev-parse", "main"); got == head {
		t.Fatal("the push went through before the password was typed")
	}
	in.Accept(typedPW)
	g.waitFor("the push to arrive", func() bool { return g.env.Git(g.bare, "rev-parse", "main") == head })

	// the host types the next one: its own window asks
	g.work.Write("e.txt", "five\n")
	head = g.work.Commit("add e")
	typeAs(extension.HostOrigin, "git push -q origin main")
	in = g.nextInput(1)
	if o := in.Options(); o.Origin != extension.HostOrigin {
		t.Errorf("the box is %+v, want one of the host's window", o)
	}
	in.Accept(typedPW)
	g.waitFor("the second push to arrive", func() bool { return g.env.Git(g.bare, "rev-parse", "main") == head })
}
