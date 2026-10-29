package redis

import "sync"

// ring keeps the last cap values: the Pub/Sub and Monitor tabs are bounded (SPEC §10.23), a busy channel must not grow memory. It belongs
// to the UI thread.
type ring[T any] struct {
	buf     []T
	start   int
	n       int
	dropped int64
}

// newRing makes a ring that holds at most capacity values (at least 1).
func newRing[T any](capacity int) *ring[T] { return &ring[T]{buf: make([]T, max(capacity, 1))} }

// push adds a value; the oldest goes when the ring is full and is counted in dropped.
func (r *ring[T]) push(v T) {
	if r.n < len(r.buf) {
		r.buf[(r.start+r.n)%len(r.buf)] = v
		r.n++
		return
	}
	r.buf[r.start] = v
	r.start = (r.start + 1) % len(r.buf)
	r.dropped++
}

// len is the number of values held.
func (r *ring[T]) len() int { return r.n }

// at returns the value i places after the oldest one.
func (r *ring[T]) at(i int) T { return r.buf[(r.start+i)%len(r.buf)] }

// snapshot copies the values, the oldest first.
func (r *ring[T]) snapshot() []T {
	out := make([]T, r.n)
	for i := range out {
		out[i] = r.at(i)
	}
	return out
}

// clear forgets the values and the count of dropped ones.
func (r *ring[T]) clear() {
	var zero T
	for i := range r.buf {
		r.buf[i] = zero
	}
	r.start, r.n, r.dropped = 0, 0, 0
}

// maxPending is how many values a feeder holds for the UI thread; a flood past it is dropped and counted.
const maxPending = 10000

// feeder hands values from a goroutine that reads a connection to the UI thread in batches: a busy channel makes one Post for
// many values, not one for each, and the values the UI thread has not taken are bounded.
type feeder[T any] struct {
	mu        sync.Mutex
	pending   []T
	lost      int64
	scheduled bool
}

// add queues a value and tells whether the caller has to Post a flush (none is scheduled yet).
func (f *feeder[T]) add(v T) (post bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.pending) >= maxPending {
		f.lost++
	} else {
		f.pending = append(f.pending, v)
	}
	if f.scheduled {
		return false
	}
	f.scheduled = true
	return true
}

// take returns the queued values and how many were dropped since the last take, and lets the next add schedule a flush.
func (f *feeder[T]) take() (values []T, lost int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	values, lost = f.pending, f.lost
	f.pending, f.lost, f.scheduled = nil, 0, false
	return values, lost
}
