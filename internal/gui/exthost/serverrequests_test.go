package exthost

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/core/lsp/lsptest"
)

// What an extension does in the protocol of its own server (step 24b.2, SPEC §9.12 "As built (step 24b.2)"): requests of methods the
// host does not know, the notifications of the server, the encoding of its positions, and the experimental capabilities the launcher
// asks to advertise.

// protocolScript is a server that answers three methods of its own and says it counts bytes.
func protocolScript() lsptest.Script {
	return lsptest.Script{
		Capabilities: raw(`{"textDocumentSync":2,"positionEncoding":"utf-8"}`),
		Responses: map[string]lsptest.Response{
			"custom/echo": {Result: raw(`{"ok":true}`)},
			"custom/null": {},
			"custom/fail": {Error: &lsp.ResponseError{Code: -32001, Message: "no luck"}},
			"custom/slow": {Result: raw(`1`), DelayMS: 5000},
		},
		Send: []lsptest.Outgoing{{After: lsp.MethodInitialized, Method: "custom/status", Params: raw(`{"health":"ok","quiescent":true}`)}},
	}
}

// protocolTest is a server test whose launcher advertises experimental capabilities.
func protocolTest(t *testing.T, script lsptest.Script, experimental map[string]any) *serverTest {
	t.Helper()
	return launcherTest(t, script, func(st *serverTest) extension.ServerLauncher {
		return func(context.Context, extension.LaunchRequest) (extension.ServerLaunch, error) {
			return extension.ServerLaunch{Program: st.prog, Args: []string{"-script", st.script}, ClientExperimental: experimental}, nil
		}
	})
}

func TestAnExtensionSendsRequestsOfTheServersOwnProtocol(t *testing.T) {
	st := protocolTest(t, protocolScript(), nil)
	tb := st.startTab("one")
	ls := func() extension.LanguageServers { return st.api.LanguageServers() }
	// the server does not run yet
	_, err := ls().Request(context.Background(), testServer, "custom/echo", nil)
	wantRoute(t, err, "serversAPI.Request.ExtensionServerNotRunning")
	wantRoute(t, ls().Notify(testServer, "custom/note", nil), "serversAPI.Notify.ExtensionServerNotRunning")
	if enc, err := ls().PositionEncoding(testServer); err != nil || enc != "utf-16" {
		t.Errorf("PositionEncoding before the server runs = %q, %v", enc, err)
	}
	st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)

	tests := []struct {
		name, method string
		params       any
		want         string
		route        string
	}{
		{"a result", "custom/echo", map[string]any{"a": 1}, `{"ok":true}`, ""},
		{"a null result", "custom/null", nil, `null`, ""},
		{"an error of the server", "custom/fail", nil, "", "ServerError"},
		{"a method the server does not know", "custom/unknown", nil, "", "ServerError"},
		{"params that do not encode", "custom/echo", make(chan int), "", "serversAPI.Request.ExtensionArgumentsInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got json.RawMessage
			var err error
			done := make(chan struct{})
			go func() {
				defer close(done)
				got, err = ls().Request(context.Background(), testServer, tt.method, tt.params)
			}()
			waitFor(t, tb, "the answer", func() bool {
				select {
				case <-done:
					return true
				default:
					return false
				}
			})
			if tt.route != "" {
				wantRoute(t, err, tt.route)
				return
			}
			if err != nil || string(got) != tt.want {
				t.Errorf("Request = %s, %v; want %s", got, err, tt.want)
			}
		})
	}
	// a notification is queued at once and the server gets it with its params
	if err := ls().Notify(testServer, "custom/note", map[string]any{"b": 2}); err != nil {
		t.Errorf("Notify: %v", err)
	}
	wantRoute(t, ls().Notify(testServer, "custom/note", make(chan int)), "serversAPI.Notify.ExtensionArgumentsInvalid")
	wantRoute(t, ls().Notify("nothing", "custom/note", nil), "serversAPI.Notify.ExtensionServerUnknown")
	waitFor(t, tb, "the notification", func() bool {
		for _, m := range traffic(t, st.controller(tb)) {
			if m.FromClient && m.Method == "custom/note" && strings.Contains(string(m.Body), `"b":2`) {
				return true
			}
		}
		return false
	})
	// the server got the params as JSON
	var seen bool
	for _, m := range traffic(t, st.controller(tb)) {
		if m.FromClient && m.Method == "custom/echo" && strings.Contains(string(m.Body), `"a":1`) {
			seen = true
		}
	}
	if !seen {
		t.Error("the server did not get the params of the request")
	}
	if enc, err := ls().PositionEncoding(testServer); err != nil || enc != "utf-8" {
		t.Errorf("PositionEncoding = %q, %v; the server chose UTF-8", enc, err)
	}
	_, err = ls().Request(context.Background(), "nothing", "custom/echo", nil)
	wantRoute(t, err, "serversAPI.Request.ExtensionServerUnknown")
	_, err = ls().PositionEncoding("nothing")
	wantRoute(t, err, "serversAPI.PositionEncoding.ExtensionServerUnknown")
	_, err = ls().OnNotification("nothing", "custom/status", func(json.RawMessage) {})
	wantRoute(t, err, "serversAPI.OnNotification.ExtensionServerUnknown")
}

func TestARequestEndsWithItsContext(t *testing.T) {
	st := protocolTest(t, protocolScript(), nil)
	tb := st.startTab("one")
	st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	var err error
	done := make(chan struct{})
	start := time.Now()
	go func() {
		defer close(done)
		_, err = st.api.LanguageServers().Request(ctx, testServer, "custom/slow", nil)
	}()
	waitFor(t, tb, "the request to end", func() bool {
		select {
		case <-done:
			return true
		default:
			return false
		}
	})
	if err == nil || time.Since(start) > 4*time.Second {
		t.Errorf("Request = %v after %v; it should end when the context does", err, time.Since(start))
	}
}

func TestARequestAfterTheActivationEndedIsRefused(t *testing.T) {
	st := protocolTest(t, protocolScript(), nil)
	tb := st.startTab("one")
	st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)
	tb.scope.Close()
	_, err := st.api.LanguageServers().Request(context.Background(), testServer, "custom/echo", nil)
	wantRoute(t, err, "serversAPI.Request.ExtensionDeactivated")
}

func TestTheLauncherAddsTheExperimentalCapabilities(t *testing.T) {
	experimental := map[string]any{"serverStatusNotification": true, "commands": map[string]any{"commands": []string{}}}
	st := protocolTest(t, protocolScript(), experimental)
	tb := st.startTab("one")
	st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)
	caps, _ := initializeParams(t, st.controller(tb))["capabilities"].(map[string]any)
	exp, _ := caps["experimental"].(map[string]any)
	if exp["serverStatusNotification"] != true {
		t.Errorf("experimental = %v", caps["experimental"])
	}
	if cmds, _ := exp["commands"].(map[string]any); cmds == nil || len(cmds["commands"].([]any)) != 0 {
		t.Errorf("experimental.commands = %v", exp["commands"])
	}
	// the rest of what Emetos advertises is still there
	if _, ok := caps["textDocument"]; !ok {
		t.Errorf("the capabilities lost their text document member: %v", caps)
	}
	if gen, _ := caps["general"].(map[string]any); gen["positionEncodings"] == nil {
		t.Errorf("general = %v", caps["general"])
	}
}

func TestWithoutExperimentalCapabilitiesTheDefaultsAreSent(t *testing.T) {
	st := protocolTest(t, protocolScript(), nil)
	tb := st.startTab("one")
	st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)
	caps, _ := initializeParams(t, st.controller(tb))["capabilities"].(map[string]any)
	if _, ok := caps["experimental"]; ok {
		t.Errorf("experimental = %v, want none", caps["experimental"])
	}
}

func TestAnExtensionHearsTheNotificationsOfItsServer(t *testing.T) {
	var mu sync.Mutex
	var got, others []string
	var cancel func()
	st := protocolTest(t, protocolScript(), nil)
	st.onActivate = func(_ context.Context, a extension.API) error {
		if err := a.LanguageServers().SetLauncher(testServer, func(context.Context, extension.LaunchRequest) (extension.ServerLaunch, error) {
			return extension.ServerLaunch{Program: st.prog, Args: []string{"-script", st.script}}, nil
		}); err != nil {
			return err
		}
		var err error
		cancel, err = a.LanguageServers().OnNotification(testServer, "custom/status", func(p json.RawMessage) {
			mu.Lock()
			got = append(got, string(p))
			mu.Unlock()
		})
		if err != nil {
			return err
		}
		_, err = a.LanguageServers().OnNotification(testServer, "custom/other", func(p json.RawMessage) {
			mu.Lock()
			others = append(others, string(p))
			mu.Unlock()
		})
		return err
	}
	tb := st.startTab("one")
	st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)
	count := func() int { mu.Lock(); defer mu.Unlock(); return len(got) }
	waitFor(t, tb, "the notification", func() bool { return count() == 1 })
	mu.Lock()
	if got[0] != `{"health":"ok","quiescent":true}` || len(others) != 0 {
		t.Errorf("got %v, others %v", got, others)
	}
	mu.Unlock()
	// the subscription outlives a restart of the server: the server says it again when it starts
	if err := st.api.LanguageServers().Restart(testServer); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tb, "the notification after the restart", func() bool { return count() == 2 })
	// a cancelled subscription hears nothing more
	cancel()
	if err := st.api.LanguageServers().Restart(testServer); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tb, "the server to start again", func() bool { return st.status(tb).Starts == 3 && st.status(tb).State == ServerRunning })
	tb.pump()
	if count() != 2 {
		t.Errorf("the cancelled subscription heard %d notifications", count())
	}
}

func TestAListenerOfNotificationsThatPanicsFailsTheExtension(t *testing.T) {
	st := protocolTest(t, protocolScript(), nil)
	st.onActivate = func(_ context.Context, a extension.API) error {
		if err := a.LanguageServers().SetLauncher(testServer, func(context.Context, extension.LaunchRequest) (extension.ServerLaunch, error) {
			return extension.ServerLaunch{Program: st.prog, Args: []string{"-script", st.script}}, nil
		}); err != nil {
			return err
		}
		_, err := a.LanguageServers().OnNotification(testServer, "custom/status", func(json.RawMessage) { panic("boom") })
		return err
	}
	tb := st.startTab("one")
	st.goDoc(tb, "a.go", "package a\n")
	waitFor(t, tb, "the extension to fail", func() bool { return tb.scope.State(testExt) == StateFailed })
	appErr, ok := errors.FromError(tb.scope.Failure(testExt).Err)
	if !ok || appErr.Tag != "ExtensionServerCallbackPanicked" || !appErr.HasRoute("Scope.runNotificationListener.func1.ExtensionServerCallbackPanicked") {
		t.Errorf("failure = %v", tb.scope.Failure(testExt).Err)
	}
}
