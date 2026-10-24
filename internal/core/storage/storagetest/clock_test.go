package storagetest

import (
	"slices"
	"testing"
	"time"
)

func TestClock(t *testing.T) {
	c := NewClock()
	var ran []string
	c.AfterFunc(2*time.Second, func() { ran = append(ran, "two") })
	stop := c.AfterFunc(time.Second, func() { ran = append(ran, "cancelled") })
	c.AfterFunc(time.Second, func() {
		ran = append(ran, "one")
		// a timer set while advancing runs in the same call when it is due
		c.AfterFunc(500*time.Millisecond, func() { ran = append(ran, "one and a half") })
	})
	if !stop() || stop() {
		t.Error("cancelling a timer must report true once")
	}
	if c.Pending() != 2 {
		t.Errorf("%d timers, want 2", c.Pending())
	}
	c.Advance(900 * time.Millisecond)
	if len(ran) != 0 {
		t.Fatalf("ran %v before the time", ran)
	}
	c.Advance(2 * time.Second)
	if want := []string{"one", "one and a half", "two"}; !slices.Equal(ran, want) {
		t.Errorf("ran %v, want %v", ran, want)
	}
	if c.Pending() != 0 {
		t.Errorf("%d timers left", c.Pending())
	}
}
