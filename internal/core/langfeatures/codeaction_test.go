package langfeatures_test

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	. "github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/core/lsp/lsptest"
)

func TestKindContains(t *testing.T) {
	tests := []struct {
		parent, child       string
		contains, intersect bool
	}{
		{"refactor", "refactor", true, true},
		{"refactor", "refactor.extract", true, true},
		{"refactor.extract", "refactor", false, true},
		{"refactor", "refactorx", false, false},
		{"source", "source.organizeImports", true, true},
		{"quickfix", "source", false, false},
		{"", "anything", true, true},
		{"quickfix", "", false, true},
	}
	for _, tc := range tests {
		if got := KindContains(tc.parent, tc.child); got != tc.contains {
			t.Errorf("KindContains(%q, %q) = %v", tc.parent, tc.child, got)
		}
		if got := KindIntersects(tc.parent, tc.child); got != tc.intersect {
			t.Errorf("KindIntersects(%q, %q) = %v", tc.parent, tc.child, got)
		}
	}
}

// The cases of VS Code's codeAction.test.ts and types: the filter decides which actions and which providers count.
func TestCodeActionFilter(t *testing.T) {
	tests := []struct {
		name      string
		filter    CodeActionFilter
		kind      string
		preferred bool
		filters   bool
		may       bool
	}{
		{"no filter takes a quick fix", CodeActionFilter{}, KindQuickFix, false, true, true},
		{"no filter takes an action without a kind", CodeActionFilter{}, "", false, true, true},
		{"source actions are left out unless asked for", CodeActionFilter{}, KindSourceOrganize, false, false, false},
		{"source actions asked for", CodeActionFilter{IncludeSourceActions: true}, KindSourceOrganize, false, true, true},
		{"include a parent kind", CodeActionFilter{Include: Kind(KindRefactor)}, KindRefactorExtract, false, true, true},
		{"include a child kind does not take the parent", CodeActionFilter{Include: Kind(KindRefactorExtract)}, KindRefactor, false, false, true},
		{"include wants a kind", CodeActionFilter{Include: Kind("")}, "", false, false, true},
		{"include another kind", CodeActionFilter{Include: Kind(KindQuickFix)}, KindRefactor, false, false, false},
		{"exclude", CodeActionFilter{Excludes: []string{KindRefactor}}, KindRefactorInline, false, false, false},
		{"exclude does not cover a more specific include", CodeActionFilter{Include: Kind(KindRefactorExtract), Excludes: []string{KindRefactor}}, KindRefactorExtract, false, true, true},
		{"only preferred", CodeActionFilter{OnlyPreferred: true}, KindQuickFix, false, false, true},
		{"only preferred takes a preferred", CodeActionFilter{OnlyPreferred: true}, KindQuickFix, true, true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.filter.Filters(CodeAction{Kind: tc.kind, Preferred: tc.preferred}); got != tc.filters {
				t.Errorf("Filters = %v, want %v", got, tc.filters)
			}
			if tc.kind != "" {
				if got := tc.filter.MayInclude(tc.kind); got != tc.may {
					t.Errorf("MayInclude = %v, want %v", got, tc.may)
				}
			}
		})
	}
}

func TestCodeActionSetOrder(t *testing.T) {
	diag := []Diagnostic{{Message: "m"}}
	set := NewCodeActionSet([]CodeAction{
		{Title: "plain"},
		{Title: "preferred", Preferred: true},
		{Title: "fix", Diagnostics: diag},
		{Title: "disabled fix", Diagnostics: diag, Disabled: "no"},
		{Title: "preferred fix", Diagnostics: diag, Preferred: true, Kind: KindQuickFix},
	})
	var all, valid []string
	for _, a := range set.All {
		all = append(all, a.Title)
	}
	for _, a := range set.Valid {
		valid = append(valid, a.Title)
	}
	if want := []string{"preferred fix", "fix", "disabled fix", "preferred", "plain"}; !slices.Equal(all, want) {
		t.Errorf("all = %v, want %v", all, want)
	}
	if want := []string{"preferred fix", "fix", "preferred", "plain"}; !slices.Equal(valid, want) {
		t.Errorf("valid = %v, want %v", valid, want)
	}
	if !set.HasAutoFix() {
		t.Error("a preferred quick fix is an auto fix")
	}
	if NewCodeActionSet([]CodeAction{{Title: "p", Preferred: true, Kind: KindRefactor}}).HasAutoFix() {
		t.Error("a preferred refactoring is no auto fix")
	}
}

// kindProvider answers the actions it is given and says which kinds it answers.
type kindProvider struct {
	fakeProvider
	kinds []string
	asked *CodeActionRequest
}

func (k *kindProvider) CodeActionKinds() []string { return k.kinds }
func (k *kindProvider) ProvideCodeActions(_ context.Context, _ Document, req CodeActionRequest) ([]CodeAction, error) {
	k.asked = &req
	return k.actions, k.err
}

func TestGetCodeActionsFiltersAndSorts(t *testing.T) {
	doc := docOf("go", "/work/a.go", "x\n")
	reg := NewRegistry()
	fixer := &kindProvider{kinds: []string{KindQuickFix, KindSourceOrganize}, fakeProvider: fakeProvider{actions: []CodeAction{
		{Title: "organize", Kind: KindSourceOrganize},
		{Title: "fix", Kind: KindQuickFix},
		{Title: "extract", Kind: KindRefactorExtract},
		{Title: "nokind"},
	}}}
	refactorer := &kindProvider{kinds: []string{KindRefactor}, fakeProvider: fakeProvider{actions: []CodeAction{{Title: "inline", Kind: KindRefactorInline}}}}
	reg.RegisterCodeActions("fixer", Selector{}, fixer)
	reg.RegisterCodeActions("refactorer", Selector{}, refactorer)

	titles := func(set CodeActionSet) []string {
		var out []string
		for _, a := range set.All {
			out = append(out, a.Title)
		}
		slices.Sort(out)
		return out
	}
	// the light bulb: every kind but the source ones, an automatic request with no `only`
	set, errs := reg.GetCodeActions(context.Background(), doc, CodeActionRequest{}, CodeActionTrigger{Type: TriggerAutomatic, Source: SourceLightbulb})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if want := []string{"extract", "fix", "inline", "nokind"}; !slices.Equal(titles(set), want) {
		t.Errorf("automatic = %v, want %v", titles(set), want)
	}
	if fixer.asked == nil || !fixer.asked.Automatic || len(fixer.asked.Only) != 0 {
		t.Errorf("request = %+v", fixer.asked)
	}
	// Refactor...: only the refactor kinds; the provider of quick fixes is not asked
	fixer.asked, refactorer.asked = nil, nil
	set, _ = reg.GetCodeActions(context.Background(), doc, CodeActionRequest{}, CodeActionTrigger{Type: TriggerInvokedAction, Filter: CodeActionFilter{Include: Kind(KindRefactor)}})
	if want := []string{"inline"}; !slices.Equal(titles(set), want) {
		t.Errorf("refactor = %v, want %v", titles(set), want)
	}
	if fixer.asked != nil || refactorer.asked == nil || !slices.Equal(refactorer.asked.Only, []string{KindRefactor}) || refactorer.asked.Automatic {
		t.Errorf("fixer = %+v, refactorer = %+v", fixer.asked, refactorer.asked)
	}
	// Organize Imports
	set, _ = reg.GetCodeActions(context.Background(), doc, CodeActionRequest{}, CodeActionTrigger{Type: TriggerInvokedAction,
		Filter: CodeActionFilter{Include: Kind(KindSourceOrganize), IncludeSourceActions: true}})
	if want := []string{"organize"}; !slices.Equal(titles(set), want) {
		t.Errorf("organize = %v, want %v", titles(set), want)
	}
	// the context keys
	if got := reg.CodeActionKinds(doc); !slices.Equal(got, []string{KindRefactor, KindQuickFix, KindSourceOrganize}) {
		t.Errorf("kinds = %v", got)
	}
	for kind, want := range map[string]bool{KindRefactor: true, KindQuickFix: true, KindSource: true, KindSourceOrganize: true, KindSourceFixAll: false, KindRefactorExtract: false} {
		if got := reg.SupportsCodeActionKind(doc, kind); got != want {
			t.Errorf("SupportsCodeActionKind(%q) = %v, want %v", kind, got, want)
		}
	}
}

func TestGetCodeActionsKeepsTheOthersWhenOneFails(t *testing.T) {
	doc := docOf("go", "/work/a.go", "x\n")
	reg := NewRegistry()
	reg.RegisterCodeActions("bad", Selector{}, &kindProvider{fakeProvider: fakeProvider{err: context.DeadlineExceeded}})
	reg.RegisterCodeActions("good", Selector{}, &kindProvider{fakeProvider: fakeProvider{actions: []CodeAction{{Title: "fix"}}}})
	set, errs := reg.GetCodeActions(context.Background(), doc, CodeActionRequest{}, CodeActionTrigger{Type: TriggerInvokedAction})
	if len(set.All) != 1 || set.All[0].Owner != "good" || len(errs) != 1 || errs[0].Owner != "bad" {
		t.Errorf("set = %+v, errors = %+v", set, errs)
	}
}

func TestServerCodeActionsResolveAndKinds(t *testing.T) {
	ctx := context.Background()
	doc := docOf("go", "/work/a.go", emojiLine)
	caps := `{"codeActionProvider":{"codeActionKinds":["quickfix","source.organizeImports"],"resolveProvider":true}}`
	c := connect(t, caps, map[string]lsptest.Response{
		lsp.MethodCodeAction: result(`[
			{"title":"has edit","kind":"quickfix","edit":{"changes":{"file:///work/a.go":[{"range":` + emojiRange + `,"newText":"z"}]}}},
			{"title":"lazy","kind":"quickfix","isPreferred":true,"data":{"id":7}},
			{"title":"off","kind":"quickfix","disabled":{"reason":"not here"}}]`),
		lsp.MethodCodeActionResolve: result(`{"title":"lazy","kind":"quickfix","edit":{"changes":{"file:///work/a.go":[{"range":` + emojiRange + `,"newText":"w"}]}}}`),
	})
	reg := NewRegistry()
	c.sp.Register(reg, "emetos.test", Selector{Languages: []string{"go"}})

	if got := c.sp.CodeActionKinds(); !slices.Equal(got, []string{"quickfix", "source.organizeImports"}) {
		t.Errorf("kinds = %v", got)
	}
	if !reg.SupportsCodeActionKind(doc, KindSourceOrganize) || reg.SupportsCodeActionKind(doc, KindRefactor) {
		t.Error("the supported kinds are those of the server")
	}
	set, errs := reg.GetCodeActions(ctx, doc, CodeActionRequest{Range: rng(0, 5, 0, 6)}, CodeActionTrigger{Type: TriggerInvokedAction, Filter: CodeActionFilter{Include: Kind(KindQuickFix)}})
	if len(errs) != 0 || len(set.All) != 3 || len(set.Valid) != 2 || set.All[0].Title != "lazy" || set.All[2].Disabled != "not here" {
		t.Fatalf("set = %+v, %v", set, errs)
	}
	var params struct {
		Context struct {
			Only        []string `json:"only"`
			TriggerKind int      `json:"triggerKind"`
		} `json:"context"`
	}
	if err := json.Unmarshal(c.requests(lsp.MethodCodeAction)[0], &params); err != nil || !slices.Equal(params.Context.Only, []string{"quickfix"}) || params.Context.TriggerKind != lsp.CodeActionTriggerInvoked {
		t.Errorf("params = %+v, %v", params, err)
	}

	// the action with an edit is not resolved; the lazy one is, and gets its edit
	var hasEdit, lazy CodeAction
	for _, a := range set.All {
		switch a.Title {
		case "has edit":
			hasEdit = a
		case "lazy":
			lazy = a
		}
	}
	if reg.CanResolveCodeAction(hasEdit) != true {
		t.Error("the server could resolve the action, it just does not need to")
	}
	if got, err := reg.ResolveCodeAction(ctx, hasEdit); err != nil || got.Edit == nil || len(c.requests(lsp.MethodCodeActionResolve)) != 0 {
		t.Errorf("resolve of an action with an edit = %+v, %v (%d requests)", got, err, len(c.requests(lsp.MethodCodeActionResolve)))
	}
	if lazy.Edit != nil || !reg.CanResolveCodeAction(lazy) {
		t.Fatalf("lazy = %+v", lazy)
	}
	got, err := reg.ResolveCodeAction(ctx, lazy)
	if err != nil || got.Edit == nil {
		t.Fatalf("resolve = %+v, %v", got, err)
	}
	reqs := c.requests(lsp.MethodCodeActionResolve)
	if len(reqs) != 1 || !strings.Contains(string(reqs[0]), `"data":{"id":7}`) {
		t.Errorf("the resolve request = %s (the data goes back to the server)", reqs)
	}
	expanded, err := ExpandEdit(ctx, got.Edit, func(context.Context, document.URI) (lsp.Lines, error) { return doc.Text, nil })
	if err != nil || len(expanded.Changes) != 1 || expanded.Changes[0].Edits[0].Text != "w" || expanded.Changes[0].Edits[0].Range != rng(0, 5, 0, 6) {
		t.Errorf("resolved edit = %+v, %v", expanded, err)
	}
}

func TestServerResolveWithoutTheCapability(t *testing.T) {
	ctx := context.Background()
	doc := docOf("go", "/work/a.go", emojiLine)
	c := connect(t, `{"codeActionProvider":true}`, map[string]lsptest.Response{lsp.MethodCodeAction: result(`[{"title":"lazy","kind":"quickfix"}]`)})
	reg := NewRegistry()
	c.sp.Register(reg, "emetos.test", Selector{})
	set, _ := reg.GetCodeActions(ctx, doc, CodeActionRequest{}, CodeActionTrigger{Type: TriggerAutomatic})
	if len(set.All) != 1 || reg.CanResolveCodeAction(set.All[0]) {
		t.Fatalf("set = %+v", set)
	}
	if got, err := reg.ResolveCodeAction(ctx, set.All[0]); err != nil || got.Edit != nil {
		t.Errorf("resolve = %+v, %v", got, err)
	}
	if reg.Has(doc, FeatureRename) {
		t.Error("the server has no rename")
	}
	if !reg.Has(doc, FeatureCodeActions) {
		t.Error("the server has code actions")
	}
}

func TestMinimalEdits(t *testing.T) {
	tests := []struct {
		name string
		text string
		edit TextEdit
		want []TextEdit
	}{
		{"no change", "abc\n", TextEdit{Range: rng(0, 0, 0, 3), Text: "abc"}, nil},
		{"common start and end", "hello world\n", TextEdit{Range: rng(0, 0, 0, 11), Text: "hello brave world"}, []TextEdit{{Range: rng(0, 6, 0, 6), Text: "brave "}}},
		{"insertion stays", "a\n", TextEdit{Range: rng(0, 1, 0, 1), Text: "b"}, []TextEdit{{Range: rng(0, 1, 0, 1), Text: "b"}}},
		{"non-ASCII keeps characters whole", "x😀y\n", TextEdit{Range: rng(0, 0, 0, 6), Text: "x😁y"}, []TextEdit{{Range: rng(0, 1, 0, 5), Text: "😁"}}},
		{"whole document replaced by almost the same", "a\nb\nc\nd\n", TextEdit{Range: rng(0, 0, 4, 0), Text: "a\nB\nc\nD\n"},
			[]TextEdit{{Range: rng(1, 0, 1, 1), Text: "B"}, {Range: rng(3, 0, 3, 1), Text: "D"}}},
		{"lines added at the end", "a\nb", TextEdit{Range: rng(0, 0, 1, 1), Text: "a\nb\nc\nd"}, []TextEdit{{Range: rng(1, 1, 1, 1), Text: "\nc\nd"}}},
		{"a line removed in the middle", "a\nb\nc\nd\ne\n", TextEdit{Range: rng(0, 0, 5, 0), Text: "a\nb\nd\ne\n"}, []TextEdit{{Range: rng(2, 0, 3, 0), Text: ""}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			snap := buffer.New(tc.text).Snapshot()
			got := MinimalEdits(snap, []TextEdit{tc.edit})
			if !equalEdits(got, tc.want) {
				t.Errorf("edits = %+v, want %+v", got, tc.want)
			}
			// whatever they are, the edits give the same text
			if applied, want := applyEdits(t, tc.text, got), applyEdits(t, tc.text, []TextEdit{tc.edit}); applied != want {
				t.Errorf("the edits make %q, the original makes %q", applied, want)
			}
		})
	}
}

func equalEdits(a, b []TextEdit) bool {
	return slices.EqualFunc(a, b, func(x, y TextEdit) bool { return x == y })
}

// applyEdits applies edits that do not overlap to a text.
func applyEdits(t *testing.T, text string, edits []TextEdit) string {
	t.Helper()
	snap := buffer.New(text).Snapshot()
	type span struct {
		s, e int
		text string
	}
	var spans []span
	for _, e := range edits {
		spans = append(spans, span{snap.OffsetAt(snap.ValidatePosition(e.Range.Start)), snap.OffsetAt(snap.ValidatePosition(e.Range.End)), e.Text})
	}
	slices.SortFunc(spans, func(a, b span) int { return a.s - b.s })
	var out strings.Builder
	at := 0
	for _, s := range spans {
		out.WriteString(text[at:s.s])
		out.WriteString(s.text)
		at = s.e
	}
	out.WriteString(text[at:])
	return out.String()
}

func TestModifiedRanges(t *testing.T) {
	base := strings.Split("a\nb\nc\nd\ne", "\n")
	text := strings.Split("a\nB\nc\nx\ny\nd\ne", "\n")
	got, err := ModifiedRanges(context.Background(), base, text)
	want := []Range{rng(1, 0, 1, 1), rng(3, 0, 4, 1)}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("ranges = %+v, %v, want %+v", got, err, want)
	}
	// deleted lines make no range
	got, err = ModifiedRanges(context.Background(), base, strings.Split("a\nb\nd\ne", "\n"))
	if err != nil || len(got) != 0 {
		t.Errorf("ranges after a deletion = %+v, %v", got, err)
	}
	// no change
	if got, _ := ModifiedRanges(context.Background(), base, base); len(got) != 0 {
		t.Errorf("ranges of an unchanged text = %+v", got)
	}
}

func TestFormatOnTypeThroughTheServer(t *testing.T) {
	ctx := context.Background()
	doc := docOf("go", "/work/a.go", emojiLine)
	c := connect(t, `{"documentOnTypeFormattingProvider":{"firstTriggerCharacter":"}","moreTriggerCharacter":[";","\n"]}}`, map[string]lsptest.Response{
		lsp.MethodOnTypeFormatting: result(`[{"range":` + emojiRange + `,"newText":"q"}]`),
	})
	reg := NewRegistry()
	c.sp.Register(reg, "emetos.test", Selector{Languages: []string{"go"}})
	if got := reg.OnTypeFormattingTriggers(doc); !slices.Equal(got, []string{"}", ";", "\n"}) {
		t.Errorf("triggers = %q", got)
	}
	if !reg.Has(doc, FeatureOnTypeFormatting) {
		t.Error("the server formats on type")
	}
	edits, err := reg.FormatOnType(ctx, doc, Position{Line: 0, Col: 6}, ";", FormattingOptions{TabSize: 4, InsertSpaces: true})
	if err != nil || len(edits) != 1 || edits[0].Text != "q" || edits[0].Range != rng(0, 5, 0, 6) {
		t.Fatalf("edits = %+v, %v", edits, err)
	}
	var p struct {
		Ch       string       `json:"ch"`
		Position lsp.Position `json:"position"`
	}
	if err := json.Unmarshal(c.requests(lsp.MethodOnTypeFormatting)[0], &p); err != nil || p.Ch != ";" || p.Position.Character != 4 {
		t.Errorf("params = %+v, %v (the column is the UTF-16 offset)", p, err)
	}
	// a character that is no trigger asks nothing
	if edits, err := reg.FormatOnType(ctx, doc, Position{}, "x", FormattingOptions{}); err != nil || edits != nil || len(c.requests(lsp.MethodOnTypeFormatting)) != 1 {
		t.Errorf("edits for a non-trigger = %+v, %v", edits, err)
	}
}

func TestFormatDocumentWithARangeFormatterOnly(t *testing.T) {
	ctx := context.Background()
	doc := docOf("go", "/work/a.go", "a\nb\n")
	c := connect(t, `{"documentRangeFormattingProvider":true}`, map[string]lsptest.Response{
		lsp.MethodRangeFormatting: result(`[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}},"newText":"A"}]`),
	})
	reg := NewRegistry()
	c.sp.Register(reg, "emetos.test", Selector{})
	if !reg.Has(doc, FeatureFormatting) || !reg.Has(doc, FeatureRangeFormatting) {
		t.Fatal("a range formatter formats the document too")
	}
	edits, err := reg.FormatDocument(ctx, doc, FormattingOptions{}, "")
	if err != nil || len(edits) != 1 || edits[0].Text != "A" {
		t.Fatalf("edits = %+v, %v", edits, err)
	}
	var p lsp.DocumentRangeFormattingParams
	if err := json.Unmarshal(c.requests(lsp.MethodRangeFormatting)[0], &p); err != nil || p.Range.End.Line != 2 || p.Range.End.Character != 0 {
		t.Errorf("the range = %+v, %v (the whole text)", p.Range, err)
	}
	// a server with neither
	d := connect(t, `{"hoverProvider":true}`, nil)
	reg2 := NewRegistry()
	d.sp.Register(reg2, "emetos.test", Selector{})
	if reg2.Has(doc, FeatureFormatting) || reg2.Has(doc, FeatureRangeFormatting) {
		t.Error("a server that does not format has no formatter")
	}
	_, err = reg2.FormatDocument(ctx, doc, FormattingOptions{}, "")
	routeOf(t, err, "Registry.FormatDocument.NoFormatter")
}

func TestExpandAndFilterEdit(t *testing.T) {
	ctx := context.Background()
	texts := func(_ context.Context, uri document.URI) (lsp.Lines, error) {
		return buffer.New("x😀y\nsecond\n").Snapshot(), nil
	}
	one := 3
	we := FromLSPEdit(lsp.UTF16, lsp.WorkspaceEdit{DocumentChanges: []lsp.DocumentChange{
		{TextDocument: &lsp.OptionalVersionedTextDocumentIdentifier{URI: "file:///work/a.go", Version: &one}, Edits: []lsp.TextEdit{
			{Range: lsp.Range{Start: lsp.Position{Character: 3}, End: lsp.Position{Character: 4}}, NewText: "z"},
			{Range: lsp.Range{Start: lsp.Position{Line: 1, Character: 0}, End: lsp.Position{Line: 1, Character: 6}}, NewText: "2nd"},
		}},
		{Kind: lsp.ChangeRenameFile, OldURI: "file:///work/a.go", NewURI: "file:///work/b.go"},
	}})
	we.Label = "Rename"
	exp, err := ExpandEdit(ctx, we, texts)
	if err != nil || exp.Label != "Rename" || len(exp.Changes) != 2 {
		t.Fatalf("expanded = %+v, %v", exp, err)
	}
	c0 := exp.Changes[0]
	if c0.Version == nil || *c0.Version != 3 || len(c0.Edits) != 2 || c0.Edits[0].Range != rng(0, 5, 0, 6) || c0.Edits[1].Range != rng(1, 0, 1, 6) {
		t.Errorf("text change = %+v (the columns are bytes now)", c0)
	}
	if exp.Changes[1].Kind != ChangeRename || exp.Changes[1].NewURI.Path != "/work/b.go" {
		t.Errorf("file change = %+v", exp.Changes[1])
	}
	// leave out the second edit and the rename
	kept := FilterEdit(exp, func(change, edit int) bool { return (change != 0 || edit != 1) && change != 1 })
	if len(kept.Changes) != 1 || len(kept.Changes[0].Edits) != 1 || kept.Changes[0].Edits[0].Text != "z" || kept.Label != "Rename" {
		t.Errorf("kept = %+v", kept)
	}
	// every edit of a document left out: the change goes
	none := FilterEdit(exp, func(change, edit int) bool { return change == 1 })
	if len(none.Changes) != 1 || none.Changes[0].Kind != ChangeRename {
		t.Errorf("none = %+v", none)
	}
	// without texts the columns are taken as they are
	raw, err := ExpandEdit(ctx, we, nil)
	if err != nil || raw.Changes[0].Edits[0].Range != rng(0, 3, 0, 4) {
		t.Errorf("without texts = %+v, %v", raw, err)
	}
	// a file that cannot be read
	_, err = ExpandEdit(ctx, we, func(context.Context, document.URI) (lsp.Lines, error) { return nil, context.DeadlineExceeded })
	routeOf(t, err, "ExpandEdit.WorkspaceEditDocumentUnavailable")
	if _, err := ExpandEdit(ctx, nil, texts); err == nil {
		t.Error("a nil edit expands")
	}
}
