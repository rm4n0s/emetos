package git

import (
	"context"
	"regexp"
	"strings"

	"github.com/rm4n0s/errors"
)

// PullOptions say how Pull pulls.
type PullOptions struct {
	// Rebase rebases the local commits on the fetched ones instead of merging (-r).
	Rebase bool
	// Remote and Branch name what to pull ("origin", "main"); both empty pulls the upstream of the current branch. One of them alone is
	// ignored, as VS Code does.
	Remote, Branch string
	// Tags fetches every tag (--tags).
	Tags bool
	// Unshallow turns a shallow clone into a full one (--unshallow).
	Unshallow bool
	// AutoStash stashes the local changes before the pull and brings them back after it (--autostash; git 2.27 has it for merge and
	// rebase, and 2.30 is the oldest git Emetos runs).
	AutoStash bool
	// Progress, when set, gets the progress of the transfer (--progress).
	Progress func(Progress)
}

var reUpToDate = regexp.MustCompile(`(?i)Already up to date`)

// Pull runs git pull. It reports false when git said that everything was up to date. An error has the tags Conflict (a merge or
// rebase that stopped on conflicts), DirtyWorkTree (local changes in the way, with "Cannot pull with rebase, you have unstaged
// changes" as VS Code words it), NoUserNameConfigured, CantLockRef, CantRebaseMultipleBranches, TagConflict (tags that would be
// replaced; the names are in the error output), NoRemoteRepositorySpecified, RemoteConnectionError and the generic ones.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.pull)
func (r *Repo) Pull(ctx context.Context, o PullOptions) (changed bool, err error) {
	args := []string{"pull"}
	if o.Progress != nil {
		args = append(args, "--progress")
	}
	if o.Tags {
		args = append(args, "--tags")
	}
	if o.Unshallow {
		args = append(args, "--unshallow")
	}
	if o.AutoStash {
		args = append(args, "--autostash")
	}
	if o.Rebase {
		args = append(args, "-r")
	}
	if o.Remote != "" && o.Branch != "" {
		if err := remoteArg(o.Remote); err != nil {
			return false, err
		}
		if err := refName(o.Branch); err != nil {
			return false, err
		}
		args = append(args, "--", o.Remote, o.Branch)
	}
	res, err := r.write(ctx, ScopeRefs|ScopeIndex|ScopeWorkTree, Command{Args: args, Progress: o.Progress})
	if err != nil {
		return false, err
	}
	return !reUpToDate.Match(res.Stdout), nil
}

// ConflictingTags reads the names of the tags a pull refused to replace from its error output ("! [rejected]  v1 -> v1  (would
// clobber existing tag)").
//
// Ported from VS Code: extensions/git/src/repository.ts (handleTagConflict)
func ConflictingTags(stderr string) []string {
	var tags []string
	for _, m := range reTagClobberLine.FindAllStringSubmatch(stderr, -1) {
		tags = append(tags, m[1])
	}
	return tags
}

var reTagClobberLine = regexp.MustCompile(`(?m)^ ! \[rejected\]\s+(\S+)\s+->\s+(\S+)\s+\(would clobber existing tag\)\s*$`)

// MaybeRebased says that the upstream of the branch has commits with the same changes as commits of the branch ("git log --oneline
// --cherry <branch>...<branch>@{upstream}" starts with an equal commit, marked "="): the branch was probably rebased, and pulling merges the
// old commits and the new ones. A failure of the command (no upstream) is false.
//
// Ported from VS Code: extensions/git/src/repository.ts (checkIfMaybeRebased)
func (r *Repo) MaybeRebased(ctx context.Context, branch string) bool {
	if branch == "" {
		branch = "HEAD"
	}
	if refName(branch) != nil {
		return false
	}
	res, err := r.read(ctx, ScopeRefs, Command{Args: []string{"log", "--oneline", "--cherry", branch + "..." + branch + "@{upstream}", "--"}, AllowExit: []int{128}})
	if err != nil || res.ExitCode != 0 {
		return false
	}
	// VS Code looks at the start of the output only: the newest commit of either side is an equal one
	return strings.HasPrefix(string(res.Stdout), "=")
}

// remoteArg checks the name of a remote given to a command: it is not empty and does not start with a dash.
func remoteArg(name string) error {
	if name == "" || strings.HasPrefix(name, "-") {
		return errors.New("GitRemoteInvalid", "this is not the name of a remote", "name", name)
	}
	return nil
}
