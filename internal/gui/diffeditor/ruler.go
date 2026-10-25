package diffeditor

import (
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/diff"
	"github.com/rm4n0s/emetos/internal/gui/editor"
)

// Ported from VS Code: src/vs/editor/browser/widget/diffEditor/features/overviewRulerFeature.ts

// rulerMinMark is the height of the smallest mark on the overview ruler.
const rulerMinMark = 2.0

// RulerMark is a mark of the overview ruler: the rows a change takes in the editor of a side, as a part of the
// height of all the rows of the editor.
type RulerMark struct {
	// Side is the editor the mark is of: the left half of the ruler is the original side's, the right half the
	// modified side's.
	Side Side
	// Top and Bottom are the extent of the change in pixels from the top of the editor's rows, zones included.
	Top, Bottom float64
}

// RulerMarks are the marks of the changes, one for every change that has lines on a side. Total is the height of
// all the rows of the editors (they are as tall as each other once aligned), which the marks are scaled to.
func (m *Model) RulerMarks() (marks []RulerMark, total float64) {
	if m.mode == Inline {
		return m.inlineRulerMarks()
	}
	total = max(m.vms[Original].Projection().Height(), m.vms[Modified].Projection().Height())
	for _, c := range m.changes {
		for _, s := range []Side{Original, Modified} {
			lr := c.Modified
			if s == Original {
				lr = c.Original
			}
			if top, bottom, ok := lineRangeExtent(m.vms[s], lr); ok {
				marks = append(marks, RulerMark{Side: s, Top: top, Bottom: bottom})
			}
		}
	}
	return marks, total
}

// lineRangeExtent is the vertical extent of the rows of the lines of a range in an editor.
func lineRangeExtent(vm *editor.ViewModel, lr diff.LineRange) (top, bottom float64, ok bool) {
	if lr.IsEmpty() {
		return 0, 0, false
	}
	proj := vm.Projection()
	first, _ := proj.LineRows(lr.StartLine - 1)
	lastFirst, count := proj.LineRows(lr.EndLine - 2)
	lastRow := lastFirst + max(count, 1) - 1
	if first > lastRow || lastRow >= proj.RowCount() {
		return 0, 0, false
	}
	return proj.RowTop(first), proj.RowTop(lastRow) + proj.RowHeight(lastRow), true
}

// ScrollToFraction scrolls the editors so that the place at a height of the overview ruler, whose own height is
// given, is in the middle of the view (a click or a drag on the ruler).
func (m *Model) ScrollToFraction(y, height float64) {
	if height <= 0 {
		return
	}
	mod := m.vms[Modified]
	_, viewport := mod.ViewportSize()
	total := max(m.vms[Original].Projection().Height(), mod.Projection().Height())
	if m.mode == Inline {
		total = mod.Projection().Height()
	}
	m.leader = Modified
	mod.ScrollTo(max(y/height*total-viewport/2, 0))
}

// rulerPainter draws the overview ruler: the marks of the removed lines on the left half and those of the
// inserted lines on the right half, at the height they have among all the rows.
type rulerPainter struct {
	rendering.CustomPainterBase
	model      *Model
	colors     Colors
	background ui.Color
}

func newRulerPainter(model *Model, colors Colors, background ui.Color) *rulerPainter {
	return &rulerPainter{CustomPainterBase: rendering.NewCustomPainterBase(model), model: model, colors: colors, background: background}
}

// Paint implements rendering.CustomPainter.
func (p *rulerPainter) Paint(canvas *ui.Canvas, size ui.Size) {
	if p.background.Alpha() > 0 {
		canvas.DrawRect(ui.NewRectFromLTWH(0, 0, size.Width, size.Height), fillPaint(p.background))
	}
	marks, total := p.model.RulerMarks()
	if total <= 0 || size.Height <= 0 {
		return
	}
	scale := size.Height / total
	half := size.Width / 2
	for _, m := range marks {
		left, color := 0.0, p.colors.RemovedOverview
		if m.Side == Modified {
			left, color = half, p.colors.InsertedOverview
		}
		y := m.Top * scale
		canvas.DrawRect(ui.NewRectFromLTWH(left, y, half, max((m.Bottom-m.Top)*scale, rulerMinMark)), fillPaint(color))
	}
}

// ShouldRepaint implements rendering.CustomPainter.
func (p *rulerPainter) ShouldRepaint(old rendering.CustomPainter) bool {
	o, ok := old.(*rulerPainter)
	return !ok || o.colors != p.colors || o.background != p.background || o.model != p.model
}

func fillPaint(c ui.Color) *ui.Paint {
	paint := ui.NewPaint()
	paint.SetColor(c)
	return paint
}

// inlineRulerMarks are the marks of the inline view: both halves of the ruler measure the one editor, the removed
// one the zone of the deleted lines of a change, the inserted one the lines that took their place.
func (m *Model) inlineRulerMarks() (marks []RulerMark, total float64) {
	mod := m.vms[Modified]
	proj := mod.Projection()
	total = proj.Height()
	zoneOf := map[int]editor.ZoneID{}
	for _, z := range m.inline {
		zoneOf[z.change] = z.id
	}
	for i, c := range m.changes {
		if id, ok := zoneOf[i]; ok {
			if first, count := proj.ZoneRows(id); count > 0 && first+count <= proj.RowCount() {
				marks = append(marks, RulerMark{Side: Original, Top: proj.RowTop(first), Bottom: proj.RowTop(first+count-1) + proj.RowHeight(first+count-1)})
			}
		}
		if top, bottom, ok := lineRangeExtent(mod, c.Modified); ok {
			marks = append(marks, RulerMark{Side: Modified, Top: top, Bottom: bottom})
		}
	}
	return marks, total
}
