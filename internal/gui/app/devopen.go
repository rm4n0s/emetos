package app

import (
	"context"
	"path/filepath"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/edit"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/fileio"
)

// openFileArguments are the arguments of dev.openFile.
type openFileArguments struct {
	// Path is the file; a relative path is relative to the folder Emetos runs in.
	Path string
	// Line and Column are the position to put the cursor at, counted from 1; 0 leaves the
	// cursor at the start.
	Line, Column int
	// Group is the place of the group to open the file in, counted from 1 in the order the groups
	// appear in the grid; 0 is the active group.
	Group int
	// Preview opens the file as the preview editor (italic tab, replaced by the next preview), as a
	// navigation does, when workbench.editor.enablePreview is on.
	Preview bool
}

// parseOpenFile reads the arguments of dev.openFile: {"path": "...", "line": n, "column": n,
// "group": n, "preview": bool}, or just the path as a string.
func parseOpenFile(args any) (openFileArguments, error) {
	var a openFileArguments
	switch v := args.(type) {
	case string:
		a.Path = v
	case map[string]any:
		a.Path, _ = v["path"].(string)
		if line, ok := v["line"].(float64); ok {
			a.Line = int(line)
		}
		if col, ok := v["column"].(float64); ok {
			a.Column = int(col)
		}
		if raw, present := v["preview"]; present {
			preview, ok := raw.(bool)
			if !ok {
				return a, errors.New("DevOpenFilePreviewInvalid", "the preview of dev.openFile is true or false", "preview", raw)
			}
			a.Preview = preview
		}
		if raw, present := v["group"]; present {
			group, ok := raw.(float64)
			if !ok || group != float64(int(group)) || group < 1 {
				return a, errors.New("DevOpenFileInvalid", "the group of dev.openFile is a whole number counted from 1", "group", raw)
			}
			a.Group = int(group)
		}
	}
	if a.Path == "" {
		return a, errors.New("DevOpenFileInvalid", "dev.openFile needs a path or {\"path\": ..., \"line\": ..., \"column\": ...}")
	}
	if a.Line < 0 || a.Column < 0 {
		return a, errors.New("DevOpenFileInvalid", "the line and the column of dev.openFile count from 1", "line", a.Line, "column", a.Column)
	}
	return a, nil
}

// bindDevOpenFile gives dev.openFile (SPEC §15.2) its handler: the window shows the file in its
// editor, with the cursor at the position.
func (s *shellState) bindDevOpenFile(binding *widgets.WidgetsBinding) {
	if !DevCommandsEnabled() {
		return
	}
	win := PeekWindow(s.Context())
	remove, err := win.Process.Commands.SetHandler("dev.openFile", func(c command.Context, args any) error {
		return s.devOpenFile(binding, c.Ctx, args)
	})
	if err != nil {
		win.Process.Logger.Error("cannot bind dev.openFile", "err", err)
		return
	}
	s.removeOpenFile = remove
}

// devOpenFile opens a file in a editor group (the active group, or the one the group argument says),
// kept (as a preview when the argument says so), and shows it with the cursor at the position. The group becomes the active
// group. A group that is not in the grid fails with the tag DevOpenFileGroupInvalid.
func (s *shellState) devOpenFile(binding *widgets.WidgetsBinding, ctx context.Context, args any) error {
	a, err := parseOpenFile(args)
	if err != nil {
		return err
	}
	var group *editorgroup.Group
	if a.Group > 0 {
		if group = s.groupAt(a.Group); group == nil {
			return errors.New("DevOpenFileGroupInvalid", "dev.openFile names a group that does not exist", "group", a.Group, "groups", s.group.grid.Count())
		}
	}
	path, absErr := filepath.Abs(a.Path)
	if absErr != nil {
		return errors.NewErr("DevOpenFileFailed", absErr).SetMetadata("path", a.Path)
	}
	doc, err := s.openFile(ctx, path)
	if err != nil {
		return err
	}
	var place func(*textEditor)
	if a.Line > 0 {
		place = func(e *textEditor) {
			model := edit.NewModel(doc.Snapshot())
			at := model.ValidatePosition(buffer.Position{Line: a.Line - 1, Col: max(a.Column-1, 0)})
			e.view.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(at)})
		}
	}
	s.show(binding, doc, openOptions{Place: place, Group: group, Preview: a.Preview})
	return nil
}

// openPathIn opens a file that the user chose (in the breadcrumbs picker of a group) and shows it as a
// preview in the group (the active group when nil; a group that is gone by the time the file is read is
// replaced by the active group). The file is read in the background; a file that cannot be opened is
// reported.
func (s *shellState) openPathIn(binding *widgets.WidgetsBinding, group *editorgroup.Group, path string) {
	s.openPath(binding, group, path, true)
}

// openPath is openPathIn for a file shown as a preview editor or, with preview false, kept (a double click in
// the explorer).
func (s *shellState) openPath(binding *widgets.WidgetsBinding, group *editorgroup.Group, path string, preview bool) {
	s.openPathAt(binding, path, 0, 0, openOptions{Preview: preview, Group: group})
}

// openPathAt reads a file in the background and shows it as opts say (Place is made here from a line and a
// column, counted from 1; 0 leaves the cursor where it is). A group that is gone by the time the file is
// read is replaced by the active group. A file that cannot be opened is reported.
func (s *shellState) openPathAt(binding *widgets.WidgetsBinding, path string, line, column int, opts openOptions) {
	win := PeekWindow(s.Context())
	s.Go(func(life context.Context) {
		doc, err := win.Documents.Acquire(life, path, document.NewOptions())
		binding.Post(func() {
			if !s.Mounted() {
				if err == nil {
					win.Documents.Release(doc)
				}
				return
			}
			if err != nil {
				if isBinaryError(err) {
					s.blockFile(path)
					return
				}
				win.Process.Notify.Error("Emetos", "Cannot open "+path+": "+describe(err))
				return
			}
			if opts.Group != nil && s.group.grid.Group(opts.Group.ID()) != opts.Group {
				opts.Group = nil
			}
			if line > 0 {
				opts.Place = func(e *textEditor) {
					model := edit.NewModel(doc.Snapshot())
					at := model.ValidatePosition(buffer.Position{Line: line - 1, Col: max(column-1, 0)})
					e.view.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(at)})
				}
			}
			s.show(binding, doc, opts)
		})
	})
}

// blockFile tells that a file looks binary: with no editor open the window shows the notice, which has a
// button to open the file anyway; otherwise the open editors stay and a notification says so.
func (s *shellState) blockFile(path string) {
	if s.editorCount() > 0 {
		PeekWindow(s.Context()).Process.Notify.Warn("Emetos", path+": "+BinaryFileNotice)
		return
	}
	s.SetState(func() { s.blocked = path })
}

// openAnyway opens a file that looks binary, as the button of its notice asks.
func (s *shellState) openAnyway(binding *widgets.WidgetsBinding, path string) {
	win := PeekWindow(s.Context())
	s.Go(func(life context.Context) {
		doc, err := win.Documents.AcquireWith(life, path, document.NewOptions(), fileio.ReadOptions{AllowBinary: true})
		binding.Post(func() {
			if !s.Mounted() {
				if err == nil {
					win.Documents.Release(doc)
				}
				return
			}
			if err != nil {
				win.Process.Notify.Error("Emetos", "Cannot open "+path+": "+describe(err))
				return
			}
			s.show(binding, doc, openOptions{})
		})
	})
}
