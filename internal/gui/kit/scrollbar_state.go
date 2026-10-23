package kit

import "math"

// minimumSliderSize is the least length of a slider, so that it can still be grabbed: a
// slider of a long document is made bigger than the proportion says.
const minimumSliderSize = 20

// ScrollbarState is the arithmetic of one scrollbar: how long the slider is, where it sits,
// and which scroll position a click or a drag asks for. It does not draw or listen; for a
// vertical scrollbar the sizes are heights, for a horizontal one widths. Values are rounded
// to whole pixels as VS Code does.
//
// Ported from VS Code: src/vs/base/browser/ui/scrollbar/scrollbarState.ts
type ScrollbarState struct {
	scrollbarSize         float64 // the thickness: the width of a vertical scrollbar
	oppositeScrollbarSize float64 // the length the other scrollbar takes out of this one
	arrowSize             float64
	visibleSize           float64 // the viewport
	scrollSize            float64 // the content
	scrollPosition        float64

	availableSize  float64
	needed         bool
	sliderSize     float64
	sliderRatio    float64
	sliderPosition float64
}

// NewScrollbarState makes the state of a scrollbar. arrowSize is the length of each arrow
// button (0 without arrows), scrollbarSize the thickness, oppositeScrollbarSize the length of
// the scrollbar on the other axis that overlaps this one, and the last three the viewport, the
// content and the position along this axis.
func NewScrollbarState(arrowSize, scrollbarSize, oppositeScrollbarSize, visibleSize, scrollSize, scrollPosition float64) *ScrollbarState {
	s := &ScrollbarState{
		scrollbarSize: round(scrollbarSize), oppositeScrollbarSize: round(oppositeScrollbarSize), arrowSize: round(arrowSize),
		visibleSize: visibleSize, scrollSize: scrollSize, scrollPosition: scrollPosition,
	}
	s.refresh()
	return s
}

// round is JavaScript's Math.round: halves go up.
func round(v float64) float64 { return math.Floor(v + 0.5) }

// Clone copies the state, to remember where a drag started.
func (s *ScrollbarState) Clone() *ScrollbarState {
	return NewScrollbarState(s.arrowSize, s.scrollbarSize, s.oppositeScrollbarSize, s.visibleSize, s.scrollSize, s.scrollPosition)
}

// SetVisibleSize sets the size of the viewport; it reports a change.
func (s *ScrollbarState) SetVisibleSize(v float64) bool {
	v = round(v)
	if s.visibleSize == v {
		return false
	}
	s.visibleSize = v
	s.refresh()
	return true
}

// SetScrollSize sets the size of the content; it reports a change.
func (s *ScrollbarState) SetScrollSize(v float64) bool {
	v = round(v)
	if s.scrollSize == v {
		return false
	}
	s.scrollSize = v
	s.refresh()
	return true
}

// SetScrollPosition sets the scroll position; it reports a change.
func (s *ScrollbarState) SetScrollPosition(v float64) bool {
	v = round(v)
	if s.scrollPosition == v {
		return false
	}
	s.scrollPosition = v
	s.refresh()
	return true
}

// SetScrollbarSize sets the thickness of the scrollbar.
func (s *ScrollbarState) SetScrollbarSize(v float64) { s.scrollbarSize = round(v) }

// SetOppositeScrollbarSize sets the length the other scrollbar takes out of this one.
func (s *ScrollbarState) SetOppositeScrollbarSize(v float64) {
	s.oppositeScrollbarSize = round(v)
	s.refresh()
}

func (s *ScrollbarState) refresh() {
	available := math.Max(0, s.visibleSize-s.oppositeScrollbarSize)
	representable := math.Max(0, available-2*s.arrowSize)
	s.availableSize = round(available)
	s.needed = s.scrollSize > 0 && s.scrollSize > s.visibleSize
	if !s.needed {
		s.sliderSize, s.sliderRatio, s.sliderPosition = round(representable), 0, 0
		return
	}
	// The slider moves from 0 to representable-sliderSize as the position moves from 0 to
	// scrollSize-visibleSize.
	size := round(math.Max(minimumSliderSize, math.Floor(s.visibleSize*representable/s.scrollSize)))
	s.sliderSize = size
	s.sliderRatio = (representable - size) / (s.scrollSize - s.visibleSize)
	s.sliderPosition = round(s.scrollPosition * s.sliderRatio)
}

// ArrowSize is the length of an arrow button.
func (s *ScrollbarState) ArrowSize() float64 { return s.arrowSize }

// ScrollPosition is the position the state was given.
func (s *ScrollbarState) ScrollPosition() float64 { return s.scrollPosition }

// RectangleLargeSize is the length of the scrollbar: the viewport less the other scrollbar.
func (s *ScrollbarState) RectangleLargeSize() float64 { return s.availableSize }

// RectangleSmallSize is the thickness of the scrollbar.
func (s *ScrollbarState) RectangleSmallSize() float64 { return s.scrollbarSize }

// IsNeeded tells whether the content is longer than the viewport.
func (s *ScrollbarState) IsNeeded() bool { return s.needed }

// SliderSize is the length of the slider.
func (s *ScrollbarState) SliderSize() float64 { return s.sliderSize }

// SliderPosition is the distance of the slider from the start of the track, without the arrow.
func (s *ScrollbarState) SliderPosition() float64 { return s.sliderPosition }

// DesiredScrollPositionFromOffset is the scroll position that puts the center of the slider at
// offset, a distance from the start of the scrollbar; it is what a click on the track asks for.
func (s *ScrollbarState) DesiredScrollPositionFromOffset(offset float64) float64 {
	if !s.needed {
		return 0
	}
	if s.sliderRatio <= 0 {
		return s.scrollPosition
	}
	return round((offset - s.arrowSize - s.sliderSize/2) / s.sliderRatio)
}

// DesiredScrollPositionFromOffsetPaged is the scroll position one page before or after the
// current one, according to whether offset is before or after the slider.
func (s *ScrollbarState) DesiredScrollPositionFromOffsetPaged(offset float64) float64 {
	if !s.needed {
		return 0
	}
	if offset-s.arrowSize < s.sliderPosition {
		return s.scrollPosition - s.visibleSize
	}
	return s.scrollPosition + s.visibleSize
}

// DesiredScrollPositionFromDelta is the scroll position the slider gets to when it moves by
// delta pixels; it is what a drag asks for.
func (s *ScrollbarState) DesiredScrollPositionFromDelta(delta float64) float64 {
	if !s.needed {
		return 0
	}
	if s.sliderRatio <= 0 {
		return s.scrollPosition
	}
	return round((s.sliderPosition + delta) / s.sliderRatio)
}
