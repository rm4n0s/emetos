package editor

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// Ported from VS Code: src/vs/editor/contrib/stickyScroll/browser/stickyScrollWidget.ts and
// stickyScroll.css

// StickyColors are the colors of sticky scroll.
type StickyColors struct {
	// Background is the background of the lines, GutterBackground that of the gutter part, Hover
	// that of the line under the pointer, Border the line under the widget and Shadow its shadow.
	Background, GutterBackground, Hover, Border, Shadow ui.Color
	// LineNumber is the color of the line numbers and FoldingControl that of the folding icons.
	LineNumber, FoldingControl ui.Color
}

// stickyShadowHeight is the room under the widget for its shadow.
const stickyShadowHeight = 4.0

// stickyLayer is the widget of RenderEditorSticky.
type stickyLayer struct {
	widgets.LeafRenderObjectWidget
	vm     *ViewModel
	scroll foundation.Listenable
	colors StickyColors
	tokens *theme.TokenTheme
	// inset is the width on the left of the gutter that is not the text's: the minimap on the left.
	inset float64
}

func newStickyLayer(vm *ViewModel, scroll foundation.Listenable, colors StickyColors, tokens *theme.TokenTheme, inset float64) *stickyLayer {
	return &stickyLayer{LeafRenderObjectWidget: widgets.NewLeafRenderObjectWidgetBase(), vm: vm, scroll: scroll, colors: colors, tokens: tokens, inset: inset}
}

// CreateRenderObject implements widgets.RenderObjectWidget.
func (w *stickyLayer) CreateRenderObject(bc widgets.BuildContext) rendering.RenderObject {
	return NewRenderEditorSticky(w.vm, w.scroll, textFactory(bc), w.colors, w.tokens, w.inset)
}

// UpdateRenderObject implements widgets.RenderObjectWidget.
func (w *stickyLayer) UpdateRenderObject(bc widgets.BuildContext, ro rendering.RenderObject) {
	ro.(*RenderEditorSticky).Update(w.vm, w.scroll, textFactory(bc), w.colors, w.tokens, w.inset)
}

// RenderEditorSticky draws the lines that stick to the top of the editor: for each scope that has
// scrolled away, the line that starts it, with its line number and folding icon, over the text.
// Its height is that of its lines, and it has none when no line sticks.
type RenderEditorSticky struct {
	rendering.RenderBox
	vm      *ViewModel
	scroll  foundation.Listenable
	factory ui.ParagraphFactory
	colors  StickyColors
	tokens  *theme.TokenTheme
	inset   float64
	styles  *styleTable

	lines stickyLines

	scrollHandle, vmHandle, stHandle foundation.ListenerHandle
	attached                         bool
}

// NewRenderEditorSticky makes the sticky lines of a view model.
func NewRenderEditorSticky(vm *ViewModel, scroll foundation.Listenable, factory ui.ParagraphFactory, colors StickyColors, tokens *theme.TokenTheme, inset float64) *RenderEditorSticky {
	r := &RenderEditorSticky{vm: vm, scroll: scroll, factory: factory, colors: colors, tokens: tokens, inset: inset}
	rendering.InitRenderBox(&r.RenderBox, r)
	return r
}

// Update copies the configuration of a widget.
func (r *RenderEditorSticky) Update(vm *ViewModel, scroll foundation.Listenable, factory ui.ParagraphFactory, colors StickyColors, tokens *theme.TokenTheme, inset float64) {
	if r.attached {
		if vm != r.vm {
			r.unlisten()
			r.vm = vm
			r.listen()
		}
		if scroll != r.scroll {
			r.scroll.RemoveListener(r.scrollHandle)
			r.scroll = scroll
			r.scrollHandle = scroll.AddListener(r.MarkNeedsLayout)
		}
	}
	r.vm, r.scroll, r.factory, r.colors, r.inset = vm, scroll, factory, colors, inset
	if tokens != r.tokens {
		r.tokens, r.styles = tokens, nil
	}
	r.MarkNeedsLayout()
}

func (r *RenderEditorSticky) listen() {
	r.vmHandle = r.vm.AddListener(r.MarkNeedsLayout)
	r.stHandle = r.vm.sticky.changed.AddListener(r.MarkNeedsPaint)
}

func (r *RenderEditorSticky) unlisten() {
	r.vm.RemoveListener(r.vmHandle)
	r.vm.sticky.changed.RemoveListener(r.stHandle)
}

// Attach implements rendering.RenderObject.
func (r *RenderEditorSticky) Attach(owner *rendering.PipelineOwner) {
	r.RenderBox.Attach(owner)
	r.scrollHandle = r.scroll.AddListener(r.MarkNeedsLayout)
	r.listen()
	r.attached = true
}

// Detach implements rendering.RenderObject.
func (r *RenderEditorSticky) Detach() {
	r.scroll.RemoveListener(r.scrollHandle)
	r.unlisten()
	r.attached = false
	r.RenderBox.Detach()
}

// IsRepaintBoundary implements rendering.RenderObject.
func (r *RenderEditorSticky) IsRepaintBoundary() bool { return true }

// ComputeMinIntrinsicWidth implements rendering.RenderBox.
func (r *RenderEditorSticky) ComputeMinIntrinsicWidth(float64) float64 { return 0 }

// ComputeMaxIntrinsicWidth implements rendering.RenderBox.
func (r *RenderEditorSticky) ComputeMaxIntrinsicWidth(float64) float64 { return 0 }

// ComputeMinIntrinsicHeight implements rendering.RenderBox.
func (r *RenderEditorSticky) ComputeMinIntrinsicHeight(float64) float64 { return 0 }

// ComputeMaxIntrinsicHeight implements rendering.RenderBox.
func (r *RenderEditorSticky) ComputeMaxIntrinsicHeight(float64) float64 { return 0 }

// stickyHeights is the height of the lines and with the room for the shadow, for the lines that stick.
func (r *RenderEditorSticky) heights(l stickyLines) (content, total float64) {
	content = l.Height(r.vm.Options().LineHeight)
	if content <= 0 {
		return 0, 0
	}
	return content, content + stickyShadowHeight
}

// ComputeDryLayout implements rendering.RenderBox.
func (r *RenderEditorSticky) ComputeDryLayout(c rendering.BoxConstraints) ui.Size {
	_, total := r.heights(r.vm.stickyLinesAt(r.vm.ScrollTop()))
	return c.Constrain(ui.Size{Width: c.MaxWidth, Height: total})
}

// PerformLayout implements rendering.RenderBox: the lines that stick at the scroll position decide
// the height.
func (r *RenderEditorSticky) PerformLayout() {
	r.lines = r.vm.stickyLinesAt(r.vm.ScrollTop())
	_, total := r.heights(r.lines)
	r.SetSize(r.BoxConstraints().Constrain(ui.Size{Width: r.BoxConstraints().MaxWidth, Height: total}))
}

// HitTestSelf implements rendering.RenderBox: the lines take the pointer, the room of the shadow
// does not.
func (r *RenderEditorSticky) HitTestSelf(p ui.Offset) bool {
	content, _ := r.heights(r.lines)
	return p.Dy >= 0 && p.Dy < content
}

// widths are the widths of the parts of the gutter.
func (r *RenderEditorSticky) widths() GutterWidths { return r.vm.GutterWidths(r.factory) }

// contentLeft is where the text of the lines starts.
func (r *RenderEditorSticky) contentLeft() float64 { return r.inset + r.widths().Total() }

// Paint draws the lines.
func (r *RenderEditorSticky) Paint(context *rendering.PaintingContext, offset ui.Offset) {
	vm := r.vm
	n := len(r.lines.Lines)
	content, _ := r.heights(r.lines)
	if n == 0 || content <= 0 {
		return
	}
	width := r.Size().Width
	rowHeight := vm.Options().LineHeight
	canvas := context.Canvas()
	canvas.Save()
	canvas.ClipRect(ui.NewRectFromLTWH(offset.Dx, offset.Dy, width, content))
	// the last line goes under the others, so that it slides in under them as its scope ends
	order := make([]int, 0, n)
	order = append(order, n-1)
	for i := range n - 1 {
		order = append(order, i)
	}
	for _, i := range order {
		y := float64(i) * rowHeight
		if i == n-1 {
			y += r.lines.Offset
		}
		r.paintLine(canvas, offset, i, y, width, rowHeight)
	}
	canvas.Restore()
	r.paintFrame(canvas, offset, width, content)
}

// paintLine draws a sticky line: the gutter with its number and icon, and the text.
func (r *RenderEditorSticky) paintLine(canvas *ui.Canvas, offset ui.Offset, i int, y, width, rowHeight float64) {
	vm := r.vm
	opts := vm.Options()
	line := r.lines.Lines[i]
	w := r.widths()
	left := r.contentLeft()
	row := ui.NewRectFromLTWH(offset.Dx, offset.Dy+y, width, rowHeight)

	background := r.colors.Background
	if vm.sticky.hover && vm.sticky.line == i {
		background = r.colors.Hover
	}
	canvas.DrawRect(ui.NewRectFromLTRB(row.Left+left, row.Top, row.Right, row.Bottom), fill(background))
	canvas.DrawRect(ui.NewRectFromLTRB(row.Left, row.Top, row.Left+left, row.Bottom), fill(r.colors.GutterBackground))

	// the text, from the horizontal scroll of the editor when the setting says so
	text, err := vm.doc.Snapshot().LineContent(line)
	if err == nil && text != "" {
		x := row.Left + left
		if opts.StickyScroll.ScrollWithEditor {
			x -= vm.ScrollLeft()
		}
		canvas.Save()
		canvas.ClipRect(ui.NewRectFromLTRB(row.Left+left, row.Top, row.Right, row.Bottom))
		style := newTextStyle(r.factory, opts, r.colors.LineNumber)
		p := style.styledParagraph(vm.Paragraphs(), text, r.spans(line, len(text)))
		canvas.DrawParagraph(p, ui.Offset{Dx: x, Dy: row.Top})
		canvas.Restore()
	}

	if opts.LineNumbers != LineNumbersOff {
		number, _ := LineNumberText(LineNumbersOn, line, -1, vm.doc.Snapshot().LineCount())
		p := newTextStyle(r.factory, opts, r.colors.LineNumber).paragraph(vm.Paragraphs(), number)
		canvas.DrawParagraph(p, ui.Offset{Dx: row.Left + r.inset + w.GlyphMargin + w.LineNumbers - p.LongestLine(), Dy: row.Top})
	}
	r.paintFoldingIcon(canvas, row, line, w)
}

// paintFoldingIcon draws the chevron of a sticky line: down for an open scope, right for a folded one;
// the open ones show while the pointer is over the widget, or always with editor.showFoldingControls "always".
func (r *RenderEditorSticky) paintFoldingIcon(canvas *ui.Canvas, row ui.Rect, line int, w GutterWidths) {
	vm := r.vm
	opts := vm.Options()
	if !opts.Folding || opts.ShowFoldingControls == "never" || r.colors.FoldingControl.Alpha() == 0 {
		return
	}
	var glyph int
	switch vm.FoldingStateOf(line) {
	case FoldingCollapsed:
		glyph = fonts.CodiconChevronRight
	case FoldingExpanded:
		if opts.ShowFoldingControls != "always" && !vm.sticky.hover {
			return
		}
		glyph = fonts.CodiconChevronDown
	default:
		return
	}
	icons := opts
	icons.FontFamilies, icons.FontSize, icons.FontWeight, icons.LetterSpacing, icons.FontFeatures = []string{fonts.Codicon}, foldingControlsGlyph, 0, 0, nil
	p := newTextStyle(r.factory, icons, r.colors.FoldingControl).paragraph(vm.Paragraphs(), string(rune(glyph)))
	x := row.Left + r.inset + w.GlyphMargin + w.LineNumbers + (foldingControlsWidth-p.LongestLine())/2
	canvas.DrawParagraph(p, ui.Offset{Dx: x, Dy: row.Top})
}

// foldingControlsGlyph is the size of the folding icon of a sticky line, as in the gutter.
const foldingControlsGlyph = foldingControlGlyphSize

// paintFrame draws the border and the shadow under the lines.
func (r *RenderEditorSticky) paintFrame(canvas *ui.Canvas, offset ui.Offset, width, content float64) {
	bottom := offset.Dy + content
	if r.colors.Border.Alpha() > 0 {
		canvas.DrawRect(ui.NewRectFromLTRB(offset.Dx, bottom-1, offset.Dx+width, bottom), fill(r.colors.Border))
	}
	shadow := r.colors.Shadow
	if shadow.Alpha() == 0 {
		return
	}
	paint := ui.NewPaint()
	rect := ui.NewRectFromLTWH(offset.Dx, bottom, width, stickyShadowHeight)
	paint.SetShader(painting.NewLinearGradient([]ui.Color{shadow.WithAlpha(shadow.Alpha() * 6 / 10), shadow.WithAlpha(0)}, painting.LinearGradientOpts{
		Begin: painting.AlignmentTopCenter, End: painting.AlignmentBottomCenter,
	}).CreateShader(rect, ui.TextDirectionLTR))
	canvas.DrawRect(rect, paint)
}

// spans are the colors of the tokens of a line.
func (r *RenderEditorSticky) spans(line, length int) []span {
	tok := r.vm.Tokenizer()
	if tok == nil || r.tokens == nil {
		return nil
	}
	lineTokens := tok.Line(line)
	if lineTokens == nil {
		return nil
	}
	if r.styles == nil || r.styles.scopes != tok.Scopes() {
		r.styles = newStyleTable(r.tokens, tok.Scopes())
	}
	return r.styles.spans(lineTokens, 0, length)
}
