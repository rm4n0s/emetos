package editor

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
)

// GutterColors are the colors of the line numbers.
type GutterColors struct {
	// LineNumber is the color of a line number.
	LineNumber ui.Color
	// ActiveLineNumber is the color of the number of the cursor's line.
	ActiveLineNumber ui.Color
	// LineHighlight and LineHighlightBorder are the background and the border of the current
	// line when the editor highlights it in the gutter.
	LineHighlight, LineHighlightBorder ui.Color
	// FoldingControl is the color of the folding controls.
	FoldingControl ui.Color
}

// GutterWidths gives the widths of the gutter for the document of the view model, in the
// font of its options. The width of a digit is measured through the paragraph cache.
func (vm *ViewModel) GutterWidths(factory ui.ParagraphFactory) GutterWidths {
	digit := 0.0
	if vm.Options().LineNumbers != LineNumbersOff {
		digit = newTextStyle(factory, vm.Options(), ui.NewColor(0)).digitWidth(vm.Paragraphs())
	}
	return ComputeGutterWidths(vm.Options().gutterOptions(digit), vm.Document().Snapshot().LineCount())
}

// RenderEditorGutter is the gutter of the editor: the line numbers of the visible rows, in
// the same offset as the text. It is a repaint boundary of its own.
type RenderEditorGutter struct {
	rendering.RenderBox
	vm      *ViewModel
	offset  rendering.ViewportOffset
	factory ui.ParagraphFactory
	colors  GutterColors

	offsetHandle foundation.ListenerHandle
	vmHandle     foundation.ListenerHandle
	selHandle    foundation.ListenerHandle
	attached     bool
}

// NewRenderEditorGutter makes the gutter of a view model.
func NewRenderEditorGutter(vm *ViewModel, offset rendering.ViewportOffset, factory ui.ParagraphFactory, colors GutterColors) *RenderEditorGutter {
	r := &RenderEditorGutter{vm: vm, offset: offset, factory: factory, colors: colors}
	rendering.InitRenderBox(&r.RenderBox, r)
	return r
}

// Update copies the configuration of a widget.
func (r *RenderEditorGutter) Update(vm *ViewModel, offset rendering.ViewportOffset, factory ui.ParagraphFactory, colors GutterColors) {
	if r.attached && vm != r.vm {
		r.vm.RemoveListener(r.vmHandle)
		r.vm.RemoveSelectionListener(r.selHandle)
		r.vmHandle = vm.AddListener(r.MarkNeedsPaint)
		r.selHandle = vm.AddSelectionListener(r.MarkNeedsPaint)
	}
	if r.attached && offset != r.offset {
		r.offset.RemoveListener(r.offsetHandle)
		r.offsetHandle = offset.AddListener(r.MarkNeedsPaint)
	}
	r.vm, r.offset, r.factory, r.colors = vm, offset, factory, colors
	r.MarkNeedsPaint()
}

// Attach implements rendering.RenderObject.
func (r *RenderEditorGutter) Attach(owner *rendering.PipelineOwner) {
	r.RenderBox.Attach(owner)
	r.offsetHandle = r.offset.AddListener(r.MarkNeedsPaint)
	r.vmHandle = r.vm.AddListener(r.MarkNeedsPaint)
	r.selHandle = r.vm.AddSelectionListener(r.MarkNeedsPaint)
	r.attached = true
}

// Detach implements rendering.RenderObject.
func (r *RenderEditorGutter) Detach() {
	r.offset.RemoveListener(r.offsetHandle)
	r.vm.RemoveListener(r.vmHandle)
	r.vm.RemoveSelectionListener(r.selHandle)
	r.attached = false
	r.RenderBox.Detach()
}

// SizedByParent implements rendering.RenderObject.
func (r *RenderEditorGutter) SizedByParent() bool { return true }

// IsRepaintBoundary implements rendering.RenderObject.
func (r *RenderEditorGutter) IsRepaintBoundary() bool { return true }

// ComputeMinIntrinsicWidth implements rendering.RenderBox.
func (r *RenderEditorGutter) ComputeMinIntrinsicWidth(float64) float64 { return 0 }

// ComputeMaxIntrinsicWidth implements rendering.RenderBox.
func (r *RenderEditorGutter) ComputeMaxIntrinsicWidth(float64) float64 { return 0 }

// ComputeMinIntrinsicHeight implements rendering.RenderBox.
func (r *RenderEditorGutter) ComputeMinIntrinsicHeight(float64) float64 { return 0 }

// ComputeMaxIntrinsicHeight implements rendering.RenderBox.
func (r *RenderEditorGutter) ComputeMaxIntrinsicHeight(float64) float64 { return 0 }

// ComputeDryLayout implements rendering.RenderBox.
func (r *RenderEditorGutter) ComputeDryLayout(c rendering.BoxConstraints) ui.Size {
	return c.Constrain(c.Biggest())
}

// PerformLayout implements rendering.RenderBox; the gutter has nothing to lay out.
func (r *RenderEditorGutter) PerformLayout() {}

// Paint draws the highlight of the current line and the numbers of the visible rows. A line
// that is cut into several rows has its number on the first one only.
func (r *RenderEditorGutter) Paint(context *rendering.PaintingContext, offset ui.Offset) {
	opts := r.vm.Options()
	size := r.Size()
	proj := r.vm.Projection()
	top := 0.0
	if r.offset.HasPixels() {
		top = r.offset.Pixels()
	}
	first, last := proj.RowAt(top), proj.RowAt(top+size.Height)

	canvas := context.Canvas()
	canvas.Save()
	canvas.ClipRect(ui.NewRectFromLTWH(offset.Dx, offset.Dy, size.Width, size.Height))
	defer canvas.Restore()
	r.paintMarkGutter(canvas, offset, size, top, first, last)
	r.paintZoneGutter(canvas, offset, size, top, first, last)
	r.paintHighlight(canvas, offset, size, top, first, last)
	widths := r.vm.GutterWidths(r.factory)
	if opts.LineNumbers != LineNumbersOff {
		r.paintNumbers(canvas, offset, top, first, last, widths)
	}
	r.paintFoldingControls(canvas, offset, top, first, last, widths)
	r.paintZoneGlyphs(canvas, offset, top, first, last, widths)
}

// paintNumbers draws the line numbers of the visible rows.
func (r *RenderEditorGutter) paintNumbers(canvas *ui.Canvas, offset ui.Offset, top float64, first, last int, widths GutterWidths) {
	opts := r.vm.Options()
	proj := r.vm.Projection()
	lineCount := r.vm.Document().Snapshot().LineCount()
	cursorLine := r.vm.Cursors().Primary().Position().Line
	normal := newTextStyle(r.factory, opts, r.colors.LineNumber)
	active := newTextStyle(r.factory, opts, r.colors.ActiveLineNumber)
	for row := first; row <= last; row++ {
		line, start, _ := proj.RowSpan(row)
		if _, _, zone := proj.RowZone(row); start != 0 || zone {
			continue // a continuation of a wrapped line, or a row of a view zone: no number, no control
		}
		text, current := LineNumberText(opts.LineNumbers, line, cursorLine, lineCount)
		if text == "" {
			continue
		}
		style := normal
		if line == cursorLine {
			style = active
		}
		p := style.paragraph(r.vm.Paragraphs(), text)
		x := offset.Dx + widths.GlyphMargin + widths.LineNumbers - p.LongestLine()
		if current {
			x = offset.Dx + widths.GlyphMargin // the cursor's line in relative mode is left-aligned
		}
		canvas.DrawParagraph(p, ui.Offset{Dx: x, Dy: offset.Dy + proj.RowTop(row) - top})
	}
}

// foldingControlGlyphSize is the size of the folding control's icon.
const foldingControlGlyphSize = 16

// paintFoldingControls draws the chevron of each visible line that starts a region: down for an open
// region, right for a folded one. With editor.showFoldingControls "mouseover" the open ones show
// only while the pointer is over the gutter; the folded ones always show.
//
// Ported from VS Code: src/vs/editor/contrib/folding/browser/foldingDecorations.ts
func (r *RenderEditorGutter) paintFoldingControls(canvas *ui.Canvas, offset ui.Offset, top float64, first, last int, widths GutterWidths) {
	opts := r.vm.Options()
	if !opts.Folding || opts.ShowFoldingControls == "never" || r.colors.FoldingControl.Alpha() == 0 {
		return
	}
	showOpen := opts.ShowFoldingControls == "always" || r.vm.fold.hoverGutter
	proj := r.vm.Projection()
	icons := opts
	icons.FontFamilies, icons.FontSize, icons.FontWeight, icons.LetterSpacing, icons.FontFeatures = []string{fonts.Codicon}, foldingControlGlyphSize, 0, 0, nil
	style := newTextStyle(r.factory, icons, r.colors.FoldingControl)
	left := offset.Dx + widths.GlyphMargin + widths.LineNumbers
	for row := first; row <= last; row++ {
		line, start, _ := proj.RowSpan(row)
		if _, _, zone := proj.RowZone(row); start != 0 || zone {
			continue // a continuation of a wrapped line, or a row of a view zone: no number, no control
		}
		var glyph int
		switch r.vm.FoldingStateOf(line) {
		case FoldingCollapsed:
			glyph = fonts.CodiconChevronRight
		case FoldingExpanded:
			if !showOpen {
				continue
			}
			glyph = fonts.CodiconChevronDown
		default:
			continue
		}
		p := style.paragraph(r.vm.Paragraphs(), string(rune(glyph)))
		x := left + (foldingControlsWidth-p.LongestLine())/2
		canvas.DrawParagraph(p, ui.Offset{Dx: x, Dy: offset.Dy + proj.RowTop(row) - top})
	}
}

// paintHighlight draws the current line across the gutter when the editor highlights it
// there. When the text has the highlight too the gutter has no border on the side of the
// text, as in VS Code.
//
// Ported from VS Code: src/vs/editor/browser/viewParts/currentLineHighlight/currentLineHighlight.ts (CurrentLineMarginHighlightOverlay)
func (r *RenderEditorGutter) paintHighlight(canvas *ui.Canvas, offset ui.Offset, size ui.Size, top float64, first, last int) {
	opts := r.vm.Options()
	focused := r.vm.Focused()
	if !highlightInMargin(opts, focused) {
		return
	}
	both := highlightInContent(opts, focused, allEmpty(r.vm.Cursors().Selections()))
	proj := r.vm.Projection()
	for _, row := range cursorRows(r.vm) {
		if row < first || row > last {
			continue
		}
		y := offset.Dy + proj.RowTop(row) - top
		rect := ui.NewRectFromLTWH(offset.Dx, y, size.Width, proj.RowHeight(row))
		paintLineHighlight(canvas, rect, r.colors.LineHighlight, r.colors.LineHighlightBorder,
			lineHighlightEdges{Left: true, Top: true, Bottom: true, Right: !both})
	}
}
