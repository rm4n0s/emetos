package app

import (
	"context"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
)

// The window's side of the language servers (step 12.3): the edits a server or a provider makes go through the documents and
// the file operations of the window.

func TestAWorkspaceEditChangesOpenAndClosedFilesAndFiles(t *testing.T) {
	a := newFileApp(t, projectFiles, "main.go")
	ctx := context.Background()
	main, ok := a.win.Documents.Get(document.FileURI(a.path("main.go")))
	if !ok {
		t.Fatal("main.go is not open")
	}
	before := a.read("go.mod")
	edit := &langfeatures.WorkspaceEdit{Changes: []langfeatures.Change{
		{Kind: langfeatures.ChangeText, URI: document.FileURI(a.path("main.go")), Edits: []langfeatures.TextEdit{{Range: buffer.NewRange(buffer.Position{}, buffer.Position{}), Text: "// edited\n"}}},
		{Kind: langfeatures.ChangeText, URI: document.FileURI(a.path("go.mod")), Edits: []langfeatures.TextEdit{{Range: buffer.NewRange(buffer.Position{}, buffer.Position{}), Text: "// edited\n"}}},
	}}
	res, err := langfeatures.Apply(ctx, workspaceLSP{a.win}.Edits(), edit)
	a.pump()
	if err != nil || !res.Applied || len(res.Documents) != 2 {
		t.Fatalf("Apply = %+v, %v", res, err)
	}
	// the open file changed in its document, and the closed one was opened in an editor that is not the active one
	if main.Text()[:10] != "// edited\n" {
		t.Errorf("main.go = %q", main.Text())
	}
	closed, ok := a.win.Documents.Get(document.FileURI(a.path("go.mod")))
	if !ok || !closed.Dirty() || closed.Text()[:10] != "// edited\n" {
		t.Fatalf("go.mod = %v %v", ok, closed)
	}
	if got := a.editors(); !slices.Contains(got, "go.mod") {
		t.Errorf("editors = %v", got)
	}
	if active := a.group().Model.ActiveEditor(); active == nil || active.Name() != "main.go" {
		t.Errorf("the active editor changed")
	}
	if a.read("go.mod") != before {
		t.Error("the file on disk was written")
	}
	// one undo step each
	if _, err := closed.Undo(); err != nil || closed.CanUndo() || closed.Dirty() {
		t.Errorf("undo of go.mod: %v can undo %v dirty %v", err, closed.CanUndo(), closed.Dirty())
	}

	// file operations: a file in a folder that is not there yet, a rename in the folder, a move to another, and the trash
	e := workspaceLSP{a.win}.Edits()
	file := func(rel string) document.URI { return document.FileURI(a.path(rel)) }
	steps := []langfeatures.Change{
		{Kind: langfeatures.ChangeCreate, URI: file("gen/deep/x.go")},
		{Kind: langfeatures.ChangeRename, URI: file("gen/deep/x.go"), NewURI: file("gen/deep/y.go")},
		{Kind: langfeatures.ChangeRename, URI: file("gen/deep/y.go"), NewURI: file("other/z.go")},
		{Kind: langfeatures.ChangeCreate, URI: file("other/z.go"), IgnoreIfExists: true},
		{Kind: langfeatures.ChangeDelete, URI: file("gen/deep"), Recursive: true},
		{Kind: langfeatures.ChangeDelete, URI: file("nothing.go"), IgnoreIfNotExists: true},
	}
	res, err = langfeatures.Apply(ctx, e, &langfeatures.WorkspaceEdit{Changes: steps})
	a.pump()
	if err != nil || !res.Applied {
		t.Fatalf("file operations: %+v, %v", res, err)
	}
	if !a.exists("other/z.go") || a.exists("gen/deep/y.go") || a.exists("gen/deep") {
		t.Errorf("files: z %v y %v deep %v", a.exists("other/z.go"), a.exists("gen/deep/y.go"), a.exists("gen/deep"))
	}
	if !slices.Contains(a.trashed(), "deep") {
		t.Errorf("trash = %v", a.trashed())
	}
	// a file in the way is an error, and Overwrite replaces it (the old one goes to the trash)
	_, err = langfeatures.Apply(ctx, e, &langfeatures.WorkspaceEdit{Changes: []langfeatures.Change{{Kind: langfeatures.ChangeCreate, URI: file("other/z.go")}}})
	if err == nil {
		t.Error("a file was created over an existing one")
	}
	if _, err = langfeatures.Apply(ctx, e, &langfeatures.WorkspaceEdit{Changes: []langfeatures.Change{{Kind: langfeatures.ChangeCreate, URI: file("other/z.go"), Overwrite: true}}}); err != nil {
		t.Errorf("overwrite: %v", err)
	}
	// renaming over a file: refused, and with IgnoreIfExists left alone
	_, err = langfeatures.Apply(ctx, e, &langfeatures.WorkspaceEdit{Changes: []langfeatures.Change{{Kind: langfeatures.ChangeRename, URI: file("other/z.go"), NewURI: file("main.go")}}})
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("workspaceEdits.RenameFile.LanguageEditFileExists") {
		t.Errorf("a file was renamed over another: %v", err)
	}
	if !a.exists("other/z.go") {
		t.Error("the refused rename moved the file")
	}
	if _, err = langfeatures.Apply(ctx, e, &langfeatures.WorkspaceEdit{Changes: []langfeatures.Change{{Kind: langfeatures.ChangeRename, URI: file("other/z.go"), NewURI: file("main.go"), IgnoreIfExists: true}}}); err != nil || !a.exists("other/z.go") {
		t.Errorf("rename with IgnoreIfExists: %v", err)
	}
	// a file that is deleted and was not there is an error
	_, err = langfeatures.Apply(ctx, e, &langfeatures.WorkspaceEdit{Changes: []langfeatures.Change{{Kind: langfeatures.ChangeDelete, URI: file("nothing.go")}}})
	if err == nil {
		t.Error("a file that is not there was deleted")
	}
}

func TestTheTextOfOtherDocumentsForTheServers(t *testing.T) {
	a := newFileApp(t, projectFiles, "main.go")
	l := workspaceLSP{a.win}
	if lines, ok := l.OpenText(document.FileURI(a.path("main.go"))); !ok || lines.LineCount() < 1 {
		t.Errorf("open text = %v %v", lines, ok)
	}
	if _, ok := l.OpenText(document.FileURI(a.path("go.mod"))); ok {
		t.Error("a closed file has open text")
	}
	lines, err := l.ReadText(context.Background(), document.FileURI(a.path("go.mod")))
	if err != nil || lines.LineCount() < 1 {
		t.Errorf("read text = %v, %v", lines, err)
	}
	_, err = l.ReadText(context.Background(), document.UntitledURI(1))
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("workspaceLSP.ReadText.LanguageTextUnavailable") {
		t.Errorf("untitled: %v", err)
	}
	if got := l.Documents(); len(got) != 1 || got[0].URI().Path != filepath.Join(a.root, "main.go") {
		t.Errorf("documents = %v", got)
	}
	var opened []string
	stop := l.WatchOpen(func(d *document.Document) { opened = append(opened, d.URI().Path) })
	if _, err := a.win.Documents.OpenFile(context.Background(), a.path("go.mod"), document.NewOptions()); err != nil {
		t.Fatal(err)
	}
	stop()
	if !slices.Equal(opened, []string{a.path("go.mod")}) {
		t.Errorf("opened = %v", opened)
	}
}
