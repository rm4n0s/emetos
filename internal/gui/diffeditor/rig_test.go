package diffeditor

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

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/language"
	"github.com/rm4n0s/emetos/internal/core/language/languagetest"
	"github.com/rm4n0s/emetos/internal/core/regex"
	"github.com/rm4n0s/emetos/internal/core/settings"
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

// rig is a diff editor on screen over two documents.
type rig struct {
	t     *testing.T
	ts    *gluttertest.Tester
	model *Model
	orig  *document.Document
	mod   *document.Document
	store *settings.Store
	errs  []error
	// tokenTheme colors the text when the rig highlights.
	tokenTheme *theme.TokenTheme
}

type rigConfig struct {
	size             ui.Size
	colors           kit.Colors
	tweak            func(*editor.Options)
	now              func() time.Time
	readOnlyModified bool
	// settings are user settings to start with; the rig turns diffEditor.useInlineViewWhenSpaceIsLimited off
	// unless they say otherwise, since its default size is under the breakpoint of the inline view.
	settings map[string]any
	// highlight shows the text as Go, with the token theme of Dark Modern or Light Modern ("Dark Modern", "Light Modern").
	highlight string
}

// newRig shows the diff of two texts and waits for the first diff.
func newRig(t *testing.T, original, modified string, cfg rigConfig) *rig {
	t.Helper()
	paths := testutil.Isolate(t)
	if cfg.size == (ui.Size{}) {
		cfg.size = ui.Size{Width: 900, Height: 400}
	}
	if cfg.colors == (kit.Colors{}) {
		cfg.colors = kit.DarkModern()
	}
	extra := map[string]any{"diffEditor.useInlineViewWhenSpaceIsLimited": false}
	for k, v := range cfg.settings {
		extra[k] = v
	}
	r := &rig{t: t, store: testutil.Settings(t, paths, extra)}
	docOpts := document.NewOptions()
	if cfg.highlight != "" {
		docOpts.LanguageID = "go"
	}
	r.orig = document.New(document.FileURI("/orig.go"), original, docOpts)
	docOpts.ReadOnly = cfg.readOnlyModified
	r.mod = document.New(document.FileURI("/mod.go"), modified, docOpts)
	o := testOptions()
	if cfg.tweak != nil {
		cfg.tweak(&o)
	}
	ovm := editor.NewViewModel(r.orig, AdjustOptions(o, Original), nil)
	mvm := editor.NewViewModel(r.mod, AdjustOptions(o, Modified), nil)
	if cfg.highlight != "" {
		reg, err := languagetest.Bundled()
		if err != nil {
			t.Fatal(err)
		}
		for _, side := range []struct {
			doc *document.Document
			vm  *editor.ViewModel
		}{{r.orig, ovm}, {r.mod, mvm}} {
			g, err := reg.NewGrammarWith("go", language.GrammarSettings{Cache: regex.NewCache(1 << 14), ScanTimeout: testutil.ScanTimeout})
			if err != nil {
				t.Fatal(err)
			}
			tok := tokens.New(side.doc, g, tokens.Options{})
			t.Cleanup(tok.Close)
			side.vm.SetTokenizer(tok)
		}
		th, err := theme.LoadBundled(cfg.highlight)
		if err != nil {
			t.Fatal(err)
		}
		r.tokenTheme = th.TokenTheme(nil)
	}
	r.ts = testutil.New(t, cfg.size)
	b := r.ts.Binding
	r.model = NewModel(ModelOpts{
		Original: ovm, Modified: mvm, Settings: r.store, Post: b.Post, Now: cfg.now,
		AfterTimer: func(d time.Duration, fn func()) func() { return b.Gestures().NewTimer(d, fn).Cancel },
		OnError:    func(err error) { r.errs = append(r.errs, err) },
	})
	t.Cleanup(func() {
		r.ts.PumpWidget(widgets.NewSizedBoxShrink(nil))
		r.model.Dispose()
		ovm.DisposeZones()
		mvm.DisposeZones()
		if len(r.errs) != 0 {
			t.Errorf("the model reported errors: %v", r.errs)
		}
	})
	r.pump(cfg.colors)
	r.model.Start()
	r.waitDiff()
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
						Original: editor.EditorOpts{Context: context.Background()},
						Modified: editor.EditorOpts{Autofocus: true, Context: context.Background()},
					})
				},
				widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: s}}})
		},
	}), kit.ThemeOpts{UIFont: testutil.MonoFamily, Tokens: r.tokenTheme}))
}

// settle runs a few frames and the work posted to the UI thread.
func (r *rig) settle() {
	r.t.Helper()
	for range 3 {
		r.ts.Pump(time.Millisecond)
		r.ts.FlushPosted()
	}
	r.ts.Pump(time.Millisecond)
}

// waitFor runs frames until the condition holds, within a bounded number of tries (the diff is computed on a
// goroutine whose result arrives through Post).
func (r *rig) waitFor(what string, cond func() bool) {
	r.t.Helper()
	for range 2000 {
		r.settle()
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	r.t.Fatalf("timed out waiting for %s", what)
}

// waitDiff waits until a diff has been applied that is newer than the one before.
func (r *rig) waitDiff() {
	r.t.Helper()
	r.waitFor("the diff", func() bool { return r.model.Computed() && !r.model.Computing() })
}

func lines(n int, f func(i int) string) string {
	var b strings.Builder
	for i := range n {
		b.WriteString(f(i))
		b.WriteByte('\n')
	}
	return b.String()
}

// noErrors fails the test for the errors the framework reported.
func (r *rig) noErrors() {
	r.t.Helper()
	if errs := r.ts.TakeErrors(); len(errs) != 0 {
		r.t.Errorf("the framework reported errors: %v", errs)
	}
}
