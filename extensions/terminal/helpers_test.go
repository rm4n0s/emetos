package terminal

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

var (
	shellOnce sync.Once
	shellPath string
	shellDir  string
	shellErr  error
)

// TestMain removes the test shell the tests built.
func TestMain(m *testing.M) {
	code := m.Run()
	if shellDir != "" {
		_ = os.RemoveAll(shellDir)
	}
	os.Exit(code)
}

// testShell builds the test shell (testdata/testshell) once and returns the path of the program.
func testShell(t testing.TB) string {
	t.Helper()
	shellOnce.Do(func() {
		shellDir, shellErr = os.MkdirTemp("", "emetos-testshell")
		if shellErr != nil {
			return
		}
		shellPath = filepath.Join(shellDir, "testshell")
		cmd := exec.Command("go", "build", "-o", shellPath, ".")
		cmd.Dir = filepath.Join("testdata", "testshell")
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			shellErr = &buildError{err: err, out: string(out)}
		}
	})
	if shellErr != nil {
		t.Fatalf("building the test shell: %v", shellErr)
	}
	return shellPath
}

type buildError struct {
	err error
	out string
}

func (b *buildError) Error() string { return b.err.Error() + "\n" + b.out }

// isolatedEnv is the environment of the shells of the tests: nothing of the user's, a home folder of the test, no rc files.
func isolatedEnv(t testing.TB) []string {
	t.Helper()
	home := t.TempDir()
	return []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + home, "ZDOTDIR=" + home, "XDG_CONFIG_HOME=" + filepath.Join(home, "config"),
		"XDG_STATE_HOME=" + filepath.Join(home, "state"), "XDG_CACHE_HOME=" + filepath.Join(home, "cache"),
		"XDG_DATA_HOME=" + filepath.Join(home, "data"), "TERM=xterm-256color", "LANG=C.UTF-8", "PS1=$ ",
	}
}

// shStarted starts /bin/sh -c script in a terminal of the given size, killed when the test ends.
func startTerminal(t testing.TB, prof profile, rows, cols int) *Terminal {
	t.Helper()
	dir := t.TempDir()
	term, err := newTerminal(t.Context(), terminalOpts{
		id: 1, profile: prof, dir: dir, env: isolatedEnv(t), rows: rows, cols: cols, scrollback: 1000, version: "test",
		grace: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("starting a terminal: %v", err)
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
	return term
}

// startTestShell starts the test shell in a terminal.
func startTestShell(t testing.TB, rows, cols int) *Terminal {
	t.Helper()
	return startTerminal(t, profile{name: "testshell", path: testShell(t)}, rows, cols)
}

// startSh starts /bin/sh with a script.
func startSh(t testing.TB, script string, rows, cols int) *Terminal {
	t.Helper()
	return startTerminal(t, profile{name: "sh", path: "/bin/sh", args: []string{"-c", script}}, rows, cols)
}

// allText is the text of the scrollback and the screen.
func allText(term *Terminal) string {
	em := term.Emulator()
	return strings.TrimRight(em.Text(0, em.LineCount()-1), "\n ")
}

// waitText waits until the text of the screen and the scrollback has want in it.
func waitText(t testing.TB, term *Terminal, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		got := allText(term)
		if strings.Contains(got, want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %q; the terminal shows:\n%s", want, got)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// routeHas checks that the error came from the errors package and its route has the text.
func routeHas(t testing.TB, err error, want string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(want) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), want)
	}
}

// testConfig is the configuration of VS Code's defaults, with the font of the tests.
func testConfig() *config {
	return &config{
		scrollback: 1000, fontFamilies: []string{"Go Mono"}, fontSize: 14, lineHeight: 1, weight: 400, weightBold: 700,
		cursorStyle: "block", cursorInactive: "outline", cursorWidth: 1, boldBright: true, minContrast: 4.5,
		wheelSens: 1, fastSens: 5, wordSeparators: " ()[]{}',\"`─‘’“”|", rightClick: "default", middleClick: "default",
		skip: newSkipSet(nil), allowChords: true, multiLinePaste: "auto", tabsEnabled: true, tabsLocation: "right",
		tabsHide: "singleTerminal", tabsTitle: "${process}", defaultIcon: "terminal",
	}
}

// postBox is the post function of the tests: functions the terminal's goroutines post wait until the tester exists.
type postBox struct {
	mu   sync.Mutex
	post func(func())
	wait []func()
}

func (p *postBox) Post(f func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.post == nil {
		p.wait = append(p.wait, f)
		return
	}
	p.post(f)
}

func (p *postBox) attach(post func(func())) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.post = post
	for _, f := range p.wait {
		post(f)
	}
	p.wait = nil
}
