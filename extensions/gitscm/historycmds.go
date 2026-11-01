package gitscm

import (
	"context"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// The commands of the Source Control Graph (SPEC §10.30 "The Source Control Graph", step 24.20): what the context menu of a commit and of a
// reference of the Graph run, the copy of a commit, and the opening of a commit or a range of changes. The Graph gives them a list of
// objects, extension.HistoryArgs reads: the source control, the commit and, for a reference, its id.
//
// Ported from VS Code: extensions/git/src/commands.ts (graph.checkout, graph.checkoutDetached, graph.deleteBranch, graph.deleteTag,
// graph.cherryPick, graph.compareRef, graph.compareWithRemote, graph.compareWithMergeBase, copyCommitId, copyCommitMessage, viewCommit,
// _openChangesBetweenRefs)

// historyHandlers are the handlers of the commands of the Graph.
func (x *gitscm) historyHandlers() map[string]extension.CommandHandler {
	return map[string]extension.CommandHandler{
		"git.graph.checkout":             x.cmdGraphCheckout,
		"git.graph.checkoutDetached":     x.cmdGraphCheckoutDetached,
		"git.graph.deleteBranch":         x.cmdGraphDeleteBranch,
		"git.graph.deleteTag":            x.cmdGraphDeleteTag,
		"git.graph.cherryPick":           x.cmdGraphCherryPick,
		"git.graph.compareRef":           x.cmdGraphCompareRef,
		"git.graph.compareWithRemote":    x.cmdGraphCompareWithRemote,
		"git.graph.compareWithMergeBase": x.cmdGraphCompareWithMergeBase,
		"git.copyCommitId":               x.cmdCopyCommitID,
		"git.copyCommitMessage":          x.cmdCopyCommitMessage,
	}
}

// graphItem is the commit a command of the Graph was run for, with the repository: nil when the command was run without one (the palette).
func (x *gitscm) graphItem(args any) (*repository, extension.HistoryArg, bool) {
	arg, ok := extension.HistoryArgs(args)
	if !ok || arg.Item == nil {
		return nil, arg, false
	}
	r := x.targetOf(args)
	if r == nil || r.repo == nil {
		return nil, arg, false
	}
	return r, arg, true
}

// graphRef is the reference of the commit that a menu of a badge named.
func graphRef(arg extension.HistoryArg) (extension.HistoryItemRef, bool) {
	if arg.Item == nil {
		return extension.HistoryItemRef{}, false
	}
	return arg.Item.Ref(arg.RefID)
}

// cmdGraphCheckout is git.graph.checkout: a branch or a tag by name, a remote branch as the local branch that tracks it (made when there
// is none).
func (x *gitscm) cmdGraphCheckout(_ context.Context, args any) error {
	r, arg, ok := x.graphItem(args)
	if !ok {
		return nil
	}
	ref, found := graphRef(arg)
	if !found {
		return nil
	}
	if strings.HasPrefix(ref.ID, "refs/heads/") || strings.HasPrefix(ref.ID, "refs/tags/") {
		x.startCheckout(r, x.refJob(ref.Name, false, false), false, nil)
		return nil
	}
	job := x.refJob(ref.Name, false, false)
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
	x.startCheckout(r, job, false, nil)
	return nil
}

// cmdGraphCheckoutDetached is git.graph.checkoutDetached: the work tree switches to the commit, detached.
func (x *gitscm) cmdGraphCheckoutDetached(_ context.Context, args any) error {
	if r, arg, ok := x.graphItem(args); ok {
		x.startCheckout(r, x.refJob(arg.Item.ID, true, false), false, nil)
	}
	return nil
}

// cmdGraphDeleteBranch is git.graph.deleteBranch: a local branch other than the current one, or a remote branch other than the one the
// current branch tracks (after a question).
func (x *gitscm) cmdGraphDeleteBranch(_ context.Context, args any) error {
	r, arg, ok := x.graphItem(args)
	if !ok {
		return nil
	}
	ref, found := graphRef(arg)
	if !found {
		return nil
	}
	r.hist.mu.Lock()
	cur, remote := r.hist.current, r.hist.remote
	r.hist.mu.Unlock()
	if strings.HasPrefix(ref.ID, "refs/heads/") {
		if cur != nil && ref.ID == cur.ID {
			x.api.Notifications().Info("The active branch cannot be deleted.")
			return nil
		}
		x.runDeleteBranch(r, false, ref.Name)
		return nil
	}
	if remote != nil && ref.ID == remote.ID {
		x.api.Notifications().Info("The remote branch of the active branch cannot be deleted.")
		return nil
	}
	if !strings.Contains(ref.Name, "/") {
		return nil
	}
	x.runDeleteBranch(r, true, ref.Name)
	return nil
}

// cmdGraphDeleteTag is git.graph.deleteTag.
func (x *gitscm) cmdGraphDeleteTag(_ context.Context, args any) error {
	if r, arg, ok := x.graphItem(args); ok {
		if ref, found := graphRef(arg); found {
			x.runDeleteTag(r, ref.Name)
		}
	}
	return nil
}

// cmdGraphCherryPick is git.graph.cherryPick: the changes of the commit are applied on the current branch.
func (x *gitscm) cmdGraphCherryPick(_ context.Context, args any) error {
	if r, arg, ok := x.graphItem(args); ok {
		x.runCherryPick(r, arg.Item.ID)
	}
	return nil
}

// cmdGraphCompareWithRemote is git.graph.compareWithRemote: the changes from the branch the current one tracks to the commit.
func (x *gitscm) cmdGraphCompareWithRemote(_ context.Context, args any) error {
	r, arg, ok := x.graphItem(args)
	if !ok {
		return nil
	}
	r.hist.mu.Lock()
	remote := r.hist.remote
	r.hist.mu.Unlock()
	if remote != nil {
		x.openRefRange(r, refEnd{id: remote.Revision, name: remote.Name}, x.commitEnd(arg.Item))
	}
	return nil
}

// cmdGraphCompareWithMergeBase is git.graph.compareWithMergeBase: the changes from the branch the current one was made from to the commit.
func (x *gitscm) cmdGraphCompareWithMergeBase(_ context.Context, args any) error {
	r, arg, ok := x.graphItem(args)
	if !ok {
		return nil
	}
	r.hist.mu.Lock()
	base := r.hist.base
	r.hist.mu.Unlock()
	if base != nil {
		x.openRefRange(r, refEnd{id: base.Revision, name: base.Name}, x.commitEnd(arg.Item))
	}
	return nil
}

// cmdGraphCompareRef is git.graph.compareRef: the changes from a reference picked from the list to the commit.
func (x *gitscm) cmdGraphCompareRef(_ context.Context, args any) error {
	r, arg, ok := x.graphItem(args)
	if !ok {
		return nil
	}
	x.loadRefs(r, x.refQuery(), func(refs []git.Ref) {
		entries := x.refEntries(r, refs, refListOptions{types: []git.RefType{git.RefHead, git.RefRemoteHead, git.RefTag}})
		x.pickRefs(r, "Select a reference to compare with", "There are no references to compare with.", entries, func(e refEntry, _ string) {
			if e.ref != nil && e.ref.Commit != "" {
				x.openRefRange(r, refEnd{id: e.ref.Commit, name: e.ref.Name}, x.commitEnd(arg.Item))
			}
		})
	})
	return nil
}

// refEnd is one end of a comparison: the commit and the name it is shown with.
type refEnd struct{ id, name string }

// commitEnd is the end of a comparison that is a commit of the Graph: shown by the name of the one reference it has, else its short hash.
//
// Ported from VS Code: commands.ts (getHistoryItemDisplayName)
func (x *gitscm) commitEnd(it *extension.HistoryItem) refEnd {
	name := it.DisplayID
	if name == "" {
		name = x.shortHash(it.ID)
	}
	if len(it.References) > 0 {
		name = it.References[0].Name
	}
	return refEnd{id: it.ID, name: name}
}

// cmdCopyCommitID is git.copyCommitId: the hash of the commit goes to the clipboard.
func (x *gitscm) cmdCopyCommitID(_ context.Context, args any) error {
	if arg, ok := extension.HistoryArgs(args); ok && arg.Item != nil {
		x.copyToClipboard(arg.Item.ID)
	}
	return nil
}

// cmdCopyCommitMessage is git.copyCommitMessage: the whole message of the commit goes to the clipboard.
func (x *gitscm) cmdCopyCommitMessage(_ context.Context, args any) error {
	if arg, ok := extension.HistoryArgs(args); ok && arg.Item != nil {
		x.copyToClipboard(arg.Item.Message)
	}
	return nil
}

// copyToClipboard writes a text to the clipboard; a failure is a notification.
func (x *gitscm) copyToClipboard(text string) {
	if err := x.api.Clipboard().WriteText(text); err != nil {
		x.log.Warn("cannot copy to the clipboard", "err", err)
		x.api.Notifications().Error("Cannot copy to the clipboard: " + describe(err))
	}
}

// rangeKey is the key of the multi-file diff editor of the changes between two commits.
func rangeKey(id, base, head string) string { return "range/" + id + "/" + base + ".." + head }
