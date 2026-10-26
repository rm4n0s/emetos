package editor

import (
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// hoverVM is a view model with the sample diagnostics and a clock the test moves.
func hoverVM(t *testing.T) (*ViewModel, *fakeTimers) {
	t.Helper()
	opts := goOptions()
	opts.CursorBlinking = "solid"
	opts.Hover = HoverOptions{Enabled: "on", Delay: 300, Sticky: true, HidingDelay: 300, Above: true}
	vm := NewViewModel(newTestDoc(diagnosticsText), opts, nil)
	clock := &fakeTimers{}
	vm.SetTimer(clock.after)
	vm.SetDiagnostics(sampleDiagnostics())
	t.Cleanup(vm.HideHover)
	return vm, clock
}

func TestHoverShowsAfterTheDelayAndHidesAfterTheHidingDelay(t *testing.T) {
	vm, clock := hoverVM(t)
	over := positionOf(3, 8) // in "undefined"
	vm.HoverPointerMoved(over)
	clock.advance(299 * time.Millisecond)
	if vm.Hover() != nil {
		t.Fatal("the hover shows before the delay")
	}
	clock.advance(time.Millisecond)
	h := vm.Hover()
	if h == nil || len(h.Parts) != 1 || h.Parts[0].Diagnostic.Message != "undefined: undefined" {
		t.Fatalf("hover = %+v", h)
	}
	if h.Pinned {
		t.Error("the hover of the pointer is not pinned")
	}
	// moving inside the range keeps it
	vm.HoverPointerMoved(positionOf(3, 10))
	clock.advance(time.Second)
	if vm.Hover() == nil {
		t.Fatal("the hover went while the pointer is on the squiggle")
	}
	// a place with nothing hides it after the hiding delay
	vm.HoverPointerMoved(positionOf(1, 0))
	clock.advance(299 * time.Millisecond)
	if vm.Hover() == nil {
		t.Fatal("the hover hid before the hiding delay")
	}
	clock.advance(time.Millisecond)
	if vm.Hover() != nil {
		t.Error("the hover is still there after the hiding delay")
	}
}

func TestStickyHoverStaysWhileThePointerIsInTheBox(t *testing.T) {
	vm, clock := hoverVM(t)
	vm.HoverPointerMoved(positionOf(3, 8))
	clock.advance(300 * time.Millisecond)
	vm.HoverPointerLeft()
	vm.HoverBoxEntered(true) // the pointer went down into the box
	clock.advance(5 * time.Second)
	if vm.Hover() == nil {
		t.Fatal("the hover went away under the pointer")
	}
	vm.HoverBoxEntered(false)
	clock.advance(300 * time.Millisecond)
	if vm.Hover() != nil {
		t.Error("the hover stays after the pointer left the box")
	}
	// without stickiness it hides at once
	o := vm.Options()
	o.Hover.Sticky = false
	vm.SetOptions(o)
	vm.SetTimer(clock.after)
	vm.HoverPointerMoved(positionOf(3, 8))
	clock.advance(300 * time.Millisecond)
	vm.HoverPointerLeft()
	if vm.Hover() != nil {
		t.Error("a hover that is not sticky stays")
	}
}

func TestHoverOptions(t *testing.T) {
	for _, tc := range []struct {
		name  string
		hover HoverOptions
		wait  time.Duration
		want  bool
	}{
		{"off", HoverOptions{Enabled: "off", Delay: 100}, time.Second, false},
		{"a shorter delay", HoverOptions{Enabled: "on", Delay: 100}, 100 * time.Millisecond, true},
		{"no delay", HoverOptions{Enabled: "on"}, 0, true},
		{"the delay is not over", HoverOptions{Enabled: "on", Delay: 1000}, 999 * time.Millisecond, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vm, clock := hoverVM(t)
			o := vm.Options()
			o.Hover = tc.hover
			vm.SetOptions(o)
			vm.HoverPointerMoved(positionOf(3, 8))
			clock.advance(tc.wait)
			if got := vm.Hover() != nil; got != tc.want {
				t.Errorf("hover shown = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHoverStacksTheDiagnosticsOfAPosition(t *testing.T) {
	vm, _ := hoverVM(t)
	vm.SetDiagnostics(append(sampleDiagnostics(), Diagnostic{
		Range: buffer.Range{Start: positionOf(3, 8), End: positionOf(3, 12)}, Severity: DiagnosticWarning, Message: "inner",
		Source: "lint", Code: "L1", CodeHref: "https://example.com/L1",
	}))
	if !vm.ShowHoverAt(positionOf(3, 9)) {
		t.Fatal("no hover")
	}
	h := vm.Hover()
	if len(h.Parts) != 2 || h.Parts[0].Diagnostic.Message != "undefined: undefined" || h.Parts[1].Diagnostic.Message != "inner" {
		t.Fatalf("parts = %+v", h.Parts)
	}
	if h.Range != (buffer.Range{Start: positionOf(3, 6), End: positionOf(3, 15)}) {
		t.Errorf("the hover is placed over %v", h.Range)
	}
	if !h.Pinned {
		t.Error("the hover of a command is pinned")
	}
	if vm.ShowHoverAt(positionOf(1, 0)) || vm.Hover() != nil {
		t.Error("a place with no diagnostics has no hover")
	}
}

func TestHoverGoesWhenTheTextOrTheCursorChanges(t *testing.T) {
	for _, tc := range []struct {
		name string
		do   func(vm *ViewModel)
	}{
		{"typing", func(vm *ViewModel) { _ = vm.RunCommand("type", map[string]any{"text": "x"}) }},
		{"a cursor move", func(vm *ViewModel) { _ = vm.RunCommand("cursorDown", nil) }},
		{"a press", func(vm *ViewModel) { vm.HideHover() }},
		{"Escape", func(vm *ViewModel) { _ = vm.RunCommand("closeMarkersNavigation", nil) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vm, _ := hoverVM(t)
			vm.Cursors().SetSelections([]buffer.Selection{cur(3, 6)})
			if !vm.ShowHoverAt(positionOf(3, 6)) {
				t.Fatal("no hover")
			}
			// the command that shows it keeps it
			vm.SelectionsChanged()
			if vm.Hover() == nil {
				t.Fatal("the hover went with no change")
			}
			tc.do(vm)
			if vm.Hover() != nil {
				t.Error("the hover is still there")
			}
		})
	}
}

func TestAPinnedHoverIgnoresThePointer(t *testing.T) {
	vm, clock := hoverVM(t)
	vm.ShowHoverAt(positionOf(3, 8))
	vm.HoverPointerLeft()
	vm.HoverPointerMoved(positionOf(1, 0))
	clock.advance(time.Second)
	if h := vm.Hover(); h == nil || !h.Pinned {
		t.Error("the pointer took a pinned hover away")
	}
	vm.SetScrollTop(100) // the editor scrolls: a pinned hover scrolls with the text
	if vm.Hover() == nil {
		t.Error("the scroll took a pinned hover away")
	}
}

func TestAHoverOfThePointerGoesWhenTheEditorScrolls(t *testing.T) {
	vm, clock := hoverVM(t)
	vm.HoverPointerMoved(positionOf(3, 8))
	clock.advance(300 * time.Millisecond)
	if vm.Hover() == nil {
		t.Fatal("no hover")
	}
	vm.SetScrollTop(50)
	if vm.Hover() != nil {
		t.Error("the hover stays when the text scrolls away from the pointer")
	}
}

func TestViewProblemIsAnActionOfTheDiagnosticPart(t *testing.T) {
	vm, _ := hoverVM(t)
	var got []buffer.Position
	vm.SetViewProblem(func(p buffer.Position) { got = append(got, p) })
	vm.ShowHoverAt(positionOf(3, 8))
	parts := vm.Hover().Parts
	if len(parts) != 1 || len(parts[0].Actions) != 1 || parts[0].Actions[0].Label != "View Problem" {
		t.Fatalf("actions = %+v", parts)
	}
	parts[0].Actions[0].Run()
	if len(got) != 1 || got[0] != positionOf(3, 6) {
		t.Errorf("View Problem was asked for %v", got)
	}
}

// ---- on screen ----

// hoverRig is the diagnostics rig with the pointer helpers of the mouse tests.
func hoverRig(t *testing.T, colors kit.Colors) (*ViewModel, *gluttertest.Tester, func(line, col int) ui.Offset) {
	t.Helper()
	vm, ts := diagnosticsRig(t, ui.Size{Width: 560, Height: 240}, colors, nil)
	gutter := vm.GutterWidths(ts.Dispatcher().TextEngine()).Total()
	cw := newGeometry(vm, ts.Dispatcher().TextEngine()).charWidth
	// at is a point of a line at a visual column: a tab counts for four.
	at := func(line, col int) ui.Offset {
		return ui.Offset{Dx: gutter + float64(col)*cw + 1, Dy: float64(line)*19 + 9}
	}
	return vm, ts, at
}

func TestThePointerShowsTheHoverOfASquiggle(t *testing.T) {
	vm, ts, at := hoverRig(t, kit.DarkModern())
	ts.Hover(at(3, 11)) // the tab is four columns wide: byte column 8
	ts.Pump(200 * time.Millisecond)
	if vm.Hover() != nil {
		t.Fatal("the hover came before the delay")
	}
	ts.Pump(200 * time.Millisecond)
	if vm.Hover() == nil || !ts.Exists(gluttertest.ByText("undefined: undefined")) {
		t.Fatalf("the hover does not show: %+v", vm.Hover())
	}
	if !ts.Exists(gluttertest.ByText("compiler(UndeclaredName)")) {
		t.Error("the source and the code are not in the hover")
	}
	// the pointer goes to a place with no diagnostic: the hover hides after the hiding delay
	ts.Hover(at(1, 2))
	ts.Pump(500 * time.Millisecond)
	if vm.Hover() != nil || ts.Exists(gluttertest.ByText("undefined: undefined")) {
		t.Error("the hover stays")
	}
	noErrors(t, ts)
}

func TestHoverLinks(t *testing.T) {
	vm, ts, _ := hoverRig(t, kit.DarkModern())
	var uris []string
	var places []string
	vm.SetOpeners(func(u string) { uris = append(uris, u) }, func(r string, l, c int) { places = append(places, r) })
	vm.SetDiagnostics([]Diagnostic{{
		Range: buffer.Range{Start: positionOf(3, 6), End: positionOf(3, 15)}, Severity: DiagnosticError, Message: "undefined: undefined",
		Source: "compiler", Code: "UndeclaredName", CodeHref: "https://pkg.go.dev/UndeclaredName",
		Related: []DiagnosticRelated{{Resource: "/src/other.go", Message: "declared here", Line: 7, Column: 2}},
	}})
	vm.ShowHoverAt(positionOf(3, 6))
	ts.Pump(50 * time.Millisecond)
	ts.Tap(gluttertest.ByText("UndeclaredName"))
	ts.Tap(gluttertest.ByText("other.go(7, 2)"))
	if len(uris) != 1 || uris[0] != "https://pkg.go.dev/UndeclaredName" {
		t.Errorf("opened URIs: %v", uris)
	}
	if len(places) != 1 || places[0] != "/src/other.go" {
		t.Errorf("opened places: %v", places)
	}
	if !ts.Exists(gluttertest.ByText(": declared here")) {
		t.Error("the message of the related place is not shown")
	}
	noErrors(t, ts)
}

func TestGoldenDiagnosticHover(t *testing.T) {
	for _, tc := range []struct{ golden, theme string }{
		{"diagnostic_hover_dark", "Dark Modern"},
		{"diagnostic_hover_light", "Light Modern"},
	} {
		t.Run(tc.theme, func(t *testing.T) {
			vm, ts := diagnosticsRig(t, ui.Size{Width: 560, Height: 240}, bundledColors(t, tc.theme), nil)
			vm.SetViewProblem(func(buffer.Position) {})
			vm.SetDiagnostics(append(sampleDiagnostics(), Diagnostic{
				Range: buffer.Range{Start: positionOf(3, 10), End: positionOf(3, 14)}, Severity: DiagnosticWarning,
				Message: "a second problem under the same word,\nwhich has two lines", Source: "lint", Code: "L1", CodeHref: "https://example.com/L1",
			}))
			vm.ShowHoverAt(positionOf(3, 11))
			ts.Pump(50 * time.Millisecond)
			ts.FlushPosted()
			ts.Pump(50 * time.Millisecond)
			noErrors(t, ts)
			testutil.MatchesGolden(ts, tc.golden)
		})
	}
}

func TestHoverStandsAboveTheLineWhenThereIsRoomAndTheOptionSaysSo(t *testing.T) {
	for _, tc := range []struct {
		name      string
		line, col int
		option    bool
		wantAbove bool
		golden    string
	}{
		{"above", 7, 2, true, true, "diagnostic_hover_above"},
		{"the option is off", 7, 2, false, false, ""},
		{"no room above the first lines", 3, 8, true, false, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vm, ts := diagnosticsRig(t, ui.Size{Width: 560, Height: 240}, kit.DarkModern(), func(o *Options) { o.Hover.Above = tc.option })
			vm.SetViewProblem(func(buffer.Position) {}) // the hover has its row of links: it is 70 pixels high
			vm.ShowHoverAt(positionOf(tc.line, tc.col))
			ts.Pump(50 * time.Millisecond)
			ts.FlushPosted()
			ts.Pump(50 * time.Millisecond)
			first := vm.Hover().Parts[0].Diagnostic.Message
			box := gluttertest.ByText(first)
			if !ts.Exists(box) {
				t.Fatal("the hover does not show")
			}
			rowTop := float64(tc.line) * 19
			if above := ts.TopLeft(box).Dy < rowTop; above != tc.wantAbove {
				t.Errorf("the hover is at %v for a line at %v: above = %v, want %v", ts.TopLeft(box).Dy, rowTop, above, tc.wantAbove)
			}
			if tc.golden != "" {
				noErrors(t, ts)
				testutil.MatchesGolden(ts, tc.golden)
			}
		})
	}
}

func TestMovingInsideTheRangeDoesNotRestartTheWaitForTheHover(t *testing.T) {
	vm, clock := hoverVM(t)
	vm.HoverPointerMoved(positionOf(3, 7))
	clock.advance(200 * time.Millisecond)
	vm.HoverPointerMoved(positionOf(3, 9)) // still on "undefined"
	clock.advance(100 * time.Millisecond)
	if vm.Hover() == nil {
		t.Error("the wait started again when the pointer moved inside the range")
	}
}
