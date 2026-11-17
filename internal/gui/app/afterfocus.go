package app

import (
	"time"

	"github.com/rm4n0s/glutter/widgets"
)

// runWhenFocusReturns runs f once a menu or a picker that had the keyboard focus is gone and the focus is back in the widget
// that had it before: a command chosen in the menu runs on the editor, which binds its commands only while it has the focus.
// The menu has already been asked to close: it leaves the tree in the next frame. The focus manager gives the focus back in a
// task it posts then, which is not bound to come before the next frame: where frames follow each other at once (X11 on Linux
// Mint, 2026-11-17) both frames used to pass before it, and Paste, Copy and Cut of the editor's context menu and of the Edit
// menu found no handler. So after the frame in which the menu left, the pending focus changes are applied at once, and f runs
// after one more frame, in which what the focus changed has been built. alive is asked before f runs.
func runWhenFocusReturns(binding *widgets.WidgetsBinding, alive func() bool, f func()) {
	scheduler := binding.Scheduler()
	scheduler.AddPostFrameCallback(func(time.Duration) {
		// The frame is over (no build is running): the focus that the menu gave up goes back now.
		binding.FocusManager().ApplyFocusChangesIfNeeded()
		scheduler.ScheduleFrame()
		scheduler.AddPostFrameCallback(func(time.Duration) {
			if alive() {
				f()
			}
		})
	})
	scheduler.ScheduleFrame()
}
