package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The goldens of the dialog that asks whether to save before closing: one file, several files and a list
// that is cut after ten files.

// goldenClose is a window with files open, all of them with unsaved changes, asking whether to close them all.
func goldenClose(t *testing.T, theme string, count int) *saveApp {
	t.Helper()
	win := settingsWindow(t, map[string]any{"workbench.colorTheme": theme, "editor.minimap.enabled": false, "breadcrumbs.enabled": false})
	var names []string
	texts := map[string]string{}
	for i := 1; i <= count; i++ {
		name := fmt.Sprintf("file%02d.go", i)
		names = append(names, name)
		texts[name] = fmt.Sprintf("package main\n\n// file %d\n", i)
	}
	if count == 1 {
		names, texts = []string{"main.go"}, map[string]string{"main.go": "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"}
	}
	g := newGroupAppWith(t, win, ui.Size{Width: 900, Height: 640}, names, texts, names...)
	g.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
	a := &saveApp{groupApp: g, fd: g.ts.FakeDesktop()}
	for _, n := range names {
		a.edit(n)
	}
	return a
}

func TestGoldenCloseDialog(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		for _, tc := range []struct {
			name  string
			files int
		}{{"one file", 1}, {"several files", 3}, {"a list cut after ten files", 12}} {
			t.Run(th.name+" "+tc.name, func(t *testing.T) {
				a := goldenClose(t, th.theme, tc.files)
				a.run("workbench.action.closeAllEditors")
				a.ts.Pump(300 * time.Millisecond)
				a.pump()
				if !a.win.Dialogs.IsShowing() {
					t.Fatal("no dialog shows")
				}
				name := map[int]string{1: "one", 3: "several", 12: "many"}[tc.files]
				a.ts.MatchesGolden("close_dialog_" + name + "_" + th.name)
				a.noErrors()
			})
		}
	}
}
