package workbench

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
)

// PanelTab is a view of the panel: its id and the title it has in the title strip of the panel.
type PanelTab struct {
	ID    string
	Title string
	// Badge, when it is above 0, is the count shown after the title (the Problems of the panel), and
	// BadgeTooltip its hover text.
	Badge        int
	BadgeTooltip string
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
	// MenuBar, when set, is the controller of the menu bar, with which the caller opens a menu or
	// reads the state of the bar; the workbench makes its own without it.
	MenuBar *kit.MenuBarController
	// Views, when set, fills the activity bar with an icon for each view container of the side bar,
	// titles the side bar with the container that is active and shows the views of that container
	// in it (SPEC §10.4), unless the slots below say otherwise.
	Views *Views
	// ViewContext is the context the `when` of the views is evaluated in; every view shows when nil.
	ViewContext contextkey.Context
	// ViewBody builds the body of a view; a view it gives nothing for has an empty body.
	ViewBody func(bc widgets.BuildContext, viewID string) widgets.Widget
	// ViewName, ViewActions, ViewBodyHeight and OnViewFocus are the slots of SideBarViewsOpts of the same
	// names: the title of a view when it is not the registry's, the buttons in its header, a body of a
	// fixed height, and the focus entering or leaving a view.
	ViewName       func(viewID string) string
	ViewActions    func(bc widgets.BuildContext, viewID string) widgets.Widget
	ViewBodyHeight func(viewID string) (float64, bool)
	OnViewFocus    func(viewID string, has bool)
	// ManageItems gives the items of the Manage menu when the user opens it, from the gear at the
	// bottom of the activity bar (it has none without this); OnRunMenu is told which one ran.
	ManageItems func() []kit.MenuItem
	// Manage, when set, opens the Manage menu from outside.
	Manage *ManageController
	// ActivityTop and ActivityBottom are the icons at the top of the activity bar (the view containers)
	// and at the bottom (the Manage entry); they replace what Views and ManageItems make.
	ActivityTop, ActivityBottom widgets.Widget
	// SideBarTitle is the title of the active view container, and SideBar its body; they replace what
	// Views makes.
	SideBarTitle string
	SideBar      widgets.Widget
	// PanelTabs are the views of the panel and ActivePanel the id of the one that shows; Panel is the
	// body, and OnSelectPanel is told which tab the user clicked.
	PanelTabs     []PanelTab
	ActivePanel   string
	Panel         widgets.Widget
	OnSelectPanel func(id string)
	// PanelActions are the actions of the active view of the panel (the filter of the Problems, the
	// channel of the Output), shown in the title strip before the buttons that maximize and hide the
	// panel.
	PanelActions widgets.Widget
	// FocusPanelView gives the keyboard focus to the content of a view container of the panel, once the
	// commands that open a view with the focus have shown it (the tree of the Problems, the editor of
	// the Output). Without it the focus goes to the panel itself.
	FocusPanelView func(container string)
	// QuickInput, when set, shows the quick input (the command palette and the pickers) over the workbench,
	// under the menu bar, above the notifications and below the dialogs (SPEC §10.6). The window keeps the
	// controller; the workbench takes over workbench.action.closeQuickOpen while it is on screen.
	QuickInput *quickinput.Controller
	// Notifications, when set, shows the toasts and the notification center of the presenter over the
	// workbench, below the dialogs, and binds the notifications.* commands (SPEC §10.7).
	Notifications *notify.Presenter
	// StatusBar, when set, fills the status bar with the items of its registry (SPEC §10.1): the left ones
	// from the left end, the right ones to the right end. A click on an item runs its command with the
	// registry of Commands.
	StatusBar *StatusBarOptions
	// StatusLeft and StatusRight are more widgets at the two ends of the status bar, outside the registry.
	// NoFolder gives the bar the colors of a window with no folder.
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
	// statusDelegate is shared by the hovers of the items of the status bar.
	statusDelegate *kit.HoverDelegate
	removeLayout   func()
	stopFollow     func()
	removeSettings func()
	removeViews    func()
	removeCommands []func()
	unregister     []func()
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
	s.menuCtl = o.MenuBar
	if s.menuCtl == nil {
		s.menuCtl = kit.NewMenuBarController()
	}
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
	if o.Views != nil {
		s.removeViews = o.Views.OnChange(func() {
			if s.Mounted() {
				s.syncKeys()
				s.SetState(func() {})
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
	if s.removeViews != nil {
		s.removeViews()
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
		if views := s.Widget().opts.Views; views != nil {
			// activePanel is the container the panel shows, and problemsVisibility says that it is the Problems
			// and that the panel is shown
			keys.Set("activePanel", views.ActivePanel())
			keys.Set("problemsVisibility", st.PanelVisible && views.ActivePanel() == command.ViewContainerMarkers)
		}
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
	if o.Notifications != nil {
		inset := 0.0
		if st.StatusBarVisible {
			inset = StatusBarHeight
		}
		top := 0.0
		if s.menuBarShown() {
			top = MenuBarHeight
		}
		body = NewNotificationsHost(body, NotificationsOptions{
			Presenter: o.Notifications, Commands: o.Commands, Keys: o.Keys, Context: o.Context, BottomInset: inset, TopInset: top,
		})
	}
	if o.QuickInput != nil {
		top := 0.0
		if s.menuBarShown() {
			top = MenuBarHeight
		}
		body = quickinput.NewHost(body, quickinput.HostOptions{
			Controller: o.QuickInput, Keys: o.Keys, Scopes: o.Scopes, Commands: o.Commands, Settings: o.Settings, TopInset: top,
		})
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

// menuBarShown says whether the menu bar takes room at the top now.
func (s *workbenchState) menuBarShown() bool {
	v := s.menuBarVisibility()
	return v == kit.MenuBarClassic || v == kit.MenuBarVisible || (v == kit.MenuBarToggle && s.menuShown)
}

// menuBarArea is the menu bar at the top. It is as high as the bar when the bar is shown (always with
// classic and visible, after the Alt key with toggle) and has no height otherwise; the bar itself is
// always there, for the Alt key.
func (s *workbenchState) menuBarArea(colors kit.Colors) widgets.Widget {
	o := s.Widget().opts
	visibility := s.menuBarVisibility()
	shown := s.menuBarShown()
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
	if top == nil && o.Views != nil {
		top = NewActivityIcons(o.Views, s.layout)
	}
	if bottom == nil && o.ManageItems != nil {
		bottom = NewManageButton(o.ManageItems, o.OnRunMenu, o.Manage)
	}
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
	body, name := o.SideBar, o.SideBarTitle
	if o.Views != nil {
		if body == nil {
			body = NewSideBarViews(o.Views, SideBarViewsOpts{
				Context: o.ViewContext, Body: o.ViewBody, Name: o.ViewName, Actions: o.ViewActions,
				BodyHeight: o.ViewBodyHeight, OnFocusChange: o.OnViewFocus,
			})
		}
		if c, ok := o.Views.ActiveContainer(); ok && name == "" {
			name = c.Title
		}
	}
	if body == nil {
		body = widgets.NewSizedBox(nil)
	}
	title := widgets.NewBuilder(func(bc widgets.BuildContext) widgets.Widget {
		return bordered(colors.SideBarTitleBackground, colors.SideBarTitleBorder, sideBottom,
			widgets.NewPadding(painting.NewEdgeInsetsFromLTRB(20, 0, 8, 0),
				widgets.NewAlign(widgets.NewText(strings.ToUpper(name), widgets.TextOpts{
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
	title := widgets.NewLayoutBuilder(func(bc widgets.BuildContext, cons rendering.BoxConstraints) widgets.Widget {
		tabs := make([]widgets.Widget, 0, len(o.PanelTabs))
		for _, tab := range o.PanelTabs {
			tabs = append(tabs, s.panelTab(bc, colors, tab, tab.ID == o.ActivePanel))
		}
		maximizeIcon, maximizeTip := fonts.CodiconChevronUp, "Maximize Panel"
		if maximized {
			maximizeIcon, maximizeTip = fonts.CodiconChevronDown, "Restore Panel"
		}
		var viewActions []widgets.Widget
		// the actions of the view take what the tabs and the buttons leave, and are left out when that is
		// too little to be of use
		room := cons.MaxWidth - panelTitleReserved
		if o.PanelActions != nil && room >= panelActionsMinWidth {
			viewActions = append(viewActions,
				widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(room)}), o.PanelActions),
				widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(4.0)}))
		}
		actions := widgets.NewRow(append(viewActions, []widgets.Widget{
			kit.NewIconButton(maximizeIcon, kit.IconButtonOpts{Tooltip: maximizeTip, OnPressed: s.toggleMaximizedPanel}),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(2.0)}),
			kit.NewIconButton(fonts.CodiconClose, kit.IconButtonOpts{Tooltip: "Hide Panel", OnPressed: func() { s.layout.SetPanelVisible(false) }}),
			widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(8.0)}),
		}...), widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
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

// What the title strip of the panel keeps for the tabs (at least) and the buttons that maximize and hide
// the panel, which the actions of the view are not given, and the least width the actions are shown in.
const (
	panelTitleReserved   = 8 + 60 + 150
	panelActionsMinWidth = 130.0
)

// panelTab is the title of a view in the title strip of the panel: uppercase 11 px text, with a line
// under the active one.
func (s *workbenchState) panelTab(bc widgets.BuildContext, colors kit.Colors, tab PanelTab, active bool) widgets.Widget {
	fg := colors.PanelTitleInactiveForeground
	line := ui.Color{}
	if active {
		fg, line = colors.PanelTitleActiveForeground, colors.PanelTitleActiveBorder
	}
	text := widgets.Widget(widgets.NewText(strings.ToUpper(tab.Title), widgets.TextOpts{Style: kit.TextStyleOf(bc, 11, fg)}))
	if tab.Badge > 0 {
		text = widgets.NewRow([]widgets.Widget{
			text, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(6.0)}), s.panelBadge(bc, colors, tab),
		}, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	}
	label := widgets.NewPadding(painting.NewEdgeInsetsSymmetric(0, 10), widgets.NewCenter(text))
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

// panelBadge is the count after the title of a tab of the panel: VS Code's count badge in the colors of
// the badge of a panel title.
//
// Ported from VS Code: src/vs/workbench/browser/parts/compositeBarActions.ts (CompositeBarActionViewItem.updateBadge)
func (s *workbenchState) panelBadge(bc widgets.BuildContext, colors kit.Colors, tab PanelTab) widgets.Widget {
	style := kit.TextStyleOf(bc, 9, colors.PanelTitleBadgeForeground).Merge(painting.NewTextStyle(painting.TextStyleOpts{FontWeight: ui.FontWeightW600}))
	text := strconv.Itoa(tab.Badge)
	if tab.Badge > 999 {
		text = "999+"
	}
	var pill widgets.Widget = widgets.NewConstrainedBox(
		rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MinWidth: foundation.Some(16.0), MinHeight: foundation.Some(16.0)}),
		widgets.NewDecoratedBox(
			painting.NewBoxDecoration(painting.BoxDecorationOpts{
				Color:        foundation.Some(colors.PanelTitleBadgeBackground),
				BorderRadius: painting.NewBorderRadiusCircular(8),
			}),
			widgets.NewPadding(painting.NewEdgeInsetsSymmetric(2, 5),
				widgets.NewText(text, widgets.TextOpts{Style: style, TextAlign: ui.TextAlignCenter, MaxLines: foundation.Some(1)}))))
	if tab.BadgeTooltip != "" {
		pill = kit.NewHover(pill, kit.HoverOpts{Text: tab.BadgeTooltip, Compact: true})
	}
	return widgets.NewKeyedSubtree(pill, widgets.WidgetOpts{Key: PanelBadgeKey(tab.ID)})
}

// PanelBadgeKey is the key of the count after the title of a tab of the panel, which a test finds it by.
func PanelBadgeKey(id string) foundation.Key {
	return foundation.NewValueKey("workbench:panel-badge:" + id)
}

// context is the Go context of the commands the status bar runs.
func (s *workbenchState) context() context.Context {
	if c := s.Widget().opts.Context; c != nil {
		return c
	}
	return context.Background()
}

// rootKeys are the context keys the commands of the status bar run in: those of the window.
func (s *workbenchState) rootKeys() contextkey.Context {
	if k := s.Widget().opts.Keys; k != nil {
		return k
	}
	return contextkey.MapContext{}
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
	var row widgets.Widget
	if o.StatusBar != nil {
		if s.statusDelegate == nil {
			s.statusDelegate = kit.NewHoverDelegate(kit.HoverDelegateOpts{Instant: true})
		}
		row = newStatusItems(*o.StatusBar, o.Commands, s.context(), s.rootKeys(), s.statusDelegate)
	} else {
		items := make([]widgets.Widget, 0, len(o.StatusLeft)+len(o.StatusRight)+1)
		items = append(items, o.StatusLeft...)
		items = append(items, widgets.NewSpacer())
		items = append(items, o.StatusRight...)
		row = widgets.NewRow(items, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	}
	content := widgets.NewDefaultTextStyle(kit.TextStyleOf(bc, 12, fg), row)
	return widgets.NewSizedBox(s.frame(PartStatusBar, bg, line, sideTop, content), widgets.SizedBoxOpts{Height: foundation.Some(StatusBarHeight)})
}
