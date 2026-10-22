package buffer

import (
	"sort"
	"unicode/utf8"
)

const (
	// maxChunk is the largest chunk, in bytes. Text longer than this is stored in
	// several chunks, so a piece never covers more than maxChunk bytes.
	maxChunk = 64 << 10
	// minAddChunk is the size of the first chunk of the add buffer.
	minAddChunk = 4 << 10
)

// chunk is a block of text that pieces point into. Its slices are never
// reassigned, and the bytes a piece covers are never written again, so a chunk
// can be read from any goroutine while the editing goroutine appends to the
// part of an add chunk that no piece covers yet.
type chunk struct {
	// data holds the text. An original chunk is full; an add chunk has the
	// capacity of its allocation and is filled from the front (the Buffer knows
	// how far).
	data []byte
	// feeds holds the offsets in data of its line feeds, in order. An original
	// chunk has exactly one entry per line feed; an add chunk has room for one
	// per byte, filled from the front like data.
	feeds []int32
}

// newOriginalChunk copies text into a chunk and finds its line feeds.
func newOriginalChunk(text string) *chunk {
	c := &chunk{data: []byte(text)}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			c.feeds = append(c.feeds, int32(i))
		}
	}
	return c
}

// newAddChunk makes an empty chunk with room for size bytes.
func newAddChunk(size int) *chunk {
	return &chunk{data: make([]byte, size), feeds: make([]int32, size)}
}

// piece is a run of bytes of one chunk: data[start:end]. Its line feeds are
// feeds[fStart:fEnd], so counting them costs nothing.
type piece struct {
	c      *chunk
	start  int
	end    int
	fStart int
	fEnd   int
}

func (p piece) len() int { return p.end - p.start }

// lf is the number of line feeds in the piece.
func (p piece) lf() int { return p.fEnd - p.fStart }

// bytes is the text of the piece. It must not be changed.
func (p piece) bytes() []byte { return p.c.data[p.start:p.end] }

// splitAt cuts the piece at off bytes from its start, 0 < off < len().
func (p piece) splitAt(off int) (piece, piece) {
	at := p.start + off
	k := p.fStart + sort.Search(p.lf(), func(i int) bool { return int(p.c.feeds[p.fStart+i]) >= at })
	return piece{p.c, p.start, at, p.fStart, k}, piece{p.c, at, p.end, k, p.fEnd}
}

// cutPoint is the largest n <= limit such that text[:n] doesn't end inside a
// UTF-8 sequence, or limit when no such n is within three bytes.
func cutPoint(text string, limit int) int {
	if limit >= len(text) {
		return len(text)
	}
	for n := limit; n > limit-utf8.UTFMax && n > 0; n-- {
		if utf8.RuneStart(text[n]) {
			return n
		}
	}
	return limit
}

// originalPieces stores text in original chunks of at most maxChunk bytes and
// returns one piece per chunk.
func originalPieces(text string) []piece {
	var out []piece
	for len(text) > 0 {
		n := cutPoint(text, maxChunk)
		c := newOriginalChunk(text[:n])
		out = append(out, piece{c, 0, len(c.data), 0, len(c.feeds)})
		text = text[n:]
	}
	return out
}
