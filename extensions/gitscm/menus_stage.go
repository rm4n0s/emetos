package gitscm

import "github.com/rm4n0s/emetos/extension"

// stageMenus are the menu items of the commands that stage, unstage and discard: the actions and the context menus of the resources
// of the groups, of the groups and of the folders of the tree view.
//
// Ported from VS Code: extensions/git/package.json (contributes.menus: scm/resourceState/context, scm/resourceGroup/context,
// scm/resourceFolder/context)
func stageMenus() []extension.MenuItem {
	const git = "scmProvider == git"
	var out []extension.MenuItem
	in := func(group string) string { return git + " && scmResourceGroup == " + group }

	// the resources of the groups (the context menu opens with the first group of each menu; inline@N are the buttons of the row)
	res := extension.MenuSCMResourceStateContext
	out = append(out,
		item(res, "git.stage", "1_modification", in(groupMerge)),
		item(res, "git.stage", "inline@2", in(groupMerge)),
		item(res, "git.unstage", "1_modification", in(groupIndex)),
		item(res, "git.unstage", "inline@2", in(groupIndex)),
		item(res, "git.stage", "1_modification", in(groupWorking)),
		item(res, "git.clean", "1_modification", in(groupWorking)),
		item(res, "git.clean", "inline@2", in(groupWorking)),
		item(res, "git.stage", "inline@2", in(groupWorking)),
		item(res, "git.ignore", "1_modification@3", in(groupWorking)),
		item(res, "git.stage", "1_modification", in(groupUntracked)),
		item(res, "git.clean", "1_modification", in(groupUntracked)+" && !gitFreshRepository"),
		item(res, "git.clean", "inline@2", in(groupUntracked)+" && !gitFreshRepository"),
		item(res, "git.stage", "inline@2", in(groupUntracked)),
		item(res, "git.ignore", "1_modification@3", in(groupUntracked)),
	)

	// the groups
	grp := extension.MenuSCMResourceGroupContext
	mixed := " && config.git.untrackedChanges == mixed"
	notMixed := " && config.git.untrackedChanges != mixed"
	out = append(out,
		item(grp, "git.stageAllMerge", "1_modification", in(groupMerge)),
		item(grp, "git.stageAllMerge", "inline@2", in(groupMerge)),
		item(grp, "git.unstageAll", "1_modification", in(groupIndex)),
		item(grp, "git.unstageAll", "inline@2", in(groupIndex)),
		item(grp, "git.cleanAll", "1_modification", in(groupWorking)+mixed),
		item(grp, "git.stageAll", "1_modification", in(groupWorking)+mixed),
		item(grp, "git.cleanAll", "inline@2", in(groupWorking)+mixed),
		item(grp, "git.stageAll", "inline@2", in(groupWorking)+mixed),
		item(grp, "git.cleanAllTracked", "1_modification", in(groupWorking)+notMixed),
		item(grp, "git.stageAllTracked", "1_modification", in(groupWorking)+notMixed),
		item(grp, "git.cleanAllTracked", "inline@2", in(groupWorking)+notMixed),
		item(grp, "git.stageAllTracked", "inline@2", in(groupWorking)+notMixed),
		item(grp, "git.cleanAllUntracked", "1_modification", in(groupUntracked)),
		item(grp, "git.stageAllUntracked", "1_modification", in(groupUntracked)),
		item(grp, "git.cleanAllUntracked", "inline@2", in(groupUntracked)),
		item(grp, "git.stageAllUntracked", "inline@2", in(groupUntracked)),
	)

	// the folders of the tree view act on the resources below them
	dir := extension.MenuSCMResourceFolderContext
	out = append(out,
		item(dir, "git.stage", "1_modification", in(groupMerge)),
		item(dir, "git.stage", "inline@2", in(groupMerge)),
		item(dir, "git.unstage", "1_modification", in(groupIndex)),
		item(dir, "git.unstage", "inline@2", in(groupIndex)),
		item(dir, "git.stage", "1_modification", in(groupWorking)),
		item(dir, "git.clean", "1_modification", in(groupWorking)),
		item(dir, "git.clean", "inline@2", in(groupWorking)),
		item(dir, "git.stage", "inline@2", in(groupWorking)),
		item(dir, "git.ignore", "1_modification@3", in(groupWorking)),
		item(dir, "git.stage", "1_modification", in(groupUntracked)),
		item(dir, "git.stage", "inline@2", in(groupUntracked)),
		item(dir, "git.clean", "1_modification", in(groupUntracked)),
		item(dir, "git.clean", "inline@2", in(groupUntracked)),
		item(dir, "git.ignore", "1_modification@3", in(groupUntracked)),
	)
	return out
}
