package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/tasks"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/views/problems"
)

// preLaunchTask and postDebugTask, debug.onTaskErrors and the settings of step 25.11 (SPEC §10.31), against the fake debug adapter and the
// Tasks extension.

// taskOutcome is what debugRunTask gave.
type taskOutcome struct {
	res debug.TaskResult
	err error
}

// runTask runs debugRunTask on a goroutine, as the service does, with a task runner that answers with the summary.
func (r *debugRig) runTask(checkErrors bool, summary TaskSummary) <-chan taskOutcome {
	r.win.taskRunner = func(context.Context, string, debug.TaskRef) (TaskSummary, error) { return summary, nil }
	ch := make(chan taskOutcome, 1)
	go func() {
		res, err := r.win.debugRunTask(context.Background(), r.folder, debug.TaskRef{Label: "build"}, checkErrors)
		ch <- taskOutcome{res, err}
	}()
	return ch
}

// outcome waits for the outcome of a runTask.
func (r *debugRig) outcome(ch <-chan taskOutcome) taskOutcome {
	r.t.Helper()
	var o taskOutcome
	r.waitFor("the outcome of the task", func() bool {
		select {
		case o = <-ch:
			return true
		default:
			return false
		}
	})
	return o
}

// setSetting changes a setting of the user.
func (r *debugRig) setSetting(key string, value any) {
	r.t.Helper()
	if err := r.win.Settings.Update(context.Background(), key, value, settings.TargetUser); err != nil {
		r.t.Fatal(err)
	}
}

// problemsShown tells whether the Problems tab of the panel is the one that shows.
func (r *debugRig) problemsShown() bool {
	return r.win.Layout.State().PanelVisible && r.win.Views.ActivePanel() == command.ViewContainerMarkers
}

// hidePanel puts the panel away, so that a test sees it open.
func (r *debugRig) hidePanel() {
	r.win.Layout.SetPanelVisible(false)
	r.pump()
}

// withErrors puts that many error markers in the Problems.
func (r *debugRig) withErrors(n int) {
	var list []problems.Marker
	for i := range n {
		list = append(list, problems.Marker{Severity: problems.SeverityError, Message: "bad", StartLine: i + 1, StartColumn: 1})
	}
	r.win.Markers.Set("test", filepath.Join(r.folder, "main.go"), list)
}

// answerDialog presses a button of the dialog that shows, with the checkbox checked when asked.
func (r *debugRig) answerDialog(label string, remember bool) {
	r.t.Helper()
	r.waitFor("the question", func() bool { return r.win.Dialogs.IsShowing() })
	r.pump()
	if remember {
		r.ts.Tap(gluttertest.ByText("Remember my choice in user settings"))
		r.pump()
	}
	r.ts.Tap(gluttertest.Descendant(gluttertest.ByType[*kit.Dialog](), gluttertest.ByText(label)))
	r.pump()
}

func intp(n int) *int { return &n }

func TestOnTaskErrorsDecidesWhatAFailedPreLaunchTaskDoes(t *testing.T) {
	tests := []struct {
		name     string
		setting  string
		summary  TaskSummary
		check    bool
		want     debug.TaskResult
		problems bool // the Problems tab shows afterwards
	}{
		{"a task that passed goes on", "abort", TaskSummary{ExitCode: intp(0)}, true, debug.TaskSuccess, false},
		{"debugAnyway goes on after a failure", "debugAnyway", TaskSummary{ExitCode: intp(2)}, true, debug.TaskSuccess, false},
		{"showErrors shows the Problems and stops", "showErrors", TaskSummary{ExitCode: intp(2)}, true, debug.TaskFailure, true},
		{"abort stops", "abort", TaskSummary{ExitCode: intp(2)}, true, debug.TaskFailure, false},
		{"a task that was stopped stops the launch", "debugAnyway", TaskSummary{Cancelled: true}, true, debug.TaskFailure, false},
		{"a task with no exit code stops the launch", "debugAnyway", TaskSummary{}, true, debug.TaskFailure, false},
		{"a task that is not checked never stops the session", "abort", TaskSummary{ExitCode: intp(2)}, false, debug.TaskSuccess, false},
	}
	r := newDebugRig(t, debugFolder(t, nil))
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r.t = t
			r.setSetting("debug.onTaskErrors", tc.setting)
			r.hidePanel()
			o := r.outcome(r.runTask(tc.check, tc.summary))
			if o.err != nil || o.res != tc.want {
				t.Errorf("result %v, err %v; want %v", o.res, o.err, tc.want)
			}
			if got := r.problemsShown(); got != tc.problems {
				t.Errorf("the Problems tab shows: %v, want %v", got, tc.problems)
			}
			if r.win.Dialogs.IsShowing() {
				t.Error("a question was asked")
			}
		})
	}
}

func TestTheQuestionAboutAFailedPreLaunchTask(t *testing.T) {
	tests := []struct {
		name     string
		errors   int
		exit     int
		message  string
		button   string // the button pressed; "" is Escape
		remember bool
		want     debug.TaskResult
		problems bool
		kept     string // debug.onTaskErrors afterwards
	}{
		{"one error", 1, 2, "Error exists after running preLaunchTask 'build'.", "Debug Anyway", false, debug.TaskSuccess, false, "prompt"},
		{"several errors", 2, 2, "Errors exist after running preLaunchTask 'build'.", "Debug Anyway", false, debug.TaskSuccess, false, "prompt"},
		{"only an exit code", 0, 7, "The preLaunchTask 'build' terminated with exit code 7.", "Debug Anyway", false, debug.TaskSuccess, false, "prompt"},
		{"Show Errors", 0, 2, "The preLaunchTask 'build' terminated with exit code 2.", "Show Errors", false, debug.TaskFailure, true, "prompt"},
		{"Abort", 0, 2, "The preLaunchTask 'build' terminated with exit code 2.", "Abort", false, debug.TaskFailure, false, "prompt"},
		{"Escape is Abort", 0, 2, "The preLaunchTask 'build' terminated with exit code 2.", "", false, debug.TaskFailure, false, "prompt"},
		{"remember Debug Anyway", 0, 2, "The preLaunchTask 'build' terminated with exit code 2.", "Debug Anyway", true, debug.TaskSuccess, false, "debugAnyway"},
		{"remember Show Errors", 0, 2, "The preLaunchTask 'build' terminated with exit code 2.", "Show Errors", true, debug.TaskFailure, true, "showErrors"},
		{"remember Abort", 0, 2, "The preLaunchTask 'build' terminated with exit code 2.", "Abort", true, debug.TaskFailure, false, "abort"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newDebugRig(t, debugFolder(t, nil))
			r.withErrors(tc.errors)
			r.hidePanel()
			ch := r.runTask(true, TaskSummary{ExitCode: intp(tc.exit)})
			r.waitFor("the question", func() bool { return r.win.Dialogs.IsShowing() })
			spec, _ := r.win.Dialogs.Current()
			if spec.Message != tc.message {
				t.Errorf("the question is %q, want %q", spec.Message, tc.message)
			}
			if want := []string{"Debug Anyway", "Show Errors", "Abort"}; len(spec.Buttons) != 3 || spec.Buttons[0] != want[0] || spec.Buttons[1] != want[1] || spec.Buttons[2] != want[2] {
				t.Errorf("the buttons are %v", spec.Buttons)
			}
			if spec.CheckboxLabel != "Remember my choice in user settings" {
				t.Errorf("the checkbox is %q", spec.CheckboxLabel)
			}
			if tc.button == "" {
				r.win.Dialogs.Dismiss()
			} else {
				r.answerDialog(tc.button, tc.remember)
			}
			o := r.outcome(ch)
			if o.err != nil || o.res != tc.want {
				t.Errorf("result %v, err %v; want %v", o.res, o.err, tc.want)
			}
			if got := r.problemsShown(); got != tc.problems {
				t.Errorf("the Problems tab shows: %v, want %v", got, tc.problems)
			}
			if got := settings.Get[string](r.win.Settings, "debug.onTaskErrors", ""); got != tc.kept {
				t.Errorf("debug.onTaskErrors is %q, want %q", got, tc.kept)
			}
		})
	}
}

func TestATaskThatCannotBeAskedForFailsTheStartWithItsOwnTag(t *testing.T) {
	r := newDebugRig(t, debugFolder(t, nil))
	call := func() taskOutcome {
		ch := make(chan taskOutcome, 1)
		go func() {
			res, err := r.win.debugRunTask(context.Background(), r.folder, debug.TaskRef{Label: "build"}, true)
			ch <- taskOutcome{res, err}
		}()
		return r.outcome(ch)
	}
	// the rig runs no Tasks extension
	assertRoute(t, call().err, "Workspace.runTaskThroughExtension.DebugTaskUnavailable")
	// a runner that fails is the failure of the start, untouched
	r.win.taskRunner = func(context.Context, string, debug.TaskRef) (TaskSummary, error) {
		return TaskSummary{}, os.ErrPermission
	}
	if o := call(); o.err != os.ErrPermission || o.res != debug.TaskFailure {
		t.Errorf("outcome = %+v", o)
	}
}

// ---- through the Tasks extension

// tasksFolder is a folder for a session with tasks: the fixture of the debug view with a launch.json of the configurations given.
func tasksFolder(t *testing.T, configs ...string) string {
	t.Helper()
	folder := debugFolder(t, scriptOf(stopSteps("breakpoint", "1")...))
	body := ""
	for i, c := range configs {
		if i > 0 {
			body += ","
		}
		body += c
	}
	launch := `{"version": "0.2.0", "configurations": [` + body + `]}`
	if err := os.WriteFile(filepath.Join(folder, ".emetos", "launch.json"), []byte(launch), 0o644); err != nil {
		t.Fatal(err)
	}
	return folder
}

// fakeConfig is a configuration of the fake debugger with more attributes (JSON members).
func fakeConfig(name string, more string) string {
	if more != "" {
		more = ", " + more
	}
	return `{"type": "dev.debug.fake", "request": "launch", "name": "` + name + `", "script": "${workspaceFolder}/fake.jsonl"` + more + `}`
}

// tasksRig is a rig with the Tasks extension and the tasks of the setting.
func tasksRig(t *testing.T, folder string, extra map[string]any) *debugRig {
	t.Helper()
	r := newDebugRigWith(t, folder, rigOpts{extensions: []extension.Factory{tasks.New}, settings: extra})
	// the setting of the tasks is registered by the extension, so the extension starts first (the window does that for a session)
	r.win.Extensions().Activate(extension.TasksID)
	r.pump()
	r.setSetting(tasks.CommandsSetting, []any{
		map[string]any{"label": "prep", "command": "touch pre.txt"},
		map[string]any{"label": "clean", "command": "touch post.txt"},
		map[string]any{"label": "broken", "command": "echo ./main.go:1:1: bad; exit 3"},
		map[string]any{"label": "quiet-failure", "command": "exit 4"},
	})
	return r
}

// start starts a configuration by its name on a goroutine, as the window does, and gives the channel of the result.
func (r *debugRig) start(name string) <-chan startResult {
	ch := make(chan startResult, 1)
	r.breakpoint() // the script of the fixture answers a setBreakpoints
	launch := r.svc().Launches().All()[0].Launch
	go func() {
		ok, err := r.svc().StartDebugging(context.Background(), launch, debug.Target{Name: name}, debug.SessionOptions{StartedByUser: true})
		ch <- startResult{ok, err}
	}()
	return ch
}

type startResult struct {
	ok  bool
	err error
}

func (r *debugRig) started(ch <-chan startResult) startResult {
	r.t.Helper()
	var s startResult
	r.waitFor("the start to end", func() bool {
		select {
		case s = <-ch:
			return true
		default:
			return false
		}
	})
	return s
}

// exists tells whether a file is in the folder.
func (r *debugRig) exists(name string) bool {
	_, err := os.Stat(filepath.Join(r.folder, name))
	return err == nil
}

func TestThePreLaunchTaskRunsBeforeTheSessionAndThePostDebugTaskAfterIt(t *testing.T) {
	folder := tasksFolder(t, fakeConfig("with tasks", `"preLaunchTask": "prep", "postDebugTask": "clean"`))
	r := tasksRig(t, folder, nil)
	s := r.started(r.start("with tasks"))
	if s.err != nil || !s.ok {
		t.Fatalf("start: %v %v", s.ok, s.err)
	}
	if !r.exists("pre.txt") {
		t.Error("the task before the session did not run")
	}
	if r.exists("post.txt") {
		t.Error("the task after the session ran with the session")
	}
	r.waitState("stopped")
	r.run(command.DebugStopCommand, nil)
	r.waitState("inactive")
	deadline := time.Now().Add(10 * time.Second)
	for !r.exists("post.txt") && time.Now().Before(deadline) {
		r.ts.FlushPosted()
		r.ts.Pump(10 * time.Millisecond)
		time.Sleep(5 * time.Millisecond)
	}
	if !r.exists("post.txt") {
		t.Error("the task after the session did not run")
	}
}

func TestAFailingPreLaunchTaskWithEachAnswer(t *testing.T) {
	tests := []struct {
		name     string
		setting  string
		button   string // for prompt
		session  bool   // a session starts
		problems bool   // the Problems tab shows afterwards
	}{
		{"debugAnyway", "debugAnyway", "", true, false},
		{"showErrors", "showErrors", "", false, true},
		{"abort", "abort", "", false, false},
		{"prompt then Debug Anyway", "prompt", "Debug Anyway", true, false},
		{"prompt then Show Errors", "prompt", "Show Errors", false, true},
		{"prompt then Abort", "prompt", "Abort", false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			folder := tasksFolder(t, fakeConfig("broken build", `"preLaunchTask": "broken"`))
			r := tasksRig(t, folder, map[string]any{"debug.onTaskErrors": tc.setting})
			r.hidePanel()
			ch := r.start("broken build")
			if tc.button != "" {
				r.waitFor("the question", func() bool { return r.win.Dialogs.IsShowing() })
				spec, _ := r.win.Dialogs.Current()
				if spec.Message != "Error exists after running preLaunchTask 'broken'." {
					t.Errorf("the question is %q", spec.Message)
				}
				r.answerDialog(tc.button, false)
			}
			s := r.started(ch)
			if s.err != nil || s.ok != tc.session {
				t.Fatalf("start: %v %v, want a session: %v", s.ok, s.err, tc.session)
			}
			if got := len(r.svc().Model().Sessions(false)) > 0; got != tc.session {
				t.Errorf("a session runs: %v, want %v", got, tc.session)
			}
			if got := r.problemsShown(); got != tc.problems {
				t.Errorf("the Problems tab shows: %v, want %v", got, tc.problems)
			}
			if tc.session {
				r.run(command.DebugStopCommand, nil)
				r.waitState("inactive")
			}
		})
	}
}

func TestAPreLaunchTaskThatFailsWithoutErrorsAsksWithItsExitCode(t *testing.T) {
	folder := tasksFolder(t, fakeConfig("quiet", `"preLaunchTask": "quiet-failure"`))
	r := tasksRig(t, folder, nil)
	ch := r.start("quiet")
	r.waitFor("the question", func() bool { return r.win.Dialogs.IsShowing() })
	if spec, _ := r.win.Dialogs.Current(); spec.Message != "The preLaunchTask 'quiet-failure' terminated with exit code 4." {
		t.Errorf("the question is %q", spec.Message)
	}
	r.answerDialog("Abort", false)
	if s := r.started(ch); s.err != nil || s.ok {
		t.Errorf("start: %v %v", s.ok, s.err)
	}
}

func TestAPreLaunchTaskThatDoesNotExistFailsTheStart(t *testing.T) {
	folder := tasksFolder(t, fakeConfig("lost", `"preLaunchTask": "nope"`), fakeConfig("lost too", `"preLaunchTask": {"type": "shell", "label": "nope"}`))
	r := tasksRig(t, folder, nil)
	for _, name := range []string{"lost", "lost too"} {
		s := r.started(r.start(name))
		if s.ok {
			t.Errorf("%s: a session started", name)
		}
		assertRoute(t, s.err, "TaskNotFound")
	}
}

func TestAWindowThatRunsNoExtensionsCannotAskForATaskOrATerminal(t *testing.T) {
	win := &Workspace{} // no extension scope
	_, err := win.runTaskThroughExtension(context.Background(), "", debug.TaskRef{Label: "build"})
	assertRoute(t, err, "Workspace.runTaskThroughExtension.DebugTaskNoScope")
	assertRoute(t, win.askTerminal(context.Background(), terminalListTopic, nil, nil), "Workspace.askTerminal.DebugTerminalNoScope")
}
