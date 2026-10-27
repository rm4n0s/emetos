// Package statefile reads and writes the small state files that the Remote Work and Relay extensions keep in
// $XDG_STATE_HOME/emetos (remote-work.json, remote-work-hosts.json, the host key; SPEC §10.18 "As built (step 15.2)").
// The files decide who may log in and which host a client trusts, so they are written atomically (a temporary file in the
// same folder, then a rename) with mode 0600 in a folder of mode 0700, and they are read only when they are what the
// extension wrote: a regular file (a link is refused), of a size within a limit, that nobody else can change (a file that
// the group or the others can write is refused) and, for a private key, nobody else can read.
package statefile

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"github.com/rm4n0s/errors"
)

// The modes a file must not have when it is read: Writable is write access for the group and the others, and Private any
// access for them.
const (
	// Writable is the check of a state file: the extension is the only one that may change it.
	Writable fs.FileMode = 0o022
	// Private is the check of a private key: the extension is the only one that may read it, too.
	Private fs.FileMode = 0o077
)

// Write replaces the file at path with data: the folder is made (mode 0700) when it is missing, the data is written to a
// temporary file of mode 0600 in it, flushed to the disk and renamed over the file, so a reader sees the old file or the new
// one and never a part of it. The errors have the tag StateFileWriteFailed.
func Write(path string, data []byte) (err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return errors.NewErr("StateFileWriteFailed", err).SetMetadata("path", path, "step", "mkdir")
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return errors.NewErr("StateFileWriteFailed", err).SetMetadata("path", path, "step", "create")
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return errors.NewErr("StateFileWriteFailed", err).SetMetadata("path", path, "step", "chmod")
	}
	if _, err := tmp.Write(data); err != nil {
		return errors.NewErr("StateFileWriteFailed", err).SetMetadata("path", path, "step", "write")
	}
	if err := tmp.Sync(); err != nil {
		return errors.NewErr("StateFileWriteFailed", err).SetMetadata("path", path, "step", "sync")
	}
	if err := tmp.Close(); err != nil {
		return errors.NewErr("StateFileWriteFailed", err).SetMetadata("path", path, "step", "close")
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return errors.NewErr("StateFileWriteFailed", err).SetMetadata("path", path, "step", "rename")
	}
	return nil
}

// Read returns the content of the file at path. The file must be a regular file that is not a link, at most limit bytes
// long, and must not have any of the bits of forbid in its mode (Writable or Private). The errors have the tags
// StateFileReadFailed (the cause is kept, so Missing tells a file that does not exist), StateFileNotRegular,
// StateFileUnsafeMode and StateFileTooLarge.
func Read(path string, limit int64, forbid fs.FileMode) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, errors.NewErr("StateFileReadFailed", err).SetMetadata("path", path)
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, errors.NewErr("StateFileReadFailed", err).SetMetadata("path", path)
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("StateFileNotRegular", "the state file is not a regular file", "path", path, "mode", info.Mode().String())
	}
	if info.Mode().Perm()&forbid != 0 {
		return nil, errors.New("StateFileUnsafeMode", "the state file can be changed or read by other users; remove their access (chmod 600)",
			"path", path, "mode", info.Mode().Perm().String())
	}
	if info.Size() > limit {
		return nil, errors.New("StateFileTooLarge", "the state file is larger than the extension ever writes", "path", path, "size", info.Size(), "limit", limit)
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, errors.NewErr("StateFileReadFailed", err).SetMetadata("path", path)
	}
	if int64(len(data)) > limit {
		return nil, errors.New("StateFileTooLarge", "the state file is larger than the extension ever writes", "path", path, "limit", limit)
	}
	return data, nil
}

// Missing tells whether err is the error of Read for a file that does not exist.
func Missing(err error) bool { return errors.Is(err, fs.ErrNotExist) }
