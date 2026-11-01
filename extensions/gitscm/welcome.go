package gitscm

import "github.com/rm4n0s/emetos/extension"

// The conditions the welcome contents share (VS Code's viewsWelcome of extensions/git/package.json).
const (
	whenReady     = "config.git.enabled && !git.missing"
	whenInit      = whenReady + " && git.state == initialized"
	whenNoRepos   = whenInit + " && scm.providerCount == 0"
	whenNoneAbove = " && git.parentRepositoryCount == 0 && git.unsafeRepositoryCount == 0 && git.closedRepositoryCount == 0 && git.untrustedRepositoryCount == 0"
)

// welcomePieces are the welcome contents of the Source Control view and of the Explorer (SPEC §10.30 "Welcome content"): VS Code's, with
// the texts that point to its documentation and its marketplace left out, the Git-not-found text of Emetos (no download link:
// Emetos downloads nothing), and the text of the repositories that wait for the trust question (D37).
//
// Ported from VS Code: extensions/git/package.json (contributes.viewsWelcome)
func welcomePieces() []extension.ViewWelcomeSpec {
	const settings = "command:workbench.action.openSettings?%5B%22"
	return []extension.ViewWelcomeSpec{
		{View: "scm", When: "!config.git.enabled",
			Contents: "If you would like to use Git features, please enable Git in your [settings](" + settings + "git.enabled%22%5D)."},
		{View: "scm", When: "config.git.enabled && git.missing",
			Contents: "Git is not installed, or is older than 2.30. Install it with your package manager and reload the window.\n[Show Git Output](command:git.showOutput)"},
		{View: "scm", When: whenReady + " && workbenchState == empty" + whenNoneAbove, Enablement: "git.state == initialized", Group: "2_open@1",
			Contents: "In order to use Git features, you can open a folder containing a Git repository or clone from a URL.\n[Open Folder](command:workbench.action.files.openFolder)\n[Clone Repository](command:git.cloneRecursive)"},
		{View: "scm", When: whenReady + " && workbenchState == folder && git.state != initialized",
			Contents: "Scanning folder for Git repositories..."},
		{View: "scm", When: whenInit + " && workbenchState == folder && scm.providerCount == 0" + whenNoneAbove, Group: "5_scm@1",
			Contents: "The folder currently open doesn't have a Git repository. You can initialize a repository which will enable source control features powered by Git.\n[Initialize Repository](command:git.init?%5Btrue%5D)\n[Clone Repository](command:git.clone)"},
		{View: "scm", When: whenInit + " && git.parentRepositoryCount == 1",
			Contents: "A Git repository was found in the parent folders of the workspace or the open file(s).\n[Open Repository](command:git.openRepositoriesInParentFolders)\nUse the [git.openRepositoryInParentFolders](" + settings + "git.openRepositoryInParentFolders%22%5D) setting to control whether Git repositories in parent folders of workspaces or open files are opened."},
		{View: "scm", When: whenInit + " && git.parentRepositoryCount > 1",
			Contents: "Git repositories were found in the parent folders of the workspace or the open file(s).\n[Open Repository](command:git.openRepositoriesInParentFolders)\nUse the [git.openRepositoryInParentFolders](" + settings + "git.openRepositoryInParentFolders%22%5D) setting to control whether Git repositories in parent folders of workspace or open files are opened."},
		{View: "scm", When: whenInit + " && git.unsafeRepositoryCount == 1",
			Contents: "The detected Git repository is potentially unsafe as the folder is owned by someone other than the current user.\n[Manage Unsafe Repositories](command:git.manageUnsafeRepositories)"},
		{View: "scm", When: whenInit + " && git.unsafeRepositoryCount > 1",
			Contents: "The detected Git repositories are potentially unsafe as the folders are owned by someone other than the current user.\n[Manage Unsafe Repositories](command:git.manageUnsafeRepositories)"},
		{View: "scm", When: whenInit + " && git.closedRepositoryCount == 1",
			Contents: "A Git repository was found that was previously closed.\n[Reopen Closed Repository](command:git.reopenClosedRepositories)"},
		{View: "scm", When: whenInit + " && git.closedRepositoryCount > 1",
			Contents: "Git repositories were found that were previously closed.\n[Reopen Closed Repositories](command:git.reopenClosedRepositories)"},
		{View: "scm", When: whenReady + " && git.untrustedRepositoryCount == 1", Group: "1_trust@1",
			Contents: "The Git repository in the folder is not trusted. Emetos runs no Git command in it until you trust it, because Git can run programs that the repository names.\n[Trust Repository](command:git.trustRepository)\n[Learn More](https://github.com/rm4n0s/emetos#git-security)"},
		{View: "scm", When: whenReady + " && git.untrustedRepositoryCount > 1", Group: "1_trust@1",
			Contents: "The Git repositories in the folder are not trusted. Emetos runs no Git command in them until you trust them, because Git can run programs that a repository names.\n[Trust Repository](command:git.trustRepository)\n[Learn More](https://github.com/rm4n0s/emetos#git-security)"},
		{View: "explorer", When: "config.git.enabled && git.state == initialized && scm.providerCount == 0", Group: "5_scm@1",
			Contents: "You can clone a repository locally.\n[Clone Repository](command:git.clone)"},
	}
}
