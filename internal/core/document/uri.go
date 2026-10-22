package document

import (
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

// The URI schemes of SPEC §7.1.
const (
	// SchemeFile is a file on disk; the path is absolute.
	SchemeFile = "file"
	// SchemeUntitled is a document that has no file yet; the path is its number.
	SchemeUntitled = "untitled"
	// SchemeVirtual is read-only generated content, such as the default settings
	// JSON or the saved version of a file for "Compare with Saved".
	SchemeVirtual = "emetos-virtual"
)

// URI identifies a document. Its String form is an identity, not a web address:
// the path is not percent-encoded.
type URI struct {
	Scheme string
	Path   string
}

// FileURI is the URI of a file on disk.
func FileURI(path string) URI { return URI{Scheme: SchemeFile, Path: path} }

// UntitledURI is the URI of the untitled document with number n.
func UntitledURI(n int) URI { return URI{Scheme: SchemeUntitled, Path: strconv.Itoa(n)} }

// VirtualURI is the URI of generated read-only content; path says what it is.
func VirtualURI(path string) URI { return URI{Scheme: SchemeVirtual, Path: path} }

// String is "file:///path", "untitled:3" or "emetos-virtual:path".
func (u URI) String() string {
	if u.Scheme == SchemeFile {
		return "file://" + u.Path
	}
	return u.Scheme + ":" + u.Path
}

// ParseURI reads the form String gives. It fails with the tag URIInvalid.
func ParseURI(s string) (URI, error) {
	scheme, rest, ok := strings.Cut(s, ":")
	if !ok {
		return URI{}, errors.New("URIInvalid", "the text has no scheme", "uri", s)
	}
	switch scheme {
	case SchemeFile:
		path, ok := strings.CutPrefix(rest, "//")
		if !ok || !filepath.IsAbs(path) {
			return URI{}, errors.New("URIInvalid", "a file URI is file:// followed by an absolute path", "uri", s)
		}
		return FileURI(path), nil
	case SchemeUntitled:
		if n, err := strconv.Atoi(rest); err != nil || n < 1 {
			return URI{}, errors.New("URIInvalid", "an untitled URI is untitled: followed by a number from 1", "uri", s)
		}
		return URI{Scheme: SchemeUntitled, Path: rest}, nil
	case SchemeVirtual:
		return VirtualURI(rest), nil
	}
	return URI{}, errors.New("URIInvalid", "unknown scheme", "uri", s, "scheme", scheme)
}

// Title is the name shown on a tab: the file name, "Untitled-n", or the last
// part of a virtual path.
func (u URI) Title() string {
	if u.Scheme == SchemeUntitled {
		return "Untitled-" + u.Path
	}
	return filepath.Base(u.Path)
}

// LowestFreeUntitled is the untitled URI with the lowest number that none of the
// open documents has.
func LowestFreeUntitled(open []URI) URI {
	used := map[string]bool{}
	for _, u := range open {
		if u.Scheme == SchemeUntitled {
			used[u.Path] = true
		}
	}
	for n := 1; ; n++ {
		if !used[strconv.Itoa(n)] {
			return UntitledURI(n)
		}
	}
}
