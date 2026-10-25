package mergeeditor

import (
	"math"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/diff"
)

func lr(a, b int) diff.LineRange { return diff.LineRange{StartLine: a, EndLine: b} }

func TestLineMapProject(t *testing.T) {
	// the lines 3-4 of the input are the lines 3-6 of the output, and the input has an empty range at 8
	m := LineMap{{In: lr(3, 5), Out: lr(3, 7)}, {In: lr(8, 8), Out: lr(10, 12)}}
	for _, tc := range []struct {
		name string
		line int
		want LinePair
	}{
		{"before the first pair", 2, LinePair{In: lr(2, 3), Out: lr(2, 3)}},
		{"in a pair", 4, LinePair{In: lr(3, 5), Out: lr(3, 7)}},
		{"the first line of a pair", 3, LinePair{In: lr(3, 5), Out: lr(3, 7)}},
		{"after a pair, moved by it", 5, LinePair{In: lr(5, 6), Out: lr(7, 8)}},
		{"further after it", 7, LinePair{In: lr(7, 8), Out: lr(9, 10)}},
		{"at an empty range", 8, LinePair{In: lr(8, 9), Out: lr(12, 13)}},
		{"after an empty range", 9, LinePair{In: lr(9, 10), Out: lr(13, 14)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := m.Project(tc.line); got != tc.want {
				t.Errorf("Project(%d) = %v, want %v", tc.line, got, tc.want)
			}
		})
	}
	if got := LineMap(nil).Project(4); got != (LinePair{In: lr(4, 5), Out: lr(4, 5)}) {
		t.Errorf("an empty map: %v", got)
	}
	rev := m.Reverse()
	if rev[0] != (LinePair{In: lr(3, 7), Out: lr(3, 5)}) || !rev.Sorted() {
		t.Errorf("reverse %v", rev)
	}
	if got := rev.Project(9); got != (LinePair{In: lr(9, 10), Out: lr(7, 8)}) {
		t.Errorf("projecting through the reverse: %v", got)
	}
}

// pixels are the pixel at which a line begins in a text whose lines are 10 high, with some lines taller.
func pixels(extra map[int]float64) func(int) float64 {
	return func(line int) float64 {
		y := float64(line-1) * 10
		for l, e := range extra {
			if l < line {
				y += e
			}
		}
		return y
	}
}

func TestScrollFor(t *testing.T) {
	// the lines 3-4 of the source (20 px) are the lines 3-8 of the target (60 px)
	m := LineMap{{In: lr(3, 5), Out: lr(3, 9)}}
	src, dst := pixels(nil), pixels(nil)
	for _, tc := range []struct {
		name      string
		top       int
		scrollTop float64
		want      float64
	}{
		{"above the pair: the same place", 2, 10, 10},
		{"at the start of the pair", 3, 20, 20},
		{"half way through the pair", 3, 30, 50},
		{"in the second line of the pair", 4, 35, 20 + 60*(15.0/20)},
		{"after the pair: moved by what it added", 6, 50, 50 + 40},
		{"a scroll past the pair's end stays at its end", 3, 90, 80},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := m.ScrollFor(tc.top, tc.scrollTop, src, dst)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("ScrollFor = %v, want %v", got, tc.want)
			}
		})
	}
	// an empty range of the scrolled text holds no line: the line at its position is after the range, and lines up
	// with the line after the range in the target
	empty := LineMap{{In: lr(3, 3), Out: lr(3, 6)}}
	if got := empty.ScrollFor(3, 20, src, dst); got != 50 {
		t.Errorf("an empty range: %v", got)
	}
	// a range with no height (a zero-height text) does not divide by zero
	flat := func(int) float64 { return 0 }
	if got := m.ScrollFor(3, 0, flat, dst); got != 20 {
		t.Errorf("a flat text: %v", got)
	}
	// zones make lines taller: the fraction counts pixels, not lines
	taller := pixels(map[int]float64{3: 20}) // 20 more pixels after line 3
	if got := m.ScrollFor(3, 40, taller, dst); math.Abs(got-(20+60*(20.0/40))) > 1e-9 {
		t.Errorf("with a zone: %v", got)
	}
}

func TestInputMap(t *testing.T) {
	regions := []diff.Region{
		{Range: diff.ModifiedBaseRange{Input1: lr(2, 4), Input2: lr(2, 3)}, Result: lr(2, 2)},
		{Range: diff.ModifiedBaseRange{Input1: lr(6, 7), Input2: lr(5, 7)}, Result: lr(4, 6)},
	}
	m1, m2 := InputMap(regions, diff.Input1), InputMap(regions, diff.Input2)
	if m1[0] != (LinePair{In: lr(2, 4), Out: lr(2, 2)}) || m2[1] != (LinePair{In: lr(5, 7), Out: lr(4, 6)}) || !m1.Sorted() || !m2.Sorted() {
		t.Errorf("maps %v %v", m1, m2)
	}
}

// TestScrollFollowsInBothDirections checks that scrolling a text and then the other one by the mapped position does
// not walk away: the round trip lands within a line of where it started for the lines outside the pairs.
func TestScrollFollowsInBothDirections(t *testing.T) {
	m := LineMap{{In: lr(3, 5), Out: lr(3, 9)}, {In: lr(12, 12), Out: lr(16, 18)}}
	src, dst := pixels(nil), pixels(nil)
	for line := 1; line <= 20; line++ {
		if line >= 3 && line < 5 {
			continue
		}
		top := src(line)
		there := m.ScrollFor(line, top, src, dst)
		// the line the other text shows at that position
		other := int(there/10) + 1
		back := m.Reverse().ScrollFor(other, there, dst, src)
		if math.Abs(back-top) > 10 {
			t.Errorf("line %d (%v px) goes to %v px and comes back to %v px", line, top, there, back)
		}
	}
}
