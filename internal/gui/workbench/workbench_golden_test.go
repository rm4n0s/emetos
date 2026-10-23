package workbench

import (
	"testing"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// demo fills the parts the way the views of the later steps will.
func demo(o *Options) {
	o.Menus = []kit.MenuBarMenu{{Label: "&File"}, {Label: "&Edit"}, {Label: "&Selection"}, {Label: "&View"}, {Label: "&Go"}, {Label: "&Help"}}
	o.SideBarTitle = "Explorer"
	o.PanelTabs = []PanelTab{{"problems", "Problems"}, {"output", "Output"}, {"debug", "Debug Console"}}
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
				h.ts.MatchesGolden("workbench_" + tc.name + "_" + th.name)
				noErrors(t, h)
			})
		}
	}
}
