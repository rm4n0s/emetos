package gitscm

import "github.com/rm4n0s/emetos/extension"

// graphMenus are the menu items of the Source Control Graph: the actions of its title, the context menu of a commit, and the menu of a
// reference (VS Code puts the second into the first as submenus, one row for each reference of the commit).
//
// Ported from VS Code: extensions/git/package.json (contributes.menus: scm/history/title, scm/historyItem/context,
// scm/historyItemRef/context)
func graphMenus() []extension.MenuItem {
	const git = "scmProvider == git"
	title, itemMenu, refMenu := extension.MenuSCMHistoryTitle, extension.MenuSCMHistoryItemContext, extension.MenuSCMHistoryItemRefContext
	return []extension.MenuItem{
		item(title, "git.fetchAll", "navigation@900", git),
		item(title, "git.pullRef", "navigation@901", git),
		item(title, "git.pushRef", "navigation@902", git+" && scmCurrentHistoryItemRefHasRemote"),
		item(title, "git.publish", "navigation@903", git+" && !scmCurrentHistoryItemRefHasRemote"),

		item(itemMenu, "git.graph.checkoutDetached", "1_checkout@2", git),
		item(itemMenu, "git.branch", "2_branch@2", git),
		item(itemMenu, "git.createTag", "3_tag@1", git),
		item(itemMenu, "git.graph.cherryPick", "4_modify@1", git),
		item(itemMenu, "git.graph.compareWithRemote", "5_compare@1", git),
		item(itemMenu, "git.graph.compareWithMergeBase", "5_compare@2", git),
		item(itemMenu, "git.graph.compareRef", "5_compare@3", git),
		item(itemMenu, "git.copyCommitId", "9_copy@1", git+" && !listMultiSelection"),
		item(itemMenu, "git.copyCommitMessage", "9_copy@2", git+" && !listMultiSelection"),

		item(refMenu, "git.graph.checkout", "1_checkout@1", git),
		item(refMenu, "git.graph.deleteBranch", "2_branch@2", git+` && scmHistoryItemRef =~ /^refs\/heads\/|^refs\/remotes\//`),
		item(refMenu, "git.graph.deleteTag", "3_tag@2", git+` && scmHistoryItemRef =~ /^refs\/tags\//`),
	}
}
