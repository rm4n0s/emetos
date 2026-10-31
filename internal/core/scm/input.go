package scm

import (
	"slices"
	"sync"
)

// Input is the commit message box of a source control (VS Code's ISCMInput). The view shows it and sets the text with the user's
// typing; the provider reads and sets it, validates it and asks for the focus.
type Input struct {
	src *Source

	// guarded by the lock of the model
	value       string
	history     *History
	reason      InputChangeReason
	placeholder string
	enabled     bool
	visible     bool
	validate    func(value string) *Validation
	validateGen uint64
	focusAsks   uint64

	lmu       sync.Mutex
	listeners []*func(string)
}

// Source is the source control the input belongs to.
func (in *Input) Source() *Source { return in.src }

// Value is the text.
func (in *Input) Value() string {
	in.src.m.mu.Lock()
	defer in.src.m.mu.Unlock()
	return in.value
}

// InputChangeReason says why the text of an input changed last (VS Code's SCMInputChangeReason, which only tells the history
// moves): the view puts the caret at the start of a message that Up brought back and at the end of any other.
type InputChangeReason int

// The reasons a text changed.
const (
	// ChangedByValue is any change that is not a move through the history: the provider set the text, or the user typed.
	ChangedByValue InputChangeReason = iota
	// ChangedByHistoryPrevious is a move to the previous message of the history.
	ChangedByHistoryPrevious
	// ChangedByHistoryNext is a move to the next message.
	ChangedByHistoryNext
)

// SetValue replaces the text as the provider does, and calls the listeners when it changed. The message that was there goes into
// the history of the repository, and the new one is added to it (VS Code's `setValue(value, false)`): after a commit the provider
// empties the box and Up brings the message back.
func (in *Input) SetValue(value string) { in.set(value, false, ChangedByValue) }

// SetTransientValue replaces the text as the view does when the user types or the box is cleared: the history is not touched
// (VS Code's `setValue(value, true)`).
func (in *Input) SetTransientValue(value string) { in.set(value, true, ChangedByValue) }

func (in *Input) set(value string, transient bool, reason InputChangeReason) {
	m := in.src.m
	m.mu.Lock()
	if in.value == value || in.src.removed {
		m.mu.Unlock()
		return
	}
	if !transient {
		in.history.ReplaceLast(in.value)
		in.history.Add(value)
	}
	in.value, in.reason = value, reason
	m.mu.Unlock()
	in.lmu.Lock()
	ls := slices.Clone(in.listeners)
	in.lmu.Unlock()
	for _, f := range ls {
		(*f)(value)
	}
	m.notify(Event{Kind: InputChanged, Source: in.src})
}

// ChangeReason is why the text changed last.
func (in *Input) ChangeReason() InputChangeReason {
	in.src.m.mu.Lock()
	defer in.src.m.mu.Unlock()
	return in.reason
}

// ShowPreviousHistoryValue replaces the text by the message before it in the history of the repository (VS Code's
// showPreviousHistoryValue): the text being written is kept as the last message, so that Down brings it back.
func (in *Input) ShowPreviousHistoryValue() {
	in.src.m.mu.Lock()
	h := in.history
	switch {
	case h.IsAtEnd():
		h.ReplaceLast(in.value)
	case !h.Has(in.value):
		h.ReplaceLast(in.value)
		h.ResetCursor()
	}
	value := h.Previous()
	in.src.m.mu.Unlock()
	in.set(value, true, ChangedByHistoryPrevious)
}

// ShowNextHistoryValue replaces the text by the message after it in the history; at the end of the history it does nothing.
func (in *Input) ShowNextHistoryValue() {
	in.src.m.mu.Lock()
	h := in.history
	if h.IsAtEnd() {
		in.src.m.mu.Unlock()
		return
	}
	if !h.Has(in.value) {
		h.ReplaceLast(in.value)
		h.ResetCursor()
	}
	value := h.Next()
	in.src.m.mu.Unlock()
	in.set(value, true, ChangedByHistoryNext)
}

// History is the messages of the repository in this session, the oldest first; the last one is the text being written when the
// cursor is at the end.
func (in *Input) History() []string {
	in.src.m.mu.Lock()
	defer in.src.m.mu.Unlock()
	return in.history.Values()
}

// OnChange calls fn with the new text each time it changes, and returns the function that stops the calls.
func (in *Input) OnChange(fn func(value string)) (cancel func()) {
	f := &fn
	in.lmu.Lock()
	in.listeners = append(in.listeners, f)
	in.lmu.Unlock()
	return func() {
		in.lmu.Lock()
		defer in.lmu.Unlock()
		in.listeners = slices.DeleteFunc(in.listeners, func(o *func(string)) bool { return o == f })
	}
}

// Placeholder is the text shown while the box is empty.
func (in *Input) Placeholder() string {
	in.src.m.mu.Lock()
	defer in.src.m.mu.Unlock()
	return in.placeholder
}

// SetPlaceholder sets the text shown while the box is empty.
func (in *Input) SetPlaceholder(p string) {
	in.change(func() bool { c := in.placeholder != p; in.placeholder = p; return c })
}

// Enabled tells whether the box accepts text (it does by default).
func (in *Input) Enabled() bool {
	in.src.m.mu.Lock()
	defer in.src.m.mu.Unlock()
	return in.enabled
}

// SetEnabled enables or disables the box.
func (in *Input) SetEnabled(e bool) {
	in.change(func() bool { c := in.enabled != e; in.enabled = e; return c })
}

// Visible tells whether the box is shown (it is by default).
func (in *Input) Visible() bool {
	in.src.m.mu.Lock()
	defer in.src.m.mu.Unlock()
	return in.visible
}

// SetVisible shows or hides the box.
func (in *Input) SetVisible(v bool) {
	in.change(func() bool { c := in.visible != v; in.visible = v; return c })
}

// SetValidation sets the function that checks the text; nil removes it.
func (in *Input) SetValidation(fn func(value string) *Validation) {
	in.change(func() bool { in.validate = fn; in.validateGen++; return true })
}

// ValidationGeneration counts the calls of SetValidation; the view validates the text again when the number it saw changed.
func (in *Input) ValidationGeneration() uint64 {
	in.src.m.mu.Lock()
	defer in.src.m.mu.Unlock()
	return in.validateGen
}

// HasValidation tells whether a check is set.
func (in *Input) HasValidation() bool {
	in.src.m.mu.Lock()
	defer in.src.m.mu.Unlock()
	return in.validate != nil
}

// Validate runs the check on a text: nil when there is none or it has no message. It is called on the UI thread by the view; the
// check is the provider's, and the host contains a panic in it.
func (in *Input) Validate(value string) *Validation {
	in.src.m.mu.Lock()
	fn := in.validate
	in.src.m.mu.Unlock()
	if fn == nil {
		return nil
	}
	return fn(value)
}

// Focus asks the view to give the box the keyboard focus.
func (in *Input) Focus() {
	in.change(func() bool { in.focusAsks++; return true })
}

// FocusRequests counts the calls of Focus; the view focuses the box when the number it saw changes.
func (in *Input) FocusRequests() uint64 {
	in.src.m.mu.Lock()
	defer in.src.m.mu.Unlock()
	return in.focusAsks
}

func (in *Input) change(fn func() bool) {
	m := in.src.m
	m.mu.Lock()
	if in.src.removed || !fn() {
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	m.notify(Event{Kind: InputChanged, Source: in.src})
}
