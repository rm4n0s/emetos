package editor

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
)

// fakeSignature is a signature help provider the tests drive: the help it answers (per request, when answers is set), the
// contexts and positions it was asked with, and a gate that holds the answer.
type fakeSignature struct {
	mu                   sync.Mutex
	help                 *langfeatures.SignatureHelp
	triggers, retriggers []string
	contexts             []langfeatures.SignatureContext
	positions            []langfeatures.Position
	gate                 chan struct{}
	ended                chan error
}

func (f *fakeSignature) ProvideSignatureHelp(ctx context.Context, _ langfeatures.Document, pos langfeatures.Position, sc langfeatures.SignatureContext) (*langfeatures.SignatureHelp, error) {
	f.mu.Lock()
	f.contexts = append(f.contexts, sc)
	f.positions = append(f.positions, pos)
	gate, help := f.gate, f.help
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
	return help, nil
}

func (f *fakeSignature) SignatureCharacters() ([]string, []string) { return f.triggers, f.retriggers }

func (f *fakeSignature) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.contexts)
}

func (f *fakeSignature) last() langfeatures.SignatureContext {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.contexts[len(f.contexts)-1]
}

func intp(i int) *int { return &i }

// sampleHelp has two overloads of Println.
func sampleHelp() *langfeatures.SignatureHelp {
	return &langfeatures.SignatureHelp{
		Signatures: []langfeatures.Signature{
			{Label: "Println(a ...any) (n int, err error)", Documentation: &langfeatures.Markup{Value: "Println formats.", Markdown: true},
				Parameters: []langfeatures.Parameter{{Label: "a ...any", Documentation: &langfeatures.Markup{Value: "the operands"}}}},
			{Label: "Println(w io.Writer, a ...any)", Parameters: []langfeatures.Parameter{{Label: "w io.Writer"}, {Label: "a ...any"}}},
			{Label: "Println()"},
		},
		ActiveParameter: 0,
	}
}

func (r *providerRig) signature(help *langfeatures.SignatureHelp) *fakeSignature {
	f := &fakeSignature{help: help, triggers: []string{"(", ","}, retriggers: []string{")"}}
	r.reg.RegisterSignatureHelp("sig", langfeatures.Selector{Languages: []string{"go"}}, f)
	return f
}

// typeAndWait types the text and makes the delay of the model pass.
func (r *providerRig) typeChars(s string) {
	r.t.Helper()
	for _, c := range s {
		if err := r.vm.RunCommand("type", string(c)); err != nil {
			r.t.Fatal(err)
		}
	}
}

func (r *providerRig) hintsVisible() bool { return r.vm.SignatureHints() != nil }

// the text the signature tests type in: the cursor stands at the end of "fmt.Println".
func signatureRig(t *testing.T, help *langfeatures.SignatureHelp) (*providerRig, *fakeSignature) {
	r := newProviderRig(t, nil)
	f := r.signature(help)
	cursorAt(r.vm, 3, 12)
	return r, f
}

func TestSignatureHelpTriggersOnATriggerCharacter(t *testing.T) {
	r, f := signatureRig(t, sampleHelp())
	r.typeChars("(")
	r.clock.advance(119 * time.Millisecond)
	if f.count() != 0 {
		t.Fatal("the provider is asked before the delay of the model")
	}
	r.clock.advance(time.Millisecond)
	r.waitFor("the hints", r.hintsVisible)
	got := f.last()
	if got.Kind != langfeatures.SignatureTriggerCharacter || got.Character != "(" || got.IsRetrigger || got.Active != nil {
		t.Errorf("context = %+v", got)
	}
	if p := f.positions[0]; p != positionOf(3, 13) {
		t.Errorf("asked at %v, want after the (", p)
	}
	keys := r.vm.SignatureKeys()
	if !keys.Visible || !keys.Multiple || !keys.HasProvider {
		t.Errorf("keys = %+v", keys)
	}
}

func TestSignatureHelpWaitsForTheLastOfSeveralTriggers(t *testing.T) {
	r, f := signatureRig(t, sampleHelp())
	r.typeChars("(")
	r.clock.advance(50 * time.Millisecond)
	r.typeChars(",")
	r.clock.advance(50 * time.Millisecond)
	r.typeChars(",")
	r.clock.advance(119 * time.Millisecond)
	if f.count() != 0 {
		t.Fatal("asked before the delay after the last trigger")
	}
	r.clock.advance(time.Millisecond)
	r.waitFor("the hints", r.hintsVisible)
	if f.count() != 1 {
		t.Fatalf("the provider is asked %d times, want once", f.count())
	}
	if got := f.last(); got.Character != "," || got.IsRetrigger {
		t.Errorf("context = %+v, want the last character", got)
	}
}

func TestSignatureHelpIsRetriggeredWhileItShows(t *testing.T) {
	r, f := signatureRig(t, sampleHelp())
	r.typeChars("(")
	r.clock.advance(120 * time.Millisecond)
	r.waitFor("the hints", r.hintsVisible)
	r.vm.signatureSetActive(1) // the user went to the second overload
	r.typeChars(",")
	r.clock.advance(120 * time.Millisecond)
	r.waitFor("the second request", func() bool { return f.count() == 2 })
	got := f.last()
	if got.Kind != langfeatures.SignatureTriggerCharacter || got.Character != "," || !got.IsRetrigger {
		t.Errorf("context = %+v, want a retrigger", got)
	}
	if got.Active == nil || got.Active.ActiveSignature != 1 || len(got.Active.Signatures) != 3 {
		t.Errorf("the active help of the context is %+v", got.Active)
	}
	r.settle()
	if !r.hintsVisible() {
		t.Error("the hints went away while they were asked for again")
	}
}

func TestSignatureHelpRetriggerCharactersOnlyWhileTriggered(t *testing.T) {
	r, f := signatureRig(t, sampleHelp())
	r.typeChars(")") // not showing, so a retrigger character is just a character
	r.clock.advance(time.Second)
	if f.count() != 0 {
		t.Fatal("a retrigger character starts the hints")
	}
	r.typeChars("(")
	r.clock.advance(120 * time.Millisecond)
	r.waitFor("the hints", r.hintsVisible)
	r.typeChars(")")
	r.clock.advance(120 * time.Millisecond)
	r.waitFor("the request", func() bool { return f.count() == 2 })
	if got := f.last(); got.Character != ")" || !got.IsRetrigger || got.Kind != langfeatures.SignatureTriggerCharacter {
		t.Errorf("context = %+v", got)
	}
}

func TestSignatureHelpContentChangeAsksAgainOnlyWhileTriggered(t *testing.T) {
	r, f := signatureRig(t, sampleHelp())
	r.typeChars("x") // nothing showing: typing and moving ask nothing
	_ = r.vm.RunCommand("cursorLeft", nil)
	r.clock.advance(time.Second)
	if f.count() != 0 {
		t.Fatal("typing asks for hints that are not on")
	}
	if err := r.vm.RunCommand("editor.action.triggerParameterHints", nil); err != nil {
		t.Fatal(err)
	}
	r.waitFor("the hints", r.hintsVisible)
	if got := f.last(); got.Kind != langfeatures.SignatureInvoke || got.IsRetrigger || got.Character != "" {
		t.Errorf("context = %+v, want an invoke", got)
	}
	// a letter typed
	r.typeChars("y")
	r.clock.advance(120 * time.Millisecond)
	r.waitFor("the request", func() bool { return f.count() == 2 })
	if got := f.last(); got.Kind != langfeatures.SignatureContentChange || !got.IsRetrigger || got.Active == nil {
		t.Errorf("context = %+v, want a content change", got)
	}
	r.settle()
	// the cursor moved
	_ = r.vm.RunCommand("cursorLeft", nil)
	r.clock.advance(120 * time.Millisecond)
	r.waitFor("the request", func() bool { return f.count() == 3 })
	if got := f.last(); got.Kind != langfeatures.SignatureContentChange {
		t.Errorf("context = %+v", got)
	}
}

func TestSignatureHelpClosesWhenTheAnswerIsEmpty(t *testing.T) {
	r, f := signatureRig(t, sampleHelp())
	r.typeChars("(")
	r.clock.advance(120 * time.Millisecond)
	r.waitFor("the hints", r.hintsVisible)
	f.mu.Lock()
	f.help = nil
	f.mu.Unlock()
	r.typeChars(")")
	r.clock.advance(120 * time.Millisecond)
	r.waitFor("the hints to close", func() bool { return !r.hintsVisible() })
	if keys := r.vm.SignatureKeys(); keys.Visible || keys.Multiple {
		t.Errorf("keys = %+v", keys)
	}
}

func TestSignatureHelpClosesOnEscapeBlurAndTheCommand(t *testing.T) {
	for _, tc := range []struct {
		name  string
		close func(r *providerRig)
	}{
		{"closeParameterHints", func(r *providerRig) { _ = r.vm.RunCommand("closeParameterHints", nil) }},
		{"the editor loses the focus", func(r *providerRig) { r.vm.SetFocused(true); r.vm.SetFocused(false) }},
		{"the setting is turned off", func(r *providerRig) {
			o := r.vm.Options()
			o.ParameterHints.Enabled = false
			r.vm.SetOptions(o)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := signatureRig(t, sampleHelp())
			r.typeChars("(")
			r.clock.advance(120 * time.Millisecond)
			r.waitFor("the hints", r.hintsVisible)
			tc.close(r)
			if r.hintsVisible() {
				t.Error("the hints are still there")
			}
			// and typing does not bring back a retrigger of what was closed
			r.typeChars(")")
			r.clock.advance(time.Second)
			r.settle()
			if r.hintsVisible() {
				t.Error("the hints came back")
			}
		})
	}
}

func TestSignatureHelpIsNotRetriggeredAfterItWasClosed(t *testing.T) {
	r, f := signatureRig(t, sampleHelp())
	r.typeChars("(")
	r.clock.advance(120 * time.Millisecond)
	r.waitFor("the hints", r.hintsVisible)
	_ = r.vm.RunCommand("closeParameterHints", nil)
	r.typeChars(",")
	r.clock.advance(120 * time.Millisecond)
	r.waitFor("the request", func() bool { return f.count() == 2 })
	if got := f.last(); got.IsRetrigger || got.Active != nil {
		t.Errorf("context = %+v, want a fresh request", got)
	}
}

func TestSignatureHelpDisabledSettingStillAllowsTheCommand(t *testing.T) {
	r := newProviderRig(t, func(o *Options) { o.ParameterHints.Enabled = false })
	f := r.signature(sampleHelp())
	cursorAt(r.vm, 3, 12)
	r.typeChars("(")
	r.clock.advance(time.Second)
	if f.count() != 0 {
		t.Fatal("a disabled setting still starts the hints with a character")
	}
	if err := r.vm.RunCommand("editor.action.triggerParameterHints", nil); err != nil {
		t.Fatal(err)
	}
	r.waitFor("the hints", r.hintsVisible)
}

func TestSignatureHelpCancelsAnOutdatedRequestAndDropsItsAnswer(t *testing.T) {
	r, f := signatureRig(t, sampleHelp())
	f.gate = make(chan struct{})
	f.ended = make(chan error, 4)
	r.typeChars("(")
	r.clock.advance(120 * time.Millisecond)
	r.waitFor("the first request", func() bool { return f.count() == 1 })
	// a new trigger while the first request is pending: it is cancelled, and the second carries the first's active hints
	r.typeChars(",")
	r.clock.advance(120 * time.Millisecond)
	select {
	case err := <-f.ended:
		if err == nil {
			t.Error("the first request ended without a cancel")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the first request is not cancelled")
	}
	r.waitFor("the second request", func() bool { return f.count() == 2 })
	if got := f.last(); !got.IsRetrigger || got.Character != "," {
		t.Errorf("context = %+v, want a retrigger", got)
	}
	close(f.gate)
	r.waitFor("the hints", r.hintsVisible)
}

func TestSignatureHelpDropsAnAnswerAboutTextThatChanged(t *testing.T) {
	r, f := signatureRig(t, sampleHelp())
	f.gate = make(chan struct{})
	r.typeChars("(")
	r.clock.advance(120 * time.Millisecond)
	r.waitFor("the request", func() bool { return f.count() == 1 })
	// the document changes under the model, with no command of the editor to tell it
	if _, err := r.vm.doc.Edit([]document.Edit{{Range: buffer.Range{Start: positionOf(1, 0), End: positionOf(1, 0)}, Text: "// c"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	close(f.gate)
	r.settle()
	if r.hintsVisible() {
		t.Error("the answer about the old text shows")
	}
}

func TestSignatureHelpPendingRetriggerKeepsThePreviousActiveHelp(t *testing.T) {
	r, f := signatureRig(t, sampleHelp())
	r.typeChars("(")
	r.clock.advance(120 * time.Millisecond)
	r.waitFor("the hints", r.hintsVisible)
	r.vm.signatureSetActive(2)
	f.gate = make(chan struct{})
	r.typeChars(",")
	r.clock.advance(120 * time.Millisecond)
	r.waitFor("the second request", func() bool { return f.count() == 2 })
	// while the second request is pending a third trigger comes: its context still has the help that showed
	r.typeChars(",")
	r.clock.advance(120 * time.Millisecond)
	r.waitFor("the third request", func() bool { return f.count() == 3 })
	got := f.last()
	if got.Active == nil || got.Active.ActiveSignature != 2 || !got.IsRetrigger {
		t.Errorf("context = %+v, want the signature the user moved to", got)
	}
}

func TestSignatureHelpTakesTheFirstProviderThatAnswers(t *testing.T) {
	r := newProviderRig(t, nil)
	first := r.signature(&langfeatures.SignatureHelp{Signatures: []langfeatures.Signature{{Label: "first()"}}})
	second := &fakeSignature{help: &langfeatures.SignatureHelp{Signatures: []langfeatures.Signature{{Label: "second()"}}}, triggers: []string{"("}}
	r.reg.RegisterSignatureHelp("second", langfeatures.Selector{Languages: []string{"go"}}, second)
	cursorAt(r.vm, 3, 12)
	r.typeChars("(")
	r.clock.advance(120 * time.Millisecond)
	r.waitFor("the hints", r.hintsVisible)
	// the provider that registered last is first in line
	if got := r.vm.SignatureHints().Signatures[0].Label; got != "second()" {
		t.Errorf("hints = %q", got)
	}
	// and the other answers when the first has nothing
	second.mu.Lock()
	second.help = nil
	second.mu.Unlock()
	r.vm.SignatureCancel()
	r.typeChars(",")
	r.clock.advance(120 * time.Millisecond)
	r.waitFor("the hints", r.hintsVisible)
	if got := r.vm.SignatureHints().Signatures[0].Label; got != "first()" {
		t.Errorf("hints = %q", got)
	}
	_ = first
}

func TestSignatureHelpOverloadsCycleAndClose(t *testing.T) {
	for _, tc := range []struct {
		name    string
		cycle   bool
		moves   []string
		active  []int
		visible bool
	}{
		{"down through all, wrapping", true, []string{"showNextParameterHint", "showNextParameterHint", "showNextParameterHint"}, []int{1, 2, 0}, true},
		{"up wraps to the last", true, []string{"showPrevParameterHint"}, []int{2}, true},
		{"down on the last closes without cycle", false, []string{"showNextParameterHint", "showNextParameterHint", "showNextParameterHint"}, []int{1, 2}, false},
		{"up on the first closes without cycle", false, []string{"showPrevParameterHint"}, nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newProviderRig(t, func(o *Options) { o.ParameterHints.Cycle = tc.cycle })
			r.signature(sampleHelp())
			cursorAt(r.vm, 3, 12)
			r.typeChars("(")
			r.clock.advance(120 * time.Millisecond)
			r.waitFor("the hints", r.hintsVisible)
			var got []int
			for _, m := range tc.moves {
				if err := r.vm.RunCommand(m, nil); err != nil {
					t.Fatal(err)
				}
				if h := r.vm.SignatureHints(); h != nil {
					got = append(got, h.ActiveSignature)
				}
			}
			if !slices.Equal(got, tc.active) || r.hintsVisible() != tc.visible {
				t.Errorf("active signatures %v, visible %v; want %v, %v", got, r.hintsVisible(), tc.active, tc.visible)
			}
		})
	}
}

func TestSignatureHelpOneSignatureHasNoMultipleKey(t *testing.T) {
	r, _ := signatureRig(t, &langfeatures.SignatureHelp{Signatures: []langfeatures.Signature{{Label: "f()"}}})
	r.typeChars("(")
	r.clock.advance(120 * time.Millisecond)
	r.waitFor("the hints", r.hintsVisible)
	if k := r.vm.SignatureKeys(); !k.Visible || k.Multiple {
		t.Errorf("keys = %+v", k)
	}
}

func TestSignatureHelpActiveSignatureOutOfRangeIsTheFirst(t *testing.T) {
	h := sampleHelp()
	h.ActiveSignature = 9
	r, _ := signatureRig(t, h)
	r.typeChars("(")
	r.clock.advance(120 * time.Millisecond)
	r.waitFor("the hints", r.hintsVisible)
	if got := r.vm.SignatureHints().ActiveSignature; got != 0 {
		t.Errorf("active signature = %d", got)
	}
}

func TestSignatureCounterIsPadded(t *testing.T) {
	for _, tc := range []struct {
		active, count int
		want          string
	}{{0, 3, "1/3"}, {1, 12, "02/12"}, {11, 12, "12/12"}, {0, 100, "001/100"}} {
		if got := signatureCounter(tc.active, tc.count); got != tc.want {
			t.Errorf("counter(%d, %d) = %q, want %q", tc.active, tc.count, got, tc.want)
		}
	}
}

func TestSignatureActiveParameterOfTheSignatureWins(t *testing.T) {
	h := &langfeatures.SignatureHelp{ActiveParameter: 1, Signatures: []langfeatures.Signature{
		{Label: "f(a, b)"}, {Label: "g(a, b)", ActiveParameter: intp(0)},
	}}
	if _, got := activeParameter(h); got != 1 {
		t.Errorf("the help's active parameter = %d", got)
	}
	h.ActiveSignature = 1
	if _, got := activeParameter(h); got != 0 {
		t.Errorf("the signature's active parameter = %d", got)
	}
}
