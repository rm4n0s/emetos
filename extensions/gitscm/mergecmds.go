package gitscm

import (
	"context"
	"regexp"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// Merge, rebase, cherry-pick and the conflicts they leave (SPEC §10.30 "Branches, tags, merge and the states in between"): the three
// commands that start the operations and their aborts, "Compute Conflicts With Git", and the staging of a file when the merge editor
// completes it. The operations that stop on conflicts leave the repository in the state git keeps in its files; the Merge Changes group,
// the branch item and the box of the Source Control view follow it (head.go, commitui.go), and the commit continues it (commit.go).
//
// Ported from VS Code: extensions/git/src/commands.ts (merge, mergeAbort, rebase, rebaseAbort, cherryPick, cherryPickAbort, acceptMerge,
// runGitMerge, MergeItem, RebaseItem, RebaseUpstreamItem) and repository.ts (merge, rebase, cherryPick)

// operationFailed is the failure handler of the commands that start an operation: a conflict is a warning that points to the merge
// changes (VS Code's button "Show Changes" opens the Source Control view, which an extension cannot do; the view shows them by itself),
// a pick whose changes are in the branch already is information, and everything else is the generic failure.
//
// Ported from VS Code: extensions/git/src/commands.ts (createCommand: the catch for Conflict, CherryPickConflict and CherryPickEmpty)
func (x *gitscm) operationFailed(err error) bool {
	switch errTag(err) {
	case "Conflict":
		x.api.Notifications().Warn("There are merge conflicts. Please resolve them before committing your changes.")
		return true
	case "CherryPickConflict":
		x.api.Notifications().Warn("There were merge conflicts while cherry picking the changes. Resolve the conflicts before committing them.")
		return true
	case "CherryPickEmpty":
		x.api.Notifications().Info("The changes are already present in the current branch.")
		return true
	}
	return false
}

// pickRefName is the name git is given for a row of a list of refs: the name, else the commit of a ref that has no name.
func pickRefName(e refEntry) string {
	if e.ref == nil {
		return ""
	}
	if e.ref.Name != "" {
		return e.ref.Name
	}
	return e.ref.Commit
}

// cmdMerge is git.merge, "Merge...": a pick of a branch, a remote branch or a tag of the other refs, merged into the current branch. A
// caller can name the ref ({"ref": "feature"}) and is not asked.
//
// Ported from VS Code: extensions/git/src/commands.ts (merge, MergeItem)
func (x *gitscm) cmdMerge(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		if ref := refArg(args); ref != "" {
			x.runMerge(r, ref)
			return
		}
		x.loadRefs(r, x.refQuery(), func(refs []git.Ref) {
			entries := x.refEntries(r, refs, refListOptions{
				types:             []git.RefType{git.RefHead, git.RefRemoteHead, git.RefTag},
				skipCurrentBranch: true, skipCurrentBranchRemote: true,
			})
			x.pickRefs(r, "Select a branch or tag to merge from", "There are no branches or tags to merge.", entries, func(e refEntry, _ string) {
				if name := pickRefName(e); name != "" {
					x.runMerge(r, name)
				}
			})
		})
	})
	return nil
}

// runMerge merges the ref into the current branch.
func (x *gitscm) runMerge(r *repository, ref string) {
	r.mutate(mutation{
		name:   "merge",
		run:    func(ctx context.Context, repo *git.Repo) error { return repo.Merge(ctx, ref) },
		failed: x.operationFailed,
	})
}

// cmdMergeAbort is git.mergeAbort: the merge that has conflicts is undone.
func (x *gitscm) cmdMergeAbort(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		r.mutate(mutation{name: "merge --abort", run: func(ctx context.Context, repo *git.Repo) error { return repo.MergeAbort(ctx) }})
	})
	return nil
}

// cmdRebase is git.rebase, "Rebase Branch...": a pick of the branch (the upstream first) the current branch is put on. A caller can name
// the ref ({"ref": "main"}).
//
// Ported from VS Code: extensions/git/src/commands.ts (rebase, RebaseItem, RebaseUpstreamItem)
func (x *gitscm) cmdRebase(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { x.rebase(r, args, false) })
	return nil
}

// cmdRebaseInteractive is git.rebaseInteractive, "Rebase Branch (Interactive)...": the same pick, and git starts its editor with the list of
// commits, which the window opens in an editor tab; closing the tab after a save goes on, closing it without a save stops the rebase.
// Emetos's own command (VS Code has none).
func (x *gitscm) cmdRebaseInteractive(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		if !x.editorOn.Load() {
			x.api.Notifications().Warn(`An interactive rebase shows the list of commits in an editor tab. Turn on "git.useEditorAsCommitInput" for it.`)
			return
		}
		x.rebase(r, args, true)
	})
	return nil
}

// rebase asks for the branch and rebases the current branch on it.
func (x *gitscm) rebase(r *repository, args any, interactive bool) {
	run := func(branch string) {
		name := "rebase"
		if interactive {
			name = "rebase -i"
		}
		r.mutate(mutation{
			name: name,
			run: func(ctx context.Context, repo *git.Repo) error {
				return repo.Rebase(ctx, branch, git.RebaseOptions{Interactive: interactive})
			},
			failed: x.operationFailed,
		})
	}
	if ref := refArg(args); ref != "" {
		run(ref)
		return
	}
	x.loadRefs(r, x.refQuery("refs/heads", "refs/remotes"), func(refs []git.Ref) {
		entries := x.refEntries(r, refs, refListOptions{
			types:             []git.RefType{git.RefHead, git.RefRemoteHead},
			skipCurrentBranch: true, skipCurrentBranchRemote: true,
		})
		// the upstream of the branch comes first, marked as such
		if up := r.head.upstream; up != nil {
			for i := range refs {
				ref := &refs[i]
				if ref.Type == git.RefRemoteHead && ref.Name == up.Remote+"/"+up.Name {
					item := extension.QuickPickItem{Label: "$(cloud) " + ref.Name, Description: "(upstream)"}
					entries = append([]refEntry{{item: item, ref: ref}}, entries...)
					break
				}
			}
		}
		x.pickRefs(r, "Select a branch to rebase onto", "There are no branches to rebase onto.", entries, func(e refEntry, _ string) {
			if e.ref != nil && e.ref.Name != "" {
				run(e.ref.Name)
			}
		})
	})
}

// cmdRebaseAbort is git.rebaseAbort: the rebase that stopped is undone. Without one it says so.
func (x *gitscm) cmdRebaseAbort(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		if r.ops.rebaseCommit == "" {
			x.api.Notifications().Info("No rebase in progress.")
			return
		}
		r.mutate(mutation{name: "rebase --abort", run: func(ctx context.Context, repo *git.Repo) error { return repo.RebaseAbort(ctx) }})
	})
	return nil
}

// cmdRebaseSkip is git.rebaseSkip: the commit the rebase stopped at is left out. Emetos's own command (VS Code has none); the way out of
// a commit whose changes the branch has already.
func (x *gitscm) cmdRebaseSkip(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		if r.ops.rebaseCommit == "" {
			x.api.Notifications().Info("No rebase in progress.")
			return
		}
		r.mutate(mutation{
			name:   "rebase --skip",
			run:    func(ctx context.Context, repo *git.Repo) error { return repo.RebaseSkip(ctx) },
			failed: x.operationFailed,
		})
	})
	return nil
}

// cmdCherryPick is git.cherryPick, "Cherry Pick...": the hash of the commit is asked for in an input box. A caller can name it
// ({"ref": "abc1234"}).
//
// Ported from VS Code: extensions/git/src/commands.ts (cherryPick)
func (x *gitscm) cmdCherryPick(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		if ref := refArg(args); ref != "" {
			x.runCherryPick(r, ref)
			return
		}
		err := x.api.InputBox().Show(extension.InputBoxOptions{
			Prompt: "Commit Hash", Message: "Please provide the commit hash", IgnoreFocusOut: true,
		}, func(hash string, ok bool) {
			if ok && strings.TrimSpace(hash) != "" && !r.disposed {
				x.runCherryPick(r, strings.TrimSpace(hash))
			}
		})
		if err != nil {
			x.logUnavailable("cannot ask for the commit to cherry pick", err)
		}
	})
	return nil
}

// runCherryPick applies the changes of the commit on the current branch.
func (x *gitscm) runCherryPick(r *repository, commit string) {
	r.mutate(mutation{
		name:   "cherry-pick",
		run:    func(ctx context.Context, repo *git.Repo) error { return repo.CherryPick(ctx, commit) },
		failed: x.operationFailed,
	})
}

// cmdCherryPickAbort is git.cherryPickAbort: the pick that has conflicts is undone.
func (x *gitscm) cmdCherryPickAbort(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		r.mutate(mutation{name: "cherry-pick --abort", run: func(ctx context.Context, repo *git.Repo) error { return repo.CherryPickAbort(ctx) }})
	})
	return nil
}

// ---- the merge editor ----

// onMergeCompleted hears that the merge editor completed a merge and stages its file.
func (x *gitscm) onMergeCompleted(ev extension.Event) {
	var done extension.MergeEditorCompleted
	if err := jsonUnmarshal(ev.Data, &done); err != nil {
		return
	}
	x.acceptMerge(done.URI)
}

// acceptMerge stages a file the merge editor completed, when it is in the Merge Changes group: the result was saved already, with the
// conflicts handled or accepted by the user (the merge editor asks first), so no question comes here.
//
// Ported from VS Code: extensions/git/src/commands.ts (acceptMerge)
func (x *gitscm) acceptMerge(uri string) {
	path, ok := uriPath(uri)
	if !ok {
		return
	}
	r := x.model.forPath(path)
	if r == nil || r.repo == nil {
		return
	}
	if _, inMerge := r.findResource(groupMerge, path); !inMerge {
		return
	}
	r.stagePaths([]string{path}, false)
}

// cmdAcceptMerge is git.acceptMerge, "Complete Merge" of Git: it stages the file named by {"uri": "file:///..."} when the file is in the
// Merge Changes group. The merge editor's own Complete Merge saves the result and then publishes emetos.mergeEditor.completed, on which
// this runs by itself; the command is for a caller that completed the merge some other way.
func (x *gitscm) cmdAcceptMerge(_ context.Context, args any) error {
	if uri := x.uriArg(args); uri != "" {
		x.acceptMerge(uri)
	}
	return nil
}

// mergeLabelRE reads the name of the other side from the message git prepares for a merge.
var mergeLabelRE = regexp.MustCompile(`^Merge (?:remote-tracking )?(?:branch|tag|commit) '([^']+)'`)

// theirsLabel is how the markers that "Compute Conflicts With Git" writes name the other side: the branch of a merge, the commit a rebase
// or a cherry-pick applies.
func (r *repository) theirsLabel() string {
	switch {
	case r.ops.rebaseCommit != "":
		label := r.m.x.shortHash(r.ops.rebaseCommit)
		if subject, _, _ := strings.Cut(r.rebaseMessage, "\n"); subject != "" {
			label += " (" + subject + ")"
		}
		return label
	case r.ops.merge:
		if r.repo != nil {
			subject, _, _ := strings.Cut(r.repo.MergeMessage(), "\n")
			if m := mergeLabelRE.FindStringSubmatch(subject); m != nil {
				return m[1]
			}
		}
		return "MERGE_HEAD"
	case r.ops.cherryPick:
		return "CHERRY_PICK_HEAD"
	case r.ops.revert:
		return "REVERT_HEAD"
	}
	return "theirs"
}

// cmdRunGitMerge is git.runGitMerge, "Compute Conflicts With Git": the result of the merge editor in front is replaced by what
// "git merge-file" makes of the three stages of its file.
func (x *gitscm) cmdRunGitMerge(_ context.Context, _ any) error {
	x.runGitMerge(false)
	return nil
}

// cmdRunGitMergeDiff3 is git.runGitMergeDiff3: the same with the base in the markers (--diff3).
func (x *gitscm) cmdRunGitMergeDiff3(_ context.Context, _ any) error {
	x.runGitMerge(true)
	return nil
}

// runGitMerge makes the merge of the stages of the file of the merge editor that is in front, off the UI thread, and replaces the
// result document with it as one edit that can be undone.
//
// Ported from VS Code: extensions/git/src/commands.ts (runGitMerge)
func (x *gitscm) runGitMerge(diff3 bool) {
	m, ok := x.api.Documents().ActiveMergeEditor()
	if !ok {
		return
	}
	path, ok := uriPath(m.Result)
	if !ok {
		return
	}
	r := x.model.forPath(path)
	if r == nil || r.repo == nil {
		x.api.Notifications().Info("This merge is not of a file of an open Git repository.")
		return
	}
	repo, ctx, ours := r.repo, r.ctx, "HEAD"
	theirs := r.theirsLabel()
	go func() {
		res, err := repo.MergeFile(ctx, path, git.MergeFileOptions{Diff3: diff3, Ours: ours, Theirs: theirs})
		x.api.Post(func() {
			if x.stopped.Load() || r.disposed || ctx.Err() != nil {
				return
			}
			if err != nil {
				x.commandFailed("merge-file", r, err, nil)
				return
			}
			x.replaceResult(m.Result, res.Text)
		})
	}()
}

// replaceResult makes the text of the result document the text, with one edit.
func (x *gitscm) replaceResult(uri, text string) {
	current, err := x.api.Documents().Read(x.ctx, uri)
	if err != nil {
		x.log.Warn("cannot read the result of the merge", "uri", uri, "err", err)
		return
	}
	edit, changed := replaceEdit(current, text)
	if !changed {
		return
	}
	err = x.api.Documents().Apply(x.ctx, extension.WorkspaceEdit{Changes: []extension.ResourceChange{{Kind: "edit", URI: uri, Edits: []extension.RangeEdit{edit}}}})
	if err != nil {
		x.log.Warn("cannot change the result of the merge", "uri", uri, "err", err)
		x.api.Notifications().Error("Git: cannot change the result of the merge: " + describe(err))
	}
}

// mergeMenus are the buttons over the content of an editor: Resolve in Merge Editor for a text editor of a file that is in Merge Changes
// and has conflict markers, and Complete Merge in the merge editor of such a file. (Emetos draws no strip of buttons over an editor yet:
// the notification of §9.11 and the Complete Merge button of the merge editor do the same; the items are registered as VS Code lists
// them, for the day the window shows them.)
//
// Ported from VS Code: extensions/git/package.json (contributes.menus: editor/content)
func mergeMenus() []extension.MenuItem {
	return []extension.MenuItem{
		item(extension.MenuEditorContent, "git.acceptMerge", "", "isMergeResultEditor && mergeEditorResultUri in git.mergeChanges"),
		item(extension.MenuEditorContent, "git.openMergeEditor", "navigation@-10",
			"config.git.enabled && !git.missing && !isInDiffEditor && !isMergeEditor && resource in git.mergeChanges && git.activeResourceHasMergeConflicts"),
	}
}
