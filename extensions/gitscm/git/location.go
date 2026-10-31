package git

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
)

// Kind says what a repository found on disk is.
type Kind int

// The kinds of repository.
const (
	// KindPlain is a repository with a .git folder, or a bare one.
	KindPlain Kind = iota
	// KindSubmodule is a work tree whose .git file points into the git directory of a super project (.git/modules/<name>).
	KindSubmodule
	// KindWorktree is a linked worktree: its .git file points into .git/worktrees/<name> of the main repository.
	KindWorktree
)

// String is the name of the kind.
func (k Kind) String() string {
	switch k {
	case KindSubmodule:
		return "submodule"
	case KindWorktree:
		return "worktree"
	}
	return "repository"
}

// Location is a repository found by looking at the file system (SPEC §10.30 "Repositories": discovery runs no git command).
type Location struct {
	// Root is the top level of the work tree, or the git directory of a bare repository, with symbolic links resolved: the key of
	// the repository.
	Root string
	// GitDir is the absolute git directory (the folder .git, or the folder a .git file points to).
	GitDir string
	// Bare says the repository has no work tree.
	Bare bool
	// Kind says whether the repository is a submodule or a linked worktree.
	Kind Kind
	// Parent is, for a submodule or a worktree, the root of the repository it belongs to (the super project, the main work
	// tree); empty for the others. Trust is inherited from it (SPEC §10.30 "Trusting a repository").
	Parent string
}

// maxGitFileSize bounds a .git file, which holds one line.
const maxGitFileSize = 4 << 10

// FindRepository looks for the repository that dir is in, from dir up to the root of the file system, by looking for a .git folder
// or file (and, in a folder that is a bare repository, for HEAD, objects and refs). It starts no process and reads no
// configuration. It reports false when there is none, or when the .git file cannot be read.
//
// Ported from VS Code: extensions/git/src/git.ts (findGitRepo, getRepositoryRoot) in spirit; VS Code asks git, Emetos must not
// before the repository is trusted (D37).
func FindRepository(dir string) (Location, bool) {
	dir = filepath.Clean(dir)
	for {
		if loc, ok := locationAt(dir); ok {
			return loc, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return Location{}, false
		}
		dir = parent
	}
}

// RepositoryAt is FindRepository for a folder that must be the top level of the repository: it reports false for a folder
// that is only inside one.
func RepositoryAt(dir string) (Location, bool) {
	return locationAt(filepath.Clean(dir))
}

// locationAt looks at one folder.
func locationAt(dir string) (Location, bool) {
	dotGit := filepath.Join(dir, ".git")
	info, err := os.Lstat(dotGit)
	switch {
	case err == nil && info.IsDir():
		if !looksLikeGitDir(dotGit) {
			return Location{}, false
		}
		return finish(Location{Root: dir, GitDir: dotGit})
	case err == nil && info.Mode().IsRegular():
		gitDir, ok := readGitFile(dir, dotGit)
		if !ok {
			return Location{}, false
		}
		return finish(Location{Root: dir, GitDir: gitDir})
	}
	if looksLikeGitDir(dir) {
		return finish(Location{Root: dir, GitDir: dir, Bare: true})
	}
	return Location{}, false
}

// looksLikeGitDir says whether a folder has the files every git directory has.
func looksLikeGitDir(dir string) bool {
	head, err := os.Stat(filepath.Join(dir, "HEAD"))
	if err != nil || !head.Mode().IsRegular() {
		return false
	}
	if o, err := os.Stat(filepath.Join(dir, "objects")); err != nil || !o.IsDir() {
		return false
	}
	// refs/ is missing in a repository that only has packed refs through the reftable format; objects and HEAD are enough
	return true
}

// readGitFile reads the "gitdir: <path>" line of a .git file; a relative path is relative to the work tree.
func readGitFile(dir, file string) (string, bool) {
	f, err := os.Open(file)
	if err != nil {
		return "", false
	}
	defer func() { _ = f.Close() }() // a read only file: closing cannot lose anything
	buf := make([]byte, maxGitFileSize)
	n, _ := f.Read(buf)
	line, _, _ := bytes.Cut(buf[:n], []byte("\n"))
	text := strings.TrimSpace(string(line))
	rest, ok := strings.CutPrefix(text, "gitdir:")
	if !ok {
		return "", false
	}
	path := strings.TrimSpace(rest)
	if path == "" {
		return "", false
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	return filepath.Clean(path), true
}

// finish resolves symbolic links of the root and works out the kind and the parent of the repository.
func finish(loc Location) (Location, bool) {
	if resolved, err := filepath.EvalSymlinks(loc.Root); err == nil {
		// the git directory is resolved with the same prefix when it sits inside the root
		if rel, err := filepath.Rel(loc.Root, loc.GitDir); err == nil && filepath.IsLocal(rel) {
			loc.GitDir = filepath.Join(resolved, rel)
		} else if g, err := filepath.EvalSymlinks(loc.GitDir); err == nil {
			loc.GitDir = g
		}
		loc.Root = resolved
	} else if g, err := filepath.EvalSymlinks(loc.GitDir); err == nil {
		loc.GitDir = g
	}
	if loc.Bare {
		return loc, true
	}
	sep := string(filepath.Separator)
	switch {
	case strings.Contains(loc.GitDir, sep+".git"+sep+"modules"+sep):
		loc.Kind = KindSubmodule
		loc.Parent = before(loc.GitDir, sep+".git"+sep+"modules"+sep)
	case filepath.Base(filepath.Dir(loc.GitDir)) == "worktrees":
		loc.Kind = KindWorktree
		common := filepath.Dir(filepath.Dir(loc.GitDir))
		switch {
		case strings.Contains(common, sep+".git"+sep+"modules"+sep):
			loc.Parent = before(common, sep+".git"+sep+"modules"+sep)
		case filepath.Base(common) == ".git":
			loc.Parent = filepath.Dir(common)
		default:
			loc.Parent = common // the main repository is bare
		}
	}
	if loc.Parent == loc.Root {
		loc.Parent = ""
	}
	return loc, true
}

// before returns the part of s before the first sep.
func before(s, sep string) string {
	before, _, _ := strings.Cut(s, sep)
	return before
}

// Within reports whether path is dir or below it, comparing whole path elements.
func Within(path, dir string) bool {
	path, dir = filepath.Clean(path), filepath.Clean(dir)
	if path == dir {
		return true
	}
	if dir == string(filepath.Separator) {
		return true
	}
	return strings.HasPrefix(path, dir+string(filepath.Separator))
}
