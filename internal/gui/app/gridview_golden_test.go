package app

import (
	"testing"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// goldenSource is a file with enough code to see in a group.
const goldenSource = `package main

import "fmt"

// greet says hello to a name.
func greet(name string) string {
	if name == "" {
		name = "world"
	}
	return fmt.Sprintf("hello, %s", name)
}

func main() {
	fmt.Println(greet(""))
}
`

// goldenGrid makes a window with three files, with the sidebar closed, in the theme.
func goldenGrid(t *testing.T, theme string) *groupApp {
	t.Helper()
	win, _ := themedWindow(t, map[string]any{"workbench.colorTheme": theme, "editor.minimap.enabled": false, "breadcrumbs.enabled": false})
	texts := map[string]string{"main.go": goldenSource, "util.go": "package main\n\n// helper\nfunc helper() {}\n", "notes.txt": "notes\n"}
	a := newGroupAppWith(t, win, ui.Size{Width: 960, Height: 560}, []string{"main.go", "util.go", "notes.txt"}, texts, "main.go", "util.go", "notes.txt")
	// the text of the window is in the test font, not in a font of the machine
	a.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
	a.run("workbench.action.toggleSidebarVisibility")
	return a
}

var goldenThemesOfTheGrid = []struct{ name, theme string }{{"dark", "Dark Modern"}, {"light", "Light Modern"}}

func TestGoldenEditorGrid(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name+" two groups side by side", func(t *testing.T) {
			a := goldenGrid(t, th.theme)
			a.run("workbench.action.splitEditorRight")
			// the first group shows another file than the second
			a.clickTab("util.go")
			a.run("workbench.action.focusRightGroup")
			a.pump()
			testutil.MatchesGolden(a.ts, "grid_two_groups_"+th.name)
			a.noErrors()
		})
		t.Run(th.name+" three groups, the one below active", func(t *testing.T) {
			a := goldenGrid(t, th.theme)
			a.run("workbench.action.splitEditorRight")
			a.run("workbench.action.splitEditorDown")
			if a.shape() != "H(1 V(2 3))" {
				t.Fatalf("the grid is %s", a.shape())
			}
			testutil.MatchesGolden(a.ts, "grid_three_groups_"+th.name)
			a.noErrors()
		})
		t.Run(th.name+" an empty group", func(t *testing.T) {
			a := goldenGrid(t, th.theme)
			a.run("workbench.action.newGroupRight")
			if !a.group(a.activeID()).Empty() || a.grid().Count() != 2 {
				t.Fatalf("the grid is %s", a.shape())
			}
			testutil.MatchesGolden(a.ts, "grid_empty_group_"+th.name)
			a.noErrors()
		})
	}
}
