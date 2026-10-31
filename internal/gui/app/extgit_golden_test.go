package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/gitscm"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// TestGoldenTheQuestionsOfADiscard shows the confirmations of the Git extension's discard commands (step 24.8) in the window: the
// question about one changed file, and the one about tracked and untracked files together, which has three buttons.
func TestGoldenTheQuestionsOfADiscard(t *testing.T) {
	cases := []struct {
		name  string
		names []string
		kind  string
	}{
		{"one", []string{"loader.go"}, "modified"},
		{"mixed", []string{"notes.md", "scratch.txt"}, "mixed"},
	}
	for _, th := range goldenThemesOfTheGrid {
		for _, tc := range cases {
			t.Run(th.name+" "+tc.name, func(t *testing.T) {
				paths := testutil.Isolate(t)
				testutil.Settings(t, paths, map[string]any{
					"workbench.colorTheme": th.theme, "editor.minimap.enabled": false, "breadcrumbs.enabled": false, "files.hotExit": "off",
					"window.menuBarVisibility": "classic",
				})
				rig := &contentRig{texts: map[string]string{}, asked: map[string]int{}}
				p := newTestProcess(ProcessOptions{Paths: paths, Extensions: []extension.Factory{rig.factory()}})
				root := filepath.Join(t.TempDir(), "project")
				if err := os.MkdirAll(root, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(root, "loader.go"), []byte(loaderNow), 0o644); err != nil {
					t.Fatal(err)
				}
				win, err := NewWorkspace(context.Background(), p, root)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(win.release)
				rig.groupApp = &groupApp{t: t, win: win, files: map[string]string{}, docs: map[string]*document.Document{}}
				rig.ts = testutil.New(t, ui.Size{Width: 900, Height: 560})
				rig.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
				rig.ts.PumpWidget(newAppDocs(context.Background(), win, nil, nil, ""))
				rig.pump()
				answered := 0
				if err := rig.api.Dialogs().Ask(gitscm.DiscardDialogs(tc.names, tc.kind, true), func(extension.DialogResult) { answered++ }); err != nil {
					t.Fatal(err)
				}
				rig.ts.Pump(300 * time.Millisecond)
				rig.pump()
				testutil.MatchesGolden(rig.ts, "git_discard_dialog_"+tc.name+"_"+th.name)
				if errs := rig.ts.TakeErrors(); len(errs) != 0 {
					t.Errorf("errors: %v", errs)
				}
				if answered != 0 {
					t.Errorf("the question was answered by itself")
				}
			})
		}
	}
}
