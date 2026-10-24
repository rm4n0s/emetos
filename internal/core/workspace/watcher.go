package workspace

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/fileio"
	"github.com/rm4n0s/emetos/internal/core/glob"
)

// Default timings of the watcher.
const (
	// DefaultDebounce is how long the watcher gathers events before it reports them (VS Code's
	// throttle of its watcher is also 50 ms).
	DefaultDebounce = 50 * time.Millisecond
	// DefaultPollInterval is the time between two scans of a polled folder.
	DefaultPollInterval = 2 * time.Second
	// maxPolledEntries bounds what a scan of a polled folder reads.
	maxPolledEntries = 500_000
	// maxOwnWrites bounds the writes of the editor that the watcher remembers.
	maxOwnWrites = 256
)

// Clock is the timer of the debounce. Tests replace it with a fake one.
type Clock interface {
	// AfterFunc calls f in its own goroutine after d, like time.AfterFunc.
	AfterFunc(d time.Duration, f func()) StopTimer
}

// StopTimer is a timer that can be stopped.
type StopTimer interface {
	// Stop prevents the call; it reports whether it did.
	Stop() bool
}

type realClock struct{}

func (realClock) AfterFunc(d time.Duration, f func()) StopTimer { return time.AfterFunc(d, f) }

// NoticeKind says why a notice was sent.
type NoticeKind int

const (
	// NoticePollingFallback says that inotify cannot be used at all and the watcher polls.
	NoticePollingFallback NoticeKind = iota + 1
	// NoticeWatchLimit says that the limit of inotify watches (fs.inotify.max_user_watches) was
	// reached and a folder is polled instead.
	NoticeWatchLimit
)

// Notice is something the user should hear of once: the watcher works, but not as well as it
// should.
type Notice struct {
	Kind NoticeKind
	// Path is the folder the notice is about.
	Path string
	// Message is a sentence for the user.
	Message string
	// Err is the error behind it, when there is one.
	Err error
}

// WatcherOptions say how a Watcher works.
type WatcherOptions struct {
	// OnEvents gets each batch of events. It is called from one goroutine of the watcher, one
	// batch at a time, so it must not block for long.
	OnEvents func(events []Event)
	// OnNotice gets the notices; each kind is sent once.
	OnNotice func(Notice)
	// Logger is where the watcher logs; slog.Default() when nil.
	Logger *slog.Logger
	// Clock times the debounce; the real one when nil.
	Clock Clock
	// Debounce is the time events are gathered for; DefaultDebounce when zero.
	Debounce time.Duration
	// PollInterval is the time between scans of the folders that are polled; DefaultPollInterval
	// when zero.
	PollInterval time.Duration
	// ForcePolling makes the watcher poll every folder, as it does when inotify cannot be used.
	ForcePolling bool

	// inotifyInit and inotifyAdd replace the system calls, for tests.
	inotifyInit func(flags int) (int, error)
	inotifyAdd  func(fd int, path string, mask uint32) (int, error)
}

// Target is a path to watch.
type Target struct {
	// Path is an absolute path of a folder, or of a file when File is set.
	Path string
	// File makes Path a file: the folder it is in is watched (not recursively, so that a file
	// replaced by renaming another one over it is seen) and only the events of the file are
	// reported. It is how the files that are open outside the folder of the window are followed.
	File bool
	// Recursive watches the folder and everything below it, as it grows; links to folders are
	// not followed (list them in files.watcherInclude). Ignored when File is set.
	Recursive bool
	// Exclude are the paths and globs of files.watcherExclude: they are matched against the path
	// relative to Path (an absolute pattern against the whole path), and what they match is not
	// watched and gives no events.
	Exclude []string
}

type excludePattern struct {
	abs  string
	glob *glob.Matcher
}

// root is a target that was added.
type root struct {
	path      string
	files     map[string]bool
	recursive bool
	exclude   []excludePattern

	polling bool

	mu      sync.Mutex // guards snap and removed
	snap    map[string]polled
	removed bool
}

// accepts tells whether the root reports the events of a path.
func (r *root) accepts(path string) bool {
	if r.files != nil {
		return r.files[path]
	}
	return !r.excluded(path)
}

// excluded tells whether files.watcherExclude covers a path.
func (r *root) excluded(path string) bool {
	if len(r.exclude) == 0 {
		return false
	}
	rel, err := filepath.Rel(r.path, path)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	for _, ex := range r.exclude {
		if ex.abs != "" {
			if path == ex.abs || isParentPath(path, ex.abs) {
				return true
			}
			continue
		}
		if rel != "." && !strings.HasPrefix(rel, "../") && ex.glob.Match(rel) {
			return true
		}
	}
	return false
}

// Watcher reports the changes of files: it watches folders with inotify and polls the ones it
// cannot watch that way (SPEC §10.5). It is made with NewWatcher, which starts it, and stops
// when its context ends.
type Watcher struct {
	opts  WatcherOptions
	log   *slog.Logger
	clock Clock
	ctx   context.Context

	raw  chan rawEvent
	done chan struct{}
	wg   sync.WaitGroup

	mu       sync.Mutex
	roots    map[*root]struct{}
	ino      *inotify
	closed   bool
	notified map[NoticeKind]bool

	ownMu    sync.Mutex
	own      map[string]fileio.FileStat
	ownOrder []string
}

// NewWatcher starts a watcher that runs until ctx ends. If inotify cannot be initialised, it
// polls instead and sends a NoticePollingFallback notice.
func NewWatcher(ctx context.Context, opts WatcherOptions) *Watcher {
	w := &Watcher{
		opts: opts, log: opts.Logger, clock: opts.Clock, ctx: ctx,
		raw: make(chan rawEvent, 1024), done: make(chan struct{}),
		roots: map[*root]struct{}{}, notified: map[NoticeKind]bool{}, own: map[string]fileio.FileStat{},
	}
	if w.log == nil {
		w.log = slog.Default()
	}
	if w.clock == nil {
		w.clock = realClock{}
	}
	if w.opts.Debounce <= 0 {
		w.opts.Debounce = DefaultDebounce
	}
	if w.opts.PollInterval <= 0 {
		w.opts.PollInterval = DefaultPollInterval
	}
	if !opts.ForcePolling {
		ino, err := newInotify(opts.inotifyInit, opts.inotifyAdd)
		if err != nil {
			w.log.Warn("inotify is not available, the watcher polls", "err", err)
			w.sendNotice(Notice{Kind: NoticePollingFallback, Err: err,
				Message: "Emetos cannot watch files for changes, so it checks for them every few seconds instead."})
		} else {
			w.ino = ino
		}
	}
	w.wg.Add(3)
	go w.batchLoop()
	go w.pollLoop()
	go func() {
		defer w.wg.Done()
		<-ctx.Done()
		w.mu.Lock()
		w.closed = true
		if w.ino != nil {
			w.ino.close()
		}
		w.mu.Unlock()
	}()
	if w.ino != nil {
		w.wg.Add(1)
		go w.readLoop()
	}
	go func() {
		w.wg.Wait()
		close(w.done)
	}()
	return w
}

// Done is closed when the watcher has stopped and all its goroutines are gone.
func (w *Watcher) Done() <-chan struct{} { return w.done }

// sendNotice sends a notice unless one of its kind was sent already.
func (w *Watcher) sendNotice(n Notice) {
	w.mu.Lock()
	if w.notified[n.Kind] {
		w.mu.Unlock()
		return
	}
	w.notified[n.Kind] = true
	w.mu.Unlock()
	if w.opts.OnNotice != nil {
		w.opts.OnNotice(n)
	}
}

// Watch starts watching a target and returns the function that stops it. It reads the folder
// (and, for a recursive target, everything below it) to place its watches before it returns,
// so call it from a goroutine of the application, not from the UI thread. It fails with the
// tags WatcherPathInvalid, WatcherPathNotFound, WatcherStatFailed, WatcherStopped and
// WatcherAddFailed (the folder itself cannot be watched; the folders below it that cannot are
// left out).
func (w *Watcher) Watch(ctx context.Context, t Target) (unwatch func(), err error) {
	if ctx.Err() != nil {
		return nil, errors.New("WatcherStopped", "the watcher has stopped or the call was cancelled", "path", t.Path)
	}
	if !filepath.IsAbs(t.Path) {
		return nil, errors.New("WatcherPathInvalid", "the path to watch is not absolute", "path", t.Path)
	}
	r := &root{path: filepath.Clean(t.Path), recursive: t.Recursive && !t.File}
	if t.File {
		r.path = filepath.Dir(r.path)
		r.files = map[string]bool{filepath.Clean(t.Path): true}
	}
	info, err := os.Stat(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.NewErr("WatcherPathNotFound", err).SetMetadata("path", r.path)
		}
		return nil, errors.NewErr("WatcherStatFailed", err).SetMetadata("path", r.path)
	}
	if !info.IsDir() {
		return nil, errors.New("WatcherPathInvalid", "the path to watch is not a folder", "path", r.path)
	}
	for _, p := range t.Exclude {
		if p == "" {
			continue
		}
		if filepath.IsAbs(p) {
			r.exclude = append(r.exclude, excludePattern{abs: filepath.Clean(p)})
			continue
		}
		r.exclude = append(r.exclude, excludePattern{glob: glob.Parse(p, glob.Options{TrimForExclusions: true})})
	}

	w.mu.Lock()
	if w.closed {
		w.mu.Unlock()
		return nil, errors.New("WatcherStopped", "the watcher has stopped", "path", r.path)
	}
	w.roots[r] = struct{}{}
	var out []rawEvent
	switch {
	case w.opts.ForcePolling || w.ino == nil:
		r.polling = true
	default:
		if err := w.addRootLocked(r, &out); err != nil {
			delete(w.roots, r)
			w.mu.Unlock()
			return nil, err
		}
	}
	polling := r.polling
	w.mu.Unlock()
	if polling {
		w.baseline(r)
	}
	w.send(out)
	return func() { w.unwatch(r) }, nil
}

// unwatch stops a root.
func (w *Watcher) unwatch(r *root) {
	r.mu.Lock()
	r.removed = true
	r.mu.Unlock()
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.roots, r)
	if w.ino != nil && !w.closed {
		w.ino.dropRoot(r)
	}
}

// send hands raw events to the batcher.
func (w *Watcher) send(events []rawEvent) {
	for _, e := range events {
		select {
		case w.raw <- e:
		case <-w.ctx.Done():
			return
		}
	}
}

// batchLoop gathers the raw events for the debounce time and reports them, coalesced.
func (w *Watcher) batchLoop() {
	defer w.wg.Done()
	co := newCoalescer()
	flush := make(chan struct{}, 1)
	var timer StopTimer
	for {
		select {
		case <-w.ctx.Done():
			if timer != nil {
				timer.Stop()
			}
			return
		case r := <-w.raw:
			co.add(r)
			if timer == nil {
				timer = w.clock.AfterFunc(w.opts.Debounce, func() {
					select {
					case flush <- struct{}{}:
					default:
					}
				})
			}
		case <-flush:
			timer = nil
			events := w.dropOwn(co.flush())
			if len(events) > 0 && w.opts.OnEvents != nil {
				w.opts.OnEvents(events)
			}
		}
	}
}

// Suppress tells the watcher that the editor wrote (or read) a file and the file then had this
// FileStat: an event of that path whose file still has the same size and modification time is
// the editor's own and is not reported (SPEC §10.5). Call it right after the write, before the
// debounce time has passed. A different stat later, or a delete, forgets the entry.
func (w *Watcher) Suppress(path string, stat fileio.FileStat) {
	path = filepath.Clean(path)
	w.ownMu.Lock()
	defer w.ownMu.Unlock()
	if _, ok := w.own[path]; !ok {
		w.ownOrder = append(w.ownOrder, path)
		if len(w.ownOrder) > maxOwnWrites {
			delete(w.own, w.ownOrder[0])
			w.ownOrder = w.ownOrder[1:]
		}
	}
	w.own[path] = stat
}

// dropOwn removes the events that are the editor's own writes.
func (w *Watcher) dropOwn(events []Event) []Event {
	w.ownMu.Lock()
	defer w.ownMu.Unlock()
	if len(w.own) == 0 {
		return events
	}
	out := events[:0:0]
	for _, e := range events {
		switch e.Kind {
		case EventDeleted:
			delete(w.own, e.Path)
		case EventCreated, EventChanged, EventRenamed:
			if stat, ok := w.own[e.Path]; ok {
				if info, err := os.Stat(e.Path); err == nil && info.Size() == stat.Size && info.ModTime().Equal(stat.ModTime) {
					continue
				}
				delete(w.own, e.Path)
			}
		}
		out = append(out, e)
	}
	return out
}

// polled is what a scan knows of an entry.
type polled struct {
	dir   bool
	size  int64
	mtime time.Time
}

// scan reads the entries of a polled root.
func (w *Watcher) scan(r *root) map[string]polled {
	out := map[string]polled{}
	if r.files != nil {
		for p := range r.files {
			if info, err := os.Stat(p); err == nil {
				out[p] = polled{size: info.Size(), mtime: info.ModTime()}
			}
		}
		return out
	}
	var walk func(dir string)
	walk = func(dir string) {
		if w.ctx.Err() != nil || len(out) >= maxPolledEntries {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			p := filepath.Join(dir, e.Name())
			if r.excluded(p) {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			out[p] = polled{dir: info.IsDir(), size: info.Size(), mtime: info.ModTime()}
			if info.IsDir() && r.recursive {
				walk(p)
			}
		}
	}
	walk(r.path)
	return out
}

// baseline takes the snapshot a polled root is compared with.
func (w *Watcher) baseline(r *root) {
	snap := w.scan(r)
	r.mu.Lock()
	r.snap = snap
	r.mu.Unlock()
}

// pollLoop scans the polled roots every PollInterval.
func (w *Watcher) pollLoop() {
	defer w.wg.Done()
	ticker := time.NewTicker(w.opts.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.pollOnce()
		}
	}
}

// pollOnce scans every polled root and sends what changed since the last scan.
func (w *Watcher) pollOnce() {
	w.mu.Lock()
	var polledRoots []*root
	for r := range w.roots {
		if r.polling {
			polledRoots = append(polledRoots, r)
		}
	}
	w.mu.Unlock()
	for _, r := range polledRoots {
		if w.ctx.Err() != nil {
			return
		}
		next := w.scan(r)
		r.mu.Lock()
		prev, removed := r.snap, r.removed
		if !removed {
			r.snap = next
		}
		r.mu.Unlock()
		if removed || prev == nil {
			continue
		}
		w.send(diffSnapshots(prev, next))
	}
}

// diffSnapshots lists what happened between two scans.
func diffSnapshots(prev, next map[string]polled) []rawEvent {
	var out []rawEvent
	for p, n := range next {
		old, ok := prev[p]
		switch {
		case !ok:
			out = append(out, rawEvent{kind: rawCreated, path: p, isDir: n.dir})
		case !n.dir && (old.size != n.size || !old.mtime.Equal(n.mtime)):
			out = append(out, rawEvent{kind: rawChanged, path: p})
		}
	}
	for p, o := range prev {
		if _, ok := next[p]; !ok {
			out = append(out, rawEvent{kind: rawDeleted, path: p, isDir: o.dir})
		}
	}
	return out
}

// isNoSpace tells whether an error is the limit of inotify watches.
func isNoSpace(err error) bool { return errors.Is(err, syscall.ENOSPC) }
