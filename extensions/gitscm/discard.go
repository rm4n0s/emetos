package gitscm

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// Discard (SPEC §10.30 "Discard"): throw the changes of the work tree away. A changed file goes back to its version in the index, a deleted
// one comes back, an untracked one is moved to the trash (git.discardUntrackedChangesToTrash) or deleted, and a submodule is put on the
// commit the index names. Every discard asks first; a clean open document follows the change on disk as any change from outside does
// (SPEC §6.3 "External changes").
//
// Ported from VS Code: extensions/git/src/commands.ts (clean, cleanAll, cleanAllTracked, cleanAllUntracked, _cleanAll,
// _cleanTrackedChanges, _cleanUntrackedChanges, getDiscardUntrackedChangesDialogDetails) and extensions/git/src/repository.ts (clean, _clean)

// namesShown is how many file names a question about many files lists.
const namesShown = 5

// irreversible is the warning under a question whose answer loses changes for good.
const irreversible = "This action is irreversible!"

// discardChoice is a button of the question that is not Cancel, and the resources it discards.
type discardChoice struct {
	label string
	list  []resource
}

// discardQuestion is the question about discarding resources and what each button does.
type discardQuestion struct {
	spec    extension.DialogSpec
	choices []discardChoice
}

// listNames shows the first names of files: "a.go, b.go and 3 more".
func listNames(list []resource) string {
	var names []string
	for i, res := range list {
		if i == namesShown {
			break
		}
		names = append(names, filepath.Base(res.path))
	}
	out := strings.Join(names, ", ")
	if more := len(list) - len(names); more > 0 {
		out += " and " + strconv.Itoa(more) + " more"
	}
	return out
}

// trackedQuestion asks about changed or deleted files that git tracks.
func trackedQuestion(list []resource) discardQuestion {
	allDeleted := true
	for _, res := range list {
		if res.status != git.StatusDeleted {
			allDeleted = false
		}
	}
	n := len(list)
	name := filepath.Base(list[0].path)
	var msg, detail, yes string
	switch {
	case allDeleted && n == 1:
		msg, yes = "Are you sure you want to restore '"+name+"'?", "Restore File"
	case allDeleted:
		msg, detail, yes = "Are you sure you want to restore ALL "+strconv.Itoa(n)+" files?", listNames(list), "Restore All "+strconv.Itoa(n)+" Files"
	case n == 1:
		msg, detail, yes = "Are you sure you want to discard changes in '"+name+"'?", irreversible, "Discard Changes"
	default:
		msg = "Are you sure you want to discard ALL changes in " + strconv.Itoa(n) + " files?"
		detail = listNames(list) + "\n\n" + irreversible + "\nYour current working set will be FOREVER LOST if you proceed."
		yes = "Discard All " + strconv.Itoa(n) + " Files"
	}
	return discardQuestion{
		spec:    extension.DialogSpec{Type: extension.DialogWarning, Message: msg, Detail: detail, Buttons: []extension.DialogButton{{Label: yes}, cancelButton}},
		choices: []discardChoice{{yes, list}},
	}
}

// untrackedTexts are the message, the detail and the button of the question about deleting untracked files.
func untrackedTexts(list []resource, toTrash bool) (msg, detail, yes string) {
	n := len(list)
	if n == 1 {
		msg = "Are you sure you want to DELETE the following untracked file: '" + filepath.Base(list[0].path) + "'?"
	} else {
		msg = "Are you sure you want to DELETE the " + strconv.Itoa(n) + " untracked files?"
		detail = listNames(list) + "\n\n"
	}
	switch {
	case toTrash && n == 1:
		detail += "You can restore this file from the Trash."
		yes = "Move to Trash"
	case toTrash:
		detail += "You can restore these files from the Trash."
		yes = "Move to Trash"
	case n == 1:
		detail += irreversible + "\nThis file will be FOREVER LOST if you proceed."
		yes = "Delete File"
	default:
		detail += irreversible + "\nThese files will be FOREVER LOST if you proceed."
		yes = "Delete All " + strconv.Itoa(n) + " Files"
	}
	return msg, detail, yes
}

// untrackedQuestion asks about untracked files.
func untrackedQuestion(list []resource, toTrash bool) discardQuestion {
	msg, detail, yes := untrackedTexts(list, toTrash)
	return discardQuestion{
		spec:    extension.DialogSpec{Type: extension.DialogWarning, Message: msg, Detail: detail, Buttons: []extension.DialogButton{{Label: yes}, cancelButton}},
		choices: []discardChoice{{yes, list}},
	}
}

// mixedQuestion asks about untracked and tracked files together, with a button for the tracked ones only and a button for all.
func mixedQuestion(tracked, untracked []resource, toTrash bool) discardQuestion {
	msg, detail, _ := untrackedTexts(untracked, toTrash)
	var trackedMsg, yesTracked string
	if len(tracked) == 1 {
		trackedMsg = "Are you sure you want to discard changes in '" + filepath.Base(tracked[0].path) + "'?"
		yesTracked = "Discard 1 Tracked File"
	} else {
		trackedMsg = "Are you sure you want to discard ALL changes in " + strconv.Itoa(len(tracked)) + " files?"
		yesTracked = "Discard All " + strconv.Itoa(len(tracked)) + " Tracked Files"
	}
	yesAll := "Discard All " + strconv.Itoa(len(tracked)+len(untracked)) + " Files"
	all := append(append([]resource(nil), tracked...), untracked...)
	return discardQuestion{
		spec: extension.DialogSpec{
			Type: extension.DialogWarning, Message: msg,
			Detail:  detail + "\n\n" + trackedMsg + "\n\n" + irreversible + "\nYour current working set will be FOREVER LOST if you proceed.",
			Buttons: []extension.DialogButton{{Label: yesTracked}, {Label: yesAll}, cancelButton},
		},
		choices: []discardChoice{{yesTracked, tracked}, {yesAll, all}},
	}
}

// DiscardDialogs are the questions of the discard commands for files with these names, for a test of the window that looks at them: one
// changed file, many, files that were deleted, untracked files (moved to the trash or deleted for good) and both kinds together.
func DiscardDialogs(names []string, kind string, toTrash bool) extension.DialogSpec {
	list := make([]resource, len(names))
	for i, n := range names {
		list[i] = resource{path: "/work/" + n, status: git.StatusModified}
		switch kind {
		case "deleted":
			list[i].status = git.StatusDeleted
		case "untracked", "mixed":
			list[i].status = git.StatusUntracked
		}
	}
	switch kind {
	case "untracked":
		return untrackedQuestion(list, toTrash).spec
	case "mixed":
		return mixedQuestion([]resource{{path: "/work/tracked.go", status: git.StatusModified}}, list, toTrash).spec
	}
	return trackedQuestion(list).spec
}

// isNew says whether discarding a resource deletes the file: an untracked or ignored file, or one that is only an intent to add. git
// checkout would turn the last into an empty file, so it is deleted like the others and forgotten by the index.
func isNew(res resource) bool { return isUntracked(res) || res.status == git.StatusIntentToAdd }

// discardable keeps the resources that can be discarded: those of Changes and Untracked Changes. The same file twice is once.
func discardable(ts []target) []target {
	seen := map[string]bool{}
	var out []target
	for _, t := range inGroups(ts, groupWorking, groupUntracked) {
		key := t.repo.root + "\x00" + t.res.path
		if !seen[key] {
			seen[key] = true
			out = append(out, t)
		}
	}
	return out
}

// cmdClean is git.clean: the selected resources, or the file of the editor, are discarded after a question.
func (x *gitscm) cmdClean(_ context.Context, args any) error {
	x.discard(discardable(x.selectionOrFile(args)))
	return nil
}

// cmdCleanAll is git.cleanAll: all of Changes (the untracked files are in it with "mixed").
func (x *gitscm) cmdCleanAll(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { x.discard(targetsOf(r, groupWorking, r.res.working)) })
	return nil
}

// cmdCleanAllTracked is git.cleanAllTracked: the files of Changes that git tracks.
func (x *gitscm) cmdCleanAllTracked(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		x.discard(targetsOf(r, groupWorking, filterResources(r.res.working, func(res resource) bool { return !isNew(res) })))
	})
	return nil
}

// cmdCleanAllUntracked is git.cleanAllUntracked: the untracked files, wherever they are shown.
func (x *gitscm) cmdCleanAllUntracked(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		var ts []target
		ts = append(ts, targetsOf(r, groupWorking, filterResources(r.res.working, isNew))...)
		ts = append(ts, targetsOf(r, groupUntracked, filterResources(r.res.untracked, isNew))...)
		x.discard(ts)
	})
	return nil
}

// filterResources keeps the resources a function accepts.
func filterResources(list []resource, keep func(resource) bool) []resource {
	var out []resource
	for _, res := range list {
		if keep(res) {
			out = append(out, res)
		}
	}
	return out
}

// targetsOf makes targets of the resources of a group of a repository.
func targetsOf(r *repository, group string, list []resource) []target {
	out := make([]target, len(list))
	for i, res := range list {
		out[i] = target{repo: r, group: group, res: res}
	}
	return out
}

// discard asks about discarding the targets and does what the answer says, one repository after the other.
func (x *gitscm) discard(ts []target) {
	repos, groups := byRepository(ts)
	for _, r := range repos {
		var tracked, untracked []resource
		for _, t := range groups[r] {
			if isNew(t.res) {
				untracked = append(untracked, t.res)
			} else {
				tracked = append(tracked, t.res)
			}
		}
		x.askDiscard(r, tracked, untracked)
	}
}

// askDiscard shows the question for the resources of a repository and discards what the answer says.
func (x *gitscm) askDiscard(r *repository, tracked, untracked []resource) {
	if len(tracked)+len(untracked) == 0 {
		return
	}
	var q discardQuestion
	switch {
	case len(untracked) == 0:
		q = trackedQuestion(tracked)
	case len(tracked) == 0:
		q = untrackedQuestion(untracked, x.cfg.discardToTrash)
	default:
		q = mixedQuestion(tracked, untracked, x.cfg.discardToTrash)
	}
	x.ask(q.spec, func(res extension.DialogResult) {
		if res.Cancelled || res.Button < 0 || res.Button >= len(q.choices) {
			return
		}
		var t, u []resource
		for _, c := range q.choices[res.Button].list {
			if isNew(c) {
				u = append(u, c)
			} else {
				t = append(t, c)
			}
		}
		r.discardNow(t, u)
	})
}

// discardOutcome is what a discard did off the UI thread.
type discardOutcome struct {
	// trashed are the files moved to the trash and undo takes them back.
	trashed []resource
	undo    func(ctx context.Context) error
	// trashFailed is the error of a trash that could not take the untracked files; they are still there.
	trashFailed error
	err         error
}

// discardNow discards without asking: the untracked files go to the trash or are deleted, the tracked ones are put back from the index.
func (r *repository) discardNow(tracked, untracked []resource) {
	if r.disposed || r.repo == nil {
		return
	}
	x := r.m.x
	all := append(append([]resource(nil), tracked...), untracked...)
	paths := pathsOf(all, func(res resource) []string { return []string{res.path} })
	r.optimisticClean(paths)
	r.beginOwn()
	ctx, repo, gen := r.ctx, r.repo, r.gen
	toTrash := x.cfg.discardToTrash
	submodules := map[string]bool{}
	for _, p := range r.submodules {
		submodules[filepath.Join(r.root, filepath.FromSlash(p))] = true
	}
	go func() {
		var out discardOutcome
		if len(untracked) > 0 {
			removeNow := true
			if toTrash {
				uris := make([]string, len(untracked))
				for i, res := range untracked {
					uris[i] = fileURI(res.path)
				}
				undo, err := x.api.Workspace().TrashFiles(ctx, uris)
				switch {
				case err == nil:
					out.trashed, out.undo, removeNow = untracked, undo, false
					out.err = forgetIntents(ctx, repo, untracked)
				case errTag(err) == "ExtensionTrashUnavailable":
					// the scope has no trash: delete, as the setting off would
				default:
					out.trashFailed, removeNow = err, false
				}
			}
			if removeNow {
				out.err = removeNew(ctx, repo, untracked)
			}
		}
		if out.err == nil && len(tracked) > 0 {
			out.err = checkoutTracked(ctx, repo, tracked, submodules)
		}
		x.api.Post(func() { r.discarded(out, tracked, untracked, gen) })
	}()
}

// removeNew deletes the new files for good: git clean for the untracked ones, git rm -f for those that are an intent to add, which also
// forgets them.
func removeNew(ctx context.Context, repo *git.Repo, list []resource) error {
	var plain, intents []string
	for _, res := range list {
		if res.status == git.StatusIntentToAdd {
			intents = append(intents, res.path)
		} else {
			plain = append(plain, res.path)
		}
	}
	if len(plain) > 0 {
		if err := repo.Clean(ctx, plain); err != nil {
			return err
		}
	}
	if len(intents) > 0 {
		return repo.Rm(ctx, intents, git.RmOptions{Force: true})
	}
	return nil
}

// forgetIntents takes the files that were an intent to add out of the index once the trash has taken them.
func forgetIntents(ctx context.Context, repo *git.Repo, list []resource) error {
	var intents []string
	for _, res := range list {
		if res.status == git.StatusIntentToAdd {
			intents = append(intents, res.path)
		}
	}
	if len(intents) == 0 {
		return nil
	}
	return repo.Rm(ctx, intents, git.RmOptions{Cached: true, Force: true})
}

// checkoutTracked puts the index version of the files back; a submodule is put on the commit the index names. A repository before its
// first commit says it is on a branch yet to be born, which is no failure here.
func checkoutTracked(ctx context.Context, repo *git.Repo, tracked []resource, submodules map[string]bool) error {
	var files, subs []string
	for _, res := range tracked {
		if submodules[res.path] {
			subs = append(subs, res.path)
		} else {
			files = append(files, res.path)
		}
	}
	if len(files) > 0 {
		if err := repo.Checkout(ctx, files); err != nil && errTag(err) != "BranchNotYetBorn" {
			return err
		}
	}
	if len(subs) > 0 {
		return repo.UpdateSubmodules(ctx, subs)
	}
	return nil
}

// discarded is the end of a discard, on the UI thread.
func (r *repository) discarded(out discardOutcome, tracked, untracked []resource, gen int) {
	if r.disposed {
		return
	}
	x := r.m.x
	defer r.endOwn()
	if gen == r.gen {
		all := pathsOf(append(append([]resource(nil), tracked...), untracked...), func(res resource) []string { return []string{res.path} })
		r.closeDiffEditors([]string{}, all, false)
	}
	if out.err != nil {
		x.commandFailed("clean", r, out.err)
	}
	if out.undo != nil {
		x.notifyTrashed(r, out.trashed, out.undo)
	}
	if out.trashFailed != nil {
		x.askDeleteForGood(r, untracked, out.trashFailed)
	}
}

// notifyTrashed tells that files are in the trash and offers to bring them back.
func (x *gitscm) notifyTrashed(r *repository, trashed []resource, undo func(ctx context.Context) error) {
	what := filepath.Base(trashed[0].path)
	if len(trashed) > 1 {
		what = strconv.Itoa(len(trashed)) + " files"
	}
	x.api.Notifications().Info("Moved to Trash: "+what, extension.NotificationAction{Label: "Undo", Run: func() {
		go func() {
			err := undo(x.ctx)
			x.api.Post(func() {
				if err != nil {
					x.api.Notifications().Error("Cannot restore " + what + " from the Trash: " + describe(err))
				}
				r.refresh()
			})
		}()
	}})
}

// askDeleteForGood asks whether to delete files for good when the trash could not take them.
//
// Ported from VS Code: extensions/git/src/repository.ts (_clean)
func (x *gitscm) askDeleteForGood(r *repository, list []resource, cause error) {
	x.logLine("cannot move files to the trash: " + describe(cause))
	yes := "Delete File"
	if len(list) > 1 {
		yes = "Delete All " + strconv.Itoa(len(list)) + " Files"
	}
	x.confirm(extension.DialogSpec{
		Type: extension.DialogWarning, Message: "Failed to delete using the Trash. Do you want to permanently delete instead?",
		Detail: irreversible, Buttons: []extension.DialogButton{{Label: yes}, cancelButton},
	}, func() {
		r.mutate(mutation{
			name: "clean",
			run:  func(ctx context.Context, repo *git.Repo) error { return removeNew(ctx, repo, list) },
		})
	})
}
