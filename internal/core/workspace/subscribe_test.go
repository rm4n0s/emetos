package workspace

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
)

// subscribed is a subscription of a watcher with the events it got.
type subscribed struct {
	mu     sync.Mutex
	events []Event
}

func (s *subscribed) on(events []Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, events...)
}

func (s *subscribed) all() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.events)
}

func (s *subscribed) has(e Event) bool { return slices.Contains(s.all(), e) }

// TestSubscribeGetsTheEventsBelowItsRoot runs with inotify and with polling: a subscription to a folder outside the window's
// folder gets its events, honours its excludes, and does not send them to OnEvents.
func TestSubscribeGetsTheEventsBelowItsRoot(t *testing.T) {
	pollingModes(t, func(t *testing.T, opts WatcherOptions) {
		a := startWatching(t, opts, Target{}, "own.txt")
		other := tree(t, "a/keep.txt", "objects/x/y.txt")
		var sub subscribed
		stop, err := a.w.Subscribe(context.Background(), Target{Path: other, Recursive: true, Exclude: []string{"objects"}}, sub.on)
		if err != nil {
			t.Fatal(err)
		}
		defer stop()
		mustWriteFile(t, filepath.Join(other, "objects", "x", "z.txt"), "excluded")
		mustWriteFile(t, filepath.Join(other, "a", "new.txt"), "x")
		mustWriteFile(t, filepath.Join(other, "a", "keep.txt"), "changed and longer")
		waitFor(t, "the events of the subscription", func() bool {
			return sub.has(Event{Kind: EventCreated, Path: filepath.Join(other, "a", "new.txt")}) &&
				sub.has(Event{Kind: EventChanged, Path: filepath.Join(other, "a", "keep.txt")})
		})
		for _, e := range sub.all() {
			if filepath.Dir(e.Path) == filepath.Join(other, "objects", "x") || e.Path == filepath.Join(other, "objects") {
				t.Errorf("an excluded path was reported: %+v", e)
			}
		}
		// the window's own target is not told, and its events do not reach the subscription
		a.sentinel("own-sentinel.txt")
		a.without(filepath.Join(other, "a", "new.txt"), filepath.Join(other, "a", "keep.txt"))
		for _, e := range sub.all() {
			if e.Path == a.path("own-sentinel.txt") {
				t.Errorf("the subscription got an event of another folder: %+v", e)
			}
		}
	})
}

// TestSubscribeAlsoGetsTheEventsOfAFolderThatTheWindowWatches covers the git directory inside the folder of the window: both
// the window and the subscription get an event below both roots.
func TestSubscribeAlsoGetsTheEventsOfAFolderThatTheWindowWatches(t *testing.T) {
	pollingModes(t, func(t *testing.T, opts WatcherOptions) {
		a := startWatching(t, opts, Target{}, "sub/f.txt")
		var sub subscribed
		stop, err := a.w.Subscribe(context.Background(), Target{Path: a.path("sub"), Recursive: true}, sub.on)
		if err != nil {
			t.Fatal(err)
		}
		defer stop()
		a.mustWrite(a.path("sub", "g.txt"), "x")
		a.mustWrite(a.path("top.txt"), "x")
		want := Event{Kind: EventCreated, Path: a.path("sub", "g.txt")}
		a.waitFor(want)
		a.waitFor(Event{Kind: EventCreated, Path: a.path("top.txt")})
		waitFor(t, "the subscription", func() bool { return sub.has(want) })
		if sub.has(Event{Kind: EventCreated, Path: a.path("top.txt")}) {
			t.Error("the subscription got an event outside its root")
		}
	})
}

func TestSubscribeStopsWhenUnwatched(t *testing.T) {
	pollingModes(t, func(t *testing.T, opts WatcherOptions) {
		a := startWatching(t, opts, Target{}, "own.txt")
		other := tree(t, "f.txt")
		var sub subscribed
		stop, err := a.w.Subscribe(context.Background(), Target{Path: other, Recursive: true}, sub.on)
		if err != nil {
			t.Fatal(err)
		}
		mustWriteFile(t, filepath.Join(other, "one.txt"), "x")
		waitFor(t, "the first event", func() bool { return sub.has(Event{Kind: EventCreated, Path: filepath.Join(other, "one.txt")}) })
		stop()
		n := len(sub.all())
		mustWriteFile(t, filepath.Join(other, "two.txt"), "x")
		a.sentinel("after.txt")
		time.Sleep(100 * time.Millisecond)
		if len(sub.all()) != n {
			t.Errorf("events after the subscription stopped: %+v", sub.all()[n:])
		}
	})
}

func TestSubscribeErrors(t *testing.T) {
	a := startWatching(t, WatcherOptions{ForcePolling: true}, Target{}, "f.txt")
	tests := []struct {
		name   string
		target Target
		sink   func([]Event)
		route  string
	}{
		{"no function", Target{Path: a.root}, nil, "Watcher.Subscribe.WatcherSinkMissing"},
		{"relative path", Target{Path: "relative"}, func([]Event) {}, "Watcher.Subscribe->Watcher.Watch.WatcherPathInvalid"},
		{"missing folder", Target{Path: filepath.Join(a.root, "nope")}, func([]Event) {}, "Watcher.Subscribe->Watcher.Watch.WatcherPathNotFound"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := a.w.Subscribe(context.Background(), tc.target, tc.sink)
			appErr, ok := errors.FromError(err)
			if !ok || !appErr.HasRoute(tc.route) {
				t.Errorf("got %v, want the route %s", err, tc.route)
			}
		})
	}
}

func mustWriteFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
