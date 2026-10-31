package app

import (
	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/command"
)

// registerDevQuickInput registers dev.quickInput.type (SPEC §15.2, step 24.9): it types a text into the quick input that is shown, a
// password box included, as the user does, so that a proof image shows the dots of a password that was typed.
func registerDevQuickInput(p *Process) {
	meta := command.Meta{
		ID:          "dev.quickInput.type",
		Description: `Type a text into the quick input that is shown, as the user does. Arguments: "text" or {"text": "..."}. Fails when no quick input is shown.`,
	}
	err := RegisterDev(p.Commands, meta, func(_ command.Context, args any) error { return devQuickInputType(p.Workspaces.Active(), args) })
	if err != nil {
		// the id is fixed, new and starts with dev., so a failure is a mistake here
		panic(err)
	}
}

// devQuickInputType types the text of the arguments into the quick input of a workspace. The tags are DevQuickInputInvalid and
// DevQuickInputNotShown.
func devQuickInputType(w *Workspace, args any) error {
	text, err := devQuickInputText(args)
	if err != nil {
		return err
	}
	if w == nil || w.QuickInput == nil || !w.QuickInput.Visible() {
		return errors.New("DevQuickInputNotShown", "no quick input is shown")
	}
	w.QuickInput.SetValue(text)
	return nil
}

// devQuickInputText reads the arguments of dev.quickInput.type. The tag is DevQuickInputInvalid.
func devQuickInputText(args any) (string, error) {
	switch v := args.(type) {
	case string:
		return v, nil
	case map[string]any:
		if text, ok := v["text"].(string); ok {
			return text, nil
		}
	}
	return "", errors.New("DevQuickInputInvalid", `dev.quickInput.type needs "text" or {"text": "..."}`, "arguments", args)
}
