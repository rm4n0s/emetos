package transport

import (
	"context"
	"sync/atomic"

	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
)

// Kind is the kind of transport a Conn runs on at the moment.
type Kind int

// The kinds of transport.
const (
	// KindNone: no link is up (the connection is being made, or a dropped QUIC connection is being resumed).
	KindNone Kind = iota
	// KindQUIC: QUIC on UDP, with datagrams for the tiles.
	KindQUIC
	// KindSSH: the SSH channel (the TCP fallback), with the tiles as messages.
	KindSSH
)

// String is the name of the kind.
func (k Kind) String() string {
	switch k {
	case KindQUIC:
		return "QUIC"
	case KindSSH:
		return "SSH"
	}
	return "none"
}

// Conn is what the extension uses to send and receive the messages of package protocol, whatever carries them. The host's
// HostConn and the client's ClientConn implement it. Every method may be called from any goroutine.
type Conn interface {
	// Kind is the transport in use now.
	Kind() Kind
	// Send queues a message on the reliable, ordered path. It never waits for the peer: a message that does not fit the
	// bounded queue (the peer is not reading) is refused with TransportQueueFull. The message is encoded at once, so it
	// may be reused when Send returns. The tiles of a picture go through SendTiles.
	Send(m protocol.Message) error
	// SendTiles hands over the tiles of one picture, as tiles.Differ.Next made them. Only the newest picture waits to be sent:
	// one that is still waiting (or only partly sent) when a newer one arrives is dropped, and nothing of it is queued.
	SendTiles(frame uint64, tiles []*protocol.Tile) error
	// OnTilesSent registers the function that is called, on a goroutine of the connection, when the last tile of a picture has been
	// handed to the transport for the first time (the picture is "out"; the tiles a client asks for again do not count). A
	// picture that was replaced before it was all sent does not call it. The picture flow times its retries from that moment, so
	// that a slow link, which needs seconds to send a picture, is not sent the same picture again over the one being sent.
	OnTilesSent(f func(frame uint64))
	// Next waits for the next message that arrived: the ones of the reliable path in their order, the tiles as they complete.
	// After the connection ended it gives what had arrived (the peer's Disconnect among it), then an error.
	Next(ctx context.Context) (protocol.Message, error)
	// Done is closed when the connection ended, and Err says why (nil while it lives; a connection that was closed on purpose
	// or by the peer's Disconnect gives TransportClosed).
	Done() <-chan struct{}
	// Err is why the connection ended, nil while it lives.
	Err() error
	// Close ends the session: the peer is told why with a Disconnect, and both the links and the goroutines of the
	// connection go. It does nothing the second time.
	Close(reason protocol.Reason, text string) error
	// Stats counts what the connection did.
	Stats() Stats
}

// Stats counts the traffic of a Conn, for tests and for the tab's status. The counters only grow.
type Stats struct {
	// DatagramsSent and DatagramsReceived are the QUIC datagrams of tile fragments.
	DatagramsSent, DatagramsReceived uint64
	// TilesSent counts the tiles whose every fragment was handed to the transport (or written, on SSH); TilesResent the ones
	// sent again because the client asked; BatchesDropped the pictures replaced before all their tiles were sent.
	TilesSent, TilesResent, BatchesDropped uint64
	// TilesReceived counts the tiles given to Next; TilesStale the fragments dropped because their picture was not the newest;
	// TilesDuplicate the tiles that arrived twice; TilesDropped the tiles dropped because Next was not being called fast enough;
	// DatagramsInvalid the datagrams and tiles that were not valid.
	TilesReceived, TilesStale, TilesDuplicate, TilesDropped, DatagramsInvalid uint64
	// MissingSent and MissingReceived count the requests for lost tiles.
	MissingSent, MissingReceived uint64
	// Resumes counts the QUIC connections that replaced a dropped one.
	Resumes uint64
}

// counters is Stats with atomic fields.
type counters struct {
	datagramsSent, datagramsReceived                                          atomic.Uint64
	tilesSent, tilesResent, batchesDropped                                    atomic.Uint64
	tilesReceived, tilesStale, tilesDuplicate, tilesDropped, datagramsInvalid atomic.Uint64
	missingSent, missingReceived, resumes                                     atomic.Uint64
}

func (c *counters) snapshot() Stats {
	return Stats{
		DatagramsSent: c.datagramsSent.Load(), DatagramsReceived: c.datagramsReceived.Load(),
		TilesSent: c.tilesSent.Load(), TilesResent: c.tilesResent.Load(), BatchesDropped: c.batchesDropped.Load(),
		TilesReceived: c.tilesReceived.Load(), TilesStale: c.tilesStale.Load(), TilesDuplicate: c.tilesDuplicate.Load(),
		TilesDropped: c.tilesDropped.Load(), DatagramsInvalid: c.datagramsInvalid.Load(),
		MissingSent: c.missingSent.Load(), MissingReceived: c.missingReceived.Load(), Resumes: c.resumes.Load(),
	}
}
