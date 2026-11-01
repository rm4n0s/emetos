package git

import (
	"context"
	"strings"

	"github.com/rm4n0s/errors"
)

// ForcePushMode says how a push overwrites the remote's branch.
type ForcePushMode int

// The modes of a force push.
const (
	// NoForce is a push that git refuses when the remote has commits the branch lacks.
	NoForce ForcePushMode = iota
	// ForceWithLease is --force-with-lease: the push goes through when the remote-tracking branch is where the remote's branch is,
	// that is, nobody pushed since the last fetch.
	ForceWithLease
	// ForceWithLeaseIfIncludes is --force-with-lease with --force-if-includes: also refused when the remote-tracking branch has commits
	// the local branch does not include.
	ForceWithLeaseIfIncludes
	// ForceAlways is --force: the remote's branch is replaced whatever it has.
	ForceAlways
)

// PushOptions say what Push pushes.
type PushOptions struct {
	// Remote and Refspec name the target ("origin", "main" or "main:main"); both empty pushes the current branch to its upstream (git
	// push, which fails with NoUpstreamBranch when it has none).
	Remote, Refspec string
	// SetUpstream makes the branch track the remote's (-u).
	SetUpstream bool
	// FollowTags pushes the annotated tags that point at the pushed commits (--follow-tags).
	FollowTags bool
	// Tags pushes every tag (--tags).
	Tags bool
	// Force is the force mode.
	Force ForcePushMode
	// Progress, when set, gets the progress of the transfer (--progress).
	Progress func(Progress)
}

// Push runs git push. An error has the tags PushRejected, ForcePushWithLeaseRejected and ForcePushWithLeaseIfIncludesRejected (for the
// force modes that say why), PermissionDenied, RemoteConnectionError, NoUpstreamBranch and the generic ones; the remote may also refuse
// a push that the branch protection of the server does not allow, with the message it sends (the last line of the error output).
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.push)
func (r *Repo) Push(ctx context.Context, o PushOptions) error {
	args := []string{"push"}
	if o.Progress != nil {
		args = append(args, "--progress")
	}
	switch o.Force {
	case ForceWithLease, ForceWithLeaseIfIncludes:
		args = append(args, "--force-with-lease")
		if o.Force == ForceWithLeaseIfIncludes {
			args = append(args, "--force-if-includes")
		}
	case ForceAlways:
		args = append(args, "--force")
	}
	if o.SetUpstream {
		args = append(args, "-u")
	}
	if o.FollowTags {
		args = append(args, "--follow-tags")
	}
	if o.Tags {
		args = append(args, "--tags")
	}
	if o.Remote != "" || o.Refspec != "" {
		args = append(args, "--")
		if o.Remote != "" {
			if err := remoteArg(o.Remote); err != nil {
				return err
			}
			args = append(args, o.Remote)
		}
		if o.Refspec != "" {
			if err := refName(o.Refspec); err != nil {
				return err
			}
			args = append(args, o.Refspec)
		}
	}
	_, err := r.write(ctx, ScopeRefs, Command{Args: args, Progress: o.Progress})
	return err
}

// SetUpstream makes a local branch track a remote-tracking branch ("git branch --set-upstream-to=<upstream> <branch>"); the branch
// is the current one when empty.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.setBranchUpstream)
func (r *Repo) SetUpstream(ctx context.Context, branch, upstream string) error {
	if err := refName(upstream); err != nil {
		return err
	}
	args := []string{"branch", "--set-upstream-to=" + upstream}
	if branch != "" {
		if err := refName(branch); err != nil {
			return err
		}
		args = append(args, branch)
	}
	_, err := r.write(ctx, ScopeRefs|ScopeConfig, Command{Args: args})
	return err
}

// FastForwardBranch moves a local branch that is not checked out to the commit its upstream has, when that is a fast-forward ("git fetch
// <remote> <upstream>:<branch>"). It does nothing for a branch with no upstream, and a branch with commits of its own is left alone
// (BranchFastForwardRejected is not an error here). The checkout of a branch calls it with git.pullBeforeCheckout.
//
// Ported from VS Code: extensions/git/src/repository.ts (fastForwardBranch)
func (r *Repo) FastForwardBranch(ctx context.Context, branch string) error {
	refs, err := r.RefsQuery(ctx, RefQuery{Patterns: []string{"refs/heads/" + strings.TrimPrefix(branch, "refs/heads/")}})
	if err != nil {
		return err
	}
	for _, ref := range refs {
		if ref.Type != RefHead || ref.Name != branch || ref.Upstream == nil || ref.Upstream.Remote == "" || ref.Upstream.Remote == "." {
			continue
		}
		err := r.Fetch(ctx, FetchOptions{Remote: ref.Upstream.Remote, Ref: ref.Upstream.Name + ":" + ref.Name})
		if err != nil && errTag(err) != "BranchFastForwardRejected" {
			return err
		}
		return nil
	}
	return nil
}

// errTag is the Tag of an error of this package, "" for another kind.
func errTag(err error) string {
	if appErr, ok := errors.FromError(err); ok {
		return appErr.Tag
	}
	return ""
}
