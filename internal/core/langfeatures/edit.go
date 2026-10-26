package langfeatures

import (
	"context"
	"sort"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/lsp"
)

// ChangeKind says what a Change of a WorkspaceEdit does.
type ChangeKind int

// The kinds of change.
const (
	// ChangeText edits the text of a document.
	ChangeText ChangeKind = iota
	// ChangeCreate creates the file URI.
	ChangeCreate
	// ChangeRename renames the file URI to NewURI.
	ChangeRename
	// ChangeDelete deletes the file or folder URI.
	ChangeDelete
)

// Change is one member of a WorkspaceEdit.
type Change struct {
	Kind ChangeKind
	// URI is the document or file, NewURI the target of a rename.
	URI, NewURI document.URI
	// Version, when set, is the version of the document the Edits are for; the edit is refused when the document has
	// another.
	Version *int
	// Edits are the edits of a ChangeText, their ranges in the text of the document as it was before the change.
	Edits []TextEdit
	// The options of the file changes.
	Overwrite, IgnoreIfExists, Recursive, IgnoreIfNotExists bool
}

// WorkspaceEdit changes documents and files together (SPEC §9.12 "Features model"). Make one from a provider's answer with
// Changes, or from a language server's with FromLSPEdit.
type WorkspaceEdit struct {
	// Label says what the edit is, for the undo label and the preview.
	Label string
	// Changes are made in order.
	Changes []Change

	server *serverEdit
}

// serverEdit is an edit as a language server sent it: its ranges count in the server's encoding, so they are converted when
// the edit is applied, with the text of each document as it is then.
type serverEdit struct {
	enc  lsp.Encoding
	edit lsp.WorkspaceEdit
}

// FromLSPEdit makes the WorkspaceEdit of an edit that a language server sent, whose positions count in the encoding.
func FromLSPEdit(enc lsp.Encoding, we lsp.WorkspaceEdit) *WorkspaceEdit {
	return &WorkspaceEdit{server: &serverEdit{enc: enc, edit: we}}
}

// FileOptions are the options of a file operation.
type FileOptions struct {
	Overwrite, IgnoreIfExists, Recursive, IgnoreIfNotExists bool
}

// EditHost is what a window offers to apply a WorkspaceEdit: its documents and its file operations (SPEC §9.12). All its
// methods are called on the goroutine that owns the documents.
type EditHost interface {
	// Document returns the open document with the URI.
	Document(uri document.URI) (*document.Document, bool)
	// OpenForEdit opens a file that is not open as a document of the window, which stays open and so shows its changes as
	// unsaved ones, as VS Code leaves the files a refactoring touched. The read may take a moment.
	OpenForEdit(ctx context.Context, path string) (*document.Document, error)
	// CreateFile creates an empty file, with the explorer's rules (a missing folder is made).
	CreateFile(ctx context.Context, path string, opts FileOptions) error
	// RenameFile moves a file or folder, with the explorer's rules; the open documents under it follow.
	RenameFile(ctx context.Context, from, to string, opts FileOptions) error
	// DeleteFile deletes a file or folder, by moving it to the trash like the explorer does.
	DeleteFile(ctx context.Context, path string, opts FileOptions) error
}

// ApplyResult says what Apply did.
type ApplyResult struct {
	// Applied is true when every change was made.
	Applied bool
	// FailedChange is the index of the change that failed, or -1.
	FailedChange int
	// Documents are the documents that were edited, in the order of the changes, each once.
	Documents []*document.Document
}

// textRun is a series of text changes with no file operation between them: the documents they touch are edited together.
type textRun struct {
	first   int
	changes []Change
	// resolve turns the edits of the change (by index in changes) into document edits for a snapshot.
	resolve []func(buffer.Snapshot) ([]document.Edit, error)
}

// plan is a WorkspaceEdit split into text runs and file operations in order.
type plan struct {
	steps []step
	// fileOps tells whether any step is a file operation.
	fileOps bool
}

type step struct {
	run  *textRun
	file *Change
	at   int
}

// Apply makes the edit (SPEC §9.12): the text edits of each document are one batch, so one undo step per document; an
// open document is edited in place, a closed file is opened by the host and left open and unsaved (VS Code's rule); the file
// operations go through the host's. An edit with only text changes is all or nothing: every document is found and every
// range checked before the first is changed. With file operations the changes are made in order and the first that fails
// stops the rest (the protocol's "abort"), after the ones before it were made. A URI the editor has no documents for fails
// with the tags of lsp.ParseDocumentURI. The error has the tags WorkspaceEditInvalid, WorkspaceEditDocumentUnavailable, WorkspaceEditStale, WorkspaceEditReadOnly,
// WorkspaceEditOverlap and WorkspaceEditFileFailed; ApplyResult.FailedChange says which change.
func Apply(ctx context.Context, host EditHost, we *WorkspaceEdit) (ApplyResult, error) {
	res := ApplyResult{FailedChange: -1}
	pl, err := planOf(we)
	if err != nil {
		return res, err
	}
	seen := map[*document.Document]bool{}
	for _, st := range pl.steps {
		if st.file != nil {
			if err := applyFile(ctx, host, *st.file); err != nil {
				res.FailedChange = st.at
				return res, err
			}
			continue
		}
		docs, err := applyRun(ctx, host, st.run)
		for _, d := range docs {
			if !seen[d] {
				seen[d] = true
				res.Documents = append(res.Documents, d)
			}
		}
		if err != nil {
			res.FailedChange = st.at
			return res, err
		}
	}
	res.Applied = true
	return res, nil
}

// planOf splits the edit into runs and file operations.
func planOf(we *WorkspaceEdit) (plan, error) {
	if we == nil {
		return plan{}, errors.New("WorkspaceEditInvalid", "there is no edit")
	}
	var pl plan
	var cur *textRun
	add := func(i int, c Change, resolve func(buffer.Snapshot) ([]document.Edit, error)) {
		if c.Kind != ChangeText {
			cur = nil
			pl.fileOps = true
			cc := c
			pl.steps = append(pl.steps, step{file: &cc, at: i})
			return
		}
		if cur == nil {
			cur = &textRun{first: i}
			pl.steps = append(pl.steps, step{run: cur, at: i})
		}
		cur.changes = append(cur.changes, c)
		cur.resolve = append(cur.resolve, resolve)
	}
	if we.server != nil {
		if err := planServerEdit(we.server, add); err != nil {
			return plan{}, err
		}
	}
	for i, c := range we.Changes {
		edits := c.Edits
		add(i, c, func(buffer.Snapshot) ([]document.Edit, error) {
			out := make([]document.Edit, len(edits))
			for k, e := range edits {
				out[k] = document.Edit{Range: e.Range, Text: e.Text}
			}
			return out, nil
		})
	}
	return pl, nil
}

// planServerEdit adds the changes of an edit of a language server. documentChanges win over changes (the protocol says
// so); changes are made in the order of their URIs, which the map does not keep, so they are sorted.
func planServerEdit(se *serverEdit, add func(int, Change, func(buffer.Snapshot) ([]document.Edit, error))) error {
	enc := se.enc
	convert := func(edits []lsp.TextEdit) func(buffer.Snapshot) ([]document.Edit, error) {
		return func(snap buffer.Snapshot) ([]document.Edit, error) {
			out := make([]document.Edit, len(edits))
			for k, e := range edits {
				out[k] = document.Edit{Range: enc.RangeFromLSP(snap, e.Range), Text: e.NewText}
			}
			return out, nil
		}
	}
	if len(se.edit.DocumentChanges) == 0 {
		uris := make([]string, 0, len(se.edit.Changes))
		for u := range se.edit.Changes {
			uris = append(uris, u)
		}
		sort.Strings(uris)
		for i, u := range uris {
			uri, err := lsp.ParseDocumentURI(u)
			if err != nil {
				return err
			}
			add(i, Change{Kind: ChangeText, URI: uri}, convert(se.edit.Changes[u]))
		}
		return nil
	}
	for i, dc := range se.edit.DocumentChanges {
		c := Change{Overwrite: dc.Overwrite, IgnoreIfExists: dc.IgnoreIfExists, Recursive: dc.Recursive, IgnoreIfNotExists: dc.IgnoreIfNotExists}
		parse := lsp.ParseDocumentURI
		var err error
		switch dc.Kind {
		case lsp.ChangeCreateFile:
			c.Kind = ChangeCreate
			c.URI, err = parse(dc.URI)
		case lsp.ChangeDeleteFile:
			c.Kind = ChangeDelete
			c.URI, err = parse(dc.URI)
		case lsp.ChangeRenameFile:
			c.Kind = ChangeRename
			if c.URI, err = parse(dc.OldURI); err == nil {
				c.NewURI, err = parse(dc.NewURI)
			}
		default:
			c.Kind = ChangeText
			if dc.TextDocument == nil {
				return errors.New("WorkspaceEditInvalid", "the edits of a document name no document")
			}
			c.Version = dc.TextDocument.Version
			c.URI, err = parse(dc.TextDocument.URI)
		}
		if err != nil {
			return err
		}
		add(i, c, convert(dc.Edits))
	}
	return nil
}

// target is a document of a run with the edits to make.
type target struct {
	doc   *document.Document
	edits []document.Edit
}

// applyRun edits the documents of a run: they are found (closed files are opened), checked, and then changed.
func applyRun(ctx context.Context, host EditHost, run *textRun) ([]*document.Document, error) {
	var order []document.URI
	byURI := map[document.URI]*target{}
	for i, c := range run.changes {
		t := byURI[c.URI]
		if t == nil {
			doc, err := findDocument(ctx, host, c.URI)
			if err != nil {
				return nil, err
			}
			t = &target{doc: doc}
			byURI[c.URI] = t
			order = append(order, c.URI)
		}
		if c.Version != nil && t.doc.Version() != *c.Version {
			return nil, errors.New("WorkspaceEditStale", "the document changed since the edit was made", "uri", c.URI.String(), "version", *c.Version, "current", t.doc.Version())
		}
		edits, err := run.resolve[i](t.doc.Snapshot())
		if err != nil {
			return nil, err
		}
		t.edits = append(t.edits, edits...)
	}
	for _, u := range order {
		t := byURI[u]
		if t.doc.ReadOnly() {
			return nil, errors.New("WorkspaceEditReadOnly", "the document is read-only", "uri", u.String())
		}
		if overlaps(t.doc.Snapshot(), t.edits) {
			return nil, errors.New("WorkspaceEditOverlap", "two edits of a document overlap", "uri", u.String())
		}
	}
	var done []*document.Document
	for _, u := range order {
		t := byURI[u]
		t.doc.PushStackElement()
		if _, err := t.doc.Edit(t.edits, document.EditOptions{Kind: document.KindOther}); err != nil {
			return done, err
		}
		t.doc.PushStackElement()
		done = append(done, t.doc)
	}
	return done, nil
}

// findDocument is the open document with the URI, or the file opened by the host.
func findDocument(ctx context.Context, host EditHost, uri document.URI) (*document.Document, error) {
	if doc, ok := host.Document(uri); ok {
		return doc, nil
	}
	if uri.Scheme != document.SchemeFile {
		return nil, errors.New("WorkspaceEditDocumentUnavailable", "only a file can be edited when it is not open", "uri", uri.String())
	}
	doc, err := host.OpenForEdit(ctx, uri.Path)
	if err != nil {
		if _, ok := errors.FromError(err); ok {
			return nil, err
		}
		return nil, errors.NewErr("WorkspaceEditDocumentUnavailable", err).SetMetadata("uri", uri.String())
	}
	return doc, nil
}

// overlaps tells whether two of the edits overlap in the text, the way the document refuses.
func overlaps(snap buffer.Snapshot, edits []document.Edit) bool {
	type span struct{ start, end int }
	spans := make([]span, 0, len(edits))
	for _, e := range edits {
		r := snap.ValidateRange(e.Range)
		spans = append(spans, span{snap.OffsetAt(r.Start), snap.OffsetAt(r.End)})
	}
	sort.SliceStable(spans, func(i, j int) bool {
		if spans[i].start != spans[j].start {
			return spans[i].start < spans[j].start
		}
		return spans[i].end < spans[j].end
	})
	for i := 1; i < len(spans); i++ {
		if spans[i].start < spans[i-1].end {
			return true
		}
	}
	return false
}

// applyFile makes a file operation through the host. A host error that is not ours is wrapped with WorkspaceEditFileFailed.
func applyFile(ctx context.Context, host EditHost, c Change) error {
	opts := FileOptions{Overwrite: c.Overwrite, IgnoreIfExists: c.IgnoreIfExists, Recursive: c.Recursive, IgnoreIfNotExists: c.IgnoreIfNotExists}
	if c.URI.Scheme != document.SchemeFile || (c.Kind == ChangeRename && c.NewURI.Scheme != document.SchemeFile) {
		return errors.New("WorkspaceEditInvalid", "a file operation is for files", "uri", c.URI.String())
	}
	var err error
	switch c.Kind {
	case ChangeCreate:
		err = host.CreateFile(ctx, c.URI.Path, opts)
	case ChangeRename:
		err = host.RenameFile(ctx, c.URI.Path, c.NewURI.Path, opts)
	case ChangeDelete:
		err = host.DeleteFile(ctx, c.URI.Path, opts)
	}
	if err == nil {
		return nil
	}
	if _, ok := errors.FromError(err); ok {
		return err
	}
	return errors.NewErr("WorkspaceEditFileFailed", err).SetMetadata("uri", c.URI.String())
}
