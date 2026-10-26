package agents

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/agents/claude/claudetest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// fakeProgram is the path of the fake claude program, built once per test run by TestMain.
var fakeProgram string

// TestMain builds the fake program, then shuts every way the tests could reach the user's own claude program or account (the
// same way the adapter's tests do): PATH is an empty folder, HOME and the XDG folders are temporary, and no variable that
// names a configuration folder, a key or a token is left. A test that wants the program gives its path in the setting
// agents.claude.path.
func TestMain(m *testing.M) { os.Exit(run(m)) }

func run(m *testing.M) int {
	dir, err := os.MkdirTemp("", "agents-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "temporary folder:", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()
	fakeProgram, err = claudetest.Build(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "building the fake claude:", err)
		return 1
	}
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "CLAUDE") || strings.HasPrefix(name, "ANTHROPIC") || strings.HasPrefix(name, "XDG_") {
			_ = os.Unsetenv(name)
		}
	}
	empty := filepath.Join(dir, "empty-path")
	if err := os.Mkdir(empty, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "empty PATH folder:", err)
		return 1
	}
	for k, v := range map[string]string{
		"PATH": empty, "HOME": dir + "/home", "XDG_CONFIG_HOME": dir + "/config", "XDG_DATA_HOME": dir + "/data",
		"XDG_CACHE_HOME": dir + "/cache", "XDG_STATE_HOME": dir + "/state",
	} {
		if err := os.Setenv(k, v); err != nil {
			fmt.Fprintln(os.Stderr, "setenv:", err)
			return 1
		}
	}
	return m.Run()
}

// rig is the extension activated over the fake API, with a folder and the fake program.
type rig struct {
	t      *testing.T
	api    *fakeapi.API
	ext    *agents
	folder string
	log    string
	cancel context.CancelFunc
	now    time.Time
}

// fixtureFolder is a small project: files to complete, and a file that must never be attached.
func fixtureFolder(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for name, text := range map[string]string{
		"main.go":                "package main\n\nfunc main() {}\n",
		"util/util.go":           "package util\n",
		"util/util_test.go":      "package util\n",
		"README.md":              "# Project\n",
		".env":                   "TOKEN=not-a-real-token\n",
		"secrets/key.txt":        "nothing\n",
		".claude/settings.json":  `{"permissions":{"deny":["Read(./secrets/**)","Bash(rm:*)","Read(//etc/passwd)"]}}`,
		"docs/notes/overview.md": "text\n",
	} {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// newRig activates the extension with the fake program replaying testdata/<script>. An empty script leaves the fake without
// one (the tests that never start a session).
func newRig(t *testing.T, script string) *rig {
	t.Helper()
	r := &rig{t: t, folder: fixtureFolder(t), log: filepath.Join(t.TempDir(), "fake.log"), now: time.Date(2026, 3, 4, 12, 0, 0, 0, time.UTC)}
	if script != "" {
		abs, err := filepath.Abs(filepath.Join("testdata", script))
		if err != nil {
			t.Fatal(err)
		}
		t.Setenv(claudetest.EnvScript, abs)
	}
	t.Setenv(claudetest.EnvLog, r.log)
	t.Setenv(claudetest.EnvAuthFile, filepath.Join(t.TempDir(), "auth"))
	e := New().(*agents)
	e.now = func() time.Time { return r.now }
	r.ext = e
	r.api = fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: r.folder})
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	if err := e.Activate(ctx, r.api); err != nil {
		t.Fatal(err)
	}
	r.api.SetSetting(PathSetting, fakeProgram)
	t.Cleanup(r.finish)
	return r
}

// finish ends everything the test started and checks that no program is left.
func (r *rig) finish() {
	r.cancel()
	for _, tab := range r.api.EditorTabs() {
		tab.Close()
	}
	r.ext.wait()
	r.noProgramLeft()
}

// started returns the records of the programs the fake wrote to its log: its process group and its command line.
func (r *rig) started() []fakeStart {
	data, err := os.ReadFile(r.log)
	if err != nil {
		return nil
	}
	var out []fakeStart
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	sc.Buffer(make([]byte, 0, 64*1024), 16<<20)
	for sc.Scan() {
		var rec struct {
			Kind string   `json:"kind"`
			Argv []string `json:"argv"`
			PGID int      `json:"pgid"`
			CWD  string   `json:"cwd"`
		}
		if json.Unmarshal(sc.Bytes(), &rec) == nil && rec.Kind == "start" {
			out = append(out, fakeStart{Argv: rec.Argv, PGID: rec.PGID, CWD: rec.CWD})
		}
	}
	return out
}

// fakeStart is one start of the fake program.
type fakeStart struct {
	Argv []string
	PGID int
	CWD  string
}

// has tells whether the command line has the argument.
func (s fakeStart) has(arg string) bool {
	for _, a := range s.Argv {
		if a == arg {
			return true
		}
	}
	return false
}

// value is the argument after a flag.
func (s fakeStart) value(flag string) string {
	for i, a := range s.Argv {
		if a == flag && i+1 < len(s.Argv) {
			return s.Argv[i+1]
		}
	}
	return ""
}

// noProgramLeft fails the test when a process group the fake started still exists.
func (r *rig) noProgramLeft() {
	r.t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for _, s := range r.started() {
		for syscall.Kill(-s.PGID, 0) == nil {
			if time.Now().After(deadline) {
				r.t.Errorf("the process group %d of the fake program is still running", s.PGID)
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// open runs the command that opens a new session and returns its tab.
func (r *rig) open() *sessionTab {
	r.t.Helper()
	before := len(r.ext.tabs)
	if err := r.api.Run(NewSessionCommand, nil); err != nil {
		r.t.Fatal(err)
	}
	if len(r.ext.tabs) != before+1 {
		r.t.Fatalf("the command opened %d tabs", len(r.ext.tabs)-before)
	}
	return r.ext.tabs[len(r.ext.tabs)-1]
}

// ready waits until the program of the tab has answered the handshake.
func (r *rig) ready(t *sessionTab) {
	r.t.Helper()
	r.api.WaitFor("the program to be ready", func() bool {
		return t.phase == phaseReady || t.phase == phaseFailed || t.phase == phaseMissing || t.phase == phaseLoggedOut
	})
	if t.phase != phaseReady {
		r.t.Fatalf("the tab is in phase %v (%s)", t.phase, t.problem)
	}
}

// send types a message and sends it.
func (r *rig) send(t *sessionTab, text string) {
	r.t.Helper()
	t.input.SetValue(text)
	t.submit(t.input.Value())
}

// idle waits for the turns of the tab to end.
func (r *rig) idle(t *sessionTab) {
	r.t.Helper()
	r.api.WaitFor("the turn to end", func() bool { return t.turns == 0 && !t.busy })
}

// texts lists the kind and text of the items of the conversation, for a short comparison.
func texts(t *sessionTab) []string {
	var out []string
	for _, it := range t.tr.items {
		switch it.Kind {
		case kindTool:
			out = append(out, "tool:"+it.Tool+":"+it.Summary)
		default:
			out = append(out, it.Kind+":"+it.Text)
		}
	}
	return out
}

// route fails the test unless err has the route.
func route(t *testing.T, err error, want string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(want) {
		t.Errorf("unexpected failure path: %s (want %s)", appErr.Route(), want)
	}
}
