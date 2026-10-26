package editor

import (
	"math"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/find"
)

// Ported from VS Code: src/vs/editor/browser/viewParts/overviewRuler/overviewRuler.ts and
// src/vs/editor/common/viewModel/overviewZoneManager.ts

// RulerColors are the colors of the marks on the overview ruler.
type RulerColors struct {
	// FindMatch marks the matches of the find widget, in the middle lane; RangeHighlight marks
	// the lines of the current match, across all three lanes.
	FindMatch, RangeHighlight ui.Color
	// Error, Warning and Info mark the diagnostics, in the right lane (hints have no mark).
	Error, Warning, Info ui.Color
}

// rulerMinMarkHeight is the height of the smallest mark.
const rulerMinMarkHeight = 2.0

// rulerLayer is the widget of RenderEditorRuler.
type rulerLayer struct {
	widgets.LeafRenderObjectWidget
	vm     *ViewModel
	colors RulerColors
}

func newRulerLayer(vm *ViewModel, colors RulerColors) *rulerLayer {
	return &rulerLayer{LeafRenderObjectWidget: widgets.NewLeafRenderObjectWidgetBase(), vm: vm, colors: colors}
}

// CreateRenderObject implements widgets.RenderObjectWidget.
func (w *rulerLayer) CreateRenderObject(widgets.BuildContext) rendering.RenderObject {
	return NewRenderEditorRuler(w.vm, w.colors)
}

// UpdateRenderObject implements widgets.RenderObjectWidget.
func (w *rulerLayer) UpdateRenderObject(_ widgets.BuildContext, ro rendering.RenderObject) {
	ro.(*RenderEditorRuler).Update(w.vm, w.colors)
}

// RenderEditorRuler draws the overview ruler: one mark for every place of the whole document
// where the find widget found something, at the height that place has in the document scaled
// to the height of the editor (SPEC §9.8). It lets the pointer through to the scrollbar.
type RenderEditorRuler struct {
	rendering.RenderBox
	vm     *ViewModel
	colors RulerColors

	vmHandle, findHandle foundation.ListenerHandle
	attached             bool
}

// NewRenderEditorRuler makes the ruler of a view model.
func NewRenderEditorRuler(vm *ViewModel, colors RulerColors) *RenderEditorRuler {
	r := &RenderEditorRuler{vm: vm, colors: colors}
	rendering.InitRenderBox(&r.RenderBox, r)
	return r
}

// Update copies the configuration of a widget.
func (r *RenderEditorRuler) Update(vm *ViewModel, colors RulerColors) {
	if r.attached && vm != r.vm {
		r.unlisten()
		r.vm = vm
		r.listen()
	}
	r.vm, r.colors = vm, colors
	r.MarkNeedsPaint()
}

func (r *RenderEditorRuler) listen() {
	r.vmHandle = r.vm.AddListener(r.MarkNeedsPaint)
	r.findHandle = r.vm.AddFindListener(r.MarkNeedsPaint)
}

func (r *RenderEditorRuler) unlisten() {
	r.vm.RemoveListener(r.vmHandle)
	r.vm.RemoveFindListener(r.findHandle)
}

// Attach implements rendering.RenderObject.
func (r *RenderEditorRuler) Attach(owner *rendering.PipelineOwner) {
	r.RenderBox.Attach(owner)
	r.listen()
	r.attached = true
}

// Detach implements rendering.RenderObject.
func (r *RenderEditorRuler) Detach() {
	r.unlisten()
	r.attached = false
	r.RenderBox.Detach()
}

// SizedByParent implements rendering.RenderObject.
func (r *RenderEditorRuler) SizedByParent() bool { return true }

// IsRepaintBoundary implements rendering.RenderObject.
func (r *RenderEditorRuler) IsRepaintBoundary() bool { return true }

// HitTestSelf implements rendering.RenderBox.
func (r *RenderEditorRuler) HitTestSelf(ui.Offset) bool { return false }

// ComputeMinIntrinsicWidth implements rendering.RenderBox.
func (r *RenderEditorRuler) ComputeMinIntrinsicWidth(float64) float64 { return 0 }

// ComputeMaxIntrinsicWidth implements rendering.RenderBox.
func (r *RenderEditorRuler) ComputeMaxIntrinsicWidth(float64) float64 { return 0 }

// ComputeMinIntrinsicHeight implements rendering.RenderBox.
func (r *RenderEditorRuler) ComputeMinIntrinsicHeight(float64) float64 { return 0 }

// ComputeMaxIntrinsicHeight implements rendering.RenderBox.
func (r *RenderEditorRuler) ComputeMaxIntrinsicHeight(float64) float64 { return 0 }

// ComputeDryLayout implements rendering.RenderBox.
func (r *RenderEditorRuler) ComputeDryLayout(c rendering.BoxConstraints) ui.Size {
	return c.Constrain(c.Biggest())
}

// PerformLayout implements rendering.RenderBox.
func (r *RenderEditorRuler) PerformLayout() {}

// rulerLane is where a mark goes across the width of the ruler.
type rulerLane int

const (
	laneCenter rulerLane = iota
	laneFull
	laneRight
)

// rulerMark is a mark on the ruler: from y to y+height in the lane. A mark of the right lane has the severity of the
// diagnostic it stands for.
type rulerMark struct {
	y, height float64
	lane      rulerLane
	severity  DiagnosticSeverity
}

// rulerCache holds the marks of the diagnostics for what they were made for: the diagnostics (their generation), the height of
// the ruler and the size of the text (its height and number of lines). Typing inside a line changes none of them, so the
// marks of thousands of diagnostics are not worked out again for every key.
type rulerCache struct {
	valid         bool
	generation    int
	height, total float64
	lines         int
	marks         []rulerMark
}

// diagnosticRulerMarks are the marks of the diagnostics for a ruler of a height: one run of pixels for each stretch of the
// ruler where the most serious diagnostic is the same, so that thousands of diagnostics make as many marks as the ruler has
// pixels at most.
//
// Ported from VS Code: src/vs/editor/common/viewModel/overviewZoneManager.ts (the zones of a lane are merged by color)
func (vm *ViewModel) diagnosticRulerMarks(height float64) []rulerMark {
	if !vm.diagnosticsShown() || height <= 0 {
		return nil
	}
	total := vm.ContentHeight()
	if total <= 0 {
		return nil
	}
	lines := vm.doc.Snapshot().LineCount()
	c := &vm.diag.ruler
	if c.valid && c.generation == vm.diag.generation && c.height == height && c.total == total && c.lines == lines {
		return c.marks
	}
	proj := vm.Projection()
	scale := height / total
	best := make([]DiagnosticSeverity, int(math.Ceil(height))+1)
	for _, d := range diagnosticsOf(vm.doc.DecorationsOf(vm.owner(diagnosticsOwner))) {
		if d.Severity == DiagnosticHint {
			continue
		}
		first, last := vm.RowOfPosition(d.Range.Start), vm.RowOfPosition(d.Range.End)
		y := int(math.Round(proj.RowTop(first) * scale))
		end := max(int(math.Round((proj.RowTop(last)+proj.RowHeight(last))*scale)), y+int(rulerMinMarkHeight))
		for p := max(y, 0); p < min(end, len(best)); p++ {
			best[p] = max(best[p], d.Severity)
		}
	}
	var marks []rulerMark
	for p := 0; p < len(best); {
		if best[p] == 0 {
			p++
			continue
		}
		q := p
		for q < len(best) && best[q] == best[p] {
			q++
		}
		marks = append(marks, rulerMark{y: float64(p), height: float64(q - p), lane: laneRight, severity: best[p]})
		p = q
	}
	*c = rulerCache{valid: true, generation: vm.diag.generation, height: height, total: total, lines: lines, marks: marks}
	return marks
}

// marks are the marks of the find decorations for a ruler of a height, merged so that none is
// drawn twice at the same place.
func (vm *ViewModel) rulerMarks(height float64) []rulerMark {
	if !vm.FindRevealed() || vm.findw.model == nil || height <= 0 {
		return nil
	}
	total := vm.ContentHeight()
	if total <= 0 {
		return nil
	}
	proj := vm.Projection()
	scale := height / total
	var marks []rulerMark
	lastCenter := math.Inf(-1)
	for _, d := range vm.doc.DecorationsOf(vm.owner(findOwner)) {
		var lane rulerLane
		switch d.Options.ClassName {
		case find.ClassFindMatch, find.ClassCurrentFindMatch:
			lane = laneCenter
		case find.ClassRangeHighlight:
			lane = laneFull
		default:
			continue
		}
		first, last := vm.RowOfPosition(d.Range.Start), vm.RowOfPosition(d.Range.End)
		y := math.Round(proj.RowTop(first) * scale)
		h := max(math.Round((proj.RowTop(last)+proj.RowHeight(last))*scale)-y, rulerMinMarkHeight)
		if lane == laneCenter {
			if y < lastCenter {
				continue // the same place as the mark before
			}
			lastCenter = y + h
		}
		marks = append(marks, rulerMark{y: y, height: h, lane: lane})
	}
	return marks
}

// severity is the color of the marks of a severity.
func (c RulerColors) severity(s DiagnosticSeverity) ui.Color {
	switch s {
	case DiagnosticError:
		return c.Error
	case DiagnosticWarning:
		return c.Warning
	}
	return c.Info
}

// Paint implements rendering.RenderObject.
func (r *RenderEditorRuler) Paint(context *rendering.PaintingContext, offset ui.Offset) {
	size := r.Size()
	marks := append(r.vm.rulerMarks(size.Height), r.vm.diagnosticRulerMarks(size.Height)...)
	if len(marks) == 0 {
		return
	}
	canvas := context.Canvas()
	third := math.Floor(size.Width / 3)
	for _, m := range marks {
		left, width, color := offset.Dx+third, size.Width-2*third, r.colors.FindMatch
		switch m.lane {
		case laneFull:
			left, width, color = offset.Dx, size.Width, r.colors.RangeHighlight
		case laneRight:
			left, width, color = offset.Dx+size.Width-third, third, r.colors.severity(m.severity)
		}
		canvas.DrawRect(ui.NewRectFromLTWH(left, offset.Dy+m.y, width, m.height), fill(color))
	}
}
