package exthost

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/core/lsp/lsptest"
)

// The launcher of a language server (step 24b.1, SPEC §10.34): the extension decides what to run at each start.

// launcherTest is a server test whose spec names a program that does not exist, so that only a launcher can start it.
func launcherTest(t *testing.T, script lsptest.Script, launcher func(st *serverTest) extension.ServerLauncher) *serverTest {
	t.Helper()
	st := newServerTest(t, script, func(_ *extension.Manifest, sp *extension.LanguageServerSpec) {
		sp.Program = "emetos-no-such-language-server"
		sp.OwnUI = true
	})
	st.onActivate = func(_ context.Context, a extension.API) error {
		return a.LanguageServers().SetLauncher(testServer, launcher(st))
	}
	return st
}

// initializeParams are the params of the initialize request the fake server got.
func initializeParams(t *testing.T, c *serverCtl) map[string]any {
	t.Helper()
	for _, m := range traffic(t, c) {
		if m.FromClient && m.Method == lsp.MethodInitialize {
			var body struct {
				Params map[string]any `json:"params"`
			}
			if err := json.Unmarshal(m.Body, &body); err != nil {
				t.Fatal(err)
			}
			return body.Params
		}
	}
	t.Fatal("the server got no initialize request")
	return nil
}

func TestTheLauncherDecidesWhatRuns(t *testing.T) {
	var mu sync.Mutex
	var requests []extension.LaunchRequest
	var root string
	st := launcherTest(t, simple(), func(st *serverTest) extension.ServerLauncher {
		return func(_ context.Context, req extension.LaunchRequest) (extension.ServerLaunch, error) {
			mu.Lock()
			requests = append(requests, req)
			mu.Unlock()
			return extension.ServerLaunch{
				Program: st.prog, Args: []string{"-script", st.script}, Root: root,
				InitializationOptions: map[string]any{"cargo": map[string]any{"features": "all"}},
			}, nil
		}
	})
	// the project root above the folder of the tab (the folder opened is a subfolder of the project)
	root = filepath.Dir(st.folder)
	tb := st.startTab("one")
	st.goDoc(tb, "a.go", "package a\n")
	s := st.waitRunning(tb)
	if s.Starts != 1 {
		t.Errorf("status = %+v", s)
	}
	mu.Lock()
	if len(requests) != 1 || requests[0].Folder != st.folder {
		t.Errorf("requests = %+v, want one for the folder %s", requests, st.folder)
	}
	// a registered setting reads, an unknown key does not (the request is for any goroutine)
	if v, ok := requests[0].Setting("editor.tabSize"); !ok || v != float64(4) {
		t.Errorf("Setting(editor.tabSize) = %v, %v", v, ok)
	}
	if v, ok := requests[0].Setting("no.such.key"); ok {
		t.Errorf("Setting(no.such.key) = %v, true", v)
	}
	mu.Unlock()
	params := initializeParams(t, st.controller(tb))
	opts, _ := params["initializationOptions"].(map[string]any)
	if cargo, _ := opts["cargo"].(map[string]any); cargo["features"] != "all" {
		t.Errorf("initializationOptions = %v", params["initializationOptions"])
	}
	if got, _ := params["rootUri"].(string); !strings.HasSuffix(got, filepath.ToSlash(root)) {
		t.Errorf("rootUri = %v, want the root of the launch %s", params["rootUri"], root)
	}
	if lines := strings.Join(tb.channels.Lines(channelName), "\n"); !strings.Contains(lines, "Starting Fake LS: "+st.prog) {
		t.Errorf("the channel has %q", lines)
	}
}

func TestTheLauncherRunsAgainAtEveryStart(t *testing.T) {
	var mu sync.Mutex
	starts := 0
	st := launcherTest(t, simple(), func(st *serverTest) extension.ServerLauncher {
		return func(_ context.Context, _ extension.LaunchRequest) (extension.ServerLaunch, error) {
			mu.Lock()
			starts++
			mu.Unlock()
			return extension.ServerLaunch{Program: st.prog, Args: []string{"-script", st.script}}, nil
		}
	})
	tb := st.startTab("one")
	st.goDoc(tb, "a.go", "package a\n")
	first := st.waitRunning(tb)
	if err := tb.run(extension.RestartServerCommand(testExt)); err != nil {
		t.Fatal(err)
	}
	second := st.waitRunning(tb)
	mu.Lock()
	defer mu.Unlock()
	if starts != 2 || second.Starts != 2 || second.PID == first.PID {
		t.Errorf("launcher ran %d times, status %+v after %+v", starts, second, first)
	}
}

func TestALauncherThatFailsLeavesTheServerMissingUntilItIsRestarted(t *testing.T) {
	var mu sync.Mutex
	found := false
	st := launcherTest(t, simple(), func(st *serverTest) extension.ServerLauncher {
		return func(_ context.Context, _ extension.LaunchRequest) (extension.ServerLaunch, error) {
			mu.Lock()
			defer mu.Unlock()
			if !found {
				return extension.ServerLaunch{}, errors.New("FakeNotFound", "the fake server is not installed")
			}
			return extension.ServerLaunch{Program: st.prog, Args: []string{"-script", st.script}}, nil
		}
	})
	var states []extension.ServerState
	prev := st.onActivate
	st.onActivate = func(ctx context.Context, a extension.API) error {
		if _, err := a.LanguageServers().OnChange(testServer, func(s extension.ServerStatus) { states = append(states, s.State) }); err != nil {
			return err
		}
		return prev(ctx, a)
	}
	tb := st.startTab("one")
	st.goDoc(tb, "a.go", "package a\n")
	waitFor(t, tb, "the server to be missing", func() bool { return st.status(tb).State == ServerMissing })
	st.goDoc(tb, "b.go", "package b\n")
	if s := st.status(tb); s.State != ServerMissing || s.Starts != 0 {
		t.Fatalf("status = %+v", s)
	}
	// the server has its own UI: no notification of the host, but the Output channel says why
	if got := tb.notifications(); len(got) != 0 {
		t.Errorf("notifications = %v", got)
	}
	if lines := strings.Join(tb.channels.Lines(channelName), "\n"); !strings.Contains(lines, "Fake LS cannot be started: the fake server is not installed") {
		t.Errorf("the channel has %q", lines)
	}
	mu.Lock()
	found = true
	mu.Unlock()
	if err := tb.run(extension.RestartServerCommand(testExt)); err != nil {
		t.Fatal(err)
	}
	st.waitRunning(tb)
	if states[len(states)-1] != extension.ServerRunning {
		t.Errorf("states = %v", states)
	}
}

func TestALauncherThatFailsInAServerWithoutOwnUIIsToldToTheUserOnce(t *testing.T) {
	st := newServerTest(t, simple(), func(_ *extension.Manifest, sp *extension.LanguageServerSpec) {
		sp.Program = "emetos-no-such-language-server"
	})
	st.onActivate = func(_ context.Context, a extension.API) error {
		return a.LanguageServers().SetLauncher(testServer, func(context.Context, extension.LaunchRequest) (extension.ServerLaunch, error) {
			return extension.ServerLaunch{}, errors.New("FakeTooOld", "version 1 is older than 2")
		})
	}
	tb := st.startTab("one")
	st.goDoc(tb, "a.go", "package a\n")
	waitFor(t, tb, "the server to be missing", func() bool { return st.status(tb).State == ServerMissing })
	st.goDoc(tb, "b.go", "package b\n")
	got := tb.notifications()
	if len(got) != 1 || !strings.Contains(got[0], "Fake LS cannot be started: version 1 is older than 2") {
		t.Errorf("notifications = %v", got)
	}
}

func TestALauncherThatAnswersNoProgramFailsTheStart(t *testing.T) {
	st := launcherTest(t, simple(), func(*serverTest) extension.ServerLauncher {
		return func(context.Context, extension.LaunchRequest) (extension.ServerLaunch, error) {
			return extension.ServerLaunch{}, nil
		}
	})
	tb := st.startTab("one")
	st.goDoc(tb, "a.go", "package a\n")
	waitFor(t, tb, "the server to be missing", func() bool { return st.status(tb).State == ServerMissing })
	if lines := strings.Join(tb.channels.Lines(channelName), "\n"); !strings.Contains(lines, "answered with no program") {
		t.Errorf("the channel has %q", lines)
	}
	// the route of the failure
	_, err := st.controller(tb).runLauncher(context.Background(), func(context.Context, extension.LaunchRequest) (extension.ServerLaunch, error) {
		return extension.ServerLaunch{}, nil
	}, extension.LaunchRequest{})
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("serverCtl.runLauncher.ExtensionLauncherNoProgram") {
		t.Errorf("err = %v", err)
	}
}

func TestALauncherThatPanicsFailsTheExtension(t *testing.T) {
	st := launcherTest(t, simple(), func(*serverTest) extension.ServerLauncher {
		return func(context.Context, extension.LaunchRequest) (extension.ServerLaunch, error) { panic("boom") }
	})
	tb := st.startTab("one")
	st.goDoc(tb, "a.go", "package a\n")
	waitFor(t, tb, "the extension to fail", func() bool { return tb.scope.State(testExt) == StateFailed })
	fl := tb.scope.Failure(testExt)
	if fl == nil {
		t.Fatal("no failure recorded")
	}
	appErr, ok := errors.FromError(fl.Err)
	if !ok || appErr.Tag != "ExtensionLauncherPanicked" || !appErr.HasRoute("serverCtl.runLauncher.func1.ExtensionLauncherPanicked") {
		t.Errorf("failure = %v", fl.Err)
	}
}

func TestTheLauncherSeesItsContextEndWhenTheServerIsRestarted(t *testing.T) {
	entered := make(chan struct{})
	ended := make(chan struct{})
	var once sync.Once
	st := launcherTest(t, simple(), func(st *serverTest) extension.ServerLauncher {
		return func(ctx context.Context, _ extension.LaunchRequest) (extension.ServerLaunch, error) {
			blocked := false
			once.Do(func() { blocked = true })
			if blocked {
				close(entered)
				<-ctx.Done()
				close(ended)
				return extension.ServerLaunch{}, ctx.Err()
			}
			return extension.ServerLaunch{Program: st.prog, Args: []string{"-script", st.script}}, nil
		}
	})
	tb := st.startTab("one")
	st.goDoc(tb, "a.go", "package a\n")
	<-entered
	if err := tb.run(extension.RestartServerCommand(testExt)); err != nil {
		t.Fatal(err)
	}
	<-ended
	st.waitRunning(tb)
}

func TestTheLauncherAnswersWorkspaceConfigurationBeforeTheSettings(t *testing.T) {
	script := simple()
	script.Send = []lsptest.Outgoing{{
		After: lsp.MethodInitialized, Request: true, Method: lsp.MethodConfiguration,
		Params: raw(`{"items":[{"section":"python"},{"section":"editor.tabSize"}]}`),
	}}
	st := launcherTest(t, script, func(st *serverTest) extension.ServerLauncher {
		return func(context.Context, extension.LaunchRequest) (extension.ServerLaunch, error) {
			return extension.ServerLaunch{
				Program: st.prog, Args: []string{"-script", st.script},
				Configuration: func(section string) (json.RawMessage, bool) {
					if section == "python" {
						return json.RawMessage(`{"pythonPath":"/venv/bin/python"}`), true
					}
					return nil, false
				},
			}, nil
		}
	})
	tb := st.startTab("one")
	st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)
	c := st.controller(tb)
	waitFor(t, tb, "the answer to workspace/configuration", func() bool { return len(responsesTo(t, c)) == 1 })
	var result []json.RawMessage
	if err := json.Unmarshal(responsesTo(t, c)[0], &result); err != nil {
		t.Fatal(err)
	}
	if len(result) != 2 || string(result[0]) != `{"pythonPath":"/venv/bin/python"}` || string(result[1]) != `4` {
		t.Errorf("result = %s", result)
	}
}

func TestTheSettingsOfTheLauncherAreSentBeforeTheDocumentsOpen(t *testing.T) {
	st := launcherTest(t, simple(), func(st *serverTest) extension.ServerLauncher {
		return func(context.Context, extension.LaunchRequest) (extension.ServerLaunch, error) {
			return extension.ServerLaunch{
				Program: st.prog, Args: []string{"-script", st.script},
				Settings: map[string]any{"typescript": map[string]any{"format": map[string]any{"semicolons": "remove"}}},
			}, nil
		}
	})
	tb := st.startTab("one")
	st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)
	var methods []string
	var settings string
	waitFor(t, tb, "the document to open", func() bool {
		methods, settings = nil, ""
		for _, m := range traffic(t, st.controller(tb)) {
			if !m.FromClient {
				continue
			}
			methods = append(methods, m.Method)
			if m.Method == lsp.MethodDidChangeConfiguration {
				settings = string(m.Body)
			}
		}
		return slices.Contains(methods, lsp.MethodDidOpen)
	})
	changed, opened := slices.Index(methods, lsp.MethodDidChangeConfiguration), slices.Index(methods, lsp.MethodDidOpen)
	if changed < 0 || changed > opened || slices.Index(methods, lsp.MethodInitialized) > changed {
		t.Errorf("methods = %v: the settings come after initialized and before the first document", methods)
	}
	if !strings.Contains(settings, `"settings":{"typescript":{"format":{"semicolons":"remove"}}}`) {
		t.Errorf("settings = %s", settings)
	}
}

func TestALauncherWithoutSettingsSendsNone(t *testing.T) {
	st := launcherTest(t, simple(), func(st *serverTest) extension.ServerLauncher {
		return func(context.Context, extension.LaunchRequest) (extension.ServerLaunch, error) {
			return extension.ServerLaunch{Program: st.prog, Args: []string{"-script", st.script}}, nil
		}
	})
	tb := st.startTab("one")
	st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)
	waitFor(t, tb, "the document to open", func() bool {
		for _, m := range traffic(t, st.controller(tb)) {
			if m.FromClient && m.Method == lsp.MethodDidOpen {
				return true
			}
		}
		return false
	})
	for _, m := range traffic(t, st.controller(tb)) {
		if m.FromClient && m.Method == lsp.MethodDidChangeConfiguration {
			t.Errorf("the client sent settings nobody gave: %s", m.Body)
		}
	}
}

func TestATraceKeyReplacesTheQualifiedName(t *testing.T) {
	st := newServerTest(t, simple(), func(_ *extension.Manifest, sp *extension.LanguageServerSpec) {
		sp.TraceKey = "fake-analyzer.trace.server"
	})
	tb := st.startTab("one")
	if _, ok := st.rig.registry.Lookup("fake-analyzer.trace.server"); !ok {
		t.Fatal("the setting named by TraceKey is not registered")
	}
	if _, ok := st.rig.registry.Lookup(testExt + "." + testServer + ".trace.server"); ok {
		t.Error("the qualified setting is registered too")
	}
	tb.changeSetting("fake-analyzer.trace.server", "messages")
	st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)
	waitFor(t, tb, "the trace in the channel", func() bool {
		return strings.Contains(strings.Join(tb.channels.Lines(channelName), "\n"), "Sending request 'initialize")
	})
}

func TestSetLauncherFailures(t *testing.T) {
	tests := []struct {
		name  string
		serve string
		route string
	}{
		{"a server the manifest does not declare", "nothing", "serversAPI.SetLauncher.ExtensionServerUnknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newServerTest(t, simple(), nil)
			st.startTab("one")
			err := st.api.LanguageServers().SetLauncher(tt.serve, func(context.Context, extension.LaunchRequest) (extension.ServerLaunch, error) {
				return extension.ServerLaunch{}, nil
			})
			appErr, ok := errors.FromError(err)
			if !ok || !appErr.HasRoute(tt.route) {
				t.Errorf("err = %v, want route %s", err, tt.route)
			}
		})
	}
}

func TestSetLauncherAfterTheActivationEnded(t *testing.T) {
	st := newServerTest(t, simple(), nil)
	tb := st.startTab("one")
	api := st.api
	tb.scope.Close()
	err := api.LanguageServers().SetLauncher(testServer, func(context.Context, extension.LaunchRequest) (extension.ServerLaunch, error) {
		return extension.ServerLaunch{}, nil
	})
	appErr, ok := errors.FromError(err)
	if !ok || !appErr.HasRoute("serversAPI.SetLauncher.ExtensionDeactivated") {
		t.Errorf("err = %v", err)
	}
}

func TestANilLauncherTakesTheLauncherAway(t *testing.T) {
	st := launcherTest(t, simple(), func(st *serverTest) extension.ServerLauncher {
		return func(context.Context, extension.LaunchRequest) (extension.ServerLaunch, error) {
			return extension.ServerLaunch{Program: st.prog, Args: []string{"-script", st.script}}, nil
		}
	})
	tb := st.startTab("one")
	if err := st.api.LanguageServers().SetLauncher(testServer, nil); err != nil {
		t.Fatal(err)
	}
	st.goDoc(tb, "a.go", "package a\n")
	// the spec's program does not exist
	if s := st.status(tb); s.State != ServerMissing {
		t.Errorf("status = %+v", s)
	}
}
