package editor

import (
	"strings"
	"testing"

	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gestures"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The tests in the editor (step 26.3): the glyphs of the tests in the left lane of the glyph margin, their presses and menu, and the failure
// messages after their lines, on the small Go program of the debug tests. The editor knows nothing of the test model: these tests give it
// marks and a host that records what the user did.

// testRig is the debug rig with a test host that records the presses.
type testRig struct {
	*debugRig
	tlog []string
}

func newTestRig(t *testing.T, themeName string) *testRig {
	t.Helper()
	// the marks go before the window does: this cleanup is registered first, so it runs last, after the tester ended
	var vm *ViewModel
	t.Cleanup(func() { vm.DisposeTesting() })
	r := &testRig{debugRig: newDebugRig(t, themeName, nil)}
	vm = r.vm
	r.vm.SetTestHost(&TestHost{
		GlyphPress: func(p TestPress) {
			r.tlog = append(r.tlog, "test "+itoaTest(p.Line)+" "+strings.Join(p.IDs, "+"))
		},
	})
	return r
}

// stateGlyph is a glyph of a state: the icon and the color the window gives it.
func stateGlyph(line int, icon int, colorID, hover string) TestGlyphMark {
	return TestGlyphMark{Line: line, IDs: []string{"t" + itoaTest(line)}, Icon: icon, ColorID: colorID, Hover: hover}
}

// everyGlyph are the glyphs of every state, one on each line from the fifth: not run, not run (several), queued, running, passed, failed,
// errored, skipped and a passed one of an outdated result.
func everyGlyph() []TestGlyphMark {
	failed := stateGlyph(9, fonts.CodiconError, "testing.iconFailed", "Click to run tests, right click for more options")
	failed.Failed = true
	retired := stateGlyph(12, fonts.CodiconPass, "testing.iconPassed.retired", "Click to run tests, right click for more options")
	return []TestGlyphMark{
		stateGlyph(4, fonts.CodiconPlay, "testing.runAction", "Click to run tests, right click for more options"),
		stateGlyph(5, fonts.CodiconRunAll, "testing.runAction", "Click to run tests, right click for more options"),
		stateGlyph(6, fonts.CodiconHistory, "testing.iconQueued", ""),
		stateGlyph(7, fonts.CodiconLoading, "", ""),
		stateGlyph(8, fonts.CodiconPass, "testing.iconPassed", ""),
		failed,
		stateGlyph(10, fonts.CodiconIssues, "testing.iconErrored", ""),
		stateGlyph(11, fonts.CodiconDebugStepOver, "testing.iconSkipped", ""),
		retired,
	}
}

func TestGoldenTestGlyphs(t *testing.T) {
	for _, tc := range []struct{ golden, theme string }{{"testing_glyphs_dark", "Dark Modern"}, {"testing_glyphs_light", "Light Modern"}} {
		t.Run(tc.theme, func(t *testing.T) {
			r := newTestRig(t, tc.theme)
			r.vm.SetTestGlyphs(everyGlyph())
			// a breakpoint on a line with a test glyph (the margin has a lane for each) and one on a line with none
			r.vm.SetDebugBreakpoints(marks(bp(8, DebugGlyphBreakpoint, true, true), bp(14, DebugGlyphBreakpoint, true, true)))
			r.settle()
			noErrors(t, r.ts)
			testutil.MatchesGolden(r.ts, tc.golden)
		})
	}
}

func TestGoldenTestMessages(t *testing.T) {
	for _, tc := range []struct{ golden, theme string }{{"testing_messages_dark", "Dark Modern"}, {"testing_messages_light", "Light Modern"}} {
		t.Run(tc.theme, func(t *testing.T) {
			r := newTestRig(t, tc.theme)
			r.vm.SetTestGlyphs([]TestGlyphMark{
				{Line: 4, IDs: []string{"main"}, Icon: fonts.CodiconError, ColorID: "testing.iconFailed", Failed: true},
			})
			r.vm.SetTestMessages([]TestMessageMark{
				{Line: 8, Text: "expected 5.25, got 6.75", Hover: "expected 5.25, got 6.75"},
				{Line: 10, Text: "total printed twice", Info: true},
			})
			r.settle()
			noErrors(t, r.ts)
			testutil.MatchesGolden(r.ts, tc.golden)
		})
	}
}

func TestTheMarginHasALaneForTheTestGlyphsOnlyWhileThereAreSome(t *testing.T) {
	r := newTestRig(t, "Dark Modern")
	widths := func() float64 { return r.vm.GutterWidths(r.ts.Dispatcher().TextEngine()).GlyphMargin }
	line := r.vm.Options().LineHeight
	if got := widths(); got != line {
		t.Fatalf("the margin is %v wide with no test glyph, want %v", got, line)
	}
	r.vm.SetTestGlyphs(everyGlyph())
	if got := widths(); got != 2*line {
		t.Errorf("the margin is %v wide with test glyphs, want %v", got, 2*line)
	}
	enabled := false
	r.vm.SetTestHost(&TestHost{Enabled: func() bool { return enabled }})
	if got := widths(); got != line {
		t.Errorf("the margin is %v wide with the gutter off, want %v", got, line)
	}
	enabled = true
	if got := widths(); got != 2*line {
		t.Errorf("the margin is %v wide with the gutter on again, want %v", got, 2*line)
	}
	r.vm.SetTestGlyphs(nil)
	if got := widths(); got != line {
		t.Errorf("the margin is %v wide after the glyphs went, want %v", got, line)
	}
}

func TestPressingATestGlyphAsksTheHost(t *testing.T) {
	r := newTestRig(t, "Dark Modern")
	r.vm.SetTestGlyphs(everyGlyph())
	r.settle()
	h := r.vm.Options().LineHeight
	left := func(line int) ui.Offset { return ui.Offset{Dx: h / 2, Dy: float64(line)*h + h/2} }
	right := func(line int) ui.Offset { return ui.Offset{Dx: h * 1.5, Dy: float64(line)*h + h/2} }
	for _, tc := range []struct {
		name  string
		at    ui.Offset
		tests string
		debug string
	}{
		{"the left lane of a line with a test", left(4), "test 4 t4", ""},
		{"the left lane of the failed test", left(9), "test 9 t9", ""},
		{"the right lane of a line with a test is the breakpoint's", right(4), "", "glyph 4"},
		{"the left lane of a line with no test is the breakpoint's", left(14), "", "glyph 14"},
		{"the numbers are neither", ui.Offset{Dx: 2*h + 10, Dy: left(4).Dy}, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r.log, r.tlog = nil, nil
			r.ts.TapAt(tc.at, r.mouse())
			if got := strings.Join(r.tlog, ","); got != tc.tests {
				t.Errorf("the test host heard %q, want %q", got, tc.tests)
			}
			if got := strings.Join(r.log, ","); got != tc.debug {
				t.Errorf("the debug host heard %q, want %q", got, tc.debug)
			}
		})
	}
	// the press moved no cursor
	if p := r.vm.Cursors().Primary().Position(); p.Line == 4 && p.Col == 0 {
		t.Errorf("the cursor moved to %v", p)
	}
}

func TestTheSecondaryPressOnATestGlyphIsRemembered(t *testing.T) {
	r := newTestRig(t, "Dark Modern")
	r.vm.SetTestGlyphs(everyGlyph())
	r.settle()
	h := r.vm.Options().LineHeight
	right := gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.SecondaryButton)}
	r.ts.TapAt(ui.Offset{Dx: h / 2, Dy: 6*h + h/2}, right)
	got := r.vm.TakeTestMenuTarget()
	if !got.InGlyph || got.Line != 6 || strings.Join(got.IDs, "+") != "t6" {
		t.Errorf("the menu target = %+v", got)
	}
	if again := r.vm.TakeTestMenuTarget(); again.InGlyph {
		t.Errorf("the target is kept after it was taken: %+v", again)
	}
	r.ts.TapAt(ui.Offset{Dx: h / 2, Dy: 14*h + h/2}, right)
	if got := r.vm.TakeTestMenuTarget(); got.InGlyph {
		t.Errorf("a press on a line with no test is a test glyph press: %+v", got)
	}
}

func TestTheHoverOfATestGlyphAndOfAMessageShow(t *testing.T) {
	r := newTestRig(t, "Dark Modern")
	r.vm.SetTestGlyphs(everyGlyph())
	r.vm.SetTestMessages([]TestMessageMark{{Line: 8, Text: "expected 5.25, got 6.75", Hover: "the whole message"}})
	r.settle()
	h := r.vm.Options().LineHeight
	r.vm.SetGlyphPointer(h/2, 4*h+h/2)
	if r.vm.hover.op == nil && r.vm.hover.shownAnn == nil {
		t.Error("the hover of the glyph is not on its way")
	}
	// off the glyph the hover goes
	r.vm.SetGlyphPointer(0, -1)
	if r.vm.hover.shownAnn != nil && r.vm.hover.shownAnn.key == testGlyphKey {
		t.Error("the hover of the glyph stays after the pointer left")
	}
	// the message is an annotation with its own hover
	ann := r.vm.LineAnnotations()
	if len(ann) != 1 || ann[0].Line != 8 || ann[0].Hover != "the whole message" || !strings.Contains(ann[0].Text, "expected 5.25") {
		t.Errorf("annotations: %+v", ann)
	}
}

func TestTestMarksMoveWithTheEdits(t *testing.T) {
	r := newTestRig(t, "Dark Modern")
	r.vm.SetTestGlyphs([]TestGlyphMark{stateGlyph(5, fonts.CodiconPlay, "testing.runAction", ""), stateGlyph(10, fonts.CodiconPass, "testing.iconPassed", "")})
	r.vm.SetTestMessages([]TestMessageMark{{Line: 10, Text: "boom"}})
	at := buffer.Position{Line: 3, Col: 0}
	if _, err := r.vm.Document().Edit([]document.Edit{{Range: buffer.Range{Start: at, End: at}, Text: "// one\n// two\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	var lines []int
	for _, g := range r.vm.TestGlyphs() {
		lines = append(lines, g.Line)
	}
	if len(lines) != 2 || lines[0] != 7 || lines[1] != 12 {
		t.Errorf("the glyphs are at lines %v, want 7 and 12", lines)
	}
	if msgs := r.vm.TestMessages(); len(msgs) != 1 || msgs[12] != "boom" {
		t.Errorf("the messages are %v, want boom at 12", msgs)
	}
	// a glyph of a line the document does not have is not kept, and disposing takes everything away
	r.vm.SetTestGlyphs([]TestGlyphMark{stateGlyph(500, fonts.CodiconPlay, "", "")})
	if len(r.vm.TestGlyphs()) != 0 {
		t.Error("a glyph of a line the document does not have was kept")
	}
	r.vm.SetTestGlyphs([]TestGlyphMark{stateGlyph(5, fonts.CodiconPlay, "", "")})
	r.vm.DisposeTesting()
	if len(r.vm.TestGlyphs()) != 0 || len(r.vm.TestMessages()) != 0 || r.vm.TestHostSet() {
		t.Error("the test marks stayed after DisposeTesting")
	}
}
