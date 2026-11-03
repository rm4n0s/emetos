package store

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/httpclient/format"
)

// Host is what the store needs of the editor: the workspace watcher and the trash. extension.Workspace is one.
type Host interface {
	// Watch is extension.Workspace.Watch.
	Watch(ctx context.Context, spec extension.WatchSpec, fn func([]extension.FileChange)) (cancel func(), err error)
	// TrashFiles is extension.Workspace.TrashFiles.
	TrashFiles(ctx context.Context, uris []string) (undo func(ctx context.Context) error, err error)
}

// Options are the optional parts of Open.
type Options struct {
	// Logger receives the store's messages (a watch that could not be placed); nil logs nothing.
	Logger *slog.Logger
}

// Store is the tree of the collections of one workspace folder.
type Store struct {
	folder string
	root   string
	host   Host
	log    *slog.Logger

	opMu sync.Mutex // one operation, or one reload, at a time

	mu        sync.Mutex
	tree      *Tree
	print     string
	listeners map[int]func()
	nextID    int
	closed    bool

	kick chan struct{}
	stop context.CancelFunc
	done chan struct{}

	watchMu     sync.Mutex
	watchTarget string
	watchCancel func()
	watchErr    error
}

// Open reads the collections of the workspace folder and, when host is not nil, follows the changes made to the files on disk
// until Close. The folder .emetos/http does not have to exist. The watch is placed in the background, as the watcher blocks while
// it reads the folders; WatchError says why it could not be placed. The error is HttpDirReadFailed or that of ctx.
func Open(ctx context.Context, folder string, host Host, opts Options) (*Store, error) {
	log := opts.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	s := &Store{
		folder: filepath.Clean(folder), host: host, log: log, listeners: map[int]func(){},
		kick: make(chan struct{}, 1), done: make(chan struct{}),
	}
	s.root = filepath.Join(s.folder, filepath.FromSlash(format.Dir))
	tree, fp, err := scan(ctx, s.folder)
	if err != nil {
		return nil, err
	}
	s.tree, s.print = tree, fp
	loopCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	s.stop = cancel
	go s.loop(loopCtx)
	if host != nil {
		s.kick <- struct{}{} // places the first watch
	}
	return s, nil
}

// Folder is the workspace folder.
func (s *Store) Folder() string { return s.folder }

// Root is the folder of the client's files, <folder>/.emetos/http.
func (s *Store) Root() string { return s.root }

// Snapshot returns the tree as it is now. It is never changed afterwards; a change makes a new one.
func (s *Store) Snapshot() *Tree {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tree
}

// OnChange calls fn after the tree changed, from a goroutine of the store (hand the work to the UI thread with Post), and returns
// the function that stops the calls. fn is not called while a lock of the store is held, so it may call Snapshot.
func (s *Store) OnChange(fn func()) (cancel func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextID
	s.nextID++
	s.listeners[id] = fn
	return func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.listeners, id)
	}
}

// Refresh reads the files again, and calls the listeners when the tree changed. The watcher does it after a change on disk; an
// operation of the store does it after it wrote. It returns whether the tree changed, and the errors of Open.
func (s *Store) Refresh(ctx context.Context) (bool, error) {
	s.opMu.Lock()
	defer s.opMu.Unlock()
	return s.reload(ctx)
}

// reload is Refresh with opMu held.
func (s *Store) reload(ctx context.Context) (bool, error) {
	tree, fp, err := scan(ctx, s.folder)
	if err != nil {
		return false, err
	}
	s.mu.Lock()
	changed := fp != s.print
	if changed {
		s.tree, s.print = tree, fp
	}
	var fns []func()
	if changed && !s.closed {
		for _, fn := range s.listeners {
			fns = append(fns, fn)
		}
	}
	s.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
	return changed, nil
}

// WatchError is why the last attempt to watch the files failed, or nil.
func (s *Store) WatchError() error {
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	return s.watchErr
}

// Close stops following the disk and waits for the background work to end. The tree stays readable.
func (s *Store) Close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	s.mu.Unlock()
	s.stop()
	<-s.done
	s.watchMu.Lock()
	if s.watchCancel != nil {
		s.watchCancel()
		s.watchCancel = nil
	}
	s.watchMu.Unlock()
}

// poke asks the background goroutine to reload and to check where it watches.
func (s *Store) poke() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// loop reloads the tree and moves the watch when poked.
func (s *Store) loop(ctx context.Context) {
	defer close(s.done)
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.kick:
		}
		if _, err := s.Refresh(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("the HTTP client could not read its files", "err", err)
		}
		s.rewatch(ctx)
	}
}

// rewatch watches the deepest of .emetos/http (all of it), .emetos and the workspace folder that exists, so a folder that appears
// later (a git pull brings .emetos/http) is seen, and moves the watch deeper when it does.
func (s *Store) rewatch(ctx context.Context) {
	if s.host == nil {
		return
	}
	target, recursive := s.root, true
	if !isDir(target) {
		target, recursive = filepath.Dir(s.root), false // .emetos
		if !isDir(target) {
			target = s.folder
		}
	}
	s.watchMu.Lock()
	same := s.watchTarget == target && s.watchCancel != nil
	s.watchMu.Unlock()
	if same || ctx.Err() != nil {
		return
	}
	cancel, err := s.host.Watch(ctx, extension.WatchSpec{Path: target, Recursive: recursive}, s.onChanges)
	s.watchMu.Lock()
	defer s.watchMu.Unlock()
	s.watchErr = err
	if err != nil {
		s.log.Warn("the HTTP client could not watch its files", "path", target, "err", err)
		return
	}
	if s.watchCancel != nil {
		s.watchCancel()
	}
	s.watchCancel, s.watchTarget = cancel, target
}

// onChanges is the callback of the watcher (the UI thread): it only pokes the loop when a change is about the client's files.
func (s *Store) onChanges(changes []extension.FileChange) {
	for _, c := range changes {
		if c.Kind == extension.FileRescan || s.relevant(c.Path) || s.relevant(c.OldPath) {
			s.poke()
			return
		}
	}
}

// relevant reports whether a path of a change (absolute, or relative to the folder) is the folder .emetos, or inside it, or the
// folder itself.
func (s *Store) relevant(p string) bool {
	if p == "" {
		return false
	}
	if filepath.IsAbs(p) {
		rel, err := filepath.Rel(s.folder, p)
		if err != nil {
			return false
		}
		p = rel
	}
	p = filepath.ToSlash(p)
	return p == "." || p == ".emetos" || strings.HasPrefix(p, ".emetos/")
}
