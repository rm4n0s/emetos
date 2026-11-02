package app

import (
	"context"
	"time"

	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/gui/kit"
)

// The question of closing a window while a debug session runs (debug.confirmOnExit, SPEC §10.3, §10.31). It comes after the questions about
// unsaved changes, so that a Cancel there ends nothing, and when the user agrees the sessions are stopped before the window goes.
//
// Ported from VS Code: src/vs/workbench/contrib/debug/common/debugLifecycle.ts

// debugExitStopTimeout is how long closing waits for the sessions to end after the user agreed to stop them; an adapter that does not
// answer is left to the end of the process.
const debugExitStopTimeout = 5 * time.Second

// rootDebugSessions is how many sessions that are not children of another are running.
func (w *Workspace) rootDebugSessions() int {
	svc := w.DebugService()
	if svc == nil {
		return 0
	}
	n := 0
	for _, s := range svc.Model().Sessions(false) {
		if s.Parent() == nil {
			n++
		}
	}
	return n
}

// debugExitMessage is the question: VS Code's two texts.
func debugExitMessage(sessions int) string {
	if sessions == 1 {
		return "There is an active debug session, are you sure you want to stop it?"
	}
	return "There are active debug sessions, are you sure you want to stop them?"
}

// confirmDebugOnClose asks whether to stop the running debug sessions when the window closes, if debug.confirmOnExit is "always" and
// sessions run, and tells done whether to go on; on "Stop Debugging" the sessions are stopped first. UI thread; done is called on it.
func (w *Workspace) confirmDebugOnClose(done func(proceed bool)) {
	sessions := w.rootDebugSessions()
	if sessions == 0 || w.isView() || settings.Get[string](w.Settings, "debug.confirmOnExit", "") != "always" {
		done(true)
		return
	}
	if w.debugExitAsking {
		done(false) // the question is open already
		return
	}
	w.debugExitAsking = true
	w.Raise()
	w.Dialogs.Show(kit.DialogSpec{
		Type: kit.DialogWarning, Message: debugExitMessage(sessions),
		Buttons: []string{"Stop Debugging", "Cancel"}, CancelButton: foundation.Some(1),
		OnAnswer: func(r kit.DialogResult) {
			w.debugExitAsking = false
			if r.Cancelled || r.Button != 0 {
				done(false)
				return
			}
			svc := w.DebugService()
			go func() {
				ctx, cancel := context.WithTimeout(context.WithoutCancel(w.ctx), debugExitStopTimeout)
				defer cancel()
				if err := svc.Stop(ctx, nil, false, false); err != nil {
					w.Process.Logger.Warn("the debug sessions did not stop for the closing window", "err", err)
				}
				w.onUI(func() { done(true) })
			}()
		},
	})
}
