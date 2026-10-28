package emulator

// Limits that keep the memory of a terminal bounded (SPEC §10.25 "Memory").
const (
	// MaxCols and MaxRows are the largest size of a terminal; Resize and New clamp to them.
	MaxCols = 2000
	MaxRows = 2000
	// MaxLogicalLine is the longest line, in cells, that reflow joins out of wrapped rows; a longer one is cut into lines.
	MaxLogicalLine = 1 << 14
	// MaxScrollback is the largest scrollback in lines.
	MaxScrollback = 1_000_000
	// pageLines is how many lines of cells one allocation holds.
	pageLines = 64
	// maxClusterBytes is the longest text of one cell (a base character with its combining marks).
	maxClusterBytes = 64
)

// line is one row of a buffer. Its cells are cols wide.
type line struct {
	cells []Cell
	// ext holds the strings of the cells that are clusters; nil when the row has none.
	ext *lineExt
	// wrapped says that the text of this row goes on in the next one (autowrap), which reflow and serialization use.
	wrapped bool
	// dirty says that the row changed since the last snapshot.
	dirty bool
}

type lineExt struct {
	clusters []string
}

// pager hands out the cells of lines, a page of pageLines lines at a time.
type pager struct {
	cols int
	page []Cell
}

func (p *pager) alloc() []Cell {
	if len(p.page) < p.cols {
		p.page = make([]Cell, p.cols*pageLines)
	}
	c := p.page[:p.cols:p.cols]
	p.page = p.page[p.cols:]
	return c
}

// ring is the lines of a buffer, oldest first: the scrollback and then the screen. A line that leaves the top when the ring
// is full is reused for the line that enters at the bottom, so nothing is allocated and no cells move.
type ring struct {
	buf   []line
	head  int
	n     int
	max   int
	pages pager
	// trimmed counts the lines that left the top, so that a line keeps its absolute number (trimmed + index).
	trimmed int64
}

func newRing(cols, max int) *ring {
	return &ring{pages: pager{cols: cols}, max: max}
}

func (r *ring) at(i int) *line {
	j := r.head + i
	if j >= len(r.buf) {
		j -= len(r.buf)
	}
	return &r.buf[j]
}

// push adds an empty line at the bottom (dropping the top one when the ring is full) and returns it; its cells are
// cleared to the blank cell with the style bce.
func (r *ring) push(bce uint16) *line {
	var l *line
	switch {
	case r.n < len(r.buf):
		j := r.head + r.n
		if j >= len(r.buf) {
			j -= len(r.buf)
		}
		l = &r.buf[j]
		r.n++
	case len(r.buf) < r.max:
		r.buf = append(r.buf, line{})
		l = &r.buf[len(r.buf)-1]
		l.cells = r.pages.alloc()
		r.n++
	default:
		// Full: the oldest line becomes the newest.
		l = &r.buf[r.head]
		r.head++
		if r.head == len(r.buf) {
			r.head = 0
		}
		r.trimmed++
	}
	if bce == 0 {
		clear(l.cells)
	} else {
		fill := Cell{Style: bce}
		for i := range l.cells {
			l.cells[i] = fill
		}
	}
	l.ext = nil
	l.wrapped = false
	l.dirty = true
	return l
}

// popBack removes the newest line.
func (r *ring) popBack() { r.n-- }

// dropFront removes the n oldest lines.
func (r *ring) dropFront(n int) {
	r.head = (r.head + n) % max(len(r.buf), 1)
	r.n -= n
	r.trimmed += int64(n)
}

// normalize rotates the storage so that the oldest line is at index 0.
func (r *ring) normalize() {
	if r.head == 0 {
		return
	}
	nb := make([]line, len(r.buf))
	for i := range r.buf {
		j := r.head + i
		if j >= len(r.buf) {
			j -= len(r.buf)
		}
		nb[i] = r.buf[j]
	}
	r.buf = nb
	r.head = 0
}

// setMax changes the largest number of lines; lines beyond it are dropped from the top.
func (r *ring) setMax(m int) {
	if m == r.max {
		return
	}
	r.normalize()
	if r.n > m {
		r.dropFront(r.n - m)
		r.normalize()
	}
	r.max = m
}

// blank clears cells[from:to] of l to the blank cell with style bce, as an erase does (ctlseqs: background color erase).
func blank(l *line, from, to int, bce uint16) {
	c := l.cells[from:to]
	if bce == 0 {
		clear(c)
		return
	}
	fill := Cell{Style: bce}
	for i := range c {
		c[i] = fill
	}
}

// repairWide makes the wide characters of l[lo:hi] (and the cell on each side) whole again after an edit: a left half with
// no right half, or a right half with no left half, becomes a blank cell.
func repairWide(l *line, lo, hi int) {
	cells := l.cells
	if lo > 0 {
		lo--
	}
	if hi < len(cells) {
		hi++
	}
	for i := lo; i < hi; i++ {
		c := &cells[i]
		switch {
		case c.Flags&CellWide != 0:
			if i+1 >= len(cells) || cells[i+1].Flags&CellTail == 0 {
				*c = Cell{Style: c.Style}
			}
		case c.Flags&CellTail != 0:
			if i == 0 || cells[i-1].Flags&CellWide == 0 {
				*c = Cell{Style: c.Style}
			}
		}
	}
}

// clusterString returns the text of a cluster cell of l.
func (l *line) clusterString(c Cell) string {
	if l.ext == nil || int(c.Rune) >= len(l.ext.clusters) {
		return " "
	}
	return l.ext.clusters[c.Rune]
}

// addCluster stores s as a cluster of the row and returns its index.
func (l *line) addCluster(s string) uint32 {
	if l.ext == nil {
		l.ext = &lineExt{}
	}
	if len(l.ext.clusters) >= 2*len(l.cells)+16 {
		l.compactClusters()
		if l.ext == nil {
			l.ext = &lineExt{}
		}
	}
	l.ext.clusters = append(l.ext.clusters, s)
	return uint32(len(l.ext.clusters) - 1)
}

// compactClusters drops the strings no cell of the row uses any more.
func (l *line) compactClusters() {
	if l.ext == nil {
		return
	}
	old := l.ext.clusters
	var nw []string
	for i := range l.cells {
		c := &l.cells[i]
		if c.Flags&CellCluster != 0 && int(c.Rune) < len(old) {
			nw = append(nw, old[c.Rune])
			c.Rune = uint32(len(nw) - 1)
		}
	}
	if len(nw) == 0 {
		l.ext = nil
		return
	}
	l.ext.clusters = nw
}

// text appends the text of cells [from, to) of the row to dst: a blank cell is a space, a wide character's right half and
// the empty cell that ends a wrapped row are skipped.
func (l *line) text(dst []byte, from, to int) []byte {
	for i := from; i < to && i < len(l.cells); i++ {
		c := l.cells[i]
		switch {
		case c.Flags&(CellTail|CellWideFill) != 0:
		case c.Flags&CellCluster != 0:
			dst = append(dst, l.clusterString(c)...)
		case c.Rune == 0:
			dst = append(dst, ' ')
		default:
			dst = utf8AppendRune(dst, rune(c.Rune))
		}
	}
	return dst
}

// savedCursor is what DECSC saves (VT510 DECSC: the cursor, the character attributes, the character sets, the autowrap
// pending state and the origin mode).
type savedCursor struct {
	x, y     int
	pen      Style
	wrapNext bool
	origin   bool
	gl       int
	charsets [4]charset
	valid    bool
}

// screen is one of the two buffers of the terminal, the main one (with the scrollback) or the alternate one.
type screen struct {
	ring       *ring
	rows, cols int
	x, y       int
	// wrapNext is the pending wrap of the last column (VT510 DECAWM): the cursor is at the last column and the next
	// graphic character goes to the next row.
	wrapNext bool
	// top and bottom are the scroll region (DECSTBM), 0-based and inclusive.
	top, bottom int
	tabs        []bool
	saved       savedCursor
	alt         bool
	tmp         []line
}

func newScreen(rows, cols, scrollback int, alt bool) *screen {
	s := &screen{rows: rows, cols: cols, alt: alt, bottom: rows - 1}
	max := rows
	if !alt {
		max += scrollback
	}
	s.ring = newRing(cols, max)
	for i := 0; i < rows; i++ {
		s.ring.push(0)
	}
	s.tabs = defaultTabs(cols)
	return s
}

func defaultTabs(cols int) []bool {
	t := make([]bool, cols)
	for i := 8; i < cols; i += 8 {
		t[i] = true
	}
	return t
}

// row returns screen row y.
func (s *screen) row(y int) *line { return s.ring.at(s.ring.n - s.rows + y) }

// scrollbackLen is the number of lines above the screen.
func (s *screen) scrollbackLen() int { return s.ring.n - s.rows }

// scrollUp moves the lines of the scroll region up by n (ECMA-48 8.3.147 SU; VT510 IND); the lines that leave the top of
// the region are lost, or, when the region is the whole screen, join the scrollback. The new lines at the bottom get the
// style bce.
func (s *screen) scrollUp(n int, bce uint16) {
	h := s.bottom - s.top + 1
	if n > h {
		n = h
	}
	if n <= 0 {
		return
	}
	if s.top == 0 && s.bottom == s.rows-1 {
		for i := 0; i < n; i++ {
			s.ring.push(bce)
		}
		return
	}
	s.rotate(s.top, s.bottom, n, bce)
}

// scrollDown moves the lines of the scroll region down by n (ECMA-48 8.3.113 SD, 8.3.104 RI).
func (s *screen) scrollDown(n int, bce uint16) {
	h := s.bottom - s.top + 1
	if n > h {
		n = h
	}
	if n <= 0 {
		return
	}
	s.rotate(s.top, s.bottom, -n, bce)
}

// rotate moves rows top..bottom up by n (down when n is negative), clearing the rows that come in. It moves the headers of
// the lines, not their cells.
func (s *screen) rotate(top, bottom, n int, bce uint16) {
	h := bottom - top + 1
	k := n
	if k < 0 {
		k = -k
	}
	if cap(s.tmp) < k {
		s.tmp = make([]line, k)
	}
	tmp := s.tmp[:k]
	base := s.ring.n - s.rows
	if n > 0 {
		for i := 0; i < k; i++ {
			tmp[i] = *s.ring.at(base + top + i)
		}
		for i := 0; i < h-k; i++ {
			*s.ring.at(base + top + i) = *s.ring.at(base + top + i + k)
			s.ring.at(base + top + i).dirty = true
		}
		for i := 0; i < k; i++ {
			l := s.ring.at(base + bottom - k + 1 + i)
			*l = tmp[i]
			clearLine(l, bce)
		}
		return
	}
	for i := 0; i < k; i++ {
		tmp[i] = *s.ring.at(base + bottom - i)
	}
	for i := h - 1; i >= k; i-- {
		*s.ring.at(base + top + i) = *s.ring.at(base + top + i - k)
		s.ring.at(base + top + i).dirty = true
	}
	for i := 0; i < k; i++ {
		l := s.ring.at(base + top + i)
		*l = tmp[i]
		clearLine(l, bce)
	}
}

// clearLine blanks a whole line.
func clearLine(l *line, bce uint16) {
	blank(l, 0, len(l.cells), bce)
	l.ext = nil
	l.wrapped = false
	l.dirty = true
}

// absLine is the absolute number of screen row y: it counts the lines since the terminal began and does not change when the
// scrollback drops its oldest lines.
func (s *screen) absLine(y int) int64 { return s.ring.trimmed + int64(s.ring.n-s.rows+y) }
