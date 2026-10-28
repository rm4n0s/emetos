package remotework

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extension/ui/uitest"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
	"github.com/rm4n0s/glutter/gluttertest"
)

// Tests of hosting a workspace from one place (SPEC §10.29, step 17e.4): the state the Sharing item reads, the commands, the
// addresses, the guide of the page and the banner.

// hostingRig is a workspace with the extension active in it, on a hub of the rig.
type hostingRig struct {
	*rig
	api    *fakeapi.API
	ext    *remoteWork
	folder string
}

// newHostingRig activates the extension in a workspace with a folder, or with none.
func newHostingRig(t *testing.T, withFolder bool) *hostingRig {
	t.Helper()
	r := newRig(t)
	folder := ""
	if withFolder {
		folder = filepath.Join(t.TempDir(), "project")
	}
	e := &remoteWork{hub: r.hub}
	api := fakeapi.New(t, e.Manifest(), extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "project", Folder: folder, ID: folder})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	if err := e.Activate(ctx, api); err != nil {
		t.Fatal(err)
	}
	return &hostingRig{rig: r, api: api, ext: e, folder: folder}
}

// ran runs a command of the extension and what it posted, and waits for the notification it ends with.
func (h *hostingRig) ran(id string) []fakeapi.Note {
	h.t.Helper()
	if err := h.api.Run(id, nil); err != nil {
		h.t.Fatal(err)
	}
	h.api.WaitFor("the answer of "+id, func() bool { h.api.Pump(); return len(h.api.Notes()) > 0 || len(h.api.EditorTabs()) > 0 })
	return h.api.Notes()
}

func TestHostingStateTellsWhatTheSharingItemShows(t *testing.T) {
	t.Run("off, on, a client, a port that is taken", func(t *testing.T) {
		h := newHostingRig(t, true)
		host := h.hub.Hosting()
		if host.Name() != "Remote Work" || host.HostCommand() != "emetos.remotework.host" || host.StopCommand() != "emetos.remotework.stopHosting" || host.SettingsCommand() != ManageCommand {
			t.Errorf("the host says %q %q %q %q", host.Name(), host.HostCommand(), host.StopCommand(), host.SettingsCommand())
		}
		if st := host.State(h.folder); st.On || st.Port != 0 || len(st.Computers) != 0 {
			t.Errorf("off: %+v", st)
		}
		b := newKey(t)
		h.on(h.folder, b)
		st := host.State(h.folder)
		if !st.On || st.Port != portNumber(t, h.hub.Addr().String()) || len(st.Computers) != 0 || st.Problem != "" {
			t.Errorf("on: %+v", st)
		}
		// another folder is not hosted
		if other := host.State(filepath.Join(t.TempDir(), "other")); other.On {
			t.Errorf("another folder: %+v", other)
		}
		conn, err := h.client(b).connect(h.addr(), "")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = conn.Close() }()
		s := h.nextSession()
		st = host.State(h.folder)
		if len(st.Computers) != 1 || st.Computers[0].Name != "laptop B" {
			t.Fatalf("with a client: %+v", st)
		}
		changed := make(chan struct{}, 8)
		stop := host.OnChange(func() { changed <- struct{}{} })
		defer stop()
		st.Computers[0].Disconnect()
		<-s.Done()
		waitFor(t, "the session to be forgotten", func() bool { return len(host.State(h.folder).Computers) == 0 })
		<-changed
	})
	t.Run("the listener cannot open", func(t *testing.T) {
		h := newHostingRig(t, true)
		h.failTo = &netError{}
		h.on(h.folder, newKey(t))
		st := h.hub.Hosting().State(h.folder)
		if !st.On || st.Port != 0 || st.Problem == "" {
			t.Errorf("port taken: %+v", st)
		}
	})
	t.Run("a workspace with no folder is never hosted", func(t *testing.T) {
		h := newHostingRig(t, false)
		if st := h.hub.Hosting().State(""); st.On || len(st.Computers) != 0 {
			t.Errorf("no folder: %+v", st)
		}
	})
}

// netError stands for the error of a listener that cannot open.
type netError struct{}

func (*netError) Error() string { return "listen tcp :47123: bind: address already in use" }

func portNumber(t *testing.T, addr string) int {
	t.Helper()
	n, err := strconv.Atoi(addr[strings.LastIndex(addr, ":")+1:])
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestTheHostCommandsStartAndStopHosting(t *testing.T) {
	tests := []struct {
		name         string
		folder       bool
		keys         int
		on           bool // Remote Work is on before the command
		command      string
		wantOn       bool
		wantNote     string // part of the notification; empty for none
		wantPage     bool   // the page is opened
		wantSeverity string
	}{
		{"host with no key opens the page at the guide", true, 0, false, HostCommand, false, "", true, ""},
		{"host with a key turns it on", true, 1, false, HostCommand, true, "Hosting this workspace with Remote Work.", false, "info"},
		{"host when it is on says so", true, 1, true, HostCommand, true, "already hosts", false, "info"},
		{"host with no folder says so", false, 1, false, HostCommand, false, "no folder", false, "warn"},
		{"stop when it is on turns it off", true, 1, true, StopCommand, false, "Stopped hosting this workspace with Remote Work.", false, "info"},
		{"stop when it is off says so", true, 1, false, StopCommand, false, "does not host", false, "info"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHostingRig(t, tc.folder)
			for i := 0; i < tc.keys; i++ {
				if _, err := h.hub.AddKey(seededKey(t, byte(2+i)).line("laptop B")); err != nil {
					t.Fatal(err)
				}
			}
			if tc.on {
				if err := h.hub.SetWorkspace(h.folder, true); err != nil {
					t.Fatal(err)
				}
			}
			notes := h.ran(tc.command)
			if tc.wantNote == "" && len(notes) != 0 {
				t.Errorf("notifications = %+v", notes)
			}
			if tc.wantNote != "" && (len(notes) != 1 || notes[0].Severity != tc.wantSeverity || !strings.Contains(notes[0].Message, tc.wantNote)) {
				t.Errorf("notifications = %+v, want %q (%s)", notes, tc.wantNote, tc.wantSeverity)
			}
			if got := len(h.api.EditorTabs()) == 1; got != tc.wantPage {
				t.Errorf("the page is open = %v, want %v", got, tc.wantPage)
			}
			if tc.folder {
				if on := h.hub.Snapshot(h.folder).On; on != tc.wantOn {
					t.Errorf("on = %v, want %v", on, tc.wantOn)
				}
			}
		})
	}
}

func TestStopHostingEndsTheSessionsAndTheClientIsTold(t *testing.T) {
	h := newHostingRig(t, true)
	b := newKey(t)
	h.on(h.folder, b)
	conn, err := h.client(b).connect(h.addr(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	s := h.nextSession()
	h.ran(StopCommand)
	m := next(t, conn)
	d, ok := m.(*protocol.Disconnect)
	if !ok || d.Reason != protocol.ReasonOff {
		t.Fatalf("message = %#v, want a Disconnect with the reason off", m)
	}
	<-s.Done()
	if h.hub.Addr() != nil {
		t.Error("the listener is still open with nothing hosted")
	}
}

func TestTheAddressesAreTheNonLoopbackOnesOfThisComputer(t *testing.T) {
	h := newHostingRig(t, true)
	h.on(h.folder, newKey(t))
	want := "192.168.1.5:" + strconv.Itoa(portNumber(t, h.hub.Addr().String()))
	if got := h.hub.Addresses(); len(got) != 1 || got[0] != want {
		t.Errorf("Addresses = %v, want [%s]", got, want)
	}
}

func TestTheGuideShowsWhatIsDone(t *testing.T) {
	tests := []struct {
		name    string
		key, on bool
		// the numbers of the steps that are still to do: a step that is done shows a check mark in place of its number
		todo []string
	}{
		{"no key", false, false, []string{"1", "2", "3"}},
		{"a key", true, false, []string{"2", "3"}},
		{"on with no key", false, true, []string{"1", "3"}},
		{"on", true, true, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := newPageRig(t, true)
			if tc.key {
				if _, err := p.hub.AddKey(seededKey(t, 2).line("laptop B")); err != nil {
					t.Fatal(err)
				}
			}
			if tc.on {
				if err := p.hub.SetWorkspace(p.folder, true); err != nil {
					t.Fatal(err)
				}
			}
			s := p.show(t, uitest.Themes()[0], pageSize)
			s.settle()
			for _, n := range []string{"1", "2", "3"} {
				want := false
				for _, d := range tc.todo {
					want = want || d == n
				}
				if got := s.ts.Exists(gluttertest.ByText(n)); got != want {
					t.Errorf("the number %s is shown = %v, want %v", n, got, want)
				}
			}
			for _, text := range []string{"How to host this workspace", "Add the public key of the computer that will connect.", "Turn hosting on.", "On the other computer, connect to:", "192.168.1.5:", "Host key fingerprint, to compare when it asks:  SHA256:"} {
				if !s.has(text) {
					t.Errorf("the guide does not show %q", text)
				}
			}
			if s.has("127.0.0.1:") || s.has("fe80") || s.has("10.0.0.7") {
				t.Error("the guide shows an address that cannot be connected to")
			}
			if n := s.ts.Count(gluttertest.ByText("Copy")); n != 1 {
				t.Errorf("Copy buttons = %d, want 1 (the address)", n)
			}
		})
	}
}

func TestTheHostingBannerFollowsTheState(t *testing.T) {
	h := newHostingRig(t, true)
	b := newKey(t)
	hosting := func() []*fakeapi.FakeBanner {
		h.api.Pump()
		var out []*fakeapi.FakeBanner
		for _, x := range h.api.ShownBanners() {
			if x.Banner().Icon == "broadcast" {
				out = append(out, x)
			}
		}
		return out
	}
	if len(hosting()) != 0 {
		t.Fatal("a banner is shown while nothing is hosted")
	}
	h.on(h.folder, b)
	h.api.WaitFor("the hosting banner", func() bool { return len(hosting()) == 1 })
	if got := hosting()[0].Banner(); got.Text != "This workspace is shared with Remote Work (nobody connected)" || len(got.Actions) != 1 || got.Actions[0].Label != "Stop Hosting" {
		t.Errorf("banner = %+v", got)
	}
	conn, err := h.client(b).connect(h.addr(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	h.nextSession()
	h.api.WaitFor("the count", func() bool {
		x := hosting()
		return len(x) == 1 && x[0].Banner().Text == "This workspace is shared with Remote Work (1 computer connected)"
	})
	// the banner's button stops hosting
	hosting()[0].Press("Stop Hosting")
	h.api.WaitFor("the banner to go", func() bool { return len(hosting()) == 0 })
	waitFor(t, "Remote Work to be off", func() bool { return !h.hub.Snapshot(h.folder).On })
}
