package git

import (
	"context"
	"strconv"
)

// The commands that talk to a remote: fetch, pull and push, and the remotes themselves (SPEC §10.30 "Remotes, fetch, pull, push and
// sync"). The user asked for each of them (or the autofetch did, with Silent), so none is a background command: git may ask for a password
// through the askpass program of the Runner's Config and the error has the tag AuthenticationFailed when none was given. They run as
// writes of the queue because they move references.

// FetchOptions say what Fetch fetches.
type FetchOptions struct {
	// Remote is the remote to fetch from; empty fetches the upstream of the current branch, or origin (what "git fetch" does).
	Remote string
	// Ref, with Remote, is the ref to fetch: "main" or "main:main", which fast-forwards the local branch main when it can be.
	Ref string
	// All fetches every remote (--all); Remote is then ignored.
	All bool
	// Prune removes the remote-tracking branches the remote no longer has (--prune).
	Prune bool
	// Depth, when it is more than 0, limits the history fetched (--depth).
	Depth int
	// Silent marks a fetch the user did not ask for (the autofetch): it never asks for a password (SPEC §10.30 "Passwords and editors").
	Silent bool
	// Progress, when set, gets the progress of the transfer (--progress).
	Progress func(Progress)
}

// silentEnv is the variable that marks a fetch as one the user did not ask for; the askpass helper answers it with a cancel
// (helper.EnvFetchSilent).
const silentEnv = "EMETOS_GIT_FETCH_SILENT=true"

// Fetch runs git fetch. An error has the tags of the generic rules (AuthenticationFailed, CantAccessRemote, RepositoryNotFound...),
// NoRemoteRepositorySpecified, RemoteConnectionError and, for a ref that cannot be fast-forwarded, BranchFastForwardRejected.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.fetch)
func (r *Repo) Fetch(ctx context.Context, o FetchOptions) error {
	if o.Remote != "" {
		if err := remoteArg(o.Remote); err != nil {
			return err
		}
	}
	args := []string{"fetch"}
	if o.Progress != nil {
		args = append(args, "--progress")
	}
	if o.Prune {
		args = append(args, "--prune")
	}
	if o.Depth > 0 {
		args = append(args, "--depth="+strconv.Itoa(o.Depth))
	}
	switch {
	case o.Remote != "":
		args = append(args, "--", o.Remote)
		if o.Ref != "" {
			args = append(args, o.Ref)
		}
	case o.All:
		args = append(args, "--all")
	}
	c := Command{Args: args, Progress: o.Progress}
	if o.Silent {
		c.Env = append(c.Env, silentEnv)
	}
	_, err := r.write(ctx, ScopeRefs, c)
	return err
}

// FetchTags fetches the tags by name from a remote, replacing the local ones when force is set ("git fetch <remote>
// refs/tags/<t>:refs/tags/<t>... [--force]"): the way out of a pull that stopped on tags that differ.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.fetchTags)
func (r *Repo) FetchTags(ctx context.Context, remote string, tags []string, force bool) error {
	if err := remoteArg(remote); err != nil {
		return err
	}
	args := []string{"fetch"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, "--", remote)
	for _, t := range tags {
		if err := refName(t); err != nil {
			return err
		}
		args = append(args, "refs/tags/"+t+":refs/tags/"+t)
	}
	_, err := r.write(ctx, ScopeRefs, Command{Args: args})
	return err
}
