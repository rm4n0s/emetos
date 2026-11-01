package git

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

// The messages of git that tell what went wrong, as VS Code's GitErrorCodes read them (extensions/git/src/git.ts,
// getGitErrorCode and the catch blocks of the commands) plus the messages of the git of 2023 to 2026, which differ from the
// older ones in their case and wording (checked against a real git in the tests, where the failure can be provoked).
var (
	reLocked = regexp.MustCompile(`Another git process seems to be running in this repository|If no other git process is currently running`)
	// reAuthFailed also reads the failure of a prompt: the askpass program that answers with an exit status (the user pressed Escape in the
	// box) makes git say "could not read Password for 'https://host': terminal prompts disabled" and no more, and the same when the
	// integrated askpass is off and git may not ask on a terminal.
	reAuthFailed       = regexp.MustCompile(`(?i)Authentication failed|could not read (?:Username|Password) for`)
	reNotARepository   = regexp.MustCompile(`(?i)Not a git repository`)
	reCantCreatePipe   = regexp.MustCompile(`cannot make pipe for command substitution|cannot create standard input pipe`)
	reRepoNotFound     = regexp.MustCompile(`(?i)Repository not found|repository '[^']*' (?:not found|does not exist)`)
	reCantAccessRemote = regexp.MustCompile(`unable to access`)
	reNotFullyMerged   = regexp.MustCompile(`branch '.+' is not fully merged`)
	reNoRemoteRef      = regexp.MustCompile(`(?i)Couldn't find remote ref`)
	reBranchExists     = regexp.MustCompile(`(?i)A branch named '.+' already exists`)
	reInvalidBranch    = regexp.MustCompile(`'.+' is not a valid branch name`)
	reDirtyWorkTree    = regexp.MustCompile(`Please,? commit your changes or stash them`)
	reUnsafe           = regexp.MustCompile(`detected dubious ownership in repository at`)
	reNoRemoteSpecific = regexp.MustCompile(`No remote repository specified\.`)
	reRemoteConnection = regexp.MustCompile(`Could not read from remote repository`)

	reNoUserName  = regexp.MustCompile(`unable to auto-detect name|no name was given|empty ident name`)
	reNoUserEmail = regexp.MustCompile(`unable to auto-detect email address|no email was given`)
	reWhoAreYou   = regexp.MustCompile(`Please tell me who you are|Author identity unknown`)

	reUnmergedFiles     = regexp.MustCompile(`not possible because you have unmerged files`)
	reEmptyMessage      = regexp.MustCompile(`Aborting commit due to empty commit message`)
	reBranchUnborn      = regexp.MustCompile(`You are on a branch yet to be born`)
	reMergeConflict     = regexp.MustCompile(`(?m)^CONFLICT `)
	rePullConflict      = regexp.MustCompile(`(?m)^CONFLICT \([^)]+\): \b`)
	rePullDirty         = regexp.MustCompile(`(?i)Pull(?:ing)? is not possible because you have unmerged files|Cannot pull with rebase: You have unstaged changes|Your local changes to the following files would be overwritten|Please, commit your changes before you can merge`)
	reCantLockRef       = regexp.MustCompile(`(?i)cannot lock ref|unable to update local ref`)
	reRebaseMultiple    = regexp.MustCompile(`(?i)cannot rebase onto multiple branches`)
	reTagClobber        = regexp.MustCompile(`(?m)! \[rejected\].*\(would clobber existing tag\)`)
	rePushRejected      = regexp.MustCompile(`(?m)^error: failed to push some refs to\b`)
	rePermissionDenied  = regexp.MustCompile(`Permission.*denied`)
	reLocalRejected     = regexp.MustCompile(`(?m)^ ! \[rejected\]`)
	reRemoteRejected    = regexp.MustCompile(`(?m)^ ! \[remote rejected\]`)
	reStaleInfo         = regexp.MustCompile(`(?m)! \[rejected\].*\(stale info\)`)
	reRemoteUpdated     = regexp.MustCompile(`(?m)! \[rejected\].*\(remote ref updated since checkout\)`)
	reFetchNonFF        = regexp.MustCompile(`(?m)! \[rejected\].*\(non-fast-forward\)`)
	reNoUpstream        = regexp.MustCompile(`(?m)^fatal: The current branch .* has no upstream branch`)
	reNoStash           = regexp.MustCompile(`No stash (?:found|entries found)|is not a valid reference`)
	reLocalChanges      = regexp.MustCompile(`error: Your local changes to the following files would be overwritten`)
	reStashConflict     = regexp.MustCompile(`(?m)^CONFLICT`)
	rePatchDoesntApply  = regexp.MustCompile(`patch does not apply`)
	reNoSuchPath        = regexp.MustCompile(`(?m)^fatal: no such path`)
	reWrongCase         = regexp.MustCompile(`exists on disk, but not in`)
	reCantOpen          = regexp.MustCompile(`does not exist in|does not exist \(neither on disk nor in the index\)|is in the index, but not at stage|Not a valid object name|invalid object name|bad object`)
	reInSubmodule       = regexp.MustCompile(` is in submodule `)
	reNoLocalChanges    = regexp.MustCompile(`No local changes to save`)
	reMustEditConflicts = regexp.MustCompile(`You must edit all merge conflicts`)
	reRebaseDirty       = regexp.MustCompile(`(?i)cannot rebase: (?:You have unstaged changes|Your index contains uncommitted changes)`)
	reCherryPickEmpty   = regexp.MustCompile(`The previous cherry-pick is now empty`)
	reCouldNotApply     = regexp.MustCompile(`(?m)^error: (?:could not apply|could not revert)\b`)
)

// classify turns the failure of a git command into an error whose Tag is one of VS Code's GitErrorCodes (SPEC §10.30
// "Errors"), or GitCommandFailed when git printed nothing that is known. The rules of a command come first, as VS Code's
// catch blocks overwrite the generic code of getGitErrorCode; the generic rules follow. Every error carries the arguments
// (with the userinfo of URLs masked), the exit status and the last 2 KiB of the error output as metadata. Each Tag is made
// here, where the failure is seen.
//
// Ported from VS Code: extensions/git/src/git.ts (getGitErrorCode and the gitErrorCode assignments of Repository).
func classify(name string, args []string, exit int, stdout, stderr string) error {
	a := maskArgs(args)
	tail := MaskURLs(lastBytes(stderr, StderrKept))
	msg := failureMessage(name, exit, stderr)

	switch name {
	case "checkout", "switch":
		if reDirtyWorkTree.MatchString(stderr) {
			return errors.New("DirtyWorkTree", msg, "args", a, "exitCode", exit, "stderr", tail)
		}
		if reBranchUnborn.MatchString(stderr) {
			return errors.New("BranchNotYetBorn", msg, "args", a, "exitCode", exit, "stderr", tail)
		}
	case "commit":
		if reUnmergedFiles.MatchString(stderr) {
			return errors.New("UnmergedChanges", msg, "args", a, "exitCode", exit, "stderr", tail)
		}
		if reEmptyMessage.MatchString(stderr) {
			return errors.New("EmptyCommitMessage", msg, "args", a, "exitCode", exit, "stderr", tail)
		}
	case "merge":
		if reMergeConflict.MatchString(stdout) {
			return errors.New("Conflict", msg, "args", a, "exitCode", exit, "stderr", tail)
		}
		if reUnmergedFiles.MatchString(stderr) {
			return errors.New("UnmergedChanges", msg, "args", a, "exitCode", exit, "stderr", tail)
		}
	case "fetch":
		if reNoRemoteSpecific.MatchString(stderr) {
			return errors.New("NoRemoteRepositorySpecified", msg, "args", a, "exitCode", exit, "stderr", tail)
		}
		if reRemoteConnection.MatchString(stderr) && !reRepoNotFound.MatchString(stderr) && !reCantAccessRemote.MatchString(stderr) && !reAuthFailed.MatchString(stderr) {
			return errors.New("RemoteConnectionError", msg, "args", a, "exitCode", exit, "stderr", tail)
		}
		// the local branch has commits of its own and cannot be fast-forwarded to the remote's (fetch origin main:main)
		if reFetchNonFF.MatchString(stderr) {
			return errors.New("BranchFastForwardRejected", msg, "args", a, "exitCode", exit, "stderr", tail)
		}
	case "pull":
		switch {
		case rePullConflict.MatchString(stdout):
			return errors.New("Conflict", msg, "args", a, "exitCode", exit, "stderr", tail)
		case reWhoAreYou.MatchString(stderr):
			return errors.New("NoUserNameConfigured", msg, "args", a, "exitCode", exit, "stderr", tail)
		case reRemoteConnection.MatchString(stderr) && !reAuthFailed.MatchString(stderr) && !reCantAccessRemote.MatchString(stderr):
			return errors.New("RemoteConnectionError", msg, "args", a, "exitCode", exit, "stderr", tail)
		case rePullDirty.MatchString(stderr):
			return errors.New("DirtyWorkTree", msg, "args", a, "exitCode", exit, "stderr", tail)
		case reCantLockRef.MatchString(stderr):
			return errors.New("CantLockRef", msg, "args", a, "exitCode", exit, "stderr", tail)
		case reRebaseMultiple.MatchString(stderr):
			return errors.New("CantRebaseMultipleBranches", msg, "args", a, "exitCode", exit, "stderr", tail)
		case reTagClobber.MatchString(stderr):
			return errors.New("TagConflict", msg, "args", a, "exitCode", exit, "stderr", tail)
		case reNoRemoteSpecific.MatchString(stderr):
			return errors.New("NoRemoteRepositorySpecified", msg, "args", a, "exitCode", exit, "stderr", tail)
		}
	case "rebase":
		if rePullConflict.MatchString(stdout) {
			return errors.New("Conflict", msg, "args", a, "exitCode", exit, "stderr", tail)
		}
		if reRebaseMultiple.MatchString(stderr) {
			return errors.New("CantRebaseMultipleBranches", msg, "args", a, "exitCode", exit, "stderr", tail)
		}
		if reUnmergedFiles.MatchString(stderr) || reMustEditConflicts.MatchString(stderr) || reMustEditConflicts.MatchString(stdout) {
			return errors.New("UnmergedChanges", msg, "args", a, "exitCode", exit, "stderr", tail)
		}
		if reRebaseDirty.MatchString(stderr) {
			return errors.New("DirtyWorkTree", msg, "args", a, "exitCode", exit, "stderr", tail)
		}
	case "cherry-pick":
		// VS Code calls every failure of a cherry-pick a conflict; a bad hash is not one (no command of Emetos runs git revert)
		switch {
		case reCherryPickEmpty.MatchString(stderr):
			return errors.New("CherryPickEmpty", msg, "args", a, "exitCode", exit, "stderr", tail)
		case reMergeConflict.MatchString(stdout) || reCouldNotApply.MatchString(stderr):
			return errors.New("CherryPickConflict", msg, "args", a, "exitCode", exit, "stderr", tail)
		case reUnmergedFiles.MatchString(stderr):
			return errors.New("UnmergedChanges", msg, "args", a, "exitCode", exit, "stderr", tail)
		}
	case "push":
		switch {
		case reRemoteRejected.MatchString(stderr) && !reLocalRejected.MatchString(stderr):
			// the server refused the update (a protected branch, a hook, a permission): it says why in the lines that start with "remote:",
			// and pulling does not help
			return errors.New("RemoteRejected", msg, "args", a, "exitCode", exit, "stderr", tail)
		case rePushRejected.MatchString(stderr):
			// a push that was asked to take the remote's state into account says which of its two checks refused it
			switch {
			case hasArg(args, "--force-with-lease") && reStaleInfo.MatchString(stderr):
				return errors.New("ForcePushWithLeaseRejected", msg, "args", a, "exitCode", exit, "stderr", tail)
			case hasArg(args, "--force-if-includes") && reRemoteUpdated.MatchString(stderr):
				return errors.New("ForcePushWithLeaseIfIncludesRejected", msg, "args", a, "exitCode", exit, "stderr", tail)
			}
			return errors.New("PushRejected", msg, "args", a, "exitCode", exit, "stderr", tail)
		case rePermissionDenied.MatchString(stderr):
			return errors.New("PermissionDenied", msg, "args", a, "exitCode", exit, "stderr", tail)
		case reRemoteConnection.MatchString(stderr) && !reAuthFailed.MatchString(stderr) && !reCantAccessRemote.MatchString(stderr):
			return errors.New("RemoteConnectionError", msg, "args", a, "exitCode", exit, "stderr", tail)
		case reNoUpstream.MatchString(stderr):
			return errors.New("NoUpstreamBranch", msg, "args", a, "exitCode", exit, "stderr", tail)
		}
	case "stash":
		switch {
		case reNoStash.MatchString(stderr):
			return errors.New("NoStashFound", msg, "args", a, "exitCode", exit, "stderr", tail)
		case reLocalChanges.MatchString(stderr):
			return errors.New("LocalChangesOverwritten", msg, "args", a, "exitCode", exit, "stderr", tail)
		case reStashConflict.MatchString(stdout):
			return errors.New("StashConflict", msg, "args", a, "exitCode", exit, "stderr", tail)
		}
	case "apply":
		if rePatchDoesntApply.MatchString(stderr) {
			return errors.New("PatchDoesNotApply", msg, "args", a, "exitCode", exit, "stderr", tail)
		}
	case "blame":
		if reNoSuchPath.MatchString(stderr) {
			return errors.New("NoPathFound", msg, "args", a, "exitCode", exit, "stderr", tail)
		}
	case "show", "cat-file":
		if reWrongCase.MatchString(stderr) {
			return errors.New("WrongCase", msg, "args", a, "exitCode", exit, "stderr", tail)
		}
		if reCantOpen.MatchString(stderr) {
			return errors.New("CantOpenResource", msg, "args", a, "exitCode", exit, "stderr", tail)
		}
	case "check-ignore":
		if reInSubmodule.MatchString(stderr) {
			return errors.New("IsInSubmodule", msg, "args", a, "exitCode", exit, "stderr", tail)
		}
	}

	// The identity that a commit needs (commit, merge, rebase, pull, stash, tag, cherry-pick all create commits).
	switch {
	case reNoUserName.MatchString(stderr):
		return errors.New("NoUserNameConfigured", msg, "args", a, "exitCode", exit, "stderr", tail)
	case reNoUserEmail.MatchString(stderr):
		return errors.New("NoUserEmailConfigured", msg, "args", a, "exitCode", exit, "stderr", tail)
	case reWhoAreYou.MatchString(stderr):
		return errors.New("NoUserNameConfigured", msg, "args", a, "exitCode", exit, "stderr", tail)
	}

	// The generic rules of VS Code's getGitErrorCode, in its order.
	switch {
	case reLocked.MatchString(stderr):
		return errors.New("RepositoryIsLocked", msg, "args", a, "exitCode", exit, "stderr", tail)
	case reAuthFailed.MatchString(stderr):
		return errors.New("AuthenticationFailed", msg, "args", a, "exitCode", exit, "stderr", tail)
	case reNotARepository.MatchString(stderr):
		return errors.New("NotAGitRepository", msg, "args", a, "exitCode", exit, "stderr", tail)
	case reCantCreatePipe.MatchString(stderr):
		return errors.New("CantCreatePipe", msg, "args", a, "exitCode", exit, "stderr", tail)
	case reRepoNotFound.MatchString(stderr):
		return errors.New("RepositoryNotFound", msg, "args", a, "exitCode", exit, "stderr", tail)
	case reCantAccessRemote.MatchString(stderr):
		return errors.New("CantAccessRemote", msg, "args", a, "exitCode", exit, "stderr", tail)
	case reNotFullyMerged.MatchString(stderr):
		return errors.New("BranchNotFullyMerged", msg, "args", a, "exitCode", exit, "stderr", tail)
	case reNoRemoteRef.MatchString(stderr):
		return errors.New("NoRemoteReference", msg, "args", a, "exitCode", exit, "stderr", tail)
	case reBranchExists.MatchString(stderr):
		return errors.New("BranchAlreadyExists", msg, "args", a, "exitCode", exit, "stderr", tail)
	case reInvalidBranch.MatchString(stderr):
		return errors.New("InvalidBranchName", msg, "args", a, "exitCode", exit, "stderr", tail)
	case reDirtyWorkTree.MatchString(stderr):
		return errors.New("DirtyWorkTree", msg, "args", a, "exitCode", exit, "stderr", tail)
	case reUnsafe.MatchString(stderr):
		return errors.New("UnsafeRepository", msg, "args", a, "exitCode", exit, "stderr", tail)
	case reRemoteConnection.MatchString(stderr):
		return errors.New("RemoteConnectionError", msg, "args", a, "exitCode", exit, "stderr", tail)
	}
	return errors.New("GitCommandFailed", msg, "args", a, "exitCode", exit, "stderr", tail)
}

// classifySuccess looks at the output of a command that exited with status 0 for the one outcome that VS Code treats as an
// error: "git stash push" with nothing to stash prints "No local changes to save" (git exits 0; VS Code looks for it in the
// error output of a failed command, which a git of today does not produce) and creates no stash. Everything else is nil.
func classifySuccess(name string, args []string, stdout, stderr string) error {
	if name != "stash" {
		return nil
	}
	if !reNoLocalChanges.MatchString(stdout) && !reNoLocalChanges.MatchString(stderr) {
		return nil
	}
	return errors.New("NoLocalChanges", "there are no local changes to save", "args", maskArgs(args), "exitCode", 0,
		"stderr", MaskURLs(lastBytes(stderr, StderrKept)))
}

// failureMessage is the message of a failure: the command, the exit status and the last line git printed.
func failureMessage(name string, exit int, stderr string) string {
	msg := "git " + name + " failed"
	if exit > 0 {
		msg += " with exit status " + strconv.Itoa(exit)
	} else if exit < 0 {
		msg += " (ended by a signal)"
	}
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	if last := strings.TrimSpace(lines[len(lines)-1]); last != "" {
		msg += ": " + MaskURLs(last)
	}
	return msg
}

// hasArg says that a command line has the option, also in the form "--force-with-lease=refs/heads/main:abc".
func hasArg(args []string, option string) bool {
	for _, a := range args {
		if a == option || strings.HasPrefix(a, option+"=") {
			return true
		}
	}
	return false
}
