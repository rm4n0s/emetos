package editor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
)

// fakeNav is a provider of navigation and references the tests drive: the answers per kind, and a gate that holds them.
type fakeNav struct {
	mu    sync.Mutex
	links map[langfeatures.NavKind][]langfeatures.LocationLink
	refs  []langfeatures.Location
	gate  chan struct{}
	calls int
}

func (f *fakeNav) wait(ctx context.Context) error {
	f.mu.Lock()
	gate := f.gate
	f.calls++
	f.mu.Unlock()
	if gate == nil {
		return nil
	}
	select {
	case <-gate:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (f *fakeNav) ProvideLocations(ctx context.Context, _ langfeatures.Document, _ langfeatures.Position, kind langfeatures.NavKind) ([]langfeatures.LocationLink, error) {
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.links[kind], nil
}

func (f *fakeNav) ProvideReferences(ctx context.Context, _ langfeatures.Document, _ langfeatures.Position, _ bool) ([]langfeatures.Location, error) {
	if err := f.wait(ctx); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.refs, nil
}

// navProgram is the text of the document of the tests: line 4 has a use of "Greet" (column 1).
const navProgram = "package main\n\nfunc Greet() {}\n\nfunc main() {\n\tGreet()\n\tGreet()\n}\n"

// opened is a call of SuggestHost.Open.
type opened struct {
	loc langfeatures.Location
	o   OpenLocation
}

// navRig is a view model with the providers above, a poster the test drives and a host that records what it is asked to open.
type navRig struct {
	t      *testing.T
	vm     *ViewModel
	reg    *langfeatures.Registry
	nav    *fakeNav
	posts  chan func()
	opens  []opened
	logged []string
}

func newNavRig(t *testing.T, tweak func(*Options)) *navRig {
	t.Helper()
	opts := goOptions()
	opts.CursorBlinking = "solid"
	opts.GotoLocation = GotoLocationOptions{DefinitionLinkOpensInPeek: false}
	if tweak != nil {
		tweak(&opts)
	}
	doc := newTestDoc(navProgram)
	doc.SetLanguageID("go")
	vm := NewViewModel(doc, opts, nil)
	r := &navRig{t: t, vm: vm, reg: langfeatures.NewRegistry(), nav: &fakeNav{links: map[langfeatures.NavKind][]langfeatures.LocationLink{}}, posts: make(chan func(), 256)}
	vm.SetPoster(func(fn func()) { r.posts <- fn })
	vm.SetSuggest(&SuggestHost{
		Ctx: t.Context(), Features: r.reg,
		Log:  func(msg string, _ error) { r.logged = append(r.logged, msg) },
		Open: func(loc langfeatures.Location, o OpenLocation) { r.opens = append(r.opens, opened{loc, o}) },
	})
	sel := langfeatures.Selector{Languages: []string{"go"}}
	r.reg.RegisterNavigation("test", sel, r.nav)
	r.reg.RegisterReferences("test", sel, r.nav)
	t.Cleanup(vm.CancelGoTo)
	t.Cleanup(vm.PeekClose)
	t.Cleanup(vm.LinkClear)
	cursorAt(vm, 5, 2) // in the first use of Greet
	return r
}

// settle runs what is posted to the UI thread until nothing comes for a short while.
func (r *navRig) settle() {
	for {
		select {
		case fn := <-r.posts:
			fn()
		case <-time.After(60 * time.Millisecond):
			return
		}
	}
}

func link(uri document.URI, l1, c1, l2, c2 int) langfeatures.LocationLink {
	return langfeatures.LocationLink{Target: langfeatures.Location{URI: uri, Range: rg(l1, c1, l2, c2)}}
}

var (
	thisFile  = document.FileURI("/test.go")
	otherFile = document.FileURI("/other.go")
)

func TestOneResultJumpsToIt(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind GoKind
		set  func(*navRig)
	}{
		{"definition", GoDefinition, func(r *navRig) {
			r.nav.links[langfeatures.NavDefinition] = []langfeatures.LocationLink{link(thisFile, 2, 5, 2, 10)}
		}},
		{"declaration", GoDeclaration, func(r *navRig) {
			r.nav.links[langfeatures.NavDeclaration] = []langfeatures.LocationLink{link(otherFile, 0, 0, 0, 3)}
		}},
		{"type definition", GoTypeDefinition, func(r *navRig) {
			r.nav.links[langfeatures.NavTypeDefinition] = []langfeatures.LocationLink{link(otherFile, 3, 0, 3, 3)}
		}},
		{"implementation", GoImplementation, func(r *navRig) {
			r.nav.links[langfeatures.NavImplementation] = []langfeatures.LocationLink{link(otherFile, 4, 0, 4, 3)}
		}},
		{"references", GoReferences, func(r *navRig) {
			r.nav.refs = []langfeatures.Location{{URI: otherFile, Range: rg(1, 1, 1, 6)}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newNavRig(t, nil)
			tc.set(r)
			if !r.vm.GoTo(tc.kind, GoConfig{}) {
				t.Fatal("no request was made")
			}
			r.settle()
			if r.vm.PeekVisible() {
				t.Fatal("the peek view opened for one result")
			}
			if len(r.opens) != 1 {
				t.Fatalf("opens = %v, want one", r.opens)
			}
			if got := r.opens[0].loc.Range.Start; got.Line < 0 {
				t.Errorf("start = %v", got)
			}
		})
	}
}

func TestSeveralResultsOpenThePeekView(t *testing.T) {
	r := newNavRig(t, nil)
	r.nav.refs = []langfeatures.Location{
		{URI: otherFile, Range: rg(3, 1, 3, 6)},
		{URI: thisFile, Range: rg(6, 1, 6, 6)},
		{URI: thisFile, Range: rg(5, 1, 5, 6)},
		{URI: thisFile, Range: rg(2, 5, 2, 10)},
	}
	if !r.vm.GoTo(GoReferences, GoConfig{}) {
		t.Fatal("no request was made")
	}
	r.settle()
	if !r.vm.PeekVisible() || len(r.opens) != 0 {
		t.Fatalf("peek = %v, opens = %v", r.vm.PeekVisible(), r.opens)
	}
	v := r.vm.Peek()
	if len(v.Refs) != 4 || len(v.Files) != 2 {
		t.Fatalf("refs = %d, files = %d", len(v.Refs), len(v.Files))
	}
	// by file, then by position, as VS Code's ReferencesModel
	if v.Refs[0].URI != otherFile || v.Refs[1].Range.Start.Line != 2 || v.Refs[3].Range.Start.Line != 6 {
		t.Errorf("order = %+v", v.Refs)
	}
	if v.Files[1].Count != 3 || v.Files[1].Name != "test.go" {
		t.Errorf("files = %+v", v.Files)
	}
	if v.Meta != "4 references" {
		t.Errorf("meta = %q", v.Meta)
	}
	// the selected one is the first reference after the cursor, as nearestReference
	if got := v.Refs[v.Selected].Range.Start; got.Line != 5 {
		t.Errorf("selected = %v", got)
	}
}

func TestPeekKeysMoveOpenAndClose(t *testing.T) {
	r := newNavRig(t, nil)
	r.nav.refs = []langfeatures.Location{
		{URI: thisFile, Range: rg(2, 5, 2, 10)}, {URI: thisFile, Range: rg(5, 1, 5, 6)}, {URI: thisFile, Range: rg(6, 1, 6, 6)},
	}
	r.vm.GoTo(GoReferences, GoConfig{})
	r.settle()
	if !r.vm.PeekVisible() {
		t.Fatal("no peek view")
	}
	start := r.vm.Peek().Selected
	r.vm.PeekMove(1)
	if got := r.vm.Peek().Selected; got != (start+1)%3 {
		t.Errorf("down: %d after %d", got, start)
	}
	r.vm.PeekMove(-1)
	r.vm.PeekMove(-1)
	if got := r.vm.Peek().Selected; got != (start+2)%3 {
		t.Errorf("up, wrapping: %d", got)
	}
	r.vm.PeekSelect(0)
	r.vm.PeekOpenSelected(false, true)
	if len(r.opens) != 1 || r.opens[0].loc.Range.Start.Line != 2 || !r.opens[0].o.Pinned || r.opens[0].o.Side {
		t.Fatalf("opens = %+v", r.opens)
	}
	if r.vm.PeekVisible() {
		t.Error("the peek view stays after a location was opened")
	}
	// Escape closes it
	r.vm.GoTo(GoReferences, GoConfig{})
	r.settle()
	if !r.vm.PeekVisible() {
		t.Fatal("no peek view the second time")
	}
	if err := r.vm.RunCommand("closeReferenceSearch", nil); err != nil {
		t.Fatal(err)
	}
	if r.vm.PeekVisible() {
		t.Error("closeReferenceSearch left the peek view open")
	}
	// Open to the side
	r.vm.GoTo(GoReferences, GoConfig{})
	r.settle()
	r.vm.PeekOpenSelected(true, false)
	if len(r.opens) != 2 || !r.opens[1].o.Side {
		t.Errorf("opens = %+v", r.opens)
	}
}

func TestPeekDefinitionOpensThePeekForOneResult(t *testing.T) {
	r := newNavRig(t, nil)
	r.nav.links[langfeatures.NavDefinition] = []langfeatures.LocationLink{link(thisFile, 2, 5, 2, 10)}
	r.vm.GoTo(GoDefinition, GoConfig{Peek: true})
	r.settle()
	if !r.vm.PeekVisible() || len(r.vm.Peek().Refs) != 1 || len(r.opens) != 0 {
		t.Errorf("peek = %v, opens = %v", r.vm.PeekVisible(), r.opens)
	}
}

func TestGotoLocationSettings(t *testing.T) {
	many := []langfeatures.Location{{URI: thisFile, Range: rg(2, 5, 2, 10)}, {URI: thisFile, Range: rg(6, 1, 6, 6)}}
	for _, tc := range []struct {
		multiple  string
		wantPeek  bool
		wantOpens int
	}{
		{"peek", true, 0},
		{"", true, 0},
		{"goto", false, 1},
		{"gotoAndPeek", false, 1}, // the peek opens in the editor that shows the location (After), which the host fake does not make
	} {
		t.Run("multiple="+tc.multiple, func(t *testing.T) {
			r := newNavRig(t, func(o *Options) { o.GotoLocation.Multiple[GoReferences] = tc.multiple })
			r.nav.refs = many
			r.vm.GoTo(GoReferences, GoConfig{})
			r.settle()
			if r.vm.PeekVisible() != tc.wantPeek || len(r.opens) != tc.wantOpens {
				t.Errorf("peek = %v, opens = %d", r.vm.PeekVisible(), len(r.opens))
			}
			if tc.multiple == "gotoAndPeek" && (len(r.opens) != 1 || r.opens[0].o.After == nil) {
				t.Errorf("the open does not ask for the peek afterwards: %+v", r.opens)
			}
		})
	}
}

func TestAlternativeCommandRunsWhenTheCursorIsOnTheOnlyResult(t *testing.T) {
	r := newNavRig(t, func(o *Options) { o.GotoLocation.Alternative[GoDefinition] = "editor.action.goToReferences" })
	// the cursor is in the definition
	cursorAt(r.vm, 2, 7)
	r.nav.links[langfeatures.NavDefinition] = []langfeatures.LocationLink{link(thisFile, 2, 5, 2, 10)}
	var ran []string
	if !r.vm.GoTo(GoDefinition, GoConfig{Run: func(id string) error { ran = append(ran, id); return nil }}) {
		t.Fatal("no request")
	}
	r.settle()
	if len(ran) != 1 || ran[0] != "editor.action.goToReferences" || len(r.opens) != 0 {
		t.Errorf("ran = %v, opens = %v", ran, r.opens)
	}
	// elsewhere the definition is gone to
	cursorAt(r.vm, 5, 2)
	ran = nil
	r.vm.GoTo(GoDefinition, GoConfig{Run: func(id string) error { ran = append(ran, id); return nil }})
	r.settle()
	if len(ran) != 0 || len(r.opens) != 1 {
		t.Errorf("ran = %v, opens = %v", ran, r.opens)
	}
}

func TestNoResultShowsAMessage(t *testing.T) {
	r := newNavRig(t, nil)
	r.vm.GoTo(GoDefinition, GoConfig{})
	r.settle()
	h := r.vm.Hover()
	if h == nil || !h.Pinned || len(r.opens) != 0 || r.vm.PeekVisible() {
		t.Fatalf("hover = %+v, opens = %v", h, r.opens)
	}
	if got := h.Parts[0].Contents[0].Value; got != "No definition found for 'Greet'" {
		t.Errorf("message = %q", got)
	}
	// a muted navigation says nothing
	r.vm.HideHover()
	r.vm.GoTo(GoDefinition, GoConfig{Mute: true})
	r.settle()
	if r.vm.Hover() != nil {
		t.Error("a muted navigation showed a message")
	}
}

func TestStaleNavigationAnswersAreDropped(t *testing.T) {
	for _, tc := range []struct {
		name string
		move func(*navRig)
	}{
		{"the cursor moved", func(r *navRig) { cursorAt(r.vm, 6, 2) }},
		{"the text changed", func(r *navRig) {
			if _, err := r.vm.Document().Edit([]document.Edit{{Range: rg(0, 0, 0, 0), Text: "// "}}, document.EditOptions{}); err != nil {
				r.t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newNavRig(t, nil)
			r.nav.gate = make(chan struct{})
			r.nav.links[langfeatures.NavDefinition] = []langfeatures.LocationLink{link(otherFile, 0, 0, 0, 3)}
			r.vm.GoTo(GoDefinition, GoConfig{})
			tc.move(r)
			close(r.nav.gate)
			r.settle()
			if len(r.opens) != 0 || r.vm.PeekVisible() {
				t.Errorf("a stale answer was used: opens = %v", r.opens)
			}
		})
	}
	t.Run("a newer request replaces it and cancels it", func(t *testing.T) {
		r := newNavRig(t, nil)
		r.nav.gate = make(chan struct{})
		r.nav.links[langfeatures.NavDefinition] = []langfeatures.LocationLink{link(otherFile, 0, 0, 0, 3)}
		r.vm.GoTo(GoDefinition, GoConfig{})
		r.vm.GoTo(GoDefinition, GoConfig{})
		close(r.nav.gate)
		r.settle()
		if len(r.opens) != 1 {
			t.Errorf("opens = %v, want the answer of the last request only", r.opens)
		}
	})
	t.Run("cancelled", func(t *testing.T) {
		r := newNavRig(t, nil)
		r.nav.gate = make(chan struct{})
		r.nav.links[langfeatures.NavDefinition] = []langfeatures.LocationLink{link(otherFile, 0, 0, 0, 3)}
		r.vm.GoTo(GoDefinition, GoConfig{})
		r.vm.CancelGoTo()
		close(r.nav.gate)
		r.settle()
		if len(r.opens) != 0 {
			t.Errorf("opens = %v", r.opens)
		}
	})
}

func TestGoToNeedsAProvider(t *testing.T) {
	r := newNavRig(t, nil)
	r.vm.Document().SetLanguageID("plaintext")
	if r.vm.GoTo(GoDefinition, GoConfig{}) || r.vm.HasNavigation(GoDefinition) {
		t.Error("a document with no provider asked for a definition")
	}
	r.vm.Document().SetLanguageID("go")
	if !r.vm.HasNavigation(GoReferences) {
		t.Error("the references are offered")
	}
}

func TestCtrlHoverUnderlinesAWordWithADefinition(t *testing.T) {
	r := newNavRig(t, nil)
	r.nav.links[langfeatures.NavDefinition] = []langfeatures.LocationLink{link(thisFile, 2, 5, 2, 10)}
	ctrl := MouseModifiers{Ctrl: true}
	r.vm.LinkPointerMoved(positionOf(5, 2), MouseModifiers{})
	r.settle()
	if r.vm.Link() != nil || r.nav.calls != 0 {
		t.Fatal("a link without the modifier")
	}
	r.vm.LinkPointerMoved(positionOf(5, 2), ctrl)
	r.settle()
	l := r.vm.Link()
	if l == nil || l.Range != rg(5, 1, 5, 6) || l.Count != 1 {
		t.Fatalf("link = %+v", l)
	}
	// moving inside the word keeps it and asks nothing more
	calls := r.nav.calls
	r.vm.LinkPointerMoved(positionOf(5, 4), ctrl)
	r.settle()
	if r.vm.Link() == nil || r.nav.calls != calls {
		t.Errorf("link = %+v, calls %d -> %d", r.vm.Link(), calls, r.nav.calls)
	}
	// releasing the key takes it away
	r.vm.LinkModifierChanged(false)
	if r.vm.Link() != nil {
		t.Error("the link stays after the key went up")
	}
	// a place with no word has none
	r.vm.LinkPointerMoved(positionOf(5, 0), ctrl)
	r.settle()
	if r.vm.Link() != nil {
		t.Error("a link on a blank")
	}
	// a word without a definition has none
	r.nav.links[langfeatures.NavDefinition] = nil
	r.vm.LinkPointerMoved(positionOf(6, 2), ctrl)
	r.settle()
	if r.vm.Link() != nil {
		t.Error("a link without a definition")
	}
}

func TestCtrlClickGoesToTheDefinition(t *testing.T) {
	for _, tc := range []struct {
		name     string
		peekOpt  bool
		mods     MouseModifiers
		wantPeek bool
		wantSide bool
	}{
		{"jump", false, MouseModifiers{Ctrl: true}, false, false},
		{"peek", true, MouseModifiers{Ctrl: true}, true, false},
		{"side", false, MouseModifiers{Ctrl: true, Alt: true}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newNavRig(t, func(o *Options) { o.GotoLocation.DefinitionLinkOpensInPeek = tc.peekOpt })
			r.nav.links[langfeatures.NavDefinition] = []langfeatures.LocationLink{link(otherFile, 0, 5, 0, 10)}
			if !r.vm.LinkClick(positionOf(6, 2), tc.mods) {
				t.Fatal("the click was not taken")
			}
			r.settle()
			if got := r.vm.Cursors().Primary().Position(); got != positionOf(6, 2) {
				t.Errorf("the cursor is at %v", got)
			}
			if r.vm.PeekVisible() != tc.wantPeek {
				t.Errorf("peek = %v", r.vm.PeekVisible())
			}
			if !tc.wantPeek && (len(r.opens) != 1 || r.opens[0].o.Side != tc.wantSide) {
				t.Errorf("opens = %+v", r.opens)
			}
		})
	}
	t.Run("an ordinary click is not taken", func(t *testing.T) {
		r := newNavRig(t, nil)
		if r.vm.LinkClick(positionOf(6, 2), MouseModifiers{}) {
			t.Error("a click without the modifier was taken")
		}
		if r.vm.LinkClick(positionOf(6, 0), MouseModifiers{Ctrl: true}) {
			t.Error("a click on a blank was taken")
		}
	})
}

func TestEmbeddedEditorsDoNotOpenPeekViews(t *testing.T) {
	r := newNavRig(t, nil)
	r.vm.SetEmbedded(true)
	r.nav.refs = []langfeatures.Location{{URI: thisFile, Range: rg(2, 5, 2, 10)}, {URI: thisFile, Range: rg(6, 1, 6, 6)}}
	r.vm.GoTo(GoReferences, GoConfig{Peek: true})
	r.settle()
	if r.vm.PeekVisible() || len(r.opens) != 1 {
		t.Errorf("peek = %v, opens = %v", r.vm.PeekVisible(), r.opens)
	}
}

func TestNavigationContextKeys(t *testing.T) {
	r := newNavRig(t, nil)
	for _, k := range []GoKind{GoDefinition, GoDeclaration, GoTypeDefinition, GoImplementation, GoReferences} {
		if !r.vm.HasNavigation(k) {
			t.Errorf("kind %d is not offered", k)
		}
	}
}
