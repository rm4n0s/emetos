package app

import (
	"context"
	"testing"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// TestGoldenStatusBarProfile shows the status bar of a window with the profile Work (a copy of the default profile, so
// the theme is the same): the item of the profile at the right of the language and before the bell. The window with the
// default profile shows no item (the goldens of the status bar have not changed).
func TestGoldenStatusBarProfile(t *testing.T) {
	for _, th := range goldenThemesOfThePanels {
		t.Run(th.name, func(t *testing.T) {
			a := goldenPanels(t, th.theme, ui.Size{Width: 960, Height: 420})
			ctx := context.Background()
			if _, err := a.win.Process.Profiles.Create(ctx, "Work", storage.CreateFrom(storage.DefaultProfile)); err != nil {
				t.Fatal(err)
			}
			if err := a.win.SwitchProfile(ctx, "Work"); err != nil {
				t.Fatal(err)
			}
			a.shell().SetState(func() {})
			a.pump()
			testutil.MatchesGolden(a.ts, "statusbar_profile_"+th.name)
			a.noErrors()
		})
	}
}
