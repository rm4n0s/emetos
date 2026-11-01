package fakeapi

import (
	"slices"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// FakeDialog is a dialog the extension asked: it waits until the test answers it with Press or Cancel, in the order asked (the
// host shows one dialog at a time).
type FakeDialog struct {
	a        *API
	spec     extension.DialogSpec
	done     func(extension.DialogResult)
	answered bool
}

// Dialogs implements extension.API.
func (a *API) Dialogs() extension.Dialogs { return dialogs{a} }

type dialogs struct{ a *API }

// Ask implements extension.Dialogs with the errors of the host: ExtensionDeactivated and ExtensionDialogInvalid.
func (d dialogs) Ask(spec extension.DialogSpec, done func(extension.DialogResult)) error {
	a := d.a
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.scm.dead {
		return errors.New("ExtensionDeactivated", "the extension was deactivated")
	}
	cancels := 0
	for _, b := range spec.Buttons {
		if b.Cancel {
			cancels++
		}
	}
	if len(spec.Buttons) == 0 || cancels > 1 {
		return errors.New("ExtensionDialogInvalid", "a dialog needs a button, and at most one of them is the cancel button", "buttons", len(spec.Buttons), "cancel", cancels)
	}
	spec.Buttons = slices.Clone(spec.Buttons)
	if spec.Origin == extension.HostOrigin {
		spec.Origin = a.origins.cur
	}
	if done != nil {
		inner, origin := done, spec.Origin
		done = func(r extension.DialogResult) { a.RunAs(origin, func() { inner(r) }) }
	}
	a.dialogs = append(a.dialogs, &FakeDialog{a: a, spec: spec, done: done})
	return nil
}

// OpenDialogs are the dialogs that wait for an answer, in the order they were asked.
func (a *API) OpenDialogs() []*FakeDialog {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []*FakeDialog
	for _, d := range a.dialogs {
		if !d.answered {
			out = append(out, d)
		}
	}
	return out
}

// Spec is the dialog as the extension described it.
func (d *FakeDialog) Spec() extension.DialogSpec { return d.spec }

// Press presses the button with the label (the answer comes when the test pumps). It fails the test when there is none.
func (d *FakeDialog) Press(label string) {
	d.a.t.Helper()
	for i, b := range d.spec.Buttons {
		if b.Label == label {
			d.answer(extension.DialogResult{Button: i})
			return
		}
	}
	d.a.t.Fatalf("fakeapi: the dialog %q has no button %q", d.spec.Message, label)
}

// Cancel closes the dialog as Escape does: the answer is the cancel button, or -1.
func (d *FakeDialog) Cancel() {
	res := extension.DialogResult{Button: -1, Cancelled: true}
	for i, b := range d.spec.Buttons {
		if b.Cancel {
			res.Button = i
		}
	}
	d.answer(res)
}

func (d *FakeDialog) answer(r extension.DialogResult) {
	a := d.a
	a.mu.Lock()
	if d.answered {
		a.mu.Unlock()
		return
	}
	d.answered = true
	done := d.done
	a.mu.Unlock()
	if done != nil {
		a.Post(func() {
			a.mu.Lock()
			dead := a.scm.dead
			a.mu.Unlock()
			if !dead {
				done(r)
			}
		})
	}
}
