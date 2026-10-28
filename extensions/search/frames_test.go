package search

import (
	"fmt"
	"runtime"
	"sort"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// streamAttempts is how many times the test runs the stream before it gives up on getting under the budget.
const streamAttempts = 5

// threadCPU is the CPU time that the calling OS thread has used; the caller holds its thread (runtime.LockOSThread). Unlike
// the wall clock it does not grow while the scheduler gives the thread's core to another process.
func threadCPU(t *testing.T) time.Duration {
	t.Helper()
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_THREAD_CPUTIME_ID, &ts); err != nil {
		t.Fatalf("reading the CPU time of the thread: %v", err)
	}
	return time.Duration(ts.Nano())
}

// referenceWork is a fixed piece of work of the same kind as a turn of the view (it allocates, builds a map and sorts) and
// does not use the code under test, timed by the CPU clock of the thread. On a quiet machine it takes about referenceQuiet.
// When it takes longer, the machine is slower than the one the budget was set on (busy with other builds, whose caches and
// memory bus it shares, or old), and a turn is given as much more time.
func referenceWork(t *testing.T) time.Duration {
	t.Helper()
	start := threadCPU(t)
	type row struct {
		key  string
		next *row
		n    int
	}
	rows := make([]*row, 0, 40000)
	byKey := map[string]*row{}
	for i := range 40000 {
		r := &row{key: "d" + strconv.Itoa(i%997) + "/f" + strconv.Itoa(i), n: i}
		rows = append(rows, r)
		byKey[r.key] = r
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].key < rows[j].key })
	for i := 1; i < len(rows); i++ {
		rows[i-1].next = rows[i]
	}
	if len(byKey) != len(rows) {
		t.Fatal("the reference work lost a row")
	}
	return threadCPU(t) - start
}

// TestTheUIThreadStaysUnderAFrameWhileAHugeSearchStreams holds the row "Search" of SPEC §14: while the results of a search of 20,000
// files stream in (the engine hands over a file at a time, as fast as it finds them), no turn of the UI thread (what the
// extension does when it takes the batch, and the frame that shows the view) takes more than 16 ms.
//
// A machine that is busy with other builds slows every turn two to six times (the core is taken away in the middle of it,
// the caches and the memory bus are shared), so the test measures the work of the code, not the load, in three ways:
//   - a turn is timed by the CPU clock of its OS thread, not by the wall clock;
//   - the same stream runs up to streamAttempts times and the test fails only when the best run is over the budget: load
//     only adds to a time, so the best run is the closest to the work of the code, and a turn that really does too much
//     (all the results in one turn, the tree made again for every file) is over the budget in every run;
//   - a piece of work that does not use the code under test (referenceWork) is timed in each run, and when it takes longer
//     than on a quiet machine, so many times more is allowed for the turns of that run (never less than the 16 ms).
//
// On a quiet machine this is the plain budget of 16 ms of CPU time (the longest turn is 9 to 12 ms). Under the race detector
// the work is about four times slower, which the budget and the reference take in account.
func TestTheUIThreadStaysUnderAFrameWhileAHugeSearchStreams(t *testing.T) {
	if testing.Short() {
		t.Skip("a long run")
	}
	slower := time.Duration(1) // the race detector slows the work of the view and of the reference alike
	if raceEnabled {
		slower = 4
	}
	budget, referenceQuiet := 16*time.Millisecond*slower, 10*time.Millisecond*slower
	runtime.LockOSThread() // the turns are timed by the CPU clock of this thread
	defer runtime.UnlockOSThread()

	bestRatio, bestWhat, bestWorst, bestLimit := 0.0, "", time.Duration(0), time.Duration(0)
	for attempt := 1; attempt <= streamAttempts; attempt++ {
		ref := referenceWork(t)
		limit := budget
		if ref > referenceQuiet { // the machine is slow now: a turn gets as much more as the reference did
			limit = time.Duration(float64(budget) * float64(ref) / float64(referenceQuiet))
		}
		worst, what, turns := streamHugeSearch(t)
		ratio := float64(worst) / float64(limit)
		t.Logf("run %d: %d turns of the UI thread, the longest %v (%s), the limit %v (reference work %v, quiet %v)",
			attempt, turns, worst, what, limit, ref, referenceQuiet)
		if attempt == 1 || ratio < bestRatio {
			bestRatio, bestWhat, bestWorst, bestLimit = ratio, what, worst, limit
		}
		if bestRatio <= 1 {
			return
		}
	}
	t.Errorf("a turn of the UI thread (%s) took %v of CPU time in the best of %d runs, the limit of that run is %v (budget %v)",
		bestWhat, bestWorst, streamAttempts, bestLimit, budget)
}

// streamHugeSearch streams 20,000 matches in 2,000 files into a new view, as fast as the engine can, while the test's thread
// plays the UI thread, and returns the longest turn it took (by CPU time), what the turn was, and the number of turns.
func streamHugeSearch(t *testing.T) (worst time.Duration, what string, turns int) {
	t.Helper()
	const files, perFile = 2000, 10 // 20,000 matches: search.maxResults
	r := newRig(t, project)
	ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 340, Height: 400}, widgetOf(r))
	r.ext.searchBox.SetValue("needle")
	r.ext.status = searchStatus{running: true}
	r.ext.gen++
	gen := r.ext.gen

	done := make(chan struct{})
	go func() { // the engine: a file at a time, as fast as it can
		defer close(done)
		for i := range files {
			f := extension.TextSearchFile{Path: fmt.Sprintf("d%03d/src/f%05d.txt", i/10, i), URI: "file:///work/f" + strconv.Itoa(i)}
			for j := range perFile {
				f.Matches = append(f.Matches, extension.TextSearchMatch{
					Range:   extension.Range{Start: extension.Position{Line: j, Col: 4}, End: extension.Position{Line: j, Col: 10}},
					Preview: "the needle is here", PreviewStart: 4, PreviewEnd: 10, Text: "needle",
				})
			}
			r.ext.push(gen, f)
		}
	}()

	measure := func(name string, f func()) {
		start := threadCPU(t)
		f()
		if d := threadCPU(t) - start; d > worst {
			worst, what = d, name
		}
		turns++
	}
	for finished := false; !finished; {
		measure("the extension's turn", func() { r.api.Pump() })
		measure("the frame", func() { ts.Pump(16 * time.Millisecond) })
		select {
		case <-done:
			measure("the end", func() { r.ext.finish(gen, extension.TextSearchSummary{}, nil) })
			measure("the last frame", func() { ts.Pump(16 * time.Millisecond) })
			finished = true
		default:
		}
	}
	for range 40 { // the batches that are still on their way
		r.api.Pump()
		time.Sleep(time.Millisecond)
	}
	matches, nfiles := r.ext.res.counts()
	if matches != files*perFile || nfiles != files {
		t.Errorf("the results have %d matches in %d files, want %d in %d", matches, nfiles, files*perFile, files)
	}
	return worst, what, turns
}

// countingAPI counts how often the extension asks the UI thread for something.
type countingAPI struct {
	extension.API
	posts atomic.Int32
}

// Post implements extension.API.
func (c *countingAPI) Post(fn func()) {
	c.posts.Add(1)
	c.API.Post(fn)
}

// TestTheEngineAsksTheUIThreadOnceForAllItsFilesAndWaitsBetweenAsks holds decision 158 without a clock to measure: whatever
// the engine found since the UI thread last looked reaches it with one ask, and after a take the engine asks again only when
// deliverGap has passed, so the work of the UI thread per second does not grow with the speed of the search.
func TestTheEngineAsksTheUIThreadOnceForAllItsFilesAndWaitsBetweenAsks(t *testing.T) {
	r := newRig(t, project)
	counter := &countingAPI{API: r.api}
	r.ext.api = counter
	r.ext.searchBox.SetValue("needle")
	r.ext.status = searchStatus{running: true}
	r.ext.gen++
	gen := r.ext.gen
	push := func(i int) {
		r.ext.push(gen, extension.TextSearchFile{Path: fmt.Sprintf("f%05d.txt", i), URI: "file:///work/f" + strconv.Itoa(i),
			Matches: []extension.TextSearchMatch{{Preview: "needle", PreviewEnd: 6, Text: "needle"}}})
	}
	for i := range 1000 {
		push(i)
	}
	if n := counter.posts.Load(); n != 1 {
		t.Errorf("the engine asked the UI thread %d times for 1000 files that came before the UI thread looked, want once", n)
	}
	took := time.Now()
	if !r.api.Pump() {
		t.Fatal("the engine did not ask the UI thread for its files")
	}
	if _, files := r.ext.res.counts(); files != 1000 {
		t.Errorf("one take has %d files, want all the 1000 that the engine found before it", files)
	}
	for i := 1000; i < 1500; i++ {
		push(i)
	}
	if time.Since(took) < deliverGap/2 { // on a machine so busy that the gap passed already, there is nothing to hold
		if n := counter.posts.Load(); n != 1 {
			t.Errorf("the engine asked the UI thread %d times within %v of a take, the gap is %v, want no new ask", n, time.Since(took), deliverGap)
		}
	}
	r.api.WaitFor("the second take", func() bool { _, files := r.ext.res.counts(); return files == 1500 })
}
