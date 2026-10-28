package emulator

import (
	"strconv"
	"unicode/utf8"
)

// Mod is a set of modifier keys.
type Mod uint8

// The modifiers. ctlseqs numbers them in the parameter of a key sequence as 1 + the sum of Shift 1, Alt 2, Ctrl 4, Meta 8.
const (
	ModShift Mod = 1 << iota
	ModAlt
	ModCtrl
	ModMeta
)

// Key is a key that is not just a character.
type Key uint16

// The keys the encoder knows. KeyRune is a character key: its Rune says which (already shifted by the keyboard layout).
const (
	KeyRune Key = iota
	KeyEnter
	KeyTab
	KeyBackspace
	KeyEscape
	KeyUp
	KeyDown
	KeyRight
	KeyLeft
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
	KeyInsert
	KeyDelete
	// KeyF1 to KeyF20 are consecutive.
	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12
	KeyF13
	KeyF14
	KeyF15
	KeyF16
	KeyF17
	KeyF18
	KeyF19
	KeyF20
	// KeyKP0 to KeyKP9 are consecutive; the keypad keys send their characters, except in application keypad mode.
	KeyKP0
	KeyKP1
	KeyKP2
	KeyKP3
	KeyKP4
	KeyKP5
	KeyKP6
	KeyKP7
	KeyKP8
	KeyKP9
	KeyKPEnter
	KeyKPAdd
	KeyKPSubtract
	KeyKPMultiply
	KeyKPDivide
	KeyKPDecimal
	KeyKPComma
	KeyKPEqual
)

// KeyEvent is a key press.
type KeyEvent struct {
	Key  Key
	Rune rune // for KeyRune
	Mods Mod
}

// KeyModes are the modes that decide what a key sends.
type KeyModes struct {
	// AppCursor is DECCKM, AppKeypad is DECKPAM, NewLine is LNM (Enter sends CR LF), MetaSendsEscape is DECSET 1036 (Alt
	// sends ESC before the character, else it sets the eighth bit).
	AppCursor, AppKeypad, NewLine, MetaSendsEscape bool
}

// KeyModes returns the modes of the terminal that the key encoder needs.
func (e *Emulator) KeyModes() KeyModes {
	e.mu.Lock()
	defer e.mu.Unlock()
	return KeyModes{AppCursor: e.modes.AppCursorKeys, AppKeypad: e.modes.AppKeypad, NewLine: e.modes.NewLine, MetaSendsEscape: e.modes.MetaSendsEscape}
}

// EncodeKey appends to dst the bytes that the key sends given the modes of the terminal, and returns it; a key that sends
// nothing leaves dst as it was.
func (e *Emulator) EncodeKey(ev KeyEvent, dst []byte) []byte {
	return EncodeKey(ev, e.KeyModes(), dst)
}

// ctrlByte is the control character that Ctrl and the character r make (ctlseqs "Alt and Meta Keys"/"Control keys": Ctrl-@
// is NUL, Ctrl-A to Ctrl-Z are 1 to 26, Ctrl-[ is ESC, Ctrl-\ 28, Ctrl-] 29, Ctrl-^ 30, Ctrl-_ 31, Ctrl-? DEL; the digits
// 2 to 8 are the same keys on keyboards that have no punctuation there).
func ctrlByte(r rune) (byte, bool) {
	switch {
	case r >= 'a' && r <= 'z':
		return byte(r - 'a' + 1), true
	case r >= 'A' && r <= 'Z':
		return byte(r - 'A' + 1), true
	}
	switch r {
	case '@', ' ', '2', '`':
		return 0, true
	case '[', '3':
		return 0x1b, true
	case '\\', '4':
		return 0x1c, true
	case ']', '5':
		return 0x1d, true
	case '^', '6':
		return 0x1e, true
	case '_', '7', '/':
		return 0x1f, true
	case '?', '8':
		return 0x7f, true
	}
	return 0, false
}

// modParam is the modifier parameter of a key sequence: 1 plus Shift 1, Alt 2, Ctrl 4, Meta 8 (ctlseqs "PC-Style Function
// Keys").
func modParam(m Mod) int { return 1 + int(m&(ModShift|ModAlt|ModCtrl|ModMeta)) }

// EncodeKey is the key encoder, from a key, its modifiers and the modes to bytes (ctlseqs "PC-Style Function Keys",
// "VT220-style Function Keys", "Alt and Meta Keys"; the keypad of "VT100/VT220 Keypad").
func EncodeKey(ev KeyEvent, m KeyModes, dst []byte) []byte {
	mods := ev.Mods
	alt := mods&ModAlt != 0
	switch ev.Key {
	case KeyRune:
		if ev.Rune <= 0 {
			return dst
		}
		var buf [utf8.UTFMax]byte
		r := ev.Rune
		if mods&ModCtrl != 0 {
			if b, ok := ctrlByte(r); ok {
				return appendMeta(dst, alt, m.MetaSendsEscape, rune(b))
			}
		}
		n := utf8.EncodeRune(buf[:], r)
		if alt && !m.MetaSendsEscape && r < 0x80 {
			return utf8.AppendRune(dst, r|0x80)
		}
		if alt {
			dst = append(dst, 0x1b)
		}
		return append(dst, buf[:n]...)
	case KeyEnter, KeyKPEnter:
		if ev.Key == KeyKPEnter && m.AppKeypad {
			return appendSS3(dst, 'M', mods)
		}
		if alt {
			dst = append(dst, 0x1b)
		}
		if m.NewLine {
			return append(dst, '\r', '\n')
		}
		return append(dst, '\r')
	case KeyTab:
		if mods&ModShift != 0 {
			if alt {
				dst = append(dst, 0x1b)
			}
			return append(dst, 0x1b, '[', 'Z') // CBT
		}
		return appendMeta(dst, alt, m.MetaSendsEscape, '\t')
	case KeyBackspace:
		if mods&ModCtrl != 0 {
			return appendMeta(dst, alt, m.MetaSendsEscape, 0x08)
		}
		return appendMeta(dst, alt, m.MetaSendsEscape, 0x7f)
	case KeyEscape:
		return appendMeta(dst, alt, m.MetaSendsEscape, 0x1b)
	case KeyUp, KeyDown, KeyRight, KeyLeft, KeyHome, KeyEnd:
		final := cursorFinals[ev.Key-KeyUp]
		if mods&(ModShift|ModAlt|ModCtrl|ModMeta) != 0 {
			dst = append(dst, 0x1b, '[', '1', ';')
			dst = strconv.AppendInt(dst, int64(modParam(mods)), 10)
			return append(dst, final)
		}
		if m.AppCursor {
			return append(dst, 0x1b, 'O', final)
		}
		return append(dst, 0x1b, '[', final)
	case KeyInsert, KeyDelete, KeyPageUp, KeyPageDown:
		return appendTilde(dst, editCodes[ev.Key-KeyPageUp], mods)
	}
	if ev.Key >= KeyF13 && ev.Key <= KeyF20 {
		// The terminfo entry of xterm-256color (kf13 to kf20) sends F13 to F20 as Shift+F1 to Shift+F8.
		ev.Key -= KeyF13 - KeyF1
		mods |= ModShift
	}
	switch {
	case ev.Key >= KeyF1 && ev.Key <= KeyF4:
		return appendSS3(dst, byte('P'+(ev.Key-KeyF1)), mods)
	case ev.Key >= KeyF5 && ev.Key <= KeyF12:
		codes := [...]int{15, 17, 18, 19, 20, 21, 23, 24}
		return appendTilde(dst, codes[ev.Key-KeyF5], mods)
	}
	return appendKeypad(ev, m, dst)
}

// cursorFinals are the final bytes of Up, Down, Right, Left, Home and End; editCodes the numbers of PageUp, PageDown, Insert
// and Delete in CSI n ~.
var (
	cursorFinals = [...]byte{'A', 'B', 'C', 'D', 'H', 'F'}
	editCodes    = [...]int{5, 6, 2, 3}
)

// appendMeta appends the byte b (a control character or a character), with ESC before it for Alt.
func appendMeta(dst []byte, alt, escape bool, b rune) []byte {
	if alt {
		if escape {
			dst = append(dst, 0x1b)
		} else if b < 0x80 {
			return utf8.AppendRune(dst, b|0x80)
		}
	}
	return utf8.AppendRune(dst, b)
}

// appendSS3 writes SS3 final, or, with modifiers, CSI 1 ; m final (ctlseqs: "F1 to F4 change their prefix to CSI with a
// modifier").
func appendSS3(dst []byte, final byte, mods Mod) []byte {
	if mods&(ModShift|ModAlt|ModCtrl|ModMeta) != 0 {
		dst = append(dst, 0x1b, '[', '1', ';')
		dst = strconv.AppendInt(dst, int64(modParam(mods)), 10)
		return append(dst, final)
	}
	return append(dst, 0x1b, 'O', final)
}

// appendTilde writes CSI n ~ or CSI n ; m ~.
func appendTilde(dst []byte, n int, mods Mod) []byte {
	dst = append(dst, 0x1b, '[')
	dst = strconv.AppendInt(dst, int64(n), 10)
	if mods&(ModShift|ModAlt|ModCtrl|ModMeta) != 0 {
		dst = append(dst, ';')
		dst = strconv.AppendInt(dst, int64(modParam(mods)), 10)
	}
	return append(dst, '~')
}

// appendKeypad writes the keypad keys: their characters, or in application keypad mode SS3 and a letter (ctlseqs:
// "VT100/VT220 Keypad": 0 to 9 are p to y, - m, + k, * j, / o, . n, , l, = X, Enter M).
func appendKeypad(ev KeyEvent, m KeyModes, dst []byte) []byte {
	var ch byte
	var app byte
	switch {
	case ev.Key >= KeyKP0 && ev.Key <= KeyKP9:
		d := byte(ev.Key - KeyKP0)
		ch, app = '0'+d, 'p'+d
	case ev.Key == KeyKPAdd:
		ch, app = '+', 'k'
	case ev.Key == KeyKPSubtract:
		ch, app = '-', 'm'
	case ev.Key == KeyKPMultiply:
		ch, app = '*', 'j'
	case ev.Key == KeyKPDivide:
		ch, app = '/', 'o'
	case ev.Key == KeyKPDecimal:
		ch, app = '.', 'n'
	case ev.Key == KeyKPComma:
		ch, app = ',', 'l'
	case ev.Key == KeyKPEqual:
		ch, app = '=', 'X'
	default:
		return dst
	}
	if m.AppKeypad {
		return appendSS3(dst, app, ev.Mods)
	}
	if ev.Mods&ModAlt != 0 {
		dst = append(dst, 0x1b)
	}
	return append(dst, ch)
}

// EncodeFocus is the report of the window gaining or losing the focus when the program asked for it (ctlseqs: DECSET 1004:
// CSI I and CSI O); it returns dst unchanged otherwise.
func (e *Emulator) EncodeFocus(focused bool, dst []byte) []byte {
	e.mu.Lock()
	on := e.modes.FocusEvents
	e.mu.Unlock()
	if !on {
		return dst
	}
	if focused {
		return append(dst, 0x1b, '[', 'I')
	}
	return append(dst, 0x1b, '[', 'O')
}

// EncodePaste appends to dst the bytes of pasted text: line breaks become CR (a terminal sends Enter as CR), and when the
// program turned bracketed paste on (ctlseqs: DECSET 2004) the text is wrapped in CSI 200 ~ and CSI 201 ~ with the ESC
// characters of the text removed, so that it cannot end the paste itself.
func (e *Emulator) EncodePaste(text string, dst []byte) []byte {
	e.mu.Lock()
	bracketed := e.modes.BracketedPaste
	e.mu.Unlock()
	if bracketed {
		dst = append(dst, "\x1b[200~"...)
	}
	for i := 0; i < len(text); i++ {
		c := text[i]
		switch {
		case c == '\r' && i+1 < len(text) && text[i+1] == '\n':
		case c == '\n':
			dst = append(dst, '\r')
		case c == 0x1b && bracketed:
		default:
			dst = append(dst, c)
		}
	}
	if bracketed {
		dst = append(dst, "\x1b[201~"...)
	}
	return dst
}
