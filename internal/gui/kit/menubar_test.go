package kit

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

func testBarMenus() []MenuBarMenu {
	return []MenuBarMenu{
		{Label: "&File", Items: []MenuItem{{ID: "new", Label: "&New File", Keybinding: "Ctrl+N"}, {ID: "open", Label: "&Open..."}, SeparatorItem(), {ID: "exit", Label: "E&xit"}}},
		{Label: "&Edit", Items: []MenuItem{{ID: "undo", Label: "&Undo", Keybinding: "Ctrl+Z"}, {ID: "redo", Label: "&Redo"}, SeparatorItem(), {ID: "cut", Label: "Cu&t"}}},
		{Label: "&Selection", Items: []MenuItem{{ID: "all", Label: "Select &All"}}},
		{Label: "&View", Items: []MenuItem{{ID: "wrap", Label: "Word &Wrap"}, SubmenuItem("&Appearance", []MenuItem{{ID: "zen", Label: "&Zen Mode"}})}},
	}
}

// menuBarRig is a menu bar over an area that has the focus.
type menuBarRig struct {
	ts      *gluttertest.Tester
	ctrl    *MenuBarController
	ran     []string
	visible []bool
	focus   []bool
	area    *widgets.FocusNode
	taps    int
}

func newMenuBarRig(t *testing.T, opts MenuBarOpts) *menuBarRig {
	t.Helper()
	r := &menuBarRig{ctrl: NewMenuBarController()}
	r.area = widgets.NewFocusNode(widgets.FocusNodeOpts{DebugLabel: "area"})
	t.Cleanup(r.area.Dispose)
	opts.Controller = r.ctrl
	opts.OnRun = func(it MenuItem) { r.ran = append(r.ran, it.ID) }
	opts.OnVisibilityChange = func(v bool) { r.visible = append(r.visible, v) }
	opts.OnFocusChange = func(f bool) { r.focus = append(r.focus, f) }
	area := widgets.NewExpanded(widgets.NewGestureDetector(widgets.NewFocus(widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(500.0)}), widgets.FocusOpts{FocusNode: r.area, Autofocus: true}),
		widgets.GestureDetectorOpts{Behavior: rendering.HitTestBehaviorOpaque, OnTap: func() { r.taps++ }}))
	page := widgets.NewColumn([]widgets.Widget{
		NewMenuBar(testBarMenus(), opts), area,
	}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})
	r.ts = pumpSized(t, ui.Size{Width: 500, Height: 300}, widgets.NewSizedBox(page, widgets.SizedBoxOpts{Width: foundation.Some(500.0), Height: foundation.Some(300.0)}))
	r.ts.Pump(time.Millisecond)
	return r
}

var menuBarLabels = []string{"File", "Edit", "Selection", "View"}

// title is the center of the title of a menu.
func (r *menuBarRig) title(i int) ui.Offset { return r.ts.Center(gluttertest.ByText(menuBarLabels[i])) }

func (r *menuBarRig) pump() { r.ts.Pump(time.Millisecond); r.ts.Pump(time.Millisecond) }

func (r *menuBarRig) key(k services.LogicalKeyboardKey) {
	r.ts.SendKeyDownEvent(k)
	r.ts.SendKeyUpEvent(k)
	r.pump()
}

// altTap presses and releases Alt on its own.
func (r *menuBarRig) altTap() {
	r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyAltLeft)
	r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyAltLeft)
	r.pump()
}

// altKey presses a key while Alt is held.
func (r *menuBarRig) altKey(k services.LogicalKeyboardKey) {
	r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyAltLeft)
	r.ts.SendKeyDownEvent(k)
	r.ts.SendKeyUpEvent(k)
	r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyAltLeft)
	r.pump()
}

// press presses the left button on the title of a menu, and release lets it go.
func (r *menuBarRig) click(i int) {
	r.ts.TapAt(r.title(i), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
	r.pump()
}

func (r *menuBarRig) hover(i int) {
	r.ts.Hover(r.title(i))
	r.pump()
}

func (r *menuBarRig) state() string {
	return fmt.Sprintf("visible=%v focused=%v open=%v menu=%d", r.ctrl.IsVisible(), r.ctrl.IsFocused(), r.ctrl.IsOpen(), r.ctrl.FocusedMenu())
}

// menuOpen says which menu shows its items: the first label of each menu that is on screen.
func (r *menuBarRig) openMenu() int {
	for i, label := range []string{"New File", "Undo", "Select All", "Word Wrap"} {
		if r.ts.Exists(gluttertest.ByText(label)) {
			return i
		}
	}
	return -1
}

func TestMenuBarVisibilityModes(t *testing.T) {
	cases := []struct {
		name        string
		opts        MenuBarOpts
		want        bool
		afterAlt    string
		afterAltEsc string
	}{
		{"classic", MenuBarOpts{}, true, "visible=true focused=true open=false menu=0", "visible=true focused=false open=false menu=-1"},
		{"classic in full screen", MenuBarOpts{FullScreen: true}, false, "visible=true focused=true open=false menu=0", "visible=false focused=false open=false menu=-1"},
		{"visible", MenuBarOpts{Visibility: MenuBarVisible}, true, "visible=true focused=true open=false menu=0", "visible=true focused=false open=false menu=-1"},
		{"visible in full screen", MenuBarOpts{Visibility: MenuBarVisible, FullScreen: true}, true, "visible=true focused=true open=false menu=0", "visible=true focused=false open=false menu=-1"},
		{"toggle", MenuBarOpts{Visibility: MenuBarToggle}, false, "visible=true focused=true open=false menu=0", "visible=false focused=false open=false menu=-1"},
		{"hidden", MenuBarOpts{Visibility: MenuBarHidden}, false, "visible=false focused=false open=false menu=-1", "visible=false focused=false open=false menu=-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newMenuBarRig(t, tc.opts)
			if r.ctrl.IsVisible() != tc.want {
				t.Errorf("visible = %v at first, want %v", r.ctrl.IsVisible(), tc.want)
			}
			if r.ts.Exists(gluttertest.ByText("File")) && !tc.want {
				if h := r.ts.Size(gluttertest.ByType[*MenuBar]()).Height; h != 0 {
					t.Errorf("a hidden bar is %v px high", h)
				}
			}
			if tc.want {
				if h := r.ts.Size(gluttertest.ByType[*MenuBar]()).Height; h != 30 {
					t.Errorf("the bar is %v px high, want 30", h)
				}
			}
			r.altTap()
			if got := r.state(); got != tc.afterAlt {
				t.Errorf("after Alt: %s, want %s", got, tc.afterAlt)
			}
			r.key(services.LogicalKeyboardKeyEscape)
			if got := r.state(); got != tc.afterAltEsc {
				t.Errorf("after Alt and Escape: %s, want %s", got, tc.afterAltEsc)
			}
			if errs := r.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

func TestMenuBarToggleShowsAndHidesTheBar(t *testing.T) {
	r := newMenuBarRig(t, MenuBarOpts{Visibility: MenuBarToggle})
	r.altTap()
	if h := r.ts.Size(gluttertest.ByType[*MenuBar]()).Height; h != 30 {
		t.Errorf("the bar is %v px high after Alt, want 30", h)
	}
	r.altTap() // a second tap gives the focus up, and the bar hides again
	if r.ctrl.IsVisible() || r.ctrl.IsFocused() {
		t.Errorf("after the second tap: %s", r.state())
	}
	if fmt.Sprint(r.visible) != "[true false]" {
		t.Errorf("OnVisibilityChange got %v, want [true false]", r.visible)
	}
}

func TestMenuBarClickOpensAndClosesMenus(t *testing.T) {
	r := newMenuBarRig(t, MenuBarOpts{})
	r.click(0)
	if r.openMenu() != 0 || r.state() != "visible=true focused=true open=true menu=0" {
		t.Fatalf("a click did not open the File menu: %s, menu %d", r.state(), r.openMenu())
	}
	// the menu opens under its title: its left edge on the title's and its top at the title's bottom
	tl := r.ts.TopLeft(gluttertest.ByType[*Menu]())
	if tl.Dy != 26 {
		t.Errorf("the menu is at y=%v, want 26 (the bottom of the 22 px title)", tl.Dy)
	}
	// a click on the title of the open menu closes everything
	r.click(0)
	if r.openMenu() != -1 || r.ctrl.IsFocused() {
		t.Errorf("a click on the open title left %s, menu %d", r.state(), r.openMenu())
	}
	// a click opens it again
	r.click(1)
	if r.openMenu() != 1 {
		t.Errorf("menu %d, want 1", r.openMenu())
	}
}

func TestMenuBarHoverSwitchesMenus(t *testing.T) {
	r := newMenuBarRig(t, MenuBarOpts{})
	r.hover(1)
	if r.ctrl.IsOpen() {
		t.Fatal("hovering a title opened its menu with no menu open")
	}
	r.click(0)
	r.hover(2)
	if r.openMenu() != 2 || r.ctrl.FocusedMenu() != 2 {
		t.Errorf("hovering another title: menu %d, %s", r.openMenu(), r.state())
	}
	// the menu that opens by hover has no item selected
	if r.ts.Exists(gluttertest.ByText("New File")) {
		t.Error("the first menu is still open")
	}
	r.hover(3)
	if r.openMenu() != 3 {
		t.Errorf("menu %d, want 3", r.openMenu())
	}
}

func TestMenuBarHoverFocusesTitleOfFocusedBar(t *testing.T) {
	r := newMenuBarRig(t, MenuBarOpts{})
	r.altTap()
	r.hover(2)
	if r.ctrl.FocusedMenu() != 2 || r.ctrl.IsOpen() {
		t.Errorf("hovering a title of a focused bar: %s", r.state())
	}
}

func TestMenuBarMenuKeyboard(t *testing.T) {
	left, right, esc := services.LogicalKeyboardKeyArrowLeft, services.LogicalKeyboardKeyArrowRight, services.LogicalKeyboardKeyEscape
	down, enter := services.LogicalKeyboardKeyArrowDown, services.LogicalKeyboardKeyEnter
	cases := []struct {
		name     string
		keys     []services.LogicalKeyboardKey
		wantOpen int
		wantMenu int
		wantBar  string
	}{
		{"Right opens the next menu", []services.LogicalKeyboardKey{right}, 1, 1, "visible=true focused=true open=true menu=1"},
		{"Right twice", []services.LogicalKeyboardKey{right, right}, 2, 2, "visible=true focused=true open=true menu=2"},
		{"Right wraps from the last menu to the first", []services.LogicalKeyboardKey{right, right, right, right}, 0, 0, "visible=true focused=true open=true menu=0"},
		{"Left wraps from the first menu to the last", []services.LogicalKeyboardKey{left}, 3, 3, "visible=true focused=true open=true menu=3"},
		{"Left goes back", []services.LogicalKeyboardKey{right, left}, 0, 0, "visible=true focused=true open=true menu=0"},
		{"Escape closes the menu and leaves its title focused", []services.LogicalKeyboardKey{esc}, -1, 0, "visible=true focused=true open=false menu=0"},
		{"Escape twice gives the focus up", []services.LogicalKeyboardKey{esc, esc}, -1, -1, "visible=true focused=false open=false menu=-1"},
		{"Down moves in the menu and keeps it open", []services.LogicalKeyboardKey{down}, 0, 0, "visible=true focused=true open=true menu=0"},
		{"Enter runs the focused item and closes the menu", []services.LogicalKeyboardKey{enter}, -1, -1, "visible=true focused=false open=false menu=-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newMenuBarRig(t, MenuBarOpts{})
			r.altKey(services.LogicalKeyboardKeyKeyF) // opens File with its first item focused
			if r.openMenu() != 0 {
				t.Fatalf("Alt+F did not open the File menu: %s", r.state())
			}
			for _, k := range tc.keys {
				r.key(k)
			}
			if got := r.openMenu(); got != tc.wantOpen {
				t.Errorf("open menu %d, want %d", got, tc.wantOpen)
			}
			if got := r.ctrl.FocusedMenu(); got != tc.wantMenu {
				t.Errorf("focused menu %d, want %d", got, tc.wantMenu)
			}
			if got := r.state(); got != tc.wantBar {
				t.Errorf("state %s, want %s", got, tc.wantBar)
			}
			if errs := r.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}

func TestMenuBarLeftRightInSubmenuDoNotSwitchMenus(t *testing.T) {
	r := newMenuBarRig(t, MenuBarOpts{})
	r.altKey(services.LogicalKeyboardKeyKeyV)
	r.key(services.LogicalKeyboardKeyArrowDown)
	r.key(services.LogicalKeyboardKeyArrowRight) // the second item is a submenu: Right opens it
	if !r.ts.Exists(gluttertest.ByText("Zen Mode")) || r.ctrl.FocusedMenu() != 3 {
		t.Fatalf("the submenu did not open: %s", r.state())
	}
	r.key(services.LogicalKeyboardKeyArrowLeft) // Left closes the submenu, and stays in the menu
	if r.ts.Exists(gluttertest.ByText("Zen Mode")) || r.ctrl.FocusedMenu() != 3 || !r.ctrl.IsOpen() {
		t.Errorf("Left in a submenu: %s", r.state())
	}
	r.key(services.LogicalKeyboardKeyArrowLeft) // now Left moves to the previous menu
	if r.ctrl.FocusedMenu() != 2 {
		t.Errorf("focused menu %d, want 2", r.ctrl.FocusedMenu())
	}
}

func TestMenuBarAltTap(t *testing.T) {
	r := newMenuBarRig(t, MenuBarOpts{})
	if r.ctrl.MnemonicsShown() {
		t.Fatal("the mnemonics are shown at first")
	}
	r.altTap()
	if got := r.state(); got != "visible=true focused=true open=false menu=0" {
		t.Fatalf("after Alt: %s", got)
	}
	if !r.ctrl.MnemonicsShown() {
		t.Error("the mnemonics are not shown after Alt")
	}
	if r.area.HasPrimaryFocus() {
		t.Error("the area kept the focus")
	}
	// Right and Left move between the titles
	r.key(services.LogicalKeyboardKeyArrowRight)
	r.key(services.LogicalKeyboardKeyArrowRight)
	if r.ctrl.FocusedMenu() != 2 || r.ctrl.IsOpen() {
		t.Errorf("after Right twice: %s", r.state())
	}
	r.key(services.LogicalKeyboardKeyArrowLeft)
	if r.ctrl.FocusedMenu() != 1 {
		t.Errorf("after Left: %s", r.state())
	}
	// Down opens the menu with its first item focused
	r.key(services.LogicalKeyboardKeyArrowDown)
	if r.openMenu() != 1 || !r.ts.Exists(gluttertest.ByText("Undo")) {
		t.Fatalf("Down did not open the Edit menu: %s", r.state())
	}
	// Escape closes the menu, the title stays focused; Escape gives the focus up and back
	r.key(services.LogicalKeyboardKeyEscape)
	r.key(services.LogicalKeyboardKeyEscape)
	if r.ctrl.IsFocused() || r.ctrl.MnemonicsShown() {
		t.Errorf("after Escape twice: %s, mnemonics %v", r.state(), r.ctrl.MnemonicsShown())
	}
	if !r.area.HasPrimaryFocus() {
		t.Error("the focus did not go back to the area")
	}
	if fmt.Sprint(r.focus) != "[true false]" {
		t.Errorf("OnFocusChange got %v", r.focus)
	}
}

func TestMenuBarEnterOpensTheFocusedMenu(t *testing.T) {
	r := newMenuBarRig(t, MenuBarOpts{})
	r.altTap()
	r.key(services.LogicalKeyboardKeyEnter)
	if r.openMenu() != 0 {
		t.Errorf("Enter did not open the menu: %s", r.state())
	}
}

func TestMenuBarAltTapGivesTheFocusUp(t *testing.T) {
	r := newMenuBarRig(t, MenuBarOpts{})
	r.altTap()
	r.altTap()
	if r.ctrl.IsFocused() || !r.area.HasPrimaryFocus() {
		t.Errorf("a second tap of Alt: %s, area focus %v", r.state(), r.area.HasPrimaryFocus())
	}
}

func TestMenuBarAltIsNoTapWithAnotherKeyOrAPointerPress(t *testing.T) {
	t.Run("another key between the press and the release", func(t *testing.T) {
		r := newMenuBarRig(t, MenuBarOpts{})
		r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyAltLeft)
		r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyKeyK) // not a mnemonic
		r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyKeyK)
		r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyAltLeft)
		r.pump()
		if r.ctrl.IsFocused() {
			t.Errorf("Alt+K focused the bar: %s", r.state())
		}
	})
	t.Run("a click between the press and the release", func(t *testing.T) {
		r := newMenuBarRig(t, MenuBarOpts{})
		r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyAltLeft)
		r.ts.TapAt(ui.Offset{Dx: 300, Dy: 200}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
		r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyAltLeft)
		r.pump()
		if r.ctrl.IsFocused() {
			t.Errorf("Alt+click focused the bar: %s", r.state())
		}
	})
	t.Run("Shift with Alt", func(t *testing.T) {
		r := newMenuBarRig(t, MenuBarOpts{})
		r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyAltLeft)
		r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyShiftLeft)
		r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyShiftLeft)
		r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyAltLeft)
		r.pump()
		if r.ctrl.IsFocused() {
			t.Errorf("Alt+Shift focused the bar: %s", r.state())
		}
	})
	t.Run("a repeating Alt is one tap", func(t *testing.T) {
		r := newMenuBarRig(t, MenuBarOpts{})
		r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyAltLeft)
		r.ts.SendKeyRepeatEvent(services.LogicalKeyboardKeyAltLeft)
		r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyAltLeft)
		r.pump()
		if !r.ctrl.IsFocused() {
			t.Errorf("Alt with repeats did not focus the bar: %s", r.state())
		}
	})
}

func TestMenuBarAltLetterOpensMenu(t *testing.T) {
	cases := []struct {
		name string
		key  services.LogicalKeyboardKey
		want int
	}{
		{"Alt+F", services.LogicalKeyboardKeyKeyF, 0},
		{"Alt+E", services.LogicalKeyboardKeyKeyE, 1},
		{"Alt+S", services.LogicalKeyboardKeyKeyS, 2},
		{"Alt+V", services.LogicalKeyboardKeyKeyV, 3},
		{"Alt+Z has no menu", services.LogicalKeyboardKeyKeyZ, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newMenuBarRig(t, MenuBarOpts{})
			r.altKey(tc.key)
			if got := r.openMenu(); got != tc.want {
				t.Errorf("open menu %d, want %d", got, tc.want)
			}
			if tc.want >= 0 {
				// the menu opened with the keyboard has its first item focused and the mnemonics on
				r.key(services.LogicalKeyboardKeyEnter)
				if len(r.ran) != 1 {
					t.Errorf("Enter ran %v, want the first item of the menu", r.ran)
				}
			}
		})
	}
}

func TestMenuBarAltLetterFromAHiddenBar(t *testing.T) {
	for _, vis := range []MenuBarVisibility{MenuBarToggle, MenuBarHidden} {
		t.Run(vis.String(), func(t *testing.T) {
			r := newMenuBarRig(t, MenuBarOpts{Visibility: vis})
			r.altKey(services.LogicalKeyboardKeyKeyE)
			if r.openMenu() != 1 || !r.ctrl.IsVisible() {
				t.Fatalf("Alt+E: menu %d, %s", r.openMenu(), r.state())
			}
			r.key(services.LogicalKeyboardKeyEscape)
			r.key(services.LogicalKeyboardKeyEscape)
			if r.ctrl.IsVisible() {
				t.Errorf("the bar stays visible after the menu is left: %s", r.state())
			}
		})
	}
}

func TestMenuBarMnemonicLettersWhileFocused(t *testing.T) {
	r := newMenuBarRig(t, MenuBarOpts{})
	r.altTap()
	r.key(services.LogicalKeyboardKeyKeyE)
	if r.openMenu() != 1 {
		t.Errorf("E did not open the Edit menu: %s", r.state())
	}
	// in the menu, a letter is the mnemonic of an item: R is Redo
	r.key(services.LogicalKeyboardKeyKeyR)
	if fmt.Sprint(r.ran) != "[redo]" {
		t.Errorf("ran %v, want [redo]", r.ran)
	}
}

func TestMenuBarMnemonicsOff(t *testing.T) {
	r := newMenuBarRig(t, MenuBarOpts{DisableMnemonics: true})
	r.altKey(services.LogicalKeyboardKeyKeyF)
	if r.ctrl.IsOpen() {
		t.Error("Alt+F opened a menu with the mnemonics off")
	}
	r.altTap()
	if r.ctrl.MnemonicsShown() {
		t.Error("the mnemonics are shown although they are off")
	}
	if r.ts.Exists(gluttertest.ByText("&File")) {
		t.Error("the marker is shown")
	}
}

func TestMenuBarDisableAltFocus(t *testing.T) {
	r := newMenuBarRig(t, MenuBarOpts{DisableAltFocus: true})
	r.altTap()
	if r.ctrl.IsFocused() {
		t.Error("Alt focused the bar with the Alt focus off")
	}
	r.altKey(services.LogicalKeyboardKeyKeyF)
	if r.openMenu() != 0 {
		t.Error("Alt+F does not open the menu with the Alt focus off")
	}
	// the toggle visibility keeps Alt
	r2 := newMenuBarRig(t, MenuBarOpts{DisableAltFocus: true, Visibility: MenuBarToggle})
	r2.altTap()
	if !r2.ctrl.IsFocused() {
		t.Error("Alt does not show a toggled bar with the Alt focus off")
	}
}

func TestMenuBarClickOutsideGivesTheFocusUp(t *testing.T) {
	t.Run("with a menu open", func(t *testing.T) {
		r := newMenuBarRig(t, MenuBarOpts{})
		r.click(1)
		r.ts.TapAt(ui.Offset{Dx: 400, Dy: 250}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
		r.pump()
		if r.ctrl.IsFocused() || r.openMenu() != -1 {
			t.Errorf("%s, menu %d", r.state(), r.openMenu())
		}
		if r.taps != 1 {
			t.Errorf("the click did not go on to the area (%d taps)", r.taps)
		}
	})
	t.Run("with a focused bar", func(t *testing.T) {
		r := newMenuBarRig(t, MenuBarOpts{})
		r.altTap()
		r.ts.TapAt(ui.Offset{Dx: 400, Dy: 250}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
		r.pump()
		if r.ctrl.IsFocused() {
			t.Errorf("%s", r.state())
		}
	})
}

func TestMenuBarRunningAnItemGivesTheFocusUp(t *testing.T) {
	r := newMenuBarRig(t, MenuBarOpts{})
	r.click(0)
	r.ts.Tap(gluttertest.ByText("Open..."))
	r.pump()
	if fmt.Sprint(r.ran) != "[open]" || r.ctrl.IsFocused() || r.ctrl.IsOpen() || r.openMenu() != -1 {
		t.Errorf("ran %v, %s, menu %d", r.ran, r.state(), r.openMenu())
	}
	if !r.area.HasPrimaryFocus() {
		t.Error("the focus did not go back to the area")
	}
}

func TestMenuBarClickedMenuHasNoMnemonics(t *testing.T) {
	r := newMenuBarRig(t, MenuBarOpts{})
	r.click(0)
	// the menu opened by the pointer shows no mnemonics, and no item is focused
	txt := r.ts.Widget(gluttertest.ByText("New File")).(*widgets.Text)
	if txt.TextSpan() != nil {
		t.Error("the label of a menu opened by a click has underlined mnemonics")
	}
	r.key(services.LogicalKeyboardKeyKeyN)
	if len(r.ran) != 0 {
		t.Errorf("a mnemonic ran %v in a menu with none", r.ran)
	}
}

func TestMenuBarToggleFocusFromOutside(t *testing.T) {
	r := newMenuBarRig(t, MenuBarOpts{})
	r.ctrl.ToggleFocus()
	r.pump()
	if !r.ctrl.IsFocused() || r.ctrl.FocusedMenu() != 0 {
		t.Fatalf("ToggleFocus: %s", r.state())
	}
	r.ctrl.ToggleFocus()
	r.pump()
	if r.ctrl.IsFocused() {
		t.Errorf("a second ToggleFocus: %s", r.state())
	}
	r.ctrl.ToggleFocus()
	r.pump()
	r.ctrl.Blur()
	r.pump()
	if r.ctrl.IsFocused() || !r.area.HasPrimaryFocus() {
		t.Errorf("Blur: %s, area focus %v", r.state(), r.area.HasPrimaryFocus())
	}
	h := newMenuBarRig(t, MenuBarOpts{Visibility: MenuBarHidden})
	h.ctrl.ToggleFocus()
	if h.ctrl.IsFocused() {
		t.Error("ToggleFocus focused a hidden bar")
	}
}

func TestMenuBarVisibilityChangesWithTheOptions(t *testing.T) {
	type barOptions struct {
		visibility MenuBarVisibility
		fullScreen bool
	}
	opts := foundation.NewValueNotifier(barOptions{})
	ctrl := NewMenuBarController()
	var visible []bool
	ts := pumpSized(t, ui.Size{Width: 500, Height: 300}, widgets.NewValueListenableBuilder[barOptions](opts, func(_ widgets.BuildContext, o barOptions, _ widgets.Widget) widgets.Widget {
		return NewMenuBar(testBarMenus(), MenuBarOpts{
			Visibility: o.visibility, FullScreen: o.fullScreen, Controller: ctrl,
			OnVisibilityChange: func(v bool) { visible = append(visible, v) },
		})
	}, nil))
	steps := []struct {
		name string
		set  barOptions
		want bool
	}{
		{"classic", barOptions{}, true},
		{"full screen hides the classic bar", barOptions{fullScreen: true}, false},
		{"visible shows it in full screen", barOptions{visibility: MenuBarVisible, fullScreen: true}, true},
		{"toggle hides it", barOptions{visibility: MenuBarToggle}, false},
		{"classic shows it", barOptions{}, true},
		{"hidden hides it", barOptions{visibility: MenuBarHidden}, false},
	}
	for _, st := range steps {
		opts.SetValue(st.set)
		ts.Pump(time.Millisecond)
		if got := ctrl.IsVisible(); got != st.want {
			t.Errorf("%s: visible = %v, want %v", st.name, got, st.want)
		}
	}
	if fmt.Sprint(visible) != "[false true false true false]" {
		t.Errorf("OnVisibilityChange got %v", visible)
	}
}

func TestParseMenuBarVisibility(t *testing.T) {
	cases := []struct {
		in   string
		want MenuBarVisibility
		ok   bool
	}{
		{"classic", MenuBarClassic, true}, {"visible", MenuBarVisible, true}, {"toggle", MenuBarToggle, true}, {"hidden", MenuBarHidden, true},
		{"compact", MenuBarClassic, false}, {"", MenuBarClassic, false}, {"Classic", MenuBarClassic, false},
	}
	for _, tc := range cases {
		got, ok := ParseMenuBarVisibility(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ParseMenuBarVisibility(%q) = %v, %v; want %v, %v", tc.in, got, ok, tc.want, tc.ok)
		}
		if ok && got.String() != tc.in {
			t.Errorf("%v.String() = %q, want %q", got, got.String(), tc.in)
		}
	}
}

// underlined says whether the title of a menu underlines its mnemonic.
func underlined(t *testing.T, ts *gluttertest.Tester, label string) bool {
	t.Helper()
	txt := ts.Widget(gluttertest.ByText(label)).(*widgets.Text)
	span, ok := txt.TextSpan().(*painting.TextSpan)
	if !ok {
		return false
	}
	for _, c := range span.Children {
		if cs, ok := c.(*painting.TextSpan); ok && cs.Style() != nil {
			if d, ok := cs.Style().Decoration.Get(); ok && d.Contains(ui.TextDecorationUnderline) {
				return true
			}
		}
	}
	return false
}

func TestMenuBarUnderlinesMnemonics(t *testing.T) {
	t.Run("not by default", func(t *testing.T) {
		r := newMenuBarRig(t, MenuBarOpts{})
		if underlined(t, r.ts, "File") {
			t.Error("underlined")
		}
	})
	t.Run("while Alt is held", func(t *testing.T) {
		r := newMenuBarRig(t, MenuBarOpts{})
		r.ts.SendKeyDownEvent(services.LogicalKeyboardKeyAltLeft)
		r.pump()
		if !underlined(t, r.ts, "File") || !underlined(t, r.ts, "Edit") {
			t.Error("not underlined while Alt is down")
		}
		r.ts.SendKeyUpEvent(services.LogicalKeyboardKeyAltLeft)
		r.pump()
		// the release focused the bar, which keeps them
		if !underlined(t, r.ts, "File") {
			t.Error("not underlined after the tap")
		}
		r.key(services.LogicalKeyboardKeyEscape)
		if underlined(t, r.ts, "File") {
			t.Error("still underlined after the focus was given up")
		}
	})
	t.Run("always on", func(t *testing.T) {
		r := newMenuBarRig(t, MenuBarOpts{AlwaysOnMnemonics: true})
		if !underlined(t, r.ts, "File") {
			t.Error("not underlined")
		}
	})
	t.Run("not when mnemonics are off", func(t *testing.T) {
		r := newMenuBarRig(t, MenuBarOpts{DisableMnemonics: true, AlwaysOnMnemonics: true})
		if underlined(t, r.ts, "File") {
			t.Error("underlined")
		}
	})
}

func TestMenuBarControllerOpensAMenuFromOutside(t *testing.T) {
	r := newMenuBarRig(t, MenuBarOpts{})
	steps := []struct {
		name string
		do   func()
		want int
	}{
		{"nothing is open", func() {}, -1},
		{"Edit", func() { r.ctrl.Open(1); r.pump() }, 1},
		{"the open one stays open", func() { r.ctrl.Open(1); r.pump() }, 1},
		{"another menu replaces it", func() { r.ctrl.Open(3); r.pump() }, 3},
		{"an index that is no menu does nothing", func() { r.ctrl.Open(9); r.ctrl.Open(-1); r.pump() }, 3},
	}
	for _, s := range steps {
		s.do()
		if got := r.openMenu(); got != s.want {
			t.Errorf("%s: the open menu is %d, want %d (%s)", s.name, got, s.want, r.state())
		}
	}
	// opened like a click: no item is selected
	if got := r.ctrl.FocusedMenu(); got != 3 {
		t.Errorf("focused menu %d", got)
	}
	r.ctrl.Blur()
	r.pump()
	if r.openMenu() != -1 {
		t.Error("Blur did not close the menu")
	}
}

func TestMenuBarControllerDoesNotOpenAMenuOfAHiddenBar(t *testing.T) {
	r := newMenuBarRig(t, MenuBarOpts{Visibility: MenuBarHidden})
	r.ctrl.Open(1)
	r.pump()
	if r.openMenu() != -1 {
		t.Error("a menu opened in a bar that is hidden")
	}
}

func TestMenuBarMenuSourceIsAskedWhenTheMenuOpens(t *testing.T) {
	asked := 0
	var ran []any
	menus := []MenuBarMenu{
		{ID: "static", Label: "&Static", Items: []MenuItem{{ID: "a", Label: "Static Item"}}},
		{ID: "dynamic", Label: "&Dynamic", Items: []MenuItem{{ID: "old", Label: "Old Item"}}, Source: func() []MenuItem {
			asked++
			return []MenuItem{{ID: "fresh", Label: "Fresh Item", Data: "argument"}, {ID: "other", Label: "Other Item", Checked: true}}
		}},
	}
	ctl := NewMenuBarController()
	page := widgets.NewColumn([]widgets.Widget{
		NewMenuBar(menus, MenuBarOpts{Controller: ctl, OnRun: func(it MenuItem) { ran = append(ran, it.ID, it.Data) }}),
		widgets.NewExpanded(widgets.NewSizedBox(nil)),
	}, widgets.ColumnOpts{CrossAxisAlignment: rendering.CrossAxisAlignmentStart})
	ts := pumpSized(t, ui.Size{Width: 400, Height: 300}, widgets.NewSizedBox(page, widgets.SizedBoxOpts{Width: foundation.Some(400.0), Height: foundation.Some(300.0)}))
	ts.Pump(time.Millisecond)
	if asked != 0 {
		t.Fatalf("the source was asked %d times before a menu opened", asked)
	}
	ctl.Open(0)
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	if asked != 0 || !ts.Exists(gluttertest.ByText("Static Item")) {
		t.Fatalf("asked %d times, Static Item shown %v", asked, ts.Exists(gluttertest.ByText("Static Item")))
	}
	ctl.Open(1)
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	if asked != 1 {
		t.Errorf("the source was asked %d times, want once when the menu opened", asked)
	}
	if !ts.Exists(gluttertest.ByText("Fresh Item")) || ts.Exists(gluttertest.ByText("Old Item")) {
		t.Error("the menu does not show what the source gave")
	}
	// the menu stays as it was built while it is open, however often it is built again
	ts.Hover(ts.Center(gluttertest.ByText("Other Item")))
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	if asked != 1 {
		t.Errorf("the source was asked %d times while the menu stayed open", asked)
	}
	ts.Tap(gluttertest.ByText("Fresh Item"))
	ts.Pump(time.Millisecond)
	if !reflect.DeepEqual(ran, []any{"fresh", "argument"}) {
		t.Errorf("ran %v: the data of the item comes back with it", ran)
	}
}
