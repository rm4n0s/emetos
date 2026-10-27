package remotework

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
	"github.com/rm4n0s/emetos/extensions/internal/sshlogin"
	"github.com/rm4n0s/emetos/extensions/remotework/login"
	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
)

// on turns Remote Work on for a workspace and lists the key.
func (r *rig) on(folder string, keys ...testKey) {
	r.t.Helper()
	for _, k := range keys {
		if _, err := r.hub.AddKey(k.line("laptop B")); err != nil {
			r.t.Fatal(err)
		}
	}
	if err := r.hub.SetWorkspace(folder, true); err != nil {
		r.t.Fatal(err)
	}
}

func notes(api *fakeapi.API) []fakeapi.Note {
	api.Pump()
	return api.Notes()
}

func TestLoginGrantsASession(t *testing.T) {
	r := newRig(t)
	api, folder := r.workspace("project")
	b := newKey(t)
	r.on(folder, b)
	c := r.client(b)
	conn, err := c.connect(r.addr(), "")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer func() { _ = conn.Close() }()
	g := conn.Grant
	if g.Status != login.StatusOK || g.Workspace != "project" || len(g.Token) != 32 || !bytes.Equal(g.CertSHA256, r.certFP) {
		t.Fatalf("grant = %+v", g)
	}
	s := r.nextSession()
	if !bytes.Equal(s.Token(), g.Token) || s.Key.Comment != "laptop B" || s.Workspace != "project" {
		t.Errorf("session = %+v", s)
	}
	if found, ok := r.hub.SessionByToken(g.Token); !ok || found != s {
		t.Error("SessionByToken does not find the live session")
	}
	if _, ok := r.hub.SessionByToken(make([]byte, 32)); ok {
		t.Error("SessionByToken found a token nobody has")
	}
	if got := r.hub.Sessions(); len(got) != 1 {
		t.Errorf("Sessions = %d", len(got))
	}

	// the banner and the notification of the login are shown in the workspace
	api.WaitFor("the banner", func() bool { return len(api.ShownBanners()) == 1 })
	banner := api.ShownBanners()[0]
	if b := banner.Banner(); b.Text != "Remote Work: laptop B connected" || b.Icon != "remote" || len(b.Actions) != 1 || b.Actions[0].Label != "Disconnect" {
		t.Errorf("banner = %+v", b)
	}
	got := notes(api)
	if len(got) != 1 || got[0].Severity != "info" || !strings.Contains(got[0].Message, "laptop B logged in from 127.0.0.1") {
		t.Errorf("notifications = %+v", got)
	}

	// Disconnect on the banner ends the session: the client is told why, the token expires, the banner goes
	banner.Press("Disconnect")
	m := next(t, conn)
	d, ok := m.(*protocol.Disconnect)
	if !ok || d.Reason != protocol.ReasonHost {
		t.Fatalf("message = %#v, want a Disconnect from the host", m)
	}
	waitFor(t, "the token to expire", func() bool { _, ok := r.hub.SessionByToken(g.Token); return !ok })
	api.WaitFor("the banner to go", func() bool { return len(api.ShownBanners()) == 0 })
	if reason, _, ended := s.Reason(); !ended || reason != protocol.ReasonHost {
		t.Errorf("Reason = %v, %v", reason, ended)
	}
	select {
	case <-s.Done():
	default:
		t.Error("Done is not closed")
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestSessionWithoutQUIC(t *testing.T) {
	r := newRig(t, rigOptions{cert: func() []byte { return nil }})
	_, folder := r.workspace("project")
	b := newKey(t)
	r.on(folder, b)
	conn, err := r.client(b).connect(r.addr(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if len(conn.Grant.CertSHA256) != 0 {
		t.Errorf("a fingerprint was sent although there is no certificate: %x", conn.Grant.CertSHA256)
	}
}

func TestListensOnlyWhileAWorkspaceHasItOn(t *testing.T) {
	r := newRig(t)
	api, folder := r.workspace("project")
	b := newKey(t)
	if _, err := r.hub.AddKey(b.line("B")); err != nil {
		t.Fatal(err)
	}
	if r.hub.Addr() != nil || len(r.listens) != 0 {
		t.Fatal("it listens although no workspace has Remote Work on")
	}
	if err := r.hub.SetWorkspace(folder, true); err != nil {
		t.Fatal(err)
	}
	addr := r.addr()
	if len(r.listens) != 1 || r.listens[0] != DefaultPort {
		t.Errorf("listened on %v, want the default port %d", r.listens, DefaultPort)
	}
	conn, err := r.client(b).connect(addr, "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	r.nextSession()
	snap := r.hub.Snapshot(folder)
	if !snap.On || !snap.Listening || snap.Addr != addr || len(snap.Sessions) != 1 {
		t.Errorf("snapshot = %+v", snap)
	}

	// off: the sessions end with the reason, the listener closes
	if err := r.hub.SetWorkspace(folder, false); err != nil {
		t.Fatal(err)
	}
	if m, ok := next(t, conn).(*protocol.Disconnect); !ok || m.Reason != protocol.ReasonOff {
		t.Errorf("the client was not told that Remote Work is off: %#v", m)
	}
	if r.hub.Addr() != nil {
		t.Error("the listener is still open")
	}
	if c, err := net.DialTimeout("tcp", addr, 500*time.Millisecond); err == nil {
		_ = c.Close()
		t.Error("the port still accepts connections")
	}
	api.WaitFor("the banner to go", func() bool { return len(api.ShownBanners()) == 0 })
	if _, ok := r.hub.SessionByToken(conn.Grant.Token); ok {
		t.Error("the token is still valid")
	}
	if snap := r.hub.Snapshot(folder); snap.On || snap.Listening {
		t.Errorf("snapshot = %+v", snap)
	}
}

func TestClosingTheWorkspaceEndsItsSessions(t *testing.T) {
	r := newRig(t)
	_, folder := r.workspace("project")
	b := newKey(t)
	r.on(folder, b)
	conn, err := r.client(b).connect(r.addr(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	r.nextSession()
	// the extension is deactivated in the workspace (it closes): the registration is undone
	r.unregister[folder]()
	if m, ok := next(t, conn).(*protocol.Disconnect); !ok || m.Reason != protocol.ReasonWorkspaceClosed {
		t.Errorf("message = %#v, want the workspace closed", m)
	}
	waitFor(t, "the listener to close", func() bool { return r.hub.Addr() == nil })
}

// TestTwoInstancesOfOneFolder: a folder open in two window tabs keeps the sessions until both are gone.
func TestTwoInstancesOfOneFolder(t *testing.T) {
	r := newRig(t)
	api, folder := r.workspace("project")
	b := newKey(t)
	r.on(folder, b)
	second := r.hub.Register(api)
	conn, err := r.client(b).connect(r.addr(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	s := r.nextSession()
	second()
	second() // twice does nothing more
	select {
	case <-s.Done():
		t.Fatal("the session ended although an instance is left")
	case <-time.After(100 * time.Millisecond):
	}
	r.unregister[folder]()
	<-s.Done()
}

// TestAWorkspaceWithoutAFolderIsNotRegistered: Remote Work names workspaces by their folder.
func TestAWorkspaceWithoutAFolderIsNotRegistered(t *testing.T) {
	r := newRig(t)
	api := fakeapi.New(t, extension.Manifest{ID: ID, Name: "Remote Work"}, extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: "No Folder"})
	r.hub.Register(api)()
	if len(r.hub.workspaces) != 0 {
		t.Errorf("workspaces = %v", r.hub.workspaces)
	}
	wantRoute(t, r.hub.SetWorkspace("", true), "func1.RemoteWorkSettingInvalid")
}

func TestWorkspaceNames(t *testing.T) {
	r := newRig(t)
	_, a := r.workspace("alpha")
	_, b := r.workspace("beta")
	k := newKey(t)
	r.on(a, k)
	r.on(b)
	c := r.client(k)

	// two workspaces are on: the client must say which
	_, err := c.connect(r.addr(), "")
	wantRoute(t, err, "Connect.RemoteWorkLoginDenied")
	if !strings.Contains(err.Error(), "several workspaces") {
		t.Errorf("error = %v", err)
	}
	conn, err := c.connect(r.addr(), "beta")
	if err != nil || conn.Grant.Workspace != "beta" {
		t.Fatalf("connect to beta: %v %+v", err, conn)
	}
	_ = conn.Close()
	_, err = c.connect(r.addr(), "gamma")
	wantRoute(t, err, "Connect.RemoteWorkLoginDenied")
	if !strings.Contains(err.Error(), "does not have Remote Work on") {
		t.Errorf("error = %v", err)
	}
	// one off: the only one needs no name
	if err := r.hub.SetWorkspace(b, false); err != nil {
		t.Fatal(err)
	}
	conn, err = c.connect(r.addr(), "")
	if err != nil || conn.Grant.Workspace != "alpha" {
		t.Fatalf("connect to the only one: %v", err)
	}
	_ = conn.Close()
}

func TestWorkspacesWithTheSameNameAreToldApartByTheirFolder(t *testing.T) {
	r := newRig(t)
	_, a := r.workspace("app")
	_, b := r.workspace("app")
	k := newKey(t)
	r.on(a, k)
	r.on(b)
	_, err := r.client(k).connect(r.addr(), "")
	wantRoute(t, err, "Connect.RemoteWorkLoginDenied")
	conn, err := r.client(k).connect(r.addr(), b)
	if err != nil || conn.Grant.Workspace != b {
		t.Fatalf("connect by folder: %v", err)
	}
	_ = conn.Close()
}

// memChannel is an SSH channel in memory: what the host writes is read back with ReadGrant.
type memChannel struct{ out bytes.Buffer }

func (c *memChannel) Read([]byte) (int, error)    { return 0, io.EOF }
func (c *memChannel) Write(b []byte) (int, error) { return c.out.Write(b) }
func (c *memChannel) Close() error                { return nil }
func (c *memChannel) CloseWrite() error           { return nil }
func (c *memChannel) SendRequest(string, bool, []byte) (bool, error) {
	return false, nil
}
func (c *memChannel) Stderr() io.ReadWriter { return &c.out }

func mustRequest(t *testing.T, workspace string) []byte {
	t.Helper()
	b, err := login.Request{Workspace: workspace}.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestNoWorkspaceIsOn: a login that reaches the server just as the last workspace is turned off is told so.
func TestNoWorkspaceIsOn(t *testing.T) {
	r := newRig(t)
	remote := &net.TCPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 1}
	ch := &memChannel{}
	r.hub.serve(&sshlogin.Session{Channel: ch, ExtraData: mustRequest(t, ""), Remote: remote})
	g, err := login.ReadGrant(&ch.out)
	if err != nil || g.Status != login.StatusNone || len(g.Workspaces) != 0 {
		t.Errorf("grant = %+v, %v", g, err)
	}
	// a request that is not one gets nothing
	bad := &memChannel{}
	r.hub.serve(&sshlogin.Session{Channel: bad, ExtraData: []byte{9}, Remote: remote})
	if bad.out.Len() != 0 {
		t.Errorf("a bad request got %d bytes", bad.out.Len())
	}
}

func TestANewLoginToTheSameWorkspaceReplacesTheOld(t *testing.T) {
	r := newRig(t)
	_, folder := r.workspace("project")
	b := newKey(t)
	r.on(folder, b)
	first, err := r.client(b).connect(r.addr(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.Close() }()
	s1 := r.nextSession()
	second, err := r.client(b).connect(r.addr(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.Close() }()
	r.nextSession()
	if m, ok := next(t, first).(*protocol.Disconnect); !ok || m.Reason != protocol.ReasonReplaced {
		t.Errorf("the first client was not told it was replaced: %#v", m)
	}
	if _, ok := r.hub.SessionByToken(first.Grant.Token); ok {
		t.Error("the token of the replaced session is valid")
	}
	if _, ok := r.hub.SessionByToken(second.Grant.Token); !ok {
		t.Error("the new session has no token")
	}
	<-s1.Done()
}

func TestRemovingAKeyEndsItsSessions(t *testing.T) {
	r := newRig(t)
	_, folder := r.workspace("project")
	b, other := newKey(t), newKey(t)
	r.on(folder, b, other)
	conn, err := r.client(b).connect(r.addr(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	r.nextSession()
	if err := r.hub.RemoveKey(other.fingerprint()); err != nil {
		t.Fatal(err)
	}
	if len(r.hub.Sessions()) != 1 {
		t.Fatal("the session of another key ended")
	}
	if err := r.hub.RemoveKey(b.fingerprint()); err != nil {
		t.Fatal(err)
	}
	if m, ok := next(t, conn).(*protocol.Disconnect); !ok || m.Reason != protocol.ReasonHost {
		t.Errorf("message = %#v", m)
	}
	// and the key cannot log in again
	_, err = r.client(b).connect(r.addr(), "")
	wantRoute(t, err, "dial.SSHLoginAuthFailed")
}

func TestRefusedAttemptsRaiseOneNotificationAMinute(t *testing.T) {
	r := newRig(t)
	api, folder := r.workspace("project")
	r.on(folder, newKey(t))
	stranger := newKey(t)
	sc := r.client(stranger)
	sc.pin(r)
	for i := 0; i < 2; i++ {
		if _, err := sc.connect(r.addr(), ""); err == nil {
			t.Fatal("a stranger logged in")
		}
	}
	waitFor(t, "the notification", func() bool { return len(notes(api)) >= 1 })
	time.Sleep(100 * time.Millisecond)
	got := notes(api)
	if len(got) != 1 || got[0].Severity != "warn" || !strings.Contains(got[0].Message, "refused a login from 127.0.0.1") ||
		!strings.Contains(got[0].Message, "its key is not in the list ("+stranger.fingerprint()+")") {
		t.Fatalf("notifications = %+v", got)
	}
	// a minute later the same address raises another
	r.clock.Advance(time.Minute + time.Second)
	if _, err := sc.connect(r.addr(), ""); err == nil {
		t.Fatal("a stranger logged in")
	}
	waitFor(t, "the second notification", func() bool { return len(notes(api)) == 2 })
}

func TestTheRateLimit(t *testing.T) {
	r := newRig(t)
	api, folder := r.workspace("project")
	listed := newKey(t)
	r.on(folder, listed)
	stranger := newKey(t)
	sc := r.client(stranger)
	sc.pin(r)
	for i := 0; i < 5; i++ {
		if _, err := sc.connect(r.addr(), ""); err == nil {
			t.Fatal("a stranger logged in")
		}
		r.clock.Advance(5 * time.Second)
	}
	// the sixth connection is closed before any login, even with the listed key
	lc := r.client(listed)
	lc.pin(r)
	_, err := lc.connect(r.addr(), "")
	wantRoute(t, err, "dial.SSHLoginConnectFailed")
	waitFor(t, "the rate limit notification", func() bool {
		for _, n := range notes(api) {
			if strings.Contains(n.Message, "blocked for a minute") {
				return true
			}
		}
		return false
	})
	r.clock.Advance(2 * time.Minute)
	conn, err := lc.connect(r.addr(), "")
	if err != nil {
		t.Fatalf("after the minute: %v", err)
	}
	_ = conn.Close()
}

func TestIdleTimeout(t *testing.T) {
	r := newRig(t)
	_, folder := r.workspace("project")
	b := newKey(t)
	r.on(folder, b)
	if err := r.hub.SetIdleMinutes(2); err != nil {
		t.Fatal(err)
	}
	conn, err := r.client(b).connect(r.addr(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	s := r.nextSession()
	r.clock.Advance(100 * time.Second)
	s.Touch() // traffic: the clock of the timeout starts again
	r.clock.Advance(100 * time.Second)
	select {
	case <-s.Done():
		t.Fatal("the session ended although it had traffic 100 seconds ago")
	case <-time.After(50 * time.Millisecond):
	}
	r.clock.Advance(30 * time.Second)
	if m, ok := next(t, conn).(*protocol.Disconnect); !ok || m.Reason != protocol.ReasonIdle {
		t.Errorf("message = %#v, want the idle timeout", m)
	}
	if _, ok := r.hub.SessionByToken(conn.Grant.Token); ok {
		t.Error("the token outlived the idle timeout")
	}
	s.Touch() // after the end: nothing happens
}

func TestSessionsEndWhenTheClientLeaves(t *testing.T) {
	r := newRig(t)
	api, folder := r.workspace("project")
	b := newKey(t)
	r.on(folder, b)
	conn, err := r.client(b).connect(r.addr(), "")
	if err != nil {
		t.Fatal(err)
	}
	s := r.nextSession()
	api.WaitFor("the banner", func() bool { return len(api.ShownBanners()) == 1 })
	_ = conn.Close()
	<-s.Done()
	waitFor(t, "the session to be forgotten", func() bool { return len(r.hub.Sessions()) == 0 })
	api.WaitFor("the banner to go", func() bool { return len(api.ShownBanners()) == 0 })
	if reason, _, ended := s.Reason(); !ended || reason != protocol.ReasonClosed {
		t.Errorf("Reason = %v, %v", reason, ended)
	}
}

func TestPortChangeMovesTheListener(t *testing.T) {
	r := newRig(t)
	_, folder := r.workspace("project")
	b := newKey(t)
	r.on(folder, b)
	conn, err := r.client(b).connect(r.addr(), "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	r.nextSession()
	if err := r.hub.SetPort(50001); err != nil {
		t.Fatal(err)
	}
	if m, ok := next(t, conn).(*protocol.Disconnect); !ok || m.Reason != protocol.ReasonOff {
		t.Errorf("message = %#v", m)
	}
	if got := r.listens; len(got) != 2 || got[1] != 50001 {
		t.Errorf("listened on %v, want the new port second", got)
	}
	if r.hub.Addr() == nil {
		t.Error("the listener did not move")
	}
	// the same port again changes nothing
	if err := r.hub.SetPort(50001); err != nil || len(r.listens) != 2 {
		t.Errorf("SetPort of the same port: %v, listens %v", err, r.listens)
	}
}

func TestListenFailureIsShown(t *testing.T) {
	r := newRig(t)
	api, folder := r.workspace("project")
	r.failTo = &net.OpError{Op: "listen", Err: os.ErrExist}
	r.on(folder, newKey(t))
	snap := r.hub.Snapshot(folder)
	if snap.Listening || snap.ListenErr == "" {
		t.Errorf("snapshot = %+v", snap)
	}
	wantRoute(t, r.hub.listenErr, "Hub.startListening.RemoteWorkListenFailed")
	got := notes(api)
	if len(got) != 1 || got[0].Severity != "error" || !strings.Contains(got[0].Message, "cannot listen on port 47123") {
		t.Errorf("notifications = %+v", got)
	}
	// fixing it and turning it off and on again listens
	r.mu.Lock()
	r.failTo = nil
	r.mu.Unlock()
	if err := r.hub.SetPort(50002); err != nil {
		t.Fatal(err)
	}
	if snap := r.hub.Snapshot(folder); !snap.Listening || snap.ListenErr != "" {
		t.Errorf("snapshot = %+v", snap)
	}
}

func TestOnChangeIsToldOfChanges(t *testing.T) {
	r := newRig(t)
	_, folder := r.workspace("project")
	n := 0
	stop := r.hub.OnChange(func() { n++ })
	if _, err := r.hub.AddKey(newKey(t).line("a")); err != nil {
		t.Fatal(err)
	}
	if err := r.hub.SetIdleMinutes(5); err != nil {
		t.Fatal(err)
	}
	if err := r.hub.SetWorkspace(folder, true); err != nil {
		t.Fatal(err)
	}
	got := n
	if got < 3 {
		t.Errorf("OnChange was called %d times", got)
	}
	stop()
	if err := r.hub.SetIdleMinutes(6); err != nil {
		t.Fatal(err)
	}
	if n != got {
		t.Error("OnChange was called after stop")
	}
}

func TestStateDirFromEnv(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	got, err := stateDirFromEnv()
	if err != nil || got != filepath.Join(root, "state", "emetos") {
		t.Errorf("stateDirFromEnv = %q, %v", got, err)
	}
	t.Setenv("XDG_STATE_HOME", "relative/state") // not absolute: the default
	t.Setenv("HOME", filepath.Join(root, "home"))
	got, err = stateDirFromEnv()
	if err != nil || got != filepath.Join(root, "home", ".local", "state", "emetos") {
		t.Errorf("stateDirFromEnv = %q, %v", got, err)
	}
	t.Setenv("HOME", "")
	t.Setenv("XDG_STATE_HOME", "")
	_, err = stateDirFromEnv()
	wantRoute(t, err, "stateDirFromEnv.RemoteWorkStateDirUnknown")

	h := NewHub(HubOptions{StateDir: func() (string, error) { return "/nowhere", nil }})
	if p, err := h.HostsPath(); err != nil || p != "/nowhere/remote-work-hosts.json" {
		t.Errorf("HostsPath = %q, %v", p, err)
	}
}

func TestStateFilesAreMode0600(t *testing.T) {
	r := newRig(t)
	_, folder := r.workspace("project")
	b := newKey(t)
	r.on(folder, b)
	for _, name := range []string{configFileName, hostKeyFileName} {
		info, err := os.Stat(filepath.Join(r.dir, name))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("%s: %v, %v; want mode 0600", name, info, err)
		}
	}
	if info, err := os.Stat(r.dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("state folder: %v, %v; want mode 0700", info, err)
	}
	// the client's pins, too
	c := r.client(b)
	conn, err := c.connect(r.addr(), "")
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	if info, err := os.Stat(c.path); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("pins file: %v, %v; want mode 0600", info, err)
	}
	// nothing of the session is on disk: no token, no passphrase
	entries, _ := os.ReadDir(r.dir)
	for _, e := range entries {
		data, _ := os.ReadFile(filepath.Join(r.dir, e.Name()))
		if bytes.Contains(data, conn.Grant.Token) || strings.Contains(string(data), "token") {
			t.Errorf("%s holds a session token", e.Name())
		}
	}
	// the file reads back the same in another hub
	h2 := NewHub(HubOptions{StateDir: func() (string, error) { return r.dir, nil }})
	snap := h2.Snapshot(folder)
	if !snap.On || len(snap.Keys) != 1 || snap.Keys[0].Comment != "laptop B" || snap.HostFingerprint == "" || snap.HostFingerprint != r.hub.Snapshot(folder).HostFingerprint {
		t.Errorf("snapshot = %+v", snap)
	}
}

func TestPinsOfTheClientSurviveAndRefuseAChangedHostKey(t *testing.T) {
	r := newRig(t)
	_, folder := r.workspace("project")
	b := newKey(t)
	r.on(folder, b)
	c := r.client(b)
	var asked []sshlogin.FirstUse
	connect := func() (*login.Connection, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return login.Connect(ctx, login.ConnectOptions{Address: r.addr(), Auth: sshlogin.SignerAuth(b.signer), Hosts: c.hosts,
			ConfirmHost: func(_ context.Context, h sshlogin.FirstUse) (bool, error) { asked = append(asked, h); return true, nil }})
	}
	conn, err := connect()
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	want := r.hub.Snapshot(folder).HostFingerprint
	if len(asked) != 1 || asked[0].Fingerprint != want {
		t.Fatalf("the first use asked %+v; the page shows %s", asked, want)
	}
	conn, err = connect()
	if err != nil || len(asked) != 1 {
		t.Fatalf("second connection: %v, asked %d times", err, len(asked))
	}
	_ = conn.Close()
	// the host is set up again with another key: the same address is refused, and the user is not asked
	if err := c.hosts.Pin(r.addr(), newKey(t).signer.PublicKey()); err != nil {
		t.Fatal(err)
	}
	_, err = connect()
	wantRoute(t, err, "dial.func1.SSHLoginHostKeyChanged")
	if len(asked) != 1 {
		t.Errorf("a changed key was put to the user as a question")
	}
}

func TestTokens(t *testing.T) {
	a, err := newToken(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := newToken(rand.Reader)
	if a == b || a == ([32]byte{}) {
		t.Error("two tokens are the same, or empty")
	}
	_, err = newToken(strings.NewReader("short"))
	wantRoute(t, err, "newToken.RemoteWorkTokenFailed")
}

// TestNoToken: a login whose token cannot be made gets no grant and no session.
func TestNoToken(t *testing.T) {
	r := newRig(t)
	_, folder := r.workspace("project")
	r.on(folder)
	r.hub.opts.Random = strings.NewReader("")
	ch := &memChannel{}
	r.hub.serve(&sshlogin.Session{Channel: ch, ExtraData: mustRequest(t, ""), Remote: &net.TCPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 1}})
	if ch.out.Len() != 0 || len(r.hub.Sessions()) != 0 {
		t.Errorf("a grant of %d bytes, %d sessions", ch.out.Len(), len(r.hub.Sessions()))
	}
}

func TestTheHostKeyNeedsAStateFolder(t *testing.T) {
	h := NewHub(HubOptions{StateDir: func() (string, error) { return "", errors.New("Nowhere", "no folder") }})
	_, err := h.HostKey()
	wantRoute(t, err, "Hub.HostKey.RemoteWorkNoStateDir")
	if snap := h.Snapshot("/x"); snap.LoadErr == "" {
		t.Error("the page is not told the state folder is unknown")
	}
}
