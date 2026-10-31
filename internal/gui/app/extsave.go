package app

import (
	"context"
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/document"
)

// Dirty and SaveAll of the documents an extension sees (SPEC §10.30 "Stash": git.promptToSaveFilesBeforeStash; the same pair serves the
// commit): the files with unsaved changes, and the Save command for a list of them.

// Dirty implements exthost.ContentHost: the URIs of the open files with unsaved changes, in the order they were opened.
func (d extensionDocuments) Dirty() []string {
	var out []string
	for _, doc := range d.w.Documents.All() {
		if u := doc.URI(); u.Scheme == document.SchemeFile && doc.Dirty() {
			out = append(out, u.String())
		}
	}
	return out
}

// SaveAll implements exthost.ContentHost: each document is saved as the Save command saves it (with its participants), and the call
// returns when all are written. It runs on the goroutine of the extension and waits for the UI thread, which does the saving.
func (d extensionDocuments) SaveAll(ctx context.Context, uris []string) error {
	done := make(chan error, 1)
	var once sync.Once
	finish := func(err error) { once.Do(func() { done <- err }) }
	d.w.onUI(func() { d.saveAllOnUI(uris, finish) })
	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		return errors.NewErr("ExtDocumentSaveCancelled", ctx.Err()).SetMetadata("documents", len(uris))
	}
}

// saveAllOnUI starts the saves on the UI thread and calls done once, with the failure of the first document that was not saved.
func (d extensionDocuments) saveAllOnUI(uris []string, done func(error)) {
	s := d.w.liveShell()
	if s == nil {
		done(errors.New("ExtDocumentSaveUnavailable", "the window tab is not on screen, so it cannot save", "documents", len(uris)))
		return
	}
	var docs []*document.Document
	for _, raw := range uris {
		u, err := document.ParseURI(raw)
		if err != nil {
			done(err)
			return
		}
		if doc, ok := d.w.Documents.Get(u); ok && doc.Dirty() {
			docs = append(docs, doc)
		}
	}
	pending := len(docs)
	if pending == 0 {
		done(nil)
		return
	}
	var failed error
	for _, doc := range docs {
		s.saveDocument(doc, saveRequest{}, func(saved bool) {
			if !saved && failed == nil {
				failed = errors.New("ExtDocumentNotSaved", "the document was not saved; the window tab shows why", "uri", doc.URI().String())
			}
			if pending--; pending == 0 {
				done(failed)
			}
		})
	}
}
