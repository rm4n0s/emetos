package explorer

import (
	"github.com/rm4n0s/glutter/widgets"

	"context"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// nodeData is the data of a node of the tree: a file, or a folder, which with explorer.compactFolders
// is a chain of folders shown as one element ("a/b/c"). The key of the node is the path of the first
// item; its children are those of the last.
type nodeData struct {
	// Item is the file or the first folder.
	Item *workspace.Item
	// Chain is the folders of the element, at least the first; nil for a file.
	Chain []*workspace.Item
	// IsDir tells a folder.
	IsDir bool
	// Edit marks the row of a new file or folder that is being named: it has no Item.
	Edit bool
}

// last is the item whose children the node shows; nil for the row of an item being created.
func (n *nodeData) last() *workspace.Item {
	if len(n.Chain) > 0 {
		return n.Chain[len(n.Chain)-1]
	}
	return n.Item
}

// label is the text of the node: the name, or the names of a chain joined by a slash.
func (n *nodeData) label() string {
	if n.Item == nil {
		return ""
	}
	if len(n.Chain) <= 1 {
		return n.Item.Name()
	}
	names := make([]string, len(n.Chain))
	for i, it := range n.Chain {
		names[i] = it.Name()
	}
	return strings.Join(names, "/")
}

// ViewOpts configure a View.
type ViewOpts struct {
	// Post hands a function to the UI thread (the binding's Post); what the background work found is
	// applied there. Without it everything runs at once, which is for tests.
	Post func(func())
	// Keys is the scope of the context keys of the window: the view sets explorerResourceIsFolder,
	// explorerResourceIsRoot and explorerResourceReadonly there for the focused row.
	Keys *contextkey.Scope
	// Icons gives the file icon theme (workbench.iconTheme) each time the rows are built; nil, or a nil
	// theme, shows no icons.
	Icons func() *theme.FileIconTheme
	// Language gives the language id of a file from its path; nil leaves the files without one.
	Language func(path string) string
	// OnOpen is called when the user opens a file: a click or an arrow key and Enter (pinned is false,
	// the file shows as a preview), a double click or Enter (pinned is true).
	OnOpen func(path string, pinned bool)
	// OnLoadFailed is called, on the UI thread, when a folder cannot be read; the folder stays closed so
	// that the next expansion tries again.
	OnLoadFailed func(path string, err error)
	// Logger gets what has nobody to tell; slog.Default() when nil.
	Logger *slog.Logger
	// Binding is the application's binding. With it, the keys typed right after the key that opens the inline input (F2, the
	// new file) go into the input before it is built (kit.TypeAhead). Nil in the tests that have no window.
	Binding *widgets.WidgetsBinding
	// AfterFrame runs a function after the next frame; the view uses it to see where the focus went after a
	// click. Without it the function is posted to the UI thread.
	AfterFrame func(func())
	// TrashEnabled tells whether deleting uses the trash (files.enableTrash); it sets the context key
	// explorerResourceMoveableToTrash. Nil means yes.
	TrashEnabled func() bool
	// OnCommitEdit is called, on the UI thread, when the user commits the name typed in the inline input of a
	// new file or folder or of a rename.
	OnCommitEdit func(EditResult)
	// DragEnabled tells whether the rows can be dragged (explorer.enableDragAndDrop); nil means yes.
	DragEnabled func() bool
	// Ctrl tells whether the Control key is held, which makes a drop a copy.
	Ctrl func() bool
	// OnDropItems is called, on the UI thread, when items of the tree are dropped on a folder of the tree: the
	// paths dragged, the folder, and whether the drop copies.
	OnDropItems func(sources []string, folder string, copy bool)
	// OnImport is called, on the UI thread, when files are dropped on the tree from another application: their
	// paths and the folder they were dropped on.
	OnImport func(paths []string, folder string)
	// OnContextMenu is called, on the UI thread, after the secondary button was pressed on a row (its path; the
	// row is focused, and selected unless it is in the selection) or on the empty part of the tree (an empty
	// path, the selection is cleared); at is the position in the window.
	OnContextMenu func(path string, at ui.Offset)
}

// View is the tree of the folder of a window without its widgets (SPEC §10.8): the model of the rows
// (a kit.TreeModel keyed by path) over a workspace.Workspace, kept up to date as the workspace changes. It
// reads the folders in the background and is used on the UI thread. NewBody is the widget that shows it, and
// the commands of the Explorer call its methods.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/views/explorerView.ts (ExplorerView) and
// src/vs/workbench/contrib/files/browser/views/explorerViewer.ts (ExplorerDataSource)
type View struct {
	ws     *workspace.Workspace
	ctx    context.Context
	cancel context.CancelFunc
	tree   *kit.TreeModel
	ctl    *kit.TreeController
	opts   ViewOpts
	stop   func()
	handle foundation.ListenerHandle

	// loaded says that the folder was read, rootErr why it could not be.
	loaded  bool
	rootErr error
	// pending is set while a rebuild of the rows waits for the UI thread.
	pending  atomic.Bool
	disposed bool
	// scrollTo is the node to scroll into view as soon as the tree is on screen.
	scrollTo  string
	listeners []*func()
	// edit is the inline input that is open, and retired the controllers of the inputs that closed, which
	// are let go when the view is, since their widgets may still be on screen for a frame.
	edit    *editState
	retired []*kit.InputBoxController
	// cut are the paths that were cut and not yet pasted; they show dimmed.
	cut map[string]bool
	// dropOver is the key of the row an external drag is over.
	dropOver string
}

// NewView makes the view of a workspace and starts reading its folder; ctx ends everything the view does in
// the background.
func NewView(ctx context.Context, ws *workspace.Workspace, opts ViewOpts) *View {
	if opts.Post == nil {
		opts.Post = func(f func()) { f() }
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	v := &View{ws: ws, opts: opts, ctl: kit.NewTreeController()}
	v.ctx, v.cancel = context.WithCancel(ctx)
	v.tree = kit.NewTreeModel(kit.TreeModelOpts{LoadChildren: v.load})
	v.stop = ws.OnChange(v.changed)
	v.handle = v.tree.AddListener(v.syncKeys)
	go v.loadRoot()
	return v
}

// Dispose ends the background work and stops following the workspace.
func (v *View) Dispose() {
	v.disposed = true
	v.cancel()
	v.stop()
	v.tree.RemoveListener(v.handle)
	v.releaseEdit()
	for _, c := range v.retired {
		c.Dispose()
	}
	v.retired = nil
	for _, key := range []string{"explorerResourceIsFolder", "explorerResourceIsRoot", "explorerResourceReadonly",
		"explorerResourceMoveableToTrash", "explorerResourceCut", "inputFocus"} {
		if v.opts.Keys != nil {
			v.opts.Keys.Reset(key)
		}
	}
}

// Workspace is the workspace the view shows.
func (v *View) Workspace() *workspace.Workspace { return v.ws }

// Tree is the model of the rows.
func (v *View) Tree() *kit.TreeModel { return v.tree }

// Controller is the controller of the tree widget: it scrolls and focuses it.
func (v *View) Controller() *kit.TreeController { return v.ctl }

// OnChange calls fn after the rows or the state of the view changed, and returns the function that stops it.
func (v *View) OnChange(fn func()) (unsubscribe func()) {
	p := &fn
	v.listeners = append(v.listeners, p)
	return func() {
		if i := slices.Index(v.listeners, p); i >= 0 {
			v.listeners = slices.Delete(v.listeners, i, i+1)
		}
	}
}

func (v *View) notify() {
	for _, f := range slices.Clone(v.listeners) {
		(*f)()
	}
}

// Loaded tells whether the folder has been read, and RootError why it could not be (nil when it could).
func (v *View) Loaded() bool { return v.loaded }

// RootError is the error of reading the folder of the window, nil when it was read.
func (v *View) RootError() error { return v.rootErr }

// loadRoot reads the folder of the window, in the background, and shows it.
func (v *View) loadRoot() {
	_, err := v.ws.Root().Children(v.ctx)
	v.opts.Post(func() {
		if v.disposed {
			return
		}
		v.loaded, v.rootErr = true, err
		v.rebuild()
	})
}

// changed follows the workspace: any change of the tree but a new size or time rebuilds the rows, once for
// any number of changes. It runs on the goroutine that changed the tree.
func (v *View) changed(c workspace.Change) {
	if c.Kind == workspace.ChangeUpdated {
		return
	}
	v.scheduleRebuild()
}

func (v *View) scheduleRebuild() {
	if !v.pending.CompareAndSwap(false, true) {
		return
	}
	v.opts.Post(func() {
		v.pending.Store(false)
		if !v.disposed {
			v.rebuild()
		}
	})
}

// rebuild makes the rows from what the workspace has read: the expansion, the focus and the selection stay,
// as the tree keeps them by key.
func (v *View) rebuild() {
	if !v.loaded {
		return
	}
	roots := v.items(v.ws.Root().Loaded())
	v.withEdit(&roots, v.ws.Path())
	if err := v.tree.SetItems(roots); err != nil {
		// the keys are paths of the tree, so a repeated one is a mistake of the model
		v.opts.Logger.Error("explorer: cannot show the folder", "err", err)
	}
	v.notify()
}

// items makes the nodes for items of the workspace that were read; a folder that was read has its
// children, one that was not is lazy.
func (v *View) items(list []*workspace.Item) []kit.TreeItem {
	out := make([]kit.TreeItem, 0, len(list))
	for _, it := range list {
		out = append(out, v.item(it))
	}
	return out
}

func (v *View) item(it *workspace.Item) kit.TreeItem {
	if !it.IsDir() {
		nd := &nodeData{Item: it}
		return kit.TreeItem{Key: it.Path(), Data: nd, Label: it.Name()}
	}
	nd := &nodeData{Item: it, Chain: v.ws.LoadedChain(it), IsDir: true}
	ti := kit.TreeItem{Key: it.Path(), Data: nd, Label: nd.label()}
	if last := nd.last(); last.Resolved() {
		ti.Children = v.items(last.Loaded())
		v.withEdit(&ti.Children, last.Path())
		ti.HasChildren = len(ti.Children) == 0
	} else {
		ti.Lazy = true
	}
	return ti
}

// load reads the children of a folder that was expanded for the first time, in the background: the chain
// of folders it starts, then the children of the last one.
func (v *View) load(key string, data any) {
	nd, ok := data.(*nodeData)
	if !ok {
		v.tree.FailLoad(key)
		return
	}
	go func() {
		chain, err := v.ws.CompactChain(v.ctx, nd.Item)
		var kids []*workspace.Item
		if err == nil {
			kids, err = chain[len(chain)-1].Children(v.ctx)
		}
		// as VS Code resolves the single child descendants of a folder it reads, a folder that is the only
		// child shows its own chain at once
		if err == nil && len(kids) == 1 && kids[0].IsDir() {
			if _, cerr := v.ws.CompactChain(v.ctx, kids[0]); cerr != nil {
				v.opts.Logger.Debug("explorer: cannot read the chain below a folder", "path", kids[0].Path(), "err", cerr)
			}
		}
		v.opts.Post(func() {
			if v.disposed || !v.tree.Has(key) {
				return
			}
			if err != nil {
				v.tree.FailLoad(key)
				if v.opts.OnLoadFailed != nil {
					v.opts.OnLoadFailed(nd.Item.Path(), err)
				}
				v.notify()
				return
			}
			nd.Chain = chain
			children := v.items(kids)
			v.withEdit(&children, chain[len(chain)-1].Path())
			if err := v.tree.SetChildren(key, children); err != nil {
				v.opts.Logger.Error("explorer: cannot show a folder", "path", key, "err", err)
			}
			v.notify()
		})
	}()
}

// Refresh reads the folders that were read again, and shows what changed. It runs in the background.
func (v *View) Refresh() {
	go func() {
		if err := v.ws.Refresh(v.ctx, nil); err != nil {
			v.opts.Logger.Debug("explorer: refresh", "err", err)
		}
		v.scheduleRebuild()
	}()
}

// CollapseAll folds every folder of the tree.
func (v *View) CollapseAll() {
	v.tree.CollapseAll()
	v.notify()
}

// Focus gives the keyboard focus to the tree.
func (v *View) Focus() { v.ctl.Focus() }

// HasFocus tells whether the tree has the keyboard focus.
func (v *View) HasFocus() bool { return v.ctl.HasFocus() }

// RevealMode says what revealing a file does.
type RevealMode int

const (
	// RevealSelect expands the folders above the file, selects and focuses its row and scrolls to it
	// (explorer.autoReveal true).
	RevealSelect RevealMode = iota
	// RevealNoScroll does the same without scrolling (explorer.autoReveal "focusNoScroll").
	RevealNoScroll
)

// Reveal shows a file or folder of the folder of the window in the tree. The folders above it are read in the
// background first; done is told, on the UI thread, whether the path was found (a path outside the folder, or
// one that files.exclude hides, is not).
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/views/explorerView.ts (selectResource)
func (v *View) Reveal(path string, mode RevealMode, done func(found bool)) {
	finish := func(found bool) {
		if done != nil {
			done(found)
		}
	}
	root := v.ws.Path()
	rel, err := filepath.Rel(root, filepath.Clean(path))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		finish(false)
		return
	}
	go func() {
		cur := v.ws.Root()
		found := true
		for _, name := range strings.Split(rel, string(filepath.Separator)) {
			if _, err := cur.Children(v.ctx); err != nil {
				found = false
				break
			}
			if cur = cur.Child(name); cur == nil {
				found = false
				break
			}
		}
		v.opts.Post(func() {
			if v.disposed || !found {
				finish(false)
				return
			}
			v.loaded = true
			v.rebuild()
			key := v.KeyOf(path)
			if key == "" {
				finish(false)
				return
			}
			v.tree.Reveal(key)
			v.tree.SetSelected(key)
			v.tree.SetFocus(key)
			if mode == RevealSelect {
				v.scrollTo = key
			}
			v.notify()
			finish(true)
		})
	}()
}

// KeyOf is the key of the node that shows a path: the path itself, or the folder at the start of the chain
// that holds it. It is empty when the tree shows none.
func (v *View) KeyOf(path string) string {
	root := v.ws.Path()
	for p := filepath.Clean(path); p != root && p != filepath.Dir(p); p = filepath.Dir(p) {
		if v.tree.Has(p) {
			return p
		}
	}
	return ""
}

// flushScroll scrolls a node that was revealed into view once the tree is on screen.
func (v *View) flushScroll() {
	if v.scrollTo != "" && v.ctl.Reveal(v.scrollTo) {
		v.scrollTo = ""
	}
}

// FocusedItem is the item of the focused row, or nil.
func (v *View) FocusedItem() *workspace.Item {
	key, ok := v.tree.Focused()
	if !ok {
		return nil
	}
	if data, ok := v.tree.Data(key); ok {
		if nd, ok := data.(*nodeData); ok {
			return nd.last()
		}
	}
	return nil
}

// SelectedPath is the path of the selected node, or "".
func (v *View) SelectedPath() string {
	if key, ok := v.tree.Selected(); ok {
		return key
	}
	return ""
}

// syncKeys sets the context keys that describe the focused row. With no row focused the context is the
// folder of the window, as in VS Code: a folder, and the root.
func (v *View) syncKeys() {
	keys := v.opts.Keys
	if keys == nil {
		return
	}
	item := v.FocusedItem()
	trash := v.opts.TrashEnabled == nil || v.opts.TrashEnabled()
	keys.Batch(func() {
		keys.Set("explorerResourceIsFolder", item == nil || item.IsDir())
		keys.Set("explorerResourceIsRoot", item == nil)
		keys.Set("explorerResourceReadonly", false)
		keys.Set("explorerResourceMoveableToTrash", item != nil && trash)
		keys.Set("explorerResourceCut", len(v.cut) > 0)
	})
}

// SyncKeys sets the context keys again, after a setting they depend on changed.
func (v *View) SyncKeys() { v.syncKeys() }

// SetCut marks paths as cut: their rows show dimmed until they are pasted (SetCut with none), the clipboard
// changes, or Escape cancels the cut.
func (v *View) SetCut(paths []string) {
	v.cut = map[string]bool{}
	for _, p := range paths {
		v.cut[p] = true
	}
	v.syncKeys()
	v.notify()
}

// CutPaths are the paths that are cut.
func (v *View) CutPaths() []string {
	out := make([]string, 0, len(v.cut))
	for p := range v.cut {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

// IsCut tells whether a path is cut.
func (v *View) IsCut(path string) bool { return v.cut[path] }

// Context are the items the commands of the Explorer work on: the focused row, and with respectMulti the
// whole selection when the focused row is part of it (VS Code's `explorerService.getContext`). It is
// empty when no row is focused, which means the folder of the window.
func (v *View) Context(respectMulti bool) []*workspace.Item {
	focused, ok := v.tree.Focused()
	if !ok {
		return nil
	}
	if respectMulti && v.tree.IsSelected(focused) {
		var out []*workspace.Item
		for _, key := range v.tree.SelectedKeys() {
			if it := v.itemOf(key); it != nil {
				out = append(out, it)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if it := v.itemOf(focused); it != nil {
		return []*workspace.Item{it}
	}
	return nil
}

// itemOf is the item a row stands for (the last folder of a compact chain), or nil.
func (v *View) itemOf(key string) *workspace.Item {
	if data, ok := v.tree.Data(key); ok {
		if nd, ok := data.(*nodeData); ok {
			return nd.last()
		}
	}
	return nil
}

// SelectedPaths are the paths of the selected rows.
func (v *View) SelectedPaths() []string {
	var out []string
	for _, key := range v.tree.SelectedKeys() {
		if it := v.itemOf(key); it != nil {
			out = append(out, it.Path())
		}
	}
	return out
}

// open opens the file of a node.
func (v *View) open(node kit.TreeNode, pinned bool) {
	nd, ok := node.Data.(*nodeData)
	if !ok || nd.IsDir || nd.Item == nil || v.opts.OnOpen == nil {
		return
	}
	v.opts.OnOpen(nd.Item.Path(), pinned)
}

// iconFor chooses the icon of a node, or reports false when there is none.
func (v *View) iconFor(nd *nodeData, expanded bool, kind theme.Type) (theme.FileIcon, bool) {
	if v.opts.Icons == nil {
		return theme.FileIcon{}, false
	}
	th := v.opts.Icons()
	if th == nil {
		return theme.FileIcon{}, false
	}
	it := nd.last()
	if it == nil {
		return theme.FileIcon{}, false
	}
	q := theme.FileIconQuery{Kind: theme.FileIconFile, Name: it.Name(), Expanded: expanded, Type: kind}
	if parent := it.Parent(); parent != nil {
		q.Parent = parent.Name()
	}
	if nd.IsDir {
		q.Kind = theme.FileIconFolder
	} else if v.opts.Language != nil {
		q.LanguageID = v.opts.Language(it.Path())
	}
	return th.IconFor(q)
}
