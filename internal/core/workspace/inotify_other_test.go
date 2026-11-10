//go:build !linux

package workspace

import "testing"

// Outside Linux the watcher polls by design: it still reports the changes, and it sends no notice about it.
func TestWatcherPollsWithoutANoticeOutsideLinux(t *testing.T) {
	a := startWatching(t, WatcherOptions{}, Target{}, "f.txt")
	if !a.w.Polling() {
		t.Fatal("the watcher should poll where there is no native file watching")
	}
	a.mustWrite(a.path("f.txt"), "polled change")
	a.waitFor(Event{Kind: EventChanged, Path: a.path("f.txt")})
	if n := a.noticesSeen(); len(n) != 0 {
		t.Errorf("notices %+v, want none", n)
	}
}
