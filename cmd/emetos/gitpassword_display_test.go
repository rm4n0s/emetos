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

// The password git asks for, as a user meets it (step 24.9, SPEC §10.30 "Passwords and editors", §15.1): a real git fetch against git
// http-backend on 127.0.0.1 behind Basic authentication, started by the editor itself; git runs the askpass script, which starts the real
// binary with --git-askpass, which asks the window over the socket in its private folder; the test types the password with the virtual
// keyboard. The remote, the clone and the credentials live in the temporary folders of the test; the editor runs with an environment of
// its own that has no ssh-agent, no display and no askpass program of the user.

const typedPassword = "Tr0ub4dor&3-typed-by-the-keys"

func TestRealInputGitPassword(t *testing.T) {
	e := gittest.NewEnv(t)
	src := e.Init("src")
	src.Write("a.txt", "one\n")
	src.Commit("one")
	src.AddRemote("origin")
	src.Git("push", "-q", "origin", "main")
	srv := e.HTTPBackend(e.Work, "ada", typedPassword)
	host := srv.URL
	withUser := strings.Replace(host, "http://", "http://ada@", 1) + "/origin.git"
	withPassword := strings.Replace(host, "http://", "http://ada:"+typedPassword+"@", 1) + "/origin.git"

	prompt := "Password for '" + withUserHost(host) + "'" // the placeholder of the box, which git wrote
	var project string
	a := display.Start(t, display.Options{
		Args: []string{"."},
		Prepare: func(dir string) {
			project = dir
			e.Git(e.Work, "clone", "-q", withPassword, dir)
			e.Git(dir, "config", "core.hooksPath", e.HooksDir)
			e.Git(dir, "remote", "set-url", "origin", withUser)
		},
		// the repository of the project is the one the test made: trusted for the session (SPEC §15.2); no system configuration
		Env: []string{"EMETOS_DEV_GIT_TRUST=" + os.TempDir(), "GIT_CONFIG_NOSYSTEM=1"},
	})
	a.Settle()
	published := func(name string) string {
		src.Write(name, name+"\n")
		head := src.Commit("add " + name)
		src.Git("push", "-q", "origin", "main")
		return head
	}
	fetch := func() {
		a.Press("ctrl+shift+p")
		a.Type("Git: Fetch")
		a.WaitText("Git: Fetch (Prune)") // all three rows are listed, the best match first
		a.Press("Enter")
	}
	remoteHead := func() string { return e.Git(project, "rev-parse", "origin/main") }

	// the password is typed in the box and git goes on
	head := published("two.txt")
	fetch()
	a.WaitText(prompt)
	in, ok := a.Focused()
	if !ok || in.Text != "" {
		t.Fatalf("the box has no keyboard: %+v (%v)", in, ok)
	}
	// the box is not closed by a click elsewhere: the program waits for the answer
	if tx, ok := a.Text(prompt); ok {
		a.Click(tx.X, tx.Y+300)
	}
	a.Settle()
	a.WaitText(prompt)
	// the click took the keyboard away, as it does in VS Code; a click on the box gives it back
	a.ClickText(prompt)
	a.Type(typedPassword)
	a.Eventually("the password to be in the box", func() (bool, string) {
		in, ok := a.Focused()
		return ok && in.Text == typedPassword, a.FocusedText()
	})
	for _, tx := range a.Texts() {
		if strings.Contains(tx.Text, typedPassword) {
			t.Errorf("the password is drawn on screen as text: %+v", tx)
		}
	}
	a.Press("Enter")
	a.Eventually("origin/main to move", func() (bool, string) { return remoteHead() == head, remoteHead() })
	a.WaitNoText(prompt)

	// Escape in the box fails the fetch, and the notification offers Retry
	published("three.txt")
	fetch()
	a.WaitText(prompt)
	a.Press("Escape")
	a.WaitText("Retry")
	a.WaitText("Failed to authenticate to git remote: " + host)
	if remoteHead() != head {
		t.Error("the fetch went through without a password")
	}

	// Retry asks again; a wrong password fails the same way
	a.ClickText("Retry")
	a.WaitText(prompt)
	a.Type("not-the-password")
	a.Press("Enter")
	a.Eventually("the second failure", func() (bool, string) {
		n := 0
		for _, tx := range a.Texts() {
			if strings.HasPrefix(tx.Text, "Failed to authenticate to git remote") {
				n++
			}
		}
		_, boxOpen := a.Text(prompt)
		return n >= 1 && !boxOpen, a.Dump()
	})

	// the password is nowhere: not in the log of the editor, not in a file of the world
	for _, secret := range []string{typedPassword, "not-the-password"} {
		if strings.Contains(a.Log(), secret) {
			t.Errorf("the log of the editor has %q:\n%s", secret, a.Log())
		}
	}
	for _, req := range srv.Requests() {
		if strings.Contains(req, typedPassword) {
			t.Errorf("the server logged the password: %s", req)
		}
	}
	err := filepath.WalkDir(e.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() {
			return nil
		}
		if data, rerr := os.ReadFile(path); rerr == nil && strings.Contains(string(data), typedPassword) {
			t.Errorf("the password is in the file %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// the editor ends and nothing of the helper is left: no askpass program of this run is alive, and its folder is gone
	a.Quit()
	runtimeFolder := filepath.Join(filepath.Dir(project), "run", "emetos")
	a.Eventually("the helper to be gone", func() (bool, string) {
		left := helperProcesses(runtimeFolder)
		_, statErr := os.Stat(runtimeFolder)
		var folders []string
		if entries, readErr := os.ReadDir(runtimeFolder); readErr == nil {
			for _, ent := range entries {
				folders = append(folders, ent.Name())
			}
		}
		return len(left) == 0 && len(folders) == 0 || os.IsNotExist(statErr) && len(left) == 0, strings.Join(left, "\n") + " folders: " + strings.Join(folders, ",")
	})
}

// helperProcesses lists the processes that run one of the helper's flags with a socket in the folder (their environment names it),
// as "pid cmdline".
func helperProcesses(folder string) []string {
	var out []string
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	for _, ent := range entries {
		pid := ent.Name()
		if pid == "" || pid[0] < '0' || pid[0] > '9' {
			continue
		}
		cmdline, err := os.ReadFile("/proc/" + pid + "/cmdline")
		if err != nil || !strings.Contains(string(cmdline), "--git-") {
			continue
		}
		environ, err := os.ReadFile("/proc/" + pid + "/environ")
		if err != nil || !strings.Contains(string(environ), "EMETOS_GIT_SOCKET="+folder) {
			continue
		}
		out = append(out, pid+" "+strings.ReplaceAll(string(cmdline), "\x00", " "))
	}
	return out
}

// withUserHost is the host of a prompt that names the user: http://ada@127.0.0.1:<port>.
func withUserHost(host string) string { return strings.Replace(host, "http://", "http://ada@", 1) }
