package sshlogin

import (
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
)

// The reasons a login attempt or a request was refused (Refusal.Reason).
const (
	// ReasonKeyNotListed is a public key that is not in the list (or was taken out of it).
	ReasonKeyNotListed = "KeyNotListed"
	// ReasonKeyRefused is a public key attempt that failed for another reason (an algorithm the server does not take).
	ReasonKeyRefused = "KeyRefused"
	// ReasonPasswordRefused is a password login; the server has none.
	ReasonPasswordRefused = "PasswordRefused"
	// ReasonMethodRefused is any other login method (keyboard-interactive, host based, GSSAPI).
	ReasonMethodRefused = "MethodRefused"
	// ReasonHandshakeFailed is a connection that ended or timed out before it tried to authenticate, or whose signature was wrong.
	ReasonHandshakeFailed = "HandshakeFailed"
	// ReasonRateLimited is a connection from an address that had too many failures.
	ReasonRateLimited = "RateLimited"
	// ReasonChannelType is a channel of a type the server does not offer.
	ReasonChannelType = "ChannelType"
	// ReasonRequest is a request on the channel (shell, exec, pty-req, env, subsystem, forwarding); there are none to serve.
	ReasonRequest = "Request"
	// ReasonNoChannel is a logged-in client that opened no channel in time.
	ReasonNoChannel = "NoChannel"
	// ReasonTooManyPending is a connection refused because too many were still logging in.
	ReasonTooManyPending = "TooManyPending"
)

// Refusal is a login attempt or a request that the server refused.
type Refusal struct {
	// Remote is the address of the client, with its port.
	Remote string
	// Address is the address the limiter counts it under (the IP).
	Address string
	// User is the user name the client gave; it means nothing to the server and is shown to tell attempts apart. May be empty.
	User string
	// Method is the login method ("publickey", "password"), or empty.
	Method string
	// KeyFingerprint is the fingerprint of the public key that was offered, or empty.
	KeyFingerprint string
	// Reason is one of the Reason constants.
	Reason string
	// Detail says more: the channel type or request type refused.
	Detail string
	// At is when it happened.
	At time.Time
}

// Login is a client that logged in.
type Login struct {
	// Remote is the address of the client, with its port.
	Remote string
	// User is the user name the client gave.
	User string
	// Key is the key that logged in.
	Key AllowedKey
	// At is when it happened.
	At time.Time
}

// Session is a client that logged in and opened the channel the server offers.
type Session struct {
	// Key is the key the client logged in with.
	Key AllowedKey
	// Remote is the address of the client.
	Remote net.Addr
	// User is the user name the client gave.
	User string
	// ExtraData is what the client sent when it opened the channel (at most MaxChannelData bytes).
	ExtraData []byte
	// Channel is the channel; the handler reads and writes it.
	Channel ssh.Channel

	conn *ssh.ServerConn
	done chan struct{}
}

// Done is closed when the connection of the session ended, whoever ended it.
func (s *Session) Done() <-chan struct{} { return s.done }

// Close ends the connection of the session: its channel and everything on it.
func (s *Session) Close() error { return s.conn.Close() }

// MaxChannelData is the most bytes a client may send when it opens the channel.
const MaxChannelData = 256

// ServerOptions are what NewServer needs.
type ServerOptions struct {
	// HostKey is the host key of the server (LoadOrCreateHostKey).
	HostKey ssh.Signer
	// ChannelType is the one channel type the server offers (emetos-remotework); every other type is refused.
	ChannelType string
	// Keys returns the keys that may log in, read at each attempt, so a key taken out of the list stops working at once.
	Keys func() []AllowedKey
	// Handler serves a session. It runs on a goroutine of its own, and when it returns the connection is closed.
	Handler func(*Session)
	// OnLogin and OnRefused are told of every login and every refusal; either may be nil. They run on the goroutines of the
	// server and must not block.
	OnLogin   func(Login)
	OnRefused func(Refusal)
	// Logger gets a line for every login and every refusal; slog.Default() when nil.
	Logger *slog.Logger
	// Limiter counts the failed attempts of each address; NewLimiter(LimiterOptions{}) when nil.
	Limiter *Limiter
	// Now is the clock of the events; time.Now when nil. The deadlines are of the real clock.
	Now func() time.Time
	// HandshakeTimeout is how long a connection has to finish logging in (10 s), ChannelTimeout how long a client that logged
	// in has to open the channel (10 s), and MaxPending how many connections may be logging in at once (16).
	HandshakeTimeout time.Duration
	ChannelTimeout   time.Duration
	MaxPending       int
	// MaxAuthTries is how many failed attempts one connection may make (5).
	MaxAuthTries int
}

// Server is the SSH server of the machine that is controlled. Make one with NewServer and give it a listener with Serve.
type Server struct {
	opts    ServerOptions
	log     *slog.Logger
	limiter *Limiter
	pending chan struct{}

	mu        sync.Mutex
	closed    bool
	listeners map[net.Listener]struct{}
	conns     map[net.Conn]struct{}
	wg        sync.WaitGroup
}

// NewServer makes a server. It fails with SSHLoginServerInvalid when the host key, the channel type, the list of keys or the
// handler is missing.
func NewServer(opts ServerOptions) (*Server, error) {
	if opts.HostKey == nil || opts.ChannelType == "" || opts.Keys == nil || opts.Handler == nil {
		return nil, errors.New("SSHLoginServerInvalid", "a server needs a host key, a channel type, the list of keys and a handler")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.HandshakeTimeout <= 0 {
		opts.HandshakeTimeout = 10 * time.Second
	}
	if opts.ChannelTimeout <= 0 {
		opts.ChannelTimeout = 10 * time.Second
	}
	if opts.MaxPending <= 0 {
		opts.MaxPending = 16
	}
	if opts.MaxAuthTries <= 0 {
		opts.MaxAuthTries = 5
	}
	s := &Server{opts: opts, log: opts.Logger, limiter: opts.Limiter, listeners: map[net.Listener]struct{}{}, conns: map[net.Conn]struct{}{}}
	if s.log == nil {
		s.log = slog.Default()
	}
	if s.limiter == nil {
		s.limiter = NewLimiter(LimiterOptions{Now: opts.Now})
	}
	s.pending = make(chan struct{}, opts.MaxPending)
	return s, nil
}

// Serve accepts connections on ln until the listener is closed or Close is called, and returns nil then. It closes ln when it
// returns. A listener that fails otherwise gives the error SSHLoginAcceptFailed.
func (s *Server) Serve(ln net.Listener) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		_ = ln.Close()
		return nil
	}
	s.listeners[ln] = struct{}{}
	s.mu.Unlock()
	defer func() {
		_ = ln.Close()
		s.mu.Lock()
		delete(s.listeners, ln)
		s.mu.Unlock()
	}()
	for {
		c, err := ln.Accept()
		if err != nil {
			s.mu.Lock()
			closed := s.closed
			s.mu.Unlock()
			if closed || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return errors.NewErr("SSHLoginAcceptFailed", err)
		}
		if !s.track(c) {
			_ = c.Close()
			return nil
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer s.untrack(c)
			s.handle(c)
		}()
	}
}

// Close stops the listeners, ends every connection and waits for their goroutines, handlers included.
func (s *Server) Close() {
	s.mu.Lock()
	s.closed = true
	for ln := range s.listeners {
		_ = ln.Close()
	}
	for c := range s.conns {
		_ = c.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

// track notes a connection so that Close can end it; false when the server was closed.
func (s *Server) track(c net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.conns[c] = struct{}{}
	return true
}

func (s *Server) untrack(c net.Conn) {
	_ = c.Close()
	s.mu.Lock()
	delete(s.conns, c)
	s.mu.Unlock()
}

// connState is what one connection learned while it logged in.
type connState struct {
	mu       sync.Mutex
	failures int
	offered  string // fingerprint of the key offered last
	key      AllowedKey
	authed   bool
}

// handle serves one connection from the accept to the end.
func (s *Server) handle(c net.Conn) {
	addr := AddressKey(c.RemoteAddr())
	if blocked, first := s.limiter.Blocked(addr); blocked {
		if first {
			s.refuse(Refusal{Remote: c.RemoteAddr().String(), Address: addr, Reason: ReasonRateLimited, Detail: "too many failed attempts from this address; its connections are closed until the minute is over"})
		}
		return
	}
	select {
	case s.pending <- struct{}{}:
	default:
		s.log.Warn("remote login refused: too many connections are logging in", "remote", c.RemoteAddr().String())
		return
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { <-s.pending }) }
	defer release()

	st := &connState{}
	if err := c.SetDeadline(time.Now().Add(s.opts.HandshakeTimeout)); err != nil {
		s.log.Warn("remote login: cannot set the deadline", "err", err)
		return
	}
	sc, chans, reqs, err := ssh.NewServerConn(c, s.config(c, addr, st))
	if err != nil {
		st.mu.Lock()
		failures := st.failures
		st.mu.Unlock()
		if failures == 0 {
			s.limiter.Fail(addr)
			s.refuse(Refusal{Remote: c.RemoteAddr().String(), Address: addr, Reason: ReasonHandshakeFailed, Detail: err.Error()})
		}
		return
	}
	defer func() { _ = sc.Close() }()
	if err := c.SetDeadline(time.Time{}); err != nil {
		return
	}
	release()

	st.mu.Lock()
	key := st.key
	st.mu.Unlock()
	login := Login{Remote: c.RemoteAddr().String(), User: sc.User(), Key: key, At: s.opts.Now()}
	s.log.Info("remote login", "remote", login.Remote, "user", login.User, "key", key.Fingerprint(), "comment", key.Comment)
	if s.opts.OnLogin != nil {
		s.opts.OnLogin(login)
	}
	go s.refuseGlobalRequests(reqs)
	s.serveChannels(sc, chans, key, addr)
}

// config makes the configuration of one connection: public keys only, from the list, with the attempts of the connection
// counted for the limiter.
func (s *Server) config(c net.Conn, addr string, st *connState) *ssh.ServerConfig {
	remote := c.RemoteAddr().String()
	cfg := &ssh.ServerConfig{
		MaxAuthTries:  s.opts.MaxAuthTries,
		ServerVersion: "SSH-2.0-Emetos",
		// No PasswordCallback, KeyboardInteractiveCallback or GSSAPI, and NoClientAuth stays false: public keys are the only way in.
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			st.mu.Lock()
			st.offered = ssh.FingerprintSHA256(key)
			st.mu.Unlock()
			if _, ok := s.find(key); !ok {
				return nil, errors.New("SSHLoginKeyNotListed", "the key is not in the list of allowed keys", "fingerprint", ssh.FingerprintSHA256(key))
			}
			return &ssh.Permissions{}, nil
		},
		// The signature was checked: the key that proved itself is the one that logs in, and it must still be in the list.
		VerifiedPublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey, perms *ssh.Permissions, _ string) (*ssh.Permissions, error) {
			allowed, ok := s.find(key)
			if !ok {
				return nil, errors.New("SSHLoginKeyNotListed", "the key is not in the list of allowed keys", "fingerprint", ssh.FingerprintSHA256(key))
			}
			st.mu.Lock()
			st.key, st.authed = allowed, true
			st.mu.Unlock()
			return perms, nil
		},
		AuthLogCallback: func(conn ssh.ConnMetadata, method string, err error) {
			if err == nil || method == "none" {
				return
			}
			st.mu.Lock()
			st.failures++
			offered := st.offered
			st.mu.Unlock()
			r := Refusal{Remote: remote, Address: addr, User: conn.User(), Method: method, Reason: refusalReason(method, err)}
			if method == "publickey" {
				r.KeyFingerprint = offered
			}
			if s.limiter.Fail(addr) {
				_ = c.Close() // the limit is reached: the connection ends here
			}
			s.refuse(r)
		},
	}
	cfg.AddHostKey(s.opts.HostKey)
	return cfg
}

// refusalReason names the reason a login attempt of a method failed with err.
func refusalReason(method string, err error) string {
	switch method {
	case "publickey":
		if appErr, ok := errors.FromError(err); ok && appErr.Tag == "SSHLoginKeyNotListed" {
			return ReasonKeyNotListed
		}
		return ReasonKeyRefused
	case "password":
		return ReasonPasswordRefused
	default:
		return ReasonMethodRefused
	}
}

// find looks the key up in the list by its wire bytes.
func (s *Server) find(key ssh.PublicKey) (AllowedKey, bool) {
	for _, k := range s.opts.Keys() {
		if SameKey(k.Key, key) {
			return k, true
		}
	}
	return AllowedKey{}, false
}

// refuse logs a refusal and tells the owner of the server.
func (s *Server) refuse(r Refusal) {
	if r.At.IsZero() {
		r.At = s.opts.Now()
	}
	s.log.Warn("remote login refused", "remote", r.Remote, "user", r.User, "method", r.Method, "reason", r.Reason, "key", r.KeyFingerprint, "detail", r.Detail)
	if s.opts.OnRefused != nil {
		s.opts.OnRefused(r)
	}
}

// refuseGlobalRequests answers every global request of the client (keepalives, forwarding) with a refusal.
func (s *Server) refuseGlobalRequests(reqs <-chan *ssh.Request) {
	for r := range reqs {
		if r.WantReply {
			_ = r.Reply(false, nil)
		}
	}
}

// serveChannels accepts the one channel of the type the server offers and refuses everything else; a client that opens a
// channel of another type, or that opens none in time, is disconnected.
func (s *Server) serveChannels(sc *ssh.ServerConn, chans <-chan ssh.NewChannel, key AllowedKey, addr string) {
	var opened atomic.Bool
	timer := time.AfterFunc(s.opts.ChannelTimeout, func() {
		if !opened.Load() {
			s.refuse(Refusal{Remote: sc.RemoteAddr().String(), Address: addr, User: sc.User(), KeyFingerprint: key.Fingerprint(), Reason: ReasonNoChannel, Detail: "no channel was opened in time"})
			_ = sc.Close()
		}
	})
	defer timer.Stop()
	var handlers sync.WaitGroup
	defer handlers.Wait()
	for nc := range chans {
		switch {
		case nc.ChannelType() != s.opts.ChannelType:
			_ = nc.Reject(ssh.UnknownChannelType, "this server offers one channel type only")
			s.refuse(Refusal{Remote: sc.RemoteAddr().String(), Address: addr, User: sc.User(), KeyFingerprint: key.Fingerprint(), Reason: ReasonChannelType, Detail: nc.ChannelType()})
			_ = sc.Close()
		case opened.Load():
			_ = nc.Reject(ssh.ResourceShortage, "one channel for each connection")
		case len(nc.ExtraData()) > MaxChannelData:
			_ = nc.Reject(ssh.ConnectionFailed, "the data of the channel is too large")
			_ = sc.Close()
		default:
			ch, creqs, err := nc.Accept()
			if err != nil {
				s.log.Warn("remote login: the channel was not accepted", "err", err)
				_ = sc.Close()
				continue
			}
			opened.Store(true)
			sess := &Session{Key: key, Remote: sc.RemoteAddr(), User: sc.User(), ExtraData: nc.ExtraData(), Channel: ch, conn: sc, done: make(chan struct{})}
			go func() {
				_ = sc.Wait()
				close(sess.done)
			}()
			go s.refuseRequests(sc, creqs, sess, addr)
			handlers.Add(1)
			go func() {
				defer handlers.Done()
				defer func() { _ = sc.Close() }()
				s.opts.Handler(sess)
				_ = ch.Close()
			}()
		}
	}
}

// refuseRequests answers every request on the channel (shell, exec, pty-req, env, subsystem, window-change, forwarding) with a
// refusal and disconnects the client: Emetos clients make none.
func (s *Server) refuseRequests(sc *ssh.ServerConn, reqs <-chan *ssh.Request, sess *Session, addr string) {
	for r := range reqs {
		if r.WantReply {
			_ = r.Reply(false, nil)
		}
		s.refuse(Refusal{Remote: sess.Remote.String(), Address: addr, User: sess.User, KeyFingerprint: sess.Key.Fingerprint(), Reason: ReasonRequest, Detail: r.Type})
		_ = sc.Close()
	}
}
