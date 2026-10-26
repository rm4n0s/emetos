package editor

import (
	"context"
	"slices"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/internal/core/buffer"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/langfeatures"
)

// The hover (SPEC §9.12 "Hover", "As built (step 12.4)", "As built (step 12.6)"): a box next to the text that tells what is
// under the pointer or at the cursor. Its content comes from participants: the diagnostics of the position (computed at once)
// and the hover providers of the language (asked off the UI thread, their answers stacked as they come), under the timing of
// VS Code's HoverOperation.
//
// Ported from VS Code: src/vs/editor/contrib/hover/browser/contentHoverController.ts, contentHoverWidgetWrapper.ts,
// hoverOperation.ts, markdownHoverParticipant.ts and markerHoverParticipant.ts

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
	// HoverPartMarkdown is the content of a hover provider: Markdown or plain text, with code blocks in the colors of their
	// language.
	HoverPartMarkdown
)

// The order of the parts in the box: the participants in the order of VS Code's hoverOrdinal, and, in the Markdown
// participant, the order of the providers (the loading message is last).
const (
	hoverOrdinalDiagnostic = 1
	hoverOrdinalMarkdown   = 3
	hoverOrdinalLoading    = 2000
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
	// Contents are the texts of a HoverPartMarkdown, one under the other, and Owner the provider that made them.
	Contents []langfeatures.Markup
	Owner    string
	// Loading says that the part is the message "Loading..." of an answer that is late.
	Loading bool
	// Range is what the part is about.
	Range buffer.Range
	// Actions are the links below the part. The diagnostics have "View Problem" and, when a provider has quick fixes for them,
	// "Quick Fix..." (step 12.8).
	Actions []HoverAction
	// Status is the text after the links of a diagnostic: "Checking for quick fixes..." while the providers are asked, and "No
	// quick fixes available" when they have none.
	Status string
	// order sorts the parts in the box.
	order [2]int
}

// Hover is what the hover shows: the parts, and the range of the text they are about (the hover is placed over it).
type Hover struct {
	Range buffer.Range
	Parts []HoverPart
	// Pinned says that the hover does not go away when the editor scrolls: the hover of a command (F8, Show Hover) stays until
	// the cursor moves or the text changes.
	Pinned bool
	// Complete says that every participant has answered; before that the box may grow.
	Complete bool
	// Focused says that the keys scroll the box (Show Hover pressed again).
	Focused bool
}

// hoverAnchor is what a hover is about: the range of the text under the pointer or at the cursor, and the position the
// providers are asked at.
type hoverAnchor struct {
	rng buffer.Range
	pos buffer.Position
	// hint, when it is set, says that the hover is that of a part of an inlay hint (inlay_interact.go), not of the text.
	hint *hintAnchor
}

// hoverOpState is the state of the computation of a hover (VS Code's HoverOperationState).
type hoverOpState int

const (
	hoverIdle hoverOpState = iota
	hoverFirstWait
	hoverSecondWait
	hoverWaitingAsync
	hoverWaitingAsyncLoading
)

// hoverOp is the computation of the hover of one anchor.
//
// Ported from VS Code: src/vs/editor/contrib/hover/browser/hoverOperation.ts. With the delay of the setting: at half of it the
// providers are asked (their answers are kept, not shown), at the delay the participants that answer at once are asked and
// what there is shows, then each answer that comes is added, and at three times the delay a message says that an answer is
// late.
type hoverOp struct {
	anchor hoverAnchor
	state  hoverOpState
	gen    int
	pinned bool
	// results are the parts that came so far; asyncDone says that every provider has answered.
	results   []HoverPart
	asyncDone bool
	// selections are the selections the op was started with, version the version of the document.
	selections []buffer.Selection
	version    int
	// cancelAsyncTimer, cancelSyncTimer and cancelLoadingTimer cancel the waits; cancelRequests ends the requests.
	cancelAsyncTimer, cancelSyncTimer, cancelLoadingTimer func()
	cancelRequests                                        context.CancelFunc
}

// hoverModel is the state of the hover of a view model.
type hoverModel struct {
	shown *Hover
	// op is the computation in progress or done for the anchor the pointer rests on; gen numbers them.
	op  *hoverOp
	gen int
	// pointerIn says that the pointer is over the hover box, which keeps a sticky hover.
	pointerIn bool
	// focused says that Show Hover was pressed on a shown hover: the keys scroll it.
	focused bool
	// cancelHide cancels the wait to hide, cancelReact the wait of a sticky hover to react to the pointer that went elsewhere.
	cancelHide, cancelReact func()
	lastPointer             buffer.Position
	// shownHint is the inlay hint the shown hover is about, or nil.
	shownHint *hintAnchor
	// selections are the selections when the hover came, which a different selection hides; unsubDoc stops the follow of the
	// document; scrollHandle is the listener of the scroll.
	selections   []buffer.Selection
	unsubDoc     func()
	scrollHandle foundation.ListenerHandle
	scrollOn     bool
	changed      *foundation.ChangeNotifier
	// scroller scrolls the box (set by the widget).
	scroller func(HoverScroll)
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

// HoverScroll is a way to scroll the box of a focused hover.
type HoverScroll int

// The ways to scroll the hover.
const (
	HoverScrollUp HoverScroll = iota
	HoverScrollDown
	HoverScrollLeft
	HoverScrollRight
	HoverScrollPageUp
	HoverScrollPageDown
	HoverScrollTop
	HoverScrollBottom
)

// SetHoverScroller gives the hover the means to scroll its box (the widget sets it).
func (vm *ViewModel) SetHoverScroller(fn func(HoverScroll)) { vm.hover.scroller = fn }

// hoverFeatures are the providers the hover asks, or nil.
func (vm *ViewModel) hoverFeatures() *langfeatures.Registry {
	if h := vm.sg.host; h != nil {
		return h.Features
	}
	return nil
}

// hoverHasProviders tells whether a hover provider matches the document.
func (vm *ViewModel) hoverHasProviders() bool {
	f := vm.hoverFeatures()
	return f != nil && f.HasHover(vm.langDocument())
}

// diagnosticHover is the participant of the diagnostics: a part for each diagnostic under the position, the most serious
// first. It answers at once.
//
// Ported from VS Code: markerHoverParticipant.ts (computeSync)
func diagnosticHover(vm *ViewModel, p buffer.Position) []HoverPart {
	ds := vm.DiagnosticsAt(p)
	parts := make([]HoverPart, len(ds))
	for i, d := range ds {
		parts[i] = HoverPart{Kind: HoverPartDiagnostic, Diagnostic: d, Range: d.Range, order: [2]int{hoverOrdinalDiagnostic, i}}
		if vm.viewProblem != nil {
			start := d.Range.Start
			parts[i].Actions = []HoverAction{{Label: "View Problem", Run: func() { vm.viewProblem(start) }}}
		}
	}
	return parts
}

// SetViewProblem gives the hover the function that "View Problem" runs, with the start of the diagnostic: the window shows the
// problem in the editor (what the marker navigation does for the marker at the position).
func (vm *ViewModel) SetViewProblem(fn func(p buffer.Position)) { vm.viewProblem = fn }

// hoverEnabled tells whether the pointer shows the hover.
func (vm *ViewModel) hoverEnabled() bool { return vm.opts.Hover.Enabled != "off" }

// hoverAnchorAt is what the hover is about at a position: the range of the diagnostic there, else the word, else the
// character, and with the keyboard the cursor itself. The pointer is over the character at the position, so the end of a word
// is not in it; the cursor may be at the end.
func (vm *ViewModel) hoverAnchorAt(p buffer.Position, keyboard bool) (hoverAnchor, bool) {
	if ds := vm.DiagnosticsAt(p); len(ds) > 0 {
		return hoverAnchor{rng: ds[0].Range, pos: p}, true
	}
	line, _ := vm.doc.Snapshot().LineContent(p.Line)
	col := min(p.Col, len(line))
	if s, e, ok := vm.wordFunc()(line, col); ok && s <= col && (col < e || keyboard && col == e) {
		return hoverAnchor{rng: buffer.Range{Start: buffer.Position{Line: p.Line, Col: s}, End: buffer.Position{Line: p.Line, Col: e}}, pos: p}, true
	}
	if col < len(line) {
		r, n := utf8.DecodeRuneInString(line[col:])
		if unicode.IsSpace(r) {
			return hoverAnchor{}, false
		}
		return hoverAnchor{rng: buffer.Range{Start: buffer.Position{Line: p.Line, Col: col}, End: buffer.Position{Line: p.Line, Col: col + n}}, pos: p}, true
	}
	if keyboard {
		return hoverAnchor{rng: buffer.Range{Start: p, End: p}, pos: p}, true
	}
	return hoverAnchor{}, false
}

// HoverPointerMoved tells the hover that the pointer rests on the text at a position. A hover that shows for the place keeps
// showing. Otherwise, with a sticky hover on screen the pointer is given editor.hover.hidingDelay to get into the box before
// the hover reacts (VS Code's grace period); then the hover of the place before starts to go (it hides) and the one of the
// place starts to be computed, to show after editor.hover.delay.
func (vm *ViewModel) HoverPointerMoved(p buffer.Position) {
	h := &vm.hover
	if !vm.hoverEnabled() || vm.hoverMouseBlocked() || h.focused {
		return
	}
	h.lastPointer = p
	if h.shown == nil && h.op != nil && !h.op.pinned && h.op.anchor.hint == nil && rangeHolds(h.op.anchor.rng, p) {
		vm.cancelHoverHide()
		return // the pointer moves inside what it rested on: the wait goes on, or the answer is known
	}
	if h.shown != nil && h.shownHint == nil && rangeHolds(h.shown.Range, p) {
		vm.cancelHoverHide()
		vm.cancelHoverReact()
		return
	}
	if h.shown != nil && vm.opts.Hover.Sticky && vm.opts.Hover.HidingDelay > 0 && vm.afterTimer != nil {
		if h.cancelReact == nil {
			h.cancelReact = vm.afterTimer(time.Duration(vm.opts.Hover.HidingDelay)*time.Millisecond, func() {
				h.cancelReact = nil
				if !h.pointerIn && !h.focused && vm.hoverEnabled() && !vm.hoverMouseBlocked() {
					vm.hoverReact(h.lastPointer)
				}
			})
		}
		return
	}
	vm.hoverReact(p)
}

// hoverReact starts the hover of the place the pointer is at now.
func (vm *ViewModel) hoverReact(p buffer.Position) {
	h := &vm.hover
	anchor, ok := vm.hoverAnchorAt(p, false)
	if !ok {
		vm.HideHover()
		return
	}
	vm.cancelHoverHide()
	if h.shown == nil && h.op != nil && !h.op.pinned && h.op.anchor.hint == nil && rangeHolds(h.op.anchor.rng, p) {
		return // the pointer moves inside what it rested on: the wait goes on, or the answer is known
	}
	vm.hoverHideShown()
	vm.hoverStart(anchor, false, false)
}

// hoverMouseBlocked tells whether the pointer cannot show the hover now: a pinned hover (the user asked for it) stays until
// something else removes it.
func (vm *ViewModel) hoverMouseBlocked() bool {
	return vm.hover.shown != nil && vm.hover.shown.Pinned || vm.hover.op != nil && vm.hover.op.pinned
}

// HoverPointerLeft tells the hover that the pointer left the text: a hover that shows hides after editor.hover.hidingDelay,
// unless the pointer went into it, and one that was still to come does not come.
func (vm *ViewModel) HoverPointerLeft() {
	vm.cancelHoverReact()
	if vm.hoverMouseBlocked() || vm.hover.focused {
		return
	}
	if vm.hover.shown == nil {
		vm.HideHover()
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
		vm.cancelHoverReact()
		return
	}
	if h.shown != nil && !h.shown.Pinned && !h.focused {
		vm.scheduleHoverHide()
	}
}

// ShowHoverAt shows the hover at a position as the command Show Hover and the marker navigation do: at once, and until the
// cursor moves or the text changes. It says whether there is something that can show: a diagnostic at the position, or a hover
// provider to ask (its answer comes later).
func (vm *ViewModel) ShowHoverAt(p buffer.Position) bool {
	vm.cancelHoverHide()
	vm.cancelHoverReact()
	anchor, ok := vm.hoverAnchorAt(p, true)
	if !ok || len(diagnosticHover(vm, p)) == 0 && !vm.hoverHasProviders() {
		vm.HideHover()
		return false
	}
	vm.hoverHideShown()
	vm.hoverStart(anchor, true, true)
	return true
}

// FocusHover gives the keys to the hover box (the command Show Hover pressed while the hover shows); it says whether there was
// a hover to focus.
func (vm *ViewModel) FocusHover() bool {
	h := &vm.hover
	if h.shown == nil {
		return false
	}
	h.focused = true
	h.shown.Focused = true
	vm.cancelHoverHide()
	vm.cancelHoverReact()
	h.changed.NotifyListeners()
	return true
}

// HoverFocused tells whether the keys scroll the hover (the key editorHoverFocused).
func (vm *ViewModel) HoverFocused() bool { return vm.hover.focused && vm.hover.shown != nil }

// HoverVisible tells whether a hover shows (the key editorHoverVisible).
func (vm *ViewModel) HoverVisible() bool { return vm.hover.shown != nil }

// ScrollHover scrolls the box of a focused hover.
func (vm *ViewModel) ScrollHover(how HoverScroll) {
	if s := vm.hover.scroller; s != nil && vm.HoverFocused() {
		s(how)
	}
}

// hoverStart starts the computation of the hover of an anchor: delayed, as the pointer's, or at once.
//
// Ported from VS Code: hoverOperation.ts (start)
func (vm *ViewModel) hoverStart(a hoverAnchor, immediate, pinned bool) {
	h := &vm.hover
	vm.hoverCancelOp()
	h.gen++
	op := &hoverOp{anchor: a, gen: h.gen, pinned: pinned, selections: vm.cursors.Selections(), version: vm.doc.Version()}
	h.op = op
	vm.hoverListen()
	delay := vm.opts.Hover.Delay
	if immediate || delay <= 0 || vm.afterTimer == nil {
		vm.hoverTriggerAsync(op)
		if op.cancelSyncTimer != nil {
			op.cancelSyncTimer()
			op.cancelSyncTimer = nil
		}
		vm.hoverTriggerSync(op)
		return
	}
	op.state = hoverFirstWait
	half := time.Duration(delay/2) * time.Millisecond
	op.cancelAsyncTimer = vm.afterTimer(half, func() { op.cancelAsyncTimer = nil; vm.hoverTriggerAsync(op) })
	op.cancelLoadingTimer = vm.afterTimer(time.Duration(3*delay)*time.Millisecond, func() { op.cancelLoadingTimer = nil; vm.hoverTriggerLoading(op) })
}

// hoverTriggerAsync asks the providers (the first half of the wait is over).
func (vm *ViewModel) hoverTriggerAsync(op *hoverOp) {
	if vm.hover.op != op {
		return
	}
	op.state = hoverSecondWait
	if delay := vm.opts.Hover.Delay; delay > 0 && vm.afterTimer != nil {
		op.cancelSyncTimer = vm.afterTimer(time.Duration(delay-delay/2)*time.Millisecond, func() { op.cancelSyncTimer = nil; vm.hoverTriggerSync(op) })
	}
	if op.anchor.hint != nil {
		vm.hoverHintAsync(op)
		return
	}
	f := vm.hoverFeatures()
	doc := vm.langDocument()
	if f == nil || !f.HasHover(doc) {
		op.asyncDone = true
		return
	}
	ctx := context.Background()
	if h := vm.sg.host; h != nil && h.Ctx != nil {
		ctx = h.Ctx
	}
	ctx, op.cancelRequests = context.WithCancel(ctx)
	pos, version, gen, anchor := op.anchor.pos, doc.Version, op.gen, op.anchor.rng
	go func() {
		errs := f.HoverEach(ctx, doc, pos, func(r langfeatures.HoverResult) {
			vm.post(func() { vm.hoverAnswer(op, gen, version, anchor, r) })
		})
		vm.post(func() { vm.hoverAnswered(op, gen, errs) })
	}()
}

// hoverTriggerSync asks the participants that answer at once (the whole delay is over) and shows what there is.
func (vm *ViewModel) hoverTriggerSync(op *hoverOp) {
	if vm.hover.op != op {
		return
	}
	if op.anchor.hint != nil {
		op.results = append(op.results, hintHoverParts(op.anchor)...)
		op.state = hoverWaitingAsync
		if op.asyncDone {
			op.state = hoverIdle
		}
		vm.hoverFire(op)
		return
	}
	diags := diagnosticHover(vm, op.anchor.pos)
	op.results = append(op.results, diags...)
	op.state = hoverWaitingAsync
	if op.asyncDone {
		op.state = hoverIdle
	}
	vm.hoverFire(op)
	vm.hoverQuickFixes(op, diags)
}

// hoverTriggerLoading adds the message "Loading..." when an answer is still to come after three times the delay.
func (vm *ViewModel) hoverTriggerLoading(op *hoverOp) {
	if vm.hover.op == op && op.state == hoverWaitingAsync {
		op.state = hoverWaitingAsyncLoading
		vm.hoverFire(op)
	}
}

// hoverAnswer takes the answer of a provider. An answer for a document that changed, or of an op that was replaced or
// cancelled, is dropped.
func (vm *ViewModel) hoverAnswer(op *hoverOp, gen, version int, anchor buffer.Range, r langfeatures.HoverResult) {
	if vm.hover.op != op || op.gen != gen || vm.doc.Version() != version {
		return
	}
	rng := anchor
	if r.Hover.Range != nil {
		rng = vm.doc.Snapshot().ValidateRange(*r.Hover.Range)
	}
	op.results = append(op.results, HoverPart{
		Kind: HoverPartMarkdown, Contents: r.Hover.Contents, Owner: r.Hover.Owner, Range: rng, order: [2]int{hoverOrdinalMarkdown, r.Ordinal},
	})
	vm.hoverFire(op)
}

// hoverAnswered is told that every provider has answered.
func (vm *ViewModel) hoverAnswered(op *hoverOp, gen int, errs []langfeatures.ProviderError) {
	if vm.hover.op != op || op.gen != gen {
		return
	}
	for _, e := range errs {
		if h := vm.sg.host; h != nil && h.Log != nil && !errors.Is(e.Err, context.Canceled) {
			h.Log("hover provider "+e.Owner+" failed", e.Err)
		}
	}
	op.asyncDone = true
	if op.state == hoverWaitingAsync || op.state == hoverWaitingAsyncLoading {
		op.state = hoverIdle
		vm.hoverFire(op)
	}
}

// hoverFire gives what the op has to the box, once the delay is over.
func (vm *ViewModel) hoverFire(op *hoverOp) {
	if op.state == hoverFirstWait || op.state == hoverSecondWait {
		return // no results before the hover time
	}
	complete := op.state == hoverIdle
	parts := slices.Clone(op.results)
	if op.state == hoverWaitingAsyncLoading {
		parts = append(parts, HoverPart{
			Kind: HoverPartMarkdown, Contents: []langfeatures.Markup{{Value: "Loading..."}}, Loading: true, Range: op.anchor.rng,
			order: [2]int{hoverOrdinalMarkdown, hoverOrdinalLoading},
		})
	}
	h := &vm.hover
	if complete && len(parts) == 0 && op.pinned {
		vm.HideHover() // the hover of a command has nothing to show: the pointer may have its own
		return
	}
	// a complete hover that shows is not replaced by a partial one: the box waits for the complete answer
	if h.shown != nil && h.shown.Complete && !complete {
		return
	}
	vm.hoverSet(op, parts, complete)
}

// hoverSet puts the parts in the box, or removes the box when there are none.
func (vm *ViewModel) hoverSet(op *hoverOp, parts []HoverPart, complete bool) {
	h := &vm.hover
	if len(parts) == 0 {
		vm.hoverHideShown()
		return
	}
	slices.SortStableFunc(parts, func(a, b HoverPart) int {
		if a.order[0] != b.order[0] {
			return a.order[0] - b.order[0]
		}
		return a.order[1] - b.order[1]
	})
	rng := parts[0].Range
	for _, p := range parts[1:] {
		if p.Range.Start.IsBefore(rng.Start) {
			rng.Start = p.Range.Start
		}
		if rng.End.IsBefore(p.Range.End) {
			rng.End = p.Range.End
		}
	}
	h.shown = &Hover{Range: rng, Parts: parts, Pinned: op.pinned, Complete: complete, Focused: h.focused}
	h.shownHint = op.anchor.hint
	h.selections = op.selections
	vm.hoverListen()
	h.changed.NotifyListeners()
}

// hoverListen follows the document and the scroll while a hover is computed or shows.
func (vm *ViewModel) hoverListen() {
	h := &vm.hover
	if h.unsubDoc == nil {
		h.unsubDoc = vm.doc.Subscribe(func(document.ChangeEvent) { vm.HideHover() })
	}
	if !h.scrollOn {
		h.scrollHandle, h.scrollOn = vm.AddScrollListener(vm.hoverScrolled), true
	}
}

// hoverScrolled hides a hover of the pointer when the editor scrolls.
func (vm *ViewModel) hoverScrolled() {
	h := &vm.hover
	if h.shown != nil && !h.shown.Pinned || h.shown == nil && h.op != nil && !h.op.pinned {
		vm.HideHover()
	}
}

// hoverCancelOp stops the computation: the waits and the requests.
func (vm *ViewModel) hoverCancelOp() {
	h := &vm.hover
	op := h.op
	if op == nil {
		return
	}
	h.op = nil
	for _, stop := range []func(){op.cancelAsyncTimer, op.cancelSyncTimer, op.cancelLoadingTimer} {
		if stop != nil {
			stop()
		}
	}
	if op.cancelRequests != nil {
		op.cancelRequests()
	}
}

// hoverHideShown removes the box and leaves the computation to go on.
func (vm *ViewModel) hoverHideShown() {
	h := &vm.hover
	h.pointerIn, h.focused = false, false
	if h.shown == nil {
		return
	}
	h.shown, h.shownHint = nil, nil
	h.changed.NotifyListeners()
}

// HideHover hides the hover, stops its computation and cancels the waits.
func (vm *ViewModel) HideHover() {
	h := &vm.hover
	vm.cancelHoverReact()
	vm.cancelHoverHide()
	vm.hoverCancelOp()
	if h.unsubDoc != nil {
		h.unsubDoc()
		h.unsubDoc = nil
	}
	if h.scrollOn {
		vm.RemoveScrollListener(h.scrollHandle)
		h.scrollOn = false
	}
	vm.hoverHideShown()
}

// hoverSelectionsChanged hides the hover when the selections are not those it came with.
func (vm *ViewModel) hoverSelectionsChanged() {
	h := &vm.hover
	switch {
	case h.shown != nil && !buffer.SelectionsEqual(h.selections, vm.cursors.Selections()):
		vm.HideHover()
	case h.shown == nil && h.op != nil && h.op.pinned && !buffer.SelectionsEqual(h.op.selections, vm.cursors.Selections()):
		vm.HideHover()
	}
}

func (vm *ViewModel) cancelHoverReact() {
	h := &vm.hover
	if h.cancelReact != nil {
		h.cancelReact()
		h.cancelReact = nil
	}
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
	if h.shown == nil || h.cancelHide != nil || h.focused || h.pointerIn && vm.opts.Hover.Sticky {
		return
	}
	delay := vm.opts.Hover.HidingDelay
	if !vm.opts.Hover.Sticky || delay <= 0 || vm.afterTimer == nil {
		vm.HideHover()
		return
	}
	h.cancelHide = vm.afterTimer(time.Duration(delay)*time.Millisecond, func() {
		h.cancelHide = nil
		if !h.pointerIn && !h.focused {
			vm.HideHover()
		}
	})
}

// markersNavigationVisible tells whether the hover that a command opened (the marker navigation) is open: Escape closes it.
func (vm *ViewModel) markersNavigationVisible() bool {
	h := vm.hover.shown
	return h != nil && h.Pinned
}

// hoverActions are the commands of the hover.
//
// Ported from VS Code: src/vs/editor/contrib/hover/browser/hoverActions.ts
func hoverActions(a map[string]action) {
	// editor.action.showHover shows the hover at the cursor, or gives the keys to the box when it already shows.
	a["editor.action.showHover"] = func(vm *ViewModel, _ any) error {
		vm.keepSessions, vm.skipReveal = true, true
		if vm.Hover() != nil {
			vm.FocusHover()
			return nil
		}
		vm.ShowHoverAt(vm.cursors.Primary().Position())
		return nil
	}
	a["editor.action.hideHover"] = func(vm *ViewModel, _ any) error {
		vm.keepSessions, vm.skipReveal = true, true
		vm.HideHover()
		return nil
	}
	scroll := func(how HoverScroll) action {
		return func(vm *ViewModel, _ any) error {
			vm.keepSessions, vm.skipReveal = true, true
			vm.ScrollHover(how)
			return nil
		}
	}
	a["editor.action.scrollUpHover"] = scroll(HoverScrollUp)
	a["editor.action.scrollDownHover"] = scroll(HoverScrollDown)
	a["editor.action.scrollLeftHover"] = scroll(HoverScrollLeft)
	a["editor.action.scrollRightHover"] = scroll(HoverScrollRight)
	a["editor.action.pageUpHover"] = scroll(HoverScrollPageUp)
	a["editor.action.pageDownHover"] = scroll(HoverScrollPageDown)
	a["editor.action.goToTopHover"] = scroll(HoverScrollTop)
	a["editor.action.goToBottomHover"] = scroll(HoverScrollBottom)
}

// hoverQuickFixes asks the code action providers for the quick fixes of each diagnostic of the hover and puts "Quick Fix..." in
// the links of the ones that have some, "No quick fixes available" in the others. "Checking for quick fixes..." shows when the
// answer is later than 200 ms.
//
// Ported from VS Code: markerHoverParticipant.ts (renderMarkerHover, getCodeActions)
func (vm *ViewModel) hoverQuickFixes(op *hoverOp, diags []HoverPart) {
	_, reg, ok := vm.codeActionsHost()
	if !ok || len(diags) == 0 {
		return
	}
	all := vm.allDiagnostics()
	doc := vm.langDocument()
	ctx, cancel := context.WithCancel(vm.hostCtx())
	prev := op.cancelRequests
	op.cancelRequests = func() {
		cancel()
		if prev != nil {
			prev()
		}
	}
	gen := op.gen
	// set puts the status and the link in the part of the diagnostic, wherever it is in the results now
	set := func(d Diagnostic, update func(*HoverPart)) {
		if vm.hover.op != op || op.gen != gen || vm.doc.Version() != doc.Version {
			return
		}
		for i := range op.results {
			p := &op.results[i]
			if p.Kind == HoverPartDiagnostic && p.Diagnostic.Range == d.Range && p.Diagnostic.Message == d.Message {
				update(p)
				vm.hoverFire(op)
				return
			}
		}
	}
	for _, part := range diags {
		d := part.Diagnostic
		if vm.afterTimer != nil {
			vm.afterTimer(200*time.Millisecond, func() {
				set(d, func(p *HoverPart) {
					if p.Status == "" && len(p.Actions) < 2 {
						p.Status = "Checking for quick fixes..."
					}
				})
			})
		}
		trig := langfeatures.CodeActionTrigger{
			Type: langfeatures.TriggerInvokedAction, Source: langfeatures.SourceQuickFixHover,
			Filter: langfeatures.CodeActionFilter{Include: langfeatures.Kind(langfeatures.KindQuickFix)},
		}
		req := langfeatures.CodeActionRequest{Range: d.Range, Diagnostics: diagnosticsTouching(all, d.Range)}
		go func() {
			set0, errs := reg.GetCodeActions(ctx, doc, req, trig)
			vm.post(func() {
				vm.logProviderErrors("code action provider", errs)
				set(d, func(p *HoverPart) {
					if len(set0.Valid) == 0 {
						p.Status = "No quick fixes available"
						return
					}
					p.Status = ""
					at := d.Range.Start
					p.Actions = append(p.Actions, HoverAction{Label: "Quick Fix...", Run: func() { vm.showCodeActionList(set0, at, false, false) }})
				})
			})
		}()
	}
}
