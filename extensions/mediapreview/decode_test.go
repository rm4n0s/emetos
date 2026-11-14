package mediapreview

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"golang.org/x/image/bmp"
	"golang.org/x/image/tiff"
)

// route fails the test unless err is an *errors.Error whose route has want.
func route(t *testing.T, err error, want string) {
	t.Helper()
	e, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	if !e.HasRoute(want) {
		t.Errorf("route %s, want %s", e.Route(), want)
	}
}

// testImage is a 3 x 2 image with a transparent pixel.
func testImage() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 3, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	img.SetNRGBA(1, 0, color.NRGBA{G: 255, A: 255})
	img.SetNRGBA(2, 0, color.NRGBA{B: 255, A: 255})
	img.SetNRGBA(0, 1, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	img.SetNRGBA(1, 1, color.NRGBA{A: 0})
	img.SetNRGBA(2, 1, color.NRGBA{R: 10, G: 20, B: 30, A: 128})
	return img
}

func encode(t *testing.T, f func(*bytes.Buffer) error) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := f(&buf); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func pngData(t *testing.T) []byte {
	return encode(t, func(b *bytes.Buffer) error { return png.Encode(b, testImage()) })
}

// threeFrameGIF is an animated GIF of three 2 x 2 frames shown 50 and 100 milliseconds, and the third with no delay (100 ms, as
// browsers), played twice.
func threeFrameGIF(t *testing.T) []byte {
	pal := color.Palette{color.Black, color.White, color.NRGBA{R: 255, A: 255}}
	g := &gif.GIF{LoopCount: 1}
	for i, delay := range []int{5, 10, 0} {
		f := image.NewPaletted(image.Rect(0, 0, 2, 2), pal)
		f.SetColorIndex(0, 0, uint8(i))
		g.Image, g.Delay = append(g.Image, f), append(g.Delay, delay)
	}
	return encode(t, func(b *bytes.Buffer) error { return gif.EncodeAll(b, g) })
}

// webpLossless is a 1 x 1 lossless WebP (the probe of Modernizr): Go has no WebP encoder.
const webpLossless = "UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA=="

// icoFile is an ICO file with the entries, each its size, its bits per pixel and its body.
func icoFile(entries ...icoTestEntry) []byte {
	var b bytes.Buffer
	le := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	le(uint16(0))
	le(uint16(1))
	le(uint16(len(entries)))
	offset := 6 + 16*len(entries)
	for _, e := range entries {
		b.WriteByte(byte(e.size % 256))
		b.WriteByte(byte(e.size % 256))
		b.WriteByte(0)
		b.WriteByte(0)
		le(uint16(1))
		le(uint16(e.bpp))
		le(uint32(len(e.body)))
		le(uint32(offset))
		offset += len(e.body)
	}
	for _, e := range entries {
		b.Write(e.body)
	}
	return b.Bytes()
}

type icoTestEntry struct {
	size, bpp int
	body      []byte
}

// dib24 is the DIB of a w x w image of 24 bits per pixel, all the color c, with an AND mask that makes the top left pixel transparent.
func dib24(w int, c color.NRGBA) []byte {
	var b bytes.Buffer
	le := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	le(uint32(40))
	le(int32(w))
	le(int32(2 * w))
	le(uint16(1))
	le(uint16(24))
	le(uint32(0))
	le(uint32(0))
	le(int32(0))
	le(int32(0))
	le(uint32(0))
	le(uint32(0))
	stride := (w*24 + 31) / 32 * 4
	for range w {
		row := make([]byte, stride)
		for x := range w {
			row[3*x], row[3*x+1], row[3*x+2] = c.B, c.G, c.R
		}
		b.Write(row)
	}
	maskStride := (w + 31) / 32 * 4
	for y := range w {
		row := make([]byte, maskStride)
		if y == w-1 { // the rows are bottom-up: the last one is the top
			row[0] = 0x80
		}
		b.Write(row)
	}
	return b.Bytes()
}

// dib4 is the DIB of a 2 x 2 image of 4 bits per pixel with a palette of two colors, the pixels 0 1 / 1 0, and no mask bit set.
func dib4() []byte {
	var b bytes.Buffer
	le := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	le(uint32(40))
	le(int32(2))
	le(int32(4))
	le(uint16(1))
	le(uint16(4))
	le(uint32(0))
	le(uint32(0))
	le(int32(0))
	le(int32(0))
	le(uint32(2))
	le(uint32(0))
	b.Write([]byte{0, 0, 255, 0, 255, 0, 0, 0}) // red, blue (BGRA)
	b.Write([]byte{0x10, 0, 0, 0})              // bottom row: 1 0
	b.Write([]byte{0x01, 0, 0, 0})              // top row: 0 1
	b.Write([]byte{0, 0, 0, 0, 0, 0, 0, 0})     // the mask
	return b.Bytes()
}

func writeFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// pixel is the premultiplied color of a pixel of a decoded frame.
func pixel(t *testing.T, img *Image, x, y int) color.RGBA {
	t.Helper()
	data, err := img.Frames[0].Image.ToByteData(0)
	if err != nil {
		t.Fatal(err)
	}
	i := 4 * (y*img.Width + x)
	return color.RGBA{R: data[i], G: data[i+1], B: data[i+2], A: data[i+3]}
}

func TestDecodingEachFormat(t *testing.T) {
	webp, err := base64.StdEncoding.DecodeString(webpLossless)
	if err != nil {
		t.Fatal(err)
	}
	pngBody := pngData(t)
	tests := []struct {
		name, file string
		data       []byte
		w, h       int
		frames     int
		format     string
	}{
		{"png", "a.png", pngBody, 3, 2, 1, "png"},
		{"jpeg", "a.jpg", encode(t, func(b *bytes.Buffer) error { return jpeg.Encode(b, testImage(), nil) }), 3, 2, 1, "jpeg"},
		{"gif with three frames", "a.gif", threeFrameGIF(t), 2, 2, 3, "gif"},
		{"bmp", "a.bmp", encode(t, func(b *bytes.Buffer) error { return bmp.Encode(b, testImage()) }), 3, 2, 1, "bmp"},
		{"webp", "a.webp", webp, 1, 1, 1, "webp"},
		{"tiff", "a.tiff", encode(t, func(b *bytes.Buffer) error { return tiff.Encode(b, testImage(), nil) }), 3, 2, 1, "tiff"},
		{"ico with a png and a bmp entry: the largest", "a.ico", icoFile(icoTestEntry{3, 32, pngBody}, icoTestEntry{4, 24, dib24(4, color.NRGBA{G: 200, A: 255})}), 4, 4, 1, "ico"},
		{"ico of a png entry", "b.ico", icoFile(icoTestEntry{3, 32, pngBody}), 3, 2, 1, "ico"},
		{"ico of a palette", "c.ico", icoFile(icoTestEntry{2, 4, dib4()}), 2, 2, 1, "ico"},
		{"svg", "a.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="40" height="30"><rect width="40" height="30" fill="red"/></svg>`), 40, 30, 0, "svg"},
		{"svg with only a viewBox", "b.svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 8"><circle cx="4" cy="4" r="4"/></svg>`), 16, 8, 0, "svg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			img, err := Decode(context.Background(), writeFile(t, tt.file, tt.data))
			if err != nil {
				t.Fatal(err)
			}
			if img.Width != tt.w || img.Height != tt.h || len(img.Frames) != tt.frames || img.Format != tt.format {
				t.Errorf("%dx%d, %d frames, format %s", img.Width, img.Height, len(img.Frames), img.Format)
			}
			if (img.SVG != nil) != (tt.format == "svg") {
				t.Errorf("svg %v", img.SVG != nil)
			}
		})
	}
}

func TestDecodedPixels(t *testing.T) {
	// a PNG keeps its transparency, premultiplied
	img, err := Decode(context.Background(), writeFile(t, "a.png", pngData(t)))
	if err != nil {
		t.Fatal(err)
	}
	if got := pixel(t, img, 0, 0); got != (color.RGBA{R: 255, A: 255}) {
		t.Errorf("red pixel %v", got)
	}
	if got := pixel(t, img, 1, 1); got.A != 0 {
		t.Errorf("transparent pixel %v", got)
	}
	// a BMP entry of an ICO: its colors, and the AND mask makes the top left pixel transparent
	img, err = Decode(context.Background(), writeFile(t, "a.ico", icoFile(icoTestEntry{4, 24, dib24(4, color.NRGBA{G: 200, A: 255})})))
	if err != nil {
		t.Fatal(err)
	}
	if got := pixel(t, img, 0, 0); got.A != 0 {
		t.Errorf("masked pixel %v", got)
	}
	if got := pixel(t, img, 3, 3); got != (color.RGBA{G: 200, A: 255}) {
		t.Errorf("green pixel %v", got)
	}
	// a palette entry: 0 1 on the top row
	img, err = Decode(context.Background(), writeFile(t, "c.ico", icoFile(icoTestEntry{2, 4, dib4()})))
	if err != nil {
		t.Fatal(err)
	}
	if a, b := pixel(t, img, 0, 0), pixel(t, img, 1, 0); a != (color.RGBA{R: 255, A: 255}) || b != (color.RGBA{B: 255, A: 255}) {
		t.Errorf("top row %v %v", a, b)
	}
}

func TestAnAnimatedGIF(t *testing.T) {
	img, err := Decode(context.Background(), writeFile(t, "a.gif", threeFrameGIF(t)))
	if err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{50 * time.Millisecond, 100 * time.Millisecond, 100 * time.Millisecond}
	for i, f := range img.Frames {
		if f.Duration != want[i] {
			t.Errorf("frame %d: %v", i, f.Duration)
		}
	}
	if img.Repeat != 1 {
		t.Errorf("repeat %d", img.Repeat)
	}
}

// bmpOfWidth is a BMP whose header says it is w pixels wide; its pixels are those of a 1 x 1 image, as only the header is read.
func bmpOfWidth(t *testing.T, w int32) []byte {
	data := encode(t, func(b *bytes.Buffer) error { return bmp.Encode(b, image.NewRGBA(image.Rect(0, 0, 1, 1))) })
	binary.LittleEndian.PutUint32(data[18:], uint32(w))
	return data
}

func TestImagesThatAreRefused(t *testing.T) {
	big := filepath.Join(t.TempDir(), "big.png")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	// a sparse file: its size is over the limit, and nothing is written
	if err := f.Truncate(MaxFileSize + 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, path, route, message string
	}{
		{"a file over 256 MiB", big, "Decode.ImageFileTooLarge", "This image file is too large to show (256.00MB)."},
		{"an image over the pixel limit", writeFile(t, "wide.bmp", bmpOfWidth(t, 40000)), "Decode->decodeBitmap.ImageTooLarge", "This image is too large to show (40000 x 1)."},
		{"a file that is not there", filepath.Join(t.TempDir(), "none.png"), "Decode.ImageReadFailed", ""},
		{"text named .png", writeFile(t, "text.png", []byte("not an image")), "Decode->decodeBitmap.ImageDecodeFailed", ""},
		{"a png cut after its header", writeFile(t, "cut.png", pngData(t)[:40]), "Decode->decodeBitmap.ImageDecodeFailed", ""},
		{"a broken svg", writeFile(t, "broken.svg", []byte("<svg")), "Decode.ImageDecodeFailed", ""},
		{"an ico that is not one", writeFile(t, "text.ico", []byte("not an icon file")), "Decode->decodeICO.ImageDecodeFailed", ""},
		{"an ico whose bitmap is cut", writeFile(t, "cut.ico", icoFile(icoTestEntry{4, 24, dib24(4, color.NRGBA{A: 255})[:60]})), "Decode->decodeICO.ImageDecodeFailed", ""},
	}
	// a file that can be seen but not read (the owner can read every file when the tests run as root)
	if os.Geteuid() != 0 {
		locked := writeFile(t, "locked.png", pngData(t))
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		tests = append(tests, struct{ name, path, route, message string }{"a file that cannot be read", locked, "Decode.ImageReadFailed", ""})
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode(context.Background(), tt.path)
			route(t, err, tt.route)
			if e, ok := errors.FromError(err); ok && tt.message != "" && e.Message != tt.message {
				t.Errorf("message %q", e.Message)
			}
		})
	}
}
