package gitscm

import (
	"path/filepath"
	"strings"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// watchSpecs are the folders a repository watches (SPEC §10.30 "Refresh"): its work tree, and its git directory and common
// directory when they are not inside it (a submodule, a linked worktree, a bare repository). The objects folder is never watched.
func (r *repository) watchSpecs(excludes []string) []extension.WatchSpec {
	root := r.root
	specs := []extension.WatchSpec{}
	if !r.loc.Bare {
		specs = append(specs, extension.WatchSpec{Path: root, Recursive: true, Excludes: append([]string{".git/objects"}, excludes...)})
	}
	seen := map[string]bool{}
	for _, dir := range []string{r.loc.GitDir, r.commonDir()} {
		if dir == "" || seen[dir] || (!r.loc.Bare && git.Within(dir, root)) {
			continue
		}
		seen[dir] = true
		specs = append(specs, extension.WatchSpec{Path: dir, Recursive: true, Excludes: []string{"objects"}})
	}
	return specs
}

// commonDir is the git directory that holds the refs and the configuration.
func (r *repository) commonDir() string {
	if r.repo != nil && r.repo.Info.CommonDir != "" {
		return r.repo.Info.CommonDir
	}
	return r.loc.GitDir
}

// watcherExcludes are the folders files.watcherExclude says not to watch.
func (x *gitscm) watcherExcludes() []string {
	m, err := extension.SettingValue[map[string]bool](x.api.Settings(), "files.watcherExclude")
	if err != nil {
		return nil
	}
	var out []string
	for p, on := range m {
		if on && !strings.Contains(p, ".git/objects") {
			out = append(out, p)
		}
	}
	return out
}

// startWatching starts the watches of the repository, on a goroutine because placing them reads the folders. It does nothing with
// git.autorefresh off.
func (r *repository) startWatching() {
	x := r.m.x
	if !x.cfg.autorefresh || r.disposed {
		return
	}
	r.stopWatching()
	specs := r.watchSpecs(x.watcherExcludes())
	for _, spec := range specs {
		go func() {
			cancel, err := x.api.Workspace().Watch(r.ctx, spec, r.onChanges)
			x.api.Post(func() {
				switch {
				case err != nil:
					x.log.Warn("cannot watch a folder of a repository", "path", spec.Path, "err", err)
					x.logLine("cannot watch " + spec.Path + ": " + err.Error())
				case r.disposed:
					cancel()
				default:
					r.watchMu.Lock()
					r.watchStops = append(r.watchStops, cancel)
					r.watchMu.Unlock()
				}
			})
		}()
	}
}

// stopWatching stops the watches and the pending refresh.
func (r *repository) stopWatching() {
	r.watchMu.Lock()
	stops := r.watchStops
	r.watchStops = nil
	r.watchMu.Unlock()
	for _, stop := range stops {
		stop()
	}
	if r.debounce != nil {
		r.debounce()
		r.debounce = nil
	}
}

// onChanges gets the changes of the watches, on the UI thread.
func (r *repository) onChanges(changes []extension.FileChange) {
	x := r.m.x
	if r.disposed || !x.cfg.autorefresh || r.own > 0 {
		return
	}
	for _, c := range changes {
		if r.relevant(c) {
			r.scheduleRefresh()
			return
		}
	}
}

// scheduleRefresh asks for a status in DebounceDelay: the changes that come before then are part of the same one, so a build that
// rewrites thousands of files makes one status per delay at most (SPEC §10.30 "Performance").
func (r *repository) scheduleRefresh() {
	if r.disposed || r.debounce != nil {
		return
	}
	x := r.m.x
	r.debounce = x.clock.AfterFunc(DebounceDelay, func() {
		x.api.Post(func() {
			r.debounce = nil
			r.refresh()
		})
	})
}

// relevant says whether a change of the watcher can change the status: a change of a file of the git directory that the status reads,
// or of a file of the work tree that git does not ignore (or that is among the changes already).
func (r *repository) relevant(c extension.FileChange) bool {
	if c.Kind == extension.FileRescan {
		return true
	}
	paths := []string{c.Path}
	if c.OldPath != "" {
		paths = append(paths, c.OldPath)
	}
	for _, p := range paths {
		if r.relevantPath(p) {
			return true
		}
	}
	return false
}

func (r *repository) relevantPath(p string) bool {
	p = filepath.Clean(p)
	for _, dir := range []string{r.loc.GitDir, r.commonDir()} {
		if dir != "" && git.Within(p, dir) {
			rel, _ := filepath.Rel(dir, p)
			return gitFileMatters(filepath.ToSlash(rel))
		}
	}
	if !git.Within(p, r.root) {
		return false
	}
	if strings.Contains(filepath.Base(p), ".watchman-cookie-") {
		return false
	}
	if r.known(p) {
		return true
	}
	return !r.ignore.Ignored(p, false)
}

// known says whether the path is among the changes the status listed: such a file is tracked, so an ignore rule does not hide it.
func (r *repository) known(p string) bool {
	for _, list := range [][]resource{r.res.merge, r.res.index, r.res.working, r.res.untracked} {
		for _, res := range list {
			if res.path == p {
				return true
			}
		}
	}
	return false
}

// gitFileMatters says whether a path below a git directory is one the status reads (SPEC §10.30 "Refresh"): HEAD, the index, refs,
// the packed refs, the files of a merge, rebase or cherry-pick in progress, the configuration and info/exclude. Lock files, the
// cookies of watchman and everything else (the reflogs, COMMIT_EDITMSG, FETCH_HEAD, hooks, gc.log) are not.
func gitFileMatters(rel string) bool {
	if rel == "." || rel == "" {
		return false
	}
	if strings.HasSuffix(rel, ".lock") || strings.Contains(rel, ".watchman-cookie-") || rel == "objects" || strings.HasPrefix(rel, "objects/") {
		return false
	}
	switch rel {
	case "HEAD", "index", "packed-refs", "config", "config.worktree", "MERGE_HEAD", "REBASE_HEAD", "CHERRY_PICK_HEAD", "info/exclude", "shallow", "commondir":
		return true
	}
	for _, prefix := range []string{"refs/", "rebase-merge", "rebase-apply", "worktrees/"} {
		if rel == strings.TrimSuffix(prefix, "/") || strings.HasPrefix(rel, prefix) {
			if strings.HasPrefix(rel, "worktrees/") {
				// the files of a linked worktree's folder that the status of that worktree reads
				parts := strings.SplitN(rel, "/", 3)
				return len(parts) == 3 && gitFileMatters(parts[2])
			}
			return true
		}
	}
	return false
}

// beginOwn marks that a command of Emetos itself is running in the repository: the changes it makes are not told to the status
// (EndOwn refreshes once).
func (r *repository) beginOwn() { r.own++ }

// endOwn ends beginOwn and refreshes the repository.
func (r *repository) endOwn() {
	if r.own > 0 {
		r.own--
	}
	if r.own == 0 {
		r.refresh()
	}
}
