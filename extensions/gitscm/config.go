package gitscm

import (
	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// config is the settings of the extension that the repositories read, as they were when load last ran.
type config struct {
	enabled                 bool
	paths                   []string
	autoDetection           string // "true", "false", "subFolders" or "openEditors"
	autorefresh             bool
	untrackedChanges        string
	statusLimit             int
	similarityThreshold     int
	ignoreSubmodules        bool
	countBadge              string
	detectSubmodules        bool
	detectSubmodulesLimit   int
	detectWorktrees         bool
	detectWorktreesLimit    int
	ignoredRepositories     []string
	scanRepositories        []string
	scanMaxDepth            int
	scanIgnoredFolders      []string
	openInParentFolders     string
	optimisticUpdate        bool
	alwaysShowStagedGroup   bool
	ignoreLimitWarning      bool
	enableStatusBarSync     bool
	commitShortHashLength   int
	commandsToLog           []string
	ignoreMissingGitWarning bool
	showProgress            bool
	decorationsEnabled      bool
	supportCancellation     bool
	useIntegratedAskPass    bool
	useEditorAsCommitInput  bool
	openDiffOnClick         bool
	mergeEditor             bool
	closeDiffOnOperation    bool
	discardToTrash          bool
	confirmCommittedDelete  bool
	// the settings of the commit (commit.go, commitui.go)
	enableSmartCommit, suggestSmartCommit, enableCommitSigning, alwaysSignOff, verboseCommit bool
	confirmEmptyCommits, requireGitUserConfig, allowNoVerifyCommit, confirmNoVerifyCommit    bool
	smartCommitChanges, promptToSaveFilesBeforeCommit, branchProtectionPrompt                string
	branchProtection                                                                         []string
	diagnosticsHook                                                                          bool
	diagnosticsSources                                                                       map[string]string
	postCommitCommand                                                                        string
	rememberPostCommitCommand, showCommitInput, inputValidation                              bool
	inputValidationLength, inputValidationSubjectLength                                      int
	showActionButton                                                                         actionButtonSettings
	// blame* are the settings of the blame text (blame.go).
	blameEditor, blameStatusBar, blameDisableHover, blameIgnoreWhitespace bool
	blameEditorTemplate, blameStatusBarTemplate                           string
	timelineDate                                                          string // "committed" or "authored"
	timelineShowAuthor                                                    bool
	timelineShowUncommitted                                               bool
}

// read gets a setting as a Go type, the default when it cannot be read (the manifest declares every key, so that is a bug of the
// host).
func read[T any](api extension.API, key string, def T) T {
	v, err := extension.SettingValue[T](api.Settings(), key)
	if err != nil {
		return def
	}
	return v
}

// load reads the settings.
func (c *config) load(api extension.API) {
	c.enabled = read(api, "git.enabled", true)
	// git.path is a string, null or an array of strings
	switch v, _ := api.Settings().Get("git.path"); p := v.(type) {
	case string:
		c.paths = []string{p}
	case []any:
		c.paths = nil
		for _, e := range p {
			if s, ok := e.(string); ok {
				c.paths = append(c.paths, s)
			}
		}
	default:
		c.paths = nil
	}
	// git.autoRepositoryDetection is true, false or a string
	switch v, _ := api.Settings().Get("git.autoRepositoryDetection"); a := v.(type) {
	case bool:
		if a {
			c.autoDetection = "true"
		} else {
			c.autoDetection = "false"
		}
	case string:
		c.autoDetection = a
	default:
		c.autoDetection = "true"
	}
	c.autorefresh = read(api, "git.autorefresh", true)
	c.untrackedChanges = read(api, "git.untrackedChanges", "mixed")
	c.statusLimit = read(api, "git.statusLimit", 10000)
	c.similarityThreshold = read(api, "git.similarityThreshold", 50)
	c.ignoreSubmodules = read(api, "git.ignoreSubmodules", false)
	c.countBadge = read(api, "git.countBadge", "all")
	c.detectSubmodules = read(api, "git.detectSubmodules", true)
	c.detectSubmodulesLimit = read(api, "git.detectSubmodulesLimit", 10)
	c.detectWorktrees = read(api, "git.detectWorktrees", false)
	c.detectWorktreesLimit = read(api, "git.detectWorktreesLimit", 50)
	c.ignoredRepositories = read(api, "git.ignoredRepositories", []string(nil))
	c.scanRepositories = read(api, "git.scanRepositories", []string(nil))
	c.scanMaxDepth = read(api, "git.repositoryScanMaxDepth", 1)
	c.scanIgnoredFolders = read(api, "git.repositoryScanIgnoredFolders", []string{"node_modules"})
	c.openInParentFolders = read(api, "git.openRepositoryInParentFolders", "prompt")
	c.optimisticUpdate = read(api, "git.optimisticUpdate", true)
	c.alwaysShowStagedGroup = read(api, "git.alwaysShowStagedChangesResourceGroup", false)
	c.ignoreLimitWarning = read(api, "git.ignoreLimitWarning", false)
	c.enableStatusBarSync = read(api, "git.enableStatusBarSync", true)
	c.commitShortHashLength = read(api, "git.commitShortHashLength", 7)
	c.commandsToLog = read(api, "git.commandsToLog", []string(nil))
	c.ignoreMissingGitWarning = read(api, "git.ignoreMissingGitWarning", false)
	c.showProgress = read(api, "git.showProgress", true)
	c.decorationsEnabled = read(api, "git.decorations.enabled", true)
	c.supportCancellation = read(api, "git.supportCancellation", false)
	c.useIntegratedAskPass = read(api, "git.useIntegratedAskPass", true)
	c.useEditorAsCommitInput = read(api, "git.useEditorAsCommitInput", true)
	c.openDiffOnClick = read(api, "git.openDiffOnClick", true)
	c.mergeEditor = read(api, "git.mergeEditor", false)
	c.closeDiffOnOperation = read(api, "git.closeDiffOnOperation", false)
	c.discardToTrash = read(api, "git.discardUntrackedChangesToTrash", true)
	c.confirmCommittedDelete = read(api, "git.confirmCommittedDelete", true)
	c.enableSmartCommit = read(api, "git.enableSmartCommit", false)
	c.suggestSmartCommit = read(api, "git.suggestSmartCommit", true)
	c.smartCommitChanges = read(api, "git.smartCommitChanges", "all")
	c.enableCommitSigning = read(api, "git.enableCommitSigning", false)
	c.alwaysSignOff = read(api, "git.alwaysSignOff", false)
	c.verboseCommit = read(api, "git.verboseCommit", false)
	c.confirmEmptyCommits = read(api, "git.confirmEmptyCommits", true)
	c.requireGitUserConfig = read(api, "git.requireGitUserConfig", true)
	c.allowNoVerifyCommit = read(api, "git.allowNoVerifyCommit", false)
	c.confirmNoVerifyCommit = read(api, "git.confirmNoVerifyCommit", true)
	c.promptToSaveFilesBeforeCommit = read(api, "git.promptToSaveFilesBeforeCommit", "always")
	c.branchProtection = read(api, "git.branchProtection", []string(nil))
	c.branchProtectionPrompt = read(api, "git.branchProtectionPrompt", "alwaysPrompt")
	c.diagnosticsHook = read(api, "git.diagnosticsCommitHook.enabled", false)
	c.diagnosticsSources = read(api, "git.diagnosticsCommitHook.sources", map[string]string{"*": "error"})
	c.postCommitCommand = read(api, "git.postCommitCommand", "none")
	c.rememberPostCommitCommand = read(api, "git.rememberPostCommitCommand", false)
	c.showCommitInput = read(api, "git.showCommitInput", true)
	c.inputValidation = read(api, "git.inputValidation", false)
	c.inputValidationLength = read(api, "git.inputValidationLength", 72)
	// null inherits git.inputValidationLength
	c.inputValidationSubjectLength = c.inputValidationLength
	if v, err := api.Settings().Get("git.inputValidationSubjectLength"); err == nil {
		if n, ok := v.(float64); ok {
			c.inputValidationSubjectLength = int(n)
		}
	}
	// a member that the object leaves out stays on, as the objects of the settings merge with their default
	shown := read(api, "git.showActionButton", map[string]bool{})
	on := func(key string) bool { v, ok := shown[key]; return !ok || v }
	c.showActionButton = actionButtonSettings{Commit: on("commit"), Publish: on("publish"), Sync: on("sync")}
	c.blameEditor = read(api, "git.blame.editorDecoration.enabled", false)
	c.blameEditorTemplate = read(api, "git.blame.editorDecoration.template", "${subject}, ${authorName} (${authorDateAgo})")
	c.blameDisableHover = read(api, "git.blame.editorDecoration.disableHover", false)
	c.blameStatusBar = read(api, "git.blame.statusBarItem.enabled", true)
	c.blameStatusBarTemplate = read(api, "git.blame.statusBarItem.template", "${authorName} (${authorDateAgo})")
	c.blameIgnoreWhitespace = read(api, "git.blame.ignoreWhitespace", false)
	c.timelineDate = read(api, "git.timeline.date", "committed")
	c.timelineShowAuthor = read(api, "git.timeline.showAuthor", true)
	c.timelineShowUncommitted = read(api, "git.timeline.showUncommitted", false)
}

// statusOptions are the options of git status for the settings (git.untrackedChanges, git.statusLimit, git.similarityThreshold,
// git.ignoreSubmodules).
func (c *config) statusOptions() git.StatusOptions {
	o := git.StatusOptions{IgnoreSubmodules: c.ignoreSubmodules, SimilarityThreshold: c.similarityThreshold, Limit: c.statusLimit}
	if c.untrackedChanges == "hidden" {
		o.Untracked = git.UntrackedNone
	}
	return o
}
