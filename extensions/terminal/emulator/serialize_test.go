package emulator

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"
)

// dump describes everything Serialize promises to carry over, as text that can be compared. A cell that was never written
// and a written space are the same.
func dump(e *Emulator) string {
	e.mu.Lock()
	defer e.mu.Unlock()
	var sb strings.Builder
	dumpScreen := func(name string, s *screen, wrapped bool) {
		cy := s.y
		if e.modes.Origin {
			cy = min(max(cy, s.top), s.bottom)
		}
		fmt.Fprintf(&sb, "%s %dx%d lines=%d cursor=(%d,%d) wrap=%v region=%d-%d\n", name, s.rows, s.cols, s.ring.n, s.x, cy, s.wrapNext, s.top, s.bottom)
		for i := 0; i < s.ring.n; i++ {
			l := s.ring.at(i)
			if wrapped {
				var next *line
				if i+1 < s.ring.n {
					next = s.ring.at(i + 1)
				}
				fmt.Fprintf(&sb, "[%v]", continues(l, next))
			}
			for _, c := range l.cells {
				if c.Flags&CellTail != 0 {
					sb.WriteString("_")
					continue
				}
				st := e.st.styles[c.Style]
				link := e.st.links[st.Link]
				st.Link = 0
				txt := " "
				switch {
				case c.Flags&CellCluster != 0:
					txt = l.clusterString(c)
				case c.Rune != 0:
					txt = string(rune(c.Rune))
				}
				fl := c.Flags &^ (CellWideFill | CellCluster)
				if c.Flags&CellWideFill != 0 {
					txt = " "
				}
				fmt.Fprintf(&sb, "<%s|%+v|%v|%v>", txt, st, link, fl)
			}
			sb.WriteString("\n")
		}
	}
	dumpScreen("main", e.mainScr, true)
	if e.modes.AltScreen {
		dumpScreen("alt", e.altScr, false)
	}
	m := e.modes
	m.SyncOutput = false
	fmt.Fprintf(&sb, "modes=%+v title=%q cwd=%q tabs=%v\n", m, e.title, e.cwd, e.scr.tabs)
	return sb.String()
}

func diffLines(a, b string) string {
	al, bl := strings.Split(a, "\n"), strings.Split(b, "\n")
	for i := 0; i < len(al) && i < len(bl); i++ {
		if al[i] == bl[i] {
			continue
		}
		ac, bc := strings.Split(al[i], ">"), strings.Split(bl[i], ">")
		for k := 0; k < len(ac) && k < len(bc); k++ {
			if ac[k] != bc[k] {
				return fmt.Sprintf("line %d cell %d:\n  got  %s\n  want %s", i, k, bc[k], ac[k])
			}
		}
		return fmt.Sprintf("line %d:\n  got  %s\n  want %s", i, bl[i], al[i])
	}
	return fmt.Sprintf("lengths %d and %d", len(al), len(bl))
}

// roundTrip serializes e and feeds the result to a new emulator of the same size.
func roundTrip(e *Emulator, scrollback int) *Emulator {
	rows, cols := e.Size()
	out := e.Serialize(-1)
	n := New(rows, cols, Options{Scrollback: scrollback})
	if _, err := n.Write(out); err != nil {
		panic(err)
	}
	return n
}

func TestSerializeExamples(t *testing.T) {
	cases := []struct {
		name string
		rows int
		cols int
		in   string
	}{
		{"plain", 5, 20, "hello\r\nworld"},
		{"empty", 5, 20, ""},
		{"colors", 5, 20, "\x1b[1;31mred\x1b[0m \x1b[38;2;1;2;3;48;5;100mmix\x1b[0m \x1b[4:3;58;2;1;2;3mcurly\x1b[0m"},
		{"background erase", 5, 20, "\x1b[44mblue\x1b[K\x1b[0m\r\nnext"},
		{"all attributes", 5, 30, "\x1b[1;2;3;4;5;7;8;9;53mx\x1b[0m"},
		{"wide and clusters", 5, 20, "日本語 😀 e\u0301x a\u0301\u0302"},
		{"wide at the edge", 5, 10, "012345678あい"},
		{"a wrapped line", 5, 10, strings.Repeat("0123456789", 3)},
		{"hyperlinks", 5, 30, "\x1b]8;;http://a\x1b\\link\x1b]8;id=1;http://b\x1b\\two\x1b]8;;\x1b\\ plain"},
		{"scrollback", 4, 10, "1\r\n2\r\n3\r\n4\r\n5\r\n6\r\n7"},
		{"scrollback with wrapping", 4, 5, strings.Repeat("abcdefghij\r\n", 4) + "klmnopqrst"},
		{"cursor in the middle", 5, 20, "abc\r\ndef\x1b[2;2H"},
		{"pending wrap", 5, 10, "0123456789"},
		{"pending wrap on a wide character", 5, 10, "01234567あ"},
		{"scroll region and origin mode", 6, 10, "1\r\n2\r\n3\r\n4\r\n5\x1b[2;5r\x1b[?6h\x1b[2;3H"},
		{"modes", 5, 20, "\x1b[?1h\x1b=\x1b[?7l\x1b[4h\x1b[20h\x1b[?25l\x1b[5 q\x1b[?2004h\x1b[?1004h\x1b[?1000h\x1b[?1006h\x1b[?1007h\x1b[?45h"},
		{"tab stops", 5, 30, "\x1b[3g\x1b[1;5H\x1bH\x1b[1;13H\x1bH\x1b[1;1H"},
		{"title and folder", 5, 20, "\x1b]2;my title\x07\x1b]7;file://host/home/me/dir%20x\x07"},
		{"palette", 5, 20, "\x1b]4;1;#112233\x07\x1b]10;#445566\x07\x1b]11;#778899\x07\x1b]12;#aabbcc\x07"},
		{"alternate screen", 5, 20, "main text\r\nmore\x1b[?1049h\x1b[2;2Halt \x1b[1mbold\x1b[0m\x1b[4;1Hlast"},
		{"alternate screen at the edge", 4, 10, "main\x1b[?1049h\x1b[4;1H0123456789"},
		{"the pen", 5, 20, "ab\x1b[1;34m"},
		{"the pen with a link", 5, 20, "ab\x1b]8;;http://x\x1b\\\x1b[3m"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := New(tc.rows, tc.cols, Options{Scrollback: 100})
			if _, err := a.Write([]byte(tc.in)); err != nil {
				t.Fatal(err)
			}
			b := roundTrip(a, 100)
			if da, db := dump(a), dump(b); da != db {
				t.Errorf("round trip differs:\n%s\nserialized: %q", diffLines(da, db), a.Serialize(-1))
			}
			// The pen carries over too.
			if a.pen != b.pen {
				t.Errorf("pen = %+v, want %+v", b.pen, a.pen)
			}
			// And a second round trip is the same bytes.
			if s1, s2 := a.Serialize(-1), b.Serialize(-1); string(s1) != string(s2) {
				t.Errorf("serializing twice differs:\n%q\n%q", s1, s2)
			}
		})
	}
	t.Run("the scrollback can be limited", func(t *testing.T) {
		a := New(3, 10, Options{Scrollback: 100})
		for i := 0; i < 50; i++ {
			if _, err := a.Write([]byte("line " + itoa(i) + "\r\n")); err != nil {
				t.Fatal(err)
			}
		}
		out := a.Serialize(5)
		b := New(3, 10, Options{Scrollback: 100})
		if _, err := b.Write(out); err != nil {
			t.Fatal(err)
		}
		assertEq(t, "scrollback", b.Scrollback(), 5)
		first, _ := b.LineText(0)
		assertEq(t, "first kept", first, "line 43")
		last, _ := b.LineText(b.LineCount() - 2)
		al, _ := a.LineText(a.LineCount() - 2)
		assertEq(t, "last", last, al)
	})
	t.Run("a used terminal is reset first", func(t *testing.T) {
		a := New(4, 10, Options{Scrollback: 10})
		if _, err := a.Write([]byte("fresh")); err != nil {
			t.Fatal(err)
		}
		b := New(4, 10, Options{Scrollback: 10})
		if _, err := b.Write([]byte("old stuff\r\nmore\x1b[?1h\x1b[1;31m\x1b[?1049h")); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Write(a.Serialize(-1)); err != nil {
			t.Fatal(err)
		}
		if da, db := dump(a), dump(b); da != db {
			t.Errorf("differs:\n%s", diffLines(da, db))
		}
	})
}

// TestSerializeRandom: any screen serialized and fed to a new emulator gives the same cells. The sequences are random but
// seeded; a seed that fails is printed, and EMETOS_SEED=<n> go test -run TestSerializeRandom repeats it.
func TestSerializeRandom(t *testing.T) {
	first, last := int64(1), int64(400)
	if testing.Short() {
		last = 60
	}
	if v, err := strconv.ParseInt(os.Getenv("EMETOS_SEEDS"), 10, 64); err == nil {
		last = v
	}
	if v, err := strconv.ParseInt(os.Getenv("EMETOS_SEED"), 10, 64); err == nil {
		first, last = v, v
	}
	for seed := first; seed <= last; seed++ {
		in, a := randomTerminal(seed)
		b := roundTrip(a, 200)
		if da, db := dump(a), dump(b); da != db {
			t.Fatalf("seed %d: round trip differs:\n%s\ninput: %q\nserialized: %q", seed, diffLines(da, db), in, a.Serialize(-1))
		}
	}
}

func randomTerminal(seed int64) (string, *Emulator) {
	rnd := rand.New(rand.NewSource(seed))
	rows, cols := 3+rnd.Intn(8), 4+rnd.Intn(30)
	e := New(rows, cols, Options{Scrollback: 200})
	var sb strings.Builder
	words := []string{"hello", "wörld", "日本語", "あ", "😀", "e\u0301", "x", "0123456789", "abcdefghijklmnopqrstuvwxyz", "ｱ", "\u0301", "a\u200db"}
	n := 20 + rnd.Intn(120)
	for i := 0; i < n; i++ {
		switch rnd.Intn(40) {
		case 0, 1, 2, 3, 4, 5:
			sb.WriteString(words[rnd.Intn(len(words))])
		case 6, 7:
			sb.WriteString(strings.Repeat(string(rune('a'+rnd.Intn(26))), 1+rnd.Intn(cols*2)))
		case 8:
			sb.WriteString("\r\n")
		case 9:
			sb.WriteString("\n")
		case 10:
			sb.WriteString("\r")
		case 11:
			sb.WriteString("\t")
		case 12:
			sb.WriteString("\b")
		case 13:
			fmt.Fprintf(&sb, "\x1b[%dm", []int{0, 1, 2, 3, 4, 5, 7, 8, 9, 21, 22, 23, 24, 27, 53, 30 + rnd.Intn(8), 40 + rnd.Intn(8), 90 + rnd.Intn(8), 39, 49}[rnd.Intn(20)])
		case 14:
			fmt.Fprintf(&sb, "\x1b[38;5;%dm", rnd.Intn(256))
		case 15:
			fmt.Fprintf(&sb, "\x1b[48;2;%d;%d;%dm", rnd.Intn(256), rnd.Intn(256), rnd.Intn(256))
		case 16:
			fmt.Fprintf(&sb, "\x1b[%d;%dH", 1+rnd.Intn(rows+1), 1+rnd.Intn(cols+1))
		case 17:
			fmt.Fprintf(&sb, "\x1b[%d%c", 1+rnd.Intn(5), "ABCD"[rnd.Intn(4)])
		case 18:
			fmt.Fprintf(&sb, "\x1b[%dJ", rnd.Intn(3))
		case 19:
			fmt.Fprintf(&sb, "\x1b[%dK", rnd.Intn(3))
		case 20:
			fmt.Fprintf(&sb, "\x1b[%d%c", 1+rnd.Intn(4), "@PXLM"[rnd.Intn(5)])
		case 21:
			fmt.Fprintf(&sb, "\x1b[%d%c", 1+rnd.Intn(3), "ST"[rnd.Intn(2)])
		case 22:
			t := 1 + rnd.Intn(rows)
			fmt.Fprintf(&sb, "\x1b[%d;%dr", t, t+1+rnd.Intn(rows))
		case 23:
			sb.WriteString([]string{"\x1b[?6h", "\x1b[?6l", "\x1b[?7h", "\x1b[?7l", "\x1b[4h", "\x1b[4l", "\x1b[20h", "\x1b[20l"}[rnd.Intn(8)])
		case 24:
			sb.WriteString([]string{"\x1b[?1049h", "\x1b[?1049l", "\x1b[?47h", "\x1b[?47l"}[rnd.Intn(4)])
		case 25:
			if rnd.Intn(2) == 0 {
				fmt.Fprintf(&sb, "\x1b]8;;http://example.com/%d\x1b\\", rnd.Intn(3))
			} else {
				sb.WriteString("\x1b]8;;\x1b\\")
			}
		case 26:
			sb.WriteString([]string{"\x1bH", "\x1b[g", "\x1b[3g", "\x1bM", "\x1bD", "\x1bE", "\x1b7", "\x1b8"}[rnd.Intn(8)])
		case 27:
			fmt.Fprintf(&sb, "\x1b]2;title %d\x07", rnd.Intn(100))
		case 28:
			sb.WriteString([]string{"\x1b[?1h", "\x1b[?1l", "\x1b=", "\x1b>", "\x1b[?25l", "\x1b[?25h", "\x1b[?2004h", "\x1b[?1000h", "\x1b[?1006h", "\x1b[?1003l"}[rnd.Intn(10)])
		case 29:
			sb.WriteString(strings.Repeat("あ", 1+rnd.Intn(cols)))
		case 30:
			fmt.Fprintf(&sb, "\x1b[%d q", rnd.Intn(7))
		case 31:
			fmt.Fprintf(&sb, "\x1b[%db", 1+rnd.Intn(10))
		case 32:
			sb.WriteString("\x1b(0lqk\x1b(B")
		default:
			sb.WriteString(strings.Repeat("text ", 1+rnd.Intn(8)))
		}
	}
	in := sb.String()
	// Split the input into random chunks, as a pseudo-terminal would deliver it.
	for len(in) > 0 {
		k := 1 + rnd.Intn(40)
		if k > len(in) {
			k = len(in)
		}
		if _, err := e.Write([]byte(in[:k])); err != nil {
			panic(err)
		}
		in = in[k:]
	}
	return sb.String(), e
}
