package exthost

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/internal/core/command"
	"github.com/rm4n0s/emetos/internal/core/lsp"
	"github.com/rm4n0s/emetos/internal/core/lsp/lsptest"
)

// The part of the extension API that follows and controls the language servers, the settings a manifest declares and the list an
// extension may show (step 14.1).

// recorder is the extension of a server test that follows its server: the states it was told, in order.
type recorder struct {
	states   []extension.ServerState
	statuses []extension.ServerStatus
}

// follow makes the Activate of the test extension subscribe to the server and read its status at once.
func (st *serverTest) follow(r *recorder, first *extension.ServerStatus) {
	st.onActivate = func(_ context.Context, a extension.API) error {
		got, err := a.LanguageServers().Status(testServer)
		if err != nil {
			return err
		}
		if first != nil {
			*first = got
		}
		_, err = a.LanguageServers().OnChange(testServer, func(s extension.ServerStatus) {
			r.states = append(r.states, s.State)
			r.statuses = append(r.statuses, s)
		})
		return err
	}
}

func TestAnExtensionFollowsAndRestartsItsServer(t *testing.T) {
	script := simple()
	script.Send = []lsptest.Outgoing{
		{After: lsp.MethodInitialized, Request: true, Method: lsp.MethodProgressCreate, Params: raw(`{"token":"t1"}`)},
		{After: lsp.MethodInitialized, Method: lsp.MethodProgress, Params: raw(`{"token":"t1","value":{"kind":"begin","title":"Loading","message":"packages","percentage":10}}`)},
		{After: lsp.MethodDidClose, Method: lsp.MethodProgress, Params: raw(`{"token":"t1","value":{"kind":"end"}}`)},
	}
	st := newServerTest(t, script, nil)
	var rec recorder
	var first extension.ServerStatus
	st.follow(&rec, &first)
	tb := st.startTab("one")
	// the status of a server that the host has not made yet (the extension is activating) is idle
	if first.State != extension.ServerIdle || first.Starts != 0 {
		t.Errorf("status during Activate = %+v", first)
	}
	a := st.goDoc(tb, "a.go", "package a\n")
	waitFor(t, tb, "the progress to show", func() bool {
		s, _ := st.api.LanguageServers().Status(testServer)
		return s.State == extension.ServerRunning && s.Progress == "Loading packages 10%"
	})
	if !slices.Contains(rec.states, extension.ServerStarting) || !slices.Contains(rec.states, extension.ServerRunning) {
		t.Errorf("the extension was told %v, want starting and then running", rec.states)
	}
	if rec.statuses[len(rec.statuses)-1].Progress != "Loading packages 10%" {
		t.Errorf("the last status = %+v", rec.statuses[len(rec.statuses)-1])
	}
	tb.lsp.closeDoc(a)
	waitFor(t, tb, "the end of the progress", func() bool {
		s, _ := st.api.LanguageServers().Status(testServer)
		return s.Progress == ""
	})
	st.goDoc(tb, "b.go", "package b\n")

	// Restart stops the server and starts it again
	before := len(rec.states)
	firstHost := st.status(tb)
	if err := st.api.LanguageServers().Restart(testServer); err != nil {
		t.Fatal(err)
	}
	second := st.waitRunning(tb)
	waitFor(t, tb, "the old server to end", func() bool { return processGone(firstHost.PID) })
	if second.Starts != 2 || second.PID == firstHost.PID {
		t.Errorf("status after the restart = %+v (before %+v)", second, firstHost)
	}
	if got := rec.states[before:]; !slices.Contains(got, extension.ServerStarting) || got[len(got)-1] != extension.ServerRunning {
		t.Errorf("states during the restart = %v", got)
	}

	// ShowOutput shows the channel of the server
	if err := st.api.LanguageServers().ShowOutput(testServer, true); err != nil {
		t.Fatal(err)
	}
	tb.pump()
	if !slices.Contains(tb.shownChannels, channelName) {
		t.Errorf("shown channels = %v, want %s", tb.shownChannels, channelName)
	}

	// the end of the activation: no more calls, and Restart fails
	api := st.api
	st.rig.host.Shutdown()
	n := len(rec.states)
	tb.pump()
	if len(rec.states) != n {
		t.Errorf("the extension was told %v after it ended", rec.states[n:])
	}
	wantRoute(t, api.LanguageServers().Restart(testServer), "serversAPI.Restart.ExtensionDeactivated")
}

func TestTheServerAPIRefusesAnIdTheManifestDoesNotDeclare(t *testing.T) {
	st := newServerTest(t, simple(), nil)
	st.startTab("one")
	ls := st.api.LanguageServers()
	_, statusErr := ls.Status("nothing")
	_, changeErr := ls.OnChange("nothing", func(extension.ServerStatus) {})
	tests := []struct {
		name  string
		err   error
		route string
	}{
		{"Status", statusErr, "serversAPI.Status.ExtensionServerUnknown"},
		{"OnChange", changeErr, "serversAPI.OnChange.ExtensionServerUnknown"},
		{"Restart", ls.Restart("nothing"), "serversAPI.Restart.ExtensionServerUnknown"},
		{"ShowOutput", ls.ShowOutput("nothing", false), "serversAPI.ShowOutput.ExtensionServerUnknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { wantRoute(t, tt.err, tt.route) })
	}
}

func TestAServerWithItsOwnUIHasNoStatusItemAndNoMissingNotification(t *testing.T) {
	st := newServerTest(t, simple(), func(_ *extension.Manifest, sp *extension.LanguageServerSpec) {
		sp.Program = "emetos-no-such-language-server"
		sp.OwnUI = true
	})
	var rec recorder
	st.follow(&rec, nil)
	tb := st.startTab("one")
	st.goDoc(tb, "a.go", "package a\n")
	if s := st.status(tb); s.State != ServerMissing {
		t.Fatalf("status = %+v", s)
	}
	if !slices.Contains(rec.states, extension.ServerMissing) {
		t.Errorf("the extension was told %v, want missing", rec.states)
	}
	if got := tb.notifications(); len(got) != 0 {
		t.Errorf("notifications = %v, want none: the extension tells the user", got)
	}
	if _, ok := tb.bar.Get(statusID); ok {
		t.Error("the host made a status item for a server with its own UI")
	}
	// the Output channel still says it
	if lines := strings.Join(tb.channels.Lines(channelName), "\n"); !strings.Contains(lines, "emetos-no-such-language-server was not found") {
		t.Errorf("the channel has %q", lines)
	}
	// the user sets the path: the server starts, and the state follows
	tb.changeSetting(testExt+".path", st.prog)
	st.waitRunning(tb)
	if rec.states[len(rec.states)-1] != extension.ServerRunning {
		t.Errorf("states = %v", rec.states)
	}
	// the crash notification is still the host's: stopped servers are not told
	if tb.scope.State(testExt) != StateActive {
		t.Errorf("the extension is %v", tb.scope.State(testExt))
	}
}

func TestTheRestartCommandIsInTheCategoryOfTheServer(t *testing.T) {
	tests := []struct {
		name     string
		category string
		want     string
	}{
		{"the name of the extension", "", "Fake Language Support"},
		{"a category of the server", "Fake", "Fake"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newServerTest(t, simple(), func(_ *extension.Manifest, sp *extension.LanguageServerSpec) { sp.Category = tt.category })
			tb := st.startTab("one")
			meta, ok := tb.commands.Lookup(extension.RestartServerCommand(testExt))
			if !ok || meta.Title != "Restart Language Server" || meta.Category != tt.want {
				t.Errorf("command = %+v, %v; want the category %q", meta, ok, tt.want)
			}
		})
	}
}

func TestACallbackThatPanicsFailsTheExtension(t *testing.T) {
	tests := []struct {
		name  string
		setup func(st *serverTest)
		route string
		tag   string
		fire  func(st *serverTest, tb *tab)
	}{
		{
			name: "a listener of the server",
			setup: func(st *serverTest) {
				st.onActivate = func(_ context.Context, a extension.API) error {
					_, err := a.LanguageServers().OnChange(testServer, func(extension.ServerStatus) { panic("boom") })
					return err
				}
			},
			route: "Scope.runServerListener.func1.ExtensionServerCallbackPanicked", tag: "ExtensionServerCallbackPanicked",
			fire: func(st *serverTest, tb *tab) { st.goDoc(tb, "a.go", "package a\n") },
		},
		{
			name: "the end of a quick pick",
			setup: func(st *serverTest) {
				st.onActivate = func(_ context.Context, a extension.API) error {
					return a.QuickPick().Show(extension.QuickPickOptions{Items: []extension.QuickPickItem{{Label: "A"}}}, func(int) { panic("boom") })
				}
			},
			route: "Scope.runQuickPickDone.func1.ExtensionQuickPickCallbackPanicked", tag: "ExtensionQuickPickCallbackPanicked",
			fire: func(_ *serverTest, tb *tab) { tb.picks[0].Chosen(0) },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := newServerTest(t, simple(), nil)
			tt.setup(st)
			tb := st.startTab("one")
			tt.fire(st, tb)
			tb.pump()
			waitFor(t, tb, "the extension to fail", func() bool { return tb.scope.State(testExt) == StateFailed })
			fl := tb.scope.Failure(testExt)
			if fl == nil {
				t.Fatal("no failure recorded")
			}
			appErr, ok := errors.FromError(fl.Err)
			if !ok || appErr.Tag != tt.tag || !appErr.HasRoute(tt.route) {
				t.Errorf("failure = %v, want tag %s and route %s", fl.Err, tt.tag, tt.route)
			}
		})
	}
}

func TestSettingsTheManifestDeclaresAreRegisteredBeforeTheActivation(t *testing.T) {
	script := simple()
	script.Send = []lsptest.Outgoing{{
		After: lsp.MethodInitialized, Request: true, Method: lsp.MethodConfiguration,
		Params: raw(`{"items":[{"section":"` + testExt + `.config"}]}`),
	}}
	var def float64 = 3
	st := newServerTest(t, script, func(m *extension.Manifest, _ *extension.LanguageServerSpec) {
		m.Contributes.Settings = []extension.SettingSpec{
			{Key: testExt + ".config", Type: "object", Default: map[string]any{"a": 1, "b": 2}, Description: "The config."},
			{Key: "outside.limit", Type: "number", Default: 5, Minimum: &def, Description: "A key that is not under the id of the extension.", Category: "Outside"},
			{Key: "editor.tabSize", Type: "number", Default: 9}, // taken by the editor: left out
		}
	})
	tb := st.startTab("one")
	cfg, ok := st.rig.registry.Lookup(testExt + ".config")
	if !ok || cfg.Description != "The config." || cfg.Category != "Fake Language Support" {
		t.Errorf("config = %+v, %v", cfg, ok)
	}
	out, ok := st.rig.registry.Lookup("outside.limit")
	if !ok || out.Category != "Outside" || out.Minimum == nil || *out.Minimum != 3 {
		t.Errorf("outside.limit = %+v, %v", out, ok)
	}
	if tab, _ := st.rig.registry.Lookup("editor.tabSize"); tab.Default == float64(9) || tab.Default == 9 {
		t.Errorf("the editor's setting was replaced: %+v", tab)
	}
	if !strings.Contains(tb.log.String(), "a setting an extension declares is left out") {
		t.Errorf("the log does not say that a setting was left out:\n%s", tb.log.String())
	}
	// the extension reads them
	got, err := st.api.Settings().Get("outside.limit")
	if err != nil || got != float64(5) {
		t.Errorf("Get = %v, %v", got, err)
	}
	// and the server is served the section: the default with the user's values on top
	tb.changeSetting(testExt+".config", map[string]any{"b": 20, "c": 30})
	st.goDoc(tb, "a.go", "package a\n")
	st.waitRunning(tb)
	c := st.controller(tb)
	waitFor(t, tb, "the answer to workspace/configuration", func() bool { return len(responsesTo(t, c)) >= 1 })
	var result []json.RawMessage
	if err := json.Unmarshal(responsesTo(t, c)[0], &result); err != nil {
		t.Fatal(err)
	}
	if len(result) != 1 || string(result[0]) != `{"b":20,"c":30}` && string(result[0]) != `{"a":1,"b":20,"c":30}` {
		t.Errorf("the server got %s", result)
	}
	// they go with the last tab that has the extension
	tb.scope.Close()
	if _, ok := st.rig.registry.Lookup("outside.limit"); ok {
		t.Error("the declared setting stays after the scope closed")
	}
}

func TestQuickPickShowsAListAndReportsTheChoice(t *testing.T) {
	st := newServerTest(t, simple(), nil)
	var commandsRun []string
	tb := st.startTab("one")
	// a command the item runs
	if err := tb.commands.Register(command.Meta{ID: "x.run"}); err != nil {
		t.Fatal(err)
	}
	remove, err := tb.commands.SetHandler("x.run", func(_ command.Context, args any) error {
		commandsRun = append(commandsRun, args.(string))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer remove()

	var chosen []int
	done := func(i int) { chosen = append(chosen, i) }
	opts := extension.QuickPickOptions{
		Placeholder: "Pick", MatchOnDescription: true,
		Items: []extension.QuickPickItem{
			{Label: "$(sync) One", Description: "first"},
			{Label: ""}, // not shown
			{Label: "Two", Detail: "second", Command: "x.run", Args: "with-args"},
		},
	}
	if err := st.api.QuickPick().Show(opts, done); err != nil {
		t.Fatal(err)
	}
	req := tb.picks[0]
	if req.Placeholder != "Pick" || !req.MatchOnDescription || len(req.Items) != 2 || req.Items[0].Label != "$(sync) One" || req.Items[1].Detail != "second" {
		t.Fatalf("request = %+v", req)
	}
	// the index is that of the extension's own list; the command runs before the extension hears of the choice
	req.Chosen(1)
	if !slices.Equal(chosen, []int{2}) || !slices.Equal(commandsRun, []string{"with-args"}) {
		t.Errorf("chosen = %v, commands run = %v", chosen, commandsRun)
	}
	// a second answer is ignored
	req.Hidden()
	req.Chosen(0)
	if !slices.Equal(chosen, []int{2}) {
		t.Errorf("chosen = %v after more answers", chosen)
	}

	// closed with no choice
	chosen = nil
	if err := st.api.QuickPick().Show(opts, done); err != nil {
		t.Fatal(err)
	}
	tb.picks[1].Hidden()
	if !slices.Equal(chosen, []int{-1}) {
		t.Errorf("chosen = %v, want -1", chosen)
	}
	// an index the list does not have counts as no choice
	chosen = nil
	if err := st.api.QuickPick().Show(opts, done); err != nil {
		t.Fatal(err)
	}
	tb.picks[2].Chosen(7)
	if !slices.Equal(chosen, []int{-1}) {
		t.Errorf("chosen = %v, want -1", chosen)
	}
	// no function to call is fine
	if err := st.api.QuickPick().Show(opts, nil); err != nil {
		t.Fatal(err)
	}
	tb.picks[3].Chosen(0)
}

func TestQuickPickRefusals(t *testing.T) {
	st := newServerTest(t, simple(), nil)
	tb := st.startTab("one")
	api := st.api
	wantRoute(t, api.QuickPick().Show(extension.QuickPickOptions{Items: []extension.QuickPickItem{{Description: "no label"}}}, nil), "quickPickAPI.Show.ExtensionQuickPickInvalid")
	wantRoute(t, api.QuickPick().Show(extension.QuickPickOptions{}, nil), "quickPickAPI.Show.ExtensionQuickPickInvalid")
	if len(tb.picks) != 0 {
		t.Errorf("a refused list was shown: %v", tb.picks)
	}
	// a tab that cannot show a list
	other := newServerTest(t, simple(), nil)
	other.rig.noPick["one"] = true
	other.startTab("one")
	wantRoute(t, other.api.QuickPick().Show(extension.QuickPickOptions{Items: []extension.QuickPickItem{{Label: "A"}}}, nil), "quickPickAPI.Show.ExtensionQuickPickUnavailable")
	// after the activation
	st.rig.host.Shutdown()
	wantRoute(t, api.QuickPick().Show(extension.QuickPickOptions{Items: []extension.QuickPickItem{{Label: "A"}}}, nil), "quickPickAPI.Show.ExtensionDeactivated")
}
