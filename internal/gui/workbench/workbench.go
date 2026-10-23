package workbench

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// PanelTab is a view of the panel: its id and the title it has in the title strip of the panel.
type PanelTab struct {
	ID    string
	Title string
}

// Options configure a Workbench. Layout is required; everything else may be left out.
type Options struct {
	// Layout is the model of the layout, which the commands change and the widget shows.
	Layout *Layout
	// Commands is the registry whose layout commands the workbench takes over while it is on screen
	// (command.DefaultRegistry has them). Without it no command is bound.
	Commands *command.Registry
	// Keys is the root scope of the context keys of the window, where the workbench sets sideBarVisible,
	// sideBarFocus, panelVisible, panelFocus, panelPosition, panelAlignment, panelMaximized,
	// mainEditorAreaVisible, editorAreaFocus and statusBarFocused. Each part also has a scope of its
	// own below it, for the views in it.
	Keys *contextkey.Scope
	// Scopes registers the focus node of each part with its scope, so that the key dispatcher of the
	// window resolves keys in the scope of the part that has the focus.
	Scopes *kit.FocusScopes
	// Settings gives window.menuBarVisibility, and is where the commands that VS Code keeps in a setting
	// (the status bar, the activity bar, the menu bar) write it; the layout follows the settings
	// workbench.sideBar.location, workbench.activityBar.location, workbench.statusBar.visible and
	// workbench.panel.opensMaximized. Without it the commands change the layout directly and the menu
	// bar is the classic one.
	Settings *settings.Store
	// Dialogs is the queue of the dialogs of the window; the workbench shows them over everything, once.
	Dialogs *kit.DialogController
	// Editor is what the editor area shows.
	Editor widgets.Widget
	// Menus are the menus of the menu bar, and OnRunMenu is told which item the user ran.
	Menus     []kit.MenuBarMenu
	OnRunMenu func(item kit.MenuItem)
	// ActivityTop and ActivityBottom are the icons at the top of the activity bar (the view containers)
	// and at the bottom (the Manage entry).
	ActivityTop, ActivityBottom widgets.Widget
	// SideBarTitle is the title of the active view container, and SideBar its body.
	SideBarTitle string
	SideBar      widgets.Widget
	// PanelTabs are the views of the panel and ActivePanel the id of the one that shows; Panel is the
	// body, and OnSelectPanel is told which tab the user clicked.
	PanelTabs     []PanelTab
	ActivePanel   string
	Panel         widgets.Widget
	OnSelectPanel func(id string)
	// StatusLeft and StatusRight are the items at the two ends of the status bar. NoFolder gives the
	// bar the colors of a window with no folder.
	StatusLeft, StatusRight []widgets.Widget
	NoFolder                bool
	// Context is the context of the commands the workbench runs; context.Background() when nil.
	Context context.Context
}

// Workbench is the layout of the window (SPEC §10.1): the menu bar over a row of the activity bar, the
// primary side bar and the center, over the status bar; the center is the editor area and the panel,
// which lies below, above, left or right of the editors. Each part can be hidden, the side bar and the
// panel are resized by their sashes, and the editor area takes what the window gains or loses. The
// dialogs of the window show over all of it.
//
// Ported from VS Code: src/vs/workbench/browser/layout.ts and src/vs/workbench/browser/parts/
type Workbench struct {
	widgets.StatefulWidget
	opts Options
}

// New makes the workbench of the options.
func New(opts Options) *Workbench {
	return &Workbench{StatefulWidget: widgets.NewStatefulWidgetBase(), opts: opts}
}

// CreateState implements widgets.StatefulWidget.
func (w *Workbench) CreateState() widgets.State { return &workbenchState{} }

// partKeys are the keys of the widgets of the parts.
var partKeys = map[Part]foundation.Key{}

func init() {
	for _, p := range []Part{PartMenuBar, PartActivityBar, PartSideBar, PartEditor, PartPanel, PartStatusBar} {
		partKeys[p] = foundation.NewValueKey("workbench:" + p.String())
	}
}

// PartKey is the key of the widget of a part, which a test finds it by. The widget of a part that is
// hidden is off stage.
func PartKey(p Part) foundation.Key { return partKeys[p] }

type workbenchState struct {
	widgets.StateOf[*Workbench]
	binding *widgets.WidgetsBinding
	layout  *Layout
	// nodes are the focus scopes of the parts and scopes their context key scopes.
	nodes  map[Part]*widgets.FocusScopeNode
	scopes map[Part]*contextkey.Scope
	// global are the global keys of the parts that move (with the position of the side bar and of the
	// panel), so that a part keeps its state, and what it shows, when it goes to another place.
	global map[Part]*widgets.GlobalKey[widgets.State]
	// ctl are the controllers of the split views.
	mainCtl, centerCtl *kit.SplitViewController
	menuCtl            *kit.MenuBarController
	// menuShown is whether the menu bar is shown while its visibility is toggle.
	menuShown bool
	// wasMaximized is the maximized state the last change was seen in, and restorePanel asks for the
	// panel to get back its size once the editor area is shown again.
	wasMaximized, restorePanel bool
	removeLayout               func()
	stopFollow                 func()
	removeSettings             func()
	removeCommands             []func()
	unregister                 []func()
}

// partOrder lists the parts that have a focus scope.
var partOrder = []Part{PartActivityBar, PartSideBar, PartEditor, PartPanel, PartStatusBar}

// focusKeys are the context keys that tell a part has the focus (VS Code's contextkeys.ts); the
// activity bar has none.
var focusKeys = map[Part]string{
	PartSideBar: "sideBarFocus", PartEditor: "editorAreaFocus", PartPanel: "panelFocus", PartStatusBar: "statusBarFocused",
}

// InitState makes the focus scopes of the parts, binds the commands, and follows the layout and
// the settings.
func (s *workbenchState) InitState() {
	o := s.Widget().opts
	s.binding = widgets.WidgetsBindingOf(s.Context())
	s.layout = o.Layout
	s.nodes = map[Part]*widgets.FocusScopeNode{}
	s.scopes = map[Part]*contextkey.Scope{}
	s.global = map[Part]*widgets.GlobalKey[widgets.State]{}
	s.mainCtl, s.centerCtl = kit.NewSplitViewController(), kit.NewSplitViewController()
	s.menuCtl = kit.NewMenuBarController()
	for _, p := range partOrder {
		node := widgets.NewFocusScopeNode(widgets.FocusScopeNodeOpts{FocusNodeOpts: widgets.FocusNodeOpts{DebugLabel: p.String()}})
		s.nodes[p] = node
		s.global[p] = widgets.NewGlobalKey[widgets.State](widgets.GlobalKeyOpts{DebugLabel: p.String()})
		if o.Keys != nil {
			scope := o.Keys.Child()
			s.scopes[p] = scope
			if o.Scopes != nil {
				s.unregister = append(s.unregister, o.Scopes.Register(&node.FocusNode, scope))
			}
		}
	}
	s.wasMaximized = s.layout.PanelMaximized()
	s.stopFollow = s.layout.FollowSettings(o.Settings, s.binding.Post)
	s.removeLayout = s.layout.OnChange(s.layoutChanged)
	if o.Settings != nil {
		s.removeSettings = o.Settings.OnChange(func(e settings.ChangeEvent) {
			if e.Affects("window.menuBarVisibility", "") {
				s.binding.Post(func() {
					if s.Mounted() {
						s.SetState(func() {})
					}
				})
			}
		})
	}
	s.syncKeys()
	s.bindCommands()
}

// Dispose stops following and gives the commands back.
func (s *workbenchState) Dispose() {
	for _, f := range s.removeCommands {
		f()
	}
	for _, f := range s.unregister {
		f()
	}
	if s.removeLayout != nil {
		s.removeLayout()
	}
	if s.stopFollow != nil {
		s.stopFollow()
	}
	if s.removeSettings != nil {
		s.removeSettings()
	}
	for _, n := range s.nodes {
		n.Dispose()
	}
}

// DidUpdateWidget keeps the context keys current.
func (s *workbenchState) DidUpdateWidget(*Workbench) { s.syncKeys() }

// layoutChanged rebuilds the workbench after the layout changed, moves the focus out of a part that
// was hidden, and keeps the context keys.
func (s *workbenchState) layoutChanged() {
	if !s.Mounted() {
		return
	}
	maximized := s.layout.PanelMaximized()
	if s.wasMaximized && !maximized {
		s.restorePanel = true
	}
	s.wasMaximized = maximized
	// the focus must not stay in a part that is gone
	for _, p := range partOrder {
		if !s.layout.Visible(p) && s.nodes[p].HasFocus() {
			if s.layout.Visible(PartEditor) {
				s.nodes[PartEditor].RequestFocus()
			} else {
				s.nodes[PartPanel].RequestFocus()
			}
			break
		}
	}
	s.syncKeys()
	s.SetState(func() {})
}

// syncKeys sets the context keys of the layout in the scope of the window.
func (s *workbenchState) syncKeys() {
	keys := s.Widget().opts.Keys
	if keys == nil {
		return
	}
	st := s.layout.State()
	keys.Batch(func() {
		keys.Set("sideBarVisible", st.SideBarVisible)
		keys.Set("panelVisible", st.PanelVisible)
		keys.Set("panelPosition", string(st.PanelPosition))
		keys.Set("panelAlignment", "center")
		keys.Set("panelMaximized", s.layout.PanelMaximized())
		keys.Set("mainEditorAreaVisible", s.layout.Visible(PartEditor))
	})
}

// focusChanged sets the context key of a part that got or lost the focus.
func (s *workbenchState) focusChanged(p Part, has bool) {
	if key, ok := focusKeys[p]; ok {
		if keys := s.Widget().opts.Keys; keys != nil {
			keys.Set(key, has)
		}
	}
}

// menuBarVisibility is window.menuBarVisibility.
func (s *workbenchState) menuBarVisibility() kit.MenuBarVisibility {
	st := s.Widget().opts.Settings
	if st == nil {
		return kit.MenuBarClassic
	}
	v, _ := kit.ParseMenuBarVisibility(settings.Get[string](st, "window.menuBarVisibility", ""))
	return v
}

// Build implements widgets.State.
func (s *workbenchState) Build(bc widgets.BuildContext) widgets.Widget {
	o := s.Widget().opts
	colors := kit.ThemeOf(bc)
	st := s.layout.State()
	rows := []widgets.Widget{
		s.menuBarArea(colors),
		widgets.NewExpanded(s.main(colors)),
	}
	if st.StatusBarVisible {
		rows = append(rows, s.statusBar(bc, colors))
	}
	var body widgets.Widget = widgets.NewColumn(rows, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	if s.restorePanel {
		s.restorePanel = false
		s.binding.Scheduler().AddPostFrameCallback(func(time.Duration) { s.restorePanelSize() })
	}
	if o.Dialogs != nil {
		body = kit.NewDialogHost(body, o.Dialogs)
	}
	return body
}

// restorePanelSize gives the panel the size it had before it was maximized, as the editor area comes
// back.
func (s *workbenchState) restorePanelSize() {
	if !s.Mounted() || s.layout.PanelMaximized() || !s.layout.State().PanelVisible {
		return
	}
	_, slots := s.layout.centerSlots()
	if i := indexOf(slots, PartPanel); i >= 0 {
		s.centerCtl.SetSize(i, s.layout.PanelSize())
	}
}

// side is a side of a part, where its border is.
type side int

const (
	sideNone side = iota
	sideTop
	sideBottom
	sideLeft
	sideRight
)

// bordered paints a background under a child and a line of one pixel on a side of it.
func bordered(bg, line ui.Color, at side, child widgets.Widget) widgets.Widget {
	children := []widgets.Widget{widgets.NewColoredBox(bg, child)}
	if line.Alpha() != 0 && at != sideNone {
		p := widgets.PositionedOpts{}
		switch at {
		case sideTop:
			p.Left, p.Right, p.Top, p.Height = foundation.Some(0.0), foundation.Some(0.0), foundation.Some(0.0), foundation.Some(1.0)
		case sideBottom:
			p.Left, p.Right, p.Bottom, p.Height = foundation.Some(0.0), foundation.Some(0.0), foundation.Some(0.0), foundation.Some(1.0)
		case sideLeft:
			p.Top, p.Bottom, p.Left, p.Width = foundation.Some(0.0), foundation.Some(0.0), foundation.Some(0.0), foundation.Some(1.0)
		case sideRight:
			p.Top, p.Bottom, p.Right, p.Width = foundation.Some(0.0), foundation.Some(0.0), foundation.Some(0.0), foundation.Some(1.0)
		}
		children = append(children, widgets.NewPositioned(widgets.NewIgnorePointer(widgets.NewColoredBox(line, widgets.NewSizedBox(nil))), p))
	}
	return widgets.NewStack(children, widgets.StackOpts{Fit: rendering.StackFitExpand})
}

// frame is a part: its focus scope around a background with a border. A part that is hidden takes no
// focus.
func (s *workbenchState) frame(p Part, bg, line ui.Color, at side, child widgets.Widget) widgets.Widget {
	scope := widgets.NewFocusScope(bordered(bg, line, at, child), widgets.FocusScopeOpts{
		Node: s.nodes[p], DebugLabel: p.String(),
		OnFocusChange: func(has bool) { s.focusChanged(p, has) },
	})
	return widgets.NewKeyedSubtree(widgets.NewKeyedSubtree(
		widgets.NewExcludeFocus(scope, widgets.ExcludeFocusOpts{NotExcluding: s.layout.Visible(p)}),
		widgets.WidgetOpts{Key: PartKey(p)}), widgets.WidgetOpts{Key: s.global[p]})
}

// menuBarArea is the menu bar at the top. It is as high as the bar when the bar is shown (always with
// classic and visible, after the Alt key with toggle) and has no height otherwise; the bar itself is
// always there, for the Alt key.
func (s *workbenchState) menuBarArea(colors kit.Colors) widgets.Widget {
	o := s.Widget().opts
	visibility := s.menuBarVisibility()
	shown := visibility == kit.MenuBarClassic || visibility == kit.MenuBarVisible || (visibility == kit.MenuBarToggle && s.menuShown)
	height := 0.0
	if shown {
		height = MenuBarHeight
	}
	bar := kit.NewMenuBar(o.Menus, kit.MenuBarOpts{
		Visibility: visibility, Controller: s.menuCtl, Height: foundation.Some(MenuBarHeight),
		OnRun: o.OnRunMenu,
		OnVisibilityChange: func(v bool) {
			if visibility == kit.MenuBarToggle && v != s.menuShown {
				s.binding.Post(func() {
					if s.Mounted() {
						s.SetState(func() { s.menuShown = v })
					}
				})
			}
		},
	})
	return widgets.NewKeyedSubtree(
		widgets.NewSizedBox(
			bordered(colors.TitleBarActiveBackground, colors.TitleBarBorder, sideBottom, widgets.NewAlign(bar, widgets.AlignOpts{Alignment: painting.AlignmentCenterLeft})),
			widgets.SizedBoxOpts{Height: foundation.Some(height)}),
		widgets.WidgetOpts{Key: PartKey(PartMenuBar)})
}

// main is the row between the menu bar and the status bar.
func (s *workbenchState) main(colors kit.Colors) widgets.Widget {
	st := s.layout.State()
	return widgets.NewLayoutBuilder(func(_ widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
		slots := fitSlots(s.layout.mainSlots(), c.MaxWidth, partCenter)
		panes := make([]kit.SplitPane, len(slots))
		for i, sl := range slots {
			var child widgets.Widget
			switch sl.part {
			case PartActivityBar:
				child = s.activityBar(colors)
			case PartSideBar:
				child = s.sideBar(colors)
			default:
				child = s.center(colors)
			}
			panes[i] = splitPane(sl, child)
		}
		split := kit.NewSplitView(panes, kit.SplitViewOpts{
			Orientation: kit.SplitHorizontal, Scrollbar: kit.ScrollbarHidden, Controller: s.mainCtl,
			OnDidChange: s.mainSizes,
		})
		return widgets.NewKeyedSubtree(split, widgets.WidgetOpts{Key: foundation.NewValueKey(fmt.Sprintf("main:%s:%d", st.SideBarPosition, s.layout.restores))})
	})
}

func splitPane(sl slot, child widgets.Widget) kit.SplitPane {
	it := sl.item
	return kit.SplitPane{
		Child: child, Size: it.Size, MinSize: it.MinSize, MaxSize: it.MaxSize, Priority: it.Priority,
		PreferredSize: it.PreferredSize, Hidden: it.Hidden,
	}
}

// mainSizes records the width the side bar was dragged to.
func (s *workbenchState) mainSizes(sizes []float64) {
	if !s.Mounted() {
		return
	}
	if i := indexOf(s.layout.mainSlots(), PartSideBar); i >= 0 && i < len(sizes) && sizes[i] > 0 {
		s.layout.SetSideBarWidth(sizes[i])
	}
}

// center is the editor area and the panel.
func (s *workbenchState) center(colors kit.Colors) widgets.Widget {
	st := s.layout.State()
	return widgets.NewLayoutBuilder(func(_ widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
		orientation, slots := s.layout.centerSlots()
		total := c.MaxWidth
		if orientation == kit.SplitVertical {
			total = c.MaxHeight
		}
		slots = fitSlots(slots, total, PartEditor)
		panes := make([]kit.SplitPane, len(slots))
		for i, sl := range slots {
			if sl.part == PartEditor {
				panes[i] = splitPane(sl, s.editorArea(colors))
			} else {
				panes[i] = splitPane(sl, s.panel(colors))
			}
		}
		split := kit.NewSplitView(panes, kit.SplitViewOpts{
			Orientation: orientation, Scrollbar: kit.ScrollbarHidden, Controller: s.centerCtl,
			OnDidChange: s.centerSizes,
		})
		return widgets.NewKeyedSubtree(split, widgets.WidgetOpts{Key: foundation.NewValueKey(fmt.Sprintf("center:%s:%d", st.PanelPosition, s.layout.restores))})
	})
}

// centerSizes records the size the panel was dragged to.
func (s *workbenchState) centerSizes(sizes []float64) {
	if !s.Mounted() {
		return
	}
	_, slots := s.layout.centerSlots()
	if i := indexOf(slots, PartPanel); i >= 0 && i < len(sizes) && sizes[i] > 0 {
		s.layout.SetPanelSize(sizes[i])
	}
}

// editorArea hosts the editor.
func (s *workbenchState) editorArea(colors kit.Colors) widgets.Widget {
	var child widgets.Widget = widgets.NewSizedBox(nil)
	if e := s.Widget().opts.Editor; e != nil {
		child = e
	}
	return s.frame(PartEditor, colors.EditorBackground, ui.Color{}, sideNone, child)
}

// activityBar is the strip of the view containers, with the Manage entry at its bottom.
func (s *workbenchState) activityBar(colors kit.Colors) widgets.Widget {
	o := s.Widget().opts
	at := sideRight
	if s.layout.State().SideBarPosition == SideBarRight {
		at = sideLeft
	}
	top, bottom := o.ActivityTop, o.ActivityBottom
	if top == nil {
		top = widgets.NewSizedBox(nil)
	}
	if bottom == nil {
		bottom = widgets.NewSizedBox(nil)
	}
	col := widgets.NewColumn([]widgets.Widget{widgets.NewExpanded(top), bottom},
		widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	return s.frame(PartActivityBar, colors.ActivityBarBackground, colors.ActivityBarBorder, at, col)
}

// titleStrip is a row of the height of a part title.
func titleStrip(child widgets.Widget) widgets.Widget {
	return widgets.NewSizedBox(child, widgets.SizedBoxOpts{Height: foundation.Some(PartTitleHeight)})
}

// opaqueOr is the color, or the fallback where the theme has none.
func opaqueOr(c, fallback ui.Color) ui.Color {
	if c.Alpha() == 0 {
		return fallback
	}
	return c
}

// sideBar is the primary side bar: the title of the active view container over its body.
func (s *workbenchState) sideBar(colors kit.Colors) widgets.Widget {
	o := s.Widget().opts
	at := sideRight
	if s.layout.State().SideBarPosition == SideBarRight {
		at = sideLeft
	}
	fg := opaqueOr(colors.SideBarTitleForeground, opaqueOr(colors.SideBarForeground, colors.Foreground))
	body := o.SideBar
	if body == nil {
		body = widgets.NewSizedBox(nil)
	}
	title := widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
		return bordered(colors.SideBarTitleBackground, colors.SideBarTitleBorder, sideBottom,
			widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(20, 0, 8, 0),
				widgets.NewAlign(widgets.NewText(strings.ToUpper(o.SideBarTitle), widgets.TextOpts{
					Style: kit.TextStyleOf(bc, 11, fg), Overflow: painting.TextOverflowEllipsis, MaxLines: foundation.Some(1)}),
					widgets.AlignOpts{Alignment: painting.AlignmentCenterLeft})))
	})
	col := widgets.NewColumn([]widgets.Widget{titleStrip(title), widgets.NewExpanded(body)},
		widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	return s.frame(PartSideBar, colors.SideBarBackground, colors.SideBarBorder, at, col)
}

// panel is the panel: a title strip with the tabs of its views and the buttons that maximize and close
// it, over the body of the active view.
func (s *workbenchState) panel(colors kit.Colors) widgets.Widget {
	o := s.Widget().opts
	position := s.layout.State().PanelPosition
	at := map[PanelPosition]side{PanelBottom: sideTop, PanelTop: sideBottom, PanelLeft: sideRight, PanelRight: sideLeft}[position]
	body := o.Panel
	if body == nil {
		body = widgets.NewSizedBox(nil)
	}
	maximized := s.layout.PanelMaximized()
	title := widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
		tabs := make([]widgets.Widget, 0, len(o.PanelTabs))
		for _, tab := range o.PanelTabs {
			tabs = append(tabs, s.panelTab(bc, colors, tab, tab.ID == o.ActivePanel))
		}
		maximizeIcon, maximizeTip := fonts.CodiconChevronUp, "Maximize Panel"
		if maximized {
			maximizeIcon, maximizeTip = fonts.CodiconChevronDown, "Restore Panel"
		}
		actions := widgets.NewRow([]widgets.Widget{
			kit.NewIconButton(maximizeIcon, kit.IconButtonOpts{Tooltip: maximizeTip, OnPressed: s.toggleMaximizedPanel}),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(2.0)}),
			kit.NewIconButton(fonts.CodiconClose, kit.IconButtonOpts{Tooltip: "Hide Panel", OnPressed: func() { s.layout.SetPanelVisible(false) }}),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}),
		}, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
		// the tabs that do not fit are scrolled, not cut: a narrow panel at a side has little room
		strip := widgets.NewSingleChildScrollView(
			widgets.NewRow(tabs, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentStretch}),
			widgets.SingleChildScrollViewOpts{ScrollDirection: painting.AxisHorizontal, Primary: foundation.Some(false)})
		row := widgets.NewRow([]widgets.Widget{
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}),
			widgets.NewExpanded(strip), actions,
		}, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
		return bordered(colors.PanelBackground, colors.PanelTitleBorder, sideBottom, row)
	})
	col := widgets.NewColumn([]widgets.Widget{titleStrip(title), widgets.NewExpanded(body)},
		widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	return s.frame(PartPanel, colors.PanelBackground, colors.PanelBorder, at, col)
}

// panelTab is the title of a view in the title strip of the panel: uppercase 11 px text, with a line
// under the active one.
func (s *workbenchState) panelTab(bc widgets.BuildContext, colors kit.Colors, tab PanelTab, active bool) widgets.Widget {
	fg := colors.PanelTitleInactiveForeground
	line := ui.Color{}
	if active {
		fg, line = colors.PanelTitleActiveForeground, colors.PanelTitleActiveBorder
	}
	label := widgets.NewPadding(painting.NewEdgeInsetsSymmetric(0, 10),
		widgets.NewCenter(widgets.NewText(strings.ToUpper(tab.Title), widgets.TextOpts{Style: kit.TextStyleOf(bc, 11, fg)})))
	var child widgets.Widget = label
	if line.Alpha() != 0 {
		child = widgets.NewStack([]widgets.Widget{
			label,
			widgets.NewPositioned(widgets.NewColoredBox(line, widgets.NewSizedBox(nil)), widgets.PositionedOpts{
				Left: foundation.Some(10.0), Right: foundation.Some(10.0), Bottom: foundation.Some(0.0), Height: foundation.Some(1.0)}),
		}, widgets.StackOpts{Fit: rendering.StackFitPassthrough})
	}
	if s.Widget().opts.OnSelectPanel == nil {
		return child
	}
	id := tab.ID
	return widgets.NewGestureDetector(child, widgets.GestureDetectorOpts{
		Behavior: rendering.HitTestBehaviorOpaque, OnTap: func() { s.Widget().opts.OnSelectPanel(id) }})
}

// statusBar is the strip at the bottom with the items at its two ends.
func (s *workbenchState) statusBar(bc widgets.BuildContext, colors kit.Colors) widgets.Widget {
	o := s.Widget().opts
	bg, fg, line := colors.StatusBarBackground, colors.StatusBarForeground, colors.StatusBarBorder
	if o.NoFolder {
		bg, fg, line = colors.StatusBarNoFolderBackground, colors.StatusBarNoFolderForeground, colors.StatusBarNoFolderBorder
	}
	// a theme that sets no background (the high contrast ones) leaves the status bar the workbench's
	if bg.Alpha() == 0 {
		bg = colors.EditorBackground
	}
	items := make([]widgets.Widget, 0, len(o.StatusLeft)+len(o.StatusRight)+1)
	items = append(items, o.StatusLeft...)
	items = append(items, widgets.NewSpacer())
	items = append(items, o.StatusRight...)
	row := widgets.NewRow(items, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	content := widgets.NewDefaultTextStyle(kit.TextStyleOf(bc, 12, fg), row)
	return widgets.NewSizedBox(s.frame(PartStatusBar, bg, line, sideTop, content), widgets.SizedBoxOpts{Height: foundation.Some(StatusBarHeight)})
}
