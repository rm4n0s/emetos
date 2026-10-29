package dbconn

import (
	"context"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// connRow and nodeRow are the data of the rows of the tree: a connection, and a node an extension listed below one.
type (
	connRow struct{ conn Connection }
	nodeRow struct {
		connID string
		node   Node
	}
)

// connKey is the key of the row of a connection, and childKey the key of a node below a row.
func connKey(id string) string { return "c:" + id }

// childKey is the key of a node below the row with the key.
func childKey(parent, nodeID string) string { return parent + "/" + nodeID }

// view is the view of the connections: the tree with the connections at its roots and, below a connected one, the nodes its
// extension lists. It is used on the UI thread.
type view struct {
	e      *Extension
	handle extension.ViewHandle
	tree   *ui.TreeModel
	ctl    *ui.TreeController
	menu   *ui.ContextMenuController

	// children are the nodes already listed, by the key of the row they hang from; loading says which rows are being read.
	children map[string][]Node
	loading  map[string]context.CancelFunc
	// gen counts the times the cache was dropped, so that the answer of a read that was replaced is not used.
	gen int
}

// newView makes the view.
func newView(e *Extension) *view {
	v := &view{e: e, ctl: ui.NewTreeController(), menu: ui.NewContextMenuController(),
		children: map[string][]Node{}, loading: map[string]context.CancelFunc{}}
	v.tree = ui.NewTreeModel(ui.TreeModelOpts{LoadChildren: v.load, FindMode: ui.FindModeFilter})
	return v
}

// provide gives the view its content.
func (v *view) provide() error {
	h, err := v.e.api.Views().Provide(ViewID, extension.ViewContent{Build: v.build})
	if err != nil {
		return err
	}
	v.handle = h
	v.rebuild()
	return nil
}

// rebuild makes the rows from the connections, their states and the nodes read so far. The expansion is kept.
func (v *view) rebuild() {
	if v.handle == nil {
		return
	}
	var roots []ui.TreeItem
	for _, c := range v.e.sortedConnections() {
		st := v.e.hub.State(c.ID)
		key := connKey(c.ID)
		if st.Status != Connected {
			v.forget(key)
		}
		item := ui.TreeItem{Key: key, Data: connRow{c}, Label: c.Name}
		if st.Status == Connected {
			item.Lazy = true
			item.Children, item.Lazy = v.itemsBelow(c.ID, key)
		}
		roots = append(roots, item)
	}
	expanded := v.tree.ExpandedKeys()
	if err := v.tree.SetItems(roots); err != nil {
		v.e.api.Log().Warn("the tree of connections cannot be made", "err", err)
		return
	}
	v.tree.SetExpandedKeys(expanded)
	v.handle.Refresh()
}

// itemsBelow makes the items of the nodes already listed below a row; lazy says that none were listed yet.
func (v *view) itemsBelow(connID, parent string) (items []ui.TreeItem, lazy bool) {
	nodes, ok := v.children[parent]
	if !ok {
		return nil, true
	}
	for _, n := range nodes {
		key := childKey(parent, n.ID)
		item := ui.TreeItem{Key: key, Data: nodeRow{connID, n}, Label: n.Label}
		if n.Expandable {
			item.Children, item.Lazy = v.itemsBelow(connID, key)
		}
		items = append(items, item)
	}
	return items, false
}

// forget drops what was listed below a row and cancels what is being read.
func (v *view) forget(key string) {
	for k, cancel := range v.loading {
		if k == key || strings.HasPrefix(k, key+"/") {
			cancel()
			delete(v.loading, k)
		}
	}
	for k := range v.children {
		if k == key || strings.HasPrefix(k, key+"/") {
			delete(v.children, k)
		}
	}
}

// load reads the nodes below a row that was expanded for the first time. The answer comes back on the UI thread.
func (v *view) load(key string, data any) {
	var connID string
	var parent *Node
	switch d := data.(type) {
	case connRow:
		connID = d.conn.ID
	case nodeRow:
		connID, parent = d.connID, &d.node
	default:
		v.tree.FailLoad(key)
		return
	}
	ctx, cancel := context.WithCancel(v.e.ctx)
	v.loading[key] = cancel
	gen := v.gen
	go func() {
		nodes, err := v.e.hub.Children(ctx, connID, parent)
		v.e.api.Post(func() {
			if _, still := v.loading[key]; !still || gen != v.gen || ctx.Err() != nil {
				return // the row went away, or the cache was dropped, while it was read
			}
			delete(v.loading, key)
			cancel()
			if err != nil {
				v.tree.FailLoad(key)
				v.e.api.Log().Warn("the nodes of a connection cannot be listed", "connection", connID, "err", err)
				v.e.api.Notifications().Warn("Cannot list what is in the connection: " + err.Error())
				return
			}
			v.children[key] = nodes
			v.rebuild()
		})
	}()
}

// refreshAll drops what was listed and lists it again where the rows are open.
func (v *view) refreshAll() {
	v.gen++
	for key := range v.children {
		v.forget(key)
	}
	for key, cancel := range v.loading {
		cancel()
		delete(v.loading, key)
	}
	if err := v.e.hub.Store().Load(); err != nil {
		v.e.api.Log().Warn("the connection file cannot be read", "err", err)
	}
	v.rebuild()
	v.e.hub.changed()
}

// collapseAll folds the tree.
func (v *view) collapseAll() { v.tree.Perform(ui.TreeActionCollapseAll) }

// ---- the widget ----

// build makes the widget of the view: what is wrong or missing, then the tree.
func (v *view) build(bc widgets.BuildContext) widgets.Widget {
	colors := ui.Theme(bc)
	var children []widgets.Widget
	note := func(text string, color gui.Color) {
		children = append(children, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 8, 12, 4),
			widgets.NewText(text, widgets.TextOpts{Style: ui.TextStyle(bc, 13, color)})))
	}
	if err := v.e.hub.StoreErr(); err != nil {
		note("The connections cannot be used: "+err.Error(), colors.ErrorForeground)
	}
	conns := v.e.hub.Store().List(v.e.folder())
	switch {
	case len(v.e.hub.Engines()) == 0 && len(conns) == 0:
		note("No database extension is active, so there is nothing to connect to yet.", colors.DescriptionForeground)
	case len(conns) == 0:
		note("There are no connections.", colors.DescriptionForeground)
		children = append(children, widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 4, 12, 8),
			ui.NewButton("New Connection...", ui.ButtonOpts{OnPressed: func() { v.e.newConnection("") }})))
	}
	children = append(children, widgets.NewExpanded(ui.NewTree(v.tree, ui.TreeOpts{
		Controller: v.ctl, Render: v.render, OnOpen: v.onOpen, OnContextMenu: v.onContextMenu,
	})))
	col := widgets.NewColumn(children, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	return ui.NewContextMenu(col, ui.ContextMenuOpts{Controller: v.menu})
}

// render builds the contents of a row.
func (v *view) render(bc widgets.BuildContext, node ui.TreeNode, st ui.RowState) widgets.Widget {
	colors := ui.Theme(bc)
	text := func(s string, color gui.Color) widgets.Widget {
		return widgets.NewText(s, widgets.TextOpts{Style: ui.TextStyle(bc, 13, color), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
	}
	gap := func(w float64) widgets.Widget {
		return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(w)})
	}
	center := rendering.CrossAxisAlignmentCenter
	switch d := node.Data.(type) {
	case connRow:
		c := d.conn
		state := v.e.hub.State(c.ID)
		icon := "database"
		description := describe(c)
		if eng, _, ok := v.e.hub.Engine(c.Engine); ok {
			icon = eng.DisplayIcon()
		} else {
			description = "its extension is not active"
		}
		row := []widgets.Widget{ui.Icon(icon, 16, st.Foreground), gap(6)}
		if swatch, ok := parseColor(c.Color); ok {
			row = append(row, widgets.NewColoredBox(swatch, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(3.0), Height: foundation.Some(14.0)})), gap(5))
		}
		row = append(row, text(c.Name, st.Foreground))
		if c.ReadOnly {
			row = append(row, gap(5), ui.Icon("lock", 12, colors.DescriptionForeground))
		}
		desc := description
		if state.Status == Failed && state.Err != "" {
			desc = state.Err
		}
		row = append(row, gap(8), widgets.NewFlexible(text(desc, colors.DescriptionForeground)), widgets.NewSpacer(), gap(6))
		switch state.Status {
		case Connected:
			row = append(row, ui.Icon("circle-filled", 10, colors.TerminalDecorationSuccess))
		case Connecting:
			row = append(row, ui.Icon("loading", 12, colors.DescriptionForeground))
		case Failed:
			row = append(row, ui.Icon("error", 12, colors.ErrorForeground))
		}
		row = append(row, gap(8))
		return widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: center})
	case nodeRow:
		n := d.node
		row := []widgets.Widget{}
		if n.Icon != "" {
			row = append(row, ui.Icon(n.Icon, 16, st.Foreground), gap(6))
		}
		row = append(row, text(n.Label, st.Foreground))
		if n.Description != "" {
			row = append(row, gap(8), widgets.NewFlexible(text(n.Description, colors.DescriptionForeground)))
		}
		return widgets.NewRow(row, widgets.RowOpts{CrossAxisAlignment: center})
	}
	return text("", st.Foreground)
}

// parseColor reads "#rrggbb".
func parseColor(s string) (gui.Color, bool) {
	if !colorPattern.MatchString(s) {
		return gui.Color{}, false
	}
	var rgb uint32
	for i := 1; i < 7; i++ {
		rgb = rgb<<4 | uint32(hexByte(s[i]))
	}
	return gui.NewColor(0xFF000000 | rgb), true
}

// hexByte is the value of a hex digit.
func hexByte(c byte) uint8 {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	}
	return c - 'A' + 10
}

// onOpen is Enter or a double click: a connection that is closed is opened.
func (v *view) onOpen(node ui.TreeNode) {
	if d, ok := node.Data.(connRow); ok && v.e.hub.State(d.conn.ID).Status != Connected {
		v.e.connect(d.conn)
	}
}

// onContextMenu opens the menu of a row at the place of the click.
func (v *view) onContextMenu(key string, at gui.Offset) {
	if key == "" {
		v.menu.Show(at, []ui.MenuItem{{Label: "New Connection...", OnRun: func() { v.e.newConnection("") }}})
		return
	}
	node, ok := v.tree.Node(key)
	if !ok {
		return
	}
	v.tree.SetSelected(key)
	var items []ui.MenuItem
	switch d := node.Data.(type) {
	case connRow:
		c := d.conn
		st := v.e.hub.State(c.ID).Status
		if st == Connected {
			items = append(items, ui.MenuItem{Label: "Disconnect", OnRun: func() { v.e.disconnect(c) }})
		} else {
			items = append(items, ui.MenuItem{Label: "Connect", Disabled: st == Connecting, OnRun: func() { v.e.connect(c) }})
		}
		items = append(items, v.driverItems(c.ID, nil)...)
		items = append(items, ui.SeparatorItem(),
			ui.MenuItem{Label: "Edit Connection...", OnRun: func() { v.e.editConnection(c) }},
			ui.MenuItem{Label: "Delete Connection...", OnRun: func() { v.e.deleteConnection(c) }},
			ui.MenuItem{Label: "Copy Name", OnRun: func() { v.copyName(c.Name) }})
	case nodeRow:
		n := d.node
		items = append(v.driverItems(d.connID, &n), ui.MenuItem{Label: "Copy Name", OnRun: func() { v.copyName(n.Label) }})
	default:
		return
	}
	v.menu.Show(at, items)
}

// driverItems are the entries of the menu that the extension of the engine adds, with a separator between its groups.
func (v *view) driverItems(connID string, n *Node) []ui.MenuItem {
	actions := v.e.hub.ActionsIn(v.e.api, connID, n)
	var items []ui.MenuItem
	lastGroup := ""
	for i, a := range actions {
		if i == 0 || a.Group != lastGroup {
			items = append(items, ui.SeparatorItem())
		}
		lastGroup = a.Group
		run := a.Run
		items = append(items, ui.MenuItem{Label: a.Title, OnRun: run})
	}
	return items
}

// copyName puts a name on the clipboard.
func (v *view) copyName(name string) {
	if err := v.e.api.Clipboard().WriteText(name); err != nil {
		v.e.api.Log().Warn("the clipboard did not take the name", "err", err)
		v.e.api.Notifications().Warn("Cannot copy to the clipboard: " + err.Error())
	}
}
