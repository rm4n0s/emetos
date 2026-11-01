package gitscm

import (
	"context"
	"encoding/json"
	"net/url"
	"path/filepath"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
	"github.com/rm4n0s/emetos/extensions/gitscm/trust"
	"github.com/rm4n0s/emetos/extensions/internal/statefile"
)

// Activate implements extension.Extension. It sets up the Git output channel, the trust store, the commands and the subscriptions,
// finds git, and scans the folder for repositories. Nothing here starts git in a repository.
func (x *gitscm) Activate(ctx context.Context, api extension.API) error {
	x.api, x.ctx, x.log = api, ctx, api.Log()
	x.folder = api.Scope().Folder
	if x.folder != "" {
		x.folder = trust.Canonical(x.folder)
	}
	x.clock = x.opts.Clock
	if x.clock == nil {
		x.clock = realClock{}
	}
	x.home = homeDir(x.opts)
	x.model = newModel(x)
	x.handlers = map[string]extension.CommandHandler{}
	x.editors = map[string]*editorWait{}

	ch, err := api.Output().Channel(ChannelName)
	if err != nil {
		return err
	}
	x.channel = ch

	stateDir := x.opts.StateDir
	if stateDir == "" {
		if stateDir, err = statefile.DirFromEnv(); err != nil {
			return err
		}
	}
	x.stateDir = stateDir
	x.state = newStateStore(stateDir)
	x.focused = true
	store, err := trust.Open(trust.Path(stateDir))
	if err != nil {
		// the file is ignored and the question is asked: one warning (SPEC §10.30)
		x.log.Warn("the trust file cannot be used", "err", err)
		x.logLine("The trust file cannot be used: " + err.Error())
		api.Notifications().Warn("Emetos could not use its list of trusted Git repositories, so it asks again: " + describe(err))
	}
	x.trust = store
	if dir := devTrustDir(x.opts); dir != "" {
		x.trust.TrustForSession(dir)
	}
	x.cache = openCache(trust.Path(stateDir), x.log)
	if x.folder != "" {
		x.cache.setFolders(func() []string { return []string{x.folder} })
	}

	x.cfg.load(api)
	x.syncHelperSettings()
	x.loadTimelineSettings()
	api.Settings().OnChange(x.settingsChanged)

	if err := x.registerCommands(); err != nil {
		return err
	}
	if _, err := api.Events().Subscribe(extension.TopicWindowFocusChanged, x.onFocus); err != nil {
		return err
	}
	if _, err := api.Events().Subscribe(extension.TopicDocumentSaved, x.onSaved); err != nil {
		return err
	}
	if _, err := api.Events().Subscribe(extension.TopicWorkspaceClosed, x.stopHelper); err != nil {
		return err
	}
	if _, err := api.Events().Subscribe(extension.TopicDocumentClosed, x.onClosed); err != nil {
		return err
	}
	if _, err := api.Events().Subscribe(extension.TopicFilesChanged, x.onFiles); err != nil {
		return err
	}
	if _, err := api.Events().Subscribe(extension.TopicEditorActiveChanged, x.onActiveEditor); err != nil {
		return err
	}
	if _, err := api.Events().Subscribe(extension.TopicMergeEditorCompleted, x.onMergeCompleted); err != nil {
		return err
	}
	if err := api.Events().Handle(TopicStatus, x.handleStatus); err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		x.stopped.Store(true)
	}()
	x.registerDev()
	x.startDecorations()
	x.provideContent()
	x.provideMultiDiff()
	x.blame = newBlame(x)
	if err := x.blame.start(); err != nil {
		return err
	}
	x.provideStashMultiDiff()
	x.model.updateContext()
	if x.cfg.enabled {
		x.start()
	}
	return nil
}

// start finds git and then scans for repositories.
func (x *gitscm) start() {
	if x.started {
		return
	}
	x.started = true
	hints := append([]string(nil), x.cfg.paths...)
	go func() {
		inst, err := git.Find(x.ctx, hints, git.FindOptions{Log: x.logLine, Env: x.opts.Env})
		x.api.Post(func() { x.gitFound(inst, err) })
	}()
}

// gitFound is the end of the search for git.
func (x *gitscm) gitFound(inst git.Installation, err error) {
	if x.stopped.Load() {
		return
	}
	if err != nil {
		x.missing = true
		x.logLine("git is not available: " + err.Error())
		x.model.state = "initialized"
		x.model.updateContext()
		if !x.cfg.ignoreMissingGitWarning {
			x.api.Notifications().Warn("Git is not installed, or is older than 2.30. Install it with your package manager and reload the window.",
				extension.NotificationAction{Label: "Don't Show Again", Run: func() { x.saveSetting("git.ignoreMissingGitWarning", true) }},
				extension.NotificationAction{Label: "Show Git Output", Run: func() { x.channel.Show(false) }})
		}
		return
	}
	x.missing = false
	x.install = inst
	x.logLine("Using git " + inst.Version.String() + " from " + inst.Path)
	x.runner = git.NewRunner(git.Config{
		Path: inst.Path, Version: inst.Version, Env: x.opts.Env, Logger: x.logLine, CommandsToLog: x.cfg.commandsToLog, Trust: git.TrustedBy(x.trust),
		ExtraFor: x.helperEnv,
	})
	x.model.scan()
	// repositories made before git was found (a setting that turned Git on) start now
	for _, r := range x.model.order {
		if x.trustedBy(r.loc) {
			r.start()
		}
	}
}

// syncHelperSettings gives the goroutines of the commands the two settings that decide the variables of git's programs.
func (x *gitscm) syncHelperSettings() {
	x.askpassOn.Store(x.cfg.useIntegratedAskPass)
	x.editorOn.Store(x.cfg.useEditorAsCommitInput)
}

// logLine writes a line to the Git channel; it may be called from any goroutine.
func (x *gitscm) logLine(line string) {
	if x.channel != nil {
		x.channel.AppendLine(line)
	}
}

// describe is the message of an error for the user.
func describe(err error) string {
	if appErr, ok := errors.FromError(err); ok && appErr.Message != "" {
		return appErr.Message
	}
	return err.Error()
}

// saveSetting writes a setting of the extension and logs a failure.
func (x *gitscm) saveSetting(key string, value any) {
	if err := x.api.Settings().Update(key, value); err != nil {
		x.log.Warn("cannot save a setting", "key", key, "err", err)
	}
}

// settingsChanged follows a change of the settings: git.enabled starts and stops everything, the settings that change what the status
// shows refresh the repositories, and git.autorefresh starts and stops the watchers.
func (x *gitscm) settingsChanged() {
	old := x.cfg
	x.cfg.load(x.api)
	x.syncHelperSettings()
	x.loadTimelineSettings()
	x.autofetchSettingChanged()
	switch {
	case old.enabled && !x.cfg.enabled:
		for _, r := range append([]*repository(nil), x.model.order...) {
			x.model.drop(r)
		}
		x.model.updateContext()
		x.blame.schedule() // there is no repository to blame in any more: the text goes
		return
	case !old.enabled && x.cfg.enabled:
		if x.runner != nil {
			x.model.scan()
		} else {
			x.start()
		}
		return
	}
	if !x.cfg.enabled {
		return
	}
	if old.decorationsEnabled != x.cfg.decorationsEnabled {
		for _, r := range x.model.order {
			r.syncDecorations()
		}
	}
	x.blame.settingsChanged(old)
	statusChanged := old.untrackedChanges != x.cfg.untrackedChanges || old.statusLimit != x.cfg.statusLimit || old.similarityThreshold != x.cfg.similarityThreshold ||
		old.ignoreSubmodules != x.cfg.ignoreSubmodules || old.countBadge != x.cfg.countBadge || old.enableStatusBarSync != x.cfg.enableStatusBarSync ||
		old.commitShortHashLength != x.cfg.commitShortHashLength || old.alwaysShowStagedGroup != x.cfg.alwaysShowStagedGroup
	for _, r := range x.model.order {
		if old.autorefresh != x.cfg.autorefresh && r.repo != nil {
			if x.cfg.autorefresh {
				r.startWatching()
			} else {
				r.stopWatching()
			}
		}
		if old.alwaysShowStagedGroup != x.cfg.alwaysShowStagedGroup && r.groups.index != nil {
			r.groups.index.SetHideWhenEmpty(!x.cfg.alwaysShowStagedGroup)
		}
		r.updateCommitUI()
		if old.inputValidation != x.cfg.inputValidation || old.inputValidationLength != x.cfg.inputValidationLength ||
			old.inputValidationSubjectLength != x.cfg.inputValidationSubjectLength {
			r.revalidate()
		}
		if statusChanged {
			r.refresh()
		}
	}
	if old.autoDetection != x.cfg.autoDetection || old.openInParentFolders != x.cfg.openInParentFolders || old.scanMaxDepth != x.cfg.scanMaxDepth ||
		old.detectSubmodules != x.cfg.detectSubmodules {
		if x.runner != nil {
			x.model.scan()
		}
	}
}

// onFocus refreshes the repositories when the window gets the focus again (SPEC §10.30 "Refresh").
func (x *gitscm) onFocus(ev extension.Event) {
	var f extension.WindowFocus
	if err := jsonUnmarshal(ev.Data, &f); err != nil {
		return
	}
	x.focused = f.Focused
	if !f.Focused {
		return
	}
	for _, r := range x.model.order {
		if x.cfg.autorefresh {
			r.scheduleRefresh()
		}
		r.pokeAutofetch() // a fetch that was due waits for the window to have the focus
	}
}

// onSaved refreshes the repository of a file that was saved.
func (x *gitscm) onSaved(ev extension.Event) {
	var saved extension.DocumentSaved
	if err := jsonUnmarshal(ev.Data, &saved); err != nil || !x.cfg.autorefresh {
		return
	}
	if path, ok := uriPath(saved.URI); ok {
		x.editorSaved(path)
		if isIgnoreFile(path) {
			x.ignoresChanged()
		}
		if r := x.model.forPath(path); r != nil && r.repo != nil {
			r.scheduleRefresh()
		}
	}
}

// onFiles follows the changes of the folder of the window tab, to find the repositories that appear.
func (x *gitscm) onFiles(ev extension.Event) {
	var files extension.FilesChanged
	if err := jsonUnmarshal(ev.Data, &files); err != nil {
		return
	}
	for _, c := range files.Changes {
		if isIgnoreFile(c.Path) {
			x.ignoresChanged()
			break
		}
	}
	x.model.onFilesChanged(files.Changes)
}

// editorDirs lists the folders of the files that are open outside the folder of the window tab.
func (x *gitscm) editorDirs() []string {
	var dirs []string
	seen := map[string]bool{}
	for _, uri := range x.api.Documents().Open() {
		path, ok := uriPath(uri)
		if !ok {
			continue
		}
		if x.folder != "" && git.Within(path, x.folder) {
			continue
		}
		dir := filepath.Dir(path)
		if !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// excludeSetting is files.exclude as the patterns that are switched on.
func (x *gitscm) excludeSetting() map[string]bool {
	m, err := extension.SettingValue[map[string]bool](x.api.Settings(), "files.exclude")
	if err != nil {
		return nil
	}
	return m
}

// ignoredRepository says whether git.ignoredRepositories lists the repository: an absolute path, or a path relative to the folder.
func (x *gitscm) ignoredRepository(root string) bool {
	for _, p := range x.cfg.ignoredRepositories {
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(x.folder, p)
		}
		if trust.Canonical(p) == root || filepath.Clean(p) == root {
			return true
		}
	}
	return false
}

// run runs one of the commands of the extension, for a button of a notification.
func (x *gitscm) run(id string) {
	if h := x.handlers[id]; h != nil {
		if err := h(x.ctx, nil); err != nil {
			x.log.Warn("a command failed", "command", id, "err", err)
		}
	}
}

// uriOf is the file: URI of a path.
func uriOf(path string) string { return (&url.URL{Scheme: "file", Path: path}).String() }

// jsonUnmarshal decodes the data of an event.
func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
