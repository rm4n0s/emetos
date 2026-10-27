package transport

import "time"

// Clock is the time the transports use for their timeouts and timers: the fallback to SSH, the wait for a first link, the
// delay before a client asks for lost tiles, the retries and the refresh of the picture flow. Tests give a clock they move by
// hand.
type Clock interface {
	// Now is the current time.
	Now() time.Time
	// AfterFunc calls f, on a goroutine of its own, after d, and returns the function that stops it (true when it stopped it
	// before it ran).
	AfterFunc(d time.Duration, f func()) (stop func() bool)
}

// RealClock is the clock of the machine.
type RealClock struct{}

// Now implements Clock.
func (RealClock) Now() time.Time { return time.Now() }

// AfterFunc implements Clock.
func (RealClock) AfterFunc(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop }

// FuncClock makes a Clock of a function that tells the time and one that sets a timer, the two a Hub has.
type FuncClock struct {
	// NowFunc gives the time; time.Now when nil.
	NowFunc func() time.Time
	// After sets a timer; time.AfterFunc when nil.
	After func(d time.Duration, f func()) (stop func() bool)
}

// Now implements Clock.
func (c FuncClock) Now() time.Time {
	if c.NowFunc == nil {
		return time.Now()
	}
	return c.NowFunc()
}

// AfterFunc implements Clock.
func (c FuncClock) AfterFunc(d time.Duration, f func()) func() bool {
	if c.After == nil {
		return time.AfterFunc(d, f).Stop
	}
	return c.After(d, f)
}
