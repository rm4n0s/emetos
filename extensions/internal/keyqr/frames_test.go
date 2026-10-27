package keyqr

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"
)

// testPicture is a w by h picture of colors that differ.
func testPicture(w, h int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x * 255 / w), G: uint8(y * 255 / h), B: uint8((x + y) % 256), A: 255})
		}
	}
	return img
}

// jpegOf encodes an image as a JPEG, as a camera's frame.
func jpegOf(t testing.TB, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// withoutTables removes the Huffman tables (DHT segments) of a JPEG, as the frames of many UVC cameras have none.
func withoutTables(t testing.TB, data []byte) []byte {
	t.Helper()
	out := append([]byte(nil), data[:2]...)
	i := 2
	for i+3 < len(data) {
		if data[i] != 0xFF {
			t.Fatalf("not a marker at %d", i)
		}
		marker := data[i+1]
		if marker == 0xDA { // the scan: the rest is entropy coded data
			return append(out, data[i:]...)
		}
		n := 2 + (int(data[i+2])<<8 | int(data[i+3]))
		if marker != 0xC4 {
			out = append(out, data[i:i+n]...)
		}
		i += n
	}
	t.Fatal("no scan in the JPEG")
	return nil
}

func TestAnMJPEGFrameBecomesAnImage(t *testing.T) {
	pic := testPicture(96, 64)
	whole := jpegOf(t, pic)
	cases := []struct {
		name string
		data []byte
	}{
		{"a frame with its tables", whole},
		{"a frame the camera left without tables", withoutTables(t, whole)},
		{"a frame with a padding after it", append(append([]byte(nil), whole...), make([]byte, 100)...)},
	}
	if bytes.Equal(withoutTables(t, whole), whole) {
		t.Fatal("the test frame had no tables to take out")
	}
	if _, err := jpeg.Decode(bytes.NewReader(cases[1].data)); err == nil {
		t.Fatal("the standard library decoded a frame with no tables, so the insertion is not what is tested")
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			img, err := DecodeMJPEG(tc.data)
			if err != nil {
				t.Fatal(err)
			}
			if img.Bounds().Dx() != 96 || img.Bounds().Dy() != 64 {
				t.Fatalf("size %v", img.Bounds())
			}
			// the same pixels as decoding the frame that has its tables
			want, err := jpeg.Decode(bytes.NewReader(whole))
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range []image.Point{{0, 0}, {50, 30}, {95, 63}} {
				if img.At(p.X, p.Y) != want.At(p.X, p.Y) {
					t.Errorf("pixel %v is %v, want %v", p, img.At(p.X, p.Y), want.At(p.X, p.Y))
				}
			}
		})
	}
}

func TestTheStandardTablesAreWhatTheEncoderUses(t *testing.T) {
	// the standard DHT is the one the Go encoder writes: the segment of a frame it made is the segment that is inserted
	whole := jpegOf(t, testPicture(16, 16))
	var dht []byte
	for i := 2; i+3 < len(whole); {
		n := 2 + (int(whole[i+2])<<8 | int(whole[i+3]))
		if whole[i+1] == 0xC4 {
			dht = append(dht, whole[i:i+n]...)
		}
		if whole[i+1] == 0xDA {
			break
		}
		i += n
	}
	if !bytes.Equal(dht, standardDHT) {
		t.Errorf("the standard tables (%d bytes) are not the encoder's (%d bytes)", len(standardDHT), len(dht))
	}
}

func TestTablesAreInsertedBeforeTheScanOnlyWhenNoneIsThere(t *testing.T) {
	whole := jpegOf(t, testPicture(16, 16))
	stripped := withoutTables(t, whole)
	original := append([]byte(nil), stripped...)
	got := withHuffmanTables(stripped)
	if !bytes.Equal(stripped, original) {
		t.Error("the frame that was given was changed")
	}
	if len(got) != len(stripped)+len(standardDHT) || !bytes.Equal(got[:2], []byte{0xFF, 0xD8}) {
		t.Errorf("the result is %d bytes, the frame %d, the tables %d", len(got), len(stripped), len(standardDHT))
	}
	if !bytes.Equal(withHuffmanTables(whole), whole) {
		t.Error("a frame that has its tables was changed")
	}
	// data that is not a JPEG, or whose segments make no sense, comes back as it is
	for _, junk := range [][]byte{nil, {0xFF}, []byte("not a jpeg at all"), {0xFF, 0xD8, 0x00, 0x00, 0x00}, {0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x00}, {0xFF, 0xD8, 0xFF, 0xFF, 0xFF, 0x01, 0xFF, 0xD9, 0x00}} {
		if got := withHuffmanTables(junk); !bytes.Equal(got, junk) {
			t.Errorf("%v became %v", junk, got)
		}
	}
}

func TestABadMJPEGFrameIsRefusedWithItsTag(t *testing.T) {
	whole := jpegOf(t, testPicture(32, 32))
	// a JPEG whose header says 4000x3000: only the header is made, there is no data after it, and nothing is allocated for it
	huge := append([]byte(nil), whole[:2]...)
	for i := 2; i+3 < len(whole); {
		n := 2 + (int(whole[i+2])<<8 | int(whole[i+3]))
		seg := append([]byte(nil), whole[i:i+n]...)
		if whole[i+1] == 0xC0 { // SOF0: precision, height, width
			seg[5], seg[6], seg[7], seg[8] = 3000>>8, 3000&0xFF, 4000>>8, 4000&0xFF
		}
		huge = append(huge, seg...)
		if whole[i+1] == 0xDA {
			break
		}
		i += n
	}
	cases := []struct {
		name  string
		data  []byte
		route string
	}{
		{"nothing", nil, "DecodeMJPEG.KeyQRFrameInvalid"},
		{"text", []byte("hello"), "DecodeMJPEG.KeyQRFrameInvalid"},
		{"a frame cut short", whole[:len(whole)/2], "DecodeMJPEG.KeyQRFrameInvalid"},
		{"a header that says a huge picture", huge, "DecodeMJPEG.KeyQRFrameTooLarge"},
		{"a frame wider than 1920", jpegOf(t, image.NewGray(image.Rect(0, 0, 1921, 8))), "DecodeMJPEG.KeyQRFrameTooLarge"},
		{"a frame higher than 1080", jpegOf(t, image.NewGray(image.Rect(0, 0, 8, 1081))), "DecodeMJPEG.KeyQRFrameTooLarge"},
		{"more bytes than any frame", make([]byte, MaxFrameBytes+1), "DecodeMJPEG.KeyQRFrameInvalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			img, err := DecodeMJPEG(tc.data)
			if img != nil {
				t.Error("an image came out")
			}
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T", err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
	// the largest frame is taken
	if _, err := DecodeMJPEG(jpegOf(t, image.NewGray(image.Rect(0, 0, MaxFrameWidth, MaxFrameHeight)))); err != nil {
		t.Errorf("a frame of 1920x1080: %v", err)
	}
}

// yuyvOf packs a picture as YUYV with a stride: Y for every pixel, and the U and V of the first pixel of each pair.
func yuyvOf(w, h, stride int, y func(x, y int) byte, u, v func(x, y int) byte) []byte {
	data := make([]byte, stride*h)
	for r := 0; r < h; r++ {
		for x := 0; x < w; x += 2 {
			o := r*stride + 2*x
			data[o], data[o+1], data[o+2], data[o+3] = y(x, r), u(x, r), y(x+1, r), v(x, r)
		}
	}
	return data
}

func TestAYUYVFrameIsConvertedPixelByPixel(t *testing.T) {
	yf := func(x, y int) byte { return byte(10*x + y) }
	uf := func(x, y int) byte { return byte(100 + x/2 + y) }
	vf := func(x, y int) byte { return byte(200 - x/2 - y) }
	for _, stride := range []int{16, 20, 64} { // 8 pixels are 16 bytes a row; the camera may pad them
		t.Run("stride "+string(rune('0'+stride/10))+string(rune('0'+stride%10)), func(t *testing.T) {
			img, err := YUYVToImage(yuyvOf(8, 4, stride, yf, uf, vf), 8, 4, stride)
			if err != nil {
				t.Fatal(err)
			}
			if img.Bounds() != image.Rect(0, 0, 8, 4) || img.SubsampleRatio != image.YCbCrSubsampleRatio422 {
				t.Fatalf("bounds %v, ratio %v", img.Bounds(), img.SubsampleRatio)
			}
			for y := 0; y < 4; y++ {
				for x := 0; x < 8; x++ {
					want := color.YCbCr{Y: yf(x, y), Cb: uf(x&^1, y), Cr: vf(x&^1, y)}
					if got := img.YCbCrAt(x, y); got != want {
						t.Fatalf("pixel %d,%d is %v, want %v", x, y, got, want)
					}
				}
			}
		})
	}
}

func TestAYUYVFrameThatDoesNotFitIsRefusedWithItsTag(t *testing.T) {
	ok := make([]byte, 16*4)
	cases := []struct {
		name         string
		data         []byte
		w, h, stride int
		route        string
	}{
		{"an odd width", ok, 7, 4, 16, "YUYVToImage.KeyQRFrameInvalid"},
		{"no width", ok, 0, 4, 16, "YUYVToImage.KeyQRFrameInvalid"},
		{"a stride that is shorter than a row", ok, 8, 4, 12, "YUYVToImage.KeyQRFrameInvalid"},
		{"data shorter than the frame", ok[:40], 8, 4, 16, "YUYVToImage.KeyQRFrameInvalid"},
		{"wider than 1920", nil, 1922, 4, 3844, "YUYVToImage.KeyQRFrameTooLarge"},
		{"higher than 1080", nil, 8, 1082, 16, "YUYVToImage.KeyQRFrameTooLarge"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			img, err := YUYVToImage(tc.data, tc.w, tc.h, tc.stride)
			if img != nil {
				t.Error("an image came out")
			}
			appErr, isErr := errors.FromError(err)
			if !isErr {
				t.Fatalf("expected *errors.Error, got %T", err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
	// the last row may be shorter than the stride: a camera that does not pad it
	if _, err := YUYVToImage(make([]byte, 20*3+16), 8, 4, 20); err != nil {
		t.Errorf("a frame whose last row is not padded: %v", err)
	}
}

func TestAQRCodeSurvivesTheJPEGOfACamera(t *testing.T) {
	// the whole way a frame goes: a picture of a code, as the MJPEG of a camera with no tables, then the image, then the text
	line := testLines(t)["ecdsa"]
	code, _, err := EncodeKey(line, 400)
	if err != nil {
		t.Fatal(err)
	}
	frame := frameWith(scaledTo(code, 500), 1280, 720, 140)
	img, err := DecodeMJPEG(withoutTables(t, jpegOf(t, frame)))
	if err != nil {
		t.Fatal(err)
	}
	if text, err := DecodeQR(img); err != nil || text != line {
		t.Errorf("read %q, %v", text, err)
	}
	// and as YUYV
	gray := frame
	data := yuyvOf(1280, 720, 1280*2, func(x, y int) byte { return gray.GrayAt(x, y).Y }, func(int, int) byte { return 128 }, func(int, int) byte { return 128 })
	yuyv, err := YUYVToImage(data, 1280, 720, 1280*2)
	if err != nil {
		t.Fatal(err)
	}
	if text, err := DecodeQR(yuyv); err != nil || text != line {
		t.Errorf("read %q, %v", text, err)
	}
	if !strings.HasPrefix(line, "ecdsa") {
		t.Fatal("the test line is not the ECDSA one")
	}
}
