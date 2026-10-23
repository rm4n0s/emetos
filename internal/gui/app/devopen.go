package app

import (
	"context"
	"path/filepath"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/edit"
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

// devOpenFile opens a file and shows it, replacing the editor of the window.
func (s *shellState) devOpenFile(binding *widgets.WidgetsBinding, ctx context.Context, args any) error {
	a, err := parseOpenFile(args)
	if err != nil {
		return err
	}
	path, absErr := filepath.Abs(a.Path)
	if absErr != nil {
		return errors.NewErr("DevOpenFileFailed", absErr).SetMetadata("path", a.Path)
	}
	win := PeekWindow(s.Context())
	doc, err := win.Documents.OpenFile(ctx, path, document.NewOptions())
	if err != nil {
		return err
	}
	s.SetState(func() {
		s.disposeEditor()
		s.initEditor(binding, doc)
		if a.Line > 0 {
			model := edit.NewModel(doc.Snapshot())
			at := model.ValidatePosition(buffer.Position{Line: a.Line - 1, Col: max(a.Column-1, 0)})
			s.view.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(at)})
		}
	})
	binding.Scheduler().AddPostFrameCallback(func(_ time.Duration) { s.view.RevealPrimary() })
	return nil
}
