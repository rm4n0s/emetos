package git

import "context"

// FetchOptions say what Fetch fetches.
type FetchOptions struct {
	// Remote is the remote to fetch from; empty fetches the upstream of the current branch, or origin (what "git fetch" does).
	Remote string
	// All fetches every remote (--all); Remote is then ignored.
	All bool
	// Prune removes the remote-tracking branches the remote no longer has (--prune).
	Prune bool
}

// Fetch runs git fetch as a write of the queue (it moves remote-tracking references). It is not a background command: the user asked for
// it, git may ask for a password through the askpass program of the Runner's Config, and the error has the tag AuthenticationFailed when
// the user did not give one.
//
// Ported from VS Code: extensions/git/src/git.ts (Repository.fetch). Step 24.15 completes the remote commands.
func (r *Repo) Fetch(ctx context.Context, o FetchOptions) error {
	args := []string{"fetch"}
	if o.Prune {
		args = append(args, "--prune")
	}
	switch {
	case o.All:
		args = append(args, "--all")
	case o.Remote != "":
		args = append(args, "--", o.Remote)
	}
	_, err := r.write(ctx, ScopeRefs, Command{Args: args})
	return err
}
