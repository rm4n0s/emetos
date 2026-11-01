package gitscm

import "github.com/rm4n0s/emetos/extension"

// worktreeMenus are the menu items of the worktrees: the Worktrees submenu in the "..." menu of the view and of a repository, its entries,
// the items of the menu of a worktree repository, and Compare with Workspace for the changed files of a worktree.
//
// Ported from VS Code: extensions/git/package.json (contributes.menus: scm/title, scm/repository, git.worktrees, scm/sourceControl,
// scm/resourceState/context)
func worktreeMenus() []extension.MenuItem {
	const (
		git      = "scmProvider == git"
		worktree = "scmProviderContext == worktree"
	)
	in := worktreesSubmenu
	res := extension.MenuSCMResourceStateContext
	return []extension.MenuItem{
		{Menu: extension.MenuSCMTitle, Submenu: worktreesSubmenu, Group: "2_main", Order: 8, When: git},
		{Menu: extension.MenuSCMRepository, Submenu: worktreesSubmenu, Group: "2_main", Order: 8, When: git},
		item(in, "git.openWorktree", "openWorktrees@1", worktree),
		item(in, "git.openWorktreeInNewWindow", "openWorktrees@2", worktree),
		item(in, "git.createWorktree", "worktrees@1", "scmProviderContext == repository"),
		item(in, "git.deleteWorktree2", "worktrees@2", worktree),
		item(extension.MenuSCMSourceControl, "git.openWorktree", "1_worktree@1", git+" && "+worktree),
		item(extension.MenuSCMSourceControl, "git.openWorktreeInNewWindow", "1_worktree@2", git+" && "+worktree),
		item(extension.MenuSCMSourceControl, "git.deleteWorktree2", "2_worktree@1", git+" && "+worktree),
		item(res, "git.compareWithWorkspace", "worktree_diff", git+" && scmResourceGroup == index && scmResourceState == worktree"),
		item(res, "git.compareWithWorkspace", "worktree_diff", git+" && scmResourceGroup == workingTree && scmResourceState == worktree"),
	}
}
