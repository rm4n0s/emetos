package exthost

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/document"
	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/core/lsp/lsptest"
	"github.com/rm4n0s/emetos/internal/core/notify"
	"github.com/rm4n0s/emetos/internal/core/settings"
	"github.com/rm4n0s/emetos/internal/core/workspace"
)

// The language servers of extensions (step 12.3), run with the fake server program of core/lsp/lsptest.

const (
	serverCaps  = `{"textDocumentSync":2,"hoverProvider":true}`
	channelName = "Fake LS"
	statusID    = "extension." + testExt + ".server." + testServer
)

func simple() lsptest.Script { return lsptest.Script{Capabilities: raw(serverCaps)} }

func TestServerStartsOnTheFirstDocumentOfItsLanguages(t *testing.T) {
	st := newServerTest(t, simple(), nil)
	tb := st.startTab("one")
	if s := st.status(tb); s.State != ServerIdle || s.Starts != 0 || s.PID != 0 {
		t.Fatalf("before any document: %+v", s)
	}
	opts := document.NewOptions()
	opts.LanguageID = "rust"
	tb.lsp.open(document.New(document.FileURI(filepath.Join(st.folder, "a.rs")), "fn main() {}\n", opts))
	tb.pump()
	if s := st.status(tb); s.State != ServerIdle {
		t.Fatalf("a document of another language started the server: %+v", s)
	}

	a := st.goDoc(tb, "a.go", "package a\n")
	s := st.waitRunning(tb)
	if s.Starts != 1 || s.PID <= 0 {
		t.Fatalf("status = %+v", s)
	}
	c := st.controller(tb)
	if got := serverDoc(t, c, uriOf(a.URI().Path)); !got.Open || got.Text != "package a\n" || got.LanguageID != "go" {
		t.Errorf("the server's copy of a.go = %+v", got)
	}

	// the other documents of its languages: one in the folder, an untitled one, one outside the folder
	b := st.goDoc(tb, "b.go", "package b\n")
	untitled := document.New(document.UntitledURI(1), "package u\n", func() document.Options { o := document.NewOptions(); o.LanguageID = "go"; return o }())
	tb.lsp.open(untitled)
	outside := document.New(document.FileURI(filepath.Join(t.TempDir(), "o.go")), "package o\n", func() document.Options { o := document.NewOptions(); o.LanguageID = "go"; return o }())
	tb.lsp.open(outside)
	tb.pump()
	if !serverDoc(t, c, uriOf(b.URI().Path)).Open || !serverDoc(t, c, "untitled:Untitled-1").Open {
		t.Error("b.go or the untitled document is not open on the server")
	}
	if serverDoc(t, c, uriOf(outside.URI().Path)).Open {
		t.Error("a document outside the folder was opened on the server")
	}
	if got := st.status(tb); got.Starts != 1 || got.PID != s.PID {
		t.Errorf("a second server was started: %+v", got)
	}

	// an edit reaches the server, and closing a document closes it there
	if _, err := a.Edit([]document.Edit{{Text: "// hi\n"}}, document.EditOptions{}); err != nil {
		t.Fatal(err)
	}
	if got := serverDoc(t, c, uriOf(a.URI().Path)).Text; got != "// hi\npackage a\n" {
		t.Errorf("after an edit the server has %q", got)
	}
	tb.lsp.closeDoc(a)
	tb.pump()
	if serverDoc(t, c, uriOf(a.URI().Path)).Open {
		t.Error("a closed document is still open on the server")
	}
}

func TestADocumentOpenedBeforeTheServerIsReadyOpensWhenItIs(t *testing.T) {
	script := simple()
	script.Responses = map[string]lsptest.Response{lsp.MethodInitialize: {Result: raw(`{"capabilities":` + serverCaps + `}`), DelayMS: 300}}
	st := newServerTest(t, script, nil)
	tb := st.startTab("one")
	a := st.goDoc(tb, "a.go", "package a\n")
	if s := st.status(tb); s.State != ServerStarting {
		t.Fatalf("status = %+v, want starting", s)
	}
	// while it starts the status bar says so
	item, ok := tb.bar.Get(statusID)
	if !ok || !item.Visible || !strings.Contains(item.Entry.Text, "starting") {
		t.Errorf("the status item while starting = %+v %v", item, ok)
	}
	b := st.goDoc(tb, "b.go", "package b\n")
	st.waitRunning(tb)
	c := st.controller(tb)
	for _, d := range []*document.Document{a, b} {
		if !serverDoc(t, c, uriOf(d.URI().Path)).Open {
			t.Errorf("%s was not opened once the server was ready", d.URI().Path)
		}
	}
	if item, _ := tb.bar.Get(statusID); item.Visible {
		t.Errorf("the status item stays after the start: %+v", item)
	}
}

func TestEachWindowTabHasItsOwnServer(t *testing.T) {
	st := newServerTest(t, simple(), nil)
	one := st.startTab("one")
	st.rig.folders["two"] = t.TempDir()
	other := st.rig.newTab("two")
	other.deferred = true
	other.scope.Start()
	st.goDoc(one, "a.go", "package a\n")
	dir2 := st.rig.folderOf("two")
	opts := document.NewOptions()
	opts.LanguageID = "go"
	other.lsp.open(document.New(document.FileURI(filepath.Join(dir2, "z.go")), "package z\n", opts))
	s1 := st.waitRunning(one)
	waitFor(t, other, "the second server", func() bool { return st.status(other).State == ServerRunning })
	s2 := st.status(other)
	if s1.PID == s2.PID || s1.Starts != 1 || s2.Starts != 1 {
		t.Fatalf("one process for two tabs: %+v %+v", s1, s2)
	}
	// each server has its own document only
	if serverDoc(t, st.controller(one), uriOf(filepath.Join(dir2, "z.go"))).Open {
		t.Error("the document of the second tab reached the first server")
	}
	// closing a tab stops its server only
	other.scope.Close()
	st.rig.host.WaitServers()
	requireGone(t, "the server of the closed tab", s2.PID)
	if processGone(s1.PID) {
		t.Error("the server of the other tab stopped too")
	}
}

func TestTheServerStopsWithItsTabAndWhenTheExtensionGoes(t *testing.T) {
	tests := []struct {
		name string
		stop func(st *serverTest, tb *tab)
	}{
		{"the tab closes", func(_ *serverTest, tb *tab) { tb.scope.Close() }},
		{"the extension is disabled", func(_ *serverTest, tb *tab) { tb.disabled = []string{testExt}; tb.scope.Refresh() }},
		{"the extension fails", func(st *serverTest, tb *tab) {
			st.api.Post(func() { panic("boom") })
			tb.pump()
		}},
		{"the host quits", func(st *serverTest, _ *tab) { st.rig.host.Shutdown() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newServerTest(t, simple(), nil)
			tb := st.startTab("one")
			st.goDoc(tb, "a.go", "package a\n")
			s := st.waitRunning(tb)
			if processGone(s.PID) {
				t.Fatal("the server is not running")
			}
			tt.stop(st, tb)
			st.rig.host.WaitServers()
			requireGone(t, "the server", s.PID)
			if got, _ := tb.scope.ServerStatus(testExt, testServer); got.State == ServerRunning {
				t.Errorf("status after the stop = %+v", got)
			}
			if _, ok := tb.bar.Get(statusID); ok {
				t.Error("the status item stays after the stop")
			}
		})
	}
}

func TestAServerThatIgnoresShutdownIsKilledAfterTheGrace(t *testing.T) {
	// the server stops answering requests, so shutdown never completes
	script := simple()
	script.StallOn = lsp.MethodShutdown
	st := newServerTest(t, script, nil)
	st.rig.host.grace = 300 * time.Millisecond
	tb := st.startTab("one")
	st.goDoc(tb, "a.go", "package a\n")
	s := st.waitRunning(tb)
	start := time.Now()
	tb.scope.Close()
	st.rig.host.WaitServers()
	requireGone(t, "the stalled server", s.PID)
	if d := time.Since(start); d > 12*time.Second {
		t.Errorf("stopping took %v", d)
	}
}

func TestACrashedServerIsRestartedWithItsDocuments(t *testing.T) {
	st := newServerTest(t, simple(), nil)
	tb := st.startTab("one")
	a := st.goDoc(tb, "a.go", "package a\n")
	first := st.waitRunning(tb)
	c := st.controller(tb)
	crash(t, c)
	waitFor(t, tb, "the restart", func() bool { s := st.status(tb); return s.State == ServerRunning && s.Starts == 2 })
	second := st.status(tb)
	if second.PID == first.PID || second.Crashes != 1 {
		t.Errorf("status after the crash = %+v (first %+v)", second, first)
	}
	requireGone(t, "the crashed server", first.PID)
	if got := serverDoc(t, c, uriOf(a.URI().Path)); !got.Open || got.Text != "package a\n" {
		t.Errorf("the document was not opened on the new server: %+v", got)
	}
	// the restart is told in the Output channel and nobody was bothered
	if lines := strings.Join(tb.channels.Lines(channelName), "\n"); !strings.Contains(lines, "Restarting Fake LS") {
		t.Errorf("Output = %s", lines)
	}
	if len(tb.notifications()) != 0 {
		t.Errorf("notifications = %v", tb.notifications())
	}
}

// crash makes the running server die.
func crash(t *testing.T, c *serverCtl) {
	t.Helper()
	client := c.currentClient()
	if client == nil {
		t.Fatal("the server is not running")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Call(ctx, lsptest.MethodCrash, nil, nil); err == nil {
		t.Fatal("the crash request was answered")
	}
}

func TestFiveCrashesInThreeMinutesStopTheRestarts(t *testing.T) {
	script := simple()
	script.CrashOn = lsp.MethodInitialize // every start dies in the handshake
	st := newServerTest(t, script, nil)
	tb := st.startTab("one")
	st.goDoc(tb, "a.go", "package a\n")
	waitFor(t, tb, "the server to give up", func() bool { return st.status(tb).State == ServerFailed })
	s := st.status(tb)
	if s.Starts != 5 || s.Crashes != 5 {
		t.Fatalf("status = %+v, want 5 starts and 5 crashes", s)
	}
	var item notify.Item
	for _, it := range tb.notify.Items() {
		if strings.Contains(it.Message, "crashed 5 times in the last 3 minutes") {
			item = it
		}
	}
	if item.ID == 0 || item.Severity != notify.Error || item.Source != "Fake Language Support" {
		t.Fatalf("notifications = %v", tb.notifications())
	}
	labels := []string{item.Actions[0].Label, item.Actions[1].Label}
	if !slices.Equal(labels, []string{"Restart", "Show Output"}) {
		t.Errorf("actions = %v", labels)
	}
	// Show Output shows the channel of the server
	item.Actions[1].Run()
	tb.pump()
	if !slices.Contains(tb.shownChannels, channelName) {
		t.Errorf("shown channels = %v", tb.shownChannels)
	}
	time.Sleep(100 * time.Millisecond)
	tb.pump()
	if st.status(tb).Starts != 5 {
		t.Errorf("the server was started again: %+v", st.status(tb))
	}

	// Restart forgets the crashes: with a script that works, the server runs
	st.writeScript(simple())
	item.Actions[0].Run()
	tb.pump()
	s = st.waitRunning(tb)
	if s.Starts != 6 || s.Crashes != 0 {
		t.Errorf("status after Restart = %+v", s)
	}
}

func TestCrashesThatAreMoreThanThreeMinutesApartDoNotStopTheRestarts(t *testing.T) {
	st := newServerTest(t, simple(), nil)
	tb := st.startTab("one")
	st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)
	for i := 1; i <= 7; i++ {
		crash(t, st.controller(tb))
		want := i + 1
		waitFor(t, tb, "the restart", func() bool { s := st.status(tb); return s.State == ServerRunning && s.Starts == want })
		tb.clock.advance(4 * time.Minute)
	}
	if s := st.status(tb); s.State != ServerRunning || s.Starts != 8 {
		t.Errorf("status = %+v", s)
	}
	if len(tb.notifications()) != 0 {
		t.Errorf("notifications = %v", tb.notifications())
	}
}

func TestAMissingProgramIsToldOnceAndNotRestarted(t *testing.T) {
	st := newServerTest(t, simple(), func(_ *extension.Manifest, sp *extension.LanguageServerSpec) {
		sp.Program = "emetos-no-such-language-server"
	})
	tb := st.startTab("one")
	st.goDoc(tb, "a.go", "package a\n")
	st.goDoc(tb, "b.go", "package b\n")
	if s := st.status(tb); s.State != ServerMissing || s.Starts != 0 {
		t.Fatalf("status = %+v", s)
	}
	var found []notify.Item
	for _, it := range tb.notify.Items() {
		if strings.Contains(it.Message, "emetos-no-such-language-server was not found") {
			found = append(found, it)
		}
	}
	if len(found) != 1 || found[0].Severity != notify.Error || len(found[0].Actions) != 1 {
		t.Fatalf("notifications = %v", tb.notifications())
	}
	if a := found[0].Actions[0]; a.Label != "Open Settings" || a.Command != "workbench.action.openSettings" || a.Args != testExt+".path" {
		t.Errorf("action = %+v", a)
	}
	st.goDoc(tb, "c.go", "package c\n")
	if n := len(tb.notify.Items()); n != 1 {
		t.Errorf("%d notifications after more documents: %v", n, tb.notifications())
	}
	// the user sets the path: the server starts
	tb.changeSetting(testExt+".path", st.prog)
	s := st.waitRunning(tb)
	if s.Starts != 1 {
		t.Errorf("status = %+v", s)
	}
}

func TestSettingsAreServedAndChangesAreSent(t *testing.T) {
	script := simple()
	script.Send = []lsptest.Outgoing{{
		After: lsp.MethodInitialized, Request: true, Method: lsp.MethodConfiguration,
		Params: raw(`{"items":[{"section":"fake"},{"section":"fake.ui"},{"section":"fake.ui.x"},{"section":"editor.tabSize"},{"section":"nothing.here"},{}]}`),
	}}
	st := newServerTest(t, script, nil)
	tb := st.startTab("one")
	// the section is a setting of the extension (the Go extension registers go.gopls)
	if err := st.rig.registry.Register(settings.Setting{Key: testExt + ".config", Types: []settings.Type{settings.Object}, Default: map[string]any{}, Scope: settings.ScopeWindow}); err != nil {
		t.Fatal(err)
	}
	tb.changeSetting(testExt+".config", map[string]any{"ui": map[string]any{"x": 1}, "on": true})
	st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)
	c := st.controller(tb)

	waitFor(t, tb, "the answer to workspace/configuration", func() bool { return len(responsesTo(t, c)) == 1 })
	var result []json.RawMessage
	if err := json.Unmarshal(responsesTo(t, c)[0], &result); err != nil {
		t.Fatal(err)
	}
	want := []string{`{"on":true,"ui":{"x":1}}`, `{"x":1}`, `1`, `4`, `null`, `{"on":true,"ui":{"x":1}}`}
	var got []string
	for _, r := range result {
		got = append(got, string(r))
	}
	if !slices.Equal(got, want) {
		t.Errorf("sections = %v\nwant       %v", got, want)
	}

	// a change of the section is sent as workspace/didChangeConfiguration; one of another setting is not
	tb.changeSetting("editor.tabSize", 2)
	tb.pump()
	tb.changeSetting(testExt+".config", map[string]any{"ui": map[string]any{"x": 2}})
	waitFor(t, tb, "didChangeConfiguration", func() bool { return countMethod(t, c, lsp.MethodDidChangeConfiguration) == 1 })
	for _, m := range traffic(t, c) {
		if m.Method == lsp.MethodDidChangeConfiguration {
			var p struct {
				Settings map[string]map[string]int `json:"settings"`
			}
			var body struct {
				Params json.RawMessage `json:"params"`
			}
			_ = json.Unmarshal(m.Body, &body)
			if err := json.Unmarshal(body.Params, &p); err != nil || p.Settings["ui"]["x"] != 2 {
				t.Errorf("didChangeConfiguration = %s", m.Body)
			}
		}
	}
}

// responsesTo lists the results of the responses the client sent to the requests of the server.
func responsesTo(t *testing.T, c *serverCtl) []json.RawMessage {
	t.Helper()
	var out []json.RawMessage
	for _, m := range traffic(t, c) {
		if m.FromClient && m.Kind == "response" {
			var body struct {
				Result json.RawMessage `json:"result"`
			}
			_ = json.Unmarshal(m.Body, &body)
			out = append(out, body.Result)
		}
	}
	return out
}

func countMethod(t *testing.T, c *serverCtl, method string) int {
	t.Helper()
	n := 0
	for _, m := range traffic(t, c) {
		if m.FromClient && m.Method == method {
			n++
		}
	}
	return n
}

func TestTheOutputChannelGetsTheLogsAndTheStderrOfTheServer(t *testing.T) {
	script := simple()
	script.Send = []lsptest.Outgoing{{After: lsp.MethodInitialized, Method: lsp.MethodLogMessage, Params: raw(`{"type":3,"message":"hello from the server"}`)},
		{After: lsp.MethodInitialized, Method: lsp.MethodLogMessage, Params: raw(`{"type":1,"message":"something broke"}`)}}
	st := newServerTest(t, script, func(_ *extension.Manifest, sp *extension.LanguageServerSpec) {
		// the shell writes to the standard error and then becomes the server
		sp.Args = append([]string{"-c", `echo "a line on stderr" >&2; exec "$0" "$@"`, sp.Program}, sp.Args...)
		sp.Program = "sh"
	})
	tb := st.startTab("one")
	st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)
	waitFor(t, tb, "the lines", func() bool {
		all := strings.Join(tb.channels.Lines(channelName), "\n")
		return strings.Contains(all, "hello from the server") && strings.Contains(all, "a line on stderr") && strings.Contains(all, "something broke")
	})
	all := strings.Join(tb.channels.Lines(channelName), "\n")
	for _, want := range []string{"[Info  - 00:00:00] hello from the server", "[Error - 00:00:00] something broke", "[Stderr - 00:00:00] a line on stderr", "Starting Fake LS"} {
		if !strings.Contains(all, want) {
			t.Errorf("Output lacks %q:\n%s", want, all)
		}
	}
	if !slices.Contains(tb.channels.Names(), channelName) {
		t.Errorf("channels = %v", tb.channels.Names())
	}
}

func TestTraceLevels(t *testing.T) {
	traceKey := testExt + "." + testServer + ".trace.server"
	tests := []struct {
		level          string
		wantRequest    bool
		wantBody       bool
		wantNotifyLine bool
	}{
		{"off", false, false, false},
		{"messages", true, false, true},
		{"verbose", true, true, true},
	}
	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			st := newServerTest(t, simple(), nil)
			tb := st.startTab("one")
			tb.changeSetting(traceKey, tt.level)
			st.goDoc(tb, "a.go", "package a\n")
			st.waitRunning(tb)
			// one more round trip, so that the response to initialize and the didOpen are in the log
			serverDoc(t, st.controller(tb), uriOf(filepath.Join(st.folder, "a.go")))
			all := strings.Join(tb.channels.Lines(channelName), "\n")
			has := func(s string) bool { return strings.Contains(all, s) }
			if got := has("Sending request 'initialize - (1)'.") && has("Received response 'initialize - (1)' in "); got != tt.wantRequest {
				t.Errorf("request and response traced = %v, want %v\n%s", got, tt.wantRequest, all)
			}
			if got := has("Sending notification 'textDocument/didOpen'."); got != tt.wantNotifyLine {
				t.Errorf("didOpen traced = %v, want %v", got, tt.wantNotifyLine)
			}
			if got := has(`"method":"textDocument/didOpen"`); got != tt.wantBody {
				t.Errorf("didOpen body traced = %v, want %v", got, tt.wantBody)
			}
		})
	}
}

func TestTheTraceLevelIsASettingOfTheExtension(t *testing.T) {
	st := newServerTest(t, simple(), nil)
	tb := st.startTab("one")
	key := extension.TraceSetting(testExt, st.spec)
	if key != testExt+"."+testServer+".trace.server" {
		t.Fatalf("key = %s", key)
	}
	setting, ok := st.rig.registry.Lookup(key)
	if !ok || setting.Default != "off" || !slices.Equal(setting.Enum, []any{"off", "messages", "verbose"}) {
		t.Fatalf("setting = %+v %v", setting, ok)
	}
	if _, ok := st.rig.registry.Lookup(testExt + ".path"); !ok {
		t.Error("the setting of the path of the program is not registered")
	}
	// they go with the last tab that has the extension
	tb.scope.Close()
	if _, ok := st.rig.registry.Lookup(key); ok {
		t.Error("the trace setting stays after the tab closed")
	}
}

func TestProgressShowsInTheStatusBar(t *testing.T) {
	script := simple()
	script.Send = []lsptest.Outgoing{
		{After: lsp.MethodInitialized, Request: true, Method: lsp.MethodProgressCreate, Params: raw(`{"token":"t1"}`)},
		{After: lsp.MethodInitialized, Method: lsp.MethodProgress, Params: raw(`{"token":"t1","value":{"kind":"begin","title":"Loading","message":"packages","percentage":10}}`)},
		{After: lsp.MethodInitialized, Method: lsp.MethodProgress, Params: raw(`{"token":"t1","value":{"kind":"report","message":"std","percentage":50}}`)},
		{After: lsp.MethodDidClose, Method: lsp.MethodProgress, Params: raw(`{"token":"t1","value":{"kind":"end"}}`)},
	}
	st := newServerTest(t, script, nil)
	tb := st.startTab("one")
	a := st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)
	waitFor(t, tb, "the progress", func() bool {
		item, ok := tb.bar.Get(statusID)
		return ok && item.Visible && strings.Contains(item.Entry.Text, "Loading std 50%")
	})
	item, _ := tb.bar.Get(statusID)
	if !strings.Contains(item.Entry.Text, "Fake LS") {
		t.Errorf("status text = %q", item.Entry.Text)
	}
	tb.lsp.closeDoc(a)
	waitFor(t, tb, "the end of the progress", func() bool { item, _ := tb.bar.Get(statusID); return !item.Visible })
}

func TestMessagesOfTheServerAreNotifications(t *testing.T) {
	script := simple()
	script.Send = []lsptest.Outgoing{
		{After: lsp.MethodInitialized, Method: lsp.MethodShowMessage, Params: raw(`{"type":1,"message":"bad news"}`)},
		{After: lsp.MethodInitialized, Method: lsp.MethodShowMessage, Params: raw(`{"type":2,"message":"careful"}`)},
		{After: lsp.MethodInitialized, Method: lsp.MethodShowMessage, Params: raw(`{"type":3,"message":"fyi"}`)},
		{After: lsp.MethodInitialized, Method: lsp.MethodShowMessage, Params: raw(`{"type":4,"message":"only a log"}`)},
		{After: lsp.MethodInitialized, Request: true, Method: lsp.MethodShowMessageRequest, Params: raw(`{"type":3,"message":"Pick one","actions":[{"title":"A"},{"title":"B"}]}`)},
		{After: lsp.MethodDidClose, Request: true, Method: lsp.MethodShowMessageRequest, Params: raw(`{"type":2,"message":"Or none","actions":[{"title":"C"}]}`)},
	}
	st := newServerTest(t, script, nil)
	tb := st.startTab("one")
	a := st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)
	waitFor(t, tb, "the notifications", func() bool { return len(tb.notify.Items()) >= 4 })
	sev := map[string]notify.Severity{}
	var pick notify.Item
	for _, it := range tb.notify.Items() {
		sev[it.Message] = it.Severity
		if it.Source != "Fake Language Support" {
			t.Errorf("source of %q = %q", it.Message, it.Source)
		}
		if it.Message == "Pick one" {
			pick = it
		}
	}
	want := map[string]notify.Severity{"bad news": notify.Error, "careful": notify.Warning, "fyi": notify.Info, "Pick one": notify.Info}
	for m, s := range want {
		if got, ok := sev[m]; !ok || got != s {
			t.Errorf("%q: severity %v found %v", m, got, ok)
		}
	}
	if _, ok := sev["only a log"]; ok {
		t.Error("a log message became a notification")
	}
	if !strings.Contains(strings.Join(tb.channels.Lines(channelName), "\n"), "only a log") {
		t.Error("a log message is not in the Output channel")
	}
	if len(pick.Actions) != 2 || pick.Actions[0].Label != "A" || pick.Actions[1].Label != "B" {
		t.Fatalf("actions of the request = %+v", pick.Actions)
	}

	// the user's choice is the answer
	c := st.controller(tb)
	pick.Actions[1].Run()
	waitFor(t, tb, "the answer", func() bool { return len(responsesTo(t, c)) >= 1 })
	var chosen struct{ Title string }
	for _, r := range responsesTo(t, c) {
		if err := json.Unmarshal(r, &chosen); err == nil && chosen.Title != "" {
			break
		}
	}
	if chosen.Title != "B" {
		t.Errorf("the answer = %q, want B", chosen.Title)
	}

	// a notification that is dismissed is the answer null
	tb.lsp.closeDoc(a)
	var or notify.Item
	waitFor(t, tb, "the second request", func() bool {
		for _, it := range tb.notify.Items() {
			if it.Message == "Or none" {
				or = it
			}
		}
		return or.ID != 0
	})
	tb.notify.Dismiss(or.ID)
	waitFor(t, tb, "the null answer", func() bool {
		rs := responsesTo(t, c)
		return len(rs) == 2 && string(rs[1]) == "null"
	})
}

func TestWatchedFilesAreSentToTheServer(t *testing.T) {
	script := simple()
	script.Send = []lsptest.Outgoing{{
		After: lsp.MethodInitialized, Request: true, Method: lsp.MethodRegisterCapability,
		Params: raw(`{"registrations":[{"id":"w1","method":"workspace/didChangeWatchedFiles","registerOptions":{"watchers":[
			{"globPattern":"**/*.mod","kind":5},
			{"globPattern":{"baseUri":"file://{{folder}}/sub","pattern":"*.txt"}}]}}]}`),
	}}
	st := newServerTest(t, script, func(_ *extension.Manifest, sp *extension.LanguageServerSpec) {
		sp.WatchedFiles = []string{"**/*.go"}
	})
	sub := filepath.Join(st.folder, "sub") // the relative pattern is below a folder of the tab
	tb := st.startTab("one")
	st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)
	c := st.controller(tb)
	waitFor(t, tb, "the registration", func() bool { return len(c.currentClient().Registered(lsp.MethodDidChangeWatchedFiles)) == 1 })

	f := func(name string) string { return filepath.Join(st.folder, name) }
	tb.lsp.files(
		workspace.Event{Kind: workspace.EventCreated, Path: f("new.go")},                 // the manifest's glob
		workspace.Event{Kind: workspace.EventChanged, Path: f("go.mod")},                 // registered, for created and deleted only (kind 5)
		workspace.Event{Kind: workspace.EventDeleted, Path: f("go.mod")},                 // registered
		workspace.Event{Kind: workspace.EventChanged, Path: f("notes.txt")},              // not watched
		workspace.Event{Kind: workspace.EventChanged, Path: filepath.Join(sub, "n.txt")}, // the relative pattern
		workspace.Event{Kind: workspace.EventRenamed, OldPath: f("old.go"), Path: f("moved.go")},
		workspace.Event{Kind: workspace.EventCreated, Path: filepath.Join(t.TempDir(), "outside.go")},
	)
	waitFor(t, tb, "the file events", func() bool { return countMethod(t, c, lsp.MethodDidChangeWatchedFiles) >= 1 })
	var got []string
	for _, m := range traffic(t, c) {
		if m.Method != lsp.MethodDidChangeWatchedFiles {
			continue
		}
		var body struct {
			Params lsp.DidChangeWatchedFilesParams `json:"params"`
		}
		if err := json.Unmarshal(m.Body, &body); err != nil {
			t.Fatal(err)
		}
		for _, ch := range body.Params.Changes {
			got = append(got, strings.TrimPrefix(ch.URI, "file://")+" "+[]string{"", "created", "changed", "deleted"}[ch.Type])
		}
	}
	want := []string{
		f("new.go") + " created", f("go.mod") + " deleted", filepath.Join(sub, "n.txt") + " changed",
		f("old.go") + " deleted", f("moved.go") + " created",
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("events sent:\n%v\nwant:\n%v", got, want)
	}
}

func TestRestartServerCommand(t *testing.T) {
	st := newServerTest(t, simple(), nil)
	tb := st.startTab("one")
	// the command is in the palette before the server has ever run
	id := extension.RestartServerCommand(testExt)
	if _, ok := tb.commands.Lookup(id); !ok {
		t.Fatalf("the command %s is not registered", id)
	}
	a := st.goDoc(tb, "a.go", "package a\n")
	first := st.waitRunning(tb)
	if err := tb.commands.Run(command.Context{Ctx: context.Background(), Source: command.SourcePalette}, id, nil); err != nil {
		t.Fatal(err)
	}
	second := st.waitRunning(tb)
	waitFor(t, tb, "the old server to end", func() bool { return processGone(first.PID) })
	if second.PID == first.PID || second.Starts != 2 || second.Crashes != 0 {
		t.Errorf("status after the restart = %+v (first %+v)", second, first)
	}
	if !serverDoc(t, st.controller(tb), uriOf(a.URI().Path)).Open {
		t.Error("the document was not opened on the restarted server")
	}
	// the command is gone with the extension
	st.rig.host.Shutdown()
	if _, ok := tb.commands.Lookup(id); ok {
		t.Error("the command stays after the tab closed")
	}
}

func TestTheServerCanApplyAnEditToTheWindow(t *testing.T) {
	script := simple()
	script.Send = []lsptest.Outgoing{{
		After: lsp.MethodInitialized, Request: true, Method: lsp.MethodApplyEdit,
		Params: raw(`{"label":"rename","edit":{"changes":{"file://{{folder}}/a.go":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":7}},"newText":"PACKAGE"}]}}}`),
	}}
	st := newServerTest(t, script, nil)
	tb := st.startTab("one")
	a := st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)
	c := st.controller(tb)
	waitFor(t, tb, "the edit", func() bool { return a.Text() == "PACKAGE a\n" })
	// the server was told the edit was applied, and has the new text
	waitFor(t, tb, "the response", func() bool { return len(responsesTo(t, c)) == 1 })
	var res lsp.ApplyWorkspaceEditResult
	if err := json.Unmarshal(responsesTo(t, c)[0], &res); err != nil || !res.Applied {
		t.Errorf("answer = %+v, %v", res, err)
	}
	if got := serverDoc(t, c, uriOf(a.URI().Path)).Text; got != "PACKAGE a\n" {
		t.Errorf("the server's copy = %q", got)
	}
}

func TestAnEditTheServerCannotApplyIsRefused(t *testing.T) {
	script := simple()
	script.Send = []lsptest.Outgoing{{
		After: lsp.MethodInitialized, Request: true, Method: lsp.MethodApplyEdit,
		Params: raw(`{"edit":{"changes":{"file://{{folder}}/missing.go":[{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":0}},"newText":"x"}]}}}`),
	}}
	st := newServerTest(t, script, nil)
	tb := st.startTab("one")
	st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)
	c := st.controller(tb)
	waitFor(t, tb, "the response", func() bool { return len(responsesTo(t, c)) == 1 })
	var res lsp.ApplyWorkspaceEditResult
	if err := json.Unmarshal(responsesTo(t, c)[0], &res); err != nil || res.Applied || res.FailureReason == "" {
		t.Errorf("answer = %+v, %v", res, err)
	}
	if !strings.Contains(strings.Join(tb.channels.Lines(channelName), "\n"), "The edit of the server was not applied") {
		t.Error("the refusal is not in the Output channel")
	}
}
