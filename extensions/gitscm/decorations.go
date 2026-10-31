package gitscm

import (
	"context"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// File decorations (SPEC §10.30 "File decorations and the Explorer", step 24.6): the colors and letters of the Explorer, Open Editors,
// the tabs, the breadcrumbs, the Search results and the Source Control view. Each repository has a provider that answers from the
// status it last ran (git.decorations.enabled), and one provider for the whole extension answers for the files git ignores, which it
// asks git for in batches.
//
// Ported from VS Code: extensions/git/src/decorationProvider.ts and extensions/git/src/repository.ts (Resource: getStatusLetter,
// getStatusColor, getStatusText, priority, resourceDecoration)

// priority orders the decorations that several providers give a file, and the colors a folder takes from the files below it: a
// conflict first, then an ignored file, a modified one and the rest.
//
// Ported from VS Code: extensions/git/src/repository.ts (Resource.priority)
func priority(s git.Status) int {
	switch s {
	case git.StatusIndexModified, git.StatusModified, git.StatusIndexCopied, git.StatusTypeChanged:
		return 2
	case git.StatusIgnored:
		return 3
	case git.StatusBothDeleted, git.StatusAddedByUs, git.StatusDeletedByThem, git.StatusAddedByThem, git.StatusDeletedByUs, git.StatusBothAdded, git.StatusBothModified:
		return 4
	}
	return 1
}

// decorationOf is the file decoration of a status. A deleted file does not propagate: the folder is not marked for what is gone.
//
// Ported from VS Code: extensions/git/src/repository.ts (Resource.resourceDecoration)
func decorationOf(s git.Status) extension.FileDecoration {
	return extension.FileDecoration{
		Badge: statusLetter(s), Tooltip: statusText(s), Color: statusColor(s),
		Propagate: s != git.StatusDeleted && s != git.StatusIndexDeleted, Priority: priority(s),
	}
}

// submoduleDecoration is the decoration of the folder of a submodule.
//
// Ported from VS Code: extensions/git/src/decorationProvider.ts (GitDecorationProvider.SubmoduleDecorationData)
var submoduleDecoration = extension.FileDecoration{Tooltip: "Submodule", Badge: "S", Color: "gitDecoration.submoduleResourceForeground"}

// decorationData collects the decorations of the files of a repository by their paths: the staged files, the untracked ones, the
// changes of the work tree and the merge changes, in that order, so that the later group wins for a file in two of them (VS Code
// builds the map the same way). A renamed file decorates the old path too, and the folders of the submodules are decorated.
//
// Ported from VS Code: extensions/git/src/decorationProvider.ts (GitDecorationProvider.onDidRunGitStatus, collectDecorationData and
// collectSubmoduleDecorationData)
func decorationData(g groups, root string, submodules []string) map[string]extension.FileDecoration {
	out := make(map[string]extension.FileDecoration, g.total()+len(submodules))
	for _, list := range [][]resource{g.index, g.untracked, g.working, g.merge} {
		for _, r := range list {
			d := decorationOf(r.status)
			out[r.path] = d
			if (r.status == git.StatusIndexRenamed || r.status == git.StatusIntentToRename) && r.from != "" {
				out[r.from] = d
			}
		}
	}
	for _, p := range submodules {
		out[filepath.Join(root, filepath.FromSlash(p))] = submoduleDecoration
	}
	return out
}

// submodulePaths lists the paths (relative, with slashes) of the submodules a repository's .gitmodules names; it reads the file and
// runs nothing.
func submodulePaths(root string) []string {
	data, err := os.ReadFile(filepath.Join(root, ".gitmodules"))
	if err != nil {
		return nil
	}
	var out []string
	for _, sm := range git.ParseGitmodules(string(data)) {
		out = append(out, sm.Path)
	}
	return out
}

// errTag is the Tag of an error of the extension's errors package, "" for another.
func errTag(err error) string {
	if appErr, ok := errors.FromError(err); ok {
		return appErr.Tag
	}
	return ""
}

// decoPath is the path of a file: URI the way the decorations service spells it: the escaped and the plain spelling both come.
func decoPath(uri string) (string, bool) {
	rest, ok := strings.CutPrefix(uri, "file://")
	if !ok {
		return "", false
	}
	if strings.IndexByte(rest, '%') >= 0 {
		if u, err := url.PathUnescape(rest); err == nil {
			rest = u
		}
	}
	return filepath.Clean(rest), true
}

// statusProvider is the provider of one repository: it answers from the decorations of the last status.
type statusProvider struct {
	reg  extension.FileDecorations
	data map[string]extension.FileDecoration
	// gen counts the sets of decorations given to the provider, so that a diff made off the UI thread knows what it was made against.
	gen int
}

// Decoration implements extension.FileDecorationProvider.
func (p *statusProvider) Decoration(uri string) (extension.FileDecoration, bool) {
	path, ok := decoPath(uri)
	if !ok {
		return extension.FileDecoration{}, false
	}
	d, ok := p.data[path]
	return d, ok
}

// changedURIs are the files whose decoration is not the same in next as in prev, as the URIs the host is told: the ones that gained, lost or
// changed a decoration. It is made on the goroutine of the status, which holds the previous map (maps given to a provider are not changed).
func changedURIs(prev, next map[string]extension.FileDecoration) []string {
	var changed []string
	for path, d := range next {
		if old, ok := prev[path]; !ok || old != d {
			changed = append(changed, fileURI(path))
		}
	}
	for path := range prev {
		if _, ok := next[path]; !ok {
			changed = append(changed, fileURI(path))
		}
	}
	return changed
}

// set puts the decorations of a new status in place and tells the host of the files that changed.
func (p *statusProvider) set(next map[string]extension.FileDecoration, changed []string) {
	p.data = next
	p.gen++
	if len(changed) > 0 {
		p.reg.Fire(changed...)
	}
}

// syncDecorations makes the decorations of the repository match git.decorations.enabled: with the setting on the repository has a provider
// that is told its status, with it off the provider goes.
func (r *repository) syncDecorations() {
	x := r.m.x
	if r.disposed || !x.cfg.decorationsEnabled || !r.trusted || r.repo == nil || !r.refreshed {
		r.dropDecorations()
		return
	}
	next := decorationData(r.res, r.root, r.submodules)
	prev, gen := r.decorationState()
	r.applyDecorations(next, changedURIs(prev, next), gen)
}

// decorationState is what a status is diffed against: the decorations the provider has and their generation (-1 when there is no provider).
func (r *repository) decorationState() (map[string]extension.FileDecoration, int) {
	if r.decos == nil {
		return nil, -1
	}
	return r.decos.data, r.decos.gen
}

// applyDecorations gives the provider of the repository the decorations of the status it found, making the provider the first time.
// changed is the diff against the decorations of generation prevGen, made off the UI thread; when the provider is another by now (the
// setting was switched meanwhile) the diff is made here.
func (r *repository) applyDecorations(next map[string]extension.FileDecoration, changed []string, prevGen int) {
	x := r.m.x
	if r.disposed || !x.cfg.decorationsEnabled {
		return
	}
	if r.decos == nil {
		p := &statusProvider{}
		reg, err := x.api.Decorations().Register(p)
		if err != nil {
			if !noDecorationsService(err) {
				x.log.Warn("cannot register the file decorations of a repository", "root", r.root, "err", err)
			}
			return
		}
		p.reg = reg
		r.decos = p
		if prevGen != -1 {
			changed = changedURIs(nil, next)
		}
	} else if r.decos.gen != prevGen {
		changed = changedURIs(r.decos.data, next)
	}
	r.decos.set(next, changed)
}

// dropDecorations takes the provider of the repository away.
func (r *repository) dropDecorations() {
	if r.decos != nil {
		r.decos.reg.Dispose()
		r.decos = nil
	}
}

// ---- the files git ignores ----

// ignoreRepo is what the provider of ignored files asks of a repository.
type ignoreRepo interface {
	// CheckIgnoreFunc is, called on the UI thread, the function that tells which of the paths git ignores (it may run on any goroutine).
	CheckIgnoreFunc() func(ctx context.Context, paths []string) ([]string, error)
}

// ignoreHost is what the provider of ignored files needs of the extension: the repository of a path, the timer and the UI thread.
type ignoreHost interface {
	// RepositoryOf is the repository that holds the path, nil for none (and for a repository that is not open yet).
	RepositoryOf(path string) ignoreRepo
	// AfterFunc calls f after d, from its own goroutine, and returns the function that stops it.
	AfterFunc(d time.Duration, f func()) (stop func())
	// Post runs f on the UI thread.
	Post(f func())
}

// The numbers of the provider of ignored files.
const (
	// ignoreDelay is how long the paths gather before git is asked (VS Code's @debounce(500)).
	ignoreDelay = 500 * time.Millisecond
	// ignoreKnownMax bounds the answers kept; when it is full they are all forgotten and asked again as the views need them.
	ignoreKnownMax = 100_000
	// ignoredPriority is the priority of an ignored file.
	ignoredPriority = 3
)

// ignoredDecoration is the decoration of an ignored file: its name dims.
//
// Ported from VS Code: extensions/git/src/decorationProvider.ts (GitIgnoreDecorationProvider.Decoration)
var ignoredDecoration = extension.FileDecoration{Color: "gitDecoration.ignoredResourceForeground", Priority: ignoredPriority}

// ignoreProvider answers for the files git ignores. A file it has not heard of is queued, and git is asked for the queue 500 ms after the
// first path came, with one check-ignore for each repository; the ones git ignores are fired. Everything is used on the UI thread.
//
// Ported from VS Code: extensions/git/src/decorationProvider.ts (GitIgnoreDecorationProvider)
type ignoreProvider struct {
	host ignoreHost
	ctx  context.Context
	log  func(msg string, args ...any)
	reg  extension.FileDecorations

	// known are the answers by path: true for an ignored file.
	known map[string]bool
	// queue are the paths waiting for git, by repository.
	queue map[ignoreRepo]map[string]struct{}
	// stop stops the timer; nil when none runs.
	stop func()
	// disposed stops everything.
	disposed bool
}

// newIgnoreProvider makes the provider; the caller registers it with the host (reg).
func newIgnoreProvider(ctx context.Context, host ignoreHost, log func(string, ...any)) *ignoreProvider {
	return &ignoreProvider{host: host, ctx: ctx, log: log, known: map[string]bool{}, queue: map[ignoreRepo]map[string]struct{}{}}
}

// Decoration implements extension.FileDecorationProvider.
func (p *ignoreProvider) Decoration(uri string) (extension.FileDecoration, bool) {
	if p.disposed {
		return extension.FileDecoration{}, false
	}
	path, ok := decoPath(uri)
	if !ok {
		return extension.FileDecoration{}, false
	}
	if ignored, ok := p.known[path]; ok {
		return ignoredDecoration, ignored
	}
	repo := p.host.RepositoryOf(path)
	if repo == nil {
		return extension.FileDecoration{}, false
	}
	q := p.queue[repo]
	if q == nil {
		q = map[string]struct{}{}
		p.queue[repo] = q
	}
	if _, ok := q[path]; !ok {
		q[path] = struct{}{}
		p.checkSoon()
	}
	return extension.FileDecoration{}, false
}

// checkSoon starts the timer of the next batch.
func (p *ignoreProvider) checkSoon() {
	if p.stop != nil {
		return
	}
	p.stop = p.host.AfterFunc(ignoreDelay, func() { p.host.Post(p.check) })
}

// check asks git, for each repository with paths waiting, which of them it ignores.
func (p *ignoreProvider) check() {
	p.stop = nil
	if p.disposed {
		return
	}
	queue := p.queue
	p.queue = map[ignoreRepo]map[string]struct{}{}
	for repo, set := range queue {
		paths := make([]string, 0, len(set))
		for path := range set {
			paths = append(paths, path)
		}
		run := repo.CheckIgnoreFunc()
		go func() {
			ignored, err := run(p.ctx, paths)
			p.host.Post(func() { p.checked(paths, ignored, err) })
		}()
	}
}

// checked keeps what git said and tells the host of the files that are ignored.
func (p *ignoreProvider) checked(paths, ignored []string, err error) {
	if p.disposed {
		return
	}
	if err != nil {
		// a path in a submodule makes git fail (VS Code leaves that one out of the log too)
		if tag := errTag(err); tag != "IsInSubmodule" && p.ctx.Err() == nil {
			p.log("git check-ignore failed", "err", err)
		}
		return
	}
	if len(p.known)+len(paths) > ignoreKnownMax {
		p.known = map[string]bool{}
	}
	ignoredSet := make(map[string]bool, len(ignored))
	for _, path := range ignored {
		ignoredSet[filepath.Clean(path)] = true
	}
	var fire []string
	for _, path := range paths {
		hit := ignoredSet[path]
		p.known[path] = hit
		if hit {
			fire = append(fire, fileURI(path))
		}
	}
	if len(fire) > 0 {
		p.reg.Fire(fire...)
	}
}

// reset forgets the answers (the ignore rules or the repositories changed) and asks the host to ask again for what the views show.
func (p *ignoreProvider) reset() {
	if p.disposed {
		return
	}
	p.known = map[string]bool{}
	p.queue = map[ignoreRepo]map[string]struct{}{}
	if p.stop != nil {
		p.stop()
		p.stop = nil
	}
	p.reg.Fire()
}

// repositoryClosed drops the paths waiting for a repository that was closed, so that git is not run for it (its folder may be gone).
func (p *ignoreProvider) repositoryClosed(repo ignoreRepo) {
	delete(p.queue, repo)
	p.reset()
}

// dispose stops the provider.
func (p *ignoreProvider) dispose() {
	p.disposed = true
	if p.stop != nil {
		p.stop()
		p.stop = nil
	}
	p.queue = nil
}

// ---- the extension's wiring ----

// ignoreHostOf is the ignoreHost of the extension.
type ignoreHostOf struct{ x *gitscm }

// RepositoryOf implements ignoreHost: the open, trusted repository that holds the path.
func (h ignoreHostOf) RepositoryOf(path string) ignoreRepo {
	r := h.x.model.forPath(path)
	if r == nil || r.repo == nil || !r.trusted || r.disposed {
		return nil
	}
	return r
}

// AfterFunc implements ignoreHost.
func (h ignoreHostOf) AfterFunc(d time.Duration, f func()) func() { return h.x.clock.AfterFunc(d, f) }

// Post implements ignoreHost.
func (h ignoreHostOf) Post(f func()) { h.x.api.Post(f) }

// CheckIgnoreFunc implements ignoreRepo.
func (r *repository) CheckIgnoreFunc() func(ctx context.Context, paths []string) ([]string, error) {
	repo := r.repo
	return repo.CheckIgnore
}

// startDecorations registers the provider of the ignored files; the providers of the repositories come with their statuses.
func (x *gitscm) startDecorations() {
	p := newIgnoreProvider(x.ctx, ignoreHostOf{x}, x.log.Warn)
	reg, err := x.api.Decorations().Register(p)
	if err != nil {
		if !noDecorationsService(err) {
			x.log.Warn("cannot register the file decorations of ignored files", "err", err)
		}
		return
	}
	p.reg = reg
	x.ignores = p
}

// ignoresChanged tells the provider of the ignored files that the rules or the repositories changed.
func (x *gitscm) ignoresChanged() {
	if x.ignores != nil {
		x.ignores.reset()
	}
}

// isIgnoreFile says whether a path is a file whose change changes what git ignores: a .gitignore or .git/info/exclude.
func isIgnoreFile(path string) bool {
	path = filepath.ToSlash(path)
	return strings.HasSuffix(path, "/.gitignore") || strings.HasSuffix(path, ".git/info/exclude")
}

// noDecorationsService tells whether Register failed because the scope has no decorations service (a host without a workbench, as the
// tests of the languages make): nothing shows decorations there, so it is not worth a warning.
func noDecorationsService(err error) bool {
	appErr, ok := errors.FromError(err)
	return ok && appErr.Tag == "ExtensionDecorationsUnavailable"
}
