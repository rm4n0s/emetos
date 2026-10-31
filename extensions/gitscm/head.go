package gitscm

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// head is where HEAD points and how it stands against its upstream (VS Code's repository.HEAD).
type head struct {
	// name is the branch ("main"), empty when HEAD is detached.
	name string
	// commit is the commit HEAD names, empty for a branch with no commit yet.
	commit string
	// tag is the tag that names the commit of a detached HEAD, when one does.
	tag string
	// upstream is the branch name keeps track of, with the remote.
	upstream *git.Upstream
	// ahead and behind count the commits against the upstream.
	ahead, behind int
	// remote is the remote the upstream is on.
	remote string
}

// readHEAD reads the file HEAD of a git directory without starting git: "ref: refs/heads/main" is a branch, a hash is a
// detached HEAD.
func readHEAD(gitDir string) (name, commit string, ok bool) {
	data, err := os.ReadFile(filepath.Join(gitDir, "HEAD"))
	if err != nil {
		return "", "", false
	}
	text := strings.TrimSpace(string(data))
	if ref, found := strings.CutPrefix(text, "ref:"); found {
		ref = strings.TrimSpace(ref)
		if b, isBranch := strings.CutPrefix(ref, "refs/heads/"); isBranch {
			return b, "", true
		}
		return ref, "", true
	}
	return "", text, true
}

// headFrom builds HEAD from the file and the branches git for-each-ref listed.
func headFrom(gitDir string, refs []git.Ref) head {
	name, commit, ok := readHEAD(gitDir)
	if !ok {
		return head{}
	}
	h := head{name: name, commit: commit}
	if name == "" {
		for _, r := range refs {
			if r.Type == git.RefTag && r.Commit == commit {
				h.tag = r.Name
				break
			}
		}
		return h
	}
	for _, r := range refs {
		if r.Type == git.RefHead && r.Name == name {
			h.commit = r.Commit
			h.ahead, h.behind = r.Ahead, r.Behind
			if r.Upstream != nil {
				h.upstream = r.Upstream
				h.remote = r.Upstream.Remote
			}
			break
		}
	}
	return h
}

// operations are the states of a repository that git keeps in files of its git directory.
type operations struct {
	merge, rebase, cherryPick bool
	// rebaseCommit is the commit a rebase stopped at, empty when none or unknown.
	rebaseCommit string
}

// readOperations looks for the files that say a merge, a rebase or a cherry-pick is in progress (SPEC §10.30 "State": by their
// files, so no process is needed).
//
// Ported from VS Code: extensions/git/src/git.ts (getRebaseCommit, getMergeHead) and repository.ts (updateModelState)
func readOperations(gitDir string) operations {
	var ops operations
	exists := func(p string) bool { _, err := os.Stat(filepath.Join(gitDir, p)); return err == nil }
	ops.merge = exists("MERGE_HEAD")
	ops.cherryPick = exists("CHERRY_PICK_HEAD")
	switch {
	case exists("rebase-merge"):
		ops.rebase = true
		ops.rebaseCommit = firstLine(filepath.Join(gitDir, "rebase-merge", "stopped-sha"))
	case exists("rebase-apply"):
		ops.rebase = true
		ops.rebaseCommit = firstLine(filepath.Join(gitDir, "rebase-apply", "original-commit"))
	}
	if ops.rebase && ops.rebaseCommit == "" {
		ops.rebaseCommit = firstLine(filepath.Join(gitDir, "REBASE_HEAD"))
	}
	return ops
}

func firstLine(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	line, _, _ := strings.Cut(string(data), "\n")
	return strings.TrimSpace(line)
}

// stashCount counts the stashes by the lines of the reflog of refs/stash in the common git directory.
func stashCount(commonDir string) int {
	data, err := os.ReadFile(filepath.Join(commonDir, "logs", "refs", "stash"))
	if err != nil {
		return 0
	}
	return strings.Count(string(data), "\n")
}
