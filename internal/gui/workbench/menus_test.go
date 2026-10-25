package workbench

import (
	"reflect"
	"slices"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// menuServices are the registries of the menus over the default commands and the default keymap.
type menuServices struct {
	commands *command.Registry
	menus    *command.MenuRegistry
	keys     *contextkey.Scope
	source   *MenuSource
	ran      []string
	failed   []string
	deferred []func()
}

func newMenuServices(keys *contextkey.Scope) *menuServices {
	m := &menuServices{commands: command.DefaultRegistry(), keys: keys}
	if m.keys == nil {
		m.keys = contextkey.NewRoot()
	}
	m.menus = command.DefaultMenus(m.commands)
	m.source = m.newSource(nil)
	return m
}

func (m *menuServices) newSource(schedule func(func())) *MenuSource {
	resolver := keybinding.NewResolver(keybinding.DefaultRegistry().Defaults(), nil, nil)
	return NewMenuSource(MenuSourceOptions{
		Menus: m.menus, Commands: m.commands, Keybindings: resolver,
		Context:  func() contextkey.Context { return m.keys },
		Schedule: schedule,
		OnError:  func(id string, err error) { m.failed = append(m.failed, id) },
	})
}

// handle gives commands handlers that record that they ran.
func (m *menuServices) handle(t *testing.T, ids ...string) {
	t.Helper()
	for _, id := range ids {
		if _, err := m.commands.SetHandler(id, func(c command.Context, args any) error {
			if c.Source != command.SourceMenu {
				t.Errorf("%s was run by %v, not by a menu", id, c.Source)
			}
			if args != nil {
				m.ran = append(m.ran, id+" "+args.(string))
			} else {
				m.ran = append(m.ran, id)
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
}

// find returns the item with an id in a list, looking into submenus.
func find(items []kit.MenuItem, id string) (kit.MenuItem, bool) {
	for _, it := range items {
		if it.ID == id && !it.Submenu {
			return it, true
		}
		if it.Submenu {
			if sub, ok := find(it.Items, id); ok {
				return sub, true
			}
		}
	}
	return kit.MenuItem{}, false
}

func labels(items []kit.MenuItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		if it.Separator {
			out[i] = "-"
		} else {
			out[i] = it.Label
		}
	}
	return out
}

func TestBarMenusAreTheSubmenusOfTheMainMenu(t *testing.T) {
	m := newMenuServices(nil)
	var got []string
	for _, menu := range m.source.BarMenus() {
		got = append(got, menu.Label)
		if menu.Source == nil || len(menu.Items) == 0 {
			t.Errorf("%s: items %d, source %v", menu.Label, len(menu.Items), menu.Source != nil)
		}
	}
	// Terminal and Help have no command yet and are not in the bar
	if want := []string{"&File", "&Edit", "&Selection", "&View", "&Go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("menus %v, want %v", got, want)
	}
}

func TestMenuItemsShowLabelsAndKeys(t *testing.T) {
	m := newMenuServices(nil)
	edit := m.source.Items(command.MenubarEditMenu, nil)
	wantEdit := []string{"&Undo", "&Redo", "-", "Cu&&t", "&Copy", "&Paste", "-", "&Find", "&Replace", "-",
		"&Toggle Line Comment", "Toggle &Block Comment", "Emmet: E&&xpand Abbreviation"}
	// the mnemonic of the menu widgets is a single ampersand
	wantEdit[3] = "Cu&t"
	wantEdit[12] = "Emmet: E&xpand Abbreviation"
	if got := labels(edit); !reflect.DeepEqual(got, wantEdit) {
		t.Errorf("Edit\n got %v\nwant %v", got, wantEdit)
	}

	cases := []struct{ command, key string }{
		{"undo", "Ctrl+Z"},
		{"redo", "Ctrl+Y"},
		{"editor.action.clipboardCutAction", "Ctrl+X"},
		{"editor.action.clipboardCopyAction", "Ctrl+C"},
		{"editor.action.clipboardPasteAction", "Ctrl+V"},
		{"actions.find", "Ctrl+F"},
		{"editor.action.startFindReplaceAction", "Ctrl+H"},
		{"editor.action.commentLine", "Ctrl+/"},
		{"editor.action.blockComment", "Ctrl+Shift+A"},
		{"editor.emmet.action.expandAbbreviation", "Tab"},
	}
	for _, tc := range cases {
		it, ok := find(edit, tc.command)
		if !ok {
			t.Errorf("%s is not in the Edit menu", tc.command)
			continue
		}
		if it.Keybinding != tc.key {
			t.Errorf("%s shows %q, want %q", tc.command, it.Keybinding, tc.key)
		}
	}

	sel := m.source.Items(command.MenubarSelectionMenu, nil)
	for _, tc := range []struct{ command, key string }{
		{"editor.action.selectAll", "Ctrl+A"},
		{"editor.action.smartSelect.expand", "Shift+Alt+RightArrow"},
		{"editor.action.copyLinesUpAction", "Ctrl+Shift+Alt+UpArrow"},
		{"editor.action.moveLinesDownAction", "Alt+DownArrow"},
		{"editor.action.insertCursorAbove", "Shift+Alt+UpArrow"},
		{"editor.action.addSelectionToNextFindMatch", "Ctrl+D"},
	} {
		if it, ok := find(sel, tc.command); !ok || it.Keybinding != tc.key {
			t.Errorf("%s shows %q (found %v), want %q", tc.command, it.Keybinding, ok, tc.key)
		}
	}

	// the View menu has the Appearance submenu with the layout toggles and their keys
	view := m.source.Items(command.MenubarViewMenu, nil)
	// the command palette and Open View come first, in a group of their own
	if len(view) < 7 || view[0].Label != "&Command Palette..." || view[1].Label != "&Open View..." || !view[2].Separator ||
		!view[3].Submenu || view[3].Label != "&Appearance" || !view[4].Submenu || view[4].Label != "Editor &Layout" || !view[5].Submenu || view[5].Label != "Workspace &Tabs" || !view[6].Separator {
		t.Fatalf("View: %v", labels(view))
	}
	if it, ok := find(view, "workbench.action.showCommands"); !ok || it.Keybinding != "Ctrl+Shift+P" {
		t.Errorf("Command Palette: %+v", it)
	}
	if it, ok := find(view, "workbench.action.toggleSidebarVisibility"); !ok || it.Keybinding != "Ctrl+B" || it.Label != "&Primary Side Bar" {
		t.Errorf("Primary Side Bar: %+v", it)
	}
	if it, ok := find(view, "workbench.action.togglePanel"); !ok || it.Keybinding != "Ctrl+J" || it.Label != "&Panel" {
		t.Errorf("Panel: %+v", it)
	}
	if it, ok := find(view, "editor.action.toggleWordWrap"); !ok || it.Keybinding != "Alt+Z" || it.Label != "&Word Wrap" {
		t.Errorf("Word Wrap: %+v", it)
	}
}

func TestMenuItemsShowTheStateOfTheWindow(t *testing.T) {
	keys := contextkey.NewRoot()
	m := newMenuServices(keys)
	state := func(id string) kit.MenuItem {
		t.Helper()
		it, ok := find(m.source.Items(command.MenubarViewMenu, nil), id)
		if !ok {
			t.Fatalf("%s is not in the View menu", id)
		}
		return it
	}
	// nothing is set yet: the toggles are off and Word Wrap cannot be toggled
	if it := state("workbench.action.toggleSidebarVisibility"); it.Checked {
		t.Error("Primary Side Bar is checked before the side bar is known to be visible")
	}
	if it := state("editor.action.toggleWordWrap"); !it.Disabled || it.Checked {
		t.Errorf("Word Wrap: %+v", it)
	}

	keys.Batch(func() {
		keys.Set("sideBarVisible", true)
		keys.Set("panelVisible", true)
		keys.Set("panelPosition", "bottom")
		keys.Set("canToggleWordWrap", true)
		keys.Set("editorWordWrap", true)
		keys.Set("config.workbench.statusBar.visible", true)
		keys.Set("config.window.menuBarVisibility", "classic")
	})
	for _, id := range []string{"workbench.action.toggleSidebarVisibility", "workbench.action.togglePanel", "editor.action.toggleWordWrap",
		"workbench.action.toggleStatusbarVisibility", "workbench.action.toggleMenuBar", "workbench.action.positionPanelBottom"} {
		if it := state(id); !it.Checked || it.Disabled {
			t.Errorf("%s: %+v", id, it)
		}
	}
	if it := state("workbench.action.positionPanelTop"); it.Checked {
		t.Errorf("Top is checked while the panel is at the bottom: %+v", it)
	}
	keys.Set("panelPosition", "left")
	if state("workbench.action.positionPanelBottom").Checked || !state("workbench.action.positionPanelLeft").Checked {
		t.Error("the Panel Position items follow the panel")
	}
}

func TestMenuItemsAreDisabledByThePrecondition(t *testing.T) {
	keys := contextkey.NewRoot()
	m := newMenuServices(keys)
	editable := func() kit.MenuItem {
		it, _ := find(m.source.Items(command.MenubarEditMenu, nil), "editor.action.commentLine")
		return it
	}
	if editable().Disabled {
		t.Error("Toggle Line Comment is dimmed in a writable editor")
	}
	keys.Set("editorReadonly", true)
	if !editable().Disabled {
		t.Error("Toggle Line Comment is not dimmed in a read-only editor")
	}
	// a disabled item is still in the menu
	if got := len(m.source.Items(command.MenubarEditMenu, nil)); got != 13 {
		t.Errorf("the Edit menu has %d items", got)
	}
}

func TestEditorContextMenuItems(t *testing.T) {
	m := newMenuServices(nil)
	writable := contextkey.MapContext{}
	readOnly := contextkey.MapContext{"editorReadonly": true}
	if got, want := labels(m.source.Items(command.EditorContext, writable)), []string{"Cut", "Copy", "Paste", "-", "Select All"}; !reflect.DeepEqual(got, want) {
		t.Errorf("context menu %v, want %v", got, want)
	}
	if got, want := labels(m.source.Items(command.EditorContext, readOnly)), []string{"Copy", "-", "Select All"}; !reflect.DeepEqual(got, want) {
		t.Errorf("read-only context menu %v, want %v", got, want)
	}
	cut, _ := find(m.source.Items(command.EditorContext, writable), "editor.action.clipboardCutAction")
	if cut.Keybinding != "Ctrl+X" {
		t.Errorf("Cut shows %q", cut.Keybinding)
	}
}

func TestManageItemsWithNothingRegistered(t *testing.T) {
	m := newMenuServices(nil)
	// the command palette, the profiles, the settings, the keyboard shortcuts and the themes are the items of the Manage menu that the
	// built-in rows have (the rows of the configuration follow a line, as VS Code's group of configuration does)
	items := m.source.ManageItems()
	if len(items) != 6 || items[0].Label != "Command Palette..." || items[0].Keybinding != "Ctrl+Shift+P" || !items[1].Separator || items[3].Label != "Settings" || items[3].Keybinding != "Ctrl+," || items[4].Label != "Keyboard Shortcuts" || items[4].Keybinding != "Ctrl+K Ctrl+S" {
		t.Errorf("the Manage menu: %+v", items)
	}
	// the Profiles submenu follows the line (step 8.6b): the six commands, the line between the groups
	if profiles := items[2]; profiles.Label != "Profiles" || !profiles.Submenu || len(profiles.Items) != 7 ||
		profiles.Items[0].ID != "workbench.profiles.actions.createProfile" || profiles.Items[0].Label != "Create Profile..." ||
		profiles.Items[3].ID != "workbench.profiles.actions.deleteProfile" || !profiles.Items[4].Separator ||
		profiles.Items[6].ID != "workbench.profiles.actions.importProfile" || profiles.Items[6].Keybinding != "" {
		t.Errorf("the Profiles submenu: %+v", items[2])
	}
	// the Themes submenu follows (step 8.4)
	if themes := items[5]; themes.Label != "Themes" || !themes.Submenu || len(themes.Items) != 2 ||
		themes.Items[0].ID != "workbench.action.selectTheme" || themes.Items[0].Label != "Color Theme" || themes.Items[0].Keybinding != "Ctrl+K Ctrl+T" ||
		themes.Items[1].ID != "workbench.action.selectIconTheme" || themes.Items[1].Label != "File Icon Theme" || themes.Items[1].Keybinding != "" {
		t.Errorf("the Themes submenu: %+v", items[5])
	}
	m.menus = command.NewMenuRegistry(m.commands)
	m.source = m.newSource(nil)
	items = m.source.ManageItems()
	if len(items) != 1 || !items[0].Disabled || items[0].Label != "(empty)" {
		t.Errorf("an empty Manage menu: %+v", items)
	}
	m.menus.MustAdd(command.MenuItem{Menu: command.GlobalActivity, Command: "workbench.action.togglePanel", Title: "Panel", Group: "2_configuration", Order: 1})
	if got := labels(m.source.ManageItems()); !reflect.DeepEqual(got, []string{"Panel"}) {
		t.Errorf("Manage menu %v", got)
	}
}

func TestMenuSourceRunsTheCommand(t *testing.T) {
	m := newMenuServices(nil)
	m.handle(t, "undo", "editor.action.insertSnippet")
	items := m.source.Items(command.MenubarEditMenu, nil)
	undo, _ := find(items, "undo")
	m.source.Run(undo)
	m.source.Run(kit.MenuItem{ID: "editor.action.insertSnippet", Data: "arg"})
	m.source.Run(kit.SeparatorItem())
	m.source.Run(kit.MenuItem{Label: "(empty)"})
	m.source.Run(kit.SubmenuItem("Sub", nil))
	if want := []string{"undo", "editor.action.insertSnippet arg"}; !reflect.DeepEqual(m.ran, want) {
		t.Errorf("ran %v, want %v", m.ran, want)
	}

	// a failure is reported, not lost
	m.source.Run(kit.MenuItem{ID: "no.such.command"})
	m.source.Run(kit.MenuItem{ID: "redo"}) // registered, but nothing handles it
	if want := []string{"no.such.command", "redo"}; !reflect.DeepEqual(m.failed, want) {
		t.Errorf("failed %v, want %v", m.failed, want)
	}

	// Schedule defers the run until the focus is back
	m.ran = nil
	deferred := m.newSource(func(f func()) { m.deferred = append(m.deferred, f) })
	deferred.Run(undo)
	if len(m.ran) != 0 || len(m.deferred) != 1 {
		t.Fatalf("ran %v, deferred %d", m.ran, len(m.deferred))
	}
	m.deferred[0]()
	if !slices.Equal(m.ran, []string{"undo"}) {
		t.Errorf("ran %v", m.ran)
	}
}

func TestMenuSourceWithoutKeysShowsNone(t *testing.T) {
	m := newMenuServices(nil)
	src := NewMenuSource(MenuSourceOptions{Menus: m.menus, Commands: m.commands})
	undo, ok := find(src.Items(command.MenubarEditMenu, nil), "undo")
	if !ok || undo.Keybinding != "" {
		t.Errorf("%+v %v", undo, ok)
	}
}

// the route of the failure of a command run by a menu is the one of the registry
func TestMenuRunErrorKeepsItsTag(t *testing.T) {
	m := newMenuServices(nil)
	var got error
	src := NewMenuSource(MenuSourceOptions{Menus: m.menus, Commands: m.commands, OnError: func(_ string, err error) { got = err }})
	src.Run(kit.MenuItem{ID: "no.such.command"})
	appErr, ok := errors.FromError(got)
	if !ok || !appErr.HasRoute("Run.CommandNotFound") {
		t.Errorf("error %v", got)
	}
}

// --- the menu bar on screen

// openBar returns a harness with the menu bar of the default menus.
func newMenuHarness(t *testing.T, m *menuServices, tune func(*Options)) *harness {
	t.Helper()
	ctl := kit.NewMenuBarController()
	// the titles of the classic menu bar are what the tests tap; the compact menu has its own test
	h := newHarness(t, testSize, kit.DarkModern(), DefaultLayoutState(), map[string]any{"window.menuBarVisibility": "classic"}, func(o *Options) {
		o.MenuBar = ctl
		m.commands = o.Commands
		m.menus = command.DefaultMenus(o.Commands)
		m.keys = o.Keys
		m.source = m.newSource(nil)
		o.Menus = m.source.BarMenus()
		o.OnRunMenu = m.source.Run
		o.ManageItems = m.source.ManageItems
		if tune != nil {
			tune(o)
		}
	})
	h.menuBar = ctl
	return h
}

func TestMenuBarShowsTheMenusAndRunsAnItem(t *testing.T) {
	m := &menuServices{}
	h := newMenuHarness(t, m, nil)
	m.handle(t, "undo", "editor.action.clipboardCopyAction")
	for _, title := range []string{"File", "Edit", "Selection", "View", "Go"} {
		if !h.ts.Exists(gluttertest.ByText(title)) {
			t.Errorf("no %s in the menu bar", title)
		}
	}
	for _, title := range []string{"Help", "Terminal"} {
		if h.ts.Exists(gluttertest.ByText(title)) {
			t.Errorf("%s is in the menu bar with nothing in it", title)
		}
	}

	h.ts.Tap(gluttertest.ByText("Edit"))
	h.settle()
	for _, text := range []string{"Undo", "Redo", "Cut", "Copy", "Paste", "Find", "Replace", "Toggle Line Comment", "Ctrl+Z", "Ctrl+Y", "Ctrl+X", "Ctrl+V", "Ctrl+F"} {
		if !h.ts.Exists(gluttertest.ByText(text)) {
			t.Errorf("the Edit menu does not show %q", text)
		}
	}
	h.ts.Tap(gluttertest.ByText("Undo"))
	h.settle()
	if !slices.Equal(m.ran, []string{"undo"}) {
		t.Errorf("ran %v", m.ran)
	}
	if h.ts.Exists(gluttertest.ByText("Redo")) {
		t.Error("the menu is still open after the item ran")
	}
	noErrors(t, h)
}

// openItems are the items of the first menu on screen.
func openItems(t *testing.T, h *harness) []kit.MenuItem {
	t.Helper()
	els := h.ts.Elements(gluttertest.ByType[*kit.Menu]())
	if len(els) == 0 {
		t.Fatal("no menu is open")
	}
	return els[0].Widget().(*kit.Menu).Items()
}

func TestMenuBarMenuFollowsTheStateWhenItOpens(t *testing.T) {
	m := &menuServices{}
	h := newMenuHarness(t, m, nil)
	wrap := func() kit.MenuItem {
		h.openMenu(3) // View
		it, ok := find(openItems(t, h), "editor.action.toggleWordWrap")
		if !ok {
			t.Fatal("no Word Wrap in the View menu")
		}
		h.menuBar.Blur()
		h.settle()
		return it
	}
	if it := wrap(); !it.Disabled || it.Checked {
		t.Errorf("Word Wrap before the editor can wrap: %+v", it)
	}
	// the menu is built when it opens, so a state set after the bar was built shows
	h.keys.Set("canToggleWordWrap", true)
	h.keys.Set("editorWordWrap", true)
	if it := wrap(); it.Disabled || !it.Checked {
		t.Errorf("Word Wrap with wrapping on: %+v", it)
	}
	noErrors(t, h)
}

func TestMenuBarOpensAMenuFromOutside(t *testing.T) {
	m := &menuServices{}
	h := newMenuHarness(t, m, nil)
	h.openMenu(2) // Selection
	if !h.ts.Exists(gluttertest.ByText("Select All")) || h.ts.Exists(gluttertest.ByText("Undo")) {
		t.Error("the Selection menu is not the one open")
	}
	h.openMenu(1) // Edit
	if !h.ts.Exists(gluttertest.ByText("Undo")) || h.ts.Exists(gluttertest.ByText("Select All")) {
		t.Error("the Edit menu is not the one open")
	}
	noErrors(t, h)
}

// openMenu opens the menu at an index of the menu bar the way a click on its title does.
func (h *harness) openMenu(i int) {
	h.t.Helper()
	if h.menuBar == nil {
		h.t.Fatal("the harness has no menu bar controller")
	}
	h.menuBar.Open(i)
	h.settle()
}
