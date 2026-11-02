package app

import (
	"image"
	"sync/atomic"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/debug"
)

// The question of closing a window while sessions run (debug.confirmOnExit, step 25.11) and the tasks of a session that a client starts
// (D46).

func TestDebugConfirmOnExit(t *testing.T) {
	tests := []struct {
		name     string
		setting  string
		sessions int
		answer   string // "Stop Debugging", "Cancel", "Escape"
		message  string // the question, empty when none is asked
		proceed  bool
		running  int // sessions left
	}{
		{"never asks nothing", "never", 1, "", "", true, 1},
		{"always with no session asks nothing", "always", 0, "", "", true, 0},
		{"always with a session: Stop Debugging", "always", 1, "Stop Debugging", "There is an active debug session, are you sure you want to stop it?", true, 0},
		{"always with a session: Cancel", "always", 1, "Cancel", "There is an active debug session, are you sure you want to stop it?", false, 1},
		{"always with a session: Escape", "always", 1, "Escape", "There is an active debug session, are you sure you want to stop it?", false, 1},
		{"always with sessions: Stop Debugging", "always", 2, "Stop Debugging", "There are active debug sessions, are you sure you want to stop them?", true, 0},
		{"always with sessions: Cancel", "always", 2, "Cancel", "There are active debug sessions, are you sure you want to stop them?", false, 2},
	}
	for _, tc := range tests {
		for reason, label := range map[CloseReason]string{CloseReasonQuit: "quit", CloseReasonTab: "tab"} {
			t.Run(tc.name+" ("+label+")", func(t *testing.T) {
				r := newDebugRigWith(t, tasksFolder(t, fakeConfig("one", ""), fakeConfig("two", "")), rigOpts{settings: map[string]any{"debug.confirmOnExit": tc.setting}})
				for _, name := range []string{"one", "two"}[:tc.sessions] {
					r.startOne(name, debug.SessionOptions{})
				}
				ch := make(chan closeResult, 1)
				r.win.closeAsk(reason, func(res closeResult) { ch <- res })
				if tc.message != "" {
					r.waitFor("the question", func() bool { return r.win.Dialogs.IsShowing() })
					spec, _ := r.win.Dialogs.Current()
					if spec.Message != tc.message {
						t.Errorf("the question is %q, want %q", spec.Message, tc.message)
					}
					if len(spec.Buttons) != 2 || spec.Buttons[0] != "Stop Debugging" || spec.Buttons[1] != "Cancel" {
						t.Errorf("the buttons are %v", spec.Buttons)
					}
					if tc.answer == "Escape" {
						r.win.Dialogs.Dismiss()
					} else {
						r.answerDialog(tc.answer, false)
					}
				}
				var res closeResult
				r.waitFor("the answer to closing", func() bool {
					select {
					case res = <-ch:
						return true
					default:
						return false
					}
				})
				if res.proceed != tc.proceed {
					t.Errorf("closing goes on: %v, want %v", res.proceed, tc.proceed)
				}
				if tc.message == "" && r.win.Dialogs.IsShowing() {
					t.Error("a question was asked")
				}
				if got := len(r.svc().Model().Sessions(false)); got != tc.running {
					t.Errorf("%d sessions are left, want %d", got, tc.running)
				}
				r.stopAll()
			})
		}
	}
}

func TestDebugConfirmOnExitIsAskedOnlyOnceAtATime(t *testing.T) {
	r := newDebugRigWith(t, debugFolder(t, scriptOf(stopSteps("breakpoint", "1")...)), rigOpts{settings: map[string]any{"debug.confirmOnExit": "always"}})
	r.startOne("Debug the order", debug.SessionOptions{})
	first := make(chan closeResult, 1)
	r.win.closeAsk(CloseReasonTab, func(res closeResult) { first <- res })
	r.waitFor("the question", func() bool { return r.win.Dialogs.IsShowing() })
	second := make(chan closeResult, 1)
	r.win.closeAsk(CloseReasonTab, func(res closeResult) { second <- res })
	select {
	case res := <-second:
		if res.proceed {
			t.Error("a second request went through")
		}
	default:
		t.Error("the second request waited")
	}
	if r.win.Dialogs.Pending() != 0 {
		t.Error("a second question was queued")
	}
	r.win.Dialogs.Dismiss()
	r.waitFor("the first answer", func() bool {
		select {
		case res := <-first:
			return !res.proceed
		default:
			return false
		}
	})
	r.stopAll()
}

func TestAClientStartsASessionWhoseTasksRunOnTheHost(t *testing.T) {
	folder := tasksFolder(t,
		fakeConfig("broken build", `"preLaunchTask": "broken"`),
		fakeConfig("with tasks", `"preLaunchTask": "prep"`))
	r := tasksRig(t, folder, nil)
	r.breakpoint() // the script of the fixture answers a setBreakpoints
	r.run(command.ViewContainerDebug, nil)
	v, err := r.win.makeHeadlessView(extension.Size{Width: 900, Height: 560}, rigFrameRate)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(v.Close)
	var frames atomic.Int64
	v.OnFrame(func(*image.RGBA) { frames.Add(1) })
	r.waitFor("the view to draw", func() bool { return frames.Load() > 0 && v.ws.shell != nil && v.ws.shell.debug.panes != nil })
	client := v.ws
	start := func(name string) {
		launches := r.svc().Launches()
		launches.Select(launches.All()[0].Launch, name, debug.SelectOptions{})
		if err := client.Commands.Run(command.Context{Ctx: t.Context(), Keys: client.Keys, Source: command.SourceAPI}, command.DebugStartCommand, nil); err != nil {
			t.Fatal(err)
		}
	}

	// a failing task asks the client, not the host; the client answers (Escape is Abort) and nothing starts
	start("broken build")
	var shown bool
	r.waitFor("the question on the client", func() bool {
		shown = client.Dialogs.IsShowing() // the view shares the UI thread of the host
		return shown
	})
	if r.win.Dialogs.IsShowing() {
		t.Error("the host was asked, not the client")
	}
	client.Dialogs.Dismiss()
	r.waitFor("the answer", func() bool {
		shown = client.Dialogs.IsShowing() // the view shares the UI thread of the host
		return !shown
	})
	if got := len(r.svc().Model().Sessions(false)); got != 0 {
		t.Fatalf("%d sessions started after Abort", got)
	}

	// a task that passes runs in the folder of the host, and the session starts there
	start("with tasks")
	r.waitFor("the host to stop", func() bool {
		return r.win.Keys.Get("debugState") == "stopped" && client.Keys.Get("debugState") == "stopped"
	})
	if !r.exists("pre.txt") {
		t.Error("the task did not run in the folder of the host")
	}
	r.stopAll()
}
