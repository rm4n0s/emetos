package engine

import (
	"compress/gzip"
	"net/http"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// TestABodyInMemoryIsHeldAboutOnce sends a body within the memory limit three ways (announced length, chunked, gzip) and checks that
// the heap holds about one copy of it afterwards, and, when the length is announced, never much more than one while it is read.
func TestABodyInMemoryIsHeldAboutOnce(t *testing.T) {
	const size = 16 << 20
	tests := []struct {
		name  string
		serve func(w http.ResponseWriter)
		// peak is the most the heap may hold, in bodies, while the body is read (0: not checked)
		peak float64
	}{
		{name: "announced length", serve: func(w http.ResponseWriter) {
			w.Header().Set("Content-Length", strconv.Itoa(size))
			generated(w, size)
		}, peak: 1.5},
		{name: "chunked", serve: func(w http.ResponseWriter) {
			w.Header().Set("Content-Type", "text/plain")
			generated(w, size)
		}},
		{name: "gzip", serve: func(w http.ResponseWriter) {
			w.Header().Set("Content-Encoding", "gzip")
			zw := gzip.NewWriter(w)
			generated(zw, size)
			_ = zw.Close()
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newServer(t, func(w http.ResponseWriter, _ *http.Request) { tt.serve(w) })
			e := newEngine(t, Config{MemoryLimit: 50 << 20})
			runtime.GC()
			var base runtime.MemStats
			runtime.ReadMemStats(&base)
			var peak atomic.Uint64
			stop, done := make(chan struct{}), make(chan struct{})
			go func() {
				defer close(done)
				var m runtime.MemStats
				for {
					runtime.ReadMemStats(&m)
					if m.HeapAlloc > peak.Load() {
						peak.Store(m.HeapAlloc)
					}
					select {
					case <-stop:
						return
					case <-time.After(2 * time.Millisecond):
					}
				}
			}()
			resp := send(t, e, get(srv.URL))
			close(stop)
			<-done
			if !resp.Body.InMemory() || resp.Body.Size() != size {
				t.Fatalf("in memory %v, size %d", resp.Body.InMemory(), resp.Body.Size())
			}
			runtime.GC()
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			held := float64(after.HeapAlloc) - float64(base.HeapAlloc)
			t.Logf("%s: the heap holds %.2f bodies after the response, peaked at %.2f", tt.name, held/size, (float64(peak.Load())-float64(base.HeapAlloc))/size)
			if held > 1.2*size {
				t.Errorf("the heap holds %.2f bodies after the response", held/size)
			}
			if tt.peak > 0 {
				if got := (float64(peak.Load()) - float64(base.HeapAlloc)) / size; got > tt.peak {
					t.Errorf("the heap peaked at %.2f bodies while the body was read", got)
				}
			}
			runtime.KeepAlive(resp)
		})
	}
}
