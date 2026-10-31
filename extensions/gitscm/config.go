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
	openDiffOnClick         bool
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
	c.openDiffOnClick = read(api, "git.openDiffOnClick", true)
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
