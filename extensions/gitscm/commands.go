package gitscm

import (
	"context"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
	"github.com/rm4n0s/emetos/extensions/gitscm/trust"
)

// The commands the extension adds to the ones of VS Code's git extension (D37).
const (
	// CommandTrustRepository is git.trustRepository, "Trust Repository...": the welcome content's button asks the trust question again for a
	// repository that was not trusted.
	CommandTrustRepository = "git.trustRepository"
	// CommandManageTrusted is git.manageTrustedRepositories, "Manage Trusted Repositories...".
	CommandManageTrusted = "git.manageTrustedRepositories"
)

// trustCommands are the two commands of the trust question.
var trustCommands = []extension.CommandSpec{
	{ID: CommandTrustRepository, Title: "Trust Repository...", Category: "Git"},
	{ID: CommandManageTrusted, Title: "Manage Trusted Repositories...", Category: "Git"},
	{ID: CommandOpenResource, Title: "Open", Category: "Git"},
}

// extraMenus are the menu items of the commands of this step: Refresh and Show Git Output in the title of the Source Control view,
// Close Repository in the menu of a repository, and the trust commands in the command palette.
//
// Ported from VS Code: extensions/git/package.json (contributes.menus: scm/title, scm/sourceControl)
var extraMenus = []extension.MenuItem{
	{Menu: extension.MenuSCMTitle, Command: "git.refresh", Group: "navigation", When: "scmProvider == git"},
	{Menu: extension.MenuSCMTitle, Command: "git.showOutput", Group: "3_footer", When: "scmProvider == git"},
	{Menu: extension.MenuSCMSourceControl, Command: "git.close", Group: "navigation@1", When: "scmProvider == git"},
	{Menu: extension.MenuSCMSourceControl, Command: "git.closeOtherRepositories", Group: "navigation@2", When: "scmProvider == git && gitOpenRepositoryCount > 1"},
	{Menu: extension.MenuCommandPalette, Command: CommandTrustRepository, When: "config.git.enabled && git.untrustedRepositoryCount != 0"},
	{Menu: extension.MenuCommandPalette, Command: CommandManageTrusted, When: "config.git.enabled"},
}

// registerCommands gives every command the extension declares its handler: its own, or the one that says it is not available yet.
func (x *gitscm) registerCommands() error {
	own := map[string]extension.CommandHandler{
		"git.refresh":                         x.cmdRefresh,
		"git.close":                           x.cmdClose,
		"git.closeOtherRepositories":          x.cmdCloseOthers,
		"git.reopenClosedRepositories":        x.cmdReopenClosed,
		"git.openRepository":                  x.cmdOpenRepository,
		"git.manageUnsafeRepositories":        x.cmdManageUnsafe,
		"git.openRepositoriesInParentFolders": x.cmdOpenParents,
		"git.showOutput":                      func(context.Context, any) error { x.channel.Show(false); return nil },
		CommandOpenResource:                   x.cmdOpenResource,
		"git.openChange":                      x.cmdOpenChange,
		"git.openFile":                        x.cmdOpenFile,
		"git.openFile2":                       x.cmdOpenFile,
		"git.openHEADFile":                    x.cmdOpenHEADFile,
		"git.openAllChanges":                  x.cmdOpenAllChanges,
		"git.viewChanges":                     x.viewGroupCommand(groupWorking, "The repository does not have any changes."),
		"git.viewStagedChanges":               x.viewGroupCommand(groupIndex, "The repository does not have any staged changes."),
		"git.viewUntrackedChanges":            x.viewGroupCommand(groupUntracked, "The repository does not have any untracked changes."),
		"git.viewCommit":                      x.cmdViewCommit,
		"git.closeAllDiffEditors":             x.cmdCloseAllDiffEditors,
		"git.closeAllUnmodifiedEditors":       x.cmdCloseAllUnmodifiedEditors,
		"git.revealInExplorer":                x.cmdRevealInExplorer,
		"git.revealFileInOS.linux":            x.cmdRevealFileInOS,
		"git.stage":                           x.cmdStage,
		"git.stageAll":                        x.cmdStageAll,
		"git.stageAllTracked":                 x.cmdStageAllTracked,
		"git.stageAllUntracked":               x.cmdStageAllUntracked,
		"git.stageAllMerge":                   x.cmdStageAllMerge,
		"git.stageFile":                       x.cmdStageFile,
		"git.unstage":                         x.cmdUnstage,
		"git.unstageAll":                      x.cmdUnstageAll,
		"git.unstageFile":                     x.cmdUnstageFile,
		"git.clean":                           x.cmdClean,
		"git.cleanAll":                        x.cmdCleanAll,
		"git.cleanAllTracked":                 x.cmdCleanAllTracked,
		"git.cleanAllUntracked":               x.cmdCleanAllUntracked,
		"git.stageChange":                     x.cmdStageChange,
		"git.unstageChange":                   x.cmdUnstageChange,
		"git.revertChange":                    x.cmdRevertChange,
		"git.stageSelectedRanges":             x.cmdStageSelectedRanges,
		"git.unstageSelectedRanges":           x.cmdUnstageSelectedRanges,
		"git.revertSelectedRanges":            x.cmdRevertSelectedRanges,
		"git.diff.stageHunk":                  x.cmdDiffStageHunk,
		"git.diff.stageSelection":             x.cmdDiffStageSelection,
		"git.ignore":                          x.cmdIgnore,
		"git.rename":                          x.cmdRename,
		"git.delete":                          x.cmdDelete,
		"git.blame.toggleEditorDecoration":    x.cmdToggleBlameEditor,
		"git.blame.toggleStatusBarItem":       x.cmdToggleBlameStatusBar,
		"git.commitMessageAccept":             x.cmdCommitMessageAccept,
		"git.commitMessageDiscard":            x.cmdCommitMessageDiscard,
		"git.fetch":                           x.cmdFetch,
		"git.fetchPrune":                      x.cmdFetchPrune,
		"git.fetchAll":                        x.cmdFetchAll,
		"git.fetchRef":                        x.cmdFetchRef,
		"git.pull":                            x.cmdPull,
		"git.pullRebase":                      x.cmdPullRebase,
		"git.pullFrom":                        x.cmdPullFrom,
		"git.pullRef":                         x.cmdPullRef,
		"git.push":                            x.cmdPush,
		"git.pushForce":                       x.cmdPushForce,
		"git.pushTo":                          x.cmdPushTo,
		"git.pushToForce":                     x.cmdPushToForce,
		"git.pushWithTags":                    x.cmdPushWithTags,
		"git.pushWithTagsForce":               x.cmdPushWithTagsForce,
		"git.pushRef":                         x.cmdPushRef,
		"git.sync":                            x.cmdSync,
		"git.syncRebase":                      x.cmdSyncRebase,
		"git.publish":                         x.cmdPublish,
		"git.addRemote":                       x.cmdAddRemote,
		"git.removeRemote":                    x.cmdRemoveRemote,
		"git.checkout":                        x.cmdCheckout,
		"git.checkoutDetached":                x.cmdCheckoutDetached,
		"git.branch":                          x.cmdBranch,
		"git.branchFrom":                      x.cmdBranchFrom,
		"git.renameBranch":                    x.cmdRenameBranch,
		"git.deleteBranch":                    x.cmdDeleteBranch,
		"git.deleteRemoteBranch":              x.cmdDeleteRemoteBranch,
		"git.createTag":                       x.cmdCreateTag,
		"git.deleteTag":                       x.cmdDeleteTag,
		"git.deleteRemoteTag":                 x.cmdDeleteRemoteTag,
		"git.pushTags":                        x.cmdPushTags,
		"git.merge":                           x.cmdMerge,
		"git.mergeAbort":                      x.cmdMergeAbort,
		"git.rebase":                          x.cmdRebase,
		"git.rebaseInteractive":               x.cmdRebaseInteractive,
		"git.rebaseAbort":                     x.cmdRebaseAbort,
		"git.rebaseSkip":                      x.cmdRebaseSkip,
		"git.cherryPick":                      x.cmdCherryPick,
		"git.cherryPickAbort":                 x.cmdCherryPickAbort,
		"git.acceptMerge":                     x.cmdAcceptMerge,
		"git.runGitMerge":                     x.cmdRunGitMerge,
		"git.runGitMergeDiff3":                x.cmdRunGitMergeDiff3,
		"git.stash":                           x.cmdStashCommand(false, false),
		"git.stashIncludeUntracked":           x.cmdStashCommand(true, false),
		"git.stashStaged":                     x.cmdStashCommand(false, true),
		"git.stashPop":                        x.cmdStashPop,
		"git.stashPopLatest":                  x.cmdStashPopLatest,
		"git.stashPopEditor":                  x.cmdStashPopEditor,
		"git.stashApply":                      x.cmdStashApply,
		"git.stashApplyLatest":                x.cmdStashApplyLatest,
		"git.stashApplyEditor":                x.cmdStashApplyEditor,
		"git.stashDrop":                       x.cmdStashDrop,
		"git.stashDropAll":                    x.cmdStashDropAll,
		"git.stashDropEditor":                 x.cmdStashDropEditor,
		"git.stashView":                       x.cmdStashView,
		CommandTrustRepository:                x.cmdTrustRepository,
		CommandManageTrusted:                  x.cmdManageTrusted,
	}
	for id, h := range x.timelineHandlers() {
		own[id] = h
	}
	for id, h := range x.commitHandlers() {
		own[id] = h
	}
	for _, spec := range append(append([]extension.CommandSpec(nil), commandSpecs...), trustCommands...) {
		h := own[spec.ID]
		if h == nil {
			h = placeholder(x, spec.Title)
		}
		x.handlers[spec.ID] = h
		if err := x.api.Commands().Handle(spec.ID, h); err != nil {
			return err
		}
	}
	// the command the Copy Hash link of the blame hover runs; VS Code has it too, and it is not in the command palette
	x.handlers[commandCopyContent] = x.cmdCopyContent
	return x.api.Commands().Register(extension.CommandSpec{ID: commandCopyContent}, x.cmdCopyContent)
}

// placeholder is the handler of a command a later step implements.
//
// TODO(step 24.9 to 24.23): each step replaces the handlers of its commands.
func placeholder(x *gitscm, title string) extension.CommandHandler {
	return func(context.Context, any) error {
		x.api.Notifications().Info(title + " is not available yet.")
		return nil
	}
}

// target finds the repository a command is for: the one its arguments name (a source control of a menu, {"root": path}, a path), the
// only open one, or the one the user picks. none is called when there is no repository.
func (x *gitscm) target(args any, open bool, fn func(*repository)) {
	if r := x.targetOf(args); r != nil {
		fn(r)
		return
	}
	var list []*repository
	for _, r := range x.model.order {
		if !open || r.repo != nil {
			list = append(list, r)
		}
	}
	switch len(list) {
	case 0:
		x.api.Notifications().Info("There are no available repositories.")
	case 1:
		fn(list[0])
	default:
		x.pickRepository(list, "Choose a repository", fn)
	}
}

// targetOf reads the repository named in the arguments of a command.
func (x *gitscm) targetOf(args any) *repository {
	for _, a := range extension.SCMArgs(args) {
		if r := x.model.byHandle(a.SourceControl); r != nil {
			return r
		}
	}
	switch v := args.(type) {
	case string:
		if v != "" {
			return x.model.forPath(filepath.Clean(strings.TrimPrefix(v, "file://")))
		}
	case map[string]any:
		if root, _ := v["root"].(string); root != "" {
			return x.model.byRoot(root)
		}
	}
	return nil
}

// pickRepository lists repositories in a quick pick and calls fn with the one chosen.
func (x *gitscm) pickRepository(list []*repository, placeholder string, fn func(*repository)) {
	items := make([]extension.QuickPickItem, len(list))
	for i, r := range list {
		items[i] = extension.QuickPickItem{Label: "$(repo) " + r.name, Description: r.root}
	}
	err := x.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: placeholder, Items: items, MatchOnDescription: true}, func(i int) {
		if i >= 0 && i < len(list) && !list[i].disposed {
			fn(list[i])
		}
	})
	if err != nil {
		x.log.Warn("cannot show the list of repositories", "err", err)
	}
}

func (x *gitscm) cmdRefresh(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { r.refresh() })
	return nil
}

func (x *gitscm) cmdClose(_ context.Context, args any) error {
	x.target(args, false, func(r *repository) { x.model.close(r) })
	return nil
}

func (x *gitscm) cmdCloseOthers(_ context.Context, args any) error {
	x.target(args, false, func(keep *repository) {
		for _, r := range append([]*repository(nil), x.model.order...) {
			if r != keep {
				x.model.close(r)
			}
		}
	})
	return nil
}

func (x *gitscm) cmdReopenClosed(context.Context, any) error {
	closed := append([]string(nil), x.model.closedOrder...)
	if len(closed) == 0 {
		x.api.Notifications().Info("There are no closed repositories.")
		return nil
	}
	items := make([]extension.QuickPickItem, len(closed))
	for i, root := range closed {
		items[i] = extension.QuickPickItem{Label: "$(repo) " + baseName(root), Description: root}
	}
	return x.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Choose a repository to reopen", Items: items, MatchOnDescription: true}, func(i int) {
		if i >= 0 && i < len(closed) {
			x.model.reopen(closed[i])
		}
	})
}

// cmdOpenRepository opens the repository in a folder: the path in the argument, or one typed in an input box.
func (x *gitscm) cmdOpenRepository(_ context.Context, args any) error {
	path := ""
	switch v := args.(type) {
	case string:
		path = v
	case map[string]any:
		path, _ = v["path"].(string)
	}
	open := func(path string) {
		if !filepath.IsAbs(path) {
			x.api.Notifications().Error("The path of a repository is absolute: " + path)
			return
		}
		loc, ok := git.FindRepository(resolve(path))
		if !ok {
			x.api.Notifications().Error("There is no Git repository in " + path)
			return
		}
		x.model.openRepository(candidate{loc: loc, why: foundFolder}, true)
		x.model.askNext()
	}
	if path != "" {
		open(path)
		return nil
	}
	return x.api.InputBox().Show(extension.InputBoxOptions{Prompt: "Path of the repository folder", Value: x.folder}, func(value string, ok bool) {
		if ok && strings.TrimSpace(value) != "" {
			open(strings.TrimSpace(value))
		}
	})
}

func (x *gitscm) cmdOpenParents(context.Context, any) error {
	x.model.openParents()
	return nil
}

// cmdManageUnsafe lists the repositories git refuses because another user owns them, and adds the one chosen to safe.directory after
// a confirmation that names the folder and its owner. Emetos never adds one by itself.
func (x *gitscm) cmdManageUnsafe(context.Context, any) error {
	list := append([]unsafeRepo(nil), x.model.unsafe...)
	if len(list) == 0 {
		x.api.Notifications().Info("There are no unsafe repositories.")
		return nil
	}
	items := make([]extension.QuickPickItem, len(list))
	for i, u := range list {
		items[i] = extension.QuickPickItem{Label: "$(repo) " + baseName(u.root), Description: u.root}
	}
	return x.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Choose a repository to mark as safe", Items: items, MatchOnDescription: true}, func(i int) {
		if i < 0 || i >= len(list) {
			return
		}
		x.confirmSafeDirectory(list[i])
	})
}

// confirmSafeDirectory asks, and runs git config --global --add safe.directory.
func (x *gitscm) confirmSafeDirectory(u unsafeRepo) {
	owner := ownerName(u.root)
	err := x.api.Dialogs().Ask(extension.DialogSpec{
		Type:    extension.DialogWarning,
		Message: "Mark " + u.safeDir + " as a safe directory?",
		Detail: "The folder is owned by " + owner + ", not by you. Git refuses to work in it unless you list it in safe.directory of your global configuration. " +
			"Only do so when you know the owner can be trusted.\n\nEmetos runs: git config --global --add safe.directory " + u.safeDir,
		Buttons: []extension.DialogButton{{Label: "Mark as Safe"}, {Label: "Cancel", Cancel: true}},
	}, func(res extension.DialogResult) {
		if res.Cancelled || res.Button != 0 || x.runner == nil {
			return
		}
		runner := x.runner
		go func() {
			_, err := runner.Run(x.ctx, git.Command{Args: []string{"config", "--global", "--add", "safe.directory", u.safeDir}})
			x.api.Post(func() {
				if err != nil {
					x.api.Notifications().Error("Git could not add the safe directory: " + describe(err))
					return
				}
				for i, o := range x.model.unsafe {
					if o.root == u.root {
						x.model.unsafe = append(x.model.unsafe[:i], x.model.unsafe[i+1:]...)
						break
					}
				}
				x.model.updateContext()
				x.model.scan()
			})
		}()
	})
	if err != nil {
		x.log.Warn("cannot ask about the safe directory", "err", err)
	}
}

// ownerName is the name of the user that owns a folder.
func ownerName(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "another user"
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "another user"
	}
	id := strconv.FormatUint(uint64(st.Uid), 10)
	if u, err := user.LookupId(id); err == nil {
		return u.Username + " (user " + id + ")"
	}
	return "user " + id
}

// cmdTrustRepository asks the trust question for a repository that waits for an answer or was declined: the one in the argument, the
// only one, or one picked.
func (x *gitscm) cmdTrustRepository(_ context.Context, args any) error {
	pending := x.model.untrusted()
	if len(pending) == 0 {
		x.api.Notifications().Info("All the repositories are trusted.")
		return nil
	}
	ask := func(r *repository) {
		x.trust.Undecline(r.root)
		x.model.ask(r)
		x.model.askNext()
	}
	if r := x.targetOf(args); r != nil && !r.trusted {
		ask(r)
		return nil
	}
	if len(pending) == 1 {
		ask(pending[0])
		return nil
	}
	x.pickRepository(pending, "Choose a repository to trust", ask)
	return nil
}

// cmdManageTrusted lists what the user trusted and takes away the entry that is picked: the repositories it covered stop at once, their
// processes are ended.
func (x *gitscm) cmdManageTrusted(context.Context, any) error {
	entries := x.trust.Entries()
	if len(entries) == 0 {
		x.api.Notifications().Info("No Git repository is trusted.")
		return nil
	}
	items := make([]extension.QuickPickItem, len(entries))
	for i, e := range entries {
		if e.Kind == trust.Parent {
			items[i] = extension.QuickPickItem{Label: "$(folder) " + e.Path, Description: "every repository in it", Detail: "Select to stop trusting"}
		} else {
			items[i] = extension.QuickPickItem{Label: "$(repo) " + e.Path, Description: "repository", Detail: "Select to stop trusting"}
		}
	}
	return x.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Select a trusted repository or folder to stop trusting it", Items: items, MatchOnDescription: true}, func(i int) {
		if i < 0 || i >= len(entries) {
			return
		}
		if _, err := x.trust.Remove(entries[i]); err != nil {
			x.model.saveFailed(err)
		}
		x.model.enforceTrust()
	})
}

// enforceTrust stops the repositories that are not trusted any more.
func (m *model) enforceTrust() {
	for _, r := range m.order {
		if r.trusted && !m.x.trustedBy(r.loc) {
			r.untrust()
			m.x.trust.Decline(r.root)
		}
	}
	m.updateContext()
}
