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
	placeholder string
	enabled     bool
	visible     bool
	validate    func(value string) *Validation
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

// SetValue replaces the text, whether the provider or the user does, and calls the listeners when it changed.
func (in *Input) SetValue(value string) {
	m := in.src.m
	m.mu.Lock()
	if in.value == value || in.src.removed {
		m.mu.Unlock()
		return
	}
	in.value = value
	m.mu.Unlock()
	in.lmu.Lock()
	ls := slices.Clone(in.listeners)
	in.lmu.Unlock()
	for _, f := range ls {
		(*f)(value)
	}
	m.notify(Event{Kind: InputChanged, Source: in.src})
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
	in.change(func() bool { in.validate = fn; return true })
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
