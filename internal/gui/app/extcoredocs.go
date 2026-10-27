package app

import (
	"context"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/gui/exthost"
)

// The window's side of the document topics of the core (SPEC §10.19 "As built (step 16.1)"): extensionDocuments is the
// exthost.CoreDocuments of a workspace. The scope has checked the URI, the version and the folder; these methods open,
// save, close and show the documents of the window, on the UI thread, the way the user's commands do.

var _ exthost.CoreDocuments = extensionDocuments{}

// Document implements exthost.CoreDocuments.
func (d extensionDocuments) Document(uri document.URI) (*document.Document, bool) {
	return d.w.Documents.Get(uri)
}

// place puts the cursor of an editor at a position of the document (clamped into the text).
func place(doc *document.Document, pos extension.Position) func(*textEditor) {
	return func(e *textEditor) {
		p := doc.Snapshot().ValidatePosition(buffer.Position{Line: pos.Line, Col: pos.Col})
		e.view.Cursors().SetSelections([]buffer.Selection{{Anchor: p, Active: p}})
	}
}

// OpenFile implements exthost.CoreDocuments: the file is read on a goroutine and the document is shown in the active group,
// with the cursor at pos; a window that is not on screen keeps the document open without an editor. The error is the
// tag of the read (fileio.Read) or ExtDocumentOpenCanceled when the request ended first.
func (d extensionDocuments) OpenFile(ctx context.Context, path string, pos extension.Position, done func(*document.Document, error)) {
	w := d.w
	s := w.liveShell()
	go func() {
		var doc *document.Document
		var err error
		if s == nil {
			doc, err = w.Documents.OpenFile(ctx, path, document.NewOptions())
		} else {
			doc, err = w.Documents.Acquire(ctx, path, document.NewOptions())
		}
		w.onUI(func() {
			switch {
			case err != nil:
				done(nil, err)
			case ctx.Err() != nil:
				if s != nil {
					w.Documents.Release(doc)
				}
				done(nil, errors.NewErr("ExtDocumentOpenCanceled", ctx.Err()).SetMetadata("path", path))
			case s == nil || !s.Mounted():
				if s != nil {
					w.Documents.Release(doc)
				}
				done(doc, nil)
			default:
				s.show(widgets.WidgetsBindingOf(s.Context()), doc, openOptions{Place: place(doc, pos)})
				done(doc, nil)
			}
		})
	}()
}

// Save implements exthost.CoreDocuments: the save of the Save command, with its participants. The window shows the failure
// of a save as a notification; the error here says that it was not saved. A window that is not on screen cannot save
// (ExtDocumentSaveUnavailable).
func (d extensionDocuments) Save(_ context.Context, doc *document.Document, done func(error)) {
	s := d.w.liveShell()
	if s == nil {
		done(errors.New("ExtDocumentSaveUnavailable", "the window tab is not on screen, so it cannot save", "uri", doc.URI().String()))
		return
	}
	s.saveDocument(doc, saveRequest{}, func(saved bool) {
		if !saved {
			done(errors.New("ExtDocumentNotSaved", "the document was not saved; the window tab shows why", "uri", doc.URI().String()))
			return
		}
		done(nil)
	})
}

// Close implements exthost.CoreDocuments: the editors that show the document close without a question and the document
// goes with the last of them; a document that no editor shows is closed by itself.
func (d extensionDocuments) Close(doc *document.Document) error {
	w := d.w
	if s := w.liveShell(); s != nil {
		var targets []editorgroup.CloseTarget
		for _, g := range s.group.grid.Groups(editorgroup.GridAppearance) {
			var editors []editorgroup.EditorInput
			for _, in := range g.Model.Editors(editorgroup.Sequential, false) {
				if editorgroup.EditedDocument(in) == doc {
					editors = append(editors, in)
				}
			}
			if len(editors) > 0 {
				targets = append(targets, editorgroup.CloseTarget{Group: g, Editors: editors})
			}
		}
		if len(targets) > 0 {
			s.closeNow(targets)
		}
	}
	if cur, ok := w.Documents.Get(doc.URI()); ok && cur == doc && w.Documents.Refs(doc) == 0 {
		w.Documents.Close(doc.URI())
	}
	return nil
}

// Reveal implements exthost.CoreDocuments: the editor that shows the document comes to the front with the cursor at pos
// (a new one when none shows it). A window that is not on screen fails with ExtDocumentRevealUnavailable.
func (d extensionDocuments) Reveal(doc *document.Document, pos extension.Position) error {
	s := d.w.liveShell()
	if s == nil {
		return errors.New("ExtDocumentRevealUnavailable", "the window tab is not on screen", "uri", doc.URI().String())
	}
	d.w.Documents.Retain(doc) // show lets go of one use
	s.show(widgets.WidgetsBindingOf(s.Context()), doc, openOptions{Place: place(doc, pos)})
	return nil
}

// BeginEdit implements exthost.CoreDocuments: the cursors of the editors follow the text while the edits are made.
func (d extensionDocuments) BeginEdit() (end func()) {
	return workspaceEdits(d).BeginEdit()
}
