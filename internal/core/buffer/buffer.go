package buffer

import (
	"github.com/rm4n0s/errors"
)

// Buffer is the text of a document: a piece tree over read-only chunks and an
// append-only add buffer (SPEC §6.1). Its offsets are byte offsets. A Buffer is
// used by one goroutine at a time; Snapshot gives other goroutines a view that
// edits never change.
type Buffer struct {
	// view is the current text; its methods are the Buffer's reading methods.
	view
	// add is the chunk new text is appended to, and addUsed and addFeeds count
	// its bytes and line feeds written so far. They belong to the editing
	// goroutine alone, so readers of a chunk never touch them.
	add      *chunk
	addUsed  int
	addFeeds int
}

// view is a Snapshot as the Buffer holds it. It is an alias so that the Buffer
// gets the reading methods of Snapshot without a field of the same name as its
// Snapshot method.
type view = Snapshot

// New makes a Buffer that holds text.
func New(text string) *Buffer {
	return &Buffer{view: Snapshot{root: fromPieces(originalPieces(text))}}
}

// Snapshot is an immutable view of the text at one moment. It is a value that
// can be copied freely and read from any goroutine while the Buffer it came from
// goes on changing.
type Snapshot struct {
	root *node
}

// Snapshot returns the text as it is now, in O(1).
func (b *Buffer) Snapshot() Snapshot {
	return b.view
}

// Len is the length of the text in bytes.
func (s Snapshot) Len() int { return size(s.root) }

// LineFeeds is the number of line feeds in the text.
func (s Snapshot) LineFeeds() int { return lineFeeds(s.root) }

// Slice returns the bytes [start, end) as a string. It fails with the tag
// OffsetRangeInvalid unless 0 <= start <= end <= Len().
func (s Snapshot) Slice(start, end int) (string, error) {
	if start < 0 || end < start || end > s.Len() {
		return "", errors.New("OffsetRangeInvalid", "the offsets are not a range of the text",
			"start", start, "end", end, "len", s.Len())
	}
	return string(s.root.appendRange(make([]byte, 0, end-start), start, end)), nil
}

// String is the whole text.
func (s Snapshot) String() string {
	return string(s.root.appendRange(make([]byte, 0, s.Len()), 0, s.Len()))
}

// Insert puts text at the byte offset. It fails with the tag OffsetOutOfRange
// unless 0 <= offset <= Len().
func (b *Buffer) Insert(offset int, text string) error {
	if offset < 0 || offset > b.Len() {
		return errors.New("OffsetOutOfRange", "the offset is not in the text", "offset", offset, "len", b.Len())
	}
	left, right := split(b.root, offset)
	b.root = b.splice(left, text, right)
	return nil
}

// Delete removes the bytes [start, end). It fails with the tag
// OffsetRangeInvalid unless 0 <= start <= end <= Len().
func (b *Buffer) Delete(start, end int) error {
	return b.Replace(start, end, "")
}

// Replace puts text in place of the bytes [start, end), in one step. It fails
// with the tag OffsetRangeInvalid unless 0 <= start <= end <= Len().
func (b *Buffer) Replace(start, end int, text string) error {
	if start < 0 || end < start || end > b.Len() {
		return errors.New("OffsetRangeInvalid", "the offsets are not a range of the text",
			"start", start, "end", end, "len", b.Len())
	}
	left, rest := split(b.root, start)
	_, right := split(rest, end-start)
	b.root = b.splice(left, text, right)
	return nil
}

// splice makes the tree of left, then text, then right.
func (b *Buffer) splice(left *node, text string, right *node) *node {
	if text == "" {
		return join2(left, right)
	}
	// Typing: when the text goes right after the last piece of the add buffer,
	// that piece grows instead of a new one being made.
	if last, ok := lastPiece(left); ok && b.canExtend(last, len(text)) {
		return join2(replaceLast(left, b.extend(last, text)), right)
	}
	ps := b.addPieces(text)
	if len(ps) == 1 {
		return join(left, ps[0], right)
	}
	return join2(join2(left, fromPieces(ps)), right)
}

// canExtend reports whether p ends where the add buffer's written part ends and
// n more bytes fit there.
func (b *Buffer) canExtend(p piece, n int) bool {
	return b.add != nil && p.c == b.add && p.end == b.addUsed && b.addUsed+n <= len(b.add.data)
}

// extend appends text to the add buffer and returns p grown by it; canExtend
// must be true.
func (b *Buffer) extend(p piece, text string) piece {
	b.write(text)
	p.end, p.fEnd = b.addUsed, b.addFeeds
	return p
}

// write copies text to the free part of the add chunk, which must have room.
func (b *Buffer) write(text string) {
	copy(b.add.data[b.addUsed:], text)
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			b.add.feeds[b.addFeeds] = int32(b.addUsed + i)
			b.addFeeds++
		}
	}
	b.addUsed += len(text)
}

// addPieces stores text in the add buffer and returns the pieces that cover it:
// one, unless the text is longer than a chunk.
func (b *Buffer) addPieces(text string) []piece {
	var out []piece
	for len(text) > 0 {
		part := text[:cutPoint(text, maxChunk)]
		if b.add == nil || b.addUsed+len(part) > len(b.add.data) {
			b.startAddChunk(len(part))
		}
		start, fStart := b.addUsed, b.addFeeds
		b.write(part)
		out = append(out, piece{b.add, start, b.addUsed, fStart, b.addFeeds})
		text = text[len(part):]
	}
	return out
}

// startAddChunk replaces the add chunk by a new one with room for at least n
// bytes. Chunks double in size up to maxChunk, so a long editing session makes
// few of them.
func (b *Buffer) startAddChunk(n int) {
	size := minAddChunk
	if b.add != nil {
		size = min(2*len(b.add.data), maxChunk)
	}
	b.add = newAddChunk(max(size, n))
	b.addUsed, b.addFeeds = 0, 0
}
