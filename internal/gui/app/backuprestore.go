package app

import (
	"context"
	"sort"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/storage"
)

// Restoring the backups of a window when it starts (SPEC §10.10). Every backup of the window's id comes back,
// whatever files.hotExit says now and whether the last run ended or crashed: a document with unsaved changes
// opens dirty with the text of its backup, an undo returns to the text the file has, and the cursors, the groups
// and the places of the editors are those of the saved UI state. Untitled documents come back with their names.
// A backup that no saved editor shows opens in the active group.

// beginBackups checks the backups of the window (what cannot be read is removed and logged), lists them for the
// restore, and writes the marker that says the window is open; a marker left by the last run means it did not end.
func (w *Workspace) beginBackups(ctx context.Context, r *restoredWindow) {
	b := &w.backups
	b.mu.Lock()
	store, tracker := b.store, b.tracker
	b.mu.Unlock()
	if store == nil {
		return
	}
	if _, err := store.Check(ctx); err != nil {
		w.Process.Logger.Warn("the backups cannot be checked", "err", err)
	}
	list, problems := store.List(ctx)
	for _, err := range problems {
		w.Process.Logger.Warn("a backup cannot be used", "err", err)
	}
	var uris []string
	for _, backup := range list {
		r.backups[backup.URI] = backup
		uris = append(uris, backup.URI)
	}
	tracker.Adopt(uris...)
	crashed := w.markRunning(ctx)
	b.mu.Lock()
	b.crashed = crashed
	b.mu.Unlock()
}

// openUnshownBackups opens the documents of the backups that no saved editor showed, in the order of their URIs:
// they open in the active group, as VS Code opens the backups that have no editor.
func (w *Workspace) openUnshownBackups(ctx context.Context, r *restoredWindow) {
	uris := make([]string, 0, len(r.backups))
	for uri := range r.backups {
		if _, tried := r.docs[uri]; !tried {
			uris = append(uris, uri)
		}
	}
	sort.Strings(uris)
	for _, value := range uris {
		doc, err := w.openSavedDocument(ctx, r, value)
		if err != nil {
			w.Process.Logger.Warn("a backup cannot be restored", "uri", value, "err", describe(err))
			w.discardBackup(ctx, value)
			continue
		}
		r.docs[value] = doc
		if doc != nil {
			r.extra = append(r.extra, doc)
		}
	}
}

// discardBackup removes the backup of a document that cannot come back.
func (w *Workspace) discardBackup(ctx context.Context, uri string) {
	b := &w.backups
	b.mu.Lock()
	store := b.store
	b.mu.Unlock()
	if store == nil {
		return
	}
	if err := store.Discard(ctx, uri); err != nil {
		w.Process.Logger.Warn("a backup cannot be removed", "uri", uri, "err", err)
	}
}

// restoreFromBackup makes the document of a backup: a file is read as it is on the disk and the text of the backup
// is put over it as an edit; an untitled document, and a file that is gone, start empty. A file that changed since
// the backup (its size or modification time is not the one the backup noted) leaves the document conflicted with
// the stat of the backup, so a save fails until the user overwrites; a file that is gone leaves it orphaned. The
// backup of a document that has no unsaved changes after all is removed.
func (w *Workspace) restoreFromBackup(ctx context.Context, r *restoredWindow, uri document.URI, b storage.Backup) (*document.Document, error) {
	u := document.Unsaved{
		Text: b.Content, Encoding: b.Encoding, HasBOM: b.BOM, LineEnding: b.LineEnding, LanguageID: b.Language,
	}
	var doc *document.Document
	if uri.Scheme == document.SchemeFile {
		var err error
		doc, err = w.Documents.OpenFile(ctx, uri.Path, document.NewOptions())
		if err == nil {
			disk := doc.Stat()
			if (b.Stat != nil && !b.Stat.Same(disk)) || (b.Stat == nil && b.Orphaned) {
				if b.Stat != nil {
					u.Stat = b.Stat.FileStat()
				}
				u.Conflict = &disk
			}
		} else {
			// the file is gone, or cannot be read now (binary, too large): the document keeps the backup
			appErr, _ := errors.FromError(err)
			w.Process.Logger.Info("the file of a backup cannot be read", "uri", uri.String(), "err", describe(err))
			doc, _ = w.Documents.OpenBlank(uri, document.Options{LanguageID: b.Language})
			u.Orphaned = appErr != nil && appErr.Tag == "FileNotFound"
			if b.Stat != nil {
				u.Stat = b.Stat.FileStat()
			}
		}
	} else {
		doc, _ = w.Documents.OpenBlank(uri, document.Options{LanguageID: b.Language})
	}
	dirty, err := doc.RestoreUnsaved(u)
	if err != nil {
		return nil, err
	}
	if !dirty {
		// the text of the backup is the one of the file: nothing is unsaved, and an untitled document is empty
		w.discardBackup(ctx, b.URI)
		if uri.Scheme == document.SchemeUntitled {
			w.Documents.Close(uri)
			return nil, errors.New("BackupEmpty", "the backup has no unsaved changes", "uri", b.URI)
		}
		return doc, nil
	}
	r.restored++
	switch {
	case u.Orphaned:
		r.gone = append(r.gone, uri.Title())
	case u.Conflict != nil:
		w.backups.mu.Lock()
		if w.backups.changed == nil {
			w.backups.changed = map[*document.Document]bool{}
		}
		w.backups.changed[doc] = true
		w.backups.mu.Unlock()
	}
	return doc, nil
}

// changedSinceBackup tells whether a restored document is conflicted because its file changed after its backup was
// made; the notification of the conflict then says so.
func (w *Workspace) changedSinceBackup(doc *document.Document) bool {
	w.backups.mu.Lock()
	defer w.backups.mu.Unlock()
	return w.backups.changed[doc]
}

// noticeRestoredBackups tells the user what the restore did that the editors do not show: the files that are gone,
// and, when the last run did not end, that the documents came back from backups. The files that changed on the
// disk are told by the notification of their conflict (see showExternalNote).
func (w *Workspace) noticeRestoredBackups(r *restoredWindow) {
	if len(r.gone) > 0 {
		w.Notify.Warn("Emetos", NoticeDeletedOnDisk(r.gone))
	}
	w.backups.mu.Lock()
	crashed := w.backups.crashed
	w.backups.mu.Unlock()
	if crashed && r.restored > 0 {
		w.Notify.Info("Emetos", NoticeBackupsRestored(r.restored))
	}
}
