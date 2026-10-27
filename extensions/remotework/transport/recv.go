package transport

import (
	"sync"
	"time"

	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
)

// The bounds of the client's reassembly of tiles. A picture has at most protocol.MaxFrameTiles tiles, but fragments of only a
// few tiles are in flight at once on a healthy link, so what a peer that sends fragments and never completes them can make the
// client hold is limited to a small number of tiles and bytes: the oldest partial tile is dropped for a new one.
const (
	maxPartialTiles = 256
	maxPartialBytes = 2 << 20
	// maxRepairTries is how many requests for the same tiles are made without any tile arriving in between.
	maxRepairTries = 8
	// maxRepairWait is the longest the client waits between two requests.
	maxRepairWait = 2 * time.Second
)

// partial is a tile of which some fragments arrived.
type partial struct {
	chunks [][]byte
	got    int
	bytes  int
}

// tileReceiver puts the datagrams of tile fragments together on the client. It follows the newest picture only: a datagram of an
// older one is dropped, one of a newer one starts the picture over. When the newest picture has been quiet for a while and
// still lacks tiles it asks for them (protocol.Missing), and asks again, a little later each time, while tiles keep coming.
type tileReceiver struct {
	clk Clock
	st  *counters
	// deliver hands a tile over; it returns false (and the tile counts as lost) when Next is not being called fast enough.
	deliver func(*protocol.Tile) bool
	// request sends a Missing on the reliable path.
	request func(*protocol.Missing)
	// delay is how long the picture must be quiet before the client asks for what is missing.
	delay func() time.Duration

	mu        sync.Mutex
	closed    bool
	frame     uint64
	total     int
	have      []bool
	haveN     int
	parts     map[uint16]*partial
	order     []uint16
	partBytes int
	last      time.Time
	armed     bool
	stop      func() bool
	tries     int
	backoff   time.Duration
}

func newTileReceiver(clk Clock, st *counters, deliver func(*protocol.Tile) bool, request func(*protocol.Missing), delay func() time.Duration) *tileReceiver {
	return &tileReceiver{clk: clk, st: st, deliver: deliver, request: request, delay: delay, parts: map[uint16]*partial{}}
}

// close stops the timer; the receiver drops every datagram after it.
func (r *tileReceiver) close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	if r.stop != nil {
		r.stop()
		r.stop = nil
	}
	r.armed = false
	r.parts, r.order, r.partBytes = map[uint16]*partial{}, nil, 0
}

// start begins the picture of the fragment, forgetting the one before it.
func (r *tileReceiver) start(f fragment) {
	if r.stop != nil {
		r.stop()
		r.stop = nil
	}
	r.armed = false
	r.frame, r.total = f.frame, int(f.total)
	r.have, r.haveN = make([]bool, r.total), 0
	r.parts, r.order, r.partBytes = map[uint16]*partial{}, nil, 0
	r.tries, r.backoff = 0, 0
}

// onDatagram takes one datagram of a QUIC connection. A datagram that is not valid, not of the newest picture, or a repeat is
// counted and dropped.
func (r *tileReceiver) onDatagram(b []byte) {
	r.st.datagramsReceived.Add(1)
	f, err := parseFragment(b)
	if err != nil {
		r.st.datagramsInvalid.Add(1)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	switch {
	case f.frame < r.frame:
		r.st.tilesStale.Add(1)
		return
	case f.frame > r.frame:
		r.start(f)
	case int(f.total) != r.total:
		r.st.datagramsInvalid.Add(1)
		return
	}
	r.last = r.clk.Now()
	if r.haveN == r.total {
		r.st.tilesDuplicate.Add(1)
		return
	}
	if r.have[f.index] {
		r.st.tilesDuplicate.Add(1)
		r.arm(0)
		return
	}
	encoded := f.chunk
	if f.numbers > 1 {
		var ok bool
		if encoded, ok = r.addFragment(f); !ok {
			r.arm(0)
			return
		}
	}
	t, err := decodeTile(f, encoded)
	if err != nil {
		r.st.datagramsInvalid.Add(1)
		r.arm(0)
		return
	}
	if !r.deliver(t) {
		r.st.tilesDropped.Add(1)
		r.arm(0)
		return
	}
	r.st.tilesReceived.Add(1)
	r.have[f.index] = true
	r.haveN++
	r.tries, r.backoff = 0, 0
	if r.haveN < r.total {
		r.arm(0)
	}
}

// addFragment keeps a fragment and returns the whole encoded tile when it was the last one.
func (r *tileReceiver) addFragment(f fragment) ([]byte, bool) {
	p := r.parts[f.index]
	if p != nil && len(p.chunks) != int(f.numbers) {
		// the fragments of one tile disagree on how many there are: the tile is lost
		r.dropPartial(f.index)
		r.st.datagramsInvalid.Add(1)
		return nil, false
	}
	if p == nil {
		for len(r.parts) >= maxPartialTiles || (r.partBytes+len(f.chunk) > maxPartialBytes && len(r.parts) > 0) {
			r.evictOldest()
		}
		p = &partial{chunks: make([][]byte, f.numbers)}
		r.parts[f.index] = p
		r.order = append(r.order, f.index)
		if len(r.order) > 4*maxPartialTiles {
			r.compactOrder()
		}
	}
	if p.chunks[f.number] != nil {
		r.st.tilesDuplicate.Add(1)
		return nil, false
	}
	p.chunks[f.number] = append([]byte(nil), f.chunk...)
	p.got++
	p.bytes += len(f.chunk)
	r.partBytes += len(f.chunk)
	if p.got < len(p.chunks) {
		return nil, false
	}
	whole := make([]byte, 0, p.bytes)
	for _, c := range p.chunks {
		whole = append(whole, c...)
	}
	r.dropPartial(f.index)
	return whole, true
}

func (r *tileReceiver) dropPartial(index uint16) {
	if p := r.parts[index]; p != nil {
		r.partBytes -= p.bytes
		delete(r.parts, index)
	}
}

func (r *tileReceiver) evictOldest() {
	for len(r.order) > 0 {
		index := r.order[0]
		r.order = r.order[1:]
		if r.parts[index] != nil {
			r.dropPartial(index)
			return
		}
	}
	// nothing is ordered but something is held: start over
	r.parts, r.partBytes = map[uint16]*partial{}, 0
}

func (r *tileReceiver) compactOrder() {
	kept := r.order[:0]
	for _, index := range r.order {
		if r.parts[index] != nil {
			kept = append(kept, index)
		}
	}
	r.order = kept
}

// wait is how long the picture must be quiet before the next request.
func (r *tileReceiver) wait() time.Duration {
	return max(r.delay(), r.backoff)
}

// arm sets the timer if it is not set. d is the time to wait; 0 means the usual wait. The caller holds mu.
func (r *tileReceiver) arm(d time.Duration) {
	if r.armed || r.closed {
		return
	}
	if d <= 0 {
		d = r.wait()
	}
	r.armed = true
	frame := r.frame
	r.stop = r.clk.AfterFunc(d, func() { r.fire(frame) })
}

// fire is the timer: when the picture has been quiet for the wait and still lacks tiles, ask for them.
func (r *tileReceiver) fire(frame uint64) {
	r.mu.Lock()
	r.armed = false
	if r.closed || frame != r.frame || r.haveN >= r.total {
		r.mu.Unlock()
		return
	}
	wait := r.wait()
	if since := r.clk.Now().Sub(r.last); since < wait {
		r.arm(wait - since)
		r.mu.Unlock()
		return
	}
	if r.tries >= maxRepairTries {
		r.mu.Unlock()
		return
	}
	r.tries++
	m := protocol.NewMissing(r.frame, r.have)
	r.backoff = min(max(r.backoff*2, r.delay()*2), maxRepairWait)
	r.last = r.clk.Now()
	r.arm(0)
	r.mu.Unlock()
	if m != nil {
		r.st.missingSent.Add(1)
		r.request(m)
	}
}
