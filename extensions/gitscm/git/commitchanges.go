package git

import (
	"context"
	"strings"

	"github.com/rm4n0s/errors"
)

// CommitChanges is what a commit changed: the commit, the tree it is compared with, and the files that differ.
type CommitChanges struct {
	// Commit is the commit.
	Commit Commit
	// Base is what the changes are against: the first parent of the commit, or the empty tree for a root commit (Root says which).
	Base string
	// Root says that the commit has no parent: Base is the empty tree.
	Root bool
	// Changes are the files that differ between Base and the commit, with git's rename detection.
	Changes []NameStatus
}

// CommitChanges reads the commit that id names and the files it changed against its first parent: a normal commit against its parent,
// a merge commit against its first parent, a root commit against the empty tree. A revision that starts with "-" would be read by git
// as an option and is refused with the tag GitRevisionInvalid; one that names no commit fails as git fails.
//
// Ported from VS Code: extensions/git/src/commands.ts (viewCommit) and git.ts (Repository.getEmptyTree, diffBetweenWithStats)
func (r *Repo) CommitChanges(ctx context.Context, id string) (CommitChanges, error) {
	if id == "" || strings.HasPrefix(id, "-") {
		return CommitChanges{}, errors.New("GitRevisionInvalid", "this is not a revision git accepts here", "revision", id)
	}
	commits, err := r.Log(ctx, LogOptions{Range: id, MaxEntries: 1})
	if err != nil {
		return CommitChanges{}, err
	}
	if len(commits) == 0 {
		return CommitChanges{}, errors.New("GitCommitNotFound", "there is no commit with this revision", "revision", id)
	}
	out := CommitChanges{Commit: commits[0]}
	if len(out.Commit.Parents) > 0 {
		out.Base = out.Commit.Parents[0]
	} else {
		out.Root = true
		if out.Base, err = r.EmptyTree(ctx); err != nil {
			return CommitChanges{}, err
		}
	}
	out.Changes, err = r.NameStatus(ctx, DiffSpec{Revs: []string{out.Base, out.Commit.Hash}, SimilarityThreshold: 50})
	if err != nil {
		return CommitChanges{}, err
	}
	return out, nil
}

// EmptyTree is the id of the empty tree of the repository's hash algorithm ("git hash-object -t tree" of nothing).
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.getEmptyTree)
func (r *Repo) EmptyTree(ctx context.Context) (string, error) {
	res, err := r.read(ctx, ScopeRefs, Command{Args: []string{"hash-object", "-t", "tree", "--stdin"}, Stdin: strings.NewReader("")})
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(string(res.Stdout))
	if !hashPattern.MatchString(id) {
		return "", errors.New("GitEmptyTreeInvalid", "git printed something that is not an object name for the empty tree", "output", id)
	}
	return id, nil
}
