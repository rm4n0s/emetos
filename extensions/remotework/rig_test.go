package remotework

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/fakeapi"
	"github.com/rm4n0s/emetos/extensions/internal/sshlogin"
	"github.com/rm4n0s/emetos/extensions/remotework/login"
	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
)

// clock is a clock and a set of timers that a test moves by hand.
type clock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	at      time.Time
	f       func()
	stopped bool
	fired   bool
}

func newClock() *clock { return &clock{now: time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) After(d time.Duration, f func()) func() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{at: c.now.Add(d), f: f}
	c.timers = append(c.timers, t)
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		was := !t.stopped && !t.fired
		t.stopped = true
		return was
	}
}

// Advance moves time forward and runs the timers that are due.
func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []*fakeTimer
	for _, t := range c.timers {
		if !t.stopped && !t.fired && !t.at.After(c.now) {
			t.fired = true
			due = append(due, t)
		}
	}
	c.mu.Unlock()
	for _, t := range due {
		t.f()
	}
}

// testKey is a key pair made for a test.
type testKey struct {
	priv   ed25519.PrivateKey
	signer ssh.Signer
}

func newKey(t testing.TB) testKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	return testKey{priv: priv, signer: signer}
}

// line is the authorized_keys line of the key with the comment.
func (k testKey) line(comment string) string {
	l := string(ssh.MarshalAuthorizedKey(k.signer.PublicKey()))
	l = l[:len(l)-1]
	if comment != "" {
		l += " " + comment
	}
	return l
}

func (k testKey) fingerprint() string { return ssh.FingerprintSHA256(k.signer.PublicKey()) }

// isolate points the XDG variables of the test at folders of its own and returns the state folder of Emetos.
func isolate(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, v := range []string{"CONFIG", "STATE", "CACHE", "DATA"} {
		t.Setenv("XDG_"+v+"_HOME", filepath.Join(root, v))
	}
	t.Setenv("XDG_RUNTIME_DIR", filepath.Join(root, "RUN"))
	t.Setenv("HOME", filepath.Join(root, "HOME"))
	t.Setenv("SSH_AUTH_SOCK", "")
	dir, err := stateDirFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// rig is a hub on a loopback port with its clock, its state folder and workspaces.
type rig struct {
	t          *testing.T
	hub        *Hub
	clock      *clock
	dir        string
	certFP     []byte
	listens    []int
	mu         sync.Mutex
	sess       chan *Session
	failTo     error
	unregister map[string]func()
}

type rigOptions struct {
	cert func() []byte
	// transport gives the hub its transport (step 15.3), with QUIC on the loopback unless noQUIC; the tests of the login leave it off
	// so that the SSH channel is theirs.
	transport bool
	noQUIC    bool
	// view lets the hub serve the session with a headless view, as it does in the editor; the tests of the transport read the
	// messages of the session themselves.
	view     bool
	hubTweak func(*HubOptions)
}

func newRig(t *testing.T, opts ...rigOptions) *rig {
	t.Helper()
	r := &rig{t: t, clock: newClock(), dir: isolate(t), sess: make(chan *Session, 8), unregister: map[string]func(){}}
	var o rigOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	r.certFP = make([]byte, 32)
	for i := range r.certFP {
		r.certFP[i] = 0xAB
	}
	cert := o.cert
	if cert == nil {
		cert = func() []byte { return r.certFP }
	}
	ho := HubOptions{
		NoTransport: !o.transport, NoQUIC: o.noQUIC, NoView: !o.view,
		ListenUDP: func(port int) (net.PacketConn, error) {
			return net.ListenPacket("udp", "127.0.0.1:"+strconv.Itoa(port))
		},
		Listen: func(port int) (net.Listener, error) {
			r.mu.Lock()
			r.listens = append(r.listens, port)
			failTo := r.failTo
			r.mu.Unlock()
			if failTo != nil {
				return nil, failTo
			}
			return net.Listen("tcp", "127.0.0.1:0")
		},
		OnSession: func(s *Session) { r.sess <- s },
		Logger:    slog.New(slog.NewTextHandler(io.Discard, nil)), Now: r.clock.Now, After: r.clock.After,
		Limiter:          sshlogin.NewLimiter(sshlogin.LimiterOptions{Now: r.clock.Now}),
		HandshakeTimeout: 5 * time.Second, ChannelTimeout: 5 * time.Second,
	}
	if !o.transport || o.cert != nil {
		ho.Cert = cert
	}
	if o.hubTweak != nil {
		o.hubTweak(&ho)
	}
	r.hub = NewHub(ho)
	t.Cleanup(r.hub.Close)
	return r
}

// workspace registers a workspace with a folder of its own and returns its fake API.
func (r *rig) workspace(name string) (*fakeapi.API, string) {
	r.t.Helper()
	folder := filepath.Join(r.t.TempDir(), name)
	if err := os.MkdirAll(folder, 0o700); err != nil {
		r.t.Fatal(err)
	}
	api := fakeapi.New(r.t, extension.Manifest{ID: ID, Name: "Remote Work"}, extension.ScopeInfo{Kind: extension.ScopeWorkspace, Name: name, Folder: folder, ID: folder})
	unregister := r.hub.Register(api)
	r.unregister[folder] = unregister
	r.t.Cleanup(unregister)
	return api, folder
}

func (r *rig) addr() string {
	r.t.Helper()
	a := r.hub.Addr()
	if a == nil {
		r.t.Fatal("the hub does not listen")
	}
	return a.String()
}

// client is a machine B: a key, a store of pinned host keys and the answer to the first-use question.
type client struct {
	t     *testing.T
	key   testKey
	hosts *sshlogin.HostStore
	path  string
}

func (r *rig) client(k testKey) *client {
	path := filepath.Join(r.t.TempDir(), "hosts.json")
	return &client{t: r.t, key: k, path: path, hosts: sshlogin.NewHostStore(path, r.clock.Now)}
}

// pin trusts the host key of the hub for the address, as a user who compared the fingerprint does.
func (c *client) pin(r *rig) {
	c.t.Helper()
	signer, err := r.hub.HostKey()
	if err != nil {
		c.t.Fatal(err)
	}
	if err := c.hosts.Pin(r.addr(), signer.PublicKey()); err != nil {
		c.t.Fatal(err)
	}
}

func (c *client) connect(addr, workspace string) (*login.Connection, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return login.Connect(ctx, login.ConnectOptions{
		Address: addr, Workspace: workspace, Auth: sshlogin.SignerAuth(c.key.signer), Hosts: c.hosts,
		ConfirmHost: func(context.Context, sshlogin.FirstUse) (bool, error) { return true, nil },
	})
}

// next reads the next message of the channel of a connection.
func next(t *testing.T, c *login.Connection) protocol.Message {
	t.Helper()
	done := make(chan struct{})
	var m protocol.Message
	var err error
	go func() {
		m, err = protocol.NewReader(c.Channel).Next()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("no message arrived")
	}
	if err != nil {
		t.Fatalf("reading the channel: %v", err)
	}
	return m
}

func (r *rig) nextSession() *Session {
	r.t.Helper()
	select {
	case s := <-r.sess:
		return s
	case <-time.After(10 * time.Second):
		r.t.Fatal("no session was started")
		return nil
	}
}

func wantRoute(t *testing.T, err error, route string) {
	t.Helper()
	if err == nil {
		t.Fatalf("no error, want %s", route)
	}
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("error %T (%v) is not an *errors.Error", err, err)
	}
	if !appErr.HasRoute(route) {
		t.Errorf("route = %s, want %s", appErr.Route(), route)
	}
}
