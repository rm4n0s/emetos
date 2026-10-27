package kit

import (
	"math"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// The application icon (SPEC §10.14, §17): a cat's hairball, the thing a cat throws up ("emetos" is Greek for
// vomiting), drawn as a round ball of ginger-and-white hair, like a basketball, on a cat-head silhouette in soft teal (a
// rounded square with two ears: one shape with one gradient and one border). The ball is a sphere seen from the
// front. About ninety curved hair strokes wrap around it, following its roundness like yarn on a ball of wool, in six
// colors from deep orange to white: the ones facing the light at the top left are pale, the ones turned away at the
// bottom right are deep orange. A soft gradient shades the ball under the hair, and
// a small glossy highlight at the top left makes it look damp. Its edge is fuzzy, not outlined: short hairs stick out
// all around it, and a few longer strands stray. It is drawn with Glutter's canvas, with paths and gradients only (no
// font, no bitmap), so the picture is the same on every machine; the strokes come from a generator with a fixed seed.
// The test of this package renders it at AppIconSizes into assets/icons/, and the application embeds those pictures
// for the window and the system tray.

// AppIconSizes are the sizes in pixels of the pictures of the application icon: the sizes of the tray (16, 22, 24, 32
// and 48), the ones of the icon theme folders (64, 128, 256 and 512).
var AppIconSizes = []int{16, 22, 24, 32, 48, 64, 128, 256, 512}

// The colors of the icon. The head is a gradient from a lighter teal at the top to a deeper one at the bottom, with a
// darker teal border; orange and white fur stands out on teal, and teal reads on a light and on a dark panel. The
// inside of the ears is a deeper teal. The fur is six colors from a deep orange to white.
var (
	appIconTop       = ui.NewColor(0xFF7FD6C8)
	appIconBottom    = ui.NewColor(0xFF2F9A93)
	appIconBorder    = ui.NewColor(0xFF1F6F69)
	appIconEarInner  = ui.NewColor(0xFF2A8A83)
	appIconHighlight = ui.NewColor(0xFFFFFFFF)

	// appIconFurColors are the colors of the hairs, from dark to light.
	appIconFurColors = [...]ui.Color{
		ui.NewColor(0xFFD8701A), // deep orange
		ui.NewColor(0xFFEE8A2A), // orange
		ui.NewColor(0xFFF7A750), // light orange
		ui.NewColor(0xFFFBC385), // apricot
		ui.NewColor(0xFFFFEBCE), // cream
		ui.NewColor(0xFFFFFFFF), // white
	}
)

// The geometry of the icon in a square of 512 units.
const (
	appIconUnits  = 512.0
	appIconRadius = 96.0  // the corner radius of the head
	appIconTopY   = 104.0 // the top edge of the head between the ears; the ear tips are at 14
	appIconBallX  = 256.0 // the center of the ball
	appIconBallY  = 318.0
	appIconBallR  = 162.0 // its radius: with the fuzz it is about 70% of the width
	appIconSteps  = 18    // the points of a hair
)

// appIconLight is the unit vector to the light: from the top left, towards the viewer (x right, y down, z to the
// viewer).
var appIconLight = appIconUnit3{-0.55, -0.6, 0.58}.norm()

// appIconUnit3 is a vector in the space of the ball.
type appIconUnit3 struct{ x, y, z float64 }

func (a appIconUnit3) norm() appIconUnit3 {
	l := math.Sqrt(a.x*a.x + a.y*a.y + a.z*a.z)
	return appIconUnit3{a.x / l, a.y / l, a.z / l}
}

func (a appIconUnit3) dot(b appIconUnit3) float64 { return a.x*b.x + a.y*b.y + a.z*b.z }

func (a appIconUnit3) cross(b appIconUnit3) appIconUnit3 {
	return appIconUnit3{a.y*b.z - a.z*b.y, a.z*b.x - a.x*b.z, a.x*b.y - a.y*b.x}
}

// appIconRand is a small generator with a fixed seed, so that the hairs are the same on every run.
type appIconRand uint32

// next returns the next number in [0, 1).
func (r *appIconRand) next() float64 {
	*r = *r*1664525 + 1013904223
	return float64(*r>>8) / (1 << 24)
}

// span returns a number in [lo, hi).
func (r *appIconRand) span(lo, hi float64) float64 { return lo + (hi-lo)*r.next() }

// appIconStrand is a hair on the ball: points on the unit sphere (x, y to the right and down, z to the viewer; all in
// front of it), the color index of appIconFurColors, the width in units and the detail level that first shows it
// (see appIconDetail).
type appIconStrand struct {
	pts   []appIconUnit3
	color int
	width float64
	level int
}

// appIconCircle returns the points of an arc of a circle on the unit sphere: the circle of the points at the angle phi
// from the axis n, from the angle t0 to t1 around it, with a small wobble of the radius so that a hair is not a perfect
// curve. Only the points in front of the sphere (z above a little) are kept, as the longest run.
func appIconCircle(n appIconUnit3, phi, t0, t1, wobble, freq, ph float64) []appIconUnit3 {
	n = n.norm()
	h := appIconUnit3{0, 0, 1}
	if math.Abs(n.z) > 0.9 {
		h = appIconUnit3{1, 0, 0}
	}
	e1 := h.cross(n).norm()
	e2 := n.cross(e1)
	const samples = 96
	var best, run []appIconUnit3
	for i := 0; i <= samples; i++ {
		t := t0 + (t1-t0)*float64(i)/samples
		w := 1 + wobble*math.Sin(freq*t+ph)
		cp, sp, ct, st := math.Cos(phi), math.Sin(phi), math.Cos(t), math.Sin(t)
		p := appIconUnit3{
			(cp*n.x + sp*(ct*e1.x+st*e2.x)) * w,
			(cp*n.y + sp*(ct*e1.y+st*e2.y)) * w,
			(cp*n.z + sp*(ct*e1.z+st*e2.z)) * w,
		}
		if p.z > 0.04 {
			run = append(run, p)
			continue
		}
		if len(run) > len(best) {
			best = run
		}
		run = nil
	}
	if len(run) > len(best) {
		best = run
	}
	return best
}

// appIconResample picks about count points evenly from a run of points.
func appIconResample(pts []appIconUnit3, count int) []appIconUnit3 {
	if len(pts) <= count {
		return pts
	}
	out := make([]appIconUnit3, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, pts[i*(len(pts)-1)/(count-1)])
	}
	return out
}

// appIconFurColor picks the color of a hair from how much it faces the light, with some noise: pale at the top left,
// deep orange at the bottom right.
func appIconFurColor(light, noise float64) int {
	v := 0.42 + 0.5*light + noise
	return int(math.Min(5, math.Max(0, math.Floor(v*6))))
}

// appIconStrands are the hairs wrapped around the ball, in the order they are drawn. The first ones are what the small
// sizes show.
var appIconStrands = func() []appIconStrand {
	r := appIconRand(31026)
	var out []appIconStrand
	for i := 0; i < 96; i++ {
		// a circle on the sphere with a random axis: a great circle hugs the edge, a smaller one makes a tighter curl
		n := appIconUnit3{r.span(-1, 1), r.span(-1, 1), r.span(-0.6, 1)}
		phi := r.span(0.55, 1.45)
		t0 := r.span(0, 2*math.Pi)
		pts := appIconCircle(n, phi, t0, t0+2*math.Pi, r.span(0.004, 0.02), r.span(5, 14), r.span(0, 6.28))
		if len(pts) < 14 {
			i--
			continue
		}
		// a stretch of the visible run, not all of it, so the hairs cross and end at different places
		keep := int(float64(len(pts)) * r.span(0.5, 1))
		start := int(float64(len(pts)-keep) * r.next())
		pts = appIconResample(pts[start:start+keep], appIconSteps)
		mid := pts[len(pts)/2]
		st := appIconStrand{
			pts:   pts,
			color: appIconFurColor(mid.dot(appIconLight), r.span(-0.2, 0.2)),
			width: r.span(5, 8.5),
			level: 2,
		}
		switch {
		case len(out) < 20:
			st.level = 0
		case len(out) < 52:
			st.level = 1
		}
		out = append(out, st)
	}
	return out
}()

// appIconFuzz is a hair at the edge of the ball: it starts inside the edge at the angle ang and sticks out len units
// (of 512), bent by curl; color indexes appIconFurColors; level is the detail level that first shows it.
type appIconFuzz struct {
	ang, length, curl float64
	color, level      int
}

// appIconFuzzes are the short hairs sticking out around the ball (the first level-1 ones every third), then the long
// stray ones.
var appIconFuzzes = func() []appIconFuzz {
	r := appIconRand(90210)
	var out []appIconFuzz
	const n = 96
	for i := 0; i < n; i++ {
		ang := 2 * math.Pi * (float64(i) + r.span(-0.4, 0.4)) / n
		light := -(math.Cos(ang)*appIconLight.x + math.Sin(ang)*appIconLight.y) / math.Hypot(appIconLight.x, appIconLight.y)
		f := appIconFuzz{
			ang:    ang,
			length: r.span(14, 32),
			curl:   r.span(-0.5, 0.5),
			color:  appIconFurColor(-light*0.8, r.span(-0.2, 0.25)),
			level:  2,
		}
		if i%3 == 0 {
			f.level = 1
		}
		out = append(out, f)
	}
	for _, ang := range []float64{3.5, 5.6, 0.9, 2.4} {
		out = append(out, appIconFuzz{ang: ang, length: r.span(52, 72), curl: r.span(0.35, 0.6) * map[bool]float64{true: 1, false: -1}[ang > 3], color: 4, level: 1})
	}
	return out
}()

// appIconDetail gives the detail level for a size in pixels: 0 below 32 px (twenty thick hairs and the
// highlight on a disc, no fuzz), 1 up to 63 px (about fifty hairs, every third short hair, the strays), 2 from 64 px
// (all of them).
func appIconDetail(s float64) int {
	switch {
	case s < 32:
		return 0
	case s < 64:
		return 1
	}
	return 2
}

// appIconPoint maps a point of the unit sphere to pixels.
func appIconPoint(p appIconUnit3, u float64) ui.Offset {
	return ui.Offset{Dx: (appIconBallX + p.x*appIconBallR*0.985) * u, Dy: (appIconBallY + p.y*appIconBallR*0.985) * u}
}

// appIconSmooth joins points by quadratic curves through their middles.
func appIconSmooth(pts []ui.Offset) *ui.Path {
	mid := func(a, b ui.Offset) ui.Offset { return ui.Offset{Dx: (a.Dx + b.Dx) / 2, Dy: (a.Dy + b.Dy) / 2} }
	path := ui.NewPath()
	path.MoveTo(pts[0].Dx, pts[0].Dy)
	for i := 1; i+1 < len(pts); i++ {
		m := mid(pts[i], pts[i+1])
		path.QuadraticBezierTo(pts[i].Dx, pts[i].Dy, m.Dx, m.Dy)
	}
	last := pts[len(pts)-1]
	path.LineTo(last.Dx, last.Dy)
	return path
}

// appIconSpherePath is the path of points on the unit sphere in pixels.
func appIconSpherePath(pts []appIconUnit3, u float64) *ui.Path {
	px := make([]ui.Offset, len(pts))
	for i, p := range pts {
		px[i] = appIconPoint(p, u)
	}
	return appIconSmooth(px)
}

// appIconFuzzPath is the path of a hair at the edge of the ball in pixels.
func appIconFuzzPath(f appIconFuzz, u float64) *ui.Path {
	dx, dy := math.Cos(f.ang), math.Sin(f.ang)
	start := ui.Offset{Dx: appIconBallX + dx*appIconBallR*0.9, Dy: appIconBallY + dy*appIconBallR*0.9}
	l := appIconBallR*0.1 + f.length
	end := ui.Offset{Dx: start.Dx + dx*l, Dy: start.Dy + dy*l}
	ctl := ui.Offset{
		Dx: (start.Dx+end.Dx)/2 - dy*f.curl*l,
		Dy: (start.Dy+end.Dy)/2 + dx*f.curl*l,
	}
	path := ui.NewPath()
	path.MoveTo(start.Dx*u, start.Dy*u)
	path.QuadraticBezierTo(ctl.Dx*u, ctl.Dy*u, end.Dx*u, end.Dy*u)
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
	paintAppIconHead(canvas, s, u)
	paintAppIconBall(canvas, s, u)
}

// appIconHeadPath is the cat head in pixels, one closed path: a rounded square with an ear on each top corner. The
// ear tips are a little rounded.
func appIconHeadPath(u float64) *ui.Path {
	const (
		r    = appIconRadius
		top  = appIconTopY
		edge = appIconUnits
		k    = 0.70710678 // the weight of a conic that makes a circular corner
	)
	p := ui.NewPath()
	// tip rounds the corner at the apex between two points a few units before and after it
	tip := func(ax, ay, fromX, fromY, toX, toY float64) {
		const cut = 16.0
		toward := func(x, y float64) (float64, float64) {
			d := math.Hypot(x-ax, y-ay)
			return ax + (x-ax)/d*cut, ay + (y-ay)/d*cut
		}
		x1, y1 := toward(fromX, fromY)
		x2, y2 := toward(toX, toY)
		p.LineTo(x1*u, y1*u)
		p.QuadraticBezierTo(ax*u, ay*u, x2*u, y2*u)
	}
	p.MoveTo(r*u, edge*u)
	p.LineTo((edge-r)*u, edge*u)
	p.ConicTo(edge*u, edge*u, edge*u, (edge-r)*u, k)
	p.LineTo(edge*u, 150*u)
	tip(468, 14, edge, 150, 316, top)
	p.LineTo(316*u, top*u)
	p.LineTo(196*u, top*u)
	tip(44, 14, 196, top, 0, 150)
	p.LineTo(0, 150*u)
	p.LineTo(0, (edge-r)*u)
	p.ConicTo(0, edge*u, r*u, edge*u, k)
	p.Close()
	return p
}

// appIconEarInnerPath is the inside of an ear in pixels; the right ear is the mirror of the left.
func appIconEarInnerPath(u float64, left bool) *ui.Path {
	pts := [][2]float64{{28, 128}, {50, 52}, {146, 106}}
	path := ui.NewPath()
	for i, p := range pts {
		x := p[0]
		if !left {
			x = appIconUnits - x
		}
		if i == 0 {
			path.MoveTo(x*u, p[1]*u)
		} else {
			path.LineTo(x*u, p[1]*u)
		}
	}
	path.Close()
	return path
}

// paintAppIconHead draws the cat head: one path with a vertical gradient and one border that is one pixel wide up to
// 128 px and grows with the picture after that, then the deeper inside of the ears.
func paintAppIconHead(canvas *ui.Canvas, s, u float64) {
	border := max(1, s/128)
	head := appIconHeadPath(u)

	fill := ui.NewPaint()
	fill.SetShader(ui.NewGradientLinear(ui.Offset{Dy: 14 * u}, ui.Offset{Dy: s}, []ui.Color{appIconTop, appIconBottom}))
	canvas.DrawPath(head, fill)

	inside := ui.NewPaint()
	inside.SetColor(appIconEarInner)
	canvas.DrawPath(appIconEarInnerPath(u, true), inside)
	canvas.DrawPath(appIconEarInnerPath(u, false), inside)

	edge := ui.NewPaint()
	edge.SetColor(appIconBorder)
	edge.SetStyle(ui.PaintingStyleStroke)
	edge.SetStrokeWidth(border * 2)
	edge.SetStrokeJoin(ui.StrokeJoinRound)
	// the border is drawn inside the head: clip to it, and stroke twice as wide, so half of it shows
	canvas.Save()
	canvas.ClipPath(head)
	canvas.DrawPath(head, edge)
	canvas.Restore()
}

// paintAppIconBall draws the ball: the shaded disc, the hairs wrapped around it, the fuzz at the edge and the highlight.
func paintAppIconBall(canvas *ui.Canvas, s, u float64) {
	detail := appIconDetail(s)
	// the small sizes get thicker hairs, so that they fill the few pixels there are
	widen := map[int]float64{0: 3.6, 1: 1.7, 2: 1}[detail]
	center := ui.Offset{Dx: appIconBallX * u, Dy: appIconBallY * u}
	radius := appIconBallR * u

	// the fuzz first, so that it comes out from under the edge
	stroke := ui.NewPaint()
	stroke.SetStyle(ui.PaintingStyleStroke)
	stroke.SetStrokeCap(ui.StrokeCapRound)
	for _, f := range appIconFuzzes {
		if f.level > detail {
			continue
		}
		stroke.SetColor(appIconFurColors[f.color])
		stroke.SetStrokeWidth(max(1, 3.4*widen*u))
		canvas.DrawPath(appIconFuzzPath(f, u), stroke)
	}

	// the disc, shaded: pale apricot at the top left, deep orange at the bottom right
	disc := ui.NewPaint()
	light := ui.Offset{Dx: center.Dx - 0.4*radius, Dy: center.Dy - 0.45*radius}
	disc.SetShader(ui.NewGradientRadial(light, 1.6*radius,
		[]ui.Color{appIconFurColors[4], appIconFurColors[3], appIconFurColors[1], appIconFurColors[0]},
		ui.GradientRadialOpts{ColorStops: []float64{0, 0.35, 0.75, 1}}))
	canvas.DrawCircle(center, radius*0.975, disc)

	for _, st := range appIconStrands {
		if st.level > detail {
			continue
		}
		stroke.SetColor(appIconFurColors[st.color])
		stroke.SetStrokeWidth(max(1, st.width*widen*u))
		canvas.DrawPath(appIconSpherePath(st.pts, u), stroke)
	}

	// the highlight: a small glossy oval at the top left of the ball, and a dot
	gloss := ui.NewPaint()
	gloss.SetColor(appIconHighlight.WithOpacity(0.6))
	canvas.Save()
	canvas.Translate(center.Dx-0.48*radius, center.Dy-0.52*radius)
	canvas.Rotate(-0.75)
	canvas.DrawOval(ui.NewRectFromCenter(ui.Offset{}, 0.34*radius, 0.12*radius), gloss)
	canvas.DrawCircle(ui.Offset{Dx: 0.26 * radius, Dy: 0.01 * radius}, 0.035*radius, gloss)
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
