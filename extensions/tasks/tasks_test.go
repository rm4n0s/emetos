package tasks

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
)

// rig is the extension activated over the fake API, in a folder with a file the fake commands can point at.
type rig struct {
	t      *testing.T
	api    *fakeapi.API
	ext    *tasks
	folder string
	cancel context.CancelFunc
}

// newRig activates the extension in a folder that has main.go, with the commands of the setting.
func newRig(t *testing.T, commands ...string) *rig {
	t.Helper()
	folder := t.TempDir()
	if err := os.WriteFile(filepath.Join(folder, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return activate(t, folder, commands...)
}

func activate(t *testing.T, folder string, commands ...string) *rig {
	t.Helper()
	e := New()
	api := fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: folder, ID: folder})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := e.Activate(ctx, api); err != nil {
		t.Fatal(err)
	}
	r := &rig{t: t, api: api, ext: e.(*tasks), folder: folder, cancel: cancel}
	if len(commands) > 0 {
		api.SetSetting(CommandsSetting, commands)
	}
	t.Cleanup(func() { r.waitForTheProcess() })
	return r
}

// waitForTheProcess lets the process of a run that was cancelled end, so the test leaves none behind.
func (r *rig) waitForTheProcess() {
	r.cancel()
	if p := r.ext.proc; p != nil {
		select {
		case <-p.done:
		case <-time.After(10 * time.Second):
			r.t.Error("a task process outlived its test")
		}
	}
}

// texts are the lines of the output of the view.
func (r *rig) texts() []string {
	var out []string
	for _, l := range r.ext.lines {
		out = append(out, l.text)
	}
	return out
}

// done waits until the run has ended.
func (r *rig) done() {
	r.t.Helper()
	r.api.WaitFor("the task to end", func() bool {
		return r.ext.cancel == nil && r.ext.result != "running" && r.ext.result != "stopping" && r.ext.result != ""
	})
}

// alive tells whether the process with the id still exists.
func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

func TestARunStreamsItsOutputAndFillsTheProblems(t *testing.T) {
	r := newRig(t, "printf 'building\\n./main.go:3:4: undefined: foo\\n./main.go:7:1: warning: unused\\nnot a place.go:1:1: x\\nmissing.go:2:2: nobody\\ndone\\n'")
	if err := r.api.Run(RunCommand, nil); err != nil {
		t.Fatal(err)
	}
	if r.api.ContextKey(RunningKey) != true || r.ext.cancel == nil {
		t.Error("the task is not running right after the command")
	}
	r.done()
	want := []string{"building", "./main.go:3:4: undefined: foo", "./main.go:7:1: warning: unused", "not a place.go:1:1: x", "missing.go:2:2: nobody", "done"}
	if got := r.texts(); !slices.Equal(got, want) {
		t.Errorf("output = %q, want %q", got, want)
	}
	// the Output channel has the command, the lines and the end
	ch := r.api.Channel(ChannelName)
	lines := ch.Lines()
	if len(lines) != len(want)+2 || !strings.HasPrefix(lines[0], "> printf") || !strings.HasPrefix(lines[len(lines)-1], "> done (") {
		t.Errorf("the channel has %q", lines)
	}
	if ch.Shown() == 0 {
		t.Error("the Output panel was not shown")
	}
	// the lines that name a file of the folder are markers, the others are not
	main := "file://" + filepath.ToSlash(filepath.Join(r.folder, "main.go"))
	got := r.api.Markers(ProblemsOwner)
	if len(got) != 1 || len(got[main]) != 2 {
		t.Fatalf("markers = %+v", got)
	}
	if m := got[main][0]; m.Severity != extension.SeverityError || m.Message != "undefined: foo" || m.StartLine != 3 || m.StartColumn != 4 {
		t.Errorf("first marker = %+v", m)
	}
	if m := got[main][1]; m.Severity != extension.SeverityWarning || m.StartLine != 7 {
		t.Errorf("second marker = %+v", m)
	}
	// the tab says how many, and the task is no longer running
	if _, badge, tip := r.api.View(ViewID).Snapshot(); badge != 2 || tip != "2 problems from the last run" {
		t.Errorf("badge %d %q", badge, tip)
	}
	if r.api.ContextKey(RunningKey) != nil || r.ext.result != "done" {
		t.Errorf("after the run: running=%v result=%q", r.api.ContextKey(RunningKey), r.ext.result)
	}
	if it := r.api.StatusItem("task"); it == nil || !strings.HasPrefix(it.Text, "$(check) printf") {
		t.Errorf("status item = %+v", it)
	}
	if reveals := r.api.View(ViewID).Reveals; len(reveals) == 0 {
		t.Error("the view was not revealed")
	}
}

func TestTheOutputIsStreamedWhileTheTaskRuns(t *testing.T) {
	gate := filepath.Join(t.TempDir(), "go")
	r := newRig(t, "echo first; while [ ! -f "+gate+" ]; do sleep 0.02; done; echo last")
	if err := r.api.Run(RunCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.api.WaitFor("the first line", func() bool { return slices.Contains(r.texts(), "first") })
	if r.ext.cancel == nil || slices.Contains(r.texts(), "last") {
		t.Fatal("the task ended before the first line was seen")
	}
	if err := os.WriteFile(gate, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	r.done()
	if got := r.texts(); !slices.Equal(got, []string{"first", "last"}) {
		t.Errorf("output = %q", got)
	}
}

func TestACommandThatFailsEndsWithItsExitCode(t *testing.T) {
	r := newRig(t, "echo oops; exit 3")
	_ = r.api.Run(RunCommand, nil)
	r.done()
	if r.ext.result != "exit 3" {
		t.Errorf("result = %q", r.ext.result)
	}
	if it := r.api.StatusItem("task"); !strings.HasPrefix(it.Text, "$(error)") {
		t.Errorf("status = %q", it.Text)
	}
}

// startChild runs a command that starts a child and prints its process id.
const startChild = "sleep 60 & echo $!; wait"

func (r *rig) childPID() int {
	r.t.Helper()
	var pid int
	r.api.WaitFor("the id of the child", func() bool {
		for _, l := range r.texts() {
			if n, err := strconv.Atoi(l); err == nil {
				pid = n
				return true
			}
		}
		return false
	})
	if !alive(pid) {
		r.t.Fatalf("the child %d is not running", pid)
	}
	return pid
}

func waitGone(t *testing.T, pid int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for alive(pid) {
		if time.Now().After(deadline) {
			t.Fatalf("the process %d is still running", pid)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStopEndsTheTaskAndEverythingItStarted(t *testing.T) {
	r := newRig(t, startChild)
	_ = r.api.Run(RunCommand, nil)
	pid := r.childPID()
	if err := r.api.Run(StopCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.done()
	if r.ext.result != "stopped" {
		t.Errorf("result = %q", r.ext.result)
	}
	waitGone(t, pid)
	if r.api.ContextKey(RunningKey) != nil {
		t.Error("the task still counts as running")
	}
	// a stop with nothing running does nothing, and another task can start
	_ = r.api.Run(StopCommand, nil)
}

func TestClosingTheScopeStopsTheTaskAndItsChildren(t *testing.T) {
	r := newRig(t, startChild)
	_ = r.api.Run(RunCommand, nil)
	pid := r.childPID()
	proc := r.ext.proc
	r.cancel() // the activation ends: the window tab closes or the extension is turned off
	select {
	case <-proc.done:
	case <-time.After(10 * time.Second):
		t.Fatal("the process did not end with the scope")
	}
	waitGone(t, pid)
}

func TestACommandThatLeavesAChildBehindDoesNotKeepIt(t *testing.T) {
	r := newRig(t, "sleep 60 & echo $!")
	_ = r.api.Run(RunCommand, nil)
	pid := r.childPID()
	r.done()
	waitGone(t, pid)
}

func TestASecondTaskIsRefusedWhileOneRuns(t *testing.T) {
	r := newRig(t, startChild, "echo second")
	_ = r.api.Run(RunCommand, nil)
	r.childPID()
	if err := r.api.Run(RunCommand, float64(1)); err != nil {
		t.Fatal(err)
	}
	notes := r.api.Notes()
	if len(notes) != 1 || notes[0].Severity != "warn" || !strings.Contains(notes[0].Message, "running already") {
		t.Errorf("notes = %+v", notes)
	}
	_ = r.api.Run(StopCommand, nil)
	r.done()
}

func TestRunTaskTakesACommandOrAPlace(t *testing.T) {
	r := newRig(t, "echo zero", "echo one")
	tests := []struct {
		name string
		args any
		want string
	}{
		{"nothing runs the first", nil, "zero"},
		{"a number runs that place", float64(1), "one"},
		{"a string runs that command line", "echo three", "three"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := r.api.Run(RunCommand, tt.args); err != nil {
				t.Fatal(err)
			}
			r.done()
			if got := r.texts(); !slices.Equal(got, []string{tt.want}) {
				t.Errorf("output = %q", got)
			}
		})
	}
	before := len(r.api.Notes())
	_ = r.api.Run(RunCommand, float64(7))
	if got := r.api.Notes(); len(got) != before+1 || !strings.Contains(got[before].Message, "no task number 7") {
		t.Errorf("notes = %+v", got)
	}
}

func TestTheDefaultCommandIsGoTest(t *testing.T) {
	r := newRig(t)
	if got := r.ext.commands(); !slices.Equal(got, []string{"go test ./..."}) {
		t.Errorf("commands = %v", got)
	}
	r.api.SetSetting(CommandsSetting, []string{"", "  "})
	before := len(r.api.Notes())
	_ = r.api.Run(RunCommand, nil)
	if got := r.api.Notes(); len(got) != before+1 || !strings.Contains(got[before].Message, "no command to run") {
		t.Errorf("notes = %+v", got)
	}
}

func TestANewRunClearsTheProblemsOfTheLastOne(t *testing.T) {
	r := newRig(t, "echo ./main.go:1:1: bad", "echo clean")
	_ = r.api.Run(RunCommand, nil)
	r.done()
	if r.api.MarkerCount() != 1 {
		t.Fatalf("%d markers after the first run", r.api.MarkerCount())
	}
	_ = r.api.Run(RunCommand, float64(1))
	r.done()
	if r.api.MarkerCount() != 0 {
		t.Errorf("%d markers after a clean run", r.api.MarkerCount())
	}
	if _, badge, _ := r.api.View(ViewID).Snapshot(); badge != 0 {
		t.Errorf("badge %d after a clean run", badge)
	}
}

func TestClearEmptiesTheOutput(t *testing.T) {
	r := newRig(t, "echo a; echo b")
	_ = r.api.Run(RunCommand, nil)
	r.done()
	_ = r.api.Run(ClearCommand, nil)
	if len(r.texts()) != 0 || len(r.api.Channel(ChannelName).Lines()) != 0 {
		t.Errorf("output %q, channel %q", r.texts(), r.api.Channel(ChannelName).Lines())
	}
}

func TestATaskThatCannotStartSaysSo(t *testing.T) {
	r := newRig(t, "echo x")
	if err := os.RemoveAll(r.folder); err != nil {
		t.Fatal(err)
	}
	_ = r.api.Run(RunCommand, nil)
	if r.ext.cancel != nil || !strings.HasPrefix(r.ext.result, "could not start") {
		t.Errorf("running=%v result=%q", r.ext.cancel != nil, r.ext.result)
	}
	notes := r.api.Notes()
	if len(notes) != 1 || notes[0].Severity != "error" {
		t.Errorf("notes = %+v", notes)
	}
}

func TestAWorkspaceWithNoFolderRunsNothing(t *testing.T) {
	r := activate(t, "")
	_ = r.api.Run(RunCommand, nil)
	notes := r.api.Notes()
	if len(notes) != 1 || !strings.Contains(notes[0].Message, "Open a folder") || r.ext.cancel != nil {
		t.Errorf("notes = %+v", notes)
	}
}

func TestTheStateKeepsTheLastRun(t *testing.T) {
	r := newRig(t, "echo one; echo two")
	_ = r.api.Run(RunCommand, nil)
	r.done()
	saved, err := r.ext.saveState()
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(saved)
	if string(data) != `{"command":"echo one; echo two","result":"done","lines":["one","two"]}` {
		t.Errorf("state = %s", data)
	}
	r2 := newRig(t)
	if err := r2.api.View(ViewID).Content.RestoreState(data); err != nil {
		t.Fatal(err)
	}
	if got := r2.texts(); !slices.Equal(got, []string{"one", "two"}) || r2.ext.last != "echo one; echo two" || r2.ext.result != "done" {
		t.Errorf("restored: %q %q %q", got, r2.ext.last, r2.ext.result)
	}
	if len(r2.ext.model.Rows()) != 2 {
		t.Errorf("the list has %d rows", len(r2.ext.model.Rows()))
	}
	// a state that is not JSON is an error of the call, and the view keeps what it has
	if err := r2.api.View(ViewID).Content.RestoreState([]byte("{")); err == nil {
		t.Error("a state that is not JSON was accepted")
	}
}

func TestAClickOnAProblemLineOpensTheFile(t *testing.T) {
	r := newRig(t, "echo ./main.go:3:4: bad; echo plain")
	_ = r.api.Run(RunCommand, nil)
	r.done()
	rows := r.ext.model.Rows()
	r.ext.onSelect(rows[1]) // the plain line points nowhere
	if len(r.api.ShownDocuments()) != 0 {
		t.Error("a line that names no place opened a document")
	}
	r.ext.onSelect(rows[0])
	got := r.api.ShownDocuments()
	if len(got) != 1 || got[0].URI != "file://"+filepath.ToSlash(filepath.Join(r.folder, "main.go")) || got[0].Line != 3 || got[0].Column != 4 {
		t.Errorf("shown = %+v", got)
	}
}

func TestLongOutputKeepsItsLastLines(t *testing.T) {
	r := newRig(t, "i=0; while [ $i -lt "+strconv.Itoa(maxLines+300)+" ]; do echo line$i; i=$((i+1)); done")
	_ = r.api.Run(RunCommand, nil)
	r.done()
	got := r.texts()
	if len(got) != maxLines || got[len(got)-1] != "line"+strconv.Itoa(maxLines+299) || got[0] != "line300" {
		t.Errorf("%d lines from %q to %q", len(got), got[0], got[len(got)-1])
	}
	if r.ext.base != 300 {
		t.Errorf("base = %d", r.ext.base)
	}
}

func TestTheManifestDeclaresWhatItNeeds(t *testing.T) {
	m := New().Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if len(m.Contributes.PanelViews) != 1 || m.Contributes.PanelViews[0].ID != ViewID {
		t.Errorf("panel views = %+v", m.Contributes.PanelViews)
	}
}
