package transport

import (
	"sync"

	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
)

// tileBuffer holds the tiles that completed and wait for Next. It is bounded by count and by bytes; a tile that does not fit is
// refused (and the client asks for it again).
type tileBuffer struct {
	mu    sync.Mutex
	q     []*protocol.Tile
	bytes int
	// ready is signalled when a tile was added.
	ready chan struct{}
}

func newTileBuffer() *tileBuffer { return &tileBuffer{ready: make(chan struct{}, 1)} }

// push adds a tile, or says there is no room.
func (b *tileBuffer) push(t *protocol.Tile) bool {
	b.mu.Lock()
	if len(b.q) >= MaxBufferedTiles || b.bytes+len(t.Data) > MaxBufferedTileBytes {
		b.mu.Unlock()
		return false
	}
	b.q = append(b.q, t)
	b.bytes += len(t.Data)
	b.mu.Unlock()
	wake(b.ready)
	return true
}

// pop takes the oldest tile.
func (b *tileBuffer) pop() (*protocol.Tile, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.q) == 0 {
		return nil, false
	}
	t := b.q[0]
	b.q[0] = nil
	b.q = b.q[1:]
	b.bytes -= len(t.Data)
	if len(b.q) == 0 {
		b.q = nil
	}
	return t, true
}
