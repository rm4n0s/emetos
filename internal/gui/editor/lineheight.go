package editor

import "math"

// The line height rule of VS Code.
//
// Ported from VS Code: src/vs/editor/common/config/fontInfo.ts
const (
	// goldenLineHeightRatio is the line height of an editor whose lineHeight setting is 0,
	// as a multiple of the font size (VS Code's value for Linux and Windows).
	goldenLineHeightRatio = 1.35
	// minimumLineHeight is the smallest line height in pixels.
	minimumLineHeight = 8
)

// LineHeight is the height of a line in pixels for the editor.lineHeight setting and the
// font size: 0 means round(1.35 × fontSize); a value below 8 is a multiplier of the font
// size; any other value is the height itself. The result is rounded to whole pixels and is
// at least 8.
func LineHeight(setting, fontSize float64) float64 {
	h := setting
	switch {
	case h == 0:
		h = goldenLineHeightRatio * fontSize
	case h < minimumLineHeight:
		h *= fontSize
	}
	h = math.Round(h)
	if h < minimumLineHeight {
		h = minimumLineHeight
	}
	return h
}
