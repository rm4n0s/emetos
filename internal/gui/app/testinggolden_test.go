package app

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The goldens of the Test Explorer (step 26.2), dark and light: the tests after a run (passed, failed and skipped, with durations and the
// summary), a run that goes on, the filter, and the list. The controller is the fake one of extensions/devtesting.

var testingGoldenSize = ui.Size{Width: 900, Height: 440}

// stepClock is a clock that moves 700 ms each time it is read, so that the duration of a run in the summary does not depend on the speed
// of the machine (a run reads it twice: when it starts and when it ends).
type stepClock struct {
	n atomic.Int64
}

var clockBase = time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC)

func (c *stepClock) now() time.Time {
	return clockBase.Add(time.Duration(c.n.Add(1)) * 700 * time.Millisecond)
}

// goldenTesting makes a rig of the theme with the Test Explorer open.
func goldenTesting(t *testing.T, theme string, script string, clock *stepClock) *testingRig {
	t.Helper()
	r := newTestingRig(t, testingOpts{theme: theme, size: testingGoldenSize, script: script, now: clock.now,
		settings: map[string]any{"testing.openTesting": "neverOpen"}}) // the goldens of the Explorer have no panel under the editor
	r.run(command.ViewContainerTesting, nil)
	return r
}

func TestGoldenTestExplorer(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name+" before a run", func(t *testing.T) {
			r := goldenTesting(t, th.theme, "", &stepClock{})
			r.pump()
			testutil.MatchesGolden(r.ts, "testing_idle_"+th.name)
		})
		t.Run(th.name+" after a run", func(t *testing.T) {
			r := goldenTesting(t, th.theme, "", &stepClock{})
			r.run(command.TestingRunAll, nil)
			r.finished()
			testutil.MatchesGolden(r.ts, "testing_run_"+th.name)
			if errs := r.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
		t.Run(th.name+" while running", func(t *testing.T) {
			// the first test is going, the others wait their turn
			script := `{"delayMs": 600000, "tests": [{"id": "a_test.go", "label": "a_test.go", "children": [
				{"id": "TestOne", "label": "TestOne", "result": "passed", "ms": 1}, {"id": "TestTwo", "label": "TestTwo", "result": "passed", "ms": 1},
				{"id": "TestThree", "label": "TestThree", "result": "failed", "ms": 1}]}]}`
			clock := &stepClock{}
			r := goldenTesting(t, th.theme, script, clock)
			r.run(command.TestingRunAll, nil)
			r.waitFor("the first test to go", func() bool { return r.state("a_test.go", "TestOne") == 2 })
			testutil.MatchesGolden(r.ts, "testing_running_"+th.name)
			r.run(command.TestingCancelRun, nil)
			r.finished()
		})
		t.Run(th.name+" filtered", func(t *testing.T) {
			r := goldenTesting(t, th.theme, "", &stepClock{})
			r.run(command.TestingRunAll, nil)
			r.finished()
			r.explorer().SetFilterText("@failed")
			r.pump()
			testutil.MatchesGolden(r.ts, "testing_filtered_"+th.name)
		})
		t.Run(th.name+" as a list, by status", func(t *testing.T) {
			r := goldenTesting(t, th.theme, "", &stepClock{})
			r.run(command.TestingRunAll, nil)
			r.finished()
			r.run(command.TestingViewAsList, nil)
			r.run(command.TestingSortByStatus, nil)
			testutil.MatchesGolden(r.ts, "testing_list_"+th.name)
		})
	}
}
