package sshlogin

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// testChannel is the channel type of the test server.
const testChannel = "emetos-test"

// fakeClock is a clock a test moves by hand.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *fakeClock { return &fakeClock{t: time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
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

func (k testKey) allowed(comment string) AllowedKey {
	return AllowedKey{Key: k.signer.PublicKey(), Comment: comment}
}

// writeKeyFile writes the key in OpenSSH format, with a passphrase when it is not empty, and returns the path.
func (k testKey) writeKeyFile(t testing.TB, dir, passphrase string) string {
	t.Helper()
	var block *pem.Block
	var err error
	if passphrase == "" {
		block, err = ssh.MarshalPrivateKey(k.priv, "test")
	} else {
		block, err = ssh.MarshalPrivateKeyWithPassphrase(k.priv, "test", []byte(passphrase))
	}
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// rig is a server on a loopback port with an ephemeral number, its clock, its list of keys and what it reported.
type rig struct {
	t        *testing.T
	srv      *Server
	addr     string
	clock    *fakeClock
	host     testKey
	hosts    *HostStore
	limiter  *Limiter
	mu       sync.Mutex
	keys     []AllowedKey
	logins   chan Login
	refusals chan Refusal
	sessions chan *Session
}

type rigOptions struct {
	handshake time.Duration
	channel   time.Duration
	pending   int
	failures  int
	handler   func(*Session)
}

func newRig(t *testing.T, opts ...rigOptions) *rig {
	t.Helper()
	var o rigOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	r := &rig{t: t, clock: newClock(), host: newKey(t), logins: make(chan Login, 32), refusals: make(chan Refusal, 64), sessions: make(chan *Session, 8)}
	r.hosts = NewHostStore(filepath.Join(t.TempDir(), "hosts.json"), r.clock.Now)
	r.limiter = NewLimiter(LimiterOptions{Now: r.clock.Now, Failures: o.failures})
	handler := o.handler
	if handler == nil {
		handler = func(s *Session) {
			r.sessions <- s
			<-s.Done()
		}
	}
	srv, err := NewServer(ServerOptions{
		HostKey: r.host.signer, ChannelType: testChannel,
		Keys: func() []AllowedKey {
			r.mu.Lock()
			defer r.mu.Unlock()
			return append([]AllowedKey(nil), r.keys...)
		},
		Handler: handler, OnLogin: func(l Login) { r.logins <- l }, OnRefused: func(f Refusal) { r.refusals <- f },
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)), Limiter: r.limiter, Now: r.clock.Now,
		HandshakeTimeout: o.handshake, ChannelTimeout: o.channel, MaxPending: o.pending,
	})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	r.srv, r.addr = srv, ln.Addr().String()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := srv.Serve(ln); err != nil {
			t.Errorf("Serve: %v", err)
		}
	}()
	t.Cleanup(func() {
		srv.Close()
		<-done
	})
	return r
}

func (r *rig) allow(keys ...AllowedKey) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keys = append(r.keys, keys...)
}

func (r *rig) setKeys(keys ...AllowedKey) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.keys = keys
}

// pin pins the server's host key for its address.
func (r *rig) pin() {
	r.t.Helper()
	if err := r.hosts.Pin(r.addr, r.host.signer.PublicKey()); err != nil {
		r.t.Fatal(err)
	}
}

// dial logs in with the key; the host must be pinned.
func (r *rig) dial(auth *Auth) (*Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return Dial(ctx, DialOptions{Address: r.addr, Auth: auth, Hosts: r.hosts})
}

func (r *rig) nextRefusal() Refusal {
	r.t.Helper()
	select {
	case f := <-r.refusals:
		return f
	case <-time.After(5 * time.Second):
		r.t.Fatal("no refusal was reported")
		return Refusal{}
	}
}

func (r *rig) nextLogin() Login {
	r.t.Helper()
	select {
	case l := <-r.logins:
		return l
	case <-time.After(5 * time.Second):
		r.t.Fatal("no login was reported")
		return Login{}
	}
}

func (r *rig) nextSession() *Session {
	r.t.Helper()
	select {
	case s := <-r.sessions:
		return s
	case <-time.After(5 * time.Second):
		r.t.Fatal("no session was served")
		return nil
	}
}

// startAgent serves an in-process ssh-agent with the keys on a socket in a temporary folder and returns the socket.
func startAgent(t *testing.T, keys ...testKey) string {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "agent.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	keyring := agent.NewKeyring()
	for _, k := range keys {
		if err := keyring.Add(agent.AddedKey{PrivateKey: k.priv}); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = c.Close() }()
				_ = agent.ServeAgent(keyring, c)
			}()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		<-done
	})
	return sock
}
