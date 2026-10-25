package mergeeditor

import (
	"context"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/diff"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/language"
	"github.com/rm4n0s/emetos/internal/core/language/languagetest"
	"github.com/rm4n0s/emetos/internal/core/regex"
	"github.com/rm4n0s/emetos/internal/core/theme"
	"github.com/rm4n0s/emetos/internal/core/tokens"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// testOptions are the options of the editors of the tests: Go Mono, 19 pixel rows, the line numbers on.
func testOptions() editor.Options {
	return editor.Options{
		FontFamilies: []string{testutil.MonoFamily, "monospace"}, FontSize: 14, LineHeight: 19, TabSize: 4,
		LineNumbers: editor.LineNumbersOn, LineNumbersMinChars: 5, GlyphMargin: true, LineDecorationsWidth: 10,
		ScrollBeyondLastLine: true, RenderLineHighlight: "line", RoundedSelection: true, CursorBlinking: "solid",
		MultiCursorModifier: "alt", MouseStyle: "text", MouseWheelScrollSensitivity: 1, FastScrollSensitivity: 5,
		DragAndDrop: true, MultiCursorPaste: "spread", EmptySelectionClipboard: true, FontWeight: 400, CursorStyle: "line",
		FontFeatures:        []editor.FontFeature{{Tag: "liga"}, {Tag: "calt"}},
		FindLoop:            true,
		CommentsInsertSpace: true, CommentsIgnoreEmptyLines: true,
	}
}

// rig is a merge editor on screen.
type rig struct {
	t       *testing.T
	ts      *gluttertest.Tester
	model   *Model
	session *Session
	in1     *document.Document
	in2     *document.Document
	result  *document.Document
	errs    []error
	// tokenTheme colors the text when the rig highlights.
	tokenTheme *theme.TokenTheme
	// onComplete is the Complete Merge button's callback.
	onComplete func()
}

type rigConfig struct {
	size   ui.Size
	colors kit.Colors
	tweak  func(*editor.Options)
	// inputs are the texts of the merge; the rig makes the result document from resultText ("" for an empty file).
	inputs     diff.MergeInputs
	resultText string
	initial    Initial
	// highlight shows the text as Go, with the token theme of Dark Modern or Light Modern ("Dark Modern", "Light Modern").
	highlight string
	// onComplete is what the Complete Merge button of the header does; the button is left out without it.
	onComplete func()
}

// newRig shows a merge and settles it.
func newRig(t *testing.T, cfg rigConfig) *rig {
	t.Helper()
	testutil.Isolate(t)
	if cfg.size == (ui.Size{}) {
		cfg.size = ui.Size{Width: 900, Height: 520}
	}
	if cfg.colors == (kit.Colors{}) {
		cfg.colors = kit.DarkModern()
	}
	r := &rig{t: t, onComplete: cfg.onComplete}
	docOpts := document.NewOptions()
	lang := ""
	if cfg.highlight != "" {
		docOpts.LanguageID, lang = "go", "go"
	}
	virtual := func(name string, lines []string) *document.Document {
		return document.NewVirtual(document.VirtualURI("/merge/"+name), document.VirtualContent{Text: diff.JoinLines(lines, "\n"), Label: name, Language: lang})
	}
	r.in1, r.in2 = virtual("incoming.go", cfg.inputs.Input1), virtual("current.go", cfg.inputs.Input2)
	r.result = document.New(document.FileURI("/w/result.go"), cfg.resultText, docOpts)
	var err error
	r.session, err = NewSession(context.Background(), SessionOpts{Result: r.result, Inputs: cfg.inputs, Initial: cfg.initial,
		OnError: func(err error) { r.errs = append(r.errs, err) }})
	if err != nil {
		t.Fatal(err)
	}
	o := testOptions()
	if cfg.tweak != nil {
		cfg.tweak(&o)
	}
	vms := [3]*editor.ViewModel{}
	for i, doc := range []*document.Document{r.in1, r.in2, r.result} {
		vms[i] = editor.NewViewModel(doc, AdjustOptions(o, Side(i)), nil)
		if cfg.highlight != "" {
			reg, err := languagetest.Bundled()
			if err != nil {
				t.Fatal(err)
			}
			g, err := reg.NewGrammarWith("go", language.GrammarSettings{Cache: regex.NewCache(1 << 14), ScanTimeout: testutil.ScanTimeout})
			if err != nil {
				t.Fatal(err)
			}
			tok := tokens.New(doc, g, tokens.Options{})
			t.Cleanup(tok.Close)
			vms[i].SetTokenizer(tok)
		}
	}
	if cfg.highlight != "" {
		th, err := theme.LoadBundled(cfg.highlight)
		if err != nil {
			t.Fatal(err)
		}
		r.tokenTheme = th.TokenTheme(nil)
	}
	r.ts = testutil.New(t, cfg.size)
	b := r.ts.Binding
	r.model = NewModel(ModelOpts{
		Session: r.session, Incoming: vms[0], Current: vms[1], Result: vms[2], Post: b.Post,
		IncomingLabel: cfg.inputs.Input1Label, CurrentLabel: cfg.inputs.Input2Label, ResultLabel: "result.go",
		OnError: func(err error) { r.errs = append(r.errs, err) },
	})
	t.Cleanup(func() {
		r.ts.PumpWidget(widgets.NewSizedBoxShrink(nil))
		r.model.Dispose()
		r.session.Dispose()
		for _, vm := range vms {
			vm.DisposeZones()
		}
		if len(r.errs) != 0 {
			t.Errorf("the model reported errors: %v", r.errs)
		}
	})
	r.pump(cfg.colors)
	r.settle()
	return r
}

// pump shows the widget in a theme.
func (r *rig) pump(colors kit.Colors) {
	r.t.Helper()
	r.ts.PumpWidget(kit.NewTheme(colors, widgets.NewWidgetsApp(colors.EditorBackground, widgets.WidgetsAppOpts{
		DebugShowCheckedModeBanner: foundation.Some(false),
		OnGenerateRoute: func(s widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(
				func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
					return NewWidget(r.model, WidgetOpts{
						Incoming: editor.EditorOpts{Context: context.Background()},
						Current:  editor.EditorOpts{Context: context.Background()},
						Result:   editor.EditorOpts{Autofocus: true, Context: context.Background()},

						OnCompleteMerge: r.onComplete,
					})
				},
				widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: s}}})
		},
	}), kit.ThemeOpts{UIFont: testutil.MonoFamily, Tokens: r.tokenTheme}))
}

// settle runs a few frames and the work posted to the UI thread.
func (r *rig) settle() {
	r.t.Helper()
	for range 4 {
		r.ts.Pump(time.Millisecond)
		r.ts.FlushPosted()
	}
	r.ts.Pump(time.Millisecond)
}

// noErrors fails the test for the errors the framework reported.
func (r *rig) noErrors() {
	r.t.Helper()
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		r.t.Errorf("the framework reported errors: %v", errs)
	}
}

// goMerge is a merge of a Go file with three changes in the base: the first is made by incoming and current alike
// (a conflict), the second only by incoming (taken without asking), the third by both differently (a conflict).
func goMerge() (diff.MergeInputs, string) {
	const base = `package main

import "fmt"

func greet(name string) {
	fmt.Println("hello", name)
}

func add(a, b int) int {
	return a + b
}

func main() {
	greet("world")
	fmt.Println(add(1, 2))
}
`
	const incoming = `package main

import "fmt"

func greet(name string) {
	fmt.Println("hello,", name)
}

// add sums its arguments.
func add(a, b int) int {
	return a + b
}

func main() {
	greet("everyone")
	fmt.Println(add(1, 2))
}
`
	const current = `package main

import "fmt"

func greet(name string) {
	fmt.Println("hi", name)
}

func add(a, b int) int {
	return a + b
}

func main() {
	greet("people")
	fmt.Println(add(1, 2))
}
`
	split := func(s string) []string { l, _ := diff.SplitLines(s); return l }
	in := diff.MergeInputsFromFiles(split(base), split(incoming), split(current))
	in.Input1Label, in.Input2Label = "incoming.go", "current.go"
	return in, base
}
