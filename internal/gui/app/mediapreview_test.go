package app

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/mediapreview"
	"github.com/rm4n0s/emetos/internal/core/editorgroup"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// Media Preview in the window (step 36.2): images open in it, with its status bar entries, its commands and its link to the text
// editor.

// previewPNG is a 200 x 120 image: a gradient with a transparent band across its middle, where the checkerboard shows.
func previewPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 200, 120))
	for y := range 120 {
		for x := range 200 {
			if y >= 50 && y < 70 {
				continue
			}
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x), G: uint8(2 * y), B: 160, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// mediaPreviewApp is a window over a folder with photo.png, broken.png (text) and notes.txt, with Media Preview, in a theme.
func mediaPreviewApp(t *testing.T, theme string) (*groupApp, string) {
	t.Helper()
	paths := testutil.Isolate(t)
	testutil.Settings(t, paths, map[string]any{
		"window.menuBarVisibility": "classic", "workbench.colorTheme": theme, "editor.minimap.enabled": false,
		"breadcrumbs.enabled": false, "files.hotExit": "off",
	})
	folder := filepath.Join(t.TempDir(), "project")
	if err := os.MkdirAll(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"photo.png": previewPNG(t), "broken.png": []byte("not an image\n"), "notes.txt": []byte("notes\n")} {
		if err := os.WriteFile(filepath.Join(folder, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p := newTestProcess(ProcessOptions{Paths: paths, Extensions: []extension.Factory{mediapreview.New}})
	win, err := NewWorkspace(context.Background(), p, folder)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(win.release)
	a := newGroupAppWith(t, win, ui.Size{Width: 960, Height: 480}, nil, nil)
	a.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) { c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12 })
	a.run("workbench.action.toggleSidebarVisibility")
	return a, folder
}

func TestAnImageOpensInMediaPreview(t *testing.T) {
	a, folder := mediaPreviewApp(t, "Default Dark Modern")
	a.openFile(filepath.Join(folder, "photo.png"), false)
	in, ok := a.activeInput().(*editorgroup.FileEditorInput)
	if !ok || in.Editor() != mediapreview.ImageEditorID {
		t.Fatalf("the active editor is %#v", a.activeInput())
	}
	// the image is read on a goroutine; its entries show when it is
	waitForRequirements(t, a, func() bool { return slices.Contains(a.statusTexts(), "Whole Image") })
	for _, want := range []string{"Whole Image", "200x120"} {
		if !slices.Contains(a.statusTexts(), want) {
			t.Errorf("no %q in %v", want, a.statusTexts())
		}
	}
	// VS Code's key of the custom editor in front, for the when-clauses of the commands
	if got := a.win.Keys.Value("activeCustomEditorId"); got != mediapreview.ImageEditorID {
		t.Errorf("activeCustomEditorId = %v", got)
	}
	a.run(mediapreview.CommandZoomIn)
	a.pump()
	if !slices.Contains(a.statusTexts(), "150%") {
		t.Errorf("after Zoom in: %v", a.statusTexts())
	}
	// Copy puts the image on the clipboard as PNG
	a.run(mediapreview.CommandCopyImage)
	a.pump()
	copied, err := png.Decode(bytes.NewReader(a.ts.ClipboardPNG()))
	if err != nil || copied.Bounds().Dx() != 200 {
		t.Errorf("the clipboard has %v (%v)", copied, err)
	}
	// another editor in front: the key and the entries go
	a.openFile(filepath.Join(folder, "notes.txt"), false)
	waitForRequirements(t, a, func() bool { _, ok := a.activeInput().(*editorgroup.TextInput); return ok })
	if got := a.win.Keys.Value("activeCustomEditorId"); got != nil {
		t.Errorf("activeCustomEditorId = %v with a text editor in front", got)
	}
	if slices.Contains(a.statusTexts(), "200x120") {
		t.Errorf("the entries stay: %v", a.statusTexts())
	}
}

func TestAnImageThatCannotBeReadOpensAsText(t *testing.T) {
	a, folder := mediaPreviewApp(t, "Default Dark Modern")
	broken := filepath.Join(folder, "broken.png")
	a.openFile(broken, false)
	link := gluttertest.ByText("Open file using Emetos's standard text/binary editor?")
	waitForRequirements(t, a, func() bool { return a.ts.Exists(link) })
	if !a.ts.Exists(gluttertest.ByText("An error occurred while loading the image.")) {
		t.Error("no message")
	}
	a.ts.Tap(link)
	waitForRequirements(t, a, func() bool { _, ok := a.activeInput().(*editorgroup.TextInput); return ok })
	if in, ok := a.activeInput().(*editorgroup.TextInput); !ok || len(a.tabNames()) != 1 {
		t.Errorf("active %#v, tabs %v", in, a.tabNames())
	}
}

// TestGoldenMediaPreview shows an image in Media Preview with its three status bar entries, in both themes.
func TestGoldenMediaPreview(t *testing.T) {
	for _, th := range goldenThemesOfThePanels {
		t.Run(th.name, func(t *testing.T) {
			a, folder := mediaPreviewApp(t, th.theme)
			a.shell().openPath(widgets.WidgetsBindingOf(a.shell().Context()), nil, filepath.Join(folder, "photo.png"), false)
			waitForRequirements(t, a, func() bool { return slices.Contains(a.statusTexts(), "Whole Image") })
			a.win.Notify.ClearAll()
			a.shell().SetState(func() {})
			a.pump()
			testutil.MatchesGolden(a.ts, "media_preview_"+th.name)
		})
	}
}
