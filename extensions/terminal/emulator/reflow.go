package emulator

// resize changes the size of both buffers (the lock is held). The main buffer reflows its wrapped lines when the width
// changes (the rows that autowrap made are joined into the line the program wrote and cut again at the new width); the
// alternate buffer, which a full-screen program redraws, is cut or extended.
func (e *Emulator) resize(rows, cols int) {
	m := e.mainScr
	if cols != m.cols {
		e.reflowMain(rows, cols)
	} else {
		e.resizeRows(m, rows)
	}
	e.resizeAlt(rows, cols)
	e.rows, e.cols = rows, cols
	e.mouseSeen = false
}

// resizeRows changes only the number of rows of the main buffer. A taller screen shows lines of the scrollback again; a
// shorter one first loses the blank rows below the cursor, then pushes its top rows into the scrollback.
func (e *Emulator) resizeRows(s *screen, rows int) {
	if rows > s.rows {
		d := rows - s.rows
		s.ring.setMax(rows + e.scrollback)
		k := min(d, s.scrollbackLen())
		s.y += k
		for i := 0; i < d-k; i++ {
			s.ring.push(0)
		}
	} else {
		d := s.rows - rows
		for d > 0 && s.rows-1 > s.y && lineBlank(s.row(s.rows-1)) {
			s.ring.popBack()
			s.rows--
			d--
		}
		shift := min(d, s.y)
		for i := 0; i < d-shift; i++ {
			s.ring.popBack()
		}
		s.y -= shift
		s.ring.setMax(rows + e.scrollback)
	}
	e.marks.dropFrom(s.ring.trimmed + int64(s.ring.n))
	s.rows = rows
	s.top, s.bottom = 0, rows-1
	s.y = min(max(s.y, 0), rows-1)
	s.saved.valid = s.saved.valid && s.saved.y < rows
	if s.saved.y >= rows {
		s.saved.y = rows - 1
	}
}

func lineBlank(l *line) bool {
	for _, c := range l.cells {
		if c.Rune != 0 || c.Style != 0 {
			return false
		}
	}
	return true
}

// reflowMain rebuilds the main buffer at a new width. The lines that autowrap joined are laid out again; the cursor keeps
// its place in the text.
func (e *Emulator) reflowMain(rows, cols int) {
	s := e.mainScr
	old := s.ring
	total := old.n
	curIdx := total - s.rows + s.y
	// The rows below the last one with text or the cursor are blank and are not carried over.
	last := total - 1
	for last > curIdx && lineBlank(old.at(last)) {
		last--
	}
	nr := newRing(cols, rows+e.scrollback)
	nr.trimmed = old.trimmed

	var stream []Cell
	var clus []string
	curRow, curCol := -1, 0
	curWrap := false
	// The markers follow their rows: a marker on row i of a logical line goes to the new row that holds the cell at the
	// offset where row i began in the stream (the end of the line if there is none).
	oldMarks := e.marks.list
	newMarks := make([]marker, 0, len(oldMarks))
	mi := 0
	for mi < len(oldMarks) && oldMarks[mi].line < old.trimmed {
		mi++
	}
	type pendingMark struct {
		id  MarkerID
		off int
	}
	var pend []pendingMark
	i := 0
	for i <= last {
		// Gather the logical line that starts at row i.
		stream = stream[:0]
		clus = clus[:0]
		curOff := -1
		pend = pend[:0]
		for {
			l := old.at(i)
			for mi < len(oldMarks) && oldMarks[mi].line-old.trimmed <= int64(i) {
				if oldMarks[mi].line-old.trimmed == int64(i) {
					pend = append(pend, pendingMark{oldMarks[mi].id, len(stream)})
				}
				mi++
			}
			end := len(l.cells)
			if !l.wrapped || i == last {
				for end > 0 && l.cells[end-1].isPlainBlank() {
					end--
				}
			}
			if i == curIdx {
				x := s.x
				if s.wrapNext {
					x++
				}
				// Offset of the cursor in the stream: the cells before its column that are kept.
				off := len(stream)
				for k := 0; k < min(x, len(l.cells)); k++ {
					if l.cells[k].Flags&(CellTail|CellWideFill) == 0 {
						off++
					}
				}
				curOff = off
				if x > end {
					end = min(x, len(l.cells))
				}
			}
			for k := 0; k < end; k++ {
				c := l.cells[k]
				if c.Flags&(CellTail|CellWideFill) != 0 {
					continue
				}
				if c.Flags&CellCluster != 0 {
					clus = append(clus, l.clusterString(c))
					c.Rune = uint32(len(clus) - 1)
				}
				stream = append(stream, c)
			}
			more := l.wrapped && i < last && len(stream) < MaxLogicalLine
			i++
			if !more {
				break
			}
		}
		// Lay it out at the new width.
		out := nr.push(0)
		outIdx := nr.n - 1 + int(nr.trimmed-old.trimmed)
		firstOut := outIdx
		x := 0
		place := func(k int) (row, col int) {
			c := stream[k]
			w := 1
			if c.Flags&CellWide != 0 {
				w = 2
			}
			if w > cols {
				return outIdx, x
			}
			if x+w > cols {
				if w == 2 && x == cols-1 {
					out.cells[x] = Cell{Flags: CellWideFill}
				}
				out.wrapped = true
				out = nr.push(0)
				outIdx++
				x = 0
			}
			if c.Flags&CellCluster != 0 {
				c.Rune = out.addCluster(clus[c.Rune])
			}
			out.cells[x] = c
			if w == 2 {
				out.cells[x+1] = Cell{Style: c.Style, Flags: CellTail}
			}
			row, col = outIdx, x
			x += w
			return row, col
		}
		pi := 0
		for k := range stream {
			row, col := place(k)
			if k == curOff {
				curRow, curCol, curWrap = row, col, false
			}
			for pi < len(pend) && pend[pi].off <= k {
				newMarks = append(newMarks, marker{pend[pi].id, old.trimmed + int64(row)})
				pi++
			}
		}
		for ; pi < len(pend); pi++ {
			row := outIdx
			if len(stream) == 0 {
				row = firstOut
			}
			newMarks = append(newMarks, marker{pend[pi].id, old.trimmed + int64(row)})
		}
		if curOff >= 0 && curOff >= len(stream) {
			curRow, curCol, curWrap = outIdx, x, false
			if x >= cols {
				curCol, curWrap = cols-1, true
			}
		}
	}
	if curRow < 0 {
		// The cursor row was not carried over (cannot happen: last >= curIdx), put it at the end.
		curRow, curCol = nr.n-1, 0
	}
	// curRow counts rows from the first one pushed; convert to a ring index.
	curRow -= int(nr.trimmed - old.trimmed)
	for nr.n < rows {
		nr.push(0)
	}
	// The cursor must be on the screen: content below it that does not fit is cut.
	for nr.n-rows > curRow && nr.n > rows {
		nr.popBack()
	}
	s.ring = nr
	e.marks.list = newMarks
	e.marks.dropBefore(nr.trimmed)
	e.marks.dropFrom(nr.trimmed + int64(nr.n))
	s.cols = cols
	s.rows = rows
	s.y = min(max(curRow-(nr.n-rows), 0), rows-1)
	s.x = min(max(curCol, 0), cols-1)
	s.wrapNext = curWrap && e.modes.Autowrap
	s.top, s.bottom = 0, rows-1
	s.tabs = resizeTabs(s.tabs, cols)
	s.saved.x = min(s.saved.x, cols-1)
	s.saved.y = min(s.saved.y, rows-1)
	s.tmp = nil
}

func resizeTabs(old []bool, cols int) []bool {
	t := defaultTabs(cols)
	if len(old) < cols {
		copy(t, old)
		// The part that was not there keeps the default stops.
		for i := len(old); i < cols; i++ {
			t[i] = i%8 == 0 && i > 0
		}
		return t
	}
	copy(t, old[:cols])
	return t
}

// resizeAlt cuts or extends the alternate buffer: the top left part of the screen stays.
func (e *Emulator) resizeAlt(rows, cols int) {
	a := e.altScr
	n := newScreen(rows, cols, 0, true)
	for y := 0; y < min(rows, a.rows); y++ {
		src, dst := a.row(y), n.row(y)
		copy(dst.cells, src.cells)
		if cols < a.cols {
			repairWide(dst, cols-1, cols)
		}
		dst.ext = src.ext
		dst.dirty = true
	}
	n.x, n.y = min(a.x, cols-1), min(a.y, rows-1)
	n.wrapNext = a.wrapNext && a.x == cols-1
	n.tabs = resizeTabs(a.tabs, cols)
	n.saved = a.saved
	n.saved.x, n.saved.y = min(n.saved.x, cols-1), min(n.saved.y, rows-1)
	e.altScr = n
	if e.scr == a {
		e.scr = n
	} else {
		e.scr = e.mainScr
	}
}
