package editor

import (
	"fmt"
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
	return o
}

// pumpEditor shows the editor of a view model in an app of the given size.
func pumpEditor(t *testing.T, vm *ViewModel, size ui.Size) *gluttertest.Tester {
	t.Helper()
	ts := testutil.New(t, size)
	colors := kit.DarkModern()
	ts.PumpWidget(kit.NewTheme(colors, widgets.NewWidgetsApp(colors.EditorBackground, widgets.WidgetsAppOpts{
		DebugShowCheckedModeBanner: foundation.Some(false),
		OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(
				func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
					return NewEditor(vm)
				},
				widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
		},
	})))
	return ts
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
	vm.RestoreViewState(ViewState{ScrollTop: 950})
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
	// Ten rows of text and no numbers and no digit measures.
	if got := vm.Paragraphs().Builds(); got > 12 {
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
