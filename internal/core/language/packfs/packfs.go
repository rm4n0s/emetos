// Package packfs gives tests the files of the language packs, which live in the extensions of Emetos (extensions/<name>/assets/,
// SPEC §8.1, §10.15). The packages under internal/ never import extensions/ (the editor has no special case for a bundled
// extension), so the tests of the language registry, of the highlighting and of everything that needs the bundled
// languages read the very same files from the source tree, in the order the editor registers them. The editor itself never
// uses this package.
//
// The order of Names must be the order of the language packs in extensions.All(); a test in cmd/emetos keeps the two the
// same.
package packfs

import (
	"io/fs"
	"os"
	"path/filepath"

	"github.com/rm4n0s/errors"
)

// names are the folders of the language packs under extensions/, in the order the editor registers them. Go, Markdown,
// JSON, YAML, Python, JavaScript and TypeScript come first (D13).
var names = []string{
	"golang", "json", "yaml", "markdown", "python", "javascript", "typescript",
	"html", "css", "scss", "less", "xml", "sql", "shellscript", "makefile", "docker", "ini",
	"cpp", "rust", "diff", "git", "gomod", "mongodb",
}

// Names gives the folders of the language packs under extensions/, in the order they are registered. The slice is a copy.
func Names() []string { return append([]string(nil), names...) }

// Root finds the root of the repository: the nearest folder above the working directory (the folder of the test) that has
// a go.mod. The error has the tag RepoRootNotFound.
func Root() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", errors.NewErr("RepoRootNotFound", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("RepoRootNotFound", "no go.mod above the working directory", "dir", dir)
		}
		dir = parent
	}
}

// Open gives the files of the language pack in extensions/<name>/assets: its package.json is at the root of the file
// system. The error has the tags RepoRootNotFound and PackNotFound (the folder is not there).
func Open(name string) (fs.FS, error) {
	root, err := Root()
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(root, "extensions", name, "assets")
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil, errors.New("PackNotFound", "the language pack has no assets folder", "pack", name, "dir", dir)
	}
	return os.DirFS(dir), nil
}

// All gives the file systems of every language pack, in the order of Names. The error is that of Open.
func All() ([]fs.FS, error) {
	out := make([]fs.FS, 0, len(names))
	for _, name := range names {
		fsys, err := Open(name)
		if err != nil {
			return nil, err
		}
		out = append(out, fsys)
	}
	return out, nil
}
