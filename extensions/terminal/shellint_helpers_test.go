package terminal

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// bashTerm is a terminal with the test's own bash and VS Code's script injected, in an isolated home folder with no rc files.
type bashTerm struct {
	*Terminal
	home, dir string
}

// startBash starts bash with the shell integration injected; the test is skipped when bash is not installed.
func startBash(t testing.TB, rows, cols int) *bashTerm { return startBashWith(t, rows, cols, nil) }

// startBashWith is startBash with a function that starts the program (nil is a real pseudo-terminal).
func startBashWith(t testing.TB, rows, cols int, start starter) *bashTerm {
	t.Helper()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}
	env := isolatedEnv(t)
	var home string
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "HOME="); ok {
			home = v
		}
	}
	root, err := writeScripts(filepath.Join(home, "cache"))
	if err != nil {
		t.Fatal(err)
	}
	inj, reason := shellIntegrationInjection("bash", nil, injectOpts{enabled: true, root: root, nonce: "testnonce", home: home})
	if reason != "" {
		t.Fatalf("injection: %s", reason)
	}
	for k, v := range inj.env {
		env = append(env, k+"="+v)
	}
	dir := t.TempDir()
	term, err := newTerminal(t.Context(), terminalOpts{
		id: 1, profile: profile{name: "bash", path: bash, args: inj.args}, dir: dir, env: env, rows: rows, cols: cols, scrollback: 1000,
		version: "test", grace: 200 * time.Millisecond, nonce: "testnonce", injected: true, start: start,
	})
	if err != nil {
		t.Fatalf("starting bash: %v", err)
	}
	t.Cleanup(func() {
		term.Kill()
		select {
		case <-term.Exited():
		case <-time.After(5 * time.Second):
			t.Errorf("terminal %d did not end", term.ID())
		}
		term.Wait()
	})
	return &bashTerm{Terminal: term, home: home, dir: dir}
}

// run types a line and waits until the shell shows the next prompt: n commands have finished.
func (b *bashTerm) run(t testing.TB, line string, n int) {
	t.Helper()
	b.Send([]byte(line + "\r"))
	b.waitCommands(t, n)
}

// waitCommands waits until n commands have finished and the shell asks for the next one.
func (b *bashTerm) waitCommands(t testing.TB, n int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		if len(b.si.Commands()) >= n && b.si.Current() != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d commands; %d finished; the terminal shows:\n%s", n, len(b.si.Commands()), allText(b.Terminal))
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// writeFile writes a file of the test.
func writeFile(t testing.TB, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
