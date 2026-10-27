package transport

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/quic-go"
	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
)

// The application error codes a QUIC connection is closed with. A code from codeDisconnect up carries the reason of a
// protocol.Disconnect, so the peer learns why even when its stream is gone.
const (
	codeNormal     quic.ApplicationErrorCode = 0
	codeToken      quic.ApplicationErrorCode = 1
	codeVersion    quic.ApplicationErrorCode = 2
	codeProtocol   quic.ApplicationErrorCode = 3
	codeBusy       quic.ApplicationErrorCode = 4
	codeReplaced   quic.ApplicationErrorCode = 5
	codeDisconnect quic.ApplicationErrorCode = 0x100
)

// The bounds of a connection. Every queue has one, so a peer that does not read, or reads slowly, cannot make a side buffer
// without limit.
const (
	// DefaultQueueMessages and DefaultQueueBytes bound the reliable messages waiting to be written (a clipboard may be 1 MiB).
	DefaultQueueMessages = 512
	DefaultQueueBytes    = 8 << 20
	// DefaultInbound bounds the reliable messages waiting for Next; one that finds no room waits for it, which slows the peer
	// down (the stream's flow control).
	DefaultInbound = 256
	// MaxBufferedTiles and MaxBufferedTileBytes bound the tiles that completed and wait for Next: a whole picture of the largest
	// size is 2,040 tiles of about 435 bytes (text, 850; the worst case is 17 KB), and 16 MiB is more than the worst
	// case of a picture.
	MaxBufferedTiles     = protocol.MaxFrameTiles
	MaxBufferedTileBytes = 16 << 20
	// DefaultStall is how long one write may take before the link is given up: the peer is not reading.
	DefaultStall = 15 * time.Second
	// farewellTimeout bounds the Disconnect written when a session ends.
	farewellTimeout = time.Second
)

type role int

const (
	roleHost role = iota
	roleClient
)

func (r role) String() string {
	if r == roleHost {
		return "host"
	}
	return "client"
}

// sshEnd is the SSH channel of the session, which carries the messages when the transport is SSH, and a Disconnect whenever
// the host ends the session.
type sshEnd struct {
	rw  io.ReadWriteCloser
	wmu sync.Mutex
}

// write writes one whole message.
func (s *sshEnd) write(b []byte) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	_, err := s.rw.Write(b)
	return err
}

// link is one connection of one kind: a QUIC connection with its stream, or the SSH channel.
type link struct {
	kind Kind
	// q is the QUIC connection, nil for SSH.
	q *quic.Conn
	// w is where reliable messages are written and wmu serializes the writes of whole messages.
	w   io.Writer
	wmu *sync.Mutex
	// abort gives up the link when a write stalls.
	abort func()

	done   chan struct{}
	once   sync.Once
	cancel context.CancelFunc
	closer func(code quic.ApplicationErrorCode, text string)
}

// close ends the link, once: its goroutines stop and the QUIC connection is closed with the code and the text.
func (l *link) close(code quic.ApplicationErrorCode, text string) {
	l.once.Do(func() {
		close(l.done)
		if l.cancel != nil {
			l.cancel()
		}
		if l.closer != nil {
			l.closer(code, text)
		}
	})
}

// batch is the newest picture's tiles.
type batch struct {
	frame  uint64
	tiles  []*protocol.Tile
	next   int
	resend []int
	queued []bool
	// out is true once the first pass over the tiles is done and OnTilesSent was called.
	out bool
}

// OnTilesSent implements Conn.
func (c *core) OnTilesSent(f func(frame uint64)) { c.onSent.Store(&f) }

// coreOptions are what newCore needs.
type coreOptions struct {
	role  role
	clock Clock
	log   *slog.Logger
	// touch tells the session there was traffic (the idle timeout); nil on a client.
	touch func()
	// end asks the owner to end the session: the host's Session.End, which calls back shutdown; the client's shutdown.
	end func(reason protocol.Reason, text string)
	// ssh is the SSH channel, nil in a test of a QUIC-only connection.
	ssh *sshEnd
	// repairAfter is the quiet time before a client asks for lost tiles; 0 derives it from the round trip.
	repairAfter                        time.Duration
	queueMessages, queueBytes, inbound int
	stall                              time.Duration
}

// core is the part of a connection both sides share: the queues, the links, the tile flow and the end of the session. A
// HostConn and a ClientConn are a core with a way to make links.
type core struct {
	role  role
	clk   Clock
	log   *slog.Logger
	touch func()
	end   func(protocol.Reason, string)
	ssh   *sshEnd
	st    counters

	queueMessages, queueBytes int
	stall                     time.Duration
	repairAfter               time.Duration

	ctx    context.Context
	cancel context.CancelFunc

	in      chan protocol.Message
	tiles   *tileBuffer
	done    chan struct{}
	relWake chan struct{}
	tilWake chan struct{}
	recv    *tileReceiver
	onSent  atomic.Pointer[func(uint64)]
	chunk   atomic.Int32

	// onLinkDown is called when the current link failed and the connection lives; onFinish when it ended.
	onLinkDown func(l *link, err error)
	onFinish   func()

	mu         sync.Mutex
	cur        *link
	hadLink    bool
	queue      [][]byte
	queueSize  int
	batch      *batch
	finished   bool
	started    bool
	closing    atomic.Bool
	err        error
	peer       *protocol.Disconnect
	peerGiven  bool
	linkErr    error
	firstLink  chan struct{}
	timers     []func() bool
	goroutines sync.WaitGroup
}

func newCore(o coreOptions) *core {
	c := &core{
		role: o.role, clk: o.clock, log: o.log, touch: o.touch, end: o.end, ssh: o.ssh,
		queueMessages: o.queueMessages, queueBytes: o.queueBytes, stall: o.stall, repairAfter: o.repairAfter,
		in: make(chan protocol.Message, max(o.inbound, 1)), tiles: newTileBuffer(), done: make(chan struct{}),
		relWake: make(chan struct{}, 1), tilWake: make(chan struct{}, 1), firstLink: make(chan struct{}),
	}
	if c.clk == nil {
		c.clk = RealClock{}
	}
	if c.log == nil {
		c.log = slog.Default()
	}
	if c.queueMessages <= 0 {
		c.queueMessages = DefaultQueueMessages
	}
	if c.queueBytes <= 0 {
		c.queueBytes = DefaultQueueBytes
	}
	if c.stall <= 0 {
		c.stall = DefaultStall
	}
	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.chunk.Store(DefaultChunk)
	c.recv = newTileReceiver(c.clk, &c.st, c.deliverTile, c.requestMissing, c.repairDelay)
	return c
}

// repairDelay is how long the newest picture must be quiet before the client asks for the tiles that did not arrive: a little
// more than a round trip, which the QUIC connection measures.
func (c *core) repairDelay() time.Duration {
	if c.repairAfter > 0 {
		return c.repairAfter
	}
	c.mu.Lock()
	l := c.cur
	c.mu.Unlock()
	if l != nil && l.q != nil {
		if rtt := l.q.ConnectionStats().SmoothedRTT; rtt > 0 {
			return max(40*time.Millisecond, rtt*3/2+20*time.Millisecond)
		}
	}
	return 100 * time.Millisecond
}

// Kind implements Conn.
func (c *core) Kind() Kind {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cur == nil {
		return KindNone
	}
	return c.cur.kind
}

// Done implements Conn.
func (c *core) Done() <-chan struct{} { return c.done }

// Err implements Conn.
func (c *core) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Stats implements Conn.
func (c *core) Stats() Stats { return c.st.snapshot() }

// Close implements Conn: it asks the owner to end the session, which calls back shutdown.
func (c *core) Close(reason protocol.Reason, text string) error {
	c.end(reason, text)
	return nil
}

// LinkError is the error that took down the link the connection had last, nil if none did; for tests and logs.
func (c *core) lastLinkError() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.linkErr
}

// Send implements Conn.
func (c *core) Send(m protocol.Message) error {
	switch m.(type) {
	case *protocol.Tile, *protocol.Hello, *protocol.Missing:
		return errors.New("TransportMessageInvalid", "this message is not sent through Send: tiles go through SendTiles, and the transport makes Hello and Missing itself", "type", m.Type().String())
	}
	b, err := protocol.Append(nil, m)
	if err != nil {
		return err
	}
	return c.enqueue(b, m.Type())
}

// enqueue puts an encoded message at the end of the queue of the reliable path. It fails with TransportClosed and
// TransportQueueFull.
func (c *core) enqueue(b []byte, t protocol.Type) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finished {
		return errors.New("TransportClosed", "the connection is closed", "type", t.String())
	}
	if len(c.queue) >= c.queueMessages || c.queueSize+len(b) > c.queueBytes {
		return errors.New("TransportQueueFull", "the peer is not reading fast enough: too many messages wait to be sent", "type", t.String(), "waiting", len(c.queue), "bytes", c.queueSize)
	}
	c.queue = append(c.queue, b)
	c.queueSize += len(b)
	wake(c.relWake)
	return nil
}

// requestMissing sends the Missing the receiver made; a full queue drops it, and the receiver asks again.
func (c *core) requestMissing(m *protocol.Missing) {
	b, err := protocol.Append(nil, m)
	if err != nil {
		return
	}
	if err := c.enqueue(b, m.Type()); err != nil {
		c.log.Debug("Remote Work: a request for lost tiles was not queued", "err", err)
	}
}

// SendTiles implements Conn.
func (c *core) SendTiles(frame uint64, tiles []*protocol.Tile) error {
	if frame == 0 || len(tiles) == 0 || len(tiles) > protocol.MaxFrameTiles {
		return errors.New("TransportTilesInvalid", "a picture has between 1 and the limit of tiles, and a number from 1", "frame", frame, "tiles", len(tiles))
	}
	for _, t := range tiles {
		if t == nil || t.Frame != frame || int(t.Total) != len(tiles) {
			return errors.New("TransportTilesInvalid", "a tile is not of the picture it is sent with", "frame", frame, "tiles", len(tiles))
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finished {
		return errors.New("TransportClosed", "the connection is closed", "frame", frame)
	}
	if old := c.batch; old != nil && old.next < len(old.tiles) {
		c.st.batchesDropped.Add(1)
	}
	c.batch = &batch{frame: frame, tiles: tiles, queued: make([]bool, len(tiles))}
	wake(c.tilWake)
	return nil
}

// Next implements Conn.
func (c *core) Next(ctx context.Context) (protocol.Message, error) {
	for {
		select {
		case m := <-c.in:
			return m, nil
		default:
		}
		if t, ok := c.tiles.pop(); ok {
			return t, nil
		}
		select {
		case m := <-c.in:
			return m, nil
		case <-c.tiles.ready:
		case <-c.done:
			select {
			case m := <-c.in:
				return m, nil
			default:
			}
			if t, ok := c.tiles.pop(); ok {
				return t, nil
			}
			c.mu.Lock()
			peer, given := c.peer, c.peerGiven
			c.peerGiven = true
			why := ""
			if c.err != nil {
				why = c.err.Error()
			}
			c.mu.Unlock()
			if peer != nil && !given {
				return peer, nil
			}
			return nil, errors.New("TransportClosed", "the connection is closed", "why", why)
		case <-ctx.Done():
			return nil, errors.NewErr("TransportCancelled", ctx.Err())
		}
	}
}

func wake(ch chan struct{}) {
	select {
	case ch <- struct{}{}:
	default:
	}
}

// deliver hands a message to Next, waiting for room; it gives up when the connection or the link ends.
func (c *core) deliver(m protocol.Message, linkDone <-chan struct{}) bool {
	select {
	case c.in <- m:
		return true
	case <-c.done:
		return false
	case <-linkDone:
		return false
	}
}

// deliverTile hands a tile that completed to Next without waiting: a tile Next cannot take (the buffer of tiles is full) is lost,
// and the client asks for it.
func (c *core) deliverTile(t *protocol.Tile) bool { return c.tiles.push(t) }

// bind makes l the current link and starts its goroutines. The link that was current is closed. It reports whether the link
// was taken (false when the connection already ended).
func (c *core) bind(l *link) bool {
	c.mu.Lock()
	if c.finished {
		c.mu.Unlock()
		l.close(codeNormal, "the session is over")
		return false
	}
	old := c.cur
	c.cur = l
	resumed := c.hadLink
	c.hadLink = true
	if resumed && c.role == roleClient {
		// whatever was on the way is lost: ask for the whole picture again, before anything queued meanwhile goes out on the
		// new link (the writers start below)
		if b, err := protocol.Append(nil, &protocol.Refresh{}); err == nil {
			c.queue = append([][]byte{b}, c.queue...)
			c.queueSize += len(b)
		}
	}
	first := c.firstLink
	c.mu.Unlock()
	if old != nil {
		old.close(codeReplaced, "another connection took the session")
	}
	select {
	case <-first:
	default:
		close(first)
	}
	c.goroutines.Add(2)
	go c.relLoop(l)
	go c.tileLoop(l)
	if l.q != nil {
		c.goroutines.Add(1)
		go c.readDatagrams(l)
	}
	wake(c.relWake)
	wake(c.tilWake)
	if resumed {
		c.st.resumes.Add(1)
	}
	return true
}

// readStream reads the reliable messages of a link until it fails.
func (c *core) readStream(l *link, r *protocol.Reader) {
	defer c.goroutines.Done()
	for {
		m, err := r.Next()
		if err != nil {
			c.linkFailed(l, err)
			return
		}
		c.onReliable(l, m)
		select {
		case <-l.done:
			return
		default:
		}
	}
}

// readDatagrams feeds the datagrams of a QUIC link to the tile receiver.
func (c *core) readDatagrams(l *link) {
	defer c.goroutines.Done()
	ctx, cancel := context.WithCancel(c.ctx)
	defer cancel()
	go func() {
		select {
		case <-l.done:
			cancel()
		case <-ctx.Done():
		}
	}()
	for {
		b, err := l.q.ReceiveDatagram(ctx)
		if err != nil {
			return
		}
		select {
		case <-l.done:
			return
		default:
		}
		c.recv.onDatagram(b)
	}
}

// onReliable handles one message that arrived on the reliable path of a link.
func (c *core) onReliable(l *link, m protocol.Message) {
	c.mu.Lock()
	current := c.cur == l
	c.mu.Unlock()
	if !current {
		return
	}
	if c.touch != nil {
		c.touch()
	}
	switch m := m.(type) {
	case *protocol.Missing:
		c.st.missingReceived.Add(1)
		c.requestResend(m)
		return
	case *protocol.Disconnect:
		c.peerDisconnected(m)
		return
	case *protocol.Hello:
		c.violation("a Hello after the first one")
		return
	case *protocol.Tile:
		c.st.tilesReceived.Add(1)
	}
	c.deliver(m, l.done)
}

// violation ends the session: the peer sent something the protocol does not allow.
func (c *core) violation(what string) {
	c.log.Warn("Remote Work: the peer broke the protocol", "role", c.role.String(), "what", what)
	c.end(protocol.ReasonProtocol, what)
}

// requestResend queues the tiles of the newest picture a client lacks to be sent again.
func (c *core) requestResend(m *protocol.Missing) {
	c.mu.Lock()
	defer c.mu.Unlock()
	b := c.batch
	if b == nil || b.frame != m.Frame || int(m.Total) != len(b.tiles) {
		return
	}
	for _, i := range m.Indexes() {
		if i < b.next && !b.queued[i] {
			b.queued[i] = true
			b.resend = append(b.resend, i)
		}
	}
	wake(c.tilWake)
}

// popQueue takes the next reliable message for the link.
func (c *core) popQueue(l *link) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cur != l || len(c.queue) == 0 {
		return nil, false
	}
	b := c.queue[0]
	c.queue[0] = nil
	c.queue = c.queue[1:]
	c.queueSize -= len(b)
	return b, true
}

// nextTile picks the next tile of the newest picture for the link: first the ones not sent yet, then the ones a client asked for.
func (c *core) nextTile(l *link) (b *batch, index int, again, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cur != l || c.batch == nil {
		return nil, 0, false, false
	}
	b = c.batch
	switch {
	case b.next < len(b.tiles):
		index = b.next
		b.next++
		return b, index, false, true
	case len(b.resend) > 0:
		index = b.resend[0]
		b.resend = b.resend[1:]
		b.queued[index] = false
		return b, index, true, true
	}
	return nil, 0, false, false
}

// write writes one whole message to the link, giving the link up when the write stalls.
func (c *core) write(l *link, b []byte) error {
	l.wmu.Lock()
	defer l.wmu.Unlock()
	stop := c.clk.AfterFunc(c.stall, l.abort)
	_, err := l.w.Write(b)
	stop()
	if err != nil {
		return errors.NewErr("TransportLinkFailed", err).SetMetadata("op", "write", "kind", l.kind.String())
	}
	return nil
}

// relLoop writes the reliable messages of the queue to the link.
func (c *core) relLoop(l *link) {
	defer c.goroutines.Done()
	for {
		select {
		case <-l.done:
			return
		case <-c.done:
			return
		default:
		}
		b, ok := c.popQueue(l)
		if !ok {
			select {
			case <-c.relWake:
				continue
			case <-l.done:
				return
			case <-c.done:
				return
			}
		}
		if err := c.write(l, b); err != nil {
			c.linkFailed(l, err)
			return
		}
	}
}

// tileLoop sends the tiles of the newest picture on the link.
func (c *core) tileLoop(l *link) {
	defer c.goroutines.Done()
	for {
		select {
		case <-l.done:
			return
		case <-c.done:
			return
		default:
		}
		b, i, again, ok := c.nextTile(l)
		if !ok {
			select {
			case <-c.tilWake:
				continue
			case <-l.done:
				return
			case <-c.done:
				return
			}
		}
		if err := c.sendTile(l, b.tiles[i], i, len(b.tiles)); err != nil {
			c.linkFailed(l, err)
			return
		}
		if again {
			c.st.tilesResent.Add(1)
			continue
		}
		c.st.tilesSent.Add(1)
		c.mu.Lock()
		out := c.batch == b && b.next == len(b.tiles) && !b.out
		if out {
			b.out = true
		}
		c.mu.Unlock()
		if f := c.onSent.Load(); out && f != nil {
			(*f)(b.frame)
		}
	}
}

// sendTile sends one tile: as datagrams on QUIC, as a message on SSH.
func (c *core) sendTile(l *link, t *protocol.Tile, index, total int) error {
	enc, err := protocol.Append(nil, t)
	if err != nil {
		c.log.Warn("Remote Work: a tile could not be encoded", "err", err)
		return nil
	}
	if l.q == nil {
		return c.write(l, enc)
	}
	for attempt := 0; attempt < 3; attempt++ {
		parts, err := splitTile(t.Frame, uint16(index), uint16(total), enc, int(c.chunk.Load()))
		if err != nil {
			c.log.Warn("Remote Work: a tile could not be cut into datagrams", "err", err)
			return nil
		}
		retry := false
		for _, d := range parts {
			err := l.q.SendDatagram(d)
			if err == nil {
				c.st.datagramsSent.Add(1)
				continue
			}
			var tooLarge *quic.DatagramTooLargeError
			if errors.As(err, &tooLarge) {
				// the path takes smaller datagrams than we thought: cut the tile finer and send it again
				c.chunk.Store(int32(max(MinChunk, int(tooLarge.MaxDatagramPayloadSize)-datagramHeader)))
				retry = true
				break
			}
			return errors.NewErr("TransportLinkFailed", err).SetMetadata("op", "datagram", "kind", l.kind.String())
		}
		if !retry {
			return nil
		}
	}
	return nil
}

// linkFailed handles a link that broke. The connection goes on: a client resumes a QUIC connection, a host waits for it.
func (c *core) linkFailed(l *link, err error) {
	c.mu.Lock()
	wasCurrent := c.cur == l
	if wasCurrent {
		c.cur = nil
		c.linkErr = err
	}
	finished := c.finished
	c.mu.Unlock()
	l.close(codeNormal, "")
	if finished || !wasCurrent {
		return
	}
	var ae *quic.ApplicationError
	if errors.As(err, &ae) && ae.Remote && ae.ErrorCode >= codeDisconnect && ae.ErrorCode < codeDisconnect+0x100 {
		d := &protocol.Disconnect{Reason: protocol.Reason(ae.ErrorCode - codeDisconnect), Text: ae.ErrorMessage}
		if _, e := protocol.Append(nil, d); e != nil {
			d.Text = ""
			if _, e := protocol.Append(nil, d); e != nil {
				d.Reason = protocol.ReasonClosed
			}
		}
		c.peerDisconnected(d)
		return
	}
	c.log.Info("Remote Work: a link went down", "role", c.role.String(), "kind", l.kind.String(), "err", err)
	if c.onLinkDown != nil {
		c.onLinkDown(l, err)
	}
}

// peerDisconnected handles the Disconnect the peer sent: it is given to Next once, then the connection ends.
func (c *core) peerDisconnected(d *protocol.Disconnect) {
	c.mu.Lock()
	if c.finished || c.peer != nil {
		c.mu.Unlock()
		return
	}
	c.peer = d
	c.mu.Unlock()
	if c.role == roleHost {
		c.end(protocol.ReasonClosed, "the client disconnected: "+d.Reason.String())
		return
	}
	c.finish(errors.New("TransportClosed", "the host ended the session", "reason", d.Reason.String(), "text", d.Text), codeNormal, "")
}

// shutdown ends the session on purpose: the peer is told why, the links close, the goroutines go. The host's Session.End calls it.
func (c *core) shutdown(reason protocol.Reason, text string) {
	if !c.closing.CompareAndSwap(false, true) {
		return
	}
	d := &protocol.Disconnect{Reason: reason, Text: text}
	b, err := protocol.Append(nil, d)
	if err != nil {
		d.Text = ""
		b, err = protocol.Append(nil, d)
		if err != nil {
			d.Reason = protocol.ReasonClosed
			b, _ = protocol.Append(nil, d)
		}
	}
	if c.ssh != nil {
		// the SSH channel always gets it: the peer may be reading only that, and it cannot be lost
		sent := make(chan struct{})
		go func() {
			defer close(sent)
			_ = c.ssh.write(b) // best effort: the peer may be gone
		}()
		t := time.NewTimer(farewellTimeout)
		select {
		case <-sent:
		case <-t.C:
		}
		t.Stop()
	}
	c.finish(errors.New("TransportClosed", "the session was closed", "reason", reason.String(), "text", text), codeDisconnect+quic.ApplicationErrorCode(d.Reason), d.Text)
}

// finish ends the connection, once.
func (c *core) finish(err error, code quic.ApplicationErrorCode, text string) {
	c.mu.Lock()
	if c.finished {
		c.mu.Unlock()
		return
	}
	c.finished = true
	c.err = err
	l := c.cur
	c.cur = nil
	c.queue, c.queueSize, c.batch = nil, 0, nil
	stops := c.timers
	c.timers = nil
	c.mu.Unlock()
	for _, stop := range stops {
		stop()
	}
	close(c.done)
	c.cancel()
	c.recv.close()
	if l != nil {
		l.close(code, text)
	}
	if c.onFinish != nil {
		c.onFinish()
	}
}

// addTimer remembers a timer to stop when the connection ends.
func (c *core) addTimer(stop func() bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.finished {
		stop()
		return
	}
	c.timers = append(c.timers, stop)
}

// wait waits for the goroutines of the connection to end, for tests.
func (c *core) wait() { c.goroutines.Wait() }
