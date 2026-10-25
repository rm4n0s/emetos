package workbench

import (
	"fmt"
	"testing"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// manyTabs are n folder tabs with long enough names to not all fit in the row.
func manyTabs(n int) []WorkspaceTab {
	var tabs []WorkspaceTab
	for i := range n {
		name := fmt.Sprintf("workspace-%02d", i)
		tabs = append(tabs, WorkspaceTab{ID: name, Name: name, Tooltip: "/w/" + name, Folder: true})
	}
	return tabs
}

// TestGoldenTitleBar shows the title bar row in Dark Modern and Light Modern: one tab, three tabs with a dirty one and two
// of the same name, tabs that overflow, and the compact and the classic menu open.
func TestGoldenTitleBar(t *testing.T) {
	one := []WorkspaceTab{{ID: "t1", Name: "emetos", Tooltip: "/home/me/emetos", Folder: true}}
	cases := []struct {
		name   string
		tabs   []WorkspaceTab
		active int
		extra  map[string]any
		do     func(r *stripRig)
		window func(*ui.WindowInfo)
	}{
		{"one_tab", one, 0, nil, nil, nil},
		{"three_tabs", threeTabs(), 1, nil, nil, nil},
		{"overflow", manyTabs(14), 6, nil, nil, nil},
		{"unfocused", threeTabs(), 0, nil, nil, func(w *ui.WindowInfo) { w.Activated = false }},
		{"floating", threeTabs(), 0, nil, nil, func(w *ui.WindowInfo) { w.Maximized = false }},
		{"compact_menu", threeTabs(), 0, nil, func(r *stripRig) {
			r.ts.TapAt(r.ts.Center(gluttertest.ByIcon(codiconMenu())), r.mouse())
		}, nil},
		{"classic_menu", threeTabs(), 0, map[string]any{"window.menuBarVisibility": "classic"}, func(r *stripRig) {
			r.ts.TapAt(r.ts.Center(gluttertest.ByText("Edit")), r.mouse())
		}, nil},
		{"tab_menu", threeTabs(), 0, nil, func(r *stripRig) {
			g := r.ts.StartGesture(r.ts.Center(gluttertest.ByText("api — home")), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(2)})
			g.Up()
		}, nil},
		{"new_menu", threeTabs(), 0, nil, func(r *stripRig) {
			r.ts.TapAt(r.ts.Center(gluttertest.ByIcon(codiconAdd())), r.mouse())
		}, nil},
	}
	for _, th := range goldenThemes {
		for _, tc := range cases {
			t.Run(th.name+" "+tc.name, func(t *testing.T) {
				r := newStripRigIn(t, ui.Size{Width: 960, Height: 300}, th.colors(), tc.tabs, tc.active, tc.extra, demo)
				if tc.window != nil {
					info := testutil.WindowInfo
					tc.window(&info)
					r.setWindow(info)
				}
				if tc.do != nil {
					tc.do(r)
					r.settle()
				}
				testutil.MatchesGolden(r.ts, "titlebar_"+tc.name+"_"+th.name)
				noErrors(t, r.harness)
			})
		}
	}
}

// newStripRigIn is newStripRig with a size and a theme.
func newStripRigIn(t *testing.T, size ui.Size, colors kit.Colors, tabs []WorkspaceTab, active int, extra map[string]any, tune func(*Options)) *stripRig {
	t.Helper()
	r := &stripRig{}
	r.bar = &TitleBarOptions{
		Tabs: tabs, Active: active,
		ContextItems: func(i int) []kit.MenuItem {
			return []kit.MenuItem{{ID: "close", Label: "Close"}, {ID: "others", Label: "Close Others"}, {ID: "right", Label: "Close to the Right", Disabled: true},
				kit.SeparatorItem(), {ID: "copy", Label: "Copy Path"}, {ID: "reveal", Label: "Reveal in File Manager"}, kit.SeparatorItem(), {ID: "profile", Label: "Switch Profile..."}}
		},
		NewItems: func() []kit.MenuItem { return []kit.MenuItem{{ID: "folder", Label: "Workspace..."}} },
	}
	r.harness = newHarness(t, size, colors, DefaultLayoutState(), extra, func(o *Options) {
		o.TitleBar = r.bar
		if tune != nil {
			tune(o)
		}
	})
	return r
}
