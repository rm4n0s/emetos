package kit

import (
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// updateIcons writes the pictures of the application icon into assets/icons/ (go test ./internal/gui/kit -run
// TestAppIconPNGs -update-icons); without it the test compares the rendering with the files. Look at every picture
// that changed.
var updateIcons = flag.Bool("update-icons", false, "write the application icon pictures to assets/icons")

// renderAppIcon draws the application icon at a size in pixels with the software renderer of the widget tester.
func renderAppIcon(t *testing.T, size int) *image.RGBA {
	t.Helper()
	ts := testutil.New(t, ui.Size{Width: float64(size), Height: float64(size)})
	ts.PumpWidget(NewAppIcon(float64(size)))
	img := ts.Screenshot()
	if img == nil {
		t.Fatalf("no picture at %d px", size)
	}
	return img
}

func TestAppIconPNGs(t *testing.T) {
	dir := filepath.Join(testutil.RepoRoot(t), "assets", "icons")
	for _, size := range AppIconSizes {
		t.Run(fmt.Sprintf("%dpx", size), func(t *testing.T) {
			img := renderAppIcon(t, size)
			if b := img.Bounds(); b.Dx() != size || b.Dy() != size {
				t.Fatalf("the picture is %dx%d, want %dx%d", b.Dx(), b.Dy(), size, size)
			}
			checkAppIconPixels(t, img)
			var buf bytes.Buffer
			if err := png.Encode(&buf, img); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, fmt.Sprintf("emetos-%d.png", size))
			if *updateIcons {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%v (run with -update-icons)", err)
			}
			want, err := png.Decode(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			got, err := png.Decode(bytes.NewReader(buf.Bytes()))
			if err != nil {
				t.Fatal(err)
			}
			if !samePixels(got, want) {
				t.Errorf("%s differs from the rendering (run with -update-icons and look at the picture)", path)
			}
		})
	}
}

// checkAppIconPixels checks what any size of the icon must have: transparent corners, an opaque body, teal around the
// hairball, grey-brown fur in the middle, and a few light pixels of the highlight.
func checkAppIconPixels(t *testing.T, img *image.RGBA) {
	t.Helper()
	size := img.Bounds().Dx()
	opaque, fur, transparent := 0, 0, 0
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			c := img.RGBAAt(x, y)
			switch {
			case c.A < 32:
				transparent++
			case c.A == 255:
				opaque++
				// fur is brown: red above green above blue, and not teal
				if c.R > c.G && c.G > c.B && c.R < 235 {
					fur++
				}
			}
		}
	}
	total := size * size
	if opaque < total/2 {
		t.Errorf("only %d of %d pixels are opaque", opaque, total)
	}
	if fur < total/10 {
		t.Errorf("only %d of %d pixels are fur", fur, total)
	}
	if transparent == 0 {
		t.Errorf("no pixel is transparent: the corners are not rounded")
	}
	if c := img.RGBAAt(0, 0); c.A >= 32 {
		t.Errorf("the top left corner is %v, want transparent", c)
	}
	// the backdrop just inside the top edge, half way across, is teal (the hairball is lower)
	x, y := size/2, max(2, size/16)
	if c := img.RGBAAt(x, y); c.A != 255 || c.G < c.R+40 || c.B < c.R+40 {
		t.Errorf("the backdrop at (%d,%d) is %v, want an opaque teal", x, y, c)
	}
	// the middle of the picture is on the hairball, not on the backdrop
	if c := img.RGBAAt(size/2, size/2); c.A != 255 || c.R <= c.B {
		t.Errorf("the middle at (%d,%d) is %v, want brown fur", size/2, size/2, c)
	}
}

// samePixels tells whether two pictures have the same size and colors.
func samePixels(a, b image.Image) bool {
	if a.Bounds() != b.Bounds() {
		return false
	}
	for y := a.Bounds().Min.Y; y < a.Bounds().Max.Y; y++ {
		for x := a.Bounds().Min.X; x < a.Bounds().Max.X; x++ {
			if color.NRGBAModel.Convert(a.At(x, y)) != color.NRGBAModel.Convert(b.At(x, y)) {
				return false
			}
		}
	}
	return true
}
