package gitscm

import "github.com/rm4n0s/emetos/extension"

// stashSubmenu is the id of the Stash submenu of the "..." menus of the Source Control view.
const stashSubmenu = "git.stash"

// stashMenus are the menu items of the stash: the Stash submenu in the "..." menu of the view and of a repository, its entries, and the
// actions in the title of the editor of a stash.
//
// Ported from VS Code: extensions/git/package.json (contributes.menus: scm/title, scm/repository, git.stash, editor/title)
func stashMenus() []extension.MenuItem {
	const (
		enabled = "config.git.enabled && !git.missing"
		git     = "scmProvider == git"
	)
	inStash := stashSubmenu
	return []extension.MenuItem{
		{Menu: extension.MenuSCMTitle, Submenu: stashSubmenu, Group: "2_main", Order: 6, When: git},
		{Menu: extension.MenuSCMRepository, Submenu: stashSubmenu, Group: "2_main", Order: 6, When: git},
		item(inStash, "git.stash", "1_stash@1", ""),
		item(inStash, "git.stashIncludeUntracked", "1_stash@2", ""),
		item(inStash, "git.stashStaged", "1_stash@3", "gitVersion235"),
		item(inStash, "git.stashApplyLatest", "2_apply@1", ""),
		item(inStash, "git.stashApply", "2_apply@2", ""),
		item(inStash, "git.stashPopLatest", "3_pop@1", ""),
		item(inStash, "git.stashPop", "3_pop@2", ""),
		item(inStash, "git.stashDrop", "4_drop@1", ""),
		item(inStash, "git.stashDropAll", "4_drop@2", ""),
		item(inStash, "git.stashView", "5_preview@1", ""),
		// VS Code shows Apply with Pop as its alternative (Alt); Emetos has no alternative actions, so Pop is a button of its own. The
		// three come before the actions of the multi-file editor (orders 1 to 3), which VS Code's apply and drop share the numbers with.
		item(extension.MenuEditorTitle, "git.stashApplyEditor", "navigation@0.1", enabled+" && resourceScheme == git-stash"),
		item(extension.MenuEditorTitle, "git.stashPopEditor", "navigation@0.2", enabled+" && resourceScheme == git-stash"),
		item(extension.MenuEditorTitle, "git.stashDropEditor", "navigation@0.3", enabled+" && resourceScheme == git-stash"),
	}
}
