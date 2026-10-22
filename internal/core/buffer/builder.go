package buffer

import (
	"io"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
)

// Builder makes a Buffer from text that arrives in parts, such as a file read
// in blocks. Text is stored in chunks of up to 64 KiB, and a chunk never ends in
// the middle of a UTF-8 sequence, even when the parts do. The text must already
// use "\n" alone for line endings (SPEC §6.1); the builder does not change it.
type Builder struct {
	pieces  []piece
	pending []byte
}

// Write adds text to the end of what is built. It always succeeds.
func (b *Builder) Write(p []byte) (int, error) {
	b.pending = append(b.pending, p...)
	// A cut at maxChunk is made only when at least a whole character follows it,
	// so a character that is still arriving is never cut.
	for len(b.pending) >= maxChunk+utf8.UTFMax {
		n := cutPoint(b.pending, maxChunk)
		b.flush(n)
	}
	return len(p), nil
}

// WriteString adds text to the end of what is built.
func (b *Builder) WriteString(s string) {
	_, _ = b.Write([]byte(s))
}

// flush turns the first n pending bytes into a chunk.
func (b *Builder) flush(n int) {
	data := append([]byte(nil), b.pending[:n]...)
	b.pieces = append(b.pieces, originalPiece(newOriginalChunk(data)))
	b.pending = append(b.pending[:0], b.pending[n:]...)
}

// Buffer returns the Buffer of everything written. The Builder is empty after it.
func (b *Builder) Buffer() *Buffer {
	if len(b.pending) > 0 {
		b.flush(len(b.pending))
	}
	buf := &Buffer{view: Snapshot{root: fromPieces(b.pieces)}}
	b.pieces = nil
	return buf
}

// NewFromReader reads r to its end and returns a Buffer of what it gave. It
// fails with the tag BufferReadFailed when r does, and gives no Buffer.
func NewFromReader(r io.Reader) (*Buffer, error) {
	var b Builder
	if _, err := io.Copy(&b, r); err != nil {
		return nil, errors.NewErr("BufferReadFailed", err)
	}
	return b.Buffer(), nil
}
