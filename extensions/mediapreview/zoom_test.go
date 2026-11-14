package mediapreview

import (
	"math"
	"testing"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

func TestZoomSteps(t *testing.T) {
	tests := []struct {
		scale, in, out float64
	}{
		{0.05, 0.1, 0.1},
		{0.1, 0.2, 0.1},
		{0.25, 0.3, 0.2},
		{1, 1.5, 0.9},
		{1.2, 1.5, 1},
		{3, 5, 2},
		{20, 20, 15},
		{25, 20, 20},
	}
	for _, tt := range tests {
		if got := ZoomIn(tt.scale); got != tt.in {
			t.Errorf("ZoomIn(%v) = %v, want %v", tt.scale, got, tt.in)
		}
		if got := ZoomOut(tt.scale); got != tt.out {
			t.Errorf("ZoomOut(%v) = %v, want %v", tt.scale, got, tt.out)
		}
	}
}

func TestWheelZoom(t *testing.T) {
	tests := []struct {
		scale, dy, want float64
	}{
		{1, -1, 1.075},
		{1, 1, 0.925},
		{20, -1, 20},
		{0.1, 1, 0.1},
	}
	for _, tt := range tests {
		if got := WheelZoom(tt.scale, tt.dy); !near(got, tt.want) {
			t.Errorf("WheelZoom(%v, %v) = %v, want %v", tt.scale, tt.dy, got, tt.want)
		}
	}
}

func TestFitScale(t *testing.T) {
	tests := []struct {
		name                     string
		imgW, imgH, viewW, viewH float64
		want                     float64
	}{
		{"smaller than the view: its own size", 100, 50, 820, 620, 1},
		{"wider than the view", 1600, 400, 820, 620, 0.5},
		{"taller than the view", 400, 1200, 820, 620, 0.5},
		{"no size", 0, 0, 820, 620, 1},
		{"a view smaller than its margin", 100, 100, 10, 10, MinScale},
	}
	for _, tt := range tests {
		if got := FitScale(tt.imgW, tt.imgH, tt.viewW, tt.viewH); !near(got, tt.want) {
			t.Errorf("%s: %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestZoomLabel(t *testing.T) {
	tests := []struct {
		fit   bool
		scale float64
		want  string
	}{
		{true, 0.3, "Whole Image"},
		{false, 1, "100%"},
		{false, 1.5, "150%"},
		{false, 0.075, "8%"},
		{false, 20, "2000%"},
	}
	for _, tt := range tests {
		if got := ZoomLabel(tt.fit, tt.scale); got != tt.want {
			t.Errorf("ZoomLabel(%v, %v) = %q, want %q", tt.fit, tt.scale, got, tt.want)
		}
	}
}

func TestPlace(t *testing.T) {
	tests := []struct {
		name                     string
		scale, offX, offY        float64
		x, y, clampedX, clampedY float64
	}{
		{"smaller: centered, the offsets dropped", 1, 30, 40, 300, 250, 0, 0},
		{"wider than the view: scrolled; not taller: centered", 5, 100, 50, -100, 50, 100, 0},
		{"the offsets held to the image", 5, 1000, -5, -200, 50, 200, 0},
		{"larger on both axes", 8, 100, 50, -100, -50, 100, 50},
	}
	for _, tt := range tests {
		// a 200 x 100 image in an 800 x 600 view
		x, y, cx, cy := Place(200, 100, tt.scale, 800, 600, tt.offX, tt.offY)
		if !near(x, tt.x) || !near(y, tt.y) || !near(cx, tt.clampedX) || !near(cy, tt.clampedY) {
			t.Errorf("%s: (%v, %v) offsets (%v, %v)", tt.name, x, y, cx, cy)
		}
	}
}

func TestAnchorKeepsThePointUnderThePointer(t *testing.T) {
	tests := []struct {
		name                 string
		from, to, offX, offY float64
		px, py               float64
	}{
		{"from centered to zoomed", 1, 5, 0, 0, 400, 260},
		{"zoomed further", 5, 10, 120, 60, 400, 300},
		{"zoomed out but still larger", 10, 7, 900, 300, 100, 500},
	}
	for _, tt := range tests {
		// a 200 x 100 image in an 800 x 600 view
		x, y, _, _ := Place(200, 100, tt.from, 800, 600, tt.offX, tt.offY)
		u, v := (tt.px-x)/tt.from, (tt.py-y)/tt.from
		nx, ny := Anchor(200, 100, tt.from, tt.to, 800, 600, tt.offX, tt.offY, tt.px, tt.py)
		x2, y2, _, _ := Place(200, 100, tt.to, 800, 600, nx, ny)
		gu, gv := (tt.px-x2)/tt.to, (tt.py-y2)/tt.to
		// on an axis where the image fits the view it is centered, so only the axes that are larger keep the point
		if 200*tt.to > 800 && !near(gu, u) {
			t.Errorf("%s: x of the image under the pointer %v, want %v", tt.name, gu, u)
		}
		if 100*tt.to > 600 && !near(gv, v) {
			t.Errorf("%s: y of the image under the pointer %v, want %v", tt.name, gv, v)
		}
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		size int64
		want string
	}{
		{0, "0B"},
		{1023, "1023B"},
		{1024, "1.00KB"},
		{251187, "245.30KB"},
		{3 << 20, "3.00MB"},
		{5 << 30, "5.00GB"},
		{2 << 40, "2.00TB"},
	}
	for _, tt := range tests {
		if got := formatSize(tt.size); got != tt.want {
			t.Errorf("formatSize(%d) = %q, want %q", tt.size, got, tt.want)
		}
	}
}
