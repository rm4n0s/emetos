package explorer

import (
	"fmt"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/decorations"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// TestHugeNumberOfDecoratedFiles is the budget of SPEC §14 for the decorations in the Explorer: a repository with 100,000 changed files
// is told to the service, as the host does, 2,000 files in each turn of the UI thread (no turn over 16 ms), and the frames of a tree of
// 2,000 files, all of them decorated, are no slower than the frames of the same tree without decorations (the median of 60 frames of a
// scroll within 25 % and 2 ms of it; the Explorer's own frame, which the software renderer of the test makes slow, is the baseline). The first
// frame after the last turn builds the rows of the screen within 100 ms. The times are the processor time of the thread, which a busy
// machine does not stretch (ten times the turn and the first frame under the race detector).
func TestHugeNumberOfDecoratedFiles(t *testing.T) {
	if testing.Short() {
		t.Skip("a big model")
	}
	const (
		visible = 2000
		total   = 100_000
		chunk   = 2000
	)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	scale := time.Duration(1)
	if raceEnabled {
		scale = 10
	}
	names := make([]string, visible)
	for i := range names {
		names[i] = fmt.Sprintf("dir%02d/file%04d.go", i%20, i)
	}
	w := newWRig(t, kit.DarkModern(), ui.Size{Width: 300, Height: 600}, names...)
	for i := 0; i < 20; i++ {
		w.view.Tree().Expand(w.path(fmt.Sprintf("dir%02d", i)))
	}
	w.wait("the folders", w.loaded)
	w.settle()
	frame := func() time.Duration {
		s := threadCPU(t)
		w.ts.Pump(16 * time.Millisecond)
		return threadCPU(t) - s
	}
	scroll := func() time.Duration {
		runtime.GC()
		at := ui.Offset{Dx: 100, Dy: 300}
		times := make([]time.Duration, 60)
		for i := range times {
			w.ts.ScrollAt(at, ui.Offset{Dy: 240})
			times[i] = frame()
		}
		slices.Sort(times)
		return times[len(times)/2]
	}
	plain := scroll()
	t.Logf("the median frame of a scroll with no decorations: %v", plain)
	data := make(map[string]decorations.Data, total)
	uris := make([]string, 0, total)
	for i := 0; i < total; i++ {
		var uri string
		if i < visible {
			uri = decorations.FileURI(w.path(names[i]))
		} else {
			uri = fmt.Sprintf("file:///elsewhere/dir%03d/file%05d.go", i%300, i)
		}
		data[uri] = decorations.Data{Weight: 10 + i%4, Letter: "M", Tooltip: "Modified", Color: "gitDecoration.modifiedResourceForeground", Bubble: true}
		uris = append(uris, uri)
	}
	reg, err := w.decos.Register("git", decorations.ProviderFunc(func(uri string) (decorations.Data, bool) { d, ok := data[uri]; return d, ok }))
	if err != nil {
		t.Fatal(err)
	}
	// the turns of the UI thread: the longest of them, and a frame between each two
	runtime.GC()
	var longest time.Duration
	for from := 0; from < total; from += chunk {
		start := threadCPU(t)
		reg.Fire(uris[from:min(from+chunk, total)]...)
		longest = max(longest, threadCPU(t)-start)
	}
	t.Logf("the longest turn of the %d files of a Fire: %v", chunk, longest)
	if longest > 16*time.Millisecond*scale {
		t.Errorf("a turn of the UI thread took %v, budget %v", longest, 16*time.Millisecond*scale)
	}
	if got := w.decos.Len(); got != total {
		t.Errorf("%d answers kept, want %d", got, total)
	}

	// the first frame after the last turn builds the rows of the screen
	start := threadCPU(t)
	w.ts.Pump(16 * time.Millisecond)
	first := threadCPU(t) - start
	t.Logf("the first frame after the last turn: %v", first)
	if first > 100*time.Millisecond*scale {
		t.Errorf("the first frame took %v, budget %v", first, 100*time.Millisecond*scale)
	}

	decorated := scroll()
	t.Logf("the median frame of a scroll with %d decorated files: %v", total, decorated)
	if limit := plain + plain/4 + 2*time.Millisecond; decorated > limit {
		t.Errorf("the median frame with decorations took %v, over %v (no decorations: %v)", decorated, limit, plain)
	}
	if w.decos.Len() > total+visible {
		t.Errorf("the cache grew to %d answers", w.decos.Len())
	}
	w.noErrors()
}
