package gitscm

import "github.com/rm4n0s/emetos/extension"

// commitSubmenu is the id of the Commit submenu of the "..." menus of the Source Control view (VS Code's git.commit submenu).
const commitSubmenu = "git.commit"

// commitMenus are the menu items of the commit: the check mark in the title of the view, the Commit submenu in the "..." menu of the view
// and of a repository, and the commands in it.
//
// Not here: Abort Rebase, which VS Code lists in the submenu with the commit commands and step 24.14 adds with the command.
//
// Ported from VS Code: extensions/git/package.json (contributes.menus: scm/title, scm/repository, git.commit)
func commitMenus() []extension.MenuItem {
	const (
		git      = "scmProvider == git"
		noVerify = "config.git.allowNoVerifyCommit"
	)
	in := commitSubmenu
	return []extension.MenuItem{
		{Menu: extension.MenuSCMTitle, Command: "git.commit", Group: "navigation", Order: -1, When: git},
		{Menu: extension.MenuSCMTitle, Submenu: commitSubmenu, Group: "2_main", Order: 1, When: git},
		{Menu: extension.MenuSCMRepository, Submenu: commitSubmenu, Group: "2_main", Order: 1, When: git},
		item(in, "git.commit", "1_commit@1", ""),
		item(in, "git.commitStaged", "1_commit@2", ""),
		item(in, "git.commitAll", "1_commit@3", ""),
		item(in, "git.undoCommit", "1_commit@4", ""),
		item(in, "git.commitNoVerify", "2_commit_noverify@1", noVerify),
		item(in, "git.commitStagedNoVerify", "2_commit_noverify@2", noVerify),
		item(in, "git.commitAllNoVerify", "2_commit_noverify@3", noVerify),
		item(in, "git.commitAmend", "3_amend@1", ""),
		item(in, "git.commitStagedAmend", "3_amend@2", ""),
		item(in, "git.commitAllAmend", "3_amend@3", ""),
		item(in, "git.commitAmendNoVerify", "4_amend_noverify@1", noVerify),
		item(in, "git.commitStagedAmendNoVerify", "4_amend_noverify@2", noVerify),
		item(in, "git.commitAllAmendNoVerify", "4_amend_noverify@3", noVerify),
		item(in, "git.commitSigned", "5_signoff@1", ""),
		item(in, "git.commitStagedSigned", "5_signoff@2", ""),
		item(in, "git.commitAllSigned", "5_signoff@3", ""),
		item(in, "git.commitSignedNoVerify", "6_signoff_noverify@1", noVerify),
		item(in, "git.commitStagedSignedNoVerify", "6_signoff_noverify@2", noVerify),
		item(in, "git.commitAllSignedNoVerify", "6_signoff_noverify@3", noVerify),
	}
}
