package kit

// Dragging the tabs of a strip (SPEC §10.3): the strip tells when a drag starts, moves and ends and
// shows where a dragged tab would go; the window decides what the drop does, because a tab can be
// dropped on another strip. TabsController gives the window the places of the strips and tabs and
// scrolls a strip.
//
// Ported from VS Code: src/vs/workbench/browser/parts/editor/multiEditorTabsControl.ts (the drag
// and drop of the tabs: updateDropFeedback, computeDropTarget) and
// src/vs/workbench/browser/parts/editor/media/multieditortabscontrol.css (the drop border)

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/ui"
)

// tabDropBorder is the width of the line that shows where a dragged tab goes
// (`.tab.drop-target-left::after`).
const tabDropBorder = 2.0

// TabDropFeedback tells a strip where a dragged tab would go. The strip draws a line there and
// builds its tabs again when the place changes. Give the same one to the strip through
// TabsOpts.Feedback and keep it between builds.
type TabDropFeedback struct {
	foundation.ChangeNotifier
	index int
}

// NewTabDropFeedback makes a feedback that shows nothing.
func NewTabDropFeedback() *TabDropFeedback {
	return &TabDropFeedback{ChangeNotifier: *foundation.NewChangeNotifier(), index: -1}
}

// Index is the place of the line: 0 is before the first tab, the number of tabs after the last one;
// -1 shows no line.
func (f *TabDropFeedback) Index() int { return f.index }

// Set moves the line to a place (-1 hides it) and tells the strip when it changed.
func (f *TabDropFeedback) Set(index int) {
	if index < 0 {
		index = -1
	}
	if f.index == index {
		return
	}
	f.index = index
	f.NotifyListeners()
}

// TabsController gives code outside a strip the places of the strip and of its tabs in the window
// and scrolls it. Give one to TabsOpts.Controller.
type TabsController struct {
	strip  Bounds
	tabs   []*Bounds
	scroll ScrollHandle
}

// NewTabsController makes a controller.
func NewTabsController() *TabsController { return &TabsController{} }

// bounds is the Bounds of the tab at an index, made when first asked for.
func (c *TabsController) bounds(index int) *Bounds {
	for len(c.tabs) <= index {
		c.tabs = append(c.tabs, &Bounds{})
	}
	return c.tabs[index]
}

// Bounds is the place of the strip in the window; false while it is not on screen.
func (c *TabsController) Bounds() (ui.Rect, bool) { return c.strip.Rect() }

// TabRect is the place of the tab at an index in the window, wherever the strip is scrolled to; false
// for a tab that is not on screen.
func (c *TabsController) TabRect(index int) (ui.Rect, bool) {
	if index < 0 || index >= len(c.tabs) {
		return ui.Rect{}, false
	}
	return c.tabs[index].Rect()
}

// ScrollBy scrolls the strip sideways by a distance, within its content.
func (c *TabsController) ScrollBy(dx float64) { c.scroll.ScrollBy(dx, 0) }

// Scrolled is how far the strip is scrolled.
func (c *TabsController) Scrolled() float64 { left, _ := c.scroll.Position(); return left }

// Reveal scrolls the strip just far enough to show the tab at an index, as far as it fits.
func (c *TabsController) Reveal(index int) {
	strip, ok := c.Bounds()
	if !ok {
		return
	}
	tab, ok := c.TabRect(index)
	if !ok {
		return
	}
	if dx := RevealDelta(tab, strip); dx != 0 {
		c.ScrollBy(dx)
	}
}

// RevealDelta is how far a strip scrolls to show a tab: negative to the left, positive to the right,
// 0 when the tab is in view. A tab wider than the strip shows its start.
func RevealDelta(tab, strip ui.Rect) float64 {
	switch {
	case tab.Left < strip.Left:
		return tab.Left - strip.Left
	case tab.Right > strip.Right && tab.Width() <= strip.Width():
		return tab.Right - strip.Right
	case tab.Right > strip.Right:
		return tab.Left - strip.Left
	}
	return 0
}

// TabEdgeStep is how far a strip scrolls in one tick for a dragged tab with the pointer at x, in a
// strip from left to right: nothing in the middle, up to 14 px towards the edge that the pointer is
// near (within 35 px) or past.
func TabEdgeStep(x, left, right float64) float64 { return edgeStep(x-left, right-left) }
