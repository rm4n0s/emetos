package remotework

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/keyqr"
	"github.com/rm4n0s/emetos/extensions/internal/sshlogin"
	"github.com/rm4n0s/emetos/extensions/remotework/login"
	"github.com/rm4n0s/emetos/extensions/remotework/transport"
)

// HubOptions are what NewHub takes; every field has a default, so the editor passes none and the tests pass their own.
type HubOptions struct {
	// StateDir gives the folder of the state files ($XDG_STATE_HOME/emetos); the default reads the environment each time.
	StateDir func() (string, error)
	// Listen opens the TCP listener of the SSH server on the port; the default listens on every interface. Tests listen on the
	// loopback with an ephemeral port.
	Listen func(port int) (net.Listener, error)
	// Cert overrides the SHA-256 fingerprint of the QUIC certificate that the grant carries. The default is the fingerprint of the
	// certificate the hub's transport made at start, empty when the hub has no QUIC (the session then runs over the SSH channel).
	// A test gives a stand-in.
	Cert func() []byte
	// OnSession is called, on the goroutine that serves the SSH channel, after the grant was sent. Session.Conn is the session's
	// transport, to which the headless view is attached (step 15.4). It must not block; the session ends with the channel.
	OnSession func(*Session)
	// NoTransport leaves the SSH channel to the caller: no transport is attached to a session and nothing reads the channel (the
	// tests of the login use it).
	NoTransport bool
	// NoView leaves the messages of a session to the caller: no headless view is made for it and nothing reads its transport (the
	// tests of the transport read the host's side themselves).
	NoView bool
	// Tab are the knobs of the remote window tab (step 15.4); every field has a default.
	Tab TabOptions
	// Camera finds and opens the cameras that Scan QR Code reads (step 15.5). The editor passes the Source of the real cameras
	// (extensions.All); nil means there is no way to read a camera, and the button says so. A test passes a keyqr.FakeSource: nothing
	// in the tests of this package opens a real camera.
	Camera keyqr.Source
	// NoQUIC makes the transport use the SSH channel only: the hub opens no UDP socket and the grant has no fingerprint.
	NoQUIC bool
	// ListenUDP opens the UDP socket of QUIC on the port the SSH listener got; the default listens on every interface. When it
	// fails the hub logs it and runs without QUIC.
	ListenUDP func(port int) (net.PacketConn, error)
	// Transport, if not nil, changes the options of the hub's transport host before it starts (tests shorten its timeouts).
	Transport func(*transport.HostOptions)
	// Limiter counts the failed attempts of each address; the default is 5 a minute.
	Limiter *sshlogin.Limiter
	// Logger is the log of the hub; slog.Default() when nil.
	Logger *slog.Logger
	// Now is the clock of the times and of the throttle of notifications; time.Now when nil.
	Now func() time.Time
	// After calls f after d and returns the function that stops it (the idle timeouts); the default uses time.AfterFunc.
	After func(d time.Duration, f func()) (stop func() bool)
	// Random is the source of the session tokens; crypto/rand.Reader when nil.
	Random io.Reader
	// HandshakeTimeout and ChannelTimeout are the deadlines of the SSH server (10 s each by default).
	HandshakeTimeout, ChannelTimeout time.Duration
}

// workspaceHost is a workspace the extension is active in: where the hub shows banners and notifications.
type workspaceHost struct {
	folder string
	api    extension.API
}

// Hub is what the instances of the extension share: remote-work.json, the host key, the listener, the sessions. It is made once
// for the process. Its methods may be called from any goroutine; what it shows in a workspace it posts to the UI thread of that
// workspace.
type Hub struct {
	opts HubOptions
	log  *slog.Logger

	recMu sync.Mutex // serializes reconcile

	mu         sync.Mutex
	loaded     bool
	dir        string
	cfg        Config
	keys       []sshlogin.AllowedKey
	loadErr    error
	hostKey    ssh.Signer
	workspaces map[string][]*workspaceHost
	sessions   map[*Session]struct{}
	srv        *sshlogin.Server
	host       *transport.Host
	srvPort    int
	addr       net.Addr
	listenErr  error
	notified   map[string]time.Time
	listeners  map[int]func()
	nextID     int

	camMu  sync.Mutex
	camera keyqr.Source
}

// NewHub makes a hub that listens on nothing until a workspace has Remote Work on.
func NewHub(opts HubOptions) *Hub {
	h := &Hub{opts: opts, camera: opts.Camera, log: opts.Logger, workspaces: map[string][]*workspaceHost{}, sessions: map[*Session]struct{}{}, notified: map[string]time.Time{}, listeners: map[int]func(){}}
	if h.log == nil {
		h.log = slog.Default()
	}
	if h.opts.StateDir == nil {
		h.opts.StateDir = stateDirFromEnv
	}
	if h.opts.Listen == nil {
		h.opts.Listen = func(port int) (net.Listener, error) { return net.Listen("tcp", ":"+strconv.Itoa(port)) }
	}
	if h.opts.ListenUDP == nil {
		h.opts.ListenUDP = func(port int) (net.PacketConn, error) { return net.ListenPacket("udp", ":"+strconv.Itoa(port)) }
	}
	if h.opts.Now == nil {
		h.opts.Now = time.Now
	}
	if h.opts.After == nil {
		h.opts.After = func(d time.Duration, f func()) func() bool { return time.AfterFunc(d, f).Stop }
	}
	if h.opts.Random == nil {
		h.opts.Random = rand.Reader
	}
	if h.opts.Limiter == nil {
		h.opts.Limiter = sshlogin.NewLimiter(sshlogin.LimiterOptions{Now: h.opts.Now})
	}
	return h
}

// stateDirFromEnv is $XDG_STATE_HOME/emetos, or ~/.local/state/emetos when the variable is unset or not absolute.
func stateDirFromEnv() (string, error) {
	base := os.Getenv("XDG_STATE_HOME")
	if !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", errors.NewErr("RemoteWorkStateDirUnknown", err)
		}
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(filepath.Clean(base), "emetos"), nil
}

// HostsPath is the file where a client pins host keys (remote-work-hosts.json), for the tab of step 15.4.
func (h *Hub) HostsPath() (string, error) {
	dir, err := h.opts.StateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, hostsFileName), nil
}

// load reads remote-work.json the first time it is needed; the caller holds mu. A file that cannot be used is remembered
// (LoadError) and leaves Remote Work off and the page read-only.
func (h *Hub) load() {
	if h.loaded {
		return
	}
	h.loaded = true
	dir, err := h.opts.StateDir()
	if err != nil {
		h.loadErr, h.cfg = err, DefaultConfig()
		return
	}
	h.dir = dir
	cfg, err := loadConfig(dir)
	if err != nil {
		h.log.Warn("remote-work.json is not used; Remote Work stays off until it is fixed", "err", err)
		h.loadErr, h.cfg = err, DefaultConfig()
		return
	}
	h.cfg = cfg
	h.keys = cfg.AllowedKeys()
}

// Register tells the hub that the extension is active in the workspace, so that it can show a banner and notifications there,
// and starts the listener when the workspace has Remote Work on. The function it returns undoes it: it ends the sessions of the
// workspace and stops the listener when no workspace is left. A workspace with no folder is not registered.
func (h *Hub) Register(api extension.API) (unregister func()) {
	folder := filepath.Clean(api.Scope().Folder)
	if api.Scope().Folder == "" {
		return func() {}
	}
	w := &workspaceHost{folder: folder, api: api}
	h.mu.Lock()
	h.load()
	h.workspaces[folder] = append(h.workspaces[folder], w)
	h.mu.Unlock()
	h.reconcile()
	var once sync.Once
	return func() {
		once.Do(func() {
			h.mu.Lock()
			list := h.workspaces[folder]
			for i, x := range list {
				if x == w {
					list = append(list[:i:i], list[i+1:]...)
					break
				}
			}
			if len(list) == 0 {
				delete(h.workspaces, folder)
			} else {
				h.workspaces[folder] = list
			}
			gone := len(list) == 0
			var ended []*Session
			if gone {
				for s := range h.sessions {
					if s.folder == folder {
						ended = append(ended, s)
					}
				}
			}
			h.mu.Unlock()
			for _, s := range ended {
				s.End(reasonWorkspaceClosed, "the workspace was closed")
			}
			h.reconcile()
			h.changed()
		})
	}
}

// OnChange calls fn, from any goroutine and with no lock held, after the state the page shows changed; the function it
// returns stops the calls.
func (h *Hub) OnChange(fn func()) (stop func()) {
	h.mu.Lock()
	h.nextID++
	id := h.nextID
	h.listeners[id] = fn
	h.mu.Unlock()
	return func() {
		h.mu.Lock()
		delete(h.listeners, id)
		h.mu.Unlock()
	}
}

func (h *Hub) changed() {
	h.mu.Lock()
	fns := make([]func(), 0, len(h.listeners))
	for _, f := range h.listeners {
		fns = append(fns, f)
	}
	h.mu.Unlock()
	for _, f := range fns {
		f()
	}
}

// onWorkspaces are the registered workspaces that have Remote Work on, by folder; the caller holds mu.
func (h *Hub) onWorkspaces() []*workspaceHost {
	var out []*workspaceHost
	for folder, list := range h.workspaces {
		if h.cfg.On(folder) && len(list) > 0 {
			out = append(out, list[0])
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].folder < out[j].folder })
	return out
}

// displayNames gives each workspace that is on its name: the name of its folder, or its whole path when two share the name.
func displayNames(list []*workspaceHost) map[*workspaceHost]string {
	count := map[string]int{}
	for _, w := range list {
		count[filepath.Base(w.folder)]++
	}
	out := make(map[*workspaceHost]string, len(list))
	for _, w := range list {
		if n := filepath.Base(w.folder); count[n] == 1 {
			out[w] = n
		} else {
			out[w] = w.folder
		}
	}
	return out
}

// reconcile makes the listener what the configuration says: open on the port while at least one registered workspace has Remote
// Work on, closed otherwise, with the sessions of a closed listener ended.
func (h *Hub) reconcile() {
	h.recMu.Lock()
	defer h.recMu.Unlock()
	h.mu.Lock()
	want := h.loadErr == nil && len(h.onWorkspaces()) > 0
	running, port, cfgPort := h.srv != nil, h.srvPort, h.cfg.Port
	h.mu.Unlock()
	switch {
	case running && want && port == cfgPort:
		return
	case running:
		text := "Remote Work was turned off"
		if want {
			text = "the port changed"
		}
		h.stopListening(reasonOff, text)
	}
	if want {
		h.startListening(cfgPort)
	}
	h.changed()
}

// startListening makes the host key if it is missing, opens the listener and serves SSH on it. A failure is kept (the page shows
// it) and told to the workspaces that have Remote Work on.
func (h *Hub) startListening(port int) {
	fail := func(err error) {
		h.mu.Lock()
		h.listenErr = err
		on := h.onWorkspaces()
		h.mu.Unlock()
		h.log.Error("Remote Work cannot listen", "port", port, "err", err)
		for _, w := range on {
			w := w
			w.api.Post(func() {
				w.api.Notifications().Error("Remote Work cannot listen on port " + strconv.Itoa(port) + ": " + err.Error())
			})
		}
	}
	signer, err := h.HostKey()
	if err != nil {
		fail(err)
		return
	}
	srv, err := sshlogin.NewServer(sshlogin.ServerOptions{
		HostKey: signer, ChannelType: login.ChannelType, Keys: h.allowedKeys, Handler: h.serve, OnRefused: h.refused,
		Logger: h.log, Limiter: h.opts.Limiter, Now: h.opts.Now, HandshakeTimeout: h.opts.HandshakeTimeout, ChannelTimeout: h.opts.ChannelTimeout,
	})
	if err != nil {
		fail(err)
		return
	}
	ln, err := h.opts.Listen(port)
	if err != nil {
		fail(errors.NewErr("RemoteWorkListenFailed", err).SetMetadata("port", port))
		return
	}
	host, err := h.startTransport(ln)
	if err != nil {
		_ = ln.Close() // the listener never served
		fail(err)
		return
	}
	h.mu.Lock()
	h.srv, h.host, h.srvPort, h.addr, h.listenErr = srv, host, port, ln.Addr(), nil
	h.mu.Unlock()
	h.log.Info("Remote Work listens", "addr", ln.Addr().String(), "quic", host != nil && host.Addr() != nil)
	go func() {
		if err := srv.Serve(ln); err != nil {
			h.log.Error("Remote Work stopped listening", "err", err)
		}
	}()
}

// startTransport starts the transport of the sessions next to the SSH listener: QUIC on the UDP port of the same number, which
// the hub gives up (and logs) if it cannot open, leaving the SSH channel to carry the sessions. It returns nil when the hub is
// made without a transport.
func (h *Hub) startTransport(ln net.Listener) (*transport.Host, error) {
	if h.opts.NoTransport {
		return nil, nil
	}
	var pc net.PacketConn
	if tcp, ok := ln.Addr().(*net.TCPAddr); ok && !h.opts.NoQUIC {
		var err error
		if pc, err = h.opts.ListenUDP(tcp.Port); err != nil {
			h.log.Warn("Remote Work: QUIC is not available, sessions will run over the SSH channel", "port", tcp.Port, "err", err)
			pc = nil
		}
	}
	o := transport.HostOptions{
		PacketConn: pc, Lookup: h.lookupConn, Clock: transport.FuncClock{NowFunc: h.opts.Now, After: h.opts.After}, Logger: h.log,
		OnRefused: func(kind transport.Kind, remote string, err error) {
			h.log.Warn("Remote Work: a transport connection was refused", "kind", kind.String(), "remote", remote, "err", err)
		},
	}
	if h.opts.Transport != nil {
		h.opts.Transport(&o)
	}
	host, err := transport.NewHost(context.Background(), o)
	if err != nil {
		if pc != nil {
			_ = pc.Close() // the host never took it
		}
		return nil, err
	}
	return host, nil
}

// lookupConn is the transport's way to find the session of a token.
func (h *Hub) lookupConn(token []byte) (*transport.HostConn, bool) {
	s, ok := h.SessionByToken(token)
	if !ok || s.conn == nil {
		return nil, false
	}
	return s.conn, true
}

// stopListening ends every session with the reason, then closes the listener and what is left of the connections.
func (h *Hub) stopListening(reason Reason, text string) {
	h.mu.Lock()
	srv, host := h.srv, h.host
	h.srv, h.host, h.addr = nil, nil, nil
	ended := make([]*Session, 0, len(h.sessions))
	for s := range h.sessions {
		ended = append(ended, s)
	}
	h.mu.Unlock()
	for _, s := range ended {
		s.End(reason, text)
	}
	for _, s := range ended {
		select {
		case <-s.closed:
		case <-time.After(3 * time.Second):
		}
	}
	if host != nil {
		host.Close()
	}
	if srv != nil {
		srv.Close()
		h.log.Info("Remote Work stopped listening")
	}
}

// Close ends every session and stops the listener; the process calls it when it quits.
func (h *Hub) Close() {
	h.recMu.Lock()
	defer h.recMu.Unlock()
	h.stopListening(reasonOff, "Emetos quit")
}

// allowedKeys is the list the SSH server checks each attempt against.
func (h *Hub) allowedKeys() []sshlogin.AllowedKey {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.keys
}

// HostKey returns the host key of this machine, made the first time it is needed and kept with mode 0600. It fails with the
// tags of sshlogin.LoadOrCreateHostKey.
func (h *Hub) HostKey() (ssh.Signer, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.load()
	if h.hostKey != nil {
		return h.hostKey, nil
	}
	if h.dir == "" {
		return nil, errors.New("RemoteWorkNoStateDir", "the state folder is unknown")
	}
	s, err := sshlogin.LoadOrCreateHostKey(filepath.Join(h.dir, hostKeyFileName))
	if err != nil {
		return nil, err
	}
	h.hostKey = s
	return s, nil
}

// SessionByToken finds the live session that has the token, comparing in constant time; the transport of step 15.3 calls it with
// the token a client presents. A token of an ended session is not found: it expired with the session.
func (h *Hub) SessionByToken(token []byte) (*Session, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	var found *Session
	for s := range h.sessions {
		if subtle.ConstantTimeCompare(s.token[:], token) == 1 && !s.over.Load() {
			found = s
		}
	}
	return found, found != nil
}

// Addr is the address the listener is on, or nil while it is closed.
func (h *Hub) Addr() net.Addr {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.addr
}

// Sessions lists the live sessions, the oldest first.
func (h *Hub) Sessions() []*Session {
	h.mu.Lock()
	out := make([]*Session, 0, len(h.sessions))
	for s := range h.sessions {
		out = append(out, s)
	}
	h.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].Started.Before(out[j].Started) })
	return out
}
