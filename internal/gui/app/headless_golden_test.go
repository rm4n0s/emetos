package app

import (
	"flag"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/services"

	"github.com/rm4n0s/emetos/extension"
)

// goldenTolerance is the largest difference in one channel of one pixel that a golden accepts, as Glutter's goldens do.
const goldenTolerance = 2

// goldenWait is how long a picture of the view may take to become the golden's: see settledGolden.
const goldenWait = 90 * time.Second

// updatingGoldens is whether -update-goldens (the flag of Glutter's golden helper) is set.
func updatingGoldens() bool {
	f := flag.Lookup("update-goldens")
	return f != nil && f.Value.String() == "true"
}

// readGolden is the picture testdata/goldens/<name>.png.
func readGolden(t testing.TB, name string) image.Image {
	t.Helper()
	in, err := os.Open(filepath.Join("testdata", "goldens", name+".png"))
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
	return want
}

// goldenDifference is how many pixels of the picture differ from the golden by more than goldenTolerance in a channel, and the
// largest difference in a channel of a pixel. A picture of another size differs in every pixel of the golden.
func goldenDifference(want image.Image, delivered *image.RGBA) (differing, worst int) {
	b := want.Bounds()
	if b.Size() != delivered.Bounds().Size() {
		return b.Dx() * b.Dy(), 255
	}
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
	return differing, worst
}

// goldenOfView compares the picture the view handed over with testdata/goldens/<name>.png. The goldens are the pictures of the
// software renderer of Glutter, which the offscreen view draws with: a picture drawn by the damage of the frames must be the
// one a whole redraw makes. -update-goldens writes it.
func goldenOfView(t *testing.T, delivered *image.RGBA, name string) {
	t.Helper()
	path := filepath.Join("testdata", "goldens", name+".png")
	if updatingGoldens() {
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
	want := readGolden(t, name)
	if want.Bounds().Size() != delivered.Bounds().Size() {
		t.Fatalf("golden %s: the picture is %v, the golden %v", name, delivered.Bounds().Size(), want.Bounds().Size())
	}
	if differing, worst := goldenDifference(want, delivered); differing > 0 {
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

// settledGolden settles the view and compares its picture with the golden, waiting for the golden to come: the view draws its
// frames on a goroutine of its own and drops the ones that a newer frame replaces, and on a loaded machine (the race detector, other
// test runs) that goroutine is seconds behind the UI, so that the picture that settle returns after a quiet second can be one of
// before the last frames (the first scroll without the scrollbars that fade in after it, the folder not listed yet). What the
// view shows is stable by then, and the pictures that come later are the final ones; a picture that never becomes the golden is
// the failure, and the last one is the one reported. The pictures only need a frame to come, so the wait is short when the
// machine is not loaded. It returns the picture that matched, or the last one.
func (r *viewRig) settledGolden(t *testing.T, name string) *image.RGBA {
	t.Helper()
	img := r.settle(t)
	if updatingGoldens() {
		goldenOfView(t, img, name)
		return img
	}
	want := readGolden(t, name)
	deadline := time.Now().Add(goldenWait)
	for {
		if differing, _ := goldenDifference(want, img); differing == 0 || time.Now().After(deadline) {
			break
		}
		r.ts.Pump(20 * time.Millisecond)
		select {
		case img = <-r.frames:
		case <-time.After(10 * time.Millisecond):
		}
	}
	goldenOfView(t, img, name)
	return img
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
	opened := r.settledGolden(t, "headless_editor")

	r.key('x', services.PhysicalKeyboardKeyKeyX, "x")
	typed := r.settledGolden(t, "headless_typed")
	if !differs(opened, typed) {
		t.Fatal("the typed character is not in the picture")
	}

	for range 4 {
		r.view.SendScroll(extension.ScrollEvent{X: 700, Y: 400, DY: 120})
		r.pump()
	}
	r.settledGolden(t, "headless_scrolled")
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

// The Source Control view in the view of a client (step 24.25, D50): the repositories the extension made in the window's model, with the
// changes and the commit box, drawn by the shell of the view, and the status items of the extension beside the editor's.
func TestGoldenOfTheSourceControlViewInAHeadlessView(t *testing.T) {
	r := newProjectRig(t, extension.Size{Width: 1280, Height: 800}, "Dark Modern")
	r.frame(t) // the shell of the view binds its commands when it is first built
	if err := r.win.Commands.Run(commandContext(t, r.win), "dev.scm.fake", map[string]any{"fixture": scmFixture(t, t.TempDir())}); err != nil {
		t.Fatal(err)
	}
	if err := r.view.ws.Commands.Run(commandContext(t, r.view.ws), "workbench.view.scm", nil); err != nil {
		t.Fatal(err)
	}
	r.pump()
	img := r.settledGolden(t, "headless_scm")
	if img == nil {
		t.Fatal("no picture")
	}
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}
