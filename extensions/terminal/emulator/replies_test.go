package emulator

import (
	"strings"
	"testing"
)

// TestReplies: the answers to the program (ECMA-48 8.3.24 DA, 8.3.35 DSR, ctlseqs "Device Status Report", "Send Device
// Attributes", "Request Mode", "Request Status String", "Window manipulation", XTVERSION).
func TestReplies(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"DSR 5", "\x1b[5n", "\x1b[0n"},
		{"CPR at home", "\x1b[6n", "\x1b[1;1R"},
		{"CPR", "\x1b[3;7H\x1b[6n", "\x1b[3;7R"},
		{"CPR in origin mode is relative", "\x1b[2;4r\x1b[?6h\x1b[2;3H\x1b[6n", "\x1b[2;3R"},
		{"DECXCPR", "\x1b[2;5H\x1b[?6n", "\x1b[?2;5;1R"},
		{"printer status", "\x1b[?15n", "\x1b[?13n"},
		{"UDK status", "\x1b[?25n", "\x1b[?21n"},
		{"keyboard status", "\x1b[?26n", "\x1b[?27;1;0;0n"},
		{"DA1", "\x1b[c", "\x1b[?1;2c"},
		{"DA1 with 0", "\x1b[0c", "\x1b[?1;2c"},
		{"DECID", "\x1bZ", "\x1b[?1;2c"},
		{"DA2", "\x1b[>c", "\x1b[>0;276;0c"},
		{"DA3", "\x1b[=c", "\x1bP!|00000000\x1b\\"},
		{"XTVERSION", "\x1b[>0q", "\x1bP>|Emetos\x1b\\"},
		{"report size in characters", "\x1b[18t", "\x1b[8;5;10t"},
		{"report screen size", "\x1b[19t", "\x1b[9;5;10t"},
		{"window operations that act are ignored", "\x1b[8;50;100t\x1b[1t\x1b[2t", ""},
		{"DECRQM autowrap set", "\x1b[?7$p", "\x1b[?7;1$y"},
		{"DECRQM autowrap reset", "\x1b[?7l\x1b[?7$p", "\x1b[?7;2$y"},
		{"DECRQM unknown", "\x1b[?9999$p", "\x1b[?9999;0$y"},
		{"DECRQM bracketed paste", "\x1b[?2004h\x1b[?2004$p", "\x1b[?2004;1$y"},
		{"DECRQM mouse", "\x1b[?1000h\x1b[?1000$p\x1b[?1002$p", "\x1b[?1000;1$y\x1b[?1002;2$y"},
		{"DECRQM synchronized output", "\x1b[?2026$p", "\x1b[?2026;2$y"},
		{"DECRQM grapheme clustering is not built", "\x1b[?2027$p", "\x1b[?2027;4$y"},
		{"DECRQM insert", "\x1b[4h\x1b[4$p", "\x1b[4;1$y"},
		{"DECRQM unknown ANSI mode", "\x1b[99$p", "\x1b[99;0$y"},
		{"DECRQSS SGR", "\x1b[1;31m\x1bP$qm\x1b\\", "\x1bP1$r0;1;31m\x1b\\"},
		{"DECRQSS SGR true color", "\x1b[38;2;1;2;3;4:3m\x1bP$qm\x1b\\", "\x1bP1$r0;4:3;38;2;1;2;3m\x1b\\"},
		{"DECRQSS default SGR", "\x1bP$qm\x1b\\", "\x1bP1$r0m\x1b\\"},
		{"DECRQSS DECSTBM", "\x1b[2;4r\x1bP$qr\x1b\\", "\x1bP1$r2;4r\x1b\\"},
		{"DECRQSS DECSCUSR", "\x1b[4 q\x1bP$q q\x1b\\", "\x1bP1$r4 q\x1b\\"},
		{"DECRQSS unknown", "\x1bP$qz\x1b\\", "\x1bP0$r\x1b\\"},
		{"DECRQSS with C1 ST", "\x1bP$qm\u009c", "\x1bP1$r0m\x1b\\"},
		{"OSC 4 query", "\x1b]4;1;?\x1b\\", "\x1b]4;1;rgb:cdcd/0000/0000\x1b\\"},
		{"OSC 4 query ends with BEL like the query", "\x1b]4;1;?\a", "\x1b]4;1;rgb:cdcd/0000/0000\a"},
		{"OSC 4 several queries", "\x1b]4;0;?;15;?\x1b\\", "\x1b]4;0;rgb:0000/0000/0000\x1b\\\x1b]4;15;rgb:ffff/ffff/ffff\x1b\\"},
		{"OSC 4 set then query", "\x1b]4;1;rgb:12/34/56\x1b\\\x1b]4;1;?\x1b\\", "\x1b]4;1;rgb:1212/3434/5656\x1b\\"},
		{"OSC 4 set with #", "\x1b]4;2;#aabbcc\x1b\\\x1b]4;2;?\x1b\\", "\x1b]4;2;rgb:aaaa/bbbb/cccc\x1b\\"},
		{"OSC 10 query", "\x1b]10;?\x1b\\", "\x1b]10;rgb:e5e5/e5e5/e5e5\x1b\\"},
		{"OSC 11 query", "\x1b]11;?\a", "\x1b]11;rgb:0000/0000/0000\a"},
		{"OSC 12 query", "\x1b]12;?\x1b\\", "\x1b]12;rgb:e5e5/e5e5/e5e5\x1b\\"},
		{"OSC 11 set then query", "\x1b]11;rgb:ff/00/80\x1b\\\x1b]11;?\x1b\\", "\x1b]11;rgb:ffff/0000/8080\x1b\\"},
		{"OSC 10 sets 10 and 11", "\x1b]10;#112233;#445566\x1b\\\x1b]10;?\x1b\\\x1b]11;?\x1b\\", "\x1b]10;rgb:1111/2222/3333\x1b\\\x1b]11;rgb:4444/5555/6666\x1b\\"},
		{"OSC 104 resets", "\x1b]4;1;#000001\x1b\\\x1b]104;1\x1b\\\x1b]4;1;?\x1b\\", "\x1b]4;1;rgb:cdcd/0000/0000\x1b\\"},
		{"OSC 104 without arguments resets all", "\x1b]4;1;#000001\x1b\\\x1b]104\x1b\\\x1b]4;1;?\x1b\\", "\x1b]4;1;rgb:cdcd/0000/0000\x1b\\"},
		{"OSC 111 resets the background", "\x1b]11;#010203\x1b\\\x1b]111\x1b\\\x1b]11;?\x1b\\", "\x1b]11;rgb:0000/0000/0000\x1b\\"},
		{"a bad color is ignored", "\x1b]4;1;rgb:zz/00/00\x1b\\\x1b]4;1;?\x1b\\", "\x1b]4;1;rgb:cdcd/0000/0000\x1b\\"},
		{"a palette index above 255 is ignored", "\x1b]4;300;?\x1b\\", ""},
		{"nothing is asked", "plain text", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(5, 10)
			r.feed(tc.in)
			assertEq(t, "reply", r.take(), tc.want)
		})
	}
	t.Run("one callback per write", func(t *testing.T) {
		calls := 0
		e := New(5, 10, Options{Callbacks: Callbacks{Reply: func([]byte) { calls++ }}})
		if _, err := e.Write([]byte("\x1b[5n\x1b[6n\x1b[c")); err != nil {
			t.Fatal(err)
		}
		assertEq(t, "calls", calls, 1)
	})
	t.Run("the version is part of XTVERSION", func(t *testing.T) {
		var got string
		e := New(5, 10, Options{Version: "1.2", Callbacks: Callbacks{Reply: func(p []byte) { got = string(p) }}})
		if _, err := e.Write([]byte("\x1b[>q")); err != nil {
			t.Fatal(err)
		}
		assertEq(t, "reply", got, "\x1bP>|Emetos(1.2)\x1b\\")
	})
	t.Run("the replies are in the order of the questions", func(t *testing.T) {
		r := newRig(5, 10)
		r.feed("\x1b[5n\x1b[1;2H\x1b[6n\x1b[c")
		assertEq(t, "reply", r.take(), "\x1b[0n\x1b[1;2R\x1b[?1;2c")
	})
	t.Run("a theme is reported", func(t *testing.T) {
		r := newRig(5, 10)
		var ansi [16]uint32
		ansi[1] = 0x112233
		r.SetTheme(ansi, 0xaabbcc, 0x010203, 0x405060)
		r.feed("\x1b]4;1;?\x1b\\\x1b]10;?\x1b\\\x1b]11;?\x1b\\\x1b]12;?\x1b\\")
		got := r.take()
		for _, want := range []string{"rgb:1111/2222/3333", "rgb:aaaa/bbbb/cccc", "rgb:0101/0202/0303", "rgb:4040/5050/6060"} {
			if !strings.Contains(got, want) {
				t.Errorf("reply %q lacks %q", got, want)
			}
		}
		r.feed("\x1bc\x1b]10;?\x1b\\")
		if got := r.take(); !strings.Contains(got, "aaaa/bbbb/cccc") {
			t.Errorf("the theme is lost by RIS: %q", got)
		}
	})
}
