package gitscm

import (
	"context"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// Branches (SPEC §10.30 "Branches, tags, merge and the states in between"): make a branch, from HEAD or from a ref, rename the current
// one, delete a local or a remote branch.
//
// Ported from VS Code: extensions/git/src/commands.ts (branch, branchFrom, _branch, renameBranch, deleteBranch, deleteRemoteBranch,
// _deleteBranch, BranchDeleteItem, HEADItem) and extensions/git/src/repository.ts (branch, renameBranch, deleteBranch, deleteRemoteRef)

// nameArg is the name a caller gave ({"name": "topic"}): the prompt is not shown for it.
func nameArg(args any) string {
	if m, ok := args.(map[string]any); ok {
		if s, ok := m["name"].(string); ok {
			return s
		}
	}
	return ""
}

func (x *gitscm) cmdBranch(_ context.Context, args any) error {
	// the Graph runs it for a commit: the branch starts there (VS Code's branch(repository, historyItem))
	target := ""
	if arg, ok := extension.HistoryArgs(args); ok && arg.Item != nil {
		target = arg.Item.ID
	}
	x.target(args, true, func(r *repository) { x.createBranch(r, nameArg(args), false, target) })
	return nil
}

func (x *gitscm) cmdBranchFrom(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { x.createBranch(r, nameArg(args), true, "") })
	return nil
}

// createBranch asks for the name of a new branch (defaultName, when there is one, is the name and nothing is asked), and for the ref it
// starts at when from is set (target is that ref, HEAD when empty), makes the branch and switches to it.
func (x *gitscm) createBranch(r *repository, defaultName string, from bool, target string) {
	if target == "" {
		target = "HEAD"
	}
	makeIt := func(target string) {
		x.promptBranchName(r, defaultName, "", func(name string) {
			r.mutate(mutation{
				name: "branch",
				run: func(ctx context.Context, repo *git.Repo) error {
					return repo.Branch(ctx, name, true, refForTarget(target))
				},
			})
		})
	}
	if !from {
		makeIt(target)
		return
	}
	x.loadRefs(r, x.refQuery(), func(refs []git.Ref) {
		entries := []refEntry{{item: extension.QuickPickItem{Label: "HEAD", Description: x.shortHash(r.head.commit), AlwaysShow: true}, command: "HEAD"}}
		entries = append(entries, x.refEntries(r, refs, refListOptions{types: []git.RefType{git.RefHead, git.RefRemoteHead, git.RefTag}})...)
		x.pickRefs(r, "Select a ref to create the branch from", "There are no refs to create a branch from.", entries, func(e refEntry, _ string) {
			if e.ref != nil && e.ref.Name != "" {
				makeIt(e.ref.Name)
				return
			}
			makeIt("HEAD")
		})
	})
}

func (x *gitscm) cmdRenameBranch(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		if r.head.name == "" {
			x.api.Notifications().Info("Please check out a branch to rename it.")
			return
		}
		x.promptBranchName(r, nameArg(args), r.head.name, func(name string) {
			r.mutate(mutation{
				name: "rename branch",
				run:  func(ctx context.Context, repo *git.Repo) error { return repo.RenameBranch(ctx, name) },
				failed: func(err error) bool {
					switch errTag(err) {
					case "InvalidBranchName":
						x.api.Notifications().Error("Invalid branch name")
						return true
					case "BranchAlreadyExists":
						x.api.Notifications().Error(`A branch named "` + name + `" already exists`)
						return true
					}
					return false
				},
			})
		})
	})
	return nil
}

func (x *gitscm) cmdDeleteBranch(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { x.deleteBranch(r, false, nameArg(args)) })
	return nil
}

func (x *gitscm) cmdDeleteRemoteBranch(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { x.deleteBranch(r, true, nameArg(args)) })
	return nil
}

// deleteBranch deletes the branch with the name a caller gave, or the one picked from the list of the local branches (the current one is
// not in it) or of the remote ones. A remote branch is deleted after a question.
func (x *gitscm) deleteBranch(r *repository, remote bool, name string) {
	if name != "" {
		x.runDeleteBranch(r, remote, name)
		return
	}
	pattern, types, placeholder := "refs/heads", []git.RefType{git.RefHead}, "Select a branch to delete"
	if remote {
		pattern, types, placeholder = "refs/remotes", []git.RefType{git.RefRemoteHead}, "Select a remote branch to delete"
	}
	x.loadRefs(r, x.refQuery(pattern), func(refs []git.Ref) {
		entries := x.refEntries(r, refs, refListOptions{types: types, skipCurrentBranch: true, skipCurrentBranchRemote: true})
		x.pickRefs(r, placeholder, "There are no branches to delete.", entries, func(e refEntry, _ string) {
			if e.ref != nil {
				x.runDeleteBranch(r, remote, e.ref.Name)
			}
		})
	})
}

// runDeleteBranch deletes a local branch, or a remote-tracking name ("origin/feature") at its remote after a question. A branch that is not
// fully merged is deleted after a second question.
func (x *gitscm) runDeleteBranch(r *repository, remote bool, name string) {
	var run func(force bool)
	run = func(force bool) {
		r.mutate(mutation{
			name: "delete branch",
			run: func(ctx context.Context, repo *git.Repo) error {
				if !remote {
					return repo.DeleteBranch(ctx, name, force)
				}
				at, ref, _ := strings.Cut(name, "/")
				return repo.DeleteRemoteRef(ctx, at, ref, force)
			},
			failed: func(err error) bool {
				if force || errTag(err) != "BranchNotFullyMerged" {
					return false
				}
				x.confirm(extension.DialogSpec{
					Type: extension.DialogWarning, Message: `The branch "` + name + `" is not fully merged. Delete anyway?`,
					Buttons: []extension.DialogButton{{Label: "Delete Branch"}, cancelButton},
				}, func() { run(true) })
				return true
			},
		})
	}
	if !remote {
		run(false)
		return
	}
	at, ref, _ := strings.Cut(name, "/")
	x.confirm(extension.DialogSpec{
		Type: extension.DialogWarning, Message: `Are you sure you want to delete the branch "` + ref + `" from the remote "` + at + `"?`,
		Detail:  "This deletes the branch on the server.",
		Buttons: []extension.DialogButton{{Label: "Delete Remote Branch"}, cancelButton},
	}, func() { run(false) })
}
