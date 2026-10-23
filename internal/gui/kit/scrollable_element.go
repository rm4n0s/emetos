package kit

import (
	"math"
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// Timing of the scrollbars, from VS Code.
const (
	// scrollbarHideDelay is how long the scrollbars stay after the last scroll or when the
	// pointer left the element.
	scrollbarHideDelay = 500 * time.Millisecond
	// scrollbarRevealDuration is how long a scrollbar takes to appear.
	scrollbarRevealDuration = 100 * time.Millisecond
	// scrollbarFadeDuration is how long a scrollbar takes to fade away.
	scrollbarFadeDuration = 800 * time.Millisecond
)

// Sizes of the scrollbars of an editor, from VS Code (editor.scrollbar.*).
const (
	// DefaultVerticalScrollbarSize is the width of the vertical scrollbar.
	DefaultVerticalScrollbarSize = 14.0
	// DefaultHorizontalScrollbarSize is the height of the horizontal scrollbar.
	DefaultHorizontalScrollbarSize = 12.0
)

// scrollShadowSize is the size of the shadow at the start of content that is scrolled.
const scrollShadowSize = 4.0

// ScrollbarVisibility says when a scrollbar is shown.
type ScrollbarVisibility int

const (
	// ScrollbarAuto shows the scrollbar when the content is longer than the viewport and the
	// pointer is over the element or the content scrolled a moment ago.
	ScrollbarAuto ScrollbarVisibility = iota
	// ScrollbarVisible shows the scrollbar whenever the content is longer than the viewport.
	ScrollbarVisible
	// ScrollbarHidden never shows the scrollbar.
	ScrollbarHidden
)

// ScrollMetrics are the sizes and the position of what a ScrollableElement scrolls, in
// pixels. The viewport is the part of the element the content is seen through (without the
// inset of the element).
type ScrollMetrics struct {
	ViewportWidth, ViewportHeight float64
	ContentWidth, ContentHeight   float64
	// Left and Top are the scroll position.
	Left, Top float64
}

// ScrollModel is what a ScrollableElement scrolls: the editor's text, a list. The element
// draws scrollbars from the model's metrics and moves them through ScrollTo; it does not
// scroll its child itself.
type ScrollModel interface {
	// AddListener calls fn when the metrics may have changed. fn may be called during layout.
	AddListener(fn foundation.VoidCallback) foundation.ListenerHandle
	// RemoveListener removes a listener.
	RemoveListener(h foundation.ListenerHandle)
	// Metrics reads the metrics now.
	Metrics() ScrollMetrics
	// ScrollTo moves the content to a position; the model keeps it within the content.
	ScrollTo(left, top float64)
}

// ScrollableElementOpts configure a ScrollableElement.
type ScrollableElementOpts struct {
	// Vertical and Horizontal say when each scrollbar is shown; the default is ScrollbarAuto.
	Vertical, Horizontal ScrollbarVisibility
	// VerticalSize is the width of the vertical scrollbar; the default is 14.
	VerticalSize foundation.Opt[float64]
	// HorizontalSize is the height of the horizontal scrollbar; the default is 12.
	HorizontalSize foundation.Opt[float64]
	// VerticalSliderSize is the width of the slider of the vertical scrollbar; the default is
	// VerticalSize.
	VerticalSliderSize foundation.Opt[float64]
	// HorizontalSliderSize is the height of the slider of the horizontal scrollbar; the
	// default is HorizontalSize.
	HorizontalSliderSize foundation.Opt[float64]
	// ScrollByPage makes a click on the track move by a page; otherwise the slider jumps to
	// the click (VS Code's default).
	ScrollByPage bool
	// InsetLeft is the width on the left of the element that is not part of the viewport,
	// such as the gutter of an editor: the horizontal scrollbar starts after it.
	InsetLeft float64
	// UseShadows draws a shadow at the top and at the left of content that is scrolled.
	UseShadows bool
}

func (o ScrollableElementOpts) verticalSize() float64 {
	return max(o.VerticalSize.OrElse(DefaultVerticalScrollbarSize), 0)
}

func (o ScrollableElementOpts) horizontalSize() float64 {
	return max(o.HorizontalSize.OrElse(DefaultHorizontalScrollbarSize), 0)
}

// ScrollableElement puts VS Code-style overlay scrollbars over a child: a vertical one at the
// right and a horizontal one at the bottom, in the colors of the theme. They appear when the
// pointer is over the element or the content scrolls, fade away half a second after, move the
// content when their slider is dragged and when the track is clicked, and never take room from
// the child. The scrolling itself is the model's.
//
// Ported from VS Code: src/vs/base/browser/ui/scrollbar/ (scrollableElement.ts, abstractScrollbar.ts, verticalScrollbar.ts, horizontalScrollbar.ts, scrollbarVisibilityController.ts)
type ScrollableElement struct {
	widgets.StatefulWidget
	child widgets.Widget
	model ScrollModel
	opts  ScrollableElementOpts
}

// NewScrollableElement puts scrollbars over a child, driven by a model.
func NewScrollableElement(child widgets.Widget, model ScrollModel, opts ...ScrollableElementOpts) *ScrollableElement {
	var o ScrollableElementOpts
	if len(opts) > 0 {
		o = opts[0]
	}
	return &ScrollableElement{StatefulWidget: widgets.NewStatefulWidgetBase(), child: child, model: model, opts: o}
}

// CreateState implements widgets.StatefulWidget.
func (w *ScrollableElement) CreateState() widgets.State { return &scrollableElementState{} }

// axis is one of the two scrollbars.
type axis int

const (
	axisVertical axis = iota
	axisHorizontal
)

// drag is a slider being dragged.
type drag struct {
	axis axis
	// start is where the pointer was along the axis, and state the scrollbar then.
	start float64
	state *ScrollbarState
}

type scrollableElementState struct {
	widgets.StateOf[*ScrollableElement]
	widgets.SingleTickerProvider
	fade        *animation.AnimationController
	binding     *widgets.WidgetsBinding
	modelHandle foundation.ListenerHandle
	hideTimer   *gestures.Timer

	mouseOver bool
	// shown is whether the scrollbars are revealed (or on their way to it).
	shown bool
	drag  *drag
	// hover is the axis whose slider the pointer is over, if any.
	hover       axis
	hovering    bool
	lastMetrics ScrollMetrics
	pending     bool
}

// InitState starts listening to the model.
func (s *scrollableElementState) InitState() {
	s.binding = widgets.WidgetsBindingOf(s.Context())
	s.fade = animation.NewAnimationController(&s.SingleTickerProvider, animation.AnimationControllerOpts{
		Duration:        foundation.Some(scrollbarRevealDuration),
		ReverseDuration: foundation.Some(scrollbarFadeDuration),
	})
	s.lastMetrics = s.Widget().model.Metrics()
	s.modelHandle = s.Widget().model.AddListener(s.modelChanged)
}

// DidUpdateWidget follows a new model.
func (s *scrollableElementState) DidUpdateWidget(old *ScrollableElement) {
	if old.model != s.Widget().model {
		old.model.RemoveListener(s.modelHandle)
		s.modelHandle = s.Widget().model.AddListener(s.modelChanged)
		s.lastMetrics = s.Widget().model.Metrics()
	}
}

// Dispose stops the timer and the listener.
func (s *scrollableElementState) Dispose() {
	s.Widget().model.RemoveListener(s.modelHandle)
	s.cancelHide()
	s.fade.Dispose()
}

// modelChanged reacts to the model: the model tells during layout, when no state may be set,
// so the scrollbars are updated after the frame. A move of the content reveals them.
func (s *scrollableElementState) modelChanged() {
	if s.pending {
		return
	}
	s.pending = true
	scheduler := s.binding.Scheduler()
	scheduler.ScheduleFrame()
	scheduler.AddPostFrameCallback(func(time.Duration) {
		s.pending = false
		if !s.Mounted() {
			return
		}
		m := s.Widget().model.Metrics()
		moved := m.Left != s.lastMetrics.Left || m.Top != s.lastMetrics.Top
		s.lastMetrics = m
		s.SetState(func() {})
		if moved {
			s.reveal()
		}
	})
}

// reveal shows the scrollbars and schedules their hiding.
func (s *scrollableElementState) reveal() {
	s.shown = true
	s.fade.Forward()
	s.scheduleHide()
}

func (s *scrollableElementState) scheduleHide() {
	if s.mouseOver || s.drag != nil {
		return
	}
	s.cancelHide()
	s.hideTimer = s.binding.Gestures().NewTimer(scrollbarHideDelay, s.hide)
}

func (s *scrollableElementState) cancelHide() {
	if s.hideTimer != nil {
		s.hideTimer.Cancel()
		s.hideTimer = nil
	}
}

// hide fades the scrollbars away unless the pointer is over the element or a slider is dragged.
func (s *scrollableElementState) hide() {
	s.hideTimer = nil
	if s.mouseOver || s.drag != nil || !s.Mounted() {
		return
	}
	s.SetState(func() { s.shown = false })
	s.fade.Reverse()
}

// states makes the state of each scrollbar from the metrics now. The vertical scrollbar has
// the whole height (it can scroll to the bottom); the horizontal one leaves room for it.
func (s *scrollableElementState) states(m ScrollMetrics) (vertical, horizontal *ScrollbarState) {
	o := s.Widget().opts
	vSize, hSize := o.verticalSize(), o.horizontalSize()
	if o.Vertical == ScrollbarHidden {
		vSize = 0
	}
	if o.Horizontal == ScrollbarHidden {
		hSize = 0
	}
	vertical = NewScrollbarState(0, vSize, 0, m.ViewportHeight, m.ContentHeight, m.Top)
	horizontal = NewScrollbarState(0, hSize, vSize, m.ViewportWidth, m.ContentWidth, m.Left)
	return vertical, horizontal
}

// scrollbarVisible tells whether an axis draws its scrollbar now.
func (s *scrollableElementState) scrollbarVisible(visibility ScrollbarVisibility, st *ScrollbarState) bool {
	return visibility != ScrollbarHidden && st.IsNeeded()
}

// scrollTo asks the model for a position along an axis and keeps the other as it is.
func (s *scrollableElementState) scrollTo(a axis, position float64) {
	m := s.Widget().model.Metrics()
	if a == axisVertical {
		s.Widget().model.ScrollTo(m.Left, position)
	} else {
		s.Widget().model.ScrollTo(position, m.Top)
	}
	s.lastMetrics = s.Widget().model.Metrics()
	s.SetState(func() {})
}

// stateOf is the scrollbar state of an axis for the metrics now.
func (s *scrollableElementState) stateOf(a axis) *ScrollbarState {
	v, h := s.states(s.Widget().model.Metrics())
	if a == axisVertical {
		return v
	}
	return h
}

// trackDown handles a pointer pressed on a track: a click outside the slider moves the
// content, and the slider can be dragged from there.
//
// Ported from VS Code: src/vs/base/browser/ui/scrollbar/abstractScrollbar.ts (_onPointerDown, _sliderPointerDown)
func (s *scrollableElementState) trackDown(a axis, e *gestures.PointerDownEvent) {
	if e.Buttons()&gestures.PrimaryButton == 0 {
		return
	}
	st := s.stateOf(a)
	local := e.LocalPosition()
	offset, pointer := local.Dy, e.Position().Dy
	if a == axisHorizontal {
		offset, pointer = local.Dx, e.Position().Dx
	}
	onSlider := offset >= st.ArrowSize()+st.SliderPosition() && offset <= st.ArrowSize()+st.SliderPosition()+st.SliderSize()
	if !onSlider {
		if s.Widget().opts.ScrollByPage {
			s.scrollTo(a, st.DesiredScrollPositionFromOffsetPaged(offset))
		} else {
			s.scrollTo(a, st.DesiredScrollPositionFromOffset(offset))
		}
		st = s.stateOf(a) // the drag goes on from where the click put the slider
	}
	s.cancelHide()
	s.SetState(func() { s.drag = &drag{axis: a, start: pointer, state: st.Clone()} })
	s.reveal()
}

// trackMove moves the content with the pointer while a slider is dragged.
func (s *scrollableElementState) trackMove(e *gestures.PointerMoveEvent) {
	if s.drag == nil {
		return
	}
	pointer := e.Position().Dy
	if s.drag.axis == axisHorizontal {
		pointer = e.Position().Dx
	}
	s.scrollTo(s.drag.axis, s.drag.state.DesiredScrollPositionFromDelta(pointer-s.drag.start))
}

// trackUp ends a drag.
func (s *scrollableElementState) trackUp() {
	if s.drag == nil {
		return
	}
	s.SetState(func() { s.drag = nil })
	s.scheduleHide()
}

// Build implements widgets.State.
func (s *scrollableElementState) Build(bc widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	colors := ThemeOf(bc)
	m := w.model.Metrics()
	vState, hState := s.states(m)
	children := []widgets.Widget{w.child}
	if w.opts.UseShadows {
		children = append(children, s.shadows(colors, m)...)
	}
	always := func(v ScrollbarVisibility) bool { return v == ScrollbarVisible }
	if s.scrollbarVisible(w.opts.Vertical, vState) {
		children = append(children, s.scrollbar(axisVertical, vState, colors, m, always(w.opts.Vertical)))
	}
	if s.scrollbarVisible(w.opts.Horizontal, hState) {
		children = append(children, s.scrollbar(axisHorizontal, hState, colors, m, always(w.opts.Horizontal)))
	}
	return widgets.NewMouseRegion(
		widgets.NewStack(children, widgets.StackOpts{Fit: rendering.StackFitExpand}),
		widgets.MouseRegionOpts{
			Opaque: foundation.Some(false),
			OnEnter: func(*gestures.PointerEnterEvent) {
				s.mouseOver = true
				s.cancelHide()
				s.reveal()
			},
			OnExit: func(*gestures.PointerExitEvent) {
				s.mouseOver = false
				s.scheduleHide()
			},
		})
}

// scrollbar is the track and slider of one axis, faded with the others.
func (s *scrollableElementState) scrollbar(a axis, st *ScrollbarState, colors Colors, m ScrollMetrics, always bool) widgets.Widget {
	o := s.Widget().opts
	thickness := st.RectangleSmallSize()
	sliderThickness := o.VerticalSliderSize.OrElse(thickness)
	if a == axisHorizontal {
		sliderThickness = o.HorizontalSliderSize.OrElse(thickness)
	}
	sliderOffset := math.Floor((thickness - sliderThickness) / 2)

	color := colors.ScrollbarSliderBackground
	switch {
	case s.drag != nil && s.drag.axis == a:
		color = colors.ScrollbarSliderActiveBackground
	case s.hovering && s.hover == a:
		color = colors.ScrollbarSliderHoverBackground
	}
	slider := widgets.NewMouseRegion(widgets.NewColoredBox(color, widgets.NewSizedBox(nil)), widgets.MouseRegionOpts{
		OnEnter: func(*gestures.PointerEnterEvent) { s.SetState(func() { s.hover, s.hovering = a, true }) },
		OnExit:  func(*gestures.PointerExitEvent) { s.SetState(func() { s.hovering = false }) },
	})
	pos := st.ArrowSize() + st.SliderPosition()
	var sliderBox *widgets.Positioned
	var trackWidth, trackHeight float64
	if a == axisVertical {
		sliderBox = widgets.NewPositioned(slider, widgets.PositionedOpts{
			Left: foundation.Some(sliderOffset), Top: foundation.Some(pos),
			Width: foundation.Some(sliderThickness), Height: foundation.Some(st.SliderSize()),
		})
		trackWidth, trackHeight = thickness, st.RectangleLargeSize()
	} else {
		sliderBox = widgets.NewPositioned(slider, widgets.PositionedOpts{
			Top: foundation.Some(sliderOffset), Left: foundation.Some(pos),
			Height: foundation.Some(sliderThickness), Width: foundation.Some(st.SliderSize()),
		})
		trackWidth, trackHeight = st.RectangleLargeSize(), thickness
	}
	body := widgets.NewListener(
		widgets.NewSizedBox(widgets.NewStack([]widgets.Widget{sliderBox}), widgets.SizedBoxOpts{
			Width: foundation.Some(trackWidth), Height: foundation.Some(trackHeight),
		}),
		widgets.ListenerOpts{
			// The whole track takes the pointer, not only the slider.
			Behavior:        rendering.HitTestBehaviorOpaque,
			OnPointerDown:   func(e *gestures.PointerDownEvent) { s.trackDown(a, e) },
			OnPointerMove:   s.trackMove,
			OnPointerUp:     func(*gestures.PointerUpEvent) { s.trackUp() },
			OnPointerCancel: func(*gestures.PointerCancelEvent) { s.trackUp() },
		})
	var faded widgets.Widget = widgets.NewFadeTransition(s.fade, body)
	revealed := always || s.shown || s.drag != nil
	if always {
		faded = body
	}
	faded = widgets.NewIgnorePointer(faded, widgets.IgnorePointerOpts{Ignoring: foundation.Some(!revealed)})
	if a == axisVertical {
		return widgets.NewPositioned(faded, widgets.PositionedOpts{Top: foundation.Some(0.0), Right: foundation.Some(0.0)})
	}
	return widgets.NewPositioned(faded, widgets.PositionedOpts{Bottom: foundation.Some(0.0), Left: foundation.Some(s.Widget().opts.InsetLeft)})
}

// shadows draws the shadow below the top edge and beside the left edge of content that has
// been scrolled away from them (an approximation of VS Code's inset box shadow).
func (s *scrollableElementState) shadows(colors Colors, m ScrollMetrics) []widgets.Widget {
	shadow := colors.ScrollbarShadow.WithAlpha(0x66)
	clear := colors.ScrollbarShadow.WithAlpha(0)
	var out []widgets.Widget
	if m.Top > 0 {
		out = append(out, widgets.NewPositioned(
			widgets.NewIgnorePointer(widgets.NewDecoratedBox(painting.NewBoxDecoration(painting.BoxDecorationOpts{
				Gradient: painting.NewLinearGradient([]ui.Color{shadow, clear}, painting.LinearGradientOpts{Begin: painting.AlignmentTopCenter, End: painting.AlignmentBottomCenter}),
			}), nil)),
			widgets.PositionedOpts{Top: foundation.Some(0.0), Left: foundation.Some(s.Widget().opts.InsetLeft), Right: foundation.Some(0.0), Height: foundation.Some(scrollShadowSize)}))
	}
	if m.Left > 0 {
		out = append(out, widgets.NewPositioned(
			widgets.NewIgnorePointer(widgets.NewDecoratedBox(painting.NewBoxDecoration(painting.BoxDecorationOpts{
				Gradient: painting.NewLinearGradient([]ui.Color{shadow, clear}, painting.LinearGradientOpts{Begin: painting.AlignmentCenterLeft, End: painting.AlignmentCenterRight}),
			}), nil)),
			widgets.PositionedOpts{Top: foundation.Some(0.0), Bottom: foundation.Some(0.0), Left: foundation.Some(s.Widget().opts.InsetLeft), Width: foundation.Some(scrollShadowSize)}))
	}
	return out
}
