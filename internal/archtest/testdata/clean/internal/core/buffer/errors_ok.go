package buffer

import (
	"os"

	"github.com/rm4n0s/errors"
)

// ErrClosed is a sentinel: a struct literal, never a package-level errors.New.
var ErrClosed = &errors.Error{Tag: "BufferClosed", Message: "the buffer is closed"}

// Open tests something, then builds the error where the failure is found.
func Open(path string, closed bool) error {
	if closed {
		return errors.New("BufferClosed", "the buffer is closed", "path", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return errors.NewErr("BufferOpenFailed", err).SetMetadata("path", path)
	}
	return f.Close()
}
