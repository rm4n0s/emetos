package langfeatures_test

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	. "github.com/rm4n0s/emetos/internal/core/langfeatures"
)

// docOf makes the Document of a request.
func docOf(language, path, text string) Document {
	return Document{URI: document.FileURI(path), LanguageID: language, Version: 1, Text: buffer.New(text).Snapshot()}
}

func routeOf(t *testing.T, err error, want string) {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T: %v", err, err)
	}
	if !appErr.HasRoute(want) {
		t.Errorf("route = %s, want it to contain %s", appErr.Route(), want)
	}
}

// fakeProvider answers every feature with what its fields say.
type fakeProvider struct {
	name      string
	items     []CompletionItem
	incompl   bool
	hover     *Hover
	sig       *SignatureHelp
	links     []LocationLink
	refs      []Location
	symbols   []DocumentSymbol
	highlight []DocumentHighlight
	actions   []CodeAction
	edits     []TextEdit
	prepare   *PrepareRename
	rename    *WorkspaceEdit
	err       error
	calls     *callLog
}

// callLog records which providers were asked; the providers run on goroutines of their own.
type callLog struct {
	mu    sync.Mutex
	names []string
}

func (c *callLog) add(name string) {
	c.mu.Lock()
	c.names = append(c.names, name)
	c.mu.Unlock()
}

func (f *fakeProvider) note() {
	if f.calls != nil {
		f.calls.add(f.name)
	}
}

func (f *fakeProvider) ProvideCompletion(context.Context, Document, Position, CompletionTrigger) (CompletionList, error) {
	f.note()
	return CompletionList{Items: f.items, Incomplete: f.incompl}, f.err
}
func (f *fakeProvider) ProvideHover(context.Context, Document, Position) (*Hover, error) {
	return f.hover, f.err
}
func (f *fakeProvider) ProvideSignatureHelp(context.Context, Document, Position, SignatureContext) (*SignatureHelp, error) {
	return f.sig, f.err
}
func (f *fakeProvider) ProvideLocations(_ context.Context, _ Document, _ Position, kind NavKind) ([]LocationLink, error) {
	if kind != NavDefinition {
		return nil, nil
	}
	return f.links, f.err
}
func (f *fakeProvider) ProvideReferences(context.Context, Document, Position, bool) ([]Location, error) {
	return f.refs, f.err
}
func (f *fakeProvider) ProvideDocumentSymbols(context.Context, Document) ([]DocumentSymbol, error) {
	return f.symbols, f.err
}
func (f *fakeProvider) ProvideDocumentHighlights(context.Context, Document, Position) ([]DocumentHighlight, error) {
	return f.highlight, f.err
}
func (f *fakeProvider) ProvideCodeActions(context.Context, Document, CodeActionRequest) ([]CodeAction, error) {
	return f.actions, f.err
}
func (f *fakeProvider) ProvideFormatting(context.Context, Document, FormattingOptions) ([]TextEdit, error) {
	return f.edits, f.err
}
func (f *fakeProvider) PrepareRename(context.Context, Document, Position) (*PrepareRename, error) {
	return f.prepare, f.err
}
func (f *fakeProvider) ProvideRename(context.Context, Document, Position, string) (*WorkspaceEdit, error) {
	return f.rename, f.err
}

// rangeFormatter is a fakeProvider that formats ranges too.
type rangeFormatter struct{ fakeProvider }

func (r *rangeFormatter) ProvideRangeFormatting(context.Context, Document, Range, FormattingOptions) ([]TextEdit, error) {
	return r.edits, r.err
}

func TestSelectorScore(t *testing.T) {
	goDoc := docOf("go", "/work/a/main.go", "")
	untitled := Document{URI: document.UntitledURI(1), LanguageID: "plaintext"}
	tests := []struct {
		name string
		sel  Selector
		doc  Document
		want int
	}{
		{"the language", Selector{Languages: []string{"go"}}, goDoc, 10},
		{"another language", Selector{Languages: []string{"rust"}}, goDoc, 0},
		{"every document", Selector{}, goDoc, 5},
		{"a pattern on the path", Selector{Pattern: "**/*.go"}, goDoc, 10},
		{"a pattern on the path below the folder", Selector{Pattern: "a/*.go", Folder: "/work"}, goDoc, 10},
		{"a pattern that does not match", Selector{Pattern: "**/*.rs"}, goDoc, 0},
		{"a pattern outside the folder only matches the whole path", Selector{Pattern: "b/*.go", Folder: "/other"}, goDoc, 0},
		{"a pattern never matches an untitled document", Selector{Pattern: "**"}, untitled, 0},
		{"the scheme", Selector{Languages: []string{"plaintext"}, Schemes: []string{"untitled"}}, untitled, 10},
		{"the wrong scheme", Selector{Schemes: []string{"file"}}, untitled, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.sel.Score(tt.doc); got != tt.want {
				t.Errorf("Score = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCompletionGroupsAndMerge(t *testing.T) {
	ctx := context.Background()
	doc := docOf("go", "/work/main.go", "package main\n")
	item := func(label string) CompletionItem { return CompletionItem{Label: label} }
	tests := []struct {
		name      string
		providers []struct {
			owner string
			sel   Selector
			p     *fakeProvider
		}
		want       []string
		owners     []string
		incomplete bool
		errs       int
		asked      []string
	}{
		{
			name: "providers of the same score are concatenated, the newer first",
			providers: []struct {
				owner string
				sel   Selector
				p     *fakeProvider
			}{
				{"server", Selector{Languages: []string{"go"}}, &fakeProvider{name: "server", items: []CompletionItem{item("a"), item("b")}}},
				{"go-provider", Selector{Languages: []string{"go"}}, &fakeProvider{name: "go-provider", items: []CompletionItem{item("c")}}},
			},
			want: []string{"c", "a", "b"}, owners: []string{"go-provider", "server", "server"}, asked: []string{"go-provider", "server"},
		},
		{
			name: "a lower score is asked only when the better ones had nothing",
			providers: []struct {
				owner string
				sel   Selector
				p     *fakeProvider
			}{
				{"words", Selector{}, &fakeProvider{name: "words", items: []CompletionItem{item("w")}}},
				{"server", Selector{Languages: []string{"go"}}, &fakeProvider{name: "server", items: []CompletionItem{item("a")}}},
			},
			want: []string{"a"}, owners: []string{"server"}, asked: []string{"server"},
		},
		{
			name: "the next score is asked when the first group answered nothing",
			providers: []struct {
				owner string
				sel   Selector
				p     *fakeProvider
			}{
				{"words", Selector{}, &fakeProvider{name: "words", items: []CompletionItem{item("w")}}},
				{"server", Selector{Languages: []string{"go"}}, &fakeProvider{name: "server"}},
			},
			want: []string{"w"}, owners: []string{"words"}, asked: []string{"server", "words"},
		},
		{
			name: "an incomplete empty list keeps the next group away and is reported",
			providers: []struct {
				owner string
				sel   Selector
				p     *fakeProvider
			}{
				{"words", Selector{}, &fakeProvider{name: "words", items: []CompletionItem{item("w")}}},
				{"server", Selector{Languages: []string{"go"}}, &fakeProvider{name: "server", incompl: true}},
			},
			want: nil, incomplete: true, asked: []string{"server"},
		},
		{
			name: "a provider that fails does not hide the others",
			providers: []struct {
				owner string
				sel   Selector
				p     *fakeProvider
			}{
				{"a", Selector{Languages: []string{"go"}}, &fakeProvider{name: "a", items: []CompletionItem{item("x")}}},
				{"b", Selector{Languages: []string{"go"}}, &fakeProvider{name: "b", err: errors.New("Boom", "it failed")}},
			},
			want: []string{"x"}, owners: []string{"a"}, errs: 1, asked: []string{"a", "b"},
		},
		{
			name: "a provider for another language is not asked",
			providers: []struct {
				owner string
				sel   Selector
				p     *fakeProvider
			}{
				{"rust", Selector{Languages: []string{"rust"}}, &fakeProvider{name: "rust", items: []CompletionItem{item("r")}}},
			},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := &callLog{}
			reg := NewRegistry()
			for _, p := range tt.providers {
				p.p.calls = calls
				reg.RegisterCompletion(p.owner, p.sel, p.p)
			}
			got, errs := reg.Completion(ctx, doc, Position{}, CompletionTrigger{})
			var labels, owners []string
			for _, it := range got.Items {
				labels = append(labels, it.Label)
				owners = append(owners, it.Owner)
			}
			if !slices.Equal(labels, tt.want) || !slices.Equal(owners, tt.owners) || got.Incomplete != tt.incomplete || len(errs) != tt.errs {
				t.Errorf("labels %v owners %v incomplete %v errors %d; want %v %v %v %d", labels, owners, got.Incomplete, len(errs), tt.want, tt.owners, tt.incomplete, tt.errs)
			}
			slices.Sort(calls.names)
			slices.Sort(tt.asked)
			if !slices.Equal(calls.names, tt.asked) {
				t.Errorf("asked %v, want %v", calls.names, tt.asked)
			}
		})
	}
}

func TestRemoveStopsAProvider(t *testing.T) {
	reg := NewRegistry()
	doc := docOf("go", "/work/main.go", "")
	remove := reg.RegisterHover("a", Selector{}, &fakeProvider{hover: &Hover{Contents: []Markup{{Value: "x"}}}})
	if hs, _ := reg.Hover(context.Background(), doc, Position{}); len(hs) != 1 {
		t.Fatalf("hovers = %v", hs)
	}
	remove()
	remove() // harmless
	if hs, _ := reg.Hover(context.Background(), doc, Position{}); len(hs) != 0 {
		t.Errorf("hovers after remove = %v", hs)
	}
}

func TestHoversAreStacked(t *testing.T) {
	reg := NewRegistry()
	doc := docOf("go", "/work/main.go", "")
	h := func(s string) *Hover { return &Hover{Contents: []Markup{{Value: s}}} }
	reg.RegisterHover("server", Selector{Languages: []string{"go"}}, &fakeProvider{hover: h("from the server")})
	reg.RegisterHover("empty", Selector{Languages: []string{"go"}}, &fakeProvider{hover: &Hover{}})
	reg.RegisterHover("none", Selector{Languages: []string{"go"}}, &fakeProvider{})
	reg.RegisterHover("words", Selector{}, &fakeProvider{hover: h("from all documents")})
	reg.RegisterHover("go", Selector{Languages: []string{"go"}}, &fakeProvider{hover: h("from Go")})
	reg.RegisterHover("broken", Selector{Languages: []string{"go"}}, &fakeProvider{err: errors.New("Boom", "it failed")})
	got, errs := reg.Hover(context.Background(), doc, Position{})
	var texts, owners []string
	for _, x := range got {
		texts = append(texts, x.Contents[0].Value)
		owners = append(owners, x.Owner)
	}
	if want := []string{"from Go", "from the server", "from all documents"}; !slices.Equal(texts, want) {
		t.Errorf("hovers = %v, want %v", texts, want)
	}
	if want := []string{"go", "server", "words"}; !slices.Equal(owners, want) {
		t.Errorf("owners = %v, want %v", owners, want)
	}
	if len(errs) != 1 || errs[0].Owner != "broken" {
		t.Errorf("errors = %+v", errs)
	}
}

func TestLocationsAreJoinedWithoutDuplicates(t *testing.T) {
	reg := NewRegistry()
	doc := docOf("go", "/work/main.go", "")
	at := func(path string, line int) LocationLink {
		l := Location{URI: document.FileURI(path), Range: Range{Start: Position{Line: line}, End: Position{Line: line, Col: 3}}}
		return LocationLink{Target: l, TargetSelection: l.Range}
	}
	reg.RegisterNavigation("a", Selector{Languages: []string{"go"}}, &fakeProvider{links: []LocationLink{at("/work/a.go", 1), at("/work/b.go", 2)}})
	reg.RegisterNavigation("b", Selector{Languages: []string{"go"}}, &fakeProvider{links: []LocationLink{at("/work/b.go", 2), at("/work/c.go", 3)}})
	got, errs := reg.Locations(context.Background(), doc, Position{}, NavDefinition)
	var paths []string
	for _, l := range got {
		paths = append(paths, l.Target.URI.Path)
	}
	if want := []string{"/work/b.go", "/work/c.go", "/work/a.go"}; !slices.Equal(paths, want) || len(errs) != 0 {
		t.Errorf("locations = %v (errors %v), want %v", paths, errs, want)
	}
	if got, _ := reg.Locations(context.Background(), doc, Position{}, NavImplementation); len(got) != 0 {
		t.Errorf("implementations = %v", got)
	}

	reg.RegisterReferences("a", Selector{}, &fakeProvider{refs: []Location{at("/work/a.go", 1).Target, at("/work/a.go", 1).Target}})
	refs, _ := reg.References(context.Background(), doc, Position{}, true)
	if len(refs) != 1 {
		t.Errorf("references = %v", refs)
	}
}

func TestFirstAnswerFeatures(t *testing.T) {
	ctx := context.Background()
	doc := docOf("go", "/work/main.go", "")
	reg := NewRegistry()
	sym := func(n string) []DocumentSymbol { return []DocumentSymbol{{Name: n}} }
	reg.RegisterSymbols("old", Selector{Languages: []string{"go"}}, &fakeProvider{symbols: sym("old")})
	reg.RegisterSymbols("empty", Selector{Languages: []string{"go"}}, &fakeProvider{})
	reg.RegisterSymbols("new", Selector{Languages: []string{"go"}}, &fakeProvider{symbols: sym("new")})
	if got, _ := reg.DocumentSymbols(ctx, doc); len(got) != 1 || got[0].Name != "new" {
		t.Errorf("symbols = %v", got)
	}
	reg.RegisterHighlights("a", Selector{}, &fakeProvider{highlight: []DocumentHighlight{{Kind: HighlightWrite}}})
	if got, _ := reg.DocumentHighlights(ctx, doc, Position{}); len(got) != 1 || got[0].Kind != HighlightWrite {
		t.Errorf("highlights = %v", got)
	}
	reg.RegisterSignatureHelp("a", Selector{}, &fakeProvider{sig: &SignatureHelp{}}, "(")
	reg.RegisterSignatureHelp("b", Selector{}, &fakeProvider{sig: &SignatureHelp{Signatures: []Signature{{Label: "f()"}}}}, "(", ",")
	got, _ := reg.SignatureHelp(ctx, doc, Position{}, SignatureContext{})
	if got == nil || got.Signatures[0].Label != "f()" || got.Owner != "b" {
		t.Errorf("signature help = %+v", got)
	}
	if tr := reg.SignatureTriggers(doc); !slices.Equal(tr, []string{"(", ","}) {
		t.Errorf("signature triggers = %v", tr)
	}
	reg.RegisterCompletion("a", Selector{}, &fakeProvider{}, ".", ":")
	reg.RegisterCompletion("b", Selector{Languages: []string{"go"}}, &fakeProvider{}, ".")
	if tr := reg.CompletionTriggers(doc); len(tr) != 2 {
		t.Errorf("completion triggers = %v", tr)
	}
	reg.RegisterCodeActions("a", Selector{}, &fakeProvider{actions: []CodeAction{{Title: "one"}}})
	reg.RegisterCodeActions("b", Selector{}, &fakeProvider{actions: []CodeAction{{Title: "two"}}})
	acts, _ := reg.CodeActions(ctx, doc, CodeActionRequest{})
	if len(acts) != 2 || acts[0].Title != "two" || acts[0].Owner != "b" {
		t.Errorf("code actions = %+v", acts)
	}
}

func TestFormatterChoice(t *testing.T) {
	ctx := context.Background()
	doc := docOf("go", "/work/main.go", "")
	edit := func(s string) []TextEdit { return []TextEdit{{Text: s}} }
	reg := NewRegistry()
	if _, _, ok := reg.PickFormatter(doc, ""); ok {
		t.Error("a formatter was found in an empty registry")
	}
	_, err := reg.FormatDocument(ctx, doc, FormattingOptions{}, "")
	routeOf(t, err, "Registry.FormatDocument.NoFormatter")
	_, err = reg.FormatRange(ctx, doc, Range{}, FormattingOptions{}, "")
	routeOf(t, err, "Registry.FormatRange.NoFormatter")

	reg.RegisterFormatting("server", Selector{Languages: []string{"go"}}, &rangeFormatter{fakeProvider{edits: edit("server")}})
	reg.RegisterFormatting("other", Selector{Languages: []string{"go"}}, &fakeProvider{edits: edit("other")})
	tests := []struct {
		name, def string
		doc, rng  string
		others    int
	}{
		{"without a default, the newest", "", "other", "server", 1},
		{"the default formatter", "server", "server", "server", 1},
		{"a default that is not registered", "nobody", "other", "server", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg2, others, ok := reg.PickFormatter(doc, tt.def)
			if !ok || reg2.Owner != tt.doc || others != tt.others {
				t.Errorf("PickFormatter = %v %d %v", reg2.Owner, others, ok)
			}
			got, err := reg.FormatDocument(ctx, doc, FormattingOptions{}, tt.def)
			if err != nil || got[0].Text != tt.doc {
				t.Errorf("FormatDocument = %v, %v; want %s", got, err, tt.doc)
			}
			// only a formatter that formats ranges is asked to
			got, err = reg.FormatRange(ctx, doc, Range{}, FormattingOptions{}, tt.def)
			if err != nil || got[0].Text != tt.rng {
				t.Errorf("FormatRange = %v, %v; want %s", got, err, tt.rng)
			}
		})
	}
}

func TestRenameUsesTheFirstThatAnswers(t *testing.T) {
	ctx := context.Background()
	doc := docOf("go", "/work/main.go", "")
	reg := NewRegistry()
	if got, err := reg.PrepareRename(ctx, doc, Position{}); got != nil || err != nil {
		t.Errorf("no providers: %v, %v", got, err)
	}
	reg.RegisterRename("old", Selector{}, &fakeProvider{prepare: &PrepareRename{Placeholder: "old"}, rename: &WorkspaceEdit{Label: "old"}})
	reg.RegisterRename("silent", Selector{}, &fakeProvider{})
	got, err := reg.PrepareRename(ctx, doc, Position{})
	if err != nil || got == nil || got.Placeholder != "old" {
		t.Errorf("PrepareRename = %+v, %v", got, err)
	}
	edit, err := reg.Rename(ctx, doc, Position{}, "x")
	if err != nil || edit == nil || edit.Label != "old" {
		t.Errorf("Rename = %+v, %v", edit, err)
	}
	reg.RegisterRename("refuses", Selector{}, &fakeProvider{err: errors.New("NotRenameable", "no")})
	if _, err := reg.PrepareRename(ctx, doc, Position{}); err == nil {
		t.Error("a provider that refuses did not stop the rename")
	}
	if _, err := reg.Rename(ctx, doc, Position{}, "x"); err == nil {
		t.Error("a provider that fails did not fail the rename")
	}
}
