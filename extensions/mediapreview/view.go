package mediapreview

import (
	"math"
	"runtime"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/svg"
	gui "github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension/ui"
)

// The widget of the image preview (SPEC §10.37): the image on VS Code's transparency checkerboard, fitted or zoomed, and the
// messages of an image that cannot be shown.
//
// Ported from VS Code: extensions/media-preview/media/imagePreview.js, imagePreview.css

// The checkerboard of VS Code's imagePreview.css: squares of 8 pixels in tiles of 16, rgb(230, 230, 230) on light themes and
// rgb(20, 20, 20) on dark ones, over the editor background.
var (
	checkerLight = gui.NewColorFromARGB(0xff, 230, 230, 230)
	checkerDark  = gui.NewColorFromARGB(0xff, 20, 20, 20)
)

const (
	checkerSquare = 8
	// dragSlop is how far the pointer moves before a press is a drag, not a click.
	dragSlop = 4
	// minFrameTime is the time a frame with no duration is shown; Glutter's codec already gives the frames of a GIF with a delay of
	// 10 ms or less 100 ms, as browsers do.
	minFrameTime = 100 * time.Millisecond
)

// build is the Build of the tab.
func (p *preview) build(widgets.BuildContext) widgets.Widget {
	return &imageView{StatefulWidget: widgets.NewStatefulWidgetBase(), p: p}
}

// imageView is the widget of a preview; its state is the preview's.
type imageView struct {
	widgets.StatefulWidget
	p *preview
}

// CreateState implements widgets.StatefulWidget.
func (w *imageView) CreateState() widgets.State { return &viewState{} }

// viewState is the state of the widget: the press of the pointer, the pinch and the timer of the next frame of an animation.
type viewState struct {
	widgets.StateOf[*imageView]
	pressed       bool
	moved         bool
	pressAt, last gui.Offset
	pinchFrom     float64
	stopTimer     func()
	timerFrame    int
}

// InitState implements widgets.State.
func (s *viewState) InitState() { s.Widget().p.view = s }

// DidUpdateWidget implements widgets.State.
func (s *viewState) DidUpdateWidget(old *imageView) {
	if old.p != s.Widget().p {
		if old.p.view == s {
			old.p.view = nil
		}
		s.Widget().p.view = s
	}
}

// Dispose implements widgets.State.
func (s *viewState) Dispose() {
	s.stopAnimation()
	if p := s.Widget().p; p.view == s {
		p.view = nil
	}
}

// changed builds the widget again.
func (s *viewState) changed() {
	if s.Mounted() {
		s.SetState(func() {})
	}
}

// stopAnimation stops the timer of the next frame.
func (s *viewState) stopAnimation() {
	if s.stopTimer != nil {
		s.stopTimer()
		s.stopTimer = nil
	}
}

// Build implements widgets.State.
func (s *viewState) Build(bc widgets.BuildContext) widgets.Widget {
	p := s.Widget().p
	colors := ui.Theme(bc)
	var body widgets.Widget
	switch {
	case p.deleted:
		s.stopAnimation()
		body = message(bc, "The file was deleted.", "", nil)
	case p.err != nil:
		s.stopAnimation()
		text := "An error occurred while loading the image."
		if e, ok := errors.FromError(p.err); ok && (e.Tag == "ImageTooLarge" || e.Tag == "ImageFileTooLarge") {
			text = e.Message
		}
		body = message(bc, text, "Open file using Emetos's standard text/binary editor?", p.tab.ReopenAsText)
	case p.img == nil:
		body = widgets.NewSizedBox(nil)
	default:
		body = widgets.NewLayoutBuilder(s.layout)
	}
	return widgets.NewColoredBox(colors.EditorBackground, body)
}

// message is a line of text in the middle of the view, with a link under it when link is set.
func message(bc widgets.BuildContext, text, link string, onLink func()) widgets.Widget {
	colors := ui.Theme(bc)
	children := []widgets.Widget{widgets.NewText(text, widgets.TextOpts{Style: ui.TextStyle(bc, 13, colors.Foreground), TextAlign: gui.TextAlignCenter})}
	if link != "" {
		style := ui.TextStyle(bc, 13, colors.TextLinkForeground).CopyWith(painting.TextStyleCopy{Decoration: foundation.Some(gui.TextDecorationUnderline)})
		children = append(children, widgets.NewSizedBox(nil, widgets.SizedBoxOpts{Height: foundation.Some(8.0)}),
			widgets.NewMouseRegion(widgets.NewGestureDetector(widgets.NewText(link, widgets.TextOpts{Style: style}),
				widgets.GestureDetectorOpts{OnTap: onLink}), widgets.MouseRegionOpts{Cursor: services.SystemMouseCursors.Click}))
	}
	return widgets.NewCenter(widgets.NewPadding(painting.NewEdgeInsetsAll(16),
		widgets.NewColumn(children, widgets.ColumnOpts{MainAxisSize: rendering.MainAxisSizeMin})))
}

// layout draws the image in a view of the constraints' size.
func (s *viewState) layout(bc widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
	p := s.Widget().p
	img := p.img
	p.viewW, p.viewH = c.MaxWidth, c.MaxHeight
	scale := p.shownScale()
	w, h := float64(img.Width), float64(img.Height)
	x, y, offX, offY := Place(w, h, scale, p.viewW, p.viewH, p.offX, p.offY)
	if !p.fit {
		p.offX, p.offY = offX, offY
	}
	dw, dh := w*scale, h*scale
	var picture widgets.Widget
	if img.SVG != nil {
		picture = svg.NewSvgPictureMemory(img.Source, svg.SvgPictureOpts{Fit: painting.BoxFitFill})
	} else {
		picture = widgets.NewRawImage(img.Frames[min(p.frame, len(img.Frames)-1)].Image,
			widgets.RawImageOpts{Scale: 1, Fit: painting.BoxFitFill, FilterQuality: filterQuality(scale)})
		s.scheduleFrame()
	}
	checker := checkerLight
	if isDark(ui.Theme(bc).EditorBackground) {
		checker = checkerDark
	}
	content := widgets.NewStack([]widgets.Widget{
		widgets.NewCustomPaint(nil, widgets.CustomPaintOpts{Painter: &checkerPainter{CustomPainterBase: rendering.NewCustomPainterBase(nil),
			rect: gui.NewRectFromLTWH(x, y, dw, dh), color: checker}, Size: gui.Size{Width: p.viewW, Height: p.viewH}}),
		widgets.NewPositioned(picture, widgets.PositionedOpts{Left: foundation.Some(x), Top: foundation.Some(y),
			Width: foundation.Some(dw), Height: foundation.Some(dh)}),
	}, widgets.StackOpts{ClipBehavior: gui.ClipHardEdge})
	return widgets.NewListener(widgets.NewSizedBox(content, widgets.SizedBoxOpts{Width: foundation.Some(p.viewW), Height: foundation.Some(p.viewH)}),
		widgets.ListenerOpts{
			Behavior:               rendering.HitTestBehaviorOpaque,
			OnPointerDown:          s.pointerDown,
			OnPointerMove:          s.pointerMove,
			OnPointerUp:            s.pointerUp,
			OnPointerSignal:        s.pointerSignal,
			OnPointerPanZoomStart:  s.panZoomStart,
			OnPointerPanZoomUpdate: s.panZoomUpdate,
		})
}

// filterQuality is how the pixels are drawn at a scale: smoothly, and as squares from PixelationThreshold (VS Code's
// image-rendering: pixelated).
func filterQuality(scale float64) gui.FilterQuality {
	if scale >= PixelationThreshold {
		return gui.FilterQualityNone
	}
	return gui.FilterQualityLow
}

// zoomOutIsAlt tells that the key of Zoom Out is Alt (macOS), not Ctrl.
func zoomOutIsAlt() bool { return runtime.GOOS == "darwin" }

// isDark tells a dark background from a light one by its luminance.
func isDark(c gui.Color) bool {
	r, g, b := float64(c.Red()), float64(c.Green()), float64(c.Blue())
	return 0.299*r+0.587*g+0.114*b < 128
}

// zoomOutKey tells whether the key that turns a click into Zoom Out (and the wheel into zoom) is held: Alt on macOS, Ctrl elsewhere,
// as VS Code's imagePreview.js.
func (s *viewState) zoomOutKey() bool {
	kb := widgets.WidgetsBindingOf(s.Context()).Keyboard()
	if zoomOutIsAlt() {
		return kb.IsAltPressed()
	}
	return kb.IsControlPressed()
}

func (s *viewState) pointerDown(e *gestures.PointerDownEvent) {
	if e.Buttons() != gestures.PrimaryButton {
		return
	}
	s.pressed, s.moved, s.pressAt, s.last = true, false, e.LocalPosition(), e.LocalPosition()
}

func (s *viewState) pointerMove(e *gestures.PointerMoveEvent) {
	if !s.pressed {
		return
	}
	pos := e.LocalPosition()
	if !s.moved && math.Hypot(pos.Dx-s.pressAt.Dx, pos.Dy-s.pressAt.Dy) > dragSlop {
		s.moved = true
	}
	if s.moved {
		s.Widget().p.scrollBy(s.last.Dx-pos.Dx, s.last.Dy-pos.Dy)
	}
	s.last = pos
}

// pointerUp is a click when the pointer did not move: Zoom In at the pointer, Zoom Out with the key.
func (s *viewState) pointerUp(e *gestures.PointerUpEvent) {
	if !s.pressed {
		return
	}
	s.pressed = false
	if s.moved {
		return
	}
	p, pos := s.Widget().p, e.LocalPosition()
	from := p.shownScale()
	if s.zoomOutKey() {
		p.setScaleAt(ZoomOut(from), pos.Dx, pos.Dy)
	} else {
		p.setScaleAt(ZoomIn(from), pos.Dx, pos.Dy)
	}
}

// pointerSignal is the wheel: zoom at the pointer with the key (a pinch of a touchpad comes as Ctrl and the wheel), else scrolling a
// zoomed image (Shift scrolls sideways).
func (s *viewState) pointerSignal(e gestures.PointerSignalEvent) {
	scroll, ok := e.(*gestures.PointerScrollEvent)
	if !ok {
		return
	}
	p, d, pos := s.Widget().p, scroll.ScrollDelta(), scroll.LocalPosition()
	kb := widgets.WidgetsBindingOf(s.Context()).Keyboard()
	if s.zoomOutKey() || kb.IsControlPressed() {
		if d.Dy != 0 {
			p.setScaleAt(WheelZoom(p.shownScale(), d.Dy), pos.Dx, pos.Dy)
		}
		return
	}
	if kb.IsShiftPressed() && d.Dx == 0 {
		d = gui.Offset{Dx: d.Dy}
	}
	p.scrollBy(d.Dx, d.Dy)
}

func (s *viewState) panZoomStart(*gestures.PointerPanZoomStartEvent) {
	s.pinchFrom = s.Widget().p.shownScale()
}

// panZoomUpdate is a pinch or a two-finger pan of a touchpad.
func (s *viewState) panZoomUpdate(e *gestures.PointerPanZoomUpdateEvent) {
	p := s.Widget().p
	if e.Scale() != 1 && s.pinchFrom > 0 {
		pos := e.LocalPosition()
		p.setScaleAt(s.pinchFrom*e.Scale(), pos.Dx, pos.Dy)
		return
	}
	d := e.LocalPanDelta()
	p.scrollBy(-d.Dx, -d.Dy)
}

// scheduleFrame starts the timer of the next frame of an animated image, once per frame. An animation stops after its last repeat,
// on its last frame.
func (s *viewState) scheduleFrame() {
	p := s.Widget().p
	img := p.img
	if len(img.Frames) < 2 || (s.stopTimer != nil && s.timerFrame == p.frame) {
		return
	}
	if img.Repeat >= 0 && p.plays > img.Repeat {
		return
	}
	s.stopAnimation()
	d := img.Frames[p.frame].Duration
	if d <= 0 {
		d = minFrameTime
	}
	s.timerFrame = p.frame
	s.stopTimer = widgets.WidgetsBindingOf(s.Context()).Painting().AfterFunc(d, func() {
		s.stopTimer = nil
		if !s.Mounted() || p.img != img {
			return
		}
		next := p.frame + 1
		if next == len(img.Frames) {
			p.plays++
			if img.Repeat >= 0 && p.plays > img.Repeat {
				return // stays on the last frame
			}
			next = 0
		}
		s.SetState(func() { p.frame = next })
	})
}

// checkerPainter draws the checkerboard under the part of the image that is in view, its squares counted from the corner of the
// image so they move with it.
type checkerPainter struct {
	rendering.CustomPainterBase
	rect  gui.Rect
	color gui.Color
}

// Paint implements rendering.CustomPainter.
func (c *checkerPainter) Paint(canvas *gui.Canvas, size gui.Size) {
	left, top := math.Max(c.rect.Left, 0), math.Max(c.rect.Top, 0)
	right, bottom := math.Min(c.rect.Right, size.Width), math.Min(c.rect.Bottom, size.Height)
	if right <= left || bottom <= top {
		return
	}
	paint := gui.NewPaint()
	paint.SetColor(c.color)
	canvas.Save()
	canvas.ClipRect(gui.NewRectFromLTRB(left, top, right, bottom))
	// the first square at or before the left and top edges of the view, on the grid of the image
	i0 := math.Floor((left - c.rect.Left) / checkerSquare)
	j0 := math.Floor((top - c.rect.Top) / checkerSquare)
	for j := j0; c.rect.Top+j*checkerSquare < bottom; j++ {
		for i := i0; c.rect.Left+i*checkerSquare < right; i++ {
			// VS Code's conic gradient colors the top right and bottom left squares of each tile
			if (int(i)+int(j))%2 == 1 {
				canvas.DrawRect(gui.NewRectFromLTWH(c.rect.Left+i*checkerSquare, c.rect.Top+j*checkerSquare, checkerSquare, checkerSquare), paint)
			}
		}
	}
	canvas.Restore()
}

// ShouldRepaint implements rendering.CustomPainter.
func (c *checkerPainter) ShouldRepaint(old rendering.CustomPainter) bool {
	o, ok := old.(*checkerPainter)
	return !ok || o.rect != c.rect || o.color != c.color
}
