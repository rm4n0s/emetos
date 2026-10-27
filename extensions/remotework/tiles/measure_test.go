package tiles

import (
	"bytes"
	"compress/flate"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
)

// loadPicture reads one of the pictures of testdata, 1280x800 frames of a headless view over a Go file (they are copies of
// internal/gui/app/testdata/goldens/headless_*.png): the file as the view opens it (editor), with one character typed
// (typed) and scrolled (scrolled).
func loadPicture(tb testing.TB, name string) *image.RGBA {
	tb.Helper()
	f, err := os.Open(filepath.Join("testdata", name+".png"))
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

func sizeOf(tiles []*protocol.Tile) (bytes int) {
	for _, t := range tiles {
		bytes += len(t.Data)
	}
	return bytes
}

// TestARealEditorFrameOnlySendsWhatChanged is the measurement of SPEC §10.18 "As built (step 15.1)": the tiles and bytes of a
// whole picture, of one typed character and of a scroll, at each level of deflate, and what one stream for the whole frame
// would be. The checks are the ones that must hold; the numbers are logged (go test -v).
func TestARealEditorFrameOnlySendsWhatChanged(t *testing.T) {
	editor, typed, scrolled := loadPicture(t, "editor"), loadPicture(t, "typed"), loadPicture(t, "scrolled")
	for _, level := range []struct {
		name  string
		level int
	}{{"huffman only", flate.HuffmanOnly}, {"best speed", flate.BestSpeed}, {"default", flate.DefaultCompression}, {"best", flate.BestCompression}} {
		d, err := NewDiffer(level.level)
		if err != nil {
			t.Fatal(err)
		}
		t0 := time.Now()
		_, full, err := d.Next(editor)
		if err != nil {
			t.Fatal(err)
		}
		fullTime := time.Since(t0)
		d.Confirm(1)
		t0 = time.Now()
		_, edit, _ := d.Next(typed)
		editTime := time.Since(t0)
		d.Confirm(2)
		_, scroll, _ := d.Next(scrolled)
		t.Logf("%-12s whole picture: %3d tiles %7d bytes %v | one character: %2d tiles %6d bytes %v | scroll: %3d tiles %7d bytes",
			level.name, len(full), sizeOf(full), fullTime.Round(time.Microsecond), len(edit), sizeOf(edit), editTime.Round(time.Microsecond), len(scroll), sizeOf(scroll))
		if len(full) != protocol.TilesAcross(1280)*protocol.TilesAcross(800) {
			t.Errorf("%s: %d tiles for the whole picture", level.name, len(full))
		}
		if len(edit) == 0 || len(edit) > 8 {
			t.Errorf("%s: a typed character changed %d tiles", level.name, len(edit))
		}
		if level.level != flate.HuffmanOnly && sizeOf(edit) > 12<<10 {
			t.Errorf("%s: a typed character costs %d bytes", level.name, sizeOf(edit))
		}
	}
	// one stream for the whole picture, for the comparison: what tiles of their own cost
	var stream bytes.Buffer
	zw, err := flate.NewWriter(&stream, flate.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Write(editor.Pix); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("one deflate stream for the whole picture at best speed: %d bytes", stream.Len())
}

func benchmarkNext(b *testing.B, level int, from, to string) {
	d, err := NewDiffer(level)
	if err != nil {
		b.Fatal(err)
	}
	base, next := loadPicture(b, from), loadPicture(b, to)
	if _, _, err := d.Next(base); err != nil {
		b.Fatal(err)
	}
	d.Confirm(1)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// the frame after the confirmed one, over and over: the same changes against the same base
		if _, _, err := d.Next(next); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkDifferOneCharacter, BenchmarkDifferScroll and BenchmarkDifferWholePicture are the time to compare a 1280x800
// picture with the confirmed one and deflate the tiles that changed, at Level.
func BenchmarkDifferOneCharacter(b *testing.B) { benchmarkNext(b, Level, "editor", "typed") }
func BenchmarkDifferScroll(b *testing.B)       { benchmarkNext(b, Level, "typed", "scrolled") }
func BenchmarkDifferWholePicture(b *testing.B) {
	d, err := NewDiffer(Level)
	if err != nil {
		b.Fatal(err)
	}
	img := loadPicture(b, "editor")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d.Reset()
		if _, _, err := d.Next(img); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkCanvasWholePicture(b *testing.B) {
	d, err := NewDiffer(flate.BestSpeed)
	if err != nil {
		b.Fatal(err)
	}
	_, tiles, err := d.Next(loadPicture(b, "editor"))
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c := NewCanvas()
		for _, tile := range tiles {
			if _, err := c.Apply(tile); err != nil {
				b.Fatal(err)
			}
		}
	}
}
