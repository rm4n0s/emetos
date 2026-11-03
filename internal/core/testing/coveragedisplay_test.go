package testing_test

import (
	"testing"

	ct "github.com/rm4n0s/emetos/internal/core/testing"
)

// The color of a bar: the highest threshold the percentage reaches; a threshold of 0 is never reached, so a low bar is red (VS Code's
// getCoverageColor).
func TestCoverageColor(t *testing.T) {
	d := ct.DefaultBarThresholds
	cases := []struct {
		name string
		pct  float64
		th   ct.BarThresholds
		want string
	}{
		{"nothing is red", 0, d, ct.CoverageRed},
		{"below yellow is red", 0.59, d, ct.CoverageRed},
		{"exactly yellow", 0.60, d, ct.CoverageYellow},
		{"between yellow and green", 0.89, d, ct.CoverageYellow},
		{"exactly green", 0.90, d, ct.CoverageGreen},
		{"everything is green", 1, d, ct.CoverageGreen},
		{"a custom yellow", 0.30, ct.BarThresholds{Red: 0, Yellow: 25, Green: 80}, ct.CoverageYellow},
		{"a custom green", 0.80, ct.BarThresholds{Red: 0, Yellow: 25, Green: 80}, ct.CoverageGreen},
		{"red from 10 below that is red too", 0.05, ct.BarThresholds{Red: 10, Yellow: 50, Green: 90}, ct.CoverageRed},
		{"red from 10 reached", 0.10, ct.BarThresholds{Red: 10, Yellow: 50, Green: 90}, ct.CoverageRed},
		{"the nearest threshold below wins", 0.55, ct.BarThresholds{Red: 10, Yellow: 50, Green: 40}, ct.CoverageYellow},
		{"no thresholds at all", 1, ct.BarThresholds{}, ct.CoverageRed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ct.CoverageColor(c.pct, c.th); got != c.want {
				t.Errorf("CoverageColor(%v, %+v) = %s, want %s", c.pct, c.th, got, c.want)
			}
		})
	}
}

// testing.coverageBarThresholds is read from the setting's object: members that are missing or not numbers keep their defaults, numbers are
// held between 0 and 100.
func TestThresholdsOf(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want ct.BarThresholds
	}{
		{"nothing", nil, ct.DefaultBarThresholds},
		{"not an object", "red", ct.DefaultBarThresholds},
		{"the default object", map[string]any{"red": 0.0, "yellow": 60.0, "green": 90.0}, ct.DefaultBarThresholds},
		{"one member", map[string]any{"green": 75.0}, ct.BarThresholds{Red: 0, Yellow: 60, Green: 75}},
		{"a member that is not a number", map[string]any{"yellow": "high"}, ct.DefaultBarThresholds},
		{"held to 0 to 100", map[string]any{"red": -5.0, "green": 140.0}, ct.BarThresholds{Red: 0, Yellow: 60, Green: 100}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ct.ThresholdsOf(c.in); got != c.want {
				t.Errorf("got %+v, want %+v", got, c.want)
			}
		})
	}
}

// The number a bar shows: testing.displayedCoveragePercent chooses between the total (Clover's), the statements and the lowest of the
// three; a count with nothing in it is full.
func TestDisplayedStat(t *testing.T) {
	stmt := ct.Count{Covered: 8, Total: 10}
	branch := &ct.Count{Covered: 1, Total: 4}
	decl := &ct.Count{Covered: 3, Total: 3}
	cases := []struct {
		name   string
		method string
		branch *ct.Count
		decl   *ct.Count
		want   float64
	}{
		{"total of the three", ct.PercentTotal, branch, decl, 12.0 / 17.0},
		{"total with statements only", ct.PercentTotal, nil, nil, 0.8},
		{"statements", ct.PercentStatement, branch, decl, 0.8},
		{"minimum picks the branches", ct.PercentMinimum, branch, decl, 0.25},
		{"minimum without branches picks the statements", ct.PercentMinimum, nil, decl, 0.8},
		{"minimum with nothing else", ct.PercentMinimum, nil, nil, 0.8},
		{"an unknown value is the total", "other", branch, decl, 12.0 / 17.0},
		{"nothing to cover is full", ct.PercentStatement, nil, nil, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := stmt
			if c.name == "nothing to cover is full" {
				s = ct.Count{}
			}
			got := ct.DisplayedStat(s, c.branch, c.decl, c.method)
			if d := got - c.want; d > 1e-9 || d < -1e-9 {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

// A percentage never shows 100% for a share that only rounds up to it.
func TestDisplayPercent(t *testing.T) {
	cases := []struct {
		v         float64
		precision int
		want      string
	}{
		{1, 2, "100.00%"},
		{0, 2, "0.00%"},
		{0.8333333, 2, "83.33%"},
		{0.8333333, 0, "83%"},
		{0.99999, 2, "99.99%"},
		{0.999, 0, "99%"},
		{0.9999999999, 2, "100.00%"},
		{0.5, 0, "50%"},
	}
	for _, c := range cases {
		if got := ct.DisplayPercent(c.v, c.precision); got != c.want {
			t.Errorf("DisplayPercent(%v, %d) = %q, want %q", c.v, c.precision, got, c.want)
		}
	}
}

// A bar is full only at 100%: the fill is floored to a pixel of 16.
func TestBarFill(t *testing.T) {
	for _, c := range []struct {
		pct  float64
		want int
	}{{0, 0}, {0.03, 0}, {0.0625, 1}, {0.5, 8}, {0.999, 15}, {1, 16}, {1.5, 16}, {-1, 0}} {
		if got := ct.BarFill(c.pct); got != c.want {
			t.Errorf("BarFill(%v) = %d, want %d", c.pct, got, c.want)
		}
	}
}
