package storagetest

import (
	"sort"
	"sync"
	"time"
)

// Clock is a clock that only moves when a test says so, for the timers of a storage.StateStore: its
// AfterFunc is a storage.AfterFunc. The functions of the timers that are due run in Advance, in the order of
// their times, on the goroutine that called it.
type Clock struct {
	mu     sync.Mutex
	now    time.Duration
	seq    int
	timers []*timer
}

type timer struct {
	at   time.Duration
	seq  int
	f    func()
	done bool
}

// NewClock makes a clock at time zero with no timers.
func NewClock() *Clock { return &Clock{} }

// AfterFunc sets a timer for the time d from now and returns the function that cancels it (it reports whether it
// did: false when the timer ran already or was cancelled).
func (c *Clock) AfterFunc(d time.Duration, f func()) (stop func() bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.seq++
	t := &timer{at: c.now + d, seq: c.seq, f: f}
	c.timers = append(c.timers, t)
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		if t.done {
			return false
		}
		t.done = true
		return true
	}
}

// Advance moves the clock by d and runs the timers that came due, the earliest first. A timer set by a function
// that runs here, and due within d, runs too.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	target := c.now + d
	c.mu.Unlock()
	for {
		c.mu.Lock()
		sort.SliceStable(c.timers, func(i, j int) bool {
			if c.timers[i].at != c.timers[j].at {
				return c.timers[i].at < c.timers[j].at
			}
			return c.timers[i].seq < c.timers[j].seq
		})
		var next *timer
		for _, t := range c.timers {
			if !t.done && t.at <= target {
				next = t
				break
			}
		}
		if next == nil {
			c.now = target
			c.timers = liveTimers(c.timers)
			c.mu.Unlock()
			return
		}
		next.done = true
		c.now = max(c.now, next.at)
		c.mu.Unlock()
		next.f()
	}
}

func liveTimers(list []*timer) []*timer {
	var out []*timer
	for _, t := range list {
		if !t.done {
			out = append(out, t)
		}
	}
	return out
}

// Pending is the number of timers that are set and have not run or been cancelled.
func (c *Clock) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(liveTimers(c.timers))
}
