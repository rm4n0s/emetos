package editor

import (
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/language"
	"github.com/rm4n0s/emetos/internal/core/regex"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/core/tokens"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// highlighted makes the view model of a Go document with its tokens.
func highlighted(t testing.TB, text string) (*ViewModel, *tokens.Tokenizer) {
	t.Helper()
	reg, err := language.Bundled()
	if err != nil {
		t.Fatal(err)
	}
	g, err := reg.NewGrammar("go", regex.NewCache(1<<14))
	if err != nil {
		t.Fatal(err)
	}
	opts := document.NewOptions()
	opts.LanguageID = "go"
	doc := document.New(document.FileURI("/sample.go"), text, opts)
	tok := tokens.New(doc, g, tokens.Options{})
	t.Cleanup(tok.Close)
	vm := NewViewModel(doc, goOptions(), nil)
	vm.SetTokenizer(tok)
	return vm, tok
}

// darkModernTokens is the token theme of Dark Modern.
func darkModernTokens(t testing.TB) *theme.TokenTheme {
	t.Helper()
	th, err := theme.LoadBundled("Dark Modern")
	if err != nil {
		t.Fatal(err)
	}
	return th.TokenTheme(nil)
}

// pumpHighlighted shows the editor in a theme with its token theme, as the app does.
func pumpHighlighted(t testing.TB, ts *gluttertest.Tester, vm *ViewModel, colors kit.Colors, tokenTheme *theme.TokenTheme) {
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
	}), kit.ThemeOpts{Tokens: tokenTheme}))
}

func hexColor(t testing.TB, s string) ui.Color {
	t.Helper()
	c, ok := theme.ParseColor(s)
	if !ok {
		t.Fatalf("%q is not a color", s)
	}
	return ui.NewColor(c.ARGB())
}

func TestSpansColorTheTokensOfARow(t *testing.T) {
	_, tok := highlighted(t, "package main\n\nfunc main() { /* x */ }\n")
	tok.Settle()
	table := newStyleTable(darkModernTokens(t), tok.Scopes())
	keyword, name := hexColor(t, "#569CD6"), hexColor(t, "#4EC9B0")

	spans := table.spans(tok.Line(0), 0, 12)
	if len(spans) < 2 || spans[0].start != 0 || spans[len(spans)-1].end != 12 {
		t.Fatalf("the spans cover the row: %+v", spans)
	}
	for i := 1; i < len(spans); i++ {
		if spans[i].start != spans[i-1].end {
			t.Errorf("spans are one after the other: %+v", spans)
		}
		if spans[i].color == spans[i-1].color && spans[i].style == spans[i-1].style {
			t.Errorf("neighbours with one look are one span: %+v", spans)
		}
	}
	if spans[0].color != keyword || spans[0].end != 7 {
		t.Errorf("package is a keyword: %+v", spans[0])
	}
	if last := spans[len(spans)-1]; last.color != name {
		t.Errorf("main is a type name: %+v", last)
	}

	// A part of the line (a row of a wrapped line) has spans relative to its start.
	part := table.spans(tok.Line(0), 8, 12)
	if len(part) != 1 || part[0].start != 0 || part[0].end != 4 || part[0].color != name {
		t.Errorf("the row main: %+v", part)
	}
	if got := table.spans(tok.Line(0), 12, 12); len(got) != 0 {
		t.Errorf("an empty row has no spans: %+v", got)
	}
	if table.spans(nil, 0, 5) != nil {
		t.Error("no tokens, no spans")
	}
	// The comment is in the color of comments, and italic or not as the theme says.
	comment := table.spans(tok.Line(2), 0, len("func main() { /* x */ }"))
	if got := comment[len(comment)-2]; got.color != hexColor(t, "#6A9955") {
		t.Errorf("a comment: %+v", comment)
	}
	// Resolving is remembered.
	if len(table.cache) == 0 {
		t.Error("the styles are cached")
	}
}

func TestSpansKeys(t *testing.T) {
	a := []span{{0, 3, ui.NewColor(0xFF112233), theme.FontStyleNone}, {3, 5, ui.NewColor(0xFF112233), theme.FontStyleBold}}
	b := []span{{0, 3, ui.NewColor(0xFF112233), theme.FontStyleNone}, {3, 5, ui.NewColor(0xFF112233), theme.FontStyleItalic}}
	again := spansKey(a)
	if spansKey(nil) != "" || spansKey(a) == "" || spansKey(a) == spansKey(b) || spansKey(a) != again {
		t.Errorf("keys: %q %q", spansKey(a), spansKey(b))
	}
}

func TestStyledRowsAreOtherParagraphs(t *testing.T) {
	vm, tok := highlighted(t, "package main\n")
	tok.Settle()
	ts := testutil.New(t, ui.Size{Width: 400, Height: 120})
	pumpHighlighted(t, ts, vm, kit.DarkModern(), nil)
	plain := vm.Paragraphs().Builds()
	if plain == 0 {
		t.Fatal("the text was drawn")
	}
	// With a token theme the row is drawn again, in spans, as a paragraph of its own.
	pumpHighlighted(t, ts, vm, kit.DarkModern(), darkModernTokens(t))
	if vm.Paragraphs().Builds() <= plain {
		t.Error("a colored row is a new paragraph")
	}
	noErrors(t, ts)
	// And it keeps its place in the cache when drawn again.
	builds := vm.Paragraphs().Builds()
	ts.Pump(16 * time.Millisecond)
	if vm.Paragraphs().Builds() != builds {
		t.Error("a frame that changed nothing builds no paragraph")
	}
}

func TestTheVisibleLinesAreTokenizedBeforeTheyAreDrawn(t *testing.T) {
	var text strings.Builder
	for range 500 {
		text.WriteString("var x = 1 // a line\n")
	}
	vm, tok := highlighted(t, text.String())
	ts := testutil.New(t, ui.Size{Width: 500, Height: 200})
	pumpHighlighted(t, ts, vm, kit.DarkModern(), darkModernTokens(t))
	if tok.Line(0) == nil || (!raceEnabled && tok.Line(5) == nil) {
		t.Error("the lines on the screen have tokens although the background loop never ran")
	}
	if tok.Line(400) != nil {
		t.Error("the lines far below are left to the background loop")
	}
	noErrors(t, ts)
}

func TestTokensOnTheScreenRepaintAndOthersDoNot(t *testing.T) {
	vm, _ := highlighted(t, "package main\n")
	var repaints int
	vm.AddListener(func() { repaints++ })
	vm.ensureTokens(0, 10)
	cases := []struct {
		from, to int
		want     int
	}{{50, 60, 0}, {5, 6, 1}, {8, 40, 1}, {11, 12, 0}}
	for _, tc := range cases {
		before := repaints
		vm.tokensChanged(tc.from, tc.to)
		if repaints-before != tc.want {
			t.Errorf("lines %d-%d: %d repaints, want %d", tc.from, tc.to, repaints-before, tc.want)
		}
	}
	// Changes that come while a repaint is posted are one repaint.
	var posted []func()
	vm.SetPoster(func(f func()) { posted = append(posted, f) })
	before := repaints
	vm.tokensChanged(1, 2)
	vm.tokensChanged(60, 70)
	vm.tokensChanged(3, 4)
	if len(posted) != 1 {
		t.Fatalf("%d repaints were posted", len(posted))
	}
	posted[0]()
	if repaints-before != 1 {
		t.Errorf("the repaint: %d", repaints-before)
	}
	// With no tokenizer the view model has none.
	vm.SetTokenizer(nil)
	if vm.Tokenizer() != nil || vm.lineTokens(0) != nil {
		t.Error("no tokenizer, no tokens")
	}
}

func TestGoldenHighlightedGo(t *testing.T) {
	vm, tok := highlighted(t, goldenSample(t))
	tok.Settle() // a golden must not depend on how far the budget got
	ts := testutil.New(t, ui.Size{Width: 640, Height: 440})
	pumpHighlighted(t, ts, vm, kit.DarkModern(), darkModernTokens(t))
	noErrors(t, ts)
	ts.MatchesGolden("highlighted_go")
}

func TestGoldenHighlightedGoLight(t *testing.T) {
	th, err := theme.LoadBundled("Light Modern")
	if err != nil {
		t.Fatal(err)
	}
	vm, tok := highlighted(t, goldenSample(t))
	tok.Settle() // a golden must not depend on how far the budget got
	ts := testutil.New(t, ui.Size{Width: 640, Height: 440})
	pumpHighlighted(t, ts, vm, bundledColors(t, "Light Modern"), th.TokenTheme(nil))
	noErrors(t, ts)
	ts.MatchesGolden("highlighted_go_light")
}
