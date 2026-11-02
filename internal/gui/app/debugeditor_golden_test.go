package app

import (
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The goldens of the debugger in the editor of the window (step 25.5), dark and light: a stop on an exception (the breakpoints, the stopped
// line, the inline values, the exception widget), and the debug hover with a tree. The goldens of each mark alone are the editor's.

func TestGoldenDebugStoppedOnAnException(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name, func(t *testing.T) {
			r, e := stoppedOnException(t, rigOpts{theme: th.theme, size: debugGoldenSize})
			r.waitFor("the exception widget and the values", func() bool { return e.view.DebugExceptionOpen() && len(e.view.InlineValues()) > 0 })
			r.settle()
			testutil.MatchesGolden(r.ts, "editor_debug_exception_"+th.name)
			if errs := r.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

func TestGoldenDebugHoverWithATree(t *testing.T) {
	script := scriptOf(joinSteps(stopSteps("breakpoint", "23.75"), []daptest.Step{
		daptest.Reply("evaluate", map[string]any{"result": "[]float64 len:3", "type": "[]float64", "variablesReference": 1005}),
		daptest.Reply("variables", map[string]any{"variables": []map[string]any{
			{"name": "[0]", "value": "4.5", "type": "float64"},
			{"name": "[1]", "value": "12", "type": "float64"},
			{"name": "[2]", "value": "7.25", "type": "float64"},
		}}),
	})...)
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name, func(t *testing.T) {
			r := newDebugRigWith(t, debugFolder(t, script), rigOpts{theme: th.theme, size: debugGoldenSize, open: []string{"main.go"}})
			r.stopped()
			e := r.mainEditor()
			r.waitFor("the editor to be stopped", func() bool { return len(e.view.DebugFrames()) == 1 })
			// the pointer rests on `prices` in line 20
			e.view.DebugShowHover(buffer.Position{Line: 19, Col: 4})
			r.waitFor("the hover", func() bool { return e.view.DebugHover() != nil })
			r.settle()
			if h := e.view.DebugHover(); h == nil || !h.Complex {
				t.Fatalf("hover = %+v, want a tree", h)
			}
			testutil.MatchesGolden(r.ts, "editor_debug_hover_"+th.name)
			if errs := r.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}
