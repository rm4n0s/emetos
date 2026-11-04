package app

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The goldens of Testing in the editor (step 26.3), dark and light: the glyphs of every state with the messages after their lines, the peek
// of a failure with its diff, the peek of a failure with its stack and its output, and Test Results.

var testingEditorGoldenSize = ui.Size{Width: 1000, Height: 900}

// goldenPeek makes a rig of the theme with the test file open, run once, and the cursor on a line.
func goldenPeek(t *testing.T, theme string, openResults bool, cursorLine int) (*testingRig, *textEditor) {
	t.Helper()
	settings := map[string]any{"testing.openTesting": "neverOpen"}
	if openResults {
		settings["testing.openTesting"] = "openOnTestStart"
	}
	r, e := peekRig(t, testingOpts{theme: theme, size: testingEditorGoldenSize, now: (&stepClock{}).now, settings: settings})
	r.run(command.TestingRunAll, nil)
	r.finished()
	r.waitFor("the messages", func() bool { return len(e.view.TestMessages()) == 2 })
	e.view.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: cursorLine, Col: 0})})
	return r, e
}

// settleDiff gives the small diff editor of the peek the time to compute its diff and lay it out.
func (r *testingRig) settleDiff(e *textEditor) {
	r.t.Helper()
	if b := e.tst.peek.Body(); b != nil && b.Ready != nil {
		r.waitFor("the diff to be computed", b.Ready)
	}
	for range 25 {
		r.ts.FlushPosted()
		r.ts.Pump(10 * time.Millisecond)
		time.Sleep(5 * time.Millisecond)
	}
	// the scroll bars fade out (the fake clock moves at once)
	time.Sleep(1500 * time.Millisecond)
	r.ts.FlushPosted()
	r.ts.Pump(5 * time.Second)
}

func TestGoldenTestingInTheEditor(t *testing.T) {
	for _, th := range goldenThemesOfTheGrid {
		t.Run(th.name+" glyphs and messages", func(t *testing.T) {
			r, _ := goldenPeek(t, th.theme, false, 0)
			r.pump()
			testutil.MatchesGolden(r.ts, "testing_editor_glyphs_"+th.name)
		})
		t.Run(th.name+" peek with a diff", func(t *testing.T) {
			r, e := goldenPeek(t, th.theme, false, 19)
			r.openPeek(e)
			if cur, _ := e.tst.peek.Current(); !cur.HasDiff {
				t.Fatalf("the peek shows %+v, want the message with a diff", cur)
			}
			r.settleDiff(e)
			testutil.MatchesGolden(r.ts, "testing_peek_diff_"+th.name)
			if errs := r.ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("errors: %v", errs)
			}
		})
		t.Run(th.name+" peek with a stack", func(t *testing.T) {
			r, e := goldenPeek(t, th.theme, false, 12)
			r.openPeek(e)
			if cur, _ := e.tst.peek.Current(); len(cur.Stack) == 0 || cur.HasDiff {
				t.Fatalf("the peek shows %+v, want the message with a stack", cur)
			}
			r.settleDiff(e)
			testutil.MatchesGolden(r.ts, "testing_peek_stack_"+th.name)
		})
		t.Run(th.name+" test results", func(t *testing.T) {
			r, _ := goldenPeek(t, th.theme, true, 0)
			r.run(command.TestingShowMostRecentOutput, nil)
			res := r.win.shell.testing.results
			for _, n := range res.Model().Rows() {
				if n.Label == "TestDivide" {
					res.Select(n.Key)
				}
			}
			r.pump()
			testutil.MatchesGolden(r.ts, "testing_results_"+th.name)
		})
	}
}

// openPeek runs Peek Output and waits until the peek of the editor is open (a loaded machine can take a few frames to show it).
func (r *testingRig) openPeek(e *textEditor) {
	r.t.Helper()
	r.run(command.TestingOpenOutputPeek, nil)
	r.waitFor("the peek to open", func() bool { return e.tst.peek.Visible() })
}
