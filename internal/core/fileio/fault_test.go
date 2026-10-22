package fileio

import (
	"os"
	"syscall"
)

// faultFS is the real file system with failures that tests switch on.
type faultFS struct {
	osFS
	createTempErr error
	writeErr      error
	syncErr       error
	renameErr     error
	openErr       error
	// devices makes Device answer from a map of paths' base names, to pretend a
	// file is on another filesystem.
	devices map[string]uint64
	// tempNames records the temporary files made.
	tempNames []string
}

type faultFile struct {
	writableFile
	fs *faultFS
}

func (f faultFile) Write(p []byte) (int, error) {
	if f.fs.writeErr != nil {
		// A real failure may write part of the data first.
		n, _ := f.writableFile.Write(p[:len(p)/2])
		return n, f.fs.writeErr
	}
	return f.writableFile.Write(p)
}

func (f faultFile) Sync() error {
	if f.fs.syncErr != nil {
		return f.fs.syncErr
	}
	return f.writableFile.Sync()
}

func (fs *faultFS) CreateTemp(dir, pattern string) (writableFile, error) {
	if fs.createTempErr != nil {
		return nil, fs.createTempErr
	}
	f, err := fs.osFS.CreateTemp(dir, pattern)
	if err != nil {
		return nil, err
	}
	fs.tempNames = append(fs.tempNames, f.Name())
	return faultFile{f, fs}, nil
}

func (fs *faultFS) OpenFile(path string, flag int, perm os.FileMode) (writableFile, error) {
	if fs.openErr != nil {
		return nil, fs.openErr
	}
	f, err := fs.osFS.OpenFile(path, flag, perm)
	if err != nil {
		return nil, err
	}
	return faultFile{f, fs}, nil
}

func (fs *faultFS) Rename(oldPath, newPath string) error {
	if fs.renameErr != nil {
		return fs.renameErr
	}
	return fs.osFS.Rename(oldPath, newPath)
}

func (fs *faultFS) Device(info os.FileInfo) (uint64, bool) {
	if dev, ok := fs.devices[info.Name()]; ok {
		return dev, true
	}
	return fs.osFS.Device(info)
}

var errNoSpace = &os.PathError{Op: "write", Path: "x", Err: syscall.ENOSPC}
