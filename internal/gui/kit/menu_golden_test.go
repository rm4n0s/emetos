package kit

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// goldenMenuItems has every kind of item: plain ones with and without a keybinding, a checked
// one, a disabled one, a separator and a submenu.
func goldenMenuItems() []MenuItem {
	return []MenuItem{
		{ID: "new", Label: "&New File", Keybinding: "Ctrl+N"},
		{ID: "open", Label: "&Open File...", Keybinding: "Ctrl+O"},
		SubmenuItem("Open &Recent", []MenuItem{
			{ID: "r1", Label: "main.go", Keybinding: "Ctrl+1"},
			{ID: "r2", Label: "README.md"},
			SeparatorItem(),
			{ID: "clear", Label: "&Clear Recently Opened"},
		}),
		SeparatorItem(),
		{ID: "save", Label: "&Save", Keybinding: "Ctrl+S", Disabled: true},
		{ID: "autosave", Label: "A&uto Save", Checked: true},
		{ID: "wrap", Label: "Word &Wrap", Keybinding: "Alt+Z", Checked: true},
		SeparatorItem(),
		{ID: "exit", Label: "E&xit"},
	}
}

func TestGoldenMenu(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			handle := NewMenuHandle()
			menu := NewMenu(goldenMenuItems(), MenuOpts{Handle: handle, EnableMnemonics: true, OnRun: func(MenuItem) {}})
			ts := pumpThemed(t, th.colors(), ui.Size{Width: 300, Height: 300}, padded(menu))
			// the second item is focused
			handle.Focus(true)
			ts.Pump(time.Millisecond)
			key(ts, services.LogicalKeyboardKeyArrowDown)
			testutil.MatchesGolden(ts, "menu_"+th.name)
		})
	}
}

func TestGoldenMenuSubmenu(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			handle := NewMenuHandle()
			menu := NewMenu(goldenMenuItems(), MenuOpts{Handle: handle, OnRun: func(MenuItem) {}})
			ts := pumpThemed(t, th.colors(), ui.Size{Width: 520, Height: 300}, padded(menu))
			handle.Focus(true)
			ts.Pump(time.Millisecond)
			key(ts, services.LogicalKeyboardKeyArrowDown)
			key(ts, services.LogicalKeyboardKeyArrowDown) // Open Recent
			key(ts, services.LogicalKeyboardKeyArrowRight)
			key(ts, services.LogicalKeyboardKeyArrowDown)
			testutil.MatchesGolden(ts, "menu_submenu_"+th.name)
		})
	}
}

func goldenBarMenus() []MenuBarMenu {
	return []MenuBarMenu{
		{Label: "&File", Items: goldenMenuItems()},
		{Label: "&Edit", Items: []MenuItem{
			{ID: "undo", Label: "&Undo", Keybinding: "Ctrl+Z"},
			{ID: "redo", Label: "&Redo", Keybinding: "Ctrl+Y"},
			SeparatorItem(),
			{ID: "cut", Label: "Cu&t", Keybinding: "Ctrl+X"},
			{ID: "copy", Label: "&Copy", Keybinding: "Ctrl+C"},
			{ID: "paste", Label: "&Paste", Keybinding: "Ctrl+V", Disabled: true},
			SeparatorItem(),
			{ID: "find", Label: "&Find", Keybinding: "Ctrl+F"},
		}},
		{Label: "&Selection", Items: []MenuItem{{ID: "all", Label: "Select &All"}}},
		{Label: "&View", Items: []MenuItem{{ID: "wrap", Label: "Word &Wrap"}}},
		{Label: "&Go", Items: []MenuItem{{ID: "line", Label: "Go to &Line..."}}},
		{Label: "&Help", Items: []MenuItem{{ID: "about", Label: "&About"}}},
	}
}

func TestGoldenMenuBar(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name+" mnemonics", func(t *testing.T) {
			ctrl := NewMenuBarController()
			ts := pumpThemed(t, th.colors(), ui.Size{Width: 420, Height: 60}, NewMenuBar(goldenBarMenus(), MenuBarOpts{Controller: ctrl}))
			// a tap of Alt focuses the first title and underlines the mnemonics, the pointer rests
			// on the third title
			ts.SendKeyDownEvent(services.LogicalKeyboardKeyAltLeft)
			ts.SendKeyUpEvent(services.LogicalKeyboardKeyAltLeft)
			ts.Pump(time.Millisecond)
			ts.Pump(time.Millisecond)
			if !ctrl.IsFocused() || !ctrl.MnemonicsShown() {
				t.Fatal("Alt did not focus the bar")
			}
			testutil.MatchesGolden(ts, "menubar_mnemonics_"+th.name)
		})
		t.Run(th.name+" open", func(t *testing.T) {
			ctrl := NewMenuBarController()
			ts := pumpThemed(t, th.colors(), ui.Size{Width: 420, Height: 260}, NewMenuBar(goldenBarMenus(), MenuBarOpts{Controller: ctrl}))
			ts.TapAt(ts.Center(gluttertest.ByText("Edit")), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
			ts.Pump(time.Millisecond)
			ts.Pump(time.Millisecond)
			ts.Hover(ts.Center(gluttertest.ByText("Copy")))
			ts.Hover(ts.Center(gluttertest.ByText("Copy")).Add(ui.Offset{Dx: 2}))
			ts.Pump(time.Millisecond)
			if !ctrl.IsOpen() {
				t.Fatal("the menu is not open")
			}
			testutil.MatchesGolden(ts, "menubar_open_"+th.name)
		})
	}
}

func TestGoldenContextMenu(t *testing.T) {
	for _, th := range goldenThemes {
		t.Run(th.name, func(t *testing.T) {
			ctrl := NewContextMenuController()
			handle := NewMenuHandle()
			page := widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Width: foundation.Some(320.0), Height: foundation.Some(300.0)})
			ts := pumpThemed(t, th.colors(), ui.Size{Width: 320, Height: 300}, NewContextMenu(page, ContextMenuOpts{Controller: ctrl, Handle: handle}))
			ctrl.Show(ui.Offset{Dx: 24, Dy: 20}, goldenMenuItems())
			ts.Pump(time.Millisecond)
			ts.Pump(time.Millisecond)
			key(ts, services.LogicalKeyboardKeyArrowDown)
			key(ts, services.LogicalKeyboardKeyArrowDown)
			if !ctrl.IsShowing() {
				t.Fatal("the menu is not showing")
			}
			testutil.MatchesGolden(ts, "context_menu_"+th.name)
		})
	}
}
