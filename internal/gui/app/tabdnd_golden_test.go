package app

import (
	"testing"

	"github.com/rm4n0s/glutter/ui"
)

// TestGoldenTabInteractions looks at a tab dragged over the edge of another group (the half of the
// area it would split to is highlighted, the label follows the pointer), over a strip (a line between
// two tabs), and at the context menu of a tab.
func TestGoldenTabInteractions(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name+" the drop overlay on an edge", func(t *testing.T) {
			a := goldenGrid(t, th.theme)
			a.run("workbench.action.splitEditorRight")
			a.clickTab("util.go")
			area := a.editorArea(2)
			a.dragTab(1, "notes.txt", ui.Offset{Dx: area.Right - 20, Dy: area.Top + area.Height()/2})
			a.ts.MatchesGolden("tabs_drop_overlay_" + th.name)
			a.noErrors()
		})
		t.Run(th.name+" the drop overlay on the middle", func(t *testing.T) {
			a := goldenGrid(t, th.theme)
			a.run("workbench.action.splitEditorRight")
			a.clickTab("util.go")
			a.dragTab(1, "notes.txt", center(a.editorArea(2)))
			a.ts.MatchesGolden("tabs_drop_overlay_center_" + th.name)
			a.noErrors()
		})
		t.Run(th.name+" the line over another strip", func(t *testing.T) {
			a := goldenGrid(t, th.theme)
			a.run("workbench.action.splitEditorRight")
			if err := a.grid().Activate(a.group(2)); err != nil {
				t.Fatal(err)
			}
			a.show("util.go", false)
			target := a.tabRect(2, "util.go")
			a.dragTab(1, "notes.txt", ui.Offset{Dx: target.Left + 10, Dy: target.Top + 15})
			a.ts.MatchesGolden("tabs_drop_strip_" + th.name)
			a.noErrors()
		})
		t.Run(th.name+" the context menu of a tab", func(t *testing.T) {
			a := goldenGrid(t, th.theme)
			a.group(1).Model.Pin(a.group(1).Model.EditorAt(1))
			a.rightClickTab(1, "util.go")
			a.ts.MatchesGolden("tab_context_menu_" + th.name)
			a.noErrors()
		})
	}
}
