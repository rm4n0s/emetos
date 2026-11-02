package editor

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
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// The debugger in the editor (step 25.5): the glyph margin, the lines of the stack frames, the inline marks, the breakpoint widget, the
// debug hover, the inline values and the exception widget, on a small Go program. The editor knows nothing of sessions: these tests give
// it marks and a host that records what the user did.

const debugSample = `package main

import "fmt"

func main() {
	prices := []float64{1.5, 2.25, 3}
	sum := 0.0
	for _, p := range prices {
		sum += p
	}
	fmt.Println("total", sum)
	process(sum, len(prices))
}

func process(total float64, n int) {
	average := total / float64(n)
	fmt.Println(average)
}
`

// debugRig is the editor on the sample in a theme, with a host that records the presses.
type debugRig struct {
	t     *testing.T
	ts    *gluttertest.Tester
	vm    *ViewModel
	host  *DebugHost
	log   []string
	ready bool
	// evaluated are the expressions the hover evaluated, and result what the host answers.
	evaluated []string
	result    *DebugHoverContent
}

func newDebugRig(t *testing.T, themeName string, tweak func(*Options)) *debugRig {
	t.Helper()
	vm, tok := highlighted(t, debugSample)
	opts := vm.Options()
	opts.ScrollBeyondLastLine = false
	opts.RenderLineHighlight = "none"
	opts.OccurrencesHighlight, opts.SelectionHighlight = "off", false
	opts.CursorBlinking = "solid"
	opts.Debug = DebugOptions{InlineValues: "auto", OverviewRuler: true, InlineCandidates: true, MiddleClickAction: "logpoint"}
	opts.Hover = HoverOptions{Enabled: "on", Delay: 300, Sticky: true, HidingDelay: 300}
	if tweak != nil {
		tweak(&opts)
	}
	vm.SetOptions(opts)
	t.Cleanup(vm.DisposeHighlights)
	t.Cleanup(vm.DisposeDebug)
	t.Cleanup(vm.DisposeInlineValues)
	r := &debugRig{t: t, vm: vm}
	r.host = &DebugHost{
		Enabled: func() bool { return true },
		GlyphPress: func(line int, mods MouseModifiers, middle bool) {
			r.log = append(r.log, "glyph "+itoaTest(line)+map[bool]string{true: " middle", false: ""}[middle]+map[bool]string{true: " shift", false: ""}[mods.Shift])
		},
		InlinePress: func(line, col int, id string) {
			r.log = append(r.log, "inline "+itoaTest(line)+":"+itoaTest(col)+" "+id)
		},
		HoverReady: func() bool { return r.ready },
		Evaluate: func(expr string, rng buffer.Range, done func(*DebugHoverContent)) {
			r.evaluated = append(r.evaluated, expr)
			done(r.result)
		},
	}
	vm.SetDebugHost(r.host)
	th, err := theme.LoadBundled(themeName)
	if err != nil {
		t.Fatal(err)
	}
	r.ts = testutil.New(t, ui.Size{Width: 640, Height: 420})
	r.ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) {
		c.UIFontFamily, c.UIFontSize = testutil.MonoFamily, 12
		c.MonospaceFontFamily, c.MonospaceFontSize = testutil.MonoFamily, 13
	})
	pumpHighlighted(t, r.ts, vm, bundledColors(t, themeName), th.TokenTheme(nil))
	tok.Settle()
	r.settle()
	return r
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var d []byte
	for ; n > 0; n /= 10 {
		d = append([]byte{byte('0' + n%10)}, d...)
	}
	if neg {
		d = append([]byte{'-'}, d...)
	}
	return string(d)
}

func (r *debugRig) settle() {
	r.t.Helper()
	for range 3 {
		r.ts.Pump(1000000)
	}
	r.ts.FlushPosted()
	for range 3 {
		r.ts.Pump(1000000)
	}
}

func (r *debugRig) mouse() gluttertest.GestureOpts {
	return gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse}
}

// glyphAt is the middle of the glyph margin on a line (from 0).
func (r *debugRig) glyphAt(line int) ui.Offset {
	h := r.vm.Options().LineHeight
	return ui.Offset{Dx: h / 2, Dy: float64(line)*h + h/2}
}

func marks(kinds ...DebugBreakpointMark) []DebugBreakpointMark { return kinds }

func bp(line int, kind DebugGlyph, enabled, verified bool) DebugBreakpointMark {
	return DebugBreakpointMark{ID: "bp" + itoaTest(line), Line: line, Kind: kind, Enabled: enabled, Verified: verified, Activated: true}
}

// glyphMarks are one breakpoint of each look on the first lines of the sample: plain, unverified, disabled, conditional, log, function and
// unverified conditional.
func glyphMarks() []DebugBreakpointMark {
	un := bp(5, DebugGlyphBreakpoint, true, false)
	un.Hover = "Unverified breakpoint"
	cu := bp(10, DebugGlyphConditional, true, false)
	return marks(
		bp(4, DebugGlyphBreakpoint, true, true), un, bp(6, DebugGlyphBreakpoint, false, true),
		bp(7, DebugGlyphConditional, true, true), bp(8, DebugGlyphLog, true, true), bp(9, DebugGlyphFunction, true, true), cu,
	)
}

func TestGoldenDebugGlyphs(t *testing.T) {
	for _, tc := range []struct{ golden, theme string }{{"debug_glyphs_dark", "Dark Modern"}, {"debug_glyphs_light", "Light Modern"}} {
		t.Run(tc.theme, func(t *testing.T) {
			r := newDebugRig(t, tc.theme, nil)
			r.vm.SetDebugBreakpoints(glyphMarks())
			// the faint hint under the pointer, on a line with none
			r.vm.SetDebugGutterPointer(r.glyphAt(11).Dy, true)
			r.settle()
			noErrors(t, r.ts)
			testutil.MatchesGolden(r.ts, tc.golden)
		})
	}
}

func TestGoldenDebugStackFrames(t *testing.T) {
	for _, tc := range []struct{ golden, theme string }{{"debug_frames_dark", "Dark Modern"}, {"debug_frames_light", "Light Modern"}} {
		t.Run(tc.theme, func(t *testing.T) {
			r := newDebugRig(t, tc.theme, nil)
			stopped := bp(11, DebugGlyphBreakpoint, true, true)
			r.vm.SetDebugBreakpoints(marks(bp(7, DebugGlyphBreakpoint, true, true), stopped))
			// the top frame at a column (the arrow before it), and the focused frame of the caller
			r.vm.SetDebugFrames([]DebugFrameMark{{Line: 11, Col: 9, Top: true, Glyph: true}, {Line: 7, Glyph: true}})
			r.settle()
			noErrors(t, r.ts)
			testutil.MatchesGolden(r.ts, tc.golden)
		})
	}
}

func TestGoldenDebugInlineBreakpoints(t *testing.T) {
	r := newDebugRig(t, "Dark Modern", nil)
	line := 5 // prices := []float64{1.5, 2.25, 3}
	r.vm.SetDebugBreakpoints(marks(
		DebugBreakpointMark{ID: "a", Line: line, Col: 1, Kind: DebugGlyphBreakpoint, Enabled: true, Verified: true, Activated: true},
		DebugBreakpointMark{ID: "b", Line: line, Col: 20, Inline: true, Kind: DebugGlyphBreakpoint, Enabled: true, Verified: true, Activated: true},
		DebugBreakpointMark{ID: "c", Line: line, Col: 26, Inline: true, Kind: DebugGlyphBreakpoint, Enabled: false, Verified: true, Activated: true},
	))
	r.vm.SetDebugCandidates([]DebugCandidate{{Line: line, Col: 1}, {Line: line, Col: 20}, {Line: line, Col: 31}})
	r.settle()
	noErrors(t, r.ts)
	testutil.MatchesGolden(r.ts, "debug_inline_breakpoints_dark")
}

func TestPressingTheGlyphMarginAsksTheHost(t *testing.T) {
	r := newDebugRig(t, "Dark Modern", nil)
	for _, tc := range []struct {
		name  string
		do    func()
		wants string
	}{
		{"a click on a line", func() { r.ts.TapAt(r.glyphAt(5), r.mouse()) }, "glyph 5"},
		{"a click on the first line", func() { r.ts.TapAt(r.glyphAt(0), r.mouse()) }, "glyph 0"},
		{"a click in the numbers is not the glyph margin", func() { r.ts.TapAt(ui.Offset{Dx: 40, Dy: r.glyphAt(5).Dy}, r.mouse()) }, ""},
		{"a click in the text is not the glyph margin", func() { r.ts.TapAt(ui.Offset{Dx: 200, Dy: r.glyphAt(5).Dy}, r.mouse()) }, ""},
		{"a click after the last line", func() { r.ts.TapAt(ui.Offset{Dx: 8, Dy: 400}, r.mouse()) }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r.log = nil
			tc.do()
			got := strings.Join(r.log, ",")
			if got != tc.wants {
				t.Errorf("the host heard %q, want %q", got, tc.wants)
			}
		})
	}
	// the press moved no cursor
	if p := r.vm.Cursors().Primary().Position(); p.Line == 5 && p.Col == 0 {
		t.Errorf("the cursor moved to %v", p)
	}
}

func TestTheGlyphMarginDoesNothingWithoutADebugger(t *testing.T) {
	r := newDebugRig(t, "Dark Modern", nil)
	r.host.Enabled = func() bool { return false }
	r.vm.SetDebugGutterPointer(r.glyphAt(3).Dy, true)
	if _, shown := r.vm.DebugGlyphHint(); shown {
		t.Error("the hint shows without a debugger")
	}
	r.ts.TapAt(r.glyphAt(3), r.mouse())
	if len(r.log) != 0 {
		t.Errorf("the host heard %v", r.log)
	}
	r2 := newDebugRig(t, "Dark Modern", func(o *Options) { o.GlyphMargin = false })
	r2.ts.TapAt(ui.Offset{Dx: 4, Dy: 30}, r2.mouse())
	if len(r2.log) != 0 {
		t.Errorf("a gutter with no glyph margin set a breakpoint: %v", r2.log)
	}
}

func TestMiddleClickAndShiftAreGivenToTheHost(t *testing.T) {
	r := newDebugRig(t, "Dark Modern", nil)
	r.ts.TapAt(r.glyphAt(4), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.TertiaryButton)})
	if got := strings.Join(r.log, ","); got != "glyph 4 middle" {
		t.Errorf("a middle click: the host heard %q", got)
	}
}

func TestTheSecondaryPressInTheGlyphMarginIsRemembered(t *testing.T) {
	r := newDebugRig(t, "Dark Modern", nil)
	r.ts.TapAt(r.glyphAt(6), gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.SecondaryButton)})
	got := r.vm.TakeDebugMenuTarget()
	if !got.InGlyph || got.Line != 6 || got.Col != -1 {
		t.Errorf("the menu target = %+v", got)
	}
	if again := r.vm.TakeDebugMenuTarget(); again.InGlyph {
		t.Errorf("the target is kept after it was taken: %+v", again)
	}
	r.ts.TapAt(ui.Offset{Dx: 200, Dy: r.glyphAt(6).Dy}, gluttertest.GestureOpts{Kind: ui.PointerDeviceKindMouse, Buttons: foundation.Some(gestures.SecondaryButton)})
	if got := r.vm.TakeDebugMenuTarget(); got.InGlyph {
		t.Errorf("a press in the text is a glyph press: %+v", got)
	}
}

func TestBreakpointsMoveWithTheEdits(t *testing.T) {
	r := newDebugRig(t, "Dark Modern", nil)
	r.vm.SetDebugBreakpoints(marks(bp(5, DebugGlyphBreakpoint, true, true), bp(10, DebugGlyphLog, true, true)))
	r.vm.SetDebugFrames([]DebugFrameMark{{Line: 10, Top: true, Glyph: true}})
	// two lines are put in above both
	at := buffer.Position{Line: 3, Col: 0}
	if _, err := r.vm.Document().Edit([]document.Edit{{Range: buffer.Range{Start: at, End: at}, Text: "// one\n// two\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	var lines []int
	for _, b := range r.vm.DebugBreakpoints() {
		lines = append(lines, b.Line)
	}
	if len(lines) != 2 || lines[0] != 7 || lines[1] != 12 {
		t.Errorf("the breakpoints are on the lines %v, want [7 12]", lines)
	}
	if f := r.vm.DebugFrames(); len(f) != 1 || f[0].Line != 12 {
		t.Errorf("the frame moved to %+v", f)
	}
	// a line deleted takes its breakpoint to the line it joined
	r.vm.SetDebugBreakpoints(nil)
	if n := len(r.vm.DebugBreakpoints()); n != 0 {
		t.Errorf("%d breakpoints are left", n)
	}
}

func TestTheBreakpointsOfALineThatDoesNotExistAreNotKept(t *testing.T) {
	r := newDebugRig(t, "Dark Modern", nil)
	r.vm.SetDebugBreakpoints(marks(bp(900, DebugGlyphBreakpoint, true, true), bp(-1, DebugGlyphBreakpoint, true, true), bp(2, DebugGlyphBreakpoint, true, true)))
	if got := r.vm.DebugBreakpoints(); len(got) != 1 || got[0].Line != 2 {
		t.Errorf("breakpoints = %+v", got)
	}
}

func TestInlineMarksAreAskedForPresses(t *testing.T) {
	r := newDebugRig(t, "Dark Modern", nil)
	r.vm.SetDebugBreakpoints(marks(
		DebugBreakpointMark{ID: "a", Line: 5, Col: 1, Kind: DebugGlyphBreakpoint, Enabled: true, Verified: true, Activated: true},
		DebugBreakpointMark{ID: "b", Line: 5, Col: 20, Inline: true, Kind: DebugGlyphBreakpoint, Enabled: true, Verified: true, Activated: true},
	))
	r.vm.SetDebugCandidates([]DebugCandidate{{Line: 5, Col: 31}})
	r.settle()
	w := r.vm.GutterWidths(r.ts.Dispatcher().TextEngine())
	rect := func(col int) ui.Rect {
		textRO := r.ts.RenderObject(gluttertest.ByType[*textLayer]()).(*RenderEditorText)
		return textRO.RectOf(buffer.Position{Line: 5, Col: col})
	}
	// the inline breakpoint is drawn before its column, in the room the mark made
	b := rect(20)
	r.log = nil
	r.ts.TapAt(ui.Offset{Dx: w.Total() + b.Left + 4, Dy: r.glyphAt(5).Dy}, r.mouse())
	if got := strings.Join(r.log, ","); got != "inline 5:20 b" {
		t.Errorf("a press on the inline breakpoint: the host heard %q", got)
	}
	c := rect(31)
	r.log = nil
	r.ts.TapAt(ui.Offset{Dx: w.Total() + c.Left + 4, Dy: r.glyphAt(5).Dy}, r.mouse())
	if got := strings.Join(r.log, ","); got != "inline 5:31 " {
		t.Errorf("a press on a candidate: the host heard %q", got)
	}
}

var _ = services.LogicalKeyboardKeyEnter
var _ widgets.Widget
var _ = kit.DarkModern

// widgetRig opens the breakpoint widget on a line of the sample and records what it accepted.
func (r *debugRig) openWidget(spec BreakpointWidgetSpec, got *[]BreakpointWidgetResult, closed *int) {
	r.t.Helper()
	spec.Accept = func(res BreakpointWidgetResult) { *got = append(*got, res) }
	spec.Closed = func() { *closed++ }
	if err := r.vm.OpenBreakpointWidget(spec); err != nil {
		r.t.Fatal(err)
	}
	r.settle()
}

func TestGoldenBreakpointWidget(t *testing.T) {
	triggers := []BreakpointTrigger{{ID: "t1", Label: "main.go: 6", Description: "prices := []float64{1.5, 2.25, 3}"}, {ID: "t2", Label: "main.go: 9", Description: "sum += p"}}
	for _, th := range []struct{ suffix, theme string }{{"dark", "Dark Modern"}, {"light", "Light Modern"}} {
		for _, k := range []struct {
			name string
			spec BreakpointWidgetSpec
			text string
		}{
			{"condition", BreakpointWidgetSpec{Line: 8, Context: BreakpointCondition}, "p > 2"},
			{"hitcount", BreakpointWidgetSpec{Line: 8, Context: BreakpointHitCount}, "> 1"},
			{"log", BreakpointWidgetSpec{Line: 8, Context: BreakpointLogMessage}, "sum is {sum}"},
			{"trigger", BreakpointWidgetSpec{Line: 8, Context: BreakpointTriggerPoint, Triggers: triggers}, ""},
			{"empty", BreakpointWidgetSpec{Line: 8, Context: BreakpointCondition}, ""},
		} {
			t.Run(k.name+" "+th.suffix, func(t *testing.T) {
				r := newDebugRig(t, th.theme, nil)
				var got []BreakpointWidgetResult
				var closed int
				r.vm.SetDebugBreakpoints(marks(bp(8, DebugGlyphConditional, true, true)))
				r.openWidget(k.spec, &got, &closed)
				if k.text != "" {
					r.vm.BreakpointWidgetSetText(k.text)
					r.settle()
				}
				noErrors(t, r.ts)
				testutil.MatchesGolden(r.ts, "debug_breakpoint_widget_"+k.name+"_"+th.suffix)
			})
		}
	}
}

func TestTheBreakpointWidgetAcceptsWithEnterAndClosesWithEscape(t *testing.T) {
	r := newDebugRig(t, "Dark Modern", nil)
	var got []BreakpointWidgetResult
	var closed int
	r.openWidget(BreakpointWidgetSpec{Line: 8, Context: BreakpointCondition}, &got, &closed)
	if !r.vm.BreakpointWidgetOpen() {
		t.Fatal("the widget is not open")
	}
	// the widget has a zone of two rows under the line
	zones := r.vm.Zones()
	if len(zones) != 1 || zones[0].AfterLine != 9 || zones[0].Rows != breakpointWidgetRows {
		t.Fatalf("zones = %+v", zones)
	}
	if !r.vm.BreakpointWidgetFocused() {
		t.Error("the input of the widget has no keyboard")
	}
	r.vm.BreakpointWidgetSetText("sum > 3")
	r.ts.SendKeyEvent(services.LogicalKeyboardKeyEnter)
	r.settle()
	if r.vm.BreakpointWidgetOpen() || closed != 1 {
		t.Errorf("Enter left the widget open (%v) or did not close it (%d)", r.vm.BreakpointWidgetOpen(), closed)
	}
	if len(got) != 1 || got[0].Condition == nil || *got[0].Condition != "sum > 3" || got[0].HitCondition != nil || got[0].LogMessage != nil || got[0].Trigger {
		t.Fatalf("accepted %+v", got)
	}
	if len(r.vm.Zones()) != 0 {
		t.Errorf("the zone stayed: %+v", r.vm.Zones())
	}

	// Escape closes without accepting
	got, closed = nil, 0
	r.openWidget(BreakpointWidgetSpec{Line: 8, Context: BreakpointLogMessage, LogMessage: "kept"}, &got, &closed)
	r.vm.BreakpointWidgetSetText("changed")
	r.ts.SendKeyEvent(services.LogicalKeyboardKeyEscape)
	r.settle()
	if r.vm.BreakpointWidgetOpen() || closed != 1 || len(got) != 0 {
		t.Errorf("Escape: open %v, closed %d, accepted %v", r.vm.BreakpointWidgetOpen(), closed, got)
	}
}

func TestTheBreakpointWidgetRemembersWhatIsTypedForEachEntry(t *testing.T) {
	r := newDebugRig(t, "Dark Modern", nil)
	var got []BreakpointWidgetResult
	var closed int
	triggers := []BreakpointTrigger{{ID: "t1", Label: "a"}, {ID: "t2", Label: "b"}}
	r.openWidget(BreakpointWidgetSpec{Line: 4, Context: BreakpointCondition, Condition: "x > 1", Triggers: triggers}, &got, &closed)
	r.vm.BreakpointWidgetSetText("x > 2")
	r.vm.BreakpointWidgetSetContext(BreakpointHitCount)
	r.vm.BreakpointWidgetSetText("> 3")
	r.vm.BreakpointWidgetSetContext(BreakpointCondition)
	r.settle()
	r.vm.AcceptBreakpointWidget()
	if len(got) != 1 {
		t.Fatalf("accepted %+v", got)
	}
	res := got[0]
	if res.Condition == nil || *res.Condition != "x > 2" || res.HitCondition == nil || *res.HitCondition != "> 3" || res.LogMessage != nil {
		t.Errorf("accepted %+v", res)
	}

	// a trigger takes no other condition
	got = nil
	r.openWidget(BreakpointWidgetSpec{Line: 4, Context: BreakpointTriggerPoint, Condition: "x", Triggers: triggers, TriggeredBy: "t2"}, &got, &closed)
	r.vm.AcceptBreakpointWidget()
	res = got[0]
	if !res.Trigger || res.TriggeredBy != "t2" || res.Condition != nil || res.HitCondition != nil || res.LogMessage != nil {
		t.Errorf("a trigger accepted %+v", res)
	}
	// an empty expression clears the condition
	got = nil
	r.openWidget(BreakpointWidgetSpec{Line: 4, Context: BreakpointCondition, Condition: "x"}, &got, &closed)
	r.vm.BreakpointWidgetSetText("")
	r.vm.AcceptBreakpointWidget()
	if res := got[0]; res.Condition == nil || *res.Condition != "" {
		t.Errorf("an empty condition accepted %+v", res)
	}
}

func TestTheBreakpointWidgetClosesWhenAnotherOpens(t *testing.T) {
	r := newDebugRig(t, "Dark Modern", nil)
	var got []BreakpointWidgetResult
	var closed int
	r.openWidget(BreakpointWidgetSpec{Line: 4}, &got, &closed)
	r.openWidget(BreakpointWidgetSpec{Line: 6}, &got, &closed)
	if closed != 1 || len(r.vm.Zones()) != 1 || r.vm.Zones()[0].AfterLine != 7 {
		t.Errorf("closed %d, zones %+v", closed, r.vm.Zones())
	}
	if err := r.vm.OpenBreakpointWidget(BreakpointWidgetSpec{Line: 4000}); err == nil {
		t.Error("a widget under a line that does not exist")
	}
}

func TestGoldenExceptionWidget(t *testing.T) {
	for _, tc := range []struct{ golden, theme string }{{"debug_exception_dark", "Dark Modern"}, {"debug_exception_light", "Light Modern"}} {
		t.Run(tc.theme, func(t *testing.T) {
			r := newDebugRig(t, tc.theme, nil)
			r.vm.SetDebugFrames([]DebugFrameMark{{Line: 7, Top: true, Glyph: true}})
			info := DebugException{ID: "runtime error", Description: "index out of range [3] with length 3", StackTrace: "main.main()\n\t/work/main.go:8 +0x4e\nruntime.main()\n\t/usr/lib/go/src/runtime/proc.go:267 +0x2bd"}
			var copied string
			if err := r.vm.DebugShowException(7, info, func(s string) { copied = s }, nil); err != nil {
				t.Fatal(err)
			}
			r.settle()
			if !r.vm.DebugExceptionOpen() {
				t.Fatal("the widget is not open")
			}
			noErrors(t, r.ts)
			testutil.MatchesGolden(r.ts, tc.golden)
			_ = copied
		})
	}
}

func TestTheExceptionWidgetCopiesItsTextAndCloses(t *testing.T) {
	r := newDebugRig(t, "Dark Modern", nil)
	info := DebugException{ID: "ValueError", Description: "bad value", StackTrace: "File x.py, line 3"}
	var copied string
	var closed int
	if err := r.vm.DebugShowException(7, info, func(s string) { copied = s }, func() { closed++ }); err != nil {
		t.Fatal(err)
	}
	r.settle()
	if !r.vm.DebugExceptionOpen() {
		t.Fatal("not open")
	}
	if want := "Exception has occurred: ValueError\nbad value\nFile x.py, line 3"; info.ExceptionText() != want {
		t.Errorf("the text = %q", info.ExceptionText())
	}
	r.ts.Tap(gluttertest.At(gluttertest.ByType[*kit.IconButton](), 0), r.mouse())
	if copied != info.ExceptionText() {
		t.Errorf("copied %q", copied)
	}
	r.ts.Tap(gluttertest.At(gluttertest.ByType[*kit.IconButton](), 1), r.mouse())
	r.settle()
	if r.vm.DebugExceptionOpen() || closed != 1 || len(r.vm.Zones()) != 0 {
		t.Errorf("open %v, closed %d, zones %v", r.vm.DebugExceptionOpen(), closed, r.vm.Zones())
	}
	// an exception with no id has the plain title
	if got := (DebugException{}).ExceptionText(); got != "Exception has occurred." {
		t.Errorf("the text of an empty exception = %q", got)
	}
}

func stubHover(text string) *DebugHoverContent {
	return &DebugHoverContent{Width: 220, Height: 24, Body: func(bc widgets.BuildContext) widgets.Widget {
		return widgets.NewText(text)
	}}
}

func TestTheDebugHoverEvaluatesTheExpressionUnderThePointer(t *testing.T) {
	r := newDebugRig(t, "Dark Modern", nil)
	r.ready = true
	r.result = stubHover("sum = 6")
	at := buffer.Position{Line: 8, Col: 2} // sum += p
	if !r.vm.DebugHoverPointer(at, false) {
		t.Fatal("the debug hover did not take the pointer")
	}
	if len(r.evaluated) != 0 {
		t.Fatalf("evaluated %v before the delay", r.evaluated)
	}
	// the delay of the hover is 300 ms, and the debug hover waits twice as long
	r.ts.Pump(500 * time.Millisecond)
	if len(r.evaluated) != 0 {
		t.Fatalf("evaluated %v before the delay", r.evaluated)
	}
	r.ts.Pump(200 * time.Millisecond)
	r.settle()
	if len(r.evaluated) != 1 || r.evaluated[0] != "sum" {
		t.Fatalf("evaluated %v", r.evaluated)
	}
	h := r.vm.DebugHover()
	if h == nil || h.Range.Start != (buffer.Position{Line: 8, Col: 2}) || h.Range.End != (buffer.Position{Line: 8, Col: 5}) {
		t.Fatalf("hover %+v", h)
	}
	if !r.ts.Exists(gluttertest.ByText("sum = 6")) {
		t.Error("the content of the box is not on screen")
	}
	// the pointer stays on the expression: nothing is evaluated again
	r.vm.DebugHoverPointer(buffer.Position{Line: 8, Col: 3}, false)
	r.ts.Pump(time.Second)
	if len(r.evaluated) != 1 {
		t.Errorf("evaluated again: %v", r.evaluated)
	}
	// Alt gives the pointer to the hover of the language
	if r.vm.DebugHoverPointer(buffer.Position{Line: 8, Col: 3}, true) || r.vm.DebugHover() != nil {
		t.Error("Alt left the debug hover")
	}
}

func TestTheDebugHoverStepsAsideWhenThereIsNoValueOrNoSession(t *testing.T) {
	r := newDebugRig(t, "Dark Modern", nil)
	at := buffer.Position{Line: 8, Col: 2}
	if r.vm.DebugHoverPointer(at, false) {
		t.Error("the debug hover takes the pointer with no session stopped here")
	}
	r.ready = true
	r.result = nil // no value
	if !r.vm.DebugHoverPointer(at, false) {
		t.Fatal("not taken")
	}
	r.ts.Pump(time.Second)
	if len(r.evaluated) != 1 || r.vm.DebugHover() != nil {
		t.Errorf("evaluated %v, hover %+v", r.evaluated, r.vm.DebugHover())
	}
	// the end of a line is no expression
	r.vm.DebugHideHover()
	r.evaluated = nil
	r.vm.DebugHoverPointer(buffer.Position{Line: 3, Col: 0}, false)
	r.ts.Pump(time.Second)
	if len(r.evaluated) != 0 {
		t.Errorf("evaluated %v on a blank line", r.evaluated)
	}
	// the hover that is turned off shows nothing
	r2 := newDebugRig(t, "Dark Modern", func(o *Options) { o.Hover.Enabled = "off" })
	r2.ready = true
	if r2.vm.DebugHoverPointer(at, false) {
		t.Error("a debug hover with editor.hover.enabled off")
	}
}

func TestAnAnswerThatCamePastAnEditIsNotShown(t *testing.T) {
	r := newDebugRig(t, "Dark Modern", nil)
	r.ready = true
	var done func(*DebugHoverContent)
	r.host.Evaluate = func(expr string, _ buffer.Range, d func(*DebugHoverContent)) { done = d }
	r.vm.DebugHoverPointer(buffer.Position{Line: 8, Col: 2}, false)
	r.ts.Pump(time.Second)
	if done == nil {
		t.Fatal("not evaluated")
	}
	at := buffer.Position{Line: 0, Col: 0}
	if _, err := r.vm.Document().Edit([]document.Edit{{Range: buffer.Range{Start: at, End: at}, Text: "// x\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	done(stubHover("late"))
	if r.vm.DebugHover() != nil {
		t.Error("an answer for a text that is gone is shown")
	}
}

func TestShowDebugHoverEvaluatesAtOnce(t *testing.T) {
	r := newDebugRig(t, "Dark Modern", nil)
	r.ready = true
	r.result = stubHover("prices = []")
	if !r.vm.DebugShowHover(buffer.Position{Line: 5, Col: 3}) {
		t.Fatal("no hover")
	}
	if len(r.evaluated) != 1 || r.evaluated[0] != "prices" || r.vm.DebugHover() == nil {
		t.Errorf("evaluated %v, hover %+v", r.evaluated, r.vm.DebugHover())
	}
	r.ready = false
	if r.vm.DebugShowHover(buffer.Position{Line: 5, Col: 3}) {
		t.Error("a hover with no session")
	}
}

func TestTheInlineValuesFollowTheEditsAndLetBlameBe(t *testing.T) {
	r := newDebugRig(t, "Dark Modern", nil)
	r.vm.SetLineAnnotations([]LineAnnotation{{Line: 8, Text: "Ada, 3 days ago"}})
	r.vm.SetInlineValues([]LineAnnotation{
		{Line: 8, Text: "sum = 6, p = 3", Tight: true, Color: "editor.inlineValuesForeground", Background: "editor.inlineValuesBackground", Hover: "```\nsum = 6,\np = 3\n```"},
		{Line: 7, Text: "p = 3", Tight: true},
	})
	got := r.vm.LineAnnotations()
	var texts []string
	for _, a := range got {
		texts = append(texts, itoaTest(a.Line)+":"+a.Text)
	}
	// the values come before the blame text of their line
	if want := "7:p = 3 8:sum = 6, p = 3 8:Ada, 3 days ago"; strings.Join(texts, " ") != want {
		t.Errorf("annotations = %q, want %q", strings.Join(texts, " "), want)
	}
	at := buffer.Position{Line: 3, Col: 0}
	if _, err := r.vm.Document().Edit([]document.Edit{{Range: buffer.Range{Start: at, End: at}, Text: "// one\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := r.vm.LineAnnotations(); got[0].Line != 8 {
		t.Errorf("the values did not move with the text: %+v", got)
	}
	// the blame of the extension is replaced without taking the values with it
	r.vm.SetLineAnnotations(nil)
	if n := len(r.vm.LineAnnotations()); n != 2 {
		t.Errorf("%d annotations after the blame went", n)
	}
	r.vm.DisposeInlineValues()
	if n := len(r.vm.LineAnnotations()); n != 0 {
		t.Errorf("%d annotations after the values went", n)
	}
}

func TestGoldenInlineValues(t *testing.T) {
	for _, tc := range []struct{ golden, theme string }{{"debug_inline_values_dark", "Dark Modern"}, {"debug_inline_values_light", "Light Modern"}} {
		t.Run(tc.theme, func(t *testing.T) {
			r := newDebugRig(t, tc.theme, nil)
			r.vm.SetDebugFrames([]DebugFrameMark{{Line: 10, Top: true, Glyph: true}})
			r.vm.SetDebugBreakpoints(marks(bp(10, DebugGlyphBreakpoint, true, true)))
			r.vm.SetInlineValues([]LineAnnotation{
				{Line: 5, Text: "prices = []float64 len:3", Tight: true, Color: "editor.inlineValuesForeground", Background: "editor.inlineValuesBackground"},
				{Line: 6, Text: "sum = 6.75", Tight: true, Color: "editor.inlineValuesForeground", Background: "editor.inlineValuesBackground"},
				{Line: 7, Text: "p = 3, prices = []float64 len:3", Tight: true, Color: "editor.inlineValuesForeground", Background: "editor.inlineValuesBackground"},
				{Line: 8, Text: "sum = 6.75, p = 3", Tight: true, Color: "editor.inlineValuesForeground", Background: "editor.inlineValuesBackground"},
				{Line: 10, Text: "sum = 6.75", Tight: true, Color: "editor.inlineValuesForeground", Background: "editor.inlineValuesBackground"},
			})
			r.settle()
			noErrors(t, r.ts)
			testutil.MatchesGolden(r.ts, tc.golden)
		})
	}
}
