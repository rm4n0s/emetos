package transport

import (
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
)

// recvRig is a tile receiver with a clock of its own and a record of what it delivered and asked for.
type recvRig struct {
	clk      *fakeClock
	st       counters
	r        *tileReceiver
	mu       sync.Mutex
	got      []*protocol.Tile
	asked    []*protocol.Missing
	accept   bool
	delay    time.Duration
	datagram func(frame uint64, index, total, size int) [][]byte
}

func newRecvRig(t *testing.T) *recvRig {
	t.Helper()
	g := &recvRig{clk: newClock(), accept: true, delay: 100 * time.Millisecond}
	g.r = newTileReceiver(g.clk, &g.st,
		func(tl *protocol.Tile) bool {
			g.mu.Lock()
			defer g.mu.Unlock()
			if g.accept {
				g.got = append(g.got, tl)
			}
			return g.accept
		},
		func(m *protocol.Missing) {
			g.mu.Lock()
			defer g.mu.Unlock()
			g.asked = append(g.asked, m)
		},
		func() time.Duration { return g.delay })
	g.datagram = func(frame uint64, index, total, size int) [][]byte {
		parts, err := splitTile(frame, uint16(index), uint16(total), encodedTile(t, frame, index, total, size), DefaultChunk)
		if err != nil {
			t.Fatal(err)
		}
		return parts
	}
	return g
}

func (g *recvRig) feed(frame uint64, index, total, size int) {
	for _, d := range g.datagram(frame, index, total, size) {
		g.r.onDatagram(d)
	}
}

func (g *recvRig) delivered() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.got)
}

func (g *recvRig) requests() []*protocol.Missing {
	g.mu.Lock()
	defer g.mu.Unlock()
	return slices.Clone(g.asked)
}

func TestACompletePictureAsksForNothing(t *testing.T) {
	g := newRecvRig(t)
	for i := range 5 {
		g.feed(1, i, 5, 2500)
	}
	if g.delivered() != 5 {
		t.Fatalf("%d tiles delivered", g.delivered())
	}
	g.clk.Advance(10 * time.Second)
	if n := len(g.requests()); n != 0 {
		t.Errorf("%d requests for a picture that is complete", n)
	}
	if g.clk.Pending() != 0 {
		t.Errorf("%d timers left", g.clk.Pending())
	}
}

func TestFragmentsMayArriveInAnyOrder(t *testing.T) {
	g := newRecvRig(t)
	var all [][]byte
	for i := range 4 {
		all = append(all, g.datagram(1, i, 4, 5000)...)
	}
	slices.Reverse(all)
	for _, d := range all {
		g.r.onDatagram(d)
	}
	if g.delivered() != 4 {
		t.Errorf("%d tiles delivered, want 4", g.delivered())
	}
}

func TestATileCountsOnlyWhenAllItsFragmentsArrived(t *testing.T) {
	g := newRecvRig(t)
	parts := g.datagram(1, 0, 2, 5000)
	for _, d := range parts[:len(parts)-1] {
		g.r.onDatagram(d)
	}
	if g.delivered() != 0 {
		t.Fatal("a tile was delivered with a fragment missing")
	}
	g.r.onDatagram(parts[len(parts)-1])
	if g.delivered() != 1 {
		t.Fatal("the tile was not delivered with all its fragments")
	}
}

func TestTheClientAsksForWhatIsMissingAfterTheQuietTime(t *testing.T) {
	g := newRecvRig(t)
	for _, i := range []int{0, 1, 3, 5, 6} {
		g.feed(4, i, 7, 800)
	}
	g.clk.Advance(g.delay - time.Millisecond)
	if len(g.requests()) != 0 {
		t.Fatal("asked before the quiet time passed")
	}
	// a tile arriving restarts the quiet time
	g.feed(4, 2, 7, 800)
	g.clk.Advance(g.delay - time.Millisecond)
	if len(g.requests()) != 0 {
		t.Fatal("asked while tiles were still arriving")
	}
	g.clk.Advance(time.Millisecond)
	reqs := g.requests()
	if len(reqs) != 1 {
		t.Fatalf("%d requests, want 1", len(reqs))
	}
	if m := reqs[0]; m.Frame != 4 || m.Total != 7 || !slices.Equal(m.Indexes(), []int{4}) {
		t.Errorf("asked for %+v (%v)", m, m.Indexes())
	}
	if g.st.missingSent.Load() != 1 {
		t.Errorf("missingSent = %d", g.st.missingSent.Load())
	}
	// the tile arrives: the picture is complete and nothing else is asked
	g.feed(4, 4, 7, 800)
	g.clk.Advance(time.Minute)
	if len(g.requests()) != 1 {
		t.Error("asked again for a complete picture")
	}
}

func TestTheClientAsksAgainALittleLaterAndGivesUpWithoutProgress(t *testing.T) {
	g := newRecvRig(t)
	g.feed(1, 0, 3, 100)
	var at []time.Duration
	start := g.clk.Now()
	for i := 0; i < 400; i++ {
		before := len(g.requests())
		g.clk.Advance(50 * time.Millisecond)
		if len(g.requests()) > before {
			at = append(at, g.clk.Now().Sub(start))
		}
	}
	if len(at) != maxRepairTries {
		t.Fatalf("asked %d times (%v), want %d", len(at), at, maxRepairTries)
	}
	for i := 2; i < len(at); i++ {
		if gap, prev := at[i]-at[i-1], at[i-1]-at[i-2]; gap < prev {
			t.Errorf("the gaps shrink: %v then %v", prev, gap)
		}
	}
	if gap := at[len(at)-1] - at[len(at)-2]; gap > maxRepairWait+100*time.Millisecond {
		t.Errorf("the last gap is %v, over the cap", gap)
	}
	// progress starts the asking again
	g.feed(1, 1, 3, 100)
	g.clk.Advance(5 * time.Second)
	if n := len(g.requests()); n <= maxRepairTries {
		t.Errorf("%d requests: no new ones after progress", n)
	}
}

func TestAPictureThatIsNotTheNewestIsDropped(t *testing.T) {
	g := newRecvRig(t)
	g.feed(5, 0, 2, 100)
	g.feed(3, 0, 2, 100)
	g.feed(3, 1, 2, 5000)
	// the datagrams of the picture 3 are 1 and 5 (the second tile is 5 fragments)
	if g.delivered() != 1 || g.st.tilesStale.Load() != 6 {
		t.Errorf("delivered %d, stale %d: want 1 and 6", g.delivered(), g.st.tilesStale.Load())
	}
	// a newer picture starts over: what was missing of the old one is forgotten, and its partial tiles
	parts := g.datagram(5, 1, 2, 5000)
	g.r.onDatagram(parts[0])
	g.feed(6, 0, 1, 100)
	for _, d := range parts[1:] {
		g.r.onDatagram(d) // fragments of the picture 5: stale now
	}
	if g.delivered() != 2 {
		t.Errorf("delivered %d, want 2", g.delivered())
	}
	g.clk.Advance(time.Minute)
	for _, m := range g.requests() {
		if m.Frame != 5 && m.Frame != 6 {
			t.Errorf("asked about frame %d", m.Frame)
		}
		if m.Frame == 6 {
			t.Error("asked about the complete picture")
		}
	}
}

func TestARepeatedTileIsCountedAndNotDelivered(t *testing.T) {
	g := newRecvRig(t)
	g.feed(1, 0, 2, 100)
	g.feed(1, 0, 2, 100)
	g.feed(1, 1, 2, 5000)
	g.feed(1, 1, 2, 5000) // after the picture is complete
	// the second tile is 5 fragments, so the repeat after the picture is complete counts 5 times, the first repeat once
	if g.delivered() != 2 || g.st.tilesDuplicate.Load() != 6 {
		t.Errorf("delivered %d, duplicate %d, want 2 and 6", g.delivered(), g.st.tilesDuplicate.Load())
	}
	// a fragment twice
	parts := g.datagram(2, 0, 2, 5000)
	g.r.onDatagram(parts[0])
	g.r.onDatagram(parts[0])
	if g.st.tilesDuplicate.Load() != 7 {
		t.Errorf("duplicate = %d, want 7", g.st.tilesDuplicate.Load())
	}
}

func TestATileTheConsumerCannotTakeIsAskedForAgain(t *testing.T) {
	g := newRecvRig(t)
	g.accept = false
	g.feed(1, 0, 2, 100)
	g.feed(1, 1, 2, 100)
	if g.st.tilesDropped.Load() != 2 || g.st.tilesReceived.Load() != 0 {
		t.Fatalf("dropped %d, received %d", g.st.tilesDropped.Load(), g.st.tilesReceived.Load())
	}
	g.clk.Advance(time.Second)
	reqs := g.requests()
	if len(reqs) == 0 || !slices.Equal(reqs[0].Indexes(), []int{0, 1}) {
		t.Fatalf("requests %+v", reqs)
	}
	g.accept = true
	g.feed(1, 0, 2, 100)
	g.feed(1, 1, 2, 100)
	if g.delivered() != 2 {
		t.Errorf("delivered %d", g.delivered())
	}
}

func TestAPeerThatNeverCompletesTilesHoldsABoundedAmount(t *testing.T) {
	g := newRecvRig(t)
	const total = 2000
	for i := range total {
		// a picture of 7680x1080 pixels has 2,040 tiles
		tl := &protocol.Tile{Frame: 1, Width: 7680, Height: 1080, X: uint16(i % 120), Y: uint16(i / 120), Total: 2040, Data: make([]byte, 5000)}
		enc, err := protocol.Append(nil, tl)
		if err != nil {
			t.Fatal(err)
		}
		parts, err := splitTile(1, uint16(i), 2040, enc, DefaultChunk)
		if err != nil {
			t.Fatal(err)
		}
		g.r.onDatagram(parts[0]) // only the first fragment of each
		g.r.mu.Lock()
		if len(g.r.parts) > maxPartialTiles || g.r.partBytes > maxPartialBytes {
			g.r.mu.Unlock()
			t.Fatalf("after %d tiles: %d partial tiles, %d bytes", i, len(g.r.parts), g.r.partBytes)
		}
		g.r.mu.Unlock()
	}
	g.r.mu.Lock()
	defer g.r.mu.Unlock()
	if len(g.r.order) > 4*maxPartialTiles+1 {
		t.Errorf("the order list grew to %d", len(g.r.order))
	}
}

func TestInvalidDatagramsAreCounted(t *testing.T) {
	g := newRecvRig(t)
	good := g.datagram(1, 0, 3, 100)[0]
	g.r.onDatagram(good)
	// a datagram of the same picture that counts another total
	g.r.onDatagram(g.datagram(1, 1, 5, 100)[0])
	// a tile that is not valid: the right header and bytes that are no tile
	bad := append(bytesOf(good[:datagramHeader]), 1, 2, 3)
	bad[8], bad[9] = 0, 1
	g.r.onDatagram(bad)
	// fragments of one tile that disagree on their number
	two := g.datagram(1, 2, 3, 3000)
	g.r.onDatagram(two[0])
	other := bytesOf(two[1])
	other[13] = byte(len(two) + 1)
	g.r.onDatagram(other)
	g.r.onDatagram([]byte{1, 2, 3})
	if n := g.st.datagramsInvalid.Load(); n != 4 {
		t.Errorf("%d invalid datagrams, want 4", n)
	}
	if g.delivered() != 1 {
		t.Errorf("delivered %d", g.delivered())
	}
}

func bytesOf(b []byte) []byte { return append([]byte(nil), b...) }

func TestAClosedReceiverDropsEverythingAndHoldsNoTimer(t *testing.T) {
	g := newRecvRig(t)
	g.feed(1, 0, 3, 100)
	if g.clk.Pending() != 1 {
		t.Fatalf("%d timers", g.clk.Pending())
	}
	g.r.close()
	if g.clk.Pending() != 0 {
		t.Error("the timer was left")
	}
	g.feed(2, 0, 1, 100)
	g.clk.Advance(time.Minute)
	if g.delivered() != 1 || len(g.requests()) != 0 {
		t.Errorf("delivered %d, requests %d", g.delivered(), len(g.requests()))
	}
}
