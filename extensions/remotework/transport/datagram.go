package transport

import (
	"encoding/binary"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
)

// A tile travels as one datagram when it fits and as fragments when it does not. A datagram is
//
//	frame:8 | index:2 | total:2 | fragment:1 | fragments:1 | chunk
//
// where frame is the picture the tile is of, index the place of the tile among the total tiles that picture sends (in the order
// the host sent them; the client counts them to know what is missing) and the chunks of the fragments, in order, are one
// encoded Tile message of package protocol, header included. Decoding it with protocol.Decode is what checks the tile.
const (
	// datagramHeader is the size of the fields before the chunk.
	datagramHeader = 8 + 2 + 2 + 1 + 1
	// MaxFragments is the most fragments one tile is cut into: a tile is at most 17,441 bytes with its header
	// (protocol.MaxTileData and the tile fields), which is 16 datagrams of the smallest chunk the transport uses (MinChunk).
	MaxFragments = 32
	// DefaultChunk is the size of the chunk of a fragment: a datagram of 1,114 bytes fits QUIC's smallest packet (1,200).
	DefaultChunk = 1100
	// MinChunk is the smallest chunk the transport shrinks to when the path cannot take DefaultChunk.
	MinChunk = 600
)

// fragment is a parsed datagram.
type fragment struct {
	frame           uint64
	index, total    uint16
	number, numbers uint8
	chunk           []byte
}

// splitTile cuts the encoded tile into datagrams of at most chunk bytes of data each. It fails with
// TransportTilesInvalid for a tile that would need more than MaxFragments, or an index that is not below the total.
func splitTile(frame uint64, index, total uint16, encoded []byte, chunk int) ([][]byte, error) {
	if chunk <= 0 || len(encoded) == 0 || int(index) >= int(total) || total == 0 || int(total) > protocol.MaxFrameTiles {
		return nil, errors.New("TransportTilesInvalid", "the tile cannot be cut into datagrams", "index", index, "total", total, "size", len(encoded), "chunk", chunk)
	}
	n := (len(encoded) + chunk - 1) / chunk
	if n > MaxFragments {
		return nil, errors.New("TransportTilesInvalid", "the tile needs more fragments than the limit", "fragments", n, "limit", MaxFragments)
	}
	out := make([][]byte, 0, n)
	for i := range n {
		part := encoded[i*chunk : min((i+1)*chunk, len(encoded))]
		d := make([]byte, 0, datagramHeader+len(part))
		d = binary.BigEndian.AppendUint64(d, frame)
		d = binary.BigEndian.AppendUint16(d, index)
		d = binary.BigEndian.AppendUint16(d, total)
		d = append(d, byte(i), byte(n))
		d = append(d, part...)
		out = append(out, d)
	}
	return out, nil
}

// parseFragment reads a datagram. The chunk is not copied. It fails with TransportDatagramInvalid.
func parseFragment(b []byte) (fragment, error) {
	if len(b) <= datagramHeader {
		return fragment{}, errors.New("TransportDatagramInvalid", "the datagram has no data", "size", len(b))
	}
	f := fragment{
		frame: binary.BigEndian.Uint64(b), index: binary.BigEndian.Uint16(b[8:]), total: binary.BigEndian.Uint16(b[10:]),
		number: b[12], numbers: b[13], chunk: b[datagramHeader:],
	}
	switch {
	case f.frame == 0:
		return fragment{}, errors.New("TransportDatagramInvalid", "the frame is 0")
	case f.total == 0 || int(f.total) > protocol.MaxFrameTiles || f.index >= f.total:
		return fragment{}, errors.New("TransportDatagramInvalid", "the tile is not one of the tiles of its picture", "index", f.index, "total", f.total)
	case f.numbers == 0 || f.numbers > MaxFragments || f.number >= f.numbers:
		return fragment{}, errors.New("TransportDatagramInvalid", "the fragment is not one of the fragments of its tile", "fragment", f.number, "fragments", f.numbers)
	}
	return f, nil
}

// decodeTile turns the chunks of a tile, joined, into the Tile message and checks that it is the tile the datagrams said
// it was. It fails with TransportDatagramInvalid, and with the tags of protocol.Decode.
func decodeTile(f fragment, encoded []byte) (*protocol.Tile, error) {
	m, err := protocol.Decode(encoded)
	if err != nil {
		return nil, err // the tags of protocol.Decode say what is wrong with it
	}
	t, ok := m.(*protocol.Tile)
	if !ok {
		return nil, errors.New("TransportDatagramInvalid", "the datagrams carry a message that is not a tile", "type", m.Type().String())
	}
	if t.Frame != f.frame || t.Total != uint32(f.total) {
		return nil, errors.New("TransportDatagramInvalid", "the tile is not the one its datagrams name", "frame", t.Frame, "total", t.Total, "datagram frame", f.frame, "datagram total", f.total)
	}
	return t, nil
}
