package editor

import (
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// DiagnosticColors are the colors of the diagnostics in the editor.
type DiagnosticColors struct {
	// Error, Warning, Info and Hint are the colors of the squiggles (the dots for a hint).
	Error, Warning, Info, Hint ui.Color
	// ErrorBorder, WarningBorder, InfoBorder and HintBorder, when they are not transparent, add a double underline (the
	// themes of high contrast).
	ErrorBorder, WarningBorder, InfoBorder, HintBorder ui.Color
	// UnnecessaryOpacity is the opacity of unused code (its alpha) and UnnecessaryBorder the dashed line under it when it is
	// not transparent.
	UnnecessaryOpacity, UnnecessaryBorder ui.Color
}

// squiggle metrics: the pattern VS Code draws is 6 pixels wide and 3 high, with its low point 1.8 pixels from the left of the
// range and its high point at 4.8.
//
// Ported from VS Code: src/vs/editor/browser/widget/codeEditor/codeEditorWidget.ts (getSquigglySVGData, getDotDotDotSVGData)
const (
	squigglePeriod = 6.0
	squiggleLow    = 1.8
	squiggleHigh   = 4.8
	squiggleHeight = 3.0
	squiggleStroke = 1.2
	// strikeAt is how far down a row the line through deprecated text is, as a fraction of the row.
	strikeAt = 0.54
)

// diagSegment is the part of a diagnostic on one row: the x range in document coordinates and the row.
type diagSegment struct {
	row    int
	x1, x2 float64
}

// segments are the parts of a diagnostic on the rows of the frame. A range with no width covers the width of a character,
// where VS Code draws it; the rows of a view zone and the lines a fold hides have none.
func (v overlayView) segments(d Diagnostic) []diagSegment {
	vm := v.geo.vm
	startRow, endRow := vm.RowOfPosition(d.Range.Start), vm.RowOfPosition(d.Range.End)
	var out []diagSegment
	for row := max(startRow, v.first); row <= min(endRow, v.last); row++ {
		if v.geo.isZoneRow(row) {
			continue
		}
		line, _, _ := v.geo.proj.RowSpan(row)
		if _, _, hidden := vm.HiddenSpan(line); hidden {
			continue
		}
		x1, x2 := v.geo.indentPx(row), v.geo.rowEnd(row)
		if row == startRow {
			x1 = v.geo.x(row, d.Range.Start.Col)
		}
		if row == endRow {
			x2 = v.geo.x(row, d.Range.End.Col)
		}
		if x2 <= x1 {
			x2 = x1 + v.geo.charWidth
		}
		out = append(out, diagSegment{row: row, x1: x1, x2: x2})
	}
	return out
}

// paintDiagnostics draws the marks that go under the text: the squiggles of errors, warnings and infos, the dots of hints and
// the dashed line under unused code. The most serious are drawn last.
func (v overlayView) paintDiagnostics() {
	vm := v.geo.vm
	ds := vm.diagnosticsIn(v.first, v.last)
	if len(ds) == 0 {
		return
	}
	c := v.colors.Diagnostics
	opts := vm.Options()
	for _, d := range ds {
		for _, seg := range v.segments(d) {
			rect := v.rowRect(seg.row, seg.x1, seg.x2)
			bottom, left, right := rect.Bottom, rect.Left, rect.Right
			switch d.Severity {
			case DiagnosticHint:
				switch {
				case d.Deprecated:
				case d.Unnecessary:
					if opts.ShowUnused {
						v.dashedLine(left, right, bottom-1, c.UnnecessaryBorder)
					}
				default:
					v.hintDots(left, bottom, c.Hint)
					v.doubleLine(left, right, bottom, c.HintBorder)
				}
			default:
				color, border := c.Error, c.ErrorBorder
				switch d.Severity {
				case DiagnosticWarning:
					color, border = c.Warning, c.WarningBorder
				case DiagnosticInfo:
					color, border = c.Info, c.InfoBorder
				}
				v.squiggle(left, right, bottom, color)
				v.doubleLine(left, right, bottom, border)
				if d.Unnecessary && opts.ShowUnused && !d.Deprecated {
					v.dashedLine(left, right, bottom-1, c.UnnecessaryBorder)
				}
			}
		}
	}
}

// squiggle draws the wavy line of a range under its text, clipped to the range.
func (v overlayView) squiggle(left, right, bottom float64, color ui.Color) {
	if color.Alpha() == 0 || right <= left {
		return
	}
	low, high := bottom-squiggleStroke/2, bottom-squiggleHeight+squiggleStroke/2
	path := ui.NewPath()
	first := true
	for x := left + squiggleLow - squigglePeriod; x < right+squigglePeriod; x += squigglePeriod {
		for _, p := range [2]struct{ dx, y float64 }{{0, low}, {squiggleHigh - squiggleLow, high}} {
			if first {
				path.MoveTo(x+p.dx, p.y)
				first = false
			} else {
				path.LineTo(x+p.dx, p.y)
			}
		}
	}
	paint := ui.NewPaint()
	paint.SetColor(color)
	paint.SetStyle(ui.PaintingStyleStroke)
	paint.SetStrokeWidth(squiggleStroke)
	v.canvas.Save()
	v.canvas.ClipRect(ui.NewRectFromLTRB(left, bottom-squiggleHeight, right, bottom))
	v.canvas.DrawPath(path, paint)
	v.canvas.Restore()
}

// hintDots draws the three dots that mark a hint, at the start of its range.
func (v overlayView) hintDots(left, bottom float64, color ui.Color) {
	if color.Alpha() == 0 {
		return
	}
	paint := fill(color)
	for _, dx := range [3]float64{1, 5, 9} {
		v.canvas.DrawCircle(ui.Offset{Dx: left + dx, Dy: bottom - squiggleHeight + 1}, 1, paint)
	}
}

// doubleLine draws the double underline a theme can ask for with the border colors of the diagnostics.
func (v overlayView) doubleLine(left, right, bottom float64, color ui.Color) {
	if color.Alpha() == 0 || right <= left {
		return
	}
	paint := fill(color)
	v.canvas.DrawRect(ui.NewRectFromLTRB(left, bottom-4, right, bottom-3), paint)
	v.canvas.DrawRect(ui.NewRectFromLTRB(left, bottom-2, right, bottom-1), paint)
}

// dashedLine draws a line of dashes two pixels thick above y.
func (v overlayView) dashedLine(left, right, y float64, color ui.Color) {
	if color.Alpha() == 0 || right <= left {
		return
	}
	paint := fill(color)
	const dash, gap = 6.0, 6.0
	for x := left; x < right; x += dash + gap {
		v.canvas.DrawRect(ui.NewRectFromLTRB(x, y-2, min(x+dash, right), y), paint)
	}
}

// paintDiagnosticText changes the text of the rows in view that a diagnostic marks as unused or deprecated: the unused is faded
// by drawing the color of the editor over it with the rest of the opacity, the deprecated is struck through. It is drawn above
// the text.
//
// Ported from VS Code: markerDecorationsService.ts (_createDecorationOption: the inline class names) and
// src/vs/editor/browser/widget/codeEditor/editor.css (.squiggly-inline-deprecated)
func (v overlayView) paintDiagnosticText() {
	vm := v.geo.vm
	opts := vm.Options()
	if !opts.ShowUnused && !opts.ShowDeprecated {
		return
	}
	ds := vm.diagnosticsIn(v.first, v.last)
	if len(ds) == 0 {
		return
	}
	var fade *ui.Paint
	if op := v.colors.Diagnostics.UnnecessaryOpacity; op.Alpha() > 0 && v.colors.Background.Alpha() > 0 {
		fade = fill(v.colors.Background.WithOpacity(1 - op.Opacity()))
	}
	strike := fill(v.colors.Foreground)
	for _, d := range ds {
		fading := d.Unnecessary && !d.Deprecated && opts.ShowUnused && fade != nil
		striking := d.Deprecated && opts.ShowDeprecated
		if !fading && !striking {
			continue
		}
		for _, seg := range v.segments(d) {
			if d.Range.Start == d.Range.End {
				continue // nothing of the text is marked
			}
			rect := v.rowRect(seg.row, seg.x1, seg.x2)
			if fading {
				v.canvas.DrawRect(rect, fade)
			}
			if striking {
				y := rect.Top + (rect.Bottom-rect.Top)*strikeAt
				v.canvas.DrawRect(ui.NewRectFromLTRB(rect.Left, y, rect.Right, y+1), strike)
			}
		}
	}
}

// diagnosticMinimapMarks are the marks of the diagnostics that touch the rows of the minimap lines first to last, in the
// colors of the minimap, the most serious last.
func (vm *ViewModel) diagnosticMinimapMarks(firstRow, lastRow int, errColor, warnColor, infoColor ui.Color) []minimapMark {
	var marks []minimapMark
	for _, d := range vm.diagnosticsIn(firstRow, lastRow) {
		var color ui.Color
		switch d.Severity {
		case DiagnosticError:
			color = errColor
		case DiagnosticWarning:
			color = warnColor
		case DiagnosticInfo:
			color = infoColor
		default:
			continue
		}
		rng := d.Range
		if rng.Start == rng.End {
			rng.End = buffer.Position{Line: rng.End.Line, Col: rng.End.Col + 1}
		}
		marks = append(marks, minimapMark{rng: rng, color: color})
	}
	return marks
}
