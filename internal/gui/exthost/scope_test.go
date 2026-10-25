package exthost

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

func TestActivationEvents(t *testing.T) {
	tests := []struct {
		name  string
		event string
		files []string
		// fire says how the event happens in the scope.
		fire func(tb *tab)
	}{
		{"onStartup", extension.OnStartup, nil, func(tb *tab) { tb.scope.Start() }},
		{"onLanguage", extension.OnLanguage("go"), nil, func(tb *tab) { tb.scope.Fire(extension.OnLanguage("go")) }},
		{"onFile at the root", extension.OnFile("go.mod"), []string{"main.go", "go.mod"}, func(tb *tab) { tb.scope.Start() }},
		{"onFile anywhere", extension.OnFile("**/go.mod"), []string{"cmd/app/go.mod"}, func(tb *tab) { tb.scope.CheckFiles() }},
		{"onCommand", extension.OnCommand("ext.run"), nil, func(tb *tab) {
			if err := tb.run("ext.run"); err != nil {
				t.Fatal(err)
			}
		}},
		{"onView", extension.OnView("ext.view"), nil, func(tb *tab) { tb.scope.Fire(extension.OnView("ext.view")) }},
		{"onWindowTab", extension.OnWindowTab("scratch"), nil, func(tb *tab) { tb.scope.Fire(extension.OnWindowTab("scratch")) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := manifest("emetos.ev", tt.event)
			m.Contributes.Commands = []extension.CommandSpec{{ID: "ext.run", Title: "Run"}}
			f, rec := factory(m, func(f *fake) {
				f.activate = func(_ context.Context, api extension.API) error {
					return api.Commands().Handle("ext.run", func(context.Context, any) error { return nil })
				}
			})
			other, otherRec := factory(manifest("emetos.other", extension.OnLanguage("rust")), nil)
			r := newRig(t, f, other)
			tb := r.newTab("one")
			tb.files = tt.files
			if got := tb.scope.State("emetos.ev"); got != StateInactive || len(rec.all()) != 0 {
				t.Fatalf("before the event: state %v, %d instances; want inactive and none", got, len(rec.all()))
			}
			tt.fire(tb)
			if got := tb.scope.State("emetos.ev"); got != StateActive {
				t.Errorf("after the event the state is %v, want active", got)
			}
			if n := len(rec.all()); n != 1 {
				t.Errorf("%d instances, want 1", n)
			}
			if got := tb.scope.State("emetos.other"); got != StateInactive || len(otherRec.all()) != 0 {
				t.Errorf("an extension with another event is %v with %d instances", got, len(otherRec.all()))
			}
			// the same event again does not make a second instance
			tt.fire(tb)
			if n := len(rec.all()); n != 1 {
				t.Errorf("%d instances after the event happened twice, want 1", n)
			}
		})
	}
}

func TestOnFileDoesNotMatchOtherFiles(t *testing.T) {
	f, rec := factory(manifest("emetos.ev", extension.OnFile("go.mod")), nil)
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.files = []string{"sub/go.mod", "main.go"}
	tb.scope.Start()
	if got := tb.scope.State("emetos.ev"); got != StateInactive || len(rec.all()) != 0 {
		t.Errorf("go.mod below the root activated the extension: %v", got)
	}
	tb.files = append(tb.files, "go.mod")
	tb.scope.CheckFiles()
	if got := tb.scope.State("emetos.ev"); got != StateActive {
		t.Errorf("go.mod in the root left the extension %v", got)
	}
}

func TestAnExtensionWithoutEventsIsNeverActivatedByItself(t *testing.T) {
	m := manifest("emetos.quiet")
	m.ActivationEvents = nil
	f, rec := factory(m, nil)
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.Start()
	tb.scope.Fire(extension.OnLanguage("go"))
	if len(rec.all()) != 0 {
		t.Errorf("an extension with no events was activated")
	}
}

func TestTwoScopesGetTwoInstances(t *testing.T) {
	f, rec := factory(manifest("emetos.two"), nil)
	r := newRig(t, f)
	a, b := r.newTab("a"), r.newTab("b")
	a.scope.Start()
	if len(rec.all()) != 1 || b.scope.State("emetos.two") != StateInactive {
		t.Fatalf("activating in a made %d instances and left b %v", len(rec.all()), b.scope.State("emetos.two"))
	}
	b.scope.Start()
	list := rec.all()
	if len(list) != 2 || list[0] == list[1] {
		t.Fatalf("%d instances, want two different ones", len(list))
	}
	if a.scope.State("emetos.two") != StateActive || b.scope.State("emetos.two") != StateActive {
		t.Errorf("both scopes should be active")
	}
	// closing a cancels only its context
	a.scope.Close()
	if list[0].ctx.Err() == nil {
		t.Errorf("closing the scope did not cancel the context of its instance")
	}
	if list[1].ctx.Err() != nil {
		t.Errorf("closing the scope a cancelled the context of the scope b")
	}
	if got := len(r.host.Scopes()); got != 1 {
		t.Errorf("the host has %d scopes after one closed, want 1", got)
	}
}

func TestClosingAScopeRemovesEveryContribution(t *testing.T) {
	m := manifest("emetos.all")
	m.Contributes = extension.Contributes{
		Commands:    []extension.CommandSpec{{ID: "all.declared", Title: "Declared"}},
		Menus:       []extension.MenuItem{{Menu: extension.MenuView, Command: "all.declared", Group: "9_all"}},
		Keybindings: []extension.Keybinding{{Command: "all.declared", Key: "ctrl+alt+d"}},
	}
	f, rec := factory(m, func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error {
			ok := func(context.Context, any) error { return nil }
			steps := []error{
				api.Commands().Handle("all.declared", ok),
				api.Commands().Register(extension.CommandSpec{ID: "all.dynamic", Title: "Dynamic"}, ok),
				api.Keybindings().Add(extension.Keybinding{Command: "all.dynamic", Key: "ctrl+alt+e"}),
				api.Menus().Add(extension.MenuItem{Menu: extension.MenuEdit, Command: "all.dynamic", Group: "9_all"}),
				api.Settings().Register(extension.SettingSpec{Key: "emetos.all.size", Type: "number", Default: 3}),
			}
			if _, err := api.StatusBar().Add(extension.StatusItem{ID: "item", Text: "all"}); err != nil {
				steps = append(steps, err)
			}
			for _, err := range steps {
				if err != nil {
					return err
				}
			}
			return nil
		}
	})
	r := newRig(t, f)
	tb := r.newTab("one")
	// the declared contributions are there before the activation
	if _, ok := tb.commands.Lookup("all.declared"); !ok {
		t.Fatal("the declared command is not registered before the activation")
	}
	if !slices.Contains(tb.menuCommands(extension.MenuView), "all.declared") || !slices.Equal(tb.keys.commands(), []string{"all.declared"}) {
		t.Fatalf("the declared menu item and key are missing: %v, %v", tb.menuCommands(extension.MenuView), tb.keys.commands())
	}
	if len(rec.all()) != 0 {
		t.Fatal("the extension was activated before its event")
	}
	tb.scope.Start()
	if tb.scope.State("emetos.all") != StateActive {
		t.Fatalf("not active: %v", tb.scope.Failure("emetos.all"))
	}
	if _, ok := tb.commands.Lookup("all.dynamic"); !ok {
		t.Error("the dynamic command is missing")
	}
	if _, ok := r.registry.Lookup("emetos.all.size"); !ok {
		t.Error("the setting is missing")
	}
	if !slices.Equal(tb.statusIDs(), []string{"extension.emetos.all.item"}) {
		t.Errorf("status items = %v", tb.statusIDs())
	}
	if len(tb.keys.commands()) != 2 || !slices.Contains(tb.menuCommands(extension.MenuEdit), "all.dynamic") {
		t.Errorf("keys %v, edit menu %v", tb.keys.commands(), tb.menuCommands(extension.MenuEdit))
	}

	tb.scope.Close()

	if rec.all()[0].ctx.Err() == nil {
		t.Error("the context was not cancelled")
	}
	for _, id := range []string{"all.declared", "all.dynamic"} {
		if _, ok := tb.commands.Lookup(id); ok {
			t.Errorf("command %s is still registered", id)
		}
	}
	if _, ok := r.registry.Lookup("emetos.all.size"); ok {
		t.Error("the setting is still registered")
	}
	if got := tb.statusIDs(); len(got) != 0 {
		t.Errorf("status items left: %v", got)
	}
	if got := tb.keys.commands(); len(got) != 0 {
		t.Errorf("keybindings left: %v", got)
	}
	if got := append(tb.menuCommands(extension.MenuView), tb.menuCommands(extension.MenuEdit)...); slices.Contains(got, "all.declared") || slices.Contains(got, "all.dynamic") {
		t.Errorf("menu items left: %v", got)
	}
}

func TestAScopeKeepsTheSettingsOfAnotherScopeAlive(t *testing.T) {
	f, _ := factory(manifest("emetos.set"), func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error {
			return api.Settings().Register(extension.SettingSpec{Key: "emetos.set.name", Type: "string", Default: "x", Description: "d"})
		}
	})
	r := newRig(t, f)
	a, b := r.newTab("a"), r.newTab("b")
	a.scope.Start()
	b.scope.Start()
	if a.scope.State("emetos.set") != StateActive || b.scope.State("emetos.set") != StateActive {
		t.Fatalf("a: %v, b: %v", a.scope.Failure("emetos.set"), b.scope.Failure("emetos.set"))
	}
	a.scope.Close()
	if _, ok := r.registry.Lookup("emetos.set.name"); !ok {
		t.Error("closing the scope a removed a setting that the scope b uses")
	}
	b.scope.Close()
	if _, ok := r.registry.Lookup("emetos.set.name"); ok {
		t.Error("the setting is still registered after the last scope closed")
	}
}

func TestDeclaredCommandActivatesAndRunsTheHandler(t *testing.T) {
	m := manifest("emetos.cmd", extension.OnLanguage("never"))
	m.Contributes.Commands = []extension.CommandSpec{{ID: "cmd.go", Title: "Go", Category: "Cmd"}}
	var got []any
	f, rec := factory(m, func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error {
			return api.Commands().Handle("cmd.go", func(_ context.Context, args any) error { got = append(got, args); return nil })
		}
	})
	r := newRig(t, f)
	tb := r.newTab("one")
	meta, ok := tb.commands.Lookup("cmd.go")
	if !ok || meta.Title != "Go" || meta.Category != "Cmd" {
		t.Fatalf("the command is %+v, %v", meta, ok)
	}
	if err := tb.commands.Run(commandContext(), "cmd.go", "arg"); err != nil {
		t.Fatal(err)
	}
	if len(rec.all()) != 1 || !slices.Equal(got, []any{"arg"}) {
		t.Errorf("instances %d, handler calls %v", len(rec.all()), got)
	}
	if err := tb.commands.Run(commandContext(), "cmd.go", "again"); err != nil {
		t.Fatal(err)
	}
	if len(rec.all()) != 1 || len(got) != 2 {
		t.Errorf("the second run made %d instances, %d calls", len(rec.all()), len(got))
	}
}

func TestAnErrorOfACommandIsTheErrorOfTheCommand(t *testing.T) {
	m := manifest("emetos.cmd")
	m.Contributes.Commands = []extension.CommandSpec{{ID: "cmd.fail", Title: "Fail"}, {ID: "cmd.unset", Title: "Unset"}}
	cause := errors.New("Cause", "it did not work")
	f, _ := factory(m, func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error {
			return api.Commands().Handle("cmd.fail", func(context.Context, any) error { return cause })
		}
	})
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.Start()
	tests := []struct{ name, id, route string }{
		{"the handler returns an error", "cmd.fail", "Scope.runCommand.ExtensionCommandFailed"},
		{"a declared command with no handler", "cmd.unset", "ExtensionCommandUnhandled"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tb.run(tt.id)
			appErr, ok := errors.FromError(err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
			}
			if !appErr.HasRoute(tt.route) {
				t.Errorf("unexpected failure path: %s", appErr.Route())
			}
			if tb.scope.State("emetos.cmd") != StateActive {
				t.Errorf("an error of a command deactivated the extension")
			}
			if len(tb.notifications()) != 0 {
				t.Errorf("an error of a command was notified as a failure of the extension: %v", tb.notifications())
			}
		})
	}
	var inner *errors.Error
	if !errors.As(tb.run("cmd.fail"), &inner) {
		t.Fatal("no error")
	}
	if errors.Unwrap(tb.run("cmd.fail")) != error(cause) {
		t.Errorf("the error of the handler is not the cause")
	}
}

func TestContainmentOfFailures(t *testing.T) {
	boom := func() { panic("boom") }
	statusItem := func(api extension.API) (extension.StatusHandle, error) {
		return api.StatusBar().Add(extension.StatusItem{ID: "item", Text: "x", OnClick: boom})
	}
	tests := []struct {
		name string
		// setup is the Activate of the failing extension; trigger makes the failure after the activation.
		setup   func(ctx context.Context, api extension.API) error
		trigger func(tb *tab)
		route   string
		tag     string
		where   string
	}{
		{
			name:  "Activate panics",
			setup: func(context.Context, extension.API) error { panic("boom") },
			route: "Scope.callActivate.func1.ExtensionActivatePanicked", tag: "ExtensionActivatePanicked", where: "activate",
		},
		{
			name:  "Activate returns an error",
			setup: func(context.Context, extension.API) error { return errors.New("Cause", "no good") },
			route: "Scope.callActivate.ExtensionActivateFailed", tag: "ExtensionActivateFailed", where: "activate",
		},
		{
			name: "a declared command handler panics",
			setup: func(_ context.Context, api extension.API) error {
				return api.Commands().Handle("x.declared", func(context.Context, any) error { panic("boom") })
			},
			trigger: func(tb *tab) { _ = tb.run("x.declared") },
			route:   "Scope.runCommand.func1.ExtensionCommandPanicked", tag: "ExtensionCommandPanicked", where: "command x.declared",
		},
		{
			name: "a registered command handler panics",
			setup: func(_ context.Context, api extension.API) error {
				return api.Commands().Register(extension.CommandSpec{ID: "x.dynamic", Title: "D"}, func(context.Context, any) error { panic("boom") })
			},
			trigger: func(tb *tab) { _ = tb.run("x.dynamic") },
			route:   "Scope.runCommand.func1.ExtensionCommandPanicked", tag: "ExtensionCommandPanicked", where: "command x.dynamic",
		},
		{
			name: "a status bar callback panics",
			setup: func(_ context.Context, api extension.API) error {
				_, err := statusItem(api)
				return err
			},
			trigger: func(tb *tab) {
				if items := tb.bar.Items(0); len(items) > 0 {
					_ = tb.run(items[0].Entry.Command)
				}
			},
			route: "Scope.runStatusClick.func1.ExtensionStatusCallbackPanicked", tag: "ExtensionStatusCallbackPanicked", where: "status bar item item",
		},
		{
			name: "a notification action panics",
			setup: func(_ context.Context, api extension.API) error {
				api.Notifications().Info("hello", extension.NotificationAction{Label: "Go", Run: boom})
				return nil
			},
			trigger: func(tb *tab) {
				for _, it := range tb.notify.Items() {
					if it.Message == "hello" {
						it.Actions[0].Run()
					}
				}
			},
			route: "Scope.runNotificationAction.func1.ExtensionNotificationActionPanicked", tag: "ExtensionNotificationActionPanicked", where: "notification action Go",
		},
		{
			name: "a posted function panics",
			setup: func(_ context.Context, api extension.API) error {
				api.Post(boom)
				return nil
			},
			route: "Scope.runPosted.func1.ExtensionPostedPanicked", tag: "ExtensionPostedPanicked", where: "posted function",
		},
		{
			name: "a settings listener panics",
			setup: func(_ context.Context, api extension.API) error {
				if err := api.Settings().Register(extension.SettingSpec{Key: "x.value", Type: "number", Default: 1}); err != nil {
					return err
				}
				api.Settings().OnChange(boom)
				return nil
			},
			trigger: func(tb *tab) { tb.changes++; tb.changeSetting("x.value", 1+tb.changes) },
			route:   "Scope.runSettingsListener.func1.ExtensionSettingsCallbackPanicked", tag: "ExtensionSettingsCallbackPanicked", where: "settings listener",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := manifest("x")
			m.Name = "Xtension"
			m.Contributes.Commands = []extension.CommandSpec{{ID: "x.declared", Title: "Declared"}}
			bad, _ := factory(m, func(f *fake) { f.activate = tt.setup })
			healthy, _ := factory(manifest("emetos.healthy"), func(f *fake) {
				f.activate = func(_ context.Context, api extension.API) error {
					return api.Commands().Register(extension.CommandSpec{ID: "healthy.cmd", Title: "H"}, func(context.Context, any) error { return nil })
				}
			})
			r := newRig(t, bad, healthy)
			tb, other := r.newTab("failing"), r.newTab("other")
			tb.scope.Start()
			other.scope.Start()
			if tt.trigger != nil {
				// the extension is active in both tabs until the failure happens in the first one
				if tb.scope.State("x") != StateActive {
					t.Fatalf("the extension is %v before the trigger: %v", tb.scope.State("x"), tb.scope.Failure("x"))
				}
				tt.trigger(tb)
			}
			if got := tb.scope.State("x"); got != StateFailed {
				t.Fatalf("state = %v, want failed", got)
			}
			fl := tb.scope.Failure("x")
			if fl == nil {
				t.Fatal("no failure recorded")
			}
			appErr, ok := errors.FromError(fl.Err)
			if !ok {
				t.Fatalf("expected *errors.Error, got %T", fl.Err)
			}
			if appErr.Tag != tt.tag || !appErr.HasRoute(tt.route) {
				t.Errorf("tag %s, route %s; want tag %s and route %s", appErr.Tag, appErr.Route(), tt.tag, tt.route)
			}
			if !strings.HasPrefix(fl.Where, strings.SplitN(tt.where, " ", 2)[0]) {
				t.Errorf("where = %q, want %q", fl.Where, tt.where)
			}
			if fl.BoxText != "Xtension failed" {
				t.Errorf("box text = %q", fl.BoxText)
			}
			// one notification, with Show Log
			items := tb.notify.Items()
			var failures []string
			for _, it := range items {
				if strings.HasPrefix(it.Message, "Xtension failed") {
					failures = append(failures, it.Message)
					if len(it.Actions) != 1 || it.Actions[0].Label != "Show Log" || it.Actions[0].Command != "workbench.panel.output.focus" {
						t.Errorf("the notification has the actions %+v", it.Actions)
					}
				}
			}
			if len(failures) != 1 {
				t.Errorf("notifications = %v, want one failure", tb.notifications())
			}
			// and the log has it with the route
			if !strings.Contains(tb.log.String(), "an extension failed") || !strings.Contains(tb.log.String(), tt.tag) {
				t.Errorf("the log does not have the failure:\n%s", tb.log.String())
			}
			// everything it contributed is gone
			if _, ok := tb.commands.Lookup("x.declared"); ok {
				t.Error("the declared command is still there")
			}
			if _, ok := tb.commands.Lookup("x.dynamic"); ok {
				t.Error("the dynamic command is still there")
			}
			if got := tb.statusIDs(); len(got) != 0 {
				t.Errorf("status items left: %v", got)
			}
			if _, ok := r.registry.Lookup("x.value"); ok && other.scope.State("x") != StateActive {
				t.Error("the setting is still registered")
			}
			// the other tab and the other extension are untouched
			if got := other.scope.State("x"); got != StateActive && tt.trigger != nil {
				t.Errorf("the other tab has the extension %v", got)
			}
			if got := tb.scope.State("emetos.healthy"); got != StateActive {
				t.Errorf("the healthy extension is %v", got)
			}
			for _, m := range other.notifications() {
				if strings.Contains(m, "failed") && tt.trigger != nil {
					t.Errorf("the other tab was told of the failure: %v", other.notifications())
				}
			}
			// a second panic of the same activation does not notify again
			if tt.trigger != nil {
				tt.trigger(tb)
			}
			n := 0
			for _, m := range tb.notifications() {
				if strings.HasPrefix(m, "Xtension failed") {
					n++
				}
			}
			if n != 1 {
				t.Errorf("%d failure notifications after the trigger ran again, want 1", n)
			}
		})
	}
}

func TestAFactoryThatPanicsAtActivationFailsTheExtensionInThatScope(t *testing.T) {
	calls := 0
	m := manifest("x")
	f := func() extension.Extension {
		calls++
		if calls > 1 {
			panic("second time")
		}
		return &fake{manifest: m}
	}
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.Start()
	fl := tb.scope.Failure("x")
	if tb.scope.State("x") != StateFailed || fl == nil {
		t.Fatalf("state %v", tb.scope.State("x"))
	}
	appErr, _ := errors.FromError(fl.Err)
	if appErr == nil || !appErr.HasRoute("Scope.instantiate.func1.ExtensionFactoryPanicked") {
		t.Errorf("failure = %+v", fl.Err)
	}
}

func TestAFactoryThatMakesNothingAtActivationFailsTheExtension(t *testing.T) {
	calls := 0
	m := manifest("x")
	f := func() extension.Extension {
		calls++
		if calls > 1 {
			return nil
		}
		return &fake{manifest: m}
	}
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.Start()
	appErr, ok := errors.FromError(tb.scope.Failure("x").Err)
	if !ok || !appErr.HasRoute("Scope.instantiate.ExtensionFactoryNil") {
		t.Errorf("failure = %+v", tb.scope.Failure("x"))
	}
}

func TestDisablingAndEnablingFollowsTheSetting(t *testing.T) {
	m := manifest("emetos.sw")
	m.Contributes.Commands = []extension.CommandSpec{{ID: "sw.cmd", Title: "Sw"}}
	m.Contributes.Menus = []extension.MenuItem{{Menu: extension.MenuView, Command: "sw.cmd"}}
	f, rec := factory(m, func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error {
			return api.Commands().Handle("sw.cmd", func(context.Context, any) error { return nil })
		}
	})
	r := newRig(t, f)
	off := r.newTab("off", "emetos.sw")
	on := r.newTab("on")
	off.scope.Start()
	on.scope.Start()
	if off.scope.State("emetos.sw") != StateDisabled || len(rec.all()) != 1 {
		t.Fatalf("a disabled extension is %v and %d instances exist", off.scope.State("emetos.sw"), len(rec.all()))
	}
	if _, ok := off.commands.Lookup("sw.cmd"); ok {
		t.Error("the command of a disabled extension is in the palette")
	}
	if slices.Contains(off.menuCommands(extension.MenuView), "sw.cmd") {
		t.Error("the menu item of a disabled extension is in the menu")
	}

	// turn it off where it runs
	on.disabled = []string{"emetos.sw"}
	on.scope.Refresh()
	if on.scope.State("emetos.sw") != StateDisabled || rec.all()[0].ctx.Err() == nil {
		t.Errorf("state %v, context error %v", on.scope.State("emetos.sw"), rec.all()[0].ctx.Err())
	}
	if _, ok := on.commands.Lookup("sw.cmd"); ok {
		t.Error("the command stayed after the extension was turned off")
	}

	// turn it on where it was off: onStartup has happened, so it activates
	off.disabled = nil
	off.scope.Refresh()
	if off.scope.State("emetos.sw") != StateActive || len(rec.all()) != 2 {
		t.Errorf("turned on again: %v, %d instances", off.scope.State("emetos.sw"), len(rec.all()))
	}
	if _, ok := off.commands.Lookup("sw.cmd"); !ok {
		t.Error("the command is missing after the extension was turned on")
	}
}

func TestTurningAFailedExtensionOffAndOnTriesAgain(t *testing.T) {
	calls := 0
	f, rec := factory(manifest("x"), func(f *fake) {
		calls++
		first := calls == 1
		f.activate = func(context.Context, extension.API) error {
			if first {
				panic("first time")
			}
			return nil
		}
	})
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.Start()
	if tb.scope.State("x") != StateFailed {
		t.Fatalf("state %v", tb.scope.State("x"))
	}
	tb.scope.Refresh() // nothing changed: it stays failed
	if tb.scope.State("x") != StateFailed || len(rec.all()) != 1 {
		t.Errorf("a refresh with no change: %v, %d instances", tb.scope.State("x"), len(rec.all()))
	}
	tb.disabled = []string{"x"}
	tb.scope.Refresh()
	tb.disabled = nil
	tb.scope.Refresh()
	if tb.scope.State("x") != StateActive || tb.scope.Failure("x") != nil {
		t.Errorf("after off and on: %v, %v", tb.scope.State("x"), tb.scope.Failure("x"))
	}
}

func TestTurningOnAnExtensionActivatesItForEventsThatHappened(t *testing.T) {
	f, rec := factory(manifest("emetos.late", extension.OnLanguage("go")), nil)
	r := newRig(t, f)
	tb := r.newTab("one", "emetos.late")
	tb.scope.Start()
	tb.scope.Fire(extension.OnLanguage("go"))
	if len(rec.all()) != 0 {
		t.Fatal("a disabled extension was activated")
	}
	tb.disabled = nil
	tb.scope.Refresh()
	if tb.scope.State("emetos.late") != StateActive {
		t.Errorf("state %v, want active: a Go document was open when it was turned on", tb.scope.State("emetos.late"))
	}
}

func TestTheAPIFailsAfterTheExtensionIsDeactivated(t *testing.T) {
	var saved extension.API
	f, _ := factory(manifest("emetos.gone"), func(f *fake) {
		f.activate = func(_ context.Context, api extension.API) error { saved = api; return nil }
	})
	r := newRig(t, f)
	tb := r.newTab("one")
	tb.scope.Start()
	tb.scope.Close()
	ok := func(context.Context, any) error { return nil }
	tests := []struct {
		name string
		call func() error
	}{
		{"Commands.Handle", func() error { return saved.Commands().Handle("x", ok) }},
		{"Commands.Register", func() error { return saved.Commands().Register(extension.CommandSpec{ID: "x.y"}, ok) }},
		{"Keybindings.Add", func() error { return saved.Keybindings().Add(extension.Keybinding{Command: "x", Key: "ctrl+k"}) }},
		{"Menus.Add", func() error { return saved.Menus().Add(extension.MenuItem{Menu: extension.MenuView, Command: "x"}) }},
		{"Settings.Register", func() error {
			return saved.Settings().Register(extension.SettingSpec{Key: "emetos.gone.k", Type: "string"})
		}},
		{"StatusBar.Add", func() error { _, err := saved.StatusBar().Add(extension.StatusItem{ID: "a"}); return err }},
		{"Documents.Read", func() error { _, err := saved.Documents().Read(context.Background(), "file:///a"); return err }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appErr, ok := errors.FromError(tt.call())
			if !ok || !appErr.HasRoute("ExtensionDeactivated") {
				t.Errorf("error = %+v", appErr)
			}
		})
	}
	// a notification of a deactivated extension is dropped
	saved.Notifications().Info("late")
	if len(tb.notifications()) != 0 {
		t.Errorf("a deactivated extension showed %v", tb.notifications())
	}
	if saved.Documents().Open() != nil {
		t.Errorf("a deactivated extension lists documents")
	}
}

func TestRowsListTheManifestsAndTheStateInEveryScope(t *testing.T) {
	good, _ := factory(manifest("emetos.good"), nil)
	bad, _ := factory(manifest("emetos.bad"), func(f *fake) {
		f.activate = func(context.Context, extension.API) error { return errors.New("Cause", "no") }
	})
	idle, _ := factory(manifest("emetos.idle", extension.OnLanguage("rust")), nil)
	r := newRig(t, good, bad, idle)
	a, b := r.newTab("alpha"), r.newTab("beta", "emetos.good")
	a.scope.Start()
	b.scope.Start()
	rows := r.host.Rows([]string{"emetos.good"})
	if len(rows) != 3 {
		t.Fatalf("%d rows", len(rows))
	}
	type cell struct {
		id       string
		disabled bool
		states   []string
	}
	var got []cell
	for _, row := range rows {
		c := cell{id: row.Manifest.ID, disabled: row.Disabled}
		for _, s := range row.Scopes {
			c.states = append(c.states, s.Scope+"="+s.State.String())
		}
		got = append(got, c)
	}
	want := []cell{
		{"emetos.good", true, []string{"alpha=active", "beta=disabled"}},
		{"emetos.bad", false, []string{"alpha=failed", "beta=failed"}},
		{"emetos.idle", false, []string{"alpha=inactive", "beta=inactive"}},
	}
	for i := range want {
		if got[i].id != want[i].id || got[i].disabled != want[i].disabled || !slices.Equal(got[i].states, want[i].states) {
			t.Errorf("row %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if rows[1].Scopes[0].Failure == nil || rows[1].Scopes[0].Failure.BoxText != "Ext emetos.bad failed" {
		t.Errorf("failure of the failed row: %+v", rows[1].Scopes[0].Failure)
	}
}

func TestShutdownClosesEveryScope(t *testing.T) {
	f, rec := factory(manifest("emetos.sd"), nil)
	r := newRig(t, f)
	r.newTab("a").scope.Start()
	r.newTab("b").scope.Start()
	r.host.Shutdown()
	for i, e := range rec.all() {
		if e.ctx.Err() == nil {
			t.Errorf("instance %d: the context was not cancelled", i)
		}
	}
	if n := len(r.host.Scopes()); n != 0 {
		t.Errorf("%d scopes left", n)
	}
}

func TestMenuItemsAndKeysThatDoNotParseAreLeftOutAndTheRestStays(t *testing.T) {
	m := manifest("emetos.bad")
	m.Contributes = extension.Contributes{
		Commands:    []extension.CommandSpec{{ID: "bad.when", Title: "When", When: "((("}, {ID: "bad.ok", Title: "Ok"}, {ID: "workbench.action.files.save", Title: "Dup"}},
		Menus:       []extension.MenuItem{{Menu: extension.MenuView, Command: "bad.ok", When: "((("}, {Menu: extension.MenuView, Command: "bad.ok", Title: "Fine"}},
		Keybindings: []extension.Keybinding{{Command: "bad.ok", Key: ""}, {Command: "bad.ok", Key: "ctrl+alt+o"}},
	}
	f, _ := factory(m, nil)
	r := newRig(t, f)
	tb := r.newTab("one")
	if _, ok := tb.commands.Lookup("bad.when"); ok {
		t.Error("a command with a when-clause that does not parse was registered")
	}
	if meta, ok := tb.commands.Lookup("workbench.action.files.save"); !ok || meta.Title == "Dup" {
		t.Errorf("the command of the editor was replaced: %+v", meta)
	}
	if _, ok := tb.commands.Lookup("bad.ok"); !ok {
		t.Error("the good command is missing")
	}
	if got := tb.menuCommands(extension.MenuView); strings.Count(strings.Join(got, ","), "bad.ok") != 1 {
		t.Errorf("view menu = %v, want one bad.ok", got)
	}
	if got := tb.keys.commands(); len(got) != 1 {
		t.Errorf("keybindings = %v, want one", got)
	}
	for _, want := range []string{"WhenClauseInvalid", "CommandDuplicate", "ExtensionKeybindingInvalid"} {
		if !strings.Contains(tb.log.String(), want) {
			t.Errorf("the log does not mention %s:\n%s", want, tb.log.String())
		}
	}
}
