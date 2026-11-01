package exthost

import (
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// InputBoxRequest is what a window shows for InputBox().Show: the input, and what it calls when it ends. Validate checks the text
// as the user types (a message that is not empty keeps the box open); Accepted is called with a valid text after the box is
// hidden; Hidden is called once when the box ends without one (closed, or replaced by another quick input).
type InputBoxRequest struct {
	Prompt, Value string
	// Message is the line under the input while the text is valid; IgnoreFocusOut keeps the box open when the focus leaves it.
	Message        string
	IgnoreFocusOut bool
	// Password shows a dot for each character typed.
	Password bool
	Validate func(value string) string
	// Buttons are the buttons inside the box; ButtonPressed is called with the index of the one pressed, the text of the box and the
	// function that replaces the text.
	Buttons       []extension.InputBoxButton
	ButtonPressed func(index int, value string, setValue func(string))
	Accepted      func(value string)
	Hidden        func()
	// Origin is the screen that shows the box (SPEC §10.18 "As built (step 24.25)"): the window of the host or the view of a client.
	Origin extension.Origin
}

// inputBoxAPI is the extension.InputBox of one activation.
type inputBoxAPI struct{ inst *instance }

var _ extension.InputBox = inputBoxAPI{}

// InputBox implements extension.API.
func (a *api) InputBox() extension.InputBox { return inputBoxAPI{a.inst} }

// Show implements extension.InputBox.
func (b inputBoxAPI) Show(opts extension.InputBoxOptions, done func(value string, ok bool)) error {
	in := b.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	show := in.scope.opts.InputBox
	if show == nil {
		return errors.New("ExtensionInputBoxUnavailable", "the scope cannot ask for a line of text", "extension", in.id)
	}
	origin := in.scope.resolveOrigin(opts.Origin)
	var once sync.Once
	finish := func(value string, ok bool) {
		once.Do(func() {
			if done != nil {
				in.scope.inOrigin(origin, func() { in.scope.runInputBoxDone(in, done, value, ok) })()
			}
		})
	}
	req := InputBoxRequest{
		Prompt: opts.Prompt, Value: opts.Value, Password: opts.Password, Message: opts.Message, IgnoreFocusOut: opts.IgnoreFocusOut, Origin: origin,
		Accepted: func(value string) { finish(value, true) },
		Hidden:   func() { finish("", false) },
	}
	if opts.Validate != nil {
		req.Validate = func(value string) string { return in.scope.runInputBoxValidate(in, opts.Validate, value) }
	}
	if len(opts.Buttons) > 0 {
		req.Buttons = append([]extension.InputBoxButton(nil), opts.Buttons...)
		if opts.OnButton != nil {
			req.ButtonPressed = func(index int, value string, setValue func(string)) {
				in.scope.runInputBoxButton(in, opts.OnButton, index, value, setValue)
			}
		}
	}
	show(req)
	return nil
}

// runInputBoxDone calls the function an extension gave to hear the end of an input box. A panic fails the extension with
// ExtensionInputBoxCallbackPanicked.
func (s *Scope) runInputBoxDone(inst *instance, f func(string, bool), value string, ok bool) {
	if inst.isDead() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionInputBoxCallbackPanicked", "it panicked in the function that hears the end of an input box: %v", r).SetMetadata("extension", inst.id, "scope", s.opts.Info.Name)
			s.fail(inst.slot, failure, "input box")
		}
	}()
	f(value, ok)
}

// runInputBoxValidate calls the validation function of an input box. A panic fails the extension with
// ExtensionInputBoxCallbackPanicked and accepts the text.
func (s *Scope) runInputBoxValidate(inst *instance, f func(string) string, value string) (msg string) {
	if inst.isDead() {
		return ""
	}
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionInputBoxCallbackPanicked", "it panicked in the function that validates the text of an input box: %v", r).SetMetadata("extension", inst.id, "scope", s.opts.Info.Name)
			s.fail(inst.slot, failure, "input box")
			msg = ""
		}
	}()
	return f(value)
}

// runInputBoxButton calls the function an extension gave to hear the buttons of an input box. A panic fails the extension with
// ExtensionInputBoxCallbackPanicked.
func (s *Scope) runInputBoxButton(inst *instance, f func(int, string, func(string)), index int, value string, setValue func(string)) {
	if inst.isDead() {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			failure := errors.NewErrf("ExtensionInputBoxCallbackPanicked", "it panicked in the function that hears a button of an input box: %v", r).SetMetadata("extension", inst.id, "scope", s.opts.Info.Name)
			s.fail(inst.slot, failure, "input box")
		}
	}()
	f(index, value, setValue)
}
