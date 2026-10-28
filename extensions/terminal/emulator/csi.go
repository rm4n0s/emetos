package emulator

import (
	"strconv"
	"unicode/utf8"
)

// escDispatch runs an escape sequence ESC [I...] F (ECMA-48 5.5 and 8.3 for the independent control functions; the VT510
// manual; ctlseqs "Controls beginning with ESC").
func (e *Emulator) escDispatch(f byte) {
	e.changed = true
	s := e.scr
	if e.ninter == 0 {
		switch f {
		case '7': // DECSC, ctlseqs: ESC 7
			e.saveCursor()
		case '8': // DECRC, ctlseqs: ESC 8
			e.restoreCursor()
		case 'D': // IND, ctlseqs: ESC D
			s.wrapNext = false
			e.index()
		case 'E': // NEL, ECMA-48 8.3.86, ctlseqs: ESC E
			e.nextLine()
		case 'H': // HTS, ECMA-48 8.3.62, ctlseqs: ESC H
			e.setTab()
		case 'M': // RI, ECMA-48 8.3.104, ctlseqs: ESC M
			e.reverseIndex()
		case 'Z': // DECID, ctlseqs: ESC Z (like DA1)
			e.deviceAttributes(0)
		case 'c': // RIS, ECMA-48 8.3.105, ctlseqs: ESC c
			e.fullReset()
		case '=': // DECKPAM, ctlseqs: ESC =
			e.modes.AppKeypad = true
		case '>': // DECKPNM, ctlseqs: ESC >
			e.modes.AppKeypad = false
		case 'n': // LS2, ECMA-48 8.3.78: G2 into GL
			e.gl = 2
			e.updateSlow()
		case 'o': // LS3, ECMA-48 8.3.80: G3 into GL
			e.gl = 3
			e.updateSlow()
		case '\\': // ST: nothing to end here (ECMA-48 8.3.143)
		case 'N', 'O': // SS2, SS3 (ECMA-48 8.3.141, 8.3.142): single shifts are ignored
		}
		return
	}
	switch e.inter[0] {
	case '(', ')', '*', '+': // designate G0 to G3 (ECMA-48 8.2.? SCS; VT510 SCS; ctlseqs: ESC ( C)
		if e.ninter != 1 {
			return
		}
		cs := csASCII
		switch f {
		case '0': // DEC Special Graphics
			cs = csDECGraphics
		case 'A': // United Kingdom
			cs = csUK
		}
		e.charsets[int(e.inter[0]-'(')%4] = cs
		e.updateSlow()
	case '#':
		if e.ninter == 1 && f == '8' { // DECALN, ctlseqs: ESC # 8
			e.alignmentTest()
		}
		// ESC # 3, 4, 5, 6 (double-height and double-width lines) are ignored.
	}
	// ESC SP F/G (S7C1T, S8C1T), ESC % (character set), and the others are ignored.
}

// csiDispatch runs a control sequence CSI P...P I...I F (ECMA-48 5.4; ctlseqs "Functions using CSI").
func (e *Emulator) csiDispatch(f byte) {
	e.changed = true
	s := e.scr
	switch {
	case e.ninter == 0 && e.marker == 0:
		e.csiPlain(f)
	case e.ninter == 0 && e.marker == '?':
		e.csiPrivate(f)
	case e.ninter == 0 && e.marker == '>':
		switch f {
		case 'c': // DA2, ctlseqs: CSI > Ps c
			e.deviceAttributes('>')
		case 'q': // XTVERSION, ctlseqs: CSI > Ps q
			e.reply = append(e.reply, "\x1bP>|"...)
			e.reply = append(e.reply, e.name...)
			if e.version != "" {
				e.reply = append(e.reply, '(')
				e.reply = append(e.reply, e.version...)
				e.reply = append(e.reply, ')')
			}
			e.reply = append(e.reply, "\x1b\\"...)
		}
		// CSI > Ps m (XTMODKEYS), CSI > Ps t, CSI > Ps T and the others are ignored.
	case e.ninter == 0 && e.marker == '=':
		if f == 'c' { // DA3, ctlseqs: CSI = Ps c
			e.reply = append(e.reply, "\x1bP!|00000000\x1b\\"...)
		}
	case e.ninter == 1 && e.marker == 0:
		switch e.inter[0] {
		case ' ':
			if f == 'q' { // DECSCUSR, ctlseqs: CSI Ps SP q
				e.setCursorStyle(e.argRaw(0))
			}
		case '!':
			if f == 'p' { // DECSTR, ctlseqs: CSI ! p
				e.softReset()
			}
		case '$':
			if f == 'p' { // DECRQM (ANSI), ctlseqs: CSI Ps $ p
				e.reportMode(e.argRaw(0), false)
			}
		}
		// CSI " q (DECSCA), CSI Ps ; Ps " p (DECSCL), CSI ... $ x/v/z/{ (rectangular operations) are ignored.
	case e.ninter == 1 && e.marker == '?' && e.inter[0] == '$' && f == 'p':
		e.reportMode(e.argRaw(0), true) // DECRQM (DEC private), ctlseqs: CSI ? Ps $ p
	}
	_ = s
}

// csiPlain handles the sequences with no private marker and no intermediate byte.
func (e *Emulator) csiPlain(f byte) {
	s := e.scr
	switch f {
	case '@': // ICH, ECMA-48 8.3.64
		e.insertChars(e.arg(0, 1))
	case 'A': // CUU, ECMA-48 8.3.22
		e.cursorUp(e.arg(0, 1))
	case 'B', 'e': // CUD, ECMA-48 8.3.19; VPR, ECMA-48 8.3.160
		e.cursorDown(e.arg(0, 1))
	case 'C', 'a': // CUF, ECMA-48 8.3.20; HPR, ECMA-48 8.3.59
		e.cursorForward(e.arg(0, 1))
	case 'D': // CUB, ECMA-48 8.3.18
		e.cursorBack(e.arg(0, 1))
	case 'E': // CNL, ECMA-48 8.3.12
		e.cursorDown(e.arg(0, 1))
		s.x = 0
	case 'F': // CPL, ECMA-48 8.3.13
		e.cursorUp(e.arg(0, 1))
		s.x = 0
	case 'G', '`': // CHA, ECMA-48 8.3.9; HPA, ECMA-48 8.3.57
		e.moveTo(s.y-e.originTop(), e.arg(0, 1)-1)
	case 'H', 'f': // CUP, ECMA-48 8.3.21; HVP, ECMA-48 8.3.63
		e.moveTo(e.arg(0, 1)-1, e.arg(1, 1)-1)
	case 'I': // CHT, ECMA-48 8.3.10
		s.wrapNext = false
		s.x = e.nextTab(s.x, e.arg(0, 1))
	case 'J': // ED, ECMA-48 8.3.39
		e.eraseInDisplay(e.argRaw(0))
	case 'K': // EL, ECMA-48 8.3.41
		e.eraseInLine(e.argRaw(0))
	case 'L': // IL, ECMA-48 8.3.67
		e.insertLines(e.arg(0, 1))
	case 'M': // DL, ECMA-48 8.3.32
		e.deleteLines(e.arg(0, 1))
	case 'P': // DCH, ECMA-48 8.3.26
		e.deleteChars(e.arg(0, 1))
	case 'S': // SU, ECMA-48 8.3.147
		s.scrollUp(e.arg(0, 1), e.bce())
		e.allDirty = true
	case 'T': // SD, ECMA-48 8.3.113 (ctlseqs: five parameters are the mouse highlight, which is ignored)
		if e.nparams <= 1 {
			s.scrollDown(e.arg(0, 1), e.bce())
			e.allDirty = true
		}
	case 'X': // ECH, ECMA-48 8.3.38
		e.eraseChars(e.arg(0, 1))
	case 'Z': // CBT, ECMA-48 8.3.7
		s.wrapNext = false
		s.x = e.prevTab(s.x, e.arg(0, 1))
	case '^': // SD, ctlseqs: CSI Ps ^
		s.scrollDown(e.arg(0, 1), e.bce())
		e.allDirty = true
	case 'b': // REP, ECMA-48 8.3.103
		if e.lastRune != 0 {
			e.repeat(e.lastRune, min(e.arg(0, 1), s.rows*s.cols))
		}
	case 'c': // DA, ECMA-48 8.3.24; ctlseqs: CSI Ps c
		e.deviceAttributes(0)
	case 'd': // VPA, ECMA-48 8.3.158
		e.moveTo(e.arg(0, 1)-1, s.x)
	case 'g': // TBC, ECMA-48 8.3.154
		switch e.argRaw(0) {
		case 0:
			s.tabs[s.x] = false
		case 3:
			clear(s.tabs)
		}
	case 'h': // SM, ECMA-48 8.3.125
		for i := 0; i < max(e.nparams, 1); i++ {
			e.setAnsiMode(e.argRaw(i), true)
		}
	case 'l': // RM, ECMA-48 8.3.106
		for i := 0; i < max(e.nparams, 1); i++ {
			e.setAnsiMode(e.argRaw(i), false)
		}
	case 'm': // SGR, ECMA-48 8.3.117
		e.sgr()
	case 'n': // DSR, ECMA-48 8.3.35
		switch e.argRaw(0) {
		case 5: // status: ready, no malfunction
			e.reply = append(e.reply, "\x1b[0n"...)
		case 6: // CPR, ECMA-48 8.3.14
			e.cursorPositionReport(false)
		}
	case 'r': // DECSTBM, VT510 DECSTBM
		e.setScrollRegion(e.arg(0, 1), e.arg(1, s.rows))
	case 's': // SCOSC (ANSI.SYS), ctlseqs: CSI s saves the cursor (no left and right margins here)
		e.saveCursor()
	case 'u': // SCORC, ctlseqs: CSI u
		e.restoreCursor()
	case 't': // window operations, ctlseqs: CSI Ps ; Ps ; Ps t
		e.windowOp()
	}
}

// originTop is the first row of the cursor's range: the top margin in origin mode, else 0.
func (e *Emulator) originTop() int {
	if e.modes.Origin {
		return e.scr.top
	}
	return 0
}

// cursorPositionReport answers CPR, `CSI r ; c R` (ECMA-48 8.3.14), or DECXCPR, `CSI ? r ; c ; p R` (ctlseqs: CSI ? 6 n).
// With the origin mode the row is relative to the top margin.
func (e *Emulator) cursorPositionReport(private bool) {
	s := e.scr
	e.reply = append(e.reply, "\x1b["...)
	if private {
		e.reply = append(e.reply, '?')
	}
	e.reply = strconv.AppendInt(e.reply, int64(s.y-e.originTop()+1), 10)
	e.reply = append(e.reply, ';')
	e.reply = strconv.AppendInt(e.reply, int64(s.x+1), 10)
	if private {
		e.reply = append(e.reply, ";1"...)
	}
	e.reply = append(e.reply, 'R')
}

// deviceAttributes answers DA1 (`CSI c`, ECMA-48 8.3.24; ctlseqs: "VT100 with Advanced Video Option": CSI ? 1 ; 2 c) and DA2
// (`CSI > c`; ctlseqs: CSI > Pp ; Pv ; Pc c, with Pp 0 and the patch level 276 that programs test for).
func (e *Emulator) deviceAttributes(kind byte) {
	if kind == '>' {
		e.reply = append(e.reply, "\x1b[>0;276;0c"...)
		return
	}
	e.reply = append(e.reply, "\x1b[?1;2c"...)
}

// windowOp is the subset of `CSI Ps ; Ps ; Ps t` (ctlseqs "Window manipulation") that reports the size in characters: 18 and
// 19. The operations that move, resize, iconify or push the title are ignored, and so are the ones that report pixels (the
// terminal does not know its font).
func (e *Emulator) windowOp() {
	switch e.argRaw(0) {
	case 18:
		e.reply = append(e.reply, "\x1b[8;"...)
	case 19:
		e.reply = append(e.reply, "\x1b[9;"...)
	default:
		return
	}
	e.reply = strconv.AppendInt(e.reply, int64(e.scr.rows), 10)
	e.reply = append(e.reply, ';')
	e.reply = strconv.AppendInt(e.reply, int64(e.scr.cols), 10)
	e.reply = append(e.reply, 't')
}

// setCursorStyle is DECSCUSR (ctlseqs: CSI Ps SP q).
func (e *Emulator) setCursorStyle(n int) {
	switch n {
	case 0, 1:
		e.modes.CursorShape, e.modes.CursorBlink = CursorBlock, true
	case 2:
		e.modes.CursorShape, e.modes.CursorBlink = CursorBlock, false
	case 3:
		e.modes.CursorShape, e.modes.CursorBlink = CursorUnderline, true
	case 4:
		e.modes.CursorShape, e.modes.CursorBlink = CursorUnderline, false
	case 5:
		e.modes.CursorShape, e.modes.CursorBlink = CursorBar, true
	case 6:
		e.modes.CursorShape, e.modes.CursorBlink = CursorBar, false
	}
}

// csiPrivate handles `CSI ? ...`.
func (e *Emulator) csiPrivate(f byte) {
	switch f {
	case 'h': // DECSET, ctlseqs: CSI ? Pm h
		for i := 0; i < e.nparams; i++ {
			e.setPrivateMode(e.argRaw(i), true)
		}
	case 'l': // DECRST, ctlseqs: CSI ? Pm l
		for i := 0; i < e.nparams; i++ {
			e.setPrivateMode(e.argRaw(i), false)
		}
	case 's': // XTSAVE, ctlseqs: CSI ? Pm s
		if e.savedMode == nil {
			e.savedMode = map[int]bool{}
		}
		for i := 0; i < e.nparams; i++ {
			if v, ok := e.privateMode(e.argRaw(i)); ok {
				e.savedMode[e.argRaw(i)] = v
			}
		}
	case 'r': // XTRESTORE, ctlseqs: CSI ? Pm r
		for i := 0; i < e.nparams; i++ {
			if v, ok := e.savedMode[e.argRaw(i)]; ok {
				e.setPrivateMode(e.argRaw(i), v)
			}
		}
	case 'J': // DECSED, ctlseqs: CSI ? Ps J (no cell is protected here, so it is ED)
		e.eraseInDisplay(e.argRaw(0))
	case 'K': // DECSEL, ctlseqs: CSI ? Ps K
		e.eraseInLine(e.argRaw(0))
	case 'n': // DSR, DEC private, ctlseqs: CSI ? Ps n
		switch e.argRaw(0) {
		case 6: // DECXCPR
			e.cursorPositionReport(true)
		case 15: // printer status: no printer
			e.reply = append(e.reply, "\x1b[?13n"...)
		case 25: // user-defined keys: locked
			e.reply = append(e.reply, "\x1b[?21n"...)
		case 26: // keyboard: North American
			e.reply = append(e.reply, "\x1b[?27;1;0;0n"...)
		case 62, 63, 64: // macro space and checksum: not supported
		case 53, 55: // locator status: no locator
			e.reply = append(e.reply, "\x1b[?53n"...)
		}
	}
}

// repeat prints r n times (REP, ECMA-48 8.3.103), through the fast path in chunks. The count is held to the size of the
// screen: more than that only prints the same cells again.
func (e *Emulator) repeat(r rune, n int) {
	var enc [utf8.UTFMax]byte
	sz := utf8.EncodeRune(enc[:], r)
	var buf [1024]byte
	for n > 0 {
		k := min(n, len(buf)/sz)
		for i := 0; i < k; i++ {
			copy(buf[i*sz:], enc[:sz])
		}
		b := buf[:k*sz]
		for len(b) > 0 {
			c := 0
			if !e.slow {
				c = e.printText(b)
			}
			if c == 0 {
				e.printRune(r)
				c = sz
			}
			b = b[c:]
		}
		n -= k
	}
}
