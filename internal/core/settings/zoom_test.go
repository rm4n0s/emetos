package settings

import (
	"math"
	"testing"
)

func TestZoomFactor(t *testing.T) {
	// 1.2 to the power of the level, for every level of the range
	for level := MinZoomLevel; level <= MaxZoomLevel; level++ {
		if got, want := ZoomFactor(level), math.Pow(1.2, level); math.Abs(got-want) > 1e-12 {
			t.Errorf("ZoomFactor(%v) = %v, want %v", level, got, want)
		}
	}
	cases := []struct {
		level, want float64
	}{
		{0, 1}, {1, 1.2}, {2, 1.44}, {-1, 1 / 1.2}, {0.5, math.Sqrt(1.2)},
		{100, math.Pow(1.2, 8)}, {-100, math.Pow(1.2, -8)}, {math.NaN(), 1}, {math.Inf(1), math.Pow(1.2, 8)},
	}
	for _, tc := range cases {
		if got := ZoomFactor(tc.level); math.Abs(got-tc.want) > 1e-12 {
			t.Errorf("ZoomFactor(%v) = %v, want %v", tc.level, got, tc.want)
		}
	}
}

func TestClampZoomLevel(t *testing.T) {
	cases := []struct{ in, want float64 }{{0, 0}, {8, 8}, {9, 8}, {-8, -8}, {-9, -8}, {3.5, 3.5}, {math.NaN(), 0}, {math.Inf(-1), -8}}
	for _, tc := range cases {
		if got := ClampZoomLevel(tc.in); got != tc.want {
			t.Errorf("ClampZoomLevel(%v) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestZoomStep(t *testing.T) {
	cases := []struct {
		name   string
		level  float64
		in     bool
		want   float64
		wantOK bool
	}{
		{"in from 0", 0, true, 1, true},
		{"out from 0", 0, false, -1, true},
		{"in to the top", 7, true, 8, true},
		{"in at the top", 8, true, 8, false},
		{"out to the bottom", -7, false, -8, true},
		{"out at the bottom", -8, false, -8, false},
		{"a level that is not whole is rounded after the step", 1.5, true, 3, true},
		{"and 1.4 goes to 2", 1.4, true, 2, true},
		{"out from 1.5", 1.5, false, 1, true},
		{"in from just below the top rounds above it", 7.6, true, 7.6, false},
		{"not a number", math.NaN(), true, math.NaN(), false},
	}
	for _, tc := range cases {
		got, ok := ZoomStep(tc.level, tc.in)
		if ok != tc.wantOK || (!math.IsNaN(tc.want) && got != tc.want) {
			t.Errorf("%s: ZoomStep(%v, %v) = %v, %v; want %v, %v", tc.name, tc.level, tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestTheZoomAndZenSettings(t *testing.T) {
	r := DefaultRegistry()
	cases := []struct {
		key  string
		want any
	}{
		{"window.zoomLevel", 0.0}, {"window.zoomPerWindow", true},
		{"zenMode.fullScreen", true}, {"zenMode.centerLayout", true}, {"zenMode.showTabs", "multiple"}, {"zenMode.hideStatusBar", true},
		{"zenMode.hideActivityBar", true}, {"zenMode.hideLineNumbers", true}, {"zenMode.restore", true}, {"zenMode.silentNotifications", true},
		{"workbench.editor.centeredLayoutAutoResize", true}, {"workbench.editor.centeredLayoutFixedWidth", false},
	}
	for _, tc := range cases {
		s, ok := r.Lookup(tc.key)
		if !ok {
			t.Errorf("%s is not registered", tc.key)
			continue
		}
		got := s.Default
		if n, isInt := got.(int); isInt {
			got = float64(n)
		}
		if got != tc.want {
			t.Errorf("%s defaults to %v, want %v", tc.key, s.Default, tc.want)
		}
	}
	s, _ := r.Lookup("window.zoomLevel")
	if s.Minimum == nil || *s.Minimum != -8 || s.Maximum == nil || *s.Maximum != 8 || s.Scope != ScopeApplication {
		t.Errorf("window.zoomLevel: %+v", s)
	}
}
