package editor

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
)

// GutterColors are the colors of the line numbers.
type GutterColors struct {
	// LineNumber is the color of a line number.
	LineNumber ui.Color
	// ActiveLineNumber is the color of the number of the cursor's line.
	ActiveLineNumber ui.Color
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
		r.vmHandle = vm.AddListener(r.MarkNeedsPaint)
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
	r.attached = true
}

// Detach implements rendering.RenderObject.
func (r *RenderEditorGutter) Detach() {
	r.offset.RemoveListener(r.offsetHandle)
	r.vm.RemoveListener(r.vmHandle)
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

// Paint draws the numbers of the visible rows. A line that is cut into several rows has its
// number on the first one only.
func (r *RenderEditorGutter) Paint(context *rendering.PaintingContext, offset ui.Offset) {
	opts := r.vm.Options()
	if opts.LineNumbers == LineNumbersOff {
		return
	}
	size := r.Size()
	proj := r.vm.Projection()
	top := 0.0
	if r.offset.HasPixels() {
		top = r.offset.Pixels()
	}
	first, last := proj.RowAt(top), proj.RowAt(top+size.Height)
	lineCount := r.vm.Document().Snapshot().LineCount()
	cursorLine := r.vm.Cursors().Primary().Position().Line
	normal := newTextStyle(r.factory, opts, r.colors.LineNumber)
	active := newTextStyle(r.factory, opts, r.colors.ActiveLineNumber)
	widths := r.vm.GutterWidths(r.factory)

	canvas := context.Canvas()
	canvas.Save()
	canvas.ClipRect(ui.NewRectFromLTWH(offset.Dx, offset.Dy, size.Width, size.Height))
	for row := first; row <= last; row++ {
		line, start, _ := proj.RowSpan(row)
		if start != 0 {
			continue
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
	canvas.Restore()
}
