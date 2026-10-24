package command

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// testMenus is a registry of commands a, b, c, d with titles, and of menus over it.
func testMenus() (*Registry, *MenuRegistry) {
	cmds := NewRegistry()
	cmds.MustRegister(
		Meta{ID: "a", Title: "Alpha"},
		Meta{ID: "b", Title: "Beta"},
		Meta{ID: "c", Title: "Gamma", Precondition: contextkey.Has("canGamma")},
		Meta{ID: "d", Title: "Delta"},
		Meta{ID: "untitled"},
	)
	return cmds, NewMenuRegistry(cmds)
}

// shape is the commands and separators of entries, for short expectations.
func shape(entries []MenuEntry) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		switch {
		case e.Separator:
			out = append(out, "-")
		case e.Submenu != "":
			out = append(out, "> "+e.Submenu)
		default:
			out = append(out, e.Command)
		}
	}
	return out
}

func TestMenuItemsOrderAndGroups(t *testing.T) {
	cases := []struct {
		name  string
		items []MenuItem
		want  []string
	}{
		{"order within a group", []MenuItem{
			{Menu: "m", Command: "a", Group: "1_x", Order: 3},
			{Menu: "m", Command: "b", Group: "1_x", Order: 1},
			{Menu: "m", Command: "d", Group: "1_x", Order: 2},
		}, []string{"b", "d", "a"}},
		{"groups by name with a separator between", []MenuItem{
			{Menu: "m", Command: "a", Group: "2_b", Order: 1},
			{Menu: "m", Command: "b", Group: "1_a", Order: 5},
			{Menu: "m", Command: "d", Group: "3_c", Order: 1},
		}, []string{"b", "-", "a", "-", "d"}},
		{"navigation first and no group last", []MenuItem{
			{Menu: "m", Command: "a"},
			{Menu: "m", Command: "b", Group: "1_x"},
			{Menu: "m", Command: "d", Group: "navigation"},
		}, []string{"d", "-", "b", "-", "a"}},
		{"an equal order sorts by title", []MenuItem{
			{Menu: "m", Command: "d", Group: "g"},
			{Menu: "m", Command: "a", Group: "g"},
			{Menu: "m", Command: "b", Group: "g"},
		}, []string{"a", "b", "d"}},
		{"fractional orders", []MenuItem{
			{Menu: "m", Command: "a", Group: "g", Order: 1.45},
			{Menu: "m", Command: "b", Group: "g", Order: 1.1},
			{Menu: "m", Command: "d", Group: "g", Order: -1},
		}, []string{"d", "b", "a"}},
		{"an item of another menu is not here", []MenuItem{
			{Menu: "m", Command: "a", Group: "g"},
			{Menu: "other", Command: "b", Group: "g"},
		}, []string{"a"}},
		{"a command that is not registered, and an item with no title, are left out", []MenuItem{
			{Menu: "m", Command: "a", Group: "g", Order: 1},
			{Menu: "m", Command: "missing", Group: "g", Order: 2},
			{Menu: "m", Command: "untitled", Group: "g", Order: 3},
			{Menu: "m", Command: "untitled", Title: "Titled here", Group: "g", Order: 4},
		}, []string{"a", "untitled"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, r := testMenus()
			r.MustAdd(tc.items...)
			if got := shape(r.Items("m", contextkey.MapContext{})); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMenuItemsFilteredByContext(t *testing.T) {
	_, r := testMenus()
	r.MustAdd(
		MenuItem{Menu: "m", Command: "a", Group: "1_x", Order: 1},
		MenuItem{Menu: "m", Command: "b", Group: "1_x", Order: 2, When: contextkey.Has("editorTextFocus")},
		MenuItem{Menu: "m", Command: "d", Group: "2_y", Order: 1, When: contextkey.Equals("resourceScheme", "file")},
		MenuItem{Menu: "m", Command: "c", Group: "3_z", Order: 1, When: contextkey.Not("editorReadonly")},
	)
	cases := []struct {
		name string
		ctx  contextkey.MapContext
		want []string
	}{
		{"nothing set", contextkey.MapContext{}, []string{"a", "-", "c"}},
		{"editor focus", contextkey.MapContext{"editorTextFocus": true}, []string{"a", "b", "-", "c"}},
		{"a file", contextkey.MapContext{"resourceScheme": "file"}, []string{"a", "-", "d", "-", "c"}},
		{"read only leaves the last group out: no separator is left at the end", contextkey.MapContext{"editorReadonly": true}, []string{"a"}},
		{"everything", contextkey.MapContext{"editorTextFocus": true, "resourceScheme": "file"}, []string{"a", "b", "-", "d", "-", "c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shape(r.Items("m", tc.ctx)); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMenuEntryStates(t *testing.T) {
	_, r := testMenus()
	r.MustAdd(
		MenuItem{Menu: "m", Command: "c", Order: 1}, // the precondition of the command
		MenuItem{Menu: "m", Command: "a", Order: 2, Precondition: contextkey.Has("canAlpha")},
		MenuItem{Menu: "m", Command: "b", Order: 3, Title: "Side Bar", MnemonicTitle: "&&Side Bar", Toggled: contextkey.Has("on"),
			ToggledTitle: "Hide Side Bar", ToggledMnemonicTitle: "Hide &&Side Bar"},
		MenuItem{Menu: "m", Command: "d", Order: 4, MnemonicTitle: "D&&elta", Toggled: contextkey.Has("on")},
	)
	cases := []struct {
		name string
		ctx  contextkey.MapContext
		want []MenuEntry
	}{
		{"off", contextkey.MapContext{}, []MenuEntry{
			{Command: "c", Title: "Gamma", MnemonicTitle: "Gamma", Disabled: true},
			{Command: "a", Title: "Alpha", MnemonicTitle: "Alpha", Disabled: true},
			{Command: "b", Title: "Side Bar", MnemonicTitle: "Hide &&Side Bar"},
			{Command: "d", Title: "Delta", MnemonicTitle: "D&&elta"},
		}},
		{"on", contextkey.MapContext{"canGamma": true, "canAlpha": true, "on": true}, []MenuEntry{
			{Command: "c", Title: "Gamma", MnemonicTitle: "Gamma"},
			{Command: "a", Title: "Alpha", MnemonicTitle: "Alpha"},
			{Command: "b", Title: "Hide Side Bar", MnemonicTitle: "Hide &&Side Bar", Checked: true},
			{Command: "d", Title: "Delta", MnemonicTitle: "D&&elta", Checked: true},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := r.Items("m", tc.ctx); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v\nwant %+v", got, tc.want)
			}
		})
	}
}

func TestMenuSubmenus(t *testing.T) {
	cases := []struct {
		name  string
		items []MenuItem
		ctx   contextkey.MapContext
		want  []string
		sub   []string
	}{
		{"a submenu holds the entries of another menu", []MenuItem{
			{Menu: "m", Submenu: "s", Title: "Sub", MnemonicTitle: "&&Sub", Group: "1_x"},
			{Menu: "s", Command: "a", Group: "1_a", Order: 1},
			{Menu: "s", Command: "b", Group: "2_b", Order: 1},
		}, nil, []string{"> s"}, []string{"a", "-", "b"}},
		{"a submenu with nothing in it is left out", []MenuItem{
			{Menu: "m", Submenu: "s", Title: "Sub"},
			{Menu: "m", Command: "a"},
		}, nil, []string{"a"}, nil},
		{"a submenu whose items are all filtered out is left out", []MenuItem{
			{Menu: "m", Submenu: "s", Title: "Sub", Group: "1_x"},
			{Menu: "m", Command: "a", Group: "2_y"},
			{Menu: "s", Command: "b", When: contextkey.Has("on")},
		}, contextkey.MapContext{}, []string{"a"}, nil},
		{"a submenu is shown when one of its items is", []MenuItem{
			{Menu: "m", Submenu: "s", Title: "Sub", Group: "1_x"},
			{Menu: "m", Command: "a", Group: "2_y"},
			{Menu: "s", Command: "b", When: contextkey.Has("on")},
		}, contextkey.MapContext{"on": true}, []string{"> s", "-", "a"}, []string{"b"}},
		{"a submenu with no title is left out", []MenuItem{
			{Menu: "m", Submenu: "s"},
			{Menu: "s", Command: "b"},
		}, nil, nil, nil},
		{"a menu that holds itself ends", []MenuItem{
			{Menu: "m", Submenu: "m", Title: "Loop"},
			{Menu: "m", Command: "a"},
		}, nil, []string{"a"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, r := testMenus()
			r.MustAdd(tc.items...)
			ctx := tc.ctx
			if ctx == nil {
				ctx = contextkey.MapContext{}
			}
			got := r.Items("m", ctx)
			if s := shape(got); !reflect.DeepEqual(s, tc.want) && (len(s) != 0 || len(tc.want) != 0) {
				t.Fatalf("got %v, want %v", s, tc.want)
			}
			if tc.sub != nil {
				i := slices.IndexFunc(got, func(e MenuEntry) bool { return e.Submenu == "s" })
				if i < 0 {
					t.Fatal("no submenu entry")
				}
				if s := shape(got[i].Items); !reflect.DeepEqual(s, tc.sub) {
					t.Errorf("submenu got %v, want %v", s, tc.sub)
				}
				if got[i].Title != "Sub" {
					t.Errorf("submenu title %q", got[i].Title)
				}
			}
		})
	}
}

func TestMenuDynamicSubmenu(t *testing.T) {
	_, r := testMenus()
	recent := []string{"one.go", "two.go"}
	r.MustAdd(
		MenuItem{Menu: "file", Submenu: "recent", Title: "Open Recent", MnemonicTitle: "Open &&Recent", Group: "2_open", Order: 4},
		MenuItem{Menu: "recent", Command: "a", Title: "Reopen Closed Editor", Group: "1_reopen", Order: 1},
		MenuItem{Menu: "recent", Command: "b", Title: "Clear Recently Opened", Group: "z_clear", Order: 1},
	)
	r.SetProvider("recent", func(ctx contextkey.Context) []MenuItem {
		var items []MenuItem
		for i, name := range recent {
			items = append(items, MenuItem{Menu: "recent", Command: "d", Title: name, Group: "2_files", Order: float64(i), Args: name})
		}
		return items
	})

	entries := r.Items("file", contextkey.MapContext{})
	if len(entries) != 1 || entries[0].Submenu != "recent" {
		t.Fatalf("file menu: %+v", entries)
	}
	sub := entries[0].Items
	titles := func(es []MenuEntry) []string {
		var out []string
		for _, e := range es {
			if e.Separator {
				out = append(out, "-")
			} else {
				out = append(out, e.Title)
			}
		}
		return out
	}
	want := []string{"Reopen Closed Editor", "-", "one.go", "two.go", "-", "Clear Recently Opened"}
	if got := titles(sub); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if sub[2].Command != "d" || sub[2].Args != "one.go" {
		t.Errorf("a dynamic entry runs its command with its argument: %+v", sub[2])
	}

	// the provider is asked again each time, so the menu follows the state of the program
	recent = nil
	if got := titles(r.Items("file", contextkey.MapContext{})[0].Items); !reflect.DeepEqual(got, []string{"Reopen Closed Editor", "-", "Clear Recently Opened"}) {
		t.Errorf("without recent files: %v", got)
	}
	// and a menu with nothing but a provider that gives nothing is empty
	r.SetProvider("only", func(contextkey.Context) []MenuItem { return nil })
	if got := r.Items("only", contextkey.MapContext{}); len(got) != 0 {
		t.Errorf("%+v", got)
	}
}

func TestMenuAddErrors(t *testing.T) {
	_, r := testMenus()
	cases := []struct {
		name  string
		item  MenuItem
		route string
	}{
		{"no menu", MenuItem{Command: "a"}, "Add.MenuItemMenuMissing"},
		{"no target", MenuItem{Menu: "m"}, "Add.MenuItemTargetInvalid"},
		{"both targets", MenuItem{Menu: "m", Command: "a", Submenu: "s"}, "Add.MenuItemTargetInvalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertRoute(t, r.Add(tc.item), tc.route)
		})
	}
	if got := r.Items("m", contextkey.MapContext{}); len(got) != 0 {
		t.Errorf("a failed Add left items: %+v", got)
	}
	defer func() {
		if recover() == nil {
			t.Error("MustAdd must panic on an invalid item")
		}
	}()
	r.MustAdd(MenuItem{Menu: "m"})
}

func TestMnemonicMenuLabel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"&&File", "&File"},
		{"Cu&&t", "Cu&t"},
		{"Menu &&Bar", "Menu &Bar"},
		{"Open &&Recent", "Open &Recent"},
		{"Q&A", "Q&&A"},
		{"Save && Quit", "Save & Quit"},
		{"No mnemonic", "No mnemonic"},
		{"", ""},
		{"Trailing &", "Trailing &&"},
	}
	for _, tc := range cases {
		if got := MnemonicMenuLabel(tc.in); got != tc.want {
			t.Errorf("MnemonicMenuLabel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestDefaultMenusRefertoCommands checks the table of the built-in rows: every row has a command that
// is registered (or a submenu), every item shows with a title, and a submenu row has a title.
func TestDefaultMenusRefertoCommands(t *testing.T) {
	cmds := DefaultRegistry()
	menus := DefaultMenus(cmds)
	all := menus.All()
	if len(all) == 0 {
		t.Fatal("no rows")
	}
	for _, it := range all {
		switch {
		case it.Command != "":
			meta, ok := cmds.Lookup(it.Command)
			if !ok {
				t.Errorf("%s: the command %s is not registered", it.Menu, it.Command)
				continue
			}
			if it.Title == "" && meta.Title == "" {
				t.Errorf("%s: %s has no title", it.Menu, it.Command)
			}
			if it.MnemonicTitle != "" && it.Title == "" && !equalWithoutMnemonic(it.MnemonicTitle, meta.Title) {
				t.Errorf("%s: %s has the title %q, the mnemonic title is %q", it.Menu, it.Command, meta.Title, it.MnemonicTitle)
			}
		case it.Submenu != "":
			if it.Title == "" {
				t.Errorf("%s: the submenu %s has no title", it.Menu, it.Submenu)
			}
		}
		if it.Menu == "" {
			t.Errorf("a row has no menu: %+v", it)
		}
	}
}

// equalWithoutMnemonic tells whether a title with its mnemonic marks taken out is the other one.
func equalWithoutMnemonic(mnemonic, title string) bool {
	out := make([]byte, 0, len(mnemonic))
	for i := 0; i < len(mnemonic); i++ {
		if mnemonic[i] == '&' && i+1 < len(mnemonic) && mnemonic[i+1] == '&' {
			i++
			continue
		}
		out = append(out, mnemonic[i])
	}
	return string(out) == title
}

func TestDefaultFileMenu(t *testing.T) {
	menus := DefaultMenus(DefaultRegistry())
	titles := func(ctx contextkey.MapContext) (rows []string) {
		for _, it := range menus.Items(MenubarFileMenu, ctx) {
			switch {
			case it.Separator:
				rows = append(rows, "-")
			case it.Disabled:
				rows = append(rows, it.MnemonicTitle+"~")
			default:
				rows = append(rows, it.MnemonicTitle)
			}
		}
		return rows
	}
	for _, tc := range []struct {
		name string
		ctx  contextkey.MapContext
		want []string
	}{
		// Save, Save As, Save All, Revert File and Close Editor are there with no editor open, dimmed
		{"no folder, no editor", contextkey.MapContext{"workbenchState": "empty"}, []string{"&&New Text File", "New &&Window", "-", "Open &&Folder...", "Open &&Recent", "-",
			"&&Save~", "Save &&As...~", "Save A&&ll~", "-", "A&&uto Save", "&&Preferences", "-", "Re&&vert File~", "&&Close Editor~", "Clos&&e Window", "-", "E&&xit"}},
		{"no folder, an editor", contextkey.MapContext{"workbenchState": "empty", "activeEditor": "emetos.textEditor", "activeEditorCanRevert": true}, []string{"&&New Text File", "New &&Window", "-", "Open &&Folder...", "Open &&Recent", "-",
			"&&Save", "Save &&As...", "Save A&&ll~", "-", "A&&uto Save", "&&Preferences", "-", "Re&&vert File", "&&Close Editor", "Clos&&e Window", "-", "E&&xit"}},
		{"an untitled editor cannot revert", contextkey.MapContext{"workbenchState": "empty", "activeEditor": "emetos.textEditor", "activeEditorCanRevert": false, "dirtyWorkingCopies": true}, []string{"&&New Text File", "New &&Window", "-", "Open &&Folder...", "Open &&Recent", "-",
			"&&Save", "Save &&As...", "Save A&&ll", "-", "A&&uto Save", "&&Preferences", "-", "Re&&vert File~", "&&Close Editor", "Clos&&e Window", "-", "E&&xit"}},
		{"a folder", contextkey.MapContext{"workbenchState": "folder", "activeEditor": "emetos.textEditor", "activeEditorCanRevert": true}, []string{"&&New Text File", "New &&Window", "-", "Open &&Folder...", "Open &&Recent", "-",
			"&&Save", "Save &&As...", "Save A&&ll~", "-", "A&&uto Save", "&&Preferences", "-", "Re&&vert File", "&&Close Editor", "Close &&Folder", "Clos&&e Window", "-", "E&&xit"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := titles(tc.ctx); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("file menu %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAutoSaveRowIsCheckedWhileAutoSaveIsOn(t *testing.T) {
	menus := DefaultMenus(DefaultRegistry())
	for _, tc := range []struct {
		value any
		want  bool
	}{
		{"off", false}, {"afterDelay", true}, {"onFocusChange", true}, {"onWindowChange", true},
	} {
		t.Run(fmt.Sprint(tc.value), func(t *testing.T) {
			found := false
			for _, it := range menus.Items(MenubarFileMenu, contextkey.MapContext{"config.files.autoSave": tc.value}) {
				if it.Command != "workbench.action.toggleAutoSave" {
					continue
				}
				found = true
				if it.Checked != tc.want || it.Disabled || it.MnemonicTitle != "A&&uto Save" || it.Title != "Auto Save" {
					t.Errorf("row %+v, want checked = %v", it, tc.want)
				}
			}
			if !found {
				t.Error("the File menu has no Auto Save row")
			}
		})
	}
	meta, ok := DefaultRegistry().Lookup("workbench.action.toggleAutoSave")
	if !ok || meta.Title != "Toggle Auto Save" || meta.Category != "File" {
		t.Errorf("command %+v (registered: %v)", meta, ok)
	}
}

func TestDefaultMenuBar(t *testing.T) {
	menus := DefaultMenus(DefaultRegistry())
	ctx := contextkey.MapContext{}
	var bar []string
	for _, e := range menus.Items(MenubarMainMenu, ctx) {
		bar = append(bar, e.MnemonicTitle)
	}
	// the menus with items so far; Terminal and Help come with their first command
	if want := []string{"&&File", "&&Edit", "&&Selection", "&&View", "&&Go"}; !reflect.DeepEqual(bar, want) {
		t.Errorf("menu bar %v, want %v", bar, want)
	}

	edit := menus.Items(MenubarEditMenu, ctx)
	var got []string
	for _, e := range edit {
		if e.Separator {
			got = append(got, "-")
		} else {
			got = append(got, e.MnemonicTitle)
		}
	}
	want := []string{"&&Undo", "&&Redo", "-", "Cu&&t", "&&Copy", "&&Paste", "-", "&&Find", "&&Replace", "-",
		"&&Toggle Line Comment", "Toggle &&Block Comment", "Emmet: E&&xpand Abbreviation"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Edit menu\n got %v\nwant %v", got, want)
	}
}

func TestDefaultMenuContexts(t *testing.T) {
	menus := DefaultMenus(DefaultRegistry())
	titles := func(menu string, ctx contextkey.MapContext) []string {
		var out []string
		for _, e := range menus.Items(menu, ctx) {
			if e.Separator {
				out = append(out, "-")
			} else {
				out = append(out, e.Title)
			}
		}
		return out
	}
	cases := []struct {
		name string
		menu string
		ctx  contextkey.MapContext
		want []string
	}{
		{"editor context menu", EditorContext, contextkey.MapContext{}, []string{"Cut", "Copy", "Paste", "-", "Select All"}},
		{"editor context menu of a read-only editor has no Cut or Paste", EditorContext, contextkey.MapContext{"editorReadonly": true}, []string{"Copy", "-", "Select All"}},
		{"panel position", PanelPositionMenu, contextkey.MapContext{"panelPosition": "bottom"}, []string{"Top", "Left", "Right", "Bottom"}},
		{"the Manage menu has the command palette", GlobalActivity, contextkey.MapContext{}, []string{"Command Palette...", "-", "Settings", "Keyboard Shortcuts", "Themes"}},
		{"Go", MenubarGoMenu, contextkey.MapContext{}, []string{"Back", "Forward", "Last Edit Location", "-", "Switch Editor", "Switch Group", "-", "Go to File...", "-", "Go to Line/Column...", "Go to Bracket"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := titles(tc.menu, tc.ctx)
			if len(got) == 0 && len(tc.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}

	// the toggles follow the context
	checked := func(menu, command string, ctx contextkey.MapContext) (bool, bool) {
		for _, e := range menus.Items(menu, ctx) {
			if e.Command == command {
				return e.Checked, true
			}
			for _, s := range e.Items {
				if s.Command == command {
					return s.Checked, true
				}
			}
		}
		return false, false
	}
	for _, tc := range []struct {
		command string
		ctx     contextkey.MapContext
		want    bool
	}{
		{"workbench.action.toggleSidebarVisibility", contextkey.MapContext{"sideBarVisible": true}, true},
		{"workbench.action.toggleSidebarVisibility", contextkey.MapContext{}, false},
		{"workbench.action.togglePanel", contextkey.MapContext{"panelVisible": true}, true},
		{"workbench.action.toggleStatusbarVisibility", contextkey.MapContext{"config.workbench.statusBar.visible": true}, true},
		{"workbench.action.toggleStatusbarVisibility", contextkey.MapContext{"config.workbench.statusBar.visible": false}, false},
		{"workbench.action.toggleMenuBar", contextkey.MapContext{"config.window.menuBarVisibility": "classic"}, true},
		{"workbench.action.toggleMenuBar", contextkey.MapContext{"config.window.menuBarVisibility": "toggle"}, false},
		{"workbench.action.positionPanelLeft", contextkey.MapContext{"panelPosition": "left"}, true},
		{"workbench.action.positionPanelBottom", contextkey.MapContext{"panelPosition": "left"}, false},
	} {
		got, found := checked(MenubarAppearanceMenu, tc.command, tc.ctx)
		if !found {
			got, found = checked(PanelPositionMenu, tc.command, tc.ctx)
		}
		if !found || got != tc.want {
			t.Errorf("%s in %v: checked %v (found %v), want %v", tc.command, tc.ctx, got, found, tc.want)
		}
	}

	// Word Wrap is in the View menu, off until the editor can toggle it
	view := menus.Items(MenubarViewMenu, contextkey.MapContext{})
	var wrap *MenuEntry
	for i := range view {
		if view[i].Command == "editor.action.toggleWordWrap" {
			wrap = &view[i]
		}
	}
	if wrap == nil || !wrap.Disabled || wrap.Checked || wrap.MnemonicTitle != "&&Word Wrap" {
		t.Fatalf("Word Wrap: %+v", wrap)
	}
	on := menus.Items(MenubarViewMenu, contextkey.MapContext{"canToggleWordWrap": true, "editorWordWrap": true})
	for _, e := range on {
		if e.Command == "editor.action.toggleWordWrap" && (e.Disabled || !e.Checked) {
			t.Errorf("Word Wrap with wrapping on: %+v", e)
		}
	}
}

func TestEditorGroupMenus(t *testing.T) {
	menus := DefaultMenus(DefaultRegistry())
	rows := func(menu string, ctx contextkey.MapContext) (titles []string, disabled map[string]bool) {
		disabled = map[string]bool{}
		for _, e := range menus.Items(menu, ctx) {
			if e.Separator {
				titles = append(titles, "-")
				continue
			}
			titles = append(titles, e.Title)
			disabled[e.Title] = e.Disabled
		}
		return titles, disabled
	}
	t.Run("the Editor Layout menu", func(t *testing.T) {
		got, _ := rows(MenubarLayoutMenu, contextkey.MapContext{})
		want := []string{"Split Up", "Split Down", "Split Left", "Split Right", "-",
			"Single", "Two Columns", "Three Columns", "Two Rows", "Three Rows", "Grid (2x2)", "Two Rows Right", "Two Columns Bottom"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})
	t.Run("the View menu has it after Appearance", func(t *testing.T) {
		got, _ := rows(MenubarViewMenu, contextkey.MapContext{})
		// the group of the command palette and Open View comes first
		if len(got) < 5 || got[3] != "Appearance" || got[4] != "Editor Layout" {
			t.Errorf("got %v", got)
		}
	})
	t.Run("the Switch Group menu with one group", func(t *testing.T) {
		got, disabled := rows(MenubarSwitchGroupMenu, contextkey.MapContext{})
		want := []string{"Group 1", "Group 2", "Group 3", "Group 4", "Group 5", "-", "Next Group", "Previous Group", "-", "Group Left", "Group Right", "Group Above", "Group Below"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for _, title := range []string{"Group 1", "Group 2"} {
			if disabled[title] {
				t.Errorf("%s is disabled", title)
			}
		}
		for _, title := range []string{"Group 3", "Next Group", "Previous Group", "Group Left", "Group Below"} {
			if !disabled[title] {
				t.Errorf("%s is enabled with a single group", title)
			}
		}
	})
	t.Run("the Switch Group menu with several groups", func(t *testing.T) {
		_, disabled := rows(MenubarSwitchGroupMenu, contextkey.MapContext{"multipleEditorGroups": true})
		for title, off := range disabled {
			if off {
				t.Errorf("%s is disabled with several groups", title)
			}
		}
	})
}

func TestEditorTitleContextMenu(t *testing.T) {
	menus := DefaultMenus(DefaultRegistry())
	// rows writes the titles of a menu; a row that is dimmed ends with "~", a submenu with ">"
	rows := func(menu string, ctx contextkey.MapContext) []string {
		var out []string
		for _, e := range menus.Items(menu, ctx) {
			switch {
			case e.Separator:
				out = append(out, "-")
			case e.Submenu != "":
				out = append(out, e.Title+">")
			case e.Disabled:
				out = append(out, e.Title+"~")
			default:
				out = append(out, e.Title)
			}
		}
		return out
	}
	base := func(extra contextkey.MapContext) contextkey.MapContext {
		ctx := contextkey.MapContext{
			"editorTabsVisible": true, "isFileSystemResource": true, "groupEditorsCount": 3.0,
			"activeEditorIsNotPreview": true, "config.workbench.editor.enablePreview": true,
		}
		for k, v := range extra {
			ctx[k] = v
		}
		return ctx
	}
	for _, tc := range []struct {
		name string
		ctx  contextkey.MapContext
		want []string
	}{
		{"a kept tab", base(nil), []string{
			"Close", "Close Others", "Close to the Right", "Close Saved", "Close All", "-",
			"Copy Path", "Copy Relative Path", "-",
			"Reveal in Explorer View", "-",
			"Keep Open~", "Pin", "-",
			"Split Right", "Split & Move>"}},
		{"a preview tab can be kept", base(contextkey.MapContext{"activeEditorIsNotPreview": false}), []string{
			"Close", "Close Others", "Close to the Right", "Close Saved", "Close All", "-",
			"Copy Path", "Copy Relative Path", "-",
			"Reveal in Explorer View", "-",
			"Keep Open", "Pin", "-",
			"Split Right", "Split & Move>"}},
		{"a pinned tab can be unpinned", base(contextkey.MapContext{"activeEditorIsPinned": true}), []string{
			"Close", "Close Others", "Close to the Right", "Close Saved", "Close All", "-",
			"Copy Path", "Copy Relative Path", "-",
			"Reveal in Explorer View", "-",
			"Keep Open~", "Unpin", "-",
			"Split Right", "Split & Move>"}},
		{"the last tab has nothing to its right", base(contextkey.MapContext{"activeEditorIsLastInGroup": true}), []string{
			"Close", "Close Others", "Close to the Right~", "Close Saved", "Close All", "-",
			"Copy Path", "Copy Relative Path", "-",
			"Reveal in Explorer View", "-",
			"Keep Open~", "Pin", "-",
			"Split Right", "Split & Move>"}},
		{"the only tab has no others", base(contextkey.MapContext{"groupEditorsCount": 1.0, "activeEditorIsLastInGroup": true}), []string{
			"Close", "Close Others~", "Close to the Right~", "Close Saved", "Close All", "-",
			"Copy Path", "Copy Relative Path", "-",
			"Reveal in Explorer View", "-",
			"Keep Open~", "Pin", "-",
			"Split Right", "Split & Move>"}},
		{"splitting down", base(contextkey.MapContext{"splitEditorsVertically": true}), []string{
			"Close", "Close Others", "Close to the Right", "Close Saved", "Close All", "-",
			"Copy Path", "Copy Relative Path", "-",
			"Reveal in Explorer View", "-",
			"Keep Open~", "Pin", "-",
			"Split Down", "Split & Move>"}},
		{"no preview editors, no Keep Open", base(contextkey.MapContext{"config.workbench.editor.enablePreview": false}), []string{
			"Close", "Close Others", "Close to the Right", "Close Saved", "Close All", "-",
			"Copy Path", "Copy Relative Path", "-",
			"Reveal in Explorer View", "-",
			"Pin", "-",
			"Split Right", "Split & Move>"}},
		{"a tab of something that is not a file", base(contextkey.MapContext{"isFileSystemResource": false}), []string{
			"Close", "Close Others", "Close to the Right", "Close Saved", "Close All", "-",
			"Keep Open~", "Pin", "-",
			"Split Right", "Split & Move>"}},
		{"a dirty tab can be saved", base(contextkey.MapContext{"activeEditorIsDirty": true}), []string{
			"Close", "Close Others", "Close to the Right", "Close Saved", "Close All", "-",
			"Save", "Save As...", "-",
			"Copy Path", "Copy Relative Path", "-",
			"Reveal in Explorer View", "-",
			"Keep Open~", "Pin", "-",
			"Split Right", "Split & Move>"}},
		{"without tabs there is no Close to the Right", base(contextkey.MapContext{"editorTabsVisible": false}), []string{
			"Close", "Close Others", "Close Saved", "Close All", "-",
			"Copy Path", "Copy Relative Path", "-",
			"Reveal in Explorer View", "-",
			"Keep Open~", "Pin", "-",
			"Split Right", "Split & Move>"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := rows(EditorTitleContext, tc.ctx); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %v\nwant %v", got, tc.want)
			}
		})
	}
	t.Run("the Split & Move submenu", func(t *testing.T) {
		want := []string{"Split Up", "Split Down", "Split Left", "Split Right", "-", "Move Above", "Move Below", "Move Left", "Move Right"}
		if got := rows(EditorSplitMoveSubmenu, contextkey.MapContext{}); !reflect.DeepEqual(got, want) {
			t.Errorf("got  %v\nwant %v", got, want)
		}
	})
	t.Run("every row runs a command that exists", func(t *testing.T) {
		commands := DefaultRegistry()
		for _, it := range menus.All() {
			if (it.Menu == EditorTitleContext || it.Menu == EditorSplitMoveSubmenu) && it.Command != "" {
				if _, ok := commands.Lookup(it.Command); !ok {
					t.Errorf("%s: no command %s", it.Menu, it.Command)
				}
			}
		}
	})
}

func TestViewMenuHasThePanelViews(t *testing.T) {
	menus := DefaultMenus(DefaultRegistry())
	var titles []string
	for _, e := range menus.Items(MenubarViewMenu, contextkey.MapContext{}) {
		if e.Separator {
			titles = append(titles, "-")
			continue
		}
		titles = append(titles, e.Title)
	}
	// the panel views come in their own group, between Editor Layout's group and Word Wrap's; the group of
	// the command palette comes first
	want := []string{"Command Palette...", "Open View...", "-", "Appearance", "Editor Layout", "-", "Problems", "Output", "-", "Word Wrap"}
	if !reflect.DeepEqual(titles, want) {
		t.Errorf("got %v, want %v", titles, want)
	}
}

// TestExplorerContextMenu checks the rows of the context menu of the Explorer for the kinds of item.
func TestExplorerContextMenu(t *testing.T) {
	menus := DefaultMenus(DefaultRegistry())
	rows := func(ctx contextkey.MapContext) []string {
		var out []string
		for _, e := range menus.Items(ExplorerContext, ctx) {
			switch {
			case e.Separator:
				out = append(out, "-")
			case e.Disabled:
				out = append(out, "("+e.Title+")")
			default:
				out = append(out, e.Title)
			}
		}
		return out
	}
	file := contextkey.MapContext{"resourceScheme": "file", "isFileSystemResource": true, "explorerResourceMoveableToTrash": true}
	with := func(base contextkey.MapContext, extra contextkey.MapContext) contextkey.MapContext {
		out := contextkey.MapContext{}
		for k, v := range base {
			out[k] = v
		}
		for k, v := range extra {
			out[k] = v
		}
		return out
	}
	cases := []struct {
		name string
		ctx  contextkey.MapContext
		want []string
	}{
		{"a file", file, []string{"Open to the Side", "Open Containing Folder", "-", "Cut", "Copy", "-", "Copy Path", "Copy Relative Path", "-", "Rename...", "Delete"}},
		{"a folder with something to paste", with(file, contextkey.MapContext{"explorerResourceIsFolder": true, "fileCopied": true}),
			[]string{"New File...", "New Folder...", "Open Containing Folder", "-", "Cut", "Copy", "Paste", "-", "Copy Path", "Copy Relative Path", "-", "Rename...", "Delete"}},
		{"a folder with nothing to paste", with(file, contextkey.MapContext{"explorerResourceIsFolder": true}),
			[]string{"New File...", "New Folder...", "Open Containing Folder", "-", "Cut", "Copy", "(Paste)", "-", "Copy Path", "Copy Relative Path", "-", "Rename...", "Delete"}},
		{"the root folder", with(file, contextkey.MapContext{"explorerResourceIsFolder": true, "explorerResourceIsRoot": true}),
			[]string{"New File...", "New Folder...", "Open Containing Folder", "-", "(Paste)", "-", "Copy Path", "Copy Relative Path"}},
		{"a file where the trash cannot be used", with(file, contextkey.MapContext{"explorerResourceMoveableToTrash": false}),
			[]string{"Open to the Side", "Open Containing Folder", "-", "Cut", "Copy", "-", "Copy Path", "Copy Relative Path", "-", "Rename...", "Delete Permanently"}},
		{"a read-only file", with(file, contextkey.MapContext{"explorerResourceReadonly": true}),
			[]string{"Open to the Side", "Open Containing Folder", "-", "Copy", "-", "Copy Path", "Copy Relative Path", "-", "(Rename...)", "Delete"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rows(tc.ctx); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got  %q\nwant %q", got, tc.want)
			}
		})
	}
}

func TestQuickInputMenus(t *testing.T) {
	reg := DefaultRegistry()
	menus := DefaultMenus(reg)
	titles := func(menu string, ctx contextkey.Context) []string {
		var out []string
		for _, e := range menus.Items(menu, ctx) {
			if !e.Separator {
				out = append(out, e.Title)
			}
		}
		return out
	}
	if got := titles(GlobalActivity, contextkey.MapContext{}); len(got) == 0 || got[0] != "Command Palette..." {
		t.Errorf("Manage: %v", got)
	}
	if got := titles(MenubarGoMenu, contextkey.MapContext{"activeEditor": true}); !slices.Contains(got, "Go to Line/Column...") {
		t.Errorf("Go: %v", got)
	}
	// Go to File... is in the group of the global navigation, before the rows of the file
	goToFile := false
	for _, it := range menus.All() {
		if it.Menu == MenubarGoMenu && it.Command == "workbench.action.quickOpen" && it.Group == "3_global_nav" && it.Order == 1 && it.MnemonicTitle == "Go to &&File..." {
			goToFile = true
		}
	}
	if !goToFile {
		t.Error("the Go menu has no Go to File row")
	}
	// the palette leaves out Quick Open View: its row has a When that never holds
	hidden := false
	for _, it := range menus.All() {
		if it.Menu == CommandPalette && it.Command == "workbench.action.quickOpenView" && it.When != nil && !it.When.Evaluate(contextkey.MapContext{}) {
			hidden = true
		}
	}
	if !hidden {
		t.Error("the CommandPalette menu does not hide workbench.action.quickOpenView")
	}
	for _, id := range []string{"workbench.action.showCommands", "workbench.action.quickOpen", "workbench.action.gotoLine", "workbench.action.gotoOffset", "workbench.action.openView", "workbench.action.quickOpenView", "workbench.action.clearCommandHistory", "workbench.action.closeQuickOpen"} {
		if _, ok := reg.Lookup(id); !ok {
			t.Errorf("command %s is not registered", id)
		}
	}
}

func TestDefaultOpenRecentMenu(t *testing.T) {
	menus := DefaultMenus(DefaultRegistry())
	rows := func(ctx contextkey.MapContext) (out []string) {
		for _, it := range menus.Items(MenubarRecentMenu, ctx) {
			switch {
			case it.Separator:
				out = append(out, "-")
			case it.Disabled:
				out = append(out, it.Title+"~")
			default:
				out = append(out, it.Title)
			}
		}
		return out
	}
	// Reopen Closed Editor is dimmed until an editor was closed; the dynamic rows of the recent folders and
	// files go between it and "More..." (see the provider of the process)
	if got, want := rows(contextkey.MapContext{}), []string{"Reopen Closed Editor~", "-", "More...", "-", "Clear Recently Opened..."}; !reflect.DeepEqual(got, want) {
		t.Errorf("open recent menu %v, want %v", got, want)
	}
	if got := rows(contextkey.MapContext{"canReopenClosedEditor": true}); got[0] != "Reopen Closed Editor" {
		t.Errorf("open recent menu %v", got)
	}
	var found bool
	for _, e := range menus.Items(MenubarFileMenu, contextkey.MapContext{}) {
		if e.Submenu == MenubarRecentMenu {
			found = e.Title == "Open Recent" && e.MnemonicTitle == "Open &&Recent"
		}
	}
	if !found {
		t.Error("the File menu has no Open Recent submenu")
	}
	cmds := DefaultRegistry()
	for id, title := range map[string]string{
		"workbench.action.openRecent": "Open Recent...", "workbench.action.clearRecentFiles": "Clear Recently Opened...",
		"openRecentFolder": "", "openRecentFile": "",
	} {
		meta, ok := cmds.Lookup(id)
		if !ok || meta.Title != title {
			t.Errorf("%s: %+v, %v, want the title %q", id, meta, ok, title)
		}
	}
}

// TestSettingsMenus checks the rows of the settings editor (step 8.1): Settings in the Preferences submenu and the
// Manage menu, and the rules of the palette.
func TestSettingsMenus(t *testing.T) {
	reg := DefaultRegistry()
	menus := DefaultMenus(reg)
	for id, title := range map[string]string{
		"workbench.action.openSettings": "Settings", "workbench.action.openSettings2": "Open Settings (UI)",
		"workbench.action.openWorkspaceSettings": "Open Workspace Settings", "workbench.action.openSettingsJson": "Open User Settings (JSON)",
		"workbench.action.openGlobalSettings": "Open User Settings", "workbench.action.openRawDefaultSettings": "Open Default Settings (JSON)",
		"workbench.action.openWorkspaceSettingsFile": "Open Workspace Settings (JSON)",
	} {
		meta, ok := reg.Lookup(id)
		if !ok {
			t.Errorf("command %s is not registered", id)
		} else if meta.Title != title || (id != "workbench.action.openSettings" && meta.Category != "Preferences") {
			t.Errorf("%s: %+v", id, meta)
		}
	}
	rowsOf := func(menu string) []string {
		var out []string
		for _, e := range menus.Items(menu, contextkey.MapContext{}) {
			out = append(out, e.MnemonicTitle+"="+e.Command)
		}
		return out
	}
	if got := rowsOf(MenubarPreferencesMenu); len(got) != 3 || got[0] != "&&Settings=workbench.action.openSettings" || got[1] != "Keyboard Shortcuts=workbench.action.openGlobalKeybindings" || got[2] != "&&Themes=" {
		t.Errorf("Preferences menu %v", got)
	}
	found := false
	for _, e := range menus.Items(MenubarFileMenu, contextkey.MapContext{}) {
		if e.Submenu == MenubarPreferencesMenu && e.MnemonicTitle == "&&Preferences" {
			found = true
		}
	}
	if !found {
		t.Error("the File menu has no Preferences submenu")
	}
	// the palette: Settings is hidden, Open Workspace Settings needs a folder
	shown := func(command string, ctx contextkey.Context) bool {
		for _, it := range menus.All() {
			if it.Menu == CommandPalette && it.Command == command && it.When != nil && !it.When.Evaluate(ctx) {
				return false
			}
		}
		return true
	}
	empty := contextkey.MapContext{"workbenchState": "empty"}
	folder := contextkey.MapContext{"workbenchState": "folder"}
	for _, c := range []struct {
		command string
		ctx     contextkey.Context
		want    bool
	}{
		{"workbench.action.openSettings", folder, false},
		{"workbench.action.openSettings2", empty, true},
		{"workbench.action.openWorkspaceSettings", empty, false},
		{"workbench.action.openWorkspaceSettings", folder, true},
		{"workbench.action.openWorkspaceSettingsFile", empty, false},
		{"workbench.action.openWorkspaceSettingsFile", folder, true},
		{"workbench.action.openSettingsJson", empty, true},
		{"workbench.action.openRawDefaultSettings", empty, true},
		{"workbench.action.openGlobalSettings", empty, true},
	} {
		if got := shown(c.command, c.ctx); got != c.want {
			t.Errorf("%s shown in the palette = %v, want %v", c.command, got, c.want)
		}
	}
}

func TestKeybindingsEditorMenus(t *testing.T) {
	reg := DefaultRegistry()
	menus := DefaultMenus(reg)
	for id, want := range map[string]Meta{
		"workbench.action.openGlobalKeybindings":        {Title: "Open Keyboard Shortcuts", Category: "Preferences"},
		"workbench.action.openGlobalKeybindingsFile":    {Title: "Open Keyboard Shortcuts (JSON)", Category: "Preferences"},
		"workbench.action.openDefaultKeybindingsFile":   {Title: "Open Default Keyboard Shortcuts (JSON)", Category: "Preferences"},
		"keybindings.editor.clearSearchResults":         {Title: "Clear Search Results"},
		"keybindings.editor.defineKeybinding":           {},
		"keybindings.editor.addKeybinding":              {},
		"keybindings.editor.defineWhenExpression":       {},
		"keybindings.editor.removeKeybinding":           {},
		"keybindings.editor.resetKeybinding":            {},
		"keybindings.editor.showConflicts":              {},
		"keybindings.editor.copyKeybindingEntry":        {},
		"keybindings.editor.copyCommandKeybindingEntry": {},
		"keybindings.editor.copyCommandTitle":           {},
		"keybindings.editor.searchKeybindings":          {},
		"keybindings.editor.recordSearchKeys":           {},
		"keybindings.editor.focusKeybindings":           {},
	} {
		meta, ok := reg.Lookup(id)
		if !ok {
			t.Errorf("command %s is not registered", id)
		} else if meta.Title != want.Title || meta.Category != want.Category {
			t.Errorf("%s: %+v, want %+v", id, meta, want)
		}
	}
	// the rows of the menus
	for _, menu := range []string{MenubarPreferencesMenu, GlobalActivity} {
		found := false
		for _, e := range menus.Items(menu, contextkey.MapContext{}) {
			if e.Command == "workbench.action.openGlobalKeybindings" && e.Title == "Keyboard Shortcuts" {
				found = true
			}
		}
		if !found {
			t.Errorf("the %s menu has no Keyboard Shortcuts row", menu)
		}
	}
	// the palette lists the commands that open the files and hides Clear Search Results
	hiddenInPalette := func(command string) bool {
		for _, it := range menus.All() {
			if it.Menu == CommandPalette && it.Command == command && it.When != nil && !it.When.Evaluate(contextkey.MapContext{}) {
				return true
			}
		}
		return false
	}
	for command, hidden := range map[string]bool{
		"keybindings.editor.clearSearchResults":       true,
		"workbench.action.openGlobalKeybindings":      false,
		"workbench.action.openGlobalKeybindingsFile":  false,
		"workbench.action.openDefaultKeybindingsFile": false,
	} {
		if got := hiddenInPalette(command); got != hidden {
			t.Errorf("%s hidden in the palette = %v, want %v", command, got, hidden)
		}
	}
}

func TestThemeMenus(t *testing.T) {
	reg := DefaultRegistry()
	menus := DefaultMenus(reg)
	for id, title := range map[string]string{"workbench.action.selectTheme": "Color Theme", "workbench.action.selectIconTheme": "File Icon Theme"} {
		meta, ok := reg.Lookup(id)
		if !ok || meta.Title != title || meta.Category != "Preferences" {
			t.Errorf("%s: %+v, registered %v", id, meta, ok)
		}
	}
	// the rows are (title, command, submenu)
	type row struct{ title, command, submenu string }
	tests := []struct {
		name string
		menu string
		want []row
	}{
		{"the Themes submenu", ThemesSubMenu, []row{
			{"Color Theme", "workbench.action.selectTheme", ""},
			{"File Icon Theme", "workbench.action.selectIconTheme", ""},
		}},
		{"the Preferences menu ends with Themes", MenubarPreferencesMenu, []row{{"&&Themes", "", ThemesSubMenu}}},
		{"the Manage menu ends with Themes", GlobalActivity, []row{{"Themes", "", ThemesSubMenu}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []row
			for _, e := range menus.Items(tc.menu, contextkey.MapContext{}) {
				title := e.Title
				if e.MnemonicTitle != "" && e.Submenu != "" {
					title = e.MnemonicTitle
				}
				got = append(got, row{title, e.Command, e.Submenu})
			}
			if len(got) < len(tc.want) || !reflect.DeepEqual(got[len(got)-len(tc.want):], tc.want) {
				t.Errorf("got %v, want it to end with %v", got, tc.want)
			}
		})
	}
}
