package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/emetos/internal/core/debug"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The goldens of the Run and Debug view (step 25.4), dark and light: before a session (with configurations, and the welcome without), stopped
// with variables open and a value that changed, the call stack of several threads and a child session, the Breakpoints, the toolbar floating
// and docked, and the status bar while a session runs. The adapter is the fake one of daptest, through the dev debugger.

var debugGoldenSize = ui.Size{Width: 900, Height: 640}

// goldenDebug makes a rig of the theme over main.go.
func goldenDebug(t *testing.T, theme string, folder string, settings map[string]any) *debugRig {
	t.Helper()
	return newDebugRigWith(t, folder, rigOpts{theme: theme, size: debugGoldenSize, open: []string{"main.go"}, settings: settings})
}

// settle lets the delayed parts of a stop (the rest of the stack, the pass of the focus) happen and the window draw.
func (r *debugRig) settle() {
	for range 6 {
		r.ts.FlushPosted()
		r.ts.Pump(100 * time.Millisecond)
	}
	r.pump()
}

// foldViews folds the views of the container.
func (r *debugRig) foldViews(ids ...string) {
	for _, id := range ids {
		r.win.Views.SetCollapsed(id, true)
	}
	r.pump()
}

func TestGoldenRunAndDebugBeforeASession(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name+" with configurations", func(t *testing.T) {
			r := goldenDebug(t, th.theme, debugFolder(t, nil), nil)
			r.run(command.ViewContainerDebug, nil)
			r.settle()
			testutil.MatchesGolden(r.ts, "run_debug_idle_"+th.name)
			if errs := r.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
		t.Run(th.name+" the welcome", func(t *testing.T) {
			folder := debugFolder(t, nil)
			if err := removeLaunch(folder); err != nil {
				t.Fatal(err)
			}
			r := goldenDebug(t, th.theme, folder, nil)
			r.run(command.ViewContainerDebug, nil)
			r.settle()
			testutil.MatchesGolden(r.ts, "run_debug_welcome_"+th.name)
			if errs := r.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

func TestGoldenRunAndDebugStopped(t *testing.T) {
	script := scriptOf(joinSteps(stopSteps("breakpoint", "23.75"), []daptest.Step{orderChildren(), daptest.Reply("next", nil)},
		stopSteps("step", "47.5"), []daptest.Step{orderChildren()})...)
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name, func(t *testing.T) {
			r := goldenDebug(t, th.theme, debugFolder(t, script), nil)
			r.run(command.ViewContainerDebug, nil)
			r.stopped()
			panes := r.win.shell.debug.panes
			r.waitFor("the variables", func() bool { return panes.Variables.Model().Len() > 0 })
			// open the variable order: its children come from the adapter
			for _, n := range panes.Variables.Model().Rows() {
				if row, ok := n.Data.(dbgviewRow); ok && row.Variable != nil && row.Variable.Name == "order" {
					panes.Variables.Model().Expand(n.Key)
				}
			}
			r.waitFor("the children of order", func() bool { return panes.Variables.Model().Len() >= 7 })
			// a step: the value of sum changes
			r.run(command.DebugStepOverCommand, nil)
			r.waitFor("the second stop", func() bool { return r.sum() == "47.5" && panes.Variables.Model().Len() >= 7 })
			r.settle()
			testutil.MatchesGolden(r.ts, "run_debug_stopped_"+th.name)
			if errs := r.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

func TestGoldenRunAndDebugCallStackWithThreadsAndAChildSession(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name, func(t *testing.T) {
			parent := scriptOf(joinSteps(stopSteps("breakpoint", "23.75"), []daptest.Step{
				daptest.Request("child", "startDebugging", map[string]any{"request": "launch", "configuration": map[string]any{
					"type": "dev.debug.fake", "name": "Worker pool", "script": "${workspaceFolder}/child.jsonl"}}),
				daptest.Await("child")})...)
			folder := debugFolder(t, parent)
			child := scriptOf(
				daptest.Event("stopped", map[string]any{"reason": "pause", "threadId": 2, "allThreadsStopped": false}),
				daptest.Reply("threads", threadsBody),
				daptest.Reply("stackTrace", map[string]any{"totalFrames": 1, "stackFrames": []map[string]any{
					{"id": 301, "name": "main.work", "line": 12, "column": 4, "source": map[string]any{"name": "main.go", "path": "${workspaceFolder}/main.go"}}}}),
			)
			writeScriptFile(t, filepath.Join(folder, "child.jsonl"), filepath.Join(folder, "fake.jsonl"), child)
			r := goldenDebug(t, th.theme, folder, nil)
			r.run(command.ViewContainerDebug, nil)
			r.foldViews(command.ViewDebugVariables, command.ViewDebugWatch, command.ViewDebugBreakpoints)
			r.stopped()
			r.waitFor("the child session", func() bool { return len(r.svc().Model().Sessions(false)) == 2 })
			r.win.shell.debug.panes.CallStack.Model().ExpandAll()
			r.settle()
			testutil.MatchesGolden(r.ts, "run_debug_callstack_"+th.name)
			if errs := r.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

func TestGoldenRunAndDebugBreakpoints(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name, func(t *testing.T) {
			r := goldenDebug(t, th.theme, debugFolder(t, nil), nil)
			ctx := context.Background()
			bps, err := r.svc().AddBreakpoints(ctx, filepath.Join(r.folder, "main.go"), []debug.BreakpointData{{Line: 21}, {Line: 14, Condition: "p > 4"}, {Line: 16, Disabled: true}})
			if err != nil || len(bps) != 3 {
				t.Fatalf("breakpoints: %v %v", bps, err)
			}
			if _, err := r.svc().AddFunctionBreakpoint(ctx, debug.FunctionBreakpointOptions{Name: "total"}); err != nil {
				t.Fatal(err)
			}
			r.run(command.ViewContainerDebug, nil)
			r.foldViews(command.ViewDebugVariables, command.ViewDebugWatch, command.ViewDebugCallStack)
			r.settle()
			testutil.MatchesGolden(r.ts, "run_debug_breakpoints_"+th.name)
			if errs := r.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

func TestGoldenRunAndDebugToolbarAndStatusBar(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		for _, place := range []string{"floating", "docked"} {
			t.Run(th.name+" "+place, func(t *testing.T) {
				r := goldenDebug(t, th.theme, debugFolder(t, scriptOf(stopSteps("breakpoint", "23.75")...)), map[string]any{"debug.toolBarLocation": place})
				r.run(command.ViewContainerDebug, nil)
				r.stopped()
				r.settle()
				testutil.MatchesGolden(r.ts, "run_debug_toolbar_"+place+"_"+th.name)
				if errs := r.ts.TakeErrors(); len(errs) != 0 {
					t.Errorf("errors: %v", errs)
				}
			})
		}
		t.Run(th.name+" the status bar", func(t *testing.T) {
			r := goldenDebug(t, th.theme, debugFolder(t, scriptOf(stopSteps("breakpoint", "23.75")...)), map[string]any{"debug.toolBarLocation": "hidden"})
			r.stopped()
			r.settle()
			if !r.win.shell.debug.debugging {
				t.Fatal("the status bar is not in the colors of a session")
			}
			testutil.MatchesGolden(r.ts, "run_debug_statusbar_"+th.name)
			r.run(command.DebugStopCommand, nil)
			r.waitState("inactive")
			r.settle()
			testutil.MatchesGolden(r.ts, "run_debug_statusbar_ended_"+th.name)
		})
	}
}
