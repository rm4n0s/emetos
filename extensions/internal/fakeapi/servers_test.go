package fakeapi

import (
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

func TestTheFakeServersRefuseAnUnknownId(t *testing.T) {
	a := New(t, serverManifest(), extension.ScopeInfo{})
	ls := a.LanguageServers()
	_, statusErr := ls.Status("none")
	_, changeErr := ls.OnChange("none", nil)
	for name, err := range map[string]error{"Status": statusErr, "OnChange": changeErr, "Restart": ls.Restart("none"), "ShowOutput": ls.ShowOutput("none", false)} {
		t.Run(name, func(t *testing.T) { route(t, err, "API.server.FakeServerUnknown") })
	}
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
