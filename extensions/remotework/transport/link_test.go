package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"image"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
)

// fakeTiles makes the n tiles of a picture, each with size bytes of data.
func fakeTiles(frame uint64, n, size int) []*protocol.Tile {
	out := make([]*protocol.Tile, n)
	for i := range out {
		data := make([]byte, size)
		for j := range data {
			data[j] = byte(i + j*7)
		}
		out[i] = &protocol.Tile{Frame: frame, Width: 640, Height: 64, X: uint16(i % 10), Total: uint32(n), Data: data}
	}
	return out
}

// dropLink closes the QUIC connection of the host, as a network that went away does for the client.
func dropLink(t *testing.T, c *HostConn) {
	t.Helper()
	c.mu.Lock()
	l := c.cur
	c.mu.Unlock()
	if l == nil || l.q == nil {
		t.Fatal("the host has no QUIC link to drop")
	}
	_ = l.q.CloseWithError(codeNormal, "the network went away")
}

func TestTilesOfEverySizeArriveWhole(t *testing.T) {
	noLeaks(t)
	r := newRig(t)
	s, ch := r.session()
	client := r.connect(r.opts(s, ch))
	eventually(t, "the host's link", func() bool { return s.conn.Kind() == KindQUIC })
	frame := uint64(0)
	for _, size := range []int{1, 100, 1000, 1100, 1101, 5000, 17000, protocol.MaxTileData} {
		frame++
		sent := fakeTiles(frame, 4, size)
		if err := s.conn.SendTiles(frame, sent); err != nil {
			t.Fatal(err)
		}
		got := map[uint16]*protocol.Tile{}
		for len(got) < len(sent) {
			m := nextOfType(t, client, protocol.TypeTile).(*protocol.Tile)
			if m.Frame != frame {
				t.Fatalf("size %d: a tile of frame %d", size, m.Frame)
			}
			got[m.X] = m
		}
		for _, want := range sent {
			if g := got[want.X]; g == nil || !bytes.Equal(g.Data, want.Data) {
				t.Fatalf("size %d: tile %d is not what was sent", size, want.X)
			}
		}
	}
}

func TestStaleTilesAreDroppedNotDelivered(t *testing.T) {
	noLeaks(t)
	r := newRig(t)
	s, ch := r.session()
	client := r.connect(r.opts(s, ch))
	eventually(t, "the host's link", func() bool { return s.conn.Kind() == KindQUIC })
	if err := s.conn.SendTiles(5, fakeTiles(5, 3, 2500)); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		nextOfType(t, client, protocol.TypeTile)
	}
	// a picture older than the newest one the client saw: every fragment is dropped on arrival
	if err := s.conn.SendTiles(3, fakeTiles(3, 2, 100)); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the stale tiles to be seen", func() bool { return client.Stats().TilesStale >= 2 })
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if m, err := client.Next(ctx); err == nil {
		t.Fatalf("a stale tile was delivered: %+v", m)
	}
	// and a newer one is accepted
	if err := s.conn.SendTiles(6, fakeTiles(6, 1, 100)); err != nil {
		t.Fatal(err)
	}
	if m := nextOfType(t, client, protocol.TypeTile).(*protocol.Tile); m.Frame != 6 {
		t.Errorf("got frame %d", m.Frame)
	}
}

func TestAClientAsksForTheTilesItLostAndGetsThem(t *testing.T) {
	noLeaks(t)
	r := newRig(t)
	s, ch := r.session()
	client := r.connect(r.opts(s, ch))
	eventually(t, "the host's link", func() bool { return s.conn.Kind() == KindQUIC })
	// the client's receiver is told of 6 tiles of picture 9 but is given only 4 of them, by sending the datagrams by hand
	sent := fakeTiles(9, 6, 3000)
	host := s.conn
	host.mu.Lock()
	q := host.cur.q
	host.mu.Unlock()
	// the host knows picture 9 (it is the batch Missing refers to), but the first pass is not sent: tiles 2 and 4 are skipped
	host.mu.Lock()
	host.batch = &batch{frame: 9, tiles: sent, next: len(sent), queued: make([]bool, len(sent))}
	host.mu.Unlock()
	for i, tl := range sent {
		if i == 2 || i == 4 {
			continue
		}
		enc, err := protocol.Append(nil, tl)
		if err != nil {
			t.Fatal(err)
		}
		parts, err := splitTile(9, uint16(i), uint16(len(sent)), enc, DefaultChunk)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range parts {
			if err := q.SendDatagram(d); err != nil {
				t.Fatal(err)
			}
		}
	}
	got := map[uint16]bool{}
	for len(got) < len(sent) {
		m := nextOfType(t, client, protocol.TypeTile).(*protocol.Tile)
		got[m.X%10] = true
		if m.Frame != 9 {
			t.Fatalf("frame %d", m.Frame)
		}
	}
	st := client.Stats()
	if st.MissingSent == 0 || host.Stats().MissingReceived == 0 || host.Stats().TilesResent != 2 {
		t.Errorf("client %+v host %+v: want the request made and 2 tiles sent again", st, host.Stats())
	}
}

func TestAFallbackToTheSSHChannelWhenUDPIsBlocked(t *testing.T) {
	noLeaks(t)
	r := newRig(t)
	s, ch := r.session()
	proxy := newProxy(t, r.host.Addr(), 0, 0, 1)
	proxy.block.Store(true)
	o := r.opts(s, ch)
	o.Address = proxy.Addr()
	o.Clock = r.clk
	before := r.clk.Pending()
	type result struct {
		c   *ClientConn
		err error
	}
	done := make(chan result, 1)
	go func() {
		c, err := Connect(context.Background(), o)
		done <- result{c, err}
	}()
	// the client waits for the QUIC handshake until its timer says three seconds have passed
	eventually(t, "the fallback timer", func() bool { return r.clk.Pending() > before })
	select {
	case <-done:
		t.Fatal("Connect returned before the three seconds")
	case <-time.After(300 * time.Millisecond):
	}
	r.clk.Advance(DefaultFallbackAfter)
	res := <-done
	if res.err != nil {
		t.Fatal(res.err)
	}
	client := res.c
	t.Cleanup(func() { _ = client.Close(protocol.ReasonClosed, ""); client.wait() })
	if client.Kind() != KindSSH {
		t.Fatalf("the client runs over %v, want SSH", client.Kind())
	}
	eventually(t, "the host to run over SSH", func() bool { return s.conn.Kind() == KindSSH })

	// the same messages
	if err := client.Send(&protocol.Key{Kind: protocol.KeyDown, Logical: 7, Text: "x"}); err != nil {
		t.Fatal(err)
	}
	if k, ok := next(t, s.conn).(*protocol.Key); !ok || k.Logical != 7 {
		t.Fatalf("got %+v", k)
	}
	if err := s.conn.Send(&protocol.Clipboard{Text: "copied"}); err != nil {
		t.Fatal(err)
	}
	if c, ok := next(t, client).(*protocol.Clipboard); !ok || c.Text != "copied" {
		t.Fatalf("got %+v", c)
	}
	// and a picture, with its tiles as messages on the channel
	editor := loadPicture(t, "editor")
	p, err := NewPresenter(context.Background(), s.conn, PresenterOptions{Clock: RealClock{}, Logger: discardLog()})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	screen := NewScreen(client, discardLog(), nil)
	waitClient := drive(client, func(m protocol.Message) { screen.Handle(m) })
	waitHost := drive(s.conn, func(m protocol.Message) { p.Handle(m) })
	p.Present(editor)
	eventually(t, "the picture over SSH", func() bool { return screen.Confirmed() == 1 })
	same := false
	screen.Draw(func(img *image.RGBA) { same = bytes.Equal(img.Pix, editor.Pix) })
	if !same {
		t.Error("the picture over SSH differs")
	}
	if st := s.conn.Stats(); st.DatagramsSent != 0 || st.TilesSent != 260 {
		t.Errorf("host stats %+v: want 260 tiles as messages and no datagram", st)
	}
	s.End(protocol.ReasonHost, "done")
	waitClient()
	waitHost()
	if proxy.forwarded.Load() != 0 {
		t.Errorf("the blocked proxy forwarded %d packets", proxy.forwarded.Load())
	}
}

func TestAHostWithoutQUICRunsOverSSH(t *testing.T) {
	noLeaks(t)
	r := newRig(t, rigOptions{noQUIC: true})
	if r.host.Fingerprint() != nil || r.host.Addr() != nil {
		t.Fatal("a host with no socket has a certificate")
	}
	s, ch := r.session()
	client := r.connect(r.opts(s, ch))
	if client.Kind() != KindSSH {
		t.Fatalf("kind = %v", client.Kind())
	}
	if err := client.Send(&protocol.Ping{Seq: 2}); err != nil {
		t.Fatal(err)
	}
	if p, ok := next(t, s.conn).(*protocol.Ping); !ok || p.Seq != 2 {
		t.Fatalf("got %+v", p)
	}
}

func TestADroppedQUICConnectionIsResumedWithTheToken(t *testing.T) {
	noLeaks(t)
	r := newRig(t)
	s, ch := r.session()
	client := r.connect(r.opts(s, ch))
	host := s.conn
	eventually(t, "the host's link", func() bool { return host.Kind() == KindQUIC })
	dropLink(t, host)
	eventually(t, "the client to notice", func() bool { return client.lastLinkError() != nil })
	// messages sent while there is no link wait in the queue
	if err := client.Send(&protocol.Ping{Seq: 41}); err != nil {
		t.Fatal(err)
	}
	if err := host.Send(&protocol.Ping{Seq: 42, Reply: true}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the client to resume", func() bool { return client.Stats().Resumes == 1 && client.Kind() == KindQUIC })
	eventually(t, "the host to take the new link", func() bool { return host.Stats().Resumes == 1 && host.Kind() == KindQUIC })
	// the client asks for a whole picture, then its queued message arrives; the host's queued message reaches the client
	if _, ok := next(t, host).(*protocol.Refresh); !ok {
		t.Error("the first message after the resume is not the Refresh")
	}
	if p, ok := next(t, host).(*protocol.Ping); !ok || p.Seq != 41 {
		t.Errorf("got %+v", p)
	}
	if p, ok := next(t, client).(*protocol.Ping); !ok || p.Seq != 42 {
		t.Errorf("got %+v", p)
	}
	// tiles flow again
	if err := host.SendTiles(1, fakeTiles(1, 2, 700)); err != nil {
		t.Fatal(err)
	}
	nextOfType(t, client, protocol.TypeTile)
	if _, over := s.ended(); over {
		t.Error("the session ended")
	}
}

func TestResumeFallsBackToSSHWhenUDPDoesNotComeBack(t *testing.T) {
	noLeaks(t)
	r := newRig(t)
	s, ch := r.session()
	proxy := newProxy(t, r.host.Addr(), 0, 0, 2)
	o := r.opts(s, ch)
	o.Address = proxy.Addr()
	o.FallbackAfter = 250 * time.Millisecond
	o.ResumeAttempts = 2
	o.MaxIdle, o.KeepAlive = time.Second, 200*time.Millisecond // the client notices a dead path after a second
	client := r.connect(o)
	host := s.conn
	eventually(t, "the host's link", func() bool { return host.Kind() == KindQUIC })
	proxy.block.Store(true)
	dropLink(t, host)
	eventually(t, "the session to run over SSH", func() bool { return client.Kind() == KindSSH && host.Kind() == KindSSH })
	if err := client.Send(&protocol.Ping{Seq: 5}); err != nil {
		t.Fatal(err)
	}
	for {
		if p, ok := next(t, host).(*protocol.Ping); ok {
			if p.Seq != 5 {
				t.Fatalf("got %+v", p)
			}
			break
		}
	}
}

// rawDial opens a QUIC connection to the host as a test client that does not use ClientConn.
func rawDial(t *testing.T, addr string, alpn string) (*quic.Conn, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return quic.DialAddr(ctx, addr, &tls.Config{InsecureSkipVerify: true, NextProtos: []string{alpn}, MinVersion: tls.VersionTLS13}, &quic.Config{EnableDatagrams: true, MaxIdleTimeout: 5 * time.Second})
}

func TestTheHostRefusesConnectionsThatDoNotOpenLikeRemoteWork(t *testing.T) {
	noLeaks(t)
	r := newRig(t)
	good, _ := r.session()
	hello := func(version uint16, token []byte) []byte {
		b, err := protocol.Append(nil, &protocol.Hello{Version: version, Token: token})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	key, err := protocol.Append(nil, &protocol.Key{Kind: protocol.KeyDown, Logical: 1, Text: "a"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		first []byte
		route string
		code  quic.ApplicationErrorCode
	}{
		{"a first message that is not a Hello", key, "readHello.TransportHelloInvalid", codeProtocol},
		{"a clipboard that claims a megabyte, refused by its header", []byte{byte(protocol.TypeClipboard), 0, 0x10, 0, 0}, "readHello.TransportHelloInvalid", codeProtocol},
		{"a Hello of another version", hello(protocol.Version+1, good.token), "Host.admit->Hello.Check.RemoteWorkVersion", codeVersion},
		{"a token that is not a session's", hello(protocol.Version, bytes.Repeat([]byte{9}, 32)), "Host.admit.TransportTokenInvalid", codeToken},
		{"an empty token", hello(protocol.Version, nil), "Host.admit.TransportTokenInvalid", codeToken},
		{"bytes that are no message", bytes.Repeat([]byte{0xff}, 20), "readHello.TransportHelloInvalid", codeProtocol},
		{"a Hello with a lying length", []byte{byte(protocol.TypeHello), 0xff, 0xff, 0xff, 0xff}, "readHello.TransportHelloInvalid", codeProtocol},
		{"a Hello with a bad magic", append([]byte{byte(protocol.TypeHello), 0, 0, 0, 7}, 'N', 'O', 'P', 'E', 0, 1, 0), "Hello.decode.RemoteWorkBadMagic", codeProtocol},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			qc, err := rawDial(t, r.addr(), ALPN)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = qc.CloseWithError(0, "") }() // may be closed by the host already
			st, err := qc.OpenStreamSync(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := st.Write(tt.first); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-r.refused:
				wantRoute(t, err, tt.route)
			case <-time.After(5 * time.Second):
				t.Fatal("the host did not refuse")
			}
			_ = st.SetReadDeadline(time.Now().Add(5 * time.Second))
			_, err = st.Read(make([]byte, 1))
			var ae *quic.ApplicationError
			if !errors.As(err, &ae) || ae.ErrorCode != tt.code {
				t.Errorf("the host closed the connection with %v, want code %d", err, tt.code)
			}
		})
	}
	if _, over := good.ended(); over {
		t.Error("a refused connection ended the session")
	}
}

func TestAnotherALPNIsNotServed(t *testing.T) {
	noLeaks(t)
	r := newRig(t)
	if qc, err := rawDial(t, r.addr(), "h3"); err == nil {
		_ = qc.CloseWithError(0, "")
		t.Fatal("the host completed a handshake for another application protocol")
	}
}

func TestConnectionsThatNeverOpenAStreamAreClosedAndTheNumberOfThemIsLimited(t *testing.T) {
	noLeaks(t)
	r := newRig(t, rigOptions{host: func(o *HostOptions) { o.MaxPending = 1; o.HelloTimeout = 400 * time.Millisecond }})
	first, err := rawDial(t, r.addr(), ALPN)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = first.CloseWithError(0, "") }() // closed by the host already
	// the second finds the waiting room full
	second, err := rawDial(t, r.addr(), ALPN)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = second.CloseWithError(0, "") }() // closed by the host already
	select {
	case <-second.Context().Done():
		var ae *quic.ApplicationError
		if !errors.As(context.Cause(second.Context()), &ae) || ae.ErrorCode != codeBusy {
			t.Errorf("the second connection was closed with %v, want the code for busy", context.Cause(second.Context()))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the second connection was not closed")
	}
	// the first gets its time, then goes
	select {
	case err := <-r.refused:
		wantRoute(t, err, "Host.admit.TransportHelloInvalid")
	case <-time.After(5 * time.Second):
		t.Fatal("the connection that sent nothing was not given up")
	}
	select {
	case <-first.Context().Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the first connection was not closed")
	}
}

func TestAStalledWriteGivesTheLinkUp(t *testing.T) {
	noLeaks(t)
	clk := newClock()
	c := bare(coreOptions{clock: clk, stall: 10 * time.Second})
	pr, pw := io.Pipe()
	var wmu sync.Mutex
	aborted := make(chan struct{})
	l := &link{kind: KindSSH, w: pw, wmu: &wmu, done: make(chan struct{}), abort: func() { _ = pw.CloseWithError(io.ErrClosedPipe); close(aborted) }}
	errc := make(chan error, 1)
	go func() { errc <- c.write(l, []byte("nobody reads this")) }()
	eventually(t, "the stall timer", func() bool { return clk.Pending() == 1 })
	clk.Advance(10 * time.Second)
	select {
	case <-aborted:
	case <-time.After(5 * time.Second):
		t.Fatal("the link was not given up")
	}
	wantRoute(t, <-errc, "write.TransportLinkFailed")
	_ = pr.Close()
}

func TestADialToNothingIsAFailureToTryTheSSHChannelInstead(t *testing.T) {
	noLeaks(t)
	dead, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	addr := dead.LocalAddr().String()
	_ = dead.Close() // nothing listens there any more
	c := &ClientConn{o: ClientOptions{Address: addr, CertSHA256: make([]byte, 32)}, fallback: 300 * time.Millisecond}
	c.core = bare(coreOptions{})
	_, _, err = c.dialQUIC(context.Background())
	wantRoute(t, err, "dialQUIC.TransportDialFailed")
	if fatal(err) {
		t.Error("a dial that failed is not fatal: the SSH channel is tried")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err = c.dialQUIC(ctx)
	wantRoute(t, err, "dialQUIC.TransportCancelled")
}

func TestTheHostsCertificateAndSocketFail(t *testing.T) {
	noLeaks(t)
	t.Run("the random source fails", func(t *testing.T) {
		udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = udp.Close() }() // the host never took it
		_, err = NewHost(context.Background(), HostOptions{PacketConn: udp, Random: failingReader{}, Lookup: func([]byte) (*HostConn, bool) { return nil, false }})
		wantRoute(t, err, "NewHost.TransportCertFailed")
	})
	t.Run("the socket is closed", func(t *testing.T) {
		udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		_ = udp.Close() // closed on purpose
		_, err = NewHost(context.Background(), HostOptions{PacketConn: udp, Lookup: func([]byte) (*HostConn, bool) { return nil, false }})
		wantRoute(t, err, "NewHost.TransportListenFailed")
	})
	t.Run("two hosts have two certificates", func(t *testing.T) {
		a, b := newRig(t), newRig(t)
		fa, fb := a.host.Fingerprint(), b.host.Fingerprint()
		if len(fa) != 32 || len(fb) != 32 || bytes.Equal(fa, fb) {
			t.Errorf("fingerprints %x and %x", fa, fb)
		}
	})
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrNoProgress }
