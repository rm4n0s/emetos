package editor

import (
	"math"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

func TestRevealPositionInCenter(t *testing.T) {
	tests := []struct {
		name string
		line int
		// want is the row, whose middle is in the middle of the viewport, or -1 for the top
		want int
	}{
		{"the first line stays at the top", 0, -1},
		{"a line in the middle", 100, 100},
		{"a line far down", 150, 150},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vm := snippetVM(t, strings.Repeat("line\n", 200))
			vm.SetViewportHeight(200)
			var got []float64
			vm.SetScroller(func(top float64) { got = append(got, top) })
			vm.RevealPositionInCenter(buffer.Position{Line: tc.line})
			if len(got) != 1 {
				t.Fatalf("scrolled %v", got)
			}
			want := 0.0
			if tc.want >= 0 {
				proj := vm.Projection()
				want = float64(proj.RowTop(tc.want)) + float64(proj.RowHeight(tc.want))/2 - 100
			}
			if math.Abs(got[0]-want) > 1 {
				t.Errorf("scrolled to %v, want %v", got[0], want)
			}
		})
	}
	t.Run("without a scroller or a viewport nothing happens", func(t *testing.T) {
		vm := snippetVM(t, "a\nb\n")
		vm.RevealPositionInCenter(buffer.Position{Line: 1})
		vm.SetScroller(func(float64) { t.Error("scrolled") })
		vm.RevealPositionInCenter(buffer.Position{Line: 1})
	})
}

func TestRangeHighlight(t *testing.T) {
	tests := []struct {
		name     string
		set      int
		wantLine int
	}{
		{"a line", 1, 1},
		{"before the first line", -4, 0},
		{"after the last line", 90, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			vm := snippetVM(t, "a\nb\nc")
			if _, ok := vm.RangeHighlight(); ok {
				t.Fatal("a highlight before it is set")
			}
			vm.SetRangeHighlight(tc.set)
			if line, ok := vm.RangeHighlight(); !ok || line != tc.wantLine {
				t.Errorf("highlight %d %v, want %d", line, ok, tc.wantLine)
			}
			vm.ClearRangeHighlight()
			if _, ok := vm.RangeHighlight(); ok {
				t.Error("the highlight stays after Clear")
			}
		})
	}
}

func TestGoToPositionAndRestoreView(t *testing.T) {
	vm := snippetVM(t, "alpha\nbeta\ngamma\n", sel(0, 1, 0, 3))
	vm.SetViewportHeight(100)
	var scrolled []float64
	vm.SetScroller(func(top float64) { scrolled = append(scrolled, top) })
	saved := vm.ViewState()

	vm.SetRangeHighlight(2)
	vm.GoToPosition(buffer.Position{Line: 1, Col: 99})
	if got := vm.Cursors().Selections(); len(got) != 1 || got[0] != sel(1, 4, 1, 4) {
		t.Errorf("selections %v: the position is put inside the text", got)
	}
	if _, ok := vm.RangeHighlight(); ok {
		t.Error("the highlight stays after GoToPosition")
	}
	if len(scrolled) == 0 {
		t.Error("the editor did not scroll")
	}

	scrolled = nil
	if err := vm.RestoreView(saved); err != nil {
		t.Fatal(err)
	}
	if got := vm.Cursors().Selections(); len(got) != 1 || got[0] != sel(0, 1, 0, 3) {
		t.Errorf("selections after RestoreView: %v", got)
	}
	if len(scrolled) != 1 || scrolled[0] != 0 {
		t.Errorf("RestoreView scrolled %v, want to the saved top", scrolled)
	}
}
