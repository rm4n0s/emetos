package format

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
)

// MaxFileSize is the largest file the client reads.
const MaxFileSize = 8 << 20

// ReadFile reads a file of the client as UTF-8 text. The errors have the tags HttpFileReadFailed (the cause is kept, so
// errors.Is(err, fs.ErrNotExist) tells a missing file), HttpFileTooLarge and HttpFileNotText.
func ReadFile(ctx context.Context, path string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", errors.NewErr("HttpCancelled", err).SetMetadata("path", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", errors.NewErr("HttpFileReadFailed", err).SetMetadata("path", path)
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, MaxFileSize+1))
	if err != nil {
		return "", errors.NewErr("HttpFileReadFailed", err).SetMetadata("path", path)
	}
	if len(data) > MaxFileSize {
		return "", errors.New("HttpFileTooLarge", "the file is larger than the client reads", "path", path, "limit", MaxFileSize)
	}
	if !utf8.Valid(data) {
		return "", errors.New("HttpFileNotText", "the file is not UTF-8 text", "path", path)
	}
	return string(data), nil
}

// WriteFile writes the text to the file at path atomically (a temporary file in the same folder, then a rename), making the
// folders it needs. The mode of a file that exists is kept; a new file is 0644. The errors have the tag HttpFileWriteFailed.
func WriteFile(ctx context.Context, path, text string) (err error) {
	if err := ctx.Err(); err != nil {
		return errors.NewErr("HttpCancelled", err).SetMetadata("path", path)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errors.NewErr("HttpFileWriteFailed", err).SetMetadata("path", path, "step", "mkdir")
	}
	mode := os.FileMode(0o644)
	if st, err := os.Stat(path); err == nil {
		mode = st.Mode().Perm()
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return errors.NewErr("HttpFileWriteFailed", err).SetMetadata("path", path, "step", "create")
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if err := tmp.Chmod(mode); err != nil {
		return errors.NewErr("HttpFileWriteFailed", err).SetMetadata("path", path, "step", "chmod")
	}
	if _, err := tmp.WriteString(text); err != nil {
		return errors.NewErr("HttpFileWriteFailed", err).SetMetadata("path", path, "step", "write")
	}
	if err := tmp.Sync(); err != nil {
		return errors.NewErr("HttpFileWriteFailed", err).SetMetadata("path", path, "step", "sync")
	}
	if err := tmp.Close(); err != nil {
		return errors.NewErr("HttpFileWriteFailed", err).SetMetadata("path", path, "step", "close")
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return errors.NewErr("HttpFileWriteFailed", err).SetMetadata("path", path, "step", "rename")
	}
	return nil
}
