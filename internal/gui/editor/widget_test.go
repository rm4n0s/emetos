package editor

import (
	"fmt"
	"image/color"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// goOptions are the options of the tests: Go Mono at 14 pixels, as the settings give them.
func goOptions() Options {
	o := testOptions()
	o.FontFamilies = []string{testutil.MonoFamily, "monospace"}
	o.FontFeatures = []FontFeature{{"liga", 0}, {"calt", 0}}
	o.FindSeedSearchStringFromSelection, o.FindAutoInSelection = "always", "never"
	o.FindCursorMoveOnType, o.FindLoop = true, true
	return o
}

// pumpEditor shows the editor of a view model in an app of the given size.
func pumpEditor(t *testing.T, vm *ViewModel, size ui.Size) *gluttertest.Tester {
	t.Helper()
	ts := testutil.New(t, size)
	pumpEditorWith(t, ts, vm, EditorOpts{Autofocus: true})
	return ts
}

// testutilNew makes a tester of the size the editor tests use.
func testutilNew(t *testing.T) *gluttertest.Tester {
	t.Helper()
	return testutil.New(t, ui.Size{Width: 640, Height: 400})
}

// pumpEditorWith shows the editor of a view model, with options, in a tester.
func pumpEditorWith(t testing.TB, ts *gluttertest.Tester, vm *ViewModel, opts EditorOpts) {
	t.Helper()
	pumpEditorColors(t, ts, vm, opts, kit.DarkModern())
}

// pumpEditorColors is pumpEditorWith in a theme with the given colors.
func pumpEditorColors(t testing.TB, ts *gluttertest.Tester, vm *ViewModel, opts EditorOpts, colors kit.Colors) {
	t.Helper()
	ts.PumpWidget(kit.NewTheme(colors, widgets.NewWidgetsApp(colors.EditorBackground, widgets.WidgetsAppOpts{
		DebugShowCheckedModeBanner: foundation.Some(false),
		OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(
				func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
					return NewEditor(vm, opts)
				},
				widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
		},
	}), kit.ThemeOpts{UIFont: testutil.MonoFamily}))
}

func linesDoc(n int) *document.Document {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "line %d of the document\n", i+1)
	}
	return newTestDoc(b.String())
}

func noErrors(t *testing.T, ts *gluttertest.Tester) {
	t.Helper()
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("the framework reported errors: %v", errs)
	}
}

func TestOnlyVisibleRowsAreBuilt(t *testing.T) {
	vm := NewViewModel(linesDoc(5000), goOptions(), nil)
	ts := pumpEditor(t, vm, ui.Size{Width: 800, Height: 400})
	noErrors(t, ts)

	// 400 / 19 = 21 rows in view and 10 more on each side for the text, 22 numbers for the
	// gutter, and the ten digits that give the width of the gutter.
	cache := vm.Paragraphs()
	if cache.Builds() < 21 {
		t.Errorf("the visible rows were not built: %d paragraphs", cache.Builds())
	}
	if cache.Builds() > 100 || cache.Len() != cache.Builds() {
		t.Errorf("%d paragraphs built for 5000 lines, %d in the cache", cache.Builds(), cache.Len())
	}
}

func TestScrollingBuildsNewRowsOnly(t *testing.T) {
	vm := NewViewModel(linesDoc(5000), goOptions(), nil)
	ts := pumpEditor(t, vm, ui.Size{Width: 800, Height: 400})
	editorFinder := gluttertest.ByType[*Editor]()
	before := vm.Paragraphs().Builds()

	ts.Scroll(editorFinder, ui.Offset{Dy: 3000})
	ts.Pump(100 * time.Millisecond)
	if vm.ScrollTop() <= 0 {
		t.Fatalf("the editor did not scroll: %v", vm.ScrollTop())
	}
	afterDown := vm.Paragraphs().Builds()
	if afterDown <= before {
		t.Errorf("rows that came into view were not built: %d then %d", before, afterDown)
	}

	ts.Scroll(editorFinder, ui.Offset{Dy: -100000})
	ts.Pump(100 * time.Millisecond)
	if vm.ScrollTop() != 0 {
		t.Errorf("scrolled back to the top: %v", vm.ScrollTop())
	}
	if got := vm.Paragraphs().Builds(); got != afterDown {
		t.Errorf("rows already built were built again: %d then %d", afterDown, got)
	}
	noErrors(t, ts)
}

func TestScrollExtentOfTheEditor(t *testing.T) {
	cases := []struct {
		name   string
		beyond bool
		want   float64
	}{
		{"beyond the last line", true, 100*19 - 19},
		{"not beyond", false, 100*19 - 300},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := goOptions()
			opts.ScrollBeyondLastLine = tc.beyond
			vm := NewViewModel(linesDoc(99), opts, nil) // 99 lines and the empty one after the last newline
			ts := pumpEditor(t, vm, ui.Size{Width: 800, Height: 300})
			ts.Scroll(gluttertest.ByType[*Editor](), ui.Offset{Dy: 100000})
			ts.Pump(100 * time.Millisecond)
			if got := vm.ScrollTop(); got != tc.want {
				t.Errorf("the scroll stops at %v, want %v", got, tc.want)
			}
			noErrors(t, ts)
		})
	}
}

func TestOneEditedLineBuildsOneParagraph(t *testing.T) {
	doc := linesDoc(200)
	vm := NewViewModel(doc, goOptions(), nil)
	ts := pumpEditor(t, vm, ui.Size{Width: 800, Height: 400})
	before := vm.Paragraphs().Builds()
	hits := vm.Paragraphs().Hits()

	_, err := doc.Edit([]document.Edit{{Range: buffer.Range{
		Start: buffer.Position{Line: 5, Col: 0}, End: buffer.Position{Line: 5, Col: 4},
	}, Text: "LINE"}}, document.EditOptions{})
	if err != nil {
		t.Fatal(err)
	}
	vm.DocumentChanged()
	ts.Pump(50 * time.Millisecond)

	if got := vm.Paragraphs().Builds() - before; got != 1 {
		t.Errorf("an edit of one line built %d paragraphs, want 1", got)
	}
	if vm.Paragraphs().Hits() <= hits {
		t.Error("the other rows were not found in the cache")
	}
	noErrors(t, ts)
}

func TestScrollPositionIsRestored(t *testing.T) {
	vm := NewViewModel(linesDoc(1000), goOptions(), nil)
	if err := vm.RestoreViewState(ViewState{ScrollTop: 950}); err != nil {
		t.Fatal(err)
	}
	ts := pumpEditor(t, vm, ui.Size{Width: 800, Height: 400})
	ts.Pump(50 * time.Millisecond)
	if got := vm.ScrollTop(); got != 950 {
		t.Errorf("the editor starts at the remembered position: %v, want 950", got)
	}
	// Rows 50 to 70 are in view: line 51 is built, line 1 is not.
	snap := vm.Document().Snapshot()
	first, _ := snap.LineContent(50)
	if first != "line 51 of the document" {
		t.Fatalf("line 51: %q", first)
	}
	if vm.Paragraphs().Builds() > 100 {
		t.Errorf("only the rows in view were built: %d", vm.Paragraphs().Builds())
	}
	noErrors(t, ts)
}

func TestScrollLeftRepaints(t *testing.T) {
	vm := NewViewModel(newTestDoc(strings.Repeat("x", 300)), goOptions(), nil)
	ts := pumpEditor(t, vm, ui.Size{Width: 400, Height: 100})
	vm.SetScrollLeft(200)
	ts.Pump(50 * time.Millisecond)
	if vm.ScrollLeft() != 200 {
		t.Errorf("scroll left: %v", vm.ScrollLeft())
	}
	noErrors(t, ts)
}

func TestLineNumbersOff(t *testing.T) {
	opts := goOptions()
	opts.LineNumbers = LineNumbersOff
	vm := NewViewModel(linesDoc(10), opts, nil)
	pumpEditor(t, vm, ui.Size{Width: 400, Height: 200})
	// Eleven rows of text, the width of a character for the layout and one for the cursors, and
	// none of the ten digits that the line numbers are measured with.
	if got := vm.Paragraphs().Builds(); got > 14 {
		t.Errorf("%d paragraphs without line numbers", got)
	}
}

func TestOptionsChangeRebuildsTheLook(t *testing.T) {
	vm := NewViewModel(linesDoc(30), goOptions(), nil)
	ts := pumpEditor(t, vm, ui.Size{Width: 600, Height: 300})
	before := vm.Paragraphs().Builds()
	opts := goOptions()
	opts.FontSize = 20
	opts.LineHeight = LineHeight(0, 20)
	vm.SetOptions(opts)
	ts.Pump(50 * time.Millisecond)
	if vm.Paragraphs().Builds() <= before {
		t.Error("a new font size must build new paragraphs")
	}
	noErrors(t, ts)
}

// goldenSample reads the Go file of the proofs.
func goldenSample(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(testutil.RepoRoot(t), "testdata", "proof", "3.1", "sample.go"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestGoldenPlainGoText(t *testing.T) {
	vm := NewViewModel(newTestDoc(goldenSample(t)), goOptions(), nil)
	ts := pumpEditor(t, vm, ui.Size{Width: 640, Height: 440})
	noErrors(t, ts)
	ts.MatchesGolden("plain_go_text")
	testutil.Proof(t, ts, "3.2-text-layer.png")
}

func TestGoldenTabs(t *testing.T) {
	text := "package tabs\n\n" +
		"func f() {\n" +
		"\tone := 1\n" +
		"\t\ttwo := 2\n" +
		"a\tb\tc\n" +
		"ab\tab\tab\n" +
		"abc\tabc\tabc\n" +
		"abcd\tabcd\tabcd\n" +
		"\t// tab then text\n" +
		"}\n"
	for _, tabSize := range []int{4, 8} {
		t.Run(fmt.Sprintf("tab size %d", tabSize), func(t *testing.T) {
			opts := goOptions()
			opts.TabSize = tabSize
			vm := NewViewModel(newTestDoc(text), opts, nil)
			ts := pumpEditor(t, vm, ui.Size{Width: 560, Height: 260})
			noErrors(t, ts)
			ts.MatchesGolden(fmt.Sprintf("tabs_%d", tabSize))
		})
	}
}

func TestGoldenRelativeLineNumbers(t *testing.T) {
	opts := goOptions()
	opts.LineNumbers = LineNumbersRelative
	vm := NewViewModel(newTestDoc(goldenSample(t)), opts, nil)
	vm.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: 9}, Active: buffer.Position{Line: 9}}})
	ts := pumpEditor(t, vm, ui.Size{Width: 640, Height: 440})
	noErrors(t, ts)
	ts.MatchesGolden("relative_line_numbers")
}

func TestGoldenIntervalLineNumbers(t *testing.T) {
	opts := goOptions()
	opts.LineNumbers = LineNumbersInterval
	vm := NewViewModel(linesDoc(34), opts, nil)
	vm.Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: 3}, Active: buffer.Position{Line: 3}}})
	ts := pumpEditor(t, vm, ui.Size{Width: 400, Height: 440})
	noErrors(t, ts)
	ts.MatchesGolden("interval_line_numbers")
}

func selectionOf(l1, c1, l2, c2 int) buffer.Selection {
	return buffer.Selection{Anchor: positionOf(l1, c1), Active: positionOf(l2, c2)}
}

// selectedEditor shows the sample Go file with the selections set.
func selectedEditor(t *testing.T, opts Options, sels ...buffer.Selection) (*ViewModel, *gluttertest.Tester) {
	t.Helper()
	vm := NewViewModel(newTestDoc(goldenSample(t)), opts, nil)
	vm.Cursors().SetSelections(sels)
	vm.SelectionsChanged()
	ts := pumpEditor(t, vm, ui.Size{Width: 640, Height: 440})
	noErrors(t, ts)
	return vm, ts
}

func TestGeometryMeasuresColumns(t *testing.T) {
	doc := newTestDoc("hello\n\ta\tb\nab  \nné!\n\n")
	vm := NewViewModel(doc, goOptions(), nil)
	ts := pumpEditor(t, vm, ui.Size{Width: 400, Height: 200})
	g := newGeometry(vm, ts.Dispatcher().TextEngine())
	cw := g.charWidth
	if cw < 5 || cw > 12 {
		t.Fatalf("the width of a character of Go Mono at 14 pixels: %v", cw)
	}
	cases := []struct {
		name     string
		row, col int
		want     float64
	}{
		{"start", 0, 0, 0},
		{"after four characters", 0, 4, 4 * cw},
		{"end of the line", 0, 5, 5 * cw},
		{"past the end", 0, 50, 5 * cw},
		{"after a tab", 1, 1, 4 * cw},
		{"after a character and a tab", 1, 3, 8 * cw},
		{"trailing spaces count", 2, 4, 4 * cw},
		{"after a two byte character", 3, 3, 2 * cw},
		{"empty row", 4, 0, 0},
		{"empty row, past the start", 4, 3, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := g.x(tc.row, tc.col); math.Abs(got-tc.want) > 0.01 {
				t.Errorf("x(%d, %d) = %v, want %v", tc.row, tc.col, got, tc.want)
			}
		})
	}
	if got := g.rowEnd(0); math.Abs(got-5*cw) > 0.01 {
		t.Errorf("end of row 0: %v", got)
	}
	noErrors(t, ts)
}

func TestSelectionRowsOfAGeometry(t *testing.T) {
	doc := newTestDoc("abcdef\n\nxyz\nlast")
	vm := NewViewModel(doc, goOptions(), nil)
	ts := pumpEditor(t, vm, ui.Size{Width: 400, Height: 200})
	g := newGeometry(vm, ts.Dispatcher().TextEngine())
	cw := g.charWidth
	rows := g.selectionRows(selectionOf(0, 2, 2, 1), 0, 3, true)
	if len(rows) != 3 {
		t.Fatalf("%d rows: %+v", len(rows), rows)
	}
	want := []struct{ left, right float64 }{{2 * cw, 7 * cw}, {0, cw}, {0, cw}}
	for i, w := range want {
		if rows[i].Row != i || math.Abs(rows[i].Left-w.left) > 0.01 || math.Abs(rows[i].Right-w.right) > 0.01 {
			t.Errorf("row %d: %+v, want %v..%v (the line break is one character wide)", i, rows[i], w.left, w.right)
		}
	}
	rows = g.selectionRows(selectionOf(0, 5, 2, 3), 0, 3, true)
	if len(rows) != 3 || rows[1].End.Bottom != cornerIntern || rows[0].End.Bottom != cornerExtern {
		t.Errorf("a row that ends before the one below turns a corner: %+v", rows)
	}
	if rows := g.selectionRows(selectionOf(0, 2, 2, 0), 0, 3, true); len(rows) != 2 {
		t.Errorf("a selection that ends at the start of a line leaves that row out: %+v", rows)
	}
	if rows := g.selectionRows(selectionOf(3, 1, 3, 3), 0, 3, false); len(rows) != 1 || rows[0].Start != (endStyle{}) {
		t.Errorf("without rounded corners nothing is styled: %+v", rows)
	}
	// Only the rows near the visible ones are measured.
	if rows := g.selectionRows(selectionOf(0, 0, 3, 2), 2, 2, true); len(rows) != 3 || rows[0].Row != 1 || rows[2].Row != 3 {
		t.Errorf("rows 1 to 3 around the visible row 2: %+v", rows)
	}
	noErrors(t, ts)
}

// pixel is the color of a pixel of the screenshot.
func pixel(ts *gluttertest.Tester, x, y int) color.NRGBA {
	return color.NRGBAModel.Convert(ts.Screenshot().At(x, y)).(color.NRGBA)
}

func TestCursorBlinksInTheWindow(t *testing.T) {
	vm := NewViewModel(linesDoc(5), goOptions(), nil)
	ts := pumpEditor(t, vm, ui.Size{Width: 400, Height: 200})
	gutter := int(vm.GutterWidths(ts.Dispatcher().TextEngine()).Total())
	cursor := func() color.NRGBA { return pixel(ts, gutter, 9) }
	shown := color.NRGBA{R: 0xAE, G: 0xAF, B: 0xAD, A: 0xFF}
	// Where the cursor is not drawn there is the left border of the current line.
	hidden := color.NRGBA{R: 0x28, G: 0x28, B: 0x28, A: 0xFF}

	if got := cursor(); got != shown {
		t.Fatalf("the cursor is shown at first: %v", got)
	}
	steps := []struct {
		pump time.Duration
		want color.NRGBA
	}{
		{BlinkInterval - 10*time.Millisecond, shown},
		{10 * time.Millisecond, hidden},
		{BlinkInterval, shown},
		{BlinkInterval, hidden},
	}
	for i, s := range steps {
		ts.Pump(s.pump)
		if got := cursor(); got != s.want {
			t.Errorf("step %d: %v, want %v", i, got, s.want)
		}
	}
	// A move shows the cursor at once.
	vm.Cursors().SetSelections([]buffer.Selection{selectionOf(2, 0, 2, 0)})
	vm.SelectionsChanged()
	ts.Pump(time.Millisecond)
	if got := pixel(ts, gutter, 2*19+9); got != shown {
		t.Errorf("the cursor is shown on its new line: %v", got)
	}
	noErrors(t, ts)
}

func TestNoBlinkingWithoutFocus(t *testing.T) {
	vm := NewViewModel(linesDoc(5), goOptions(), nil)
	ts := pumpEditor(t, vm, ui.Size{Width: 400, Height: 200})
	gutter := int(vm.GutterWidths(ts.Dispatcher().TextEngine()).Total())
	shown := color.NRGBA{R: 0xAE, G: 0xAF, B: 0xAD, A: 0xFF}
	hidden := color.NRGBA{R: 0x28, G: 0x28, B: 0x28, A: 0xFF} // the border of the current line

	ts.SetViewFocus(ui.ImplicitViewID, false)
	ts.Pump(time.Millisecond)
	if vm.Focused() || vm.CursorsVisible() {
		t.Fatal("the window lost the focus, so the editor has no cursor")
	}
	// Nothing changes however long the window stays unfocused: no timer toggles the cursor.
	for range 4 {
		ts.Pump(BlinkInterval)
		if got := pixel(ts, gutter, 9); got != hidden {
			t.Fatalf("a cursor is drawn without focus: %v", got)
		}
	}
	ts.SetViewFocus(ui.ImplicitViewID, true)
	ts.Pump(time.Millisecond)
	if got := pixel(ts, gutter, 9); got != shown {
		t.Errorf("the cursor is back with the focus: %v", got)
	}
	ts.Pump(BlinkInterval)
	if got := pixel(ts, gutter, 9); got != hidden {
		t.Errorf("and blinks again: %v", got)
	}
	noErrors(t, ts)
}

func TestBlinkingStopsWhenTheEditorIsGone(t *testing.T) {
	vm := NewViewModel(linesDoc(5), goOptions(), nil)
	ts := pumpEditor(t, vm, ui.Size{Width: 400, Height: 200})
	ts.PumpWidget(widgets.NewSizedBox(nil))
	ts.Pump(time.Millisecond)
	if vm.blink.Active() {
		t.Error("a timer is left running after the editor was removed")
	}
	toggles := 0
	vm.AddBlinkListener(func() { toggles++ })
	ts.Pump(3 * BlinkInterval)
	if toggles != 0 {
		t.Errorf("the cursor blinked %d times without an editor", toggles)
	}
	noErrors(t, ts)
}

func TestGoldenMultiLineSelection(t *testing.T) {
	_, ts := selectedEditor(t, goOptions(), selectionOf(12, 7, 17, 5))
	ts.MatchesGolden("selection_multiline")
}

func TestGoldenSelectionStaircase(t *testing.T) {
	// A selection that starts to the right of where it ends: the corners turn both ways.
	_, ts := selectedEditor(t, goOptions(), selectionOf(13, 10, 17, 4))
	ts.MatchesGolden("selection_staircase")
}

func TestGoldenSelectionNotRounded(t *testing.T) {
	opts := goOptions()
	opts.RoundedSelection = false
	_, ts := selectedEditor(t, opts, selectionOf(12, 7, 17, 5))
	ts.MatchesGolden("selection_square")
}

func TestGoldenInactiveSelection(t *testing.T) {
	vm, ts := selectedEditor(t, goOptions(), selectionOf(12, 7, 17, 5))
	ts.SetViewFocus(ui.ImplicitViewID, false)
	ts.Pump(time.Millisecond)
	if vm.Focused() {
		t.Fatal("the editor lost the focus")
	}
	ts.MatchesGolden("selection_inactive")
}

func TestGoldenThreeCursors(t *testing.T) {
	_, ts := selectedEditor(t, goOptions(), selectionOf(13, 4, 13, 4), selectionOf(15, 8, 15, 8), selectionOf(18, 6, 18, 6))
	ts.MatchesGolden("three_cursors")
}

func TestGoldenSeveralSelections(t *testing.T) {
	_, ts := selectedEditor(t, goOptions(), selectionOf(3, 0, 6, 8), selectionOf(12, 4, 12, 8), selectionOf(18, 4, 19, 9))
	ts.MatchesGolden("selection_several")
}

func TestGoldenCurrentLine(t *testing.T) {
	_, ts := selectedEditor(t, goOptions(), selectionOf(13, 9, 13, 9))
	ts.MatchesGolden("current_line")
}

func TestGoldenCurrentLineInGutterAndText(t *testing.T) {
	opts := goOptions()
	opts.RenderLineHighlight = "all"
	_, ts := selectedEditor(t, opts, selectionOf(13, 9, 13, 9))
	ts.MatchesGolden("current_line_all")
}

func TestGoldenCurrentLineInGutter(t *testing.T) {
	opts := goOptions()
	opts.RenderLineHighlight = "gutter"
	_, ts := selectedEditor(t, opts, selectionOf(13, 3, 15, 3))
	ts.MatchesGolden("current_line_gutter")
}

func TestCurrentLineHighlightOffAndOnlyWhenFocused(t *testing.T) {
	cases := []struct {
		name        string
		setting     string
		onlyFocused bool
		unfocus     bool
		wantBorder  bool
	}{
		{"line", "line", false, false, true},
		{"none", "none", false, false, false},
		{"line without focus", "line", false, true, true},
		{"only when focused, without focus", "line", true, true, false},
		{"only when focused, with focus", "line", true, false, true},
	}
	border := color.NRGBA{R: 0x28, G: 0x28, B: 0x28, A: 0xFF}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			opts := goOptions()
			opts.RenderLineHighlight, opts.RenderLineHighlightOnlyWhenFocus = tc.setting, tc.onlyFocused
			vm := NewViewModel(linesDoc(5), opts, nil)
			ts := pumpEditor(t, vm, ui.Size{Width: 400, Height: 200})
			if tc.unfocus {
				ts.SetViewFocus(ui.ImplicitViewID, false)
				ts.Pump(time.Millisecond)
			}
			// the first row's top border, to the right of the cursor
			got := pixel(ts, 300, 0) == border
			if got != tc.wantBorder {
				t.Errorf("border drawn: %v, want %v", got, tc.wantBorder)
			}
			noErrors(t, ts)
		})
	}
}

// TestProofCursors makes the proof image of step 3.3 with -proof: a multi-line selection and
// two more cursors, with the current line highlighted in the gutter (the text has no highlight
// while a selection has text, as in VS Code).
func TestProofCursors(t *testing.T) {
	if !testutil.ProofEnabled() {
		t.Skip("run with -proof to make the proof image")
	}
	opts := goOptions()
	opts.RenderLineHighlight = "all"
	_, ts := selectedEditor(t, opts, selectionOf(12, 7, 17, 5), selectionOf(3, 8, 3, 8), selectionOf(19, 12, 19, 12))
	testutil.Proof(t, ts, "3.3-cursors.png")
}
