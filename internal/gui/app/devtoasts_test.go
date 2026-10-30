package app

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

func TestDevToastsListsTheToastsAndDismissRemovesOne(t *testing.T) {
	a := newNotifyApp(t)
	var toasts []DevToast
	query := func() {
		t.Helper()
		raw, err := runQuery(t, a.win.Process, "dev.toasts")
		if err != nil {
			t.Fatal(err)
		}
		toasts = nil
		if err := json.Unmarshal(raw, &toasts); err != nil {
			t.Fatal(err)
		}
	}
	query()
	if len(toasts) != 0 {
		t.Fatalf("toasts before any notification: %+v", toasts)
	}
	first := a.win.Notify.Warn("Emetos", "first warning")
	second := a.win.Notify.Info("", "second info")
	a.pump()
	query()
	// the newest first
	want := []DevToast{
		{ID: second, Severity: "info", Message: "second info"},
		{ID: first, Severity: "warning", Source: "Emetos", Message: "first warning"},
	}
	if len(toasts) != len(want) || toasts[0] != want[0] || toasts[1] != want[1] {
		t.Fatalf("dev.toasts = %+v, want %+v", toasts, want)
	}

	// dismissing one takes only that one, by the number or by {"id": n}
	if err := a.run(t, "dev.dismissNotification", float64(first)); err != nil {
		t.Fatal(err)
	}
	query()
	if len(toasts) != 1 || toasts[0].ID != second || a.has("first warning") || !a.has("second info") {
		t.Errorf("after dismissing the first: %+v", toasts)
	}
	if err := a.run(t, "dev.dismissNotification", map[string]any{"id": float64(second)}); err != nil {
		t.Fatal(err)
	}
	query()
	if len(toasts) != 0 || len(a.win.Notify.Items()) != 0 {
		t.Errorf("after dismissing both: %+v, items %+v", toasts, a.win.Notify.Items())
	}
}

func TestDevDismissNotificationErrorRoutes(t *testing.T) {
	a := newNotifyApp(t)
	for _, tt := range []struct {
		name  string
		args  any
		route string
	}{
		{"no arguments", nil, "parseDismissNotification.DevDismissNotificationInvalid"},
		{"a text", "3", "parseDismissNotification.DevDismissNotificationInvalid"},
		{"zero", 0.0, "parseDismissNotification.DevDismissNotificationInvalid"},
		{"a fraction", 1.5, "parseDismissNotification.DevDismissNotificationInvalid"},
		{"an object with no id", map[string]any{}, "parseDismissNotification.DevDismissNotificationInvalid"},
		{"an id that is not there", 99.0, "devDismissNotification.DevDismissNotificationMissing"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			err := a.run(t, "dev.dismissNotification", tt.args)
			appErr, ok := errors.FromError(err)
			if !ok || !appErr.HasRoute(tt.route) {
				t.Errorf("err = %v, want the route %s", err, tt.route)
			}
		})
	}
}

func TestDevToastsAndDismissWithoutAWorkspace(t *testing.T) {
	if !DevCommandsEnabled() {
		t.Skip("this build does not run dev commands")
	}
	p := newTestProcess(ProcessOptions{Paths: testutil.Isolate(t)})
	for _, tt := range []struct {
		id    string
		args  any
		route string
	}{
		{"dev.toasts", nil, "devToasts.DevToastsNoWorkspace"},
		{"dev.dismissNotification", 1.0, "devDismissNotification.DevDismissNotificationNoWorkspace"},
	} {
		err := p.Commands.Run(command.Context{Ctx: context.Background(), Source: command.SourceAPI}, tt.id, tt.args)
		appErr, ok := errors.FromError(err)
		if !ok || !appErr.HasRoute(tt.route) {
			t.Errorf("%s: err = %v, want the route %s", tt.id, err, tt.route)
		}
	}
}
