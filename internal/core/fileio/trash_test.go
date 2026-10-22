package fileio

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

var trashTime = time.Date(2026, 10, 23, 14, 5, 9, 0, time.Local)

// newTrash makes a trash in a folder of its own and a folder to trash files from.
func newTrash(t *testing.T) (trash *HomeTrash, trashDir, work string) {
	t.Helper()
	trashDir = filepath.Join(t.TempDir(), "Trash")
	work = t.TempDir()
	trash = NewHomeTrash(trashDir)
	trash.Now = func() time.Time { return trashTime }
	return trash, trashDir, work
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func TestTrashAndRestoreAFile(t *testing.T) {
	trash, dir, work := newTrash(t)
	path := filepath.Join(work, "note.txt")
	writeFile(t, path, "hello")

	item, err := trash.Trash(context.Background(), path)
	if err != nil {
		t.Fatalf("Trash: %v", err)
	}
	if item.Name != "note.txt" || item.OriginalPath != path || !item.DeletedAt.Equal(trashTime) {
		t.Errorf("item = %+v", item)
	}
	if exists(path) {
		t.Error("the file is still where it was")
	}
	if got := string(readBytes(t, filepath.Join(dir, "files", "note.txt"))); got != "hello" {
		t.Errorf("trashed content = %q", got)
	}
	want := "[Trash Info]\nPath=" + path + "\nDeletionDate=2026-10-23T14:05:09\n"
	if got := string(readBytes(t, filepath.Join(dir, "info", "note.txt.trashinfo"))); got != want {
		t.Errorf("trashinfo = %q, want %q", got, want)
	}
	for _, sub := range []string{"", "files", "info"} {
		if info, err := os.Stat(filepath.Join(dir, sub)); err != nil || info.Mode().Perm() != 0o700 {
			t.Errorf("trash folder %q: %v %v", sub, info, err)
		}
	}

	if err := trash.Restore(context.Background(), item); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got := string(readBytes(t, path)); got != "hello" {
		t.Errorf("restored content = %q", got)
	}
	if exists(filepath.Join(dir, "files", "note.txt")) || exists(filepath.Join(dir, "info", "note.txt.trashinfo")) {
		t.Error("the trash still holds the restored item")
	}
}

func TestTrashEncodesThePath(t *testing.T) {
	trash, dir, work := newTrash(t)
	path := filepath.Join(work, "my file (1)é#%.txt")
	writeFile(t, path, "x")
	item, err := trash.Trash(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	info := string(readBytes(t, filepath.Join(dir, "info", item.Name+".trashinfo")))
	wantPath := work + "/my%20file%20(1)%C3%A9%23%25.txt"
	if want := "[Trash Info]\nPath=" + wantPath + "\n"; len(info) < len(want) || info[:len(want)] != want {
		t.Errorf("trashinfo = %q, want it to start with %q", info, want)
	}
}

func TestTrashNameClashes(t *testing.T) {
	trash, dir, work := newTrash(t)
	path := filepath.Join(work, "a.txt")
	var items []TrashedItem
	for i, content := range []string{"first", "second", "third"} {
		writeFile(t, path, content)
		item, err := trash.Trash(context.Background(), path)
		if err != nil {
			t.Fatalf("Trash %d: %v", i, err)
		}
		items = append(items, item)
	}
	if items[0].Name != "a.txt" || items[1].Name != "a.txt.2" || items[2].Name != "a.txt.3" {
		t.Errorf("names = %q %q %q", items[0].Name, items[1].Name, items[2].Name)
	}
	for _, it := range items {
		if !exists(filepath.Join(dir, "files", it.Name)) || !exists(filepath.Join(dir, "info", it.Name+".trashinfo")) {
			t.Errorf("%s is incomplete in the trash", it.Name)
		}
	}
	// A name taken only by a .trashinfo is not reused either.
	writeFile(t, path, "fourth")
	writeFile(t, filepath.Join(dir, "info", "a.txt.4.trashinfo"), "x")
	item, err := trash.Trash(context.Background(), path)
	if err != nil || item.Name != "a.txt.5" {
		t.Errorf("Trash = %+v, %v; want a.txt.5", item, err)
	}
	// Restoring the second leaves the others; the first can't come back over it.
	if err := trash.Restore(context.Background(), items[1]); err != nil {
		t.Fatalf("Restore second: %v", err)
	}
	if got := string(readBytes(t, path)); got != "second" {
		t.Errorf("restored = %q", got)
	}
	err = trash.Restore(context.Background(), items[0])
	wantTag(t, err, "Restore", "RestoreTargetExists")
	if !exists(filepath.Join(dir, "files", "a.txt")) {
		t.Error("a refused restore removed the item from the trash")
	}
}

func TestTrashAFolder(t *testing.T) {
	trash, dir, work := newTrash(t)
	folder := filepath.Join(work, "project")
	writeFile(t, filepath.Join(folder, "src", "main.go"), "package main")
	writeFile(t, filepath.Join(folder, "README"), "hi")
	item, err := trash.Trash(context.Background(), folder)
	if err != nil {
		t.Fatal(err)
	}
	if exists(folder) || !exists(filepath.Join(dir, "files", "project", "src", "main.go")) {
		t.Error("the folder did not move whole")
	}
	if err := trash.Restore(context.Background(), item); err != nil {
		t.Fatal(err)
	}
	if got := string(readBytes(t, filepath.Join(folder, "src", "main.go"))); got != "package main" {
		t.Errorf("restored = %q", got)
	}
}

func TestTrashALink(t *testing.T) {
	trash, dir, work := newTrash(t)
	target := filepath.Join(work, "target")
	link := filepath.Join(work, "link")
	writeFile(t, target, "t")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := trash.Trash(context.Background(), link); err != nil {
		t.Fatal(err)
	}
	if !exists(target) {
		t.Error("trashing a link removed its target")
	}
	if info, err := os.Lstat(filepath.Join(dir, "files", "link")); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link in the trash is %v, %v", info, err)
	}
}

func TestRestoreRecreatesMissingFolders(t *testing.T) {
	trash, _, work := newTrash(t)
	path := filepath.Join(work, "a", "b", "f")
	writeFile(t, path, "x")
	item, err := trash.Trash(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(work, "a")); err != nil {
		t.Fatal(err)
	}
	if err := trash.Restore(context.Background(), item); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got := string(readBytes(t, path)); got != "x" {
		t.Errorf("restored = %q", got)
	}
}

func TestTrashCrossDevice(t *testing.T) {
	work := t.TempDir()
	path := filepath.Join(work, "f")
	writeFile(t, path, "x")

	t.Run("different device ids", func(t *testing.T) {
		trash := NewHomeTrash(filepath.Join(t.TempDir(), "Trash"))
		trash.fs = &faultFS{devices: map[string]uint64{"f": 1, "Trash": 2}}
		_, err := trash.Trash(context.Background(), path)
		wantTag(t, err, "Trash", "TrashCrossDevice")
		if !exists(path) {
			t.Error("a refused trash moved the file")
		}
	})
	t.Run("rename says so", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "Trash")
		trash := NewHomeTrash(dir)
		trash.fs = &faultFS{renameErr: &os.LinkError{Op: "rename", Err: syscall.EXDEV}}
		_, err := trash.Trash(context.Background(), path)
		wantTag(t, err, "Trash", "TrashCrossDevice")
		if !exists(path) {
			t.Error("a failed trash moved the file")
		}
		if names, _ := os.ReadDir(filepath.Join(dir, "info")); len(names) != 0 {
			t.Errorf("a failed trash left %d trashinfo files", len(names))
		}
	})
}

func TestTrashFailures(t *testing.T) {
	work := t.TempDir()
	path := filepath.Join(work, "f")
	writeFile(t, path, "x")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	trash, _, _ := newTrash(t)
	for name, p := range map[string]string{"relative": "f", "unclean": work + "/../" + filepath.Base(work) + "/f", "root": "/", "empty": ""} {
		_, err := trash.Trash(context.Background(), p)
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		wantTag(t, err, "Trash", "TrashPathInvalid")
	}
	_, err := trash.Trash(context.Background(), filepath.Join(work, "missing"))
	wantTag(t, err, "Trash", "TrashSourceMissing")
	_, err = trash.Trash(cancelled, path)
	wantTag(t, err, "Trash", "TrashFailed")
	if !exists(path) {
		t.Error("a failed trash moved the file")
	}

	// A trash that can't be made: its folder is a file.
	blocked := filepath.Join(t.TempDir(), "Trash")
	writeFile(t, blocked, "in the way")
	_, err = NewHomeTrash(blocked).Trash(context.Background(), path)
	wantTag(t, err, "Trash", "TrashFailed")

	// A rename that fails for another reason.
	bad := NewHomeTrash(filepath.Join(t.TempDir(), "Trash"))
	bad.fs = &faultFS{renameErr: &os.LinkError{Op: "rename", Err: syscall.EIO}}
	_, err = bad.Trash(context.Background(), path)
	wantTag(t, err, "Trash", "TrashFailed")

	// A .trashinfo that can't be written.
	bad = NewHomeTrash(filepath.Join(t.TempDir(), "Trash"))
	bad.fs = &faultFS{openErr: errNoSpace}
	_, err = bad.Trash(context.Background(), path)
	wantTag(t, err, "Trash", "TrashFailed")
	bad.fs = &faultFS{writeErr: errNoSpace}
	_, err = bad.Trash(context.Background(), path)
	wantTag(t, err, "Trash", "TrashFailed")
	if names, _ := os.ReadDir(filepath.Join(bad.dir, "info")); len(names) != 0 {
		t.Errorf("a failed .trashinfo write left %d files", len(names))
	}
}

func TestRestoreFailures(t *testing.T) {
	trash, _, work := newTrash(t)
	path := filepath.Join(work, "f")
	writeFile(t, path, "x")
	item, err := trash.Trash(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for name, it := range map[string]TrashedItem{
		"never trashed":     {Name: "other", OriginalPath: path},
		"no name":           {OriginalPath: path},
		"name with a slash": {Name: "../escape", OriginalPath: path},
		"dot dot":           {Name: "..", OriginalPath: path},
	} {
		wantTag(t, trash.Restore(context.Background(), it), "Restore", "TrashItemMissing")
		_ = name
	}
	wantTag(t, trash.Restore(cancelled, item), "Restore", "RestoreFailed")
	wantTag(t, trash.Restore(context.Background(), TrashedItem{Name: item.Name, OriginalPath: "relative"}), "Restore", "RestoreFailed")

	// The original folder can't be made: a file is in its place.
	blocker := filepath.Join(work, "blocker")
	writeFile(t, blocker, "x")
	wantTag(t, trash.Restore(context.Background(), TrashedItem{Name: item.Name, OriginalPath: filepath.Join(blocker, "sub", "f")}), "Restore", "RestoreFailed")

	// The rename fails.
	trash.fs = &faultFS{renameErr: &os.LinkError{Op: "rename", Err: syscall.EIO}}
	wantTag(t, trash.Restore(context.Background(), item), "Restore", "RestoreFailed")
}

func TestDelete(t *testing.T) {
	work := t.TempDir()
	file := filepath.Join(work, "f")
	folder := filepath.Join(work, "d")
	writeFile(t, file, "x")
	writeFile(t, filepath.Join(folder, "sub", "g"), "y")
	for _, p := range []string{file, folder} {
		if err := Delete(context.Background(), p); err != nil {
			t.Errorf("Delete(%s): %v", p, err)
		}
		if exists(p) {
			t.Errorf("%s is still there", p)
		}
	}
	if exists(work) == false {
		t.Error("Delete removed the folder above")
	}
	wantTag(t, Delete(context.Background(), file), "Delete", "FileNotFound")
	for _, p := range []string{"", "relative", "/", work + "/../x"} {
		wantTag(t, Delete(context.Background(), p), "Delete", "DeletePathInvalid")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	writeFile(t, file, "x")
	wantTag(t, Delete(cancelled, file), "Delete", "DeleteFailed")
	if !exists(file) {
		t.Error("a cancelled delete removed the file")
	}
	// A path whose stat fails for another reason than a missing file.
	wantTag(t, deleteWith(context.Background(), osFS{}, "/"+"a\x00b"), "deleteWith", "DeleteFailed")
}
