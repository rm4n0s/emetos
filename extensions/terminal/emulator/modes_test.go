package emulator

import "testing"

// TestModes: SM, RM, DECSET and DECRST (ECMA-48 8.3.125, 8.3.106; ctlseqs "CSI Pm h", "CSI ? Pm h").
func TestModes(t *testing.T) {
	cases := []struct {
		name string
		in   string
		get  func(Modes) any
		want any
	}{
		{"autowrap on at the start", "", func(m Modes) any { return m.Autowrap }, true},
		{"cursor visible at the start", "", func(m Modes) any { return m.CursorVisible }, true},
		{"meta sends escape at the start", "", func(m Modes) any { return m.MetaSendsEscape }, true},
		{"DECCKM", "\x1b[?1h", func(m Modes) any { return m.AppCursorKeys }, true},
		{"DECCKM reset", "\x1b[?1h\x1b[?1l", func(m Modes) any { return m.AppCursorKeys }, false},
		{"DECKPAM", "\x1b=", func(m Modes) any { return m.AppKeypad }, true},
		{"DECKPNM", "\x1b=\x1b>", func(m Modes) any { return m.AppKeypad }, false},
		{"DECNKM", "\x1b[?66h", func(m Modes) any { return m.AppKeypad }, true},
		{"DECSCNM", "\x1b[?5h", func(m Modes) any { return m.ReverseVideo }, true},
		{"DECOM", "\x1b[?6h", func(m Modes) any { return m.Origin }, true},
		{"DECAWM off", "\x1b[?7l", func(m Modes) any { return m.Autowrap }, false},
		{"att610 blink", "\x1b[?12h", func(m Modes) any { return m.CursorBlink }, true},
		{"DECTCEM off", "\x1b[?25l", func(m Modes) any { return m.CursorVisible }, false},
		{"reverse wrap", "\x1b[?45h", func(m Modes) any { return m.ReverseWrap }, true},
		{"IRM", "\x1b[4h", func(m Modes) any { return m.Insert }, true},
		{"IRM reset", "\x1b[4h\x1b[4l", func(m Modes) any { return m.Insert }, false},
		{"LNM", "\x1b[20h", func(m Modes) any { return m.NewLine }, true},
		{"several modes in one sequence", "\x1b[?1;7;25l", func(m Modes) any { return [3]bool{m.AppCursorKeys, m.Autowrap, m.CursorVisible} }, [3]bool{false, false, false}},
		{"X10 mouse", "\x1b[?9h", func(m Modes) any { return m.Mouse }, MouseX10},
		{"normal mouse", "\x1b[?1000h", func(m Modes) any { return m.Mouse }, MouseNormal},
		{"button mouse", "\x1b[?1002h", func(m Modes) any { return m.Mouse }, MouseDrag},
		{"any mouse", "\x1b[?1003h", func(m Modes) any { return m.Mouse }, MouseAny},
		{"mouse reset", "\x1b[?1003h\x1b[?1003l", func(m Modes) any { return m.Mouse }, MouseNone},
		{"another mouse mode replaces", "\x1b[?1000h\x1b[?1002h", func(m Modes) any { return m.Mouse }, MouseDrag},
		{"UTF-8 encoding", "\x1b[?1005h", func(m Modes) any { return m.MouseEnc }, MouseEncUTF8},
		{"SGR encoding", "\x1b[?1006h", func(m Modes) any { return m.MouseEnc }, MouseEncSGR},
		{"URXVT encoding", "\x1b[?1015h", func(m Modes) any { return m.MouseEnc }, MouseEncURXVT},
		{"encoding reset", "\x1b[?1006h\x1b[?1006l", func(m Modes) any { return m.MouseEnc }, MouseEncDefault},
		{"focus events", "\x1b[?1004h", func(m Modes) any { return m.FocusEvents }, true},
		{"alternate scroll", "\x1b[?1007h", func(m Modes) any { return m.AltScroll }, true},
		{"meta off", "\x1b[?1036l", func(m Modes) any { return m.MetaSendsEscape }, false},
		{"bracketed paste", "\x1b[?2004h", func(m Modes) any { return m.BracketedPaste }, true},
		{"synchronized output", "\x1b[?2026h", func(m Modes) any { return m.SyncOutput }, true},
		{"alternate screen 1049", "\x1b[?1049h", func(m Modes) any { return m.AltScreen }, true},
		{"alternate screen 47", "\x1b[?47h", func(m Modes) any { return m.AltScreen }, true},
		{"alternate screen left", "\x1b[?1049h\x1b[?1049l", func(m Modes) any { return m.AltScreen }, false},
		{"unknown modes change nothing", "\x1b[?9999h\x1b[?3h\x1b[?40h\x1b[99h", func(m Modes) any { return m.Autowrap }, true},
		{"DECSCUSR block", "\x1b[2 q", func(m Modes) any { return [2]any{m.CursorShape, m.CursorBlink} }, [2]any{CursorBlock, false}},
		{"DECSCUSR blinking underline", "\x1b[3 q", func(m Modes) any { return [2]any{m.CursorShape, m.CursorBlink} }, [2]any{CursorUnderline, true}},
		{"DECSCUSR steady underline", "\x1b[4 q", func(m Modes) any { return [2]any{m.CursorShape, m.CursorBlink} }, [2]any{CursorUnderline, false}},
		{"DECSCUSR blinking bar", "\x1b[5 q", func(m Modes) any { return [2]any{m.CursorShape, m.CursorBlink} }, [2]any{CursorBar, true}},
		{"DECSCUSR steady bar", "\x1b[6 q", func(m Modes) any { return [2]any{m.CursorShape, m.CursorBlink} }, [2]any{CursorBar, false}},
		{"DECSCUSR 0 is a blinking block", "\x1b[6 q\x1b[0 q", func(m Modes) any { return [2]any{m.CursorShape, m.CursorBlink} }, [2]any{CursorBlock, true}},
		{"DECSCUSR out of range changes nothing", "\x1b[4 q\x1b[9 q", func(m Modes) any { return m.CursorShape }, CursorUnderline},
		{"XTSAVE and XTRESTORE", "\x1b[?1h\x1b[?1s\x1b[?1l\x1b[?1r", func(m Modes) any { return m.AppCursorKeys }, true},
		{"XTRESTORE without a save changes nothing", "\x1b[?1h\x1b[?1r", func(m Modes) any { return m.AppCursorKeys }, true},
		{"DECSTR keeps the screen mode", "\x1b[?1049h\x1b[?1h\x1b[!p", func(m Modes) any { return [2]bool{m.AltScreen, m.AppCursorKeys} }, [2]bool{true, false}},
		{"RIS resets all", "\x1b[?1h\x1b[?1000h\x1b[?2004h\x1bc", func(m Modes) any { return m.AppCursorKeys || m.BracketedPaste || m.Mouse != MouseNone }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(5, 10)
			r.feed(tc.in)
			if got := tc.get(r.Modes()); got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestReverseVideoAndScrollbackAPI(t *testing.T) {
	t.Run("reverse video marks everything", func(t *testing.T) {
		r := newRig(3, 10)
		var f Frame
		r.Snapshot(&f)
		r.feed("\x1b[?5h")
		r.Snapshot(&f)
		assertEq(t, "full", f.Full, true)
		assertEq(t, "mode", f.Modes.ReverseVideo, true)
	})
	t.Run("SetScrollback drops the oldest lines", func(t *testing.T) {
		r := newRigSB(3, 10, 100)
		for i := 0; i < 50; i++ {
			r.feed(itoa(i) + "\r\n")
		}
		assertEq(t, "before", r.Scrollback(), 48)
		r.SetScrollback(5)
		assertEq(t, "after", r.Scrollback(), 5)
		first, _ := r.LineText(0)
		assertEq(t, "oldest", first, "43")
		r.SetScrollback(0)
		assertEq(t, "none", r.Scrollback(), 0)
		r.SetScrollback(10)
		r.feed("x\r\ny\r\nz\r\n")
		assertEq(t, "grows again", r.Scrollback(), 3)
	})
	t.Run("Reset", func(t *testing.T) {
		r := newRig(3, 10)
		r.feed("abc\x1b[?1h\x1b]2;t\x07")
		r.Reset()
		assertEq(t, "screen", r.screenString(), "")
		assertEq(t, "mode", r.Modes().AppCursorKeys, false)
	})
	t.Run("RIS keeps the scrollback", func(t *testing.T) {
		r := newRigSB(3, 10, 100)
		r.feed("1\r\n2\r\n3\r\n4\r\n5")
		r.feed("\x1bc")
		assertEq(t, "scrollback", r.Scrollback(), 2)
	})
}
