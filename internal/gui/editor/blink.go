package editor

import (
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
)

// BlinkInterval is how long the cursor stays shown or hidden while it blinks.
//
// Ported from VS Code: src/vs/editor/browser/viewParts/viewCursors/viewCursors.ts (BLINK_INTERVAL)
const BlinkInterval = 500 * time.Millisecond

// TimerFunc runs fn once on the UI thread after d and returns the function that cancels it.
// The editor widget gives the view model the timers of the window's binding (so that tests
// drive them with a fake clock); the view model does not blink without one.
type TimerFunc func(d time.Duration, fn func()) (cancel func())

// blinkStyle is what the cursors do now.
type blinkStyle int

const (
	// blinkHidden is the state of an editor that has no focus: no cursor, no timer.
	blinkHidden blinkStyle = iota
	// blinkSolid shows the cursor all the time.
	blinkSolid
	// blinkFlat shows and hides the cursor every BlinkInterval.
	blinkFlat
	// blinkSmooth fades the cursor out and in (editor.cursorBlinking "smooth").
	blinkSmooth
	// blinkPhase fades it more slowly, staying dark longer ("phase").
	blinkPhase
	// blinkExpand shrinks it to nothing and grows it back, vertically ("expand").
	blinkExpand
)

// animated tells whether a style is drawn by an animation that runs on every frame, not by a
// timer that shows and hides.
func (s blinkStyle) animated() bool { return s == blinkSmooth || s == blinkPhase || s == blinkExpand }

// blinkStyleOf is the style of an editor: hidden when it has no focus, otherwise as the
// editor.cursorBlinking setting says.
func blinkStyleOf(focused bool, setting string) blinkStyle {
	switch {
	case !focused:
		return blinkHidden
	case setting == "solid":
		return blinkSolid
	case setting == "smooth":
		return blinkSmooth
	case setting == "phase":
		return blinkPhase
	case setting == "expand":
		return blinkExpand
	default:
		return blinkFlat
	}
}

// The animated styles repeat a keyframe animation that VS Code writes in CSS: after
// BlinkInterval of showing the cursor, 20 iterations of BlinkInterval each, every other one
// played backwards, ending with the cursor shown again.
const (
	animationIterations = 20
	// The keyframes: the cursor is whole up to the first fraction of an iteration, goes away
	// by the second (ease-in-out in between), and stays away until the iteration ends.
	animationHold   = 0.2
	smoothGone      = 0.6
	phaseGone       = 0.9
	expandGone      = 0.8
	animationLength = BlinkInterval * animationIterations
)

// cursorFrame is how the cursors look at a moment of an animation: how opaque they are and how
// much of their height is left.
type cursorFrame struct {
	Opacity, ScaleY float64
}

// animationFrame is the look of an animated style a time after the cursors were shown, and
// whether the animation is over (the cursors are whole again from then on).
//
// Ported from VS Code: src/vs/editor/browser/viewParts/viewCursors/viewCursors.css (monaco-cursor-smooth,
// monaco-cursor-phase, monaco-cursor-expand)
func animationFrame(style blinkStyle, elapsed time.Duration) (cursorFrame, bool) {
	whole := cursorFrame{Opacity: 1, ScaleY: 1}
	elapsed -= BlinkInterval
	if elapsed < 0 {
		return whole, false
	}
	if elapsed >= animationLength {
		return whole, true
	}
	iteration := int(elapsed / BlinkInterval)
	p := float64(elapsed%BlinkInterval) / float64(BlinkInterval)
	if iteration%2 == 1 { // alternate: the odd iterations run backwards
		p = 1 - p
	}
	gone := smoothGone
	switch style {
	case blinkPhase:
		gone = phaseGone
	case blinkExpand:
		gone = expandGone
	}
	v := 1.0 // how much of the cursor is left
	switch {
	case p <= animationHold:
	case p >= gone:
		v = 0
	default:
		v = 1 - animation.Curves.EaseInOut.Transform((p-animationHold)/(gone-animationHold))
	}
	if style == blinkExpand {
		return cursorFrame{Opacity: 1, ScaleY: v}, false
	}
	return cursorFrame{Opacity: v, ScaleY: 1}, false
}

// cursorBlink is the visibility of the cursors over time. Every restart shows the cursors and
// starts counting again, so a cursor that moves or types does not disappear in the middle of
// the action.
//
// Ported from VS Code: src/vs/editor/browser/viewParts/viewCursors/viewCursors.ts (_updateBlinking)
type cursorBlink struct {
	foundation.ChangeNotifier
	after   TimerFunc
	cancel  func()
	visible bool
	style   blinkStyle
	// frame is the look of an animated style now; restarts counts the restarts, for what
	// animates to know when to begin again.
	frame    cursorFrame
	restarts int
}

func newCursorBlink() *cursorBlink {
	return &cursorBlink{ChangeNotifier: *foundation.NewChangeNotifier(), style: blinkHidden, frame: cursorFrame{Opacity: 1, ScaleY: 1}}
}

// SetTimer changes the source of timers and restarts the blinking with it. A nil source
// stops the timer.
func (b *cursorBlink) SetTimer(after TimerFunc) {
	b.stop()
	b.after = after
	b.Restart(b.style)
}

// Visible tells whether the cursors are drawn now.
func (b *cursorBlink) Visible() bool { return b.visible }

// Restart applies a style: it shows the cursors (or hides them for blinkHidden), cancels the
// timer and, for blinkFlat, starts a new one.
func (b *cursorBlink) Restart(style blinkStyle) {
	b.stop()
	b.style = style
	b.restarts++
	b.setFrame(cursorFrame{Opacity: 1, ScaleY: 1})
	b.setVisible(style != blinkHidden)
	if style == blinkFlat && b.after != nil {
		b.arm()
	}
}

// Stop cancels the timer; the cursors keep the state they have.
func (b *cursorBlink) Stop() { b.stop() }

// Active tells whether a timer is waiting to toggle the cursors.
func (b *cursorBlink) Active() bool { return b.cancel != nil }

func (b *cursorBlink) stop() {
	if b.cancel != nil {
		b.cancel()
		b.cancel = nil
	}
}

func (b *cursorBlink) arm() {
	b.cancel = b.after(BlinkInterval, func() {
		b.cancel = nil
		if b.style != blinkFlat {
			return
		}
		b.setVisible(!b.visible)
		b.arm()
	})
}

func (b *cursorBlink) setVisible(v bool) {
	if v == b.visible {
		return
	}
	b.visible = v
	b.NotifyListeners()
}

// Frame is the look of the cursors in an animated style; whole in every other.
func (b *cursorBlink) Frame() cursorFrame { return b.frame }

// advance moves an animated style to the time since the last restart, and tells whether it has
// more to do.
func (b *cursorBlink) advance(elapsed time.Duration) bool {
	if !b.style.animated() {
		return false
	}
	frame, done := animationFrame(b.style, elapsed)
	b.setFrame(frame)
	return !done
}

func (b *cursorBlink) setFrame(f cursorFrame) {
	if f == b.frame {
		return
	}
	b.frame = f
	b.NotifyListeners()
}
