package gitscm

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// model is the set of repositories of the scope: the open ones, the ones the user closed, the ones in the parent folders that wait
// for a word, the unsafe ones, and the state of the scan (VS Code's Model).
type model struct {
	x *gitscm

	repos map[string]*repository
	order []*repository

	closed      map[string]bool
	closedOrder []string

	parents []candidate
	unsafe  []unsafeRepo

	// state is "uninitialized" until the first scan ended, then "initialized" (the context key git.state).
	state string

	// queue holds the repositories that wait for the trust question, and asking says that a question is open; askedParent are the
	// parent folders that were asked about as a whole.
	queue       []*repository
	asking      bool
	askedParent map[string]bool

	// rescan is the pending scan for the repositories that appeared (a .git that was made).
	rescan      func()
	scanning    bool
	rescanAgain bool
	notified    map[string]bool
}

// unsafeRepo is a repository git refuses because another user owns it.
type unsafeRepo struct {
	root string
	// safeDir is the path git says to put in safe.directory.
	safeDir string
}

func newModel(x *gitscm) *model {
	return &model{x: x, repos: map[string]*repository{}, closed: map[string]bool{}, state: "uninitialized", askedParent: map[string]bool{}, notified: map[string]bool{}}
}

// scan finds the repositories with the file system, off the UI thread, and opens them. The first scan ends in the state
// "initialized".
func (m *model) scan() {
	x := m.x
	if m.scanning {
		m.rescanAgain = true
		return
	}
	m.scanning = true
	in := discoveryInput{folder: x.folder, home: x.home, cfg: &x.cfg, editorDirs: x.editorDirs()}
	in.excluded = excludedBy(x.excludeSetting())
	in.ignored = func(root string) bool { return x.ignoredRepository(root) }
	go func() {
		found := discover(in)
		x.api.Post(func() { m.applyDiscovery(found) })
	}()
}

// applyDiscovery opens what the scan found.
func (m *model) applyDiscovery(found discovered) {
	m.scanning = false
	if m.x.stopped.Load() {
		return
	}
	for _, c := range found.open {
		m.openRepository(c, false)
	}
	parentsBefore := len(m.parents)
	m.parents = m.parents[:0]
	for _, c := range found.parents {
		if m.repos[c.loc.Root] == nil {
			m.parents = append(m.parents, c)
		}
	}
	first := m.state != "initialized"
	m.state = "initialized"
	m.updateContext()
	if len(m.parents) > 0 && m.x.cfg.openInParentFolders == "prompt" && (first || len(m.parents) > parentsBefore) {
		m.parentNotification()
	}
	m.askNext()
	if m.rescanAgain {
		m.rescanAgain = false
		m.scan()
	}
}

// openRepository makes the repository of a location a repository of the view. It does nothing for a repository that is open, ignored
// or closed (unless force). A repository that the user did not trust has a source control with no groups and waits for the question.
//
// Ported from VS Code: extensions/git/src/model.ts (openRepository)
func (m *model) openRepository(c candidate, force bool) *repository {
	x := m.x
	if !x.cfg.enabled || x.stopped.Load() {
		return nil
	}
	root := c.loc.Root
	if r := m.repos[root]; r != nil {
		return r
	}
	if x.ignoredRepository(root) {
		return nil
	}
	if !force && m.closed[root] {
		return nil
	}
	r, err := m.newRepository(c.loc)
	if err != nil {
		x.log.Warn("cannot make the source control of a repository", "root", root, "err", err)
		return nil
	}
	m.repos[root] = r
	m.order = append(m.order, r)
	delete(m.closed, root)
	m.closedOrder = slices.DeleteFunc(m.closedOrder, func(s string) bool { return s == root })
	x.logLine("Opened repository (path): " + root)
	if x.trustedBy(c.loc) {
		m.notifyPrograms(r)
		r.start()
	} else if !x.trust.Declined(root) {
		m.ask(r)
	}
	m.updateContext()
	return r
}

// opened is called when a repository is open (git opened it), to look for its submodules.
func (m *model) opened(r *repository) {
	m.updateContext()
	m.x.ignoresChanged()
	if m.x.cfg.detectSubmodules {
		m.openSubmodules(r)
	}
}

// openSubmodules opens the submodules of the repository that are checked out, up to git.detectSubmodulesLimit, as repositories of
// their own (SPEC §10.30): found in the .gitmodules file and by their .git, with no process.
func (m *model) openSubmodules(r *repository) {
	data, err := os.ReadFile(filepath.Join(r.root, ".gitmodules"))
	if err != nil {
		return
	}
	n := 0
	for _, sm := range git.ParseGitmodules(string(data)) {
		if n >= m.x.cfg.detectSubmodulesLimit {
			break
		}
		loc, ok := git.RepositoryAt(filepath.Join(r.root, filepath.FromSlash(sm.Path)))
		if !ok || loc.Kind != git.KindSubmodule {
			continue
		}
		n++
		m.openRepository(candidate{loc: loc, why: foundSubmodule}, false)
	}
}

// openFailed is called when git could not open a repository that was trusted: an unsafe repository is listed in the welcome content
// and in Manage Unsafe Repositories, any other failure is logged and the repository is dropped.
func (m *model) openFailed(r *repository, err error) {
	x := m.x
	if appErr, ok := errors.FromError(err); ok && appErr.Tag == "UnsafeRepository" {
		stderr, _ := appErr.Metadata["stderr"].(string)
		m.addUnsafe(r.root, safeDirectory(stderr, r.root))
		m.drop(r)
		return
	}
	x.log.Warn("git cannot open a repository", "root", r.root, "err", err)
	x.logLine("Opening repository failed (" + r.root + "): " + err.Error())
	m.drop(r)
}

var reSafeDirectory = regexp.MustCompile(`git config --global --add safe\.directory '?([^'\n]+)'?`)

// safeDirectory reads the path git wants in safe.directory from its message.
func safeDirectory(stderr, fallback string) string {
	if m := reSafeDirectory.FindStringSubmatch(stderr); m != nil {
		return strings.TrimSpace(m[1])
	}
	return fallback
}

// addUnsafe lists an unsafe repository, with a notification when the scan is over and a repository is open already.
func (m *model) addUnsafe(root, safeDir string) {
	for _, u := range m.unsafe {
		if u.root == root {
			return
		}
	}
	m.unsafe = append(m.unsafe, unsafeRepo{root: root, safeDir: safeDir})
	m.updateContext()
	if m.state == "initialized" && len(m.order) > 0 {
		m.x.api.Notifications().Error("The git repository in the current folder is potentially unsafe as the folder is owned by someone other than the current user.",
			extension.NotificationAction{Label: "Manage Unsafe Repositories", Run: func() { m.x.run("git.manageUnsafeRepositories") }})
	}
}

// drop removes a repository without remembering it as closed.
func (m *model) drop(r *repository) {
	r.dispose()
	if m.x.ignores != nil {
		m.x.ignores.repositoryClosed(r)
	}
	delete(m.repos, r.root)
	m.order = slices.DeleteFunc(m.order, func(o *repository) bool { return o == r })
	m.queue = slices.DeleteFunc(m.queue, func(o *repository) bool { return o == r })
	m.updateContext()
}

// close closes a repository (Close Repository): it goes from the view and is remembered, so that a scan does not bring it back.
func (m *model) close(r *repository) {
	m.drop(r)
	if !m.closed[r.root] {
		m.closed[r.root] = true
		m.closedOrder = append(m.closedOrder, r.root)
	}
	m.updateContext()
}

// reopen opens the closed repository with this root.
func (m *model) reopen(root string) {
	loc, ok := git.RepositoryAt(root)
	if !ok {
		delete(m.closed, root)
		m.closedOrder = slices.DeleteFunc(m.closedOrder, func(s string) bool { return s == root })
		m.updateContext()
		return
	}
	m.openRepository(candidate{loc: loc, why: foundFolder}, true)
	m.askNext()
}

// openOnlyOpened lists the repositories git opened (the trusted ones).
func (m *model) openRepos() []*repository {
	var out []*repository
	for _, r := range m.order {
		if r.repo != nil && !r.disposed {
			out = append(out, r)
		}
	}
	return out
}

// parentNotification asks whether to open the repositories of the parent folders (the mode "prompt").
//
// Ported from VS Code: extensions/git/src/model.ts (showParentRepositoryNotification)
func (m *model) parentNotification() {
	x := m.x
	msg := "A git repository was found in the parent folders of the workspace or the open file(s). Would you like to open the repository?"
	if len(m.parents) > 1 {
		msg = "Git repositories were found in the parent folders of the workspace or the open file(s). Would you like to open the repositories?"
	}
	set := func(value string) {
		if err := x.api.Settings().Update("git.openRepositoryInParentFolders", value); err != nil {
			x.log.Warn("cannot save git.openRepositoryInParentFolders", "err", err)
		}
	}
	x.api.Notifications().Info(msg,
		extension.NotificationAction{Label: "Yes", Run: func() { m.openParents() }},
		extension.NotificationAction{Label: "Always", Run: func() { set("always"); m.openParents() }},
		extension.NotificationAction{Label: "Never", Run: func() { set("never") }})
}

// openParents opens the repositories of the parent folders.
func (m *model) openParents() {
	list := append([]candidate(nil), m.parents...)
	m.parents = nil
	for _, c := range list {
		m.openRepository(c, true)
	}
	m.updateContext()
	m.askNext()
}

// updateContext sets the context keys the menus, the welcome contents and the palette read.
func (m *model) updateContext() {
	x := m.x
	if x.stopped.Load() {
		return
	}
	open := m.openRepos()
	roots := make([]string, len(open))
	for i, r := range open {
		roots[i] = r.root
	}
	x.owners.set(roots)
	var merge, rebase, cherry, fresh, committing bool
	for _, r := range open {
		committing = committing || r.committing
		merge = merge || r.ops.merge
		rebase = rebase || r.ops.rebase
		cherry = cherry || r.ops.cherryPick
		fresh = fresh || (r.refreshed && r.fresh)
	}
	set := func(key string, v any) {
		if err := x.api.Context().Set(key, v); err != nil {
			x.log.Debug("cannot set a context key", "key", key, "err", err)
		}
	}
	set("git.state", m.state)
	x.stash.scanned.Store(m.state == "initialized")
	set("git.missing", x.missing)
	set("git.untrustedRepositoryCount", len(m.untrusted()))
	set("gitOpenRepositoryCount", len(open))
	set("git.parentRepositoryCount", len(m.parents))
	set("git.unsafeRepositoryCount", len(m.unsafe))
	set("git.closedRepositoryCount", len(m.closedOrder))
	set("gitMergeInProgress", merge)
	set("gitRebaseInProgress", rebase)
	set("gitCherryPickInProgress", cherry)
	set("gitFreshRepository", fresh)
	set("git.commitInProgress", committing)
	set("gitVersion235", x.install.Version.AtLeast(2, 35))
}

// onFilesChanged hears the changes of the folder of the window tab and scans again when a .git appeared or went (VS Code's
// onPossibleGitRepositoryChange), after the debounce.
func (m *model) onFilesChanged(changes []extension.FileChange) {
	x := m.x
	if !x.cfg.enabled || x.cfg.autoDetection == "false" || x.stopped.Load() {
		return
	}
	for _, c := range changes {
		if c.Kind == extension.FileRescan && c.Path == "." {
			m.scheduleScan()
			return
		}
		for _, p := range []string{c.Path, c.OldPath} {
			if p == ".git" || strings.HasSuffix(p, "/.git") {
				// a repository made, moved or removed; changes inside a .git are the repository's own watcher's
				if m.repos[filepath.Join(x.folder, filepath.FromSlash(strings.TrimSuffix(p, ".git")))] == nil || c.Kind == extension.FileDeleted {
					m.scheduleScan()
					return
				}
			}
		}
	}
}

// scheduleScan scans again in DebounceDelay.
func (m *model) scheduleScan() {
	if m.rescan != nil || m.x.stopped.Load() {
		return
	}
	x := m.x
	m.rescan = x.clock.AfterFunc(DebounceDelay, func() {
		x.api.Post(func() {
			m.rescan = nil
			m.scan()
		})
	})
}

// byHandle finds the repository whose source control has this handle.
func (m *model) byHandle(handle string) *repository {
	for _, r := range m.order {
		if r.handle == handle {
			return r
		}
	}
	return nil
}

// byRoot finds the repository with this root.
func (m *model) byRoot(root string) *repository { return m.repos[filepath.Clean(root)] }

// forPath finds the open repository a file is in, the one with the longest root.
func (m *model) forPath(path string) *repository {
	var best *repository
	for _, r := range m.order {
		if git.Within(path, r.root) && (best == nil || len(r.root) > len(best.root)) {
			best = r
		}
	}
	return best
}

// uriPath is the path of a file: URI.
func uriPath(uri string) (string, bool) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return "", false
	}
	return filepath.Clean(u.Path), true
}

// notifyPrograms tells the user, once in the session, that a repository the user did not answer the question for in this session (a
// parent folder was trusted, or the repository belongs to a trusted one) has a configuration that names programs (SPEC §10.30
// "Untrusted repositories"): they run as in a terminal, and the notification says why Emetos allows it.
func (m *model) notifyPrograms(r *repository) {
	x := m.x
	if m.notified["programs:"+r.root] {
		return
	}
	kind, via := x.trust.Why(r.root)
	var reason string
	switch {
	case kind == "repository":
		return // the user trusted this repository by itself: they saw the list in the question
	case kind == "parent":
		reason = "you trusted all the repositories in " + via
	case kind == "session":
		return
	case r.loc.Parent != "":
		reason = "it belongs to the trusted repository " + r.loc.Parent
	default:
		return
	}
	programs := m.programsOf(r)
	if len(programs) == 0 {
		return
	}
	m.notified["programs:"+r.root] = true
	var keys []string
	for _, p := range programs {
		keys = append(keys, p.Key)
	}
	x.api.Notifications().Info("The Git configuration of "+r.name+" names programs that Git can run ("+strings.Join(keys, ", ")+"). Emetos runs Git there because "+reason+".",
		extension.NotificationAction{Label: "Manage Trusted Repositories", Run: func() { x.run(CommandManageTrusted) }})
}
