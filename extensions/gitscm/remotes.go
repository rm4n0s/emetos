package gitscm

import (
	"context"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// The commands that talk to a remote (SPEC §10.30 "Remotes, fetch, pull, push and sync"): fetch, pull, and the machinery they share with
// push and sync (pushcmds.go): the job that runs on a goroutine with the sync item spinning, the progress bar of the repository, the
// questions a job asks in the middle of its work, and the failures with their buttons. Fetch and pull are here.
//
// Ported from VS Code: extensions/git/src/commands.ts (fetch, fetchPrune, fetchAll, fetchRef, pullFrom, pull, pullRebase, pullRef,
// RemoteItem, FetchAllRemotesItem) and repository.ts (fetchDefault, fetchPrune, fetchAll, fetch, pullWithRebase, pull, pullFrom,
// _pullAndHandleTagConflict, checkIfMaybeRebased, handleTagConflict)

// remoteJob is a command that talks to a remote. It runs as a mutation (a write of the queue, the status runs again when it ends), with
// the extras of the remote commands: pull, push and sync make the sync item spin and disable the Sync and Publish buttons (VS Code's
// Operation.Pull, Push and Sync), and every job but a silent one shows the progress bar of the repository (git.showProgress).
type remoteJob struct {
	// name is what the job is called in a failure and in the Git channel ("fetch", "pull", "push", "sync").
	name string
	// spin marks a pull, a push or a sync: the sync item spins while it runs, and a second one while it runs is dropped (VS Code
	// throttles them).
	spin bool
	// silent is the autofetch: no progress bar, and a failure makes no notification.
	silent bool
	// begin and finally run on the UI thread before the job starts and after it ended (the progress notification of a sync).
	begin, finally func()
	// run is the job, on a goroutine. It must not read or change the state of the repository: what it needs is captured when the job
	// is made. w asks the user questions.
	run func(ctx context.Context, repo *git.Repo, w *remoteWork) error
	// after runs on the UI thread when the job succeeded.
	after func()
	// failed, when set, sees the failure first (see mutation.failed).
	failed func(err error) bool
	// retry runs the command again; the failure of an authentication offers it as Retry.
	retry func()
}

// remoteWork is what a job's goroutine uses besides git: the questions it asks the user.
type remoteWork struct {
	x *gitscm
	r *repository
}

// ask shows a modal question from the goroutine of a job and waits for the answer; a question that cannot be shown, a repository that is
// closed and a job that is cancelled answer with a cancel. The setting changes the answer asks for are made with set, on the UI thread.
func (w *remoteWork) ask(ctx context.Context, spec extension.DialogSpec) extension.DialogResult {
	cancelled := extension.DialogResult{Button: -1, Cancelled: true}
	ch := make(chan extension.DialogResult, 1)
	w.x.api.Post(func() {
		if w.r.disposed {
			ch <- cancelled
			return
		}
		if err := w.x.api.Dialogs().Ask(spec, func(res extension.DialogResult) { ch <- res }); err != nil {
			w.x.logUnavailable("cannot ask a question of a remote command", err)
			ch <- cancelled
		}
	})
	select {
	case res := <-ch:
		return res
	case <-ctx.Done():
		return cancelled
	}
}

// set changes a setting of the user from the goroutine of a job.
func (w *remoteWork) set(key string, value any) {
	w.x.api.Post(func() { w.x.saveSetting(key, value) })
}

// runRemote starts a job on a repository.
func (x *gitscm) runRemote(r *repository, j remoteJob) {
	if r.disposed || r.repo == nil || x.stopped.Load() {
		return
	}
	if j.spin && r.syncing {
		return // the same click twice is one run
	}
	r.mutate(mutation{
		name:  j.name,
		retry: j.retry,
		begin: func() {
			if j.spin {
				r.setSyncing(true)
			}
			if !j.silent {
				r.beginProgress()
			}
			if j.begin != nil {
				j.begin()
			}
		},
		finally: func() {
			if j.finally != nil {
				j.finally()
			}
			if !j.silent {
				r.endProgress()
			}
			if j.spin {
				r.setSyncing(false)
			}
		},
		run: func(ctx context.Context, repo *git.Repo) error { return j.run(ctx, repo, &remoteWork{x: x, r: r}) },
		after: func() {
			switch j.name {
			case "fetch", "pull", "push", "sync":
				x.goodRemoteOperation(r)
			}
			if j.after != nil {
				j.after()
			}
		},
		failed: j.failed,
	})
}

// setSyncing marks a pull, a push or a sync as running or ended, and shows it in the sync item and the buttons. The commits that waited
// for the post-commit command of a commit start once it ended.
func (r *repository) setSyncing(v bool) {
	r.syncing = v
	r.updateCommitUI()
	r.updateStatusBar()
	if !v {
		r.releaseCommits()
	}
}

// beginProgress and endProgress show the progress bar of the repository while a remote command runs (git.showProgress).
func (r *repository) beginProgress() {
	if !r.m.x.cfg().showProgress {
		return
	}
	r.opBusy++
	r.src.SetBusy(true)
}

func (r *repository) endProgress() {
	if !r.m.x.cfg().showProgress || r.opBusy == 0 {
		return
	}
	r.opBusy--
	if r.opBusy == 0 && !r.busy {
		r.src.SetBusy(false)
	}
}

// fetchOptions are the options of a fetch with git.pruneOnFetch applied.
func (x *gitscm) fetchOptions(o git.FetchOptions) git.FetchOptions {
	if !o.Prune {
		o.Prune = read(x.api, "git.pruneOnFetch", false)
	}
	return o
}

// fetchJob is a fetch of the user (or, silent, the autofetch).
func (x *gitscm) fetchJob(r *repository, o git.FetchOptions, silent bool) remoteJob {
	o = x.fetchOptions(o)
	o.Silent = silent
	return remoteJob{
		name:   "fetch",
		silent: silent,
		run:    func(ctx context.Context, repo *git.Repo, _ *remoteWork) error { return repo.Fetch(ctx, o) },
		retry:  func() { x.runRemote(r, x.fetchJob(r, o, silent)) },
	}
}

// remoteItem is a remote with its URL for a list; the user information of the URL is masked.
func remoteItem(rem git.Remote) extension.QuickPickItem {
	return extension.QuickPickItem{Label: "$(cloud) " + rem.Name, Description: git.MaskURLs(rem.FetchURL)}
}

// fetchableRemotes are the remotes of the repository now, read from its configuration.
func (r *repository) fetchableRemotes() []git.Remote {
	r.readRemotes()
	return r.remotes
}

// cmdFetch is git.fetch: with one remote it fetches that; with more it asks which, the upstream's first, or all of them.
//
// Ported from VS Code: extensions/git/src/commands.ts (fetch)
func (x *gitscm) cmdFetch(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		remotes := r.fetchableRemotes()
		switch len(remotes) {
		case 0:
			x.api.Notifications().Warn("This repository has no remotes configured to fetch from.")
			return
		case 1:
			x.runRemote(r, x.fetchJob(r, git.FetchOptions{}, false))
			return
		}
		ordered := append([]git.Remote(nil), remotes...)
		if up := r.head.upstream; up != nil {
			for i, rem := range ordered {
				if rem.Name == up.Remote {
					ordered = append([]git.Remote{rem}, append(ordered[:i:i], ordered[i+1:]...)...)
					break
				}
			}
		}
		items := make([]extension.QuickPickItem, 0, len(ordered)+2)
		for _, rem := range ordered {
			items = append(items, remoteItem(rem))
		}
		items = append(items, extension.QuickPickItem{Label: " ", Separator: true}, extension.QuickPickItem{Label: "$(cloud-download) Fetch all remotes"})
		err := x.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Select a remote to fetch", Items: items}, func(i int) {
			switch {
			case i < 0 || r.disposed:
			case i < len(ordered):
				x.runRemote(r, x.fetchJob(r, git.FetchOptions{Remote: ordered[i].Name}, false))
			case i == len(items)-1:
				x.runRemote(r, x.fetchJob(r, git.FetchOptions{All: true}, false))
			}
		})
		if err != nil {
			x.logUnavailable("cannot show the remotes to fetch from", err)
		}
	})
	return nil
}

// cmdFetchPrune is git.fetchPrune: a fetch that removes the remote-tracking branches the remote no longer has.
//
// Ported from VS Code: extensions/git/src/commands.ts (fetchPrune)
func (x *gitscm) cmdFetchPrune(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		if len(r.fetchableRemotes()) == 0 {
			x.api.Notifications().Warn("This repository has no remotes configured to fetch from.")
			return
		}
		x.runRemote(r, x.fetchJob(r, git.FetchOptions{Prune: true}, false))
	})
	return nil
}

// cmdFetchAll is git.fetchAll.
//
// Ported from VS Code: extensions/git/src/commands.ts (fetchAll)
func (x *gitscm) cmdFetchAll(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		if len(r.fetchableRemotes()) == 0 {
			x.api.Notifications().Warn("This repository has no remotes configured to fetch from.")
			return
		}
		x.runRemote(r, x.fetchJob(r, git.FetchOptions{All: true}, false))
	})
	return nil
}

// splitRemoteRef reads a remote-tracking branch given as "refs/remotes/origin/main" or "origin/main" into the remote and the branch: the
// remote is the longest name of a remote of the repository that starts the path (a remote called "up/stream" is possible).
func (r *repository) splitRemoteRef(ref string) (remote, branch string, ok bool) {
	ref = strings.TrimPrefix(ref, "refs/remotes/")
	best := ""
	for _, rem := range r.fetchableRemotes() {
		if strings.HasPrefix(ref, rem.Name+"/") && len(rem.Name) > len(best) {
			best = rem.Name
		}
	}
	if best == "" || len(ref) == len(best)+1 {
		return "", "", false
	}
	return best, ref[len(best)+1:], true
}

// remoteRefArg is the remote-tracking branch a command of the Graph is for: the ref a caller named ({"ref": "origin/main"}), else the
// branch the current one tracks (VS Code: currentHistoryItemRemoteRef); empty when there is none.
func (r *repository) remoteRefArg(args any) string {
	if m, ok := args.(map[string]any); ok {
		if s, _ := m["ref"].(string); s != "" {
			return s
		}
	}
	r.hist.mu.Lock()
	defer r.hist.mu.Unlock()
	if r.hist.remote != nil {
		return r.hist.remote.ID
	}
	return ""
}

// cmdFetchRef is git.fetchRef, the Graph's title action: it fetches the branch of the remote a ref names ({"ref": "origin/main"}), or the
// branch the current one tracks. With neither it does nothing.
//
// Ported from VS Code: extensions/git/src/commands.ts (fetchRef)
func (x *gitscm) cmdFetchRef(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		remote, branch, ok := r.splitRemoteRef(r.remoteRefArg(args))
		if !ok {
			return
		}
		x.runRemote(r, x.fetchJob(r, git.FetchOptions{Remote: remote, Ref: branch}, false))
	})
	return nil
}

// pullSettings are the settings a pull reads, taken on the UI thread when the job is made.
type pullSettings struct {
	autoStash, fetchOnPull, tags, ignoreRebaseWarning, replaceTags bool
	prune                                                          bool
}

func (x *gitscm) pullSettings() pullSettings {
	return pullSettings{
		autoStash:           read(x.api, "git.autoStash", false),
		fetchOnPull:         read(x.api, "git.fetchOnPull", false),
		tags:                read(x.api, "git.pullTags", true),
		ignoreRebaseWarning: read(x.api, "git.ignoreRebaseWarning", false),
		replaceTags:         read(x.api, "git.replaceTagsWhenPull", false),
		prune:               read(x.api, "git.pruneOnFetch", false),
	}
}

// pullPlan is one pull: what it pulls and the facts of the repository it needs, captured on the UI thread.
type pullPlan struct {
	rebase         bool
	remote, branch string
	unshallow      bool
	// headName is the branch HEAD is on, for the question about a rebased branch; upstreamRemote is where tags are fetched from when the
	// pull names no remote.
	headName, upstreamRemote string
	set                      pullSettings
	progress                 func(git.Progress)
}

// pullOf makes the plan of a pull of the current branch from its upstream, or of the named branch.
func (x *gitscm) pullOf(r *repository, rebase bool, remote, branch string) pullPlan {
	p := pullPlan{rebase: rebase, remote: remote, branch: branch, headName: r.head.name, set: x.pullSettings()}
	if up := r.head.upstream; up != nil {
		p.upstreamRemote = up.Remote
	}
	return p
}

// pullCurrent is the pull of the current branch: from its upstream when it has one, else git decides.
func (x *gitscm) pullCurrent(r *repository, rebase bool) pullPlan {
	remote, branch := "", ""
	if up := r.head.upstream; r.head.name != "" && up != nil {
		remote, branch = up.Remote, up.Name
	}
	return x.pullOf(r, rebase, remote, branch)
}

// pullJob is a job that only pulls.
func (x *gitscm) pullJob(r *repository, p pullPlan) remoteJob {
	return remoteJob{
		name: "pull", spin: true,
		run: func(ctx context.Context, repo *git.Repo, w *remoteWork) error {
			_, err := w.pull(ctx, repo, p)
			return err
		},
		retry:  func() { x.runRemote(r, x.pullJob(r, x.pullOf(r, p.rebase, p.remote, p.branch))) },
		failed: func(err error) bool { return x.pullFailed(r, err) },
	}
}

// pull is the pull of a job: fetch everything first with git.fetchOnPull, ask whether to pull a branch that looks rebased, pull, and
// when tags differ ask whether to replace the local ones and pull again. It reports false when the user declined to pull.
//
// Ported from VS Code: extensions/git/src/repository.ts (pullFrom, _pullAndHandleTagConflict, handleTagConflict)
func (w *remoteWork) pull(ctx context.Context, repo *git.Repo, p pullPlan) (pulled bool, err error) {
	if p.set.fetchOnPull {
		if err := repo.Fetch(ctx, git.FetchOptions{All: true, Prune: p.set.prune}); err != nil {
			return false, err
		}
	}
	if !w.maybeRebased(ctx, repo, p) {
		return false, nil
	}
	opts := git.PullOptions{
		Rebase: p.rebase, Remote: p.remote, Branch: p.branch, Tags: p.set.tags, Unshallow: p.unshallow, AutoStash: p.set.autoStash,
		Progress: p.progress,
	}
	_, err = repo.Pull(ctx, opts)
	if errTag(err) != "TagConflict" {
		return err == nil, err
	}
	remote := p.remote
	if remote == "" {
		remote = p.upstreamRemote
	}
	stderr := metaString(err, "stderr")
	tags := git.ConflictingTags(stderr)
	if remote == "" || len(tags) == 0 {
		return false, err // nothing to replace: the failure of git stands
	}
	if !p.set.replaceTags {
		res := w.ask(ctx, extension.DialogSpec{
			Type:    extension.DialogError,
			Message: "Unable to pull from remote repository due to conflicting tag(s): " + strings.Join(tags, ", ") + ". Would you like to resolve the conflict by replacing the local tag(s)?",
			Buttons: []extension.DialogButton{{Label: "Replace Local Tag(s)"}, {Label: "Always Replace Local Tag(s)"}, cancelButton},
		})
		if res.Cancelled || res.Button > 1 {
			return false, nil
		}
		if res.Button == 1 {
			w.set("git.replaceTagsWhenPull", true)
		}
	}
	if err := repo.FetchTags(ctx, remote, tags, true); err != nil {
		return false, err
	}
	_, err = repo.Pull(ctx, opts)
	return err == nil, err
}

// maybeRebased asks whether to pull into a branch that might have been rebased (its upstream has commits with the same changes); true
// when to go on. Always Pull stops the question for good (git.ignoreRebaseWarning).
//
// Ported from VS Code: extensions/git/src/repository.ts (checkIfMaybeRebased)
func (w *remoteWork) maybeRebased(ctx context.Context, repo *git.Repo, p pullPlan) bool {
	if p.set.ignoreRebaseWarning || !repo.MaybeRebased(ctx, p.headName) {
		return true
	}
	message := "It looks like the current branch might have been rebased. Are you sure you still want to pull into it?"
	if p.headName != "" {
		message = `It looks like the current branch "` + p.headName + `" might have been rebased. Are you sure you still want to pull into it?`
	}
	res := w.ask(ctx, extension.DialogSpec{
		Type: extension.DialogWarning, Message: message,
		Buttons: []extension.DialogButton{{Label: "Always Pull"}, {Label: "Pull"}, {Label: "Don't Pull", Cancel: true}},
	})
	switch {
	case res.Cancelled || res.Button == 2:
		return false
	case res.Button == 0:
		w.set("git.ignoreRebaseWarning", true)
	}
	return true
}

// metaString is a string of the metadata of an error of this package.
func metaString(err error, key string) string {
	if appErr, ok := errors.FromError(err); ok {
		s, _ := appErr.Metadata[key].(string)
		return s
	}
	return ""
}

// pullFailed is the failure of a pull: a conflict is a warning; everything else is the generic failure.
func (x *gitscm) pullFailed(r *repository, err error) bool {
	if x.operationFailed(err) {
		return true
	}
	if errTag(err) == "NoUserNameConfigured" || errTag(err) == "NoUserEmailConfigured" {
		x.identityMissing(r, func() { x.runRemote(r, x.pullJob(r, x.pullCurrent(r, false))) })
		return true
	}
	return false
}

// cmdPull is git.pull: it pulls the upstream of the current branch.
//
// Ported from VS Code: extensions/git/src/commands.ts (pull)
func (x *gitscm) cmdPull(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { x.pull(r, false) })
	return nil
}

// cmdPullRebase is git.pullRebase.
//
// Ported from VS Code: extensions/git/src/commands.ts (pullRebase)
func (x *gitscm) cmdPullRebase(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { x.pull(r, true) })
	return nil
}

func (x *gitscm) pull(r *repository, rebase bool) {
	if len(r.fetchableRemotes()) == 0 {
		x.api.Notifications().Warn("Your repository has no remotes configured to pull from.")
		return
	}
	x.runRemote(r, x.pullJob(r, x.pullCurrent(r, rebase)))
}

// cmdPullRef is git.pullRef, the Graph's title action: it pulls the branch of the remote a ref names ({"ref": "origin/main"}), or the
// branch the current one tracks.
//
// Ported from VS Code: extensions/git/src/commands.ts (pullRef)
func (x *gitscm) cmdPullRef(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		remote, branch, ok := r.splitRemoteRef(r.remoteRefArg(args))
		if !ok {
			return
		}
		x.runRemote(r, x.pullJob(r, x.pullOf(r, false, remote, branch)))
	})
	return nil
}

// cmdPullFrom is git.pullFrom: it asks for the remote (when there are several) and the branch of that remote to pull.
//
// Ported from VS Code: extensions/git/src/commands.ts (pullFrom)
func (x *gitscm) cmdPullFrom(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		remotes := r.fetchableRemotes()
		if len(remotes) == 0 {
			x.api.Notifications().Warn("Your repository has no remotes configured to pull from.")
			return
		}
		if len(remotes) == 1 {
			x.pickBranchToPull(r, remotes[0].Name)
			return
		}
		items := make([]extension.QuickPickItem, len(remotes))
		for i, rem := range remotes {
			items[i] = extension.QuickPickItem{Label: rem.Name, Description: git.MaskURLs(rem.FetchURL)}
		}
		err := x.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Pick a remote to pull the branch from", Items: items}, func(i int) {
			if i >= 0 && i < len(remotes) && !r.disposed {
				x.pickBranchToPull(r, remotes[i].Name)
			}
		})
		if err != nil {
			x.logUnavailable("cannot show the remotes to pull from", err)
		}
	})
	return nil
}

// pickBranchToPull lists the branches of a remote that were fetched and pulls the one that is chosen.
func (x *gitscm) pickBranchToPull(r *repository, remote string) {
	x.loadRefs(r, x.refQuery("refs/remotes/"+remote+"/"), func(refs []git.Ref) {
		entries := x.refEntries(r, refs, refListOptions{types: []git.RefType{git.RefRemoteHead}})
		var picks []refEntry
		for _, e := range entries {
			if !e.item.Separator {
				picks = append(picks, e)
			}
		}
		x.pickRefs(r, "Pick a branch to pull from", "There are no branches to pull from.", picks, func(e refEntry, _ string) {
			if e.ref == nil || !strings.HasPrefix(e.ref.Name, remote+"/") {
				return
			}
			x.runRemote(r, x.pullJob(r, x.pullOf(r, false, remote, strings.TrimPrefix(e.ref.Name, remote+"/"))))
		})
	})
}
