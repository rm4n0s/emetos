package app

import (
	"testing"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// goldenPanels makes a window with a folder (made up: the markers name files by path, and none is
// opened) whose panel is open on a tab, with the sidebar closed, in the theme.
func goldenPanels(t *testing.T, theme string, size ui.Size) *panelApp {
	t.Helper()
	if !DevCommandsEnabled() {
		t.Skip("this build does not run dev commands")
	}
	win, _ := themedWindow(t, map[string]any{"workbench.colorTheme": theme, "editor.minimap.enabled": false, "breadcrumbs.enabled": false})
	win.Folder = "/work/project"
	a := newGroupAppWith(t, win, size, []string{"main.go"}, map[string]string{"main.go": goldenSource}, "main.go")
	// the text of the window is in the test font, not in a font of the machine
	a.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
	a.run("workbench.action.toggleSidebarVisibility")
	// the text that was built in the frames around the change of the font is drawn in the fallback font,
	// which the test machine does not have: build it again
	a.shell().SetState(func() {})
	a.pump()
	return &panelApp{a}
}

// goldenMarkers sets the markers of three files of the made up project.
func (a *panelApp) goldenMarkers() {
	a.t.Helper()
	set := func(path string, markers ...any) {
		a.mustDo("dev.setMarkers", map[string]any{"owner": "golden", "resource": path, "markers": markers})
	}
	set("/work/project/main.go",
		marker("error", "undefined: foo", 13, 6, "compiler", "UndeclaredName"),
		marker("error", "missing return", 11, 2, "vet"),
		marker("warning", "variable x is unused", 9, 3, "compiler"))
	set("/work/project/pkg/util.go",
		marker("warning", "exported function Helper should have a comment", 4, 6, "lint", "exported"),
		marker("info", "this call can be simplified", 7, 2, "lint"))
	set("/work/project/web/style.css", marker("error", "Unknown property: 'colour'", 12, 3, "css", "unknownProperty"))
}

var goldenThemesOfThePanels = []struct{ name, theme string }{{"dark", "Dark Modern"}, {"light", "Light Modern"}}

func TestGoldenProblemsPanel(t *testing.T) {
	for _, th := range goldenThemesOfThePanels {
		t.Run(th.name+" with markers", func(t *testing.T) {
			a := goldenPanels(t, th.theme, ui.Size{Width: 960, Height: 560})
			a.goldenMarkers()
			a.mustDo("workbench.action.showErrorsWarnings", nil)
			testutil.MatchesGolden(a.ts, "problems_markers_"+th.name)
			a.noErrors()
		})
		t.Run(th.name+" empty", func(t *testing.T) {
			a := goldenPanels(t, th.theme, ui.Size{Width: 960, Height: 560})
			a.mustDo("workbench.action.showErrorsWarnings", nil)
			testutil.MatchesGolden(a.ts, "problems_empty_"+th.name)
			a.noErrors()
		})
		t.Run(th.name+" filtered", func(t *testing.T) {
			a := goldenPanels(t, th.theme, ui.Size{Width: 960, Height: 560})
			a.goldenMarkers()
			a.mustDo("workbench.action.showErrorsWarnings", nil)
			a.mustDo("workbench.actions.workbench.panel.markers.view.toggleErrors", nil)
			a.win.Problems.Filter().SetValue("lint")
			a.pump()
			testutil.MatchesGolden(a.ts, "problems_filtered_"+th.name)
			a.noErrors()
		})
	}
}

func TestGoldenOutputPanel(t *testing.T) {
	for _, th := range goldenThemesOfThePanels {
		t.Run(th.name, func(t *testing.T) {
			a := goldenPanels(t, th.theme, ui.Size{Width: 960, Height: 560})
			for _, line := range []string{
				"2026-10-24 09:00:01.120 [info] Emetos 0.1.0 starting",
				"2026-10-24 09:00:01.134 [info] Opened the folder /work/project",
				"2026-10-24 09:00:01.201 [warning] The settings file has a comment after the last entry",
				"2026-10-24 09:00:02.017 [info] Loaded 14 languages and 3 themes",
				"2026-10-24 09:00:02.350 [error] Cannot read /work/project/missing.go: no such file or directory",
			} {
				a.mustDo("dev.log", map[string]any{"message": line})
			}
			a.mustDo("dev.log", map[string]any{"channel": "Tasks", "message": "no task has run"})
			a.win.Views.OpenPanel(command.ViewContainerOutput)
			a.pump()
			testutil.MatchesGolden(a.ts, "output_lines_"+th.name)
			a.noErrors()
		})
	}
}
