package emulator

import "testing"

// TestEncodeKey: the PC-style keys of ctlseqs ("PC-Style Function Keys", "VT100/VT220 Keypad", "Alt and Meta Keys").
func TestEncodeKey(t *testing.T) {
	normal := KeyModes{MetaSendsEscape: true}
	app := KeyModes{AppCursor: true, AppKeypad: true, MetaSendsEscape: true}
	eight := KeyModes{}
	nl := KeyModes{NewLine: true, MetaSendsEscape: true}
	ch := func(r rune, m Mod) KeyEvent { return KeyEvent{Key: KeyRune, Rune: r, Mods: m} }
	k := func(key Key, m Mod) KeyEvent { return KeyEvent{Key: key, Mods: m} }
	cases := []struct {
		name  string
		ev    KeyEvent
		modes KeyModes
		want  string
	}{
		{"letter", ch('a', 0), normal, "a"},
		{"shifted letter", ch('A', ModShift), normal, "A"},
		{"UTF-8", ch('é', 0), normal, "é"},
		{"CJK", ch('日', 0), normal, "日"},
		{"emoji", ch('😀', 0), normal, "😀"},
		{"Ctrl+A", ch('a', ModCtrl), normal, "\x01"},
		{"Ctrl+Z", ch('z', ModCtrl), normal, "\x1a"},
		{"Ctrl+Shift+A is the same", ch('A', ModCtrl|ModShift), normal, "\x01"},
		{"Ctrl+C", ch('c', ModCtrl), normal, "\x03"},
		{"Ctrl+Space", ch(' ', ModCtrl), normal, "\x00"},
		{"Ctrl+@", ch('@', ModCtrl), normal, "\x00"},
		{"Ctrl+[", ch('[', ModCtrl), normal, "\x1b"},
		{"Ctrl+\\", ch('\\', ModCtrl), normal, "\x1c"},
		{"Ctrl+]", ch(']', ModCtrl), normal, "\x1d"},
		{"Ctrl+^", ch('^', ModCtrl), normal, "\x1e"},
		{"Ctrl+_", ch('_', ModCtrl), normal, "\x1f"},
		{"Ctrl+/", ch('/', ModCtrl), normal, "\x1f"},
		{"Ctrl+?", ch('?', ModCtrl), normal, "\x7f"},
		{"Ctrl+2", ch('2', ModCtrl), normal, "\x00"},
		{"Ctrl+3", ch('3', ModCtrl), normal, "\x1b"},
		{"Ctrl+8", ch('8', ModCtrl), normal, "\x7f"},
		{"Ctrl+1 has no control character", ch('1', ModCtrl), normal, "1"},
		{"Alt+a sends ESC a", ch('a', ModAlt), normal, "\x1ba"},
		{"Alt+a sets the eighth bit without MetaSendsEscape", ch('a', ModAlt), eight, "á"},
		{"Alt+Ctrl+a", ch('a', ModAlt|ModCtrl), normal, "\x1b\x01"},
		{"Alt+é", ch('é', ModAlt), normal, "\x1bé"},
		{"Enter", k(KeyEnter, 0), normal, "\r"},
		{"Enter in new line mode", k(KeyEnter, 0), nl, "\r\n"},
		{"Alt+Enter", k(KeyEnter, ModAlt), normal, "\x1b\r"},
		{"Shift+Enter", k(KeyEnter, ModShift), normal, "\r"},
		{"Tab", k(KeyTab, 0), normal, "\t"},
		{"Shift+Tab", k(KeyTab, ModShift), normal, "\x1b[Z"},
		{"Alt+Tab", k(KeyTab, ModAlt), normal, "\x1b\t"},
		{"Backspace", k(KeyBackspace, 0), normal, "\x7f"},
		{"Ctrl+Backspace", k(KeyBackspace, ModCtrl), normal, "\x08"},
		{"Alt+Backspace", k(KeyBackspace, ModAlt), normal, "\x1b\x7f"},
		{"Escape", k(KeyEscape, 0), normal, "\x1b"},
		{"Alt+Escape", k(KeyEscape, ModAlt), normal, "\x1b\x1b"},
		{"Up", k(KeyUp, 0), normal, "\x1b[A"},
		{"Down", k(KeyDown, 0), normal, "\x1b[B"},
		{"Right", k(KeyRight, 0), normal, "\x1b[C"},
		{"Left", k(KeyLeft, 0), normal, "\x1b[D"},
		{"Home", k(KeyHome, 0), normal, "\x1b[H"},
		{"End", k(KeyEnd, 0), normal, "\x1b[F"},
		{"Up in application mode", k(KeyUp, 0), app, "\x1bOA"},
		{"Down in application mode", k(KeyDown, 0), app, "\x1bOB"},
		{"Right in application mode", k(KeyRight, 0), app, "\x1bOC"},
		{"Left in application mode", k(KeyLeft, 0), app, "\x1bOD"},
		{"Home in application mode", k(KeyHome, 0), app, "\x1bOH"},
		{"End in application mode", k(KeyEnd, 0), app, "\x1bOF"},
		{"Shift+Up", k(KeyUp, ModShift), normal, "\x1b[1;2A"},
		{"Alt+Up", k(KeyUp, ModAlt), normal, "\x1b[1;3A"},
		{"Shift+Alt+Up", k(KeyUp, ModShift|ModAlt), normal, "\x1b[1;4A"},
		{"Ctrl+Up", k(KeyUp, ModCtrl), normal, "\x1b[1;5A"},
		{"Shift+Ctrl+Up", k(KeyUp, ModShift|ModCtrl), normal, "\x1b[1;6A"},
		{"Alt+Ctrl+Up", k(KeyUp, ModAlt|ModCtrl), normal, "\x1b[1;7A"},
		{"Shift+Alt+Ctrl+Up", k(KeyUp, ModShift|ModAlt|ModCtrl), normal, "\x1b[1;8A"},
		{"Meta+Up", k(KeyUp, ModMeta), normal, "\x1b[1;9A"},
		{"Ctrl+Left in application mode", k(KeyLeft, ModCtrl), app, "\x1b[1;5D"},
		{"Ctrl+Home", k(KeyHome, ModCtrl), normal, "\x1b[1;5H"},
		{"Ctrl+End", k(KeyEnd, ModCtrl), normal, "\x1b[1;5F"},
		{"Insert", k(KeyInsert, 0), normal, "\x1b[2~"},
		{"Delete", k(KeyDelete, 0), normal, "\x1b[3~"},
		{"PageUp", k(KeyPageUp, 0), normal, "\x1b[5~"},
		{"PageDown", k(KeyPageDown, 0), normal, "\x1b[6~"},
		{"Ctrl+Delete", k(KeyDelete, ModCtrl), normal, "\x1b[3;5~"},
		{"Shift+PageUp", k(KeyPageUp, ModShift), normal, "\x1b[5;2~"},
		{"Delete in application mode is the same", k(KeyDelete, 0), app, "\x1b[3~"},
		{"F1", k(KeyF1, 0), normal, "\x1bOP"},
		{"F2", k(KeyF2, 0), normal, "\x1bOQ"},
		{"F3", k(KeyF3, 0), normal, "\x1bOR"},
		{"F4", k(KeyF4, 0), normal, "\x1bOS"},
		{"F5", k(KeyF5, 0), normal, "\x1b[15~"},
		{"F6", k(KeyF6, 0), normal, "\x1b[17~"},
		{"F7", k(KeyF7, 0), normal, "\x1b[18~"},
		{"F8", k(KeyF8, 0), normal, "\x1b[19~"},
		{"F9", k(KeyF9, 0), normal, "\x1b[20~"},
		{"F10", k(KeyF10, 0), normal, "\x1b[21~"},
		{"F11", k(KeyF11, 0), normal, "\x1b[23~"},
		{"F12", k(KeyF12, 0), normal, "\x1b[24~"},
		{"F13 is Shift+F1 (terminfo kf13)", k(KeyF13, 0), normal, "\x1b[1;2P"},
		{"F14", k(KeyF14, 0), normal, "\x1b[1;2Q"},
		{"F15", k(KeyF15, 0), normal, "\x1b[1;2R"},
		{"F16", k(KeyF16, 0), normal, "\x1b[1;2S"},
		{"F17", k(KeyF17, 0), normal, "\x1b[15;2~"},
		{"F18", k(KeyF18, 0), normal, "\x1b[17;2~"},
		{"F19", k(KeyF19, 0), normal, "\x1b[18;2~"},
		{"F20", k(KeyF20, 0), normal, "\x1b[19;2~"},
		{"Ctrl+F13 is Shift+Ctrl+F1", k(KeyF13, ModCtrl), normal, "\x1b[1;6P"},
		{"Shift+F1 changes the prefix to CSI", k(KeyF1, ModShift), normal, "\x1b[1;2P"},
		{"Ctrl+F4", k(KeyF4, ModCtrl), normal, "\x1b[1;5S"},
		{"Shift+F5", k(KeyF5, ModShift), normal, "\x1b[15;2~"},
		{"Alt+Ctrl+F12", k(KeyF12, ModAlt|ModCtrl), normal, "\x1b[24;7~"},
		{"F1 in application mode is the same", k(KeyF1, 0), app, "\x1bOP"},
		{"keypad 5", k(KeyKP5, 0), normal, "5"},
		{"keypad 5 in application mode", k(KeyKP5, 0), app, "\x1bOu"},
		{"keypad 0 in application mode", k(KeyKP0, 0), app, "\x1bOp"},
		{"keypad 9 in application mode", k(KeyKP9, 0), app, "\x1bOy"},
		{"keypad +", k(KeyKPAdd, 0), normal, "+"},
		{"keypad + in application mode", k(KeyKPAdd, 0), app, "\x1bOk"},
		{"keypad -", k(KeyKPSubtract, 0), app, "\x1bOm"},
		{"keypad *", k(KeyKPMultiply, 0), app, "\x1bOj"},
		{"keypad /", k(KeyKPDivide, 0), app, "\x1bOo"},
		{"keypad .", k(KeyKPDecimal, 0), app, "\x1bOn"},
		{"keypad ,", k(KeyKPComma, 0), app, "\x1bOl"},
		{"keypad =", k(KeyKPEqual, 0), app, "\x1bOX"},
		{"keypad Enter", k(KeyKPEnter, 0), normal, "\r"},
		{"keypad Enter in application mode", k(KeyKPEnter, 0), app, "\x1bOM"},
		{"nothing for a rune of 0", ch(0, 0), normal, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(EncodeKey(tc.ev, tc.modes, nil))
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
	t.Run("the keys append", func(t *testing.T) {
		got := string(EncodeKey(ch('a', 0), normal, []byte("x")))
		assertEq(t, "appended", got, "xa")
	})
	t.Run("the modes come from the terminal", func(t *testing.T) {
		r := newRig(3, 10)
		assertEq(t, "normal", string(r.EncodeKey(k(KeyUp, 0), nil)), "\x1b[A")
		r.feed("\x1b[?1h\x1b=")
		assertEq(t, "application", string(r.EncodeKey(k(KeyUp, 0), nil)), "\x1bOA")
		assertEq(t, "keypad", string(r.EncodeKey(k(KeyKP1, 0), nil)), "\x1bOq")
		r.feed("\x1b[?1l\x1b>\x1b[20h")
		assertEq(t, "enter", string(r.EncodeKey(k(KeyEnter, 0), nil)), "\r\n")
		r.feed("\x1b[?1036l")
		assertEq(t, "alt", string(r.EncodeKey(ch('a', ModAlt), nil)), "á")
	})
}

// TestEncodePasteAndFocus: ctlseqs DECSET 2004 and 1004.
func TestEncodePasteAndFocus(t *testing.T) {
	r := newRig(3, 10)
	assertEq(t, "plain paste", string(r.EncodePaste("a\nb\r\nc", nil)), "a\rb\rc")
	assertEq(t, "ESC in a plain paste is kept", string(r.EncodePaste("a\x1bb", nil)), "a\x1bb")
	r.feed("\x1b[?2004h")
	assertEq(t, "bracketed", string(r.EncodePaste("a\nb", nil)), "\x1b[200~a\rb\x1b[201~")
	assertEq(t, "bracketed paste cannot end itself", string(r.EncodePaste("x\x1b[201~y", nil)), "\x1b[200~x[201~y\x1b[201~")
	assertEq(t, "focus off", string(r.EncodeFocus(true, nil)), "")
	r.feed("\x1b[?1004h")
	assertEq(t, "focus in", string(r.EncodeFocus(true, nil)), "\x1b[I")
	assertEq(t, "focus out", string(r.EncodeFocus(false, nil)), "\x1b[O")
}

// TestEncodeMouse: ctlseqs "Mouse Tracking".
func TestEncodeMouse(t *testing.T) {
	press := func(b MouseButton, x, y int, m Mod) MouseEvent {
		return MouseEvent{Button: b, Action: MousePress, X: x, Y: y, Mods: m}
	}
	release := func(b MouseButton, x, y int, m Mod) MouseEvent {
		return MouseEvent{Button: b, Action: MouseRelease, X: x, Y: y, Mods: m}
	}
	move := func(b MouseButton, x, y int) MouseEvent { return MouseEvent{Button: b, Action: MouseMove, X: x, Y: y} }
	cases := []struct {
		name string
		set  string
		evs  []MouseEvent
		want string
	}{
		{"off", "", []MouseEvent{press(MouseLeft, 0, 0, 0)}, ""},
		{"X10 press", "\x1b[?9h", []MouseEvent{press(MouseLeft, 0, 0, 0)}, "\x1b[M !!"},
		{"X10 right press", "\x1b[?9h", []MouseEvent{press(MouseRight, 4, 9, 0)}, "\x1b[M\"%*"},
		{"X10 ignores the release and modifiers", "\x1b[?9h", []MouseEvent{release(MouseLeft, 0, 0, 0), press(MouseLeft, 0, 0, ModCtrl)}, "\x1b[M !!"},
		{"normal press", "\x1b[?1000h", []MouseEvent{press(MouseLeft, 0, 0, 0)}, "\x1b[M !!"},
		{"normal middle press", "\x1b[?1000h", []MouseEvent{press(MouseMiddle, 1, 2, 0)}, "\x1b[M!\"#"},
		{"normal release names no button", "\x1b[?1000h", []MouseEvent{release(MouseRight, 0, 0, 0)}, "\x1b[M#!!"},
		{"normal shift", "\x1b[?1000h", []MouseEvent{press(MouseLeft, 0, 0, ModShift)}, "\x1b[M$!!"},
		{"normal alt", "\x1b[?1000h", []MouseEvent{press(MouseLeft, 0, 0, ModAlt)}, "\x1b[M(!!"},
		{"normal ctrl", "\x1b[?1000h", []MouseEvent{press(MouseLeft, 0, 0, ModCtrl)}, "\x1b[M0!!"},
		{"normal all modifiers", "\x1b[?1000h", []MouseEvent{press(MouseLeft, 0, 0, ModShift|ModAlt|ModCtrl)}, "\x1b[M<!!"},
		{"normal ignores motion", "\x1b[?1000h", []MouseEvent{move(MouseLeft, 3, 3)}, ""},
		{"wheel up", "\x1b[?1000h", []MouseEvent{press(MouseWheelUp, 0, 0, 0)}, "\x1b[M`!!"},
		{"wheel down", "\x1b[?1000h", []MouseEvent{press(MouseWheelDown, 0, 0, 0)}, "\x1b[Ma!!"},
		{"wheel left", "\x1b[?1000h", []MouseEvent{press(MouseWheelLeft, 0, 0, 0)}, "\x1b[Mb!!"},
		{"wheel right", "\x1b[?1000h", []MouseEvent{press(MouseWheelRight, 0, 0, 0)}, "\x1b[Mc!!"},
		{"wheel release is not reported", "\x1b[?1000h", []MouseEvent{release(MouseWheelUp, 0, 0, 0)}, ""},
		{"drag motion with a button", "\x1b[?1002h", []MouseEvent{move(MouseLeft, 5, 5)}, "\x1b[M@&&"},
		{"drag motion with button 3", "\x1b[?1002h", []MouseEvent{move(MouseRight, 5, 5)}, "\x1b[MB&&"},
		{"drag ignores motion with no button", "\x1b[?1002h", []MouseEvent{move(MouseNoButton, 5, 5)}, ""},
		{"drag reports a cell once", "\x1b[?1002h", []MouseEvent{move(MouseLeft, 5, 5), move(MouseLeft, 5, 5), move(MouseLeft, 6, 5)}, "\x1b[M@&&\x1b[M@'&"},
		{"any motion with no button", "\x1b[?1003h", []MouseEvent{move(MouseNoButton, 0, 0)}, "\x1b[MC!!"},
		{"a press resets the cell", "\x1b[?1003h", []MouseEvent{move(MouseNoButton, 0, 0), press(MouseLeft, 0, 0, 0), move(MouseNoButton, 0, 0)}, "\x1b[MC!!\x1b[M !!\x1b[MC!!"},
		{"default encoding cannot go beyond 222", "\x1b[?1000h", []MouseEvent{press(MouseLeft, 223, 0, 0)}, ""},
		{"default encoding at the limit", "\x1b[?1000h", []MouseEvent{press(MouseLeft, 222, 0, 0)}, "\x1b[M \xff!"},
		{"SGR press", "\x1b[?1000h\x1b[?1006h", []MouseEvent{press(MouseLeft, 0, 0, 0)}, "\x1b[<0;1;1M"},
		{"SGR release keeps the button", "\x1b[?1000h\x1b[?1006h", []MouseEvent{release(MouseRight, 9, 4, 0)}, "\x1b[<2;10;5m"},
		{"SGR large coordinates", "\x1b[?1000h\x1b[?1006h", []MouseEvent{press(MouseLeft, 999, 499, 0)}, "\x1b[<0;1000;500M"},
		{"SGR wheel", "\x1b[?1000h\x1b[?1006h", []MouseEvent{press(MouseWheelDown, 2, 3, 0)}, "\x1b[<65;3;4M"},
		{"SGR motion", "\x1b[?1003h\x1b[?1006h", []MouseEvent{move(MouseNoButton, 2, 3)}, "\x1b[<35;3;4M"},
		{"SGR ctrl", "\x1b[?1000h\x1b[?1006h", []MouseEvent{press(MouseLeft, 0, 0, ModCtrl)}, "\x1b[<16;1;1M"},
		{"UTF-8 small", "\x1b[?1000h\x1b[?1005h", []MouseEvent{press(MouseLeft, 0, 0, 0)}, "\x1b[M !!"},
		{"UTF-8 large", "\x1b[?1000h\x1b[?1005h", []MouseEvent{press(MouseLeft, 300, 0, 0)}, "\x1b[M " + string(rune(300+1+32)) + "!"},
		{"UTF-8 beyond the limit", "\x1b[?1000h\x1b[?1005h", []MouseEvent{press(MouseLeft, 2000, 0, 0)}, ""},
		{"URXVT press", "\x1b[?1000h\x1b[?1015h", []MouseEvent{press(MouseLeft, 0, 0, 0)}, "\x1b[32;1;1M"},
		{"URXVT release", "\x1b[?1000h\x1b[?1015h", []MouseEvent{release(MouseLeft, 4, 5, 0)}, "\x1b[35;5;6M"},
		{"reset turns it off", "\x1b[?1000h\x1b[?1000l", []MouseEvent{press(MouseLeft, 0, 0, 0)}, ""},
		{"another protocol replaces the first", "\x1b[?1000h\x1b[?1003h", []MouseEvent{move(MouseNoButton, 0, 0)}, "\x1b[MC!!"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(3, 10)
			r.feed(tc.set)
			var got []byte
			for _, ev := range tc.evs {
				got = r.EncodeMouse(ev, got)
			}
			assertEq(t, "reply", string(got), tc.want)
		})
	}
	t.Run("the wheel scrolls the alternate screen as cursor keys", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("\x1b[?1049h")
		assertEq(t, "off", string(r.EncodeMouse(press(MouseWheelUp, 0, 0, 0), nil)), "")
		r.feed("\x1b[?1007h")
		assertEq(t, "up", string(r.EncodeMouse(press(MouseWheelUp, 0, 0, 0), nil)), "\x1b[A")
		assertEq(t, "down", string(r.EncodeMouse(press(MouseWheelDown, 0, 0, 0), nil)), "\x1b[B")
		r.feed("\x1b[?1h")
		assertEq(t, "application", string(r.EncodeMouse(press(MouseWheelUp, 0, 0, 0), nil)), "\x1bOA")
		r.feed("\x1b[?1049l")
		assertEq(t, "main screen", string(r.EncodeMouse(press(MouseWheelUp, 0, 0, 0), nil)), "")
	})
}
