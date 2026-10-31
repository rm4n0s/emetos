package gitscm

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// The stash (SPEC §10.30 "Stash"): put the changes aside, bring them back, drop them, and look at what a stash holds in the
// multi-file diff editor. The commands are VS Code's, with its questions and messages; the stash is the one of "git stash".
//
// Ported from VS Code: extensions/git/src/commands.ts (_stash, stash, stashStaged, stashIncludeUntracked, stashPop, stashPopLatest,
// stashPopEditor, stashApply, stashApplyLatest, stashApplyEditor, stashDrop, stashDropAll, stashDropEditor, stashView, pickStash,
// getStashFromUri, _viewStash, StashItem) and extensions/git/src/repository.ts (createStash, popStash, applyStash, dropStash)

// StashMultiDiffKind is the kind of the sets of the multi-file diff editor that show a stash. It is also the resourceScheme of such an
// editor, as the scheme "git-stash" is of VS Code's: the editor-title actions Apply, Pop and Drop read it.
const StashMultiDiffKind = "git-stash"

// stashKey is the key of the set of a stash: the repository and the hash of the stash commit. The hash, not the index, names the stash:
// the index of a stash changes when another is made or dropped, and a tab must keep showing the stash it was opened for.
func stashKey(id, hash string) string { return id + "/" + hash }

// stashState is what the extension keeps for the stash editors.
type stashState struct {
	mu  sync.Mutex
	reg extension.MultiDiffRegistration
	// opened are the keys of the editors that were opened, by repository id, so that a repository that closes can say they are gone.
	opened map[string]map[string]bool
	// dropped are the keys of the stashes the user dropped from their own editor: the provider answers "gone" and the tab closes.
	dropped map[string]bool
	// known are the ids of the repositories that were opened, even while their first status is not there yet, and scanned says that
	// the first search for repositories ended: together they tell a repository that is still coming from one that is not there.
	known   map[string]bool
	scanned atomic.Bool
}

// know records that a repository with this id was opened.
func (s *stashState) know(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.known == nil {
		s.known = map[string]bool{}
	}
	s.known[id] = true
}

func (s *stashState) isKnown(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.known[id]
}

func (s *stashState) open(id, key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.opened == nil {
		s.opened = map[string]map[string]bool{}
	}
	if s.opened[id] == nil {
		s.opened[id] = map[string]bool{}
	}
	s.opened[id][key] = true
}

func (s *stashState) isDropped(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dropped[key]
}

// forget returns the keys opened for a repository and forgets them.
func (s *stashState) forget(id string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.known, id)
	var keys []string
	for k := range s.opened[id] {
		keys = append(keys, k)
	}
	delete(s.opened, id)
	return keys
}

// provideStashMultiDiff registers the provider of the stash editors, as provideMultiDiff does for the changes.
func (x *gitscm) provideStashMultiDiff() {
	reg, err := x.api.Documents().ProvideMultiDiff(extension.MultiDiffProvider{Kind: StashMultiDiffKind, Resources: x.stashResources})
	if err != nil {
		if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "ExtensionDocumentUnavailable" {
			x.log.Warn("the stash editor cannot be provided", "err", err)
		}
		return
	}
	x.stash.mu.Lock()
	x.stash.reg = reg
	x.stash.mu.Unlock()
}

// stashGone tells the window that the stash editors of a repository that was closed are gone.
func (x *gitscm) stashGone(id string) {
	keys := x.stash.forget(id)
	x.stash.mu.Lock()
	reg := x.stash.reg
	x.stash.mu.Unlock()
	if reg != nil && len(keys) > 0 {
		reg.Changed(keys...)
	}
}

// stashResources is the provider of the stash editors: the files of the stash the key names, on a goroutine of the window.
func (x *gitscm) stashResources(ctx context.Context, key string) ([]extension.MultiDiffResource, error) {
	id, hash, ok := strings.Cut(key, "/")
	if !ok || id == "" || hash == "" {
		return nil, errors.New("GitStashKeyInvalid", "this is not the key of a stash", "key", key)
	}
	if x.stash.isDropped(key) {
		return nil, errors.New("MultiDiffGone", "the stash was dropped", "key", key)
	}
	snap, repo, open := x.awaitRepository(ctx, id)
	if !open {
		return nil, errors.New("MultiDiffGone", "the repository of this stash is not open", "key", key)
	}
	ch, err := repo.StashChangesAt(ctx, hash)
	if err != nil {
		return nil, err
	}
	return stashMultiDiff(id, snap.root, ch), nil
}

// awaitRepository gives the repository with the id and its snapshot. A saved stash editor asks for its files when the extension starts,
// before the repositories are found and have their first status, so the answer waits (off the UI thread, until ctx ends) for a
// repository that is on its way; it is false for one that is not there once the first search ended, and when ctx or the extension ends.
func (x *gitscm) awaitRepository(ctx context.Context, id string) (repoSnapshot, *git.Repo, bool) {
	for {
		snap, open := x.multi.get(id)
		if repo := x.content.repos.get(id); open && repo != nil {
			return snap, repo, true
		}
		if (x.stash.scanned.Load() && !x.stash.isKnown(id)) || ctx.Err() != nil || x.stopped.Load() {
			return repoSnapshot{}, nil, false
		}
		select {
		case <-ctx.Done():
		case <-time.After(20 * time.Millisecond):
		}
	}
}

// stashMultiDiff are the files of a stash: each tracked file against the same path (or the old path of a rename) in the commit the
// stash was made on, each untracked file as an added file from the commit that holds the untracked files.
//
// Ported from VS Code: extensions/git/src/commands.ts (_viewStash, toMultiFileDiffEditorUris)
func stashMultiDiff(id, root string, ch git.StashChanges) []extension.MultiDiffResource {
	out := make([]extension.MultiDiffResource, 0, len(ch.Changes))
	for _, c := range ch.Changes {
		abs := filepath.Join(root, filepath.FromSlash(c.Path))
		from := abs
		if c.OrigPath != "" {
			from = filepath.Join(root, filepath.FromSlash(c.OrigPath))
		}
		r := extension.MultiDiffResource{Path: c.Path, Context: map[string]any{"scmProvider": "git", "scmResourceGroup": "stash"}}
		switch {
		case c.Untracked:
			r.Modified = toGitURI(id, root, abs, ch.Untracked)
			r.File = fileURI(abs)
			r.Letter, r.Color, r.Tooltip = statusLetter(git.StatusUntracked), statusColor(git.StatusUntracked), statusText(git.StatusUntracked)
		default:
			if c.Status != 'A' {
				r.Original = toGitURI(id, root, from, ch.Base)
			}
			if c.Status != 'D' {
				r.Modified = toGitURI(id, root, abs, ch.Stash.Hash)
				r.File = fileURI(abs)
			}
			r.Letter, r.Color, r.Tooltip = commitStatus(c.Status)
		}
		out = append(out, r)
	}
	return out
}

// stashItemLabel is the label of a stash in the pickers and the title of its editor: "#0: WIP on main".
func stashItemLabel(s git.Stash) string { return "#" + strconv.Itoa(s.Index) + ": " + s.Description }

// stashItemDescription is what follows the label of a stash in a picker: how long ago it was made and the branch, "2 hrs • main".
//
// Ported from VS Code: extensions/git/src/util.ts (getStashDescription)
func stashItemDescription(s git.Stash, now time.Time) string {
	var parts []string
	if !s.CommitDate.IsZero() {
		parts = append(parts, fromNowShort(s.CommitDate, now))
	}
	if s.Branch != "" {
		parts = append(parts, s.Branch)
	}
	return strings.Join(parts, " • ")
}

// fromNowShort is the time between a date and now in VS Code's short words, without "ago": "now", "5 mins", "2 hrs", "3 days", "1 wk",
// "2 mos", "1 yr".
//
// Ported from VS Code: extensions/git/src/util.ts (fromNow)
func fromNowShort(date, now time.Time) string {
	const (
		minute = 60
		hour   = minute * 60
		day    = hour * 24
		week   = day * 7
		month  = day * 30
		year   = day * 365
	)
	seconds := int(now.Sub(date).Round(time.Second) / time.Second)
	unit := func(n int, one, many string) string {
		if n == 1 {
			return "1 " + one
		}
		return strconv.Itoa(n) + " " + many
	}
	switch {
	case seconds < -30:
		return "in " + fromNowShort(now.Add(now.Sub(date)), now)
	case seconds < 30:
		return "now"
	case seconds < minute:
		return unit(seconds, "sec", "secs")
	case seconds < hour:
		return unit(seconds/minute, "min", "mins")
	case seconds < day:
		return unit(seconds/hour, "hr", "hrs")
	case seconds < week:
		return unit(seconds/day, "day", "days")
	case seconds < month:
		return unit(seconds/week, "wk", "wks")
	case seconds < year:
		return unit(seconds/month, "mo", "mos")
	}
	return unit(seconds/year, "yr", "yrs")
}

// stashMessageArg is the message a caller gave the stash commands ({"message": "..."}, as the dev commands and the tests do): when it is
// there the input box is not shown.
func stashMessageArg(args any) (string, bool) {
	if m, ok := args.(map[string]any); ok {
		if s, ok := m["message"].(string); ok {
			return s, true
		}
	}
	return "", false
}

// stashIndexArg is the index of the stash a caller named ({"index": 1}), so that no picker is shown.
func stashIndexArg(args any) (int, bool) {
	m, ok := args.(map[string]any)
	if !ok {
		return 0, false
	}
	switch v := m["index"].(type) {
	case float64:
		return int(v), v >= 0 && v == float64(int(v))
	case int:
		return v, v >= 0
	}
	return 0, false
}

// cmdStashCommand makes the handler of git.stash, git.stashIncludeUntracked and git.stashStaged.
func (x *gitscm) cmdStashCommand(includeUntracked, staged bool) extension.CommandHandler {
	return func(_ context.Context, args any) error {
		x.target(args, true, func(r *repository) { x.stashChanges(r, includeUntracked, staged, args) })
		return nil
	}
}

// stashChanges is VS Code's _stash: the checks of what there is to stash, the question about unsaved files, the message, and the stash.
func (x *gitscm) stashChanges(r *repository, includeUntracked, staged bool, args any) {
	if r.repo == nil {
		return
	}
	if staged && !x.install.Version.AtLeast(2, 35) {
		x.api.Notifications().Error("Needs git 2.35 or newer")
		return
	}
	noUnstaged := len(r.res.working) == 0 && (!includeUntracked || len(r.res.untracked) == 0)
	noStaged := len(r.res.index) == 0
	switch {
	case staged && noStaged:
		x.api.Notifications().Info("There are no staged changes to stash.")
		return
	case !staged && noUnstaged && noStaged:
		x.api.Notifications().Info("There are no changes to stash.")
		return
	}
	unsaved := x.unsavedBeforeStash(r)
	if len(unsaved) == 0 {
		x.askStashMessage(r, includeUntracked, staged, args)
		return
	}
	message := "There are " + strconv.Itoa(len(unsaved)) + " unsaved files.\n\nWould you like to save them before stashing?"
	if len(unsaved) == 1 {
		name := baseName(unsaved[0].path)
		message = "The following file has unsaved changes which won't be included in the stash if you proceed: " + name +
			".\n\nWould you like to save it before stashing?"
	}
	x.ask(extension.DialogSpec{
		Type: extension.DialogWarning, Message: message,
		Buttons: []extension.DialogButton{{Label: "Save All & Stash"}, {Label: "Stash Anyway"}, cancelButton},
	}, func(res extension.DialogResult) {
		switch {
		case res.Cancelled || r.disposed:
		case res.Button == 0:
			uris := make([]string, len(unsaved))
			for i, u := range unsaved {
				uris[i] = u.uri
			}
			go func() {
				err := x.api.Documents().SaveAll(x.ctx, uris)
				x.api.Post(func() {
					if x.stopped.Load() || r.disposed {
						return
					}
					if err != nil {
						// a stash that leaves out the unsaved work is not what the user chose: stop and say why
						x.log.Warn("cannot save the files before the stash", "err", err)
						x.api.Notifications().Error("Git: the files could not be saved, so nothing was stashed: " + describe(err))
						return
					}
					x.askStashMessage(r, includeUntracked, staged, args)
				})
			}()
		case res.Button == 1:
			x.askStashMessage(r, includeUntracked, staged, args)
		}
	})
}

// unsavedFile is a file with unsaved changes.
type unsavedFile struct{ uri, path string }

// unsavedBeforeStash are the files of the repository with unsaved changes that git.promptToSaveFilesBeforeStash asks about: with
// "staged", or while something is staged, only those that are staged; with "never", none.
func (x *gitscm) unsavedBeforeStash(r *repository) []unsavedFile {
	mode := read(x.api, "git.promptToSaveFilesBeforeStash", "always")
	if mode == "never" {
		return nil
	}
	var out []unsavedFile
	for _, uri := range x.api.Documents().Dirty() {
		p, ok := uriPath(uri)
		if !ok || !isUnder(r.root, p) {
			continue
		}
		out = append(out, unsavedFile{uri: uri, path: p})
	}
	if mode == "staged" || len(r.res.index) > 0 {
		kept := out[:0]
		for _, u := range out {
			for _, s := range r.res.index {
				if s.path == u.path {
					kept = append(kept, u)
					break
				}
			}
		}
		out = kept
	}
	return out
}

// isUnder tells whether a path is the folder or below it.
func isUnder(folder, path string) bool {
	rel, err := filepath.Rel(folder, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// askStashMessage asks for the message of the stash (an input box, with the text of the commit box when
// git.useCommitInputAsStashMessage is on) and makes the stash. A caller that gave {"message": ...} is not asked.
func (x *gitscm) askStashMessage(r *repository, includeUntracked, staged bool, args any) {
	if r.disposed {
		return
	}
	if msg, ok := stashMessageArg(args); ok {
		x.createStash(r, includeUntracked, staged, msg)
		return
	}
	value := ""
	if read(x.api, "git.useCommitInputAsStashMessage", false) {
		// TODO(step 24.10): VS Code leaves the commit box alone while it holds the commit template; the template is read there.
		value = r.src.Input().Value()
	}
	err := x.api.InputBox().Show(extension.InputBoxOptions{Prompt: "Optionally provide a stash message", Value: value}, func(msg string, ok bool) {
		if ok {
			x.createStash(r, includeUntracked, staged, msg)
		}
	})
	if err != nil {
		x.logUnavailable("cannot ask for the stash message", err)
	}
}

// logUnavailable logs the failure of a question, except for a scope that cannot show it.
func (x *gitscm) logUnavailable(what string, err error) {
	if appErr, ok := errors.FromError(err); ok && (appErr.Tag == "ExtensionInputBoxUnavailable" || appErr.Tag == "ExtensionQuickPickUnavailable" || appErr.Tag == "ExtensionDeactivated") {
		return
	}
	x.log.Warn(what, "err", err)
}

// createStash runs git stash push and closes the diff editors of the files that were stashed.
//
// Ported from VS Code: extensions/git/src/repository.ts (createStash)
func (x *gitscm) createStash(r *repository, includeUntracked, staged bool, message string) {
	closeIndex := pathsOf(r.res.index, func(res resource) []string { return []string{res.path} })
	closeWork := []string{}
	if !staged {
		closeWork = append(closeWork, pathsOf(r.res.working, func(res resource) []string { return []string{res.path} })...)
	}
	if includeUntracked {
		closeWork = append(closeWork, pathsOf(r.res.untracked, func(res resource) []string { return []string{res.path} })...)
	}
	r.mutate(mutation{
		name: "stash",
		run: func(ctx context.Context, repo *git.Repo) error {
			if err := stashNeedsACommit(ctx, repo); err != nil {
				return err
			}
			return repo.Stash(ctx, git.StashOptions{Message: message, IncludeUntracked: includeUntracked, Staged: staged})
		},
		after: func() { r.closeDiffEditors(closeIndex, closeWork, false) },
		failed: func(err error) bool {
			if appErr, ok := errors.FromError(err); ok && appErr.Tag == "StashNoInitialCommit" {
				x.api.Notifications().Info("The repository does not have any commits. Please make an initial commit before creating a stash.")
				return true
			}
			return false
		},
	})
}

// stashNeedsACommit fails with the tag StashNoInitialCommit in a repository before its first commit, where git cannot make a stash (VS Code
// finds this out from the error output of git; asking first gives the same answer without reading text).
func stashNeedsACommit(ctx context.Context, repo *git.Repo) error {
	has, err := repo.HasCommit(ctx)
	if err != nil {
		return err
	}
	if !has {
		return errors.New("StashNoInitialCommit", "a stash needs a commit to be made on", "root", repo.Root)
	}
	return nil
}

// stashFailed is the failure handler of the commands that apply a stash: conflicts are a warning that points to the merge changes, as
// VS Code's, and everything else is the generic failure.
func (x *gitscm) stashFailed(err error) bool {
	if appErr, ok := errors.FromError(err); ok && appErr.Tag == "StashConflict" {
		// VS Code's button "Show Changes" opens the Source Control view; an extension cannot run that command, but the view shows the
		// merge changes by itself
		x.api.Notifications().Warn("There are merge conflicts while applying the stash. Please resolve them before committing your changes.")
		return true
	}
	return false
}

// pickStash lists the stashes of the repository in a picker and calls fn with the one chosen. Nothing is chosen from a repository that
// has none: the picker says so.
func (x *gitscm) pickStash(r *repository, placeholder string, fn func(git.Stash)) {
	repo, ctx := r.repo, r.ctx
	if repo == nil {
		return
	}
	go func() {
		stashes, err := repo.Stashes(ctx)
		x.api.Post(func() {
			if x.stopped.Load() || r.disposed || ctx.Err() != nil {
				return
			}
			if err != nil {
				x.commandFailed("stash list", r, err, nil)
				return
			}
			items := make([]extension.QuickPickItem, 0, len(stashes))
			for _, s := range stashes {
				items = append(items, extension.QuickPickItem{Label: stashItemLabel(s), Description: stashItemDescription(s, time.Now())})
			}
			if len(items) == 0 {
				items = append(items, extension.QuickPickItem{Label: "$(info) This repository has no stashes."})
			}
			err := x.api.QuickPick().Show(extension.QuickPickOptions{Placeholder: placeholder, Items: items}, func(i int) {
				if i >= 0 && i < len(stashes) && !r.disposed {
					fn(stashes[i])
				}
			})
			if err != nil {
				x.logUnavailable("cannot show the stashes", err)
			}
		})
	}()
}

// withStash calls fn with the stash a command is for: the one its arguments name ({"index": n}), else the one picked.
func (x *gitscm) withStash(r *repository, args any, placeholder string, fn func(git.Stash)) {
	index, ok := stashIndexArg(args)
	if !ok {
		x.pickStash(r, placeholder, fn)
		return
	}
	repo, ctx := r.repo, r.ctx
	if repo == nil {
		return
	}
	go func() {
		stashes, err := repo.Stashes(ctx)
		x.api.Post(func() {
			if x.stopped.Load() || r.disposed || ctx.Err() != nil {
				return
			}
			if err != nil {
				x.commandFailed("stash list", r, err, nil)
				return
			}
			for _, s := range stashes {
				if s.Index == index {
					fn(s)
					return
				}
			}
			x.api.Notifications().Info("There is no stash #" + strconv.Itoa(index) + " in the repository.")
		})
	}()
}

// withStashCount runs fn with the number of stashes, or tells the user that there are none.
func (x *gitscm) withStashCount(r *repository, fn func(count int)) {
	repo, ctx := r.repo, r.ctx
	if repo == nil {
		return
	}
	go func() {
		stashes, err := repo.Stashes(ctx)
		x.api.Post(func() {
			if x.stopped.Load() || r.disposed || ctx.Err() != nil {
				return
			}
			switch {
			case err != nil:
				x.commandFailed("stash list", r, err, nil)
			case len(stashes) == 0:
				x.api.Notifications().Info("There are no stashes in the repository.")
			default:
				fn(len(stashes))
			}
		})
	}()
}

// applyStash runs git stash pop or apply on the stash with the index (git.StashLatest for the newest).
func (x *gitscm) applyStash(r *repository, index int, pop bool) {
	name := "stash apply"
	if pop {
		name = "stash pop"
	}
	r.mutate(mutation{
		name: name,
		run: func(ctx context.Context, repo *git.Repo) error {
			if pop {
				return repo.StashPop(ctx, index, git.StashApplyOptions{})
			}
			return repo.StashApply(ctx, index, git.StashApplyOptions{})
		},
		failed: x.stashFailed,
	})
}

func (x *gitscm) cmdStashPop(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		x.withStash(r, args, "Pick a stash to pop", func(s git.Stash) { x.applyStash(r, s.Index, true) })
	})
	return nil
}

func (x *gitscm) cmdStashApply(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		x.withStash(r, args, "Pick a stash to apply", func(s git.Stash) { x.applyStash(r, s.Index, false) })
	})
	return nil
}

func (x *gitscm) cmdStashPopLatest(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { x.withStashCount(r, func(int) { x.applyStash(r, git.StashLatest, true) }) })
	return nil
}

func (x *gitscm) cmdStashApplyLatest(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) { x.withStashCount(r, func(int) { x.applyStash(r, git.StashLatest, false) }) })
	return nil
}

// confirmDropStash asks "Are you sure you want to drop the stash: ...?" and drops it after Yes; done is told whether it was dropped.
func (x *gitscm) confirmDropStash(r *repository, s git.Stash, done func(dropped bool)) {
	x.ask(extension.DialogSpec{
		Type: extension.DialogWarning, Message: "Are you sure you want to drop the stash: " + s.Description + "?",
		Buttons: []extension.DialogButton{{Label: "Yes"}, cancelButton},
	}, func(res extension.DialogResult) {
		if res.Cancelled || res.Button != 0 || r.disposed {
			return
		}
		r.mutate(mutation{
			name: "stash drop",
			run:  func(ctx context.Context, repo *git.Repo) error { return repo.StashDrop(ctx, s.Index) },
			after: func() {
				if done != nil {
					done(true)
				}
			},
		})
	})
}

func (x *gitscm) cmdStashDrop(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		x.withStash(r, args, "Pick a stash to drop", func(s git.Stash) { x.confirmDropStash(r, s, nil) })
	})
	return nil
}

// cmdStashDropAll asks "Are you sure you want to drop ALL stashes?" and clears the stash.
func (x *gitscm) cmdStashDropAll(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		x.withStashCount(r, func(count int) {
			question := "Are you sure you want to drop ALL stashes? There is 1 stash that will be subject to pruning, and MAY BE IMPOSSIBLE TO RECOVER."
			if count != 1 {
				question = "Are you sure you want to drop ALL stashes? There are " + strconv.Itoa(count) +
					" stashes that will be subject to pruning, and MAY BE IMPOSSIBLE TO RECOVER."
			}
			x.confirm(extension.DialogSpec{
				Type: extension.DialogWarning, Message: question, Buttons: []extension.DialogButton{{Label: "Yes"}, cancelButton},
			}, func() {
				r.mutate(mutation{name: "stash drop all", run: func(ctx context.Context, repo *git.Repo) error { return repo.StashClear(ctx) }})
			})
		})
	})
	return nil
}

// cmdStashView picks a stash and opens its changes in the multi-file diff editor.
func (x *gitscm) cmdStashView(_ context.Context, args any) error {
	x.target(args, true, func(r *repository) {
		x.withStash(r, args, "Pick a stash to view", func(s git.Stash) { x.viewStash(r, s) })
	})
	return nil
}

// viewStash reads the changes of a stash in the background and opens the editor; a stash that changed nothing opens nothing.
//
// Ported from VS Code: extensions/git/src/commands.ts (_viewStash)
func (x *gitscm) viewStash(r *repository, s git.Stash) {
	repo, ctx := r.repo, r.ctx
	if repo == nil {
		return
	}
	go func() {
		ch, err := repo.StashChangesOf(ctx, s)
		x.api.Post(func() {
			if x.stopped.Load() || r.disposed {
				return
			}
			if err != nil {
				if ctx.Err() == nil {
					x.log.Warn("cannot read the stash", "stash", s.Index, "err", err)
					x.api.Notifications().Error("Cannot open the stash: " + describe(err))
				}
				return
			}
			if len(ch.Changes) == 0 {
				x.api.Notifications().Info("The stash has no changes.")
				return
			}
			spec := extension.MultiDiffSpec{Title: "Git Stash " + stashItemLabel(s), Kind: StashMultiDiffKind, Key: stashKey(r.id, s.Hash)}
			x.stash.open(r.id, spec.Key)
			if err := x.api.Documents().OpenMultiDiff(spec); err != nil {
				x.logUnavailable("cannot open a stash", err)
				if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "ExtensionDocumentUnavailable" {
					x.api.Notifications().Error("Cannot open the stash: " + describe(err))
				}
			}
		})
	}()
}

// stashOfEditor finds the repository and the stash of the editor a title action was run from. The editor names its stash by hash, so
// the stash is found in the list now; false (and a message) when the repository is closed or the stash is not in the list any more.
//
// Ported from VS Code: extensions/git/src/commands.ts (getStashFromUri)
func (x *gitscm) stashOfEditor(args any, fn func(r *repository, s git.Stash, key string)) {
	m, _ := args.(map[string]any)
	key, _ := m["key"].(string)
	id, hash, ok := strings.Cut(key, "/")
	if kind, _ := m["kind"].(string); !ok || kind != StashMultiDiffKind {
		return
	}
	var r *repository
	for _, c := range x.model.order {
		if c.id == id && c.repo != nil {
			r = c
			break
		}
	}
	if r == nil {
		x.api.Notifications().Info("The repository of this stash is not open.")
		return
	}
	repo, ctx := r.repo, r.ctx
	go func() {
		stashes, err := repo.Stashes(ctx)
		x.api.Post(func() {
			if x.stopped.Load() || r.disposed || ctx.Err() != nil {
				return
			}
			if err != nil {
				x.commandFailed("stash list", r, err, nil)
				return
			}
			for _, s := range stashes {
				if s.Hash == hash {
					fn(r, s, key)
					return
				}
			}
			x.api.Notifications().Info("This stash is not in the stash list any more.")
		})
	}()
}

func (x *gitscm) cmdStashPopEditor(_ context.Context, args any) error {
	x.stashOfEditor(args, func(r *repository, s git.Stash, _ string) { x.applyStash(r, s.Index, true) })
	return nil
}

func (x *gitscm) cmdStashApplyEditor(_ context.Context, args any) error {
	x.stashOfEditor(args, func(r *repository, s git.Stash, _ string) { x.applyStash(r, s.Index, false) })
	return nil
}

// cmdStashDropEditor drops the stash of the editor after the question, and closes the editor.
func (x *gitscm) cmdStashDropEditor(_ context.Context, args any) error {
	x.stashOfEditor(args, func(r *repository, s git.Stash, key string) {
		x.confirmDropStash(r, s, func(bool) {
			x.stash.mu.Lock()
			if x.stash.dropped == nil {
				x.stash.dropped = map[string]bool{}
			}
			x.stash.dropped[key] = true
			reg := x.stash.reg
			x.stash.mu.Unlock()
			if reg != nil {
				reg.Changed(key)
			}
		})
	})
	return nil
}
