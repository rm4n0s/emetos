package editor

import (
	"math"
	"slices"
	"sort"
	"unicode/utf8"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/edit"
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

// rowFull is the whole text of a row and the byte column of the line it starts at.
func (g geometry) rowFull(row int) (text string, start int) {
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

// rowText is the text a row shows and the byte column of the line it starts at: the text of the
// row up to editor.stopRenderingLineAfter bytes (SPEC §6.4); what is beyond is not laid out or
// drawn, and the positions in it are worked out with the width of a character.
func (g geometry) rowText(row int) (text string, start int) {
	text, start = g.rowFull(row)
	return text[:shownLength(text, g.vm.opts.StopRenderingLineAfter)], start
}

// shownLength is how many bytes of a row's text are laid out and drawn: all of it, or up to
// editor.stopRenderingLineAfter bytes, cut at the start of a character. A limit of zero or less
// is no limit.
//
// Ported from VS Code: src/vs/editor/common/viewLayout/viewLineRenderer.ts (stopRenderingLineAfter)
func shownLength(text string, stopAfter int) int {
	if stopAfter <= 0 || len(text) <= stopAfter {
		return len(text)
	}
	cut := stopAfter
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return cut
}

// indentPx is how far a row is indented from the left edge of the text: the rows that word
// wrap makes after the first of a line start further right (editor.wrappingIndent).
func (g geometry) indentPx(row int) float64 {
	if ri, ok := g.proj.(rowIndenter); ok {
		return float64(ri.RowIndentColumns(row)) * g.charWidth
	}
	return 0
}

// x is the distance from the left edge of the text to a byte column of the line shown on a
// row; a column before the row is its start and one after is its end.
func (g geometry) x(row, col int) float64 {
	indent := g.indentPx(row)
	full, start := g.rowFull(row)
	shown := full[:shownLength(full, g.vm.opts.StopRenderingLineAfter)]
	rel := min(max(col-start, 0), len(full))
	if rel == 0 {
		return indent
	}
	if rel > len(shown) {
		return indent + g.textWidth(shown) + float64(rel-len(shown))*g.charWidth
	}
	p := g.style.paragraph(g.vm.Paragraphs(), shown)
	right := 0.0
	for _, b := range p.GetBoxesForRange(0, rel) {
		right = max(right, b.Right)
	}
	if right == 0 && rel == len(shown) {
		right = p.LongestLine()
	}
	return indent + right
}

// textWidth is the width of a text laid out in the font of the editor.
func (g geometry) textWidth(text string) float64 {
	if text == "" {
		return 0
	}
	return g.style.paragraph(g.vm.Paragraphs(), text).LongestLine()
}

// rowEnd is the x of the end of a row's text, also when part of it is not drawn.
func (g geometry) rowEnd(row int) float64 {
	full, start := g.rowFull(row)
	return g.x(row, start+len(full))
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
		left := g.indentPx(row)
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

// cursorRows are the rows that have a cursor, each once, in order; with word wrap all the rows of
// the line of a cursor.
//
// Ported from VS Code: src/vs/editor/browser/viewParts/currentLineHighlight/currentLineHighlight.ts (the lines of a wrapped line)
func cursorRows(vm *ViewModel) []int {
	var rows []int
	proj := vm.Projection()
	for _, sel := range vm.Cursors().Selections() {
		row := vm.RowOfPosition(sel.Active)
		if !vm.wrap.key.on {
			rows = append(rows, row)
			continue
		}
		first, count := proj.LineRows(sel.Active.Line)
		for r := first; r < first+max(count, 1); r++ {
			rows = append(rows, r)
		}
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

// scrollBeyondLastColumn is how many characters of room the horizontal scroll leaves after the
// longest line.
const scrollBeyondLastColumn = 4

// isMonospace tells whether the font has one width for every character, by measuring a narrow
// and a wide one.
func (g geometry) isMonospace() bool {
	narrow := g.style.paragraph(g.vm.Paragraphs(), "i").LongestLine()
	wide := g.style.paragraph(g.vm.Paragraphs(), "W").LongestLine()
	return math.Abs(narrow-wide) < 0.001 && math.Abs(narrow-g.charWidth) < 0.001
}

// asciiWithoutTabs tells whether a text is made of single-column characters whose x is the
// column times the width of a character.
func asciiWithoutTabs(text string) bool {
	for i := range len(text) {
		if c := text[i]; c >= utf8.RuneSelf || c == '\t' || c < ' ' {
			return false
		}
	}
	return true
}

// columnAt is the byte column of a text nearest to x, at the start of a grapheme: the column
// before a character when x is in its left half, after it otherwise. An x before the text is
// column 0 and one past the end is the length of the text.
func (g geometry) columnAt(text string, x float64) int {
	if text == "" || x <= 0 {
		return 0
	}
	if asciiWithoutTabs(text) && g.isMonospace() {
		return min(int(math.Floor(x/g.charWidth+0.5)), len(text))
	}
	return g.slowColumnAt(text, x)
}

// slowColumnAt finds the column through the paragraph: a binary search over the grapheme
// boundaries of the text with the x of each from the paragraph's boxes.
func (g geometry) slowColumnAt(text string, x float64) int {
	bounds := edit.GraphemeBoundaries(text)
	p := g.style.paragraph(g.vm.Paragraphs(), text)
	xAt := func(col int) float64 {
		if col == 0 {
			return 0
		}
		right := 0.0
		for _, b := range p.GetBoxesForRange(0, col) {
			right = max(right, b.Right)
		}
		if right == 0 && col == len(text) {
			right = p.LongestLine()
		}
		return right
	}
	// the first boundary at or after x
	i := sort.Search(len(bounds), func(i int) bool { return xAt(bounds[i]) >= x })
	if i == len(bounds) {
		return len(text)
	}
	if i > 0 && x-xAt(bounds[i-1]) < xAt(bounds[i])-x {
		i--
	}
	return bounds[i]
}

// positionAt is the position of the document nearest to a point, in document coordinates (x
// from the left edge of the text, y from the top of the first row).
//
// Ported from VS Code: src/vs/editor/browser/controller/mouseTarget.ts (the content position of a point, without the
// special cases of decorations)
func (g geometry) positionAt(x, y float64) buffer.Position {
	row := g.proj.RowAt(y)
	full, start := g.rowFull(row)
	text := full[:shownLength(full, g.vm.opts.StopRenderingLineAfter)]
	line, _, _ := g.proj.RowSpan(row)
	px := x - g.indentPx(row)
	if len(text) < len(full) {
		// past the part that is drawn the columns are as wide as a character
		if w := g.textWidth(text); px > w && g.charWidth > 0 {
			col := len(text) + int(math.Floor((px-w)/g.charWidth+0.5))
			return buffer.Position{Line: line, Col: start + min(col, len(full))}
		}
	}
	return buffer.Position{Line: line, Col: start + g.columnAt(text, px)}
}

// rectOf is the rectangle of the character at a position, in document coordinates: from the x
// of the position to the x of the next grapheme, as high as the row. At the end of a line it is
// as wide as one character.
func (g geometry) rectOf(p buffer.Position) ui.Rect {
	row := g.vm.RowOfPosition(p)
	text, start := g.rowText(row)
	full, _ := g.rowFull(row)
	col := min(max(p.Col-start, 0), len(full))
	left := g.x(row, start+col)
	right := left + g.charWidth
	if col < len(text) {
		right = g.x(row, start+edit.NextGrapheme(text, col))
	}
	top := g.proj.RowTop(row)
	return ui.NewRectFromLTRB(left, top, max(right, left), top+g.proj.RowHeight(row))
}

// visualColumnAt is the visual column nearest to an x in document coordinates, counted in
// characters of the font, also past the end of a line (a column selection reaches into the
// empty space there).
//
// Ported from VS Code: src/vs/editor/browser/controller/mouseTarget.ts (MouseTargetFactory.getMouseColumn)
func (g geometry) visualColumnAt(x float64) int {
	if g.charWidth <= 0 {
		return 0
	}
	return max(int(math.Floor(x/g.charWidth+0.5)), 0)
}
