package app

import (
	"context"
	"os"
	"path/filepath"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/core/workspace"
)

// The window's side of the language servers of its extensions (SPEC §9.12 "As built (step 12.3)"): the documents they follow,
// the events of the file watcher, the text that the positions of an answer refer to, and the application of workspace edits
// through the documents and the file operations of the window.

// workspaceLSP is the exthost.LanguageWorkspace of a workspace.
type workspaceLSP struct{ w *Workspace }

// Documents implements exthost.LanguageWorkspace.
func (l workspaceLSP) Documents() []*document.Document { return l.w.Documents.All() }

// WatchOpen implements exthost.LanguageWorkspace.
func (l workspaceLSP) WatchOpen(fn func(*document.Document)) (stop func()) {
	return l.w.Documents.WatchOpen(fn)
}

// WatchClose implements exthost.LanguageWorkspace.
func (l workspaceLSP) WatchClose(fn func(*document.Document)) (stop func()) {
	return l.w.Documents.WatchClose(fn)
}

// WatchFiles implements exthost.LanguageWorkspace.
func (l workspaceLSP) WatchFiles(fn func([]workspace.Event)) (stop func()) {
	return l.w.OnFileEvents(fn)
}

// OpenText implements exthost.LanguageWorkspace.
func (l workspaceLSP) OpenText(uri document.URI) (lsp.Lines, bool) {
	doc, ok := l.w.Documents.Get(uri)
	if !ok {
		return nil, false
	}
	return doc.Snapshot(), true
}

// ReadText implements exthost.LanguageWorkspace. It fails with the tag LanguageTextUnavailable for a document that is not a
// file; the tags of fileio.Read otherwise.
func (l workspaceLSP) ReadText(ctx context.Context, uri document.URI) (lsp.Lines, error) {
	if uri.Scheme != document.SchemeFile {
		return nil, errors.New("LanguageTextUnavailable", "only the text of a file can be read from disk", "uri", uri.String())
	}
	res, err := fileio.Read(ctx, uri.Path, fileio.ReadOptions{})
	if err != nil {
		return nil, err
	}
	return buffer.New(res.Text).Snapshot(), nil
}

// Edits implements exthost.LanguageWorkspace.
func (l workspaceLSP) Edits() langfeatures.EditHost { return workspaceEdits(l) }

// workspaceEdits is the langfeatures.EditHost of a workspace. Its methods belong to the UI thread.
type workspaceEdits struct{ w *Workspace }

// BeginEdit implements langfeatures.EditTracker: the cursors of the editors follow the text while the edit is made, and the
// ones of the editors that show a document the edit changed are kept where they were relative to the text.
func (e workspaceEdits) BeginEdit() (end func()) {
	s := e.w.liveShell()
	if s == nil {
		return func() {}
	}
	var restore []func()
	for _, ed := range s.group.editors {
		if ed.view != nil {
			restore = append(restore, ed.view.TrackCursors())
		}
	}
	return func() {
		for _, r := range restore {
			r()
		}
	}
}

// Document implements langfeatures.EditHost.
func (e workspaceEdits) Document(uri document.URI) (*document.Document, bool) {
	return e.w.Documents.Get(uri)
}

// OpenForEdit implements langfeatures.EditHost: the file is read into a document of the window and shown in an inactive
// editor of the active group, as VS Code does for the files a refactoring touches; a window that is not on screen keeps the
// document open without an editor.
func (e workspaceEdits) OpenForEdit(ctx context.Context, path string) (*document.Document, error) {
	w := e.w
	s := w.shell
	if s == nil || !s.Mounted() {
		return w.Documents.OpenFile(ctx, path, document.NewOptions())
	}
	doc, err := w.Documents.Acquire(ctx, path, document.NewOptions())
	if err != nil {
		return nil, err
	}
	s.show(widgets.WidgetsBindingOf(s.Context()), doc, openOptions{Inactive: true, NoMergeNotice: true})
	return doc, nil
}

// pathExists tells whether something is at the path.
func pathExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// afterFileOp makes the tree and the editors follow an operation of the file operations of the window.
func (e workspaceEdits) afterFileOp(ctx context.Context, res workspace.OpResult) {
	w := e.w
	if res.Empty() {
		return
	}
	if ws := w.currentWorkspace(); ws != nil {
		ws.Apply(ctx, res.Events())
	}
	if s := w.shell; s != nil && s.Mounted() {
		s.followFileOp(widgets.WidgetsBindingOf(s.Context()), res)
	}
}

// existingAncestor is the closest folder of the path that exists, and the path below it.
func existingAncestor(path string) (parent, rel string) {
	parent = filepath.Dir(path)
	for !pathExists(parent) && parent != filepath.Dir(parent) {
		parent = filepath.Dir(parent)
	}
	rel, err := filepath.Rel(parent, path)
	if err != nil {
		return filepath.Dir(path), filepath.Base(path)
	}
	return parent, rel
}

// CreateFile implements langfeatures.EditHost: an empty file, with the folders above it. A file that exists is left alone
// with IgnoreIfExists, replaced with Overwrite (the old one goes to the trash), and is an error otherwise.
func (e workspaceEdits) CreateFile(ctx context.Context, path string, opts langfeatures.FileOptions) error {
	if pathExists(path) {
		switch {
		case opts.IgnoreIfExists && !opts.Overwrite:
			return nil
		case opts.Overwrite:
			res, err := e.w.FileOps.Trash(ctx, []string{path})
			e.afterFileOp(ctx, res)
			if err != nil {
				return err
			}
		}
	}
	parent, rel := existingAncestor(path)
	res, err := e.w.FileOps.CreateFile(ctx, parent, filepath.ToSlash(rel))
	e.afterFileOp(ctx, res)
	return err
}

// RenameFile implements langfeatures.EditHost: in the same folder it is a rename of the explorer, elsewhere a move (to a
// folder that is made when missing) and, when the name changes too, a rename after it.
func (e workspaceEdits) RenameFile(ctx context.Context, from, to string, opts langfeatures.FileOptions) error {
	ops := e.w.FileOps
	if from == to {
		return nil
	}
	if pathExists(to) {
		switch {
		case opts.IgnoreIfExists && !opts.Overwrite:
			return nil
		case opts.Overwrite:
			res, err := ops.Trash(ctx, []string{to}) // the replaced file goes to the trash, so that nothing is lost
			e.afterFileOp(ctx, res)
			if err != nil {
				return err
			}
		default:
			return errors.New("LanguageEditFileExists", "a file of this name exists already", "path", to)
		}
	}
	if filepath.Dir(from) == filepath.Dir(to) {
		res, err := ops.Rename(ctx, from, filepath.Base(to))
		e.afterFileOp(ctx, res)
		return err
	}
	dstDir := filepath.Dir(to)
	if !pathExists(dstDir) {
		parent, rel := existingAncestor(to)
		rel = filepath.ToSlash(filepath.Dir(rel))
		res, err := ops.CreateFolder(ctx, parent, rel)
		e.afterFileOp(ctx, res)
		if err != nil {
			return err
		}
	}
	res, err := ops.Move(ctx, dstDir, []string{from}, workspace.MoveOptions{})
	e.afterFileOp(ctx, res)
	if err != nil {
		return err
	}
	if moved := filepath.Join(dstDir, filepath.Base(from)); moved != to {
		res, err = ops.Rename(ctx, moved, filepath.Base(to))
		e.afterFileOp(ctx, res)
		return err
	}
	return nil
}

// DeleteFile implements langfeatures.EditHost: the file or folder goes to the trash. A path that is not there is fine with
// IgnoreIfNotExists and an error otherwise.
func (e workspaceEdits) DeleteFile(ctx context.Context, path string, opts langfeatures.FileOptions) error {
	if !pathExists(path) && opts.IgnoreIfNotExists {
		return nil
	}
	res, err := e.w.FileOps.Trash(ctx, []string{path})
	e.afterFileOp(ctx, res)
	return err
}
