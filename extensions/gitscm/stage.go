package gitscm

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// Stage and unstage (SPEC §10.30 "Stage and unstage"): the commands that move changes between the work tree and the index, with the
// question about files that still have conflict markers, and the three kinds of command (a selection of resources, a whole group, the
// file of an editor).
//
// Ported from VS Code: extensions/git/src/commands.ts (stage, stageAll, stageAllTracked, stageAllUntracked, stageAllMerge,
// _stageDeletionConflict, stageFile, unstage, unstageAll, unstageFile, categorizeResourceByResolution) and
// extensions/git/src/repository.ts (add, revert, rm)

// mutation is a command of the user that changes a repository: it runs on a goroutine as a write of the queue, the groups change at
// once (git.optimisticUpdate), and the repository is told to refresh when it ends (the watcher is quiet meanwhile, SPEC §10.30
// "Refresh"). A failure shows a notification and the refresh puts the real state back.
type mutation struct {
	// name says what the command does, for the notification of a failure ("stage").
	name string
	// optimistic changes the groups before the command runs; nil for none.
	optimistic func()
	// run is the command, on a goroutine.
	run func(ctx context.Context, repo *git.Repo) error
	// after runs on the UI thread when run succeeded.
	after func()
	// retry, when set, runs the command again: the notification of an authentication failure offers it as Retry.
	retry func()
	// failed, when set, sees the failure of run on the UI thread before the generic notification is made, and returns true when it
	// told the user itself.
	failed func(err error) bool
	// begin runs on the UI thread before run starts, and finally when it ended, whatever the result (the checkout shows its running
	// state between the two).
	begin, finally func()
}

// mutate runs a mutation on the repository.
func (r *repository) mutate(m mutation) {
	if r.disposed || r.repo == nil {
		return
	}
	x := r.m.x
	if m.optimistic != nil {
		m.optimistic()
	}
	if m.begin != nil {
		m.begin()
	}
	r.beginOwn()
	ctx, repo, gen := r.ctx, r.repo, r.gen
	go func() {
		err := m.run(ctx, repo)
		x.api.Post(func() {
			if r.disposed {
				return
			}
			if m.finally != nil {
				m.finally()
			}
			if err == nil && gen == r.gen && m.after != nil {
				m.after()
			}
			if err != nil && (m.failed == nil || !m.failed(err)) {
				x.commandFailed(m.name, r, err, m.retry)
			}
			r.endOwn()
		})
	}()
}

// commandFailed shows the failure of a command of the user: a notification with the message of git and a button for the Git output. A
// command that was cancelled (the repository was untrusted or closed, the extension ended) says nothing. A failure to authenticate (the
// user pressed Escape in the box that asked for a password, or the password was wrong) says so, with the remote when git named it, and
// offers Retry when the caller can run the command again (SPEC §10.30 "Passwords and editors"). The texts are cleared of the user
// information of URLs first.
//
// Ported from VS Code: extensions/git/src/commands.ts (createCommand: the catch of AuthenticationFailed).
func (x *gitscm) commandFailed(what string, r *repository, err error, retry func()) {
	x.reportFailure(what, r.root, err, retry)
}

// reportFailure is commandFailed for a command that has no repository (a clone): where is what the Git channel's line names.
func (x *gitscm) reportFailure(what, where string, err error, retry func()) {
	appErr, isApp := errors.FromError(err)
	if isApp && (appErr.Tag == "GitCancelled" || appErr.Tag == "GitQueueCancelled") {
		return
	}
	text := git.MaskURLs(describe(err))
	x.logLine("git " + what + " failed in " + where + ": " + text)
	showOutput := extension.NotificationAction{Label: "Show Git Output", Run: func() { x.channel.Show(false) }}
	if isApp && appErr.Tag == "AuthenticationFailed" {
		message := "Failed to authenticate to git remote."
		if remote := authRemote(appErr); remote != "" {
			message = "Failed to authenticate to git remote: " + remote
		}
		actions := []extension.NotificationAction{showOutput}
		if retry != nil {
			actions = append([]extension.NotificationAction{{Label: "Retry", Run: retry}}, actions...)
		}
		x.api.Notifications().Error(message, actions...)
		return
	}
	if isApp {
		switch appErr.Tag {
		case "PermissionDenied", "RemoteConnectionError", "CantAccessRemote", "RepositoryNotFound", "NoRemoteRepositorySpecified":
			// the first line git printed says why ("user@host: Permission denied (publickey)."), the last only that the remote could not be
			// read: VS Code shows the first line of the error output of a command it has no message for
			text = firstStderrLine(err, text)
		}
	}
	x.api.Notifications().Error("Git: "+text, showOutput)
}

// authRemote is the remote an authentication failure names: the URL in "Authentication failed for '...'" or in "could not read Password for
// '...'", without the user information, "" when git named none.
func authRemote(err *errors.Error) string {
	stderr, _ := err.Metadata["stderr"].(string)
	if m := reAuthRemote.FindStringSubmatch(stderr); m != nil {
		return reUserinfo.ReplaceAllString(m[1], "${1}")
	}
	return ""
}

// reUserinfo is the user information of a URL, which the notification leaves out (the user name is not a secret; a password never is in
// the text of a prompt, and one in a URL is not shown either).
var reUserinfo = regexp.MustCompile(`^([a-z][a-z0-9+.-]*://)[^/@]*@`)

// reAuthRemote finds the remote in the error output of a command that could not authenticate.
var reAuthRemote = regexp.MustCompile(`(?i)(?:Authentication failed for|could not read (?:Username|Password) for) '([^']*)'`)

// ask shows a modal question and calls done with the answer. A scope that cannot show a dialog (a host with no workbench) or an
// extension that ended answers nothing: no log line, nothing happens.
func (x *gitscm) ask(spec extension.DialogSpec, done func(extension.DialogResult)) {
	if err := x.api.Dialogs().Ask(spec, done); err != nil {
		if appErr, ok := errors.FromError(err); ok && (appErr.Tag == "ExtensionDialogsUnavailable" || appErr.Tag == "ExtensionDeactivated") {
			return
		}
		x.log.Warn("cannot ask a question", "message", spec.Message, "err", err)
	}
}

// confirm asks a question with a button that does it and a Cancel button, and calls yes when the first is pressed.
func (x *gitscm) confirm(spec extension.DialogSpec, yes func()) {
	x.ask(spec, func(res extension.DialogResult) {
		if !res.Cancelled && res.Button == 0 {
			yes()
		}
	})
}

// cancelButton is the Cancel button of the questions.
var cancelButton = extension.DialogButton{Label: "Cancel", Cancel: true}

// byRepository sorts targets by repository, keeping the order of the repositories and of the targets.
func byRepository(ts []target) (repos []*repository, groups map[*repository][]target) {
	groups = map[*repository][]target{}
	for _, t := range ts {
		if _, ok := groups[t.repo]; !ok {
			repos = append(repos, t.repo)
		}
		groups[t.repo] = append(groups[t.repo], t)
	}
	return repos, groups
}

// stagePathsOf are the paths git add must name for a resource of the work tree: the file, and for a rename that is not staged yet the
// old path too, so that the deletion and the addition are staged as a pair.
func stagePathsOf(res resource) []string {
	if res.status == git.StatusIntentToRename && res.from != "" {
		return []string{res.from, res.path}
	}
	return []string{res.path}
}

// unstagePathsOf are the paths git reset must name for a staged resource: for a rename the old path too, so that the rename is undone as a
// pair and the file is not left as an untracked one next to a staged deletion.
func unstagePathsOf(res resource) []string {
	if res.status == git.StatusIndexRenamed && res.from != "" {
		return []string{res.from, res.path}
	}
	return []string{res.path}
}

// pathsOf collects the paths of resources with one of the functions above.
func pathsOf(list []resource, of func(resource) []string) []string {
	var out []string
	for _, res := range list {
		out = append(out, of(res)...)
	}
	return out
}

// stagePaths stages the paths of a repository (git add -A, or -u for the files git tracks already).
func (r *repository) stagePaths(paths []string, update bool) {
	if len(paths) == 0 {
		return
	}
	r.mutate(mutation{
		name:       "add",
		optimistic: func() { r.optimisticStage(paths) },
		run: func(ctx context.Context, repo *git.Repo) error {
			return repo.Add(ctx, paths, git.AddOptions{Update: update})
		},
		after: func() { r.closeDiffEditors([]string{}, paths, false) },
	})
}

// unstagePaths unstages the paths of a repository; with none it unstages everything.
func (r *repository) unstagePaths(paths []string) {
	closing := paths
	if len(paths) == 0 {
		closing = pathsOf(r.res.index, unstagePathsOf)
	}
	r.mutate(mutation{
		name:       "reset",
		optimistic: func() { r.optimisticUnstage(append([]string(nil), closing...)) },
		run:        func(ctx context.Context, repo *git.Repo) error { return repo.Reset(ctx, paths) },
		after:      func() { r.closeDiffEditors(closing, []string{}, false) },
	})
}

// removePaths runs git rm on the paths (the answer "Delete File" to a conflict of a deletion).
func (r *repository) removePaths(paths []string) {
	r.mutate(mutation{
		name:       "rm",
		optimistic: func() { r.optimisticStage(paths) },
		run:        func(ctx context.Context, repo *git.Repo) error { return repo.Rm(ctx, paths, git.RmOptions{}) },
		after:      func() { r.closeDiffEditors([]string{}, paths, false) },
	})
}

// fileTargets are the resources of the file of an editor (a path or a URI of a file or of a version in git), or nothing.
func (x *gitscm) fileTargets(uri string) []target {
	if uri == "" {
		if doc, ok := x.api.Documents().Active(); ok {
			uri = doc.URI
		}
	}
	path, ok := x.fileOfURI(uri)
	if !ok {
		return nil
	}
	r := x.model.forPath(path)
	if r == nil {
		return nil
	}
	var out []target
	for _, g := range []string{groupWorking, groupUntracked, groupIndex, groupMerge} {
		if res, ok := r.findResource(g, path); ok {
			out = append(out, target{repo: r, group: g, res: res})
		}
	}
	return out
}

// selectionOrFile is the resources a menu gave a command, else those of the file of the editor or of the URI in the arguments.
func (x *gitscm) selectionOrFile(args any) []target {
	if ts := x.resourceTargets(args); len(ts) > 0 {
		return ts
	}
	return x.fileTargets(x.uriArg(args))
}

// inGroups keeps the targets that are in one of the groups.
func inGroups(ts []target, groups ...string) []target {
	var out []target
	for _, t := range ts {
		for _, g := range groups {
			if t.group == g {
				out = append(out, t)
				break
			}
		}
	}
	return out
}

// cmdStage is git.stage: the selected resources, or the file of the editor, go to the index. Files with conflict markers are asked
// about; a conflict of a deletion asks what to do with the file.
func (x *gitscm) cmdStage(_ context.Context, args any) error {
	ts := inGroups(x.selectionOrFile(args), groupWorking, groupUntracked, groupMerge)
	if len(ts) > 0 {
		x.stage(ts)
	}
	return nil
}

// stage stages targets of the work tree, the untracked files and the merge changes.
func (x *gitscm) stage(ts []target) {
	var plain, merge []target
	for _, t := range ts {
		if t.group == groupMerge {
			merge = append(merge, t)
		} else {
			plain = append(plain, t)
		}
	}
	x.stageMerge(merge, func(resolved []target, ok bool) {
		if !ok {
			return
		}
		all := append(plain, resolved...)
		repos, groups := byRepository(all)
		for _, r := range repos {
			var list []resource
			for _, t := range groups[r] {
				list = append(list, t.res)
			}
			r.stagePaths(pathsOf(list, stagePathsOf), false)
		}
	})
}

// stageMerge decides what happens to the merge changes among the targets: the files with conflict markers are asked about once, the
// conflicts of a deletion one by one. It calls done with the files to add, and false when the user cancelled.
func (x *gitscm) stageMerge(merge []target, done func(add []target, ok bool)) {
	if len(merge) == 0 {
		done(nil, true)
		return
	}
	// the repositories are busy while the files are read, so that nothing looks finished before the answer comes
	repos, _ := byRepository(merge)
	for _, r := range repos {
		r.beginOwn()
	}
	go func() {
		marked := map[string]bool{}
		for _, t := range merge {
			if t.res.status == git.StatusBothModified || t.res.status == git.StatusBothAdded {
				marked[t.res.path] = hasConflictMarkers(t.res.path)
			}
		}
		x.api.Post(func() {
			defer func() {
				for _, r := range repos {
					r.endOwn()
				}
			}()
			var unresolved, deletions, add []target
			for _, t := range merge {
				switch t.res.status {
				case git.StatusDeletedByThem, git.StatusDeletedByUs:
					deletions = append(deletions, t)
				default:
					if marked[t.res.path] || (t.res.status != git.StatusBothModified && t.res.status != git.StatusBothAdded) {
						unresolved = append(unresolved, t)
					}
					add = append(add, t)
				}
			}
			x.confirmUnresolved(unresolved, func() {
				x.resolveDeletions(deletions, func(ok bool) { done(add, ok) })
			}, func() { done(nil, false) })
		})
	}()
}

// confirmUnresolved asks whether to stage files that have merge conflicts; with none it goes on at once.
func (x *gitscm) confirmUnresolved(unresolved []target, yes, no func()) {
	if len(unresolved) == 0 {
		yes()
		return
	}
	message := "Are you sure you want to stage " + filepath.Base(unresolved[0].res.path) + " with merge conflicts?"
	if len(unresolved) > 1 {
		message = "Are you sure you want to stage " + strconv.Itoa(len(unresolved)) + " files with merge conflicts?"
	}
	x.ask(extension.DialogSpec{Type: extension.DialogWarning, Message: message, Buttons: []extension.DialogButton{{Label: "Yes"}, cancelButton}},
		func(res extension.DialogResult) {
			if res.Cancelled || res.Button != 0 {
				no()
				return
			}
			yes()
		})
}

// resolveDeletions asks, for each file that one side deleted and the other changed, whether to keep the changed file or delete it, and does
// it. It calls done with false when the user cancelled one of the questions (the files already answered are done).
func (x *gitscm) resolveDeletions(list []target, done func(ok bool)) {
	if len(list) == 0 {
		done(true)
		return
	}
	t, rest := list[0], list[1:]
	// "deleted by them" is the conflict where we changed the file and they deleted it
	keep, deleted, modified := "Keep Our Version", "them", "us"
	if t.res.status == git.StatusDeletedByUs {
		keep, deleted, modified = "Keep Their Version", "us", "them"
	}
	message := "File \"" + filepath.Base(t.res.path) + "\" was deleted by " + deleted + " and modified by " + modified + "."
	x.ask(extension.DialogSpec{
		Type: extension.DialogQuestion, Message: message, Detail: "What would you like to do?",
		Buttons: []extension.DialogButton{{Label: keep}, {Label: "Delete File"}, cancelButton},
	}, func(res extension.DialogResult) {
		switch {
		case res.Cancelled || res.Button == 2:
			done(false)
			return
		case res.Button == 0:
			t.repo.stagePaths([]string{t.res.path}, false)
		default:
			t.repo.removePaths([]string{t.res.path})
		}
		x.resolveDeletions(rest, done)
	})
}

// conflictMarker matches the lines that open, split and close a conflict.
var conflictMarker = regexp.MustCompile(`^<{7}\s|^={7}$|^>{7}\s`)

// hasConflictMarkers says whether a file has the markers of a conflict. A file that cannot be read has none.
//
// Ported from VS Code: extensions/git/src/util.ts (grep)
func hasConflictMarkers(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for sc.Scan() {
		if conflictMarker.Match(sc.Bytes()) {
			return true
		}
	}
	return false
}

// workAndUntracked are the resources of Changes and Untracked Changes.
func (r *repository) workAndUntracked() []resource {
	return append(append([]resource(nil), r.res.working...), r.res.untracked...)
}

// isUntracked says whether a resource is a file git does not know (or one it ignores).
func isUntracked(res resource) bool {
	return res.status == git.StatusUntracked || res.status == git.StatusIgnored
}

// cmdStageAll is git.stageAll: Changes, and with "mixed" the untracked files in it; with another git.untrackedChanges only the tracked
// files are staged ("git add -u").
func (x *gitscm) cmdStageAll(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		if x.cfg.untrackedChanges == "mixed" {
			r.stagePaths(pathsOf(r.workAndUntracked(), stagePathsOf), false)
			return
		}
		// the untracked files are not part of "all" here, and git add -u refuses a path it does not track
		r.stagePaths(pathsOf(filterResources(r.res.working, func(res resource) bool { return !isUntracked(res) }), stagePathsOf), true)
	})
	return nil
}

// cmdStageAllTracked is git.stageAllTracked: the files of Changes that git tracks.
func (x *gitscm) cmdStageAllTracked(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		var list []resource
		for _, res := range r.res.working {
			if !isUntracked(res) {
				list = append(list, res)
			}
		}
		r.stagePaths(pathsOf(list, stagePathsOf), false)
	})
	return nil
}

// cmdStageAllUntracked is git.stageAllUntracked: the untracked files, wherever they are shown.
func (x *gitscm) cmdStageAllUntracked(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		var list []resource
		for _, res := range r.workAndUntracked() {
			if isUntracked(res) {
				list = append(list, res)
			}
		}
		r.stagePaths(pathsOf(list, stagePathsOf), false)
	})
	return nil
}

// cmdStageAllMerge is git.stageAllMerge: every file of Merge Changes, after the questions about conflicts.
func (x *gitscm) cmdStageAllMerge(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		var ts []target
		for _, res := range r.res.merge {
			ts = append(ts, target{repo: r, group: groupMerge, res: res})
		}
		x.stage(ts)
	})
	return nil
}

// cmdStageFile is git.stageFile: the changes of the file of a URI (the title of a file in the multi-file diff editor).
func (x *gitscm) cmdStageFile(_ context.Context, args any) error {
	if ts := inGroups(x.fileTargets(x.uriArg(args)), groupWorking, groupUntracked); len(ts) > 0 {
		x.stage(ts)
	}
	return nil
}

// cmdUnstage is git.unstage: the staged resources of the selection, or of the file of the editor, go back to the work tree.
func (x *gitscm) cmdUnstage(_ context.Context, args any) error {
	x.unstage(inGroups(x.selectionOrFile(args), groupIndex))
	return nil
}

// cmdUnstageFile is git.unstageFile.
func (x *gitscm) cmdUnstageFile(_ context.Context, args any) error {
	x.unstage(inGroups(x.fileTargets(x.uriArg(args)), groupIndex))
	return nil
}

// unstage unstages staged resources.
func (x *gitscm) unstage(ts []target) {
	repos, groups := byRepository(ts)
	for _, r := range repos {
		var list []resource
		for _, t := range groups[r] {
			list = append(list, t.res)
		}
		r.unstagePaths(pathsOf(list, unstagePathsOf))
	}
}

// cmdUnstageAll is git.unstageAll: everything in the index goes back to HEAD.
func (x *gitscm) cmdUnstageAll(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { r.unstagePaths(nil) })
	return nil
}
