package scmgraph

import (
	"math"

	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/scm/graph"
)

// The graph column of a row: the lanes, the circle of the commit and the curves that join the lanes, drawn on a canvas as VS Code's
// renderSCMHistoryItemGraph draws them in an SVG (a lane every 11 px, a row 22 px high, lines 1 px wide, circles with a 2 px halo in the
// color of the row's background).
//
// Ported from VS Code: src/vs/workbench/contrib/scm/browser/scmHistory.ts (renderSCMHistoryItemGraph, renderSCMHistoryGraphPlaceholder)
// and src/vs/workbench/contrib/scm/browser/media/scm.css (the circles of the head and of the incoming and outgoing changes)

const (
	laneHeight  = RowHeight
	curveRadius = 5.0
	circleR     = 4.0
	circleStrk  = 2.0
	lineWidth   = 1.0
)

// ColumnWidth is the width of the graph column of a row: a lane for each of the widest of its input and output lanes, and one more.
func ColumnWidth(in, out int) float64 { return LaneWidth * float64(max(in, out, 1)+1) }

// lanePainter draws the graph column of a row.
type lanePainter struct {
	rendering.CustomPainterBase
	row graph.Row
	// resolve gives the color of a lane by its theme color id.
	resolve func(id string) ui.Color
	// halo is the color of the background of the row, which the circles are outlined with.
	halo ui.Color
}

// ShouldRepaint implements rendering.CustomPainter.
func (p *lanePainter) ShouldRepaint(old rendering.CustomPainter) bool {
	o, ok := old.(*lanePainter)
	return !ok || o.halo != p.halo || !sameRow(o.row, p.row)
}

func sameRow(a, b graph.Row) bool {
	if a.Kind != b.Kind || a.Item.ID != b.Item.ID || len(a.Input) != len(b.Input) || len(a.Output) != len(b.Output) || len(a.Item.ParentIDs) != len(b.Item.ParentIDs) {
		return false
	}
	for i := range a.Input {
		if a.Input[i] != b.Input[i] {
			return false
		}
	}
	for i := range a.Output {
		if a.Output[i] != b.Output[i] {
			return false
		}
	}
	return true
}

func stroke(c ui.Color, w float64) *ui.Paint {
	p := ui.NewPaint()
	p.SetColor(c)
	p.SetStyle(ui.PaintingStyleStroke)
	p.SetStrokeWidth(w)
	p.SetStrokeCap(ui.StrokeCapRound)
	return p
}

func fill(c ui.Color) *ui.Paint {
	p := ui.NewPaint()
	p.SetColor(c)
	return p
}

func arc(r float64) ui.ArcToPointOpts { return ui.ArcToPointOpts{Radius: ui.Radius{X: r, Y: r}} }

func arcBack(r float64) ui.ArcToPointOpts {
	cw := false
	return ui.ArcToPointOpts{Radius: ui.Radius{X: r, Y: r}, Clockwise: &cw}
}

// Paint implements rendering.CustomPainter.
func (p *lanePainter) Paint(canvas *ui.Canvas, _ ui.Size) {
	const W, H = LaneWidth, laneHeight
	r := p.row
	inputIndex := -1
	for i, n := range r.Input {
		if n.ID == r.Item.ID {
			inputIndex = i
			break
		}
	}
	circleIndex := r.Index()
	circleColor := graph.RefColor
	switch {
	case circleIndex < len(r.Output):
		circleColor = r.Output[circleIndex].Color
	case circleIndex < len(r.Input):
		circleColor = r.Input[circleIndex].Color
	}
	line := func(x, y1, y2 float64, color string) {
		path := ui.NewPath()
		path.MoveTo(x, y1)
		path.LineTo(x, y2)
		canvas.DrawPath(path, stroke(p.resolve(color), lineWidth))
	}
	outIndex := 0
	for index, node := range r.Input {
		x := W * float64(index+1)
		if node.ID == r.Item.ID {
			if index != circleIndex {
				// a second lane that leads to the commit joins its circle: / and -
				path := ui.NewPath()
				path.MoveTo(x, 0)
				path.ArcToPoint(ui.Offset{Dx: W * float64(index), Dy: W}, arc(W))
				path.LineTo(W*float64(circleIndex+1), W)
				canvas.DrawPath(path, stroke(p.resolve(node.Color), lineWidth))
			} else {
				outIndex++
			}
			continue
		}
		if outIndex < len(r.Output) && node.ID == r.Output[outIndex].ID {
			if index == outIndex {
				line(x, 0, H, node.Color)
			} else {
				// the lane moves to the left: | / - / |
				ox := W * float64(outIndex+1)
				path := ui.NewPath()
				path.MoveTo(x, 0)
				path.LineTo(x, 6)
				path.ArcToPoint(ui.Offset{Dx: x - curveRadius, Dy: H / 2}, arc(curveRadius))
				path.LineTo(ox+curveRadius, H/2)
				path.ArcToPoint(ui.Offset{Dx: ox, Dy: H/2 + curveRadius}, arcBack(curveRadius))
				path.LineTo(ox, H)
				canvas.DrawPath(path, stroke(p.resolve(node.Color), lineWidth))
			}
			outIndex++
		}
	}
	// the other parents of a merge: -\
	for i := 1; i < len(r.Item.ParentIDs); i++ {
		po := -1
		for j := len(r.Output) - 1; j >= 0; j-- {
			if r.Output[j].ID == r.Item.ParentIDs[i] {
				po = j
				break
			}
		}
		if po < 0 {
			continue
		}
		path := ui.NewPath()
		path.MoveTo(W*float64(po), H/2)
		path.ArcToPoint(ui.Offset{Dx: W * float64(po+1), Dy: H}, arc(W))
		path.MoveTo(W*float64(po), H/2)
		path.LineTo(W*float64(circleIndex+1), H/2)
		canvas.DrawPath(path, stroke(p.resolve(r.Output[po].Color), lineWidth))
	}
	cx := W * float64(circleIndex+1)
	if inputIndex != -1 {
		line(cx, 0, H/2, r.Input[inputIndex].Color)
	}
	if len(r.Item.ParentIDs) > 0 {
		line(cx, H/2, H, circleColor)
	}
	color := p.resolve(circleColor)
	disc := func(radius, strokeWidth float64, fillColor ui.Color, filled bool) {
		c := ui.Offset{Dx: cx, Dy: W}
		if filled {
			canvas.DrawCircle(c, radius, fill(fillColor))
		}
		canvas.DrawCircle(c, radius, stroke(p.halo, strokeWidth))
	}
	switch r.Kind {
	case graph.KindHead:
		disc(circleR+3, circleStrk, color, true)
		disc(circleStrk, circleR, p.halo, true)
	case graph.KindIncoming, graph.KindOutgoing:
		disc(circleR+3, circleStrk, color, true)
		disc(circleR+1, circleStrk+1, p.halo, true)
		p.dashedCircle(canvas, ui.Offset{Dx: cx, Dy: W}, circleR+1, color)
	default:
		if len(r.Item.ParentIDs) > 1 {
			disc(circleR+2, circleStrk, color, true)
			disc(circleR-1, circleStrk, color, true)
		} else {
			disc(circleR+1, circleStrk, color, true)
		}
	}
}

// dashedCircle draws a circle with a dash of 4 px and a gap of 2 px, as the SVG's stroke-dasharray "4,2".
func (p *lanePainter) dashedCircle(canvas *ui.Canvas, c ui.Offset, radius float64, color ui.Color) {
	paint := stroke(color, 1)
	rect := ui.NewRectFromCenter(c, 2*radius, 2*radius)
	const dash, gap = 4.0, 2.0
	for at := 0.0; at < 2*math.Pi*radius; at += dash + gap {
		canvas.DrawArc(rect, at/radius, math.Min(dash, 2*math.Pi*radius-at)/radius, false, paint)
	}
}

// placeholderPainter draws the lanes that go on through a row that has no commit: the load-more row.
type placeholderPainter struct {
	rendering.CustomPainterBase
	columns []graph.Node
	resolve func(id string) ui.Color
}

// ShouldRepaint implements rendering.CustomPainter.
func (p *placeholderPainter) ShouldRepaint(old rendering.CustomPainter) bool {
	o, ok := old.(*placeholderPainter)
	if !ok || len(o.columns) != len(p.columns) {
		return true
	}
	for i := range o.columns {
		if o.columns[i] != p.columns[i] {
			return true
		}
	}
	return false
}

// Paint implements rendering.CustomPainter.
func (p *placeholderPainter) Paint(canvas *ui.Canvas, _ ui.Size) {
	for i, n := range p.columns {
		path := ui.NewPath()
		x := LaneWidth * float64(i+1)
		path.MoveTo(x, 0)
		path.LineTo(x, laneHeight)
		canvas.DrawPath(path, stroke(p.resolve(n.Color), lineWidth))
	}
}
