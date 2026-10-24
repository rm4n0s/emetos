package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/storage"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// themedWindow makes a window over a process with isolated folders whose user settings are
// extra (plus the editor font of the tests).
func themedWindow(t *testing.T, extra map[string]any) (*Window, storage.Paths) {
	t.Helper()
	paths := testutil.Isolate(t)
	if extra == nil {
		extra = map[string]any{}
	}
	testutil.Settings(t, paths, extra)
	win, err := NewWindow(context.Background(), newTestProcess(ProcessOptions{Paths: paths}), "")
	if err != nil {
		t.Fatalf("NewWindow: %v", err)
	}
	return win, paths
}

// uiBackground is the editor background of the theme the app below shows.
func uiBackground(t *testing.T, ts *gluttertest.Tester) ui.Color {
	t.Helper()
	return ts.Widget(gluttertest.ByType[*kit.Theme]()).(*kit.Theme).Colors().EditorBackground
}

func useBrightness(ts *gluttertest.Tester, b ui.Brightness) {
	ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.PlatformBrightness = b })
}

func TestTheUIHasTheColorsOfTheTheme(t *testing.T) {
	win, _ := themedWindow(t, nil)
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, nil, nil))
	if got := uiBackground(t, ts); got != ui.NewColor(0xFF1F1F1F) {
		t.Errorf("the default theme is Dark Modern: %v", got)
	}
	colors := ts.Widget(gluttertest.ByType[*kit.Theme]()).(*kit.Theme).Colors()
	if colors != kit.DarkModern() {
		t.Error("the colors are those of the Dark Modern theme")
	}
	noErrors(t, ts)
}

func TestTheThemeSettingChoosesTheTheme(t *testing.T) {
	win, _ := themedWindow(t, map[string]any{"workbench.colorTheme": "Light Modern"})
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, nil, nil))
	if got := uiBackground(t, ts); got != ui.NewColor(0xFFFFFFFF) {
		t.Errorf("Light Modern: %v", got)
	}
	// A change of the setting changes the theme while the app runs.
	if err := win.Settings.Update(context.Background(), "workbench.colorTheme", "Dark High Contrast", settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	ts.Pump(50 * time.Millisecond)
	if got := uiBackground(t, ts); got != ui.NewColor(0xFF000000) {
		t.Errorf("Dark High Contrast: %v", got)
	}
	noErrors(t, ts)
}

func TestTheThemeFollowsTheSystemBrightness(t *testing.T) {
	win, _ := themedWindow(t, map[string]any{
		"window.autoDetectColorScheme":      true,
		"workbench.preferredDarkColorTheme": "Dark+",
		// Light Modern is the default of the light preference.
		"workbench.colorTheme": "Light+",
	})
	ts := testutil.New(t, testWindowSize)
	useBrightness(ts, ui.BrightnessDark)
	ts.PumpWidget(newApp(context.Background(), win, nil, nil))
	if got := uiBackground(t, ts); got != ui.NewColor(0xFF1E1E1E) {
		t.Errorf("a dark system gets Dark+: %v", got)
	}
	useBrightness(ts, ui.BrightnessLight)
	ts.Pump(50 * time.Millisecond)
	if got := uiBackground(t, ts); got != ui.NewColor(0xFFFFFFFF) {
		t.Errorf("a light system gets Light Modern: %v", got)
	}
	useBrightness(ts, ui.BrightnessDark)
	ts.Pump(50 * time.Millisecond)
	if got := uiBackground(t, ts); got != ui.NewColor(0xFF1E1E1E) {
		t.Errorf("the system turns dark again: %v", got)
	}
	// Without the setting the system is ignored.
	if err := win.Settings.Update(context.Background(), "window.autoDetectColorScheme", false, settings.TargetUser); err != nil {
		t.Fatal(err)
	}
	ts.Pump(50 * time.Millisecond)
	if got := uiBackground(t, ts); got != ui.NewColor(0xFFFFFFFF) {
		t.Errorf("Light+ from workbench.colorTheme: %v", got)
	}
	noErrors(t, ts)
}

func TestAThemeThatIsNotThereIsAWarning(t *testing.T) {
	win, _ := themedWindow(t, map[string]any{"workbench.colorTheme": "Nope"})
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, nil, nil))
	if got := uiBackground(t, ts); got != ui.NewColor(0xFF1F1F1F) {
		t.Errorf("the default theme is used: %v", got)
	}
	var warned bool
	for _, item := range win.Notify.Items() {
		if item.Severity == notify.Warning && item.Source == "Emetos" {
			warned = true
		}
	}
	if !warned {
		t.Errorf("a warning was expected: %+v", win.Notify.Items())
	}
}

func TestUserThemesComeFromTheConfigFolder(t *testing.T) {
	paths := testutil.Isolate(t)
	dir := paths.ThemesDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	theme := `{"name": "Mine", "type": "light", "colors": {"editor.background": "#fafafa"}}`
	if err := os.WriteFile(filepath.Join(dir, "mine.json"), []byte(theme), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.Settings(t, paths, map[string]any{"workbench.colorTheme": "Mine"})
	win, err := NewWindow(context.Background(), newTestProcess(ProcessOptions{Paths: paths}), "")
	if err != nil {
		t.Fatal(err)
	}
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, nil, nil))
	if got := uiBackground(t, ts); got != ui.NewColor(0xFFFAFAFA) {
		t.Errorf("the user's theme: %v", got)
	}
	if n := len(win.Notify.Items()); n != 0 {
		t.Errorf("a theme that is in the folder is no warning: %+v", win.Notify.Items())
	}
}

func noErrors(t *testing.T, ts *gluttertest.Tester) {
	t.Helper()
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("the framework reported errors: %v", errs)
	}
}
