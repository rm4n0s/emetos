package storage_test

import (
	"context"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/core/storage/storagetest"
)

// trackerEnv is a tracker over an isolated store, with a clock that is moved by hand and a count of the writes.
type trackerEnv struct {
	t       *testing.T
	clock   *storagetest.Clock
	store   *storage.BackupStore
	tracker *storage.BackupTracker
	writes  atomic.Int32
	fail    atomic.Bool
	mu      sync.Mutex
	problem []error
}

func newTrackerEnv(t *testing.T, mutate func(*storage.BackupTrackerOptions)) *trackerEnv {
	t.Helper()
	e := &trackerEnv{t: t, clock: storagetest.NewClock()}
	e.store, _ = newBackupStore(t, func(o *storage.BackupStoreOptions) {
		o.WriteFile = func(path string, data []byte) error {
			if e.fail.Load() {
				return os.ErrPermission
			}
			e.writes.Add(1)
			return storage.WriteFileAtomic(path, data)
		}
		o.MaxSize = 1 << 20
	})
	opts := storage.BackupTrackerOptions{
		Store: e.store, After: e.clock.AfterFunc, Logger: quietLogger(),
		OnProblem: func(_ *document.Document, err error) {
			e.mu.Lock()
			defer e.mu.Unlock()
			e.problem = append(e.problem, err)
		},
	}
	if mutate != nil {
		mutate(&opts)
	}
	e.tracker = storage.NewBackupTracker(opts)
	t.Cleanup(e.tracker.Dispose)
	return e
}

// settle waits for the writer.
func (e *trackerEnv) settle() {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.tracker.Wait(ctx); err != nil {
		e.t.Fatal(err)
	}
}

// advance moves the clock and waits for the writes it caused.
func (e *trackerEnv) advance(d time.Duration) {
	e.t.Helper()
	e.clock.Advance(d)
	e.settle()
}

// backups lists the URIs of the backups on the disk.
func (e *trackerEnv) backups() map[string]string {
	e.t.Helper()
	list, problems := e.store.List(context.Background())
	if len(problems) != 0 {
		e.t.Fatalf("problems: %v", problems)
	}
	out := map[string]string{}
	for _, b := range list {
		out[b.URI] = b.Content
	}
	return out
}

func (e *trackerEnv) wantBackups(want map[string]string) {
	e.t.Helper()
	got := e.backups()
	if len(got) != len(want) {
		e.t.Fatalf("backups = %v, want %v", got, want)
	}
	for uri, content := range want {
		if g, ok := got[uri]; !ok || g != content {
			e.t.Fatalf("backups = %v, want %v", got, want)
		}
	}
}

func typeAtEnd(t *testing.T, doc *document.Document, text string) {
	t.Helper()
	snap := doc.Snapshot()
	end := snap.PositionAt(snap.Len())
	if _, err := doc.Edit([]document.Edit{{Range: buffer.Range{Start: end, End: end}, Text: text}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
}

func fileDoc(path, text string) *document.Document {
	return document.FromRead(document.FileURI(path), fileio.ReadResult{
		Text: text, Encoding: fileio.UTF8, LineEnding: fileio.LF,
		Stat: fileio.FileStat{ModTime: time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC), Size: int64(len(text))},
	}, document.NewOptions())
}

func TestTrackerWritesTheBackupADelayAfterTheLastChange(t *testing.T) {
	e := newTrackerEnv(t, nil)
	doc := fileDoc("/w/a.go", "package a\n")
	e.tracker.Track(doc)
	e.advance(time.Hour)
	e.wantBackups(nil) // a clean document has no backup

	typeAtEnd(t, doc, "// one\n")
	e.advance(999 * time.Millisecond)
	e.wantBackups(nil)
	e.advance(time.Millisecond)
	e.wantBackups(map[string]string{"file:///w/a.go": "package a\n// one\n"})
	if n := e.writes.Load(); n != 1 {
		t.Errorf("%d writes, want 1", n)
	}

	b, err := e.store.Read(context.Background(), "file:///w/a.go")
	if err != nil {
		t.Fatal(err)
	}
	if b.Encoding != "utf8" || b.LineEnding != fileio.LF || b.Language != "plaintext" || b.Stat == nil || b.Stat.Size != int64(len("package a\n")) || b.Orphaned {
		t.Errorf("metadata = %+v (stat %+v)", b, b.Stat)
	}
}

func TestTrackerABurstOfChangesIsOneWrite(t *testing.T) {
	e := newTrackerEnv(t, nil)
	doc := fileDoc("/w/a.go", "")
	e.tracker.Track(doc)
	for i := range 10 {
		typeAtEnd(t, doc, strings.Repeat("x", i+1))
		e.clock.Advance(900 * time.Millisecond) // each change restarts the delay
	}
	e.settle()
	if n := e.writes.Load(); n != 0 {
		t.Fatalf("%d writes during the burst, want 0", n)
	}
	e.advance(100 * time.Millisecond)
	if n := e.writes.Load(); n != 1 {
		t.Fatalf("%d writes after the burst, want 1", n)
	}
	e.wantBackups(map[string]string{"file:///w/a.go": doc.Text()})
}

func TestTrackerDiscardsTheBackupOfACleanDocument(t *testing.T) {
	tests := []struct {
		name  string
		clean func(t *testing.T, doc *document.Document)
	}{
		{"saved", func(t *testing.T, doc *document.Document) {
			if _, err := doc.Save(context.Background(), document.SaveOptions{IgnoreConflict: true}); err != nil {
				t.Fatal(err)
			}
		}},
		{"undone to the saved text", func(t *testing.T, doc *document.Document) {
			if _, err := doc.Undo(); err != nil {
				t.Fatal(err)
			}
		}},
		{"reverted", func(t *testing.T, doc *document.Document) {
			if err := doc.Revert(context.Background(), fileio.ReadOptions{}); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := newTrackerEnv(t, nil)
			path := t.TempDir() + "/a.txt"
			if err := os.WriteFile(path, []byte("one\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			res, err := fileio.Read(context.Background(), path, fileio.ReadOptions{})
			if err != nil {
				t.Fatal(err)
			}
			doc := document.FromRead(document.FileURI(path), res, document.NewOptions())
			e.tracker.Track(doc)
			typeAtEnd(t, doc, "two\n")
			e.advance(time.Second)
			e.wantBackups(map[string]string{doc.URI().String(): "one\ntwo\n"})

			tc.clean(t, doc)
			e.settle()
			e.wantBackups(nil)
			// a later change brings the backup back
			typeAtEnd(t, doc, "three\n")
			e.advance(time.Second)
			if len(e.backups()) != 1 {
				t.Errorf("backups = %v", e.backups())
			}
		})
	}
}

func TestTrackerUntrackDiscardsOrKeeps(t *testing.T) {
	for _, discard := range []bool{true, false} {
		name := "keeps the backup"
		if discard {
			name = "discards the backup"
		}
		t.Run(name, func(t *testing.T) {
			e := newTrackerEnv(t, nil)
			doc := fileDoc("/w/a.go", "x")
			e.tracker.Track(doc)
			typeAtEnd(t, doc, "y")
			e.advance(time.Second)
			e.tracker.Untrack(doc, discard)
			e.settle()
			if discard {
				e.wantBackups(nil)
			} else {
				e.wantBackups(map[string]string{"file:///w/a.go": "xy"})
			}
			// an untracked document is not followed any more
			typeAtEnd(t, doc, "z")
			e.advance(time.Hour)
			if discard {
				e.wantBackups(nil)
			}
		})
	}
	t.Run("a change that was not written yet leaves nothing", func(t *testing.T) {
		e := newTrackerEnv(t, nil)
		doc := fileDoc("/w/a.go", "x")
		e.tracker.Track(doc)
		typeAtEnd(t, doc, "y")
		e.tracker.Untrack(doc, true)
		e.advance(time.Hour)
		e.wantBackups(nil)
		if n := e.writes.Load(); n != 0 {
			t.Errorf("%d writes", n)
		}
	})
}

func TestTrackerUntitledDocuments(t *testing.T) {
	e := newTrackerEnv(t, nil)
	empty := document.New(document.UntitledURI(1), "", document.NewOptions())
	notes := document.New(document.UntitledURI(2), "", document.Options{LanguageID: "markdown"})
	e.tracker.Track(empty)
	e.tracker.Track(notes)
	typeAtEnd(t, notes, "# notes\n")
	e.advance(time.Second)
	e.wantBackups(map[string]string{"untitled:2": "# notes\n"})
	b, err := e.store.Read(context.Background(), "untitled:2")
	if err != nil || b.Language != "markdown" || b.Stat != nil {
		t.Errorf("backup = %+v, %v", b, err)
	}
	// emptied again: no backup
	if _, err := notes.Undo(); err != nil {
		t.Fatal(err)
	}
	e.settle()
	e.wantBackups(nil)
}

func TestTrackerIgnoresWhatItDoesNotBackUp(t *testing.T) {
	e := newTrackerEnv(t, nil)
	virtual := document.New(document.VirtualURI("settings"), "", document.NewOptions())
	readOnly := document.New(document.FileURI("/w/ro.txt"), "text", document.Options{ReadOnly: true})
	e.tracker.Track(virtual)
	e.tracker.Track(readOnly)
	e.advance(time.Hour)
	e.wantBackups(nil)
	if storage.Backs(virtual) || storage.Backs(readOnly) {
		t.Error("Backs accepts a virtual or a read-only document")
	}
}

func TestTrackerRenameMovesTheBackup(t *testing.T) {
	e := newTrackerEnv(t, nil)
	doc := fileDoc("/w/old.go", "a")
	e.tracker.Track(doc)
	typeAtEnd(t, doc, "b")
	e.advance(time.Second)
	e.wantBackups(map[string]string{"file:///w/old.go": "ab"})

	doc.SetURI(document.FileURI("/w/new.go"))
	e.settle()
	e.wantBackups(nil) // the old name goes at once
	e.advance(time.Second)
	e.wantBackups(map[string]string{"file:///w/new.go": "ab"})

	// the Save As of an untitled document: the backup of the untitled name goes, the file is clean
	path := t.TempDir() + "/saved.txt"
	untitled := document.New(document.UntitledURI(4), "", document.NewOptions())
	e.tracker.Track(untitled)
	typeAtEnd(t, untitled, "text")
	e.advance(time.Second)
	if _, ok := e.backups()["untitled:4"]; !ok {
		t.Fatalf("backups = %v", e.backups())
	}
	if _, err := untitled.SaveAs(context.Background(), path, document.SaveOptions{}); err != nil {
		t.Fatal(err)
	}
	e.advance(time.Second)
	if _, ok := e.backups()["untitled:4"]; ok {
		t.Errorf("the backup of the untitled name stays: %v", e.backups())
	}
	if _, ok := e.backups()["file://"+path]; ok {
		t.Errorf("the saved document has a backup: %v", e.backups())
	}
}

func TestTrackerOrphanedFlag(t *testing.T) {
	e := newTrackerEnv(t, nil)
	doc := fileDoc("/w/gone.go", "text")
	e.tracker.Track(doc)
	typeAtEnd(t, doc, "!")
	e.advance(time.Second)
	if b, _ := e.store.Read(context.Background(), "file:///w/gone.go"); b.Orphaned {
		t.Fatal("orphaned before the file is gone")
	}
	// the file is deleted: the document looks at it and becomes orphaned
	if _, err := doc.CheckExternal(context.Background(), nil, fileio.ReadOptions{}); err != nil {
		t.Fatal(err)
	}
	if !doc.Orphaned() {
		t.Fatal("the document is not orphaned")
	}
	e.advance(time.Second)
	if b, err := e.store.Read(context.Background(), "file:///w/gone.go"); err != nil || !b.Orphaned || b.Content != "text!" {
		t.Errorf("backup = %+v, %v: want the orphaned flag", b, err)
	}
}

func TestTrackerHotExitOffMakesNoBackupsAndDropsTheOldOnes(t *testing.T) {
	e := newTrackerEnv(t, nil)
	doc := fileDoc("/w/a.go", "a")
	e.tracker.Track(doc)
	typeAtEnd(t, doc, "b")
	e.advance(time.Second)
	e.wantBackups(map[string]string{"file:///w/a.go": "ab"})

	e.tracker.SetEnabled(false)
	e.settle()
	e.wantBackups(nil)
	typeAtEnd(t, doc, "c")
	e.advance(time.Hour)
	e.wantBackups(nil)
	if e.tracker.Enabled() {
		t.Error("still enabled")
	}

	e.tracker.SetEnabled(true) // the document is dirty: it is backed up again
	e.advance(time.Second)
	e.wantBackups(map[string]string{"file:///w/a.go": "abc"})
	e.tracker.SetEnabled(true)
}

func TestTrackerStartsDisabled(t *testing.T) {
	e := newTrackerEnv(t, func(o *storage.BackupTrackerOptions) { o.Disabled = true })
	// a backup from an earlier run is not touched at start
	if err := e.store.Write(context.Background(), storage.Backup{URI: "file:///old", Content: "old"}); err != nil {
		t.Fatal(err)
	}
	doc := fileDoc("/w/a.go", "a")
	e.tracker.Track(doc)
	typeAtEnd(t, doc, "b")
	e.advance(time.Hour)
	e.wantBackups(map[string]string{"file:///old": "old"})
}

func TestTrackerPauseAndResume(t *testing.T) {
	e := newTrackerEnv(t, nil)
	doc := fileDoc("/w/a.go", "a")
	e.tracker.Track(doc)
	typeAtEnd(t, doc, "b")
	e.advance(time.Second)
	e.tracker.Pause()
	typeAtEnd(t, doc, "c")
	e.advance(time.Hour)
	e.wantBackups(map[string]string{"file:///w/a.go": "ab"}) // paused: not rewritten
	e.tracker.Untrack(doc, true)
	e.settle()
	e.wantBackups(map[string]string{"file:///w/a.go": "ab"}) // paused: not discarded

	other := fileDoc("/w/b.go", "")
	e.tracker.Track(other)
	typeAtEnd(t, other, "x")
	e.tracker.Resume()
	e.advance(time.Second)
	e.wantBackups(map[string]string{"file:///w/a.go": "ab", "file:///w/b.go": "x"})
}

func TestTrackerAdoptedBackupsAreDiscardedWhenTheDocumentIsClean(t *testing.T) {
	e := newTrackerEnv(t, nil)
	ctx := context.Background()
	if err := e.store.Write(ctx, storage.Backup{URI: "file:///w/a.go", Content: "ab"}); err != nil {
		t.Fatal(err)
	}
	e.tracker.Adopt("file:///w/a.go")
	doc := fileDoc("/w/a.go", "a")
	e.tracker.Track(doc)
	typeAtEnd(t, doc, "b")
	e.advance(time.Second)
	if _, err := doc.Undo(); err != nil {
		t.Fatal(err)
	}
	e.settle()
	e.wantBackups(nil)

	// a document that is closed without saving
	if err := e.store.Write(ctx, storage.Backup{URI: "file:///w/b.go", Content: "x"}); err != nil {
		t.Fatal(err)
	}
	e.tracker.Adopt("file:///w/b.go")
	closed := fileDoc("/w/b.go", "")
	e.tracker.Track(closed)
	e.tracker.Untrack(closed, true)
	e.settle()
	e.wantBackups(nil)
}

func TestTrackerBackupAllFlushesBeforeTheDelay(t *testing.T) {
	e := newTrackerEnv(t, nil)
	a := fileDoc("/w/a.go", "a")
	b := document.New(document.UntitledURI(1), "", document.NewOptions())
	clean := fileDoc("/w/clean.go", "c")
	for _, d := range []*document.Document{a, b, clean} {
		e.tracker.Track(d)
	}
	typeAtEnd(t, a, "1")
	typeAtEnd(t, b, "2")
	if err := e.tracker.BackupAll(context.Background(), []*document.Document{a, b, clean}); err != nil {
		t.Fatal(err)
	}
	e.wantBackups(map[string]string{"file:///w/a.go": "a1", "untitled:1": "2"})
	// the timers were cancelled: nothing is written again
	before := e.writes.Load()
	e.advance(time.Hour)
	if e.writes.Load() != before {
		t.Errorf("%d writes after the flush", e.writes.Load()-before)
	}
	// and the backups are known: saving one takes it away
	if _, err := a.Undo(); err != nil {
		t.Fatal(err)
	}
	e.settle()
	e.wantBackups(map[string]string{"untitled:1": "2"})
}

func TestTrackerBackupAllAwaitsTheWritesInFlight(t *testing.T) {
	e := newTrackerEnv(t, nil)
	doc := fileDoc("/w/a.go", "a")
	e.tracker.Track(doc)
	typeAtEnd(t, doc, "1")
	e.clock.Advance(time.Second) // the write is handed to the writer, maybe not done
	typeAtEnd(t, doc, "2")
	if err := e.tracker.BackupAll(context.Background(), []*document.Document{doc}); err != nil {
		t.Fatal(err)
	}
	e.wantBackups(map[string]string{"file:///w/a.go": "a12"}) // the newest text wins, whatever the order of the writes
}

func TestTrackerBackupAllFailures(t *testing.T) {
	ctx := context.Background()
	t.Run("a write that fails", func(t *testing.T) {
		e := newTrackerEnv(t, nil)
		doc := fileDoc("/w/a.go", "a")
		e.tracker.Track(doc)
		typeAtEnd(t, doc, "1")
		e.fail.Store(true)
		err := e.tracker.BackupAll(ctx, []*document.Document{doc})
		if appErr := routeOf(t, err); !appErr.HasRoute("BackupStore.Write.BackupWriteFailed") {
			t.Errorf("unexpected failure path: %s", appErr.Route())
		}
	})
	t.Run("a text over the limit", func(t *testing.T) {
		e := newTrackerEnv(t, nil)
		doc := fileDoc("/w/big.txt", "")
		e.tracker.Track(doc)
		typeAtEnd(t, doc, strings.Repeat("x", (1<<20)+1))
		err := e.tracker.BackupAll(ctx, []*document.Document{doc})
		if appErr := routeOf(t, err); !appErr.HasRoute("BackupTracker.Prepare.BackupTooLarge") {
			t.Errorf("unexpected failure path: %s", appErr.Route())
		}
	})
	t.Run("a cancelled wait", func(t *testing.T) {
		clock := storagetest.NewClock()
		release := make(chan struct{})
		blocked, _ := newBackupStore(t, func(o *storage.BackupStoreOptions) {
			o.WriteFile = func(string, []byte) error { <-release; return nil }
		})
		tr := storage.NewBackupTracker(storage.BackupTrackerOptions{Store: blocked, After: clock.AfterFunc, Logger: quietLogger()})
		defer func() { close(release); tr.Dispose() }()
		doc := fileDoc("/w/a.go", "a")
		tr.Track(doc)
		typeAtEnd(t, doc, "1")
		clock.Advance(time.Second) // the writer is stuck in the write
		cancelled, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
		defer cancel()
		err := tr.Wait(cancelled)
		if appErr := routeOf(t, err); !appErr.HasRoute("BackupTracker.Wait.BackupWaitCancelled") || !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("unexpected failure: %v", err)
		}
	})
}

func TestTrackerDiscardAll(t *testing.T) {
	e := newTrackerEnv(t, nil)
	a := fileDoc("/w/a.go", "a")
	e.tracker.Track(a)
	typeAtEnd(t, a, "1")
	e.advance(time.Second)
	if err := e.store.Write(context.Background(), storage.Backup{URI: "file:///w/other", Content: "x"}); err != nil {
		t.Fatal(err)
	}
	if err := e.tracker.DiscardAll(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.wantBackups(nil)
}

func TestTrackerAWriteThatFailsIsReportedOnceAndTriedAgain(t *testing.T) {
	e := newTrackerEnv(t, nil)
	doc := fileDoc("/w/a.go", "a")
	e.tracker.Track(doc)
	e.fail.Store(true)
	for range 3 {
		typeAtEnd(t, doc, "x")
		e.advance(time.Second)
	}
	e.mu.Lock()
	n := len(e.problem)
	e.mu.Unlock()
	if n != 1 {
		t.Fatalf("%d problems reported, want 1", n)
	}
	e.mu.Lock()
	err := e.problem[0]
	e.mu.Unlock()
	if appErr := routeOf(t, err); appErr.Tag != "BackupWriteFailed" {
		t.Errorf("tag = %s", appErr.Tag)
	}
	e.fail.Store(false)
	typeAtEnd(t, doc, "y")
	e.advance(time.Second)
	e.wantBackups(map[string]string{"file:///w/a.go": "axxxy"})
}

func TestTrackerRunsTimersOnTheOwnerThread(t *testing.T) {
	var ran atomic.Int32
	e := newTrackerEnv(t, func(o *storage.BackupTrackerOptions) {
		o.Run = func(f func()) { ran.Add(1); f() }
	})
	doc := fileDoc("/w/a.go", "a")
	e.tracker.Track(doc)
	typeAtEnd(t, doc, "1")
	e.advance(time.Second)
	if ran.Load() == 0 {
		t.Error("Run was not used")
	}
}

func TestTrackerDisposeLeavesNoTimers(t *testing.T) {
	e := newTrackerEnv(t, nil)
	for _, path := range []string{"/w/a.go", "/w/b.go", "/w/c.go"} {
		doc := fileDoc(path, "")
		e.tracker.Track(doc)
		typeAtEnd(t, doc, "x")
	}
	if e.clock.Pending() != 3 {
		t.Fatalf("%d timers, want 3", e.clock.Pending())
	}
	e.tracker.Dispose()
	if e.clock.Pending() != 0 {
		t.Errorf("%d timers left", e.clock.Pending())
	}
	e.advance(time.Hour)
	e.wantBackups(nil)
}
