package transport

import (
	"bytes"
	"context"
	"image"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
)

// observer records, on the client, when each numbered picture was first seen (any tile of it on the canvas) and when it was
// complete (the canvas is exactly that picture and the client confirmed it).
type observer struct {
	t         *testing.T
	screen    *Screen
	mu        sync.Mutex
	targets   map[int]*image.RGBA
	presented map[int]time.Time
	seen      map[int]time.Time
	complete  map[int]time.Time
	lastConf  uint64
}

func (o *observer) present(p *Presenter, id int, img *image.RGBA) {
	pic := withID(img, id)
	o.mu.Lock()
	o.targets[id] = pic
	o.presented[id] = time.Now()
	o.mu.Unlock()
	p.Present(pic)
}

func (o *observer) onChange() {
	now := time.Now()
	id := -1
	o.screen.Draw(func(img *image.RGBA) { id = idOf(img) })
	if id < 0 {
		return
	}
	o.mu.Lock()
	if _, ok := o.seen[id]; !ok {
		o.seen[id] = now
	}
	o.mu.Unlock()
}

// handled is called after the screen took a tile: a new confirmed picture is checked against the one it should be.
func (o *observer) handled() {
	conf := o.screen.Confirmed()
	o.mu.Lock()
	changed := conf != o.lastConf
	o.lastConf = conf
	o.mu.Unlock()
	if !changed {
		return
	}
	now := time.Now()
	o.screen.Draw(func(img *image.RGBA) {
		id := idOf(img)
		o.mu.Lock()
		defer o.mu.Unlock()
		if want := o.targets[id]; want != nil && bytes.Equal(want.Pix, img.Pix) {
			if _, ok := o.complete[id]; !ok {
				o.complete[id] = now
			}
		}
	})
}

func (o *observer) waitComplete(id int, limit time.Duration) (time.Duration, bool) {
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		o.mu.Lock()
		at, ok := o.complete[id]
		from := o.presented[id]
		o.mu.Unlock()
		if ok {
			return at.Sub(from), true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return 0, false
}

func percentile(d []time.Duration, p float64) time.Duration {
	if len(d) == 0 {
		return 0
	}
	s := slices.Clone(d)
	slices.Sort(s)
	return s[min(len(s)-1, int(float64(len(s))*p))]
}

// measurement is what one scenario reports.
type measurement struct {
	firstFull, scroll, scrollBack time.Duration
	offered, shown, completed     int
	shownLatency, completeLatency []time.Duration
	settle                        time.Duration
	forwarded, dropped            int64
	host, client                  Stats
}

// runScenario runs the editor's pictures over a UDP link that loses a share of its packets and delays all of them, with the
// real differ, on real-sized pictures, and measures what a user would see.
func runScenario(t *testing.T, loss float64, delay time.Duration, typing time.Duration, seed uint64) measurement {
	t.Helper()
	r := newRig(t)
	s, ch := r.session()
	proxy := newProxy(t, r.host.Addr(), loss, delay, seed)
	o := r.opts(s, ch)
	o.Address = proxy.Addr()
	o.RepairAfter = 0 // derived from the round trip the connection measures
	client := r.connect(o)
	eventually(t, "the host's link", func() bool { return s.conn.Kind() == KindQUIC })
	host := s.conn

	p, err := NewPresenter(context.Background(), host, PresenterOptions{Logger: discardLog()})
	if err != nil {
		t.Fatal(err)
	}
	obs := &observer{t: t, targets: map[int]*image.RGBA{}, presented: map[int]time.Time{}, seen: map[int]time.Time{}, complete: map[int]time.Time{}}
	obs.screen = NewScreen(client, discardLog(), obs.onChange)
	waitClient := drive(client, func(m protocol.Message) {
		if obs.screen.Handle(m) {
			obs.handled()
		}
	})
	waitHost := drive(host, func(m protocol.Message) { p.Handle(m) })
	defer func() {
		p.Close()
		s.End(protocol.ReasonHost, "measured")
		waitClient()
		waitHost()
	}()

	editor, typed, scrolled := loadPicture(t, "editor"), loadPicture(t, "typed"), loadPicture(t, "scrolled")
	var m measurement

	// 1. the first picture: all 260 tiles
	obs.present(p, 1, editor)
	d, ok := obs.waitComplete(1, 30*time.Second)
	if !ok {
		t.Fatalf("the first picture never completed; host %+v client %+v", host.Stats(), client.Stats())
	}
	m.firstFull = d

	// 2. typing: a picture every 100 ms (10 a second), one character changes (7 tiles) and the first tile carries the number
	start := time.Now()
	id := 1
	for tick := time.NewTicker(100 * time.Millisecond); time.Since(start) < typing; <-tick.C {
		id++
		base := typed
		if id%2 == 0 {
			base = editor
		}
		obs.present(p, id, base)
	}
	last := id
	m.offered = last - 1
	if d, ok := obs.waitComplete(last, 30*time.Second); ok {
		m.settle = d
	} else {
		t.Fatalf("the last typed picture never completed; host %+v client %+v", host.Stats(), client.Stats())
	}
	obs.mu.Lock()
	for k := 2; k <= last; k++ {
		if at, ok := obs.seen[k]; ok {
			m.shown++
			m.shownLatency = append(m.shownLatency, at.Sub(obs.presented[k]))
		}
		if at, ok := obs.complete[k]; ok {
			m.completed++
			m.completeLatency = append(m.completeLatency, at.Sub(obs.presented[k]))
		}
	}
	obs.mu.Unlock()

	// 3. a scroll: 132 tiles change at once, and back
	obs.present(p, 1000, scrolled)
	if d, ok = obs.waitComplete(1000, 30*time.Second); !ok {
		t.Fatalf("the scrolled picture never completed; host %+v client %+v", host.Stats(), client.Stats())
	}
	m.scroll = d
	obs.present(p, 1001, editor)
	if d, ok = obs.waitComplete(1001, 30*time.Second); !ok {
		t.Fatalf("the picture after the scroll never completed; host %+v client %+v", host.Stats(), client.Stats())
	}
	m.scrollBack = d
	m.forwarded, m.dropped = proxy.forwarded.Load(), proxy.dropped.Load()
	m.host, m.client = host.Stats(), client.Stats()
	return m
}

func (m measurement) log(t *testing.T, name string, typing time.Duration) {
	t.Helper()
	secs := typing.Seconds()
	t.Logf("%s: first full picture (260 tiles) %v; typing: %d pictures offered in %.0fs, %.1f shown/s (latency median %v, p95 %v), %.1f complete/s (median %v, p95 %v), last one complete %v after it was presented; scroll (132 tiles) %v, and back (%d tiles changed) %v; link: %d packets forwarded, %d dropped; host: %d tiles sent, %d resent, %d datagrams, %d requests for lost tiles, %d pictures dropped before sent; client: %d tiles stale, %d duplicate",
		name, m.firstFull.Round(time.Millisecond), m.offered, secs,
		float64(m.shown)/secs, percentile(m.shownLatency, 0.5).Round(time.Millisecond), percentile(m.shownLatency, 0.95).Round(time.Millisecond),
		float64(m.completed)/secs, percentile(m.completeLatency, 0.5).Round(time.Millisecond), percentile(m.completeLatency, 0.95).Round(time.Millisecond),
		m.settle.Round(time.Millisecond), m.scroll.Round(time.Millisecond), 132, m.scrollBack.Round(time.Millisecond),
		m.forwarded, m.dropped, m.host.TilesSent, m.host.TilesResent, m.host.DatagramsSent, m.client.MissingSent, m.host.BatchesDropped,
		m.client.TilesStale, m.client.TilesDuplicate)
}

// TestLossyLinkMeasurement is the measurement of SPEC §10.18 "As built (step 15.3)": the real tile differ on 1280x800 pictures of an
// editor, through a UDP proxy that delays every packet by 50 ms in each direction (a round trip of 100 ms) and drops 5% of
// them. Run it with -v to read the numbers.
func TestLossyLinkMeasurement(t *testing.T) {
	noLeaks(t)
	typing := 4 * time.Second
	for _, sc := range []struct {
		name  string
		loss  float64
		delay time.Duration
	}{
		{"no loss, no delay", 0, 0},
		{"no loss, 50 ms each way", 0, 50 * time.Millisecond},
		{"5% loss, 50 ms each way", 0.05, 50 * time.Millisecond},
	} {
		t.Run(sc.name, func(t *testing.T) {
			m := runScenario(t, sc.loss, sc.delay, typing, 20261027)
			m.log(t, sc.name, typing)
			if m.shown < 5 || m.completed < 2 {
				t.Errorf("only %d pictures shown and %d complete of %d offered", m.shown, m.completed, m.offered)
			}
			if m.firstFull > 20*time.Second || m.scroll > 20*time.Second {
				t.Errorf("a picture took %v and %v to complete", m.firstFull, m.scroll)
			}
		})
	}
}
