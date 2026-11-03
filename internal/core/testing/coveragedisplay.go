package testing

import (
	"fmt"
	"math"
)

// The way coverage is shown (SPEC §10.32 "Test Coverage"): the percentages, the colors of the bars and the number a bar shows. These
// are pure functions of the counts and of the settings testing.displayedCoveragePercent and testing.coverageBarThresholds.
//
// Ported from VS Code: src/vs/workbench/contrib/testing/browser/codeCoverageDisplayUtils.ts

// The values of testing.displayedCoveragePercent.
const (
	// PercentTotal is the combined statement, declaration and branch coverage (the default), PercentStatement the statement coverage
	// and PercentMinimum the lowest of the three.
	PercentTotal     = "totalCoverage"
	PercentStatement = "statement"
	PercentMinimum   = "minimum"
)

// BarThresholds are the lowest percentages (0 to 100) that have the colors of a coverage bar (testing.coverageBarThresholds).
type BarThresholds struct{ Red, Yellow, Green float64 }

// DefaultBarThresholds are VS Code's: red from 0, yellow from 60 and green from 90.
var DefaultBarThresholds = BarThresholds{Red: 0, Yellow: 60, Green: 90}

// ThresholdsOf reads the value of testing.coverageBarThresholds: an object with the numbers red, yellow and green; a missing or invalid
// member keeps its default, and a number is held between 0 and 100.
func ThresholdsOf(v any) BarThresholds {
	t := DefaultBarThresholds
	m, ok := v.(map[string]any)
	if !ok {
		return t
	}
	for key, slot := range map[string]*float64{"red": &t.Red, "yellow": &t.Yellow, "green": &t.Green} {
		if n, ok := m[key].(float64); ok && !math.IsNaN(n) {
			*slot = min(max(n, 0), 100)
		}
	}
	return t
}

// Coverage colors, the names CoverageColor gives.
const (
	CoverageRed    = "red"
	CoverageYellow = "yellow"
	CoverageGreen  = "green"
)

// CoverageColor is the color of a bar that is pct full (from 0 to 1): of the colors whose threshold pct reaches, the one with the highest
// threshold; red when it reaches none. A threshold of 0 is never reached, so a bar below every threshold is red (VS Code's rule).
func CoverageColor(pct float64, t BarThresholds) string {
	best, distance := CoverageRed, pct
	for _, c := range []struct {
		key string
		at  float64
	}{{CoverageRed, t.Red}, {CoverageYellow, t.Yellow}, {CoverageGreen, t.Green}} {
		th := c.at / 100
		if th != 0 && pct >= th && pct-th < distance {
			best, distance = c.key, pct-th
		}
	}
	return best
}

// ClampedPercent is the share that was covered, held between 0 and 1 (VS Code's percent); a count with nothing to cover is full.
func ClampedPercent(c Count) float64 { return min(max(c.Percent(), 0), 1) }

const percentEpsilon = 10e-8

// DisplayPercent writes a share from 0 to 1 as a percentage with the precision given, never showing 100% for a share that only rounds up
// to it.
func DisplayPercent(value float64, precision int) string {
	display := fmt.Sprintf("%.*f", precision, value*100)
	if value < 1-percentEpsilon && display == fmt.Sprintf("%.*f", precision, 100.0) {
		return fmt.Sprintf("%.*f%%", precision, 100-math.Pow(10, -float64(precision)))
	}
	return display + "%"
}

// DisplayedStat is the share the bars write as their number, as testing.displayedCoveragePercent says.
func DisplayedStat(statement Count, branch, declaration *Count, method string) float64 {
	switch method {
	case PercentStatement:
		return ClampedPercent(statement)
	case PercentMinimum:
		v := ClampedPercent(statement)
		if branch != nil {
			v = min(v, ClampedPercent(*branch))
		}
		if declaration != nil {
			v = min(v, ClampedPercent(*declaration))
		}
		return v
	}
	return TotalPercent(statement, branch, declaration)
}

// BarFill is how many of the 16 pixels of a bar are filled: floored, so that a bar is full only at 100%.
func BarFill(pct float64) int { return min(max(int(math.Floor(pct*16)), 0), 16) }
