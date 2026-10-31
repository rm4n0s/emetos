package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// TestGoldenMultiFileDiffEditor looks at the editor with a modified file, an added one, a deleted one, a binary one and one that is
// collapsed.
func TestGoldenMultiFileDiffEditor(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name, func(t *testing.T) {
			paths := testutil.Isolate(t)
			testutil.Settings(t, paths, map[string]any{
				"workbench.colorTheme": th.theme, "editor.minimap.enabled": false, "breadcrumbs.enabled": false, "files.hotExit": "off",
				"window.menuBarVisibility": "classic",
			})
			rig := &multiRig{texts: map[string]string{}, sets: map[string][]extension.MultiDiffResource{}, err: map[string]error{}, asked: map[string]int{}}
			p := newTestProcess(ProcessOptions{Paths: paths, Extensions: []extension.Factory{rig.factory()}})
			root := filepath.Join(t.TempDir(), "project")
			if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
				t.Fatal(err)
			}
			for name, text := range map[string]string{"src/loader.go": loaderNow, "docs/notes.md": "# Notes\n\nA new file.\n"} {
				if err := os.MkdirAll(filepath.Dir(filepath.Join(root, name)), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			win, err := NewWorkspace(context.Background(), p, root)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(win.release)
			rig.groupApp = &groupApp{t: t, win: win, files: map[string]string{}, docs: map[string]*document.Document{}}
			rig.ts = testutil.New(t, ui.Size{Width: 1400, Height: 900})
			rig.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
			rig.ts.PumpWidget(newAppDocs(context.Background(), win, nil, nil, ""))
			rig.pump()

			id := "0123456789ab"
			rig.set(tst(id+"/~/src/loader.go"), loaderOld)
			rig.set(tst(id+"/HEAD/old/gone.txt"), "this file\nwas deleted\n")
			rig.set(tst(id+"/~/logo.png"), "BINARY")
			rig.set(tst(id+"/~/src/old.go"), "package src\n")
			loader := document.FileURI(filepath.Join(root, "src/loader.go")).String()
			notes := document.FileURI(filepath.Join(root, "docs/notes.md")).String()
			rig.setFiles("k", extension.MultiDiffResource{
				Original: tst(id + "/~/src/loader.go"), Modified: loader, Path: "src/loader.go", File: loader,
				Letter: "M", Color: "gitDecoration.modifiedResourceForeground", Tooltip: "Modified",
			}, extension.MultiDiffResource{
				Modified: notes, Path: "docs/notes.md", File: notes,
				Letter: "U", Color: "gitDecoration.untrackedResourceForeground", Tooltip: "Untracked",
			}, extension.MultiDiffResource{
				Original: tst(id + "/HEAD/old/gone.txt"), Path: "old/gone.txt",
				Letter: "D", Color: "gitDecoration.deletedResourceForeground", Tooltip: "Deleted",
			}, extension.MultiDiffResource{
				Original: tst(id + "/~/logo.png"), Modified: tst(id + "/~/logo.png"), Path: "logo.png",
				Letter: "M", Color: "gitDecoration.modifiedResourceForeground", Tooltip: "Modified",
			}, extension.MultiDiffResource{
				Original: tst(id + "/~/src/old.go"), Modified: tst(id + "/~/src/old.go"), Path: "src/old.go",
				Letter: "M", Color: "gitDecoration.modifiedResourceForeground", Tooltip: "Modified",
			})
			if err := rig.api.Documents().OpenMultiDiff(extension.MultiDiffSpec{Title: "Git: Changes", Kind: "tst.changes", Key: "k"}); err != nil {
				t.Fatal(err)
			}
			waitFor(t, rig.ts, "the files", func() bool { return len(rig.multiPanes()) == 1 && rig.pane().input.Loaded() })
			rig.pane().model.SetCollapsed(4, true)
			rig.settleBodies()
			time.Sleep(50 * time.Millisecond)
			rig.pump()
			testutil.MatchesGolden(rig.ts, "multi_diff_"+th.name)
			if errs := rig.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}
