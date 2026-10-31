package decorations

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// threadCPU is the processor time of the calling thread, which a busy machine does not stretch.
func threadCPU(t testing.TB) time.Duration {
	t.Helper()
	var ts unix.Timespec
	if err := unix.ClockGettime(unix.CLOCK_THREAD_CPUTIME_ID, &ts); err != nil {
		t.Fatal(err)
	}
	return time.Duration(ts.Nano())
}

// TestHugeNumberOfDecorations is the budget of SPEC §14 for the service: 100,000 decorated files in one repository are asked for with one
// Fire; every answer is kept (the cache is bounded at 250,000), the folders above them are merged lazily, and the answer for a row is a
// lookup that stays well below a millisecond. The times are the processor time of the thread.
func TestHugeNumberOfDecorations(t *testing.T) {
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
	uris := make([]string, n)
	data := make(map[string]Data, n)
	for i := range uris {
		uris[i] = fmt.Sprintf("file:///work/big/dir%03d/sub%02d/file%05d.go", i%200, i%7, i)
		data[uris[i]] = Data{Weight: 10 + i%4, Letter: "M", Color: "gitDecoration.modifiedResourceForeground", Tooltip: "Modified", Bubble: true}
	}
	s := New(Options{})
	reg, err := s.Register("git", ProviderFunc(func(uri string) (Data, bool) { d, ok := data[uri]; return d, ok }))
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	start := threadCPU(t)
	reg.Fire(uris...)
	fire := threadCPU(t) - start
	t.Logf("Fire of %d files: %v of processor time (%d answers kept)", n, fire, s.Len())
	if s.Len() != n {
		t.Errorf("%d answers kept, want %d", s.Len(), n)
	}
	// the first look at the root merges everything below it; the rows after it are lookups
	start = threadCPU(t)
	d, ok := s.Get("file:///work/big", true)
	first := threadCPU(t) - start
	t.Logf("the first Get of the root folder: %v", first)
	if !ok || !d.Bubble {
		t.Fatalf("root = %+v, %v", d, ok)
	}
	start = threadCPU(t)
	const rows = 5000
	for i := 0; i < rows; i++ {
		s.Get(uris[(i*37)%n], false)
		s.Get(fmt.Sprintf("file:///work/big/dir%03d", i%200), true)
	}
	per := (threadCPU(t) - start) / (2 * rows)
	t.Logf("a lookup of a row: %v", per)
	if per > 100*time.Microsecond*scale {
		t.Errorf("a lookup took %v, budget %v", per, 100*time.Microsecond*scale)
	}
	if first > 100*time.Millisecond*scale {
		t.Errorf("the first look at the root took %v, budget %v", first, 100*time.Millisecond*scale)
	}
	if fire > 500*time.Millisecond*scale {
		t.Errorf("Fire of %d files took %v, budget %v", n, fire, 500*time.Millisecond*scale)
	}
	// a change of one file only merges the folders above it again
	data[uris[5]] = Data{Weight: 10, Letter: "A", Bubble: true}
	start = threadCPU(t)
	reg.Fire(uris[5])
	s.Get("file:///work/big", true)
	one := threadCPU(t) - start
	t.Logf("one change and the root again: %v", one)
	if one > 100*time.Millisecond*scale {
		t.Errorf("one change took %v", one)
	}
}
