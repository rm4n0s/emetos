package workspace

import (
	"os"
	"sync"
	"syscall"
	"testing"

	"github.com/rm4n0s/errors"
	"golang.org/x/sys/unix"
)

// realAddWatch is the system call the tests wrap.
var realAddWatch = unix.InotifyAddWatch

func TestWatcherPollingFallbackWhenInotifyCannotStart(t *testing.T) {
	opts := WatcherOptions{inotifyInit: func(int) (int, error) { return -1, syscall.EMFILE }}
	a := startWatching(t, opts, Target{}, "f.txt")
	waitFor(t, "the notice", func() bool { return len(a.noticesSeen()) == 1 })
	n := a.noticesSeen()[0]
	if n.Kind != NoticePollingFallback || n.Message == "" {
		t.Errorf("notice %+v", n)
	}
	appErr, ok := errors.FromError(n.Err)
	if !ok || !appErr.HasRoute("newInotify.WatcherInitFailed") || !errors.Is(n.Err, syscall.EMFILE) {
		t.Errorf("the error of the notice: %v", n.Err)
	}
	a.mustWrite(a.path("f.txt"), "polled change")
	a.waitFor(Event{Kind: EventChanged, Path: a.path("f.txt")})
}

func TestWatcherPollingFallbackWhenTheLimitOfWatchesIsReached(t *testing.T) {
	var mu sync.Mutex
	used := 0
	opts := WatcherOptions{inotifyAdd: func(fd int, path string, mask uint32) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		if used >= 2 {
			return -1, syscall.ENOSPC
		}
		used++
		return realAddWatch(fd, path, mask)
	}}
	a := startWatching(t, opts, Target{}, "d1/f.txt", "d2/f.txt", "d3/f.txt")
	waitFor(t, "the notice", func() bool { return len(a.noticesSeen()) == 1 })
	if n := a.noticesSeen()[0]; n.Kind != NoticeWatchLimit || !errors.Is(n.Err, syscall.ENOSPC) {
		t.Errorf("notice %+v", n)
	}
	// the whole root is polled now, not only the folder that did not get a watch
	a.mustWrite(a.path("d1", "f.txt"), "polled change")
	a.waitFor(Event{Kind: EventChanged, Path: a.path("d1", "f.txt")})
	a.mustWrite(a.path("d3", "g.txt"), "x")
	a.waitFor(Event{Kind: EventCreated, Path: a.path("d3", "g.txt")})
	a.w.mu.Lock()
	left := len(a.w.ino.wds)
	a.w.mu.Unlock()
	if left != 0 {
		t.Errorf("%d inotify watches are left for a root that is polled", left)
	}
	if n := len(a.noticesSeen()); n != 1 {
		t.Errorf("%d notices, want one", n)
	}
}

func TestWatcherLimitReachedWhileWatching(t *testing.T) {
	var mu sync.Mutex
	used := 0
	limit := 100
	opts := WatcherOptions{inotifyAdd: func(fd int, path string, mask uint32) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		if used >= limit {
			return -1, syscall.ENOSPC
		}
		used++
		return realAddWatch(fd, path, mask)
	}}
	a := startWatching(t, opts, Target{}, "d1/")
	mu.Lock()
	limit = used // no watch is left
	mu.Unlock()
	a.must(os.Mkdir(a.path("d2"), 0o755))
	waitFor(t, "the notice", func() bool { return len(a.noticesSeen()) == 1 })
	// the root is polled from now on, and the consumers are told to read everything again
	a.waitFor(Event{Kind: EventRescan, Path: a.root, IsDir: true})
	a.mustWrite(a.path("d2", "late.txt"), "x")
	a.waitFor(Event{Kind: EventCreated, Path: a.path("d2", "late.txt")})
}

func TestWatcherQueueOverflowAsksForARescan(t *testing.T) {
	a := startWatching(t, WatcherOptions{}, Target{})
	a.w.mu.Lock()
	var out []rawEvent
	a.w.handleOne(-1, 0x4000, 0, "", map[uint32]string{}, &out) // IN_Q_OVERFLOW
	a.w.mu.Unlock()
	a.w.send(out)
	a.waitFor(Event{Kind: EventRescan, Path: a.root, IsDir: true})
}
