package mergeeditor

import (
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// Ported from VS Code: src/vs/workbench/contrib/mergeEditor/browser/view/editors/inputCodeEditorView.ts
// (MergeConflictGutterItemView) and the `.merge-accept-gutter-marker` rules of view/media/mergeEditor.css

// GutterWidth is the width of the gutter of an input, which holds the checkboxes of its conflicts.
const GutterWidth = 24.0

// Metrics of the checkbox and of the bracket along a tall conflict.
const (
	checkboxLeft   = 3.0
	checkboxRadius = 3.0
	bracketLeft    = 12.0
	bracketRight   = 22.0
	bracketWidth   = 2.0
)

// gutterBox is the rectangle of the checkbox of an item, in the gutter's coordinates.
func gutterBox(it GutterItem, viewHeight float64) ui.Rect {
	y := CheckboxTop(it.Top, it.Height, 0, viewHeight)
	return ui.NewRectFromLTWH(checkboxLeft, y, CheckboxSize, CheckboxSize)
}

// borderColor is the color of the border of a checkbox and of the bracket by the state of its conflict (VS Code's
// `.merge-accept-gutter-marker` rules): the conflict colors while it is not handled, the checkbox border once it is.
func borderColor(c kit.Colors, it GutterItem) ui.Color {
	switch {
	case !it.Handled && it.Focused:
		return c.MergeUnhandledFocusedBorder
	case !it.Handled:
		return c.MergeUnhandledUnfocusedBorder
	case it.Focused:
		return c.MergeHandledFocusedBorder
	}
	return c.CheckboxBorder
}

// gutterPainter draws the checkboxes of an input.
type gutterPainter struct {
	rendering.CustomPainterBase
	model  *Model
	side   Side
	colors kit.Colors
}

func newGutterPainter(model *Model, side Side, colors kit.Colors) *gutterPainter {
	return &gutterPainter{CustomPainterBase: rendering.NewCustomPainterBase(model.repaint), model: model, side: side, colors: colors}
}

func fillPaint(c ui.Color) *ui.Paint {
	p := ui.NewPaint()
	p.SetColor(c)
	return p
}

func strokePaint(c ui.Color, width float64) *ui.Paint {
	p := ui.NewPaint()
	p.SetColor(c)
	p.SetStyle(ui.PaintingStyleStroke)
	p.SetStrokeWidth(width)
	return p
}

// Paint implements rendering.CustomPainter.
func (p *gutterPainter) Paint(canvas *ui.Canvas, size ui.Size) {
	canvas.Save()
	defer canvas.Restore()
	canvas.ClipRect(ui.NewRectFromLTWH(0, 0, size.Width, size.Height))
	c := p.colors
	for _, it := range p.model.GutterItems(p.side) {
		if it.Top+it.Height < 0 || it.Top > size.Height {
			continue
		}
		border := borderColor(c, it)
		if it.Height > MultiLineHeight {
			// a bracket along the lines of the conflict, open on the right
			w := bracketWidth
			canvas.DrawRect(ui.NewRectFromLTWH(bracketLeft, it.Top, bracketRight-bracketLeft, w), fillPaint(border))
			canvas.DrawRect(ui.NewRectFromLTWH(bracketLeft, it.Top+it.Height-w, bracketRight-bracketLeft, w), fillPaint(border))
			canvas.DrawRect(ui.NewRectFromLTWH(bracketLeft, it.Top, w, it.Height), fillPaint(border))
		}
		box := gutterBox(it, size.Height)
		rr := ui.NewRRectFromRectAndRadius(box, ui.NewRadiusCircular(checkboxRadius))
		canvas.DrawRRect(rr, fillPaint(c.EditorBackground))
		canvas.DrawRRect(rr, fillPaint(c.CheckboxBackground))
		canvas.DrawRRect(ui.NewRRectFromRectAndRadius(box.Deflate(0.5), ui.NewRadiusCircular(checkboxRadius)), strokePaint(border, 1))
		p.drawCheck(canvas, box, it.Check)
	}
}

// drawCheck draws what the checkbox shows: a check mark, two for the second of two inputs, a dot for a result that was
// edited, nothing when the result does not have the lines of the input.
func (p *gutterPainter) drawCheck(canvas *ui.Canvas, box ui.Rect, check ChecksOf) {
	fg := p.colors.CheckboxForeground
	mark := func(dx float64) {
		path := ui.NewPath()
		path.MoveTo(box.Left+4.5+dx, box.Top+9.5)
		path.LineTo(box.Left+7.5+dx, box.Top+12.5)
		path.LineTo(box.Left+13.5+dx, box.Top+5.5)
		canvas.DrawPath(path, strokePaint(fg, 1.5))
	}
	switch check {
	case CheckFirst:
		mark(0)
	case CheckSecond:
		mark(-1.5)
		mark(1.5)
	case CheckUnrecognized:
		canvas.DrawCircle(ui.Offset{Dx: box.Left + box.Width()/2, Dy: box.Top + box.Height()/2}, 3, fillPaint(fg))
	}
}

// ShouldRepaint implements rendering.CustomPainter.
func (p *gutterPainter) ShouldRepaint(old rendering.CustomPainter) bool {
	o, ok := old.(*gutterPainter)
	return !ok || o.colors != p.colors || o.model != p.model || o.side != p.side
}

// hit finds the checkbox at a point of the gutter.
func (m *Model) hit(side Side, p ui.Offset, viewHeight float64) (GutterItem, bool) {
	for _, it := range m.GutterItems(side) {
		if gutterBox(it, viewHeight).Contains(p) {
			return it, true
		}
	}
	return GutterItem{}, false
}

// ClickGutter handles a press at a point of the gutter of an input: on a checkbox it toggles the input in the
// result of that conflict. It reports whether a checkbox was there.
func (m *Model) ClickGutter(side Side, p ui.Offset, viewHeight float64) bool {
	in, ok := side.Input()
	if !ok {
		return false
	}
	it, hit := m.hit(side, p, viewHeight)
	if !hit {
		return false
	}
	m.Toggle(it.Conflict, in)
	return true
}
