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
	// Password shows a dot for each character typed.
	Password bool
	Validate func(value string) string
	Accepted func(value string)
	Hidden   func()
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
	var once sync.Once
	finish := func(value string, ok bool) {
		once.Do(func() {
			if done != nil {
				in.scope.runInputBoxDone(in, done, value, ok)
			}
		})
	}
	req := InputBoxRequest{
		Prompt: opts.Prompt, Value: opts.Value, Password: opts.Password,
		Accepted: func(value string) { finish(value, true) },
		Hidden:   func() { finish("", false) },
	}
	if opts.Validate != nil {
		req.Validate = func(value string) string { return in.scope.runInputBoxValidate(in, opts.Validate, value) }
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
