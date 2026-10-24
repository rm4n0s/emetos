package app

import (
	"reflect"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/workbench"
)

// openMenuItems are the items of the menu that is open.
func (a *keyApp) openMenuItems(t *testing.T) []kit.MenuItem {
	t.Helper()
	els := a.ts.Elements(gluttertest.ByType[*kit.Menu]())
	if len(els) == 0 {
		t.Fatal("no menu is open")
	}
	return els[0].Widget().(*kit.Menu).Items()
}

func (a *keyApp) pump() {
	for range 4 {
		a.ts.Pump(20 * time.Millisecond)
	}
}

func menuItem(items []kit.MenuItem, id string) (kit.MenuItem, bool) {
	for _, it := range items {
		if it.ID == id && !it.Submenu {
			return it, true
		}
		if it.Submenu {
			if sub, ok := menuItem(it.Items, id); ok {
				return sub, true
			}
		}
	}
	return kit.MenuItem{}, false
}

func (a *keyApp) devCommand(t *testing.T, id string, args any) error {
	t.Helper()
	err := a.win.Commands.Run(command.Context{Ctx: t.Context(), Keys: a.win.Keys, Source: command.SourceAPI}, id, args)
	a.pump()
	return err
}

func TestTheWindowHasTheMenuBarOfTheRegistry(t *testing.T) {
	a := newKeyApp(t, "alpha\nbeta\n")
	for _, title := range []string{"File", "Edit", "Selection", "View", "Go"} {
		if !a.ts.Exists(gluttertest.ByText(title)) {
			t.Errorf("no %s in the menu bar", title)
		}
	}
	for _, title := range []string{"Terminal", "Help"} {
		if a.ts.Exists(gluttertest.ByText(title)) {
			t.Errorf("%s is in the menu bar before it has a command", title)
		}
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestAMenuItemRunsTheCommandOfTheEditor(t *testing.T) {
	a := newKeyApp(t, "alpha\nbeta\n")
	a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(0, 2)})
	a.vm.SelectionsChanged()
	a.pump()

	a.ts.Tap(gluttertest.ByText("Selection"))
	a.pump()
	if it, ok := menuItem(a.openMenuItems(t), "editor.action.selectAll"); !ok || it.Keybinding != "Ctrl+A" {
		t.Fatalf("Select All in the menu: %+v %v", it, ok)
	}
	a.ts.Tap(gluttertest.ByText("Select All"))
	a.pump()
	// the menu had the focus while it was open: the command still reached the editor, which has it back
	if got := a.selections(); len(got) != 1 || got[0] != sel(0, 0, 2, 0) {
		t.Errorf("selections after Selection > Select All: %v", got)
	}
	if !a.vm.Focused() {
		t.Error("the editor does not have the focus after the menu closed")
	}

	// a command that changes the text, from the Edit menu
	a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(1, 4)})
	a.vm.SelectionsChanged()
	a.ts.Tap(gluttertest.ByText("Edit"))
	a.pump()
	a.ts.Tap(gluttertest.ByText("Undo"))
	a.pump()
	a.ts.Tap(gluttertest.ByText("Selection"))
	a.pump()
	a.ts.Tap(gluttertest.ByText("Copy Line Down"))
	a.pump()
	if got := a.text(); got != "alpha\nbeta\nbeta\n" {
		t.Errorf("text after Selection > Copy Line Down: %q", got)
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestMenusShowTheStateOfTheWindow(t *testing.T) {
	a := newKeyApp(t, "alpha beta gamma delta\n")
	// items opens a menu, reads its items and closes it
	items := func(menu string) []kit.MenuItem {
		t.Helper()
		if err := a.devCommand(t, "dev.openMenu", map[string]any{"id": menu}); err != nil {
			t.Fatal(err)
		}
		got := a.openMenuItems(t)
		a.ts.SendKeyEvent(services.LogicalKeyboardKeyEscape)
		a.pump()
		return got
	}
	// view is an item of the View menu, which has the Appearance submenu
	view := func(id string) kit.MenuItem {
		t.Helper()
		it, ok := menuItem(items(command.MenubarViewMenu), id)
		if !ok {
			t.Fatalf("%s is not in the View menu", id)
		}
		return it
	}
	check := func(when string, cases map[string]bool) {
		t.Helper()
		for id, want := range cases {
			if got := view(id).Checked; got != want {
				t.Errorf("%s: %s checked = %v, want %v", when, id, got, want)
			}
		}
	}
	// the layout toggles follow the layout and the settings, Word Wrap the editor
	check("at the start", map[string]bool{
		"workbench.action.toggleSidebarVisibility":   true,
		"workbench.action.togglePanel":               false,
		"workbench.action.toggleStatusbarVisibility": true,
		"workbench.action.toggleMenuBar":             true,
		"workbench.action.positionPanelBottom":       true,
		"workbench.action.positionPanelLeft":         false,
		"editor.action.toggleWordWrap":               false,
	})
	if view("editor.action.toggleWordWrap").Disabled {
		t.Error("Word Wrap is dimmed with an editor open")
	}
	for _, id := range []string{"workbench.action.toggleSidebarVisibility", "workbench.action.togglePanel", "workbench.action.toggleStatusbarVisibility", "workbench.action.positionPanelLeft"} {
		if err := a.devCommand(t, id, nil); err != nil {
			t.Fatal(err)
		}
	}
	a.vm.SetWordWrapOverride("on")
	a.pump()
	check("after the changes", map[string]bool{
		"workbench.action.toggleSidebarVisibility":   false,
		"workbench.action.togglePanel":               true,
		"workbench.action.toggleStatusbarVisibility": false,
		"workbench.action.positionPanelLeft":         true,
		"workbench.action.positionPanelBottom":       false,
		"editor.action.toggleWordWrap":               true,
	})

	// the Edit menu's items are dimmed while the document is read-only
	comment := func() kit.MenuItem {
		it, _ := menuItem(items(command.MenubarEditMenu), "editor.action.commentLine")
		return it
	}
	if comment().Disabled {
		t.Error("Toggle Line Comment is dimmed in a writable document")
	}
	a.vm.Document().SetReadOnly(true)
	if !comment().Disabled {
		t.Error("Toggle Line Comment is not dimmed in a read-only document")
	}
}

func TestTheEditorContextMenu(t *testing.T) {
	a := newKeyApp(t, "alpha beta\ngamma delta\n")
	right := gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.SecondaryButton)}
	a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(0, 0)})
	a.vm.SelectionsChanged()
	a.pump()
	// a right click inside the text, on the second line
	at := a.ts.TopLeft(gluttertest.ByType[*kit.ContextMenuRegion]())
	size := a.ts.Size(gluttertest.ByType[*kit.ContextMenuRegion]())
	a.ts.TapAt(ui.Offset{Dx: at.Dx + 120, Dy: at.Dy + size.Height/3}, right)
	a.pump()
	items := a.openMenuItems(t)
	var got []string
	for _, it := range items {
		got = append(got, it.Label)
	}
	if want := []string{"Cut", "Copy", "Paste", "", "Select All"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("context menu %v, want %v", got, want)
	}
	if it, _ := menuItem(items, "editor.action.clipboardCutAction"); it.Keybinding != "Ctrl+X" {
		t.Errorf("Cut shows %q", it.Keybinding)
	}
	if p := a.selections()[0].Active; p.Line == 0 && p.Col == 0 {
		t.Errorf("the cursor did not go to the click: %v", a.selections())
	}

	// Select All runs on the editor, which has the focus back
	a.ts.Tap(gluttertest.ByText("Select All"))
	a.pump()
	if !a.vm.Focused() {
		t.Error("the editor does not have the focus after the context menu closed")
	}

	if got := a.selections(); len(got) != 1 || got[0] != sel(0, 0, 2, 0) {
		t.Errorf("selections after Select All: %v", got)
	}
	if a.ts.Exists(gluttertest.ByText("Select All")) {
		t.Error("the menu is still open")
	}

	// a read-only document has no Cut or Paste
	a.vm.Document().SetReadOnly(true)
	a.ts.TapAt(ui.Offset{Dx: at.Dx + 120, Dy: at.Dy + size.Height/3}, right)
	a.pump()
	got = nil
	for _, it := range a.openMenuItems(t) {
		got = append(got, it.Label)
	}
	if want := []string{"Copy", "", "Select All"}; !reflect.DeepEqual(got, want) {
		t.Errorf("read-only context menu %v, want %v", got, want)
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestDevOpenMenu(t *testing.T) {
	cases := []struct {
		name string
		args any
		want string // a text of the menu that opens
		not  string // a text of the other menus
	}{
		{"the Edit menu", map[string]any{"id": "MenubarEditMenu"}, "Undo", "Select All"},
		{"the Selection menu as a string", "MenubarSelectionMenu", "Select All", "Undo"},
		{"the View menu", map[string]any{"id": "MenubarViewMenu"}, "Word Wrap", "Undo"},
		{"the Go menu", map[string]any{"id": "MenubarGoMenu"}, "Go to Bracket", "Undo"},
		{"the Manage menu", map[string]any{"id": "GlobalActivity"}, "Command Palette...", "Undo"},
		{"the context menu of the editor", map[string]any{"id": "EditorContext"}, "Paste", "Undo"},
		{"the context menu where it is told", map[string]any{"id": "EditorContext", "x": 300.0, "y": 100.0}, "Paste", "Undo"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := newKeyApp(t, "alpha\n")
			if err := a.devCommand(t, "dev.openMenu", tc.args); err != nil {
				t.Fatal(err)
			}
			if !a.ts.Exists(gluttertest.ByText(tc.want)) {
				t.Errorf("the menu does not show %q", tc.want)
			}
			if a.ts.Exists(gluttertest.ByText(tc.not)) && tc.not != "Undo" {
				t.Errorf("another menu shows %q", tc.not)
			}
			if errs := a.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
	t.Run("the position of a context menu", func(t *testing.T) {
		a := newKeyApp(t, "alpha\n")
		if err := a.devCommand(t, "dev.openMenu", map[string]any{"id": "EditorContext", "x": 300.0, "y": 100.0}); err != nil {
			t.Fatal(err)
		}
		if got := a.ts.TopLeft(gluttertest.ByType[*kit.Menu]()); got.Dx != 300 || got.Dy != 102 {
			t.Errorf("the menu is at %v, at the position it was told", got)
		}
	})
}

func TestParseOpenMenu(t *testing.T) {
	for _, tc := range []struct {
		name string
		args any
		id   string
		at   ui.Offset
	}{
		{"an id", "MenubarEditMenu", "MenubarEditMenu", devOpenMenuDefault},
		{"an object", map[string]any{"id": "EditorContext", "x": 10.0, "y": 20.0}, "EditorContext", ui.Offset{Dx: 10, Dy: 20}},
		{"an object with an id only", map[string]any{"id": "EditorContext"}, "EditorContext", devOpenMenuDefault},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, at, err := parseOpenMenu(tc.args)
			if err != nil || id != tc.id || at != tc.at {
				t.Errorf("got %q %v %v, want %q %v", id, at, err, tc.id, tc.at)
			}
		})
	}
	for _, tc := range []struct {
		name string
		args any
	}{{"nothing", nil}, {"no id", map[string]any{"x": 1.0}}, {"an empty id", ""}, {"a number", 3.0}} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := parseOpenMenu(tc.args)
			appErr, ok := errors.FromError(err)
			if !ok || !appErr.HasRoute("parseOpenMenu.DevOpenMenuInvalid") {
				t.Errorf("error %v", err)
			}
		})
	}
}

func TestDevOpenMenuOfAnUnknownMenu(t *testing.T) {
	a := newKeyApp(t, "alpha\n")
	err := a.devCommand(t, "dev.openMenu", map[string]any{"id": "NoSuchMenu"})
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("devOpenMenu.DevOpenMenuUnknown") {
		t.Errorf("error %v", err)
	}
	// Terminal has no command yet, so it is not in the bar either
	err = a.devCommand(t, "dev.openMenu", map[string]any{"id": "MenubarTerminalMenu"})
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("devOpenMenu.DevOpenMenuUnknown") {
		t.Errorf("Terminal: %v", err)
	}
}

func TestTheActivityBarAndTheSideBarOfTheWindow(t *testing.T) {
	a := newKeyApp(t, "alpha\n")
	icon := gluttertest.ByKey(workbench.ActivityItemKey(command.ViewContainerExplorer))
	if !a.ts.Exists(icon) || !a.ts.Exists(gluttertest.ByText("EXPLORER")) || !a.ts.Exists(gluttertest.ByText("NO FOLDER OPENED")) {
		t.Fatal("the Explorer is not in the activity bar and the side bar")
	}
	a.ts.Tap(icon)
	a.pump()
	if a.win.Layout.State().SideBarVisible || a.win.Keys.Get("sideBarVisible") != false {
		t.Error("a click on the active icon did not hide the side bar")
	}
	a.ts.Tap(icon)
	a.pump()
	if !a.win.Layout.State().SideBarVisible || !a.ts.Exists(gluttertest.ByText("NO FOLDER OPENED")) {
		t.Error("a click on the icon did not show the side bar")
	}
	a.ts.Tap(gluttertest.ByKey(workbench.ViewHeaderKey(command.ViewExplorerEmpty)))
	a.pump()
	if !a.win.Views.IsCollapsed(command.ViewExplorerEmpty) {
		t.Error("a click on the header did not collapse the view")
	}
	if !a.ts.Exists(gluttertest.ByKey(workbench.ManageKey)) {
		t.Error("no Manage button")
	}
}

func TestTheMenuContextFollowsTheSettings(t *testing.T) {
	a := newKeyApp(t, "alpha\n")
	ctx := menuContext{base: a.win.Keys, settings: a.win.Settings, extra: map[string]any{"editorIsOpen": true}}
	cases := []struct {
		key  string
		want any
	}{
		{"config.workbench.statusBar.visible", true},
		{"config.workbench.sideBar.location", "left"},
		{"config.window.menuBarVisibility", "classic"},
		{"config.no.such.setting", nil},
		{"editorIsOpen", true},
		{"noSuchKey", nil},
	}
	for _, tc := range cases {
		if got := ctx.Value(tc.key); got != tc.want {
			t.Errorf("%s = %v (%T), want %v", tc.key, got, got, tc.want)
		}
	}
	// a key of the scope wins over the settings, and the extra ones over both
	a.win.Keys.Set("config.window.menuBarVisibility", "hidden")
	if got := ctx.Value("config.window.menuBarVisibility"); got != "hidden" {
		t.Errorf("got %v", got)
	}
	ctx.extra = map[string]any{"config.window.menuBarVisibility": "visible"}
	if got := ctx.Value("config.window.menuBarVisibility"); got != "visible" {
		t.Errorf("got %v", got)
	}
	var _ contextkey.Context = ctx
}
