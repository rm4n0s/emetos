package editor

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/kit/fonts"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

const hoverWidgetText = "package main\n\nimport \"fmt\"\n\nfunc main() {\n\tfmt.Println(\"hello\", 42)\n\tx := undefined\n}\n"

// goDocs is what gopls says about fmt.Println: a code block, a rule and the documentation.
const goDocs = "```go\nfunc fmt.Println(a ...any) (n int, err error)\n```\n\n---\n\nPrintln formats using the default formats for its operands and writes to standard output. Spaces are always added between operands and a newline is appended.\n\n[`fmt.Println` on pkg.go.dev](https://pkg.go.dev/fmt#Println)"

var goWord = regexp.MustCompile(`"[^"]*"|[A-Za-z_][A-Za-z0-9_]*|\d+`)

// goTokens colors a few words of Go as a grammar would, for the tests that look at code blocks.
func goTokens(language, text string) ([][]extension.Token, bool) {
	if language != "go" {
		return nil, false
	}
	var out [][]extension.Token
	for _, line := range strings.Split(text, "\n") {
		var toks []extension.Token
		for _, loc := range goWord.FindAllStringIndex(line, -1) {
			w := line[loc[0]:loc[1]]
			scope := ""
			switch {
			case w[0] == '"':
				scope = "string.quoted.double.go"
			case w == "func" || w == "return":
				scope = "storage.type.function.go"
			case w == "int" || w == "error" || w == "any" || w == "string":
				scope = "entity.name.type.go"
			case w[0] >= '0' && w[0] <= '9':
				scope = "constant.numeric.go"
			case strings.HasPrefix(line[loc[1]:], "("):
				scope = "entity.name.function.go"
			}
			if scope != "" {
				toks = append(toks, extension.Token{Start: loc[0], End: loc[1], Scopes: []string{scope, "source.go"}})
			}
		}
		out = append(out, toks)
	}
	return out, true
}

// hoverWidgetRig is an editor on screen with a hover provider and the sample diagnostics.
func hoverWidgetRig(t *testing.T, size ui.Size, themeName string, tweak func(reg *langfeatures.Registry)) (*ViewModel, *gluttertest.Tester) {
	t.Helper()
	colors := bundledColors(t, themeName)
	opts := goOptions()
	opts.CursorBlinking = "solid"
	opts.Minimap.Enabled = false
	opts.Hover = HoverOptions{Enabled: "on", Delay: 300, Sticky: true, HidingDelay: 300, Above: true}
	opts.ParameterHints = ParameterHintsOptions{Enabled: true, Cycle: true}
	doc := newTestDoc(hoverWidgetText)
	doc.SetLanguageID("go")
	vm := NewViewModel(doc, opts, nil)
	reg := langfeatures.NewRegistry()
	if tweak != nil {
		tweak(reg)
	}
	vm.SetSuggest(&SuggestHost{Ctx: t.Context(), Features: reg, Tokenize: goTokens})
	t.Cleanup(vm.HideHover)
	t.Cleanup(vm.SignatureCancel)
	ts := testutil.New(t, size)
	// the monospace font of the desktop is the test font, and the tokens are those of the bundled theme of the colors
	ts.SetPlatformConfiguration(func(c *ui.PlatformConfiguration) {
		c.MonospaceFontFamily, c.MonospaceFontSize = testutil.MonoFamily, 13
	})
	tokens, err := kit.BundledTokens(themeName)
	if err != nil {
		t.Fatal(err)
	}
	ts.PumpWidget(kit.NewTheme(colors, widgets.NewWidgetsApp(colors.EditorBackground, widgets.WidgetsAppOpts{
		DebugShowCheckedModeBanner: foundation.Some(false),
		OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(
				func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
					return NewEditor(vm, EditorOpts{Autofocus: true})
				},
				widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
		},
	}), kit.ThemeOpts{UIFont: testutil.MonoFamily, Tokens: tokens}))
	for range 3 {
		ts.Pump(time.Millisecond)
	}
	return vm, ts
}

// richContaining finds the rich text that has a text in it.
func richContaining(sub string) gluttertest.Finder {
	return gluttertest.ByPredicate("rich text with "+sub, func(w widgets.Widget) bool {
		rt, ok := w.(*widgets.RichText)
		return ok && strings.Contains(rt.PlainText(), sub)
	})
}

// settleWidgets runs the answers posted to the UI thread and draws.
func settleWidgets(ts *gluttertest.Tester) {
	for range 4 {
		time.Sleep(20 * time.Millisecond)
		ts.FlushPosted()
		ts.Pump(50 * time.Millisecond)
	}
}

func TestGoldenHoverWithMarkdown(t *testing.T) {
	for _, tc := range []struct{ golden, theme string }{
		{"hover_markdown_dark", "Dark Modern"},
		{"hover_markdown_light", "Light Modern"},
	} {
		t.Run(tc.theme, func(t *testing.T) {
			vm, ts := hoverWidgetRig(t, ui.Size{Width: 640, Height: 380}, tc.theme, func(reg *langfeatures.Registry) {
				reg.RegisterHover("gopls", langfeatures.Selector{Languages: []string{"go"}}, &fakeHover{hover: &langfeatures.Hover{
					Contents: []langfeatures.Markup{{Value: goDocs, Markdown: true}},
					Range:    &buffer.Range{Start: positionOf(5, 5), End: positionOf(5, 12)},
				}})
			})
			vm.SetDiagnostics([]Diagnostic{{Range: buffer.Range{Start: positionOf(5, 5), End: positionOf(5, 12)}, Severity: DiagnosticWarning, Message: "a warning stacked in the same hover", Source: "lint"}})
			vm.ShowHoverAt(positionOf(5, 7))
			settleWidgets(ts)
			noErrors(t, ts)
			if vm.Hover() == nil || len(vm.Hover().Parts) != 2 {
				t.Fatalf("hover = %+v", vm.Hover())
			}
			testutil.MatchesGolden(ts, tc.golden)
		})
	}
}

func TestHoverBoxFocusedShowsTheFocusBorderAndScrolls(t *testing.T) {
	long := "```go\nfunc long()\n```\n\n" + strings.Repeat("A paragraph of documentation that goes on and on.\n\n", 30)
	vm, ts := hoverWidgetRig(t, ui.Size{Width: 640, Height: 400}, "Dark Modern", func(reg *langfeatures.Registry) {
		reg.RegisterHover("gopls", langfeatures.Selector{Languages: []string{"go"}}, &fakeHover{hover: &langfeatures.Hover{Contents: []langfeatures.Markup{{Value: long, Markdown: true}}}})
	})
	cursorAt(vm, 5, 7)
	if err := vm.RunCommand("editor.action.showHover", nil); err != nil {
		t.Fatal(err)
	}
	settleWidgets(ts)
	if vm.Hover() == nil {
		t.Fatal("no hover")
	}
	if err := vm.RunCommand("editor.action.showHover", nil); err != nil {
		t.Fatal(err)
	}
	ts.Pump(50 * time.Millisecond)
	before := ts.TopLeft(richContaining("func long()"))
	for range 3 {
		if err := vm.RunCommand("editor.action.pageDownHover", nil); err != nil {
			t.Fatal(err)
		}
		ts.Pump(50 * time.Millisecond)
	}
	if after := ts.TopLeft(richContaining("func long()")); after.Dy >= before.Dy {
		t.Errorf("the box did not scroll: the code was at %v and is at %v", before, after)
	}
	if err := vm.RunCommand("editor.action.goToTopHover", nil); err != nil {
		t.Fatal(err)
	}
	ts.Pump(50 * time.Millisecond)
	if again := ts.TopLeft(richContaining("func long()")); again != before {
		t.Errorf("Go To Top left the code at %v, want %v", again, before)
	}
	noErrors(t, ts)
}

func TestHoverLinkOpensThroughTheOpener(t *testing.T) {
	vm, ts := hoverWidgetRig(t, ui.Size{Width: 640, Height: 300}, "Dark Modern", func(reg *langfeatures.Registry) {
		reg.RegisterHover("gopls", langfeatures.Selector{Languages: []string{"go"}}, &fakeHover{hover: &langfeatures.Hover{Contents: []langfeatures.Markup{{Value: goDocs, Markdown: true}}}})
	})
	var opened []string
	vm.SetOpeners(func(u string) { opened = append(opened, u) }, nil)
	vm.ShowHoverAt(positionOf(5, 7))
	settleWidgets(ts)
	at := ts.TopLeft(richContaining("on pkg.go.dev"))
	ts.TapAt(ui.Offset{Dx: at.Dx + 30, Dy: at.Dy + 8})
	if len(opened) != 1 || opened[0] != "https://pkg.go.dev/fmt#Println" {
		t.Errorf("opened %v", opened)
	}
}

// signatureWidgetRig is an editor with the parameter hints of fmt.Println open, with the cursor after the opening bracket.
func signatureWidgetRig(t *testing.T, size ui.Size, themeName string, help *langfeatures.SignatureHelp) (*ViewModel, *gluttertest.Tester) {
	t.Helper()
	vm, ts := hoverWidgetRig(t, size, themeName, func(reg *langfeatures.Registry) {
		reg.RegisterSignatureHelp("gopls", langfeatures.Selector{Languages: []string{"go"}}, &fakeSignature{help: help, triggers: []string{"(", ","}})
	})
	cursorAt(vm, 5, 13) // after the "(" of fmt.Println(
	if err := vm.RunCommand("editor.action.triggerParameterHints", nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for vm.SignatureHints() == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		ts.FlushPosted()
	}
	if vm.SignatureHints() == nil {
		t.Fatal("the hints do not show")
	}
	settleWidgets(ts)
	return vm, ts
}

func TestGoldenParameterHints(t *testing.T) {
	println1 := langfeatures.Signature{
		Label: "Println(a ...any) (n int, err error)", Documentation: &langfeatures.Markup{Markdown: true, Value: "Println formats using the default formats for its operands and writes to standard output.\n\nSpaces are always added between operands and a newline is appended."},
		Parameters: []langfeatures.Parameter{{Label: "a ...any", Documentation: &langfeatures.Markup{Value: "the operands to print"}}},
	}
	// the label of this parameter is a pair of offsets, in the characters of the label
	println2 := langfeatures.Signature{
		Label:      "Println(w io.Writer, a ...any) (n int, err error)",
		Parameters: []langfeatures.Parameter{{Label: "w io.Writer", Offsets: &[2]int{8, 19}}, {Label: "a ...any", Offsets: &[2]int{21, 29}}},
	}
	for _, tc := range []struct {
		golden, theme string
		help          *langfeatures.SignatureHelp
	}{
		{"parameter_hints_dark", "Dark Modern", &langfeatures.SignatureHelp{Signatures: []langfeatures.Signature{println1}}},
		{"parameter_hints_light", "Light Modern", &langfeatures.SignatureHelp{Signatures: []langfeatures.Signature{println1}}},
		{"parameter_hints_overloads_dark", "Dark Modern", &langfeatures.SignatureHelp{Signatures: []langfeatures.Signature{println2, println1}, ActiveParameter: 1}},
		{"parameter_hints_overloads_light", "Light Modern", &langfeatures.SignatureHelp{Signatures: []langfeatures.Signature{println2, println1}, ActiveParameter: 1}},
	} {
		t.Run(tc.golden, func(t *testing.T) {
			_, ts := signatureWidgetRig(t, ui.Size{Width: 640, Height: 300}, tc.theme, tc.help)
			noErrors(t, ts)
			testutil.MatchesGolden(ts, tc.golden)
		})
	}
}

func TestParameterHintsArrowsAndCounterWork(t *testing.T) {
	a := langfeatures.Signature{Label: "f(a)", Parameters: []langfeatures.Parameter{{Label: "a"}}}
	b := langfeatures.Signature{Label: "f(a, b)", Parameters: []langfeatures.Parameter{{Label: "a"}, {Label: "b"}}}
	vm, ts := signatureWidgetRig(t, ui.Size{Width: 640, Height: 300}, "Dark Modern", &langfeatures.SignatureHelp{Signatures: []langfeatures.Signature{a, b}})
	if !ts.Exists(gluttertest.ByText("1/2")) {
		t.Fatal("the counter does not show 1/2")
	}
	ts.Tap(gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconChevronDown)))
	ts.Pump(50 * time.Millisecond)
	if vm.SignatureHints().ActiveSignature != 1 || !ts.Exists(gluttertest.ByText("2/2")) {
		t.Errorf("the down arrow does not show the second signature: %d", vm.SignatureHints().ActiveSignature)
	}
	ts.Tap(gluttertest.ByIcon(fonts.CodiconIcon(fonts.CodiconChevronUp)))
	ts.Pump(50 * time.Millisecond)
	if vm.SignatureHints().ActiveSignature != 0 {
		t.Error("the up arrow does not go back")
	}
	noErrors(t, ts)
}

func TestParameterHintsStandAboveTheCursorOrBelowWhenThereIsNoRoom(t *testing.T) {
	sig := langfeatures.Signature{Label: "Println(a ...any)", Parameters: []langfeatures.Parameter{{Label: "a ...any"}}}
	for _, tc := range []struct {
		name      string
		line      int
		wantAbove bool
	}{{"room above", 5, true}, {"first line", 0, false}} {
		t.Run(tc.name, func(t *testing.T) {
			vm, ts := signatureWidgetRig(t, ui.Size{Width: 640, Height: 300}, "Dark Modern", &langfeatures.SignatureHelp{Signatures: []langfeatures.Signature{sig}})
			cursorAt(vm, tc.line, 0)
			ts.Pump(50 * time.Millisecond)
			at := ts.TopLeft(richContaining("Println(a ...any)"))
			rowTop := float64(tc.line) * 19
			if above := at.Dy < rowTop; above != tc.wantAbove {
				t.Errorf("the hints are at %v for a line at %v: above = %v, want %v", at.Dy, rowTop, above, tc.wantAbove)
			}
		})
	}
}
