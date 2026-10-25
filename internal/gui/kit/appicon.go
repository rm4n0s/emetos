package kit

import (
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// The application icon (SPEC §10.14, §17): a rounded square in indigo with a white `</>` mark, the symbol of the
// home page of an empty window. It is drawn with Glutter's canvas, with paths only (no font), so the picture is the
// same on every machine. The test of this package renders it at AppIconSizes into assets/icons/, and the application
// embeds those pictures for the window and the system tray.

// AppIconSizes are the sizes in pixels of the pictures of the application icon: the sizes of the tray (16, 22, 24, 32
// and 48), the ones of the icon theme folders (64, 128, 256 and 512).
var AppIconSizes = []int{16, 22, 24, 32, 48, 64, 128, 256, 512}

// The colors of the icon. The body is a gradient from a lighter indigo at the top to a deeper one at the bottom; both
// read on a light and on a dark panel. The border is a darker indigo and the mark is white.
var (
	appIconTop    = ui.NewColor(0xFF7467F7)
	appIconBottom = ui.NewColor(0xFF4A3DD4)
	appIconBorder = ui.NewColor(0xFF3A32AB)
	appIconMark   = ui.NewColor(0xFFFFFFFF)
)

// The geometry of the icon in a square of 512 units. The mark is two chevrons and a slash, each a path stroked with
// round caps and joins.
const (
	appIconUnits  = 512.0
	appIconRadius = 104.0 // the corner radius of the body
	appIconStroke = 44.0  // the width of the strokes of the mark
)

// appIconMarkPaths are the strokes of the mark in units: the left chevron, the slash and the right chevron. Each is a
// list of points.
var appIconMarkPaths = [][]ui.Offset{
	{{Dx: 138, Dy: 170}, {Dx: 64, Dy: 256}, {Dx: 138, Dy: 342}},
	{{Dx: 284, Dy: 138}, {Dx: 228, Dy: 374}},
	{{Dx: 374, Dy: 170}, {Dx: 448, Dy: 256}, {Dx: 374, Dy: 342}},
}

// AppIconPainter paints the application icon in the square of the smaller side of the box, from the top left corner.
// It has no state: every painter paints the same picture.
type AppIconPainter struct {
	rendering.CustomPainterBase
}

// NewAppIconPainter creates the painter of the application icon.
func NewAppIconPainter() *AppIconPainter {
	return &AppIconPainter{CustomPainterBase: rendering.NewCustomPainterBase(nil)}
}

// Paint implements rendering.CustomPainter.
func (p *AppIconPainter) Paint(canvas *ui.Canvas, size ui.Size) {
	s := min(size.Width, size.Height)
	if s <= 0 {
		return
	}
	u := s / appIconUnits
	// the border is one pixel wide up to 128 px, and grows with the picture after that
	border := max(1, s/128)
	radius := appIconRadius * u

	body := ui.NewRRectFromRectAndRadius(ui.NewRectFromLTWH(0, 0, s, s), ui.NewRadiusCircular(radius))
	fill := ui.NewPaint()
	fill.SetShader(ui.NewGradientLinear(ui.Offset{}, ui.Offset{Dy: s}, []ui.Color{appIconTop, appIconBottom}))
	canvas.DrawRRect(body, fill)

	edge := ui.NewPaint()
	edge.SetColor(appIconBorder)
	edge.SetStyle(ui.PaintingStyleStroke)
	edge.SetStrokeWidth(border)
	inner := ui.NewRRectFromRectAndRadius(ui.NewRectFromLTWH(border/2, border/2, s-border, s-border), ui.NewRadiusCircular(radius-border/2))
	canvas.DrawRRect(inner, edge)

	mark := ui.NewPaint()
	mark.SetColor(appIconMark)
	mark.SetStyle(ui.PaintingStyleStroke)
	// a stroke is at least 1.6 px wide, or the mark turns grey at the small sizes
	mark.SetStrokeWidth(max(appIconStroke*u, 1.6))
	mark.SetStrokeCap(ui.StrokeCapRound)
	mark.SetStrokeJoin(ui.StrokeJoinRound)
	for _, points := range appIconMarkPaths {
		path := ui.NewPath()
		for i, pt := range points {
			if i == 0 {
				path.MoveTo(pt.Dx*u, pt.Dy*u)
				continue
			}
			path.LineTo(pt.Dx*u, pt.Dy*u)
		}
		canvas.DrawPath(path, mark)
	}
}

// ShouldRepaint implements rendering.CustomPainter: the picture never changes.
func (p *AppIconPainter) ShouldRepaint(old rendering.CustomPainter) bool {
	_, same := old.(*AppIconPainter)
	return !same
}

// NewAppIcon creates the widget that shows the application icon, size logical pixels wide and high (the icon fills
// the box it is given when the box is tight).
func NewAppIcon(size float64) widgets.Widget {
	return widgets.NewSizedBox(
		widgets.NewCustomPaint(nil, widgets.CustomPaintOpts{Painter: NewAppIconPainter(), Size: ui.Size{Width: size, Height: size}}),
		widgets.SizedBoxOpts{Width: foundation.Some(size), Height: foundation.Some(size)})
}
