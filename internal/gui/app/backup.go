package app

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
)

// Hot exit (SPEC §3.1 "Hot exit", §10.10, §10.11): the unsaved documents of a window, untitled ones included, are
// backed up as they change and when the window closes, and come back dirty the next time the window opens. The
// backups of a window are kept by core/storage (BackupStore, BackupTracker); this file connects them to the
// window: the tracker follows the documents of the window, the settings turn it on and off, PrepareRestore puts
// the backups back (state.go), and closing (closing.go) asks for the last backups or falls back to the dialog.
//
// Ported from VS Code: src/vs/workbench/services/workingCopy/common/workingCopyBackupTracker.ts and
// src/vs/workbench/services/workingCopy/electron-sandbox/workingCopyBackupTracker.ts (the hot exit modes at
// shutdown, the restore of the backups at start), src/vs/workbench/contrib/backup/common/backupRestorer.ts
// (a backup that has no editor opens in the active group)

// backupExitTimeout bounds the writes of the backups made when the window closes.
const backupExitTimeout = 30 * time.Second

// hotExitEnabled tells whether a value of files.hotExit makes backups: every value but "off" does; a value the
// setting does not have counts as "off".
func hotExitEnabled(mode string) bool { return mode == "onExit" || mode == "onExitAndWindowClose" }

// backupState is what a window keeps of its backups.
type backupState struct {
	mu sync.Mutex
	// store and tracker are those of the window's state id (initBackups); nil when the window keeps no state.
	store   *storage.BackupStore
	tracker *storage.BackupTracker
	// marked is whether the running marker of the store was written; crashed whether the last run of the window
	// left it.
	marked, crashed bool
	// stopSettings stops following files.hotExit.
	stopSettings func()
	// post runs a function on the UI thread; pending holds what was asked before the shell gave one.
	post    func(func())
	pending []func()
	// changed are the restored documents whose file changed on the disk since their backup (they are conflicted).
	changed map[*document.Document]bool
}

// hotExitMode is the value of files.hotExit.
func (w *Workspace) hotExitMode() string {
	return settings.Get[string](w.Settings, "files.hotExit", "")
}

// Backups is the tracker of the backups of the window, or nil when the window keeps no state.
func (w *Workspace) Backups() *storage.BackupTracker {
	w.backups.mu.Lock()
	defer w.backups.mu.Unlock()
	return w.backups.tracker
}

// runUI runs f on the UI thread: through the function the shell gave (setUIPoster), and as soon as it gives
// one when it has not yet. The backup tracker reads the documents only through it.
func (w *Workspace) runUI(f func()) {
	b := &w.backups
	b.mu.Lock()
	post := b.post
	if post == nil {
		b.pending = append(b.pending, f)
	}
	b.mu.Unlock()
	if post != nil {
		post(f)
	}
}

// setUIPoster gives the window the function that runs a function on the UI thread, and runs what waited for it.
func (w *Workspace) setUIPoster(post func(func())) {
	b := &w.backups
	b.mu.Lock()
	b.post = post
	pending := b.pending
	b.pending = nil
	b.mu.Unlock()
	if post == nil {
		return
	}
	for _, f := range pending {
		post(f)
	}
}

// initBackups makes the store and the tracker of the backups of the window, for the state id it has now (it is
// called with initState: when the window is made and when its folder changes). The documents that are open are
// tracked. The tracker of the id the window had is let go, and so is its running marker.
func (w *Workspace) initBackups(ctx context.Context) {
	b := &w.backups
	b.mu.Lock()
	oldTracker, oldStore, marked := b.tracker, b.store, b.marked
	b.tracker, b.store, b.marked = nil, nil, false
	b.mu.Unlock()
	if oldTracker != nil {
		oldTracker.Dispose()
	}
	if marked && oldStore != nil {
		if err := oldStore.ClearRunning(); err != nil {
			w.Process.Logger.Warn("the marker of the window cannot be removed", "err", err)
		}
	}
	w.Documents.SetObservers(w.trackDocument, w.dropDocument)
	if w.stateID == "" {
		return
	}
	p := w.Process
	store, err := storage.NewBackupStore(p.Paths, w.stateID, storage.BackupStoreOptions{WriteFile: p.backupWrite, Logger: p.Logger, MaxSize: p.backupMax})
	if err != nil {
		p.Logger.Warn("the window keeps no backups", "err", err)
		return
	}
	tracker := storage.NewBackupTracker(storage.BackupTrackerOptions{
		Store: store, After: p.backupAfter, Run: w.runUI, Logger: p.Logger,
		OnProblem: w.backupProblem, Disabled: !hotExitEnabled(w.hotExitMode()),
	})
	b.mu.Lock()
	b.store, b.tracker = store, tracker
	if b.stopSettings == nil {
		b.stopSettings = w.Settings.OnChange(func(e settings.ChangeEvent) {
			if e.Affects("files.hotExit", "") {
				w.runUI(func() {
					if t := w.Backups(); t != nil {
						t.SetEnabled(hotExitEnabled(w.hotExitMode()))
					}
				})
			}
		})
	}
	b.mu.Unlock()
	for _, doc := range w.Documents.All() {
		tracker.Track(doc)
	}
	if marked {
		w.markRunning(ctx)
	}
}

// trackDocument and dropDocument are what the set of documents tells the tracker: a document that opens is
// followed, and one that closes is let go of with its backup (it was saved, or given up). While the window is going
// away (the tracker is paused) nothing is discarded: the backups are what is left of the documents.
func (w *Workspace) trackDocument(doc *document.Document) {
	if t := w.Backups(); t != nil {
		t.Track(doc)
	}
}

func (w *Workspace) dropDocument(doc *document.Document) {
	if t := w.Backups(); t != nil {
		t.Untrack(doc, true)
	}
	w.backups.mu.Lock()
	delete(w.backups.changed, doc)
	w.backups.mu.Unlock()
}

// backupProblem tells the user, once for a document, that its unsaved changes cannot be backed up.
func (w *Workspace) backupProblem(doc *document.Document, err error) {
	name := doc.URI().Title()
	if appErr, ok := errors.FromError(err); ok && appErr.Tag == "BackupTooLarge" {
		w.Notify.Warn("Emetos", "'"+name+"' is too large to be backed up. Its unsaved changes are lost if Emetos closes unexpectedly.")
		return
	}
	w.Notify.Warn("Emetos", "The unsaved changes of '"+name+"' cannot be backed up: "+describe(err))
}

// markRunning writes the marker that says the window is open; it tells whether the last run left it. A failure is
// logged: the window works without.
func (w *Workspace) markRunning(ctx context.Context) (wasRunning bool) {
	b := &w.backups
	b.mu.Lock()
	store := b.store
	b.mu.Unlock()
	if store == nil {
		return false
	}
	wasRunning, err := store.MarkRunning(ctx)
	if err != nil {
		w.Process.Logger.Warn("the window cannot note that it is open", "err", err)
		return wasRunning
	}
	b.mu.Lock()
	b.marked = true
	b.mu.Unlock()
	return wasRunning
}

// endBackups removes the running marker: the window closes as it should.
func (w *Workspace) endBackups() {
	b := &w.backups
	b.mu.Lock()
	store, marked := b.store, b.marked
	b.marked = false
	b.mu.Unlock()
	if store == nil || !marked {
		return
	}
	if err := store.ClearRunning(); err != nil {
		w.Process.Logger.Warn("the marker of the window cannot be removed", "err", err)
	}
}

// discardBackups removes every backup of the window: it closes with nothing left to restore (files.hotExit off, or
// the unsaved documents were saved or given up).
func (w *Workspace) discardBackups() {
	t := w.Backups()
	if t == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(w.ctx), backupExitTimeout)
	defer cancel()
	if err := t.DiscardAll(ctx); err != nil {
		w.Process.Logger.Warn("the backups cannot be removed", "err", err)
	}
}

// NoticeBackupsRestored is what the notification of a restore after a crash says.
func NoticeBackupsRestored(n int) string {
	if n == 1 {
		return "Emetos did not close properly the last time. The unsaved changes of 1 file were restored from a backup."
	}
	return "Emetos did not close properly the last time. The unsaved changes of " + strconv.Itoa(n) + " files were restored from backups."
}

// NoticeChangedOnDisk is what the notification of a restored document says when its file changed on the disk since
// its backup was made.
func NoticeChangedOnDisk(name string) string {
	return "'" + name + "' has changed on disk since its backup was made. Your unsaved changes were restored; saving will ask before overwriting."
}

// NoticeDeletedOnDisk is what the notification of restored documents says when their files are gone.
func NoticeDeletedOnDisk(names []string) string {
	if len(names) == 1 {
		return "'" + names[0] + "' no longer exists on disk. Its unsaved changes were restored from a backup; saving will create the file again."
	}
	return strconv.Itoa(len(names)) + " files no longer exist on disk. Their unsaved changes were restored from backups; saving will create the files again."
}

// ---- the shell ----

// initBackups connects the backups of the window to the shell: the tracker reads the documents on the UI thread
// from now on, and the documents that have unsaved changes (the ones that came back from backups) are scheduled.
func (s *shellState) initBackups(binding *widgets.WidgetsBinding) {
	win := PeekWorkspace(s.Context())
	win.setUIPoster(binding.Post)
	if t := win.Backups(); t != nil {
		t.Resume()
	}
}

// disposeBackups lets the tracker go quiet while the shell goes away: the documents close with it, and a document
// that closes then does not lose its backup.
func (s *shellState) disposeBackups() {
	win := s.win
	if win == nil {
		return
	}
	if t := win.Backups(); t != nil {
		t.Pause()
	}
	win.setUIPoster(nil)
}

// showRestoredConflicts shows the notification of each restored document whose file changed on the disk since its
// backup (they are conflicted, and the editors were made before the notifications could be).
func (s *shellState) showRestoredConflicts() {
	for _, doc := range s.win.Documents.All() {
		if doc != nil && doc.Conflicted() {
			s.externalStateChanged(doc, doc.ExternalState())
		}
	}
}

// backupOnSignal writes the backups of the unsaved documents when the signal that ends the program arrives (it does
// not go through the question about unsaved changes), so that the next start restores them. The writes are
// waited for, but not for long.
func (s *shellState) backupOnSignal() {
	win := s.win
	t := win.Backups()
	if t == nil || !t.Enabled() {
		return
	}
	t.Pause() // the documents close with the program: their backups stay
	ctx, cancel := context.WithTimeout(context.WithoutCancel(win.ctx), backupExitTimeout)
	defer cancel()
	if err := t.BackupAll(ctx, win.Documents.All()); err != nil {
		win.Process.Logger.Warn("the unsaved documents cannot be backed up", "err", err)
		return
	}
	win.endBackups()
}
