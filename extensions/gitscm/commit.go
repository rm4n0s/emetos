package gitscm

import (
	"context"
	"regexp"
	"strconv"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// Commit (SPEC §10.30 "Staging, discarding and committing"): the commands that make a commit from the box of the Source Control view, with
// the questions that come first (unsaved files, smart commit, an empty commit, no-verify, the diagnostics of the staged files, a protected
// branch), the commit itself as a write of the queue, and what follows it (the box takes the template, the post-commit command runs).
//
// Ported from VS Code: extensions/git/src/commands.ts (smartCommit, commitWithAnyInput, the git.commit* commands, _commitEmpty,
// undoCommit, restoreCommitTemplate) and extensions/git/src/repository.ts (commit, commitOperationCleanup,
// commitOperationGetOptimisticResourceGroups)

// postCommitSpec is the command to run after a commit, as the second argument of git.commit gives it.
type postCommitSpec struct {
	// set says that a command was named (by the action button): the choice is remembered. Without it the command of the setting, or the
	// remembered one, runs (VS Code's undefined).
	set bool
	// command is "git.push" or "git.sync"; empty is none (VS Code's null).
	command string
}

// commitOpts are the options of one commit command (VS Code's CommitOptions).
type commitOpts struct {
	// all stages every change first (smart commit, git.commitAll); tracked stages only the files git tracks ("git add -u").
	all, tracked bool
	// amend replaces the last commit; signoff adds a Signed-off-by trailer; empty allows a commit with no changes; noVerify skips the hooks.
	amend, signoff, empty, noVerify bool
	// post is the command to run after the commit.
	post postCommitSpec
}

// commitVariants are the commands of the commit, with the options each one stands for. A commit with no word of staged, all or empty
// takes what the box and the settings say.
var commitVariants = map[string]commitOpts{
	"git.commit":                     {},
	"git.commitAmend":                {amend: true},
	"git.commitSigned":               {signoff: true},
	"git.commitStaged":               {},
	"git.commitStagedSigned":         {signoff: true},
	"git.commitStagedAmend":          {amend: true},
	"git.commitAll":                  {all: true},
	"git.commitAllSigned":            {all: true, signoff: true},
	"git.commitAllAmend":             {all: true, amend: true},
	"git.commitNoVerify":             {noVerify: true},
	"git.commitStagedNoVerify":       {noVerify: true},
	"git.commitStagedSignedNoVerify": {signoff: true, noVerify: true},
	"git.commitAmendNoVerify":        {amend: true, noVerify: true},
	"git.commitSignedNoVerify":       {signoff: true, noVerify: true},
	"git.commitStagedAmendNoVerify":  {amend: true, noVerify: true},
	"git.commitAllNoVerify":          {all: true, noVerify: true},
	"git.commitAllSignedNoVerify":    {all: true, signoff: true, noVerify: true},
	"git.commitAllAmendNoVerify":     {all: true, amend: true, noVerify: true},
}

// commitHandlers are the handlers of the commit commands.
func (x *gitscm) commitHandlers() map[string]extension.CommandHandler {
	out := map[string]extension.CommandHandler{}
	for id, o := range commitVariants {
		out[id] = x.cmdCommit(id, o)
	}
	out["git.commitEmpty"] = x.cmdCommitEmpty(false)
	out["git.commitEmptyNoVerify"] = x.cmdCommitEmpty(true)
	out["git.undoCommit"] = x.cmdUndoCommit
	out["git.restoreCommitTemplate"] = x.cmdRestoreCommitTemplate
	return out
}

// cmdCommit makes the handler of a commit command. Its arguments are the source control (the menus, the action button and Ctrl+Enter
// in the box give it) and, from the action button, the command to run afterwards: nothing, or null for none, or "git.push" or "git.sync".
func (x *gitscm) cmdCommit(id string, o commitOpts) extension.CommandHandler {
	return func(_ context.Context, args any) error {
		o := o
		if id == "git.commit" {
			o.post = postCommitArg(args)
		}
		x.target(args, true, func(r *repository) { x.startCommit(r, o) })
		return nil
	}
}

// postCommitArg reads the second argument of git.commit: the command the action button asks for after the commit.
func postCommitArg(args any) postCommitSpec {
	list, ok := args.([]any)
	if !ok || len(list) < 2 {
		return postCommitSpec{}
	}
	switch v := list[1].(type) {
	case string:
		if v == "git.push" || v == "git.sync" {
			return postCommitSpec{set: true, command: v}
		}
	case nil:
		return postCommitSpec{set: true}
	}
	return postCommitSpec{}
}

// cmdCommitEmpty makes the handler of git.commitEmpty: it asks first, unless git.confirmEmptyCommits was turned off.
//
// Ported from VS Code: extensions/git/src/commands.ts (_commitEmpty)
func (x *gitscm) cmdCommitEmpty(noVerify bool) extension.CommandHandler {
	return func(_ context.Context, args any) error {
		x.target(args, true, func(r *repository) {
			start := func() { x.startCommit(r, commitOpts{empty: true, noVerify: noVerify}) }
			if !x.cfg.confirmEmptyCommits {
				start()
				return
			}
			x.ask(extension.DialogSpec{
				Type: extension.DialogWarning, Message: "Are you sure you want to create an empty commit?",
				Buttons: []extension.DialogButton{{Label: "Yes"}, {Label: "Yes, Don't Show Again"}, cancelButton},
			}, func(res extension.DialogResult) {
				switch {
				case res.Cancelled || res.Button == 2:
				case res.Button == 1:
					x.saveSetting("git.confirmEmptyCommits", false)
					start()
				default:
					start()
				}
			})
		})
		return nil
	}
}

// commitRun is one run of a commit command through its questions.
type commitRun struct {
	x *gitscm
	r *repository
	o commitOpts

	// input is the text of the box when the command started; message is what goes to git.
	input, message string
	// noStaged and noUnstaged say that the index has no change and that Changes has none.
	noStaged, noUnstaged bool
	// smart says that smart commit is on (the setting, or the answer to its question).
	smart bool
	// sign is the -S decision, signoff and the editor flags follow the settings.
	sign      *bool
	useEditor bool
	verbose   bool
}

// startCommit begins a commit command in a repository. A commit that is already running holds the new one back until it has ended and
// the status shows what it did.
func (x *gitscm) startCommit(r *repository, o commitOpts) {
	if r.disposed || r.repo == nil || x.stopped.Load() {
		return
	}
	if r.committing {
		r.commitQueue = append(r.commitQueue, func() { x.startCommit(r, o) })
		return
	}
	c := &commitRun{x: x, r: r, o: o, input: r.src.Input().Value(), noStaged: len(r.res.index) == 0, noUnstaged: len(r.res.working) == 0}
	c.steps(c.promptToSave, c.smartCommit, c.flags, c.emptyCommit, c.noVerify, c.getMessage, c.messageRequired, c.trackedOnly,
		c.diagnostics, c.branchProtection)
}

// steps runs the steps in order, each one calling next to go on and not calling it to stop; the last step is followed by the commit.
func (c *commitRun) steps(steps ...func(next func())) {
	at := 0
	var next func()
	next = func() {
		if c.r.disposed || c.r.repo == nil || c.x.stopped.Load() {
			return
		}
		if at == len(steps) {
			c.commit()
			return
		}
		step := steps[at]
		at++
		step(next)
	}
	next()
}

// promptToSave asks about the documents with unsaved changes in the repository, before the commit leaves their changes out.
//
// Ported from VS Code: extensions/git/src/commands.ts (smartCommit: promptToSaveFilesBeforeCommit)
func (c *commitRun) promptToSave(next func()) {
	x, r := c.x, c.r
	mode := x.cfg.promptToSaveFilesBeforeCommit
	if c.o.empty || mode == "never" {
		next()
		return
	}
	unsaved := x.unsavedIn(r, mode == "staged" || len(r.res.index) > 0)
	if len(unsaved) == 0 {
		next()
		return
	}
	spec := extension.DialogSpec{
		Type:    extension.DialogWarning,
		Message: "There are " + strconv.Itoa(len(unsaved)) + " unsaved files.",
		Detail:  "Would you like to save them before committing?",
		Buttons: []extension.DialogButton{{Label: "Save All & Commit Changes"}, {Label: "Commit Changes"}, cancelButton},
	}
	if len(unsaved) == 1 {
		spec.Message = "The following file has unsaved changes which won't be included in the commit if you proceed: " + baseName(unsaved[0].path) + "."
		spec.Detail = "Would you like to save it before committing?"
	}
	x.ask(spec, func(res extension.DialogResult) {
		switch {
		case res.Cancelled || res.Button == 2 || r.disposed:
		case res.Button == 1:
			next()
		default:
			c.saveThenRefresh(unsaved, next)
		}
	})
}

// unsavedDoc is a document with unsaved changes that is in a repository.
type unsavedDoc struct {
	uri, path string
}

// unsavedIn lists the documents with unsaved changes that are files of the repository; with onlyStaged just those that are in the index.
func (x *gitscm) unsavedIn(r *repository, onlyStaged bool) []unsavedDoc {
	var out []unsavedDoc
	staged := map[string]bool{}
	for _, res := range r.res.index {
		staged[res.path] = true
	}
	for _, uri := range x.api.Documents().Dirty() {
		path, ok := uriPath(uri)
		if !ok || !isUnder(r.root, path) {
			continue
		}
		if onlyStaged && !staged[path] {
			continue
		}
		out = append(out, unsavedDoc{uri: uri, path: path})
	}
	return out
}

// saveThenRefresh saves the documents, stages again those that were in the index (the saved text is not in it yet), and goes on when
// the status shows the result.
//
// Ported from VS Code: extensions/git/src/commands.ts (smartCommit: the documents that were saved and are in the index group are added)
func (c *commitRun) saveThenRefresh(unsaved []unsavedDoc, next func()) {
	x, r := c.x, c.r
	uris := make([]string, len(unsaved))
	for i, u := range unsaved {
		uris[i] = u.uri
	}
	staged := map[string]bool{}
	for _, res := range r.res.index {
		staged[res.path] = true
	}
	var again []string
	for _, u := range unsaved {
		if staged[u.path] {
			again = append(again, u.path)
		}
	}
	repo, ctx := r.repo, r.ctx
	r.beginOwn()
	go func() {
		err := x.api.Documents().SaveAll(ctx, uris)
		if err == nil && len(again) > 0 {
			err = repo.Add(ctx, again, git.AddOptions{})
		}
		x.api.Post(func() {
			if r.disposed {
				return
			}
			if err != nil {
				r.endOwn()
				x.commandFailed("commit", r, err, nil)
				return
			}
			r.endOwn()
			r.whenFresh(func() {
				c.noStaged, c.noUnstaged = len(r.res.index) == 0, len(r.res.working) == 0
				next()
			})
		})
	}()
}

// smartCommit decides what to do about a commit with nothing staged: ask whether to commit every change, or do it when smart commit is
// on.
//
// Ported from VS Code: extensions/git/src/commands.ts (smartCommit)
func (c *commitRun) smartCommit(next func()) {
	x := c.x
	c.smart = x.cfg.enableSmartCommit
	if c.o.empty {
		next()
		return
	}
	finish := func() {
		if c.smart && !c.o.all {
			c.o.all = c.noStaged
		}
		next()
	}
	if c.noUnstaged || !c.noStaged || c.smart || c.o.all || c.o.amend {
		finish()
		return
	}
	if !x.cfg.suggestSmartCommit {
		return
	}
	x.ask(extension.DialogSpec{
		Type: extension.DialogWarning, Message: "There are no staged changes to commit.",
		Detail:  "Would you like to stage all your changes and commit them directly?",
		Buttons: []extension.DialogButton{{Label: "Yes"}, {Label: "Always"}, {Label: "Never"}, cancelButton},
	}, func(res extension.DialogResult) {
		switch {
		case res.Cancelled || res.Button == 3:
		case res.Button == 1:
			c.smart = true
			x.saveSetting("git.enableSmartCommit", true)
			finish()
		case res.Button == 2:
			x.saveSetting("git.suggestSmartCommit", false)
		default:
			c.smart = true
			finish()
		}
	})
}

// flags reads the settings that decide how git commits: signing, sign-off, the editor and the verbose diff.
func (c *commitRun) flags(next func()) {
	cfg := &c.x.cfg
	if cfg.enableCommitSigning {
		t := true
		c.sign = &t
	}
	if cfg.alwaysSignOff {
		c.o.signoff = true
	}
	if cfg.useEditorAsCommitInput {
		c.useEditor = true
		c.verbose = cfg.verboseCommit
	}
	next()
}

// emptyCommit offers an empty commit when there is nothing to commit and the command is not one that has a reason to go on (an amend, a
// merge or a rebase in progress).
func (c *commitRun) emptyCommit(next func()) {
	x, r := c.x, c.r
	onlyUntracked := true
	for _, res := range r.res.working {
		if res.status != git.StatusUntracked {
			onlyUntracked = false
			break
		}
	}
	nothing := (c.noStaged && c.noUnstaged) || (!c.o.all && c.noStaged) ||
		(c.noStaged && x.cfg.smartCommitChanges == "tracked" && onlyUntracked)
	if !nothing || c.o.amend || c.o.empty || r.ops.merge || r.ops.rebaseCommit != "" {
		next()
		return
	}
	x.api.Notifications().Info("There are no changes to commit.", extension.NotificationAction{Label: "Create Empty Commit", Run: func() {
		c.o.empty = true
		next()
	}})
}

// noVerify checks that a commit without the hooks is allowed and asked for.
func (c *commitRun) noVerify(next func()) {
	x := c.x
	if !c.o.noVerify {
		next()
		return
	}
	if !x.cfg.allowNoVerifyCommit {
		x.api.Notifications().Error(`Commits without verification are not allowed, please enable them with the "git.allowNoVerifyCommit" setting.`)
		return
	}
	if !x.cfg.confirmNoVerifyCommit {
		next()
		return
	}
	x.ask(extension.DialogSpec{
		Type: extension.DialogWarning, Message: "You are about to commit your changes without verification, this skips pre-commit hooks and can be undesirable.",
		Detail:  "Are you sure to continue?",
		Buttons: []extension.DialogButton{{Label: "OK"}, {Label: "OK, Don't Ask Again"}, cancelButton},
	}, func(res extension.DialogResult) {
		switch {
		case res.Cancelled || res.Button == 2:
		case res.Button == 1:
			x.saveSetting("git.confirmNoVerifyCommit", false)
			next()
		default:
			next()
		}
	})
}

// getMessage takes the message of the box; with none and no editor it asks for one in an input box, except for an amend, which keeps
// the message it has.
//
// Ported from VS Code: extensions/git/src/commands.ts (commitWithAnyInput)
func (c *commitRun) getMessage(next func()) {
	x, r := c.x, c.r
	c.message = c.input
	if c.message != "" || x.cfg.useEditorAsCommitInput {
		next()
		return
	}
	if c.o.amend && r.head.commit != "" {
		next()
		return
	}
	placeholder := "Commit message"
	if name := r.headShortName(); name != "" {
		placeholder = `Message (commit on "` + name + `")`
	}
	err := x.api.InputBox().Show(extension.InputBoxOptions{Prompt: placeholder, Message: "Please provide a commit message", IgnoreFocusOut: true},
		func(value string, ok bool) {
			if !ok {
				return
			}
			c.message = value
			next()
		})
	if err != nil {
		x.logUnavailable("cannot ask for the commit message", err)
	}
}

// messageRequired stops a commit that has no message when nothing else can give it one.
func (c *commitRun) messageRequired(next func()) {
	if c.message == "" && !c.o.amend && !c.useEditor {
		return
	}
	next()
}

// trackedOnly narrows "stage everything" to the files git tracks when smart commit or git.untrackedChanges says so.
func (c *commitRun) trackedOnly(next func()) {
	cfg := &c.x.cfg
	if c.o.all && cfg.smartCommitChanges == "tracked" {
		c.o.tracked = true
	}
	if c.o.all && cfg.untrackedChanges != "mixed" {
		c.o.tracked = true
	}
	next()
}

// commit makes the commit as a write of the queue.
func (c *commitRun) commit() {
	x, r := c.x, c.r
	if r.committing {
		// another command got here first: this one is started again once that one ended, with the state it left
		o := c.o
		r.commitQueue = append(r.commitQueue, func() { x.startCommit(r, o) })
		return
	}
	if c.o.post.set {
		r.postStored = c.o.post
		r.updateCommitUI()
	}
	r.setCommitting(true)
	opts := git.CommitOptions{
		Message: c.message, UseEditor: c.useEditor, Verbose: c.verbose, Amend: c.o.amend, Signoff: c.o.signoff, Sign: c.sign,
		Empty: c.o.empty, NoVerify: c.o.noVerify, RequireUserConfig: x.cfg.requireGitUserConfig,
	}
	indexPaths := pathsOf(r.res.index, func(res resource) []string { return []string{res.path} })
	if indexPaths == nil {
		indexPaths = []string{}
	}
	workPaths := []string{}
	if c.o.all && !c.o.tracked {
		workPaths = pathsOf(r.res.working, func(res resource) []string { return []string{res.path} })
	}
	all, tracked, rebase, home := c.o.all, c.o.tracked, r.ops.rebaseCommit != "", x.home
	var template string
	r.mutate(mutation{
		name:       "commit",
		optimistic: func() { r.optimisticCommit(all, tracked) },
		run: func(ctx context.Context, repo *git.Repo) error {
			if all {
				if err := repo.Add(ctx, nil, git.AddOptions{Update: tracked}); err != nil {
					return err
				}
			}
			if rebase {
				if err := repo.RebaseContinue(ctx); err != nil {
					return err
				}
			} else if err := repo.Commit(ctx, opts); err != nil {
				return err
			}
			template = repo.InputTemplate(ctx, home)
			return nil
		},
		after: func() {
			if c.message != "" {
				r.template = template
				r.src.Input().SetValue(template)
			}
			r.closeDiffEditors(indexPaths, workPaths, false)
			x.runPostCommit(r, c.o.post)
			r.endCommit()
		},
		failed: func(err error) bool {
			r.endCommit()
			return c.failed(err)
		},
	})
}

// reEditorProblem is what git says when its editor failed (older versions capitalize it).
var reEditorProblem = regexp.MustCompile(`(?i)there was a problem with the editor`)

// failed tells the user why the commit failed when VS Code has a word for it; false leaves the generic notification.
//
// Ported from VS Code: extensions/git/src/commands.ts (createCommand: the catch for NoUserNameConfigured, NoUserEmailConfigured and
// EmptyCommitMessage)
func (c *commitRun) failed(err error) bool {
	x, r := c.x, c.r
	appErr, ok := errors.FromError(err)
	if !ok {
		return false
	}
	switch appErr.Tag {
	case "NoUserNameConfigured", "NoUserEmailConfigured":
		x.logLine("git commit failed in " + r.root + ": " + git.MaskURLs(describe(err)))
		x.identityMissing(r, func() { x.startCommit(r, c.o) })
		return true
	case "EmptyCommitMessage":
		x.logLine("git commit failed in " + r.root + ": " + git.MaskURLs(describe(err)))
		x.api.Notifications().Info("Commit operation was cancelled due to empty commit message.")
		return true
	}
	// the tab of the message was closed without saving: the editor answered with failure, and git stopped (SPEC §10.30 "The editor")
	if stderr, _ := appErr.Metadata["stderr"].(string); reEditorProblem.MatchString(stderr) {
		x.logLine("git commit failed in " + r.root + ": " + git.MaskURLs(describe(err)))
		x.api.Notifications().Info("Commit operation was cancelled.")
		return true
	}
	return false
}

// optimisticCommit changes the groups at once when a commit starts: the index is empty afterwards, Changes too when everything is staged
// first (only the untracked files stay when just the tracked ones are), and the merge changes are resolved.
//
// Ported from VS Code: extensions/git/src/repository.ts (commitOperationGetOptimisticResourceGroups)
func (r *repository) optimisticCommit(all, tracked bool) {
	if !r.m.x.cfg.optimisticUpdate || r.groups.merge == nil {
		return
	}
	g := r.res
	g.index, g.merge = nil, nil
	switch {
	case tracked:
		g.working = filterResources(r.res.working, isUntrackedResource)
	case all:
		g.working, g.untracked = nil, nil
	}
	r.setGroups(g)
	r.updateStatusBar()
}

// isUntrackedResource says that a resource is a file git does not track yet (an ignored file is not one: it is not in Changes).
func isUntrackedResource(res resource) bool { return res.status == git.StatusUntracked }

// runPostCommit runs the command that follows a commit: the one the action button named, else the remembered one, else the setting.
//
// TODO(step 24.15): git.push and git.sync are real commands then; VS Code waits for them to end before the commit counts as finished.
//
// Ported from VS Code: extensions/git/src/postCommitCommands.ts (executePostCommitCommand)
func (x *gitscm) runPostCommit(r *repository, post postCommitSpec) {
	command := post.command
	if !post.set {
		command = r.primaryPostCommit().spec.command
	}
	if command != "" {
		if h := x.handlers[command]; h != nil {
			if err := h(x.ctx, []any{map[string]any{extension.SCMArgSourceControl: r.handle}}); err != nil {
				x.commandFailed("post-commit "+command, r, err, nil)
			}
		}
	}
	if !x.cfg.rememberPostCommitCommand {
		r.postStored = postCommitSpec{}
		r.updateCommitUI()
	}
}

// cmdUndoCommit is git.undoCommit: the last commit goes (HEAD moves to its parent and its changes stay staged) and its message comes
// back in the box.
//
// Ported from VS Code: extensions/git/src/commands.ts (undoCommit)
func (x *gitscm) cmdUndoCommit(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { x.undoCommit(r) })
	return nil
}

func (x *gitscm) undoCommit(r *repository) {
	if r.head.commit == "" {
		x.api.Notifications().Warn("Can't undo because HEAD doesn't point to any commit.")
		return
	}
	repo, ctx := r.repo, r.ctx
	r.beginOwn()
	go func() {
		c, err := repo.LastCommit(ctx)
		x.api.Post(func() {
			defer r.endOwn()
			if r.disposed {
				return
			}
			if err != nil {
				x.commandFailed("undo commit", r, err, nil)
				return
			}
			if len(c.Parents) > 1 {
				x.ask(extension.DialogSpec{
					Type: extension.DialogWarning, Message: "The last commit was a merge commit. Are you sure you want to undo it?",
					Buttons: []extension.DialogButton{{Label: "Undo merge commit"}, cancelButton},
				}, func(res extension.DialogResult) {
					if !res.Cancelled && res.Button == 0 {
						r.resetToParent(c)
					}
				})
				return
			}
			r.resetToParent(c)
		})
	}()
}

// resetToParent moves HEAD to the parent of the commit (or removes the branch of a first commit and unstages its files) and puts the message
// of the commit in the box.
func (r *repository) resetToParent(c git.Commit) {
	hasParent := len(c.Parents) > 0
	r.mutate(mutation{
		name: "undo commit",
		run: func(ctx context.Context, repo *git.Repo) error {
			if hasParent {
				return repo.ResetSoft(ctx, "HEAD~")
			}
			if err := repo.DeleteRef(ctx, "HEAD"); err != nil {
				return err
			}
			return repo.Reset(ctx, nil)
		},
		after: func() { r.src.Input().SetValue(c.Message) },
	})
}

// cmdRestoreCommitTemplate is git.restoreCommitTemplate: the box gets the commit template again.
func (x *gitscm) cmdRestoreCommitTemplate(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		repo, ctx, home := r.repo, r.ctx, x.home
		go func() {
			template := repo.CommitTemplate(ctx, home)
			x.api.Post(func() {
				if !r.disposed {
					r.template = template
					r.src.Input().SetValue(template)
				}
			})
		}()
	})
	return nil
}
