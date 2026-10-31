package gitscm

import "github.com/rm4n0s/emetos/extension"

// The submenus Branch and Tags of the "..." menus of the Source Control view, and the checkout entry of their header.
const (
	branchSubmenu = "git.branch"
	tagsSubmenu   = "git.tags"
)

// branchMenus are the menu items of branches and tags: Checkout to... in the header of the view and of a repository, the Branch and Tags
// submenus and their entries. Merge, Rebase and Publish Branch of VS Code's Branch submenu come with steps 24.14 and 24.15.
//
// Ported from VS Code: extensions/git/package.json (contributes.menus: scm/title, scm/repository, git.branch, git.tags)
func branchMenus() []extension.MenuItem {
	const git = "scmProvider == git"
	return []extension.MenuItem{
		{Menu: extension.MenuSCMTitle, Command: "git.checkout", Group: "1_header", Order: 4, When: git},
		{Menu: extension.MenuSCMRepository, Command: "git.checkout", Group: "1_header", Order: 4, When: git},
		{Menu: extension.MenuSCMTitle, Submenu: branchSubmenu, Group: "2_main", Order: 4, When: git},
		{Menu: extension.MenuSCMRepository, Submenu: branchSubmenu, Group: "2_main", Order: 4, When: git},
		{Menu: extension.MenuSCMTitle, Submenu: tagsSubmenu, Group: "2_main", Order: 7, When: git},
		{Menu: extension.MenuSCMRepository, Submenu: tagsSubmenu, Group: "2_main", Order: 7, When: git},
		item(branchSubmenu, "git.branch", "2_branch@1", ""),
		item(branchSubmenu, "git.branchFrom", "2_branch@2", ""),
		item(branchSubmenu, "git.renameBranch", "3_modify@1", ""),
		item(branchSubmenu, "git.deleteBranch", "3_modify@2", ""),
		item(branchSubmenu, "git.deleteRemoteBranch", "3_modify@3", ""),
		item(tagsSubmenu, "git.createTag", "1_tags@1", ""),
		item(tagsSubmenu, "git.deleteTag", "1_tags@2", ""),
		item(tagsSubmenu, "git.deleteRemoteTag", "1_tags@3", ""),
		item(tagsSubmenu, "git.pushTags", "2_tags@1", ""),
	}
}
