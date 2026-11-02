//go:build emetos_perf

package app

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/ui"
	"golang.org/x/sys/unix"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
)

// The measurement of the Variables view with a variable of 10,000 children (step 25.12, SPEC §14, docs/PERFORMANCE.md "Run and Debug"): the
// adapter answers the expansion of `order` with 10,000 variables in one reply (no paging, the worst case for the view; with paging the
// view asks for chunks of 100). The times are the UI thread's processor time of build, layout and paint with the fake platform.
//
//	scripts/heavy.sh --alone -- go test -tags 'emetos_perf glutter_release' ./internal/gui/app -run TestPerfVariables -v -count=1

func threadCPU(t testing.TB) time.Duration {
	t.Helper()
	var ru unix.Rusage
	if err := unix.Getrusage(unix.RUSAGE_THREAD, &ru); err != nil {
		t.Fatalf("Getrusage: %v", err)
	}
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

func TestPerfVariablesTenThousandChildren(t *testing.T) {
	const n = 10_000
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	children := make([]map[string]any, n)
	for i := range children {
		children[i] = map[string]any{"name": fmt.Sprintf("[%d]", i), "value": fmt.Sprintf("%d", i*3), "type": "int", "evaluateName": fmt.Sprintf("order.Items[%d]", i)}
	}
	script := scriptOf(joinSteps(stopSteps("breakpoint", "23.75"), []daptest.Step{daptest.Reply("variables", map[string]any{"variables": children})})...)
	r := newDebugRigWith(t, debugFolder(t, script), rigOpts{open: []string{"main.go"}, size: ui.Size{Width: 1280, Height: 800}})
	r.run(command.ViewContainerDebug, nil)
	r.stopped()
	panes := r.win.shell.debug.panes
	r.waitFor("the variables", func() bool { return panes.Variables.Model().Len() > 0 })
	r.settle()

	frame := func() time.Duration {
		s := threadCPU(t)
		r.ts.Pump(16 * time.Millisecond)
		return threadCPU(t) - s
	}
	var key string
	for _, node := range panes.Variables.Model().Rows() {
		if row, ok := node.Data.(dbgviewRow); ok && row.Variable != nil && row.Variable.Name == "order" {
			key = node.Key
		}
	}
	if key == "" {
		t.Fatal("no variable order")
	}
	runtime.GC()
	start := time.Now()
	panes.Variables.Model().Expand(key)
	for panes.Variables.Model().Len() < n { // no frame is pumped meanwhile: the first one is measured below
		r.ts.FlushPosted()
		time.Sleep(time.Millisecond)
	}
	t.Logf("expanding a variable with %d children: %v of wall time until the model has the rows (the request, the reply of %d variables and their wrapping)", n, time.Since(start), n)
	t.Logf("the first frame after the rows: %v", frame())
	var total, worst time.Duration
	const frames = 60
	at := ui.Offset{Dx: 100, Dy: 200}
	for range frames {
		r.ts.ScrollAt(at, ui.Offset{Dy: 240})
		d := frame()
		total += d
		worst = max(worst, d)
	}
	t.Logf("%d frames of a steady scroll: average %v, longest %v", frames, total/frames, worst)
	t.Logf("a frame with nothing changed: %v", frame())
	s := time.Now()
	panes.Variables.Model().Collapse(key)
	r.pump()
	t.Logf("collapsing it again: %v of wall time, %d rows left", time.Since(s), panes.Variables.Model().Len())
}
