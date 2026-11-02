package fakeapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
)

// The Debug API of the fake (step 25.3) has the rules of the host: the same errors, the registrations ending with the activation, the
// providers called in the order they were registered.

func newDebugFake(t *testing.T) *API {
	t.Helper()
	return New(t, manifest(), extension.ScopeInfo{Folder: t.TempDir()})
}

func routeOf(t *testing.T, err error) string {
	t.Helper()
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T (%v)", err, err)
	}
	return appErr.Tag
}

func TestFakeDebugRegistrationsFailLikeTheHost(t *testing.T) {
	factory := func(context.Context, extension.DebugSession, *extension.DebugAdapter) (*extension.DebugAdapter, error) {
		return nil, nil
	}
	provider := extension.DebugConfigurationProvider{Resolve: func(context.Context, string, extension.DebugConfiguration) (extension.DebugConfiguration, error) {
		return nil, nil
	}}
	tracker := func(context.Context, extension.DebugSession) (*extension.DebugAdapterTracker, error) { return nil, nil }
	tests := []struct {
		name string
		do   func(a *API) error
		prep func(a *API)
		tag  string
	}{
		{"a factory with no type", func(a *API) error { _, err := a.Debug().RegisterAdapterDescriptorFactory("", factory); return err }, nil, "ExtensionDebugInvalid"},
		{"no factory function", func(a *API) error { _, err := a.Debug().RegisterAdapterDescriptorFactory("go", nil); return err }, nil, "ExtensionDebugInvalid"},
		{"a provider with no function", func(a *API) error {
			_, err := a.Debug().RegisterConfigurationProvider("go", extension.DebugConfigurationProvider{}, extension.DebugTriggerInitial)
			return err
		}, nil, "ExtensionDebugInvalid"},
		{"no tracker function", func(a *API) error { _, err := a.Debug().RegisterAdapterTrackerFactory("go", nil); return err }, nil, "ExtensionDebugInvalid"},
		{"a second factory", func(a *API) error { _, err := a.Debug().RegisterAdapterDescriptorFactory("go", factory); return err },
			func(a *API) {
				if _, err := a.Debug().RegisterAdapterDescriptorFactory("go", factory); err != nil {
					panic(err)
				}
			}, "ExtensionDebugFactoryTaken"},
		{"no service", func(a *API) error {
			_, err := a.Debug().RegisterConfigurationProvider("go", provider, extension.DebugTriggerDynamic)
			return err
		}, func(a *API) { a.SetDebugUnavailable(true) }, "ExtensionDebugUnavailable"},
		{"a deactivated extension", func(a *API) error { _, err := a.Debug().RegisterAdapterTrackerFactory("go", tracker); return err },
			func(a *API) { a.Deactivate() }, "ExtensionDeactivated"},
		{"a start with nothing to start", func(a *API) error {
			_, err := a.Debug().StartDebugging(context.Background(), "", extension.DebugTarget{}, extension.DebugStartOptions{})
			return err
		}, nil, "ExtensionDebugInvalid"},
		{"a breakpoint outside a file", func(a *API) error {
			return a.Debug().AddBreakpoints(context.Background(), []extension.DebugBreakpoint{{URI: "untitled:1"}})
		}, nil, "ExtensionDebugBreakpointInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newDebugFake(t)
			if tt.prep != nil {
				tt.prep(a)
			}
			if got := routeOf(t, tt.do(a)); got != tt.tag {
				t.Errorf("tag = %s, want %s", got, tt.tag)
			}
		})
	}
}

func TestFakeDebugProvidersRunInTheOrderTheyRegistered(t *testing.T) {
	a := newDebugFake(t)
	var order []string
	add := func(typ, tag string, trigger extension.DebugProviderTrigger) func() {
		d, err := a.Debug().RegisterConfigurationProvider(typ, extension.DebugConfigurationProvider{
			Provide: func(context.Context, string) ([]extension.DebugConfiguration, error) {
				return []extension.DebugConfiguration{{"name": tag}}, nil
			},
			Resolve: func(_ context.Context, _ string, c extension.DebugConfiguration) (extension.DebugConfiguration, error) {
				order = append(order, "resolve "+tag)
				c[tag] = true
				return c, nil
			},
			ResolveSubstituted: func(_ context.Context, _ string, c extension.DebugConfiguration) (extension.DebugConfiguration, error) {
				order = append(order, "substituted "+tag)
				return c, nil
			},
		}, trigger)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	add("go", "one", extension.DebugTriggerInitial)
	removeStar := add("*", "star", extension.DebugTriggerInitial)
	add("go", "two", extension.DebugTriggerDynamic)
	ctx := context.Background()
	got, err := a.ResolveDebugConfiguration(ctx, "go", "/w", extension.DebugConfiguration{"name": "x"})
	if err != nil || got["one"] != true || got["two"] != true || got["star"] != nil {
		t.Fatalf("resolve = %v, %v", got, err)
	}
	if _, err := a.ResolveDebugConfigurationSubstituted(ctx, "go", "/w", got); err != nil {
		t.Fatal(err)
	}
	if want := "resolve one, resolve two, substituted one, substituted two, substituted star"; strings.Join(order, ", ") != want {
		t.Errorf("order = %s\nwant    %s", strings.Join(order, ", "), want)
	}
	initial, _ := a.ProvideDebugConfigurations(ctx, "go", "/w", extension.DebugTriggerInitial)
	dynamic, _ := a.ProvideDebugConfigurations(ctx, "go", "/w", extension.DebugTriggerDynamic)
	if len(initial) != 1 || initial[0]["name"] != "one" || len(dynamic) != 1 || dynamic[0]["name"] != "two" {
		t.Errorf("initial %v dynamic %v", initial, dynamic)
	}
	removeStar()
	order = nil
	if _, err := a.ResolveDebugConfigurationSubstituted(ctx, "go", "/w", got); err != nil || strings.Contains(strings.Join(order, ","), "star") {
		t.Errorf("a removed provider ran: %v %v", order, err)
	}
}

func TestFakeDebugAProviderThatGivesNothingCancelsAndAPanicTakesTheExtensionOff(t *testing.T) {
	a := newDebugFake(t)
	ran := false
	cancel := func(context.Context, string, extension.DebugConfiguration) (extension.DebugConfiguration, error) {
		return nil, nil
	}
	if _, err := a.Debug().RegisterConfigurationProvider("go", extension.DebugConfigurationProvider{Resolve: cancel}, extension.DebugTriggerInitial); err != nil {
		t.Fatal(err)
	}
	if _, err := a.Debug().RegisterConfigurationProvider("go", extension.DebugConfigurationProvider{Resolve: func(_ context.Context, _ string, c extension.DebugConfiguration) (extension.DebugConfiguration, error) {
		ran = true
		return c, nil
	}}, extension.DebugTriggerInitial); err != nil {
		t.Fatal(err)
	}
	got, err := a.ResolveDebugConfiguration(context.Background(), "go", "", extension.DebugConfiguration{"name": "x"})
	if got != nil || err != nil || ran {
		t.Errorf("resolve = %v, %v (ran %v)", got, err, ran)
	}
	b := newDebugFake(t)
	if _, err := b.Debug().RegisterConfigurationProvider("go", extension.DebugConfigurationProvider{Resolve: func(context.Context, string, extension.DebugConfiguration) (extension.DebugConfiguration, error) {
		panic("boom")
	}}, extension.DebugTriggerInitial); err != nil {
		t.Fatal(err)
	}
	_, err = b.ResolveDebugConfiguration(context.Background(), "go", "", extension.DebugConfiguration{})
	if routeOf(t, err) != "ExtensionDebugPanicked" || !b.Failed() {
		t.Errorf("err = %v, failed %v", err, b.Failed())
	}
}

func TestFakeDebugAdapterFactoriesAndTrackers(t *testing.T) {
	a := newDebugFake(t)
	def := &extension.DebugAdapter{Kind: extension.DebugAdapterExecutable, Command: "dlv"}
	s := a.NewDebugSession("1", "go", "Go", extension.DebugConfiguration{"name": "Go"})
	// no factory: the default
	if got, err := a.DebugAdapter(context.Background(), "go", s, def); err != nil || got != def {
		t.Fatalf("DebugAdapter = %v, %v", got, err)
	}
	dispose, err := a.Debug().RegisterAdapterDescriptorFactory("go", func(_ context.Context, ss extension.DebugSession, exe *extension.DebugAdapter) (*extension.DebugAdapter, error) {
		if exe != def || ss.Name() != "Go" {
			t.Errorf("the factory got %v and %v", exe, ss)
		}
		return &extension.DebugAdapter{Kind: extension.DebugAdapterServer, Port: 4711}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := a.DebugAdapter(context.Background(), "go", s, def); err != nil || got.Port != 4711 {
		t.Fatalf("DebugAdapter = %v, %v", got, err)
	}
	if types := a.DebugFactoryTypes(); len(types) != 1 || types[0] != "go" {
		t.Errorf("factory types = %v", types)
	}
	dispose()
	if len(a.DebugFactoryTypes()) != 0 {
		t.Error("the factory remains after dispose")
	}
	for _, typ := range []string{"go", "*", "other"} {
		typ := typ
		if _, err := a.Debug().RegisterAdapterTrackerFactory(typ, func(context.Context, extension.DebugSession) (*extension.DebugAdapterTracker, error) {
			return &extension.DebugAdapterTracker{}, nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if got := a.DebugTrackers(context.Background(), "go", s); len(got) != 2 {
		t.Errorf("%d trackers, want the ones of go and *", len(got))
	}
	// a panic in a factory
	b := newDebugFake(t)
	if _, err := b.Debug().RegisterAdapterDescriptorFactory("go", func(context.Context, extension.DebugSession, *extension.DebugAdapter) (*extension.DebugAdapter, error) {
		panic("boom")
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.DebugAdapter(context.Background(), "go", s, nil); routeOf(t, err) != "ExtensionDebugPanicked" || !b.Failed() {
		t.Errorf("panic: err %v failed %v", err, b.Failed())
	}
}

func TestFakeDebugSessionsListenersAndBreakpoints(t *testing.T) {
	a := newDebugFake(t)
	var log []string
	api := a.Debug()
	api.OnDidStartSession(func(s extension.DebugSession) { log = append(log, "start "+s.Name()) })
	api.OnDidTerminateSession(func(s extension.DebugSession) { log = append(log, "end "+s.Name()) })
	api.OnDidChangeActiveSession(func(s extension.DebugSession) {
		if s == nil {
			log = append(log, "active none")
		} else {
			log = append(log, "active "+s.Name())
		}
	})
	api.OnDidReceiveCustomEvent(func(e extension.DebugSessionCustomEvent) { log = append(log, "custom "+e.Event+" "+string(e.Body)) })
	var changes []extension.DebugBreakpointsChange
	api.OnDidChangeBreakpoints(func(c extension.DebugBreakpointsChange) { changes = append(changes, c) })

	s := a.NewDebugSession("1", "go", "Go", extension.DebugConfiguration{"name": "Go"})
	a.StartDebugSession(s)
	a.SetActiveDebugSession(s)
	a.DebugCustomEvent(s, "x", json.RawMessage(`1`))
	if len(api.Sessions()) != 1 || api.ActiveSession() == nil {
		t.Errorf("sessions %v active %v", api.Sessions(), api.ActiveSession())
	}
	child := a.NewDebugSession("2", "go", "Child", nil)
	child.SetParent(s)
	child.SetFolder("/w")
	if child.Parent() != extension.DebugSession(s) || child.Folder() != "/w" || child.Type() != "go" || child.ID() != "2" {
		t.Error("the session does not report what it was made with")
	}
	s.OnCustomRequest(func(command string, _ any) (json.RawMessage, error) { return json.RawMessage(`{"n":1}`), nil })
	if body, err := s.CustomRequest(context.Background(), "stats", nil); err != nil || string(body) != `{"n":1}` || len(s.CustomRequests()) != 1 {
		t.Errorf("CustomRequest = %s, %v", body, err)
	}
	if err := api.StopDebugging(context.Background(), s); err != nil {
		t.Fatal(err)
	}
	if want := "start Go, active Go, custom x 1, end Go, active none"; strings.Join(log, ", ") != want {
		t.Errorf("log = %s\nwant  %s", strings.Join(log, ", "), want)
	}

	ctx := context.Background()
	if err := api.AddBreakpoints(ctx, []extension.DebugBreakpoint{{URI: "file:///w/a.go", Position: extension.Position{Line: 3}}, {Function: "main.run"}}); err != nil {
		t.Fatal(err)
	}
	bps := api.Breakpoints()
	if len(bps) != 2 || bps[0].ID == "" || len(changes) != 1 || len(changes[0].Added) != 2 {
		t.Fatalf("breakpoints %+v changes %+v", bps, changes)
	}
	if err := api.RemoveBreakpoints(ctx, []extension.DebugBreakpoint{{URI: "file:///w/a.go", Position: extension.Position{Line: 3}}, {Function: "main.run"}}); err != nil {
		t.Fatal(err)
	}
	if len(api.Breakpoints()) != 0 || len(changes) != 2 || len(changes[1].Removed) != 2 {
		t.Errorf("after removing: %+v %+v", api.Breakpoints(), changes)
	}

	api.Console().Append("a")
	api.Console().AppendLine("b")
	if a.DebugConsoleText() != "ab\n" {
		t.Errorf("console = %q", a.DebugConsoleText())
	}

	// what the activation's end takes away
	removeLate := api.OnDidStartSession(func(extension.DebugSession) { log = append(log, "late") })
	_ = removeLate
	a.Deactivate()
	log = nil
	a.StartDebugSession(a.NewDebugSession("3", "go", "After", nil))
	if len(log) != 0 {
		t.Errorf("a listener of an ended activation was called: %v", log)
	}
	if routeOf(t, api.StopDebugging(ctx, nil)) != "ExtensionDeactivated" {
		t.Error("StopDebugging after the end did not fail")
	}
}

func TestFakeDebugStartDebuggingIsRecordedAndAnswered(t *testing.T) {
	a := newDebugFake(t)
	ctx := context.Background()
	ok, err := a.Debug().StartDebugging(ctx, "/w", extension.DebugTarget{Name: "Go"}, extension.DebugStartOptions{NoDebug: true})
	if !ok || err != nil {
		t.Fatalf("StartDebugging = %v, %v", ok, err)
	}
	a.OnDebugStart(func(DebugStart) (bool, error) { return false, nil })
	if ok, _ := a.Debug().StartDebugging(ctx, "", extension.DebugTarget{Config: extension.DebugConfiguration{"name": "x"}}, extension.DebugStartOptions{}); ok {
		t.Error("the answer of OnDebugStart was not used")
	}
	starts := a.DebugStarts()
	if len(starts) != 2 || starts[0].Folder != "/w" || starts[0].Target.Name != "Go" || !starts[0].Options.NoDebug || starts[1].Target.Config["name"] != "x" {
		t.Errorf("starts = %+v", starts)
	}
	a.SetDebugUnavailable(true)
	_, err = a.Debug().StartDebugging(ctx, "", extension.DebugTarget{Name: "x"}, extension.DebugStartOptions{})
	if routeOf(t, err) != "ExtensionDebugUnavailable" {
		t.Errorf("err = %v", err)
	}
}
