package transport

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"io"
	"log/slog"
	"math/big"
	"net"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
)

// ALPN is the application protocol of the TLS inside QUIC; a client that offers another is refused by the handshake.
const ALPN = "emetos-remotework/1"

// The defaults of the host.
const (
	// DefaultFirstLink is how long a session may exist without any transport: the client has 3 s for QUIC and then falls back,
	// so this leaves it ample time.
	DefaultFirstLink = 20 * time.Second
	// DefaultHelloTimeout is how long a connection may take to present its token, and the client to read the answer.
	DefaultHelloTimeout = 5 * time.Second
	// DefaultMaxIdle and DefaultKeepAlive are the QUIC idle timeout and keep-alive period: a dead path is noticed in 15 s and the
	// keep-alive keeps a live one open. They are the link's, not the session's idle timeout, which only traffic of the user
	// restarts (hub.Session.Touch).
	DefaultMaxIdle   = 15 * time.Second
	DefaultKeepAlive = 5 * time.Second
	// DefaultMaxPending is how many connections may be waiting to present their token; one more is closed.
	DefaultMaxPending = 32
)

// HostSession is what the host side needs of a session of the hub (package remotework adapts its Session to it).
type HostSession interface {
	// Token is the session token the client presents.
	Token() []byte
	// Channel is the SSH channel of the session, which the transport reads and writes.
	Channel() io.ReadWriteCloser
	// Touch tells the session there was traffic: the idle timeout starts again.
	Touch()
	// End ends the session; the session calls HostConn.Shutdown with the reason before it closes the SSH connection.
	End(reason protocol.Reason, text string)
	// Done is closed when the session is over.
	Done() <-chan struct{}
}

// HostOptions are what NewHost takes; every field but Lookup has a default.
type HostOptions struct {
	// PacketConn is the UDP socket QUIC listens on, with the same port number as the SSH server; the host closes it. Nil means
	// no QUIC: Fingerprint is empty and every session runs over the SSH channel.
	PacketConn net.PacketConn
	// Lookup finds the connection of the session that has the token (remotework.Hub.SessionByToken, which compares in constant
	// time and skips a session that ended). A connection whose token is not found is closed with a reason and held no longer.
	Lookup func(token []byte) (*HostConn, bool)
	// Clock is the time of the timers (the wait for a first link, the repair of lost tiles); the machine's when nil.
	Clock Clock
	// Logger is the log; slog.Default() when nil.
	Logger *slog.Logger
	// OnRefused is told of every connection or SSH Hello that was refused, with the transport it came on and the error (the tags
	// TransportHelloInvalid, TransportTokenInvalid and RemoteWorkVersion, and those of protocol).
	OnRefused func(kind Kind, remote string, err error)
	// Random makes the certificate; crypto/rand.Reader when nil.
	Random io.Reader
	// FirstLink, HelloTimeout, MaxIdle, KeepAlive and MaxPending override the defaults above.
	FirstLink, HelloTimeout, MaxIdle, KeepAlive time.Duration
	MaxPending                                  int
	// RepairAfter, QueueMessages, QueueBytes, Inbound and Stall are the knobs of every connection (see coreOptions); tests set them.
	RepairAfter                        time.Duration
	QueueMessages, QueueBytes, Inbound int
	Stall                              time.Duration
}

// Host is the host side of the transport: the QUIC listener, with the certificate made in memory at start, and the factory of the
// connections of the sessions.
type Host struct {
	opts HostOptions
	clk  Clock
	log  *slog.Logger

	cert        tls.Certificate
	fingerprint []byte
	ln          *quic.Listener
	pending     chan struct{}

	cancel    context.CancelFunc
	wg        sync.WaitGroup
	closeOnce sync.Once
}

// NewHost makes the certificate and starts the listener on the socket of the options (when there is one). It stops when ctx ends or
// Close is called. It fails with TransportCertFailed and TransportListenFailed.
func NewHost(ctx context.Context, opts HostOptions) (*Host, error) {
	h := &Host{opts: opts, clk: opts.Clock, log: opts.Logger}
	if h.clk == nil {
		h.clk = RealClock{}
	}
	if h.log == nil {
		h.log = slog.Default()
	}
	if h.opts.FirstLink <= 0 {
		h.opts.FirstLink = DefaultFirstLink
	}
	if h.opts.HelloTimeout <= 0 {
		h.opts.HelloTimeout = DefaultHelloTimeout
	}
	if h.opts.MaxIdle <= 0 {
		h.opts.MaxIdle = DefaultMaxIdle
	}
	if h.opts.KeepAlive <= 0 {
		h.opts.KeepAlive = DefaultKeepAlive
	}
	if h.opts.MaxPending <= 0 {
		h.opts.MaxPending = DefaultMaxPending
	}
	h.pending = make(chan struct{}, h.opts.MaxPending)
	ctx, h.cancel = context.WithCancel(ctx)
	if opts.PacketConn == nil {
		return h, nil
	}
	random := opts.Random
	if random == nil {
		random = rand.Reader
	}
	// The certificate: a P-256 key made now, in memory and never written, and a self-signed certificate whose SHA-256
	// fingerprint goes to the client over the SSH channel. The client accepts that certificate and no other, so it needs no
	// name, no expiry worth checking and no chain.
	key, err := ecdsa.GenerateKey(elliptic.P256(), random)
	if err != nil {
		return nil, errors.NewErr("TransportCertFailed", err)
	}
	serial, err := rand.Int(random, new(big.Int).Lsh(big.NewInt(1), 120))
	if err != nil {
		return nil, errors.NewErr("TransportCertFailed", err)
	}
	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: "Emetos Remote Work"},
		NotBefore: now.Add(-time.Hour), NotAfter: now.AddDate(100, 0, 0),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(random, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, errors.NewErr("TransportCertFailed", err)
	}
	sum := sha256.Sum256(der)
	h.fingerprint = sum[:]
	h.cert = tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	tlsConf := &tls.Config{Certificates: []tls.Certificate{h.cert}, MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13, NextProtos: []string{ALPN}}
	ln, err := quic.Listen(opts.PacketConn, tlsConf, &quic.Config{
		EnableDatagrams: true, HandshakeIdleTimeout: h.opts.HelloTimeout, MaxIdleTimeout: h.opts.MaxIdle, KeepAlivePeriod: h.opts.KeepAlive,
		MaxIncomingStreams: 2, MaxIncomingUniStreams: -1,
	})
	if err != nil {
		return nil, errors.NewErr("TransportListenFailed", err)
	}
	h.ln = ln
	h.wg.Add(1)
	go h.acceptLoop(ctx)
	return h, nil
}

// Fingerprint is the SHA-256 fingerprint of the certificate, the 32 bytes the grant carries; nil when the host has no QUIC.
func (h *Host) Fingerprint() []byte { return append([]byte(nil), h.fingerprint...) }

// Addr is the UDP address QUIC listens on, nil when the host has no QUIC.
func (h *Host) Addr() net.Addr {
	if h.ln == nil {
		return nil
	}
	return h.ln.Addr()
}

// Close stops the listener and closes the socket. The connections of the sessions are the sessions' to end (they end with
// them); the QUIC ones close with the listener.
func (h *Host) Close() {
	h.closeOnce.Do(func() {
		h.cancel()
		if h.ln != nil {
			_ = h.ln.Close() // the listener's error is that it was closed
		}
		h.wg.Wait()
		if h.opts.PacketConn != nil {
			_ = h.opts.PacketConn.Close() // the socket only closes
		}
	})
}

func (h *Host) refused(kind Kind, remote string, err error) {
	h.log.Info("Remote Work: a transport connection was refused", "kind", kind.String(), "remote", remote, "err", err)
	if h.opts.OnRefused != nil {
		h.opts.OnRefused(kind, remote, err)
	}
}

// acceptLoop takes the QUIC connections that complete their handshake; each is given a limited time to present its token.
func (h *Host) acceptLoop(ctx context.Context) {
	defer h.wg.Done()
	for {
		qc, err := h.ln.Accept(ctx)
		if err != nil {
			return
		}
		select {
		case h.pending <- struct{}{}:
			h.wg.Add(1)
			go h.handshake(ctx, qc)
		default:
			_ = qc.CloseWithError(codeBusy, "too many connections are being set up") // the peer is told, and gone
		}
	}
}

// handshake admits one QUIC connection, or closes it with the reason.
func (h *Host) handshake(ctx context.Context, qc *quic.Conn) {
	defer h.wg.Done()
	conn, st, r, err := h.admit(ctx, qc)
	<-h.pending
	if err != nil {
		h.refused(KindQUIC, qc.RemoteAddr().String(), err)
		code, text := codeProtocol, "the connection did not open as a Remote Work connection"
		if appErr, ok := errors.FromError(err); ok {
			switch appErr.Tag {
			case "TransportTokenInvalid":
				code, text = codeToken, "the token is not the token of a live session"
			case "RemoteWorkVersion":
				code, text = codeVersion, "the protocol versions differ"
			}
		}
		_ = qc.CloseWithError(code, text)
		return
	}
	conn.bindQUIC(qc, st, r)
}

// admit reads the Hello that starts the first stream and finds the session of its token. It fails with TransportHelloInvalid,
// TransportTokenInvalid, RemoteWorkVersion and the tags of protocol.Reader.Next.
func (h *Host) admit(ctx context.Context, qc *quic.Conn) (*HostConn, *quic.Stream, *protocol.Reader, error) {
	actx, cancel := context.WithTimeout(ctx, h.opts.HelloTimeout)
	defer cancel()
	st, err := qc.AcceptStream(actx)
	if err != nil {
		return nil, nil, nil, errors.NewErr("TransportHelloInvalid", err).SetMetadata("remote", qc.RemoteAddr().String())
	}
	if err := st.SetReadDeadline(time.Now().Add(h.opts.HelloTimeout)); err != nil {
		return nil, nil, nil, errors.NewErr("TransportHelloInvalid", err)
	}
	hello, err := readHello(st)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := hello.Check(); err != nil {
		return nil, nil, nil, err
	}
	conn, ok := h.opts.Lookup(hello.Token)
	if !ok || conn == nil {
		return nil, nil, nil, errors.New("TransportTokenInvalid", "the token is not the token of a live session")
	}
	if err := st.SetReadDeadline(time.Time{}); err != nil {
		return nil, nil, nil, errors.NewErr("TransportHelloInvalid", err)
	}
	return conn, st, protocol.NewReader(st), nil
}

// readHello reads the first message of a connection that has not shown its token yet, which must be a Hello: the header is
// checked before the payload is read, so such a connection costs at most the 128 bytes of a Hello, whatever it says. It fails with
// TransportHelloInvalid and the tags of protocol.Decode.
func readHello(r io.Reader) (*protocol.Hello, error) {
	var head [protocol.HeaderSize]byte
	if _, err := io.ReadFull(r, head[:]); err != nil {
		return nil, errors.NewErr("TransportHelloInvalid", err).SetMetadata("what", "the header")
	}
	max, _ := protocol.MaxPayload(protocol.TypeHello)
	n := int(binary.BigEndian.Uint32(head[1:]))
	if protocol.Type(head[0]) != protocol.TypeHello || n > max {
		return nil, errors.New("TransportHelloInvalid", "the first message of a connection is not a Hello", "type", head[0], "length", n)
	}
	msg := make([]byte, protocol.HeaderSize+n)
	copy(msg, head[:])
	if _, err := io.ReadFull(r, msg[protocol.HeaderSize:]); err != nil {
		return nil, errors.NewErr("TransportHelloInvalid", err).SetMetadata("what", "the payload")
	}
	m, err := protocol.Decode(msg)
	if err != nil {
		return nil, err
	}
	return m.(*protocol.Hello), nil
}

// HostConn is the transport of one session on the host: it implements Conn. It waits for the client to open QUIC (a connection that
// presents the token) or the SSH channel (a Hello on it); a client that comes again, after a drop, replaces the link it had.
type HostConn struct {
	*core
	host *Host
	sess HostSession
}

// Attach makes the connection of a session. The hub calls it before the grant is written, so the token is known to Lookup by the
// time the client uses it, and calls Start once the session can be ended safely (the grant was sent).
func (h *Host) Attach(s HostSession) *HostConn {
	ssh := &sshEnd{rw: s.Channel()}
	c := &HostConn{host: h, sess: s}
	c.core = newCore(coreOptions{
		role: roleHost, clock: h.clk, log: h.log, touch: s.Touch, end: s.End, ssh: ssh, repairAfter: h.opts.RepairAfter,
		queueMessages: h.opts.QueueMessages, queueBytes: h.opts.QueueBytes, inbound: h.opts.Inbound, stall: h.opts.Stall,
	})
	c.onLinkDown = func(l *link, _ error) {
		if l.kind == KindSSH {
			// the SSH channel is the session: when it goes, the session is over
			c.end(protocol.ReasonClosed, "the SSH connection ended")
		}
	}
	return c
}

// Start begins to read the SSH channel and sets the timer for the client's first transport: if it opens none within FirstLink the
// session ends. A connection that was shut down before Start does nothing. Start is called once.
func (c *HostConn) Start() {
	c.mu.Lock()
	if c.finished || c.started {
		c.mu.Unlock()
		return
	}
	c.started = true
	c.goroutines.Add(1)
	c.mu.Unlock()
	go c.sshPump()
	c.addTimer(c.host.clk.AfterFunc(c.host.opts.FirstLink, func() {
		select {
		case <-c.firstLink:
		default:
			c.end(protocol.ReasonProtocol, "the client opened no transport")
		}
	}))
}

// Shutdown ends the transport of the session: the client is told why on the SSH channel and by the closing of QUIC, the links close
// and the goroutines go. The session calls it from End, before it closes the SSH connection. It does nothing the second time.
func (c *HostConn) Shutdown(reason protocol.Reason, text string) { c.shutdown(reason, text) }

// Wait returns when the goroutines of the connection ended; they end after Shutdown and the closing of the SSH channel.
func (c *HostConn) Wait() { c.wait() }

// bindQUIC answers the Hello and makes the QUIC connection the link of the session.
func (c *HostConn) bindQUIC(qc *quic.Conn, st *quic.Stream, r *protocol.Reader) {
	reply, err := protocol.Append(nil, &protocol.Hello{Version: protocol.Version})
	if err != nil {
		_ = qc.CloseWithError(codeProtocol, "the answer could not be made")
		return
	}
	err = st.SetWriteDeadline(time.Now().Add(c.host.opts.HelloTimeout))
	if err == nil {
		_, err = st.Write(reply)
	}
	if err != nil {
		_ = qc.CloseWithError(codeProtocol, "the answer was not sent")
		return
	}
	_ = st.SetWriteDeadline(time.Time{}) // clearing a deadline cannot fail on a stream that just wrote
	var wmu sync.Mutex
	l := &link{kind: KindQUIC, q: qc, w: st, wmu: &wmu, done: make(chan struct{})}
	l.closer = func(code quic.ApplicationErrorCode, text string) { _ = qc.CloseWithError(code, text) }
	l.abort = func() { l.close(codeNormal, "a write stalled") }
	if !c.bind(l) {
		return
	}
	c.goroutines.Add(1)
	go c.readStream(l, r)
}

// sshPump reads the SSH channel: the Hello of a client that uses it as its transport, then its messages; before that, and
// when QUIC is the transport, only a Disconnect can come.
func (c *HostConn) sshPump() {
	defer c.goroutines.Done()
	r := protocol.NewReader(c.ssh.rw)
	var l *link
	for {
		m, err := r.Next()
		if err != nil {
			c.mu.Lock()
			finished := c.finished
			c.mu.Unlock()
			if !finished {
				c.end(protocol.ReasonClosed, "the SSH connection ended")
			}
			return
		}
		switch m := m.(type) {
		case *protocol.Disconnect:
			c.peerDisconnected(m)
			continue
		case *protocol.Hello:
			if l != nil {
				c.violation("a second Hello on the SSH channel")
				return
			}
			var err error
			if l, err = c.helloSSH(m); err != nil {
				c.host.refused(KindSSH, "", err)
				c.end(protocol.ReasonProtocol, "the Hello on the SSH channel was refused")
				return
			}
			continue
		}
		if l == nil {
			c.violation("a message on the SSH channel before its Hello")
			return
		}
		c.onReliable(l, m)
	}
}

// helloSSH answers the Hello of a client that uses the SSH channel and makes it the link. It fails with RemoteWorkVersion and
// TransportTokenInvalid.
func (c *HostConn) helloSSH(m *protocol.Hello) (*link, error) {
	if err := m.Check(); err != nil {
		return nil, err
	}
	token := c.sess.Token()
	if subtle.ConstantTimeCompare(token, m.Token) != 1 {
		return nil, errors.New("TransportTokenInvalid", "the token is not the token of this session")
	}
	reply, err := protocol.Append(nil, &protocol.Hello{Version: protocol.Version})
	if err != nil {
		return nil, err
	}
	if err := c.ssh.write(reply); err != nil {
		return nil, errors.NewErr("TransportLinkFailed", err).SetMetadata("op", "write", "kind", KindSSH.String())
	}
	l := &link{kind: KindSSH, w: c.ssh.rw, wmu: &c.ssh.wmu, done: make(chan struct{}), abort: func() { _ = c.ssh.rw.Close() }}
	if !c.bind(l) {
		return nil, errors.New("TransportClosed", "the session ended while the Hello was answered")
	}
	return l, nil
}

var _ Conn = (*HostConn)(nil)
