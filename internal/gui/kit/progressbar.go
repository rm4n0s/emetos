package kit

import (
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/rendering"
	"github.com/rm4n0s/glutter/widgets"
)

// Metrics and times of VS Code's progress bar (`.monaco-progress-container`).
const (
	progressBarHeight = 2.0
	// progressBitFraction is the width of the bit as a fraction of the bar: 2 %.
	progressBitFraction = 0.02
	// ProgressInfiniteDuration is the time one trip of the bit of an infinite bar takes.
	ProgressInfiniteDuration = 4 * time.Second
	// ProgressLongRunningThreshold is how long an infinite bar runs before it moves in steps of 1 % of its
	// trip, to ask less of the GPU (VS Code's `infinite-long-running`).
	ProgressLongRunningThreshold = 10 * time.Second
	// ProgressDiscreteTransition is the time the bar of a discrete progress takes to grow to a new value.
	ProgressDiscreteTransition = 100 * time.Millisecond
)

// InfiniteProgressBit gives the place of the bit of an infinite progress bar at a point of its trip
// (phase from 0 to 1, over ProgressInfiniteDuration), as the start and the width in fractions of the
// bar. The bit is 2 % wide at the start, grows to three times that at the middle, where its center is at
// 51 %, and is 2 % wide again as it reaches the end: VS Code's `@keyframes progress`, a `translateX` and
// `scaleX` that move linearly between 0 %, 50 % and 100 %.
func InfiniteProgressBit(phase float64) (start, width float64) {
	phase = min(max(phase, 0), 1)
	var shift, scale float64 // shift in widths of the bit
	if phase < 0.5 {
		s := phase / 0.5
		shift, scale = 25*s, 1+2*s
	} else {
		s := (phase - 0.5) / 0.5
		shift, scale = 25+24*s, 3-2*s
	}
	width = scale * progressBitFraction
	center := shift*progressBitFraction + progressBitFraction/2
	return center - width/2, width
}

// ProgressBarOpts are the options of NewProgressBar.
type ProgressBarOpts struct {
	// Key is the key of the widget.
	Key foundation.Key
	// Infinite shows progress with no total: a bit that runs along the bar again and again. It takes
	// the place of Total and Worked.
	Infinite bool
	// Total is the amount of work of a discrete progress (VS Code's total(); 100 where it is not set)
	// and Worked the amount that is done: the bar fills to Worked / Total, growing to a new value in
	// 100 ms. Nothing shows while Worked is not above 0.
	Total, Worked float64
}

// ProgressBar is VS Code's progress bar: a line 2 px high across the width it is given, in
// progressBar.background. Infinite, a bit runs along it (4 s a trip, linear, in steps of 1 % after
// 10 s); discrete, it fills from the left to Worked of Total.
//
// Ported from VS Code: src/vs/base/browser/ui/progressbar/progressbar.ts and progressbar.css. Not
// ported: the fade out when done (the caller stops showing the bar) and the screen reader signal.
type ProgressBar struct {
	widgets.StatefulWidget
	opts ProgressBarOpts
}

// NewProgressBar makes a progress bar.
func NewProgressBar(opts ...ProgressBarOpts) *ProgressBar {
	p := &ProgressBar{}
	if len(opts) > 0 {
		p.opts = opts[0]
	}
	p.StatefulWidget = widgets.NewStatefulWidgetBase(widgets.WidgetOpts{Key: p.opts.Key})
	return p
}

// CreateState implements widgets.StatefulWidget.
func (p *ProgressBar) CreateState() widgets.State { return &progressBarState{} }

type progressBarState struct {
	widgets.StateOf[*ProgressBar]
	widgets.SingleTickerProvider
	trip *animation.AnimationController
	// long is true once the infinite bar has run for ProgressLongRunningThreshold.
	long  bool
	timer *gestures.Timer
}

// InitState makes the controller of the trip and starts it for an infinite bar.
func (s *progressBarState) InitState() {
	s.trip = animation.NewAnimationController(&s.SingleTickerProvider, animation.AnimationControllerOpts{
		Duration: foundation.Some(ProgressInfiniteDuration),
	})
	s.sync()
}

// DidUpdateWidget starts or stops the trip.
func (s *progressBarState) DidUpdateWidget(old *ProgressBar) {
	if old.opts.Infinite != s.Widget().opts.Infinite {
		s.sync()
	}
}

// sync runs the trip while the bar is infinite, and times the move to the long running mode.
func (s *progressBarState) sync() {
	s.cancelTimer()
	s.long = false
	if !s.Widget().opts.Infinite {
		s.trip.Stop()
		return
	}
	s.trip.Repeat()
	s.timer = widgets.WidgetsBindingOf(s.Context()).Gestures().NewTimer(ProgressLongRunningThreshold, func() {
		s.timer = nil
		if s.Mounted() {
			s.SetState(func() { s.long = true })
		}
	})
}

func (s *progressBarState) cancelTimer() {
	if s.timer != nil {
		s.timer.Cancel()
		s.timer = nil
	}
}

// Dispose stops the trip.
func (s *progressBarState) Dispose() {
	s.cancelTimer()
	s.trip.Dispose()
}

// Build implements widgets.State.
func (s *progressBarState) Build(bc widgets.BuildContext) widgets.Widget {
	o := s.Widget().opts
	color := ThemeOf(bc).ProgressBarBackground
	bit := func(left, width float64) widgets.Widget {
		return widgets.NewPositioned(
			widgets.NewDecoratedBox(painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(color)}), nil),
			widgets.PositionedOpts{Left: foundation.Some(left), Top: foundation.Some(0.0), Width: foundation.Some(width), Height: foundation.Some(progressBarHeight)})
	}
	var content widgets.Widget
	switch {
	case o.Infinite:
		content = widgets.NewLayoutBuilder(func(_ widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
			return widgets.NewAnimatedBuilder(s.trip, func(widgets.BuildContext, widgets.Widget) widgets.Widget {
				phase := s.trip.Value()
				if s.long {
					phase = float64(int(phase*100)) / 100
				}
				start, width := InfiniteProgressBit(phase)
				return widgets.NewStack([]widgets.Widget{bit(start*c.MaxWidth, width*c.MaxWidth)}, widgets.StackOpts{Fit: rendering.StackFitExpand})
			}, nil)
		})
	default:
		content = widgets.NewLayoutBuilder(func(_ widgets.BuildContext, c rendering.BoxConstraints) widgets.Widget {
			width := DiscreteProgress(o.Total, o.Worked) * c.MaxWidth
			return widgets.NewStack([]widgets.Widget{
				widgets.NewAnimatedPositioned(ProgressDiscreteTransition,
					widgets.NewDecoratedBox(painting.NewBoxDecoration(painting.BoxDecorationOpts{Color: foundation.Some(color)}), nil),
					widgets.AnimatedPositionedOpts{Left: foundation.Some(0.0), Top: foundation.Some(0.0), Width: foundation.Some(width), Height: foundation.Some(progressBarHeight)}),
			}, widgets.StackOpts{Fit: rendering.StackFitExpand})
		})
	}
	return widgets.NewSizedBox(widgets.NewClipRect(content, widgets.ClipRectOpts{}), widgets.SizedBoxOpts{Height: foundation.Some(progressBarHeight)})
}

// DiscreteProgress is the fraction of the bar a discrete progress fills: worked of total (of 100 where
// total is not above 0), at most 1, and 0 where nothing is worked.
//
// Ported from VS Code: src/vs/base/browser/ui/progressbar/progressbar.ts (doSetWorked)
func DiscreteProgress(total, worked float64) float64 {
	if total <= 0 {
		total = 100
	}
	if worked <= 0 {
		return 0
	}
	return min(worked, total) / total
}
