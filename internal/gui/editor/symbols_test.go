package editor

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/edit"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
)

// fakeSymbols is a provider of document symbols and highlights the tests drive.
type fakeSymbols struct {
	mu    sync.Mutex
	syms  []langfeatures.DocumentSymbol
	hls   []langfeatures.DocumentHighlight
	gate  chan struct{}
	calls int
}

func (f *fakeSymbols) ProvideDocumentSymbols(ctx context.Context, _ langfeatures.Document) ([]langfeatures.DocumentSymbol, error) {
	f.mu.Lock()
	f.calls++
	gate, syms := f.gate, f.syms
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return syms, nil
}

func (f *fakeSymbols) ProvideDocumentHighlights(_ context.Context, _ langfeatures.Document, _ langfeatures.Position) ([]langfeatures.DocumentHighlight, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hls, nil
}

func (f *fakeSymbols) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// sampleSymbols are the symbols of stickySample: Outer holds a loop, and Other follows.
func sampleSymbols() []langfeatures.DocumentSymbol {
	return []langfeatures.DocumentSymbol{
		{Name: "Outer", Kind: 12, Range: rg(2, 0, 50, 1), SelectionRange: rg(2, 5, 2, 10), Children: []langfeatures.DocumentSymbol{
			{Name: "loop", Kind: 13, Range: rg(3, 1, 48, 2), SelectionRange: rg(3, 1, 3, 4)},
		}},
		{Name: "Other", Kind: 12, Range: rg(52, 0, 52, 14), SelectionRange: rg(52, 5, 52, 10)},
	}
}

// symbolRig is an editor over stickySample with the provider above and a clock the test drives.
type symbolRig struct {
	t     *testing.T
	vm    *ViewModel
	reg   *langfeatures.Registry
	cache *langfeatures.SymbolCache
	prov  *fakeSymbols
	clock *fakeTimers
	posts chan func()
}

func newSymbolRig(t *testing.T, register bool) *symbolRig {
	t.Helper()
	doc := document.New(document.FileURI("/sample.go"), stickySample(), document.NewOptions())
	doc.SetLanguageID("go")
	opts := goOptions()
	opts.CursorBlinking = "solid"
	opts.OccurrencesHighlight, opts.OccurrencesHighlightDelay = "singleFile", 250
	opts.WordSeparators = edit.DefaultWordSeparators
	opts.StickyScroll = StickyScrollOptions{Enabled: true, MaxLineCount: 5, ScrollWithEditor: true}
	vm := NewViewModel(doc, opts, nil)
	r := &symbolRig{t: t, vm: vm, reg: langfeatures.NewRegistry(), prov: &fakeSymbols{syms: sampleSymbols()}, clock: &fakeTimers{}, posts: make(chan func(), 256)}
	r.cache = langfeatures.NewSymbolCache(t.Context(), r.reg)
	vm.SetTimer(r.clock.after)
	vm.SetPoster(func(fn func()) { r.posts <- fn })
	if register {
		sel := langfeatures.Selector{Languages: []string{"go"}}
		r.reg.RegisterSymbols("test", sel, r.prov)
		r.reg.RegisterHighlights("test", sel, r.prov)
	}
	vm.SetSuggest(&SuggestHost{Ctx: t.Context(), Features: r.reg, Symbols: r.cache})
	t.Cleanup(vm.DisposeSymbols)
	t.Cleanup(vm.DisposeHighlights)
	return r
}

func (r *symbolRig) settle() {
	for {
		select {
		case fn := <-r.posts:
			fn()
		case <-time.After(60 * time.Millisecond):
			return
		}
	}
}

func (r *symbolRig) edit(text string) {
	r.t.Helper()
	if _, err := r.vm.Document().Edit([]document.Edit{{Range: rg(0, 0, 0, 0), Text: text}}, document.EditOptions{}); err != nil {
		r.t.Fatal(err)
	}
}

func TestSymbolsAreAskedForWhenTheHostIsSet(t *testing.T) {
	r := newSymbolRig(t, true)
	r.settle()
	syms, ok := r.vm.Symbols()
	if !ok || len(syms) != 2 || syms[0].Name != "Outer" || r.prov.callCount() != 1 {
		t.Fatalf("symbols = %v, %v after %d calls", syms, ok, r.prov.callCount())
	}
	if r.vm.SymbolsVersion() != r.vm.Document().Version() {
		t.Errorf("version = %d", r.vm.SymbolsVersion())
	}
}

func TestEditorsOfADocumentShareTheRequest(t *testing.T) {
	r := newSymbolRig(t, true)
	r.settle()
	other := NewViewModel(r.vm.Document(), r.vm.Options(), nil)
	other.SetPoster(func(fn func()) { r.posts <- fn })
	other.SetSuggest(&SuggestHost{Ctx: t.Context(), Features: r.reg, Symbols: r.cache})
	t.Cleanup(other.DisposeSymbols)
	r.settle()
	if syms, ok := other.Symbols(); !ok || len(syms) != 2 || r.prov.callCount() != 1 {
		t.Errorf("the second editor has %v, %v after %d calls", syms, ok, r.prov.callCount())
	}
}

func TestTypingAsksOnceAfterTheDelay(t *testing.T) {
	r := newSymbolRig(t, true)
	r.settle()
	v0 := r.vm.SymbolsVersion()
	r.edit("a")
	r.clock.advance(100 * time.Millisecond)
	r.edit("b")
	r.clock.advance(250 * time.Millisecond)
	r.settle()
	if r.prov.callCount() != 1 {
		t.Fatalf("asked %d times before the delay was over", r.prov.callCount())
	}
	r.clock.advance(60 * time.Millisecond)
	r.settle()
	if r.prov.callCount() != 2 || r.vm.SymbolsVersion() <= v0 || r.vm.SymbolsVersion() != r.vm.Document().Version() {
		t.Errorf("calls = %d, version %d (document %d)", r.prov.callCount(), r.vm.SymbolsVersion(), r.vm.Document().Version())
	}
}

func TestAnAnswerForAnOlderVersionIsDropped(t *testing.T) {
	r := newSymbolRig(t, true)
	r.settle()
	r.prov.mu.Lock()
	r.prov.gate = make(chan struct{})
	first := []langfeatures.DocumentSymbol{{Name: "old", Kind: 12, Range: rg(0, 0, 1, 0), SelectionRange: rg(0, 0, 0, 1)}}
	r.prov.syms = first
	r.prov.mu.Unlock()
	r.edit("a")
	r.clock.advance(300 * time.Millisecond) // the request for the version that is gone starts and waits
	for r.prov.callCount() < 2 {
		time.Sleep(time.Millisecond)
	}
	r.edit("b")
	r.prov.mu.Lock()
	r.prov.syms = sampleSymbols()
	r.prov.mu.Unlock()
	r.clock.advance(300 * time.Millisecond)
	close(r.prov.gate)
	r.settle()
	syms, _ := r.vm.Symbols()
	if len(syms) != 2 || syms[0].Name != "Outer" || r.vm.SymbolsVersion() != r.vm.Document().Version() {
		t.Errorf("symbols = %v at %d (document %d)", syms, r.vm.SymbolsVersion(), r.vm.Document().Version())
	}
}

func TestNoProviderNoSymbols(t *testing.T) {
	r := newSymbolRig(t, false)
	r.settle()
	if _, ok := r.vm.Symbols(); ok {
		t.Error("an editor with no provider has symbols")
	}
	// a server that starts later is noticed
	sel := langfeatures.Selector{Languages: []string{"go"}}
	r.reg.RegisterSymbols("late", sel, r.prov)
	r.vm.FeaturesChanged()
	r.settle()
	if syms, ok := r.vm.Symbols(); !ok || len(syms) != 2 {
		t.Errorf("symbols = %v, %v", syms, ok)
	}
}

func TestSymbolPathFollowsTheCursor(t *testing.T) {
	r := newSymbolRig(t, true)
	r.settle()
	names := func() []string {
		var out []string
		for _, s := range r.vm.SymbolPath(nil) {
			out = append(out, s.Name)
		}
		return out
	}
	cursorAt(r.vm, 10, 4)
	if got := names(); len(got) != 2 || got[0] != "Outer" || got[1] != "loop" {
		t.Errorf("inside the loop: %v", got)
	}
	cursorAt(r.vm, 49, 1)
	if got := names(); len(got) != 1 || got[0] != "Outer" {
		t.Errorf("after the loop: %v", got)
	}
	cursorAt(r.vm, 51, 0)
	if got := names(); len(got) != 0 {
		t.Errorf("between the functions: %v", got)
	}
	// a kind the settings hide is left out of the path
	cursorAt(r.vm, 10, 4)
	got := r.vm.SymbolPath(func(s langfeatures.DocumentSymbol) bool { return s.Kind != 13 })
	if len(got) != 1 || got[0].Name != "Outer" {
		t.Errorf("without variables: %v", got)
	}
}

func TestRevealSymbolMovesTheCursorToItsName(t *testing.T) {
	r := newSymbolRig(t, true)
	r.settle()
	syms, _ := r.vm.Symbols()
	var jumped []buffer.Position
	h := r.vm.sg.host
	h.JumpTo = func(_ *ViewModel, p buffer.Position) { jumped = append(jumped, p) }
	r.vm.RevealSymbol(&syms[1])
	if len(jumped) != 1 || jumped[0] != positionOf(52, 5) {
		t.Errorf("jumped = %v", jumped)
	}
	h.JumpTo = nil
	r.vm.RevealSymbol(&syms[0])
	if got := r.vm.Cursors().Primary().Position(); got != positionOf(2, 5) {
		t.Errorf("cursor = %v", got)
	}
}

func TestStickyScrollUsesTheSymbolsAndFallsBack(t *testing.T) {
	r := newSymbolRig(t, true)
	r.vm.viewport = 400
	r.vm.opts.LineHeight = 19
	r.settle()
	if !r.vm.stickyUsesSymbols() {
		t.Fatal("sticky scroll does not use the symbols")
	}
	// scrolled into the loop: the function and the loop stick (the line of the name of each)
	lines := r.vm.stickyLinesAt(10 * 19)
	if len(lines.Lines) != 2 || lines.Lines[0] != 2 || lines.Lines[1] != 3 {
		t.Errorf("lines = %v", lines.Lines)
	}
	// the setting picks the folding model
	o := r.vm.Options()
	o.StickyScroll.DefaultModel = "foldingProviderModel"
	r.vm.SetOptions(o)
	if r.vm.stickyUsesSymbols() {
		t.Error("the folding model was asked for")
	}
	// a provider that has no symbols leaves the folding scopes
	o.StickyScroll.DefaultModel = "outlineModel"
	r.vm.SetOptions(o)
	r.prov.mu.Lock()
	r.prov.syms = nil
	r.prov.mu.Unlock()
	r.edit("//\n")
	r.clock.advance(300 * time.Millisecond)
	r.settle()
	if r.vm.stickyUsesSymbols() {
		t.Error("an empty answer feeds the sticky scopes")
	}
}

func TestDocumentHighlightsReplaceTheWordsByKind(t *testing.T) {
	r := newSymbolRig(t, true)
	r.settle()
	r.prov.mu.Lock()
	r.prov.hls = []langfeatures.DocumentHighlight{
		{Range: rg(2, 5, 2, 10), Kind: langfeatures.HighlightWrite},
		{Range: rg(60, 0, 60, 0), Kind: langfeatures.HighlightRead}, // beyond the text: made valid
		{Range: rg(52, 5, 52, 10), Kind: langfeatures.HighlightText},
	}
	r.prov.mu.Unlock()
	cursorAt(r.vm, 2, 7)
	r.vm.updateWordHighlight(false)
	r.clock.advance(250 * time.Millisecond)
	r.settle()
	classes := map[buffer.Range]string{}
	for _, d := range r.vm.Document().DecorationsOf(r.vm.owner(wordHighlightOwner)) {
		classes[d.Range] = d.Options.ClassName
	}
	if len(classes) != 3 || classes[rg(2, 5, 2, 10)] != classWordHighlightStrong || classes[rg(52, 5, 52, 10)] != classWordHighlightText {
		t.Errorf("classes = %v", classes)
	}
	var read int
	for _, c := range classes {
		if c == classWordHighlight {
			read++
		}
	}
	if read != 1 {
		t.Errorf("read marks = %d in %v", read, classes)
	}
	// a provider with nothing to say leaves the textual occurrences
	r.prov.mu.Lock()
	r.prov.hls = nil
	r.prov.mu.Unlock()
	cursorAt(r.vm, 0, 2)
	r.vm.updateWordHighlight(false)
	r.clock.advance(250 * time.Millisecond)
	r.settle()
	for _, d := range r.vm.Document().DecorationsOf(r.vm.owner(wordHighlightOwner)) {
		if d.Options.ClassName != classWordHighlightText {
			t.Errorf("a textual occurrence has the class %q", d.Options.ClassName)
		}
	}
}

func TestStaleHighlightsAreDropped(t *testing.T) {
	r := newSymbolRig(t, true)
	r.settle()
	r.prov.mu.Lock()
	r.prov.hls = []langfeatures.DocumentHighlight{{Range: rg(2, 5, 2, 10), Kind: langfeatures.HighlightRead}}
	r.prov.mu.Unlock()
	cursorAt(r.vm, 2, 7)
	r.vm.updateWordHighlight(false)
	r.clock.advance(250 * time.Millisecond)
	r.edit("x") // the version changes before the answer is taken
	r.settle()
	for _, d := range r.vm.Document().DecorationsOf(r.vm.owner(wordHighlightOwner)) {
		if d.Options.ClassName == classWordHighlight {
			t.Error("an answer for an older version was marked")
		}
	}
}
