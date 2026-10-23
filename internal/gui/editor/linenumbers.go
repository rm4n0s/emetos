package editor

import (
	"math"
	"strconv"
)

// LineNumbersMode is the value of the editor.lineNumbers setting.
type LineNumbersMode int

// The ways to show line numbers.
const (
	// LineNumbersOn shows the number of each line.
	LineNumbersOn LineNumbersMode = iota
	// LineNumbersOff shows none, and the gutter has no room for them.
	LineNumbersOff
	// LineNumbersRelative shows the distance in lines to the cursor; the cursor's own line
	// shows its number.
	LineNumbersRelative
	// LineNumbersInterval shows the number of the cursor's line, of every tenth line and of
	// the last line.
	LineNumbersInterval
)

// ParseLineNumbersMode reads the value of editor.lineNumbers; an unknown value is "on".
func ParseLineNumbersMode(s string) LineNumbersMode {
	switch s {
	case "off":
		return LineNumbersOff
	case "relative":
		return LineNumbersRelative
	case "interval":
		return LineNumbersInterval
	}
	return LineNumbersOn
}

// LineNumberText is what the gutter shows for a model line (from 0), given the line of the
// cursor and the number of lines. It is empty when no number is shown. current is true for
// the cursor's line in relative mode, which VS Code shows left-aligned.
//
// Ported from VS Code: src/vs/editor/browser/viewParts/lineNumbers/lineNumbers.ts
func LineNumberText(mode LineNumbersMode, line, cursorLine, lineCount int) (text string, current bool) {
	number := strconv.Itoa(line + 1)
	switch mode {
	case LineNumbersOff:
		return "", false
	case LineNumbersRelative:
		diff := line - cursorLine
		if diff == 0 {
			return number, true
		}
		return strconv.Itoa(max(diff, -diff)), false
	case LineNumbersInterval:
		if line == cursorLine || (line+1)%10 == 0 || line == lineCount-1 {
			return number, false
		}
		return "", false
	}
	return number, false
}

// GutterWidths are the widths of the parts of the gutter, left to right.
type GutterWidths struct {
	// GlyphMargin is the margin for glyphs such as breakpoints.
	GlyphMargin float64
	// LineNumbers is the room for the line numbers.
	LineNumbers float64
	// Decorations is the room for line decorations and the folding controls.
	Decorations float64
}

// Total is the width of the whole gutter.
func (w GutterWidths) Total() float64 { return w.GlyphMargin + w.LineNumbers + w.Decorations }

// GutterOptions are what the width of the gutter depends on.
type GutterOptions struct {
	LineNumbers         LineNumbersMode
	LineNumbersMinChars int
	// DigitWidth is the width of the widest digit of the editor font.
	DigitWidth float64
	LineHeight float64
	// GlyphMargin says whether there is a glyph margin; it is as wide as a line is high.
	GlyphMargin bool
	// LineDecorationsWidth is the width of the line decorations (10 in VS Code).
	LineDecorationsWidth float64
	// Folding says whether the folding controls take room (16 pixels more).
	Folding bool
}

// foldingControlsWidth is the room of the folding controls.
const foldingControlsWidth = 16

// ComputeGutterWidths gives the widths of the gutter for a document of lineCount lines. The
// line numbers have room for the digits of the last line or LineNumbersMinChars, whichever
// is more.
//
// Ported from VS Code: src/vs/editor/common/config/editorOptions.ts (EditorLayoutInfoComputer)
func ComputeGutterWidths(o GutterOptions, lineCount int) GutterWidths {
	var w GutterWidths
	if o.GlyphMargin {
		w.GlyphMargin = o.LineHeight
	}
	if o.LineNumbers != LineNumbersOff {
		digits := max(len(strconv.Itoa(max(lineCount, 1))), o.LineNumbersMinChars)
		w.LineNumbers = math.Round(float64(digits) * o.DigitWidth)
	}
	w.Decorations = o.LineDecorationsWidth
	if o.Folding {
		w.Decorations += foldingControlsWidth
	}
	return w
}
