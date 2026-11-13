package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// goldenViewersApp is a window over a folder with cat.png and the extension Viewers, in a theme, for the goldens of step 36.1.
func goldenViewersApp(t *testing.T, theme string, values map[string]any) (*groupApp, string) {
	t.Helper()
	paths := testutil.Isolate(t)
	all := map[string]any{
		"window.menuBarVisibility": "classic", "workbench.colorTheme": theme, "editor.minimap.enabled": false,
		"breadcrumbs.enabled": false, "files.hotExit": "off",
	}
	for k, v := range values {
		all[k] = v
	}
	testutil.Settings(t, paths, all)
	folder := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "cat.png"), pngBytes, 0o644); err != nil {
		t.Fatal(err)
	}
	p := newTestProcess(ProcessOptions{Paths: paths, Extensions: []extension.Factory{viewersFactory(&viewerLog{})}})
	win, err := NewWorkspace(context.Background(), p, folder)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(win.release)
	a := newGroupAppWith(t, win, ui.Size{Width: 960, Height: 480}, nil, nil)
	a.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
	a.run("workbench.action.toggleSidebarVisibility")
	return a, filepath.Join(folder, "cat.png")
}

// TestGoldenReopenEditorWith shows the pick of Reopen Editor With over a PNG in its image preview, in both themes.
func TestGoldenReopenEditorWith(t *testing.T) {
	for _, th := range goldenThemesOfThePanels {
		t.Run(th.name, func(t *testing.T) {
			a, cat := goldenViewersApp(t, th.theme, nil)
			a.shell().openPath(widgets.WidgetsBindingOf(a.shell().Context()), nil, cat, false)
			a.pump()
			a.run("workbench.action.reopenWithEditor")
			a.win.Notify.ClearAll()
			a.shell().SetState(func() {})
			a.pump()
			testutil.MatchesGolden(a.ts, "reopen_editor_with_"+th.name)
		})
	}
}

// TestGoldenBinaryNoticeOpenWith shows the notice of a file that looks binary, opened as text by association, with Open Anyway and
// Open With..., in both themes.
func TestGoldenBinaryNoticeOpenWith(t *testing.T) {
	for _, th := range goldenThemesOfThePanels {
		t.Run(th.name, func(t *testing.T) {
			a, cat := goldenViewersApp(t, th.theme, map[string]any{editorAssociationsSetting: map[string]any{"*.png": "default"}})
			a.shell().openPath(widgets.WidgetsBindingOf(a.shell().Context()), nil, cat, false)
			waitForRequirements(t, a, func() bool { return a.shell().blocked == cat })
			a.win.Notify.ClearAll()
			a.shell().SetState(func() {})
			a.pump()
			testutil.MatchesGolden(a.ts, "binary_notice_open_with_"+th.name)
		})
	}
}
