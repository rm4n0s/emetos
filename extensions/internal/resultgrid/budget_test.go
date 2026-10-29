package resultgrid

import (
	"runtime"
	"sort"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// budgetAttempts is how many times a budget test runs before it gives up on getting under its budget.
const budgetAttempts = 5

// The budgets (docs/PERFORMANCE.md "Result grid (step 19.1)"): a frame of the grid while it scrolls through 100,000 rows stays under
// a frame of a 60 Hz display, the first frame of a result of that size appears before the next one, and a sort and a copy of the
// whole result do not freeze the window for more than a second.
const (
	frameBudget  = 16 * time.Millisecond
	firstBudget  = 100 * time.Millisecond
	sortBudget   = time.Second
	copyBudget   = 2 * time.Second
	referenceCPU = 10 * time.Millisecond // what referenceWork takes on the machine the budgets were set on
)

// threadCPU is the CPU time that the calling OS thread has used; the caller holds its thread (runtime.LockOSThread). Unlike the wall
// clock it does not grow while the scheduler gives the thread's core to another process.
func threadCPU(t testing.TB) time.Duration {
	t.Helper()
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_THREAD_CPUTIME_ID, &ts); err != nil {
		t.Fatalf("reading the CPU time of the thread: %v", err)
	}
	return time.Duration(ts.Nano())
}

// referenceWork is a fixed piece of work of the same kind as a frame of the grid (it allocates, builds a map and sorts) that does not
// use the code under test, timed by the CPU clock of the thread. When it takes longer than referenceCPU, the machine is slower than the
// one the budgets were set on (busy with other builds, which share its caches and memory bus), and a measurement is given as much
// more time.
func referenceWork(t testing.TB) time.Duration {
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

// withinBudget runs measure up to budgetAttempts times and passes when the best run is within the budget, scaled by how slow the
// machine is in that run (the reference work) and by the race detector. Load only adds to a time, so the best run is the closest to
// the work of the code; a budget that the code really overruns is overrun in every run.
func withinBudget(t *testing.T, what string, budget time.Duration, measure func() time.Duration) {
	t.Helper()
	slower := time.Duration(1)
	if raceEnabled {
		slower = 5 // the race detector slows the work of the code and of the reference alike
	}
	runtime.LockOSThread() // the work is timed by the CPU clock of this thread
	defer runtime.UnlockOSThread()
	bestRatio, bestTook, bestLimit := 0.0, time.Duration(0), time.Duration(0)
	for attempt := 1; attempt <= budgetAttempts; attempt++ {
		ref := referenceWork(t)
		limit := budget * slower
		if ref > referenceCPU*slower {
			limit = time.Duration(float64(limit) * float64(ref) / float64(referenceCPU*slower))
		}
		took := measure()
		ratio := float64(took) / float64(limit)
		t.Logf("%s, run %d: %v (limit %v, reference work %v, quiet %v)", what, attempt, took, limit, ref, referenceCPU*slower)
		if attempt == 1 || ratio < bestRatio {
			bestRatio, bestTook, bestLimit = ratio, took, limit
		}
		if bestRatio <= 1 {
			return
		}
	}
	t.Errorf("%s took %v of CPU time in the best of %d runs, the limit of that run is %v (budget %v)", what, bestTook, budgetAttempts, bestLimit, budget)
}

// TestAFrameWhileScrollingAHundredThousandRowsStaysUnderTheBudget holds the budget of a frame: the wheel moves the rows by five
// lines at a time, and no frame (the work of the UI thread between two Pumps: build, layout and paint) takes more than 16 ms. The
// rows off screen are never laid out: the number of rows built is the number that came into view.
func TestAFrameWhileScrollingAHundredThousandRowsStaysUnderTheBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("a long run")
	}
	withinBudget(t, "the longest frame of 200 scrolls", frameBudget, func() time.Duration {
		g := New(Options{})
		defer g.Dispose()
		g.SetSource(numbered(benchRows))
		ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 900, Height: 420}, g.Widget())
		at := gui.Offset{Dx: 400, Dy: 200}
		var worst time.Duration
		built := g.RowsBuilt()
		for i := range 200 {
			ts.ScrollAt(at, gui.Offset{Dy: 110 * float64(1-2*(i/100%2))})
			start := threadCPU(t)
			ts.Pump(16 * time.Millisecond)
			if d := threadCPU(t) - start; d > worst {
				worst = d
			}
		}
		if top := g.vCtl.Offset(); top < 0 || top > 200*110 {
			t.Errorf("scrolled to %v", top)
		}
		if per := float64(g.RowsBuilt()-built) / 200; per > 40 {
			t.Errorf("%.1f rows built for each frame: the rows off screen are being built", per)
		}
		return worst
	})
}

// TestTheFirstFrameOfAHundredThousandRowsStaysUnderTheBudget: a result of 100,000 rows that is made lazily is on screen within
// firstBudget: the widths are guessed from the first 200 rows and only the rows in view are built.
func TestTheFirstFrameOfAHundredThousandRowsStaysUnderTheBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("a long run")
	}
	withinBudget(t, "the first frame", firstBudget, func() time.Duration {
		start := threadCPU(t)
		g := New(Options{})
		defer g.Dispose()
		g.SetSource(numbered(benchRows))
		ts := uitest.Pump(t, uitest.Themes()[0], gui.Size{Width: 900, Height: 420}, g.Widget())
		ts.Pump(16 * time.Millisecond)
		return threadCPU(t) - start
	})
}

// TestSortingAndCopyingAHundredThousandRowsStayUnderTheirBudgets: the sort of a column and the copy of the whole result, which run on
// the UI thread, take a second or two at the most.
func TestSortingAndCopyingAHundredThousandRowsStayUnderTheirBudgets(t *testing.T) {
	if testing.Short() {
		t.Skip("a long run")
	}
	m := NewModel(ModelOptions{})
	m.SetSource(numbered(benchRows))
	withinBudget(t, "the sort", sortBudget, func() time.Duration {
		start := threadCPU(t)
		m.SortBy(1, SortDesc)
		took := threadCPU(t) - start
		m.SortBy(1, SortNone)
		return took
	})
	withinBudget(t, "the copy of everything", copyBudget, func() time.Duration {
		start := threadCPU(t)
		if len(m.CopyAll(true)) < benchRows {
			t.Fatal("the copy is short")
		}
		return threadCPU(t) - start
	})
}
