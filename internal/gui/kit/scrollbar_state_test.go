package kit

import "testing"

func TestScrollbarStateSlider(t *testing.T) {
	cases := []struct {
		name                             string
		arrow, thickness, opposite       float64
		visible, content, position       float64
		wantNeeded                       bool
		wantAvailable, wantSize, wantPos float64
	}{
		{"content fits", 0, 14, 0, 400, 400, 0, false, 400, 400, 0},
		{"content empty", 0, 14, 0, 400, 0, 0, false, 400, 400, 0},
		{"half visible, at the top", 0, 14, 0, 400, 800, 0, true, 400, 200, 0},
		{"half visible, at the bottom", 0, 14, 0, 400, 800, 400, true, 400, 200, 200},
		{"half visible, in the middle", 0, 14, 0, 400, 800, 200, true, 400, 200, 100},
		{"a long document gets the minimum slider", 0, 14, 0, 400, 100000, 0, true, 400, 20, 0},
		{"a long document, at the bottom", 0, 14, 0, 400, 100000, 99600, true, 400, 20, 380},
		{"the other scrollbar takes room", 0, 12, 14, 400, 800, 400, true, 386, 193, 193},
		{"arrows take room", 11, 14, 0, 400, 800, 0, true, 400, 189, 0},
		{"positions are rounded", 0, 14, 0, 400, 1000, 123.6, true, 400, 160, 49},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewScrollbarState(tc.arrow, tc.thickness, tc.opposite, tc.visible, tc.content, tc.position)
			if s.IsNeeded() != tc.wantNeeded || s.RectangleLargeSize() != tc.wantAvailable ||
				s.SliderSize() != tc.wantSize || s.SliderPosition() != tc.wantPos {
				t.Errorf("needed %v, length %v, slider %v at %v; want %v, %v, %v at %v",
					s.IsNeeded(), s.RectangleLargeSize(), s.SliderSize(), s.SliderPosition(),
					tc.wantNeeded, tc.wantAvailable, tc.wantSize, tc.wantPos)
			}
			if s.RectangleSmallSize() != tc.thickness {
				t.Errorf("thickness %v", s.RectangleSmallSize())
			}
		})
	}
}

func TestScrollbarStateDesiredPositions(t *testing.T) {
	// 400 visible of 1200: the slider is 133 long and travels 267 as the content moves 800.
	s := NewScrollbarState(0, 14, 0, 400, 1200, 0)
	if s.SliderSize() != 133 {
		t.Fatalf("slider size %v", s.SliderSize())
	}
	cases := []struct {
		name string
		got  float64
		want float64
	}{
		{"center of the slider at the very start", s.DesiredScrollPositionFromOffset(66.5), 0},
		{"click in the middle of the track", s.DesiredScrollPositionFromOffset(200), 400},
		{"click at the end of the track (the model clamps it)", s.DesiredScrollPositionFromOffset(400), 999},
		{"click before the start (the model clamps it)", s.DesiredScrollPositionFromOffset(0), -199},
		{"drag by 10 pixels", s.DesiredScrollPositionFromDelta(10), 30},
		{"drag by the whole track", s.DesiredScrollPositionFromDelta(267), 800},
		{"paged click below the slider", s.DesiredScrollPositionFromOffsetPaged(300), 400},
		{"paged click above the slider", NewScrollbarState(0, 14, 0, 400, 1200, 500).DesiredScrollPositionFromOffsetPaged(10), 100},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s: %v, want %v", tc.name, tc.got, tc.want)
		}
	}
	none := NewScrollbarState(0, 14, 0, 400, 400, 0)
	if none.DesiredScrollPositionFromOffset(50) != 0 || none.DesiredScrollPositionFromDelta(5) != 0 || none.DesiredScrollPositionFromOffsetPaged(50) != 0 {
		t.Error("without a need for a slider every request is the start")
	}
}

func TestScrollbarStatePagedWithArrows(t *testing.T) {
	s := NewScrollbarState(11, 14, 0, 400, 1200, 400)
	// slider at 400*(378-126)/800... the arrow is taken from the offset before comparing
	pos := s.SliderPosition()
	if got := s.DesiredScrollPositionFromOffsetPaged(11 + pos - 1); got != 0 {
		t.Errorf("just before the slider: %v", got)
	}
	if got := s.DesiredScrollPositionFromOffsetPaged(11 + pos); got != 800 {
		t.Errorf("at the slider: %v", got)
	}
}

func TestScrollbarStateSetters(t *testing.T) {
	s := NewScrollbarState(0, 14, 0, 400, 800, 0)
	if s.SetVisibleSize(400) || s.SetScrollSize(800) || s.SetScrollPosition(0) {
		t.Error("the same values are no change")
	}
	if !s.SetScrollPosition(400.4) || s.ScrollPosition() != 400 || s.SliderPosition() != 200 {
		t.Errorf("position: %v slider at %v", s.ScrollPosition(), s.SliderPosition())
	}
	if !s.SetScrollSize(1600) || s.SliderSize() != 100 {
		t.Errorf("content: slider %v", s.SliderSize())
	}
	if !s.SetVisibleSize(1600) || s.IsNeeded() != false || s.SliderPosition() != 0 {
		t.Errorf("viewport as big as the content: needed %v", s.IsNeeded())
	}
	s.SetScrollbarSize(10)
	s.SetOppositeScrollbarSize(14)
	if s.RectangleSmallSize() != 10 || s.RectangleLargeSize() != 1586 {
		t.Errorf("sizes %v %v", s.RectangleSmallSize(), s.RectangleLargeSize())
	}
	c := s.Clone()
	c.SetScrollPosition(5)
	if s.ScrollPosition() == 5 {
		t.Error("a clone is its own state")
	}
}

func TestScrollbarStateNoRatio(t *testing.T) {
	// The slider fills the track (a tiny viewport): there is nothing to compute from.
	s := NewScrollbarState(0, 14, 0, 20, 400, 100)
	if s.SliderSize() != 20 {
		t.Fatalf("slider %v", s.SliderSize())
	}
	if s.DesiredScrollPositionFromOffset(5) != 100 || s.DesiredScrollPositionFromDelta(5) != 100 {
		t.Error("a slider that cannot move leaves the position")
	}
}
