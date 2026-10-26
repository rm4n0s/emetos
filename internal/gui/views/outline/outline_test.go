package outline

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/rm4n0s/glutter/animation"
	"github.com/rm4n0s/glutter/foundation"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/painting"
	"github.com/rm4n0s/glutter/ui"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

func rg(l1, c1, l2, c2 int) buffer.Range {
	return buffer.Range{Start: buffer.Position{Line: l1, Col: c1}, End: buffer.Position{Line: l2, Col: c2}}
}

const source = "package x\n\ntype Server struct {\n\tAddr string\n}\n\nfunc (s *Server) Start() error {\n\tgo func() {}()\n\treturn nil\n}\n\nfunc Stop() {}\n"

func symbols() []langfeatures.DocumentSymbol {
	return []langfeatures.DocumentSymbol{
		{Name: "Server", Kind: 23, Detail: "struct", Range: rg(2, 0, 4, 1), SelectionRange: rg(2, 5, 2, 11), Children: []langfeatures.DocumentSymbol{
			{Name: "Addr", Kind: 8, Detail: "string", Range: rg(3, 1, 3, 12), SelectionRange: rg(3, 1, 3, 5)},
		}},
		{Name: "Start", Kind: 6, Range: rg(6, 0, 9, 1), SelectionRange: rg(6, 18, 6, 23), Children: []langfeatures.DocumentSymbol{
			{Name: "func1", Kind: 12, Range: rg(7, 4, 7, 15), SelectionRange: rg(7, 4, 7, 8)},
		}},
		{Name: "Stop", Kind: 12, Deprecated: true, Range: rg(11, 0, 11, 14), SelectionRange: rg(11, 5, 11, 9)},
	}
}

type fixed struct{ syms []langfeatures.DocumentSymbol }

func (f fixed) ProvideDocumentSymbols(context.Context, langfeatures.Document) ([]langfeatures.DocumentSymbol, error) {
	return f.syms, nil
}

// rig is an outline over an editor whose provider answers the symbols above.
type rig struct {
	t     *testing.T
	o     *Outline
	vm    *editor.ViewModel
	posts chan func()
}

func newRig(t *testing.T, opts Opts, provider bool) *rig {
	t.Helper()
	doc := document.New(document.FileURI("/x.go"), source, document.NewOptions())
	doc.SetLanguageID("go")
	vm := editor.NewViewModel(doc, editor.Options{FontFamilies: []string{testutil.MonoFamily}, FontSize: 14, LineHeight: 19, TabSize: 4}, nil)
	r := &rig{t: t, vm: vm, o: New(opts), posts: make(chan func(), 64)}
	vm.SetPoster(func(fn func()) { r.posts <- fn })
	reg := langfeatures.NewRegistry()
	if provider {
		reg.RegisterSymbols("test", langfeatures.Selector{}, fixed{symbols()})
	}
	vm.SetSuggest(&editor.SuggestHost{Ctx: t.Context(), Features: reg, Symbols: langfeatures.NewSymbolCache(t.Context(), reg)})
	t.Cleanup(vm.DisposeSymbols)
	t.Cleanup(r.o.Dispose)
	r.o.SetEditor(vm)
	r.settle()
	return r
}

func (r *rig) settle() {
	for {
		select {
		case fn := <-r.posts:
			fn()
		case <-time.After(60 * time.Millisecond):
			return
		}
	}
}

func (r *rig) keys() []string {
	var out []string
	for _, row := range r.o.Tree().Rows() {
		out = append(out, row.Key)
	}
	return out
}

func (r *rig) cursor(line, col int) {
	r.vm.Cursors().SetSelections([]buffer.Selection{buffer.NewCursor(buffer.Position{Line: line, Col: col})})
	r.vm.SelectionsChanged()
}

func TestTheTreeHasTheSymbols(t *testing.T) {
	r := newRig(t, Opts{}, true)
	if !r.o.HasSymbols() {
		t.Fatal("no symbols")
	}
	want := []string{"/Server@23", "/Server@23/Addr@8", "/Start@6", "/Start@6/func1@12", "/Stop@12"}
	if got := r.keys(); !slices.Equal(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
}

func TestAnEditorWithoutAProviderHasNoSymbols(t *testing.T) {
	r := newRig(t, Opts{}, false)
	if r.o.HasSymbols() || len(r.keys()) != 0 {
		t.Errorf("symbols = %v, rows = %v", r.o.HasSymbols(), r.keys())
	}
	r.o.SetEditor(nil)
	if r.o.Editor() != nil || r.o.HasSymbols() {
		t.Error("an outline with no editor has symbols")
	}
}

func TestHiddenKindsPromoteTheirChildren(t *testing.T) {
	r := newRig(t, Opts{Show: func(kind int) bool { return kind != 23 }}, true) // no structs
	want := []string{"/Server@23/Addr@8", "/Start@6", "/Start@6/func1@12", "/Stop@12"}
	if got := r.keys(); !slices.Equal(got, want) {
		t.Errorf("rows = %v, want %v", got, want)
	}
	r.o.opts.Show = nil
	r.o.Refresh()
	if got := r.keys(); !slices.Contains(got, "/Server@23") {
		t.Errorf("rows = %v", got)
	}
}

func TestCollapseItems(t *testing.T) {
	r := newRig(t, Opts{Collapse: func() string { return "alwaysCollapse" }}, true)
	if got := r.keys(); !slices.Equal(got, []string{"/Server@23", "/Start@6", "/Stop@12"}) {
		t.Errorf("rows = %v", got)
	}
	r.cursor(7, 5) // inside func1: the folders above it open
	if got := r.keys(); !slices.Contains(got, "/Start@6/func1@12") {
		t.Errorf("rows = %v", got)
	}
	r.o.CollapseAll()
	if got := r.keys(); len(got) != 3 {
		t.Errorf("after Collapse All: %v", got)
	}
}

func TestTheSelectionFollowsTheCursor(t *testing.T) {
	r := newRig(t, Opts{}, true)
	sel := func() string { k, _ := r.o.Tree().Selected(); return k }
	for _, tc := range []struct {
		name      string
		line, col int
		want      string
	}{
		{"in a field", 3, 3, "/Server@23/Addr@8"},
		{"in a nested function", 7, 6, "/Start@6/func1@12"},
		{"in the method", 8, 2, "/Start@6"},
		{"in the last function", 11, 6, "/Stop@12"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r.cursor(tc.line, tc.col)
			if got := sel(); got != tc.want {
				t.Errorf("selected %q, want %q", got, tc.want)
			}
		})
	}
	// between the symbols nothing changes
	r.cursor(10, 0)
	if got := sel(); got != "/Stop@12" {
		t.Errorf("selected %q between symbols", got)
	}
	r.o.SetFollowing(false)
	r.cursor(3, 3)
	if got := sel(); got != "/Stop@12" {
		t.Errorf("selected %q while not following", got)
	}
	r.o.SetFollowing(true)
	if got := sel(); got != "/Server@23/Addr@8" {
		t.Errorf("selected %q after following again", got)
	}
}

func TestChoosingASymbolGoesToItsName(t *testing.T) {
	var got []string
	r := newRig(t, Opts{OnReveal: func(_ *editor.ViewModel, s langfeatures.DocumentSymbol) { got = append(got, s.Name) }}, true)
	r.o.Choose("/Start@6")
	r.o.Choose("/missing")
	if !slices.Equal(got, []string{"Start"}) {
		t.Errorf("revealed %v", got)
	}
	r2 := newRig(t, Opts{}, true)
	r2.o.Choose("/Stop@12")
	if p := r2.vm.Cursors().Primary().Position(); p != (buffer.Position{Line: 11, Col: 5}) {
		t.Errorf("cursor = %v", p)
	}
}

func TestRepeatedNamesGetOwnKeys(t *testing.T) {
	r := newRig(t, Opts{}, true)
	got := keys("", []langfeatures.DocumentSymbol{{Name: "a", Kind: 12}, {Name: "a", Kind: 12}, {Name: "a", Kind: 13}})
	if !slices.Equal(got, []string{"/a@12", "/a@12~1", "/a@13"}) {
		t.Errorf("keys = %v", got)
	}
	_ = r
}

func TestListenersAreTold(t *testing.T) {
	r := newRig(t, Opts{}, true)
	n := 0
	stop := r.o.OnChange(func() { n++ })
	r.cursor(11, 6)
	r.o.SetFollowing(false)
	if n == 0 {
		t.Error("the listener heard nothing")
	}
	stop()
	n = 0
	r.o.SetFollowing(true)
	if n != 0 {
		t.Error("a stopped listener was called")
	}
}

func pumpBody(t *testing.T, colors kit.Colors, size ui.Size, o *Outline) *gluttertest.Tester {
	t.Helper()
	ts := testutil.New(t, size)
	ts.PumpWidget(widgets.NewWidgetsApp(colors.SideBarBackground, widgets.WidgetsAppOpts{
		DebugShowCheckedModeBanner: foundation.Some(false),
		OnGenerateRoute: func(settings widgets.RouteSettings) widgets.Route {
			return widgets.NewPageRouteBuilder(func(widgets.BuildContext, animation.Animation[float64], animation.Animation[float64]) widgets.Widget {
				page := widgets.NewColoredBox(colors.SideBarBackground, widgets.NewAlign(
					widgets.NewSizedBox(NewBody(o), widgets.SizedBoxOpts{Width: foundation.Some(size.Width), Height: foundation.Some(size.Height)}),
					widgets.AlignOpts{Alignment: painting.AlignmentTopLeft}))
				return kit.NewTheme(colors, page, kit.ThemeOpts{UIFont: testutil.MonoFamily})
			}, widgets.PageRouteBuilderOpts{PageRouteOpts: widgets.PageRouteOpts{RouteOpts: widgets.RouteOpts{Settings: settings}}})
		},
	}))
	ts.Pump(time.Millisecond)
	return ts
}

func TestBodyShowsTheMessageAndThenTheTree(t *testing.T) {
	r := newRig(t, Opts{}, false)
	ts := pumpBody(t, kit.DarkModern(), ui.Size{Width: 260, Height: 200}, r.o)
	if !ts.Exists(gluttertest.ByText(NoSymbols)) {
		t.Fatal("the message is not shown")
	}
	// a provider that starts later: the body follows the model
	reg := langfeatures.NewRegistry()
	reg.RegisterSymbols("late", langfeatures.Selector{}, fixed{symbols()})
	h := &editor.SuggestHost{Ctx: t.Context(), Features: reg, Symbols: langfeatures.NewSymbolCache(t.Context(), reg)}
	r.vm.SetSuggest(h)
	r.settle()
	ts.Pump(time.Millisecond)
	ts.Pump(time.Millisecond)
	if !ts.Exists(gluttertest.ByKey(TreeKey)) || ts.Exists(gluttertest.ByText(NoSymbols)) {
		t.Error("the tree is not shown")
	}
	if errs := ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("the framework reported errors: %v", errs)
	}
}

func TestGoldenOutline(t *testing.T) {
	for _, tc := range []struct {
		name   string
		colors kit.Colors
	}{{"dark", kit.DarkModern()}, {"light", kit.LightModern()}} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, Opts{}, true)
			r.cursor(7, 6)
			ts := pumpBody(t, tc.colors, ui.Size{Width: 260, Height: 160}, r.o)
			for range 3 {
				ts.Pump(time.Millisecond)
			}
			if errs := ts.TakeErrors(); len(errs) != 0 {
				t.Errorf("the framework reported errors: %v", errs)
			}
			testutil.MatchesGolden(ts, "outline_"+tc.name)
		})
	}
	t.Run("message", func(t *testing.T) {
		r := newRig(t, Opts{}, false)
		ts := pumpBody(t, kit.DarkModern(), ui.Size{Width: 260, Height: 80}, r.o)
		testutil.MatchesGolden(ts, "outline_message_dark")
	})
}
