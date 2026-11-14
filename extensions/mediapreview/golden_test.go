package mediapreview

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui/uitest"
)

// ringPNG is a w x h image: a colored ring on a transparent background, so the checkerboard shows inside and around it.
func ringPNG(t *testing.T, w, h int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	cx, cy, r := float64(w)/2, float64(h)/2, float64(min(w, h))/2
	for y := range h {
		for x := range w {
			dx, dy := float64(x)+0.5-cx, float64(y)+0.5-cy
			d := dx*dx + dy*dy
			if d <= r*r && d >= r*r/4 {
				img.SetNRGBA(x, y, color.NRGBA{R: uint8(255 * x / w), G: 120, B: uint8(255 * y / h), A: 255})
			}
		}
	}
	return encode(t, func(b *bytes.Buffer) error { return png.Encode(b, img) })
}

func TestGoldenImagePreview(t *testing.T) {
	svgSource := []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 64 48"><rect x="4" y="4" width="56" height="40" rx="8" fill="#3c8dbc"/><circle cx="32" cy="24" r="14" fill="#f0c040" fill-opacity="0.8"/></svg>`)
	cases := []struct {
		name, file string
		data       func(*testing.T) []byte
		scale      float64
	}{
		{"fitted", "ring.png", func(t *testing.T) []byte { return ringPNG(t, 640, 400) }, 0},
		{"zoomed", "small.png", func(t *testing.T) []byte { return ringPNG(t, 24, 16) }, 5},
		{"svg", "shapes.svg", func(*testing.T) []byte { return svgSource }, 4},
	}
	for _, th := range uitest.Themes() {
		for _, c := range cases {
			t.Run(th.Name+" "+c.name, func(t *testing.T) {
				r := newRig(t)
				fe, p := r.open(r.write(c.file, c.data(t)), nil)
				ts := uitest.Pump(t, th, gui.Size{Width: 480, Height: 320}, widgets.NewBuilder(fe.Content.Build))
				if c.scale != 0 {
					p.setScaleAt(c.scale, 240, 160)
					ts.Pump(time.Millisecond)
				}
				uitest.MatchesGolden(ts, "image_preview_"+c.name+"_"+th.Name)
			})
		}
	}
}
