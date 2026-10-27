package remotework

import (
	"bytes"
	"context"
	"io"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/remotework/login"
	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
	"github.com/rm4n0s/emetos/extensions/remotework/transport"
)

// noLeaks fails the test if goroutines are left when it ends; the check runs after every other cleanup.
func noLeaks(t *testing.T) {
	t.Helper()
	before := runtime.NumGoroutine()
	t.Cleanup(func() {
		deadline := time.Now().Add(8 * time.Second)
		for runtime.NumGoroutine() > before {
			if time.Now().After(deadline) {
				buf := make([]byte, 1<<20)
				n := runtime.Stack(buf, true)
				t.Errorf("goroutines left: %d before, %d now\n%s", before, runtime.NumGoroutine(), buf[:n])
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
	})
}

// session is one login with its transport on both sides.
type session struct {
	login  *login.Connection
	client *transport.ClientConn
	host   *Session
}

// transportSession logs in with a listed key and opens the transport, as the tab of step 15.4 does.
func (r *rig) transportSession(c *client, extra ...func(*transport.ClientOptions)) *session {
	r.t.Helper()
	conn, err := c.connect(r.addr(), "")
	if err != nil {
		r.t.Fatalf("login: %v", err)
	}
	o := transport.FromLogin(conn, r.addr())
	o.RepairAfter = 30 * time.Millisecond
	for _, f := range extra {
		f(&o)
	}
	tc, err := transport.Connect(context.Background(), o)
	if err != nil {
		r.t.Fatalf("transport: %v", err)
	}
	r.t.Cleanup(func() { _ = tc.Close(protocol.ReasonClosed, "the test is over") })
	return &session{login: conn, client: tc, host: r.nextSession()}
}

func nextMsg(t *testing.T, c transport.Conn) protocol.Message {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	m, err := c.Next(ctx)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	return m
}

func TestARealLoginThenQUICGivesASessionTheViewCanBeAttachedTo(t *testing.T) {
	noLeaks(t)
	r := newRig(t, rigOptions{transport: true})
	_, folder := r.workspace("project")
	b := newKey(t)
	r.on(folder, b)
	s := r.transportSession(r.client(b))
	g := s.login.Grant
	if len(g.CertSHA256) != 32 {
		t.Fatalf("the grant has a fingerprint of %d bytes", len(g.CertSHA256))
	}
	if s.client.Kind() != transport.KindQUIC {
		t.Fatalf("the client runs over %v, want QUIC", s.client.Kind())
	}
	conn := s.host.Conn()
	if conn == nil {
		t.Fatal("the session has no transport")
	}
	waitFor(t, "the host's QUIC link", func() bool { return conn.Kind() == transport.KindQUIC })

	// messages both ways
	if err := s.client.Send(&protocol.Key{Kind: protocol.KeyDown, Logical: 9, Text: "q"}); err != nil {
		t.Fatal(err)
	}
	if k, ok := nextMsg(t, conn).(*protocol.Key); !ok || k.Text != "q" {
		t.Fatalf("got %+v", k)
	}
	if err := conn.Send(&protocol.Notification{ID: 1, Source: "Git", Message: "ready"}); err != nil {
		t.Fatal(err)
	}
	if n, ok := nextMsg(t, s.client).(*protocol.Notification); !ok || n.Message != "ready" {
		t.Fatalf("got %+v", n)
	}
	// and tiles, as datagrams
	tl := &protocol.Tile{Frame: 1, Width: 64, Height: 64, Total: 1, Data: bytes.Repeat([]byte{3}, 3000)}
	if err := conn.SendTiles(1, []*protocol.Tile{tl}); err != nil {
		t.Fatal(err)
	}
	for {
		if got, ok := nextMsg(t, s.client).(*protocol.Tile); ok {
			if !bytes.Equal(got.Data, tl.Data) {
				t.Fatal("the tile differs")
			}
			break
		}
	}
	if conn.Stats().DatagramsSent == 0 {
		t.Error("no datagram was sent")
	}
	// the session ends: the client is told why and everything stops
	s.host.End(protocol.ReasonHost, "the user pressed Disconnect")
	if d, ok := nextMsg(t, s.client).(*protocol.Disconnect); !ok || d.Reason != protocol.ReasonHost {
		t.Fatalf("got %+v", d)
	}
	select {
	case <-s.client.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the client did not end")
	}
	_ = s.login.Close()
	waitFor(t, "the session to be forgotten", func() bool { return len(r.hub.Sessions()) == 0 })
	conn.Wait()
}

func TestTheIdleTimeoutEndsAQUICSessionAndTrafficRestartsIt(t *testing.T) {
	noLeaks(t)
	r := newRig(t, rigOptions{transport: true})
	_, folder := r.workspace("project")
	b := newKey(t)
	r.on(folder, b)
	if err := r.hub.SetIdleMinutes(30); err != nil {
		t.Fatal(err)
	}
	s := r.transportSession(r.client(b))
	conn := s.host.Conn()
	waitFor(t, "the host's QUIC link", func() bool { return conn.Kind() == transport.KindQUIC })
	r.clock.Advance(20 * time.Minute)
	// a message of the client is traffic: the 30 minutes start again
	if err := s.client.Send(&protocol.Ping{Seq: 1}); err != nil {
		t.Fatal(err)
	}
	if _, ok := nextMsg(t, conn).(*protocol.Ping); !ok {
		t.Fatal("the ping did not arrive")
	}
	r.clock.Advance(20 * time.Minute)
	select {
	case <-s.host.Done():
		t.Fatal("the session ended 20 minutes after its last traffic")
	case <-time.After(50 * time.Millisecond):
	}
	r.clock.Advance(11 * time.Minute)
	d, ok := nextMsg(t, s.client).(*protocol.Disconnect)
	if !ok || d.Reason != protocol.ReasonIdle {
		t.Fatalf("got %+v, want the idle timeout", d)
	}
	<-s.client.Done()
	if _, ok := r.hub.SessionByToken(s.login.Grant.Token); ok {
		t.Error("the token outlived the session")
	}
	_ = s.login.Close()
}

func TestTurningRemoteWorkOffEndsTheTransportAndClosesTheUDPSocket(t *testing.T) {
	noLeaks(t)
	r := newRig(t, rigOptions{transport: true})
	_, folder := r.workspace("project")
	b := newKey(t)
	r.on(folder, b)
	s := r.transportSession(r.client(b))
	waitFor(t, "the host's QUIC link", func() bool { return s.host.Conn().Kind() == transport.KindQUIC })
	udp := r.hub.host.Addr().String()
	if err := r.hub.SetWorkspace(folder, false); err != nil {
		t.Fatal(err)
	}
	if d, ok := nextMsg(t, s.client).(*protocol.Disconnect); !ok || d.Reason != protocol.ReasonOff {
		t.Fatalf("got %+v", d)
	}
	<-s.client.Done()
	_ = s.login.Close()
	// nothing listens on the UDP port any more: a new socket can take it
	pc, err := net.ListenPacket("udp", udp)
	if err != nil {
		t.Fatalf("the UDP port is still taken: %v", err)
	}
	_ = pc.Close()
}

func TestWithoutQUICTheSessionRunsOverTheSSHChannel(t *testing.T) {
	noLeaks(t)
	r := newRig(t, rigOptions{transport: true, noQUIC: true})
	_, folder := r.workspace("project")
	b := newKey(t)
	r.on(folder, b)
	s := r.transportSession(r.client(b))
	if len(s.login.Grant.CertSHA256) != 0 {
		t.Errorf("a fingerprint was granted although the hub has no QUIC")
	}
	if s.client.Kind() != transport.KindSSH {
		t.Fatalf("kind = %v", s.client.Kind())
	}
	conn := s.host.Conn()
	waitFor(t, "the host's SSH link", func() bool { return conn.Kind() == transport.KindSSH })
	if err := s.client.Send(&protocol.Clipboard{Text: "copied"}); err != nil {
		t.Fatal(err)
	}
	if c, ok := nextMsg(t, conn).(*protocol.Clipboard); !ok || c.Text != "copied" {
		t.Fatalf("got %+v", c)
	}
	if err := conn.SendTiles(1, []*protocol.Tile{{Frame: 1, Width: 64, Height: 64, Total: 1, Data: []byte{1, 2, 3}}}); err != nil {
		t.Fatal(err)
	}
	for {
		if _, ok := nextMsg(t, s.client).(*protocol.Tile); ok {
			break
		}
	}
	if st := conn.Stats(); st.DatagramsSent != 0 || st.TilesSent != 1 {
		t.Errorf("stats %+v", st)
	}
	s.host.End(protocol.ReasonHost, "done")
	if d, ok := nextMsg(t, s.client).(*protocol.Disconnect); !ok || d.Reason != protocol.ReasonHost {
		t.Fatalf("got %+v", d)
	}
	_ = s.login.Close()
}

func TestAUDPPortThatCannotBeOpenedLeavesTheSSHChannel(t *testing.T) {
	noLeaks(t)
	r := newRig(t, rigOptions{transport: true, hubTweak: func(o *HubOptions) {
		o.ListenUDP = func(int) (net.PacketConn, error) { return nil, io.ErrClosedPipe }
		o.Cert = nil
	}})
	_, folder := r.workspace("project")
	b := newKey(t)
	r.on(folder, b)
	s := r.transportSession(r.client(b))
	if len(s.login.Grant.CertSHA256) != 0 || s.client.Kind() != transport.KindSSH {
		t.Errorf("grant %x, kind %v: want no QUIC", s.login.Grant.CertSHA256, s.client.Kind())
	}
	_ = s.login.Close()
}

func TestTheTokenOfAnEndedSessionIsRefusedByQUIC(t *testing.T) {
	noLeaks(t)
	r := newRig(t, rigOptions{transport: true})
	_, folder := r.workspace("project")
	b := newKey(t)
	r.on(folder, b)
	conn, err := r.client(b).connect(r.addr(), "")
	if err != nil {
		t.Fatal(err)
	}
	s := r.nextSession()
	grant := conn.Grant
	s.End(protocol.ReasonHost, "gone")
	<-s.Done()
	waitFor(t, "the session to be forgotten", func() bool { return len(r.hub.Sessions()) == 0 })
	_ = conn.Close()
	// a client that kept the token and the fingerprint presents them on a channel of its own
	_, other := net.Pipe()
	_, err = transport.Connect(context.Background(), transport.ClientOptions{Address: r.addr(), Token: grant.Token, CertSHA256: grant.CertSHA256, Channel: other})
	wantRoute(t, err, "TransportRefused")
}
