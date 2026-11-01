package fakeapi

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/rm4n0s/emetos/extension"
)

func serverManifest() extension.Manifest {
	m := manifest()
	m.Contributes.LanguageServers = []extension.LanguageServerSpec{{ID: "ls", Name: "LS", Languages: []string{"go"}, Program: "ls"}}
	m.Contributes.Settings = []extension.SettingSpec{{Key: "free.key", Type: "boolean", Default: true}}
	return m
}

func TestTheFakeServersFollowTheManifest(t *testing.T) {
	a := New(t, serverManifest(), extension.ScopeInfo{})
	ls := a.LanguageServers()
	if s, err := ls.Status("ls"); err != nil || s.State != extension.ServerIdle {
		t.Fatalf("Status = %+v, %v", s, err)
	}
	var told []extension.ServerState
	cancel, err := ls.OnChange("ls", func(s extension.ServerStatus) { told = append(told, s.State) })
	if err != nil {
		t.Fatal(err)
	}
	a.SetServerStatus("ls", extension.ServerStatus{State: extension.ServerStarting})
	a.SetServerStatus("ls", extension.ServerStatus{State: extension.ServerRunning, Starts: 1, Progress: "Loading 1%"})
	cancel()
	a.SetServerStatus("ls", extension.ServerStatus{State: extension.ServerFailed})
	if !slices.Equal(told, []extension.ServerState{extension.ServerStarting, extension.ServerRunning}) {
		t.Errorf("told %v", told)
	}
	if s, _ := ls.Status("ls"); s.State != extension.ServerFailed {
		t.Errorf("Status = %+v", s)
	}
	if err := ls.Restart("ls"); err != nil {
		t.Fatal(err)
	}
	if err := ls.ShowOutput("ls", true); err != nil {
		t.Fatal(err)
	}
	if a.ServerRestarts("ls") != 1 || a.ServerOutputShown("ls") != 1 || a.ServerRestarts("none") != 0 || a.ServerOutputShown("none") != 0 {
		t.Errorf("restarts %d, shown %d", a.ServerRestarts("ls"), a.ServerOutputShown("ls"))
	}
	// the settings the manifest declares are there
	if v, err := a.Settings().Get("free.key"); err != nil || v != true {
		t.Errorf("Get = %v, %v", v, err)
	}
}

func TestTheFakeKeepsTheLauncherAndRunsItWithItsSettings(t *testing.T) {
	folder := t.TempDir()
	m := serverManifest()
	m.Contributes.Settings = append(m.Contributes.Settings, extension.SettingSpec{Key: "ls.path", Type: "string", Default: ""})
	a := New(t, m, extension.ScopeInfo{Folder: folder})
	if a.Launcher("ls") != nil || a.Launcher("none") != nil {
		t.Fatal("a launcher before SetLauncher")
	}
	var got extension.LaunchRequest
	err := a.LanguageServers().SetLauncher("ls", func(_ context.Context, req extension.LaunchRequest) (extension.ServerLaunch, error) {
		got = req
		return extension.ServerLaunch{Program: "/bin/ls-server"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	a.SetSetting("ls.path", "/opt/ls")
	launch, err := a.Launch(context.Background(), "ls")
	if err != nil || launch.Program != "/bin/ls-server" || got.Folder != folder {
		t.Fatalf("Launch = %+v, %v; request %+v", launch, err, got)
	}
	if v, ok := got.Setting("ls.path"); !ok || v != "/opt/ls" {
		t.Errorf("Setting(ls.path) = %v, %v", v, ok)
	}
	if v, ok := got.Setting("free.key"); !ok || v != true {
		t.Errorf("Setting(free.key) = %v, %v", v, ok)
	}
	if _, ok := got.Setting("none"); ok {
		t.Error("an unknown setting was read")
	}
}

func TestTheFakeServersRefuseAnUnknownId(t *testing.T) {
	a := New(t, serverManifest(), extension.ScopeInfo{})
	ls := a.LanguageServers()
	_, statusErr := ls.Status("none")
	_, changeErr := ls.OnChange("none", nil)
	launchErr := ls.SetLauncher("none", nil)
	for name, err := range map[string]error{"Status": statusErr, "OnChange": changeErr, "Restart": ls.Restart("none"), "ShowOutput": ls.ShowOutput("none", false), "SetLauncher": launchErr} {
		t.Run(name, func(t *testing.T) { route(t, err, "API.server.FakeServerUnknown") })
	}
}

func TestTheFakeQuickPickWithSeparatorsAndTypedText(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{})
	var chosen []int
	var typed []string
	opts := extension.QuickPickOptions{
		Title:    "Checkout",
		Items:    []extension.QuickPickItem{{Label: "branches", Separator: true}, {Label: "main"}, {Label: "Create", AlwaysShow: true}},
		OnAccept: func(v string) { typed = append(typed, v) },
	}
	if err := a.QuickPick().Show(opts, func(i int) { chosen = append(chosen, i) }); err != nil {
		t.Fatal(err)
	}
	a.LastPick().ChooseTyped(2, "my-topic")
	if !slices.Equal(chosen, []int{2}) || !slices.Equal(typed, []string{"my-topic"}) {
		t.Errorf("chosen %v, typed %q", chosen, typed)
	}
	if a.LastPick().Options().Title != "Checkout" {
		t.Errorf("title = %q", a.LastPick().Options().Title)
	}
	// a list of separators only is no list
	route(t, a.QuickPick().Show(extension.QuickPickOptions{Items: []extension.QuickPickItem{{Label: "heading", Separator: true}}}, nil), "quickPick.Show.FakeQuickPickInvalid")
}

func TestTheFakeQuickPick(t *testing.T) {
	a := New(t, manifest(), extension.ScopeInfo{})
	var chosen []int
	opts := extension.QuickPickOptions{Items: []extension.QuickPickItem{{Label: "One"}, {Label: "Two", Command: "x.run", Args: 7}}}
	if err := a.QuickPick().Show(opts, func(i int) { chosen = append(chosen, i) }); err != nil {
		t.Fatal(err)
	}
	p := a.LastPick()
	if !slices.Equal(p.Labels(), []string{"One", "Two"}) || p.Options().Items[1].Command != "x.run" {
		t.Fatalf("pick = %v", p.Labels())
	}
	p.Choose(1)
	if !slices.Equal(chosen, []int{1}) || len(a.RanCommands()) != 1 || a.RanCommands()[0].ID != "x.run" || a.RanCommands()[0].Args != 7 {
		t.Errorf("chosen %v, commands %v", chosen, a.RanCommands())
	}
	if err := a.QuickPick().Show(opts, func(i int) { chosen = append(chosen, i) }); err != nil {
		t.Fatal(err)
	}
	a.LastPick().Close()
	a.LastPick().Close() // the second close is ignored
	if !slices.Equal(chosen, []int{1, -1}) || len(a.Picks()) != 2 {
		t.Errorf("chosen %v, picks %d", chosen, len(a.Picks()))
	}
	route(t, a.QuickPick().Show(extension.QuickPickOptions{Items: []extension.QuickPickItem{{Description: "no label"}}}, nil), "quickPick.Show.FakeQuickPickInvalid")
	if New(t, manifest(), extension.ScopeInfo{}).LastPick() != nil {
		t.Error("a fake that showed nothing has a pick")
	}
}

func TestTheFakeRequestsOfTheServersOwnProtocol(t *testing.T) {
	a := New(t, serverManifest(), extension.ScopeInfo{})
	ls := a.LanguageServers()
	// a server that does not run answers no request
	_, err := ls.Request(context.Background(), "ls", "custom/echo", nil)
	route(t, err, "serversAPI.Request.ExtensionServerNotRunning")
	a.SetServerStatus("ls", extension.ServerStatus{State: extension.ServerRunning})
	// no handler: null
	got, err := ls.Request(context.Background(), "ls", "custom/echo", map[string]any{"a": 1})
	if err != nil || string(got) != "null" {
		t.Errorf("Request = %s, %v", got, err)
	}
	a.OnServerRequest("ls", func(req ServerRequest) (json.RawMessage, error) {
		return json.RawMessage(`{"method":"` + req.Method + `"}`), nil
	})
	got, err = ls.Request(context.Background(), "ls", "custom/other", nil)
	if err != nil || string(got) != `{"method":"custom/other"}` {
		t.Errorf("Request = %s, %v", got, err)
	}
	reqs := a.ServerRequests("ls")
	if len(reqs) != 2 || reqs[0].Method != "custom/echo" || string(reqs[0].Params) != `{"a":1}` || string(reqs[1].Params) != "null" {
		t.Errorf("requests = %+v", reqs)
	}
	_, err = ls.Request(context.Background(), "ls", "custom/echo", make(chan int))
	route(t, err, "serversAPI.Request.FakeRequestParamsInvalid")
	_, err = ls.Request(context.Background(), "none", "custom/echo", nil)
	route(t, err, "FakeServerUnknown")
	if a.ServerRequests("none") != nil {
		t.Error("requests of a server that is not there")
	}
}

func TestTheFakeNotificationsAndTheEncodingOfTheServer(t *testing.T) {
	a := New(t, serverManifest(), extension.ScopeInfo{})
	ls := a.LanguageServers()
	var got []string
	cancel, err := ls.OnNotification("ls", "custom/status", func(p json.RawMessage) { got = append(got, string(p)) })
	if err != nil {
		t.Fatal(err)
	}
	a.ServerNotify("ls", "custom/status", map[string]any{"n": 1})
	a.ServerNotify("ls", "custom/other", 2)
	cancel()
	a.ServerNotify("ls", "custom/status", map[string]any{"n": 3})
	if !slices.Equal(got, []string{`{"n":1}`}) {
		t.Errorf("heard %v", got)
	}
	_, err = ls.OnNotification("none", "custom/status", func(json.RawMessage) {})
	route(t, err, "FakeServerUnknown")
	if enc, err := ls.PositionEncoding("ls"); err != nil || enc != "utf-16" {
		t.Errorf("PositionEncoding = %q, %v", enc, err)
	}
	a.SetServerEncoding("ls", "utf-8")
	if enc, _ := ls.PositionEncoding("ls"); enc != "utf-8" {
		t.Errorf("PositionEncoding = %q", enc)
	}
	_, err = ls.PositionEncoding("none")
	route(t, err, "FakeServerUnknown")
}
