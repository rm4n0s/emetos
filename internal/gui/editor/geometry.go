package editor

import (
	"slices"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// geometry measures positions of the document in the rows of the editor, through the same
// cached paragraphs the text layer draws. It is made for one frame.
type geometry struct {
	vm          *ViewModel
	proj        Projection
	style       textStyle
	lineContent func(int) (string, error)
	// charWidth is the width of one character of the font, the width of the end of a line
	// in a selection.
	charWidth float64
}

// newGeometry makes the geometry of the view model's text in the given text engine.
func newGeometry(vm *ViewModel, factory ui.ParagraphFactory) geometry {
	style := newTextStyle(factory, vm.Options(), ui.NewColor(0xFFFFFFFF))
	return geometry{
		vm:          vm,
		proj:        vm.Projection(),
		style:       style,
		lineContent: vm.Document().Snapshot().LineContent,
		charWidth:   style.paragraph(vm.Paragraphs(), "0").LongestLine(),
	}
}

// rowText is the text a row shows and the byte column of the line it starts at.
func (g geometry) rowText(row int) (text string, start int) {
	line, start, end := g.proj.RowSpan(row)
	text, err := g.lineContent(line)
	if err != nil {
		return "", 0
	}
	if end < 0 || end > len(text) {
		end = len(text)
	}
	start = min(start, end)
	return text[start:end], start
}

// x is the distance from the left edge of the text to a byte column of the line shown on a
// row; a column before the row is its start and one after is its end.
func (g geometry) x(row, col int) float64 {
	text, start := g.rowText(row)
	rel := min(max(col-start, 0), len(text))
	if rel == 0 {
		return 0
	}
	p := g.style.paragraph(g.vm.Paragraphs(), text)
	right := 0.0
	for _, b := range p.GetBoxesForRange(0, rel) {
		right = max(right, b.Right)
	}
	if right == 0 && rel == len(text) {
		right = p.LongestLine()
	}
	return right
}

// rowEnd is the x of the end of a row's text.
func (g geometry) rowEnd(row int) float64 {
	text, start := g.rowText(row)
	return g.x(row, start+len(text))
}

// lastRowOfLine tells whether a row is the last one of its line.
func (g geometry) lastRowOfLine(row int) bool {
	line, _, _ := g.proj.RowSpan(row)
	first, count := g.proj.LineRows(line)
	return row == first+count-1
}

// selectionRows are the rows of a selection that a frame needs: those between the rows
// first and last (which the editor shows) with one more row on each side, so that the corners
// at the edge of the editor are styled as the whole selection says. A row where the selection
// has no width is left out, as in VS Code.
//
// Ported from VS Code: src/vs/editor/browser/viewParts/selections/selections.ts (linesVisibleRangesForRange with includeNewLine)
func (g geometry) selectionRows(sel buffer.Selection, first, last int, rounded bool) []selectionRow {
	start, end := sel.Start(), sel.End()
	startRow, endRow := g.vm.RowOfPosition(start), g.vm.RowOfPosition(end)
	var rows []selectionRow
	for row := max(startRow, first-1); row <= min(endRow, last+1); row++ {
		left := 0.0
		if row == startRow {
			left = g.x(row, start.Col)
		}
		var right float64
		if row == endRow {
			right = g.x(row, end.Col)
		} else {
			right = g.rowEnd(row)
			if g.lastRowOfLine(row) {
				right += g.charWidth // the line break is selected too
			}
		}
		if right-left <= 0 {
			continue
		}
		rows = append(rows, selectionRow{Row: row, Left: left, Right: right})
	}
	if rounded {
		styleSelectionRows(rows, g.charWidth)
	}
	return rows
}

// cursorRows are the rows that have a cursor, each once, in order.
func cursorRows(vm *ViewModel) []int {
	var rows []int
	for _, sel := range vm.Cursors().Selections() {
		rows = append(rows, vm.RowOfPosition(sel.Active))
	}
	slices.Sort(rows)
	return slices.Compact(rows)
}

// allEmpty tells whether no selection has text.
func allEmpty(sels []buffer.Selection) bool {
	for _, s := range sels {
		if !s.IsEmpty() {
			return false
		}
	}
	return true
}

// highlightInContent tells whether the current line is highlighted in the text: the setting
// says line or all, no selection has text and, when the setting asks for it, the editor has
// the focus.
//
// Ported from VS Code: src/vs/editor/browser/viewParts/currentLineHighlight/currentLineHighlight.ts (_shouldRenderInContent)
func highlightInContent(o Options, focused, selectionsEmpty bool) bool {
	return (o.RenderLineHighlight == "line" || o.RenderLineHighlight == "all") &&
		selectionsEmpty && (!o.RenderLineHighlightOnlyWhenFocus || focused)
}

// highlightInMargin tells whether the current line is highlighted in the gutter.
//
// Ported from VS Code: src/vs/editor/browser/viewParts/currentLineHighlight/currentLineHighlight.ts (_shouldRenderInMargin)
func highlightInMargin(o Options, focused bool) bool {
	return (o.RenderLineHighlight == "gutter" || o.RenderLineHighlight == "all") &&
		(!o.RenderLineHighlightOnlyWhenFocus || focused)
}
