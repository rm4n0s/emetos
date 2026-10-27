package sshlogin

import (
	"context"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
)

func TestLoginWithAListedKey(t *testing.T) {
	r := newRig(t)
	k := newKey(t)
	r.allow(k.allowed("laptop B"))
	r.pin()
	c, err := r.dial(SignerAuth(k.signer))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = c.Close() }()
	if want := ssh.FingerprintSHA256(r.host.signer.PublicKey()); c.HostFingerprint != want {
		t.Errorf("HostFingerprint = %q, want %q", c.HostFingerprint, want)
	}
	ch, err := c.OpenChannel(testChannel, []byte("workspace"))
	if err != nil {
		t.Fatalf("OpenChannel: %v", err)
	}
	login := r.nextLogin()
	if login.Key.Comment != "laptop B" || login.Key.Fingerprint() != k.allowed("").Fingerprint() {
		t.Errorf("login = %+v", login)
	}
	s := r.nextSession()
	if string(s.ExtraData) != "workspace" || s.Key.Comment != "laptop B" {
		t.Errorf("session = %+v", s)
	}
	// the channel carries bytes both ways
	go func() { _, _ = s.Channel.Write([]byte("hello")) }()
	buf := make([]byte, 5)
	if _, err := ch.Read(buf); err != nil || string(buf) != "hello" {
		t.Errorf("read %q, %v", buf, err)
	}
	_ = c.Close()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Error("the session did not end when the client closed")
	}
}

func TestRefusals(t *testing.T) {
	listed := newKey(t)
	tests := []struct {
		name   string
		run    func(t *testing.T, r *rig)
		reason string
		method string
	}{
		{"unlisted key", func(t *testing.T, r *rig) {
			_, err := r.dial(SignerAuth(newKey(t).signer))
			wantRoute(t, err, "dial.SSHLoginAuthFailed")
		}, ReasonKeyNotListed, "publickey"},
		// The server advertises public keys only, so a client that has a password never sends it: it stops after "none".
		{"password", func(t *testing.T, r *rig) {
			cfg := rawConfig(r, ssh.Password("hunter2"))
			_, err := ssh.Dial("tcp", r.addr, cfg)
			if err == nil || !strings.Contains(err.Error(), "attempted methods [none]") {
				t.Fatalf("a password was taken, or the server offered it: %v", err)
			}
		}, ReasonHandshakeFailed, ""},
		{"keyboard interactive", func(t *testing.T, r *rig) {
			cfg := rawConfig(r, ssh.KeyboardInteractive(func(string, string, []string, []bool) ([]string, error) { return nil, nil }))
			_, err := ssh.Dial("tcp", r.addr, cfg)
			if err == nil || !strings.Contains(err.Error(), "attempted methods [none]") {
				t.Fatalf("keyboard-interactive was taken, or the server offered it: %v", err)
			}
		}, ReasonHandshakeFailed, ""},
		{"channel session", func(t *testing.T, r *rig) {
			c := mustLogin(t, r, listed)
			_, err := c.OpenChannel("session", nil)
			wantRoute(t, err, "OpenChannel.SSHLoginChannelRefused")
		}, ReasonChannelType, ""},
		{"channel direct-tcpip", func(t *testing.T, r *rig) {
			c := mustLogin(t, r, listed)
			_, err := c.OpenChannel("direct-tcpip", ssh.Marshal(struct {
				Host string
				Port uint32
				OH   string
				OP   uint32
			}{"127.0.0.1", 22, "127.0.0.1", 1}))
			wantRoute(t, err, "OpenChannel.SSHLoginChannelRefused")
		}, ReasonChannelType, ""},
		{"shell request", func(t *testing.T, r *rig) {
			c := mustLogin(t, r, listed)
			ch, err := c.OpenChannel(testChannel, nil)
			if err != nil {
				t.Fatal(err)
			}
			ok, err := ch.SendRequest("shell", true, nil)
			if ok {
				t.Errorf("shell was accepted (err %v)", err)
			}
		}, ReasonRequest, ""},
		{"exec request", func(t *testing.T, r *rig) {
			c := mustLogin(t, r, listed)
			ch, err := c.OpenChannel(testChannel, nil)
			if err != nil {
				t.Fatal(err)
			}
			ok, _ := ch.SendRequest("exec", true, ssh.Marshal(struct{ Command string }{"id"}))
			if ok {
				t.Error("exec was accepted")
			}
		}, ReasonRequest, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t)
			r.allow(listed.allowed("B"))
			r.pin()
			tt.run(t, r)
			var got Refusal
			for {
				got = r.nextRefusal()
				if got.Reason == tt.reason {
					break
				}
				if got.Reason == ReasonHandshakeFailed {
					continue // the end of the same connection
				}
				t.Fatalf("refusal = %+v, want reason %s", got, tt.reason)
			}
			if tt.method != "" && got.Method != tt.method {
				t.Errorf("method = %q, want %q", got.Method, tt.method)
			}
			if got.Address != "127.0.0.1" || got.Remote == "" {
				t.Errorf("refusal = %+v", got)
			}
		})
	}
}

// rawConfig is a client that is not the Emetos one: it knows the host key and offers the auth methods.
func rawConfig(r *rig, methods ...ssh.AuthMethod) *ssh.ClientConfig {
	return &ssh.ClientConfig{User: "someone", Auth: methods, HostKeyCallback: ssh.FixedHostKey(r.host.signer.PublicKey()), Timeout: 5 * time.Second}
}

func mustLogin(t *testing.T, r *rig, k testKey) *Client {
	t.Helper()
	c, err := r.dial(SignerAuth(k.signer))
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
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

func TestAgentLogin(t *testing.T) {
	r := newRig(t)
	listed, other := newKey(t), newKey(t)
	r.allow(listed.allowed("agent key"))
	r.pin()
	sock := startAgent(t, other, listed)

	keys, err := AgentKeys(context.Background(), sock)
	if err != nil || len(keys) != 2 {
		t.Fatalf("AgentKeys = %v, %v", keys, err)
	}
	// every key is offered in the agent's order: the unlisted one fails, the listed one logs in
	auth, err := AgentAuth(context.Background(), sock, "")
	if err != nil {
		t.Fatal(err)
	}
	c, err := r.dial(auth)
	_ = auth.Close()
	if err != nil {
		t.Fatalf("Dial with the agent: %v", err)
	}
	_ = c.Close()
	if l := r.nextLogin(); l.Key.Comment != "agent key" {
		t.Errorf("login = %+v", l)
	}
	// a chosen fingerprint offers that key only
	auth, err = AgentAuth(context.Background(), sock, other.allowed("").Fingerprint())
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.dial(auth)
	_ = auth.Close()
	wantRoute(t, err, "dial.SSHLoginAuthFailed")
	// the unlisted key is reported once
	if f := r.nextRefusal(); f.Reason != ReasonKeyNotListed || f.KeyFingerprint != other.allowed("").Fingerprint() {
		t.Errorf("refusal = %+v", f)
	}
}

func TestAgentErrors(t *testing.T) {
	ctx := context.Background()
	empty := startAgent(t)
	tests := []struct {
		name  string
		run   func() error
		route string
	}{
		{"no socket", func() error { _, err := AgentAuth(ctx, "", ""); return err }, "AgentAuth.SSHLoginAgentUnavailable"},
		{"no socket for the list", func() error { _, err := AgentKeys(ctx, ""); return err }, "AgentKeys.SSHLoginAgentUnavailable"},
		{"nothing behind the socket", func() error { _, err := AgentAuth(ctx, t.TempDir()+"/none.sock", ""); return err }, "agentSigners.SSHLoginAgentUnavailable"},
		{"no key", func() error { _, err := AgentAuth(ctx, empty, ""); return err }, "AgentAuth.SSHLoginAgentNoKey"},
		{"not the chosen key", func() error { _, err := AgentAuth(ctx, empty, "SHA256:nope"); return err }, "AgentAuth.SSHLoginAgentNoKey"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { wantRoute(t, tt.run(), tt.route) })
	}
	if got := AgentSocket(func(k string) string { return map[string]string{"SSH_AUTH_SOCK": "/run/agent"}[k] }); got != "/run/agent" {
		t.Errorf("AgentSocket = %q", got)
	}
}

func TestAgentThatDoesNotAnswer(t *testing.T) {
	sock := t.TempDir() + "/bad.sock"
	ln := listenUnix(t, sock)
	go func() {
		c, err := ln.Accept()
		if err == nil {
			_, _ = c.Write([]byte("not an agent reply"))
			_ = c.Close()
		}
	}()
	_, err := AgentAuth(context.Background(), sock, "")
	wantRoute(t, err, "agentSigners.SSHLoginAgentFailed")
}

func TestKeyFiles(t *testing.T) {
	k := newKey(t)
	dir := t.TempDir()
	plain := k.writeKeyFile(t, dir, "")
	dir2 := t.TempDir()
	locked := k.writeKeyFile(t, dir2, "correct horse")
	var asked []int
	tests := []struct {
		name  string
		path  string
		ask   PassphraseFunc
		route string
		asked int
	}{
		{"no passphrase", plain, nil, "", 0},
		{"passphrase", locked, func(a int) ([]byte, bool) { return []byte("correct horse"), true }, "", 0},
		{"second try", locked, func(a int) ([]byte, bool) {
			if a == 1 {
				return []byte("wrong"), true
			}
			return []byte("correct horse"), true
		}, "", 0},
		{"wrong passphrase", locked, func(a int) ([]byte, bool) { asked = append(asked, a); return []byte("wrong"), true }, "KeyFileAuth.SSHLoginWrongPassphrase", 3},
		{"cancelled", locked, func(int) ([]byte, bool) { return nil, false }, "KeyFileAuth.SSHLoginPassphraseCancelled", 0},
		{"no way to ask", locked, nil, "KeyFileAuth.SSHLoginPassphraseRequired", 0},
		{"missing file", dir + "/none", nil, "KeyFileAuth.SSHLoginKeyFileUnreadable", 0},
		{"not a key", writeFile(t, "junk", "this is not a key"), nil, "KeyFileAuth.SSHLoginKeyFileInvalid", 0},
		{"too large", writeFile(t, "big", strings.Repeat("A", maxKeyFile+10)), nil, "KeyFileAuth.SSHLoginKeyFileInvalid", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			asked = nil
			a, err := KeyFileAuth(tt.path, tt.ask)
			if tt.route != "" {
				wantRoute(t, err, tt.route)
				if tt.asked > 0 && len(asked) != tt.asked {
					t.Errorf("asked %v, want %d times", asked, tt.asked)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if !SameKey(a.PublicKey, k.signer.PublicKey()) {
				t.Error("the Auth has another key")
			}
		})
	}
}

func TestPassphraseIsWipedAfterUse(t *testing.T) {
	k := newKey(t)
	locked := k.writeKeyFile(t, t.TempDir(), "secret words")
	pass := []byte("secret words")
	if _, err := KeyFileAuth(locked, func(int) ([]byte, bool) { return pass, true }); err != nil {
		t.Fatal(err)
	}
	for _, b := range pass {
		if b != 0 {
			t.Fatalf("the passphrase was left in memory: %q", pass)
		}
	}
}

func TestLoginWithAPassphraseKeyFile(t *testing.T) {
	r := newRig(t)
	k := newKey(t)
	r.allow(k.allowed("locked"))
	r.pin()
	auth, err := KeyFileAuth(k.writeKeyFile(t, t.TempDir(), "pw"), func(int) ([]byte, bool) { return []byte("pw"), true })
	if err != nil {
		t.Fatal(err)
	}
	c, err := r.dial(auth)
	if err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	if l := r.nextLogin(); l.Key.Comment != "locked" {
		t.Errorf("login = %+v", l)
	}
}

func TestHostKeyPinning(t *testing.T) {
	r := newRig(t, rigOptions{failures: 100}) // every probe of an unknown host counts as a failed attempt
	k := newKey(t)
	r.allow(k.allowed("B"))
	fp := ssh.FingerprintSHA256(r.host.signer.PublicKey())
	ctx := context.Background()
	opts := func(confirm ConfirmHostFunc) DialOptions {
		return DialOptions{Address: r.addr, Auth: SignerAuth(k.signer), Hosts: r.hosts, ConfirmHost: confirm}
	}

	// first use: refused when nothing can ask, and when the user says no; nothing is pinned
	_, err := Dial(ctx, opts(nil))
	wantRoute(t, err, "Dial.SSHLoginHostKeyRejected")
	_, err = Dial(ctx, opts(func(context.Context, FirstUse) (bool, error) { return false, nil }))
	wantRoute(t, err, "Dial.SSHLoginHostKeyRejected")
	_, err = Dial(ctx, opts(func(context.Context, FirstUse) (bool, error) { return false, errors.New("Dialog", "closed") }))
	wantRoute(t, err, "Dial.SSHLoginConfirmFailed")
	if pins, _ := r.hosts.List(); len(pins) != 0 {
		t.Fatalf("pinned after a refusal: %v", pins)
	}

	// first use: the user compares the fingerprint and says yes; the host is pinned and the login goes on
	var asked []FirstUse
	c, err := Dial(ctx, opts(func(_ context.Context, h FirstUse) (bool, error) { asked = append(asked, h); return true, nil }))
	if err != nil {
		t.Fatalf("first use: %v", err)
	}
	_ = c.Close()
	if len(asked) != 1 || asked[0].Fingerprint != fp || asked[0].Type != "ssh-ed25519" || asked[0].Address != r.addr {
		t.Errorf("asked %+v, want the fingerprint %s", asked, fp)
	}
	pins, err := r.hosts.List()
	if err != nil || len(pins) != 1 || pins[0].Fingerprint != fp || pins[0].FirstSeen.IsZero() {
		t.Fatalf("pins = %+v, %v", pins, err)
	}

	// the second use asks nothing
	c, err = Dial(ctx, opts(func(context.Context, FirstUse) (bool, error) { t.Error("asked again"); return false, nil }))
	if err != nil {
		t.Fatalf("second use: %v", err)
	}
	_ = c.Close()

	// a changed key is refused and nothing is sent to the host: it never sees a login attempt
	other := newKey(t)
	if err := r.hosts.Pin(r.addr, other.signer.PublicKey()); err != nil {
		t.Fatal(err)
	}
	before := len(r.logins)
	_, err = Dial(ctx, opts(func(context.Context, FirstUse) (bool, error) { t.Error("asked about a changed key"); return true, nil }))
	wantRoute(t, err, "dial.func1.SSHLoginHostKeyChanged")
	appErr, _ := errors.FromError(err)
	if appErr.Metadata["pinned"] != other.allowed("").Fingerprint() || appErr.Metadata["presented"] != fp {
		t.Errorf("metadata = %v", appErr.Metadata)
	}
	if len(r.logins) != before {
		t.Error("a login was made to a host with a changed key")
	}

	// forgetting the pin makes the next use a first use again
	if err := r.hosts.Forget(r.addr); err != nil {
		t.Fatal(err)
	}
	asked = nil
	c, err = Dial(ctx, opts(func(_ context.Context, h FirstUse) (bool, error) { asked = append(asked, h); return true, nil }))
	if err != nil || len(asked) != 1 {
		t.Fatalf("after Forget: %v, asked %v", err, asked)
	}
	_ = c.Close()
}

func TestDialErrors(t *testing.T) {
	k := newKey(t)
	hosts := NewHostStore(t.TempDir()+"/hosts.json", nil)
	ctx := context.Background()
	tests := []struct {
		name  string
		opts  DialOptions
		route string
	}{
		{"no auth", DialOptions{Address: "127.0.0.1:1", Hosts: hosts}, "Dial.SSHLoginDialInvalid"},
		{"no store", DialOptions{Address: "127.0.0.1:1", Auth: SignerAuth(k.signer)}, "Dial.SSHLoginDialInvalid"},
		{"bad address", DialOptions{Address: "nope", Auth: SignerAuth(k.signer), Hosts: hosts}, "NormalizeAddress.SSHLoginAddressInvalid"},
		{"nobody listens", DialOptions{Address: freeAddr(t), Auth: SignerAuth(k.signer), Hosts: hosts}, "dial.SSHLoginConnectFailed"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Dial(ctx, tt.opts)
			wantRoute(t, err, tt.route)
		})
	}
}

func TestRemovedKeyStopsLoggingIn(t *testing.T) {
	r := newRig(t)
	k := newKey(t)
	r.allow(k.allowed("B"))
	r.pin()
	c := mustLogin(t, r, k)
	_ = c.Close()
	r.setKeys() // the key is taken out of the list
	_, err := r.dial(SignerAuth(k.signer))
	wantRoute(t, err, "dial.SSHLoginAuthFailed")
}

func TestOneChannelForEachConnection(t *testing.T) {
	r := newRig(t)
	k := newKey(t)
	r.allow(k.allowed("B"))
	r.pin()
	c := mustLogin(t, r, k)
	if _, err := c.OpenChannel(testChannel, nil); err != nil {
		t.Fatal(err)
	}
	_, err := c.OpenChannel(testChannel, nil)
	wantRoute(t, err, "OpenChannel.SSHLoginChannelRefused")
	_, err = mustLoginDial(r, k).OpenChannel(testChannel, make([]byte, MaxChannelData+1))
	wantRoute(t, err, "OpenChannel.SSHLoginChannelRefused")
}

func mustLoginDial(r *rig, k testKey) *Client {
	c, err := r.dial(SignerAuth(k.signer))
	if err != nil {
		r.t.Fatal(err)
	}
	r.t.Cleanup(func() { _ = c.Close() })
	return c
}

func TestLoginThatOpensNoChannelIsDropped(t *testing.T) {
	r := newRig(t, rigOptions{channel: 100 * time.Millisecond})
	k := newKey(t)
	r.allow(k.allowed("B"))
	r.pin()
	c := mustLogin(t, r, k)
	done := make(chan struct{})
	go func() { _ = c.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a client that opened no channel was kept")
	}
	if f := r.nextRefusal(); f.Reason != ReasonNoChannel {
		t.Errorf("refusal = %+v", f)
	}
}

func TestSilentConnectionTimesOut(t *testing.T) {
	r := newRig(t, rigOptions{handshake: 100 * time.Millisecond})
	c, err := net.Dial("tcp", r.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 4096)
	for {
		if _, err := c.Read(buf); err != nil {
			break // the server sent its version line, then closed
		}
	}
	f := r.nextRefusal()
	if f.Reason != ReasonHandshakeFailed {
		t.Errorf("refusal = %+v", f)
	}
}

func TestTooManyConnectionsLoggingIn(t *testing.T) {
	r := newRig(t, rigOptions{pending: 2, handshake: 5 * time.Second})
	var held []net.Conn
	for i := 0; i < 2; i++ {
		c, err := net.Dial("tcp", r.addr)
		if err != nil {
			t.Fatal(err)
		}
		held = append(held, c)
	}
	defer func() {
		for _, c := range held {
			_ = c.Close()
		}
	}()
	// wait until the server holds both, then a third is closed at once
	deadline := time.Now().Add(5 * time.Second)
	for len(r.srv.pending) < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	c, err := net.Dial("tcp", r.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if n, err := c.Read(make([]byte, 16)); err == nil {
		t.Errorf("the third connection got %d bytes", n)
	}
}

func TestServerOptionsAreChecked(t *testing.T) {
	k := newKey(t)
	good := ServerOptions{HostKey: k.signer, ChannelType: "x", Keys: func() []AllowedKey { return nil }, Handler: func(*Session) {}}
	for name, mutate := range map[string]func(*ServerOptions){
		"host key": func(o *ServerOptions) { o.HostKey = nil }, "channel": func(o *ServerOptions) { o.ChannelType = "" },
		"keys": func(o *ServerOptions) { o.Keys = nil }, "handler": func(o *ServerOptions) { o.Handler = nil },
	} {
		t.Run(name, func(t *testing.T) {
			o := good
			mutate(&o)
			_, err := NewServer(o)
			wantRoute(t, err, "NewServer.SSHLoginServerInvalid")
		})
	}
	if _, err := NewServer(good); err != nil {
		t.Fatal(err)
	}
}

func TestCloseEndsSessions(t *testing.T) {
	r := newRig(t)
	k := newKey(t)
	r.allow(k.allowed("B"))
	r.pin()
	c := mustLogin(t, r, k)
	if _, err := c.OpenChannel(testChannel, nil); err != nil {
		t.Fatal(err)
	}
	s := r.nextSession()
	r.srv.Close()
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Close left the session open")
	}
	if err := c.Wait(); err == nil {
		t.Log("client saw a clean end")
	}
	// a closed server takes no more connections
	if _, err := net.DialTimeout("tcp", r.addr, 500*time.Millisecond); err == nil {
		t.Error("the closed server still listens")
	}
}

func TestPublicKeyCallbackRoute(t *testing.T) {
	r := newRig(t)
	st := &connState{}
	cfg := r.srv.config(fakeConn{}, "127.0.0.1", st)
	_, err := cfg.PublicKeyCallback(nil, newKey(t).signer.PublicKey())
	wantRoute(t, err, "config.func1.SSHLoginKeyNotListed")
	_, err = cfg.VerifiedPublicKeyCallback(nil, newKey(t).signer.PublicKey(), &ssh.Permissions{}, "")
	wantRoute(t, err, "config.func2.SSHLoginKeyNotListed")
}

// forgetfulStore is a store of host keys that trusts nothing, even what it was told to pin.
type forgetfulStore struct{ pinned int }

func (s *forgetfulStore) Check(string, ssh.PublicKey) (Verdict, Pinned, error) {
	return HostUnknown, Pinned{}, nil
}
func (s *forgetfulStore) Pin(string, ssh.PublicKey) error { s.pinned++; return nil }

// TestAPinThatDoesNotHoldIsAnError: the host key was pinned and the second connection does not find it, which the user is not asked about
// again.
func TestAPinThatDoesNotHoldIsAnError(t *testing.T) {
	r := newRig(t, rigOptions{failures: 100})
	k := newKey(t)
	r.allow(k.allowed("B"))
	store := &forgetfulStore{}
	asked := 0
	_, err := Dial(context.Background(), DialOptions{Address: r.addr, Auth: SignerAuth(k.signer), Hosts: store,
		ConfirmHost: func(context.Context, FirstUse) (bool, error) { asked++; return true, nil }})
	wantRoute(t, err, "Dial.SSHLoginHostKeyUnknown")
	if asked != 1 || store.pinned != 1 {
		t.Errorf("asked %d times, pinned %d times", asked, store.pinned)
	}
}

// brokenListener fails to accept.
type brokenListener struct{ net.Listener }

func (brokenListener) Accept() (net.Conn, error) { return nil, io.ErrNoProgress }
func (brokenListener) Close() error              { return nil }

func TestAListenerThatFailsEndsServe(t *testing.T) {
	r := newRig(t)
	srv, err := NewServer(ServerOptions{HostKey: r.host.signer, ChannelType: "x", Keys: func() []AllowedKey { return nil }, Handler: func(*Session) {}})
	if err != nil {
		t.Fatal(err)
	}
	wantRoute(t, srv.Serve(brokenListener{}), "Server.Serve.SSHLoginAcceptFailed")
	// a server that was closed takes no listener
	srv.Close()
	if err := srv.Serve(brokenListener{}); err != nil {
		t.Errorf("Serve on a closed server: %v", err)
	}
}
