package git

import (
	"context"
	"os"
	"path/filepath"

	"github.com/rm4n0s/errors"
)

// The operations that can stop on conflicts and the commands around them (SPEC §10.30 "Branches, tags, merge and the states in
// between"): merge, rebase, cherry-pick and their abort, continue and skip, the commit of a ref, and the three-way merge of a file's
// stages. The user asked for each of them, so none is a background command. They go through the Queue as writes of everything the
// operation touches.
//
// A command that stops on conflicts fails with the tag Conflict (merge, rebase) or CherryPickConflict, and leaves the repository in the
// state git keeps in its files (MERGE_HEAD, rebase-merge/, CHERRY_PICK_HEAD): the failure is how the caller learns that the user has
// conflicts to resolve, not that git broke.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.merge, mergeAbort, rebase, rebaseAbort, rebaseContinue, cherryPick,
// cherryPickAbort, mergeFile, getCommit).

// Operation is an operation that git can leave half done.
type Operation int

// The operations.
const (
	// OperationMerge is "git merge" (MERGE_HEAD).
	OperationMerge Operation = iota
	// OperationRebase is "git rebase" (rebase-merge/ or rebase-apply/).
	OperationRebase
	// OperationCherryPick is "git cherry-pick" (CHERRY_PICK_HEAD).
	OperationCherryPick
	// OperationRevert is "git revert" (REVERT_HEAD).
	OperationRevert
)

// Command is the name of the git command of the operation.
func (o Operation) Command() string {
	switch o {
	case OperationRebase:
		return "rebase"
	case OperationCherryPick:
		return "cherry-pick"
	case OperationRevert:
		return "revert"
	}
	return "merge"
}

// everything is what an operation that moves HEAD changes.
const everything = ScopeIndex | ScopeWorkTree | ScopeRefs

// Merge merges the ref into the current branch ("git merge <ref>"). A merge that has conflicts fails with the tag Conflict and stays in
// progress; one that local changes would be overwritten by fails with DirtyWorkTree.
func (r *Repo) Merge(ctx context.Context, ref string) error {
	if err := refName(ref); err != nil {
		return err
	}
	_, err := r.write(ctx, everything, Command{Args: []string{"merge", ref}})
	return err
}

// MergeAbort ends a merge that has conflicts and puts the work tree back as it was ("git merge --abort").
func (r *Repo) MergeAbort(ctx context.Context) error {
	_, err := r.write(ctx, everything, Command{Args: []string{"merge", "--abort"}})
	return err
}

// RebaseOptions are the options of Rebase.
type RebaseOptions struct {
	// Onto is the commit the commits are put on ("--onto"); the branch itself when empty.
	Onto string
	// RebaseMerges keeps the merge commits ("--rebase-merges").
	RebaseMerges bool
	// Interactive lets the user edit the list of commits first ("-i"): git starts the editor of the Runner's configuration (the
	// sequence editor, then GIT_EDITOR) with the file git-rebase-todo.
	Interactive bool
}

// Rebase puts the commits of the current branch on top of the branch ("git rebase [-i] [--rebase-merges] [--onto <commit>] <branch>").
// A rebase that stops on a conflict fails with the tag Conflict and stays in progress; one that cannot start because of local changes
// fails with DirtyWorkTree; a branch with several heads fails with CantRebaseMultipleBranches.
func (r *Repo) Rebase(ctx context.Context, branch string, o RebaseOptions) error {
	if err := refName(branch); err != nil {
		return err
	}
	args := []string{"rebase"}
	if o.Interactive {
		args = append(args, "-i")
	}
	if o.RebaseMerges {
		args = append(args, "--rebase-merges")
	}
	if o.Onto != "" {
		if err := refName(o.Onto); err != nil {
			return err
		}
		args = append(args, "--onto", o.Onto)
	}
	args = append(args, branch)
	_, err := r.write(ctx, everything, Command{Args: args})
	return err
}

// RebaseAbort ends a rebase and puts the branch back as it was ("git rebase --abort").
func (r *Repo) RebaseAbort(ctx context.Context) error {
	_, err := r.write(ctx, everything, Command{Args: []string{"rebase", "--abort"}})
	return err
}

// RebaseSkip leaves out the commit the rebase stopped at ("git rebase --skip").
func (r *Repo) RebaseSkip(ctx context.Context) error {
	_, err := r.write(ctx, everything, Command{Args: []string{"rebase", "--skip"}})
	return err
}

// CherryPick applies the changes of a commit on the current branch and commits them ("git cherry-pick <commit>"). A pick that has
// conflicts fails with the tag CherryPickConflict and stays in progress. A pick that leaves nothing to commit (the changes are in the
// branch already) is ended with "git cherry-pick --abort", as VS Code does, and fails with CherryPickEmpty.
func (r *Repo) CherryPick(ctx context.Context, commit string) error {
	if err := refName(commit); err != nil {
		return err
	}
	_, err := r.write(ctx, everything, Command{Args: []string{"cherry-pick", commit}})
	if err == nil {
		return nil
	}
	if appErr, ok := errors.FromError(err); ok && appErr.Tag == "CherryPickEmpty" {
		if abortErr := r.CherryPickAbort(ctx); abortErr != nil {
			return abortErr
		}
	}
	return err
}

// CherryPickAbort ends a cherry-pick that has conflicts ("git cherry-pick --abort").
func (r *Repo) CherryPickAbort(ctx context.Context) error {
	_, err := r.write(ctx, everything, Command{Args: []string{"cherry-pick", "--abort"}})
	return err
}

// Continue finishes the operation after its conflicts were resolved and staged ("git <command> --continue"), with the message the
// operation prepared and no editor (GIT_EDITOR=true). A rebase goes on to its next commit and may stop again on a conflict (the tag
// Conflict); files that are still unmerged fail with UnmergedChanges.
func (r *Repo) Continue(ctx context.Context, op Operation) error {
	_, err := r.write(ctx, everything, Command{Args: []string{op.Command(), "--continue"}, Env: []string{"GIT_EDITOR=true"}})
	return err
}

// CommitAt is the commit that rev names, with its message and parents. The error has the tag GitNoCommit when rev names none.
func (r *Repo) CommitAt(ctx context.Context, rev string) (Commit, error) {
	if err := refName(rev); err != nil {
		return Commit{}, err
	}
	commits, err := r.Log(ctx, LogOptions{Range: rev, MaxEntries: 1})
	if err != nil {
		return Commit{}, err
	}
	if len(commits) == 0 {
		return Commit{}, errors.New("GitNoCommit", "the revision does not name a commit", "rev", rev, "root", r.Root)
	}
	return commits[0], nil
}

// MergeFileOptions are the options of MergeFile.
type MergeFileOptions struct {
	// Diff3 puts the base between the two sides in the markers ("--diff3"); the others have "--no-diff3".
	Diff3 bool
	// Ours, Base and Theirs label the sections of the markers ("-L"): "HEAD", "base" and the name of the other side when empty.
	Ours, Base, Theirs string
}

// MergeFileResult is the file git merge-file made.
type MergeFileResult struct {
	// Text is the merged file, with conflict markers where the sides disagree.
	Text string
	// Conflicts is the number of conflicts in Text; 0 when the merge was clean.
	Conflicts int
}

// maxMergeFileConflicts is the largest exit status that "git merge-file" uses for a number of conflicts (it caps the number at 127).
const maxMergeFileConflicts = 127

// MergeFile does the three-way merge of the stages of a conflicted file again ("git merge-file -p"): stage 2 (ours), stage 1 (the
// base) and stage 3 (theirs) of the index, written to a temporary folder that is removed afterwards. A file that was added on both sides
// has no base and merges against an empty one, as git does. A stage that is missing on a side that matters (a file deleted on one side)
// fails with the tag MergeStageMissing. path is absolute or relative to the root.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.mergeFile)
func (r *Repo) MergeFile(ctx context.Context, path string, o MergeFileOptions) (MergeFileResult, error) {
	rel, err := r.relPaths([]string{path}, false)
	if err != nil {
		return MergeFileResult{}, err
	}
	entries, err := r.LsFiles(ctx, rel[0])
	if err != nil {
		return MergeFileResult{}, err
	}
	has := map[string]bool{}
	for _, e := range entries {
		has[e.Stage] = true
	}
	if !has["2"] || !has["3"] {
		return MergeFileResult{}, errors.New("MergeStageMissing", "the file does not have both sides of a conflict", "path", rel[0], "stages", len(has))
	}
	stage := func(n string) ([]byte, error) {
		if !has[n] {
			return nil, nil // no base: the file was added on both sides
		}
		return r.ReadBlob(ctx, ":"+n+":"+rel[0])
	}
	var blobs [3][]byte
	for i, n := range []string{"2", "1", "3"} {
		if blobs[i], err = stage(n); err != nil {
			return MergeFileResult{}, err
		}
	}
	dir, err := os.MkdirTemp("", "emetos-merge-file-")
	if err != nil {
		return MergeFileResult{}, errors.NewErr("MergeFileTempFailed", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	var paths [3]string
	for i, name := range []string{"ours", "base", "theirs"} {
		paths[i] = filepath.Join(dir, name)
		if err := os.WriteFile(paths[i], blobs[i], 0o600); err != nil {
			return MergeFileResult{}, errors.NewErr("MergeFileTempFailed", err).SetMetadata("path", paths[i])
		}
	}
	label := func(s, def string) string {
		if s == "" {
			return def
		}
		return s
	}
	args := []string{"merge-file", "-p",
		"-L", label(o.Ours, "HEAD"), "-L", label(o.Base, "base"), "-L", label(o.Theirs, "theirs")}
	if o.Diff3 {
		args = append(args, "--diff3")
	} else {
		args = append(args, "--no-diff3")
	}
	args = append(args, paths[0], paths[1], paths[2])
	allow := make([]int, maxMergeFileConflicts)
	for i := range allow {
		allow[i] = i + 1
	}
	res, err := r.read(ctx, ScopeIndex, Command{Args: args, AllowExit: allow})
	if err != nil {
		return MergeFileResult{}, err
	}
	return MergeFileResult{Text: string(res.Stdout), Conflicts: res.ExitCode}, nil
}
