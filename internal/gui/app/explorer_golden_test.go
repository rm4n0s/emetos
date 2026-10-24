package app

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// goldenExplorer makes a window of the theme over a small project, with the files in open shown, the Open
// Editors visible, and the text of the window in the test font.
func goldenExplorer(t *testing.T, theme string, withFolder bool, open ...string) *explorerApp {
	t.Helper()
	paths := testutil.Isolate(t)
	testutil.Settings(t, paths, map[string]any{"workbench.colorTheme": theme, "editor.minimap.enabled": false, "breadcrumbs.enabled": false})
	root := ""
	if withFolder {
		root = filepath.Join(t.TempDir(), "project")
		writeTree(t, root, "main.go", "go.mod", "README.md", "Makefile", ".gitignore", "assets/logo.png", "docs/guide.md",
			"internal/server/server.go", "internal/server/server_test.go", "internal/store/store.go")
	}
	win, err := NewWindow(context.Background(), NewProcess(ProcessOptions{Paths: paths}), root)
	if err != nil {
		t.Fatal(err)
	}
	var docs []*document.Document
	for _, p := range open {
		doc, err := win.Documents.OpenFile(context.Background(), filepath.Join(root, p), document.NewOptions())
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, doc)
	}
	a := &explorerApp{t: t, win: win, root: win.Folder}
	a.ts = testutil.New(t, ui.Size{Width: 900, Height: 460})
	a.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
	a.ts.PumpWidget(newAppDocs(context.Background(), win, docs, nil, ""))
	a.pump()
	return a
}

// TestGoldenExplorerInTheWorkbench looks at the Explorer in the side bar of a window: the name of the folder
// in the header, the file that is open revealed with the folders above it, the Open Editors above the
// tree, and the welcome content of a window that has no folder.
func TestGoldenExplorerInTheWorkbench(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name+" a folder", func(t *testing.T) {
			a := goldenExplorer(t, th.theme, true, "main.go", "internal/server/server.go")
			a.win.Views.SetHidden(command.ViewOpenEditors, false)
			a.openFile("internal/server/server.go")
			a.ts.Pump(300 * time.Millisecond)
			a.pump()
			// the tokens of the open file come from a goroutine: a loaded machine paints the text before them
			a.waitFor("the tokens of the open file", func() bool {
				vm := a.ts.Widget(gluttertest.ByType[*editor.Editor]()).(*editor.Editor).ViewModel()
				if !vm.Tokenizer().Active() {
					return false
				}
				vm.Tokenizer().Settle()
				return vm.Tokenizer().Line(0) != nil
			})
			a.pump()
			a.ts.MatchesGolden("explorer_folder_" + th.name)
			if errs := a.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
		t.Run(th.name+" no folder", func(t *testing.T) {
			a := goldenExplorer(t, th.theme, false)
			a.ts.MatchesGolden("explorer_no_folder_" + th.name)
			if errs := a.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}
