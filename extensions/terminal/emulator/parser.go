package emulator

import "unicode/utf8"

// parserState is a state of the DEC parser (vt100.net/emu/dec_ansi_parser).
type parserState uint8

const (
	stGround parserState = iota
	stEscape
	stEscapeInter
	stCSIEntry
	stCSIParam
	stCSIInter
	stCSIIgnore
	stDCSEntry
	stDCSParam
	stDCSInter
	stDCSPass
	stDCSIgnore
	stOSC
	stSOSPMAPC
)

type dcsKind uint8

const (
	dcsNone dcsKind = iota
	// dcsRQSS is DECRQSS, DCS $ q Pt ST (VT510 DECRQSS; ctlseqs: "DCS $ q Pt ST").
	dcsRQSS
)

// maxDCS is the longest data string of a DCS that is kept.
const maxDCS = 64

// clearSeq forgets the collected parameters and intermediates (dec_ansi_parser: the action clear).
func (e *Emulator) clearSeq() {
	e.nparams = 0
	e.colons = 0
	e.curParam = 0
	e.haveCur = false
	e.marker = 0
	e.ninter = 0
}

// feedByte runs the parser for one byte below 0x80 (dec_ansi_parser; ECMA-48 5.3 for the control strings).
func (e *Emulator) feedByte(b byte) {
	// The transitions that apply in every state.
	switch b {
	case 0x18, 0x1a: // CAN, SUB: cancel the sequence (ECMA-48 8.3.6 CAN, 8.3.148 SUB)
		e.leaveString(false)
		e.state = stGround
		return
	case 0x1b: // ESC
		e.leaveString(true)
		e.clearSeq()
		e.state = stEscape
		return
	}
	switch e.state {
	case stGround:
		switch {
		case b < 0x20:
			e.execute(b)
		case b == 0x7f: // DEL is ignored (dec_ansi_parser)
		default:
			e.printRune(rune(b))
		}
	case stEscape:
		switch {
		case b < 0x20:
			e.execute(b)
		case b == 0x7f:
		case b < 0x30:
			e.collect(b)
			e.state = stEscapeInter
		case b == 'P':
			e.clearSeq()
			e.state = stDCSEntry
		case b == 'X' || b == '^' || b == '_':
			e.state = stSOSPMAPC
		case b == '[':
			e.clearSeq()
			e.state = stCSIEntry
		case b == ']':
			e.startOSC()
		default:
			e.escDispatch(b)
			e.state = stGround
		}
	case stEscapeInter:
		switch {
		case b < 0x20:
			e.execute(b)
		case b == 0x7f:
		case b < 0x30:
			e.collect(b)
		default:
			e.escDispatch(b)
			e.state = stGround
		}
	case stCSIEntry, stCSIParam:
		switch {
		case b < 0x20:
			e.execute(b)
		case b == 0x7f:
		case b >= '0' && b <= '9':
			e.digit(b)
			e.state = stCSIParam
		case b == ';':
			e.pushParam()
			e.state = stCSIParam
		case b == ':':
			// ITU T.416 sub-parameters, which SGR 4:3 and 38:2:: use; the DEC state machine would ignore the sequence.
			e.pushParam()
			e.colons |= 1 << uint(min(e.nparams, 31))
			e.state = stCSIParam
		case b >= 0x3c && b <= 0x3f:
			if e.state == stCSIEntry {
				e.marker = b
				e.state = stCSIParam
			} else {
				e.state = stCSIIgnore
			}
		case b < 0x30:
			e.collect(b)
			e.state = stCSIInter
		default: // 0x40 to 0x7e
			e.finishParams()
			e.csiDispatch(b)
			e.state = stGround
		}
	case stCSIInter:
		switch {
		case b < 0x20:
			e.execute(b)
		case b == 0x7f:
		case b < 0x30:
			e.collect(b)
		case b < 0x40:
			e.state = stCSIIgnore
		default:
			e.finishParams()
			e.csiDispatch(b)
			e.state = stGround
		}
	case stCSIIgnore:
		switch {
		case b < 0x20:
			e.execute(b)
		case b >= 0x40 && b <= 0x7e:
			e.state = stGround
		}
	case stDCSEntry, stDCSParam:
		switch {
		case b < 0x20 || b == 0x7f:
		case b >= '0' && b <= '9':
			e.digit(b)
			e.state = stDCSParam
		case b == ';':
			e.pushParam()
			e.state = stDCSParam
		case b == ':':
			e.state = stDCSIgnore
		case b >= 0x3c && b <= 0x3f:
			if e.state == stDCSEntry {
				e.marker = b
				e.state = stDCSParam
			} else {
				e.state = stDCSIgnore
			}
		case b < 0x30:
			e.collect(b)
			e.state = stDCSInter
		default:
			e.finishParams()
			e.dcsHook(b)
			e.state = stDCSPass
		}
	case stDCSInter:
		switch {
		case b < 0x20 || b == 0x7f:
		case b < 0x30:
			e.collect(b)
		case b < 0x40:
			e.state = stDCSIgnore
		default:
			e.finishParams()
			e.dcsHook(b)
			e.state = stDCSPass
		}
	case stDCSPass:
		if b >= 0x20 && b < 0x7f && e.dcsKind != dcsNone && len(e.dcsBuf) < maxDCS {
			e.dcsBuf = append(e.dcsBuf, b)
		}
	case stDCSIgnore, stSOSPMAPC:
	case stOSC:
		switch {
		case b == 0x07: // BEL ends an OSC string too (ctlseqs: xterm accepts it as the terminator)
			e.oscBEL = true
			e.leaveString(true)
			e.state = stGround
		case b >= 0x20 && b != 0x7f:
			e.oscPut(b)
		}
	}
}

// feedRune runs the parser for a decoded character of 0x80 or more: the C1 controls (U+0080 to U+009F, which UTF-8 encodes
// as two bytes) and the graphic characters.
func (e *Emulator) feedRune(r rune) {
	if r < 0x80 {
		e.feedByte(byte(r))
		return
	}
	if r <= 0x9f {
		e.c1(byte(r))
		return
	}
	switch e.state {
	case stGround:
		e.printRune(r)
	case stOSC:
		var buf [4]byte
		n := utf8.EncodeRune(buf[:], r)
		for _, b := range buf[:n] {
			e.oscPut(b)
		}
	case stEscape, stEscapeInter:
		e.state = stGround
	case stCSIEntry, stCSIParam, stCSIInter:
		e.state = stCSIIgnore
	case stDCSEntry, stDCSParam, stDCSInter:
		e.state = stDCSIgnore
	}
}

// c1 handles a C1 control (ECMA-48 5.4 and 8.3; dec_ansi_parser "anywhere" transitions for 0x80 to 0x9f).
func (e *Emulator) c1(b byte) {
	switch b {
	case 0x90: // DCS
		e.leaveString(true)
		e.clearSeq()
		e.state = stDCSEntry
	case 0x98, 0x9e, 0x9f: // SOS, PM, APC
		e.leaveString(true)
		e.state = stSOSPMAPC
	case 0x9b: // CSI
		e.leaveString(true)
		e.clearSeq()
		e.state = stCSIEntry
	case 0x9c: // ST
		e.leaveString(true)
		e.state = stGround
	case 0x9d: // OSC
		e.leaveString(true)
		e.startOSC()
	default:
		e.leaveString(true)
		e.state = stGround
		switch b {
		case 0x84: // IND, VT510 and ctlseqs ESC D
			e.index()
		case 0x85: // NEL, ECMA-48 8.3.86
			e.nextLine()
		case 0x88: // HTS, ECMA-48 8.3.62
			e.setTab()
		case 0x8d: // RI, ECMA-48 8.3.104
			e.reverseIndex()
		case 0x9a: // DECID, VT510: like DA1
			e.deviceAttributes(0)
		}
	}
}

// leaveString ends the control string the parser is in: an OSC is run when dispatch is true (ST, BEL or the start of a
// new sequence end it; CAN and SUB abort it), and a DCS is unhooked (dec_ansi_parser: osc_end, unhook).
func (e *Emulator) leaveString(dispatch bool) {
	switch e.state {
	case stOSC:
		if dispatch {
			e.oscEnd()
		}
		e.osc = e.osc[:0]
	case stDCSPass:
		if dispatch {
			e.dcsUnhook()
		}
		e.dcsKind = dcsNone
		e.dcsBuf = e.dcsBuf[:0]
	}
}

func (e *Emulator) collect(b byte) {
	if e.ninter < len(e.inter) {
		e.inter[e.ninter] = b
	}
	e.ninter++
}

func (e *Emulator) digit(b byte) {
	v := e.curParam*10 + int32(b-'0')
	if v > maxParamValue {
		v = maxParamValue
	}
	e.curParam = v
	e.haveCur = true
}

// pushParam ends the parameter being read (dec_ansi_parser: the action param). An empty one is stored as -1, which the
// handlers read as the default.
func (e *Emulator) pushParam() {
	if e.nparams < MaxParams {
		if e.haveCur {
			e.params[e.nparams] = e.curParam
		} else {
			e.params[e.nparams] = -1
		}
		e.nparams++
	}
	e.curParam = 0
	e.haveCur = false
}

// finishParams stores the last parameter when the final byte arrives.
func (e *Emulator) finishParams() {
	if e.haveCur || e.nparams > 0 {
		e.pushParam()
	}
}

// arg is parameter i, or def when it is absent or 0 (ECMA-48 5.4.2: a missing or zero parameter takes the default).
func (e *Emulator) arg(i int, def int) int {
	if i >= e.nparams || e.params[i] <= 0 {
		return def
	}
	return int(e.params[i])
}

// argRaw is parameter i, with absent as 0 and no default.
func (e *Emulator) argRaw(i int) int {
	if i >= e.nparams || e.params[i] < 0 {
		return 0
	}
	return int(e.params[i])
}

// hasInter reports whether the sequence has exactly the intermediate byte b.
func (e *Emulator) hasInter(b byte) bool { return e.ninter == 1 && e.inter[0] == b }
