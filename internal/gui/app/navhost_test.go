package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/gui/editor"
	"github.com/rm4n0s/emetos/internal/gui/quickinput"
	outlineview "github.com/rm4n0s/emetos/internal/gui/views/outline"
)

const navSource = "package x\n\nfunc Alpha() {}\n\nfunc Beta() {\n\tAlpha()\n}\n\ntype Gamma struct{}\n"

func rng(l1, c1, l2, c2 int) buffer.Range {
	return buffer.Range{Start: buffer.Position{Line: l1, Col: c1}, End: buffer.Position{Line: l2, Col: c2}}
}

// navSymbols are the symbols of navSource.
func navSymbols() []langfeatures.DocumentSymbol {
	return []langfeatures.DocumentSymbol{
		{Name: "Alpha", Kind: 12, Range: rng(2, 0, 2, 15), SelectionRange: rng(2, 5, 2, 10)},
		{Name: "Beta", Kind: 12, Range: rng(4, 0, 6, 1), SelectionRange: rng(4, 5, 4, 9), Children: []langfeatures.DocumentSymbol{
			{Name: "local", Kind: 13, Range: rng(5, 1, 5, 8), SelectionRange: rng(5, 1, 5, 6)},
		}},
		{Name: "Gamma", Kind: 23, Range: rng(8, 0, 8, 19), SelectionRange: rng(8, 5, 8, 10)},
	}
}

type fixedSymbols struct{ syms []langfeatures.DocumentSymbol }

func (f fixedSymbols) ProvideDocumentSymbols(context.Context, langfeatures.Document) ([]langfeatures.DocumentSymbol, error) {
	return f.syms, nil
}

// withSymbols gives the editor in front a provider of symbols (and keeps the navigation host of the window).
func (a *groupApp) withSymbols(syms []langfeatures.DocumentSymbol) {
	a.t.Helper()
	reg := langfeatures.NewRegistry()
	reg.RegisterSymbols("test", langfeatures.Selector{}, fixedSymbols{syms})
	h := a.win.suggestHost()
	h.Features, h.Symbols = reg, a.win.symbolCache(reg)
	a.vm().SetSuggest(h)
	a.waitFor("the symbols", func() bool { _, ok := a.vm().Symbols(); return ok })
}

func navApp(t *testing.T) *groupApp {
	t.Helper()
	return newGroupAppWith(t, newTestWorkspace(t, ""), testWindowSize, []string{"a.go", "b.go"},
		map[string]string{"a.go": navSource, "b.go": navSource}, "a.go")
}

func (a *groupApp) open(name string, o editor.OpenLocation, at buffer.Range) {
	a.t.Helper()
	a.shell().navOpen(langfeatures.Location{URI: document.FileURI(a.files[name]), Range: at}, o)
	a.waitFor(name+" to show", func() bool { return a.active() == name })
}

func TestNavOpenShowsAnotherFileAsAPreviewAndTheHistoryGoesBack(t *testing.T) {
	a := navApp(t)
	a.cursorTo(5)
	a.open("b.go", editor.OpenLocation{}, rng(2, 5, 2, 10))
	if got := a.vm().Cursors().Primary().Position(); got != (buffer.Position{Line: 2, Col: 5}) {
		t.Errorf("cursor = %v", got)
	}
	if it := a.item("b.go"); !it.Preview {
		t.Errorf("tab = %+v, want a preview editor", it)
	}
	a.run("workbench.action.navigateBack")
	a.waitFor("a.go again", func() bool { return a.active() == "a.go" })
	if a.line() != 5 {
		t.Errorf("back went to line %d, want 5", a.line())
	}
	a.noErrors()
}

func TestNavOpenPinnedAndToTheSide(t *testing.T) {
	a := navApp(t)
	a.open("b.go", editor.OpenLocation{Pinned: true}, rng(4, 5, 4, 9))
	if it := a.item("b.go"); it.Preview {
		t.Errorf("tab = %+v, want a kept editor", it)
	}
	if got := a.vm().Cursors().Primary().Position(); got != (buffer.Position{Line: 4, Col: 5}) {
		t.Errorf("cursor = %v", got)
	}
	a2 := navApp(t)
	a2.shell().navOpen(langfeatures.Location{URI: document.FileURI(a2.files["b.go"]), Range: rng(4, 5, 4, 9)}, editor.OpenLocation{Side: true})
	a2.waitFor("the group beside", func() bool {
		return a2.shell().group.grid.Count() == 2 && a2.shell().activeEditor() != nil && a2.shell().activeEditor().doc == a2.doc("b.go")
	})
	if got := a2.shell().activeEditor().view.Cursors().Primary().Position(); got != (buffer.Position{Line: 4, Col: 5}) {
		t.Errorf("cursor = %v", got)
	}
}

func TestNavOpenInTheSameFileJumpsAndRecordsHistory(t *testing.T) {
	a := navApp(t)
	a.cursorTo(1)
	after := 0
	a.shell().navOpen(langfeatures.Location{URI: document.FileURI(a.files["a.go"]), Range: rng(8, 5, 8, 10)}, editor.OpenLocation{After: func(*editor.ViewModel) { after++ }})
	if a.line() != 8 || after != 1 {
		t.Fatalf("line = %d, after = %d", a.line(), after)
	}
	a.run("workbench.action.navigateBack")
	if a.line() != 1 {
		t.Errorf("back went to line %d, want 1", a.line())
	}
}

func TestNavOpenOfAFileThatIsGoneIsReported(t *testing.T) {
	a := navApp(t)
	a.shell().navOpen(langfeatures.Location{URI: document.FileURI(a.files["a.go"] + ".missing")}, editor.OpenLocation{})
	a.waitFor("the notification", func() bool { return len(a.win.Notify.Items()) > 0 })
}

func TestEmbeddedEditorsHaveNoTabAndLetGoOfTheirDocument(t *testing.T) {
	a := navApp(t)
	tabs := a.tabNames()
	other := document.FileURI(a.files["b.go"])
	var vm *editor.ViewModel
	var release func()
	var gotErr error
	a.shell().openEmbedded(context.Background(), other, func(v *editor.ViewModel, r func(), err error) { vm, release, gotErr = v, r, err })
	a.waitFor("the editor", func() bool { return vm != nil || gotErr != nil })
	if gotErr != nil {
		t.Fatal(gotErr)
	}
	if !vm.IsEmbedded() || !slices.Equal(a.tabNames(), tabs) {
		t.Errorf("embedded = %v, tabs %v", vm.IsEmbedded(), a.tabNames())
	}
	a.wantTokens(vm, "a file read for the peek view")
	doc, ok := a.win.Documents.Get(other)
	if !ok || a.win.Documents.Refs(doc) != 1 {
		t.Fatalf("document open = %v", ok)
	}
	release()
	if _, ok := a.win.Documents.Get(other); ok {
		t.Error("the document stays open after the editor let go of it")
	}
	// an open document is shared, not read again
	a.show("b.go", true)
	var vm2 *editor.ViewModel
	var release2 func()
	a.shell().openEmbedded(context.Background(), document.FileURI(a.files["b.go"]), func(v *editor.ViewModel, r func(), _ error) { vm2, release2 = v, r })
	if vm2 == nil || vm2.Document() != a.doc("b.go") {
		t.Fatal("the embedded editor does not share the open document")
	}
	a.wantTokens(vm2, "an open document shown in the peek view")
	release2()
	if _, ok := a.win.Documents.Get(document.FileURI(a.files["b.go"])); !ok {
		t.Error("the editor of the tab lost its document")
	}
}

func TestEmbeddedEditorOfAnUnsupportedURIFails(t *testing.T) {
	a := navApp(t)
	var got error
	a.shell().openEmbedded(context.Background(), document.URI{Scheme: "https", Path: "/x"}, func(_ *editor.ViewModel, _ func(), err error) { got = err })
	appErr, ok := errors.FromError(got)
	if !ok {
		t.Fatalf("error = %T %v", got, got)
	}
	if !appErr.HasRoute("openEmbedded.PeekFileUnsupported") {
		t.Errorf("route = %s", appErr.Route())
	}
}

func TestSymbolPickerListsFiltersAndGoesToASymbol(t *testing.T) {
	a := navApp(t)
	a.withSymbols(navSymbols())
	a.cursorTo(0)
	a.press("ctrl+shift+o")
	if !a.win.QuickInput.Visible() || a.win.QuickInput.Provider().Info().Prefix != "@" {
		t.Fatalf("the quick input shows %v", a.win.QuickInput.Visible())
	}
	if got := a.pickerLabels(); !slices.Equal(got, []string{"Alpha", "Beta", "local", "Gamma"}) {
		t.Fatalf("rows = %v", got)
	}
	a.typeInPicker("@gam")
	if got := a.pickerLabels(); !slices.Equal(got, []string{"Gamma"}) {
		t.Fatalf("filtered = %v", got)
	}
	a.press("enter")
	if got := a.vm().Cursors().Primary().Position(); got != (buffer.Position{Line: 8, Col: 5}) {
		t.Errorf("cursor = %v", got)
	}
	a.run("workbench.action.navigateBack")
	if a.line() != 0 {
		t.Errorf("back went to line %d, want 0", a.line())
	}
}

func TestSymbolPickerByCategory(t *testing.T) {
	a := navApp(t)
	a.withSymbols(navSymbols())
	a.run("workbench.action.gotoSymbol")
	a.typeInPicker("@:")
	rows := a.win.QuickInput.Picker().Rows()
	var seps []string
	for _, r := range rows {
		if r.Separator != "" {
			seps = append(seps, r.Separator)
		}
	}
	if !slices.Equal(seps, []string{"functions (2)", "variables (1)", "structs (1)"}) {
		t.Errorf("headings = %v", seps)
	}
	a.press("escape")
}

func TestSymbolPickerWithoutSymbols(t *testing.T) {
	a := navApp(t)
	a.run("workbench.action.gotoSymbol")
	if got := a.pickerLabels(); len(got) != 1 || !strings.Contains(got[0], "does not provide symbol information") {
		t.Errorf("rows = %v", got)
	}
	a.press("escape")
}

func (a *groupApp) typeInPicker(text string) {
	a.t.Helper()
	a.ts.EnterText(gluttertest.First(gluttertest.ByType[*widgets.EditableText]()), text)
	a.pump()
}

func TestSymbolItems(t *testing.T) {
	syms := navSymbols()
	var opened []string
	open := func(s *langfeatures.DocumentSymbol) { opened = append(opened, s.Name) }
	for _, tc := range []struct {
		name, filter string
		want         []string
	}{
		{"all, in the order of the document", "", []string{"Alpha", "Beta", "local", "Gamma"}},
		{"a fuzzy filter", "ga", []string{"Gamma"}},
		{"nothing matches", "zzz", nil},
		{"by kind", ":", []string{"functions (2)", "Alpha", "Beta", "variables (1)", "local", "structs (1)", "Gamma"}},
		{"by kind, filtered", ":al", []string{"functions (1)", "Alpha", "variables (1)", "local"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, it := range symbolItems(syms, tc.filter, open) {
				got = append(got, it.Label)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("items = %v, want %v", got, tc.want)
			}
		})
	}
	items := symbolItems(syms, "", open)
	if items[2].Description != "Beta" {
		t.Errorf("the container of the nested symbol is %q", items[2].Description)
	}
	items[0].Accept(quickinput.KeyMods{})
	if !slices.Equal(opened, []string{"Alpha"}) {
		t.Errorf("opened = %v", opened)
	}
}

func TestWorkspaceSymbolItems(t *testing.T) {
	syms := []langfeatures.WorkspaceSymbol{
		{Name: "xAlphaLong", Kind: 12, Location: langfeatures.Location{URI: document.FileURI("/proj/a.go")}},
		{Name: "Alpha", Kind: 12, ContainerName: "pkg", Location: langfeatures.Location{URI: document.FileURI("/proj/sub/b.go")}},
	}
	var opened []string
	var sides []bool
	items := workspaceSymbolItems(syms, "Alpha", "/proj", func(s langfeatures.WorkspaceSymbol, side bool) {
		opened = append(opened, s.Name)
		sides = append(sides, side)
	})
	if len(items) != 2 || items[0].Label != "Alpha" || items[0].Detail != "sub/b.go" || items[0].Description != "pkg" {
		t.Fatalf("items = %+v", items)
	}
	items[0].Accept(quickinput.KeyMods{})
	items[1].Accept(quickinput.KeyMods{Ctrl: true})
	if !slices.Equal(opened, []string{"Alpha", "xAlphaLong"}) || !slices.Equal(sides, []bool{false, true}) {
		t.Errorf("opened = %v, sides = %v", opened, sides)
	}
}

func TestOutlineFollowsTheCursorAndTheEditor(t *testing.T) {
	a := navApp(t)
	out := a.shell().explorer.outline
	if out.HasSymbols() {
		t.Fatal("the outline has symbols before a provider answered")
	}
	a.withSymbols(navSymbols())
	a.waitFor("the outline", out.HasSymbols)
	if out.Editor() != a.vm() {
		t.Fatal("the outline shows another editor")
	}
	tree := out.Tree()
	cur := func() string {
		k, _ := tree.Selected()
		return k
	}
	a.vm().Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: 5, Col: 3}, Active: buffer.Position{Line: 5, Col: 3}}})
	a.vm().SelectionsChanged()
	if got := cur(); !strings.HasSuffix(got, "local@13") {
		t.Errorf("selected %q with the cursor in the local variable", got)
	}
	a.vm().Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: 8, Col: 1}, Active: buffer.Position{Line: 8, Col: 1}}})
	a.vm().SelectionsChanged()
	if got := cur(); got != "/Gamma@23" {
		t.Errorf("selected %q", got)
	}
	// turned off, it stays
	out.SetFollowing(false)
	a.vm().Cursors().SetSelections([]buffer.Selection{{Anchor: buffer.Position{Line: 2, Col: 1}, Active: buffer.Position{Line: 2, Col: 1}}})
	a.vm().SelectionsChanged()
	if got := cur(); got != "/Gamma@23" {
		t.Errorf("selected %q while not following", got)
	}
	// choosing a symbol goes to it
	out.SetFollowing(true)
	out.Choose("/Beta@12")
	if got := a.vm().Cursors().Primary().Position(); got != (buffer.Position{Line: 4, Col: 5}) {
		t.Errorf("cursor = %v", got)
	}
	// another editor
	a.show("b.go", false)
	if out.Editor() != a.vm() || out.HasSymbols() {
		t.Errorf("the outline shows the editor of a.go, symbols %v", out.HasSymbols())
	}
}

func TestOutlineViewShowsTheSymbolsAndTheMessage(t *testing.T) {
	a := navApp(t)
	a.win.Views.Open(command.ViewContainerExplorer)
	a.win.Views.SetCollapsed(command.ViewOutline, false)
	a.pump()
	if !a.ts.Exists(gluttertest.ByText(outlineview.NoSymbols)) {
		t.Error("the message of an editor with no symbols is not shown")
	}
	a.withSymbols(navSymbols())
	a.pump()
	if !a.ts.Exists(gluttertest.ByKey(outlineview.TreeKey)) {
		t.Fatal("the tree of symbols is not shown")
	}
	a.run("outline.collapse")
	a.run("outline.followCursor")
	if a.shell().explorer.outline.Following() {
		t.Error("outline.followCursor did not turn the following off")
	}
	a.noErrors()
}

func TestDirLabel(t *testing.T) {
	win := newTestWorkspace(t, "")
	win.Folder = "/work/proj"
	home, _ := os.UserHomeDir()
	for _, tc := range []struct{ dir, want string }{
		{"/work/proj", ""},
		{"/work/proj/internal/x", "internal/x"},
		{"/work/other", "/work/other"},
		{"/work/projection", "/work/projection"},
		{home, "~"},
		{filepath.Join(home, "go", "pkg"), "~/go/pkg"},
	} {
		if got := win.dirLabel(tc.dir); got != tc.want {
			t.Errorf("dirLabel(%q) = %q, want %q", tc.dir, got, tc.want)
		}
	}
}

// wantTokens checks that the embedded editor of a Go file has the tokens of the grammar of Go, as the editors of the tabs have.
func (a *groupApp) wantTokens(vm *editor.ViewModel, what string) {
	a.t.Helper()
	tok := vm.Tokenizer()
	if tok == nil || !tok.Active() {
		a.t.Fatalf("%s has no tokenizer for the language %q", what, vm.Document().LanguageID())
	}
	if !tok.Ensure(2, 5*time.Second) || len(tok.Line(2)) < 3 {
		a.t.Errorf("%s: the tokens of line 3 (func Alpha() {}) are %v", what, tok.Line(2))
	}
}
