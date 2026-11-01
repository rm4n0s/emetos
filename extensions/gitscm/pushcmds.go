package gitscm

import (
	"context"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// Push, sync, publish and the remotes (SPEC §10.30 "Remotes, fetch, pull, push and sync"). Everything here runs the user's git: its
// credential helpers, its SSH keys and agent, and the password prompts of step 24.9. No service repository is created: publishing a
// branch pushes it to a remote that exists, or that the user adds first.
//
// Ported from VS Code: extensions/git/src/commands.ts (_push, push, pushForce, pushFollowTags, pushFollowTagsForce, pushRef, pushTo,
// pushToForce, pushTags, addRemote, removeRemote, _sync, sync, syncRebase, publish, AddRemoteItem) and repository.ts (push, pushTo,
// pushFollowTags, pushTags, sync, _sync)

// pushType is what a push command pushes.
type pushType int

const (
	// pushCurrent pushes the current branch to its upstream (git.push).
	pushCurrent pushType = iota
	// pushToRemote pushes the current branch to a remote that is chosen or named (git.pushTo).
	pushToRemote
	// pushFollowTags pushes the current branch's commits and the annotated tags that point at them (git.pushWithTags).
	pushFollowTags
	// pushAllTags pushes every tag (git.pushTags).
	pushAllTags
)

// pushRequest is a push command with what its caller gave it.
type pushRequest struct {
	typ pushType
	// force asks for a force push (git.pushForce and the like).
	force bool
	// remote, refspec and setUpstream are the target of a pushToRemote that is given (the arguments of the command).
	remote, refspec string
	setUpstream     bool
	// silent is a push nobody asked for in person (the post-commit command of a keystroke is not one; none is yet): it makes no
	// notification when there is nothing to push to.
	silent bool
}

// forcePushMode is the mode the settings give a force push.
func (x *gitscm) forcePushMode() git.ForcePushMode {
	switch {
	case !read(x.api, "git.useForcePushWithLease", true):
		return git.ForceAlways
	case read(x.api, "git.useForcePushIfIncludes", true):
		return git.ForceWithLeaseIfIncludes
	}
	return git.ForceWithLease
}

// push is git.push and its variants.
//
// Ported from VS Code: extensions/git/src/commands.ts (_push)
func (x *gitscm) push(r *repository, req pushRequest) {
	if r.disposed || r.repo == nil {
		return
	}
	if len(r.fetchableRemotes()) == 0 {
		if req.silent {
			return
		}
		x.api.Notifications().Warn("Your repository has no remotes configured to push to.",
			extension.NotificationAction{Label: "Add Remote", Run: func() { x.addRemote(r, nil) }})
		return
	}
	if !req.force {
		x.pushWith(r, req, git.NoForce)
		return
	}
	if !read(x.api, "git.allowForcePush", false) {
		x.api.Notifications().Error(`Force push is not allowed, please enable it with the "git.allowForcePush" setting.`)
		return
	}
	mode := x.forcePushMode()
	if !read(x.api, "git.confirmForcePush", true) {
		x.pushWith(r, req, mode)
		return
	}
	x.ask(extension.DialogSpec{
		Type:    extension.DialogWarning,
		Message: "You are about to force push your changes, this can be destructive and could inadvertently overwrite changes made by others.",
		Detail:  "Are you sure to continue?",
		Buttons: []extension.DialogButton{{Label: "OK"}, {Label: "OK, Don't Ask Again"}, cancelButton},
	}, func(res extension.DialogResult) {
		switch {
		case res.Cancelled || res.Button == 2 || r.disposed:
		case res.Button == 1:
			x.saveSetting("git.confirmForcePush", false)
			x.pushWith(r, req, mode)
		default:
			x.pushWith(r, req, mode)
		}
	})
}

// pushWith pushes once the force question is answered.
func (x *gitscm) pushWith(r *repository, req pushRequest, mode git.ForcePushMode) {
	switch req.typ {
	case pushFollowTags:
		x.runRemote(r, x.pushJob(r, git.PushOptions{FollowTags: true, Force: mode}, req.silent, nil))
		return
	case pushAllTags:
		// VS Code goes on to ask where to publish the branch after the tags are pushed; Emetos stops (decision 783)
		x.runRemote(r, x.pushJob(r, git.PushOptions{Tags: true, Remote: r.head.remote, Force: mode}, req.silent, nil))
		return
	}
	if r.head.name == "" {
		if !req.silent {
			x.api.Notifications().Warn("Please check out a branch to push to a remote.")
		}
		return
	}
	branch := r.head.name
	if req.typ == pushCurrent {
		o := git.PushOptions{Force: mode}
		if up := r.head.upstream; up != nil {
			o.Remote, o.Refspec = up.Remote, branch+":"+up.Name
		}
		x.runRemote(r, x.pushJob(r, o, req.silent, func() { x.noUpstream(r, req.silent) }))
		return
	}
	if req.remote != "" {
		refspec := req.refspec
		if refspec == "" {
			refspec = branch
		}
		x.runRemote(r, x.pushJob(r, git.PushOptions{Remote: req.remote, Refspec: refspec, SetUpstream: req.setUpstream, Force: mode}, req.silent, nil))
		return
	}
	x.pickRemoteToPush(r, branch, func(remote string) {
		x.runRemote(r, x.pushJob(r, git.PushOptions{Remote: remote, Refspec: branch, Force: mode}, req.silent, nil))
	})
}

// pickRemoteToPush asks to which remote the branch goes: the remotes that can be pushed to, or a new one.
//
// Ported from VS Code: extensions/git/src/commands.ts (_push: the pick of PushTo, AddRemoteItem)
func (x *gitscm) pickRemoteToPush(r *repository, branch string, then func(remote string)) {
	var remotes []git.Remote
	for _, rem := range r.fetchableRemotes() {
		if rem.PushURL != "" {
			remotes = append(remotes, rem)
		}
	}
	items := make([]extension.QuickPickItem, 0, len(remotes)+1)
	for _, rem := range remotes {
		items = append(items, extension.QuickPickItem{Label: rem.Name, Description: git.MaskURLs(rem.PushURL)})
	}
	items = append(items, extension.QuickPickItem{Label: "$(plus) Add a new remote...", AlwaysShow: true})
	err := x.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: `Pick a remote to publish the branch "` + branch + `" to:`, Items: items}, func(i int) {
		switch {
		case i < 0 || r.disposed:
		case i < len(remotes):
			then(remotes[i].Name)
		default:
			x.addRemote(r, then)
		}
	})
	if err != nil {
		x.logUnavailable("cannot show the remotes to push to", err)
	}
}

// noUpstream is what a push of a branch with no upstream leads to: the question whether to publish it (once, with a way to never ask
// again, kept in the state of the profile), or nothing for a push nobody asked for.
//
// Ported from VS Code: extensions/git/src/commands.ts (_push: the catch of NoUpstreamBranch)
func (x *gitscm) noUpstream(r *repository, silent bool) {
	if silent || r.disposed {
		return
	}
	if !x.state.get().ConfirmBranchPublishOff {
		x.ask(extension.DialogSpec{
			Type:    extension.DialogWarning,
			Message: `The branch "` + r.head.name + `" has no remote branch. Would you like to publish this branch?`,
			Buttons: []extension.DialogButton{{Label: "OK"}, {Label: "OK, Don't Ask Again"}, cancelButton},
		}, func(res extension.DialogResult) {
			switch {
			case res.Cancelled || res.Button == 2 || r.disposed:
			case res.Button == 1:
				x.state.update(x, func(s *profileState) { s.ConfirmBranchPublishOff = true })
				x.publish(r)
			default:
				x.publish(r)
			}
		})
		return
	}
	x.publish(r)
}

// pushJob is a push as a job; noUpstream, when set, is what to do when the branch has no upstream.
func (x *gitscm) pushJob(r *repository, o git.PushOptions, silent bool, noUpstream func()) remoteJob {
	return remoteJob{
		name: "push", spin: true, silent: silent,
		run:    func(ctx context.Context, repo *git.Repo, _ *remoteWork) error { return repo.Push(ctx, o) },
		after:  func() { x.pushed() },
		retry:  func() { x.runRemote(r, x.pushJob(r, o, silent, noUpstream)) },
		failed: func(err error) bool { return x.pushFailed(r, err, noUpstream) },
	}
}

// pushed tells that a push succeeded, when git.showPushSuccessNotification says so.
//
// Ported from VS Code: extensions/git/src/repository.ts (the listener of onDidRunOperation for a push)
func (x *gitscm) pushed() {
	if read(x.api, "git.showPushSuccessNotification", false) {
		x.api.Notifications().Info("Successfully pushed.")
	}
}

// pushFailed is the failure of a push: a push the remote refused because it has commits the branch lacks says so and offers Pull; a branch
// with no upstream leads to the question about publishing it, or offers Publish Branch where no question is asked; everything else is the
// generic failure. A refusal by the server itself (a protected branch, a hook) shows the message the remote sent, as git prints it.
//
// Ported from VS Code: extensions/git/src/commands.ts (createCommand: the catch for PushRejected, ForcePushWithLeaseRejected)
func (x *gitscm) pushFailed(r *repository, err error, noUpstream func()) bool {
	pull := extension.NotificationAction{Label: "Pull", Run: func() { x.pull(r, false) }}
	showOutput := extension.NotificationAction{Label: "Show Git Output", Run: func() { x.channel.Show(false) }}
	switch errTag(err) {
	case "PushRejected":
		x.logLine("git push failed in " + r.root + ": " + git.MaskURLs(describe(err)))
		x.api.Notifications().Error(`Can't push refs to remote. Try running "Pull" first to integrate your changes.`, pull, showOutput)
		return true
	case "ForcePushWithLeaseRejected", "ForcePushWithLeaseIfIncludesRejected":
		x.logLine("git push failed in " + r.root + ": " + git.MaskURLs(describe(err)))
		x.api.Notifications().Error(`Can't force push refs to remote. The tip of the remote-tracking branch has been updated since the last checkout. Try running "Pull" first to pull the latest changes from the remote branch first.`, pull, showOutput)
		return true
	case "RemoteRejected":
		// a protected branch or a hook of the server: the message is the one the remote sent
		x.logLine("git push failed in " + r.root + ": " + git.MaskURLs(describe(err)))
		x.api.Notifications().Error("Git: "+remoteMessage(err), showOutput)
		return true
	case "NoUpstreamBranch":
		if noUpstream != nil {
			noUpstream()
			return true
		}
		x.logLine("git push failed in " + r.root + ": " + git.MaskURLs(describe(err)))
		x.api.Notifications().Error("Git: "+firstStderrLine(err, describe(err)),
			extension.NotificationAction{Label: "Publish Branch", Run: func() { x.publish(r) }}, showOutput)
		return true
	}
	return false
}

// cmdPush is git.push.
//
// Ported from VS Code: extensions/git/src/commands.ts (push)
func (x *gitscm) cmdPush(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { x.push(r, pushRequest{typ: pushCurrent}) })
	return nil
}

// cmdPushRef is git.pushRef, the Graph's title action.
//
// Ported from VS Code: extensions/git/src/commands.ts (pushRef)
func (x *gitscm) cmdPushRef(ctx context.Context, args any) error { return x.cmdPush(ctx, args) }

// cmdPushForce is git.pushForce.
//
// Ported from VS Code: extensions/git/src/commands.ts (pushForce)
func (x *gitscm) cmdPushForce(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { x.push(r, pushRequest{typ: pushCurrent, force: true}) })
	return nil
}

// cmdPushWithTags is git.pushWithTags.
//
// Ported from VS Code: extensions/git/src/commands.ts (pushFollowTags)
func (x *gitscm) cmdPushWithTags(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { x.push(r, pushRequest{typ: pushFollowTags}) })
	return nil
}

// cmdPushWithTagsForce is git.pushWithTagsForce.
//
// Ported from VS Code: extensions/git/src/commands.ts (pushFollowTagsForce)
func (x *gitscm) cmdPushWithTagsForce(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { x.push(r, pushRequest{typ: pushFollowTags, force: true}) })
	return nil
}

// cmdPushTags is git.pushTags.
//
// Ported from VS Code: extensions/git/src/commands.ts (pushTags)
func (x *gitscm) cmdPushTags(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { x.push(r, pushRequest{typ: pushAllTags}) })
	return nil
}

// pushToArgs reads the arguments VS Code's pushTo takes after the repository: {"remote": "origin", "refspec": "main", "setUpstream": true}.
func pushToArgs(args any, force bool) pushRequest {
	req := pushRequest{typ: pushToRemote, force: force}
	for _, a := range pushArgMaps(args) {
		if s, ok := a["remote"].(string); ok {
			req.remote = s
		}
		if s, ok := a["refspec"].(string); ok {
			req.refspec = s
		}
		if b, ok := a["setUpstream"].(bool); ok {
			req.setUpstream = b
		}
	}
	return req
}

// pushArgMaps are the maps among the arguments of a command.
func pushArgMaps(args any) []map[string]any {
	switch v := args.(type) {
	case map[string]any:
		return []map[string]any{v}
	case []any:
		var out []map[string]any
		for _, e := range v {
			if m, ok := e.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

// cmdPushTo is git.pushTo, "Push to...".
//
// Ported from VS Code: extensions/git/src/commands.ts (pushTo)
func (x *gitscm) cmdPushTo(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { x.push(r, pushToArgs(args, false)) })
	return nil
}

// cmdPushToForce is git.pushToForce.
//
// Ported from VS Code: extensions/git/src/commands.ts (pushToForce)
func (x *gitscm) cmdPushToForce(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { x.push(r, pushToArgs(args, true)) })
	return nil
}

// cmdSync is git.sync: pull, then push (SPEC: git.rebaseWhenSync makes the pull a rebase).
//
// Ported from VS Code: extensions/git/src/commands.ts (sync)
func (x *gitscm) cmdSync(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { x.sync(r, read(x.api, "git.rebaseWhenSync", false)) })
	return nil
}

// cmdSyncRebase is git.syncRebase.
//
// Ported from VS Code: extensions/git/src/commands.ts (syncRebase)
func (x *gitscm) cmdSyncRebase(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { x.sync(r, true) })
	return nil
}

// sync asks for the confirmation (git.confirmSync) and runs the sync. A branch with no upstream is pushed instead, which leads to the
// question about publishing it.
//
// Ported from VS Code: extensions/git/src/commands.ts (_sync)
func (x *gitscm) sync(r *repository, rebase bool) {
	h := r.head
	if r.disposed || r.repo == nil || (h.name == "" && h.commit == "") {
		return
	}
	if h.upstream == nil {
		x.push(r, pushRequest{typ: pushCurrent})
		return
	}
	remoteName := h.remote
	if remoteName == "" {
		remoteName = h.upstream.Remote
	}
	r.readRemotes()
	rem := r.remoteNamed(remoteName)
	readOnly := rem != nil && rem.ReadOnly
	if readOnly || !read(x.api, "git.confirmSync", true) {
		x.runRemote(r, x.syncJob(r, rebase))
		return
	}
	x.ask(extension.DialogSpec{
		Type:    extension.DialogWarning,
		Message: `This action will pull and push commits from and to "` + h.upstream.Remote + "/" + h.upstream.Name + `".`,
		Buttons: []extension.DialogButton{{Label: "OK"}, {Label: "OK, Don't Show Again"}, cancelButton},
	}, func(res extension.DialogResult) {
		switch {
		case res.Cancelled || res.Button == 2 || r.disposed:
		case res.Button == 1:
			x.saveSetting("git.confirmSync", false)
			x.runRemote(r, x.syncJob(r, rebase))
		default:
			x.runRemote(r, x.syncJob(r, rebase))
		}
	})
}

// syncJob is the pull and the push of a sync. With git.supportCancellation the pull shows a notification with a Cancel button and the
// progress of the transfer; cancelling it ends the pull and the sync. The push follows when the branch is ahead of its upstream, unless
// the remote is read-only.
//
// Ported from VS Code: extensions/git/src/repository.ts (_sync)
func (x *gitscm) syncJob(r *repository, rebase bool) remoteJob {
	h := r.head
	up := *h.upstream
	remoteName := h.remote
	if remoteName == "" {
		remoteName = up.Remote
	}
	rem := r.remoteNamed(remoteName)
	readOnly := rem != nil && rem.ReadOnly
	followTags := read(x.api, "git.followTagsWhenSync", false)
	cancellable := read(x.api, "git.supportCancellation", false)
	plan := x.pullOf(r, rebase, up.Remote, up.Name)
	branch := h.name

	var (
		prog         extension.NotificationProgress
		pullCtx      context.Context
		cancelPull   context.CancelFunc
		stopWatching func() bool
	)
	j := remoteJob{name: "sync", spin: true}
	if cancellable {
		j.begin = func() {
			pullCtx, cancelPull = context.WithCancel(r.ctx)
			prog = x.api.Notifications().Progress(extension.ProgressOptions{
				Title: "Syncing. Cancelling may cause serious damages to the repository", Cancellable: true, OnCancel: cancelPull,
			})
			// a repository that is closed or untrusted while the sync runs ends the job without its finally: the notification goes anyway
			stopWatching = context.AfterFunc(r.ctx, func() { x.api.Post(prog.Done) })
			plan.progress = func(p git.Progress) {
				percent := p.Overall()
				if percent < 0 {
					return
				}
				x.api.Post(func() { prog.Report(p.Phase, float64(percent)) })
			}
		}
		j.finally = func() {
			stopWatching()
			prog.Done()
			cancelPull()
		}
	}
	j.run = func(ctx context.Context, repo *git.Repo, w *remoteWork) error {
		pulling := ctx
		if pullCtx != nil {
			pulling = pullCtx
		}
		if _, err := w.pull(pulling, repo, plan); err != nil {
			return err
		}
		if readOnly || branch == "" {
			return nil
		}
		if ahead, known := aheadOf(ctx, repo, branch); known && ahead == 0 {
			return nil
		}
		return repo.Push(ctx, git.PushOptions{Remote: up.Remote, Refspec: branch + ":" + up.Name, FollowTags: followTags})
	}
	j.retry = func() { x.sync(r, rebase) }
	j.failed = func(err error) bool {
		if x.pullFailed(r, err) {
			return true
		}
		return x.pushFailed(r, err, nil)
	}
	return j
}

// aheadOf is how many commits the branch has that its upstream lacks, as git sees it now; false when git cannot tell.
func aheadOf(ctx context.Context, repo *git.Repo, branch string) (int, bool) {
	refs, err := repo.RefsQuery(ctx, git.RefQuery{Patterns: []string{"refs/heads/" + branch}})
	if err != nil {
		return 0, false
	}
	for _, ref := range refs {
		if ref.Type == git.RefHead && ref.Name == branch && ref.Upstream != nil {
			return ref.Ahead, true
		}
	}
	return 0, false
}

// cmdPublish is git.publish, "Publish Branch...": the branch is pushed to a remote of the repository with -u. A repository with no
// remote asks to add one first, and no service repository is made.
//
// Ported from VS Code: extensions/git/src/commands.ts (publish)
func (x *gitscm) cmdPublish(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { x.publish(r) })
	return nil
}

func (x *gitscm) publish(r *repository) {
	if r.disposed || r.repo == nil {
		return
	}
	branch := r.head.name
	if branch == "" {
		x.api.Notifications().Warn("Please check out a branch to push to a remote.")
		return
	}
	remotes := r.fetchableRemotes()
	publishTo := func(remote string) {
		x.runRemote(r, x.pushJob(r, git.PushOptions{Remote: remote, Refspec: branch, SetUpstream: true}, false, nil))
	}
	switch len(remotes) {
	case 0:
		x.addRemote(r, publishTo)
	case 1:
		publishTo(remotes[0].Name)
	default:
		items := make([]extension.QuickPickItem, 0, len(remotes)+1)
		for _, rem := range remotes {
			items = append(items, extension.QuickPickItem{Label: rem.Name, Description: git.MaskURLs(rem.PushURL)})
		}
		items = append(items, extension.QuickPickItem{Label: "$(plus) Add a new remote...", AlwaysShow: true})
		err := x.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: `Pick a remote to publish the branch "` + branch + `" to:`, Items: items}, func(i int) {
			switch {
			case i < 0 || r.disposed:
			case i < len(remotes):
				publishTo(remotes[i].Name)
			default:
				x.addRemote(r, publishTo)
			}
		})
		if err != nil {
			x.logUnavailable("cannot show the remotes to publish to", err)
		}
	}
}

// remoteNameFor is the name a remote gets from what the user typed: trimmed, with what git refuses in a name replaced by a dash; empty
// when nothing is left.
//
// Ported from VS Code: extensions/git/src/commands.ts (sanitizeRemoteName)
func remoteNameFor(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return reBranchUnsafe.ReplaceAllString(name, "-")
}

// cmdAddRemote is git.addRemote: it asks for the URL and the name, adds the remote and fetches it.
//
// Ported from VS Code: extensions/git/src/commands.ts (addRemote)
func (x *gitscm) cmdAddRemote(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { x.addRemote(r, nil) })
	return nil
}

// addRemote adds a remote; then, when set, is called with its name once it was added and fetched. The URL is checked as a path, a file,
// https, http, git or ssh URL, or an scp-like address (git.ValidateRemoteURL).
func (x *gitscm) addRemote(r *repository, then func(name string)) {
	if r.disposed || r.repo == nil {
		return
	}
	err := x.api.InputBox().Show(extension.InputBoxOptions{
		Prompt: "Provide repository URL", Message: "Add remote from URL", IgnoreFocusOut: true, Validate: git.ValidateRemoteURL,
	}, func(rawURL string, ok bool) {
		rawURL = strings.TrimSpace(rawURL)
		if !ok || rawURL == "" || r.disposed {
			return
		}
		x.askRemoteName(r, rawURL, then)
	})
	if err != nil {
		x.logUnavailable("cannot ask for the URL of the remote", err)
	}
}

func (x *gitscm) askRemoteName(r *repository, rawURL string, then func(name string)) {
	err := x.api.InputBox().Show(extension.InputBoxOptions{
		Prompt: "Remote name", Message: "Please provide a remote name", IgnoreFocusOut: true,
		Validate: func(name string) string {
			switch {
			case remoteNameFor(name) == "":
				return "Remote name format invalid"
			}
			for _, rem := range r.fetchableRemotes() {
				if rem.Name == name {
					return `Remote "` + name + `" already exists.`
				}
			}
			return ""
		},
	}, func(typed string, ok bool) {
		name := remoteNameFor(typed)
		if !ok || name == "" || r.disposed {
			return
		}
		x.runRemote(r, remoteJob{
			name: "add remote",
			run: func(ctx context.Context, repo *git.Repo, _ *remoteWork) error {
				if err := repo.AddRemote(ctx, name, rawURL); err != nil {
					return err
				}
				return repo.Fetch(ctx, x.fetchOptions(git.FetchOptions{Remote: name}))
			},
			after: func() {
				r.readRemotes()
				r.updateStatusBar()
				if then != nil {
					then(name)
				}
			},
			failed: func(error) bool {
				r.readRemotes() // the remote was added even when the first fetch failed
				r.updateStatusBar()
				return false
			},
			retry: func() { x.askRemoteName(r, rawURL, then) },
		})
	})
	if err != nil {
		x.logUnavailable("cannot ask for the name of the remote", err)
	}
}

// cmdRemoveRemote is git.removeRemote: a pick of the remotes, the chosen one is removed with its remote-tracking branches.
//
// Ported from VS Code: extensions/git/src/commands.ts (removeRemote)
func (x *gitscm) cmdRemoveRemote(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		remotes := r.fetchableRemotes()
		if len(remotes) == 0 {
			x.api.Notifications().Error("Your repository has no remotes.")
			return
		}
		items := make([]extension.QuickPickItem, len(remotes))
		for i, rem := range remotes {
			items[i] = remoteItem(rem)
		}
		err := x.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Pick a remote to remove", Items: items}, func(i int) {
			if i < 0 || i >= len(remotes) || r.disposed {
				return
			}
			name := remotes[i].Name
			r.mutate(mutation{
				name:  "remove remote",
				run:   func(ctx context.Context, repo *git.Repo) error { return repo.RemoveRemote(ctx, name) },
				after: func() { r.readRemotes(); r.updateStatusBar() },
			})
		})
		if err != nil {
			x.logUnavailable("cannot show the remotes to remove", err)
		}
	})
	return nil
}

// remoteMessage is what the remote said about a push it refused: the lines it sent ("remote: protected branch..."), else the reason git
// prints after the ref ("(pre-receive hook declined)").
func remoteMessage(err error) string {
	var said []string
	reason := ""
	for _, line := range strings.Split(metaString(err, "stderr"), "\n") {
		line = strings.TrimSpace(line)
		if text, ok := strings.CutPrefix(line, "remote:"); ok {
			if text = strings.TrimSpace(text); text != "" {
				said = append(said, text)
			}
		} else if strings.HasPrefix(line, "! [remote rejected]") {
			if i := strings.LastIndex(line, "("); i >= 0 && strings.HasSuffix(line, ")") {
				reason = line[i+1 : len(line)-1]
			}
		}
	}
	switch {
	case len(said) > 0:
		return git.MaskURLs(strings.Join(said, " "))
	case reason != "":
		return reason
	}
	return firstStderrLine(err, describe(err))
}

// firstStderrLine is the first line of the error output of a failed command, with the user information of URLs masked; the message of the
// error when there is none. It is the text VS Code shows for a failure it has no message for.
func firstStderrLine(err error, fallback string) string {
	for _, line := range strings.Split(metaString(err, "stderr"), "\n") {
		if line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "error: ")); line != "" {
			return git.MaskURLs(line)
		}
	}
	return git.MaskURLs(fallback)
}
