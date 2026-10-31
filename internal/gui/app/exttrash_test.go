package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/errors"
)

// The trash an extension uses (Workspace().TrashFiles, step 24.8) is the one of the file operations of the window, isolated by the
// test process.

func TestAnExtensionMovesFilesToTheTrashAndTakesThemBack(t *testing.T) {
	p := extensionProcess(t)
	w := newExtensionWorkspace(t, p, "", "a.txt", "b.txt", "keep.txt")
	ctx := context.Background()
	a, b := filepath.Join(w.Folder, "a.txt"), filepath.Join(w.Folder, "b.txt")
	undo, err := w.extensionTrash(ctx, []string{a, b})
	if err != nil || undo == nil {
		t.Fatalf("%v, undo %v", err, undo != nil)
	}
	for _, gone := range []string{a, b} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("%s is still there (%v)", gone, err)
		}
	}
	if _, err := os.Stat(filepath.Join(w.Folder, "keep.txt")); err != nil {
		t.Errorf("keep.txt: %v", err)
	}
	// the trash is the isolated one of the test
	trash := filepath.Join(os.Getenv("XDG_DATA_HOME"), "Trash", "files")
	if entries, err := os.ReadDir(trash); err != nil || len(entries) != 2 {
		t.Errorf("the isolated trash %s has %d entries (%v)", trash, len(entries), err)
	}
	// it is one operation of the history of the Explorer too, which something done after it does not hide from the function
	if _, err := w.FileOps.CreateFile(ctx, w.Folder, "later.txt"); err != nil {
		t.Fatal(err)
	}
	if err := undo(ctx); err != nil {
		t.Fatal(err)
	}
	for _, back := range []string{a, b} {
		if _, err := os.Stat(back); err != nil {
			t.Errorf("%s did not come back: %v", back, err)
		}
	}
	if label, _, ok := w.FileOps.NextUndo(); !ok || label != "Create later.txt" {
		t.Errorf("the next undo of the Explorer = %q, %v", label, ok)
	}
	// a second call finds the operation gone
	err = undo(ctx)
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("FileOps.undoRecord.FileOpNothingToUndo") {
		t.Errorf("second undo: %v", err)
	}
}

func TestAnExtensionTrashThatFailsKeepsWhatItMoved(t *testing.T) {
	p := extensionProcess(t)
	w := newExtensionWorkspace(t, p, "", "a.txt")
	a := filepath.Join(w.Folder, "a.txt")
	undo, err := w.extensionTrash(context.Background(), []string{a, filepath.Join(w.Folder, "missing.txt")})
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("HomeTrash.Trash.TrashSourceMissing") {
		t.Fatalf("%v", err)
	}
	if undo != nil {
		t.Error("an undo for a failed operation")
	}
	if _, err := os.Stat(a); !os.IsNotExist(err) {
		t.Errorf("the file moved before the failure is still there: %v", err)
	}
}

func TestAWindowWithoutFileOperationsHasNoTrashForExtensions(t *testing.T) {
	w := &Workspace{}
	undo, err := w.extensionTrash(context.Background(), []string{"/a"})
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("Workspace.extensionTrash.ExtTrashUnavailable") || undo != nil {
		t.Errorf("%v", err)
	}
}
