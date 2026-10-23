package editor

import (
	"container/list"

	"github.com/rm4n0s/glutter/ui"
)

// DefaultParagraphCacheSize is how many paragraphs the cache keeps (SPEC §9.2).
const DefaultParagraphCacheSize = 2000

// paragraphKey identifies a paragraph: what it shows and everything that changes how it
// looks. A change of any of them is a miss, and the stale entries leave by the LRU.
type paragraphKey struct {
	// text is the text of the row.
	text string
	// style is the fingerprint of the font, size, line height, colors and features.
	style string
	// tabSize is the distance between tab stops.
	tabSize int
	// spans are the colors and font styles of the parts of the text that the tokens and the
	// theme give it; empty for text in one style.
	spans string
}

// ParagraphCache keeps the laid-out paragraphs of rows, so that a row whose text and look
// did not change is not shaped again. It drops the least recently used paragraph when it
// holds more than its capacity. It is used on the UI thread only.
type ParagraphCache struct {
	capacity int
	items    map[paragraphKey]*list.Element
	order    *list.List // front is the most recently used
	hits     int
	builds   int
}

type cacheEntry struct {
	key       paragraphKey
	paragraph *ui.Paragraph
}

// NewParagraphCache makes a cache of the given capacity; zero or less means
// DefaultParagraphCacheSize.
func NewParagraphCache(capacity int) *ParagraphCache {
	if capacity <= 0 {
		capacity = DefaultParagraphCacheSize
	}
	return &ParagraphCache{capacity: capacity, items: map[paragraphKey]*list.Element{}, order: list.New()}
}

// Get returns the paragraph of a key, calling build when it is not cached.
func (c *ParagraphCache) get(key paragraphKey, build func() *ui.Paragraph) *ui.Paragraph {
	if el, ok := c.items[key]; ok {
		c.order.MoveToFront(el)
		c.hits++
		return el.Value.(*cacheEntry).paragraph
	}
	c.builds++
	p := build()
	c.items[key] = c.order.PushFront(&cacheEntry{key: key, paragraph: p})
	for c.order.Len() > c.capacity {
		oldest := c.order.Back()
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*cacheEntry).key)
	}
	return p
}

// Len is the number of paragraphs held.
func (c *ParagraphCache) Len() int { return c.order.Len() }

// Hits is how many times a paragraph was found in the cache.
func (c *ParagraphCache) Hits() int { return c.hits }

// Builds is how many times a paragraph had to be built.
func (c *ParagraphCache) Builds() int { return c.builds }

// Clear forgets every paragraph (the counters stay).
func (c *ParagraphCache) Clear() {
	c.items = map[paragraphKey]*list.Element{}
	c.order.Init()
}
