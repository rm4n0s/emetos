package editor

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/theme"
)

// rowMargin is how many rows beyond the visible ones layout prepares paragraphs for, so that
// a scroll by a few rows finds them built.
const rowMargin = 10

// RenderEditorText is the text layer of the editor (SPEC §9.1): a leaf render box driven by
// the vertical offset of a Scrollable and the horizontal offset of the view model. It lays
// out and paints only the visible rows plus a margin, one cached paragraph per row, and is a
// repaint boundary, so that Glutter's raster cache keeps the text when only a cursor moves.
type RenderEditorText struct {
	rendering.RenderBox
	vm      *ViewModel
	offset  rendering.ViewportOffset
	factory ui.ParagraphFactory
	color   ui.Color
	// tokenTheme colors the tokens of the document; nil draws all the text in color.
	tokenTheme *theme.TokenTheme
	styles     *styleTable
	// bracketColors color the brackets by nesting level (editor.bracketPairColorization).
	bracketColors BracketColors

	offsetHandle foundation.ListenerHandle
	vmHandle     foundation.ListenerHandle
	attached     bool
}

// NewRenderEditorText makes the render object for a view model. The paragraphs are made by
// factory (the platform's text engine) in color.
func NewRenderEditorText(vm *ViewModel, offset rendering.ViewportOffset, factory ui.ParagraphFactory, color ui.Color) *RenderEditorText {
	r := &RenderEditorText{vm: vm, offset: offset, factory: factory, color: color}
	rendering.InitRenderBox(&r.RenderBox, r)
	return r
}

// SetViewModel changes the view model.
func (r *RenderEditorText) SetViewModel(vm *ViewModel) {
	if vm == r.vm {
		return
	}
	if r.attached {
		r.vm.RemoveListener(r.vmHandle)
		r.vmHandle = vm.AddListener(r.onViewModel)
	}
	r.vm = vm
	r.MarkNeedsLayout()
}

// SetOffset changes the vertical offset.
func (r *RenderEditorText) SetOffset(offset rendering.ViewportOffset) {
	if offset == r.offset {
		return
	}
	if r.attached {
		r.offset.RemoveListener(r.offsetHandle)
		r.offsetHandle = offset.AddListener(r.MarkNeedsPaint)
	}
	r.offset = offset
	r.MarkNeedsLayout()
}

// SetColor changes the color of the text.
func (r *RenderEditorText) SetColor(color ui.Color) {
	if color == r.color {
		return
	}
	r.color = color
	r.MarkNeedsPaint()
}

// SetTokenTheme changes the theme that colors the tokens; nil makes the text one color.
func (r *RenderEditorText) SetTokenTheme(t *theme.TokenTheme) {
	if t == r.tokenTheme {
		return
	}
	r.tokenTheme = t
	r.styles = nil
	r.MarkNeedsPaint()
}

// SetBracketColors changes the colors of the brackets.
func (r *RenderEditorText) SetBracketColors(c BracketColors) {
	if c == r.bracketColors {
		return
	}
	r.bracketColors = c
	r.MarkNeedsPaint()
}

// SetFactory changes the text engine.
func (r *RenderEditorText) SetFactory(factory ui.ParagraphFactory) {
	if factory == r.factory {
		return
	}
	r.factory = factory
	r.MarkNeedsLayout()
}

// onViewModel reacts to a change of the view model: the document or the options changed, so
// the rows may be others, and so may the scroll extent.
func (r *RenderEditorText) onViewModel() {
	r.MarkNeedsLayout()
	r.MarkNeedsPaint()
}

// Attach implements rendering.RenderObject.
func (r *RenderEditorText) Attach(owner *rendering.PipelineOwner) {
	r.RenderBox.Attach(owner)
	r.offsetHandle = r.offset.AddListener(r.MarkNeedsPaint)
	r.vmHandle = r.vm.AddListener(r.onViewModel)
	r.attached = true
}

// Detach implements rendering.RenderObject.
func (r *RenderEditorText) Detach() {
	r.offset.RemoveListener(r.offsetHandle)
	r.vm.RemoveListener(r.vmHandle)
	r.attached = false
	r.RenderBox.Detach()
}

// SizedByParent implements rendering.RenderObject: the editor fills the space it is given.
func (r *RenderEditorText) SizedByParent() bool { return true }

// IsRepaintBoundary implements rendering.RenderObject.
func (r *RenderEditorText) IsRepaintBoundary() bool { return true }

// HitTestSelf implements rendering.RenderBox: every point of the editor is the editor's.
func (r *RenderEditorText) HitTestSelf(ui.Offset) bool { return true }

// ComputeMinIntrinsicWidth implements rendering.RenderBox.
func (r *RenderEditorText) ComputeMinIntrinsicWidth(float64) float64 { return 0 }

// ComputeMaxIntrinsicWidth implements rendering.RenderBox.
func (r *RenderEditorText) ComputeMaxIntrinsicWidth(float64) float64 { return 0 }

// ComputeMinIntrinsicHeight implements rendering.RenderBox.
func (r *RenderEditorText) ComputeMinIntrinsicHeight(float64) float64 { return 0 }

// ComputeMaxIntrinsicHeight implements rendering.RenderBox.
func (r *RenderEditorText) ComputeMaxIntrinsicHeight(float64) float64 { return 0 }

// ComputeDryLayout implements rendering.RenderBox: the biggest size the constraints allow.
func (r *RenderEditorText) ComputeDryLayout(c rendering.BoxConstraints) ui.Size {
	return c.Constrain(c.Biggest())
}

// style is the look of the text now.
func (r *RenderEditorText) style() textStyle {
	return newTextStyle(r.factory, r.vm.Options(), r.color)
}

// scrollExtent is how far the content can scroll: with scrollBeyondLastLine the last line
// can reach the top of the editor, otherwise the last line stays at the bottom.
func scrollExtent(contentHeight, viewport, lineHeight float64, beyond bool) float64 {
	if beyond {
		return max(contentHeight-lineHeight, 0)
	}
	return max(contentHeight-viewport, 0)
}

// PerformLayout tells the offset the size of the viewport and of the content, and prepares
// the paragraphs of the visible rows and the margin around them.
func (r *RenderEditorText) PerformLayout() {
	size := r.Size()
	proj := r.vm.Projection()
	opts := r.vm.Options()
	extent := scrollExtent(proj.Height(), size.Height, opts.LineHeight, opts.ScrollBeyondLastLine)
	r.offset.ApplyViewportDimension(size.Height)
	r.offset.ApplyContentDimensions(0, extent)

	top := r.top()
	first, last := proj.RowAt(top), proj.RowAt(top+size.Height)
	style := r.style()
	snap := r.vm.Document().Snapshot()
	r.prepareTokens(proj, first, last)
	widest := 0.0
	for row := max(first-rowMargin, 0); row <= min(last+rowMargin, proj.RowCount()-1); row++ {
		if p := r.rowParagraph(style, proj, snap.LineContent, row); p != nil {
			widest = max(widest, p.LongestLine())
		}
	}
	charWidth := style.paragraph(r.vm.Paragraphs(), "0").LongestLine()
	r.vm.SetTextMetrics(size.Width, size.Height, extent, widest, charWidth)
}

// top is the vertical scroll position.
func (r *RenderEditorText) top() float64 {
	if r.offset.HasPixels() {
		return r.offset.Pixels()
	}
	return r.vm.ScrollTop()
}

// rowParagraph returns the paragraph of a row, or nil for a row with no text.
func (r *RenderEditorText) rowParagraph(style textStyle, proj Projection, lineContent func(int) (string, error), row int) *ui.Paragraph {
	line, start, end := proj.RowSpan(row)
	text, err := lineContent(line)
	if err != nil {
		return nil
	}
	if end < 0 || end > len(text) {
		end = len(text)
	}
	start = min(start, end)
	text = text[start:end]
	if text == "" {
		return nil
	}
	spans := overlaySpans(r.rowSpans(line, start, end), r.vm.bracketSpans(r.bracketColors, line, start, end))
	return style.styledParagraph(r.vm.Paragraphs(), text, spans)
}

// prepareTokens tokenizes, as far as there is time, up to the last line on the screen, so that
// the lines about to be drawn have their colors, and tells the view model which lines those are.
func (r *RenderEditorText) prepareTokens(proj Projection, first, last int) {
	if r.vm.Tokenizer() == nil {
		return
	}
	firstLine, _, _ := proj.RowSpan(max(first, 0))
	lastLine, _, _ := proj.RowSpan(min(max(last, 0), proj.RowCount()-1))
	r.vm.ensureTokens(firstLine, lastLine)
}

// rowSpans are the colored parts of the bytes [start, end) of a line, or nil when the line has
// no tokens yet or there is no token theme.
func (r *RenderEditorText) rowSpans(line, start, end int) []span {
	tok := r.vm.Tokenizer()
	if tok == nil || r.tokenTheme == nil {
		return nil
	}
	lineTokens := tok.Line(line)
	if lineTokens == nil {
		return nil
	}
	if r.styles == nil || r.styles.scopes != tok.Scopes() {
		r.styles = newStyleTable(r.tokenTheme, tok.Scopes())
	}
	return r.styles.spans(lineTokens, start, end)
}

// Paint draws the visible rows.
func (r *RenderEditorText) Paint(context *rendering.PaintingContext, offset ui.Offset) {
	size := r.Size()
	proj := r.vm.Projection()
	top := r.top()
	first, last := proj.RowAt(top), proj.RowAt(top+size.Height)
	style := r.style()
	snap := r.vm.Document().Snapshot()
	r.prepareTokens(proj, first, last)

	canvas := context.Canvas()
	canvas.Save()
	canvas.ClipRect(ui.NewRectFromLTWH(offset.Dx, offset.Dy, size.Width, size.Height))
	left := r.vm.ScrollLeft()
	for row := first; row <= last; row++ {
		if p := r.rowParagraph(style, proj, snap.LineContent, row); p != nil {
			canvas.DrawParagraph(p, ui.Offset{Dx: offset.Dx - left, Dy: offset.Dy + proj.RowTop(row) - top})
		}
	}
	canvas.Restore()
}

// PositionAt is the position of the document nearest to a point of the text layer, given in
// the layer's own coordinates: the row from the projection, then the column from the
// paragraph of the row, or from the width of a character for plain ASCII in a monospace font.
// A point above or below the rows is the first or last row, and one past the end of a line is
// its end.
func (r *RenderEditorText) PositionAt(local ui.Offset) buffer.Position {
	return newGeometry(r.vm, r.factory).positionAt(local.Dx+r.vm.ScrollLeft(), local.Dy+r.top())
}

// RectOf is the rectangle of the character at a position, in the layer's own coordinates: as
// high as the row and as wide as the character, or one character at the end of a line. It can
// lie outside the layer when the position is scrolled out of view.
func (r *RenderEditorText) RectOf(p buffer.Position) ui.Rect {
	return newGeometry(r.vm, r.factory).rectOf(p).Shift(ui.Offset{Dx: -r.vm.ScrollLeft(), Dy: -r.top()})
}

// VisualColumnAt is the visual column nearest to a point of the text layer, given in the
// layer's own coordinates, counted in characters of the font and also past the end of a line.
func (r *RenderEditorText) VisualColumnAt(local ui.Offset) int {
	return newGeometry(r.vm, r.factory).visualColumnAt(local.Dx + r.vm.ScrollLeft())
}

// CharWidth is the width of a character of the font.
func (r *RenderEditorText) CharWidth() float64 {
	return newGeometry(r.vm, r.factory).charWidth
}
