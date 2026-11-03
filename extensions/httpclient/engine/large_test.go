package engine

import (
	"io"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// generated writes size bytes of a repeating pattern without ever holding more than one block.
func generated(w io.Writer, size int64) {
	block := make([]byte, 64<<10)
	for i := range block {
		block[i] = byte('a' + i%26)
	}
	for size > 0 {
		n := int64(len(block))
		if size < n {
			n = size
		}
		if _, err := w.Write(block[:n]); err != nil {
			return
		}
		size -= n
	}
}

// TestLargeBodyIsStreamedToTheCache sends a 200 MiB body (made on the fly by the server) and checks that the engine writes it to
// the cache and that the live heap stays near the memory limit, not near the size of the body.
func TestLargeBodyIsStreamedToTheCache(t *testing.T) {
	if testing.Short() {
		t.Skip("moves 200 MiB")
	}
	const size = 200 << 20
	const limit = 4 << 20
	srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", strconv.Itoa(size))
		generated(w, size)
	})
	e := newEngine(t, Config{MemoryLimit: limit})

	runtime.GC()
	var peakHeap, peakSys atomic.Uint64
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		var m runtime.MemStats
		for {
			runtime.ReadMemStats(&m)
			if m.HeapAlloc > peakHeap.Load() {
				peakHeap.Store(m.HeapAlloc)
			}
			if m.HeapSys > peakSys.Load() {
				peakSys.Store(m.HeapSys)
			}
			select {
			case <-stop:
				return
			case <-time.After(5 * time.Millisecond):
			}
		}
	}()
	resp := send(t, e, get(srv.URL))
	close(stop)
	<-done

	b := resp.Body
	if b.InMemory() || b.Size() != size || resp.Transferred != size {
		t.Fatalf("in memory %v, size %d, transferred %d", b.InMemory(), b.Size(), resp.Transferred)
	}
	st, err := os.Stat(b.Path())
	if err != nil || st.Size() != size {
		t.Fatalf("cache file %v %v", st, err)
	}
	page := make([]byte, 1000)
	off := int64(size - 1000)
	if n, err := b.ReadAt(page, off); n != 1000 || err != nil {
		t.Fatalf("ReadAt: %d %v", n, err)
	}
	for i, c := range page {
		if want := byte('a' + (off+int64(i))%(64<<10)%26); c != want {
			t.Fatalf("byte %d is %q, want %q", i, c, want)
		}
	}
	t.Logf("200 MiB with a %d MiB limit: peak live heap %.1f MiB, peak heap obtained from the system %.1f MiB, %v",
		limit>>20, float64(peakHeap.Load())/(1<<20), float64(peakSys.Load())/(1<<20), resp.Timings.Total)
	// the heap holds the limit's buffer (it grows by doubling, so up to twice the limit) and the garbage not yet collected
	if got := peakHeap.Load(); got > 48<<20 {
		t.Fatalf("the peak live heap was %d MiB for a body of %d MiB", got>>20, size>>20)
	}
	if err := b.Remove(); err != nil {
		t.Fatal(err)
	}
}
