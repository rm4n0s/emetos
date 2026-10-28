package emulator

import "strconv"

// MouseButton is the button of a mouse event.
type MouseButton uint8

// The buttons. MouseNoButton is a move with no button down.
const (
	MouseLeft MouseButton = iota
	MouseMiddle
	MouseRight
	MouseNoButton
	MouseWheelUp
	MouseWheelDown
	MouseWheelLeft
	MouseWheelRight
)

// MouseAction is what the mouse did.
type MouseAction uint8

// The actions.
const (
	MousePress MouseAction = iota
	MouseRelease
	MouseMove
)

// MouseEvent is a mouse event at a cell (0-based).
type MouseEvent struct {
	Button MouseButton
	Action MouseAction
	Mods   Mod
	X, Y   int
}

// EncodeMouse appends to dst the report of the mouse event as the program asked for it (ctlseqs "Mouse Tracking": the
// protocols DECSET 9, 1000, 1002 and 1003, the encodings default, 1005, 1006 and 1015), and returns it. It appends nothing
// when the program did not ask for the event. With no mouse protocol, the wheel on the alternate screen sends cursor keys
// when the program turned DECSET 1007 on. A move inside the cell of the last reported one is not reported.
func (e *Emulator) EncodeMouse(ev MouseEvent, dst []byte) []byte {
	e.mu.Lock()
	defer e.mu.Unlock()
	m := e.modes
	wheel := ev.Button >= MouseWheelUp
	if m.Mouse == MouseNone {
		if wheel && ev.Action == MousePress && m.AltScroll && m.AltScreen {
			final := byte('A')
			if ev.Button == MouseWheelDown {
				final = 'B'
			}
			if ev.Button == MouseWheelLeft || ev.Button == MouseWheelRight {
				return dst
			}
			if m.AppCursorKeys {
				return append(dst, 0x1b, 'O', final)
			}
			return append(dst, 0x1b, '[', final)
		}
		return dst
	}
	if wheel && ev.Action != MousePress {
		return dst // wheel releases are not reported
	}
	var cb int
	switch ev.Action {
	case MousePress:
		if wheel {
			cb = 64 + int(ev.Button-MouseWheelUp)
		} else if ev.Button <= MouseRight {
			cb = int(ev.Button)
		} else {
			return dst
		}
	case MouseRelease:
		if m.Mouse == MouseX10 || ev.Button > MouseRight {
			return dst
		}
		cb = int(ev.Button)
		if m.MouseEnc != MouseEncSGR {
			cb = 3 // a release names no button (ctlseqs)
		}
	case MouseMove:
		if m.Mouse != MouseDrag && m.Mouse != MouseAny {
			return dst
		}
		if m.Mouse == MouseDrag && ev.Button > MouseRight {
			return dst
		}
		if e.mouseSeen && e.mouseX == ev.X && e.mouseY == ev.Y {
			return dst
		}
		b := int(ev.Button)
		if ev.Button > MouseRight {
			b = 3
		}
		cb = 32 + b
	}
	e.mouseX, e.mouseY, e.mouseSeen = ev.X, ev.Y, ev.Action == MouseMove
	if m.Mouse != MouseX10 {
		if ev.Mods&ModShift != 0 {
			cb |= 4
		}
		if ev.Mods&ModAlt != 0 {
			cb |= 8
		}
		if ev.Mods&ModCtrl != 0 {
			cb |= 16
		}
	}
	x, y := ev.X+1, ev.Y+1
	switch m.MouseEnc {
	case MouseEncSGR:
		dst = append(dst, 0x1b, '[', '<')
		dst = strconv.AppendInt(dst, int64(cb), 10)
		dst = append(dst, ';')
		dst = strconv.AppendInt(dst, int64(x), 10)
		dst = append(dst, ';')
		dst = strconv.AppendInt(dst, int64(y), 10)
		if ev.Action == MouseRelease {
			return append(dst, 'm')
		}
		return append(dst, 'M')
	case MouseEncURXVT:
		dst = append(dst, 0x1b, '[')
		dst = strconv.AppendInt(dst, int64(cb+32), 10)
		dst = append(dst, ';')
		dst = strconv.AppendInt(dst, int64(x), 10)
		dst = append(dst, ';')
		dst = strconv.AppendInt(dst, int64(y), 10)
		return append(dst, 'M')
	case MouseEncUTF8:
		if x+32 > 2015 || y+32 > 2015 {
			return dst
		}
		dst = append(dst, 0x1b, '[', 'M')
		dst = utf8AppendRune(dst, rune(cb+32))
		dst = utf8AppendRune(dst, rune(x+32))
		return utf8AppendRune(dst, rune(y+32))
	}
	if cb+32 > 255 || x+32 > 255 || y+32 > 255 {
		return dst
	}
	return append(dst, 0x1b, '[', 'M', byte(cb+32), byte(x+32), byte(y+32))
}
