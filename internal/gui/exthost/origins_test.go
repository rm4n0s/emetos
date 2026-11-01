package exthost

import (
	"context"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/contextkey"
)

// The origin of a command (step 24.25, SPEC §10.18 "As built (step 24.25)", D50): the scope serves the window of the host and the views of
// its clients, a command carries the screen it came from, and what its handler asks is shown there.

// originRig is a scope with one extension whose commands ask questions.
type originRig struct {
	*tab
	api     extension.API
	current []extension.Origin // Origins().Current() seen by the handlers and the callbacks
}

func newOriginRig(t *testing.T) *originRig {
	t.Helper()
	o := &originRig{}
	f, _ := factory(manifest("oz"), func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error {
			o.api = api
			seen := func() { o.current = append(o.current, api.Origins().Current()) }
			reg := func(id string, h extension.CommandHandler) {
				if err := api.Commands().Register(extension.CommandSpec{ID: id, Title: id}, h); err != nil {
					return
				}
			}
			reg("oz.pick", func(context.Context, any) error {
				seen()
				return api.QuickPick().Show(extension.QuickPickOptions{Items: []extension.QuickPickItem{{Label: "a"}}}, func(int) { seen() })
			})
			reg("oz.input", func(context.Context, any) error {
				seen()
				return api.InputBox().Show(extension.InputBoxOptions{Prompt: "Password", Password: true}, func(string, bool) { seen() })
			})
			reg("oz.dialog", func(context.Context, any) error {
				return api.Dialogs().Ask(extension.DialogSpec{Message: "Sure?", Buttons: []extension.DialogButton{{Label: "Yes"}}}, func(extension.DialogResult) { seen() })
			})
			reg("oz.later", func(context.Context, any) error {
				// work that goes on after the handler: the origin is kept and given back, as git's job does
				origin := api.Origins().Current()
				api.Post(func() {
					api.Origins().Run(origin, func() {
						_ = api.InputBox().Show(extension.InputBoxOptions{Prompt: "Later"}, nil)
					})
				})
				return nil
			})
			reg("oz.forgotten", func(context.Context, any) error {
				api.Post(func() { _ = api.InputBox().Show(extension.InputBoxOptions{Prompt: "Forgotten"}, nil) })
				return nil
			})
			reg("oz.explicit", func(context.Context, any) error {
				return api.InputBox().Show(extension.InputBoxOptions{Prompt: "Explicit", Origin: 5}, nil)
			})
			reg("oz.note", func(context.Context, any) error {
				api.Notifications().Info("Done", extension.NotificationAction{Label: "Open", Run: seen})
				return nil
			})
			return nil
		}
	})
	r := newRig(t, f)
	o.tab = r.newTab("w")
	o.scope.Start()
	return o
}

// runAs runs a command of the tab as a client's view would: through the command context with the origin.
func (o *originRig) runAs(origin extension.Origin, id string) {
	o.t.Helper()
	cc := command.Context{Ctx: context.Background(), Source: command.SourceMenu, Origin: origin}
	if err := o.commands.Run(cc, id, nil); err != nil {
		o.t.Fatalf("%s: %v", id, err)
	}
}

func TestWhatACommandAsksIsShownOnTheScreenItCameFrom(t *testing.T) {
	const view extension.Origin = 3
	tests := []struct {
		name   string
		origin extension.Origin
		cmd    string
		want   extension.Origin
		kind   string // what is asked
	}{
		{"a list of a client", view, "oz.pick", view, "pick"},
		{"a list of the host", extension.HostOrigin, "oz.pick", extension.HostOrigin, "pick"},
		{"a box of a client", view, "oz.input", view, "input"},
		{"a dialog of a client", view, "oz.dialog", view, "dialog"},
		{"a box asked later by work that kept the origin", view, "oz.later", view, "input"},
		{"a box asked later by work that forgot it is the host's", view, "oz.forgotten", extension.HostOrigin, "input"},
		{"a box that names its screen", view, "oz.explicit", 5, "input"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			o := newOriginRig(t)
			o.deferred = true // Post hands the function to the UI thread's next turn, after the command
			o.runAs(tc.origin, tc.cmd)
			o.pump()
			var got extension.Origin
			switch tc.kind {
			case "pick":
				if len(o.picks) != 1 {
					t.Fatalf("%d lists", len(o.picks))
				}
				got = o.picks[0].Origin
			case "input":
				if len(o.inputs) != 1 {
					t.Fatalf("%d boxes", len(o.inputs))
				}
				got = o.inputs[0].Origin
			case "dialog":
				if len(o.dialogs) != 1 {
					t.Fatalf("%d dialogs", len(o.dialogs))
				}
				got = o.dialogs[0].Origin
			}
			if got != tc.want {
				t.Errorf("shown on %d, want %d", got, tc.want)
			}
		})
	}
}

func TestTheCallbackThatEndsAQuestionRunsWithTheOriginOfTheCommand(t *testing.T) {
	const view extension.Origin = 3
	o := newOriginRig(t)
	o.runAs(view, "oz.input")
	if len(o.current) != 1 || o.current[0] != view {
		t.Fatalf("the handler saw %v", o.current)
	}
	// the answer comes later, on the UI thread, outside any command
	o.inputs[0].Accepted("secret")
	if len(o.current) != 2 || o.current[1] != view {
		t.Errorf("the answer was handled on %v, want the view %d", o.current, view)
	}
	if got := o.api.Origins().Current(); got != extension.HostOrigin {
		t.Errorf("after the command the origin is %d, want the host's", got)
	}
}

func TestANotificationOfAClientsCommandIsForThatClient(t *testing.T) {
	const view extension.Origin = 3
	o := newOriginRig(t)
	o.runAs(view, "oz.note")
	items := o.notify.Items()
	if len(items) != 1 || items[0].Origin != uint64(view) {
		t.Fatalf("notifications %+v", items)
	}
	if !items[0].ShownOn(uint64(view)) || items[0].ShownOn(0) {
		t.Errorf("shown on the view %v, on the host %v", items[0].ShownOn(uint64(view)), items[0].ShownOn(0))
	}
	items[0].Actions[0].Run()
	if len(o.current) != 1 || o.current[0] != view {
		t.Errorf("the button ran on %v, want the view %d", o.current, view)
	}
	o.runAs(0, "oz.note")
	if n := o.notify.Items(); len(n) == 0 || !n[len(n)-1].ShownOn(0) || !n[len(n)-1].ShownOn(8) {
		t.Errorf("a notification of the host's window is on every screen: %+v", n)
	}
}

func TestTheOriginsAnExtensionRemembersAreTheScopes(t *testing.T) {
	o := newOriginRig(t)
	if got := o.api.Origins().Recall("terminal:1"); got != extension.HostOrigin {
		t.Errorf("an unknown key is the origin %d", got)
	}
	o.api.Origins().Remember("terminal:1", 4)
	if got := o.api.Origins().Recall("terminal:1"); got != 4 {
		t.Errorf("Recall = %d, want 4", got)
	}
	o.api.Origins().Remember("terminal:1", extension.HostOrigin)
	if got := o.api.Origins().Recall("terminal:1"); got != extension.HostOrigin {
		t.Errorf("Recall after the host typed = %d, want the host's", got)
	}
}

func TestContextKeysOfOneScreenAreNotSeenByTheOthers(t *testing.T) {
	m := manifest("ok")
	m.Contributes.ContextKeys = []string{"termFocus", "termCount"}
	f, _ := factory(m, func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error {
			_ = api.Context().Set("termCount", 2)
			_ = api.Context().SetFor(3, "termFocus", true)
			_ = api.Context().SetFor(extension.HostOrigin, "termFocus", false)
			return nil
		}
	})
	r := newRig(t, f)
	tb := r.newTab("w")
	view := contextkey.NewRoot()
	tb.originKeys = map[extension.Origin]*contextkey.Scope{3: view}
	var shared []string
	tb.scope.OnSharedKey(func(key string, _ any, reset bool) {
		if !reset {
			shared = append(shared, key)
		}
	})
	tb.scope.Start()
	if got := view.Get("termFocus"); got != true {
		t.Errorf("the view's termFocus = %v, want true", got)
	}
	if got := tb.ctxKeys.Get("termFocus"); got != false {
		t.Errorf("the host's termFocus = %v, want false: the key of a screen is its own", got)
	}
	if got := tb.ctxKeys.Get("termCount"); got != float64(2) {
		t.Errorf("the host's termCount = %v", got)
	}
	if len(shared) != 1 || shared[0] != "termCount" {
		t.Errorf("shared keys %v: only the keys of the workspace are copied to the views", shared)
	}
	if v := tb.scope.SharedKeys(); len(v) != 1 || v["termCount"] != 2 {
		t.Errorf("SharedKeys = %v", v)
	}
}
