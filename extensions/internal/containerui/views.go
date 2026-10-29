package containerui

import (
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui"
)

// section is one view of the side bar: what it lists and how a row looks and acts.
type section interface {
	// name is the suffix of the id of the view, res the resource it lists.
	name() string
	res() resource
	// title is the name in the header of the view.
	title() string
	// items makes the nodes of the tree from the lists; message is what the view says instead of an empty tree.
	items(e *ext) (items []ui.TreeItem, message string)
	// render builds the contents of a row.
	render(bc widgets.BuildContext, v *sectionView, node ui.TreeNode, st ui.RowState) widgets.Widget
	// menu lists the items of the context menu for the rows with the keys.
	menu(v *sectionView, keys []string) []ui.MenuItem
	// open is what Enter or a double click does on a row.
	open(v *sectionView, node ui.TreeNode)
}

// sectionView is a section and its tree, in the side bar.
type sectionView struct {
	e      *ext
	sec    section
	handle extension.ViewHandle
	tree   *ui.TreeModel
	ctl    *ui.TreeController
	menu   *ui.ContextMenuController
	// message is what the view says over the tree.
	message string
	// seen are the groups that were shown already.
	seen map[string]bool
}

// newSectionView makes the view of a section.
func newSectionView(e *ext, sec section) *sectionView {
	v := &sectionView{e: e, sec: sec, ctl: ui.NewTreeController(), menu: ui.NewContextMenuController(), seen: map[string]bool{}}
	opts := ui.TreeModelOpts{MultiSelect: true}
	if sec.res() == resContainers {
		opts.LoadChildren = v.loadChildren
	}
	v.tree = ui.NewTreeModel(opts)
	return v
}

// provide gives the view its content.
func (v *sectionView) provide() error {
	content := extension.ViewContent{Build: v.build}
	if v.sec.res() == resContainers {
		content.SaveState = v.e.saveState
		content.RestoreState = v.e.restoreState
	}
	h, err := v.e.api.Views().Provide(v.e.cfg.ViewID(v.sec.name()), content)
	if err != nil {
		return err
	}
	v.handle = h
	v.rebuild()
	return nil
}

// rebuild makes the nodes again from the lists and asks for a new build of the view.
func (v *sectionView) rebuild() {
	items, msg := v.sec.items(v.e)
	if err := v.tree.SetItems(items); err != nil {
		v.e.api.Log().Warn("the tree cannot be made", "section", v.sec.name(), "err", err)
	}
	v.message = msg
	// a group is open the first time it is seen; one the user closed stays closed
	for _, it := range items {
		if _, ok := it.Data.(groupNode); ok && !v.seen[it.Key] {
			v.seen[it.Key] = true
			v.tree.Expand(it.Key)
		}
	}
	if v.handle != nil {
		v.handle.Refresh()
	}
}

// rebuild makes the tree of a resource again.
func (e *ext) rebuild(r resource) {
	if v := e.views[sectionOf(r)]; v != nil {
		v.rebuild()
	}
}

// rebuildAll makes every tree again.
func (e *ext) rebuildAll() {
	for _, v := range e.views {
		v.rebuild()
	}
}

// refreshViews builds every view again without making the trees again (the state of the connection changed).
func (e *ext) refreshViews() {
	for _, v := range e.views {
		if v.handle != nil {
			v.handle.Refresh()
		}
	}
}

// selection returns the keys of the selected rows.
func (v *sectionView) selection() []string { return v.tree.SelectedKeys() }

// keysFor are the rows an action works on: the one named by the argument {"key": ...}, else the selected rows.
func (v *sectionView) keysFor(args any) []string {
	if m, ok := args.(map[string]any); ok {
		if key, ok := m["key"].(string); ok && key != "" {
			return []string{key}
		}
	}
	return v.selection()
}

// onContextMenu opens the menu of a row at the place of the click. A row outside the selection becomes the selection.
func (v *sectionView) onContextMenu(key string, at gui.Offset) {
	if key == "" {
		return
	}
	if !v.tree.IsSelected(key) {
		v.tree.SetSelected(key)
	}
	items := v.sec.menu(v, v.selection())
	if len(items) == 0 {
		return
	}
	v.menu.Show(at, items)
}

// onSelectionChange keeps the selection in the view (the tree keeps it itself); it exists so that the tree is multi-select.
func (v *sectionView) onOpen(node ui.TreeNode) { v.sec.open(v, node) }

// build makes the widget of the view: the confirmation, the notes and the progress of what goes on, then the tree.
func (v *sectionView) build(bc widgets.BuildContext) widgets.Widget {
	e := v.e
	colors := ui.Theme(bc)
	var col []widgets.Widget
	if c := e.confirm; c != nil && c.section == v.sec.res() {
		col = append(col, e.confirmBar(bc, c))
	}
	if v.sec.res() == resContainers {
		switch e.status {
		case statusMissing:
			col = append(col, widgets.NewExpanded(e.missingView(bc)))
			return v.wrap(col)
		case statusIdle, statusConnecting:
			col = append(col, ui.NewProgressBar(ui.ProgressBarOpts{Infinite: true}), note(bc, colors, "Looking for "+e.cfg.Engine+"..."))
			return v.wrap(col)
		}
		if !e.note.seen && e.cfg.FirstRunNote != "" {
			col = append(col, e.firstRunNote(bc))
		}
	}
	if v.sec.res() == resImages {
		col = append(col, e.operationRows(bc)...)
	}
	if v.message != "" {
		col = append(col, note(bc, colors, v.message))
	}
	col = append(col, widgets.NewExpanded(ui.NewTree(v.tree, ui.TreeOpts{
		Controller: v.ctl, Render: v.render, MultiSelect: true, OnOpen: v.onOpen,
		OnContextMenu: v.onContextMenu,
	})))
	return v.wrap(col)
}

// wrap puts the parts in a column inside the context menu.
func (v *sectionView) wrap(col []widgets.Widget) widgets.Widget {
	c := widgets.NewColumn(col, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	return ui.NewContextMenu(c, ui.ContextMenuOpts{Controller: v.menu})
}

// render builds the contents of a row.
func (v *sectionView) render(bc widgets.BuildContext, node ui.TreeNode, st ui.RowState) widgets.Widget {
	return v.sec.render(bc, v, node, st)
}

// loadChildren reads the children of a lazy node: the files of a container.
func (v *sectionView) loadChildren(key string, _ any) { v.e.loadFiles(key) }

// ---- small widgets ----

// note is a line of text over a tree, in the color of descriptions.
func note(bc widgets.BuildContext, colors ui.Colors, text string) widgets.Widget {
	return widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(12, 6, 8, 6),
		widgets.NewText(text, widgets.TextOpts{Style: ui.TextStyle(bc, 13, colors.DescriptionForeground)}))
}

// gap is empty space of a width.
func gap(w float64) widgets.Widget {
	return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(w)})
}

// vgap is empty space of a height.
func vgap(h float64) widgets.Widget {
	return widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(h)})
}

// text is a line of text of a row.
func text(bc widgets.BuildContext, s string, size float64, color gui.Color) widgets.Widget {
	return widgets.NewText(s, widgets.TextOpts{Style: ui.TextStyle(bc, size, color), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
}

// rowButton is the button that shows on a row the pointer is over or that is selected (VS Code's inline actions); on any other
// row it is a gap so that the text does not move.
func rowButton(st ui.RowState, icon, tooltip string, run func()) widgets.Widget {
	if !st.Hovered && !st.Selected {
		return gap(0)
	}
	return ui.NewIconButton(icon, ui.IconButtonOpts{Tooltip: tooltip, OnPressed: run})
}

// labelAndDescription is the usual row: a label, and after it a description in a dimmer color, in one line of text that is cut
// with an ellipsis where the row ends (the label first: it is the last thing to go). It takes the room that is left in the row.
func labelAndDescription(bc widgets.BuildContext, colors ui.Colors, st ui.RowState, label, description string) []widgets.Widget {
	spans := []painting.InlineSpan{painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(label), Style: ui.TextStyle(bc, 13, st.Foreground)})}
	if description != "" {
		spans = append(spans, painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some("  " + description), Style: ui.TextStyle(bc, 12, colors.DescriptionForeground)}))
	}
	rich := widgets.NewRichText(painting.NewTextSpan(painting.TextSpanOpts{Children: spans}),
		widgets.RichTextOpts{MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis, SoftWrap: foundation.Some(false)})
	return []widgets.Widget{widgets.NewExpanded(rich)}
}

// splitLines cuts a text in lines.
func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
