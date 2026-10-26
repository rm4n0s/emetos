package editor

import (
	"context"
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
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/language"
	"github.com/rm4n0s/emetos/internal/core/language/languagetest"
	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/core/regex"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/core/tokens"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// peekFiles are the files the peek tests show: the editor's own file and another one.
func peekFiles() map[document.URI]string {
	return map[document.URI]string{
		thisFile:  "package main\n\nfunc Greet(name string) string {\n\treturn \"hello \" + name\n}\n\nfunc main() {\n\tfmt.Println(Greet(\"a\"))\n\tx := Greet(\"b\")\n\t_ = x\n}\n",
		otherFile: "package main\n\nimport \"testing\"\n\nfunc TestGreet(t *testing.T) {\n\tif Greet(\"x\") != \"hello x\" {\n\t\tt.Fatal(\"bad\")\n\t}\n\t_ = Greet(\"y\")\n}\n",
	}
}

// peekHost is a host whose editors of the peek view are view models over scripted files, and that records what it is asked to
// open.
func peekHost(t *testing.T, files map[document.URI]string, reg *langfeatures.Registry) (*SuggestHost, *[]opened, *int) {
	t.Helper()
	var opens []opened
	released := 0
	h := &SuggestHost{
		Ctx: t.Context(), Features: reg,
		Open: func(loc langfeatures.Location, o OpenLocation) { opens = append(opens, opened{loc, o}) },
		Embedded: func(_ context.Context, uri document.URI, done func(*ViewModel, func(), error)) {
			text, ok := files[uri]
			if !ok {
				done(nil, nil, errNoFile)
				return
			}
			doc := document.New(uri, text, document.NewOptions())
			doc.SetLanguageID("go")
			opts := goOptions()
			opts.CursorBlinking = "solid"
			opts.Minimap.Enabled, opts.StickyScroll.Enabled, opts.Folding = false, false, false
			vm := NewViewModel(doc, opts, nil)
			vm.SetEmbedded(true)
			tok := goTokenizer(t, doc)
			vm.SetTokenizer(tok)
			tok.Settle()
			done(vm, func() { released++; tok.Close() }, nil)
		},
		Texts: func(_ context.Context, uri document.URI) (lsp.Lines, error) {
			text, ok := files[uri]
			if !ok {
				return nil, errNoFile
			}
			return document.New(uri, text, document.NewOptions()).Snapshot(), nil
		},
	}
	return h, &opens, &released
}

// peekRig is an editor on screen with the peek view of the uses of Greet open.
func peekRig(t *testing.T, size ui.Size, colors kit.Colors) (*ViewModel, *gluttertest.Tester, *[]opened, *int) {
	t.Helper()
	return peekRigWith(t, size, colors, false)
}

// peekRigWith is peekRig with or without the minimap.
func peekRigWith(t *testing.T, size ui.Size, colors kit.Colors, minimap bool) (*ViewModel, *gluttertest.Tester, *[]opened, *int) {
	t.Helper()
	files := peekFiles()
	doc := document.New(thisFile, files[thisFile], document.NewOptions())
	doc.SetLanguageID("go")
	opts := goOptions()
	opts.CursorBlinking = "solid"
	opts.Minimap.Enabled = minimap
	vm := NewViewModel(doc, opts, nil)
	h, opens, released := peekHost(t, files, langfeatures.NewRegistry())
	vm.SetSuggest(h)
	ts := testutil.New(t, size)
	// the window's theme has the token theme of the color theme; the editor of the peek view inherits it
	tokenTheme := darkModernTokens(t)
	if colors.EditorBackground != kit.DarkModern().EditorBackground {
		th, err := theme.LoadBundled("Light Modern")
		if err != nil {
			t.Fatal(err)
		}
		tokenTheme = th.TokenTheme(nil)
	}
	pumpEditorTokens(t, ts, vm, colors, tokenTheme)
	cursorAt(vm, 2, 7)
	vm.OpenPeek(GoReferences, []NavTarget{
		{URI: otherFile, Range: rg(5, 4, 5, 9)}, {URI: otherFile, Range: rg(8, 5, 8, 10)},
		{URI: thisFile, Range: rg(2, 5, 2, 10)}, {URI: thisFile, Range: rg(7, 13, 7, 18)}, {URI: thisFile, Range: rg(8, 6, 8, 11)},
	})
	t.Cleanup(vm.PeekClose)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ts.FlushPosted()
		ts.Pump(time.Millisecond)
		v := vm.Peek()
		if v != nil && v.Current != nil && peekPreviewsLoaded(v) {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	for range 4 {
		ts.Pump(time.Millisecond)
		ts.FlushPosted()
	}
	return vm, ts, opens, released
}

// pumpEditorTokens shows the editor in a theme with a token theme and the font of the tests for the workbench's text.
func pumpEditorTokens(t testing.TB, ts *gluttertest.Tester, vm *ViewModel, colors kit.Colors, tokenTheme *theme.TokenTheme) {
	t.Helper()
	ts.PumpWidget(kit.NewTheme(colors, widgets.NewWidgetsApp(colors.EditorBackground, widgets.WidgetsAppOpts{
		DebugShowCheckedModeBanner: foundation.Some(false),
		OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(
				func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
					return NewEditor(vm, EditorOpts{Autofocus: true})
				},
				widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
		},
	}), kit.ThemeOpts{Tokens: tokenTheme, UIFont: testutil.MonoFamily}))
}

// goTokenizer is the tokenizer of a Go document, running as the window's does.
func goTokenizer(t testing.TB, doc *document.Document) *tokens.Tokenizer {
	t.Helper()
	reg, err := languagetest.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	g, err := reg.NewGrammarWith("go", language.GrammarSettings{Cache: regex.NewCache(1 << 14), ScanTimeout: testutil.ScanTimeout})
	if err != nil {
		t.Fatal(err)
	}
	return tokens.New(doc, g, tokens.Options{})
}

func peekPreviewsLoaded(v *PeekView) bool {
	for _, r := range v.Refs {
		if !r.Loaded {
			return false
		}
	}
	return true
}

var errNoFile = &testError{"no such file"}

type testError struct{ s string }

func (e *testError) Error() string { return e.s }

func TestPeekViewShowsTheLocationsAndAnEditor(t *testing.T) {
	vm, ts, opens, released := peekRig(t, ui.Size{Width: 960, Height: 560}, kit.DarkModern())
	noErrors(t, ts)
	v := vm.Peek()
	if v == nil || len(v.Refs) != 5 || len(v.Files) != 2 {
		t.Fatalf("view = %+v", v)
	}
	if v.Current == nil || !v.Current.IsEmbedded() {
		t.Fatal("no embedded editor")
	}
	// the rows have the text of their lines
	if r := v.Refs[0]; r.Before != "if " || r.Match != "Greet" || !strings.HasPrefix(r.After, "(") {
		t.Errorf("first row = %+v", r)
	}
	// the references of the file are marked in its editor, and the cursor is at the selected one
	if got := occurrences(v.Current, peekMatchOwner); len(got) != 3 {
		t.Errorf("marked = %v", got)
	}
	// the selected one is the nearest to the cursor: in the same file
	if v.Refs[v.Selected].URI != thisFile {
		t.Errorf("selected %d is in %v", v.Selected, v.Refs[v.Selected].URI)
	}
	// moving to another file replaces the editor and lets go of the first
	before := v.Current
	vm.PeekSelect(0)
	for range 3 {
		ts.FlushPosted()
		ts.Pump(time.Millisecond)
	}
	if now := vm.Peek().Current; now == before || vm.Peek().CurrentURI != otherFile {
		t.Errorf("the editor did not change: %v", vm.Peek().CurrentURI)
	}
	if *released != 1 {
		t.Errorf("released = %d, want the editor of the first file", *released)
	}
	vm.PeekClose()
	if *released != 2 || len(*opens) != 0 {
		t.Errorf("released = %d, opens = %v", *released, *opens)
	}
}

func TestPeekViewReportsAFileItCannotShow(t *testing.T) {
	files := peekFiles()
	delete(files, otherFile)
	doc := document.New(thisFile, files[thisFile], document.NewOptions())
	vm := NewViewModel(doc, goOptions(), nil)
	// the previews are read on a goroutine and handed to the UI thread: this test has none, so what they hand over is dropped
	vm.SetPoster(func(func()) {})
	h, _, _ := peekHost(t, files, langfeatures.NewRegistry())
	vm.SetSuggest(h)
	vm.OpenPeek(GoReferences, []NavTarget{{URI: otherFile, Range: rg(0, 0, 0, 1)}, {URI: thisFile, Range: rg(0, 0, 0, 1)}})
	t.Cleanup(vm.PeekClose)
	vm.PeekSelect(0)
	if v := vm.Peek(); v.Failed == "" || v.Current != nil {
		t.Errorf("view = %+v", v)
	}
}

func TestGoldenPeekView(t *testing.T) {
	for _, tc := range []struct{ golden, theme string }{
		{"peek_view_dark", "Dark Modern"},
		{"peek_view_light", "Light Modern"},
	} {
		t.Run(tc.golden, func(t *testing.T) {
			vm, ts, _, _ := peekRig(t, ui.Size{Width: 960, Height: 560}, bundledColors(t, tc.theme))
			noErrors(t, ts)
			_ = vm
			testutil.MatchesGolden(ts, tc.golden)
		})
	}
}

func TestPeekZoneClosesOnPressOutside(t *testing.T) {
	vm, ts, _, _ := peekRig(t, ui.Size{Width: 960, Height: 560}, kit.DarkModern())
	if !vm.PeekVisible() {
		t.Fatal("no peek")
	}
	// the press in the text above the zone closes it
	ts.TapAt(ui.Offset{Dx: 300, Dy: 10}, mouseOpts())
	ts.FlushPosted()
	if vm.PeekVisible() {
		t.Error("the peek view stays after a press outside it")
	}
	_ = buffer.Position{}
}

func TestFillPreviewKeepsTheMatchVisible(t *testing.T) {
	doc := document.New(thisFile, "\t\tvery_long_prefix_of_a_line_that_goes_on := Greet(1)\n", document.NewOptions())
	r := PeekRef{URI: thisFile, Range: rg(0, 45, 0, 50)}
	fillPreview(&r, doc.Snapshot())
	if !r.Loaded || r.Match != "Greet" || r.After != "(1)" || !strings.HasPrefix(r.Before, "…") || len([]rune(r.Before)) != peekBeforeRunes+1 {
		t.Errorf("ref = %+v", r)
	}
	short := PeekRef{URI: thisFile, Range: rg(0, 0, 0, 0)}
	fillPreview(&short, document.New(thisFile, "\tx := 1\n", document.NewOptions()).Snapshot())
	if short.Before != "" {
		t.Errorf("before = %q", short.Before)
	}
}
