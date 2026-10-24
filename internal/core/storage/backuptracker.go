package storage

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/document"
)

// The backup tracker (SPEC §10.10 "Hot exit") keeps the backups of the documents of a window up to date: a
// document that has unsaved changes gets its backup written a delay (DefaultBackupDelay) after its last change;
// when it is clean again (saved, reverted, undone to the saved text) or is closed without saving, the backup is
// removed. The documents belong to the thread of the window: the tracker reads them only through the Run
// function it is given, and the writes happen on a goroutine of its own, one after the other and in the order
// they were asked for.
//
// Ported from VS Code: src/vs/workbench/services/workingCopy/common/workingCopyBackupTracker.ts (scheduleBackup:
// a timer per working copy that a new change restarts; discardBackup when the working copy is not dirty any
// more; the backup of every dirty working copy is made before shutdown, backupBeforeShutdown) and
// src/vs/workbench/services/workingCopy/common/workingCopyBackupService.ts (backup, discardBackup)

// BackupTrackerOptions say what a BackupTracker is made of.
type BackupTrackerOptions struct {
	// Store holds the backups of the window; required.
	Store *BackupStore
	// Delay is how long after the last change of a document its backup is written; DefaultBackupDelay when zero.
	Delay time.Duration
	// After sets the timers of the delay; the system clock when nil.
	After AfterFunc
	// Run calls f on the thread that owns the documents. The timers fire on a goroutine of their own, and the
	// tracker reads a document only inside Run. When nil f is called at once.
	Run func(f func())
	// Logger gets the failures of the writes that nobody waits for; slog.Default() when nil.
	Logger *slog.Logger
	// OnProblem, when set, is told through Run that the backup of a document could not be made (its tag is
	// BackupTooLarge or BackupWriteFailed): once for a document, until a backup of it succeeds.
	OnProblem func(doc *document.Document, err error)
	// Disabled starts the tracker with hot exit off: it makes no backups until SetEnabled(true).
	Disabled bool
}

// BackupTracker follows the documents of a window and keeps their backups (see the comment above). It is safe
// for concurrent use; the documents it is given belong to the thread Run calls.
type BackupTracker struct {
	opts BackupTrackerOptions

	mu      sync.Mutex
	enabled bool
	paused  bool
	docs    map[*document.Document]*trackedDoc
	// adopted are the URIs whose backups are on the disk already (restored ones), until a document with that
	// URI is tracked.
	adopted map[string]bool

	// qmu guards the queue of the writer goroutine.
	qmu      sync.Mutex
	queue    []backupOp
	draining bool
	idle     sync.WaitGroup
}

// trackedDoc is what the tracker keeps for a document.
type trackedDoc struct {
	doc *document.Document
	// uri is the URI the backup is named by.
	uri string
	// stop cancels the timer, nil when none is set; gen tells which timer is the current one.
	stop func() bool
	gen  int
	// known is whether a backup file of the document may exist (so a discard is worth asking for).
	known bool
	// failed is whether the user was told that the backup fails, until one succeeds.
	failed bool
	stops  []func()
}

// backupOp is a write or a discard that waits for the writer goroutine.
type backupOp struct {
	write      *Backup
	doc        *document.Document
	discardURI string
	discardAll bool
}

// NewBackupTracker makes a tracker that tracks no document yet.
func NewBackupTracker(opts BackupTrackerOptions) *BackupTracker {
	if opts.Delay <= 0 {
		opts.Delay = DefaultBackupDelay
	}
	if opts.After == nil {
		opts.After = realAfter
	}
	if opts.Run == nil {
		opts.Run = func(f func()) { f() }
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &BackupTracker{opts: opts, enabled: !opts.Disabled, docs: map[*document.Document]*trackedDoc{}, adopted: map[string]bool{}}
}

// Backs reports whether the tracker backs a document up: it has a file or is untitled, and accepts edits.
func Backs(doc *document.Document) bool { return backable(doc) && !doc.ReadOnly() }

// backable tells whether a document can have a backup at all: it has a file or is untitled.
func backable(doc *document.Document) bool {
	switch doc.URI().Scheme {
	case document.SchemeFile, document.SchemeUntitled:
		return true
	}
	return false
}

// SnapshotBackup reads the state of a document to back up, on the thread of the document.
func SnapshotBackup(doc *document.Document) Backup {
	b := Backup{
		URI: doc.URI().String(), Content: doc.Text(), Encoding: doc.Encoding(), BOM: doc.HasBOM(),
		LineEnding: doc.LineEnding(), Language: doc.LanguageID(), Orphaned: doc.Orphaned(),
	}
	if doc.URI().Scheme == document.SchemeFile {
		if stat := doc.Stat(); !stat.ModTime.IsZero() {
			s := BackupStatOf(stat)
			b.Stat = &s
		}
	}
	return b
}

// Enabled tells whether the tracker makes backups (files.hotExit is not "off").
func (t *BackupTracker) Enabled() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.enabled
}

// Adopt tells the tracker that backups of the URIs are on the disk (they were restored): the backup of a document
// with such a URI is removed when the document is clean again or is closed without saving.
func (t *BackupTracker) Adopt(uris ...string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, u := range uris {
		t.adopted[u] = true
		for _, e := range t.docs {
			if e.uri == u {
				e.known = true
			}
		}
	}
}

// Track follows a document: its backup is written after its changes and removed when it is clean. A document that
// has unsaved changes already gets a backup after the delay. Documents that are not backed up (see Backs) are
// ignored, and so is a document tracked already.
func (t *BackupTracker) Track(doc *document.Document) {
	if !Backs(doc) {
		return
	}
	t.mu.Lock()
	if _, ok := t.docs[doc]; ok {
		t.mu.Unlock()
		return
	}
	e := &trackedDoc{doc: doc, uri: doc.URI().String()}
	e.known = t.adopted[e.uri]
	t.docs[doc] = e
	t.mu.Unlock()
	e.stops = []func(){
		doc.Subscribe(func(document.ChangeEvent) { t.refresh(e) }),
		doc.OnSaved(func(document.SavedEvent) { t.refresh(e) }),
		doc.OnExternalStateChange(func(document.ExternalState) { t.refresh(e) }),
		doc.OnURIChange(func(old, nu document.URI) { t.renamed(e, old, nu) }),
	}
	t.mu.Lock()
	live := t.enabled && !t.paused
	t.mu.Unlock()
	if live && doc.Dirty() {
		t.schedule(e)
	}
}

// Untrack stops following a document that was closed. With discard its backup is removed (the document was closed
// without saving, or saved); without it the backup stays (the window is shutting down).
func (t *BackupTracker) Untrack(doc *document.Document, discard bool) {
	t.mu.Lock()
	e, ok := t.docs[doc]
	if !ok {
		t.mu.Unlock()
		return
	}
	delete(t.docs, doc)
	t.cancelLocked(e)
	live := !t.paused
	known, uri := e.known, e.uri
	t.mu.Unlock()
	for _, stop := range e.stops {
		stop()
	}
	if discard && live && known {
		t.enqueue(backupOp{discardURI: uri})
	}
}

// Pause stops the tracker reacting: no timer runs and nothing is discarded, as when the window is going away
// and its documents close with it. Resume goes on.
func (t *BackupTracker) Pause() {
	t.mu.Lock()
	t.paused = true
	for _, e := range t.docs {
		t.cancelLocked(e)
	}
	t.mu.Unlock()
}

// Resume ends a Pause: the documents that have unsaved changes get their backups scheduled again. It must be called
// on the thread of the documents.
func (t *BackupTracker) Resume() {
	t.mu.Lock()
	t.paused = false
	entries := t.entriesLocked()
	t.mu.Unlock()
	for _, e := range entries {
		t.refresh(e)
	}
}

// SetEnabled turns the backups on or off (files.hotExit). Turning them off cancels the timers and removes the
// backups that are on the disk; turning them on schedules the documents that have unsaved changes. It must be
// called on the thread of the documents.
func (t *BackupTracker) SetEnabled(on bool) {
	t.mu.Lock()
	if t.enabled == on {
		t.mu.Unlock()
		return
	}
	t.enabled = on
	entries := t.entriesLocked()
	if !on {
		for _, e := range entries {
			t.cancelLocked(e)
			e.known = false
		}
		t.adopted = map[string]bool{}
	}
	t.mu.Unlock()
	if !on {
		t.enqueue(backupOp{discardAll: true})
		return
	}
	for _, e := range entries {
		t.refresh(e)
	}
}

func (t *BackupTracker) entriesLocked() []*trackedDoc {
	out := make([]*trackedDoc, 0, len(t.docs))
	for _, e := range t.docs {
		out = append(out, e)
	}
	return out
}

// cancelLocked stops the timer of a document; mu is held.
func (t *BackupTracker) cancelLocked(e *trackedDoc) {
	e.gen++
	if e.stop != nil {
		e.stop()
		e.stop = nil
	}
}

// refresh reacts to a change of a document: unsaved changes restart the timer, and a clean document loses its
// backup.
func (t *BackupTracker) refresh(e *trackedDoc) {
	t.mu.Lock()
	if t.paused || t.docs[e.doc] != e {
		t.mu.Unlock()
		return
	}
	enabled := t.enabled
	t.mu.Unlock()
	if e.doc.Dirty() {
		if enabled {
			t.schedule(e)
		}
		return
	}
	t.mu.Lock()
	t.cancelLocked(e)
	discard := e.known
	e.known = false
	uri := e.uri
	t.mu.Unlock()
	if discard {
		t.enqueue(backupOp{discardURI: uri})
	}
}

// schedule (re)starts the timer of a document.
func (t *BackupTracker) schedule(e *trackedDoc) {
	t.mu.Lock()
	t.cancelLocked(e)
	gen := e.gen
	t.mu.Unlock()
	// the timer may run its function before After returns, and the function takes mu
	stop := t.opts.After(t.opts.Delay, func() { t.opts.Run(func() { t.fire(e, gen) }) })
	t.mu.Lock()
	if e.gen == gen {
		e.stop = stop
	} else {
		stop()
	}
	t.mu.Unlock()
}

// fire is the timer of a document, on the thread of the documents: the backup is read and handed to the writer.
func (t *BackupTracker) fire(e *trackedDoc, gen int) {
	t.mu.Lock()
	if e.gen != gen || t.paused || !t.enabled || t.docs[e.doc] != e {
		t.mu.Unlock()
		return
	}
	e.stop = nil
	t.mu.Unlock()
	if !e.doc.Dirty() {
		t.refresh(e)
		return
	}
	b := SnapshotBackup(e.doc)
	t.mu.Lock()
	e.uri = b.URI
	e.known = true
	t.mu.Unlock()
	t.enqueue(backupOp{write: &b, doc: e.doc})
}

// renamed follows a document that got another URI (its file was moved, or a Save As): the backup of the old URI
// goes, and a document that still has unsaved changes gets one under the new URI.
func (t *BackupTracker) renamed(e *trackedDoc, old, nu document.URI) {
	t.mu.Lock()
	if t.docs[e.doc] != e {
		t.mu.Unlock()
		return
	}
	t.cancelLocked(e)
	discard := e.known && !t.paused
	e.known = t.adopted[nu.String()]
	e.uri = nu.String()
	t.mu.Unlock()
	if discard {
		t.enqueue(backupOp{discardURI: old.String()})
	}
	if Backs(e.doc) {
		t.refresh(e)
	}
}

// ---- the writer ----

// enqueue hands an operation to the writer goroutine. A write that waits is replaced by a newer write of the
// same document.
func (t *BackupTracker) enqueue(op backupOp) {
	t.qmu.Lock()
	if op.write != nil {
		for i, queued := range t.queue {
			if queued.write != nil && queued.write.URI == op.write.URI {
				t.queue = append(t.queue[:i:i], t.queue[i+1:]...)
				break
			}
		}
	}
	t.queue = append(t.queue, op)
	start := !t.draining
	if start {
		t.draining = true
		t.idle.Add(1)
	}
	t.qmu.Unlock()
	if start {
		go t.drain()
	}
}

// drain runs the operations until the queue is empty.
func (t *BackupTracker) drain() {
	defer t.idle.Done()
	ctx := context.Background()
	for {
		t.qmu.Lock()
		if len(t.queue) == 0 {
			t.draining = false
			t.qmu.Unlock()
			return
		}
		op := t.queue[0]
		t.queue = t.queue[1:]
		t.qmu.Unlock()
		t.run(ctx, op)
	}
}

func (t *BackupTracker) run(ctx context.Context, op backupOp) {
	switch {
	case op.write != nil:
		err := t.opts.Store.Write(ctx, *op.write)
		t.written(op.doc, err)
	case op.discardAll:
		if err := t.opts.Store.DiscardAll(ctx); err != nil {
			t.opts.Logger.Warn("the backups cannot be removed", "err", err)
		}
	default:
		if err := t.opts.Store.Discard(ctx, op.discardURI); err != nil {
			t.opts.Logger.Warn("a backup cannot be removed", "uri", op.discardURI, "err", err)
		}
	}
}

// written handles the result of a write that nobody waits for: a failure is logged and, once, handed to OnProblem.
func (t *BackupTracker) written(doc *document.Document, err error) {
	t.mu.Lock()
	e := t.docs[doc]
	tell := false
	switch {
	case e == nil:
	case err == nil:
		e.failed = false
	case !e.failed:
		e.failed, tell = true, true
	}
	t.mu.Unlock()
	if err == nil {
		return
	}
	t.opts.Logger.Warn("the backup of a document cannot be written; it is tried again at the next change", "err", err)
	if tell && t.opts.OnProblem != nil {
		t.opts.Run(func() { t.opts.OnProblem(doc, err) })
	}
}

// Wait returns once every operation handed to the writer before the call is done, or when ctx is. The error has
// the tag BackupWaitCancelled.
func (t *BackupTracker) Wait(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		t.idle.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return errors.NewErr("BackupWaitCancelled", ctx.Err())
	}
}

// ---- shutdown ----

// BackupBatch is the backups of the documents of a window that are made together, before it closes: the texts
// are read on the thread of the documents (Prepare), and written on any goroutine (Write).
type BackupBatch struct {
	t       *BackupTracker
	backups []Backup
	docs    []*document.Document
	// skipped are the documents that cannot be backed up (a text over the limit), with the error.
	skipped []error
}

// Prepare reads the backups of the documents that have unsaved changes, on the thread of the documents; the
// timers of those documents are cancelled, since the batch is the newest state. A document whose text is over
// the limit is reported by Write with the tag BackupTooLarge. Clean documents and those that cannot have a backup
// (virtual ones) are left out; a document that became read-only with unsaved changes is still backed up.
func (t *BackupTracker) Prepare(docs []*document.Document) *BackupBatch {
	b := &BackupBatch{t: t}
	for _, doc := range docs {
		if doc == nil || !backable(doc) || !doc.Dirty() {
			continue
		}
		snap := SnapshotBackup(doc)
		t.mu.Lock()
		if e := t.docs[doc]; e != nil {
			t.cancelLocked(e)
			e.uri = snap.URI
		}
		t.mu.Unlock()
		if len(snap.Content) > t.opts.Store.MaxSize() {
			b.skipped = append(b.skipped, errors.New("BackupTooLarge", "the text is too large to be backed up",
				"uri", snap.URI, "size", len(snap.Content), "limit", t.opts.Store.MaxSize()))
			continue
		}
		b.backups = append(b.backups, snap)
		b.docs = append(b.docs, doc)
	}
	return b
}

// Len is the number of backups the batch writes.
func (b *BackupBatch) Len() int { return len(b.backups) }

// Write waits for the writes that are going on, then writes the backups of the batch, all of them even when one
// fails, and returns the first failure (tags BackupTooLarge, BackupWriteFailed and BackupWaitCancelled). It does
// not touch the documents, so it can run on any goroutine.
func (b *BackupBatch) Write(ctx context.Context) error {
	var first error
	if len(b.skipped) > 0 {
		first = b.skipped[0]
	}
	if err := b.t.Wait(ctx); err != nil {
		return err
	}
	for i, backup := range b.backups {
		err := b.t.opts.Store.Write(ctx, backup)
		b.t.mu.Lock()
		if e := b.t.docs[b.docs[i]]; e != nil {
			e.known = e.known || err == nil
			if err == nil {
				e.failed = false
			}
		}
		b.t.mu.Unlock()
		if err != nil && first == nil {
			first = err
		}
	}
	return first
}

// BackupAll is Prepare and Write in a row, for callers that need no goroutine.
func (t *BackupTracker) BackupAll(ctx context.Context, docs []*document.Document) error {
	return t.Prepare(docs).Write(ctx)
}

// DiscardAll waits for the writes that are going on and removes every backup of the window: what is done when the
// window closes and its documents were saved or given up. The tracker forgets that any backup exists.
func (t *BackupTracker) DiscardAll(ctx context.Context) error {
	t.mu.Lock()
	for _, e := range t.docs {
		t.cancelLocked(e)
		e.known = false
	}
	t.adopted = map[string]bool{}
	t.mu.Unlock()
	if err := t.Wait(ctx); err != nil {
		return err
	}
	return t.opts.Store.DiscardAll(ctx)
}

// Dispose stops every timer and every subscription. The writes that wait are still done (use Wait to await
// them); the backups on the disk stay.
func (t *BackupTracker) Dispose() {
	t.mu.Lock()
	entries := t.entriesLocked()
	t.docs = map[*document.Document]*trackedDoc{}
	for _, e := range entries {
		t.cancelLocked(e)
	}
	t.mu.Unlock()
	for _, e := range entries {
		for _, stop := range e.stops {
			stop()
		}
	}
}
