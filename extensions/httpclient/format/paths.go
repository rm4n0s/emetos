package format

import (
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"
)

// ResolvePath returns the absolute path of p, a path relative to the workspace folder (an absolute one must lie inside it),
// and refuses a path that leaves the folder, by a ".." or by a link that points out of it. A path that does not exist yet is
// checked through its nearest folder that does. The errors have the tags HttpPathInvalid (empty, or with a NUL) and
// HttpPathOutsideWorkspace.
func ResolvePath(folder, p string) (string, error) {
	if p == "" || strings.ContainsRune(p, 0) {
		return "", errors.New("HttpPathInvalid", "the path is empty or has a NUL", "path", p)
	}
	folder = filepath.Clean(folder)
	abs := p
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(folder, abs)
	}
	abs = filepath.Clean(abs)
	if !within(folder, abs) {
		return "", errors.New("HttpPathOutsideWorkspace", "the path is outside the workspace folder", "path", p, "folder", folder)
	}
	realFolder, err := filepath.EvalSymlinks(folder)
	if err != nil {
		return abs, nil //nolint:nilerr // a folder that cannot be resolved has no links to follow; the lexical check stands
	}
	// resolve the nearest ancestor that exists, then put the rest back
	probe, rest := abs, ""
	for {
		resolved, err := filepath.EvalSymlinks(probe)
		if err == nil {
			if !within(realFolder, filepath.Join(resolved, rest)) {
				return "", errors.New("HttpPathOutsideWorkspace", "the path leaves the workspace folder through a link", "path", p, "folder", folder)
			}
			return abs, nil
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return abs, nil
		}
		rest = filepath.Join(filepath.Base(probe), rest)
		probe = parent
	}
}

// within reports whether path is dir or below it (both clean).
func within(dir, path string) bool {
	rel, err := filepath.Rel(dir, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// CheckPaths checks every file path a request names (the path of a binary body and the files of multipart fields) with
// ResolvePath, and returns the first error.
func CheckPaths(folder string, r Request) error {
	if r.Body == nil {
		return nil
	}
	if r.Body.Type == BodyBinary && r.Body.Path != "" && !strings.Contains(r.Body.Path, "{{") {
		if _, err := ResolvePath(folder, r.Body.Path); err != nil {
			return err
		}
	}
	for _, f := range r.Body.Fields {
		if f.File != "" && !strings.Contains(f.File, "{{") {
			if _, err := ResolvePath(folder, f.File); err != nil {
				return err
			}
		}
	}
	return nil
}
