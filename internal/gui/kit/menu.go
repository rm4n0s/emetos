package kit

import (
	"strings"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Metrics and times of VS Code's menus.
//
// Ported from VS Code: src/vs/base/browser/ui/menu/menu.ts (getMenuWidgetCSS) and menubar.css
const (
	menuFontSize        = 13.0
	menuItemHeight      = 24.0
	menuItemMarginH     = 4.0
	menuPaddingV        = 4.0
	menuItemRadius      = 6.0
	menuRadius          = 8.0
	menuMinWidth        = 160.0
	menuLabelPadding    = 26.0 // 2em of the label and of the keybinding
	menuSeparatorHeight = 11.0 // 1 px line with 5 px above and below
	menuSubmenuReserve  = 43.0 // room for the arrow of a submenu item
	menuArrowSize       = 16.0
	menuCheckSize       = 13.0
	menuShadowBlur      = 12.0
	menuScreenMargin    = 35.0 // a menu keeps this far from the bottom of the window
	menuScrollbarSize   = 7.0
	menuMinMaxHeight    = 10.0
	menuDimOpacity      = 0.7
	menuDisabledOpacity = 0.4

	// MenuSubmenuShowDelay is how long the pointer rests on a submenu item before its submenu
	// opens.
	MenuSubmenuShowDelay = 250 * time.Millisecond
	// MenuSubmenuHideDelay is how long a submenu stays open after the pointer moved to another item
	// of its parent.
	MenuSubmenuHideDelay = 750 * time.Millisecond
)

// menuItemExtent is the height of an item's row.
func menuItemExtent(it MenuItem) float64 {
	if it.Separator {
		return menuSeparatorHeight
	}
	return menuItemHeight
}

// MenuHandle gives code outside a Menu a hold on it: it moves the focus into the menu and tells
// which item has it. Give it to the MenuOpts of one menu.
type MenuHandle struct {
	state *menuState
}

// NewMenuHandle makes a handle.
func NewMenuHandle() *MenuHandle { return &MenuHandle{} }

// Focus gives the keyboard focus to the menu; with selectFirst the first item that can take the
// focus is focused (VS Code's `Menu.focus(selectFirst)`).
func (h *MenuHandle) Focus(selectFirst bool) {
	if h.state != nil {
		h.state.focusMenu(selectFirst)
	}
}

// FocusedIndex is the index of the focused item of the menu, in the list that PrepareMenuItems
// gives, or -1.
func (h *MenuHandle) FocusedIndex() int {
	if h.state == nil {
		return -1
	}
	return h.state.model.focused
}

// OpenSubmenu is the index of the item whose submenu is open, or -1.
func (h *MenuHandle) OpenSubmenu() int {
	if h.state == nil {
		return -1
	}
	return h.state.openSub
}

// Path lists the focused item of the menu and then of each submenu that is open inside it, from
// the menu down.
func (h *MenuHandle) Path() []int {
	var out []int
	for s := h.state; s != nil; s = s.child {
		out = append(out, s.model.focused)
	}
	return out
}

// MenuOpts are the options of NewMenu.
type MenuOpts struct {
	// Key is the key of the widget.
	Key foundation.Key
	// OnRun is called with the item the user ran, after the item's own OnRun: a click, Enter or
	// Space on the focused item, or its mnemonic. It is called for the items of submenus as well,
	// never for a disabled item, a separator or a submenu. The menu does not hide itself: the code
	// that shows it does, in this callback.
	OnRun func(item MenuItem)
	// OnCancel is called when Escape is pressed in the menu itself (Escape in a submenu only
	// closes the submenu). VS Code's `onDidCancel`.
	OnCancel func()
	// EnableMnemonics shows the mnemonic of each label underlined and lets the mnemonic key run
	// or focus the item. Without it the marker is left out of the label.
	EnableMnemonics bool
	// Autofocus takes the keyboard focus when the menu is shown, even from a widget that has it.
	Autofocus bool
	// SelectFirst focuses the first item that can take the focus when the menu is shown
	// (VS Code's `menu.focus(true)`).
	SelectFirst bool
	// MaxHeight is the height past which the menu scrolls; the default is no limit. A menu in
	// the window gets VS Code's: the height of the window less 35 px.
	MaxHeight foundation.Opt[float64]
	// Handle, when set, moves the focus from outside and reports it.
	Handle *MenuHandle

	// tapGroup is the group of tap regions the menus of one tree share: a tap in any of them is
	// not a tap outside.
	tapGroup any
	// link is set for the menu of a submenu item: its parent menu.
	link *submenuLink
	// sideKey is called with -1 or 1 for Left and Right pressed in the menu itself when it has no
	// submenu to close or open (the menu bar moves to the previous or next menu).
	sideKey func(dir int)
	// run is the function that runs an item, for the submenus of a menu; it is the menu's own for
	// the first level.
	run func(MenuItem)
}

// submenuLink ties the menu of a submenu to the one that opened it.
type submenuLink struct {
	parent      *menuState
	selectFirst bool
}

// Menu is VS Code's menu: a vertical list of items on a rounded panel. An item is an action with
// a label (its mnemonic underlined when the mnemonics are on), a keybinding label at the right
// edge, an optional check mark, or a separator, or a submenu that opens beside its item. Items
// that are disabled dim and cannot take the focus.
//
// The keyboard: Up and Down move the focus, wrapping around the ends and skipping separators and
// disabled items; Home and PageUp go to the first item and End and PageDown to the last; Enter and
// Space run the focused item (they open its submenu if it has one); Right opens a submenu with the
// first item focused; Left and Escape close a submenu and give the focus back to its item; Escape
// in the menu itself calls OnCancel; with mnemonics on, the mnemonic key runs the item that has it
// (a submenu opens), or, if several share it, focuses the next of them.
//
// The pointer: moving over an item focuses it (the menu reacts to the pointer moving, not to a
// menu that appears under a pointer that rests), a click runs it, and the pointer resting on a
// submenu item opens its submenu after 250 ms. The submenu closes 750 ms after the pointer moved
// to another item of its parent. A click on a disabled item does nothing.
//
// A Menu is the panel only: the ContextMenu and the MenuBar place and show it. Used on its own it
// keeps the submenus it opens in the nearest overlay.
//
// Not ported: the alternative actions of the Alt and Shift keys, the check of the `radio`
// group (VS Code has none), touch gestures, and the frosted glass of the modern UI.
//
// Ported from VS Code: src/vs/base/browser/ui/menu/menu.ts
type Menu struct {
	widgets.StatefulWidget
	items []MenuItem
	opts  MenuOpts
}

// NewMenu makes a menu of items.
func NewMenu(items []MenuItem, opts ...MenuOpts) *Menu {
	m := &Menu{items: items}
	if len(opts) > 0 {
		m.opts = opts[0]
	}
	m.StatefulWidget = widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: m.opts.Key})
	return m
}

// Items are the items of the menu.
func (m *Menu) Items() []MenuItem { return m.items }

// CreateState implements widgets.StatefulWidget.
func (m *Menu) CreateState() widgets.State { return &menuState{openSub: -1, pointerIn: -1} }

type menuState struct {
	widgets.StateOf[*Menu]
	model *menuModel
	node  *widgets.FocusNode
	// ctrls are the controllers of the anchors of the submenu items, by index.
	ctrls map[int]*widgets.MenuController
	// openSub is the item whose submenu is open, or -1; child is that submenu's state.
	openSub int
	child   *menuState
	// pointerIn is the item the pointer last moved over, or -1.
	pointerIn int
	showTimer *gestures.Timer
	hideTimer *gestures.Timer
	scroll    widgets.ScrollController
	bound     *widgets.WidgetsBinding
	handle    *MenuHandle
	// group is the controller of the group that roots the submenus of a menu that is not inside
	// the overlay of a ContextMenu or a MenuBar.
	group *widgets.MenuController
	// pendingFirst is whether the submenu that is about to open focuses its first item.
	pendingFirst bool
}

// InitState prepares the model and, when the menu asks for it, the focus.
func (s *menuState) InitState() {
	w := s.Widget()
	s.bound = widgets.WidgetsBindingOf(s.Context())
	s.model = newMenuModel(prepareItems(w.items))
	s.node = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "Menu"})
	s.ctrls = map[int]*widgets.MenuController{}
	s.scroll = widgets.NewScrollController()
	if w.opts.link != nil {
		w.opts.link.parent.child = s
	}
	s.attach()
	if w.opts.SelectFirst || (w.opts.link != nil && w.opts.link.selectFirst) {
		s.model.First()
	}
	if w.opts.Autofocus || w.opts.link != nil {
		// An autofocus only wins in a scope that has nothing focused, so the focus is requested
		// after the first frame: a menu over an editor must take it.
		scheduler := s.bound.Scheduler()
		scheduler.ScheduleFrame()
		scheduler.AddPostFrameCallback(func(time.Duration) {
			if s.Mounted() {
				s.node.RequestFocus()
			}
		})
	}
}

// prepareItems cleans the list and gives an empty submenu its one disabled item.
func prepareItems(items []MenuItem) []MenuItem {
	out := PrepareMenuItems(items)
	for i, it := range out {
		if it.Submenu && len(it.Items) == 0 {
			out[i].Items = []MenuItem{{Label: "(empty)", Disabled: true}}
		}
	}
	return out
}

func (s *menuState) attach() {
	if s.handle != nil && s.handle.state == s {
		s.handle.state = nil
	}
	s.handle = s.Widget().opts.Handle
	if s.handle != nil {
		s.handle.state = s
	}
}

// DidUpdateWidget follows new items and a new handle.
func (s *menuState) DidUpdateWidget(*Menu) {
	s.attach()
	focused := s.model.focused
	s.model.items = prepareItems(s.Widget().items)
	if !s.model.canFocus(focused) {
		focused = -1
	}
	s.model.focused = focused
	if s.openSub >= len(s.model.items) {
		s.openSub = -1
	}
}

// Dispose stops the timers and releases the focus node.
func (s *menuState) Dispose() {
	s.cancelShow()
	s.cancelHide()
	if s.handle != nil && s.handle.state == s {
		s.handle.state = nil
	}
	if l := s.Widget().opts.link; l != nil && l.parent.child == s {
		l.parent.child = nil
	}
	s.node.Dispose()
	s.scroll.Dispose()
}

func (s *menuState) set(f func()) {
	if s.Mounted() {
		s.SetState(f)
	}
}

func (s *menuState) cancelShow() {
	if s.showTimer != nil {
		s.showTimer.Cancel()
		s.showTimer = nil
	}
}

func (s *menuState) cancelHide() {
	if s.hideTimer != nil {
		s.hideTimer.Cancel()
		s.hideTimer = nil
	}
}

// focusMenu gives the menu the keyboard focus (VS Code's `Menu.focus`).
func (s *menuState) focusMenu(selectFirst bool) {
	if selectFirst {
		s.model.First()
	}
	s.node.RequestFocus()
	s.set(func() {})
	s.ensureVisible()
}

// run runs an item: its own function, then the OnRun of the menu.
func (s *menuState) run(it MenuItem) {
	if f := s.Widget().opts.run; f != nil {
		f(it)
		return
	}
	s.runHere(it)
}

// runHere is the run of the first level.
func (s *menuState) runHere(it MenuItem) {
	if it.OnRun != nil {
		it.OnRun()
	}
	if f := s.Widget().opts.OnRun; f != nil {
		f(it)
	}
	// the menu closes after an item ran
	if s.Mounted() && s.group != nil {
		s.closeSubmenu(false)
	}
}

// ctrlFor gives the controller of the anchor of a submenu item.
func (s *menuState) ctrlFor(i int) *widgets.MenuController {
	c, ok := s.ctrls[i]
	if !ok {
		c = widgets.NewMenuController()
		s.ctrls[i] = c
	}
	return c
}

// openSubmenu opens the submenu of an item.
func (s *menuState) openSubmenu(i int, selectFirst bool) {
	if i < 0 || i >= len(s.model.items) || !s.model.items[i].Submenu {
		return
	}
	s.cancelShow()
	s.cancelHide()
	s.model.focused = i
	if s.openSub == i && s.child != nil {
		// the submenu is open: give it the focus again
		s.child.focusMenu(selectFirst)
		return
	}
	s.pendingFirst = selectFirst
	s.ctrlFor(i).Open()
	s.set(func() {})
}

// closeSubmenu closes the open submenu and, with refocus, takes the keyboard focus back.
func (s *menuState) closeSubmenu(refocus bool) {
	s.cancelHide()
	if s.openSub >= 0 {
		if c, ok := s.ctrls[s.openSub]; ok {
			c.Close()
		}
	}
	if refocus && s.Mounted() {
		s.node.RequestFocus()
	}
}

// activate runs the item at an index, or opens it if it is a submenu.
func (s *menuState) activate(i int) {
	if i < 0 || i >= len(s.model.items) {
		return
	}
	it := s.model.items[i]
	switch {
	case it.Separator:
	case it.Submenu:
		s.openSubmenu(i, true)
	case !it.Disabled:
		s.run(it)
	}
}

// pointerMoved is the pointer moving over an item.
func (s *menuState) pointerMoved(i int) {
	if l := s.Widget().opts.link; l != nil {
		l.parent.cancelHide() // the pointer went into the submenu
	}
	if i == s.pointerIn {
		return
	}
	s.pointerIn = i
	s.cancelShow()
	if s.model.canFocus(i) {
		s.model.focused = i
	} else {
		s.model.focused = -1
	}
	if !s.node.HasPrimaryFocus() {
		s.node.RequestFocus()
	}
	if s.openSub >= 0 && s.openSub != i && s.hideTimer == nil {
		s.hideTimer = s.bound.Gestures().NewTimer(MenuSubmenuHideDelay, func() {
			s.hideTimer = nil
			if s.Mounted() && s.pointerIn != s.openSub {
				s.closeSubmenu(true)
			}
		})
	}
	if s.openSub == i {
		s.cancelHide()
	}
	if i >= 0 && i < len(s.model.items) && s.model.items[i].Submenu && s.openSub != i {
		s.showTimer = s.bound.Gestures().NewTimer(MenuSubmenuShowDelay, func() {
			s.showTimer = nil
			if s.Mounted() && s.pointerIn == i {
				s.openSubmenu(i, false)
			}
		})
	}
	s.set(func() {})
	s.ensureVisible()
}

// pointerLeft is the pointer leaving the menu.
func (s *menuState) pointerLeft() {
	s.pointerIn = -1
	s.cancelShow()
	// the item keeps its highlight while its submenu is open
	if s.openSub < 0 && s.model.focused >= 0 {
		s.model.focused = -1
	}
	s.set(func() {})
}

// submenuOpened and submenuClosed follow the anchors of the submenu items.
func (s *menuState) submenuOpened(i int) {
	s.openSub = i
	s.set(func() {})
}

func (s *menuState) submenuClosed(i int) {
	if s.openSub == i {
		s.openSub = -1
		s.child = nil
	}
	s.set(func() {})
}

// extentBefore is the offset of an item from the top of the list.
func (s *menuState) extentBefore(i int) float64 {
	y := 0.0
	for _, it := range s.model.items[:i] {
		y += menuItemExtent(it)
	}
	return y
}

// ensureVisible scrolls the list so that the focused item is in view.
func (s *menuState) ensureVisible() {
	i := s.model.focused
	if i < 0 || i >= len(s.model.items) {
		return
	}
	scheduler := s.bound.Scheduler()
	scheduler.ScheduleFrame()
	scheduler.AddPostFrameCallback(func(time.Duration) {
		if !s.Mounted() || !s.scroll.HasClients() || i >= len(s.model.items) {
			return
		}
		p := s.scroll.Positions()[0]
		if !p.HasPixels() || !p.HasViewportDimension() {
			return
		}
		top := menuPaddingV + s.extentBefore(i)
		bottom := top + menuItemExtent(s.model.items[i])
		pos, view := p.Pixels(), p.ViewportDimension()
		switch {
		case top < pos:
			pos = top - menuPaddingV
		case bottom > pos+view:
			pos = bottom + menuPaddingV - view
		default:
			return
		}
		s.scroll.JumpTo(min(max(pos, p.MinScrollExtent()), p.MaxScrollExtent()))
	})
}

// mnemonicOf is the lower case key of a key event for the mnemonics: its character or, without
// one, its label.
func mnemonicOf(e services.KeyEvent) string {
	if ch := e.Character(); ch != "" {
		return strings.ToLower(ch)
	}
	if label := e.LogicalKey().KeyLabel(); len([]rune(label)) == 1 {
		return strings.ToLower(label)
	}
	return ""
}

// handleKey is the keyboard of the menu.
func (s *menuState) handleKey(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
	switch e.(type) {
	case *services.KeyDownEvent, *services.KeyRepeatEvent:
	default:
		return widgets.KeyEventResultIgnored
	}
	w := s.Widget()
	m := s.model
	changed := false
	switch k := e.LogicalKey(); k {
	case services.LogicalKeyboardKeyArrowDown:
		changed = m.Move(1)
	case services.LogicalKeyboardKeyArrowUp:
		changed = m.Move(-1)
	case services.LogicalKeyboardKeyHome, services.LogicalKeyboardKeyPageUp:
		changed = m.First()
	case services.LogicalKeyboardKeyEnd, services.LogicalKeyboardKeyPageDown:
		changed = m.Last()
	case services.LogicalKeyboardKeyEnter, services.LogicalKeyboardKeyNumpadEnter, services.LogicalKeyboardKeySpace:
		if _, repeat := e.(*services.KeyRepeatEvent); !repeat {
			s.activate(m.focused)
		}
		return widgets.KeyEventResultHandled
	case services.LogicalKeyboardKeyArrowRight:
		if i := m.focused; i >= 0 && m.items[i].Submenu {
			s.openSubmenu(i, true)
		} else if w.opts.link == nil && w.opts.sideKey != nil {
			w.opts.sideKey(1)
		}
		return widgets.KeyEventResultHandled
	case services.LogicalKeyboardKeyArrowLeft:
		if w.opts.link != nil {
			w.opts.link.parent.closeSubmenu(true)
		} else if w.opts.sideKey != nil {
			w.opts.sideKey(-1)
		}
		return widgets.KeyEventResultHandled
	case services.LogicalKeyboardKeyEscape:
		switch {
		case w.opts.link != nil:
			w.opts.link.parent.closeSubmenu(true)
		case w.opts.OnCancel != nil:
			w.opts.OnCancel()
		default:
			return widgets.KeyEventResultIgnored
		}
		return widgets.KeyEventResultHandled
	case services.LogicalKeyboardKeyTab:
		return widgets.KeyEventResultHandled // menus stop tab navigation
	default:
		kb := s.bound.Keyboard()
		key := mnemonicOf(e)
		if !w.opts.EnableMnemonics || key == "" || kb.IsControlPressed() || kb.IsMetaPressed() {
			return widgets.KeyEventResultIgnored
		}
		i, unique := m.Mnemonic(key)
		if i < 0 {
			return widgets.KeyEventResultIgnored
		}
		m.focused = i
		if unique {
			s.activate(i)
		}
		changed = true
	}
	s.cancelHide()
	if changed {
		s.set(func() {})
		s.ensureVisible()
	}
	return widgets.KeyEventResultHandled
}

// Build implements widgets.State.
func (s *menuState) Build(bc widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	colors := ThemeOf(bc)
	rows := make([]widgets.Widget, len(s.model.items))
	for i := range s.model.items {
		rows[i] = s.row(bc, colors, i)
	}
	column := widgets.NewColumn(rows, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentStretch})
	var list widgets.Widget = widgets.NewPadding(painting.NewEdgeInsetsSymmetric(menuPaddingV, 0), widgets.NewIntrinsicWidth(column))

	borderWidth := 0.0
	if colors.MenuBorder.Alpha() > 0 {
		borderWidth = 1
	}
	if maxH, ok := w.opts.MaxHeight.Get(); ok {
		inner := max(maxH, menuMinMaxHeight) - 2*borderWidth
		if s.contentHeight() > inner {
			// the element fills the room it gets, so the width is asked of the content
			list = widgets.NewIntrinsicWidth(widgets.NewSizedBox(NewScrollableElement(
				widgets.NewSingleChildScrollView(list, widgets.SingleChildScrollViewOpts{Controller: s.scroll, Primary: foundation.Some(false)}),
				ScrollableElementOpts{Controller: s.scroll, VerticalSize: foundation.Some(menuScrollbarSize), AlwaysConsumeMouseWheel: true}),
				widgets.SizedBoxOpts{Height: foundation.Some(inner)}))
		}
	}
	panel := widgets.NewContainer(list, widgets.ContainerOpts{
		Constraints: foundation.Some(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MinWidth: foundation.Some(menuMinWidth)})),
		Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color:        foundation.Some(colors.MenuBackground),
			Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.MenuBorder), Width: foundation.Some(borderWidth)}),
			BorderRadius: painting.NewBorderRadiusCircular(menuRadius),
			BoxShadow:    []painting.BoxShadow{painting.NewBoxShadow(painting.BoxShadowOpts{Color: foundation.Some(colors.WidgetShadow), BlurRadius: menuShadowBlur})},
		}),
	})
	var out widgets.Widget = widgets.NewFocus(
		widgets.NewMouseRegion(panel, widgets.MouseRegionOpts{OnExit: func(*gestures.PointerExitEvent) { s.pointerLeft() }, Cursor: services.SystemMouseCursors.Basic}),
		widgets.FocusOpts{FocusNode: s.node, OnKeyEvent: s.handleKey, DebugLabel: "Menu"})
	group := w.opts.tapGroup
	if w.opts.link == nil && w.opts.tapGroup == nil {
		// Not inside the overlay of a context menu or a menu bar: this menu roots its submenus.
		if s.group == nil {
			s.group = widgets.NewMenuController()
		}
		group = s.group
		out = widgets.NewRawMenuAnchorGroup(s.group, out)
	}
	if group != nil {
		out = widgets.NewTapRegion(out, widgets.TapRegionOpts{GroupID: group})
	}
	return out
}

// contentHeight is the height of the list with its padding.
func (s *menuState) contentHeight() float64 {
	return s.extentBefore(len(s.model.items)) + 2*menuPaddingV
}

// row builds the row of an item.
func (s *menuState) row(bc widgets.BuildContext, colors Colors, i int) widgets.Widget {
	it := s.model.items[i]
	if it.Separator {
		return widgets.NewContainer(
			widgets.NewPadding(painting.NewEdgeInsetsSymmetric(5, 0),
				widgets.NewContainer(nil, widgets.ContainerOpts{Height: foundation.Some(1.0), Color: foundation.Some(colors.MenuSeparatorBackground)})),
			widgets.ContainerOpts{Height: foundation.Some(menuSeparatorHeight)})
	}
	w := s.Widget()
	selected := i == s.model.focused
	fg := colors.MenuForeground
	var bg ui.Color
	if selected {
		fg, bg = colors.MenuSelectionForeground, colors.MenuSelectionBackground
	}
	labelColor := fg
	if it.Disabled && !it.Submenu {
		labelColor = colors.DisabledForeground
	}
	label := menuLabelWidget(bc, it.Label, w.opts.EnableMnemonics, labelColor)
	parts := []widgets.Widget{widgets.NewPadding(painting.NewEdgeInsetsSymmetric(0, menuLabelPadding), label), widgets.NewExpanded(widgets.NewSizedBox(nil))}
	var overlays []widgets.Widget
	switch {
	case it.Submenu:
		parts = append(parts, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(menuSubmenuReserve)}))
		overlays = append(overlays, widgets.NewPositioned(
			widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconChevronRight), widgets.IconOpts{Size: foundation.Some(menuArrowSize), Color: foundation.Some(fg.WithOpacity(menuDimOpacity))}),
			widgets.PositionedOpts{Right: foundation.Some(3.0), Top: foundation.Some(0.0), Bottom: foundation.Some(0.0)}))
	case it.Keybinding != "":
		opacity := menuDimOpacity
		if selected {
			opacity = 1
		}
		if it.Disabled {
			opacity = menuDisabledOpacity
		}
		kb := widgets.NewText(it.Keybinding, widgets.TextOpts{Style: TextStyleOf(bc, menuFontSize, fg.WithOpacity(opacity)), MaxLines: foundation.Some(1)})
		parts = append(parts, widgets.NewPadding(painting.NewEdgeInsetsSymmetric(0, menuLabelPadding), kb))
	}
	if it.Checked {
		overlays = append(overlays, widgets.NewPositioned(
			widgets.NewCenter(widgets.NewIcon(fonts.CodiconIcon(fonts.CodiconCheck), widgets.IconOpts{Size: foundation.Some(menuCheckSize), Color: foundation.Some(fg)})),
			widgets.PositionedOpts{Left: foundation.Some(0.0), Top: foundation.Some(0.0), Bottom: foundation.Some(0.0), Width: foundation.Some(menuLabelPadding)}))
	}
	content := widgets.NewStack(append([]widgets.Widget{widgets.NewRow(parts, widgets.RowOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})}, overlays...),
		widgets.StackOpts{Fit: rendering.StackFitPassthrough})
	opts := widgets.ContainerOpts{
		Height: foundation.Some(menuItemHeight),
		Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color: foundation.Some(bg), BorderRadius: painting.NewBorderRadiusCircular(menuItemRadius),
		}),
	}
	if selected && colors.MenuSelectionBorder.Alpha() > 0 {
		opts.ForegroundDecoration = painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.MenuSelectionBorder), Width: foundation.Some(1.0)}),
			BorderRadius: painting.NewBorderRadiusCircular(menuItemRadius),
		})
	}
	var body widgets.Widget = widgets.NewMouseRegion(
		widgets.NewGestureDetector(
			widgets.NewPadding(painting.NewEdgeInsetsSymmetric(0, menuItemMarginH), widgets.NewContainer(content, opts)),
			widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: func() { s.activate(i) }}),
		widgets.MouseRegionOpts{
			Cursor:  services.SystemMouseCursors.Basic,
			OnHover: func(e *gestures.PointerHoverEvent) { s.hovered(i, e) },
		})
	if it.Submenu {
		body = s.submenuAnchor(i, it, body)
	}
	return body
}

// hovered reacts to the pointer moving over an item, and not to one that rests.
func (s *menuState) hovered(i int, e *gestures.PointerHoverEvent) {
	if e.Delta() == (ui.Offset{}) {
		return
	}
	s.pointerMoved(i)
}

// submenuAnchor wraps the row of a submenu item in the anchor that shows its submenu.
func (s *menuState) submenuAnchor(i int, it MenuItem, row widgets.Widget) widgets.Widget {
	w := s.Widget()
	return widgets.NewRawMenuAnchor(s.ctrlFor(i), func(bc widgets.BuildContext, info widgets.RawMenuOverlayInfo) widgets.Widget {
		entry := info.AnchorRect
		// the item includes the padding of the parent menu above and below
		entry.Top -= menuPaddingV
		entry.Bottom += menuPaddingV
		sub := NewMenu(it.Items, MenuOpts{
			EnableMnemonics: w.opts.EnableMnemonics,
			MaxHeight:       foundation.Some(max(info.OverlaySize.Height-menuScreenMargin, menuMinMaxHeight)),
			tapGroup:        info.TapRegionGroupID,
			link:            &submenuLink{parent: s, selectFirst: s.pendingFirst},
			run:             s.rootRun(),
		})
		layout := &anchoredLayout{SingleChildLayoutDelegateBase: rendering.NewSingleChildLayoutDelegateBase(nil), place: func(size, child ui.Size) ui.Offset {
			return PlaceSubmenu(entry, child, size)
		}, key: entry}
		return widgets.NewPositionedFill(widgets.NewCustomSingleChildLayout(layout, sub))
	}, row, widgets.RawMenuAnchorOpts{
		OnOpen:  func() { s.submenuOpened(i) },
		OnClose: func() { s.submenuClosed(i) },
	})
}

// rootRun is the function that runs an item, for a submenu: the first level's.
func (s *menuState) rootRun() func(MenuItem) {
	if f := s.Widget().opts.run; f != nil {
		return f
	}
	return s.runHere
}

// anchoredLayout places a menu in an overlay with a function of the overlay's size and its own.
type anchoredLayout struct {
	rendering.SingleChildLayoutDelegateBase
	place func(overlay, child ui.Size) ui.Offset
	// key is what the placement depends on, to relayout when it changes.
	key ui.Rect
}

func (l *anchoredLayout) GetConstraintsForChild(c rendering.BoxConstraints) rendering.BoxConstraints {
	return rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(c.MaxWidth), MaxHeight: foundation.Some(c.MaxHeight)})
}

func (l *anchoredLayout) GetPositionForChild(size, childSize ui.Size) ui.Offset {
	return l.place(size, childSize)
}

func (l *anchoredLayout) ShouldRelayout(old rendering.SingleChildLayoutDelegate) bool {
	return l.key != old.(*anchoredLayout).key
}

// menuLabelWidget shows a label with its mnemonic underlined when the mnemonics are on.
func menuLabelWidget(bc widgets.BuildContext, label string, mnemonics bool, color ui.Color) widgets.Widget {
	return labelWidget(bc, label, mnemonics, mnemonics, menuFontSize, color)
}
