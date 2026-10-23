package editor

import (
	"math"
	"time"

	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

const (
	// wheelNotch is how many logical pixels Glutter reports for one notch of a mouse wheel.
	wheelNotch = 53.0
	// wheelStep is how far one notch scrolls at a sensitivity of 1.
	//
	// Ported from VS Code: src/vs/base/browser/ui/scrollbar/scrollableElement.ts (SCROLL_WHEEL_SENSITIVITY)
	wheelStep = 50.0
	// smoothScrollDuration is how long an animated scroll takes.
	//
	// Ported from VS Code: src/vs/editor/common/viewLayout/viewLayout.ts (_smoothScrollDuration)
	smoothScrollDuration = 125 * time.Millisecond
	// maxAutoScrollStep is the longest time one frame of the scrolling of a drag counts for, so
	// that a stalled window does not jump.
	maxAutoScrollStep = 100 * time.Millisecond
)

// wheelDelta turns the scroll of a wheel into how far to scroll, in pixels, positive towards
// the end: the sensitivity of the setting multiplies it, Shift scrolls sideways, and Alt
// scrolls faster. A step is rounded away from zero, as VS Code rounds it.
//
// Ported from VS Code: src/vs/base/browser/ui/scrollbar/scrollableElement.ts (_onMouseWheel)
func (s *editorState) wheelDelta(delta ui.Offset, mods MouseModifiers) (dx, dy float64) {
	opts := s.Widget().vm.Options()
	dx = delta.Dx / wheelNotch * wheelStep * opts.MouseWheelScrollSensitivity
	dy = delta.Dy / wheelNotch * wheelStep * opts.MouseWheelScrollSensitivity
	if mods.Shift && dx == 0 {
		dx, dy = dy, 0
	}
	if mods.Alt {
		dx *= opts.FastScrollSensitivity
		dy *= opts.FastScrollSensitivity
	}
	return roundAway(dx), roundAway(dy)
}

// roundAway rounds to a whole number away from zero.
func roundAway(v float64) float64 {
	if v < 0 {
		return math.Floor(v)
	}
	return math.Ceil(v)
}

// currentTop is the vertical scroll position now.
func (s *editorState) currentTop() float64 {
	if s.controller.HasClients() {
		return s.controller.Offset()
	}
	return s.Widget().vm.ScrollTop()
}

// futureTop is where the vertical scroll position will be: the end of the animation that runs.
func (s *editorState) futureTop() float64 {
	if s.smoothTarget != nil {
		return *s.smoothTarget
	}
	return s.currentTop()
}

// clampTop moves a vertical position within what the content scrolls through.
func (s *editorState) clampTop(top float64) float64 {
	if !s.controller.HasClients() {
		return top
	}
	pos := s.controller.Position()
	return min(max(top, pos.MinScrollExtent()), pos.MaxScrollExtent())
}

// pointerSignal claims the scrolling of a mouse wheel when it would move the editor, so that
// the Scrollable around it does not scroll too (SPEC §9.3).
func (s *editorState) pointerSignal(e gestures.PointerSignalEvent) {
	scroll, ok := e.(*gestures.PointerScrollEvent)
	if !ok {
		return
	}
	dx, dy := s.wheelDelta(scroll.ScrollDelta(), s.modifiers())
	vm := s.Widget().vm
	moves := (dy != 0 && s.clampTop(s.futureTop()+dy) != s.futureTop()) ||
		(dx != 0 && min(max(vm.ScrollLeft()+dx, 0), vm.MaxScrollLeft()) != vm.ScrollLeft())
	if !moves {
		return
	}
	smooth := vm.Options().SmoothScrolling
	s.binding.Gestures().PointerSignalResolver.Register(e, func(gestures.PointerSignalEvent) { s.scrollBy(dx, dy, smooth) })
}

// panZoomUpdate scrolls with the fingers on a touchpad: the content follows them, so the
// pan is the opposite of the scroll.
func (s *editorState) panZoomUpdate(e *gestures.PointerPanZoomUpdateEvent) {
	d := e.PanDelta()
	opts := s.Widget().vm.Options()
	k := opts.MouseWheelScrollSensitivity
	if s.modifiers().Alt {
		k *= opts.FastScrollSensitivity
	}
	s.scrollBy(-d.Dx*k, -d.Dy*k, false)
}

// scrollBy scrolls by pixels, positive towards the end. Vertically it can animate, and a scroll
// during an animation goes on from where the animation ends.
func (s *editorState) scrollBy(dx, dy float64, smooth bool) {
	vm := s.Widget().vm
	if dy != 0 && s.controller.HasClients() {
		target := s.clampTop(s.futureTop() + dy)
		if smooth {
			s.animateTo(target)
		} else {
			s.smoothTarget = nil
			s.controller.JumpTo(target)
		}
	}
	if dx != 0 {
		vm.SetScrollLeft(vm.ScrollLeft() + dx)
	}
}

// smoothScroll is an animated vertical scroll: from where it was when it began to a target,
// easing out. The position moves with JumpTo on every frame, not with the scroll position's own
// animation, because Scrollable ignores the pointer (and so the clicks and the wheel that reach
// the editor) while that one runs.
type smoothScroll struct {
	from, to float64
	// start is the time of the first frame; started is false until it came.
	start   time.Duration
	started bool
	pending bool
}

// animateTo scrolls to a vertical position over a short time, from where the editor is.
func (s *editorState) animateTo(target float64) {
	a := &s.smooth
	a.from, a.to, a.started = s.currentTop(), target, false
	s.smoothTarget = &a.to
	if a.pending {
		return
	}
	a.pending = true
	s.scheduleSmoothFrame()
}

// scheduleSmoothFrame asks for the next frame of the animation.
func (s *editorState) scheduleSmoothFrame() {
	scheduler := s.binding.Scheduler()
	scheduler.ScheduleFrame()
	scheduler.AddPostFrameCallback(func(stamp time.Duration) { s.smoothFrame(stamp) })
}

// smoothFrame moves the scroll position along the animation.
//
// Ported from VS Code: src/vs/base/common/scrollable.ts (SmoothScrollingOperation, easeOutCubic)
func (s *editorState) smoothFrame(stamp time.Duration) {
	a := &s.smooth
	if s.smoothTarget == nil || !s.Mounted() || !s.controller.HasClients() {
		a.pending, s.smoothTarget = false, nil
		return
	}
	if !a.started {
		a.start, a.started = stamp, true
	}
	progress := min(max(float64(stamp-a.start)/float64(smoothScrollDuration), 0), 1)
	left := 1 - progress
	eased := 1 - left*left*left
	s.controller.JumpTo(s.clampTop(a.from + (a.to-a.from)*eased))
	if progress >= 1 {
		a.pending, s.smoothTarget = false, nil
		return
	}
	s.scheduleSmoothFrame()
}

// autoScrollEdge is the edge of the editor the pointer is beyond while it drags a selection.
type autoScrollEdge int

const (
	edgeNone autoScrollEdge = iota
	edgeAbove
	edgeBelow
	edgeLeft
	edgeRight
)

// autoScroll is the scrolling that goes on while a selection is dragged beyond an edge of
// the editor: it runs every frame, as long as the button is down and the pointer is out.
type autoScroll struct {
	edge     autoScrollEdge
	distance float64
	// x is where the pointer is along the text, y along the rows, and line the line it is
	// over, which a sideways scroll keeps selecting on.
	local ui.Offset
	line  int
	// last is the time of the last frame, from which the next one counts.
	last    time.Duration
	started bool
	// pending is whether a frame callback is waiting.
	pending bool
}

// edgeOf says where the pointer is beyond the editor, given in the coordinates of the row of
// gutter and text, and how far.
func (s *editorState) edgeOf(local ui.Offset, gutterWidth float64) (autoScrollEdge, float64) {
	w, h := s.Widget().vm.ViewportSize()
	switch {
	case local.Dy < 0:
		return edgeAbove, -local.Dy
	case local.Dy > h:
		return edgeBelow, local.Dy - h
	case local.Dx < 0:
		return edgeLeft, -local.Dx
	case local.Dx > gutterWidth+w:
		return edgeRight, local.Dx - gutterWidth - w
	}
	return edgeNone, 0
}

// scrollSpeed is how many units a second a drag scrolls at, for a pointer that is distance
// units beyond the edge of a viewport of the given size in units.
//
// Ported from VS Code: src/vs/editor/browser/controller/dragScrolling.ts (_getScrollSpeed)
func scrollSpeed(distance, viewport float64) float64 {
	switch {
	case distance <= 1.5:
		return math.Max(30, viewport*(1+distance))
	case distance <= 3:
		return math.Max(60, viewport*(2+distance))
	}
	return math.Max(200, viewport*(7+distance))
}

// followEdge starts or updates the scrolling of a drag beyond an edge, or stops it for a
// pointer that is back inside.
func (s *editorState) followEdge(edge autoScrollEdge, distance float64, local ui.Offset, line int) {
	a := &s.auto
	if edge == edgeNone {
		s.stopAutoScroll()
		return
	}
	if a.edge != edgeNone && (a.edge == edgeAbove || a.edge == edgeBelow) != (edge == edgeAbove || edge == edgeBelow) {
		a.started = false // from vertical to sideways: start counting again
	}
	a.edge, a.distance, a.local, a.line = edge, distance, local, line
	s.scheduleAutoScroll()
}

// stopAutoScroll ends the scrolling of a drag.
func (s *editorState) stopAutoScroll() {
	s.auto.edge = edgeNone
	s.auto.started = false
}

// scheduleAutoScroll asks for the next frame of the scrolling of a drag.
func (s *editorState) scheduleAutoScroll() {
	a := &s.auto
	if a.pending {
		return
	}
	a.pending = true
	scheduler := s.binding.Scheduler()
	scheduler.ScheduleFrame()
	scheduler.AddPostFrameCallback(func(stamp time.Duration) {
		a.pending = false
		s.autoScrollFrame(stamp)
	})
}

// autoScrollFrame scrolls for the time since the last frame and moves the end of the selection
// to the edge it scrolled towards.
//
// Ported from VS Code: src/vs/editor/browser/controller/dragScrolling.ts (TopBottomDragScrollingOperation._execute, LeftRightDragScrollingOperation._execute)
func (s *editorState) autoScrollFrame(stamp time.Duration) {
	a := &s.auto
	if a.edge == edgeNone || !s.mouse.pressed || s.textRO == nil || !s.Mounted() {
		return
	}
	elapsed := time.Duration(0)
	if a.started {
		elapsed = min(max(stamp-a.last, 0), maxAutoScrollStep)
	}
	a.started, a.last = true, stamp

	vm := s.Widget().vm
	ev := MouseEvent{
		Mods: s.mouse.mods, OnLineNumbers: s.mouse.onNumbers, Clicks: s.mouse.last.Clicks,
		Middle: s.mouse.last.Middle, Drag: true, NoReveal: true,
	}
	opts := vm.Options()
	w, h := vm.ViewportSize()
	switch a.edge {
	case edgeAbove, edgeBelow:
		speed := scrollSpeed(a.distance/opts.LineHeight, h/opts.LineHeight)
		px := speed * elapsed.Seconds() * opts.LineHeight
		if a.edge == edgeAbove {
			px = -px
		}
		s.smoothTarget = nil
		s.controller.JumpTo(s.clampTop(s.currentTop() + px))
		ev.Position, ev.VisualColumn = s.edgePosition()
	default:
		cw := s.textRO.CharWidth()
		px := scrollSpeed(a.distance/cw, w/cw) * elapsed.Seconds() * cw * 0.5
		if a.edge == edgeLeft {
			px = -px
		}
		vm.SetScrollLeft(vm.ScrollLeft() + px)
		ev.Position = buffer.Position{Line: a.line}
		if a.edge == edgeRight {
			ev.Position = vm.lineEndOf(ev.Position)
		}
		ev.VisualColumn = ev.Position.Col
	}
	s.reportInputError(vm.DispatchMouse(ev))
	s.scheduleAutoScroll()
}

// edgePosition is the position the end of a selection goes to while a drag scrolls up or down:
// the one at the edge row under the pointer, or the start or the end of the edge line when the
// pointer is not over it or the editor cannot scroll further.
func (s *editorState) edgePosition() (buffer.Position, int) {
	a := &s.auto
	vm := s.Widget().vm
	proj := vm.Projection()
	_, h := vm.ViewportSize()
	top := s.currentTop()
	y, row := 0.0, proj.RowAt(top)
	last := proj.RowCount() - 1
	cannot := row <= 0
	if a.edge == edgeBelow {
		y = h - 0.1
		row = proj.RowAt(top + h - 0.1)
		cannot = row >= last
	}
	line, _, _ := proj.RowSpan(row)
	in := ui.Offset{Dx: a.local.Dx - s.gutterWidth, Dy: y}
	pos := s.textRO.PositionAt(in)
	if pos.Line != line || cannot {
		pos = buffer.Position{Line: line}
		if a.edge == edgeBelow {
			pos = vm.lineEndOf(pos)
		}
		return pos, pos.Col
	}
	return pos, s.textRO.VisualColumnAt(in)
}
