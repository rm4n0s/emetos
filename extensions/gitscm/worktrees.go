package gitscm

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// Worktrees (SPEC §10.30 "Worktrees"): create, open and delete linked worktrees, move the changes of one to the repository, and compare a
// file of one with the main work tree.
//
// Ported from VS Code: extensions/git/src/commands.ts (createWorktree, _createWorktree, getWorktreeCommitishAndBranch, getWorktreePath,
// handleWorktreeConflict, handleWorktreeAlreadyExists, handleWorktreeBranchAlreadyUsed, deleteWorktreeFromPalette, deleteWorktree,
// openWorktreeInCurrentWindow, openWorktreeInNewWindow, migrateWorktreeChanges, openWorktreeMergeEditor, compareWithWorkspace) and
// extensions/git/src/repository.ts (createWorktree, deleteWorktree, migrateChanges, resolveCompareWithWorkspaceCommand)

const (
	// worktreesSubmenu is the id of the Worktrees submenu of the "..." menus of the Source Control view.
	worktreesSubmenu = "git.worktrees"
	// commandOpenWorktreeMergeEditor is git.openWorktreeMergeEditor: the merge editor of a file of a migration, with the sides named
	// "Workspace" and "Worktree". VS Code has it as a command that no menu shows, and so has Emetos.
	commandOpenWorktreeMergeEditor = "git.openWorktreeMergeEditor"
)

// mainKinds are the kinds of repository that make, delete and migrate worktrees (VS Code's repositoryFilter ['repository', 'submodule']).
var mainKinds = []git.Kind{git.KindPlain, git.KindSubmodule}

// reBranchUsed reads the branch and the work tree of git's refusal to check a branch out in a second place.
var reBranchUsed = regexp.MustCompile(`fatal: '([^']+)' is already (?:used by worktree|checked out) at '([^']+)'`)

// reWorktreeTaken reads the folder of git's refusal to add a worktree where a folder is.
var reWorktreeTaken = regexp.MustCompile(`fatal: '([^']+)' already exists`)

// targetKinds finds the repository a command is for, as target does, but a repository that has to be picked is one of the kinds.
func (x *gitscm) targetKinds(args any, kinds []git.Kind, fn func(*repository)) {
	if r := x.targetOf(args); r != nil {
		fn(r)
		return
	}
	var list []*repository
	for _, r := range x.model.order {
		if r.repo != nil && slices.Contains(kinds, r.kind()) {
			list = append(list, r)
		}
	}
	switch len(list) {
	case 0:
		x.api.Notifications().Info("There are no available repositories matching the filter.")
	case 1:
		fn(list[0])
	default:
		x.pickRepository(list, "Choose a repository", fn)
	}
}

// samePath says whether two paths name the same folder, comparing their real paths.
func samePath(a, b string) bool {
	return git.Within(realOf(a), realOf(b)) && git.Within(realOf(b), realOf(a))
}

// realOf cleans a path and resolves its symbolic links when it can.
func realOf(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// worktreeFolderName is the name of the folder of a worktree made for a branch or tag: the name without git.branchPrefix, and with "/"
// as "-".
func worktreeFolderName(prefix, ref string) string {
	return strings.ReplaceAll(strings.TrimPrefix(ref, prefix), "/", "-")
}

// uniqueWorktreePath is the path, or when a worktree has it already the path with -1, -2... added to it.
//
// Ported from VS Code: extensions/git/src/repository.ts (createWorktree)
func uniqueWorktreePath(list []git.Worktree, path string) string {
	taken := func(p string) bool {
		_, ok := git.FindWorktree(list, p)
		return ok
	}
	if !taken(path) {
		return path
	}
	for n := 1; ; n++ {
		if candidate := path + "-" + strconv.Itoa(n); !taken(candidate) {
			return candidate
		}
	}
}

// ---- create ----

func (x *gitscm) cmdCreateWorktree(_ context.Context, args any) error {
	x.targetKinds(args, mainKinds, x.createWorktree)
	return nil
}

// createWorktree asks which branch or tag the worktree starts at (or for a new branch), then where to put it.
//
// Ported from VS Code: extensions/git/src/commands.ts (_createWorktree, getWorktreeCommitishAndBranch)
func (x *gitscm) createWorktree(r *repository) {
	repo, ctx := r.repo, r.ctx
	if repo == nil {
		return
	}
	query := x.refQuery()
	go func() {
		refs, err := repo.RefsQuery(ctx, query)
		var list []git.Worktree
		if err == nil {
			list, err = repo.Worktrees(ctx)
		}
		x.api.Post(func() {
			if x.stopped.Load() || r.disposed || ctx.Err() != nil {
				return
			}
			if err != nil {
				x.commandFailed("worktree list", r, err, nil)
				return
			}
			x.pickWorktreeStart(r, refs, list)
		})
	}()
}

// pickWorktreeStart shows the branches, remote branches and tags, with "Create new branch..." before them.
func (x *gitscm) pickWorktreeStart(r *repository, refs []git.Ref, worktrees []git.Worktree) {
	entries := x.refEntries(r, refs, refListOptions{types: []git.RefType{git.RefHead, git.RefRemoteHead, git.RefTag}})
	all := []refEntry{
		{item: extension.QuickPickItem{Label: "$(plus) Create new branch...", AlwaysShow: true}, command: entryCreate},
		{item: extension.QuickPickItem{Separator: true}},
	}
	all = append(all, entries...)
	x.pickRefs(r, "Select a branch or tag to create the new worktree from", "", all, func(e refEntry, _ string) {
		if e.command == entryCreate {
			x.newBranchWorktree(r)
			return
		}
		if e.ref == nil || e.ref.Name == "" {
			return
		}
		name := e.ref.Name
		if name == r.head.name {
			x.ask(extension.DialogSpec{
				Type: extension.DialogWarning, Message: `Branch "` + name + `" is already checked out in the current repository.`,
				Buttons: []extension.DialogButton{{Label: "Create New Branch"}, cancelButton},
			}, func(res extension.DialogResult) {
				if !res.Cancelled && res.Button == 0 && !r.disposed {
					x.newBranchWorktree(r)
				}
			})
			return
		}
		if e.ref.Type == git.RefHead {
			for _, w := range worktrees {
				if w.Ref == "refs/heads/"+name {
					x.worktreeConflict(w.Path, `Branch "`+name+`" is already checked out in the worktree at "`+w.Path+`".`)
					return
				}
			}
		}
		x.worktreeAt(r, name, "")
	})
}

// newBranchWorktree asks for the name of a new branch and makes the worktree on it, starting at HEAD.
func (x *gitscm) newBranchWorktree(r *repository) {
	x.promptBranchName(r, "", "", func(branch string) { x.worktreeAt(r, "HEAD", branch) })
}

// worktreeAt asks where the worktree goes and makes it.
func (x *gitscm) worktreeAt(r *repository, commitish, branch string) {
	prefix := read(x.api, "git.branchPrefix", "")
	ref := commitish
	if branch != "" {
		ref = branch
	}
	x.askWorktreePath(r, worktreeFolderName(prefix, ref), func(path string) { x.addWorktree(r, path, commitish, branch) })
}

// defaultWorktreeRoot is the folder the worktrees of a repository are put in: the one of the last worktree made for it, else
// <repository>.worktrees beside it (a worktree's own neighbours when the repository is one).
func (x *gitscm) defaultWorktreeRoot(r *repository) string {
	if root := x.state.get().WorktreeRoots[r.root]; root != "" {
		return root
	}
	if r.kind() == git.KindWorktree {
		return filepath.Dir(r.root)
	}
	return filepath.Join(filepath.Dir(r.root), baseName(r.root)+".worktrees")
}

// askWorktreePath asks for the path of the worktree in an input box that starts with <root>/<name> and has a button that chooses the
// folder of the destination.
//
// Ported from VS Code: extensions/git/src/commands.ts (getWorktreePath)
func (x *gitscm) askWorktreePath(r *repository, name string, done func(path string)) {
	root := x.defaultWorktreeRoot(r)
	start := filepath.Dir(r.root)
	if stored := x.state.get().WorktreeRoots[r.root]; stored != "" {
		start = stored
	}
	err := x.api.InputBox().Show(extension.InputBoxOptions{
		Prompt: "Worktree path", Message: "Please provide a worktree path", Value: filepath.Join(root, name), IgnoreFocusOut: true,
		Buttons: []extension.InputBoxButton{{Icon: "folder", Tooltip: "Select Worktree Destination"}},
		OnButton: func(_ int, _ string, set func(string)) {
			x.chooseFolder(extension.FileChooserOptions{Title: "Select Worktree Destination", AcceptLabel: "Select as Worktree Destination", Folder: start}, func(folder string) {
				if folder != "" {
					set(filepath.Join(folder, name))
				}
			})
		},
	}, func(value string, ok bool) {
		if ok && value != "" && !r.disposed {
			done(value)
		}
	})
	if err != nil {
		x.logUnavailable("cannot ask for the path of a worktree", err)
	}
}

// addWorktree runs git worktree add. A path that a worktree has already gets -1, -2... added; a folder that is taken, and a branch that is
// checked out elsewhere, offer to open what is there.
//
// Ported from VS Code: extensions/git/src/repository.ts (createWorktree) and extensions/git/src/commands.ts (_createWorktree)
func (x *gitscm) addWorktree(r *repository, path, commitish, branch string) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(r.root, path)
	}
	path = filepath.Clean(path)
	var final string
	r.mutate(mutation{
		name: "worktree add",
		run: func(ctx context.Context, repo *git.Repo) error {
			list, err := repo.Worktrees(ctx)
			if err != nil {
				return err
			}
			final = uniqueWorktreePath(list, path)
			return repo.AddWorktree(ctx, git.AddWorktreeOptions{Path: final, Commitish: commitish, Branch: branch})
		},
		after: func() { x.worktreeCreated(r, final, path) },
		failed: func(err error) bool {
			switch errTag(err) {
			case "WorktreeAlreadyExists":
				if m := reWorktreeTaken.FindStringSubmatch(metaString(err, "stderr")); m != nil {
					return x.worktreeConflict(m[1], `A worktree already exists at "`+m[1]+`".`)
				}
			case "WorktreeBranchAlreadyUsed":
				return x.worktreeBranchUsed(r, err)
			}
			return false
		},
	})
}

// worktreeCreated remembers the folder the worktrees of the repository are put in, starts the setup of the new one, and offers to open it.
func (x *gitscm) worktreeCreated(r *repository, path, requested string) {
	if r.disposed {
		return
	}
	if root := filepath.Dir(path); !samePath(root, x.defaultWorktreeRoot(r)) {
		x.state.update(x, func(s *profileState) {
			if s.WorktreeRoots == nil {
				s.WorktreeRoots = map[string]string{}
			}
			s.WorktreeRoots[r.root] = root
		})
	}
	if path != requested {
		x.api.Notifications().Info(`A worktree already exists at "` + requested + `". The new worktree was made at "` + path + `".`)
	}
	x.setupWorktree(r, path)
	x.api.Notifications().Info(`The worktree "`+baseName(path)+`" was created at "`+path+`". Open the worktree?`,
		extension.NotificationAction{Label: "Open Worktree in Current Window", Run: func() { x.openFolder(path, false) }},
		extension.NotificationAction{Label: "Open Worktree in New Window", Run: func() { x.openFolder(path, true) }})
}

// worktreeBranchUsed is the failure of a checkout or a worktree that wants a branch that another work tree has: it offers to open that one.
// It is false when git did not say which one.
//
// Ported from VS Code: extensions/git/src/commands.ts (handleWorktreeBranchAlreadyUsed, and the catch of _checkout)
func (x *gitscm) worktreeBranchUsed(r *repository, err error) bool {
	m := reBranchUsed.FindStringSubmatch(metaString(err, "stderr"))
	if m == nil {
		return false
	}
	branch, path := m[1], m[2]
	// a worktree that wants the branch of the main work tree, which is open in the view
	if r.kind() == git.KindWorktree && r.loc.Parent != "" && samePath(path, r.loc.Parent) {
		if main := x.model.byRoot(r.loc.Parent); main != nil {
			x.ask(extension.DialogSpec{
				Type: extension.DialogError, Message: `Branch "` + branch + `" is already checked out in the current window.`,
				Buttons: []extension.DialogButton{{Label: "OK", Cancel: true}},
			}, func(extension.DialogResult) {})
			return true
		}
	}
	return x.worktreeConflict(path, `Branch "`+branch+`" is already checked out in the worktree at "`+path+`".`)
}

// worktreeConflict tells the user that a worktree or a branch is there already and offers to open the worktree, in this tab or in a
// new one. The repository of the folder is opened in the view first. It is false when the folder is no repository, and the failure is
// told in the usual way.
//
// Ported from VS Code: extensions/git/src/commands.ts (handleWorktreeConflict)
func (x *gitscm) worktreeConflict(path, message string) bool {
	repo := x.openRepositoryAt(path)
	if repo == nil {
		return false
	}
	root := repo.root
	x.ask(extension.DialogSpec{
		Type: extension.DialogWarning, Message: message,
		Buttons: []extension.DialogButton{{Label: "Open Worktree in Current Window"}, {Label: "Open Worktree in New Window"}, cancelButton},
	}, func(res extension.DialogResult) {
		switch {
		case res.Cancelled:
		case res.Button == 0:
			x.openFolder(root, false)
		case res.Button == 1:
			x.openFolder(root, true)
		}
	})
	return true
}

// openRepositoryAt makes the repository at a folder one of the view (also one the user closed) and returns it; nil when the folder is not
// the top level of a repository.
func (x *gitscm) openRepositoryAt(path string) *repository {
	loc, ok := git.RepositoryAt(path)
	if !ok {
		return nil
	}
	if r := x.model.byRoot(loc.Root); r != nil {
		return r
	}
	return x.model.openRepository(candidate{loc: loc, why: foundWorktree}, true)
}

// withRepositoryAt opens the repository at a folder in the view, waits for git to have opened it and read its status, and calls fn
// with it. Nothing is called for a folder that is no repository, or one that does not open.
func (x *gitscm) withRepositoryAt(path string, fn func(*repository)) {
	r := x.openRepositoryAt(path)
	if r == nil {
		return
	}
	switch {
	case r.repo != nil:
		r.whenFresh(func() { fn(r) })
	case !r.disposed:
		r.afterOpen = append(r.afterOpen, func() { fn(r) })
		x.model.askNext() // a repository that waits for the trust question asks it now; its answer starts it
	}
}

// ---- open ----

func (x *gitscm) cmdOpenWorktree(_ context.Context, args any) error {
	x.target(args, false, func(r *repository) { x.openFolder(r.root, false) })
	return nil
}

func (x *gitscm) cmdOpenWorktreeInNewWindow(_ context.Context, args any) error {
	x.target(args, false, func(r *repository) { x.openFolder(r.root, true) })
	return nil
}

// ---- delete ----

// worktreeFolderOfTab says whether a folder is the one the window tab shows or holds it: such a worktree is the one you are in.
func (x *gitscm) worktreeFolderOfTab(path string) bool {
	return x.folder != "" && git.Within(realOf(x.folder), realOf(path))
}

func (x *gitscm) cmdDeleteWorktree(_ context.Context, args any) error {
	x.targetKinds(args, mainKinds, x.pickWorktreeToDelete)
	return nil
}

// pickWorktreeToDelete lists the linked worktrees with their branch, short hash and subject.
//
// Ported from VS Code: extensions/git/src/commands.ts (deleteWorktreeFromPalette, WorktreeDeleteItem)
func (x *gitscm) pickWorktreeToDelete(r *repository) {
	repo, ctx := r.repo, r.ctx
	if repo == nil {
		return
	}
	go func() {
		list, err := repo.WorktreeDetails(ctx)
		x.api.Post(func() {
			if x.stopped.Load() || r.disposed || ctx.Err() != nil {
				return
			}
			if err != nil {
				x.commandFailed("worktree list", r, err, nil)
				return
			}
			linked := linkedWorktrees(list)
			items := make([]extension.QuickPickItem, 0, len(linked))
			for _, w := range linked {
				branch := w.Branch()
				if w.Detached {
					branch = "detached"
				}
				var parts []string
				for _, p := range []string{branch, x.shortHash(w.Head), w.Subject} {
					if p != "" {
						parts = append(parts, p)
					}
				}
				items = append(items, extension.QuickPickItem{Label: "$(list-tree) " + w.Name, Description: strings.Join(parts, " • "), Detail: w.Path})
			}
			if len(items) == 0 {
				items = append(items, extension.QuickPickItem{Label: "$(info) This repository has no worktrees."})
			}
			err = x.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Select a worktree to delete", Items: items, MatchOnDescription: true}, func(i int) {
				if i >= 0 && i < len(linked) && !r.disposed {
					x.deleteWorktree(r, linked[i].Path)
				}
			})
			if err != nil {
				x.logUnavailable("cannot show the worktrees", err)
			}
		})
	}()
}

// linkedWorktrees leaves out the main work tree, which git cannot remove.
func linkedWorktrees(list []git.Worktree) []git.Worktree {
	var out []git.Worktree
	for _, w := range list {
		if !w.Main {
			out = append(out, w)
		}
	}
	return out
}

// cmdDeleteWorktree2 is the Delete Worktree of a worktree's own menu.
//
// Ported from VS Code: extensions/git/src/commands.ts (deleteWorktree)
func (x *gitscm) cmdDeleteWorktree2(_ context.Context, args any) error {
	x.target(args, false, func(r *repository) {
		if r.kind() != git.KindWorktree || r.loc.Parent == "" {
			return
		}
		main := x.model.byRoot(r.loc.Parent)
		if main == nil || main.repo == nil {
			x.youAreInIt()
			return
		}
		x.deleteWorktree(main, r.root)
	})
	return nil
}

// youAreInIt is the refusal to delete the worktree the window is on.
func (x *gitscm) youAreInIt() {
	x.ask(extension.DialogSpec{
		Type:    extension.DialogError,
		Message: "You cannot delete the worktree you are currently in. Please switch to the main repository first.",
		Buttons: []extension.DialogButton{{Label: "OK", Cancel: true}},
	}, func(extension.DialogResult) {})
}

// deleteWorktree asks, removes the worktree with git worktree remove, and closes its repository. A worktree with changes asks again, to
// remove it with --force. The worktree the window shows is never deleted.
//
// Ported from VS Code: extensions/git/src/repository.ts (deleteWorktree)
func (x *gitscm) deleteWorktree(main *repository, path string) {
	if x.worktreeFolderOfTab(path) {
		x.youAreInIt()
		return
	}
	x.confirm(extension.DialogSpec{
		Type: extension.DialogWarning, Message: `Are you sure you want to delete the worktree "` + baseName(path) + `"?`,
		Detail:  "Worktree: " + path,
		Buttons: []extension.DialogButton{{Label: "Delete Worktree"}, cancelButton},
	}, func() { x.removeWorktree(main, path, false) })
}

// removeWorktree runs git worktree remove.
func (x *gitscm) removeWorktree(main *repository, path string, force bool) {
	main.mutate(mutation{
		name: "worktree remove",
		run:  func(ctx context.Context, repo *git.Repo) error { return repo.RemoveWorktree(ctx, path, force) },
		after: func() {
			if wr := x.model.byRoot(realOf(path)); wr != nil {
				x.model.drop(wr)
			}
		},
		failed: func(err error) bool {
			if errTag(err) != "WorktreeContainsChanges" || force {
				return false
			}
			x.confirm(extension.DialogSpec{
				Type: extension.DialogWarning, Message: "The worktree contains modified or untracked files. Do you want to force delete?",
				Detail: "Worktree: " + path, Buttons: []extension.DialogButton{{Label: "Force Delete"}, cancelButton},
			}, func() { x.removeWorktree(main, path, true) })
			return true
		},
	})
}

// ---- migrate ----

func (x *gitscm) cmdMigrateWorktreeChanges(_ context.Context, args any) error {
	x.targetKinds(args, mainKinds, x.pickWorktreeToMigrate)
	return nil
}

// pickWorktreeToMigrate picks the worktree to take the changes from, or takes the only one.
//
// Ported from VS Code: extensions/git/src/commands.ts (migrateWorktreeChanges)
func (x *gitscm) pickWorktreeToMigrate(r *repository) {
	repo, ctx := r.repo, r.ctx
	if repo == nil {
		return
	}
	go func() {
		list, err := repo.Worktrees(ctx)
		x.api.Post(func() {
			if x.stopped.Load() || r.disposed || ctx.Err() != nil {
				return
			}
			if err != nil {
				x.commandFailed("worktree list", r, err, nil)
				return
			}
			linked := linkedWorktrees(list)
			if len(linked) == 1 {
				x.migrateFrom(r, linked[0].Path)
				return
			}
			items := make([]extension.QuickPickItem, 0, len(linked))
			for _, w := range linked {
				items = append(items, extension.QuickPickItem{Label: "$(list-tree) " + w.Name, Description: w.Path})
			}
			if len(items) == 0 {
				items = append(items, extension.QuickPickItem{Label: "$(info) This repository has no worktrees."})
			}
			err = x.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Select a worktree to migrate changes from", Items: items, MatchOnDescription: true}, func(i int) {
				if i >= 0 && i < len(linked) && !r.disposed {
					x.migrateFrom(r, linked[i].Path)
				}
			})
			if err != nil {
				x.logUnavailable("cannot show the worktrees", err)
			}
		})
	}()
}

// migrateFrom opens the repository of the worktree in the view, when it is not open, and moves its changes.
func (x *gitscm) migrateFrom(target *repository, path string) {
	// the changes of both are the ones on disk now: the files of the status are what the comparison reads
	target.whenFresh(func() {
		x.withRepositoryAt(path, func(source *repository) {
			if source.kind() == git.KindWorktree && !target.disposed && target.repo != nil {
				x.migrateChanges(target, source)
			}
		})
	})
}

// relPaths are the paths of the resources of the groups relative to the root, in the order of the groups.
func relPaths(root string, groups ...[]resource) []string {
	var out []string
	for _, g := range groups {
		for _, res := range g {
			rel, err := filepath.Rel(root, res.path)
			if err != nil {
				rel = res.path
			}
			out = append(out, filepath.ToSlash(rel))
		}
	}
	return out
}

// migrateChanges moves the changes of a worktree (the untracked files too) to the repository: they are stashed in the worktree, with the
// name migration:<branch of the worktree>-<branch here>, and the stash is popped here. The files the repository changed itself stop it;
// a conflict leaves the conflicts in the Merge Changes group, and the worktree gets its changes back.
//
// Ported from VS Code: extensions/git/src/repository.ts (migrateChanges)
func (x *gitscm) migrateChanges(target, source *repository) {
	sourcePaths := relPaths(source.root, source.res.index, source.res.working, source.res.untracked)
	if len(sourcePaths) == 0 {
		x.api.Notifications().Info("There are no changes in the selected worktree to migrate.")
		return
	}
	targetPaths := relPaths(target.root, target.res.working, target.res.untracked)
	var overlaps []string
	for _, p := range sourcePaths {
		if slices.Contains(targetPaths, p) {
			overlaps = append(overlaps, p)
		}
	}
	if len(overlaps) > 0 {
		const shown = 5
		list := strings.Join(overlaps[:min(len(overlaps), shown)], "\n ")
		if more := len(overlaps) - shown; more > 0 {
			plural := ""
			if more > 1 {
				plural = "s"
			}
			list += "\n and " + strconv.Itoa(more) + " more file" + plural + "..."
		}
		x.ask(extension.DialogSpec{
			Type: extension.DialogError,
			Message: "Your local changes to the following files would be overwritten by merge:\n " + list +
				"\n\nPlease stage, commit, or stash your changes in the repository before migrating changes.",
			Buttons: []extension.DialogButton{{Label: "OK", Cancel: true}},
		}, func(extension.DialogResult) {})
		return
	}
	x.confirm(extension.DialogSpec{
		Type: extension.DialogWarning, Message: "Proceed with migrating changes to the current repository?",
		Detail:  "This will apply the worktree's changes to this repository and discard changes in the worktree.\nThis is IRREVERSIBLE!",
		Buttons: []extension.DialogButton{{Label: "Proceed"}, cancelButton},
	}, func() { x.runMigration(target, source) })
}

// headName is the branch of a repository, else the commit HEAD names.
func (r *repository) headName() string {
	if r.head.name != "" {
		return r.head.name
	}
	return r.head.commit
}

// runMigration stashes the changes in the worktree and pops them in the repository.
func (x *gitscm) runMigration(target, source *repository) {
	if source.repo == nil || target.repo == nil || source.disposed || target.disposed {
		return
	}
	name := "migration:" + source.headName() + "-" + target.headName()
	sourceRepo := source.repo
	conflict := false
	source.beginOwn()
	target.mutate(mutation{
		name: "migrate changes",
		run: func(ctx context.Context, repo *git.Repo) error {
			if err := sourceRepo.Stash(ctx, git.StashOptions{Message: name, IncludeUntracked: true}); err != nil {
				return err
			}
			back := func() error {
				return sourceRepo.StashPop(ctx, git.StashLatest, git.StashApplyOptions{ReinstateIndex: true})
			}
			err := repo.StashPop(ctx, git.StashLatest, git.StashApplyOptions{})
			switch {
			case err == nil:
				return nil
			case errTag(err) == "StashConflict":
				conflict = true
				if restore := back(); restore != nil {
					return restore
				}
				return nil
			}
			if restore := back(); restore != nil {
				x.logLine("migrate changes: the changes could not be put back in the worktree: " + describe(restore))
			}
			return err
		},
		after: func() {
			if conflict {
				target.worktreeMigrating = true
				x.api.Notifications().Warn("There are merge conflicts from migrating changes. Please resolve them before committing.")
			}
		},
		finally: func() {
			source.endOwn()
			source.refresh()
		},
	})
}

// ---- the merge editor and the comparison ----

// cmdOpenWorktreeMergeEditor opens the file named by the argument in the merge editor, the sides called "Workspace" and "Worktree".
//
// Ported from VS Code: extensions/git/src/commands.ts (openWorktreeMergeEditor)
func (x *gitscm) cmdOpenWorktreeMergeEditor(_ context.Context, args any) error {
	uri := x.uriArg(args)
	if uri == "" {
		return nil
	}
	x.openInWorktreeMergeEditor(uri)
	return nil
}

// worktreeMergeLabels name the sides of the merge of a migration.
var worktreeMergeLabels = &extension.MergeLabels{Current: "Workspace", Incoming: "Worktree"}

// openInWorktreeMergeEditor opens a file in the merge editor with the sides of a migration.
func (x *gitscm) openInWorktreeMergeEditor(uri string) {
	err := x.api.Documents().OpenDocument(extension.OpenSpec{URI: uri, MergeEditor: true, MergeLabels: worktreeMergeLabels})
	if err != nil {
		x.logUnavailable("cannot open the merge editor", err)
	}
}

// cmdCompareWithWorkspace compares a changed file of a worktree with the same path in the main work tree; for a repository that is not a
// worktree it opens the change.
//
// Ported from VS Code: extensions/git/src/repository.ts (resolveCompareWithWorkspaceCommand)
func (x *gitscm) cmdCompareWithWorkspace(_ context.Context, args any) error {
	targets := x.resourceTargets(args)
	if len(targets) == 0 {
		return nil
	}
	t := targets[0]
	if t.repo.kind() != git.KindWorktree || t.repo.loc.Parent == "" {
		x.openChange(t, openOptions{preview: true})
		return nil
	}
	s := t.repo.sidesOf(t.group, t.res)
	right := s.Right
	if right == "" {
		right = fileURI(t.res.path)
	}
	rel, err := filepath.Rel(t.repo.root, t.res.path)
	if err != nil {
		return nil
	}
	main := filepath.Join(t.repo.loc.Parent, rel)
	if _, err := os.Stat(main); err != nil {
		// the file is not in the main work tree: there is nothing to compare with
		x.openChange(t, openOptions{preview: true})
		return nil
	}
	err = x.api.Documents().OpenDiff(extension.DiffSpec{Left: fileURI(main), Right: right, Title: s.Title, Preview: true})
	if err != nil {
		x.logUnavailable("cannot open the comparison", err)
	}
	return nil
}
