package emulator

// The cursor and editing functions. Each one names its definition: ECMA-48 (section number), the VT510 manual or ctlseqs.

// clampY returns the lowest and highest row the cursor may move to by relative moves from row y: inside the scroll region it
// stops at the margins, outside it stops at the screen edges (VT510 DECSTBM; ctlseqs: CUU, CUD).
func (s *screen) clampUp(y int) int {
	if y >= s.top {
		return s.top
	}
	return 0
}

func (s *screen) clampDown(y int) int {
	if y <= s.bottom {
		return s.bottom
	}
	return s.rows - 1
}

// cursorUp is CUU (ECMA-48 8.3.22).
func (e *Emulator) cursorUp(n int) {
	s := e.scr
	s.wrapNext = false
	s.y = max(s.y-n, s.clampUp(s.y))
}

// cursorDown is CUD (ECMA-48 8.3.19).
func (e *Emulator) cursorDown(n int) {
	s := e.scr
	s.wrapNext = false
	s.y = min(s.y+n, s.clampDown(s.y))
}

// cursorForward is CUF (ECMA-48 8.3.20).
func (e *Emulator) cursorForward(n int) {
	s := e.scr
	s.wrapNext = false
	s.x = min(s.x+n, s.cols-1)
}

// cursorBack is CUB (ECMA-48 8.3.18).
func (e *Emulator) cursorBack(n int) {
	s := e.scr
	s.wrapNext = false
	s.x = max(s.x-n, 0)
}

// moveTo sets the cursor to row, col (0-based); with the origin mode (VT510 DECOM) the row is relative to the top margin and
// the cursor stays inside the region.
func (e *Emulator) moveTo(row, col int) {
	s := e.scr
	s.wrapNext = false
	if e.modes.Origin {
		row = min(row+s.top, s.bottom)
	}
	s.y = min(max(row, 0), s.rows-1)
	s.x = min(max(col, 0), s.cols-1)
}

// eraseCells blanks cells [from, to) of row y with the background color erase style.
func (e *Emulator) eraseCells(y, from, to int) {
	s := e.scr
	l := s.row(y)
	from, to = max(from, 0), min(to, s.cols)
	if from >= to {
		return
	}
	blank(l, from, to, e.bce())
	repairWide(l, from, to)
	if to == s.cols {
		l.wrapped = false
	}
	l.dirty = true
	e.changed = true
}

// eraseInLine is EL (ECMA-48 8.3.41; ctlseqs: CSI Ps K).
func (e *Emulator) eraseInLine(mode int) {
	s := e.scr
	switch mode {
	case 0:
		e.eraseCells(s.y, s.x, s.cols)
	case 1:
		e.eraseCells(s.y, 0, s.x+1)
	case 2:
		e.eraseCells(s.y, 0, s.cols)
	}
}

// eraseInDisplay is ED (ECMA-48 8.3.39; ctlseqs: CSI Ps J). Mode 3 deletes the scrollback (ctlseqs: "Erase Saved Lines").
func (e *Emulator) eraseInDisplay(mode int) {
	s := e.scr
	switch mode {
	case 0:
		e.eraseCells(s.y, s.x, s.cols)
		for y := s.y + 1; y < s.rows; y++ {
			e.eraseCells(y, 0, s.cols)
		}
	case 1:
		for y := 0; y < s.y; y++ {
			e.eraseCells(y, 0, s.cols)
		}
		e.eraseCells(s.y, 0, s.x+1)
	case 2:
		for y := 0; y < s.rows; y++ {
			e.eraseCells(y, 0, s.cols)
		}
	case 3:
		if sb := s.scrollbackLen(); sb > 0 {
			s.ring.dropFront(sb)
			s.ring.normalize()
			e.allDirty, e.changed = true, true
		}
	}
}

// insertLines is IL (ECMA-48 8.3.67): it has no effect outside the scroll region. The cursor goes to the first column
// (VT510 IL; DEC STD 070 says the active position moves to the left margin).
func (e *Emulator) insertLines(n int) {
	s := e.scr
	if s.y < s.top || s.y > s.bottom {
		return
	}
	n = min(n, s.bottom-s.y+1)
	s.rotate(s.y, s.bottom, -n, e.bce())
	s.x = 0
	s.wrapNext = false
	e.changed = true
}

// deleteLines is DL (ECMA-48 8.3.32).
func (e *Emulator) deleteLines(n int) {
	s := e.scr
	if s.y < s.top || s.y > s.bottom {
		return
	}
	n = min(n, s.bottom-s.y+1)
	s.rotate(s.y, s.bottom, n, e.bce())
	s.x = 0
	s.wrapNext = false
	e.changed = true
}

// insertChars is ICH (ECMA-48 8.3.64): the characters from the cursor move right, the ones that pass the edge are lost.
func (e *Emulator) insertChars(n int) {
	s := e.scr
	l := s.row(s.y)
	n = min(n, s.cols-s.x)
	copy(l.cells[s.x+n:], l.cells[s.x:s.cols-n])
	blank(l, s.x, s.x+n, e.bce())
	repairWide(l, s.x, s.cols)
	l.dirty = true
	s.wrapNext = false
	e.changed = true
}

// deleteChars is DCH (ECMA-48 8.3.26): the characters to the right of the cursor move left.
func (e *Emulator) deleteChars(n int) {
	s := e.scr
	l := s.row(s.y)
	n = min(n, s.cols-s.x)
	copy(l.cells[s.x:], l.cells[s.x+n:])
	blank(l, s.cols-n, s.cols, e.bce())
	repairWide(l, s.x, s.cols)
	l.dirty = true
	s.wrapNext = false
	e.changed = true
}

// eraseChars is ECH (ECMA-48 8.3.38): n characters from the cursor are blanked.
func (e *Emulator) eraseChars(n int) {
	s := e.scr
	e.eraseCells(s.y, s.x, s.x+n)
	s.wrapNext = false
}

// setScrollRegion is DECSTBM (VT510 DECSTBM; ctlseqs: CSI Ps ; Ps r): top and bottom are 1-based; a region of fewer than two
// rows is refused. The cursor goes home.
func (e *Emulator) setScrollRegion(top, bottom int) {
	s := e.scr
	if bottom <= 0 || bottom > s.rows {
		bottom = s.rows
	}
	if top <= 0 {
		top = 1
	}
	if top >= bottom {
		return
	}
	s.top, s.bottom = top-1, bottom-1
	e.moveTo(0, 0)
}

// saveCursor is DECSC (VT510 DECSC; ECMA-48 has no equivalent; ctlseqs: ESC 7).
func (e *Emulator) saveCursor() {
	s := e.scr
	s.saved = savedCursor{x: s.x, y: s.y, pen: e.pen, wrapNext: s.wrapNext, origin: e.modes.Origin, gl: e.gl, charsets: e.charsets, valid: true}
}

// restoreCursor is DECRC (VT510 DECRC; ctlseqs: ESC 8). With nothing saved the cursor goes home and the pen resets.
func (e *Emulator) restoreCursor() {
	s := e.scr
	c := s.saved
	if !c.valid {
		c = savedCursor{origin: false}
	}
	s.x, s.y = min(c.x, s.cols-1), min(c.y, s.rows-1)
	e.pen = c.pen
	e.penOK, e.bceOK = false, false
	e.modes.Origin = c.origin
	s.wrapNext = c.wrapNext && e.modes.Autowrap
	e.gl = c.gl
	e.charsets = c.charsets
	e.updateSlow()
}

// alignmentTest is DECALN (VT510 DECALN; ctlseqs: ESC # 8): the screen is filled with E.
func (e *Emulator) alignmentTest() {
	s := e.scr
	s.top, s.bottom = 0, s.rows-1
	for y := 0; y < s.rows; y++ {
		l := s.row(y)
		for i := range l.cells {
			l.cells[i] = Cell{Rune: 'E'}
		}
		l.ext = nil
		l.wrapped = false
		l.dirty = true
	}
	e.moveTo(0, 0)
	e.changed = true
}

// softReset is DECSTR (VT510 DECSTR; ctlseqs: CSI ! p): modes, the scroll region, the pen, the saved cursor and the
// character sets are reset; the screen and the cursor position stay.
func (e *Emulator) softReset() {
	s := e.scr
	cs, vis := e.modes.CursorShape, e.modes.CursorBlink
	e.modes = Modes{Autowrap: true, CursorVisible: true, MetaSendsEscape: true, CursorShape: cs, CursorBlink: vis,
		AltScreen: e.modes.AltScreen, Mouse: e.modes.Mouse, MouseEnc: e.modes.MouseEnc, BracketedPaste: e.modes.BracketedPaste,
		FocusEvents: e.modes.FocusEvents}
	e.charsets = [4]charset{}
	e.gl = 0
	e.slow = false
	s.top, s.bottom = 0, s.rows-1
	s.saved = savedCursor{}
	e.pen = Style{}
	e.penOK, e.bceOK = false, false
	s.wrapNext = false
	e.changed = true
}
