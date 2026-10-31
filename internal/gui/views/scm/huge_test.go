package scm

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/ui"

	scmcore "github.com/rm4n0s/emetos/internal/core/scm"
)

// TestHugeChanges is the budget of SPEC §14 for the view: 100,000 changed files in one repository build the rows of the screen only.
// The first frame after the update (the rows made on the UI thread, the sort made on the goroutine that changed the model) is
// within 100 ms, and while scrolling no frame of the UI thread goes over 16 ms (build, layout and paint, without the renderer;
// ten times these under the race detector, which slows everything). The times are the processor time of the thread, which a
// busy machine does not stretch; the collector works on other threads. A frame may still be late for a cache that misses, so two
// of the 60 frames of the steady scroll may be; a jump builds a new screen of rows and has twice the budget.
func TestHugeChanges(t *testing.T) {
	if testing.Short() {
		t.Skip("a big model")
	}
	const n = 100_000
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	scale := time.Duration(1)
	if raceEnabled {
		scale = 10
	}
	r := newRig(t)
	// the window hands the work to the UI thread: here it is run when the test says
	var queue []func()
	r.view.opts.Post = func(f func()) { queue = append(queue, f) }
	src := r.model.Add(scmcore.SourceSpec{ProviderID: "git", Label: "Git", RootURI: "file:///work/big", ContextValue: "repository", Owner: "e"})
	g := src.AddGroup("workingTree", "Changes", false, "")
	r.show(goldenThemes[0].colors, ui.Size{Width: 400, Height: 600})
	for _, f := range queue {
		f()
	}
	queue = nil

	// the first frame after an update; a machine that is busy with other work may make one attempt slow, so the best of three counts
	var first time.Duration
	for attempt := range 3 {
		rs := make([]scmcore.Resource, n)
		for i := range rs {
			rs[i] = scmcore.Resource{URI: fmt.Sprintf("file:///work/big/dir%03d/file%05d.go", i%200, i+attempt*n)}
		}
		changed := time.Now()
		g.SetResources(rs) // on the goroutine of the provider: the model, its notice and the sort
		update := time.Since(changed)
		runtime.GC()
		start, wall := threadCPU(t), time.Now()
		for _, f := range queue {
			f()
		}
		queue = nil
		r.ts.Pump(16 * time.Millisecond)
		cpu, elapsed := threadCPU(t)-start, time.Since(wall)
		if rows := r.view.RowCount(); rows != n+2 {
			t.Fatalf("%d rows, want %d", rows, n+2)
		}
		t.Logf("attempt %d: the update of the model took %v off the UI thread; the first frame after it %v of processor time (%v of wall time)", attempt+1, update, cpu, elapsed)
		if attempt == 0 || cpu < first {
			first = cpu
		}
		if first <= 100*time.Millisecond*scale {
			break
		}
	}
	if first > 100*time.Millisecond*scale {
		t.Errorf("the first frame after the update took %v, budget %v", first, 100*time.Millisecond*scale)
	}

	// scroll through the list: wheel steps, then jumps to the middle, the end and the top
	runtime.GC()
	frame := func() time.Duration {
		s := threadCPU(t)
		r.ts.Pump(16 * time.Millisecond)
		return threadCPU(t) - s
	}
	budget := 16 * time.Millisecond * scale
	var worst time.Duration
	late := 0
	at := ui.Offset{Dx: 100, Dy: 300}
	for range 60 {
		r.ts.ScrollAt(at, ui.Offset{Dy: 240})
		d := frame()
		worst = max(worst, d)
		if d > budget {
			late++
		}
	}
	t.Logf("the longest of 60 frames of a steady scroll: %v (%d over %v)", worst, late, budget)
	if late > 2 {
		t.Errorf("%d of 60 frames took more than %v while scrolling (the longest %v)", late, budget, worst)
	}
	var jump time.Duration
	// the end is the exact end of the content: the input and the button are taller than the files, and the list knows its height
	end := r.view.List().ContentHeight()
	for _, y := range []float64{float64(n) * rowHeight / 2, end, 0, end} {
		r.view.ctl.ScrollTo(y)
		d := frame()
		t.Logf("a jump to %.0f: %v", y, d)
		jump = max(jump, d)
		if y == end {
			if got, want := r.view.ctl.ScrollOffset()+r.view.ctl.ViewportHeight(), end; got != want {
				t.Errorf("a jump to the end: the view ends at %v, the content at %v", got, want)
			}
		}
	}
	t.Logf("the longest frame after a jump: %v", jump)
	if jump > 2*budget {
		t.Errorf("a frame after a jump took %v, budget %v", jump, 2*budget)
	}
	r.noErrors()
}
