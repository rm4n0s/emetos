package markdown

import (
	"container/list"
	"context"
	"image"
	"math"
	"os"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
	xdraw "golang.org/x/image/draw"

	// the formats an image of a document can have
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/webp"

	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The limits of the decoding of images.
const (
	// DefaultImageBudget is the number of bytes of decoded pixels that an ImageCache keeps.
	DefaultImageBudget = 64 << 20
	// maxImagePixels is the largest image that is decoded, in pixels: a bigger one is not shown (its alt text is), because its
	// pixels alone would take 256 MB.
	maxImagePixels = 64 << 20
	// maxImageDecodes is the number of images that are decoded at the same time.
	maxImageDecodes = 2
	// maxImageWidth and maxImageHeight are the largest size an image is shown at, in logical pixels.
	maxImageWidth, maxImageHeight = 800.0, 600.0
)

// An ImageCache decodes the images of Markdown documents off the UI thread, at the size they are shown, and keeps the decoded
// pixels within a budget of bytes, least recently used first. An image is identified by its path, its size and modification
// time, and the size it is shown at, so a changed file or a resized window is another entry. A file that cannot be decoded is
// an entry too (with its error), so it is not read again at every build.
//
// Share one cache between the previews of a window. Its methods are for the UI thread; only the decoding runs elsewhere.
type ImageCache struct {
	budget int

	// ctx ends the decodings that run when the cache is closed.
	ctx    context.Context
	cancel context.CancelFunc

	mu   sync.Mutex // guards post only
	post func(func())

	entries map[imageKey]*imageEntry
	lru     *list.List // of *imageEntry, most recently used first
	used    int
	loads   map[imageKey]*imageLoad
	slots   chan struct{}

	// decode and stamp are the decoding and the reading of the file's stamp; tests replace them.
	decode func(ctx context.Context, path string, fit imageFit) (imageResult, error)
	stamp  func(path string) (time.Time, int64, error)
}

// NewImageCache makes a cache that keeps at most budget bytes of decoded pixels (DefaultImageBudget when it is not positive).
func NewImageCache(budget int) *ImageCache {
	if budget <= 0 {
		budget = DefaultImageBudget
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &ImageCache{
		ctx: ctx, cancel: cancel, budget: budget, entries: map[imageKey]*imageEntry{}, lru: list.New(), loads: map[imageKey]*imageLoad{},
		slots: make(chan struct{}, maxImageDecodes), decode: decodeImage, stamp: fileStamp,
	}
}

// Close ends the decodings that are running; the cache is not used after it.
func (c *ImageCache) Close() { c.cancel() }

// Budget is the number of bytes of pixels the cache keeps at most.
func (c *ImageCache) Budget() int { return c.budget }

// Used is the number of bytes of pixels the cache keeps now.
func (c *ImageCache) Used() int { return c.used }

// pending is the number of images that are being decoded.
func (c *ImageCache) pending() int { return len(c.loads) }

// Len is the number of entries (images and failures).
func (c *ImageCache) Len() int { return len(c.entries) }

// attach tells the cache how to run a function on the UI thread; the first view that is built does, and the cache is meant for
// the views of one window.
func (c *ImageCache) attach(post func(func())) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.post == nil {
		c.post = post
	}
}

func (c *ImageCache) poster() func(func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.post
}

// imageFit is the size an image is shown at: at most max logical pixels, with ratio device pixels to a logical pixel.
type imageFit struct {
	maxWidth, maxHeight, ratio float64
}

// imageKey identifies an entry: the file as it is now, and the size it is shown at.
type imageKey struct {
	path     string
	modTime  int64
	size     int64
	maxW     int
	maxH     int
	ratio100 int
}

// imageResult is a decoded image, or the reason it is not.
type imageResult struct {
	// pic holds the pixels, at most the size the image is shown at in device pixels.
	pic *kit.Picture
	// width and height are the size the image is shown at, in logical pixels.
	width, height float64
	err           error
	// abandoned says that nothing was decoded because no view waited for the image or the cache was closed; it is not stored.
	abandoned bool
}

type imageEntry struct {
	key   imageKey
	res   imageResult
	bytes int
	elem  *list.Element
}

// imageLoad is a decoding that was asked for: the views that wait for it.
type imageLoad struct {
	waiters map[int]func(imageResult)
	next    int
}

// ImageRequest says which image a view wants and how large.
type imageRequest struct {
	path string
	fit  imageFit
}

func (c *ImageCache) keyOf(ctx context.Context, req imageRequest) (imageKey, error) {
	if err := ctx.Err(); err != nil {
		return imageKey{}, errors.NewErr("ImageRequestCancelled", err)
	}
	mod, size, err := c.stamp(req.path)
	if err != nil {
		return imageKey{}, err
	}
	return imageKey{
		path: req.path, modTime: mod.UnixNano(), size: size,
		maxW: int(math.Ceil(req.fit.maxWidth)), maxH: int(math.Ceil(req.fit.maxHeight)), ratio100: int(math.Round(req.fit.ratio * 100)),
	}, nil
}

// Get gives the entry of an image if it is in the cache (and makes it the most recently used), or false when it has to be
// loaded. An error is the failure to read the file's stamp, which is the entry's result.
func (c *ImageCache) get(ctx context.Context, req imageRequest) (imageKey, imageResult, bool) {
	key, err := c.keyOf(ctx, req)
	if err != nil {
		return imageKey{}, imageResult{err: err}, true
	}
	if e, ok := c.entries[key]; ok {
		c.lru.MoveToFront(e.elem)
		return key, e.res, true
	}
	return key, imageResult{}, false
}

// load decodes an image off the UI thread and calls done with the result on the UI thread, unless cancel was called first. A
// decoding that is already running is shared. Decodings that nobody waits for any more are not started.
func (c *ImageCache) load(key imageKey, req imageRequest, done func(imageResult)) (cancel func()) {
	ctx := c.ctx
	l := c.loads[key]
	if l != nil {
		id := l.next
		l.next++
		l.waiters[id] = done
		return func() { delete(l.waiters, id) }
	}
	l = &imageLoad{waiters: map[int]func(imageResult){0: done}, next: 1}
	c.loads[key] = l
	post := c.poster()
	go func() {
		var res imageResult
		select {
		case c.slots <- struct{}{}:
			// nobody waits for it any more: do not read the file (waiters is read on the UI thread, so ask it)
			wanted := make(chan bool, 1)
			post(func() { wanted <- len(l.waiters) > 0 })
			select {
			case yes := <-wanted:
				if yes {
					var err error
					res, err = c.decode(ctx, key.path, req.fit)
					if err != nil {
						res = imageResult{err: err}
					}
				} else {
					res.abandoned = true
				}
			case <-ctx.Done():
				res.abandoned = true
			}
			<-c.slots
		case <-ctx.Done():
			res.abandoned = true
		}
		post(func() { c.finish(key, l, res) })
	}()
	return func() { delete(l.waiters, 0) }
}

// finish stores the result of a load and tells the views that wait for it.
func (c *ImageCache) finish(key imageKey, l *imageLoad, res imageResult) {
	delete(c.loads, key)
	if res.abandoned {
		return
	}
	e := &imageEntry{key: key, res: res}
	if res.pic != nil {
		e.bytes = res.pic.Width() * res.pic.Height() * 4
	}
	e.elem = c.lru.PushFront(e)
	c.entries[key] = e
	c.used += e.bytes
	c.evict(e)
	for _, f := range l.waiters {
		f(res)
	}
}

// evict drops the least recently used entries until the cache is within its budget; the entry that was just added stays, even
// when it alone is over the budget (a view is about to show it).
func (c *ImageCache) evict(keep *imageEntry) {
	for c.used > c.budget {
		back := c.lru.Back()
		if back == nil || back.Value.(*imageEntry) == keep {
			return
		}
		e := back.Value.(*imageEntry)
		c.lru.Remove(back)
		delete(c.entries, e.key)
		c.used -= e.bytes
	}
}

// fileStamp is the modification time and the size of a file.
func fileStamp(path string) (time.Time, int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}, 0, errors.NewErr("ImageStatFailed", err).SetMetadata("path", path)
	}
	return info.ModTime(), info.Size(), nil
}

// fitImage is the size in logical pixels an image of w by h pixels is shown at: its own size (one pixel is one logical
// pixel), made smaller to fit the limits, never larger.
func fitImage(w, h int, fit imageFit) (float64, float64) {
	lw, lh := float64(w), float64(h)
	scale := math.Min(1, math.Min(fit.maxWidth/lw, fit.maxHeight/lh))
	return math.Max(1, math.Round(lw*scale)), math.Max(1, math.Round(lh*scale))
}

// decodeImage reads an image file and gives its pixels at the size it is shown at (SPEC §10.15 "As built (step 17b.2)"): the
// file is decoded whole (a decoder cannot do less) but only the pixels that are drawn are kept, so a screenshot of 4000 pixels
// that is shown 700 logical pixels wide costs 700 times the pixel ratio, not 4000, in memory.
func decodeImage(ctx context.Context, path string, fit imageFit) (imageResult, error) {
	if err := ctx.Err(); err != nil {
		return imageResult{}, errors.NewErr("ImageRequestCancelled", err)
	}
	f, err := os.Open(path)
	if err != nil {
		return imageResult{}, errors.NewErr("ImageOpenFailed", err).SetMetadata("path", path)
	}
	defer func() { _ = f.Close() }() // a file that was only read
	cfg, _, err := image.DecodeConfig(f)
	if err != nil {
		return imageResult{}, errors.NewErr("ImageDecodeFailed", err).SetMetadata("path", path)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxImagePixels {
		return imageResult{}, errors.New("ImageTooLarge", "the image has too many pixels to be shown", "path", path, "width", cfg.Width, "height", cfg.Height)
	}
	if _, err := f.Seek(0, 0); err != nil {
		return imageResult{}, errors.NewErr("ImageOpenFailed", err).SetMetadata("path", path)
	}
	src, _, err := image.Decode(f)
	if err != nil {
		return imageResult{}, errors.NewErr("ImageDecodeFailed", err).SetMetadata("path", path)
	}
	lw, lh := fitImage(cfg.Width, cfg.Height, fit)
	tw := min(cfg.Width, max(1, int(math.Round(lw*fit.ratio))))
	th := min(cfg.Height, max(1, int(math.Round(lh*float64(tw)/lw))))
	dst := image.NewRGBA(image.Rect(0, 0, tw, th))
	if tw == cfg.Width && th == cfg.Height {
		xdraw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, xdraw.Src)
	} else {
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Src, nil)
	}
	if err := ctx.Err(); err != nil {
		return imageResult{}, errors.NewErr("ImageRequestCancelled", err)
	}
	pic, err := kit.NewPicture(dst)
	if err != nil {
		return imageResult{}, err
	}
	return imageResult{pic: pic, width: lw, height: lh}, nil
}
