package markdown

import "github.com/rm4n0s/emetos/internal/gui/kit"

// Controller moves a View: it scrolls to the block that a line of the source belongs to.
type Controller struct {
	scroll kit.ScrollHandle
	view   controlled
	// pending is the line a scroll waits for, because the view has no blocks yet.
	pending    int
	hasPending bool
}

// controlled is what a Controller needs from the view it moves.
type controlled interface {
	// scrollToLine scrolls to the block of a line; false when the view has no blocks (yet).
	scrollToLine(line int) bool
	// topLine is the first line of the block at the top of the view.
	topLine() int
	// stats describes the view.
	stats() Stats
}

// Stats describes a preview, for tests and the dev server (SPEC §15.2).
type Stats struct {
	// Blocks is the number of blocks of the document, Built the number that have widgets now (the ones near the viewport).
	Blocks int `json:"blocks"`
	Built  int `json:"built"`
	// Builds is the number of block widgets made so far: a block that stays in the window, or comes back with the same text,
	// does not add to it.
	Builds int `json:"builds"`
	// Measured is the number of blocks whose height was measured; the others have an estimate.
	Measured int `json:"measured"`
	// Parsing tells whether the text is being parsed, or waits for the delay before it is.
	Parsing bool `json:"parsing"`
	// TopLine is the first line of the block at the top.
	TopLine int `json:"topLine"`
	// ScrollTop and ContentHeight are the scroll position and the height of the content, in logical pixels.
	ScrollTop     float64 `json:"scrollTop"`
	ContentHeight float64 `json:"contentHeight"`
	// ImageBytes and Images are the bytes of pixels and the number of entries the image cache of the view holds.
	ImageBytes int `json:"imageBytes"`
	Images     int `json:"images"`
}

// Inspector is implemented by the state of a preview, so that a tool can read its Stats from the element tree.
type Inspector interface {
	// MarkdownStats describes the preview.
	MarkdownStats() Stats
}

// NewController makes a controller for a View.
func NewController() *Controller { return &Controller{} }

// ScrollToLine scrolls the view so that the block the line (counted from 0) belongs to is at the top: the last block that
// starts at or before the line. It waits for the view to have blocks when it has none yet.
func (c *Controller) ScrollToLine(line int) {
	if c.view != nil && c.view.scrollToLine(line) {
		c.hasPending = false
		return
	}
	c.pending, c.hasPending = line, true
}

// TopLine is the first line of the source of the block at the top of the view.
func (c *Controller) TopLine() int {
	if c.view == nil {
		return 0
	}
	return c.view.topLine()
}

// Stats describes the view the controller moves; false while no view has it.
func (c *Controller) Stats() (Stats, bool) {
	if c.view == nil {
		return Stats{}, false
	}
	return c.view.stats(), true
}
