package golang

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/golang/gotest"
)

// "Go: Run" (step 14.2) with the fake go program: nothing here runs a real go.

// runsOf are the runs of the fake go that were `go run`.
func (r *rig) goRuns() []goRun {
	var out []goRun
	for _, run := range r.runs() {
		if len(run.Args) > 0 && run.Args[0] == "run" {
			out = append(out, run)
		}
	}
	return out
}

// text is the output of the run.
func (j *runJob) text() string {
	j.mu.Lock()
	defer j.mu.Unlock()
	return strings.Join(j.output, "\n")
}

// ended waits for the newest run to end and returns it.
func (r *rig) ended() *runJob {
	r.t.Helper()
	r.api.WaitFor("the run to end", func() bool { return r.ext.runs.run != nil && r.ext.runs.run.state != runRunning })
	return r.ext.runs.run
}

func TestRunStreamsTheOutputOfGoRun(t *testing.T) {
	gate := filepath.Join(t.TempDir(), "gate")
	r := newRig(t, rigOptions{withGo: true, env: []string{
		gotest.EnvRunStdout + "=first line", gotest.EnvRunGate + "=" + gate, gotest.EnvRunStderr + "=second line", "FAKE_TOOLS_VAR=",
	}, settings: map[string]any{ToolsEnvVarsSetting: map[string]any{"FAKE_TOOLS_VAR": "from-settings"}}})
	r.focus("main.go", "package main\n§\nfunc main() {}\n")
	if err := r.api.Run(RunCommand, nil); err != nil {
		t.Fatal(err)
	}
	view := r.api.View(RunViewID)
	if len(view.Reveals) != 1 || !view.Reveals[0] {
		t.Errorf("the panel was revealed %v", view.Reveals)
	}
	run := r.ext.runs.run
	if run.state != runRunning {
		t.Fatalf("state = %d, want running", run.state)
	}
	// the first line is in the panel while the program still runs (it waits for the gate)
	r.api.WaitFor("the first line", func() bool { return strings.Contains(run.text(), "first line") })
	if run.state != runRunning || run.text() != "$ go run .\nfirst line" {
		t.Errorf("state %d, output %q", run.state, run.text())
	}
	before, _, _ := view.Snapshot()
	if err := os.WriteFile(gate, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	r.ended()
	if run.state != runExited || run.status != "exit status 0" || run.text() != "$ go run .\nfirst line\nsecond line" {
		t.Errorf("state %d, status %q, output %q", run.state, run.status, run.text())
	}
	if after, _, _ := view.Snapshot(); after <= before {
		t.Errorf("the panel was built %d times before the end and %d after it", before, after)
	}
	// go run . in the folder of the file, with the environment of the settings
	runs := r.goRuns()
	if len(runs) != 1 || !slices.Equal(runs[0].Args, []string{"run", "."}) || runs[0].Dir != realPath(t, r.folder) || runs[0].Env["FAKE_TOOLS_VAR"] != "from-settings" {
		t.Errorf("runs = %+v", runs)
	}
	if run.label != "." {
		t.Errorf("label = %q", run.label)
	}
}

func TestRunTellsTheExitStatus(t *testing.T) {
	tests := []struct {
		name      string
		env       []string
		state     int
		status    string
		wantInOut string
	}{
		{"a program that succeeds", []string{gotest.EnvRunStdout + "=hello"}, runExited, "exit status 0", "hello"},
		{"a program that fails", []string{gotest.EnvRunStderr + "=panic: boom", gotest.EnvRunExit + "=3"}, runFailed, "exit status 3", "panic: boom"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{withGo: true, env: tt.env})
			if err := r.api.Run(RunCommand, nil); err != nil {
				t.Fatal(err)
			}
			run := r.ended()
			if run.state != tt.state || run.status != tt.status || !strings.Contains(run.text(), tt.wantInOut) {
				t.Errorf("state %d, status %q, output %q", run.state, run.status, run.text())
			}
		})
	}
}

func TestRunInTheFolderOfTheFileInFront(t *testing.T) {
	r := newRig(t, rigOptions{withGo: true})
	sub := filepath.Join(r.folder, "cmd", "tool")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// the file in front is in a folder below the workspace
	uri := fileURI(filepath.Join(sub, "main.go"))
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: uri, LanguageID: "go"})
	if err := r.api.Run(RunCommand, nil); err != nil {
		t.Fatal(err)
	}
	run := r.ended()
	if run.label != "cmd/tool" {
		t.Errorf("label = %q", run.label)
	}
	if runs := r.goRuns(); len(runs) != 1 || runs[0].Dir != realPath(t, sub) {
		t.Errorf("runs = %+v", runs)
	}
	// a file that is not Go: the folder of the workspace
	r.api.Emit(extension.TopicEditorActiveChanged, extension.ActiveEditor{URI: fileURI(filepath.Join(sub, "README.md")), LanguageID: "markdown"})
	if err := r.api.Run(RunCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.api.WaitFor("the second run", func() bool { return len(r.goRuns()) == 2 })
	if runs := r.goRuns(); runs[1].Dir != realPath(t, r.folder) {
		t.Errorf("runs = %+v", runs)
	}
	r.ended()
}

func TestRunStopKillsTheWholeGroup(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pids")
	r := newRig(t, rigOptions{withGo: true, env: []string{gotest.EnvRunStdout + "=serving", gotest.EnvRunHang + "=1", gotest.EnvPidfile + "=" + pidfile}})
	if err := r.api.Run(RunCommand, nil); err != nil {
		t.Fatal(err)
	}
	pid, child := pidsOf(t, pidfile)
	run := r.ext.runs.run
	r.api.WaitFor("the output", func() bool { return strings.Contains(run.text(), "serving") })
	// the Stop button of the panel
	run.stopping = true
	run.cancel()
	r.ended()
	if run.state != runStopped || run.status != "stopped" {
		t.Errorf("state %d, status %q", run.state, run.status)
	}
	requireGone(t, "the program", pid)
	if !waitGone(child) {
		t.Errorf("the child %d of the program is still there", child)
	}
}

// waitGone waits for a process to end.
func waitGone(pid int) bool {
	for i := 0; !gone(pid) && i < 1500; i++ {
		sleepMS(10)
	}
	return gone(pid)
}

func TestRunAgainStopsTheRunThatGoesOn(t *testing.T) {
	pidfile := filepath.Join(t.TempDir(), "pids")
	r := newRig(t, rigOptions{withGo: true, env: []string{gotest.EnvRunHang + "=1", gotest.EnvPidfile + "=" + pidfile}})
	if err := r.api.Run(RunCommand, nil); err != nil {
		t.Fatal(err)
	}
	pid, _ := pidsOf(t, pidfile)
	first := r.ext.runs.run
	if err := os.Remove(pidfile); err != nil {
		t.Fatal(err)
	}
	if err := r.api.Run(RunCommand, nil); err != nil {
		t.Fatal(err)
	}
	second := r.ext.runs.run
	if second == first {
		t.Fatal("no new run")
	}
	r.api.WaitFor("the first run to end", func() bool { return first.state != runRunning })
	if first.state != runStopped {
		t.Errorf("the first run is %d (%s)", first.state, first.status)
	}
	requireGone(t, "the first program", pid)
	newPid, _ := pidsOf(t, pidfile)
	if newPid == pid || second.state != runRunning {
		t.Errorf("second run: state %d, pid %d (first %d)", second.state, newPid, pid)
	}
	// the end of the first run does not touch the panel of the second
	if r.ext.runs.run != second {
		t.Error("the panel shows another run")
	}
	// the end of the extension kills the second
	r.cancel()
	r.ext.wait()
	requireGone(t, "the second program", newPid)
}

func TestRunWithoutGoOrWithoutAPlace(t *testing.T) {
	r := newRig(t, rigOptions{})
	if err := r.api.Run(RunCommand, nil); err != nil {
		t.Fatal(err)
	}
	if sev, msg := r.lastNote(); sev != "error" || !strings.Contains(msg, "go command was not found") {
		t.Errorf("notification = %s %q", sev, msg)
	}
	if r.ext.runs.run != nil {
		t.Error("a run was made without a go program")
	}
	// no workspace folder and no file
	none := newRig(t, rigOptions{withGo: true, noFolder: true})
	if err := none.api.Run(RunCommand, nil); err != nil {
		t.Fatal(err)
	}
	if sev, msg := none.lastNote(); sev != "info" || !strings.Contains(msg, "Run needs a Go file or a folder") {
		t.Errorf("notification = %s %q", sev, msg)
	}
}

func TestRunAProgramThatCannotStart(t *testing.T) {
	r := newRig(t, rigOptions{withGo: true, settings: map[string]any{AlternateToolsSetting: map[string]any{"go": filepath.Join(fakeGoDir, "go")}}})
	// the program is found, but the folder it must run in is not there
	dir := filepath.Join(r.folder, "gone")
	r.ext.startRun(dir)
	run := r.ended()
	if run.state != runNotStarted || run.status == "" {
		t.Errorf("state %d, status %q", run.state, run.status)
	}
}
