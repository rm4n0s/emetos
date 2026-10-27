package app

import (
	"flag"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/glutter/services"

	"github.com/rm4n0s/emetos/extension"
)

// goldenTolerance is the largest difference in one channel of one pixel that a golden accepts, as Glutter's goldens do.
const goldenTolerance = 2

// goldenOfView compares the picture the view handed over with testdata/goldens/<name>.png. The goldens are the pictures of the
// software renderer of Glutter, which the offscreen view draws with: a picture drawn by the damage of the frames must be the
// one a whole redraw makes. -update-goldens (the flag of Glutter's golden helper) writes it.
func goldenOfView(t *testing.T, delivered *image.RGBA, name string) {
	t.Helper()
	path := filepath.Join("testdata", "goldens", name+".png")
	if f := flag.Lookup("update-goldens"); f != nil && f.Value.String() == "true" {
		out, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(out, delivered); err != nil {
			t.Fatal(err)
		}
		if err := out.Close(); err != nil {
			t.Fatal(err)
		}
		return
	}
	in, err := os.Open(path)
	if err != nil {
		t.Fatalf("no golden (run with -update-goldens): %v", err)
	}
	want, err := png.Decode(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := in.Close(); err != nil {
		t.Fatal(err)
	}
	if want.Bounds().Size() != delivered.Bounds().Size() {
		t.Fatalf("golden %s: the picture is %v, the golden %v", name, delivered.Bounds().Size(), want.Bounds().Size())
	}
	differing, worst := 0, 0
	b := want.Bounds()
	for y := range b.Dy() {
		for x := range b.Dx() {
			wr, wg, wb, wa := want.At(b.Min.X+x, b.Min.Y+y).RGBA()
			g := delivered.RGBAAt(delivered.Rect.Min.X+x, delivered.Rect.Min.Y+y)
			d := max(absDiff(uint8(wr>>8), g.R), absDiff(uint8(wg>>8), g.G), absDiff(uint8(wb>>8), g.B), absDiff(uint8(wa>>8), g.A))
			if d > goldenTolerance {
				differing++
			}
			worst = max(worst, d)
		}
	}
	if differing > 0 {
		actual := filepath.Join("testdata", "goldens", name+".actual.png")
		if out, err := os.Create(actual); err == nil {
			if err := png.Encode(out, delivered); err != nil {
				t.Error(err)
			}
			if err := out.Close(); err != nil {
				t.Error(err)
			}
		}
		t.Errorf("golden %s: %d pixels differ by more than %d (the largest difference is %d); see %s", name, differing, goldenTolerance, worst, actual)
	}
}

func absDiff(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

// The pictures of a 1280x800 view over a Go file, three of them: the file as the view opens it, with one character typed and
// scrolled. They are what the tile differ is measured on (extensions/remotework/tiles/testdata has copies).
func TestGoldenOfAHeadlessView(t *testing.T) {
	r := newProjectRig(t, extension.Size{Width: 1280, Height: 800}, "Dark Modern")
	opened := r.settle(t)
	goldenOfView(t, opened, "headless_editor")

	r.key('x', services.PhysicalKeyboardKeyKeyX, "x")
	typed := r.settle(t)
	if !differs(opened, typed) {
		t.Fatal("the typed character is not in the picture")
	}
	goldenOfView(t, typed, "headless_typed")

	for range 4 {
		r.view.SendScroll(extension.ScrollEvent{X: 700, Y: 400, DY: 120})
		r.pump()
	}
	scrolled := r.settle(t)
	goldenOfView(t, scrolled, "headless_scrolled")
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}
