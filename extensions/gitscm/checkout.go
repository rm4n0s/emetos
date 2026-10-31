package gitscm

import (
	"context"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// Checkout (SPEC §10.30 "Branches, tags, merge and the states in between"): the picker of branches, remote branches and tags, the
// entries that make a branch or check out a commit, the switch itself, and the questions about a work tree whose changes the switch would
// overwrite.
//
// Ported from VS Code: extensions/git/src/commands.ts (checkout, checkoutDetached, _checkout, CheckoutItem, CheckoutProtectedItem,
// CheckoutRemoteHeadItem, CheckoutTagItem, CreateBranchItem, CreateBranchFromItem, CheckoutDetachedItem) and
// extensions/git/src/repository.ts (checkout, checkoutTracking)

// The entries of the checkout picker that are commands, not refs.
const (
	entryCreate     = "create"
	entryCreateFrom = "createFrom"
	entryDetached   = "detached"
)

// checkoutJob is one switch of the work tree.
type checkoutJob struct {
	// label is what the branch item of the status bar shows while the switch runs: the ref, or the short hash for a detached checkout.
	label string
	// run is the switch, on a goroutine; force throws away the changes that would be overwritten.
	run func(ctx context.Context, repo *git.Repo, force bool) error
	// dirtyFlow asks what to do with local changes that stop the switch (the switches the user picked; one given by a caller's
	// argument fails with the message of git, as VS Code's does).
	dirtyFlow bool
	// pullBefore is git.pullBeforeCheckout: fast-forward the branch before it is checked out.
	// TODO(step 24.15): the pull commands give this a fast-forward ("git merge --ff-only" of the upstream) to run first.
	pullBefore bool
}

// treeishArg is the ref a caller named ({"treeish": "v1.0"}): no picker is shown for it.
func treeishArg(args any) string {
	if m, ok := args.(map[string]any); ok {
		if s, ok := m["treeish"].(string); ok {
			return s
		}
	}
	return ""
}

func (x *gitscm) cmdCheckout(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { x.checkout(r, false, treeishArg(args)) })
	return nil
}

func (x *gitscm) cmdCheckoutDetached(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { x.checkout(r, true, treeishArg(args)) })
	return nil
}

// checkout switches to the ref a caller named, or shows the picker.
func (x *gitscm) checkout(r *repository, detached bool, treeish string) {
	if treeish == "" {
		x.pickCheckout(r, detached)
		return
	}
	x.startCheckout(r, x.refJob(treeish, detached, false), false, nil)
}

// refJob is the switch to a ref by its name; a detached one is labeled with the short hash.
func (x *gitscm) refJob(ref string, detached, dirtyFlow bool) checkoutJob {
	label := ref
	if detached {
		label = x.shortHash(ref)
	}
	return checkoutJob{
		label: label, dirtyFlow: dirtyFlow, pullBefore: !detached && read(x.api, "git.pullBeforeCheckout", false),
		run: func(ctx context.Context, repo *git.Repo, force bool) error {
			return repo.CheckoutRef(ctx, ref, git.CheckoutOptions{Detached: detached, Force: force})
		},
	}
}

// checkoutTypes are the kinds of ref the picker lists, in the order of git.checkoutType; no tags for a detached checkout.
func checkoutTypes(api extension.API, detached bool) []git.RefType {
	var names []string
	switch v, _ := api.Settings().Get("git.checkoutType"); t := v.(type) {
	case string:
		names = []string{t}
	case []any:
		for _, e := range t {
			if s, ok := e.(string); ok {
				names = append(names, s)
			}
		}
	}
	if len(names) == 0 || (len(names) == 1 && names[0] == "all") {
		names = []string{"local", "remote", "tags"}
	}
	var types []git.RefType
	for _, n := range names {
		switch n {
		case "local":
			types = append(types, git.RefHead)
		case "remote":
			types = append(types, git.RefRemoteHead)
		case "tags":
			if !detached {
				types = append(types, git.RefTag)
			}
		}
	}
	return types
}

// loadRefs reads the refs of the repository for a picker off the UI thread and calls fn with them, unless the repository went away
// meanwhile. A failure is the generic one of a command.
func (x *gitscm) loadRefs(r *repository, q git.RefQuery, fn func(refs []git.Ref)) {
	repo, ctx := r.repo, r.ctx
	if repo == nil {
		return
	}
	go func() {
		refs, err := repo.RefsQuery(ctx, q)
		x.api.Post(func() {
			if x.stopped.Load() || r.disposed || ctx.Err() != nil {
				return
			}
			if err != nil {
				x.commandFailed("for-each-ref", r, err, nil)
				return
			}
			fn(refs)
		})
	}()
}

// refQuery is the query of the pickers: sorted by git.branchSortOrder, with the details when git.showReferenceDetails is on.
func (x *gitscm) refQuery(patterns ...string) git.RefQuery {
	sort := read(x.api, "git.branchSortOrder", "committerdate")
	return git.RefQuery{Sort: sort, Details: read(x.api, "git.showReferenceDetails", true), Patterns: patterns}
}

// pickRefs shows the entries in a quick pick and calls fn with the entry chosen and the text typed in the filter. A list with nothing to
// choose says so in a notification.
func (x *gitscm) pickRefs(r *repository, placeholder, empty string, entries []refEntry, fn func(e refEntry, typed string)) {
	if !hasChoice(entries) {
		x.api.Notifications().Info(empty)
		return
	}
	typed := ""
	err := x.api.QuickPick().Show(extension.QuickPickOptions{
		Placeholder: placeholder, Items: pickItems(entries), OnAccept: func(v string) { typed = v },
	}, func(i int) {
		if i >= 0 && i < len(entries) && !entries[i].item.Separator && !r.disposed {
			fn(entries[i], typed)
		}
	})
	if err != nil {
		x.logUnavailable("cannot show the list of refs", err)
	}
}

// hasChoice says whether the entries have a row that can be chosen.
func hasChoice(entries []refEntry) bool {
	for _, e := range entries {
		if !e.item.Separator {
			return true
		}
	}
	return false
}

// pickCheckout shows the picker: "Create new branch...", "Create new branch from...", "Checkout detached..." (always shown, whatever is
// typed), and the refs.
func (x *gitscm) pickCheckout(r *repository, detached bool) {
	x.loadRefs(r, x.refQuery(), func(refs []git.Ref) {
		entries := x.refEntries(r, refs, refListOptions{types: checkoutTypes(x.api, detached), lockProtected: true, keepRemoteHEAD: detached})
		var all []refEntry
		if !detached {
			for _, c := range []struct{ kind, label string }{
				{entryCreate, "$(plus) Create new branch..."}, {entryCreateFrom, "$(plus) Create new branch from..."},
				{entryDetached, "$(debug-disconnect) Checkout detached..."},
			} {
				all = append(all, refEntry{item: extension.QuickPickItem{Label: c.label, AlwaysShow: true}, command: c.kind})
			}
		}
		all = append(all, entries...)
		placeholder := "Select a branch or tag to checkout"
		if detached {
			placeholder = "Select a branch to checkout in detached mode"
		}
		x.pickRefs(r, placeholder, "There are no branches or tags to check out.", all, func(e refEntry, typed string) {
			switch {
			case e.command == entryCreate:
				x.createBranch(r, typed, false, "")
			case e.command == entryCreateFrom:
				x.createBranch(r, typed, true, "")
			case e.command == entryDetached:
				x.checkout(r, true, "")
			case e.ref != nil:
				x.startCheckout(r, x.pickedJob(*e.ref, detached), false, nil)
			}
		})
	})
}

// pickedJob is the switch to the ref the user picked: a local branch or a tag by name, a remote branch as the local branch that tracks it
// (made when there is none), a detached one at the commit.
//
// Ported from VS Code: extensions/git/src/commands.ts (CheckoutItem.run, CheckoutRemoteHeadItem.run, CheckoutTagItem.run)
func (x *gitscm) pickedJob(ref git.Ref, detached bool) checkoutJob {
	switch {
	case detached:
		commit := ref.Commit
		if commit == "" {
			commit = ref.Name
		}
		return x.refJob(commit, true, true)
	case ref.Type == git.RefRemoteHead:
		job := x.refJob(ref.Name, false, true)
		job.run = func(ctx context.Context, repo *git.Repo, force bool) error {
			branches, err := repo.FindTrackingBranches(ctx, ref.Name)
			if err != nil {
				return err
			}
			if len(branches) > 0 {
				return repo.CheckoutRef(ctx, branches[0], git.CheckoutOptions{Force: force})
			}
			return repo.CheckoutRef(ctx, ref.Name, git.CheckoutOptions{Track: true, Force: force})
		}
		return job
	}
	return x.refJob(ref.Name, false, true)
}

// startCheckout runs a switch with its running state in the branch item, and then calls then. Changes that stop it start the dirty
// work tree questions of the job.
func (x *gitscm) startCheckout(r *repository, job checkoutJob, force bool, then func()) {
	r.mutate(mutation{
		name: "checkout",
		begin: func() {
			r.checkingOut, r.checkoutLabel = true, job.label
			r.updateStatusBar()
			r.updateCommitUI()
		},
		finally: func() {
			r.checkingOut, r.checkoutLabel = false, ""
			r.updateStatusBar()
			r.updateCommitUI()
		},
		run:   func(ctx context.Context, repo *git.Repo) error { return job.run(ctx, repo, force) },
		after: then,
		failed: func(err error) bool {
			if job.dirtyFlow && errTag(err) == "DirtyWorkTree" {
				x.dirtyCheckout(r, job)
				return true
			}
			return false
		},
	})
}

// dirtyCheckout is what happens when local changes would be overwritten by the switch: with git.autoStash the changes are stashed, the
// switch is made and they are brought back; else the user chooses to Stash & Checkout (the changes stay in the stash), Migrate Changes
// (stash, switch, pop) or Force Checkout (throw the changes away).
//
// Ported from VS Code: extensions/git/src/commands.ts (_checkout: the catch of DirtyWorkTree)
func (x *gitscm) dirtyCheckout(r *repository, job checkoutJob) {
	job.dirtyFlow = false // a second refusal after the stash is the failure of git
	if read(x.api, "git.autoStash", false) {
		x.stashAndCheckout(r, job, true, true)
		return
	}
	x.ask(extension.DialogSpec{
		Type: extension.DialogWarning, Message: "Your local changes would be overwritten by checkout.",
		Buttons: []extension.DialogButton{{Label: "Stash & Checkout"}, {Label: "Migrate Changes"}, {Label: "Force Checkout"}, cancelButton},
	}, func(res extension.DialogResult) {
		if res.Cancelled || r.disposed {
			return
		}
		switch res.Button {
		case 0:
			x.stashAndCheckout(r, job, false, false)
		case 1:
			x.stashAndCheckout(r, job, true, false)
		case 2:
			x.startCheckout(r, job, true, nil)
		}
	})
}

// stashAndCheckout stashes the changes (the untracked files too), makes the switch and, when migrate is set, pops the stash on the new
// ref. silent makes the stash without the questions of the stash command.
func (x *gitscm) stashAndCheckout(r *repository, job checkoutJob, migrate, silent bool) {
	switchNow := func(stashed bool) {
		if !stashed || r.disposed {
			return
		}
		x.startCheckout(r, job, false, func() {
			if migrate {
				x.applyStash(r, git.StashLatest, true)
			}
		})
	}
	if silent {
		x.createStash(r, true, false, "", switchNow)
		return
	}
	x.stashChanges(r, true, false, nil, switchNow)
}

// refForTarget is the argument of git for the start of a new branch: nothing for HEAD.
func refForTarget(target string) string {
	if target == "HEAD" {
		return ""
	}
	return strings.TrimSpace(target)
}
