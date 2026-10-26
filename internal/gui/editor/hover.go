package editor

import (
	"time"

	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
)

// The hover (SPEC §9.12 "Hover", "As built (step 12.4)"): a box next to the text that tells what is under the pointer. Step
// 12.4 builds the hover and its first participant, the diagnostics of the position; step 12.6 adds the participants of the
// language providers, whose parts stack with these in the same box.
//
// Ported from VS Code: src/vs/editor/contrib/hover/browser/contentHoverController.ts, contentHoverWidget.ts and
// markerHoverParticipant.ts

// HoverOptions are the settings of the hover (editor.hover.*).
type HoverOptions struct {
	// Enabled is editor.hover.enabled: "on" (also when empty), "off" or "onKeyboardModifier".
	Enabled string
	// Delay is editor.hover.delay, the milliseconds the pointer rests on the text before the hover shows.
	Delay int
	// Sticky is editor.hover.sticky: the hover stays while the pointer is over it, and HidingDelay (editor.hover.hidingDelay)
	// is the milliseconds it waits to hide after the pointer left the text.
	Sticky      bool
	HidingDelay int
	// Above is editor.hover.above: the hover prefers the place above the line.
	Above bool
}

// HoverPartKind tells what a part of the hover is.
type HoverPartKind int

// The kinds of part of the hover.
const (
	// HoverPartDiagnostic is a diagnostic under the position: its message, source and code, and the places related to it.
	HoverPartDiagnostic HoverPartKind = iota
	// TODO(step 12.6): the Markdown part of the hover providers.
)

// HoverAction is a link in the row of actions below a part of the hover.
type HoverAction struct {
	// Label is the text of the link, Run what it does.
	Label string
	Run   func()
}

// HoverPart is one part of the content of the hover.
type HoverPart struct {
	Kind HoverPartKind
	// Diagnostic is the diagnostic of a HoverPartDiagnostic.
	Diagnostic Diagnostic
	// Actions are the links below the part. The diagnostics have "View Problem"; step 12.8 adds "Quick Fix..." and the quick
	// fixes of the diagnostic here.
	Actions []HoverAction
}

// Hover is what the hover shows: the parts, and the range of the text they are about (the hover is placed over it).
type Hover struct {
	Range buffer.Range
	Parts []HoverPart
	// Pinned says that the hover does not go away when the editor scrolls: the hover of a command (F8, Show Hover) stays until
	// the cursor moves or the text changes.
	Pinned bool
}

// hoverParticipant gives the parts of the hover for a position.
type hoverParticipant func(vm *ViewModel, p buffer.Position) (parts []HoverPart, anchor buffer.Range)

// hoverModel is the state of the hover of a view model.
type hoverModel struct {
	shown *Hover
	// pointerIn says that the pointer is over the hover box, which keeps a sticky hover.
	pointerIn bool
	// cancelShow cancels the wait to show, cancelHide the wait to hide; pending is the range the wait to show is for.
	cancelShow, cancelHide func()
	pending                buffer.Range
	hasPending             bool
	// selections are the selections when the hover came, which a different selection hides; unsubDoc stops the follow of the
	// document; scrollHandle is the listener of the scroll.
	selections   []buffer.Selection
	unsubDoc     func()
	scrollHandle foundation.ListenerHandle
	scrollOn     bool
	changed      *foundation.ChangeNotifier
}

func newHoverModel() hoverModel { return hoverModel{changed: foundation.NewChangeNotifier()} }

// Hover is the hover that shows now, or nil.
func (vm *ViewModel) Hover() *Hover { return vm.hover.shown }

// AddHoverListener calls fn when the hover shows, changes or hides.
func (vm *ViewModel) AddHoverListener(fn foundation.VoidCallback) foundation.ListenerHandle {
	return vm.hover.changed.AddListener(fn)
}

// RemoveHoverListener removes a listener of AddHoverListener.
func (vm *ViewModel) RemoveHoverListener(h foundation.ListenerHandle) {
	vm.hover.changed.RemoveListener(h)
}

// hoverParts asks the participants for the parts under a position: the diagnostics for now.
func (vm *ViewModel) hoverParts(p buffer.Position) ([]HoverPart, buffer.Range) {
	var parts []HoverPart
	var anchor buffer.Range
	for _, participant := range []hoverParticipant{diagnosticHover} {
		got, rng := participant(vm, p)
		if len(got) == 0 {
			continue
		}
		if len(parts) == 0 {
			anchor = rng
		}
		parts = append(parts, got...)
	}
	return parts, anchor
}

// diagnosticHover is the participant of the diagnostics: a part for each diagnostic under the position, the most serious
// first, anchored on the range of the first.
//
// Ported from VS Code: markerHoverParticipant.ts (computeSync)
func diagnosticHover(vm *ViewModel, p buffer.Position) ([]HoverPart, buffer.Range) {
	ds := vm.DiagnosticsAt(p)
	if len(ds) == 0 {
		return nil, buffer.Range{}
	}
	parts := make([]HoverPart, len(ds))
	for i, d := range ds {
		parts[i] = HoverPart{Kind: HoverPartDiagnostic, Diagnostic: d}
		if vm.viewProblem != nil {
			start := d.Range.Start
			parts[i].Actions = []HoverAction{{Label: "View Problem", Run: func() { vm.viewProblem(start) }}}
		}
	}
	return parts, ds[0].Range
}

// SetViewProblem gives the hover the function that "View Problem" runs, with the start of the diagnostic: the window shows the
// problem in the editor (what the marker navigation does for the marker at the position).
func (vm *ViewModel) SetViewProblem(fn func(p buffer.Position)) { vm.viewProblem = fn }

// hoverEnabled tells whether the pointer shows the hover.
func (vm *ViewModel) hoverEnabled() bool { return vm.opts.Hover.Enabled != "off" }

// HoverPointerMoved tells the hover that the pointer rests on the text at a position: after editor.hover.delay it shows what
// is there, and a hover that shows for another place hides.
func (vm *ViewModel) HoverPointerMoved(p buffer.Position) {
	h := &vm.hover
	if !vm.hoverEnabled() || vm.hoverMouseBlocked() {
		return
	}
	if h.shown != nil && rangeHolds(h.shown.Range, p) {
		vm.cancelHoverHide()
		vm.cancelHoverShow()
		return
	}
	parts, anchor := vm.hoverParts(p)
	if len(parts) == 0 {
		vm.cancelHoverShow()
		vm.scheduleHoverHide()
		return
	}
	vm.cancelHoverHide()
	if h.hasPending && rangeHolds(h.pending, p) {
		return // the pointer moves inside what it rested on: the wait goes on
	}
	vm.cancelHoverShow()
	h.pending, h.hasPending = anchor, true
	if vm.opts.Hover.Delay <= 0 || vm.afterTimer == nil {
		vm.showHover(p, false)
		return
	}
	h.cancelShow = vm.afterTimer(time.Duration(vm.opts.Hover.Delay)*time.Millisecond, func() {
		h.cancelShow, h.hasPending = nil, false
		vm.showHover(p, false)
	})
}

// hoverMouseBlocked tells whether the pointer cannot show the hover now: a pinned hover (the user asked for it) stays until
// something else removes it.
func (vm *ViewModel) hoverMouseBlocked() bool { return vm.hover.shown != nil && vm.hover.shown.Pinned }

// HoverPointerLeft tells the hover that the pointer left the text: a hover that shows hides after editor.hover.hidingDelay,
// unless the pointer went into it.
func (vm *ViewModel) HoverPointerLeft() {
	vm.cancelHoverShow()
	if vm.hoverMouseBlocked() {
		return
	}
	vm.scheduleHoverHide()
}

// HoverBoxEntered tells the hover that the pointer entered or left its box. With editor.hover.sticky a pointer in the box keeps
// the hover; one that left it starts the wait to hide.
func (vm *ViewModel) HoverBoxEntered(in bool) {
	h := &vm.hover
	h.pointerIn = in
	if in {
		vm.cancelHoverHide()
		return
	}
	if h.shown != nil && !h.shown.Pinned {
		vm.scheduleHoverHide()
	}
}

// ShowHoverAt shows the hover at a position at once, as the command Show Hover and the marker navigation do, and keeps it
// until the cursor moves or the text changes. It says whether there was something to show.
func (vm *ViewModel) ShowHoverAt(p buffer.Position) bool {
	vm.cancelHoverShow()
	vm.cancelHoverHide()
	return vm.showHover(p, true)
}

// showHover shows the parts under a position.
func (vm *ViewModel) showHover(p buffer.Position, pinned bool) bool {
	h := &vm.hover
	parts, anchor := vm.hoverParts(p)
	if len(parts) == 0 {
		vm.HideHover()
		return false
	}
	h.shown = &Hover{Range: anchor, Parts: parts, Pinned: pinned}
	h.selections = vm.cursors.Selections()
	if h.unsubDoc == nil {
		h.unsubDoc = vm.doc.Subscribe(func(document.ChangeEvent) { vm.HideHover() })
	}
	if !h.scrollOn {
		h.scrollHandle, h.scrollOn = vm.AddScrollListener(vm.hoverScrolled), true
	}
	h.changed.NotifyListeners()
	return true
}

// hoverScrolled hides a hover of the pointer when the editor scrolls.
func (vm *ViewModel) hoverScrolled() {
	if h := vm.hover.shown; h != nil && !h.Pinned {
		vm.HideHover()
	}
}

// HideHover hides the hover and cancels the waits.
func (vm *ViewModel) HideHover() {
	h := &vm.hover
	vm.cancelHoverShow()
	vm.cancelHoverHide()
	h.pointerIn = false
	if h.unsubDoc != nil {
		h.unsubDoc()
		h.unsubDoc = nil
	}
	if h.scrollOn {
		vm.RemoveScrollListener(h.scrollHandle)
		h.scrollOn = false
	}
	if h.shown == nil {
		return
	}
	h.shown = nil
	h.changed.NotifyListeners()
}

// hoverSelectionsChanged hides the hover when the selections are not those it came with.
func (vm *ViewModel) hoverSelectionsChanged() {
	h := &vm.hover
	if h.shown != nil && !buffer.SelectionsEqual(h.selections, vm.cursors.Selections()) {
		vm.HideHover()
	}
}

func (vm *ViewModel) cancelHoverShow() {
	h := &vm.hover
	if h.cancelShow != nil {
		h.cancelShow()
		h.cancelShow = nil
	}
	h.hasPending = false
}

func (vm *ViewModel) cancelHoverHide() {
	h := &vm.hover
	if h.cancelHide != nil {
		h.cancelHide()
		h.cancelHide = nil
	}
}

// scheduleHoverHide hides the hover after the hiding delay; without stickiness, at once.
func (vm *ViewModel) scheduleHoverHide() {
	h := &vm.hover
	if h.shown == nil || h.cancelHide != nil || h.pointerIn && vm.opts.Hover.Sticky {
		return
	}
	delay := vm.opts.Hover.HidingDelay
	if !vm.opts.Hover.Sticky || delay <= 0 || vm.afterTimer == nil {
		vm.HideHover()
		return
	}
	h.cancelHide = vm.afterTimer(time.Duration(delay)*time.Millisecond, func() {
		h.cancelHide = nil
		if !h.pointerIn {
			vm.HideHover()
		}
	})
}

// markersNavigationVisible tells whether the hover that a command opened (the marker navigation) is open: Escape closes it.
func (vm *ViewModel) markersNavigationVisible() bool {
	h := vm.hover.shown
	return h != nil && h.Pinned
}
