package app

import (
	"strconv"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
)

// stepKeyDelay is how long after the new stop shows the key is pressed.
var stepKeyDelay = 20 * time.Millisecond

// TestF10IsHonouredWhenTheLineOfTheStepIsShown presses F10 once the result of a step (the new value of the variable sum, which is read
// after the stop is focused) is on screen, a hundred times in a row: the keys of the steps need debugState to be "stopped", and it is by
// then. (Step 25.12 saw F10 ignored 12 times in 100. That was its harness taking the line of the breakpoint in the Breakpoints view for
// the top frame while the thread ran and the stack was empty; the real delay was the stop of a step waiting behind the 420 ms wait of the
// previous stop for the rest of its stack: see TestAStepDoesNotWaitForTheRestOfThePreviousStack in core/debug.)
func TestF10IsHonouredWhenTheLineOfTheStepIsShown(t *testing.T) {
	const steps = 100
	var cycles []daptest.Step
	for i := 1; i <= steps; i++ {
		cycles = append(cycles, daptest.Reply("next", nil))
		cycles = append(cycles, stopSteps("step", "v"+strconv.Itoa(i))...)
	}
	r := newDebugRig(t, debugFolder(t, scriptOf(joinSteps(stopSteps("breakpoint", "v0"), cycles)...)))
	r.stopped()
	ignored := 0
	for i := 1; i <= steps; i++ {
		if got := r.sum(); got != "v"+strconv.Itoa(i-1) {
			r.waitFor("the stop "+strconv.Itoa(i-1), func() bool { return r.sum() == "v"+strconv.Itoa(i-1) })
		}
		for end := time.Now().Add(stepKeyDelay); time.Now().Before(end); {
			r.ts.FlushPosted()
			r.ts.Pump(5 * time.Millisecond)
			time.Sleep(time.Millisecond)
		}
		if state := r.win.Keys.Get("debugState"); state != "stopped" {
			t.Errorf("step %d: the stop is shown and debugState is %v", i, state)
		}
		if !sendChord(r.ts, keybinding.Chord{Key: keybinding.F10}) {
			ignored++
			t.Errorf("step %d: F10 was ignored in the state %v", i, r.win.Keys.Get("debugState"))
			r.waitState("stopped")
			r.run(command.DebugStepOverCommand, nil)
		}
		r.waitFor("the next stop", func() bool { return r.sum() == "v"+strconv.Itoa(i) })
	}
	if ignored != 0 {
		t.Errorf("F10 was ignored %d times in %d", ignored, steps)
	}
}
