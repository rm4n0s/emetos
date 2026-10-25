package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// TestGoldenExtensionsPage shows the page "Extensions" with an extension that is active, one that waits for its event, one
// that failed and one the user turned off, in both themes.
func TestGoldenExtensionsPage(t *testing.T) {
	for _, th := range goldenThemesOfThePanels {
		t.Run(th.name, func(t *testing.T) {
			greeter, _ := greeterFactory([]string{extension.OnStartup}, nil)
			idle := func() extension.Extension {
				return &testExtension{manifest: extension.Manifest{
					ID: "emetos.rustfmt", Name: "Rust Format", Version: "0.4.0", Description: "Formats Rust files with rustfmt.",
					ActivationEvents: []string{extension.OnLanguage("rust"), extension.OnFile("Cargo.toml")},
				}}
			}
			broken := func() extension.Extension {
				return &testExtension{
					manifest: extension.Manifest{ID: "emetos.broken", Name: "Broken Tools", Version: "2.0.1", Description: "Panics when it starts.", ActivationEvents: []string{extension.OnStartup}},
					activate: func(context.Context, extension.API) error { panic("boom") },
				}
			}
			off := func() extension.Extension {
				return &testExtension{manifest: extension.Manifest{
					ID: "emetos.linter", Name: "Linter", Version: "1.0.0", Description: "Shows lint results in the Problems panel.", ActivationEvents: []string{extension.OnStartup},
				}}
			}
			paths := testutil.Isolate(t)
			testutil.Settings(t, paths, map[string]any{
				"window.menuBarVisibility": "classic", "workbench.colorTheme": th.theme, "editor.minimap.enabled": false,
				"breadcrumbs.enabled": false, "files.hotExit": "off", "extensions.disabled": []any{"emetos.linter"},
			})
			folder := filepath.Join(t.TempDir(), "project")
			if err := os.MkdirAll(folder, 0o755); err != nil {
				t.Fatal(err)
			}
			p := newTestProcess(ProcessOptions{Paths: paths, Extensions: []extension.Factory{greeter, idle, broken, off}})
			win, err := NewWorkspace(context.Background(), p, folder)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(win.release)
			a := newGroupAppWith(t, win, ui.Size{Width: 960, Height: 560}, nil, nil)
			a.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
			a.run("workbench.action.toggleSidebarVisibility")
			a.run("workbench.extensions.action.showInstalledExtensions")
			win.Notify.ClearAll()
			// the text that was built in the frames around the change of the font is drawn in the fallback font: build it again
			a.shell().SetState(func() {})
			a.pump()
			testutil.MatchesGolden(a.ts, "extensions_page_"+th.name)
		})
	}
}
