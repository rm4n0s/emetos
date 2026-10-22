package fileio

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/rm4n0s/errors"
)

// trashInfoTime is the layout of the DeletionDate of a .trashinfo file.
const trashInfoTime = "2006-01-02T15:04:05"

// HomeTrash is the home trash of the freedesktop.org Trash specification
// (https://specifications.freedesktop.org/trash-spec/1.0/): a folder with
// "files" and "info" inside, where a trashed item is kept under a unique name
// together with a .trashinfo file that says where it came from. Only the home
// trash is used, so a file on another filesystem can't be trashed.
type HomeTrash struct {
	dir string
	// Now gives the time recorded in the .trashinfo file; time.Now when nil.
	Now func() time.Time

	fs fileSystem // replaced by tests
}

// NewHomeTrash makes the trash that lives in dir, normally Paths.TrashDir(). The
// folders are created when something is first trashed.
func NewHomeTrash(dir string) *HomeTrash {
	return &HomeTrash{dir: dir}
}

func (t *HomeTrash) fsys() fileSystem {
	if t.fs != nil {
		return t.fs
	}
	return osFS{}
}

func (t *HomeTrash) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

// TrashedItem is something in the trash.
type TrashedItem struct {
	// Name is the unique name in the trash's "files" folder.
	Name string
	// OriginalPath is where the item was.
	OriginalPath string
	// DeletedAt is when it was trashed.
	DeletedAt time.Time
}

func (t *HomeTrash) filesPath(name string) string { return filepath.Join(t.dir, "files", name) }
func (t *HomeTrash) infoPath(name string) string {
	return filepath.Join(t.dir, "info", name+".trashinfo")
}

// Trash moves a file or folder to the trash. The path must be absolute. It fails
// with the tags TrashPathInvalid, TrashSourceMissing, TrashCrossDevice (the item
// is not on the trash's filesystem, so the caller asks to delete it permanently)
// and TrashFailed.
func (t *HomeTrash) Trash(ctx context.Context, path string) (TrashedItem, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return TrashedItem{}, errors.New("TrashPathInvalid", "the path must be absolute, clean and not the root", "path", path)
	}
	if err := ctx.Err(); err != nil {
		return TrashedItem{}, errors.NewErr("TrashFailed", err)
	}
	fsys := t.fsys()
	info, err := fsys.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return TrashedItem{}, errors.NewErr("TrashSourceMissing", err).SetMetadata("path", path)
		}
		return TrashedItem{}, errors.NewErr("TrashFailed", err).SetMetadata("path", path)
	}
	for _, sub := range []string{"files", "info"} {
		if err := fsys.MkdirAll(filepath.Join(t.dir, sub), 0o700); err != nil {
			return TrashedItem{}, errors.NewErr("TrashFailed", err).SetMetadata("path", filepath.Join(t.dir, sub))
		}
	}
	trashInfo, err := fsys.Stat(t.dir)
	if err != nil {
		return TrashedItem{}, errors.NewErr("TrashFailed", err).SetMetadata("path", t.dir)
	}
	srcDev, ok1 := fsys.Device(info)
	dstDev, ok2 := fsys.Device(trashInfo)
	if ok1 && ok2 && srcDev != dstDev {
		return TrashedItem{}, errors.New("TrashCrossDevice", "the item is on another filesystem than the trash", "path", path)
	}

	item := TrashedItem{OriginalPath: path, DeletedAt: t.now()}
	name, err := t.claimName(fsys, filepath.Base(path), path, item.DeletedAt)
	if err != nil {
		return TrashedItem{}, err
	}
	item.Name = name
	if err := fsys.Rename(path, t.filesPath(name)); err != nil {
		_ = fsys.Remove(t.infoPath(name))
		if isCrossDevice(err) {
			return TrashedItem{}, errors.New("TrashCrossDevice", "the item is on another filesystem than the trash", "path", path)
		}
		return TrashedItem{}, errors.NewErr("TrashFailed", err).SetMetadata("path", path)
	}
	return item, nil
}

func isCrossDevice(err error) bool {
	if le, ok := err.(*os.LinkError); ok {
		err = le.Err
	}
	return err == syscall.EXDEV
}

// claimName picks a name that is free in "files" and "info", and creates its
// .trashinfo file at the same time so that no one else takes it.
func (t *HomeTrash) claimName(fsys fileSystem, base, original string, deleted time.Time) (string, error) {
	content := "[Trash Info]\nPath=" + escapeTrashPath(original) + "\nDeletionDate=" + deleted.Format(trashInfoTime) + "\n"
	for n := 1; ; n++ {
		name := base
		if n > 1 {
			name = base + "." + strconv.Itoa(n)
		}
		if _, err := fsys.Lstat(t.filesPath(name)); err == nil {
			continue
		}
		f, err := fsys.OpenFile(t.infoPath(name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			if os.IsExist(err) {
				continue
			}
			return "", errors.NewErr("TrashFailed", err).SetMetadata("path", t.infoPath(name))
		}
		_, werr := f.Write([]byte(content))
		cerr := f.Close()
		if werr != nil || cerr != nil {
			_ = fsys.Remove(t.infoPath(name))
			return "", errors.NewErr("TrashFailed", firstErr(werr, cerr)).SetMetadata("path", t.infoPath(name))
		}
		return name, nil
	}
}

func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// escapeTrashPath percent-encodes a path as the specification asks (RFC 2396):
// letters, digits and "-_.!~*'()" and "/" stay as they are.
func escapeTrashPath(p string) string {
	var sb strings.Builder
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', strings.IndexByte("-_.!~*'()/", c) >= 0:
			sb.WriteByte(c)
		default:
			fmt.Fprintf(&sb, "%%%02X", c)
		}
	}
	return sb.String()
}

// Restore puts a trashed item back where it was. The folders above it are created
// if they are gone. It fails with the tags TrashItemMissing, RestoreTargetExists
// (something is at the original path now) and RestoreFailed.
func (t *HomeTrash) Restore(ctx context.Context, item TrashedItem) error {
	fsys := t.fsys()
	if err := ctx.Err(); err != nil {
		return errors.NewErr("RestoreFailed", err)
	}
	if item.Name == "" || strings.ContainsRune(item.Name, '/') || item.Name == "." || item.Name == ".." {
		return errors.New("TrashItemMissing", "the item has no valid name in the trash", "name", item.Name)
	}
	if _, err := fsys.Lstat(t.filesPath(item.Name)); err != nil {
		return errors.NewErr("TrashItemMissing", err).SetMetadata("name", item.Name)
	}
	if !filepath.IsAbs(item.OriginalPath) {
		return errors.New("RestoreFailed", "the original path is not absolute", "path", item.OriginalPath)
	}
	if _, err := fsys.Lstat(item.OriginalPath); err == nil {
		return errors.New("RestoreTargetExists", "something is at the original path", "path", item.OriginalPath)
	}
	if err := fsys.MkdirAll(filepath.Dir(item.OriginalPath), 0o755); err != nil {
		return errors.NewErr("RestoreFailed", err).SetMetadata("path", item.OriginalPath)
	}
	if err := fsys.Rename(t.filesPath(item.Name), item.OriginalPath); err != nil {
		return errors.NewErr("RestoreFailed", err).SetMetadata("path", item.OriginalPath)
	}
	_ = fsys.Remove(t.infoPath(item.Name))
	return nil
}

// Delete removes a file or folder for good. The path must be absolute and clean,
// and not the root. It fails with the tags DeletePathInvalid, FileNotFound and
// DeleteFailed.
func Delete(ctx context.Context, path string) error {
	return deleteWith(ctx, osFS{}, path)
}

func deleteWith(ctx context.Context, fsys fileSystem, path string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return errors.New("DeletePathInvalid", "the path must be absolute, clean and not the root", "path", path)
	}
	if err := ctx.Err(); err != nil {
		return errors.NewErr("DeleteFailed", err)
	}
	if _, err := fsys.Lstat(path); err != nil {
		if os.IsNotExist(err) {
			return errors.NewErr("FileNotFound", err).SetMetadata("path", path)
		}
		return errors.NewErr("DeleteFailed", err).SetMetadata("path", path)
	}
	if err := fsys.RemoveAll(path); err != nil {
		return errors.NewErr("DeleteFailed", err).SetMetadata("path", path)
	}
	return nil
}
