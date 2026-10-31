package app

import (
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/gui/exthost"
)

func TestDevQuickInputTypesIntoTheBoxThatIsShown(t *testing.T) {
	w := settingsWindow(t, nil)
	t.Cleanup(w.release)

	wantRoute := func(err error, route string) {
		t.Helper()
		appErr, ok := errors.FromError(err)
		if !ok || !appErr.HasRoute(route) {
			t.Errorf("err = %v, want the route %s", err, route)
		}
	}
	wantRoute(devQuickInputType(w, "abc"), "devQuickInputType.DevQuickInputNotShown")
	wantRoute(devQuickInputType(nil, "abc"), "devQuickInputType.DevQuickInputNotShown")
	wantRoute(devQuickInputType(w, 3.0), "devQuickInputText.DevQuickInputInvalid")
	wantRoute(devQuickInputType(w, map[string]any{"text": 3.0}), "devQuickInputText.DevQuickInputInvalid")

	var accepted []string
	w.extensionInputBox(exthost.InputBoxRequest{Prompt: "Password", Password: true, Accepted: func(v string) { accepted = append(accepted, v) }, Hidden: func() {}})
	for _, args := range []any{"hunter2", map[string]any{"text": "hunter3"}} {
		if err := devQuickInputType(w, args); err != nil {
			t.Fatal(err)
		}
	}
	if got := w.QuickInput.Picker().Value(); got != "hunter3" {
		t.Errorf("the box holds %q after the second call", got)
	}
	if !w.QuickInput.Picker().Obscure {
		t.Error("the box is not a password box")
	}

	// the command exists in a debug build
	found := false
	for _, m := range w.Process.Commands.All() {
		found = found || m.ID == "dev.quickInput.type"
	}
	if DevCommandsEnabled() && !found {
		t.Error("dev.quickInput.type is not registered")
	}
	if len(accepted) != 0 {
		t.Errorf("typing accepted %v", accepted)
	}
}
