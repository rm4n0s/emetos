package gitscm

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// Clone, initialize and open a repository (SPEC §10.30 "Clone, init, open"). The commands ask on the UI thread (an input box, the file
// chooser of the desktop on a goroutine of their own), run git on a goroutine and come back through api.Post. A repository that these
// commands make is trusted at once (D37): the user made it. VS Code's quick pick of remote source providers is not built (no providers
// exist); the URL is typed in an input box.
//
// Ported from VS Code: extensions/git/src/cloneManager.ts and commands.ts (clone, cloneRecursive, init, openRepository)

// reGitClone is the "git clone " a pasted command line starts with.
var reGitClone = regexp.MustCompile(`^git\s+clone\s+`)

// cloneRequest is a clone being made.
type cloneRequest struct {
	// url is the address, trimmed and without the "git clone " of a pasted command.
	url string
	// parent is the folder the clone goes into; empty asks for it.
	parent string
	// ref is the branch or tag to check out.
	ref string
	// recursive clones the submodules too.
	recursive bool
	// none says that nothing is asked and opened after the clone (the "none" post clone action of VS Code's API).
	none bool
}

// cloneArgs reads the arguments of git.clone and git.cloneRecursive: the URL as a string, or {"url", "parentPath", "ref",
// "postCloneAction": "none"}. VS Code's command takes (url, parentPath, options) as separate arguments; a list is read the same way.
func cloneArgs(args any) cloneRequest {
	var req cloneRequest
	read := func(a any) {
		switch v := a.(type) {
		case string:
			if req.url == "" {
				req.url = v
			} else if req.parent == "" {
				req.parent = v
			}
		case map[string]any:
			if s, ok := v["url"].(string); ok {
				req.url = s
			}
			if s, ok := v["parentPath"].(string); ok {
				req.parent = s
			}
			if s, ok := v["ref"].(string); ok {
				req.ref = s
			}
			if s, ok := v["postCloneAction"].(string); ok && s == "none" {
				req.none = true
			}
		}
	}
	if list, ok := args.([]any); ok {
		for _, a := range list {
			read(a)
		}
	} else {
		read(args)
	}
	req.url = cleanCloneURL(req.url)
	return req
}

// cleanCloneURL trims a typed address and takes off the "git clone " of a pasted command.
func cleanCloneURL(s string) string {
	return reGitClone.ReplaceAllString(strings.TrimSpace(s), "")
}

// validateCloneURL is the validation of the input box that asks for the address.
func validateCloneURL(s string) string {
	return git.ValidateRemoteURL(cleanCloneURL(s))
}

// safeNotification is a message that no link or command in it can act on: a text with a Markdown link or a "command:" in it (the
// address of a repository is typed by the user or comes from a web page) is replaced by the fallback.
//
// Ported from VS Code: extensions/git/src/notification.ts (getSafeNotificationMessage)
func safeNotification(message, fallback string) string {
	if strings.Contains(message, "](") || strings.Contains(strings.ToLower(message), "command:") {
		return fallback
	}
	return message
}

// cmdClone is git.clone.
//
// Ported from VS Code: extensions/git/src/commands.ts (clone)
func (x *gitscm) cmdClone(_ context.Context, args any) error {
	x.startClone(cloneArgs(args))
	return nil
}

// cmdCloneRecursive is git.cloneRecursive.
//
// Ported from VS Code: extensions/git/src/commands.ts (cloneRecursive)
func (x *gitscm) cmdCloneRecursive(_ context.Context, args any) error {
	req := cloneArgs(args)
	req.recursive = true
	x.startClone(req)
	return nil
}

// startClone asks for the address when the request has none and goes on with the clone.
func (x *gitscm) startClone(req cloneRequest) {
	if x.runner == nil {
		x.api.Notifications().Error("Git is not available.")
		return
	}
	if req.url != "" {
		if msg := git.ValidateRemoteURL(req.url); msg != "" {
			x.api.Notifications().Error(msg)
			return
		}
		x.cloneFrom(req)
		return
	}
	err := x.api.InputBox().Show(extension.InputBoxOptions{
		Prompt: "Provide repository URL", Message: "Clone from URL", IgnoreFocusOut: true, Validate: validateCloneURL,
	}, func(value string, ok bool) {
		req.url = cleanCloneURL(value)
		if !ok || req.url == "" || x.stopped.Load() {
			return
		}
		x.cloneFrom(req)
	})
	if err != nil {
		x.logUnavailable("cannot ask for the URL to clone", err)
	}
}

// cloneFrom clones an address, or opens the clone of it that the repository cache knows.
//
// Ported from VS Code: extensions/git/src/cloneManager.ts (clone, tryOpenExistingRepository)
func (x *gitscm) cloneFrom(req cloneRequest) {
	if x.cache != nil {
		if cached, ok := x.cache.get(req.url); ok && len(cached) > 0 {
			x.tryOpenExisting(cached, req)
			return
		}
	}
	x.cloneRepository(req)
}

// tryOpenExisting offers the clones of the address that exist already: the one in the folder of the window tab is the answer (nothing
// is cloned or opened), a single other one is opened, and several are a pick that also has Clone again. Clones whose folder is gone are
// forgotten.
func (x *gitscm) tryOpenExisting(cached []repoCacheInfo, req cloneRequest) {
	var existing []repoCacheInfo
	for _, c := range cached {
		if _, err := os.Stat(c.WorkspacePath); err == nil {
			existing = append(existing, c)
		}
	}
	if len(existing) == 0 {
		x.cloneRepository(req)
		return
	}
	for _, c := range existing {
		if x.folder != "" && c.WorkspacePath == x.folder {
			x.api.Notifications().Info("The repository is open already, in " + c.WorkspacePath + ".")
			return
		}
	}
	if len(existing) == 1 {
		x.postClone(existing[0].WorkspacePath, req)
		return
	}
	items := make([]extension.QuickPickItem, 0, len(existing)+1)
	for _, c := range existing {
		items = append(items, extension.QuickPickItem{Label: filepath.Base(c.WorkspacePath), Description: c.WorkspacePath})
	}
	items = append(items, extension.QuickPickItem{Label: "Clone again"})
	err := x.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Open Existing Repository Clone", Items: items, MatchOnDescription: true}, func(i int) {
		switch {
		case i < 0 || x.stopped.Load():
		case i == len(existing):
			x.cloneRepository(req)
		default:
			x.postClone(existing[i].WorkspacePath, req)
		}
	})
	if err != nil {
		x.logUnavailable("cannot show the clones that exist", err)
	}
}

// defaultCloneDirectory is the folder the chooser of the destination starts in: git.defaultCloneDirectory with a leading "~" for the
// home folder, else the home folder.
func (x *gitscm) defaultCloneDirectory() string {
	dir := ""
	if v, _ := x.api.Settings().Get("git.defaultCloneDirectory"); v != nil {
		dir, _ = v.(string)
	}
	if dir == "" {
		return x.home
	}
	if dir == "~" || strings.HasPrefix(dir, "~/") {
		return filepath.Join(x.home, strings.TrimPrefix(dir, "~"))
	}
	return dir
}

// chooseFolder shows the folder chooser of the desktop on a goroutine and calls done on the UI thread with the folder, "" when the dialog
// was closed or could not be shown.
func (x *gitscm) chooseFolder(opts extension.FileChooserOptions, done func(folder string)) {
	opts.Directory = true
	if origin := x.api.Origins().Current(); origin != extension.HostOrigin {
		// the file chooser of the desktop opens on the screen of the host, where a client of a hosted workspace cannot see it (SPEC §10.18
		// "As built (step 24.25)"): the folder is typed in a box of the client's view
		x.askFolder(opts, origin, done)
		return
	}
	go func() {
		path, err := x.api.FileChooser().Open(x.ctx, opts)
		x.api.Post(func() {
			if x.stopped.Load() {
				return
			}
			if err != nil {
				x.logUnavailable("cannot choose a folder", err)
				done("")
				return
			}
			done(path)
		})
	}()
}

// askFolder asks for a folder in an input box on the screen of a client, in place of the chooser of the desktop; done gets "" when the box
// was closed or could not be shown.
func (x *gitscm) askFolder(opts extension.FileChooserOptions, origin extension.Origin, done func(folder string)) {
	err := x.api.InputBox().Show(extension.InputBoxOptions{
		Prompt: opts.Title, Value: opts.Folder, Message: "Type the path of a folder on the computer of the host", IgnoreFocusOut: true, Origin: origin,
		Validate: func(value string) string {
			if strings.TrimSpace(value) == "" || !filepath.IsAbs(strings.TrimSpace(value)) {
				return "The path of the folder is absolute"
			}
			return ""
		},
	}, func(value string, ok bool) {
		if x.stopped.Load() || !ok {
			done("")
			return
		}
		done(filepath.Clean(strings.TrimSpace(value)))
	})
	if err != nil {
		x.logUnavailable("cannot ask for a folder", err)
		done("")
	}
}

// cloneRepository asks for the parent folder when the request has none and runs the clone.
func (x *gitscm) cloneRepository(req cloneRequest) {
	if req.parent != "" {
		x.runClone(req)
		return
	}
	title := safeNotification("Choose a folder to clone "+git.MaskURLs(req.url)+" into", "Choose a folder to clone into")
	x.chooseFolder(extension.FileChooserOptions{Title: title, AcceptLabel: "Select as Repository Destination", Folder: x.defaultCloneDirectory()}, func(folder string) {
		if folder == "" {
			return
		}
		req.parent = folder
		x.runClone(req)
	})
}

// runClone runs git clone with the progress notification, which has a Cancel button that stops git and removes the partly cloned folder.
//
// Ported from VS Code: extensions/git/src/cloneManager.ts (cloneRepository)
func (x *gitscm) runClone(req cloneRequest) {
	if x.runner == nil || x.stopped.Load() {
		return
	}
	title := safeNotification(`Cloning git repository "`+git.MaskURLs(req.url)+`"...`, "Cloning git repository...")
	origin := x.api.Origins().Current() // the screen that asked: git's questions and the end of the clone go there
	ctx, cancel := context.WithCancel(withOrigin(x.ctx, origin))
	prog := x.api.Notifications().Progress(extension.ProgressOptions{Title: title, Cancellable: true, OnCancel: cancel})
	x.logLine("Cloning " + git.MaskURLs(req.url) + " into " + req.parent)
	runner, last := x.runner, ""
	go func() {
		defer cancel()
		path, err := runner.Clone(ctx, req.url, req.parent, git.CloneOptions{Recursive: req.recursive, Ref: req.ref, Progress: func(p git.Progress) {
			percent := p.Overall()
			if percent < 0 {
				return
			}
			if key := p.Phase + ":" + strconv.Itoa(percent); key != last {
				last = key
				x.api.Post(func() { prog.Report(p.Phase, float64(percent)) })
			}
		}})
		x.api.Post(func() {
			x.api.Origins().Run(origin, func() {
				prog.Done()
				if x.stopped.Load() {
					return
				}
				if err != nil {
					x.cloneFailed(req, err)
					return
				}
				x.madeRepository(path)
				x.postClone(path, req)
			})
		})
	}()
}

// cloneFailed tells the user why a clone failed. A clone that was cancelled says nothing.
func (x *gitscm) cloneFailed(req cloneRequest, err error) {
	if appErr, ok := errors.FromError(err); ok {
		switch appErr.Tag {
		case "GitCancelled":
			x.logLine("The clone of " + git.MaskURLs(req.url) + " was cancelled")
			return
		case "GitCloneTargetExists":
			path, _ := appErr.Metadata["path"].(string)
			x.logLine("git clone failed: " + path + " already exists and is not an empty directory")
			x.api.Notifications().Error("Git: The folder "+path+" already exists and is not an empty directory.",
				extension.NotificationAction{Label: "Show Git Output", Run: func() { x.channel.Show(false) }})
			return
		}
	}
	x.reportFailure("clone", req.parent, err, func() { x.runClone(req) })
}

// madeRepository trusts a repository that a command of the user made (Clone, Initialize Repository): the user made it. Its submodules are
// trusted with it. The answer is kept like any answer to the question; a file that cannot be written keeps it for the session and says so.
func (x *gitscm) madeRepository(root string) {
	if err := x.trust.Trust(root); err != nil {
		x.model.saveFailed(err)
	}
}

// postClone does what is to be done with a clone: nothing, open it as the folder of the window tab, open it in a new workspace tab, or
// ask. git.openAfterClone decides: always, alwaysNewWindow, whenNoFolderOpen (the tab has no folder) and prompt.
//
// Ported from VS Code: extensions/git/src/cloneManager.ts (doPostCloneAction)
func (x *gitscm) postClone(path string, req cloneRequest) {
	if req.none {
		return
	}
	switch read(x.api, "git.openAfterClone", "prompt") {
	case "always":
		x.openFolder(path, false)
		return
	case "alwaysNewWindow":
		x.openFolder(path, true)
		return
	case "whenNoFolderOpen":
		if x.folder == "" {
			x.openFolder(path, false)
			return
		}
	}
	x.ask(extension.DialogSpec{
		Type: extension.DialogQuestion, Message: "Would you like to open the repository?",
		Buttons: []extension.DialogButton{{Label: "Open"}, {Label: "Open in New Window"}, cancelButton},
	}, func(res extension.DialogResult) {
		switch {
		case res.Cancelled || x.stopped.Load():
		case res.Button == 0:
			x.openFolder(path, false)
		case res.Button == 1:
			x.openFolder(path, true)
		}
	})
}

// openFolder opens a folder as the folder of the window tab or in a new workspace tab (SPEC §10.14).
func (x *gitscm) openFolder(path string, newTab bool) {
	if err := x.api.Workspace().OpenFolder(path, newTab); err != nil {
		if appErr, ok := errors.FromError(err); ok && appErr.Tag == "ExtensionOpenFolderUnavailable" {
			x.logLine("cannot open " + path + ": " + appErr.Message)
			return
		}
		x.logUnavailable("cannot open a folder", err)
		x.api.Notifications().Error("Cannot open the folder " + path + ": " + describe(err))
	}
}

// initArgs reads the argument of git.init: skipFolderPrompt, true or [true] (the welcome content's link is git.init?[true]).
func initArgs(args any) bool {
	switch v := args.(type) {
	case bool:
		return v
	case []any:
		return len(v) > 0 && initArgs(v[0])
	}
	return false
}

// cmdInit is git.init, "Initialize Repository": the folder of the window tab (asked unless skipFolderPrompt) or a folder to choose,
// git init -b <git.defaultBranchName>, then the repository opens.
//
// Ported from VS Code: extensions/git/src/commands.ts (init)
func (x *gitscm) cmdInit(_ context.Context, args any) error {
	if x.runner == nil {
		x.api.Notifications().Error("Git is not available.")
		return nil
	}
	skip := initArgs(args)
	switch {
	case x.folder != "" && skip:
		x.initialize(x.folder, false)
	case x.folder != "":
		items := []extension.QuickPickItem{{Label: filepath.Base(x.folder), Description: x.folder}, {Label: "Choose Folder..."}}
		err := x.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: "Pick workspace folder to initialize git repo in", Items: items, IgnoreFocusOut: true}, func(i int) {
			switch i {
			case 0:
				x.initialize(x.folder, false)
			case 1:
				x.chooseToInitialize()
			}
		})
		if err != nil {
			x.logUnavailable("cannot ask which folder to initialize", err)
		}
	default:
		x.chooseToInitialize()
	}
	return nil
}

// chooseToInitialize asks for a folder to initialize. The home folder, or a folder above it, asks for a confirmation first.
func (x *gitscm) chooseToInitialize() {
	start := x.home
	if x.folder != "" {
		start = x.folder
	}
	x.chooseFolder(extension.FileChooserOptions{Title: "Initialize Repository", AcceptLabel: "Initialize Repository", Folder: start}, func(folder string) {
		if folder == "" {
			return
		}
		folder = filepath.Clean(folder)
		open := x.folder == "" || filepath.Clean(x.folder) != folder
		if x.home != "" && git.Within(x.home, folder) {
			x.ask(extension.DialogSpec{
				Type: extension.DialogWarning, Message: "This will create a Git repository in the selected folder. Are you sure you want to continue?",
				Detail:  folder,
				Buttons: []extension.DialogButton{{Label: "Initialize Repository"}, cancelButton},
			}, func(res extension.DialogResult) {
				if !res.Cancelled && res.Button == 0 && !x.stopped.Load() {
					x.initialize(folder, open)
				}
			})
			return
		}
		x.initialize(folder, open)
	})
}

// initialize runs git init in the folder. A folder that is not the one of the window tab (askToOpen) then asks whether to open it; the
// folder of the window tab becomes a repository of the view.
func (x *gitscm) initialize(folder string, askToOpen bool) {
	branch := sanitizeBranchName(read(x.api, "git.defaultBranchName", "main"), read(x.api, "git.branchWhitespaceChar", "-"))
	runner := x.runner
	go func() {
		err := runner.Init(x.ctx, folder, git.InitOptions{DefaultBranch: branch})
		x.api.Post(func() {
			if x.stopped.Load() {
				return
			}
			if err != nil {
				x.reportFailure("init", folder, err, func() { x.initialize(folder, askToOpen) })
				return
			}
			x.madeRepository(folder)
			if !askToOpen {
				x.openAsRepository(folder)
				return
			}
			x.ask(extension.DialogSpec{
				Type: extension.DialogQuestion, Message: "Would you like to open the initialized repository?",
				Buttons: []extension.DialogButton{{Label: "Open"}, {Label: "Open in New Window"}, cancelButton},
			}, func(res extension.DialogResult) {
				switch {
				case x.stopped.Load():
				case !res.Cancelled && res.Button == 0:
					x.openFolder(folder, false)
				case !res.Cancelled && res.Button == 1:
					x.openFolder(folder, true)
				default:
					x.openAsRepository(folder)
				}
			})
		})
	}()
}

// openAsRepository opens the repository of a folder in the view without opening the folder as the folder of the window tab.
func (x *gitscm) openAsRepository(path string) {
	loc, ok := git.FindRepository(resolve(path))
	if !ok {
		x.api.Notifications().Error("There is no Git repository in " + path)
		return
	}
	x.model.openRepository(candidate{loc: loc, why: foundFolder}, true)
	x.model.askNext()
}

// cmdOpenRepository is git.openRepository: the folder in the argument (a path, or {"path": ...}), else a folder to choose; the repository
// that holds it opens in the view, and the folder is not opened as the folder of the window tab.
//
// Ported from VS Code: extensions/git/src/commands.ts (openRepository)
func (x *gitscm) cmdOpenRepository(_ context.Context, args any) error {
	path := ""
	switch v := args.(type) {
	case string:
		path = v
	case map[string]any:
		path, _ = v["path"].(string)
	}
	if path != "" {
		if !filepath.IsAbs(path) {
			x.api.Notifications().Error("The path of a repository is absolute: " + path)
			return nil
		}
		x.openAsRepository(path)
		return nil
	}
	x.chooseFolder(extension.FileChooserOptions{Title: "Open Repository", AcceptLabel: "Open Repository", Folder: x.home}, func(folder string) {
		if folder != "" {
			x.openAsRepository(folder)
		}
	})
	return nil
}

// cloneMenus are the Clone item of the "..." menus of the Source Control view and of a repository.
//
// Ported from VS Code: extensions/git/package.json (contributes.menus: scm/title, scm/repository)
func cloneMenus() []extension.MenuItem {
	var out []extension.MenuItem
	for _, menu := range []string{extension.MenuSCMTitle, extension.MenuSCMRepository} {
		out = append(out, extension.MenuItem{Menu: menu, Command: "git.clone", Group: "1_header", Order: 3, When: "scmProvider == git"})
	}
	return out
}
