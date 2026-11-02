package exthost

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/dap"
	"github.com/rm4n0s/emetos/internal/core/dap/daptest"
	"github.com/rm4n0s/emetos/internal/core/debug"
)

// The debug host (step 25.3, SPEC §10.31 "The extension contract"): the debugger of an extension is activated by the first configuration
// of its type, its adapter comes from its factory (an executable, a server, a Unix socket, or a program that prints its port), the
// configuration providers run in VS Code's order, the trackers see every message, and the sessions end with the extension.

func TestAnAdapterIsReachedInEveryWayADescriptorCanSayIt(t *testing.T) {
	tests := []struct {
		name      string
		transport string
		kind      extension.DebugAdapterKind
	}{
		{"an executable that speaks DAP on its standard streams", "exec", extension.DebugAdapterExecutable},
		{"an executable that prints the port it listens on", "execserver", extension.DebugAdapterExecutableServer},
		{"a server that listens on a port already", "server", extension.DebugAdapterServer},
		{"a server on a Unix socket", "pipe", extension.DebugAdapterPipeServer},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDebugRig(t, dbgOpts{})
			d.script("s", sessionScript())
			if st := d.scope.State("dbg"); st != StateInactive {
				t.Fatalf("the debugger's extension is %v before the first session: it waits for its debug type", st)
			}
			s := d.start(fakeConfig("s", tt.transport))
			if st := d.scope.State("dbg"); st != StateActive {
				t.Errorf("the debugger's extension is %v after the first session", st)
			}
			d.idle(s)
			d.mu.Lock()
			if len(d.adapters) != 1 || d.adapters[0].Kind != tt.kind {
				t.Errorf("adapters = %+v, want one of kind %d", d.adapters, tt.kind)
			}
			d.mu.Unlock()
			if s.Type() != "fake" || s.Name() != "s" || s.Configuration()["transport"] != tt.transport || s.Parent() != nil {
				t.Errorf("session = %s %s %v parent %v", s.Type(), s.Name(), s.Configuration(), s.Parent())
			}
			if got := d.starter.Debug().ActiveSession(); got == nil || got.ID() != s.ID() {
				t.Errorf("active session = %v, want %s", got, s.ID())
			}
			d.stop()
			if got := d.starter.Debug().ActiveSession(); got != nil {
				t.Errorf("active session after the end = %v", got)
			}
			if srv := d.server("s"); srv != nil {
				if v := srv.Violations(); len(v) > 0 {
					t.Errorf("the fake adapter noticed %v", v)
				}
			}
		})
	}
}

func TestTheManifestsProgramIsTheAdapterWhenTheFactoryGivesNone(t *testing.T) {
	script := scriptFileFor(t, sessionScript())
	tests := []struct {
		name string
		opts func() dbgOpts
	}{
		{"a factory that gives nil", func() dbgOpts {
			return dbgOpts{manifest: func(m *extension.Manifest) {
				m.Contributes.Debuggers[0].Program, m.Contributes.Debuggers[0].Args = daptest.Program(t), []string{"-script", script}
			}}
		}},
		{"no factory", func() dbgOpts {
			return dbgOpts{noFactory: true, manifest: func(m *extension.Manifest) {
				m.Contributes.Debuggers[0].Program, m.Contributes.Debuggers[0].Args = daptest.Program(t), []string{"-script", script}
			}}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDebugRig(t, tt.opts())
			s := d.start(fakeConfig("s", "default"))
			d.idle(s)
			d.mu.Lock()
			def := d.executable
			d.mu.Unlock()
			if tt.name == "a factory that gives nil" && (def == nil || def.Kind != extension.DebugAdapterExecutable || def.Command != daptest.Program(t) || len(def.Args) != 2) {
				t.Errorf("the factory got %+v as the manifest's adapter", def)
			}
			d.stop()
		})
	}
}

func TestARuntimeRunsTheProgramOfTheManifest(t *testing.T) {
	spec := extension.DebuggerSpec{Type: "x", Runtime: "node", RuntimeArgs: []string{"--inspect"}, Program: "/a/js-debug.js", Args: []string{"0"}}
	a := defaultAdapter(spec, "/w")
	if a.Kind != extension.DebugAdapterExecutable || a.Command != "node" || strings.Join(a.Args, " ") != "--inspect /a/js-debug.js 0" || a.Cwd != "/w" {
		t.Errorf("adapter = %+v", a)
	}
}

func TestADebuggerWithNoAdapterFailsTheSession(t *testing.T) {
	d := newDebugRig(t, dbgOpts{noFactory: true})
	ok, err := d.startErr(fakeConfig("s", "exec"))
	if ok {
		t.Error("a session started with no adapter")
	}
	appErr, isErr := errors.FromError(err)
	if !isErr || !appErr.HasRoute("DebugHost.descriptor.DebugAdapterMissing") {
		t.Fatalf("err = %v, want the route DebugHost.descriptor.DebugAdapterMissing", err)
	}
	if len(d.starter.Debug().Sessions()) != 0 {
		t.Error("the failed session is listed")
	}
}

func TestAnInvalidDescriptorFailsTheSession(t *testing.T) {
	tests := []struct {
		name string
		a    extension.DebugAdapter
	}{
		{"no kind", extension.DebugAdapter{Command: "x"}},
		{"a program with no command", extension.DebugAdapter{Kind: extension.DebugAdapterExecutable}},
		{"a program that prints its port, with no command", extension.DebugAdapter{Kind: extension.DebugAdapterExecutableServer}},
		{"a server with no port", extension.DebugAdapter{Kind: extension.DebugAdapterServer}},
		{"a server with a port that is too big", extension.DebugAdapter{Kind: extension.DebugAdapterServer, Port: 70000}},
		{"a pipe with no path", extension.DebugAdapter{Kind: extension.DebugAdapterPipeServer}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDebugRig(t, dbgOpts{})
			d.override = func(extension.DebugSession) (*extension.DebugAdapter, error) { a := tt.a; return &a, nil }
			ok, err := d.startErr(fakeConfig("s", "exec"))
			appErr, isErr := errors.FromError(err)
			if ok || !isErr || !appErr.HasRoute("DebugHost.adapterSpec.ExtensionDebugAdapterInvalid") {
				t.Fatalf("StartDebugging = %v, %v", ok, err)
			}
		})
	}
}

func TestAFactoryThatFailsFailsTheSessionWithItsError(t *testing.T) {
	d := newDebugRig(t, dbgOpts{})
	d.override = func(extension.DebugSession) (*extension.DebugAdapter, error) {
		return nil, errors.New("TestNoAdapterHere", "there is no adapter")
	}
	ok, err := d.startErr(fakeConfig("s", "exec"))
	appErr, isErr := errors.FromError(err)
	if ok || !isErr || appErr.Tag != "TestNoAdapterHere" {
		t.Fatalf("StartDebugging = %v, %v", ok, err)
	}
}

func TestConnectNeedsTheSessionInItsContext(t *testing.T) {
	d := newDebugRig(t, dbgOpts{})
	dbg, ok := d.debug.debugger("fake")
	if !ok {
		t.Fatal("the debug type is not there")
	}
	_, err := dbg.Connect(context.Background(), debug.Config{"name": "x", "type": "fake"}, dap.ClientOptions{})
	appErr, isErr := errors.FromError(err)
	if !isErr || !appErr.HasRoute("DebugHost.reach.DebugConnectNoSession") {
		t.Fatalf("err = %v", err)
	}
}

func TestAPanicInAFactoryTakesTheExtensionOff(t *testing.T) {
	d := newDebugRig(t, dbgOpts{})
	d.override = func(extension.DebugSession) (*extension.DebugAdapter, error) { panic("boom") }
	ok, err := d.startErr(fakeConfig("s", "exec"))
	// the failure ends the session of the type as well: whether the error or the stop reaches StartDebugging first is not fixed
	if appErr, isErr := errors.FromError(err); ok || (err != nil && (!isErr || !appErr.HasRoute("DebugHost.guard.func1.ExtensionDebugPanicked"))) {
		t.Fatalf("StartDebugging = %v, %v", ok, err)
	}
	if st := d.scope.State("dbg"); st != StateFailed {
		t.Errorf("the extension is %v, want failed", st)
	}
	if f := d.scope.Failure("dbg"); f == nil || !strings.Contains(f.Where, "the adapter factory of fake") {
		t.Errorf("failure = %+v", f)
	}
	if got := d.debug.Debuggers(); len(got) != 0 {
		t.Errorf("the debug types of a failed extension remain: %+v", got)
	}
}

func TestTurningTheExtensionOffEndsItsSessions(t *testing.T) {
	d := newDebugRig(t, dbgOpts{})
	d.script("a", sessionScript())
	d.script("b", sessionScript())
	ended := &strRecorder{}
	d.starter.Debug().OnDidTerminateSession(func(s extension.DebugSession) { ended.add(s.Name()) })
	d.start(fakeConfig("a", "exec"))
	d.start(fakeConfig("b", "server"))
	if n := len(d.starter.Debug().Sessions()); n != 2 {
		t.Fatalf("%d sessions", n)
	}
	d.disabled = []string{"dbg"}
	d.scope.Refresh()
	waitDebug(t, "the sessions of the extension to end", func() bool { return len(d.starter.Debug().Sessions()) == 0 })
	waitDebug(t, "the listeners to hear of both", func() bool { return len(ended.all()) == 2 })
	if got := d.debug.Debuggers(); len(got) != 0 {
		t.Errorf("the debug types remain after the extension went: %+v", got)
	}
	// and a new session of the type has no debugger
	ok, err := d.startErr(fakeConfig("c", "exec"))
	appErr, isErr := errors.FromError(err)
	if ok || !isErr || appErr.Tag != "DebugTypeUnsupported" {
		t.Errorf("StartDebugging = %v, %v", ok, err)
	}
	// turned on again it is a debugger again
	d.disabled = nil
	d.scope.Refresh()
	d.script("d", sessionScript())
	d.start(fakeConfig("d", "exec"))
	d.stop()
}

func TestClosingTheScopeEndsItsSessions(t *testing.T) {
	d := newDebugRig(t, dbgOpts{})
	d.script("a", sessionScript())
	d.start(fakeConfig("a", "exec"))
	svc := d.debug.Service()
	d.scope.Close()
	waitDebug(t, "the session to end", func() bool { return len(svc.Model().Sessions(false)) == 0 })
}

// ---- configuration providers

func TestProvidersResolveInTheOrderTheyRegisteredAndTheStarLast(t *testing.T) {
	var mu sync.Mutex
	var calls []string
	note := func(s string) { mu.Lock(); calls = append(calls, s); mu.Unlock() }
	add := func(api extension.API, typ, tag string) {
		_, err := api.Debug().RegisterConfigurationProvider(typ, extension.DebugConfigurationProvider{
			Resolve: func(_ context.Context, folder string, cfg extension.DebugConfiguration) (extension.DebugConfiguration, error) {
				note("resolve " + tag)
				cfg[tag] = len(cfg)
				return cfg, nil
			},
			ResolveSubstituted: func(_ context.Context, _ string, cfg extension.DebugConfiguration) (extension.DebugConfiguration, error) {
				note("substituted " + tag)
				cfg[tag+"S"] = cfg["program"]
				return cfg, nil
			},
		}, extension.DebugTriggerInitial)
		if err != nil {
			t.Fatal(err)
		}
	}
	d := newDebugRig(t, dbgOpts{activate: func(_ *debugRig, api extension.API) error {
		add(api, "fake", "first")
		add(api, "*", "star")
		add(api, "fake", "second")
		return nil
	}})
	d.script("s", sessionScript())
	cfg := fakeConfig("s", "exec")
	cfg["program"] = "a${pathSeparator}main"
	s := d.start(cfg)
	d.idle(s)
	mu.Lock()
	got := strings.Join(calls, ", ")
	mu.Unlock()
	if want := "resolve first, resolve second, resolve star, substituted first, substituted second, substituted star"; got != want {
		t.Errorf("calls = %s\nwant    %s", got, want)
	}
	// each provider saw what the one before made
	c := s.Configuration()
	if c["first"] != float64(5) && c["first"] != 5 || c["second"] == nil || c["star"] == nil {
		t.Errorf("configuration = %v", c)
	}
	if c["secondS"] == nil || c["secondS"] != c["program"] {
		t.Errorf("the provider after the substitution saw %v, the program is %v", c["secondS"], c["program"])
	}
	d.stop()
}

func TestAProviderThatGivesNothingCancelsTheLaunch(t *testing.T) {
	later := 0
	d := newDebugRig(t, dbgOpts{activate: func(_ *debugRig, api extension.API) error {
		if _, err := api.Debug().RegisterConfigurationProvider("fake", extension.DebugConfigurationProvider{
			Resolve: func(context.Context, string, extension.DebugConfiguration) (extension.DebugConfiguration, error) {
				return nil, nil
			},
		}, extension.DebugTriggerInitial); err != nil {
			return err
		}
		_, err := api.Debug().RegisterConfigurationProvider("fake", extension.DebugConfigurationProvider{
			Resolve: func(_ context.Context, _ string, cfg extension.DebugConfiguration) (extension.DebugConfiguration, error) {
				later++
				return cfg, nil
			},
		}, extension.DebugTriggerInitial)
		return err
	}})
	ok, err := d.startErr(fakeConfig("s", "exec"))
	if ok || err != nil {
		t.Errorf("StartDebugging = %v, %v: a provider that gives nothing cancels the launch without an error", ok, err)
	}
	if later != 0 {
		t.Error("a provider ran after one that cancelled")
	}
	if len(d.starter.Debug().Sessions()) != 0 {
		t.Error("a session was started")
	}
}

func TestAProviderCanAskForLaunchJSONToBeShown(t *testing.T) {
	var mu sync.Mutex
	var opened []string
	d := newDebugRig(t, dbgOpts{
		tune: func(o *debug.Options) {
			o.OpenLaunchFile = func(_ context.Context, l *debug.Launch, typ string) {
				mu.Lock()
				opened = append(opened, l.Folder+" "+typ)
				mu.Unlock()
			}
		},
		activate: func(_ *debugRig, api extension.API) error {
			_, err := api.Debug().RegisterConfigurationProvider("fake", extension.DebugConfigurationProvider{
				Resolve: func(context.Context, string, extension.DebugConfiguration) (extension.DebugConfiguration, error) {
					return nil, extension.ErrDebugOpenLaunchJSON
				},
			}, extension.DebugTriggerInitial)
			return err
		},
	})
	folder := t.TempDir()
	if err := os.MkdirAll(filepath.Join(folder, ".emetos"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(debug.LaunchFilePath(folder), []byte(`{"configurations": [{"name": "L", "type": "fake", "request": "launch"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := d.debug.Service().Launches().Reload(context.Background(), debug.LaunchSources{Folder: folder}); err != nil {
		t.Fatal(err)
	}
	ok, err := d.starter.Debug().StartDebugging(context.Background(), folder, extension.DebugTarget{Name: "L"}, extension.DebugStartOptions{})
	if ok || err != nil {
		t.Fatalf("StartDebugging = %v, %v", ok, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(opened) != 1 || opened[0] != folder+" fake" {
		t.Errorf("launch.json was shown for %v", opened)
	}
}

func TestAProviderThatFailsFailsTheLaunch(t *testing.T) {
	d := newDebugRig(t, dbgOpts{activate: func(_ *debugRig, api extension.API) error {
		_, err := api.Debug().RegisterConfigurationProvider("fake", extension.DebugConfigurationProvider{
			Resolve: func(context.Context, string, extension.DebugConfiguration) (extension.DebugConfiguration, error) {
				return nil, errors.New("TestProviderFailed", "no program")
			},
		}, extension.DebugTriggerInitial)
		return err
	}})
	ok, err := d.startErr(fakeConfig("s", "exec"))
	appErr, isErr := errors.FromError(err)
	if ok || !isErr || appErr.Tag != "TestProviderFailed" {
		t.Fatalf("StartDebugging = %v, %v", ok, err)
	}
}

func TestAPanicInAProviderTakesTheExtensionOff(t *testing.T) {
	d := newDebugRig(t, dbgOpts{activate: func(_ *debugRig, api extension.API) error {
		_, err := api.Debug().RegisterConfigurationProvider("fake", extension.DebugConfigurationProvider{
			Resolve: func(context.Context, string, extension.DebugConfiguration) (extension.DebugConfiguration, error) {
				panic("boom")
			},
		}, extension.DebugTriggerInitial)
		return err
	}})
	ok, err := d.startErr(fakeConfig("s", "exec"))
	appErr, isErr := errors.FromError(err)
	if ok || !isErr || !appErr.HasRoute("DebugHost.guard.func1.ExtensionDebugPanicked") {
		t.Fatalf("StartDebugging = %v, %v", ok, err)
	}
	if st := d.scope.State("dbg"); st != StateFailed {
		t.Errorf("the extension is %v, want failed", st)
	}
}

func TestTheConfigurationsAProviderGivesForALaunchFileAndTheDropdown(t *testing.T) {
	d := newDebugRig(t, dbgOpts{activate: func(_ *debugRig, api extension.API) error {
		for _, p := range []struct {
			trigger extension.DebugProviderTrigger
			name    string
		}{{extension.DebugTriggerInitial, "initial one"}, {extension.DebugTriggerDynamic, "dynamic one"}, {extension.DebugTriggerInitial, "initial two"}} {
			if _, err := api.Debug().RegisterConfigurationProvider("fake", extension.DebugConfigurationProvider{
				Provide: func(_ context.Context, folder string) ([]extension.DebugConfiguration, error) {
					return []extension.DebugConfiguration{{"name": p.name + " " + folder, "request": "launch"}}, nil
				},
			}, p.trigger); err != nil {
				return err
			}
		}
		return nil
	}})
	ctx := context.Background()
	initial, err := d.debug.initialConfigurations(ctx, "/w", "fake")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, c := range initial {
		names = append(names, c.Name()+"/"+c.Type())
	}
	// the manifest's first, then the providers', each with the type of the debugger
	want := "Fake: Launch/fake, initial one /w/fake, initial two /w/fake"
	if got := strings.Join(names, ", "); got != want {
		t.Errorf("initial configurations = %s, want %s", got, want)
	}
	dyn, err := d.debug.dynamicConfigurations(ctx, "/w", "fake")
	if err != nil || len(dyn) != 1 || dyn[0].Name() != "dynamic one /w" || dyn[0].Type() != "fake" {
		t.Errorf("dynamic configurations = %v, %v", dyn, err)
	}
	types := d.debug.dynamicTypes()
	if len(types) != 1 || types[0].Type != "fake" || types[0].Label != "Fake" {
		t.Errorf("dynamic types = %+v", types)
	}
}

func TestADebuggerThatDeclaresDynamicConfigurationsIsListedBeforeItsExtensionIsActive(t *testing.T) {
	d := newDebugRig(t, dbgOpts{manifest: func(m *extension.Manifest) { m.Contributes.Debuggers[0].DynamicConfigurations = true }})
	if st := d.scope.State("dbg"); st != StateInactive {
		t.Fatalf("state = %v", st)
	}
	if got := d.debug.dynamicTypes(); len(got) != 1 || got[0].Type != "fake" {
		t.Errorf("dynamic types = %+v", got)
	}
	if infos := d.debug.Debuggers(); len(infos) != 1 || !infos[0].Dynamic || infos[0].Extension != "dbg" {
		t.Errorf("debuggers = %+v", infos)
	}
}

// ---- trackers

// traced decodes the kind and the name of a protocol message.
func traced(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var m struct {
		Type    string `json:"type"`
		Command string `json:"command"`
		Event   string `json:"event"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("a tracker got %q: %v", raw, err)
	}
	if m.Type == "event" {
		return "event " + m.Event
	}
	return m.Type + " " + m.Command
}

func TestTrackersSeeEveryMessageInTheOrderOfTheWire(t *testing.T) {
	rec := &strRecorder{}
	var wild strRecorder
	d := newDebugRig(t, dbgOpts{activate: func(d *debugRig, api extension.API) error {
		if _, err := api.Debug().RegisterAdapterTrackerFactory("fake", func(_ context.Context, s extension.DebugSession) (*extension.DebugAdapterTracker, error) {
			rec.add("create " + s.Name())
			return &extension.DebugAdapterTracker{
				OnWillStartSession:   func() { rec.add("start") },
				OnWillReceiveMessage: func(m json.RawMessage) { rec.add("to " + traced(d.t, m)) },
				OnDidSendMessage:     func(m json.RawMessage) { rec.add("from " + traced(d.t, m)) },
				OnWillStopSession:    func() { rec.add("stop") },
			}, nil
		}); err != nil {
			return err
		}
		// a tracker for every type, one that is nil, and one whose factory fails: neither stops the session
		if _, err := api.Debug().RegisterAdapterTrackerFactory("*", func(context.Context, extension.DebugSession) (*extension.DebugAdapterTracker, error) {
			return &extension.DebugAdapterTracker{OnDidSendMessage: func(json.RawMessage) { wild.add("x") }}, nil
		}); err != nil {
			return err
		}
		if _, err := api.Debug().RegisterAdapterTrackerFactory("fake", func(context.Context, extension.DebugSession) (*extension.DebugAdapterTracker, error) { return nil, nil }); err != nil {
			return err
		}
		_, err := api.Debug().RegisterAdapterTrackerFactory("fake", func(context.Context, extension.DebugSession) (*extension.DebugAdapterTracker, error) {
			return nil, errors.New("TestTrackerFactoryFailed", "no")
		})
		return err
	}})
	d.script("s", sessionScript(daptest.Event("output", map[string]any{"category": "console", "output": "hi"})))
	s := d.start(fakeConfig("s", "server"))
	d.idle(s)
	d.stop()
	waitDebug(t, "the tracker to hear of the end", func() bool {
		l := rec.all()
		return len(l) > 0 && l[len(l)-1] == "stop"
	})
	// the messages the tracker heard are the messages the adapter saw and sent, in order
	srv := d.server("s")
	var want []string
	for _, m := range srv.Traffic() {
		switch {
		case m.Direction == "in" && m.Type == "request":
			want = append(want, "to request "+m.Name)
		case m.Direction == "out" && m.Type == "response":
			want = append(want, "from response "+m.Name)
		case m.Direction == "out" && m.Type == "event":
			want = append(want, "from event "+m.Name)
		}
	}
	got := rec.all()
	if len(got) < 4 || got[0] != "create s" || got[1] != "start" || got[len(got)-1] != "stop" {
		t.Fatalf("tracker log = %v", got)
	}
	got = got[2 : len(got)-1]
	// the two sides are written by different goroutines: compare each side in order
	side := func(list []string, prefix string) string {
		var out []string
		for _, l := range list {
			if strings.HasPrefix(l, prefix) {
				out = append(out, l)
			}
		}
		return strings.Join(out, "|")
	}
	if side(got, "to ") != side(want, "to ") {
		t.Errorf("messages to the adapter:\n got %s\nwant %s", side(got, "to "), side(want, "to "))
	}
	if side(got, "from ") != side(want, "from ") {
		t.Errorf("messages from the adapter:\n got %s\nwant %s", side(got, "from "), side(want, "from "))
	}
	if !strings.Contains(side(got, "from "), "from event output") || !strings.Contains(side(got, "to "), "to request initialize") {
		t.Errorf("the log misses the first and last kinds: %v", got)
	}
	if len(wild.all()) == 0 {
		t.Error("the tracker of the type * heard nothing")
	}
}

func TestTrackersHearThatTheAdapterCouldNotBeReached(t *testing.T) {
	rec := &strRecorder{}
	d := newDebugRig(t, dbgOpts{activate: func(_ *debugRig, api extension.API) error {
		_, err := api.Debug().RegisterAdapterTrackerFactory("fake", func(context.Context, extension.DebugSession) (*extension.DebugAdapterTracker, error) {
			return &extension.DebugAdapterTracker{
				OnWillStartSession: func() { rec.add("start") },
				OnError:            func(err error) { rec.add("error " + err.Error()) },
				OnWillStopSession:  func() { rec.add("stop") },
			}, nil
		})
		return err
	}})
	d.override = func(extension.DebugSession) (*extension.DebugAdapter, error) {
		return nil, errors.New("TestNoAdapterHere", "there is no adapter")
	}
	if ok, err := d.startErr(fakeConfig("s", "exec")); ok || err == nil {
		t.Fatalf("StartDebugging = %v, %v", ok, err)
	}
	if got := strings.Join(rec.all(), "|"); got != "start|error there is no adapter|stop" {
		t.Errorf("tracker log = %s", got)
	}
}

func TestTrackersHearThatTheAdapterDied(t *testing.T) {
	var mu sync.Mutex
	var code *int
	rec := &strRecorder{}
	d := newDebugRig(t, dbgOpts{activate: func(_ *debugRig, api extension.API) error {
		_, err := api.Debug().RegisterAdapterTrackerFactory("fake", func(context.Context, extension.DebugSession) (*extension.DebugAdapterTracker, error) {
			return &extension.DebugAdapterTracker{
				OnError:           func(err error) { rec.add("error") },
				OnExit:            func(c *int) { mu.Lock(); code = c; mu.Unlock() },
				OnWillStopSession: func() { rec.add("stop") },
			}, nil
		})
		return err
	}})
	d.script("s", daptest.Script{
		daptest.Reply("initialize", map[string]any{}),
		daptest.Expect("launch", map[string]any{"type": "fake"}),
		daptest.Event("initialized", nil),
		daptest.ReplyArgs("setExceptionBreakpoints", map[string]any{"filters": []string{}}, map[string]any{}),
		daptest.Respond("launch", nil),
		daptest.Reply("threads", map[string]any{"threads": []map[string]any{{"id": 1, "name": "main"}}}),
		daptest.Crash(3),
	})
	s := d.start(fakeConfig("s", "exec"))
	_ = s
	waitDebug(t, "the tracker to hear of the death", func() bool { return len(rec.all()) == 2 })
	mu.Lock()
	defer mu.Unlock()
	if code == nil || *code != 3 {
		t.Errorf("exit code = %v, want 3", code)
	}
	if got := strings.Join(rec.all(), "|"); got != "error|stop" {
		t.Errorf("tracker log = %s", got)
	}
}

func TestTheOutputOfAnAdapterThatRunsTheProgramGoesToTheDebugConsole(t *testing.T) {
	tests := []struct {
		name          string
		consoleOutput bool
		kind          extension.DebugAdapterKind
		want          []string
	}{
		{"a server program that asks for it: its standard error, not the line with the port", true, extension.DebugAdapterExecutableServer, []string{"from the program\n"}},
		{"a server program that does not ask", false, extension.DebugAdapterExecutableServer, nil},
		{"a program on the standard streams that asks: only its standard error", true, extension.DebugAdapterExecutable, []string{"from the program\n"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := newDebugRig(t, dbgOpts{})
			script := sessionScript()
			script = append(daptest.Script{daptest.Stderr("from the program")}, script...)
			d.override = func(extension.DebugSession) (*extension.DebugAdapter, error) {
				a := &extension.DebugAdapter{Kind: tt.kind, Command: daptest.Program(d.t), ConsoleOutput: tt.consoleOutput}
				if tt.kind == extension.DebugAdapterExecutableServer {
					a.Args = []string{"-listen", "tcp", "-announce", "-script", d.scriptFile(script)}
				} else {
					a.Args = []string{"-script", d.scriptFile(script)}
				}
				return a, nil
			}
			s := d.start(fakeConfig("s", "exec"))
			d.idle(s)
			waitDebug(t, "the console to have the output", func() bool {
				return len(d.coreSession(s).Repl().Elements()) >= len(tt.want)
			})
			var got []string
			for _, e := range d.coreSession(s).Repl().Elements() {
				if o, ok := e.(*debug.ReplOutput); ok {
					got = append(got, o.Value)
				}
			}
			if strings.Join(got, "|") != strings.Join(tt.want, "|") {
				t.Errorf("console = %q, want %q", got, tt.want)
			}
			d.stop()
		})
	}
}
