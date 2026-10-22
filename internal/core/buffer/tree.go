package buffer

import "sort"

// node is a node of the piece tree, an AVL tree ordered by position in the text.
// A node is never changed after it is made: an edit builds new nodes along the
// path it touches and shares the rest, so a root is an immutable snapshot of the
// text and costs nothing to keep. There are no parent pointers.
type node struct {
	left, right *node
	p           piece
	height      int
	// size and lf are the bytes and line feeds of the whole subtree.
	size int
	lf   int
}

func height(n *node) int {
	if n == nil {
		return 0
	}
	return n.height
}

func size(n *node) int {
	if n == nil {
		return 0
	}
	return n.size
}

func lineFeeds(n *node) int {
	if n == nil {
		return 0
	}
	return n.lf
}

// mk makes a node. It does not balance.
func mk(l *node, p piece, r *node) *node {
	return &node{
		left: l, right: r, p: p,
		height: 1 + max(height(l), height(r)),
		size:   size(l) + p.len() + size(r),
		lf:     lineFeeds(l) + p.lf() + lineFeeds(r),
	}
}

func rotateLeft(n *node) *node {
	r := n.right
	return mk(mk(n.left, n.p, r.left), r.p, r.right)
}

func rotateRight(n *node) *node {
	l := n.left
	return mk(l.left, l.p, mk(l.right, n.p, n.right))
}

// join makes a balanced tree of l, then p, then r, where l and r are balanced
// trees. It takes O(|height(l)-height(r)|) steps.
func join(l *node, p piece, r *node) *node {
	switch {
	case height(l) > height(r)+1:
		return joinRight(l, p, r)
	case height(r) > height(l)+1:
		return joinLeft(l, p, r)
	}
	return mk(l, p, r)
}

// joinRight is join for a left tree more than one level taller than the right.
func joinRight(l *node, p piece, r *node) *node {
	c := l.right
	if height(c) <= height(r)+1 {
		t := mk(c, p, r)
		if t.height <= height(l.left)+1 {
			return mk(l.left, l.p, t)
		}
		return rotateLeft(mk(l.left, l.p, rotateRight(t)))
	}
	t := joinRight(c, p, r)
	res := mk(l.left, l.p, t)
	if t.height <= height(l.left)+1 {
		return res
	}
	return rotateLeft(res)
}

// joinLeft is join for a right tree more than one level taller than the left.
func joinLeft(l *node, p piece, r *node) *node {
	c := r.left
	if height(c) <= height(l)+1 {
		t := mk(l, p, c)
		if t.height <= height(r.right)+1 {
			return mk(t, r.p, r.right)
		}
		return rotateRight(mk(rotateLeft(t), r.p, r.right))
	}
	t := joinLeft(l, p, c)
	res := mk(t, r.p, r.right)
	if t.height <= height(r.right)+1 {
		return res
	}
	return rotateRight(res)
}

// join2 makes a balanced tree of l followed by r.
func join2(l, r *node) *node {
	if l == nil {
		return r
	}
	if r == nil {
		return l
	}
	rest, last := removeLast(l)
	return join(rest, last, r)
}

// removeLast takes the last piece off a non-empty tree.
func removeLast(n *node) (*node, piece) {
	if n.right == nil {
		return n.left, n.p
	}
	rest, last := removeLast(n.right)
	return join(n.left, n.p, rest), last
}

// lastPiece is the last piece of a tree.
func lastPiece(n *node) (piece, bool) {
	if n == nil {
		return piece{}, false
	}
	for n.right != nil {
		n = n.right
	}
	return n.p, true
}

// replaceLast replaces the last piece of a non-empty tree by one of the same
// shape in the tree (the height does not change).
func replaceLast(n *node, p piece) *node {
	if n.right == nil {
		return mk(n.left, p, nil)
	}
	return mk(n.left, n.p, replaceLast(n.right, p))
}

// split cuts a tree at off bytes: the first result holds the first off bytes
// and the second the rest. A piece that off falls inside is cut in two.
func split(n *node, off int) (*node, *node) {
	switch {
	case n == nil:
		return nil, nil
	case off <= 0:
		return nil, n
	case off >= n.size:
		return n, nil
	}
	ls := size(n.left)
	pl := n.p.len()
	switch {
	case off <= ls:
		ll, lr := split(n.left, off)
		return ll, join(lr, n.p, n.right)
	case off >= ls+pl:
		rl, rr := split(n.right, off-ls-pl)
		return join(n.left, n.p, rl), rr
	}
	a, b := n.p.splitAt(off - ls)
	return join(n.left, a, nil), join(nil, b, n.right)
}

// fromPieces builds a balanced tree of the pieces, in order.
func fromPieces(ps []piece) *node {
	if len(ps) == 0 {
		return nil
	}
	mid := len(ps) / 2
	return mk(fromPieces(ps[:mid]), ps[mid], fromPieces(ps[mid+1:]))
}

// appendRange appends the bytes [start, end) of the subtree to dst; the range
// has already been checked to lie inside the subtree.
func (n *node) appendRange(dst []byte, start, end int) []byte {
	if n == nil || start >= end {
		return dst
	}
	ls := size(n.left)
	if start < ls {
		dst = n.left.appendRange(dst, start, min(end, ls))
	}
	pl := n.p.len()
	lo, hi := max(start-ls, 0), min(end-ls, pl)
	if lo < hi {
		dst = append(dst, n.p.c.data[n.p.start+lo:n.p.start+hi]...)
	}
	if rs := ls + pl; end > rs {
		dst = n.right.appendRange(dst, max(start-rs, 0), end-rs)
	}
	return dst
}

// afterLineFeed is the offset just after the k-th line feed of the subtree,
// 1 <= k <= lineFeeds(n). It takes O(log n) steps: the counts in the nodes pick
// the way down, and the chunk's list of line feeds gives the place in a piece.
func (n *node) afterLineFeed(k int) int {
	off := 0
	for n != nil {
		ll := lineFeeds(n.left)
		switch {
		case k <= ll:
			n = n.left
		case k <= ll+n.p.lf():
			feed := int(n.p.c.feeds[n.p.fStart+k-ll-1])
			return off + size(n.left) + feed - n.p.start + 1
		default:
			k -= ll + n.p.lf()
			off += size(n.left) + n.p.len()
			n = n.right
		}
	}
	return off
}

// lineFeedsBefore is the number of line feeds in the first off bytes of the
// subtree, in O(log n).
func (n *node) lineFeedsBefore(off int) int {
	count := 0
	for n != nil && off > 0 {
		ls := size(n.left)
		if off <= ls {
			n = n.left
			continue
		}
		count += lineFeeds(n.left)
		off -= ls
		if pl := n.p.len(); off < pl {
			at := n.p.start + off
			return count + sort.Search(n.p.lf(), func(i int) bool {
				return int(n.p.c.feeds[n.p.fStart+i]) >= at
			})
		}
		count += n.p.lf()
		off -= n.p.len()
		n = n.right
	}
	return count
}

// byteAt is the byte at off, 0 <= off < size(n).
func (n *node) byteAt(off int) byte {
	for {
		ls := size(n.left)
		switch {
		case off < ls:
			n = n.left
		case off < ls+n.p.len():
			return n.p.c.data[n.p.start+off-ls]
		default:
			off -= ls + n.p.len()
			n = n.right
		}
	}
}

// walk calls yield with the text of each piece, cut to the bytes [start, end)
// of the subtree, in order, until yield returns false. It reports whether it
// went through all of them. The slices belong to the buffer and must not be
// changed or kept.
func (n *node) walk(start, end int, yield func([]byte) bool) bool {
	if n == nil || start >= end {
		return true
	}
	ls := size(n.left)
	if start < ls && !n.left.walk(start, min(end, ls), yield) {
		return false
	}
	pl := n.p.len()
	if lo, hi := max(start-ls, 0), min(end-ls, pl); lo < hi && !yield(n.p.c.data[n.p.start+lo:n.p.start+hi]) {
		return false
	}
	if rs := ls + pl; end > rs {
		return n.right.walk(max(start-rs, 0), end-rs, yield)
	}
	return true
}
