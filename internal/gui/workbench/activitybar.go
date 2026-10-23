package workbench

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Metrics of the items of VS Code's activity bar.
//
// Ported from VS Code: src/vs/workbench/browser/parts/activitybar/media/activitybarpart.css
const (
	// activityItemSize is the width and the height of an item; the icons are 24 px.
	activityItemSize = 48.0
	activityIconSize = 24.0
	// activityIndicatorWidth is the line beside the active item.
	activityIndicatorWidth = 2.0
)

// ManageKey is the key of the Manage button, which a test finds it by.
var ManageKey = foundation.NewValueKey("workbench:manage")

// ActivityItemKey is the key of the icon of a view container in the activity bar.
func ActivityItemKey(containerID string) foundation.Key {
	return foundation.NewValueKey("workbench:activity:" + containerID)
}

// activityItem is an icon of the activity bar: a Codicon of 24 px in a square of 48 px, in the color of
// an inactive item until the pointer is over it, in the color of an active one when its container is
// open, with a line of 2 px beside it then. It takes the focus and runs on Enter and Space.
type activityItem struct {
	widgets.StatefulWidget
	icon    int
	tooltip string
	active  bool
	onPress func()
	node    *widgets.FocusNode
}

func newActivityItem(key foundation.Key, icon int, tooltip string, active bool, node *widgets.FocusNode, onPress func()) *activityItem {
	return &activityItem{
		StatefulWidget: widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: key}),
		icon:           icon, tooltip: tooltip, active: active, onPress: onPress, node: node,
	}
}

// CreateState implements widgets.StatefulWidget.
func (a *activityItem) CreateState() widgets.State { return &activityItemState{} }

type activityItemState struct {
	widgets.StateOf[*activityItem]
	node                  *widgets.FocusNode
	ownNode               bool
	hover, pressed, focus bool
}

// InitState makes the focus node.
func (s *activityItemState) InitState() { s.pickNode() }

func (s *activityItemState) pickNode() {
	if n := s.Widget().node; n != nil {
		s.node, s.ownNode = n, false
		return
	}
	s.node, s.ownNode = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "ActivityItem"}), true
}

// DidUpdateWidget follows a new focus node.
func (s *activityItemState) DidUpdateWidget(old *activityItem) {
	if old.node != s.Widget().node {
		if s.ownNode {
			s.node.Dispose()
		}
		s.pickNode()
	}
}

// Dispose lets go of the focus node.
func (s *activityItemState) Dispose() {
	if s.ownNode {
		s.node.Dispose()
	}
}

func (s *activityItemState) set(f func()) {
	if s.Mounted() {
		s.SetState(f)
	}
}

func (s *activityItemState) press() {
	if f := s.Widget().onPress; f != nil {
		f()
	}
}

func (s *activityItemState) handleKey(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
	if _, ok := e.(*services.KeyDownEvent); ok {
		switch e.LogicalKey() {
		case services.LogicalKeyboardKeyEnter, services.LogicalKeyboardKeyNumpadEnter, services.LogicalKeyboardKeySpace:
			s.press()
			return widgets.KeyEventResultHandled
		}
	}
	return widgets.KeyEventResultIgnored
}

// Build implements widgets.State.
func (s *activityItemState) Build(bc widgets.BuildContext) widgets.Widget {
	a := s.Widget()
	colors := kit.ThemeOf(bc)
	fg := colors.ActivityBarInactiveForeground
	if a.active || s.hover || s.pressed {
		fg = colors.ActivityBarForeground
	}
	var bg ui.Color
	if a.active {
		bg = colors.ActivityBarActiveBackground
	}
	children := []widgets.Widget{
		widgets.NewColoredBox(bg, widgets.NewCenter(
			widgets.NewIcon(fonts.CodiconIcon(a.icon), widgets.IconOpts{Size: foundation.Some(activityIconSize), Color: foundation.Some(fg)}))),
	}
	if a.active && colors.ActivityBarActiveBorder.Alpha() != 0 {
		children = append(children, widgets.NewPositioned(
			widgets.NewIgnorePointer(widgets.NewColoredBox(colors.ActivityBarActiveBorder, widgets.NewSizedBox(nil))),
			widgets.PositionedOpts{Left: foundation.Some(0.0), Top: foundation.Some(0.0), Bottom: foundation.Some(0.0), Width: foundation.Some(activityIndicatorWidth)}))
	}
	if s.focus {
		outline := colors.FocusBorder
		if a.active && colors.ActivityBarActiveFocusBorder.Alpha() != 0 {
			outline = colors.ActivityBarActiveFocusBorder
		}
		children = append(children, widgets.NewPositioned(
			widgets.NewIgnorePointer(widgets.NewDecoratedBox(
				painting.NewBoxDecoration(painting.BoxDecorationOpts{
					Border: painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(outline), Width: foundation.Some(1.0)})}),
				widgets.NewSizedBox(nil))),
			widgets.PositionedOpts{Left: foundation.Some(1.0), Top: foundation.Some(1.0), Right: foundation.Some(1.0), Bottom: foundation.Some(1.0)}))
	}
	var body widgets.Widget = widgets.NewSizedBox(
		widgets.NewStack(children, widgets.StackOpts{Fit: rendering.StackFitExpand}),
		widgets.SizedBoxOpts{Width: foundation.Some(activityItemSize), Height: foundation.Some(activityItemSize)})
	body = widgets.NewMouseRegion(
		widgets.NewGestureDetector(body, widgets.GestureDetectorOpts{
			Behavior:    rendering.HitTestBehaviorOpaque,
			OnTapDown:   func(gestures.TapDownDetails) { s.set(func() { s.pressed = true }) },
			OnTapUp:     func(gestures.TapUpDetails) { s.set(func() { s.pressed = false }) },
			OnTapCancel: func() { s.set(func() { s.pressed = false }) },
			OnTap:       s.press,
		}),
		widgets.MouseRegionOpts{
			Cursor:  services.SystemMouseCursors.Click,
			OnEnter: func(*gestures.PointerEnterEvent) { s.set(func() { s.hover = true }) },
			OnExit:  func(*gestures.PointerExitEvent) { s.set(func() { s.hover = false }) },
		})
	body = widgets.NewFocus(body, widgets.FocusOpts{
		FocusNode: s.node, OnKeyEvent: s.handleKey, DebugLabel: "ActivityItem",
		OnFocusChange: func(has bool) { s.set(func() { s.focus = has }) },
	})
	if a.tooltip != "" {
		body = kit.NewHover(body, kit.HoverOpts{Text: a.tooltip, Compact: true, Position: kit.HoverRight})
	}
	return body
}

// ActivityIcons is the top of the activity bar: an icon for each view container of the side bar, in
// the order of the registry, with a tooltip of the container's title (SPEC §10.4). A click opens the
// container in the side bar, or hides the side bar when the container is open already. The icon of the
// container that the side bar shows is active; none is while the side bar is hidden.
//
// Ported from VS Code: src/vs/workbench/browser/parts/activitybar/activitybarPart.ts and
// src/vs/workbench/browser/parts/compositeBar.ts
type ActivityIcons struct {
	widgets.StatefulWidget
	views  *Views
	layout *Layout
}

// NewActivityIcons makes the icons of the view containers of views; layout is the layout of the
// window, whose side bar they show.
func NewActivityIcons(views *Views, layout *Layout) *ActivityIcons {
	return &ActivityIcons{StatefulWidget: widgets.NewStatefulWidgetBase(), views: views, layout: layout}
}

// CreateState implements widgets.StatefulWidget.
func (a *ActivityIcons) CreateState() widgets.State { return &activityIconsState{} }

type activityIconsState struct {
	widgets.StateOf[*ActivityIcons]
	stopViews, stopLayout func()
}

// InitState follows the views and the layout.
func (s *activityIconsState) InitState() {
	s.follow()
}

func (s *activityIconsState) follow() {
	s.unfollow()
	rebuild := func() {
		if s.Mounted() {
			s.SetState(func() {})
		}
	}
	w := s.Widget()
	s.stopViews = w.views.OnChange(rebuild)
	if w.layout != nil {
		s.stopLayout = w.layout.OnChange(rebuild)
	}
}

func (s *activityIconsState) unfollow() {
	if s.stopViews != nil {
		s.stopViews()
		s.stopViews = nil
	}
	if s.stopLayout != nil {
		s.stopLayout()
		s.stopLayout = nil
	}
}

// DidUpdateWidget follows new views.
func (s *activityIconsState) DidUpdateWidget(old *ActivityIcons) {
	if w := s.Widget(); old.views != w.views || old.layout != w.layout {
		s.follow()
	}
}

// Dispose stops following.
func (s *activityIconsState) Dispose() { s.unfollow() }

// Build implements widgets.State.
func (s *activityIconsState) Build(widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	visible := w.layout == nil || w.layout.State().SideBarVisible
	var items []widgets.Widget
	for _, c := range w.views.Registry().Containers(command.ViewLocationSideBar) {
		id := c.ID
		items = append(items, newActivityItem(ActivityItemKey(id), c.Icon, c.Title, visible && w.views.Active() == id, nil, func() { w.views.Toggle(id) }))
	}
	return widgets.NewColumn(items, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
}

// ManageController opens the Manage menu from outside, as a click on the gear does.
type ManageController struct{ state *manageState }

// NewManageController makes a controller; give it to the Manage option of the workbench.
func NewManageController() *ManageController { return &ManageController{} }

// Open opens the Manage menu, when the gear is on screen and the menu is not open.
func (c *ManageController) Open() {
	if c.state != nil && c.state.Mounted() && !c.state.menu.IsShowing() {
		c.state.show()
	}
}

// ManageButton is the gear at the bottom of the activity bar: it opens the Manage menu beside the
// activity bar, with the items that items gives at that moment, and onRun is told which one the user
// ran.
//
// Ported from VS Code: src/vs/workbench/browser/parts/globalCompositeBar.ts
type ManageButton struct {
	widgets.StatefulWidget
	items func() []kit.MenuItem
	onRun func(kit.MenuItem)
	ctl   *ManageController
}

// NewManageButton makes the Manage button; ctl, which may be nil, lets code outside open its menu.
func NewManageButton(items func() []kit.MenuItem, onRun func(kit.MenuItem), ctl *ManageController) *ManageButton {
	return &ManageButton{StatefulWidget: widgets.NewStatefulWidgetBase(), items: items, onRun: onRun, ctl: ctl}
}

// CreateState implements widgets.StatefulWidget.
func (b *ManageButton) CreateState() widgets.State { return &manageState{} }

type manageState struct {
	widgets.StateOf[*ManageButton]
	node *widgets.FocusNode
	menu *kit.ContextMenuController
	open bool
	// wasOpen is whether the menu was open when the pointer was pressed on the button: the press
	// closes the menu (it is outside it), and the click that follows must not open it again.
	wasOpen bool
	ctl     *ManageController
}

// InitState makes the focus node and the controller of the menu.
func (s *manageState) InitState() {
	s.node = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "Manage"})
	s.menu = kit.NewContextMenuController()
	s.attach()
}

func (s *manageState) attach() {
	if s.ctl != nil && s.ctl.state == s {
		s.ctl.state = nil
	}
	s.ctl = s.Widget().ctl
	if s.ctl != nil {
		s.ctl.state = s
	}
}

// DidUpdateWidget follows a new controller.
func (s *manageState) DidUpdateWidget(*ManageButton) { s.attach() }

// Dispose lets go of the focus node and the controller.
func (s *manageState) Dispose() {
	if s.ctl != nil && s.ctl.state == s {
		s.ctl.state = nil
	}
	s.node.Dispose()
}

// show opens the menu with its bottom on the bottom of the button and its left edge on the right edge.
func (s *manageState) show() {
	if s.menu.IsShowing() || s.wasOpen {
		s.wasOpen = false
		s.menu.Hide()
		return
	}
	r := s.node.Rect()
	s.menu.Show(ui.Offset{Dx: r.Right, Dy: r.Bottom}, s.Widget().items())
	s.SetState(func() { s.open = true })
}

// Build implements widgets.State.
func (s *manageState) Build(widgets.BuildContext) widgets.Widget {
	item := widgets.NewListener(newActivityItem(ManageKey, fonts.CodiconSettingsGear, "Manage", s.open, s.node, s.show), widgets.ListenerOpts{
		Behavior:      rendering.HitTestBehaviorTranslucent,
		OnPointerDown: func(*gestures.PointerDownEvent) { s.wasOpen = s.menu.IsShowing() },
	})
	return kit.NewContextMenu(item, kit.ContextMenuOpts{
		Controller: s.menu,
		OnRun: func(it kit.MenuItem) {
			if f := s.Widget().onRun; f != nil {
				f(it)
			}
		},
		OnHide: func() {
			if s.Mounted() {
				s.SetState(func() { s.open = false })
			}
		},
	})
}
