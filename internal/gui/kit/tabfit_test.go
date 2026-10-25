package kit

import (
	"math"
	"testing"
)

func TestFitTabWidths(t *testing.T) {
	const eps = 1e-6
	cases := []struct {
		name      string
		natural   []float64
		available float64
		want      []float64
		scrolls   bool
	}{
		{"no tabs", nil, 500, []float64{}, false},
		{"short labels are 120 wide", []float64{60, 80}, 1000, []float64{120, 120}, false},
		{"long labels are 240 wide", []float64{400, 300}, 1000, []float64{240, 240}, false},
		{"in between they keep the label width", []float64{150, 200}, 1000, []float64{150, 200}, false},
		{"they fit exactly", []float64{200, 200, 200}, 600, []float64{200, 200, 200}, false},
		{"the widest shrink first", []float64{240, 240, 130}, 560, []float64{215, 215, 130}, false},
		{"all shrink together to fit", []float64{240, 240, 240}, 600, []float64{200, 200, 200}, false},
		{"down to 120 and no further", []float64{240, 240, 240}, 360, []float64{120, 120, 120}, false},
		{"then the strip scrolls", []float64{240, 240, 240, 240}, 400, []float64{120, 120, 120, 120}, true},
		{"one tab alone in a narrow strip", []float64{240}, 100, []float64{120}, true},
		{"no room measured keeps the natural widths", []float64{240, 100}, 0, []float64{240, 120}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, scrolls := FitTabWidths(tc.natural, tc.available, WorkspaceTabMinWidth, WorkspaceTabMaxWidth)
			if scrolls != tc.scrolls || len(got) != len(tc.want) {
				t.Fatalf("got %v scrolls=%v, want %v scrolls=%v", got, scrolls, tc.want, tc.scrolls)
			}
			sum := 0.0
			for i := range got {
				sum += got[i]
				if math.Abs(got[i]-tc.want[i]) > 1e-3 {
					t.Errorf("tab %d is %.3f wide, want %.3f (all: %v)", i, got[i], tc.want[i], got)
				}
			}
			if !tc.scrolls && tc.available > 0 && sum > tc.available+eps {
				t.Errorf("the widths add up to %.3f, more than the %.3f available", sum, tc.available)
			}
		})
	}
}
