package tiles

import (
	"bytes"
	"compress/flate"
	"image"
	"io"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
)

// Result says what Canvas.Apply did with a tile.
type Result int

// The results of Apply.
const (
	// Applied: the tile is on the picture; its frame is not complete yet.
	Applied Result = iota
	// Complete: the tile was the last of its frame. The picture is that frame now, and the client confirms it
	// (protocol.FrameConfirm with Canvas.Confirmed).
	Complete
	// Stale: the tile is of an older frame than one the Canvas saw, or the changes to a picture the Canvas has not (its
	// base is not the confirmed one). It was dropped.
	Stale
	// Duplicate: the Canvas has this tile of this frame already.
	Duplicate
)

// Canvas is the client's picture. It shows the tiles as they arrive, keeps the last complete picture to put tiles back
// to, and says when a frame is complete. It belongs to one session of one Differ (the frame numbers count from 1 in each),
// and it is not safe for concurrent use.
type Canvas struct {
	display, confirmed *image.RGBA
	across, down       int
	// confirmedFrame is the frame the confirmed picture is, 0 for none.
	confirmedFrame uint64
	// current is the newest frame seen, got the tiles of it received, count how many, and total the tiles it has.
	current uint64
	got     []bool
	count   uint32
	total   uint32
	// dirty are the tiles of display that differ from confirmed (a tile of a frame that is not complete was applied).
	dirty []bool
	// stale counts the tiles dropped in a row for their base.
	stale int

	zr      io.ReadCloser
	scratch []byte
}

// NewCanvas makes an empty Canvas.
func NewCanvas() *Canvas { return &Canvas{} }

// Image is the picture to show: the tiles applied so far. It is nil before the first tile, and it is the Canvas's own,
// which Apply changes: copy it, or draw it before the next Apply.
func (c *Canvas) Image() *image.RGBA { return c.display }

// Confirmed is the number of the last complete frame, 0 for none.
func (c *Canvas) Confirmed() uint64 { return c.confirmedFrame }

// StaleRun is the number of tiles dropped for their base since a tile was last applied. A client that sees it grow (the
// host bases its pictures on a confirm that never reached it) sends protocol.Refresh.
func (c *Canvas) StaleRun() int { return c.stale }

// Reset forgets the picture, as a client does that asks for protocol.Refresh.
func (c *Canvas) Reset() { *c = Canvas{zr: c.zr, scratch: c.scratch} }

// Apply puts a tile on the picture. A tile of another size than the picture starts the picture again (the frame is a whole
// one then: its base is 0).
//
// The error has the tags TilesInconsistent (the tile contradicts one of its frame or its geometry), TilesInflateFailed
// and TilesDataSize (the data is not the pixels of the tile).
func (c *Canvas) Apply(t *protocol.Tile) (Result, error) {
	w, h := int(t.Width), int(t.Height)
	across, down := protocol.TilesAcross(w), protocol.TilesAcross(h)
	if c.display == nil || c.display.Rect.Dx() != w || c.display.Rect.Dy() != h {
		if t.Base != 0 {
			c.stale++
			return Stale, nil
		}
		c.display, c.confirmed = image.NewRGBA(image.Rect(0, 0, w, h)), image.NewRGBA(image.Rect(0, 0, w, h))
		c.across, c.down = across, down
		c.confirmedFrame, c.current, c.count, c.total = 0, 0, 0, 0
		c.got, c.dirty = make([]bool, across*down), make([]bool, across*down)
	}
	if t.Base != 0 && t.Base != c.confirmedFrame || t.Frame < c.current {
		c.stale++
		return Stale, nil
	}
	if t.Frame > c.current {
		c.current, c.count, c.total = t.Frame, 0, t.Total
		clear(c.got)
	}
	if t.Total != c.total {
		return Stale, errors.New("TilesInconsistent", "two tiles of one frame disagree on how many tiles it has", "frame", t.Frame, "total", t.Total, "other", c.total)
	}
	idx := int(t.Y)*c.across + int(t.X)
	if idx >= len(c.got) {
		return Stale, errors.New("TilesInconsistent", "the tile is outside the picture", "x", t.X, "y", t.Y)
	}
	if c.got[idx] {
		return Duplicate, nil
	}
	if err := c.inflate(t); err != nil {
		return Stale, err
	}
	c.stale = 0
	c.got[idx], c.dirty[idx] = true, true
	c.count++
	if c.count < c.total {
		return Applied, nil
	}
	c.complete()
	return Complete, nil
}

// complete makes the picture the frame that just got its last tile: the tiles of the frame are kept as the confirmed
// picture, and the tiles an older, unfinished frame changed that this one does not are put back.
func (c *Canvas) complete() {
	for idx := range c.got {
		switch {
		case c.got[idx]:
			c.copyTile(c.confirmed, c.display, idx)
		case c.dirty[idx]:
			c.copyTile(c.display, c.confirmed, idx)
		}
		c.dirty[idx] = false
	}
	c.confirmedFrame = c.current
}

// copyTile copies one tile of the pictures, which have the same size.
func (c *Canvas) copyTile(dst, src *image.RGBA, idx int) {
	w, h := dst.Rect.Dx(), dst.Rect.Dy()
	x0, y0 := (idx%c.across)*protocol.TileSize, (idx/c.across)*protocol.TileSize
	x1, y1 := min(x0+protocol.TileSize, w), min(y0+protocol.TileSize, h)
	for y := y0; y < y1; y++ {
		off := dst.PixOffset(x0, y)
		copy(dst.Pix[off:off+(x1-x0)*protocol.BytesPerPixel], src.Pix[off:off+(x1-x0)*protocol.BytesPerPixel])
	}
}

// inflate decodes the data of a tile into the picture. It reads at most the size of the tile, so data that inflates to
// more is refused, whatever it claims.
func (c *Canvas) inflate(t *protocol.Tile) error {
	w, h := c.display.Rect.Dx(), c.display.Rect.Dy()
	x0, y0 := int(t.X)*protocol.TileSize, int(t.Y)*protocol.TileSize
	x1, y1 := min(x0+protocol.TileSize, w), min(y0+protocol.TileSize, h)
	rowBytes := (x1 - x0) * protocol.BytesPerPixel
	want := rowBytes * (y1 - y0)
	if c.zr == nil {
		c.zr = flate.NewReader(bytes.NewReader(t.Data))
	} else if err := c.zr.(flate.Resetter).Reset(bytes.NewReader(t.Data), nil); err != nil {
		return errors.NewErr("TilesInflateFailed", err).SetMetadata("x", t.X, "y", t.Y)
	}
	if cap(c.scratch) < want+1 {
		c.scratch = make([]byte, want+1)
	}
	buf := c.scratch[:want+1]
	n, err := io.ReadFull(c.zr, buf)
	switch {
	case err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF):
		return errors.NewErr("TilesInflateFailed", err).SetMetadata("x", t.X, "y", t.Y)
	case n != want:
		return errors.New("TilesDataSize", "the tile does not inflate to the size of its pixels", "x", t.X, "y", t.Y, "want", want, "got", n)
	}
	for y := y0; y < y1; y++ {
		off := c.display.PixOffset(x0, y)
		copy(c.display.Pix[off:off+rowBytes], buf[(y-y0)*rowBytes:(y-y0+1)*rowBytes])
	}
	return nil
}
