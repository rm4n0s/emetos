package app

import (
	"context"
	"fmt"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The golden images of the zoom, zen mode and the centered layout (step 8.5).

// goldenModes makes a window with two files, the first shown, in the theme, with the side bar open.
func goldenModes(t *testing.T, theme string, size ui.Size) (*groupApp, *fullScreenFake) {
	t.Helper()
	if !DevCommandsEnabled() {
		t.Skip("this build does not run dev commands")
	}
	win, _ := themedWindow(t, map[string]any{"workbench.colorTheme": theme, "editor.minimap.enabled": false, "breadcrumbs.enabled": false, "files.hotExit": "off"})
	texts := map[string]string{"main.go": goldenSource, "util.go": "package main\n\n// helper\nfunc helper() {}\n"}
	a := newGroupAppWith(t, win, size, []string{"main.go", "util.go"}, texts, "main.go", "util.go")
	// the text of the window is in the test font, not in a font of the machine
	a.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
	// what was built around the change of the font is drawn in the fallback font: build it again
	a.shell().SetState(func() {})
	a.pump()
	fake := newFullScreenFake(true)
	a.connect(fake)
	return a, fake
}

func (a *groupApp) showMenu(id string) {
	a.t.Helper()
	if err := a.win.Commands.Run(command.Context{Ctx: context.Background(), Keys: a.win.Keys}, "dev.openMenu", map[string]any{"id": id}); err != nil {
		a.t.Fatal(err)
	}
	a.pump()
}

func TestGoldenZoom(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		for _, level := range []float64{2, -2} {
			t.Run(fmt.Sprintf("%s level %v", th.name, level), func(t *testing.T) {
				a, _ := goldenModes(t, th.theme, ui.Size{Width: 960, Height: 560})
				a.win.SetZoomLevel(level)
				a.pump()
				a.showMenu(command.MenubarFileMenu)
				name := "zoom_in"
				if level < 0 {
					name = "zoom_out"
				}
				testutil.MatchesGolden(a.ts, name+"_"+th.name)
				a.noErrors()
			})
		}
	}
}

func TestGoldenZenMode(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name, func(t *testing.T) {
			a, _ := goldenModes(t, th.theme, ui.Size{Width: 960, Height: 560})
			a.run("workbench.action.toggleZenMode")
			testutil.MatchesGolden(a.ts, "zen_mode_"+th.name)
			a.noErrors()
		})
		t.Run(th.name+" with single tab", func(t *testing.T) {
			a, _ := goldenModes(t, th.theme, ui.Size{Width: 960, Height: 560})
			if err := a.win.Settings.Update(context.Background(), "zenMode.showTabs", "single", settings.TargetUser); err != nil {
				t.Fatal(err)
			}
			a.run("workbench.action.toggleZenMode")
			testutil.MatchesGolden(a.ts, "zen_mode_single_tab_"+th.name)
			a.noErrors()
		})
	}
}

func TestGoldenCenteredLayout(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name, func(t *testing.T) {
			a, _ := goldenModes(t, th.theme, ui.Size{Width: 1100, Height: 520})
			a.run("workbench.action.toggleCenteredLayout")
			testutil.MatchesGolden(a.ts, "centered_layout_"+th.name)
			a.noErrors()
		})
	}
}

func TestGoldenAppearanceMenu(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name, func(t *testing.T) {
			a, fake := goldenModes(t, th.theme, ui.Size{Width: 960, Height: 560})
			fake.report(true)
			a.win.Layout.SetCenteredLayout(true)
			a.pump()
			a.showMenu(command.MenubarViewMenu)
			a.ts.Tap(gluttertest.ByText("Appearance"))
			a.pump()
			testutil.MatchesGolden(a.ts, "appearance_menu_"+th.name)
			a.noErrors()
		})
	}
}
