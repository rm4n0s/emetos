package app

import (
	"testing"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// TestGoldenStatusBar shows the status bar of a window with a file, markers and a notification, with a
// selection, with several cursors, and the language picker that its item opens.
func TestGoldenStatusBar(t *testing.T) {
	for _, th := range goldenThemesOfThePanels {
		size := ui.Size{Width: 960, Height: 420}
		t.Run(th.name+" items", func(t *testing.T) {
			a := goldenPanels(t, th.theme, size)
			a.goldenMarkers()
			a.mustDo("dev.notify", "Build finished")
			a.vm().Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: 9, Col: 1}, Active: buffer.Position{Line: 9, Col: 7}}})
			a.vm().SelectionsChanged()
			a.pump()
			testutil.MatchesGolden(a.ts, "statusbar_items_"+th.name)
			a.noErrors()
		})
		t.Run(th.name+" several cursors", func(t *testing.T) {
			a := goldenPanels(t, th.theme, size)
			a.vm().Cursors().SetSelections([]buffer.Selection{
				{Anchor: buffer.Position{Line: 3, Col: 0}, Active: buffer.Position{Line: 3, Col: 4}},
				{Anchor: buffer.Position{Line: 5, Col: 0}, Active: buffer.Position{Line: 5, Col: 2}},
				{Anchor: buffer.Position{Line: 7, Col: 1}, Active: buffer.Position{Line: 7, Col: 1}},
			})
			a.vm().SelectionsChanged()
			a.pump()
			testutil.MatchesGolden(a.ts, "statusbar_multi_"+th.name)
			a.noErrors()
		})
		t.Run(th.name+" language picker", func(t *testing.T) {
			a := goldenPanels(t, th.theme, ui.Size{Width: 960, Height: 560})
			a.mustDo("workbench.action.editor.changeLanguageMode", nil)
			a.pump()
			testutil.MatchesGolden(a.ts, "statusbar_language_picker_"+th.name)
			a.noErrors()
		})
		t.Run(th.name+" no editor", func(t *testing.T) {
			a := goldenPanels(t, th.theme, size)
			a.run("workbench.action.closeAllEditors")
			testutil.MatchesGolden(a.ts, "statusbar_no_editor_"+th.name)
			a.noErrors()
		})
	}
}
