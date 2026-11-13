package app

import (
	"context"
	"testing"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

// dev.dialog.answer presses a button of the dialog that shows (step 35.1).
func TestDevAnswerDialogPressesTheButton(t *testing.T) {
	tests := []struct {
		name      string
		args      any
		button    int
		cancelled bool
	}{
		{"by its label", "Trust", 0, false},
		{"as an object", map[string]any{"button": "Don't Trust"}, 1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newNotifyApp(t)
			var got *kit.DialogResult
			a.win.Dialogs.Show(kit.DialogSpec{Message: "Trust it?", Buttons: []string{"Trust", "Don't Trust"}, CancelButton: foundation.Some(1),
				OnAnswer: func(r kit.DialogResult) { got = &r }})
			a.pump()
			if err := a.run(t, "dev.dialog.answer", tt.args); err != nil {
				t.Fatal(err)
			}
			a.pump()
			if got == nil || got.Button != tt.button || got.Cancelled != tt.cancelled {
				t.Errorf("answer %+v", got)
			}
			if a.win.Dialogs.IsShowing() {
				t.Error("the dialog still shows")
			}
		})
	}
}

func TestDevAnswerDialogErrorRoutes(t *testing.T) {
	a := newNotifyApp(t)
	for _, tt := range []struct {
		name   string
		args   any
		dialog bool
		route  string
	}{
		{"no arguments", nil, false, "parseAnswerDialog.DevAnswerDialogInvalid"},
		{"an empty label", map[string]any{"button": ""}, false, "parseAnswerDialog.DevAnswerDialogInvalid"},
		{"a number", 1.0, false, "parseAnswerDialog.DevAnswerDialogInvalid"},
		{"no dialog shows", "OK", false, "devAnswerDialog.DevAnswerDialogNone"},
		{"no such button", "Maybe", true, "devAnswerDialog.DevAnswerDialogNoButton"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if tt.dialog {
				a.win.Dialogs.Show(kit.DialogSpec{Message: "Sure?", Buttons: []string{"Yes", "No"}})
				defer a.win.Dialogs.Dismiss()
			}
			err := a.run(t, "dev.dialog.answer", tt.args)
			appErr, ok := errors.FromError(err)
			if !ok || !appErr.HasRoute(tt.route) {
				t.Errorf("err = %v, want the route %s", err, tt.route)
			}
		})
	}
	p := newTestProcess(ProcessOptions{Paths: testutil.Isolate(t)})
	err := p.Commands.Run(command.Context{Ctx: context.Background(), Source: command.SourceAPI}, "dev.dialog.answer", "OK")
	if appErr, ok := errors.FromError(err); !ok || !appErr.HasRoute("devAnswerDialog.DevAnswerDialogNoWorkspace") {
		t.Errorf("without a workspace: err = %v", err)
	}
}
