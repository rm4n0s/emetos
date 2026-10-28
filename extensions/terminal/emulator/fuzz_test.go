package emulator

import (
	"fmt"
	"testing"
)

// checkInvariants returns what is wrong with the state of the emulator, "" when it is sound: the facts that the rest of the
// code relies on (sizes, wide characters, indexes into the tables) and the bounds of SPEC §10.25 "Memory".
func checkInvariants(e *Emulator) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.rows < 1 || e.rows > MaxRows || e.cols < 1 || e.cols > MaxCols {
		return fmt.Sprintf("size %dx%d", e.rows, e.cols)
	}
	if e.nparams > MaxParams {
		return fmt.Sprintf("%d parameters", e.nparams)
	}
	if len(e.osc) > MaxOSCLength || len(e.dcsBuf) > maxDCS {
		return "a control string is over its cap"
	}
	if len(e.st.styles) > MaxStyles+1 || len(e.st.links) > MaxLinks+1 {
		return "a table is over its cap"
	}
	for _, s := range []*screen{e.mainScr, e.altScr} {
		if s.rows != e.rows || s.cols != e.cols {
			return fmt.Sprintf("screen %dx%d in a %dx%d terminal", s.rows, s.cols, e.rows, e.cols)
		}
		if s.x < 0 || s.x >= s.cols || s.y < 0 || s.y >= s.rows {
			return fmt.Sprintf("cursor (%d,%d) in %dx%d", s.x, s.y, s.cols, s.rows)
		}
		if s.top < 0 || s.bottom >= s.rows || s.top >= s.bottom && s.rows > 1 {
			return fmt.Sprintf("region %d-%d in %d rows", s.top, s.bottom, s.rows)
		}
		if len(s.tabs) != s.cols {
			return fmt.Sprintf("%d tab stops in %d columns", len(s.tabs), s.cols)
		}
		if s.ring.n < s.rows || s.ring.n > s.ring.max {
			return fmt.Sprintf("ring has %d lines, rows %d, max %d", s.ring.n, s.rows, s.ring.max)
		}
		if s.alt && s.ring.n != s.rows {
			return "the alternate screen has scrollback"
		}
		if !s.alt && s.scrollbackLen() > e.scrollback {
			return fmt.Sprintf("scrollback %d over its limit %d", s.scrollbackLen(), e.scrollback)
		}
		for i := 0; i < s.ring.n; i++ {
			l := s.ring.at(i)
			if len(l.cells) != s.cols {
				return fmt.Sprintf("line %d has %d cells, want %d", i, len(l.cells), s.cols)
			}
			for x, c := range l.cells {
				switch {
				case c.Flags&CellWide != 0:
					if x+1 >= s.cols || l.cells[x+1].Flags&CellTail == 0 {
						return fmt.Sprintf("line %d: wide character at %d has no right half", i, x)
					}
				case c.Flags&CellTail != 0:
					if x == 0 || l.cells[x-1].Flags&CellWide == 0 {
						return fmt.Sprintf("line %d: right half at %d has no left half", i, x)
					}
				}
				if c.Flags&CellCluster != 0 && (l.ext == nil || int(c.Rune) >= len(l.ext.clusters)) {
					return fmt.Sprintf("line %d: cell %d has no cluster", i, x)
				}
				if int(c.Style) >= len(e.st.styles) {
					return fmt.Sprintf("line %d: cell %d has style %d of %d", i, x, c.Style, len(e.st.styles))
				}
				if st := e.st.styles[c.Style]; int(st.Link) >= len(e.st.links) {
					return fmt.Sprintf("style %d has link %d of %d", c.Style, st.Link, len(e.st.links))
				}
			}
			if l.ext != nil && len(l.ext.clusters) > 2*len(l.cells)+17 {
				return fmt.Sprintf("line %d keeps %d cluster strings", i, len(l.ext.clusters))
			}
		}
	}
	return ""
}

var fuzzSeeds = []string{
	"plain text\r\nmore text",
	"\x1b[1;31mred\x1b[0m \x1b[38;2;1;2;3mtrue\x1b[48;5;200mbg\x1b[m",
	"\x1b[2J\x1b[H\x1b[3;4Hx\x1b[10A\x1b[10B\x1b[10C\x1b[10D\x1b[99;99H",
	"\x1b[?1049h\x1b[2;4r\x1b[5T\x1b[5S\x1b[3L\x1b[3M\x1b[?1049l",
	"\x1b]0;title\x07\x1b]8;;http://x\x1b\\link\x1b]8;;\x1b\\\x1b]4;1;?\x1b\\\x1b]52;c;ZGF0YQ==\x07",
	"\x1bP$qm\x1b\\\x1bP1;2|data\x1b\\\x1b_apc\x1b\\\x1bXsos\x1b\\\x1b^pm\x1b\\",
	"日本語😀e\u0301a\u200db\x1b[1@\x1b[1P\x1b[3X",
	"\u009b1;2H\u0084\u0085\u0088\u008d\u009d0;t\u009c\u0090x\u009c",
	"\xff\xfe\xe3\x81\xf0\x9f\x98\xc0\x80\xed\xa0\x80",
	"\x1b[?7l\x1b[4h\x1b[20h\x1b[?6h\x1b[?2004h\x1b[?1000h\x1b[?1006h\x1b[?1007h\x1b[?25l",
	"\x1b(0lqk\x1b(B\x0e\x0f\x1b)0\x1b#8\x1bc\x1b7\x1b8\x1bD\x1bE\x1bH\x1bM",
	"\x1b[6n\x1b[5n\x1b[c\x1b[>c\x1b[?6n\x1b[18t\x1b[?7$p\x1b[4$p",
	"\x1b[3g\x1b[1;5H\x1bH\t\t\x1b[2I\x1b[3Z",
	"\x1b[1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1;1m",
	"\x1b[99999999999999999999;99999999999999999999H\x1b[99999999999@",
	"\x1b[38:2::1:2:3;4:3;58:2:1:2:3m\x1b[38:5:9m",
	"\x1b[!p\x1b[0 q\x1b[6 q\x1b[?47h\x1b[?47l\x1b[?1047h\x1b[?1047l",
	"abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz0123456789\x1b[1S",
	"\x1b[1;2;3;4;5;6;7;8;9;10;11;12;13;14;15;16;17;18;19;20;21;22;23;24;25;26;27;28;29;30;31;32;33;34;35;36;37;38;39;40$p",
	"\x1b\x1b\x1b[\x1b]\x1bP\x18\x1a\x1b[1\x18m",
}

// FuzzParser feeds arbitrary bytes, cut anywhere, with resizes in between, and checks the invariants, a snapshot, a
// serialization and the encoders. Run it with the limits of CLAUDE.md "Memory":
//
//	GOMEMLIMIT=2GiB go test ./extensions/terminal/emulator -run xxx -fuzz FuzzParser -parallel=1 -fuzztime=30s
func FuzzParser(f *testing.F) {
	for i, s := range fuzzSeeds {
		f.Add([]byte(s), uint8(10+i), uint8(20+3*i), uint8(i))
	}
	f.Fuzz(func(t *testing.T, data []byte, rows, cols, cut uint8) {
		if len(data) > 1<<16 {
			data = data[:1<<16]
		}
		e := New(int(rows%50)+1, int(cols%150)+1, Options{Scrollback: 40, Callbacks: Callbacks{
			Reply: func([]byte) {}, OSC: func(OSCEvent) {}, Title: func(string) {},
		}})
		k := 0
		if len(data) > 0 {
			k = int(cut) % (len(data) + 1)
		}
		if _, err := e.Write(data[:k]); err != nil {
			t.Fatal(err)
		}
		e.Resize(int(cols%40)+1, int(rows%100)+1)
		if _, err := e.Write(data[k:]); err != nil {
			t.Fatal(err)
		}
		if msg := checkInvariants(e); msg != "" {
			t.Fatalf("unsound state: %s", msg)
		}
		var fr Frame
		e.Snapshot(&fr)
		e.ReadLines(0, e.LineCount(), &fr)
		out := e.Serialize(-1)
		n := New(e.rows, e.cols, Options{Scrollback: 40})
		if _, err := n.Write(out); err != nil {
			t.Fatal(err)
		}
		if msg := checkInvariants(n); msg != "" {
			t.Fatalf("unsound state after a round trip: %s", msg)
		}
		e.EncodeMouse(MouseEvent{X: int(cols), Y: int(rows)}, nil)
		e.EncodePaste(string(data), nil)
		_ = e.Text(0, e.LineCount())
	})
}

// TestFuzzSeedsAreSound runs the seed corpus through the invariants with several sizes and cuts, without the fuzzer.
func TestFuzzSeedsAreSound(t *testing.T) {
	for i, s := range fuzzSeeds {
		for _, size := range [][2]int{{1, 1}, {2, 3}, {24, 80}, {5, 200}} {
			for cut := 0; cut <= len(s); cut += 1 + len(s)/7 {
				e := New(size[0], size[1], Options{Scrollback: 20})
				if _, err := e.Write([]byte(s[:cut])); err != nil {
					t.Fatal(err)
				}
				e.Resize(size[1]%30+1, size[0]%50+2)
				if _, err := e.Write([]byte(s[cut:])); err != nil {
					t.Fatal(err)
				}
				if msg := checkInvariants(e); msg != "" {
					t.Errorf("seed %d, size %v, cut %d: %s", i, size, cut, msg)
				}
			}
		}
	}
}

// TestRandomTerminalsAreSound checks the invariants on the random terminals of the round trip test.
func TestRandomTerminalsAreSound(t *testing.T) {
	for seed := int64(1); seed <= 300; seed++ {
		in, e := randomTerminal(seed)
		if msg := checkInvariants(e); msg != "" {
			t.Fatalf("seed %d: %s\ninput: %q", seed, msg, in)
		}
		// And after a resize.
		e.Resize(3+int(seed%7), 5+int(seed%23))
		if msg := checkInvariants(e); msg != "" {
			t.Fatalf("seed %d after a resize: %s\ninput: %q", seed, msg, in)
		}
	}
}
