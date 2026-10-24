package kit

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// ContextMenuController shows and hides the menu of one ContextMenu from outside.
type ContextMenuController struct {
	state *contextMenuState
}

// NewContextMenuController makes a controller; give it to the ContextMenuOpts of one ContextMenu.
func NewContextMenuController() *ContextMenuController { return &ContextMenuController{} }

// Show opens the menu with the items at a position in the window's coordinates, replacing the
// menu that is open, if any (VS Code's `showContextMenu`). The menu is placed by PlaceContextMenu.
// It does nothing for a list without items or when the ContextMenu is not in the tree.
func (c *ContextMenuController) Show(position ui.Offset, items []MenuItem) {
	if c.state != nil {
		c.state.show(position, items)
	}
}

// Hide closes the menu.
func (c *ContextMenuController) Hide() {
	if c.state != nil {
		c.state.hide()
	}
}

// IsShowing says whether the menu is open.
func (c *ContextMenuController) IsShowing() bool {
	return c.state != nil && c.state.ctrl.IsOpen()
}

// ContextMenuOpts are the options of NewContextMenu and NewContextMenuRegion.
type ContextMenuOpts struct {
	// Key is the key of the widget.
	Key foundation.Key
	// Controller, when set, shows and hides the menu from outside.
	Controller *ContextMenuController
	// OnRun is called with the item the user ran, after the menu was hidden and the item's own
	// OnRun was called.
	OnRun func(item MenuItem)
	// OnHide is called when the menu closes, whatever the reason: an item ran, Escape was
	// pressed, the user clicked outside or the window changed size.
	OnHide func()
	// EnableMnemonics shows mnemonics in the menu and runs items by them; VS Code's context menus
	// do not have them.
	EnableMnemonics bool
	// SelectFirst focuses the first item when the menu opens; by default none is focused until the
	// user moves the pointer or presses a key (VS Code's `autoSelectFirstItem`).
	SelectFirst bool
	// Handle, when set, reports the focus of the menu while it is open.
	Handle *MenuHandle
}

// ContextMenu is the host of a context menu: a widget around a child in whose window a menu
// opens at a position that a ContextMenuController gives (VS Code's context menu service). The
// menu sits in the window's root overlay, at the position or, where the window has no room, on
// the other side of it (PlaceContextMenu); it is at most as high as the window less 35 px and
// scrolls beyond that. It takes the keyboard focus while it is open (see Menu for the keyboard)
// and gives it back when it closes. A click anywhere outside the menu, Escape, the window changing
// size and an item running all close it; the click goes on to what is under it.
//
// ContextMenuRegion opens a menu by a right click on a child.
//
// Ported from VS Code: src/vs/platform/contextview/browser/contextMenuHandler.ts and
// src/vs/base/browser/ui/contextview/contextview.ts
type ContextMenu struct {
	widgets.StatefulWidget
	child widgets.Widget
	opts  ContextMenuOpts
}

// NewContextMenu makes the host of a context menu around a child.
func NewContextMenu(child widgets.Widget, opts ...ContextMenuOpts) *ContextMenu {
	m := &ContextMenu{child: child}
	if len(opts) > 0 {
		m.opts = opts[0]
	}
	m.StatefulWidget = widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: m.opts.Key})
	return m
}

// CreateState implements widgets.StatefulWidget.
func (m *ContextMenu) CreateState() widgets.State {
	return &contextMenuState{ctrl: widgets.NewMenuController()}
}

type contextMenuState struct {
	widgets.StateOf[*ContextMenu]
	ctrl  *widgets.MenuController
	at    ui.Offset
	items []MenuItem
	c     *ContextMenuController
}

// InitState attaches the controller.
func (s *contextMenuState) InitState() { s.attach() }

func (s *contextMenuState) attach() {
	if s.c != nil && s.c.state == s {
		s.c.state = nil
	}
	s.c = s.Widget().opts.Controller
	if s.c != nil {
		s.c.state = s
	}
}

// DidUpdateWidget follows a new controller.
func (s *contextMenuState) DidUpdateWidget(*ContextMenu) { s.attach() }

// Dispose lets go of the controller.
func (s *contextMenuState) Dispose() {
	if s.c != nil && s.c.state == s {
		s.c.state = nil
	}
}

func (s *contextMenuState) show(position ui.Offset, items []MenuItem) {
	if !s.Mounted() || len(PrepareMenuItems(items)) == 0 {
		return
	}
	// the position is the pointer's, in the window's coordinates; the menu is placed in those of the overlay, which a
	// zoom of the window makes different
	position = GlobalToOverlay(s.Context(), position)
	s.SetState(func() { s.at, s.items = position, items })
	s.ctrl.Open()
}

func (s *contextMenuState) hide() {
	if s.Mounted() {
		s.ctrl.Close()
	}
}

func (s *contextMenuState) closed() {
	if f := s.Widget().opts.OnHide; f != nil {
		f()
	}
}

// overlay builds the menu in the overlay.
func (s *contextMenuState) overlay(_ widgets.BuildContext, info widgets.RawMenuOverlayInfo) widgets.Widget {
	o := s.Widget().opts
	at := s.at
	menu := NewMenu(s.items, MenuOpts{
		EnableMnemonics: o.EnableMnemonics,
		SelectFirst:     o.SelectFirst,
		Autofocus:       true,
		Handle:          o.Handle,
		MaxHeight:       foundation.Some(max(info.OverlaySize.Height-menuScreenMargin, menuMinMaxHeight)),
		tapGroup:        info.TapRegionGroupID,
		OnCancel:        s.hide,
		OnRun: func(it MenuItem) {
			s.hide()
			if o.OnRun != nil {
				o.OnRun(it)
			}
		},
		sideKey: func(int) {},
	})
	layout := &anchoredLayout{SingleChildLayoutDelegateBase: rendering.NewSingleChildLayoutDelegateBase(nil), place: func(overlay, child ui.Size) ui.Offset {
		return PlaceContextMenu(at, child, overlay)
	}, key: ui.NewRectFromLTWH(at.Dx, at.Dy, 0, 0)}
	return widgets.NewPositionedFill(widgets.NewTapRegion(
		widgets.NewCustomSingleChildLayout(layout, menu),
		widgets.TapRegionOpts{GroupID: info.TapRegionGroupID, OnTapOutside: func(*gestures.PointerDownEvent) { s.hide() }, Behavior: rendering.HitTestBehaviorDeferToChild}))
}

// Build implements widgets.State.
func (s *contextMenuState) Build(widgets.BuildContext) widgets.Widget {
	// The anchor is a point of no size next to the child: the anchor's own tap region would
	// count a click on the child as a click inside the menu.
	anchor := widgets.NewRawMenuAnchor(s.ctrl, s.overlay, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(0.0), Height: foundation.Some(0.0)}),
		widgets.RawMenuAnchorOpts{UseRootOverlay: true, OnClose: s.closed})
	return widgets.NewStack([]widgets.Widget{
		s.Widget().child,
		widgets.NewPositioned(anchor, widgets.PositionedOpts{Left: foundation.Some(0.0), Top: foundation.Some(0.0), Width: foundation.Some(0.0), Height: foundation.Some(0.0)}),
	}, widgets.StackOpts{Fit: rendering.StackFitPassthrough})
}

// ContextMenuRegion opens a context menu at the pointer when the user clicks the secondary
// button on its child: the items are asked for at that moment. A list without items opens
// nothing. The click goes on to the child. The menu key and Shift+F10 pressed while a widget in the
// child has the focus open it at the bottom left corner of that widget.
type ContextMenuRegion struct {
	widgets.StatefulWidget
	child widgets.Widget
	items func() []MenuItem
	opts  ContextMenuOpts
}

// NewContextMenuRegion makes a context menu that a right click on the child opens, with the
// items that the function gives.
func NewContextMenuRegion(child widgets.Widget, items func() []MenuItem, opts ...ContextMenuOpts) *ContextMenuRegion {
	r := &ContextMenuRegion{child: child, items: items}
	if len(opts) > 0 {
		r.opts = opts[0]
	}
	r.StatefulWidget = widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: r.opts.Key})
	return r
}

// CreateState implements widgets.StatefulWidget.
func (r *ContextMenuRegion) CreateState() widgets.State { return &contextMenuRegionState{} }

type contextMenuRegionState struct {
	widgets.StateOf[*ContextMenuRegion]
	ctrl *ContextMenuController
}

// InitState chooses the controller.
func (s *contextMenuRegionState) InitState() { s.pick() }

func (s *contextMenuRegionState) pick() {
	if s.ctrl = s.Widget().opts.Controller; s.ctrl == nil {
		s.ctrl = NewContextMenuController()
	}
}

// DidUpdateWidget follows a new controller.
func (s *contextMenuRegionState) DidUpdateWidget(old *ContextMenuRegion) {
	if old.opts.Controller != s.Widget().opts.Controller {
		s.pick()
	}
}

// handleKey opens the menu for the menu key and Shift+F10 at the bottom left corner of the widget
// that has the focus, as VS Code opens the context menu of the focused element.
func (s *contextMenuRegionState) handleKey(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
	if _, ok := e.(*services.KeyDownEvent); !ok {
		return widgets.KeyEventResultIgnored
	}
	binding := widgets.WidgetsBindingOf(s.Context())
	shiftF10 := e.LogicalKey() == services.LogicalKeyboardKeyF10 && binding.Keyboard().IsShiftPressed()
	if e.LogicalKey() != services.LogicalKeyboardKeyContextMenu && !shiftF10 {
		return widgets.KeyEventResultIgnored
	}
	focus := binding.FocusManager().PrimaryFocus()
	if focus == nil || s.Widget().items == nil {
		return widgets.KeyEventResultIgnored
	}
	r := focus.Rect()
	s.ctrl.Show(ui.Offset{Dx: r.Left, Dy: r.Bottom}, s.Widget().items())
	return widgets.KeyEventResultHandled
}

// Build implements widgets.State.
func (s *contextMenuRegionState) Build(widgets.BuildContext) widgets.Widget {
	r := s.Widget()
	opts := r.opts
	opts.Key, opts.Controller = nil, s.ctrl
	return NewContextMenu(widgets.NewFocus(
		widgets.NewGestureDetector(r.child, widgets.GestureDetectorOpts{
			Behavior: rendering.HitTestBehaviorTranslucent,
			OnSecondaryTapDown: func(d gestures.TapDownDetails) {
				if r.items != nil {
					s.ctrl.Show(d.GlobalPosition, r.items())
				}
			},
		}),
		widgets.FocusOpts{OnKeyEvent: s.handleKey, CanRequestFocus: foundation.Some(false), SkipTraversal: foundation.Some(true), DebugLabel: "ContextMenuRegion"}), opts)
}
