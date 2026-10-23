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
	"github.com/rm4n0s/emetos/internal/core/fileio"
)

// openFileArguments are the arguments of dev.openFile.
type openFileArguments struct {
	// Path is the file; a relative path is relative to the folder Emetos runs in.
	Path string
	// Line and Column are the position to put the cursor at, counted from 1; 0 leaves the
	// cursor at the start.
	Line, Column int
}

// parseOpenFile reads the arguments of dev.openFile: {"path": "...", "line": n, "column": n}, or
// just the path as a string.
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

// devOpenFile opens a file in the editor group, kept (not as a preview), and shows it with the cursor at
// the position.
func (s *shellState) devOpenFile(binding *widgets.WidgetsBinding, ctx context.Context, args any) error {
	a, err := parseOpenFile(args)
	if err != nil {
		return err
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
	s.show(binding, doc, openOptions{Place: place})
	return nil
}

// openPath opens a file that the user chose (in a breadcrumbs picker) and shows it as a preview. The
// file is read in the background; a file that cannot be opened is reported.
func (s *shellState) openPath(binding *widgets.WidgetsBinding, path string) {
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
			s.show(binding, doc, openOptions{Preview: true})
		})
	})
}

// blockFile tells that a file looks binary: with no editor open the window shows the notice, which has a
// button to open the file anyway; otherwise the open editors stay and a notification says so.
func (s *shellState) blockFile(path string) {
	if s.group.model.Count() > 0 {
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
