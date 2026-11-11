//go:build emetos_perf

package httpclient

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// The measurements of the HTTP client (step 27.9, SPEC §14, docs/PERFORMANCE.md "HTTP Client"): a collection of 1,000 requests in the side
// bar, a 50 MiB JSON response, and the runner over 500 requests against a server on 127.0.0.1. The times are the UI thread's processor
// time of build, layout and paint with the fake platform (the test goroutine is the UI thread, locked to its OS thread), as in the other
// view measurements. Run them alone:
//
//	scripts/heavy.sh --alone -- go test -tags emetos_perf ./extensions/httpclient -run TestPerf -v -count=1

// frameStats describes the processor times of frames.
func frameStats(d []time.Duration) string {
	if len(d) == 0 {
		return "no frames"
	}
	s := slices.Clone(d)
	slices.Sort(s)
	var total time.Duration
	over, over100 := 0, 0
	for _, x := range s {
		total += x
		if x > 16*time.Millisecond {
			over++
		}
		if x > 100*time.Millisecond {
			over100++
		}
	}
	return fmt.Sprintf("%d frames: in all %v, average %v, median %v, p90 %v, longest %v, %d over 16 ms, %d over 100 ms", len(s), total.Round(time.Microsecond),
		(total / time.Duration(len(s))).Round(time.Microsecond), s[len(s)/2].Round(time.Microsecond), s[len(s)*9/10].Round(time.Microsecond),
		s[len(s)-1].Round(time.Microsecond), over, over100)
}

const (
	perfFolders          = 10
	perfRequestsInFolder = 100
)

func TestPerfCollectionOfAThousandRequests(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	var loadFrom time.Time
	r := newRig(t, func(folder string) {
		root := filepath.Join(folder, ".emetos", "http", "collections", "big")
		writeFile(t, filepath.Join(root, "collection.jsonc"), `{"emetosHttp": 1, "name": "Big API"}`)
		for f := range perfFolders {
			dir := filepath.Join(root, fmt.Sprintf("area-%02d", f))
			writeFile(t, filepath.Join(dir, "folder.jsonc"), fmt.Sprintf(`{"emetosHttp": 1, "name": "Area %02d"}`, f))
			for i := range perfRequestsInFolder {
				methods := []string{"GET", "POST", "PUT", "PATCH", "DELETE"}
				writeFile(t, filepath.Join(dir, fmt.Sprintf("request-%03d.request.jsonc", i)), fmt.Sprintf(
					`{"emetosHttp": 1, "name": "Request %d of area %d", "method": %q, "url": "{{baseUrl}}/v1/area%d/item%d", "headers": [{"name": "X-Area", "value": "%d"}]}`,
					i, f, methods[i%len(methods)], f, i, f))
			}
		}
		loadFrom = time.Now()
	})
	load := time.Since(loadFrom)
	t.Logf("the extension activated and the 1,000 requests (%d folders of %d, 1,002 files) read and parsed in the background: %v of wall time (polled every 2 ms)", perfFolders, perfRequestsInFolder, load.Round(time.Millisecond))
	if r.ext.tree == nil || len(r.ext.tree.Collections) < 2 {
		t.Fatal("the collection is not in the tree")
	}

	// the rebuild of the rows (every change of a file does it, on the UI thread)
	runtime.GC()
	var rebuilds []time.Duration
	for range 5 {
		s := threadCPU(t)
		r.ext.collections.rebuild()
		rebuilds = append(rebuilds, threadCPU(t)-s)
	}
	t.Logf("rebuild of the rows from the tree of the store (SetItems of 1,000 requests, what a change on disk costs the UI thread): %v", frameStats(rebuilds))

	s := threadCPU(t)
	ts := r.show(uitest.Themes()[0], CollectionsViewID, 320, 700)
	ts.Pump(16 * time.Millisecond)
	t.Logf("open: the view shown in a new window and its first frames (includes making the test window): %v", (threadCPU(t) - s).Round(time.Microsecond))
	frame := func() time.Duration {
		s := threadCPU(t)
		r.api.Pump()
		ts.Pump(16 * time.Millisecond)
		return threadCPU(t) - s
	}
	var keys []string
	for _, c := range r.ext.tree.Collections {
		if c.Name != "Big API" {
			continue
		}
		for _, f := range c.Children {
			keys = append(keys, f.Path)
		}
	}
	if len(keys) != perfFolders {
		t.Fatalf("%d folders in the big collection", len(keys))
	}
	t.Logf("collapsed (the collection open, %d folder rows): a frame %v", perfFolders, frame())

	tree := r.ext.collections.tree
	var expand []time.Duration
	for _, k := range keys {
		tree.SetExpandedKeys(append(tree.ExpandedKeys(), k))
		expand = append(expand, frame())
	}
	t.Logf("expand: one folder of 100 requests after the other: %s", frameStats(expand))
	tree.SetExpandedKeys(append(tree.ExpandedKeys(), keys...))
	t.Logf("all the folders open (1,000 request rows and the %d above them): a frame %v", perfFolders+2, frame())

	var scroll []time.Duration
	at := gui.Offset{Dx: 150, Dy: 300}
	for range 60 {
		ts.ScrollAt(at, gui.Offset{Dy: 240})
		scroll = append(scroll, frame())
	}
	t.Logf("a steady scroll through the rows: %s", frameStats(scroll))
	var idle []time.Duration
	for range 10 {
		idle = append(idle, frame())
	}
	t.Logf("nothing changed: %s", frameStats(idle))
	var again []time.Duration
	for range 5 {
		s := threadCPU(t)
		r.ext.collections.rebuild()
		ts.Pump(16 * time.Millisecond)
		again = append(again, threadCPU(t)-s)
	}
	t.Logf("a rebuild of the rows and the frame that draws it, everything open: %s", frameStats(again))
}

// jsonOfSize writes a JSON array of objects of about size bytes to w, in chunks, never holding more than one chunk.
func jsonOfSize(w http.ResponseWriter, size int) {
	w.Header().Set("Content-Type", "application/json")
	var b strings.Builder
	b.WriteString("[")
	written := 0
	for i := 0; ; i++ {
		item := `{"id":` + strconv.Itoa(i) + `,"name":"item-` + strconv.Itoa(i) + `","tags":["alpha","beta"],"score":0.5,"active":true}`
		if i > 0 {
			item = "," + item
		}
		if written+b.Len()+len(item)+1 >= size {
			break
		}
		b.WriteString(item)
		if b.Len() >= 64<<10 {
			_, _ = w.Write([]byte(b.String()))
			written += b.Len()
			b.Reset()
		}
	}
	b.WriteString("]")
	_, _ = w.Write([]byte(b.String()))
}

func TestPerfFiftyMiBJSONResponse(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	srv := bodyServer(t, map[string]func(http.ResponseWriter){
		"/json50": func(w http.ResponseWriter) { jsonOfSize(w, 50<<20) },
		"/json2":  func(w http.ResponseWriter) { jsonOfSize(w, 2<<20-64) },
	})
	r, tab := reqRig(t, pCreate)

	// the largest body that still has a Pretty view (the limit is 2 MiB)
	start := time.Now()
	r.sendTo(tab, srv, "/json2")
	t.Logf("a 2 MiB JSON response (the largest with a Pretty view): sent, read and displayed in %v of wall time; %d pretty lines, mode %s", time.Since(start).Round(time.Millisecond),
		len(tab.pane.disp.pretty), tab.pane.mode)
	s := threadCPU(t)
	ts := r.showReq(uitest.Themes()[0], tab, respW, respH)
	ts.Pump(16 * time.Millisecond)
	t.Logf("  its first frames in a new window (includes making the test window): %v", (threadCPU(t) - s).Round(time.Microsecond))
	frame := func() time.Duration {
		s := threadCPU(t)
		r.api.Pump()
		ts.Pump(16 * time.Millisecond)
		return threadCPU(t) - s
	}
	var scroll []time.Duration
	for range 60 {
		ts.ScrollAt(gui.Offset{Dx: 400, Dy: 400}, gui.Offset{Dy: 240})
		scroll = append(scroll, frame())
	}
	t.Logf("  Pretty, a steady scroll: %s", frameStats(scroll))
	tab.dropResponse()

	// 50 MiB
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	tab2 := tab
	start = time.Now()
	tab2.d.method = "GET"
	tab2.d.url.SetValue(srv.URL + "/json50")
	tab2.sendRequest()
	var posted []time.Duration
	for {
		s := threadCPU(t)
		r.api.Pump()
		posted = append(posted, threadCPU(t)-s)
		if tab2.send.resp != nil && tab2.pane != nil && r.ext.pending.Load() == 0 {
			break
		}
		if time.Since(start) > 120*time.Second {
			t.Fatal("the response did not arrive")
		}
		time.Sleep(time.Millisecond)
	}
	wall := time.Since(start)
	body := tab2.send.resp.Body
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	t.Logf("a 50 MiB JSON response: sent and read in %v of wall time (%d bytes, in memory %v, %d pages of 64 KiB); the UI thread's work while it came in (the posted callbacks): %d callbacks, longest %v",
		wall.Round(time.Millisecond), body.Size(), body.InMemory(), tab2.pane.disp.pages, len(posted), slices.Max(posted).Round(time.Microsecond))
	t.Logf("  the user gets: mode %q, a Pretty view: %v, the note %q; heap in use %d MiB before, %d MiB after", tab2.pane.mode, tab2.pane.disp.hasPretty(), tab2.pane.disp.why,
		before.HeapAlloc>>20, after.HeapAlloc>>20)
	runtime.GC()
	var kept runtime.MemStats
	runtime.ReadMemStats(&kept)
	t.Logf("  the heap after a collection with the response held: %d MiB (%.2f bodies above the %d MiB before; the test server holds one chunk of 64 KiB)",
		kept.HeapAlloc>>20, (float64(kept.HeapAlloc)-float64(before.HeapAlloc))/float64(body.Size()), before.HeapAlloc>>20)

	bd := time.Now()
	buildDisplay(tab2.send.resp)
	t.Logf("  buildDisplay (the work off the UI thread when a response arrives) takes %v", time.Since(bd).Round(time.Microsecond))

	s = threadCPU(t)
	ts2 := r.showReq(uitest.Themes()[0], tab2, respW, respH)
	ts2.Pump(16 * time.Millisecond)
	t.Logf("  the first page shown (first frames in a new window, includes making the test window): %v", (threadCPU(t) - s).Round(time.Microsecond))
	frame2 := func() time.Duration {
		s := threadCPU(t)
		r.api.Pump()
		ts2.Pump(16 * time.Millisecond)
		return threadCPU(t) - s
	}
	var idle []time.Duration
	for range 20 {
		idle = append(idle, frame2())
	}
	t.Logf("  Raw, page 1, nothing changed: %s", frameStats(idle))
	var turns, scrolls []time.Duration
	for range 100 {
		s := threadCPU(t)
		ts2.Tap(iconButton("Next page"))
		ts2.Pump(16 * time.Millisecond)
		turns = append(turns, threadCPU(t)-s)
	}
	t.Logf("  100 page turns (Next page, the tap and its frame): %s; now on page %d", frameStats(turns), tab2.pane.rawPage+1)
	for range 60 {
		ts2.ScrollAt(gui.Offset{Dx: 400, Dy: 400}, gui.Offset{Dy: 240})
		scrolls = append(scrolls, frame2())
	}
	t.Logf("  a steady scroll inside a page: %s", frameStats(scrolls))
	s = threadCPU(t)
	ts2.Tap(iconButton("Last page"))
	ts2.Pump(16 * time.Millisecond)
	t.Logf("  Last page (page %d): %v", tab2.pane.rawPage+1, (threadCPU(t) - s).Round(time.Microsecond))
	tab2.dropResponse()
}

func TestPerfRunnerOverFiveHundredRequests(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	const n = 500
	var served atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		served.Add(1)
		w.Header().Set("Content-Type", "application/json")
		id := strings.TrimPrefix(r.URL.Path, "/item/")
		_, _ = w.Write([]byte(`{"id":` + id + `,"name":"item ` + id + `"}`))
	}))
	t.Cleanup(srv.Close)
	r := newRig(t, func(folder string) {
		root := filepath.Join(folder, ".emetos", "http", "collections", "bulk")
		writeFile(t, filepath.Join(root, "collection.jsonc"), `{"emetosHttp": 1, "name": "Bulk"}`)
		for i := range n {
			writeFile(t, filepath.Join(root, fmt.Sprintf("r%03d.request.jsonc", i)), fmt.Sprintf(
				`{"emetosHttp": 1, "name": "Item %d", "method": "GET", "url": "{{baseUrl}}/item/%d",
  "extract": [{"from": "body", "path": "$.id", "to": "last"}],
  "checks": [{"subject": "status", "op": "equals", "value": 200}, {"subject": "json", "path": "$.name", "op": "exists"}]}`, i, i))
		}
	})
	r.useBase(srv.URL)
	tab := r.openRunner("bulk")
	ts := r.showRunner(uitest.Themes()[0], tab, 980, 700)
	ts.Pump(16 * time.Millisecond)
	runtime.GC()

	tab.start()
	var frames []time.Duration
	var walls []time.Duration
	start := time.Now()
	for {
		f0 := time.Now()
		s := threadCPU(t)
		r.api.Pump()
		ts.Pump(16 * time.Millisecond)
		frames = append(frames, threadCPU(t)-s)
		walls = append(walls, time.Since(f0))
		if !tab.running && r.ext.pending.Load() == 0 {
			break
		}
		if time.Since(start) > 120*time.Second {
			t.Fatal("the run did not end")
		}
		if rest := 16*time.Millisecond - time.Since(f0); rest > 0 {
			time.Sleep(rest)
		}
	}
	wall := time.Since(start)
	r.settle()
	t.Logf("a run of %d requests against a server on 127.0.0.1 (each with an extraction and two checks): %v of wall time, %d served, %d results", n, wall.Round(time.Millisecond), served.Load(), len(tab.results))
	t.Logf("  the UI thread while the results stream in (processor time of each frame at 60 a second): %s", frameStats(frames))
	t.Logf("  the same frames by wall time (the UI thread blocked, whatever it did): %s", frameStats(walls))
	var scroll, idle []time.Duration
	for range 10 {
		s := threadCPU(t)
		ts.Pump(16 * time.Millisecond)
		idle = append(idle, threadCPU(t)-s)
	}
	for range 60 {
		s := threadCPU(t)
		ts.ScrollAt(gui.Offset{Dx: 500, Dy: 450}, gui.Offset{Dy: 240})
		ts.Pump(16 * time.Millisecond)
		scroll = append(scroll, threadCPU(t)-s)
	}
	t.Logf("  after the run, nothing changed: %s", frameStats(idle))
	t.Logf("  after the run, a steady scroll through the results: %s", frameStats(scroll))
}
