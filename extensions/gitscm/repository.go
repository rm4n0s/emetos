package gitscm

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// repository is one repository of the Source Control view: its source control, its groups and its state (VS Code's Repository). It
// is used on the UI thread; the commands of git run on goroutines that hand their results back with api.Post.
type repository struct {
	m    *model
	loc  git.Location
	root string
	name string
	// id is the id of the repository in the URIs of the versions of its files (uri.go).
	id string

	src extension.SourceControl
	// handle is the handle of the source control, which the menus' arguments carry.
	handle string

	// trusted says that the user trusted the repository (or git.TrustedBy does): only then git is started in it.
	trusted bool
	// disposed says that the repository was closed.
	disposed bool

	ctx    context.Context
	cancel context.CancelFunc

	repo   *git.Repo
	queue  *git.Queue
	groups struct{ merge, index, working, untracked extension.ResourceGroup }

	// state, as the last refresh found it
	head       head
	refs       []git.Ref
	remotes    []git.Remote
	stashes    int
	ops        operations
	fresh      bool
	res        groups
	hugeLimit  int
	didWarn    bool
	refreshed  bool
	lastStatus []git.StatusEntry
	// timings are the times of the last refresh.
	timings Timings

	// refreshing and again coalesce the refreshes: one runs at a time, the asks that come while it runs are one more run.
	refreshing bool
	again      bool
	// busyStop stops the timer that shows the progress bar after a delay.
	busyStop func()
	busy     bool

	// own counts the commands of Emetos itself that are running: the changes they cause are not told by the watcher, the command
	// refreshes when it ends (SPEC §10.30 "Refresh").
	own int

	// decos is the provider of the file decorations of the repository (decorations.go), nil while there is none, and submodules the
	// paths of its submodules from the last status.
	decos      *statusProvider
	submodules []string

	watchMu    sync.Mutex
	watchStops []func()
	debounce   func()
	ignore     *ignoreMatcher
	refreshes  int // the number of status runs, for the tests and the Output channel
	published  *RepositoryState
	// gen counts the times the repository was untrusted, so that the result of a command that was cancelled by it is dropped.
	gen int
}

// kind is the kind of the repository.
func (r *repository) kind() git.Kind { return r.loc.Kind }

// newRepository makes the repository of a location, with its source control.
func (m *model) newRepository(loc git.Location) (*repository, error) {
	x := m.x
	r := &repository{m: m, loc: loc, root: loc.Root, name: baseName(loc.Root), id: repoID(loc.Root)}
	src, err := x.api.SourceControl().Create(extension.SourceControlSpec{
		ID: "git", Label: "Git", RootURI: fileURI(loc.Root), ContextValue: loc.Kind.String(),
	})
	if err != nil {
		return nil, err
	}
	r.src, r.handle = src, src.Handle()
	r.ctx, r.cancel = context.WithCancel(x.ctx)
	r.queue = git.NewQueue(0)
	r.ignore = newIgnoreMatcher(loc.Root, loc.GitDir)
	return r, nil
}

// createGroups makes the groups of the source control, once the repository is trusted and git opened it.
func (r *repository) createGroups() error {
	specs := []struct {
		dst           *extension.ResourceGroup
		id, label     string
		hideWhenEmpty bool
	}{
		{&r.groups.merge, groupMerge, "Merge Changes", true},
		{&r.groups.index, groupIndex, "Staged Changes", !r.m.x.cfg.alwaysShowStagedGroup},
		{&r.groups.working, groupWorking, "Changes", false},
		{&r.groups.untracked, groupUntracked, "Untracked Changes", true},
	}
	for _, s := range specs {
		if *s.dst != nil {
			continue
		}
		g, err := r.src.CreateGroup(extension.ResourceGroupSpec{ID: s.id, Label: s.label, HideWhenEmpty: s.hideWhenEmpty, ContextValue: s.id})
		if err != nil {
			return err
		}
		*s.dst = g
	}
	return nil
}

// clearGroups takes the groups, the count and the status items away: the repository is not trusted (any more).
func (r *repository) clearGroups() {
	for _, g := range []*extension.ResourceGroup{&r.groups.merge, &r.groups.index, &r.groups.working, &r.groups.untracked} {
		if *g != nil {
			(*g).Dispose()
			*g = nil
		}
	}
	r.src.SetCount(-1)
	r.src.SetStatusBarCommands(nil)
	r.src.SetBusy(false)
}

// start opens the repository with git (rev-parse) now that it is trusted, and when that worked, runs the first status.
func (r *repository) start() {
	if r.disposed || r.repo != nil {
		return
	}
	r.trusted = true
	x := r.m.x
	runner := x.runner
	if runner == nil {
		return // git is not there; the repository starts when it is
	}
	x.stash.know(r.id) // its first status is on its way: a saved multi-file editor waits for it (awaitRepository)
	go func() {
		repo, err := git.Open(r.ctx, runner, r.queue, r.loc.Root)
		x.api.Post(func() { r.opened(repo, err) })
	}()
}

// opened is the end of start, on the UI thread.
func (r *repository) opened(repo *git.Repo, err error) {
	if r.disposed || r.ctx.Err() != nil {
		return
	}
	if err != nil {
		r.m.openFailed(r, err)
		return
	}
	r.repo = repo
	x := r.m.x
	x.content.repos.set(r.id, repo)
	if err := r.createGroups(); err != nil {
		r.m.x.log.Warn("cannot make the groups of a repository", "root", r.root, "err", err)
		return
	}
	r.src.SetActiveRepositoryName("")
	r.readRemotes()
	r.m.opened(r)
	x.ensureTimeline()
	r.refresh()
	r.startWatching()
}

// readRemotes reads the remotes from the configuration file (no process) and tells the repository cache.
func (r *repository) readRemotes() {
	entries, err := git.ReadLocalConfig(r.loc)
	if err != nil {
		r.m.x.log.Debug("cannot read the configuration of a repository", "root", r.root, "err", err)
		return
	}
	old := r.remotes
	r.remotes = git.RemotesFromConfig(entries)
	if r.m.x.cache != nil {
		r.m.x.cache.update(addedRemotes(old, r.remotes), addedRemotes(r.remotes, old), r.root)
	}
}

// addedRemotes lists the remotes of next that prev lacks (by name and URL).
func addedRemotes(prev, next []git.Remote) []git.Remote {
	var out []git.Remote
	for _, n := range next {
		found := false
		for _, p := range prev {
			if p.Name == n.Name && p.FetchURL == n.FetchURL {
				found = true
				break
			}
		}
		if !found {
			out = append(out, n)
		}
	}
	return out
}

// untrust ends everything git does in the repository, as Manage Trusted Repositories does when the user stops trusting it: the
// watchers stop, the commands that run are cancelled (SIGTERM to their process groups), and the groups, the count and the status items
// go. The repository stays in the view as an untrusted one.
func (r *repository) untrust() {
	r.stopWatching()
	r.cancel()
	r.ctx, r.cancel = context.WithCancel(r.m.x.ctx)
	r.gen++
	r.m.x.content.repos.remove(r.id)
	r.timelineGone()
	r.trusted, r.repo, r.refreshing, r.again, r.refreshed = false, nil, false, false, false
	r.res, r.head, r.refs, r.ops = groups{}, head{}, nil, operations{}
	r.setBusy(false)
	r.clearGroups()
	r.dropDecorations()
	r.m.x.ignoresChanged()
	r.publish()
}

// dispose closes the repository: the watchers stop, the commands that run are cancelled (SIGTERM to their process groups), and the
// source control goes.
func (r *repository) dispose() {
	if r.disposed {
		return
	}
	r.disposed = true
	r.m.x.content.repos.remove(r.id)
	r.timelineGone()
	r.multiGone()
	r.m.x.stashGone(r.id)
	r.stopWatching()
	r.dropDecorations()
	r.cancel()
	if r.busyStop != nil {
		r.busyStop()
	}
	r.src.Dispose()
}

// snapshot is what a refresh found, made off the UI thread.
type snapshot struct {
	gen     int
	status  git.StatusResult
	refs    []git.Ref
	stashes int
	ops     operations
	head    head
	fresh   bool
	err     error
	// groups is the status split into the groups, and states their resources as the host takes them: made off the UI thread.
	groups groups
	states [4][]extension.ResourceState
	// decos are the file decorations of the files and submodules the status found (nil when git.decorations.enabled was off).
	decos      map[string]extension.FileDecoration
	decoDiff   []string
	decoGen    int
	submodules []string
	// timings are the times of the parts of the refresh, for the measurements.
	timings Timings
}

// Timings are the times of the parts of the last refresh of a repository: git's status command with the parse of its output, the
// grouping and sorting off the UI thread, and the hand-over of the resources to the host on the UI thread (SPEC §14).
type Timings struct {
	// Status is the time of the status command, the parse of its stream included.
	Status time.Duration
	// Group is the time of splitting the entries into the groups and building the resources of the host, off the UI thread.
	Group time.Duration
	// Apply is the time the UI thread spends to put the snapshot in place.
	Apply time.Duration
}

// refresh asks for a status. Many asks while one runs are one more run (SPEC §10.30 "Queue": a refresh is coalesced).
func (r *repository) refresh() {
	if r.disposed || r.repo == nil {
		return
	}
	if r.refreshing {
		r.again = true
		return
	}
	r.refreshing = true
	r.setBusy(true)
	x := r.m.x
	repo, opts := r.repo, x.cfg.statusOptions()
	gitDir, commonDir := repo.Info.GitDir, repo.Info.CommonDir
	ctx, gen := r.ctx, r.gen
	root, mode, kind, decorate := r.root, x.cfg.untrackedChanges, r.kind(), x.cfg.decorationsEnabled
	prevDecos, prevGen := r.decorationState()
	go func() {
		snap := compute(ctx, repo, opts, gitDir, commonDir)
		if snap.err == nil {
			start := time.Now()
			snap.groups = splitStatus(root, snap.status.Entries, mode)
			snap.states = statesOf(snap.groups, kind)
			snap.timings.Group = time.Since(start)
			if decorate {
				snap.submodules = submodulePaths(root)
				snap.decos = decorationData(snap.groups, root, snap.submodules)
				snap.decoDiff, snap.decoGen = changedURIs(prevDecos, snap.decos), prevGen
			}
		}
		snap.gen = gen
		x.api.Post(func() { r.applySnapshot(snap) })
	}()
}

// compute runs the commands of a refresh: git status and git for-each-ref in parallel, and the files of the git directory.
func compute(ctx context.Context, repo *git.Repo, opts git.StatusOptions, gitDir, commonDir string) snapshot {
	var snap snapshot
	var wg sync.WaitGroup
	var statusErr, refsErr error
	wg.Add(2)
	go func() {
		defer wg.Done()
		start := time.Now()
		snap.status, statusErr = repo.Status(ctx, opts)
		snap.timings.Status = time.Since(start)
	}()
	go func() {
		defer wg.Done()
		snap.refs, refsErr = repo.Refs(ctx, "refs/heads", "refs/tags")
	}()
	wg.Wait()
	switch {
	case statusErr != nil:
		snap.err = statusErr
	case refsErr != nil:
		snap.err = refsErr
	}
	snap.ops = readOperations(gitDir)
	snap.head = headFrom(gitDir, snap.refs)
	snap.stashes = stashCount(commonDir)
	snap.fresh = snap.head.commit == ""
	return snap
}

// applySnapshot puts what a refresh found into the source control, on the UI thread.
func (r *repository) applySnapshot(snap snapshot) {
	if snap.gen != r.gen {
		return // the repository was untrusted while the command ran
	}
	r.refreshing = false
	r.refreshes++
	if r.disposed {
		return
	}
	applyStart := time.Now()
	defer func() {
		snap.timings.Apply = time.Since(applyStart)
		r.timings = snap.timings
		if r.again && !r.disposed {
			r.again = false
			r.refresh()
			return
		}
		r.setBusy(false)
	}()
	if snap.err != nil {
		if r.ctx.Err() == nil {
			r.m.x.log.Warn("git status failed", "root", r.root, "err", snap.err)
			r.m.x.logLine("git status failed in " + r.root + ": " + snap.err.Error())
		}
		return
	}
	x := r.m.x
	headBefore := r.head.commit
	r.head, r.refs, r.stashes, r.ops, r.fresh = snap.head, snap.refs, snap.stashes, snap.ops, snap.fresh
	r.lastStatus = snap.status.Entries
	if snap.status.HitLimit {
		r.hugeLimit = x.cfg.statusLimit
		r.warnLimit()
	} else {
		r.hugeLimit = 0
	}
	r.putGroups(snap.groups, snap.states)
	r.refreshed = true
	r.submodules = snap.submodules
	if snap.decos != nil {
		r.applyDecorations(snap.decos, snap.decoDiff, snap.decoGen)
	} else {
		r.syncDecorations()
	}
	r.updateStatusBar()
	r.m.updateContext()
	r.contentChanged(snap.head.commit != headBefore)
	r.multiChanged()
	if snap.head.commit != headBefore {
		x.blame.headMoved(r)
	}
	r.timelineSync(snap.head.commit != headBefore)
	x.updateActiveResource()
	r.publish()
}

// statesOf builds the resources of the host for the groups.
func statesOf(g groups, kind git.Kind) [4][]extension.ResourceState {
	return [4][]extension.ResourceState{toStates(g.merge, kind), toStates(g.index, kind), toStates(g.working, kind), toStates(g.untracked, kind)}
}

// setGroups puts the resources into the groups of the source control and sets the count.
func (r *repository) setGroups(g groups) { r.putGroups(g, statesOf(g, r.kind())) }

// putGroups is setGroups with the resources of the host made already (a refresh makes them off the UI thread).
func (r *repository) putGroups(g groups, states [4][]extension.ResourceState) {
	r.res = g
	if r.groups.merge == nil {
		return
	}
	r.groups.merge.SetResources(states[0])
	r.groups.index.SetResources(states[1])
	r.groups.working.SetResources(states[2])
	r.groups.untracked.SetResources(states[3])
	r.src.SetCount(countFor(g, r.m.x.cfg.countBadge))
}

// warnLimit shows the warning of a repository with more changes than git.statusLimit, once (VS Code's didWarnAboutLimit).
//
// Ported from VS Code: extensions/git/src/repository.ts (updateModelState)
func (r *repository) warnLimit() {
	x := r.m.x
	if x.cfg.ignoreLimitWarning || r.didWarn {
		return
	}
	r.didWarn = true
	x.api.Notifications().Warn("The git repository at \""+r.root+"\" has too many active changes, only a subset of Git features will be enabled.",
		extension.NotificationAction{Label: "Ignore", Run: func() {
			if err := x.api.Settings().Update("git.ignoreLimitWarning", true); err != nil {
				x.log.Warn("cannot save git.ignoreLimitWarning", "err", err)
			}
		}})
}

// setBusy shows the progress bar of the repository after a short delay, so a status that takes a few milliseconds does not
// flash it (git.showProgress).
func (r *repository) setBusy(busy bool) {
	x := r.m.x
	if busy {
		if r.busy || r.busyStop != nil || !x.cfg.showProgress {
			return
		}
		r.busyStop = x.clock.AfterFunc(300*time.Millisecond, func() {
			x.api.Post(func() {
				if r.disposed || !r.refreshing {
					return
				}
				r.busy = true
				r.src.SetBusy(true)
			})
		})
		return
	}
	if r.busyStop != nil {
		r.busyStop()
		r.busyStop = nil
	}
	if r.busy {
		r.busy = false
		r.src.SetBusy(false)
	}
}

// headLabel is the text of the branch item: the branch, a tag or the short commit, with * for changes, + for staged changes and ! for
// a merge or rebase in progress.
//
// Ported from VS Code: extensions/git/src/repository.ts (headLabel)
func (r *repository) headLabel() string {
	h := r.head
	label := h.name
	if label == "" {
		label = h.tag
	}
	if label == "" {
		label = h.commit
		if n := r.m.x.cfg.commitShortHashLength; n > 0 && len(label) > n {
			label = label[:n]
		}
	}
	if len(r.res.working)+len(r.res.untracked) > 0 {
		label += "*"
	}
	if len(r.res.index) > 0 {
		label += "+"
	}
	if r.ops.merge || r.ops.rebaseCommit != "" {
		label += "!"
	}
	return label
}

// syncLabel is the text of the sync item: "2↓ 1↑".
//
// Ported from VS Code: extensions/git/src/repository.ts (syncLabel)
func (r *repository) syncLabel() string {
	h := r.head
	if h.name == "" || h.commit == "" || h.upstream == nil || (h.ahead == 0 && h.behind == 0) {
		return ""
	}
	if rem := r.remoteNamed(h.remote); rem != nil && rem.ReadOnly {
		return strconv.Itoa(h.behind) + "↓"
	}
	return strconv.Itoa(h.behind) + "↓ " + strconv.Itoa(h.ahead) + "↑"
}

// syncTooltip is the tooltip of the sync item.
func (r *repository) syncTooltip() string {
	h := r.head
	if h.name == "" || h.commit == "" || h.upstream == nil || (h.ahead == 0 && h.behind == 0) {
		return "Synchronize Changes"
	}
	if rem := r.remoteNamed(h.remote); rem != nil && rem.ReadOnly {
		return "Pull " + strconv.Itoa(h.behind) + " commits from " + h.upstream.Remote + "/" + h.upstream.Name
	}
	return "Pull " + strconv.Itoa(h.behind) + " and push " + strconv.Itoa(h.ahead) + " commits from/to " + h.upstream.Remote + "/" + h.upstream.Name
}

func (r *repository) remoteNamed(name string) *git.Remote {
	for i := range r.remotes {
		if r.remotes[i].Name == name {
			return &r.remotes[i]
		}
	}
	return nil
}

// updateStatusBar sets the items of the branch and of the sync (display only until steps 24.13 and 24.15 give them their commands).
//
// Ported from VS Code: extensions/git/src/statusbar.ts
func (r *repository) updateStatusBar() {
	if r.src == nil || r.disposed {
		return
	}
	args := []any{map[string]any{extension.SCMArgSourceControl: r.handle}}
	var items []extension.StatusCommand
	h := r.head
	if h.name != "" || h.commit != "" {
		icon := "git-commit"
		switch {
		case h.name != "":
			switch {
			case r.ops.merge || r.ops.rebaseCommit != "":
				icon = "git-branch-conflicts"
			case len(r.res.index) > 0:
				icon = "git-branch-staged-changes"
			case len(r.res.working)+len(r.res.untracked) > 0:
				icon = "git-branch-changes"
			default:
				icon = "git-branch"
			}
		case h.tag != "":
			icon = "tag"
		}
		label := r.headLabel()
		if r.ops.rebaseCommit != "" {
			label += " (Rebasing)"
		}
		items = append(items, extension.StatusCommand{Text: "$(" + icon + ") " + label, Tooltip: label + ", Checkout Branch/Tag...", Command: "git.checkout", Args: args})
	}
	if sync, ok := r.syncItem(args); ok {
		items = append(items, sync)
	}
	r.src.SetStatusBarCommands(items)
}

// syncItem is the sync item: the arrows of what is behind and ahead, "publish" when the branch has no upstream or the repository has no
// remote (git.publish asks for one, decision 430). It is absent with git.enableStatusBarSync off, on a branch with no commit yet,
// and for a detached HEAD in a repository with no remote.
//
// Ported from VS Code: extensions/git/src/statusbar.ts (SyncStatusBar.command)
func (r *repository) syncItem(args []any) (extension.StatusCommand, bool) {
	if !r.m.x.cfg.enableStatusBarSync {
		return extension.StatusCommand{}, false
	}
	h := r.head
	onBranch := h.name != "" && h.commit != ""
	switch {
	case len(r.remotes) == 0 && !onBranch:
		return extension.StatusCommand{}, false
	case !onBranch:
		return extension.StatusCommand{Text: "$(sync)", Args: args}, true
	case len(r.remotes) == 0 || h.upstream == nil:
		return extension.StatusCommand{Text: "$(cloud-upload)", Tooltip: "Publish Branch", Command: "git.publish", Args: args}, true
	}
	return extension.StatusCommand{Text: strings.TrimSpace("$(sync) " + r.syncLabel()), Tooltip: r.syncTooltip(), Command: "git.sync", Args: args}, true
}

// optimisticStage moves the resources with these paths from Changes, Untracked Changes and Merge Changes to Staged Changes before the
// confirming status arrives, and optimisticUnstage the other way (git.optimisticUpdate, SPEC §10.30): the hooks that the staging
// commands call. They change the groups at once and do nothing when the setting is off.
//
// Ported from VS Code: extensions/git/src/repository.ts (add, revert and clean: the function that gives the optimistic resource groups)
func (r *repository) optimisticStage(paths []string) {
	if !r.m.x.cfg.optimisticUpdate || r.groups.merge == nil {
		return
	}
	want := pathSet(paths)
	have := map[string]bool{}
	for _, res := range r.res.index {
		have[res.path] = true
	}
	var staged []resource
	move := func(list []resource) []resource {
		var keep []resource
		for _, res := range list {
			if !want[res.path] {
				keep = append(keep, res)
				continue
			}
			if have[res.path] {
				continue // staged already: it only leaves this group
			}
			switch res.status {
			case git.StatusUntracked:
				res.status = git.StatusIndexAdded
			case git.StatusBothDeleted, git.StatusAddedByUs, git.StatusDeletedByThem, git.StatusAddedByThem, git.StatusDeletedByUs, git.StatusBothAdded, git.StatusBothModified:
				res.status = git.StatusIndexModified
			default:
				res.status = indexStatusOf(res.status)
			}
			have[res.path] = true
			staged = append(staged, res)
		}
		return keep
	}
	g := r.res
	g.merge = move(g.merge)
	g.working = move(g.working)
	g.untracked = move(g.untracked)
	g.index = append(append([]resource(nil), g.index...), staged...)
	r.setGroups(g)
	r.updateStatusBar()
}

// optimisticUnstage is the hook of the unstage commands: Staged Changes go back to Changes; a file that was added goes back to the
// untracked files, which are in Changes ("mixed"), in Untracked Changes ("separate") or not shown ("hidden").
func (r *repository) optimisticUnstage(paths []string) {
	if !r.m.x.cfg.optimisticUpdate || r.groups.merge == nil {
		return
	}
	want := pathSet(paths)
	mode := r.m.x.cfg.untrackedChanges
	g := r.res
	g.index = nil
	for _, res := range r.res.index {
		if !want[res.path] {
			g.index = append(g.index, res)
			continue
		}
		res.status = workStatusOf(res.status)
		switch {
		case res.status != git.StatusUntracked:
			g.working = append(append([]resource(nil), g.working...), res)
		case mode == "mixed":
			g.working = append(append([]resource(nil), g.working...), res)
		case mode == "separate":
			g.untracked = append(append([]resource(nil), g.untracked...), res)
		}
	}
	r.setGroups(g)
	r.updateStatusBar()
}

// optimisticClean takes the resources with these paths out of Changes and Untracked Changes: they were discarded.
func (r *repository) optimisticClean(paths []string) {
	if !r.m.x.cfg.optimisticUpdate || r.groups.merge == nil {
		return
	}
	want := pathSet(paths)
	drop := func(list []resource) []resource {
		var keep []resource
		for _, res := range list {
			if !want[res.path] {
				keep = append(keep, res)
			}
		}
		return keep
	}
	g := r.res
	g.working = drop(g.working)
	g.untracked = drop(g.untracked)
	r.setGroups(g)
	r.updateStatusBar()
}

func pathSet(paths []string) map[string]bool {
	m := make(map[string]bool, len(paths))
	for _, p := range paths {
		m[filepath.Clean(p)] = true
	}
	return m
}

// indexStatusOf is the status a changed file has once it is staged.
func indexStatusOf(s git.Status) git.Status {
	switch s {
	case git.StatusModified, git.StatusTypeChanged:
		return git.StatusIndexModified
	case git.StatusDeleted:
		return git.StatusIndexDeleted
	case git.StatusIntentToAdd:
		return git.StatusIndexAdded
	case git.StatusIntentToRename:
		return git.StatusIndexRenamed
	}
	return s
}

// workStatusOf is the status an unstaged file has.
func workStatusOf(s git.Status) git.Status {
	switch s {
	case git.StatusIndexModified:
		return git.StatusModified
	case git.StatusIndexDeleted:
		return git.StatusDeleted
	case git.StatusIndexAdded:
		return git.StatusUntracked
	case git.StatusIndexRenamed:
		return git.StatusIntentToRename
	}
	return s
}
