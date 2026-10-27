package transport

import (
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// loadPicture reads one of the 1280x800 pictures of a headless view over a Go file that package tiles keeps as fixtures:
// editor (the file as the view opens it), typed (one character typed) and scrolled.
func loadPicture(tb testing.TB, name string) *image.RGBA {
	tb.Helper()
	f, err := os.Open(filepath.Join("..", "tiles", "testdata", name+".png"))
	if err != nil {
		tb.Fatal(err)
	}
	defer func() { _ = f.Close() }() // a file that was only read
	src, err := png.Decode(f)
	if err != nil {
		tb.Fatal(err)
	}
	img := image.NewRGBA(src.Bounds())
	draw.Draw(img, img.Bounds(), src, src.Bounds().Min, draw.Src)
	return img
}

// withID returns a copy of the picture with the number n written in a block of pixels of the first tile, so that a client can tell
// which picture it shows (the first tile changes with every picture, as a status clock would).
func withID(img *image.RGBA, n int) *image.RGBA {
	out := image.NewRGBA(img.Rect)
	copy(out.Pix, img.Pix)
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			o := out.PixOffset(x, y)
			out.Pix[o], out.Pix[o+1], out.Pix[o+2], out.Pix[o+3] = byte(n>>8), byte(n), 0x5a, 0xff
		}
	}
	return out
}

// idOf reads the number withID wrote.
func idOf(img *image.RGBA) int {
	if img == nil {
		return -1
	}
	o := img.PixOffset(0, 0)
	return int(img.Pix[o])<<8 | int(img.Pix[o+1])
}
