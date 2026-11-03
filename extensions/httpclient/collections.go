package httpclient

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/widgets"

	gui "github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
	"github.com/rm4n0s/emetos/extensions/httpclient/store"
)

// collectionsView is the Collections view: the tree of collections, folders and requests. It is used on the UI thread.
type collectionsView struct {
	e      *Extension
	handle extension.ViewHandle
	tree   *ui.TreeModel
	ctl    *ui.TreeController
	menu   *ui.ContextMenuController
	// seeded says that the collections were expanded the first time there were any.
	seeded bool
}

// newCollectionsView makes the view.
func newCollectionsView(e *Extension) *collectionsView {
	return &collectionsView{
		e: e, ctl: ui.NewTreeController(), menu: ui.NewContextMenuController(),
		tree: ui.NewTreeModel(ui.TreeModelOpts{FindMode: ui.FindModeFilter}),
	}
}

// provide gives the view its content.
func (v *collectionsView) provide() error {
	h, err := v.e.api.Views().Provide(CollectionsViewID, extension.ViewContent{Build: v.build})
	if err != nil {
		return err
	}
	v.handle = h
	return nil
}

// rebuild makes the rows from the tree of the store. The expansion and the selection are kept.
func (v *collectionsView) rebuild() {
	if v.handle == nil {
		return
	}
	var items []ui.TreeItem
	var expand []string
	if t := v.e.tree; t != nil {
		for _, c := range t.Collections {
			items = append(items, nodeItem(c))
			expand = append(expand, c.Path)
		}
	}
	expanded := v.tree.ExpandedKeys()
	if !v.seeded && len(items) > 0 {
		expanded, v.seeded = append(expanded, expand...), true
	}
	selected, hadSelection := v.tree.Selected()
	if err := v.tree.SetItems(items); err != nil {
		v.e.api.Log().Warn("the tree of collections cannot be made", "err", err)
		return
	}
	v.tree.SetExpandedKeys(expanded)
	if hadSelection && v.tree.Has(selected) {
		v.tree.SetSelected(selected)
	}
	v.handle.Refresh()
}

// nodeItem makes the item of a node and of the nodes below it.
func nodeItem(n *store.Node) ui.TreeItem {
	item := ui.TreeItem{Key: n.Path, Data: n, Label: n.Name}
	for _, c := range n.Children {
		item.Children = append(item.Children, nodeItem(c))
	}
	item.HasChildren = n.Kind == store.KindCollection || n.Kind == store.KindFolder
	return item
}

// nodeOf is the node a key of the tree stands for.
func (v *collectionsView) nodeOf(key string) *store.Node { return v.e.tree.Find(key) }

// selectedNode is the node that is selected, or nil.
func (v *collectionsView) selectedNode() *store.Node {
	key, ok := v.tree.Selected()
	if !ok {
		return nil
	}
	return v.nodeOf(key)
}

// reveal selects the node with the path, opening the nodes above it.
func (v *collectionsView) reveal(path string) {
	if v.tree.Has(path) {
		v.tree.Reveal(path)
		v.tree.SetSelected(path)
		v.handle.Refresh()
	}
}

// collapseAll folds the tree.
func (v *collectionsView) collapseAll() { v.tree.Perform(ui.TreeActionCollapseAll) }

// ---- the widget ----

// build makes the widget of the view: what is wrong or missing, then the tree.
func (v *collectionsView) build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	e := v.e
	var children []widgets.Widget
	switch {
	case e.folder == "":
		children = append(children, note(bc, "Open a folder to use the HTTP client: its collections are files of the folder.", colors.DescriptionForeground))
	case e.loadErr != nil:
		children = append(children, note(bc, "The collections cannot be read: "+e.loadErr.Error(), colors.ErrorForeground))
	case e.store == nil:
		children = append(children, note(bc, "Reading the collections...", colors.DescriptionForeground))
	case len(e.tree.Collections) == 0:
		children = append(children, note(bc, "There are no collections in this folder yet. A collection is a folder of requests in .emetos/http that you can share with git.", colors.DescriptionForeground),
			widgets.NewPadding(paddingH(12), ui.NewButton("New Collection...", ui.ButtonOpts{OnPressed: func() { e.newCollection() }})))
	}
	if e.tree != nil && len(e.tree.Collections) > 0 {
		tree := ui.NewTree(v.tree, ui.TreeOpts{
			Controller: v.ctl, Render: v.render, OnOpen: v.onOpen, OnContextMenu: v.onContextMenu, DragAndDrop: v.dragAndDrop(),
		})
		children = append(children, widgets.NewExpanded(widgets.NewFocus(tree, widgets.FocusOpts{
			OnKeyEvent: v.onKey, CanRequestFocus: foundation.Some(false), SkipTraversal: foundation.Some(true),
		})))
	}
	return ui.NewContextMenu(column(children...), ui.ContextMenuOpts{Controller: v.menu})
}

// onKey is F2 (rename) and Delete (delete) on the row that is selected, keys the tree leaves alone.
func (v *collectionsView) onKey(_ *widgets.FocusNode, ev services.KeyEvent) widgets.KeyEventResult {
	if _, down := ev.(*services.KeyDownEvent); !down {
		return widgets.KeyEventResultIgnored
	}
	n := v.selectedNode()
	if n == nil {
		return widgets.KeyEventResultIgnored
	}
	switch ev.LogicalKey() {
	case services.LogicalKeyboardKeyF2:
		v.e.rename(n)
	case services.LogicalKeyboardKeyDelete:
		v.e.delete(n)
	default:
		return widgets.KeyEventResultIgnored
	}
	return widgets.KeyEventResultHandled
}

// render builds the contents of a row.
func (v *collectionsView) render(bc widgets.BuildContext, tn ui.TreeNode, st ui.RowState) widgets.Widget {
	colors := ui.Theme(bc)
	n, ok := tn.Data.(*store.Node)
	if !ok {
		return rowText(bc, "", st.Foreground)
	}
	center := widgets.RowOpts{CrossAxisAlignment: crossCenter}
	var row []widgets.Widget
	switch n.Kind {
	case store.KindRequest:
		if n.Request == nil { // a file that cannot be read has no method
			row = append(row, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: optSome(38.0)}))
		} else {
			row = append(row, methodBadge(bc, n.Request.Method))
		}
	case store.KindCollection:
		row = append(row, ui.Icon("library", 16, st.Foreground), gap(6))
	default:
		icon := "folder"
		if tn.Expanded {
			icon = "folder-opened"
		}
		row = append(row, ui.Icon(icon, 16, st.Foreground), gap(6))
	}
	row = append(row, widgets.NewFlexible(rowText(bc, n.Name, st.Foreground)))
	tip := ""
	switch {
	case n.ReadOnly:
		row = append(row, gap(6), ui.Icon("lock", 12, colors.DescriptionForeground))
		tip = "From a newer version of the format: it is shown read-only."
	case n.Err != nil:
		row = append(row, gap(6), ui.Icon("error", 12, colors.ErrorForeground))
		tip = n.Err.Error()
	}
	row = append(row, gap(8))
	var out widgets.Widget = widgets.NewRow(row, center)
	if tip != "" {
		out = ui.NewHover(out, ui.HoverOpts{Text: tip, Compact: true})
	}
	return out
}

// onOpen is Enter or a double click: a request is opened.
func (v *collectionsView) onOpen(tn ui.TreeNode) {
	if n, ok := tn.Data.(*store.Node); ok && n.Kind == store.KindRequest {
		v.e.openNode(n)
	}
}

// onContextMenu opens the menu of a row at the place of the click.
func (v *collectionsView) onContextMenu(key string, at gui.Offset) {
	e := v.e
	if key == "" {
		v.menu.Show(at, []ui.MenuItem{{Label: "New Collection...", OnRun: e.newCollection}})
		return
	}
	n := v.nodeOf(key)
	if n == nil {
		return
	}
	v.tree.SetSelected(key)
	run := func(fn func(*store.Node)) func() { return func() { fn(n) } }
	var items []ui.MenuItem
	switch n.Kind {
	case store.KindCollection, store.KindFolder:
		runLabel := "Run Folder..."
		if n.Kind == store.KindCollection {
			runLabel = "Run Collection..."
		}
		items = append(items,
			ui.MenuItem{Label: runLabel, OnRun: func() { e.runGroup(n, nil) }},
			ui.SeparatorItem(),
			ui.MenuItem{Label: "New Request...", Disabled: n.ReadOnly, OnRun: func() { e.newRequest(n.Path) }},
			ui.MenuItem{Label: "New Folder...", Disabled: n.ReadOnly, OnRun: func() { e.newFolder(n.Path) }},
			ui.SeparatorItem())
	case store.KindRequest:
		items = append(items, ui.MenuItem{Label: "Open", OnRun: run(e.openNode)}, ui.SeparatorItem())
	}
	items = append(items,
		ui.MenuItem{Label: "Rename...", Keybinding: "F2", Disabled: n.ReadOnly, OnRun: run(e.rename)},
		ui.MenuItem{Label: "Duplicate", Disabled: n.ReadOnly, OnRun: run(e.duplicate)},
		ui.MenuItem{Label: "Delete...", Keybinding: "Delete", OnRun: run(e.delete)},
		ui.SeparatorItem(),
		ui.MenuItem{Label: "Copy Name", OnRun: func() { e.copyText(n.Name) }})
	v.menu.Show(at, items)
}

// ---- drag and drop ----

// dropPlan is where a drop of a node on a row would put it: into the collection or folder dest, at the place index among its entries
// (without the node itself), -1 for last.
type dropPlan struct {
	node  *store.Node
	dest  *store.Node
	index int
}

// dragAndDrop is the drag and drop of the rows: a request or a folder is dragged and dropped on a collection or folder (it goes last
// in it), or between the rows of a collection or folder (it goes there, in the order of the file).
func (v *collectionsView) dragAndDrop() *ui.DragAndDrop {
	return &ui.DragAndDrop{
		CanDrag: func(keys []string) bool {
			if len(keys) != 1 {
				return false
			}
			n := v.nodeOf(keys[0])
			return n != nil && (n.Kind == store.KindRequest || n.Kind == store.KindFolder) && !n.ReadOnly && n.Err == nil
		},
		CanDrop: func(target string, pos ui.DropPosition, keys []string) ui.DropEffect {
			if _, ok := v.plan(target, pos, keys); ok {
				return ui.DropEffectMove
			}
			return ui.DropEffectNone
		},
		OnDrop: func(target string, pos ui.DropPosition, keys []string) {
			if p, ok := v.plan(target, pos, keys); ok {
				v.e.move(p)
			}
		},
	}
}

// plan works out what a drop does; false when it does nothing or cannot be done.
func (v *collectionsView) plan(target string, pos ui.DropPosition, keys []string) (dropPlan, bool) {
	if len(keys) != 1 || target == "" {
		return dropPlan{}, false
	}
	n, t := v.nodeOf(keys[0]), v.nodeOf(target)
	if n == nil || t == nil || (n.Kind != store.KindRequest && n.Kind != store.KindFolder) || t.Path == n.Path {
		return dropPlan{}, false
	}
	isGroup := t.Kind == store.KindCollection || t.Kind == store.KindFolder
	var p dropPlan
	switch {
	case pos == ui.DropInside && isGroup:
		p = dropPlan{node: n, dest: t, index: -1}
	case t.Kind == store.KindCollection:
		return dropPlan{}, false // collections are not ordered among themselves
	default:
		parent := v.e.tree.Parent(t)
		if parent == nil {
			return dropPlan{}, false
		}
		var siblings []string
		for _, c := range parent.Children {
			if c.Path != n.Path {
				siblings = append(siblings, c.Path)
			}
		}
		at := indexOf(siblings, t.Path)
		if at < 0 {
			return dropPlan{}, false
		}
		if pos == ui.DropAfter || pos == ui.DropInside {
			at++
		}
		p = dropPlan{node: n, dest: parent, index: at}
	}
	if p.dest.ReadOnly || p.dest.Err != nil {
		return dropPlan{}, false
	}
	if p.dest.Path == n.Path || strings.HasPrefix(p.dest.Path, n.Path+string(filepath.Separator)) {
		return dropPlan{}, false
	}
	if parent := v.e.tree.Parent(n); parent != nil && parent.Path == p.dest.Path && p.unchanged(parent) {
		return dropPlan{}, false
	}
	return p, true
}

// unchanged says that the move would leave the node where it is.
func (p dropPlan) unchanged(parent *store.Node) bool {
	var siblings []string
	cur := -1
	for _, c := range parent.Children {
		if c.Path == p.node.Path {
			cur = len(siblings)
			continue
		}
		siblings = append(siblings, c.Path)
	}
	at := p.index
	if at < 0 || at > len(siblings) {
		at = len(siblings)
	}
	return at == cur
}

func indexOf(list []string, s string) int {
	for i, x := range list {
		if x == s {
			return i
		}
	}
	return -1
}

// move moves a node as the plan says, and selects it where it went.
func (e *Extension) move(p dropPlan) {
	var path string
	e.async(func(ctx context.Context) (err error) {
		path, err = e.store.Move(ctx, p.node.Path, p.dest.Path, p.index)
		return err
	}, func(err error) {
		if err != nil {
			e.fail("move \""+p.node.Name+"\"", err)
			return
		}
		e.collections.tree.Expand(p.dest.Path)
		e.collections.reveal(path)
	})
}
