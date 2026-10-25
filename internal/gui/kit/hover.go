package kit

import (
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// Metrics and times of VS Code's hover.
//
// Ported from VS Code: src/vs/platform/hover/browser/hover.css and hoverWidget.ts
const (
	hoverFontSize        = 13.0
	hoverCompactFontSize = 12.0
	hoverLineHeight      = 19.0
	hoverMaxWidth        = 700.0
	hoverRadius          = 5.0
	hoverPadV            = 4.0
	hoverCompactPadV     = 2.0
	hoverPadH            = 8.0
	hoverEdgeMargin      = 2.0
	hoverShadowBlur      = 8.0

	// DefaultHoverDelay is the delay before a hover shows (`workbench.hover.delay` on Linux).
	DefaultHoverDelay = 500 * time.Millisecond
	// DefaultHoverInstantLimit is how long after a hover hid the next one shows without a delay
	// when the delegate has instant hovers on (VS Code's `WorkbenchHoverDelegate.timeLimit`).
	DefaultHoverInstantLimit = 200 * time.Millisecond
)

// HoverPosition says on which side of its target a hover shows.
type HoverPosition int

// The sides of the target where a hover can show. A hover that does not fit on its side flips to
// the other side (Below and Above), or to the opposite side or Below (Right and Left), as VS Code's
// does.
const (
	// HoverBelow puts the hover under the target, its left edge on the target's.
	HoverBelow HoverPosition = iota
	// HoverAbove puts the hover over the target, its left edge on the target's.
	HoverAbove
	// HoverRight puts the hover to the right of the target, its top edge on the target's.
	HoverRight
	// HoverLeft puts the hover to the left of the target, its top edge on the target's.
	HoverLeft
)

// PlaceHover gives the position of the top left corner of a hover of a size next to a target
// (both in the coordinates of a window of a size), and the side it ended up on. The hover keeps
// inside the window.
//
// Ported from VS Code: src/vs/platform/hover/browser/hoverWidget.ts (computeXCordinate,
// computeYCordinate, adjustHorizontalHoverPosition, adjustVerticalHoverPosition). VS Code puts
// the top of a hover below its target 2 px over the target's bottom edge; this puts it on the edge.
func PlaceHover(target ui.Rect, size, window ui.Size, pos HoverPosition) (ui.Offset, HoverPosition) {
	switch pos {
	case HoverRight:
		if window.Width-target.Right < size.Width {
			switch {
			case target.Left >= size.Width:
				pos = HoverLeft
			default:
				pos = HoverBelow
			}
		}
	case HoverLeft:
		if target.Left < size.Width {
			switch {
			case window.Width-target.Right >= size.Width:
				pos = HoverRight
			default:
				pos = HoverBelow
			}
		}
	case HoverAbove:
		if target.Top-size.Height < 0 {
			pos = HoverBelow
		}
	}
	if pos == HoverBelow && target.Bottom+size.Height > window.Height && target.Top-size.Height >= 0 {
		pos = HoverAbove
	}
	var x, y float64
	switch pos {
	case HoverAbove:
		x, y = target.Left, target.Top-size.Height
	case HoverRight:
		x, y = target.Right, target.Top
	case HoverLeft:
		x, y = target.Left-size.Width, target.Top
	default:
		x, y = target.Left, target.Bottom
	}
	// keep it inside the window
	if x+size.Width >= window.Width {
		x = window.Width - size.Width - hoverEdgeMargin
	}
	if x < 0 {
		x = 0
	}
	if y+size.Height > window.Height {
		y = window.Height - size.Height
	}
	if y < 0 {
		y = 0
	}
	return ui.Offset{Dx: x, Dy: y}, pos
}

// HoverDelegateOpts are the options of NewHoverDelegate.
type HoverDelegateOpts struct {
	// Delay is the time the pointer rests on a target before its hover shows; the default is
	// DefaultHoverDelay.
	Delay foundation.Opt[time.Duration]
	// Instant makes a hover show without a delay when another hover of the delegate hid less than
	// InstantLimit ago, so that moving along a row of buttons shows each one's hover at once
	// (VS Code's instant hover delegate, used by action bars).
	Instant bool
	// InstantLimit is that time; the default is DefaultHoverInstantLimit.
	InstantLimit foundation.Opt[time.Duration]
}

// HoverDelegate is what a group of hovers share (VS Code's `IHoverDelegate`): the delay, the rule
// for showing a hover at once after another, and the fact that only one hover of the group shows at a
// time. A delegate is used from the UI thread only; a window creates it and gives it to the
// widgets that make hovers (an ActionBar makes its own).
type HoverDelegate struct {
	delay   time.Duration
	instant bool
	limit   time.Duration
	// recent is true while the time limit after a hover hid has not passed.
	recent  bool
	timer   *gestures.Timer
	current *hoverState
}

// NewHoverDelegate makes a delegate.
func NewHoverDelegate(opts ...HoverDelegateOpts) *HoverDelegate {
	var o HoverDelegateOpts
	if len(opts) > 0 {
		o = opts[0]
	}
	return &HoverDelegate{delay: o.Delay.OrElse(DefaultHoverDelay), instant: o.Instant, limit: o.InstantLimit.OrElse(DefaultHoverInstantLimit)}
}

// Delay is the delay a hover that has no delay of its own waits for now: zero right after another
// hover of an instant delegate hid, else the delegate's delay.
func (d *HoverDelegate) Delay() time.Duration {
	if d.recent {
		return 0
	}
	return d.delay
}

// Visible says whether a hover of the delegate is showing.
func (d *HoverDelegate) Visible() bool { return d.current != nil }

// shown records the hover that shows now and hides the one that showed before.
func (d *HoverDelegate) shown(s *hoverState) {
	if prev := d.current; prev != nil && prev != s {
		prev.Hide()
	}
	d.current = s
}

// hidden records that a hover hid and, for an instant delegate, starts the time in which the next
// one shows at once.
func (d *HoverDelegate) hidden(s *hoverState, binding *gestures.GestureBinding) {
	if d.current == s {
		d.current = nil
	}
	if !d.instant {
		return
	}
	if d.timer != nil {
		d.timer.Cancel()
	}
	d.recent = true
	d.timer = binding.NewTimer(d.limit, func() { d.recent = false; d.timer = nil })
}

// HoverController shows and hides the hover of one Hover from outside (an action bar shows the
// hover of the item that has the focus when the keyboard moves it).
type HoverController struct {
	state *hoverState
}

// NewHoverController makes a controller; give it to the HoverOpts of one Hover.
func NewHoverController() *HoverController { return &HoverController{} }

// Show shows the hover now, without the delay. It does nothing when the Hover has no content or is
// not in the tree.
func (c *HoverController) Show() {
	if c.state != nil {
		c.state.Show()
	}
}

// Hide hides the hover and cancels one that is waiting to show.
func (c *HoverController) Hide() {
	if c.state != nil {
		c.state.Hide()
	}
}

// Visible says whether the hover shows.
func (c *HoverController) Visible() bool { return c.state != nil && c.state.visible }

// HoverOpts are the options of NewHover.
type HoverOpts struct {
	// Key is the key of the widget.
	Key foundation.Key
	// Text is the text of the hover. A hover with neither Text nor Content never shows.
	Text string
	// Content builds a widget to show in place of Text.
	Content func(bc widgets.BuildContext) widgets.Widget
	// Position is the side of the target the hover prefers; the default is HoverBelow.
	Position HoverPosition
	// Delay is the time the pointer rests on the target before the hover shows; the default is
	// the delay of the delegate.
	Delay foundation.Opt[time.Duration]
	// Compact is the look of a tooltip: 12 px text and 2 px of padding above and below, where a
	// plain hover has 13 px text and 4 px.
	Compact bool
	// Delegate is the group the hover belongs to; the default is a group of its own.
	Delegate *HoverDelegate
	// Controller, when set, can show and hide the hover.
	Controller *HoverController
}

func (o HoverOpts) hasContent() bool { return o.Text != "" || o.Content != nil }

// Hover wraps a widget (its target) and shows a popup next to it when the pointer rests on it for
// the delay: VS Code's hover. The popup hides when the pointer leaves the target or is pressed,
// and when a key is pressed (a hover that is still waiting for its delay is cancelled too) (VS Code's
// `hideOnKeyDown`; Escape among them, a modifier on its own not). It shows in the nearest Overlay,
// next to the target on the side that fits, and never leaves the window. The text of the popup
// cannot be selected and the pointer cannot go into it.
//
// It is built on Glutter's RawTooltip: the popup, the wait, the overlay and the dismissal by a
// press come from it. The hover adds the delay rule of the delegate (RawTooltipOpts.HoverDelayFunc),
// the group of hovers and its time limit (OnShown, OnHidden), the controller and PlaceHover for
// the place. The key handler is the hover's own, not RawTooltipOpts.DismissOnKey: VS Code does not
// hide a hover for a modifier pressed on its own, and a hover that still waits for its delay is
// cancelled by a key, neither of which DismissOnKey can do.
//
// Ported from VS Code: src/vs/base/browser/ui/hover/hover.ts, src/vs/platform/hover/browser/hover.ts
// (WorkbenchHoverDelegate) and src/vs/platform/hover/browser/hoverWidget.ts
type Hover struct {
	widgets.StatefulWidget
	child widgets.Widget
	opts  HoverOpts
}

// NewHover makes a hover for a widget.
func NewHover(child widgets.Widget, opts ...HoverOpts) *Hover {
	h := &Hover{child: child}
	if len(opts) > 0 {
		h.opts = opts[0]
	}
	h.StatefulWidget = widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: h.opts.Key})
	return h
}

// CreateState implements widgets.StatefulWidget.
func (h *Hover) CreateState() widgets.State {
	return &hoverState{tip: widgets.NewRawTooltipController()}
}

type hoverState struct {
	widgets.StateOf[*Hover]
	// tip shows and hides the RawTooltip the state builds.
	tip      *widgets.RawTooltipController
	delegate *HoverDelegate
	// own is true when the delegate is the one the state made.
	own     bool
	visible bool
	key     services.KeyHandlerHandle
	// watching is true while the key handler is added: from the start of the wait to the hide.
	watching bool
	// ctrl is the controller the state is attached to.
	ctrl *HoverController
	// bound is the binding of the window, kept for Dispose, which runs when the context is gone.
	bound    *widgets.WidgetsBinding
	disposed bool
}

// InitState attaches the controller and chooses the delegate.
func (s *hoverState) InitState() {
	s.bound = widgets.WidgetsBindingOf(s.Context())
	s.attach()
}

// DidUpdateWidget follows a new delegate or controller.
func (s *hoverState) DidUpdateWidget(old *Hover) {
	if old.opts.Controller != s.Widget().opts.Controller || old.opts.Delegate != s.Widget().opts.Delegate {
		s.Hide()
		s.attach()
	}
}

func (s *hoverState) attach() {
	o := s.Widget().opts
	if s.ctrl != nil && s.ctrl.state == s {
		s.ctrl.state = nil
	}
	s.ctrl = o.Controller
	if s.ctrl != nil {
		s.ctrl.state = s
	}
	s.delegate, s.own = o.Delegate, false
	if s.delegate == nil {
		s.delegate, s.own = NewHoverDelegate(), true
	}
}

// Dispose stops the key handler. The RawTooltip below reports the hide of a popup that was showing.
func (s *hoverState) Dispose() {
	s.disposed = true
	s.removeKeyHandler()
	if s.ctrl != nil && s.ctrl.state == s {
		s.ctrl.state = nil
	}
}

func (s *hoverState) binding() *widgets.WidgetsBinding { return s.bound }

// watchKeys starts to hide the hover on keys, if it is not doing so yet.
func (s *hoverState) watchKeys() {
	if !s.watching && !s.disposed {
		s.watching = true
		s.key = s.binding().Keyboard().AddHandler(s.handleKey)
	}
}

func (s *hoverState) removeKeyHandler() {
	if s.watching {
		s.watching = false
		s.binding().Keyboard().RemoveHandler(s.key)
		s.key = services.KeyHandlerHandle{}
	}
}

// Show shows the hover now.
func (s *hoverState) Show() {
	if s.visible || s.disposed || !s.Mounted() || !s.Widget().opts.hasContent() {
		return
	}
	s.tip.Show()
}

// Hide hides the hover and cancels the wait for it.
func (s *hoverState) Hide() {
	s.removeKeyHandler()
	s.tip.Hide()
}

// handleKey hides the hover on a key that is not a modifier, and lets the key go on.
func (s *hoverState) handleKey(e services.KeyEvent) bool {
	if _, ok := e.(*services.KeyDownEvent); !ok {
		return false
	}
	switch e.LogicalKey() {
	case services.LogicalKeyboardKeyShiftLeft, services.LogicalKeyboardKeyShiftRight,
		services.LogicalKeyboardKeyControlLeft, services.LogicalKeyboardKeyControlRight,
		services.LogicalKeyboardKeyAltLeft, services.LogicalKeyboardKeyAltRight,
		services.LogicalKeyboardKeyMetaLeft, services.LogicalKeyboardKeyMetaRight:
		return false
	}
	s.Hide()
	return false
}

// delay is the wait for a pointer that enters now: none right after another hover of an instant
// delegate hid, else the delay of the hover or of the delegate. RawTooltip asks for it when the
// pointer enters, which is also when the hover starts to watch the keys.
func (s *hoverState) delay() time.Duration {
	o := s.Widget().opts
	if !o.hasContent() {
		return 0
	}
	d := o.Delay.OrElse(s.delegate.delay)
	if s.delegate.recent {
		d = 0
	}
	if d > 0 {
		s.watchKeys()
	}
	return d
}

// shown records the popup in the delegate, which hides the one that showed before.
func (s *hoverState) shown() {
	s.visible = true
	if s.disposed {
		return
	}
	s.watchKeys()
	s.delegate.shown(s)
}

// hidden records the hide in the delegate, which starts the time of the instant rule.
func (s *hoverState) hidden() {
	s.visible = false
	if s.disposed {
		// the target left the tree: the next hover waits for its delay
		if s.delegate.current == s {
			s.delegate.current = nil
		}
		return
	}
	s.removeKeyHandler()
	s.delegate.hidden(s, s.binding().Gestures())
}

// Build implements widgets.State.
func (s *hoverState) Build(widgets.BuildContext) widgets.Widget {
	h := s.Widget()
	o := h.opts
	semantics := foundation.None[string]()
	switch {
	case !o.hasContent():
		// an empty tooltip builds nothing but the child
		semantics = foundation.Some("")
	case o.Content == nil:
		semantics = foundation.Some(o.Text)
	}
	return widgets.NewRawTooltip(semantics, s.buildPopup, h.child, widgets.RawTooltipOpts{
		TriggerMode:      widgets.TooltipTriggerModeManual,
		HoverDelayFunc:   s.delay,
		DismissDelay:     foundation.Some(time.Duration(0)),
		AnimationStyle:   animation.AnimationStyle{Duration: foundation.Some(time.Duration(0)), ReverseDuration: foundation.Some(time.Duration(0))},
		IgnorePointer:    true,
		OnShown:          s.shown,
		OnHidden:         s.hidden,
		Controller:       s.tip,
		PositionDelegate: s.place,
	})
}

// place gives the top left corner of the popup with PlaceHover.
func (s *hoverState) place(c widgets.TooltipPositionContext) ui.Offset {
	target := ui.NewRectFromLTWH(c.Target.Dx-c.TargetSize.Width/2, c.Target.Dy-c.TargetSize.Height/2, c.TargetSize.Width, c.TargetSize.Height)
	offset, _ := PlaceHover(target, c.TooltipSize, c.OverlaySize, s.Widget().opts.Position)
	return offset
}

func (s *hoverState) buildPopup(bc widgets.BuildContext, _ animation.Animation[float64]) widgets.Widget {
	return widgets.NewConstrainedBox(rendering.NewBoxConstraints(rendering.BoxConstraintsOpts{MaxWidth: foundation.Some(hoverMaxWidth)}), hoverBox(bc, s.Widget().opts))
}

// hoverBox is the popup: the text or the content in a box with the colors of the hover.
func hoverBox(bc widgets.BuildContext, o HoverOpts) widgets.Widget {
	colors := ThemeOf(bc)
	size, padV := hoverFontSize, hoverPadV
	if o.Compact {
		size, padV = hoverCompactFontSize, hoverCompactPadV
	}
	var content widgets.Widget
	if o.Content != nil {
		content = o.Content(bc)
	} else {
		style := painting.NewTextStyle(painting.TextStyleOpts{
			FontFamily:         UIFontOf(bc),
			FontFamilyFallback: []string{"sans-serif"},
			FontSize:           foundation.Some(size),
			Height:             foundation.Some(hoverLineHeight / size),
			Color:              foundation.Some(colors.EditorHoverWidgetForeground),
		})
		content = widgets.NewText(o.Text, widgets.TextOpts{Style: style})
	}
	return widgets.NewContainer(
		widgets.NewPadding(painting.NewEdgeInsetsSymmetric(padV, hoverPadH), content),
		widgets.ContainerOpts{Decoration: painting.NewBoxDecoration(painting.BoxDecorationOpts{
			Color:        foundation.Some(colors.EditorHoverWidgetBackground),
			Border:       painting.NewBorderAll(painting.BorderSideOpts{Color: foundation.Some(colors.EditorHoverWidgetBorder), Width: foundation.Some(1.0)}),
			BorderRadius: painting.NewBorderRadiusCircular(hoverRadius),
			BoxShadow:    []painting.BoxShadow{painting.NewBoxShadow(painting.BoxShadowOpts{Color: foundation.Some(colors.WidgetShadow), BlurRadius: hoverShadowBlur})},
		})})
}
