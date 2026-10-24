package app

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/gluttertest"
	"github.com/rm4n0s/glutter/widgets"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/keybinding"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/gui/kit"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

func TestParseNotify(t *testing.T) {
	cases := []struct {
		name string
		args any
		want notifyArguments
	}{
		{"a message", "hello", notifyArguments{Message: "hello"}},
		{"an info", map[string]any{"severity": "info", "message": "m"}, notifyArguments{Severity: notify.Info, Message: "m"}},
		{"a warning", map[string]any{"severity": "warning", "message": "m"}, notifyArguments{Severity: notify.Warning, Message: "m"}},
		{"an error", map[string]any{"severity": "error", "message": "m"}, notifyArguments{Severity: notify.Error, Message: "m"}},
		{"no severity is an info", map[string]any{"message": "m"}, notifyArguments{Severity: notify.Info, Message: "m"}},
		{"a source", map[string]any{"message": "m", "source": "Git"}, notifyArguments{Message: "m", Source: "Git"}},
		{"actions", map[string]any{"message": "m", "actions": []any{"Yes", "No"}}, notifyArguments{Message: "m", Actions: []string{"Yes", "No"}}},
		{"no actions", map[string]any{"message": "m", "actions": []any{}}, notifyArguments{Message: "m"}},
		{"a progress", map[string]any{"message": "m", "progress": 40.0}, notifyArguments{Message: "m", Progress: &notify.Progress{Total: 100, Worked: 40}}},
		{"zero progress", map[string]any{"message": "m", "progress": 0.0}, notifyArguments{Message: "m", Progress: &notify.Progress{Total: 100}}},
		{"an infinite progress", map[string]any{"message": "m", "progress": "infinite"}, notifyArguments{Message: "m", Progress: &notify.Progress{Infinite: true}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseNotify(tc.args)
			if err != nil {
				t.Fatal(err)
			}
			if got.Severity != tc.want.Severity || got.Message != tc.want.Message || got.Source != tc.want.Source ||
				strings.Join(got.Actions, ",") != strings.Join(tc.want.Actions, ",") ||
				(got.Progress == nil) != (tc.want.Progress == nil) || (got.Progress != nil && *got.Progress != *tc.want.Progress) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseNotifyErrors(t *testing.T) {
	cases := []struct {
		name  string
		args  any
		route string
	}{
		{"no arguments", nil, "parseNotify.DevNotifyArgumentsInvalid"},
		{"a number", 3.0, "parseNotify.DevNotifyArgumentsInvalid"},
		{"a list", []any{"a"}, "parseNotify.DevNotifyArgumentsInvalid"},
		{"an unknown severity", map[string]any{"severity": "fatal", "message": "m"}, "parseNotify.DevNotifySeverityInvalid"},
		{"a severity that is not a text", map[string]any{"severity": 2.0, "message": "m"}, "parseNotify.DevNotifySeverityInvalid"},
		{"no message", map[string]any{"severity": "info"}, "parseNotify.DevNotifyMessageInvalid"},
		{"an empty message", map[string]any{"message": ""}, "parseNotify.DevNotifyMessageInvalid"},
		{"a message that is not a text", map[string]any{"message": 4.0}, "parseNotify.DevNotifyMessageInvalid"},
		{"a source that is not a text", map[string]any{"message": "m", "source": 1.0}, "parseNotify.DevNotifySourceInvalid"},
		{"actions that are not a list", map[string]any{"message": "m", "actions": "Yes"}, "parseNotify.DevNotifyActionsInvalid"},
		{"an action that is not a text", map[string]any{"message": "m", "actions": []any{1.0}}, "parseNotify.DevNotifyActionsInvalid"},
		{"an empty action", map[string]any{"message": "m", "actions": []any{""}}, "parseNotify.DevNotifyActionsInvalid"},
		{"a progress over 100", map[string]any{"message": "m", "progress": 101.0}, "parseNotify.DevNotifyProgressInvalid"},
		{"a negative progress", map[string]any{"message": "m", "progress": -1.0}, "parseNotify.DevNotifyProgressInvalid"},
		{"an unknown progress text", map[string]any{"message": "m", "progress": "forever"}, "parseNotify.DevNotifyProgressInvalid"},
		{"a progress that is a switch", map[string]any{"message": "m", "progress": true}, "parseNotify.DevNotifyProgressInvalid"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseNotify(tc.args)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tc.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

// notifyApp is a window with nothing open, whose process logs to a buffer.
type notifyApp struct {
	win *Window
	ts  *gluttertest.Tester
	log *bytes.Buffer
}

func newNotifyApp(t *testing.T) *notifyApp {
	t.Helper()
	if !DevCommandsEnabled() {
		t.Skip("this build does not run dev commands")
	}
	paths := testutil.Isolate(t)
	testutil.Settings(t, paths, nil)
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	win, err := NewWindow(context.Background(), NewProcess(ProcessOptions{Paths: paths, Logger: logger}), "")
	if err != nil {
		t.Fatal(err)
	}
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, nil, nil))
	a := &notifyApp{win: win, ts: ts, log: &buf}
	a.pump()
	return a
}

func (a *notifyApp) pump() {
	for range 4 {
		a.ts.Pump(20 * time.Millisecond)
	}
}

func (a *notifyApp) run(t *testing.T, id string, args any) error {
	t.Helper()
	err := a.win.Process.Commands.Run(command.Context{Ctx: t.Context(), Keys: a.win.Keys, Source: command.SourceAPI}, id, args)
	a.pump()
	return err
}

func (a *notifyApp) has(text string) bool { return a.ts.Exists(gluttertest.ByText(text)) }

func labelButton(label string) gluttertest.Finder {
	return gluttertest.ByPredicate("button "+label, func(w widgets.Widget) bool {
		b, ok := w.(*kit.Button)
		return ok && b.Label() == label
	})
}

func TestDevNotifyShowsAToastWithActions(t *testing.T) {
	a := newNotifyApp(t)
	args := map[string]any{"severity": "info", "message": "Info with actions", "actions": []any{"Yes", "No"}}
	if err := a.run(t, "dev.notify", args); err != nil {
		t.Fatal(err)
	}
	if err := a.run(t, "dev.notify", map[string]any{"severity": "error", "message": "An error"}); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{"Info with actions", "An error"} {
		if !a.has(text) {
			t.Errorf("%q is not on screen", text)
		}
	}
	if !a.ts.Exists(labelButton("Yes")) || !a.ts.Exists(labelButton("No")) {
		t.Fatal("the buttons are not on screen")
	}
	items := a.win.Process.Notify.Items()
	if len(items) != 2 || items[0].Severity != notify.Info || len(items[0].Actions) != 2 || items[1].Severity != notify.Error {
		t.Fatalf("items: %+v", items)
	}
	// a button closes the toast and the log says which one was pressed
	a.ts.Tap(labelButton("No"))
	a.pump()
	if a.has("Info with actions") || len(a.win.Process.Notify.Items()) != 1 {
		t.Error("the button did not close the toast")
	}
	if !strings.Contains(a.log.String(), `msg="dev.notify action" action=No`) {
		t.Errorf("the log does not say which action ran:\n%s", a.log.String())
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestDevNotifyWithProgressAndSource(t *testing.T) {
	a := newNotifyApp(t)
	if err := a.run(t, "dev.notify", map[string]any{"message": "Indexing", "source": "Language", "progress": 40.0}); err != nil {
		t.Fatal(err)
	}
	if !a.ts.Exists(gluttertest.ByType[*kit.ProgressBar]()) {
		t.Error("no progress bar")
	}
	items := a.win.Process.Notify.Items()
	if len(items) != 1 || items[0].Source != "Language" || !items[0].HasActiveProgress() {
		t.Errorf("items: %+v", items)
	}
	// a string is a message
	if err := a.run(t, "dev.notify", "plain"); err != nil {
		t.Fatal(err)
	}
	if !a.has("plain") {
		t.Error("the message is not on screen")
	}
}

func TestDevNotifyFailsOnBadArguments(t *testing.T) {
	a := newNotifyApp(t)
	err := a.run(t, "dev.notify", map[string]any{"severity": "loud", "message": "m"})
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("parseNotify.DevNotifySeverityInvalid") {
		t.Errorf("err = %v", err)
	}
	if len(a.win.Process.Notify.Items()) != 0 {
		t.Error("a notification was sent")
	}
}

func TestDevNotifyIsRegisteredOnlyInDevBuilds(t *testing.T) {
	p := NewProcess(ProcessOptions{Paths: testutil.Isolate(t)})
	if _, ok := p.Commands.Lookup("dev.notify"); ok != DevCommandsEnabled() {
		t.Errorf("dev.notify is registered = %v, want %v", ok, DevCommandsEnabled())
	}
}

func TestNotificationKeysRunThroughTheDispatcher(t *testing.T) {
	a := newNotifyApp(t)
	svc := a.win.Process.Notify
	svc.Info("", "a toast")
	a.pump()
	if !a.has("a toast") || a.win.Keys.Get("notificationToastsVisible") != true {
		t.Fatalf("no toast: keys %v", a.win.Keys.Get("notificationToastsVisible"))
	}
	// Escape hides the toasts, and the notification stays
	if !sendChord(a.ts, keybinding.Chord{Key: keybinding.Escape}) {
		t.Error("escape was not used while a toast shows")
	}
	a.pump()
	if a.has("a toast") || a.win.Keys.Get("notificationToastsVisible") != false || len(svc.Items()) != 1 {
		t.Error("escape did not hide the toasts")
	}
	if sendChord(a.ts, keybinding.Chord{Key: keybinding.Escape}) {
		t.Error("escape was used with nothing to hide")
	}

	// Ctrl+K Ctrl+Shift+N shows the center
	sendChord(a.ts, ctrl(keybinding.KeyK))
	if !sendChord(a.ts, keybinding.Chord{Ctrl: true, Shift: true, Key: keybinding.KeyN}) {
		t.Error("ctrl+k ctrl+shift+n was not used")
	}
	a.pump()
	if !a.win.Notifications.CenterVisible() || a.win.Keys.Get("notificationCenterVisible") != true || !a.has("a toast") || !a.has("NOTIFICATIONS") {
		t.Fatalf("the center is not open: visible %v", a.win.Notifications.CenterVisible())
	}
	// Escape closes it
	if !sendChord(a.ts, keybinding.Chord{Key: keybinding.Escape}) {
		t.Error("escape was not used with the center open")
	}
	a.pump()
	if a.win.Notifications.CenterVisible() || a.win.Keys.Get("notificationCenterVisible") != false || a.has("a toast") {
		t.Error("escape did not close the center")
	}
	if errs := a.ts.TakeErrors(); len(errs) != 0 {
		t.Errorf("errors: %v", errs)
	}
}

func TestTheEditorKeepsEscapeWhenNothingShows(t *testing.T) {
	a := newKeyApp(t, "alpha beta\n")
	a.ts.SendKeyEvent('a', gluttertest.KeyOpts{Character: "a"})
	// no toast and no center: Escape is not taken by the notifications
	if sendChord(a.ts, keybinding.Chord{Key: keybinding.Escape}) {
		t.Error("escape was used with no notification showing")
	}
}

func TestANotificationBeforeTheWindowShows(t *testing.T) {
	// Run reports a file that cannot be opened before the window is up: the toast shows when it is
	paths := testutil.Isolate(t)
	testutil.Settings(t, paths, nil)
	win, err := NewWindow(context.Background(), NewProcess(ProcessOptions{Paths: paths}), "")
	if err != nil {
		t.Fatal(err)
	}
	win.Process.Notify.Error("Emetos", "Cannot open nothing.txt")
	ts := testutil.New(t, testWindowSize)
	ts.PumpWidget(newApp(context.Background(), win, nil, nil))
	for range 4 {
		ts.Pump(20 * time.Millisecond)
	}
	if !ts.Exists(gluttertest.ByText("Cannot open nothing.txt")) {
		t.Error("the toast is not on screen")
	}
}

func TestEveryNotificationCommandHasAHandlerInTheWindow(t *testing.T) {
	a := newNotifyApp(t)
	var n int
	for _, meta := range a.win.Process.Commands.All() {
		if !strings.HasPrefix(meta.ID, "notifications.") {
			continue
		}
		n++
		if !a.win.Process.Commands.HasHandler(meta.ID) {
			t.Errorf("%s has no handler", meta.ID)
		}
	}
	if n != 7 {
		t.Errorf("%d notification commands, want 7", n)
	}
}
