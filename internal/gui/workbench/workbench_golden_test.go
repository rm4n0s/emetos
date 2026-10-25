package workbench

import (
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// demo fills the parts the way the views of the later steps will.
func demo(o *Options) {
	o.Menus = []kit.MenuBarMenu{{Label: "&File"}, {Label: "&Edit"}, {Label: "&Selection"}, {Label: "&View"}, {Label: "&Go"}, {Label: "&Help"}}
	o.SideBarTitle = "Explorer"
	o.PanelTabs = []PanelTab{{ID: "problems", Title: "Problems"}, {ID: "output", Title: "Output"}, {ID: "debug", Title: "Debug Console"}}
	o.ActivePanel = "output"
	o.StatusLeft = []Widget{statusText("main")}
	o.StatusRight = []Widget{statusText("Ln 3, Col 14"), statusText("Spaces: 4"), statusText("UTF-8"), statusText("Go")}
}

func TestGoldenWorkbench(t *testing.T) {
	cases := []struct {
		name  string
		state func(*LayoutState)
		extra map[string]any
	}{
		{"all_parts", func(s *LayoutState) { s.PanelVisible = true }, nil},
		{"panel_right", func(s *LayoutState) { s.PanelVisible, s.PanelPosition = true, PanelRight }, nil},
		{"panel_left", func(s *LayoutState) { s.PanelVisible, s.PanelPosition = true, PanelLeft }, nil},
		{"no_sidebar", func(s *LayoutState) { s.SideBarVisible, s.PanelVisible = false, true }, nil},
		{"sidebar_right", func(s *LayoutState) { s.SideBarPosition, s.PanelVisible = SideBarRight, true }, nil},
		{"panel_maximized", func(s *LayoutState) { s.PanelVisible, s.PanelMaximized = true, true }, nil},
		{"editor_only", func(s *LayoutState) {
			s.SideBarVisible, s.ActivityBarVisible, s.StatusBarVisible = false, false, false
		}, map[string]any{"window.menuBarVisibility": "hidden"}},
	}
	for _, th := range goldenThemes {
		for _, tc := range cases {
			t.Run(th.name+" "+tc.name, func(t *testing.T) {
				st := DefaultLayoutState()
				tc.state(&st)
				h := newHarness(t, ui.Size{Width: 960, Height: 600}, th.colors(), st, tc.extra, demo)
				testutil.MatchesGolden(h.ts, "workbench_"+tc.name+"_"+th.name)
				noErrors(t, h)
			})
		}
	}
}

// viewsDemo fills the activity bar, the side bar and the menus from the registries, the way the window
// does.
func viewsDemo(m *menuServices, views **Views, ctl *kit.MenuBarController) func(*Options) {
	return func(o *Options) {
		m.commands = o.Commands
		m.menus = command.DefaultMenus(o.Commands)
		m.keys = o.Keys
		m.source = m.newSource(nil)
		*views = NewViews(testViewRegistry(), o.Layout)
		o.Views, o.ViewContext, o.MenuBar = *views, o.Keys, ctl
		o.Menus = m.source.BarMenus()
		o.OnRunMenu = m.source.Run
		o.ManageItems = func() []kit.MenuItem {
			return []kit.MenuItem{
				{ID: "palette", Label: "Command &Palette...", Keybinding: "Ctrl+Shift+P"}, kit.SeparatorItem(),
				{ID: "profiles", Label: "&Profiles"}, {ID: "settings", Label: "&Settings", Keybinding: "Ctrl+,"},
				{ID: "keys", Label: "&Keyboard Shortcuts", Keybinding: "Ctrl+K Ctrl+S"}, {ID: "themes", Label: "&Themes", Submenu: true,
					Items: []kit.MenuItem{{ID: "color", Label: "Color Theme"}}},
			}
		}
		o.PanelTabs = []PanelTab{{ID: "problems", Title: "Problems"}, {ID: "output", Title: "Output"}}
		o.ActivePanel = "problems"
		o.StatusLeft = []Widget{statusText("main")}
		o.StatusRight = []Widget{statusText("Ln 3, Col 14"), statusText("Go")}
	}
}

func TestGoldenViewsAndMenus(t *testing.T) {
	size := ui.Size{Width: 760, Height: 480}
	classic := map[string]any{"window.menuBarVisibility": "classic"}
	cases := []struct {
		name  string
		extra map[string]any
		do    func(h *harness, v *Views)
	}{
		{"views", nil, func(h *harness, v *Views) { v.SetCollapsed("outline", false) }},
		{"views_collapsed", nil, func(h *harness, v *Views) { v.SetCollapsed("folders", true) }},
		// the menus of the classic menu bar: titles in the title bar row
		{"edit_menu", classic, func(h *harness, v *Views) { h.openMenu(1) }},
		{"view_menu", classic, func(h *harness, v *Views) { h.keys.Set("sideBarVisible", true); h.openMenu(3) }},
		{"manage_menu", nil, func(h *harness, v *Views) { h.ts.Tap(gluttertest.ByKey(ManageKey)); h.settle() }},
		// the compact menu: the "≡" button, its menu and the submenu of Edit
		{"compact_menu", nil, func(h *harness, v *Views) { h.openMenu(0) }},
		{"compact_submenu", nil, func(h *harness, v *Views) {
			h.openMenu(0)
			h.ts.TapAt(h.ts.Center(gluttertest.ByText("Edit")), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse})
		}},
	}
	for _, th := range goldenThemes {
		for _, tc := range cases {
			t.Run(th.name+" "+tc.name, func(t *testing.T) {
				var views *Views
				m := &menuServices{}
				ctl := kit.NewMenuBarController()
				st := DefaultLayoutState()
				st.PanelVisible = true
				h := newHarness(t, size, th.colors(), st, tc.extra, viewsDemo(m, &views, ctl))
				h.menuBar = ctl
				tc.do(h, views)
				h.settle()
				testutil.MatchesGolden(h.ts, "workbench_"+tc.name+"_"+th.name)
				noErrors(t, h)
			})
		}
	}
}
