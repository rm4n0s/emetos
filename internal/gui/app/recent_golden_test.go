package app

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The goldens of the recently opened list: the Open Recent submenu of the File menu and the Open Recent picker.

func goldenRecent(t *testing.T, theme string) *stateRun {
	t.Helper()
	env := newStateEnv(t, map[string]any{"workbench.colorTheme": theme, "editor.minimap.enabled": false, "breadcrumbs.enabled": false})
	env.size = ui.Size{Width: 900, Height: 460}
	env.prepare = func(ts *gluttertest.Tester) {
		ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
	}
	other := env.folder("glutter", "g.go", "pkg/h.go")
	first := env.start(other)
	first.open(filepath.Join(other, "g.go"), 0)
	first.open(filepath.Join(other, "pkg/h.go"), 0)
	first.close()
	run := env.start(env.root)
	run.open(env.path("main.go"), 0)
	run.open(env.path("src/a.go"), 0)
	run.command("workbench.action.closeActiveEditor", nil) // Reopen Closed Editor is enabled
	return run
}

func (r *stateRun) press(keys string) {
	r.env.t.Helper()
	kb, err := keybinding.ParseKeybinding(keys)
	if err != nil {
		r.env.t.Fatal(err)
	}
	for i, chord := range kb {
		if !sendChord(r.ts, chord) && i == len(kb)-1 {
			r.env.t.Errorf("the keys %s were not used", keys)
		}
	}
	r.pump()
}

func TestGoldenOpenRecent(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name+" submenu", func(t *testing.T) {
			r := goldenRecent(t, th.theme)
			s := r.shell()
			s.menuBar.Open(s.menus.BarIndex(command.MenubarFileMenu))
			r.ts.Pump(300 * time.Millisecond)
			r.pump()
			// New Text File, New Window, Open Folder, Open Recent
			r.press("down")
			r.press("down")
			r.press("down")
			r.press("down")
			r.press("right")
			r.ts.Pump(300 * time.Millisecond)
			r.pump()
			testutil.MatchesGolden(r.ts, "open_recent_menu_"+th.name)
			if errs := r.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
		t.Run(th.name+" picker", func(t *testing.T) {
			r := goldenRecent(t, th.theme)
			r.command("workbench.action.openRecent", nil)
			r.waitFor("the picker", func() bool { return r.win.QuickInput.Visible() })
			// the caret blinks every 500 ms of pumped time after the focus: pump little
			r.pump()
			testutil.MatchesGolden(r.ts, "open_recent_picker_"+th.name)
			if errs := r.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}
