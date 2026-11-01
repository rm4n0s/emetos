package exthost

import (
	"sync"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// DialogRequest is what a window shows for Dialogs().Ask: the dialog, and what it calls with the answer. Answered is called once,
// on the UI thread, when the dialog closes.
type DialogRequest struct {
	Spec     extension.DialogSpec
	Answered func(extension.DialogResult)
	// Origin is the screen that shows the dialog (SPEC §10.18 "As built (step 24.25)"): the window of the host or the view of a client.
	Origin extension.Origin
}

// dialogsAPI is the extension.Dialogs of one activation.
type dialogsAPI struct{ inst *instance }

var _ extension.Dialogs = dialogsAPI{}

// Dialogs implements extension.API.
func (a *api) Dialogs() extension.Dialogs { return dialogsAPI{a.inst} }

// Ask implements extension.Dialogs.
func (d dialogsAPI) Ask(spec extension.DialogSpec, done func(extension.DialogResult)) error {
	in := d.inst
	if in.isDead() {
		return errors.New("ExtensionDeactivated", "the extension was deactivated", "extension", in.id)
	}
	cancels := 0
	for _, b := range spec.Buttons {
		if b.Cancel {
			cancels++
		}
	}
	if len(spec.Buttons) == 0 || cancels > 1 {
		return errors.New("ExtensionDialogInvalid", "a dialog needs a button, and at most one of them is the cancel button", "extension", in.id, "buttons", len(spec.Buttons), "cancel", cancels)
	}
	show := in.scope.opts.Dialog
	if show == nil {
		return errors.New("ExtensionDialogsUnavailable", "the scope cannot show a dialog", "extension", in.id)
	}
	origin := in.scope.resolveOrigin(spec.Origin)
	var once sync.Once
	show(DialogRequest{Spec: spec, Origin: origin, Answered: func(r extension.DialogResult) {
		once.Do(func() {
			if done != nil {
				in.scope.inOrigin(origin, func() { in.scope.runDialogDone(in, done, r) })()
			}
		})
	}})
	return nil
}

// runDialogDone calls the function an extension gave to hear the answer of a dialog. A panic fails the extension with
// ExtensionDialogCallbackPanicked.
func (s *Scope) runDialogDone(inst *instance, f func(extension.DialogResult), r extension.DialogResult) {
	if inst.isDead() {
		return
	}
	defer func() {
		if p := recover(); p != nil {
			failure := errors.NewErrf("ExtensionDialogCallbackPanicked", "it panicked in the function that hears the answer of a dialog: %v", p).SetMetadata("extension", inst.id, "scope", s.opts.Info.Name)
			s.fail(inst.slot, failure, "dialog")
		}
	}()
	f(r)
}
