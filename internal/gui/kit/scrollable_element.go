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

// Sizes of scrollbars.
const (
	// DefaultScrollbarSize is the thickness of a scrollbar that says nothing about it, as VS Code's
	// lists, trees and panels have (`DEFAULT_SCROLLBAR_SIZE`).
	DefaultScrollbarSize = 10.0
	// DefaultVerticalScrollbarSize is the width of the vertical scrollbar of an editor, from
	// VS Code (`editor.scrollbar.verticalScrollbarSize`).
	DefaultVerticalScrollbarSize = 14.0
	// DefaultHorizontalScrollbarSize is the height of the horizontal scrollbar of an editor, from
	// VS Code (`editor.scrollbar.horizontalScrollbarSize`).
	DefaultHorizontalScrollbarSize = 12.0
)

// scrollShadowSize is the size of the shadow at the start of content that is scrolled.
const scrollShadowSize = 4.0

// Wheel scrolling, from VS Code.
const (
	// wheelNotch is how many logical pixels Glutter reports for one notch of a mouse wheel.
	wheelNotch = 53.0
	// wheelStep is how far one notch scrolls at a sensitivity of 1 (`SCROLL_WHEEL_SENSITIVITY`).
	wheelStep = 50.0
	// scrollSmoothDuration is how long the scroll of a mouse wheel takes when it is smooth.
	scrollSmoothDuration = 125 * time.Millisecond
	// DefaultFastScrollSensitivity is the factor of the wheel while Alt is held.
	DefaultFastScrollSensitivity = 5.0
)

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

// ScrollAxes are the directions in which a ScrollableElement scrolls a child of its own.
type ScrollAxes int

const (
	// ScrollAxisVertical scrolls up and down; the child is as wide as the element and as tall as
	// it wants to be.
	ScrollAxisVertical ScrollAxes = iota
	// ScrollAxisHorizontal scrolls left and right; the child is as tall as the element and as
	// wide as it wants to be.
	ScrollAxisHorizontal
	// ScrollAxisBoth scrolls both ways; the child is as large as it wants to be.
	ScrollAxisBoth
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

// ScrollableElementOpts configure a ScrollableElement. The element scrolls its child in one of
// three ways: with a Model it draws scrollbars for a model and leaves the scrolling to it; with
// a Controller (and HorizontalController) it does so for the Glutter scroll views of the child
// (a list, a tree) that use the controllers; with neither it scrolls the child itself, which can
// be any widget, in the directions of Axes.
type ScrollableElementOpts struct {
	// Model is what the scrollbars follow and move.
	Model ScrollModel
	// Controller and HorizontalController are the controllers of the vertical and of the
	// horizontal scroll view in the child (see ScrollControllerModel).
	Controller, HorizontalController widgets.ScrollController
	// Axes are the directions in which the element scrolls a child of its own (when it has
	// neither Model nor a controller).
	Axes ScrollAxes

	// Vertical and Horizontal say when each scrollbar is shown; the default is ScrollbarAuto.
	Vertical, Horizontal ScrollbarVisibility
	// VerticalSize is the width of the vertical scrollbar; the default is DefaultScrollbarSize.
	VerticalSize foundation.Opt[float64]
	// HorizontalSize is the height of the horizontal scrollbar; the default is
	// DefaultScrollbarSize.
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
	// UseShadows draws a shadow at the top and at the left of content that is scrolled (VS
	// Code's default is on; lists and trees turn it off, so does this).
	UseShadows bool

	// HandleMouseWheel makes the element scroll with the mouse wheel and the touchpad. The
	// default is on when the element scrolls a child of its own, and off with a Model or a
	// controller, where the child scrolls itself.
	HandleMouseWheel foundation.Opt[bool]
	// MouseWheelScrollSensitivity multiplies the scroll of the wheel; the default is 1
	// (`mouseWheelScrollSensitivity`).
	MouseWheelScrollSensitivity foundation.Opt[float64]
	// FastScrollSensitivity multiplies it again while Alt is held; the default is 5
	// (`fastScrollSensitivity`).
	FastScrollSensitivity foundation.Opt[float64]
	// NoScrollPredominantAxis lets a wheel that moves both ways scroll both ways; by default
	// only the axis with the larger movement scrolls (`scrollPredominantAxis`).
	NoScrollPredominantAxis bool
	// FlipAxes swaps the axes of the wheel (`flipAxes`).
	FlipAxes bool
	// ScrollYToX turns vertical wheel scrolling into horizontal (`scrollYToX`); Shift does that
	// too.
	ScrollYToX bool
	// AlwaysConsumeMouseWheel keeps a wheel over the element from scrolling what is below it even
	// when the element does not move (`alwaysConsumeMouseWheel`).
	AlwaysConsumeMouseWheel bool
	// ConsumeMouseWheelIfScrollbarIsNeeded does that only while a scrollbar is needed
	// (`consumeMouseWheelIfScrollbarIsNeeded`).
	ConsumeMouseWheelIfScrollbarIsNeeded bool
	// NoSmoothScroll scrolls at once, instead of over 125 ms, with a mouse wheel
	// (`mouseWheelSmoothScroll`).
	NoSmoothScroll bool
}

func (o ScrollableElementOpts) verticalSize() float64 {
	return max(o.VerticalSize.OrElse(DefaultScrollbarSize), 0)
}

func (o ScrollableElementOpts) horizontalSize() float64 {
	return max(o.HorizontalSize.OrElse(DefaultScrollbarSize), 0)
}

// plain tells whether the element scrolls a child of its own.
func (o ScrollableElementOpts) plain() bool {
	return o.Model == nil && o.Controller == nil && o.HorizontalController == nil
}

// ScrollableElement puts VS Code-style overlay scrollbars over a child: a vertical one at the
// right and a horizontal one at the bottom, in the colors of the theme. They appear when the
// pointer is over the element or the content scrolls, fade away half a second after, move the
// content when their slider is dragged and when the track is clicked, and never take room from
// the child. The element also scrolls with the mouse wheel (Shift for sideways, Alt for faster,
// smoothly) when it scrolls a child of its own; see ScrollableElementOpts for the other ways.
//
// Ported from VS Code: src/vs/base/browser/ui/scrollbar/ (scrollableElement.ts, abstractScrollbar.ts, verticalScrollbar.ts, horizontalScrollbar.ts, scrollbarVisibilityController.ts)
// Not ported: scrollbar arrows, inertial scrolling, the touch gestures, the corner between the
// scrollbars, and the reset of a drag that moves far from the scrollbar (Windows only).
type ScrollableElement struct {
	widgets.StatefulWidget
	child widgets.Widget
	opts  ScrollableElementOpts
}

// NewScrollableElement puts scrollbars over a child.
func NewScrollableElement(child widgets.Widget, opts ...ScrollableElementOpts) *ScrollableElement {
	var o ScrollableElementOpts
	if len(opts) > 0 {
		o = opts[0]
	}
	return &ScrollableElement{StatefulWidget: widgets.NewStatefulWidgetBase(), child: child, opts: o}
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

// smoothScroll is an animated scroll: from where it was when it began to a target, easing out.
//
// Ported from VS Code: src/vs/base/common/scrollable.ts (SmoothScrollingOperation, easeOutCubic)
type smoothScroll struct {
	fromLeft, fromTop, toLeft, toTop float64
	// start is the time of the first frame; started is false until it came.
	start   time.Duration
	started bool
	running bool
}

type scrollableElementState struct {
	widgets.StateOf[*ScrollableElement]
	widgets.SingleTickerProvider
	fade        *animation.AnimationController
	binding     *widgets.WidgetsBinding
	model       ScrollModel
	modelHandle foundation.ListenerHandle
	hideTimer   *gestures.Timer
	// ownModel is the model made of controllers; verticalCtl and horizontalCtl are the
	// controllers of the scroll views the element makes for a child of its own.
	ownModel                   *ScrollControllerModel
	verticalCtl, horizontalCtl *widgets.ScrollControllerBase

	mouseOver bool
	// shown is whether the scrollbars are revealed (or on their way to it).
	shown bool
	drag  *drag
	// hover is the axis whose slider the pointer is over, if any.
	hover       axis
	hovering    bool
	lastMetrics ScrollMetrics
	pending     bool
	smooth      smoothScroll
}

// InitState starts listening to the model.
func (s *scrollableElementState) InitState() {
	s.binding = widgets.WidgetsBindingOf(s.Context())
	s.fade = animation.NewAnimationController(&s.SingleTickerProvider, animation.AnimationControllerOpts{
		Duration:        foundation.Some(scrollbarRevealDuration),
		ReverseDuration: foundation.Some(scrollbarFadeDuration),
	})
	s.attach()
}

// attach finds the model the opts ask for and listens to it.
func (s *scrollableElementState) attach() {
	o := s.Widget().opts
	switch {
	case o.Model != nil:
		s.model = o.Model
	case o.plain():
		if s.verticalCtl == nil {
			s.verticalCtl, s.horizontalCtl = widgets.NewScrollController(), widgets.NewScrollController()
		}
		s.ownModel = NewScrollControllerModel(s.verticalCtl, s.horizontalCtl)
		s.model = s.ownModel
	default:
		s.ownModel = NewScrollControllerModel(o.Controller, o.HorizontalController)
		s.model = s.ownModel
	}
	s.lastMetrics = s.model.Metrics()
	s.modelHandle = s.model.AddListener(s.modelChanged)
}

// detach stops listening to the model.
func (s *scrollableElementState) detach() {
	s.model.RemoveListener(s.modelHandle)
	if s.ownModel != nil {
		s.ownModel.Dispose()
		s.ownModel = nil
	}
	s.smooth.running = false
}

// DidUpdateWidget follows a new model or new controllers.
func (s *scrollableElementState) DidUpdateWidget(old *ScrollableElement) {
	o, n := old.opts, s.Widget().opts
	if o.Model != n.Model || o.Controller != n.Controller || o.HorizontalController != n.HorizontalController {
		s.detach()
		s.attach()
	}
}

// Dispose stops the timer and the listener.
func (s *scrollableElementState) Dispose() {
	s.detach()
	if s.verticalCtl != nil {
		s.verticalCtl.Dispose()
		s.horizontalCtl.Dispose()
	}
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
		m := s.model.Metrics()
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
	s.smooth.running = false
	m := s.model.Metrics()
	if a == axisVertical {
		s.model.ScrollTo(m.Left, position)
	} else {
		s.model.ScrollTo(position, m.Top)
	}
	s.lastMetrics = s.model.Metrics()
	s.SetState(func() {})
}

// stateOf is the scrollbar state of an axis for the metrics now.
func (s *scrollableElementState) stateOf(a axis) *ScrollbarState {
	v, h := s.states(s.model.Metrics())
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

// handlesWheel tells whether the element scrolls with the wheel.
func (s *scrollableElementState) handlesWheel() bool {
	o := s.Widget().opts
	return o.HandleMouseWheel.OrElse(o.plain())
}

// WheelDelta turns the movement of a mouse wheel into how far to scroll, in pixels, positive
// towards the end of the content: the sensitivity multiplies it, only the axis that moves more
// scrolls unless predominantAxis is false, Shift (or yToX) scrolls sideways, and Alt scrolls
// faster. A step is rounded away from zero, as VS Code rounds it.
//
// Ported from VS Code: src/vs/base/browser/ui/scrollbar/scrollableElement.ts (_onMouseWheel)
func WheelDelta(delta ui.Offset, shift, alt bool, opts ScrollableElementOpts) (dx, dy float64) {
	sensitivity := opts.MouseWheelScrollSensitivity.OrElse(1)
	dx, dy = delta.Dx/wheelNotch*sensitivity, delta.Dy/wheelNotch*sensitivity
	if !opts.NoScrollPredominantAxis {
		switch {
		case opts.ScrollYToX && dx+dy == 0:
			// Both axes compete with opposite values: there is no way to choose.
			dx, dy = 0, 0
		case abs64(dy) >= abs64(dx):
			dx = 0
		default:
			dy = 0
		}
	}
	if opts.FlipAxes {
		dx, dy = dy, dx
	}
	if (opts.ScrollYToX || shift) && dx == 0 {
		dx, dy = dy, 0
	}
	if alt {
		fast := opts.FastScrollSensitivity.OrElse(DefaultFastScrollSensitivity)
		dx, dy = dx*fast, dy*fast
	}
	return roundAway(dx * wheelStep), roundAway(dy * wheelStep)
}

// roundAway rounds to a whole number away from zero, so that a slow wheel still scrolls.
func roundAway(v float64) float64 {
	if v < 0 {
		return math.Floor(v)
	}
	return math.Ceil(v)
}

// maxScroll is how far the content scrolls along each axis.
func maxScroll(m ScrollMetrics) (left, top float64) {
	return max(m.ContentWidth-m.ViewportWidth, 0), max(m.ContentHeight-m.ViewportHeight, 0)
}

// futurePosition is where the content is, or where the smooth scroll that runs takes it.
func (s *scrollableElementState) futurePosition(m ScrollMetrics) (left, top float64) {
	if s.smooth.running {
		return s.smooth.toLeft, s.smooth.toTop
	}
	return m.Left, m.Top
}

// pointerSignal scrolls with the wheel, and keeps what is below from scrolling too when it does
// (or always, with AlwaysConsumeMouseWheel).
//
// Ported from VS Code: src/vs/base/browser/ui/scrollbar/scrollableElement.ts (_onMouseWheel)
func (s *scrollableElementState) pointerSignal(e gestures.PointerSignalEvent) {
	scroll, ok := e.(*gestures.PointerScrollEvent)
	if !ok || !s.handlesWheel() {
		return
	}
	o := s.Widget().opts
	kb := s.binding.Keyboard()
	dx, dy := WheelDelta(scroll.ScrollDelta(), kb.IsShiftPressed(), kb.IsAltPressed(), o)
	m := s.model.Metrics()
	maxLeft, maxTop := maxScroll(m)
	left, top := s.futurePosition(m)
	toLeft, toTop := min(max(left+dx, 0), maxLeft), min(max(top+dy, 0), maxTop)
	moves := (dx != 0 || dy != 0) && (toLeft != left || toTop != top)
	consume := moves || o.AlwaysConsumeMouseWheel
	if !consume && o.ConsumeMouseWheelIfScrollbarIsNeeded {
		v, h := s.states(m)
		consume = v.IsNeeded() || h.IsNeeded()
	}
	if !consume {
		return
	}
	smooth := !o.NoSmoothScroll && scroll.Kind() == ui.PointerDeviceKindMouse
	s.binding.Gestures().PointerSignalResolver.Register(e, func(gestures.PointerSignalEvent) {
		if moves {
			s.scrollToPosition(toLeft, toTop, smooth)
		}
		scroll.Respond(false)
	})
}

// panZoomUpdate scrolls with the fingers on a touchpad: the content follows them.
func (s *scrollableElementState) panZoomUpdate(e *gestures.PointerPanZoomUpdateEvent) {
	if !s.handlesWheel() {
		return
	}
	o := s.Widget().opts
	k := o.MouseWheelScrollSensitivity.OrElse(1)
	if s.binding.Keyboard().IsAltPressed() {
		k *= o.FastScrollSensitivity.OrElse(DefaultFastScrollSensitivity)
	}
	d := e.PanDelta()
	m := s.model.Metrics()
	maxLeft, maxTop := maxScroll(m)
	left, top := s.futurePosition(m)
	s.scrollToPosition(min(max(left-d.Dx*k, 0), maxLeft), min(max(top-d.Dy*k, 0), maxTop), false)
}

// scrollToPosition moves the content to a position at once, or over a short time.
func (s *scrollableElementState) scrollToPosition(left, top float64, smooth bool) {
	if !smooth {
		s.smooth.running = false
		s.model.ScrollTo(left, top)
		s.lastMetrics = s.model.Metrics()
		s.SetState(func() {})
		return
	}
	m := s.model.Metrics()
	a := &s.smooth
	a.fromLeft, a.fromTop, a.toLeft, a.toTop, a.started = m.Left, m.Top, left, top, false
	if a.running {
		return
	}
	a.running = true
	s.scheduleSmoothFrame()
}

func (s *scrollableElementState) scheduleSmoothFrame() {
	scheduler := s.binding.Scheduler()
	scheduler.ScheduleFrame()
	scheduler.AddPostFrameCallback(s.smoothFrame)
}

// smoothFrame moves the content along the animation.
func (s *scrollableElementState) smoothFrame(stamp time.Duration) {
	a := &s.smooth
	if !a.running || !s.Mounted() {
		a.running = false
		return
	}
	if !a.started {
		a.start, a.started = stamp, true
	}
	progress := min(max(float64(stamp-a.start)/float64(scrollSmoothDuration), 0), 1)
	left := 1 - progress
	eased := 1 - left*left*left
	s.model.ScrollTo(a.fromLeft+(a.toLeft-a.fromLeft)*eased, a.fromTop+(a.toTop-a.fromTop)*eased)
	if progress >= 1 {
		a.running = false
		return
	}
	s.scheduleSmoothFrame()
}

// Build implements widgets.State.
func (s *scrollableElementState) Build(bc widgets.BuildContext) widgets.Widget {
	w := s.Widget()
	colors := ThemeOf(bc)
	m := s.model.Metrics()
	vState, hState := s.states(m)
	children := []widgets.Widget{s.content(w.child)}
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
	var body widgets.Widget = widgets.NewMouseRegion(
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
	return widgets.NewListener(body, widgets.ListenerOpts{
		Behavior:               rendering.HitTestBehaviorTranslucent,
		OnPointerSignal:        s.pointerSignal,
		OnPointerPanZoomUpdate: s.panZoomUpdate,
	})
}

// scrollBehavior is the behavior of the scroll views below the element: Glutter's own scrollbars
// and overscroll glow are off (this element draws the scrollbars), and only a finger or a pen
// drags (the wheel and the touchpad are the element's, or the scroll view's own).
func scrollBehavior() widgets.ScrollBehavior {
	return widgets.NewScrollBehavior().CopyWith(widgets.ScrollBehaviorCopy{
		Scrollbars:  foundation.Some(false),
		Overscroll:  foundation.Some(false),
		DragDevices: []ui.PointerDeviceKind{ui.PointerDeviceKindTouch, ui.PointerDeviceKindStylus},
	})
}

// content is the child, scrolled by the element when it has no scroll view of its own.
func (s *scrollableElementState) content(child widgets.Widget) widgets.Widget {
	o := s.Widget().opts
	if !o.plain() {
		// The scroll views of the child tell when their size changes.
		return widgets.NewScrollConfiguration(scrollBehavior(),
			widgets.NewNotificationListener(func(*widgets.ScrollMetricsNotification) bool {
				s.modelChanged()
				return false
			}, child))
	}
	never := widgets.NewNeverScrollableScrollPhysics(nil)
	if o.Axes == ScrollAxisHorizontal || o.Axes == ScrollAxisBoth {
		child = widgets.NewSingleChildScrollView(child, widgets.SingleChildScrollViewOpts{
			ScrollDirection: painting.AxisHorizontal, Controller: s.horizontalCtl, Physics: never, Primary: foundation.Some(false),
		})
	}
	if o.Axes == ScrollAxisVertical || o.Axes == ScrollAxisBoth {
		child = widgets.NewSingleChildScrollView(child, widgets.SingleChildScrollViewOpts{
			ScrollDirection: painting.AxisVertical, Controller: s.verticalCtl, Physics: never, Primary: foundation.Some(false),
		})
	}
	return widgets.NewScrollConfiguration(scrollBehavior(),
		widgets.NewNotificationListener(func(*widgets.ScrollMetricsNotification) bool {
			s.modelChanged()
			return false
		}, child))
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
	var track widgets.Widget = widgets.NewStack([]widgets.Widget{sliderBox})
	if colors.ScrollbarBackground.Alpha() != 0 {
		track = widgets.NewColoredBox(colors.ScrollbarBackground, track)
	}
	body := widgets.NewListener(
		widgets.NewSizedBox(track, widgets.SizedBoxOpts{
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
