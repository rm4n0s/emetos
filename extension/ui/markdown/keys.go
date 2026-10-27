package markdown

import (
	"hash"
	"hash/fnv"
	"strconv"
)

// blockKey identifies a top-level block by what it shows, not by where it is: the same text gives the same key after an edit
// above it, so the widgets and the measured heights of the blocks that did not change are kept. sum is a 64-bit hash of the
// block's content (not its position in the source), and nth tells apart blocks with the same content (the second "---" of a
// document is the second block with that sum).
type blockKey struct {
	sum uint64
	nth int
}

// blockKeys gives the key of each block.
func blockKeys(blocks []Block) []blockKey {
	keys := make([]blockKey, len(blocks))
	seen := map[uint64]int{}
	h := fnv.New64a()
	var w keyWriter
	for i, b := range blocks {
		h.Reset()
		w.h = h
		w.block(b)
		sum := h.Sum64()
		keys[i] = blockKey{sum: sum, nth: seen[sum]}
		seen[sum]++
	}
	return keys
}

// keyWriter writes what a block shows into a hash: its kind, its text and its structure, not the lines it came from.
type keyWriter struct{ h hash.Hash64 }

func (w *keyWriter) str(s string) {
	w.int(len(s))
	_, _ = w.h.Write([]byte(s)) // a hash.Hash never fails
}

func (w *keyWriter) int(n int) {
	var buf [24]byte
	s := append(strconv.AppendInt(buf[:0], int64(n), 10), ',')
	_, _ = w.h.Write(s)
}

func (w *keyWriter) tag(t byte) { _, _ = w.h.Write([]byte{t}) }

func (w *keyWriter) blocks(bs []Block) {
	w.int(len(bs))
	for _, b := range bs {
		w.block(b)
	}
}

func (w *keyWriter) block(b Block) {
	switch b := b.(type) {
	case Heading:
		w.tag('H')
		w.int(b.Level)
		w.inlines(b.Inlines)
	case Paragraph:
		w.tag('P')
		w.inlines(b.Inlines)
	case CodeBlock:
		w.tag('C')
		w.str(b.Info)
		w.str(b.Text)
		w.int(boolInt(b.Fenced))
	case Quote:
		w.tag('Q')
		w.blocks(b.Blocks)
	case List:
		w.tag('L')
		w.int(boolInt(b.Ordered))
		w.int(b.Start)
		w.int(boolInt(b.Tight))
		w.int(len(b.Items))
		for _, it := range b.Items {
			w.int(boolInt(it.Task))
			w.int(boolInt(it.Checked))
			w.blocks(it.Blocks)
		}
	case Table:
		w.tag('T')
		w.int(len(b.Align))
		for _, a := range b.Align {
			w.int(int(a))
		}
		w.row(b.Header)
		w.int(len(b.Rows))
		for _, r := range b.Rows {
			w.row(r)
		}
	case Rule:
		w.tag('R')
	case HTMLBlock:
		w.tag('M')
		w.str(b.Text)
	}
}

func (w *keyWriter) row(cells []TableCell) {
	w.int(len(cells))
	for _, c := range cells {
		w.inlines(c.Inlines)
	}
}

func (w *keyWriter) inlines(in []Inline) {
	w.int(len(in))
	for _, n := range in {
		switch n := n.(type) {
		case Text:
			w.tag('t')
			w.str(n.S)
		case Emph:
			w.tag('e')
			w.inlines(n.Children)
		case Strong:
			w.tag('s')
			w.inlines(n.Children)
		case Strike:
			w.tag('x')
			w.inlines(n.Children)
		case Code:
			w.tag('c')
			w.str(n.S)
		case Link:
			w.tag('l')
			w.str(n.Dest)
			w.inlines(n.Children)
		case Image:
			w.tag('i')
			w.str(n.Dest)
			w.str(n.Alt)
		case SoftBreak:
			w.tag('b')
		case HardBreak:
			w.tag('B')
		case HTMLInline:
			w.tag('h')
			w.str(n.S)
		}
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
