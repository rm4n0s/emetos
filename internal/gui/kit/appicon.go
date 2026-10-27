package kit

import (
	"math"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// The application icon (SPEC §10.14, §17): a cat's hairball, the thing a cat throws up ("emetos" is Greek for
// vomiting), on a rounded square in soft teal. The hairball is an elongated, slightly bent, lumpy sausage of matted
// grey-brown fur with a few darker clumps, short stray hairs sticking out of its outline and ends, a few fur strokes
// across its body, a small glossy highlight that makes it look damp, and a flat puddle under it. It is drawn with
// Glutter's canvas, with paths and gradients only (no font, no bitmap), so the picture is the same on every machine.
// The test of this package renders it at AppIconSizes into assets/icons/, and the application embeds those pictures
// for the window and the system tray.

// AppIconSizes are the sizes in pixels of the pictures of the application icon: the sizes of the tray (16, 22, 24, 32
// and 48), the ones of the icon theme folders (64, 128, 256 and 512).
var AppIconSizes = []int{16, 22, 24, 32, 48, 64, 128, 256, 512}

// The colors of the icon. The backdrop is a gradient from a lighter teal at the top to a deeper one at the bottom, with
// a darker teal border; the teal reads on a light and on a dark panel, and the brown fur stands out on it. The fur is
// a gradient from a light grey-brown on the top of the hairball to a darker one underneath, with a dark brown outline,
// darker clumps and strokes, and lighter strands. The puddle is a deeper teal with a lighter rim.
var (
	appIconTop    = ui.NewColor(0xFF93E3D5)
	appIconBottom = ui.NewColor(0xFF3FAFA4)
	appIconBorder = ui.NewColor(0xFF2A857E)

	appIconFurLight   = ui.NewColor(0xFFC4B09A)
	appIconFurDark    = ui.NewColor(0xFF85705B)
	appIconFurOutline = ui.NewColor(0xFF5A4636)
	appIconFurClump   = ui.NewColor(0xFF6B5645)
	appIconFurStrand  = ui.NewColor(0xFFDCCBB6)

	appIconPuddle     = ui.NewColor(0xFF1F6F69)
	appIconPuddleRim  = ui.NewColor(0xFFB6F0E6)
	appIconHighlight  = ui.NewColor(0xFFFFFFFF)
	appIconSplashDrop = ui.NewColor(0xFF1F6F69)
)

// The geometry of the icon in a square of 512 units.
const (
	appIconUnits   = 512.0
	appIconRadius  = 104.0 // the corner radius of the backdrop
	appIconThick   = 70.0  // the half thickness of the hairball in its middle
	appIconOutline = 8.0   // the width of the dark outline of the hairball
	appIconSamples = 90    // the points of each side of the outline of the hairball
)

// The hairball is a thick line: a quadratic curve from the left end through a control point above to the right end,
// with a half thickness along it that is zero at the ends (a blunt cap, not a point) and lumpy between them.
var (
	appIconBodyA = ui.Offset{Dx: 78, Dy: 312}
	appIconBodyB = ui.Offset{Dx: 256, Dy: 166}
	appIconBodyC = ui.Offset{Dx: 436, Dy: 292}
)

// appIconHair is a stray hair: it leaves the outline at the parameter t of the body, on a side (-1 the top, 1 the
// belly, 0 the end of the body that t names), len units long and bent by curl (a fraction of its length). level is
// the detail level that first shows it (see appIconDetail).
type appIconHair struct {
	t     float64
	side  float64
	len   float64
	curl  float64
	level int
}

var appIconHairs = []appIconHair{
	{t: 0.00, side: 0, len: 60, curl: -0.30, level: 1},
	{t: 1.00, side: 0, len: 56, curl: 0.30, level: 1},
	{t: 0.22, side: -1, len: 46, curl: -0.40, level: 1},
	{t: 0.74, side: -1, len: 50, curl: 0.40, level: 1},
	{t: 0.45, side: -1, len: 34, curl: 0.30, level: 2},
	{t: 0.90, side: -1, len: 34, curl: -0.30, level: 2},
	{t: 0.10, side: 1, len: 34, curl: 0.40, level: 2},
	{t: 0.62, side: 1, len: 30, curl: -0.35, level: 2},
	{t: 0.88, side: 1, len: 32, curl: 0.35, level: 2},
}

// appIconTufts are the short tufts of fur along the outline, the fuzz of matted hair: one every 0.0475 of the body,
// on the top and the belly in turn, with lengths and curls that vary without a pattern.
var appIconTufts = func() []appIconHair {
	var tufts []appIconHair
	for i := 0; i < 20; i++ {
		side := -1.0
		if i%3 == 2 {
			side = 1
		}
		tufts = append(tufts, appIconHair{
			t:     0.05 + 0.0475*float64(i),
			side:  side,
			len:   15 + 11*math.Abs(math.Sin(float64(i)*2.7)),
			curl:  0.35 * math.Sin(float64(i)*1.9),
			level: 2,
		})
	}
	return tufts
}()

// appIconFur is a fur stroke across the body: it crosses the body at the parameter t, from across (-1 the top edge to
// 1 the belly edge) of start to end of the half thickness, and bulges along the body by bend units. strand strokes are
// lighter than the body, the others darker. level is the detail level that first shows it.
type appIconFur struct {
	t, from, to, bend float64
	strand            bool
	level             int
}

var appIconFurs = []appIconFur{
	{t: 0.27, from: -0.65, to: 0.30, bend: 12, level: 1},
	{t: 0.55, from: -0.30, to: 0.70, bend: -12, level: 1},
	{t: 0.80, from: -0.60, to: 0.35, bend: 11, level: 1},
	{t: 0.14, from: 0.00, to: 0.65, bend: -9, level: 2},
	{t: 0.42, from: -0.75, to: -0.05, bend: -10, level: 2},
	{t: 0.67, from: 0.15, to: 0.75, bend: 9, level: 2},
	{t: 0.36, from: 0.20, to: 0.80, bend: 8, strand: true, level: 2},
	{t: 0.62, from: -0.80, to: -0.25, bend: -8, strand: true, level: 2},
	{t: 0.90, from: -0.30, to: 0.30, bend: 7, strand: true, level: 2},
}

// appIconDetail gives the detail level for a size in pixels: 0 below 32 px (the body, the highlight and the puddle
// only), 1 up to 63 px (four stray hairs, three fur strokes), 2 from 64 px (all of them).
func appIconDetail(s float64) int {
	switch {
	case s < 32:
		return 0
	case s < 64:
		return 1
	}
	return 2
}

// appIconBodyPoint is the point of the middle line of the hairball at t (0 to 1) and its unit normal, which points
// to the belly.
func appIconBodyPoint(t float64) (pt, normal ui.Offset) {
	a, b, c := appIconBodyA, appIconBodyB, appIconBodyC
	m := 1 - t
	pt = ui.Offset{
		Dx: m*m*a.Dx + 2*m*t*b.Dx + t*t*c.Dx,
		Dy: m*m*a.Dy + 2*m*t*b.Dy + t*t*c.Dy,
	}
	tx := 2*m*(b.Dx-a.Dx) + 2*t*(c.Dx-b.Dx)
	ty := 2*m*(b.Dy-a.Dy) + 2*t*(c.Dy-b.Dy)
	n := math.Hypot(tx, ty)
	return pt, ui.Offset{Dx: -ty / n, Dy: tx / n}
}

// appIconHalfThickness is the half thickness of the hairball at t on a side (-1 the top, 1 the belly): zero at the
// ends, round caps there, and lumps between. thick scales the whole (the small sizes get a fatter body).
func appIconHalfThickness(t, side, thick float64) float64 {
	lumps := 1 + 0.09*math.Sin(2*math.Pi*2.3*t+side*1.1) + 0.05*math.Sin(2*math.Pi*5.1*t+side*2.3+0.6) + 0.035*math.Sin(2*math.Pi*11.7*t+side*0.7)
	return thick * math.Pow(math.Sin(math.Pi*t), 0.36) * lumps
}

// appIconBodyPath is the outline of the hairball in pixels: points along the top from the left end to the right end
// and back along the belly, joined by quadratic curves through their middles so the line is smooth. thick is the half
// thickness in units, u pixels per unit.
func appIconBodyPath(thick, u float64) *ui.Path {
	var pts []ui.Offset
	at := func(i int, side float64) ui.Offset {
		// the samples crowd at the ends, where the outline turns fast
		t := 0.5 - 0.5*math.Cos(math.Pi*float64(i)/appIconSamples)
		c, n := appIconBodyPoint(t)
		r := appIconHalfThickness(t, side, thick)
		return ui.Offset{Dx: (c.Dx + n.Dx*side*r) * u, Dy: (c.Dy + n.Dy*side*r) * u}
	}
	for i := 0; i <= appIconSamples; i++ {
		pts = append(pts, at(i, -1))
	}
	for i := appIconSamples - 1; i >= 1; i-- {
		pts = append(pts, at(i, 1))
	}
	mid := func(a, b ui.Offset) ui.Offset { return ui.Offset{Dx: (a.Dx + b.Dx) / 2, Dy: (a.Dy + b.Dy) / 2} }
	path := ui.NewPath()
	start := mid(pts[len(pts)-1], pts[0])
	path.MoveTo(start.Dx, start.Dy)
	for i, p := range pts {
		m := mid(p, pts[(i+1)%len(pts)])
		path.QuadraticBezierTo(p.Dx, p.Dy, m.Dx, m.Dy)
	}
	path.Close()
	return path
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
	paintAppIconBackdrop(canvas, s, u)
	// the small sizes show the hairball and its puddle larger, about the middle of the picture, so that they fill the
	// few pixels there are
	k := 1.0
	if s < 32 {
		k = 1.08
	}
	canvas.Save()
	canvas.Translate(256*u, 268*u)
	canvas.ScaleUniform(k)
	canvas.Translate(-256*u, -268*u)
	paintAppIconPuddle(canvas, s, u)
	paintAppIconHairball(canvas, s, u)
	canvas.Restore()
}

// paintAppIconBackdrop draws the rounded square: a vertical gradient with a border that is one pixel wide up to 128 px
// and grows with the picture after that.
func paintAppIconBackdrop(canvas *ui.Canvas, s, u float64) {
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
}

// paintAppIconPuddle draws the flat puddle under the hairball: a deeper teal oval with a lighter rim and a few drops
// at the small sizes' expense (the drops only from 64 px).
func paintAppIconPuddle(canvas *ui.Canvas, s, u float64) {
	oval := ui.NewRectFromCenter(ui.Offset{Dx: 262 * u, Dy: 350 * u}, 330*u, 78*u)
	fill := ui.NewPaint()
	fill.SetColor(appIconPuddle.WithOpacity(0.55))
	canvas.DrawOval(oval, fill)

	if s >= 32 {
		rim := ui.NewPaint()
		rim.SetColor(appIconPuddleRim.WithOpacity(0.55))
		rim.SetStyle(ui.PaintingStyleStroke)
		rim.SetStrokeWidth(max(1, 5*u))
		canvas.DrawOval(ui.NewRectFromCenter(ui.Offset{Dx: 262 * u, Dy: 350 * u}, 316*u, 66*u), rim)
	}
	if appIconDetail(s) == 2 {
		drop := ui.NewPaint()
		drop.SetColor(appIconSplashDrop.WithOpacity(0.5))
		canvas.DrawOval(ui.NewRectFromCenter(ui.Offset{Dx: 70 * u, Dy: 376 * u}, 34*u, 14*u), drop)
		canvas.DrawOval(ui.NewRectFromCenter(ui.Offset{Dx: 440 * u, Dy: 332 * u}, 26*u, 11*u), drop)
	}
}

// paintAppIconHairball draws the hairball: stray hairs under the body, the body with its gradient, clumps, fur strokes
// and outline, then the highlight.
func paintAppIconHairball(canvas *ui.Canvas, s, u float64) {
	detail := appIconDetail(s)
	thick := appIconThick
	body := appIconBodyPath(thick, u)

	// the stray hairs go under the body, so they seem to come out of the fur
	hair := ui.NewPaint()
	hair.SetColor(appIconFurOutline)
	hair.SetStyle(ui.PaintingStyleStroke)
	hair.SetStrokeCap(ui.StrokeCapRound)
	hair.SetStrokeWidth(max(1.2, (map[int]float64{1: 11, 2: 6}[detail])*u))
	for _, h := range appIconHairs {
		if h.level <= detail && detail > 0 {
			canvas.DrawPath(appIconHairPath(h, thick, u), hair)
		}
	}

	if detail == 2 {
		tuft := ui.NewPaint()
		tuft.SetColor(appIconFurOutline)
		tuft.SetStyle(ui.PaintingStyleStroke)
		tuft.SetStrokeCap(ui.StrokeCapRound)
		tuft.SetStrokeWidth(max(1.2, 4.5*u))
		for _, h := range appIconTufts {
			canvas.DrawPath(appIconHairPath(h, thick, u), tuft)
		}
	}

	fill := ui.NewPaint()
	fill.SetShader(ui.NewGradientLinear(ui.Offset{Dy: 130 * u}, ui.Offset{Dy: 340 * u}, []ui.Color{appIconFurLight, appIconFurDark}))
	canvas.DrawPath(body, fill)

	canvas.Save()
	canvas.ClipPath(body)
	paintAppIconFurDetail(canvas, thick, u, detail)
	canvas.Restore()

	outline := ui.NewPaint()
	outline.SetColor(appIconFurOutline)
	outline.SetStyle(ui.PaintingStyleStroke)
	outline.SetStrokeJoin(ui.StrokeJoinRound)
	outline.SetStrokeWidth(max(1, appIconOutline*u))
	canvas.DrawPath(body, outline)

	paintAppIconHighlight(canvas, thick, u)
}

// paintAppIconFurDetail draws, inside the body, the darker clumps and the fur strokes of the detail level.
func paintAppIconFurDetail(canvas *ui.Canvas, thick, u float64, detail int) {
	// the clumps: darker patches on the belly and on the ends, which make the body lumpy
	clump := ui.NewPaint()
	clump.SetColor(appIconFurClump.WithOpacity(0.45))
	for _, c := range []struct{ t, across, rx, ry float64 }{
		{0.16, 0.50, 56, 36},
		{0.50, 0.85, 90, 34},
		{0.86, 0.40, 54, 38},
	} {
		pt, n := appIconBodyPoint(c.t)
		x, y := pt.Dx+n.Dx*c.across*thick, pt.Dy+n.Dy*c.across*thick
		canvas.DrawOval(ui.NewRectFromCenter(ui.Offset{Dx: x * u, Dy: y * u}, c.rx*2*u, c.ry*2*u), clump)
	}
	if detail == 0 {
		return
	}

	stroke := ui.NewPaint()
	stroke.SetStyle(ui.PaintingStyleStroke)
	stroke.SetStrokeCap(ui.StrokeCapRound)
	stroke.SetStrokeWidth(max(1.2, 6*u))
	for _, f := range appIconFurs {
		if f.level > detail {
			continue
		}
		if f.strand {
			stroke.SetColor(appIconFurStrand.WithOpacity(0.8))
		} else {
			stroke.SetColor(appIconFurOutline.WithOpacity(0.55))
		}
		canvas.DrawPath(appIconFurPath(f, thick, u), stroke)
	}
}

// appIconHairPath is the path of a stray hair in pixels.
func appIconHairPath(h appIconHair, thick, u float64) *ui.Path {
	c, n := appIconBodyPoint(h.t)
	var dir ui.Offset
	var start ui.Offset
	if h.side == 0 {
		// the ends: the hair leaves along the middle line, outward
		other, _ := appIconBodyPoint(h.t + map[bool]float64{true: -0.02, false: 0.02}[h.t > 0.5])
		d := ui.Offset{Dx: c.Dx - other.Dx, Dy: c.Dy - other.Dy}
		l := math.Hypot(d.Dx, d.Dy)
		dir = ui.Offset{Dx: d.Dx / l, Dy: d.Dy / l}
		start = ui.Offset{Dx: c.Dx - dir.Dx*10, Dy: c.Dy - dir.Dy*10}
	} else {
		r := appIconHalfThickness(h.t, h.side, thick)
		dir = ui.Offset{Dx: n.Dx * h.side, Dy: n.Dy * h.side}
		start = ui.Offset{Dx: c.Dx + dir.Dx*(r-10), Dy: c.Dy + dir.Dy*(r-10)}
	}
	end := ui.Offset{Dx: start.Dx + dir.Dx*h.len, Dy: start.Dy + dir.Dy*h.len}
	// the control point is in the middle, pushed to the side to curl the hair
	ctl := ui.Offset{
		Dx: (start.Dx+end.Dx)/2 - dir.Dy*h.curl*h.len,
		Dy: (start.Dy+end.Dy)/2 + dir.Dx*h.curl*h.len,
	}
	path := ui.NewPath()
	path.MoveTo(start.Dx*u, start.Dy*u)
	path.QuadraticBezierTo(ctl.Dx*u, ctl.Dy*u, end.Dx*u, end.Dy*u)
	return path
}

// appIconFurPath is the path of a fur stroke in pixels.
func appIconFurPath(f appIconFur, thick, u float64) *ui.Path {
	c, n := appIconBodyPoint(f.t)
	tangent := ui.Offset{Dx: n.Dy, Dy: -n.Dx}
	at := func(across float64) ui.Offset {
		return ui.Offset{Dx: c.Dx + n.Dx*across*thick, Dy: c.Dy + n.Dy*across*thick}
	}
	a, b := at(f.from), at(f.to)
	m := at((f.from + f.to) / 2)
	ctl := ui.Offset{Dx: m.Dx + tangent.Dx*f.bend*2, Dy: m.Dy + tangent.Dy*f.bend*2}
	path := ui.NewPath()
	path.MoveTo(a.Dx*u, a.Dy*u)
	path.QuadraticBezierTo(ctl.Dx*u, ctl.Dy*u, b.Dx*u, b.Dy*u)
	return path
}

// paintAppIconHighlight draws the small glossy highlight on the upper left of the body: a tilted oval along the body
// and a dot after it, which make the fur look damp.
func paintAppIconHighlight(canvas *ui.Canvas, thick, u float64) {
	c, n := appIconBodyPoint(0.27)
	x := c.Dx - n.Dx*thick*0.45
	y := c.Dy - n.Dy*thick*0.45
	gloss := ui.NewPaint()
	gloss.SetColor(appIconHighlight.WithOpacity(0.62))
	canvas.Save()
	canvas.Translate(x*u, y*u)
	canvas.Rotate(math.Atan2(-n.Dx, n.Dy) - 0.05)
	canvas.DrawOval(ui.NewRectFromCenter(ui.Offset{}, 52*u, 17*u), gloss)
	canvas.DrawCircle(ui.Offset{Dx: 38 * u, Dy: 2 * u}, 5.5*u, gloss)
	canvas.Restore()
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
