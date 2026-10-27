package remotework

import (
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"

	"github.com/rm4n0s/emetos/extension"
	"github.com/rm4n0s/emetos/extensions/internal/sshlogin"
	"github.com/rm4n0s/emetos/extensions/remotework/login"
	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
	"github.com/rm4n0s/emetos/extensions/remotework/transport"
)

// Reason is why a session ends, as the Disconnect message says it to the client.
type Reason = protocol.Reason

// The reasons the host ends a session with.
const (
	reasonHost            = protocol.ReasonHost
	reasonIdle            = protocol.ReasonIdle
	reasonOff             = protocol.ReasonOff
	reasonReplaced        = protocol.ReasonReplaced
	reasonWorkspaceClosed = protocol.ReasonWorkspaceClosed
	reasonProtocol        = protocol.ReasonProtocol
	reasonClosed          = protocol.ReasonClosed
)

// Session is a client that logged in and was granted a workspace: the key that did it, the token of the session, and the SSH
// channel. It lives until the client disconnects, the user presses Disconnect, the idle timeout passes, the key is taken out of
// the list, Remote Work is turned off or the workspace closes.
type Session struct {
	// Key is the key the client logged in with and Remote its address.
	Key    sshlogin.AllowedKey
	Remote string
	// Workspace is the name the client was granted and Started when the session began.
	Workspace string
	Started   time.Time

	hub    *Hub
	folder string
	token  [login.TokenSize]byte
	ssh    *sshlogin.Session
	// conn is the transport of the session (nil when the hub has none), made before the session is visible to anyone.
	conn *transport.HostConn

	// over is true from the moment the session is asked to end; the token is not valid from then on.
	over atomic.Bool

	mu     sync.Mutex
	ended  bool
	reason Reason
	text   string
	idle   func() bool
	banner extension.BannerHandle

	endCh  chan struct{} // closed when the session is asked to end
	closed chan struct{} // closed when its connection is closed
}

// Token is a copy of the session token: 256 random bits, in memory only, valid until the session ends.
func (s *Session) Token() []byte { return append([]byte(nil), s.token[:]...) }

// Channel is the SSH channel of the session. After the grant it carries the messages of package protocol when the session runs over
// SSH (the TCP fallback); the transport reads and writes it, and the hub writes the Disconnect message on it when it ends the
// session. A session with a transport is used through Conn, not this.
func (s *Session) Channel() ssh.Channel { return s.ssh.Channel }

// Conn is the transport of the session: what the headless view of step 15.4 sends and receives the messages of package
// protocol through, over QUIC or the SSH channel. It is nil when the hub was made with HubOptions.NoTransport. It ends with the
// session.
func (s *Session) Conn() *transport.HostConn { return s.conn }

// hostSession is the Session as the transport sees it.
type hostSession struct{ s *Session }

func (h hostSession) Token() []byte                      { return h.s.Token() }
func (h hostSession) Channel() io.ReadWriteCloser        { return h.s.ssh.Channel }
func (h hostSession) Touch()                             { h.s.Touch() }
func (h hostSession) End(r protocol.Reason, text string) { h.s.End(r, text) }
func (h hostSession) Done() <-chan struct{}              { return h.s.endCh }

// Done is closed when the session is over: it was asked to end, or its connection closed.
func (s *Session) Done() <-chan struct{} { return s.endCh }

// Reason is why the session ended, and the text that went with it; false while it lives.
func (s *Session) Reason() (Reason, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.reason, s.text, s.ended
}

// Touch tells the session that there was traffic: the idle timeout starts again. The transport of step 15.3 calls it for every
// message.
func (s *Session) Touch() {
	d := s.hub.idleTimeout() // before mu: the hub's lock is never taken under a session's
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ended {
		return
	}
	s.armIdleLocked(d)
}

// armIdleLocked starts the idle timer anew; the caller holds mu.
func (s *Session) armIdleLocked(d time.Duration) {
	if s.idle != nil {
		s.idle()
	}
	s.idle = s.hub.opts.After(d, func() { s.End(reasonIdle, "no activity for "+d.String()) })
}

// End ends the session: the token is no longer valid, the client is told why with a Disconnect message, the connection is closed
// and the banner goes. It does nothing the second time.
func (s *Session) End(reason Reason, text string) {
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return
	}
	s.ended, s.reason, s.text = true, reason, text
	s.over.Store(true)
	if s.idle != nil {
		s.idle()
		s.idle = nil
	}
	banner := s.banner
	s.banner = nil
	s.mu.Unlock()
	close(s.endCh)
	if banner != nil {
		if w := s.hub.workspaceFor(s.folder); w != nil {
			w.api.Post(banner.Dispose)
		} else {
			banner.Dispose()
		}
	}
	go func() {
		defer close(s.closed)
		sent := make(chan struct{})
		go func() {
			defer close(sent)
			// the client is told why; a client that does not read must not hold the end of the session
			if s.conn != nil {
				s.conn.Shutdown(reason, text) // says it on the SSH channel and by closing QUIC, without breaking a message in flight
				return
			}
			_ = protocol.NewWriter(s.ssh.Channel).Write(&protocol.Disconnect{Reason: reason, Text: text})
		}()
		select {
		case <-sent:
		case <-time.After(2 * time.Second):
		}
		_ = s.ssh.Close()
	}()
}

// workspaceFor returns the first instance registered for the folder.
func (h *Hub) workspaceFor(folder string) *workspaceHost {
	h.mu.Lock()
	defer h.mu.Unlock()
	if list := h.workspaces[folder]; len(list) > 0 {
		return list[0]
	}
	return nil
}

// idleTimeout is the idle timeout of the configuration.
func (h *Hub) idleTimeout() time.Duration {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cfg.IdleTimeout()
}

// serve is the handler of the SSH server: it picks the workspace, makes the session and its token, sends the grant, shows the
// banner and the notification, and lives until the session ends.
func (h *Hub) serve(ss *sshlogin.Session) {
	req, err := login.ParseRequest(ss.ExtraData)
	if err != nil {
		h.log.Warn("Remote Work: a request that is not one", "remote", ss.Remote.String(), "err", err)
		return
	}
	h.mu.Lock()
	on := h.onWorkspaces()
	names := displayNames(on)
	var chosen *workspaceHost
	var list []string
	for _, w := range on {
		list = append(list, names[w])
		if req.Workspace != "" && names[w] == req.Workspace {
			chosen = w
		}
	}
	status := login.StatusOK
	switch {
	case len(on) == 0:
		status = login.StatusNone
	case req.Workspace != "" && chosen == nil:
		status = login.StatusUnknown
	case req.Workspace == "" && len(on) > 1:
		status = login.StatusAmbiguous
	case req.Workspace == "":
		chosen = on[0]
	}
	if status != login.StatusOK {
		h.mu.Unlock()
		if status == login.StatusNone {
			list = nil
		}
		if err := login.WriteGrant(ss.Channel, login.Grant{Status: status, Workspaces: list}); err != nil {
			h.log.Warn("Remote Work: the refusal was not sent", "err", err)
		}
		h.log.Info("Remote Work: no session granted", "remote", ss.Remote.String(), "status", status)
		return
	}
	s := &Session{
		Key: ss.Key, Remote: ss.Remote.String(), Workspace: names[chosen], Started: h.opts.Now(),
		hub: h, folder: chosen.folder, ssh: ss, endCh: make(chan struct{}), closed: make(chan struct{}),
	}
	token, err := newToken(h.opts.Random)
	if err != nil {
		h.mu.Unlock()
		h.log.Error("Remote Work: no token could be made", "err", err)
		return
	}
	s.token = token
	host := h.host
	if host != nil {
		// before the session is visible: the transport must know it by the time the client presents the token
		s.conn = host.Attach(hostSession{s})
	}
	var replaced []*Session
	for old := range h.sessions {
		if old.folder == s.folder {
			replaced = append(replaced, old)
		}
	}
	h.sessions[s] = struct{}{}
	h.mu.Unlock()
	for _, old := range replaced {
		old.End(reasonReplaced, "another client logged in to this workspace")
	}
	defer h.sessionOver(s)

	var cert []byte
	switch {
	case h.opts.Cert != nil:
		cert = h.opts.Cert()
	case host != nil:
		cert = host.Fingerprint()
	}
	grant := login.Grant{Status: login.StatusOK, Workspace: s.Workspace, Token: s.Token(), CertSHA256: cert}
	if err := login.WriteGrant(ss.Channel, grant); err != nil {
		h.log.Warn("Remote Work: the grant was not sent", "err", err)
		s.End(reasonClosed, "")
		<-s.closed
		return
	}
	if s.conn != nil {
		s.conn.Start()
	}
	d := h.idleTimeout()
	s.mu.Lock()
	s.armIdleLocked(d)
	s.mu.Unlock()
	h.log.Info("Remote Work session", "remote", s.Remote, "key", s.Key.Fingerprint(), "comment", s.Key.Comment, "workspace", s.Workspace)
	h.showSession(chosen, s)
	h.changed()
	// the view of the workspace for the client, until the session ends
	viewDone := make(chan struct{})
	go func() {
		defer close(viewDone)
		if !h.opts.NoView {
			h.runView(chosen, s)
		}
	}()
	if h.opts.OnSession != nil {
		h.opts.OnSession(s)
	}
	select {
	case <-ss.Done():
		s.End(reasonClosed, "")
	case <-s.endCh:
	}
	<-s.closed
	<-viewDone
}

// showSession shows the banner and the notification of a login in the workspace.
func (h *Hub) showSession(w *workspaceHost, s *Session) {
	w.api.Post(func() {
		if _, _, ended := s.Reason(); ended {
			return
		}
		banner, err := w.api.Banners().Show(extension.Banner{
			Text: "Remote Work: " + s.Key.Name() + " connected", Icon: "remote",
			Actions: []extension.BannerAction{{Label: "Disconnect", Run: func() { s.End(reasonHost, "the user of the host pressed Disconnect") }}},
		})
		if err != nil {
			h.log.Warn("Remote Work: the banner was not shown", "err", err)
		} else {
			s.mu.Lock()
			if s.ended {
				s.mu.Unlock()
				banner.Dispose()
			} else {
				s.banner = banner
				s.mu.Unlock()
			}
		}
		w.api.Notifications().Info("Remote Work: " + s.Key.Name() + " logged in from " + hostOf(s.Remote) + ".")
	})
}

// sessionOver forgets a session that ended.
func (h *Hub) sessionOver(s *Session) {
	h.mu.Lock()
	delete(h.sessions, s)
	h.mu.Unlock()
	h.changed()
}

// hostOf is the IP of a host:port address.
func hostOf(addr string) string {
	if host, _, err := net.SplitHostPort(addr); err == nil {
		return host
	}
	return addr
}

// refusalWords say a refusal to the user.
func refusalWords(r sshlogin.Refusal) string {
	switch r.Reason {
	case sshlogin.ReasonKeyNotListed:
		return "its key is not in the list (" + r.KeyFingerprint + ")"
	case sshlogin.ReasonKeyRefused:
		return "its key was refused"
	case sshlogin.ReasonPasswordRefused:
		return "it tried a password, which is never accepted"
	case sshlogin.ReasonMethodRefused:
		return "it tried a way of logging in that is not accepted (" + r.Method + ")"
	case sshlogin.ReasonRateLimited:
		return "too many attempts from this address failed; it is blocked for a minute"
	case sshlogin.ReasonChannelType:
		return "it asked for a channel of type " + r.Detail
	case sshlogin.ReasonRequest:
		return "it sent a request (" + r.Detail + ") that is never served"
	case sshlogin.ReasonNoChannel:
		return "it logged in and opened nothing"
	}
	return r.Reason
}

// notifyThrottle is how often one address may raise a notification of a refused attempt.
const notifyThrottle = time.Minute

// refused is told by the SSH server of every refusal (it logged it already): the workspaces that have Remote Work on get one
// notification for each address a minute, for a login attempt that was refused (not for one that never began).
func (h *Hub) refused(r sshlogin.Refusal) {
	// A connection that ends before it tries to log in is also what a client does on first use, while its user compares the
	// fingerprint: it is in the log and counts for the rate limit, but it is not a refused attempt to tell the user about.
	if r.Reason == sshlogin.ReasonHandshakeFailed {
		return
	}
	now := h.opts.Now()
	// the block of an address is told on its own, whatever else the address raised a moment before
	key := r.Address
	if r.Reason == sshlogin.ReasonRateLimited {
		key += "|blocked"
	}
	h.mu.Lock()
	if last, ok := h.notified[key]; ok && now.Sub(last) < notifyThrottle {
		h.mu.Unlock()
		return
	}
	if len(h.notified) >= 256 {
		for k, t := range h.notified {
			if now.Sub(t) >= notifyThrottle {
				delete(h.notified, k)
			}
		}
		if len(h.notified) >= 256 {
			h.mu.Unlock()
			return
		}
	}
	h.notified[key] = now
	on := h.onWorkspaces()
	h.mu.Unlock()
	msg := "Remote Work refused a login from " + hostOf(r.Remote) + ": " + refusalWords(r) + "."
	for _, w := range on {
		w := w
		w.api.Post(func() { w.api.Notifications().Warn(msg) })
	}
}

// newToken makes a session token of TokenSize random bytes. It fails with RemoteWorkTokenFailed.
func newToken(random io.Reader) ([login.TokenSize]byte, error) {
	var t [login.TokenSize]byte
	if _, err := io.ReadFull(random, t[:]); err != nil {
		return t, errors.NewErr("RemoteWorkTokenFailed", err)
	}
	return t, nil
}
