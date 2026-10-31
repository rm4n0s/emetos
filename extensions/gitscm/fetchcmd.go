package gitscm

import (
	"context"

	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// cmdFetch is git.fetch: it fetches the upstream remote of the repository (git fetch). It is the smallest fetch there is: the
// variants (prune, all remotes, a ref) and the progress, the autofetch and the sync come with the remote commands of step 24.15. It is
// here so that git's questions have a command to ask them: the password prompt (step 24.9) is shown over the fetch of a remote that
// requires one, and a failed authentication offers Retry.
//
// Ported from VS Code: extensions/git/src/commands.ts (fetch).
func (x *gitscm) cmdFetch(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { r.fetch() })
	return nil
}

// fetch runs git fetch as a command of the user.
func (r *repository) fetch() {
	r.mutate(mutation{
		name: "fetch",
		run:  func(ctx context.Context, repo *git.Repo) error { return repo.Fetch(ctx, git.FetchOptions{}) },
		// the notification of a failed authentication offers the same fetch again
		retry: r.fetch,
	})
}
