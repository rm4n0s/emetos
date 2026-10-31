package git

import (
	"path/filepath"
	"strings"
)

// TrustStore says which repositories the user trusted (extensions/gitscm/trust); the paths it is asked about are real paths, with
// symbolic links resolved.
type TrustStore interface {
	// IsTrusted reports whether the repository with this root, or a folder above it that was trusted as a parent, is trusted.
	IsTrusted(root string) bool
}

// TrustedBy makes the Config.Trust function of a store: a repository is trusted when the store trusts its root, or the repository
// it belongs to as a submodule or a linked worktree (SPEC §10.30 "Trusting a repository": they are trusted with their parent).
func TrustedBy(store TrustStore) func(Location) bool {
	var trusted func(loc Location, depth int) bool
	trusted = func(loc Location, depth int) bool {
		if store.IsTrusted(loc.Root) {
			return true
		}
		if loc.Parent == "" || depth > 8 {
			return false
		}
		if parent, ok := RepositoryAt(loc.Parent); ok {
			return trusted(parent, depth+1)
		}
		return store.IsTrusted(loc.Parent)
	}
	return func(loc Location) bool { return trusted(loc, 0) }
}

// untrustedRepository looks at everything that makes a command run in a repository: its working directory, the folders of -C and
// --work-tree, and the git directories of --git-dir, GIT_DIR, GIT_WORK_TREE and GIT_COMMON_DIR. It returns the first repository
// that Config.Trust does not trust (all of them when Config.Trust is nil), found by looking at the file system, so that no
// process has been started yet (SPEC §10.30 "Trusting a repository", D37). A folder that is in no repository is no concern.
func (r *Runner) untrustedRepository(c Command) (Location, bool) {
	check := func(loc Location, ok bool) (Location, bool) {
		if !ok {
			return Location{}, false
		}
		if r.cfg.Trust != nil && r.cfg.Trust(loc) {
			return Location{}, false
		}
		return loc, true
	}
	cwd := c.Dir
	if cwd != "" {
		if loc, bad := check(FindRepository(cwd)); bad {
			return loc, true
		}
	}
	base := cwd
	for i := 0; i < len(c.Args); i++ {
		a := c.Args[i]
		var value string
		switch {
		case a == "-C" && i+1 < len(c.Args):
			i++
			value = c.Args[i]
			value = absolute(base, value)
			base = value
			if loc, bad := check(FindRepository(value)); bad {
				return loc, true
			}
			continue
		case a == "--git-dir" || a == "--work-tree":
			if i+1 < len(c.Args) {
				i++
				value = c.Args[i]
			}
		case strings.HasPrefix(a, "--git-dir=") || strings.HasPrefix(a, "--work-tree="):
			_, value, _ = strings.Cut(a, "=")
		case a == "-c" || a == "--namespace" || a == "--exec-path":
			i++
			continue
		case !strings.HasPrefix(a, "-"):
			i = len(c.Args) // the command: no more global options
			continue
		default:
			continue
		}
		if value == "" {
			continue
		}
		if loc, bad := check(ownerOf(absolute(base, value))); bad {
			return loc, true
		}
	}
	for _, env := range [][]string{r.cfg.Extra, c.Env} {
		for _, kv := range env {
			name, value, _ := strings.Cut(kv, "=")
			if value == "" || (name != "GIT_DIR" && name != "GIT_WORK_TREE" && name != "GIT_COMMON_DIR") {
				continue
			}
			if loc, bad := check(ownerOf(absolute(cwd, value))); bad {
				return loc, true
			}
		}
	}
	return Location{}, false
}

// absolute makes path absolute against base (the root of the file system when base is empty).
func absolute(base, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	if base == "" {
		base = string(filepath.Separator)
	}
	return filepath.Join(base, path)
}

// ownerOf finds the repository a path belongs to when the path is a git directory or a work tree: the folder above a ".git" in
// the path (so .git, .git/modules/a and .git/worktrees/w all name the repository that holds them), else the repository the path is
// in.
func ownerOf(path string) (Location, bool) {
	for p := path; ; {
		if filepath.Base(p) == ".git" {
			return FindRepository(filepath.Dir(p))
		}
		parent := filepath.Dir(p)
		if parent == p {
			break
		}
		p = parent
	}
	return FindRepository(path)
}
