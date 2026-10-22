package fileio

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
)

// newFileMode is the permission of a file Write creates.
const newFileMode os.FileMode = 0o644

// WriteOptions say how text is turned into bytes and how the file is replaced.
type WriteOptions struct {
	// Encoding is the id of the encoding to write; empty means "utf8". The ids
	// "utf8bom", "utf16le" and "utf16be" are the only ones that can have a mark.
	Encoding string
	// LineEnding is the line ending to write in place of "\n"; empty means LF.
	LineEnding LineEnding
	// BOM writes a byte order mark for UTF-16. UTF-8 gets one when Encoding is
	// "utf8bom"; other encodings have none.
	BOM bool
	// Overwrite lets a read-only file be saved: its owner write permission is
	// added first ("Overwrite" of VS Code's save conflict prompt).
	Overwrite bool

	fs fileSystem // replaced by tests
}

func (o WriteOptions) fsys() fileSystem {
	if o.fs != nil {
		return o.fs
	}
	return osFS{}
}

// Write saves text in a file (SPEC §6.3) and returns its new FileStat. The text
// uses "\n"; it is turned into the line ending and the encoding of opts, with a
// byte order mark when they ask for one. The file is replaced atomically: the
// bytes go to a temporary file in the same folder, which is flushed to disk with
// fsync, given the permissions of the original, and renamed over it, so a failure
// leaves the original as it was. When the folder can't be written to, the file is
// written in place instead. A link is followed, so the file it points to is
// replaced. A new file gets the permissions 0644.
//
// It fails with the tags EncodingUnknown, EncodingCannotRepresent, FileReadOnly,
// FileIsDirectory, FileNotRegular, FileNotFound (the folder doesn't exist),
// FilePermissionDenied, FileWriteFailed and WriteCancelled.
func Write(ctx context.Context, path, text string, opts WriteOptions) (FileStat, error) {
	data, err := encodeText(text, opts)
	if err != nil {
		return FileStat{}, err
	}
	if err := ctx.Err(); err != nil {
		return FileStat{}, errors.NewErr("WriteCancelled", err)
	}
	fsys := opts.fsys()

	target, mode, exists, err := prepareTarget(fsys, path, opts.Overwrite)
	if err != nil {
		return FileStat{}, err
	}
	if err := writeAtomic(fsys, target, data, mode); err != nil {
		if !isPermission(err) {
			return FileStat{}, err
		}
		// The folder isn't writable: write the file itself.
		if err := writeInPlace(fsys, target, data, mode, exists); err != nil {
			return FileStat{}, err
		}
	}
	info, err := fsys.Stat(target)
	if err != nil {
		return FileStat{}, errors.NewErr("FileWriteFailed", err).SetMetadata("path", path)
	}
	return FileStat{ModTime: info.ModTime(), Size: info.Size(), Mode: info.Mode()}, nil
}

// prepareTarget looks at what is at path: the file to replace (after links), the
// permissions to give the result, and whether it exists. A read-only file fails
// unless overwrite is set, which makes it writable.
func prepareTarget(fsys fileSystem, path string, overwrite bool) (target string, mode os.FileMode, exists bool, err error) {
	info, err := fsys.Stat(path)
	switch {
	case err == nil:
	case os.IsNotExist(err):
		return path, newFileMode, false, nil
	case os.IsPermission(err):
		return "", 0, false, errors.NewErr("FilePermissionDenied", err).SetMetadata("path", path)
	default:
		return "", 0, false, errors.NewErr("FileWriteFailed", err).SetMetadata("path", path)
	}
	switch {
	case info.IsDir():
		return "", 0, false, errors.New("FileIsDirectory", "the path is a folder", "path", path)
	case !info.Mode().IsRegular():
		return "", 0, false, errors.New("FileNotRegular", "the path is not a regular file", "path", path, "mode", info.Mode().String())
	}
	target = path
	if resolved, err := fsys.EvalSymlinks(path); err == nil {
		target = resolved
	}
	mode = info.Mode().Perm()
	if mode&0o200 == 0 {
		if !overwrite {
			return "", 0, false, errors.New("FileReadOnly", "the file is read-only", "path", path)
		}
		mode |= 0o200
		if err := fsys.Chmod(target, mode); err != nil {
			return "", 0, false, errors.NewErr("FilePermissionDenied", err).SetMetadata("path", path)
		}
	}
	return target, mode, true, nil
}

// writeAtomic writes data to a temporary file next to target and renames it over.
func writeAtomic(fsys fileSystem, target string, data []byte, mode os.FileMode) (err error) {
	dir, base := filepath.Split(target)
	if dir == "" {
		dir = "."
	}
	tmp, err := fsys.CreateTemp(dir, "."+base+".emetos-*.tmp")
	if err != nil {
		return writeError(err, target)
	}
	renamed := false
	defer func() {
		if !renamed {
			_ = tmp.Close()
			_ = fsys.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return writeError(err, target)
	}
	if err := tmp.Chmod(mode); err != nil {
		return writeError(err, target)
	}
	if err := tmp.Sync(); err != nil {
		return writeError(err, target)
	}
	if err := tmp.Close(); err != nil {
		return writeError(err, target)
	}
	if err := fsys.Rename(tmp.Name(), target); err != nil {
		return writeError(err, target)
	}
	renamed = true
	fsys.SyncDir(dir)
	return nil
}

// writeInPlace writes data into the file itself, for a folder that can't be written to.
func writeInPlace(fsys fileSystem, target string, data []byte, mode os.FileMode, exists bool) error {
	flag := os.O_WRONLY | os.O_TRUNC
	if !exists {
		flag |= os.O_CREATE
	}
	f, err := fsys.OpenFile(target, flag, mode)
	if err != nil {
		return writeError(err, target)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return writeError(err, target)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return writeError(err, target)
	}
	if err := f.Close(); err != nil {
		return writeError(err, target)
	}
	return nil
}

// writeError tags an error of the operating system.
func writeError(err error, path string) error {
	switch {
	case os.IsPermission(err):
		return errors.NewErr("FilePermissionDenied", err).SetMetadata("path", path)
	case os.IsNotExist(err):
		return errors.NewErr("FileNotFound", err).SetMetadata("path", path)
	}
	return errors.NewErr("FileWriteFailed", err).SetMetadata("path", path)
}

func isPermission(err error) bool {
	appErr, ok := errors.FromError(err)
	return ok && appErr.Tag == "FilePermissionDenied"
}

// encodeText turns text, which uses "\n", into the bytes to write.
func encodeText(text string, opts WriteOptions) ([]byte, error) {
	id := opts.Encoding
	if id == "" {
		id = UTF8
	}
	enc, ok := LookupEncoding(id)
	if !ok {
		return nil, errors.New("EncodingUnknown", "there is no such encoding", "encoding", id)
	}
	if opts.LineEnding == CRLF {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	var bom []byte
	var body []byte
	if enc.enc == nil { // UTF-8
		body = []byte(text)
		if id == UTF8BOM {
			bom = []byte{0xEF, 0xBB, 0xBF}
		}
	} else {
		var err error
		body, err = enc.enc.NewEncoder().Bytes([]byte(text))
		if err != nil {
			return nil, cannotRepresent(id, text, err)
		}
		switch {
		case id == UTF16LE && opts.BOM:
			bom = []byte{0xFF, 0xFE}
		case id == UTF16BE && opts.BOM:
			bom = []byte{0xFE, 0xFF}
		}
	}
	return append(bom, body...), nil
}

// cannotRepresent finds the first character the encoding has no code for.
func cannotRepresent(id, text string, cause error) error {
	enc, _ := LookupEncoding(id)
	line := 1
	for _, r := range text {
		if r == '\n' {
			line++
			continue
		}
		if r == utf8.RuneError {
			continue
		}
		if _, err := enc.enc.NewEncoder().String(string(r)); err != nil {
			return errors.NewErr("EncodingCannotRepresent", cause).SetMetadata("encoding", id, "character", string(r), "line", line)
		}
	}
	return errors.NewErr("EncodingCannotRepresent", cause).SetMetadata("encoding", id)
}
