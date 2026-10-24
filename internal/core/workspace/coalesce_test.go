package workspace

import (
	"context"
	"os"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/fileio"
)

func TestCoalesce(t *testing.T) {
	c := func(p string) rawEvent { return rawEvent{kind: rawCreated, path: p} }
	m := func(p string) rawEvent { return rawEvent{kind: rawChanged, path: p} }
	d := func(p string) rawEvent { return rawEvent{kind: rawDeleted, path: p} }
	from := func(p string, cookie uint32) rawEvent { return rawEvent{kind: rawMovedFrom, path: p, cookie: cookie} }
	to := func(p string, cookie uint32) rawEvent { return rawEvent{kind: rawMovedTo, path: p, cookie: cookie} }
	created := func(p string) Event { return Event{Kind: EventCreated, Path: p} }
	changed := func(p string) Event { return Event{Kind: EventChanged, Path: p} }
	deleted := func(p string) Event { return Event{Kind: EventDeleted, Path: p} }
	renamed := func(old, p string) Event { return Event{Kind: EventRenamed, OldPath: old, Path: p} }

	tests := []struct {
		name string
		in   []rawEvent
		want []Event
	}{
		{"nothing", nil, nil},
		{"one of each", []rawEvent{c("/r/a"), m("/r/b"), d("/r/c")},
			[]Event{deleted("/r/c"), created("/r/a"), changed("/r/b")}},
		{"many writes are one change", []rawEvent{m("/r/a"), m("/r/a"), m("/r/a")}, []Event{changed("/r/a")}},
		{"created then changed stays created", []rawEvent{c("/r/a"), m("/r/a"), m("/r/a")}, []Event{created("/r/a")}},
		{"created then deleted cancels", []rawEvent{c("/r/a"), m("/r/a"), d("/r/a")}, nil},
		{"created, deleted, created again is created", []rawEvent{c("/r/a"), d("/r/a"), c("/r/a")}, []Event{created("/r/a")}},
		{"deleted then created is changed", []rawEvent{d("/r/a"), c("/r/a")}, []Event{changed("/r/a")}},
		{"deleted, created, changed is changed", []rawEvent{d("/r/a"), c("/r/a"), m("/r/a")}, []Event{changed("/r/a")}},
		{"changed then deleted is deleted", []rawEvent{m("/r/a"), d("/r/a")}, []Event{deleted("/r/a")}},
		{"deleted twice is deleted once", []rawEvent{d("/r/a"), d("/r/a")}, []Event{deleted("/r/a")}},
		{"a delete below a deleted folder is dropped, whatever the order",
			[]rawEvent{d("/r/dir/sub/f"), d("/r/dir/g"), d("/r/dir"), d("/r/other")},
			[]Event{deleted("/r/dir"), deleted("/r/other")}},
		{"a folder whose name starts like another is not its child",
			[]rawEvent{d("/r/dir"), d("/r/dir2/f")}, []Event{deleted("/r/dir"), deleted("/r/dir2/f")}},
		{"a rename", []rawEvent{from("/r/a", 7), to("/r/b", 7)}, []Event{renamed("/r/a", "/r/b")}},
		{"a rename of a folder", []rawEvent{{kind: rawMovedFrom, path: "/r/a", isDir: true, cookie: 1}, {kind: rawMovedTo, path: "/r/b", isDir: true, cookie: 1}},
			[]Event{{Kind: EventRenamed, OldPath: "/r/a", Path: "/r/b", IsDir: true}}},
		{"two renames keep their order", []rawEvent{from("/r/a", 1), to("/r/b", 1), from("/r/c", 2), to("/r/d", 2)},
			[]Event{renamed("/r/a", "/r/b"), renamed("/r/c", "/r/d")}},
		{"interleaved moves are paired by cookie", []rawEvent{from("/r/a", 1), from("/r/c", 2), to("/r/d", 2), to("/r/b", 1)},
			[]Event{renamed("/r/c", "/r/d"), renamed("/r/a", "/r/b")}},
		{"a move out of the folder is a delete", []rawEvent{from("/r/a", 3)}, []Event{deleted("/r/a")}},
		{"a move into the folder is a create", []rawEvent{to("/r/a", 3)}, []Event{created("/r/a")}},
		{"replace by rename: the temporary file is born and renamed over the target",
			[]rawEvent{c("/r/.tmp123"), m("/r/.tmp123"), m("/r/.tmp123"), from("/r/.tmp123", 9), to("/r/file.txt", 9)},
			[]Event{changed("/r/file.txt")}},
		{"replace by rename of a target that changed before",
			[]rawEvent{m("/r/file.txt"), c("/r/.tmp"), from("/r/.tmp", 9), to("/r/file.txt", 9)},
			[]Event{changed("/r/file.txt")}},
		{"a rename onto a watched path of one that is not watched is a change",
			[]rawEvent{{kind: rawMovedFrom, path: "/r/.tmp", cookie: 5, skip: true}, to("/r/file", 5)}, []Event{changed("/r/file")}},
		{"a rename of a watched path to one that is not watched is a delete",
			[]rawEvent{from("/r/file", 5), {kind: rawMovedTo, path: "/r/.x", cookie: 5, skip: true}}, []Event{deleted("/r/file")}},
		{"a rename between paths that are not watched is nothing",
			[]rawEvent{{kind: rawMovedFrom, path: "/r/.a", cookie: 5, skip: true}, {kind: rawMovedTo, path: "/r/.b", cookie: 5, skip: true}}, nil},
		{"moves of paths that are not watched and not paired are nothing",
			[]rawEvent{{kind: rawMovedFrom, path: "/r/.a", cookie: 5, skip: true}, {kind: rawMovedTo, path: "/r/.b", cookie: 6, skip: true}}, nil},
		{"a rename of a path that changed drops the stale change",
			[]rawEvent{m("/r/a"), from("/r/a", 4), to("/r/b", 4)}, []Event{renamed("/r/a", "/r/b")}},
		{"a rename and then a new file of the old name",
			[]rawEvent{from("/r/a", 4), to("/r/b", 4), c("/r/a")}, []Event{renamed("/r/a", "/r/b"), created("/r/a")}},
		{"rescans come last", []rawEvent{{kind: rawRescan, path: "/r"}, c("/r/a"), d("/r/b")},
			[]Event{deleted("/r/b"), created("/r/a"), {Kind: EventRescan, Path: "/r", IsDir: true}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			co := newCoalescer()
			for _, r := range tc.in {
				co.add(r)
			}
			got := co.flush()
			if !slices.Equal(got, tc.want) {
				t.Errorf("got  %+v\nwant %+v", got, tc.want)
			}
			if !co.empty() {
				t.Error("the coalescer is not empty after the flush")
			}
		})
	}
}

// fakeClock fires its timers when it is advanced.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Duration
	timers []*fakeTimer
}

type fakeTimer struct {
	at      time.Duration
	f       func()
	stopped bool
	fired   bool
	clock   *fakeClock
}

func (t *fakeTimer) Stop() bool {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	was := !t.stopped && !t.fired
	t.stopped = true
	return was
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) StopTimer {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{at: c.now + d, f: f, clock: c}
	c.timers = append(c.timers, t)
	return t
}

func (c *fakeClock) pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, t := range c.timers {
		if !t.stopped && !t.fired {
			n++
		}
	}
	return n
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now += d
	var due []*fakeTimer
	for _, t := range c.timers {
		if !t.stopped && !t.fired && t.at <= c.now {
			t.fired = true
			due = append(due, t)
		}
	}
	c.mu.Unlock()
	for _, t := range due {
		t.f()
	}
}

// waitFor polls a condition for up to five seconds.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// batches collects what a watcher reports.
type batches struct {
	mu  sync.Mutex
	all [][]Event
}

func (b *batches) add(events []Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.all = append(b.all, slices.Clone(events))
}

func (b *batches) get() [][]Event {
	b.mu.Lock()
	defer b.mu.Unlock()
	return slices.Clone(b.all)
}

func TestTheDebounceBatchesEventsUntilTheTimerFires(t *testing.T) {
	clock := &fakeClock{}
	var got batches
	ctx, cancel := context.WithCancel(context.Background())
	w := NewWatcher(ctx, WatcherOptions{OnEvents: got.add, Clock: clock, ForcePolling: true, PollInterval: time.Hour})
	t.Cleanup(func() { cancel(); <-w.Done() })

	w.send([]rawEvent{{kind: rawCreated, path: "/r/a"}, {kind: rawChanged, path: "/r/a"}})
	waitFor(t, "the timer to be set", func() bool { return clock.pending() == 1 })
	w.send([]rawEvent{{kind: rawChanged, path: "/r/b"}})
	// the second event waits for the same timer; nothing is reported before it fires
	clock.advance(DefaultDebounce - time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	if n := len(got.get()); n != 0 {
		t.Fatalf("%d batches before the timer fired", n)
	}
	if clock.pending() != 1 {
		t.Fatalf("%d timers, want the one of the batch", clock.pending())
	}
	clock.advance(time.Millisecond)
	waitFor(t, "the batch", func() bool { return len(got.get()) == 1 })
	want := []Event{{Kind: EventCreated, Path: "/r/a"}, {Kind: EventChanged, Path: "/r/b"}}
	if b := got.get()[0]; !slices.Equal(b, want) {
		t.Errorf("batch %+v, want %+v", b, want)
	}
	// the next event starts a new batch
	w.send([]rawEvent{{kind: rawDeleted, path: "/r/a"}})
	waitFor(t, "a new timer", func() bool { return clock.pending() == 1 })
	waitFor(t, "the events", func() bool { return len(w.raw) == 0 })
	clock.advance(DefaultDebounce)
	waitFor(t, "the second batch", func() bool { return len(got.get()) == 2 })
}

func TestABatchThatCancelsItselfIsNotReported(t *testing.T) {
	clock := &fakeClock{}
	var got batches
	ctx, cancel := context.WithCancel(context.Background())
	w := NewWatcher(ctx, WatcherOptions{OnEvents: got.add, Clock: clock, ForcePolling: true, PollInterval: time.Hour})
	t.Cleanup(func() { cancel(); <-w.Done() })

	w.send([]rawEvent{{kind: rawCreated, path: "/r/a"}, {kind: rawDeleted, path: "/r/a"}})
	waitFor(t, "the timer", func() bool { return clock.pending() == 1 })
	waitFor(t, "the events", func() bool { return len(w.raw) == 0 })
	clock.advance(DefaultDebounce)
	w.send([]rawEvent{{kind: rawChanged, path: "/r/sentinel"}})
	waitFor(t, "the timer", func() bool { return clock.pending() == 1 })
	waitFor(t, "the events", func() bool { return len(w.raw) == 0 })
	clock.advance(DefaultDebounce)
	waitFor(t, "the sentinel", func() bool { return len(got.get()) == 1 })
	if b := got.get()[0]; len(b) != 1 || b[0].Path != "/r/sentinel" {
		t.Errorf("batches: %+v", got.get())
	}
}

func TestOwnWritesAreDroppedByTheirFileStat(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/own.txt"
	other := dir + "/other.txt"
	for _, p := range []string{path, other} {
		if _, err := fileio.Write(context.Background(), p, "one", fileio.WriteOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	stat, err := fileio.Write(context.Background(), path, "two", fileio.WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}

	clock := &fakeClock{}
	var got batches
	ctx, cancel := context.WithCancel(context.Background())
	w := NewWatcher(ctx, WatcherOptions{OnEvents: got.add, Clock: clock, ForcePolling: true, PollInterval: time.Hour})
	t.Cleanup(func() { cancel(); <-w.Done() })
	w.Suppress(path, stat)

	flush := func(raws ...rawEvent) {
		t.Helper()
		before := len(got.get())
		w.send(raws)
		waitFor(t, "the timer", func() bool { return clock.pending() == 1 })
		// the channel is buffered: advance only when the batcher has taken every event, or the batch splits
		waitFor(t, "the events", func() bool { return len(w.raw) == 0 })
		clock.advance(DefaultDebounce)
		_ = before
	}
	// the write the editor made is dropped, another file's event is not
	flush(rawEvent{kind: rawChanged, path: path}, rawEvent{kind: rawChanged, path: other})
	waitFor(t, "the batch", func() bool { return len(got.get()) == 1 })
	if b := got.get()[0]; len(b) != 1 || b[0].Path != other {
		t.Fatalf("batch %+v: the own write was reported or the other file lost", b)
	}
	// an event for the file again, with the same stat, is dropped again
	flush(rawEvent{kind: rawChanged, path: path}, rawEvent{kind: rawChanged, path: other})
	waitFor(t, "the batch", func() bool { return len(got.get()) == 2 })
	if b := got.get()[1]; len(b) != 1 || b[0].Path != other {
		t.Fatalf("second batch %+v", b)
	}
	// an outside change (a different size) is reported and forgets the entry
	if err := writeFile(path, "three, longer"); err != nil {
		t.Fatal(err)
	}
	flush(rawEvent{kind: rawChanged, path: path})
	waitFor(t, "the batch", func() bool { return len(got.get()) == 3 })
	if b := got.get()[2]; len(b) != 1 || b[0].Path != path {
		t.Fatalf("third batch %+v", b)
	}
	// a rename onto the file with the stat the editor wrote is dropped too
	stat2, err := fileio.Write(context.Background(), path, "four", fileio.WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	w.Suppress(path, stat2)
	flush(rawEvent{kind: rawMovedFrom, path: dir + "/tmp", cookie: 1}, rawEvent{kind: rawMovedTo, path: path, cookie: 1}, rawEvent{kind: rawChanged, path: other})
	waitFor(t, "the batch", func() bool { return len(got.get()) == 4 })
	if b := got.get()[3]; len(b) != 1 || b[0].Path != other {
		t.Fatalf("fourth batch %+v", b)
	}
}

func TestSuppressForgetsTheOldestWrites(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	w := NewWatcher(ctx, WatcherOptions{ForcePolling: true, PollInterval: time.Hour})
	t.Cleanup(func() { cancel(); <-w.Done() })
	for i := 0; i < maxOwnWrites+10; i++ {
		w.Suppress("/x/"+time.Duration(i).String(), fileio.FileStat{})
	}
	w.ownMu.Lock()
	defer w.ownMu.Unlock()
	if len(w.own) != maxOwnWrites || len(w.ownOrder) != maxOwnWrites {
		t.Errorf("%d entries, want %d", len(w.own), maxOwnWrites)
	}
	if _, ok := w.own["/x/"+time.Duration(0).String()]; ok {
		t.Error("the oldest entry is still there")
	}
}

func writeFile(path, text string) error {
	return os.WriteFile(path, []byte(text), 0o644)
}
