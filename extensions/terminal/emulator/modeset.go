package emulator

import "strconv"

// setAnsiMode is SM and RM with the ANSI modes (ECMA-48 8.3.125, 8.3.106; ctlseqs "CSI Pm h"): 4 insert (IRM) and 20 new
// line (LNM). 2 (KAM) and 12 (SRM) are accepted and ignored.
func (e *Emulator) setAnsiMode(n int, on bool) {
	switch n {
	case 4:
		e.modes.Insert = on
		e.updateSlow()
	case 20:
		e.modes.NewLine = on
	}
}

// ansiMode reports an ANSI mode for DECRQM.
func (e *Emulator) ansiMode(n int) (set, known bool) {
	switch n {
	case 2, 12: // KAM is always reset; SRM (local echo off) is always set
		return n == 12, true
	case 4:
		return e.modes.Insert, true
	case 20:
		return e.modes.NewLine, true
	}
	return false, false
}

// privateMode reads a DEC private mode.
func (e *Emulator) privateMode(n int) (set, known bool) {
	m := &e.modes
	switch n {
	case 1:
		return m.AppCursorKeys, true
	case 3: // DECCOLM: not supported, always 80 columns off
		return false, true
	case 5:
		return m.ReverseVideo, true
	case 6:
		return m.Origin, true
	case 7:
		return m.Autowrap, true
	case 8: // DECARM: always set
		return true, true
	case 9:
		return m.Mouse == MouseX10, true
	case 12:
		return m.CursorBlink, true
	case 25:
		return m.CursorVisible, true
	case 45:
		return m.ReverseWrap, true
	case 47, 1047, 1049:
		return m.AltScreen, true
	case 66:
		return m.AppKeypad, true
	case 1000:
		return m.Mouse == MouseNormal, true
	case 1002:
		return m.Mouse == MouseDrag, true
	case 1003:
		return m.Mouse == MouseAny, true
	case 1004:
		return m.FocusEvents, true
	case 1005:
		return m.MouseEnc == MouseEncUTF8, true
	case 1006:
		return m.MouseEnc == MouseEncSGR, true
	case 1007:
		return m.AltScroll, true
	case 1015:
		return m.MouseEnc == MouseEncURXVT, true
	case 1036, 1039:
		return m.MetaSendsEscape, true
	case 2004:
		return m.BracketedPaste, true
	case 2026:
		return m.SyncOutput, true
	}
	return false, false
}

// setPrivateMode is DECSET and DECRST (ctlseqs: "CSI ? Pm h", "CSI ? Pm l"; VT510 for the DEC modes).
func (e *Emulator) setPrivateMode(n int, on bool) {
	m := &e.modes
	switch n {
	case 1: // DECCKM: application cursor keys
		m.AppCursorKeys = on
	case 5: // DECSCNM: reverse video
		m.ReverseVideo = on
		e.allDirty = true
	case 6: // DECOM: origin mode; the cursor goes home
		m.Origin = on
		e.moveTo(0, 0)
	case 7: // DECAWM: autowrap
		m.Autowrap = on
		if !on {
			e.mainScr.wrapNext, e.altScr.wrapNext = false, false
		}
	case 9: // X10 mouse
		m.Mouse = mouseIf(on, MouseX10)
	case 12: // att610: blinking cursor
		m.CursorBlink = on
	case 25: // DECTCEM: show the cursor
		m.CursorVisible = on
	case 45: // reverse wrap-around
		m.ReverseWrap = on
	case 47: // alternate screen, no clearing, no cursor save
		e.switchScreen(on, false, false)
	case 1047: // alternate screen; the alternate screen is cleared when leaving it
		e.switchScreen(on, !on, false)
	case 1048: // save and restore the cursor (DECSC, DECRC)
		if on {
			e.saveCursor()
		} else {
			e.restoreCursor()
		}
	case 1049: // save the cursor and switch to the cleared alternate screen; leaving restores the cursor
		if on {
			e.saveCursor()
			e.switchScreen(true, true, false)
		} else {
			e.switchScreen(false, false, true)
		}
	case 66: // DECNKM: application keypad
		m.AppKeypad = on
	case 1000:
		m.Mouse = mouseIf(on, MouseNormal)
	case 1002:
		m.Mouse = mouseIf(on, MouseDrag)
	case 1003:
		m.Mouse = mouseIf(on, MouseAny)
	case 1004:
		m.FocusEvents = on
	case 1005:
		m.MouseEnc = encIf(on, MouseEncUTF8)
	case 1006:
		m.MouseEnc = encIf(on, MouseEncSGR)
	case 1015:
		m.MouseEnc = encIf(on, MouseEncURXVT)
	case 1007:
		m.AltScroll = on
	case 1036, 1039:
		m.MetaSendsEscape = on
	case 2004:
		m.BracketedPaste = on
	case 2026: // synchronized output
		m.SyncOutput = on
		e.changed = true
	}
	// DECSCLM (4), DECARM (8), DECCOLM (3), DECNRCM (42), 1001, 1010, 1011, 1016, 2027, 2031 and the others are ignored.
}

func mouseIf(on bool, m MouseMode) MouseMode {
	if on {
		return m
	}
	return MouseNone
}

func encIf(on bool, enc MouseEncoding) MouseEncoding {
	if on {
		return enc
	}
	return MouseEncDefault
}

// reportMode answers DECRQM (ctlseqs: CSI Ps $ p and CSI ? Ps $ p): `CSI [?] Ps ; Pm $ y` with Pm 1 set, 2 reset, 0 not
// recognized.
func (e *Emulator) reportMode(n int, private bool) {
	var set, known bool
	if private {
		set, known = e.privateMode(n)
	} else {
		set, known = e.ansiMode(n)
	}
	pm := 0
	if known {
		pm = 2
		if set {
			pm = 1
		}
	}
	if private && n == 2027 { // grapheme clustering: permanently reset
		pm = 4
	}
	e.reply = append(e.reply, "\x1b["...)
	if private {
		e.reply = append(e.reply, '?')
	}
	e.reply = strconv.AppendInt(e.reply, int64(n), 10)
	e.reply = append(e.reply, ';')
	e.reply = strconv.AppendInt(e.reply, int64(pm), 10)
	e.reply = append(e.reply, "$y"...)
}

// switchScreen shows the alternate screen (alt) or the main one. clear blanks the alternate screen (entering with 47 does
// not, 1047 and 1049 do on entering and 1047 on leaving); restore puts the cursor back from DECSC (1049).
func (e *Emulator) switchScreen(alt, clearAlt, restore bool) {
	if alt == e.modes.AltScreen {
		return
	}
	e.modes.AltScreen = alt
	if alt {
		from := e.mainScr
		e.scr = e.altScr
		e.altScr.x, e.altScr.y, e.altScr.wrapNext = from.x, from.y, from.wrapNext
		if clearAlt {
			e.clearScreen(e.altScr)
		}
	} else {
		if clearAlt {
			e.clearScreen(e.altScr)
		}
		e.scr = e.mainScr
		if restore {
			e.restoreCursor()
		}
	}
	e.allDirty, e.changed = true, true
}

func (e *Emulator) clearScreen(s *screen) {
	for y := 0; y < s.rows; y++ {
		clearLine(s.row(y), 0)
	}
	s.top, s.bottom = 0, s.rows-1
}
