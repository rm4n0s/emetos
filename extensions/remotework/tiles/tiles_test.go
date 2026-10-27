package tiles

import (
	"bytes"
	"compress/flate"
	"image"
	"io"
	"math/rand"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
)

// picture makes an image of a size with pixels that depend on a seed, in blocks (so that it deflates like a screen does).
func picture(w, h int, seed int64) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	rng := rand.New(rand.NewSource(seed))
	for y := 0; y < h; y += 8 {
		for x := 0; x < w; x += 8 {
			c := [4]byte{byte(rng.Intn(256)), byte(rng.Intn(256)), byte(rng.Intn(256)), 255}
			for yy := y; yy < min(y+8, h); yy++ {
				for xx := x; xx < min(x+8, w); xx++ {
					copy(img.Pix[img.PixOffset(xx, yy):], c[:])
				}
			}
		}
	}
	return img
}

func clone(img *image.RGBA) *image.RGBA {
	c := image.NewRGBA(img.Rect)
	copy(c.Pix, img.Pix)
	return c
}

func newDiffer(t *testing.T) *Differ {
	t.Helper()
	d, err := NewDiffer(flate.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func tilePositions(tiles []*protocol.Tile) map[[2]int]bool {
	out := map[[2]int]bool{}
	for _, t := range tiles {
		out[[2]int{int(t.X), int(t.Y)}] = true
	}
	return out
}

// deliver applies the tiles and returns the last result.
func deliver(t *testing.T, c *Canvas, tiles []*protocol.Tile) Result {
	t.Helper()
	var res Result
	for _, tile := range tiles {
		var err error
		if res, err = c.Apply(tile); err != nil {
			t.Fatal(err)
		}
	}
	return res
}

func equalImages(a, b *image.RGBA) bool {
	return a != nil && b != nil && a.Rect == b.Rect && bytes.Equal(a.Pix, b.Pix)
}

func TestTheFirstPictureIsSentWholeAndLaterOnesOnlyWhatChanged(t *testing.T) {
	img := picture(200, 150, 1) // 4x3 tiles, the last column 8 pixels wide, the last row 22 high
	d, c := newDiffer(t), NewCanvas()
	frame, tiles, err := d.Next(img)
	if err != nil || frame != 1 || len(tiles) != 12 {
		t.Fatalf("first picture: frame %d, %d tiles, %v", frame, len(tiles), err)
	}
	for _, tile := range tiles {
		if tile.Base != 0 || tile.Total != 12 || tile.Width != 200 || tile.Height != 150 {
			t.Fatalf("tile %+v", tile)
		}
	}
	if got := deliver(t, c, tiles); got != Complete || c.Confirmed() != 1 || !equalImages(c.Image(), img) {
		t.Fatalf("the canvas is not the picture: %v, confirmed %d", got, c.Confirmed())
	}
	if !d.Confirm(1) || d.Confirmed() != 1 {
		t.Fatal("the confirm was not taken")
	}
	// nothing changed: nothing is sent
	if frame, tiles, err = d.Next(img); frame != 0 || tiles != nil || err != nil {
		t.Fatalf("an unchanged picture sent frame %d with %d tiles (%v)", frame, len(tiles), err)
	}
	// one pixel changes: one tile (the one of the pixel, at the partial edge) is sent
	img2 := clone(img)
	img2.Pix[img2.PixOffset(195, 140)] ^= 0xFF
	frame, tiles, err = d.Next(img2)
	if err != nil || frame != 2 || len(tiles) != 1 || !tilePositions(tiles)[[2]int{3, 2}] || tiles[0].Base != 1 || tiles[0].Total != 1 {
		t.Fatalf("one pixel: frame %d, %d tiles %v (%v)", frame, len(tiles), tilePositions(tiles), err)
	}
	if got := deliver(t, c, tiles); got != Complete || !equalImages(c.Image(), img2) {
		t.Fatalf("the canvas does not show the change: %v", got)
	}
}

func TestTilesAreSentAgainAgainstTheConfirmedPictureUntilItIsConfirmed(t *testing.T) {
	img := picture(128, 128, 2) // 2x2 tiles
	d, c := newDiffer(t), NewCanvas()
	_, tiles, _ := d.Next(img)
	deliver(t, c, tiles)
	d.Confirm(1)
	// picture 2 changes the tile (0,0); the client does not get it
	img2 := clone(img)
	img2.Pix[0] ^= 1
	f2, t2, _ := d.Next(img2)
	// picture 3 changes the tile (1,1) as well: it has both changes, because the base is still picture 1
	img3 := clone(img2)
	img3.Pix[img3.PixOffset(100, 100)] ^= 1
	f3, t3, _ := d.Next(img3)
	if f2 != 2 || f3 != 3 || len(t2) != 1 || len(t3) != 2 || !tilePositions(t3)[[2]int{0, 0}] || !tilePositions(t3)[[2]int{1, 1}] {
		t.Fatalf("frames %d %d, tiles %d %d %v", f2, f3, len(t2), len(t3), tilePositions(t3))
	}
	if got := deliver(t, c, t3); got != Complete || !equalImages(c.Image(), img3) {
		t.Fatal("picture 3 is not on the canvas")
	}
	// the late tile of picture 2 is dropped
	if res, err := c.Apply(t2[0]); err != nil || res != Stale {
		t.Fatalf("an old tile gave %v, %v", res, err)
	}
	if !equalImages(c.Image(), img3) {
		t.Fatal("an old tile changed the picture")
	}
}

func TestAnUnfinishedPicturesTilesArePutBackWhenTheNextOneDoesNotChangeThem(t *testing.T) {
	img := picture(128, 64, 3) // 2x1 tiles
	d, c := newDiffer(t), NewCanvas()
	_, tiles, _ := d.Next(img)
	deliver(t, c, tiles)
	d.Confirm(1)
	// picture 2 changes both tiles; only the tile (0,0) reaches the client
	img2 := clone(img)
	img2.Pix[0] ^= 0xFF
	img2.Pix[img2.PixOffset(100, 0)] ^= 0xFF
	_, t2, _ := d.Next(img2)
	if len(t2) != 2 {
		t.Fatalf("%d tiles", len(t2))
	}
	if res, _ := c.Apply(t2[0]); res != Applied || equalImages(c.Image(), img) {
		t.Fatalf("the tile was not shown (%v)", res)
	}
	// the picture goes back to picture 1 except tile (1,0): the host sends picture 3 with that one only
	img3 := clone(img)
	img3.Pix[img3.PixOffset(100, 0)] ^= 0x0F
	f3, t3, _ := d.Next(img3)
	if f3 != 3 || len(t3) != 1 || !tilePositions(t3)[[2]int{1, 0}] {
		t.Fatalf("frame %d with tiles %v", f3, tilePositions(t3))
	}
	if got := deliver(t, c, t3); got != Complete {
		t.Fatalf("got %v", got)
	}
	if !equalImages(c.Image(), img3) {
		t.Error("the tile (0,0) of picture 2 stayed on the canvas")
	}
}

func TestAPictureThatReturnedToTheConfirmedOneStillCompletes(t *testing.T) {
	img := picture(128, 64, 4)
	d, c := newDiffer(t), NewCanvas()
	_, tiles, _ := d.Next(img)
	deliver(t, c, tiles)
	d.Confirm(1)
	img2 := clone(img)
	img2.Pix[0] ^= 0xFF
	_, t2, _ := d.Next(img2)
	if res, _ := c.Apply(t2[0]); res != Complete { // picture 2 has one tile, so it completes: the client shows it and confirms
		t.Fatalf("got %v", res)
	}
	// the confirm is lost; the picture goes back to picture 1 before the host hears of it
	f3, t3, _ := d.Next(img)
	if f3 != 3 || len(t3) != 1 {
		t.Fatalf("frame %d with %d tiles: a picture equal to the confirmed one must still be answered while pictures are unconfirmed", f3, len(t3))
	}
	// picture 3 has base 1, the client's confirmed picture is 2: the tile is dropped, and the stale run says to ask again
	if res, _ := c.Apply(t3[0]); res != Stale || c.StaleRun() != 1 {
		t.Fatalf("got %v, stale run %d", res, c.StaleRun())
	}
	// the confirm of picture 2 arrives late; the next picture is based on it
	d.Confirm(2)
	f4, t4, _ := d.Next(img)
	if f4 != 4 || t4[0].Base != 2 {
		t.Fatalf("frame %d base %d", f4, t4[0].Base)
	}
	if got := deliver(t, c, t4); got != Complete || !equalImages(c.Image(), img) {
		t.Fatal("the client did not come back to the picture")
	}
}

func TestRefreshAndResetSendTheWholePicture(t *testing.T) {
	img := picture(130, 70, 5) // 3x2 tiles
	d, c := newDiffer(t), NewCanvas()
	_, tiles, _ := d.Next(img)
	deliver(t, c, tiles)
	d.Confirm(1)

	d.Refresh() // the periodic refresh: all tiles, but against the confirmed picture the client still has
	f, tiles, _ := d.Next(img)
	if f != 2 || len(tiles) != 6 || tiles[0].Base != 0 {
		t.Fatalf("refresh: frame %d, %d tiles, base %d", f, len(tiles), tiles[0].Base)
	}
	if got := deliver(t, c, tiles); got != Complete || !equalImages(c.Image(), img) {
		t.Fatal("the refresh did not reach the canvas")
	}
	d.Confirm(2)

	d.Reset() // the client lost its picture and asked
	if d.Confirmed() != 0 {
		t.Error("Reset kept the confirmed picture")
	}
	c.Reset()
	f, tiles, _ = d.Next(img)
	if f != 3 || len(tiles) != 6 || tiles[0].Base != 0 {
		t.Fatalf("reset: frame %d, %d tiles, base %d", f, len(tiles), tiles[0].Base)
	}
	if got := deliver(t, c, tiles); got != Complete || !equalImages(c.Image(), img) {
		t.Fatal("the picture did not come back after a reset")
	}
}

func TestAConfirmTheDifferDoesNotKnowMakesItSendTheWholePicture(t *testing.T) {
	img := picture(64, 64, 6)
	d := newDiffer(t)
	if _, _, err := d.Next(img); err != nil {
		t.Fatal(err)
	}
	if d.Confirm(77) {
		t.Fatal("a picture that was never made was taken")
	}
	_, tiles, _ := d.Next(img)
	if len(tiles) != 1 || tiles[0].Base != 0 {
		t.Fatalf("%d tiles with base %d", len(tiles), tiles[0].Base)
	}
	// a picture that was forgotten (more than MaxPending ago) is not known either
	d = newDiffer(t)
	for i := range MaxPending + 3 {
		if _, _, err := d.Next(picture(64, 64, int64(100+i))); err != nil {
			t.Fatal(err)
		}
	}
	if d.Confirm(1) {
		t.Error("a forgotten picture was taken")
	}
}

func TestAResizedPictureIsSentWholeAndTheCanvasFollows(t *testing.T) {
	d, c := newDiffer(t), NewCanvas()
	small := picture(100, 100, 7)
	_, tiles, _ := d.Next(small)
	deliver(t, c, tiles)
	d.Confirm(1)
	big := picture(300, 200, 8)
	f, tiles, _ := d.Next(big)
	if f != 2 || len(tiles) != 20 || tiles[0].Base != 0 {
		t.Fatalf("frame %d with %d tiles base %d", f, len(tiles), tiles[0].Base)
	}
	if got := deliver(t, c, tiles); got != Complete || !equalImages(c.Image(), big) {
		t.Fatal("the canvas did not follow the new size")
	}
}

// TestLossyDeliveryConvergesAndEveryCompletePictureIsExact simulates a link that loses, repeats and reorders tiles and
// delays confirms. Whenever the canvas says a frame is complete it must be exactly that picture.
func TestLossyDeliveryConvergesAndEveryCompletePictureIsExact(t *testing.T) {
	totalCompletes := 0
	for seed := int64(1); seed <= 20; seed++ {
		rng := rand.New(rand.NewSource(seed))
		w, h := 64*(2+rng.Intn(4))+rng.Intn(60), 64*(1+rng.Intn(3))+rng.Intn(60)
		d, c := newDiffer(t), NewCanvas()
		img := picture(w, h, seed)
		sent := map[uint64]*image.RGBA{}
		var inflight []*protocol.Tile
		var confirms []uint64
		completes := 0
		for step := 0; step < 120; step++ {
			// the picture changes in a few places (sometimes it goes back to an earlier state)
			if rng.Intn(3) > 0 {
				for range 1 + rng.Intn(4) {
					x, y := rng.Intn(w), rng.Intn(h)
					img.Pix[img.PixOffset(x, y)] = byte(rng.Intn(256))
				}
			}
			if rng.Intn(10) == 0 {
				d.Refresh()
			}
			frame, tiles, err := d.Next(img)
			if err != nil {
				t.Fatal(err)
			}
			if frame != 0 {
				sent[frame] = clone(img)
				for _, tile := range tiles {
					if rng.Intn(5) > 0 { // 20% loss
						inflight = append(inflight, tile)
						if rng.Intn(8) == 0 {
							inflight = append(inflight, tile) // repeated
						}
					}
				}
			}
			rng.Shuffle(len(inflight), func(i, j int) { inflight[i], inflight[j] = inflight[j], inflight[i] })
			n := rng.Intn(len(inflight) + 1)
			for _, tile := range inflight[:n] {
				res, err := c.Apply(tile)
				if err != nil {
					t.Fatal(err)
				}
				if res == Complete {
					completes++
					if !equalImages(c.Image(), sent[tile.Frame]) {
						t.Fatalf("seed %d step %d: frame %d is complete and the canvas is not that picture", seed, step, tile.Frame)
					}
					confirms = append(confirms, c.Confirmed())
				}
			}
			inflight = inflight[n:]
			// the confirms arrive late, some never
			for len(confirms) > 0 && rng.Intn(2) == 0 {
				d.Confirm(confirms[0])
				confirms = confirms[1:]
			}
		}
		totalCompletes += completes
		// the link gets good: the canvas must converge to the picture
		for range 30 {
			frame, tiles, err := d.Next(img)
			if err != nil {
				t.Fatal(err)
			}
			if frame != 0 {
				sent[frame] = clone(img)
			}
			deliver(t, c, append(append([]*protocol.Tile(nil), inflight...), tiles...))
			inflight = nil
			if c.Confirmed() != 0 {
				d.Confirm(c.Confirmed())
			}
		}
		if !equalImages(c.Image(), img) {
			t.Fatalf("seed %d: the canvas did not converge", seed)
		}
	}
	if totalCompletes < 20 {
		t.Errorf("only %d pictures completed in the lossy phase; the test checks too little", totalCompletes)
	}
}

func TestOnlyChangedTilesCostBytes(t *testing.T) {
	// a screen of flat colors with a few blocks of noise: the tile that changes is the only one that is sent, and a
	// flat tile deflates to a few bytes
	img := image.NewRGBA(image.Rect(0, 0, 1280, 800))
	for i := range img.Pix {
		img.Pix[i] = 0xFF
	}
	d := newDiffer(t)
	_, tiles, _ := d.Next(img)
	total := 0
	for _, tile := range tiles {
		total += len(tile.Data)
	}
	if len(tiles) != 20*13 || total > 20*13*40 {
		t.Fatalf("a flat screen: %d tiles, %d bytes", len(tiles), total)
	}
	d.Confirm(1)
	img2 := clone(img)
	copy(img2.Pix[img2.PixOffset(70, 70):], []byte{1, 2, 3, 255})
	_, tiles, _ = d.Next(img2)
	if len(tiles) != 1 {
		t.Fatalf("%d tiles for one pixel", len(tiles))
	}
}

func TestDifferErrorsAreRouted(t *testing.T) {
	check := func(t *testing.T, err error, route string) {
		t.Helper()
		appErr, ok := errors.FromError(err)
		if !ok {
			t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
		}
		if !appErr.HasRoute(route) {
			t.Errorf("unexpected failure path: %s, want %s", appErr.Route(), route)
		}
	}
	t.Run("level", func(t *testing.T) {
		_, err := NewDiffer(42)
		check(t, err, "NewDiffer.TilesLevelInvalid")
	})
	t.Run("size", func(t *testing.T) {
		_, _, err := newDiffer(t).Next(image.NewRGBA(image.Rect(0, 0, 0, 0)))
		check(t, err, "Differ.Next.TilesSizeInvalid")
		_, _, err = newDiffer(t).Next(image.NewRGBA(image.Rect(0, 0, 4000, 4000)))
		check(t, err, "Differ.Next.TilesSizeInvalid")
	})
	t.Run("compress", func(t *testing.T) {
		d := newDiffer(t)
		d.sink = failingWriter{}
		_, _, err := d.Next(picture(64, 64, 1))
		check(t, err, "Differ.compress.TilesCompressFailed")
	})
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func deflated(t *testing.T, raw []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	zw, err := flate.NewWriter(&b, flate.BestSpeed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := zw.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestCanvasRefusesTilesThatLieWithARoute(t *testing.T) {
	good := &protocol.Tile{Frame: 1, Width: 70, Height: 64, X: 1, Y: 0, Total: 2, Data: deflated(t, make([]byte, 6*64*4))}
	tests := []struct {
		name  string
		tile  func() *protocol.Tile
		route string
	}{
		{"data that is not deflate", func() *protocol.Tile { c := *good; c.Data = []byte{0xFF, 0xFF, 0xFF}; return &c }, "Canvas.inflate.TilesInflateFailed"},
		{"too few pixels", func() *protocol.Tile { c := *good; c.Data = deflated(t, make([]byte, 100)); return &c }, "Canvas.inflate.TilesDataSize"},
		{"too many pixels", func() *protocol.Tile { c := *good; c.Data = deflated(t, make([]byte, 6*64*4+1)); return &c }, "Canvas.inflate.TilesDataSize"},
		{"a bomb", func() *protocol.Tile { c := *good; c.Data = deflated(t, make([]byte, 50<<20)); return &c }, "Canvas.inflate.TilesDataSize"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewCanvas().Apply(tt.tile())
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("unexpected failure path: %s, want %s", appErr.Route(), tt.route)
			}
		})
	}
	t.Run("two totals", func(t *testing.T) {
		c := NewCanvas()
		if _, err := c.Apply(good); err != nil {
			t.Fatal(err)
		}
		other := *good
		other.X, other.Total = 0, 3
		_, err := c.Apply(&other)
		appErr, ok := errors.FromError(err)
		if !ok || !appErr.HasRoute("Canvas.Apply.TilesInconsistent") {
			t.Errorf("unexpected failure path: %v", err)
		}
	})
	t.Run("outside the picture", func(t *testing.T) {
		out := *good
		out.X = 5
		_, err := NewCanvas().Apply(&out)
		appErr, ok := errors.FromError(err)
		if !ok || !appErr.HasRoute("Canvas.Apply.TilesInconsistent") {
			t.Errorf("unexpected failure path: %v", err)
		}
	})
	t.Run("a duplicate", func(t *testing.T) {
		c := NewCanvas()
		if res, err := c.Apply(good); err != nil || res != Applied {
			t.Fatal(res, err)
		}
		if res, err := c.Apply(good); err != nil || res != Duplicate {
			t.Fatalf("%v %v", res, err)
		}
	})
	t.Run("changes to a picture the canvas does not have", func(t *testing.T) {
		based := *good
		based.Base = 9
		based.Frame = 10
		if res, err := NewCanvas().Apply(&based); err != nil || res != Stale {
			t.Fatalf("%v %v", res, err)
		}
	})
}

func TestTilesSurviveTheProtocol(t *testing.T) {
	img := picture(200, 130, 9)
	d, c := newDiffer(t), NewCanvas()
	_, tiles, _ := d.Next(img)
	for _, tile := range tiles {
		b, err := protocol.Append(nil, tile)
		if err != nil {
			t.Fatal(err)
		}
		m, err := protocol.Decode(b)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := c.Apply(m.(*protocol.Tile)); err != nil {
			t.Fatal(err)
		}
	}
	if !equalImages(c.Image(), img) {
		t.Error("the picture changed on the way")
	}
}
