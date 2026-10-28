package exthost

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// Languages().References and Implementations, Documents().Active and Clipboard().WriteText (step 17e.3, SPEC §10.28).

// refProvider answers the references of any position with the locations it was given, or its error.
type refProvider struct {
	locs []extension.Location
	err  error
	wait chan struct{}
	seen chan extension.Position
}

func (p *refProvider) ProvideReferences(ctx context.Context, _ extension.TextDocument, pos extension.Position, _ bool) ([]extension.Location, error) {
	if p.seen != nil {
		p.seen <- pos
	}
	if p.wait != nil {
		select {
		case <-p.wait:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return p.locs, p.err
}

// answer is what a done function received.
type answer struct {
	locs []extension.Location
	err  error
}

// refTab makes a tab whose extension registers the provider and keeps its API, with a Go document open at /work/one/a.go.
func refTab(t *testing.T, id string, p *refProvider, tweak func(*rig)) (*rig, *tab, extension.API) {
	t.Helper()
	var api extension.API
	f, _ := factory(manifest(id), func(f *fake) {
		f.activate = func(_ context.Context, a extension.API) error {
			api = a
			if p == nil {
				return nil
			}
			_, err := a.Languages().RegisterReferenceProvider(extension.DocumentSelector{Languages: []string{"go"}}, p)
			return err
		}
	})
	r := newRig(t, f)
	if tweak != nil {
		tweak(r)
	}
	tb := r.newTab("one")
	doc := document.New(document.FileURI("/work/one/a.go"), "package a\n\nfunc F() {}\n", document.Options{LanguageID: "go"})
	tb.lsp.open(doc)
	tb.scope.Start()
	if api == nil {
		t.Fatal("the extension was not activated")
	}
	return r, tb, api
}

// wait receives an answer or fails the test.
func wait(t *testing.T, ch <-chan answer) answer {
	t.Helper()
	select {
	case a := <-ch:
		return a
	case <-time.After(10 * time.Second):
		t.Fatal("the answer did not come")
		return answer{}
	}
}

func TestReferencesAskTheProvidersOfTheScope(t *testing.T) {
	want := []extension.Location{
		{URI: "file:///work/one/a.go", Range: extension.Range{Start: extension.Position{Line: 2, Col: 5}, End: extension.Position{Line: 2, Col: 6}}},
		{URI: "file:///work/one/b.go", Range: extension.Range{Start: extension.Position{Line: 7, Col: 1}, End: extension.Position{Line: 7, Col: 2}}},
	}
	p := &refProvider{locs: want, seen: make(chan extension.Position, 1)}
	_, _, api := refTab(t, "refs", p, nil)
	got := make(chan answer, 1)
	pos := extension.Position{Line: 2, Col: 5}
	if err := api.Languages().References(context.Background(), "file:///work/one/a.go", pos, func(l []extension.Location, err error) { got <- answer{l, err} }); err != nil {
		t.Fatal(err)
	}
	a := wait(t, got)
	if a.err != nil || !slices.Equal(a.locs, want) {
		t.Errorf("answer = %+v", a)
	}
	if seen := <-p.seen; seen != pos {
		t.Errorf("the provider was asked about %+v", seen)
	}
}

func TestReferencesFailures(t *testing.T) {
	boom := errors.New("Boom", "the provider failed")
	tests := []struct {
		name  string
		p     *refProvider
		tweak func(*rig)
		uri   string
		call  func(l extension.Languages, uri string, done func([]extension.Location, error)) error
		// at says that the error comes from the call itself, else from the answer
		at    string
		route string
	}{
		{
			name: "no provider offers implementations", p: &refProvider{}, uri: "file:///work/one/a.go", at: "answer", route: "languagesAPI.locations.ExtensionNoProvider",
			call: func(l extension.Languages, uri string, done func([]extension.Location, error)) error {
				return l.Implementations(context.Background(), uri, extension.Position{}, done)
			},
		},
		{
			name: "no provider at all", p: nil, uri: "file:///work/one/a.go", at: "answer", route: "languagesAPI.locations.ExtensionNoProvider",
			call: func(l extension.Languages, uri string, done func([]extension.Location, error)) error {
				return l.References(context.Background(), uri, extension.Position{}, done)
			},
		},
		{
			name: "a document that is not open", p: &refProvider{}, uri: "file:///work/one/zzz.go", at: "call", route: "languagesAPI.locations.ExtensionDocumentUnavailable",
			call: func(l extension.Languages, uri string, done func([]extension.Location, error)) error {
				return l.References(context.Background(), uri, extension.Position{}, done)
			},
		},
		{
			name: "a URI nobody knows", p: &refProvider{}, uri: "nonsense", at: "call", route: "ParseURI.URIInvalid",
			call: func(l extension.Languages, uri string, done func([]extension.Location, error)) error {
				return l.References(context.Background(), uri, extension.Position{}, done)
			},
		},
		{
			name: "no function to call", p: &refProvider{}, uri: "file:///work/one/a.go", at: "call", route: "languagesAPI.locations.ExtensionRequestInvalid",
			call: func(l extension.Languages, uri string, _ func([]extension.Location, error)) error {
				return l.References(context.Background(), uri, extension.Position{}, nil)
			},
		},
		{
			name: "the scope has no language workspace", p: &refProvider{}, uri: "file:///work/one/a.go", at: "call", route: "languagesAPI.locations.ExtensionDocumentUnavailable",
			tweak: func(r *rig) { r.noLSP["one"] = true },
			call: func(l extension.Languages, uri string, done func([]extension.Location, error)) error {
				return l.References(context.Background(), uri, extension.Position{}, done)
			},
		},
		{
			name: "the only provider fails", p: &refProvider{err: boom}, uri: "file:///work/one/a.go", at: "answer", route: "ExtensionProviderFailed",
			call: func(l extension.Languages, uri string, done func([]extension.Location, error)) error {
				return l.References(context.Background(), uri, extension.Position{}, done)
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, _, api := refTab(t, "fail", tc.p, tc.tweak)
			got := make(chan answer, 1)
			err := tc.call(api.Languages(), tc.uri, func(l []extension.Location, err error) { got <- answer{l, err} })
			if tc.at == "answer" {
				if err != nil {
					t.Fatalf("the call failed: %v", err)
				}
				err = wait(t, got).err
			}
			route(t, err, tc.route)
		})
	}
}

func TestReferencesEndWhenTheContextDoes(t *testing.T) {
	p := &refProvider{wait: make(chan struct{}), seen: make(chan extension.Position, 1)}
	_, _, api := refTab(t, "cancel", p, nil)
	ctx, cancel := context.WithCancel(context.Background())
	got := make(chan answer, 1)
	if err := api.Languages().References(ctx, "file:///work/one/a.go", extension.Position{}, func(l []extension.Location, err error) { got <- answer{l, err} }); err != nil {
		t.Fatal(err)
	}
	<-p.seen // the provider is waiting
	cancel()
	a := wait(t, got)
	if len(a.locs) != 0 {
		t.Errorf("locations = %+v", a.locs)
	}
	route(t, a.err, "languagesAPI.locations.func2.ExtensionRequestCancelled")
	if !errors.Is(a.err, context.Canceled) {
		t.Errorf("the error does not wrap the cancellation: %v", a.err)
	}
}

func TestReferencesAfterTheActivationEnded(t *testing.T) {
	p := &refProvider{}
	_, tb, api := refTab(t, "dead", p, nil)
	tb.scope.Close()
	err := api.Languages().References(context.Background(), "file:///work/one/a.go", extension.Position{}, func([]extension.Location, error) {})
	route(t, err, "languagesAPI.locations.ExtensionDeactivated")
	route(t, api.Clipboard().WriteText("x"), "clipboardAPI.WriteText.ExtensionDeactivated")
}

// activeDocs is a DocumentSource that also says which editor is in front.
type activeDocs struct {
	*docSource
	mu  sync.Mutex
	doc *extension.ActiveDocument
}

func (d *activeDocs) Active() (extension.ActiveDocument, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.doc == nil {
		return extension.ActiveDocument{}, false
	}
	return *d.doc, true
}

func TestTheActiveDocumentComesFromTheWindow(t *testing.T) {
	var api extension.API
	f, _ := factory(manifest("active"), func(f *fake) {
		f.activate = func(_ context.Context, a extension.API) error { api = a; return nil }
	})
	r := newRig(t, f)
	tb := r.newTab("one")
	// a window that does not say which editor is in front
	tb.scope.Start()
	if _, ok := api.Documents().Active(); ok {
		t.Error("a source that cannot tell had an active document")
	}
	src := &activeDocs{docSource: tb.docs}
	tb.scope.opts.Documents = src
	if _, ok := api.Documents().Active(); ok {
		t.Error("no editor is in front, and there was an active document")
	}
	want := extension.ActiveDocument{URI: "file:///work/one/a.go", LanguageID: "go", Version: 4, Selections: []extension.Selection{{
		Anchor: extension.Position{Line: 1, Col: 2}, Active: extension.Position{Line: 3, Col: 4},
	}}}
	src.doc = &want
	got, ok := api.Documents().Active()
	if !ok || got.URI != want.URI || got.Version != 4 || !slices.Equal(got.Selections, want.Selections) {
		t.Errorf("active = %+v %v", got, ok)
	}
}

func TestTheClipboardOfAnExtension(t *testing.T) {
	var api extension.API
	f, _ := factory(manifest("clip"), func(f *fake) {
		f.activate = func(_ context.Context, a extension.API) error { api = a; return nil }
	})
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.Start()
	if err := api.Clipboard().WriteText("copied"); err != nil || !slices.Equal(tb.clips, []string{"copied"}) {
		t.Errorf("clips = %v, %v", tb.clips, err)
	}
	tb.clipErr = errors.New("ClipboardBroken", "the desktop refused")
	route(t, api.Clipboard().WriteText("again"), "ClipboardBroken")

	r2 := newRig(t, f)
	r2.noClip = map[string]bool{"two": true}
	tb2 := r2.newTab("two")
	tb2.scope.Start()
	route(t, api.Clipboard().WriteText("x"), "ExtensionClipboardUnavailable")
}

func TestAViewTakesATitleFromItsExtension(t *testing.T) {
	m := manifest("titled")
	m.Contributes.ViewContainers = []extension.ViewContainerSpec{{ID: "titled.box", Title: "Box", Icon: "list-tree"}}
	m.Contributes.Views = []extension.ViewSpec{{ID: "titled.view", Name: "Items", Container: "titled.box"}}
	var h extension.ViewHandle
	f, _ := factory(m, func(f *fake) {
		f.activate = func(_ context.Context, a extension.API) error {
			var err error
			h, err = a.Views().Provide("titled.view", extension.ViewContent{Build: func(widgets.BuildContext) widgets.Widget { return nil }})
			return err
		}
	})
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.Start()
	tb.scope.Fire(extension.OnView("titled.view"))
	if got := tb.scope.ViewTitle("titled.view"); got != "" {
		t.Errorf("title before = %q", got)
	}
	h.SetTitle("Implementations")
	if got := tb.scope.ViewTitle("titled.view"); got != "Implementations" {
		t.Errorf("title = %q", got)
	}
	h.SetTitle("")
	if got := tb.scope.ViewTitle("titled.view"); got != "" {
		t.Errorf("title after = %q", got)
	}
	if got := tb.scope.ViewTitle("no.such.view"); got != "" {
		t.Errorf("title of a view that is not there = %q", got)
	}
}
