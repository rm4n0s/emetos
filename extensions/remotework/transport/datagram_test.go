package transport

import (
	"bytes"
	"math/rand/v2"
	"testing"

	"github.com/rm4n0s/errors"

	"github.com/rm4n0s/emetos/extensions/remotework/protocol"
)

func encodedTile(t testing.TB, frame uint64, index, total, size int) []byte {
	t.Helper()
	tl := fakeTiles(frame, total, size)[index]
	b, err := protocol.Append(nil, tl)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestATileIsCutIntoDatagramsAndPutBackTogether(t *testing.T) {
	tests := []struct {
		name      string
		size      int
		chunk     int
		fragments int
	}{
		{"a small tile is one datagram", 100, DefaultChunk, 1},
		{"a tile that just fits", DefaultChunk - 5 - 28, DefaultChunk, 1},
		{"a tile one byte over", DefaultChunk - 5 - 28 + 1, DefaultChunk, 2},
		{"a typical text tile of 850 bytes", 850, DefaultChunk, 1},
		{"a tile of 5000 bytes", 5000, DefaultChunk, 5},
		{"the largest tile at the usual chunk", protocol.MaxTileData, DefaultChunk, 16},
		{"the largest tile at the smallest chunk", protocol.MaxTileData, MinChunk, 30},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			enc := encodedTile(t, 7, 3, 5, tt.size)
			parts, err := splitTile(7, 3, 5, enc, tt.chunk)
			if err != nil {
				t.Fatal(err)
			}
			if len(parts) != tt.fragments {
				t.Fatalf("%d fragments, want %d", len(parts), tt.fragments)
			}
			var whole []byte
			for i, d := range parts {
				if len(d) > datagramHeader+tt.chunk {
					t.Errorf("fragment %d is %d bytes", i, len(d))
				}
				f, err := parseFragment(d)
				if err != nil {
					t.Fatal(err)
				}
				if f.frame != 7 || f.index != 3 || f.total != 5 || int(f.number) != i || int(f.numbers) != tt.fragments {
					t.Errorf("fragment %d parsed as %+v", i, f)
				}
				whole = append(whole, f.chunk...)
			}
			if !bytes.Equal(whole, enc) {
				t.Error("the chunks do not make the tile again")
			}
		})
	}
}

func TestSplitTileRefusesWhatCannotBeSent(t *testing.T) {
	enc := encodedTile(t, 1, 0, 1, 10)
	tests := []struct {
		name  string
		index uint16
		total uint16
		enc   []byte
		chunk int
	}{
		{"an index that is not below the total", 3, 3, enc, DefaultChunk},
		{"a total of 0", 0, 0, enc, DefaultChunk},
		{"a total over the limit", 0, protocol.MaxFrameTiles + 1, enc, DefaultChunk},
		{"no data", 0, 1, nil, DefaultChunk},
		{"a chunk of 0", 0, 1, enc, 0},
		{"more fragments than the limit", 0, 1, make([]byte, MaxFragments*10+1), 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := splitTile(1, tt.index, tt.total, tt.enc, tt.chunk)
			wantRoute(t, err, "splitTile.TransportTilesInvalid")
		})
	}
}

func TestMalformedDatagramsAreRefusedWithTheirRoute(t *testing.T) {
	good, err := splitTile(2, 1, 4, encodedTile(t, 2, 1, 4, 50), DefaultChunk)
	if err != nil {
		t.Fatal(err)
	}
	mod := func(f func(b []byte)) []byte {
		b := bytes.Clone(good[0])
		f(b)
		return b
	}
	tests := []struct {
		name string
		in   []byte
	}{
		{"nothing", nil},
		{"a header and no data", good[0][:datagramHeader]},
		{"half a header", good[0][:7]},
		{"frame 0", mod(func(b []byte) { clear(b[:8]) })},
		{"a total of 0", mod(func(b []byte) { b[10], b[11] = 0, 0 })},
		{"a total over the limit", mod(func(b []byte) { b[10], b[11] = 0x10, 0x01 })},
		{"an index not below the total", mod(func(b []byte) { b[8], b[9] = 0, 4 })},
		{"0 fragments", mod(func(b []byte) { b[13] = 0 })},
		{"more fragments than the limit", mod(func(b []byte) { b[13] = MaxFragments + 1 })},
		{"a fragment number not below the count", mod(func(b []byte) { b[12], b[13] = 2, 2 })},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseFragment(tt.in)
			wantRoute(t, err, "parseFragment.TransportDatagramInvalid")
		})
	}
}

func TestDecodeTileChecksWhatTheDatagramsSay(t *testing.T) {
	f := fragment{frame: 2, index: 1, total: 4}
	ping, err := protocol.Append(nil, &protocol.Ping{Seq: 1})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name  string
		frag  fragment
		bytes []byte
		route string
	}{
		{"a message that is not a tile", f, ping, "decodeTile.TransportDatagramInvalid"},
		{"a tile of another frame", fragment{frame: 3, index: 1, total: 4}, encodedTile(t, 2, 1, 4, 20), "decodeTile.TransportDatagramInvalid"},
		{"a tile that counts another total", fragment{frame: 2, index: 1, total: 5}, encodedTile(t, 2, 1, 4, 20), "decodeTile.TransportDatagramInvalid"},
		{"bytes that are no message", f, []byte{0xff, 1, 2}, "Decode.RemoteWorkTruncated"},
		{"a message cut short", f, encodedTile(t, 2, 1, 4, 20)[:20], "Decode.RemoteWorkTruncated"},
		{"a message with bytes after it", f, append(encodedTile(t, 2, 1, 4, 20), 1), "Decode.RemoteWorkTrailing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := decodeTile(tt.frag, tt.bytes)
			wantRoute(t, err, tt.route)
		})
	}
	if tl, err := decodeTile(f, encodedTile(t, 2, 1, 4, 20)); err != nil || tl.Frame != 2 {
		t.Errorf("a good tile: %+v %v", tl, err)
	}
}

func TestSeededRandomDatagramsNeverPanic(t *testing.T) {
	rng := rand.New(rand.NewPCG(20261027, 3))
	good, err := splitTile(9, 2, 6, encodedTile(t, 9, 2, 6, 3000), DefaultChunk)
	if err != nil {
		t.Fatal(err)
	}
	accepted := 0
	for range 20000 {
		var in []byte
		switch rng.IntN(3) {
		case 0:
			in = make([]byte, rng.IntN(60))
			for i := range in {
				in[i] = byte(rng.IntN(256))
			}
		default:
			in = bytes.Clone(good[rng.IntN(len(good))])
			for range 1 + rng.IntN(3) {
				in[rng.IntN(len(in))] = byte(rng.IntN(256))
			}
			in = in[:1+rng.IntN(len(in))]
		}
		f, err := parseFragment(in)
		if err != nil {
			if _, ok := errors.FromError(err); !ok {
				t.Fatalf("the error is %T", err)
			}
			continue
		}
		accepted++
		if f.index >= f.total || f.number >= f.numbers || len(f.chunk) == 0 || !bytes.Equal(f.chunk, in[datagramHeader:]) {
			t.Fatalf("%x parsed as %+v", in, f)
		}
		// and whatever they hold either decodes to the tile they name or is refused with an error
		if _, err := decodeTile(f, f.chunk); err != nil {
			if _, ok := errors.FromError(err); !ok {
				t.Fatalf("the error is %T", err)
			}
		}
	}
	if accepted == 0 {
		t.Error("the random input never made a valid datagram")
	}
}
