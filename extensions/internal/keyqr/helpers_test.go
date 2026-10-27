package keyqr

import (
	"image"
	"image/color"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	xdraw "golang.org/x/image/draw"
)

// testLines are one public line for each kind of key the round trip is tested with, read from testdata/*.pub: the public halves
// of keys made for this purpose, whose private halves were thrown away (no private key is in the repository).
func testLines(t testing.TB) map[string]string {
	t.Helper()
	lines := map[string]string{}
	for name, file := range map[string]string{"ed25519": "ed25519.pub", "ecdsa": "ecdsa.pub", "rsa-4096": "rsa4096.pub"} {
		data, err := os.ReadFile(filepath.Join("testdata", file))
		if err != nil {
			t.Fatal(err)
		}
		lines[name] = strings.TrimSpace(string(data))
	}
	return lines
}

// scaledTo scales an image to a square of the given side with a bilinear filter, as a camera's lens and sensor do.
func scaledTo(src image.Image, side int) *image.Gray {
	dst := image.NewGray(image.Rect(0, 0, side, side))
	xdraw.BiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	return dst
}

// frameWith draws a gray image on a gray frame of w by h pixels, the way a code held in front of a camera looks.
func frameWith(code image.Image, w, h int, background uint8) *image.Gray {
	frame := image.NewGray(image.Rect(0, 0, w, h))
	for i := range frame.Pix {
		frame.Pix[i] = background
	}
	b := code.Bounds()
	at := image.Pt((w-b.Dx())/2, (h-b.Dy())/2)
	xdraw.Draw(frame, image.Rectangle{Min: at, Max: at.Add(b.Size())}, code, b.Min, xdraw.Src)
	return frame
}

// rotated turns an image by degrees around its middle, the corners that come from outside are white.
func rotated(src *image.Gray, degrees float64) *image.Gray {
	b := src.Bounds()
	dst := image.NewGray(b)
	sin, cos := math.Sincos(degrees * math.Pi / 180)
	cx, cy := float64(b.Dx())/2, float64(b.Dy())/2
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			dx, dy := float64(x)-cx, float64(y)-cy
			sx, sy := cos*dx+sin*dy+cx, -sin*dx+cos*dy+cy
			dst.SetGray(x, y, color.Gray{Y: bilinear(src, sx, sy)})
		}
	}
	return dst
}

// bilinear reads an image between its pixels; outside it is white.
func bilinear(src *image.Gray, x, y float64) uint8 {
	b := src.Bounds()
	if x < 0 || y < 0 || x >= float64(b.Dx()-1) || y >= float64(b.Dy()-1) {
		return 255
	}
	x0, y0 := int(x), int(y)
	fx, fy := x-float64(x0), y-float64(y0)
	p := func(px, py int) float64 { return float64(src.GrayAt(px, py).Y) }
	v := p(x0, y0)*(1-fx)*(1-fy) + p(x0+1, y0)*fx*(1-fy) + p(x0, y0+1)*(1-fx)*fy + p(x0+1, y0+1)*fx*fy
	return uint8(v + 0.5)
}

// blurred is a box blur of a radius in pixels.
func blurred(src *image.Gray, radius int) *image.Gray {
	b := src.Bounds()
	dst := image.NewGray(b)
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			sum, n := 0, 0
			for dy := -radius; dy <= radius; dy++ {
				for dx := -radius; dx <= radius; dx++ {
					if px, py := x+dx, y+dy; px >= 0 && py >= 0 && px < b.Dx() && py < b.Dy() {
						sum += int(src.GrayAt(px, py).Y)
						n++
					}
				}
			}
			dst.SetGray(x, y, color.Gray{Y: uint8(sum / n)})
		}
	}
	return dst
}

// noisy adds Gaussian noise of a standard deviation (in gray levels) from a seed.
func noisy(src *image.Gray, sigma float64, seed int64) *image.Gray {
	rng := rand.New(rand.NewSource(seed))
	dst := image.NewGray(src.Bounds())
	for i, v := range src.Pix {
		dst.Pix[i] = uint8(math.Max(0, math.Min(255, float64(v)+rng.NormFloat64()*sigma)))
	}
	return dst
}
