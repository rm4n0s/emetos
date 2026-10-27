package transport

import (
	"bytes"
	"context"
	"image"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
	"github.com/rm4n0s/emetos/extensions/remotework/tiles"
)

// fakeConn records what the picture flow sends.
type fakeConn struct {
	mu      sync.Mutex
	kind    Kind
	batches []sentBatch
	sent    []protocol.Message
	done    chan struct{}
	onSent  func(uint64)
	slow    atomic.Bool
}

type sentBatch struct {
	frame uint64
	tiles []*protocol.Tile
}

func newFakeConn(kind Kind) *fakeConn { return &fakeConn{kind: kind, done: make(chan struct{})} }

func (f *fakeConn) Kind() Kind { f.mu.Lock(); defer f.mu.Unlock(); return f.kind }
func (f *fakeConn) Send(m protocol.Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, m)
	return nil
}
func (f *fakeConn) SendTiles(frame uint64, t []*protocol.Tile) error {
	f.mu.Lock()
	f.batches = append(f.batches, sentBatch{frame, t})
	cb := f.onSent
	f.mu.Unlock()
	if cb != nil && !f.slow.Load() {
		cb(frame)
	}
	return nil
}
func (f *fakeConn) OnTilesSent(fn func(uint64)) { f.mu.Lock(); f.onSent = fn; f.mu.Unlock() }
func (f *fakeConn) finishSending(frame uint64) {
	f.mu.Lock()
	cb := f.onSent
	f.mu.Unlock()
	cb(frame)
}
func (f *fakeConn) Next(ctx context.Context) (protocol.Message, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (f *fakeConn) Done() <-chan struct{}               { return f.done }
func (f *fakeConn) Err() error                          { return nil }
func (f *fakeConn) Close(protocol.Reason, string) error { return nil }
func (f *fakeConn) Stats() Stats                        { return Stats{} }
func (f *fakeConn) count() int                          { f.mu.Lock(); defer f.mu.Unlock(); return len(f.batches) }
func (f *fakeConn) last() sentBatch {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.batches[len(f.batches)-1]
}
func (f *fakeConn) messages() []protocol.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]protocol.Message(nil), f.sent...)
}

var _ Conn = (*fakeConn)(nil)

func newTestPresenter(t *testing.T, kind Kind) (*Presenter, *fakeConn, *fakeClock) {
	t.Helper()
	noLeaks(t)
	conn, clk := newFakeConn(kind), newClock()
	p, err := NewPresenter(context.Background(), conn, PresenterOptions{Clock: clk, Logger: discardLog()})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p, conn, clk
}

func waitCount(t *testing.T, c *fakeConn, n int) {
	t.Helper()
	eventually(t, "the tiles to be sent", func() bool { return c.count() >= n })
	// nothing more than n
	time.Sleep(20 * time.Millisecond)
	if got := c.count(); got != n {
		t.Fatalf("%d pictures sent, want %d", got, n)
	}
}

func TestThePresenterSendsAWholePictureAndThenOnlyWhatChanged(t *testing.T) {
	p, conn, _ := newTestPresenter(t, KindQUIC)
	editor, typed := loadPicture(t, "editor"), loadPicture(t, "typed")
	p.Present(editor)
	waitCount(t, conn, 1)
	if b := conn.last(); b.frame != 1 || len(b.tiles) != 260 {
		t.Fatalf("first picture: frame %d, %d tiles", b.frame, len(b.tiles))
	}
	// the client confirms it; the next picture is the changes to it
	p.Handle(&protocol.FrameConfirm{Frame: 1})
	p.Present(typed)
	waitCount(t, conn, 2)
	if b := conn.last(); b.frame != 2 || len(b.tiles) > 20 || b.tiles[0].Base != 1 {
		t.Fatalf("second picture: frame %d, %d tiles, base %d", b.frame, len(b.tiles), b.tiles[0].Base)
	}
	// the same picture again sends nothing new
	p.Handle(&protocol.FrameConfirm{Frame: 2})
	p.Present(typed)
	time.Sleep(50 * time.Millisecond)
	if conn.count() != 2 {
		t.Errorf("a picture that did not change sent %d more", conn.count()-2)
	}
}

func TestThePresenterKeepsAWindowOfPicturesInFlightAndOnlyTheNewestWaits(t *testing.T) {
	p, conn, clk := newTestPresenter(t, KindQUIC)
	pics := []*image.RGBA{loadPicture(t, "editor"), loadPicture(t, "typed"), loadPicture(t, "scrolled"), withID(loadPicture(t, "editor"), 7)}
	p.Present(pics[0])
	waitCount(t, conn, 1)
	p.Present(pics[1])
	waitCount(t, conn, 2)
	// two are in flight and none is confirmed: the next ones wait, and the older of them is dropped
	p.Present(pics[2])
	p.Present(pics[3])
	time.Sleep(50 * time.Millisecond)
	if conn.count() != 2 {
		t.Fatalf("a third picture was sent with the window full")
	}
	clk.Advance(DefaultHold - time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	if conn.count() != 2 {
		t.Fatal("the hold passed too early")
	}
	clk.Advance(time.Millisecond)
	waitCount(t, conn, 3)
	// it is the last picture presented, not the one before it
	frame, got := conn.last().frame, conn.last().tiles
	if frame != 3 {
		t.Errorf("frame %d", frame)
	}
	c := tiles.NewCanvas()
	for _, tl := range got {
		if _, err := c.Apply(tl); err != nil {
			t.Fatal(err)
		}
	}
	if idOf(c.Image()) != 7 {
		t.Errorf("the picture sent is not the newest (id %d)", idOf(c.Image()))
	}
}

func TestAConfirmationOpensTheWindow(t *testing.T) {
	p, conn, _ := newTestPresenter(t, KindQUIC)
	p.Present(loadPicture(t, "editor"))
	waitCount(t, conn, 1)
	p.Present(loadPicture(t, "typed"))
	waitCount(t, conn, 2)
	p.Present(loadPicture(t, "scrolled"))
	time.Sleep(30 * time.Millisecond)
	if conn.count() != 2 {
		t.Fatal("the window was not full")
	}
	p.Handle(&protocol.FrameConfirm{Frame: 1})
	waitCount(t, conn, 3)
}

func TestThePresenterSendsAnUnconfirmedPictureAgainOverQUICOnly(t *testing.T) {
	for _, tt := range []struct {
		name  string
		kind  Kind
		again bool
	}{{"QUIC", KindQUIC, true}, {"SSH", KindSSH, false}} {
		t.Run(tt.name, func(t *testing.T) {
			p, conn, clk := newTestPresenter(t, tt.kind)
			p.Present(loadPicture(t, "editor"))
			waitCount(t, conn, 1)
			clk.Advance(DefaultRetryAfter - time.Millisecond)
			time.Sleep(30 * time.Millisecond)
			if conn.count() != 1 {
				t.Fatal("sent again too early")
			}
			clk.Advance(time.Millisecond)
			if !tt.again {
				time.Sleep(50 * time.Millisecond)
				if conn.count() != 1 {
					t.Errorf("the picture was sent again over SSH")
				}
				return
			}
			waitCount(t, conn, 2)
			if b := conn.last(); b.frame != 2 || len(b.tiles) != 260 {
				t.Errorf("the retry: frame %d, %d tiles", b.frame, len(b.tiles))
			}
			// a confirmation ends the retries
			p.Handle(&protocol.FrameConfirm{Frame: 2})
			clk.Advance(time.Minute)
			// ... but not the refresh, which is a whole picture
			waitCount(t, conn, 3)
			if len(conn.last().tiles) != 260 {
				t.Errorf("the refresh has %d tiles", len(conn.last().tiles))
			}
		})
	}
}

func TestTheWholePictureIsRefreshedEveryFiveSecondsWhilePicturesAreSent(t *testing.T) {
	p, conn, clk := newTestPresenter(t, KindQUIC)
	p.Present(loadPicture(t, "editor"))
	waitCount(t, conn, 1)
	p.Handle(&protocol.FrameConfirm{Frame: 1})
	clk.Advance(DefaultRefreshEvery - time.Second)
	time.Sleep(30 * time.Millisecond)
	if conn.count() != 1 {
		t.Fatal("refreshed too early")
	}
	clk.Advance(time.Second)
	waitCount(t, conn, 2)
	if b := conn.last(); len(b.tiles) != 260 || b.tiles[0].Base != 0 {
		t.Errorf("the refresh: %d tiles with base %d", len(b.tiles), b.tiles[0].Base)
	}
	// nothing changed since: no more refreshes while the view is idle
	p.Handle(&protocol.FrameConfirm{Frame: 2})
	clk.Advance(time.Minute)
	time.Sleep(50 * time.Millisecond)
	if conn.count() != 2 {
		t.Errorf("an idle view was refreshed %d more times", conn.count()-2)
	}
}

func TestNoRefreshOverSSH(t *testing.T) {
	p, conn, clk := newTestPresenter(t, KindSSH)
	p.Present(loadPicture(t, "editor"))
	waitCount(t, conn, 1)
	p.Handle(&protocol.FrameConfirm{Frame: 1})
	clk.Advance(time.Minute)
	time.Sleep(50 * time.Millisecond)
	if conn.count() != 1 {
		t.Errorf("%d pictures over SSH", conn.count())
	}
}

func TestARefreshRequestMakesTheNextPictureWhole(t *testing.T) {
	p, conn, _ := newTestPresenter(t, KindQUIC)
	p.Present(loadPicture(t, "editor"))
	waitCount(t, conn, 1)
	p.Handle(&protocol.FrameConfirm{Frame: 1})
	if !p.Handle(&protocol.Refresh{}) {
		t.Fatal("Refresh was not handled")
	}
	waitCount(t, conn, 2)
	if b := conn.last(); len(b.tiles) != 260 || b.tiles[0].Base != 0 {
		t.Errorf("after Refresh: %d tiles with base %d", len(b.tiles), b.tiles[0].Base)
	}
	if p.Handle(&protocol.Ping{}) || p.Handle(&protocol.Key{}) {
		t.Error("a message that is not about the picture was handled")
	}
}

func TestThePresenterIgnoresNothingAndStopsWithTheConnection(t *testing.T) {
	noLeaks(t)
	conn := newFakeConn(KindQUIC)
	p, err := NewPresenter(context.Background(), conn, PresenterOptions{Clock: newClock(), Logger: discardLog()})
	if err != nil {
		t.Fatal(err)
	}
	p.Present(nil)
	close(conn.done)
	p.Close()
	p.Close() // the second close does nothing
	p.Present(loadPicture(t, "editor"))
}

// tilesOf makes the tiles of a picture with a real differ.
func tilesOf(t *testing.T, img *image.RGBA) (uint64, []*protocol.Tile) {
	t.Helper()
	d, err := tiles.NewDiffer(tiles.Level)
	if err != nil {
		t.Fatal(err)
	}
	frame, tl, err := d.Next(img)
	if err != nil {
		t.Fatal(err)
	}
	return frame, tl
}

func TestTheScreenShowsTheTilesAndConfirmsTheCompletePicture(t *testing.T) {
	conn := newFakeConn(KindQUIC)
	changes := 0
	s := NewScreen(conn, discardLog(), func() { changes++ })
	editor := loadPicture(t, "editor")
	frame, tl := tilesOf(t, editor)
	for i, tile := range tl {
		if !s.Handle(tile) {
			t.Fatal("a tile was not handled")
		}
		if i < len(tl)-1 && len(conn.messages()) != 0 {
			t.Fatalf("confirmed after %d of %d tiles", i+1, len(tl))
		}
	}
	msgs := conn.messages()
	if len(msgs) != 1 || msgs[0].(*protocol.FrameConfirm).Frame != frame {
		t.Fatalf("messages %+v", msgs)
	}
	if changes != len(tl) || s.Confirmed() != frame {
		t.Errorf("%d changes, confirmed %d", changes, s.Confirmed())
	}
	same := false
	s.Draw(func(img *image.RGBA) { same = bytes.Equal(img.Pix, editor.Pix) })
	if !same {
		t.Error("the picture differs")
	}
	if s.Handle(&protocol.Ping{}) {
		t.Error("a ping was handled")
	}
}

func TestTheScreenAsksForAWholePictureWhenItsPictureIsLost(t *testing.T) {
	_, tl := tilesOf(t, loadPicture(t, "editor"))
	t.Run("a tile that contradicts its frame", func(t *testing.T) {
		conn := newFakeConn(KindQUIC)
		s := NewScreen(conn, discardLog(), nil)
		s.Handle(tl[0])
		bad := *tl[1]
		bad.Total = tl[1].Total - 1
		s.Handle(&bad)
		msgs := conn.messages()
		if len(msgs) != 1 {
			t.Fatalf("messages %+v", msgs)
		}
		if _, ok := msgs[0].(*protocol.Refresh); !ok {
			t.Errorf("sent %T", msgs[0])
		}
		if s.Confirmed() != 0 {
			t.Error("the picture was kept")
		}
	})
	t.Run("tiles that all build on a picture the client lacks", func(t *testing.T) {
		conn := newFakeConn(KindQUIC)
		s := NewScreen(conn, discardLog(), nil)
		s.Handle(tl[0]) // a picture exists, but not frame 9
		for i := range staleRunLimit {
			x := *tl[i%len(tl)]
			x.Frame, x.Base = 10, 9
			s.Handle(&x)
		}
		var refresh int
		for _, m := range conn.messages() {
			if _, ok := m.(*protocol.Refresh); ok {
				refresh++
			}
		}
		if refresh != 1 {
			t.Errorf("%d Refresh messages, want 1", refresh)
		}
	})
}

func TestAPictureStillBeingSentIsNotSentAgain(t *testing.T) {
	p, conn, clk := newTestPresenter(t, KindQUIC)
	conn.slow.Store(true) // the transport takes its time: the picture is not out
	p.Present(loadPicture(t, "editor"))
	waitCount(t, conn, 1)
	clk.Advance(4 * time.Second) // well past the retry, short of the refresh
	time.Sleep(50 * time.Millisecond)
	if conn.count() != 1 {
		t.Fatalf("a picture that is still being sent was sent again (%d)", conn.count())
	}
	// when it is out, the retry's time counts from then
	conn.finishSending(1)
	time.Sleep(30 * time.Millisecond)
	clk.Advance(DefaultRetryAfter - time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	if conn.count() != 1 {
		t.Fatal("retried too early")
	}
	clk.Advance(time.Millisecond)
	waitCount(t, conn, 2)
}
