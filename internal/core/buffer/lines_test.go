package buffer

import (
	"math/rand"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rm4n0s/errors"
)

// modelLineStarts are the offsets where the lines of s start.
func modelLineStarts(s string) []int {
	starts := []int{0}
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// modelPosition is VS Code's validatePosition written again on plain strings.
func modelPosition(lines []string, p Position) Position {
	if p.Line < 0 {
		return Position{}
	}
	if p.Line >= len(lines) {
		return Position{Line: len(lines) - 1, Col: len(lines[len(lines)-1])}
	}
	line := lines[p.Line]
	col := min(max(p.Col, 0), len(line))
	for col > 0 && col < len(line) && !utf8.RuneStart(line[col]) {
		col--
	}
	return Position{Line: p.Line, Col: col}
}

// checkAll checks the tree and everything the line index says against model.
func checkAll(t *testing.T, b *Buffer, model string) {
	t.Helper()
	checkTree(t, b, model)
	checkIndex(t, b.Snapshot(), model)
}

// checkIndex compares every answer of the line index with plain-string code. A
// long text is checked at a sample of offsets and positions, the same ones each
// time for the same text.
func checkIndex(t *testing.T, s Snapshot, model string) {
	t.Helper()
	lines := strings.Split(model, "\n")
	starts := modelLineStarts(model)
	if s.LineCount() != len(lines) {
		t.Fatalf("LineCount = %d, want %d", s.LineCount(), len(lines))
	}
	rng := rand.New(rand.NewSource(int64(len(model))))
	sample := func(n int) []int {
		if n <= 200 {
			all := make([]int, n)
			for i := range all {
				all[i] = i
			}
			return all
		}
		out := []int{0, n - 1, n / 2}
		for i := 0; i < 60; i++ {
			out = append(out, rng.Intn(n))
		}
		return out
	}
	for _, i := range sample(len(lines)) {
		got, err := s.LineContent(i)
		if err != nil || got != lines[i] {
			t.Fatalf("LineContent(%d) = %q, %v, want %q", i, got, err, lines[i])
		}
		if n, err := s.LineLength(i); err != nil || n != len(lines[i]) {
			t.Fatalf("LineLength(%d) = %d, %v, want %d", i, n, err, len(lines[i]))
		}
		if start, err := s.LineStart(i); err != nil || start != starts[i] {
			t.Fatalf("LineStart(%d) = %d, %v, want %d", i, start, err, starts[i])
		}
	}
	for _, off := range sample(len(model) + 1) {
		pos := s.PositionAt(off)
		line := len(modelLineStartsUpTo(starts, off)) - 1
		if want := (Position{Line: line, Col: off - starts[line]}); pos != want {
			t.Fatalf("PositionAt(%d) = %+v, want %+v", off, pos, want)
		}
		if got := s.OffsetAt(pos); got != off && atRuneStart(model, off) {
			t.Fatalf("OffsetAt(PositionAt(%d)) = %d", off, got)
		}
	}
	// Positions outside the text, and in the middle of characters.
	for _, li := range append(sample(len(lines)), -1, len(lines), len(lines)+5) {
		for _, col := range []int{-3, 0, 1, 2, 3, 5, 100} {
			p := Position{Line: li, Col: col}
			want := modelPosition(lines, p)
			if got := s.ValidatePosition(p); got != want {
				t.Fatalf("ValidatePosition(%+v) = %+v, want %+v", p, got, want)
			}
			if got, wantOff := s.OffsetAt(p), starts[want.Line]+want.Col; got != wantOff {
				t.Fatalf("OffsetAt(%+v) = %d, want %d", p, got, wantOff)
			}
		}
	}
	// Text of a range.
	for i := 0; i < 20; i++ {
		a, b := rng.Intn(len(model)+1), rng.Intn(len(model)+1)
		r := NewRange(s.PositionAt(a), s.PositionAt(b))
		vr := s.ValidateRange(r)
		want := model[starts[vr.Start.Line]+vr.Start.Col : starts[vr.End.Line]+vr.End.Col]
		if got := s.Text(r); got != want {
			t.Fatalf("Text(%+v) = %q, want %q", r, got, want)
		}
	}
	// Iterators.
	var sb strings.Builder
	for c := range s.Chunks() {
		if c == "" {
			t.Fatal("Chunks gave an empty chunk")
		}
		sb.WriteString(c)
	}
	if sb.String() != model {
		t.Fatalf("Chunks gave %d bytes that differ from the text of %d bytes", sb.Len(), len(model))
	}
	for _, from := range []int{-2, 0, len(lines) / 2, len(lines) - 1, len(lines), len(lines) + 3} {
		want := lines[min(max(from, 0), len(lines)):]
		next := max(from, 0)
		count := 0
		for i, text := range s.Lines(from) {
			if i != next || text != want[count] {
				t.Fatalf("Lines(%d) gave %d %q, want %d %q", from, i, text, next, want[count])
			}
			next++
			count++
		}
		if count != len(want) {
			t.Fatalf("Lines(%d) gave %d lines, want %d", from, count, len(want))
		}
	}
}

// modelLineStartsUpTo are the line starts at or before off.
func modelLineStartsUpTo(starts []int, off int) []int {
	n := 0
	for n < len(starts) && starts[n] <= off {
		n++
	}
	return starts[:n]
}

// atRuneStart reports whether off in s is at the start of a character or at the
// end of s.
func atRuneStart(s string, off int) bool {
	return off >= len(s) || utf8.RuneStart(s[off])
}

func TestLineAccessErrors(t *testing.T) {
	b := New("a\nbc")
	calls := map[string]func(int) error{
		"LineContent": func(l int) error { _, err := b.LineContent(l); return err },
		"LineLength":  func(l int) error { _, err := b.LineLength(l); return err },
		"LineStart":   func(l int) error { _, err := b.LineStart(l); return err },
	}
	for name, call := range calls {
		for _, line := range []int{-1, 2, 100} {
			appErr, ok := errors.FromError(call(line))
			if !ok {
				t.Fatalf("%s(%d): expected *errors.Error", name, line)
			}
			if !appErr.HasRoute(name) || !appErr.HasRoute("LineOutOfRange") {
				t.Errorf("%s(%d): unexpected failure path: %s", name, line, appErr.Route())
			}
		}
		if err := call(1); err != nil {
			t.Errorf("%s(1): %v", name, err)
		}
	}
}

func TestLinesStopWhenAsked(t *testing.T) {
	b := New(strings.Repeat("line\n", 5000))
	n := 0
	for range b.Lines(0) {
		n++
		if n == 3 {
			break
		}
	}
	if n != 3 {
		t.Errorf("Lines kept going after break: %d", n)
	}
	n = 0
	for range b.Chunks() {
		n++
		break
	}
	if n != 1 {
		t.Errorf("Chunks kept going after break: %d", n)
	}
}

func TestMultibytePositions(t *testing.T) {
	// "aα日😀" is 1+2+3+4 bytes.
	b := New("aα日😀\nβ")
	checkAll(t, b, "aα日😀\nβ")
	tests := []struct {
		in, want Position
	}{
		{Position{0, 0}, Position{0, 0}},
		{Position{0, 1}, Position{0, 1}},
		{Position{0, 2}, Position{0, 1}}, // inside α
		{Position{0, 3}, Position{0, 3}},
		{Position{0, 5}, Position{0, 3}}, // inside 日
		{Position{0, 7}, Position{0, 6}}, // inside 😀
		{Position{0, 9}, Position{0, 6}}, // inside 😀
		{Position{0, 10}, Position{0, 10}},
		{Position{0, 11}, Position{0, 10}},
		{Position{1, 1}, Position{1, 0}}, // inside β
		{Position{1, 2}, Position{1, 2}},
	}
	for _, tt := range tests {
		if got := b.ValidatePosition(tt.in); got != tt.want {
			t.Errorf("ValidatePosition(%+v) = %+v, want %+v", tt.in, got, tt.want)
		}
	}
}
