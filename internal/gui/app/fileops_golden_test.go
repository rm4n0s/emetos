package app

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"

	"github.com/rm4n0s/emetos/internal/gui/testutil"
	"github.com/rm4n0s/emetos/internal/gui/views/explorer"
)

// The goldens of the file operations of the Explorer: the inline input, its balloon, the dialog that asks before a delete, the
// context menu, and a cut item.

func TestGoldenExplorerFileOperations(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name, func(t *testing.T) {
			// the input of a new file, with the text typed so far
			t.Run("new file input", func(t *testing.T) {
				a := goldenExplorer(t, th.theme, true, "main.go")
				fa := &fileApp{a}
				fa.focusTree("assets")
				fa.mustRun("explorer.newFile", nil)
				fa.ts.EnterText(gluttertest.ByKey(explorer.EditInputKey), "handler.go")
				fa.ts.Pump(300 * time.Millisecond)
				fa.pump()
				testutil.MatchesGolden(fa.ts, "explorer_new_file_"+th.name)
				fa.noErrors()
			})
			// the input with the balloon of a name that is taken
			t.Run("input with an error", func(t *testing.T) {
				a := goldenExplorer(t, th.theme, true, "main.go")
				fa := &fileApp{a}
				fa.focusTree("main.go")
				fa.mustRun("explorer.newFile", nil)
				fa.ts.EnterText(gluttertest.ByKey(explorer.EditInputKey), "go.mod")
				fa.ts.Pump(300 * time.Millisecond)
				fa.pump()
				testutil.MatchesGolden(fa.ts, "explorer_input_error_"+th.name)
				fa.noErrors()
			})
			t.Run("rename input", func(t *testing.T) {
				a := goldenExplorer(t, th.theme, true, "main.go")
				fa := &fileApp{a}
				fa.focusTree("README.md")
				fa.mustRun("renameFile", nil)
				fa.ts.Pump(300 * time.Millisecond)
				fa.pump()
				testutil.MatchesGolden(fa.ts, "explorer_rename_"+th.name)
				fa.noErrors()
			})
			t.Run("delete confirmation", func(t *testing.T) {
				a := goldenExplorer(t, th.theme, true, "main.go")
				fa := &fileApp{a}
				fa.focusTree("README.md")
				fa.mustRun("moveFileToTrash", nil)
				fa.ts.Pump(300 * time.Millisecond)
				fa.pump()
				testutil.MatchesGolden(fa.ts, "explorer_delete_dialog_"+th.name)
				fa.noErrors()
			})
			t.Run("context menu", func(t *testing.T) {
				a := goldenExplorer(t, th.theme, true, "main.go")
				fa := &fileApp{a}
				fa.focusTree("main.go")
				fa.mustRun("workbench.files.action.collapseExplorerFolders", nil)
				fa.ts.TapAt(fa.rowCenter("docs"), secondaryDown)
				fa.ts.Pump(300 * time.Millisecond)
				fa.pump()
				testutil.MatchesGolden(fa.ts, "explorer_context_menu_"+th.name)
				fa.noErrors()
			})
			t.Run("cut item", func(t *testing.T) {
				a := goldenExplorer(t, th.theme, true, "main.go")
				fa := &fileApp{a}
				fa.focusTree("go.mod")
				fa.mustRun("filesExplorer.cut", nil)
				fa.focusTree("Makefile") // another row has the selection, so the cut row shows its own look
				fa.ts.Pump(300 * time.Millisecond)
				fa.pump()
				testutil.MatchesGolden(fa.ts, "explorer_cut_"+th.name)
				fa.noErrors()
			})
		})
	}
}

func (a *fileApp) noErrors() {
	a.t.Helper()
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		a.t.Errorf("errors: %v", errs)
	}
}
