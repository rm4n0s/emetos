package tiles

import (
	"bytes"
	"compress/flate"
	"hash/maphash"
	"image"
	"io"
	"slices"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
)

// Level is the level of deflate Remote Work uses for its tiles: the default one. Measured on 1280x800 pictures of an editor
// (SPEC §10.18 "As built (step 15.1)"), it makes a whole picture 14% smaller than flate.BestSpeed (113 KB against 132 KB)
// and a scroll 12% smaller, for 5 ms more a whole picture, on the goroutine that encodes; flate.BestCompression saves 2.4%
// more and takes seven times as long, and flate.HuffmanOnly makes a whole picture seven times larger.
const Level = flate.DefaultCompression

// MaxPending is the number of pictures that were sent and not confirmed that a Differ remembers (as hashes). A confirm
// of an older one makes it send the whole picture again.
const MaxPending = 16

// Differ makes the tiles of each new picture of a view that the client does not have yet. It is not safe for concurrent
// use: the one goroutine that encodes calls it.
type Differ struct {
	seed maphash.Seed

	// size of the pictures; a picture of another size is sent whole.
	width, height int
	// confirmed is the picture the client has (0 for none) and confirmedHashes the hash of each of its tiles.
	confirmed       uint64
	confirmedHashes []uint64
	// pending are the pictures sent after it and not confirmed, oldest first.
	pending []pendingFrame
	// last is the number of the last picture made.
	last uint64
	// refresh says that the next picture is sent whole.
	refresh bool

	// scratch is the pixels of the tile being deflated, out the deflated bytes, zw the compressor.
	scratch []byte
	out     bytes.Buffer
	zw      *flate.Writer
	// sink is where the compressor writes: out, except in a test of a writer that fails.
	sink io.Writer
}

type pendingFrame struct {
	frame  uint64
	hashes []uint64
}

// NewDiffer makes a Differ that deflates its tiles at the level of compress/flate (Level is what Remote Work uses).
//
// The error has the tag TilesLevelInvalid.
func NewDiffer(level int) (*Differ, error) {
	zw, err := flate.NewWriter(nil, level)
	if err != nil {
		return nil, errors.NewErr("TilesLevelInvalid", err).SetMetadata("level", level)
	}
	d := &Differ{seed: maphash.MakeSeed(), zw: zw, refresh: true}
	d.sink = &d.out
	return d, nil
}

// Refresh makes the next picture a whole one, as the periodic refresh of the session does (SPEC §10.18): the client keeps
// the picture it has, and the whole one makes up for any drift between the two sides.
func (d *Differ) Refresh() { d.refresh = true }

// Reset forgets what the client has and makes the next picture a whole one: the client asked for it (protocol.Refresh),
// having lost its picture.
func (d *Differ) Reset() {
	d.confirmed, d.confirmedHashes, d.pending = 0, nil, d.pending[:0]
	d.refresh = true
}

// Confirmed is the number of the picture the client confirmed, 0 for none.
func (d *Differ) Confirmed() uint64 { return d.confirmed }

// Confirm notes that the client has the whole picture frame. It returns false for a picture it does not know (it was
// forgotten, or never made); the next picture is then a whole one, because the client's base is not one the Differ can
// compare with. A confirm of a picture older than the confirmed one changes nothing.
func (d *Differ) Confirm(frame uint64) bool {
	if frame <= d.confirmed {
		return true
	}
	i := slices.IndexFunc(d.pending, func(p pendingFrame) bool { return p.frame == frame })
	if i < 0 {
		d.refresh = true
		return false
	}
	d.confirmed, d.confirmedHashes = frame, d.pending[i].hashes
	d.pending = slices.Delete(d.pending, 0, i+1)
	return true
}

// Next compares the picture with the confirmed one and returns the number it gives the picture and the Tile messages of
// the tiles that differ, each deflated. When nothing differs and the client has everything that was sent, it returns no
// tiles and a frame number of 0 (there is nothing to send). When nothing differs from the confirmed picture but pictures
// were sent that are not confirmed (the client may show tiles of them), one tile is sent, so that the client completes a
// picture and puts the tiles it changed back.
//
// A picture of another size than the one before is sent whole, and so is the first, one after Refresh and one after a
// confirm that was not known. The pixels are read, not kept.
//
// The error has the tags TilesSizeInvalid and TilesCompressFailed.
func (d *Differ) Next(img *image.RGBA) (frame uint64, tiles []*protocol.Tile, err error) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	if !protocol.ValidSize(w, h) {
		return 0, nil, errors.New("TilesSizeInvalid", "the picture has a size the protocol does not allow", "width", w, "height", h)
	}
	if w != d.width || h != d.height {
		// the hashes of the pictures of the old size say nothing about this one
		d.width, d.height = w, h
		d.Reset()
	}
	across, down := protocol.TilesAcross(w), protocol.TilesAcross(h)
	hashes := make([]uint64, across*down)
	d.hashTiles(img, hashes, across)
	full := d.refresh || d.confirmed == 0
	var changed []int
	for i, hv := range hashes {
		if full || hv != d.confirmedHashes[i] {
			changed = append(changed, i)
		}
	}
	if len(changed) == 0 {
		if len(d.pending) == 0 {
			return 0, nil, nil
		}
		changed = []int{0}
	}
	base := d.confirmed
	if full {
		base = 0
	}
	d.last++
	frame = d.last
	tiles = make([]*protocol.Tile, 0, len(changed))
	for _, i := range changed {
		x, y := i%across, i/across
		data, err := d.compress(img, x, y)
		if err != nil {
			return 0, nil, err
		}
		tiles = append(tiles, &protocol.Tile{
			Frame: frame, Base: base, Width: uint16(w), Height: uint16(h), X: uint16(x), Y: uint16(y),
			Total: uint32(len(changed)), Data: data,
		})
	}
	d.refresh = false
	d.pending = append(d.pending, pendingFrame{frame: frame, hashes: hashes})
	if len(d.pending) > MaxPending {
		d.pending = slices.Delete(d.pending, 0, len(d.pending)-MaxPending)
	}
	return frame, tiles, nil
}

// hashTiles fills hashes with the hash of each tile of the picture, row by row (the tiles at the edges are smaller).
func (d *Differ) hashTiles(img *image.RGBA, hashes []uint64, across int) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	for i := range hashes {
		x0, y0 := (i%across)*protocol.TileSize, (i/across)*protocol.TileSize
		x1, y1 := min(x0+protocol.TileSize, w), min(y0+protocol.TileSize, h)
		var sum uint64
		for y := y0; y < y1; y++ {
			off := img.PixOffset(img.Rect.Min.X+x0, img.Rect.Min.Y+y)
			sum = sum*1099511628211 + maphash.Bytes(d.seed, img.Pix[off:off+(x1-x0)*protocol.BytesPerPixel])
		}
		hashes[i] = sum
	}
}

// compress deflates the pixels of one tile.
func (d *Differ) compress(img *image.RGBA, tx, ty int) ([]byte, error) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	x0, y0 := tx*protocol.TileSize, ty*protocol.TileSize
	x1, y1 := min(x0+protocol.TileSize, w), min(y0+protocol.TileSize, h)
	rowBytes := (x1 - x0) * protocol.BytesPerPixel
	d.scratch = slices.Grow(d.scratch[:0], rowBytes*(y1-y0))
	for y := y0; y < y1; y++ {
		off := img.PixOffset(img.Rect.Min.X+x0, img.Rect.Min.Y+y)
		d.scratch = append(d.scratch, img.Pix[off:off+rowBytes]...)
	}
	d.out.Reset()
	d.zw.Reset(d.sink)
	if _, err := d.zw.Write(d.scratch); err != nil {
		return nil, errors.NewErr("TilesCompressFailed", err).SetMetadata("x", tx, "y", ty)
	}
	if err := d.zw.Close(); err != nil {
		return nil, errors.NewErr("TilesCompressFailed", err).SetMetadata("x", tx, "y", ty)
	}
	return bytes.Clone(d.out.Bytes()), nil
}
