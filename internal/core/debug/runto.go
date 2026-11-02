package debug

import (
	"context"
	"sync"
	"time"
)

// Run to Cursor (step 25.5): a breakpoint at the cursor that lives until the program stops or ends, and a continue (or a start when no
// session runs).
//
// Ported from VS Code: src/vs/workbench/contrib/debug/browser/debugService.ts (runTo, addAndValidateBreakpoints)

// runToVerifyWait is how long a new breakpoint is waited for to be verified before the program is let go.
const runToVerifyWait = 2 * time.Second

// RunTo lets the program run to a place of a file: when a session is stopped its thread continues, and when none runs the selected
// configuration is started. A breakpoint is added at the place for the run, unless one is there already, and removed again as soon as
// the debugger stops or ends. column is 0 for the whole line. A program that runs (or is being started) is left alone.
func (s *Service) RunTo(ctx context.Context, path string, line, column int) error {
	state := s.State()
	if state != Inactive && state != Stopped {
		return nil
	}
	thread := s.model.Focus().Thread
	var temp *Breakpoint
	if len(s.model.Breakpoints(BreakpointFilter{OriginalPath: path, Line: line, Column: column})) == 0 {
		added, err := s.AddBreakpoints(ctx, path, []BreakpointData{{Line: line, Column: column}})
		if err != nil {
			s.removeTemporary(added)
			return err
		}
		if len(added) > 0 {
			temp = &added[0]
			if !temp.Verified {
				s.waitVerified(ctx, temp.ID)
			}
			// the thread that is stopped in the focused session, else a stopped thread of any session
			thread = s.stoppedThread(thread)
		}
	}
	if temp != nil {
		s.removeWhenItStops(temp.ID, state)
	}
	switch state {
	case Inactive:
		_, err := s.StartDebugging(ctx, nil, Target{}, SessionOptions{StartedByUser: true})
		return err
	case Stopped:
		if thread == nil {
			return nil
		}
		return thread.Continue(ctx)
	}
	return nil
}

// stoppedThread is the thread to continue: the one that was focused when it is stopped, else a stopped thread of any session.
func (s *Service) stoppedThread(focused *Thread) *Thread {
	if focused != nil && focused.Stopped() {
		return focused
	}
	for _, sess := range s.model.Sessions(false) {
		for _, t := range sess.Threads() {
			if t.Stopped() {
				return t
			}
		}
	}
	return focused
}

// waitVerified waits until the breakpoint is verified, for two seconds at most.
func (s *Service) waitVerified(ctx context.Context, id string) {
	done := make(chan struct{}, 1)
	cancel := s.model.Subscribe(func(ev Event) {
		if ev.Kind != EventBreakpoints {
			return
		}
		if bp, ok := s.model.Breakpoint(id); ok && bp.Verified {
			select {
			case done <- struct{}{}:
			default:
			}
		}
	})
	defer cancel()
	// it may have been verified between the add and the subscription
	if bp, ok := s.model.Breakpoint(id); ok && bp.Verified {
		return
	}
	timer := time.NewTimer(runToVerifyWait)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
	case <-ctx.Done():
	}
}

// removeWhenItStops removes the temporary breakpoint when a thread stops, or when the debugger ends after it ran (a session that ended
// before it ran, or a start that has not made its session yet, leave it).
func (s *Service) removeWhenItStops(id string, from State) {
	var cancel func()
	ready := make(chan struct{})
	var mu sync.Mutex
	active, done := from == Stopped, false
	cancel = s.model.Subscribe(func(ev Event) {
		now := s.State()
		mu.Lock()
		if now == Running || now == Initializing {
			active = true
		}
		finish := !done && (ev.Kind == EventStopped || active && now == Inactive)
		if finish {
			done = true
		}
		mu.Unlock()
		if !finish {
			return
		}
		go func() {
			<-ready
			cancel()
			if err := s.RemoveBreakpoints(s.base, id); err != nil {
				s.logger().Warn("removing the breakpoint of run to cursor failed", "err", err)
			}
		}()
	})
	close(ready)
}

// removeTemporary removes the breakpoints a failed run added.
func (s *Service) removeTemporary(added []Breakpoint) {
	for _, bp := range added {
		if err := s.RemoveBreakpoints(s.base, bp.ID); err != nil {
			s.logger().Warn("removing the breakpoint of run to cursor failed", "err", err)
		}
	}
}
