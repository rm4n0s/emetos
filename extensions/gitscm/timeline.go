package gitscm

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// The Timeline of a file (SPEC §10.30 "The Timeline view", step 24.21): the provider "Git History" gives the Timeline view the commits that
// changed the file in front (git log --follow, so a rename does not end the history), in pages, and in the first page the staged changes of
// the file and, with git.timeline.showUncommitted, its changes in the work tree. The view draws them; the commands of this file are the
// ones of its menus: Open Changes, Open Commit, Select for Compare and Compare with Selected, Copy Commit Hash and Copy Commit Message.
//
// Ported from VS Code: extensions/git/src/timelineProvider.ts and commands.ts (the git.timeline.* commands). Not built: the timeline of a
// folder and the workspace timeline (the view shows files, SPEC §10.30).

// TimelineProviderID is the source of the items: "git-history", as in VS Code.
const TimelineProviderID = "git-history"

// timelineDebounce is how long the changes of a repository are gathered before the Timeline is told (VS Code's fireChanged).
const timelineDebounce = 500 * time.Millisecond

// The values of the context key timelineItem of the items.
const (
	timelineCommit  = "git:file:commit"
	timelineIndex   = "git:file:index"
	timelineWorking = "git:file:working"
)

// The ids of the two items that are not commits.
const (
	timelineIndexID   = "index"
	timelineWorkingID = "working"
)

// timelineRepo is what the provider needs of a repository: copied on the UI thread after each status and read from any goroutine.
type timelineRepo struct {
	id, root string
	repo     *git.Repo
	// index and work are the files of the Staged Changes and of the Changes (the work tree) with their status.
	index, work map[string]git.Status
}

// timelineSettings are git.timeline.date, .showAuthor and .showUncommitted, copied for the provider.
type timelineSettings struct {
	authored        bool
	showAuthor      bool
	showUncommitted bool
	shortHash       int
}

// timelineState is what the extension keeps for the Timeline.
type timelineState struct {
	mu       sync.Mutex
	repos    map[string]*timelineRepo
	settings timelineSettings
	reg      extension.TimelineRegistration
	// failed says that registering the provider was refused for a reason that is not a missing Timeline view, so that it is not tried
	// again for each repository.
	failed bool
	// asked says that the Timeline view has asked the provider once: before that there is nothing to tell it (it asks when it shows a file),
	// and no timer is made for it.
	asked atomic.Bool
	// stopFire cancels the debounced call of Changed (UI thread).
	stopFire func()
	// selected is the item Select for Compare chose (UI thread).
	selected *timelineSelected
}

// timelineSelected is the item chosen with Select for Compare.
type timelineSelected struct {
	uri  string
	item timelineItemData
}

func (t *timelineState) set(tr *timelineRepo) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.repos == nil {
		t.repos = map[string]*timelineRepo{}
	}
	t.repos[tr.root] = tr
}

func (t *timelineState) remove(root string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.repos, root)
}

// forPath is the repository a file is in, the innermost one.
func (t *timelineState) forPath(p string) *timelineRepo {
	t.mu.Lock()
	defer t.mu.Unlock()
	var best *timelineRepo
	for _, tr := range t.repos {
		if git.Within(p, tr.root) && (best == nil || len(tr.root) > len(best.root)) {
			best = tr
		}
	}
	return best
}

func (t *timelineState) config() timelineSettings {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.settings
}

// loadTimelineSettings copies the settings the provider reads.
func (x *gitscm) loadTimelineSettings() {
	s := timelineSettings{
		authored:        x.cfg().timelineDate == "authored",
		showAuthor:      x.cfg().timelineShowAuthor,
		showUncommitted: x.cfg().timelineShowUncommitted,
		shortHash:       x.cfg().commitShortHashLength,
	}
	x.tl.mu.Lock()
	changed := x.tl.settings != s
	x.tl.settings = s
	x.tl.mu.Unlock()
	if changed {
		x.timelineFire()
	}
}

// ensureTimeline registers the provider when the first repository is opened (VS Code does the same). A host with no Timeline view has no
// provider, which is not worth a log line.
func (x *gitscm) ensureTimeline() {
	if x.tl.reg != nil || x.tl.failed {
		return
	}
	reg, err := x.api.Timeline().RegisterProvider(extension.TimelineProvider{
		ID: TimelineProviderID, Label: "Git History", Schemes: []string{"file"}, Provide: x.timelineProvide,
	})
	if err != nil {
		if tag := errTag(err); tag != "ExtensionTimelineUnavailable" && tag != "ExtensionDeactivated" {
			x.log.Warn("cannot register the timeline provider", "err", err)
		}
		x.tl.failed = true
		return
	}
	x.tl.reg = reg
}

// timelineFire tells the Timeline, after a pause, that the history of the files may have changed.
func (x *gitscm) timelineFire() {
	if x.tl.reg == nil || x.stopped.Load() || !x.tl.asked.Load() {
		return
	}
	if x.tl.stopFire != nil {
		x.tl.stopFire()
	}
	reg := x.tl.reg
	x.tl.stopFire = x.clock.AfterFunc(timelineDebounce, func() {
		x.api.Post(func() {
			if !x.stopped.Load() {
				reg.Changed()
			}
		})
	})
}

// timelineSync is called after each status of the repository, on the UI thread: it copies what the provider needs and tells the Timeline
// when the history of a file may have changed: HEAD moved, the staged files changed, or, with git.timeline.showUncommitted, the files
// of the work tree did.
func (r *repository) timelineSync(headMoved bool) {
	x := r.m.x
	if r.repo == nil {
		return
	}
	tr := &timelineRepo{id: r.id, root: r.root, repo: r.repo, index: map[string]git.Status{}, work: map[string]git.Status{}}
	for _, res := range r.res.index {
		tr.index[res.path] = res.status
	}
	for _, res := range r.res.working {
		tr.work[res.path] = res.status
	}
	for _, res := range r.res.untracked {
		tr.work[res.path] = res.status
	}
	x.tl.mu.Lock()
	prev := x.tl.repos[r.root]
	x.tl.mu.Unlock()
	x.tl.set(tr)
	changed := headMoved || prev == nil || !sameStatuses(prev.index, tr.index) ||
		(x.tl.config().showUncommitted && !sameStatuses(prev.work, tr.work))
	if changed {
		x.timelineFire()
	}
}

// timelineGone forgets a repository that was closed or that is not trusted any more.
func (r *repository) timelineGone() {
	r.m.x.tl.remove(r.root)
	r.m.x.timelineFire()
}

func sameStatuses(a, b map[string]git.Status) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// timelineItemData is what the commands of an item know about it: the versions of the file the item compares.
type timelineItemData struct {
	// Root is the root of the repository and Ref the commit of the item ("" for the file in the work tree and "~" for the index).
	Root, Ref string
	// PreviousRef is what the item is compared with: the commit before it in the history of the file, the empty tree for the first
	// one, "HEAD" or "~" for the two items of the changes.
	PreviousRef string
	// Path and PreviousPath are the paths the file had in Ref and in PreviousRef (absolute), which differ across a rename.
	Path, PreviousPath string
	// Message is the whole message of a commit.
	Message string
}

func (d timelineItemData) args() map[string]any {
	return map[string]any{"root": d.Root, "ref": d.Ref, "previousRef": d.PreviousRef, "path": d.Path, "previousPath": d.PreviousPath, "message": d.Message}
}

func timelineDataOf(v any) (timelineItemData, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return timelineItemData{}, false
	}
	str := func(k string) string { s, _ := m[k].(string); return s }
	d := timelineItemData{Root: str("root"), Ref: str("ref"), PreviousRef: str("previousRef"), Path: str("path"), PreviousPath: str("previousPath"), Message: str("message")}
	return d, d.Root != "" && d.Path != ""
}

// timelineCursor makes the cursor of the next page: the commit it starts at and the path the file had there, so that the log can go on
// across a rename (the new name of the file is not in the commits before the rename).
func timelineCursor(hash, path string) string { return hash + ":" + path }

func splitTimelineCursor(c string) (hash, path string) {
	hash, path, _ = strings.Cut(c, ":")
	return hash, path
}

// timelineProvide is the Provide of the provider: a page of the history of a file. It runs off the UI thread.
func (x *gitscm) timelineProvide(ctx context.Context, uri string, opts extension.TimelineOptions) (extension.Timeline, error) {
	x.tl.asked.Store(true)
	p, ok := uriPath(uri)
	if !ok {
		return extension.Timeline{}, nil
	}
	tr := x.tl.forPath(p)
	if tr == nil {
		return extension.Timeline{}, nil
	}
	if info, err := os.Stat(p); err == nil && info.IsDir() {
		return extension.Timeline{}, nil
	}
	cfg := x.tl.config()
	path := p
	cursorHash, cursorPath := splitTimelineCursor(opts.Cursor)
	if cursorPath != "" {
		path = filepath.Join(tr.root, filepath.FromSlash(cursorPath))
	}
	// one more than the page, to know whether there is another and what the last item is compared with
	limit := 0
	if opts.Limit > 0 {
		limit = opts.Limit + 1
	}
	commits, err := tr.repo.LogFile(ctx, git.LogFileOptions{Path: path, Hash: cursorHash, MaxEntries: limit, Follow: true, ShortStats: true})
	if err != nil {
		return extension.Timeline{}, err
	}
	var out extension.Timeline
	shown := commits
	if limit > 0 && len(commits) >= limit {
		extra := commits[len(commits)-1]
		out.Paging.Cursor = timelineCursor(extra.Hash, relSlash(tr.root, pathAt(tr.root, extra.FilePath, path)))
		shown = commits[:len(commits)-1]
	}
	emptyTree := ""
	for i, c := range shown {
		here := pathAt(tr.root, c.FilePath, path)
		prevRef, prevPath := "", here
		if i+1 < len(commits) {
			prev := commits[i+1]
			prevRef, prevPath = prev.Hash, pathAt(tr.root, prev.FilePath, path)
		} else {
			if emptyTree == "" {
				if emptyTree, err = tr.repo.EmptyTree(ctx); err != nil {
					return extension.Timeline{}, err
				}
			}
			prevRef = emptyTree
		}
		data := timelineItemData{Root: tr.root, Ref: c.Hash, PreviousRef: prevRef, Path: here, PreviousPath: prevPath, Message: c.Message}
		date := c.CommitDate
		if cfg.authored {
			date = c.AuthorDate
		}
		subject, _, _ := strings.Cut(c.Message, "\n")
		item := extension.TimelineItem{
			ID: c.Hash, Label: subject, Timestamp: date, Icon: "git-commit", ContextValue: timelineCommit, Args: data.args(),
			Detail:  commitHover(c.AuthorName, c.AuthorEmail, date, c.Message, c.ShortStat),
			Command: &extension.Command{ID: "git.timeline.openDiff", Title: "Open Comparison"},
		}
		if cfg.showAuthor {
			item.Description = c.AuthorName
		}
		out.Items = append(out.Items, item)
	}
	if opts.Cursor == "" {
		out.Items = append(x.timelineChangeItems(tr, p, cfg), out.Items...)
	}
	return out, nil
}

// pathAt is the absolute path a file had in a commit: the path the log read (relative), or def when it did not.
func pathAt(root, logged, def string) string {
	if logged == "" {
		return def
	}
	return filepath.Join(root, filepath.FromSlash(logged))
}

// timelineChangeItems are the items of the first page that are not commits: the staged changes of the file and, with
// git.timeline.showUncommitted, its changes in the work tree, the newest first.
func (x *gitscm) timelineChangeItems(tr *timelineRepo, p string, cfg timelineSettings) []extension.TimelineItem {
	var items []extension.TimelineItem
	indexStatus, staged := tr.index[p]
	if staged {
		data := timelineItemData{Root: tr.root, Ref: refWorkTree, PreviousRef: refHEAD, Path: p, PreviousPath: p}
		items = append(items, extension.TimelineItem{
			ID: timelineIndexID, Label: "Staged Changes", Timestamp: modTime(filepath.Join(gitDirOf(tr), "index")), Icon: "git-commit",
			ContextValue: timelineIndex, Args: data.args(), Detail: changeHover(statusText(indexStatus)),
			Command: &extension.Command{ID: "git.timeline.openDiff", Title: "Open Comparison"},
		})
	}
	if workStatus, ok := tr.work[p]; ok && cfg.showUncommitted {
		prev := refHEAD
		if staged {
			prev = refWorkTree
		}
		data := timelineItemData{Root: tr.root, Ref: "", PreviousRef: prev, Path: p, PreviousPath: p}
		items = append([]extension.TimelineItem{{
			ID: timelineWorkingID, Label: "Uncommitted Changes", Timestamp: modTime(p), Icon: "circle-outline",
			ContextValue: timelineWorking, Args: data.args(), Detail: changeHover(statusText(workStatus)),
			Command: &extension.Command{ID: "git.timeline.openDiff", Title: "Open Comparison"},
		}}, items...)
	}
	return items
}

// gitDirOf is the git directory of the repository, where the index is.
func gitDirOf(tr *timelineRepo) string {
	if tr.repo != nil && tr.repo.Info.GitDir != "" {
		return tr.repo.Info.GitDir
	}
	return filepath.Join(tr.root, ".git")
}

// modTime is the time a file was last written, now when it cannot be read.
func modTime(p string) time.Time {
	if info, err := os.Stat(p); err == nil {
		return info.ModTime()
	}
	return time.Now()
}

// commitHover is the tooltip of a commit: the author with the e-mail and the date, the message, and the stat.
//
// Ported from VS Code: extensions/git/src/hover.ts (getCommitHover)
func commitHover(name, email string, date time.Time, message string, stat *git.ShortStat) string {
	var b strings.Builder
	if name != "" {
		if email != "" {
			b.WriteString("[**" + escapeTimelineText(name) + "**](mailto:" + email + ")")
		} else {
			b.WriteString("**" + escapeTimelineText(name) + "**")
		}
		if !date.IsZero() {
			b.WriteString(", " + date.Local().Format("January 2, 2006 at 3:04 PM"))
		}
		b.WriteString("\n\n")
	}
	message = strings.ReplaceAll(message, "![", "&#33;&#91;")
	b.WriteString(strings.Join(strings.Split(strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(message), "\n"), "\n\n"))
	if stat != nil {
		b.WriteString("\n\n---\n\n" + timelinePlural(stat.Files, "file changed", "files changed"))
		if stat.Insertions > 0 {
			b.WriteString(", " + timelinePlural(stat.Insertions, "insertion(+)", "insertions(+)"))
		}
		if stat.Deletions > 0 {
			b.WriteString(", " + timelinePlural(stat.Deletions, "deletion(-)", "deletions(-)"))
		}
	}
	return b.String()
}

// changeHover is the tooltip of the staged or the uncommitted item.
func changeHover(status string) string { return "**You**\n\n" + status }

func timelinePlural(n int, one, many string) string {
	if n == 1 {
		return strconv.Itoa(n) + " " + one
	}
	return strconv.Itoa(n) + " " + many
}

// escapeTimelineText escapes the characters that Markdown reads in a name.
func escapeTimelineText(s string) string {
	return strings.NewReplacer(`\`, `\\`, "`", "\\`", "*", `\*`, "_", `\_`, "[", `\[`, "]", `\]`, "<", `\<`, ">", `\>`, "#", `\#`).Replace(s)
}

// shortRef is the first characters of a commit, as many as git.commitShortHashLength (7), which VS Code's timeline makes 8; "~", "HEAD"
// and "" stay as they are.
func (x *gitscm) shortRef(ref string) string {
	if ref == "" || ref == refWorkTree || ref == refHEAD {
		return ref
	}
	n := x.tl.config().shortHash
	if n <= 0 {
		n = 7
	}
	if len(ref) > n {
		ref = ref[:n]
	}
	return ref
}

// timelineLabel is how a version is named in a title: the file in the work tree, the index, or a commit.
func (x *gitscm) timelineLabel(base string, d timelineItemData, withPrevious bool) string {
	switch {
	case (d.PreviousRef == refHEAD || d.PreviousRef == refWorkTree) && d.Ref == "":
		return base + " (Working Tree)"
	case d.PreviousRef == refHEAD && d.Ref == refWorkTree:
		return base + " (Index)"
	}
	if withPrevious {
		return base + " (" + x.shortRef(d.PreviousRef) + ") ↔ " + base + " (" + x.shortRef(d.Ref) + ")"
	}
	return base + " (" + x.shortRef(d.Ref) + ")"
}

// versionURI is the document of a version of a file: the file itself for the work tree, else the provided document of the ref.
func versionURI(tr *timelineRepo, path, ref string) string {
	if ref == "" {
		return "file://" + path
	}
	return toGitURI(tr.id, tr.root, path, ref)
}

// timelineItemOf reads the item a command of the Timeline menus is for: the first argument.
func (x *gitscm) timelineItemOf(args any) (a extension.TimelineArg, d timelineItemData, tr *timelineRepo, ok bool) {
	list := extension.TimelineArgs(args)
	if len(list) == 0 || list[0].Source != TimelineProviderID {
		return a, d, nil, false
	}
	a = list[0]
	d, ok = timelineDataOf(a.Args)
	if !ok {
		return a, d, nil, false
	}
	tr = x.tl.forPath(d.Path)
	if tr == nil || tr.root != d.Root {
		return a, d, nil, false
	}
	return a, d, tr, true
}

// cmdTimelineOpenDiff opens the file at the commit of the item against the version it is compared with: the commit before it in the history
// of the file; the staged item against HEAD and the uncommitted one against the index.
func (x *gitscm) cmdTimelineOpenDiff(_ context.Context, args any) error {
	_, d, tr, ok := x.timelineItemOf(args)
	if !ok {
		return nil
	}
	base := filepath.Base(d.Path)
	err := x.api.Documents().OpenDiff(extension.DiffSpec{
		Left: versionURI(tr, d.PreviousPath, d.PreviousRef), Right: versionURI(tr, d.Path, d.Ref), Title: x.timelineLabel(base, d, true),
		Preview: true, PreserveFocus: true,
	})
	return x.documentsError(err)
}

// documentsError is nil for the errors of a scope that cannot show documents (a host with no workbench), which are not worth a log line.
func (x *gitscm) documentsError(err error) error {
	if err == nil || errTag(err) == "ExtensionDocumentUnavailable" || errTag(err) == "ExtensionDeactivated" {
		return nil
	}
	return err
}

// cmdTimelineViewCommit opens what the commit of the item changed in the multi-file diff editor, scrolled to the file.
func (x *gitscm) cmdTimelineViewCommit(_ context.Context, args any) error {
	a, d, tr, ok := x.timelineItemOf(args)
	if !ok || a.ContextValue != timelineCommit {
		return nil
	}
	if r := x.model.byRoot(tr.root); r != nil {
		x.openCommit(r, d.Ref, "file://"+d.Path)
	}
	return nil
}

// cmdTimelineCopyCommitID puts the hash of the commit of the item on the clipboard.
func (x *gitscm) cmdTimelineCopyCommitID(_ context.Context, args any) error {
	a, d, _, ok := x.timelineItemOf(args)
	if !ok || a.ContextValue != timelineCommit {
		return nil
	}
	return x.clipboard(d.Ref)
}

// cmdTimelineCopyCommitMessage puts the message of the commit of the item on the clipboard.
func (x *gitscm) cmdTimelineCopyCommitMessage(_ context.Context, args any) error {
	a, d, _, ok := x.timelineItemOf(args)
	if !ok || a.ContextValue != timelineCommit {
		return nil
	}
	return x.clipboard(d.Message)
}

// clipboard writes a text; a scope with no clipboard is not worth a log line.
func (x *gitscm) clipboard(text string) error {
	err := x.api.Clipboard().WriteText(text)
	if tag := errTag(err); err == nil || tag == "ExtensionClipboardUnavailable" || tag == "ExtensionDeactivated" {
		return nil
	}
	return err
}

// cmdTimelineSelectForCompare remembers the item and sets the context key that shows Compare with Selected.
func (x *gitscm) cmdTimelineSelectForCompare(_ context.Context, args any) error {
	a, d, _, ok := x.timelineItemOf(args)
	if !ok || a.URI == "" {
		return nil
	}
	x.tl.selected = &timelineSelected{uri: a.URI, item: d}
	if err := x.api.Context().Set("git.timeline.selectedForCompare", true); err != nil {
		x.log.Debug("cannot set a context key", "key", "git.timeline.selectedForCompare", "err", err)
	}
	return nil
}

// cmdTimelineCompareWithSelected opens the two versions of the file, the selected one on the left, in a diff named
// "a.go (abc1234) ↔ a.go (def5678)". It does nothing for an item of another file.
func (x *gitscm) cmdTimelineCompareWithSelected(_ context.Context, args any) error {
	a, d, tr, ok := x.timelineItemOf(args)
	sel := x.tl.selected
	if !ok || a.URI == "" || sel == nil || sel.uri != a.URI {
		return nil
	}
	base := filepath.Base(d.Path)
	title := x.timelineLabel(base, sel.item, false) + " ↔ " + x.timelineLabel(base, d, false)
	err := x.api.Documents().OpenDiff(extension.DiffSpec{
		Left: versionURI(tr, sel.item.Path, sel.item.Ref), Right: versionURI(tr, d.Path, d.Ref), Title: title,
		Preview: true, PreserveFocus: false,
	})
	return x.documentsError(err)
}

// timelineHandlers are the handlers of the commands of the Timeline.
func (x *gitscm) timelineHandlers() map[string]extension.CommandHandler {
	return map[string]extension.CommandHandler{
		"git.timeline.openDiff":            x.cmdTimelineOpenDiff,
		"git.timeline.viewCommit":          x.cmdTimelineViewCommit,
		"git.timeline.copyCommitId":        x.cmdTimelineCopyCommitID,
		"git.timeline.copyCommitMessage":   x.cmdTimelineCopyCommitMessage,
		"git.timeline.selectForCompare":    x.cmdTimelineSelectForCompare,
		"git.timeline.compareWithSelected": x.cmdTimelineCompareWithSelected,
	}
}

// timelineMenus are the rows of the menu of an item of the Timeline.
//
// Ported from VS Code: extensions/git/package.json (contributes.menus: timeline/item/context)
func timelineMenus() []extension.MenuItem {
	menu := extension.MenuTimelineItemContext
	const on = "config.git.enabled && !git.missing && "
	const commit = `timelineItem =~ /git:(file|folder|workspace):commit\b/ && !listMultiSelection`
	const file = `timelineItem =~ /git:file\b/ && !listMultiSelection`
	return []extension.MenuItem{
		item(menu, "git.timeline.viewCommit", "inline", on+commit),
		item(menu, "git.timeline.openDiff", "1_actions@1", on+file),
		item(menu, "git.timeline.viewCommit", "1_actions@2", on+commit),
		item(menu, "git.timeline.compareWithSelected", "3_compare@1", on+"git.timeline.selectedForCompare && "+file),
		item(menu, "git.timeline.selectForCompare", "3_compare@2", on+file),
		item(menu, "git.timeline.copyCommitId", "5_copy@1", on+commit),
		item(menu, "git.timeline.copyCommitMessage", "5_copy@2", on+commit),
	}
}
