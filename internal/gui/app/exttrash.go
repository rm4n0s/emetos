package app

import (
	"context"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/workspace"
)

// extensionTrash is the TrashFiles of the scope (Workspace().TrashFiles, SPEC §10.30 "Discard"): the files go to the trash of the file
// operations of the window as one operation, the tree of the Explorer and the editors of the files follow as they do for a deletion
// in the Explorer, and the function it returns takes exactly that operation back. It runs on the goroutine of the extension.
func (w *Workspace) extensionTrash(ctx context.Context, paths []string) (func(context.Context) error, error) {
	ops := w.FileOps
	if ops == nil {
		return nil, errors.New("ExtTrashUnavailable", "the window has no file operations")
	}
	res, undo, err := ops.TrashUndoable(ctx, paths)
	w.afterExtensionFileOp(ctx, res)
	if err != nil || undo == nil {
		return nil, err
	}
	return func(ctx context.Context) error {
		res, err := undo(ctx)
		w.afterExtensionFileOp(ctx, res)
		return err
	}, nil
}

// afterExtensionFileOp makes the tree and the editors follow an operation of an extension (the one code path of workspaceEdits).
func (w *Workspace) afterExtensionFileOp(ctx context.Context, res workspace.OpResult) {
	if res.Empty() {
		return
	}
	if ws := w.currentWorkspace(); ws != nil {
		ws.Apply(ctx, res.Events())
	}
	w.onUI(func() {
		if s := w.shell; s != nil && s.Mounted() {
			s.followFileOp(widgets.WidgetsBindingOf(s.Context()), res)
		}
	})
}
