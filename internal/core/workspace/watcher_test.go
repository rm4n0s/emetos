package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/fileio"
)

// watched is a watcher started on a folder, with the events it reported.
type watched struct {
	t       *testing.T
	root    string
	w       *Watcher
	cancel  context.CancelFunc
	mu      sync.Mutex
	events  []Event
	notices []Notice
}

func (a *watched) onEvents(events []Event) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.events = append(a.events, events...)
}

func (a *watched) onNotice(n Notice) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.notices = append(a.notices, n)
}

func (a *watched) all() []Event {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.events)
}

func (a *watched) noticesSeen() []Notice {
	a.mu.Lock()
	defer a.mu.Unlock()
	return slices.Clone(a.notices)
}

// startWatching makes a folder with some entries and a recursive watcher on it.
func startWatching(t *testing.T, opts WatcherOptions, target Target, entries ...string) *watched {
	t.Helper()
	root := tree(t, entries...)
	a := &watched{t: t, root: root}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	opts.OnEvents, opts.OnNotice = a.onEvents, a.onNotice
	if opts.PollInterval == 0 {
		opts.PollInterval = 20 * time.Millisecond
	}
	a.w = NewWatcher(ctx, opts)
	t.Cleanup(func() {
		cancel()
		select {
		case <-a.w.Done():
		case <-time.After(5 * time.Second):
			t.Error("the watcher did not stop")
		}
	})
	if target.Path == "" {
		target.Path = root
	}
	target.Recursive = target.Recursive || !target.File
	if _, err := a.w.Watch(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	return a
}

func (a *watched) path(parts ...string) string {
	return filepath.Join(append([]string{a.root}, parts...)...)
}

// sentinel writes a file and waits until its event is seen: everything that happened before it
// and was going to be reported has been reported by then (the batches are in order).
func (a *watched) sentinel(name string) {
	a.t.Helper()
	p := a.path(name)
	if err := os.WriteFile(p, []byte("s"), 0o644); err != nil {
		a.t.Fatal(err)
	}
	a.waitFor(Event{Kind: EventCreated, Path: p})
}

// waitFor waits until an event was reported.
func (a *watched) waitFor(want Event) {
	a.t.Helper()
	waitFor(a.t, fmt.Sprintf("%+v (got %+v)", want, a.all()), func() bool { return slices.Contains(a.all(), want) })
}

// without checks that no event for a path was reported.
func (a *watched) without(paths ...string) {
	a.t.Helper()
	for _, e := range a.all() {
		for _, p := range paths {
			if e.Path == p || e.OldPath == p {
				a.t.Errorf("unexpected event %+v", e)
			}
		}
	}
}

func (a *watched) mustWrite(path, text string) {
	a.t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		a.t.Fatal(err)
	}
}

func (a *watched) must(err error) {
	a.t.Helper()
	if err != nil {
		a.t.Fatal(err)
	}
}

// pollingModes runs a test with inotify and with forced polling: the events are the same.
func pollingModes(t *testing.T, fn func(t *testing.T, opts WatcherOptions)) {
	t.Run("inotify", func(t *testing.T) { fn(t, WatcherOptions{}) })
	t.Run("polling", func(t *testing.T) { fn(t, WatcherOptions{ForcePolling: true}) })
}

func TestWatcherCreateChangeDelete(t *testing.T) {
	pollingModes(t, func(t *testing.T, opts WatcherOptions) {
		a := startWatching(t, opts, Target{}, "existing.txt", "sub/")
		p := a.path("new.txt")
		a.mustWrite(p, "one")
		a.waitFor(Event{Kind: EventCreated, Path: p})

		a.mustWrite(a.path("existing.txt"), "changed content")
		a.waitFor(Event{Kind: EventChanged, Path: a.path("existing.txt")})

		a.must(os.Remove(p))
		a.waitFor(Event{Kind: EventDeleted, Path: p})

		sub := a.path("sub", "in.txt")
		a.mustWrite(sub, "x")
		a.waitFor(Event{Kind: EventCreated, Path: sub})
	})
}

func TestWatcherRenames(t *testing.T) {
	t.Run("inotify pairs a rename inside the folder", func(t *testing.T) {
		a := startWatching(t, WatcherOptions{}, Target{}, "old.txt", "dir/", "outside_target/")
		a.must(os.Rename(a.path("old.txt"), a.path("new.txt")))
		a.waitFor(Event{Kind: EventRenamed, OldPath: a.path("old.txt"), Path: a.path("new.txt")})
		// into another folder of the tree
		a.must(os.Rename(a.path("new.txt"), a.path("dir", "moved.txt")))
		a.waitFor(Event{Kind: EventRenamed, OldPath: a.path("new.txt"), Path: a.path("dir", "moved.txt")})
	})
	t.Run("inotify reports a folder renamed", func(t *testing.T) {
		a := startWatching(t, WatcherOptions{}, Target{}, "d/f.txt")
		a.must(os.Rename(a.path("d"), a.path("e")))
		a.waitFor(Event{Kind: EventRenamed, OldPath: a.path("d"), Path: a.path("e"), IsDir: true})
		// what is below it is still watched, under its new path
		a.mustWrite(a.path("e", "f.txt"), "changed")
		a.waitFor(Event{Kind: EventChanged, Path: a.path("e", "f.txt")})
		a.mustWrite(a.path("e", "g.txt"), "new")
		a.waitFor(Event{Kind: EventCreated, Path: a.path("e", "g.txt")})
	})
	t.Run("inotify reports a move out of the folder as a delete and a move in as a create", func(t *testing.T) {
		a := startWatching(t, WatcherOptions{}, Target{}, "leaving.txt", "dir/x.txt")
		away := t.TempDir()
		a.must(os.Rename(a.path("leaving.txt"), filepath.Join(away, "leaving.txt")))
		a.waitFor(Event{Kind: EventDeleted, Path: a.path("leaving.txt")})
		a.must(os.Rename(filepath.Join(away, "leaving.txt"), a.path("back.txt")))
		a.waitFor(Event{Kind: EventCreated, Path: a.path("back.txt")})
		// a folder moved out is no longer watched
		a.must(os.Rename(a.path("dir"), filepath.Join(away, "dir")))
		a.waitFor(Event{Kind: EventDeleted, Path: a.path("dir"), IsDir: true})
		a.mustWrite(filepath.Join(away, "dir", "x.txt"), "after")
		a.sentinel("done.txt")
		a.without(a.path("dir", "x.txt"), filepath.Join(away, "dir", "x.txt"))
	})
	t.Run("polling sees a rename as a delete and a create", func(t *testing.T) {
		a := startWatching(t, WatcherOptions{ForcePolling: true}, Target{}, "old.txt")
		a.must(os.Rename(a.path("old.txt"), a.path("new.txt")))
		a.waitFor(Event{Kind: EventDeleted, Path: a.path("old.txt")})
		a.waitFor(Event{Kind: EventCreated, Path: a.path("new.txt")})
	})
}

func TestWatcherReplaceByRenameIsAChange(t *testing.T) {
	a := startWatching(t, WatcherOptions{}, Target{}, "settings.json")
	target := a.path("settings.json")
	for i := 0; i < 3; i++ {
		// what editors and fileio.Write do: write a temporary file and rename it over the target
		tmp := a.path(fmt.Sprintf(".settings.json.tmp%d", i))
		a.mustWrite(tmp, strings.Repeat("x", 100+i))
		a.must(os.Rename(tmp, target))
		a.waitFor(Event{Kind: EventChanged, Path: target})
		a.sentinel(fmt.Sprintf("sentinel%d", i))
		a.without(tmp)
		for _, e := range a.all() {
			if e.Kind == EventRenamed || e.Kind == EventDeleted {
				t.Fatalf("unexpected event %+v", e)
			}
		}
	}
}

func TestWatcherAtomicSaveByFileio(t *testing.T) {
	a := startWatching(t, WatcherOptions{}, Target{}, "doc.txt")
	if _, err := fileio.Write(context.Background(), a.path("doc.txt"), "saved text", fileio.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	a.waitFor(Event{Kind: EventChanged, Path: a.path("doc.txt")})
	a.sentinel("sentinel")
	for _, e := range a.all() {
		if e.Path != a.path("doc.txt") && e.Path != a.path("sentinel") {
			t.Errorf("unexpected event %+v (the temporary file leaked)", e)
		}
	}
}

func TestWatcherOwnWritesAreIgnored(t *testing.T) {
	a := startWatching(t, WatcherOptions{}, Target{}, "doc.txt")
	stat, err := fileio.Write(context.Background(), a.path("doc.txt"), "the editor saved this", fileio.WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	a.w.Suppress(a.path("doc.txt"), stat)
	a.sentinel("sentinel")
	a.without(a.path("doc.txt"))
	// a change made by someone else afterwards is reported
	a.mustWrite(a.path("doc.txt"), "somebody else wrote this, and it is longer")
	a.waitFor(Event{Kind: EventChanged, Path: a.path("doc.txt")})
}

func TestWatcherNewFoldersAreWatched(t *testing.T) {
	a := startWatching(t, WatcherOptions{}, Target{})
	a.must(os.MkdirAll(a.path("a", "b", "c"), 0o755))
	a.waitFor(Event{Kind: EventCreated, Path: a.path("a"), IsDir: true})
	a.waitFor(Event{Kind: EventCreated, Path: a.path("a", "b", "c"), IsDir: true})
	// the deepest folder was created in one go, and a file in it is seen
	deep := a.path("a", "b", "c", "f.txt")
	a.mustWrite(deep, "x")
	a.waitFor(Event{Kind: EventCreated, Path: deep})
	a.mustWrite(deep, "xy")
	a.waitFor(Event{Kind: EventChanged, Path: deep})
}

func TestWatcherAFolderThatAppearsWithFiles(t *testing.T) {
	// a folder moved in from elsewhere comes with its files: they are watched
	a := startWatching(t, WatcherOptions{}, Target{})
	src := t.TempDir()
	a.mustWrite(filepath.Join(src, "f.txt"), "x")
	a.must(os.Mkdir(filepath.Join(src, "g"), 0o755))
	a.must(os.Rename(src, a.path("incoming")))
	a.waitFor(Event{Kind: EventCreated, Path: a.path("incoming"), IsDir: true})
	a.mustWrite(a.path("incoming", "f.txt"), "changed")
	a.waitFor(Event{Kind: EventChanged, Path: a.path("incoming", "f.txt")})
	a.mustWrite(a.path("incoming", "g", "h.txt"), "new")
	a.waitFor(Event{Kind: EventCreated, Path: a.path("incoming", "g", "h.txt")})
}

func TestWatcherExcludedFoldersAreNotWatched(t *testing.T) {
	pollingModes(t, func(t *testing.T, opts WatcherOptions) {
		a := startWatching(t, opts, Target{Exclude: []string{"node_modules/**", "*/.git/objects/**", "build"}},
			"node_modules/pkg/index.js", "pkg/.git/objects/ab/cd", "pkg/src/x.go", "build/out.o")
		if opts.ForcePolling {
			a.mustWrite(a.path("node_modules", "pkg", "index.js"), "changed!")
		} else {
			a.w.mu.Lock()
			var watchedPaths []string
			for _, d := range a.w.ino.wds {
				watchedPaths = append(watchedPaths, d.path)
			}
			a.w.mu.Unlock()
			slices.Sort(watchedPaths)
			want := []string{a.root, a.path("pkg"), a.path("pkg", ".git"), a.path("pkg", "src")}
			if !slices.Equal(watchedPaths, want) {
				t.Errorf("watched folders\n got %q\nwant %q", watchedPaths, want)
			}
			a.mustWrite(a.path("node_modules", "pkg", "index.js"), "changed!")
		}
		a.mustWrite(a.path("pkg", "src", "x.go"), "package x // changed")
		a.waitFor(Event{Kind: EventChanged, Path: a.path("pkg", "src", "x.go")})
		a.mustWrite(a.path("build", "new.o"), "x")
		a.sentinel("sentinel")
		a.without(a.path("node_modules", "pkg", "index.js"), a.path("build", "new.o"))
		// an event of an excluded path is not reported even when its folder is watched
		a.must(os.Mkdir(a.path("build", "sub"), 0o755))
		a.mustWrite(a.path("pkg", "src", "y.go"), "y")
		a.waitFor(Event{Kind: EventCreated, Path: a.path("pkg", "src", "y.go")})
		a.without(a.path("build", "sub"))
	})
}

func TestWatcherAbsoluteExcludes(t *testing.T) {
	root := tree(t, "keep/", "skip/sub/")
	a := &watched{t: t, root: root}
	ctx, cancel := context.WithCancel(context.Background())
	w := NewWatcher(ctx, WatcherOptions{OnEvents: a.onEvents})
	t.Cleanup(func() { cancel(); <-w.Done() })
	a.w = w
	if _, err := w.Watch(context.Background(), Target{Path: root, Recursive: true, Exclude: []string{filepath.Join(root, "skip")}}); err != nil {
		t.Fatal(err)
	}
	a.mustWrite(a.path("skip", "sub", "f"), "x")
	a.mustWrite(a.path("keep", "f"), "x")
	a.waitFor(Event{Kind: EventCreated, Path: a.path("keep", "f")})
	a.without(a.path("skip", "sub", "f"))
}

func TestWatcherFilesOutsideTheFolder(t *testing.T) {
	pollingModes(t, func(t *testing.T, opts WatcherOptions) {
		outside := t.TempDir()
		file := filepath.Join(outside, "open.txt")
		neighbour := filepath.Join(outside, "neighbour.txt")
		for _, p := range []string{file, neighbour} {
			if err := os.WriteFile(p, []byte("1"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		a := startWatching(t, opts, Target{Path: file, File: true})
		a.root = outside
		// only the file is reported, not its neighbours
		a.mustWrite(neighbour, "neighbour changed")
		a.mustWrite(file, "the file changed")
		a.waitFor(Event{Kind: EventChanged, Path: file})
		a.without(neighbour)
		// replaced by rename, as an atomic save does
		tmp := filepath.Join(outside, ".open.txt.tmp")
		a.mustWrite(tmp, "replacement content")
		a.must(os.Rename(tmp, file))
		waitFor(t, "a second change", func() bool {
			n := 0
			for _, e := range a.all() {
				if e == (Event{Kind: EventChanged, Path: file}) {
					n++
				}
			}
			return n >= 2
		})
		a.must(os.Remove(file))
		a.waitFor(Event{Kind: EventDeleted, Path: file})
		a.without(neighbour, tmp)
	})
}

func TestWatcherUnwatch(t *testing.T) {
	a := startWatching(t, WatcherOptions{}, Target{})
	other := t.TempDir()
	unwatch, err := a.w.Watch(context.Background(), Target{Path: other, Recursive: true})
	if err != nil {
		t.Fatal(err)
	}
	a.mustWrite(filepath.Join(other, "one"), "x")
	a.waitFor(Event{Kind: EventCreated, Path: filepath.Join(other, "one")})
	unwatch()
	a.mustWrite(filepath.Join(other, "two"), "x")
	a.sentinel("sentinel")
	a.without(filepath.Join(other, "two"))
	a.w.mu.Lock()
	for _, d := range a.w.ino.wds {
		if strings.HasPrefix(d.path, other) {
			t.Errorf("%s is still watched", d.path)
		}
	}
	a.w.mu.Unlock()
}

func TestWatcherTwoRootsShareAFolder(t *testing.T) {
	a := startWatching(t, WatcherOptions{}, Target{}, "f.txt")
	unwatch, err := a.w.Watch(context.Background(), Target{Path: a.path("f.txt"), File: true})
	if err != nil {
		t.Fatal(err)
	}
	unwatch() // the folder is still watched for the first root
	a.mustWrite(a.path("f.txt"), "changed")
	a.waitFor(Event{Kind: EventChanged, Path: a.path("f.txt")})
}

func TestWatcherStopsWithItsContext(t *testing.T) {
	a := startWatching(t, WatcherOptions{}, Target{}, "f.txt")
	a.mustWrite(a.path("g.txt"), "x")
	a.waitFor(Event{Kind: EventCreated, Path: a.path("g.txt")})
	a.cancel()
	select {
	case <-a.w.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the watcher did not stop")
	}
	n := len(a.all())
	a.mustWrite(a.path("after.txt"), "x")
	time.Sleep(150 * time.Millisecond)
	if len(a.all()) != n {
		t.Error("an event was reported after the watcher stopped")
	}
	_, err := a.w.Watch(context.Background(), Target{Path: a.root, Recursive: true})
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("Watcher.Watch.WatcherStopped") {
		t.Errorf("got %v", err)
	}
}

func TestWatcherPollingDetectsChanges(t *testing.T) {
	a := startWatching(t, WatcherOptions{ForcePolling: true}, Target{}, "a/b/old.txt", "keep.txt")
	if len(a.noticesSeen()) != 0 {
		t.Errorf("a forced polling sent a notice: %+v", a.noticesSeen())
	}
	a.mustWrite(a.path("a", "b", "new.txt"), "x")
	a.waitFor(Event{Kind: EventCreated, Path: a.path("a", "b", "new.txt")})
	a.mustWrite(a.path("keep.txt"), "a different size")
	a.waitFor(Event{Kind: EventChanged, Path: a.path("keep.txt")})
	a.must(os.RemoveAll(a.path("a")))
	a.waitFor(Event{Kind: EventDeleted, Path: a.path("a"), IsDir: true})
	// the deletes of what was below it are dropped
	a.without(a.path("a", "b"), a.path("a", "b", "old.txt"))
}

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

func TestWatchErrors(t *testing.T) {
	root := tree(t, "file.txt", "dir/")
	ctx, cancel := context.WithCancel(context.Background())
	w := NewWatcher(ctx, WatcherOptions{})
	t.Cleanup(func() { cancel(); <-w.Done() })
	tests := []struct {
		name   string
		target Target
		route  string
	}{
		{"relative", Target{Path: "relative/dir"}, "Watcher.Watch.WatcherPathInvalid"},
		{"missing", Target{Path: filepath.Join(root, "nope"), Recursive: true}, "Watcher.Watch.WatcherPathNotFound"},
		{"a file as a folder", Target{Path: filepath.Join(root, "file.txt")}, "Watcher.Watch.WatcherPathInvalid"},
		{"a file in a missing folder", Target{Path: filepath.Join(root, "nope", "f"), File: true}, "Watcher.Watch.WatcherPathNotFound"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := w.Watch(context.Background(), tc.target)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

func TestWatchAddFailed(t *testing.T) {
	root := tree(t, "locked/")
	locked := filepath.Join(root, "locked")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	if _, err := os.ReadDir(locked); err == nil {
		t.Skip("permissions are not enforced (running as root?)")
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := NewWatcher(ctx, WatcherOptions{})
	t.Cleanup(func() { cancel(); <-w.Done() })
	_, err := w.Watch(context.Background(), Target{Path: locked})
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !appErr.HasRoute("Watcher.Watch->Watcher.addRootLocked.WatcherAddFailed") {
		t.Errorf("unexpected failure path: %s", appErr.Route())
	}
	// the root that failed is not kept
	w.mu.Lock()
	n := len(w.roots)
	w.mu.Unlock()
	if n != 0 {
		t.Errorf("%d roots after the failure", n)
	}
}

func TestWatchStatFailed(t *testing.T) {
	root := tree(t, "closed/inner/")
	closed := filepath.Join(root, "closed")
	if err := os.Chmod(closed, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(closed, 0o755) })
	if _, err := os.Stat(filepath.Join(closed, "inner")); err == nil {
		t.Skip("permissions are not enforced (running as root?)")
	}
	ctx, cancel := context.WithCancel(context.Background())
	w := NewWatcher(ctx, WatcherOptions{})
	t.Cleanup(func() { cancel(); <-w.Done() })
	_, err := w.Watch(context.Background(), Target{Path: filepath.Join(closed, "inner")})
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("Watcher.Watch.WatcherStatFailed") {
		t.Errorf("got %v", err)
	}
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

func TestWatcherEventsReachTheWorkspace(t *testing.T) {
	root := tree(t, "a/f.txt")
	w, err := Open(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := w.Root().Children(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Find(filepath.Join(root, "a")).Children(ctx); err != nil {
		t.Fatal(err)
	}
	a := &watched{t: t, root: root}
	wctx, cancel := context.WithCancel(context.Background())
	a.w = NewWatcher(wctx, WatcherOptions{OnEvents: func(events []Event) {
		w.Apply(ctx, events)
		a.onEvents(events)
	}})
	t.Cleanup(func() { cancel(); <-a.w.Done() })
	if _, err := a.w.Watch(context.Background(), Target{Path: root, Recursive: true}); err != nil {
		t.Fatal(err)
	}
	a.mustWrite(a.path("a", "new.txt"), "x")
	waitFor(t, "the new file in the tree", func() bool { return w.Find(a.path("a", "new.txt")) != nil })
	a.must(os.Rename(a.path("a", "new.txt"), a.path("renamed.txt")))
	waitFor(t, "the rename in the tree", func() bool {
		return w.Find(a.path("renamed.txt")) != nil && w.Find(a.path("a", "new.txt")) == nil
	})
	a.must(os.Remove(a.path("a", "f.txt")))
	waitFor(t, "the delete in the tree", func() bool { return w.Find(a.path("a", "f.txt")) == nil })
}
