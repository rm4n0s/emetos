package dap_test

import (
	"encoding/json"
	"runtime"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
)

// TestEventBurstIsReadWithBoundedMemory: an adapter that sends 10,000 events (about 11 MB) at once while the consumer does not
// read. The client stops reading the adapter when its buffer is full, so the adapter is held up by the pipe and the memory does
// not grow; when the consumer reads, every event comes, in order.
func TestEventBurstIsReadWithBoundedMemory(t *testing.T) {
	const (
		total   = 10000
		payload = 1000
		buffer  = 64
	)
	for _, tr := range []daptest.Transport{daptest.InProcess, daptest.Stdio} {
		name := map[daptest.Transport]string{daptest.InProcess: "in process", daptest.Stdio: "stdio"}[tr]
		t.Run(name, func(t *testing.T) {
			h := daptest.Start(t, daptest.Options{Transport: tr, Client: dap.ClientOptions{EventBuffer: buffer}, Script: daptest.Script{
				daptest.Expect("threads", nil),
				daptest.Burst(total, payload),
				daptest.Respond("threads", dap.ThreadsResponseBody{Threads: []dap.Thread{{ID: 1, Name: "done"}}}),
			}})
			c := h.Client
			runtime.GC()
			var before runtime.MemStats
			runtime.ReadMemStats(&before)

			type result struct {
				threads dap.ThreadsResponseBody
				err     error
			}
			resc := make(chan result, 1)
			go func() {
				th, err := dap.Call[dap.ThreadsResponseBody](ctxT(t), c, dap.CmdThreads, nil)
				resc <- result{th, err}
			}()

			// Phase 1: nobody reads. The buffer fills and the client stops reading the adapter.
			waitFor(t, "the event buffer to fill", func() bool { return len(c.Events()) == buffer })
			time.Sleep(300 * time.Millisecond) // time enough for an unbounded reader to take the whole burst
			if got := len(c.Events()); got != buffer {
				t.Errorf("%d events are buffered, want exactly the %d of EventBuffer", got, buffer)
			}
			if h.Server != nil {
				if sent := h.Server.Sent(); sent >= total/10 {
					t.Errorf("the adapter has written %d of %d events: the client did not hold it up", sent, total)
				}
			}
			runtime.GC()
			var held runtime.MemStats
			runtime.ReadMemStats(&held)
			if grown := int64(held.HeapAlloc) - int64(before.HeapAlloc); grown > 4<<20 {
				t.Errorf("the heap grew by %d bytes while nobody read; the burst is %d bytes", grown, total*payload)
			}

			// Phase 2: the consumer reads everything, in order.
			maxQueued := 0
			for n := range total {
				ev := nextEvent(t, c)
				if q := len(c.Events()); q > maxQueued {
					maxQueued = q
				}
				var body struct {
					N      int    `json:"n"`
					Output string `json:"output"`
				}
				if ev.Event != dap.EventOutput || json.Unmarshal(ev.Body, &body) != nil || body.N != n || len(body.Output) < payload {
					t.Fatalf("event %d = %s %.60s", n, ev.Event, ev.Body)
				}
			}
			if maxQueued > buffer {
				t.Errorf("up to %d events were queued, want at most %d", maxQueued, buffer)
			}
			select {
			case r := <-resc:
				if r.err != nil || len(r.threads.Threads) != 1 || r.threads.Threads[0].Name != "done" {
					t.Errorf("threads = %+v, %v", r.threads, r.err)
				}
			case <-time.After(wait):
				t.Fatal("the response after the burst never came")
			}
			runtime.GC()
			var after runtime.MemStats
			runtime.ReadMemStats(&after)
			if grown := int64(after.HeapAlloc) - int64(before.HeapAlloc); grown > 8<<20 {
				t.Errorf("the heap grew by %d bytes over the whole burst", grown)
			}
			if v := h.Violations(); len(v) > 0 {
				t.Errorf("violations: %q", v)
			}
		})
	}
}
