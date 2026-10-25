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

// checkAppIconPixels checks what any size of the icon must have: transparent corners, an opaque body, a mark that is
// white, and indigo around it.
func checkAppIconPixels(t *testing.T, img *image.RGBA) {
	t.Helper()
	size := img.Bounds().Dx()
	opaque, white, transparent := 0, 0, 0
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			c := img.RGBAAt(x, y)
			switch {
			case c.A < 32:
				transparent++
			case c.A == 255:
				opaque++
				if c.R > 240 && c.G > 240 && c.B > 240 {
					white++
				}
			}
		}
	}
	total := size * size
	if opaque < total/2 {
		t.Errorf("only %d of %d pixels are opaque", opaque, total)
	}
	if white < total/20 {
		t.Errorf("only %d of %d pixels belong to the white mark", white, total)
	}
	if transparent == 0 {
		t.Errorf("no pixel is transparent: the corners are not rounded")
	}
	if c := img.RGBAAt(0, 0); c.A >= 32 {
		t.Errorf("the top left corner is %v, want transparent", c)
	}
	// the body just inside the left edge, half way down, is the indigo of the body (the mark is farther in)
	x, y := max(1, size/20), size/2
	if c := img.RGBAAt(x, y); c.A != 255 || c.B < c.R || c.B < 150 {
		t.Errorf("the body at (%d,%d) is %v, want an opaque indigo", x, y, c)
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
