package app

import (
	"testing"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// TestGoldenDefaultSettings looks at the default settings document in a text editor in both themes: the tab named
// Default Settings, the comments of each setting in the colors of a JSONC file and the group headers, with the
// editor read-only.
func TestGoldenDefaultSettings(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name, func(t *testing.T) {
			win := settingsWindow(t, map[string]any{"workbench.colorTheme": th.theme, "editor.minimap.enabled": false, "breadcrumbs.enabled": false})
			g := newGroupAppWith(t, win, ui.Size{Width: 900, Height: 520}, []string{"main.go"}, map[string]string{"main.go": "package main\n"}, "main.go")
			g.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
			g.run("workbench.action.openRawDefaultSettings")
			if g.active() != "Default Settings" {
				t.Fatalf("active tab %q", g.active())
			}
			testutil.MatchesGolden(g.ts, "default_settings_"+th.name)
			g.noErrors()
		})
	}
}
