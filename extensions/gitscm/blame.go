package gitscm

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm/git"
)

// Blame (SPEC §10.30 "Blame", step 24.22): who changed the line the cursor is on, as a muted text after the line (git.blame.editorDecoration.*)
// and as an item of the status bar (git.blame.statusBarItem.*). The extension follows the active editor and its cursors through the events
// of the editor, waits 150 ms after the last change of either, and blames the version of the file in HEAD with "git blame --root
// --incremental" (cached by file and HEAD). The lines of the document map to the lines of that version by a line diff (blamedata.go), so a line
// the user changed or added shows "Uncommitted changes" and the name from user.name, and every other line keeps the commit that made it.
// Nothing runs without a change of the editor, of the cursors, of HEAD or of a setting.
//
// Ported from VS Code: extensions/git/src/blame.ts, hover.ts (getCommitHover) and commands.ts (copyContentToClipboard,
// toggleBlameEditorDecoration, toggleBlameStatusBarItem).

// The ids of the blame item and of the set of annotations, and its limits.
const (
	// blameKey is the key of the set of annotations and the id of the status bar item.
	blameKey = "git.blame"
	// blameDelay is how long the cursor stays before the blame is computed.
	blameDelay = 150 * time.Millisecond
	// blameMaxFile is the largest file that is blamed.
	blameMaxFile = 2 << 20
	// blameCacheSize is how many blamed files and commits are kept.
	blameCacheSize = 100
	// blameMaxLines is how many cursor lines show a blame text; blameMaxCommits how many commits are asked for their details.
	blameMaxLines   = 100
	blameMaxCommits = 10
	// uncommittedSubject is the subject of a line that is not in HEAD.
	uncommittedSubject = "Uncommitted changes"
	// uncommittedHash is the hash of the commit that does not exist.
	uncommittedHash = "0000000000000000000000000000000000000000"
	// commandCopyContent is VS Code's command that the Copy Hash link runs.
	commandCopyContent = "git.copyContentToClipboard"
)

// blameController is the blame text of one extension.
type blameController struct {
	x *gitscm
	// loc is the time zone dates are shown in, and now the clock of the "ago" texts.
	loc *time.Location
	now func() time.Time

	// gen numbers the runs: the one that is current is the last that was asked; cancel ends its git commands, stopTimer its wait.
	gen       int
	cancel    context.CancelFunc
	stopTimer func()

	// status is the item of the status bar (nil until it is needed) and shown the uri the annotations were set for.
	status extension.StatusHandle
	shown  bool

	mu         sync.Mutex
	indexes    *blameCache[blameKeyOf, *blameIndex]
	commits    *blameCache[string, *git.Commit]
	identities map[string]blameIdentity
	logged     map[blameKeyOf]bool
}

// blameKeyOf is what a blame is cached by: the file, the commit of HEAD and the white space rule.
type blameKeyOf struct {
	root, rel, head  string
	ignoreWhitespace bool
}

// blameIdentity is the name and e-mail of the user (user.name and user.email) of a repository.
type blameIdentity struct{ name, email string }

func newBlame(x *gitscm) *blameController {
	return &blameController{
		x: x, loc: time.Local, now: time.Now,
		indexes: newBlameCache[blameKeyOf, *blameIndex](blameCacheSize), commits: newBlameCache[string, *git.Commit](blameCacheSize),
		identities: map[string]blameIdentity{}, logged: map[blameKeyOf]bool{},
	}
}

// start subscribes to the events that make the blame text change.
func (b *blameController) start() error {
	for _, topic := range []string{extension.TopicEditorSelectionChanged, extension.TopicEditorActiveChanged, extension.TopicDocumentChanged} {
		if _, err := b.x.api.Events().Subscribe(topic, func(extension.Event) { b.schedule() }); err != nil {
			return err
		}
	}
	return nil
}

// enabled says whether either of the two shows blame.
func (b *blameController) enabled() bool { return b.x.cfg.blameEditor || b.x.cfg.blameStatusBar }

// schedule asks for the blame of the cursor to be shown after blameDelay: the run that was waiting or working is cancelled, so that a fast
// sequence of changes starts one.
func (b *blameController) schedule() {
	x := b.x
	if x.stopped.Load() {
		return
	}
	b.gen++
	gen := b.gen
	b.cancelRun()
	if _, active := x.api.Documents().Active(); !b.enabled() || !active {
		b.clear() // nothing to blame: no timer is started, so an editor that is idle costs nothing
		return
	}
	b.stopTimer = x.clock.AfterFunc(blameDelay, func() { x.api.Post(func() { b.run(gen) }) })
}

// cancelRun ends the wait and the git commands of the run in progress.
func (b *blameController) cancelRun() {
	if b.stopTimer != nil {
		b.stopTimer()
		b.stopTimer = nil
	}
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
}

// settingsChanged follows a change of the settings of the blame text.
func (b *blameController) settingsChanged(old config) {
	c := b.x.cfg
	if old.blameEditor == c.blameEditor && old.blameStatusBar == c.blameStatusBar && old.blameEditorTemplate == c.blameEditorTemplate &&
		old.blameStatusBarTemplate == c.blameStatusBarTemplate && old.blameDisableHover == c.blameDisableHover &&
		old.blameIgnoreWhitespace == c.blameIgnoreWhitespace && old.commitShortHashLength == c.commitShortHashLength {
		return
	}
	b.schedule()
}

// headMoved follows the first status of a repository and a change of its HEAD: the identity is read again and the blame is made again.
func (b *blameController) headMoved(r *repository) {
	b.mu.Lock()
	delete(b.identities, r.root)
	b.mu.Unlock()
	if b.enabled() {
		b.schedule()
	}
}

// clear takes the blame text away.
func (b *blameController) clear() {
	if b.shown {
		b.x.api.EditorDecorations().Clear(blameKey)
		b.shown = false
	}
	if b.status != nil {
		b.status.SetVisible(false)
	}
}

// blameJob is what a run works on, read on the UI thread.
type blameJob struct {
	gen  int
	repo *git.Repo
	// root and rel are the repository and the file in it (with slashes), head the commit of HEAD.
	root, rel, head string
	uri, text       string
	// lines are the lines of the cursors, from 0.
	lines            []int
	ignoreWhitespace bool
	wantHover        bool
}

// lineBlame is the blame of one cursor line.
type lineBlame struct {
	line int
	info blameInfo
	// commit is what git knows of the commit, for the hover; nil for an uncommitted line or when git could not say.
	commit *git.Commit
}

// blameInfo is what is known of the commit of a line (VS Code's BlameInformation).
type blameInfo struct {
	hash, authorName, authorEmail, subject string
	authorDate                             time.Time
	uncommitted                            bool
}

// run starts the blame of the cursor now, on the UI thread, when it is still the one that was asked for.
func (b *blameController) run(gen int) {
	x := b.x
	if gen != b.gen || x.stopped.Load() {
		return
	}
	b.stopTimer = nil
	job, ok := b.jobFor(gen)
	if !ok {
		b.clear()
		return
	}
	ctx, cancel := context.WithCancel(x.ctx)
	b.cancel = cancel
	go func() {
		res := b.compute(ctx, job)
		x.api.Post(func() {
			if gen != b.gen || x.stopped.Load() {
				return
			}
			b.cancel = nil
			cancel()
			b.apply(job, res)
		})
	}()
}

// jobFor works out what to blame: the active text editor, its file in a trusted repository, and the lines of its cursors. It reports false
// when there is nothing to blame: no editor, a document that is not a file, a file outside the repositories, untracked, ignored or too large.
func (b *blameController) jobFor(gen int) (blameJob, bool) {
	x := b.x
	doc, ok := x.api.Documents().Active()
	if !ok || len(doc.Selections) == 0 {
		return blameJob{}, false
	}
	path, ok := uriPath(doc.URI)
	if !ok {
		return blameJob{}, false
	}
	r := x.model.forPath(path)
	if r == nil || r.repo == nil || !r.trusted || r.disposed || r.head.commit == "" {
		return blameJob{}, false
	}
	if res, found := r.findResource(groupUntracked, path); found && res.status == git.StatusUntracked {
		return blameJob{}, false
	}
	if res, found := r.findResource(groupWorking, path); found && res.status == git.StatusUntracked {
		return blameJob{}, false
	}
	if r.ignore != nil && r.ignore.Ignored(path, false) {
		return blameJob{}, false
	}
	if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() || info.Size() > blameMaxFile {
		return blameJob{}, false
	}
	text, err := x.api.Documents().Read(x.ctx, doc.URI)
	if err != nil || len(text) > blameMaxFile {
		return blameJob{}, false
	}
	rel, err := filepath.Rel(r.root, path)
	if err != nil || !filepath.IsLocal(rel) {
		return blameJob{}, false
	}
	job := blameJob{
		gen: gen, repo: r.repo, root: r.root, rel: filepath.ToSlash(rel), head: r.head.commit, uri: doc.URI, text: text,
		ignoreWhitespace: x.cfg.blameIgnoreWhitespace, wantHover: x.cfg.blameEditor && !x.cfg.blameDisableHover,
	}
	seen := map[int]bool{}
	for _, s := range doc.Selections {
		if l := s.Active.Line; !seen[l] && len(job.lines) < blameMaxLines {
			seen[l] = true
			job.lines = append(job.lines, l)
		}
	}
	return job, true
}

// blameResult is what a run found: nothing to show (none), or the blame of each cursor line.
type blameResult struct {
	none  bool
	lines []lineBlame
	err   error
}

// compute runs on a goroutine: the blame of the version in HEAD (from the cache, or git's), the mapping of the lines of the document to it, and
// the details of the commits for the hover.
func (b *blameController) compute(ctx context.Context, job blameJob) blameResult {
	key := blameKeyOf{root: job.root, rel: job.rel, head: job.head, ignoreWhitespace: job.ignoreWhitespace}
	idx, err := b.index(ctx, job, key)
	if err != nil {
		return blameResult{err: err}
	}
	if idx == nil {
		return blameResult{none: true}
	}
	docLines := lineHashes(job.text, job.ignoreWhitespace)
	mapping, ok := mapLines(idx.hashes, docLines)
	if !ok {
		return blameResult{none: true}
	}
	who := b.identity(ctx, job)
	var out []lineBlame
	details := 0
	for _, line := range job.lines {
		if line < 0 || line >= len(mapping) {
			continue
		}
		var lb lineBlame
		lb.line = line
		if m := mapping[line]; m < 0 {
			lb.info = blameInfo{hash: uncommittedHash, authorName: who.name, authorEmail: who.email, subject: uncommittedSubject, authorDate: b.now(), uncommitted: true}
		} else {
			c, found := idx.commitOf(m)
			if !found {
				continue
			}
			lb.info = blameInfo{hash: c.Hash, authorName: c.Author, authorEmail: c.AuthorEmail, subject: c.Summary, authorDate: c.AuthorTime}
			if job.wantHover && details < blameMaxCommits {
				details++
				lb.commit = b.commitDetails(ctx, job, c.Hash)
			}
		}
		out = append(out, lb)
	}
	return blameResult{lines: out}
}

// index is the blamed version of the file in HEAD: from the cache, or made with git blame and the text of the version. nil means that the file
// has no blame (it is not in HEAD); an error is a git that failed.
func (b *blameController) index(ctx context.Context, job blameJob, key blameKeyOf) (*blameIndex, error) {
	b.mu.Lock()
	idx, cached := b.indexes.get(key)
	b.mu.Unlock()
	if cached {
		return idx, nil
	}
	commits, err := job.repo.Blame(ctx, git.BlameOptions{Path: job.rel, Rev: job.head, IgnoreWhitespace: job.ignoreWhitespace})
	if err != nil {
		if appErr, ok := errors.FromError(err); ok && appErr.Tag == "NoPathFound" {
			b.mu.Lock()
			b.indexes.put(key, nil)
			b.mu.Unlock()
			return nil, nil
		}
		return nil, err
	}
	data, found, err := job.repo.ReadVersion(ctx, job.head, job.rel)
	if err != nil {
		return nil, err
	}
	if found && !git.LooksBinary(data) {
		idx = newBlameIndex(commits, decodeText(data), job.ignoreWhitespace)
	}
	b.mu.Lock()
	b.indexes.put(key, idx)
	b.mu.Unlock()
	return idx, nil
}

// identity is the name and e-mail of the user of the repository: git's user.name and user.email, "You" when there is no name (or git cannot
// say).
func (b *blameController) identity(ctx context.Context, job blameJob) blameIdentity {
	b.mu.Lock()
	who, ok := b.identities[job.root]
	b.mu.Unlock()
	if ok {
		return who
	}
	name, _, err := job.repo.ConfigValue(ctx, "user.name")
	if err != nil {
		return blameIdentity{name: "You"}
	}
	email, _, err := job.repo.ConfigValue(ctx, "user.email")
	if err != nil {
		return blameIdentity{name: "You"}
	}
	who = blameIdentity{name: name, email: email}
	if who.name == "" {
		who.name = "You"
	}
	b.mu.Lock()
	b.identities[job.root] = who
	b.mu.Unlock()
	return who
}

// commitDetails asks git for the message, the statistics and the co-authors of a commit, for the hover; nil when git cannot say.
func (b *blameController) commitDetails(ctx context.Context, job blameJob, hash string) *git.Commit {
	b.mu.Lock()
	c, ok := b.commits.get(hash)
	b.mu.Unlock()
	if ok {
		return c
	}
	list, err := job.repo.Log(ctx, git.LogOptions{Range: hash, MaxEntries: 1, ShortStats: true})
	if err != nil || len(list) == 0 {
		return nil
	}
	c = &list[0]
	b.mu.Lock()
	b.commits.put(hash, c)
	b.mu.Unlock()
	return c
}

// apply shows what a run found, on the UI thread.
func (b *blameController) apply(job blameJob, res blameResult) {
	x := b.x
	if res.err != nil {
		if ctxErr := x.ctx.Err(); ctxErr == nil && !errors.Is(res.err, context.Canceled) {
			key := blameKeyOf{root: job.root, rel: job.rel, head: job.head, ignoreWhitespace: job.ignoreWhitespace}
			b.mu.Lock()
			first := !b.logged[key]
			b.logged[key] = true
			b.mu.Unlock()
			if first {
				x.log.Warn("git blame failed", "file", job.rel, "err", res.err)
				x.logLine("git blame failed for " + job.rel + ": " + res.err.Error())
			}
		}
		b.clear()
		return
	}
	if res.none || len(res.lines) == 0 {
		b.clear()
		return
	}
	hashLen := x.cfg.commitShortHashLength
	if x.cfg.blameEditor {
		list := make([]extension.LineAnnotation, 0, len(res.lines))
		for _, lb := range res.lines {
			an := extension.LineAnnotation{Line: lb.line, Text: b.format(x.cfg.blameEditorTemplate, lb.info, hashLen), Color: "git.blame.editorDecorationForeground"}
			if !x.cfg.blameDisableHover && !lb.info.uncommitted {
				an.Hover = b.hover(job.uri, job.root, lb, hashLen)
			}
			list = append(list, an)
		}
		if err := x.api.EditorDecorations().Set(blameKey, job.uri, list); err != nil {
			if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "ExtensionEditorDecorationsUnavailable" {
				x.log.Warn("the blame text cannot be shown", "err", err)
			}
		} else {
			b.shown = true
		}
	} else if b.shown {
		x.api.EditorDecorations().Clear(blameKey)
		b.shown = false
	}
	b.updateStatus(job, res.lines[0], hashLen)
}

// updateStatus shows the blame of the first cursor in the status bar, or hides the item when the setting is off.
func (b *blameController) updateStatus(job blameJob, lb lineBlame, hashLen int) {
	x := b.x
	if !x.cfg.blameStatusBar {
		if b.status != nil {
			b.status.SetVisible(false)
		}
		return
	}
	item := extension.StatusItem{
		ID: blameKey, Name: "Git Blame Information", Alignment: extension.StatusRight, Priority: 200,
		Text: "$(git-commit) " + b.format(x.cfg.blameStatusBarTemplate, lb.info, hashLen), Tooltip: b.tooltip(lb, hashLen),
	}
	if !lb.info.uncommitted {
		item.Command, item.Args = "git.viewCommit", blameCommitArgs(lb.info.hash, job.uri, job.root)
	}
	if b.status == nil {
		h, err := x.api.StatusBar().Add(item)
		if err != nil {
			if appErr, ok := errors.FromError(err); !ok || appErr.Tag != "ExtensionStatusBarUnavailable" {
				x.log.Warn("the blame item cannot be added to the status bar", "err", err)
			}
			return
		}
		b.status = h
		return
	}
	b.status.Update(item)
	b.status.SetVisible(true)
}

// format fills a template with the variables of a commit.
func (b *blameController) format(template string, info blameInfo, hashLen int) string {
	date := info.authorDate
	if date.IsZero() {
		date = b.now()
	}
	return formatBlame(template, blameValues{
		hash: info.hash, hashShort: shortHash(info.hash, hashLen), subject: truncate(info.subject, blameSubjectMax),
		authorName: info.authorName, authorEmail: info.authorEmail, authorDate: dateString(date, b.loc), authorDateAgo: fromNow(date, b.now(), true),
	})
}

// tooltip is the hover of the status bar item, as plain text.
func (b *blameController) tooltip(lb lineBlame, hashLen int) string {
	info := lb.info
	if info.uncommitted {
		return "Git Blame Information\n\n" + info.subject + ": the line is not in the last commit"
	}
	var sb strings.Builder
	sb.WriteString("Git Blame Information\n\n")
	sb.WriteString(shortHash(info.hash, hashLen) + " " + info.subject + "\n")
	sb.WriteString(info.authorName)
	if info.authorEmail != "" {
		sb.WriteString(" <" + info.authorEmail + ">")
	}
	sb.WriteString(", " + fromNow(info.authorDate, b.now(), true) + " (" + longDateString(info.authorDate, b.loc) + ")")
	return sb.String()
}

// blameCommitArgs are the arguments of git.viewCommit for a commit and the file the blame is about.
func blameCommitArgs(hash, uri, root string) map[string]any {
	return map[string]any{"id": hash, "revealUri": uri, "root": root}
}

// hover is the markdown of the hover of a blame text: who, when and the message of the commit, what it changed, and the links that open it and
// copy its hash.
//
// Ported from VS Code: extensions/git/src/hover.ts (getCommitHover, appendContent, appendShortStats, appendCommands)
func (b *blameController) hover(uri, root string, lb lineBlame, hashLen int) string {
	info := lb.info
	name, email, date, message := info.authorName, info.authorEmail, info.authorDate, info.subject
	hash := info.hash
	var stats *git.ShortStat
	var co []git.CoAuthor
	if c := lb.commit; c != nil {
		hash = c.Hash
		name, email, date, message = c.AuthorName, c.AuthorEmail, c.AuthorDate, c.Message
		stats, co = c.ShortStat, c.CoAuthors
	}
	var sb strings.Builder
	if name != "" {
		if email != "" {
			sb.WriteString("[**" + escapeMarkdown(name) + "**](mailto:" + linkDestination(email) + ")")
		} else {
			sb.WriteString("**" + escapeMarkdown(name) + "**")
		}
		if !date.IsZero() {
			sb.WriteString(", " + escapeMarkdown(fromNow(date, b.now(), true)+" ("+longDateString(date, b.loc)+")"))
		}
		sb.WriteString("\n\n")
	}
	for _, c := range co {
		if c.Email != "" {
			sb.WriteString("[**" + escapeMarkdown(c.Name) + "**](mailto:" + linkDestination(c.Email) + ")")
		} else {
			sb.WriteString("**" + escapeMarkdown(c.Name) + "**")
		}
		sb.WriteString(" _(Co-author)_\n\n")
	}
	sb.WriteString(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(message, "![", "&#33;&#91;"), "\r\n", "\n"), "\r", "\n"), "\n", "\n\n"))
	sb.WriteString("\n\n---\n\n")
	if stats != nil {
		sb.WriteString(plural(stats.Files, "file", false) + " changed")
		if stats.Insertions > 0 {
			sb.WriteString(", " + plural(stats.Insertions, "insertion", false) + "(+)")
		}
		if stats.Deletions > 0 {
			sb.WriteString(", " + plural(stats.Deletions, "deletion", false) + "(-)")
		}
		sb.WriteString("\n\n---\n\n")
	}
	sb.WriteString(commandLink("Open Commit", "git.viewCommit", "Open Commit "+shortHash(hash, hashLen), blameCommitArgs(hash, uri, root)))
	sb.WriteString(" | ")
	sb.WriteString(commandLink("Copy Hash", commandCopyContent, "Copy Commit Hash", hash))
	return sb.String()
}

// commandLink is the markdown of a link that runs a command with one argument: command:<id>?<the argument in a JSON array, percent-encoded>.
func commandLink(label, command, title string, arg any) string {
	data, err := json.Marshal([]any{arg})
	if err != nil {
		return escapeMarkdown(label)
	}
	return "[" + escapeMarkdown(label) + "](command:" + command + "?" + encodeURIComponent(string(data)) + " \"" + strings.ReplaceAll(title, `"`, "'") + "\")"
}

// encodeURIComponent escapes a text as JavaScript's function does for a part of a URI (a space is %20, not +).
func encodeURIComponent(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

// linkDestination makes a text safe to use as the destination of a Markdown link: the characters that end or break it are percent-encoded.
func linkDestination(s string) string {
	return strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29", "<", "%3C", ">", "%3E", "\n", "", "\r", "").Replace(s)
}

// escapeMarkdown puts a backslash before the characters that have a meaning in Markdown (VS Code's MarkdownString.appendText).
func escapeMarkdown(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if strings.ContainsRune("\\`*_{}[]()#+-.!<>|~&", r) {
			sb.WriteByte('\\')
		}
		sb.WriteRune(r)
	}
	return sb.String()
}

// cmdCopyContent is git.copyContentToClipboard: a string that the Copy Hash link of the hover gives goes to the clipboard.
func (x *gitscm) cmdCopyContent(_ context.Context, args any) error {
	if text, ok := args.(string); ok {
		if err := x.api.Clipboard().WriteText(text); err != nil {
			x.log.Warn("cannot copy to the clipboard", "err", err)
			x.api.Notifications().Error("Cannot copy to the clipboard: " + describe(err))
		}
	}
	return nil
}

// cmdToggleBlameEditor is git.blame.toggleEditorDecoration.
func (x *gitscm) cmdToggleBlameEditor(context.Context, any) error {
	x.saveSetting("git.blame.editorDecoration.enabled", !x.cfg.blameEditor)
	return nil
}

// cmdToggleBlameStatusBar is git.blame.toggleStatusBarItem.
func (x *gitscm) cmdToggleBlameStatusBar(context.Context, any) error {
	x.saveSetting("git.blame.statusBarItem.enabled", !x.cfg.blameStatusBar)
	return nil
}

// blameCache is a small cache that drops the least recently used entry when it is full. The caller locks.
type blameCache[K comparable, V any] struct {
	size  int
	items map[K]V
	order []K
}

func newBlameCache[K comparable, V any](size int) *blameCache[K, V] {
	return &blameCache[K, V]{size: size, items: map[K]V{}}
}

func (l *blameCache[K, V]) get(k K) (V, bool) {
	v, ok := l.items[k]
	if ok {
		l.touch(k)
	}
	return v, ok
}

func (l *blameCache[K, V]) put(k K, v V) {
	if _, ok := l.items[k]; !ok && len(l.items) >= l.size && len(l.order) > 0 {
		delete(l.items, l.order[0])
		l.order = l.order[1:]
	}
	l.items[k] = v
	l.touch(k)
}

func (l *blameCache[K, V]) touch(k K) {
	for i, o := range l.order {
		if o == k {
			l.order = append(l.order[:i], l.order[i+1:]...)
			break
		}
	}
	l.order = append(l.order, k)
}
