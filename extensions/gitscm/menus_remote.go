package gitscm

import "github.com/rm4n0s/emetos/extension"

// The submenus Pull, Push and Remote of the "..." menus of the Source Control view, and the Pull, Push and Fetch entries of its header.
const (
	pullPushSubmenu = "git.pullpush"
	remotesSubmenu  = "git.remotes"
)

// remoteMenus are the menu items of the remote commands: Pull, Push and Fetch in the header of the view and of a repository, the Pull, Push
// submenu with Sync, Pull, Push and Fetch, the Remote submenu, and Publish Branch in the Branch submenu.
//
// Ported from VS Code: extensions/git/package.json (contributes.menus: scm/title, scm/repository, git.pullpush, git.remotes, git.branch)
func remoteMenus() []extension.MenuItem {
	const git = "scmProvider == git"
	var out []extension.MenuItem
	for _, menu := range []string{extension.MenuSCMTitle, extension.MenuSCMRepository} {
		out = append(out,
			extension.MenuItem{Menu: menu, Command: "git.pull", Group: "1_header", Order: 1, When: git},
			extension.MenuItem{Menu: menu, Command: "git.push", Group: "1_header", Order: 2, When: git},
			extension.MenuItem{Menu: menu, Command: "git.fetch", Group: "1_header", Order: 5, When: git},
			extension.MenuItem{Menu: menu, Submenu: pullPushSubmenu, Group: "2_main", Order: 3, When: git},
			extension.MenuItem{Menu: menu, Submenu: remotesSubmenu, Group: "2_main", Order: 5, When: git},
		)
	}
	return append(out,
		// VS Code's "gitState == idle" is a key nothing sets, so its Sync (Rebase) never shows; Emetos shows it once the repositories are known
		item(pullPushSubmenu, "git.sync", "1_sync@1", ""),
		item(pullPushSubmenu, "git.syncRebase", "1_sync@2", "git.state == initialized"),
		item(pullPushSubmenu, "git.pull", "2_pull@1", ""),
		item(pullPushSubmenu, "git.pullRebase", "2_pull@2", ""),
		item(pullPushSubmenu, "git.pullFrom", "2_pull@3", ""),
		item(pullPushSubmenu, "git.push", "3_push@1", ""),
		item(pullPushSubmenu, "git.pushForce", "3_push@2", "config.git.allowForcePush"),
		item(pullPushSubmenu, "git.pushTo", "3_push@3", ""),
		item(pullPushSubmenu, "git.pushToForce", "3_push@4", "config.git.allowForcePush"),
		item(pullPushSubmenu, "git.fetch", "4_fetch@1", ""),
		item(pullPushSubmenu, "git.fetchPrune", "4_fetch@2", ""),
		item(pullPushSubmenu, "git.fetchAll", "4_fetch@3", ""),
		item(remotesSubmenu, "git.addRemote", "remote@1", ""),
		item(remotesSubmenu, "git.removeRemote", "remote@2", ""),
		item(branchSubmenu, "git.publish", "4_publish@1", ""),
	)
}
