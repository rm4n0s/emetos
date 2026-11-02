package app

import (
	"context"
	"image"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	apperrors "github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	dbgview "github.com/rm4n0s/emetos/internal/gui/views/debug"
)

// The scripts of the fake adapter the tests of the Run and Debug view use, built on the script of the fixture of the proof: its start (up to
// the first stop) and its end are kept, and the steps in between are the test's.

var threadsBody = map[string]any{"threads": []map[string]any{{"id": 1, "name": "main"}, {"id": 2, "name": "worker"}}}

// stopSteps are the stop of the thread 1 and the requests of the views that follow it: the threads, the stack of two frames, the scopes and
// the variables, with sum as the value of the variable sum.
func stopSteps(reason, sum string) []daptest.Step {
	return []daptest.Step{
		daptest.Event("stopped", map[string]any{"reason": reason, "threadId": 1, "allThreadsStopped": true}),
		daptest.Reply("threads", threadsBody),
		daptest.Reply("stackTrace", map[string]any{"totalFrames": 2, "stackFrames": []map[string]any{
			{"id": 101, "name": "main.main", "line": 21, "column": 2, "source": map[string]any{"name": "main.go", "path": "${workspaceFolder}/main.go"}},
			{"id": 102, "name": "runtime.main", "line": 267, "column": 3, "presentationHint": "subtle", "source": map[string]any{"name": "proc.go", "path": "/usr/lib/go/src/runtime/proc.go"}},
		}}),
		daptest.Reply("scopes", map[string]any{"scopes": []map[string]any{{"name": "Locals", "variablesReference": 1000}}}),
		daptest.Reply("variables", map[string]any{"variables": []map[string]any{
			{"name": "prices", "value": "[]float64 len:3", "type": "[]float64", "variablesReference": 1001, "evaluateName": "prices"},
			{"name": "order", "value": "main.Order", "type": "main.Order", "variablesReference": 1002, "evaluateName": "order"},
			{"name": "sum", "value": sum, "type": "float64", "evaluateName": "sum"},
		}}),
	}
}

// orderChildren is the answer for the expansion of the variable order.
func orderChildren() daptest.Step {
	return daptest.Reply("variables", map[string]any{"variables": []map[string]any{
		{"name": "ID", "value": "42", "type": "int", "evaluateName": "order.ID"},
		{"name": "Total", "value": "0", "type": "float64", "evaluateName": "order.Total"},
		{"name": "Items", "value": "[]string len:3", "type": "[]string", "variablesReference": 1003, "evaluateName": "order.Items"},
	}})
}

// scriptOf is a script for debugFolder: the start of the fixture's script, the steps, and the end (disconnect).
func scriptOf(steps ...daptest.Step) func(daptest.Script) daptest.Script {
	return func(def daptest.Script) daptest.Script {
		var out daptest.Script
		for _, st := range def {
			if st.Op == daptest.OpEvent && st.Event == "stopped" {
				break
			}
			out = append(out, st)
		}
		out = append(out, steps...)
		return append(out, daptest.Reply("disconnect", nil), daptest.WaitClose())
	}
}

// join puts lists of steps together.
func joinSteps(parts ...[]daptest.Step) []daptest.Step {
	var out []daptest.Step
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// waitState waits for the context key debugState of the window to be the state.
func (r *debugRig) waitState(state string) {
	r.t.Helper()
	r.waitFor("the state "+state, func() bool { return r.win.Keys.Get("debugState") == state })
}

func TestTheCommandsAndKeysDriveASession(t *testing.T) {
	// two stops: the first at the start, the second after the step
	two := func(request string) func(daptest.Script) daptest.Script {
		return scriptOf(joinSteps(stopSteps("breakpoint", "23.75"),
			[]daptest.Step{daptest.Reply(request, nil)}, stopSteps("step", "47.5"))...)
	}
	cases := []struct {
		name    string
		script  func(daptest.Script) daptest.Script
		command string
		key     keybinding.Chord
		// check runs after the command and waits for its effect
		check func(t *testing.T, r *debugRig)
	}{
		{"Step Over", two("next"), command.DebugStepOverCommand, keybinding.Chord{Key: keybinding.F10}, nil},
		{"Step Into", two("stepIn"), command.DebugStepIntoCommand, keybinding.Chord{Key: keybinding.F11}, nil},
		{"Step Out", two("stepOut"), command.DebugStepOutCommand, keybinding.Chord{Key: keybinding.F11, Shift: true}, nil},
		{"Continue", scriptOf(joinSteps(stopSteps("breakpoint", "23.75"), []daptest.Step{daptest.Reply("continue", map[string]any{"allThreadsContinued": true})})...),
			command.DebugContinueCommand, keybinding.Chord{Key: keybinding.F5},
			func(t *testing.T, r *debugRig) { r.waitState("running") }},
		{"Pause", scriptOf(joinSteps(stopSteps("breakpoint", "23.75"), []daptest.Step{daptest.Reply("continue", nil), daptest.Reply("pause", nil)},
			stopSteps("pause", "23.75"))...), command.DebugPauseCommand, keybinding.Chord{Key: keybinding.F6},
			func(t *testing.T, r *debugRig) { r.waitState("stopped") }},
		{"Stop", scriptOf(stopSteps("breakpoint", "23.75")...), command.DebugStopCommand, keybinding.Chord{Key: keybinding.F5, Shift: true},
			func(t *testing.T, r *debugRig) { r.waitState("inactive") }},
	}
	for _, tc := range cases {
		for _, how := range []string{"command", "key"} {
			t.Run(tc.name+" by the "+how, func(t *testing.T) {
				r := newDebugRig(t, debugFolder(t, tc.script))
				r.stopped()
				// the commands that need another stop first (Pause needs a running thread; Continue is the key that does it)
				if tc.name == "Pause" {
					r.run(command.DebugContinueCommand, nil)
					r.waitState("running")
				}
				if how == "command" {
					r.run(tc.command, nil)
				} else if !sendChord(r.ts, tc.key) {
					t.Fatalf("the key %v is bound to nothing in the state %v", tc.key, r.win.Keys.Get("debugState"))
				}
				r.pump()
				switch {
				case tc.check != nil:
					tc.check(t, r)
				default:
					// a step ends with a second stop: the variable has its new value
					r.waitFor("the second stop", func() bool { return r.sum() == "47.5" })
				}
				if errs := r.ts.TakeErrors(); len(errs) != 0 {
					t.Errorf("the framework reported errors: %v", errs)
				}
			})
		}
	}
}

func TestF5StartsASessionAndThenContinuesIt(t *testing.T) {
	r := newDebugRig(t, debugFolder(t, scriptOf(joinSteps(stopSteps("breakpoint", "23.75"), []daptest.Step{daptest.Reply("continue", map[string]any{"allThreadsContinued": true})})...)))
	r.breakpoint()
	r.run(command.ViewContainerDebug, nil)
	if !sendChord(r.ts, keybinding.Chord{Key: keybinding.F5}) {
		t.Fatal("F5 starts nothing before a session")
	}
	r.waitState("stopped")
	if !sendChord(r.ts, keybinding.Chord{Key: keybinding.F5}) {
		t.Fatal("F5 continues nothing while stopped")
	}
	r.waitState("running")
}

func TestRestartingASessionStartsItAgain(t *testing.T) {
	r := newDebugRig(t, debugFolder(t, scriptOf(stopSteps("breakpoint", "23.75")...)))
	r.stopped()
	sess := r.svc().Model().Focus().Session
	var started atomic.Int32
	stop := r.svc().Subscribe(func(ev debug.Event) {
		if ev.Kind == debug.EventSessionStarted {
			started.Add(1)
		}
	})
	defer stop()
	r.run(command.DebugRestartCommand, nil)
	// a restart is the same session over a new adapter: it announces its start again and stops again at the breakpoint
	r.waitFor("the session to start again", func() bool {
		f := r.svc().Model().Focus()
		return started.Load() > 0 && f.Session == sess && f.Frame != nil && r.win.Keys.Get("debugState") == "stopped"
	})
}

func TestTheCallStackCommandsMoveTheFocus(t *testing.T) {
	r := newDebugRig(t, debugFolder(t, scriptOf(stopSteps("breakpoint", "23.75")...)))
	r.stopped()
	index := func() int { return r.svc().Model().Focus().Frame.Index }
	for _, tc := range []struct {
		id   string
		want int
	}{{command.DebugCallStackDown, 1}, {command.DebugCallStackUp, 0}, {command.DebugCallStackBottom, 1}, {command.DebugCallStackTop, 0}} {
		r.run(tc.id, nil)
		if got := index(); got != tc.want {
			t.Errorf("%s: the focused frame is %d, want %d", tc.id, got, tc.want)
		}
	}
}

func TestTheWatchAndCopyCommandsWorkOnTheRows(t *testing.T) {
	r := newDebugRig(t, debugFolder(t, scriptOf(joinSteps(stopSteps("breakpoint", "23.75"), []daptest.Step{
		daptest.Reply("evaluate", map[string]any{"result": "23.75", "type": "float64"})})...)))
	r.run(command.ViewContainerDebug, nil)
	r.stopped()
	panes := r.win.shell.debug.panes
	r.waitFor("the variables", func() bool { return panes.Variables.Model().Len() > 0 })
	var sum dbgview.Row
	for _, n := range panes.Variables.Model().Rows() {
		if row, ok := n.Data.(dbgview.Row); ok && row.Variable != nil && row.Variable.Name == "sum" {
			sum = row
		}
	}
	if sum.Variable == nil {
		t.Fatal("the variable sum is not in the view")
	}
	// Add to Watch puts the expression of the variable in the Watch view
	r.run(command.DebugAddToWatch, sum)
	got := r.svc().Model().WatchExpressions()
	if len(got) != 1 || got[0].Name != "sum" {
		t.Fatalf("the watch expressions are %v", got)
	}
	// Copy as Expression and Copy Value put text on the clipboard of the window
	r.run(command.DebugCopyEvaluatePath, sum)
	if text, _ := r.ts.ClipboardText(); text != "sum" {
		t.Errorf("Copy as Expression copied %q", text)
	}
	r.run(command.DebugCopyValue, sum)
	if text, _ := r.ts.ClipboardText(); text != "23.75" {
		t.Errorf("Copy Value copied %q", text)
	}
	r.run(command.DebugRemoveAllWatch, nil)
	if n := len(r.svc().Model().WatchExpressions()); n != 0 {
		t.Errorf("%d watch expressions are left", n)
	}
}

func TestTheBreakpointCommandsChangeTheModel(t *testing.T) {
	r := newDebugRig(t, debugFolder(t, nil))
	path := filepath.Join(r.folder, "main.go")
	if _, err := r.svc().AddBreakpoints(context.Background(), path, []debug.BreakpointData{{Line: 21}, {Line: 12}}); err != nil {
		t.Fatal(err)
	}
	m := r.svc().Model()
	r.run(command.DebugDisableAllBreakpoint, nil)
	for _, b := range m.Breakpoints(debug.BreakpointFilter{}) {
		if b.Enabled {
			t.Errorf("the breakpoint at line %d is still enabled", b.Line)
		}
	}
	r.run(command.DebugEnableAllBreakpoints, nil)
	for _, b := range m.Breakpoints(debug.BreakpointFilter{}) {
		if !b.Enabled {
			t.Errorf("the breakpoint at line %d is not enabled", b.Line)
		}
	}
	r.run(command.DebugToggleActivated, nil)
	r.waitFor("the breakpoints to be deactivated", func() bool { return !m.BreakpointsActivated() })
	if got := r.win.Keys.Get("breakpointsDeactivated"); got != true {
		t.Errorf("breakpointsDeactivated = %v", got)
	}
	r.run(command.DebugToggleActivated, nil)
	r.waitFor("the breakpoints to be activated", func() bool { return m.BreakpointsActivated() })
	r.run(command.DebugAddFunctionBreakpoit, nil)
	r.waitFor("the row of a function breakpoint", func() bool { return len(m.FunctionBreakpoints()) == 1 })
	r.win.shell.debug.panes.Breakpoints.CancelEdit()
	r.run(command.DebugRemoveAllBreakpoints, nil)
	r.waitFor("no breakpoint", func() bool {
		return len(m.Breakpoints(debug.BreakpointFilter{})) == 0 && len(m.FunctionBreakpoints()) == 0
	})
}

func TestTheViewStatesFollowTheModel(t *testing.T) {
	r := newDebugRig(t, debugFolder(t, scriptOf(stopSteps("breakpoint", "23.75")...)))
	r.run(command.ViewContainerDebug, nil)
	keys := r.win.Keys
	idle := map[string]any{"debugState": "inactive", "inDebugMode": false, "debugUx": "default", "breakpointsExist": false, "multiSessionDebug": false}
	for k, want := range idle {
		if got := keys.Get(k); got != want {
			t.Errorf("idle: %s = %v, want %v", k, got, want)
		}
	}
	views := func() []string {
		var ids []string
		for _, v := range r.win.Views.Registry().Views(command.ViewContainerDebug, keys) {
			ids = append(ids, v.ID)
		}
		return ids
	}
	want := []string{command.ViewDebugVariables, command.ViewDebugWatch, command.ViewDebugCallStack, command.ViewDebugBreakpoints}
	if got := views(); !equalStrings(got, want) {
		t.Errorf("the views with a configuration are %v, want %v", got, want)
	}
	r.stopped()
	if keys.Get("inDebugMode") != true || r.win.shell.debug.panes.CallStack.Model().Len() == 0 {
		t.Error("the keys and the call stack do not show the session")
	}
	r.run(command.DebugStopCommand, nil)
	r.waitState("inactive")
	if keys.Get("hasDebugged") != true {
		t.Error("hasDebugged is false after a session")
	}
	if n := r.win.shell.debug.panes.CallStack.Model().Len(); n != 0 {
		t.Errorf("the call stack has %d rows after the session ended", n)
	}
}

func TestTheWelcomeShowsWhileThereIsNoConfiguration(t *testing.T) {
	folder := debugFolder(t, nil)
	if err := os.Remove(filepath.Join(folder, ".emetos", "launch.json")); err != nil {
		t.Fatal(err)
	}
	r := newDebugRigWith(t, folder, rigOpts{})
	r.pump()
	if got := r.win.Keys.Get("debugUx"); got != "simple" {
		t.Errorf("debugUx = %v with no configuration", got)
	}
	for _, v := range r.win.Views.Registry().Views(command.ViewContainerDebug, r.win.Keys) {
		if v.ID != command.ViewDebugWelcome {
			t.Errorf("the view %s is shown with no configuration", v.ID)
		}
	}
}

// A client of a hosted workspace starts a session from its view, and the host's view shows the same session (step 25.4, decision 1166,
// SPEC §10.18): one debug service for the workspace, a set of views and commands for each screen.
func TestAClientStartsASessionFromItsViewAndTheHostShowsIt(t *testing.T) {
	r := newDebugRig(t, debugFolder(t, scriptOf(stopSteps("breakpoint", "23.75")...)))
	r.breakpoint()
	r.run(command.ViewContainerDebug, nil)
	v, err := r.win.makeHeadlessView(extension.Size{Width: 900, Height: 560}, rigFrameRate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(v.Close)
	var frames atomic.Int64
	v.OnFrame(func(*image.RGBA) { frames.Add(1) })
	r.waitFor("the view to draw", func() bool { return frames.Load() > 0 && v.ws.shell != nil && v.ws.shell.debug.panes != nil })
	client := v.ws
	if client.DebugService() != r.svc() {
		t.Fatal("the client's view has a debug service of its own")
	}
	// the commands of the client are its own handlers, not the host's
	if err := client.Commands.Run(command.Context{Ctx: context.Background(), Keys: client.Keys, Source: command.SourceAPI}, command.DebugStartCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.waitFor("the host to stop", func() bool {
		f := r.svc().Model().Focus()
		return f.Frame != nil && r.win.Keys.Get("debugState") == "stopped" && client.Keys.Get("debugState") == "stopped"
	})
	host, cl := r.win.shell.debug.panes, client.shell.debug.panes
	r.waitFor("both call stacks", func() bool { return host.CallStack.Model().Len() > 0 && cl.CallStack.Model().Len() > 0 })
	if h, c := host.CallStack.Model().Len(), cl.CallStack.Model().Len(); h != c {
		t.Errorf("the host's call stack has %d rows and the client's %d", h, c)
	}
	if !r.win.shell.debug.debugging || !client.shell.debug.debugging {
		t.Errorf("the status bars do not both show the session: host %v, client %v", r.win.shell.debug.debugging, client.shell.debug.debugging)
	}
	// the client stops it, and both screens go back
	if err := client.Commands.Run(command.Context{Ctx: context.Background(), Keys: client.Keys, Source: command.SourceAPI}, command.DebugStopCommand, nil); err != nil {
		t.Fatal(err)
	}
	r.waitFor("the end on both screens", func() bool {
		return r.win.Keys.Get("debugState") == "inactive" && client.Keys.Get("debugState") == "inactive"
	})
}

// dbgviewRow is the row of the views of Run and Debug.
type dbgviewRow = dbgview.Row

// removeLaunch removes the launch file of a folder.
func removeLaunch(folder string) error {
	return os.Remove(filepath.Join(folder, ".emetos", "launch.json"))
}

// writeScriptFile writes the script that make gives (the start of the script in from, then its steps) to a file.
func writeScriptFile(t *testing.T, to, from string, make func(daptest.Script) daptest.Script) {
	t.Helper()
	s, err := daptest.LoadScript(from)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := make(s).Lines()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, lines, 0o644); err != nil {
		t.Fatal(err)
	}
}

// routeOf asserts the route of a failure.
func assertRoute(t *testing.T, err error, route string) {
	t.Helper()
	appErr, ok := apperrors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("unexpected failure path: %s, want %s", appErr.Route(), route)
	}
}

func TestAConfigurationWithATaskFailsWhileThereIsNoTaskRunner(t *testing.T) {
	r := newDebugRig(t, debugFolder(t, nil))
	_, err := r.win.debugRunTask(context.Background(), r.folder, debug.TaskRef{Label: "build"})
	assertRoute(t, err, "Workspace.debugRunTask.DebugTaskUnavailable")
	// with a runner, a task that failed asks what to do (debug.onTaskErrors) and the answer decides
	r.win.taskRunner = func(context.Context, string, debug.TaskRef) (bool, error) { return false, nil }
	if res, err := r.win.debugRunTask(context.Background(), r.folder, debug.TaskRef{Label: "build"}); err != nil || res != debug.TaskSuccess {
		t.Errorf("a task that passed: %v %v", res, err)
	}
}

func TestAStartThatSavesUntitledFilesEndsWhenTheDialogIsCancelled(t *testing.T) {
	r := newDebugRig(t, debugFolder(t, nil))
	r.win.SaveDialog = func(context.Context, widgets.BuildContext, SaveDialogOptions) (string, error) { return "", nil }
	r.run("workbench.action.files.newUntitledFile", nil)
	e := r.win.shell.activeEditor()
	if e == nil {
		t.Fatal("no untitled editor")
	}
	e.doc.SetText("x := 1\n")
	done := make(chan error, 1)
	go func() { done <- r.win.debugSaveBeforeStart(context.Background()) }()
	var err error
	r.waitFor("the start to give up", func() bool {
		select {
		case err = <-done:
			return true
		default:
			return false
		}
	})
	assertRoute(t, err, "Workspace.debugSaveBeforeStart.DebugSaveCancelled")
}

type fakeInitial struct{}

func (fakeInitial) InitialConfigurations(context.Context, string, string) ([]debug.Config, error) {
	return []debug.Config{{"name": "Fake", "type": "fake", "request": "launch"}}, nil
}

func TestLaunchJSONIsWrittenAndExtended(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".emetos", "launch.json")
	if err := writeLaunchConfiguration(context.Background(), fakeInitial{}, dir, path, "fake", false); err != nil {
		t.Fatal(err)
	}
	file, problems := debug.ParseLaunch(readFile(t, path))
	if len(problems) != 0 || len(file.Configurations) != 1 || file.Configurations[0].Name() != "Fake" {
		t.Fatalf("the new file is %+v with %v", file, problems)
	}
	// an existing file gets the configuration added at the end
	if err := writeLaunchConfiguration(context.Background(), fakeInitial{}, dir, path, "fake", true); err != nil {
		t.Fatal(err)
	}
	if file, _ := debug.ParseLaunch(readFile(t, path)); len(file.Configurations) != 2 {
		t.Errorf("after adding, %d configurations", len(file.Configurations))
	}
	// a folder where the file cannot be made
	blocked := filepath.Join(dir, "blocked")
	if err := os.WriteFile(blocked, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	err := writeLaunchConfiguration(context.Background(), fakeInitial{}, dir, filepath.Join(blocked, "launch.json"), "fake", false)
	assertRoute(t, err, "writeLaunchConfiguration.DebugLaunchWriteFailed")
	// a file that is not an object cannot take a configuration
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("[1, 2]"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = writeLaunchConfiguration(context.Background(), fakeInitial{}, dir, bad, "fake", true)
	assertRoute(t, err, "writeLaunchConfiguration")
}

func TestTheArgumentsOfTheDevBreakpointCommand(t *testing.T) {
	tests := []struct {
		name string
		args any
		ok   bool
	}{
		{"valid", map[string]any{"path": "main.go", "line": 3.0}, true},
		{"no object", "main.go", false},
		{"no line", map[string]any{"path": "main.go"}, false},
		{"no path", map[string]any{"line": 2.0}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := devBreakpointArgs(tc.args)
			if tc.ok != (err == nil) {
				t.Fatalf("err = %v", err)
			}
			if !tc.ok {
				assertRoute(t, err, "devBreakpointArgs.DevDebugBreakpointInvalid")
			}
		})
	}
}

func TestShellQuoteAndTheCommandLineOfARunInTerminalRequest(t *testing.T) {
	tests := []struct{ in, want string }{{"abc", "abc"}, {"a b", "'a b'"}, {"it's", `'it'\''s'`}, {"", "''"}, {"x=1", "x=1"}}
	for _, tc := range tests {
		if got := shellQuote(tc.in); got != tc.want {
			t.Errorf("shellQuote(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// sum is the value of the variable sum in the Variables view, or "" while it is not shown.
func (r *debugRig) sum() string {
	for _, n := range r.win.shell.debug.panes.Variables.Model().Rows() {
		if row, ok := n.Data.(dbgview.Row); ok && row.Variable != nil && row.Variable.Name == "sum" {
			return row.Variable.Value()
		}
	}
	return ""
}
