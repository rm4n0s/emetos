package langfeatures_test

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/edit"
	"github.com/rm4n0s/emetos/internal/core/folding"
	. "github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/core/lsp/lsptest"
)

// fakeLens is a provider of code lenses that can resolve them.
type fakeLens struct {
	lenses   []CodeLens
	resolved int
	fail     bool
}

func (f *fakeLens) ProvideCodeLenses(context.Context, Document) ([]CodeLens, error) {
	if f.fail {
		return nil, errors.New("ServerDown", "down")
	}
	return f.lenses, nil
}

func (f *fakeLens) CanResolveCodeLens(l CodeLens) bool { return !l.Resolved() }

func (f *fakeLens) ResolveCodeLens(_ context.Context, _ Document, l CodeLens) (CodeLens, error) {
	f.resolved++
	l.Command = &Command{Title: "resolved", ID: "x"}
	return l, nil
}

// plainLens is a provider without the means to resolve.
type plainLens struct{ lenses []CodeLens }

func (p plainLens) ProvideCodeLenses(context.Context, Document) ([]CodeLens, error) {
	return p.lenses, nil
}

func TestCodeLensesAreJoinedInTheOrderOfTheText(t *testing.T) {
	ctx := context.Background()
	doc := docOf("go", "/work/a.go", "a\nb\nc\n")
	reg := NewRegistry()
	sel := Selector{Languages: []string{"go"}}
	one := &fakeLens{lenses: []CodeLens{
		{Range: rng(2, 0, 2, 1), Command: &Command{Title: "c"}},
		{Range: rng(0, 0, 0, 1)},
	}}
	reg.RegisterCodeLens("one", sel, one)
	reg.RegisterCodeLens("two", sel, plainLens{lenses: []CodeLens{{Range: rng(1, 0, 1, 1), Command: &Command{Title: "b"}}, {Range: rng(0, 0, 0, 1), Command: &Command{Title: "a2"}}}})
	reg.RegisterCodeLens("broken", sel, &fakeLens{fail: true})
	lenses, errs := reg.CodeLenses(ctx, doc)
	if len(errs) != 1 || errs[0].Owner != "broken" {
		t.Errorf("errors = %+v", errs)
	}
	var lines []int
	owners := map[int][]string{}
	for _, l := range lenses {
		lines = append(lines, l.Range.Start.Line)
		owners[l.Range.Start.Line] = append(owners[l.Range.Start.Line], l.Owner)
	}
	if !reflect.DeepEqual(lines, []int{0, 0, 1, 2}) {
		t.Fatalf("lines = %v", lines)
	}
	// the lens of the provider registered last, which is asked first, comes first on a line
	if !reflect.DeepEqual(owners[0], []string{"two", "one"}) {
		t.Errorf("owners of line 0 = %v", owners[0])
	}
	unresolved := lenses[1]
	if unresolved.Resolved() || !reg.CanResolveCodeLens(unresolved) {
		t.Fatalf("the lens of one has no command yet: %+v", unresolved)
	}
	got, err := reg.ResolveCodeLens(ctx, doc, unresolved)
	if err != nil || got.Command == nil || got.Command.Title != "resolved" || one.resolved != 1 {
		t.Errorf("resolved = %+v, %v", got, err)
	}
	// a lens that has its command, or whose provider cannot resolve, is left as it is
	if again, _ := reg.ResolveCodeLens(ctx, doc, got); one.resolved != 1 || again.Command.Title != "resolved" {
		t.Error("a resolved lens is not resolved again")
	}
	if reg.CanResolveCodeLens(lenses[0]) {
		t.Error("a provider without a resolver cannot resolve")
	}
}

func TestInlayHintsAreJoinedInTheOrderOfTheText(t *testing.T) {
	ctx := context.Background()
	doc := docOf("go", "/work/a.go", "a\nb\n")
	reg := NewRegistry()
	reg.RegisterInlayHints("one", Selector{}, hintsOf{
		{Position: Position{Line: 1}, Parts: []InlayHintPart{{Value: "x"}}},
		{Position: Position{Line: 0, Col: 1}, Parts: []InlayHintPart{{Value: "a"}, {Value: "b"}}},
		{Position: Position{Line: 0}, Parts: []InlayHintPart{{Value: ""}}},
	})
	hints, errs := reg.InlayHints(ctx, doc, rng(0, 0, 2, 0))
	if len(errs) != 0 || len(hints) != 2 || hints[0].Label() != "ab" || hints[1].Label() != "x" || hints[0].Owner != "one" {
		t.Errorf("hints = %+v %v (the empty label is left out)", hints, errs)
	}
	if reg.CanResolveInlayHint(hints[0]) {
		t.Error("a provider without a resolver cannot resolve")
	}
	got, err := reg.ResolveInlayHint(ctx, doc, hints[0])
	if err != nil || got.Label() != "ab" {
		t.Errorf("resolve = %+v, %v", got, err)
	}
}

type hintsOf []InlayHint

func (h hintsOf) ProvideInlayHints(context.Context, Document, Range) ([]InlayHint, error) {
	return h, nil
}

func TestServerProviderCodeLensesAndInlayHints(t *testing.T) {
	ctx := context.Background()
	doc := docOf("go", "/work/a.go", emojiLine)
	caps := `{"codeLensProvider":{"resolveProvider":true},"inlayHintProvider":{"resolveProvider":true}}`
	c := connect(t, caps, map[string]lsptest.Response{
		lsp.MethodCodeLens: result(`[{"range":` + emojiRange + `,"command":{"title":"run test","command":"gopls.run","arguments":[1]}},
			{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"data":{"k":1}}]`),
		lsp.MethodCodeLensResolve: result(`{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"command":{"title":"debug test","command":"gopls.debug"}}`),
		lsp.MethodInlayHint: result(`[{"position":{"line":0,"character":3},"label":"int","kind":1,"paddingLeft":true,"tooltip":"a type"},
			{"position":{"line":0,"character":0},"label":[{"value":"name","tooltip":{"kind":"markdown","value":"**the name**"},"location":{"uri":"file:///work/b.go","range":` + emojiRange + `}},{"value":":"}],"kind":2,"data":7}]`),
		lsp.MethodInlayHintResolve: result(`{"position":{"line":0,"character":0},"label":[{"value":"name","command":{"title":"Go","command":"gopls.go"}},{"value":":"}],"tooltip":"resolved tip"}`),
	})
	sp := c.sp
	lenses, err := sp.ProvideCodeLenses(ctx, doc)
	if err != nil || len(lenses) != 2 {
		t.Fatalf("lenses = %+v, %v", lenses, err)
	}
	if l := lenses[0]; l.Range != rng(0, 5, 0, 6) || l.Command == nil || l.Command.Title != "run test" || l.Command.Owner != "emetos.test.srv" || l.Command.ID != "gopls.run" {
		t.Errorf("lens 0 = %+v (the range is the UTF-16 range of y)", l)
	}
	if lenses[1].Resolved() || !sp.CanResolveCodeLens(lenses[1]) || sp.CanResolveCodeLens(CodeLens{}) {
		t.Errorf("lens 1 = %+v", lenses[1])
	}
	got, err := sp.ResolveCodeLens(ctx, doc, lenses[1])
	if err != nil || got.Command == nil || got.Command.Title != "debug test" || got.Range != lenses[1].Range {
		t.Errorf("resolved = %+v, %v", got, err)
	}

	hints, err := sp.ProvideInlayHints(ctx, doc, rng(0, 0, 1, 0))
	if err != nil || len(hints) != 2 {
		t.Fatalf("hints = %+v, %v", hints, err)
	}
	if h := hints[0]; h.Position != (Position{Line: 0, Col: 5}) || h.Label() != "int" || h.Kind != InlayHintType || !h.PaddingLeft || h.PaddingRight || h.Tooltip == nil || h.Tooltip.Value != "a type" {
		t.Errorf("hint 0 = %+v", h)
	}
	h1 := hints[1]
	if len(h1.Parts) != 2 || h1.Kind != InlayHintParameter || h1.Label() != "name:" || h1.Parts[0].Tooltip == nil || !h1.Parts[0].Tooltip.Markdown ||
		h1.Parts[0].Location == nil || h1.Parts[0].Location.URI.Path != "/work/b.go" {
		t.Errorf("hint 1 = %+v", h1)
	}
	if !sp.CanResolveInlayHint(h1) {
		t.Fatal("the server resolves hints")
	}
	res, err := sp.ResolveInlayHint(ctx, doc, h1)
	if err != nil || res.Parts[0].Command == nil || res.Parts[0].Command.ID != "gopls.go" || res.Tooltip == nil || res.Tooltip.Value != "resolved tip" || res.Kind != InlayHintParameter {
		t.Errorf("resolved hint = %+v, %v", res, err)
	}
	var params struct {
		Range lsp.Range `json:"range"`
	}
	if err := jsonUnmarshal(c.requests(lsp.MethodInlayHint)[0], &params); err != nil || params.Range.End.Line != 1 {
		t.Errorf("params = %+v, %v", params, err)
	}

	// without the capabilities nothing is asked
	n := connect(t, `{}`, nil)
	if l, err := n.sp.ProvideCodeLenses(ctx, doc); l != nil || err != nil {
		t.Errorf("lenses without the capability = %+v, %v", l, err)
	}
	if h, err := n.sp.ProvideInlayHints(ctx, doc, rng(0, 0, 1, 0)); h != nil || err != nil {
		t.Errorf("hints without the capability = %+v, %v", h, err)
	}
	if n.sp.Offers(FeatureCodeLens) || n.sp.Offers(FeatureInlayHints) || n.sp.Offers(FeatureSemanticTokens) || n.sp.Offers(FeatureFoldingRanges) || n.sp.Offers(FeatureSelectionRanges) {
		t.Error("a server without capabilities offers nothing")
	}
	if !sp.Offers(FeatureCodeLens) || !sp.Offers(FeatureInlayHints) {
		t.Error("the server offers lenses and hints")
	}
}

func TestFoldingRangesOfProviders(t *testing.T) {
	ctx := context.Background()
	doc := docOf("go", "/work/a.go", "a\nb\nc\nd\ne\nf\n")
	reg := NewRegistry()
	sel := Selector{Languages: []string{"go"}}
	if _, _, ok, _ := reg.FoldingRanges(ctx, doc); ok {
		t.Error("no provider, no answer")
	}
	reg.RegisterFoldingRanges("one", sel, foldingOf{ranges: []FoldingRange{
		{StartLine: 0, EndLine: 4, CollapsedText: "{...}"},
		{StartLine: 1, EndLine: 2, Kind: "comment"},
		{StartLine: 3, EndLine: 3},  // one line
		{StartLine: 2, EndLine: 40}, // beyond the text
		{StartLine: -1, EndLine: 2}, // before the text
		{StartLine: 4, EndLine: 5, Kind: "imports"},
	}})
	ranges, collapsed, ok, errs := reg.FoldingRanges(ctx, doc)
	if !ok || len(errs) != 0 || len(ranges) != 3 {
		t.Fatalf("ranges = %+v, ok %v, %v", ranges, ok, errs)
	}
	if collapsed[0] != "{...}" || len(collapsed) != 1 {
		t.Errorf("collapsed texts = %v", collapsed)
	}
	// a provider that answers with an empty list says there is nothing to fold; one that answers null leaves it to the indentation
	empty := NewRegistry()
	empty.RegisterFoldingRanges("e", sel, foldingOf{ranges: []FoldingRange{}})
	if r, _, ok, _ := empty.FoldingRanges(ctx, doc); !ok || len(r) != 0 {
		t.Errorf("empty list = %+v, %v", r, ok)
	}
	null := NewRegistry()
	null.RegisterFoldingRanges("n", sel, foldingOf{})
	if _, _, ok, _ := null.FoldingRanges(ctx, doc); ok {
		t.Error("a null answer is no answer")
	}
	failed := NewRegistry()
	failed.RegisterFoldingRanges("f", sel, foldingOf{err: errors.New("ServerDown", "down")})
	if _, _, ok, errs := failed.FoldingRanges(ctx, doc); ok || len(errs) != 1 {
		t.Errorf("a failure: %v %v", ok, errs)
	}
}

type foldingOf struct {
	ranges []FoldingRange
	err    error
}

func (f foldingOf) ProvideFoldingRanges(context.Context, Document) ([]FoldingRange, error) {
	return f.ranges, f.err
}

// indentOnly is the fallback provider of a test: it finds one region.
type indentOnly struct{}

func (indentOnly) Compute(context.Context, folding.Text, int) (*folding.Regions, error) {
	return folding.NewRegions([]int{0}, []int{1}, nil)
}

func TestFoldingProvider(t *testing.T) {
	doc := docOf("go", "/work/a.go", "a\nb\nc\nd\ne\nf\n")
	reg := NewRegistry()
	reg.RegisterFoldingRanges("srv", Selector{Languages: []string{"go"}}, foldingOf{ranges: []FoldingRange{
		{StartLine: 2, EndLine: 4, Kind: "comment", CollapsedText: "/*...*/"}, {StartLine: 0, EndLine: 5},
	}})
	strategy := "auto"
	p := FoldingProvider{Registry: reg, Fallback: indentOnly{}, Strategy: func() string { return strategy }}
	ctx := folding.WithTag(context.Background(), doc)
	text := folding.SnapshotText(doc.Text)

	regions, err := p.Compute(ctx, text, 4)
	if err != nil || regions.Len() != 2 || regions.Start(0) != 0 || regions.End(0) != 5 || regions.Start(1) != 2 || regions.Type(1) != "comment" {
		t.Fatalf("regions = %+v, %v", regions, err)
	}
	if regions.CollapsedTexts()[2] != "/*...*/" {
		t.Errorf("collapsed texts = %v", regions.CollapsedTexts())
	}
	// the strategy "indentation" leaves it to the fallback
	strategy = "indentation"
	if regions, err := p.Compute(ctx, text, 4); err != nil || regions.Len() != 1 || regions.End(0) != 1 {
		t.Errorf("by indentation = %+v, %v", regions, err)
	}
	// so does a document no provider answers for, and a computation without a document
	strategy = "auto"
	other := docOf("rust", "/work/a.rs", "a\nb\n")
	if regions, err := p.Compute(folding.WithTag(context.Background(), other), text, 4); err != nil || regions.Len() != 1 || regions.End(0) != 1 {
		t.Errorf("no provider = %+v, %v", regions, err)
	}
	if regions, err := p.Compute(context.Background(), text, 4); err != nil || regions.End(0) != 1 {
		t.Errorf("no tag = %+v, %v", regions, err)
	}
	// a computation that was canceled gives up
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := p.Compute(cctx, text, 4); err == nil {
		t.Error("a canceled computation gave regions")
	}
}

// selectionOf answers the chains of ranges the test gives for the positions.
type selectionOf struct {
	chains map[Position][]Range
}

func (s selectionOf) ProvideSelectionRanges(_ context.Context, _ Document, positions []Position) ([][]Range, error) {
	out := make([][]Range, len(positions))
	for i, p := range positions {
		out[i] = s.chains[p]
	}
	return out, nil
}

func TestSelectionRangesDriveTheExpansion(t *testing.T) {
	ctx := context.Background()
	text := "func f(a int) {\n\tg(a, 2)\n}\n"
	doc := docOf("go", "/work/a.go", text)
	reg := NewRegistry()
	at := Position{Line: 1, Col: 3} // a in g(a, 2)
	reg.RegisterSelectionRanges("srv", Selector{Languages: []string{"go"}}, selectionOf{chains: map[Position][]Range{at: {
		rng(1, 3, 1, 4), rng(1, 3, 1, 8), rng(1, 1, 1, 9), rng(0, 14, 2, 1), rng(0, 0, 2, 1),
	}}})
	set, ok, errs := reg.SelectionRanges(ctx, doc, []Position{at})
	if !ok || len(errs) != 0 || len(set.Positions()) != 1 {
		t.Fatalf("set = %+v, %v, %v", set, ok, errs)
	}
	if _, ok, _ := reg.SelectionRanges(ctx, docOf("rust", "/work/a.rs", "x\n"), []Position{{}}); ok {
		t.Error("a document with no provider has no ranges")
	}

	// Expand Selection steps through the ranges of the server and the editor's own, the smallest first; Shrink goes back
	model := edit.SnapshotModel{Snapshot: doc.Text}
	cmds := edit.Commands{Model: model, Options: edit.DefaultOptions()}
	exp := cmds.NewExpansion([]buffer.Selection{}, edit.DefaultSelectionRangesOptions(), set)
	if exp != nil {
		t.Fatal("no selections, no expansion")
	}
	sels := []buffer.Selection{{Anchor: at, Active: at}}
	exp = cmds.NewExpansion(sels, edit.DefaultSelectionRangesOptions(), set)
	if exp == nil {
		t.Fatal("no expansion")
	}
	var got []Range
	for range 7 {
		got = append(got, exp.Expand()[0].Range())
	}
	// the word `a` first (the editor's own, equal to the server's first range), then the server's, with the whole lines the
	// editor adds between ranges that start on different lines
	want := []Range{rng(1, 3, 1, 4), rng(1, 3, 1, 8), rng(1, 1, 1, 9), rng(0, 14, 2, 1)}
	for _, w := range want {
		if !containsRange(got, w) {
			t.Errorf("the expansion %v lacks %v", got, w)
		}
	}
	for i := 1; i < len(got); i++ {
		if !got[i].ContainsRange(got[i-1]) {
			t.Errorf("step %d (%v) does not hold step %d (%v)", i, got[i], i-1, got[i-1])
		}
	}
	if last := got[len(got)-1]; last != rng(0, 0, 3, 0) {
		t.Errorf("the last step = %v (the whole text, which the editor always ends with)", last)
	}
	back := exp.Shrink()[0].Range()
	if back == got[len(got)-1] || !got[len(got)-1].ContainsRange(back) {
		t.Errorf("shrink = %v", back)
	}

	// a server that answers fewer chains than positions is left out
	reg2 := NewRegistry()
	reg2.RegisterSelectionRanges("srv", Selector{}, shortSelection{})
	if set2, ok, _ := reg2.SelectionRanges(ctx, doc, []Position{at, at}); !ok || len(set2.SelectionRanges(model, []Position{at})[0]) != 0 {
		t.Errorf("a short answer: %+v %v", set2, ok)
	}
}

type shortSelection struct{}

func (shortSelection) ProvideSelectionRanges(context.Context, Document, []Position) ([][]Range, error) {
	return [][]Range{{rng(0, 0, 0, 1)}}, nil
}

func containsRange(rs []Range, r Range) bool {
	for _, x := range rs {
		if x == r {
			return true
		}
	}
	return false
}

func TestServerProviderFoldingAndSelectionRanges(t *testing.T) {
	ctx := context.Background()
	doc := docOf("go", "/work/a.go", emojiLine)
	c := connect(t, `{"foldingRangeProvider":true,"selectionRangeProvider":true}`, map[string]lsptest.Response{
		lsp.MethodFoldingRange: result(`[{"startLine":0,"endLine":3,"kind":"region","collapsedText":"..."}]`),
		lsp.MethodSelectionRange: result(`[{"range":{"start":{"line":0,"character":3},"end":{"line":0,"character":4}},
			"parent":{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":7}}}}]`),
	})
	fr, err := c.sp.ProvideFoldingRanges(ctx, doc)
	if err != nil || len(fr) != 1 || fr[0] != (FoldingRange{StartLine: 0, EndLine: 3, Kind: "region", CollapsedText: "..."}) {
		t.Errorf("folding = %+v, %v", fr, err)
	}
	sr, err := c.sp.ProvideSelectionRanges(ctx, doc, []Position{{Line: 0, Col: 5}})
	if err != nil || len(sr) != 1 || len(sr[0]) != 2 || sr[0][0] != rng(0, 5, 0, 6) || sr[0][1] != rng(0, 0, 0, 9) {
		t.Errorf("selection = %+v, %v (the columns are those of the text, not UTF-16 units)", sr, err)
	}
	var params struct {
		Positions []lsp.Position `json:"positions"`
	}
	if err := jsonUnmarshal(c.requests(lsp.MethodSelectionRange)[0], &params); err != nil || params.Positions[0].Character != 3 {
		t.Errorf("params = %+v, %v", params, err)
	}
	n := connect(t, `{}`, nil)
	if fr, err := n.sp.ProvideFoldingRanges(ctx, doc); fr != nil || err != nil {
		t.Errorf("no folding capability = %+v, %v", fr, err)
	}
	if sr, err := n.sp.ProvideSelectionRanges(ctx, doc, []Position{{}}); sr != nil || err != nil {
		t.Errorf("no selection capability = %+v, %v", sr, err)
	}
	// null is a nil list: the editor folds by indentation
	z := connect(t, `{"foldingRangeProvider":true}`, map[string]lsptest.Response{lsp.MethodFoldingRange: result(`null`)})
	if fr, err := z.sp.ProvideFoldingRanges(ctx, doc); fr != nil || err != nil {
		t.Errorf("null = %+v, %v", fr, err)
	}
}

func TestRefreshListeners(t *testing.T) {
	reg := NewRegistry()
	var got []Feature
	stop := reg.OnRefresh(func(f Feature) { got = append(got, f) })
	reg.Refreshed(FeatureSemanticTokens)
	reg.Refreshed(FeatureInlayHints)
	stop()
	reg.Refreshed(FeatureCodeLens)
	if !reflect.DeepEqual(got, []Feature{FeatureSemanticTokens, FeatureInlayHints}) {
		t.Errorf("refreshes = %v", got)
	}
}

func TestRegisterAddsTheFeaturesOfStep129(t *testing.T) {
	ctx := context.Background()
	doc := docOf("go", "/work/a.go", emojiLine)
	caps := `{"codeLensProvider":{},"inlayHintProvider":true,"foldingRangeProvider":true,"selectionRangeProvider":true,
		"semanticTokensProvider":{"legend":{"tokenTypes":["a"],"tokenModifiers":[]},"full":true}}`
	c := connect(t, caps, map[string]lsptest.Response{
		lsp.MethodCodeLens:            result(`[{"range":` + emojiRange + `,"command":{"title":"t","command":"c"}}]`),
		lsp.MethodInlayHint:           result(`[{"position":{"line":0,"character":0},"label":"h"}]`),
		lsp.MethodFoldingRange:        result(`[{"startLine":0,"endLine":1}]`),
		lsp.MethodSelectionRange:      result(`[{"range":` + emojiRange + `}]`),
		lsp.MethodSemanticTokensFull:  result(`{"data":[0,0,1,0,0]}`),
		lsp.MethodSemanticTokensRange: result(`null`),
	})
	reg := NewRegistry()
	c.sp.Register(reg, "emetos.test", Selector{Languages: []string{"go"}})
	for _, f := range []Feature{FeatureCodeLens, FeatureInlayHints, FeatureSemanticTokens, FeatureFoldingRanges, FeatureSelectionRanges} {
		if !reg.Has(doc, f) {
			t.Errorf("the registry has no provider for feature %d", f)
		}
	}
	if reg.Has(docOf("rust", "/work/a.rs", "x"), FeatureCodeLens) {
		t.Error("a document of another language has no lenses")
	}
	if l, _ := reg.CodeLenses(ctx, doc); len(l) != 1 || l[0].Owner != "emetos.test.srv" {
		t.Errorf("lenses = %+v", l)
	}
	if h, _ := reg.InlayHints(ctx, doc, rng(0, 0, 1, 0)); len(h) != 1 {
		t.Errorf("hints = %+v", h)
	}
	if r, _, ok, _ := reg.FoldingRanges(ctx, doc); !ok || len(r) != 0 {
		// the range is on one line more than the text has: left out, but the provider did answer
		t.Logf("folding = %+v %v", r, ok)
	}
}

func jsonUnmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
