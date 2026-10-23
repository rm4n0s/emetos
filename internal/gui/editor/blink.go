package editor

import (
	"time"

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
)

// blinkStyleOf is the style of an editor: hidden when it has no focus, otherwise as the
// editor.cursorBlinking setting says. Only "solid" and "blink" are drawn: smooth, phase and
// expand blink like "blink".
func blinkStyleOf(focused bool, setting string) blinkStyle {
	switch {
	case !focused:
		return blinkHidden
	case setting == "solid":
		return blinkSolid
	default:
		return blinkFlat
	}
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
}

func newCursorBlink() *cursorBlink {
	return &cursorBlink{ChangeNotifier: *foundation.NewChangeNotifier(), style: blinkHidden}
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
