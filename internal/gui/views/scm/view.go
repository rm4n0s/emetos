package scm

import (
	"encoding/json"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/rm4n0s/errors"

	scmcore "github.com/rm4n0s/emetos/internal/core/scm"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// ViewID is the id of the Changes view (VS Code's VIEW_PANE_ID) and RepositoriesID the id of the Repositories view.
const (
	ViewID         = "workbench.scm"
	RepositoriesID = "workbench.scm.repositories"
)

// rowKind says what a row of the Changes view is.
type rowKind int

const (
	// kindRepository is the header of a repository, shown when several are or scm.alwaysShowRepositories is on.
	kindRepository rowKind = iota
	// kindGroup is the header of a group of resources.
	kindGroup
	// kindFolder is a folder of the tree view mode, or a chain of folders joined by compact folders.
	kindFolder
	// kindResource is a changed file.
	kindResource
)

// row is the data of a row of the Changes view.
type row struct {
	kind     rowKind
	src      *scmcore.Source
	group    *scmcore.Group
	res      *scmcore.Resource
	entry    scmcore.Entry // of a folder
	depth    int
	expanded bool // of a repository, a group and a folder
	// collapse is the key the collapsed state of the row is kept under: the same for a repository, group or folder whatever
	// the handle the source control got in this session.
	collapse string
}

// View is the state of the Changes view: the rows of the visible repositories, their collapsed state, the focus and the
// selection (in the list model), and what the model changed since the rows were made. It is used on the UI thread.
type View struct {
	opts  Options
	model *scmcore.Model
	list  *kit.ListViewModel
	ctl   *kit.ListController

	collapsed map[string]bool
	// revealed is the file that the last reveal selected, so that a rebuild does not scroll back to it.
	scrollTo string
	// version counts the rebuilds; the inline actions of rows are cached for one.
	version uint64
	actions map[*row][]kit.Action

	drag rowDrag
	// forceHover is the key of a row drawn as hovered (SetHover).
	forceHover string
	dirty      atomic.Bool
	structure  atomic.Bool
	stop       func()
	listeners  map[int]func()
	next       int
	disposed   bool

	// collapseAllKeys are the keys of the repositories, for the context keys of the title.
	lastRepos int
}

// New makes the Changes view of a model and follows it.
func New(opts Options) *View {
	v := &View{
		opts: opts, model: opts.Model, collapsed: map[string]bool{}, actions: map[*row][]kit.Action{},
		ctl: kit.NewListController(), listeners: map[int]func(){},
		list: kit.NewListViewModel(kit.ListViewModelOpts{MultiSelect: true}),
	}
	v.stop = v.model.Subscribe(v.onModelEvent)
	v.rebuild()
	return v
}

// SetHover draws the row of a file as the one under the pointer (a proof image shows the inline actions that way); "" ends it.
func (v *View) SetHover(uri string) {
	v.forceHover = ""
	if uri != "" {
		for _, s := range v.model.Visible() {
			for _, g := range s.Groups() {
				if findResource(g, uri) != nil {
					v.forceHover = "f:" + s.Key() + ":" + g.ID() + ":" + uri
				}
			}
		}
	}
	v.notify()
}

// Dispose stops following the model.
func (v *View) Dispose() {
	v.disposed = true
	if v.stop != nil {
		v.stop()
	}
}

// Model is the model the view shows.
func (v *View) Model() *scmcore.Model { return v.model }

// List is the model of the list; tests read the rows from it.
func (v *View) List() *kit.ListViewModel { return v.list }

// OnChange calls fn each time the view has to be drawn again; it returns the function that stops the calls.
func (v *View) OnChange(fn func()) (stop func()) {
	v.next++
	id := v.next
	v.listeners[id] = fn
	return func() { delete(v.listeners, id) }
}

func (v *View) notify() {
	for _, fn := range slices.Collect(mapValues(v.listeners)) {
		fn()
	}
}

func mapValues(m map[int]func()) func(yield func(func()) bool) {
	return func(yield func(func()) bool) {
		for _, fn := range m {
			if !yield(fn) {
				return
			}
		}
	}
}

// onModelEvent runs on the goroutine that changed the model: the work is handed to the UI thread once for a burst of changes.
func (v *View) onModelEvent(e scmcore.Event) {
	switch e.Kind {
	case scmcore.SourceChanged, scmcore.InputChanged:
		// the count, the status items, the busy mark: the rows stay
	default:
		v.structure.Store(true)
		v.warmSorted()
	}
	if v.dirty.Swap(true) {
		return
	}
	post := v.opts.Post
	if post == nil {
		post = func(f func()) { f() }
	}
	post(v.flush)
}

// warmSorted makes the sorted lists of the groups here, on the goroutine that changed the model, so that the UI thread finds them
// made when it builds the rows (a sort of 100,000 resources by path takes 200 ms; SPEC §14 asks for the first frame within 100 ms
// of the update). The tree view mode has no such list: its tree belongs to the UI thread.
func (v *View) warmSorted() {
	if v.model.ViewMode() != scmcore.ViewList {
		return
	}
	key := v.model.SortKey()
	for _, src := range v.model.Visible() {
		for _, g := range src.Groups() {
			if g.Len() > 1 {
				g.Sorted(key)
			}
		}
	}
}

// flush makes the rows again when the model changed their structure, and tells the widgets.
func (v *View) flush() {
	v.dirty.Store(false)
	if v.disposed {
		return
	}
	if v.structure.Swap(false) {
		v.rebuild()
		return
	}
	v.version++
	clear(v.actions)
	v.notify()
}

// Refresh makes the rows again from the model, for a change the view was not told of (a setting).
func (v *View) Refresh() { v.rebuild() }

// showRepositories tells whether the repositories have headers: several are shown, or scm.alwaysShowRepositories.
func (v *View) showRepositories(visible int) bool {
	return visible > 1 || v.opts.flag(v.opts.AlwaysShowRepositories)
}

// rebuild makes the rows of the visible repositories, folded as the user left them.
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmViewPane.ts (SCMTreeDataSource.getChildren)
func (v *View) rebuild() {
	vis := v.model.Visible()
	mode, sortKey, compact := v.model.ViewMode(), v.model.SortKey(), v.model.CompactFolders()
	showRepos := v.showRepositories(len(vis))
	hint := 0
	for _, src := range vis {
		hint++
		for _, g := range src.Groups() {
			hint += 1 + g.Len()
		}
	}
	items := make([]kit.ListViewItem, 0, hint)
	add := func(key, label string, r *row) {
		items = append(items, kit.ListViewItem{Key: key, Label: label, Data: r})
	}
	for _, src := range vis {
		depth := 0
		if showRepos {
			r := &row{kind: kindRepository, src: src, collapse: "r\x00" + src.StorageKey()}
			r.expanded = !v.collapsed[r.collapse]
			add("r:"+src.Key(), src.Name(), r)
			if !r.expanded {
				continue
			}
			depth = 1
		}
		groups := src.Groups()
		some := slices.ContainsFunc(groups, func(g *scmcore.Group) bool { return g.Len() > 0 })
		// TODO(step 24.4): the input and the action button rows come before the groups; the groups are listed when a repository
		// has changes, or when it is the only one and shows no action button.
		if !some && len(vis) != 1 {
			continue
		}
		for _, g := range groups {
			if !g.Visible() {
				continue
			}
			gr := &row{kind: kindGroup, src: src, group: g, depth: depth, collapse: "g\x00" + src.StorageKey() + "\x00" + g.ID()}
			gr.expanded = !v.collapsed[gr.collapse]
			add("g:"+src.Key()+":"+g.ID(), g.Label(), gr)
			if !gr.expanded {
				continue
			}
			if mode == scmcore.ViewTree {
				items = v.addTree(items, src, g, g.Tree(), g.Tree().Root(), depth+1, compact)
				continue
			}
			prefix := "f:" + src.Key() + ":" + g.ID() + ":"
			for _, res := range g.Sorted(sortKey) {
				add(prefix+res.URI, res.Name(), &row{kind: kindResource, src: src, group: g, res: res, depth: depth + 1})
			}
		}
	}
	if err := v.list.SetItems(items); err != nil {
		// two resources with one URI in a group: the later is left out, the view still shows the rest
		items = dedupe(items)
		if err := v.list.SetItems(items); err != nil {
			// the keys are made here, so this cannot be reached; an empty view says so best
			_ = v.list.SetItems(nil)
		}
	}
	v.lastRepos = len(vis)
	v.version++
	clear(v.actions)
	v.notify()
}

// addTree adds the rows of the children of a node of the tree view mode.
func (v *View) addTree(items []kit.ListViewItem, src *scmcore.Source, g *scmcore.Group, t *scmcore.Tree, n *scmcore.Node, depth int, compact bool) []kit.ListViewItem {
	prefix := src.Key() + ":" + g.ID() + ":"
	for _, e := range t.Children(n, compact) {
		if e.IsFolder() {
			r := &row{kind: kindFolder, src: src, group: g, entry: e, depth: depth, collapse: "d\x00" + src.StorageKey() + "\x00" + g.ID() + "\x00" + e.Node.Path()}
			r.expanded = !v.collapsed[r.collapse]
			items = append(items, kit.ListViewItem{Key: "d:" + prefix + e.Node.Path(), Label: e.Label, Data: r})
			if r.expanded {
				items = v.addTree(items, src, g, t, e.Node, depth+1, compact)
			}
			continue
		}
		items = append(items, kit.ListViewItem{Key: "f:" + prefix + e.Resource.URI, Label: e.Label,
			Data: &row{kind: kindResource, src: src, group: g, res: e.Resource, depth: depth}})
	}
	return items
}

// dedupe drops the items whose key came before.
func dedupe(items []kit.ListViewItem) []kit.ListViewItem {
	seen := make(map[string]bool, len(items))
	out := items[:0]
	for _, it := range items {
		if !seen[it.Key] {
			seen[it.Key] = true
			out = append(out, it)
		}
	}
	return out
}

// rowOf is the data of a row of the list.
func rowOf(r kit.ListViewRow) *row {
	d, _ := r.Data.(*row)
	return d
}

// RowCount is the number of rows.
func (v *View) RowCount() int { return len(v.list.Rows()) }

// Empty tells whether the view shows no repository: the welcome content stands in place of the list.
func (v *View) Empty() bool { return v.model.Count() == 0 }

// ---- collapsing ----

// toggle folds or unfolds a repository, a group or a folder.
func (v *View) toggle(r *row) {
	if r.kind == kindResource {
		return
	}
	v.setCollapsed(r, r.expanded)
}

func (v *View) setCollapsed(r *row, collapsed bool) {
	if r.expanded != collapsed {
		return
	}
	if collapsed {
		v.collapsed[r.collapse] = true
	} else {
		delete(v.collapsed, r.collapse)
	}
	v.rebuild()
}

// CollapseAllRepositories folds the headers of the repositories (workbench.scm.action.collapseAllRepositories).
func (v *View) CollapseAllRepositories() { v.setAllRepositories(true) }

// ExpandAllRepositories unfolds them.
func (v *View) ExpandAllRepositories() { v.setAllRepositories(false) }

func (v *View) setAllRepositories(collapsed bool) {
	if !v.showRepositories(len(v.model.Visible())) {
		return
	}
	for _, s := range v.model.Visible() {
		k := "r\x00" + s.StorageKey()
		if collapsed {
			v.collapsed[k] = true
		} else {
			delete(v.collapsed, k)
		}
	}
	v.rebuild()
}

// CollapseAll folds every folder of a group of the tree view mode (workbench.scm.action.collapseAll).
func (v *View) CollapseAll(g *scmcore.Group) {
	src := g.Source()
	var walk func(n *scmcore.Node)
	t := g.Tree()
	walk = func(n *scmcore.Node) {
		for _, e := range t.Children(n, v.model.CompactFolders()) {
			if e.IsFolder() {
				v.collapsed["d\x00"+src.StorageKey()+"\x00"+g.ID()+"\x00"+e.Node.Path()] = true
				walk(e.Node)
			}
		}
	}
	walk(t.Root())
	v.rebuild()
}

// RepositoryKeys tells whether any repository can be folded and whether all of them are: the context keys
// scmViewIsAnyRepositoryCollapsible and scmViewAreAllRepositoriesCollapsed.
func (v *View) RepositoryKeys() (collapsible, allCollapsed bool) {
	vis := v.model.Visible()
	if !v.showRepositories(len(vis)) || len(vis) == 0 {
		return false, false
	}
	all := true
	for _, s := range vis {
		if !v.collapsed["r\x00"+s.StorageKey()] {
			all = false
		}
	}
	return true, all
}

// ---- state ----

type savedState struct {
	// Collapsed are the keys of the repositories, groups and folders that were folded, joined with a NUL.
	Collapsed []string `json:"collapsed,omitempty"`
}

// SaveState is the state of the view that outlives the window: what was folded, per repository (VS Code keeps the view state of
// its tree in scm.viewState2).
func (v *View) SaveState() json.RawMessage {
	if len(v.collapsed) == 0 {
		return nil
	}
	keys := make([]string, 0, len(v.collapsed))
	for k := range v.collapsed {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	data, err := json.Marshal(savedState{Collapsed: keys})
	if err != nil {
		return nil
	}
	return data
}

// RestoreState gives the view the state SaveState gave. It fails with ScmViewStateInvalid for a text that is not that state.
func (v *View) RestoreState(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var st savedState
	if err := json.Unmarshal(raw, &st); err != nil {
		return errors.NewErr("ScmViewStateInvalid", err)
	}
	clear(v.collapsed)
	for _, k := range st.Collapsed {
		if strings.Count(k, "\x00") >= 1 {
			v.collapsed[k] = true
		}
	}
	v.rebuild()
	return nil
}

// Collapsed tells whether a repository, group or folder is folded, by the key of its row (tests).
func (v *View) Collapsed(key string) bool {
	it, ok := v.list.Item(key)
	if !ok {
		return false
	}
	r, _ := it.Data.(*row)
	return r != nil && r.kind != kindResource && !r.expanded
}
