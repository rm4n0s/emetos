package editor

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
)

// fakeHover is a hover provider the tests drive: what it answers, the positions it was asked at, and a gate that holds the
// answer until the test opens it. ctxErr is the state of the context of the last request when it ended.
type fakeHover struct {
	mu     sync.Mutex
	hover  *langfeatures.Hover
	err    error
	gate   chan struct{}
	calls  []langfeatures.Position
	ended  chan error
	answer func(pos langfeatures.Position) *langfeatures.Hover
}

func (f *fakeHover) ProvideHover(ctx context.Context, _ langfeatures.Document, pos langfeatures.Position) (*langfeatures.Hover, error) {
	f.mu.Lock()
	f.calls = append(f.calls, pos)
	gate, h, err, answer := f.gate, f.hover, f.err, f.answer
	f.mu.Unlock()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			if f.ended != nil {
				f.ended <- ctx.Err()
			}
			return nil, ctx.Err()
		}
	}
	if answer != nil {
		return answer(pos), err
	}
	return h, err
}

func (f *fakeHover) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (f *fakeHover) lastPosition() langfeatures.Position {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[len(f.calls)-1]
}

func md(value string) langfeatures.Hover {
	return langfeatures.Hover{Contents: []langfeatures.Markup{{Value: value, Markdown: true}}}
}

// hoverProgram is the program the hover tests hover over: line 2 has "fmt.Println(x)", line 3 "x := undefined".
const hoverProgram = "package main\n\nfunc main() {\n\tfmt.Println(x)\n\tx := undefined\n}\n"

// providerRig is a view model with hover providers, a clock and a poster the test drives.
type providerRig struct {
	t        *testing.T
	vm       *ViewModel
	clock    *fakeTimers
	reg      *langfeatures.Registry
	posts    chan func()
	logged   []string
	runLogMu sync.Mutex
}

func newProviderRig(t *testing.T, tweak func(*Options)) *providerRig {
	t.Helper()
	opts := goOptions()
	opts.CursorBlinking = "solid"
	opts.Hover = HoverOptions{Enabled: "on", Delay: 300, Sticky: true, HidingDelay: 300, Above: true}
	opts.ParameterHints = ParameterHintsOptions{Enabled: true, Cycle: true}
	if tweak != nil {
		tweak(&opts)
	}
	doc := newTestDoc(hoverProgram)
	doc.SetLanguageID("go")
	vm := NewViewModel(doc, opts, nil)
	r := &providerRig{t: t, vm: vm, clock: &fakeTimers{}, reg: langfeatures.NewRegistry(), posts: make(chan func(), 256)}
	vm.SetTimer(r.clock.after)
	vm.SetPoster(func(fn func()) { r.posts <- fn })
	vm.SetSuggest(&SuggestHost{Ctx: t.Context(), Features: r.reg, Log: func(msg string, err error) {
		r.runLogMu.Lock()
		defer r.runLogMu.Unlock()
		r.logged = append(r.logged, msg)
	}})
	t.Cleanup(vm.HideHover)
	t.Cleanup(vm.SignatureCancel)
	return r
}

func (r *providerRig) provider(owner string, h *langfeatures.Hover) *fakeHover {
	f := &fakeHover{hover: h}
	r.reg.RegisterHover(owner, langfeatures.Selector{Languages: []string{"go"}}, f)
	return f
}

// waitFor runs what is posted to the UI thread until the condition holds.
func (r *providerRig) waitFor(what string, cond func() bool) {
	r.t.Helper()
	deadline := time.After(3 * time.Second)
	for !cond() {
		select {
		case fn := <-r.posts:
			fn()
		case <-deadline:
			r.t.Fatalf("timed out waiting for %s", what)
		case <-time.After(2 * time.Millisecond):
		}
	}
}

// settle runs what is posted until nothing comes for a short while.
func (r *providerRig) settle() {
	for {
		select {
		case fn := <-r.posts:
			fn()
		case <-time.After(60 * time.Millisecond):
			return
		}
	}
}

// parts are the texts of the parts of the hover that shows: the message of a diagnostic, the first text of a provider.
func (r *providerRig) parts() []string {
	h := r.vm.Hover()
	if h == nil {
		return nil
	}
	var out []string
	for _, p := range h.Parts {
		if p.Kind == HoverPartDiagnostic {
			out = append(out, "diagnostic: "+p.Diagnostic.Message)
		} else {
			out = append(out, p.Contents[0].Value)
		}
	}
	return out
}

func TestHoverProvidersFollowTheTimingOfVSCode(t *testing.T) {
	r := newProviderRig(t, nil)
	f := r.provider("a", &langfeatures.Hover{Contents: []langfeatures.Markup{{Value: "**Println**", Markdown: true}}})
	over := positionOf(3, 6) // in "Println"
	r.vm.HoverPointerMoved(over)
	r.clock.advance(149 * time.Millisecond)
	if f.callCount() != 0 {
		t.Fatal("the provider is asked before half of the delay")
	}
	r.clock.advance(time.Millisecond)
	r.waitFor("the request", func() bool { return f.callCount() == 1 })
	if got := f.lastPosition(); got != over {
		t.Errorf("asked at %v, want %v", got, over)
	}
	r.settle() // the answer is here, and is not shown yet
	if r.vm.Hover() != nil {
		t.Fatal("the answer shows before the delay")
	}
	r.clock.advance(149 * time.Millisecond)
	if r.vm.Hover() != nil {
		t.Fatal("the hover shows before the delay")
	}
	r.clock.advance(time.Millisecond)
	h := r.vm.Hover()
	if h == nil || !slices.Equal(r.parts(), []string{"**Println**"}) || !h.Complete || h.Pinned {
		t.Fatalf("hover = %+v", h)
	}
	if want := (buffer.Range{Start: positionOf(3, 5), End: positionOf(3, 12)}); h.Range != want {
		t.Errorf("the hover is over %v, want the word %v", h.Range, want)
	}
	// the pointer moving inside the word does not ask again
	r.vm.HoverPointerMoved(positionOf(3, 8))
	r.clock.advance(time.Second)
	r.settle()
	if f.callCount() != 1 || r.vm.Hover() == nil {
		t.Errorf("%d requests, hover %v", f.callCount(), r.vm.Hover())
	}
}

func TestHoverSlowProviderShowsLoadingAfterThreeTimesTheDelay(t *testing.T) {
	r := newProviderRig(t, nil)
	f := r.provider("a", &langfeatures.Hover{Contents: []langfeatures.Markup{{Value: "docs"}}})
	f.gate = make(chan struct{})
	r.vm.SetDiagnostics([]Diagnostic{{Range: buffer.Range{Start: positionOf(4, 6), End: positionOf(4, 15)}, Severity: DiagnosticError, Message: "undefined: undefined"}})
	r.vm.HoverPointerMoved(positionOf(4, 8))
	r.clock.advance(300 * time.Millisecond)
	// the diagnostics are there at the delay, the provider is still working
	if got := r.parts(); !slices.Equal(got, []string{"diagnostic: undefined: undefined"}) || r.vm.Hover().Complete {
		t.Fatalf("parts = %q", got)
	}
	r.clock.advance(599 * time.Millisecond)
	if len(r.parts()) != 1 {
		t.Fatal("Loading... shows before three times the delay")
	}
	r.clock.advance(time.Millisecond)
	if got := r.parts(); !slices.Equal(got, []string{"diagnostic: undefined: undefined", "Loading..."}) {
		t.Fatalf("parts = %q", got)
	}
	close(f.gate)
	r.waitFor("the answer", func() bool { return r.vm.Hover() != nil && r.vm.Hover().Complete })
	if got := r.parts(); !slices.Equal(got, []string{"diagnostic: undefined: undefined", "docs"}) {
		t.Errorf("parts = %q, want the diagnostic and the answer, no Loading...", got)
	}
}

func TestHoverStacksProvidersAndDiagnosticsInOrder(t *testing.T) {
	r := newProviderRig(t, nil)
	first := r.provider("first", &langfeatures.Hover{Contents: []langfeatures.Markup{{Value: "from the first"}}})
	second := r.provider("second", &langfeatures.Hover{Contents: []langfeatures.Markup{{Value: "from the second"}, {Value: "more from the second"}}})
	// the provider that registered last is asked first (VS Code's order), and an answer that comes first is not shown first
	second.gate = make(chan struct{})
	r.vm.SetDiagnostics([]Diagnostic{
		{Range: buffer.Range{Start: positionOf(3, 5), End: positionOf(3, 12)}, Severity: DiagnosticWarning, Message: "warning"},
		{Range: buffer.Range{Start: positionOf(3, 5), End: positionOf(3, 12)}, Severity: DiagnosticError, Message: "error"},
	})
	r.vm.HoverPointerMoved(positionOf(3, 6))
	r.clock.advance(300 * time.Millisecond)
	r.waitFor("the first answer", func() bool { return len(r.parts()) == 3 })
	close(second.gate)
	r.waitFor("the complete hover", func() bool { return r.vm.Hover().Complete })
	want := []string{"diagnostic: error", "diagnostic: warning", "from the second", "from the first"}
	if got := r.parts(); !slices.Equal(got, want) {
		t.Errorf("parts = %q, want %q", got, want)
	}
	if n := len(r.vm.Hover().Parts[2].Contents); n != 2 {
		t.Errorf("the second provider has %d contents, want 2 in one part", n)
	}
	if first.callCount() != 1 || second.callCount() != 1 {
		t.Error("each provider is asked once")
	}
}

func TestHoverUsesTheRangeOfTheProvider(t *testing.T) {
	r := newProviderRig(t, nil)
	h := md("`fmt`")
	h.Range = &buffer.Range{Start: positionOf(3, 1), End: positionOf(3, 4)}
	r.provider("a", &h)
	r.vm.HoverPointerMoved(positionOf(3, 6))
	r.clock.advance(300 * time.Millisecond)
	r.waitFor("the hover", func() bool { return r.vm.Hover() != nil })
	if got := r.vm.Hover().Range; got != *h.Range {
		t.Errorf("range = %v, want the provider's %v", got, *h.Range)
	}
}

func TestHoverDropsStaleAnswers(t *testing.T) {
	t.Run("the pointer went to another word", func(t *testing.T) {
		r := newProviderRig(t, nil)
		f := r.provider("a", nil)
		f.answer = func(pos langfeatures.Position) *langfeatures.Hover {
			if pos.Line == 3 {
				return &langfeatures.Hover{Contents: []langfeatures.Markup{{Value: "old"}}}
			}
			return &langfeatures.Hover{Contents: []langfeatures.Markup{{Value: "new"}}}
		}
		f.gate = make(chan struct{})
		f.ended = make(chan error, 4)
		r.vm.HoverPointerMoved(positionOf(3, 6))
		r.clock.advance(150 * time.Millisecond)
		r.waitFor("the request", func() bool { return f.callCount() == 1 })
		// the sticky hover is not on screen yet, so the pointer going elsewhere replaces the wait at once
		r.vm.HoverPointerMoved(positionOf(4, 1))
		select {
		case err := <-f.ended:
			if !errors.Is(err, context.Canceled) {
				t.Errorf("the request ended with %v, want a cancel", err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("the request of the word the pointer left is not cancelled")
		}
		close(f.gate)
		r.clock.advance(time.Second)
		r.settle()
		if got := r.parts(); !slices.Equal(got, []string{"new"}) {
			t.Errorf("parts = %q, want only the answer for the word the pointer is on", got)
		}
	})
	t.Run("the text changed", func(t *testing.T) {
		r := newProviderRig(t, nil)
		f := r.provider("a", &langfeatures.Hover{Contents: []langfeatures.Markup{{Value: "old"}}})
		f.gate = make(chan struct{})
		r.vm.HoverPointerMoved(positionOf(3, 6))
		r.clock.advance(300 * time.Millisecond)
		r.waitFor("the request", func() bool { return f.callCount() == 1 })
		cursorAt(r.vm, 0, 0)
		if err := r.vm.RunCommand("type", "x"); err != nil {
			t.Fatal(err)
		}
		close(f.gate)
		r.settle()
		if r.vm.Hover() != nil {
			t.Errorf("the answer about the old text shows: %q", r.parts())
		}
	})
}

func TestHoverCancelsTheRequestWhenItClosesAndLogsFailures(t *testing.T) {
	r := newProviderRig(t, nil)
	f := r.provider("a", nil)
	f.gate = make(chan struct{})
	f.ended = make(chan error, 1)
	r.vm.HoverPointerMoved(positionOf(3, 6))
	r.clock.advance(150 * time.Millisecond)
	r.waitFor("the request", func() bool { return f.callCount() == 1 })
	r.vm.HideHover()
	select {
	case err := <-f.ended:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("ended with %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("closing the hover does not cancel the request")
	}
	// a provider that fails is logged and its failure does not show
	r2 := newProviderRig(t, nil)
	bad := r2.provider("bad", nil)
	bad.err = errors.New("HoverTestFailure", "the provider fails")
	good := r2.provider("good", &langfeatures.Hover{Contents: []langfeatures.Markup{{Value: "fine"}}})
	_ = good
	r2.vm.HoverPointerMoved(positionOf(3, 6))
	r2.clock.advance(300 * time.Millisecond)
	r2.waitFor("the answer", func() bool { return r2.vm.Hover() != nil && r2.vm.Hover().Complete })
	if got := r2.parts(); !slices.Equal(got, []string{"fine"}) {
		t.Errorf("parts = %q", got)
	}
	r2.runLogMu.Lock()
	defer r2.runLogMu.Unlock()
	if len(r2.logged) != 1 {
		t.Errorf("logged %q, want one failure", r2.logged)
	}
}

func TestHoverAsksOnlyWhereThereIsText(t *testing.T) {
	r := newProviderRig(t, nil)
	f := r.provider("a", &langfeatures.Hover{Contents: []langfeatures.Markup{{Value: "x"}}})
	for _, p := range []buffer.Position{positionOf(1, 0), positionOf(3, 0), positionOf(3, 50)} {
		r.vm.HoverPointerMoved(p) // an empty line, white space, past the end of the line
		r.clock.advance(time.Second)
	}
	r.settle()
	if f.callCount() != 0 {
		t.Errorf("the provider is asked %d times for places with no text", f.callCount())
	}
	// a punctuation mark is a place too
	r.vm.HoverPointerMoved(positionOf(3, 4))
	r.clock.advance(300 * time.Millisecond)
	r.waitFor("the request", func() bool { return f.callCount() == 1 })
}

func TestHoverShowHoverCommandShowsFocusesAndHides(t *testing.T) {
	r := newProviderRig(t, nil)
	f := r.provider("a", &langfeatures.Hover{Contents: []langfeatures.Markup{{Value: "docs"}}})
	cursorAt(r.vm, 3, 12) // at the end of Println
	if err := r.vm.RunCommand("editor.action.showHover", nil); err != nil {
		t.Fatal(err)
	}
	// no wait: the request goes out at once and the answer shows as it comes
	r.waitFor("the request", func() bool { return f.callCount() == 1 })
	r.waitFor("the hover", func() bool { return r.vm.Hover() != nil })
	if h := r.vm.Hover(); !h.Pinned || h.Focused || r.vm.HoverFocused() || !r.vm.HoverVisible() {
		t.Fatalf("hover = %+v", h)
	}
	if got := f.lastPosition(); got != positionOf(3, 12) {
		t.Errorf("asked at %v, want the cursor", got)
	}
	// the pointer does not take a pinned hover away
	r.vm.HoverPointerMoved(positionOf(4, 1))
	r.vm.HoverPointerLeft()
	r.clock.advance(5 * time.Second)
	if r.vm.Hover() == nil {
		t.Fatal("the pointer hid the hover of the command")
	}
	// a second press gives the box the keys
	if err := r.vm.RunCommand("editor.action.showHover", nil); err != nil {
		t.Fatal(err)
	}
	if !r.vm.HoverFocused() || !r.vm.Hover().Focused {
		t.Error("the second press does not focus the hover")
	}
	scrolled := []HoverScroll{}
	r.vm.SetHoverScroller(func(how HoverScroll) { scrolled = append(scrolled, how) })
	for id, want := range map[string]HoverScroll{
		"editor.action.scrollUpHover": HoverScrollUp, "editor.action.scrollDownHover": HoverScrollDown,
		"editor.action.pageUpHover": HoverScrollPageUp, "editor.action.pageDownHover": HoverScrollPageDown,
		"editor.action.goToTopHover": HoverScrollTop, "editor.action.goToBottomHover": HoverScrollBottom,
		"editor.action.scrollLeftHover": HoverScrollLeft, "editor.action.scrollRightHover": HoverScrollRight,
	} {
		scrolled = scrolled[:0]
		if err := r.vm.RunCommand(id, nil); err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(scrolled, []HoverScroll{want}) {
			t.Errorf("%s scrolled %v, want %v", id, scrolled, want)
		}
	}
	if err := r.vm.RunCommand("editor.action.hideHover", nil); err != nil {
		t.Fatal(err)
	}
	if r.vm.Hover() != nil || r.vm.HoverFocused() {
		t.Error("Hide Hover leaves the hover")
	}
	// a hover with nothing to say does not stay: the pointer may have its own afterwards
	cursorAt(r.vm, 1, 0)
	if err := r.vm.RunCommand("editor.action.showHover", nil); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.hover = nil
	f.mu.Unlock()
	r.settle()
	if r.vm.hoverMouseBlocked() {
		t.Error("an empty hover of the command blocks the pointer")
	}
}

func TestHoverStaysWhileTheBoxIsFocusedAndHidesWhenTheCursorMoves(t *testing.T) {
	r := newProviderRig(t, nil)
	r.provider("a", &langfeatures.Hover{Contents: []langfeatures.Markup{{Value: "docs"}}})
	cursorAt(r.vm, 3, 8)
	_ = r.vm.RunCommand("editor.action.showHover", nil)
	r.waitFor("the hover", func() bool { return r.vm.Hover() != nil })
	_ = r.vm.RunCommand("editor.action.showHover", nil)
	r.vm.HoverPointerLeft()
	r.clock.advance(5 * time.Second)
	if r.vm.Hover() == nil {
		t.Fatal("the focused hover went away")
	}
	if err := r.vm.RunCommand("cursorRight", nil); err != nil {
		t.Fatal(err)
	}
	if r.vm.Hover() != nil || r.vm.HoverFocused() {
		t.Error("the hover stays after the cursor moved")
	}
}

func TestHoverProviderHoverIsStickyAndSettingsApply(t *testing.T) {
	r := newProviderRig(t, nil)
	r.provider("a", &langfeatures.Hover{Contents: []langfeatures.Markup{{Value: "docs"}}})
	r.vm.HoverPointerMoved(positionOf(3, 6))
	r.clock.advance(300 * time.Millisecond)
	r.waitFor("the hover", func() bool { return r.vm.Hover() != nil })
	// the pointer leaves the word: the hover waits for the hiding delay, and a pointer that got into the box keeps it
	r.vm.HoverPointerMoved(positionOf(2, 0))
	r.clock.advance(299 * time.Millisecond)
	if r.vm.Hover() == nil {
		t.Fatal("the hover hid before the hiding delay")
	}
	r.vm.HoverBoxEntered(true)
	r.clock.advance(5 * time.Second)
	if r.vm.Hover() == nil {
		t.Fatal("the hover went away under the pointer")
	}
	r.vm.HoverBoxEntered(false)
	r.clock.advance(300 * time.Millisecond)
	if r.vm.Hover() != nil {
		t.Error("the hover stays after the pointer left the box")
	}
	// editor.hover.enabled off asks nothing
	o := r.vm.Options()
	o.Hover.Enabled = "off"
	r.vm.SetOptions(o)
	r.vm.SetTimer(r.clock.after)
	r.vm.HoverPointerMoved(positionOf(3, 6))
	r.clock.advance(time.Second)
	r.settle()
	if r.vm.Hover() != nil {
		t.Error("a disabled hover shows")
	}
}

func TestHoverPointerLeavingBeforeTheDelayShowsNothing(t *testing.T) {
	r := newProviderRig(t, nil)
	f := r.provider("a", &langfeatures.Hover{Contents: []langfeatures.Markup{{Value: "docs"}}})
	r.vm.HoverPointerMoved(positionOf(3, 6))
	r.clock.advance(100 * time.Millisecond)
	r.vm.HoverPointerLeft()
	r.clock.advance(time.Second)
	r.settle()
	if f.callCount() != 0 || r.vm.Hover() != nil {
		t.Errorf("%d requests, hover %v", f.callCount(), r.vm.Hover())
	}
}
