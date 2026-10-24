package storage

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"path/filepath"

	"github.com/rm4n0s/errors"
)

// windowIDLength is the number of hexadecimal digits of a window id.
const windowIDLength = 16

// WindowID is the id of the window that has a folder: the first 16 hexadecimal digits of the SHA-256 of
// the absolute path of the folder (SPEC §10.10). The path is cleaned first, so "/a/b/" and "/a/b" are the
// same window. A path that is not absolute has the tag WindowFolderNotAbsolute.
func WindowID(folder string) (string, error) {
	if !filepath.IsAbs(folder) {
		return "", errors.New("WindowFolderNotAbsolute", "the folder of a window is an absolute path", "folder", folder)
	}
	sum := sha256.Sum256([]byte(filepath.Clean(folder)))
	return hex.EncodeToString(sum[:])[:windowIDLength], nil
}

// NewEmptyWindowID makes a random id for the window that has no folder (SPEC §10.10); the global state
// keeps it, so the window has the same id in every run. It has the tag WindowIDUnavailable when the system
// has no randomness.
func NewEmptyWindowID() (string, error) { return newEmptyWindowID(rand.Reader) }

// newEmptyWindowID reads the random bytes of the id from r.
func newEmptyWindowID(r io.Reader) (string, error) {
	var b [windowIDLength / 2]byte
	if _, err := io.ReadFull(r, b[:]); err != nil {
		return "", errors.NewErr("WindowIDUnavailable", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// validWindowID tells whether id has the shape of a window id, which also makes it safe as a file name.
func validWindowID(id string) bool {
	if len(id) != windowIDLength {
		return false
	}
	for _, c := range id {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
