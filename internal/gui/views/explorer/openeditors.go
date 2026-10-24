package explorer

import (
	"slices"
	"strconv"
	"strings"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/core/workspace"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// groupRow is the row of an editor group in the Open Editors, shown when there are several groups.
type groupRow struct {
	Group *editorgroup.Group
	Label string
}

// editorRow is the row of an editor of a group.
type editorRow struct {
	Group *editorgroup.Group
	Input editorgroup.EditorInput
	// Preview says that the editor is the preview editor of its group, Dirty that it has unsaved changes and
	// Deleted that its file was deleted by another program (the label is struck through).
	Preview, Dirty, Deleted bool
}

// OpenEditorsOpts configure an OpenEditors.
type OpenEditorsOpts struct {
	// Grid is the editor groups of the window; the list follows it.
	Grid *editorgroup.Grid
	// SortOrder gives explorer.openEditors.sortOrder ("editorOrder", "alphabetical" or "fullPath");
	// nil is editorOrder. Visible gives explorer.openEditors.visible; nil is 9.
	SortOrder func() string
	Visible   func() int
	// DecorationColors gives workbench.editor.decorations.colors: a deleted editor is drawn in the error color;
	// nil is yes.
	DecorationColors func() bool
	// Icons and Language are those of ViewOpts.
	Icons    func() *theme.FileIconTheme
	Language func(path string) string
	// Keys is the scope of the context keys of the window.
	Keys *contextkey.Scope
	// OnOpen is called when the user opens an editor: a click on it (pinned is false), a double click or
	// Enter (true). OnActivateGroup is called for a click on a group; OnClose for the close button of an
	// editor and for a click of the middle button.
	OnOpen          func(group *editorgroup.Group, input editorgroup.EditorInput, pinned bool)
	OnActivateGroup func(group *editorgroup.Group)
	OnClose         func(group *editorgroup.Group, input editorgroup.EditorInput)
}

// OpenEditors is the Open Editors view without its widgets (SPEC §10.8): the editors of every group of the
// window as a list, with a row for each group when there are several. It follows the editor groups, keeps the
// active editor selected, and is used on the UI thread. NewOpenEditorsBody is the widget that shows it.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/views/openEditorsView.ts (OpenEditorsView)
type OpenEditors struct {
	opts      OpenEditorsOpts
	list      *kit.ListViewModel
	ctl       *kit.ListController
	handle    foundation.ListenerHandle
	stop      func()
	ids       map[editorgroup.EditorInput]int
	nextID    int
	count     int
	dirty     int
	listeners []*func()
	disposed  bool
}

// OpenEditorsRowHeight is the height of a row of the Open Editors.
const OpenEditorsRowHeight = 22.0

// DefaultOpenEditorsVisible is explorer.openEditors.visible when no setting is given.
const DefaultOpenEditorsVisible = 9

// NewOpenEditors makes the Open Editors view of the groups of a grid.
func NewOpenEditors(opts OpenEditorsOpts) *OpenEditors {
	o := &OpenEditors{opts: opts, list: kit.NewListViewModel(), ctl: kit.NewListController(), ids: map[editorgroup.EditorInput]int{}}
	o.handle = o.list.AddListener(o.syncKeys)
	o.stop = opts.Grid.OnChange(func(editorgroup.GridEvent) { o.Refresh() })
	o.Refresh()
	return o
}

// Dispose stops following the groups.
func (o *OpenEditors) Dispose() {
	o.disposed = true
	o.stop()
	o.list.RemoveListener(o.handle)
	if o.opts.Keys != nil {
		o.opts.Keys.Reset("groupFocusedInOpenEditors")
	}
}

// List is the model of the rows.
func (o *OpenEditors) List() *kit.ListViewModel { return o.list }

// Controller is the controller of the list widget.
func (o *OpenEditors) Controller() *kit.ListController { return o.ctl }

// Focus gives the keyboard focus to the list.
func (o *OpenEditors) Focus() { o.ctl.Focus() }

// OnChange calls fn after the rows changed, and returns the function that stops it.
func (o *OpenEditors) OnChange(fn func()) (unsubscribe func()) {
	p := &fn
	o.listeners = append(o.listeners, p)
	return func() {
		if i := slices.Index(o.listeners, p); i >= 0 {
			o.listeners = slices.Delete(o.listeners, i, i+1)
		}
	}
}

// Count is the number of rows: the editors and, with several groups, the groups.
func (o *OpenEditors) Count() int { return o.count }

// DirtyCount is the number of editors that have unsaved changes.
func (o *OpenEditors) DirtyCount() int { return o.dirty }

// BodyHeight is the height of the list: room for explorer.openEditors.visible rows (at least one), or for
// all of them when there are fewer; the list scrolls past that.
func (o *OpenEditors) BodyHeight() float64 {
	visible := DefaultOpenEditorsVisible
	if o.opts.Visible != nil {
		visible = o.opts.Visible()
	}
	return float64(min(max(visible, 1), o.count)) * OpenEditorsRowHeight
}

// groupLabel is the name of a group: "Group 1" for the first in the grid.
func groupLabel(index int) string { return "Group " + strconv.Itoa(index+1) }

// Refresh makes the rows again from the groups of the grid and selects the active editor.
func (o *OpenEditors) Refresh() {
	if o.disposed {
		return
	}
	grid := o.opts.Grid
	groups := grid.Groups(editorgroup.GridAppearance)
	sortOrder := "editorOrder"
	if o.opts.SortOrder != nil {
		sortOrder = o.opts.SortOrder()
	}
	var items []kit.ListViewItem
	present := map[editorgroup.EditorInput]int{}
	o.dirty = 0
	for i, g := range groups {
		if len(groups) > 1 {
			items = append(items, kit.ListViewItem{Key: "g:" + strconv.Itoa(g.ID()), Data: groupRow{Group: g, Label: groupLabel(i)}, Label: groupLabel(i)})
		}
		editors := g.Model.Editors(editorgroup.Sequential, false)
		sortEditors(editors, sortOrder)
		for _, e := range editors {
			id, ok := o.ids[e]
			if !ok {
				o.nextID++
				id = o.nextID
			}
			present[e] = id
			row := editorRow{Group: g, Input: e, Preview: !g.Model.IsPinned(e), Dirty: e.Dirty(), Deleted: editorgroup.IsDeleted(e)}
			if row.Dirty {
				o.dirty++
			}
			items = append(items, kit.ListViewItem{Key: "e:" + strconv.Itoa(g.ID()) + ":" + strconv.Itoa(id), Data: row, Label: e.Name()})
		}
	}
	o.ids = present
	o.count = len(items)
	// the keys are made of the id of a group and a number of this list, so they cannot repeat
	_ = o.list.SetItems(items)
	o.selectActive()
	o.notify()
}

// selectActive selects and focuses the row of the active editor of the active group, as VS Code does when
// the active editor changes.
func (o *OpenEditors) selectActive() {
	g := o.opts.Grid.Active()
	if g == nil {
		o.list.ClearSelection()
		return
	}
	active := g.Model.ActiveEditor()
	for _, r := range o.list.Rows() {
		if er, ok := r.Data.(editorRow); ok && er.Group == g && er.Input == active {
			o.list.SetSelected(r.Key)
			o.list.SetFocus(r.Key)
			return
		}
	}
	o.list.ClearSelection()
	o.list.ClearFocus()
}

func (o *OpenEditors) notify() {
	for _, f := range slices.Clone(o.listeners) {
		(*f)()
	}
}

// syncKeys sets groupFocusedInOpenEditors, the context key that says the focused row is a group.
func (o *OpenEditors) syncKeys() {
	if o.opts.Keys == nil {
		return
	}
	group := false
	if key, ok := o.list.Focused(); ok {
		if it, ok := o.list.Item(key); ok {
			_, group = it.Data.(groupRow)
		}
	}
	o.opts.Keys.Set("groupFocusedInOpenEditors", group)
}

// sortEditors puts the editors of a group in the order of explorer.openEditors.sortOrder.
//
// Ported from VS Code: src/vs/workbench/contrib/files/browser/views/openEditorsView.ts (getElements)
func sortEditors(editors []editorgroup.EditorInput, order string) {
	switch order {
	case "alphabetical":
		slices.SortStableFunc(editors, func(a, b editorgroup.EditorInput) int {
			return workspace.CompareFileNamesDefault(a.Name(), b.Name())
		})
	case "fullPath":
		slices.SortStableFunc(editors, func(a, b editorgroup.EditorInput) int {
			ra, oka := a.Resource()
			rb, okb := b.Resource()
			switch {
			case !oka && !okb:
				return workspace.CompareFileNamesDefault(a.Name(), b.Name())
			case !oka:
				return -1
			case !okb:
				return 1
			case (ra.Scheme == document.SchemeFile) != (rb.Scheme == document.SchemeFile):
				// editors of other schemes come before files
				if ra.Scheme != document.SchemeFile {
					return -1
				}
				return 1
			}
			return strings.Compare(strings.ToLower(ra.Path), strings.ToLower(rb.Path))
		})
	}
}

// open handles the opening of a row.
func (o *OpenEditors) open(row kit.ListViewRow, pinned bool) {
	switch d := row.Data.(type) {
	case editorRow:
		if o.opts.OnOpen != nil {
			o.opts.OnOpen(d.Group, d.Input, pinned)
		}
	case groupRow:
		if o.opts.OnActivateGroup != nil {
			o.opts.OnActivateGroup(d.Group)
		}
	}
}

// closeRow closes the editor of a row.
func (o *OpenEditors) closeRow(d editorRow) {
	if o.opts.OnClose != nil {
		o.opts.OnClose(d.Group, d.Input)
	}
}

// OpenEditorsBody is the body of the Open Editors view: the list of its rows. A row of an editor has the
// icon of its file, its name and, dimmed, its folder; the close button (a dot while the editor has unsaved
// changes) is before them, shown for the row under the pointer, the selected row and a dirty editor. The
// editor of a preview is in italics.
type OpenEditorsBody struct {
	widgets.StatefulWidget
	view *OpenEditors
}

// NewOpenEditorsBody makes the body of the Open Editors view.
func NewOpenEditorsBody(view *OpenEditors) *OpenEditorsBody {
	return &OpenEditorsBody{StatefulWidget: widgets.NewStatefulWidgetBase(), view: view}
}

// CreateState implements widgets.StatefulWidget.
func (b *OpenEditorsBody) CreateState() widgets.State { return &openEditorsState{} }

type openEditorsState struct {
	widgets.StateOf[*OpenEditorsBody]
	stop func()
}

// InitState follows the view.
func (s *openEditorsState) InitState() { s.follow() }

func (s *openEditorsState) follow() {
	s.stop = s.Widget().view.OnChange(func() {
		if s.Mounted() {
			s.SetState(func() {})
		}
	})
}

// DidUpdateWidget follows another view.
func (s *openEditorsState) DidUpdateWidget(old *OpenEditorsBody) {
	if old.view != s.Widget().view {
		s.stop()
		s.follow()
	}
}

// Dispose stops following the view.
func (s *openEditorsState) Dispose() { s.stop() }

// Build implements widgets.State.
func (s *openEditorsState) Build(widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	o := w.view
	list := kit.NewList(o.list, kit.ListOpts{
		Key: OpenEditorsKey, Controller: o.ctl, TypeToFind: foundation.Some(false),
		Render: func(bc widgets.BuildContext, row kit.ListViewRow, st kit.RowState) widgets.Widget {
			return o.render(bc, row, st)
		},
		OnSelect: func(row kit.ListViewRow) { o.open(row, false) },
		OnOpen:   func(row kit.ListViewRow) { o.open(row, true) },
	})
	return list
}

// render builds the contents of a row.
func (o *OpenEditors) render(bc widgets.BuildContext, row kit.ListViewRow, st kit.RowState) widgets.Widget {
	switch d := row.Data.(type) {
	case groupRow:
		style := kit.TextStyleOf(bc, 11, st.Foreground).Merge(painting.NewTextStyle(painting.TextStyleOpts{FontWeight: ui.FontWeightW700}))
		return widgets.NewText(strings.ToUpper(d.Label), widgets.TextOpts{Style: style, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
	case editorRow:
		return o.renderEditor(bc, d, st)
	}
	return kit.NewHighlightedLabel(bc, row.Label, st)
}

func (o *OpenEditors) renderEditor(bc widgets.BuildContext, d editorRow, st kit.RowState) widgets.Widget {
	colors := kit.ThemeOf(bc)
	fg := st.Foreground
	// the close button: a dot while the editor has unsaved changes, a cross for the row under the
	// pointer or the selected one
	var button widgets.Widget = widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(16.0), Height: foundation.Some(22.0)})
	switch {
	case d.Dirty && !st.Hovered:
		button = o.button(fonts.CodiconCircleFilled, fg, nil)
	case st.Hovered || st.Selected || d.Dirty:
		button = o.button(fonts.CodiconClose, fg, func() { o.closeRow(d) })
	}
	var icon widgets.Widget
	name := d.Input.Name()
	if uri, ok := d.Input.Resource(); ok && uri.Scheme == document.SchemeFile {
		if th := o.iconTheme(); th != nil {
			q := theme.FileIconQuery{Kind: theme.FileIconFile, Name: name, Type: colors.Type}
			if o.opts.Language != nil {
				q.LanguageID = o.opts.Language(uri.Path)
			}
			if ic, ok := th.IconFor(q); ok {
				icon = FileIconWidget(ic, fg)
			}
		}
	}
	if d.Deleted && (o.opts.DecorationColors == nil || o.opts.DecorationColors()) {
		if c := colors.ListErrorForeground; c.Alpha() != 0 {
			fg = c
		}
	}
	labelStyle := kit.TextStyleOf(bc, labelFontSize, fg)
	if d.Preview {
		labelStyle = labelStyle.Merge(painting.NewTextStyle(painting.TextStyleOpts{FontStyle: ui.FontStyleItalic}))
	}
	if d.Deleted {
		labelStyle = labelStyle.Merge(painting.NewTextStyle(painting.TextStyleOpts{Decoration: foundation.Some(ui.TextDecorationLineThrough)}))
	}
	cells := []widgets.Widget{
		widgets.NewText(name, widgets.TextOpts{Style: labelStyle, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis}),
	}
	if desc := d.Input.Description(); desc != "" {
		dim := kit.TextStyleOf(bc, labelFontSize*0.9, fg.WithOpacity(0.7))
		if d.Deleted {
			dim = dim.Merge(painting.NewTextStyle(painting.TextStyleOpts{Decoration: foundation.Some(ui.TextDecorationLineThrough)}))
		}
		cells = append(cells,
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}),
			widgets.NewFlexible(widgets.NewText(desc, widgets.TextOpts{Style: dim, MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})))
	}
	label := widgets.NewExpanded(widgets.NewRow(cells, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter}))
	return widgets.NewRow([]widgets.Widget{button, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(4.0)}), widgets.NewExpanded(iconRow(icon, label))},
		widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
}

// button is the close button of a row: a 16 px Codicon that runs onTap when pressed.
func (o *OpenEditors) button(code int, color ui.Color, onTap func()) widgets.Widget {
	icon := widgets.NewIcon(fonts.CodiconIcon(code), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(color)})
	var box widgets.Widget = widgets.NewSizedBox(widgets.NewCenter(icon), widgets.SizedBoxOpts{Width: foundation.Some(16.0), Height: foundation.Some(22.0)})
	if onTap == nil {
		return box
	}
	return widgets.NewMouseRegion(
		widgets.NewGestureDetector(box, widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: onTap}),
		widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click})
}

func (o *OpenEditors) iconTheme() *theme.FileIconTheme {
	if o.opts.Icons == nil {
		return nil
	}
	return o.opts.Icons()
}
