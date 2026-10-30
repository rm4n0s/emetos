package git

import (
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"
)

// RepoInfo is what "git rev-parse" tells about the repository a folder is in.
type RepoInfo struct {
	// WorkTree is the top level of the work tree ("--show-toplevel"); empty for a bare repository.
	WorkTree string
	// GitDir is the git directory ("--git-dir"), absolute: the folder .git, or for a linked worktree a folder under
	// .git/worktrees of the main repository.
	GitDir string
	// CommonDir is the git directory that holds the objects and refs shared by the worktrees ("--git-common-dir"); equal to
	// GitDir for a repository that is not a linked worktree.
	CommonDir string
	// SuperProject is the work tree of the repository that holds this one as a submodule, empty when there is none.
	SuperProject string
	// InsideWorkTree says that the folder is inside the work tree (false inside the git directory).
	InsideWorkTree bool
	// Bare says that the repository has no work tree.
	Bare bool
	// Shallow says that the repository has an incomplete history.
	Shallow bool
}

// RevParseGitDirArgs and RevParseTopLevelArgs are the two "git rev-parse" commands that RepoInfo is made from. The first works
// in any repository, bare or not; the second only in a work tree, and prints the top level and, inside a submodule, the work
// tree of the super project.
var (
	RevParseGitDirArgs   = []string{"rev-parse", "--git-dir", "--git-common-dir", "--is-inside-work-tree", "--is-bare-repository", "--is-shallow-repository"}
	RevParseTopLevelArgs = []string{"rev-parse", "--show-toplevel", "--show-superproject-working-tree"}
)

// ParseRevParse builds the RepoInfo from the outputs of the two rev-parse commands run in cwd (topLevel is empty for a bare
// repository or a folder inside the git directory). Relative paths are made absolute against cwd. The error has the tag
// GitOutputUnexpected.
//
// Ported from VS Code: extensions/git/src/git.ts (getRepositoryRoot, getRepositoryDotGit).
func ParseRevParse(gitDirOutput, topLevelOutput, cwd string) (RepoInfo, error) {
	lines := strings.Split(strings.TrimRight(gitDirOutput, "\n"), "\n")
	if len(lines) != 5 {
		return RepoInfo{}, errors.New("GitOutputUnexpected", "git rev-parse printed an unexpected number of lines",
			"lines", len(lines), "want", 5)
	}
	abs := func(p string) string {
		if filepath.IsAbs(p) {
			return filepath.Clean(p)
		}
		return filepath.Join(cwd, p)
	}
	info := RepoInfo{
		GitDir:         abs(lines[0]),
		CommonDir:      abs(lines[1]),
		InsideWorkTree: lines[2] == "true",
		Bare:           lines[3] == "true",
		Shallow:        lines[4] == "true",
	}
	if top := strings.TrimRight(topLevelOutput, "\n"); top != "" {
		parts := strings.Split(top, "\n")
		info.WorkTree = filepath.Clean(parts[0])
		if len(parts) > 1 && parts[1] != "" {
			info.SuperProject = filepath.Clean(parts[1])
		}
	}
	return info, nil
}

// ConfigEntry is one setting printed by "git config --list -z".
type ConfigEntry struct {
	// Key is the name, "section.subsection.name" with the section and the name in lower case.
	Key string
	// Value is the value; empty for a key with no value.
	Value string
	// HasValue is false for a key written without "=" (a boolean true).
	HasValue bool
}

// ParseConfigList parses the output of "git config --list -z": records "key\nvalue\0", or "key\0" for a key with no value. A
// record that is cut short is dropped.
func ParseConfigList(data []byte) []ConfigEntry {
	var out []ConfigEntry
	for len(data) > 0 {
		end := indexNUL(data)
		if end < 0 {
			break
		}
		rec := string(data[:end])
		data = data[end+1:]
		key, value, has := strings.Cut(rec, "\n")
		out = append(out, ConfigEntry{Key: key, Value: value, HasValue: has})
	}
	return out
}

func indexNUL(b []byte) int {
	for i, c := range b {
		if c == 0 {
			return i
		}
	}
	return -1
}
