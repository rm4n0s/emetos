package workspace

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/emetos/internal/core/glob"
	"github.com/rm4n0s/emetos/internal/core/settings"
)

// Ported from VS Code (in structure only): src/vs/workbench/services/search/node/fileSearch.ts and
// src/vs/workbench/services/search/common/queryBuilder.ts decide which files a file search lists;
// the walk is Emetos's own.

// maxIgnoreFileSize bounds the ignore files that are read: a larger one is a mistake.
const maxIgnoreFileSize = 1 << 20

// IndexConfig is what decides which files the FileIndex lists.
type IndexConfig struct {
	// Exclude are the patterns of files.exclude and search.exclude together (search.exclude wins for a
	// pattern both have). A folder that matches is not entered.
	Exclude glob.Expression
	// UseIgnoreFiles is search.useIgnoreFiles: .gitignore and .ignore files in the folders that are
	// walked, and .git/info/exclude, leave files out.
	UseIgnoreFiles bool
	// UseParentIgnoreFiles is search.useParentIgnoreFiles: the ignore files of the parent folders of the
	// opened folder count too. It needs UseIgnoreFiles.
	UseParentIgnoreFiles bool
	// GlobalIgnoreFile is the path of the user's global ignore file, used when it is not empty; it needs
	// UseIgnoreFiles (search.useGlobalIgnoreFiles).
	GlobalIgnoreFile string
	// FollowSymlinks is search.followSymlinks: links to files and folders are listed and entered.
	FollowSymlinks bool
}

// DefaultIndexConfig is the configuration of the default settings.
func DefaultIndexConfig() IndexConfig {
	return IndexConfig{
		Exclude: glob.ExpressionFromSetting(map[string]any{
			"**/.git": true, "**/.svn": true, "**/.hg": true, "**/.jj": true, "**/.DS_Store": true, "**/Thumbs.db": true,
			"**/node_modules": true, "**/bower_components": true, "**/*.code-search": true,
		}),
		UseIgnoreFiles: true,
		FollowSymlinks: true,
	}
}

// IndexConfigFromSettings reads the settings of the file search: files.exclude and search.exclude
// (the second overrides the first for the same pattern), search.useIgnoreFiles,
// search.useGlobalIgnoreFiles, search.useParentIgnoreFiles and search.followSymlinks.
func IndexConfigFromSettings(store *settings.Store) IndexConfig {
	merged := map[string]any{}
	for _, key := range []string{"files.exclude", "search.exclude"} {
		for k, v := range settings.Get[map[string]any](store, key, "") {
			merged[k] = v
		}
	}
	cfg := IndexConfig{
		Exclude:              glob.ExpressionFromSetting(merged),
		UseIgnoreFiles:       settings.Get[bool](store, "search.useIgnoreFiles", ""),
		UseParentIgnoreFiles: settings.Get[bool](store, "search.useParentIgnoreFiles", ""),
		FollowSymlinks:       settings.Get[bool](store, "search.followSymlinks", ""),
	}
	if settings.Get[bool](store, "search.useGlobalIgnoreFiles", "") {
		cfg.GlobalIgnoreFile = globalIgnorePath()
	}
	return cfg
}

// globalIgnorePath is where git looks for the global ignore file when core.excludesFile is not set:
// $XDG_CONFIG_HOME/git/ignore or ~/.config/git/ignore.
func globalIgnorePath() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "git", "ignore")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".config", "git", "ignore")
	}
	return ""
}

// IndexEventKind says what happened to a FileIndex.
type IndexEventKind int

const (
	// IndexGrew says that the walk found more files (at most about ten times a second).
	IndexGrew IndexEventKind = iota + 1
	// IndexDone says that the walk is complete.
	IndexDone
	// IndexChanged says that files were added or removed by Apply or SetFiles.
	IndexChanged
)

// IndexEvent is told to the listeners of OnChange, on the goroutine that changed the index; a
// listener must not block.
type IndexEvent struct {
	Kind IndexEventKind
	// Version is the version of the list after the change.
	Version uint64
}

// IndexOptions say how a FileIndex is made.
type IndexOptions struct {
	// Config is the configuration; the zero value is DefaultIndexConfig.
	Config *IndexConfig
	// FS is the file system; the default is the operating system's.
	FS FS
	// ReadFile reads the ignore files; the default is os.ReadFile.
	ReadFile func(path string) ([]byte, error)
}

// IndexSnapshot is the files of an index at one moment.
type IndexSnapshot struct {
	// Files are the paths of the files relative to the folder, with "/", in the order they were found
	// (a breadth-first walk, each folder sorted by name, then the files that Apply added). The slice is
	// shared and must not be changed.
	Files []string
	// Complete says that the walk has finished.
	Complete bool
	// Version grows with every change of the list.
	Version uint64
	// Folders is the number of folders the walk read; Unreadable the number it could not read.
	Folders, Unreadable int
}

// parentIgnore is the ignore files of one parent folder of the opened folder.
type parentIgnore struct {
	ig *glob.Ignore
	// prefix is the path of the opened folder relative to the parent, with a "/" at the end.
	prefix string
}

// walkState is one walk of the folder and the rules it uses; Rebuild makes a new one and the old
// one stops.
type walkState struct {
	ctx    context.Context
	cancel context.CancelFunc
	cfg    IndexConfig
	match  *glob.ExprMatcher
	// rootReal is the folder with its links resolved.
	rootReal string

	// igMu guards ig and igDirs: the walk adds the ignore files as it reads them while Apply asks.
	igMu sync.RWMutex
	ig   *glob.Ignore
	// igDirs are the folders whose ignore files were looked for, layerDirs the ones that had any.
	igDirs    map[string]bool
	layerDirs []string
	parents   []parentIgnore

	// lastFire is when IndexGrew was last told (guarded by the index's mutex).
	lastFire time.Time
}

// FileIndex is the list of the files of a folder that the quick open searches: a background walk of
// the folder that leaves out what files.exclude, search.exclude and the ignore files leave out,
// kept up to date with the events of the watcher. All methods are safe for concurrent use.
//
// Memory: the paths are one string each; the walk keeps no other per-file data.
type FileIndex struct {
	root     string
	fsys     FS
	readFile func(string) ([]byte, error)

	mu        sync.RWMutex
	cfg       IndexConfig
	parent    context.Context
	cur       *walkState
	closed    bool
	files     []string
	set       map[string]struct{}
	version   uint64
	complete  bool
	folders   int
	unread    int
	listeners []*func(IndexEvent)
	wg        sync.WaitGroup
	nowFn     func() time.Time
}

// NewFileIndex makes an index of the folder at root (an absolute path). It lists nothing until Start.
func NewFileIndex(root string, opts ...IndexOptions) *FileIndex {
	var o IndexOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	cfg := DefaultIndexConfig()
	if o.Config != nil {
		cfg = *o.Config
	}
	x := &FileIndex{root: filepath.Clean(root), fsys: o.FS, readFile: o.ReadFile, cfg: cfg, nowFn: time.Now}
	if x.fsys == nil {
		x.fsys = osFS{}
	}
	if x.readFile == nil {
		x.readFile = os.ReadFile
	}
	return x
}

// Root is the folder the index lists.
func (x *FileIndex) Root() string { return x.root }

// Config is the configuration in use.
func (x *FileIndex) Config() IndexConfig {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.cfg
}

// OnChange calls fn when the list changes (see IndexEvent) and returns the function that stops it.
func (x *FileIndex) OnChange(fn func(IndexEvent)) (unsubscribe func()) {
	p := &fn
	x.mu.Lock()
	x.listeners = append(x.listeners, p)
	x.mu.Unlock()
	return func() {
		x.mu.Lock()
		defer x.mu.Unlock()
		if i := slices.Index(x.listeners, p); i >= 0 {
			x.listeners = slices.Delete(x.listeners, i, i+1)
		}
	}
}

// fire tells the listeners; the caller does not hold mu.
func (x *FileIndex) fire(kind IndexEventKind, version uint64) {
	x.mu.RLock()
	ls := slices.Clone(x.listeners)
	x.mu.RUnlock()
	for _, f := range ls {
		(*f)(IndexEvent{Kind: kind, Version: version})
	}
}

// Start begins the walk in the background. It stops when ctx is done or Close is called. A second
// call does nothing.
func (x *FileIndex) Start(ctx context.Context) {
	x.mu.Lock()
	if x.parent != nil || x.closed {
		x.mu.Unlock()
		return
	}
	x.parent = ctx
	x.startWalkLocked()
	x.mu.Unlock()
}

// SetConfig changes the configuration; when it differs from the current one the list is made
// again (Rebuild). It tells whether it did.
func (x *FileIndex) SetConfig(cfg IndexConfig) bool {
	x.mu.Lock()
	if reflect.DeepEqual(x.cfg, cfg) {
		x.mu.Unlock()
		return false
	}
	x.cfg = cfg
	started := x.parent != nil && !x.closed
	if started {
		x.startWalkLocked()
	}
	v := x.version
	x.mu.Unlock()
	if started {
		x.fire(IndexChanged, v)
	}
	return true
}

// Rebuild stops the walk, forgets the files and walks the folder again. The old list is empty at once;
// the new one grows as the walk goes.
func (x *FileIndex) Rebuild() {
	x.mu.Lock()
	if x.parent == nil || x.closed {
		x.mu.Unlock()
		return
	}
	x.startWalkLocked()
	v := x.version
	x.mu.Unlock()
	x.fire(IndexChanged, v)
}

// Close stops the walk and waits for it. The index lists nothing new afterwards.
func (x *FileIndex) Close() {
	x.mu.Lock()
	x.closed = true
	if x.cur != nil {
		x.cur.cancel()
	}
	x.mu.Unlock()
	x.wg.Wait()
}

// Wait blocks until the walk is complete or the index is closed or its context is done.
func (x *FileIndex) Wait() { x.wg.Wait() }

// SetFiles replaces the list with paths (relative to the folder, with "/") and marks it complete,
// without walking: for tests and benchmarks.
func (x *FileIndex) SetFiles(paths []string) {
	x.mu.Lock()
	if x.cur != nil {
		x.cur.cancel()
		x.cur = nil
	}
	x.files, x.set, x.complete = slices.Clone(paths), nil, true
	x.version++
	v := x.version
	x.mu.Unlock()
	x.fire(IndexChanged, v)
}

// Snapshot is the files found so far.
func (x *FileIndex) Snapshot() IndexSnapshot {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return IndexSnapshot{Files: x.files, Complete: x.complete, Version: x.version, Folders: x.folders, Unreadable: x.unread}
}

// startWalkLocked stops the walk in progress and starts another. The caller holds mu.
func (x *FileIndex) startWalkLocked() {
	if x.cur != nil {
		x.cur.cancel()
	}
	ctx, cancel := context.WithCancel(x.parent)
	ws := &walkState{ctx: ctx, cancel: cancel, cfg: x.cfg, igDirs: map[string]bool{}}
	ws.match = glob.ParseExpression(x.cfg.Exclude, glob.Options{})
	ws.rootReal = x.root
	if real, err := filepath.EvalSymlinks(x.root); err == nil {
		ws.rootReal = real
	}
	ws.ig = glob.NewIgnore(glob.IgnoreOptions{})
	x.cur = ws
	x.files, x.set, x.complete = nil, nil, false
	x.folders, x.unread = 0, 0
	x.version++
	x.wg.Add(1)
	go func() {
		defer x.wg.Done()
		x.loadIgnoreSources(ws)
		if x.walk(ws, []dirNode{{rel: "", real: ws.rootReal, parent: -1}}) {
			x.finishWalk(ws)
		}
	}()
}

// loadIgnoreSources reads the ignore files that are not in the walked folders: the exclude file of
// git, the global ignore file and the files of the parent folders.
func (x *FileIndex) loadIgnoreSources(ws *walkState) {
	if !ws.cfg.UseIgnoreFiles {
		return
	}
	var parents []parentIgnore
	if ws.cfg.UseParentIgnoreFiles {
		prefix := filepath.Base(x.root) + "/"
		for dir := filepath.Dir(x.root); ; dir = filepath.Dir(dir) {
			ig := glob.NewIgnore(glob.IgnoreOptions{})
			found := false
			for _, name := range []string{".gitignore", ".ignore"} {
				if text, ok := x.readIgnoreFile(filepath.Join(dir, name)); ok {
					ig.AddFile("", text)
					found = true
				}
			}
			if found {
				parents = append(parents, parentIgnore{ig: ig, prefix: prefix})
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			prefix = filepath.Base(dir) + "/" + prefix
		}
	}
	ws.igMu.Lock()
	defer ws.igMu.Unlock()
	ws.parents = parents
	if text, ok := x.readIgnoreFile(filepath.Join(x.root, ".git", "info", "exclude")); ok {
		ws.ig.AddGlobal(text)
	}
	if ws.cfg.GlobalIgnoreFile != "" {
		if text, ok := x.readIgnoreFile(ws.cfg.GlobalIgnoreFile); ok {
			ws.ig.AddGlobal(text)
		}
	}
}

// readIgnoreFile reads an ignore file; ok is false when it cannot be read or is too big.
func (x *FileIndex) readIgnoreFile(path string) (string, bool) {
	data, err := x.readFile(path)
	if err != nil || len(data) > maxIgnoreFileSize {
		return "", false
	}
	return string(data), true
}

// dirNode is a folder of a walk: its path relative to the root, its path with the links resolved,
// and the index in the list of its parent.
type dirNode struct {
	rel, real string
	parent    int
}

// walkFireEvery and walkCtxEvery decide how the walk hands its findings over and how often it looks
// at its context.
const (
	walkFireEvery = 100 * time.Millisecond
	walkCtxEvery  = 512
)

// walk reads the folders breadth-first from the start folders and adds the files it finds. It tells
// whether it read them all: false when its context is done.
func (x *FileIndex) walk(ws *walkState, start []dirNode) bool {
	nodes := slices.Clone(start)
	for next := 0; next < len(nodes); next++ {
		if ws.ctx.Err() != nil {
			return false
		}
		n := nodes[next]
		abs := x.abs(n.rel)
		entries, err := x.fsys.ReadDir(abs)
		if err != nil {
			x.mu.Lock()
			if x.cur == ws {
				x.unread++
			}
			x.mu.Unlock()
			continue
		}
		var names map[string]struct{}
		hasSibling := func(name string) bool {
			if names == nil {
				names = make(map[string]struct{}, len(entries))
				for _, e := range entries {
					names[e.Name()] = struct{}{}
				}
			}
			_, ok := names[name]
			return ok
		}
		if ws.cfg.UseIgnoreFiles {
			x.readFolderIgnoreFiles(ws, n.rel, abs, entries)
		}
		var found []string
		for i, e := range entries {
			if i%walkCtxEvery == walkCtxEvery-1 && ws.ctx.Err() != nil {
				return false
			}
			name := e.Name()
			if name == ".git" {
				continue
			}
			rel := name
			if n.rel != "" {
				rel = n.rel + "/" + name
			}
			isDir, real, ok := x.classify(ws, e, abs, next, nodes, name)
			if !ok {
				continue
			}
			if x.excluded(ws, rel, name, isDir, hasSibling) {
				continue
			}
			if isDir {
				nodes = append(nodes, dirNode{rel: rel, real: real, parent: next})
			} else {
				found = append(found, rel)
			}
		}
		x.add(ws, found)
	}
	return true
}

// finishWalk marks the list complete when the walk was the one of the current state.
func (x *FileIndex) finishWalk(ws *walkState) {
	x.mu.Lock()
	if x.cur != ws {
		x.mu.Unlock()
		return
	}
	x.complete = true
	x.version++
	v := x.version
	x.mu.Unlock()
	x.fire(IndexDone, v)
}

// abs is the absolute path of a path relative to the folder.
func (x *FileIndex) abs(rel string) string {
	if rel == "" {
		return x.root
	}
	return filepath.Join(x.root, filepath.FromSlash(rel))
}

// classify tells whether an entry is a folder to enter (with the path of the folder with its links
// resolved) or a file to list; ok is false for the entries that are neither (sockets, broken links,
// links when they are not followed, and folders that would loop).
func (x *FileIndex) classify(ws *walkState, e fs.DirEntry, abs string, cur int, nodes []dirNode, name string) (isDir bool, real string, ok bool) {
	n := nodes[cur]
	typ := e.Type()
	if typ&fs.ModeSymlink == 0 {
		switch {
		case typ.IsDir():
			return true, n.real + "/" + name, true
		case typ.IsRegular():
			return false, "", true
		}
		return false, "", false
	}
	if !ws.cfg.FollowSymlinks {
		return false, "", false
	}
	path := filepath.Join(abs, name)
	info, err := x.fsys.Stat(path)
	if err != nil {
		return false, "", false // a broken link
	}
	if !info.IsDir() {
		return false, "", info.Mode().IsRegular()
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false, "", false
	}
	// A link to the folder itself, to a parent of it or to a folder that contains the opened one loops.
	if isPathPrefix(target, ws.rootReal) {
		return false, "", false
	}
	for i := cur; i >= 0; i = nodes[i].parent {
		if nodes[i].real == target {
			return false, "", false
		}
	}
	return true, target, true
}

// isPathPrefix tells whether dir is path or one of its parents.
func isPathPrefix(dir, path string) bool {
	return path == dir || strings.HasPrefix(path, strings.TrimSuffix(dir, "/")+"/")
}

// readFolderIgnoreFiles adds the .gitignore and .ignore of a folder to the rules, unless the folder was
// read before.
func (x *FileIndex) readFolderIgnoreFiles(ws *walkState, rel, abs string, entries []fs.DirEntry) {
	ws.igMu.RLock()
	seen := ws.igDirs[rel]
	ws.igMu.RUnlock()
	if seen {
		return
	}
	var texts []string
	for _, name := range []string{".gitignore", ".ignore"} {
		if !hasEntry(entries, name) {
			continue
		}
		if text, ok := x.readIgnoreFile(filepath.Join(abs, name)); ok {
			texts = append(texts, text)
		}
	}
	ws.igMu.Lock()
	defer ws.igMu.Unlock()
	ws.igDirs[rel] = true
	if len(texts) > 0 {
		ws.layerDirs = append(ws.layerDirs, rel)
	}
	for _, text := range texts {
		ws.ig.AddFile(rel, text)
	}
}

// hasEntry tells whether a folder has an entry of a name.
func hasEntry(entries []fs.DirEntry, name string) bool {
	for _, e := range entries {
		if e.Name() == name {
			return true
		}
	}
	return false
}

// excluded tells whether a path (an entry of a folder that was entered, so that its parents are
// not excluded) is left out by the exclude patterns or the ignore files.
func (x *FileIndex) excluded(ws *walkState, rel, name string, isDir bool, hasSibling func(string) bool) bool {
	if !ws.match.IsEmpty() {
		if _, hit := ws.match.Match(rel, name, hasSibling); hit {
			return true
		}
	}
	if !ws.cfg.UseIgnoreFiles {
		return false
	}
	ws.igMu.RLock()
	defer ws.igMu.RUnlock()
	if ignored, _ := ws.ig.Check(rel, isDir); ignored {
		return true
	}
	for _, p := range ws.parents {
		if p.ig.Ignored(p.prefix+rel, isDir) {
			return true
		}
	}
	return false
}

// add appends found files to the list (the ones that are not in it) when the state is current, and
// tells the listeners now and then.
func (x *FileIndex) add(ws *walkState, found []string) {
	if len(found) == 0 {
		x.mu.Lock()
		if x.cur == ws {
			x.folders++
		}
		x.mu.Unlock()
		return
	}
	x.mu.Lock()
	if x.cur != ws {
		x.mu.Unlock()
		return
	}
	x.folders++
	x.appendLocked(found)
	fireNow := false
	if now := x.nowFn(); now.Sub(ws.lastFire) >= walkFireEvery {
		ws.lastFire, fireNow = now, true
	}
	v := x.version
	x.mu.Unlock()
	if fireNow {
		x.fire(IndexGrew, v)
	}
}

// appendLocked appends paths that are not in the list. The caller holds mu. Snapshots share the
// array: only the part past their length is written.
func (x *FileIndex) appendLocked(paths []string) {
	if x.set == nil {
		x.files = append(x.files, paths...)
		x.version++
		return
	}
	for _, p := range paths {
		if _, dup := x.set[p]; dup {
			continue
		}
		x.set[p] = struct{}{}
		x.files = append(x.files, p)
	}
	x.version++
}

// ensureSetLocked makes the set that finds a file by its path. The caller holds mu.
func (x *FileIndex) ensureSetLocked() {
	if x.set != nil {
		return
	}
	x.set = make(map[string]struct{}, len(x.files))
	for _, p := range x.files {
		x.set[p] = struct{}{}
	}
}

// rel is the path relative to the folder of an absolute path; ok is false outside the folder or for
// the folder itself.
func (x *FileIndex) rel(path string) (string, bool) {
	rel, ok := strings.CutPrefix(path, strings.TrimSuffix(x.root, "/")+"/")
	if !ok || rel == "" {
		return "", false
	}
	return filepath.ToSlash(rel), true
}
