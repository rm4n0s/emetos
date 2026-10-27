package transport

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/remotework/login"
	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
)

// The defaults of the client.
const (
	// DefaultFallbackAfter is how long QUIC has to complete its handshake before the session runs over the SSH channel (SPEC
	// §10.18).
	DefaultFallbackAfter = 3 * time.Second
	// DefaultResumeAttempts is how many times a dropped QUIC connection is dialed again before the SSH channel carries the session.
	DefaultResumeAttempts = 3
)

// ClientOptions are what Connect needs.
type ClientOptions struct {
	// Address is host:port of the UDP endpoint of the host: the port of the SSH server, since QUIC uses the same number.
	Address string
	// Token is the session token of the grant and CertSHA256 the fingerprint of the host's certificate, 32 bytes or none (the
	// host offers no QUIC: the session runs over the SSH channel at once).
	Token      []byte
	CertSHA256 []byte
	// Channel is the SSH channel of the session; it is closed when the connection ends, and CloseSSH, if set, is called then to
	// close the SSH connection under it.
	Channel  io.ReadWriteCloser
	CloseSSH func()
	// Clock is the time of the fallback and the repair timers; the machine's when nil. Logger is the log.
	Clock  Clock
	Logger *slog.Logger
	// FallbackAfter, ResumeAttempts, HelloTimeout, MaxIdle and KeepAlive override the defaults.
	FallbackAfter  time.Duration
	ResumeAttempts int
	HelloTimeout   time.Duration
	MaxIdle        time.Duration
	KeepAlive      time.Duration
	// RepairAfter, QueueMessages, QueueBytes, Inbound and Stall are the knobs of the connection (tests set them).
	RepairAfter                        time.Duration
	QueueMessages, QueueBytes, Inbound int
	Stall                              time.Duration
}

// FromLogin makes the options of a connection from a login that was granted a session. address is the host:port the login used.
func FromLogin(c *login.Connection, address string) ClientOptions {
	return ClientOptions{Address: address, Token: c.Grant.Token, CertSHA256: c.Grant.CertSHA256, Channel: c.Channel, CloseSSH: func() { _ = c.Close() }}
}

// ClientConn is the transport of a session on the client: it implements Conn. It tries QUIC for FallbackAfter, uses the SSH channel
// if that fails, and dials a dropped QUIC connection again with the token while the SSH connection lives.
type ClientConn struct {
	*core
	o         ClientOptions
	fallback  time.Duration
	attempts  int
	helloWait time.Duration

	helloCh chan *protocol.Hello

	// linkMu guards sshLink (the embedded core has a mutex of its own, mu).
	linkMu  sync.Mutex
	sshLink *link
	// certGot is the fingerprint of the certificate a dial saw when it was not the one expected.
	certGot atomic.Value
}

// Connect makes the connection. It returns when a transport is up, QUIC or SSH. The errors have the tags TransportOptionsInvalid,
// TransportCertMismatch (the host showed another certificate than the SSH channel promised: nothing is sent to it, and the
// SSH channel is not used instead, because the mismatch means something is answering on the UDP port that the host did not
// start), TransportRefused (the host closed the connection with a reason), TransportHelloInvalid, TransportCancelled, and those
// of protocol; a QUIC handshake that does not complete is not an error but the fallback.
func Connect(ctx context.Context, o ClientOptions) (*ClientConn, error) {
	if len(o.Token) == 0 || len(o.Token) > protocol.MaxToken || o.Channel == nil || (len(o.CertSHA256) != 0 && len(o.CertSHA256) != sha256.Size) {
		return nil, errors.New("TransportOptionsInvalid", "a connection needs the token, the SSH channel, and a certificate fingerprint of 32 bytes or none")
	}
	if o.Address == "" && len(o.CertSHA256) != 0 {
		return nil, errors.New("TransportOptionsInvalid", "a connection with a certificate needs the address of the host")
	}
	c := &ClientConn{o: o, fallback: o.FallbackAfter, attempts: o.ResumeAttempts, helloWait: o.HelloTimeout, helloCh: make(chan *protocol.Hello, 1)}
	if c.fallback <= 0 {
		c.fallback = DefaultFallbackAfter
	}
	if c.attempts <= 0 {
		c.attempts = DefaultResumeAttempts
	}
	if c.helloWait <= 0 {
		c.helloWait = DefaultHelloTimeout
	}
	c.core = newCore(coreOptions{
		role: roleClient, clock: o.Clock, log: o.Logger, end: nil, ssh: &sshEnd{rw: o.Channel}, repairAfter: o.RepairAfter,
		queueMessages: o.QueueMessages, queueBytes: o.QueueBytes, inbound: o.Inbound, stall: o.Stall,
	})
	c.end = c.shutdown
	c.onLinkDown = c.linkDown
	c.onFinish = func() {
		_ = c.ssh.rw.Close() // the channel only closes
		if o.CloseSSH != nil {
			o.CloseSSH()
		}
	}
	c.goroutines.Add(1)
	go c.sshPump()
	if err := c.establish(ctx); err != nil {
		c.finish(err, codeNormal, "")
		c.wait()
		return nil, err
	}
	return c, nil
}

// establish makes the first transport: QUIC if the host offers it and it works within the fallback time, else the SSH channel.
func (c *ClientConn) establish(ctx context.Context) error {
	if len(c.o.CertSHA256) != 0 {
		err := c.attachQUIC(ctx)
		if err == nil {
			return nil
		}
		if ctx.Err() != nil || fatal(err) {
			return err
		}
		c.log.Info("Remote Work: no QUIC, the session runs over the SSH channel", "err", err)
	}
	return c.attachSSH(ctx)
}

// fatal tells whether an error of a QUIC attempt means the session must not go on: the host showed another certificate, refused
// the token or the version, or sent something that is not the answer.
func fatal(err error) bool {
	appErr, ok := errors.FromError(err)
	if !ok {
		return false
	}
	switch appErr.Tag {
	case "TransportCertMismatch", "TransportRefused", "TransportHelloInvalid", "RemoteWorkVersion", "TransportCancelled":
		return true
	}
	return false
}

// attachQUIC dials, presents the token and makes the connection the link.
func (c *ClientConn) attachQUIC(ctx context.Context) error {
	l, r, err := c.dialQUIC(ctx)
	if err != nil {
		return err
	}
	if !c.bind(l) {
		return errors.New("TransportClosed", "the session ended while QUIC was being opened")
	}
	c.goroutines.Add(1)
	go c.readStream(l, r)
	return nil
}

// dialQUIC makes one QUIC connection to the host and presents the token on its first stream. It fails with the tags of Connect
// and TransportDialFailed.
func (c *ClientConn) dialQUIC(ctx context.Context) (*link, *protocol.Reader, error) {
	want := c.o.CertSHA256
	c.certGot.Store("")
	tlsConf := &tls.Config{
		// The certificate is self-signed and has no name worth checking: the only thing that makes it right is that its
		// fingerprint is the one the SSH channel gave us. VerifyConnection runs for every connection, so nothing
		// of the system's roots is consulted and nothing decides but the comparison.
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS13, MaxVersion: tls.VersionTLS13,
		NextProtos: []string{ALPN},
		ServerName: "emetos-remotework",
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) != 1 {
				c.certGot.Store("no certificate or a chain")
				return errors.New("TransportCertMismatch", "the host did not show one certificate")
			}
			sum := sha256.Sum256(cs.PeerCertificates[0].Raw)
			if subtle.ConstantTimeCompare(sum[:], want) != 1 {
				c.certGot.Store(fingerprintHex(sum[:]))
				return errors.New("TransportCertMismatch", "the certificate is not the one the SSH channel promised")
			}
			return nil
		},
	}
	conf := &quic.Config{
		EnableDatagrams: true, HandshakeIdleTimeout: c.fallback + time.Second, MaxIdleTimeout: orDefault(c.o.MaxIdle, DefaultMaxIdle),
		KeepAlivePeriod: orDefault(c.o.KeepAlive, DefaultKeepAlive), MaxIncomingStreams: -1, MaxIncomingUniStreams: -1,
	}
	hsCtx, hsCancel := context.WithCancel(ctx)
	stop := c.clk.AfterFunc(c.fallback, hsCancel)
	qc, err := quic.DialAddr(hsCtx, c.o.Address, tlsConf, conf)
	stop()
	hsCancel()
	if err != nil {
		if got, _ := c.certGot.Load().(string); got != "" {
			return nil, nil, errors.New("TransportCertMismatch", "the host showed a certificate that is not the one the SSH channel promised: nothing was sent to it",
				"address", c.o.Address, "presented", got, "expected", fingerprintHex(want))
		}
		if ctx.Err() != nil {
			return nil, nil, errors.NewErr("TransportCancelled", ctx.Err())
		}
		return nil, nil, errors.NewErr("TransportDialFailed", err).SetMetadata("address", c.o.Address)
	}
	l, r, err := c.hello(ctx, qc)
	if err != nil {
		_ = qc.CloseWithError(codeProtocol, "the session was not opened") // the connection only closes
		return nil, nil, err
	}
	return l, r, nil
}

// hello presents the token on the first stream and reads the host's answer.
func (c *ClientConn) hello(ctx context.Context, qc *quic.Conn) (*link, *protocol.Reader, error) {
	hctx, cancel := context.WithTimeout(ctx, c.helloWait)
	defer cancel()
	st, err := qc.OpenStreamSync(hctx)
	if err != nil {
		return nil, nil, c.helloFailure(qc, err)
	}
	b, err := protocol.Append(nil, &protocol.Hello{Version: protocol.Version, Token: c.o.Token})
	if err != nil {
		return nil, nil, err
	}
	deadline := time.Now().Add(c.helloWait)
	if err := st.SetDeadline(deadline); err != nil {
		return nil, nil, errors.NewErr("TransportDialFailed", err)
	}
	if _, err := st.Write(b); err != nil {
		return nil, nil, c.helloFailure(qc, err)
	}
	r := protocol.NewReader(st)
	m, err := r.Next()
	if err != nil {
		return nil, nil, c.helloFailure(qc, err)
	}
	reply, ok := m.(*protocol.Hello)
	if !ok {
		return nil, nil, errors.New("TransportHelloInvalid", "the host's answer is not a Hello", "type", m.Type().String())
	}
	if err := reply.Check(); err != nil {
		return nil, nil, err
	}
	if err := st.SetDeadline(time.Time{}); err != nil {
		return nil, nil, errors.NewErr("TransportDialFailed", err)
	}
	var wmu sync.Mutex
	l := &link{kind: KindQUIC, q: qc, w: st, wmu: &wmu, done: make(chan struct{})}
	l.closer = func(code quic.ApplicationErrorCode, text string) { _ = qc.CloseWithError(code, text) }
	l.abort = func() { l.close(codeNormal, "a write stalled") }
	return l, r, nil
}

// helloFailure says why the first stream did not work: the host closed the connection with a reason (TransportRefused), or the
// network did not carry it (TransportDialFailed).
func (c *ClientConn) helloFailure(_ *quic.Conn, err error) error {
	var ae *quic.ApplicationError
	if errors.As(err, &ae) && ae.Remote {
		return errors.New("TransportRefused", "the host closed the connection: "+ae.ErrorMessage, "code", uint64(ae.ErrorCode), "text", ae.ErrorMessage)
	}
	return errors.NewErr("TransportDialFailed", err).SetMetadata("address", c.o.Address)
}

// attachSSH makes the SSH channel the transport: it sends the Hello on it and waits for the answer.
func (c *ClientConn) attachSSH(ctx context.Context) error {
	b, err := protocol.Append(nil, &protocol.Hello{Version: protocol.Version, Token: c.o.Token})
	if err != nil {
		return err
	}
	select { // an answer left over from an attempt before
	case <-c.helloCh:
	default:
	}
	if err := c.ssh.write(b); err != nil {
		return errors.NewErr("TransportLinkFailed", err).SetMetadata("op", "write", "kind", KindSSH.String())
	}
	t := time.NewTimer(c.helloWait)
	defer t.Stop()
	select {
	case reply := <-c.helloCh:
		if err := reply.Check(); err != nil {
			return err
		}
	case <-t.C:
		return errors.New("TransportHelloInvalid", "the host did not answer the Hello on the SSH channel")
	case <-ctx.Done():
		return errors.NewErr("TransportCancelled", ctx.Err())
	case <-c.done:
		return errors.New("TransportClosed", "the session ended while the SSH channel was being opened")
	}
	l := &link{kind: KindSSH, w: c.ssh.rw, wmu: &c.ssh.wmu, done: make(chan struct{}), abort: func() { _ = c.ssh.rw.Close() }}
	c.linkMu.Lock()
	c.sshLink = l
	c.linkMu.Unlock()
	if !c.bind(l) {
		return errors.New("TransportClosed", "the session ended while the SSH channel was being opened")
	}
	return nil
}

// sshPump reads the SSH channel for the life of the connection: the Hello that answers the fallback, the messages of the session
// when the SSH channel is the transport, and, whatever the transport, the host's Disconnect.
func (c *ClientConn) sshPump() {
	defer c.goroutines.Done()
	r := protocol.NewReader(c.ssh.rw)
	for {
		m, err := r.Next()
		if err != nil {
			c.mu.Lock()
			finished := c.finished
			c.mu.Unlock()
			if !finished {
				c.finish(errors.New("TransportClosed", "the SSH connection ended", "cause", err.Error()), codeNormal, "")
			}
			return
		}
		switch m := m.(type) {
		case *protocol.Disconnect:
			c.peerDisconnected(m)
			continue
		case *protocol.Hello:
			select {
			case c.helloCh <- m:
			default:
				c.violation("a Hello nobody asked for on the SSH channel")
				return
			}
			continue
		}
		c.linkMu.Lock()
		l := c.sshLink
		c.linkMu.Unlock()
		if l == nil {
			c.violation("a message on the SSH channel before its Hello was answered")
			return
		}
		c.onReliable(l, m)
	}
}

// linkDown is called when the link broke and the connection lives: a dropped QUIC connection is dialed again, a broken SSH channel
// ends the session.
func (c *ClientConn) linkDown(l *link, err error) {
	if l.kind == KindSSH {
		c.finish(errors.New("TransportClosed", "the SSH channel failed", "cause", err.Error()), codeNormal, "")
		return
	}
	c.goroutines.Add(1)
	go c.resume()
}

// resume dials the dropped QUIC connection again with the token, a few times; when UDP does not come back the session runs over the
// SSH channel instead. A host that refuses the token ends the session (it ended there).
func (c *ClientConn) resume() {
	defer c.goroutines.Done()
	for attempt := 1; attempt <= c.attempts; attempt++ {
		err := c.attachQUIC(c.ctx)
		if err == nil {
			return
		}
		if c.ctx.Err() != nil {
			return
		}
		if fatal(err) {
			c.finish(err, codeNormal, "")
			return
		}
		c.log.Info("Remote Work: resuming QUIC failed", "attempt", attempt, "err", err)
		t := time.NewTimer(time.Duration(attempt) * 200 * time.Millisecond)
		select {
		case <-t.C:
		case <-c.ctx.Done():
			t.Stop()
			return
		}
	}
	if err := c.attachSSH(c.ctx); err != nil && c.ctx.Err() == nil {
		c.finish(err, codeNormal, "")
	}
}

func orDefault(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// fingerprintHex writes a fingerprint as hex.
func fingerprintHex(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, x := range b {
		out = append(out, digits[x>>4], digits[x&15])
	}
	return string(out)
}

var _ Conn = (*ClientConn)(nil)
