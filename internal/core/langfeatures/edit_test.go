package langfeatures_test

import (
	"context"
	"slices"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/document"
	. "github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/lsp"
)

// memHost is an EditHost over documents in memory and a fake disk.
type memHost struct {
	docs map[document.URI]*document.Document
	// disk holds the closed files by path.
	disk map[string]string
	// log lists the operations in the order they happened.
	log []string
	// openErr and fileErr make OpenForEdit and the file operations fail.
	openErr, fileErr error
	opts             []FileOptions
}

func newHost() *memHost {
	return &memHost{docs: map[document.URI]*document.Document{}, disk: map[string]string{}}
}

func (h *memHost) open(path, text string) *document.Document {
	d := document.New(document.FileURI(path), text, document.NewOptions())
	h.docs[d.URI()] = d
	return d
}

func (h *memHost) Document(uri document.URI) (*document.Document, bool) {
	d, ok := h.docs[uri]
	return d, ok
}

func (h *memHost) OpenForEdit(_ context.Context, path string) (*document.Document, error) {
	if h.openErr != nil {
		return nil, h.openErr
	}
	text, ok := h.disk[path]
	if !ok {
		return nil, errors.New("FileNotFound", "no such file", "path", path)
	}
	h.log = append(h.log, "open "+path)
	return h.open(path, text), nil
}

func (h *memHost) CreateFile(_ context.Context, path string, o FileOptions) error {
	h.opts = append(h.opts, o)
	if h.fileErr != nil {
		return h.fileErr
	}
	h.log = append(h.log, "create "+path)
	h.disk[path] = ""
	return nil
}

func (h *memHost) RenameFile(_ context.Context, from, to string, o FileOptions) error {
	h.opts = append(h.opts, o)
	if h.fileErr != nil {
		return h.fileErr
	}
	h.log = append(h.log, "rename "+from+" "+to)
	h.disk[to] = h.disk[from]
	delete(h.disk, from)
	return nil
}

func (h *memHost) DeleteFile(_ context.Context, path string, o FileOptions) error {
	h.opts = append(h.opts, o)
	if h.fileErr != nil {
		return h.fileErr
	}
	h.log = append(h.log, "delete "+path)
	delete(h.disk, path)
	return nil
}

func textChange(path string, edits ...TextEdit) Change {
	return Change{Kind: ChangeText, URI: document.FileURI(path), Edits: edits}
}

func replace(l1, c1, l2, c2 int, text string) TextEdit {
	return TextEdit{Range: rng(l1, c1, l2, c2), Text: text}
}

func TestApplyEditsAnOpenDocumentWithOneUndoStep(t *testing.T) {
	h := newHost()
	d := h.open("/work/a.go", "one\ntwo\nthree\n")
	res, err := Apply(context.Background(), h, &WorkspaceEdit{Changes: []Change{
		textChange("/work/a.go", replace(0, 0, 0, 3, "1"), replace(2, 0, 2, 5, "3")),
		textChange("/work/a.go", replace(1, 0, 1, 3, "2")), // a second entry for the same document joins the batch
	}})
	if err != nil || !res.Applied || res.FailedChange != -1 {
		t.Fatalf("Apply = %+v, %v", res, err)
	}
	if got := d.Text(); got != "1\n2\n3\n" {
		t.Errorf("text = %q", got)
	}
	if len(res.Documents) != 1 || res.Documents[0] != d {
		t.Errorf("documents = %v", res.Documents)
	}
	if _, err := d.Undo(); err != nil {
		t.Fatal(err)
	}
	if got := d.Text(); got != "one\ntwo\nthree\n" {
		t.Errorf("after one undo = %q", got)
	}
	if d.CanUndo() {
		t.Error("the edit took more than one undo step")
	}
}

func TestApplyOpensClosedFilesAndLeavesThemOpenAndUnsaved(t *testing.T) {
	h := newHost()
	open := h.open("/work/a.go", "a\n")
	h.disk["/work/b.go"] = "b\n"
	res, err := Apply(context.Background(), h, &WorkspaceEdit{Changes: []Change{
		textChange("/work/a.go", replace(0, 0, 0, 1, "A")),
		textChange("/work/b.go", replace(0, 0, 0, 1, "B")),
	}})
	if err != nil || len(res.Documents) != 2 {
		t.Fatalf("Apply = %+v, %v", res, err)
	}
	closed, ok := h.Document(document.FileURI("/work/b.go"))
	if !ok || closed.Text() != "B\n" || !closed.Dirty() || open.Text() != "A\n" {
		t.Errorf("closed file = %v %q %v", ok, closed.Text(), closed.Dirty())
	}
	if !slices.Equal(h.log, []string{"open /work/b.go"}) {
		t.Errorf("log = %v", h.log)
	}
	if h.disk["/work/b.go"] != "b\n" {
		t.Error("the file on disk was written")
	}
	// the closed file has one undo step too
	if _, err := closed.Undo(); err != nil || closed.Text() != "b\n" || closed.CanUndo() {
		t.Errorf("undo of the closed file: %q %v", closed.Text(), err)
	}
}

func TestApplyIsAllOrNothingForTextEdits(t *testing.T) {
	tests := []struct {
		name  string
		setup func(h *memHost)
		edit  func() *WorkspaceEdit
		route string
	}{
		{
			name: "a stale version",
			edit: func() *WorkspaceEdit {
				v := 99
				c := textChange("/work/b.go", replace(0, 0, 0, 1, "X"))
				c.Version = &v
				return &WorkspaceEdit{Changes: []Change{textChange("/work/a.go", replace(0, 0, 0, 1, "X")), c}}
			},
			route: "applyRun.WorkspaceEditStale",
		},
		{
			name: "overlapping edits",
			edit: func() *WorkspaceEdit {
				return &WorkspaceEdit{Changes: []Change{
					textChange("/work/a.go", replace(0, 0, 0, 1, "X")),
					textChange("/work/b.go", replace(0, 0, 0, 2, "X"), replace(0, 1, 0, 3, "Y")),
				}}
			},
			route: "applyRun.WorkspaceEditOverlap",
		},
		{
			name:  "a read-only document",
			setup: func(h *memHost) { h.docs[document.FileURI("/work/b.go")].SetReadOnly(true) },
			edit: func() *WorkspaceEdit {
				return &WorkspaceEdit{Changes: []Change{textChange("/work/a.go", replace(0, 0, 0, 1, "X")), textChange("/work/b.go", replace(0, 0, 0, 1, "X"))}}
			},
			route: "applyRun.WorkspaceEditReadOnly",
		},
		{
			name: "a closed file that cannot be opened",
			edit: func() *WorkspaceEdit {
				return &WorkspaceEdit{Changes: []Change{textChange("/work/a.go", replace(0, 0, 0, 1, "X")), textChange("/work/missing.go", replace(0, 0, 0, 1, "X"))}}
			},
			route: "findDocument->memHost.OpenForEdit.FileNotFound",
		},
		{
			name: "a document that is not a file and is not open",
			edit: func() *WorkspaceEdit {
				c := textChange("/work/a.go", replace(0, 0, 0, 1, "X"))
				c.URI = document.UntitledURI(7)
				return &WorkspaceEdit{Changes: []Change{textChange("/work/a.go", replace(0, 0, 0, 1, "X")), c}}
			},
			route: "findDocument.WorkspaceEditDocumentUnavailable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHost()
			a := h.open("/work/a.go", "aaa\n")
			b := h.open("/work/b.go", "bbb\n")
			if tt.setup != nil {
				tt.setup(h)
			}
			res, err := Apply(context.Background(), h, tt.edit())
			if err == nil || res.Applied {
				t.Fatalf("Apply = %+v, %v", res, err)
			}
			routeOf(t, err, tt.route)
			if a.Text() != "aaa\n" || b.Text() != "bbb\n" || len(res.Documents) != 0 {
				t.Errorf("a document changed: %q %q", a.Text(), b.Text())
			}
		})
	}
}

func TestApplyOpenErrorThatIsNotOurs(t *testing.T) {
	h := newHost()
	h.openErr = context.DeadlineExceeded
	_, err := Apply(context.Background(), h, &WorkspaceEdit{Changes: []Change{textChange("/work/b.go", replace(0, 0, 0, 1, "X"))}})
	routeOf(t, err, "findDocument.WorkspaceEditDocumentUnavailable")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("the cause was lost: %v", err)
	}
}

func TestApplyFileOperationsInOrder(t *testing.T) {
	h := newHost()
	h.disk["/work/old.go"] = "old\n"
	h.disk["/work/gone.go"] = "gone\n"
	opts := Change{Overwrite: true, IgnoreIfExists: true}
	create := Change{Kind: ChangeCreate, URI: document.FileURI("/work/new.go"), Overwrite: opts.Overwrite, IgnoreIfExists: opts.IgnoreIfExists}
	rename := Change{Kind: ChangeRename, URI: document.FileURI("/work/old.go"), NewURI: document.FileURI("/work/renamed.go"), Overwrite: true}
	del := Change{Kind: ChangeDelete, URI: document.FileURI("/work/gone.go"), Recursive: true, IgnoreIfNotExists: true}
	res, err := Apply(context.Background(), h, &WorkspaceEdit{Changes: []Change{
		create,
		textChange("/work/new.go", replace(0, 0, 0, 0, "package new\n")), // the file made just before is opened and edited
		rename, del,
	}})
	if err != nil || !res.Applied {
		t.Fatalf("Apply = %+v, %v", res, err)
	}
	want := []string{"create /work/new.go", "open /work/new.go", "rename /work/old.go /work/renamed.go", "delete /work/gone.go"}
	if !slices.Equal(h.log, want) {
		t.Errorf("log = %v, want %v", h.log, want)
	}
	if d, _ := h.Document(document.FileURI("/work/new.go")); d == nil || d.Text() != "package new\n" {
		t.Errorf("new.go = %v", d)
	}
	wantOpts := []FileOptions{{Overwrite: true, IgnoreIfExists: true}, {Overwrite: true}, {Recursive: true, IgnoreIfNotExists: true}}
	if !slices.Equal(h.opts, wantOpts) {
		t.Errorf("options = %+v, want %+v", h.opts, wantOpts)
	}
}

func TestApplyStopsAtTheFirstFailedFileOperation(t *testing.T) {
	h := newHost()
	a := h.open("/work/a.go", "a\n")
	h.fileErr = context.Canceled
	res, err := Apply(context.Background(), h, &WorkspaceEdit{Changes: []Change{
		textChange("/work/a.go", replace(0, 0, 0, 1, "A")),
		{Kind: ChangeDelete, URI: document.FileURI("/work/x.go")},
		textChange("/work/a.go", replace(0, 0, 0, 1, "Z")),
	}})
	routeOf(t, err, "applyFile.WorkspaceEditFileFailed")
	if res.Applied || res.FailedChange != 1 || a.Text() != "A\n" {
		t.Errorf("result = %+v, text %q: the changes before the failure stay, the ones after do not run", res, a.Text())
	}
	// a file operation on something that is not a file
	_, err = Apply(context.Background(), newHost(), &WorkspaceEdit{Changes: []Change{{Kind: ChangeCreate, URI: document.UntitledURI(1)}}})
	routeOf(t, err, "applyFile.WorkspaceEditInvalid")
	// an error of the host that is ours is returned as it is
	h2 := newHost()
	h2.fileErr = errors.New("FileOpDestinationExists", "exists")
	_, err = Apply(context.Background(), h2, &WorkspaceEdit{Changes: []Change{{Kind: ChangeCreate, URI: document.FileURI("/work/x.go")}}})
	routeOf(t, err, "FileOpDestinationExists")
}

func TestApplyNilEdit(t *testing.T) {
	_, err := Apply(context.Background(), newHost(), nil)
	routeOf(t, err, "planOf.WorkspaceEditInvalid")
}

func TestApplyEditOfAServer(t *testing.T) {
	// the server counts UTF-16 units: y is at 3 and the emoji takes two
	h := newHost()
	d := h.open("/work/a.go", emojiLine)
	h.disk["/work/b.go"] = "x😀y\n"
	one := 1
	we := FromLSPEdit(lsp.UTF16, lsp.WorkspaceEdit{DocumentChanges: []lsp.DocumentChange{
		{TextDocument: &lsp.OptionalVersionedTextDocumentIdentifier{URI: "file:///work/a.go", Version: &one}, Edits: []lsp.TextEdit{
			{Range: lsp.Range{Start: lsp.Position{Character: 3}, End: lsp.Position{Character: 4}}, NewText: "z"},
		}},
		{TextDocument: &lsp.OptionalVersionedTextDocumentIdentifier{URI: "file:///work/b.go"}, Edits: []lsp.TextEdit{
			{Range: lsp.Range{Start: lsp.Position{Character: 3}, End: lsp.Position{Character: 4}}, NewText: "Z"},
		}},
		{Kind: lsp.ChangeCreateFile, URI: "file:///work/c.go", IgnoreIfExists: true},
		{Kind: lsp.ChangeRenameFile, OldURI: "file:///work/b.go", NewURI: "file:///work/d.go"},
		{Kind: lsp.ChangeDeleteFile, URI: "file:///work/c.go"},
	}})
	res, err := Apply(context.Background(), h, we)
	if err != nil || !res.Applied {
		t.Fatalf("Apply = %+v, %v", res, err)
	}
	if d.Text() != "x😀z = 1\n" {
		t.Errorf("a.go = %q", d.Text())
	}
	b, _ := h.Document(document.FileURI("/work/b.go"))
	if b == nil || b.Text() != "x😀Z\n" {
		t.Errorf("b.go = %v", b)
	}
	wantLog := []string{"open /work/b.go", "create /work/c.go", "rename /work/b.go /work/d.go", "delete /work/c.go"}
	if !slices.Equal(h.log, wantLog) {
		t.Errorf("log = %v, want %v", h.log, wantLog)
	}
}

func TestApplyServerEditWithChangesAndStaleVersion(t *testing.T) {
	h := newHost()
	a := h.open("/work/a.go", "aaa\n")
	b := h.open("/work/b.go", "bbb\n")
	we := FromLSPEdit(lsp.UTF8, lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{
		"file:///work/b.go": {{Range: lsp.Range{End: lsp.Position{Character: 1}}, NewText: "B"}},
		"file:///work/a.go": {{Range: lsp.Range{End: lsp.Position{Character: 1}}, NewText: "A"}},
	}})
	res, err := Apply(context.Background(), h, we)
	if err != nil || len(res.Documents) != 2 || res.Documents[0] != a || res.Documents[1] != b {
		t.Fatalf("Apply = %+v, %v (the documents are edited in the order of their URIs)", res, err)
	}
	if a.Text() != "Aaa\n" || b.Text() != "Bbb\n" {
		t.Errorf("texts = %q %q", a.Text(), b.Text())
	}
	old := 0
	stale := FromLSPEdit(lsp.UTF8, lsp.WorkspaceEdit{DocumentChanges: []lsp.DocumentChange{
		{TextDocument: &lsp.OptionalVersionedTextDocumentIdentifier{URI: "file:///work/a.go", Version: &old}, Edits: []lsp.TextEdit{{NewText: "!"}}},
	}})
	if _, err := Apply(context.Background(), h, stale); err == nil {
		t.Error("an edit for an older version was applied")
	} else {
		routeOf(t, err, "applyRun.WorkspaceEditStale")
	}
}

func TestApplyServerEditErrors(t *testing.T) {
	tests := []struct {
		name  string
		edit  lsp.WorkspaceEdit
		route string
	}{
		{"a URI the editor has no documents for", lsp.WorkspaceEdit{Changes: map[string][]lsp.TextEdit{"jar:file:///x.jar!/A.class": nil}}, "planServerEdit->ParseDocumentURI.DocumentURIUnsupported"},
		{"a document change with no document", lsp.WorkspaceEdit{DocumentChanges: []lsp.DocumentChange{{}}}, "planServerEdit.WorkspaceEditInvalid"},
		{"a bad URI in a file operation", lsp.WorkspaceEdit{DocumentChanges: []lsp.DocumentChange{{Kind: lsp.ChangeDeleteFile, URI: "file://host/x"}}}, "DocumentURIInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Apply(context.Background(), newHost(), FromLSPEdit(lsp.UTF16, tt.edit))
			routeOf(t, err, tt.route)
		})
	}
}
