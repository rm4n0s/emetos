package notify

import (
	"bytes"
	"slices"
	"testing"
	"time"
)

// fakeClock is a timer that runs when the test says time passed.
type fakeClock struct {
	now    time.Duration
	timers []*fakeTimer
}

type fakeTimer struct {
	at      time.Duration
	f       func()
	stopped bool
	fired   bool
}

func (c *fakeClock) after(d time.Duration, f func()) func() {
	t := &fakeTimer{at: c.now + d, f: f}
	c.timers = append(c.timers, t)
	return func() { t.stopped = true }
}

// advance moves time forward and runs the timers that are due, in order.
func (c *fakeClock) advance(d time.Duration) {
	target := c.now + d
	for {
		var next *fakeTimer
		for _, t := range c.timers {
			if !t.stopped && !t.fired && t.at <= target && (next == nil || t.at < next.at) {
				next = t
			}
		}
		if next == nil {
			break
		}
		c.now = next.at
		next.fired = true
		next.f()
	}
	c.now = target
}

// pending counts the timers that did not run and were not stopped.
func (c *fakeClock) pending() int {
	n := 0
	for _, t := range c.timers {
		if !t.stopped && !t.fired {
			n++
		}
	}
	return n
}

// newPresenter makes a presenter of a service whose clock is the fake one, bound on the spot.
func newPresenter(t *testing.T) (*Presenter, *Service, *fakeClock) {
	t.Helper()
	var buf bytes.Buffer
	svc, _ := newTestService(&buf)
	clock := &fakeClock{}
	// the service stamps items with the time of the fake clock, so that the spam protection sees it
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	svc.now = func() time.Time { return base.Add(clock.now) }
	p := NewPresenter(svc)
	unbind := p.Bind(nil, clock.after)
	t.Cleanup(unbind)
	return p, svc, clock
}

func ids(items []Item) []uint64 {
	out := make([]uint64, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

func TestToastsShowNewestFirst(t *testing.T) {
	p, svc, _ := newPresenter(t)
	a := svc.Info("", "a")
	b := svc.Warn("", "b")
	if got := ids(p.Toasts()); !slices.Equal(got, []uint64{b, a}) {
		t.Errorf("toasts %v, want newest first %v", got, []uint64{b, a})
	}
	if !p.ToastsVisible() {
		t.Error("toasts are visible")
	}
}

func TestAtMostThreeToastsShowAndTheOlderOnesWait(t *testing.T) {
	p, svc, clock := newPresenter(t)
	var all []uint64
	// one in a second, so that the spam protection leaves each of them a toast
	for _, m := range []string{"a", "b", "c", "d"} {
		all = append(all, svc.Info("", m))
		clock.advance(time.Second)
	}
	a, b, c, d := all[0], all[1], all[2], all[3]
	if got := ids(p.Toasts()); !slices.Equal(got, []uint64{d, c, b}) {
		t.Fatalf("toasts %v, want the newest three %v", got, []uint64{d, c, b})
	}
	// closing a visible one lets the waiting one show
	svc.Dismiss(c)
	if got := ids(p.Toasts()); !slices.Equal(got, []uint64{d, b, a}) {
		t.Errorf("toasts %v, want %v", got, []uint64{d, b, a})
	}
}

func TestSpamProtection(t *testing.T) {
	cases := []struct {
		name  string
		gaps  []time.Duration // time before each notification
		toast int
	}{
		{"four at once", []time.Duration{0, 0, 0, 0}, 3},
		{"four within the interval", []time.Duration{0, 200 * time.Millisecond, 200 * time.Millisecond, 200 * time.Millisecond}, 3},
		{"the fourth after the interval", []time.Duration{0, 0, 0, SpamInterval + time.Millisecond}, 4},
		{"two", []time.Duration{0, 10 * time.Millisecond}, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, svc, clock := newPresenter(t)
			var all []uint64
			for _, g := range tc.gaps {
				clock.advance(g)
				all = append(all, svc.Info("", "m"+string(rune('a'+len(all)))))
			}
			if got := len(p.toasts); got != tc.toast {
				t.Errorf("%d toasts made, want %d", got, tc.toast)
			}
			// every notification is in the service whatever the toasts are
			if len(svc.Items()) != len(tc.gaps) {
				t.Error("a notification was lost")
			}
		})
	}
}

func TestPurgeTimeouts(t *testing.T) {
	cases := []struct {
		name     string
		n        Notification
		timeout  time.Duration
		stays    bool // is still there after the timeout
		stayLong bool // is still there after a minute
	}{
		{"info", Notification{Severity: Info, Message: "m"}, 10 * time.Second, false, false},
		{"warning", Notification{Severity: Warning, Message: "m"}, 12 * time.Second, false, false},
		{"error", Notification{Severity: Error, Message: "m"}, 15 * time.Second, false, false},
		{"sticky", Notification{Severity: Info, Message: "m", Sticky: true}, 10 * time.Second, true, true},
		{"error with an action", Notification{Severity: Error, Message: "m", Actions: []Action{{Label: "Fix"}}}, 15 * time.Second, true, true},
		{"info with an action", Notification{Severity: Info, Message: "m", Actions: []Action{{Label: "Ok"}}}, 10 * time.Second, false, false},
		{"progress", Notification{Severity: Info, Message: "m", Progress: &Progress{Infinite: true}}, 10 * time.Second, true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, svc, clock := newPresenter(t)
			id := svc.Notify(tc.n)
			clock.advance(tc.timeout - time.Millisecond)
			if len(p.Toasts()) != 1 {
				t.Fatal("the toast went away before its time")
			}
			clock.advance(time.Millisecond)
			if got := len(p.Toasts()) == 1; got != tc.stays {
				t.Errorf("after %v the toast is there = %v, want %v", tc.timeout, got, tc.stays)
			}
			clock.advance(time.Minute)
			if got := len(p.Toasts()) == 1; got != tc.stayLong {
				t.Errorf("a minute later the toast is there = %v, want %v", got, tc.stayLong)
			}
			// the notification is in the center whatever the toast did
			if _, ok := svc.Get(id); !ok {
				t.Error("the notification went away with its toast")
			}
		})
	}
}

func TestAToastLeavesWhenItsProgressEnds(t *testing.T) {
	p, svc, clock := newPresenter(t)
	id := svc.Notify(Notification{Severity: Info, Message: "m", Progress: &Progress{Total: 10, Worked: 5}})
	clock.advance(time.Minute)
	if len(p.Toasts()) != 1 {
		t.Fatal("a toast with a running progress stays")
	}
	svc.UpdateProgress(id, Progress{Done: true})
	clock.advance(PurgeTimeout(Info))
	if len(p.Toasts()) != 0 {
		t.Error("the toast stays after its progress ended")
	}
}

func TestHoverAndFocusKeepAToast(t *testing.T) {
	cases := []struct {
		name string
		hold func(p *Presenter, id uint64)
		free func(p *Presenter, id uint64)
	}{
		{"hover", func(p *Presenter, id uint64) { p.SetHovered(id, true) }, func(p *Presenter, id uint64) { p.SetHovered(id, false) }},
		{"focus", func(p *Presenter, id uint64) { p.SetFocused(id, true) }, func(p *Presenter, id uint64) { p.SetFocused(id, false) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, svc, clock := newPresenter(t)
			id := svc.Info("", "m")
			clock.advance(5 * time.Second)
			tc.hold(p, id)
			clock.advance(time.Minute)
			if len(p.Toasts()) != 1 {
				t.Fatal("the toast went away while it was held")
			}
			tc.free(p, id)
			// the time starts again from the last check, so it goes within one timeout
			clock.advance(PurgeTimeout(Info))
			if len(p.Toasts()) != 0 {
				t.Error("the toast stays after it was let go")
			}
		})
	}
	// naming an item that has no toast does nothing
	p, _, _ := newPresenter(t)
	p.SetHovered(99, true)
	p.SetFocused(99, true)
}

func TestDismissStopsTheTimer(t *testing.T) {
	p, svc, clock := newPresenter(t)
	id := svc.Info("", "m")
	if clock.pending() != 1 {
		t.Fatalf("%d timers", clock.pending())
	}
	svc.Dismiss(id)
	if len(p.Toasts()) != 0 || clock.pending() != 0 {
		t.Errorf("toasts %d, timers %d after the item was dismissed", len(p.Toasts()), clock.pending())
	}
}

func TestSilentNotificationsHaveNoToast(t *testing.T) {
	p, svc, _ := newPresenter(t)
	svc.Notify(Notification{Message: "quiet", Silent: true})
	if p.ToastsVisible() {
		t.Error("a silent notification made a toast")
	}
	if len(svc.Items()) != 1 {
		t.Error("it is in the center")
	}
}

func TestDoNotDisturbFiltersToasts(t *testing.T) {
	p, svc, clock := newPresenter(t)
	before := svc.Info("", "before")
	if len(p.Toasts()) != 1 {
		t.Fatal("no toast")
	}
	p.ToggleDoNotDisturb()
	if !svc.DoNotDisturb() {
		t.Fatal("do not disturb is on")
	}
	if len(p.Toasts()) != 0 || clock.pending() != 0 {
		t.Error("turning it on hides the toasts that show")
	}
	clock.advance(2 * SpamInterval)
	svc.Info("", "info")
	svc.Warn("", "warning")
	e := svc.Error("", "error")
	if got := ids(p.Toasts()); !slices.Equal(got, []uint64{e}) {
		t.Errorf("toasts %v, want only the error %v", got, []uint64{e})
	}
	if len(svc.Items()) != 4 {
		t.Errorf("the center has %d notifications, want 4 (%d was there before)", len(svc.Items()), before)
	}
	p.ToggleDoNotDisturb()
	clock.advance(2 * SpamInterval)
	i := svc.Info("", "again")
	if got := ids(p.Toasts()); len(got) == 0 || got[0] != i {
		t.Errorf("toasts %v, want the new info on top", got)
	}
}

func TestTheCenterHidesToastsAndStopsNewOnes(t *testing.T) {
	p, svc, clock := newPresenter(t)
	svc.Info("", "a")
	var changes int
	p.OnChange(func() { changes++ })
	p.ShowCenter()
	if !p.CenterVisible() || len(p.Toasts()) != 0 || clock.pending() != 0 {
		t.Error("opening the center takes the toasts away")
	}
	clock.advance(2 * SpamInterval)
	svc.Info("", "b")
	if len(p.Toasts()) != 0 {
		t.Error("no toast shows while the center is open")
	}
	p.ShowCenter() // again: nothing changes
	p.HideCenter()
	if p.CenterVisible() {
		t.Error("the center is closed")
	}
	if len(p.Toasts()) != 0 {
		t.Error("the toasts that went away do not come back")
	}
	clock.advance(2 * SpamInterval)
	svc.Info("", "c")
	if len(p.Toasts()) != 1 {
		t.Error("new notifications toast again")
	}
	p.ToggleCenter()
	if !p.CenterVisible() {
		t.Error("toggle opens")
	}
	p.ToggleCenter()
	if p.CenterVisible() {
		t.Error("toggle closes")
	}
	if changes == 0 {
		t.Error("the listeners were not told")
	}
}

func TestHideToastsKeepsTheNotifications(t *testing.T) {
	p, svc, clock := newPresenter(t)
	svc.Info("", "a")
	p.HideToasts()
	if p.ToastsVisible() || clock.pending() != 0 || len(svc.Items()) != 1 {
		t.Error("hiding the toasts leaves the notification in the center")
	}
	p.HideToasts() // nothing to hide
}

func TestClearAllClosesTheCenterAndDismisses(t *testing.T) {
	p, svc, _ := newPresenter(t)
	svc.Info("", "a")
	svc.Error("", "b")
	p.ShowCenter()
	p.ClearAll()
	if p.CenterVisible() || len(svc.Items()) != 0 {
		t.Errorf("center %v, items %d", p.CenterVisible(), len(svc.Items()))
	}
}

func TestBindShowsWhatCameBefore(t *testing.T) {
	var buf bytes.Buffer
	svc, _ := newTestService(&buf)
	first := svc.Info("", "early")
	svc.Notify(Notification{Message: "quiet", Silent: true})
	clock := &fakeClock{}
	p := NewPresenter(svc)
	if len(p.Toasts()) != 0 {
		t.Error("an unbound presenter shows nothing")
	}
	unbind := p.Bind(nil, clock.after)
	if got := ids(p.Toasts()); !slices.Equal(got, []uint64{first}) {
		t.Errorf("toasts %v, want the early one %v", got, []uint64{first})
	}
	unbind()
	if p.ToastsVisible() || clock.pending() != 0 {
		t.Error("unbinding takes the toasts away")
	}
	svc.Info("", "later")
	if p.ToastsVisible() {
		t.Error("an unbound presenter does not follow the service")
	}
}

func TestBindPostsEventsToTheUIThread(t *testing.T) {
	var buf bytes.Buffer
	svc, _ := newTestService(&buf)
	var queue []func()
	p := NewPresenter(svc)
	p.Bind(func(f func()) { queue = append(queue, f) }, (&fakeClock{}).after)
	svc.Info("", "a")
	if p.ToastsVisible() || len(queue) != 1 {
		t.Fatalf("the event is handled on the thread of the sender: %d queued", len(queue))
	}
	queue[0]()
	if !p.ToastsVisible() {
		t.Error("the event was not applied")
	}
	// an item dismissed before its event ran gets no toast
	id := svc.Info("", "b")
	svc.Dismiss(id)
	for _, f := range queue[1:] {
		f()
	}
	if len(p.Toasts()) != 1 {
		t.Errorf("%d toasts, want 1", len(p.Toasts()))
	}
}

func TestPurgeTimeoutOfSeverities(t *testing.T) {
	if PurgeTimeout(Info) != 10*time.Second || PurgeTimeout(Warning) != 12*time.Second || PurgeTimeout(Error) != 15*time.Second {
		t.Error("VS Code's timeouts are 10, 12 and 15 seconds")
	}
}
