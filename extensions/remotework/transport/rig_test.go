package transport

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"io"
	"log/slog"
	"math/rand/v2"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
)

// fakeClock is a clock and a set of timers a test moves by hand.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	timers []*fakeTimer
}

type fakeTimer struct {
	at      time.Time
	f       func()
	stopped bool
	fired   bool
}

func newClock() *fakeClock { return &fakeClock{now: time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) func() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	t := &fakeTimer{at: c.now.Add(d), f: f}
	c.timers = append(c.timers, t)
	return func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		was := !t.stopped && !t.fired
		t.stopped = true
		return was
	}
}

// Advance moves time forward and runs the timers that are due, in order.
func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	var due []*fakeTimer
	for _, t := range c.timers {
		if !t.stopped && !t.fired && !t.at.After(c.now) {
			t.fired = true
			due = append(due, t)
		}
	}
	c.mu.Unlock()
	for _, t := range due {
		t.f()
	}
}

// Pending is the number of timers that have not fired or been stopped.
func (c *fakeClock) Pending() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, t := range c.timers {
		if !t.stopped && !t.fired {
			n++
		}
	}
	return n
}

// eventually polls cond for up to 10 s.
func eventually(t testing.TB, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

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

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// tcpPair makes the two ends of a loopback TCP connection: the SSH channel of a test (buffered, like the real one).
func tcpPair(t testing.TB) (host, client net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }() // the listener served its one connection
	type result struct {
		c   net.Conn
		err error
	}
	ch := make(chan result, 1)
	go func() {
		c, err := ln.Accept()
		ch <- result{c, err}
	}()
	client, err = net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	r := <-ch
	if r.err != nil {
		t.Fatal(r.err)
	}
	t.Cleanup(func() { _ = r.c.Close(); _ = client.Close() }) // already closed by most tests
	return r.c, client
}

// fakeSession is a session of the hub as the transport sees it.
type fakeSession struct {
	token   []byte
	rw      io.ReadWriteCloser
	touches atomic.Int64
	endCh   chan struct{}
	over    chan struct{}
	once    sync.Once
	reason  atomic.Int32
	mu      sync.Mutex
	text    string
	conn    *HostConn
}

func (s *fakeSession) Token() []byte               { return s.token }
func (s *fakeSession) Channel() io.ReadWriteCloser { return s.rw }
func (s *fakeSession) Touch()                      { s.touches.Add(1) }
func (s *fakeSession) Done() <-chan struct{}       { return s.endCh }

// End does what the hub's Session.End does: the transport says goodbye, then the connection closes.
func (s *fakeSession) End(reason protocol.Reason, text string) {
	s.once.Do(func() {
		s.reason.Store(int32(reason))
		s.mu.Lock()
		s.text = text
		s.mu.Unlock()
		close(s.endCh)
		go func() {
			defer close(s.over)
			if s.conn != nil {
				s.conn.Shutdown(reason, text)
			}
			_ = s.rw.Close() // the hub closes the SSH connection after the goodbye
		}()
	})
}

func (s *fakeSession) ended() (protocol.Reason, bool) {
	select {
	case <-s.endCh:
		return protocol.Reason(s.reason.Load()), true
	default:
		return 0, false
	}
}

// rig is a host with its UDP socket and a table of sessions by token.
type rig struct {
	t       *testing.T
	clk     *fakeClock
	host    *Host
	mu      sync.Mutex
	byToken map[string]*fakeSession
	refused chan error
}

type rigOptions struct {
	noQUIC bool
	host   func(*HostOptions)
}

func newRig(t *testing.T, ro ...rigOptions) *rig {
	t.Helper()
	r := &rig{t: t, clk: newClock(), byToken: map[string]*fakeSession{}, refused: make(chan error, 64)}
	var o rigOptions
	if len(ro) > 0 {
		o = ro[0]
	}
	opts := HostOptions{
		Lookup: r.lookup, Clock: r.clk, Logger: discardLog(),
		OnRefused: func(_ Kind, _ string, err error) {
			select {
			case r.refused <- err:
			default:
			}
		},
	}
	if !o.noQUIC {
		udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		if err != nil {
			t.Fatal(err)
		}
		opts.PacketConn = udp
	}
	if o.host != nil {
		o.host(&opts)
	}
	h, err := NewHost(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	r.host = h
	t.Cleanup(h.Close)
	return r
}

func (r *rig) lookup(token []byte) (*HostConn, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.byToken[string(token)]
	if !ok {
		return nil, false
	}
	if _, over := s.ended(); over {
		return nil, false
	}
	return s.conn, true
}

// session makes a session with a token and attaches a connection to it. client is the other end of its SSH channel.
func (r *rig) session() (*fakeSession, net.Conn) {
	r.t.Helper()
	hostEnd, clientEnd := tcpPair(r.t)
	s := &fakeSession{token: make([]byte, 32), rw: hostEnd, endCh: make(chan struct{}), over: make(chan struct{})}
	if _, err := crand.Read(s.token); err != nil {
		r.t.Fatal(err)
	}
	r.mu.Lock()
	s.conn = r.host.Attach(s)
	r.byToken[string(s.token)] = s
	r.mu.Unlock()
	s.conn.Start()
	r.t.Cleanup(func() {
		s.End(protocol.ReasonClosed, "the test is over")
		<-s.over
		s.conn.Wait()
	})
	return s, clientEnd
}

// addr is the UDP address of the host.
func (r *rig) addr() string {
	if a := r.host.Addr(); a != nil {
		return a.String()
	}
	return ""
}

// opts are the client options for a session; the caller may change them.
func (r *rig) opts(s *fakeSession, channel io.ReadWriteCloser) ClientOptions {
	return ClientOptions{
		Address: r.addr(), Token: s.token, CertSHA256: r.host.Fingerprint(), Channel: channel, Logger: discardLog(),
		RepairAfter: 30 * time.Millisecond,
	}
}

// connect connects a client with the options and closes it when the test ends.
func (r *rig) connect(o ClientOptions) *ClientConn {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := Connect(ctx, o)
	if err != nil {
		r.t.Fatalf("Connect: %v", err)
	}
	r.t.Cleanup(func() {
		_ = c.Close(protocol.ReasonClosed, "the test is over")
		c.wait()
	})
	return c
}

// next reads the next message with a limit of 10 s.
func next(t testing.TB, c Conn) protocol.Message {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	m, err := c.Next(ctx)
	if err != nil {
		t.Fatalf("Next: %v", err)
	}
	return m
}

// nextOfType reads messages until one has the type.
func nextOfType(t testing.TB, c Conn, typ protocol.Type) protocol.Message {
	t.Helper()
	for {
		m := next(t, c)
		if m.Type() == typ {
			return m
		}
	}
}

// udpProxy forwards UDP between a client and a server on the loopback, dropping a share of the packets and delaying all of
// them, in order. It is the lossy link of the measurement and the blocked UDP of the fallback tests.
type udpProxy struct {
	front, back *net.UDPConn
	server      *net.UDPAddr
	loss        float64
	delay       time.Duration
	block       atomic.Bool
	mu          sync.Mutex
	rng         *rand.Rand
	client      *net.UDPAddr
	forwarded   atomic.Int64
	dropped     atomic.Int64
	toServer    chan delayed
	toClient    chan delayed
	wg          sync.WaitGroup
	closeOnce   sync.Once
	closed      chan struct{}
}

type delayed struct {
	at time.Time
	b  []byte
}

func newProxy(t testing.TB, server net.Addr, loss float64, delay time.Duration, seed uint64) *udpProxy {
	t.Helper()
	front, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	back, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	p := &udpProxy{
		front: front, back: back, server: server.(*net.UDPAddr), loss: loss, delay: delay, rng: rand.New(rand.NewPCG(seed, seed+1)),
		toServer: make(chan delayed, 8192), toClient: make(chan delayed, 8192), closed: make(chan struct{}),
	}
	p.wg.Add(4)
	go p.read(front, func(a *net.UDPAddr) {
		p.mu.Lock()
		p.client = a
		p.mu.Unlock()
	}, p.toServer)
	go p.read(back, nil, p.toClient)
	go p.send(p.toServer, func(b []byte) { _, _ = back.WriteToUDP(b, p.server) })
	go p.send(p.toClient, func(b []byte) {
		p.mu.Lock()
		c := p.client
		p.mu.Unlock()
		if c != nil {
			_, _ = front.WriteToUDP(b, c)
		}
	})
	t.Cleanup(p.Close)
	return p
}

func (p *udpProxy) Addr() string { return p.front.LocalAddr().String() }

func (p *udpProxy) read(c *net.UDPConn, learn func(*net.UDPAddr), out chan<- delayed) {
	defer p.wg.Done()
	buf := make([]byte, 65536)
	for {
		n, from, err := c.ReadFromUDP(buf)
		if err != nil {
			return
		}
		if learn != nil {
			learn(from)
		}
		p.mu.Lock()
		drop := p.block.Load() || (p.loss > 0 && p.rng.Float64() < p.loss)
		p.mu.Unlock()
		if drop {
			p.dropped.Add(1)
			continue
		}
		pkt := delayed{at: time.Now().Add(p.delay), b: bytes.Clone(buf[:n])}
		select {
		case out <- pkt:
		default:
			p.dropped.Add(1)
		}
	}
}

func (p *udpProxy) send(in <-chan delayed, write func([]byte)) {
	defer p.wg.Done()
	for {
		select {
		case pkt := <-in:
			if d := time.Until(pkt.at); d > 0 {
				t := time.NewTimer(d)
				select {
				case <-t.C:
				case <-p.closed:
					t.Stop()
					return
				}
			}
			write(pkt.b)
			p.forwarded.Add(1)
		case <-p.closed:
			return
		}
	}
}

// Close stops the proxy.
func (p *udpProxy) Close() {
	p.closeOnce.Do(func() {
		close(p.closed)
		_ = p.front.Close() // sockets of a test
		_ = p.back.Close()
		p.wg.Wait()
	})
}
