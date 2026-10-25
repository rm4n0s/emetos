package kit

import (
	"math"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"
)

// Metrics and times of VS Code's sash.
//
// Ported from VS Code: src/vs/base/browser/ui/sash/sash.css and sash.ts
const (
	// DefaultSashSize is the width of a vertical sash and the height of a horizontal one
	// (`--vscode-sash-size`); it is also the size of the line that shows under the pointer
	// (`--vscode-sash-hover-size`).
	DefaultSashSize = 4.0
	// DefaultSashHoverDelay is how long the pointer rests on a sash before the sash shows its
	// hover color.
	DefaultSashHoverDelay = 300 * time.Millisecond
	// sashDoubleClickInterval is the time within which a second press on a sash is a double
	// click.
	sashDoubleClickInterval = 400 * time.Millisecond
	// sashDoubleClickSlop is how far, in pixels, the second press can be from the first.
	sashDoubleClickSlop = 5.0
)

// SashOrientation says which way a sash runs.
type SashOrientation int

const (
	// SashVertical is a vertical line, dragged to the left and to the right: the sash between
	// views side by side.
	SashVertical SashOrientation = iota
	// SashHorizontal is a horizontal line, dragged up and down: the sash between stacked views.
	SashHorizontal
)

// SashEvent tells where the pointer is during the drag of a sash, in window coordinates.
type SashEvent struct {
	// StartX and StartY are where the pointer was pressed, CurrentX and CurrentY where it is.
	StartX, StartY, CurrentX, CurrentY float64
	// Alt says whether Alt is held now.
	Alt bool
}

// SashOpts configure a Sash.
type SashOpts struct {
	// Orientation says which way the sash runs; the default is SashVertical.
	Orientation SashOrientation
	// Size is the width of a vertical sash, or the height of a horizontal one, which is the part
	// that takes the pointer; the default is DefaultSashSize. A parent that positions the sash
	// gives it that size.
	Size foundation.Opt[float64]
	// State says what the sash can do; the default is SashEnabled.
	State SashState
	// HoverDelay is how long the pointer rests on the sash before it shows its hover color; the
	// default is DefaultSashHoverDelay. A sash that is dragged shows it at once.
	HoverDelay foundation.Opt[time.Duration]
	// OnDidStart is called when the primary button is pressed on the sash.
	OnDidStart func(SashEvent)
	// OnDidChange is called when the pointer moves while the sash is dragged.
	OnDidChange func(SashEvent)
	// OnDidEnd is called when the button is released, or the drag is cancelled.
	OnDidEnd func()
	// OnDidReset is called after a double click: the second press and its release have happened
	// (so OnDidStart and OnDidEnd were called for it) and the pointer stayed near the first.
	OnDidReset func()
}

// Sash is the thin line between two views that the user drags to resize them: it fills the space
// its parent gives it (4 px across by default), shows `sash.hoverBorder` once the pointer has
// rested on it (or at once while it is dragged), shows a resize cursor, and reports the drag and
// a double click. It does not move anything itself; a SplitView does.
//
// Ported from VS Code: src/vs/base/browser/ui/sash/sash.ts (Sash), without corner sashes,
// linked sashes and touch gestures.
type Sash struct {
	widgets.StatefulWidget
	opts SashOpts
}

// NewSash makes a sash.
func NewSash(opts ...SashOpts) *Sash {
	s := &Sash{StatefulWidget: widgets.NewStatefulWidgetBase()}
	if len(opts) > 0 {
		s.opts = opts[0]
	}
	return s
}

// CreateState implements widgets.StatefulWidget.
func (w *Sash) CreateState() widgets.State { return &sashState{} }

// SashCursor is the cursor a sash shows in a state: a double arrow when it can be dragged both
// ways, and an arrow when it can only be dragged one way.
//
// Ported from VS Code: src/vs/base/browser/ui/sash/sash.css and sash.ts (updateStyle)
func SashCursor(o SashOrientation, state SashState) services.MouseCursor {
	c := services.SystemMouseCursors
	switch {
	case state == SashDisabled:
		return services.MouseCursorDefer
	case o == SashHorizontal && state == SashAtMinimum:
		return c.ResizeDown
	case o == SashHorizontal && state == SashAtMaximum:
		return c.ResizeUp
	case o == SashHorizontal:
		return c.ResizeUpDown
	case state == SashAtMinimum:
		return c.ResizeRight
	case state == SashAtMaximum:
		return c.ResizeLeft
	}
	return c.ResizeLeftRight
}

type sashState struct {
	widgets.StateOf[*Sash]
	binding *widgets.WidgetsBinding

	// inside says the pointer is over the sash; hover that the hover color shows; active that
	// the sash is dragged.
	inside, hover, active bool
	hoverTimer            *gestures.Timer
	// start is where the drag began and pointer the pointer of the drag.
	start   ui.Offset
	pointer int
	// lastDown and lastAt are the time and the place of the last press; second says the press
	// being held is the second of a double click.
	lastDown time.Duration
	lastAt   ui.Offset
	hasLast  bool
	second   bool
}

// InitState reads the binding.
func (s *sashState) InitState() { s.binding = widgets.WidgetsBindingOf(s.Context()) }

// Dispose stops the timer.
func (s *sashState) Dispose() { s.cancelHover() }

func (s *sashState) cancelHover() {
	if s.hoverTimer != nil {
		s.hoverTimer.Cancel()
		s.hoverTimer = nil
	}
}

func (s *sashState) disabled() bool { return s.Widget().opts.State == SashDisabled }

// enter starts the hover delay, or shows the hover color at once when the sash is dragged.
//
// Ported from VS Code: src/vs/base/browser/ui/sash/sash.ts (onMouseEnter)
func (s *sashState) enter() {
	s.inside = true
	if s.active {
		s.cancelHover()
		s.SetState(func() { s.hover = true })
		return
	}
	s.cancelHover()
	delay := s.Widget().opts.HoverDelay.OrElse(DefaultSashHoverDelay)
	s.hoverTimer = s.binding.Gestures().NewTimer(delay, func() {
		s.hoverTimer = nil
		if s.Mounted() && s.inside {
			s.SetState(func() { s.hover = true })
		}
	})
}

// leave takes the hover color away.
func (s *sashState) leave() {
	s.inside = false
	s.cancelHover()
	if s.hover {
		s.SetState(func() { s.hover = false })
	}
}

// down starts a drag with the primary button, and counts a double click.
func (s *sashState) down(e *gestures.PointerDownEvent) {
	if e.Buttons()&gestures.PrimaryButton == 0 || s.disabled() || s.active {
		return
	}
	at, stamp := e.Position(), e.TimeStamp()
	s.second = s.hasLast && stamp-s.lastDown <= sashDoubleClickInterval &&
		abs64(at.Dx-s.lastAt.Dx) <= sashDoubleClickSlop && abs64(at.Dy-s.lastAt.Dy) <= sashDoubleClickSlop
	s.lastDown, s.lastAt, s.hasLast = stamp, at, true
	if s.second {
		s.hasLast = false // a third press is the first of the next
	}
	s.start, s.pointer = at, e.Pointer()
	s.cancelHover()
	s.SetState(func() { s.active = true })
	if f := s.Widget().opts.OnDidStart; f != nil {
		f(s.event(at))
	}
}

func (s *sashState) event(at ui.Offset) SashEvent {
	return SashEvent{StartX: s.start.Dx, StartY: s.start.Dy, CurrentX: at.Dx, CurrentY: at.Dy, Alt: s.binding.Keyboard().IsAltPressed()}
}

func (s *sashState) move(e *gestures.PointerMoveEvent) {
	if !s.active || e.Pointer() != s.pointer {
		return
	}
	if f := s.Widget().opts.OnDidChange; f != nil {
		f(s.event(e.Position()))
	}
}

// up ends the drag; after the second press of a double click it also resets.
func (s *sashState) up(cancelled bool) {
	if !s.active {
		return
	}
	reset := s.second && !cancelled
	s.second = false
	s.SetState(func() {
		s.active = false
		s.hover = s.hover && s.inside
	})
	opts := s.Widget().opts
	if opts.OnDidEnd != nil {
		opts.OnDidEnd()
	}
	if reset && opts.OnDidReset != nil {
		opts.OnDidReset()
	}
}

func abs64(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// Build implements widgets.State.
func (s *sashState) Build(bc widgets.BuildContext) widgets.Widget {
	opts := s.Widget().opts
	colors := ThemeOf(bc)
	size := opts.Size.OrElse(DefaultSashSize)
	line := ui.NewColor(0)
	if s.hover || s.active {
		line = colors.SashHoverBorder
	}
	// The line is as long as the sash (an infinite size is cut down to the space there is) and
	// `size` thick, in the middle.
	var bar widgets.Widget = widgets.NewColoredBox(line, widgets.NewSizedBox(nil))
	if opts.Orientation == SashHorizontal {
		bar = widgets.NewCenter(widgets.NewSizedBox(bar, widgets.SizedBoxOpts{Width: foundation.Some(math.Inf(1)), Height: foundation.Some(size)}))
	} else {
		bar = widgets.NewCenter(widgets.NewSizedBox(bar, widgets.SizedBoxOpts{Width: foundation.Some(size), Height: foundation.Some(math.Inf(1))}))
	}
	body := widgets.NewListener(bar, widgets.ListenerOpts{
		Behavior:        rendering.HitTestBehaviorOpaque,
		OnPointerDown:   s.down,
		OnPointerMove:   s.move,
		OnPointerUp:     func(*gestures.PointerUpEvent) { s.up(false) },
		OnPointerCancel: func(*gestures.PointerCancelEvent) { s.up(true) },
	})
	region := widgets.NewMouseRegion(body, widgets.MouseRegionOpts{
		Cursor: SashCursor(opts.Orientation, opts.State),
		// the resize cursor stays for the whole drag, also when the pointer outruns the sash
		// (VS Code: `* { cursor: ... !important }` while a sash is dragged)
		HoldCursorWhilePressed: true,
		OnEnter:                func(*gestures.PointerEnterEvent) { s.enter() },
		OnExit:                 func(*gestures.PointerExitEvent) { s.leave() },
	})
	return widgets.NewIgnorePointer(region, widgets.IgnorePointerOpts{Ignoring: foundation.Some(s.disabled())})
}
