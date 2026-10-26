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
	r := g.injectedRow(row, shown, start)
	if rel > len(shown) {
		return indent + g.injectedWidth(r) + float64(rel-len(shown))*g.charWidth
	}
	p := g.injectedParagraph(r)
	right := 0.0
	for _, b := range p.GetBoxesForRange(0, r.viewOffset(rel)) {
		right = max(right, b.Right)
	}
	if right == 0 && rel == len(shown) {
		right = p.LongestLine()
	}
	return indent + right
}

// injectedRow is the text a row shows (the first shown bytes of its line from start) with the inlay hints in it.
func (g geometry) injectedRow(row int, shown string, start int) injectedRow {
	line, _, end := g.proj.RowSpan(row)
	return inject(shown, start, g.vm.rowInjections(line, start, end, len(shown), g.lastRowOfLine(row)))
}

// injectedParagraph is the paragraph of an injected row for measuring: the font of the hints is in it.
func (g geometry) injectedParagraph(r injectedRow) *ui.Paragraph {
	return g.style.styledParagraph(g.vm.Paragraphs(), r.text, r.hintSpans())
}

// injectedWidth is the width of an injected row.
func (g geometry) injectedWidth(r injectedRow) float64 {
	if r.text == "" {
		return 0
	}
	return g.injectedParagraph(r).LongestLine()
}

// hintAt finds the inlay hint at a distance from the left edge of the text on a row, and the label part it is on; false when
// the point is not on a hint.
func (g geometry) hintAt(row int, x float64) (inj *injection, part int, ok bool) {
	full, start := g.rowFull(row)
	shown := full[:shownLength(full, g.vm.opts.StopRenderingLineAfter)]
	r := g.injectedRow(row, shown, start)
	if len(r.injs) == 0 {
		return nil, 0, false
	}
	x -= g.indentPx(row)
	p := g.injectedParagraph(r)
	xAt := func(off int) float64 {
		if off == 0 {
			return 0
		}
		right := 0.0
		for _, b := range p.GetBoxesForRange(0, off) {
			right = max(right, b.Right)
		}
		if right == 0 && off == len(r.text) {
			right = p.LongestLine()
		}
		return right
	}
	for i, in := range r.injs {
		from, to := xAt(r.at[i]+in.bgFrom), xAt(r.at[i]+in.bgTo)
		if x < from || x >= to {
			continue
		}
		for _, pt := range in.parts {
			if x >= xAt(r.at[i]+pt.from) && x < xAt(r.at[i]+pt.to) {
				return in, pt.index, true
			}
		}
		return in, -1, true // on the padding of the hint
	}
	return nil, 0, false
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
		if g.isZoneRow(row) {
			continue // a selection is not drawn in the rows of a view zone
		}
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
		// the rows on either side of a view zone are not next to each other: each run is styled alone
		for from := 0; from < len(rows); {
			to := from + 1
			for to < len(rows) && rows[to].Row == rows[to-1].Row+1 {
				to++
			}
			styleSelectionRows(rows[from:to], g.charWidth)
			from = to
		}
	}
	return rows
}

// nearestTextRow is the row of text nearest to a height in the rows of a view zone: the row above
// the zone when the height is in its upper half, the row below when it is in the lower half; when the
// zone is at the top or at the bottom of the document, the only side that has text. A view zone has
// no position of its own (VS Code's mouse target is the zone), so a drag that goes over it selects up
// to the text next to it.
func (g geometry) nearestTextRow(row int, y float64) int {
	id, _, _ := g.proj.RowZone(row)
	first, count := g.proj.ZoneRows(id)
	// a zone may be followed or preceded by the rows of other zones
	above, below := first-1, first+count
	for above >= 0 && g.isZoneRow(above) {
		above--
	}
	for below < g.proj.RowCount() && g.isZoneRow(below) {
		below++
	}
	upper := y < g.proj.RowTop(first)+float64(count)*g.proj.RowHeight(first)/2
	switch {
	case above < 0 && below >= g.proj.RowCount():
		return row
	case above < 0:
		return below
	case below >= g.proj.RowCount():
		return above
	case upper:
		return above
	}
	return below
}

// isZoneRow tells whether a row belongs to a view zone, which shows no text.
func (g geometry) isZoneRow(row int) bool {
	_, _, ok := g.proj.RowZone(row)
	return ok
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
	return g.slowColumnAt(text, nil, x)
}

// columnAtRow is columnAt for the text of a row, whose hints are among the characters it measures: the column of the
// document nearest to x, which is the place of the hint when x is on one.
func (g geometry) columnAtRow(row int, text string, start int, x float64) int {
	r := g.injectedRow(row, text, start)
	if len(r.injs) == 0 {
		return g.columnAt(text, x)
	}
	if x <= 0 {
		return 0
	}
	return r.modelOffset(g.slowColumnAt(r.text, r.hintSpans(), x))
}

// slowColumnAt finds the column through the paragraph: a binary search over the grapheme
// boundaries of the text with the x of each from the paragraph's boxes.
func (g geometry) slowColumnAt(text string, spans []span, x float64) int {
	bounds := edit.GraphemeBoundaries(text)
	p := g.style.styledParagraph(g.vm.Paragraphs(), text, spans)
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
	if g.isZoneRow(row) {
		row = g.nearestTextRow(row, y)
	}
	full, start := g.rowFull(row)
	text := full[:shownLength(full, g.vm.opts.StopRenderingLineAfter)]
	line, _, _ := g.proj.RowSpan(row)
	px := x - g.indentPx(row)
	if len(text) < len(full) {
		// past the part that is drawn the columns are as wide as a character
		if w := g.injectedWidth(g.injectedRow(row, text, start)); px > w && g.charWidth > 0 {
			col := len(text) + int(math.Floor((px-w)/g.charWidth+0.5))
			return buffer.Position{Line: line, Col: start + min(col, len(full))}
		}
	}
	return buffer.Position{Line: line, Col: start + g.columnAtRow(row, text, start, px)}
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
