package workspace

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/glob"
)

// FS is what the tree asks of the file system. Tests replace it to count the reads.
type FS interface {
	// ReadDir lists a folder in any order.
	ReadDir(path string) ([]fs.DirEntry, error)
	// Stat follows links.
	Stat(path string) (fs.FileInfo, error)
	// Lstat does not follow links.
	Lstat(path string) (fs.FileInfo, error)
}

type osFS struct{}

func (osFS) ReadDir(path string) ([]fs.DirEntry, error) { return os.ReadDir(path) }
func (osFS) Stat(path string) (fs.FileInfo, error)      { return os.Stat(path) }
func (osFS) Lstat(path string) (fs.FileInfo, error)     { return os.Lstat(path) }

// Options say how a Workspace is made.
type Options struct {
	// Config is the configuration of the tree; the zero value is DefaultConfig.
	Config *Config
	// FS is the file system; the default is the operating system's.
	FS FS
}

// ChangeKind says what happened to the tree.
type ChangeKind int

const (
	// ChangeAdded is an item added to a folder whose children were read.
	ChangeAdded ChangeKind = iota + 1
	// ChangeRemoved is an item removed (it was deleted or moved out of the tree).
	ChangeRemoved
	// ChangeUpdated is an item whose size, time or kind changed.
	ChangeUpdated
	// ChangeRenamed is an item that got a new name or folder; OldPath is where it was.
	ChangeRenamed
	// ChangeRefreshed is a folder whose children were read again; the UI rebuilds everything
	// below Path.
	ChangeRefreshed
)

// Change tells the UI what Apply or Refresh did to the tree.
type Change struct {
	Kind ChangeKind
	// Path is the path of the item (the new path for ChangeRenamed).
	Path string
	// OldPath is the former path of a renamed item.
	OldPath string
	// Item is the item; for ChangeRemoved it is the item that was removed, no longer in the tree.
	Item *Item
	// Parent is the folder the item is in (for ChangeRemoved, the folder it was in).
	Parent *Item
}

// Workspace is the folder open in a window: a tree of items read lazily. It is safe for
// concurrent use; the items it hands out are shared and their accessors lock the tree.
type Workspace struct {
	fsys FS

	mu        sync.RWMutex
	root      *Item
	cfg       Config
	exclude   *glob.ExprMatcher
	listeners []*func(Change)
}

// Open opens the folder at path (made absolute and clean). The tree is empty until Children
// reads it. It fails with the tags WorkspaceOpenCancelled, WorkspacePathInvalid,
// WorkspaceFolderNotFound, WorkspaceStatFailed and WorkspaceNotDirectory.
func Open(ctx context.Context, path string, opts ...Options) (*Workspace, error) {
	var o Options
	if len(opts) > 0 {
		o = opts[0]
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.NewErr("WorkspaceOpenCancelled", err).SetMetadata("path", path)
	}
	if path == "" {
		return nil, errors.New("WorkspacePathInvalid", "the path of the folder is empty")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, errors.NewErr("WorkspacePathInvalid", err).SetMetadata("path", path)
	}
	fsys := o.FS
	if fsys == nil {
		fsys = osFS{}
	}
	info, err := fsys.Stat(abs)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.NewErr("WorkspaceFolderNotFound", err).SetMetadata("path", abs)
		}
		return nil, errors.NewErr("WorkspaceStatFailed", err).SetMetadata("path", abs)
	}
	if !info.IsDir() {
		return nil, errors.New("WorkspaceNotDirectory", "the path is not a folder", "path", abs)
	}
	w := &Workspace{fsys: fsys}
	cfg := DefaultConfig()
	if o.Config != nil {
		cfg = *o.Config
	}
	w.setConfig(cfg)
	w.root = &Item{ws: w, name: filepath.Base(abs), path: abs, isDir: true, mtime: info.ModTime()}
	return w, nil
}

// Name is the name of the folder.
func (w *Workspace) Name() string { return w.root.Name() }

// Path is the absolute path of the folder.
func (w *Workspace) Path() string { return w.root.Path() }

// Root is the item of the folder.
func (w *Workspace) Root() *Item { return w.root }

// Config is the configuration of the tree.
func (w *Workspace) Config() Config {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.cfg
}

// SetConfig changes the configuration. The order of the children follows it at once; the
// files.exclude patterns only apply to folders read from now on, so call Refresh to apply
// them to the folders that were read. It reports whether the configuration changed.
func (w *Workspace) SetConfig(cfg Config) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cfg.equal(cfg) {
		return false
	}
	w.setConfig(cfg)
	return true
}

func (w *Workspace) setConfig(cfg Config) {
	if cfg.SortOrder == "" {
		cfg.SortOrder = SortDefault
	}
	if cfg.Lexicographic == "" {
		cfg.Lexicographic = LexDefault
	}
	w.cfg = cfg
	w.exclude = glob.ParseExpression(cfg.Exclude, glob.Options{})
}

// OnChange registers fn to be called, in the goroutine that changed the tree and with no lock
// held, after Apply or Refresh changed it. The returned function removes it.
func (w *Workspace) OnChange(fn func(Change)) (unsubscribe func()) {
	p := &fn
	w.mu.Lock()
	w.listeners = append(w.listeners, p)
	w.mu.Unlock()
	return func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		w.listeners = slices.DeleteFunc(w.listeners, func(x *func(Change)) bool { return x == p })
	}
}

func (w *Workspace) notify(changes []Change) {
	if len(changes) == 0 {
		return
	}
	w.mu.RLock()
	listeners := slices.Clone(w.listeners)
	w.mu.RUnlock()
	for _, c := range changes {
		for _, l := range listeners {
			(*l)(c)
		}
	}
}

// Excluded tells whether files.exclude hides a path inside the folder. hasSibling says whether
// a file of a name is next to the path (for the patterns with a "when"); nil means none is.
func (w *Workspace) Excluded(path string, hasSibling func(name string) bool) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	rel, ok := w.relative(path)
	return ok && w.excludedRel(rel, hasSibling)
}

// relative is the slash-separated path of a path below the folder, "" for the folder itself;
// ok is false for a path outside it. The caller holds mu or owns the workspace.
func (w *Workspace) relative(path string) (string, bool) {
	root := w.root.path
	if path == root {
		return "", true
	}
	rel, ok := strings.CutPrefix(path, strings.TrimSuffix(root, string(filepath.Separator))+string(filepath.Separator))
	if !ok {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

func (w *Workspace) excludedRel(rel string, hasSibling func(string) bool) bool {
	if rel == "" || w.exclude.IsEmpty() {
		return false
	}
	_, hit := w.exclude.Match(rel, path0(rel), hasSibling)
	return hit
}

// path0 is the last segment of a slash-separated path.
func path0(rel string) string {
	if i := strings.LastIndexByte(rel, '/'); i >= 0 {
		return rel[i+1:]
	}
	return rel
}

// Find returns the item at a path if the tree has read it, or nil.
func (w *Workspace) Find(path string) *Item {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.find(path)
}

func (w *Workspace) find(path string) *Item {
	rel, ok := w.relative(filepath.Clean(path))
	if !ok {
		return nil
	}
	item := w.root
	if rel == "" {
		return item
	}
	for _, name := range strings.Split(rel, "/") {
		if !item.isDir {
			return nil
		}
		item = item.children[name]
		if item == nil {
			return nil
		}
	}
	return item
}

// Item is a file or folder of the tree (VS Code's ExplorerItem). Its accessors are safe for
// concurrent use.
type Item struct {
	ws     *Workspace
	parent *Item
	name   string
	path   string

	isDir     bool
	isSymlink bool
	// isUnknown is a path that is neither a file nor a folder (a socket, a device).
	isUnknown bool
	size      int64
	mtime     time.Time

	resolved bool
	err      error
	children map[string]*Item
}

// Name is the name of the item.
func (i *Item) Name() string {
	i.ws.mu.RLock()
	defer i.ws.mu.RUnlock()
	return i.name
}

// Path is the absolute path of the item.
func (i *Item) Path() string {
	i.ws.mu.RLock()
	defer i.ws.mu.RUnlock()
	return i.path
}

// IsDir tells a folder, or a link to one, from a file.
func (i *Item) IsDir() bool {
	i.ws.mu.RLock()
	defer i.ws.mu.RUnlock()
	return i.isDir
}

// IsSymlink tells whether the item is a symbolic link; IsDir then says what it points at.
func (i *Item) IsSymlink() bool {
	i.ws.mu.RLock()
	defer i.ws.mu.RUnlock()
	return i.isSymlink
}

// IsUnknown tells whether the item is neither a file nor a folder (a socket or a device).
func (i *Item) IsUnknown() bool {
	i.ws.mu.RLock()
	defer i.ws.mu.RUnlock()
	return i.isUnknown
}

// Size is the size of a file in bytes.
func (i *Item) Size() int64 {
	i.ws.mu.RLock()
	defer i.ws.mu.RUnlock()
	return i.size
}

// ModTime is the time of the last change.
func (i *Item) ModTime() time.Time {
	i.ws.mu.RLock()
	defer i.ws.mu.RUnlock()
	return i.mtime
}

// IsRoot tells whether the item is the folder of the workspace.
func (i *Item) IsRoot() bool { return i == i.ws.root }

// Parent is the folder the item is in, nil for the root (and for an item that was removed).
func (i *Item) Parent() *Item {
	i.ws.mu.RLock()
	defer i.ws.mu.RUnlock()
	return i.parent
}

// Resolved tells whether the children of a folder were read.
func (i *Item) Resolved() bool {
	i.ws.mu.RLock()
	defer i.ws.mu.RUnlock()
	return i.resolved
}

// Err is the error of the last attempt to read the folder, nil if it worked. A folder that
// cannot be read does not fail the tree.
func (i *Item) Err() error {
	i.ws.mu.RLock()
	defer i.ws.mu.RUnlock()
	return i.err
}

// Child returns the child of a folder with a name, if it was read.
func (i *Item) Child(name string) *Item {
	i.ws.mu.RLock()
	defer i.ws.mu.RUnlock()
	return i.children[name]
}

// Loaded returns the children that were read, sorted, without reading the folder.
func (i *Item) Loaded() []*Item {
	i.ws.mu.RLock()
	defer i.ws.mu.RUnlock()
	return i.sortedChildren()
}

// sortedChildren is the children in the order of the configuration. The caller holds mu.
func (i *Item) sortedChildren() []*Item {
	out := make([]*Item, 0, len(i.children))
	for _, c := range i.children {
		out = append(out, c)
	}
	cmp := i.ws.cfg.comparer()
	slices.SortFunc(out, cmp)
	return out
}

// Children reads the folder if that was not done yet, and returns its children in the order
// of explorer.sortOrder, without the files that files.exclude hides. A file has none. A folder
// that cannot be read stays unread, keeps the error (Err) and fails with WorkspaceReadDirFailed
// (WorkspaceReadCancelled when ctx ends); the rest of the tree is not affected.
func (i *Item) Children(ctx context.Context) ([]*Item, error) {
	i.ws.mu.RLock()
	isDir, resolved := i.isDir, i.resolved
	i.ws.mu.RUnlock()
	if !isDir {
		return nil, nil
	}
	if !resolved {
		if err := i.ws.read(ctx, i); err != nil {
			return nil, err
		}
	}
	return i.Loaded(), nil
}

// read lists a folder and merges what it finds into the item: the children that are still
// there keep their Item (and so what is read below them), the others go.
func (w *Workspace) read(ctx context.Context, dir *Item) error {
	w.mu.RLock()
	path := dir.path
	w.mu.RUnlock()
	infos, err := w.list(ctx, path)
	if err != nil {
		w.mu.Lock()
		dir.err = err
		w.mu.Unlock()
		return err
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.merge(dir, infos)
	return nil
}

// child is what is known of an entry of a folder.
type child struct {
	name      string
	isDir     bool
	isSymlink bool
	isUnknown bool
	size      int64
	mtime     time.Time
}

// list reads the entries of a folder, without the excluded ones. It does not need the lock of
// the tree except to read the configuration.
func (w *Workspace) list(ctx context.Context, path string) ([]child, error) {
	if err := ctx.Err(); err != nil {
		return nil, errors.NewErr("WorkspaceReadCancelled", err).SetMetadata("path", path)
	}
	entries, err := w.fsys.ReadDir(path)
	if err != nil {
		return nil, errors.NewErr("WorkspaceReadDirFailed", err).SetMetadata("path", path)
	}
	names := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		names[e.Name()] = struct{}{}
	}
	hasSibling := func(name string) bool { _, ok := names[name]; return ok }
	w.mu.RLock()
	rel, _ := w.relative(path)
	exclude := w.exclude
	w.mu.RUnlock()
	out := make([]child, 0, len(entries))
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return nil, errors.NewErr("WorkspaceReadCancelled", err).SetMetadata("path", path)
		}
		name := e.Name()
		childRel := name
		if rel != "" {
			childRel = rel + "/" + name
		}
		if !exclude.IsEmpty() {
			if _, hit := exclude.Match(childRel, name, hasSibling); hit {
				continue
			}
		}
		c, ok := w.describe(filepath.Join(path, name), e)
		if ok {
			out = append(out, c)
		}
	}
	return out, nil
}

// describe makes the child for an entry; ok is false for an entry that vanished meanwhile.
func (w *Workspace) describe(path string, e fs.DirEntry) (child, bool) {
	info, err := e.Info()
	if err != nil {
		return child{}, false
	}
	c := child{name: e.Name(), isDir: info.IsDir(), size: info.Size(), mtime: info.ModTime()}
	if info.Mode()&fs.ModeSymlink != 0 {
		c.isSymlink = true
		if target, err := w.fsys.Stat(path); err == nil {
			c.isDir, c.size, c.mtime = target.IsDir(), target.Size(), target.ModTime()
			c.isUnknown = !target.IsDir() && !target.Mode().IsRegular()
		}
		return c, true
	}
	c.isUnknown = !c.isDir && !info.Mode().IsRegular()
	return c, true
}

// merge replaces the children of dir with the entries found. The caller holds mu for writing.
func (w *Workspace) merge(dir *Item, infos []child) {
	old := dir.children
	dir.children = make(map[string]*Item, len(infos))
	for _, c := range infos {
		item := old[c.name]
		if item == nil {
			item = &Item{ws: w, parent: dir, name: c.name, path: filepath.Join(dir.path, c.name)}
		}
		item.setFrom(c)
		dir.children[c.name] = item
	}
	for name, gone := range old {
		if _, kept := dir.children[name]; !kept {
			gone.parent = nil
		}
	}
	dir.resolved = true
	dir.err = nil
}

// setFrom copies what was read of an entry. A folder that became a file forgets its children.
func (i *Item) setFrom(c child) {
	if i.isDir && !c.isDir {
		i.children, i.resolved = nil, false
	}
	i.isDir, i.isSymlink, i.isUnknown, i.size, i.mtime = c.isDir, c.isSymlink, c.isUnknown, c.size, c.mtime
}

// Refresh reads again the folder of an item and every folder below it that was read, keeping
// the items that are still there (and with them what was read below them). A nil item is the
// root. A folder that cannot be read keeps the error (Err); a folder that is gone leaves the
// tree. It fails only with WorkspaceReadCancelled, when ctx ends.
func (w *Workspace) Refresh(ctx context.Context, item *Item) error {
	if item == nil {
		item = w.root
	}
	w.mu.RLock()
	resolved := item.resolved
	w.mu.RUnlock()
	if !resolved {
		return nil
	}
	if err := w.refresh(ctx, item); err != nil {
		return err
	}
	w.notify([]Change{{Kind: ChangeRefreshed, Path: item.Path(), Item: item, Parent: item.Parent()}})
	return nil
}

func (w *Workspace) refresh(ctx context.Context, dir *Item) error {
	if err := w.read(ctx, dir); err != nil {
		if appErr, ok := errors.FromError(err); ok && appErr.Tag == "WorkspaceReadCancelled" {
			return err
		}
		if errors.Is(err, fs.ErrNotExist) {
			w.mu.Lock()
			w.detach(dir)
			w.mu.Unlock()
		}
		return nil // the error stays on the item
	}
	w.mu.RLock()
	var below []*Item
	for _, c := range dir.children {
		if c.isDir && c.resolved {
			below = append(below, c)
		}
	}
	w.mu.RUnlock()
	for _, c := range below {
		if err := w.refresh(ctx, c); err != nil {
			return err
		}
	}
	return nil
}

// detach takes an item out of its folder. The caller holds mu for writing.
func (w *Workspace) detach(item *Item) {
	if p := item.parent; p != nil {
		if p.children[item.name] == item {
			delete(p.children, item.name)
		}
		item.parent = nil
	}
}

// CompactChain returns the folders that the Explorer shows as one element when
// explorer.compactFolders is on, starting at item: the item, and then its only child while that
// is a folder, and so on, reading the folders it needs. A folder directly below the root is
// never part of a chain (VS Code's ExplorerCompressionDelegate), nor a link (a link to a parent
// would not end). A folder that cannot be read ends the chain and its error is returned with the
// chain so far. With the setting off, or for an item that is not a folder that can start a chain,
// the chain is the item alone. The children of the element are those of the last item of the
// chain.
func (w *Workspace) CompactChain(ctx context.Context, item *Item) ([]*Item, error) {
	chain := []*Item{item}
	if !w.Config().CompactFolders || !w.compressible(item) {
		return chain, nil
	}
	for cur := item; ; {
		kids, err := cur.Children(ctx)
		if err != nil {
			return chain, err
		}
		if len(kids) != 1 || !w.compressible(kids[0]) {
			return chain, nil
		}
		cur = kids[0]
		chain = append(chain, cur)
	}
}

// LoadedChain is CompactChain for what was read already: it follows the only child of a folder as long as
// the folder was read, and reads nothing, so the UI thread can ask it. A folder that was not read ends the
// chain (the explorer shows it longer once it has read it).
func (w *Workspace) LoadedChain(item *Item) []*Item {
	chain := []*Item{item}
	if !w.Config().CompactFolders || !w.compressible(item) {
		return chain
	}
	for cur := item; cur.Resolved(); {
		kids := cur.Loaded()
		if len(kids) != 1 || !w.compressible(kids[0]) {
			break
		}
		cur = kids[0]
		chain = append(chain, cur)
	}
	return chain
}

// compressible tells whether a folder can be part of a chain.
func (w *Workspace) compressible(item *Item) bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return item.isDir && !item.isSymlink && item.parent != nil && item.parent.parent != nil
}
