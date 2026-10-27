package transport

import (
	"context"
	"image"
	"log/slog"
	"sync"
	"time"

	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
	"github.com/rm4n0s/emetos/extensions/remotework/tiles"
)

// The defaults of the picture flow (SPEC §10.18 "As built (step 15.3)").
const (
	// DefaultWindow is how many pictures may be in flight (sent and not confirmed) before the next waits for a confirmation.
	DefaultWindow = 2
	// DefaultHold is how long a full window holds the next picture back; after it the picture is sent anyway, so a confirmation
	// that was lost does not stop the view.
	DefaultHold = 400 * time.Millisecond
	// DefaultRetryAfter is how long an unconfirmed picture waits before the host sends the picture again (QUIC only: a picture
	// whose every datagram was lost leaves the client no way to know there was one).
	DefaultRetryAfter = 600 * time.Millisecond
	// DefaultRefreshEvery is the period of the full refresh (SPEC §10.18: every 5 s) while pictures are being sent over QUIC.
	DefaultRefreshEvery = 5 * time.Second
)

// PresenterOptions are what NewPresenter takes; every field has a default.
type PresenterOptions struct {
	// Clock is the time of the retries and the refresh; the machine's when nil. Logger is the log.
	Clock  Clock
	Logger *slog.Logger
	// Window, Hold, RetryAfter and RefreshEvery override the defaults above.
	Window       int
	Hold         time.Duration
	RetryAfter   time.Duration
	RefreshEvery time.Duration
}

// Presenter is the host's picture flow on a Conn. It is given the pictures of the headless view (Present), keeps only the newest,
// and sends the tiles that differ from the picture the client confirmed (package tiles) as soon as the window allows. The
// messages of the client that concern the picture (FrameConfirm, Refresh) go to Handle.
//
//   - At most Window pictures are in flight; a newer one replaces the one waiting, so the host never queues old pictures.
//   - Over QUIC a picture that is not confirmed after RetryAfter is sent again, and while pictures are being sent a whole picture
//     goes every RefreshEvery: the datagrams can be lost, and the two make up for it (the client also asks for the tiles it
//     lacks, which the Conn answers). Over the SSH channel nothing is lost, so neither is done.
type Presenter struct {
	conn Conn
	clk  Clock
	log  *slog.Logger

	window                        int
	hold, retryAfter, refreshEach time.Duration

	differ *tiles.Differ
	kick   chan struct{}
	stop   chan struct{}
	wg     sync.WaitGroup

	mu          sync.Mutex
	img         *image.RGBA
	pending     bool
	confirmed   uint64
	applied     uint64
	reset       bool
	inFlight    []uint64
	lastFrame   uint64
	sentAt      time.Time // when the last picture was all handed to the transport, or the client last asked for tiles of it; zero while it is being sent
	missingSeen uint64
	lastSend    time.Time
	lastRefresh time.Time
	activity    bool
	timerStop   func() bool
	closed      bool
}

// NewPresenter starts the picture flow of a connection. It stops when ctx ends or Close is called. It fails with the tags of
// tiles.NewDiffer.
func NewPresenter(ctx context.Context, conn Conn, o PresenterOptions) (*Presenter, error) {
	d, err := tiles.NewDiffer(tiles.Level)
	if err != nil {
		return nil, err
	}
	p := &Presenter{
		conn: conn, clk: o.Clock, log: o.Logger, window: o.Window, hold: o.Hold, retryAfter: o.RetryAfter, refreshEach: o.RefreshEvery,
		differ: d, kick: make(chan struct{}, 1), stop: make(chan struct{}),
	}
	if p.clk == nil {
		p.clk = RealClock{}
	}
	if p.log == nil {
		p.log = slog.Default()
	}
	if p.window <= 0 {
		p.window = DefaultWindow
	}
	if p.hold <= 0 {
		p.hold = DefaultHold
	}
	if p.retryAfter <= 0 {
		p.retryAfter = DefaultRetryAfter
	}
	if p.refreshEach <= 0 {
		p.refreshEach = DefaultRefreshEvery
	}
	conn.OnTilesSent(p.sent)
	p.wg.Add(1)
	go p.run(ctx)
	return p, nil
}

// Close stops the flow and waits for its goroutine.
func (p *Presenter) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		p.wg.Wait()
		return
	}
	p.closed = true
	if p.timerStop != nil {
		p.timerStop()
	}
	p.mu.Unlock()
	close(p.stop)
	p.wg.Wait()
}

// Present gives the flow the newest picture of the view. A picture still waiting is replaced. The picture must not be changed
// after the call (the headless view hands over a new one for each frame).
func (p *Presenter) Present(img *image.RGBA) {
	if img == nil {
		return
	}
	p.mu.Lock()
	p.img, p.pending = img, true
	p.mu.Unlock()
	p.wake()
}

// Handle takes the messages of the client that are about the picture: FrameConfirm (the client has the whole picture) and
// Refresh (it lost its picture and wants a whole one). It reports whether the message was one of them.
func (p *Presenter) Handle(m protocol.Message) bool {
	switch m := m.(type) {
	case *protocol.FrameConfirm:
		p.mu.Lock()
		if m.Frame > p.confirmed {
			p.confirmed = m.Frame
		}
		p.mu.Unlock()
		p.wake()
		return true
	case *protocol.Refresh:
		p.mu.Lock()
		p.reset = true
		p.mu.Unlock()
		p.wake()
		return true
	}
	return false
}

// sent is called by the connection when the tiles of a picture are all out.
func (p *Presenter) sent(frame uint64) {
	p.mu.Lock()
	if frame == p.lastFrame {
		p.sentAt = p.clk.Now()
	}
	p.mu.Unlock()
	p.wake()
}

func (p *Presenter) wake() {
	select {
	case p.kick <- struct{}{}:
	default:
	}
}

func (p *Presenter) run(ctx context.Context) {
	defer p.wg.Done()
	for {
		select {
		case <-p.kick:
		case <-p.stop:
			return
		case <-ctx.Done():
			return
		case <-p.conn.Done():
			return
		}
		p.step()
	}
}

// step does what is due: sends the picture, the retry or the refresh, and sets the timer for the next thing that will be.
func (p *Presenter) step() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	if p.reset {
		p.differ.Reset()
		p.inFlight, p.reset = nil, false
		p.pending = p.img != nil
	}
	if p.confirmed > p.applied {
		p.differ.Confirm(p.confirmed)
		p.applied = p.confirmed
		kept := p.inFlight[:0]
		for _, f := range p.inFlight {
			if f > p.confirmed {
				kept = append(kept, f)
			}
		}
		p.inFlight = kept
	}
	if p.img == nil {
		p.mu.Unlock()
		return
	}
	now := p.clk.Now()
	quic := p.conn.Kind() == KindQUIC
	// A client that asks for the tiles it lacks is receiving the picture: the retry waits for the repair instead of replacing
	// the picture the client is putting together.
	if seen := p.conn.Stats().MissingReceived; seen != p.missingSeen {
		p.missingSeen = seen
		if !p.sentAt.IsZero() {
			p.sentAt = now
		}
	}
	var next time.Duration // when to look again, 0 for never
	soonest := func(d time.Duration) {
		if d < time.Millisecond {
			d = time.Millisecond
		}
		if next == 0 || d < next {
			next = d
		}
	}
	send, refresh := false, false
	switch {
	case p.pending:
		if len(p.inFlight) >= p.window && now.Sub(p.lastSend) < p.hold {
			soonest(p.hold - now.Sub(p.lastSend))
		} else {
			send = true
		}
	case len(p.inFlight) > 0 && quic && !p.sentAt.IsZero():
		// the picture is out and nothing came back: the datagrams may all have been lost
		if since := now.Sub(p.sentAt); since >= p.retryAfter {
			send = true
		} else {
			soonest(p.retryAfter - since)
		}
	}
	// a picture still being sent is not sent over again by a refresh
	if !send && quic && p.activity && (p.lastFrame == 0 || !p.sentAt.IsZero()) {
		if since := now.Sub(p.lastRefresh); since >= p.refreshEach {
			send, refresh = true, true
		} else {
			soonest(p.refreshEach - since)
		}
	}
	img := p.img
	if send {
		p.pending = false
		if refresh {
			p.differ.Refresh()
			p.lastRefresh, p.activity = now, false
		}
	}
	p.arm(next)
	p.mu.Unlock()
	if !send {
		return
	}
	frame, tl, err := p.differ.Next(img)
	if err != nil {
		p.log.Warn("Remote Work: a picture could not be cut into tiles", "err", err)
		return
	}
	if frame == 0 {
		return
	}
	p.mu.Lock()
	p.lastFrame, p.sentAt = frame, time.Time{}
	p.mu.Unlock()
	if err := p.conn.SendTiles(frame, tl); err != nil {
		p.log.Debug("Remote Work: the tiles were not sent", "err", err)
		return
	}
	p.mu.Lock()
	p.inFlight = append(p.inFlight, frame)
	p.lastFrame = frame
	if len(p.inFlight) > tiles.MaxPending {
		p.inFlight = p.inFlight[len(p.inFlight)-tiles.MaxPending:]
	}
	p.lastSend = now
	if !refresh {
		if !p.activity {
			p.lastRefresh = now // the period of the refresh counts from the first picture after the last one
		}
		p.activity = true
	}
	p.mu.Unlock()
	// the window or the retry may be due again
	p.wake()
}

// arm sets the one timer to wake the flow after d; 0 clears it. The caller holds mu.
func (p *Presenter) arm(d time.Duration) {
	if p.timerStop != nil {
		p.timerStop()
		p.timerStop = nil
	}
	if d > 0 {
		p.timerStop = p.clk.AfterFunc(d, p.wake)
	}
}

// Screen is the client's picture flow on a Conn: the canvas of package tiles, fed with the tiles Next gives, which confirms every
// picture it completes and asks the host for a whole one when it gets lost. The tab (step 15.4) draws what Draw gives it.
type Screen struct {
	conn     Conn
	log      *slog.Logger
	onChange func()

	mu     sync.Mutex
	canvas *tiles.Canvas
}

// staleRunLimit is how many tiles in a row the canvas may drop for their base before the client asks for a whole picture.
const staleRunLimit = 16

// NewScreen makes the client's picture flow. onChange, if not nil, is called after tiles were put on the picture, from the
// goroutine that calls Handle (the tab invalidates its widget there).
func NewScreen(conn Conn, log *slog.Logger, onChange func()) *Screen {
	if log == nil {
		log = slog.Default()
	}
	return &Screen{conn: conn, log: log, onChange: onChange, canvas: tiles.NewCanvas()}
}

// Handle takes a message from Next. It puts a Tile on the picture and returns true; any other message is not the screen's and
// gives false.
func (s *Screen) Handle(m protocol.Message) bool {
	t, ok := m.(*protocol.Tile)
	if !ok {
		return false
	}
	s.mu.Lock()
	res, err := s.canvas.Apply(t)
	confirmed, stale := s.canvas.Confirmed(), s.canvas.StaleRun()
	lost := err != nil || stale >= staleRunLimit
	if lost {
		s.canvas.Reset()
	}
	s.mu.Unlock()
	switch {
	case lost:
		if err != nil {
			s.log.Warn("Remote Work: a tile contradicts the picture; asking for a whole one", "err", err)
		}
		if err := s.conn.Send(&protocol.Refresh{}); err != nil {
			s.log.Debug("Remote Work: the request for a whole picture was not queued", "err", err)
		}
	case res == tiles.Complete:
		if err := s.conn.Send(&protocol.FrameConfirm{Frame: confirmed}); err != nil {
			s.log.Debug("Remote Work: a confirmation was not queued", "err", err)
		}
	}
	if (res == tiles.Applied || res == tiles.Complete) && s.onChange != nil {
		s.onChange()
	}
	return true
}

// Draw calls f with the picture as it is now, under the lock that keeps the screen from changing it; the picture is nil before the
// first tile. f must not keep the picture.
func (s *Screen) Draw(f func(img *image.RGBA)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f(s.canvas.Image())
}

// Confirmed is the number of the last complete picture, 0 for none.
func (s *Screen) Confirmed() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.canvas.Confirmed()
}
