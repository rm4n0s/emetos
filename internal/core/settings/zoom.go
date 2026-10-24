package settings

import "math"

// The range of window.zoomLevel and the size of a step of the zoom commands (SPEC §10.9).
//
// Ported from VS Code: src/vs/workbench/electron-browser/actions/windowActions.ts,
// src/vs/platform/window/electron-browser/window.ts (MIN_ZOOM_LEVEL, MAX_ZOOM_LEVEL) and
// src/vs/platform/window/common/window.ts (zoomLevelToZoomFactor)
const (
	// MinZoomLevel is the lowest zoom level of a window.
	MinZoomLevel = -8.0
	// MaxZoomLevel is the highest zoom level of a window.
	MaxZoomLevel = 8.0
	// ZoomFactorPerLevel is how much larger a window is for each zoom level above 0 (Electron's rule:
	// the scale is 1.2 to the power of the level).
	ZoomFactorPerLevel = 1.2
)

// ZoomFactor is the scale of a window at a zoom level: 1.2 to the power of the level, so that level 0 is 1,
// level 1 is 120% and level -1 is about 83%. A level that is not a number is level 0; a level outside the
// range is clamped to it (ClampZoomLevel).
func ZoomFactor(level float64) float64 {
	return math.Pow(ZoomFactorPerLevel, ClampZoomLevel(level))
}

// ClampZoomLevel keeps a zoom level in the range from MinZoomLevel to MaxZoomLevel; NaN is 0.
func ClampZoomLevel(level float64) float64 {
	if math.IsNaN(level) {
		return 0
	}
	return min(max(level, MinZoomLevel), MaxZoomLevel)
}

// ZoomStep is the level the zoom commands go to from a level: one more (in) or one less (out) than the
// level rounded to a whole number, as VS Code's actions round the level so that no fractional level is
// made. ok is false when the new level is outside the range, where the commands do nothing.
func ZoomStep(level float64, in bool) (next float64, ok bool) {
	delta := -1.0
	if in {
		delta = 1
	}
	next = math.Round(level + delta) // VS Code: Math.round(levelOrReset)
	if math.IsNaN(next) || next > MaxZoomLevel || next < MinZoomLevel {
		return level, false
	}
	return next, true
}
