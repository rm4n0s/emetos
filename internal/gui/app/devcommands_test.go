package app

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"github.com/rm4n0s/glutter/foundation"

	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/gui/testutil"
)

func TestParseDevCommands(t *testing.T) {
	cases := []struct {
		name string
		text string
		want []DevStep
	}{
		{"empty", "", nil},
		{"blank", "  \n", nil},
		{"no steps", "[]", []DevStep{}},
		{"a command", `[["workbench.action.quickOpen"]]`, []DevStep{{Command: "workbench.action.quickOpen"}}},
		{"a command with a string", `[["workbench.action.quickOpen", ">line"]]`, []DevStep{{Command: "workbench.action.quickOpen", Args: ">line"}}},
		{"a command with an object", `[["dev.open", {"path": "a.go", "line": 3}]]`, []DevStep{{Command: "dev.open", Args: map[string]any{"path": "a.go", "line": 3.0}}}},
		{"a null argument", `[["dev.x", null]]`, []DevStep{{Command: "dev.x"}}},
		{"a sleep", `[["sleep", "200ms"]]`, []DevStep{{Sleep: 200 * time.Millisecond}}},
		{"steps in order", `[["a"], ["sleep", "1s"], ["b", 1]]`, []DevStep{{Command: "a"}, {Sleep: time.Second}, {Command: "b", Args: 1.0}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseDevCommands(tc.text)
			if err != nil {
				t.Fatalf("ParseDevCommands: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseDevCommandsErrors(t *testing.T) {
	cases := []struct{ name, text string }{
		{"not JSON", `[["a"`},
		{"not an array", `{"a": 1}`},
		{"a step that is not an array", `["a"]`},
		{"an empty step", `[[]]`},
		{"a step with three parts", `[["a", 1, 2]]`},
		{"a command that is not a string", `[[3]]`},
		{"an empty command", `[[""]]`},
		{"a sleep without a duration", `[["sleep"]]`},
		{"a sleep with a number", `[["sleep", 5]]`},
		{"a sleep with a bad duration", `[["sleep", "soon"]]`},
		{"a negative sleep", `[["sleep", "-1s"]]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseDevCommands(tc.text)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute("ParseDevCommands.DevCommandsInvalid") {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
		})
	}
}

// TestDevCommandsAreGated checks the gate of SPEC D15: debug builds and builds with the
// emetos_dev tag run dev commands, and a release build registers none.
func TestDevCommandsAreGated(t *testing.T) {
	if got, want := DevCommandsEnabled(), foundation.DebugMode || devBuild; got != want {
		t.Errorf("DevCommandsEnabled = %v, want %v", got, want)
	}
	r := command.NewRegistry()
	err := RegisterDev(r, command.Meta{ID: "dev.thing"}, func(command.Context, any) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r.Lookup("dev.thing"); ok != DevCommandsEnabled() {
		t.Errorf("a dev command is registered = %v, want %v", ok, DevCommandsEnabled())
	}
	if r.HasHandler("dev.thing") != DevCommandsEnabled() {
		t.Error("the handler follows the registration")
	}

	// An id without the dev. prefix is a mistake whatever the build.
	err = RegisterDev(r, command.Meta{ID: "thing"}, func(command.Context, any) error { return nil })
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("RegisterDev.DevCommandIDInvalid") {
		t.Errorf("err = %v", err)
	}
}

func TestDevRunnerRunsStepsInOrder(t *testing.T) {
	r := command.NewRegistry()
	var log []string
	for _, id := range []string{"a", "b", "fail"} {
		r.MustRegister(command.Meta{ID: id})
		if _, err := r.SetHandler(id, func(_ command.Context, args any) error {
			log = append(log, id)
			if id == "fail" {
				return errors.New("Boom", "it failed")
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	var failed []string
	runner := &DevRunner{
		Steps:    []DevStep{{Command: "a"}, {Command: "fail"}, {Sleep: time.Millisecond}, {Command: "missing"}, {Command: "b"}},
		Commands: r,
		Post:     func(f func()) { f() },
		OnError:  func(step DevStep, err error) { failed = append(failed, step.Command) },
	}
	runner.Run(context.Background())
	if want := []string{"a", "fail", "b"}; !reflect.DeepEqual(log, want) {
		t.Errorf("ran %v, want %v", log, want)
	}
	if want := []string{"fail", "missing"}; !reflect.DeepEqual(failed, want) {
		t.Errorf("errors for %v, want %v", failed, want)
	}
}

func TestDevRunnerPassesTheArgument(t *testing.T) {
	r := command.NewRegistry()
	r.MustRegister(command.Meta{ID: "workbench.action.quickOpen"})
	var got []any
	if _, err := r.SetHandler("workbench.action.quickOpen", func(_ command.Context, args any) error {
		got = append(got, args)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	steps, err := ParseDevCommands(`[["workbench.action.quickOpen", ">line"], ["workbench.action.quickOpen"]]`)
	if err != nil {
		t.Fatal(err)
	}
	(&DevRunner{Steps: steps, Commands: r, Post: func(f func()) { f() }}).Run(context.Background())
	if want := []any{">line", nil}; !reflect.DeepEqual(got, want) {
		t.Errorf("arguments %v, want %v", got, want)
	}
}

func TestDevRunnerStopsWhenCanceled(t *testing.T) {
	r := command.NewRegistry()
	r.MustRegister(command.Meta{ID: "a"})
	ran := 0
	if _, err := r.SetHandler("a", func(command.Context, any) error { ran++; return nil }); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	runner := &DevRunner{
		Steps:    []DevStep{{Command: "a"}, {Sleep: time.Hour}, {Command: "a"}},
		Commands: r,
		Post:     func(f func()) { f(); cancel() },
	}
	done := make(chan struct{})
	go func() { runner.Run(ctx); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the runner did not stop when its context was canceled")
	}
	if ran != 1 {
		t.Errorf("the command ran %d times, want 1", ran)
	}
}

func TestDevStepsFromTextAndEnvironment(t *testing.T) {
	p := NewProcess(ProcessOptions{Paths: newTestWindow(t, "").Process.Paths})
	if !DevCommandsEnabled() {
		t.Skip("this build does not run dev commands")
	}
	if got := devSteps(p, `[["a"]]`); len(got) != 1 {
		t.Errorf("from the text: %v", got)
	}
	t.Setenv(DevCommandsEnv, `[["b"], ["sleep", "1ms"]]`)
	if got := devSteps(p, ""); len(got) != 2 {
		t.Errorf("from the environment: %v", got)
	}
	if got := devSteps(p, "not json"); got != nil {
		t.Errorf("a text that does not parse gives no steps: %v", got)
	}
	items := p.Notify.Items()
	if len(items) != 1 || items[0].Source != "Emetos" {
		t.Errorf("the mistake is reported once: %+v", items)
	}
}

func TestTheEditorDevCommandsAreInTheProcess(t *testing.T) {
	p := NewProcess(ProcessOptions{Paths: testutil.Isolate(t)})
	for _, id := range []string{"dev.type", "dev.scroll"} {
		if _, ok := p.Commands.Lookup(id); ok != DevCommandsEnabled() {
			t.Errorf("%s is registered = %v, want %v", id, ok, DevCommandsEnabled())
		}
		if !DevCommandsEnabled() {
			continue
		}
		// With no editor in front, the command says so.
		err := p.Commands.Run(command.Context{}, id, "x")
		appErr, ok := errors.FromError(err)
		if !ok || !appErr.HasRoute("DevNoEditor") {
			t.Errorf("%s without an editor: %v", id, err)
		}
	}
}
