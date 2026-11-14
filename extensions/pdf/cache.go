package pdf

import (
	"math"

	gui "github.com/rm4n0s/glutter/ui"
)

// cacheLimit is how many bytes of pixels the drawn pages of a tab may hold.
const cacheLimit = 128 << 20

// drawn is a page drawn at a resolution.
type drawn struct {
	page  int
	dpi   float64
	img   *gui.Image
	bytes int
}

// pageCache keeps drawn pages up to a number of bytes of pixels, the least recently used dropped first.
type pageCache struct {
	limit int
	size  int
	// entries are in the order of their use, the least recent first.
	entries []*drawn
}

func newPageCache(limit int) *pageCache { return &pageCache{limit: limit} }

// sameDPI tells whether two resolutions are the same picture.
func sameDPI(a, b float64) bool { return math.Abs(a-b) <= 0.005*max(a, b) }

// get is the page drawn at the resolution, or nil; it counts as a use.
func (c *pageCache) get(page int, dpi float64) *drawn {
	for i, d := range c.entries {
		if d.page == page && sameDPI(d.dpi, dpi) {
			c.entries = append(append(c.entries[:i:i], c.entries[i+1:]...), d)
			return d
		}
	}
	return nil
}

// best is the page drawn at the highest resolution it was drawn at, or nil; it does not count as a use.
func (c *pageCache) best(page int) *drawn {
	var out *drawn
	for _, d := range c.entries {
		if d.page == page && (out == nil || d.dpi > out.dpi) {
			out = d
		}
	}
	return out
}

// put keeps a drawn page, in place of the same page at the same resolution, and drops the least recently used pages until the cache
// is within its limit (the new page stays, even alone over it).
func (c *pageCache) put(page int, dpi float64, img *gui.Image) {
	for i, d := range c.entries {
		if d.page == page && sameDPI(d.dpi, dpi) {
			c.size -= d.bytes
			c.entries = append(c.entries[:i:i], c.entries[i+1:]...)
			break
		}
	}
	d := &drawn{page: page, dpi: dpi, img: img, bytes: img.Width() * img.Height() * 4}
	c.entries = append(c.entries, d)
	c.size += d.bytes
	for c.size > c.limit && len(c.entries) > 1 {
		c.size -= c.entries[0].bytes
		c.entries = c.entries[1:]
	}
}

// clear drops every page. The pictures are not disposed: one may still be on screen until the next frame, and the collector frees
// them.
func (c *pageCache) clear() {
	c.entries, c.size = nil, 0
}
