package app

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The goldens of the theme pickers (step 8.4): the color theme picker, the picker while it previews the other kind
// of theme, the file icon theme picker, the Explorer with no file icons and the Themes submenus.

// themeGolden is the Explorer of a small project in a theme with the picker command run, and the caret of the
// input not yet blinking.
func themeGolden(t *testing.T, th, picker string) *explorerApp {
	t.Helper()
	a := goldenExplorer(t, th, true, "main.go")
	if picker != "" {
		if err := a.run(picker, nil); err != nil {
			t.Fatal(err)
		}
		a.waitFor("the picker", func() bool { return a.win.QuickInput.Visible() })
	}
	a.pump()
	return a
}

// pressKey sends a key to the window.
func (a *explorerApp) pressKey(keys string) {
	a.t.Helper()
	kb, err := keybinding.ParseKeybinding(keys)
	if err != nil {
		a.t.Fatal(err)
	}
	for _, chord := range kb {
		sendChord(a.ts, chord)
	}
	a.pump()
}

func TestGoldenColorThemePicker(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name, func(t *testing.T) {
			a := themeGolden(t, th.theme, "workbench.action.selectTheme")
			testutil.MatchesGolden(a.ts, "theme_picker_"+th.name)
			a.noErrors()
		})
	}
}

func (a *explorerApp) noErrors() {
	a.t.Helper()
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		a.t.Errorf("errors: %v", errs)
	}
}

func TestGoldenColorThemePickerPreview(t *testing.T) {
	tests := []struct {
		name  string
		theme string
		keys  []string // from the theme in use to the one previewed
		want  string
	}{
		{"light over dark", "Dark Modern", []string{"up", "up"}, "Light Modern"},
		{"dark over light", "Light Modern", []string{"down", "down"}, "Dark Modern"},
		{"high contrast over dark", "Dark Modern", []string{"down", "down"}, "Dark High Contrast"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := themeGolden(t, tc.theme, "workbench.action.selectTheme")
			for _, k := range tc.keys {
				a.pressKey(k)
			}
			if got := a.win.Themes.CurrentInfo().Label; got != tc.want {
				t.Fatalf("the preview is %q, want %q", got, tc.want)
			}
			testutil.MatchesGolden(a.ts, "theme_picker_preview_"+goldenName(tc.name))
			a.noErrors()
		})
	}
}

// goldenName makes a file name of a test name.
func goldenName(name string) string {
	out := []byte(name)
	for i, c := range out {
		if c == ' ' {
			out[i] = '_'
		}
	}
	return string(out)
}

func TestGoldenFileIconThemePicker(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name, func(t *testing.T) {
			a := themeGolden(t, th.theme, "workbench.action.selectIconTheme")
			testutil.MatchesGolden(a.ts, "icon_theme_picker_"+th.name)
			a.noErrors()
		})
	}
}

func TestGoldenExplorerWithoutIcons(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name, func(t *testing.T) {
			a := themeGolden(t, th.theme, "workbench.action.selectIconTheme")
			a.pressKey("up") // None
			if a.ts.Exists(gluttertest.ByText(goGlyph)) {
				t.Fatal("the Go icon is still drawn")
			}
			a.pressKey("enter")
			a.ts.Pump(300 * time.Millisecond)
			a.pump()
			testutil.MatchesGolden(a.ts, "explorer_no_icons_"+th.name)
			a.noErrors()
		})
	}
}

func TestGoldenThemesMenus(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name+" the Preferences submenu", func(t *testing.T) {
			a := themeGolden(t, th.theme, "")
			// the classic menu bar, whose File menu this image shows (the compact menu has its own images)
			if err := a.win.Settings.Update(t.Context(), "window.menuBarVisibility", "classic", settings.TargetUser); err != nil {
				t.Fatal(err)
			}
			// wide enough for the submenus to open to the right
			a.ts.SetViewSize(ui.Size{Width: 1100, Height: 460})
			a.pump()
			s := a.shell()
			s.menuBar.Open(s.menus.BarIndex(command.MenubarFileMenu))
			a.ts.Pump(300 * time.Millisecond)
			a.pump()
			a.ts.Tap(gluttertest.ByText("Preferences"))
			a.ts.Pump(300 * time.Millisecond)
			a.pump()
			a.ts.Tap(gluttertest.ByText("Themes"))
			a.ts.Pump(300 * time.Millisecond)
			a.pump()
			testutil.MatchesGolden(a.ts, "themes_menu_"+th.name)
			a.noErrors()
		})
		t.Run(th.name+" the Manage menu", func(t *testing.T) {
			a := themeGolden(t, th.theme, "")
			if err := a.run("dev.openMenu", map[string]any{"id": command.GlobalActivity}); err != nil {
				t.Fatal(err)
			}
			a.ts.Pump(300 * time.Millisecond)
			a.pump()
			a.ts.Tap(gluttertest.ByText("Themes"))
			a.ts.Pump(300 * time.Millisecond)
			a.pump()
			if len(a.ts.Elements(gluttertest.ByType[*kit.Menu]())) < 2 {
				t.Error("the Themes submenu is not open")
			}
			testutil.MatchesGolden(a.ts, "manage_menu_themes_"+th.name)
			a.noErrors()
		})
	}
}
