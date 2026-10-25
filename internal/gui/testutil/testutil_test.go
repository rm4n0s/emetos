package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
)

func TestFontsAreGoMonoAndTheIconFonts(t *testing.T) {
	fonts := Fonts()
	counts := map[string]int{}
	for _, f := range fonts {
		if len(f.Data) == 0 {
			t.Errorf("font %q has no data", f.Family)
		}
		counts[f.Family]++
	}
	// and the font of the tab (step 10.1: fonts.Controls, which the editor puts first in a row with a tab)
	if counts[MonoFamily] != 4 || counts["codicon"] != 1 || counts["seti"] != 1 || counts["emetos-controls"] != 1 || len(fonts) != 7 {
		t.Errorf("fonts by family: %v", counts)
	}
}

// TestTheTestFontIsUsed renders text in Go Mono and checks the screenshot has ink: a font
// that failed to load would draw nothing.
func TestTheTestFontIsUsed(t *testing.T) {
	ts := New(t, ui.Size{Width: 200, Height: 60})
	style := painting.NewTextStyle(painting.TextStyleOpts{
		FontFamily: MonoFamily, FontSize: foundation.Some(24.0), Color: foundation.Some(ui.NewColor(0xFFFFFFFF)),
	})
	ts.PumpWidget(widgets.NewDirectionality(ui.TextDirectionLTR,
		widgets.NewColoredBox(ui.NewColor(0xFF000000),
			widgets.NewText("MMMM iiii", widgets.TextOpts{Style: style}))))
	if err := gluttertest.FindsOneWidget(ts, gluttertest.ByText("MMMM iiii")); err != nil {
		t.Fatal(err)
	}
	img := ts.Screenshot()
	white := 0
	for y := 0; y < img.Bounds().Dy(); y++ {
		for x := 0; x < img.Bounds().Dx(); x++ {
			if r, _, _, _ := img.At(x, y).RGBA(); r > 0x8000 {
				white++
			}
		}
	}
	if white < 50 {
		t.Errorf("the text left only %d light pixels", white)
	}
	// In a monospace font "iiii" is as wide as "MMMM": the font is the one asked for.
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestIsolateAndSettings(t *testing.T) {
	paths := Isolate(t)
	home, _ := os.UserHomeDir()
	if !strings.HasPrefix(paths.ConfigHome, os.Getenv("HOME")) && !strings.Contains(paths.ConfigHome, "config") {
		t.Errorf("config home %q", paths.ConfigHome)
	}
	if strings.HasPrefix(paths.ConfigHome, "/home/") && strings.HasPrefix(paths.ConfigHome, home+"/.config") {
		t.Errorf("the real configuration folder is in use: %s", paths.ConfigHome)
	}
	store := Settings(t, paths, map[string]any{"editor.fontSize": 18.0})
	if got := settings.Get[string](store, "editor.fontFamily", ""); got != MonoFamily {
		t.Errorf("editor.fontFamily = %q", got)
	}
	if got := settings.Get[float64](store, "editor.fontSize", ""); got != 18 {
		t.Errorf("editor.fontSize = %v", got)
	}
	userPath, err := settings.UserSettingsPath(paths, storage.DefaultProfile)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(userPath)
	if err != nil || !strings.Contains(string(data), MonoFamily) {
		t.Errorf("the settings file: %v %q", err, data)
	}
}

func TestRepoRoot(t *testing.T) {
	root := RepoRoot(t)
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Errorf("RepoRoot = %q: %v", root, err)
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "SPEC.md")); err != nil {
		t.Errorf("RepoRoot = %q is not the repository: %v", root, err)
	}
}

func TestProofDoesNothingWithoutTheFlag(t *testing.T) {
	if ProofEnabled() {
		t.Skip("the run has -proof")
	}
	ts := New(t, ui.Size{Width: 50, Height: 50})
	ts.PumpWidget(widgets.NewDirectionality(ui.TextDirectionLTR, widgets.NewSizedBox(nil)))
	Proof(t, ts, "never-written.png")
	if _, err := os.Stat(filepath.Join(RepoRoot(t), "docs", "implemented-proof-images", "never-written.png")); err == nil {
		t.Error("a proof image was written without -proof")
	}
}
