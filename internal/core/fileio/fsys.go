package fileio

import (
	"io"
	"os"
	"path/filepath"
)

// writableFile is the part of *os.File that writing a file uses.
type writableFile interface {
	io.Writer
	Sync() error
	Close() error
	Name() string
	Chmod(mode os.FileMode) error
}

// fileSystem is what writing and the trash ask of the operating system. The
// tests replace it to make a write fail halfway or a rename cross devices.
type fileSystem interface {
	Stat(path string) (os.FileInfo, error)
	Lstat(path string) (os.FileInfo, error)
	EvalSymlinks(path string) (string, error)
	CreateTemp(dir, pattern string) (writableFile, error)
	OpenFile(path string, flag int, perm os.FileMode) (writableFile, error)
	Rename(oldPath, newPath string) error
	Remove(path string) error
	RemoveAll(path string) error
	Chmod(path string, mode os.FileMode) error
	MkdirAll(path string, perm os.FileMode) error
	// SyncDir flushes a folder's entries to disk; it is best effort.
	SyncDir(path string)
	// Device is the id of the filesystem a file is on.
	Device(info os.FileInfo) (uint64, bool)
}

// osFS is the real file system.
type osFS struct{}

func (osFS) Stat(path string) (os.FileInfo, error)  { return os.Stat(path) }
func (osFS) Lstat(path string) (os.FileInfo, error) { return os.Lstat(path) }
func (osFS) EvalSymlinks(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}
func (osFS) CreateTemp(dir, pattern string) (writableFile, error) { return os.CreateTemp(dir, pattern) }
func (osFS) OpenFile(path string, flag int, perm os.FileMode) (writableFile, error) {
	return os.OpenFile(path, flag, perm)
}
func (osFS) Rename(oldPath, newPath string) error      { return os.Rename(oldPath, newPath) }
func (osFS) Remove(path string) error                  { return os.Remove(path) }
func (osFS) RemoveAll(path string) error               { return os.RemoveAll(path) }
func (osFS) Chmod(path string, mode os.FileMode) error { return os.Chmod(path, mode) }
func (osFS) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (osFS) SyncDir(path string) {
	d, err := os.Open(path)
	if err != nil {
		return
	}
	_ = d.Sync()
	_ = d.Close()
}
