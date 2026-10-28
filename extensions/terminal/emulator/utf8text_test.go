package emulator

import (
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestAppendValidUTF8(t *testing.T) {
	tests := []struct {
		name     string
		src      string
		final    bool
		want     string
		wantHeld int
	}{
		{"ascii", "plain text", false, "plain text", 0},
		{"valid multibyte", "café 日本 \U0001F600", false, "café 日本 \U0001F600", 0},
		{"a lone continuation byte", "a\x80b", false, "a�b", 0},
		{"two bad bytes are two characters", "\xff\xfe", false, "��", 0},
		{"an overlong C0 80", "\xc0\x80", false, "��", 0},
		{"a surrogate ED A0 80 is three", "\xed\xa0\x80", false, "���", 0},
		{"a cut euro sign inside the text is one", "a\xe2\x82b", false, "a�b", 0},
		{"a cut euro sign at the end waits", "a\xe2\x82", false, "a", 2},
		{"a cut euro sign at the end of the stream is one", "a\xe2\x82", true, "a�", 0},
		{"the lead of four waits", "x\xf0", false, "x", 1},
		{"a start that cannot go on is not waited for", "x\xf0\x28", false, "x�(", 0},
		{"F4 90 is out of range", "\xf4\x90\x80\x80", false, "����", 0},
		{"E0 80 is overlong", "\xe0\x80\xaf", false, "���", 0},
		{"a complete four byte character", "\xf0\x9f\x98\x80", false, "\U0001F600", 0},
		{"empty", "", false, "", 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, held := AppendValidUTF8([]byte("<"), []byte(tt.src), tt.final)
			if string(got) != "<"+tt.want || held != tt.wantHeld {
				t.Errorf("AppendValidUTF8(%q, %v) = %q, held %d; want %q, held %d", tt.src, tt.final, got, held, "<"+tt.want, tt.wantHeld)
			}
			if !utf8.Valid(got) {
				t.Errorf("the result %q is not valid UTF-8", got)
			}
		})
	}
}

// Feeding the emulator the raw bytes and feeding it the text AppendValidUTF8 makes of them, chunk by chunk with the held
// bytes carried to the next one, draw the same cells.
func TestAppendValidUTF8IsWhatTheEmulatorReads(t *testing.T) {
	alphabet := []string{"a", "b c", "\n", "\xe2\x82\xac", "\xe2\x82", "\xe2", "\xf0\x9f\x98\x80", "\xf0\x9f\x98", "\xf0", "\x80", "\xbf", "\xc0", "\xc2", "\xc3\xa9",
		"\xed\xa0", "\xed\xa0\x80", "\xf4\x90", "\xff", "\xe0\x80", "\x1b[31m", "日本", "\x07", "\t"}
	for seed := int64(1); seed <= 200; seed++ {
		rng := rand.New(rand.NewSource(seed))
		var sb strings.Builder
		for range 40 {
			sb.WriteString(alphabet[rng.Intn(len(alphabet))])
		}
		raw := []byte(sb.String())
		a := New(10, 20, Options{Scrollback: 50})
		b := New(10, 20, Options{Scrollback: 50})
		var held []byte
		for len(raw) > 0 {
			n := 1 + rng.Intn(7)
			n = min(n, len(raw))
			chunk := raw[:n]
			raw = raw[n:]
			if _, err := a.Write(chunk); err != nil {
				t.Fatal(err)
			}
			in := append(append([]byte(nil), held...), chunk...)
			out, h := AppendValidUTF8(nil, in, false)
			held = append(held[:0], in[len(in)-h:]...)
			if _, err := b.Write(out); err != nil {
				t.Fatal(err)
			}
		}
		out, _ := AppendValidUTF8(nil, held, true)
		_, _ = b.Write(out)
		// the emulator keeps a cut sequence at the end waiting too: give both a character that ends it
		_, _ = a.Write([]byte("."))
		_, _ = b.Write([]byte("."))
		if x, y := a.Text(0, a.LineCount()-1), b.Text(0, b.LineCount()-1); x != y {
			t.Fatalf("seed %d: the emulator drew %q from the bytes and %q from the text", seed, x, y)
		}
	}
}

// Leaving the alternate screen puts back the pen that DECSET 1049 saved, so a snapshot taken meanwhile has to carry it.
func TestSerializeKeepsThePenThatLeavingTheAlternateScreenRestores(t *testing.T) {
	host := newRig(6, 30)
	host.feed("\x1b[33;45mcolored\x1b[?1049h\x1b[0mplain on the alternate screen")
	follower := newRig(6, 30)
	if _, err := follower.Write(host.Serialize(-1)); err != nil {
		t.Fatal(err)
	}
	const rest = "\x1b[?1049lafter"
	host.feed(rest)
	follower.feed(rest)
	for x := 0; x < 30; x++ {
		var a, b Frame
		host.ReadLines(0, host.LineCount(), &a)
		follower.ReadLines(0, follower.LineCount(), &b)
		if a.StyleOf(a.Cell(0, x)) != b.StyleOf(b.Cell(0, x)) || a.Text(0, x) != b.Text(0, x) {
			t.Fatalf("column %d: the host has %q in %+v and the follower %q in %+v", x, a.Text(0, x), a.StyleOf(a.Cell(0, x)), b.Text(0, x), b.StyleOf(b.Cell(0, x)))
		}
	}
}
