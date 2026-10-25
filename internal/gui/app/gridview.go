package app

import (
	"fmt"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// The sizes below which a group is not made smaller (VS Code's EditorGroupView minimumWidth and
// minimumHeight).
const (
	groupMinWidth  = 220.0
	groupMinHeight = 70.0
	// gridScale is what the share of a pane is multiplied by to make the size a split view starts
	// from: the split view fits the sizes to its own, in proportion.
	gridScale = 1000.0
)

// groupView is what the widgets of one group keep from build to build.
type groupView struct {
	// key keeps the widgets of the group (and so their state) when the group moves in the tree.
	key widgets.GlobalObjectKey[widgets.State]
	// empty is the focus node of the empty area of the group.
	empty *widgets.FocusNode
	// tabs tells where the tab strip and its tabs are, and feedback shows where a dragged tab would go
	// in it; area is where the group is, and tabMenu shows the context menu of a tab (step 6.10).
	tabs     *kit.TabsController
	feedback *kit.TabDropFeedback
	area     kit.Bounds
	tabMenu  *kit.ContextMenuController
}

// view is the groupView of a group.
func (s *shellState) view(group *editorgroup.Group) *groupView {
	v := s.group.views[group]
	if v == nil {
		v = &groupView{
			key:   widgets.NewGlobalObjectKey[widgets.State](group),
			empty: widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: fmt.Sprintf("Empty editor group %d", group.ID())}),
			tabs:  kit.NewTabsController(), feedback: kit.NewTabDropFeedback(), tabMenu: kit.NewContextMenuController(),
		}
		s.group.views[group] = v
	}
	return v
}

// editorArea is what the editor area shows: the grid of groups.
func (s *shellState) editorArea(bc widgets.BuildContext) widgets.Widget {
	return newCenteredArea(PeekWorkspace(bc), s.group.grid, s.nodeWidget(bc, s.group.grid.Tree(), nil))
}

// nodeWidget is a node of the tree of splits: a group, or a split view of the nodes below it. A
// split with one child (the root of a grid of one group) is that child.
func (s *shellState) nodeWidget(bc widgets.BuildContext, n editorgroup.Node, path []int) widgets.Widget {
	if n.Group != nil {
		return s.groupWidget(bc, n.Group)
	}
	if len(n.Children) == 1 {
		return s.nodeWidget(bc, n.Children[0], append(append([]int(nil), path...), 0))
	}
	grid := s.group.grid
	colors := kit.ThemeOf(bc)
	orientation := kit.SplitVertical
	if n.Orientation == editorgroup.Horizontal {
		orientation = kit.SplitHorizontal
	}
	panes := make([]kit.SplitPane, len(n.Children))
	for i, c := range n.Children {
		childPath := append(append([]int(nil), path...), i)
		panes[i] = kit.SplitPane{
			Child:   s.nodeWidget(bc, c, childPath),
			Size:    c.Size * gridScale,
			MinSize: minimumSize(c, n.Orientation),
			Hidden:  grid.Maximized() != nil && !containsGroup(c, grid.Maximized()),
		}
	}
	split := kit.NewSplitView(panes, kit.SplitViewOpts{
		Orientation:        orientation,
		ProportionalLayout: true,
		SeparatorBorder:    colors.EditorGroupBorder,
		OnDidChange:        func(sizes []float64) { s.sized(path, sizes) },
	})
	// the sizes start over from the grid's whenever the grid changes them itself, which a new key
	// asks for; the groups keep their state through their global keys
	return widgets.NewKeyedSubtree(split, widgets.WidgetOpts{Key: foundation.NewKey(fmt.Sprintf("grid-%d", grid.Version()))})
}

// minimumSize is the least size of a node along an axis: a group has its least width or height, a
// split along the axis the sum of its children and a split across it the greatest of them.
func minimumSize(n editorgroup.Node, axis editorgroup.Orientation) float64 {
	if n.Group != nil {
		if axis == editorgroup.Horizontal {
			return groupMinWidth
		}
		return groupMinHeight
	}
	var sum, greatest float64
	for _, c := range n.Children {
		m := minimumSize(c, axis)
		sum += m
		greatest = max(greatest, m)
	}
	if n.Orientation == axis {
		return sum
	}
	return greatest
}

// containsGroup tells whether a group is in a node.
func containsGroup(n editorgroup.Node, group *editorgroup.Group) bool {
	if n.Group != nil {
		return n.Group == group
	}
	for _, c := range n.Children {
		if containsGroup(c, group) {
			return true
		}
	}
	return false
}

// sized records the sizes of the children of a split that the user dragged.
func (s *shellState) sized(path []int, sizes []float64) {
	if !s.Mounted() || s.group.grid.Maximized() != nil {
		return
	}
	s.group.grid.SetSizes(path, sizes)
}

// groupWidget is a group: the tab strip, the breadcrumbs and the editor of the active tab, or the
// empty area. A pointer pressed in it makes it the active group.
func (s *shellState) groupWidget(bc widgets.BuildContext, group *editorgroup.Group) widgets.Widget {
	win := PeekWorkspace(bc)
	v := s.view(group)
	active := s.group.grid.Active() == group
	var content widgets.Widget
	if ed := s.group.editors[group.Model.ActiveEditor()]; ed != nil {
		content = s.editorColumn(bc, win, group, ed, active)
	} else if p := s.group.panes[group.Model.ActiveEditor()]; p != nil {
		content = s.paneColumn(bc, win, group, group.Model.ActiveEditor(), p, active)
	} else {
		content = s.emptyGroup(bc, group, active)
	}
	pressed := widgets.NewListener(content, widgets.ListenerOpts{
		OnPointerDown: func(*gestures.PointerDownEvent) { s.groupFocused(group) },
	})
	return widgets.NewKeyedSubtree(kit.NewBoundsProbe(pressed, &v.area), widgets.WidgetOpts{Key: v.key})
}

// editorColumn is the tab strip over the breadcrumbs over the editor of a group.
func (s *shellState) editorColumn(bc widgets.BuildContext, win *Workspace, group *editorgroup.Group, ed *textEditor, active bool) widgets.Widget {
	binding := widgets.WidgetsBindingOf(bc)
	text := editor.NewEditor(ed.view, editor.EditorOpts{
		Commands: win.Commands, Keys: win.Keys, Scopes: win.Scopes, Autofocus: active, Context: win.ctx, ClipboardMemory: win.Clipboard,
		FindMemory: win.Find, Zoom: win.Zoom, InputMode: win.InputMode, Focus: ed.focus,
		OnFocus: func(has bool) { s.editorFocus(group, ed, has) },
	})
	// each editor has its own widget state (scroll, focus), which a key apart keeps; the key is global
	// so that the state goes with the editor when it moves to another group
	var area widgets.Widget = widgets.NewKeyedSubtree(
		// a right click opens the context menu of the editor
		kit.NewContextMenuRegion(text, s.editorMenuItems, kit.ContextMenuOpts{Controller: s.editorMenu, OnRun: s.menus.Run}),
		widgets.WidgetOpts{Key: widgets.NewGlobalObjectKey[widgets.State](ed)})
	column := []widgets.Widget{}
	if tabs := s.tabStrip(bc, win, group); tabs != nil {
		// a right click on a tab opens its context menu
		column = append(column, tabs)
	}
	if uri := ed.doc.URI(); uri.Scheme == document.SchemeFile {
		// the path of the file above the text
		column = append(column, editor.NewBreadcrumbs(win.ctx, uri.Path, editor.BreadcrumbsOptionsFromSettings(win.Settings), editor.BreadcrumbsOpts{
			Root:   win.Folder,
			OnOpen: func(path string) { s.openPathIn(binding, group, path) },
		}))
	}
	column = append(column, widgets.NewExpanded(area))
	return widgets.NewColumn(column, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// editorFocus follows the keyboard focus into and out of an editor: a group whose editor has it is
// the active group.
func (s *shellState) editorFocus(group *editorgroup.Group, ed *textEditor, has bool) {
	if has {
		s.group.focused[ed] = true
		s.focusMoved(group)
		return
	}
	delete(s.group.focused, ed)
	s.editorBlurred(widgets.WidgetsBindingOf(s.Context()), ed)
}

// emptyGroup is a group with no editor: the notice of a file that looks binary, with a button to open
// it anyway, or the home page. The area takes the keyboard focus when it is pressed, and shows the
// border of a focused empty group while the group is active.
func (s *shellState) emptyGroup(bc widgets.BuildContext, group *editorgroup.Group, active bool) widgets.Widget {
	v := s.view(group)
	colors := kit.ThemeOf(bc)
	var area widgets.Widget
	if active {
		area = s.emptyArea(bc)
	} else {
		area = s.homePage(bc)
	}
	background := colors.EditorBackground
	if colors.EditorGroupEmptyBackground.Alpha() != 0 {
		background = colors.EditorGroupEmptyBackground
	}
	focused := widgets.NewFocus(
		widgets.NewListener(area, widgets.ListenerOpts{OnPointerDown: func(*gestures.PointerDownEvent) { v.empty.RequestFocus() }}),
		widgets.FocusOpts{FocusNode: v.empty, OnFocusChange: func(has bool) {
			if has {
				s.focusMoved(group)
			}
		}})
	deco := painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(background)})
	if active && colors.EditorGroupFocusedEmptyBorder.Alpha() != 0 {
		side := painting.NewBorderSide(painting.BorderSideOpts{Color: foundation.Some(colors.EditorGroupFocusedEmptyBorder), Width: foundation.Some(1.0)})
		deco = painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(background), Border: painting.Border{Top: side, Right: side, Bottom: side, Left: side}})
	}
	return widgets.NewContainer(focused, widgets.ContainerOpts{Decoration: deco})
}

// tabItems are the tabs of a group, in order: the label and the state of each editor. Editors that
// share a name show their folders too.
func (s *shellState) tabItems(colors kit.Colors, group *editorgroup.Group) []kit.TabItem {
	m := group.Model
	editors := m.Editors(editorgroup.Sequential, false)
	labels := editorgroup.TabLabels(editors)
	items := make([]kit.TabItem, len(editors))
	for i, e := range editors {
		key := labels[i].Name
		if uri, ok := e.Resource(); ok {
			key = uri.String()
		} else if k, ok := e.(editorgroup.TabKeyed); ok {
			key = k.TabKey()
		}
		items[i] = kit.TabItem{
			Key: key, Name: labels[i].Name, Description: labels[i].Description, Title: editorgroup.DecoratedTitle(e, labels[i].Title),
			Preview: !m.IsPinned(e), Sticky: m.IsSticky(e), Dirty: e.Dirty(), Deleted: editorgroup.IsDeleted(e),
			Icon: s.tabIcon(colors, e, e == m.ActiveEditor()),
		}
	}
	return items
}

// tabsOptions are the options of the tab strip of a group from the settings and the model; the strip of
// a group that is not the active group has the colors of an unfocused one.
func (s *shellState) tabsOptions(win *Workspace, group *editorgroup.Group) kit.TabsOpts {
	m := group.Model
	opts := kit.TabsOpts{
		Active:            m.IndexOf(m.ActiveEditor()),
		Unfocused:         s.group.grid.Active() != group,
		HighlightModified: settings.Get[bool](win.Settings, "workbench.editor.highlightModifiedTabs", ""),
		DecorationColors:  settings.Get[bool](win.Settings, "workbench.editor.decorations.colors", ""),
		OnActivate:        func(i int) { s.activateAt(group, i) },
		OnClose:           func(i int) { s.closeAt(group, i) },
		OnPin:             func(i int) { m.Pin(m.EditorAt(i)) },
		OnUnpin:           func(i int) { m.Unstick(m.EditorAt(i)) },
		OnContextMenu:     func(i int, at ui.Offset) { s.openTabMenu(group, i, at) },
		OnDragStart:       func(i int) bool { return s.dragStart(group, i) },
		OnDragMove:        func(_ int, at ui.Offset) { s.dragMove(at) },
		OnDragEnd:         func(_ int, at ui.Offset, dropped bool) { s.dragEnd(at, dropped) },
		Feedback:          s.view(group).feedback,
		Controller:        s.view(group).tabs,
	}
	// the close button: the old setting tabCloseButton is honored with the two that replace it
	button := settings.Get[string](win.Settings, "workbench.editor.tabCloseButton", "")
	switch {
	case button == "off" || !settings.Get[bool](win.Settings, "workbench.editor.tabActionCloseVisibility", ""):
		opts.CloseButton = kit.TabCloseOff
	case button == "left" || settings.Get[string](win.Settings, "workbench.editor.tabActionLocation", "") == "left":
		opts.CloseButton = kit.TabCloseLeft
	}
	switch settings.Get[string](win.Settings, "workbench.editor.pinnedTabSizing", "") {
	case "compact":
		opts.PinnedSizing = kit.PinnedCompact
	case "shrink":
		opts.PinnedSizing = kit.PinnedShrink
	}
	if win.ShowTabs() == "single" {
		opts.Mode = kit.TabsSingle
	}
	return opts
}

// activateAt makes the editor at an index of the tabs of a group the active one, and the group the
// active group.
func (s *shellState) activateAt(group *editorgroup.Group, index int) {
	if e := group.Model.EditorAt(index); e != nil {
		group.Model.SetActive(e)
		s.groupFocused(group)
	}
}

// closeAt closes the editor at an index of the tabs of a group.
func (s *shellState) closeAt(group *editorgroup.Group, index int) {
	if e := group.Model.EditorAt(index); e != nil {
		s.groupFocused(group)
		s.closeEditors(group, []editorgroup.EditorInput{e})
	}
}

// emptyArea is the area of an empty group: the notice of a file that looks binary, with a button to
// open it anyway, or the home page.
func (s *shellState) emptyArea(bc widgets.BuildContext) widgets.Widget {
	if s.blocked != "" {
		return editor.NewNotice(BinaryFileNotice, editor.NoticeOpts{Action: "Open Anyway", OnAction: func() { s.openAnyway(widgets.WidgetsBindingOf(bc), s.blocked) }})
	}
	return s.homePage(bc)
}

// tabIcon is the icon of the tab of an editor of a kind that has one (the settings editor's gear); a text editor's
// tab shows none (the file icons of step 6.14 are for the explorer).
func (s *shellState) tabIcon(colors kit.Colors, e editorgroup.EditorInput, active bool) widgets.Widget {
	k, ok := kindOf(e.TypeID())
	if !ok || k.icon == 0 {
		return nil
	}
	color := colors.TabInactiveForeground
	if active {
		color = colors.TabActiveForeground
	}
	return widgets.NewIcon(fonts.CodiconIcon(k.icon), widgets.IconOpts{Size: foundation.Some(16.0), Color: foundation.Some(color)})
}
