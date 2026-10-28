package emulator

import "unicode/utf8"

func utf8AppendRune(dst []byte, r rune) []byte { return utf8.AppendRune(dst, r) }

// printText is the fast path of the ground state: it writes the printable characters at the start of p to the cells
// without a state machine step per byte (SPEC §10.25 "Speed"). Runs of ASCII are copied into the row; UTF-8 text is decoded
// in place. It stops at a control byte, at a character that needs the general path (a combining mark, a wide character
// that does not fit, a C1 control, an invalid or cut sequence) and returns the number of bytes it consumed.
func (e *Emulator) printText(p []byte) int {
	s := e.scr
	cols := s.cols
	n := 0
	for n < len(p) {
		if b := p[n]; b < 0x20 || b == 0x7f {
			break
		}
		if s.wrapNext {
			// A zero-width character or an invalid sequence does not wrap: the general path decides.
			if !spacingAhead(p[n:]) {
				return n
			}
			e.wrapLine()
		}
		l := s.row(s.y)
		cells := l.cells
		x := s.x
		start := x
		style := e.curStyle()
		stop := false
	run:
		for n < len(p) {
			// A run of printable ASCII, found by a scan and then written to the cells in one loop.
			if room := cols - x; room > 0 {
				src := p[n:]
				if len(src) > room {
					src = src[:room]
				}
				j := 0
				for j < len(src) && src[j]-0x20 < 0x5f {
					j++
				}
				if j > 0 {
					dst := cells[x : x+j]
					src = src[:len(dst)]
					for i := range dst {
						dst[i] = Cell{Rune: uint32(src[i]), Style: style}
					}
					x += j
					n += j
					if n >= len(p) {
						break run
					}
				}
			}
			b := p[n]
			if b < 0x80 {
				if b >= 0x20 && b != 0x7f {
					break run // the row is full
				}
				stop = true
				break run
			}
			if b < 0xc2 || !utf8.FullRune(p[n:]) {
				stop = true
				break run
			}
			r, sz := utf8.DecodeRune(p[n:])
			if r == utf8.RuneError && sz == 1 {
				stop = true
				break run
			}
			w := 1
			if r < 0xa0 { // a C1 control
				stop = true
				break run
			}
			if r >= 0x300 {
				w = runeWidth(r)
			}
			switch w {
			case 1:
				if x >= cols {
					break run
				}
				cells[x] = Cell{Rune: uint32(r), Style: style}
				x++
			case 2:
				if x+2 > cols {
					stop = true
					break run
				}
				cells[x] = Cell{Rune: uint32(r), Style: style, Flags: CellWide}
				cells[x+1] = Cell{Style: style, Flags: CellTail}
				x += 2
			default:
				stop = true
				break run
			}
			n += sz
		}
		progressed := x > start
		if progressed {
			// The run may have overwritten one half of a wide character: the other half is blanked.
			if start > 0 && cells[start-1].Flags&CellWide != 0 {
				cells[start-1] = Cell{Style: cells[start-1].Style}
			}
			if x < cols && cells[x].Flags&CellTail != 0 {
				cells[x] = Cell{Style: cells[x].Style}
			}
			l.dirty = true
			e.changed = true
			e.lastRune = rune(cells[x-1].Rune)
			if cells[x-1].Flags&CellTail != 0 {
				e.lastRune = rune(cells[x-2].Rune)
			}
			if x >= cols {
				x = cols - 1
				if e.modes.Autowrap {
					s.wrapNext = true
				}
			}
			s.x = x
		}
		if stop || !progressed {
			return n
		}
	}
	return n
}

// spacingAhead reports whether the text at the start of p begins with a character that takes a cell.
func spacingAhead(p []byte) bool {
	b := p[0]
	if b < 0x80 {
		return true
	}
	if b < 0xc2 || !utf8.FullRune(p) {
		return false
	}
	r, sz := utf8.DecodeRune(p)
	return (r != utf8.RuneError || sz != 1) && r >= 0xa0 && runeWidth(r) > 0
}

// The cell of the first column of a run that overwrites the right half of a wide character leaves the left half alone in
// the row; printText above and repairWide below put a blank in its place.

// printRune prints one graphic character (ECMA-48 8.2.1 and 8.3: the graphic characters; VT510: autowrap DECAWM; ctlseqs:
// "DECSET 7"). It is the general path for what printText does not handle.
func (e *Emulator) printRune(r rune) {
	if r < 0x80 {
		switch e.charsets[e.gl] {
		case csDECGraphics: // VT100 user guide: the DEC Special Graphics set
			if r >= 0x2b && r <= 0x7e && decGraphics[r-0x2b] != 0 {
				r = decGraphics[r-0x2b]
			}
		case csUK:
			if r == '#' {
				r = '£'
			}
		}
	}
	w := runeWidth(r)
	if w == 0 {
		e.attachMark(r)
		return
	}
	s := e.scr
	if w > s.cols {
		return
	}
	e.lastRune = r
	e.changed = true
	if s.wrapNext {
		e.wrapLine()
	}
	if w == 2 && s.x == s.cols-1 {
		if !e.modes.Autowrap {
			return
		}
		l := s.row(s.y)
		l.cells[s.x] = Cell{Flags: CellWideFill}
		repairWide(l, s.x, s.x+1)
		l.dirty = true
		e.wrapLine()
	}
	l := s.row(s.y)
	x := s.x
	style := e.curStyle()
	if e.modes.Insert {
		// ECMA-48 8.3.125 SM, mode IRM (4): the characters to the right move right and the ones that pass the edge are lost.
		copy(l.cells[x+w:], l.cells[x:s.cols-w])
		repairWide(l, x, s.cols)
	}
	if w == 2 {
		l.cells[x] = Cell{Rune: uint32(r), Style: style, Flags: CellWide}
		l.cells[x+1] = Cell{Style: style, Flags: CellTail}
	} else {
		l.cells[x] = Cell{Rune: uint32(r), Style: style}
	}
	repairWide(l, x, x+w)
	l.dirty = true
	x += w
	if x >= s.cols {
		x = s.cols - 1
		if e.modes.Autowrap {
			s.wrapNext = true
		}
	}
	s.x = x
}

// attachMark adds a zero-width character (a combining mark, a joiner, a variation selector) to the character before the
// cursor, making that cell a cluster (Unicode: grapheme extension; the wcwidth model of width.go).
func (e *Emulator) attachMark(r rune) {
	s := e.scr
	x := s.x
	if !s.wrapNext {
		x--
	}
	if x < 0 {
		return
	}
	l := s.row(s.y)
	if l.cells[x].Flags&CellTail != 0 && x > 0 {
		x--
	}
	c := &l.cells[x]
	if c.Flags&CellWideFill != 0 {
		return
	}
	var base string
	switch {
	case c.Flags&CellCluster != 0:
		base = l.clusterString(*c)
	case c.Rune == 0:
		base = " "
	default:
		base = string(rune(c.Rune))
	}
	if len(base)+utf8.RuneLen(r) > maxClusterBytes {
		return
	}
	idx := l.addCluster(base + string(r))
	c.Rune = idx
	c.Flags |= CellCluster
	l.dirty = true
	e.changed = true
}

// wrapLine moves to the start of the next row because the text reached the right edge (VT510 DECAWM).
func (e *Emulator) wrapLine() {
	s := e.scr
	s.row(s.y).wrapped = true
	s.x = 0
	s.wrapNext = false
	e.index()
}

// index is IND: the cursor moves down one row, and the scroll region scrolls up when it is on the bottom row of the region
// (VT510 IND; ctlseqs: ESC D).
func (e *Emulator) index() {
	s := e.scr
	switch {
	case s.y == s.bottom:
		s.scrollUp(1, e.bce())
		e.allDirty, e.changed = true, true
	case s.y < s.rows-1:
		s.y++
	}
}

// reverseIndex is RI: the cursor moves up one row, and the scroll region scrolls down when it is on the top row of the
// region (ECMA-48 8.3.104).
func (e *Emulator) reverseIndex() {
	s := e.scr
	s.wrapNext = false
	switch {
	case s.y == s.top:
		s.scrollDown(1, e.bce())
		e.allDirty, e.changed = true, true
	case s.y > 0:
		s.y--
	}
}

// nextLine is NEL: IND and CR (ECMA-48 8.3.86).
func (e *Emulator) nextLine() {
	s := e.scr
	s.x = 0
	s.wrapNext = false
	e.index()
}

// execute runs a C0 control (ECMA-48 5.3 and 8.3).
func (e *Emulator) execute(b byte) {
	s := e.scr
	switch b {
	case 0x07: // BEL, ECMA-48 8.3.3
		e.events = append(e.events, event{kind: evBell})
	case 0x08: // BS, ECMA-48 8.3.5 (ctlseqs DECSET 45: reverse wrap-around)
		s.wrapNext = false
		switch {
		case s.x > 0:
			s.x--
		case e.modes.ReverseWrap && s.y > 0 && s.row(s.y-1).wrapped:
			s.y--
			s.x = s.cols - 1
		}
	case 0x09: // HT, ECMA-48 8.3.60
		s.wrapNext = false
		s.x = e.nextTab(s.x, 1)
	case 0x0a, 0x0b, 0x0c: // LF, VT, FF: ECMA-48 8.3.74 (with LNM, SM 20, they also do CR)
		s.wrapNext = false
		if e.modes.NewLine {
			s.x = 0
		}
		e.index()
	case 0x0d: // CR, ECMA-48 8.3.15
		s.wrapNext = false
		s.x = 0
	case 0x0e: // SO (LS1), ECMA-48 8.3.126 (LS1, 8.3.76): G1 into GL
		e.gl = 1
		e.updateSlow()
	case 0x0f: // SI (LS0), ECMA-48 8.3.119 (LS0, 8.3.75): G0 into GL
		e.gl = 0
		e.updateSlow()
	case 0x05: // ENQ: the answerback of Emetos is empty
	}
	e.changed = true
}

func (e *Emulator) updateSlow() { e.slow = e.modes.Insert || e.charsets[e.gl] != csASCII }

// nextTab returns the column of the n-th tab stop after x, or the last column (ECMA-48 8.3.60 HT, 8.3.10 CHT).
func (e *Emulator) nextTab(x, n int) int {
	s := e.scr
	for ; n > 0; n-- {
		x++
		for x < s.cols-1 && !s.tabs[x] {
			x++
		}
		if x >= s.cols-1 {
			return s.cols - 1
		}
	}
	return x
}

// prevTab returns the column of the n-th tab stop before x, or column 0 (ECMA-48 8.3.7 CBT).
func (e *Emulator) prevTab(x, n int) int {
	s := e.scr
	for ; n > 0; n-- {
		x--
		for x > 0 && !s.tabs[x] {
			x--
		}
		if x <= 0 {
			return 0
		}
	}
	return x
}

// setTab sets a tab stop at the cursor (ECMA-48 8.3.62 HTS).
func (e *Emulator) setTab() { e.scr.tabs[e.scr.x] = true }
