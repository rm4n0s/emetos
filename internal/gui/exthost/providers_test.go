package exthost

import (
	"context"
	"slices"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/core/lsp/lsptest"
)

// The providers an extension writes in Go (step 12.3).

// allProviders implements every provider interface of package extension.
type allProviders struct {
	hover func() (*extension.Hover, error)
	seen  *extension.TextDocument
}

var (
	_ extension.CompletionProvider        = (*allProviders)(nil)
	_ extension.HoverProvider             = (*allProviders)(nil)
	_ extension.SignatureHelpProvider     = (*allProviders)(nil)
	_ extension.DefinitionProvider        = (*allProviders)(nil)
	_ extension.ReferenceProvider         = (*allProviders)(nil)
	_ extension.DocumentSymbolProvider    = (*allProviders)(nil)
	_ extension.DocumentHighlightProvider = (*allProviders)(nil)
	_ extension.CodeActionProvider        = (*allProviders)(nil)
	_ extension.FormattingProvider        = (*allProviders)(nil)
	_ extension.RangeFormattingProvider   = (*allProviders)(nil)
	_ extension.RenameProvider            = (*allProviders)(nil)
)

func (p *allProviders) ProvideCompletion(_ context.Context, doc extension.TextDocument, pos extension.Position, tr extension.CompletionTrigger) (extension.CompletionList, error) {
	p.seen = &doc
	return extension.CompletionList{Incomplete: true, Items: []extension.CompletionItem{{
		Label: "Println", Detail: tr.Kind + tr.Character, Documentation: &extension.Markup{Value: "d", Markdown: true}, Kind: extension.CompletionFunction,
		Snippet: true, InsertText: "Println($0)", Range: &extension.Range{Start: pos, End: pos},
		AdditionalEdits: []extension.RangeEdit{{Text: "import"}}, Command: &extension.Command{ID: "c"},
	}}}, nil
}

func (p *allProviders) ProvideHover(context.Context, extension.TextDocument, extension.Position) (*extension.Hover, error) {
	if p.hover != nil {
		return p.hover()
	}
	return &extension.Hover{Contents: []extension.Markup{{Value: "go hover"}}, Range: &extension.Range{End: extension.Position{Col: 3}}}, nil
}

func (p *allProviders) ProvideSignatureHelp(context.Context, extension.TextDocument, extension.Position) (*extension.SignatureHelp, error) {
	return &extension.SignatureHelp{ActiveParameter: 1, Signatures: []extension.Signature{{Label: "f(a, b)", Parameters: []extension.Parameter{{Label: "a"}, {Label: "b"}}}}}, nil
}

func (p *allProviders) ProvideDefinition(context.Context, extension.TextDocument, extension.Position) ([]extension.Location, error) {
	return []extension.Location{{URI: "file:///work/d.go", Range: extension.Range{Start: extension.Position{Line: 2}, End: extension.Position{Line: 2, Col: 4}}}}, nil
}

func (p *allProviders) ProvideReferences(_ context.Context, _ extension.TextDocument, _ extension.Position, decl bool) ([]extension.Location, error) {
	if !decl {
		return nil, nil
	}
	return []extension.Location{{URI: "file:///work/r.go"}, {URI: "nonsense"}}, nil
}

func (p *allProviders) ProvideDocumentSymbols(context.Context, extension.TextDocument) ([]extension.DocumentSymbol, error) {
	return []extension.DocumentSymbol{{Name: "T", Kind: 23, Children: []extension.DocumentSymbol{{Name: "f", Kind: 8}}}}, nil
}

func (p *allProviders) ProvideDocumentHighlights(context.Context, extension.TextDocument, extension.Position) ([]extension.DocumentHighlight, error) {
	return []extension.DocumentHighlight{{Kind: 1}, {Kind: 3}}, nil
}

func (p *allProviders) ProvideCodeActions(_ context.Context, _ extension.TextDocument, req extension.CodeActionRequest) ([]extension.CodeAction, error) {
	return []extension.CodeAction{
		{Title: "Fix", Kind: "quickfix", Preferred: true, Diagnostics: req.Diagnostics, Edit: &extension.WorkspaceEdit{Changes: []extension.ResourceChange{
			{Kind: "edit", URI: "file:///work/a.go", Edits: []extension.RangeEdit{{Text: "x"}}},
			{Kind: "rename", URI: "file:///work/a.go", NewURI: "file:///work/b.go"},
		}}},
		{Title: "Bad", Edit: &extension.WorkspaceEdit{Changes: []extension.ResourceChange{{Kind: "explode", URI: "file:///work/a.go"}}}},
	}, nil
}

func (p *allProviders) ProvideFormatting(context.Context, extension.TextDocument, extension.FormattingOptions) ([]extension.RangeEdit, error) {
	return []extension.RangeEdit{{Text: "doc"}}, nil
}

func (p *allProviders) ProvideRangeFormatting(context.Context, extension.TextDocument, extension.Range, extension.FormattingOptions) ([]extension.RangeEdit, error) {
	return []extension.RangeEdit{{Text: "range"}}, nil
}

func (p *allProviders) PrepareRename(_ context.Context, _ extension.TextDocument, pos extension.Position) (*extension.PrepareRenameResult, error) {
	if pos.Line == 9 {
		return nil, errors.New("KeywordRename", "cannot rename a keyword")
	}
	return &extension.PrepareRenameResult{Range: extension.Range{End: extension.Position{Col: 1}}, Placeholder: "x"}, nil
}

func (p *allProviders) ProvideRename(context.Context, extension.TextDocument, extension.Position, string) (*extension.WorkspaceEdit, error) {
	return &extension.WorkspaceEdit{Changes: []extension.ResourceChange{{URI: "file:///work/a.go", Edits: []extension.RangeEdit{{Text: "n"}}}}}, nil
}

// register registers p for every feature.
func (p *allProviders) register(l extension.Languages, sel extension.DocumentSelector) error {
	regs := []func() (func(), error){
		func() (func(), error) { return l.RegisterCompletionProvider(sel, p, ".") },
		func() (func(), error) { return l.RegisterHoverProvider(sel, p) },
		func() (func(), error) { return l.RegisterSignatureHelpProvider(sel, p, "(") },
		func() (func(), error) { return l.RegisterDefinitionProvider(sel, p) },
		func() (func(), error) { return l.RegisterReferenceProvider(sel, p) },
		func() (func(), error) { return l.RegisterDocumentSymbolProvider(sel, p) },
		func() (func(), error) { return l.RegisterDocumentHighlightProvider(sel, p) },
		func() (func(), error) { return l.RegisterCodeActionProvider(sel, p) },
		func() (func(), error) { return l.RegisterFormattingProvider(sel, p) },
		func() (func(), error) { return l.RegisterRenameProvider(sel, p) },
	}
	for _, r := range regs {
		if _, err := r(); err != nil {
			return err
		}
	}
	return nil
}

func goDocument(text string) langfeatures.Document {
	return langfeatures.Document{URI: document.FileURI("/work/one/a.go"), LanguageID: "go", Version: 3, Text: buffer.New(text).Snapshot()}
}

// providerTab makes a tab of an extension that registers the providers in Activate.
func providerTab(t *testing.T, id string, activate func(a extension.API) error) (*rig, *tab) {
	t.Helper()
	f, _ := factory(manifest(id), func(f *fake) {
		f.activate = func(_ context.Context, a extension.API) error { return activate(a) }
	})
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.Start()
	return r, tb
}

func TestGoProvidersAnswerEveryFeature(t *testing.T) {
	p := &allProviders{}
	_, tb := providerTab(t, "gofeat", func(a extension.API) error {
		return p.register(a.Languages(), extension.DocumentSelector{Languages: []string{"go"}})
	})
	ctx := context.Background()
	f := tb.scope.Features()
	doc := goDocument("package a\n")
	pos := langfeatures.Position{Line: 0, Col: 4}

	list, errs := f.Completion(ctx, doc, pos, langfeatures.CompletionTrigger{Kind: langfeatures.TriggerCharacter, Character: "."})
	if len(errs) != 0 || len(list.Items) != 1 || !list.Incomplete {
		t.Fatalf("completion = %+v %v", list, errs)
	}
	it := list.Items[0]
	if it.Label != "Println" || it.Detail != "character." || !it.Snippet || it.InsertText != "Println($0)" || it.Kind != langfeatures.CompletionKind(extension.CompletionFunction) ||
		it.Insert == nil || it.Insert.Start != pos || len(it.AdditionalEdits) != 1 || it.Command == nil || it.Command.Owner != "gofeat" || it.Owner != "gofeat" || it.Documentation == nil || !it.Documentation.Markdown {
		t.Errorf("item = %+v", it)
	}
	if p.seen == nil || p.seen.URI != "file:///work/one/a.go" || p.seen.Text != "package a\n" || p.seen.Version != 3 || p.seen.LanguageID != "go" {
		t.Errorf("the provider saw %+v", p.seen)
	}
	if tr := f.CompletionTriggers(doc); !slices.Equal(tr, []string{"."}) {
		t.Errorf("triggers = %v", tr)
	}
	if tr := f.SignatureTriggers(doc); !slices.Equal(tr, []string{"("}) {
		t.Errorf("signature triggers = %v", tr)
	}

	hs, _ := f.Hover(ctx, doc, pos)
	if len(hs) != 1 || hs[0].Contents[0].Value != "go hover" || hs[0].Range == nil || hs[0].Range.End.Col != 3 {
		t.Errorf("hover = %+v", hs)
	}
	sh, _ := f.SignatureHelp(ctx, doc, pos, langfeatures.SignatureContext{})
	if sh == nil || sh.ActiveParameter != 1 || sh.Signatures[0].Parameters[1].Label != "b" {
		t.Errorf("signature help = %+v", sh)
	}
	links, _ := f.Locations(ctx, doc, pos, langfeatures.NavDefinition)
	if len(links) != 1 || links[0].Target.URI.Path != "/work/d.go" || links[0].Target.Range.Start.Line != 2 {
		t.Errorf("definition = %+v", links)
	}
	if links, _ := f.Locations(ctx, doc, pos, langfeatures.NavImplementation); len(links) != 0 {
		t.Errorf("implementations = %+v", links)
	}
	// a location with a URI nobody knows fails that provider, which is told in the errors
	if refs, errs := f.References(ctx, doc, pos, true); len(refs) != 0 || len(errs) != 1 {
		t.Errorf("references = %+v %v", refs, errs)
	}
	if refs, errs := f.References(ctx, doc, pos, false); len(refs) != 0 || len(errs) != 0 {
		t.Errorf("references without the declaration = %+v %v", refs, errs)
	}
	syms, _ := f.DocumentSymbols(ctx, doc)
	if len(syms) != 1 || syms[0].Name != "T" || len(syms[0].Children) != 1 {
		t.Errorf("symbols = %+v", syms)
	}
	hl, _ := f.DocumentHighlights(ctx, doc, pos)
	if len(hl) != 2 || hl[0].Kind != langfeatures.HighlightText || hl[1].Kind != langfeatures.HighlightWrite {
		t.Errorf("highlights = %+v", hl)
	}

	// the code actions: the one with an edit of a kind that does not exist fails the provider as a whole
	_, errs = f.CodeActions(ctx, doc, langfeatures.CodeActionRequest{})
	if len(errs) != 1 {
		t.Errorf("code action errors = %v", errs)
	}
	assertRoute(t, errs[0].Err, "ExtensionProviderFailed")
	assertRoute(t, errors.Unwrap(errs[0].Err), "fromWorkspaceEdit.ExtensionEditInvalid")

	edits, err := f.FormatDocument(ctx, doc, langfeatures.FormattingOptions{}, "gofeat")
	if err != nil || len(edits) != 1 || edits[0].Text != "doc" {
		t.Errorf("format = %+v %v", edits, err)
	}
	edits, err = f.FormatRange(ctx, doc, langfeatures.Range{}, langfeatures.FormattingOptions{}, "")
	if err != nil || len(edits) != 1 || edits[0].Text != "range" {
		t.Errorf("format range = %+v %v", edits, err)
	}
	prep, err := f.PrepareRename(ctx, doc, pos)
	if err != nil || prep == nil || prep.Placeholder != "x" || prep.Range.End.Col != 1 {
		t.Errorf("prepare = %+v %v", prep, err)
	}
	if _, err := f.PrepareRename(ctx, doc, langfeatures.Position{Line: 9}); err == nil {
		t.Error("a refusal was not an error")
	} else {
		assertRoute(t, err, "ExtensionProviderFailed")
	}
	we, err := f.Rename(ctx, doc, pos, "n")
	if err != nil || we == nil || len(we.Changes) != 1 || we.Changes[0].Kind != langfeatures.ChangeText || we.Changes[0].URI.Path != "/work/a.go" {
		t.Errorf("rename = %+v %v", we, err)
	}
}

func TestCodeActionsOfAGoProviderWithGoodEdits(t *testing.T) {
	_, tb := providerTab(t, "gofix", func(a extension.API) error {
		_, err := a.Languages().RegisterCodeActionProvider(extension.DocumentSelector{}, goodActions{})
		return err
	})
	acts, errs := tb.scope.Features().CodeActions(context.Background(), goDocument(""), langfeatures.CodeActionRequest{
		Diagnostics: []langfeatures.Diagnostic{{Message: "m", Severity: langfeatures.SeverityWarning}},
	})
	if len(errs) != 0 || len(acts) != 1 {
		t.Fatalf("actions = %+v %v", acts, errs)
	}
	a := acts[0]
	if a.Title != "Fix" || !a.Preferred || a.Owner != "gofix" || len(a.Diagnostics) != 1 || a.Diagnostics[0].Severity != langfeatures.SeverityWarning || a.Edit == nil || len(a.Edit.Changes) != 3 {
		t.Errorf("action = %+v", a)
	}
	kinds := []langfeatures.ChangeKind{a.Edit.Changes[0].Kind, a.Edit.Changes[1].Kind, a.Edit.Changes[2].Kind}
	if !slices.Equal(kinds, []langfeatures.ChangeKind{langfeatures.ChangeText, langfeatures.ChangeRename, langfeatures.ChangeDelete}) || a.Edit.Changes[1].NewURI.Path != "/work/b.go" {
		t.Errorf("changes = %+v", a.Edit.Changes)
	}
}

type goodActions struct{}

func (goodActions) ProvideCodeActions(_ context.Context, _ extension.TextDocument, req extension.CodeActionRequest) ([]extension.CodeAction, error) {
	return []extension.CodeAction{{Title: "Fix", Preferred: true, Diagnostics: req.Diagnostics, Edit: &extension.WorkspaceEdit{Changes: []extension.ResourceChange{
		{Kind: "edit", URI: "file:///work/a.go", Edits: []extension.RangeEdit{{Text: "x"}}},
		{Kind: "rename", URI: "file:///work/a.go", NewURI: "file:///work/b.go"},
		{Kind: "delete", URI: "file:///work/c.go"},
	}}}}, nil
}

func TestAGoProviderAndAServerAreMerged(t *testing.T) {
	script := lsptest.Script{
		Capabilities: raw(`{"textDocumentSync":2,"hoverProvider":true,"definitionProvider":true}`),
		Responses: map[string]lsptest.Response{
			lsp.MethodHover:      {Result: raw(`{"contents":{"kind":"markdown","value":"from the server"}}`)},
			lsp.MethodDefinition: {Result: raw(`{"uri":"file:///work/s.go","range":{"start":{"line":1,"character":0},"end":{"line":1,"character":2}}}`)},
		},
	}
	st := newServerTest(t, script, nil)
	st.onActivate = func(_ context.Context, a extension.API) error {
		return (&allProviders{}).register(a.Languages(), extension.DocumentSelector{Languages: []string{"go"}})
	}
	tb := st.startTab("one")
	d := st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)
	doc := langfeatures.Document{URI: d.URI(), LanguageID: "go", Version: d.Version(), Text: d.Snapshot()}
	hs, errs := tb.scope.Features().Hover(context.Background(), doc, langfeatures.Position{})
	if len(errs) != 0 || len(hs) != 2 {
		t.Fatalf("hovers = %+v %v", hs, errs)
	}
	// both are the extension's: the server registered last, so it comes first
	if hs[0].Contents[0].Value != "from the server" || hs[1].Contents[0].Value != "go hover" {
		t.Errorf("hovers = %+v", hs)
	}
	// the positions of a location in another file are converted with its text, which is taken on the UI thread: the call
	// runs on a goroutine while the test pumps, as the UI thread does
	type answer struct {
		links []langfeatures.LocationLink
		errs  []langfeatures.ProviderError
	}
	done := make(chan answer, 1)
	go func() {
		l, e := tb.scope.Features().Locations(context.Background(), doc, langfeatures.Position{}, langfeatures.NavDefinition)
		done <- answer{l, e}
	}()
	var got answer
	waitFor(t, tb, "the definitions", func() bool {
		select {
		case got = <-done:
			return true
		default:
			return false
		}
	})
	links, errs := got.links, got.errs
	var paths []string
	for _, l := range links {
		paths = append(paths, l.Target.URI.Path)
	}
	if len(errs) != 0 || !slices.Equal(paths, []string{"/work/s.go", "/work/d.go"}) {
		t.Errorf("definitions = %v %v", paths, errs)
	}
}

func TestAPanicInAGoProviderFailsOnlyThatExtensionInThatTab(t *testing.T) {
	bad, _ := factory(manifest("bad"), func(f *fake) {
		f.activate = func(_ context.Context, a extension.API) error {
			p := &allProviders{hover: func() (*extension.Hover, error) {
				if a.Scope().Name == "one" {
					panic("provider boom")
				}
				return &extension.Hover{Contents: []extension.Markup{{Value: "fine"}}}, nil
			}}
			return p.register(a.Languages(), extension.DocumentSelector{Languages: []string{"go"}})
		}
	})
	good, _ := factory(manifest("good"), func(f *fake) {
		f.activate = func(_ context.Context, a extension.API) error {
			_, err := a.Languages().RegisterHoverProvider(extension.DocumentSelector{}, &allProviders{hover: func() (*extension.Hover, error) {
				return &extension.Hover{Contents: []extension.Markup{{Value: "good"}}}, nil
			}})
			return err
		}
	})
	r := newRig(t, bad, good)
	one, two := r.newTab("one"), r.newTab("two")
	one.deferred = true
	for _, tb := range []*tab{one, two} {
		tb.scope.Start()
	}
	doc := goDocument("package a\n")
	hs, errs := one.scope.Features().Hover(context.Background(), doc, langfeatures.Position{})
	// the answer of the panicking provider is dropped, the other extension's stays
	if len(hs) != 1 || hs[0].Contents[0].Value != "good" || len(errs) != 1 || errs[0].Owner != "bad" {
		t.Fatalf("hovers %+v errors %+v", hs, errs)
	}
	assertRoute(t, errs[0].Err, "guardProvider.func1.ExtensionProviderPanicked")
	one.pump() // the failure is made on the UI thread
	if one.scope.State("bad") != StateFailed || one.scope.State("good") != StateActive {
		t.Fatalf("states in the first tab: bad %v good %v", one.scope.State("bad"), one.scope.State("good"))
	}
	f := one.scope.Failure("bad")
	if f == nil {
		t.Fatal("no failure recorded")
	}
	assertRoute(t, f.Err, "guardProvider.func1.ExtensionProviderPanicked")
	if two.scope.State("bad") != StateActive {
		t.Errorf("the second tab: bad is %v", two.scope.State("bad"))
	}
	// the providers of the failed extension are gone in the first tab and work in the second
	if hs, errs := one.scope.Features().Hover(context.Background(), doc, langfeatures.Position{}); len(hs) != 1 || len(errs) != 0 {
		t.Errorf("hovers after the failure = %+v %v", hs, errs)
	}
	if hs, _ := two.scope.Features().Hover(context.Background(), doc, langfeatures.Position{}); len(hs) != 2 {
		t.Errorf("hovers in the second tab = %+v", hs)
	}
	if n := len(one.notifications()); n != 1 {
		t.Errorf("notifications = %v", one.notifications())
	}
}

func TestProviderRegistrationErrorsAndLifetime(t *testing.T) {
	var api extension.API
	_, tb := providerTab(t, "reg", func(a extension.API) error { api = a; return nil })
	l := api.Languages()
	_, err := l.RegisterHoverProvider(extension.DocumentSelector{}, nil)
	assertRoute(t, err, "register.ExtensionProviderInvalid")

	fail := &allProviders{hover: func() (*extension.Hover, error) { return nil, errors.New("ProviderFailure", "it failed") }}
	dispose, err := l.RegisterHoverProvider(extension.DocumentSelector{Languages: []string{"go"}}, fail)
	if err != nil {
		t.Fatal(err)
	}
	doc := goDocument("")
	_, errs := tb.scope.Features().Hover(context.Background(), doc, langfeatures.Position{})
	if len(errs) != 1 {
		t.Fatalf("errors = %v", errs)
	}
	assertRoute(t, errs[0].Err, "guardProvider.ExtensionProviderFailed")
	if tb.scope.State("reg") != StateActive {
		t.Error("an error of a provider failed the extension")
	}
	dispose()
	if _, errs := tb.scope.Features().Hover(context.Background(), doc, langfeatures.Position{}); len(errs) != 0 {
		t.Errorf("the provider answered after it was disposed of: %v", errs)
	}

	// a provider that was called after the activation ended is refused, and the registration is refused too
	live := &allProviders{}
	if _, err := l.RegisterHoverProvider(extension.DocumentSelector{}, live); err != nil {
		t.Fatal(err)
	}
	tb.scope.Close()
	_, err = l.RegisterHoverProvider(extension.DocumentSelector{}, live)
	assertRoute(t, err, "register.ExtensionDeactivated")
	_, errs = tb.scope.Features().Hover(context.Background(), doc, langfeatures.Position{})
	if len(errs) != 0 {
		t.Errorf("errors = %v", errs)
	}
}

func TestAGuardedCallAfterDeactivationIsRefused(t *testing.T) {
	var api extension.API
	var inst *instance
	_, tb := providerTab(t, "late", func(a extension.API) error { api = a; return nil })
	_ = api
	tb.scope.mu.Lock()
	inst = tb.scope.slots[0].inst
	tb.scope.mu.Unlock()
	inst.kill()
	err := tb.scope.guardProvider(inst, "hover", func() error { return nil })
	assertRoute(t, err, "guardProvider.ExtensionDeactivated")
}

func assertRoute(t *testing.T, err error, route string) {
	t.Helper()
	mustRoute(t, err, route)
}

// lensProvider answers two lenses on the first line: one that runs and one that is disabled.
type lensProvider struct {
	seen  *extension.TextDocument
	fail  bool
	panic bool
}

func (p *lensProvider) ProvideCodeLenses(_ context.Context, doc extension.TextDocument) ([]extension.CodeLens, error) {
	p.seen = &doc
	if p.panic {
		panic("lens boom")
	}
	if p.fail {
		return nil, errors.New("LensFailure", "it failed")
	}
	run, err := extension.NewCommand("run test", "go.runTest", map[string]any{"name": "TestA"})
	if err != nil {
		return nil, err
	}
	return []extension.CodeLens{
		{Range: extension.Range{Start: extension.Position{Line: 2}, End: extension.Position{Line: 2, Col: 5}}, Command: run},
		{Range: extension.Range{Start: extension.Position{Line: 2}, End: extension.Position{Line: 2}}, Command: extension.Command{Title: "debug test"}, Disabled: true},
	}, nil
}

func TestACodeLensProviderGivesLensesWithCommandsOfTheExtension(t *testing.T) {
	p := &lensProvider{}
	var api extension.API
	_, tb := providerTab(t, "golens", func(a extension.API) error {
		api = a
		_, err := a.Languages().RegisterCodeLensProvider(extension.DocumentSelector{Languages: []string{"go"}}, p)
		return err
	})
	doc := goDocument("package a\n\nfunc TestA(t *testing.T) {}\n")
	lenses, errs := tb.scope.Features().CodeLenses(context.Background(), doc)
	if len(errs) != 0 || len(lenses) != 2 {
		t.Fatalf("lenses = %+v %v", lenses, errs)
	}
	run, dbg := lenses[0], lenses[1]
	if run.Range.Start.Line != 2 || run.Command == nil || run.Command.Title != "run test" || run.Command.ID != "go.runTest" || run.Command.Owner != "golens" || run.Command.Disabled || run.Owner != "golens" {
		t.Errorf("run lens = %+v %+v", run, run.Command)
	}
	if len(run.Command.Arguments) != 1 || string(run.Command.Arguments[0]) != `{"name":"TestA"}` {
		t.Errorf("arguments = %s", run.Command.Arguments)
	}
	if dbg.Command == nil || dbg.Command.Title != "debug test" || !dbg.Command.Disabled {
		t.Errorf("debug lens = %+v", dbg.Command)
	}
	if p.seen == nil || p.seen.URI != "file:///work/one/a.go" || p.seen.LanguageID != "go" {
		t.Errorf("the provider saw %+v", p.seen)
	}
	// a document of another language is not the provider's
	other := doc
	other.LanguageID = "rust"
	if lenses, _ := tb.scope.Features().CodeLenses(context.Background(), other); len(lenses) != 0 {
		t.Errorf("lenses of another language = %+v", lenses)
	}
	// RefreshCodeLenses tells the listeners of the registry, once for each call
	refreshes := 0
	stop := tb.scope.Features().OnRefresh(func(f langfeatures.Feature) {
		if f == langfeatures.FeatureCodeLens {
			refreshes++
		}
	})
	api.Languages().RefreshCodeLenses()
	api.Languages().RefreshCodeLenses()
	stop()
	if refreshes != 2 {
		t.Errorf("%d refreshes", refreshes)
	}
}

func TestACodeLensProviderThatFailsOrPanicsGivesNoLenses(t *testing.T) {
	failing := &lensProvider{fail: true}
	_, tb := providerTab(t, "lensfail", func(a extension.API) error {
		_, err := a.Languages().RegisterCodeLensProvider(extension.DocumentSelector{}, failing)
		return err
	})
	lenses, errs := tb.scope.Features().CodeLenses(context.Background(), goDocument("package a\n"))
	if len(lenses) != 0 || len(errs) != 1 {
		t.Fatalf("lenses = %+v errors = %v", lenses, errs)
	}
	assertRoute(t, errs[0].Err, "guardProvider.ExtensionProviderFailed")
	failing.fail, failing.panic = false, true
	_, errs = tb.scope.Features().CodeLenses(context.Background(), goDocument("package a\n"))
	if len(errs) != 1 {
		t.Fatalf("errors = %v", errs)
	}
	assertRoute(t, errs[0].Err, "guardProvider.func1.ExtensionProviderPanicked")
	tb.pump()
	if tb.scope.State("lensfail") != StateFailed {
		t.Errorf("state = %v", tb.scope.State("lensfail"))
	}
	var api extension.API
	_, tb2 := providerTab(t, "lensnil", func(a extension.API) error { api = a; return nil })
	_ = tb2
	_, err := api.Languages().RegisterCodeLensProvider(extension.DocumentSelector{}, nil)
	assertRoute(t, err, "register.ExtensionProviderInvalid")
}
