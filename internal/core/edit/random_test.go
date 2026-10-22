package edit

import (
	"math/rand"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rm4n0s/emetos/internal/core/buffer"
)

// offsetPosition is the position of a byte offset in text.
func offsetPosition(text string, off int) buffer.Position {
	line := strings.Count(text[:off], "\n")
	return buffer.Position{Line: line, Col: off - (strings.LastIndexByte(text[:off], '\n') + 1)}
}

func positionOffset(text string, p buffer.Position) int {
	off := 0
	for i := 0; i < p.Line; i++ {
		off += strings.IndexByte(text[off:], '\n') + 1
	}
	return off + p.Col
}

// randomText makes a text of a few short lines of characters of every width.
func randomText(r *rand.Rand) string {
	alphabet := []string{"a", "b", " ", "\n", "é", "😍", "x", "\n"}
	var sb strings.Builder
	for n := r.Intn(30); n > 0; n-- {
		sb.WriteString(alphabet[r.Intn(len(alphabet))])
	}
	return sb.String()
}

// boundaries are the byte offsets of text where a cursor may be.
func boundaries(text string) []int {
	var out []int
	for i := 0; i <= len(text); {
		out = append(out, i)
		if i == len(text) {
			break
		}
		_, w := utf8.DecodeRuneInString(text[i:])
		i += w
	}
	return out
}

// randomCarets picks distinct places for cursors, in a random order.
func randomCarets(r *rand.Rand, text string) []int {
	b := boundaries(text)
	r.Shuffle(len(b), func(i, j int) { b[i], b[j] = b[j], b[i] })
	return b[:1+r.Intn(min(len(b), 5))]
}

func caretsAt(text string, offs []int) []buffer.Selection {
	var sels []buffer.Selection
	for _, o := range offs {
		sels = append(sels, buffer.NewCursor(offsetPosition(text, o)))
	}
	return sels
}

// check compares the result of a command with the text and the cursor offsets
// that a plain string model gives.
func checkRandom(t *testing.T, seed int, before string, res Result, wantText string, wantOffs []int) {
	t.Helper()
	d := newEd(t, DefaultOptions(), before)
	if len(res.Edits) > 0 {
		if _, err := d.doc.Edit(res.Edits, editOptionsOf(res)); err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
	}
	if got := d.doc.Text(); got != wantText {
		t.Fatalf("seed %d: text %q, want %q (before %q)", seed, got, wantText, before)
	}
	var got []int
	for _, s := range res.Selections {
		if s.Anchor != s.Active {
			t.Fatalf("seed %d: selection %+v is not empty", seed, s)
		}
		got = append(got, positionOffset(wantText, s.Active))
	}
	if len(got) != len(wantOffs) {
		t.Fatalf("seed %d: cursors %v, want %v", seed, got, wantOffs)
	}
	for i := range got {
		if got[i] != wantOffs[i] {
			t.Fatalf("seed %d: cursors %v, want %v (text %q, before %q)", seed, got, wantOffs, wantText, before)
		}
	}
}

func TestRandomMultiCursorTyping(t *testing.T) {
	for seed := 0; seed < 400; seed++ {
		r := rand.New(rand.NewSource(int64(seed)))
		text := randomText(r)
		offs := randomCarets(r, text)
		ins := []string{"-", "xy", "é", "😍"}[r.Intn(4)]
		cmd := NewCommands(NewModel(newEd(t, DefaultOptions(), text).doc.Snapshot()), DefaultOptions())
		res := cmd.Type(caretsAt(text, offs), ins)

		sorted := append([]int(nil), offs...)
		sort.Ints(sorted)
		var sb strings.Builder
		prev := 0
		after := map[int]int{}
		for i, o := range sorted {
			sb.WriteString(text[prev:o])
			sb.WriteString(ins)
			prev = o
			after[o] = o + (i+1)*len(ins)
		}
		sb.WriteString(text[prev:])
		var want []int
		for _, o := range offs {
			want = append(want, after[o])
		}
		checkRandom(t, seed, text, res, sb.String(), want)
	}
}

func TestRandomMultiCursorDeleting(t *testing.T) {
	opts := DefaultOptions()
	opts.UseTabStops = false
	for seed := 0; seed < 400; seed++ {
		r := rand.New(rand.NewSource(int64(1000 + seed)))
		text := randomText(r)
		offs := randomCarets(r, text)
		left := r.Intn(2) == 0
		cmd := NewCommands(NewModel(newEd(t, opts, text).doc.Snapshot()), opts)
		var res Result
		if left {
			res = cmd.DeleteLeft(caretsAt(text, offs))
		} else {
			res = cmd.DeleteRight(caretsAt(text, offs))
		}
		// The string model: delete the character before or after each cursor.
		type span struct{ from, to int }
		spans := map[int]span{}
		for _, o := range offs {
			switch {
			case left && o > 0:
				_, w := utf8.DecodeLastRuneInString(text[:o])
				spans[o] = span{o - w, o}
			case !left && o < len(text):
				_, w := utf8.DecodeRuneInString(text[o:])
				spans[o] = span{o, o + w}
			default:
				spans[o] = span{o, o}
			}
		}
		sorted := append([]int(nil), offs...)
		sort.Ints(sorted)
		var sb strings.Builder
		prev, removed := 0, 0
		after := map[int]int{}
		for _, o := range sorted {
			s := spans[o]
			sb.WriteString(text[prev:s.from])
			prev = s.to
			removed += s.to - s.from
			after[o] = s.from - (removed - (s.to - s.from))
		}
		sb.WriteString(text[prev:])
		var want []int
		for _, o := range offs {
			want = append(want, after[o])
		}
		checkRandom(t, seed, text, res, sb.String(), want)
	}
}
