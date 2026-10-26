package editor

import (
	"context"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
)

// The parameter hints (SPEC §9.12 "Signature help", "As built (step 12.6)"): the signatures of the function being called, with
// the parameter at the cursor marked. This file is the model, VS Code's ParameterHintsModel: when a request is made, how the
// answer is kept, and when the hints close. The widget is in signature_widget.go.
//
// Ported from VS Code: src/vs/editor/contrib/parameterHints/browser/parameterHintsModel.ts, parameterHints.ts and
// provideSignatureHelp.ts

// ParameterHintsOptions are the settings of the parameter hints (editor.parameterHints.*).
type ParameterHintsOptions struct {
	// Enabled is editor.parameterHints.enabled: typing a trigger character asks for the hints. The command always does.
	Enabled bool
	// Cycle is editor.parameterHints.cycle: Down on the last signature goes to the first, instead of closing the hints.
	Cycle bool
}

// signatureDelay is how long a trigger waits for the next before the request is made (the delay of VS Code's model).
const signatureDelay = 120

// sigState is the state of the model (VS Code's ParameterHintState).
type sigState int

const (
	sigDefault sigState = iota
	sigPending
	sigActive
)

// sigTrigger is why a request was asked for.
type sigTrigger struct {
	kind langfeatures.SignatureTrigger
	char string
}

// signatureModel is the parameter hints of a view model.
type signatureModel struct {
	state sigState
	// hints are the active hints (state sigActive); previous the hints that were active when the request that is pending was
	// made.
	hints, previous *langfeatures.SignatureHelp
	// shown is what the widget shows: it stays while the next request is pending, and goes when the hints close.
	shown *langfeatures.SignatureHelp
	// cancelRequest ends the request that is pending.
	cancelRequest context.CancelFunc
	// pending are the triggers that wait for the delay, cancelDelay the wait (nil when none) and id the number of the last
	// trigger: an answer for an older one is dropped.
	pending     []sigTrigger
	cancelDelay func()
	id          int
	changed     *foundation.ChangeNotifier
}

func newSignatureModel() signatureModel {
	return signatureModel{changed: foundation.NewChangeNotifier()}
}

// AddSignatureListener calls fn when the parameter hints show, change or close.
func (vm *ViewModel) AddSignatureListener(fn foundation.VoidCallback) foundation.ListenerHandle {
	return vm.sig.changed.AddListener(fn)
}

// RemoveSignatureListener removes a listener of AddSignatureListener.
func (vm *ViewModel) RemoveSignatureListener(h foundation.ListenerHandle) {
	vm.sig.changed.RemoveListener(h)
}

// SignatureHints are the hints that show now, or nil. The active signature is within the signatures.
func (vm *ViewModel) SignatureHints() *langfeatures.SignatureHelp { return vm.sig.shown }

// SignatureKeys are the context keys of the parameter hints.
type SignatureKeys struct {
	// Visible is parameterHintsVisible, Multiple parameterHintsMultipleSignatures and HasProvider editorHasSignatureHelpProvider.
	Visible, Multiple, HasProvider bool
}

// SignatureKeys are the context keys of the parameter hints now.
func (vm *ViewModel) SignatureKeys() SignatureKeys {
	k := SignatureKeys{HasProvider: vm.signatureHasProvider()}
	if h := vm.sig.shown; h != nil {
		k.Visible, k.Multiple = true, len(h.Signatures) > 1
	}
	return k
}

func (vm *ViewModel) signatureHasProvider() bool {
	h := vm.sg.host
	return h != nil && h.Features != nil && h.Features.HasSignatureHelp(vm.langDocument())
}

// signatureTriggered tells whether the hints show, are being asked for, or a request waits for its delay.
func (vm *ViewModel) signatureTriggered() bool {
	m := &vm.sig
	return m.state != sigDefault || m.cancelDelay != nil
}

// SignatureTrigger asks for the parameter hints at the cursor now (the command Trigger Parameter Hints, Ctrl+Shift+Space).
func (vm *ViewModel) SignatureTrigger() {
	vm.signatureTrigger(sigTrigger{kind: langfeatures.SignatureInvoke}, 0)
}

// signatureTrigger asks for the hints after a delay; asking again before the delay is over restarts it, and the triggers
// that waited are made one request.
//
// Ported from VS Code: parameterHintsModel.ts (trigger)
func (vm *ViewModel) signatureTrigger(t sigTrigger, delay int) {
	m := &vm.sig
	if !vm.signatureHasProvider() {
		return
	}
	m.id++
	id := m.id
	m.pending = append(m.pending, t)
	if m.cancelDelay != nil {
		m.cancelDelay()
		m.cancelDelay = nil
	}
	if delay <= 0 || vm.afterTimer == nil {
		vm.signatureDoTrigger(id)
		return
	}
	m.cancelDelay = vm.afterTimer(time.Duration(delay)*time.Millisecond, func() {
		m.cancelDelay = nil
		vm.signatureDoTrigger(id)
	})
}

// mergeSigTriggers is the reduction of VS Code's mergeTriggerContexts: an invoke or a typed character wins over what came
// before, a change of the content does not.
func mergeSigTriggers(ts []sigTrigger) sigTrigger {
	out := ts[0]
	for _, t := range ts[1:] {
		if t.kind != langfeatures.SignatureContentChange {
			out = t
		}
	}
	return out
}

// signatureDoTrigger makes the request.
//
// Ported from VS Code: parameterHintsModel.ts (doTrigger)
func (vm *ViewModel) signatureDoTrigger(id int) {
	m := &vm.sig
	isRetrigger := m.state == sigActive || m.state == sigPending
	active := vm.signatureLastActive()
	vm.signatureCancelQuiet()
	if len(m.pending) == 0 {
		return
	}
	t := mergeSigTriggers(m.pending)
	m.pending = nil
	h := vm.sg.host
	if h == nil || h.Features == nil {
		return
	}
	sc := langfeatures.SignatureContext{Kind: t.kind, Character: t.char, IsRetrigger: isRetrigger, Active: active}
	doc := vm.langDocument()
	pos := vm.cursors.Primary().Position()
	ctx := h.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	m.state, m.previous, m.cancelRequest = sigPending, active, cancel
	features := h.Features
	go func() {
		help, errs := features.SignatureHelp(ctx, doc, pos, sc)
		vm.post(func() { vm.signatureResult(id, doc.Version, help, errs) })
	}()
}

// signatureLastActive is the hints a request is made with as the active ones: those that show, else those that showed when the
// request that is pending was made.
func (vm *ViewModel) signatureLastActive() *langfeatures.SignatureHelp {
	m := &vm.sig
	switch m.state {
	case sigActive:
		return m.hints
	case sigPending:
		return m.previous
	}
	return nil
}

// signatureResult takes the answer of a request. An answer to an older request, or about a text that changed, is dropped.
func (vm *ViewModel) signatureResult(id, version int, help *langfeatures.SignatureHelp, errs []langfeatures.ProviderError) {
	m := &vm.sig
	if m.id != id || m.state != sigPending || vm.doc.Version() != version {
		return
	}
	for _, e := range errs {
		if h := vm.sg.host; h != nil && h.Log != nil && !errors.Is(e.Err, context.Canceled) {
			h.Log("signature help provider "+e.Owner+" failed", e.Err)
		}
	}
	if help == nil || len(help.Signatures) == 0 {
		vm.SignatureCancel()
		return
	}
	hints := *help
	if hints.ActiveSignature < 0 || hints.ActiveSignature >= len(hints.Signatures) {
		hints.ActiveSignature = 0
	}
	m.state, m.hints, m.previous, m.cancelRequest = sigActive, &hints, nil, nil
	m.shown = &hints
	m.changed.NotifyListeners()
	vm.sel.NotifyListeners()
}

// signatureCancelQuiet closes the request and the wait, and leaves the widget as it is (a request follows).
func (vm *ViewModel) signatureCancelQuiet() {
	m := &vm.sig
	if m.cancelRequest != nil {
		m.cancelRequest()
		m.cancelRequest = nil
	}
	m.state, m.hints, m.previous = sigDefault, nil, nil
	if m.cancelDelay != nil {
		m.cancelDelay()
		m.cancelDelay = nil
	}
}

// SignatureCancel closes the hints (the command Close Parameter Hints, Escape) and stops what waits to open them.
func (vm *ViewModel) SignatureCancel() {
	m := &vm.sig
	vm.signatureCancelQuiet()
	m.pending = nil
	if m.shown != nil {
		m.shown = nil
		m.changed.NotifyListeners()
		vm.sel.NotifyListeners()
	}
}

// SignatureNext shows the next signature (Down); on the last it goes to the first with editor.parameterHints.cycle, else the
// hints close.
//
// Ported from VS Code: parameterHintsModel.ts (next)
func (vm *ViewModel) SignatureNext() {
	m := &vm.sig
	if m.state != sigActive {
		return
	}
	n, active := len(m.hints.Signatures), m.hints.ActiveSignature
	last := active%n == n-1
	cycle := vm.opts.ParameterHints.Cycle
	if (n < 2 || last) && !cycle {
		vm.SignatureCancel()
		return
	}
	if last && cycle {
		vm.signatureSetActive(0)
		return
	}
	vm.signatureSetActive(active + 1)
}

// SignaturePrevious shows the previous signature (Up).
//
// Ported from VS Code: parameterHintsModel.ts (previous)
func (vm *ViewModel) SignaturePrevious() {
	m := &vm.sig
	if m.state != sigActive {
		return
	}
	n, active := len(m.hints.Signatures), m.hints.ActiveSignature
	first := active == 0
	cycle := vm.opts.ParameterHints.Cycle
	if (n < 2 || first) && !cycle {
		vm.SignatureCancel()
		return
	}
	if first && cycle {
		vm.signatureSetActive(n - 1)
		return
	}
	vm.signatureSetActive(active - 1)
}

func (vm *ViewModel) signatureSetActive(i int) {
	m := &vm.sig
	if m.state != sigActive {
		return
	}
	h := *m.hints
	h.ActiveSignature = i
	m.hints, m.shown = &h, &h
	m.changed.NotifyListeners()
	vm.sel.NotifyListeners()
}

// signatureAfter lets the hints see what a command did, in VS Code's order: a change of the text or of the cursor asks again
// when the hints show or are asked for, and a typed character asks when it is a trigger character, or a retrigger character
// while the hints are on. A cursor moved with the mouse closes them (the pointer does that, see signatureCancel).
func (vm *ViewModel) signatureAfter(pre suggestPre, typed string) {
	if vm.sg.host == nil {
		return
	}
	changed := vm.doc.Version() != pre.version
	moved := !buffer.SelectionsEqual(vm.cursors.Selections(), pre.sels)
	if (changed || moved) && vm.signatureTriggered() {
		vm.signatureTrigger(sigTrigger{kind: langfeatures.SignatureContentChange}, signatureDelay)
	}
	if typed == "" || !vm.opts.ParameterHints.Enabled {
		return
	}
	if vm.sg.host.Features == nil {
		return
	}
	last, _ := utf8.DecodeLastRuneInString(typed)
	c := string(last)
	triggers, retriggers := vm.sg.host.Features.SignatureCharacters(vm.langDocument())
	if slices.Contains(triggers, c) || vm.signatureTriggered() && slices.Contains(retriggers, c) {
		vm.signatureTrigger(sigTrigger{kind: langfeatures.SignatureTriggerCharacter, char: c}, signatureDelay)
	}
}

// signatureActions are the commands of the parameter hints.
//
// Ported from VS Code: src/vs/editor/contrib/parameterHints/browser/parameterHints.ts
func signatureActions(a map[string]action) {
	a["editor.action.triggerParameterHints"] = func(vm *ViewModel, _ any) error {
		vm.keepSessions, vm.skipReveal = true, true
		vm.SignatureTrigger()
		return nil
	}
	a["closeParameterHints"] = func(vm *ViewModel, _ any) error {
		vm.keepSessions, vm.skipReveal = true, true
		vm.SignatureCancel()
		return nil
	}
	a["showPrevParameterHint"] = func(vm *ViewModel, _ any) error {
		vm.keepSessions, vm.skipReveal = true, true
		vm.SignaturePrevious()
		return nil
	}
	a["showNextParameterHint"] = func(vm *ViewModel, _ any) error {
		vm.keepSessions, vm.skipReveal = true, true
		vm.SignatureNext()
		return nil
	}
}
