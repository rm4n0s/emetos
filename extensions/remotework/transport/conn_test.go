package transport

import (
	"bytes"
	"context"
	"image"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
)

// drive reads a Conn until it ends, giving the messages to f. It returns a function that waits for the end.
func drive(c Conn, f func(protocol.Message)) (wait func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			m, err := c.Next(context.Background())
			if err != nil {
				return
			}
			f(m)
		}
	}()
	return func() { <-done }
}

func TestASessionRunsOverQUIC(t *testing.T) {
	noLeaks(t)
	r := newRig(t)
	s, ch := r.session()
	client := r.connect(r.opts(s, ch))
	host := s.conn
	if client.Kind() != KindQUIC {
		t.Fatalf("the client runs over %v, want QUIC", client.Kind())
	}
	eventually(t, "the host to have its link", func() bool { return host.Kind() == KindQUIC })

	// reliable messages, both ways, in order
	for i := range 5 {
		if err := client.Send(&protocol.Key{Kind: protocol.KeyDown, Physical: uint64(i), Logical: 1, Text: "a"}); err != nil {
			t.Fatal(err)
		}
	}
	for i := range 5 {
		k, ok := next(t, host).(*protocol.Key)
		if !ok || k.Physical != uint64(i) {
			t.Fatalf("message %d: %+v", i, k)
		}
	}
	if err := host.Send(&protocol.Notification{ID: 4, Source: "git", Message: "pulled", Actions: []string{"Open"}}); err != nil {
		t.Fatal(err)
	}
	if n, ok := next(t, client).(*protocol.Notification); !ok || n.ID != 4 || n.Actions[0] != "Open" {
		t.Fatalf("notification: %+v", n)
	}
	if got := s.touches.Load(); got < 5 {
		t.Errorf("the session was touched %d times for 5 messages", got)
	}

	// a picture: the host presents it, the client's screen shows it and confirms it
	editor := loadPicture(t, "editor")
	p, err := NewPresenter(context.Background(), host, PresenterOptions{Clock: RealClock{}, Logger: discardLog()})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	screen := NewScreen(client, discardLog(), nil)
	waitClient := drive(client, func(m protocol.Message) { screen.Handle(m) })
	waitHost := drive(host, func(m protocol.Message) { p.Handle(m) })
	p.Present(editor)
	time.Sleep(3 * time.Second)
	t.Logf("host %+v client %+v confirmed %d kinds %v %v", host.Stats(), client.Stats(), screen.Confirmed(), host.Kind(), client.Kind())
	eventually(t, "the whole picture on the client", func() bool {
		return screen.Confirmed() == 1
	})
	t.Logf("host %+v client %+v", host.Stats(), client.Stats())
	same := false
	screen.Draw(func(img *image.RGBA) { same = bytes.Equal(img.Pix, editor.Pix) })
	if !same {
		t.Error("the client's picture is not the host's")
	}
	st := host.Stats()
	if st.DatagramsSent == 0 || st.TilesSent != 260 {
		t.Errorf("host stats %+v: want 260 tiles sent as datagrams", st)
	}
	if cs := client.Stats(); cs.TilesReceived != 260 {
		t.Errorf("client received %d tiles, want 260", cs.TilesReceived)
	}
	// one typed character is a few tiles
	typed := loadPicture(t, "typed")
	p.Present(typed)
	eventually(t, "the typed picture", func() bool { return screen.Confirmed() == 2 })
	if st2 := host.Stats(); st2.TilesSent-st.TilesSent > 20 {
		t.Errorf("one typed character sent %d tiles", st2.TilesSent-st.TilesSent)
	}

	// the host ends the session: the client is told why, then the connection ends
	s.End(protocol.ReasonHost, "bye")
	waitClient()
	waitHost()
	select {
	case <-client.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the client did not end")
	}
	wantRoute(t, client.Err(), "TransportClosed")
}

func TestTheClientIsToldWhyTheSessionEnded(t *testing.T) {
	noLeaks(t)
	for _, tt := range []struct {
		name  string
		noUDP bool
	}{{"over QUIC", false}, {"over SSH", true}} {
		t.Run(tt.name, func(t *testing.T) {
			r := newRig(t, rigOptions{noQUIC: tt.noUDP})
			s, ch := r.session()
			o := r.opts(s, ch)
			if tt.noUDP {
				o.CertSHA256 = nil
			}
			client := r.connect(o)
			want := KindQUIC
			if tt.noUDP {
				want = KindSSH
			}
			if client.Kind() != want {
				t.Fatalf("kind = %v, want %v", client.Kind(), want)
			}
			s.End(protocol.ReasonIdle, "no activity for 30m0s")
			d, ok := next(t, client).(*protocol.Disconnect)
			if !ok || d.Reason != protocol.ReasonIdle || d.Text != "no activity for 30m0s" {
				t.Fatalf("got %+v", d)
			}
			_, err := client.Next(context.Background())
			wantRoute(t, err, "Next.TransportClosed")
			<-client.Done()
		})
	}
}

func TestTheClientsDisconnectEndsTheHostsSession(t *testing.T) {
	noLeaks(t)
	r := newRig(t)
	s, ch := r.session()
	client := r.connect(r.opts(s, ch))
	eventually(t, "the host to have its link", func() bool { return s.conn.Kind() == KindQUIC })
	if err := client.Close(protocol.ReasonClosed, "closed the tab"); err != nil {
		t.Fatal(err)
	}
	eventually(t, "the host to end the session", func() bool { _, over := s.ended(); return over })
	d, ok := next(t, s.conn).(*protocol.Disconnect)
	if !ok || d.Reason != protocol.ReasonClosed {
		t.Fatalf("the host got %+v", d)
	}
	<-s.over
}

func TestARefusedTokenHoldsNothing(t *testing.T) {
	noLeaks(t)
	r := newRig(t)
	s, ch := r.session()
	good := r.opts(s, ch)
	bad := good
	bad.Token = bytes.Repeat([]byte{7}, 32)
	_, bad.Channel = tcpPair(t) // a failed connection closes its channel
	_, err := Connect(context.Background(), bad)
	wantRoute(t, err, "TransportRefused")
	select {
	case err := <-r.refused:
		wantRoute(t, err, "Host.admit.TransportTokenInvalid")
	case <-time.After(5 * time.Second):
		t.Fatal("the host did not report the refusal")
	}
	// a token of a session that ended is as good as none
	s2, ch2 := r.session()
	o2 := r.opts(s2, ch2)
	_ = ch2
	s2.End(protocol.ReasonHost, "gone")
	<-s2.over
	_, err = Connect(context.Background(), o2)
	wantRoute(t, err, "TransportRefused")
	// the session of the good token still works
	client := r.connect(good)
	if client.Kind() != KindQUIC {
		t.Errorf("kind = %v", client.Kind())
	}
}

func TestAWrongCertificateIsRefusedAndNothingIsSent(t *testing.T) {
	noLeaks(t)
	r := newRig(t)
	s, ch := r.session()
	o := r.opts(s, ch)
	o.CertSHA256 = bytes.Repeat([]byte{0xAB}, 32)
	_, err := Connect(context.Background(), o)
	wantRoute(t, err, "dialQUIC.TransportCertMismatch")
	appErr, _ := errors.FromError(err)
	if got, _ := appErr.Metadata["expected"].(string); got != fingerprintHex(o.CertSHA256) {
		t.Errorf("metadata %v", appErr.Metadata)
	}
	// the client closed the handshake before it opened a stream: the host never saw a token, and nothing was sent
	select {
	case err := <-r.refused:
		t.Errorf("the host admitted or refused a connection: %v", err)
	case <-time.After(300 * time.Millisecond):
	}
	if s.touches.Load() != 0 {
		t.Error("the session was touched")
	}
}

func TestBadOptionsAreRefused(t *testing.T) {
	noLeaks(t)
	good := ClientOptions{Address: "127.0.0.1:1", Token: []byte{1}, Channel: nopCloser{}}
	tests := []struct {
		name string
		mod  func(*ClientOptions)
	}{
		{"no token", func(o *ClientOptions) { o.Token = nil }},
		{"token too long", func(o *ClientOptions) { o.Token = make([]byte, protocol.MaxToken+1) }},
		{"no channel", func(o *ClientOptions) { o.Channel = nil }},
		{"a fingerprint of 5 bytes", func(o *ClientOptions) { o.CertSHA256 = []byte{1, 2, 3, 4, 5} }},
		{"a fingerprint and no address", func(o *ClientOptions) { o.CertSHA256 = make([]byte, 32); o.Address = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := good
			tt.mod(&o)
			_, err := Connect(context.Background(), o)
			wantRoute(t, err, "Connect.TransportOptionsInvalid")
		})
	}
}

// nopCloser is a channel for options that are refused before it is used.
type nopCloser struct{}

func (nopCloser) Read([]byte) (int, error)    { return 0, io.EOF }
func (nopCloser) Write(b []byte) (int, error) { return len(b), nil }
func (nopCloser) Close() error                { return nil }

// silentChannel is an SSH channel of a host that never answers: reads wait until it is closed.
type silentChannel struct {
	once sync.Once
	done chan struct{}
}

func newSilentChannel() *silentChannel { return &silentChannel{done: make(chan struct{})} }

func (c *silentChannel) Read([]byte) (int, error)    { <-c.done; return 0, io.EOF }
func (c *silentChannel) Write(b []byte) (int, error) { return len(b), nil }
func (c *silentChannel) Close() error                { c.once.Do(func() { close(c.done) }); return nil }

func TestSendRefusesWhatItShouldNot(t *testing.T) {
	noLeaks(t)
	r := newRig(t)
	s, ch := r.session()
	client := r.connect(r.opts(s, ch))
	tests := []struct {
		name  string
		m     protocol.Message
		route string
	}{
		{"a tile", &protocol.Tile{Frame: 1, Width: 1, Height: 1, Total: 1, Data: []byte{1}}, "Send.TransportMessageInvalid"},
		{"a hello", &protocol.Hello{Version: 1}, "Send.TransportMessageInvalid"},
		{"a missing", &protocol.Missing{Frame: 1, Total: 1, Bitmap: []byte{1}}, "Send.TransportMessageInvalid"},
		{"a message the protocol refuses", &protocol.Resize{}, "Resize.encode.RemoteWorkEncodeInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { wantRoute(t, client.Send(tt.m), tt.route) })
	}
}

func TestSendTilesRefusesWhatItShouldNot(t *testing.T) {
	noLeaks(t)
	r := newRig(t)
	s, ch := r.session()
	r.connect(r.opts(s, ch))
	ok := &protocol.Tile{Frame: 3, Width: 64, Height: 64, Total: 1, Data: []byte{1}}
	tests := []struct {
		name  string
		frame uint64
		tiles []*protocol.Tile
	}{
		{"frame 0", 0, []*protocol.Tile{ok}},
		{"no tiles", 3, nil},
		{"too many tiles", 3, make([]*protocol.Tile, protocol.MaxFrameTiles+1)},
		{"a tile of another frame", 4, []*protocol.Tile{ok}},
		{"a nil tile", 3, []*protocol.Tile{nil}},
		{"a tile that counts another total", 3, []*protocol.Tile{ok, ok}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantRoute(t, s.conn.SendTiles(tt.frame, tt.tiles), "SendTiles.TransportTilesInvalid")
		})
	}
}

func TestACancelledNextAndAClosedConn(t *testing.T) {
	noLeaks(t)
	r := newRig(t)
	s, ch := r.session()
	client := r.connect(r.opts(s, ch))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client.Next(ctx)
	wantRoute(t, err, "Next.TransportCancelled")
	if err := client.Close(protocol.ReasonClosed, ""); err != nil {
		t.Fatal(err)
	}
	<-client.Done()
	wantRoute(t, client.Send(&protocol.Refresh{}), "enqueue.TransportClosed")
	wantRoute(t, client.SendTiles(1, []*protocol.Tile{{Frame: 1, Width: 1, Height: 1, Total: 1, Data: []byte{1}}}), "SendTiles.TransportClosed")
	if err := client.Close(protocol.ReasonClosed, ""); err != nil {
		t.Fatal(err) // the second close does nothing
	}
}

// bare is a connection with no link and no peer, to test the queues.
func bare(opts coreOptions) *core {
	if opts.log == nil {
		opts.log = discardLog()
	}
	opts.end = func(protocol.Reason, string) {}
	return newCore(opts)
}

func TestTheQueueOfReliableMessagesIsBounded(t *testing.T) {
	noLeaks(t)
	tests := []struct {
		name string
		opts coreOptions
		msgs func(i int) protocol.Message
		want int // how many fit
	}{
		{"by count", coreOptions{queueMessages: 3}, func(int) protocol.Message { return &protocol.Refresh{} }, 3},
		{"by bytes", coreOptions{queueBytes: 3 << 20}, func(int) protocol.Message { return &protocol.Clipboard{Text: string(make([]byte, 1<<20))} }, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := bare(tt.opts)
			n := 0
			for ; n < 100; n++ {
				if err := c.Send(tt.msgs(n)); err != nil {
					wantRoute(t, err, "enqueue.TransportQueueFull")
					break
				}
			}
			if n != tt.want {
				t.Errorf("%d messages fit, want %d", n, tt.want)
			}
		})
	}
}

func TestOnlyTheNewestPictureWaitsToBeSent(t *testing.T) {
	noLeaks(t)
	c := bare(coreOptions{})
	mk := func(frame uint64, n int) []*protocol.Tile {
		out := make([]*protocol.Tile, n)
		for i := range out {
			out[i] = &protocol.Tile{Frame: frame, Width: 1280, Height: 800, X: uint16(i), Total: uint32(n), Data: []byte{1}}
		}
		return out
	}
	for f := uint64(1); f <= 4; f++ {
		if err := c.SendTiles(f, mk(f, 5)); err != nil {
			t.Fatal(err)
		}
	}
	if got := c.Stats().BatchesDropped; got != 3 {
		t.Errorf("%d pictures dropped before they were sent, want 3", got)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.batch == nil || c.batch.frame != 4 {
		t.Errorf("the batch is %+v, want the picture 4", c.batch)
	}
}

func TestAFullInboundQueueSlowsReliableMessagesAndDropsTiles(t *testing.T) {
	noLeaks(t)
	r := newRig(t, rigOptions{host: func(o *HostOptions) { o.Inbound = 4; o.QueueBytes = 6 << 20 }})
	s, ch := r.session()
	o := r.opts(s, ch)
	o.QueueBytes = 6 << 20
	client := r.connect(o)
	// the host does not call Next: its inbound queue holds 4 messages, the stream window some megabytes, then the client's
	// queue is full and Send says so, instead of the client buffering for ever
	big := &protocol.Clipboard{Text: string(make([]byte, protocol.MaxClipboard))}
	sent, refused := 0, false
	deadline := time.Now().Add(20 * time.Second)
	for !refused && time.Now().Before(deadline) {
		if err := client.Send(big); err != nil {
			wantRoute(t, err, "enqueue.TransportQueueFull")
			refused = true
			break
		}
		sent++
		time.Sleep(time.Millisecond)
	}
	if !refused {
		t.Fatalf("%d clipboards were accepted without the queue filling", sent)
	}
	if sent > 40 {
		t.Errorf("%d MiB were accepted before the queue was full", sent)
	}
}

func TestMalformedDatagramsAreCountedAndDoNotEndTheSession(t *testing.T) {
	noLeaks(t)
	r := newRig(t)
	s, ch := r.session()
	client := r.connect(r.opts(s, ch))
	eventually(t, "the host's link", func() bool { return s.conn.Kind() == KindQUIC })
	s.conn.mu.Lock()
	q := s.conn.cur.q
	s.conn.mu.Unlock()
	for _, b := range [][]byte{{1}, bytes.Repeat([]byte{0xff}, 40), make([]byte, 14), append(make([]byte, 13), 1, 1, 1, 1)} {
		if err := q.SendDatagram(b); err != nil {
			t.Fatal(err)
		}
	}
	eventually(t, "the datagrams to be counted", func() bool { return client.Stats().DatagramsInvalid >= 4 })
	if err := s.conn.Send(&protocol.Ping{Seq: 1}); err != nil {
		t.Fatal(err)
	}
	if p, ok := next(t, client).(*protocol.Ping); !ok || p.Seq != 1 {
		t.Fatalf("got %+v", p)
	}
}

func TestAProtocolViolationEndsTheSession(t *testing.T) {
	noLeaks(t)
	r := newRig(t)
	s, ch := r.session()
	client := r.connect(r.opts(s, ch))
	eventually(t, "the host's link", func() bool { return s.conn.Kind() == KindQUIC })
	b, err := protocol.Append(nil, &protocol.Hello{Version: protocol.Version})
	if err != nil {
		t.Fatal(err)
	}
	if err := client.enqueue(b, protocol.TypeHello); err != nil {
		t.Fatal(err)
	}
	d, ok := next(t, client).(*protocol.Disconnect)
	if !ok || d.Reason != protocol.ReasonProtocol {
		t.Fatalf("got %+v", d)
	}
	if reason, over := s.ended(); !over || reason != protocol.ReasonProtocol {
		t.Errorf("session ended = %v %v", reason, over)
	}
}

func TestASessionWithoutATransportEnds(t *testing.T) {
	noLeaks(t)
	r := newRig(t)
	s, ch := r.session()
	_ = ch
	r.clk.Advance(DefaultFirstLink - time.Second)
	if _, over := s.ended(); over {
		t.Fatal("the session ended early")
	}
	r.clk.Advance(2 * time.Second)
	eventually(t, "the session to end", func() bool { _, over := s.ended(); return over })
	if reason, _ := s.ended(); reason != protocol.ReasonProtocol {
		t.Errorf("reason = %v", reason)
	}
	<-s.over
}

var _ sync.Locker = (*sync.Mutex)(nil)

func TestAHostThatDoesNotAnswerTheSSHHelloIsGivenUp(t *testing.T) {
	noLeaks(t)
	_, err := Connect(context.Background(), ClientOptions{Token: []byte{1, 2, 3}, Channel: newSilentChannel(), HelloTimeout: 200 * time.Millisecond, Logger: discardLog()})
	wantRoute(t, err, "attachSSH.TransportHelloInvalid")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err = Connect(ctx, ClientOptions{Token: []byte{1, 2, 3}, Channel: newSilentChannel(), Logger: discardLog()})
	wantRoute(t, err, "attachSSH.TransportCancelled")
}
