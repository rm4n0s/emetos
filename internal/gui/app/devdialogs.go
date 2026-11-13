package app

import (
	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/command"
)

// parseAnswerDialog reads the arguments of dev.dialog.answer: {"button": "<label>"}, or just the label. The tag is
// DevAnswerDialogInvalid.
func parseAnswerDialog(args any) (string, error) {
	raw := args
	if v, ok := args.(map[string]any); ok {
		raw = v["button"]
	}
	label, ok := raw.(string)
	if !ok || label == "" {
		return "", errors.New("DevAnswerDialogInvalid", `dev.dialog.answer needs the label of a button, or {"button": "<label>"}`, "arguments", args)
	}
	return label, nil
}

// devAnswerDialog runs dev.dialog.answer: it presses a button of the dialog that shows in the workspace of the active tab, as a click
// on it would (a proof answers a question with it: Git's trust question, SPEC §10.30, step 35.1).
func devAnswerDialog(p *Process, args any) error {
	label, err := parseAnswerDialog(args)
	if err != nil {
		return err
	}
	w := p.Workspaces.Active()
	if w == nil || w.Dialogs == nil {
		return errors.New("DevAnswerDialogNoWorkspace", "no workspace tab is active")
	}
	if !w.Dialogs.IsShowing() {
		return errors.New("DevAnswerDialogNone", "no dialog shows", "button", label)
	}
	if !w.Dialogs.Answer(label) {
		return errors.New("DevAnswerDialogNoButton", "the dialog has no button with this label", "button", label)
	}
	return nil
}

// registerDevDialogs registers dev.dialog.answer (SPEC §15.2); it exists in builds that run dev commands only.
func registerDevDialogs(p *Process) {
	meta := command.Meta{ID: "dev.dialog.answer", Description: `Press a button of the dialog that shows, by its label. Arguments: the label, or {"button": "<label>"}.`}
	if err := RegisterDev(p.Commands, meta, func(_ command.Context, args any) error { return devAnswerDialog(p, args) }); err != nil {
		// the id is fixed, new and starts with dev., so a failure is a mistake here
		panic(err)
	}
}
