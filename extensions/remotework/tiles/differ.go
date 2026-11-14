package tiles

import (
	"bytes"
	"compress/flate"
	"hash/maphash"
	"image"
	"io"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"

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
// use: the one goroutine that encodes calls it (Next itself shares the tiles of a large picture out between goroutines of
// its own and waits for them).
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

	// level is the level of the compressors; deflaters are the compressors, the first made with the Differ and the others
	// when a picture has enough tiles for them (Next).
	level     int
	deflaters []*deflater
	// sink, when a test sets it, is where the first compressor writes instead of its own buffer (a writer that fails).
	sink io.Writer
}

// deflater is one compressor with its buffers: scratch is the pixels of the tile being deflated, out the deflated bytes.
type deflater struct {
	scratch []byte
	out     bytes.Buffer
	zw      *flate.Writer
}

const (
	// tilesPerDeflater is the number of tiles of a picture that are worth one more compressor: a typed character is a
	// handful of tiles and is deflated by the goroutine that calls Next, a scroll or a whole picture (hundreds of tiles,
	// 13 to 20 ms on one goroutine at 1920x1080) is shared out.
	tilesPerDeflater = 16
	// maxDeflaters is the most compressors a Differ makes (each holds about a megabyte).
	maxDeflaters = 8
)

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
	return &Differ{seed: maphash.MakeSeed(), level: level, deflaters: []*deflater{{zw: zw}}, refresh: true}, nil
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
	datas, err := d.compressAll(img, changed, across)
	if err != nil {
		return 0, nil, err
	}
	tiles = make([]*protocol.Tile, 0, len(changed))
	for n, i := range changed {
		x, y := i%across, i/across
		tiles = append(tiles, &protocol.Tile{
			Frame: frame, Base: base, Width: uint16(w), Height: uint16(h), X: uint16(x), Y: uint16(y),
			Total: uint32(len(changed)), Data: datas[n],
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

// compressAll deflates the tiles of a picture with the indexes given, in their order. A picture with many tiles is shared
// out between several compressors, each on a goroutine of its own, so that a scroll at 1920x1080 does not wait 13 ms and more
// for one goroutine (SPEC §10.18 "Speed"); the tiles are deflated each on its own, so the bytes are the same either way.
func (d *Differ) compressAll(img *image.RGBA, changed []int, across int) ([][]byte, error) {
	datas := make([][]byte, len(changed))
	workers := min(len(changed)/tilesPerDeflater, runtime.GOMAXPROCS(0), maxDeflaters)
	if workers < 2 || d.sink != nil {
		for n, i := range changed {
			data, err := d.compress(d.deflaters[0], img, i%across, i/across)
			if err != nil {
				return nil, err
			}
			datas[n] = data
		}
		return datas, nil
	}
	for len(d.deflaters) < workers {
		zw, err := flate.NewWriter(nil, d.level)
		if err != nil { // the level was accepted when the Differ was made
			workers = len(d.deflaters)
			break
		}
		d.deflaters = append(d.deflaters, &deflater{zw: zw})
	}
	var next atomic.Int64
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for k := range workers {
		wg.Go(func() {
			z := d.deflaters[k]
			for {
				n := int(next.Add(1)) - 1
				if n >= len(changed) {
					return
				}
				i := changed[n]
				if datas[n], errs[k] = d.compress(z, img, i%across, i/across); errs[k] != nil {
					return
				}
			}
		})
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return nil, err
		}
	}
	return datas, nil
}

// compress deflates the pixels of one tile with a compressor.
func (d *Differ) compress(z *deflater, img *image.RGBA, tx, ty int) ([]byte, error) {
	w, h := img.Rect.Dx(), img.Rect.Dy()
	x0, y0 := tx*protocol.TileSize, ty*protocol.TileSize
	x1, y1 := min(x0+protocol.TileSize, w), min(y0+protocol.TileSize, h)
	rowBytes := (x1 - x0) * protocol.BytesPerPixel
	z.scratch = slices.Grow(z.scratch[:0], rowBytes*(y1-y0))
	for y := y0; y < y1; y++ {
		off := img.PixOffset(img.Rect.Min.X+x0, img.Rect.Min.Y+y)
		z.scratch = append(z.scratch, img.Pix[off:off+rowBytes]...)
	}
	z.out.Reset()
	var sink io.Writer = &z.out
	if d.sink != nil {
		sink = d.sink
	}
	z.zw.Reset(sink)
	if _, err := z.zw.Write(z.scratch); err != nil {
		return nil, errors.NewErr("TilesCompressFailed", err).SetMetadata("x", tx, "y", ty)
	}
	if err := z.zw.Close(); err != nil {
		return nil, errors.NewErr("TilesCompressFailed", err).SetMetadata("x", tx, "y", ty)
	}
	return bytes.Clone(z.out.Bytes()), nil
}
