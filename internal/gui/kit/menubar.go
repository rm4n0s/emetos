package kit

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// Metrics of VS Code's menu bar: the bar is as high as the title bar (30 px), its titles are 22 px
// high with 8 px of padding at each side and a radius of 5.
//
// Ported from VS Code: src/vs/base/browser/ui/menu/menubar.css and
// src/vs/workbench/browser/parts/titlebar/media/titlebarpart.css
const (
	menuBarHeight      = 30.0
	menuBarTitleHeight = 22.0
	menuBarTitlePadH   = 8.0
	menuBarTitleRadius = 5.0
	menuBarFontSize    = 13.0
)

// MenuBarVisibility is the `window.menuBarVisibility` setting: when the menu bar is shown.
type MenuBarVisibility int

// The values of the setting that the menu bar supports (not `compact`, the menu in the side bar).
const (
	// MenuBarClassic shows the menu bar, except in full screen mode.
	MenuBarClassic MenuBarVisibility = iota
	// MenuBarVisible always shows the menu bar, in full screen mode too.
	MenuBarVisible
	// MenuBarToggle hides the menu bar until the Alt key is pressed and released.
	MenuBarToggle
	// MenuBarHidden always hides the menu bar (its menus still open with the mnemonics).
	MenuBarHidden
)

// ParseMenuBarVisibility reads the value of the setting: "classic", "visible", "toggle" or
// "hidden".
func ParseMenuBarVisibility(s string) (MenuBarVisibility, bool) {
	switch s {
	case "classic":
		return MenuBarClassic, true
	case "visible":
		return MenuBarVisible, true
	case "toggle":
		return MenuBarToggle, true
	case "hidden":
		return MenuBarHidden, true
	}
	return MenuBarClassic, false
}

// String is the value of the setting.
func (v MenuBarVisibility) String() string {
	switch v {
	case MenuBarVisible:
		return "visible"
	case MenuBarToggle:
		return "toggle"
	case MenuBarHidden:
		return "hidden"
	}
	return "classic"
}

// barPhase is how much the menu bar is in use (VS Code's `MenubarState`); each phase includes the
// ones before it.
type barPhase int

const (
	barHidden barPhase = iota
	barVisible
	barFocused
	barOpen
)

// menuBarModel is the state machine of the menu bar, without widgets: which phase it is in, which
// menu is focused or open, whether the mnemonics are in use and how the Alt key is understood.
//
// Ported from VS Code: src/vs/base/browser/ui/menu/menubar.ts (MenuBar)
type menuBarModel struct {
	visibility MenuBarVisibility
	fullScreen bool
	phase      barPhase
	// n is the number of menus.
	n int
	// focused is the menu that has the focus (or is open), or -1.
	focused int
	// openedViaKeyboard says that the menu that opens selects its first item.
	openedViaKeyboard bool
	// mnemonicsInUse says that the mnemonics are shown and work: the bar got the focus through
	// Alt, or a menu opened through a mnemonic.
	mnemonicsInUse bool
	// awaitingAltRelease is set when Alt was pressed to give the focus up: its release does not
	// give it back.
	awaitingAltRelease bool
	enableMnemonics    bool
	alwaysOnMnemonics  bool
	disableAltFocus    bool
	mnemonics          map[string]int
}

func newMenuBarModel() *menuBarModel {
	m := &menuBarModel{focused: -1, mnemonics: map[string]int{}}
	m.setUnfocused()
	return m
}

func (m *menuBarModel) isVisible() bool { return m.phase >= barVisible }
func (m *menuBarModel) isFocused() bool { return m.phase >= barFocused }
func (m *menuBarModel) isOpen() bool    { return m.phase >= barOpen }

// openIndex is the menu that is open, or -1.
func (m *menuBarModel) openIndex() int {
	if m.isOpen() {
		return m.focused
	}
	return -1
}

// setUnfocused gives the bar its resting phase: hidden for the visibility `toggle` and `hidden`
// and for `classic` in full screen, else visible.
func (m *menuBarModel) setUnfocused() {
	switch {
	case m.visibility == MenuBarToggle || m.visibility == MenuBarHidden:
		m.phase = barHidden
	case m.visibility == MenuBarClassic && m.fullScreen:
		m.phase = barHidden
	default:
		m.phase = barVisible
	}
	m.focused = -1
	m.mnemonicsInUse = false
}

// setMnemonics registers the mnemonic keys of the menu labels.
func (m *menuBarModel) setMnemonics(labels []string) {
	m.n = len(labels)
	m.mnemonics = map[string]int{}
	for i, l := range labels {
		if key := MnemonicKey(l); key != "" {
			m.mnemonics[key] = i
		}
	}
}

// toggleFocus focuses the first menu, or gives the focus up (`toggleFocus`).
func (m *menuBarModel) toggleFocus() {
	switch {
	case !m.isFocused() && m.visibility != MenuBarHidden && m.n > 0:
		m.mnemonicsInUse = true
		m.focused = 0
		m.phase = barFocused
	case !m.isOpen():
		m.setUnfocused()
	}
}

// trigger is a click or a mnemonic on a menu (`onMenuTriggered`): a closed menu opens, the
// open one closes with everything else, and another menu replaces the open one.
func (m *menuBarModel) trigger(index int, clicked bool) {
	switch {
	case m.isOpen() && m.focused == index:
		m.setUnfocused()
	case m.isOpen():
		m.focused = index
	default:
		m.focused = index
		m.openedViaKeyboard = !clicked
		m.phase = barOpen
	}
}

// switchTo replaces the open menu with another, selecting its first item or not.
func (m *menuBarModel) switchTo(index int, selectFirst bool) {
	m.focused = index
	m.openedViaKeyboard = selectFirst
	m.phase = barOpen
}

// hoverEnter is the pointer entering a menu title: with a menu open it opens that one, else with
// the bar focused it moves the focus there.
func (m *menuBarModel) hoverEnter(index int) {
	switch {
	case m.isOpen() && m.focused != index:
		m.switchTo(index, false)
	case m.isFocused() && !m.isOpen():
		m.focused = index
	}
}

// move moves to the previous or next menu (wrapping), opening it when a menu is open.
func (m *menuBarModel) move(dir int) {
	if m.focused < 0 || m.n == 0 {
		return
	}
	next := (m.focused + dir + m.n) % m.n
	if next == m.focused {
		return
	}
	switch {
	case m.isOpen():
		m.switchTo(next, true)
	case m.isFocused():
		m.focused = next
	}
}

// openFocused opens the focused menu with the keyboard (Down or Enter on its title).
func (m *menuBarModel) openFocused() {
	if m.focused >= 0 && !m.isOpen() {
		m.openedViaKeyboard = true
		m.phase = barOpen
	}
}

// cancelMenu is Escape in an open menu: the menu closes and its title keeps the focus.
func (m *menuBarModel) cancelMenu() {
	if m.isOpen() {
		m.phase = barFocused
	}
}

// shouldAltFocus says whether a tap of Alt moves the focus to the bar.
func (m *menuBarModel) shouldAltFocus() bool {
	return !m.disableAltFocus || m.visibility == MenuBarToggle
}

// altDown is Alt pressed, and altTap is a clean press and release of Alt on its own.
func (m *menuBarModel) altDown() {
	if m.visibility == MenuBarHidden {
		return
	}
	// Alt pressed while the bar is focused gives the focus back, and its release does not
	// take it again
	if m.isFocused() {
		m.setUnfocused()
		m.awaitingAltRelease = true
	}
}

func (m *menuBarModel) altTap() {
	if m.visibility == MenuBarHidden || m.awaitingAltRelease {
		return
	}
	switch {
	case !m.isFocused() && m.shouldAltFocus() && m.n > 0:
		m.mnemonicsInUse = true
		m.focused = 0
		m.phase = barFocused
	case !m.isOpen():
		m.setUnfocused()
	}
}

// altLetter is Alt with a letter: it opens the menu that has the letter as its mnemonic.
func (m *menuBarModel) altLetter(key string) bool {
	if !m.enableMnemonics {
		return false
	}
	i, ok := m.mnemonics[key]
	if !ok {
		return false
	}
	m.mnemonicsInUse = true
	m.trigger(i, false)
	return true
}

// MenuBarMenu is a menu of the menu bar: its label (with the mnemonic marked by an ampersand,
// "&File") and its items.
type MenuBarMenu struct {
	Label string
	Items []MenuItem
}

// MenuBarController moves the focus of a MenuBar from outside (the command that focuses the
// application menu) and tells what state it is in.
type MenuBarController struct {
	state *menuBarState
}

// NewMenuBarController makes a controller; give it to the MenuBarOpts of one bar.
func NewMenuBarController() *MenuBarController { return &MenuBarController{} }

// ToggleFocus gives the focus to the first menu and shows the bar and its mnemonics, or, when the
// bar has the focus, gives it up (VS Code's `toggleFocus`). It does nothing for the visibility
// hidden.
func (c *MenuBarController) ToggleFocus() {
	if c.state != nil {
		c.state.m.toggleFocus()
		c.state.sync()
	}
}

// Blur gives the focus up and closes the menu that is open.
func (c *MenuBarController) Blur() {
	if c.state != nil {
		c.state.m.setUnfocused()
		c.state.sync()
	}
}

// IsVisible says whether the bar is shown.
func (c *MenuBarController) IsVisible() bool { return c.state != nil && c.state.m.isVisible() }

// IsFocused says whether the bar has the focus (a menu may be open).
func (c *MenuBarController) IsFocused() bool { return c.state != nil && c.state.m.isFocused() }

// IsOpen says whether a menu is open.
func (c *MenuBarController) IsOpen() bool { return c.state != nil && c.state.m.isOpen() }

// FocusedMenu is the index of the menu that has the focus or is open, or -1.
func (c *MenuBarController) FocusedMenu() int {
	if c.state == nil {
		return -1
	}
	return c.state.m.focused
}

// MnemonicsShown says whether the mnemonics of the titles are underlined.
func (c *MenuBarController) MnemonicsShown() bool { return c.state != nil && c.state.mnemonicsShown() }

// MenuBarOpts are the options of NewMenuBar.
type MenuBarOpts struct {
	// Key is the key of the widget.
	Key foundation.Key
	// Visibility is when the bar is shown (`window.menuBarVisibility`); the default is classic.
	Visibility MenuBarVisibility
	// FullScreen says that the window is in full screen mode, where the visibility classic hides
	// the bar.
	FullScreen bool
	// OnRun is called with the item of a menu that the user ran.
	OnRun func(item MenuItem)
	// DisableMnemonics turns off the Alt-key shortcuts of the menus and the underlines
	// (`window.enableMenuBarMnemonics` off).
	DisableMnemonics bool
	// AlwaysOnMnemonics underlines the mnemonics all the time.
	AlwaysOnMnemonics bool
	// DisableAltFocus stops a tap of Alt from focusing the bar (`window.customMenuBarAltFocus`
	// off); it still toggles the bar with the visibility toggle.
	DisableAltFocus bool
	// Controller, when set, moves the focus from outside and reports the state.
	Controller *MenuBarController
	// OnVisibilityChange is called with true when the bar becomes visible and false when it hides.
	OnVisibilityChange func(visible bool)
	// OnFocusChange is called when the bar gets or loses the focus (a menu that is open counts
	// as focus).
	OnFocusChange func(focused bool)
	// Height is the height of the bar; the default is the 30 px of the title bar.
	Height foundation.Opt[float64]
}

// MenuBar is VS Code's menu bar: a row of menu titles (File, Edit, ...) that open their menus.
//
// The pointer: a click on a title opens its menu (on the press, as VS Code does) and a click on the
// title of the open menu closes it; with a menu open, moving the pointer to another title opens
// that menu; with the bar focused but no menu open, the title under the pointer takes the focus.
// A click outside the bar and its menus closes the menu and gives the focus up.
//
// The keyboard: pressing and releasing Alt on its own focuses the first title and shows the
// mnemonics (the titles' mnemonic letters underlined); with the bar focused, Left and Right move
// between the titles, Down and Enter open the focused menu with its first item focused, a mnemonic
// letter opens its menu, Escape gives the focus up, and so does another tap of Alt. With a menu
// open, Left and Right (not inside a submenu) switch to the previous or next menu, wrapping, and
// Escape closes the menu and leaves its title focused. Alt with a mnemonic letter, from anywhere,
// opens that menu. Alt held down underlines the mnemonics; pressing another key or the pointer
// between the press and the release of Alt makes it no tap. The focus goes back to where it was
// when the bar gives it up.
//
// The visibility (`window.menuBarVisibility`): classic and visible show the bar all the time (classic
// hides it in full screen), toggle hides it until the Alt key focuses it and hides it again when it
// gives the focus up, and hidden never shows it for the Alt key (a mnemonic with Alt still opens
// its menu, as in VS Code).
//
// Colors: titles in `titleBar.activeForeground`; the title that is open, focused or under the
// pointer (while the bar is not focused) in `menubar.selectionForeground` over
// `menubar.selectionBackground`, with a `menubar.selectionBorder` outline when the theme has one.
// The menus are Menu widgets that open under their title.
//
// Not ported: the compact mode and the overflow menu for a bar that is too narrow, the alternative
// actions of Alt, the tab navigation of macOS, and the dashed border for a title under the pointer
// (it is solid).
//
// Ported from VS Code: src/vs/base/browser/ui/menu/menubar.ts and menubar.css, and
// src/vs/workbench/browser/parts/titlebar/media/menubarControl.css
type MenuBar struct {
	widgets.StatefulWidget
	menus []MenuBarMenu
	opts  MenuBarOpts
}

// NewMenuBar makes a menu bar of menus.
func NewMenuBar(menus []MenuBarMenu, opts ...MenuBarOpts) *MenuBar {
	b := &MenuBar{menus: menus}
	if len(opts) > 0 {
		b.opts = opts[0]
	}
	b.StatefulWidget = widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: b.opts.Key})
	return b
}

// Menus are the menus of the bar.
func (b *MenuBar) Menus() []MenuBarMenu { return b.menus }

// CreateState implements widgets.StatefulWidget.
func (b *MenuBar) CreateState() widgets.State {
	return &menuBarState{m: newMenuBarModel(), hover: -1, shown: -1}
}

type menuBarState struct {
	widgets.StateOf[*MenuBar]
	m     *menuBarModel
	node  *widgets.FocusNode
	group *widgets.MenuController
	ctrls []*widgets.MenuController
	// hover is the title under the pointer, or -1; shown is the menu the anchors have open.
	hover, shown int
	// altHeld is true between the press and the release of Alt; altCandidate while no other key
	// or pointer press has come in between.
	altHeld, altCandidate bool
	// ignoreNextUp is set when the press opened a menu: the release must not close it.
	ignoreNextUp bool
	keyHandle    services.KeyHandlerHandle
	routeHandle  gestures.PointerRouteHandle
	bound        *widgets.WidgetsBinding
	ctl          *MenuBarController
	// wasVisible and wasFocused remember the last state reported to the callbacks.
	wasVisible, wasFocused bool
	// returnFocus is the node that had the focus when the bar took it.
	returnFocus *widgets.FocusNode
}

// InitState starts watching the keyboard and the pointer.
func (s *menuBarState) InitState() {
	s.bound = widgets.WidgetsBindingOf(s.Context())
	s.node = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "MenuBar"})
	s.group = widgets.NewMenuController()
	s.configure()
	s.m.setUnfocused()
	s.wasVisible, s.wasFocused = s.m.isVisible(), s.m.isFocused()
	s.attach()
	s.keyHandle = s.bound.Keyboard().AddHandler(s.handleGlobalKey)
	s.routeHandle = s.bound.Gestures().PointerRouter.AddGlobalRoute(func(e gestures.PointerEvent) {
		if _, ok := e.(*gestures.PointerDownEvent); ok {
			s.altCandidate = false
		}
	}, nil)
}

func (s *menuBarState) attach() {
	if s.ctl != nil && s.ctl.state == s {
		s.ctl.state = nil
	}
	s.ctl = s.Widget().opts.Controller
	if s.ctl != nil {
		s.ctl.state = s
	}
}

// configure takes the options and the labels into the model.
func (s *menuBarState) configure() {
	w := s.Widget()
	m := s.m
	m.visibility, m.fullScreen = w.opts.Visibility, w.opts.FullScreen
	m.enableMnemonics, m.alwaysOnMnemonics, m.disableAltFocus = !w.opts.DisableMnemonics, w.opts.AlwaysOnMnemonics, w.opts.DisableAltFocus
	labels := make([]string, len(w.menus))
	for i, menu := range w.menus {
		labels[i] = menu.Label
	}
	m.setMnemonics(labels)
	for len(s.ctrls) < len(labels) {
		s.ctrls = append(s.ctrls, widgets.NewMenuController())
	}
	s.ctrls = s.ctrls[:len(labels)]
	if m.focused >= len(labels) {
		m.focused = -1
	}
}

// DidUpdateWidget follows new options, menus and controller.
func (s *menuBarState) DidUpdateWidget(old *MenuBar) {
	s.attach()
	s.configure()
	if !s.m.isFocused() && (old.opts.Visibility != s.Widget().opts.Visibility || old.opts.FullScreen != s.Widget().opts.FullScreen) {
		s.m.setUnfocused()
	}
	s.notify()
}

// Dispose stops watching and releases the focus node.
func (s *menuBarState) Dispose() {
	s.bound.Keyboard().RemoveHandler(s.keyHandle)
	s.bound.Gestures().PointerRouter.RemoveGlobalRoute(s.routeHandle)
	if s.ctl != nil && s.ctl.state == s {
		s.ctl.state = nil
	}
	s.node.Dispose()
}

// mnemonicsShown says whether the titles show their mnemonics underlined: always when asked to,
// while they are in use, and while Alt is held down (unless Alt gave the focus up).
func (s *menuBarState) mnemonicsShown() bool {
	m := s.m
	if !m.enableMnemonics {
		return false
	}
	return m.alwaysOnMnemonics || m.mnemonicsInUse || (!m.isOpen() && s.altHeld && !m.awaitingAltRelease)
}

// sync makes the widgets follow the model: the menu that is open, the focus, the callbacks.
func (s *menuBarState) sync() {
	if !s.Mounted() {
		return
	}
	m := s.m
	want := m.openIndex()
	if want != s.shown {
		old := s.shown
		s.shown = want
		if old >= 0 && old < len(s.ctrls) {
			s.ctrls[old].Close()
		}
		if want >= 0 && want < len(s.ctrls) {
			s.ctrls[want].Open()
		}
	}
	if m.isFocused() && !s.node.HasFocus() {
		if prim := s.bound.FocusManager().PrimaryFocus(); prim != nil {
			s.returnFocus = prim
		}
	}
	switch {
	case m.phase == barFocused:
		if !s.node.HasPrimaryFocus() {
			s.node.RequestFocus()
		}
	case m.phase < barFocused:
		if s.node.HasFocus() {
			s.node.Unfocus()
			if rf := s.returnFocus; rf != nil && rf.Context() != nil {
				rf.RequestFocus()
			}
		}
		s.returnFocus = nil
	}
	s.notify()
	s.SetState(func() {})
}

// notify tells the callbacks what changed.
func (s *menuBarState) notify() {
	o := s.Widget().opts
	if v := s.m.isVisible(); v != s.wasVisible {
		s.wasVisible = v
		if o.OnVisibilityChange != nil {
			o.OnVisibilityChange(v)
		}
	}
	if f := s.m.isFocused(); f != s.wasFocused {
		s.wasFocused = f
		if o.OnFocusChange != nil {
			o.OnFocusChange(f)
		}
	}
}

// menuClosed follows an anchor that closed: a menu that closes on its own (a click outside, the
// window changing size) gives the focus up.
func (s *menuBarState) menuClosed(i int) {
	if s.shown == i {
		s.shown = -1
		s.m.setUnfocused()
		s.sync()
	}
}

// isAlt says whether a key is one of the Alt keys.
func isAltKey(k services.LogicalKeyboardKey) bool {
	return k == services.LogicalKeyboardKeyAltLeft || k == services.LogicalKeyboardKeyAltRight || k == services.LogicalKeyboardKeyAlt
}

// handleGlobalKey sees every key of the window: it understands a tap of Alt and Alt with a
// mnemonic letter.
func (s *menuBarState) handleGlobalKey(e services.KeyEvent) bool {
	k := e.LogicalKey()
	kb := s.bound.Keyboard()
	switch e.(type) {
	case *services.KeyDownEvent:
		if isAltKey(k) {
			s.altHeld, s.altCandidate = true, true
			s.m.altDown()
			s.sync()
			return false
		}
		s.altCandidate = false
		if !kb.IsAltPressed() || kb.IsControlPressed() {
			return false
		}
		key := mnemonicOf(e)
		if key == "" {
			return false
		}
		if !s.m.altLetter(key) {
			return false
		}
		s.sync()
		return true
	case *services.KeyUpEvent:
		if !isAltKey(k) {
			return false
		}
		tap := s.altCandidate && !kb.IsControlPressed() && !kb.IsShiftPressed() && !kb.IsMetaPressed() && !kb.IsAltPressed()
		s.altHeld, s.altCandidate = false, false
		if tap {
			s.m.altTap()
		}
		s.m.awaitingAltRelease = false
		s.sync()
	}
	return false
}

// handleKey is the keyboard of the bar when one of its titles has the focus.
func (s *menuBarState) handleKey(_ *widgets.FocusNode, e services.KeyEvent) widgets.KeyEventResult {
	switch e.(type) {
	case *services.KeyDownEvent, *services.KeyRepeatEvent:
	default:
		return widgets.KeyEventResultIgnored
	}
	m := s.m
	kb := s.bound.Keyboard()
	handled := true
	switch k := e.LogicalKey(); k {
	case services.LogicalKeyboardKeyArrowLeft:
		m.move(-1)
	case services.LogicalKeyboardKeyArrowRight:
		m.move(1)
	case services.LogicalKeyboardKeyEscape:
		if m.isFocused() && !m.isOpen() {
			m.setUnfocused()
		} else {
			handled = false
		}
	case services.LogicalKeyboardKeyArrowDown, services.LogicalKeyboardKeyEnter, services.LogicalKeyboardKeyNumpadEnter:
		if m.isOpen() {
			handled = false
		} else {
			m.openFocused()
		}
	case services.LogicalKeyboardKeyTab:
		// the bar never lets Tab move the focus
	default:
		key := mnemonicOf(e)
		i, ok := m.mnemonics[key]
		if m.isOpen() || kb.IsControlPressed() || !m.enableMnemonics || !m.mnemonicsInUse || key == "" || !ok {
			handled = false
		} else {
			m.trigger(i, false)
		}
	}
	if !handled {
		return widgets.KeyEventResultIgnored
	}
	s.sync()
	return widgets.KeyEventResultHandled
}

// pressed is the pointer going down on a title: a menu opens at once and its release is ignored.
func (s *menuBarState) pressed(i int, e *gestures.PointerDownEvent) {
	if e.Buttons()&gestures.PrimaryButton == 0 {
		return
	}
	if !s.m.isOpen() {
		s.ignoreNextUp = true
		s.m.trigger(i, true)
		s.sync()
	} else {
		s.ignoreNextUp = false
	}
}

// released is the pointer going up on a title: it triggers the title unless the press did.
func (s *menuBarState) released(i int) {
	if s.ignoreNextUp {
		s.ignoreNextUp = false
		return
	}
	if s.m.isFocused() {
		s.m.trigger(i, true)
		s.sync()
	}
}

func (s *menuBarState) entered(i int) {
	s.hover = i
	s.m.hoverEnter(i)
	s.sync()
}

func (s *menuBarState) exited(i int) {
	if s.hover == i {
		s.hover = -1
		s.SetState(func() {})
	}
}

// tappedOutside is a click outside the bar and its menus.
func (s *menuBarState) tappedOutside() {
	if s.m.isFocused() {
		s.m.setUnfocused()
		s.sync()
	}
}

func (s *menuBarState) run(it MenuItem) {
	// the menu is gone before the item runs
	s.m.setUnfocused()
	s.sync()
	if f := s.Widget().opts.OnRun; f != nil {
		f(it)
	}
}

// Build implements widgets.State.
func (s *menuBarState) Build(bc widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	colors := ThemeOf(bc)
	height := w.opts.Height.OrElse(menuBarHeight)
	titles := make([]widgets.Widget, len(w.menus))
	for i := range w.menus {
		titles[i] = s.button(bc, colors, i, height)
	}
	row := widgets.NewRow(titles, widgets.RowOpts{MainAxisSize: rendering.MainAxisSizeMin, CrossAxisAlignment: rendering.CrossAxisAlignmentCenter})
	var body widgets.Widget = widgets.NewRawMenuAnchorGroup(s.group, widgets.NewOffstage(row, widgets.OffstageOpts{Offstage: foundation.Some(!s.m.isVisible())}))
	body = widgets.NewTapRegion(body, widgets.TapRegionOpts{GroupID: s.group, OnTapOutside: func(*gestures.PointerDownEvent) { s.tappedOutside() }, Behavior: rendering.HitTestBehaviorTranslucent})
	return widgets.NewFocus(body, widgets.FocusOpts{
		FocusNode: s.node, OnKeyEvent: s.handleKey, SkipTraversal: foundation.Some(true), DebugLabel: "MenuBar",
		OnFocusChange: func(has bool) {
			if !has && s.m.isFocused() {
				s.m.setUnfocused()
				s.sync()
			}
		},
	})
}

// button builds the title of a menu and its anchor.
func (s *menuBarState) button(bc widgets.BuildContext, colors Colors, i int, height float64) widgets.Widget {
	w := s.Widget()
	m := s.m
	open := m.isOpen() && m.focused == i
	focused := m.isFocused() && !m.isOpen() && m.focused == i
	hovered := s.hover == i && !m.isFocused()
	fg, bg := colors.TitleBarActiveForeground, ui.Color{}
	selected := open || focused || hovered
	if selected {
		fg, bg = colors.MenubarSelectionForeground, colors.MenubarSelectionBackground
	}
	opts := widgets.ContainerOpts{
		Height:     foundation.Some(menuBarTitleHeight),
		Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(bg), BorderRadius: painting.NewBorderRadiusCircular(menuBarTitleRadius)}),
	}
	if selected && colors.MenubarSelectionBorder.Alpha() > 0 {
		opts.ForegroundDecoration = painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.MenubarSelectionBorder), Width: foundation.Some(1.0)}),
			BorderRadius: painting.NewBorderRadiusCircular(menuBarTitleRadius),
		})
	}
	label := labelWidget(bc, w.menus[i].Label, m.enableMnemonics, s.mnemonicsShown(), menuBarFontSize, fg)
	title := widgets.NewContainer(widgets.NewPadding(painting.NewEdgeInsetsSymmetric(0, menuBarTitlePadH), widgets.NewCenter(label)), opts)
	anchor := widgets.NewRawMenuAnchor(s.ctrls[i], func(bc widgets.BuildContext, info widgets.RawMenuOverlayInfo) widgets.Widget {
		return s.overlay(i, info)
	}, title, widgets.RawMenuAnchorOpts{UseRootOverlay: true, OnClose: func() { s.menuClosed(i) }})
	return widgets.NewMouseRegion(
		widgets.NewListener(widgets.NewSizedBox(widgets.NewCenter(anchor), widgets.SizedBoxOpts{Height: foundation.Some(height)}), widgets.ListenerOpts{
			Behavior:      rendering.HitTestBehaviorOpaque,
			OnPointerDown: func(e *gestures.PointerDownEvent) { s.pressed(i, e) },
			OnPointerUp:   func(*gestures.PointerUpEvent) { s.released(i) },
		}),
		widgets.MouseRegionOpts{
			Cursor:  services.SystemMouseCursors.Basic,
			OnEnter: func(*gestures.PointerEnterEvent) { s.entered(i) },
			OnExit:  func(*gestures.PointerExitEvent) { s.exited(i) },
		})
}

// overlay builds the menu of a title under it.
func (s *menuBarState) overlay(i int, info widgets.RawMenuOverlayInfo) widgets.Widget {
	m := s.m
	anchor := info.AnchorRect
	menu := NewMenu(s.Widget().menus[i].Items, MenuOpts{
		EnableMnemonics: m.alwaysOnMnemonics || (m.mnemonicsInUse && m.enableMnemonics),
		Autofocus:       true,
		SelectFirst:     m.openedViaKeyboard,
		MaxHeight:       foundation.Some(max(info.OverlaySize.Height-anchor.Bottom-menuScreenMargin, menuMinMaxHeight)),
		tapGroup:        info.TapRegionGroupID,
		OnCancel: func() {
			s.m.cancelMenu()
			s.sync()
		},
		OnRun: s.run,
		sideKey: func(dir int) {
			s.m.move(dir)
			s.sync()
		},
	})
	layout := &anchoredLayout{SingleChildLayoutDelegateBase: rendering.NewSingleChildLayoutDelegateBase(nil), place: func(overlay, child ui.Size) ui.Offset {
		// under the title; a menu that would leave the window is pushed in
		return ui.Offset{Dx: min(anchor.Left, max(overlay.Width-child.Width, 0)), Dy: anchor.Bottom}
	}, key: anchor}
	return widgets.NewPositionedFill(widgets.NewCustomSingleChildLayout(layout, menu))
}

// labelWidget shows a label in a size: with mnemonics on, the mnemonic character is underlined
// when underline is set.
func labelWidget(bc widgets.BuildContext, label string, mnemonics, underline bool, size float64, color ui.Color) widgets.Widget {
	p := ParseMenuLabel(label, mnemonics)
	style := TextStyleOf(bc, size, color)
	if p.Mnemonic == "" || !underline {
		return widgets.NewText(p.Text(), widgets.TextOpts{Style: style, MaxLines: foundation.Some(1)})
	}
	under := painting.NewTextStyle(painting.TextStyleOpts{Decoration: foundation.Some(ui.TextDecorationUnderline)})
	return widgets.NewTextRich(painting.NewTextSpan(painting.TextSpanOpts{
		Style: style,
		Children: []painting.InlineSpan{
			painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(p.Before)}),
			painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(p.Mnemonic), Style: under}),
			painting.NewTextSpan(painting.TextSpanOpts{Text: foundation.Some(p.After)}),
		},
	}), widgets.TextOpts{MaxLines: foundation.Some(1)})
}
