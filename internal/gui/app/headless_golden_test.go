package app

import (
	"image"
	"log/slog"
	"testing"

	"github.com/rm4n0s/glutter/services"

	"github.com/rm4n0s/emetos/extension"
)

// goldenOfView compares the picture the view last handed over with the golden. The golden is the picture of the scene made by
// Glutter's software renderer, which is what Tester.MatchesGolden compares, so the renderer of the view is asked once more
// for the same scene and the two pictures must be the same.
func goldenOfView(t *testing.T, r *viewRig, delivered *image.RGBA, name string) {
	t.Helper()
	rast := &rasterizer{log: slog.Default()}
	t.Cleanup(rast.reset)
	img, err := rast.render(r.view.eng.lastScene, delivered.Rect.Dx(), delivered.Rect.Dy())
	if err != nil {
		t.Fatal(err)
	}
	if differs(img, delivered) {
		t.Error("the picture the view handed over is not the one of its last scene")
	}
	rast.tester.MatchesGolden(name)
}

// The pictures of a 1280x800 view over a Go file, three of them: the file as the view opens it, with one character typed and
// scrolled. They are what the tile differ is measured on (extensions/remotework/tiles/testdata has copies).
func TestGoldenOfAHeadlessView(t *testing.T) {
	if raceEnabled {
		t.Skip("the software renderer draws a 1280x800 picture about a hundred times slower under the race detector; the other tests of the view run under it")
	}
	r := newProjectRig(t, extension.Size{Width: 1280, Height: 800}, "Dark Modern")
	opened := r.frame(t)
	goldenOfView(t, r, opened, "headless_editor")

	r.key('x', services.PhysicalKeyboardKeyKeyX, "x")
	typed := r.frame(t)
	if !differs(opened, typed) {
		t.Fatal("the typed character is not in the picture")
	}
	goldenOfView(t, r, typed, "headless_typed")

	for range 4 {
		r.view.SendScroll(extension.ScrollEvent{X: 700, Y: 400, DY: 120})
		r.pump()
	}
	scrolled := r.frame(t)
	goldenOfView(t, r, scrolled, "headless_scrolled")
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}
