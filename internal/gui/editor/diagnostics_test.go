package editor

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// diagnosticsText is the text of the goldens of the squiggles and of most of the tests.
const diagnosticsText = "package main\n" + // 0
	"\n" + // 1
	"func main() {\n" + // 2
	"\tx := undefined\n" + // 3
	"\ty := 1\n" + // 4
	"\tz := deprecatedCall()\n" + // 5
	"\tvar unused int\n" + // 6
	"\twarn := 2\n" + // 7
	"\t_ = hint\n" + // 8
	"}\n" // 9

// sampleDiagnostics has one of each severity and of each tag, on the lines of diagnosticsText.
func sampleDiagnostics() []Diagnostic {
	at := func(line, from, to int) buffer.Range {
		return buffer.Range{Start: positionOf(line, from), End: positionOf(line, to)}
	}
	return []Diagnostic{
		{Range: at(3, 6, 15), Severity: DiagnosticError, Message: "undefined: undefined", Source: "compiler", Code: "UndeclaredName"},
		{Range: at(4, 6, 7), Severity: DiagnosticWarning, Message: "the value is not used"},
		{Range: at(5, 6, 20), Severity: DiagnosticWarning, Message: "deprecatedCall is deprecated", Deprecated: true},
		{Range: at(6, 5, 11), Severity: DiagnosticHint, Message: "unused is not used", Unnecessary: true},
		{Range: at(7, 1, 5), Severity: DiagnosticInfo, Message: "warn could be renamed"},
		{Range: at(8, 6, 10), Severity: DiagnosticHint, Message: "consider something else"},
	}
}

// diagnosticsRig is an editor of diagnosticsText with the sample diagnostics, on screen in a theme.
func diagnosticsRig(t *testing.T, size ui.Size, colors kit.Colors, tweak func(*Options)) (*ViewModel, *gluttertest.Tester) {
	t.Helper()
	opts := goOptions()
	opts.ShowUnused, opts.ShowDeprecated, opts.CursorBlinking = true, true, "solid"
	opts.Minimap = MinimapOptions{Enabled: true, Side: "right", Size: "proportional", ShowSlider: "mouseover", Scale: 1, MaxColumn: 120}
	opts.Hover = HoverOptions{Enabled: "on", Delay: 300, Sticky: true, HidingDelay: 300, Above: true}
	if tweak != nil {
		tweak(&opts)
	}
	vm := NewViewModel(newTestDoc(diagnosticsText), opts, nil)
	t.Cleanup(vm.DisposeDiagnostics)
	ts := testutil.New(t, size)
	pumpEditorColors(t, ts, vm, EditorOpts{Autofocus: true}, colors)
	vm.SetDiagnostics(sampleDiagnostics())
	for range 4 {
		ts.Pump(time.Millisecond)
	}
	ts.FlushPosted()
	for range 3 {
		ts.Pump(time.Millisecond)
	}
	return vm, ts
}

func TestGoldenDiagnostics(t *testing.T) {
	for _, tc := range []struct{ golden, theme string }{
		{"diagnostics_dark", "Dark Modern"},
		{"diagnostics_light", "Light Modern"},
	} {
		t.Run(tc.theme, func(t *testing.T) {
			_, ts := diagnosticsRig(t, ui.Size{Width: 560, Height: 240}, bundledColors(t, tc.theme), nil)
			noErrors(t, ts)
			testutil.MatchesGolden(ts, tc.golden)
		})
	}
}

func TestDiagnosticsAreNotShownInReadOnlyEditors(t *testing.T) {
	for _, tc := range []struct {
		name     string
		readOnly bool
		want     int
	}{
		{"shown", false, 6},
		{"read-only editor", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := goOptions()
			opts.ReadOnly = tc.readOnly
			vm := NewViewModel(newTestDoc(diagnosticsText), opts, nil)
			vm.SetDiagnostics(sampleDiagnostics())
			if got := len(vm.diagnosticsIn(0, 9)); got != tc.want {
				t.Errorf("%d diagnostics are drawn, want %d", got, tc.want)
			}
		})
	}
}

func TestDiagnosticsMoveWithTheEditsAndSetReplacesThem(t *testing.T) {
	vm := NewViewModel(newTestDoc(diagnosticsText), goOptions(), nil)
	vm.SetDiagnostics(sampleDiagnostics())
	if got := vm.DiagnosticCount(); got != 6 {
		t.Fatalf("%d diagnostics", got)
	}
	// a line inserted above moves every squiggle down; text typed before one moves it right
	vm.Cursors().SetSelections([]buffer.Selection{cur(0, 0)})
	if err := vm.RunCommand("type", map[string]any{"text": "// hello\n"}); err != nil {
		t.Fatal(err)
	}
	vm.Cursors().SetSelections([]buffer.Selection{cur(4, 6)})
	if err := vm.RunCommand("type", map[string]any{"text": "ab"}); err != nil {
		t.Fatal(err)
	}
	var got Diagnostic
	for _, d := range vm.Diagnostics() {
		if d.Message == "undefined: undefined" {
			got = d
		}
	}
	want := buffer.Range{Start: positionOf(4, 8), End: positionOf(4, 17)}
	if got.Range != want {
		t.Errorf("the error is at %v, want %v", got.Range, want)
	}
	if got.Source != "compiler" || got.Code != "UndeclaredName" || got.Severity != DiagnosticError {
		t.Errorf("the error lost what it carries: %+v", got)
	}
	vm.SetDiagnostics(sampleDiagnostics()[:2])
	if n := vm.DiagnosticCount(); n != 2 || len(vm.Diagnostics()) != 2 {
		t.Errorf("after the second set: %d, %d", n, len(vm.Diagnostics()))
	}
	vm.DisposeDiagnostics()
	if n := len(vm.Diagnostics()); n != 0 {
		t.Errorf("%d diagnostics after dispose", n)
	}
}

func TestDiagnosticRangeFollowsVSCode(t *testing.T) {
	const text = "foo bar  \n\nbaz"
	for _, tc := range []struct {
		name string
		d    Diagnostic
		want buffer.Range
	}{
		{"a range is kept", Diagnostic{Severity: DiagnosticError, Range: buffer.Range{Start: positionOf(0, 4), End: positionOf(0, 7)}}, buffer.Range{Start: positionOf(0, 4), End: positionOf(0, 7)}},
		{"an empty range is the word", Diagnostic{Severity: DiagnosticError, Range: buffer.Range{Start: positionOf(0, 5), End: positionOf(0, 5)}}, buffer.Range{Start: positionOf(0, 4), End: positionOf(0, 7)}},
		{"an empty range at the end of the text stays", Diagnostic{Severity: DiagnosticError, Range: buffer.Range{Start: positionOf(0, 7), End: positionOf(0, 7)}}, buffer.Range{Start: positionOf(0, 7), End: positionOf(0, 7)}},
		{"an empty range on an empty line stays", Diagnostic{Severity: DiagnosticError, Range: buffer.Range{Start: positionOf(1, 0), End: positionOf(1, 0)}}, buffer.Range{Start: positionOf(1, 0), End: positionOf(1, 0)}},
		{"a hint is two characters", Diagnostic{Severity: DiagnosticHint, Range: buffer.Range{Start: positionOf(0, 0), End: positionOf(2, 3)}}, buffer.Range{Start: positionOf(0, 0), End: positionOf(0, 2)}},
		{"a hint with a tag keeps its range", Diagnostic{Severity: DiagnosticHint, Unnecessary: true, Range: buffer.Range{Start: positionOf(0, 0), End: positionOf(0, 3)}}, buffer.Range{Start: positionOf(0, 0), End: positionOf(0, 3)}},
		{"a range past the text is made valid", Diagnostic{Severity: DiagnosticError, Range: buffer.Range{Start: positionOf(2, 1), End: positionOf(9, 9)}}, buffer.Range{Start: positionOf(2, 1), End: positionOf(2, 3)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vm := NewViewModel(newTestDoc(text), goOptions(), nil)
			vm.SetDiagnostics([]Diagnostic{tc.d})
			ds := vm.Diagnostics()
			if len(ds) != 1 || ds[0].Range != tc.want {
				t.Errorf("got %+v, want %v", ds, tc.want)
			}
		})
	}
}

func TestDiagnosticsAtAPosition(t *testing.T) {
	vm := NewViewModel(newTestDoc(diagnosticsText), goOptions(), nil)
	vm.SetDiagnostics(append(sampleDiagnostics(), Diagnostic{
		Range: buffer.Range{Start: positionOf(3, 8), End: positionOf(3, 12)}, Severity: DiagnosticWarning, Message: "inner",
	}))
	for _, tc := range []struct {
		name string
		at   buffer.Position
		want []string
	}{
		{"before the range", positionOf(3, 5), nil},
		{"the first character", positionOf(3, 6), []string{"undefined: undefined"}},
		{"the last character", positionOf(3, 14), []string{"undefined: undefined"}},
		{"just past the end", positionOf(3, 15), nil},
		{"two of them, the more serious first", positionOf(3, 9), []string{"undefined: undefined", "inner"}},
		{"another line", positionOf(1, 0), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, d := range vm.DiagnosticsAt(tc.at) {
				got = append(got, d.Message)
			}
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// ---- the overview ruler ----

func TestRulerMarksMergeByPixelAndKeepTheMoreSerious(t *testing.T) {
	var b strings.Builder
	for range 400 {
		b.WriteString("line of text\n")
	}
	vm := NewViewModel(newTestDoc(b.String()), goOptions(), nil)
	ds := []Diagnostic{
		{Range: buffer.Range{Start: positionOf(0, 0), End: positionOf(0, 4)}, Severity: DiagnosticWarning, Message: "w"},
		{Range: buffer.Range{Start: positionOf(1, 0), End: positionOf(1, 4)}, Severity: DiagnosticError, Message: "e"},
		{Range: buffer.Range{Start: positionOf(200, 0), End: positionOf(200, 4)}, Severity: DiagnosticInfo, Message: "i"},
		{Range: buffer.Range{Start: positionOf(399, 0), End: positionOf(399, 4)}, Severity: DiagnosticHint, Message: "h"},
	}
	vm.SetDiagnostics(ds)
	vm.SetTextMetrics(500, 300, vm.Projection().Height()-300, 100, 8) // the content is as high as its rows
	marks := vm.diagnosticRulerMarks(200)
	if len(marks) < 2 {
		t.Fatalf("marks = %+v", marks)
	}
	if marks[0].severity != DiagnosticError || marks[0].y != 0 {
		t.Errorf("the first mark is %+v: the error wins where the lines share pixels", marks[0])
	}
	last := marks[len(marks)-1]
	if last.severity != DiagnosticInfo {
		t.Errorf("the last mark is %+v: a hint has none, the info does", last)
	}
	if last.y < 90 || last.y > 110 {
		t.Errorf("the info of line 200 of 400 is at %v of 200", last.y)
	}
	for _, m := range marks {
		if m.lane != laneRight {
			t.Errorf("mark %+v is not in the right lane", m)
		}
	}
	// the same set, the same height and the same text give the marks again without working them out
	again := vm.diagnosticRulerMarks(200)
	if &again[0] != &marks[0] {
		t.Error("the marks were worked out again")
	}
	vm.SetDiagnostics(ds[:1])
	if now := vm.diagnosticRulerMarks(200); len(now) != 1 || now[0].severity != DiagnosticWarning {
		t.Errorf("a new set gives %+v", now)
	}
}

// ---- performance ----

// thousandDiagnostics are n diagnostics spread over a text of n/2 lines.
func thousandDiagnostics(n int) (string, []Diagnostic) {
	var b strings.Builder
	for i := range n / 2 {
		fmt.Fprintf(&b, "line %d with a value and another value\n", i)
	}
	ds := make([]Diagnostic, n)
	for i := range ds {
		line := i / 2
		ds[i] = Diagnostic{
			Range:    buffer.Range{Start: positionOf(line, 12+(i%2)*10), End: positionOf(line, 17+(i%2)*10)},
			Severity: []DiagnosticSeverity{DiagnosticError, DiagnosticWarning, DiagnosticInfo, DiagnosticHint}[i%4],
			Message:  fmt.Sprintf("problem %d", i),
		}
	}
	return b.String(), ds
}

func TestManyDiagnosticsDoNotSlowTheFrameOrTheTyping(t *testing.T) {
	text, ds := thousandDiagnostics(6000)
	vm := NewViewModel(newTestDoc(text), goOptions(), nil)
	vm.SetDiagnostics(ds)
	vm.SetTextMetrics(500, 400, vm.Projection().Height()-400, 100, 8)
	// a frame looks at the diagnostics of the rows in view only: that is the number of rows times two, not 6000
	if got := len(vm.diagnosticsIn(1000, 1020)); got > 60 {
		t.Errorf("%d diagnostics looked up for 21 rows", got)
	}
	// 200 keys typed into a text with 6000 diagnostics, each followed by what a frame does of them
	vm.Cursors().SetSelections([]buffer.Selection{cur(1500, 0)})
	start := time.Now()
	for i := range 200 {
		if err := vm.RunCommand("type", map[string]any{"text": "x"}); err != nil {
			t.Fatal(err)
		}
		vm.diagnosticsIn(1495, 1515)
		vm.diagnosticRulerMarks(300 + float64(i%2)*0) // typing in a line leaves the marks as they are
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("200 keys with 6000 diagnostics took %v", took)
	}
	if n := len(vm.Diagnostics()); n != 6000 {
		t.Errorf("%d diagnostics after typing", n)
	}
}

func BenchmarkTypingWithDiagnostics(b *testing.B) {
	text, ds := thousandDiagnostics(6000)
	vm := NewViewModel(newTestDoc(text), goOptions(), nil)
	vm.SetDiagnostics(ds)
	vm.Cursors().SetSelections([]buffer.Selection{cur(1500, 0)})
	vm.diagnosticRulerMarks(300)
	b.ResetTimer()
	for range b.N {
		if err := vm.RunCommand("type", map[string]any{"text": "x"}); err != nil {
			b.Fatal(err)
		}
		vm.diagnosticsIn(1495, 1515)
		vm.diagnosticRulerMarks(300)
	}
}

func BenchmarkSetSixThousandDiagnostics(b *testing.B) {
	text, ds := thousandDiagnostics(6000)
	vm := NewViewModel(newTestDoc(text), goOptions(), nil)
	b.ResetTimer()
	for range b.N {
		vm.SetDiagnostics(ds)
	}
}
