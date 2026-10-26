package golang

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/golang/gotest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// fakeGoDir is the folder that holds the fake go program as "go"; the tests that want a go program put it first in PATH. It
// is built once per test run by TestMain.
var fakeGoDir string

// fakeLintDir is the folder that holds the fake lint tool as staticcheck, golangci-lint and revive (step 14.3); a test that wants
// them puts it in PATH after the fake go.
var fakeLintDir string

// emptyPath is an empty folder: the PATH of every test that has no go program, whatever the environment of the run is.
var emptyPath string

// TestMain builds the fake go program, then shuts every way the tests could reach the user's own Go: PATH is an empty folder
// (a test that wants the fake puts its folder there), HOME and the XDG folders are temporary, and no variable of the Go
// environment is left. With the build tag emetos_gopls (the tests of the real tools) the environment is left alone.
func TestMain(m *testing.M) { os.Exit(run(m)) }

func run(m *testing.M) int {
	dir, err := os.MkdirTemp("", "golang-ext-test-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "temporary folder:", err)
		return 1
	}
	defer func() { _ = os.RemoveAll(dir) }()
	fakeGoDir = filepath.Join(dir, "fakego")
	if err := os.Mkdir(fakeGoDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "fake go folder:", err)
		return 1
	}
	if _, err := gotest.Build(fakeGoDir); err != nil {
		fmt.Fprintln(os.Stderr, "building the fake go:", err)
		return 1
	}
	fakeLintDir = filepath.Join(dir, "fakelint")
	if err := os.Mkdir(fakeLintDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "fake lint folder:", err)
		return 1
	}
	for _, name := range []string{"staticcheck", "golangci-lint", "revive"} {
		if _, err := gotest.BuildLint(fakeLintDir, name); err != nil {
			fmt.Fprintln(os.Stderr, "building the fake lint tool:", err)
			return 1
		}
	}
	emptyPath = filepath.Join(dir, "empty-path")
	if err := os.Mkdir(emptyPath, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "empty PATH folder:", err)
		return 1
	}
	if !realTools {
		isolate(dir)
	}
	return m.Run()
}

// isolate empties PATH and moves HOME and the XDG folders into dir.
func isolate(dir string) {
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "GO") || strings.HasPrefix(name, "FAKEGO") || strings.HasPrefix(name, "XDG_") || name == "CGO_ENABLED" {
			_ = os.Unsetenv(name)
		}
	}
	for k, v := range map[string]string{
		"PATH": emptyPath, "HOME": dir + "/home", "XDG_CONFIG_HOME": dir + "/config", "XDG_DATA_HOME": dir + "/data",
		"XDG_CACHE_HOME": dir + "/cache", "XDG_STATE_HOME": dir + "/state",
	} {
		if err := os.Setenv(k, v); err != nil {
			fmt.Fprintln(os.Stderr, "setenv:", err)
			os.Exit(1)
		}
	}
}

// rig is the extension activated over the fake API, with a folder.
type rig struct {
	t      *testing.T
	api    *fakeapi.API
	ext    *golang
	folder string
	log    string
	// lintLog is the file the fake lint tools write a record of each run to.
	lintLog string
	cancel  context.CancelFunc
}

// rigOptions are what a test changes before the activation.
type rigOptions struct {
	// withGo puts the fake go program in PATH; realGo leaves PATH as the run has it, for the tests of the real tools (build tag
	// emetos_gopls) that only read. Without either PATH is empty, so that no test can reach the user's own go.
	withGo, realGo bool
	// withLint puts the fake lint tools (staticcheck, golangci-lint, revive) in PATH after the fake go.
	withLint bool
	// env are the variables of the fake ("NAME=value") and settings the values of settings set before the activation.
	env      []string
	settings map[string]any
	// noFolder makes a scope without a folder.
	noFolder bool
}

// newRig activates the extension. The fake go is in PATH when asked, and writes a record of each run to the log of the rig.
func newRig(t *testing.T, opts rigOptions) *rig {
	t.Helper()
	r := &rig{t: t, folder: t.TempDir(), log: filepath.Join(t.TempDir(), "fakego.log")}
	for name, text := range map[string]string{"go.mod": "module example.com/p\n\ngo 1.21\n", "main.go": "package main\n\nfunc main() {}\n"} {
		if err := os.WriteFile(filepath.Join(r.folder, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	switch {
	case opts.withGo && opts.withLint:
		t.Setenv("PATH", fakeGoDir+string(os.PathListSeparator)+fakeLintDir)
	case opts.withGo:
		t.Setenv("PATH", fakeGoDir)
	case !opts.realGo:
		t.Setenv("PATH", emptyPath)
	}
	t.Setenv(gotest.EnvLog, r.log)
	r.lintLog = filepath.Join(t.TempDir(), "fakelint.log")
	t.Setenv(gotest.EnvLintLog, r.lintLog)
	for _, kv := range opts.env {
		name, value, _ := strings.Cut(kv, "=")
		t.Setenv(name, value)
	}
	e := New().(*golang)
	r.ext = e
	info := extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: r.folder}
	if opts.noFolder {
		info.Folder = ""
	}
	r.api = fakeapi.New(t, e.Manifest(), info)
	for k, v := range opts.settings {
		r.api.SetSetting(k, v)
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	if err := e.Activate(ctx, r.api); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.finish)
	return r
}

// finish ends the activation and checks that no program is left.
func (r *rig) finish() {
	r.cancel()
	r.ext.wait()
	r.noProgramLeft()
}

// runs are the records of the runs of the fake go.
func (r *rig) runs() []goRun {
	r.t.Helper()
	data, err := os.ReadFile(r.log)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		r.t.Fatal(err)
	}
	var out []goRun
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var run goRun
		if err := json.Unmarshal([]byte(line), &run); err != nil {
			r.t.Fatalf("log line %q: %v", line, err)
		}
		out = append(out, run)
	}
	return out
}

// goRun is a record of the fake go.
type goRun struct {
	Args []string          `json:"args"`
	Dir  string            `json:"dir"`
	Pid  int               `json:"pid"`
	Pgid int               `json:"pgid"`
	Env  map[string]string `json:"env"`
}

// noProgramLeft checks that no process of a run of the fake is alive.
func (r *rig) noProgramLeft() {
	r.t.Helper()
	for _, run := range r.runs() {
		requireGone(r.t, "the fake go "+strings.Join(run.Args, " "), run.Pgid)
	}
	for _, run := range r.lintRuns() {
		requireGone(r.t, "the fake "+run.Tool+" "+strings.Join(run.Args, " "), run.Pgid)
	}
}

// requireGone waits for the process group to end.
func requireGone(t *testing.T, what string, pgid int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for syscall.Kill(-pgid, 0) != syscall.ESRCH {
		if time.Now().After(deadline) {
			t.Fatalf("%s (group %d) is still running", what, pgid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// pidsOf reads the process ids a hanging install wrote.
func pidsOf(t *testing.T, path string) (pid, child int) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil {
			if n, _ := fmt.Sscanf(string(data), "%d\n%d\n", &pid, &child); n == 2 {
				return pid, child
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("the pid file %s was not written", path)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// gone tells whether the process does not exist.
func gone(pid int) bool { return syscall.Kill(pid, 0) == syscall.ESRCH }

// sleepMS waits some milliseconds.
func sleepMS(n int) { time.Sleep(time.Duration(n) * time.Millisecond) }
