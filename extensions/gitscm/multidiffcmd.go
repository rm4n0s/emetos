package gitscm

import (
	"context"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// The commands that open the multi-file diff editor (SPEC §10.30 "Diffs of changes"): Open Changes, Open Staged Changes and Open
// Untracked Changes for a group of a repository, and Open Commit for a commit.
//
// Ported from VS Code: extensions/git/src/commands.ts (viewChanges, viewStagedChanges, viewUntrackedChanges, _viewResourceGroupChanges,
// viewCommit)

// viewGroupCommand makes the handler of a command that opens the changes of a group.
func (x *gitscm) viewGroupCommand(group, none string) extension.CommandHandler {
	return func(_ context.Context, args any) error {
		x.target(args, true, func(r *repository) {
			if list, _ := groupOf(r.res, group); len(list) == 0 {
				x.api.Notifications().Info(none)
				return
			}
			x.openGroupChanges(r, group)
		})
		return nil
	}
}

// commitArgs reads what Open Commit is asked for: a revision, or an object with the revision as "id" ("historyItemId" and "hash" are the
// names the Graph and the Timeline use) and optionally "revealUri", the file to scroll to.
func commitArgs(args any) (id, reveal string) {
	switch v := args.(type) {
	case string:
		return v, ""
	case map[string]any:
		for _, k := range []string{"id", "historyItemId", "hash"} {
			if s, _ := v[k].(string); s != "" {
				id = s
				break
			}
		}
		reveal, _ = v["revealUri"].(string)
	}
	return id, reveal
}

// cmdViewCommit opens what a commit changed in the multi-file diff editor: against its first parent, and a root commit against the empty
// tree. The commit is read in the background; the editor opens when it is known.
func (x *gitscm) cmdViewCommit(_ context.Context, args any) error {
	// the Graph names the range of the incoming and the outgoing changes
	if arg, ok := extension.HistoryArgs(args); ok && arg.Range != nil {
		if r := x.targetOf(args); r != nil {
			x.openRange(r, arg.Range.Base, arg.Range.Head, arg.Range.Title)
		}
		return nil
	}
	if arg, ok := extension.HistoryArgs(args); ok && arg.Item != nil {
		args = map[string]any{"id": arg.Item.ID, "root": rootOfArgs(x, args)}
	}
	id, reveal := commitArgs(args)
	if id == "" {
		return nil
	}
	x.target(args, true, func(r *repository) { x.openCommit(r, id, reveal) })
	return nil
}

// openCommit asks git for the commit and opens its editor.
func (x *gitscm) openCommit(r *repository, id, reveal string) {
	repo, ctx := r.repo, r.ctx
	if repo == nil {
		return
	}
	go func() {
		ch, err := repo.CommitChanges(ctx, id)
		x.api.Post(func() {
			if x.stopped.Load() || r.disposed {
				return
			}
			if err != nil {
				if ctx.Err() == nil {
					x.log.Warn("cannot read the commit", "revision", id, "err", err)
					x.api.Notifications().Error("Cannot open the commit " + id + ": " + err.Error())
				}
				return
			}
			hash := ch.Commit.Hash
			short := hash
			if n := x.cfg.commitShortHashLength; n > 0 && len(short) > n {
				short = short[:n]
			}
			subject, _, _ := strings.Cut(ch.Commit.Message, "\n")
			spec := extension.MultiDiffSpec{Title: short + " - " + subject, Kind: MultiDiffKind, Key: commitKey(r.id, hash)}
			if p, ok := uriPath(reveal); ok {
				spec.Reveal = toGitURI(r.id, r.root, p, hash)
			}
			x.multi.opened(r.id, spec.Key)
			if err := x.api.Documents().OpenMultiDiff(spec); err != nil {
				x.log.Warn("cannot open a commit", "revision", id, "err", err)
				x.api.Notifications().Error("Cannot open the commit " + id + ": " + err.Error())
			}
		})
	}()
}

// multiDiffMenus are the menu items of the multi-file diff editor: the stage and unstage buttons in the header of a file, and the
// button of the header of a group that opens its changes.
//
// Ported from VS Code: extensions/git/package.json (contributes.menus: multiDiffEditor/resource/title, scm/resourceGroup/context)
func multiDiffMenus() []extension.MenuItem {
	file := extension.MenuMultiDiffFileToolbar
	group := extension.MenuSCMResourceGroupContext
	return []extension.MenuItem{
		item(file, "git.stageFile", "navigation", "scmProvider == git && scmResourceGroup == workingTree"),
		item(file, "git.stageFile", "navigation", "scmProvider == git && scmResourceGroup == untracked"),
		item(file, "git.unstageFile", "navigation", "scmProvider == git && scmResourceGroup == index"),
		item(group, "git.viewStagedChanges", "inline@1", "scmProvider == git && scmResourceGroup == index"),
		item(group, "git.viewChanges", "inline@1", "scmProvider == git && scmResourceGroup == workingTree"),
		item(group, "git.viewUntrackedChanges", "inline@1", "scmProvider == git && scmResourceGroup == untracked"),
	}
}

// rootOfArgs is the root of the repository the arguments name, for a command that builds its own arguments from them.
func rootOfArgs(x *gitscm, args any) string {
	if r := x.targetOf(args); r != nil {
		return r.root
	}
	return ""
}

// openRefRange opens the changes from one commit to another, after finding that there are some: with none a notification says so. The
// ends are shown by their names.
//
// Ported from VS Code: commands.ts (_openChangesBetweenRefs)
func (x *gitscm) openRefRange(r *repository, from, to refEnd) {
	if from.id == "" || to.id == "" || r.repo == nil {
		return
	}
	repo, ctx := r.repo, r.ctx
	go func() {
		changes, err := repo.NameStatus(ctx, git.DiffSpec{Revs: []string{from.id, to.id}, SimilarityThreshold: 50})
		x.api.Post(func() {
			if x.stopped.Load() || r.disposed {
				return
			}
			switch {
			case err != nil:
				if ctx.Err() == nil {
					x.log.Warn("cannot compare", "from", from.id, "to", to.id, "err", err)
					x.api.Notifications().Error(`Failed to open changes between "` + from.name + `" and "` + to.name + `": ` + describe(err))
				}
			case len(changes) == 0:
				x.api.Notifications().Info(`There are no changes between "` + from.name + `" and "` + to.name + `".`)
			default:
				x.openRange(r, from.id, to.id, from.name+" \u2194 "+to.name)
			}
		})
	}()
}

// openRange opens the multi-file diff editor of the changes between two commits.
func (x *gitscm) openRange(r *repository, base, head, title string) {
	if base == "" || head == "" {
		return
	}
	spec := extension.MultiDiffSpec{Title: title, Kind: MultiDiffKind, Key: rangeKey(r.id, base, head)}
	x.multi.opened(r.id, spec.Key)
	if err := x.api.Documents().OpenMultiDiff(spec); err != nil {
		x.log.Warn("cannot open a range of changes", "err", err)
		x.api.Notifications().Error("Cannot open the changes: " + err.Error())
	}
}
