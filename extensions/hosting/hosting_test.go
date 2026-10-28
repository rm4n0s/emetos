package hosting

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
	common "github.com/rm4n0s/emetos/extensions/internal/sharing"
)

// fakeHost is an extension that hosts workspaces, with a state the test sets.
type fakeHost struct {
	name, description, host, stop, settings string
	mu                                      sync.Mutex
	state                                   common.State
	listeners                               []func()
}

func (f *fakeHost) Name() string            { return f.name }
func (f *fakeHost) Description() string     { return f.description }
func (f *fakeHost) HostCommand() string     { return f.host }
func (f *fakeHost) StopCommand() string     { return f.stop }
func (f *fakeHost) SettingsCommand() string { return f.settings }

func (f *fakeHost) State(folder string) common.State {
	f.mu.Lock()
	defer f.mu.Unlock()
	if folder == "" {
		return common.State{}
	}
	return f.state
}

func (f *fakeHost) OnChange(fn func()) func() {
	f.mu.Lock()
	f.listeners = append(f.listeners, fn)
	f.mu.Unlock()
	return func() {}
}

// set changes the state and tells the listeners.
func (f *fakeHost) set(s common.State) {
	f.mu.Lock()
	f.state = s
	fns := append([]func(){}, f.listeners...)
	f.mu.Unlock()
	for _, fn := range fns {
		fn()
	}
}

func newHosts() (rw, ps *fakeHost) {
	rw = &fakeHost{name: "Remote Work", description: "Allowed computers see this window and control it", host: "emetos.remotework.host", stop: "emetos.remotework.stopHosting", settings: "emetos.remotework.manage"}
	ps = &fakeHost{name: "Relay", description: "Allowed computers watch this workspace's events and, if you allow it, send commands", host: "emetos.relay.host", stop: "emetos.relay.stopHosting", settings: "emetos.relay.open"}
	return rw, ps
}

// rig activates the extension over the hosts in a workspace.
type rig struct {
	api    *fakeapi.API
	rw, ps *fakeHost
	folder string
}

func newRig(t *testing.T, withFolder bool) *rig {
	t.Helper()
	rw, ps := newHosts()
	folder := ""
	if withFolder {
		folder = filepath.Join(t.TempDir(), "project")
	}
	e := NewFactory(rw, ps)()
	api := fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: folder, ID: folder})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := e.Activate(ctx, api); err != nil {
		t.Fatal(err)
	}
	return &rig{api: api, rw: rw, ps: ps, folder: folder}
}

func (r *rig) item(t *testing.T) fakeapi.StatusItem {
	t.Helper()
	r.api.Pump()
	it := r.api.StatusItem(ItemID)
	if it == nil {
		t.Fatal("the extension has no status bar item")
	}
	return *it
}

func computers(names ...string) []common.Computer {
	var out []common.Computer
	for _, n := range names {
		out = append(out, common.Computer{Name: n, Disconnect: func() {}})
	}
	return out
}

func TestTheStatusItemShowsWhatHostsTheWorkspace(t *testing.T) {
	tests := []struct {
		name     string
		folder   bool
		rw, ps   common.State
		wantText string
		wantTip  string
		kind     extension.StatusKind
	}{
		{"not hosted", true, common.State{}, common.State{}, "$(broadcast)", "This workspace is not shared. Click to host it.", extension.StatusStandard},
		{"no folder", false, common.State{}, common.State{}, "$(broadcast)", "This workspace has no folder, so it cannot be shared. Open a folder first.", extension.StatusStandard},
		{"Remote Work on, nobody connected", true, common.State{On: true, Port: 47123}, common.State{}, "$(broadcast) Hosting", "Remote Work on port 47123: nobody connected.", extension.StatusRemote},
		{"Relay on, nobody connected", true, common.State{}, common.State{On: true, Port: 47124}, "$(broadcast) Hosting", "Relay on port 47124: nobody connected.", extension.StatusRemote},
		{"Remote Work with a computer", true, common.State{On: true, Port: 47123, Computers: computers("laptop")}, common.State{}, "$(broadcast) Hosting · 1", "Remote Work on port 47123: 1 computer connected (laptop).", extension.StatusRemote},
		{"both, with computers", true,
			common.State{On: true, Port: 47123, Computers: computers("laptop")}, common.State{On: true, Port: 47124, Computers: computers("tablet", "phone")},
			"$(broadcast) Hosting · 3", "Remote Work on port 47123: 1 computer connected (laptop). Relay on port 47124: 2 computers connected (tablet, phone).", extension.StatusRemote},
		{"both, one with nobody", true,
			common.State{On: true, Port: 47123, Computers: computers("laptop")}, common.State{On: true, Port: 47124},
			"$(broadcast) Hosting · 1", "Remote Work on port 47123: 1 computer connected (laptop). Relay on port 47124: nobody connected.", extension.StatusRemote},
		{"cannot listen", true, common.State{On: true, Problem: "address already in use"}, common.State{}, "$(broadcast) Hosting", "Remote Work cannot listen: address already in use.", extension.StatusRemote},
		{"starting", true, common.State{On: true}, common.State{}, "$(broadcast) Hosting", "Remote Work is starting to listen.", extension.StatusRemote},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, tc.folder)
			r.rw.set(tc.rw)
			r.ps.set(tc.ps)
			it := r.item(t)
			if it.Text != tc.wantText || it.Tooltip != tc.wantTip || it.Kind != tc.kind {
				t.Errorf("item = %q / %q / kind %d, want %q / %q / kind %d", it.Text, it.Tooltip, it.Kind, tc.wantText, tc.wantTip, tc.kind)
			}
			if it.Alignment != extension.StatusLeft || it.Priority <= 50 {
				t.Errorf("the item is %v at priority %v, want the left end before the problems item (50)", it.Alignment, it.Priority)
			}
		})
	}
}

func TestTheItemFollowsTheHostsWhileItIsOnScreen(t *testing.T) {
	r := newRig(t, true)
	if got := r.item(t).Text; got != "$(broadcast)" {
		t.Fatalf("text = %q", got)
	}
	r.rw.set(common.State{On: true, Port: 47123})
	if got := r.item(t).Text; got != "$(broadcast) Hosting" {
		t.Errorf("text after Host = %q", got)
	}
	r.rw.set(common.State{On: true, Port: 47123, Computers: computers("laptop")})
	if got := r.item(t).Text; got != "$(broadcast) Hosting · 1" {
		t.Errorf("text with a computer = %q", got)
	}
	r.rw.set(common.State{})
	if got := r.item(t).Text; got != "$(broadcast)" {
		t.Errorf("text after Stop = %q", got)
	}
}

func TestTheMenuListsWhatTheStateAllows(t *testing.T) {
	tests := []struct {
		name   string
		rw, ps common.State
		want   []string
	}{
		{"off", common.State{}, common.State{}, []string{
			"Host with Remote Work", "Host with Relay", "Remote Work Settings...", "Relay Settings..."}},
		{"Remote Work on", common.State{On: true, Port: 1}, common.State{}, []string{
			"Stop Hosting with Remote Work", "Host with Relay", "Remote Work Settings...", "Relay Settings..."}},
		{"Relay on", common.State{}, common.State{On: true, Port: 1}, []string{
			"Host with Remote Work", "Stop Hosting with Relay", "Remote Work Settings...", "Relay Settings..."}},
		{"both on, with computers", common.State{On: true, Port: 1, Computers: computers("laptop")}, common.State{On: true, Port: 2, Computers: computers("tablet", "phone")}, []string{
			"Stop Hosting with Remote Work", "Stop Hosting with Relay", "Disconnect laptop", "Disconnect tablet", "Disconnect phone", "Remote Work Settings...", "Relay Settings..."}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, true)
			r.rw.set(tc.rw)
			r.ps.set(tc.ps)
			r.item(t)
			r.api.StatusItem(ItemID).OnClick()
			pick := r.api.LastPick()
			if pick == nil {
				t.Fatal("the click showed no menu")
			}
			var got []string
			for _, l := range pick.Labels() {
				got = append(got, strings.TrimSpace(l[strings.Index(l, ")")+1:]))
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("the menu is\n%v\nwant\n%v", got, tc.want)
			}
			for i, it := range pick.Options().Items {
				if it.Description == "" {
					t.Errorf("the row %d (%q) has no line that says what it means", i, it.Label)
				}
			}
		})
	}
}

func TestChoosingARowRunsItsCommandOrDisconnects(t *testing.T) {
	tests := []struct {
		name        string
		on          bool // both extensions host the workspace, each with a computer
		label       string
		wantCommand string
		wantDisc    string
	}{
		{"host with Remote Work", false, "Host with Remote Work", "emetos.remotework.host", ""},
		{"host with Relay", false, "Host with Relay", "emetos.relay.host", ""},
		{"stop with Remote Work", true, "Stop Hosting with Remote Work", "emetos.remotework.stopHosting", ""},
		{"stop with Relay", true, "Stop Hosting with Relay", "emetos.relay.stopHosting", ""},
		{"disconnect the first computer", true, "Disconnect laptop", "", "laptop"},
		{"disconnect the second computer", true, "Disconnect tablet", "", "tablet"},
		{"Remote Work settings", true, "Remote Work Settings...", "emetos.remotework.manage", ""},
		{"Relay settings", false, "Relay Settings...", "emetos.relay.open", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var disconnected []string
			disc := func(name string) common.Computer {
				return common.Computer{Name: name, Disconnect: func() { mu.Lock(); disconnected = append(disconnected, name); mu.Unlock() }}
			}
			r := newRig(t, true)
			if tc.on {
				r.rw.set(common.State{On: true, Port: 1, Computers: []common.Computer{disc("laptop")}})
				r.ps.set(common.State{On: true, Port: 2, Computers: []common.Computer{disc("tablet")}})
			}
			r.api.Pump()
			r.api.StatusItem(ItemID).OnClick()
			pick := r.api.LastPick()
			index := -1
			for i, l := range pick.Labels() {
				if strings.HasSuffix(l, tc.label) {
					index = i
				}
			}
			if index < 0 {
				t.Fatalf("no row %q in %v", tc.label, pick.Labels())
			}
			pick.Choose(index)
			mu.Lock()
			defer mu.Unlock()
			ran := r.api.RanCommands()
			if tc.wantCommand != "" && (len(ran) != 1 || ran[0].ID != tc.wantCommand) {
				t.Errorf("ran %+v, want %s", ran, tc.wantCommand)
			}
			if tc.wantCommand == "" && len(ran) != 0 {
				t.Errorf("a Disconnect row ran a command: %+v", ran)
			}
			if tc.wantDisc != "" && !reflect.DeepEqual(disconnected, []string{tc.wantDisc}) {
				t.Errorf("disconnected %v, want [%s]", disconnected, tc.wantDisc)
			}
			if tc.wantDisc == "" && len(disconnected) != 0 {
				t.Errorf("a computer was disconnected by %q: %v", tc.label, disconnected)
			}
		})
	}
}

func TestTheMenuCommandOpensTheMenuAndAWorkspaceWithNoFolderSaysWhy(t *testing.T) {
	r := newRig(t, true)
	if err := r.api.Run(MenuCommand, nil); err != nil {
		t.Fatal(err)
	}
	if r.api.LastPick() == nil {
		t.Error("the command opened no menu")
	}
	n := newRig(t, false)
	n.api.StatusItem(ItemID).OnClick()
	n.api.Pump()
	notes := n.api.Notes()
	if n.api.LastPick() != nil || len(notes) != 1 || notes[0].Severity != "warn" || !strings.Contains(notes[0].Message, "no folder") {
		t.Errorf("pick %v, notes %+v", n.api.LastPick(), notes)
	}
}

func TestTheExtensionHasNoItemInAWindowTab(t *testing.T) {
	rw, ps := newHosts()
	e := NewFactory(rw, ps)()
	api := fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWindowTab, Name: "Relay", ID: "tab"})
	if err := e.Activate(context.Background(), api); err != nil {
		t.Fatal(err)
	}
	if api.StatusItem(ItemID) != nil {
		t.Error("a window tab has no status bar")
	}
}

func TestManifest(t *testing.T) {
	m := (&hostingExt{}).Manifest()
	if err := extension.CheckManifest(m); err != nil {
		t.Fatal(err)
	}
	if m.ID != "emetos.hosting" || m.Name != "Sharing" || len(m.Contributes.Commands) != 1 || m.Contributes.Commands[0].ID != "emetos.hosting.menu" {
		t.Errorf("manifest = %+v", m)
	}
}
