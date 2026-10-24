package editor

import (
	"reflect"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/edit"
)

// OverlayColors are the colors of the layers drawn above and below the text.
type OverlayColors struct {
	// Background is the color of the editor, used to cut reversed corners into selections.
	Background ui.Color
	// Selection is the color of a selection in the editor that has the focus.
	Selection ui.Color
	// InactiveSelection is the color of a selection in an editor without the focus.
	InactiveSelection ui.Color
	// LineHighlight is the background of the current line.
	LineHighlight ui.Color
	// LineHighlightBorder is the border of the current line.
	LineHighlightBorder ui.Color
	// Cursor is the color of the cursors.
	Cursor ui.Color
	// CursorText is the color of the character a block cursor, or a wide line cursor, covers:
	// the opposite of the cursor's color.
	CursorText ui.Color
	// Foreground is the color of the text, for the text an input method composes.
	Foreground ui.Color
	// BracketMatch and BracketMatchBorder are the background and the border of the brackets
	// that match.
	BracketMatch, BracketMatchBorder ui.Color
	// Guides are the colors of the indent guides and of the bracket pair guides.
	Guides GuideColors
	// FoldBackground is the background of a folded line and FoldPlaceholder the color of the
	// "…" after it.
	FoldBackground, FoldPlaceholder ui.Color
	// FindMatch and FindMatchBorder color the current match of the find widget,
	// FindMatchHighlight and its border the other matches, FindRangeHighlight and its border the
	// lines of the current match and the range the search is limited to.
	FindMatch, FindMatchBorder                   ui.Color
	FindMatchHighlight, FindMatchHighlightBorder ui.Color
	FindRangeHighlight, FindRangeHighlightBorder ui.Color
	// RangeHighlight and its border color the line that go to line shows while the user types.
	RangeHighlight, RangeHighlightBorder ui.Color
	// Whitespace is the color of the dots and arrows of editor.renderWhitespace and Ruler the
	// color of the rulers that have none of their own.
	Whitespace, Ruler ui.Color
	// SelectionHighlight and its border color the text that equals the selection, WordHighlight
	// and its border the other occurrences of the word at the cursor.
	SelectionHighlight, SelectionHighlightBorder ui.Color
	WordHighlight, WordHighlightBorder           ui.Color
	// SnippetTabstop and its border color the tab stops of a snippet that is being filled in,
	// SnippetFinalTabstop and its border the final one.
	SnippetTabstop, SnippetTabstopBorder           ui.Color
	SnippetFinalTabstop, SnippetFinalTabstopBorder ui.Color
}

// overlayKind tells which part of the decorations a RenderEditorOverlay draws.
type overlayKind int

const (
	// overlayUnder is drawn below the text: the current line and the selections.
	overlayUnder overlayKind = iota
	// overlayCursors is drawn above the text: the boxes of control characters and the cursors.
	overlayCursors
)

// lineHighlightBorderWidth is the width of the border of the current line.
const lineHighlightBorderWidth = 2.0

// cursorDefaultWidth is the width of the line cursor when editor.cursorWidth is 0.
const cursorDefaultWidth = 2.0

// dropCaretWidth is the width of the dotted line that shows where dragged text goes, and of its dots.
const dropCaretWidth = 2.0

// RenderEditorOverlay is a layer of the editor that draws decorations of the text without
// touching it: the one below the text has the current line and the selections, the one above
// has the cursors. Each is a repaint boundary, so that the blinking of the cursors repaints
// only the cursor layer and a new selection does not repaint the text (SPEC §9.1).
type RenderEditorOverlay struct {
	rendering.RenderBox
	vm      *ViewModel
	offset  rendering.ViewportOffset
	factory ui.ParagraphFactory
	colors  OverlayColors
	kind    overlayKind

	offsetHandle foundation.ListenerHandle
	vmHandle     foundation.ListenerHandle
	selHandle    foundation.ListenerHandle
	blinkHandle  foundation.ListenerHandle
	attached     bool
}

// NewRenderEditorOverlay makes the layer of a kind for a view model.
func NewRenderEditorOverlay(kind overlayKind, vm *ViewModel, offset rendering.ViewportOffset, factory ui.ParagraphFactory, colors OverlayColors) *RenderEditorOverlay {
	r := &RenderEditorOverlay{kind: kind, vm: vm, offset: offset, factory: factory, colors: colors}
	rendering.InitRenderBox(&r.RenderBox, r)
	return r
}

// Update copies the configuration of a widget.
func (r *RenderEditorOverlay) Update(vm *ViewModel, offset rendering.ViewportOffset, factory ui.ParagraphFactory, colors OverlayColors) {
	if r.attached {
		if vm != r.vm {
			r.unlisten()
			r.vm = vm
			r.listen()
		}
		if offset != r.offset {
			r.offset.RemoveListener(r.offsetHandle)
			r.offset = offset
			r.offsetHandle = offset.AddListener(r.MarkNeedsPaint)
		}
	}
	r.vm, r.offset, r.factory = vm, offset, factory
	if !reflect.DeepEqual(colors, r.colors) {
		r.colors = colors
	}
	r.MarkNeedsPaint()
}

func (r *RenderEditorOverlay) listen() {
	r.vmHandle = r.vm.AddListener(r.MarkNeedsPaint)
	r.selHandle = r.vm.AddSelectionListener(r.MarkNeedsPaint)
	if r.kind == overlayCursors {
		r.blinkHandle = r.vm.AddBlinkListener(r.MarkNeedsPaint)
	}
}

func (r *RenderEditorOverlay) unlisten() {
	r.vm.RemoveListener(r.vmHandle)
	r.vm.RemoveSelectionListener(r.selHandle)
	if r.kind == overlayCursors {
		r.vm.RemoveBlinkListener(r.blinkHandle)
	}
}

// Attach implements rendering.RenderObject.
func (r *RenderEditorOverlay) Attach(owner *rendering.PipelineOwner) {
	r.RenderBox.Attach(owner)
	r.offsetHandle = r.offset.AddListener(r.MarkNeedsPaint)
	r.listen()
	r.attached = true
}

// Detach implements rendering.RenderObject.
func (r *RenderEditorOverlay) Detach() {
	r.offset.RemoveListener(r.offsetHandle)
	r.unlisten()
	r.attached = false
	r.RenderBox.Detach()
}

// SizedByParent implements rendering.RenderObject.
func (r *RenderEditorOverlay) SizedByParent() bool { return true }

// IsRepaintBoundary implements rendering.RenderObject.
func (r *RenderEditorOverlay) IsRepaintBoundary() bool { return true }

// HitTestSelf implements rendering.RenderBox: the layer lets the pointer through to the text.
func (r *RenderEditorOverlay) HitTestSelf(ui.Offset) bool { return false }

// ComputeMinIntrinsicWidth implements rendering.RenderBox.
func (r *RenderEditorOverlay) ComputeMinIntrinsicWidth(float64) float64 { return 0 }

// ComputeMaxIntrinsicWidth implements rendering.RenderBox.
func (r *RenderEditorOverlay) ComputeMaxIntrinsicWidth(float64) float64 { return 0 }

// ComputeMinIntrinsicHeight implements rendering.RenderBox.
func (r *RenderEditorOverlay) ComputeMinIntrinsicHeight(float64) float64 { return 0 }

// ComputeMaxIntrinsicHeight implements rendering.RenderBox.
func (r *RenderEditorOverlay) ComputeMaxIntrinsicHeight(float64) float64 { return 0 }

// ComputeDryLayout implements rendering.RenderBox.
func (r *RenderEditorOverlay) ComputeDryLayout(c rendering.BoxConstraints) ui.Size {
	return c.Constrain(c.Biggest())
}

// PerformLayout implements rendering.RenderBox; the layer has nothing to lay out.
func (r *RenderEditorOverlay) PerformLayout() {}

func (r *RenderEditorOverlay) top() float64 {
	if r.offset.HasPixels() {
		return r.offset.Pixels()
	}
	return r.vm.ScrollTop()
}

// Paint draws the decorations of the visible rows.
func (r *RenderEditorOverlay) Paint(context *rendering.PaintingContext, offset ui.Offset) {
	size := r.Size()
	top := r.top()
	proj := r.vm.Projection()
	first, last := proj.RowAt(top), proj.RowAt(top+size.Height)
	canvas := context.Canvas()
	canvas.Save()
	canvas.ClipRect(ui.NewRectFromLTWH(offset.Dx, offset.Dy, size.Width, size.Height))
	view := overlayView{
		canvas: canvas, geo: newGeometry(r.vm, r.factory), colors: r.colors, factory: r.factory,
		origin: ui.Offset{Dx: offset.Dx - r.vm.ScrollLeft(), Dy: offset.Dy - top}, width: size.Width,
		first: first, last: last, bounds: ui.NewRectFromLTWH(offset.Dx, offset.Dy, size.Width, size.Height),
	}
	if r.kind == overlayUnder {
		view.paintLineHighlight()
		view.paintFolded()
		view.paintSelections()
		view.paintRulers()
		view.paintGuides()
		view.paintOccurrences()
		view.paintSnippetPlaceholders()
		view.paintFind()
		view.paintRangeHighlight()
		view.paintWhitespace()
		view.paintMatchingBrackets()
	} else {
		// the boxes of control characters cover what the font draws for them
		view.paintControlCharacters()
		view.paintCursors()
	}
	canvas.Restore()
}

// overlayView is what a layer draws a frame with.
type overlayView struct {
	canvas  *ui.Canvas
	geo     geometry
	colors  OverlayColors
	factory ui.ParagraphFactory
	// origin is where the top left of the document is on the canvas.
	origin ui.Offset
	// width is the width of the layer.
	width       float64
	first, last int
	// bounds is the layer on the canvas.
	bounds ui.Rect
}

func fill(c ui.Color) *ui.Paint {
	p := ui.NewPaint()
	p.SetColor(c)
	return p
}

func (v overlayView) rowRect(row int, left, right float64) ui.Rect {
	top := v.origin.Dy + v.geo.proj.RowTop(row)
	return ui.NewRectFromLTRB(v.origin.Dx+left, top, v.origin.Dx+right, top+v.geo.proj.RowHeight(row))
}

// paintLineHighlight draws the current line in the text: the background across the layer
// and, when the theme gives no background, the border of the line.
//
// Ported from VS Code: src/vs/editor/browser/viewParts/currentLineHighlight/currentLineHighlight.ts
func (v overlayView) paintLineHighlight() {
	vm := v.geo.vm
	if !highlightInContent(vm.Options(), vm.Focused(), allEmpty(vm.Cursors().Selections())) {
		return
	}
	for _, row := range cursorRows(vm) {
		if row < v.first || row > v.last {
			continue
		}
		// the layer's left edge is the horizontal scroll to the right of the document's
		rect := v.rowRect(row, vm.ScrollLeft(), vm.ScrollLeft()+v.width)
		paintLineHighlight(v.canvas, rect, v.colors.LineHighlight, v.colors.LineHighlightBorder, lineHighlightEdges{true, true, true, true})
	}
}

// lineHighlightEdges say which sides of the current line have a border.
type lineHighlightEdges struct{ Left, Top, Right, Bottom bool }

// paintLineHighlight fills a rectangle with the highlight color and draws the border inside
// it, as VS Code's `box-sizing: border-box; border: 2px solid`.
func paintLineHighlight(canvas *ui.Canvas, rect ui.Rect, background, border ui.Color, edges lineHighlightEdges) {
	if background.Alpha() > 0 {
		canvas.DrawRect(rect, fill(background))
	}
	// VS Code draws the border when the theme has no highlight color or one that is
	// transparent (and when the theme defines a border, which Emetos's colors do not tell).
	if border.Alpha() == 0 || background.Alpha() > 0 {
		return
	}
	w := lineHighlightBorderWidth
	paint := fill(border)
	if edges.Top {
		canvas.DrawRect(ui.NewRectFromLTRB(rect.Left, rect.Top, rect.Right, rect.Top+w), paint)
	}
	if edges.Bottom {
		canvas.DrawRect(ui.NewRectFromLTRB(rect.Left, rect.Bottom-w, rect.Right, rect.Bottom), paint)
	}
	if edges.Left {
		canvas.DrawRect(ui.NewRectFromLTRB(rect.Left, rect.Top+w, rect.Left+w, rect.Bottom-w), paint)
	}
	if edges.Right {
		canvas.DrawRect(ui.NewRectFromLTRB(rect.Right-w, rect.Top+w, rect.Right, rect.Bottom-w), paint)
	}
}

// paintGuides draws the indent guides and the bracket pair guides of the visible rows: a line
// of 1 pixel at a column, or a horizontal one at the top or the bottom of a row, below the text.
//
// Ported from VS Code: src/vs/editor/browser/viewParts/indentGuides/indentGuides.ts
func (v overlayView) paintGuides() {
	vm := v.geo.vm
	rows := vm.rowGuides(v.first, v.last)
	for i, guides := range rows {
		row := v.first + i
		for _, g := range guides {
			list := v.colors.Guides.Indent
			if g.Bracket {
				list = v.colors.Guides.Bracket
			}
			c, ok := guideColor(list, g.Level)
			if !ok {
				continue
			}
			color := c.Color
			if g.Active {
				color = c.Active
			}
			rect := v.rowRect(row, 0, 0)
			if g.Horizontal == nil {
				x := v.origin.Dx + float64(g.VisibleColumn-1)*v.geo.charWidth
				v.canvas.DrawRect(ui.NewRectFromLTRB(x, rect.Top, x+1, rect.Bottom), fill(color))
				continue
			}
			left := float64(g.VisibleColumn-1) * v.geo.charWidth
			if g.Column >= 0 {
				left = v.geo.x(row, g.Column)
			}
			right := v.geo.x(row, g.Horizontal.EndColumn)
			if right <= left {
				continue
			}
			y := rect.Top
			if !g.Horizontal.Top {
				y = rect.Bottom - 1
			}
			v.canvas.DrawRect(ui.NewRectFromLTRB(v.origin.Dx+left, y, v.origin.Dx+right, y+1), fill(color))
		}
	}
}

// paintMatchingBrackets draws a box around each bracket that matches the one next to a cursor:
// a background and a border of 1 pixel inside it, as VS Code's `.bracket-match`.
func (v overlayView) paintMatchingBrackets() {
	for _, r := range v.geo.vm.matchedBrackets() {
		if r.Start.Line != r.End.Line {
			continue
		}
		// rectOf is in document coordinates; the canvas is clipped to the layer.
		a, b := v.geo.rectOf(r.Start), v.geo.rectOf(r.End)
		rect := ui.NewRectFromLTRB(v.origin.Dx+a.Left, v.origin.Dy+a.Top, v.origin.Dx+b.Left, v.origin.Dy+a.Bottom)
		if v.colors.BracketMatch.Alpha() > 0 {
			v.canvas.DrawRect(rect, fill(v.colors.BracketMatch))
		}
		if v.colors.BracketMatchBorder.Alpha() > 0 {
			const w = 1.0
			paint := fill(v.colors.BracketMatchBorder)
			v.canvas.DrawRect(ui.NewRectFromLTRB(rect.Left, rect.Top, rect.Right, rect.Top+w), paint)
			v.canvas.DrawRect(ui.NewRectFromLTRB(rect.Left, rect.Bottom-w, rect.Right, rect.Bottom), paint)
			v.canvas.DrawRect(ui.NewRectFromLTRB(rect.Left, rect.Top+w, rect.Left+w, rect.Bottom-w), paint)
			v.canvas.DrawRect(ui.NewRectFromLTRB(rect.Right-w, rect.Top+w, rect.Right, rect.Bottom-w), paint)
		}
	}
}

// paintSelections draws the selections of the editor, row by row: the reversed corners of
// all of them first, then the rest, as VS Code.
func (v overlayView) paintSelections() {
	vm := v.geo.vm
	opts := vm.Options()
	sels := vm.Cursors().Selections()
	color := v.colors.InactiveSelection
	if vm.Focused() {
		color = v.colors.Selection
	}
	corners := map[int][]selectionPiece{}
	rest := map[int][]selectionPiece{}
	for _, sel := range sels {
		if sel.IsEmpty() {
			continue
		}
		rows := v.geo.selectionRows(sel, v.first, v.last, opts.RoundedSelection)
		c, r := selectionPieces(rows, opts.RoundedSelection, len(sels) > 1)
		for i, row := range rows {
			corners[row.Row] = append(corners[row.Row], c[i]...)
			rest[row.Row] = append(rest[row.Row], r[i]...)
		}
	}
	for row := v.first; row <= v.last; row++ {
		for _, p := range corners[row] {
			v.paintPiece(row, p, color)
		}
		for _, p := range rest[row] {
			v.paintPiece(row, p, color)
		}
	}
}

func (v overlayView) paintPiece(row int, p selectionPiece, selection ui.Color) {
	rect := v.rowRect(row, p.Left, p.Right)
	rect = ui.NewRectFromLTRB(rect.Left, rect.Top+p.TopInset, rect.Right, rect.Bottom-p.BottomInset)
	color := selection
	if p.Background {
		color = v.colors.Background
	}
	if !p.TopLeft && !p.TopRight && !p.BottomRight && !p.BottomLeft {
		v.canvas.DrawRect(rect, fill(color))
		return
	}
	radius := func(round bool) ui.Radius {
		if round {
			return ui.NewRadiusCircular(selectionRadius)
		}
		return ui.NewRadiusCircular(0)
	}
	v.canvas.DrawRRect(ui.NewRRectFromRectAndCorners(rect, ui.Corners{
		TopLeft: radius(p.TopLeft), TopRight: radius(p.TopRight),
		BottomRight: radius(p.BottomRight), BottomLeft: radius(p.BottomLeft),
	}), fill(color))
}

// paintCursors draws the text an input method composes and the line cursors of the visible
// rows, while they are shown. While there is a preedit the primary cursor is in it, where the
// method's own cursor is.
//
// Ported from VS Code: src/vs/editor/browser/viewParts/viewCursors/viewCursor.ts (_prepareRender, line style)
// paintDropCaret draws the dotted line where dragged text would be dropped.
//
// Ported from VS Code: src/vs/editor/contrib/dnd/browser/dnd.css (.dnd-target)
func (v overlayView) paintDropCaret() {
	vm := v.geo.vm
	pos, ok := vm.DropCaret()
	if !ok {
		return
	}
	row := vm.RowOfPosition(pos)
	if row < v.first || row > v.last {
		return
	}
	rect := v.rowRect(row, v.geo.x(row, pos.Col), v.geo.x(row, pos.Col)+dropCaretWidth)
	paint := fill(v.colors.Cursor)
	for y := rect.Top; y < rect.Bottom; y += 2 * dropCaretWidth {
		v.canvas.DrawRect(ui.NewRectFromLTRB(rect.Left, y, rect.Right, min(y+dropCaretWidth, rect.Bottom)), paint)
	}
}

func (v overlayView) paintCursors() {
	vm := v.geo.vm
	v.paintDropCaret()
	preedit, composing := vm.Preedit()
	if composing {
		v.paintPreedit(preedit)
	}
	if !vm.CursorsVisible() {
		return
	}
	opacity, scale := vm.CursorFrame()
	if opacity <= 0 || scale <= 0 {
		return
	}
	for i, sel := range vm.Cursors().Selections() {
		row := vm.RowOfPosition(sel.Active)
		if row < v.first || row > v.last {
			continue
		}
		shift := 0.0
		if i == 0 && composing {
			shift = v.preeditOffset(preedit, preedit.End)
		}
		v.paintCursor(sel.Active, row, shift, opacity, scale)
	}
}

// cursorShape is the part of a cell that a cursor fills, and how it is drawn.
type cursorShape int

const (
	shapeLine cursorShape = iota
	shapeBlock
	shapeUnderline
	shapeOutline
)

// cursorStyleOf is the shape of a style of editor.cursorStyle, how thick its line is (zero for
// the width of the setting, 2 pixels by default) and whether it is the thin variant.
func cursorStyleOf(style string) (shape cursorShape, thickness float64) {
	switch style {
	case "block":
		return shapeBlock, 0
	case "underline":
		return shapeUnderline, 2
	case "underline-thin":
		return shapeUnderline, 1
	case "block-outline":
		return shapeOutline, 1
	case "line-thin":
		return shapeLine, 1
	}
	return shapeLine, 0
}

// paintCursor draws one cursor at a position in the style of the setting. In the line styles it
// is a bar between the characters; in the others it covers the character it is before (the
// width of a character at the end of a line and for a tab). Opacity and scale are the frame
// of the animated blinking: the cursor fades, or shrinks towards the middle of its height.
//
// Ported from VS Code: src/vs/editor/browser/viewParts/viewCursors/viewCursor.ts (_prepareRender) and viewCursors.css
func (v overlayView) paintCursor(pos buffer.Position, row int, shift, opacity, scale float64) {
	vm := v.geo.vm
	opts := vm.Options()
	shape, thickness := cursorStyleOf(opts.CursorStyle)
	if shift != 0 {
		shape, thickness = shapeLine, 0 // an input method composes: the cursor is its caret
	}
	cell := v.geo.rectOf(pos)
	line, _ := vm.Document().Snapshot().LineContent(pos.Line)
	grapheme := ""
	if pos.Col < len(line) {
		grapheme = line[pos.Col:edit.NextGrapheme(line, pos.Col)]
	}
	var rect ui.Rect
	switch shape {
	case shapeLine:
		width := thickness
		if width == 0 {
			width = cursorDefaultWidth
			if w := min(opts.CursorWidth, v.geo.charWidth); w > 0 {
				width = w
			}
		}
		left := cell.Left + shift
		if width >= 2 && left >= 1 {
			left-- // between the characters, not over the next one
		}
		rect = v.rowRect(row, left, left+width)
	default:
		width := cell.Right - cell.Left
		if grapheme == "\t" || width < 1 {
			width = v.geo.charWidth
		}
		rect = v.rowRect(row, cell.Left, cell.Left+width)
		if shape == shapeUnderline {
			rect.Top = rect.Bottom - thickness
		}
	}
	if scale < 1 {
		mid, half := (rect.Top+rect.Bottom)/2, (rect.Bottom-rect.Top)/2*scale
		rect.Top, rect.Bottom = mid-half, mid+half
	}
	color := v.colors.Cursor.WithOpacity(v.colors.Cursor.Opacity() * opacity)
	switch shape {
	case shapeOutline:
		v.paintOutline(rect, color)
	default:
		v.canvas.DrawRect(rect, fill(color))
	}
	// A wide bar and a block show the character they cover in the color that stands out.
	covers := shape == shapeBlock || (shape == shapeLine && thickness == 0 && rect.Right-rect.Left > 2)
	if covers && grapheme != "" && grapheme != "\t" && scale >= 1 {
		textColor := v.colors.CursorText.WithOpacity(v.colors.CursorText.Opacity() * opacity)
		p := newTextStyle(v.factory, opts, textColor).paragraph(vm.Paragraphs(), grapheme)
		v.canvas.DrawParagraph(p, ui.Offset{Dx: v.origin.Dx + cell.Left, Dy: v.origin.Dy + v.geo.proj.RowTop(row)})
	}
}

// paintOutline draws a one pixel border inside a rectangle.
func (v overlayView) paintOutline(r ui.Rect, color ui.Color) {
	paint := fill(color)
	v.canvas.DrawRect(ui.NewRectFromLTRB(r.Left, r.Top, r.Right, r.Top+1), paint)
	v.canvas.DrawRect(ui.NewRectFromLTRB(r.Left, r.Bottom-1, r.Right, r.Bottom), paint)
	v.canvas.DrawRect(ui.NewRectFromLTRB(r.Left, r.Top+1, r.Left+1, r.Bottom-1), paint)
	v.canvas.DrawRect(ui.NewRectFromLTRB(r.Right-1, r.Top+1, r.Right, r.Bottom-1), paint)
}

// preeditOffset is the width of the first n bytes of the preedit.
func (v overlayView) preeditOffset(p Preedit, n int) float64 {
	style := newTextStyle(v.factory, v.geo.vm.Options(), v.colors.Foreground)
	right := 0.0
	for _, b := range style.paragraph(v.geo.vm.Paragraphs(), p.Text).GetBoxesForRange(0, n) {
		right = max(right, b.Right)
	}
	return right
}

// paintPreedit draws the composed text over the text at the primary cursor, on the color of
// the editor so that what is under it does not show, with a line below it and a thicker one
// below the part the method highlights.
func (v overlayView) paintPreedit(p Preedit) {
	vm := v.geo.vm
	primary := vm.Cursors().Primary().Position()
	row := vm.RowOfPosition(primary)
	if row < v.first || row > v.last {
		return
	}
	left := v.geo.x(row, primary.Col)
	style := newTextStyle(v.factory, vm.Options(), v.colors.Foreground)
	para := style.paragraph(vm.Paragraphs(), p.Text)
	rect := v.rowRect(row, left, left+para.LongestLine())
	v.canvas.DrawRect(rect, fill(v.colors.Background))
	v.canvas.DrawParagraph(para, ui.Offset{Dx: rect.Left, Dy: rect.Top})
	line := fill(v.colors.Foreground)
	v.canvas.DrawRect(ui.NewRectFromLTRB(rect.Left, rect.Bottom-2, rect.Right, rect.Bottom-1), line)
	if p.End > p.Begin {
		from, to := rect.Left+v.preeditOffset(p, p.Begin), rect.Left+v.preeditOffset(p, p.End)
		v.canvas.DrawRect(ui.NewRectFromLTRB(from, rect.Bottom-3, to, rect.Bottom-1), line)
	}
}

// foldPlaceholderText is what stands for the lines a fold hides, after the line that holds it.
const foldPlaceholderText = "…"

// foldPlaceholderMargin is the room on each side of the placeholder, in ems.
const foldPlaceholderMargin = 0.2

// foldPlaceholder is the rectangle of the placeholder after the folded line on a row, in document
// coordinates.
func (g geometry) foldPlaceholder(row int) ui.Rect {
	p := g.style.paragraph(g.vm.Paragraphs(), foldPlaceholderText)
	margin := foldPlaceholderMargin * g.vm.Options().FontSize
	left := g.rowEnd(row)
	top := g.proj.RowTop(row)
	return ui.NewRectFromLTRB(left, top, left+p.LongestLine()+2*margin, top+g.proj.RowHeight(row))
}

// paintFolded gives the folded lines a background and draws the placeholder after their text.
//
// Ported from VS Code: src/vs/editor/contrib/folding/browser/folding.css
func (v overlayView) paintFolded() {
	vm := v.geo.vm
	if !vm.foldingEnabled() {
		return
	}
	proj := v.geo.proj
	style := newTextStyle(v.factory, vm.Options(), v.colors.FoldPlaceholder)
	for row := v.first; row <= v.last; row++ {
		if v.geo.isZoneRow(row) {
			continue
		}
		line, _, _ := proj.RowSpan(row)
		if !vm.FoldedLine(line) {
			continue
		}
		if vm.Options().FoldingHighlight && v.colors.FoldBackground.Alpha() > 0 {
			v.canvas.DrawRect(v.rowRect(row, vm.ScrollLeft(), vm.ScrollLeft()+v.width), fill(v.colors.FoldBackground))
		}
		if first, count := proj.LineRows(line); row != first+count-1 {
			continue // the placeholder is after the last row of a wrapped line
		}
		rect := v.geo.foldPlaceholder(row)
		margin := foldPlaceholderMargin * vm.Options().FontSize
		v.canvas.DrawParagraph(style.paragraph(vm.Paragraphs(), foldPlaceholderText),
			ui.Offset{Dx: v.origin.Dx + rect.Left + margin, Dy: v.origin.Dy + rect.Top})
	}
}
