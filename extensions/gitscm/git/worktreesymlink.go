package git

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/errors"
)

// The folders of the main work tree that a new worktree gets a symbolic link to (git.worktreeSymlinkFolders, SPEC §10.30 "Worktrees"):
// which ignored folders match the patterns, and the link.
//
// Ported from VS Code: extensions/git/src/worktreeSymlink.ts

// WorktreeSymlinkStatus says what CreateWorktreeSymlink did.
type WorktreeSymlinkStatus string

// The results of CreateWorktreeSymlink.
const (
	// SymlinkCreated: the link was made.
	SymlinkCreated WorktreeSymlinkStatus = "created"
	// SymlinkSourceContainsWorktree: the folder holds the worktree, so a link to it would be a loop.
	SymlinkSourceContainsWorktree WorktreeSymlinkStatus = "sourceContainsWorktree"
	// SymlinkTargetExists: there is something at the place of the link.
	SymlinkTargetExists WorktreeSymlinkStatus = "targetExists"
)

// WorktreeSymlinkFolderCandidates returns the ancestor folders of the files that are both ignored and matched by the patterns, nearest
// folder first for each file, each folder once. ignoredOutput and matchedOutput are NUL separated lists of files.
func WorktreeSymlinkFolderCandidates(ignoredOutput, matchedOutput string) []string {
	matched := setOf(splitNulString(matchedOutput))
	seen := map[string]bool{}
	var out []string
	for _, file := range splitNulString(ignoredOutput) {
		if !matched[file] {
			continue
		}
		for i := strings.LastIndexByte(file, '/'); i != -1; i = strings.LastIndexByte(file[:i], '/') {
			dir := file[:i]
			if !seen[dir] {
				seen[dir] = true
				out = append(out, dir)
			}
		}
	}
	return out
}

// FilterWorktreeSymlinkFolders selects the candidate folders that are ignored themselves, match the patterns and contain no tracked
// file, and drops a folder that lies inside another one of the result (the link of the outer one covers it). The outputs are NUL
// separated lists of folders with a trailing slash: those "git check-ignore" says are ignored, those the patterns match, and
// "git ls-files --others --ignored --exclude-standard --directory" (the wholly ignored folders).
func FilterWorktreeSymlinkFolders(candidates []string, ignoredOutput, matchedOutput, directoryOutput string) []string {
	ignored := setOf(splitNulString(ignoredOutput))
	matched := setOf(splitNulString(matchedOutput))
	whole := map[string]bool{}
	for _, d := range splitNulString(directoryOutput) {
		if strings.HasSuffix(d, "/") {
			whole[d] = true
		}
	}
	var dirs []string
	set := map[string]bool{}
	for _, d := range candidates {
		if ignored[d+"/"] && matched[d+"/"] && hasContainingDirectory(d, whole, true) {
			dirs = append(dirs, d)
			set[d+"/"] = true
		}
	}
	var out []string
	for _, d := range dirs {
		if !hasContainingDirectory(d, set, false) {
			out = append(out, d)
		}
	}
	return out
}

// hasContainingDirectory says whether the folder, or with includeSelf unset only a folder above it, is in directories (written with a
// trailing slash).
func hasContainingDirectory(dir string, directories map[string]bool, includeSelf bool) bool {
	i := len(dir)
	if !includeSelf {
		i = strings.LastIndexByte(dir, '/')
	}
	for i > 0 {
		if directories[dir[:i]+"/"] {
			return true
		}
		i = strings.LastIndexByte(dir[:i], '/')
	}
	return false
}

// CreateWorktreeSymlink makes a symbolic link in the worktree to a folder of the repository. folder is relative to both, with forward
// slashes. It does nothing when the folder holds the worktree or the place of the link is taken. The error has the tags
// GitWorktreeSymlinkFailed (the paths could not be read or the link not made), GitWorktreeSymlinkParentLink and
// GitWorktreeSymlinkParentNotDir (a folder above the link in the worktree is a link or not a folder).
func CreateWorktreeSymlink(repositoryRoot, worktreeRoot, folder string) (WorktreeSymlinkStatus, error) {
	folder = filepath.FromSlash(folder)
	source := filepath.Join(repositoryRoot, folder)
	target := filepath.Join(worktreeRoot, folder)
	sourceReal, err := filepath.EvalSymlinks(source)
	if err != nil {
		return "", errors.NewErr("GitWorktreeSymlinkFailed", err).SetMetadata("path", source, "step", "realpath-source")
	}
	worktreeReal, err := filepath.EvalSymlinks(worktreeRoot)
	if err != nil {
		return "", errors.NewErr("GitWorktreeSymlinkFailed", err).SetMetadata("path", worktreeRoot, "step", "realpath-worktree")
	}
	if Within(worktreeReal, sourceReal) {
		return SymlinkSourceContainsWorktree, nil
	}
	if parent := filepath.Dir(folder); parent != "." {
		current := worktreeRoot
		for _, segment := range strings.Split(parent, string(filepath.Separator)) {
			current = filepath.Join(current, segment)
			info, err := os.Lstat(current)
			if os.IsNotExist(err) {
				break
			}
			if err != nil {
				return "", errors.NewErr("GitWorktreeSymlinkFailed", err).SetMetadata("path", current, "step", "lstat-parent")
			}
			if info.Mode()&os.ModeSymlink != 0 {
				return "", errors.New("GitWorktreeSymlinkParentLink", "cannot create the link because a folder above it in the worktree is a symbolic link", "path", current)
			}
			if !info.IsDir() {
				return "", errors.New("GitWorktreeSymlinkParentNotDir", "cannot create the link because a folder above it in the worktree is not a folder", "path", current)
			}
		}
	}
	if _, err := os.Lstat(target); err == nil {
		return SymlinkTargetExists, nil
	} else if !os.IsNotExist(err) {
		return "", errors.NewErr("GitWorktreeSymlinkFailed", err).SetMetadata("path", target, "step", "lstat-target")
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return "", errors.NewErr("GitWorktreeSymlinkFailed", err).SetMetadata("path", target, "step", "mkdir")
	}
	if err := os.Symlink(source, target); err != nil {
		return "", errors.NewErr("GitWorktreeSymlinkFailed", err).SetMetadata("path", target, "step", "symlink")
	}
	return SymlinkCreated, nil
}
