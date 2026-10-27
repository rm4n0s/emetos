package exthost

import (
	"context"
	"sort"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/confine"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// CoreDocuments is the optional part of ScopeOptions.Documents that the document topics of the core need (SPEC §10.19
// "Core request topics"): the window's documents and what it does with them. The scope checks the URIs, the versions, the
// folder and the limits; the window does the work. A scope whose Documents has no such part fails the document topics with
// CoreDocumentsUnavailable. All the methods are called on the UI thread.
type CoreDocuments interface {
	// Document returns the open document with the URI.
	Document(uri document.URI) (*document.Document, bool)
	// OpenFile opens the file (an absolute path inside the folder, checked) as a document of the window, shows it in the
	// editor with the cursor at pos, and calls done, on the UI thread, with the document or the error.
	OpenFile(ctx context.Context, path string, pos extension.Position, done func(*document.Document, error))
	// Save saves the document through the path of the Save command (save participants, format on save) and calls done, on
	// the UI thread, with nil when it was saved or the reason it was not.
	Save(ctx context.Context, doc *document.Document, done func(error))
	// Close closes the editors that show the document, and the document with the last of them. It does not ask: the scope
	// refused a document with unsaved changes unless the client discards them.
	Close(doc *document.Document) error
	// Reveal shows the document in the editor of the window, with the cursor at pos.
	Reveal(doc *document.Document, pos extension.Position) error
	// BeginEdit is called before the edits of a request are made, and the function it returns after: the window keeps the
	// cursors of its editors in their places in the text meanwhile. It may return nil.
	BeginEdit() (end func())
}

// coreDocs is the CoreDocuments of the scope, or nil.
func (s *Scope) coreDocs() CoreDocuments {
	d, _ := s.opts.Documents.(CoreDocuments)
	return d
}

// coreDocument finds the open document of a URI for a topic that changes or shows it, and calls fn with it on the UI thread.
// The document must be a file inside the folder of the workspace (checked off the UI thread, links resolved) or an untitled
// one, which has no file (not when saves is true: saving an untitled document asks for a file name). It fails with
// CoreDocumentsUnavailable, the tags of document.ParseURI, CoreDocumentSchemeUnsupported, the tags of confine.Contains
// (ConfineOutside for a file of another folder) and CoreDocumentNotOpen.
func (s *Scope) coreDocument(c *coreCall, uri string, saves bool, fn func(docs CoreDocuments, doc *document.Document)) {
	docs := s.coreDocs()
	if docs == nil {
		c.reply(nil, errors.New("CoreDocumentsUnavailable", "the scope cannot edit documents"))
		return
	}
	u, err := document.ParseURI(uri)
	if err != nil {
		c.reply(nil, err)
		return
	}
	find := func() {
		doc, ok := docs.Document(u)
		if !ok {
			c.reply(nil, errors.New("CoreDocumentNotOpen", "the workspace has no open document with this URI", "uri", uri))
			return
		}
		fn(docs, doc)
	}
	switch {
	case u.Scheme == document.SchemeFile:
		folder := s.opts.Info.Folder
		go func() {
			_, err := confine.Contains(c.ctx, folder, u.Path)
			s.post(func() {
				if err != nil {
					c.reply(nil, err)
					return
				}
				find()
			})
		}()
	case u.Scheme == document.SchemeUntitled && !saves:
		find()
	default:
		c.reply(nil, errors.New("CoreDocumentSchemeUnsupported", "only a file of the folder can be saved, and only a file or an untitled document can be edited, closed or revealed", "uri", uri))
	}
}

// coreDocumentOpen answers emetos.document.open: the file (relative to the folder, confined) is opened in the workspace and
// shown with the cursor at the line and column. The reply is the document's info.
func (s *Scope) coreDocumentOpen(c *coreCall) {
	var req extension.DocumentOpenRequest
	err := decodeCore(c, &req)
	if err != nil {
		c.reply(nil, err)
		return
	}
	docs := s.coreDocs()
	if docs == nil {
		c.reply(nil, errors.New("CoreDocumentsUnavailable", "the scope cannot edit documents"))
		return
	}
	folder := s.opts.Info.Folder
	go func() {
		abs, err := confine.CheckFile(c.ctx, folder, req.Path)
		s.post(func() {
			if err != nil {
				c.reply(nil, err)
				return
			}
			docs.OpenFile(c.ctx, abs, extension.Position{Line: max(req.Line, 0), Col: max(req.Col, 0)}, func(doc *document.Document, err error) {
				if err != nil {
					c.reply(nil, err)
					return
				}
				c.reply(extension.DocumentInfo{URI: doc.URI().String(), LanguageID: doc.LanguageID(), Version: doc.Version()}, nil)
			})
		})
	}()
}

// coreDocumentRead answers emetos.document.read: the text and the version of a document that is open in the workspace,
// whatever it is (a document outside the folder too: the events show its text already). It fails with
// CoreDocumentsUnavailable, the tags of document.ParseURI, CoreDocumentNotOpen and CoreDocumentTooLarge.
func (s *Scope) coreDocumentRead(c *coreCall) {
	var req extension.DocumentURIRequest
	err := decodeCore(c, &req)
	if err != nil {
		c.reply(nil, err)
		return
	}
	docs := s.coreDocs()
	if docs == nil {
		c.reply(nil, errors.New("CoreDocumentsUnavailable", "the scope cannot edit documents"))
		return
	}
	u, err := document.ParseURI(req.URI)
	if err != nil {
		c.reply(nil, err)
		return
	}
	doc, ok := docs.Document(u)
	if !ok {
		c.reply(nil, errors.New("CoreDocumentNotOpen", "the workspace has no open document with this URI", "uri", req.URI))
		return
	}
	if n := doc.Snapshot().Len(); n > extension.MaxDocumentReadSize {
		c.reply(nil, errors.New("CoreDocumentTooLarge", "the document is larger than the limit of the topic", "uri", req.URI, "size", n, "limit", extension.MaxDocumentReadSize))
		return
	}
	c.reply(extension.DocumentContent{URI: u.String(), LanguageID: doc.LanguageID(), Version: doc.Version(), Text: doc.Text(), Dirty: doc.Dirty()}, nil)
}

// coreDocumentEdit answers emetos.document.edit: the edits are made against the version the request names, as one step of
// the undo history (ApplyTextEdits). A version that is not the document's current one fails with RelayStaleVersion.
func (s *Scope) coreDocumentEdit(c *coreCall) {
	var req extension.DocumentEditRequest
	err := decodeCore(c, &req)
	if err != nil {
		c.reply(nil, err)
		return
	}
	s.coreDocument(c, req.URI, false, func(docs CoreDocuments, doc *document.Document) {
		end := docs.BeginEdit()
		if end != nil {
			defer end()
		}
		reply, err := ApplyTextEdits(doc, req.Version, req.Edits)
		if err != nil {
			c.reply(nil, err)
			return
		}
		reply.URI = doc.URI().String()
		c.reply(reply, nil)
	})
}

// ApplyTextEdits makes the edits of a client in the document: they are in the text of the version given, which must be the
// document's current one; they are checked as a whole (ranges inside the text, on character boundaries, not overlapping, at
// most extension.MaxDocumentEdits and extension.MaxDocumentEditBytes of text) before the first is made; and they are one
// step of the undo history, as the edits of a rename are. It fails with RelayStaleVersion, CoreDocumentReadOnly,
// CoreEditTooLarge and CoreEditInvalid.
func ApplyTextEdits(doc *document.Document, version int, edits []extension.TextChange) (extension.DocumentEditReply, error) {
	if doc.Version() != version {
		return extension.DocumentEditReply{}, errors.New("RelayStaleVersion", "the document changed since the version the edit was made against", "uri", doc.URI().String(), "version", version, "current", doc.Version())
	}
	if doc.ReadOnly() {
		return extension.DocumentEditReply{}, errors.New("CoreDocumentReadOnly", "the document is read-only", "uri", doc.URI().String())
	}
	bytes := 0
	for _, e := range edits {
		bytes += len(e.Text)
	}
	if len(edits) > extension.MaxDocumentEdits || bytes > extension.MaxDocumentEditBytes {
		return extension.DocumentEditReply{}, errors.New("CoreEditTooLarge", "the edit has too many changes or too much text", "edits", len(edits), "bytes", bytes)
	}
	snap := doc.Snapshot()
	out := make([]document.Edit, 0, len(edits))
	type span struct{ start, end int }
	spans := make([]span, 0, len(edits))
	for i, e := range edits {
		start := buffer.Position{Line: e.Range.Start.Line, Col: e.Range.Start.Col}
		end := buffer.Position{Line: e.Range.End.Line, Col: e.Range.End.Col}
		if start.Line < 0 || start.Col < 0 || end.Line < 0 || end.Col < 0 || snap.ValidatePosition(start) != start || snap.ValidatePosition(end) != end || snap.OffsetAt(end) < snap.OffsetAt(start) {
			return extension.DocumentEditReply{}, errors.New("CoreEditInvalid", "a range is outside the text, inside a character, or ends before it starts", "edit", i)
		}
		out = append(out, document.Edit{Range: buffer.Range{Start: start, End: end}, Text: e.Text})
		spans = append(spans, span{snap.OffsetAt(start), snap.OffsetAt(end)})
	}
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].start != spans[j].start {
			return spans[i].start < spans[j].start
		}
		return spans[i].end < spans[j].end
	})
	for i := 1; i < len(spans); i++ {
		if spans[i].start < spans[i-1].end {
			return extension.DocumentEditReply{}, errors.New("CoreEditInvalid", "two edits overlap", "edit", i)
		}
	}
	doc.PushStackElement()
	res, err := doc.Edit(out, document.EditOptions{Kind: document.KindOther})
	doc.PushStackElement()
	if err != nil {
		return extension.DocumentEditReply{}, err
	}
	return extension.DocumentEditReply{Version: doc.Version(), Changed: res.Changed}, nil
}

// coreDocumentSave answers emetos.document.save: the document is saved through the path of the Save command, so the save
// participants of its language and format on save run. The reply comes when the file is written. It fails with the tags
// of coreDocument and the window's (the document was not saved).
func (s *Scope) coreDocumentSave(c *coreCall) {
	var req extension.DocumentURIRequest
	err := decodeCore(c, &req)
	if err != nil {
		c.reply(nil, err)
		return
	}
	s.coreDocument(c, req.URI, true, func(docs CoreDocuments, doc *document.Document) {
		docs.Save(c.ctx, doc, func(err error) {
			if err != nil {
				c.reply(nil, err)
				return
			}
			c.reply(extension.DocumentSaved{URI: doc.URI().String(), Version: doc.Version()}, nil)
		})
	})
}

// coreDocumentClose answers emetos.document.close. A document with unsaved changes is closed only when the request discards
// them (CoreDocumentDirty otherwise).
func (s *Scope) coreDocumentClose(c *coreCall) {
	var req extension.DocumentCloseRequest
	err := decodeCore(c, &req)
	if err != nil {
		c.reply(nil, err)
		return
	}
	s.coreDocument(c, req.URI, false, func(docs CoreDocuments, doc *document.Document) {
		if doc.Dirty() && !req.Discard {
			c.reply(nil, errors.New("CoreDocumentDirty", "the document has unsaved changes; save it or close it with discard", "uri", req.URI))
			return
		}
		uri := doc.URI().String()
		if err := docs.Close(doc); err != nil {
			c.reply(nil, err)
			return
		}
		c.reply(extension.DocumentClosed{URI: uri}, nil)
	})
}

// coreEditorReveal answers emetos.editor.reveal: the open document is shown in the editor of this workspace's window tab
// with the cursor at the line and column.
func (s *Scope) coreEditorReveal(c *coreCall) {
	var req extension.EditorRevealRequest
	err := decodeCore(c, &req)
	if err != nil {
		c.reply(nil, err)
		return
	}
	s.coreDocument(c, req.URI, false, func(docs CoreDocuments, doc *document.Document) {
		if err := docs.Reveal(doc, extension.Position{Line: max(req.Line, 0), Col: max(req.Col, 0)}); err != nil {
			c.reply(nil, err)
			return
		}
		c.reply(extension.EditorRevealReply{URI: doc.URI().String()}, nil)
	})
}
