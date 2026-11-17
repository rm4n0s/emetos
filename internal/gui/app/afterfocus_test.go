package app

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/services"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// framesBeforeTasks draws frames the way X11 on Linux Mint does: one after the other, before the tasks posted in the
// meantime run (the focus manager posts the one that gives the focus back when a menu leaves the tree).
func framesBeforeTasks(ts *gluttertest.Tester, n int) {
	sched := ts.Binding.Scheduler()
	for range n {
		if !sched.HasScheduledFrame() {
			return
		}
		ts.Clock.Advance(time.Millisecond)
		sched.HandleBeginFrame(foundation.Some(ts.Elapsed()))
		sched.HandleDrawFrame()
	}
}

// A command chosen in a menu or the Command Palette reaches the editor although the frames come before the task that gives the
// focus back to it: on Linux Mint the editor's context menu and the Edit menu did nothing for Paste, Copy and Cut ("the command
// has no handler"), while the keys worked (2026-11-17).
func TestAMenuCommandReachesTheEditorWhenFramesComeBeforeTheFocus(t *testing.T) {
	mouse := func(button int) gluttertest.GestureOpts {
		return gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(button)}
	}
	// press clicks the middle of a text as a mouse does: the press and the release, nothing more
	press := func(a *keyApp, f gluttertest.Finder, button int) {
		g := a.ts.StartGesture(a.ts.Center(f), mouse(button))
		g.Up()
	}
	tests := []struct {
		name string
		// choose chooses Paste; it leaves the last step undone by frames, as a click or a key does
		choose func(t *testing.T, a *keyApp)
	}{
		{"the editor's context menu", func(t *testing.T, a *keyApp) {
			region := gluttertest.ByType[*kit.ContextMenuRegion]()
			at, size := a.ts.TopLeft(region), a.ts.Size(region)
			g := a.ts.StartGesture(ui.Offset{Dx: at.Dx + 120, Dy: at.Dy + size.Height/3}, mouse(gestures.SecondaryButton))
			g.Up()
			a.pump()
			press(a, gluttertest.ByText("Paste"), gestures.PrimaryButton)
		}},
		{"the Edit menu", func(t *testing.T, a *keyApp) {
			a.ts.Tap(gluttertest.ByText("Edit"))
			a.pump()
			press(a, gluttertest.ByText("Paste"), gestures.PrimaryButton)
		}},
		{"the Command Palette", func(t *testing.T, a *keyApp) {
			a.press(t, "ctrl+shift+p")
			a.ts.EnterText(gluttertest.First(gluttertest.ByType[*widgets.EditableText]()), ">Paste")
			a.pump()
			a.ts.SendKeyEvent(services.LogicalKeyboardKeyEnter)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newKeyApp(t, "alpha\n")
			a.vm.Cursors().SetSelections([]buffer.Selection{cursorAt(0, 0)})
			a.vm.SelectionsChanged()
			a.ts.SetClipboardText("pasted ")
			a.pump()
			tt.choose(t, a)
			framesBeforeTasks(a.ts, 4)
			a.pump() // the clipboard answers
			if got := a.text(); !strings.Contains(got, "pasted ") {
				t.Errorf("text %q: Paste did not reach the editor", got)
			}
			if !a.vm.Focused() {
				t.Error("the editor does not have the focus back")
			}
			if errs := a.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
	}
}
