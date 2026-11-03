//go:build emetos_perf

package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/command"
	testview "github.com/rm4n0s/emetos/internal/gui/views/testing"
)

// The measurement of Testing with a Go module of 5,000 tests (step 26.10, SPEC §14, docs/PERFORMANCE.md "Testing"): the discovery of the
// Go extension, a full run with the real go while the results stream into the open Test Explorer, and the Explorer's frames while the
// 5,000 rows scroll. The module is made in a temporary folder: 5 packages of 10 files of 100 tests, one test in 250 fails. The times
// are the UI thread's processor time of build, layout and paint with the fake platform, as in the other view measurements.
//
//	scripts/heavy.sh --alone -- go test -tags 'emetos_perf glutter_release' ./internal/gui/app -run TestPerfTesting -v -count=1

const (
	perfPackages = 5
	perfFiles    = 10
	perfTests    = 100
)

// writePerfModule makes the module of perfPackages*perfFiles*perfTests tests.
func writePerfModule(t testing.TB, folder string) {
	t.Helper()
	write := func(name, text string) {
		path := filepath.Join(folder, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/big\n\ngo 1.21\n")
	n := 0
	for p := range perfPackages {
		for f := range perfFiles {
			var b strings.Builder
			fmt.Fprintf(&b, "package p%d\n\nimport \"testing\"\n", p)
			for i := range perfTests {
				if n%250 == 249 {
					fmt.Fprintf(&b, "\nfunc TestF%dN%d(t *testing.T) {\n\tt.Errorf(\"got %%d, want %%d\", %d, %d)\n}\n", f, i, n, n+1)
				} else {
					fmt.Fprintf(&b, "\nfunc TestF%dN%d(t *testing.T) {\n\tif %d+1 != %d {\n\t\tt.Fatal(\"arithmetic\")\n\t}\n}\n", f, i, n, n+1)
				}
				n++
			}
			write(fmt.Sprintf("p%d/f%d_test.go", p, f), b.String())
		}
	}
}

// frameStats describes the processor times of frames.
func frameStats(d []time.Duration) string {
	if len(d) == 0 {
		return "no frames"
	}
	s := slices.Clone(d)
	slices.Sort(s)
	var total time.Duration
	over := 0
	for _, x := range s {
		total += x
		if x > 16*time.Millisecond {
			over++
		}
	}
	return fmt.Sprintf("%d frames: average %v, median %v, p90 %v, longest %v, %d over 16 ms", len(s), total/time.Duration(len(s)), s[len(s)/2],
		s[len(s)*9/10], s[len(s)-1], over)
}

func TestPerfTestingFiveThousandGoTests(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	const tests = perfPackages * perfFiles * perfTests
	r := newRealGoRigIn(t, func(folder string) { writePerfModule(t, folder) }, 300*time.Second, nil)
	svc := r.win.TestingService()
	win := r.win

	// the discovery: the Go extension was activated at the end of newRealGoRigIn, which returns when the tree has items; the time to the
	// complete tree is measured from a Refresh Tests
	run := func(id string) {
		if err := win.Commands.Run(command.Context{Ctx: t.Context(), Keys: win.Keys, Source: command.SourceAPI}, id, nil); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	wantItems := tests + perfPackages + perfPackages*perfFiles
	r.waitFor("the whole tree", func() bool { return svc.ItemCount("go") >= wantItems })
	t.Logf("discovery at the start (the extension activated, the tree made from the workspace's file list): %v of wall time to the whole tree of %d items", time.Since(r.activated), svc.ItemCount("go"))
	run(command.ViewContainerTesting)
	r.waitFor("the Test Explorer", func() bool { return win.shell.testing.explorer != nil && win.shell.testing.explorer.Model().Len() > 0 })
	runtime.GC()
	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- svc.RefreshTests(t.Context(), "go") }()
	var discovery []time.Duration
refresh:
	for {
		s := threadCPU(t)
		r.ts.FlushPosted()
		r.ts.Pump(16 * time.Millisecond)
		discovery = append(discovery, threadCPU(t)-s)
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			break refresh
		default:
			time.Sleep(4 * time.Millisecond)
		}
	}
	t.Logf("Refresh Tests (every _test.go file read again with go/parser, unchanged files keep their items): %v of wall time, %d items; the UI thread meanwhile: %s",
		time.Since(start), svc.ItemCount("go"), frameStats(discovery))

	// the Explorer's frames while the rows scroll: as a list (5,000 rows) and as a tree with everything expanded (5,055 rows)
	ex := win.shell.testing.explorer
	r.ts.Pump(16 * time.Millisecond)
	scroll := func(what string) {
		frame := func() time.Duration {
			s := threadCPU(t)
			r.ts.FlushPosted()
			r.ts.Pump(16 * time.Millisecond)
			return threadCPU(t) - s
		}
		t.Logf("%s: %d rows; the first frame after the change: %v", what, ex.Model().Len(), frame())
		var frames []time.Duration
		at := ui.Offset{Dx: 150, Dy: 250}
		for range 60 {
			r.ts.ScrollAt(at, ui.Offset{Dy: 240})
			frames = append(frames, frame())
		}
		t.Logf("%s, a steady scroll: %s", what, frameStats(frames))
		var idle []time.Duration
		for range 10 {
			idle = append(idle, frame())
		}
		t.Logf("%s, nothing changed: %s", what, frameStats(idle))
	}
	ex.SetMode(testview.ModeList)
	r.ts.Pump(16 * time.Millisecond)
	scroll("the list")
	ex.SetMode(testview.ModeTree)
	ex.Model().ExpandAll()
	r.ts.Pump(16 * time.Millisecond)
	scroll("the tree, expanded")
	ex.Model().CollapseAll()
	r.ts.Pump(80 * time.Millisecond)

	// a full run (Run All Tests) with the tree collapsed to the packages, then with the Explorer as a list of 5,000 rows
	fullRun(t, r, "a full run of 5000 tests with the real go (the tree collapsed to the packages)", 1)
	ex.SetMode(testview.ModeList)
	r.ts.Pump(80 * time.Millisecond)
	fullRun(t, r, "a second full run with the Explorer as a list (5000 rows)", 2)
}

// fullRun runs everything (Run All Tests) and logs every frame of the run on the UI thread, at 60 frames per second; results is the
// number of results there are when the run has ended.
func fullRun(t *testing.T, r *realGoRig, what string, results int) {
	t.Helper()
	win := r.win
	svc := win.TestingService()
	if err := win.Commands.Run(command.Context{Ctx: t.Context(), Keys: win.Keys, Source: command.SourceAPI}, command.TestingRunAll, nil); err != nil {
		t.Fatal(err)
	}
	var frames []time.Duration
	var cpu time.Duration
	runStart := time.Now()
	for {
		frameStart := time.Now()
		s := threadCPU(t)
		r.ts.FlushPosted()
		r.ts.Pump(16 * time.Millisecond)
		d := threadCPU(t) - s
		frames = append(frames, d)
		cpu += d
		rs := svc.Results()
		if len(rs) >= results && rs[0].IsComplete() && !svc.IsRunning() {
			break
		}
		if time.Since(runStart) > 280*time.Second {
			t.Fatal("the run did not end")
		}
		if rest := 16*time.Millisecond - time.Since(frameStart); rest > 0 {
			time.Sleep(rest)
		}
	}
	t.Logf("%s: %v of wall time; the UI thread: %v of processor time in all, %s", what, time.Since(runStart), cpu, frameStats(frames))
	res := svc.Results()[0]
	counts := map[string]int{}
	for _, tr := range res.Tests() {
		counts[tr.State.String()]++
	}
	var outBytes int
	for _, task := range res.Tasks() {
		out, _ := res.Output(task.ID)
		outBytes += len(out)
	}
	t.Logf("the result: %v, %d bytes of output", counts, outBytes)
}

// The same full run with Test Results kept closed (testing.openTesting is neverOpen): what the run costs the UI thread without the pane
// that shows the output of a result.
func TestPerfTestingFullRunWithTestResultsClosed(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	r := newRealGoRigIn(t, func(folder string) { writePerfModule(t, folder) }, 300*time.Second, map[string]any{"testing.openTesting": "neverOpen"})
	r.waitFor("the whole tree", func() bool { return r.win.TestingService().ItemCount("go") >= perfPackages*perfFiles*perfTests })
	fullRun(t, r, "a full run of 5000 tests with Test Results closed", 1)
}
