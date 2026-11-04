//go:build emetos_perf

package debug

import (
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	coredebug "github.com/rm4n0s/emetos/internal/core/debug"
)

// The measurement of the Debug Console with 100,000 lines of output (step 25.12, SPEC §14, docs/PERFORMANCE.md "Run and Debug"): the time to
// take the lines into the model, the UI thread's first frame after them, the frames of a steady scroll and of jumps to the top and the end,
// the heap the lines cost, and the filter. A frame's time is the processor time of the UI thread (build, layout and paint, with the fake
// platform: no renderer), which a busy machine does not stretch.
//
//	scripts/heavy.sh --alone -- go test -tags 'emetos_perf glutter_release' ./internal/gui/views/debug -run TestPerfConsole -v -count=1

func TestPerfConsoleHundredThousandLines(t *testing.T) {
	const n = 100_000
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r := newConsoleRig(t, consoleRigOpts{script: scriptOf(fullCaps), size: ui.Size{Width: 900, Height: 560}})
	sess := r.con.Session()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)

	start := time.Now()
	for i := range n {
		sess.AppendToRepl("line "+strconv.Itoa(i)+" of the output of the program, \x1b[32mwith color\x1b[0m and some more text\n", coredebug.ReplInfo)
	}
	for r.con.Len() < n { // no frame is pumped meanwhile: the first one is measured below
		r.drain()
		time.Sleep(time.Millisecond)
	}
	t.Logf("%d lines taken into the model in %v (wall, including the wait on the UI queue)", n, time.Since(start))
	runtime.GC()
	runtime.ReadMemStats(&after)
	t.Logf("heap after the lines: %.1f MB more", float64(after.HeapAlloc-before.HeapAlloc)/(1<<20))

	frame := func() time.Duration {
		s := threadCPU(t)
		r.ts.Pump(16 * time.Millisecond)
		return threadCPU(t) - s
	}
	t.Logf("the first frame after the lines: %v", frame())
	var total, worst time.Duration
	const frames = 60
	at := ui.Offset{Dx: 300, Dy: 200}
	for range frames {
		r.ts.ScrollAt(at, ui.Offset{Dy: -240})
		d := frame()
		total += d
		worst = max(worst, d)
	}
	t.Logf("60 frames of a steady scroll up: average %v, longest %v", total/frames, worst)
	total, worst = 0, 0
	for range frames {
		r.ts.ScrollAt(at, ui.Offset{Dy: 240})
		d := frame()
		total += d
		worst = max(worst, d)
	}
	t.Logf("60 frames of a steady scroll down: average %v, longest %v", total/frames, worst)
	if !r.ts.Exists(gluttertest.ByText("line 99999 of the output of the program, with color and some more text", gluttertest.FindOpts{FindRichText: true})) {
		t.Log("the end of the console is not in view after the scroll down (the lines scrolled are 60 x 240 px)")
	}

	// the filter: the lines are matched on a goroutine after the delay; the UI thread's frames meanwhile and the one that swaps the rows
	// in are measured
	s := time.Now()
	cpu0 := threadCPU(t)
	r.con.SetFilter("line 5")
	var worstFrame time.Duration
	for r.con.Len() == n {
		r.drain()
		worstFrame = max(worstFrame, frame())
		time.Sleep(2 * time.Millisecond)
	}
	r.drain()
	worstFrame = max(worstFrame, frame())
	t.Logf("a filter over %d lines: %v to the rows (%d match); the longest UI-thread frame meanwhile %v; UI-thread time of the whole: %v", n, time.Since(s), r.con.Len(), worstFrame, threadCPU(t)-cpu0)
	r.con.SetFilter("")
	r.pump()

	// a live program: lines arrive while the console is on screen
	var lateLines time.Duration
	for i := range 20 {
		for j := range 1000 {
			sess.AppendToRepl("live "+strconv.Itoa(i*1000+j)+"\n", coredebug.ReplInfo)
		}
		r.drain()
		lateLines = max(lateLines, frame())
	}
	t.Logf("20 frames while 1,000 lines arrive in each: the longest %v", lateLines)
}
