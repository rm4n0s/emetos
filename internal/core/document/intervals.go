package document

import "sort"

// Stickiness says how a decoration's ends move when text is typed at them (VS
// Code's TrackedRangeStickiness; the numbers are VS Code's).
type Stickiness int

const (
	// AlwaysGrowsWhenTypingAtEdges grows when text is typed at either edge.
	AlwaysGrowsWhenTypingAtEdges Stickiness = iota
	// NeverGrowsWhenTypingAtEdges does not grow when text is typed at either edge.
	NeverGrowsWhenTypingAtEdges
	// GrowsOnlyWhenTypingBefore grows when text is typed at its start (before it).
	GrowsOnlyWhenTypingBefore
	// GrowsOnlyWhenTypingAfter grows when text is typed at its end (after it).
	GrowsOnlyWhenTypingAfter
)

type moveSemantics int

const (
	markerDefined moveSemantics = iota
	forceMove
	forceStay
)

// adjustMarkerBeforeColumn reports whether a marker at markerOffset stays before
// checkOffset (so that it is "done") when text is edited there: a marker before
// stays, one after does not, and one at it depends on the semantics and on
// whether it sticks to the previous character.
func adjustMarkerBeforeColumn(markerOffset int, sticksToPrevious bool, checkOffset int, sem moveSemantics) bool {
	switch {
	case markerOffset < checkOffset:
		return true
	case markerOffset > checkOffset:
		return false
	case sem == forceMove:
		return false
	case sem == forceStay:
		return true
	}
	return sticksToPrevious
}

// acceptEdit is where a decoration [start, end) ends up after the bytes
// [editStart, editEnd) are replaced by textLen bytes (VS Code's nodeAcceptEdit,
// "a lot more complicated than strictly necessary to maintain the same
// behaviour as when decorations were implemented using two markers"). With
// forceMoveMarkers both ends move after text inserted at them. With collapse, a
// decoration that the edit covers entirely collapses to the start of the edit.
func acceptEdit(start, end int, stickiness Stickiness, collapse bool, editStart, editEnd, textLen int, forceMoveMarkers bool) (int, int) {
	startSticks := stickiness == AlwaysGrowsWhenTypingAtEdges || stickiness == GrowsOnlyWhenTypingBefore
	endSticks := stickiness == NeverGrowsWhenTypingAtEdges || stickiness == GrowsOnlyWhenTypingBefore

	deleting, inserting := editEnd-editStart, textLen
	common := min(deleting, inserting)

	nodeStart, nodeEnd := start, end
	newStart, newEnd := start, end
	startDone, endDone := false, false

	if editStart <= nodeStart && nodeEnd <= editEnd && collapse {
		newStart, startDone = editStart, true
		newEnd, endDone = editStart, true
	}

	{
		sem := markerDefined
		if forceMoveMarkers {
			sem = forceMove
		} else if deleting > 0 {
			sem = forceStay
		}
		if !startDone && adjustMarkerBeforeColumn(nodeStart, startSticks, editStart, sem) {
			startDone = true
		}
		if !endDone && adjustMarkerBeforeColumn(nodeEnd, endSticks, editStart, sem) {
			endDone = true
		}
	}

	if common > 0 && !forceMoveMarkers {
		sem := markerDefined
		if deleting > inserting {
			sem = forceStay
		}
		if !startDone && adjustMarkerBeforeColumn(nodeStart, startSticks, editStart+common, sem) {
			startDone = true
		}
		if !endDone && adjustMarkerBeforeColumn(nodeEnd, endSticks, editStart+common, sem) {
			endDone = true
		}
	}

	{
		sem := markerDefined
		if forceMoveMarkers {
			sem = forceMove
		}
		if !startDone && adjustMarkerBeforeColumn(nodeStart, startSticks, editEnd, sem) {
			newStart = editStart + inserting
			startDone = true
		}
		if !endDone && adjustMarkerBeforeColumn(nodeEnd, endSticks, editEnd, sem) {
			newEnd = editStart + inserting
			endDone = true
		}
	}

	delta := inserting - deleting
	if !startDone {
		newStart = max(0, nodeStart+delta)
	}
	if !endDone {
		newEnd = max(0, nodeEnd+delta)
	}
	if newStart > newEnd {
		newEnd = newStart
	}
	return newStart, newEnd
}

// DecorationOptions are what a decoration means and how it moves. ClassName and
// Data are opaque to the document: the feature that adds a decoration and the
// view that draws it agree on them.
type DecorationOptions struct {
	Stickiness Stickiness
	// CollapseOnReplaceEdit makes the decoration collapse when an edit replaces
	// all of it, instead of moving to the end of the new text.
	CollapseOnReplaceEdit bool
	ClassName             string
	Data                  any
}

const (
	// blockSize is the number of decorations a block of the store holds; it is
	// split in two at twice that.
	blockSize = 64
)

// node is a decoration in the store. Its start and end are relative to the shift
// of its block, so that a whole block moves by changing one number.
type node struct {
	id, owner  int
	start, end int
	seq        uint64
	opts       DecorationOptions
	blk        *block
}

func (n *node) absStart() int { return n.start + n.blk.shift }
func (n *node) absEnd() int   { return n.end + n.blk.shift }

// block is a run of decorations in order of position.
type block struct {
	items  []*node
	shift  int
	maxEnd int // relative to shift
}

func (b *block) recomputeMaxEnd() {
	b.maxEnd = 0
	for i, n := range b.items {
		if i == 0 || n.end > b.maxEnd {
			b.maxEnd = n.end
		}
	}
}

// intervalStore keeps decorations sorted by (start, end, creation order) in
// blocks, each with the largest end it holds, so that a search skips whole
// blocks and an edit shifts whole blocks in one step. It plays the part of VS
// Code's augmented red-black interval tree with lazy deltas.
type intervalStore struct {
	blocks  []*block
	nextSeq uint64
}

// less orders keys.
func less(aStart, aEnd int, aSeq uint64, bStart, bEnd int, bSeq uint64) bool {
	if aStart != bStart {
		return aStart < bStart
	}
	if aEnd != bEnd {
		return aEnd < bEnd
	}
	return aSeq < bSeq
}

func (s *intervalStore) first(b *block) *node { return b.items[0] }

// blockFor is the index of the block a key belongs in: the last whose first key is not after it.
func (s *intervalStore) blockFor(start, end int, seq uint64) int {
	i := sort.Search(len(s.blocks), func(i int) bool {
		f := s.first(s.blocks[i])
		return less(start, end, seq, f.absStart(), f.absEnd(), f.seq)
	})
	return max(i-1, 0)
}

// insert puts a decoration with absolute offsets in the store. n.id, owner and
// opts must be set; the sequence number is assigned here.
func (s *intervalStore) insert(n *node, start, end int) {
	s.nextSeq++
	n.seq = s.nextSeq
	s.place(n, start, end)
}

// place puts a decoration that already has its sequence number in the store.
func (s *intervalStore) place(n *node, start, end int) {
	if len(s.blocks) == 0 {
		b := &block{items: []*node{n}, maxEnd: end}
		n.blk, n.start, n.end = b, start, end
		s.blocks = []*block{b}
		return
	}
	b := s.blocks[s.blockFor(start, end, n.seq)]
	n.blk = b
	n.start, n.end = start-b.shift, end-b.shift
	pos := sort.Search(len(b.items), func(i int) bool {
		o := b.items[i]
		return less(start, end, n.seq, o.absStart(), o.absEnd(), o.seq)
	})
	b.items = append(b.items, nil)
	copy(b.items[pos+1:], b.items[pos:])
	b.items[pos] = n
	if len(b.items) == 1 || n.end > b.maxEnd {
		b.maxEnd = n.end
	}
	if len(b.items) > 2*blockSize {
		s.split(b)
	}
}

func (s *intervalStore) split(b *block) {
	half := len(b.items) / 2
	right := &block{items: append([]*node(nil), b.items[half:]...), shift: b.shift}
	b.items = b.items[:half:half]
	for _, n := range right.items {
		n.blk = right
	}
	b.recomputeMaxEnd()
	right.recomputeMaxEnd()
	for i, x := range s.blocks {
		if x == b {
			s.blocks = append(s.blocks, nil)
			copy(s.blocks[i+2:], s.blocks[i+1:])
			s.blocks[i+1] = right
			return
		}
	}
}

// remove takes a decoration out of the store.
func (s *intervalStore) remove(n *node) {
	b := n.blk
	start, end := n.absStart(), n.absEnd()
	pos := sort.Search(len(b.items), func(i int) bool {
		o := b.items[i]
		return !less(o.absStart(), o.absEnd(), o.seq, start, end, n.seq)
	})
	if pos >= len(b.items) || b.items[pos] != n {
		return
	}
	b.items = append(b.items[:pos], b.items[pos+1:]...)
	n.blk = nil
	if len(b.items) == 0 {
		for i, x := range s.blocks {
			if x == b {
				s.blocks = append(s.blocks[:i], s.blocks[i+1:]...)
				break
			}
		}
		return
	}
	if n.end == b.maxEnd {
		b.recomputeMaxEnd()
	}
}

// search returns the decorations that overlap [start, end], ends included (a
// decoration that only touches it is a result), in order of position.
func (s *intervalStore) search(start, end int) []*node {
	var out []*node
	for _, b := range s.blocks {
		if s.first(b).absStart() > end {
			break
		}
		if b.maxEnd+b.shift < start {
			continue
		}
		for _, n := range b.items {
			ns := n.start + b.shift
			if ns > end {
				break
			}
			if n.end+b.shift >= start {
				out = append(out, n)
			}
		}
	}
	return out
}

// all returns every decoration in order of position.
func (s *intervalStore) all() []*node {
	var out []*node
	for _, b := range s.blocks {
		out = append(out, b.items...)
	}
	return out
}

// shiftAfter moves every decoration that starts after pos by delta.
func (s *intervalStore) shiftAfter(pos, delta int) {
	if delta == 0 {
		return
	}
	bi := sort.Search(len(s.blocks), func(i int) bool {
		b := s.blocks[i]
		return b.items[len(b.items)-1].absStart() > pos
	})
	if bi == len(s.blocks) {
		return
	}
	b := s.blocks[bi]
	from := sort.Search(len(b.items), func(i int) bool { return b.items[i].absStart() > pos })
	for _, n := range b.items[from:] {
		n.start += delta
		n.end += delta
	}
	b.recomputeMaxEnd()
	for _, later := range s.blocks[bi+1:] {
		later.shift += delta
	}
}

// applyEdit moves the decorations for the replacement of [editStart, editEnd) by
// textLen bytes. Those that overlap or touch the edit are adjusted one by one by
// their stickiness and put back in order; those after it move by the difference
// in length; those before it stay.
func (s *intervalStore) applyEdit(editStart, editEnd, textLen int, forceMoveMarkers bool) {
	type touched struct {
		n          *node
		start, end int
	}
	var affected []touched
	for _, n := range s.search(editStart, editEnd) {
		affected = append(affected, touched{n, n.absStart(), n.absEnd()})
	}
	for _, t := range affected {
		s.remove(t.n)
	}
	s.shiftAfter(editEnd, textLen-(editEnd-editStart))
	for _, t := range affected {
		ns, ne := acceptEdit(t.start, t.end, t.n.opts.Stickiness, t.n.opts.CollapseOnReplaceEdit, editStart, editEnd, textLen, forceMoveMarkers)
		s.place(t.n, ns, ne) // with its old sequence number: the creation order orders equal places
	}
}
