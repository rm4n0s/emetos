package editor

import (
	"math"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/snippet"
)

// Ported from VS Code: src/vs/editor/browser/viewParts/rulers/rulers.ts,
// src/vs/editor/browser/viewParts/whitespace/whitespace.ts and
// src/vs/editor/contrib/wordHighlighter/browser/highlightDecorations.ts

// paintRulers draws the vertical lines of editor.rulers: a line of 1 pixel at the left of the
// column, as high as the layer.
func (v overlayView) paintRulers() {
	for _, r := range v.geo.vm.Options().Rulers {
		color := r.Color
		if color.Alpha() == 0 {
			color = v.colors.Ruler
		}
		if color.Alpha() == 0 {
			continue
		}
		x := v.origin.Dx + float64(r.Column)*v.geo.charWidth
		if x+1 < v.bounds.Left || x > v.bounds.Right {
			continue
		}
		v.canvas.DrawRect(ui.NewRectFromLTRB(x, v.bounds.Top, x+1, v.bounds.Bottom), fill(color))
	}
}

// paintOccurrences draws the text that equals the selection and the other occurrences of the word
// at the cursor, under the find matches.
func (v overlayView) paintOccurrences() {
	vm := v.geo.vm
	for _, c := range []struct {
		owner              int
		background, border ui.Color
	}{
		{selectionHighlightOwner, v.colors.SelectionHighlight, v.colors.SelectionHighlightBorder},
		{wordHighlightOwner, v.colors.WordHighlight, v.colors.WordHighlightBorder},
	} {
		for _, d := range vm.occurrencesIn(c.owner, v.first, v.last) {
			if _, _, hidden := vm.HiddenSpan(d.Range.Start.Line); !hidden {
				v.paintMatch(d.Range, c.background, c.border)
			}
		}
	}
}

// paintSnippetPlaceholders draws the tab stops of the snippet that is being filled in: the final
// one ($0) and the others in the colors of their own. A tab stop with no text is not drawn; the
// cursor is there.
//
// Ported from VS Code: src/vs/editor/contrib/snippet/browser/snippetSession.ts (the decorations)
func (v overlayView) paintSnippetPlaceholders() {
	vm := v.geo.vm
	for _, d := range vm.snippetPlaceholders(v.first, v.last) {
		if d.Range.Start == d.Range.End {
			continue
		}
		if _, _, hidden := vm.HiddenSpan(d.Range.Start.Line); hidden {
			continue
		}
		if d.Options.ClassName == snippet.ClassFinalPlaceholder {
			v.paintMatch(d.Range, v.colors.SnippetFinalTabstop, v.colors.SnippetFinalTabstopBorder)
		} else {
			v.paintMatch(d.Range, v.colors.SnippetTabstop, v.colors.SnippetTabstopBorder)
		}
	}
}

// paintWhitespace draws the dots and arrows of editor.renderWhitespace on the spaces and tabs of
// the visible rows: a dot a seventh of a space in radius in the middle of a space, and an arrow
// as wide as a space on the left of a tab.
//
// Ported from VS Code: whitespace.ts (WhitespaceOverlay._renderCircle, _renderArrow)
func (v overlayView) paintWhitespace() {
	vm := v.geo.vm
	mode := vm.Options().RenderWhitespace
	if mode == "" || mode == "none" || v.colors.Whitespace.Alpha() == 0 {
		return
	}
	var sels = vm.Cursors().Selections()
	paint := fill(v.colors.Whitespace)
	space := v.geo.charWidth
	for row := v.first; row <= v.last; row++ {
		text, start := v.geo.rowText(row)
		line, _, _ := v.geo.proj.RowSpan(row)
		var spans []colSpan
		if mode == "selection" {
			spans = selectionSpans(sels, line, start, start+len(text))
		}
		marks := whitespaceMarks(text, mode, spans, !v.geo.lastRowOfLine(row), vm.Options().StopRenderingLineAfter)
		if len(marks) == 0 {
			continue
		}
		rect := v.rowRect(row, 0, 0)
		cy := rect.Top + v.geo.proj.RowHeight(row)/2
		for _, m := range marks {
			left := v.origin.Dx + v.markX(row, text, start, m.Col)
			if m.Tab {
				v.canvas.DrawPath(whitespaceArrow(left, cy, space), paint)
			} else {
				v.canvas.DrawCircle(ui.Offset{Dx: left + space/2, Dy: cy}, space/7, paint)
			}
		}
	}
}

// markX is the x of a column of a row's text: from the width of a character when the text is
// plain ASCII, from the paragraph otherwise.
func (v overlayView) markX(row int, text string, start, col int) float64 {
	if asciiWithoutTabs(text) && v.geo.isMonospace() {
		return v.geo.indentPx(row) + float64(col)*v.geo.charWidth
	}
	return v.geo.x(row, start+col)
}

// whitespaceArrow is the arrow of a tab: a bar and a head, one space wide, vertically centered
// at cy.
//
// Ported from VS Code: whitespace.ts (WhitespaceOverlay._renderArrow)
func whitespaceArrow(left, cy, width float64) *ui.Path {
	stroke := width / 7
	type pt struct{ x, y float64 }
	p1 := pt{0, stroke / 2}
	p2 := pt{100.0 / 125.0 * width, p1.y}
	p3 := pt{p2.x - 0.2*p2.x, p2.y + 0.2*p2.x}
	p4 := pt{p3.x + 0.1*p2.x, p3.y + 0.1*p2.x}
	p5 := pt{p4.x + 0.35*p2.x, p4.y - 0.35*p2.x}
	p6 := pt{p5.x, -p5.y}
	p7 := pt{p4.x, -p4.y}
	p8 := pt{p3.x, -p3.y}
	p9 := pt{p2.x, -p2.y}
	p10 := pt{p1.x, -p1.y}
	path := ui.NewPath()
	for i, p := range []pt{p1, p2, p3, p4, p5, p6, p7, p8, p9, p10} {
		x, y := left+p.x, cy+p.y
		if i == 0 {
			path.MoveTo(x, y)
		} else {
			path.LineTo(x, y)
		}
	}
	path.Close()
	return path
}

// controlBackground is the color of the boxes of control characters (VS Code's .mtkcontrol, whose
// glyph is white, as the paragraph of the geometry is).
var controlBackground = ui.NewColor(0xFF960000)

// minControlWidth is the width of the box of a control character that has no width of its own.
const minControlWidth = 2.0

// paintControlCharacters draws a box over each control character of the visible rows: the
// character's cell, or a bar when it has no width, in dark red with the Control Pictures glyph of
// the character in white.
//
// Ported from VS Code: viewLineRenderer.ts (extractControlCharacters), viewLines.css (.mtkcontrol)
func (v overlayView) paintControlCharacters() {
	vm := v.geo.vm
	if !vm.Options().RenderControlCharacters {
		return
	}
	for row := v.first; row <= v.last; row++ {
		text, start := v.geo.rowText(row)
		for _, m := range controlMarks(text) {
			left := v.origin.Dx + v.geo.x(row, start+m.Col)
			right := v.origin.Dx + v.geo.x(row, start+m.Col+m.Len)
			if right-left < minControlWidth {
				right = left + minControlWidth
			}
			rect := v.rowRect(row, 0, 0)
			box := ui.NewRectFromLTRB(left, rect.Top, right, rect.Bottom)
			v.canvas.DrawRect(box, fill(controlBackground))
			if m.Picture == 0 || right-left < v.geo.charWidth*0.5 {
				continue
			}
			p := v.geo.style.paragraph(vm.Paragraphs(), string(m.Picture))
			v.canvas.Save()
			v.canvas.ClipRect(box)
			y := box.Top + (box.Height()-p.Height())/2
			x := left + (right-left-math.Min(p.LongestLine(), right-left))/2
			v.canvas.DrawParagraph(p, ui.Offset{Dx: x, Dy: y})
			v.canvas.Restore()
		}
	}
}
