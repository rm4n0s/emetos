package kit

// Ported from VS Code: src/vs/base/browser/ui/list/listView.ts (the sectors of a row, the feedback of a
// drag, the scrolling near the edges), src/vs/base/browser/ui/list/list.ts (IListDragAndDrop) and
// src/vs/base/browser/ui/tree/abstractTree.ts (TreeNodeListDragAndDrop: expanding a folder that a drag rests
// on for 500 ms). Not ported: dragging to other applications and windows, and drops from other applications
// (a Glutter DragTarget does that), the drag image of a custom renderer.

import (
	"fmt"
	"math"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// Metrics of the drag and drop of lists and trees.
const (
	// dragSlop is how far the pointer moves with the button held before a drag starts (HTML5 drag and drop
	// starts after a few pixels).
	dragSlop = 4.0
	// dragEdge is the width of the strip at the top and at the bottom of the view in which a drag scrolls it
	// (`animateDragAndDropScrollTop`), and dragMaxStep the farthest it scrolls in one tick.
	dragEdge    = 35.0
	dragMaxStep = 14.0
	// dragScrollInterval is the time between two ticks of the scrolling (a frame).
	dragScrollInterval = 16 * time.Millisecond
	// DefaultAutoExpandDelay is how long a drag rests on a folder before it expands (500 ms).
	DefaultAutoExpandDelay = 500 * time.Millisecond
	// dragBetweenHeight is the height of the line between two rows (`list.dropBetweenBackground`).
	dragBetweenHeight = 1.0
)

// DropPosition says where, in relation to a row, something is dropped.
type DropPosition int

const (
	// DropBefore drops between the row and the one above it (the top quarter of the row).
	DropBefore DropPosition = iota
	// DropInside drops on the row (its middle half): the row has the background `list.dropBackground`.
	DropInside
	// DropAfter drops between the row and the one below it (the bottom quarter).
	DropAfter
)

// String names the position.
func (p DropPosition) String() string {
	switch p {
	case DropBefore:
		return "before"
	case DropInside:
		return "inside"
	case DropAfter:
		return "after"
	}
	return fmt.Sprintf("DropPosition(%d)", int(p))
}

// DropPositionAt is the position that a pointer at an offset from the top of a row of a height means: the top
// quarter of the row is before it, the bottom quarter after it and the middle half on it
// (`ListViewTargetSector`: top, center-top, center-bottom, bottom).
func DropPositionAt(offset, height float64) DropPosition {
	if height <= 0 {
		return DropInside
	}
	switch frac := offset / height; {
	case frac < 0.25:
		return DropBefore
	case frac >= 0.75:
		return DropAfter
	}
	return DropInside
}

// DropEffect is what a drop would do, as far as the list shows it.
type DropEffect int

const (
	// DropEffectNone refuses the drop: nothing is highlighted and releasing the pointer drops nothing.
	DropEffectNone DropEffect = iota
	// DropEffectMove accepts the drop (the dragged rows move).
	DropEffectMove
	// DropEffectCopy accepts the drop (the dragged rows are copied).
	DropEffectCopy
)

// DragAndDrop makes the rows of a list or tree draggable and the rows a drop target (`IListDragAndDrop`,
// `ITreeDragAndDrop`). The items of a drag are the keys of the rows: the selected rows when the pressed row is
// one of them, else the pressed row alone.
type DragAndDrop struct {
	// CanDrag says whether the rows can be dragged; without it they can.
	CanDrag func(keys []string) bool
	// DragLabel is the text of the drag image; without it the label of the row, or "N items" for several.
	DragLabel func(keys []string) string
	// CanDrop says what a drop on a row at a position would do. The target is the key of the row; it is
	// empty, with DropInside, for the empty part of the view below the rows (the root). It is called each
	// time the pointer moves to another row or position. Without it nothing can be dropped.
	CanDrop func(target string, position DropPosition, keys []string) DropEffect
	// OnDrop is called when the pointer is released over a target that CanDrop accepted.
	OnDrop func(target string, position DropPosition, keys []string)
	// AutoExpandDelay is how long a drag rests on a folder (a tree row that can be expanded) that accepts the
	// drop before the folder expands; the default is DefaultAutoExpandDelay.
	AutoExpandDelay foundation.Opt[time.Duration]
}

// DropTarget is the place under a drag: a row (the root when Key is empty) and the position in it, with what a
// drop there would do.
type DropTarget struct {
	// Key is the key of the row; empty for the root.
	Key string
	// Position is where in the row.
	Position DropPosition
	// Effect is what CanDrop said; DropEffectNone for a place that is no target.
	Effect DropEffect
}

// OK says that a drop on the target would be accepted.
func (t DropTarget) OK() bool { return t.Effect != DropEffectNone }

// dragSession is the state of one drag: the rows dragged, the label and the target under the pointer. It
// knows nothing of widgets and is tested on its own.
type dragSession struct {
	dnd    *DragAndDrop
	keys   []string
	label  string
	active bool
	// cancelled is true after Escape: the pointer is still down, nothing is dropped when it is released.
	cancelled bool
	target    DropTarget
}

// dragKeys are the items of a drag that starts on a row: the selected rows that are shown when the row is
// one of them, else the row.
func dragKeys(nav *rowNav, pressed string) []string {
	if !nav.IsSelected(pressed) {
		return []string{pressed}
	}
	var keys []string
	for _, k := range nav.SelectedKeys() {
		if nav.IndexOfKey(k) >= 0 {
			keys = append(keys, k)
		}
	}
	if len(keys) == 0 {
		return []string{pressed}
	}
	return keys
}

// targetAt is the drop target at a position of the content (the distance from the top of the first row).
func (s *dragSession) targetAt(nav *rowNav, y float64) DropTarget {
	i := nav.indexAt(y)
	t := DropTarget{Position: DropInside}
	if i >= 0 && i < nav.Len() {
		if nav.isBlocked(i) {
			return t
		}
		t.Key = nav.keys[i]
		t.Position = DropPositionAt(y-nav.rowTop(i), nav.rowHeightAt(i))
	}
	if s.dnd != nil && s.dnd.CanDrop != nil {
		t.Effect = s.dnd.CanDrop(t.Key, t.Position, s.keys)
	}
	return t
}

// dragHost is what the drag controller needs of the list or tree it works in.
type dragHost interface {
	// rowNav is the navigation state of the rows.
	navigator() *rowNav
	// bindingOf is the binding, for its timers (they run on the clock of the binding, so a test's fake clock
	// moves them).
	bindingOf() *widgets.WidgetsBinding
	// box is the render box of the view, to turn the position of the pointer into one of the content.
	box() rendering.Box
	// scrollOffset is how far the content is scrolled, and viewHeight the height of the view.
	scrollOffset() float64
	viewHeight() float64
	// scrollTo scrolls the content to a position.
	scrollTo(offset float64)
	// expandable says whether a row can be expanded and whether it is; expand expands it.
	expandable(key string) (can, expanded bool)
	expand(key string)
	// refresh rebuilds the widget.
	refresh()
	// takeFocus gives the view the keyboard focus.
	takeFocus()
}

// dragController turns the pointer events of a list or tree into a drag: it recognizes the start, finds the
// target under the pointer, scrolls the view near its edges, expands a folder the drag rests on and drops.
//
// It is built on the pointer events of one Listener around the rows, not on widgets.Draggable and DragTarget,
// although Glutter 1.7 has what the first version of this lacked (DraggableOpts.Slop, CancelOnEscape and
// DragTargetOpts.RepeatMove): the rows of a list come and go as it scrolls, so one controller in the state of the
// list holds the drag, finds the target from the geometry of the rows (dragSession, which knows nothing of widgets
// and has tests of its own) and drives the scrolling and the expanding with the timers of the binding. Moving it
// to Draggable would give the same behavior with the state spread over the rows, so it stays.
type dragController struct {
	host dragHost
	dnd  func() *DragAndDrop
	// label gives the label of a row for the default drag label.
	label func(key string) string

	session dragSession
	pressed bool
	pointer int
	key     string
	start   ui.Offset
	// local is the position of the pointer in the view, as of the last event.
	local ui.Offset
	// suppressTap is set once a drag started: the tap that ends the press is not a click.
	suppressTap bool

	expandTimer *gestures.Timer
	expandKey   string
	scrollTimer *gestures.Timer
}

func (c *dragController) enabled() bool { return c.dnd() != nil }

// active says that a drag is going on.
func (c *dragController) active() bool { return c.session.active }

// consumeTap says whether the tap that follows a release belongs to a drag; it clears the mark.
func (c *dragController) consumeTap() bool {
	was := c.suppressTap
	c.suppressTap = false
	return was
}

// press notes the press of a button on a row: it may become a drag.
func (c *dragController) press(e *gestures.PointerDownEvent, key string) {
	c.suppressTap = false
	if !c.enabled() || e.Buttons()&gestures.PrimaryButton == 0 || c.pressed {
		return
	}
	c.pressed, c.pointer, c.key, c.start = true, e.Pointer(), key, e.Position()
	c.session = dragSession{dnd: c.dnd()}
}

// move follows the pointer that is down.
func (c *dragController) move(e *gestures.PointerMoveEvent) {
	if !c.pressed || e.Pointer() != c.pointer || c.session.cancelled {
		return
	}
	pos := e.Position()
	if !c.session.active {
		if math.Hypot(pos.Dx-c.start.Dx, pos.Dy-c.start.Dy) < dragSlop {
			return
		}
		nav := c.host.navigator()
		keys := dragKeys(nav, c.key)
		if d := c.dnd(); d != nil && d.CanDrag != nil && !d.CanDrag(keys) {
			c.pressed = false
			return
		}
		c.session.keys = keys
		c.session.label = c.dragLabel(keys)
		c.session.active = true
		c.suppressTap = true
		c.host.takeFocus()
	}
	c.update(pos)
}

func (c *dragController) dragLabel(keys []string) string {
	if d := c.dnd(); d != nil && d.DragLabel != nil {
		return d.DragLabel(keys)
	}
	if len(keys) == 1 {
		if c.label != nil {
			return c.label(keys[0])
		}
		return keys[0]
	}
	return fmt.Sprintf("%d items", len(keys))
}

// update finds the target under a position of the pointer (in window coordinates).
func (c *dragController) update(global ui.Offset) {
	if b := c.host.box(); b != nil {
		c.local = b.GlobalToLocal(global, nil)
	}
	c.retarget()
	c.scrollNearEdge()
}

// retarget finds the target under the pointer, for the scroll position of now.
func (c *dragController) retarget() {
	nav := c.host.navigator()
	t := c.session.targetAt(nav, c.local.Dy+c.host.scrollOffset())
	c.session.target = t
	c.autoExpand()
	c.host.refresh()
}

// autoExpand starts the timer that expands a folder the drag rests on, and stops it when the pointer leaves.
func (c *dragController) autoExpand() {
	t := c.session.target
	can, expanded := false, false
	if t.Key != "" {
		can, expanded = c.host.expandable(t.Key)
	}
	want := t.Key != "" && can && !expanded && t.OK() && t.Position == DropInside
	if !want {
		c.stopExpand()
		return
	}
	if c.expandKey == t.Key && c.expandTimer != nil {
		return
	}
	c.stopExpand()
	c.expandKey = t.Key
	delay := DefaultAutoExpandDelay
	if d := c.dnd(); d != nil {
		delay = d.AutoExpandDelay.OrElse(DefaultAutoExpandDelay)
	}
	key := t.Key
	c.expandTimer = c.host.bindingOf().Gestures().NewTimer(delay, func() {
		c.expandTimer, c.expandKey = nil, ""
		if !c.session.active {
			return
		}
		c.host.expand(key)
		c.retarget()
	})
}

func (c *dragController) stopExpand() {
	if c.expandTimer != nil {
		c.expandTimer.Cancel()
		c.expandTimer = nil
	}
	c.expandKey = ""
}

// edgeStep is how far the view scrolls in one tick for a pointer at a distance from the top of the view
// (`animateDragAndDropScrollTop`): nothing in the middle, up to 14 px towards the edge the pointer is near.
func edgeStep(y, height float64) float64 {
	upper := height - dragEdge
	switch {
	case y < dragEdge:
		return math.Max(-dragMaxStep, math.Floor(0.3*(y-dragEdge)))
	case y > upper:
		return math.Min(dragMaxStep, math.Floor(0.3*(y-upper)))
	}
	return 0
}

// scrollNearEdge starts the scrolling when the pointer is near the top or the bottom of the view.
func (c *dragController) scrollNearEdge() {
	if c.scrollTimer != nil || !c.session.active {
		return
	}
	if edgeStep(c.local.Dy, c.host.viewHeight()) == 0 {
		return
	}
	c.scrollTimer = c.host.bindingOf().Gestures().NewTimer(dragScrollInterval, c.scrollTick)
}

func (c *dragController) scrollTick() {
	c.scrollTimer = nil
	if !c.session.active {
		return
	}
	step := edgeStep(c.local.Dy, c.host.viewHeight())
	if step == 0 {
		return
	}
	c.host.scrollTo(c.host.scrollOffset() + step)
	c.retarget()
	c.scrollNearEdge()
}

// up ends the press: a drag over a target that accepts the drop drops.
func (c *dragController) up(e *gestures.PointerUpEvent) {
	if !c.pressed || e.Pointer() != c.pointer {
		return
	}
	s := c.session
	c.end()
	if !s.active || s.cancelled || !s.target.OK() {
		return
	}
	if d := c.dnd(); d != nil && d.OnDrop != nil {
		d.OnDrop(s.target.Key, s.target.Position, s.keys)
	}
}

// pointerCancel ends the press without dropping.
func (c *dragController) pointerCancel(e *gestures.PointerCancelEvent) {
	if c.pressed && e.Pointer() == c.pointer {
		c.end()
	}
}

// cancel stops the drag that is going on (Escape): the pointer may still be down; nothing is dropped.
func (c *dragController) cancel() {
	if !c.session.active {
		return
	}
	c.session.active, c.session.cancelled = false, true
	c.session.target = DropTarget{}
	c.stopTimers()
	c.host.refresh()
}

// end forgets the press and the drag.
func (c *dragController) end() {
	wasActive := c.session.active
	c.pressed = false
	c.session = dragSession{}
	c.stopTimers()
	if wasActive {
		c.host.refresh()
	}
}

func (c *dragController) stopTimers() {
	c.stopExpand()
	if c.scrollTimer != nil {
		c.scrollTimer.Cancel()
		c.scrollTimer = nil
	}
}

// dispose stops the timers.
func (c *dragController) dispose() { c.stopTimers() }

// rowDrop says how the row of a key is a target: the whole row (inside), or the line above or below it.
func (c *dragController) rowDrop(key string) (inside, before, after bool) {
	t := c.session.target
	if !c.session.active || !t.OK() || t.Key != key || key == "" {
		return false, false, false
	}
	switch t.Position {
	case DropBefore:
		return false, true, false
	case DropAfter:
		return false, false, true
	}
	return true, false, false
}

// rootDrop says that the view itself is the target (the empty part below the rows).
func (c *dragController) rootDrop() bool {
	t := c.session.target
	return c.session.active && t.OK() && t.Key == ""
}

// feedback is the drag image: a small box with the label, near the pointer; nil when no drag is going on.
func (c *dragController) feedback(bc widgets.BuildContext, colors Colors) widgets.Widget {
	if !c.session.active || c.session.label == "" {
		return nil
	}
	text := widgets.NewText(c.session.label, widgets.TextOpts{Style: TextStyleOf(bc, 12, orColor(colors.ListActiveSelectionForeground, colors.Foreground)), MaxLines: foundation.Some(1), Overflow: painting.TextOverflowEllipsis})
	box := widgets.NewContainer(text, widgets.ContainerOpts{
		Padding: painting.NewEdgeInsetsSymmetric(7, 1),
		Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color:        foundation.Some(colors.ListActiveSelectionBackground),
			Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.ListFocusOutline), Width: foundation.Some(1.0)}),
			BorderRadius: painting.NewBorderRadiusCircular(10),
		}),
	})
	return widgets.NewPositioned(
		widgets.NewIgnorePointer(widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(240.0)}), box)),
		widgets.PositionedOpts{Left: foundation.Some(c.local.Dx + 12), Top: foundation.Some(c.local.Dy + 12)})
}

// dropLine is a line across a row, at its top or bottom, in the color of the lines between rows.
func dropLine(color ui.Color, atTop bool) widgets.Widget {
	opts := widgets.PositionedOpts{Left: foundation.Some(0.0), Right: foundation.Some(0.0), Height: foundation.Some(dragBetweenHeight)}
	if atTop {
		opts.Top = foundation.Some(0.0)
	} else {
		opts.Bottom = foundation.Some(0.0)
	}
	return widgets.NewPositioned(widgets.NewColoredBox(color, widgets.NewSizedBox(nil)), opts)
}
