package git

import (
	"context"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/errors"
)

// StashFormat is the --format of "git stash list -z" that ParseStashes reads: hash, parents, "stash@{n}", the reflog subject,
// author date, commit date.
const StashFormat = "%H%n%P%n%gd%n%gs%n%at%n%ct"

// Stash is an entry of the stash.
type Stash struct {
	Hash    string
	Parents []string
	// Index is the n of stash@{n}.
	Index int
	// Branch is the branch the stash was made on.
	Branch string
	// Description is the stash's message; "WIP (message)" for a stash made without one.
	Description string
	AuthorDate  time.Time
	CommitDate  time.Time
}

var stashSubject = regexp.MustCompile(`(?i)^(WIP\s)?on\s([^:]+):\s(.*)$`)

// ParseStashes parses the output of "git stash list -z --format=StashFormat". A record that is cut short is dropped.
//
// Ported from VS Code: extensions/git/src/git.ts (parseGitStashes).
func ParseStashes(data []byte) []Stash {
	var out []Stash
	for _, rec := range terminated(string(data)) {
		f := strings.Split(strings.TrimLeft(rec, "\n"), "\n")
		if len(f) != 6 || !hashPattern.MatchString(f[0]) {
			continue
		}
		ref := strings.TrimSuffix(strings.TrimPrefix(f[2], "stash@{"), "}")
		index, err := strconv.Atoi(ref)
		if err != nil || !strings.HasPrefix(f[2], "stash@{") {
			continue
		}
		s := Stash{Hash: f[0], Index: index, AuthorDate: time.Unix(int64(atoi(f[4])), 0).UTC(), CommitDate: time.Unix(int64(atoi(f[5])), 0).UTC()}
		if f[1] != "" {
			s.Parents = strings.Split(f[1], " ")
		}
		if m := stashSubject.FindStringSubmatch(f[3]); m != nil {
			s.Branch = strings.TrimSpace(m[2])
			s.Description = strings.TrimSpace(m[3])
			if m[1] != "" {
				s.Description = "WIP (" + s.Description + ")"
			}
		} else {
			s.Description = f[3]
		}
		out = append(out, s)
	}
	return out
}

// StashLatest as the index of a stash command means the newest stash: no stash@{n} is given to git.
const StashLatest = -1

// StashOptions are the options of Stash.
type StashOptions struct {
	// Message names the stash ("git stash push -m"); empty gives git's own "WIP on <branch>: <commit> <subject>".
	Message string
	// IncludeUntracked also stashes the untracked files ("-u").
	IncludeUntracked bool
	// Staged stashes only what is in the index ("-S", git 2.35 or newer).
	Staged bool
}

// Stash puts the changes aside ("git stash push [-u] [-S] [-m message]"). A repository with nothing to stash fails with the tag
// NoLocalChanges, and one before its first commit with git's own refusal.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.createStash)
func (r *Repo) Stash(ctx context.Context, o StashOptions) error {
	args := []string{"stash", "push"}
	if o.IncludeUntracked {
		args = append(args, "-u")
	}
	if o.Staged {
		args = append(args, "-S")
	}
	if o.Message != "" {
		args = append(args, "-m", o.Message)
	}
	_, err := r.write(ctx, ScopeAll, Command{Args: args})
	return err
}

// StashApplyOptions are the options of StashApply and StashPop.
type StashApplyOptions struct {
	// ReinstateIndex puts back what was staged, not only the changes ("--index").
	ReinstateIndex bool
}

// StashPop applies a stash and drops it ("git stash pop [--index] [stash@{n}]"); StashLatest is the newest. A stash that conflicts
// with the work tree fails with the tag StashConflict and is kept, a missing one with NoStashFound, and files that would be
// overwritten with LocalChangesOverwritten.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.popStash)
func (r *Repo) StashPop(ctx context.Context, index int, o StashApplyOptions) error {
	return r.popOrApply(ctx, "pop", index, o)
}

// StashApply applies a stash and keeps it ("git stash apply [--index] [stash@{n}]"), failing as StashPop does.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.applyStash)
func (r *Repo) StashApply(ctx context.Context, index int, o StashApplyOptions) error {
	return r.popOrApply(ctx, "apply", index, o)
}

func (r *Repo) popOrApply(ctx context.Context, verb string, index int, o StashApplyOptions) error {
	args := []string{"stash", verb}
	if o.ReinstateIndex {
		args = append(args, "--index")
	}
	if index >= 0 {
		args = append(args, stashRef(index))
	}
	_, err := r.write(ctx, ScopeAll, Command{Args: args})
	return err
}

// StashDrop removes the stash with the index ("git stash drop stash@{n}"); StashLatest is the newest. A missing one fails with the tag
// NoStashFound.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.dropStash)
func (r *Repo) StashDrop(ctx context.Context, index int) error {
	args := []string{"stash", "drop"}
	if index >= 0 {
		args = append(args, stashRef(index))
	}
	_, err := r.write(ctx, ScopeStash, Command{Args: args})
	return err
}

// StashClear removes every stash ("git stash clear").
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.dropStash without an index)
func (r *Repo) StashClear(ctx context.Context) error {
	_, err := r.write(ctx, ScopeStash, Command{Args: []string{"stash", "clear"}})
	return err
}

func stashRef(index int) string { return "stash@{" + strconv.Itoa(index) + "}" }

// StashChange is a file a stash changed.
type StashChange struct {
	NameStatus
	// Untracked says that the file was untracked when the stash was made: it is in the commit of the untracked files (the third
	// parent of the stash), not in the stash itself.
	Untracked bool
}

// StashChanges is what a stash holds: the files it changed against the commit it was made on, and the untracked files it saved.
type StashChanges struct {
	// Stash is the stash.
	Stash Stash
	// Base is the commit the stash was made on (its first parent): what the changes are against.
	Base string
	// Untracked is the commit that holds the untracked files (the third parent of the stash), empty when the stash has none.
	Untracked string
	// Changes are the files, tracked ones first with git's rename detection, then the untracked ones as added files.
	Changes []StashChange
}

// StashChanges reads the stash stash@{index}: "git diff --name-status" of the commit it was made on against the stash for the tracked
// files, and the files of the commit of the untracked ones. It fails with the tag GitStashMissing when there is no such stash.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.showStash) and commands.ts (_viewStash)
func (r *Repo) StashChanges(ctx context.Context, index int) (StashChanges, error) {
	stashes, err := r.Stashes(ctx)
	if err != nil {
		return StashChanges{}, err
	}
	for _, s := range stashes {
		if s.Index == index {
			return r.StashChangesOf(ctx, s)
		}
	}
	return StashChanges{}, errors.New("GitStashMissing", "there is no stash with this index", "index", index)
}

// StashChangesAt reads what the stash commit with this hash holds, as StashChanges does: the stash need not be in the list any more as
// long as git still has its commits (git keeps the commit of a dropped stash for a while), which is how a stash tab keeps showing a
// stash that was popped. The error has the tag GitStashHashInvalid for something that is not a full object name, and GitCommitNotFound
// for a commit git does not have.
func (r *Repo) StashChangesAt(ctx context.Context, hash string) (StashChanges, error) {
	if !hashPattern.MatchString(hash) {
		return StashChanges{}, errors.New("GitStashHashInvalid", "a stash is named by the full hash of its commit", "hash", hash)
	}
	commits, err := r.Log(ctx, LogOptions{Range: hash, MaxEntries: 1})
	if err != nil {
		return StashChanges{}, err
	}
	if len(commits) == 0 {
		return StashChanges{}, errors.New("GitCommitNotFound", "there is no commit with this hash", "hash", hash)
	}
	return r.StashChangesOf(ctx, Stash{Hash: commits[0].Hash, Parents: commits[0].Parents, CommitDate: commits[0].CommitDate, AuthorDate: commits[0].AuthorDate})
}

// StashChangesOf reads what a stash holds.
func (r *Repo) StashChangesOf(ctx context.Context, s Stash) (StashChanges, error) {
	out := StashChanges{Stash: s, Base: s.Hash + "^"}
	if len(s.Parents) > 0 {
		out.Base = s.Parents[0]
	}
	changes, err := r.NameStatus(ctx, DiffSpec{Revs: []string{out.Base, s.Hash}, SimilarityThreshold: 50})
	if err != nil {
		return StashChanges{}, err
	}
	for _, c := range changes {
		out.Changes = append(out.Changes, StashChange{NameStatus: c})
	}
	if len(s.Parents) == 3 {
		out.Untracked = s.Parents[2]
		entries, err := r.LsTree(ctx, out.Untracked, "", true)
		if err != nil {
			return StashChanges{}, err
		}
		for _, e := range entries {
			out.Changes = append(out.Changes, StashChange{NameStatus: NameStatus{Status: 'A', Path: e.Path}, Untracked: true})
		}
	}
	return out, nil
}
