package command

import (
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
	items := menus.Items(MenubarFileMenu, contextkey.MapContext{})
	if len(items) != 1 || items[0].Command != "workbench.action.closeActiveEditor" || items[0].MnemonicTitle != "&&Close Editor" {
		t.Fatalf("file menu %+v", items)
	}
	// the item is there with no editor open, dimmed
	if !items[0].Disabled {
		t.Error("Close Editor is enabled with no editor open")
	}
	open := menus.Items(MenubarFileMenu, contextkey.MapContext{"activeEditor": "emetos.textEditor"})
	if len(open) != 1 || open[0].Disabled {
		t.Errorf("Close Editor with an editor open: %+v", open)
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
		{"the Manage menu has no commands yet", GlobalActivity, contextkey.MapContext{}, nil},
		{"Go", MenubarGoMenu, contextkey.MapContext{}, []string{"Switch Group", "-", "Go to Bracket"}},
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
		if len(got) < 2 || got[0] != "Appearance" || got[1] != "Editor Layout" {
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
